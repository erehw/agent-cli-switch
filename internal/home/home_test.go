package home

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"acsw/internal/config"
	"acsw/internal/proc"
	"acsw/internal/source"
	tu "acsw/internal/testutil"
)

const userCfg = `# 我自己的配置
model = "gpt-old"
approval_policy = "on-request"
model_provider = "api1"

[mcp_servers.foo]
command = "foo"
`

func env(p config.Paths) Env { return Env{Paths: p, Proc: &proc.Fake{AlivePIDs: map[int]bool{}}} }

func use(t *testing.T, e Env, h, s string) UseResult {
	t.Helper()
	r, err := Use(e, UseOptions{Home: h, Source: s})
	if err != nil {
		t.Fatalf("use %s %s 失败：%v", h, s, err)
	}
	return r
}

func homeSource(t *testing.T, p config.Paths, h string) string {
	m, err := config.LoadMap(p)
	tu.Must(t, err)
	return m.Homes[h].Source
}

func TestRenderConfigKeepsUserContent(t *testing.T) {
	out, err := RenderConfig(userCfg, config.SourceMeta{Type: "custom", BaseURL: "https://a.example/v1", Model: "gpt-x"})
	tu.Must(t, err)
	var c struct {
		Model          string `toml:"model"`
		ModelProvider  string `toml:"model_provider"`
		ApprovalPolicy string `toml:"approval_policy"`
		ModelProviders map[string]struct {
			BaseURL            string `toml:"base_url"`
			EnvKey             string `toml:"env_key"`
			WireAPI            string `toml:"wire_api"`
			RequiresOpenAIAuth bool   `toml:"requires_openai_auth"`
			SupportsWebsockets *bool  `toml:"supports_websockets"`
		} `toml:"model_providers"`
		MCP map[string]any `toml:"mcp_servers"`
	}
	_, err = toml.Decode(out, &c)
	tu.Must(t, err)
	cu := c.ModelProviders["custom"]
	if c.ModelProvider != "custom" || c.Model != "gpt-x" || c.ApprovalPolicy != "on-request" || c.MCP["foo"] == nil ||
		cu.BaseURL != "https://a.example/v1" || cu.EnvKey != "ACSW_API_KEY" || cu.WireAPI != "responses" || cu.RequiresOpenAIAuth ||
		cu.SupportsWebsockets == nil || *cu.SupportsWebsockets {
		t.Fatalf("配置不对：\n%s", out)
	}
	// 再切回 openai：管理块替换，custom 表消失，旧的被注释行保留注释状态
	out2, err := RenderConfig(out, config.SourceMeta{Type: "openai"})
	tu.Must(t, err)
	if strings.Contains(out2, "[model_providers.custom]") || strings.Count(out2, "acsw:top 由") != 1 {
		t.Fatalf("切回 openai 后不对：\n%s", out2)
	}
	var c2 map[string]any
	_, err = toml.Decode(out2, &c2)
	tu.Must(t, err)
	if c2["model_provider"] != "openai" || c2["approval_policy"] != "on-request" {
		t.Fatal(c2)
	}
	// 幂等
	out3, _ := RenderConfig(out2, config.SourceMeta{Type: "openai"})
	if out3 != out2 {
		t.Fatalf("不幂等：\n%s\n----\n%s", out2, out3)
	}
}

func TestRenderConfigRejectsUserCustomTable(t *testing.T) {
	_, err := RenderConfig("[model_providers.custom]\nbase_url=\"x\"\n", config.SourceMeta{Type: "openai"})
	if err == nil {
		t.Fatal("应拒绝")
	}
	_, err = RenderConfig("this is = = broken", config.SourceMeta{Type: "openai"})
	if err == nil {
		t.Fatal("坏配置应报错")
	}
}

