package config

import (
	"runtime"
	"strings"
)

// 启动 codex 前要从父环境清掉的变量：避免误用别的 key 或被别的 CODEX_HOME 带偏。
var DropEnv = []string{"OPENAI_API_KEY", "CODEX_API_KEY", "ACSW_API_KEY", "CODEX_HOME", "CODEX_SQLITE_HOME", "OPENAI_BASE_URL"}

// KeyEnv 是 custom provider 读取 key 的环境变量名（写进 config.toml 的 env_key）。
const KeyEnv = "ACSW_API_KEY"

// ChildEnv 基于 base 生成子进程环境：先删 drop，再设 set。Windows 上变量名不分大小写。
func ChildEnv(base []string, drop []string, set map[string]string) []string {
	ci := runtime.GOOS == "windows"
	norm := func(s string) string {
		if ci {
			return strings.ToUpper(s)
		}
		return s
	}
	kill := map[string]bool{}
	for _, d := range drop {
		kill[norm(d)] = true
	}
	for k := range set {
		kill[norm(k)] = true
	}
	out := make([]string, 0, len(base)+len(set))
	for _, kv := range base {
		k := kv
		if i := strings.Index(kv, "="); i > 0 {
			k = kv[:i]
		}
		if kill[norm(k)] {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range set {
		out = append(out, k+"="+v)
	}
	return out
}
