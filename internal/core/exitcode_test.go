package core

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"acsw/internal/config"
	"acsw/internal/cred"
	"acsw/internal/exitcode"
	"acsw/internal/home"
	"acsw/internal/proc"
	tu "acsw/internal/testutil"
)

func codeOf(t *testing.T, err error) exitcode.Code {
	t.Helper()
	c, ok := exitcode.Of(err)
	if !ok {
		t.Fatalf("错误没有附退出码：%v", err)
	}
	return c
}

func TestRunErrorCodes(t *testing.T) {
	p := tu.Fixture(t)
	a := &App{Paths: p, Cred: cred.NewMemory(), Proc: &proc.Fake{}, CodexPath: filepath.Join(p.Root, "no-such-codex.exe"), Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}
	unmapped := t.TempDir()
	prj := filepath.Join(p.Root, "prj")
	tu.Must(t, os.MkdirAll(prj, 0o755))
	_, err := a.MapProject(prj, "home2")
	tu.Must(t, err)

	run := func(dir, h, expect string) error {
		_, err := a.Run(RunOptions{Dir: dir, Home: h, ExpectHome: expect})
		return err
	}
	if c := codeOf(t, run(unmapped, "", "")); c != exitcode.NotMapped {
		t.Fatal(c)
	}
	if c := codeOf(t, run(unmapped, "", "home2")); c != exitcode.NotMapped {
		t.Fatal(c)
	}
	if c := codeOf(t, run(prj, "", "home1")); c != exitcode.ExpectMismatch {
		t.Fatal(c)
	}
	if c := codeOf(t, run(prj, "home1", "home2")); c != exitcode.HomeConflict {
		t.Fatal(c)
	}
	if c := codeOf(t, run(prj, "nohome", "")); c != exitcode.HomeNotFound {
		t.Fatal(c)
	}
	if c := codeOf(t, run(prj, "", "home2")); c != exitcode.NoSource {
		t.Fatal(c)
	}
	// 有 source 但凭据里没 key
	_, err = a.Use(home.UseOptions{Home: "home2", Source: "api1"})
	tu.Must(t, err)
	if c := codeOf(t, run(prj, "", "home2")); c != exitcode.KeyUnreadable {
		t.Fatal(c)
	}
	// 有 key，codex 路径不存在 → 启动失败
	tu.Must(t, a.Cred.Set(cred.Target("api1"), "k"))
	if c := codeOf(t, run(prj, "", "home2")); c != exitcode.CodexStart {
		t.Fatal(c)
	}
	// 用法类错误仍能被 IsUsage 识别
	if !IsUsage(run(unmapped, "", "")) {
		t.Fatal("NotMapped 应仍是用法错误")
	}
}

func TestLockBusyCode(t *testing.T) {
	p := tu.Fixture(t)
	a := &App{Paths: p, Cred: cred.NewMemory(), Proc: &proc.Fake{}}
	unlock, err := config.Lock(p, 0)
	tu.Must(t, err)
	defer unlock()
	old := lockWait
	lockWait = 0
	defer func() { lockWait = old }()
	_, err = a.Use(home.UseOptions{Home: "home1", Source: "api1"})
	if c := codeOf(t, err); c != exitcode.LockBusy || !errors.Is(err, config.ErrLockBusy) {
		t.Fatal(c, err)
	}
}

func TestModelsErrorCodes(t *testing.T) {
	p := tu.Fixture(t)
	a := &App{Paths: p, Cred: cred.NewMemory(), Proc: &proc.Fake{}}
	if c := codeOf(t, func() error { _, err := a.SourceModels(t.Context(), "plus1"); return err }()); c != exitcode.SourceInvalid {
		t.Fatal(c)
	}
	if c := codeOf(t, func() error { _, err := a.SourceModels(t.Context(), "nope"); return err }()); c != exitcode.SourceInvalid {
		t.Fatal(c)
	}
	if c := codeOf(t, func() error { _, err := a.SourceModels(t.Context(), "api1"); return err }()); c != exitcode.KeyUnreadable {
		t.Fatal(c)
	}
}
