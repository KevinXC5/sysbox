#!/usr/bin/env bash
# 生成面向用户的更新说明，并附上完整代码差异链接。
# 本次更新过 RELEASE_NOTES.md 时使用手写内容，否则按约定式提交类型自动整理。
# 用法：bash scripts/release-notes.sh 当前提交 上次版本标签 仓库名称
set -euo pipefail

current=${1:?缺少当前提交}
previous=${2-}
repository=${3:?缺少仓库名称}
current=$(git rev-parse --verify "${current}^{commit}")
notes_path=RELEASE_NOTES.md
range=$current
base=
url="https://github.com/$repository/commits/$current"

if [[ -n "$previous" ]]; then
  # 标签不存在或历史不连续时停止，避免生成误导性的发布说明。
  base=$(git rev-parse --verify "refs/tags/${previous}^{commit}")
  git merge-base --is-ancestor "$base" "$current"
  range="$base..$current"
  url="https://github.com/$repository/compare/$previous...$current"
fi

# manual_notes 输出本次手写的说明；未更新或为空时返回失败
manual_notes() {
  local notes
  notes=$(git show "$current:$notes_path" 2>/dev/null) || return 1
  [[ -n "${notes//[[:space:]]/}" ]] || return 1
  # 与上一版本相同说明本次没有手写，改为自动整理
  if [[ -n "$base" ]] && git cat-file -e "$base:$notes_path" 2>/dev/null &&
    [[ "$(git show "$base:$notes_path")" == "$notes" ]]; then
    return 1
  fi
  printf '%s\n' "$notes"
}

# commit_notes 按约定式提交类型整理，只保留用户能感知的改动
commit_notes() {
  local feat=() fix=() perf=() subject re='^(feat|fix|perf)(\([^)]*\))?!?: *(.+)$'
  while IFS= read -r subject; do
    [[ "$subject" == *"[skip release]"* ]] && continue
    [[ "$subject" =~ $re ]] || continue
    case ${BASH_REMATCH[1]} in
    feat) feat+=("${BASH_REMATCH[3]}") ;;
    fix) fix+=("${BASH_REMATCH[3]}") ;;
    perf) perf+=("${BASH_REMATCH[3]}") ;;
    esac
  done < <(git log --reverse --format='%s' "$range")

  if ((${#feat[@]} + ${#fix[@]} + ${#perf[@]} == 0)); then
    printf '%s\n' '本次为内部维护更新，功能无变化。'
    return
  fi
  local sep=
  section() {
    local title=$1
    shift
    (($# > 0)) || return 0
    printf '%s## %s\n\n' "$sep" "$title"
    printf -- '- %s\n' "$@"
    sep=$'\n'
  }
  section 新增功能 ${feat[@]+"${feat[@]}"}
  section 使用体验 ${perf[@]+"${perf[@]}"}
  section 问题修复 ${fix[@]+"${fix[@]}"}
}

if ! notes=$(manual_notes); then
  notes=$(commit_notes)
fi

printf '%s\n\n[完整变更记录](%s)\n' "$notes" "$url"
