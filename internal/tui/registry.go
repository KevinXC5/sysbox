package tui

import (
	"runtime"

	"github.com/KevinXC5/sysbox/internal/cleanup"
	"github.com/KevinXC5/sysbox/internal/tools/agentjunk"
	"github.com/KevinXC5/sysbox/internal/tools/devcache"
	"github.com/KevinXC5/sysbox/internal/tools/jetbrains"
	"github.com/KevinXC5/sysbox/internal/tools/projects"
	"github.com/KevinXC5/sysbox/internal/tools/vscode"
	"github.com/KevinXC5/sysbox/internal/tui/screens"
)

// Tools 工具注册表：新增工具只需在这里登记一项
func Tools() []screens.Tool {
	return []screens.Tool{
		cleanTool(screens.Tool{
			ID: "jetbrains", Group: "清理", Name: "JetBrains 缓存", Desc: "索引、编译缓存与 IDE 日志",
			Detail: jetbrainsDetail(runtime.GOOS),
		}, func(screens.Env) (cleanup.Source, error) { return jetbrains.NewSource() }),
		cleanTool(screens.Tool{
			ID: "vscode", Group: "清理", Name: "VS Code 系编辑器", Desc: "VS Code、Cursor 等的缓存与旧扩展",
			Detail: []string{
				"覆盖 VS Code、Insiders、Cursor、Windsurf、Trae",
				"清理可重建缓存、日志和崩溃报告，识别旧扩展，默认保留当前引用和最新版本",
				"设置、用户数据和未保存文件备份默认不勾选，旧服务端可手动勾选",
				"只要求退出所选条目涉及的编辑器，删除前重新核对清理条件",
			},
		}, func(screens.Env) (cleanup.Source, error) { return vscode.NewSource() }),
		cleanTool(screens.Tool{
			ID: "agent", Group: "清理", Name: "Agent 垃圾", Desc: "Claude、Codex 等的过期缓存与旧版本",
			Detail: []string{
				"默认清理 Claude、Codex、OpenCode 等 agent 超过保留期的缓存",
				"近期缓存、日志和疑似备份默认不勾选，可手动纳入",
				"识别可以确认的旧版本，保留当前版本和最高版本",
				"删除前逐项复核 inode 与版本链接，目标有变化自动跳过",
			},
		}, func(env screens.Env) (cleanup.Source, error) { return agentjunk.NewSource(env.Config.Agent.KeepDays) }),
		cleanTool(screens.Tool{
			ID: "devcache", Group: "清理", Name: "开发缓存", Desc: "Go、npm、Gradle 等包管理器缓存",
			Detail: []string{
				"覆盖 Go、npm、pnpm、Yarn、Bun、pip、uv、Gradle、Maven、Cargo、Homebrew",
				"默认勾选只影响下次安装速度的下载与构建缓存",
				"Go 模块、pnpm store、Maven 仓库等构建依赖默认不勾选，删除后需要联网",
				"Gradle 只保留最新版本的缓存与发行包，Go、uv 调用官方清理命令",
			},
		}, func(screens.Env) (cleanup.Source, error) { return devcache.NewSource() }),
		cleanTool(screens.Tool{
			ID: "projects", Group: "清理", Name: "项目构建产物", Desc: "node_modules、target 等过期产物",
			Detail: []string{
				"在项目目录下查找 node_modules、target、build、.venv 等构建产物",
				"产物旁必须有 package.json、Cargo.toml 等项目文件才会列出",
				"默认勾选长期未活动项目的产物，近期项目可手动勾选",
				"扫描目录与保留天数可在配置文件的 projects 中修改",
			},
		}, func(env screens.Env) (cleanup.Source, error) {
			return projects.NewSource(env.Config.Projects.Roots, env.Config.Projects.KeepDays)
		}),
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

// cleanTool 清理类工具：页面和首页汇总共用同一个数据源构造函数
func cleanTool(t screens.Tool, src func(screens.Env) (cleanup.Source, error)) screens.Tool {
	t.Source = src
	t.New = func(env screens.Env) screens.Page {
		s, err := src(env)
		if err != nil {
			return screens.NewErrorPage([]string{"清理", t.Name}, err)
		}
		return screens.NewClean(s, env)
	}
	return t
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
