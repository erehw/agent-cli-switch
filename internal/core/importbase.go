package core

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"acsw/internal/config"
	"acsw/internal/exitcode"
	"acsw/internal/home"
	"acsw/internal/proc"
)

// importBaseKeys 是可以从官方 config.toml 导入的、和 provider 无关的顶层键（白名单）。
// 值是表（如 mcp_servers、projects、features）时按条目合并：官方有的条目新增或覆盖，home 自己独有的条目保留。
var importBaseKeys = map[string]bool{
	"mcp_servers": true, "features": true, "tools": true,
	"sandbox_mode": true, "sandbox_workspace_write": true, "approval_policy": true,
	"tui": true, "notify": true, "history": true, "file_opener": true, "notice": true,
	"hide_agent_reasoning": true, "show_raw_agent_reasoning": true,
	"shell_environment_policy": true, "projects": true, "profiles": true, "profile": true,
	"project_doc_max_bytes": true, "project_doc_fallback_filenames": true,
	"windows_wsl_setup_acknowledged": true, "windows": true,
	// hooks（配合 features.hooks）、personality（回复风格）也和 provider 无关。
	// 不导入：service_tier（OpenAI 专有）、plugins/marketplaces（可能依赖 ChatGPT 登录）、desktop 等未知键。
	"hooks": true, "personality": true,
}

