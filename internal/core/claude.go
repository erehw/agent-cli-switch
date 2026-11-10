package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"acsw/internal/claude"
	"acsw/internal/config"
	"acsw/internal/cred"
	"acsw/internal/exitcode"
	"acsw/internal/source"
)

// getenv 从 a.environ() 里取变量（Windows 上不分大小写）。
func (a *App) getenv(k string) string {
	ci := runtime.GOOS == "windows"
	for _, kv := range a.environ() {
		n, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if n == k || (ci && strings.EqualFold(n, k)) {
			return v
		}
	}
	return ""
}

// ClaudeInfo 返回 claude 的查找结果（和 acsw claude 用的规则相同）：App.ClaudePath、ACSW_CLAUDE、PATH 里的 claude。
func (a *App) ClaudeInfo() CodexInfo {
	if a.ClaudePath != "" {
		ci := CodexInfo{Path: a.ClaudePath, From: "app", Found: config.Exists(a.ClaudePath)}
		if !ci.Found {
			ci.Error = "指定的 claude 不存在：" + a.ClaudePath
		}
		return ci
	}
	if p := strings.TrimSpace(a.getenv("ACSW_CLAUDE")); p != "" {
		ci := CodexInfo{Path: p, From: "ACSW_CLAUDE", Found: config.Exists(p)}
		if !ci.Found {
			ci.Error = "ACSW_CLAUDE 指向的文件不存在：" + p
		}
		return ci
	}
	p, err := exec.LookPath("claude")
	if err != nil {
		return CodexInfo{Error: "PATH 里找不到 claude，也没有设置 ACSW_CLAUDE"}
	}
	return CodexInfo{Path: p, From: "PATH", Found: true}
}

func (a *App) claudeExe() (string, error) {
	ci := a.ClaudeInfo()
	if !ci.Found {
		return "", exitcode.With(exitcode.ClaudeMissing, errors.New("找不到 claude 命令："+ci.Error+"。请确认它在 PATH 里，或用环境变量 ACSW_CLAUDE 指定完整路径"))
	}
	return ci.Path, nil
}

// claudeMeta 读 claude source，把错误换成合适的退出码。
func (a *App) claudeMeta(name string) (*config.SourceMeta, error) {
	meta, err := source.LoadClaude(a.Paths, name)
	if err != nil {
		var te *source.ToolError
		if errors.As(err, &te) {
			return nil, exitcode.WithF(exitcode.ToolMismatch, fmt.Errorf("%s；acsw claude 只能用 claude source（acsw source add <名> --tool claude ...）", err), exitcode.F{"source": name, "tool": te.Got})
		}
		return nil, exitcode.WithF(exitcode.SourceInvalid, err, exitcode.F{"source": name})
	}
	return meta, nil
}

func (a *App) claudeKey(name string) (string, error) {
	t := cred.TargetFor(config.ToolClaude, name)
	key, err := a.Cred.Get(t)
	if err != nil || strings.TrimSpace(key) == "" {
		return "", exitcode.WithF(exitcode.KeyUnreadable, fmt.Errorf("从%s读不到 claude source %s 的 key（目标 %s）。可以用 acsw source edit %s --set-key 重新存入", a.Cred.Kind(), name, t, name), exitcode.F{"source": name})
	}
	return key, nil
}

// ClaudeKey 返回 claude source 的 key（给 apiKeyHelper 用：acsw claude key <source>）。
func (a *App) ClaudeKey(name string) (string, error) {
	if _, err := a.claudeMeta(name); err != nil {
		return "", err
	}
	return a.claudeKey(name)
}

// AddClaudeSource 建 claude source。
func (a *App) AddClaudeSource(name string, f source.ClaudeFields, key string) error {
	unlock, err := a.lock()
	if err != nil {
		return err
	}
	defer unlock()
	return source.AddClaude(a.Paths, a.Cred, name, f, key)
}

// ClaudeEditResult 是 EditClaudeSource 的结果。
type ClaudeEditResult struct {
	Name        string   `json:"name"`
	MetaChanged bool     `json:"meta_changed"`
	KeyChanged  bool     `json:"key_changed"`
	Notes       []string `json:"notes"`
}

