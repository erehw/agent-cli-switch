package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"acsw/internal/config"
)

func j(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestHomeEndpoints(t *testing.T) {
	s, p := newServer(t)
	exe := filepath.Join(t.TempDir(), "acsw.exe")
	s.AcswExe = exe
	h := s.Handler()
	post := func(url string, body any) (int, map[string]any) {
		w := do(h, "POST", url, host, "tok123", j(body))
		var d map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &d)
		return w.Code, d
	}

	// 新增 home
	if c, d := post("/api/home/add", map[string]string{"home": "home7"}); c != 200 || !config.Exists(p.HomeDir("home7")) {
		t.Fatal(c, d)
	}
	if c, _ := post("/api/home/add", map[string]string{"home": "home7"}); c != 400 {
		t.Fatal("重复新增应失败", c)
	}
	if c, _ := post("/api/home/add", map[string]string{"home": "../x"}); c == 200 {
		t.Fatal("非法名称")
	}

	// 映射：相对路径、不存在的目录拒绝
	prj := filepath.Join(t.TempDir(), "my prj")
	if c, _ := post("/api/map", map[string]string{"home": "home7", "dir": "rel\\dir"}); c != 400 {
		t.Fatal(c)
	}
	if c, _ := post("/api/map", map[string]string{"home": "home7", "dir": prj}); c != 400 {
		t.Fatal(c)
	}
	if err := os.MkdirAll(prj, 0o755); err != nil {
		t.Fatal(err)
	}
	if c, d := post("/api/map", map[string]string{"home": "home7", "dir": prj}); c != 200 {
		t.Fatal(c, d)
	}

	// 有映射时删除 home：409 home_in_use
	if c, d := post("/api/home/rm", map[string]string{"home": "home7"}); c != 409 || d["kind"] != "home_in_use" {
		t.Fatal(c, d)
	}
	// 取消映射：子目录不行（404 not_mapped），目录本身可以
	if c, d := post("/api/unmap", map[string]string{"dir": filepath.Join(prj, "sub")}); c != 404 || d["kind"] != "not_mapped" {
		t.Fatal(c, d)
	}
	if c, d := post("/api/unmap", map[string]string{"dir": prj}); c != 200 {
		t.Fatal(c, d)
	}

	// 生成 .cmd
	c, d := post("/api/home/cmd", map[string]string{"home": "home7"})
	if c != 200 {
		t.Fatal(c, d)
	}
	b, _ := os.ReadFile(filepath.Join(p.HomeDir("home7"), "acsw-codex.cmd"))
	if !strings.Contains(string(b), `"`+exe+`" codex --expect-home home7`) {
		t.Fatal(string(b))
	}

	// 官方别名：设置、拒绝 use、取消
	official := filepath.Join(t.TempDir(), "official")
	if err := os.MkdirAll(official, 0o700); err != nil {
		t.Fatal(err)
	}
	if c, d := post("/api/home/alias", map[string]any{"home": "home7", "dir": official}); c != 200 || d["alias_of"] == "" {
		t.Fatal(c, d)
	}
	if c, d := post("/api/use", map[string]any{"home": "home7", "source": "api1"}); c != 409 || !strings.Contains(j(d), "--off") {
		t.Fatal(c, d)
	}
	if c, d := post("/api/home/alias", map[string]any{"home": "home7", "off": true}); c != 200 {
		t.Fatal(c, d)
	}

	// 删除：移到备份区（网页不提供 purge）
	c, d = post("/api/home/rm", map[string]string{"home": "home7"})
	if c != 200 || config.Exists(p.HomeDir("home7")) {
		t.Fatal(c, d)
	}
	res := d["result"].(map[string]any)
	if res["purged"] != false || res["backup_dir"] == "" {
		t.Fatal(d)
	}
	if c, _ := post("/api/home/rm", map[string]string{"home": "home7"}); c != 404 {
		t.Fatal("再删应 404", c)
	}
}

func TestStatusEnvDoctorLast(t *testing.T) {
	s, p := newServer(t)
	h := s.Handler()
	w := do(h, "GET", "/api/status", host, "tok123", "")
	var st struct {
		Env struct {
			Root     string `json:"root"`
			RootFrom string `json:"root_from"`
			Codex    struct {
				From  string `json:"from"`
				Found bool   `json:"found"`
			} `json:"codex"`
		} `json:"env"`
		Homes []struct {
			Dir string `json:"dir"`
		} `json:"homes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil || st.Env.Root != p.Root || st.Env.RootFrom == "" || len(st.Homes) == 0 || st.Homes[0].Dir == "" {
		t.Fatal(w.Body.String())
	}
	for _, u := range []string{"/api/doctor", "/api/last?home=home1"} {
		if w := do(h, "GET", u, host, "", ""); w.Code != 403 {
			t.Fatal("需要 token", u, w.Code)
		}
	}
	w = do(h, "GET", "/api/doctor", host, "tok123", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"checks"`) {
		t.Fatal(w.Body.String())
	}
	w = do(h, "GET", "/api/last?home=home1&n=3", host, "tok123", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"threads"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do(h, "GET", "/api/last?home=nope", host, "tok123", ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
}