// providerish 判断键名是否和 provider、模型或登录有关（这些永远不导入）。
func providerish(k string) bool {
	l := strings.ToLower(k)
	if strings.HasPrefix(l, "model") {
		return true
	}
	for _, s := range []string{"auth", "key", "token", "secret", "credential", "login", "password", "base_url", "chatgpt", "openai"} {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

// ImportChange 是一处会改动的键（只有键名，不含值）。
type ImportChange struct {
	Key    string `json:"key"`
	Action string `json:"action"` // add（home 里没有）/ replace（home 里有但不同）
}

// ImportSkip 是官方配置里没导入的键和原因。
type ImportSkip struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// ImportBaseResult 是 acsw home import-base 的结果。只含键名，不含任何值（mcp_servers 的 env 等可能有密钥）。
type ImportBaseResult struct {
	Home       string         `json:"home"`
	From       string         `json:"from"`   // 官方 config.toml（只读）
	Config     string         `json:"config"` // home 的 config.toml
	DryRun     bool           `json:"dry_run"`
	Changes    []ImportChange `json:"changes"`
	Skipped    []ImportSkip   `json:"skipped"`
	Unchanged  []string       `json:"unchanged"`
	Warnings   []string       `json:"warnings"`
	BackupFile string         `json:"backup_file,omitempty"`
	Written    bool           `json:"written"`
}

// ImportBase 把官方 CODEX_HOME 的 config.toml 里和 provider 无关的设置导入 home 的 config.toml。
// 官方文件只读；provider、模型、登录相关的键不导入；home 的 acsw 管理块（provider/model）原样保留；
// 写之前把原 config.toml 备份到 <根>\.acsw-backup。别名 home 拒绝；home 里有 codex 在跑时拒绝（预演可以）。
// officialDir 为空时用 %USERPROFILE%\.codex。
func (a *App) ImportBase(homeName, officialDir string, dryRun bool) (ImportBaseResult, error) {
	res := ImportBaseResult{Home: homeName, DryRun: dryRun, Changes: []ImportChange{}, Skipped: []ImportSkip{}, Unchanged: []string{}, Warnings: []string{}}
	if err := config.ValidName("home", homeName); err != nil {
		return res, exitcode.WithF(exitcode.HomeNotFound, err, exitcode.F{"home": homeName})
	}
	if officialDir == "" {
		d, err := DefaultOfficialCodexHome()
		if err != nil {
			return res, err
		}
		officialDir = d
	}
	unlock, err := a.lock()
	if err != nil {
		return res, err
	}
	defer unlock()
	m, err := config.LoadMap(a.Paths)
	if err != nil {
		return res, err
	}
	hdir := a.Paths.HomeDir(homeName)
	if !config.Exists(hdir) {
		return res, exitcode.WithF(exitcode.HomeNotFound, fmt.Errorf("home %s 不存在", homeName), exitcode.F{"home": homeName})
	}
	if alias := m.Homes[homeName].AliasOf; alias != "" {
		return res, exitcode.WithF(exitcode.ReadonlyAlias, fmt.Errorf("home %s 就是官方目录 %s 的别名，不需要也不能导入", homeName, alias), exitcode.F{"home": homeName, "alias_of": alias})
	}
	res.From = filepath.Join(officialDir, "config.toml")
	res.Config = filepath.Join(hdir, "config.toml")
	if same, _ := sameFile(res.From, res.Config); same {
		return res, exitcode.WithF(exitcode.ReadonlyAlias, fmt.Errorf("home %s 的 config.toml 就是官方的 config.toml", homeName), exitcode.F{"home": homeName})
	}
	if n := len(proc.Running(hdir, a.Proc)); n > 0 {
		if !dryRun {
			return res, exitcode.WithF(exitcode.HomeInUse, fmt.Errorf("home %s 里还有 %d 个 codex 在跑，等它们退出再导入（codex 退出时可能覆盖配置）", homeName, n), exitcode.F{"home": homeName, "running": n})
		}
		res.Warnings = append(res.Warnings, fmt.Sprintf("home %s 里还有 %d 个 codex 在跑，正式导入前要先退出", homeName, n))
	}

	ob, err := os.ReadFile(res.From) // 只读
	if err != nil {
		return res, fmt.Errorf("读不到官方 config.toml（%s）：%w", res.From, err)
	}
	var official map[string]any
	if _, err := toml.Decode(string(ob), &official); err != nil {
		return res, fmt.Errorf("官方 config.toml 格式有误：%w", err)
	}

	old := ""
	if b, err := os.ReadFile(res.Config); err == nil {
		old = string(b)
	} else if !os.IsNotExist(err) {
		return res, err
	}
	top, user, tables := home.SplitManaged(old)
	userMap := map[string]any{}
	if _, err := toml.Decode(user, &userMap); err != nil {
		return res, fmt.Errorf("home 的 config.toml 里 acsw 管理块之外的内容格式有误，请先修好：%w", err)
	}

	keys := make([]string, 0, len(official))
	for k := range official {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := official[k]
		switch {
		case providerish(k):
			res.Skipped = append(res.Skipped, ImportSkip{k, "和 provider、模型或登录有关，不导入"})
			continue
		case !importBaseKeys[k]:
			res.Skipped = append(res.Skipped, ImportSkip{k, "不在可导入的白名单里（不确定是否和 provider 无关）"})
			continue
		case k == "profile":
			continue // 最后看对应的 profile 有没有导入
		}
		if k == "profiles" {
			v = filterProfiles(v, &res)
		}
		if sub, ok := v.(map[string]any); ok {
			dst, _ := userMap[k].(map[string]any)
			if dst == nil {
				dst = map[string]any{}
			}
			names := make([]string, 0, len(sub))
			for n := range sub {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				key := k + "." + n
				if ov, ok := dst[n]; !ok {
					res.Changes = append(res.Changes, ImportChange{key, "add"})
				} else if !reflect.DeepEqual(ov, sub[n]) {
					res.Changes = append(res.Changes, ImportChange{key, "replace"})
				} else {
					res.Unchanged = append(res.Unchanged, key)
					continue
				}
				dst[n] = sub[n]
			}
			if len(dst) > 0 {
				userMap[k] = dst
			}
			continue
		}
		if ov, ok := userMap[k]; !ok {
			res.Changes = append(res.Changes, ImportChange{k, "add"})
		} else if !reflect.DeepEqual(ov, v) {
			res.Changes = append(res.Changes, ImportChange{k, "replace"})
		} else {
			res.Unchanged = append(res.Unchanged, k)
			continue
		}
		userMap[k] = v
	}
	if p, ok := official["profile"].(string); ok {
		profs, _ := userMap["profiles"].(map[string]any)
		if _, has := profs[p]; !has {
			res.Skipped = append(res.Skipped, ImportSkip{"profile", "指向的 profile 没有导入（它引用了 provider/模型）"})
		} else if userMap["profile"] != p {
			act := "add"
			if _, ok := userMap["profile"]; ok {
				act = "replace"
			}
			res.Changes = append(res.Changes, ImportChange{"profile", act})
			userMap["profile"] = p
		} else {
			res.Unchanged = append(res.Unchanged, "profile")
		}
	}
	if len(res.Changes) == 0 {
		res.Warnings = append(res.Warnings, "没有需要导入的改动")
		return res, nil
	}
	if n := strings.Count(user, "\n#") + boolInt(strings.HasPrefix(strings.TrimSpace(user), "#")); n > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("home 的 config.toml 块外有 %d 行注释，导入后不保留（原文件会先备份）", n))
	}

	var ub bytes.Buffer
	if len(userMap) > 0 {
		if err := toml.NewEncoder(&ub).Encode(userMap); err != nil {
			return res, fmt.Errorf("生成配置失败：%w", err)
		}
	}
	var parts []string
	for _, s := range []string{top, strings.TrimSpace(ub.String()), tables} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	out := strings.Join(parts, "\n\n") + "\n"

	// 校验：整体能解析；provider/model 和原来一样
	var before, after map[string]any
	if _, err := toml.Decode(old, &before); err != nil {
		before = map[string]any{}
	}
	if _, err := toml.Decode(out, &after); err != nil {
		return res, fmt.Errorf("导入后的 config.toml 解析失败（什么都没改）：%w", err)
	}
	for _, k := range []string{"model_provider", "model", "model_providers", "cli_auth_credentials_store"} {
		if !reflect.DeepEqual(before[k], after[k]) {
			return res, fmt.Errorf("导入会改变 %s，已放弃（什么都没改）", k)
		}
	}
	if dryRun {
		return res, nil
	}
	if old != "" {
		if err := os.MkdirAll(a.Paths.BackupDir(), 0o700); err != nil {
			return res, fmt.Errorf("建备份目录失败：%w", err)
		}
		res.BackupFile = filepath.Join(a.Paths.BackupDir(), time.Now().Format("20060102-150405.000")+"-home-"+homeName+"-config.toml")
		if err := os.WriteFile(res.BackupFile, []byte(old), 0o600); err != nil {
			return res, fmt.Errorf("备份 config.toml 失败（什么都没改）：%w", err)
		}
	}
	if err := config.WriteFileAtomic(res.Config, []byte(out), 0o644); err != nil {
		return res, fmt.Errorf("写 %s 失败：%w", res.Config, err)
	}
	res.Written = true
	return res, nil
}

// filterProfiles 去掉引用了 provider、模型或登录的 profile。
func filterProfiles(v any, res *ImportBaseResult) any {
	profs, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := map[string]any{}
	names := make([]string, 0, len(profs))
	for n := range profs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p, ok := profs[n].(map[string]any)
		bad := !ok
		for k := range p {
			if providerish(k) {
				bad = true
			}
		}
		if bad {
			res.Skipped = append(res.Skipped, ImportSkip{"profiles." + n, "引用了 provider、模型或登录，不导入"})
			continue
		}
		out[n] = p
	}
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func sameFile(a, b string) (bool, error) {
	sa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(sa, sb), nil
}
