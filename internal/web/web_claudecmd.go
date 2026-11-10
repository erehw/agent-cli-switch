package web

import (
	"net/http"
	"strings"
)

// claudeCmd 生成 claude-<source>.cmd（默认放在 acsw.exe 所在目录）。同名文件不是 acsw 生成的时返回 409
// confirm_required，页面确认后带 force:true 再调。
func (s *Server) claudeCmd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Dir   string `json:"dir"`
		Force bool   `json:"force"`
	}
	if !s.readPost(w, r, 4096, &req) {
		return
	}
	if s.AcswExe == "" {
		writeJSON(w, 500, map[string]string{"error": "不知道 acsw.exe 的路径，请在命令行运行 acsw claude cmd " + req.Name})
		return
	}
	res, err := s.App.WriteClaudeCmd(strings.TrimSpace(req.Name), s.AcswExe, strings.TrimSpace(req.Dir), "", "", req.Force)
	if err != nil {
		errJSON(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "result": res})
}
