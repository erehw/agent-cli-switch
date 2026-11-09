// Package home 管理 home（真正的 CODEX_HOME），核心是 Use：把 home 切到某个 source。
package home

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"acsw/internal/codexdb"
	"acsw/internal/config"
	"acsw/internal/exitcode"
	"acsw/internal/proc"
	"acsw/internal/source"
)

// Add 建一个 home 目录并登记到 map（source 为空）。调用方负责加锁。
func Add(p config.Paths, name string) error {
	if err := config.ValidName("home", name); err != nil {
		return err
	}
	dir := p.HomeDir(name)
	if config.Exists(dir) {
		return fmt.Errorf("home %q 已存在：%s", name, dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("建 home 目录失败：%w", err)
	}
	m, err := config.LoadMap(p)
	if err != nil {
		return err
	}
	if _, ok := m.Homes[name]; !ok {
		m.Homes[name] = config.HomeEntry{}
	}
	return config.SaveMap(p, m)
}

// Env 是 Use 需要的外部依赖。
type Env struct {
	Paths config.Paths
	Proc  proc.Checker
	Now   func() time.Time
	// Hook 仅供测试：每步完成后调用，返回错误即模拟失败。
	Hook func(step string) error
}

// UseOptions 是 use 的参数。
type UseOptions struct {
	Home, Source string
	DryRun       bool
	// StrictCodex 为真时，系统里有不是 acsw 登记的 codex 进程就拒绝。
	StrictCodex bool
}

// UseResult 汇报做了什么（或 dry-run 将做什么）。
type UseResult struct {
	Steps     []string
	Warnings  []string
	BackupDir string
	Changed   int64 // 改了多少条会话的 provider
}

// journal 记录改动前的文件状态，失败时倒序恢复。
type journal struct {
	dir     string
	entries []jent
	n       int
}

type jent struct {
	path   string
	backup string // 空表示原先不存在
}

func (j *journal) snapshot(path, label string) error {
	for _, e := range j.entries {
		if e.path == path {
			return nil
		}
	}
	if err := os.MkdirAll(j.dir, 0o700); err != nil {
		return fmt.Errorf("建备份目录失败：%w", err)
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		j.entries = append(j.entries, jent{path: path})
		return nil
	}
	j.n++
	bk := filepath.Join(j.dir, fmt.Sprintf("%02d-%s", j.n, label))
	if err := config.CopyFile(path, bk, 0o600); err != nil {
		return fmt.Errorf("备份 %s 失败：%w", path, err)
	}
	j.entries = append(j.entries, jent{path: path, backup: bk})
	return nil
}

func (j *journal) rollback() []error {
	var errs []error
	for i := len(j.entries) - 1; i >= 0; i-- {
		e := j.entries[i]
		var err error
		if e.backup == "" {
			err = os.Remove(e.path)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		} else {
			bk, rerr := os.ReadFile(e.backup)
			cur, cerr := os.ReadFile(e.path)
			if rerr == nil && cerr == nil && bytes.Equal(bk, cur) {
				continue // 没被改过，不用恢复
			}
			err = config.CopyFile(e.backup, e.path, 0o600)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("恢复 %s 失败：%w", e.path, err))
		}
	}
	return errs
}

func (j *journal) writeManifest() {
	var b strings.Builder
	b.WriteString("acsw 备份清单（原路径 ← 备份文件）\n")
	for _, e := range j.entries {
		bk := "（原来没有这个文件）"
		if e.backup != "" {
			bk = filepath.Base(e.backup)
		}
		fmt.Fprintf(&b, "%s ← %s\n", e.path, bk)
	}
	_ = os.WriteFile(filepath.Join(j.dir, "manifest.txt"), []byte(b.String()), 0o600)
}

