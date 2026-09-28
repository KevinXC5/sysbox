#!/usr/bin/env bash
# 生成面向用户的更新说明，并附上完整代码差异链接。
# 优先级：本次手写的 RELEASE_NOTES.md > GitHub Models 自动总结 > 按提交类型整理。
# 用法：bash scripts/release-notes.sh 当前提交 上次版本标签 仓库名称
# 环境变量：MODELS_TOKEN 调用 GitHub Models 的令牌，为空时跳过 AI 总结；
#          RELEASE_NOTES_MODEL 模型名称，默认 openai/gpt-4.1。
set -euo pipefail

current=${1:?缺少当前提交}
previous=${2-}
repository=${3:?缺少仓库名称}
current=$(git rev-parse --verify "${current}^{commit}")
notes_path=RELEASE_NOTES.md
model=${RELEASE_NOTES_MODEL:-openai/gpt-4.1}
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
  # 与上一版本相同说明本次没有手写，交给自动总结
  if [[ -n "$base" ]] && git cat-file -e "$base:$notes_path" 2>/dev/null &&
    [[ "$(git show "$base:$notes_path")" == "$notes" ]]; then
    return 1
  fi
  printf '%s\n' "$notes"
}

# ai_notes 调用 GitHub Models 总结提交说明与代码改动
ai_notes() {
  [[ -n "${MODELS_TOKEN-}" ]] || return 1
  local commits stat diff prompt body reply
  # 免费额度单次输入约 8000 token，各部分都要截断
  commits=$(git log --reverse --format='- %s%n%b' "$range" | grep -v '^\s*$' | head -c 4000)
  stat=$(git diff --stat "${base:-$(git hash-object -t tree /dev/null)}" "$current" | tail -40)
  diff=$(git diff "${base:-$(git hash-object -t tree /dev/null)}" "$current" -- . \
    ':(exclude)*_test.go' ':(exclude)*.md' ':(exclude)docs/**' ':(exclude)go.sum' | head -c 12000)
  prompt=$(
    cat <<EOF
你是 macOS 维护工具箱 sysbox 的发布说明撰写者。根据下面的提交说明和代码改动，写一份面向普通用户的更新说明。

要求：
- 使用简体中文，输出纯 Markdown，不要用代码块包裹，不要写标题“更新说明”或版本号。
- 第一行用一句话概括本次更新给用户带来的价值。
- 然后按实际内容分组，可选的二级标题只有：## 新增功能、## 使用体验、## 问题修复、## 升级注意事项；没有内容的分组不要写。
- 每条说明用户在什么场景下会遇到什么变化，相关提交合并成一条。
- 不出现提交哈希、feat/fix 等前缀、[skip release] 等标记、函数名和文件路径。
- 只涉及文档、测试、构建流程、代码重构的改动不写；如果全部都是这类改动，只输出一句“本次为内部维护更新，功能无变化。”
- Markdown 加粗时，标点放在 ** 外面。

## 提交说明
$commits

## 改动文件
$stat

## 代码改动（可能已截断）
$diff
EOF
  )
  body=$(jq -n --arg model "$model" --arg prompt "$prompt" \
    '{model: $model, temperature: 0.2, messages: [{role: "user", content: $prompt}]}')
  reply=$(curl -fsS --max-time 90 https://models.github.ai/inference/chat/completions \
    -H "Authorization: Bearer $MODELS_TOKEN" \
    -H "Accept: application/vnd.github+json" \
    -H "Content-Type: application/json" \
    -d "$body" | jq -r '.choices[0].message.content // empty') || return 1
  # 去掉模型偶尔包裹的代码块标记
  reply=$(printf '%s\n' "$reply" | sed '/^```/d')
  [[ -n "${reply//[[:space:]]/}" ]] || return 1
  printf '%s\n' "$reply"
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
  if ! notes=$(ai_notes); then
    printf '%s\n' '提示：AI 总结不可用，改为按提交类型整理更新说明。' >&2
    notes=$(commit_notes)
  fi
fi

printf '%s\n\n[完整变更记录](%s)\n' "$notes" "$url"
