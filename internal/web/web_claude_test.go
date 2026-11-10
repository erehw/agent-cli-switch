package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"acsw/internal/cred"
)

func TestClaudePostGuards(t *testing.T) {
	s, _ := newServer(t)
	h := s.Handler()
	body := `{"name":"gw1","base_url":"http://x","model":"m","key":"dummy-k"}`
	for _, u := range []string{"/api/claude/add", "/api/claude/edit", "/api/claude/export"} {
		if w := do(h, "POST", u, host, "", body); w.Code != 403 {
			t.Fatal(u, "无 token", w.Code)
		}
		if w := do(h, "POST", u, "evil.example", "tok123", body); w.Code != 403 {
			t.Fatal(u, "外部 Host", w.Code)
		}
		if w := do(h, "POST", u, host, "tok123", `{"name":"`+strings.Repeat("a", 9000)+`"}`); w.Code != 400 {
			t.Fatal(u, "超大请求", w.Code)
		}
	}
}

func TestClaudeAddEditExportDelete(t *testing.T) {
	s, _ := newServer(t)
	h := s.Handler()
	mem := s.App.Cred.(*cred.Memory)
	exe := filepath.Join(t.TempDir(), "acsw.exe")
	if err := os.WriteFile(exe, []byte("fake"), 0o700); err != nil {
		t.Fatal(err)
	}

	s.App.SelfExe = exe
	s.App.Environ = func() []string { return []string{"CLAUDE_CONFIG_DIR=" + t.TempDir()} }

	w := do(h, "POST", "/api/claude/add", host, "tok123", `{"name":"gw1","base_url":"http://127.0.0.1:9","model":"m1","sonnet_model":"s1","key":"dummy-k"}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), "dummy-k") {
		t.Fatal(w.Code, w.Body.String())
	}
	if v, _ := mem.Get("acsw/claude/gw1"); v != "dummy-k" {
		t.Fatal("key 没存")
	}
	if w := do(h, "POST", "/api/claude/add", host, "tok123", `{"name":"key","base_url":"http://x","model":"m","key":"k"}`); w.Code != 400 {
		t.Fatal("保留名应拒绝", w.Code)
	}
	// claude source 不进 codex 的 sources，进 claude.sources
	w = do(h, "GET", "/api/status", host, "tok123", "")
	var st struct {
		Sources []struct{ Name string } `json:"sources"`
		Claude  struct {
			Sources []struct {
				Name   string            `json:"name"`
				Models map[string]string `json:"models"`
				HasKey bool              `json:"has_key"`
			} `json:"sources"`
		} `json:"claude"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil || strings.Contains(w.Body.String(), "dummy-k") {
		t.Fatal(err, w.Body.String())
	}
	for _, x := range st.Sources {
		if x.Name == "gw1" {
			t.Fatal("gw1 不应出现在 codex sources")
		}
	}
	if len(st.Claude.Sources) != 1 || st.Claude.Sources[0].Models["sonnet"] != "s1" || !st.Claude.Sources[0].HasKey {
		t.Fatalf("%+v", st.Claude)
	}
	// codex 切换不能用 claude source
	if w := do(h, "POST", "/api/use", host, "tok123", `{"home":"home1","source":"gw1"}`); w.Code == 200 {
		t.Fatal("claude source 不能用于 codex home")
	}

	w = do(h, "POST", "/api/claude/edit", host, "tok123", `{"name":"gw1","model":"m2","sonnet_model":""}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"meta_changed":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do(h, "POST", "/api/claude/edit", host, "tok123", `{"name":"api1","model":"m2"}`); w.Code != 400 || !strings.Contains(w.Body.String(), "source_tool_mismatch") {
		t.Fatal(w.Code, w.Body.String())
	}

	w = do(h, "POST", "/api/claude/export", host, "tok123", `{"name":"gw1","mode":"helper"}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), "dummy-k") || !strings.Contains(w.Body.String(), "gw1.json") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do(h, "POST", "/api/claude/export", host, "tok123", `{"name":"gw1","mode":"plain"}`); w.Code != 400 {
		t.Fatal("明文导出没确认应拒绝", w.Code)
	}
	w = do(h, "POST", "/api/claude/export", host, "tok123", `{"name":"gw1","mode":"plain","confirm":true}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), "dummy-k") || !strings.Contains(w.Body.String(), "warning") {
		t.Fatal(w.Code, w.Body.String())
	}

	w = do(h, "POST", "/api/source/delete", host, "tok123", `{"name":"gw1"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := mem.Get("acsw/claude/gw1"); err == nil {
		t.Fatal("key 应删掉")
	}
}
