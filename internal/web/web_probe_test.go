package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"acsw/internal/source"
)

type probeSrv struct {
	mu    sync.Mutex
	hits  []*http.Request
	mode  string
	delay time.Duration
}

func (p *probeSrv) handler(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.hits = append(p.hits, r.Clone(r.Context()))
	mode, delay := p.mode, p.delay
	p.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
	}
	switch mode {
	case "401":
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	case "weird":
		_, _ = w.Write([]byte(`<html>not json</html>`))
	default:
		_, _ = w.Write([]byte(`{"data":[{"id":"m-b"},{"id":"m-a"},{"id":"m-b"}]}`))
	}
}

func (p *probeSrv) take() []*http.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	h := p.hits
	p.hits = nil
	return h
}

func probeBody(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err, body)
	}
	return m
}

func TestModelsProbe(t *testing.T) {
	s, _ := newServer(t)
	h := s.Handler()
	ps := &probeSrv{}
	up := httptest.NewServer(http.HandlerFunc(ps.handler))
	defer up.Close()
	const formKey = "dummy-form-key"

	// 守卫
	if w := do(h, "POST", "/api/models/probe", host, "", `{"tool":"codex"}`); w.Code != 403 {
		t.Fatal("无 token", w.Code)
	}

	// 新 codex source：表单里的 base_url + key
	w := do(h, "POST", "/api/models/probe", host, "tok123", `{"tool":"codex","base_url":"`+up.URL+`/v1/","key":"`+formKey+`"}`)
	m := probeBody(t, w.Body.String())
	if w.Code != 200 || strings.Join(toStrings(m["ids"]), ",") != "m-a,m-b" || strings.Contains(w.Body.String(), formKey) {
		t.Fatal(w.Code, w.Body.String())
	}
	if hs := ps.take(); len(hs) != 1 || hs[0].URL.Path != "/v1/models" || hs[0].Header.Get("Authorization") != "Bearer "+formKey {
		t.Fatal("codex 请求不对")
	}

	// 新 claude source：/v1/models + x-api-key
	w = do(h, "POST", "/api/models/probe", host, "tok123", `{"tool":"claude","base_url":"`+up.URL+`","key":"`+formKey+`"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if hs := ps.take(); len(hs) != 1 || hs[0].URL.Path != "/v1/models" || hs[0].Header.Get("x-api-key") != formKey {
		t.Fatal("claude 请求不对")
	}

	// 新 source 没填 key：不发请求
	if w := do(h, "POST", "/api/models/probe", host, "tok123", `{"tool":"codex","base_url":"`+up.URL+`"}`); w.Code != 400 {
		t.Fatal(w.Code)
	}

	// 已保存的 claude source：用凭据管理器里的 key
	tuMust(t, s.App.AddClaudeSource("gw1", source.ClaudeFields{BaseURL: up.URL, Model: "m"}, "dummy-saved-key"))
	w = do(h, "POST", "/api/models/probe", host, "tok123", `{"tool":"claude","name":"gw1"}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), "dummy-saved-key") {
		t.Fatal(w.Code, w.Body.String())
	}
	if hs := ps.take(); len(hs) != 1 || hs[0].Header.Get("x-api-key") != "dummy-saved-key" {
		t.Fatal("已保存 source 应用凭据管理器里的 key")
	}
	// base_url 改了但没输入 key：拒绝，不把已保存的 key 发到新地址
	w = do(h, "POST", "/api/models/probe", host, "tok123", `{"tool":"claude","name":"gw1","base_url":"http://127.0.0.1:1"}`)
	if w.Code != 400 || len(ps.take()) != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	// 工具不对
	if w := do(h, "POST", "/api/models/probe", host, "tok123", `{"tool":"codex","name":"gw1"}`); w.Code != 400 || !strings.Contains(w.Body.String(), "source_tool_mismatch") {
		t.Fatal(w.Code, w.Body.String())
	}

	// 401
	ps.mode = "401"
	w = do(h, "POST", "/api/models/probe", host, "tok123", `{"tool":"codex","base_url":"`+up.URL+`","key":"`+formKey+`"}`)
	m = probeBody(t, w.Body.String())
	if w.Code != 502 || m["reason"] != "unauthorized" || m["http_status"] != float64(401) || strings.Contains(w.Body.String(), formKey) {
		t.Fatal(w.Code, w.Body.String())
	}

	// 非标准响应
	ps.mode = "weird"
	w = do(h, "POST", "/api/models/probe", host, "tok123", `{"tool":"codex","base_url":"`+up.URL+`","key":"`+formKey+`"}`)
	m = probeBody(t, w.Body.String())
	if w.Code != 200 || m["nonstandard"] != true || !strings.Contains(m["raw"].(string), "not json") {
		t.Fatal(w.Code, w.Body.String())
	}

	// 超时
	ps.mode, ps.delay = "", 2*time.Second
	old := probeTimeout
	probeTimeout = 200 * time.Millisecond
	defer func() { probeTimeout = old }()
	w = do(h, "POST", "/api/models/probe", host, "tok123", `{"tool":"codex","base_url":"`+up.URL+`","key":"`+formKey+`"}`)
	m = probeBody(t, w.Body.String())
	if w.Code != 502 || m["reason"] != "timeout" {
		t.Fatal(w.Code, w.Body.String())
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func tuMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
