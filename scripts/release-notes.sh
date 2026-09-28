#!/usr/bin/env bash
# 生成面向用户的更新说明，并附上完整代码差异链接。
# 来源依次为：本次更新过的 RELEASE_NOTES.md、DeepSeek 根据提交说明整理（需要 DEEPSEEK_API_KEY）、
# 按约定式提交类型自动整理。前一种不可用时使用后一种。
# 用法：bash scripts/release-notes.sh 当前提交 上次版本标签 仓库名称
set -euo pipefail

ref=${1:?缺少当前提交}
previous=${2-}
repository=${3:?缺少仓库名称}
current=$(git rev-parse --verify "${ref}^{commit}")
notes_path=RELEASE_NOTES.md
range=$current
base=
url="https://github.com/$repository/commits/$ref"

if [[ -n "$previous" ]]; then
  # 标签不存在或历史不连续时停止，避免生成误导性的发布说明。
  base=$(git rev-parse --verify "refs/tags/${previous}^{commit}")
  git merge-base --is-ancestor "$base" "$current"
  range="$base..$current"
  url="https://github.com/$repository/compare/$previous...$ref"
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

# ai_notes 用 DeepSeek 把提交说明整理成面向用户的说明；没有密钥、调用失败或返回为空时返回失败
ai_notes() {
  [[ -n "${DEEPSEEK_API_KEY:-}" ]] || return 1
  local commits prompt response notes
  commits=$(git log --reverse --no-merges --format='- %s%n%b' "$range")
  [[ -n "${commits//[[:space:]]/}" ]] || return 1
  prompt="下面是 sysbox（macOS 与 Windows 上的终端系统维护工具箱）自上一版本以来的 git 提交说明。
请据此写一份面向用户的简体中文更新说明，使用 Markdown：
- 开头用一句话概括本次更新的价值；
- 再按实际内容分为“新增功能”“使用体验”“问题修复”“升级注意事项”等二级标题，没有内容的分类不写；
- 条目说明用户在什么场景下会遇到什么变化，相关提交合并为一条，用平实的语言；
- 只写用户能感知的变化，测试、重构、CI、文档等内部改动不写；如果没有用户能感知的变化，只输出“本次为内部维护更新，功能无变化。”；
- 不写提交前缀、提交哈希和工作流标记，不要任何开场白或结束语，只输出更新说明本身。

提交说明：
$commits"
  response=$(curl -fsS --max-time 300 https://api.deepseek.com/chat/completions \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $DEEPSEEK_API_KEY" \
    -d "$(jq -n --arg prompt "$prompt" '{model: "deepseek-v4-pro", messages: [{role: "user", content: $prompt}]}')") || return 1
  notes=$(jq -r '.choices[0].message.content // empty' <<<"$response") || return 1
  [[ -n "${notes//[[:space:]]/}" ]] || return 1
  printf '%s\n' "$notes"
}

if ! notes=$(manual_notes); then
  if ! notes=$(ai_notes); then
    [[ -z "${DEEPSEEK_API_KEY:-}" ]] || echo "DeepSeek 整理失败，改为按提交类型整理" >&2
    notes=$(commit_notes)
  fi
fi

printf '%s\n\n[完整变更记录](%s)\n' "$notes" "$url"
