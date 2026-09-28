本次更新让 sysbox 从个人 macOS 工具箱变成通用工具，Windows 用户也可以安装使用。

## 新增功能

- 支持 Windows，x86_64 与 ARM 架构均可。在 PowerShell 中执行 `irm https://raw.githubusercontent.com/KevinXC5/sysbox/main/install.ps1 | iex` 即可安装。
- JetBrains 缓存清理会按系统定位缓存和日志目录，设置与插件目录在两个系统上都不会被改动。
- Claude Code 更新能识别 Windows，下载对应的官方版本。
- Agent 垃圾在 Windows 上无法确认当前版本时，旧版本只列为“待核查”，不会自动删除。
- 首页的系统信息和主题跟随系统深浅色在 Windows 上同样可用。Windows 上的配置文件位于 `%APPDATA%\sysbox\config.json`。

## 升级注意事项

- 移除了 appstoreagent、CursorUIViewService、深信服客户端三个只适用于 macOS 特定问题的工具。之前用这些工具禁用过的服务不会自动恢复，如需恢复，请在升级前用旧版本执行恢复操作。
- 移除了 Obsidian 目录链接工具。已经创建的链接保持原样，不需要时可以直接在库里删除，源目录不受影响。
