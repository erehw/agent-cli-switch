package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"acsw/internal/config"
	"acsw/internal/cred"
	"acsw/internal/exitcode"
	"acsw/internal/home"
	"acsw/internal/proc"
	tu "acsw/internal/testutil"
)

// 测试用“假 codex”：把测试二进制自身当 codex 启动，记录它看到的环境后按 FAKE_EXIT 退出。
const helperEnv = "ACSW_TEST_FAKE_CODEX_OUT"

func TestMain(m *testing.M) {
	if out := os.Getenv(helperEnv); out != "" {
		_, hasKey := os.LookupEnv(config.KeyEnv)
		in, _ := io.ReadAll(os.Stdin) // 读到 EOF：stdin 没关的话这里会卡住，测试会超时
		sum := sha256.Sum256(in)
		var envNames []string // 子进程环境里与 Claude 登录有关的变量名（给 acsw claude 的测试用）
		for _, kv := range os.Environ() {
			n, _, _ := strings.Cut(kv, "=")
			if u := strings.ToUpper(n); strings.HasPrefix(u, "ANTHROPIC_") || strings.HasPrefix(u, "CLAUDE_") {
				envNames = append(envNames, u)
			}
		}
		sort.Strings(envNames)
		b, _ := json.Marshal(map[string]any{"codex_home": os.Getenv("CODEX_HOME"), "has_key": hasKey, "args": os.Args[1:], "stdin_len": len(in), "stdin_sha": hex.EncodeToString(sum[:]), "env_names": envNames})
		_ = os.WriteFile(out, b, 0o600)
		os.Exit(7)
	}
	os.Exit(m.Run())
}

// snapshot 记录目录下所有文件的相对路径、大小和内容哈希。
func snapshot(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			lines = append(lines, "D "+rel)
			return nil
		}
		b, _ := os.ReadFile(p)
		h := sha256.Sum256(b)
		lines = append(lines, "F "+rel+" "+hex.EncodeToString(h[:]))
		return nil
	})
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func aliasFixture(t *testing.T) (*App, string, string) {
	p := tu.Fixture(t)
	official := filepath.Join(t.TempDir(), "fake-official-codex")
	tu.Must(t, os.MkdirAll(filepath.Join(official, "sessions"), 0o700))
	tu.Must(t, os.WriteFile(filepath.Join(official, "auth.json"), tu.FakeAuth("acct-official", "official"), 0o600))
	tu.Must(t, os.WriteFile(filepath.Join(official, "config.toml"), []byte("model = \"gpt-x\"\n"), 0o600))
	prj := filepath.Join(p.Root, "prj1")
	tu.Must(t, os.MkdirAll(prj, 0o755))
	tu.MakeDB(t, filepath.Join(official, "state_5.sqlite"), [][]any{
		{"t-old", "openai", prj, time.Now().Add(-time.Hour).Unix(), "旧会话"},
		{"t-new", "openai", prj, time.Now().Unix(), "新会话"},
		{"t-other", "openai", `X:\elsewhere`, time.Now().Unix(), "别的项目"},
	})
	a := &App{Paths: p, Cred: cred.NewMemory(), Proc: &proc.Fake{AlivePIDs: map[int]bool{}}, Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard, CI: config.CaseInsensitiveFS()}
	return a, official, prj
}