// EditClaudeSource 修改 claude source。改了以后：已有的 helper 文件重新生成；已有的明文文件删掉（里面的设置或 key 已过时），需要时重新导出。
func (a *App) EditClaudeSource(name string, e source.ClaudeEdit) (ClaudeEditResult, error) {
	res := ClaudeEditResult{Name: name, Notes: []string{}}
	unlock, err := a.lock()
	if err != nil {
		return res, err
	}
	defer unlock()
	if _, err := a.claudeMeta(name); err != nil {
		return res, err
	}
	_, mc, kc, err := source.EditClaude(a.Paths, a.Cred, name, e)
	res.MetaChanged, res.KeyChanged = mc, kc
	if err != nil {
		return res, err
	}
	if !mc && !kc {
		res.Notes = append(res.Notes, "没有变化")
		return res, nil
	}
	if p := a.Paths.ClaudeSettingsFile(name, true); config.Exists(p) {
		if err := os.Remove(p); err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("旧的明文文件 %s 删不掉：%v，请手动删除", p, err))
		} else {
			res.Notes = append(res.Notes, fmt.Sprintf("已删除过时的明文文件 %s；需要时重新运行 acsw claude export %s --key-mode plain", p, name))
		}
	}
	if p := a.Paths.ClaudeSettingsFile(name, false); mc && config.Exists(p) {
		if _, _, err := a.writeClaudeSettings(name, claude.ModeHelper); err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("helper 文件 %s 没能重新生成：%v（下次 acsw claude %s 时会再生成）", p, err, name))
		} else {
			res.Notes = append(res.Notes, "已重新生成 "+p)
		}
	}
	return res, nil
}

// claudeMode 决定 key 模式：命令行 > source 的 key_mode > helper。
func claudeMode(meta *config.SourceMeta, override string) (claude.Mode, error) {
	m, err := claude.ParseMode(override)
	if err != nil {
		return "", exitcode.With(exitcode.BadArgs, Usage(err.Error()))
	}
	if m != "" {
		return m, nil
	}
	if m, _ = claude.ParseMode(meta.KeyMode); m != "" {
		return m, nil
	}
	return claude.ModeHelper, nil
}

// ClaudeSettingsContent 生成某个 claude source 在某个模式下的 settings 内容（plain 模式含明文 key，调用方不得显示）。
func (a *App) claudeSettingsContent(name string, meta *config.SourceMeta, mode claude.Mode) ([]byte, error) {
	key, err := a.claudeKey(name) // helper 模式也先确认 key 读得到，免得 claude 跑起来才发现
	if err != nil {
		return nil, err
	}
	helper := ""
	if mode == claude.ModeHelper {
		if helper, err = claude.HelperCommand(a.SelfExe, name); err != nil {
			return nil, exitcode.WithF(exitcode.Other, err, exitcode.F{"source": name})
		}
		if !config.Exists(a.SelfExe) {
			return nil, exitcode.WithF(exitcode.Other, fmt.Errorf("acsw 程序 %s 不存在，apiKeyHelper 用不了", a.SelfExe), exitcode.F{"source": name})
		}
		key = ""
	}
	b, err := claude.Build(source.ClaudeSpec(*meta), mode, key, helper)
	if err != nil {
		return nil, exitcode.WithF(exitcode.SourceInvalid, err, exitcode.F{"source": name})
	}
	return b, nil
}

// writeClaudeSettings 生成并写 settings 文件（内容没变就不写）。返回文件路径和是否写了。
func (a *App) writeClaudeSettings(name string, mode claude.Mode) (string, bool, error) {
	meta, err := a.claudeMeta(name)
	if err != nil {
		return "", false, err
	}
	dir := a.Paths.ClaudeSettingsDir()
	if strings.ContainsAny(dir, claude.UnsafePathChars) {
		return "", false, exitcode.WithF(exitcode.Other, fmt.Errorf("settings 目录 %s 含 %% ^ \" ! 之一，cmd 会改写它；请换一个数据根目录", dir), exitcode.F{"source": name})
	}
	data, err := a.claudeSettingsContent(name, meta, mode)
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", false, fmt.Errorf("建 settings 目录失败：%w", err)
	}
	if mode == claude.ModePlain {
		gi := filepath.Join(dir, ".gitignore")
		if !config.Exists(gi) {
			_ = config.WriteFileAtomic(gi, []byte("*.plain.json\n"), 0o644)
		}
	}
	p := a.Paths.ClaudeSettingsFile(name, mode == claude.ModePlain)
	if old, err := os.ReadFile(p); err == nil && bytes.Equal(old, data) {
		return p, false, nil
	}
	if err := config.WriteFileAtomic(p, data, 0o600); err != nil {
		return "", false, fmt.Errorf("写 %s 失败：%w", p, err)
	}
	return p, true, nil
}

