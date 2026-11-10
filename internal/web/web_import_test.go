package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeImportBase(t *testing.T) {
	s, p := newServer(t)
	h := s.Handler()
	official := filepath.Join(t.TempDir(), "fake-official")
	if err := os.MkdirAll(official, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(official, "config.toml"), []byte("model = \"x\"\napproval_policy = \"never\"\n[mcp_servers.fs]\ncommand = \"npx\"\nenv = { K = \"dummy-secret\" }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.OfficialDir = official
	if w := do(h, "POST", "/api/home/import-base", host, "", `{"home":"home1","dry_run":true}`); w.Code != 403 {
		t.Fatal("无 token", w.Code)
	}
	w := do(h, "POST", "/api/home/import-base", host, "tok123", `{"home":"home1","dry_run":true}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"mcp_servers.fs"`) || strings.Contains(w.Body.String(), "dummy-secret") || strings.Contains(w.Body.String(), `"written":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(p.HomeDir("home1"), "config.toml")); err == nil {
		t.Fatal("预演不应写文件")
	}
	w = do(h, "POST", "/api/home/import-base", host, "tok123", `{"home":"home1"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"written":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	b, _ := os.ReadFile(filepath.Join(p.HomeDir("home1"), "config.toml"))
	if !strings.Contains(string(b), "approval_policy") || strings.Contains(string(b), "model =") {
		t.Fatalf("%s", b)
	}
	if w := do(h, "POST", "/api/home/import-base", host, "tok123", `{"home":"nope","dry_run":true}`); w.Code != 404 {
		t.Fatal(w.Code)
	}
}
