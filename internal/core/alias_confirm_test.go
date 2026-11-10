package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"acsw/internal/config"
	"acsw/internal/cred"
	"acsw/internal/exitcode"
	"acsw/internal/home"
	"acsw/internal/proc"
	tu "acsw/internal/testutil"
)

func mapBytes(t *testing.T, a *App) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(a.Paths.Root, "map.toml"))
	tu.Must(t, err)
	return string(b)
}

func TestAliasHomeConfirm(t *testing.T) {
	a, official, _ := aliasFixture(t)
	official2 := filepath.Join(t.TempDir(), "official2")
	tu.Must(t, os.MkdirAll(official2, 0o700))

	// 空的普通 home：直接设，不用确认
	r, err := a.AliasHome("home1", official, false)
	tu.Must(t, err)
	if r.Unchanged || r.NeedsConfirm() {
		t.Fatalf("%+v", r)
	}

	// 同一个目录：不写 map.toml
	before := mapBytes(t, a)
	st, _ := os.Stat(filepath.Join(a.Paths.Root, "map.toml"))
	time.Sleep(20 * time.Millisecond)
	r, err = a.AliasHome("home1", official, false)
	tu.Must(t, err)
	st2, _ := os.Stat(filepath.Join(a.Paths.Root, "map.toml"))
	if !r.Unchanged || mapBytes(t, a) != before || !st.ModTime().Equal(st2.ModTime()) {
		t.Fatalf("同一个目录不应写：%+v", r)
	}

	// 另一个目录：不确认就拒绝，不写
	r, err = a.AliasHome("home1", official2, false)
	codeIs(t, err, exitcode.NeedConfirm)
	if r.PrevAliasOf == "" || r.AliasOf == "" || mapBytes(t, a) != before {
		t.Fatalf("%+v", r)
	}
	if f := exitcode.Fields(err); f["prev_alias_of"] == nil || f["home"] != "home1" {
		t.Fatal(f)
	}
	// home 里有 codex 在跑：即使确认也拒绝
	a.Proc.(*proc.Fake).AlivePIDs[4242] = true
	cleanup, err := proc.Register(a.Paths.HomeDir("home1"), proc.Entry{PID: 4242, Started: time.Now()})
	tu.Must(t, err)
	_, err = a.AliasHome("home1", official2, true)
	codeIs(t, err, exitcode.HomeInUse)
	cleanup()
	r, err = a.AliasHome("home1", official2, true)
	tu.Must(t, err)
	m, _ := config.LoadMap(a.Paths)
	if !config.SamePath(m.Homes["home1"].AliasOf, official2, a.CI) || r.PrevAliasOf == "" {
		t.Fatalf("%+v %+v", r, m.Homes["home1"])
	}

	// 在用 source 的普通 home：要确认；确认后 source 从 map 去掉，目录保留
	tu.Must(t, a.Cred.Set(cred.Target("api1"), "dummy-k"))
	_, err = a.Use(home.UseOptions{Home: "home2", Source: "api1"})
	tu.Must(t, err)
	r, err = a.AliasHome("home2", official, false)
	codeIs(t, err, exitcode.NeedConfirm)
	if r.PrevSource != "api1" || r.DirHasFiles != true {
		t.Fatalf("%+v", r)
	}
	r, err = a.AliasHome("home2", official, true)
	tu.Must(t, err)
	m, _ = config.LoadMap(a.Paths)
	if m.Homes["home2"].Source != "" || m.Homes["home2"].AliasOf == "" || !config.Exists(filepath.Join(a.Paths.HomeDir("home2"), "config.toml")) {
		t.Fatalf("%+v", m.Homes["home2"])
	}

	// 目录里有文件、没 source 的普通 home：要确认；只有 .acsw-run 不算
	tu.Must(t, a.AddHome("home3"))
	tu.Must(t, os.MkdirAll(proc.RunDir(a.Paths.HomeDir("home3")), 0o700))
	off3 := filepath.Join(t.TempDir(), "official3")
	tu.Must(t, os.MkdirAll(off3, 0o700))
	tu.Must(t, a.AddHome("home4"))
	tu.Must(t, os.WriteFile(filepath.Join(a.Paths.HomeDir("home4"), "history.jsonl"), []byte("x"), 0o600))
	_, err = a.AliasHome("home4", off3, false)
	codeIs(t, err, exitcode.NeedConfirm)
	_, err = a.AliasHome("home3", off3, false)
	tu.Must(t, err)
}
