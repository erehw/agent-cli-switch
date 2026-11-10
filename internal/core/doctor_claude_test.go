package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tu "acsw/internal/testutil"
)

func checksOf(rep DoctorReport) map[string]DoctorCheck {
	m := map[string]DoctorCheck{}
	for _, c := range rep.Checks {
		m[c.Name] = c
	}
	return m
}

func TestDoctorClaude(t *testing.T) {
	a := claudeApp(t)
	cfg := t.TempDir()
	tu.Must(t, os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(`{"env":{"ANTHROPIC_AUTH_TOKEN":"dummy-user-token","ANTHROPIC_BETA_TEST":"dummy-v"}}`), 0o600))
	self, _ := os.Executable()
	out := filepath.Join(t.TempDir(), "seen.json")
	a.ClaudePath = self // 假 claude：--version 不返回版本号
	a.Environ = func() []string { return append(os.Environ(), "CLAUDE_CONFIG_DIR="+cfg, helperEnv+"="+out) }
	_, _, err := a.ExportClaude("gw1", "helper")
	tu.Must(t, err)
	_, _, err = a.ExportClaude("gw1", "plain")
	tu.Must(t, err)

	rep := a.Doctor()
	c := checksOf(rep)
	if c["claude"].Level != "warn" || c["claude-source:gw1"].Level != "ok" || c["claude-source:gw1:settings"].Level != "ok" || c["claude-source:gw1:plain"].Level != "warn" {
		t.Fatalf("%+v", rep.Checks)
	}
	if u := c["claude-user-settings"]; u.Level != "warn" || !strings.Contains(u.Msg, "ANTHROPIC_BETA_TEST") {
		t.Fatalf("%+v", u)
	}
	if _, ok := c["source:gw1"]; ok {
		t.Fatal("claude source 不应出现在 codex 的 source 检查里")
	}
	b, _ := json.Marshal(rep)
	for _, s := range []string{dummyClaudeKey, "dummy-user-token", "dummy-v"} {
		if strings.Contains(string(b), s) {
			t.Fatalf("doctor 输出不应含值 %s", s)
		}
	}

	// helper 路径没加引号 → error；key 缺失 → error
	hp := a.Paths.ClaudeSettingsFile("gw1", false)

	tu.Must(t, os.WriteFile(hp, []byte(strings.Replace(string(mustBuildFull(t, a)), `"\"`, `"`, 1)), 0o600))
	tu.Must(t, a.Cred.Delete("acsw/claude/gw1"))
	rep = a.Doctor()
	c = checksOf(rep)
	if c["claude-source:gw1"].Level != "error" || c["claude-source:gw1:settings"].Level != "error" || rep.OK {
		t.Fatalf("%+v", rep.Checks)
	}
}

// mustBuildFull 返回当前 helper 文件的内容（全量变量）。
func mustBuildFull(t *testing.T, a *App) []byte {
	t.Helper()
	tu.Must(t, a.Cred.Set("acsw/claude/gw1", dummyClaudeKey))
	p, _, err := a.ExportClaude("gw1", "helper")
	tu.Must(t, err)
	b, err := os.ReadFile(p)
	tu.Must(t, err)
	return b
}

func TestStatusReportSeparatesClaudeSources(t *testing.T) {
	a := claudeApp(t)
	cfg := t.TempDir()
	a.Environ = func() []string { return []string{"CLAUDE_CONFIG_DIR=" + cfg} }
	rep, err := a.StatusReport()
	tu.Must(t, err)
	for _, s := range rep.Sources {
		if s.Name == "gw1" {
			t.Fatal("sources 里只放 codex source")
		}
	}
	if len(rep.Claude.Sources) != 1 || rep.Claude.Sources[0].Name != "gw1" || rep.Claude.SessionsDir != filepath.Join(cfg, "projects") {
		t.Fatalf("%+v", rep.Claude)
	}
	b, _ := json.Marshal(rep)
	if strings.Contains(string(b), dummyClaudeKey) || !strings.Contains(string(b), `"claude":{"claude":`) {
		t.Fatalf("%s", b)
	}
}
