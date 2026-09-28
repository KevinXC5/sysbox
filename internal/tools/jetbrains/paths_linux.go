//go:build linux

package jetbrains

import "path/filepath"

// platformOptions Linux：缓存遵循 XDG_CACHE_HOME，默认 ~/.cache/JetBrains，
// 日志在各产品目录的 log 子目录；配置遵循 XDG_CONFIG_HOME，默认 ~/.config/JetBrains；
// 插件等用户数据遵循 XDG_DATA_HOME，默认 ~/.local/share/JetBrains。后两者不能删。
func platformOptions(home string, lookup func(string) string) Options {
	cacheBase := lookup("XDG_CACHE_HOME")
	if cacheBase == "" {
		cacheBase = filepath.Join(home, ".cache")
	}
	configBase := lookup("XDG_CONFIG_HOME")
	if configBase == "" {
		configBase = filepath.Join(home, ".config")
	}
	dataBase := lookup("XDG_DATA_HOME")
	if dataBase == "" {
		dataBase = filepath.Join(home, ".local", "share")
	}
	return Options{
		Home:           home,
		CacheRoot:      resolve(filepath.Join(cacheBase, "JetBrains")),
		LogRoot:        "", // 日志在各产品缓存目录的 log 子目录，扫描时按产品展开
		LogInsideCache: true,
		AppSupport:     resolve(filepath.Join(configBase, "JetBrains")),
		DataRoot:       resolve(filepath.Join(dataBase, "JetBrains")),
		CacheLabel:     "cache/JetBrains",
		LogLabel:       "缓存内 log",
		ConfigLabel:    "config/JetBrains",
		DataLabel:      "local/share",
	}
}

// forbiddenRoots 根目录、家目录和 XDG 默认父目录本身不能删
func forbiddenRoots(o Options) []string {
	return []string{
		"/",
		o.Home,
		filepath.Join(o.Home, ".cache"),
		filepath.Join(o.Home, ".config"),
		filepath.Join(o.Home, ".local"),
		filepath.Join(o.Home, ".local", "share"),
	}
}

// configNote 面向用户的配置与用户数据说明
func configNote() string {
	return "不改动 ~/.config/JetBrains 与 ~/.local/share/JetBrains（设置、插件本体）"
}
