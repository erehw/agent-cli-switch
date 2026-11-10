package core

import (
	"path/filepath"

	"acsw/internal/config"
)

// StatusJSONVersion 是 acsw status --json 的格式版本。只加字段不升版本；删改字段才升。
const StatusJSONVersion = 1

// StatusHome 是 status --json 里的一个 home。
type StatusHome struct {
	Home     string   `json:"home"`
	Source   string   `json:"source"`   // 未设置时为 ""
	Provider string   `json:"provider"` // openai / custom；未设置 source 时为 ""
	BaseURL  string   `json:"base_url,omitempty"`
	Model    string   `json:"model,omitempty"`
	HasLogin *bool    `json:"has_login"` // custom：凭据管理器里有 key；openai：source 有 auth.json；未设置 source 时为 null
	Running  int      `json:"running"`   // acsw 登记的、仍在运行的 codex 个数
	Projects []string `json:"projects"`  // 映射到这个 home 的项目目录（前缀）
	Problem  string   `json:"problem,omitempty"`
	AliasOf  string   `json:"alias_of"` // 官方 CODEX_HOME 别名时为该目录（provider 为 "official"）；普通 home 总是 ""
	Dir      string   `json:"dir"`      // codex 实际用的 CODEX_HOME（和 acsw codex 设置的相同）；会话在 <dir>\sessions
}

// StatusSource 是 status --json 里的一个 source。
type StatusSource struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"` // openai / custom
	BaseURL  string   `json:"base_url,omitempty"`
	Model    string   `json:"model,omitempty"`
	HasLogin bool     `json:"has_login"`
	UsedBy   []string `json:"used_by"`
}

// StatusReport 是 acsw status --json 的完整输出。不含任何 key 或 auth.json 内容。
type StatusReport struct {
	Version int    `json:"version"`
	Root    string `json:"root"`
	// 以下三项是后加的（version 仍为 1）
	RootFrom    string         `json:"root_from"`    // "env"（ACSW_ROOT）或 "default"
	AcswVersion string         `json:"acsw_version"` // acsw 程序版本
	Codex       CodexInfo      `json:"codex"`        // acsw codex 会用的 codex
	Homes       []StatusHome   `json:"homes"`
	Sources     []StatusSource `json:"sources"` // 只含 codex 的 source；claude source 在 claude.sources
	// 后加的（version 仍为 1）：Claude Code 的 source 和 settings 文件
	Claude ClaudeStatus `json:"claude"`
}

// StatusReport 汇总 home 和 source，供 status --json 使用。
func (a *App) StatusReport() (StatusReport, error) {
	env := a.Env()
	rep := StatusReport{Version: StatusJSONVersion, Root: a.Paths.Root, RootFrom: env.RootFrom, AcswVersion: env.Version, Codex: env.Codex, Homes: []StatusHome{}, Sources: []StatusSource{}}
	st, err := a.Status()
	if err != nil {
		return rep, err
	}
	srcs, err := a.Sources()
	if err != nil {
		return rep, err
	}
	byName := map[string]StatusSource{}
	for _, s := range srcs {
		if s.Meta.IsClaude() {
			continue
		}
		v := StatusSource{Name: s.Name, Type: s.Meta.Type, BaseURL: s.Meta.BaseURL, Model: s.Meta.Model, HasLogin: s.HasLogin, UsedBy: s.UsedBy}
		if v.UsedBy == nil {
			v.UsedBy = []string{}
		}
		byName[s.Name] = v
		rep.Sources = append(rep.Sources, v)
	}
	for _, h := range st {
		v := StatusHome{Home: h.Home, Source: h.Source, Provider: h.Provider, Running: h.Running, Projects: h.Projects, Problem: h.Problem, AliasOf: h.AliasOf, Dir: h.Dir}
		if v.Projects == nil {
			v.Projects = []string{}
		}
		if h.AliasOf != "" {
			// 只检查官方目录里 auth.json 是否存在，不读内容。
			hl := config.Exists(filepath.Join(h.AliasOf, "auth.json"))
			v.HasLogin = &hl
		} else if s, ok := byName[h.Source]; ok {
			v.BaseURL, v.Model = s.BaseURL, s.Model
			hl := s.HasLogin
			v.HasLogin = &hl
		}
		rep.Homes = append(rep.Homes, v)
	}
	if rep.Claude, err = a.ClaudeStatus(); err != nil {
		return rep, err
	}
	return rep, nil
}
