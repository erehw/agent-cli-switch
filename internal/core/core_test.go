package core

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"acsw/internal/config"
	"acsw/internal/cred"
	"acsw/internal/home"
	"acsw/internal/proc"
	tu "acsw/internal/testutil"
)

// fakeCodex 写一个假的 codex 脚本：把环境、参数、运行登记目录写进 out 文件，按 FAKE_EXIT 退出。
func fakeCodex(t *testing.T, out string) string {
	if runtime.GOOS == "windows" {
		t.Skip("假 codex 脚本只在类 Unix 上跑")
	}
	p := filepath.Join(t.TempDir(), "codex")
	script := `#!/bin/sh
{
  echo "ARGS=$*"
  echo "PWD=$(pwd)"
  env | grep -E '^(CODEX_HOME|ACSW_API_KEY|OPENAI_API_KEY|CODEX_API_KEY|CODEX_SQLITE_HOME|KEEP_ME)=' | sort
  echo "RUNREG=$(ls "$CODEX_HOME/.acsw-run" 2>/dev/null | wc -l)"
} > "` + out + `"
if [ "$1" = "login" ]; then
  printf '%s' '{"tokens":{"refresh_token":"fake-r","account_id":"acct-login"}}' > "$CODEX_HOME/auth.json"
fi
exit ${FAKE_EXIT:-0}
`
	tu.Must(t, os.WriteFile(p, []byte(script), 0o755))
	return p
}

func newApp(t *testing.T, codex string) (*App, *bytes.Buffer) {
	p := tu.Fixture(t)
	var errb bytes.Buffer
	return &App{
		Paths: p, Cred: cred.NewMemory(), Proc: proc.System{}, CodexPath: codex,
		Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &errb,
		Environ: func() []string {
			return []string{"PATH=" + os.Getenv("PATH"), "OPENAI_API_KEY=parent-secret", "CODEX_API_KEY=parent2", "CODEX_HOME=/wrong", "CODEX_SQLITE_HOME=/wrong2", "KEEP_ME=1", "FAKE_EXIT=" + os.Getenv("FAKE_EXIT")}
		},
	}, &errb
}

func TestRunCustomInjectsKeyOnly(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.txt")
	a, errb := newApp(t, fakeCodex(t, out))
	tu.Must(t, a.Cred.Set(cred.Target("api1"), "sk-test-not-real"))
	proj := t.TempDir()
	_, err := a.MapProject(proj, "home1")
	tu.Must(t, err)
	_, err = a.Use(home.UseOptions{Home: "home1", Source: "api1"})
	tu.Must(t, err)

	code, err := a.Run(RunOptions{Dir: proj, Args: []string{"resume", "abc"}})
	tu.Must(t, err)
	if code != 0 {
		t.Fatal(code)
	}
	got := tu.Read(out)
	for _, want := range []string{"ARGS=resume abc", "ACSW_API_KEY=sk-test-not-real", "CODEX_HOME=" + a.Paths.HomeDir("home1"), "KEEP_ME=1", "RUNREG=1", "PWD=" + proj} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少 %q：\n%s", want, got)
		}
	}
	for _, bad := range []string{"OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_SQLITE_HOME", "/wrong"} {
		if strings.Contains(got, bad) {
			t.Errorf("不该出现 %q：\n%s", bad, got)
		}
	}
	if strings.Contains(errb.String(), "sk-test") {
		t.Fatal("输出里泄露了 key")
	}
	if !strings.Contains(errb.String(), "home=home1  source=api1 (custom)") {
		t.Fatal(errb.String())
	}
	if ents, _ := os.ReadDir(proc.RunDir(a.Paths.HomeDir("home1"))); len(ents) != 0 {
		t.Fatal("退出后登记没清理")
	}
}

