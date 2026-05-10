#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
DEPLOY_SCRIPT="${REPO_ROOT}/deploy/scripts/deploy.sh"

PASS=0
FAIL=0

assert_contains() {
  local label="$1" output="$2" expected="$3"
  if echo "$output" | grep -qF "$expected"; then
    echo "  PASS: $label"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $label (expected '$expected' in output)"
    FAIL=$((FAIL + 1))
  fi
}

assert_exit_code() {
  local label="$1" actual="$2" expected="$3"
  if [ "$actual" -eq "$expected" ]; then
    echo "  PASS: $label (exit $actual)"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $label (expected exit $expected, got $actual)"
    FAIL=$((FAIL + 1))
  fi
}

echo "=== Deploy Script Tests ==="
echo ""

# -------------------------------------------------------
# Scenario 1: GKE dry-run validates command sequence
# -------------------------------------------------------
echo "Scenario 1: GKE dry-run"

export EC_PROJECT_ID="test-project-123"
export EC_REGISTRY_URL="gcr.io/test-project-123/agentic-ecommerce"
export EC_TERRAFORM_STATE_BUCKET="test-tf-state"

output=$(bash "$DEPLOY_SCRIPT" --cloud gke --env prod --region australia-southeast1 --dry-run 2>&1 || true)

assert_contains "Shows cloud=gke" "$output" "Cloud:       gke"
assert_contains "Shows env=prod" "$output" "Environment: prod"
assert_contains "Shows region" "$output" "Region:      australia-southeast1"
assert_contains "Shows dry-run=true" "$output" "Dry-run:     true"
assert_contains "Terraform dry-run logged" "$output" "[DRY-RUN]"
assert_contains "Docker build dry-run" "$output" "docker build"
assert_contains "Helm deploy dry-run" "$output" "helm upgrade"

echo ""

# -------------------------------------------------------
# Scenario 2: EKS dry-run validates different cloud CLI
# -------------------------------------------------------
echo "Scenario 2: EKS dry-run"

output=$(bash "$DEPLOY_SCRIPT" --cloud eks --env staging --region ap-southeast-2 --dry-run 2>&1 || true)

assert_contains "Shows cloud=eks" "$output" "Cloud:       eks"
assert_contains "Shows env=staging" "$output" "Environment: staging"
assert_contains "EKS Terraform path" "$output" "deploy/terraform/eks"

echo ""

# -------------------------------------------------------
# Scenario 3: Prerequisites check with missing tool
# -------------------------------------------------------
echo "Scenario 3: Prerequisites check with missing tool"

# Create a temporary PATH that excludes common tools to trigger the prereq check
FAKE_PATH="/usr/bin:/bin"
output=$(PATH="$FAKE_PATH" bash "$DEPLOY_SCRIPT" --cloud gke --env dev --region us-central1 2>&1 || true)
ec=$?

assert_contains "Reports missing tools" "$output" "Missing required tools"

echo ""

# -------------------------------------------------------
# Scenario 4: Invalid cloud parameter
# -------------------------------------------------------
echo "Scenario 4: Invalid cloud parameter"

output=$(bash "$DEPLOY_SCRIPT" --cloud azure --env dev --region us-east-1 2>&1 || true)

assert_contains "Rejects unsupported cloud" "$output" "Unsupported cloud: azure"

echo ""

# -------------------------------------------------------
# Scenario 5: Missing required arguments
# -------------------------------------------------------
echo "Scenario 5: Missing required arguments"

output=$(bash "$DEPLOY_SCRIPT" --cloud gke 2>&1 || true)

assert_contains "Requires all three args" "$output" "Required: --cloud, --env, and --region"

echo ""

# -------------------------------------------------------
# Summary
# -------------------------------------------------------
TOTAL=$((PASS + FAIL))
echo "=== Results: $PASS/$TOTAL passed, $FAIL failed ==="
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
