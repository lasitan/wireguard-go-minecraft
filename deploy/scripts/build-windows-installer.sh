#!/usr/bin/env bash
# Build the Windows installer (x64 + ARM64 in one setup.exe) with NSIS.
# Usage (from repo root, after cross-compiling both Windows binaries):
#   bash deploy/scripts/build-windows-installer.sh VERSION EXE_AMD64 EXE_ARM64 OUTFILE
# Needs makensis (Debian/Ubuntu: apt-get install nsis).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
[[ $# -eq 4 ]] || { sed -n '3,5p' "$0" >&2; exit 1; }
VERSION="${1#v}"
EXE_AMD64="$(realpath "$2")"
EXE_ARM64="$(realpath "$3")"
OUTFILE="$(realpath -m "$4")"

if ! command -v makensis >/dev/null 2>&1; then
  echo "==> Installing nsis"
  if [[ $EUID -eq 0 ]]; then SUDO=""; else SUDO="sudo"; fi
  $SUDO apt-get update -qq
  $SUDO apt-get install -y -qq --no-install-recommends nsis >/dev/null
fi

# VIProductVersion needs four numeric parts: 2.2.1-rc1 -> 2.2.1.0
IFS=. read -r v1 v2 v3 _ <<<"${VERSION%%[-+]*}"
VI_VERSION="${v1:-0}.${v2:-0}.${v3:-0}.0"

mkdir -p "$(dirname "$OUTFILE")"
makensis -V2 -INPUTCHARSET UTF8 \
  -DVERSION="$VERSION" \
  -DVI_VERSION="$VI_VERSION" \
  -DEXE_AMD64="$EXE_AMD64" \
  -DEXE_ARM64="$EXE_ARM64" \
  -DOUTFILE="$OUTFILE" \
  "$ROOT/deploy/windows/installer.nsi"
echo "==> Installer ready: $OUTFILE"
