//go:build darwin

package jetbrains

import "path/filepath"

// platformOptions macOS：缓存、日志、配置各自独立，行为与改造前一致。
// 日志不在缓存目录内部。
func platformOptions(home string, _ func(string) string) Options {
	lib := filepath.Join(home, "Library")
	return Options{
		Home:           home,
		CacheRoot:      resolve(filepath.Join(lib, "Caches", "JetBrains")),
		LogRoot:        resolve(filepath.Join(lib, "Logs", "JetBrains")),
		LogInsideCache: false,
		AppSupport:     resolve(filepath.Join(lib, "Application Support", "JetBrains")),
		CacheLabel:     "Caches/JetBrains",
		LogLabel:       "Logs/JetBrains",
		ConfigLabel:    "Application Support",
	}
}

// forbiddenRoots 家目录和 ~/Library 本身不能删
func forbiddenRoots(o Options) []string {
	return []string{"/", o.Home, filepath.Join(o.Home, "Library")}
}

// configNote 面向用户的配置目录说明
func configNote() string {
	return "不改动 Application Support（设置、插件本体）"
}
