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

// AliasResult 是设置官方别名的结果。Prev* 是改之前的状态（需要确认时用来展示）。
type AliasResult struct {
	Home        string `json:"home"`
	AliasOf     string `json:"alias_of"`                // 新的官方目录
	Unchanged   bool   `json:"unchanged,omitempty"`     // 已经是同一个目录的别名，什么都没改
	PrevAliasOf string `json:"prev_alias_of,omitempty"` // 原来的别名目录
	PrevSource  string `json:"prev_source,omitempty"`   // 原来用的 source（设别名后不再用）
	DirHasFiles bool   `json:"dir_has_files,omitempty"` // 原 home 目录里有文件（不删，只是不再用）
}

// NeedsConfirm 表示这次会覆盖已有状态：改掉另一个别名、不再用原 source、或原 home 目录里已有文件。
func (r AliasResult) NeedsConfirm() bool {
	return !r.Unchanged && (r.PrevAliasOf != "" || r.PrevSource != "" || r.DirHasFiles)
}

// PrevState 用一句话描述改之前的状态（不含 key）。
func (r AliasResult) PrevState() string {
	switch {
	case r.PrevAliasOf != "":
		return "官方目录 " + r.PrevAliasOf + " 的别名"
	case r.PrevSource != "":
		return "普通 home，用 source " + r.PrevSource + "（home 目录保留不删）"
	case r.DirHasFiles:
		return "普通 home，目录里已有文件（保留不删）"
	}
	return "空的普通 home"
}

// SetHomeAlias 同 AliasHome(homeName, officialDir, false)，只返回官方目录。
func (a *App) SetHomeAlias(homeName, officialDir string) (string, error) {
	r, err := a.AliasHome(homeName, officialDir, false)
	return r.AliasOf, err
}

// homeDirHasFiles 判断 home 目录里有没有 acsw 运行登记以外的文件。
func homeDirHasFiles(dir string) bool {
	es, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range es {
		if filepath.Join(dir, e.Name()) != proc.RunDir(dir) {
			return true
		}
	}
	return false
}

// AliasHome 把 home 声明为官方 CODEX_HOME（officialDir）的只读别名。只改 map.toml，
// 必要时在 acsw 根目录下建 home 目录（放运行登记），不在 officialDir 里创建或修改任何东西。
//   - 已经是同一个目录的别名：什么都不写，Unchanged=true。
//   - 会覆盖已有状态（另一个别名、在用的 source、home 目录里已有文件）时，confirm 为 false 就返回
//     NeedConfirm 错误（结果里带原状态，给调用方展示）；confirm 为 true 才改。原 source 只是从 map 里去掉，
//     home 目录原样保留，取消别名后可以再 acsw use 回来。
//   - home 里有 codex 在跑：拒绝（HomeInUse）。
func (a *App) AliasHome(homeName, officialDir string, confirm bool) (AliasResult, error) {
	res := AliasResult{Home: homeName}
	if err := config.ValidName("home", homeName); err != nil {
		return res, exitcode.WithF(exitcode.HomeNotFound, err, exitcode.F{"home": homeName})
	}
	abs, err := filepath.Abs(officialDir)
	if err != nil {
		return res, err
	}
	res.AliasOf = abs
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return res, Usage(fmt.Sprintf("官方目录 %s 不存在或不是目录", abs))
	}
	if config.SamePath(abs, a.Paths.Root, a.CI) || config.UnderDir(abs, a.Paths.Root, a.CI) || config.UnderDir(a.Paths.Root, abs, a.CI) {
		return res, Usage(fmt.Sprintf("官方目录 %s 不能和 acsw 根目录 %s 互相包含", abs, a.Paths.Root))
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
	e := m.Homes[homeName]
	if e.AliasOf != "" && config.SamePath(e.AliasOf, abs, a.CI) {
		res.AliasOf = e.AliasOf
		res.Unchanged = true
		return res, nil
	}
	res.PrevAliasOf = e.AliasOf
	res.PrevSource = e.Source
	res.DirHasFiles = e.AliasOf == "" && homeDirHasFiles(a.Paths.HomeDir(homeName))
	for h, other := range m.Homes {
		if h != homeName && other.AliasOf != "" && config.SamePath(other.AliasOf, abs, a.CI) {
			return res, Usage(fmt.Sprintf("home %s 已经是 %s 的别名", h, abs))
		}
	}
	if n := len(proc.Running(a.Paths.HomeDir(homeName), a.Proc)); n > 0 {
		return res, exitcode.WithF(exitcode.HomeInUse, fmt.Errorf("home %s 里还有 %d 个 codex 在跑，请先退出", homeName, n), exitcode.F{"home": homeName})
	}
	if res.NeedsConfirm() && !confirm {
		return res, exitcode.WithF(exitcode.NeedConfirm,
			fmt.Errorf("home %s 现在是%s，改成官方目录 %s 的别名需要确认（命令行加 --yes）", homeName, res.PrevState(), abs),
			exitcode.F{"home": homeName, "alias_of": abs, "prev_alias_of": res.PrevAliasOf, "prev_source": res.PrevSource, "dir_has_files": res.DirHasFiles})
	}
	if err := os.MkdirAll(a.Paths.HomeDir(homeName), 0o700); err != nil {
		return res, err
	}
	e.AliasOf = abs
	e.Source = ""
	m.Homes[homeName] = e
	return res, config.SaveMap(a.Paths, m)
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
