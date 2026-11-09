package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"acsw/internal/config"
	"acsw/internal/cred"
	tu "acsw/internal/testutil"
)

const webKey = "sk-web-SECRET-789"

func TestSourceModelsEndpoint(t *testing.T) {
	raw := []byte("{\"object\":\"list\",\"data\":[{\"id\":\"x-1\"},{\"id\":\"x-2\"}]}\n")
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(403)
			_, _ = w.Write([]byte("denied " + auth))
			return
		}
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	s, p := newServer(t)
	h := s.Handler()
	tu.Must(t, s.App.Cred.Set(cred.Target("api1"), webKey))
	tu.Must(t, config.SaveSource(p, "api1", &config.SourceMeta{Type: "custom", BaseURL: srv.URL + "/v1", Model: "m"}))
	tu.Must(t, s.App.Cred.Set(cred.Target("api2"), webKey))
	tu.Must(t, config.SaveSource(p, "api2", &config.SourceMeta{Type: "custom", BaseURL: srv.URL + "/bad", Model: "m"}))

	// 防护：token、Host、JSON、大小
	body := `{"name":"api1"}`
	if w := do(h, "POST", "/api/source/models", host, "", body); w.Code != 403 {
		t.Fatal("无 token", w.Code)
	}
	if w := do(h, "POST", "/api/source/models", "evil.example", "tok123", body); w.Code != 403 {
		t.Fatal("外部 Host", w.Code)
	}
	req := httptest.NewRequest("POST", "/api/source/models", strings.NewReader(body))
	req.Host = host
	req.Header.Set("X-ACSW-Token", "tok123")
	req.Header.Set("Content-Type", "text/plain")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != 415 {
		t.Fatal("非 JSON", rw.Code)
	}
	if w := do(h, "POST", "/api/source/models", host, "tok123", `{"name":"`+strings.Repeat("a", 2000)+`"}`); w.Code != 400 {
		t.Fatal("超大请求", w.Code)
	}
	if auth != "" {
		t.Fatal("被拦下的请求不应访问上游")
	}

	// 成功：原样返回
	w := do(h, "POST", "/api/source/models", host, "tok123", body)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), raw) || auth != "Bearer "+webKey {
		t.Fatal(w.Code, w.Body.String())
	}
	// 上游非 2xx → 502，不带 key
	w = do(h, "POST", "/api/source/models", host, "tok123", `{"name":"api2"}`)
	if w.Code != 502 || !strings.Contains(w.Body.String(), "403") || strings.Contains(w.Body.String(), webKey) {
		t.Fatal(w.Code, w.Body.String())
	}
	// openai / 不存在 → 400
	for _, n := range []string{"plus1", "nope"} {
		if w := do(h, "POST", "/api/source/models", host, "tok123", `{"name":"`+n+`"}`); w.Code != 400 || strings.Contains(w.Body.String(), webKey) {
			t.Fatal(n, w.Code, w.Body.String())
		}
	}
}
