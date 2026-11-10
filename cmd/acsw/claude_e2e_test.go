package main

// 端到端测试（Q40）：用真的 claude，配本地假服务和假 key，验证三种用法都只把对的 key 发到对的端口，
// 并且沙盒里用户级 settings.json 的假 token 不会漏出去。找不到 claude（或 -short、ACSW_SKIP_CLAUDE_E2E=1）时跳过。
//
// 安全：ACSW_ROOT 和 CLAUDE_CONFIG_DIR 都指向临时目录；key 全是假的；凭据目标名带随机后缀（acsw/claude/acswe2e…），
// 测试结束时删除；假服务只听 127.0.0.1，不会连真实模型。

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"acsw/internal/cred"
)

type e2eReq struct {
	Port   int
	Path   string
	XKey   string
	Auth   string
	Model  string
	Body   string
	Server string
}

type fakeAnthropic struct {
	mu   sync.Mutex
	reqs []e2eReq
}

func (f *fakeAnthropic) start(t *testing.T, label string, status int) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		_ = json.Unmarshal(b, &body)
		f.mu.Lock()
		f.reqs = append(f.reqs, e2eReq{Port: port, Path: r.URL.Path, XKey: r.Header.Get("x-api-key"), Auth: r.Header.Get("Authorization"), Model: body.Model, Body: string(b), Server: label})
		f.mu.Unlock()
		if status != 200 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"fake 401"}}`)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/v1/messages") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"input_tokens":1,"data":[]}`)
			return
		}
		text := "E2E-OK-" + label
		if !body.Stream {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"id":"msg_e2e","type":"message","role":"assistant","model":%q,"content":[{"type":"text","text":%q}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`, body.Model, text)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		ev := func(name, data string) { fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data) }
		ev("message_start", fmt.Sprintf(`{"type":"message_start","message":{"id":"msg_e2e","type":"message","role":"assistant","model":%q,"content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`, body.Model))
		ev("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		ev("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%q}}`, text))
		ev("content_block_stop", `{"type":"content_block_stop","index":0}`)
		ev("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}`)
		ev("message_stop", `{"type":"message_stop"}`)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return port
}

func (f *fakeAnthropic) take() []e2eReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.reqs
	f.reqs = nil
	return r
}

func randHex(t *testing.T) string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// sandboxEnv 去掉父进程里所有 ANTHROPIC_* 和 CLAUDE_*，再加上沙盒变量。
func sandboxEnv(extra ...string) []string {
	var out []string
	for _, kv := range os.Environ() {
		n, _, _ := strings.Cut(kv, "=")
		u := strings.ToUpper(n)
		if strings.HasPrefix(u, "ANTHROPIC_") || strings.HasPrefix(u, "CLAUDE_") || u == "ACSW_ROOT" || u == "ACSW_CLAUDE" {
			continue
		}
		out = append(out, kv)
	}
	return append(out, extra...)
}

type runResult struct {
	Code   int
	Stdout string
	Stderr string
}

func run(t *testing.T, env []string, dir, stdin string, name string, args ...string) runResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env, cmd.Dir = env, dir
	cmd.Stdin = strings.NewReader(stdin)
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("运行 %s 失败：%v", name, err)
	}
	return runResult{code, o.String(), e.String()}
}

