package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"acsw/internal/cred"
	"acsw/internal/proc"
	tu "acsw/internal/testutil"
)

func TestDoctor(t *testing.T) {
	a, official, prj := aliasFixture(t)
	self, _ := os.Executable()
	a.CodexPath = self
	a.Version = "9.9.9"
	tu.Must(t, a.Cred.Set(cred.Target("api1"), "sk-doctor-SECRET"))
	_, err := a.SetHomeAlias("home1", official)
	tu.Must(t, err)
	_, err = a.MapProject(prj, "home1")
	tu.Must(t, err)
	_, err = a.MapProject(filepath.Join(a.Paths.Root, "gone"), "home2")
	tu.Must(t, err)
	// 一条已退出进程的运行登记
	_, err = proc.Register(a.Paths.HomeDir("home2"), proc.Entry{PID: 999999})
	tu.Must(t, err)

	rep := a.Doctor()
	js, _ := json.Marshal(rep)
	if strings.Contains(string(js), "SECRET") || strings.Contains(string(js), "acct-official") || strings.Contains(string(js), "fake-access") {
		t.Fatal("doctor 不能带 key 或 auth.json 内容")
	}
	lv := map[string]string{}
	for _, c := range rep.Checks {
		lv[c.Name] = c.Level
	}
	want := map[string]string{
		"root": "ok", "lock": "ok", "codex": "ok", "map": "ok",
		"source:api1": "ok", "home:home1": "ok", "home:home2": "warn", "runs": "warn",
		"project:" + filepath.Join(a.Paths.Root, "gone"): "warn",
	}
	for k, v := range want {
		if lv[k] != v {
			t.Fatalf("%s=%q，应为 %q\n%s", k, lv[k], v, js)
		}
	}
	// 只有 error 才让 OK 为 false：去掉其他没有 key 的 source 之前，确认 OK=false 来自 error
	for _, c := range rep.Checks {
		if c.Level == "error" && !strings.HasPrefix(c.Name, "source:") {
			t.Fatal("意外的 error", c)
		}
	}
	// 其他 source（fixture 里没有 key 的 custom）应是 error，于是整体不 OK
	if rep.OK || rep.Env.Version != "9.9.9" || rep.Env.Codex.From != "app" {
		t.Fatal(string(js))
	}
	// codex 找不到 → error
	a.CodexPath = filepath.Join(t.TempDir(), "no-codex.exe")
	for _, x := range a.Doctor().Checks {
		if x.Name == "codex" && (x.Level != "error" || !strings.Contains(x.Msg, "no-codex.exe")) {
			t.Fatal(x)
		}
	}
}

func TestDoctorOKWhenClean(t *testing.T) {
	p := tu.Fixture(t)
	a := &App{Paths: p, Cred: cred.NewMemory(), Proc: &proc.Fake{AlivePIDs: map[int]bool{}}}
	self, _ := os.Executable()
	a.CodexPath = self
	// 去掉 fixture 里的 source，只留 home
	tu.Must(t, os.RemoveAll(p.SourcesDir()))
	rep := a.Doctor()
	if !rep.OK {
		js, _ := json.Marshal(rep)
		t.Fatal(string(js))
	}
}
