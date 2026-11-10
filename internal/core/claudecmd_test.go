package core

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"acsw/internal/exitcode"
	tu "acsw/internal/testutil"
)

func TestWriteClaudeCmd(t *testing.T) {
	a := claudeApp(t)
	binDir := t.TempDir()
	exe := filepath.Join(binDir, "acsw.exe")
	tu.Must(t, os.WriteFile(exe, []byte("x"), 0o755))

	// 默认：acsw.exe 所在目录、claude-<source>.cmd、不写 --key-mode
	r, err := a.WriteClaudeCmd("gw1", exe, "", "", "", false)
	tu.Must(t, err)
	if r.Path != filepath.Join(binDir, "claude-gw1.cmd") || r.Overwritten {
		t.Fatalf("%+v", r)
	}
	got := tu.Read(r.Path)
	want := "@echo off\r\n" + CmdMarker + " by acsw claude cmd: source=gw1 (regenerate with acsw claude cmd; manual edits are overwritten)\r\n\"" + exe + "\" claude gw1 %*\r\nexit /b %ERRORLEVEL%\r\n"
	if got != want {
		t.Fatalf("内容不对：\n%q\nwant\n%q", got, want)
	}
	if strings.Contains(got, dummyClaudeKey) || !IsAcswCmd(r.Path) {
		t.Fatal("不应含 key，且应能认出是 acsw 生成的")
	}

	// acsw 生成的：直接覆盖
	r, err = a.WriteClaudeCmd("gw1", exe, "", "", "plain", false)
	tu.Must(t, err)
	if !r.Overwritten || r.Forced || !strings.Contains(tu.Read(r.Path), "claude gw1 --key-mode plain %*") {
		t.Fatalf("%+v", r)
	}

	// --dir、--name（补 .cmd）
	other := t.TempDir()
	r, err = a.WriteClaudeCmd("gw1", exe, other, "cx", "", false)
	tu.Must(t, err)
	if r.Path != filepath.Join(other, "cx.cmd") {
		t.Fatal(r.Path)
	}

	// 不是 acsw 生成的：不覆盖，NeedConfirm；--force 才覆盖
	mine := filepath.Join(other, "claude-gw1.cmd")
	tu.Must(t, os.WriteFile(mine, []byte("@echo mine\r\n"), 0o644))
	r, err = a.WriteClaudeCmd("gw1", exe, other, "", "", false)
	codeIs(t, err, exitcode.NeedConfirm)
	if tu.Read(mine) != "@echo mine\r\n" {
		t.Fatal("不应覆盖别人的文件")
	}
	r, err = a.WriteClaudeCmd("gw1", exe, other, "", "", true)
	tu.Must(t, err)
	if !r.Forced || !IsAcswCmd(mine) {
		t.Fatalf("%+v", r)
	}

	// 路径里的 % 写成 %%
	if c := ClaudeCmdContent(`C:\a%b\acsw.exe`, "gw1", ""); !strings.Contains(c, `"C:\a%%b\acsw.exe" claude gw1 %*`) {
		t.Fatal(c)
	}

	// 错误：不存在的 source、codex source、坏的文件名、不存在的目录、坏的 key-mode
	_, err = a.WriteClaudeCmd("nope", exe, "", "", "", false)
	if err == nil {
		t.Fatal("不存在的 source 应报错")
	}
	_, err = a.WriteClaudeCmd("api1", exe, "", "", "", false)
	codeIs(t, err, exitcode.ToolMismatch)
	for _, n := range []string{`..\x.cmd`, `a/b.cmd`, "a%b"} {
		if _, err := a.WriteClaudeCmd("gw1", exe, "", n, "", false); err == nil {
			t.Fatal("文件名应拒绝", n)
		}
	}
	if _, err := a.WriteClaudeCmd("gw1", exe, filepath.Join(other, "nope"), "", "", false); err == nil {
		t.Fatal("目录不存在应报错")
	}
	_, err = a.WriteClaudeCmd("gw1", exe, "", "", "weird", false)
	codeIs(t, err, exitcode.BadArgs)
}

// 真的用 cmd.exe 跑生成的 .cmd：参数（含空格、&、%）原样转交，stdin 继承，退出码传回。
func TestClaudeCmdRunsOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" || testing.Short() {
		t.Skip("只在 Windows 上跑")
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
