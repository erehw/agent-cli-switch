package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"acsw/internal/config"
	"acsw/internal/cred"
	"acsw/internal/home"
	"acsw/internal/proc"
	tu "acsw/internal/testutil"
)

func srcApp(t *testing.T, pc proc.Checker) *App {
	p := tu.Fixture(t)
	return &App{Paths: p, Cred: cred.NewMemory(), Proc: pc, Stdout: &strings.Builder{}, Stderr: &strings.Builder{}}
}

func TestEditCustomSourceReapplies(t *testing.T) {
	f := &proc.Fake{AlivePIDs: map[int]bool{}}
	a := srcApp(t, f)
	tu.Must(t, a.Cred.Set(cred.Target("api1"), "k"))
	for _, h := range []string{"home1", "home2"} {
		_, err := a.Use(home.UseOptions{Home: h, Source: "api1"})
		tu.Must(t, err)
	}
	res, err := a.EditCustomSource("api1", "https://changed.example/v9", "gpt-z", "")
	tu.Must(t, err)
	if !res.MetaChanged || res.KeyChanged || len(res.Homes) != 2 || !res.Homes[0].OK || !res.Homes[1].OK {
		t.Fatal(res)
	}
	for _, h := range []string{"home1", "home2"} {
		c := tu.Read(filepath.Join(a.Paths.HomeDir(h), "config.toml"))
		if !strings.Contains(c, "https://changed.example/v9") || !strings.Contains(c, `model = "gpt-z"`) {
			t.Fatal(h, c)
		}
	}
	if v, _ := a.Cred.Get(cred.Target("api1")); v != "k" {
		t.Fatal("空 key 不应改 key")
	}
}

func TestEditCustomSourceRefusesBusyHome(t *testing.T) {
	f := &proc.Fake{AlivePIDs: map[int]bool{}}
	a := srcApp(t, f)
	tu.Must(t, a.Cred.Set(cred.Target("api1"), "k"))
	for _, h := range []string{"home1", "home2"} {
		_, err := a.Use(home.UseOptions{Home: h, Source: "api1"})
		tu.Must(t, err)
	}
	before1 := tu.Read(filepath.Join(a.Paths.HomeDir("home1"), "config.toml"))
	metaBefore, _ := config.LoadSource(a.Paths, "api1")
	// home2 有 codex 在跑
	f.AlivePIDs[4242] = true
	cleanup, err := proc.Register(a.Paths.HomeDir("home2"), proc.Entry{PID: 4242})
	tu.Must(t, err)
	defer cleanup()

	_, err = a.EditCustomSource("api1", "https://changed.example/v9", "gpt-z", "k2")
	if !errors.Is(err, ErrHomeBusy) || !strings.Contains(err.Error(), "home2") || strings.Contains(err.Error(), "home1") {
		t.Fatal(err)
	}
	if m, _ := config.LoadSource(a.Paths, "api1"); m.BaseURL != metaBefore.BaseURL || m.Model != metaBefore.Model {
		t.Fatal("被拒绝时 source 不应改", m)
	}
	if v, _ := a.Cred.Get(cred.Target("api1")); v != "k" {
		t.Fatal("被拒绝时 key 不应改")
	}
	if tu.Read(filepath.Join(a.Paths.HomeDir("home1"), "config.toml")) != before1 {
		t.Fatal("被拒绝时空闲的 home 也不应改")
	}
	// 只改 key 不涉及重新应用，不受在跑的 codex 影响
	res, err := a.EditCustomSource("api1", "", "", "k3")
	tu.Must(t, err)
	if !res.KeyChanged || res.MetaChanged {
		t.Fatal(res)
	}
	// 把 home2 切走后，它在跑 codex 不影响改 api1
	cleanup()
	tu.Must(t, a.Cred.Set(cred.Target("api2"), "k"))
	_, err = a.Use(home.UseOptions{Home: "home2", Source: "api2"})
	tu.Must(t, err)
	f.AlivePIDs[5555] = true
	c2, err := proc.Register(a.Paths.HomeDir("home2"), proc.Entry{PID: 5555})
	tu.Must(t, err)
	defer c2()
	res, err = a.EditCustomSource("api1", "https://changed.example/v9", "gpt-z", "")
	tu.Must(t, err)
	if !res.MetaChanged || len(res.Homes) != 1 || res.Homes[0].Home != "home1" || !res.Homes[0].OK {
		t.Fatal(res)
	}
}
func TestEditCustomSourceKeyOnly(t *testing.T) {
	a := srcApp(t, &proc.Fake{})
	tu.Must(t, a.Cred.Set(cred.Target("api1"), "k"))
	_, err := a.Use(home.UseOptions{Home: "home1", Source: "api1"})
	tu.Must(t, err)
	before := tu.Read(filepath.Join(a.Paths.HomeDir("home1"), "config.toml"))
	res, err := a.EditCustomSource("api1", "", "", "k2")
	tu.Must(t, err)
	if res.MetaChanged || !res.KeyChanged || len(res.Homes) != 0 || !strings.Contains(strings.Join(res.Notes, ""), "下次启动") {
		t.Fatal(res)
	}
	if tu.Read(filepath.Join(a.Paths.HomeDir("home1"), "config.toml")) != before {
		t.Fatal("只改 key 不应重写 home")
	}
	if v, _ := a.Cred.Get(cred.Target("api1")); v != "k2" {
		t.Fatal("key 没更新")
	}
}

func TestRemoveCustomSource(t *testing.T) {
	a := srcApp(t, &proc.Fake{})
	tu.Must(t, a.Cred.Set(cred.Target("api1"), "k"))
	tu.Must(t, a.Cred.Set(cred.Target("api2"), "k"))
	_, err := a.Use(home.UseOptions{Home: "home1", Source: "api1"})
	tu.Must(t, err)
	if _, err := a.RemoveCustomSource("api1"); err == nil || !strings.Contains(err.Error(), "home1") {
		t.Fatal(err)
	}
	dir, err := a.RemoveCustomSource("api2")
	tu.Must(t, err)
	if _, err := os.Stat(dir); err != nil || config.Exists(a.Paths.SourceDir("api2")) {
		t.Fatal(dir, err)
	}
}