// CheckIdle 检查 home 是否有 acsw 登记的进程在跑；返回告警（未登记的 codex 进程）。
func CheckIdle(p config.Paths, c proc.Checker, homeName string, strict bool) ([]string, error) {
	running := proc.Running(p.HomeDir(homeName), c)
	if len(running) > 0 {
		var ids []string
		for _, e := range running {
			ids = append(ids, fmt.Sprintf("%d", e.PID))
		}
		return nil, fmt.Errorf("home %s 里还有 %d 个 codex 在跑（acsw 进程号 %s），请先全部退出再切换", homeName, len(running), strings.Join(ids, ","))
	}
	var warns []string
	known := map[int]bool{}
	homes, _ := config.ListHomes(p)
	for _, h := range homes {
		for _, e := range proc.Running(p.HomeDir(h), c) {
			known[e.PID] = true
			if e.ChildPID > 0 {
				known[e.ChildPID] = true
			}
		}
	}
	unknown := proc.UnmanagedCodex(c, known)
	if len(unknown) > 0 {
		msg := fmt.Sprintf("系统里有 %d 个 codex 进程不是 acsw run 启动的（%v），acsw 判断不了它们用哪个 CODEX_HOME；请确认它们没有在用 home %s", len(unknown), unknown, homeName)
		if strict {
			return nil, errors.New(msg + "（--strict 模式下拒绝切换）")
		}
		warns = append(warns, msg)
	}
	return warns, nil
}

// ProviderOfficial 是官方 CODEX_HOME 别名 home 在状态里显示的 provider。
const ProviderOfficial = "official"

// ReadonlyAliasError 是对官方别名 home 做写操作时的统一错误（退出码 99994008）。
func ReadonlyAliasError(homeName, alias string) error {
	return exitcode.WithF(exitcode.ReadonlyAlias,
		fmt.Errorf("home %s 是官方目录 %s 的只读别名，acsw 不会改它的登录、配置或 source。要切 source 请用别的 home；确实要取消别名：acsw home alias %s --off", homeName, alias, homeName),
		exitcode.F{"home": homeName, "alias_of": alias})
}

