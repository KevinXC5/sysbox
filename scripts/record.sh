#!/usr/bin/env bash
# 截取 README 与官网的高清静态 PNG，依赖 VHS 与 Maple Mono NF CN 字体。
# 用法：scripts/record.sh [--website] [dark|light]...；默认截取全部页面和两种主题。
# 普通页面使用真实数据，只查看；确认与完成示例单独使用演练模式，绝不删除文件。
set -euo pipefail
cd "$(dirname "$0")/.."
command -v vhs >/dev/null
website_only=false
if [[ ${1:-} == --website ]]; then website_only=true; shift; fi
themes=("$@")
[[ ${#themes[@]} -gt 0 ]] || themes=(dark light)
for theme in "${themes[@]}"; do
  case "$theme" in dark|light) ;; *) echo "主题必须是 dark 或 light：$theme" >&2; exit 2 ;; esac
done
capture_dir=$(mktemp -d /tmp/sysbox-capture.XXXXXX)
trap 'rm -rf "$capture_dir"' EXIT
mkdir -p "$capture_dir/config/sysbox"
printf '%s\n' '{"update":{"disable_check":true},"animation":"off"}' > "$capture_dir/config/sysbox/config.json"
capture_version=$(git describe --tags --exact-match HEAD 2>/dev/null || printf dev)
go build -ldflags "-X github.com/KevinXC5/sysbox/internal/meta.Version=$capture_version" -o bin/sysbox ./cmd/sysbox

# 等待页面就绪后才开启采帧；Screenshot 取下一帧，1 FPS 下至少等待 1 秒再隐藏。
open_page() {
  local tool=$1 ready=$2 mode=${3:-}
  cat <<TAPE
Hide
Type "clear; ./bin/sysbox $mode --theme $theme $tool"
Enter
Wait+Screen@60s /$ready/
Sleep 200ms
TAPE
}
screenshot() {
  cat <<TAPE
Show
Screenshot "$capture_dir/$theme/$1.png"
Sleep 1200ms
Hide
TAPE
}
close_page() { printf 'Ctrl+C\nSleep 200ms\n'; }
shot() {
  open_page "$2" "$3" "${4:-}"
  screenshot "$1"
  close_page
}

for theme in "${themes[@]}"; do
  if [[ $theme == dark ]]; then
    background='#10111B'; foreground='#E2E8F0'; margin='#111116'
  else
    background='#FAFBFD'; foreground='#1E293B'; margin='#F1EDE4'
  fi
  mkdir -p "$capture_dir/$theme" "docs/screenshots/$theme"
  tape="$capture_dir/$theme.tape"
  {
    # 不声明 Output，VHS 仅生成 Screenshot 指定的 PNG，无 GIF 或视频编码。
    cat <<TAPE
Set Shell zsh
Set Framerate 1
Set TypingSpeed 0ms
Set CursorBlink false
Set FontSize 22
Set FontFamily "Maple Mono NF CN"
Set Width 2720
Set Height 2064
Set LineHeight 1.25
Set Padding 33
Set Margin 27
Set MarginFill "$margin"
Set WindowBar Colorful
Set BorderRadius 12
Set Theme { "name": "sysbox-$theme", "background": "$background", "foreground": "$foreground" }
Env COLORTERM "truecolor"
Env TERM "xterm-256color"
Env XDG_CONFIG_HOME "$capture_dir/config"
TAPE
    shot 01-home '' '个工具'
    shot 07-devcache devcache '已选 [0-9]+ 项'
    shot processes processes '个进程.*每'
    open_page docker '项 · 已选'
    # 基础列表与异步详情分别加载，给详情留出时间。
    printf 'Sleep 1s\n'
    screenshot docker
    close_page
    # 等待全部目标检测完成，状态栏显示条目数
    shot network network '[0-9]+ . [0-9]+ 项'
    open_page disk '选择磁盘'
    printf 'Type "g"\nCtrl+U\nType "."\nEnter\nWait+Screen@60s /扫描于/\n'
    screenshot disk
    close_page
    shot services services '共 [0-9]+ 项'
    if ! $website_only; then
      open_page '' '个工具'
      printf 'Type "s"\nWait+Screen@60s /共可释放/\nSleep 200ms\n'
      screenshot 01-home-scan
      close_page
      open_page '' '个工具'
      printf 'Left\nSleep 200ms\n'
      screenshot 01-home-settings
      close_page
      shot 02-jetbrains jetbrains '已选 [0-9]+ 项'
      # 两张操作示例在演练模式下完成，其他截图均来自正常运行。
      open_page devcache '已选 [0-9]+ 项' --dry-run
      printf 'Enter\nWait+Screen@60s /确认清理/\nRight\nSleep 200ms\n'
      screenshot 03-devcache-confirm
      printf 'Enter\nWait+Screen@60s /演练完成/\n'
      screenshot 04-devcache-done
      close_page
      shot 05-agent agent '已选 [0-9]+ 项'
      shot 06-vscode vscode '已选 [0-9]+ 项'
      shot 08-projects projects '已选 [0-9]+ 项'
      shot 10-claude claude '版本'
    fi
  } > "$tape"
  vhs validate "$tape"
  start_seconds=$SECONDS
  env -u NO_COLOR -u CLICOLOR -u FORCE_COLOR vhs "$tape" --quiet
  # 整个主题成功后才替换图片，避免失败时留下半套输出。
  for image in "$capture_dir/$theme/"*.png; do cp "$image" "docs/screenshots/$theme/"; done
  for pair in '01-home home' '07-devcache cache' 'processes processes' 'docker docker' 'network network' 'disk disk' 'services services'; do
    read -r source target <<< "$pair"
    cp "$capture_dir/$theme/$source.png" "website/assets/$target-$theme.png"
  done
  echo "已截取 $theme 高清 PNG（2720 × 2064），耗时 $((SECONDS-start_seconds)) 秒"
done
