package core

import (
	"fmt"
	"os"
	"path/filepath"

	"acsw/internal/config"
	"acsw/internal/exitcode"
	"acsw/internal/proc"
)

// DefaultOfficialCodexHome 返回官方 CODEX_HOME 的默认位置：%USERPROFILE%\.codex。
func DefaultOfficialCodexHome() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".codex"), nil
}

// SetHomeAlias 把 home 声明为官方 CODEX_HOME（officialDir）的只读别名。只改 map.toml，
// 必要时在 acsw 根目录下建 home 目录（放运行登记），不在 officialDir 里创建或修改任何东西。
func (a *App) SetHomeAlias(homeName, officialDir string) (string, error) {
	if err := config.ValidName("home", homeName); err != nil {
		return "", exitcode.WithF(exitcode.HomeNotFound, err, exitcode.F{"home": homeName})
	}
	abs, err := filepath.Abs(officialDir)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return "", Usage(fmt.Sprintf("官方目录 %s 不存在或不是目录", abs))
	}
	if config.SamePath(abs, a.Paths.Root, a.CI) || config.UnderDir(abs, a.Paths.Root, a.CI) || config.UnderDir(a.Paths.Root, abs, a.CI) {
		return "", Usage(fmt.Sprintf("官方目录 %s 不能和 acsw 根目录 %s 互相包含", abs, a.Paths.Root))
	}
	unlock, err := a.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	m, err := config.LoadMap(a.Paths)
	if err != nil {
		return "", err
	}
	e := m.Homes[homeName]
	if e.Source != "" {
		return "", Usage(fmt.Sprintf("home %s 正在用 source %s，不能直接改成官方别名；请换一个没用过的 home 名，或先手工清掉它在 map.toml 里的 source", homeName, e.Source))
	}
	for h, other := range m.Homes {
		if h != homeName && other.AliasOf != "" && config.SamePath(other.AliasOf, abs, a.CI) {
			return "", Usage(fmt.Sprintf("home %s 已经是 %s 的别名", h, abs))
		}
	}
	if n := len(proc.Running(a.Paths.HomeDir(homeName), a.Proc)); n > 0 {
		return "", fmt.Errorf("home %s 里还有 %d 个 codex 在跑，请先退出", homeName, n)
	}
	if err := os.MkdirAll(a.Paths.HomeDir(homeName), 0o700); err != nil {
		return "", err
	}
	e.AliasOf = abs
	m.Homes[homeName] = e
	return abs, config.SaveMap(a.Paths, m)
}

// ClearHomeAlias 取消 home 的官方别名（只改 map.toml）。
func (a *App) ClearHomeAlias(homeName string) error {
	unlock, err := a.lock()
	if err != nil {
		return err
	}
	defer unlock()
	m, err := config.LoadMap(a.Paths)
	if err != nil {
		return err
	}
	e, ok := m.Homes[homeName]
	if !ok || e.AliasOf == "" {
		return Usage(fmt.Sprintf("home %s 不是官方别名", homeName))
	}
	if n := len(proc.Running(a.Paths.HomeDir(homeName), a.Proc)); n > 0 {
		return fmt.Errorf("home %s 里还有 %d 个 codex 在跑，请先退出", homeName, n)
	}
	e.AliasOf = ""
	m.Homes[homeName] = e
	return config.SaveMap(a.Paths, m)
}
