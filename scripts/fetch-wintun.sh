#!/usr/bin/env bash
# Download official Wintun 0.14.1 and place amd64/wintun.dll for go:embed.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="${ROOT}/third_party/wintun/amd64"
VERSION="0.14.1"
URL="https://www.wintun.net/builds/wintun-${VERSION}.zip"
EXPECT_SHA256="07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51"

mkdir -p "${OUT_DIR}"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

ZIP="${TMP}/wintun.zip"
echo "Downloading ${URL}"
curl -fsSL "${URL}" -o "${ZIP}"

got="$(sha256sum "${ZIP}" | awk '{print $1}')"
if [[ "${got}" != "${EXPECT_SHA256}" ]]; then
  echo "SHA256 mismatch: got ${got}, want ${EXPECT_SHA256}" >&2
  exit 1
fi

unzip -q "${ZIP}" -d "${TMP}/extract"
dll="$(find "${TMP}/extract" -type f -path '*/amd64/wintun.dll' | head -n1)"
[[ -n "${dll}" ]] || { echo "amd64/wintun.dll not found in zip" >&2; exit 1; }
install -m 0644 "${dll}" "${OUT_DIR}/wintun.dll"

lic="$(find "${TMP}/extract" -type f \( -iname 'LICENSE*' -o -iname 'COPYING*' \) | head -n1 || true)"
if [[ -n "${lic}" ]]; then
  install -m 0644 "${lic}" "${ROOT}/third_party/wintun/LICENSE.txt"
fi

echo "Installed ${OUT_DIR}/wintun.dll ($(wc -c < "${OUT_DIR}/wintun.dll") bytes)"
