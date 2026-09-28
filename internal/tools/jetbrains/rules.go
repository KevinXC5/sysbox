package jetbrains

// 清理规则。键是缓存目录下的条目名，值是展示给用户的说明。

// skipRules 删除后需要联网重新下载，一律跳过
var skipRules = map[string]string{
	"acp-agents":         "AI Agent 本体，无法读取时整体跳过",
	"full-line":          "整行补全模型，删除后需要重新下载",
	"splash":             "启动画面资源，删除后需要重新下载",
	"kubernetes":         "Kubernetes 插件下载的工具，删除后需要重新下载",
	"global-model-cache": "全局模型缓存，删除后需要重新下载",
}

// keepRules IDE 元数据，必须保留
var keepRules = map[string]string{
	".appinfo": "IDE 元数据，保留",
	".home":    "IDE 元数据，保留",
	".pid":     "IDE 进程号记录，保留",
}

// localHistory 本地变更历史，删除后无法重建，默认保留，可手动纳入清理
const (
	localHistoryName = "LocalHistory"
	localHistoryNote = "本地变更历史，删除后无法恢复"
)

// cleanRules 可本地重建的目录，基本不耗流量
var cleanRules = map[string]string{
	"index":                 "项目索引，打开项目时自动重建",
	"caches":                "IDE 内部缓存，按需重建",
	"jcef_cache":            "内嵌浏览器缓存",
	"jcef_cache_gpu":        "内嵌浏览器 GPU 缓存",
	"plugins":               "插件更新包与元数据，已装插件不受影响",
	"tmp":                   "临时文件",
	"code-provenance":       "代码溯源缓存",
	"projects":              "项目级缓存",
	"editor":                "编辑器状态缓存",
	"compile-server":        "编译服务缓存",
	"compiler":              "编译输出缓存",
	"ai-assistant-log-data": "AI 助手日志数据",
	"vcs-log":               "版本控制日志索引",
	"vcs-users":             "版本控制用户索引",
	"fileHistory":           "文件历史索引",
	"database-log":          "数据库工具日志",
	"testHistory":           "测试运行历史",
	"test-history":          "测试运行历史",
	"restart":               "重启临时数据",
	"stat":                  "使用统计缓存",
	"grape":                 "Grape 依赖缓存",
	"data-source":           "数据源元数据缓存",
	"frameworks":            "框架检测缓存",
	"aia":                   "AI 助手缓存",
	"semantic-search":       "语义搜索索引",
	"ts-go-native-preview":  "TypeScript 原生预览缓存",
	"ts-go-fork-embedded":   "TypeScript 内嵌服务缓存",
	"openapi":               "OpenAPI 解析缓存",
	"httpFileSystem":        "远程文件缓存",
	"extResources":          "外部资源缓存",
	"qoder-cn":              "Qoder 插件缓存",
	"Conversion":            "项目格式转换缓存",
	"captured_telemetry":    "遥测采集缓存",
	"coverage":              "覆盖率报告缓存",
	"profiler":              "性能分析快照缓存",
}

// 其余说明文案
const (
	noteIconCache = "图标缓存，启动时重建"
	noteLog       = "IDE 日志，启动时重建"
	noteOutbox    = "可能含未发送的队列，只展示不删除"
	noteSymlink   = "符号链接，不跟随也不删除"
	noteUnknown   = "未识别的条目，只展示不删除"
	noteUnread    = "无法读取，只展示不删除"
)
