//go:build windows

package selfupdate

import (
	"os"
	"os/exec"
)

// Restart 在当前终端运行新版本，等待其退出后再返回。
// Windows 没有 syscall.Exec；旧进程必须继续等待，避免 PowerShell 提前接管终端输入。
func Restart(exe string, args, env []string) error {
	var extra []string
	if len(args) > 0 {
		extra = args[1:]
	}
	cmd := exec.Command(exe, extra...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
