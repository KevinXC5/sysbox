



# sysbox

跨平台的系统维护工具箱：把零散的清理和工具更新脚本收进一个终端界面，支持 macOS 与 Windows。

## 功能

| 分组 | 工具 | 作用 |
|---|---|---|
| 清理 | JetBrains 缓存 | 清理索引、编译缓存与 IDE 日志；跳过需要重新下载的 Agent 与补全模型，不碰设置与插件目录 |
| 清理 | Agent 垃圾 | 清理 Claude、Codex、OpenCode 等超过保留期的缓存和日志，以及可以确认的旧版本 |
| 工具 | Claude Code 更新 | 断点续传下载指定版本，SHA-256 校验后调用官方安装 |

所有会修改系统的操作都有确认弹窗；加上 `--dry-run` 可以完整走一遍流程而不做任何修改。

## 安装

支持 macOS 与 Windows，x86_64 与 ARM 架构均可。

macOS：

```bash
curl -fsSL https://raw.githubusercontent.com/KevinXC5/sysbox/main/install.sh | bash
```

默认安装到 `~/.local/bin/sysbox`，可用 `SYSBOX_INSTALL_DIR` 指定目录，用 `SYSBOX_VERSION=v0.1.12` 安装指定版本。

Windows（PowerShell）：

```powershell
irm https://raw.githubusercontent.com/KevinXC5/sysbox/main/install.ps1 | iex
```

默认安装到 `%LOCALAPPDATA%\Programs\sysbox` 并加入用户 PATH，同样支持 `SYSBOX_INSTALL_DIR` 与 `SYSBOX_VERSION` 环境变量。建议使用 Windows Terminal 运行。

## 升级

- 界面启动时每天最多检查一次新版本，有更新时顶栏会提示，首页按 `u` 即可升级并重启
- 也可以在命令行执行 `sysbox update`
- 在配置文件中设置 `"update": {"disable_check": true}` 可关闭自动检查

## 使用

```bash
sysbox                 # 打开界面
sysbox jetbrains       # 直接打开某个工具，工具名见 sysbox list
sysbox --dry-run       # 演练模式
sysbox --theme light   # 指定主题：auto、light、dark
sysbox list            # 列出全部工具
sysbox update          # 升级到最新版本
sysbox version         # 显示版本号
```

通用按键：`↑↓` 移动，`enter` 确认，`esc` 返回，`t` 切换主题，`ctrl+c` 退出。各页面的其余按键显示在底栏。

主题默认跟随终端背景色自动选择深色或浅色，按 `t` 在 自动 → 浅色 → 深色 之间切换，选择会被记住。

## 配置

配置文件位于 `~/.config/sysbox/config.json`（遵循 `XDG_CONFIG_HOME`），Windows 上位于 `%APPDATA%\sysbox\config.json`。所有字段都可省略：

```json
{
  "theme": "auto",
  "agent": { "keep_days": 30 },
  "claude": { "proxy": "http://127.0.0.1:7890", "direct": false },
  "update": { "disable_check": false }
}
```

| 字段 | 说明 |
|---|---|
| `agent.keep_days` | 缓存与日志的保留天数，默认 30 |
| `claude.proxy` | 下载 Claude Code 使用的代理；代理不可用时自动直连。也可用环境变量 `PROXY_URL` 覆盖 |
| `claude.direct` | 始终直连，等同于环境变量 `CLAUDE_NO_PROXY=1` |

## 截图

| | |
|---|---|
| ![首页](docs/screenshots/dark/01-home.png) | ![JetBrains 缓存](docs/screenshots/dark/02-jetbrains.png) |
| ![确认弹窗](docs/screenshots/dark/03-jetbrains-confirm.png) | ![清理完成](docs/screenshots/dark/04-jetbrains-done.png) |
| ![Agent 垃圾](docs/screenshots/dark/05-agent.png) | ![Claude Code 更新](docs/screenshots/dark/10-claude.png) |

浅色主题：

| | |
|---|---|
| ![JetBrains 缓存](docs/screenshots/light/02-jetbrains.png) | ![Agent 垃圾](docs/screenshots/light/05-agent.png) |

开发与贡献请参阅[开发指南](CONTRIBUTING.md)。
