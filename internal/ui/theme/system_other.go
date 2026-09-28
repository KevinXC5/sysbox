//go:build !darwin && !windows

package theme

import (
	"os/exec"
	"strings"
)

// SystemDark 读取 GNOME 的配色偏好；没有 gsettings 的桌面环境一律按浅色处理。
// 此函数只读取系统状态，可在后台调用。
func SystemDark() bool {
	out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "color-scheme").Output()
	return err == nil && strings.Contains(string(out), "dark")
}
