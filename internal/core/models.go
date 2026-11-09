package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"acsw/internal/config"
	"acsw/internal/cred"
	"acsw/internal/exitcode"
	"acsw/internal/source"
)

// ModelsTimeout 是 source models 整个请求的超时。
const ModelsTimeout = 30 * time.Second

// modelsClient 用默认 Transport（会读 HTTP_PROXY/HTTPS_PROXY/NO_PROXY）。
var modelsClient = &http.Client{Timeout: ModelsTimeout}

// UpstreamError 表示错误发生在请求上游 /models 时（网络、超时、非 2xx），而不是本地配置问题。
type UpstreamError struct{ Err error }

func (e *UpstreamError) Error() string { return e.Err.Error() }
func (e *UpstreamError) Unwrap() error { return e.Err }

// SourceModels 用 custom source 的 base_url 和凭据管理器里的 key 请求 <base_url>/models，
// 返回原始响应体。key 只放进请求头，不出现在返回值或错误里。
func (a *App) SourceModels(ctx context.Context, name string) ([]byte, error) {
	meta, err := config.LoadSource(a.Paths, name)
	if err != nil {
		return nil, exitcode.WithF(exitcode.SourceInvalid, err, exitcode.F{"source": name})
	}
	if meta.Type != config.ProviderCustom {
		return nil, exitcode.WithF(exitcode.SourceInvalid, fmt.Errorf("source %s 是 %s 类型；source models 只支持 custom（API）source", name, meta.Type), exitcode.F{"source": name})
	}
	key, err := a.Cred.Get(cred.Target(name))
	if err != nil || key == "" {
		return nil, exitcode.WithF(exitcode.KeyUnreadable, fmt.Errorf("从%s读不到 source %s 的 key（目标 %s）。可以用 acsw source edit %s --set-key 重新存入", a.Cred.Kind(), name, cred.Target(name), name), exitcode.F{"source": name})
	}
	body, err := source.FetchModels(ctx, modelsClient, meta.BaseURL, key)
	if err != nil {
		f := exitcode.F{"source": name, "url": source.ModelsURL(meta.BaseURL)}
		var he *source.ModelsHTTPError
		if errors.As(err, &he) {
			f["http_status"] = he.StatusCode
		}
		return nil, exitcode.WithF(exitcode.Upstream, &UpstreamError{Err: err}, f)
	}
	return body, nil
}