// Use 把 home 切到 source。调用方负责持有全局锁。
func Use(env Env, opt UseOptions) (res UseResult, err error) {
	p := env.Paths
	now := time.Now
	if env.Now != nil {
		now = env.Now
	}
	hook := func(s string) error {
		if env.Hook != nil {
			return env.Hook(s)
		}
		return nil
	}
	if err := config.ValidName("home", opt.Home); err != nil {
		return res, err
	}
	hdir := p.HomeDir(opt.Home)
	if !config.Exists(hdir) {
		return res, fmt.Errorf("home %q 不存在，先用 acsw home add %s", opt.Home, opt.Home)
	}
	newMeta, err := config.LoadSource(p, opt.Source)
	if err != nil {
		return res, err
	}
	m, err := config.LoadMap(p)
	if err != nil {
		return res, err
	}
	if alias := m.Homes[opt.Home].AliasOf; alias != "" {
		return res, ReadonlyAliasError(opt.Home, alias)
	}
	oldName := m.Homes[opt.Home].Source
	var oldMeta *config.SourceMeta
	if oldName != "" {
		if oldMeta, err = config.LoadSource(p, oldName); err != nil {
			return res, fmt.Errorf("home %s 当前记录的 source %q 读不出来：%w", opt.Home, oldName, err)
		}
	}

	// 1. home 是否空闲
	warns, err := CheckIdle(p, env.Proc, opt.Home, opt.StrictCodex)
	if err != nil {
		return res, err
	}
	res.Warnings = append(res.Warnings, warns...)

	// 2. 同一个 openai source 不能同时给两个 home
	if newMeta.Type == config.ProviderOpenAI {
		for _, h := range m.HomesUsingSource(opt.Source) {
			if h != opt.Home {
				return res, fmt.Errorf("source %s 是订阅登录，正被 home %s 使用；同一个订阅同时给两个 home 会互相刷掉登录。请先把 %s 切到别的 source（acsw use %s <其他source>）", opt.Source, h, h, h)
			}
		}
		if !config.Exists(source.AuthFile(p, opt.Source)) {
			return res, fmt.Errorf("source %s 没有 auth.json，先重新登录：acsw source add 重建，或把登录复制进 %s", opt.Source, p.SourceDir(opt.Source))
		}
	}

	homeAuth := filepath.Join(hdir, "auth.json")
	homeCfg := filepath.Join(hdir, "config.toml")
	db, dbExists := codexdb.FindStateDB(hdir)
	provider := newMeta.Provider()

	// 预先算好新 config，出错就不动任何文件。
	oldCfg, _ := os.ReadFile(homeCfg)
	newCfg, err := RenderConfig(string(oldCfg), *newMeta)
	if err != nil {
		return res, err
	}

	// 预检：存回的登录是否同一账号。
	saveBack := oldMeta != nil && oldMeta.Type == config.ProviderOpenAI && config.Exists(homeAuth)
	if saveBack {
		hb, _ := os.ReadFile(homeAuth)
		sb, _ := os.ReadFile(source.AuthFile(p, oldName))
		ha, sa := source.AccountID(hb), source.AccountID(sb)
		if !source.LooksLikeChatGPTAuth(hb) {
			saveBack = false
			res.Warnings = append(res.Warnings, "home 里的 auth.json 不像 ChatGPT 登录，不存回 source "+oldName+"（会移到备份目录）")
		} else if ha != "" && sa != "" && ha != sa {
			return res, fmt.Errorf("home %s 里的登录和 source %s 不是同一个账号，为安全起见不覆盖。请手工确认后再切换", opt.Home, oldName)
		} else if bytes.Equal(hb, sb) {
			saveBack = false
		}
	}

	var toRetag int64
	if dbExists {
		r, err := codexdb.CountToRetag(db, provider)
		if err != nil {
			return res, err
		}
		toRetag = r.Changed
		if r.Skipped {
			res.Warnings = append(res.Warnings, "状态库 "+filepath.Base(db)+"："+r.Reason+"，不改会话 provider")
		}
	}

	// 计划
	oldDesc := "（未设置）"
	if oldMeta != nil {
		oldDesc = oldName + " (" + oldMeta.Provider() + ")"
	}
	res.Steps = append(res.Steps, fmt.Sprintf("home %s：%s → %s (%s)", opt.Home, oldDesc, opt.Source, provider))
	if saveBack {
		res.Steps = append(res.Steps, fmt.Sprintf("把 home 里刷新过的登录存回 source %s", oldName))
	}
	if provider == config.ProviderOpenAI {
		res.Steps = append(res.Steps, fmt.Sprintf("放入 source %s 的登录 auth.json", opt.Source))
	} else if config.Exists(homeAuth) {
		res.Steps = append(res.Steps, "把 home 里的 auth.json 移到备份目录（custom 不用登录文件）")
	}
	res.Steps = append(res.Steps, "更新 config.toml 里 acsw 管理的配置块（其他内容保留）")
	if toRetag > 0 {
		res.Steps = append(res.Steps, fmt.Sprintf("备份状态库，再把 %d 条会话的 model_provider 改成 %s", toRetag, provider))
	}
	res.Steps = append(res.Steps, "更新 map.toml")
	if opt.DryRun {
		res.Changed = toRetag
		return res, nil
	}

	// 执行（事务式）
	j := &journal{dir: filepath.Join(hdir, ".acsw-backup", now().Format("20060102-150405.000"))}
	res.BackupDir = j.dir
	defer func() {
		if err != nil {
			errs := j.rollback()
			if len(errs) == 0 {
				err = fmt.Errorf("%w（已回滚，原文件都恢复了；备份在 %s）", err, j.dir)
			} else {
				err = fmt.Errorf("%w；回滚时又出错：%v，请按 %s 里的 manifest.txt 手工恢复", err, errs, j.dir)
			}
			j.writeManifest()
			res.Changed = 0
		}
	}()

	step := func(name string, f func() error) error {
		if e := f(); e != nil {
			return e
		}
		return hook(name)
	}

	if saveBack {
		if err = step("saveback", func() error {
			if e := j.snapshot(source.AuthFile(p, oldName), "source-"+oldName+"-auth.json"); e != nil {
				return e
			}
			return config.CopyFile(homeAuth, source.AuthFile(p, oldName), 0o600)
		}); err != nil {
			return res, fmt.Errorf("存回登录失败：%w", err)
		}
	}
	if err = step("auth", func() error {
		if e := j.snapshot(homeAuth, "home-auth.json"); e != nil {
			return e
		}
		if provider == config.ProviderOpenAI {
			return config.CopyFile(source.AuthFile(p, opt.Source), homeAuth, 0o600)
		}
		e := os.Remove(homeAuth)
		if errors.Is(e, os.ErrNotExist) {
			e = nil
		}
		return e
	}); err != nil {
		return res, fmt.Errorf("放入登录失败：%w", err)
	}
	if err = step("config", func() error {
		if e := j.snapshot(homeCfg, "config.toml"); e != nil {
			return e
		}
		return config.WriteFileAtomic(homeCfg, []byte(newCfg), 0o644)
	}); err != nil {
		return res, fmt.Errorf("写 config.toml 失败：%w", err)
	}
	if toRetag > 0 {
		if err = step("db", func() error {
			for _, f := range codexdb.DBFiles(db) {
				if e := j.snapshot(f, filepath.Base(f)); e != nil {
					return e
				}
			}
			r, e := codexdb.Retag(db, provider)
			res.Changed = r.Changed
			return e
		}); err != nil {
			return res, fmt.Errorf("改状态库失败：%w", err)
		}
	}
	if err = step("map", func() error {
		if e := j.snapshot(p.MapFile(), "map.toml"); e != nil {
			return e
		}
		m.Homes[opt.Home] = config.HomeEntry{Source: opt.Source, Updated: now()}
		return config.SaveMap(p, m)
	}); err != nil {
		return res, fmt.Errorf("写 map.toml 失败：%w", err)
	}
	j.writeManifest()
	return res, nil
}

