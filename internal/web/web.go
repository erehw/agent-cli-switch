// Package web 是 acsw ui 的本机网页：只监听 127.0.0.1，调用和命令行同一个 core。
package web

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"net"
	"net/http"
	"strings"

	"acsw/internal/core"
	"acsw/internal/home"
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
	App   *core.App
	Token string
	Port  int
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
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("POST /api/use", s.use)
	return s.guard(mux)
}

// guard 防 DNS 重绑定：只接受 Host 为 127.0.0.1/localhost 的请求。
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
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
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
		sv = append(sv, sourceView{Name: x.Name, Type: x.Meta.Type, BaseURL: x.Meta.BaseURL, Model: x.Meta.Model, HasLogin: x.HasLogin, UsedBy: x.UsedBy})
	}
	if st == nil {
		st = []home.Status{}
	}
	if sv == nil {
		sv = []sourceView{}
	}
	writeJSON(w, 200, map[string]any{"homes": st, "sources": sv})
}

func (s *Server) use(w http.ResponseWriter, r *http.Request) {
	tok := r.Header.Get("X-ACSW-Token")
	if subtle.ConstantTimeCompare([]byte(tok), []byte(s.Token)) != 1 || s.Token == "" {
		writeJSON(w, 403, map[string]string{"error": "token 不对，请刷新页面"})
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeJSON(w, 415, map[string]string{"error": "需要 JSON"})
		return
	}
	var req struct {
		Home   string `json:"home"`
		Source string `json:"source"`
		DryRun bool   `json:"dry_run"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "请求格式不对"})
		return
	}
	res, err := s.App.Use(home.UseOptions{Home: req.Home, Source: req.Source, DryRun: req.DryRun})
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": err.Error(), "steps": res.Steps})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "dry_run": req.DryRun, "steps": res.Steps, "warnings": res.Warnings, "backup_dir": res.BackupDir, "changed": res.Changed})
}
