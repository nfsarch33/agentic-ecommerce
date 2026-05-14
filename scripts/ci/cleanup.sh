#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
artifact_dir="${repo_root}/.gitlab-artifacts/cleanup"
mkdir -p "${artifact_dir}"

GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go run ./cmd/testing-lane --lane=cleanup-testing \
  >"${artifact_dir}/summary.json"
