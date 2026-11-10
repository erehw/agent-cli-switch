package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"acsw/internal/cred"
	"acsw/internal/exitcode"
	"acsw/internal/home"
	"acsw/internal/proc"
	tu "acsw/internal/testutil"
)

const fakeOfficialConfig = `model = "gpt-official"
model_provider = "openai"
model_reasoning_effort = "high"
preferred_auth_method = "chatgpt"
approval_policy = "on-request"
sandbox_mode = "workspace-write"
notify = ["notify.exe"]
profile = "safe"
otel_x = 1

[features]
web_search = true

[mcp_servers.fs]
command = "npx"
env = { SECRET_TOKEN = "dummy-mcp-secret" }

[projects.'D:\work\p']
trust_level = "trusted"

[profiles.safe]
approval_policy = "never"

[profiles.fast]
model = "gpt-mini"

[model_providers.foo]
base_url = "http://127.0.0.1:1"

[tui]
notifications = true
`

func importFixture(t *testing.T) (*App, string, string) {
	t.Helper()
	a, _ := newApp(t, "")
	tu.Must(t, a.Cred.Set(cred.Target("api1"), "dummy-k"))
	_, err := a.Use(home.UseOptions{Home: "home1", Source: "api1"})
	tu.Must(t, err)
	cfg := filepath.Join(a.Paths.HomeDir("home1"), "config.toml")
	b, err := os.ReadFile(cfg)
	tu.Must(t, err)
	// home 自己块外已有的设置：一个不同的 approval_policy（顶层键，放在 top 块后）和一个本地 mcp
	s := strings.Replace(string(b), "# <<< acsw:top <<<", "# <<< acsw:top <<<\n\napproval_policy = \"never\"", 1)
	tu.Must(t, os.WriteFile(cfg, []byte(s+"\n[mcp_servers.local]\ncommand = \"local.exe\"\n"), 0o644))
	official := filepath.Join(t.TempDir(), "fake-official-codex")
	tu.Must(t, os.MkdirAll(official, 0o700))
	tu.Must(t, os.WriteFile(filepath.Join(official, "config.toml"), []byte(fakeOfficialConfig), 0o600))
	return a, official, cfg
}

