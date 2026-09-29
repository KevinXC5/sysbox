package theme

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// SystemDark 读取 macOS 外观设置，浅色模式下该键不存在。
// 此函数只读取系统状态；读取超时或失败时使用浅色模式。
func SystemDark() bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/defaults", "read", "-g", "AppleInterfaceStyle")
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "Dark"
}
