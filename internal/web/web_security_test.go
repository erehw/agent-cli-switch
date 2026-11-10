package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"acsw/internal/core"
)

func TestStatusNeedsToken(t *testing.T) {
	s, _ := newServer(t)
	for _, tok := range []string{"", "wrong"} {
		if w := do(s.Handler(), "GET", "/api/status", "127.0.0.1:7799", tok, ""); w.Code != 403 {
			t.Fatal(tok, w.Code)
		}
	}
}

func TestOriginCheck(t *testing.T) {
	s, _ := newServer(t)
	h := s.Handler()
	send := func(origin string) int {
		req := httptest.NewRequest("POST", "/api/use", strings.NewReader(`{"home":"home1","source":"nope","dry_run":true}`))
		req.Host = "127.0.0.1:7799"
		req.Header.Set("X-ACSW-Token", "tok123")
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}
	for _, o := range []string{"http://evil.example", "http://127.0.0.1:7800", "http://localhost:7799", "null"} {
		if c := send(o); c != 403 {
			t.Fatal(o, c)
		}
	}
	// 同源（Origin 与 Host 一致）或没有 Origin：放行到业务逻辑（这里 source 不存在，返回 409 而不是 403）
	for _, o := range []string{"http://127.0.0.1:7799", ""} {
		if c := send(o); c == 403 {
			t.Fatal(o, c)
		}
	}
}

func TestHTTPServerTimeouts(t *testing.T) {
	s, _ := newServer(t)
	srv := s.HTTPServer()
	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.IdleTimeout <= 0 || srv.WriteTimeout <= core.ModelsTimeout {
		t.Fatalf("%+v", srv)
	}
}
