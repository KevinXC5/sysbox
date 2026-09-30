#!/usr/bin/env bash
# 截取正式运行的首页、开发缓存、进程管理与 Docker 管理；只打开、查看和退出，不执行清理或结束进程。
# Docker 默认选中最新创建的容器，录制前确认它的详情里没有不宜公开的路径与配置。
set -euo pipefail
cd "$(dirname "$0")/.."
command -v vhs >/dev/null
# 标记发布版本的源码使用真实版本号，其余源码保留 dev 标识。
capture_version=$(git describe --tags --exact-match HEAD 2>/dev/null || printf dev)
go build -ldflags "-X github.com/KevinXC5/sysbox/internal/meta.Version=$capture_version" -o bin/sysbox ./cmd/sysbox
capture_dir=$(mktemp -d /tmp/sysbox-website.XXXXXX)
trap 'rm -rf "$capture_dir"' EXIT
mkdir -p "$capture_dir/config/sysbox"
# 隔离展示设置，保持用户的配置不变，并关闭录制期间的更新检查。
printf '%s\n' '{"update":{"disable_check":true}}' > "$capture_dir/config/sysbox/config.json"
for theme in dark light; do
  if [[ "$theme" == dark ]]; then
    background='#10111B'
    foreground='#E2E8F0'
    margin='#111116'
  else
    background='#FAFBFD'
    foreground='#1E293B'
    margin='#F1EDE4'
  fi
  tape="$capture_dir/$theme.tape"
  cat > "$tape" <<TAPE
Output "$capture_dir/$theme.gif"
Set Shell zsh
Set FontSize 15
Set FontFamily "Maple Mono NF CN"
Set Width 1814
Set Height 1376
Set LineHeight 1.25
Set Padding 22
Set Margin 18
Set MarginFill "$margin"
Set WindowBar Colorful
Set BorderRadius 8
Set Theme { "name": "sysbox-$theme", "background": "$background", "foreground": "$foreground" }
Env COLORTERM "truecolor"
Env TERM "xterm-256color"
Env XDG_CONFIG_HOME "$capture_dir/config"
Hide
Type "clear; ./bin/sysbox --theme $theme"
Enter
Sleep 3s
Show
Sleep 1s
Screenshot "website/assets/home-$theme.png"
Sleep 500ms
Hide
Ctrl+C
Sleep 500ms
Type "clear; ./bin/sysbox --theme $theme devcache"
Enter
Sleep 8s
Show
Sleep 1s
Screenshot "website/assets/cache-$theme.png"
Sleep 500ms
Hide
Ctrl+C
Sleep 500ms
Type "clear; ./bin/sysbox --theme $theme processes"
Enter
Sleep 5s
Show
Sleep 1s
Screenshot "website/assets/processes-$theme.png"
Sleep 500ms
Hide
Ctrl+C
Sleep 500ms
Type "clear; ./bin/sysbox --theme $theme docker"
Enter
Sleep 6s
Show
Sleep 1s
Screenshot "website/assets/docker-$theme.png"
Sleep 500ms
Hide
Ctrl+C
TAPE
  env -u NO_COLOR -u CLICOLOR -u FORCE_COLOR vhs "$tape" >/dev/null
  echo "已截取 $theme 正式运行界面"
done
