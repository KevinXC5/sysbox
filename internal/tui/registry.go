package tui

import (
	"github.com/KevinXC5/sysbox/internal/tools/agentjunk"
	"github.com/KevinXC5/sysbox/internal/tools/jetbrains"
	"github.com/KevinXC5/sysbox/internal/tui/screens"
)

// Tools 工具注册表：新增工具只需在这里登记一项
func Tools() []screens.Tool {
	return []screens.Tool{
		{
			ID: "jetbrains", Group: "清理", Name: "JetBrains 缓存", Desc: "索引、编译缓存与 IDE 日志",
			Origin: "jetbrains-cache-clean.sh",
			Detail: []string{
				"清理可本地重建的缓存，包括索引、编译缓存、内嵌浏览器缓存等",
				"清理 Logs/JetBrains 下的全部日志",
				"跳过需要重新下载的 Agent 与补全模型",
				"不改动 Application Support（设置、插件本体）",
			},
			New: func(env screens.Env) screens.Page {
				src, err := jetbrains.NewSource()
				if err != nil {
					return screens.NewErrorPage([]string{"清理", "JetBrains 缓存"}, err)
				}
				return screens.NewClean(src, env)
			},
		},
		{
			ID: "agent", Group: "清理", Name: "Agent 垃圾", Desc: "Claude、Codex 等的过期缓存与旧版本",
			Origin: "clean_agent_junk.py",
			Detail: []string{
				"清理 Claude、Codex、OpenCode 等 agent 超过保留期的缓存和日志",
				"识别可以确认的旧版本，保留当前版本和最高版本",
				"单文件安装的 agent 只列出疑似备份，交给人工判断",
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
			ID: "appstore", Group: "进程治理", Name: "appstoreagent", Desc: "ArcadeManager 死循环占满 CPU",
			Origin: "appstoreagent-cpu.sh",
			Detail: []string{
				"实时查看 appstoreagent 的 CPU 占用，自动识别疑似死循环",
				"禁用自动拉起并结束当前进程，重启后仍生效",
				"Apple 修复后一键恢复默认行为",
			},
			New: screens.NewAppStore,
		},
		{
			ID: "cursorui", Group: "进程治理", Name: "CursorUIViewService", Desc: "排查进程关联的 App 与文件",
			Origin: "cursorui-check.sh",
			Detail: []string{
				"列出 CursorUIViewService 进程及资源占用",
				"分析打开的文件，定位关联的 App、输入法、废纸篓资源与网络连接",
				"先正常退出、超时再强制结束，并检查系统是否重新拉起",
			},
			New: screens.NewCursorUI,
		},
		{
			ID: "sangfor", Group: "进程治理", Name: "深信服客户端", Desc: "退出 aTrust 与 VDI，压住 ecosystemd",
			Origin: "sangfor-stop.sh / sangfor-start.sh",
			Detail: []string{
				"查看全部 launchd 服务的加载与自启状态，以及 ecosystemd 占用",
				"彻底停止并禁用客户端，重启 ecosystemd 清除高 CPU 状态",
				"按依赖顺序恢复启动与开机自启",
				"按 plist 登记的程序路径精确识别进程，不做模糊匹配",
			},
			New: screens.NewSangfor,
		},
		{
			ID: "claude", Group: "工具", Name: "Claude Code 更新", Desc: "断点续传，校验后安装",
			Origin: "claude-update.sh",
			Detail: []string{
				"安装 latest、stable 或指定版本的 Claude Code",
				"断点续传下载，网络中断和低速卡住都会自动重试",
				"SHA-256 校验通过后调用官方安装",
				"代理不可用时自动改为直连",
			},
			New: screens.NewClaude,
		},
		{
			ID: "obsidian", Group: "工具", Name: "Obsidian 目录链接", Desc: "按子目录选择性链接进 vault",
			Origin: "obsidian-link.py",
			Detail: []string{
				"把工作目录按一级子目录选择性链接进 Obsidian 库",
				"避免归档、素材、node_modules 被 Obsidian 索引",
				"实时预览新增与移除的链接，源目录保持不动",
			},
			New: screens.NewObsidian,
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
