package core

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"acsw/internal/claude"
	"acsw/internal/config"
	"acsw/internal/cred"
)

// ClaudeTestedVersion 是沙盒实测过 --settings / apiKeyHelper 行为的 claude 版本。
const ClaudeTestedVersion = "2.1.319"

var claudeVerRe = regexp.MustCompile(`\d+\.\d+\.\d+`)

// claudeVersion 运行 claude --version（本地命令，不连模型），取不到时返回 ""。
func (a *App) claudeVersion(exe string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--version")
	cmd.Env = config.ChildEnv(a.environ(), claude.AllEnv(), nil)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return claudeVerRe.FindString(string(out))
}

func majorMinor(v string) string {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return v
	}
	return parts[0] + "." + parts[1]
}

// doctorClaude 检查 claude 命令、各 claude source 的 key 和生成的 settings 文件、用户级 settings 里的登录变量（只看名字）。
// 只在有 claude source 时调用。
func (a *App) doctorClaude(add func(name, level, format string, args ...any)) {
	ci := a.ClaudeInfo()
	if !ci.Found {
		add("claude", "error", "%s", ci.Error)
	} else if v := a.claudeVersion(ci.Path); v == "" {
		add("claude", "warn", "%s（来自 %s），但 claude --version 没有返回版本号", ci.Path, ci.From)
	} else if majorMinor(v) != majorMinor(ClaudeTestedVersion) {
		add("claude", "warn", "%s（来自 %s）版本 %s，和实测过的 %s 差得较远；--settings/apiKeyHelper 的行为可能变了，建议重新验证", ci.Path, ci.From, v, ClaudeTestedVersion)
	} else {
		add("claude", "ok", "%s（来自 %s）版本 %s", ci.Path, ci.From, v)
	}

	st, err := a.ClaudeStatus()
	if err != nil {
		add("claude-sources", "error", "读 claude source 失败：%v", err)
		return
	}
	for _, s := range st.Sources {
		name := "claude-source:" + s.Name
		if !s.HasKey {
			add(name, "error", "claude source %s 在凭据管理器里没有 key（%s）", s.Name, cred.TargetFor(config.ToolClaude, s.Name))
		} else {
			add(name, "ok", "claude source %s：key 正常，key_mode=%s，%s", s.Name, s.KeyMode, s.BaseURL)
		}
		if s.Settings.Exists {
			b, err := os.ReadFile(s.Settings.Path)
			in, ierr := claude.Inspect(b)
			switch {
			case err != nil || ierr != nil:
				add(name+":settings", "error", "%s 读不出来或不是合法 JSON；下次 acsw claude %s 会重新生成", s.Settings.Path, s.Name)
			case len(in.Missing) > 0:
				add(name+":settings", "warn", "%s 缺变量 %s；下次 acsw claude %s 会重新生成", s.Settings.Path, strings.Join(in.Missing, "、"), s.Name)
			case in.Helper == "":
				add(name+":settings", "warn", "%s 里没有 apiKeyHelper；下次 acsw claude %s 会重新生成", s.Settings.Path, s.Name)
			case !in.HelperQuoted:
				add(name+":settings", "error", "%s 的 apiKeyHelper 路径没加引号，claude 会静默发出空认证；重新运行 acsw claude export %s", s.Settings.Path, s.Name)
			case !config.Exists(in.HelperExe):
				add(name+":settings", "warn", "%s 的 apiKeyHelper 指向的 %s 不存在（acsw 挪过位置？）；重新运行 acsw claude export %s", s.Settings.Path, in.HelperExe, s.Name)
			case !s.Settings.Current:
				add(name+":settings", "warn", "%s 和当前设置不一致；下次 acsw claude %s 会重新生成，直接用 claude --settings 的话先运行 acsw claude export %s", s.Settings.Path, s.Name, s.Name)
			default:
				add(name+":settings", "ok", "%s 完整，apiKeyHelper 正常", s.Settings.Path)
			}
		}
		if s.Plain != nil {
			add(name+":plain", "warn", "%s 里有明文 key；不用了可以删掉", s.Plain.Path)
		}
	}

	// 用户级 settings：只看变量名，不读值
	for _, f := range claude.Scan(claude.UserSettingsFiles(st.ConfigDir)) {
		name := "claude-user-settings"
		switch {
		case f.Error != "":
			add(name, "warn", "%s：%s", f.File, f.Error)
		case len(f.Uncovered) > 0:
			add(name, "warn", "%s 里有 acsw 盖不住的登录变量 %s，用 acsw claude 时会漏进来（acsw 不改这个文件）", f.File, strings.Join(f.Uncovered, "、"))
		default:
			var what []string
			what = append(what, f.Covered...)
			if f.Helper {
				what = append(what, "apiKeyHelper")
			}
			add(name, "ok", "%s 里有 %s，acsw 生成的文件会把它们盖住（acsw 不改这个文件）", f.File, strings.Join(what, "、"))
		}
	}
	add("claude-sessions", "ok", "会话目录 %s（acsw 不设 CLAUDE_CONFIG_DIR，不改会话位置）", st.SessionsDir)
}
