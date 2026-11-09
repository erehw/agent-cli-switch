package core

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"acsw/internal/codexdb"
	"acsw/internal/config"
	"acsw/internal/cred"
	"acsw/internal/exitcode"
	"acsw/internal/home"
	"acsw/internal/proc"
	"acsw/internal/source"
)

// ErrUsage 是用法/前置条件错误，命令行应退出码 2。
type ErrUsage struct{ Msg string }

func (e ErrUsage) Error() string { return e.Msg }

// Usage 构造 ErrUsage。
func Usage(msg string) error { return ErrUsage{Msg: msg} }

// IsUsage 判断是否为用法错误。
func IsUsage(err error) bool {
	var u ErrUsage
	return errors.As(err, &u)
}

// App 汇集外部依赖。
type App struct {
	Paths     config.Paths
	Cred      cred.Store
	Proc      proc.Checker
	CodexPath string // 为空时按 ACSW_CODEX 或 PATH 里的 codex 找
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	Environ   func() []string // 默认 os.Environ
	Hook      func(step string) error
	CI        bool // 路径比较是否忽略大小写
}

// New 用真实系统依赖建 App。
func New() (*App, error) {
	root, err := config.Root()
	if err != nil {
		return nil, err
	}
	return &App{
		Paths:  config.Paths{Root: root},
		Cred:   cred.Default(root),
		Proc:   proc.System{},
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		CI:     config.CaseInsensitiveFS(),
	}, nil
}

// lockWait 是等全局锁的时间（测试里可改短）。
var lockWait = 10 * time.Second

func (a *App) lock() (func(), error) {
	u, err := config.Lock(a.Paths, lockWait)
	if errors.Is(err, config.ErrLockBusy) {
		return nil, exitcode.With(exitcode.LockBusy, err)
	}
	return u, err
}

func (a *App) environ() []string {
	if a.Environ != nil {
		return a.Environ()
	}
	return os.Environ()
}

// Codex 返回 codex 可执行路径。
func (a *App) Codex() (string, error) {
	if a.CodexPath != "" {
		return a.CodexPath, nil
	}
	if p := strings.TrimSpace(os.Getenv("ACSW_CODEX")); p != "" {
		return p, nil
	}
	p, err := exec.LookPath("codex")
	if err != nil {
		return "", exitcode.With(exitcode.CodexNotFound, errors.New("找不到 codex 命令：请确认它在 PATH 里，或用环境变量 ACSW_CODEX 指定完整路径"))
	}
	return p, nil
}

// AddOpenAISource 建 openai source：from 非空则复制登录，否则跑 codex login。
func (a *App) AddOpenAISource(name, from string) error {
	unlock, err := a.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if from != "" {
		abs, err := filepath.Abs(from)
		if err != nil {
			return err
		}
		return source.AddOpenAIFrom(a.Paths, name, abs)
	}
	c, err := a.Codex()
	if err != nil {
		return err
	}
	return source.AddOpenAILogin(a.Paths, name, c, a.Stdin, a.Stdout, a.Stderr)
}

// AddCustomSource 建 custom source。
func (a *App) AddCustomSource(name, baseURL, model, key string) error {
	unlock, err := a.lock()
	if err != nil {
		return err
	}
	defer unlock()
	return source.AddCustom(a.Paths, a.Cred, name, baseURL, model, key)
}

// Sources 列出 source。
func (a *App) Sources() ([]source.Info, error) {
	m, err := config.LoadMap(a.Paths)
	if err != nil {
		return nil, err
	}
	return source.List(a.Paths, a.Cred, m)
}

// AddHome 建 home。
func (a *App) AddHome(name string) error {
	unlock, err := a.lock()
	if err != nil {
		return err
	}
	defer unlock()
	return home.Add(a.Paths, name)
}

// MapProject 把项目目录映射到 home。
func (a *App) MapProject(dir, homeName string) (string, error) {
	unlock, err := a.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if err := config.ValidName("home", homeName); err != nil {
		return "", err
	}
	if !config.Exists(a.Paths.HomeDir(homeName)) {
		return "", fmt.Errorf("home %q 不存在，先用 acsw home add %s", homeName, homeName)
	}
	m, err := config.LoadMap(a.Paths)
	if err != nil {
		return "", err
	}
	m.SetProject(abs, homeName)
	if _, ok := m.Homes[homeName]; !ok {
		m.Homes[homeName] = config.HomeEntry{}
	}
	return abs, config.SaveMap(a.Paths, m)
}

// Use 切换 home 的 source。
func (a *App) Use(opt home.UseOptions) (home.UseResult, error) {
	unlock, err := a.lock()
	if err != nil {
		return home.UseResult{}, err
	}
	defer unlock()
	return home.Use(home.Env{Paths: a.Paths, Proc: a.Proc, Hook: a.Hook}, opt)
}

// Status 返回所有 home 状态。
func (a *App) Status() ([]home.Status, error) { return home.StatusAll(a.Paths, a.Proc) }

