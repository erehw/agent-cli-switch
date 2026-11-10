package core

import (
	"fmt"
	"os"
	"path/filepath"

	"acsw/internal/config"
	"acsw/internal/proc"
)

// DoctorCheck 是一项检查结果。Level：ok / warn / error。
type DoctorCheck struct {
	Name  string `json:"name"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

// DoctorReport 是 acsw doctor 的结果。OK 表示没有 error 级的问题。不含任何 key 或 auth.json 内容。
type DoctorReport struct {
	Version int           `json:"version"` // 格式版本，目前 1
	OK      bool          `json:"ok"`
	Env     EnvInfo       `json:"env"`
	Checks  []DoctorCheck `json:"checks"`
}

// Doctor 检查根目录、map.toml、codex、各 source 的登录/key、各 home、项目目录和运行登记。
// 只读，唯一的写操作是和 status 一样清理已退出进程留下的运行登记。
func (a *App) Doctor() (rep DoctorReport) {
	rep = DoctorReport{Version: 1, Env: a.Env(), Checks: []DoctorCheck{}}
	add := func(name, level, format string, args ...any) {
		rep.Checks = append(rep.Checks, DoctorCheck{Name: name, Level: level, Msg: fmt.Sprintf(format, args...)})
	}
	defer func() {
		rep.OK = true
		for _, c := range rep.Checks {
			if c.Level == "error" {
				rep.OK = false
			}
		}
	}()

	// 根目录
	root := a.Paths.Root
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		add("root", "warn", "根目录 %s 还不存在（来源：%s）；先运行 acsw home add / source add", root, rep.Env.RootFrom)
	} else {
		add("root", "ok", "根目录 %s（来源：%s）", root, rep.Env.RootFrom)
		if unlock, err := config.Lock(a.Paths, 0); err != nil {
			add("lock", "warn", "配置锁被占用：另一个 acsw 正在修改配置")
		} else {
			unlock()
			add("lock", "ok", "配置锁空闲")
		}
	}

	// codex
	if ci := rep.Env.Codex; ci.Found {
		add("codex", "ok", "%s（来自 %s）", ci.Path, ci.From)
	} else {
		add("codex", "error", "%s", ci.Error)
	}

	// map.toml
	m, err := config.LoadMap(a.Paths)
	if err != nil {
		add("map", "error", "%v", err)
		return rep
	}
	add("map", "ok", "map.toml：%d 个项目映射，%d 个 home 记录", len(m.Projects), len(m.Homes))

	// source
	srcs, err := a.Sources()
	if err != nil {
		add("sources", "error", "读 source 失败：%v", err)
	}
	nClaude := 0
	for _, s := range srcs {
		name := "source:" + s.Name
		if s.Meta.IsClaude() {
			nClaude++
			continue // 在下面的 claude 部分检查
		}
		switch {
		case !s.HasLogin && s.Meta.Type == config.ProviderCustom:
			add(name, "error", "custom source %s 在凭据管理器里没有 key（%s）", s.Name, "acsw/codex/"+s.Name)
		case !s.HasLogin:
			add(name, "error", "openai source %s 没有 auth.json，需要重新登录", s.Name)
		default:
			add(name, "ok", "%s source %s 登录/key 正常", s.Meta.Type, s.Name)
		}
	}

	if nClaude > 0 {
		a.doctorClaude(add)
	}

	// 运行登记：先数登记文件，再清理已退出的
	homes, _ := config.ListHomes(a.Paths)
	stale := 0
	for _, h := range homes {
		ents, _ := os.ReadDir(proc.RunDir(a.Paths.HomeDir(h)))
		alive := len(proc.Running(a.Paths.HomeDir(h), a.Proc))
		if n := len(ents) - alive; n > 0 {
			stale += n
		}
	}
	if stale > 0 {
		add("runs", "warn", "已清理 %d 条已退出进程留下的运行登记", stale)
	} else {
		add("runs", "ok", "运行登记正常")
	}

	// home
	st, err := a.Status()
	if err != nil {
		add("homes", "error", "读 home 状态失败：%v", err)
		return rep
	}
	known := map[string]bool{}
	for _, h := range st {
		known[h.Home] = true
		name := "home:" + h.Home
		switch {
		case h.Problem != "":
			add(name, "error", "%s", h.Problem)
		case h.AliasOf != "":
			if config.Exists(filepath.Join(h.AliasOf, "auth.json")) {
				add(name, "ok", "官方目录别名 %s（有 auth.json）", h.AliasOf)
			} else {
				add(name, "warn", "官方目录别名 %s 里没有 auth.json：codex 会要求登录", h.AliasOf)
			}
		case h.Source == "":
			add(name, "warn", "还没指定 source：acsw use %s <source>", h.Home)
		default:
			add(name, "ok", "source %s，目录 %s", h.Source, h.Dir)
		}
	}

	// 项目映射
	for _, p := range m.Projects {
		name := "project:" + p.Dir
		switch {
		case !known[p.Home]:
			add(name, "error", "映射到不存在的 home %s", p.Home)
		case !config.Exists(p.Dir):
			add(name, "warn", "目录不存在（→ home %s）；不再用的话 acsw unmap \"%s\"", p.Home, p.Dir)
		}
	}
	return rep
}
