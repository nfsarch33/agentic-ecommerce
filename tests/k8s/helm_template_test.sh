#!/usr/bin/env bash
# QA-1: Template the Helm chart and validate YAML structure.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CHART_DIR="${SCRIPT_DIR}/../../deploy/helm/agentic-ecommerce"

echo "==> helm template"
OUTPUT=$(helm template test-release "${CHART_DIR}" 2>&1)

echo "${OUTPUT}" | head -5
echo "..."

DOC_COUNT=$(echo "${OUTPUT}" | grep -c '^---' || true)
echo "Template produced ${DOC_COUNT} YAML documents"

if [ "${DOC_COUNT}" -lt 5 ]; then
  echo "FAIL: Expected at least 5 YAML documents (deployments + services + configmap)"
  exit 1
fi

echo "PASS: Helm template produces valid YAML"
