package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"acsw/internal/config"
	"acsw/internal/cred"
	"acsw/internal/proc"
	tu "acsw/internal/testutil"
)

const host = "127.0.0.1:7799"

func TestSourcePostGuards(t *testing.T) {
	s, _ := newServer(t)
	h := s.Handler()
	body := `{"name":"api9","base_url":"https://x.example/v1","model":"m","key":"secret-xyz"}`
	for _, u := range []string{"/api/source/add", "/api/source/edit", "/api/source/delete"} {
		if w := do(h, "POST", u, host, "", body); w.Code != 403 {
			t.Fatal(u, "无 token", w.Code)
		}
		if w := do(h, "POST", u, host, "bad", body); w.Code != 403 {
			t.Fatal(u, "错 token", w.Code)
		}
		if w := do(h, "POST", u, "evil.example", "tok123", body); w.Code != 403 {
			t.Fatal(u, "外部 Host", w.Code)
		}
		req := httptest.NewRequest("POST", u, strings.NewReader(body))
		req.Host = host
		req.Header.Set("X-ACSW-Token", "tok123")
		req.Header.Set("Content-Type", "text/plain")
		rw := httptest.NewRecorder()
		h.ServeHTTP(rw, req)
		if rw.Code != 415 {
			t.Fatal(u, "非 JSON", rw.Code)
		}
		big := `{"name":"` + strings.Repeat("a", 9000) + `"}`
		if w := do(h, "POST", u, host, "tok123", big); w.Code != 400 {
			t.Fatal(u, "超大请求", w.Code)
		}
	}
}

func TestSourceAddEditDelete(t *testing.T) {
	s, p := newServer(t)
	h := s.Handler()
	mem := s.App.Cred.(*cred.Memory)

	w := do(h, "POST", "/api/source/add", host, "tok123", `{"name":"api9","base_url":"https://x.example/v1/","model":"m1","key":"secret-xyz"}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), "secret-xyz") {
		t.Fatal(w.Code, w.Body.String())
	}
	if v, _ := mem.Get(cred.Target("api9")); v != "secret-xyz" {
		t.Fatal("key 没存")
	}
	// 重复、非法
	if w := do(h, "POST", "/api/source/add", host, "tok123", `{"name":"api9","base_url":"https://x.example","model":"m","key":"k"}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := do(h, "POST", "/api/source/add", host, "tok123", `{"name":"bad/name","base_url":"https://x.example","model":"m","key":"k"}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := do(h, "POST", "/api/source/add", host, "tok123", `{"name":"api8","base_url":"https://x.example","model":"m","key":""}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
	// status 不带 key
	w = do(h, "GET", "/api/status", host, "", "")
	if strings.Contains(w.Body.String(), "secret") || !strings.Contains(w.Body.String(), `"name":"api9"`) {
		t.Fatal(w.Body.String())
	}

	// 用起来，再改 base_url → 自动重新应用
	if w := do(h, "POST", "/api/use", host, "tok123", `{"home":"home2","source":"api9"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = do(h, "POST", "/api/source/edit", host, "tok123", `{"name":"api9","base_url":"https://y.example/v2","model":"m2","key":""}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var er struct {
		AllOK  bool `json:"all_homes_ok"`
		Result struct {
			MetaChanged bool `json:"meta_changed"`
			KeyChanged  bool `json:"key_changed"`
			Homes       []struct {
				Home string `json:"home"`
				OK   bool   `json:"ok"`
			} `json:"homes"`
		} `json:"result"`
	}
	tu.Must(t, json.Unmarshal(w.Body.Bytes(), &er))
	if !er.AllOK || !er.Result.MetaChanged || er.Result.KeyChanged || len(er.Result.Homes) != 1 || er.Result.Homes[0].Home != "home2" {
		t.Fatal(w.Body.String())
	}
	if c := tu.Read(p.HomeDir("home2") + "/config.toml"); !strings.Contains(c, "https://y.example/v2") {
		t.Fatal(c)
	}
	if v, _ := mem.Get(cred.Target("api9")); v != "secret-xyz" {
		t.Fatal("空 key 不应改 key")
	}
	// 只改 key
	w = do(h, "POST", "/api/source/edit", host, "tok123", `{"name":"api9","base_url":"https://y.example/v2","model":"m2","key":"secret-2"}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), "secret-2") || !strings.Contains(w.Body.String(), "下次启动") {
		t.Fatal(w.Code, w.Body.String())
	}
	// openai 不能改
	if w := do(h, "POST", "/api/source/edit", host, "tok123", `{"name":"plus1","base_url":"https://y.example","model":"m"}`); w.Code != 400 {
		t.Fatal(w.Code)
	}

	// 在用不能删
	w = do(h, "POST", "/api/source/delete", host, "tok123", `{"name":"api9"}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "home2") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do(h, "POST", "/api/source/delete", host, "tok123", `{"name":"plus1"}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
	// 切走再删
	tu.Must(t, s.App.Cred.Set(cred.Target("api1"), "k"))
	if w := do(h, "POST", "/api/use", host, "tok123", `{"home":"home2","source":"api1"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = do(h, "POST", "/api/source/delete", host, "tok123", `{"name":"api9"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if config.Exists(p.SourceDir("api9")) {
		t.Fatal("目录还在")
	}
	if _, err := mem.Get(cred.Target("api9")); err == nil {
		t.Fatal("凭据没删")
	}
}

func TestSourceEditRunningHome(t *testing.T) {
	s, p := newServer(t)
	f := &proc.Fake{AlivePIDs: map[int]bool{777: true}}
	s.App.Proc = f
	h := s.Handler()
	tu.Must(t, s.App.Cred.Set(cred.Target("api1"), "k"))
	if w := do(h, "POST", "/api/use", host, "tok123", `{"home":"home1","source":"api1"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cleanup, err := proc.Register(p.HomeDir("home1"), proc.Entry{PID: 777})
	tu.Must(t, err)
	defer cleanup()
	w := do(h, "POST", "/api/source/edit", host, "tok123", `{"name":"api1","base_url":"https://z.example","model":"m"}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "在跑") || !strings.Contains(w.Body.String(), "home1") {
		t.Fatal(w.Code, w.Body.String())
	}
	if m, _ := config.LoadSource(p, "api1"); m.BaseURL == "https://z.example" {
		t.Fatal("被拒绝时 source 不应改")
	}
	if c := tu.Read(p.HomeDir("home1") + "/config.toml"); strings.Contains(c, "z.example") {
		t.Fatal("被拒绝时 home 不应改", c)
	}
}
