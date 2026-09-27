#!/usr/bin/env bash
# 录制 README 用的截图，依赖 vhs（brew install vhs）与 Maple Mono NF CN 字体。
# 用法：scripts/record.sh [dark|light]...   默认两种主题都录
# 全程使用 --dry-run 演练模式和独立的演示配置，不会修改系统，也不会读写真实配置。
set -euo pipefail
cd "$(dirname "$0")/.."

go build -o bin/sysbox ./cmd/sysbox
./scripts/demo-env.sh >/dev/null

dark_theme='{ "name": "sysbox-night", "background": "#0E1325", "foreground": "#E2E8F0", "cursor": "#A78BFA", "selection": "#2A3350", "black": "#1A2138", "red": "#FB7185", "green": "#34D399", "yellow": "#FBBF24", "blue": "#60A5FA", "magenta": "#A78BFA", "cyan": "#22D3EE", "white": "#E2E8F0", "brightBlack": "#5B6785", "brightRed": "#FDA4AF", "brightGreen": "#6EE7B7", "brightYellow": "#FDE68A", "brightBlue": "#93C5FD", "brightMagenta": "#C4B5FD", "brightCyan": "#67E8F9", "brightWhite": "#F8FAFC" }'
light_theme='{ "name": "sysbox-day", "background": "#FAFBFD", "foreground": "#1E293B", "cursor": "#7C3AED", "selection": "#E2E8F0", "black": "#1E293B", "red": "#E11D48", "green": "#059669", "yellow": "#C2410C", "blue": "#2563EB", "magenta": "#7C3AED", "cyan": "#0891B2", "white": "#E2E8F0", "brightBlack": "#64748B", "brightRed": "#F43F5E", "brightGreen": "#10B981", "brightYellow": "#EA580C", "brightBlue": "#3B82F6", "brightMagenta": "#8B5CF6", "brightCyan": "#06B6D4", "brightWhite": "#FFFFFF" }'

# shot <文件名> <工具> <等待> [按键...]：打开工具，等待加载，按顺序按键后截图
shot() {
  local name=$1 tool=$2 wait=$3
  shift 3
  cat <<EOF
Hide
Type "clear; ./bin/sysbox --dry-run --theme $THEME $tool"
Enter
Sleep $wait
Show
EOF
  for key in "$@"; do
    case "$key" in
      Up|Down|Left|Right|Enter|Space|Tab) echo "$key"; echo "Sleep 300ms" ;;
      *) echo "Type \"$key\""; echo "Sleep 300ms" ;;
    esac
  done
  cat <<EOF
Sleep 700ms
Screenshot "docs/screenshots/$THEME/$name.png"
Sleep 700ms
Hide
Ctrl+C
Sleep 400ms
EOF
}

record() {
  THEME=$1
  local term_theme margin
  if [[ $THEME == dark ]]; then term_theme=$dark_theme margin="#3B2F7A"; else term_theme=$light_theme margin="#DDD6FE"; fi
  mkdir -p "docs/screenshots/$THEME"
  local tape
  tape=$(mktemp -t sysbox-tape).tape
  {
    cat <<EOF
Output "/tmp/sysbox-demo/$THEME.gif"
Set Shell zsh
Set FontSize 15
Set FontFamily "Maple Mono NF CN"
Set Width 1560
Set Height 1000
Set LineHeight 1.25
Set Padding 22
Set Margin 32
Set MarginFill "$margin"
Set WindowBar Colorful
Set BorderRadius 12
Set Theme $term_theme
Env COLORTERM "truecolor"
Env XDG_CONFIG_HOME "/tmp/sysbox-demo/config"
EOF
    shot 01-home "" 1.5s
    shot 02-jetbrains jetbrains 2.5s Down Down
    shot 03-jetbrains-confirm jetbrains 2.5s Enter Right
    shot 04-jetbrains-done jetbrains 2.5s Enter Right Enter Sleep
    shot 05-agent agent 3s
    shot 06-appstore appstore 2.5s
    shot 07-appstore-confirm appstore 2.5s d Right
    shot 08-sangfor sangfor 3s
    shot 09-cursorui cursorui 3s
    shot 10-claude claude 2.5s
    shot 11-obsidian obsidian 1.5s Down Down
    shot 12-obsidian-confirm obsidian 1.5s Enter Right
  } | sed 's/^Type "Sleep"$/Sleep 5s/' > "$tape"
  vhs "$tape" >/dev/null
  rm -f "$tape"
  echo "已录制：docs/screenshots/$THEME/"
}

themes=("$@")
[[ ${#themes[@]} -gt 0 ]] || themes=(dark light)
for t in "${themes[@]}"; do record "$t"; done
