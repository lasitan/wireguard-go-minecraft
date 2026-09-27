#!/usr/bin/env bash
# Ensure GitHub CLI (gh) is on PATH. Downloads a static binary if missing.
# Usage (from repo root / checkout):
#   bash scripts/ensure-gh.sh
# Under Actions, appends install dir to $GITHUB_PATH for later steps.
set -euo pipefail

if command -v gh >/dev/null 2>&1; then
  gh --version
  exit 0
fi

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) garch=amd64 ;;
  aarch64|arm64) garch=arm64 ;;
  armv7l|armhf) garch=armv6 ;;
  i386|i686) garch=386 ;;
  *)
    echo "ensure-gh: unsupported arch: ${arch}" >&2
    exit 1
    ;;
esac

GH_VERSION="${GH_VERSION:-2.67.0}"
base="gh_${GH_VERSION}_linux_${garch}"
url="https://github.com/cli/cli/releases/download/v${GH_VERSION}/${base}.tar.gz"
dest="${RUNNER_TEMP:-/tmp}/gh-cli-$$"
mkdir -p "${dest}"
echo "ensure-gh: downloading ${url}"
curl -fsSL "$url" | tar -xz -C "${dest}"
install_dir="${HOME}/.local/bin"
mkdir -p "${install_dir}"
install -m 0755 "${dest}/${base}/bin/gh" "${install_dir}/gh"
rm -rf "${dest}"
export PATH="${install_dir}:${PATH}"
if [[ -n "${GITHUB_PATH:-}" ]]; then
  echo "${install_dir}" >> "${GITHUB_PATH}"
elif [[ -n "${GITHUB_ENV:-}" ]]; then
  echo "PATH=${install_dir}:${PATH}" >> "${GITHUB_ENV}"
fi
gh --version
