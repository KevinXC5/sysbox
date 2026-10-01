# sysbox 官网

以蓝色为主色，采用深色首屏、分层终端展示、不对称功能布局和深浅交替的内容区。使用正式工具截图和轻量入场动效，强调 sysbox 的终端操作体验。使用原生 HTML、CSS 与 JavaScript，不需要安装依赖或构建。

## 本地预览

在仓库根目录运行：

```bash
python3 -m http.server 4173 --directory website
```

访问 http://localhost:4173 。手机尺寸下导航折叠为菜单。

## GitHub Pages 部署

仓库的 Settings → Pages → Build and deployment → Source 选择 GitHub Actions。

将官网与 `.github/workflows/pages.yml` 推送到 main 后，工作流会部署 `website/`。也可以在 Actions 中手动运行「部署官网」。默认网址为 https://kevinxc5.github.io/sysbox/ 。

所有本地资源使用相对路径，支持仓库子路径与自定义域名。自定义域名在 Settings → Pages 中设置，并按 GitHub 提示配置 DNS。

## 内容维护

- `index.html`：功能介绍、安装区、FAQ 与链接。
- `styles.css`：配色、布局、手机适配与减少动态效果设置。
- `app.js`：首屏截图轮换、界面与主题切换、平台切换、复制命令和手机导航。
- `assets/`：高清终端截图与网站图标。
- `capture.sh`：使用 VHS 重新截取正式界面；依赖 vhs 与 Maple Mono NF CN 字体。截取首页、开发缓存、进程管理、Docker 管理、网络诊断、磁盘分析和服务与启动项的深浅两种主题，只打开和查看，不执行清理或结束进程。

功能说明以仓库 README 为准。截图来自当前版本的正常运行，重新截取可在仓库根目录运行 `website/capture.sh`。录制与 `scripts/record.sh` 共用流程，使用临时展示配置，关闭更新检查和动画，保留真实的功能与缓存扫描。使用 1 FPS 和零打字延迟，输出 2720 × 2064 的静态 PNG，不生成 GIF 或视频。进程与 Docker 截图展示本机的真实列表，Docker 详情默认选中最新创建的容器；发布前确认截图没有展示敏感数据。
