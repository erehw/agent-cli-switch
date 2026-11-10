package source

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"acsw/internal/claude"
	"acsw/internal/config"
	"acsw/internal/cred"
)

// ReservedClaudeNames 是 claude source 不能用的名字（会和 acsw claude export/key/cmd 子命令撞车）。
var ReservedClaudeNames = []string{"export", "key", "cmd"}

// ClaudeFields 是 claude source 除 key 以外的设置。
type ClaudeFields struct {
	BaseURL  string            `json:"base_url"`
	Model    string            `json:"model"`
	Opus     string            `json:"opus_model"`
	Sonnet   string            `json:"sonnet_model"`
	Haiku    string            `json:"haiku_model"`
	Fable    string            `json:"fable_model"`
	KeyMode  string            `json:"key_mode"`
	ExtraEnv map[string]string `json:"extra_env,omitempty"`
}

// ClaudeFieldsOf 从元数据取出 claude 设置（key_mode 为空时按 helper）。
func ClaudeFieldsOf(m config.SourceMeta) ClaudeFields {
	km := m.KeyMode
	if km == "" {
		km = string(claude.ModeHelper)
	}
	return ClaudeFields{BaseURL: m.BaseURL, Model: m.Model, Opus: m.OpusModel, Sonnet: m.SonnetModel, Haiku: m.HaikuModel, Fable: m.FableModel, KeyMode: km, ExtraEnv: m.ExtraEnv}
}

// ClaudeSpec 把元数据变成生成 settings 用的 Spec。
func ClaudeSpec(m config.SourceMeta) claude.Spec {
	return claude.Spec{BaseURL: m.BaseURL, Model: m.Model, Opus: m.OpusModel, Sonnet: m.SonnetModel, Haiku: m.HaikuModel, Fable: m.FableModel, Extra: m.ExtraEnv}
}

// normalizeClaude 校验并规整 claude 设置。
func normalizeClaude(f ClaudeFields) (ClaudeFields, error) {
	var err error
	if f.BaseURL, f.Model, err = validateCustom(f.BaseURL, f.Model); err != nil {
		return f, err
	}
	for _, p := range []*string{&f.Opus, &f.Sonnet, &f.Haiku, &f.Fable} {
		*p = strings.TrimSpace(*p)
		if hasCtl(*p) {
			return f, fmt.Errorf("模型名 %q 含控制字符", *p)
		}
	}
	m, err := claude.ParseMode(f.KeyMode)
	if err != nil {
		return f, err
	}
	if m == "" {
		m = claude.ModeHelper
	}
	f.KeyMode = string(m)
	if len(f.ExtraEnv) == 0 {
		f.ExtraEnv = nil
	}
	if err := claude.ValidateExtraEnv(f.ExtraEnv); err != nil {
		return f, err
	}
	return f, nil
}

func applyClaude(m *config.SourceMeta, f ClaudeFields) {
	m.BaseURL, m.Model = f.BaseURL, f.Model
	m.OpusModel, m.SonnetModel, m.HaikuModel, m.FableModel = f.Opus, f.Sonnet, f.Haiku, f.Fable
	m.KeyMode = f.KeyMode
	if m.KeyMode == string(claude.ModeHelper) {
		m.KeyMode = "" // 默认值不写进文件
	}
	m.ExtraEnv = f.ExtraEnv
}

// AddClaude 建 claude source：key 存凭据管理器 acsw/claude/<name>，元数据写 acsw-source.toml。
func AddClaude(p config.Paths, store cred.Store, name string, f ClaudeFields, key string) error {
	for _, r := range ReservedClaudeNames {
		if strings.EqualFold(name, r) {
			return fmt.Errorf("claude source 不能叫 %s（和 acsw claude %s 子命令撞车）", name, r)
		}
	}
	f, err := normalizeClaude(f)
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
	if err := store.Set(cred.TargetFor(config.ToolClaude, name), key); err != nil {
		return err
	}
	m := &config.SourceMeta{Tool: config.ToolClaude, Type: config.ProviderCustom, Created: time.Now()}
	applyClaude(m, f)
	return config.SaveSource(p, name, m)
}

// LoadClaude 读 claude source；不是 claude source 时报错（错误里说明是哪个工具）。
func LoadClaude(p config.Paths, name string) (*config.SourceMeta, error) {
	meta, err := config.LoadSource(p, name)
	if err != nil {
		return nil, err
	}
	if !meta.IsClaude() {
		return nil, &ToolError{Name: name, Want: config.ToolClaude, Got: meta.ToolName()}
	}
	return meta, nil
}