func TestImportBase(t *testing.T) {
	a, official, cfg := importFixture(t)
	before, _ := os.ReadFile(cfg)

	r, err := a.ImportBase("home1", official, true)
	tu.Must(t, err)
	got := map[string]string{}
	for _, c := range r.Changes {
		got[c.Key] = c.Action
	}
	want := map[string]string{"approval_policy": "replace", "sandbox_mode": "add", "notify": "add", "features.web_search": "add", "mcp_servers.fs": "add",
		`projects.D:\work\p`: "add", "profiles.safe": "add", "profile": "add", "tui.notifications": "add"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s 应为 %s，实际 %q（全部：%v）", k, v, got[k], got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("多出改动：%v", got)
	}
	skipped := map[string]bool{}
	for _, s := range r.Skipped {
		skipped[s.Key] = true
	}
	for _, k := range []string{"model", "model_provider", "model_reasoning_effort", "preferred_auth_method", "model_providers", "otel_x", "profiles.fast"} {
		if !skipped[k] {
			t.Errorf("%s 应跳过", k)
		}
	}
	jb, _ := json.Marshal(r)
	if strings.Contains(string(jb), "dummy-mcp-secret") || strings.Contains(string(jb), "npx") {
		t.Fatal("结果里不应有值")
	}
	if after, _ := os.ReadFile(cfg); string(after) != string(before) || r.Written {
		t.Fatal("预演不应写文件")
	}

	offBefore, _ := os.ReadFile(filepath.Join(official, "config.toml"))
	r, err = a.ImportBase("home1", official, false)
	tu.Must(t, err)
	if !r.Written || r.BackupFile == "" || !strings.HasPrefix(r.BackupFile, a.Paths.BackupDir()) {
		t.Fatalf("%+v", r)
	}
	if b, _ := os.ReadFile(r.BackupFile); string(b) != string(before) {
		t.Fatal("备份内容不对")
	}
	if offAfter, _ := os.ReadFile(filepath.Join(official, "config.toml")); string(offAfter) != string(offBefore) {
		t.Fatal("官方 config.toml 不能被改")
	}
	var m map[string]any
	nb, _ := os.ReadFile(cfg)
	_, err = toml.Decode(string(nb), &m)
	tu.Must(t, err)
	mcp := m["mcp_servers"].(map[string]any)
	mp := m["model_providers"].(map[string]any)
	if m["model_provider"] != "custom" || m["model"] != "gpt-x" || m["approval_policy"] != "on-request" || mcp["fs"] == nil || mcp["local"] == nil ||
		m["model_reasoning_effort"] != nil || m["preferred_auth_method"] != nil || mp["foo"] != nil || mp["custom"] == nil {
		t.Fatalf("导入结果不对：\n%s", nb)
	}
	if !strings.Contains(string(nb), "# >>> acsw:top") || !strings.Contains(string(nb), "# >>> acsw:tables") {
		t.Fatal("acsw 管理块应保留")
	}

	// 再导一次：没有改动
	r, err = a.ImportBase("home1", official, false)
	tu.Must(t, err)
	if len(r.Changes) != 0 || r.Written {
		t.Fatalf("%+v", r.Changes)
	}
	// 之后照常能切 source，导入的设置保留
	_, err = a.Use(home.UseOptions{Home: "home1", Source: "api2"})
	tu.Must(t, err)
	nb, _ = os.ReadFile(cfg)
	if !strings.Contains(string(nb), "mcp_servers.fs") || !strings.Contains(string(nb), "api2.example") {
		t.Fatalf("切换后：\n%s", nb)
	}
}

func TestImportBaseRefusals(t *testing.T) {
	a, official, _ := importFixture(t)
	// home 里有 codex 在跑：正式导入拒绝，预演可以
	cleanup, err := proc.Register(a.Paths.HomeDir("home1"), proc.Entry{PID: os.Getpid(), Started: time.Now()})
	tu.Must(t, err)
	_, err = a.ImportBase("home1", official, false)
	codeIs(t, err, exitcode.HomeInUse)
	r, err := a.ImportBase("home1", official, true)
	tu.Must(t, err)
	if len(r.Warnings) == 0 {
		t.Fatal("预演应提醒有 codex 在跑")
	}
	cleanup()

	// 别名 home
	_, err = a.SetHomeAlias("home2", official)
	tu.Must(t, err)
	_, err = a.ImportBase("home2", official, true)
	codeIs(t, err, exitcode.ReadonlyAlias)
	_, err = a.ImportBase("nope", official, true)
	codeIs(t, err, exitcode.HomeNotFound)
	_, err = a.ImportBase("home1", filepath.Join(t.TempDir(), "missing"), true)
	if err == nil {
		t.Fatal("官方 config.toml 不存在应报错")
	}
}

// 顶层键被写在 acsw 表块之后（实际属于 [model_providers.xxx]），重写会改变 provider 的含义：必须拒绝且不写文件。
func TestImportBaseRefusesProviderChange(t *testing.T) {
	a, official, cfg := importFixture(t)
	b, _ := os.ReadFile(cfg)
	tu.Must(t, os.WriteFile(cfg, []byte(strings.Replace(string(b), "# <<< acsw:tables <<<", "# <<< acsw:tables <<<\nweird_top = 1", 1)), 0o644))
	before, _ := os.ReadFile(cfg)
	_, err := a.ImportBase("home1", official, false)
	if err == nil || !strings.Contains(err.Error(), "model_providers") {
		t.Fatalf("应拒绝：%v", err)
	}
	if after, _ := os.ReadFile(cfg); string(after) != string(before) {
		t.Fatal("拒绝时不能写文件")
	}
}
