package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"acsw/internal/claude"
	"acsw/internal/config"
	"acsw/internal/cred"
	"acsw/internal/exitcode"
	"acsw/internal/home"
	"acsw/internal/source"
	tu "acsw/internal/testutil"
)

const dummyClaudeKey = "dummy-claude-key-not-real"

// claudeApp 建一个带 claude source gw1 的 App；SelfExe 指向一个路径里带空格的假 acsw.exe。
func claudeApp(t *testing.T) *App {
	t.Helper()
	a, _ := newApp(t, "")
	exe := filepath.Join(t.TempDir(), "dir with space", "acsw.exe")
	tu.Must(t, os.MkdirAll(filepath.Dir(exe), 0o700))
	tu.Must(t, os.WriteFile(exe, []byte("fake"), 0o700))
	a.SelfExe = exe
	f := source.ClaudeFields{BaseURL: "http://127.0.0.1:9/", Model: "m-main", Opus: "m-opus", Haiku: "m-haiku"}
	tu.Must(t, a.AddClaudeSource("gw1", f, dummyClaudeKey))
	return a
}

func codeIs(t *testing.T, err error, want exitcode.Code) {
	t.Helper()
	if c, ok := exitcode.Of(err); !ok || c != want {
		t.Fatalf("退出码应为 %d（%s），实际 %d：%v", want, want.Kind(), c, err)
	}
}

func readEnv(t *testing.T, p string) (string, map[string]string) {
	t.Helper()
	b, err := os.ReadFile(p)
	tu.Must(t, err)
	var f struct {
		APIKeyHelper string            `json:"apiKeyHelper"`
		Env          map[string]string `json:"env"`
	}
	tu.Must(t, json.Unmarshal(b, &f))
	return f.APIKeyHelper, f.Env
}

func TestAddClaudeSource(t *testing.T) {
	a := claudeApp(t)
	meta, err := config.LoadSource(a.Paths, "gw1")
	tu.Must(t, err)
	if !meta.IsClaude() || meta.Type != "custom" || meta.BaseURL != "http://127.0.0.1:9" || meta.OpusModel != "m-opus" || meta.KeyMode != "" {
		t.Fatalf("%+v", meta)
	}
	if k, err := a.Cred.Get("acsw/claude/gw1"); err != nil || k != dummyClaudeKey {
		t.Fatal("key 应存在 acsw/claude/gw1")
	}
	if _, err := a.Cred.Get(cred.Target("gw1")); err == nil {
		t.Fatal("不应写 acsw/codex/gw1")
	}
	for _, n := range []string{"export", "KEY"} {
		if err := a.AddClaudeSource(n, source.ClaudeFields{BaseURL: "http://x", Model: "m"}, "k"); err == nil {
			t.Fatalf("%s 应被拒绝", n)
		}
	}
	if err := a.AddClaudeSource("gw2", source.ClaudeFields{BaseURL: "http://x", Model: "m", ExtraEnv: map[string]string{"MY_TOKEN": "x"}}, "k"); err == nil {
		t.Fatal("extra_env 含 TOKEN 应被拒绝")
	}
	list, err := a.Sources()
	tu.Must(t, err)
	for _, s := range list {
		if s.Name == "gw1" && !s.HasLogin {
			t.Fatal("gw1 应显示有 key")
		}
	}
	_, err = a.Use(home.UseOptions{Home: "home1", Source: "gw1"})
	codeIs(t, err, exitcode.ToolMismatch)
}

func TestClaudeKeyAndToolMismatch(t *testing.T) {
	a := claudeApp(t)
	k, err := a.ClaudeKey("gw1")
	if err != nil || k != dummyClaudeKey {
		t.Fatal(err)
	}
	_, err = a.ClaudeKey("api1")
	codeIs(t, err, exitcode.ToolMismatch)
	_, err = a.ClaudeKey("nope")
	codeIs(t, err, exitcode.SourceInvalid)
	tu.Must(t, a.Cred.Delete("acsw/claude/gw1"))
	_, err = a.ClaudeKey("gw1")
	codeIs(t, err, exitcode.KeyUnreadable)
}