// ToolError 表示 source 属于另一个工具。
type ToolError struct{ Name, Want, Got string }

func (e *ToolError) Error() string {
	return fmt.Sprintf("source %s 是给 %s 用的，不是 %s source", e.Name, e.Got, e.Want)
}

// ClaudeEdit 描述对 claude source 的修改；nil 表示不改。ExtraEnv 非 nil 时整体替换（空 map 表示清空）。
type ClaudeEdit struct {
	BaseURL  *string
	Model    *string
	Opus     *string
	Sonnet   *string
	Haiku    *string
	Fable    *string
	KeyMode  *string
	ExtraEnv map[string]string
	Key      string
}

// EditClaude 修改 claude source。先写 key 再写元数据；key 写失败就什么都不改。返回元数据是否变了、key 是否变了。
func EditClaude(p config.Paths, store cred.Store, name string, e ClaudeEdit) (meta *config.SourceMeta, metaChanged, keyChanged bool, err error) {
	meta, err = LoadClaude(p, name)
	if err != nil {
		return nil, false, false, err
	}
	f := ClaudeFieldsOf(*meta)
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	set(&f.BaseURL, e.BaseURL)
	set(&f.Model, e.Model)
	set(&f.Opus, e.Opus)
	set(&f.Sonnet, e.Sonnet)
	set(&f.Haiku, e.Haiku)
	set(&f.Fable, e.Fable)
	set(&f.KeyMode, e.KeyMode)
	if e.ExtraEnv != nil {
		f.ExtraEnv = e.ExtraEnv
	}
	if f, err = normalizeClaude(f); err != nil {
		return nil, false, false, err
	}
	old := ClaudeFieldsOf(*meta)
	nm := *meta
	applyClaude(&nm, f)
	metaChanged = !sameFields(old, ClaudeFieldsOf(nm))
	if k := strings.TrimSpace(e.Key); k != "" {
		if err := store.Set(cred.TargetFor(config.ToolClaude, name), k); err != nil {
			return nil, false, false, err
		}
		keyChanged = true
	}
	if metaChanged {
		if err := config.SaveSource(p, name, &nm); err != nil {
			return nil, false, keyChanged, fmt.Errorf("保存 source %s 失败：%w", name, err)
		}
	}
	return &nm, metaChanged, keyChanged, nil
}

func sameFields(a, b ClaudeFields) bool {
	if a.BaseURL != b.BaseURL || a.Model != b.Model || a.Opus != b.Opus || a.Sonnet != b.Sonnet || a.Haiku != b.Haiku || a.Fable != b.Fable || a.KeyMode != b.KeyMode || len(a.ExtraEnv) != len(b.ExtraEnv) {
		return false
	}
	for k, v := range a.ExtraEnv {
		if bv, ok := b.ExtraEnv[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// RemoveClaude 删除 claude source：目录移到 <根>/.acsw-backup/，删掉生成的 settings 文件（plain 文件里有明文 key，不备份），再删凭据。
func RemoveClaude(p config.Paths, store cred.Store, name string, now time.Time) (string, error) {
	if _, err := LoadClaude(p, name); err != nil {
		return "", err
	}
	if err := os.MkdirAll(p.BackupDir(), 0o700); err != nil {
		return "", fmt.Errorf("建备份目录失败：%w", err)
	}
	dst := filepath.Join(p.BackupDir(), now.Format("20060102-150405.000")+"-source-"+name)
	if err := os.Rename(p.SourceDir(name), dst); err != nil {
		return "", fmt.Errorf("把 source 目录移到备份区失败（什么都没删）：%w", err)
	}
	var errs []string
	for _, f := range []string{p.ClaudeSettingsFile(name, false), p.ClaudeSettingsFile(name, true)} {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Sprintf("删除 %s 失败：%v", f, err))
		}
	}
	t := cred.TargetFor(config.ToolClaude, name)
	if err := store.Delete(t); err != nil && !errors.Is(err, cred.ErrNotFound) {
		errs = append(errs, fmt.Sprintf("删除凭据 %s 失败：%v，请到凭据管理器手动删除", t, err))
	}
	if len(errs) > 0 {
		return dst, fmt.Errorf("source 目录已移到 %s，但：%s", dst, strings.Join(errs, "；"))
	}
	return dst, nil
}
