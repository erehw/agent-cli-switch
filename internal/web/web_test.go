package web

import (
	"bytes"
	"encoding/json"
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

func newServer(t *testing.T) (*Server, config.Paths) {
	p := tu.Fixture(t)
	app := &core.App{Paths: p, Cred: cred.NewMemory(), Proc: &proc.Fake{}, Stdout: io.Discard, Stderr: io.Discard}
	return &Server{App: app, Token: "tok123"}, p
}

func do(h http.Handler, method, url, host, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Host = host
	if token != "" {
		req.Header.Set("X-ACSW-Token", token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestIndexHasToken(t *testing.T) {
	s, _ := newServer(t)
	w := do(s.Handler(), "GET", "/", "127.0.0.1:7799", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"tok123"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestHostGuard(t *testing.T) {
	s, _ := newServer(t)
	if w := do(s.Handler(), "GET", "/api/status", "evil.example:7799", "", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := do(s.Handler(), "GET", "/api/status", "localhost:7799", "", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestStatusAndUse(t *testing.T) {
	s, p := newServer(t)
	h := s.Handler()
	w := do(h, "GET", "/api/status", "127.0.0.1:7799", "", "")
	var st struct {
		Homes   []map[string]any `json:"homes"`
		Sources []map[string]any `json:"sources"`
	}
	tu.Must(t, json.Unmarshal(w.Body.Bytes(), &st))
	if len(st.Homes) != 2 || len(st.Sources) != 4 {
		t.Fatal(w.Body.String())
	}
	body := `{"home":"home1","source":"plus1"}`
	if w := do(h, "POST", "/api/use", "127.0.0.1:7799", "", body); w.Code != 403 {
		t.Fatal("无 token 应拒绝", w.Code)
	}
	if w := do(h, "POST", "/api/use", "127.0.0.1:7799", "wrong", body); w.Code != 403 {
		t.Fatal("错 token 应拒绝", w.Code)
	}
	// dry-run 不改
	w = do(h, "POST", "/api/use", "127.0.0.1:7799", "tok123", `{"home":"home1","source":"plus1","dry_run":true}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"dry_run":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	m, _ := config.LoadMap(p)
	if m.Homes["home1"].Source != "" {
		t.Fatal("dry-run 改了 map")
	}
	w = do(h, "POST", "/api/use", "127.0.0.1:7799", "tok123", body)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	m, _ = config.LoadMap(p)
	if m.Homes["home1"].Source != "plus1" {
		t.Fatal("没切换")
	}
	// 同 openai source 第二个 home → 409
	w = do(h, "POST", "/api/use", "127.0.0.1:7799", "tok123", `{"home":"home2","source":"plus1"}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "正被 home home1 使用") {
		t.Fatal(w.Code, w.Body.String())
	}
	// 非 JSON
	req := httptest.NewRequest("POST", "/api/use", bytes.NewReader([]byte("home=home1")))
	req.Host = "127.0.0.1"
	req.Header.Set("X-ACSW-Token", "tok123")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != 415 {
		t.Fatal(rw.Code)
	}
}
