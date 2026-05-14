#!/usr/bin/env bash
set -euo pipefail

state="${1:-}"
description="${2:-GitLab pipeline update}"

if [[ -z "${state}" ]]; then
  echo "usage: github_status.sh <pending|success|failure|error> [description]" >&2
  exit 2
fi

if [[ -z "${GITHUB_TOKEN:-}" || -z "${GITHUB_REPOSITORY:-}" || -z "${GITHUB_SHA:-}" ]]; then
  echo "github status bridge skipped: missing GITHUB_TOKEN, GITHUB_REPOSITORY, or GITHUB_SHA" >&2
  exit 0
fi

target_url="${GITHUB_TARGET_URL:-${CI_PIPELINE_URL:-}}"
context="${GITHUB_STATUS_CONTEXT:-gitlab/node-a-primary}"
api_url="https://api.github.com/repos/${GITHUB_REPOSITORY}/statuses/${GITHUB_SHA}"

payload="$(python3 - "${state}" "${description}" "${context}" "${target_url}" <<'PY'
import json
import sys

state, description, context, target_url = sys.argv[1:5]
payload = {
    "state": state,
    "description": description[:140],
    "context": context,
}
if target_url:
    payload["target_url"] = target_url
print(json.dumps(payload))
PY
)"

curl --fail --silent --show-error \
  -X POST \
  -H "Accept: application/vnd.github+json" \
  -H "Authorization: Bearer ${GITHUB_TOKEN}" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  "${api_url}" \
  -d "${payload}" >/dev/null
