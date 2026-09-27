# 开发指南

需要 Go 1.26 及以上。

```bash
make build    # 编译到 bin/sysbox
make dry      # 演练模式运行
make test     # 格式检查、静态检查与测试
make record   # 重新录制截图，依赖 vhs 与 Maple Mono NF CN 字体
```

### 目录结构

```
cmd/sysbox/          入口与子命令
internal/
  meta/              版本号（构建时注入）与仓库信息
  config/            用户配置读写
  fsx/               磁盘占用统计、大小格式化
  sysx/              系统交互：命令执行、进程查询、launchd、演练执行器
  cleanup/           “扫描 → 勾选 → 删除”类工具的通用模型
  selfupdate/        从 GitHub Release 检查与安装新版本
  tools/             各工具的领域逻辑，不依赖界面，均有单元测试
    jetbrains/  agentjunk/  appstore/  cursorui/  sangfor/  claudeupdate/  obsidianlink/
  ui/
    theme/           配色（浅色、深色两套）、渐变、Logo
    widget/          顶栏、底栏、面板、弹窗、进度条、步骤日志等组件
  tui/
    app.go           根模型：路由、全局按键、顶栏底栏
    registry.go      工具注册表
    screens/         页面：首页、通用清理页、通用操作页及各工具页
scripts/             演示环境与截图录制
install.sh           安装脚本
```

### 分层约定

- `tools/` 只包含领域逻辑，通过 `sysx.Runner` 执行命令，测试时替换为 `sysxtest.Runner`
- 演练模式使用 `sysx.DryRunner`：只读命令照常执行，修改类命令只记录，工具代码无需区分
- 清理类工具实现 `cleanup.Source` 接口，复用 `screens.NewClean`
- 查看状态并执行操作的工具实现 `screens.OpsTool`，复用 `screens.NewOps`
- 新增工具：实现领域逻辑与页面，在 `internal/tui/registry.go` 登记一项即可
