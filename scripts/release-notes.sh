#!/usr/bin/env bash
# 读取当前提交中的用户更新说明，并附上完整代码差异链接。
# 用法：bash scripts/release-notes.sh 当前提交 上次版本标签 仓库名称
set -euo pipefail

current=${1:?缺少当前提交}
previous=${2-}
repository=${3:?缺少仓库名称}
current=$(git rev-parse --verify "${current}^{commit}")
notes_path=RELEASE_NOTES.md
url="https://github.com/$repository/commits/$current"

# 从待发布提交读取，确保发布内容不会混入工作区的未提交修改。
if ! notes=$(git show "$current:$notes_path" 2>/dev/null) || [[ -z "${notes//[[:space:]]/}" ]]; then
  printf '%s\n' '发布失败：请在 RELEASE_NOTES.md 中撰写本次面向用户的更新说明。' >&2
  exit 1
fi
if [[ -n "$previous" ]]; then
  base=$(git rev-parse --verify "refs/tags/${previous}^{commit}")
  git merge-base --is-ancestor "$base" "$current"
  # 禁止重复使用上一版本的说明，避免新增功能发布时携带过时内容。
  if git cat-file -e "$base:$notes_path" 2>/dev/null &&
    [[ "$(git show "$base:$notes_path")" == "$notes" ]]; then
    printf '%s\n' '发布失败：RELEASE_NOTES.md 与上一版本相同，请更新本次说明。' >&2
    exit 1
  fi
  url="https://github.com/$repository/compare/$previous...$current"
fi

printf '%s\n\n[完整变更记录](%s)\n' "$notes" "$url"
