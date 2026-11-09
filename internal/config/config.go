// Package config 负责 acsw 的根目录、map.toml、source 元数据的读写和项目目录匹配。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/gofrs/flock"
)

// 固定的两种 provider 名。
const (
	ProviderOpenAI = "openai"
	ProviderCustom = "custom"
)

// Root 返回 acsw 根目录：ACSW_ROOT 优先，否则 ~/agent-source/codex。
func Root() (string, error) {
	if r := strings.TrimSpace(os.Getenv("ACSW_ROOT")); r != "" {
		return filepath.Abs(r)
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("找不到用户目录：%w", err)
	}
	return filepath.Join(h, "agent-source", "codex"), nil
}

// Paths 是根目录下各位置的计算器。
type Paths struct{ Root string }

func (p Paths) SourcesDir() string           { return filepath.Join(p.Root, "source") }
func (p Paths) HomesDir() string             { return filepath.Join(p.Root, "home") }
func (p Paths) SourceDir(name string) string { return filepath.Join(p.SourcesDir(), name) }
func (p Paths) HomeDir(name string) string   { return filepath.Join(p.HomesDir(), name) }

// CodexHome 返回 home 实际交给 codex 的 CODEX_HOME：官方别名 home 为 alias_of，否则为 HomeDir(name)。
// acsw codex、acsw last、status 都用它，别处不要自己拼路径。
func (p Paths) CodexHome(name string, e HomeEntry) string {
	if e.AliasOf != "" {
		return e.AliasOf
	}
	return p.HomeDir(name)
}
func (p Paths) MapFile() string  { return filepath.Join(p.Root, "map.toml") }
func (p Paths) LockFile() string { return filepath.Join(p.Root, ".acsw.lock") }

// BackupDir 是根目录下的备份区（删除的 source 移到这里）。
func (p Paths) BackupDir() string { return filepath.Join(p.Root, ".acsw-backup") }
func (p Paths) SourceMetaFile(name string) string {
	return filepath.Join(p.SourceDir(name), "acsw-source.toml")
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidName 检查 source/home 名称。
func ValidName(kind, name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("%s 名称 %q 不合法：只能用字母、数字、点、下划线、横线，且以字母或数字开头", kind, name)
	}
	return nil
}

// Project 是一条“项目目录前缀 → home”的映射。
type Project struct {
	Dir  string `toml:"dir"`
	Home string `toml:"home"`
}

// HomeEntry 记录 home 当前用的 source。
type HomeEntry struct {
	Source  string    `toml:"source"`
	Updated time.Time `toml:"updated,omitempty"`
	// AliasOf 非空时，这个 home 是官方 CODEX_HOME（如 %USERPROFILE%\.codex）的只读别名：
	// acsw codex 直接用该目录启动 codex，不注入 source/key；acsw 从不写入该目录。
	AliasOf string `toml:"alias_of,omitempty"`
}

// Map 对应 map.toml。
type Map struct {
	Projects []Project            `toml:"project"`
	Homes    map[string]HomeEntry `toml:"home"`
}

// LoadMap 读取 map.toml，不存在时返回空 Map。
func LoadMap(p Paths) (*Map, error) {
	m := &Map{Homes: map[string]HomeEntry{}}
	b, err := os.ReadFile(p.MapFile())
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 map.toml 失败：%w", err)
	}
	if _, err := toml.Decode(string(b), m); err != nil {
		return nil, fmt.Errorf("map.toml 格式有误：%w", err)
	}
	if m.Homes == nil {
		m.Homes = map[string]HomeEntry{}
	}
	return m, nil
}

// SaveMap 原子写入 map.toml。
func SaveMap(p Paths, m *Map) error {
	sort.SliceStable(m.Projects, func(i, j int) bool { return m.Projects[i].Dir < m.Projects[j].Dir })
	var sb strings.Builder
	sb.WriteString("# acsw 映射表：项目目录前缀 → home；home → 当前 source。可手改，改前先确认没有 acsw 在运行。\n\n")
	if err := toml.NewEncoder(&sb).Encode(m); err != nil {
		return fmt.Errorf("生成 map.toml 失败：%w", err)
	}
	return WriteFileAtomic(p.MapFile(), []byte(sb.String()), 0o644)
}

// SetProject 新增或更新一条项目映射。
func (m *Map) SetProject(dir, home string) {
	for i := range m.Projects {
		if SamePath(m.Projects[i].Dir, dir, CaseInsensitiveFS()) {
			m.Projects[i].Home = home
			return
		}
	}
	m.Projects = append(m.Projects, Project{Dir: dir, Home: home})
}

// HomesUsingSource 返回当前使用某 source 的 home 名（排序）。
func (m *Map) HomesUsingSource(source string) []string {
	var out []string
	for h, e := range m.Homes {
		if e.Source == source {
			out = append(out, h)
		}
	}
	sort.Strings(out)
	return out
}

// CaseInsensitiveFS 表示当前平台路径比较是否忽略大小写。
func CaseInsensitiveFS() bool { return runtime.GOOS == "windows" || runtime.GOOS == "darwin" }

