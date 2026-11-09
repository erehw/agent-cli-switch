// acsw：agent cli switch。给 Codex 按 home 切换登录/API 来源。
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"acsw/internal/config"
	"acsw/internal/core"
	"acsw/internal/home"
	"acsw/internal/web"
)

var version = "0.1.0"

const usage = `acsw %s — agent cli switch（别名 code switch）

用法：
  acsw source add <名称> --type openai [--from <现有CODEX_HOME>]
  acsw source add <名称> --type custom --base-url <URL> --model <模型> [--key-stdin]
  acsw source list
  acsw home add <名称>
  acsw home cmd <home>                         # 生成 home\<home>\acsw-codex.cmd（给别的程序调用）
  acsw map <home> [项目目录]
  acsw use <home> <source> [--dry-run] [--strict]
  acsw codex [--home <home>] [--expect-home <home>] [codex 参数...]
  acsw run [codex] [...]                       # 等同 acsw codex（兼容旧用法）
  acsw last [--home <home>] [-n 5]
  acsw status
  acsw ui [--port 7799] [--open]
  acsw version

根目录：%s（可用环境变量 ACSW_ROOT 改）
codex 路径：PATH 里的 codex，或环境变量 ACSW_CODEX 指定

给别的程序调用：用 acsw home cmd <home> 生成的 acsw-codex.cmd（路径里可见 home）。
自己在终端：在已映射目录里直接 acsw codex ...
`

func main() {
	os.Exit(realMain(os.Args[1:]))
}

func realMain(args []string) int {
	app, err := core.New()
	if err != nil {
		return fail(err)
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Printf(usage, version, app.Paths.Root)
		return 0
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version", "--version":
		fmt.Println("acsw", version)
		return 0
	case "source":
		return cmdSource(app, rest)
	case "home":
		return cmdHome(app, rest)
	case "map":
		_, pos, _, err := parseArgs(rest, nil, nil)
		if err == nil && len(pos) == 1 {
			wd, werr := os.Getwd()
			if werr != nil {
				return fail(werr)
			}
			pos = append(pos, wd)
		}
		if err != nil || len(pos) != 2 {
			return fail(errors.New("用法：acsw map <home> [项目目录]（不写目录就用当前目录）"))
		}
		abs, err := app.MapProject(pos[1], pos[0])
		if err != nil {
			return fail(err)
		}
		fmt.Printf("已映射：%s → %s\n", abs, pos[0])
		return 0
	case "use":
		f, pos, _, err := parseArgs(rest, nil, set("dry-run", "strict"))
		if err != nil || len(pos) != 2 {
			return fail(errors.New("用法：acsw use <home> <source> [--dry-run] [--strict]"))
		}
		dry := f["dry-run"] != ""
		res, err := app.Use(home.UseOptions{Home: pos[0], Source: pos[1], DryRun: dry, StrictCodex: f["strict"] != ""})
		if err != nil {
			for _, w := range res.Warnings {
				fmt.Fprintln(os.Stderr, "注意："+w)
			}
			return fail(err)
		}
		home.PrintResult(os.Stdout, res, dry)
		return 0
	case "codex":
		return cmdCodex(app, rest)
	case "run":
		// 预留按工具分发：acsw run [codex|claude|grok] ...；现在只实现 codex。
		if len(rest) > 0 && rest[0] == "codex" {
			rest = rest[1:]
		} else if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") && rest[0] != "--" {
			tool := rest[0]
			return fail(fmt.Errorf("还不支持工具 %q（目前只有 codex；也可用 acsw codex ...）", tool))
		}
		return cmdCodex(app, rest)
	case "last":
		f, _, _, err := parseArgs(rest, set("home", "n"), nil)
		if err != nil {
			return fail(err)
		}
		n := 5
		if f["n"] != "" {
			if n, err = strconv.Atoi(f["n"]); err != nil || n < 1 {
				return fail(errors.New("-n 要是正整数"))
			}
		}
		cwd, _ := os.Getwd()
		r, err := app.Last(cwd, f["home"], n)
		if err != nil {
			return fail(err)
		}
		fmt.Printf("home %s，目录 %s 下最近的会话：\n", r.Home, cwd)
		if r.Note != "" {
			fmt.Println("（" + r.Note + "）")
		}
		if len(r.Threads) == 0 {
			fmt.Println("  没有找到")
			return 0
		}
		for _, t := range r.Threads {
			ts := "-"
			if !t.Updated.IsZero() {
				ts = t.Updated.Local().Format("2006-01-02 15:04")
			}
			fmt.Printf("  %s  %s  %s\n", t.ID, ts, t.Title)
		}
		fmt.Printf("接续：acsw codex resume %s\n", r.Threads[0].ID)
		return 0
	case "status":
		st, err := app.Status()
		if err != nil {
			return fail(err)
		}
		if len(st) == 0 {
			fmt.Println("还没有 home。先运行：acsw home add home1")
			return 0
		}
		fmt.Printf("%-12s %-12s %-8s %-6s %s\n", "home", "source", "provider", "运行中", "项目")
		for _, s := range st {
			src := s.Source
			if src == "" {
				src = "（未设置）"
			}
			fmt.Printf("%-12s %-12s %-8s %-6d %s\n", s.Home, src, s.Provider, s.Running, strings.Join(s.Projects, "; "))
			if s.Problem != "" {
				fmt.Println("  问题：" + s.Problem)
			}
		}
		return 0
	case "ui":
		f, _, _, err := parseArgs(rest, set("port"), set("open"))
		if err != nil {
			return fail(err)
		}
		port := 7799
		if f["port"] != "" {
			if port, err = strconv.Atoi(f["port"]); err != nil || port < 1 || port > 65535 {
				return fail(errors.New("--port 不对"))
			}
		}
		return serveUI(app, port, f["open"] != "")
	default:
		return fail(fmt.Errorf("不认识的命令 %q，运行 acsw help 看用法", cmd))
	}
}

