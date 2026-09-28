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
  sysx/              系统交互：命令执行、进程查询
  cleanup/           “扫描 → 勾选 → 删除”类工具的通用模型
  selfupdate/        从 GitHub Release 检查与安装新版本
  tools/             各工具的领域逻辑，不依赖界面，均有单元测试
    jetbrains/  agentjunk/  claudeupdate/
  ui/
    theme/           配色（浅色、深色两套）、渐变、Logo
    widget/          顶栏、底栏、面板、弹窗、进度条等组件
  tui/
    app.go           根模型：路由、全局按键、顶栏底栏
    registry.go      工具注册表
    screens/         页面：首页、通用清理页及各工具页
scripts/record.sh    录制 README 截图
install.sh           macOS 安装脚本
install.ps1          Windows 安装脚本
```

### 分层约定

- `tools/` 只包含领域逻辑，通过 `sysx.Runner` 执行命令，测试时可替换为假的实现
- 演练模式由各工具根据 `screens.Env.DryRun` 自行处理，只走流程不做修改
- 清理类工具实现 `cleanup.Source` 接口，复用 `screens.NewClean`
- 新增工具：实现领域逻辑与页面，在 `internal/tui/registry.go` 登记一项即可

### 发布

提交直接推送到 main，推送和 PR 只执行测试，不会发布。需要发布时在 main 的最新提交上执行：

```bash
make release           # 在上一个标签基础上递增补丁号，例如 v0.1.21 → v0.1.22
make release V=v0.2.0  # 指定版本号
```

`make release` 会确认工作区干净、当前提交与 `origin/main` 一致，然后打标签并推送。推送 `v*` 标签后，发布工作流先跑一遍测试，再编译 macOS 与 Windows 的 arm64、amd64 版本并创建 GitHub Release。发布失败时可以修好后在 Actions 页面手动运行 release 工作流，填入同一个标签重新发布。

### 发布说明

发布工作流自动生成面向用户的更新说明，并附上完整代码差异链接，来源按以下顺序选择：

1. 本次更新过的 `RELEASE_NOTES.md`：内容与上一版本不同时直接使用，适合需要详细介绍的版本。
2. DeepSeek 整理：把上个标签以来的提交说明交给 DeepSeek，按新增功能、使用体验、问题修复、升级注意事项整理成面向用户的说明。需要在仓库的 Actions secrets 中配置 `DEEPSEEK_API_KEY`；未配置或调用失败时使用下一种。
3. 按提交类型整理：将上次发布以来的 `feat`、`perf`、`fix` 提交分别归入新增功能、使用体验、问题修复，去掉提交前缀；其他类型的提交不列出。

因此提交说明应使用约定式提交，并用面向用户的语言描述变化。

手写说明时仅保留本次发布的内容。开头概括本次更新的价值，再按实际内容组织新增功能、使用体验、问题修复或升级注意事项；无需填写没有内容的分类。条目应说明用户在什么场景下会遇到什么变化，相关提交合并为一个完整描述，不使用提交前缀、提交哈希或工作流控制标记作为正文。
