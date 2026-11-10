// Package claude 生成给 Claude Code 用的 settings 文件（claude --settings <文件>），并检查别处的 settings 会不会漏进来。
//
// 沙盒实测（claude 2.1.319）的结论决定了这里的写法：
//   - --settings 文件里写了的变量会盖过用户级和项目级 settings；没写的变量会从那里漏进来，所以每次都把整套变量写全，不用的写空串。
//   - apiKeyHelper 模式必须同时写 ANTHROPIC_AUTH_TOKEN=""，否则用户级的 token 也会被发出去。
//   - claude 2.1.322 读字符串变量时先 trim，空串等于没设置；所以 /model 菜单的名字、说明等变量（ShieldedEnv）写空串，
//     标签就回落到 source 的模型 id，不会显示用户级 settings 里别的 provider 的名字。
//   - apiKeyHelper 的命令在 Windows 上要写成 "\"<exe>\" 参数"，路径不加引号时会静默失败、发出空认证。
package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Mode 是 key 的交付方式。
type Mode string

const (
	// ModeHelper：settings 里只有 apiKeyHelper（调用 acsw claude key <source>），key 不落盘。
	ModeHelper Mode = "helper"
	// ModePlain：key 明文写进 settings 的 ANTHROPIC_AUTH_TOKEN。
	ModePlain Mode = "plain"
)

// ParseMode 解析 --key-mode / key_mode。空串返回 ""（由调用方决定默认值）；file 是 plain 的别名。
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return "", nil
	case "helper":
		return ModeHelper, nil
	case "plain", "file":
		return ModePlain, nil
	}
	return "", fmt.Errorf("key 模式 %q 不认识，只能是 helper 或 plain（file）", s)
}

// 生成文件里总会写的变量（不用的写空串，挡住用户级/项目级 settings 里的同名变量）。
const (
	EnvBaseURL   = "ANTHROPIC_BASE_URL"
	EnvAuthToken = "ANTHROPIC_AUTH_TOKEN"
	EnvAPIKey    = "ANTHROPIC_API_KEY"
	EnvModel     = "ANTHROPIC_MODEL"
	EnvOpus      = "ANTHROPIC_DEFAULT_OPUS_MODEL"
	EnvSonnet    = "ANTHROPIC_DEFAULT_SONNET_MODEL"
	EnvHaiku     = "ANTHROPIC_DEFAULT_HAIKU_MODEL"
	EnvFable     = "ANTHROPIC_DEFAULT_FABLE_MODEL"
)

// ManagedEnv 是 acsw 生成文件里一定会出现的全部变量名。
var ManagedEnv = []string{
	EnvBaseURL, EnvAuthToken, EnvAPIKey,
	EnvModel, EnvOpus, EnvSonnet, EnvHaiku, EnvFable,
	"ANTHROPIC_SMALL_FAST_MODEL", "ANTHROPIC_CUSTOM_HEADERS",
	"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
}

// ShieldedEnv 是和模型选择、/model 菜单显示有关但 acsw 不填值的变量（claude 2.1.322 读这些）。
// 生成文件里一律写空串：claude 读字符串变量时先 trim，空串当没设置，于是菜单标签回落到 source 的模型 id、
// 说明回落到默认文字；这样用户级 settings（如 cc-switch 写的 GLM 配置）里的同名变量就漏不进来。
// 和 ManagedEnv 不同，这些可以用 --env 改成自己的值（例如给菜单起名字）。
var ShieldedEnv = []string{
	"ANTHROPIC_DEFAULT_MODEL",
	"ANTHROPIC_DEFAULT_OPUS_MODEL_NAME", "ANTHROPIC_DEFAULT_OPUS_MODEL_DESCRIPTION", "ANTHROPIC_DEFAULT_OPUS_MODEL_SUPPORTED_CAPABILITIES",
	"ANTHROPIC_DEFAULT_SONNET_MODEL_NAME", "ANTHROPIC_DEFAULT_SONNET_MODEL_DESCRIPTION", "ANTHROPIC_DEFAULT_SONNET_MODEL_SUPPORTED_CAPABILITIES",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL_NAME", "ANTHROPIC_DEFAULT_HAIKU_MODEL_DESCRIPTION", "ANTHROPIC_DEFAULT_HAIKU_MODEL_SUPPORTED_CAPABILITIES",
	"ANTHROPIC_DEFAULT_FABLE_MODEL_NAME", "ANTHROPIC_DEFAULT_FABLE_MODEL_DESCRIPTION", "ANTHROPIC_DEFAULT_FABLE_MODEL_SUPPORTED_CAPABILITIES",
	"ANTHROPIC_CUSTOM_MODEL_OPTION", "ANTHROPIC_CUSTOM_MODEL_OPTION_NAME", "ANTHROPIC_CUSTOM_MODEL_OPTION_DESCRIPTION", "ANTHROPIC_CUSTOM_MODEL_OPTION_SUPPORTED_CAPABILITIES",
	"ANTHROPIC_SMALL_FAST_MODEL_AWS_REGION",
	"CLAUDE_CODE_SUBAGENT_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL_FORCE", "CLAUDE_CODE_WORKFLOW_SUBAGENT_MODEL",
	"CLAUDE_CODE_AUTO_MODE_MODEL", "CLAUDE_CODE_BG_CLASSIFIER_MODEL",
	"CLAUDE_CODE_MODEL_CATALOG", "CLAUDE_CODE_MODEL_CATALOG_URL", "CLAUDE_CODE_MODEL_CAPABILITIES",
}

