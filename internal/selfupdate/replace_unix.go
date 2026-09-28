//go:build !windows

package selfupdate

import "os"

// replaceExecutable 原子替换可执行文件。Unix 上正在运行的进程可以安全地被 rename 覆盖。
func replaceExecutable(tmp, exe string) error {
	return os.Rename(tmp, exe)
}
