// Package exitcode 集中定义 acsw 自己的退出码，避免和 codex 的退出码撞车。
//
// Windows 上退出码是 32 位，acsw 用 9999xxxx 段：
//   - 99994xxx：映射 / home / 参数问题（改配置或调用方式能解决）
//   - 99995xxx：运行环境问题（key、codex、锁、网络……）
//
// codex 正常启动后，acsw 原样返回 codex 自己的退出码，不经过这里。
// 非 Windows 平台退出码会被截成 0-255，所以保留旧的小码：99994xxx → 2，99995xxx → 1。
package exitcode

import (
	"errors"
	"runtime"
)

// Code 是 acsw 的逻辑退出码（数值即 Windows 上的实际退出码）。
type Code int

const (
	NotMapped      Code = 99994001 // 当前目录没有映射到任何 home
	ExpectMismatch Code = 99994002 // 当前目录映射的 home 不是 --expect-home 指定的
	NoSource       Code = 99994003 // home 还没指定 source
	BadArgs        Code = 99994004 // 用法错误 / 参数不对 / 不认识的命令
	HomeConflict   Code = 99994005 // --home 与 --expect-home 冲突
	HomeNotFound   Code = 99994006 // home 不存在或名称不合法
	SourceInvalid  Code = 99994007 // source 不存在、类型不对（如对 openai 查模型）或元数据损坏
	ReadonlyAlias  Code = 99994008 // home 是官方 CODEX_HOME 的只读别名，拒绝 use/改配置等写操作
	HomeInUse      Code = 99994009 // home 还有项目映射或在跑的 codex，拒绝删除
	ToolMismatch   Code = 99994010 // source 属于另一个工具（如把 claude source 用到 codex home，或反过来）
	SettingsClash  Code = 99994011 // --strict 时发现用户级/项目级 Claude settings 里有会漏进来的登录设置

	KeyUnreadable Code = 99995001 // 凭据管理器里读不到 key
	CodexNotFound Code = 99995002 // 找不到 codex 可执行文件
	LockBusy      Code = 99995003 // 等全局锁超时（另一个 acsw 正在改配置）
	CodexStart    Code = 99995004 // codex 启动失败
	Upstream      Code = 99995005 // 请求上游失败（/models 非 2xx、网络、超时）
	ClaudeMissing Code = 99995006 // 找不到 claude 可执行文件
	ClaudeStart   Code = 99995007 // claude 启动失败
	Other         Code = 99995999 // 其他 acsw 错误
)

// F 是附在错误上的上下文字段（如 home、dir、source），会写进 stderr 最后一行的 JSON。不得放 key。
type F map[string]any

// Error 给错误附上退出码和可选的上下文字段；Unwrap 保留原错误，errors.Is/As 照常可用。
type Error struct {
	Code   Code
	Err    error
	Fields F
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// With 给 err 附上退出码；err 为 nil 时返回 nil。
func With(c Code, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: c, Err: err}
}

// WithF 同 With，另附上下文字段。
func WithF(c Code, err error, f F) error {
	if err == nil {
		return nil
	}
	return &Error{Code: c, Err: err, Fields: f}
}

// Fields 合并错误链上所有 *Error 的上下文字段，外层优先；值为空串或 nil 的跳过。
func Fields(err error) F {
	out := F{}
	for err != nil {
		if e, ok := err.(*Error); ok {
			for k, v := range e.Fields {
				if _, seen := out[k]; seen || v == nil || v == "" {
					continue
				}
				out[k] = v
			}
		}
		err = errors.Unwrap(err)
	}
	return out
}

// kinds 是每个退出码对应的稳定 kind 字符串（写进 JSON，供程序判断；只增不改）。
var kinds = map[Code]string{
	NotMapped:      "not_mapped",
	ExpectMismatch: "expect_home_mismatch",
	NoSource:       "no_source",
	BadArgs:        "bad_args",
	HomeConflict:   "home_conflict",
	HomeNotFound:   "home_not_found",
	SourceInvalid:  "source_invalid",
	ReadonlyAlias:  "home_readonly_alias",
	HomeInUse:      "home_in_use",
	ToolMismatch:   "source_tool_mismatch",
	SettingsClash:  "settings_conflict",
	ClaudeMissing:  "claude_not_found",
	ClaudeStart:    "claude_start_failed",
	KeyUnreadable:  "key_unreadable",
	CodexNotFound:  "codex_not_found",
	LockBusy:       "lock_busy",
	CodexStart:     "codex_start_failed",
	Upstream:       "upstream_error",
	Other:          "other",
}

// Kind 返回退出码的 kind 字符串；未知码返回 "other"。
func (c Code) Kind() string {
	if k, ok := kinds[c]; ok {
		return k
	}
	return "other"
}

// Of 取 err 上最外层的退出码；没有附码时 ok 为 false。
func Of(err error) (Code, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Code, true
	}
	return 0, false
}

// Exit 返回当前平台上实际使用的进程退出码。
func (c Code) Exit() int { return exitFor(c, runtime.GOOS) }

func exitFor(c Code, goos string) int {
	if goos == "windows" {
		return int(c)
	}
	if c/1000 == 99994 {
		return 2
	}
	return 1
}
