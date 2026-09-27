#!/usr/bin/env bash
# Build (and optionally push) the wireguard-mc Docker image.
# Usage (from repo root):
#   bash scripts/package-docker.sh
#   bash scripts/package-docker.sh --push
#   VERSION=v1.1.5 IMAGE=ghcr.io/OWNER/REPO bash scripts/package-docker.sh --push
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PUSH=0
for a in "$@"; do
  case "$a" in
    --push) PUSH=1 ;;
    -h|--help)
      sed -n '2,7p' "$0"
      exit 0
      ;;
    *)
      echo "Unknown arg: $a" >&2
      exit 1
      ;;
  esac
done

VERSION_RAW="$(tr -d '\r\n\t ' < .github/build/version.txt 2>/dev/null || true)"
VERSION_RAW="${VERSION:-${VERSION_RAW:-dev}}"
if [[ "$VERSION_RAW" == v* ]]; then
  TAG="$VERSION_RAW"
  DEB_UPSTREAM="${VERSION_RAW#v}"
else
  TAG="v${VERSION_RAW}"
  DEB_UPSTREAM="$VERSION_RAW"
fi

GO_VERSION="${GO_VERSION:-1.23.1}"
IMAGE="${IMAGE:-wireguard-mc}"
PLATFORM="${PLATFORM:-}"

echo "==> Building ${IMAGE}:${TAG} (GO_VERSION=${GO_VERSION})"
build_args=(
  -f Dockerfile
  --build-arg "VERSION=${TAG}"
  --build-arg "GO_VERSION=${GO_VERSION}"
  -t "${IMAGE}:${TAG}"
  -t "${IMAGE}:${DEB_UPSTREAM}"
  -t "${IMAGE}:latest"
)
if [[ -n "$PLATFORM" ]]; then
  build_args+=(--platform "$PLATFORM")
fi

docker build "${build_args[@]}" .

echo "Built:"
echo "  ${IMAGE}:${TAG}"
echo "  ${IMAGE}:${DEB_UPSTREAM}"
echo "  ${IMAGE}:latest"

if [[ "$PUSH" -eq 1 ]]; then
  echo "==> Pushing"
  docker push "${IMAGE}:${TAG}"
  docker push "${IMAGE}:${DEB_UPSTREAM}"
  docker push "${IMAGE}:latest"
fi
