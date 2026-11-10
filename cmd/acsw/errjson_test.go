package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"acsw/internal/core"
	"acsw/internal/cred"
	"acsw/internal/exitcode"
	"acsw/internal/home"
	"acsw/internal/proc"
	tu "acsw/internal/testutil"
)

// lastJSON 校验 stderr 以 \n 结尾、最后一行是 JSON、倒数第二行是给人看的 "acsw 错误：msg"，返回解析结果。
func lastJSON(t *testing.T, stderr string) map[string]any {
	t.Helper()
	if !strings.HasSuffix(stderr, "\n") {
		t.Fatalf("stderr 应以换行结尾：%q", stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("至少两行：%q", stderr)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		t.Fatalf("最后一行不是 JSON：%q", lines[len(lines)-1])
	}
	if lines[len(lines)-2] != "acsw 错误："+m["msg"].(string) {
		t.Fatalf("msg 应与人读行一致：%q / %q", lines[len(lines)-2], m["msg"])
	}
	return m
}

func expectErr(t *testing.T, m map[string]any, c exitcode.Code, extra map[string]any) {
	t.Helper()
	if int(m["acsw_error"].(float64)) != c.Exit() || m["kind"] != c.Kind() {
		t.Fatalf("码或 kind 不对：%v（期望 %d %s）", m, c.Exit(), c.Kind())
	}
	for k, v := range extra {
		if m[k] != v {
			t.Fatalf("字段 %s = %v，期望 %v（%v）", k, m[k], v, m)
		}
	}
}

func TestErrorJSONLine(t *testing.T) {
	var buf strings.Builder
	code := failTo(&buf, core.Usage("用法：acsw x"))
	m := lastJSON(t, buf.String())
	expectErr(t, m, exitcode.BadArgs, nil)
	if code != exitcode.BadArgs.Exit() || len(m) != 3 {
		t.Fatal(code, m)
	}
	buf.Reset()
	failTo(&buf, errors.New("别的错"))
	expectErr(t, lastJSON(t, buf.String()), exitcode.Other, nil)
	// 特殊字符：仍是一行，且 HTML 字符不转义
	line := errorJSON(1, "other", "a\"b\nc<d>&", exitcode.F{"dir": `C:\x y`, "n": 5})
	if bytes.Count(line, []byte("\n")) != 1 || !bytes.Contains(line, []byte("<d>&")) {
		t.Fatalf("%q", line)
	}
	var m2 map[string]any
	if json.Unmarshal(line, &m2) != nil || m2["msg"] != "a\"b\nc<d>&" || m2["dir"] != `C:\x y` || m2["n"].(float64) != 5 {
		t.Fatalf("%q", line)
	}
	if !bytes.HasPrefix(line, []byte(`{"acsw_error":1,"kind":"other","msg":`)) {
		t.Fatalf("固定字段应在前：%q", line)
	}
	for _, c := range []exitcode.Code{exitcode.NotMapped, exitcode.ExpectMismatch, exitcode.NoSource, exitcode.BadArgs, exitcode.HomeConflict,
		exitcode.HomeNotFound, exitcode.SourceInvalid, exitcode.ReadonlyAlias, exitcode.HomeInUse, exitcode.KeyUnreadable, exitcode.CodexNotFound, exitcode.LockBusy,
		exitcode.CodexStart, exitcode.Upstream, exitcode.Other} {
		if c.Kind() == "" || (c != exitcode.Other && c.Kind() == "other") {
			t.Fatal("每个码都要有 kind", c)
		}
	}
}

func TestRunErrorsJSON(t *testing.T) {
	const key = "sk-run-SECRET-111"
	p := tu.Fixture(t)
	a := &core.App{Paths: p, Cred: cred.NewMemory(), Proc: &proc.Fake{}, CodexPath: filepath.Join(p.Root, "no-such-codex.exe"),
		Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}
	prj := filepath.Join(p.Root, "prj")
	tu.Must(t, os.MkdirAll(prj, 0o755))
	_, err := a.MapProject(prj, "home2")
	tu.Must(t, err)
	unmapped := t.TempDir()

	run := func(dir, h, expect string) map[string]any {
		_, err := a.Run(core.RunOptions{Dir: dir, Home: h, ExpectHome: expect})
		if err == nil {
			t.Fatal("应失败")
		}
		var buf strings.Builder
		failTo(&buf, err)
		if strings.Contains(buf.String(), key) {
			t.Fatal("输出里有 key")
		}
		return lastJSON(t, buf.String())
	}
	expectErr(t, run(unmapped, "", "home2"), exitcode.NotMapped, map[string]any{"dir": unmapped, "expect_home": "home2"})
	expectErr(t, run(unmapped, "", ""), exitcode.NotMapped, map[string]any{"dir": unmapped})
	expectErr(t, run(prj, "", "home1"), exitcode.ExpectMismatch, map[string]any{"home": "home2", "expect_home": "home1", "dir": prj})
	expectErr(t, run(prj, "home1", "home2"), exitcode.HomeConflict, map[string]any{"home": "home1", "expect_home": "home2"})
	expectErr(t, run(prj, "nohome", ""), exitcode.HomeNotFound, map[string]any{"home": "nohome"})
	expectErr(t, run(prj, "", "home2"), exitcode.NoSource, map[string]any{"home": "home2"})
	_, err = a.Use(home.UseOptions{Home: "home2", Source: "api1"})
	tu.Must(t, err)
	expectErr(t, run(prj, "", "home2"), exitcode.KeyUnreadable, map[string]any{"home": "home2", "source": "api1"})
	tu.Must(t, a.Cred.Set(cred.Target("api1"), key))
	expectErr(t, run(prj, "", "home2"), exitcode.CodexStart, map[string]any{"home": "home2"})
}

func TestModelsErrorJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("oops " + r.Header.Get("Authorization")))
	}))
	defer srv.Close()
	a := modelsApp(t, srv.URL+"/v1")
	var out, errb bytes.Buffer
	if code := sourceModels(a, "api1", &out, &errb); code != exitcode.Upstream.Exit() || out.Len() != 0 {
		t.Fatal(code, out.String())
	}
	if strings.Contains(errb.String(), cliKey) {
		t.Fatal("输出里有 key")
	}
	expectErr(t, lastJSON(t, errb.String()), exitcode.Upstream, map[string]any{"source": "api1", "http_status": float64(500), "url": srv.URL + "/v1/models"})
	for n, c := range map[string]exitcode.Code{"plus1": exitcode.SourceInvalid, "nope": exitcode.SourceInvalid} {
		out.Reset()
		errb.Reset()
		sourceModels(a, n, &out, &errb)
		if out.Len() != 0 {
			t.Fatal("stdout 应为空")
		}
		expectErr(t, lastJSON(t, errb.String()), c, map[string]any{"source": n})
	}
	tu.Must(t, a.Cred.Delete(cred.Target("api1")))
	errb.Reset()
	sourceModels(a, "api1", &out, &errb)
	expectErr(t, lastJSON(t, errb.String()), exitcode.KeyUnreadable, map[string]any{"source": "api1"})
}
