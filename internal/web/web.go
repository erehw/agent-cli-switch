// Package web 是 acsw ui 的本机网页：只监听 127.0.0.1，调用和命令行同一个 core。
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"net"
	"net/http"
	"strings"
	"time"

	"acsw/internal/core"
	"acsw/internal/home"
	"acsw/internal/source"
)

//go:embed static/index.html
var static embed.FS

var page = template.Must(template.ParseFS(static, "static/index.html"))

// NewToken 生成随机 CSRF token。
func NewToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Server 是网页处理器。
type Server struct {
	App     *core.App
	Token   string
	Port    int
	AcswExe string // 生成 acsw-codex.cmd 时写进去的 acsw.exe 完整路径
	// OfficialDir 是导入基础配置时读取的官方 CODEX_HOME；空时用 %USERPROFILE%\.codex（测试里指向假目录）。
	OfficialDir string
}

type sourceView struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	BaseURL  string   `json:"base_url,omitempty"`
	Model    string   `json:"model,omitempty"`
	HasLogin bool     `json:"has_login"`
	UsedBy   []string `json:"used_by"`
}

// Handler 返回 http.Handler。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /api/status", s.needToken(s.status))
	mux.HandleFunc("POST /api/use", s.use)
	mux.HandleFunc("POST /api/source/add", s.sourceAdd)
	mux.HandleFunc("POST /api/source/edit", s.sourceEdit)
	mux.HandleFunc("POST /api/source/delete", s.sourceDelete)
	mux.HandleFunc("POST /api/source/models", s.sourceModels)
	mux.HandleFunc("POST /api/models/probe", s.modelsProbe)
	mux.HandleFunc("POST /api/claude/add", s.claudeAdd)
	mux.HandleFunc("POST /api/claude/edit", s.claudeEdit)
	mux.HandleFunc("POST /api/claude/export", s.claudeExport)
	mux.HandleFunc("POST /api/home/add", s.homeAdd)
	mux.HandleFunc("POST /api/home/alias", s.homeAlias)
	mux.HandleFunc("POST /api/home/cmd", s.homeCmd)
	mux.HandleFunc("POST /api/home/rm", s.homeRm)
	mux.HandleFunc("POST /api/home/import-base", s.homeImportBase)
	mux.HandleFunc("POST /api/map", s.mapProject)
	mux.HandleFunc("POST /api/unmap", s.unmapProject)
	mux.HandleFunc("GET /api/doctor", s.needToken(s.doctor))
	mux.HandleFunc("GET /api/last", s.needToken(s.last))
	return s.guard(mux)
}

// guard 防 DNS 重绑定：只接受 Host 为 127.0.0.1/localhost 的请求。
// 带 Origin 头的请求（浏览器跨源请求、同源 POST 都会带）还要求 Origin 正好是 http://<Host>，挡住别的网页发来的请求。
// 没有 Origin 的请求（命令行工具、同源 GET）照常处理；所有 /api/ 请求另外都要 X-ACSW-Token。
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "只允许本机访问", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
			http.Error(w, "跨源请求被拒绝", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// needToken 要求 GET 接口也带 X-ACSW-Token（页面里嵌着 token，别的进程/网页拿不到）。
func (s *Server) needToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.tokenOK(r) {
			writeJSON(w, 403, map[string]string{"error": "token 不对，请刷新页面"})
			return
		}
		next(w, r)
	}
}

func (s *Server) tokenOK(r *http.Request) bool {
	return s.Token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-ACSW-Token")), []byte(s.Token)) == 1
}

