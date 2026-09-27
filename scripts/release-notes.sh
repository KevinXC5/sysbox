#!/usr/bin/env bash
# 根据上次正式发布到当前提交的完整记录生成更新日志；首次发布包含全部记录。+# 用法：bash scripts/release-notes.sh 当前提交 上次版本标签 仓库名称
set -euo pipefail

current=${1:?缺少当前提交}
previous=${2-}
repository=${3:?缺少仓库名称}
current=$(git rev-parse --verify "${current}^{commit}")
range=$current
url="https://github.com/$repository/commits/$current"
if [[ -n "$previous" ]]; then
  # 标签不存在或历史不连续时停止，避免生成误导性的发布说明。
  base=$(git rev-parse --verify "refs/tags/${previous}^{commit}")
  git merge-base --is-ancestor "$base" "$current"
  range="$base..$current"
  url="https://github.com/$repository/compare/$previous...$current"
fi

printf '## 更新内容\n\n'
if [[ -z "$(git rev-list -1 "$range")" ]]; then
  printf '%s\n' '- 本次重新构建发布，没有新增提交。'
else
  # 不把提交说明当作命令执行；保留完整提交链接，便于追溯变更。
  git log --reverse --format="- %s ([%h](https://github.com/$repository/commit/%H))" "$range"
fi
printf '\n[完整变更记录](%s)\n' "$url"