// ResolveHome 按目录（或显式 home）找 home 名。
func (a *App) ResolveHome(dir, explicit string) (string, error) {
	if explicit != "" {
		if err := config.ValidName("home", explicit); err != nil {
			return "", exitcode.WithF(exitcode.HomeNotFound, err, exitcode.F{"home": explicit})
		}
		if !config.Exists(a.Paths.HomeDir(explicit)) {
			return "", exitcode.WithF(exitcode.HomeNotFound, fmt.Errorf("home %q 不存在", explicit), exitcode.F{"home": explicit})
		}
		return explicit, nil
	}
	m, err := config.LoadMap(a.Paths)
	if err != nil {
		return "", err
	}
	pr, ok := m.MatchHome(dir, a.CI)
	if !ok {
		return "", exitcode.WithF(exitcode.NotMapped, Usage("当前目录没有映射，先运行 acsw map <home>"), exitcode.F{"dir": dir})
	}
	return pr.Home, nil
}

// mappedHome 只按目录映射解析 home，不认 --home。
func (a *App) mappedHome(dir string) (string, error) {
	m, err := config.LoadMap(a.Paths)
	if err != nil {
		return "", err
	}
	pr, ok := m.MatchHome(dir, a.CI)
	if !ok {
		return "", exitcode.WithF(exitcode.NotMapped, Usage("当前目录没有映射，先运行 acsw map <home>"), exitcode.F{"dir": dir})
	}
	return pr.Home, nil
}

// RunOptions 是 run/codex 的参数。
type RunOptions struct {
	Dir        string
	Home       string // --home，显式指定
	ExpectHome string // --expect-home，目录映射必须等于它；不能用 --home 绕过
	Args       []string
}

