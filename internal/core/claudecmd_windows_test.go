package core

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	tu "acsw/internal/testutil"
)

// 真的用 cmd.exe 跑生成的 .cmd：参数（含空格、&、%）原样转交，stdin 继承，退出码传回。
func TestClaudeCmdRunsOnWindows(t *testing.T) {
	if testing.Short() {
		t.Skip("-short 时跳过")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("没有 go")
	}
	a := claudeApp(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	tu.Must(t, os.WriteFile(src, []byte(`package main

import (
	"encoding/json"
	"io"
	"os"
)

func main() {
	in, _ := io.ReadAll(os.Stdin)
	json.NewEncoder(os.Stdout).Encode(map[string]any{"args": os.Args[1:], "stdin": string(in)})
	os.Exit(7)
}
`), 0o644))
	exe := filepath.Join(dir, "fake acsw.exe")
	b := exec.Command(goBin, "build", "-o", exe, src)
	b.Dir = dir
	b.Env = append(os.Environ(), "GO111MODULE=off")
	if out, err := b.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	r, err := a.WriteClaudeCmd("gw1", exe, dir, "", "", false)
	tu.Must(t, err)
	c := exec.Command(os.Getenv("ComSpec"))
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd /d /s /c ""` + r.Path + `" "a b" "x&y" 100% -p"`}
	c.Stdin = strings.NewReader("hello")
	out, err := c.Output()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 7 {
		t.Fatalf("退出码应为 7：%v %s", err, out)
	}
	var got struct {
		Args  []string `json:"args"`
		Stdin string   `json:"stdin"`
	}
	tu.Must(t, json.Unmarshal(out, &got))
	want := []string{"claude", "gw1", "a b", "x&y", "100%", "-p"}
	if strings.Join(got.Args, "|") != strings.Join(want, "|") || got.Stdin != "hello" {
		t.Fatalf("%+v", got)
	}
}