func TestExportClaudeHelperAndPlain(t *testing.T) {
	a := claudeApp(t)
	p, m, err := a.ExportClaude("gw1", "")
	tu.Must(t, err)
	if m != claude.ModeHelper || p != filepath.Join(filepath.Dir(a.Paths.Root), "claude", "settings", "gw1.json") {
		t.Fatal(p, m)
	}
	h, env := readEnv(t, p)
	if h != `"`+a.SelfExe+`" claude key gw1` || env["ANTHROPIC_AUTH_TOKEN"] != "" || env["ANTHROPIC_MODEL"] != "m-main" || env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "" {
		t.Fatalf("%q %v", h, env)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), dummyClaudeKey) {
		t.Fatal("helper 文件不应含 key")
	}
	st1, _ := os.Stat(p)
	if _, _, err := a.ExportClaude("gw1", "helper"); err != nil {
		t.Fatal(err)
	}
	st2, _ := os.Stat(p)
	if !st1.ModTime().Equal(st2.ModTime()) {
		t.Fatal("内容没变不应重写")
	}

	pp, m, err := a.ExportClaude("gw1", "plain")
	tu.Must(t, err)
	if m != claude.ModePlain || filepath.Base(pp) != "gw1.plain.json" {
		t.Fatal(pp, m)
	}
	h, env = readEnv(t, pp)
	if h != "" || env["ANTHROPIC_AUTH_TOKEN"] != dummyClaudeKey || env["ANTHROPIC_API_KEY"] != "" {
		t.Fatal("plain 文件不对")
	}
	if gi, err := os.ReadFile(filepath.Join(filepath.Dir(pp), ".gitignore")); err != nil || !strings.Contains(string(gi), "*.plain.json") {
		t.Fatal("应生成 .gitignore")
	}
	if _, _, err := a.ExportClaude("gw1", "env"); err == nil {
		t.Fatal("不认识的模式应报错")
	}
	a.SelfExe = filepath.Join(t.TempDir(), "50%", "acsw.exe")
	_, _, err = a.ExportClaude("gw1", "helper")
	if err == nil || !strings.Contains(err.Error(), "%") {
		t.Fatal("路径含 % 应拒绝", err)
	}
}

func TestEditClaudeSource(t *testing.T) {
	a := claudeApp(t)
	hp, _, err := a.ExportClaude("gw1", "helper")
	tu.Must(t, err)
	pp, _, err := a.ExportClaude("gw1", "plain")
	tu.Must(t, err)
	nm, empty := "m-new", ""
	res, err := a.EditClaudeSource("gw1", source.ClaudeEdit{Model: &nm, Opus: &empty})
	tu.Must(t, err)
	if !res.MetaChanged || res.KeyChanged {
		t.Fatalf("%+v", res)
	}
	if config.Exists(pp) {
		t.Fatal("过时的明文文件应删掉")
	}
	_, env := readEnv(t, hp)
	if env["ANTHROPIC_MODEL"] != "m-new" || env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "" {
		t.Fatal("helper 文件应重新生成", env)
	}
	res, err = a.EditClaudeSource("gw1", source.ClaudeEdit{Key: "dummy-new"})
	tu.Must(t, err)
	if !res.KeyChanged || res.MetaChanged {
		t.Fatalf("%+v", res)
	}
	if k, _ := a.Cred.Get("acsw/claude/gw1"); k != "dummy-new" {
		t.Fatal("key 没更新")
	}
	bad := "ftp://x"
	if _, err := a.EditClaudeSource("gw1", source.ClaudeEdit{BaseURL: &bad}); err == nil {
		t.Fatal("非法 base_url 应拒绝")
	}
	_, err = a.EditClaudeSource("api1", source.ClaudeEdit{Model: &nm})
	codeIs(t, err, exitcode.ToolMismatch)
	if _, err := a.EditCustomSource("gw1", "", "x", ""); err == nil {
		t.Fatal("codex 的修改入口不应改 claude source")
	}
}

func TestRemoveClaudeSource(t *testing.T) {
	a := claudeApp(t)
	hp, _, _ := a.ExportClaude("gw1", "helper")
	pp, _, _ := a.ExportClaude("gw1", "plain")
	dst, err := a.RemoveCustomSource("gw1")
	tu.Must(t, err)
	if !config.Exists(dst) || config.Exists(a.Paths.SourceDir("gw1")) || config.Exists(hp) || config.Exists(pp) {
		t.Fatal("删除后目录应在备份区、settings 文件应删掉")
	}
	if _, err := a.Cred.Get("acsw/claude/gw1"); err == nil {
		t.Fatal("凭据应删掉")
	}
}