// NormPath 把路径规整为可比较的形式（统一分隔符、去掉 \\?\ 前缀、去尾部分隔符）。
func NormPath(p string, caseInsensitive bool) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, `\\?\`)
	p = strings.ReplaceAll(p, `\`, "/")
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		p = strings.TrimRight(p, "/")
		if p == "" {
			p = "/"
		}
	}
	if len(p) == 2 && p[1] == ':' { // "M:" → "M:/"
		p += "/"
	}
	if caseInsensitive {
		p = strings.ToLower(p)
	}
	return p
}

// SamePath 比较两路径是否相同。
func SamePath(a, b string, ci bool) bool { return NormPath(a, ci) == NormPath(b, ci) }

// UnderDir 判断 dir 是否等于 prefix 或在其下面（按路径边界，不是字符串前缀）。
func UnderDir(dir, prefix string, ci bool) bool {
	d, p := NormPath(dir, ci), NormPath(prefix, ci)
	if d == p {
		return true
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return strings.HasPrefix(d, p)
}

// MatchHome 按最长前缀找出 dir 所属的 home。
func (m *Map) MatchHome(dir string, ci bool) (Project, bool) {
	best, ok := Project{}, false
	for _, pr := range m.Projects {
		if UnderDir(dir, pr.Dir, ci) {
			if !ok || len(NormPath(pr.Dir, ci)) > len(NormPath(best.Dir, ci)) {
				best, ok = pr, true
			}
		}
	}
	return best, ok
}

// SourceMeta 对应 source/<name>/acsw-source.toml。
type SourceMeta struct {
	Type    string    `toml:"type"` // openai 或 custom
	BaseURL string    `toml:"base_url,omitempty"`
	Model   string    `toml:"model,omitempty"`
	Created time.Time `toml:"created,omitempty"`
}

// Provider 返回 source 对应的 provider 名。
func (s SourceMeta) Provider() string {
	if s.Type == ProviderCustom {
		return ProviderCustom
	}
	return ProviderOpenAI
}

// LoadSource 读取 source 元数据。
func LoadSource(p Paths, name string) (*SourceMeta, error) {
	if err := ValidName("source", name); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p.SourceMetaFile(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("source %q 不存在，先用 acsw source add 建立", name)
	}
	if err != nil {
		return nil, fmt.Errorf("读取 source %q 失败：%w", name, err)
	}
	var s SourceMeta
	if _, err := toml.Decode(string(b), &s); err != nil {
		return nil, fmt.Errorf("source %q 的 acsw-source.toml 格式有误：%w", name, err)
	}
	if s.Type != ProviderOpenAI && s.Type != ProviderCustom {
		return nil, fmt.Errorf("source %q 的类型 %q 不认识，只能是 openai 或 custom", name, s.Type)
	}
	return &s, nil
}

// SaveSource 写 source 元数据。
func SaveSource(p Paths, name string, s *SourceMeta) error {
	var sb strings.Builder
	if err := toml.NewEncoder(&sb).Encode(s); err != nil {
		return err
	}
	return WriteFileAtomic(p.SourceMetaFile(name), []byte(sb.String()), 0o644)
}

// ListSources 列出所有已建的 source 名。
func ListSources(p Paths) ([]string, error) { return listDirs(p.SourcesDir(), true, p) }

// ListHomes 列出所有 home 目录名。
func ListHomes(p Paths) ([]string, error) { return listDirs(p.HomesDir(), false, p) }

func listDirs(dir string, needMeta bool, p Paths) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() || ValidName("x", e.Name()) != nil {
			continue
		}
		if needMeta {
			if _, err := os.Stat(p.SourceMetaFile(e.Name())); err != nil {
				continue
			}
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

// Lock 取得根目录下的全局文件锁（跨进程）。超时返回错误。
// ErrLockBusy 表示等锁超时：另一个 acsw 正在修改配置。
var ErrLockBusy = errors.New("另一个 acsw 正在修改配置，请稍后再试")

func Lock(p Paths, timeout time.Duration) (unlock func(), err error) {
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		return nil, fmt.Errorf("建根目录失败：%w", err)
	}
	fl := flock.New(p.LockFile())
	deadline := time.Now().Add(timeout)
	for {
		ok, err := fl.TryLock()
		if err != nil {
			return nil, fmt.Errorf("加锁失败：%w", err)
		}
		if ok {
			return func() { _ = fl.Unlock() }, nil
		}
		if time.Now().After(deadline) {
			return nil, ErrLockBusy
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// WriteFileAtomic 先写临时文件再改名，避免写一半。
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		_ = os.Chmod(tmp, perm)
		werr = os.Rename(tmp, path)
	}
	if werr != nil {
		_ = os.Remove(tmp)
		return werr
	}
	return nil
}

// CopyFile 复制文件（原子写目标）。
func CopyFile(src, dst string, perm os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return WriteFileAtomic(dst, b, perm)
}

// Exists 判断路径是否存在。
func Exists(p string) bool { _, err := os.Stat(p); return err == nil }
