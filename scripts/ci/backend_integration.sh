#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
artifact_dir="${repo_root}/.gitlab-artifacts/backend-integration"
mkdir -p "${artifact_dir}"

GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go run ./cmd/testing-lane --lane=backend-integration \
  2>&1 | tee "${artifact_dir}/backend-integration.log"
