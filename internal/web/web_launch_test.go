package web

import (
	"encoding/json"
	"testing"

	"acsw/internal/config"
	tu "acsw/internal/testutil"
)

func TestStatusHasLaunchCommands(t *testing.T) {
	s, p := newServer(t)
	m, err := config.LoadMap(p)
	tu.Must(t, err)
	m.SetProject(`M:\work\my proj`, "home1")
	tu.Must(t, config.SaveMap(p, m))

	w := do(s.Handler(), "GET", "/api/status", host, "", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var d struct {
		Homes []struct {
			Home     string   `json:"home"`
			Dir      string   `json:"dir"`
			AliasOf  *string  `json:"alias_of"`
			Projects []string `json:"projects"`
			Launches []struct {
				Dir  string `json:"dir"`
				Cmd  string `json:"cmd"`
				PS   string `json:"ps"`
				Warn string `json:"warn"`
			} `json:"launches"`
		} `json:"homes"`
	}
	tu.Must(t, json.Unmarshal(w.Body.Bytes(), &d))
	found := false
	for _, h := range d.Homes {
		if h.Launches == nil {
			t.Fatal("launches 应为数组", h.Home)
		}
		if h.Dir != p.CodexHome(h.Home, m.Homes[h.Home]) || h.Dir != p.HomeDir(h.Home) || h.AliasOf == nil || *h.AliasOf != "" {
			t.Fatal("dir/alias_of", h.Home, h.Dir)
		}
		if h.Home != "home1" {
			continue
		}
		for _, l := range h.Launches {
			if l.Dir == `M:\work\my proj` {
				found = true
				if l.Cmd != `cd /d "M:\work\my proj" && acsw codex --expect-home home1` || l.PS != `Set-Location -LiteralPath 'M:\work\my proj'; acsw codex --expect-home home1` || l.Warn != "" {
					t.Fatal(l)
				}
			}
		}
		if len(h.Launches) != len(h.Projects) {
			t.Fatal("每个项目目录都应有命令", h)
		}
	}
	if !found {
		t.Fatal(w.Body.String())
	}
}
