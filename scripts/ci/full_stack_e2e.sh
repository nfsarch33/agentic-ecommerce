#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
frontend_repo="${EC_FRONTEND_REPO_PATH:-/home/jason/Code/agentic-ecommerce-web}"
run_stamp="${CI_PIPELINE_ID:-manual}-$(date -u +%Y%m%dT%H%M%SZ)"
artifact_dir="${repo_root}/.gitlab-artifacts/full-stack-e2e/${run_stamp}"
host_log_dir="${HOME}/logs/runx/test-lanes/full-stack-e2e/${run_stamp}"
env_file="${artifact_dir}/.env.compose"
mkdir -p "${artifact_dir}" "${host_log_dir}"

if [[ ! -d "${frontend_repo}" ]]; then
  echo "frontend repo not found at ${frontend_repo}" >&2
  exit 1
fi

if [[ ! -f "${frontend_repo}/package.json" ]]; then
  echo "frontend repo at ${frontend_repo} does not look valid" >&2
  exit 1
fi

cp "${repo_root}/.env.compose.example" "${env_file}"
cat >>"${env_file}" <<EOF
BIND_HOST=127.0.0.1
WEB_IMAGE_TAG=gitlab-local
ECOMMERCE_IMAGE_TAG=gitlab-local
VERSION=${VERSION:-v8.x-dev}
COMMIT=${CI_COMMIT_SHA:-local}
POSTGRES_PASSWORD=${POSTGRES_PASSWORD:-gitlab-local-postgres}
GRAFANA_ADMIN_PASSWORD=${GRAFANA_ADMIN_PASSWORD:-gitlab-local-grafana}
ECOMMERCE_JWT_SECRET=${ECOMMERCE_JWT_SECRET:-gitlab-local-jwt-secret-0123456789}
ECOMMERCE_ADMIN_USERNAME=${ECOMMERCE_ADMIN_USERNAME:-admin@example.invalid}
ECOMMERCE_ADMIN_PASSWORD=${ECOMMERCE_ADMIN_PASSWORD:-gitlab-local-admin}
ECOMMERCE_ALLOWED_ORIGIN=http://127.0.0.1:3000
EC_FRONTEND_REPO_PATH=${frontend_repo}
NEXT_PUBLIC_APP_ORIGIN=https://127.0.0.1:3000
NEXT_PUBLIC_MC_API_BASE_URL=https://127.0.0.1:8080
EOF

wait_for_http() {
  local url="$1"
  local label="$2"
  local attempts="${3:-40}"
  local sleep_seconds="${4:-3}"
  local try=1
  while (( try <= attempts )); do
    if curl -fsS "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep "${sleep_seconds}"
    ((try += 1))
  done
  echo "timed out waiting for ${label} at ${url}" >&2
  return 1
}

collect_compose_state() {
  docker compose --env-file "${env_file}" -f docker-compose.yml -f docker-compose.gitlab-local.yml ps \
    >"${artifact_dir}/compose-ps.txt" 2>&1 || true
  docker compose --env-file "${env_file}" -f docker-compose.yml -f docker-compose.gitlab-local.yml logs --tail 200 \
    >"${artifact_dir}/compose-logs.txt" 2>&1 || true
}

docker compose --env-file "${env_file}" -f docker-compose.yml -f docker-compose.gitlab-local.yml up -d --build \
  mc-api frontend postgres redis prometheus grafana 2>&1 | tee "${artifact_dir}/compose-up.log"

trap 'collect_compose_state; cp -R "${artifact_dir}/." "${host_log_dir}/" 2>/dev/null || true' EXIT

wait_for_http "http://127.0.0.1:8080/healthz" "backend health"
wait_for_http "http://127.0.0.1:8080/readyz" "backend readiness"
wait_for_http "http://127.0.0.1:3000/healthz" "frontend health"
wait_for_http "http://127.0.0.1:3000/readyz" "frontend readiness"

(
  cd "${frontend_repo}"
  bun install --frozen-lockfile
  CI=1 \
  E2E_LIVE_STACK=true \
  PLAYWRIGHT_DISABLE_WEBSERVER=true \
  PLAYWRIGHT_BASE_URL=http://127.0.0.1:3000 \
  PLAYWRIGHT_HTML_REPORT="${artifact_dir}/playwright-report" \
  MC_API_BASE_URL=http://127.0.0.1:8080 \
  NEXT_PUBLIC_MC_API_BASE_URL=http://127.0.0.1:8080 \
  bun run test:e2e:local-stack
) 2>&1 | tee "${artifact_dir}/playwright.log"

collect_compose_state