// ExportClaude 导出 claude source 的 settings 文件，供 claude --settings <文件> 直接使用。
// mode 为空时用 source 的 key_mode。plain 文件里有明文 key，只写到固定位置 <settings 目录>\<source>.plain.json。
func (a *App) ExportClaude(name, mode string) (path string, m claude.Mode, err error) {
	meta, err := a.claudeMeta(name)
	if err != nil {
		return "", "", err
	}
	if m, err = claudeMode(meta, mode); err != nil {
		return "", "", err
	}
	path, _, err = a.writeClaudeSettings(name, m)
	return path, m, err
}

// ClaudeRunOptions 是 acsw claude <source> 的参数。
type ClaudeRunOptions struct {
	Source  string
	KeyMode string // 空：用 source 的 key_mode，默认 helper
	Strict  bool   // 用户级/项目级 settings 有会漏进来的设置时拒绝启动
	Dir     string
	Args    []string
}

// ClaudeLeaks 检查用户级和项目级 settings 里会漏进来的登录设置（只有变量名）。
func (a *App) ClaudeLeaks(dir string, mode claude.Mode) []claude.Finding {
	files := append(claude.UserSettingsFiles(claude.ConfigDir(a.getenv)), claude.ProjectSettingsFiles(dir)...)
	var out []claude.Finding
	for _, f := range claude.Scan(files) {
		if f.Leaky(mode) || f.Error != "" {
			out = append(out, f)
		}
	}
	return out
}

func describeLeak(f claude.Finding, mode claude.Mode) string {
	if f.Error != "" {
		return f.File + "：" + f.Error
	}
	var parts []string
	if len(f.Uncovered) > 0 {
		parts = append(parts, strings.Join(f.Uncovered, "、"))
	}
	if mode == claude.ModePlain && f.Helper {
		parts = append(parts, "apiKeyHelper")
	}
	return f.File + "：" + strings.Join(parts, "、")
}

// RunClaude 用 claude source 启动 claude：生成 settings 文件，再运行 claude --settings <文件> <参数>，返回 claude 的退出码。
func (a *App) RunClaude(opt ClaudeRunOptions) (int, error) {
	for _, s := range opt.Args {
		if s == "--settings" || strings.HasPrefix(s, "--settings=") {
			return 1, exitcode.WithF(exitcode.BadArgs, Usage("acsw claude 自己会传 --settings，不能再传 --settings；要用自己的 settings 文件请直接运行 claude"), exitcode.F{"source": opt.Source})
		}
	}
	meta, err := a.claudeMeta(opt.Source)
	if err != nil {
		return 1, err
	}
	mode, err := claudeMode(meta, opt.KeyMode)
	if err != nil {
		return 1, err
	}
	exe, err := a.claudeExe()
	if err != nil {
		return 1, err
	}
	path, _, err := a.writeClaudeSettings(opt.Source, mode)
	if err != nil {
		return 1, err
	}
	if leaks := a.ClaudeLeaks(opt.Dir, mode); len(leaks) > 0 {
		var lines, files []string
		for _, f := range leaks {
			lines = append(lines, describeLeak(f, mode))
			files = append(files, f.File)
		}
		msg := "这些 settings 里有 acsw 盖不住的登录设置，可能会漏进来：" + strings.Join(lines, "；")
		if opt.Strict {
			return 1, exitcode.WithF(exitcode.SettingsClash, errors.New(msg+"。去掉 --strict 可以照常启动"), exitcode.F{"source": opt.Source, "files": files})
		}
		fmt.Fprintln(a.Stderr, "acsw 注意："+msg)
	}
	if mode == claude.ModePlain {
		fmt.Fprintf(a.Stderr, "acsw 注意：plain 模式，key 以明文写在 %s\n", path)
	}
	fmt.Fprintf(a.Stderr, "acsw：claude source=%s  base_url=%s  model=%s  key=%s  settings=%s\n", opt.Source, meta.BaseURL, meta.Model, mode, path)

	cmd := exec.Command(exe, append([]string{"--settings", path}, opt.Args...)...)
	cmd.Dir = opt.Dir
	cmd.Env = config.ChildEnv(a.environ(), claude.ManagedEnv, nil)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = a.Stdin, a.Stdout, a.Stderr

	// Ctrl+C 交给 claude 自己处理，acsw 只等它退出。
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	go func() {
		for range sig {
		}
	}()
	if err := cmd.Start(); err != nil {
		return 1, exitcode.WithF(exitcode.ClaudeStart, fmt.Errorf("启动 claude 失败：%w", err), exitcode.F{"source": opt.Source, "dir": opt.Dir})
	}
	err = cmd.Wait()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}

