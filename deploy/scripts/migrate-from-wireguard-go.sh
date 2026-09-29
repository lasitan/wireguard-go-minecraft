#!/usr/bin/env bash
# Detect leftover wireguard-go / wireguard-mc and losslessly migrate to lasitan-cluster.
#
# Usage:
#   sudo bash deploy/scripts/migrate-from-wireguard-go.sh
#   curl -fsSL …/migrate-from-wireguard-go.sh | sudo bash
#
# Behavior:
#   1. Prefer `lasitan-cluster migrate` if the new binary is already installed
#   2. Otherwise download the latest release binary to a temp dir and run migrate
#   3. Works even when /usr/bin/wireguard-go PATH entry was deleted mid-upgrade
#      but the binary file and/or /etc/wireguard configs remain
set -euo pipefail

REPO="lasitan/wireguard-go-minecraft"
PROXY="${LASITAN_GH_PROXY:-}"
if [[ -n "$PROXY" && "$PROXY" != */ ]]; then PROXY="$PROXY/"; fi

say() { printf '\033[1;32m==>\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31mError:\033[0m %s\n' "$*" >&2; exit 1; }

[[ "$(uname -s)" == "Linux" ]] || die "仅支持 Linux"
[[ "$(id -u)" -eq 0 ]] || die "需要 root：请用 sudo 运行"

if command -v lasitan-cluster >/dev/null 2>&1; then
  say "使用已安装的 lasitan-cluster 执行迁移"
  exec lasitan-cluster migrate "$@"
fi

command -v curl >/dev/null || die "需要 curl（或先安装 lasitan-cluster）"

case "$(uname -m)" in
  x86_64 | amd64) FRIENDLY=linux-amd64 ;;
  aarch64 | arm64) FRIENDLY=linux-arm64 ;;
  armv7l | armv6l | armhf) FRIENDLY=linux-armv7 ;;
  i386 | i686) FRIENDLY=linux-386 ;;
  *) die "不支持的架构 $(uname -m)" ;;
esac

latest_version() {
  local tag
  tag="$(curl -fsSL -H 'Accept: application/vnd.github+json' \
    "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null |
    sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)" || true
  if [[ -z "$tag" ]]; then
    tag="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
      "${PROXY}https://github.com/${REPO}/releases/latest" 2>/dev/null | sed -n 's#.*/tag/##p')" || true
  fi
  [[ -n "$tag" ]] || die "无法获取最新版本（可设置 LASITAN_GH_PROXY 或 LASITAN_VERSION）"
  echo "${tag#v}"
}

VERSION="${LASITAN_VERSION:-$(latest_version)}"
VERSION="${VERSION#v}"
BASE="${PROXY}https://github.com/${REPO}/releases/download/v${VERSION}"
BIN="lasitan-cluster-${FRIENDLY}-${VERSION}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
say "下载 $BIN 用于迁移"
curl -fL --retry 3 --progress-bar -o "$TMP/lasitan-cluster" "${BASE}/${BIN}" || die "下载失败"
chmod 0755 "$TMP/lasitan-cluster"
"$TMP/lasitan-cluster" --version >/dev/null || die "下载的二进制无法运行"

say "执行无损迁移…"
"$TMP/lasitan-cluster" migrate --no-start

# If a system binary exists now (user installed in parallel), finish start;
# otherwise tell the user to install then migrate --start.
if command -v lasitan-cluster >/dev/null 2>&1; then
  lasitan-cluster migrate --start
else
  say "配置已迁入 /etc/lasitan-cluster；请安装 lasitan-cluster 后执行："
  say "  sudo lasitan-cluster migrate --start"
  say "或一键安装："
  say "  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/deploy/scripts/install.sh | sudo bash"
fi
