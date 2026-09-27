#!/usr/bin/env bash
# 准备截图用的演示环境：独立的配置目录与一套虚构的 Obsidian 源目录，不触碰真实配置。
set -euo pipefail

root=/tmp/sysbox-demo
rm -rf "$root"
mkdir -p "$root/config/sysbox" "$root/vault/工作"
for d in 项目文档 会议纪要 技术方案 读书笔记 周刊 培训资料 归档 素材 node_modules; do
  mkdir -p "$root/work/$d"
done
# 模拟已有的部分链接
ln -s "$root/work/项目文档" "$root/vault/工作/项目文档"
ln -s "$root/work/会议纪要" "$root/vault/工作/会议纪要"
ln -s "$root/work/归档" "$root/vault/工作/归档"

cat > "$root/config/sysbox/config.json" <<JSON
{
  "obsidian": {
    "src": "$root/work",
    "dest": "$root/vault/工作",
    "excludes": ["归档", "素材", "node_modules"]
  },
  "update": { "disable_check": true }
}
JSON
echo "演示环境已就绪：$root"
