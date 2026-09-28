//go:build windows

package selfupdate

import (
	"os"
	"path/filepath"
)

// CleanupOld 尝试删除上次升级留下的 exe.old。
// 进程仍占用该文件时删除会失败，静默忽略，等下次启动再试。
func CleanupOld() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	_ = os.Remove(exe + oldSuffix)
}
