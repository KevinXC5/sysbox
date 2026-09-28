//go:build linux

package claudeupdate

import (
	"os"
	"os/exec"
	"strings"
)

// muslLibc 判断当前系统是否使用 musl，与官方 install.sh 的探测方式一致：
// 标准 musl 动态库存在，或 ldd /bin/ls 的输出里出现 musl。
func muslLibc() bool {
	if _, err := os.Stat("/lib/libc.musl-x86_64.so.1"); err == nil {
		return true
	}
	if _, err := os.Stat("/lib/libc.musl-aarch64.so.1"); err == nil {
		return true
	}
	out, err := exec.Command("ldd", "/bin/ls").CombinedOutput()
	return err == nil && strings.Contains(string(out), "musl")
}
