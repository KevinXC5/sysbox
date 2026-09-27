#!/usr/bin/env bash
# sysbox 安装脚本：
#   curl -fsSL https://raw.githubusercontent.com/KevinXC5/sysbox/main/install.sh | bash
#
# 环境变量：
#   SYSBOX_VERSION      安装指定版本，如 v0.1.12；默认最新版本
#   SYSBOX_INSTALL_DIR  安装目录，默认 ~/.local/bin
# 已安装后可直接用 sysbox update 升级，界面里也会提示新版本。
set -euo pipefail

REPO="KevinXC5/sysbox"
INSTALL_DIR="${SYSBOX_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${SYSBOX_VERSION:-latest}"

info() { printf '\033[35m◆\033[0m %s\n' "$*"; }
ok() { printf '\033[32m✓\033[0m %s\n' "$*"; }
die() { printf '\033[31m✗\033[0m %s\n' "$*" >&2; exit 1; }

[[ "$(uname -s)" == Darwin ]] || die "sysbox 只支持 macOS"

# 在 Apple 芯片上经 Rosetta 转译运行的终端也安装 arm64 版本
case "$(uname -m)" in
  arm64) arch=arm64 ;;
  x86_64)
    if [[ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" == 1 ]]; then arch=arm64; else arch=amd64; fi ;;
  *) die "不支持的架构：$(uname -m)" ;;
esac
asset="sysbox-darwin-${arch}"

if [[ "${VERSION}" == latest ]]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/${VERSION}"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

info "下载 ${asset}（${VERSION}）"
curl -fsSL --retry 3 -o "$tmp/${asset}" "${base}/${asset}" || die "下载失败：${base}/${asset}"
curl -fsSL --retry 3 -o "$tmp/checksums.txt" "${base}/checksums.txt" || die "下载校验文件失败"

want="$(awk -v f="${asset}" '$2 == f { print $1 }' "$tmp/checksums.txt")"
got="$(shasum -a 256 "$tmp/${asset}" | awk '{ print $1 }')"
[[ -n "${want}" && "${want}" == "${got}" ]] || die "SHA-256 校验失败，未安装"
ok "校验通过"

mkdir -p "${INSTALL_DIR}"
chmod 755 "$tmp/${asset}"
mv -f "$tmp/${asset}" "${INSTALL_DIR}/sysbox"
ok "已安装 $("${INSTALL_DIR}/sysbox" version) 到 ${INSTALL_DIR}/sysbox"

case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *) printf '\n把下面这行加入 ~/.zshrc 后重新打开终端：\n  export PATH="%s:$PATH"\n' "${INSTALL_DIR}" ;;
esac
