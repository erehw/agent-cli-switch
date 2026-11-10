package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"acsw/internal/core"
	"acsw/internal/exitcode"
)

// homeAlias 执行 acsw home alias <home> [dir] [--yes]。
// 会覆盖已有状态时：终端里先显示现在和改后的状态，问 y/N（默认 N）；不是终端又没加 --yes 就报 confirm_required。
func homeAlias(app *core.App, home, dir string, yes bool, in io.Reader, tty bool, out, errw io.Writer) int {
	r, err := app.AliasHome(home, dir, yes)
	if c, _ := exitcode.Of(err); c == exitcode.NeedConfirm && tty {
		fmt.Fprintf(errw, "home %s\n  现在：%s\n  改后：官方目录 %s 的只读别名（只改 map.toml）\n", home, r.PrevState(), r.AliasOf)
		if r.PrevSource != "" {
			fmt.Fprintf(errw, "  source %s 会从 map.toml 里去掉；home 目录不删，取消别名后可以 acsw use %s %s 恢复\n", r.PrevSource, home, r.PrevSource)
		}
		fmt.Fprint(errw, "确认修改？[y/N] ")
		line, _ := bufio.NewReader(in).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			fmt.Fprintln(out, "已取消，什么都没改")
			return 0
		}
		r, err = app.AliasHome(home, dir, true)
	}
	if err != nil {
		return failTo(errw, err)
	}
	if r.Unchanged {
		fmt.Fprintf(out, "home %s 已经是官方目录 %s 的别名，没有改动\n", home, r.AliasOf)
		return 0
	}
	if r.PrevAliasOf != "" {
		fmt.Fprintf(out, "已改：home %s 原来是 %s 的别名，现在是 %s 的只读别名（只改了 map.toml，没有碰官方目录）\n", home, r.PrevAliasOf, r.AliasOf)
	} else {
		fmt.Fprintf(out, "已设置：home %s = 官方 CODEX_HOME %s 的只读别名（只改了 map.toml，没有碰官方目录）\n", home, r.AliasOf)
	}
	if r.PrevSource != "" {
		fmt.Fprintf(out, "原 source %s 已从 map.toml 去掉，home 目录保留；取消别名（--off）后可以 acsw use %s %s 恢复\n", r.PrevSource, home, r.PrevSource)
	}
	fmt.Fprintf(out, "映射到 %s 的目录里运行 acsw codex，会直接用官方目录和它自己的登录启动 codex\n", home)
	return 0
}
