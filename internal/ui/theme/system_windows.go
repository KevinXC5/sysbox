package theme

import "golang.org/x/sys/windows/registry"

// SystemDark 读取 Windows 应用的深浅色设置，AppsUseLightTheme 为 0 表示深色。
// 此函数只读取系统状态，可在后台调用。
func SystemDark() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}