func TestHomeAliasRunLastStatus(t *testing.T) {
	a, official, prj := aliasFixture(t)
	tu.Must(t, a.Cred.Set(cred.Target("api1"), "sk-alias-SECRET"))
	abs, err := a.SetHomeAlias("home1", official)
	tu.Must(t, err)
	_, err = a.MapProject(prj, "home1")
	tu.Must(t, err)
	before := snapshot(t, official)

	// A：codex 用官方目录启动，不注入 key；退出码透传
	out := filepath.Join(t.TempDir(), "seen.json")
	self, _ := os.Executable()
	a.CodexPath = self
	a.Environ = func() []string { return append(os.Environ(), helperEnv+"="+out, config.KeyEnv+"=leak") }
	code, err := a.Run(RunOptions{Dir: prj, ExpectHome: "home1", Args: []string{"exec", "-"}})
	tu.Must(t, err)
	if code != 7 {
		t.Fatal("退出码应透传", code)
	}
	var seen struct {
		CodexHome string   `json:"codex_home"`
		HasKey    bool     `json:"has_key"`
		Args      []string `json:"args"`
	}
	b, err := os.ReadFile(out)
	tu.Must(t, err)
	tu.Must(t, json.Unmarshal(b, &seen))
	if seen.CodexHome != abs || seen.HasKey || strings.Join(seen.Args, " ") != "exec -" {
		t.Fatal(string(b))
	}
	// 运行登记在 acsw 根目录下，不在官方目录
	if !config.Exists(proc.RunDir(a.Paths.HomeDir("home1"))) {
		t.Fatal("运行登记应在 acsw 的 home 目录")
	}

	// B：last 只读官方状态库
	lr, err := a.Last(prj, "", 5)
	tu.Must(t, err)
	if lr.Home != "home1" || len(lr.Threads) != 2 || lr.Threads[0].ID != "t-new" || lr.Threads[0].From != "db" {
		t.Fatal(lr)
	}

	// 写操作一律拒绝
	_, err = a.Use(home.UseOptions{Home: "home1", Source: "api1"})
	if c, _ := exitcode.Of(err); c != exitcode.ReadonlyAlias || !strings.Contains(err.Error(), "--off") {
		t.Fatal(err)
	}
	if _, err := a.Use(home.UseOptions{Home: "home1", Source: "api1", DryRun: true}); err == nil {
		t.Fatal("dry-run 也应拒绝")
	}

	// status / --json
	rep, err := a.StatusReport()
	tu.Must(t, err)
	var h1 StatusHome
	for _, h := range rep.Homes {
		if h.Home == "home1" {
			h1 = h
		}
	}
	// dir 就是 acsw codex 设置的 CODEX_HOME
	if h1.Dir != abs || h1.Dir != seen.CodexHome {
		t.Fatalf("dir=%q codex_home=%q", h1.Dir, seen.CodexHome)
	}
	if h1.AliasOf != abs || h1.Provider != "official" || h1.Source != "" || h1.HasLogin == nil || !*h1.HasLogin || len(h1.Projects) != 1 || h1.Problem != "" {
		t.Fatalf("%+v", h1)
	}
	js, _ := json.Marshal(rep)
	if strings.Contains(string(js), "acct-official") || strings.Contains(string(js), "fake-access") || strings.Contains(string(js), "SECRET") {
		t.Fatal("status 不能带 auth.json 内容或 key")
	}

	// 官方目录一字节未变（没有 -shm/-wal、.acsw-run、config 改动）
	if after := snapshot(t, official); after != before {
		t.Fatalf("官方目录被改动了：\n前：\n%s\n后：\n%s", before, after)
	}

	// 取消别名后 home1 回到普通 home（没 source）
	tu.Must(t, a.ClearHomeAlias("home1"))
	_, err = a.Run(RunOptions{Dir: prj, ExpectHome: "home1"})
	if c, _ := exitcode.Of(err); c != exitcode.NoSource {
		t.Fatal(err)
	}
}

// acsw codex 把 stdin 原样交给 codex（codex exec ... -）：中文、特殊字符、CRLF/LF、超过命令行上限的大段内容都不变，
// 能读到 EOF，退出码透传。*os.File（真实运行时的 os.Stdin，句柄直接继承）和普通 io.Reader 两种都测。
func TestRunStdinPassthrough(t *testing.T) {
	a, official, prj := aliasFixture(t)
	_, err := a.SetHomeAlias("home1", official)
	tu.Must(t, err)
	_, err = a.MapProject(prj, "home1")
	tu.Must(t, err)
	self, _ := os.Executable()
	a.CodexPath = self

	special := "中文提示：% ^ & | < > \" ' ! %PATH% ^^ !x!\t"
	var big strings.Builder
	for i := 0; big.Len() < 200*1024; i++ {
		big.WriteString(special + "第" + strings.Repeat("x", i%50) + "行\n")
	}
	cases := map[string][]byte{
		"crlf":  []byte(strings.ReplaceAll(special+"\n第二行\n", "\n", "\r\n")),
		"lf":    []byte(special + "\n第二行，无结尾换行"),
		"big":   []byte(big.String()),
		"empty": {},
	}
	for name, payload := range cases {
		for _, asFile := range []bool{true, false} {
			if asFile {
				f := filepath.Join(t.TempDir(), "in.txt")
				tu.Must(t, os.WriteFile(f, payload, 0o600))
				fh, err := os.Open(f)
				tu.Must(t, err)
				defer fh.Close()
				a.Stdin = fh
			} else {
				a.Stdin = strings.NewReader(string(payload))
			}
			out := filepath.Join(t.TempDir(), "seen.json")
			a.Environ = func() []string { return append(os.Environ(), helperEnv+"="+out) }
			code, err := a.Run(RunOptions{Dir: prj, ExpectHome: "home1", Args: []string{"exec", "-"}})
			tu.Must(t, err)
			var seen struct {
				Args []string `json:"args"`
				Len  int      `json:"stdin_len"`
				Sha  string   `json:"stdin_sha"`
			}
			b, err := os.ReadFile(out)
			tu.Must(t, err)
			tu.Must(t, json.Unmarshal(b, &seen))
			sum := sha256.Sum256(payload)
			if code != 7 || seen.Len != len(payload) || seen.Sha != hex.EncodeToString(sum[:]) || strings.Join(seen.Args, " ") != "exec -" {
				t.Fatalf("%s asFile=%v code=%d seen=%s", name, asFile, code, b)
			}
		}
	}
}

