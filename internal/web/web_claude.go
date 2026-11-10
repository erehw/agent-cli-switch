package web

import (
	"net/http"
	"strings"

	"acsw/internal/claude"
	"acsw/internal/core"
	"acsw/internal/source"
)

// claudeReq 是新增/修改 claude source 的请求。修改时字段为 null（不写）表示不改；extra_env 为 null 表示不改、{} 表示清空。
type claudeReq struct {
	Name     string            `json:"name"`
	BaseURL  *string           `json:"base_url"`
	Model    *string           `json:"model"`
	Opus     *string           `json:"opus_model"`
	Sonnet   *string           `json:"sonnet_model"`
	Haiku    *string           `json:"haiku_model"`
	Fable    *string           `json:"fable_model"`
	KeyMode  *string           `json:"key_mode"`
	ExtraEnv map[string]string `json:"extra_env"`
	Key      string            `json:"key"`
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (s *Server) claudeAdd(w http.ResponseWriter, r *http.Request) {
	var req claudeReq
	if !s.readPost(w, r, sourceBodyLimit, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	f := source.ClaudeFields{BaseURL: deref(req.BaseURL), Model: deref(req.Model), Opus: deref(req.Opus), Sonnet: deref(req.Sonnet), Haiku: deref(req.Haiku), Fable: deref(req.Fable), KeyMode: deref(req.KeyMode), ExtraEnv: req.ExtraEnv}
	if err := s.App.AddClaudeSource(name, f, req.Key); err != nil {
		errJSON(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "name": name, "launch": "acsw claude " + name, "notes": []string{"已建 claude source，key 已存入" + s.App.Cred.Kind() + "（acsw/claude/" + name + "）"}})
}

func (s *Server) claudeEdit(w http.ResponseWriter, r *http.Request) {
	var req claudeReq
	if !s.readPost(w, r, sourceBodyLimit, &req) {
		return
	}
	e := source.ClaudeEdit{BaseURL: req.BaseURL, Model: req.Model, Opus: req.Opus, Sonnet: req.Sonnet, Haiku: req.Haiku, Fable: req.Fable, KeyMode: req.KeyMode, ExtraEnv: req.ExtraEnv, Key: req.Key}
	res, err := s.App.EditClaudeSource(strings.TrimSpace(req.Name), e)
	if err != nil {
		errJSON(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "result": res})
}

// claudeExport 生成 settings 文件。plain（明文 key）必须带 confirm:true。返回文件路径，不返回内容。
func (s *Server) claudeExport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Mode    string `json:"mode"`
		Confirm bool   `json:"confirm"`
	}
	if !s.readPost(w, r, 1024, &req) {
		return
	}
	if m, err := claude.ParseMode(req.Mode); err == nil && m == claude.ModePlain && !req.Confirm {
		errJSON(w, core.Usage("导出明文 key 需要确认（confirm: true）"))
		return
	}
	path, mode, err := s.App.ExportClaude(strings.TrimSpace(req.Name), req.Mode)
	if err != nil {
		errJSON(w, err)
		return
	}
	out := map[string]any{"ok": true, "path": path, "mode": mode, "command": `claude --settings "` + path + `"`}
	if mode == claude.ModePlain {
		out["warning"] = "文件里有明文 key，不要提交、不要发给别人；用完可以删掉"
	}
	writeJSON(w, 200, out)
}