// Status 是一个 home 的状态。
type Status struct {
	Home     string   `json:"home"`
	Source   string   `json:"source"`
	Provider string   `json:"provider"`
	Running  int      `json:"running"`
	Projects []string `json:"projects"`
	Problem  string   `json:"problem,omitempty"`
	AliasOf  string   `json:"alias_of"` // 官方 CODEX_HOME 别名时为该目录（此时 Provider 为 "official"）；普通 home 为 ""
	Dir      string   `json:"dir"`      // codex 实际用的 CODEX_HOME（config.Paths.CodexHome）；会话在 <dir>\sessions
}

// StatusAll 汇总所有 home。
func StatusAll(p config.Paths, c proc.Checker) ([]Status, error) {
	m, err := config.LoadMap(p)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	dirs, _ := config.ListHomes(p)
	for _, h := range dirs {
		names[h] = true
	}
	for h := range m.Homes {
		names[h] = true
	}
	var out []Status
	for h := range names {
		st := Status{Home: h, Source: m.Homes[h].Source, AliasOf: m.Homes[h].AliasOf, Dir: p.CodexHome(h, m.Homes[h])}
		if !config.Exists(p.HomeDir(h)) {
			st.Problem = "home 目录不存在"
		}
		if st.AliasOf != "" {
			st.Provider = ProviderOfficial
			if fi, err := os.Stat(st.AliasOf); err != nil || !fi.IsDir() {
				st.Problem = "官方目录不存在：" + st.AliasOf
			}
		} else if st.Source != "" {
			if meta, err := config.LoadSource(p, st.Source); err == nil {
				st.Provider = meta.Provider()
			} else {
				st.Problem = err.Error()
			}
		}
		st.Running = len(proc.Running(p.HomeDir(h), c))
		for _, pr := range m.Projects {
			if pr.Home == h {
				st.Projects = append(st.Projects, pr.Dir)
			}
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Home < out[j].Home })
	return out, nil
}

// PrintResult 输出 use 的结果。
func PrintResult(w io.Writer, r UseResult, dry bool) {
	if dry {
		fmt.Fprintln(w, "【预演，不会改任何文件】将要：")
	} else {
		fmt.Fprintln(w, "已完成：")
	}
	for i, s := range r.Steps {
		fmt.Fprintf(w, "  %d. %s\n", i+1, s)
	}
	for _, s := range r.Warnings {
		fmt.Fprintln(w, "  注意："+s)
	}
	if r.BackupDir != "" && !dry {
		fmt.Fprintln(w, "  备份目录："+r.BackupDir)
	}
}
