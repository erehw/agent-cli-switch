//go:build windows

package proc

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const stillActive = 259

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// 拒绝访问说明进程存在但权限不够，按存活处理更安全。
		return err == windows.ERROR_ACCESS_DENIED
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	return code == stillActive
}

func snapshot() ([]PInfo, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	var out []PInfo
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		out = append(out, PInfo{
			PID:  int(pe.ProcessID),
			PPID: int(pe.ParentProcessID),
			Name: strings.ToLower(windows.UTF16ToString(pe.ExeFile[:])),
		})
	}
	return out, nil
}
