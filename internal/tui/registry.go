package tui

import (
	"errors"
	"net/url"
	"runtime"
	"strconv"
	"strings"

	"github.com/KevinXC5/sysbox/internal/cleanup"
	"github.com/KevinXC5/sysbox/internal/config"
	"github.com/KevinXC5/sysbox/internal/tools/agentjunk"
	"github.com/KevinXC5/sysbox/internal/tools/claudeupdate"
	"github.com/KevinXC5/sysbox/internal/tools/devcache"
	"github.com/KevinXC5/sysbox/internal/tools/jetbrains"
	"github.com/KevinXC5/sysbox/internal/tools/projects"
	"github.com/KevinXC5/sysbox/internal/tools/vscode"
	"github.com/KevinXC5/sysbox/internal/tui/screens"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
)

// Groups 首页固定显示的分类，按顺序排列；还没有条目的分类也会显示方块
func Groups() []screens.Group {
	return []screens.Group{
		{Name: "清理", Icon: iconClean},
		{Name: "工具", Icon: iconTools},
		{Name: "系统", Icon: iconSystem},
		{Name: "设置", Icon: iconSettings},
	}
}

// Tools 工具注册表：新增工具只需在这里登记一项
func Tools() []screens.Tool {
	return []screens.Tool{
		cleanTool(screens.Tool{
			ID: "jetbrains", Group: "清理", Name: "JetBrains 缓存", Icon: iconJetBrains, Desc: "索引、编译缓存与 IDE 日志",
			Detail: jetbrainsDetail(runtime.GOOS),
		}, func(screens.Env) (cleanup.Source, error) { return jetbrains.NewSource() }),
		cleanTool(screens.Tool{
			ID: "vscode", Group: "清理", Name: "VS Code 系编辑器", Icon: iconVSCode, Desc: "VS Code、Cursor 等的缓存与旧扩展",
			Detail: []string{
				"覆盖 VS Code、Insiders、Cursor、Windsurf、Trae",
				"清理可重建缓存、日志和崩溃报告，识别旧扩展，默认保留当前引用和最新版本",
				"设置、用户数据和未保存文件备份默认不勾选，旧服务端可手动勾选",
				"只要求退出所选条目涉及的编辑器，删除前重新核对清理条件",
			},
		}, func(screens.Env) (cleanup.Source, error) { return vscode.NewSource() }),
		cleanTool(screens.Tool{
			ID: "agent", Group: "清理", Name: "Agent 垃圾", Icon: iconAgent, Desc: "Claude、Codex 等的过期缓存与旧版本",
			Detail: []string{
				"默认清理 Claude、Codex、OpenCode 等 agent 超过保留期的缓存",
				"近期缓存、日志和疑似备份默认不勾选，可手动纳入",
				"识别可以确认的旧版本，保留当前版本和最高版本",
				"删除前逐项复核 inode 与版本链接，目标有变化自动跳过",
			},
		}, func(env screens.Env) (cleanup.Source, error) { return agentjunk.NewSource(env.Config.Agent.KeepDays) }),
		cleanTool(screens.Tool{
			ID: "devcache", Group: "清理", Name: "开发缓存", Icon: iconPackage, Desc: "Go、npm、Gradle 等包管理器缓存",
			Detail: []string{
				"覆盖 Go、npm、pnpm、Yarn、Bun、pip、uv、Gradle、Maven、Cargo、Homebrew",
				"默认勾选只影响下次安装速度的下载与构建缓存",
				"Go 模块、pnpm store、Maven 仓库等构建依赖默认不勾选，删除后需要联网",
				"Gradle 只保留最新版本的缓存与发行包，Go、uv 调用官方清理命令",
			},
		}, func(screens.Env) (cleanup.Source, error) { return devcache.NewSource() }),
		cleanTool(screens.Tool{
			ID: "projects", Group: "清理", Name: "项目构建产物", Icon: iconCube, Desc: "node_modules、target 等过期产物",
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
			ID: "docker", Group: "工具", Name: "Docker 管理", Icon: iconDocker, Desc: "快速管理容器与镜像",
			Detail: []string{"切换 Docker 环境，筛选容器与镜像", "容器启停、重启、日志、终端，多选批量操作", "拉取镜像、添加标签、创建容器与删除镜像", "左右分屏自动显示基础信息，快捷键直接操作"},
			New:    screens.NewDocker,
		},
		{
			ID: "k8s", Group: "工具", Name: "Kubernetes 管理", Icon: iconKubernetes, Desc: "管理 Deployment、ConfigMap、Pod 与 Node",
			Detail: []string{"切换集群和命名空间，不改写全局 kubeconfig", "Deployment 扩缩容、滚动重启与发布状态", "查看日志、进入 Pod 终端、查看和编辑 YAML", "左右分屏自动显示基础信息，快捷键直接操作"},
			New:    screens.NewKubernetes,
		},
		{
			ID: "claude", Group: "工具", Name: "Claude Code 更新", Icon: iconDownload, Desc: "断点续传，校验后安装",
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

// Settings 首页“设置”分类里的配置项，修改后立即写回配置文件
func Settings() []screens.Setting {
	days := []string{"7", "14", "30", "60", "90"}
	dayLabel := func(v string) string { return v + " 天" }
	setDays := func(dst *int) func(string) error {
		return func(v string) error {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return errors.New("天数必须是正整数")
			}
			*dst = n
			return nil
		}
	}
	orDefault := func(n, def int) string {
		if n <= 0 {
			n = def
		}
		return strconv.Itoa(n)
	}
	onOff := func(v string) string {
		if v == "on" {
			return "开启"
		}
		return "关闭"
	}
	list := []screens.Setting{
		{
			ID: "theme", Name: "主题", Desc: "界面配色", Icon: iconTheme,
			Detail: []string{
				"自动：跟随终端背景色与系统外观",
				"浅色、深色：固定使用一套配色",
				"在任意页面按 t 也能切换",
			},
			Options: []string{"auto", "light", "dark"},
			Get:     func(screens.Env) string { return theme.CurrentMode().String() },
			Label: func(v string) string {
				m, _ := theme.ParseMode(v)
				return m.Label()
			},
			Set: func(c *config.Config, v string) error { c.Theme = v; return nil },
		},
		{
			ID: "icons", Name: "列表图标", Desc: "条目左侧的彩色像素图标", Icon: iconIcons,
			Detail: []string{
				"图标用字符色块绘制，不依赖字体，需要终端支持真彩色",
				"关闭后列表改为一项一行，一屏能看到更多条目",
				"分类方块里的图标始终显示",
			},
			Options: []string{"show", "none"},
			Get: func(env screens.Env) string {
				if env.Config.Icons == "none" {
					return "none"
				}
				return "show"
			},
			Label: func(v string) string {
				if v == "none" {
					return "关闭"
				}
				return "显示"
			},
			Set: func(c *config.Config, v string) error { c.Icons = v; return nil },
		},
		{
			ID: "animation", Name: "界面动画", Desc: "进入和退出工具时的过渡", Icon: iconMotion,
			Detail: []string{
				"打开工具时首页的分类方块收成顶部分类条，工具页自上而下展开",
				"返回首页时反向播放，全程约四分之一秒",
				"远程连接或终端刷新较慢时可以关闭",
			},
			Options: []string{"on", "off"},
			Get: func(env screens.Env) string {
				if env.Config.Animation == "off" {
					return "off"
				}
				return "on"
			},
			Label: onOff,
			Set:   func(c *config.Config, v string) error { c.Animation = v; return nil },
		},
		{
			ID: "update-check", Name: "启动时检查更新", Desc: "有新版本时在顶栏提示", Icon: iconBell,
			Detail: []string{
				"每次启动界面时在后台检查一次新版本",
				"关闭后仍可运行 sysbox update 手动升级",
				"下次启动时生效",
			},
			Options: []string{"on", "off"},
			Get: func(env screens.Env) string {
				if env.Config.Update.DisableCheck {
					return "off"
				}
				return "on"
			},
			Label: onOff,
			Set:   func(c *config.Config, v string) error { c.Update.DisableCheck = v == "off"; return nil },
		},
		{
			ID: "agent-keep", Name: "Agent 缓存保留天数", Desc: "超过天数的缓存默认勾选", Icon: iconCalendar,
			Detail: []string{
				"Agent 垃圾清理只默认勾选超过这个天数未修改的缓存",
				"近期缓存仍会列出，可以手动勾选",
			},
			Options: days,
			Get:     func(env screens.Env) string { return orDefault(env.Config.Agent.KeepDays, agentjunk.DefaultKeepDays) },
			Label:   dayLabel,
			Set:     func(c *config.Config, v string) error { return setDays(&c.Agent.KeepDays)(v) },
		},
		{
			ID: "projects-keep", Name: "项目过期天数", Desc: "多久未活动的项目算过期", Icon: iconHourglass,
			Detail: []string{
				"项目构建产物清理只默认勾选超过这个天数未活动的项目",
				"近期项目的产物仍会列出，可以手动勾选",
			},
			Options: days,
			Get:     func(env screens.Env) string { return orDefault(env.Config.Projects.KeepDays, projects.DefaultKeepDays) },
			Label:   dayLabel,
			Set:     func(c *config.Config, v string) error { return setDays(&c.Projects.KeepDays)(v) },
		},
		{
			ID: "projects-roots", Name: "项目扫描目录", Desc: "查找构建产物的根目录", Icon: iconFolder,
			Detail: []string{
				"多个目录用逗号分隔，支持 ~ 开头",
				"留空时自动查找常见的项目目录",
			},
			Get: func(env screens.Env) string { return strings.Join(env.Config.Projects.Roots, ", ") },
			Label: func(v string) string {
				if v == "" {
					return "自动查找"
				}
				return v
			},
			Set: func(c *config.Config, v string) error {
				var roots []string
				for _, r := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '，' }) {
					if r = strings.TrimSpace(r); r != "" {
						roots = append(roots, r)
					}
				}
				c.Projects.Roots = roots
				return nil
			},
		},
		{
			ID: "claude-proxy", Name: "Claude 下载代理", Desc: "下载 Claude Code 时使用", Icon: iconGlobe,
			Detail: []string{
				"留空时使用默认代理 " + claudeupdate.DefaultProxy,
				"代理不可用时会自动改为直连",
				"环境变量 PROXY_URL 优先于这里的设置",
			},
			Get: func(env screens.Env) string { return env.Config.Claude.Proxy },
			Label: func(v string) string {
				if v == "" {
					return "默认"
				}
				return v
			},
			Set: func(c *config.Config, v string) error {
				if u, err := url.Parse(v); v != "" && (err != nil || u.Scheme == "" || u.Host == "") {
					return errors.New("请输入完整的代理地址，例如 " + claudeupdate.DefaultProxy)
				}
				c.Claude.Proxy = v
				return nil
			},
		},
		{
			ID: "claude-direct", Name: "Claude 总是直连", Desc: "下载时跳过代理", Icon: iconBolt,
			Detail: []string{
				"开启后不使用代理，直接连接下载服务器",
				"关闭时先尝试代理，不可用再直连",
				"环境变量 CLAUDE_NO_PROXY=1 同样会强制直连",
			},
			Options: []string{"off", "on"},
			Get: func(env screens.Env) string {
				if env.Config.Claude.Direct {
					return "on"
				}
				return "off"
			},
			Label: onOff,
			Set:   func(c *config.Config, v string) error { c.Claude.Direct = v == "on"; return nil },
		},
	}
	for i := range list {
		list[i].Group = "设置"
	}
	return list
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