func cmdHome(app *core.App, rest []string) int {
	if len(rest) == 0 {
		return fail(errors.New("用法：acsw home add <名称>  或  acsw home cmd <home>"))
	}
	switch rest[0] {
	case "add":
		if len(rest) != 2 {
			return fail(errors.New("用法：acsw home add <名称>"))
		}
		if err := app.AddHome(rest[1]); err != nil {
			return fail(err)
		}
		fmt.Printf("已建 home %s：%s\n下一步：acsw use %s <source>\n", rest[1], app.Paths.HomeDir(rest[1]), rest[1])
		return 0
	case "cmd":
		if len(rest) != 2 {
			return fail(errors.New("用法：acsw home cmd <home>"))
		}
		exe, err := os.Executable()
		if err != nil {
			return fail(fmt.Errorf("找不到本程序路径：%w", err))
		}
		if p, err := filepath.EvalSymlinks(exe); err == nil {
			exe = p
		}
		path, existed, err := app.WriteHomeCmd(rest[1], exe)
		if err != nil {
			return fail(err)
		}
		if existed {
			fmt.Printf("已覆盖：%s\n", path)
		} else {
			fmt.Printf("已生成：%s\n", path)
		}
		return 0
	default:
		return fail(fmt.Errorf("不认识的 home 子命令 %q", rest[0]))
	}
}

