#!/usr/bin/env bash
# 准备截图用的演示环境：独立的配置目录，不触碰真实配置。
set -euo pipefail

root=/tmp/sysbox-demo
rm -rf "$root"
mkdir -p "$root/config/sysbox"

cat > "$root/config/sysbox/config.json" <<JSON
{
  "update": { "disable_check": true }
}
JSON
echo "演示环境已就绪：$root"
