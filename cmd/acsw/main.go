// acsw：agent cli switch。给 Codex 按 home 切换登录/API 来源。
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"acsw/internal/config"
	"acsw/internal/core"
	"acsw/internal/exitcode"
	"acsw/internal/home"
	"acsw/internal/web"
)

var version = "0.1.0"

const usage = `acsw %s — agent cli switch（别名 code switch）

用法：
  acsw source add <名称> --type openai [--from <现有CODEX_HOME>]
  acsw source add <名称> --type custom --base-url <URL> --model <模型> [--key-stdin]
  acsw source list
  acsw source edit <名称> [--base-url <URL>] [--model <模型>] [--set-key|--key-stdin]   # 只改 custom
  acsw source rm <名称> --yes                  # 只删 custom；有 home 在用就拒绝
  acsw source models <名称>                    # 只限 custom：GET <base_url>/models，原样输出响应体
  acsw home add <名称>
  acsw home cmd <home>                         # 生成 home\<home>\acsw-codex.cmd（给别的程序调用）
  acsw home alias <home> [官方CODEX_HOME]       # 把 home 设为官方目录的只读别名（默认 %%USERPROFILE%%\.codex）
  acsw home alias <home> --off                 # 取消别名
  acsw map <home> [项目目录]
  acsw use <home> <source> [--dry-run] [--strict]
  acsw codex [--home <home>] [--expect-home <home>] [codex 参数...]
  acsw run [codex] [...]                       # 等同 acsw codex（兼容旧用法）
  acsw last [--home <home>] [-n 5]
  acsw status [--json]                         # --json：稳定的 JSON（见 README），不含 key
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
			return fail(core.Usage("用法：acsw map <home> [项目目录]（不写目录就用当前目录）"))
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
			return fail(core.Usage("用法：acsw use <home> <source> [--dry-run] [--strict]"))
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
			return fail(core.Usage(fmt.Sprintf("还不支持工具 %q（目前只有 codex；也可用 acsw codex ...）", tool)))
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
		sf, _, _, err := parseArgs(rest, nil, set("json"))
		if err != nil {
			return fail(core.Usage("用法：acsw status [--json]"))
		}
		if sf["json"] != "" {
			return statusJSON(app, os.Stdout)
		}
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
			if s.AliasOf != "" {
				src = "（官方别名）"
			}
			fmt.Printf("%-12s %-12s %-8s %-6d %s\n", s.Home, src, s.Provider, s.Running, strings.Join(s.Projects, "; "))
			if s.AliasOf != "" {
				fmt.Println("  别名：" + s.AliasOf + "（只读，acsw 不写入）")
			}
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
		return fail(core.Usage(fmt.Sprintf("不认识的命令 %q，运行 acsw help 看用法", cmd)))
	}
}

func cmdHome(app *core.App, rest []string) int {
	if len(rest) == 0 {
		return fail(core.Usage("用法：acsw home add <名称> | acsw home cmd <home> | acsw home alias <home> [官方目录|--off]"))
	}
	switch rest[0] {
	case "add":
		if len(rest) != 2 {
			return fail(core.Usage("用法：acsw home add <名称>"))
		}
		if err := app.AddHome(rest[1]); err != nil {
			return fail(err)
		}
		fmt.Printf("已建 home %s：%s\n下一步：acsw use %s <source>\n", rest[1], app.Paths.HomeDir(rest[1]), rest[1])
		return 0
	case "alias":
		f, pos, _, err := parseArgs(rest[1:], nil, set("off"))
		if err != nil || len(pos) < 1 || len(pos) > 2 || (f["off"] != "" && len(pos) != 1) {
			return fail(core.Usage("用法：acsw home alias <home> [官方CODEX_HOME，默认 %USERPROFILE%\\.codex]  或  acsw home alias <home> --off"))
		}
		if f["off"] != "" {
			if err := app.ClearHomeAlias(pos[0]); err != nil {
				return fail(err)
			}
			fmt.Printf("已取消 home %s 的官方别名（只改了 map.toml）\n", pos[0])
			return 0
		}
		dir := ""
		if len(pos) == 2 {
			dir = pos[1]
		} else if dir, err = core.DefaultOfficialCodexHome(); err != nil {
			return fail(err)
		}
		abs, err := app.SetHomeAlias(pos[0], dir)
		if err != nil {
			return fail(err)
		}
		fmt.Printf("已设置：home %s = 官方 CODEX_HOME %s 的只读别名（只改了 map.toml，没有碰官方目录）\n", pos[0], abs)
		fmt.Printf("映射到 %s 的目录里运行 acsw codex，会直接用官方目录和它自己的登录启动 codex\n", pos[0])
		return 0
	case "cmd":
		if len(rest) != 2 {
			return fail(core.Usage("用法：acsw home cmd <home>"))
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
		return fail(core.Usage(fmt.Sprintf("不认识的 home 子命令 %q", rest[0])))
	}
}

// cmdCodex 启动 codex。提示/错误走 stderr；stdout 只留给 codex。
func cmdCodex(app *core.App, rest []string) int {
	homeFlag, expect, codexArgs, err := parseCodexArgs(rest)
	if err != nil {
		return fail(core.Usage(err.Error()))
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
		return fail(core.Usage("用法：acsw source add|list|edit|rm ..."))
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
			return fail(core.Usage("用法：acsw source add <名称> --type openai|custom ..."))
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
	case "edit":
		f, pos, _, err := parseArgs(rest[1:], set("base-url", "model"), set("key-stdin", "set-key"))
		if err != nil || len(pos) != 1 {
			return fail(core.Usage("用法：acsw source edit <名称> [--base-url <URL>] [--model <模型>] [--set-key|--key-stdin]（只支持 custom；不写的项不改）"))
		}
		key := ""
		if f["set-key"] != "" || f["key-stdin"] != "" {
			if key, err = readKey(f["key-stdin"] != ""); err != nil {
				return fail(err)
			}
			if key == "" {
				return fail(errors.New("key 是空的，什么都没改"))
			}
		}
		res, err := app.EditCustomSource(pos[0], f["base-url"], f["model"], key)
		if err != nil {
			return fail(err)
		}
		for _, n := range res.Notes {
			fmt.Println(n)
		}
		var bad []string
		for _, h := range res.Homes {
			if h.OK {
				fmt.Printf("home %s：已重新应用\n", h.Home)
			} else {
				bad = append(bad, h.Home)
				fmt.Fprintf(os.Stderr, "home %s：未应用：%s\n", h.Home, h.Error)
			}
			for _, w := range h.Warnings {
				fmt.Println("  注意：" + w)
			}
		}
		if len(bad) > 0 {
			return fail(exitcode.WithF(exitcode.Other, fmt.Errorf("source %s 已保存，但 home %s 没有重新应用（原因见上）", pos[0], strings.Join(bad, ", ")), exitcode.F{"source": pos[0], "homes": bad}))
		}
		return 0
	case "models":
		_, pos, _, err := parseArgs(rest[1:], nil, nil)
		if err != nil || len(pos) != 1 {
			return fail(core.Usage("用法：acsw source models <名称>（只支持 custom）"))
		}
		return sourceModels(app, pos[0], os.Stdout, os.Stderr)
	case "rm":
		f, pos, _, err := parseArgs(rest[1:], nil, set("yes"))
		if err != nil || len(pos) != 1 {
			return fail(core.Usage("用法：acsw source rm <名称> --yes（只支持 custom）"))
		}
		if f["yes"] == "" {
			return fail(core.Usage("删除前请确认：加 --yes。会删掉凭据管理器里的 key，目录移到 " + app.Paths.BackupDir()))
		}
		dir, err := app.RemoveCustomSource(pos[0])
		if err != nil {
			return fail(err)
		}
		fmt.Printf("已删除 source %s：key 已从%s删除，目录移到 %s\n", pos[0], app.Cred.Kind(), dir)
		return 0
	}
	return fail(core.Usage(fmt.Sprintf("不认识的 source 子命令 %q", rest[0])))
}

// readKey 读 API key：终端上不回显；否则从标准输入读一行。
// sourceModels 请求 custom source 的 <base_url>/models：成功时 stdout 只有原样的响应体，其余信息都走 stderr。
func sourceModels(app *core.App, name string, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), core.ModelsTimeout)
	defer cancel()
	body, err := app.SourceModels(ctx, name)
	if err != nil {
		return failTo(stderr, err)
	}
	if _, err := stdout.Write(body); err != nil {
		return failTo(stderr, fmt.Errorf("写出响应失败：%w", err))
	}
	return 0
}

// statusJSON 把 StatusReport 以 JSON 写到 stdout；stdout 不输出别的东西。
func statusJSON(app *core.App, w io.Writer) int {
	rep, err := app.StatusReport()
	if err != nil {
		return fail(err)
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rep); err != nil {
		return fail(err)
	}
	return 0
}

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
					return nil, nil, nil, core.Usage(fmt.Sprintf("--%s 后面缺值", k))
				}
				i++
				v = args[i]
			}
			f[k] = v
		case boolFlags[k]:
			f[k] = "1"
		default:
			return nil, nil, nil, core.Usage(fmt.Sprintf("不认识的参数 %s", a))
		}
	}
	return f, pos, nil, nil
}

// codeOf 决定 acsw 自己出错时的退出码（见 internal/exitcode）：附了码的用该码，其余用法错误为 BadArgs，别的为 Other。
func codeOf(err error) exitcode.Code {
	if c, ok := exitcode.Of(err); ok {
		return c
	}
	if core.IsUsage(err) {
		return exitcode.BadArgs
	}
	return exitcode.Other
}

// fail 在 stderr 打印 "acsw 错误：…"，返回本平台的退出码。
func fail(err error) int { return failTo(os.Stderr, err) }

// failTo 先写给人看的一行 "acsw 错误：…"，再把一行紧凑 JSON 作为 stderr 的最后一行（给程序解析），返回退出码。
// JSON 固定字段：acsw_error（实际退出码）、kind（稳定字符串）、msg（与人读行相同）；其余为可选上下文字段。
// 只在 acsw 自己出错时调用；codex 跑起来后的退出码直接透传，不经过这里。
func failTo(w io.Writer, err error) int {
	c := codeOf(err)
	code := c.Exit()
	msg := err.Error()
	fmt.Fprintln(w, "acsw 错误："+msg)
	_, _ = w.Write(errorJSON(code, c.Kind(), msg, exitcode.Fields(err)))
	return code
}

// errorJSON 生成单行 JSON（以 \n 结尾）：先 acsw_error、kind、msg，再按字母序放上下文字段。
func errorJSON(code int, kind, msg string, f exitcode.F) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(struct {
		AcswError int    `json:"acsw_error"`
		Kind      string `json:"kind"`
		Msg       string `json:"msg"`
	}{code, kind, msg})
	out := bytes.TrimRight(b.Bytes(), "\n")
	keys := make([]string, 0, len(f))
	for k := range f {
		if k != "acsw_error" && k != "kind" && k != "msg" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		out = out[:len(out)-1] // 去掉 }
		for _, k := range keys {
			var kb, vb bytes.Buffer
			ke, ve := json.NewEncoder(&kb), json.NewEncoder(&vb)
			ke.SetEscapeHTML(false)
			ve.SetEscapeHTML(false)
			if ke.Encode(k) != nil || ve.Encode(f[k]) != nil {
				continue
			}
			out = append(out, ',')
			out = append(out, bytes.TrimRight(kb.Bytes(), "\n")...)
			out = append(out, ':')
			out = append(out, bytes.TrimRight(vb.Bytes(), "\n")...)
		}
		out = append(out, '}')
	}
	return append(out, '\n')
}