// 普通 home：status --json 的 dir 等于 acsw codex 设置的 CODEX_HOME（<root>\home\<名字>），alias_of 总是 ""。
func TestStatusDirMatchesCodexHome(t *testing.T) {
	a, official, _ := aliasFixture(t)
	tu.Must(t, a.Cred.Set(cred.Target("api1"), "sk-dir-SECRET"))
	_, err := a.Use(home.UseOptions{Home: "home2", Source: "api1"})
	tu.Must(t, err)
	abs, err := a.SetHomeAlias("home1", official)
	tu.Must(t, err)
	prj2 := filepath.Join(a.Paths.Root, "prj2")
	tu.Must(t, os.MkdirAll(prj2, 0o755))
	_, err = a.MapProject(prj2, "home2")
	tu.Must(t, err)

	out := filepath.Join(t.TempDir(), "seen.json")
	self, _ := os.Executable()
	a.CodexPath = self
	a.Environ = func() []string { return append(os.Environ(), helperEnv+"="+out) }
	_, err = a.Run(RunOptions{Dir: prj2, ExpectHome: "home2", Args: []string{"exec", "-"}})
	tu.Must(t, err)
	var seen struct {
		CodexHome string `json:"codex_home"`
		HasKey    bool   `json:"has_key"`
	}
	b, err := os.ReadFile(out)
	tu.Must(t, err)
	tu.Must(t, json.Unmarshal(b, &seen))

	rep, err := a.StatusReport()
	tu.Must(t, err)
	dirs := map[string]StatusHome{}
	for _, h := range rep.Homes {
		dirs[h.Home] = h
	}
	if h := dirs["home2"]; h.Dir != seen.CodexHome || h.Dir != a.Paths.HomeDir("home2") || h.AliasOf != "" || !seen.HasKey {
		t.Fatalf("home2 %+v seen=%+v", h, seen)
	}
	if h := dirs["home1"]; h.Dir != abs || h.AliasOf != abs {
		t.Fatalf("home1 %+v", h)
	}
	js, _ := json.Marshal(rep)
	if strings.Count(string(js), `"alias_of":""`) != 1 || strings.Count(string(js), `"dir":`) != 2 || strings.Contains(string(js), "SECRET") {
		t.Fatal(string(js))
	}
}

func TestHomeAliasRefusals(t *testing.T) {
	a, official, _ := aliasFixture(t)
	tu.Must(t, a.Cred.Set(cred.Target("api1"), "k"))
	_, err := a.Use(home.UseOptions{Home: "home2", Source: "api1"})
	tu.Must(t, err)
	if _, err := a.SetHomeAlias("home2", official); err == nil || !IsUsage(err) {
		t.Fatal("在用 source 的 home 不能设别名", err)
	}
	if _, err := a.SetHomeAlias("home1", filepath.Join(official, "nope")); err == nil {
		t.Fatal("不存在的目录应拒绝")
	}
	if _, err := a.SetHomeAlias("home1", a.Paths.Root); err == nil {
		t.Fatal("acsw 根目录不能当官方目录")
	}
	_, err = a.SetHomeAlias("home1", official)
	tu.Must(t, err)
	tu.Must(t, a.AddHome("home3"))
	if _, err := a.SetHomeAlias("home3", official); err == nil {
		t.Fatal("同一个官方目录不能有两个别名")
	}
	if err := a.ClearHomeAlias("home2"); err == nil {
		t.Fatal("不是别名的 home 不能取消")
	}
	// 官方目录被删掉：run 报 home_not_found，status 有 problem
	tu.Must(t, os.RemoveAll(official))
	prj := t.TempDir()
	_, err = a.MapProject(prj, "home1")
	tu.Must(t, err)
	_, err = a.Run(RunOptions{Dir: prj, ExpectHome: "home1"})
	if c, _ := exitcode.Of(err); c != exitcode.HomeNotFound {
		t.Fatal(err)
	}
	st, err := a.Status()
	tu.Must(t, err)
	for _, s := range st {
		if s.Home == "home1" && !strings.Contains(s.Problem, "官方目录不存在") {
			t.Fatal(s)
		}
	}
}