func TestUseFullCycle(t *testing.T) {
	p := tu.Fixture(t)
	e := env(p)
	h1 := p.HomeDir("home1")
	db := filepath.Join(h1, "state_5.sqlite")
	tu.Must(t, os.WriteFile(filepath.Join(h1, "config.toml"), []byte(userCfg), 0o644))

	// 1. 首次 use plus1：放入登录
	use(t, e, "home1", "plus1")
	if tu.Read(filepath.Join(h1, "auth.json")) != tu.Read(source.AuthFile(p, "plus1")) {
		t.Fatal("auth.json 没放进 home")
	}
	tu.MakeDB(t, db, [][]any{{"t1", "openai", "/p1", 1, "a"}, {"t2", "openai", "/p1", 2, "b"}, {"t3", "api1", "/p1", 3, "旧的自定义名"}})

	// 模拟 codex 刷新了 token
	refreshed := tu.FakeAuth("acct-plus", "plus1-v2")
	tu.Must(t, os.WriteFile(filepath.Join(h1, "auth.json"), refreshed, 0o600))

	// 2. 切 api1：存回登录、移走 auth.json、改库
	r := use(t, e, "home1", "api1")
	if tu.Read(source.AuthFile(p, "plus1")) != string(refreshed) {
		t.Fatal("刷新后的登录没存回 plus1")
	}
	if config.Exists(filepath.Join(h1, "auth.json")) {
		t.Fatal("custom 时 home 里不该有 auth.json")
	}
	if r.Changed != 3 {
		t.Fatalf("应改 3 条，实际 %d", r.Changed)
	}
	for id, pv := range tu.Providers(t, db) {
		if pv != "custom" {
			t.Fatal(id, pv)
		}
	}
	if homeSource(t, p, "home1") != "api1" {
		t.Fatal("map 没更新")
	}
	if !config.Exists(filepath.Join(r.BackupDir, "manifest.txt")) {
		t.Fatal("没写备份清单")
	}
	bk, _ := os.ReadDir(r.BackupDir)
	var hasDB bool
	for _, b := range bk {
		if strings.HasSuffix(b.Name(), "state_5.sqlite") {
			hasDB = true
		}
	}
	if !hasDB {
		t.Fatal("没备份状态库")
	}
	cfg := tu.Read(filepath.Join(h1, "config.toml"))
	if !strings.Contains(cfg, `model_provider = "custom"`) || !strings.Contains(cfg, "[mcp_servers.foo]") {
		t.Fatal(cfg)
	}

	// 3. custom → custom（api2）：不改库
	r = use(t, e, "home1", "api2")
	if r.Changed != 0 {
		t.Fatal("custom 之间不应改库")
	}

	// 4. 切回 pro1：放入 pro1 登录，库改回 openai
	r = use(t, e, "home1", "pro1")
	if r.Changed != 3 || tu.Read(filepath.Join(h1, "auth.json")) != tu.Read(source.AuthFile(p, "pro1")) {
		t.Fatal("切回 openai 不对", r.Changed)
	}
	// 5. pro1 → plus1：openai 之间不改库，pro1 存回
	r = use(t, e, "home1", "plus1")
	if r.Changed != 0 || tu.Read(filepath.Join(h1, "auth.json")) != string(refreshed) {
		t.Fatal("openai 之间切换不对")
	}
}

func TestUseRejectsSameOpenAISourceInTwoHomes(t *testing.T) {
	p := tu.Fixture(t)
	e := env(p)
	use(t, e, "home1", "plus1")
	_, err := Use(e, UseOptions{Home: "home2", Source: "plus1"})
	if err == nil || !strings.Contains(err.Error(), "正被 home home1 使用") {
		t.Fatal(err)
	}
	// custom 可以共用
	use(t, e, "home1", "api1")
	use(t, e, "home2", "api1")
	// home1 已不用 plus1，home2 现在可以用
	use(t, e, "home2", "plus1")
}

func TestUseRejectsRunningHome(t *testing.T) {
	p := tu.Fixture(t)
	f := &proc.Fake{AlivePIDs: map[int]bool{4242: true}}
	e := Env{Paths: p, Proc: f}
	use(t, e, "home1", "plus1")
	cleanup, err := proc.Register(p.HomeDir("home1"), proc.Entry{PID: 4242, ChildPID: 4243})
	tu.Must(t, err)
	defer cleanup()
	_, err = Use(e, UseOptions{Home: "home1", Source: "api1"})
	if err == nil || !strings.Contains(err.Error(), "还有 1 个 codex 在跑") {
		t.Fatal(err)
	}
	// 进程退出后可以切换，残留登记被清理
	f.AlivePIDs[4242] = false
	use(t, e, "home1", "api1")
	if ents, _ := os.ReadDir(proc.RunDir(p.HomeDir("home1"))); len(ents) != 0 {
		t.Fatal("残留登记没清理")
	}
}

func TestUseUnmanagedCodexWarnAndStrict(t *testing.T) {
	p := tu.Fixture(t)
	f := &proc.Fake{AlivePIDs: map[int]bool{100: true}, Procs: []proc.PInfo{
		{PID: 100, PPID: 1, Name: "acsw.exe"}, {PID: 101, PPID: 100, Name: "node.exe"}, {PID: 102, PPID: 101, Name: "codex.exe"}, // acsw 启动的（home2）
		{PID: 200, PPID: 1, Name: "codex.exe"}, // 外部的
	}}
	cleanup, err := proc.Register(p.HomeDir("home2"), proc.Entry{PID: 100, ChildPID: 101})
	tu.Must(t, err)
	defer cleanup()
	e := Env{Paths: p, Proc: f}
	r := use(t, e, "home1", "api1")
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "[200]") {
		t.Fatal(r.Warnings)
	}
	_, err = Use(e, UseOptions{Home: "home1", Source: "api2", StrictCodex: true})
	if err == nil || !strings.Contains(err.Error(), "--strict") {
		t.Fatal(err)
	}
}