type claudeSeen struct {
	Args     []string `json:"args"`
	Len      int      `json:"stdin_len"`
	EnvNames []string `json:"env_names"`
}

func runFakeClaude(t *testing.T, a *App, opt ClaudeRunOptions, cfg string) (int, claudeSeen, error) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "seen.json")
	self, _ := os.Executable()
	a.ClaudePath = self
	a.Environ = func() []string {
		return append(os.Environ(), helperEnv+"="+out, "ANTHROPIC_AUTH_TOKEN=dummy-parent-token", "ANTHROPIC_BASE_URL=http://127.0.0.1:1", "CLAUDE_CODE_USE_BEDROCK=1", "CLAUDE_CONFIG_DIR="+cfg)
	}
	code, err := a.RunClaude(opt)
	var seen claudeSeen
	if err == nil {
		b, rerr := os.ReadFile(out)
		tu.Must(t, rerr)
		tu.Must(t, json.Unmarshal(b, &seen))
	}
	return code, seen, err
}

func TestRunClaudePassesSettingsArgsStdinAndCleansEnv(t *testing.T) {
	a := claudeApp(t)
	cfg := t.TempDir()
	tu.Must(t, os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(`{"env":{"ANTHROPIC_AUTH_TOKEN":"dummy-user-token","ANTHROPIC_BASE_URL":"http://127.0.0.1:2"}}`), 0o600))
	a.Stdin = strings.NewReader("hello from stdin")
	proj := t.TempDir()
	code, seen, err := runFakeClaude(t, a, ClaudeRunOptions{Source: "gw1", Dir: proj, Args: []string{"-p", "hi there", "--model", "x"}}, cfg)
	tu.Must(t, err)
	if code != 7 {
		t.Fatal("退出码应透传", code)
	}
	want := []string{"--settings", a.Paths.ClaudeSettingsFile("gw1", false), "-p", "hi there", "--model", "x"}
	if strings.Join(seen.Args, "|") != strings.Join(want, "|") {
		t.Fatalf("参数 %q", seen.Args)
	}
	if seen.Len != len("hello from stdin") {
		t.Fatal("stdin 应透传", seen.Len)
	}
	names := "," + strings.Join(seen.EnvNames, ",") + ","
	if strings.Contains(names, "ANTHROPIC_") || strings.Contains(names, ",CLAUDE_CODE_USE_BEDROCK,") || !strings.Contains(names, ",CLAUDE_CONFIG_DIR,") {
		t.Fatal("子进程环境应去掉 ANTHROPIC_*/CLAUDE_CODE_USE_*，保留 CLAUDE_CONFIG_DIR：", names)
	}
	errs := a.Stderr.(interface{ String() string }).String()
	if strings.Contains(errs, "dummy") || strings.Contains(errs, "可能会漏进来") {
		t.Fatalf("用户 settings 里只有 acsw 盖得住的变量，不应提醒，也不应输出任何值：%s", errs)
	}
}

func TestRunClaudeLeakWarningAndStrict(t *testing.T) {
	a := claudeApp(t)
	cfg := t.TempDir()
	tu.Must(t, os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(`{"env":{"ANTHROPIC_BETA_TEST":"dummy-value"}}`), 0o600))
	code, _, err := runFakeClaude(t, a, ClaudeRunOptions{Source: "gw1", Dir: t.TempDir()}, cfg)
	tu.Must(t, err)
	errs := a.Stderr.(interface{ String() string }).String()
	if code != 7 || !strings.Contains(errs, "ANTHROPIC_BETA_TEST") || strings.Contains(errs, "dummy-value") {
		t.Fatalf("应提醒变量名（不含值）：%s", errs)
	}
	_, _, err = runFakeClaude(t, a, ClaudeRunOptions{Source: "gw1", Dir: t.TempDir(), Strict: true}, cfg)
	codeIs(t, err, exitcode.SettingsClash)

	// 项目级 settings 的 apiKeyHelper：helper 模式会被盖住，plain 模式要提醒
	proj := t.TempDir()
	tu.Must(t, os.MkdirAll(filepath.Join(proj, ".claude"), 0o700))
	tu.Must(t, os.WriteFile(filepath.Join(proj, ".claude", "settings.local.json"), []byte(`{"apiKeyHelper":"x"}`), 0o600))
	empty := t.TempDir()
	_, _, err = runFakeClaude(t, a, ClaudeRunOptions{Source: "gw1", Dir: proj, Strict: true}, empty)
	tu.Must(t, err)
	_, _, err = runFakeClaude(t, a, ClaudeRunOptions{Source: "gw1", Dir: proj, Strict: true, KeyMode: "plain"}, empty)
	codeIs(t, err, exitcode.SettingsClash)
}

