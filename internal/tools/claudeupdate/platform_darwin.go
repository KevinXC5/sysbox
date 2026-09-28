//go:build darwin

package claudeupdate

import "golang.org/x/sys/unix"

// rosettaTranslated 当前进程是否经 Rosetta 2 转译。
// sysctl.proc_translated 为 1 表示 Apple 芯片上运行的 x64 进程，应改下 arm64 原生包。
func rosettaTranslated() bool {
	v, err := unix.SysctlUint32("sysctl.proc_translated")
	return err == nil && v == 1
}
