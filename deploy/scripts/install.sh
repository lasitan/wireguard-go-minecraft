#!/usr/bin/env bash
# One-line install / upgrade for wireguard-mc (Linux):
#   curl -fsSL https://raw.githubusercontent.com/lasitan/wireguard-go-minecraft/main/deploy/scripts/install.sh | sudo bash
# Env:
#   WG_MC_VERSION   pin a version (e.g. 2.0.3); default = latest release
#   WG_MC_GH_PROXY  download mirror prefix (e.g. https://ghfast.top/)
#   WG_MC_FORCE=1   reinstall even when already up to date
#   WG_MC_NO_DEB=1  install the standalone binary even when dpkg exists
set -euo pipefail

REPO="lasitan/wireguard-go-minecraft"
PROXY="${WG_MC_GH_PROXY:-}"
if [[ -n "$PROXY" && "$PROXY" != */ ]]; then PROXY="$PROXY/"; fi

say() { printf '\033[1;32m==>\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31mError:\033[0m %s\n' "$*" >&2; exit 1; }

[[ "$(uname -s)" == "Linux" ]] || die "仅支持 Linux；Windows 请使用 install.ps1"
[[ "$(id -u)" -eq 0 ]] || die "需要 root：请用 sudo 运行"
command -v curl >/dev/null || die "需要 curl"

case "$(uname -m)" in
  x86_64 | amd64) FRIENDLY=linux-amd64 DEBARCH=amd64 ;;
  i386 | i686) FRIENDLY=linux-386 DEBARCH=i386 ;;
  aarch64 | arm64) FRIENDLY=linux-arm64 DEBARCH=arm64 ;;
  armv7l | armv6l | armhf) FRIENDLY=linux-armv7 DEBARCH=armhf ;;
  *) die "不支持的架构 $(uname -m)" ;;
esac

latest_version() {
  local tag
  tag="$(curl -fsSL -H 'Accept: application/vnd.github+json' \
    "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null |
    sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)" || true
  if [[ -z "$tag" ]]; then
    # API blocked or rate-limited: follow the /releases/latest redirect instead.
    tag="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
      "${PROXY}https://github.com/${REPO}/releases/latest" 2>/dev/null | sed -n 's#.*/tag/##p')" || true
  fi
  [[ -n "$tag" ]] || die "无法获取最新版本（可设置 WG_MC_GH_PROXY 或 WG_MC_VERSION）"
  echo "${tag#v}"
}

VERSION="${WG_MC_VERSION:-$(latest_version)}"
VERSION="${VERSION#v}"
BASE="${PROXY}https://github.com/${REPO}/releases/download/v${VERSION}"

CURRENT=""
if command -v wireguard-go >/dev/null 2>&1; then
  CURRENT="$(wireguard-go --version 2>/dev/null | sed -n '1s/^wireguard-go v*//p' | tr -d '[:space:]')" || true
fi
say "最新版本 ${VERSION}，当前 ${CURRENT:-未安装}"
if [[ "$CURRENT" == "$VERSION" && "${WG_MC_FORCE:-0}" != "1" ]]; then
  say "已是最新版本（WG_MC_FORCE=1 可强制重装）"
  exit 0
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fetch() {
  say "下载 $1"
  curl -fL --retry 3 --progress-bar -o "$2" "$1" || die "下载失败：$1"
}

restart_units() {
  command -v systemctl >/dev/null || return 0
  systemctl daemon-reload || true
  local units
  units="$(systemctl list-units --type=service --state=active --no-legend --plain \
    'wireguard-go@*' 'wireguard-go-master.service' 2>/dev/null | awk '{print $1}')" || true
  for u in $units; do
    systemctl restart "$u" && say "已重启 $u"
  done
}

if command -v dpkg >/dev/null 2>&1 && [[ "${WG_MC_NO_DEB:-0}" != "1" ]]; then
  DEB="wireguard-mc_${VERSION}-1_${DEBARCH}.deb"
  fetch "${BASE}/${DEB}" "$TMP/$DEB"
  # postinst reloads units and restarts running tunnels + master.
  dpkg -i "$TMP/$DEB"
else
  BIN="wireguard-mc-${FRIENDLY}-${VERSION}"
  fetch "${BASE}/${BIN}" "$TMP/wireguard-go"
  chmod 0755 "$TMP/wireguard-go"
  "$TMP/wireguard-go" --version >/dev/null || die "下载的二进制无法运行"
  DEST="$(command -v wireguard-go || echo /usr/local/bin/wireguard-go)"
  DEST="$(readlink -f "$DEST" 2>/dev/null || echo "$DEST")"
  install -D -m 0755 "$TMP/wireguard-go" "${DEST}.new"
  mv -f "${DEST}.new" "$DEST"
  say "已安装到 $DEST"
  restart_units
fi

say "完成：$(wireguard-go --version 2>/dev/null | head -n1)"
if [[ -z "$CURRENT" ]]; then
  cat >&2 <<'EOF'

下一步：
  Master： 编辑 /etc/wireguard/wireguard-go-master.json 后执行  sudo wireguard-go install master
  Agent：  编辑 /etc/wireguard/wireguard-go-agent.json（masterUrl + key）后执行  sudo wireguard-go install
          作为服务端（其他客户端都连本机）再加 "role": "server"，可选 "endpoint": "公网IP:25590"
以后升级： sudo wireguard-go update
EOF
fi
