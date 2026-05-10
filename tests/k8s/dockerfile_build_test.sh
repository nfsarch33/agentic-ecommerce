#!/usr/bin/env bash
# QA-1: Build all 8 binary images and verify size <30MB each.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="${SCRIPT_DIR}/../.."
cd "${REPO_ROOT}"

BINARIES="mc-api wc-sync content-worker agent-worker temporal-worker uiauto-compare ec-cli evomap-rollup"
MAX_SIZE_MB=30
FAILURES=0

for binary in ${BINARIES}; do
  IMAGE_TAG="agentic-ecommerce-${binary}:test"
  echo "==> Building ${binary} ..."
  docker build \
    --build-arg TARGET="${binary}" \
    --build-arg VERSION=test \
    --build-arg COMMIT=test \
    --target "${binary}" \
    -t "${IMAGE_TAG}" \
    -f Dockerfile \
    . 2>&1 | tail -3

  SIZE_BYTES=$(docker image inspect "${IMAGE_TAG}" --format='{{.Size}}' 2>/dev/null || echo "0")
  SIZE_MB=$((SIZE_BYTES / 1048576))
  echo "    ${binary}: ${SIZE_MB}MB"

  if [ "${SIZE_MB}" -gt "${MAX_SIZE_MB}" ]; then
    echo "    FAIL: ${binary} image ${SIZE_MB}MB exceeds ${MAX_SIZE_MB}MB limit"
    FAILURES=$((FAILURES + 1))
  fi

  docker rmi "${IMAGE_TAG}" >/dev/null 2>&1 || true
done

if [ "${FAILURES}" -gt 0 ]; then
  echo "FAIL: ${FAILURES} image(s) exceeded size limit"
  exit 1
fi

echo "PASS: All 8 images built under ${MAX_SIZE_MB}MB"
