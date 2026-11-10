// Package source 管理 source：一份登录（openai）或一份 API 配置（custom）。
package source

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"acsw/internal/config"
	"acsw/internal/cred"
)

// AuthFile 返回 source 下的 auth.json 路径。
func AuthFile(p config.Paths, name string) string {
	return filepath.Join(p.SourceDir(name), "auth.json")
}

// 写进 openai source 目录的 config.toml，让 codex login 把登录存成文件而不是系统钥匙串。
const sourceConfig = "# acsw：让登录写进本目录的 auth.json\ncli_auth_credentials_store = \"file\"\n"

func prepareDir(p config.Paths, name string) error {
	if err := config.ValidName("source", name); err != nil {
		return err
	}
	if config.Exists(p.SourceMetaFile(name)) {
		return fmt.Errorf("source %q 已存在", name)
	}
	return os.MkdirAll(p.SourceDir(name), 0o700)
}

// AddOpenAIFrom 从现有 CODEX_HOME 复制 auth.json 建立 openai source。
func AddOpenAIFrom(p config.Paths, name, fromHome string) error {
	src := filepath.Join(fromHome, "auth.json")
	b, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("读不到 %s：%w", src, err)
	}
	if !LooksLikeChatGPTAuth(b) {
		return fmt.Errorf("%s 看起来不是 ChatGPT 登录（没有 tokens），openai 类型的 source 需要订阅登录", src)
	}
	if err := prepareDir(p, name); err != nil {
		return err
	}
	if err := config.WriteFileAtomic(AuthFile(p, name), b, 0o600); err != nil {
		return err
	}
	_ = config.WriteFileAtomic(filepath.Join(p.SourceDir(name), "config.toml"), []byte(sourceConfig), 0o644)
	return config.SaveSource(p, name, &config.SourceMeta{Type: config.ProviderOpenAI, Created: time.Now()})
}

// AddOpenAILogin 以 CODEX_HOME=source 目录运行 `codex login`。
func AddOpenAILogin(p config.Paths, name, codexPath string, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := prepareDir(p, name); err != nil {
		return err
	}
	dir := p.SourceDir(name)
	_ = config.WriteFileAtomic(filepath.Join(dir, "config.toml"), []byte(sourceConfig), 0o644)
	cmd := exec.Command(codexPath, "login")
	cmd.Env = config.ChildEnv(os.Environ(), config.DropEnv, map[string]string{"CODEX_HOME": dir})
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("codex login 没有成功：%w（source 目录 %s 已保留，可删掉重来）", err, dir)
	}
	b, err := os.ReadFile(AuthFile(p, name))
	if err != nil || !LooksLikeChatGPTAuth(b) {
		return fmt.Errorf("codex login 结束了，但 %s 里没有 ChatGPT 登录。若登录存进了系统钥匙串，请检查 codex 的 cli_auth_credentials_store 设置", dir)
	}
	return config.SaveSource(p, name, &config.SourceMeta{Type: config.ProviderOpenAI, Created: time.Now()})
}

// AddCustom 建立 custom source，key 存凭据管理器。
func AddCustom(p config.Paths, store cred.Store, name, baseURL, model, key string) error {
	baseURL, model, err := validateCustom(baseURL, model)
	if err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("key 是空的，没有保存")
	}
	if err := prepareDir(p, name); err != nil {
		return err
	}
	if err := store.Set(cred.Target(name), key); err != nil {
		return err
	}
	return config.SaveSource(p, name, &config.SourceMeta{Type: config.ProviderCustom, BaseURL: baseURL, Model: model, Created: time.Now()})
}

type authDoc struct {
	Tokens *struct {
		AccountID    string `json:"account_id"`
		RefreshToken string `json:"refresh_token"`
	} `json:"tokens"`
}

// LooksLikeChatGPTAuth 判断 auth.json 是否含 ChatGPT 登录（只看结构，不输出内容）。
func LooksLikeChatGPTAuth(b []byte) bool {
	var d authDoc
	return json.Unmarshal(b, &d) == nil && d.Tokens != nil && d.Tokens.RefreshToken != ""
}

// AccountID 取 auth.json 里的 account_id，取不到返回空。
func AccountID(b []byte) string {
	var d authDoc
	if json.Unmarshal(b, &d) != nil || d.Tokens == nil {
		return ""
	}
	return d.Tokens.AccountID
}

// Info 是 source list 的一行。
type Info struct {
	Name     string
	Meta     config.SourceMeta
	HasLogin bool // openai：有 auth.json；custom：凭据管理器里有 key
	UsedBy   []string
}

// List 列出所有 source。
func List(p config.Paths, store cred.Store, m *config.Map) ([]Info, error) {
	names, err := config.ListSources(p)
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, n := range names {
		meta, err := config.LoadSource(p, n)
		if err != nil {
			return nil, err
		}
		in := Info{Name: n, Meta: *meta, UsedBy: m.HomesUsingSource(n)}
		if meta.Type == config.ProviderOpenAI {
			in.HasLogin = config.Exists(AuthFile(p, n))
		} else {
			_, err := store.Get(cred.TargetFor(meta.Tool, n))
			in.HasLogin = err == nil
		}
		out = append(out, in)
	}
	return out, nil
}
