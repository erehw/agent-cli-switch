package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"acsw/internal/core"
	"acsw/internal/exitcode"
)

// errStatus 把 acsw 错误映射成 HTTP 状态码，并带上 kind 方便页面判断。
func errJSON(w http.ResponseWriter, err error) {
	code := 400
	c, _ := exitcode.Of(err)
	switch c {
	case exitcode.HomeNotFound, exitcode.NotMapped:
		code = 404
	case exitcode.HomeInUse, exitcode.ReadonlyAlias, exitcode.LockBusy, exitcode.NeedConfirm:
		code = 409
	}
	kind := "other"
	if c != 0 {
		kind = c.Kind()
	} else if core.IsUsage(err) {
		kind = exitcode.BadArgs.Kind()
	}
	writeJSON(w, code, map[string]string{"error": err.Error(), "kind": kind})
}

type homeReq struct {
	Home string `json:"home"`
	Dir  string `json:"dir"`
	Off  bool   `json:"off"`
	// Confirm 为真才允许覆盖已有状态（另一个别名、在用的 source、目录里已有文件）。
	Confirm bool `json:"confirm"`
}

func (s *Server) homeAdd(w http.ResponseWriter, r *http.Request) {
	var req homeReq
	if !s.readPost(w, r, 1024, &req) {
		return
	}
	name := strings.TrimSpace(req.Home)
	if err := s.App.AddHome(name); err != nil {
		errJSON(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "home": name, "dir": s.App.Paths.HomeDir(name)})
}

// homeAlias 设置或取消官方 CODEX_HOME 别名；dir 为空时用默认的 %USERPROFILE%\.codex。只改 map.toml。
func (s *Server) homeAlias(w http.ResponseWriter, r *http.Request) {
	var req homeReq
	if !s.readPost(w, r, 4096, &req) {
		return
	}
	if req.Off {
		if err := s.App.ClearHomeAlias(req.Home); err != nil {
			errJSON(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "home": req.Home, "alias_of": ""})
		return
	}
	dir := strings.TrimSpace(req.Dir)
	if dir == "" {
		d, err := core.DefaultOfficialCodexHome()
		if err != nil {
			errJSON(w, err)
			return
		}
		dir = d
	}
	res, err := s.App.AliasHome(req.Home, dir, req.Confirm)
	if c, _ := exitcode.Of(err); c == exitcode.NeedConfirm {
		// 需要确认：409，带上现在和改后的状态，页面弹确认框后带 confirm:true 再调一次
		writeJSON(w, 409, map[string]any{"error": err.Error(), "kind": c.Kind(), "home": req.Home, "alias_of": res.AliasOf,
			"prev_alias_of": res.PrevAliasOf, "prev_source": res.PrevSource, "dir_has_files": res.DirHasFiles, "prev_state": res.PrevState()})
		return
	}
	if err != nil {
		errJSON(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "home": req.Home, "alias_of": res.AliasOf, "unchanged": res.Unchanged,
		"prev_alias_of": res.PrevAliasOf, "prev_source": res.PrevSource})
}

func (s *Server) homeCmd(w http.ResponseWriter, r *http.Request) {
	var req homeReq
	if !s.readPost(w, r, 1024, &req) {
		return
	}
	if s.AcswExe == "" {
		writeJSON(w, 500, map[string]string{"error": "不知道 acsw.exe 的路径，请在命令行运行 acsw home cmd " + req.Home})
		return
	}
	path, existed, err := s.App.WriteHomeCmd(req.Home, s.AcswExe)
	if err != nil {
		errJSON(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "path": path, "overwritten": existed})
}

// homeRm 只做默认删除：home 目录移到备份区。彻底删除（--purge）只在命令行提供。
func (s *Server) homeRm(w http.ResponseWriter, r *http.Request) {
	var req homeReq
	if !s.readPost(w, r, 1024, &req) {
		return
	}
	res, err := s.App.RemoveHome(req.Home, false)
	if err != nil {
		errJSON(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "result": res})
}

// mapProject 映射项目目录：网页上要求绝对路径且目录已存在（命令行的 acsw map 不检查存在性）。
func (s *Server) mapProject(w http.ResponseWriter, r *http.Request) {
	var req homeReq
	if !s.readPost(w, r, 4096, &req) {
		return
	}
	dir := strings.TrimSpace(req.Dir)
	if !filepath.IsAbs(dir) {
		writeJSON(w, 400, map[string]string{"error": "请填项目目录的完整路径，例如 D:\\work\\proj", "kind": exitcode.BadArgs.Kind()})
		return
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		writeJSON(w, 400, map[string]string{"error": "目录不存在：" + dir, "kind": exitcode.BadArgs.Kind()})
		return
	}
	abs, err := s.App.MapProject(dir, req.Home)
	if err != nil {
		errJSON(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "dir": abs, "home": req.Home})
}

func (s *Server) unmapProject(w http.ResponseWriter, r *http.Request) {
	var req homeReq
	if !s.readPost(w, r, 4096, &req) {
		return
	}
	res, err := s.App.UnmapProject(strings.TrimSpace(req.Dir))
	if err != nil {
		errJSON(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "result": res})
}

func (s *Server) doctor(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.App.Doctor())
}

type threadView struct {
	ID      string `json:"id"`
	Updated string `json:"updated"` // RFC3339；未知时为 ""
	Title   string `json:"title"`
	Cwd     string `json:"cwd"`
}

// last 列出某个 home 最近的会话（id、时间、标题、目录，和命令行 acsw last 显示的一样）。
func (s *Server) last(w http.ResponseWriter, r *http.Request) {
	n := 5
	if v := r.URL.Query().Get("n"); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x >= 1 && x <= 50 {
			n = x
		}
	}
	res, err := s.App.RecentInHome(r.URL.Query().Get("home"), n)
	if err != nil {
		errJSON(w, err)
		return
	}
	out := []threadView{}
	for _, t := range res.Threads {
		v := threadView{ID: t.ID, Title: t.Title, Cwd: t.Cwd}
		if !t.Updated.IsZero() {
			v.Updated = t.Updated.Format("2006-01-02T15:04:05Z07:00")
		}
		out = append(out, v)
	}
	writeJSON(w, 200, map[string]any{"home": res.Home, "note": res.Note, "threads": out})
}