// HTTPServer 返回带超时设置的 http.Server。最慢的接口是查模型（上游 ModelsTimeout），写超时留出余量；没有长轮询接口。
func (s *Server) HTTPServer() *http.Server {
	return &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      core.ModelsTimeout + 15*time.Second,
		IdleTimeout:       2 * time.Minute,
	}
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = page.Execute(w, map[string]any{"Token": s.Token})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// readPost 统一校验 POST：token、JSON 类型、请求体大小，再解码。失败时已写好响应，返回 false。
func (s *Server) readPost(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	if !s.tokenOK(r) {
		writeJSON(w, 403, map[string]string{"error": "token 不对，请刷新页面"})
		return false
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeJSON(w, 415, map[string]string{"error": "需要 JSON"})
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(v); err != nil {
		writeJSON(w, 400, map[string]string{"error": "请求格式不对"})
		return false
	}
	return true
}

// launchView 是某个项目目录的启动命令（只给用户复制，页面不会执行它）。
type launchView struct {
	Dir  string `json:"dir"`
	Cmd  string `json:"cmd"` // cmd 形式
	PS   string `json:"ps"`  // PowerShell 形式
	Warn string `json:"warn,omitempty"`
}

// homeView 在 home.Status 之外附上每个项目目录的启动命令。
type homeView struct {
	home.Status
	Launches []launchView `json:"launches"`
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	st, err := s.App.Status()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	srcs, err := s.App.Sources()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	var sv []sourceView
	for _, x := range srcs {
		if x.Meta.IsClaude() {
			continue // claude source 单独放在 claude 部分，不出现在 codex 的切换列表里
		}
		sv = append(sv, sourceView{Name: x.Name, Type: x.Meta.Type, BaseURL: x.Meta.BaseURL, Model: x.Meta.Model, HasLogin: x.HasLogin, UsedBy: x.UsedBy})
	}
	hv := []homeView{}
	for _, h := range st {
		v := homeView{Status: h, Launches: []launchView{}}
		for _, d := range h.Projects {
			c, ps, warn := core.LaunchCommands(d, h.Home)
			v.Launches = append(v.Launches, launchView{Dir: d, Cmd: c, PS: ps, Warn: warn})
		}
		hv = append(hv, v)
	}
	if sv == nil {
		sv = []sourceView{}
	}
	cs, err := s.App.ClaudeStatus()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"env": s.App.Env(), "homes": hv, "sources": sv, "claude": cs})
}

func (s *Server) use(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Home   string `json:"home"`
		Source string `json:"source"`
		DryRun bool   `json:"dry_run"`
	}
	if !s.readPost(w, r, 4096, &req) {
		return
	}
	res, err := s.App.Use(home.UseOptions{Home: req.Home, Source: req.Source, DryRun: req.DryRun})
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": err.Error(), "steps": res.Steps})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "dry_run": req.DryRun, "steps": res.Steps, "warnings": res.Warnings, "backup_dir": res.BackupDir, "changed": res.Changed})
}

// sourceReq 是新增/编辑 custom source 的请求。key 只进凭据管理器，任何响应都不带它。
type sourceReq struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	Key     string `json:"key"`
}

const sourceBodyLimit = 8192

func (s *Server) sourceAdd(w http.ResponseWriter, r *http.Request) {
	var req sourceReq
	if !s.readPost(w, r, sourceBodyLimit, &req) {
		return
	}
	if err := s.App.AddCustomSource(strings.TrimSpace(req.Name), req.BaseURL, req.Model, req.Key); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "name": strings.TrimSpace(req.Name), "notes": []string{"已建 custom source，key 已存入" + s.App.Cred.Kind()}})
}

func (s *Server) sourceEdit(w http.ResponseWriter, r *http.Request) {
	var req sourceReq
	if !s.readPost(w, r, sourceBodyLimit, &req) {
		return
	}
	res, err := s.App.EditCustomSource(req.Name, req.BaseURL, req.Model, req.Key)
	if err != nil {
		code := 400
		if errors.Is(err, core.ErrHomeBusy) {
			code = 409
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	allOK := true
	for _, h := range res.Homes {
		allOK = allOK && h.OK
	}
	writeJSON(w, 200, map[string]any{"ok": true, "all_homes_ok": allOK, "result": res})
}

// sourceModels 代为请求 custom source 的 <base_url>/models，成功时原样返回上游响应体；key 不出 acsw。
func (s *Server) sourceModels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !s.readPost(w, r, 1024, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), core.ModelsTimeout)
	defer cancel()
	body, err := s.App.SourceModels(ctx, strings.TrimSpace(req.Name))
	if err != nil {
		code := 400 // 本地问题：不是 custom、不存在、读不到 key
		var ue *core.UpstreamError
		if errors.As(err, &ue) {
			code = 502 // 上游 /models 出错
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}

func (s *Server) sourceDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !s.readPost(w, r, 1024, &req) {
		return
	}
	dir, err := s.App.RemoveCustomSource(req.Name)
	if err != nil {
		code := 400
		if errors.Is(err, source.ErrInUse) {
			code = 409
		}
		writeJSON(w, code, map[string]any{"error": err.Error(), "backup_dir": dir})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "backup_dir": dir})
}
