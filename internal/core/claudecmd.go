package core

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"acsw/internal/claude"
	"acsw/internal/config"
	"acsw/internal/exitcode"
)

// CmdMarker 是 acsw 生成的 .cmd 里的标记行前缀。有这一行的文件可以放心覆盖；没有的（别人写的）默认不覆盖。
const CmdMarker = "rem acsw-generated"

// ClaudeCmdContent 生成 claude-<source>.cmd 的正文（CRLF、无 BOM、只有 ASCII 以外的字符来自路径）。
// 调 acsw.exe（完整路径，加引号）的 claude <source>，%* 原样转交参数，stdin 自然继承，
// exit /b %ERRORLEVEL% 把 acsw（即 claude）的退出码传回调用方。路径里的 % 写成 %%，免得被 cmd 展开。
// mode 为空时不写 --key-mode，用 source 自己的默认（helper）。
func ClaudeCmdContent(acswExe, source string, mode claude.Mode) string {
	exe := strings.ReplaceAll(acswExe, "%", "%%")
	extra := ""
	if mode != "" {
		extra = " --key-mode " + string(mode)
	}
	return "@echo off\r\n" +
		CmdMarker + " by acsw claude cmd: source=" + source + " (regenerate with acsw claude cmd; manual edits are overwritten)\r\n" +
		fmt.Sprintf("\"%s\" claude %s%s %%*\r\n", exe, source, extra) +
		"exit /b %ERRORLEVEL%\r\n"
}

// IsAcswCmd 判断 .cmd 文件是不是 acsw 生成的（前 8KB 里有标记行）。
func IsAcswCmd(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 8192)
	n, _ := f.Read(buf)
	sc := bufio.NewScanner(bytes.NewReader(buf[:n]))
	for sc.Scan() {
		l := strings.ToLower(strings.TrimSpace(sc.Text()))
		l = strings.TrimPrefix(l, "@")
		if strings.HasPrefix(l, CmdMarker) {
			return true
		}
	}
	return false
}

// ClaudeCmdResult 是 acsw claude cmd 的结果。
type ClaudeCmdResult struct {
	Path        string `json:"path"`
	Source      string `json:"source"`
	KeyMode     string `json:"key_mode,omitempty"` // 空 = 用 source 自己的默认
	Overwritten bool   `json:"overwritten"`        // 覆盖了已有文件
	Forced      bool   `json:"forced,omitempty"`   // 覆盖的是不是 acsw 生成的文件（用了 --force）
}

// WriteClaudeCmd 生成 claude-<source>.cmd。dir 为空时放在 acsw.exe 所在目录（通常在 PATH 里）；
// name 为空时用 claude-<source>.cmd，没写扩展名时补 .cmd。已有 acsw 生成的同名文件直接覆盖；
// 已有别的文件时不覆盖（NeedConfirm），force 为真才覆盖。
func (a *App) WriteClaudeCmd(src, acswExe, dir, name, keyMode string, force bool) (ClaudeCmdResult, error) {
	res := ClaudeCmdResult{Source: src}
	if _, err := a.claudeMeta(src); err != nil {
		return res, err
	}
	var mode claude.Mode
	if strings.TrimSpace(keyMode) != "" {
		m, err := claude.ParseMode(keyMode)
		if err != nil {
			return res, exitcode.With(exitcode.BadArgs, Usage(err.Error()))
		}
		mode = m
		res.KeyMode = string(m)
	}
	acswExe = strings.TrimSpace(acswExe)
	if acswExe == "" {
		return res, errors.New("acsw 可执行路径为空")
	}
	exe, err := filepath.Abs(acswExe)
	if err != nil {
		return res, err
	}
	if strings.TrimSpace(dir) == "" {
		dir = filepath.Dir(exe)
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return res, err
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return res, Usage(fmt.Sprintf("目录 %s 不存在或不是目录", dir))
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "claude-" + src + ".cmd"
	}
	if name != filepath.Base(name) || strings.ContainsAny(name, `\/:*?"<>|%`) || strings.Trim(name, ". ") == "" {
		return res, Usage(fmt.Sprintf("文件名 %q 不合法（只写文件名，不带目录）", name))
	}
	if ext := strings.ToLower(filepath.Ext(name)); ext != ".cmd" && ext != ".bat" {
		name += ".cmd"
	}
	res.Path = filepath.Join(dir, name)
	if st, err := os.Stat(res.Path); err == nil {
		if st.IsDir() {
			return res, Usage(fmt.Sprintf("%s 是目录", res.Path))
		}
		res.Overwritten = true
		if !IsAcswCmd(res.Path) {
			if !force {
				return res, exitcode.WithF(exitcode.NeedConfirm,
					fmt.Errorf("%s 已存在，而且不是 acsw 生成的，没有覆盖；确定要覆盖请加 --force", res.Path),
					exitcode.F{"path": res.Path, "source": src})
			}
			res.Forced = true
		}
	}
	if err := config.WriteFileAtomic(res.Path, []byte(ClaudeCmdContent(exe, src, mode)), 0o644); err != nil {
		return res, fmt.Errorf("写 %s 失败：%w", res.Path, err)
	}
	return res, nil
}
