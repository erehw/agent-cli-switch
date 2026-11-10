package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"acsw/internal/config"
	"acsw/internal/exitcode"
	"acsw/internal/proc"
	tu "acsw/internal/testutil"
)

func TestUnmapProject(t *testing.T) {
	a, official, prj := aliasFixture(t)
	_ = official
	_, err := a.MapProject(prj, "home2")
	tu.Must(t, err)
	// 子目录不是映射表里的那条：拒绝，并指出上级映射
	sub := filepath.Join(prj, "sub")
	_, err = a.UnmapProject(sub)
	if c, _ := exitcode.Of(err); c != exitcode.NotMapped || !strings.Contains(err.Error(), prj) {
		t.Fatal(err)
	}
	if f := exitcode.Fields(err); f["mapped_dir"] != prj || f["home"] != "home2" {
		t.Fatal(f)
	}
	// 在跑的 codex：允许取消，只提示
	f := a.Proc.(*proc.Fake)
	f.AlivePIDs[4242] = true
	cleanup, err := proc.Register(a.Paths.HomeDir("home2"), proc.Entry{PID: 4242, Cwd: sub, Started: time.Now()})
	tu.Must(t, err)
	defer cleanup()
	r, err := a.UnmapProject(strings.ToUpper(prj[:1]) + prj[1:])
	tu.Must(t, err)
	if r.Home != "home2" || r.Running != 1 || !config.SamePath(r.Dir, prj, a.CI) {
		t.Fatalf("%+v", r)
	}
	m, err := config.LoadMap(a.Paths)
	tu.Must(t, err)
	if len(m.Projects) != 0 {
		t.Fatal(m.Projects)
	}
	if _, err := a.UnmapProject(prj); err == nil {
		t.Fatal("再取消一次应报未映射")
	}
}

func TestRecentInHome(t *testing.T) {
	a, official, _ := aliasFixture(t)
	before := snapshot(t, official)
	_, err := a.SetHomeAlias("home1", official)
	tu.Must(t, err)
	r, err := a.RecentInHome("home1", 10)
	tu.Must(t, err)
	if len(r.Threads) != 3 || r.Threads[0].From != "db" {
		t.Fatalf("%+v", r)
	}
	if after := snapshot(t, official); after != before {
		t.Fatal("官方目录被改动了")
	}
	if _, err := a.RecentInHome("nope", 5); err == nil {
		t.Fatal("不存在的 home")
	}
}

func TestRemoveHome(t *testing.T) {
	a, official, prj := aliasFixture(t)
	before := snapshot(t, official)
	tu.Must(t, a.AddHome("home3"))
	tu.Must(t, os.MkdirAll(filepath.Join(a.Paths.HomeDir("home3"), "sessions"), 0o700))
	_, err := a.MapProject(prj, "home3")
	tu.Must(t, err)

	if _, err := a.RemoveHome("nope", false); err == nil {
		t.Fatal("不存在的 home")
	} else if c, _ := exitcode.Of(err); c != exitcode.HomeNotFound {
		t.Fatal(err)
	}
	// 有项目映射：拒绝
	_, err = a.RemoveHome("home3", false)
	if c, _ := exitcode.Of(err); c != exitcode.HomeInUse {
		t.Fatal(err)
	}
	_, err = a.UnmapProject(prj)
	tu.Must(t, err)
	// 有 codex 在跑：拒绝
	f := a.Proc.(*proc.Fake)
	f.AlivePIDs[777] = true
	cleanup, err := proc.Register(a.Paths.HomeDir("home3"), proc.Entry{PID: 777})
	tu.Must(t, err)
	if _, err := a.RemoveHome("home3", false); err == nil {
		t.Fatal("有 codex 在跑应拒绝")
	} else if c, _ := exitcode.Of(err); c != exitcode.HomeInUse {
		t.Fatal(err)
	}
	cleanup()
	// 默认：移到备份区，会话还在
	r, err := a.RemoveHome("home3", false)
	tu.Must(t, err)
	if r.Purged || r.BackupDir == "" || !config.Exists(filepath.Join(r.BackupDir, "sessions")) || config.Exists(a.Paths.HomeDir("home3")) {
		t.Fatalf("%+v", r)
	}
	m, err := config.LoadMap(a.Paths)
	tu.Must(t, err)
	if _, ok := m.Homes["home3"]; ok {
		t.Fatal("map 里还有 home3")
	}
	// purge：彻底删除
	tu.Must(t, a.AddHome("home4"))
	r, err = a.RemoveHome("home4", true)
	tu.Must(t, err)
	if !r.Purged || r.BackupDir != "" || config.Exists(a.Paths.HomeDir("home4")) {
		t.Fatalf("%+v", r)
	}
	// 别名 home：只删 acsw 自己的目录，官方目录不动
	abs, err := a.SetHomeAlias("home1", official)
	tu.Must(t, err)
	r, err = a.RemoveHome("home1", true)
	tu.Must(t, err)
	if r.AliasOf != abs || !config.Exists(official) {
		t.Fatalf("%+v", r)
	}
	if after := snapshot(t, official); after != before {
		t.Fatal("官方目录被改动了")
	}
}