// AllEnv 是生成文件里一定会出现的全部变量名（ManagedEnv + ShieldedEnv）。
func AllEnv() []string {
	return append(append([]string{}, ManagedEnv...), ShieldedEnv...)
}

func inList(list []string, name string) bool {
	for _, n := range list {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

// IsManaged 判断变量名是否由 acsw 管理、不能用 --env 改（不分大小写）。
func IsManaged(name string) bool { return inList(ManagedEnv, name) }

// IsShielded 判断变量名是否在 ShieldedEnv 里（默认写空串，可用 --env 改）。
func IsShielded(name string) bool { return inList(ShieldedEnv, name) }

// Spec 是生成 settings 需要的 source 信息（不含 key）。
type Spec struct {
	BaseURL string
	Model   string
	Opus    string
	Sonnet  string
	Haiku   string
	Fable   string
	Extra   map[string]string
}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateExtraEnv 检查附加环境变量：名字要合法；不能是 acsw 管理的变量；名字含 KEY/TOKEN/SECRET 的拒绝（key 只能放凭据管理器）。
func ValidateExtraEnv(env map[string]string) error {
	for k, v := range env {
		if !envNameRe.MatchString(k) {
			return fmt.Errorf("附加环境变量名 %q 不合法", k)
		}
		u := strings.ToUpper(k)
		if strings.Contains(u, "KEY") || strings.Contains(u, "TOKEN") || strings.Contains(u, "SECRET") {
			return fmt.Errorf("附加环境变量 %s 的名字像是密钥（含 KEY/TOKEN/SECRET），不允许写进 source；key 只放凭据管理器", k)
		}
		if IsManaged(k) {
			return fmt.Errorf("附加环境变量 %s 由 acsw 管理，不能在 extra_env 里改", k)
		}
		if strings.ContainsAny(v, "\x00\r\n") {
			return fmt.Errorf("附加环境变量 %s 的值含控制字符", k)
		}
	}
	return nil
}

// HelperCommand 生成 apiKeyHelper 的命令：带引号的 acsw 路径 + " claude key <source>"。
// 路径含 % ^ " ! 时拒绝（cmd 会改写这些字符）。
func HelperCommand(exe, source string) (string, error) {
	if exe == "" {
		return "", errors.New("不知道 acsw.exe 的完整路径，无法生成 apiKeyHelper")
	}
	if strings.ContainsAny(exe, "%^\"!\r\n") {
		return "", fmt.Errorf("acsw 路径 %q 含 %% ^ \" ! 之一，cmd 会改写它，apiKeyHelper 用不了；请把 acsw.exe 放到不含这些字符的目录，或改用 --key-mode plain", exe)
	}
	return `"` + exe + `" claude key ` + source, nil
}

// file 是 settings 文件的结构；字段顺序即输出顺序。
type file struct {
	APIKeyHelper string            `json:"apiKeyHelper,omitempty"`
	Env          map[string]string `json:"env"`
}

// Env 生成完整的 env 表。plain 模式下 token 为 key，helper 模式为空串。
func (s Spec) Env(token string) map[string]string {
	env := map[string]string{}
	for _, n := range AllEnv() {
		env[n] = ""
	}
	for k, v := range s.Extra {
		if IsShielded(k) {
			k = canonical(k)
		}
		env[k] = v
	}
	env[EnvBaseURL] = s.BaseURL
	env[EnvAuthToken] = token
	env[EnvModel] = s.Model
	env[EnvOpus] = s.Opus
	env[EnvSonnet] = s.Sonnet
	env[EnvHaiku] = s.Haiku
	env[EnvFable] = s.Fable
	return env
}

// canonical 返回 ShieldedEnv 里的标准写法（大写）。
func canonical(name string) string {
	for _, n := range ShieldedEnv {
		if strings.EqualFold(n, name) {
			return n
		}
	}
	return name
}

// Build 生成 settings 文件内容（UTF-8 JSON，LF，结尾换行）。
// helper 模式需要 helperCmd、不放 key；plain 模式需要 key。
func Build(s Spec, mode Mode, key, helperCmd string) ([]byte, error) {
	if err := ValidateExtraEnv(s.Extra); err != nil {
		return nil, err
	}
	var f file
	switch mode {
	case ModeHelper:
		if helperCmd == "" {
			return nil, errors.New("helper 模式缺 apiKeyHelper 命令")
		}
		f = file{APIKeyHelper: helperCmd, Env: s.Env("")}
	case ModePlain:
		if strings.TrimSpace(key) == "" {
			return nil, errors.New("plain 模式缺 key")
		}
		f = file{Env: s.Env(key)}
	default:
		return nil, fmt.Errorf("key 模式 %q 不认识", mode)
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Inspection 是对一个已生成 settings 文件的检查结果。不含 key 值。
type Inspection struct {
	Missing      []string // 缺了哪些 acsw 管理的变量
	Helper       string   // apiKeyHelper 命令（不含 key）
	HelperExe    string   // 从命令里解析出的 exe 路径
	HelperQuoted bool     // 路径是否用双引号包住
	HasPlainKey  bool     // ANTHROPIC_AUTH_TOKEN 非空（明文 key）
}

// Inspect 解析一个 settings 文件内容。
func Inspect(data []byte) (Inspection, error) {
	var in Inspection
	var f struct {
		APIKeyHelper string            `json:"apiKeyHelper"`
		Env          map[string]string `json:"env"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return in, fmt.Errorf("不是合法的 JSON：%w", err)
	}
	for _, n := range AllEnv() {
		if _, ok := f.Env[n]; !ok {
			in.Missing = append(in.Missing, n)
		}
	}
	in.HasPlainKey = f.Env[EnvAuthToken] != ""
	in.Helper = f.APIKeyHelper
	if h := f.APIKeyHelper; h != "" {
		if strings.HasPrefix(h, `"`) {
			if j := strings.Index(h[1:], `"`); j >= 0 {
				in.HelperQuoted = true
				in.HelperExe = h[1 : j+1]
			}
		} else {
			in.HelperExe, _, _ = strings.Cut(h, " ")
		}
	}
	return in, nil
}

// ConfigDir 返回 Claude Code 的用户配置目录：CLAUDE_CONFIG_DIR，否则 ~/.claude。
func ConfigDir(getenv func(string) string) string {
	if d := strings.TrimSpace(getenv("CLAUDE_CONFIG_DIR")); d != "" {
		return d
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, ".claude")
}

// UserSettingsFiles 是用户级 settings 文件（存在与否都列出）。
func UserSettingsFiles(configDir string) []string {
	if configDir == "" {
		return nil
	}
	return []string{filepath.Join(configDir, "settings.json")}
}

// ProjectSettingsFiles 是项目级 settings 文件（只看当前目录下的 .claude）。
func ProjectSettingsFiles(dir string) []string {
	if dir == "" {
		return nil
	}
	return []string{filepath.Join(dir, ".claude", "settings.json"), filepath.Join(dir, ".claude", "settings.local.json")}
}

// Finding 是一个 settings 文件里与登录来源有关的设置（只有变量名，没有值）。
type Finding struct {
	File      string   `json:"file"`
	Covered   []string `json:"covered,omitempty"`   // acsw 生成文件会盖住的变量
	Uncovered []string `json:"uncovered,omitempty"` // ANTHROPIC_* 里 acsw 不管、会漏进来的变量
	Helper    bool     `json:"api_key_helper,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// Leaky 表示这个文件里有会漏进来的设置。plain 模式下别处的 apiKeyHelper 也算。
func (f Finding) Leaky(mode Mode) bool {
	return len(f.Uncovered) > 0 || (mode == ModePlain && f.Helper)
}

// relevant 判断变量是否和登录来源有关：ANTHROPIC_* 都算（CLAUDE_CODE_USE_BEDROCK/VERTEX/FOUNDRY 已在 ManagedEnv 里）。
// CLAUDE_CODE_USE_POWERSHELL_TOOL 这类功能开关不算。
func relevant(name string) bool {
	return strings.HasPrefix(strings.ToUpper(name), "ANTHROPIC_")
}

// Scan 读这些 settings 文件，只取 env 里与登录来源有关的变量名和是否有 apiKeyHelper。
// 不存在的文件跳过；不返回任何值。
func Scan(files []string) []Finding {
	var out []Finding
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				out = append(out, Finding{File: p, Error: "读不出来：" + err.Error()})
			}
			continue
		}
		var f struct {
			APIKeyHelper json.RawMessage            `json:"apiKeyHelper"`
			Env          map[string]json.RawMessage `json:"env"`
		}
		if err := json.Unmarshal(b, &f); err != nil {
			out = append(out, Finding{File: p, Error: "不是合法的 JSON"})
			continue
		}
		fd := Finding{File: p, Helper: len(f.APIKeyHelper) > 0 && string(f.APIKeyHelper) != "null" && string(f.APIKeyHelper) != `""`}
		for k := range f.Env {
			switch {
			case IsManaged(k) || IsShielded(k):
				fd.Covered = append(fd.Covered, k)
			case relevant(k):
				fd.Uncovered = append(fd.Uncovered, k)
			}
		}
		sort.Strings(fd.Covered)
		sort.Strings(fd.Uncovered)
		if len(fd.Covered) > 0 || len(fd.Uncovered) > 0 || fd.Helper {
			out = append(out, fd)
		}
	}
	return out
}

// UnsafePathChars 是生成文件路径里不允许出现的字符（cmd 会改写）。
const UnsafePathChars = "%^\"!"
