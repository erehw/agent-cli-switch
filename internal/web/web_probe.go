package web

import (
	"context"
	"errors"
	"net/http"

	"acsw/internal/core"
)

// probeTimeout 是“从上游获取模型”的超时（测试里可改短）。
var probeTimeout = core.ModelsTimeout

// modelsProbe 给新增/编辑表单从上游获取模型列表。
// 已保存的 source 用凭据管理器里的 key（key 不出 acsw）；新 source 可以带表单里输入的 key，只用于这一次请求，不保存、不记录。
func (s *Server) modelsProbe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tool    string `json:"tool"`
		Name    string `json:"name"`
		BaseURL string `json:"base_url"`
		Key     string `json:"key"`
	}
	if !s.readPost(w, r, sourceBodyLimit, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), probeTimeout)
	defer cancel()
	res, err := s.App.ProbeModels(ctx, req.Tool, req.Name, req.BaseURL, req.Key)
	if err != nil {
		var pe *core.ProbeError
		if errors.As(err, &pe) {
			out := map[string]any{"error": err.Error(), "kind": "upstream_error", "reason": pe.Reason, "url": res.URL}
			if pe.HTTPStatus != 0 {
				out["http_status"] = pe.HTTPStatus
			}
			writeJSON(w, 502, out)
			return
		}
		errJSON(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "url": res.URL, "ids": res.IDs, "nonstandard": res.Nonstandard, "raw": res.Raw})
}
