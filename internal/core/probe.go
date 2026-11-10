package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"unicode/utf8"

	"acsw/internal/config"
	"acsw/internal/cred"
	"acsw/internal/exitcode"
	"acsw/internal/source"
)

// ModelsProbe 是“从上游获取模型”的结果：解析出的模型 id；响应不是常见格式时 Nonstandard 为真并附上截断的原文。
type ModelsProbe struct {
	URL         string   `json:"url"`
	IDs         []string `json:"ids"`
	Nonstandard bool     `json:"nonstandard,omitempty"`
	Raw         string   `json:"raw,omitempty"`
}

// ProbeError 说明获取模型失败的原因：unauthorized（401/403）、timeout、http（其他非 2xx）、network。
type ProbeError struct {
	Reason     string
	HTTPStatus int
	Err        error
}

func (e *ProbeError) Error() string { return e.Err.Error() }
func (e *ProbeError) Unwrap() error { return e.Err }

// ParseModelIDs 从 /models 响应里取模型 id：支持 {"data":[{"id"}]}（OpenAI/Anthropic）、{"models":[{"id"|"name"}]}、
// 以及字符串数组。取不到时 ok 为假。
func ParseModelIDs(body []byte) (ids []string, ok bool) {
	type item struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	pick := func(items []item) {
		for _, it := range items {
			if it.ID != "" {
				ids = append(ids, it.ID)
			} else if it.Name != "" {
				ids = append(ids, it.Name)
			}
		}
	}
	var obj struct {
		Data   []item `json:"data"`
		Models []item `json:"models"`
	}
	if err := json.Unmarshal(body, &obj); err == nil && (obj.Data != nil || obj.Models != nil) {
		pick(obj.Data)
		pick(obj.Models)
	} else {
		var arr []string
		if err := json.Unmarshal(body, &arr); err != nil {
			return nil, false
		}
		ids = arr
	}
	if len(ids) == 0 {
		return []string{}, true
	}
	seen := map[string]bool{}
	out := ids[:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, true
}

func classifyProbe(ctx context.Context, err error) error {
	var ue *UpstreamError
	if !errors.As(err, &ue) {
		return err // 本地问题（source 不存在、读不到 key……），保留原退出码
	}
	pe := &ProbeError{Reason: "network", Err: err}
	var he *source.ModelsHTTPError
	var ne net.Error
	switch {
	case errors.As(err, &he):
		pe.HTTPStatus = he.StatusCode
		pe.Reason = "http"
		if he.StatusCode == 401 || he.StatusCode == 403 {
			pe.Reason = "unauthorized"
			pe.Err = fmt.Errorf("上游拒绝了 key（HTTP %d）：%w", he.StatusCode, err)
		}
	// 下层错误为了去掉 key 被转成了文本，所以除了 errors.Is 也看 ctx 和错误文本
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) ||
		errors.Is(ctx.Err(), context.DeadlineExceeded) || strings.Contains(err.Error(), "Client.Timeout exceeded") || strings.Contains(err.Error(), "context deadline exceeded"):
		pe.Reason = "timeout"
		pe.Err = fmt.Errorf("请求上游超时：%w", err)
	}
	return exitcode.WithF(exitcode.Upstream, pe, exitcode.Fields(err))
}

// ProbeModels 从上游获取模型列表，供新增/编辑表单选用。tool 为 codex 或 claude。
//   - key 非空：用表单里的 baseURL 和 key 直接请求（新建、还没保存的 source）；key 只用于这一次请求，不保存、不记录。
//   - key 为空：用已保存的 source（name）及其凭据管理器里的 key；baseURL 若和已保存的不同则拒绝，
//     免得把已保存的 key 发到一个新地址（请在表单里输入 key 再查）。
func (a *App) ProbeModels(ctx context.Context, tool, name, baseURL, key string) (ModelsProbe, error) {
	var res ModelsProbe
	if tool != config.ToolClaude {
		tool = config.ToolCodex
	}
	key = strings.TrimSpace(key)
	var body []byte
	var err error
	if key != "" {
		b, verr := source.ValidateBaseURL(baseURL)
		if verr != nil {
			return res, exitcode.With(exitcode.BadArgs, Usage(verr.Error()))
		}
		if tool == config.ToolClaude {
			res.URL = source.ClaudeModelsURL(b)
			body, err = source.FetchClaudeModels(ctx, modelsClient, b, key)
		} else {
			res.URL = source.ModelsURL(b)
			body, err = source.FetchModels(ctx, modelsClient, b, key)
		}
		if err != nil {
			err = exitcode.WithF(exitcode.Upstream, &UpstreamError{Err: err}, exitcode.F{"url": res.URL})
		}
	} else {
		name = strings.TrimSpace(name)
		if name == "" {
			return res, exitcode.With(exitcode.BadArgs, Usage("新 source 要先填 base_url 和 key 才能获取模型"))
		}
		meta, lerr := config.LoadSource(a.Paths, name)
		if lerr != nil {
			return res, exitcode.WithF(exitcode.SourceInvalid, lerr, exitcode.F{"source": name})
		}
		if meta.ToolName() != tool {
			return res, exitcode.WithF(exitcode.ToolMismatch, fmt.Errorf("source %s 是给 %s 用的，不是 %s source", name, meta.ToolName(), tool), exitcode.F{"source": name})
		}
		if strings.TrimSpace(baseURL) != "" {
			if b, verr := source.ValidateBaseURL(baseURL); verr != nil || b != meta.BaseURL {
				return res, exitcode.WithF(exitcode.BadArgs, Usage("base_url 和已保存的不同：为了不把已保存的 key 发到新地址，请在表单里输入 key 再获取，或先保存"), exitcode.F{"source": name})
			}
		}
		if tool == config.ToolClaude {
			res.URL = source.ClaudeModelsURL(meta.BaseURL)
		} else {
			res.URL = source.ModelsURL(meta.BaseURL)
		}
		body, err = a.SourceModels(ctx, name)
		if err == nil {
			key, _ = a.Cred.Get(cred.TargetFor(meta.Tool, name)) // 只用来把原文里万一回显的 key 遮掉
		}
	}
	if err != nil {
		return res, classifyProbe(ctx, err)
	}
	ids, ok := ParseModelIDs(body)
	if !ok {
		res.IDs, res.Nonstandard = []string{}, true
		raw := redactKey(string(body), key)
		if len(raw) > 2000 {
			raw = raw[:2000]
			for len(raw) > 0 && !utf8.ValidString(raw) {
				raw = raw[:len(raw)-1]
			}
			raw += "…（已截断）"
		}
		res.Raw = raw
		return res, nil
	}
	res.IDs = ids
	return res, nil
}

func redactKey(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, "***")
}