func TestClaudeE2E(t *testing.T) {
	if testing.Short() || os.Getenv("ACSW_SKIP_CLAUDE_E2E") == "1" {
		t.Skip("跳过 claude 端到端测试")
	}
	claudeExe, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("PATH 里没有 claude，跳过端到端测试")
	}

	base := t.TempDir()
	exe := filepath.Join(base, "bin dir", "acsw")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	gobin, err := exec.LookPath(filepath.Join(runtime.GOROOT(), "bin", "go"))
	if err != nil {
		if gobin, err = exec.LookPath("go"); err != nil {
			t.Skip("找不到 go 命令，无法编译 acsw，跳过端到端测试")
		}
	}
	if out, err := exec.Command(gobin, "build", "-o", exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("编译 acsw 失败：%v\n%s", err, out)
	}
	root := filepath.Join(base, "agent-source", "codex")
	cfg := filepath.Join(base, "claude-config")
	proj := filepath.Join(base, "proj")
	for _, d := range []string{root, cfg, proj} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	fake := &fakeAnthropic{}
	portA := fake.start(t, "A", 200)
	portB := fake.start(t, "B", 200)
	portU := fake.start(t, "USER", 200)
	portX := fake.start(t, "X401", 401)
	const userToken = "dummy-user-token-e2e"
	userSettings := fmt.Sprintf(`{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:%d","ANTHROPIC_AUTH_TOKEN":%q,"ANTHROPIC_MODEL":"user-model"}}`, portU, userToken)
	if err := os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(userSettings), 0o600); err != nil {
		t.Fatal(err)
	}

	env := sandboxEnv("ACSW_ROOT="+root, "CLAUDE_CONFIG_DIR="+cfg, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "CLAUDE_CODE_MAX_RETRIES=1",
		"DISABLE_AUTOUPDATER=1", "NO_PROXY=127.0.0.1,localhost", "no_proxy=127.0.0.1,localhost")

	suffix := randHex(t)
	type src struct {
		name, key, model string
		port             int
	}
	srcs := map[string]src{
		"A": {"acswe2e" + suffix + "a", "dummy-e2e-key-a-" + suffix, "model-a", portA},
		"B": {"acswe2e" + suffix + "b", "dummy-e2e-key-b-" + suffix, "model-b", portB},
		"X": {"acswe2e" + suffix + "x", "dummy-e2e-key-x-" + suffix, "model-x", portX},
	}
	store := cred.Default(root)
	for _, s := range srcs {
		s := s
		t.Cleanup(func() {
			_ = store.Delete(cred.TargetFor("claude", s.name))
			if _, err := store.Get(cred.TargetFor("claude", s.name)); err == nil {
				t.Errorf("测试凭据 %s 没删掉", cred.TargetFor("claude", s.name))
			}
		})
		r := run(t, env, proj, s.key+"\n", exe, "source", "add", s.name, "--tool", "claude", "--base-url", "http://127.0.0.1:"+strconv.Itoa(s.port), "--model", s.model, "--haiku", s.model, "--key-stdin")
		if r.Code != 0 {
			t.Fatalf("source add %s：%d %s", s.name, r.Code, r.Stderr)
		}
	}
	allKeys := []string{srcs["A"].key, srcs["B"].key, srcs["X"].key, userToken}

	authKinds := func(reqs []e2eReq) string {
		var x, a int
		for _, q := range reqs {
			if q.XKey != "" {
				x++
			}
			if q.Auth != "" {
				a++
			}
		}
		return fmt.Sprintf("x-api-key %d/%d，Authorization %d/%d", x, len(reqs), a, len(reqs))
	}
	// check：这一轮请求全部打到 want 的端口，只带 want 的 key，没有用户 token；至少一条用的是 source 的主模型。
	check := func(label string, want src, r runResult) {
		t.Helper()
		reqs := fake.take()
		if len(reqs) == 0 {
			t.Fatalf("%s：假服务没收到请求；退出码 %d\nstdout:%s\nstderr:%s", label, r.Code, r.Stdout, r.Stderr)
		}
		sawModel := false
		for _, q := range reqs {
			if q.Port != want.port {
				t.Errorf("%s：请求打到了 %s（端口 %d），应只打到 %d", label, q.Server, q.Port, want.port)
			}
			for _, k := range allKeys {
				if k != want.key && (strings.Contains(q.XKey, k) || strings.Contains(q.Auth, k)) {
					t.Errorf("%s：请求里出现了不该有的 key（%s 的）", label, map[bool]string{true: "用户 token", false: "别的 source"}[k == userToken])
				}
			}
			if q.XKey != want.key && q.Auth != "Bearer "+want.key {
				t.Errorf("%s：请求没带对的 key（x-api-key 有值：%v，Authorization 有值：%v）", label, q.XKey != "", q.Auth != "")
			}
			// 只能有这一个 key：带了的认证头都必须是它
			if (q.XKey != "" && q.XKey != want.key) || (q.Auth != "" && q.Auth != "Bearer "+want.key) {
				t.Errorf("%s：认证头里有别的值（x-api-key 对：%v，Authorization 对：%v）", label, q.XKey == want.key, q.Auth == "Bearer "+want.key)
			}
			if q.Model == want.model {
				sawModel = true
			}
			if q.Model == "user-model" {
				t.Errorf("%s：用了用户 settings 里的模型", label)
			}
		}
		if !sawModel {
			t.Errorf("%s：没有请求用 %s", label, want.model)
		}
		t.Logf("%s：%d 个请求，全部到端口 %d，key 正确（%s）", label, len(reqs), want.port, authKinds(reqs))
	}

	A, B, X := srcs["A"], srcs["B"], srcs["X"]

	// 用法一：导出明文 settings，直接 claude --settings
	r := run(t, env, proj, "", exe, "claude", "export", A.name, "--key-mode", "plain")
	plainFile := strings.TrimSpace(r.Stdout)
	if r.Code != 0 || !strings.HasSuffix(plainFile, A.name+".plain.json") || !strings.Contains(r.Stderr, "明文") {
		t.Fatalf("export plain：%d %q %s", r.Code, r.Stdout, r.Stderr)
	}
	if want := filepath.Join(base, "agent-source", "claude", "settings"); filepath.Dir(plainFile) != want {
		t.Fatalf("settings 目录应为 %s，实际 %s", want, filepath.Dir(plainFile))
	}
	r = run(t, env, proj, "", claudeExe, "-p", "say hi", "--settings", plainFile)
	if r.Code != 0 || !strings.Contains(r.Stdout, "E2E-OK-A") {
		t.Fatalf("plain 直接运行：%d\n%s\n%s", r.Code, r.Stdout, r.Stderr)
	}
	check("用法一 plain 文件", A, r)

	// 用法三：导出 helper settings（不含 key），直接 claude --settings；apiKeyHelper 调用临时编译的 acsw（路径带空格）
	r = run(t, env, proj, "", exe, "claude", "export", A.name)
	helperFile := strings.TrimSpace(r.Stdout)
	hb, _ := os.ReadFile(helperFile)
	if r.Code != 0 || bytes.Contains(hb, []byte(A.key)) || !bytes.Contains(hb, []byte(`"apiKeyHelper": "\"`)) {
		t.Fatalf("export helper：%d %s\n%s", r.Code, r.Stderr, hb)
	}
	r = run(t, env, proj, "", claudeExe, "-p", "say hi", "--settings", helperFile)
	if r.Code != 0 || !strings.Contains(r.Stdout, "E2E-OK-A") {
		t.Fatalf("helper 直接运行：%d\n%s\n%s", r.Code, r.Stdout, r.Stderr)
	}
	check("用法三 helper 文件", A, r)

	// 用法二：acsw claude 包装（默认 helper），另一个 source、另一个端口
	r = run(t, env, proj, "", exe, "claude", B.name, "-p", "say hi")
	if r.Code != 0 || !strings.Contains(r.Stdout, "E2E-OK-B") {
		t.Fatalf("包装 helper：%d\n%s\n%s", r.Code, r.Stdout, r.Stderr)
	}
	check("用法二 包装 helper", B, r)

	r = run(t, env, proj, "", exe, "claude", B.name, "--key-mode", "plain", "-p", "say hi")
	if r.Code != 0 || !strings.Contains(r.Stdout, "E2E-OK-B") {
		t.Fatalf("包装 plain：%d\n%s\n%s", r.Code, r.Stdout, r.Stderr)
	}
	check("用法二 包装 plain", B, r)

	// stdin 透传：提示词从标准输入给
	marker := "e2e-stdin-marker-" + suffix
	r = run(t, env, proj, marker, exe, "claude", A.name, "-p")
	reqs := fake.take()
	found := false
	for _, q := range reqs {
		found = found || strings.Contains(q.Body, marker)
	}
	if r.Code != 0 || !found {
		t.Fatalf("stdin 应透传给 claude：%d found=%v\n%s", r.Code, found, r.Stderr)
	}

	// 并发：A、B 同时跑，各打各的端口
	var wg sync.WaitGroup
	res := make([]runResult, 2)
	for i, s := range []src{A, B} {
		wg.Add(1)
		go func(i int, s src) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "claude", s.name, "-p", "say hi")
			cmd.Env, cmd.Dir = env, proj
			var o, e bytes.Buffer
			cmd.Stdout, cmd.Stderr = &o, &e
			err := cmd.Run()
			code := 0
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			}
			res[i] = runResult{code, o.String(), e.String()}
		}(i, s)
	}
	wg.Wait()
	reqs = fake.take()
	for i, s := range []src{A, B} {
		if res[i].Code != 0 || !strings.Contains(res[i].Stdout, "E2E-OK-"+map[int]string{0: "A", 1: "B"}[i]) {
			t.Fatalf("并发 %s：%d\n%s\n%s", s.name, res[i].Code, res[i].Stdout, res[i].Stderr)
		}
	}
	for _, q := range reqs {
		want := map[int]src{portA: A, portB: B}[q.Port]
		if want.key == "" || (q.XKey != want.key && q.Auth != "Bearer "+want.key) || (q.XKey != "" && q.XKey != want.key) || (q.Auth != "" && q.Auth != "Bearer "+want.key) {
			t.Fatalf("并发时请求和 key 对不上（端口 %d）", q.Port)
		}
	}

	// 退出码透传：上游 401 时 claude 非 0 退出，acsw 原样返回
	r = run(t, env, proj, "", exe, "claude", "export", X.name)
	xFile := strings.TrimSpace(r.Stdout)
	direct := run(t, env, proj, "", claudeExe, "-p", "say hi", "--settings", xFile)
	wrapped := run(t, env, proj, "", exe, "claude", X.name, "-p", "say hi")
	fake.take()
	if direct.Code == 0 || wrapped.Code != direct.Code {
		t.Fatalf("退出码应透传：直接 %d，包装 %d", direct.Code, wrapped.Code)
	}

	// acsw 自己的错误：最后一行是 JSON，退出码 9999xxxx（非 Windows 为 2/1）
	lastJSON := func(s string) map[string]any {
		lines := strings.Split(strings.TrimSpace(s), "\n")
		var m map[string]any
		_ = json.Unmarshal([]byte(lines[len(lines)-1]), &m)
		return m
	}
	for _, c := range []struct {
		args []string
		kind string
	}{
		{[]string{"claude", "nosuch" + suffix, "-p", "x"}, "source_invalid"},
		{[]string{"claude", A.name, "--settings", "x.json"}, "bad_args"},
		{[]string{"claude", A.name, "--key-mode", "env"}, "bad_args"},
	} {
		r := run(t, env, proj, "", exe, c.args...)
		if m := lastJSON(r.Stderr); m["kind"] != c.kind || r.Code == 0 {
			t.Errorf("%v：应为 %s，实际 %d %v", c.args, c.kind, r.Code, m)
		}
	}
	if reqs := fake.take(); len(reqs) != 0 {
		t.Fatalf("出错时不应发请求，收到 %d 个", len(reqs))
	}

	// acsw claude key：只输出 key
	r = run(t, env, proj, "", exe, "claude", "key", A.name)
	if r.Code != 0 || strings.TrimSpace(r.Stdout) != A.key {
		t.Fatal("acsw claude key 输出不对")
	}

	// 沙盒里的用户 settings 没被改
	if b, _ := os.ReadFile(filepath.Join(cfg, "settings.json")); string(b) != userSettings {
		t.Fatal("用户级 settings.json 被改了")
	}

	// 删除：凭据、settings 文件都删掉
	for _, s := range []src{A, B, X} {
		r := run(t, env, proj, "", exe, "source", "rm", s.name, "--yes")
		if r.Code != 0 {
			t.Fatalf("source rm %s：%s", s.name, r.Stderr)
		}
		if _, err := store.Get(cred.TargetFor("claude", s.name)); err == nil {
			t.Fatalf("%s 的凭据应已删除", s.name)
		}
	}
	if _, err := os.Stat(plainFile); err == nil {
		t.Fatal("明文文件应已删除")
	}
}
