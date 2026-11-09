package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

// ModelsBodyLimit 是 /models 响应体的上限，防止异常服务端把内存撑爆。
const ModelsBodyLimit = 32 << 20

// ModelsURL 按 codex 的约定拼出 /models 地址：base_url（通常已含 /v1）去掉结尾的 / 再接 /models。
func ModelsURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/models"
}

// ModelsHTTPError 是 /models 返回非 2xx 时的错误。Body 已截断并去掉了 key。
type ModelsHTTPError struct {
	URL        string
	Status     string
	StatusCode int
	Body       string
}

func (e *ModelsHTTPError) Error() string {
	msg := fmt.Sprintf("GET %s 返回 HTTP %s", e.URL, e.Status)
	if e.Body != "" {
		msg += "：" + e.Body
	}
	return msg
}

// redact 把文本里出现的 key 换成 ***（服务端或错误信息万一回显了 key）。
func redact(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, "***")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…（已截断）"
}

// FetchModels 用 Authorization: Bearer <key> 请求 <base_url>/models。
// 2xx 时原样返回响应体（不解析、不改动）；否则返回 *ModelsHTTPError 或网络错误，错误里不含 key。
func FetchModels(ctx context.Context, client *http.Client, baseURL, key string) ([]byte, error) {
	u := ModelsURL(baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败：%s", redact(err.Error(), key))
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 %s 失败：%s", u, redact(err.Error(), key))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, ModelsBodyLimit+1))
	if err != nil {
		return nil, fmt.Errorf("读取 %s 的响应失败（HTTP %s）：%s", u, resp.Status, redact(err.Error(), key))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &ModelsHTTPError{URL: u, Status: resp.Status, StatusCode: resp.StatusCode, Body: truncate(strings.TrimSpace(redact(string(body), key)), 500)}
	}
	if len(body) > ModelsBodyLimit {
		return nil, errors.New("响应体超过 32MB，放弃输出")
	}
	return body, nil
}
