//go:build windows

package selfupdate

import (
	"os"
	"os/exec"
)

// Restart 启动新进程后退出当前进程。Windows 没有 syscall.Exec，也不能覆盖正在运行的 exe，
// 所以这里另起进程，由调用方在返回前结束当前进程。
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
	return cmd.Start()
}
