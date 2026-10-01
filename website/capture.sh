#!/usr/bin/env bash
# README 与官网共用静态截图流程，仅截取官网所需页面，不生成 GIF 或视频。
set -euo pipefail
cd "$(dirname "$0")/.."
exec scripts/record.sh --website "$@"
