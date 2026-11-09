package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"acsw/internal/config"
	"acsw/internal/core"
	"acsw/internal/cred"
	"acsw/internal/home"
	"acsw/internal/proc"
	tu "acsw/internal/testutil"
)

func TestStatusJSON(t *testing.T) {
	const key = "sk-status-SECRET-000"
	p := tu.Fixture(t)
	c := cred.NewMemory()
	tu.Must(t, c.Set(cred.Target("api1"), key))
	f := &proc.Fake{AlivePIDs: map[int]bool{}}
	a := &core.App{Paths: p, Cred: c, Proc: f, Stdout: io.Discard, Stderr: io.Discard}
	_, err := a.Use(home.UseOptions{Home: "home2", Source: "api1"})
	tu.Must(t, err)
	_, err = a.MapProject(p.Root+`\prj with space`, "home2")
	tu.Must(t, err)
	f.AlivePIDs[4321] = true
	cleanup, err := proc.Register(p.HomeDir("home2"), proc.Entry{PID: 4321})
	tu.Must(t, err)
	defer cleanup()

	var out bytes.Buffer
	if code := statusJSON(a, &out); code != 0 {
		t.Fatal(code)
	}
	if strings.Contains(out.String(), key) || strings.Contains(out.String(), "acct-plus") {
		t.Fatal("输出里不能有 key 或 auth.json 内容")
	}
	// 形状：只认文档里写的字段
	var raw map[string]json.RawMessage
	tu.Must(t, json.Unmarshal(out.Bytes(), &raw))
	for _, k := range []string{"version", "root", "homes", "sources"} {
		if _, ok := raw[k]; !ok {
			t.Fatal("缺字段", k)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	dec.DisallowUnknownFields()
	var rep core.StatusReport
	tu.Must(t, dec.Decode(&rep))
	if rep.Version != 1 || rep.Root != p.Root || len(rep.Homes) != 2 || len(rep.Sources) != 4 {
		t.Fatal(out.String())
	}
	h1, h2 := rep.Homes[0], rep.Homes[1]
	if h1.Dir != p.HomeDir("home1") || h2.Dir != p.HomeDir("home2") || h1.AliasOf != "" || h2.AliasOf != "" {
		t.Fatal("dir/alias_of", out.String())
	}
	if strings.Count(out.String(), `"alias_of": ""`) != 2 {
		t.Fatal("alias_of 应总是出现", out.String())
	}
	if h1.Home != "home1" || h1.Source != "" || h1.Provider != "" || h1.HasLogin != nil || h1.Projects == nil || len(h1.Projects) != 0 {
		t.Fatal("home1", out.String())
	}
	if h2.Home != "home2" || h2.Source != "api1" || h2.Provider != config.ProviderCustom || h2.BaseURL != "https://api1.example/v1" ||
		h2.Model != "gpt-x" || h2.HasLogin == nil || !*h2.HasLogin || h2.Running != 1 || len(h2.Projects) != 1 || !strings.HasSuffix(h2.Projects[0], "prj with space") {
		t.Fatal("home2", out.String())
	}
	for _, s := range rep.Sources {
		if s.UsedBy == nil {
			t.Fatal("used_by 应为数组", s.Name)
		}
		if s.Name == "api2" && s.HasLogin {
			t.Fatal("api2 没 key")
		}
	}
	// home1 未设置 source 时 has_login 是 null
	if !strings.Contains(out.String(), `"has_login": null`) {
		t.Fatal(out.String())
	}
}
