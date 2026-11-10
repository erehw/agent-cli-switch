package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"acsw/internal/config"
	"acsw/internal/core"
	"acsw/internal/cred"
	"acsw/internal/exitcode"
	"acsw/internal/proc"
	tu "acsw/internal/testutil"
)

func TestHomeAliasConfirmCLI(t *testing.T) {
	p := tu.Fixture(t)
	a := &core.App{Paths: p, Cred: cred.NewMemory(), Proc: &proc.Fake{}, Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard, CI: config.CaseInsensitiveFS()}
	off1, off2 := filepath.Join(t.TempDir(), "o1"), filepath.Join(t.TempDir(), "o2")
	tu.Must(t, os.MkdirAll(off1, 0o700))
	tu.Must(t, os.MkdirAll(off2, 0o700))
	mapNow := func() string { b, _ := os.ReadFile(filepath.Join(p.Root, "map.toml")); return string(b) }
	run := func(dir string, yes, tty bool, input string) (int, string, string) {
		var out, errw strings.Builder
		c := homeAlias(a, "home1", dir, yes, strings.NewReader(input), tty, &out, &errw)
		return c, out.String(), errw.String()
	}

	if c, out, _ := run(off1, false, false, ""); c != 0 || !strings.Contains(out, "已设置") {
		t.Fatal(c, out)
	}
	before := mapNow()
	// 同一个目录：提示没有改动，退出码 0
	if c, out, _ := run(off1, false, false, ""); c != 0 || !strings.Contains(out, "没有改动") || mapNow() != before {
		t.Fatal(c, out)
	}
	// 非交互、没 --yes：拒绝，最后一行 JSON 是 confirm_required
	c, _, errs := run(off2, false, false, "")
	if c != exitcode.NeedConfirm.Exit() || mapNow() != before {
		t.Fatal(c, errs)
	}
	expectErr(t, lastJSON(t, errs), exitcode.NeedConfirm, map[string]any{"home": "home1"})
	// 交互：回车（默认 N）→ 不改
	if c, out, errs := run(off2, false, true, "\n"); c != 0 || !strings.Contains(out, "已取消") || !strings.Contains(errs, "[y/N]") || !strings.Contains(errs, off1) || mapNow() != before {
		t.Fatal(c, out, errs)
	}
	// 交互：y → 改
	if c, out, _ := run(off2, false, true, "y\n"); c != 0 || !strings.Contains(out, "已改") || mapNow() == before {
		t.Fatal(c, out)
	}
	// --yes：非交互也直接改
	if c, out, _ := run(off1, true, false, ""); c != 0 || !strings.Contains(out, "已改") {
		t.Fatal(c, out)
	}
}
