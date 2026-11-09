//go:build !windows

package proc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// snapshot 在 Linux 上扫 /proc；其他平台返回空。
func snapshot() ([]PInfo, error) {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil, nil
	}
	var out []PInfo
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		s := string(b)
		l, r := strings.Index(s, "("), strings.LastIndex(s, ")")
		if l < 0 || r < l {
			continue
		}
		f := strings.Fields(s[r+1:])
		if len(f) < 2 {
			continue
		}
		ppid, _ := strconv.Atoi(f[1])
		out = append(out, PInfo{PID: pid, PPID: ppid, Name: strings.ToLower(s[l+1 : r])})
	}
	return out, nil
}
