//go:build windows

package sysx

import (
	"context"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Procs 用 Toolhelp 快照列出进程。只能拿到 pid 与 exe 文件名，
// CPU、内存等字段保持为零：调用方只用名字判断是否在运行，缺失这些字段不会误判。
func Procs(ctx context.Context, r Runner) ([]Proc, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return nil, err
	}
	var procs []Proc
	for {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if name != "" && entry.ProcessID != 0 {
			procs = append(procs, Proc{PID: int(entry.ProcessID), Path: name})
		}
		err = windows.Process32Next(snap, &entry)
		if err != nil {
			if err == syscall.ERROR_NO_MORE_FILES {
				break
			}
			return nil, err
		}
	}
	return procs, nil
}
