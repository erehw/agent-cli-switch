package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"acsw/internal/config"
	"acsw/internal/core"
	"acsw/internal/cred"
	"acsw/internal/proc"
	tu "acsw/internal/testutil"
)

const cliKey = "sk-cli-SECRET-456"

func modelsApp(t *testing.T, baseURL string) *core.App {
	p := tu.Fixture(t)
	tu.Must(t, config.SaveSource(p, "api1", &config.SourceMeta{Type: "custom", BaseURL: baseURL, Model: "m"}))
	c := cred.NewMemory()
	tu.Must(t, c.Set(cred.Target("api1"), cliKey))
	return &core.App{Paths: p, Cred: c, Proc: &proc.Fake{}, Stdout: io.Discard, Stderr: io.Discard}
}

func TestSourceModelsCLI(t *testing.T) {
	raw := []byte("{\"data\":[{\"id\":\"a\"},{\"id\":\"b\"}],\"object\":\"list\"}\r\n  ")
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(500)
			_, _ = w.Write([]byte("boom " + auth))
			return
		}
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	// 成功：stdout 逐字节等于上游响应，stderr 为空；base_url 结尾的 / 也能处理
	a := modelsApp(t, srv.URL+"/v1/")
	var out, errb bytes.Buffer
	if code := sourceModels(a, "api1", &out, &errb); code != 0 || !bytes.Equal(out.Bytes(), raw) || errb.Len() != 0 {
		t.Fatal(code, out.String(), errb.String())
	}
	if auth != "Bearer "+cliKey || path != "/v1/models" {
		t.Fatal("请求头或路径不对", path)
	}

	// 非 2xx：非零退出，stderr 有状态码，stdout 为空，任何输出都没有 key
	a = modelsApp(t, srv.URL+"/v2")
	out.Reset()
	errb.Reset()
	if code := sourceModels(a, "api1", &out, &errb); code == 0 || out.Len() != 0 || !strings.Contains(errb.String(), "500") {
		t.Fatal(code, out.String(), errb.String())
	}
	if strings.Contains(out.String()+errb.String(), cliKey) {
		t.Fatal("输出里出现了 key")
	}

	// openai、不存在：拒绝，非零退出，不发请求
	path = ""
	for _, n := range []string{"plus1", "nope"} {
		out.Reset()
		errb.Reset()
		if code := sourceModels(a, n, &out, &errb); code == 0 || out.Len() != 0 || errb.Len() == 0 || path != "" {
			t.Fatal(n, code, out.String(), errb.String())
		}
	}
	if !strings.Contains(errb.String(), "不存在") {
		t.Fatal(errb.String())
	}
}
