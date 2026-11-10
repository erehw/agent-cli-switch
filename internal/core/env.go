package core

import (
	"os"
	"os/exec"
	"strings"

	"acsw/internal/config"
)

// CodexInfo 说明 acsw 会用哪个 codex。
type CodexInfo struct {
	Path  string `json:"path"`            // 找到的路径；找不到时为 ""
	From  string `json:"from"`            // "ACSW_CODEX"、"PATH"，测试里直接指定时为 "app"；找不到时为 ""
	Found bool   `json:"found"`           // 路径存在
	Error string `json:"error,omitempty"` // 找不到时的说明
}

// CodexInfo 返回 codex 的查找结果（和 Run 用的规则相同）。
func (a *App) CodexInfo() CodexInfo {
	if a.CodexPath != "" {
		ci := CodexInfo{Path: a.CodexPath, From: "app", Found: config.Exists(a.CodexPath)}
		if !ci.Found {
			ci.Error = "指定的 codex 不存在：" + a.CodexPath
		}
		return ci
	}
	if p := strings.TrimSpace(os.Getenv("ACSW_CODEX")); p != "" {
		ci := CodexInfo{Path: p, From: "ACSW_CODEX", Found: config.Exists(p)}
		if !ci.Found {
			ci.Error = "ACSW_CODEX 指向的文件不存在：" + p
		}
		return ci
	}
	p, err := exec.LookPath("codex")
	if err != nil {
		return CodexInfo{Error: "PATH 里找不到 codex，也没有设置 ACSW_CODEX"}
	}
	return CodexInfo{Path: p, From: "PATH", Found: true}
}

// EnvInfo 是网页和 status --json 顶部显示的运行环境。
type EnvInfo struct {
	Root     string    `json:"root"`
	RootFrom string    `json:"root_from"` // "env"（ACSW_ROOT）或 "default"
	Version  string    `json:"acsw_version"`
	Codex    CodexInfo `json:"codex"`
}

// Env 汇总运行环境。
func (a *App) Env() EnvInfo {
	return EnvInfo{Root: a.Paths.Root, RootFrom: config.RootFrom(), Version: a.Version, Codex: a.CodexInfo()}
}
