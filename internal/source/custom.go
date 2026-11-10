package source

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"acsw/internal/config"
	"acsw/internal/cred"
)

// ErrInUse 表示 source 正被某些 home 使用。
var ErrInUse = errors.New("source 正在被 home 使用")

// InUseError 说明是哪些 home 在用。
type InUseError struct {
	Name  string
	Homes []string
}

func (e *InUseError) Error() string {
	return fmt.Sprintf("source %s 正被 home %s 使用，不能删除。请先把这些 home 切到别的 source", e.Name, strings.Join(e.Homes, ", "))
}

// Is 让 errors.Is(err, ErrInUse) 成立。
func (e *InUseError) Is(t error) bool { return t == ErrInUse }

func hasCtl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// validateCustom 校验并规整 custom source 的 base_url 和 model（AddCustom 与 EditCustom 共用）。
func validateCustom(baseURL, model string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || hasCtl(baseURL) {
		return "", "", fmt.Errorf("--base-url %q 不是合法的 http(s) 地址", baseURL)
	}
	if strings.TrimSpace(model) == "" {
		return "", "", errors.New("custom 类型需要 --model")
	}
	if hasCtl(model) {
		return "", "", fmt.Errorf("model %q 含控制字符", model)
	}
	return strings.TrimRight(strings.TrimSpace(baseURL), "/"), strings.TrimSpace(model), nil
}

// ValidateBaseURL 校验并规整 base_url（http/https，去掉结尾的 /）。
func ValidateBaseURL(baseURL string) (string, error) {
	b, _, err := validateCustom(baseURL, "x")
	return b, err
}

func loadCustom(p config.Paths, name string) (*config.SourceMeta, error) {
	meta, err := config.LoadSource(p, name)
	if err != nil {
		return nil, err
	}
	if meta.Type != config.ProviderCustom {
		return nil, fmt.Errorf("source %s 是 %s 类型，这里只能改 custom 类型", name, meta.Type)
	}
	if meta.IsClaude() {
		return nil, fmt.Errorf("source %s 是 claude source，请用 claude 的参数修改（acsw source edit %s --base-url/--model/--opus ...）", name, name)
	}
	return meta, nil
}

// EditResult 说明 EditCustom 改了什么。
type EditResult struct {
	MetaChanged bool // base_url 或 model 变了
	KeyChanged  bool // 写了新 key
	Meta        config.SourceMeta
}

// PlanEditCustom 只计算修改后的 base_url/model，不写任何东西。baseURL/model 为空表示不改。
// 返回的 EditResult 里 MetaChanged 表示 base_url 或 model 会变，Meta 是修改后的元数据。
func PlanEditCustom(p config.Paths, name, baseURL, model string) (EditResult, error) {
	var res EditResult
	meta, err := loadCustom(p, name)
	if err != nil {
		return res, err
	}
	nb, nm := meta.BaseURL, meta.Model
	if strings.TrimSpace(baseURL) != "" {
		nb = baseURL
	}
	if strings.TrimSpace(model) != "" {
		nm = model
	}
	nb, nm, err = validateCustom(nb, nm)
	if err != nil {
		return res, err
	}
	res.MetaChanged = nb != meta.BaseURL || nm != meta.Model
	meta.BaseURL, meta.Model = nb, nm
	res.Meta = *meta
	return res, nil
}

// EditCustom 修改 custom source。baseURL/model/key 为空表示不改。
// key 只写凭据管理器；先写 key 再写元数据，key 写失败就什么都不改。
func EditCustom(p config.Paths, store cred.Store, name, baseURL, model, key string) (EditResult, error) {
	res, err := PlanEditCustom(p, name, baseURL, model)
	if err != nil {
		return res, err
	}
	if key = strings.TrimSpace(key); key != "" {
		if err := store.Set(cred.Target(name), key); err != nil {
			return EditResult{}, err
		}
		res.KeyChanged = true
	}
	if res.MetaChanged {
		meta := res.Meta
		if err := config.SaveSource(p, name, &meta); err != nil {
			return res, fmt.Errorf("保存 source %s 失败：%w", name, err)
		}
	}
	return res, nil
}

// RemoveCustom 删除 custom source：有 home 在用就拒绝；否则把 source 目录移到 <根>/.acsw-backup/，再删凭据。
// 返回备份目录。调用方负责加锁。
func RemoveCustom(p config.Paths, store cred.Store, m *config.Map, name string, now time.Time) (string, error) {
	if _, err := loadCustom(p, name); err != nil {
		return "", err
	}
	if homes := m.HomesUsingSource(name); len(homes) > 0 {
		return "", &InUseError{Name: name, Homes: homes}
	}
	if err := os.MkdirAll(p.BackupDir(), 0o700); err != nil {
		return "", fmt.Errorf("建备份目录失败：%w", err)
	}
	dst := filepath.Join(p.BackupDir(), now.Format("20060102-150405.000")+"-source-"+name)
	if err := os.Rename(p.SourceDir(name), dst); err != nil {
		return "", fmt.Errorf("把 source 目录移到备份区失败（什么都没删）：%w", err)
	}
	if err := store.Delete(cred.Target(name)); err != nil {
		return dst, fmt.Errorf("source 目录已移到 %s，但删除凭据 %s 失败：%w，请到凭据管理器手动删除", dst, cred.Target(name), err)
	}
	return dst, nil
}
