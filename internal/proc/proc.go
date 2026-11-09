// Package proc 记录 acsw run 启动的进程，并判断某个 home 是否空闲。
package proc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PInfo 是进程快照中的一项。
type PInfo struct {
	PID, PPID int
	Name      string // 小写可执行名，如 codex.exe / codex
}

// Checker 判断进程是否存活，并能取进程快照。可在测试中替换。
type Checker interface {
	Alive(pid int) bool
	Snapshot() ([]PInfo, error)
}

// System 是真实系统实现。
type System struct{}

func (System) Alive(pid int) bool         { return alive(pid) }
func (System) Snapshot() ([]PInfo, error) { return snapshot() }

// IsCodexName 判断进程名是不是 codex 本体。
func IsCodexName(n string) bool { return n == "codex.exe" || n == "codex" }

// UnmanagedCodex 返回不在 managed 进程树下的 codex 进程号。
// managed 是 acsw run 登记的 pid（acsw 自身和其子进程）。npm 安装的 codex 是 codex.cmd→node→codex.exe，
// 所以按父进程链向上找（最多 16 层）。
func UnmanagedCodex(c Checker, managed map[int]bool) []int {
	ps, err := c.Snapshot()
	if err != nil {
		return nil
	}
	parent := map[int]int{}
	for _, p := range ps {
		parent[p.PID] = p.PPID
	}
	var out []int
	for _, p := range ps {
		if !IsCodexName(p.Name) {
			continue
		}
		cur, ok := p.PID, false
		for i := 0; i < 16 && cur > 0; i++ {
			if managed[cur] {
				ok = true
				break
			}
			nxt, has := parent[cur]
			if !has || nxt == cur {
				break
			}
			cur = nxt
		}
		if !ok {
			out = append(out, p.PID)
		}
	}
	return out
}

// Entry 是一条运行登记。
type Entry struct {
	PID      int       `json:"pid"`       // acsw 自身 pid
	ChildPID int       `json:"child_pid"` // codex 子进程 pid
	Source   string    `json:"source"`
	Cwd      string    `json:"cwd"`
	Started  time.Time `json:"started"`
}

// RunDir 是 home 下的登记目录。
func RunDir(home string) string { return filepath.Join(home, ".acsw-run") }

// Register 写一条登记，返回清理函数。
func Register(home string, e Entry) (func(), error) {
	dir := RunDir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("建登记目录失败：%w", err)
	}
	p := filepath.Join(dir, strconv.Itoa(e.PID)+".json")
	b, _ := json.MarshalIndent(e, "", "  ")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return nil, fmt.Errorf("写运行登记失败：%w", err)
	}
	return func() { _ = os.Remove(p) }, nil
}

// Update 覆盖已有登记（例如补上子进程 pid）。
func Update(home string, e Entry) error {
	p := filepath.Join(RunDir(home), strconv.Itoa(e.PID)+".json")
	b, _ := json.MarshalIndent(e, "", "  ")
	return os.WriteFile(p, b, 0o644)
}

// Running 返回 home 下仍存活的登记；已退出的登记顺手删掉。
func Running(home string, c Checker) []Entry {
	ents, err := os.ReadDir(RunDir(home))
	if err != nil {
		return nil
	}
	var out []Entry
	orphans := &orphanIndex{c: c}
	for _, de := range ents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		p := filepath.Join(RunDir(home), de.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var e Entry
		if json.Unmarshal(b, &e) != nil || e.PID <= 0 {
			_ = os.Remove(p)
			continue
		}
		if c.Alive(e.PID) || (e.ChildPID > 0 && c.Alive(e.ChildPID)) || orphans.has(e) {
			out = append(out, e)
		} else {
			_ = os.Remove(p)
		}
	}
	return out
}

// Fake 是测试用实现。
type Fake struct {
	AlivePIDs map[int]bool
	Procs     []PInfo
}

func (f *Fake) Alive(pid int) bool         { return f.AlivePIDs[pid] }
func (f *Fake) Snapshot() ([]PInfo, error) { return f.Procs, nil }

// orphanIndex 处理“acsw 或中间的 codex.cmd 被强杀、codex 本体还在跑”的情况：
// Windows 进程快照里保留原父进程号，所以顺着父进程链能找到登记过的 pid。
// 只认 codex 本体，降低 pid 复用造成误判的概率。快照只在需要时取一次。
type orphanIndex struct {
	c      Checker
	loaded bool
	parent map[int]int
	codex  []int
}

func (o *orphanIndex) has(e Entry) bool {
	if !o.loaded {
		o.loaded = true
		ps, err := o.c.Snapshot()
		if err != nil {
			return false
		}
		o.parent = map[int]int{}
		for _, p := range ps {
			o.parent[p.PID] = p.PPID
			if IsCodexName(p.Name) {
				o.codex = append(o.codex, p.PID)
			}
		}
	}
	for _, pid := range o.codex {
		cur := pid
		for i := 0; i < 16 && cur > 0; i++ {
			if cur == e.PID || (e.ChildPID > 0 && cur == e.ChildPID) {
				return true
			}
			nxt, ok := o.parent[cur]
			if !ok || nxt == cur {
				break
			}
			cur = nxt
		}
	}
	return false
}
