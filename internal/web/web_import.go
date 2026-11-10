package web

import "net/http"

// homeImportBase 从官方 config.toml 导入和 provider 无关的设置。dry_run 为真时只返回预览。只返回键名，不返回任何值。
func (s *Server) homeImportBase(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Home   string `json:"home"`
		DryRun bool   `json:"dry_run"`
	}
	if !s.readPost(w, r, 1024, &req) {
		return
	}
	res, err := s.App.ImportBase(req.Home, s.OfficialDir, req.DryRun)
	if err != nil {
		errJSON(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "result": res})
}