// cmdCodex 启动 codex。提示/错误走 stderr；stdout 只留给 codex。
func cmdCodex(app *core.App, rest []string) int {
	homeFlag, expect, codexArgs, err := parseCodexArgs(rest)
	if err != nil {
		return fail(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	code, err := app.Run(core.RunOptions{Dir: cwd, Home: homeFlag, ExpectHome: expect, Args: codexArgs})
	if err != nil {
		return fail(err)
	}
	return code
}

func cmdSource(app *core.App, rest []string) int {
	if len(rest) == 0 {
		return fail(errors.New("用法：acsw source add|list ..."))
	}
	switch rest[0] {
	case "list":
		list, err := app.Sources()
		if err != nil {
			return fail(err)
		}
		if len(list) == 0 {
			fmt.Println("还没有 source。")
			return 0
		}
		for _, s := range list {
			login := "登录：有"
			if s.Meta.Type == config.ProviderCustom {
				login = "key：有（" + app.Cred.Kind() + "）"
			}
			if !s.HasLogin {
				login = strings.Replace(login, "有", "缺", 1)
			}
			extra := ""
			if s.Meta.Type == config.ProviderCustom {
				extra = "  " + s.Meta.BaseURL + "  model=" + s.Meta.Model
			}
			used := "未使用"
			if len(s.UsedBy) > 0 {
				used = "被 " + strings.Join(s.UsedBy, ",") + " 使用"
			}
			fmt.Printf("%-12s %-7s %s  %s%s\n", s.Name, s.Meta.Type, login, used, extra)
		}
		return 0
	case "add":
		f, pos, _, err := parseArgs(rest[1:], set("type", "from", "base-url", "model"), set("key-stdin"))
		if err != nil || len(pos) != 1 {
			return fail(errors.New("用法：acsw source add <名称> --type openai|custom ..."))
		}
		name := pos[0]
		switch f["type"] {
		case "openai":
			if err := app.AddOpenAISource(name, f["from"]); err != nil {
				return fail(err)
			}
			fmt.Printf("已建 openai source %s（登录内容不显示）\n", name)
			return 0
		case "custom":
			if f["base-url"] == "" || f["model"] == "" {
				return fail(errors.New("custom 类型需要 --base-url 和 --model"))
			}
			key, err := readKey(f["key-stdin"] != "")
			if err != nil {
				return fail(err)
			}
			if err := app.AddCustomSource(name, f["base-url"], f["model"], key); err != nil {
				return fail(err)
			}
			fmt.Printf("已建 custom source %s，key 已存入%s（目标名 acsw/codex/%s）\n", name, app.Cred.Kind(), name)
			return 0
		default:
			return fail(errors.New("--type 只能是 openai 或 custom"))
		}
	}
	return fail(fmt.Errorf("不认识的 source 子命令 %q", rest[0]))
}

// readKey 读 API key：终端上不回显；否则从标准输入读一行。
func readKey(fromStdin bool) (string, error) {
	fd := int(os.Stdin.Fd())
	if !fromStdin && term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, "输入 API key（不显示）：")
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("读 key 失败：%w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func serveUI(app *core.App, port int, open bool) int {
	s := &web.Server{App: app, Token: web.NewToken(), Port: port}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fail(fmt.Errorf("端口 %d 打不开：%w（换一个：--port 7800）", port, err))
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	fmt.Printf("acsw 网页：%s（只限本机，Ctrl+C 退出）\n", url)
	if open {
		openBrowser(url)
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fail(err)
	}
	return 0
}

func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		c = exec.Command("open", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	_ = c.Start()
}

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// parseCodexArgs 只认开头的 --home / --expect-home（可用 -- 分隔）；其余原样交给 codex。
func parseCodexArgs(args []string) (home, expect string, codexArgs []string, err error) {
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "--" {
			return home, expect, args[i+1:], nil
		}
		name, val, ok, perr := takeFlag(args, &i, "home", "expect-home")
		if perr != nil {
			return "", "", nil, perr
		}
		if ok {
			switch name {
			case "home":
				home = val
			case "expect-home":
				expect = val
			}
			continue
		}
		// 其余全部交给 codex（含 -m、resume、--version 等）
		return home, expect, args[i:], nil
	}
	return home, expect, nil, nil
}

// takeFlag 若 args[*i] 是允许的旗标之一，推进 *i 并返回名字和值。
func takeFlag(args []string, i *int, names ...string) (name, val string, ok bool, err error) {
	a := args[*i]
	for _, n := range names {
		long := "--" + n
		if a == long || a == "-"+n {
			if *i+1 >= len(args) {
				return "", "", false, fmt.Errorf("--%s 后面缺值", n)
			}
			*i += 2
			return n, args[*i-1], true, nil
		}
		pref := long + "="
		if strings.HasPrefix(a, pref) {
			*i++
			return n, a[len(pref):], true, nil
		}
	}
	return "", "", false, nil
}

// parseArgs 支持 --k v、--k=v、-k v，标志和位置参数可以穿插；"--" 之后原样返回。
func parseArgs(args []string, valueFlags, boolFlags map[string]bool) (map[string]string, []string, []string, error) {
	f := map[string]string{}
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return f, pos, args[i+1:], nil
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
			continue
		}
		k := strings.TrimLeft(a, "-")
		v, hasV := "", false
		if j := strings.Index(k, "="); j >= 0 {
			k, v, hasV = k[:j], k[j+1:], true
		}
		switch {
		case valueFlags[k]:
			if !hasV {
				if i+1 >= len(args) {
					return nil, nil, nil, fmt.Errorf("--%s 后面缺值", k)
				}
				i++
				v = args[i]
			}
			f[k] = v
		case boolFlags[k]:
			f[k] = "1"
		default:
			return nil, nil, nil, fmt.Errorf("不认识的参数 %s", a)
		}
	}
	return f, pos, nil, nil
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "acsw 错误："+err.Error())
	if core.IsUsage(err) {
		return 2
	}
	return 1
}
