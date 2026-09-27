#!/usr/bin/env bash
# Build Master Web UI (src/web) into src/master/ui/dist for go:embed.
# Prefer local npm when available; otherwise run via Node container.
# Usage (from repo root):
#   bash deploy/scripts/build-master-ui.sh
#   NODE_IMAGE=node:22-bookworm bash deploy/scripts/build-master-ui.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

NODE_IMAGE="${NODE_IMAGE:-node:22-bookworm}"
UI_OUT="src/master/ui/dist"

run_npm_build() {
  if [[ -f package-lock.json ]]; then
    npm ci
  else
    npm install
  fi
  npm run build
}

if command -v npm >/dev/null 2>&1; then
  echo "==> Building Master UI with local npm"
  (cd src/web && run_npm_build)
elif command -v docker >/dev/null 2>&1; then
  echo "==> Building Master UI with ${NODE_IMAGE}"
  docker run --rm \
    -e HOST_UID="$(id -u)" \
    -e HOST_GID="$(id -g)" \
    -e npm_config_update_notifier=false \
    -v "${ROOT}:/repo" \
    -w /repo/src/web \
    "${NODE_IMAGE}" \
    bash -ce '
      set -euo pipefail
      if [[ -f package-lock.json ]]; then
        npm ci
      else
        npm install
      fi
      npm run build
      if [[ -n "${HOST_UID:-}" && -n "${HOST_GID:-}" ]]; then
        chown -R "${HOST_UID}:${HOST_GID}" /repo/src/master/ui/dist 2>/dev/null || true
      fi
    '
else
  echo "Neither npm nor docker available to build Master UI" >&2
  exit 1
fi

[[ -f "${UI_OUT}/index.html" ]] || { echo "${UI_OUT}/index.html missing after UI build" >&2; exit 1; }
echo "==> Master UI ready: ${UI_OUT}/"
ls -la "${UI_OUT}/"
