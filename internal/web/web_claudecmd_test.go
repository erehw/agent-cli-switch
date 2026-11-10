package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeCmdEndpoint(t *testing.T) {
	s, _ := newServer(t)
	h := s.Handler()
	bin := t.TempDir()
	s.AcswExe = filepath.Join(bin, "acsw.exe")
	if err := os.WriteFile(s.AcswExe, []byte("fake"), 0o700); err != nil {
		t.Fatal(err)
	}
	if w := do(h, "POST", "/api/claude/add", host, "tok123", `{"name":"gw1","base_url":"http://127.0.0.1:9","model":"m1","key":"dummy-k"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do(h, "POST", "/api/claude/cmd", host, "", `{"name":"gw1"}`); w.Code != 403 {
		t.Fatal("无 token", w.Code)
	}
	w := do(h, "POST", "/api/claude/cmd", host, "tok123", `{"name":"gw1"}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), "dummy-k") {
		t.Fatal(w.Code, w.Body.String())
	}
	var d struct {
		Result struct {
			Path        string `json:"path"`
			Overwritten bool   `json:"overwritten"`
		} `json:"result"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	if d.Result.Path != filepath.Join(bin, "claude-gw1.cmd") || d.Result.Overwritten {
		t.Fatal(w.Body.String())
	}
	b, _ := os.ReadFile(d.Result.Path)
	if !strings.Contains(string(b), `" claude gw1 %*`) || strings.Contains(string(b), "dummy-k") {
		t.Fatalf("%s", b)
	}
	// 别人写的同名文件：409 confirm_required，force 后覆盖
	other := t.TempDir()
	mine := filepath.Join(other, "claude-gw1.cmd")
	if err := os.WriteFile(mine, []byte("@echo mine\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"gw1","dir":` + strings.ReplaceAll(`"`+other+`"`, `\`, `\\`)
	if w := do(h, "POST", "/api/claude/cmd", host, "tok123", body+`}`); w.Code != 409 || !strings.Contains(w.Body.String(), "confirm_required") {
		t.Fatal(w.Code, w.Body.String())
	}
	if b, _ := os.ReadFile(mine); string(b) != "@echo mine\r\n" {
		t.Fatal("不应覆盖")
	}
	if w := do(h, "POST", "/api/claude/cmd", host, "tok123", body+`,"force":true}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do(h, "POST", "/api/claude/cmd", host, "tok123", `{"name":"nope"}`); w.Code < 400 {
		t.Fatal(w.Code)
	}
}
