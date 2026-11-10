package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"acsw/internal/core"
	"acsw/internal/source"
)

// takeMulti 取出所有 --<name> <值> / --<name>=<值>（可重复），返回这些值和剩下的参数。"--" 之后不动。
func takeMulti(args []string, name string) (vals, rest []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return vals, append(rest, args[i:]...)
		}
		if a == "--"+name || a == "-"+name {
			if i+1 < len(args) {
				vals = append(vals, args[i+1])
				i++
				continue
			}
		}
		if v, ok := strings.CutPrefix(a, "--"+name+"="); ok {
			vals = append(vals, v)
			continue
		}
		rest = append(rest, a)
	}
	return vals, rest
}

// parseEnvs 把若干 名=值 变成 map。
func parseEnvs(envs []string) (map[string]string, error) {
	if len(envs) == 0 {
		return nil, nil
	}
	m := map[string]string{}
	for _, e := range envs {
		k, v, ok := strings.Cut(e, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("--env 要写成 名=值，%q 不对", e)
		}
		m[strings.TrimSpace(k)] = v
	}
	return m, nil
}

// editClaude 处理 acsw source edit <claude source> ...。
func editClaude(app *core.App, args []string) int {
	envs, args := takeMulti(args, "env")
	f, pos, _, err := parseArgs(args, set("base-url", "model", "opus", "sonnet", "haiku", "fable", "key-mode"), set("key-stdin", "set-key", "clear-env"))
	if err != nil || len(pos) != 1 {
		return fail(core.Usage("用法：acsw source edit <claude source> [--base-url ..] [--model ..] [--opus ..] [--sonnet ..] [--haiku ..] [--fable ..] [--key-mode helper|plain] [--env 名=值 ...|--clear-env] [--set-key|--key-stdin]"))
	}
	var e source.ClaudeEdit
	ptr := func(k string) *string {
		if v, ok := f[k]; ok {
			return &v
		}
		return nil
	}
	e.BaseURL, e.Model, e.Opus, e.Sonnet, e.Haiku, e.Fable, e.KeyMode = ptr("base-url"), ptr("model"), ptr("opus"), ptr("sonnet"), ptr("haiku"), ptr("fable"), ptr("key-mode")
	if f["clear-env"] != "" {
		if len(envs) > 0 {
			return fail(core.Usage("--env 和 --clear-env 不能一起用"))
		}
		e.ExtraEnv = map[string]string{}
	} else if len(envs) > 0 {
		if e.ExtraEnv, err = parseEnvs(envs); err != nil {
			return fail(core.Usage(err.Error()))
		}
	}
	if f["set-key"] != "" || f["key-stdin"] != "" {
		if e.Key, err = readKey(f["key-stdin"] != ""); err != nil {
			return fail(err)
		}
		if e.Key == "" {
			return fail(errors.New("key 是空的，什么都没改"))
		}
	}
	res, err := app.EditClaudeSource(pos[0], e)
	if err != nil {
		return fail(err)
	}
	if res.KeyChanged {
		fmt.Println("key 已更新")
	}
	if res.MetaChanged {
		fmt.Println("设置已更新")
	}
	for _, n := range res.Notes {
		fmt.Println(n)
	}
	return 0
}

const claudeUsage = "用法：acsw claude <source> [--key-mode helper|plain] [--strict] [--] [claude 参数...]；acsw claude export <source> [--key-mode helper|plain]；acsw claude key <source>"

// parseClaudeArgs 解析 acsw claude <source> 后面的参数：开头的 --key-mode/--strict 归 acsw，
// 遇到 "--" 或第一个别的参数起全部原样交给 claude。
func parseClaudeArgs(args []string) (keyMode string, strict bool, rest []string, err error) {
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--":
			return keyMode, strict, args[i+1:], nil
		case a == "--strict":
			strict = true
			i++
		case a == "--key-mode":
			if i+1 >= len(args) {
				return "", false, nil, errors.New("--key-mode 后面缺值")
			}
			keyMode = args[i+1]
			i += 2
		case strings.HasPrefix(a, "--key-mode="):
			keyMode = strings.TrimPrefix(a, "--key-mode=")
			i++
		default:
			return keyMode, strict, args[i:], nil
		}
	}
	return keyMode, strict, nil, nil
}

func cmdClaude(app *core.App, rest []string) int {
	if len(rest) == 0 || strings.HasPrefix(rest[0], "-") {
		return fail(core.Usage(claudeUsage))
	}
	switch rest[0] {
	case "export":
		f, pos, _, err := parseArgs(rest[1:], set("key-mode"), nil)
		if err != nil || len(pos) != 1 {
			return fail(core.Usage("用法：acsw claude export <source> [--key-mode helper|plain]"))
		}
		path, mode, err := app.ExportClaude(pos[0], f["key-mode"])
		if err != nil {
			return fail(err)
		}
		if mode == "plain" {
			fmt.Fprintf(os.Stderr, "acsw 注意：%s 里有明文 key，不要提交、不要发给别人；用完可以删掉\n", path)
		}
		fmt.Println(path)
		return 0
	case "key":
		_, pos, _, err := parseArgs(rest[1:], nil, nil)
		if err != nil || len(pos) != 1 {
			return fail(core.Usage("用法：acsw claude key <source>"))
		}
		key, err := app.ClaudeKey(pos[0])
		if err != nil {
			return fail(err)
		}
		fmt.Println(key)
		return 0
	}
	keyMode, strict, args, err := parseClaudeArgs(rest[1:])
	if err != nil {
		return fail(core.Usage(err.Error()))
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	code, err := app.RunClaude(core.ClaudeRunOptions{Source: rest[0], KeyMode: keyMode, Strict: strict, Dir: cwd, Args: args})
	if err != nil {
		return fail(err)
	}
	return code
}