func TestUseRollback(t *testing.T) {
	for _, failAt := range []string{"saveback", "auth", "config", "db", "map"} {
		t.Run(failAt, func(t *testing.T) {
			p := tu.Fixture(t)
			e := env(p)
			h1 := p.HomeDir("home1")
			use(t, e, "home1", "plus1")
			db := filepath.Join(h1, "state_5.sqlite")
			tu.MakeDB(t, db, [][]any{{"t1", "openai", "/p1", 1, "a"}})
			tu.Must(t, os.WriteFile(filepath.Join(h1, "auth.json"), tu.FakeAuth("acct-plus", "refreshed"), 0o600))
			before := map[string]string{}
			files := []string{filepath.Join(h1, "auth.json"), filepath.Join(h1, "config.toml"), p.MapFile(), source.AuthFile(p, "plus1")}
			for _, f := range files {
				before[f] = tu.Read(f)
			}
			e.Hook = func(s string) error {
				if s == failAt {
					return errors.New("模拟故障")
				}
				return nil
			}
			_, err := Use(e, UseOptions{Home: "home1", Source: "api1"})
			if err == nil || !strings.Contains(err.Error(), "已回滚") {
				t.Fatal(err)
			}
			for _, f := range files {
				if tu.Read(f) != before[f] {
					t.Errorf("%s 没恢复", f)
				}
			}
			if pv := tu.Providers(t, db)["t1"]; pv != "openai" {
				t.Errorf("库没恢复：%s", pv)
			}
			if homeSource(t, p, "home1") != "plus1" {
				t.Error("map 没恢复")
			}
		})
	}
}

func TestUseDryRun(t *testing.T) {
	p := tu.Fixture(t)
	e := env(p)
	use(t, e, "home1", "plus1")
	db := filepath.Join(p.HomeDir("home1"), "state_5.sqlite")
	tu.MakeDB(t, db, [][]any{{"t1", "openai", "/p1", 1, "a"}})
	mapBefore := tu.Read(p.MapFile())
	r, err := Use(e, UseOptions{Home: "home1", Source: "api1", DryRun: true})
	tu.Must(t, err)
	if r.Changed != 1 || len(r.Steps) < 3 || r.BackupDir != "" {
		t.Fatalf("%+v", r)
	}
	if tu.Read(p.MapFile()) != mapBefore || tu.Providers(t, db)["t1"] != "openai" || !config.Exists(filepath.Join(p.HomeDir("home1"), "auth.json")) {
		t.Fatal("dry-run 改了文件")
	}
}

func TestUseRejectsAccountMismatch(t *testing.T) {
	p := tu.Fixture(t)
	e := env(p)
	use(t, e, "home1", "plus1")
	tu.Must(t, os.WriteFile(filepath.Join(p.HomeDir("home1"), "auth.json"), tu.FakeAuth("someone-else", "x"), 0o600))
	_, err := Use(e, UseOptions{Home: "home1", Source: "api1"})
	if err == nil || !strings.Contains(err.Error(), "不是同一个账号") {
		t.Fatal(err)
	}
}

func TestUseErrors(t *testing.T) {
	p := tu.Fixture(t)
	e := env(p)
	if _, err := Use(e, UseOptions{Home: "nohome", Source: "plus1"}); err == nil {
		t.Fatal("home 不存在应报错")
	}
	if _, err := Use(e, UseOptions{Home: "home1", Source: "nosrc"}); err == nil {
		t.Fatal("source 不存在应报错")
	}
	os.Remove(source.AuthFile(p, "pro1"))
	if _, err := Use(e, UseOptions{Home: "home1", Source: "pro1"}); err == nil || !strings.Contains(err.Error(), "没有 auth.json") {
		t.Fatal(err)
	}
}

func TestStatusAll(t *testing.T) {
	p := tu.Fixture(t)
	e := env(p)
	use(t, e, "home1", "plus1")
	use(t, e, "home2", "api1")
	st, err := StatusAll(p, e.Proc)
	tu.Must(t, err)
	if len(st) != 2 || st[0].Provider != "openai" || st[1].Provider != "custom" {
		t.Fatalf("%+v", st)
	}
}

func TestAddHome(t *testing.T) {
	p := config.Paths{Root: t.TempDir()}
	tu.Must(t, Add(p, "h9"))
	if err := Add(p, "h9"); err == nil {
		t.Fatal("重复应报错")
	}
	if err := Add(p, "../bad"); err == nil {
		t.Fatal("坏名应报错")
	}
}

func TestUseRollbackOnUnwritableConfig(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("Windows 或 root 下目录只读无效")
	}
	p := tu.Fixture(t)
	e := env(p)
	use(t, e, "home1", "plus1")
	h1 := p.HomeDir("home1")
	// 让 home 目录不可写，原子写 config.toml 失败
	authBefore := tu.Read(source.AuthFile(p, "plus1"))
	tu.Must(t, os.WriteFile(filepath.Join(h1, "auth.json"), tu.FakeAuth("acct-plus", "r2"), 0o600))
	tu.Must(t, os.Chmod(h1, 0o500))
	defer os.Chmod(h1, 0o700)
	_, err := Use(e, UseOptions{Home: "home1", Source: "api1"})
	if err == nil {
		t.Fatal("应失败")
	}
	if tu.Read(source.AuthFile(p, "plus1")) != authBefore {
		t.Fatal("source 登录没恢复")
	}
	if homeSource(t, p, "home1") != "plus1" {
		t.Fatal("map 不该变")
	}
}
