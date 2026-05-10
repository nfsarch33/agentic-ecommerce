#!/usr/bin/env bash
# QA-1: Validate GKE Autopilot Terraform module.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TF_DIR="${SCRIPT_DIR}/../../deploy/terraform/gke"

echo "==> terraform init -backend=false (GKE module)"
terraform -chdir="${TF_DIR}" init -backend=false -input=false 2>&1

echo "==> terraform validate"
terraform -chdir="${TF_DIR}" validate 2>&1

echo "PASS: Terraform GKE module is valid"