func TestRunClaudeErrors(t *testing.T) {
	a := claudeApp(t)
	cfg := t.TempDir()
	_, _, err := runFakeClaude(t, a, ClaudeRunOptions{Source: "gw1", Dir: cfg, Args: []string{"--settings=x.json"}}, cfg)
	codeIs(t, err, exitcode.BadArgs)
	_, _, err = runFakeClaude(t, a, ClaudeRunOptions{Source: "api1", Dir: cfg}, cfg)
	codeIs(t, err, exitcode.ToolMismatch)
	_, _, err = runFakeClaude(t, a, ClaudeRunOptions{Source: "nope", Dir: cfg}, cfg)
	codeIs(t, err, exitcode.SourceInvalid)
	_, _, err = runFakeClaude(t, a, ClaudeRunOptions{Source: "gw1", Dir: cfg, KeyMode: "bad"}, cfg)
	codeIs(t, err, exitcode.BadArgs)
	a.ClaudePath = filepath.Join(cfg, "no-such-claude.exe")
	_, err = a.RunClaude(ClaudeRunOptions{Source: "gw1", Dir: cfg})
	codeIs(t, err, exitcode.ClaudeMissing)
	a.ClaudePath = ""
	tu.Must(t, a.Cred.Delete("acsw/claude/gw1"))
	_, _, err = runFakeClaude(t, a, ClaudeRunOptions{Source: "gw1", Dir: cfg}, cfg)
	codeIs(t, err, exitcode.KeyUnreadable)
}

func TestClaudeModelsUsesAnthropicHeaders(t *testing.T) {
	a, _ := newApp(t, "")
	var got http.Header
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, path = r.Header.Clone(), r.URL.Path
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer srv.Close()
	tu.Must(t, a.AddClaudeSource("gw1", source.ClaudeFields{BaseURL: srv.URL + "/", Model: "m"}, dummyClaudeKey))
	body, err := a.SourceModels(context.Background(), "gw1")
	tu.Must(t, err)
	if string(body) != `{"data":[{"id":"m1"}]}` || path != "/v1/models" || got.Get("x-api-key") != dummyClaudeKey || got.Get("Authorization") != "Bearer "+dummyClaudeKey || got.Get("anthropic-version") == "" {
		t.Fatalf("%s %s %v", body, path, got)
	}
}

func TestClaudeStatus(t *testing.T) {
	a := claudeApp(t)
	cfg := t.TempDir()
	a.Environ = func() []string { return []string{"CLAUDE_CONFIG_DIR=" + cfg} }
	st, err := a.ClaudeStatus()
	tu.Must(t, err)
	if st.ConfigDir != cfg || st.SessionsDir != filepath.Join(cfg, "projects") || st.SettingsDir != a.Paths.ClaudeSettingsDir() || len(st.Sources) != 1 {
		t.Fatalf("%+v", st)
	}
	s := st.Sources[0]
	if s.Name != "gw1" || !s.HasKey || s.KeyMode != "helper" || s.Settings.Exists || s.Models["opus"] != "m-opus" || s.Launch != "acsw claude gw1" {
		t.Fatalf("%+v", s)
	}
	_, _, err = a.ExportClaude("gw1", "")
	tu.Must(t, err)
	st, _ = a.ClaudeStatus()
	if !st.Sources[0].Settings.Exists || !st.Sources[0].Settings.Current {
		t.Fatalf("%+v", st.Sources[0])
	}
	b, _ := json.Marshal(st)
	if strings.Contains(string(b), dummyClaudeKey) {
		t.Fatal("状态里不应有 key")
	}
	var te *source.ToolError
	_, err = source.LoadClaude(a.Paths, "api1")
	if !errors.As(err, &te) {
		t.Fatal(err)
	}
}
