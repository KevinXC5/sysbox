# 开发指南

需要 Go 1.26 及以上。

```bash
make build    # 编译到 bin/sysbox
make dry      # 演练模式运行
make test     # 格式检查、静态检查与测试
make record   # 生成 2720 × 2064 高清 PNG，依赖 vhs 与 Maple Mono NF CN 字体
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
  uninstall/         卸载：删除程序、撤销安装时写入的 PATH、可选删除配置
  tools/             各工具的领域逻辑，不依赖界面，均有单元测试
    jetbrains/  vscode/  agentjunk/  devcache/  projects/  claudeupdate/  containers/  processes/  network/  disk/  services/
  ui/
    theme/           配色（浅色、深色两套）、渐变、Logo、像素图标渲染
    widget/          顶栏、底栏、面板、弹窗、进度条等组件
  tui/
    app.go           根模型：路由、全局按键、顶栏底栏
    registry.go      注册表：首页分类、工具与设置项
    icons.go         首页的彩色像素图标
    screens/         页面：首页、通用清理页及各工具页
scripts/record.sh    录制 README 截图
install.sh           macOS 安装脚本
install.ps1          Windows 安装脚本
```

### 分层约定

- `tools/` 只包含领域逻辑，通过 `sysx.Runner` 执行命令，测试时可替换为假的实现
- 演练模式由各工具根据 `screens.Env.DryRun` 自行处理，只走流程不做修改
- 清理类工具实现 `cleanup.Source` 接口，在注册表中用 `cleanTool` 登记，复用 `screens.NewClean`，并自动加入首页的可释放空间汇总
- 删除前检查只需关心所选条目时，额外实现 `cleanup.ItemChecker`
- 新增工具：实现领域逻辑与页面，在 `internal/tui/registry.go` 登记一项即可；`Group` 决定放在首页哪个分类，`Icon` 是 6×4 的像素图标，画在 `internal/tui/icons.go`
- 新增设置项：在 `Settings()` 中登记，选项型填 `Options`，文字型留空并在 `Set` 中校验输入

### 静态截图

`make record` 截取 README 和官网所需页面的深浅两种主题；`scripts/record.sh dark` 只截取深色，`website/capture.sh` 只截取官网展示的页面。

截图使用 1 FPS、零打字延迟和 2720 × 2064 画布，只输出 PNG，不生成 GIF 或视频。等待页面就绪后才采帧，截图指令后等待 1.2 秒，确保下一帧完整保存。录制使用临时配置，关闭更新检查和界面动画，不改动用户配置。

普通页面使用真实数据，仅打开、查看和退出。确认与完成两张示例在演练模式下生成，不执行删除。官网复用同一套 PNG。宣传视频由独立的视频制作流程生成。

### Docker 实测

普通测试使用模拟命令返回值，不依赖 Docker 引擎或 Kubernetes 集群。真实 Docker 测试需显式开启，并指定已有的本地镜像（需默认命令持续运行且提供 `/bin/sh`，例如 nginx）：

```bash
SYSBOX_DOCKER_TEST=1 SYSBOX_DOCKER_TEST_IMAGE=nginx:1.29 go test -v ./internal/tools/containers -run TestDockerIntegration -count=1
SYSBOX_DOCKER_TEST_PULL=1 go test -v ./internal/tools/containers -run TestDockerPullIntegration -count=1
```

启停、重启、日志、镜像标签和删除测试仅操作随机名称的临时容器和临时标签，结束后清理。拉取测试使用 `hello-world:latest`，测试前该标签已存在时跳过，避免更新现有镜像。

### 发布

提交直接推送到 main，推送和 PR 只执行测试，不会发布。需要发布时在 main 的最新提交上执行：

```bash
make release           # 在上一个标签基础上递增补丁号，例如 v0.1.21 → v0.1.22
make release V=v0.2.0  # 指定版本号
```

`make release` 会确认工作区干净、当前提交与 `origin/main` 一致，然后打标签并推送。推送 `v*` 标签后，发布工作流先跑一遍测试，再编译 macOS 与 Windows 的 arm64、amd64 版本并创建 GitHub Release。发布失败时可以修好后在 Actions 页面手动运行 release 工作流，填入同一个标签重新发布。

### 发布说明

发布工作流把上个标签以来的提交说明交给 DeepSeek（`deepseek-v4-flash`），整理成面向用户的更新说明，按新增功能、使用体验、问题修复、升级注意事项分类，并附上完整代码差异链接。需要在仓库的 Actions secrets 中配置 `DEEPSEEK_API_KEY`；未配置或调用失败时发布会停止，修复后在 Actions 页面手动运行 release 工作流，填入同一个标签重新发布。

更新说明完全来自提交说明，因此提交说明应使用约定式提交，并用面向用户的语言描述变化。
