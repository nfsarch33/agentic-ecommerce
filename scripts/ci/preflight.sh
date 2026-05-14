#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
artifact_dir="${repo_root}/.gitlab-artifacts/preflight"
mkdir -p "${artifact_dir}"

go version | tee "${artifact_dir}/go-version.txt"
docker version >"${artifact_dir}/docker-version.txt"
docker compose --env-file .env.compose.example -f docker-compose.yml -f docker-compose.gitlab-local.yml config --quiet
docker compose -f docker-compose.dev.yml config --quiet
GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go mod download
make testing-lane

if command -v runx >/dev/null 2>&1; then
  runx version >"${artifact_dir}/runx-version.txt" 2>/dev/null || true
fi

if command -v cursor-tools >/dev/null 2>&1; then
  cursor-tools version >"${artifact_dir}/cursor-tools-version.txt" 2>/dev/null || true
fi