func (a *App) claudeModels(ctx context.Context, name string, meta *config.SourceMeta) ([]byte, error) {
	key, err := a.claudeKey(name)
	if err != nil {
		return nil, err
	}
	body, err := source.FetchClaudeModels(ctx, modelsClient, meta.BaseURL, key)
	if err != nil {
		f := exitcode.F{"source": name, "url": source.ClaudeModelsURL(meta.BaseURL)}
		var he *source.ModelsHTTPError
		if errors.As(err, &he) {
			f["http_status"] = he.StatusCode
		}
		return nil, exitcode.WithF(exitcode.Upstream, &UpstreamError{Err: err}, f)
	}
	return body, nil
}

// ClaudeSettingsState 是一个 settings 文件的状态。
type ClaudeSettingsState struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Current bool   `json:"current"` // 内容和现在会生成的一致（只比较 helper 文件；plain 文件不读，总为 false）
}

// ClaudeSourceStatus 是 status --json 里的一个 claude source。不含 key。
type ClaudeSourceStatus struct {
	Name     string               `json:"name"`
	BaseURL  string               `json:"base_url"`
	Models   map[string]string    `json:"models"` // model / opus / sonnet / haiku / fable
	KeyMode  string               `json:"key_mode"`
	HasKey   bool                 `json:"has_key"`
	ExtraEnv []string             `json:"extra_env"` // 只有变量名
	Settings ClaudeSettingsState  `json:"settings"`  // helper 文件
	Plain    *ClaudeSettingsState `json:"plain,omitempty"`
	Launch   string               `json:"launch"` // 启动命令
}

// ClaudeStatus 是 status --json 的 claude 部分。
type ClaudeStatus struct {
	Claude      CodexInfo            `json:"claude"`       // acsw claude 会用的 claude
	SettingsDir string               `json:"settings_dir"` // 生成的 settings 文件目录
	ConfigDir   string               `json:"config_dir"`   // Claude Code 用户配置目录（CLAUDE_CONFIG_DIR 或 ~\.claude）
	SessionsDir string               `json:"sessions_dir"` // 会话目录：<config_dir>\projects（acsw 不改它）
	Sources     []ClaudeSourceStatus `json:"sources"`
}

// ClaudeStatus 汇总 claude source。
func (a *App) ClaudeStatus() (ClaudeStatus, error) {
	cfg := claude.ConfigDir(a.getenv)
	st := ClaudeStatus{Claude: a.ClaudeInfo(), SettingsDir: a.Paths.ClaudeSettingsDir(), ConfigDir: cfg, Sources: []ClaudeSourceStatus{}}
	if cfg != "" {
		st.SessionsDir = filepath.Join(cfg, "projects")
	}
	names, err := config.ListSources(a.Paths)
	if err != nil {
		return st, err
	}
	for _, n := range names {
		meta, err := config.LoadSource(a.Paths, n)
		if err != nil || !meta.IsClaude() {
			continue
		}
		f := source.ClaudeFieldsOf(*meta)
		v := ClaudeSourceStatus{Name: n, BaseURL: meta.BaseURL, KeyMode: f.KeyMode, ExtraEnv: []string{}, Launch: "acsw claude " + n,
			Models: map[string]string{"model": meta.Model, "opus": meta.OpusModel, "sonnet": meta.SonnetModel, "haiku": meta.HaikuModel, "fable": meta.FableModel}}
		for k := range meta.ExtraEnv {
			v.ExtraEnv = append(v.ExtraEnv, k)
		}
		sort.Strings(v.ExtraEnv)
		_, kerr := a.Cred.Get(cred.TargetFor(config.ToolClaude, n))
		v.HasKey = kerr == nil
		hp := a.Paths.ClaudeSettingsFile(n, false)
		v.Settings = ClaudeSettingsState{Path: hp, Exists: config.Exists(hp)}
		if v.Settings.Exists && a.SelfExe != "" {
			if cmdline, err := claude.HelperCommand(a.SelfExe, n); err == nil {
				if want, err := claude.Build(source.ClaudeSpec(*meta), claude.ModeHelper, "", cmdline); err == nil {
					if got, err := os.ReadFile(hp); err == nil {
						v.Settings.Current = bytes.Equal(got, want)
					}
				}
			}
		}
		if pp := a.Paths.ClaudeSettingsFile(n, true); config.Exists(pp) {
			v.Plain = &ClaudeSettingsState{Path: pp, Exists: true}
		}
		st.Sources = append(st.Sources, v)
	}
	return st, nil
}
