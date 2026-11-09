package core

import "strings"

// LaunchCommands 生成“粘贴到终端即可在项目目录里启动 codex”的命令，页面只复制不执行：
//
//	cmd：        cd /d "<项目目录>" && acsw codex --expect-home <home>
//	PowerShell： Set-Location -LiteralPath '<项目目录>'; acsw codex --expect-home <home>
//
// 先进入项目目录再运行 acsw codex；--expect-home 核对目录映射确实是这个 home，映射被改过就拒绝启动。home 名只含字母数字._-，不需要引号。
// cd /d 在 cmd 里连盘符一起切换；PowerShell 用单引号 + -LiteralPath，$ ` [ ] 都按字面处理，单引号写两遍转义。
// warn 非空表示 cmd 形式可能出问题（目录含 %）。目录为空或含双引号、换行时两个命令都为空。
func LaunchCommands(dir, homeName string) (cmd, ps, warn string) {
	if dir == "" || strings.ContainsAny(dir, "\"\r\n") {
		return "", "", "目录为空或含双引号、换行，无法生成安全的命令"
	}
	cmd = `cd /d "` + dir + `" && acsw codex --expect-home ` + homeName
	ps = `Set-Location -LiteralPath '` + strings.ReplaceAll(dir, "'", "''") + `'; acsw codex --expect-home ` + homeName
	if strings.Contains(dir, "%") {
		warn = "目录含 %，cmd 可能把它当变量展开；建议用 PowerShell 形式"
	}
	return cmd, ps, warn
}
