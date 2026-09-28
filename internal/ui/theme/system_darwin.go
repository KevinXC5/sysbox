package theme

import (
	"os/exec"
	"strings"
)

// SystemDark 读取 macOS 外观设置，浅色模式下该键不存在。
// 此函数只读取系统状态，可在后台调用。
func SystemDark() bool {
	out, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").Output()
	return err == nil && strings.TrimSpace(string(out)) == "Dark"
}
