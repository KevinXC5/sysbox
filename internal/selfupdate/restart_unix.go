//go:build !windows

package selfupdate

import "syscall"

// Restart 用新版本原地替换当前进程。调用前应已完成可执行文件替换。
func Restart(exe string, args, env []string) error {
	return syscall.Exec(exe, args, env)
}
