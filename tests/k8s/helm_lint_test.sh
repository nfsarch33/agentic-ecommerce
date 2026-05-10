#!/usr/bin/env bash
# QA-1: Lint the Helm chart.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CHART_DIR="${SCRIPT_DIR}/../../deploy/helm/agentic-ecommerce"

echo "==> helm lint"
helm lint "${CHART_DIR}" 2>&1

echo "PASS: Helm chart linted successfully"
