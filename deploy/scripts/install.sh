#!/usr/bin/env bash
# One-line install / upgrade for lasitan-cluster (Linux):
#   curl -fsSL https://raw.githubusercontent.com/lasitan/Lasitan-Cluster/main/deploy/scripts/install.sh | sudo bash
# Env:
#   LASITAN_VERSION   pin a version (e.g. 2.0.3); default = latest release
#   LASITAN_GH_PROXY  download mirror prefix (e.g. https://ghfast.top/)
#   LASITAN_FORCE=1   reinstall even when already up to date
#   LASITAN_NO_DEB=1  install the standalone binary even when dpkg exists
set -euo pipefail

REPO="lasitan/Lasitan-Cluster"
PROXY="${LASITAN_GH_PROXY:-}"
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
  [[ -n "$tag" ]] || die "无法获取最新版本（可设置 LASITAN_GH_PROXY 或 LASITAN_VERSION）"
  echo "${tag#v}"
}

VERSION="${LASITAN_VERSION:-$(latest_version)}"
VERSION="${VERSION#v}"
BASE="${PROXY}https://github.com/${REPO}/releases/download/v${VERSION}"

CURRENT=""
if command -v lasitan-cluster >/dev/null 2>&1; then
  CURRENT="$(lasitan-cluster --version 2>/dev/null | sed -n '1s/^lasitan-cluster v*//p' | tr -d '[:space:]')" || true
fi
say "最新版本 ${VERSION}，当前 ${CURRENT:-未安装}"
if [[ "$CURRENT" == "$VERSION" && "${LASITAN_FORCE:-0}" != "1" ]]; then
  say "已是最新版本（LASITAN_FORCE=1 可强制重装）"
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
    'lasitan-cluster@*' 'lasitan-cluster-master.service' 2>/dev/null | awk '{print $1}')" || true
  for u in $units; do
    systemctl restart "$u" && say "已重启 $u"
  done
}

# Download first so we can migrate leftover wireguard-go even when PATH entry is gone.
BIN="lasitan-cluster-${FRIENDLY}-${VERSION}"
DEB="lasitan-cluster_${VERSION}-1_${DEBARCH}.deb"
MIGRATE_BIN=""

if command -v dpkg >/dev/null 2>&1 && [[ "${LASITAN_NO_DEB:-0}" != "1" ]]; then
  fetch "${BASE}/${DEB}" "$TMP/$DEB"
  # Extract binary from the deb for pre-install migrate (dpkg may conflict with wireguard-mc).
  dpkg-deb -x "$TMP/$DEB" "$TMP/debroot" 2>/dev/null || true
  if [[ -x "$TMP/debroot/usr/bin/lasitan-cluster" ]]; then
    MIGRATE_BIN="$TMP/debroot/usr/bin/lasitan-cluster"
  fi
else
  fetch "${BASE}/${BIN}" "$TMP/lasitan-cluster"
  chmod 0755 "$TMP/lasitan-cluster"
  "$TMP/lasitan-cluster" --version >/dev/null || die "下载的二进制无法运行"
  MIGRATE_BIN="$TMP/lasitan-cluster"
fi

if [[ -n "$MIGRATE_BIN" ]]; then
  say "检测并迁移旧版 wireguard-go（若存在）…"
  "$MIGRATE_BIN" migrate --no-start || true
fi

if command -v dpkg >/dev/null 2>&1 && [[ "${LASITAN_NO_DEB:-0}" != "1" ]]; then
  # postinst reloads units and restarts running tunnels + master.
  dpkg -i "$TMP/$DEB"
else
  DEST="$(command -v lasitan-cluster || echo /usr/local/bin/lasitan-cluster)"
  DEST="$(readlink -f "$DEST" 2>/dev/null || echo "$DEST")"
  install -D -m 0755 "$TMP/lasitan-cluster" "${DEST}.new"
  mv -f "${DEST}.new" "$DEST"
  say "已安装到 $DEST"
fi

# Start services recorded by migrate (or restart already-active lasitan units).
say "启动 / 恢复服务…"
lasitan-cluster migrate --start || true
restart_units

say "完成：$(lasitan-cluster --version 2>/dev/null | head -n1)"

json_str() {
  # Escape for JSON string values (enroll keys are restricted; URLs are trusted from Master UI).
  local s=$1
  s=${s//\\/\\\\}
  s=${s//\"/\\\"}
  printf '%s' "$s"
}

apply_agent_bootstrap() {
  [[ "${LASITAN_BOOTSTRAP:-}" == "agent" ]] || return 0
  local url="${LASITAN_MASTER_URL:-}"
  local key="${LASITAN_ENROLL_KEY:-}"
  [[ -n "$url" && -n "$key" ]] || die "LASITAN_BOOTSTRAP=agent 需要 LASITAN_MASTER_URL 与 LASITAN_ENROLL_KEY"
  local conf="/etc/lasitan-cluster/lasitan-cluster-agent.json"
  local iface="${LASITAN_IFACE:-lc0}"
  local role="${LASITAN_ROLE:-client}"
  mkdir -p /etc/lasitan-cluster
  if [[ -f "$conf" && "${LASITAN_FORCE:-0}" != "1" ]]; then
    say "保留已有 $conf（LASITAN_FORCE=1 可覆盖）"
  else
    {
      printf '{\n  "masterUrl": "%s",\n  "key": "%s"' "$(json_str "$url")" "$(json_str "$key")"
      if [[ "$role" == "server" ]]; then
        printf ',\n  "role": "server"'
        if [[ -n "${LASITAN_ENDPOINT:-}" ]]; then
          printf ',\n  "endpoint": "%s"' "$(json_str "$LASITAN_ENDPOINT")"
        fi
        if [[ -n "${LASITAN_LISTEN_PORT:-}" ]]; then
          printf ',\n  "listenPort": %s' "$LASITAN_LISTEN_PORT"
        fi
      fi
      printf '\n}\n'
    } >"$conf"
    chmod 600 "$conf"
    say "已写入 $conf"
  fi
  say "注册开机自启并启动 Agent（$iface）…"
  lasitan-cluster install "$iface" || die "lasitan-cluster install 失败"
  say "Agent 已安装并指向 Master $url，启动后会自动入网"
}

apply_agent_bootstrap

if [[ -z "$CURRENT" && "${LASITAN_BOOTSTRAP:-}" != "agent" ]]; then
  cat >&2 <<'EOF'

下一步：
  Master： 编辑 /etc/lasitan-cluster/lasitan-cluster-master.json 后执行  sudo lasitan-cluster install master
  Agent：  编辑 /etc/lasitan-cluster/lasitan-cluster-agent.json（masterUrl + key）后执行  sudo lasitan-cluster install
          作为服务端（其他客户端都连本机）再加 "role": "server"，可选 "endpoint": "公网IP:25590"
  或在 Master Web「一键安装 Agent」复制整行命令（含配置）
以后升级： sudo lasitan-cluster update
EOF
fi
