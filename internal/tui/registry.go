package tui

import (
	"runtime"

	"github.com/KevinXC5/sysbox/internal/tools/agentjunk"
	"github.com/KevinXC5/sysbox/internal/tools/jetbrains"
	"github.com/KevinXC5/sysbox/internal/tools/vscode"
	"github.com/KevinXC5/sysbox/internal/tui/screens"
)

// Tools 工具注册表：新增工具只需在这里登记一项
func Tools() []screens.Tool {
	return []screens.Tool{
		{
			ID: "jetbrains", Group: "清理", Name: "JetBrains 缓存", Desc: "索引、编译缓存与 IDE 日志",
			Detail: jetbrainsDetail(runtime.GOOS),
			New: func(env screens.Env) screens.Page {
				src, err := jetbrains.NewSource()
				if err != nil {
					return screens.NewErrorPage([]string{"清理", "JetBrains 缓存"}, err)
				}
				return screens.NewClean(src, env)
			},
		},
		{
			ID: "vscode", Group: "清理", Name: "VS Code 清理", Desc: "旧扩展、旧服务端、缓存与日志",
			Detail: []string{
				"清理 VS Code 与 Insiders 的可重建缓存、日志和崩溃报告",
				"识别旧扩展，默认保留当前引用和最新版本；旧服务端可手动勾选",
				"设置、用户数据和未保存文件备份默认不勾选，可手动纳入",
				"清理前检查运行中的 VS Code，删除前重新核对清理条件",
			},
			New: func(env screens.Env) screens.Page {
				src, err := vscode.NewSource()
				if err != nil {
					return screens.NewErrorPage([]string{"清理", "VS Code 清理"}, err)
				}
				return screens.NewClean(src, env)
			},
		},
		{
			ID: "agent", Group: "清理", Name: "Agent 垃圾", Desc: "Claude、Codex 等的过期缓存与旧版本",
			Detail: []string{
				"默认清理 Claude、Codex、OpenCode 等 agent 超过保留期的缓存",
				"近期缓存、日志和疑似备份默认不勾选，可手动纳入",
				"识别可以确认的旧版本，保留当前版本和最高版本",
				"删除前逐项复核 inode 与版本链接，目标有变化自动跳过",
			},
			New: func(env screens.Env) screens.Page {
				src, err := agentjunk.NewSource(env.Config.Agent.KeepDays)
				if err != nil {
					return screens.NewErrorPage([]string{"清理", "Agent 垃圾"}, err)
				}
				return screens.NewClean(src, env)
			},
		},
		{
			ID: "claude", Group: "工具", Name: "Claude Code 更新", Desc: "断点续传，校验后安装",
			Detail: []string{
				"安装 latest、stable 或指定版本的 Claude Code",
				"断点续传下载，网络中断和低速卡住都会自动重试",
				"SHA-256 校验通过后调用官方安装",
				"代理不可用时自动改为直连",
			},
			New: screens.NewClaude,
		},
	}
}

// Find 按 ID 查找工具
func Find(id string) (screens.Tool, bool) {
	for _, t := range Tools() {
		if t.ID == id {
			return t, true
		}
	}
	return screens.Tool{}, false
}

// jetbrainsDetail JetBrains 的日志与配置目录位置在 macOS 与 Windows 上不同
func jetbrainsDetail(goos string) []string {
	logs, keep := "清理 Logs/JetBrains 下的全部日志", "不改动 Application Support（设置、插件本体）"
	switch goos {
	case "windows":
		logs, keep = "清理各产品缓存目录下 log 子目录中的日志", `不改动 %APPDATA%\JetBrains（设置、插件本体）`
	}
	return []string{
		"清理可本地重建的缓存，包括索引、编译缓存、内嵌浏览器缓存等",
		logs,
		"默认跳过需要重新下载的 Agent 与补全模型，也可手动勾选",
		keep,
	}
}
