package vscode

// 只认固定白名单里的可重建缓存。其余目录一律保留，避免误删设置、会话和未知数据。

// cleanCache 应用数据根下可本地重建的缓存目录。键是目录名，值是给用户看的说明。
var cleanCache = map[string]string{
	"Cache":                "HTTP 缓存，启动后按需重建",
	"CachedData":           "V8 代码缓存，启动后按需重建",
	"Code Cache":           "JS 代码缓存，启动后按需重建",
	"GPUCache":             "GPU 着色器缓存，启动后按需重建",
	"CachedExtensionVSIXs": "已安装扩展的安装包缓存，不影响已装扩展",
	"logs":                 "运行日志，启动后重新生成",
	"Crashpad":             "崩溃转储，删除不影响设置与会话",
	"CachedConfigurations": "配置缓存，启动后按需重建",
	"CachedProfilesData":   "配置档缓存，启动后按需重建",
	"DawnGraphiteCache":    "Dawn 图形缓存，启动后按需重建",
	"DawnWebGPUCache":      "Dawn 图形缓存，启动后按需重建",
}

// 必须整目录保护的用户数据。即使名字碰巧命中白名单也不进入可清理。
var protectedDirs = map[string]string{
	"User":            "用户设置、快捷键、代码片段与工作区状态",
	"Backups":         "未保存文件的备份，删除后无法恢复",
	"Local Storage":   "本地存储，可能含登录与界面状态",
	"Session Storage": "会话存储，删除后当前会话状态丢失",
	"WebStorage":      "IndexedDB 等网页存储，可能含扩展状态",
	"Partitions":      "站点分区数据，可能含登录状态",
}

// noteUnknown 未识别条目的说明
const noteUnknown = "未识别的条目，用途未知，默认保留"

// noteSymlink 符号链接的说明
const noteSymlink = "符号链接，不跟随目标，默认保留"

// noteUnread 无法读取时的说明
const noteUnread = "无法读取，为避免误删而跳过"
