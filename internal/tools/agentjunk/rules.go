// Package agentjunk 清理常用 AI agent 的过期缓存、日志和可以确认的旧版本。
// 只处理白名单内可重新生成的内容；插件、会话、凭据和用户配置都不在清理范围。
package agentjunk

import (
	"regexp"
	"runtime"

	"github.com/KevinXC5/sysbox/internal/cleanup"
)

// 分类下标，与 categories 顺序一致
const (
	CatCache  = iota // 超过保留期的缓存
	CatLog           // 超过保留期的日志，默认不勾选
	CatOld           // 可确认的旧版本
	CatRecent        // 保留期内有修改，默认不勾选，可手动删除
	CatReview        // 疑似可删但无法确认，默认不勾选，可手动删除
	CatError         // 检查失败，不可选
)

var categories = []cleanup.Category{
	CatCache:  {Label: "缓存", Tone: cleanup.ToneGood, Primary: true},
	CatLog:    {Label: "日志", Tone: cleanup.ToneGood, Primary: true},
	CatOld:    {Label: "旧版本", Tone: cleanup.ToneGood, Primary: true},
	CatRecent: {Label: "近期使用", Sub: "保留期内，可手动勾选", Tone: cleanup.ToneInfo},
	CatReview: {Label: "待核查", Sub: "未确认，可手动勾选", Tone: cleanup.ToneWarn},
	CatError:  {Label: "检查失败", Sub: "无法读取", Tone: cleanup.ToneDanger},
}

// dirRule 一个可清理目录及其所属 agent
type dirRule struct {
	Rel   string // 相对家目录的路径
	Agent string
}

// cacheDirs 可重新生成的缓存目录。fx 只有会话目录，没有可独立清理的缓存
var cacheDirs = []dirRule{
	{".claude/cache", "Claude"},
	{".codex/cache", "Codex"},
	{".cache/claude", "Claude"},
	{".cache/opencode", "OpenCode"},
	{".cache/mastra", "Mastra"},
	{".omp/cache", "omp"},
	{".omp/agent/cache", "omp"},
	{".pi/web-search-cache", "pi"},
	{".pi/agent/web-search-cache", "pi"},
}

// logDirs 日志目录，同样遵循保留期；不包括会话和模型下载
var logDirs = []dirRule{
	{".local/share/opencode/log", "OpenCode"},
	{".omp/logs", "omp"},
	{".grok/logs", "Grok"},
}

// versionRule 以“版本目录 + 当前版本链接”方式安装的 agent
type versionRule struct {
	Agent      string
	Dir        string         // 版本目录，相对家目录
	Link       string         // 指向当前版本的链接；Windows 上常是复制品而不是链接
	Executable string         // 版本为目录时，目录内的可执行文件；版本为单文件时为空
	Pattern    *regexp.Regexp // 版本名格式
	// copyEntry 为真时入口可能是复制品：单文件版本的入口独立于版本目录，只保留最高版本；
	// 版本为目录时无法确认入口是否独立，降级为待核查
	copyEntry bool
}

var (
	semverName = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:[-+][A-Za-z0-9._-]+)?$`)
	// Grok 的版本文件在 bin 下是 grok-1.0.44，在 downloads 下带平台段；
	// 平台段同时识别 macos 与 windows，避免 Windows 上整条规则失效后静默跳过
	grokName = regexp.MustCompile(`^grok-(\d+)\.(\d+)\.(\d+)(?:-(?:macos|windows|linux)-(?:aarch64|x86_64|arm64|x64))?(?:\.exe)?$`)
)

// goos 运行平台，测试可替换，避免只能在对应系统上验证布局差异
var goos = runtime.GOOS

// exe 非 Windows 原样返回，Windows 补上 .exe
func exe(name string) string {
	if goos == "windows" && name != "" {
		return name + ".exe"
	}
	return name
}

func versionRules() []versionRule {
	// Windows 上 Claude 的 claude.exe 是从版本目录复制出来的独立文件，不是符号链接；
	// Codex 的 current 无法靠链接确认当前版本。
	copyEntry := goos == "windows"
	return []versionRule{
		// Claude 链接直接指向版本文件
		{"Claude", ".local/share/claude/versions", ".local/bin/" + exe("claude"), "", semverName, copyEntry},
		// Codex 链接指向包含 bin/codex 的 release 目录
		{"Codex", ".codex/packages/standalone/releases", ".codex/packages/standalone/current", "bin/" + exe("codex"), semverName, copyEntry},
		// Codex 桌面端的 app-server 守护进程另有一套 release 目录，布局与 standalone 相同；
		// local-<hash> 开头的本地构建不符合版本格式，会被跳过，守护进程可能用它回滚
		{"Codex", ".codex/packages/app-server-daemon/releases", ".codex/packages/app-server-daemon/current", "bin/" + exe("codex"), semverName, copyEntry},
	}
}

// Grok 的版本文件可能在 bin 也可能在 downloads，另有一个 agent 命令链接，单独处理
const (
	grokBin       = ".grok/bin"
	grokDownloads = ".grok/downloads"
)

// grokSystemBin 安装器可能额外放置命令链接的系统目录，测试可替换
var grokSystemBin = "/usr/local/bin"

// agentProcs 用于提示“相关 agent 正在运行”的进程名
var agentProcs = []string{"claude", "codex", "opencode", "grok", "omp"}
