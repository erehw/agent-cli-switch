package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"acsw/internal/config"
	"acsw/internal/exitcode"
	"acsw/internal/proc"
)

// UnmapResult 是 UnmapProject 的结果。
type UnmapResult struct {
	Dir     string `json:"dir"`     // 被删掉的那条映射的目录
	Home    string `json:"home"`    // 它原来映射到的 home
	Running int    `json:"running"` // 这个目录下仍在跑的 codex 个数（不受影响，只提示）
}

// UnmapProject 删除一条项目映射。dir 必须正好是映射表里的某条目录（不按前缀找上级目录）。
// 在跑的 codex 不受影响：它启动时已经拿到 CODEX_HOME；之后在这个目录运行 acsw codex 会报未映射。
func (a *App) UnmapProject(dir string) (UnmapResult, error) {
	var res UnmapResult
	unlock, err := a.lock()
	if err != nil {
		return res, err
	}
	defer unlock()
	abs, err := filepath.Abs(dir)
	if err != nil {
		return res, err
	}
	m, err := config.LoadMap(a.Paths)
	if err != nil {
		return res, err
	}
	idx := -1
	for i, p := range m.Projects {
		if config.SamePath(p.Dir, abs, a.CI) {
			idx = i
			break
		}
	}
	if idx < 0 {
		f := exitcode.F{"dir": abs}
		msg := fmt.Sprintf("%s 不在映射表里", abs)
		if p, ok := m.MatchHome(abs, a.CI); ok {
			msg += fmt.Sprintf("（它是通过上级目录 %s 映射到 home %s 的；要取消请对那个目录运行 acsw unmap）", p.Dir, p.Home)
			f["mapped_dir"], f["home"] = p.Dir, p.Home
		}
		return res, exitcode.WithF(exitcode.NotMapped, errors.New(msg), f)
	}
	p := m.Projects[idx]
	m.Projects = append(m.Projects[:idx], m.Projects[idx+1:]...)
	if err := config.SaveMap(a.Paths, m); err != nil {
		return res, err
	}
	res = UnmapResult{Dir: p.Dir, Home: p.Home}
	for _, e := range proc.Running(a.Paths.HomeDir(p.Home), a.Proc) {
		if e.Cwd != "" && config.UnderDir(e.Cwd, p.Dir, a.CI) {
			res.Running++
		}
	}
	return res, nil
}

// RemoveHomeResult 是 RemoveHome 的结果。
type RemoveHomeResult struct {
	Home      string `json:"home"`
	AliasOf   string `json:"alias_of"`   // 别名 home：官方目录（从不改动）
	BackupDir string `json:"backup_dir"` // 非 purge：acsw 的 home 目录移到了这里
	Purged    bool   `json:"purged"`     // purge：acsw 的 home 目录已彻底删除
}

// RemoveHome 删除一个 home。前提：没有项目映射到它、没有 acsw 登记的 codex 在跑。
//   - 默认把 acsw 根目录下的 home 目录（里面有 codex 会话、config、auth 等）整个移到备份区 .acsw-backup，可以手动找回；
//   - purge 为 true 时直接删除该目录（会话一起没了）；
//   - 别名 home 只删 acsw 自己的 home 目录和 map 记录，官方目录一字节不动。
func (a *App) RemoveHome(name string, purge bool) (RemoveHomeResult, error) {
	res := RemoveHomeResult{Home: name}
	if err := config.ValidName("home", name); err != nil {
		return res, exitcode.WithF(exitcode.HomeNotFound, err, exitcode.F{"home": name})
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
	dir := a.Paths.HomeDir(name)
	entry, inMap := m.Homes[name]
	if !inMap && !config.Exists(dir) {
		return res, exitcode.WithF(exitcode.HomeNotFound, fmt.Errorf("home %s 不存在", name), exitcode.F{"home": name})
	}
	res.AliasOf = entry.AliasOf
	var projects []string
	for _, p := range m.Projects {
		if p.Home == name {
			projects = append(projects, p.Dir)
		}
	}
	if len(projects) > 0 {
		return res, exitcode.WithF(exitcode.HomeInUse,
			fmt.Errorf("home %s 还有 %d 个项目目录映射到它，先用 acsw unmap <目录> 取消：%v", name, len(projects), projects),
			exitcode.F{"home": name, "projects": projects})
	}
	if n := len(proc.Running(dir, a.Proc)); n > 0 {
		return res, exitcode.WithF(exitcode.HomeInUse, fmt.Errorf("home %s 里还有 %d 个 codex 在跑，等它们退出再删", name, n), exitcode.F{"home": name, "running": n})
	}
	if config.Exists(dir) {
		if purge {
			if err := os.RemoveAll(dir); err != nil {
				return res, fmt.Errorf("删除 %s 失败（可能已删掉一部分）：%w", dir, err)
			}
			res.Purged = true
		} else {
			if err := os.MkdirAll(a.Paths.BackupDir(), 0o700); err != nil {
				return res, fmt.Errorf("建备份目录失败：%w", err)
			}
			dst := filepath.Join(a.Paths.BackupDir(), time.Now().Format("20060102-150405.000")+"-home-"+name)
			if err := os.Rename(dir, dst); err != nil {
				return res, fmt.Errorf("把 home 目录移到备份区失败（什么都没删）：%w", err)
			}
			res.BackupDir = dst
		}
	}
	delete(m.Homes, name)
	if err := config.SaveMap(a.Paths, m); err != nil {
		if res.BackupDir != "" {
			if rerr := os.Rename(res.BackupDir, dir); rerr == nil {
				res.BackupDir = ""
			}
		}
		return res, fmt.Errorf("写 map.toml 失败：%w", err)
	}
	return res, nil
}