func TestRunOpenAINoKeyAndExitCode(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.txt")
	a, _ := newApp(t, fakeCodex(t, out))
	_, err := a.Use(home.UseOptions{Home: "home2", Source: "plus1"})
	tu.Must(t, err)
	t.Setenv("FAKE_EXIT", "3")
	code, err := a.Run(RunOptions{Dir: t.TempDir(), Home: "home2"})
	tu.Must(t, err)
	if code != 3 {
		t.Fatal("退出码没传出", code)
	}
	got := tu.Read(out)
	if strings.Contains(got, "ACSW_API_KEY") || strings.Contains(got, "OPENAI_API_KEY") {
		t.Fatal(got)
	}
}

func TestRunErrors(t *testing.T) {
	a, _ := newApp(t, "/nonexistent/codex")
	if _, err := a.Run(RunOptions{Dir: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "没有映射") || !IsUsage(err) {
		t.Fatal(err)
	}
	if _, err := a.Run(RunOptions{Dir: t.TempDir(), Home: "home1"}); err == nil || !strings.Contains(err.Error(), "还没指定 source") || !IsUsage(err) {
		t.Fatal(err)
	}
	_, err := a.Use(home.UseOptions{Home: "home1", Source: "api1"})
	tu.Must(t, err)
	if _, err := a.Run(RunOptions{Dir: t.TempDir(), Home: "home1"}); err == nil || !strings.Contains(err.Error(), "读不到 source api1 的 key") {
		t.Fatal(err)
	}
}

func TestExpectHome(t *testing.T) {
	a, _ := newApp(t, "/nonexistent/codex")
	proj := t.TempDir()
	other := t.TempDir()
	_, err := a.MapProject(proj, "home2")
	tu.Must(t, err)
	_, err = a.Use(home.UseOptions{Home: "home2", Source: "plus1"})
	tu.Must(t, err)

	// 匹配：过了映射/source 校验后才会尝试启动（这里 CodexPath 无效，应得到非 Usage 错误）
	if _, err := a.Run(RunOptions{Dir: proj, ExpectHome: "home2"}); err == nil || IsUsage(err) {
		t.Fatal("匹配时应继续到启动阶段", err)
	}
	// 未映射
	if _, err := a.Run(RunOptions{Dir: other, ExpectHome: "home2"}); err == nil || !IsUsage(err) || !strings.Contains(err.Error(), "没有映射") {
		t.Fatal(err)
	}
	// 映射到别的 home
	_, err = a.MapProject(other, "home1")
	tu.Must(t, err)
	if _, err := a.Run(RunOptions{Dir: other, ExpectHome: "home2"}); err == nil || !IsUsage(err) || !strings.Contains(err.Error(), "属于 home1") {
		t.Fatal(err)
	}
	// --home 不能绕过 --expect-home
	if _, err := a.Run(RunOptions{Dir: other, Home: "home2", ExpectHome: "home2"}); err == nil || !IsUsage(err) || !strings.Contains(err.Error(), "属于 home1") {
		t.Fatal(err)
	}
	if _, err := a.Run(RunOptions{Dir: proj, Home: "home1", ExpectHome: "home2"}); err == nil || !IsUsage(err) || !strings.Contains(err.Error(), "冲突") {
		t.Fatal(err)
	}
}

func TestWriteHomeCmd(t *testing.T) {
	a, _ := newApp(t, "")
	exe := filepath.Join(t.TempDir(), "acsw.exe")
	tu.Must(t, os.WriteFile(exe, []byte("x"), 0o755))
	path, existed, err := a.WriteHomeCmd("home2", exe)
	tu.Must(t, err)
	if existed {
		t.Fatal("首次不应已存在")
	}
	got := tu.Read(path)
	abs, _ := filepath.Abs(exe)
	want := HomeCmdContent(abs, "home2")
	if got != want {
		t.Fatalf("内容不对:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(got, "codex --expect-home home2 %*") {
		t.Fatal(got)
	}
	if !strings.Contains(got, "@exit /b %ERRORLEVEL%") {
		t.Fatal(got)
	}
	if strings.HasPrefix(got, "\ufeff") {
		t.Fatal("不应有 BOM")
	}
	path2, existed, err := a.WriteHomeCmd("home2", exe)
	tu.Must(t, err)
	if !existed || path2 != path {
		t.Fatal(existed, path2)
	}
	if _, _, err := a.WriteHomeCmd("nope", exe); err == nil {
		t.Fatal("不存在的 home 应报错")
	}
}

func TestSourceAdd(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.txt")
	a, _ := newApp(t, fakeCodex(t, out))
	// openai via codex login（假 codex 写 auth.json）
	tu.Must(t, a.AddOpenAISource("plus9", ""))
	if !strings.Contains(tu.Read(out), "ARGS=login") || !strings.Contains(tu.Read(out), "CODEX_HOME="+a.Paths.SourceDir("plus9")) {
		t.Fatal(tu.Read(out))
	}
	// openai --from
	from := t.TempDir()
	tu.Must(t, os.WriteFile(filepath.Join(from, "auth.json"), tu.FakeAuth("a", "b"), 0o600))
	tu.Must(t, a.AddOpenAISource("pro9", from))
	if err := a.AddOpenAISource("pro9", from); err == nil {
		t.Fatal("重复应报错")
	}
	bad := t.TempDir()
	os.WriteFile(filepath.Join(bad, "auth.json"), []byte(`{"OPENAI_API_KEY":"x"}`), 0o600)
	if err := a.AddOpenAISource("bad9", bad); err == nil {
		t.Fatal("非 ChatGPT 登录应拒绝")
	}
	// custom
	tu.Must(t, a.AddCustomSource("api9", "https://api9.example/v1/", "m1", " sk-x "))
	if k, _ := a.Cred.Get("acsw/codex/api9"); k != "sk-x" {
		t.Fatal(k)
	}
	if err := a.AddCustomSource("api8", "ftp://x", "m", "k"); err == nil {
		t.Fatal("坏 URL 应拒绝")
	}
	if err := a.AddCustomSource("api7", "https://x", "m", ""); err == nil {
		t.Fatal("空 key 应拒绝")
	}
	list, err := a.Sources()
	tu.Must(t, err)
	names := []string{}
	for _, s := range list {
		names = append(names, s.Name)
		if s.Name == "api9" && (!s.HasLogin || s.Meta.BaseURL != "https://api9.example/v1") {
			t.Fatalf("%+v", s)
		}
		if s.Name == "api1" && s.HasLogin {
			t.Fatal("api1 没存 key")
		}
	}
	if !strings.Contains(strings.Join(names, ","), "api9") || !strings.Contains(strings.Join(names, ","), "plus9") {
		t.Fatal(names)
	}
	// source 目录里不该有 key
	filepath.Walk(a.Paths.Root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.Contains(tu.Read(p), "sk-x") {
			t.Errorf("key 落到了文件 %s", p)
		}
		return nil
	})
}

func TestLast(t *testing.T) {
	a, _ := newApp(t, "/nonexistent")
	proj := t.TempDir()
	_, err := a.MapProject(proj, "home1")
	tu.Must(t, err)
	tu.MakeDB(t, filepath.Join(a.Paths.HomeDir("home1"), "state_5.sqlite"), [][]any{
		{"old", "openai", proj, 100, "旧"}, {"new", "openai", filepath.Join(proj, "sub"), 200, "新"}, {"other", "openai", "/elsewhere", 300, "别处"},
	})
	r, err := a.Last(filepath.Join(proj), "", 5)
	tu.Must(t, err)
	if r.Home != "home1" || len(r.Threads) != 2 || r.Threads[0].ID != "new" {
		t.Fatalf("%+v", r)
	}
	if _, err := a.Last(t.TempDir(), "", 5); err == nil {
		t.Fatal("没映射应报错")
	}
}

func TestMapProject(t *testing.T) {
	a, _ := newApp(t, "")
	if _, err := a.MapProject(t.TempDir(), "nohome"); err == nil {
		t.Fatal("home 不存在应报错")
	}
	d := t.TempDir()
	abs, err := a.MapProject(d, "home2")
	tu.Must(t, err)
	m, _ := config.LoadMap(a.Paths)
	if pr, ok := m.MatchHome(filepath.Join(abs, "x"), a.CI); !ok || pr.Home != "home2" {
		t.Fatal(pr)
	}
}