// Run 启动 codex，返回其退出码。用法错误返回 ErrUsage（调用方应退出码 2）。
func (a *App) Run(opt RunOptions) (int, error) {
	unlock, err := a.lock()
	if err != nil {
		return 1, err
	}
	locked := true
	defer func() {
		if locked {
			unlock()
		}
	}()

	var h string
	if opt.ExpectHome != "" {
		if err := config.ValidName("home", opt.ExpectHome); err != nil {
			return 1, Usage(err.Error())
		}
		if opt.Home != "" && opt.Home != opt.ExpectHome {
			return 1, exitcode.WithF(exitcode.HomeConflict, Usage(fmt.Sprintf("--home %s 与 --expect-home %s 冲突；--expect-home 不能用 --home 绕过", opt.Home, opt.ExpectHome)), exitcode.F{"home": opt.Home, "expect_home": opt.ExpectHome, "dir": opt.Dir})
		}
		mapped, err := a.mappedHome(opt.Dir)
		if err != nil {
			if IsUsage(err) {
				return 1, exitcode.WithF(exitcode.NotMapped, Usage(fmt.Sprintf("当前目录没有映射，期望 home %s。先运行：acsw map %s", opt.ExpectHome, opt.ExpectHome)), exitcode.F{"expect_home": opt.ExpectHome, "dir": opt.Dir})
			}
			return 1, err
		}
		if mapped != opt.ExpectHome {
			return 1, exitcode.WithF(exitcode.ExpectMismatch, Usage(fmt.Sprintf("当前目录属于 %s，不是 %s", mapped, opt.ExpectHome)), exitcode.F{"home": mapped, "expect_home": opt.ExpectHome, "dir": opt.Dir})
		}
		if !config.Exists(a.Paths.HomeDir(opt.ExpectHome)) {
			return 1, exitcode.WithF(exitcode.HomeNotFound, Usage(fmt.Sprintf("home %q 不存在", opt.ExpectHome)), exitcode.F{"home": opt.ExpectHome, "dir": opt.Dir})
		}
		h = opt.ExpectHome
	} else {
		h, err = a.ResolveHome(opt.Dir, opt.Home)
		if err != nil {
			return 1, err
		}
	}

	m, err := config.LoadMap(a.Paths)
	if err != nil {
		return 1, err
	}
	srcName := m.Homes[h].Source
	ctrl := a.Paths.HomeDir(h) // acsw 自己的 home 目录：运行登记总放这里（别名 home 也一样，不写进官方目录）
	codexHome := a.Paths.CodexHome(h, m.Homes[h])
	set := map[string]string{}
	var banner string
	if alias := m.Homes[h].AliasOf; alias != "" {
		// 官方 CODEX_HOME 别名：codex 用官方目录里自己的登录，acsw 不注入 source/key，也不往官方目录写任何东西。
		if st, err := os.Stat(alias); err != nil || !st.IsDir() {
			return 1, exitcode.WithF(exitcode.HomeNotFound, fmt.Errorf("home %s 是官方目录 %s 的别名，但这个目录不存在", h, alias), exitcode.F{"home": h, "alias_of": alias, "dir": opt.Dir})
		}
		srcName = ""
		banner = fmt.Sprintf("acsw：home=%s  官方 CODEX_HOME 别名（不注入 source/key）  CODEX_HOME=%s\n", h, alias)
	} else {
		if srcName == "" {
			return 1, exitcode.WithF(exitcode.NoSource, Usage(fmt.Sprintf("home %s 还没指定 source，先运行：acsw use %s <source>", h, h)), exitcode.F{"home": h, "dir": opt.Dir})
		}
		meta, err := config.LoadSource(a.Paths, srcName)
		if err != nil {
			return 1, exitcode.WithF(exitcode.SourceInvalid, err, exitcode.F{"home": h, "source": srcName})
		}
		if meta.Type == config.ProviderCustom {
			key, err := a.Cred.Get(cred.Target(srcName))
			if err != nil {
				return 1, exitcode.WithF(exitcode.KeyUnreadable, fmt.Errorf("从%s读不到 source %s 的 key（%v）。请重新运行 acsw source add %s --type custom ...", a.Cred.Kind(), srcName, err, srcName), exitcode.F{"home": h, "source": srcName})
			}
			set[config.KeyEnv] = key
		} else if !config.Exists(filepath.Join(codexHome, "auth.json")) {
			fmt.Fprintf(a.Stderr, "acsw 注意：home %s 里没有 auth.json，codex 可能会要求登录\n", h)
		}
		banner = fmt.Sprintf("acsw：home=%s  source=%s (%s)  CODEX_HOME=%s\n", h, srcName, meta.Provider(), codexHome)
	}
	set["CODEX_HOME"] = codexHome
	codex, err := a.Codex()
	if err != nil {
		return 1, err
	}
	fmt.Fprint(a.Stderr, banner)

	ent := proc.Entry{PID: os.Getpid(), Source: srcName, Cwd: opt.Dir, Started: time.Now()}
	cleanup, err := proc.Register(ctrl, ent)
	if err != nil {
		return 1, err
	}
	defer cleanup()
	unlock()
	locked = false

	cmd := exec.Command(codex, opt.Args...)
	cmd.Dir = opt.Dir
	cmd.Env = config.ChildEnv(a.environ(), config.DropEnv, set)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = a.Stdin, a.Stdout, a.Stderr

	// Ctrl+C 交给 codex 自己处理，acsw 只等它退出。
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	go func() {
		for range sig {
		}
	}()

	if err := cmd.Start(); err != nil {
		return 1, exitcode.WithF(exitcode.CodexStart, fmt.Errorf("启动 codex 失败：%w", err), exitcode.F{"home": h, "dir": opt.Dir})
	}
	ent.ChildPID = cmd.Process.Pid
	_ = proc.Update(ctrl, ent)
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

// HomeCmdContent 生成 acsw-codex.cmd 的正文（CRLF、无 BOM）。
func HomeCmdContent(acswExe, homeName string) string {
	return fmt.Sprintf("@\"%s\" codex --expect-home %s %%*\r\n@exit /b %%ERRORLEVEL%%\r\n", acswExe, homeName)
}

// WriteHomeCmd 在 home 目录写 acsw-codex.cmd。acswExe 应为完整路径。existed 表示覆盖了已有文件。
func (a *App) WriteHomeCmd(homeName, acswExe string) (path string, existed bool, err error) {
	if err := config.ValidName("home", homeName); err != nil {
		return "", false, err
	}
	if !config.Exists(a.Paths.HomeDir(homeName)) {
		return "", false, fmt.Errorf("home %q 不存在，先用 acsw home add %s", homeName, homeName)
	}
	acswExe = strings.TrimSpace(acswExe)
	if acswExe == "" {
		return "", false, errors.New("acsw 可执行路径为空")
	}
	abs, err := filepath.Abs(acswExe)
	if err != nil {
		return "", false, err
	}
	path = filepath.Join(a.Paths.HomeDir(homeName), "acsw-codex.cmd")
	existed = config.Exists(path)
	if err := os.WriteFile(path, []byte(HomeCmdContent(abs, homeName)), 0o644); err != nil {
		return "", existed, fmt.Errorf("写 %s 失败：%w", path, err)
	}
	return path, existed, nil
}

// LastResult 是 last 的结果。
type LastResult struct {
	Home    string
	Threads []codexdb.Thread
	Note    string
}

// Last 列出当前项目最近的会话。
func (a *App) Last(dir, explicitHome string, limit int) (LastResult, error) {
	h, err := a.ResolveHome(dir, explicitHome)
	if err != nil {
		return LastResult{}, err
	}
	same := func(threadCwd, cur string) bool { return threadCwd != "" && config.UnderDir(threadCwd, cur, a.CI) }
	m, err := config.LoadMap(a.Paths)
	if err != nil {
		return LastResult{}, err
	}
	if alias := m.Homes[h].AliasOf; alias != "" {
		// 官方目录只读：状态库复制到临时目录再读，不在官方目录里打开/改动任何文件。
		ts, note, err := codexdb.RecentReadOnly(a.Paths.CodexHome(h, m.Homes[h]), dir, limit, same)
		return LastResult{Home: h, Threads: ts, Note: note}, err
	}
	ts, note, err := codexdb.Recent(a.Paths.CodexHome(h, m.Homes[h]), dir, limit, same)
	return LastResult{Home: h, Threads: ts, Note: note}, err
}
