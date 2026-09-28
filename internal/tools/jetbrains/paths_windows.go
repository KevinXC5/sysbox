//go:build windows

package jetbrains

import "path/filepath"

// platformOptions Windows：缓存与日志在 %LOCALAPPDATA%\JetBrains，
// 日志是各产品目录下的 log 子目录；配置在 %APPDATA%\JetBrains，不能删。
// 环境变量为空时回退到家目录下的 AppData，不读取本机真实目录，方便注入测试。
func platformOptions(home string, lookup func(string) string) Options {
	local := lookup("LOCALAPPDATA")
	if local == "" {
		local = filepath.Join(home, "AppData", "Local")
	}
	roaming := lookup("APPDATA")
	if roaming == "" {
		roaming = filepath.Join(home, "AppData", "Roaming")
	}
	cache := resolve(filepath.Join(local, "JetBrains"))
	return Options{
		Home:           home,
		CacheRoot:      cache,
		LogRoot:        "", // 日志在各产品缓存目录的 log 子目录，扫描时按产品展开
		LogInsideCache: true,
		AppSupport:     resolve(filepath.Join(roaming, "JetBrains")),
		CacheLabel:     "Local\\JetBrains",
		LogLabel:       "缓存内 log",
		ConfigLabel:    "Roaming\\JetBrains",
	}
}

// forbiddenRoots 盘符根、家目录和 AppData 本身不能删
func forbiddenRoots(o Options) []string {
	roots := []string{string(filepath.Separator), o.Home}
	if vol := filepath.VolumeName(o.Home); vol != "" {
		roots = append(roots, vol+string(filepath.Separator))
	}
	appData := filepath.Join(o.Home, "AppData")
	roots = append(roots,
		appData,
		filepath.Join(appData, "Local"),
		filepath.Join(appData, "Roaming"),
	)
	return roots
}

// configNote 面向用户的配置目录说明
func configNote() string {
	return "不改动 %APPDATA%\\JetBrains（设置、插件本体）"
}
