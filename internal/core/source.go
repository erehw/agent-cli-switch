package core

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"acsw/internal/config"
	"acsw/internal/home"
	"acsw/internal/proc"
	"acsw/internal/source"
)

// HomeApply 是改 source 后某个 home 重新应用的结果。
type HomeApply struct {
	Home      string   `json:"home"`
	OK        bool     `json:"ok"`
	Error     string   `json:"error,omitempty"`
	Steps     []string `json:"steps,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
	BackupDir string   `json:"backup_dir,omitempty"`
}

// EditSourceResult 是 EditCustomSource 的结果。
type EditSourceResult struct {
	Name        string      `json:"name"`
	MetaChanged bool        `json:"meta_changed"`
	KeyChanged  bool        `json:"key_changed"`
	Homes       []HomeApply `json:"homes"`
	Notes       []string    `json:"notes"`
}

// ErrHomeBusy 表示要重新应用的 home 里有 codex 在跑，改 source 被拒绝。
var ErrHomeBusy = errors.New("home 里有 codex 在跑")

// HomeBusyError 说明是哪些 home 在跑 codex。
type HomeBusyError struct {
	Source string
	Busy   []string // 形如 "home1（2 个）"
}

func (e *HomeBusyError) Error() string {
	return fmt.Sprintf("没有保存：source %s 的 base_url/model 改了以后要重新应用到在用它的 home，但 home %s 里还有 codex 在跑。请先退出这些 codex 再保存（这次什么都没改，key 也没改）",
		e.Source, strings.Join(e.Busy, "、"))
}

// Is 让 errors.Is(err, ErrHomeBusy) 成立。
func (e *HomeBusyError) Is(t error) bool { return t == ErrHomeBusy }

// EditCustomSource 修改 custom source（空值表示不改）。base_url/model 变了就把在用的 home 逐个重新应用；
// 只要其中某个 home 有 acsw 登记的 codex 在跑，就整体拒绝（source、key、home 都不动）。
// 预检通过后个别 home 重新应用仍失败的，source 的修改保留，失败记在结果里。
func (a *App) EditCustomSource(name, baseURL, model, key string) (EditSourceResult, error) {
	res := EditSourceResult{Name: name, Homes: []HomeApply{}, Notes: []string{}}
	unlock, err := a.lock()
	if err != nil {
		return res, err
	}
	defer unlock()
	plan, err := source.PlanEditCustom(a.Paths, name, baseURL, model)
	if err != nil {
		return res, err
	}
	var homes []string
	if plan.MetaChanged {
		m, err := config.LoadMap(a.Paths)
		if err != nil {
			return res, err
		}
		homes = m.HomesUsingSource(name)
		var busy []string
		for _, h := range homes {
			if n := len(proc.Running(a.Paths.HomeDir(h), a.Proc)); n > 0 {
				busy = append(busy, fmt.Sprintf("%s（%d 个）", h, n))
			}
		}
		if len(busy) > 0 {
			return res, &HomeBusyError{Source: name, Busy: busy}
		}
	}
	r, err := source.EditCustom(a.Paths, a.Cred, name, baseURL, model, key)
	if err != nil {
		return res, err
	}
	res.MetaChanged, res.KeyChanged = r.MetaChanged, r.KeyChanged
	if !r.MetaChanged && !r.KeyChanged {
		res.Notes = append(res.Notes, "没有改动")
		return res, nil
	}
	if r.KeyChanged {
		res.Notes = append(res.Notes, "key 已更新（存入"+a.Cred.Kind()+"），下次启动 codex 时生效；已经在跑的 codex 不受影响")
	}
	if !r.MetaChanged {
		return res, nil
	}
	if len(homes) == 0 {
		res.Notes = append(res.Notes, "base_url/model 已保存；没有 home 在用这个 source，不需要重新应用")
		return res, nil
	}
	for _, h := range homes {
		ur, err := home.Use(home.Env{Paths: a.Paths, Proc: a.Proc, Hook: a.Hook}, home.UseOptions{Home: h, Source: name})
		ha := HomeApply{Home: h, OK: err == nil, Steps: ur.Steps, Warnings: ur.Warnings, BackupDir: ur.BackupDir}
		if err != nil {
			ha.Error = fmt.Sprintf("%v。source 的修改已保存，但这个 home 还是旧配置：处理后把 %s 重新切到 %s（acsw use %s %s）即可", err, h, name, h, name)
			ha.BackupDir = ""
		}
		res.Homes = append(res.Homes, ha)
	}
	return res, nil
}

// RemoveCustomSource 删除没人用的 custom source；目录移到 <根>/.acsw-backup/，返回该目录。
func (a *App) RemoveCustomSource(name string) (string, error) {
	unlock, err := a.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	m, err := config.LoadMap(a.Paths)
	if err != nil {
		return "", err
	}
	return source.RemoveCustom(a.Paths, a.Cred, m, name, time.Now())
}
