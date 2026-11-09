package proc

import (
	"os"
	"testing"
)

func TestRegisterRunningCleanup(t *testing.T) {
	h := t.TempDir()
	f := &Fake{AlivePIDs: map[int]bool{10: true}}
	c1, err := Register(h, Entry{PID: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Register(h, Entry{PID: 11, ChildPID: 12}); err != nil {
		t.Fatal(err)
	}
	if got := Running(h, f); len(got) != 1 || got[0].PID != 10 {
		t.Fatal(got)
	}
	if _, err := os.Stat(RunDir(h) + "/11.json"); !os.IsNotExist(err) {
		t.Fatal("死进程登记应被清理")
	}
	c1()
	if got := Running(h, f); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestSystemAliveSelf(t *testing.T) {
	if !(System{}).Alive(os.Getpid()) {
		t.Fatal("自己应存活")
	}
	if (System{}).Alive(0) {
		t.Fatal("pid 0 不算")
	}
	if _, err := (System{}).Snapshot(); err != nil {
		t.Fatal(err)
	}
}

func TestUnmanagedCodex(t *testing.T) {
	f := &Fake{Procs: []PInfo{{1, 0, "init"}, {5, 1, "acsw"}, {6, 5, "node"}, {7, 6, "codex"}, {9, 1, "codex.exe"}, {10, 10, "codex"}}}
	got := UnmanagedCodex(f, map[int]bool{5: true})
	if len(got) != 2 || got[0] != 9 || got[1] != 10 {
		t.Fatal(got)
	}
}

func TestRunningOrphanCodex(t *testing.T) {
	h := t.TempDir()
	// acsw(20) 和 cmd(21) 都已被杀，node(22)、codex(23) 还在；快照里保留原父进程号。
	f := &Fake{AlivePIDs: map[int]bool{22: true, 23: true}, Procs: []PInfo{{22, 21, "node.exe"}, {23, 22, "codex.exe"}}}
	if _, err := Register(h, Entry{PID: 20, ChildPID: 21}); err != nil {
		t.Fatal(err)
	}
	if got := Running(h, f); len(got) != 1 || got[0].PID != 20 {
		t.Fatalf("孤儿 codex 应算作仍在运行：%v", got)
	}
	f.Procs = []PInfo{{22, 21, "node.exe"}}
	if got := Running(h, f); len(got) != 0 {
		t.Fatalf("codex 本体退出后应清理登记：%v", got)
	}
}
