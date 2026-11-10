package home

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"acsw/internal/config"
)

// config.toml 里由 acsw 管理的两个标记块：
//   - top 块放在文件最前面（顶层键必须出现在任何 [表] 之前）；
//   - tables 块放在文件最后（[model_providers.custom]）。
//
// 块外的内容原样保留。
const (
	topStart = "# >>> acsw:top 由 acsw 管理，勿手改 >>>"
	topEnd   = "# <<< acsw:top <<<"
	tabStart = "# >>> acsw:tables 由 acsw 管理，勿手改 >>>"
	tabEnd   = "# <<< acsw:tables <<<"
	disabled = "# acsw 已注释（改由 acsw 管理）: "
)

func tomlStr(s string) (string, error) {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("值 %q 含控制字符", s)
		}
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`, nil
}

// SplitManaged 把 config.toml 拆成 acsw 的 top 块（含标记行）、块外内容、tables 块（含标记行）。没有的部分为空串。
func SplitManaged(text string) (top, user, tables string) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var t, u, b []string
	cur := &u
	for _, l := range lines {
		switch strings.TrimSpace(l) {
		case topStart:
			cur = &t
		case tabStart:
			cur = &b
		}
		*cur = append(*cur, l)
		switch strings.TrimSpace(l) {
		case topEnd, tabEnd:
			cur = &u
		}
	}
	j := func(x []string) string { return strings.Trim(strings.Join(x, "\n"), "\n") }
	return j(t), j(u), j(b)
}

// stripBlocks 去掉已有的 acsw 标记块。
func stripBlocks(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var out []string
	skip := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case t == topStart || t == tabStart:
			skip = true
			continue
		case t == topEnd || t == tabEnd:
			skip = false
			continue
		}
		if !skip {
			out = append(out, l)
		}
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

var tableHdr = regexp.MustCompile(`^\s*\[`)

// disableTopKeys 把块外、第一个表头之前与 acsw 管理冲突的顶层键注释掉。
func disableTopKeys(text string, keys []string) string {
	if text == "" {
		return text
	}
	re := regexp.MustCompile(`^\s*(` + strings.Join(keys, "|") + `)\s*=`)
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if tableHdr.MatchString(l) {
			break
		}
		if re.MatchString(l) {
			lines[i] = disabled + l
		}
	}
	return strings.Join(lines, "\n")
}

// RenderConfig 根据 source 生成新的 config.toml 全文。old 为原内容（可空）。
func RenderConfig(old string, meta config.SourceMeta) (string, error) {
	user := stripBlocks(old)
	keys := []string{"model_provider", "cli_auth_credentials_store"}
	if meta.Model != "" {
		keys = append(keys, "model")
	}
	user = disableTopKeys(user, keys)

	// 用户自己若已有 [model_providers.custom]，会和 acsw 冲突。
	var probe map[string]any
	if _, err := toml.Decode(user, &probe); err != nil {
		return "", fmt.Errorf("config.toml 里 acsw 管理块之外的内容格式有误，请先修好：%w", err)
	}
	if mp, ok := probe["model_providers"].(map[string]any); ok {
		if _, ok := mp[config.ProviderCustom]; ok {
			return "", fmt.Errorf("config.toml 里已经有你自己写的 [model_providers.custom]，和 acsw 冲突，请删掉这一段")
		}
	}

	var b strings.Builder
	b.WriteString(topStart + "\n")
	fmt.Fprintf(&b, "model_provider = %q\n", meta.Provider())
	if meta.Model != "" {
		v, err := tomlStr(meta.Model)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "model = %s\n", v)
	}
	b.WriteString("cli_auth_credentials_store = \"file\"\n")
	b.WriteString(topEnd + "\n")
	if user != "" {
		b.WriteString("\n" + user + "\n")
	}
	if meta.Provider() == config.ProviderCustom {
		u, err := tomlStr(meta.BaseURL)
		if err != nil {
			return "", err
		}
		b.WriteString("\n" + tabStart + "\n")
		b.WriteString("[model_providers.custom]\n")
		b.WriteString("name = \"custom\"\n")
		fmt.Fprintf(&b, "base_url = %s\n", u)
		fmt.Fprintf(&b, "env_key = %q\n", config.KeyEnv)
		b.WriteString("wire_api = \"responses\"\n")
		b.WriteString("requires_openai_auth = false\n")
		b.WriteString("supports_websockets = false\n")
		b.WriteString(tabEnd + "\n")
	}
	out := b.String()

	// 校验：整体能解析，且 model_provider 正确。
	var chk struct {
		ModelProvider string `toml:"model_provider"`
	}
	if _, err := toml.Decode(out, &chk); err != nil {
		return "", fmt.Errorf("生成的 config.toml 解析失败（可能你的配置里有重复键）：%w", err)
	}
	if chk.ModelProvider != meta.Provider() {
		return "", fmt.Errorf("生成的 config.toml 里 model_provider 不对：%q", chk.ModelProvider)
	}
	return out, nil
}
