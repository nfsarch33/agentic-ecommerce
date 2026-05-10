#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

CLOUD=""
ENVIRONMENT=""
REGION=""
DRY_RUN=false
SKIP_TERRAFORM=false
SKIP_BUILD=false
SKIP_HELM=false
SKIP_VERIFY=false

usage() {
  cat <<EOF
Usage: $(basename "$0") --cloud <gke|eks|oci> --env <dev|staging|prod> --region <region> [OPTIONS]

Deploy the Agentic E-Commerce stack to any supported cloud.

Required:
  --cloud <gke|eks|oci>           Target cloud platform
  --env <dev|staging|prod>        Deployment environment
  --region <region>               Cloud region (e.g. australia-southeast1, ap-southeast-2)

Options:
  --dry-run                       Print commands without executing
  --skip-terraform                Skip Terraform apply step
  --skip-build                    Skip Docker build+push step
  --skip-helm                     Skip Helm deploy step
  --skip-verify                   Skip health verification step
  -h, --help                      Show this help message

Environment variables (must be set per public-repo-gate):
  EC_PROJECT_ID                   Cloud project/account ID
  EC_REGISTRY_URL                 Docker registry URL
  EC_REGISTRY_TOKEN               Docker registry auth token (or use cloud CLI auth)
  EC_TERRAFORM_STATE_BUCKET       Remote state bucket name
  EC_HELM_RELEASE_NAME            Helm release name (default: agentic-ecommerce)
EOF
  exit 0
}

log() { printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }
err() { log "ERROR: $*" >&2; }
die() { err "$@"; exit 1; }

run_cmd() {
  if [ "$DRY_RUN" = true ]; then
    log "[DRY-RUN] $*"
  else
    log "Running: $*"
    "$@"
  fi
}

check_prerequisites() {
  local missing=()
  local required_common=(terraform helm kubectl docker)

  for tool in "${required_common[@]}"; do
    if ! command -v "$tool" &>/dev/null; then
      missing+=("$tool")
    fi
  done

  case "$CLOUD" in
    gke)
      if ! command -v gcloud &>/dev/null; then missing+=(gcloud); fi
      ;;
    eks)
      if ! command -v aws &>/dev/null; then missing+=(aws); fi
      ;;
    oci)
      if ! command -v oci &>/dev/null; then missing+=(oci); fi
      ;;
  esac

  if [ ${#missing[@]} -gt 0 ]; then
    die "Missing required tools: ${missing[*]}"
  fi

  log "All prerequisites satisfied for cloud=$CLOUD"
}

validate_env_vars() {
  local missing=()

  if [ -z "${EC_PROJECT_ID:-}" ]; then missing+=(EC_PROJECT_ID); fi
  if [ -z "${EC_REGISTRY_URL:-}" ]; then missing+=(EC_REGISTRY_URL); fi
  if [ -z "${EC_TERRAFORM_STATE_BUCKET:-}" ]; then missing+=(EC_TERRAFORM_STATE_BUCKET); fi

  if [ ${#missing[@]} -gt 0 ]; then
    die "Missing required environment variables: ${missing[*]}"
  fi

  log "Environment variables validated"
}

apply_terraform() {
  if [ "$SKIP_TERRAFORM" = true ]; then
    log "Skipping Terraform (--skip-terraform)"
    return 0
  fi

  local tf_dir="${REPO_ROOT}/deploy/terraform/${CLOUD}"
  if [ ! -d "$tf_dir" ]; then
    die "Terraform directory not found: $tf_dir"
  fi

  log "Applying Terraform for cloud=$CLOUD env=$ENVIRONMENT region=$REGION"
  run_cmd terraform -chdir="$tf_dir" init \
    -backend-config="bucket=${EC_TERRAFORM_STATE_BUCKET}" \
    -backend-config="prefix=agentic-ecommerce/${ENVIRONMENT}"
  run_cmd terraform -chdir="$tf_dir" plan \
    -var="project_id=${EC_PROJECT_ID}" \
    -var="region=${REGION}" \
    -var="environment=${ENVIRONMENT}" \
    -out=tfplan
  run_cmd terraform -chdir="$tf_dir" apply tfplan
}

build_and_push() {
  if [ "$SKIP_BUILD" = true ]; then
    log "Skipping Docker build (--skip-build)"
    return 0
  fi

  local commit_sha
  commit_sha="$(git -C "$REPO_ROOT" rev-parse --short HEAD)"
  local binaries=(mc-api wc-sync content-worker carrier-bridge frontend agent-worker ec-cli temporal-worker)

  log "Building and pushing ${#binaries[@]} images (sha=$commit_sha)"
  for binary in "${binaries[@]}"; do
    run_cmd docker build \
      --build-arg "TARGET=$binary" \
      --build-arg "COMMIT=$commit_sha" \
      -t "${EC_REGISTRY_URL}:${commit_sha}-${binary}" \
      "$REPO_ROOT"
    run_cmd docker push "${EC_REGISTRY_URL}:${commit_sha}-${binary}"
  done
}

deploy_helm() {
  if [ "$SKIP_HELM" = true ]; then
    log "Skipping Helm deploy (--skip-helm)"
    return 0
  fi

  local release_name="${EC_HELM_RELEASE_NAME:-agentic-ecommerce}"
  local chart_dir="${REPO_ROOT}/deploy/helm/agentic-ecommerce"
  local commit_sha
  commit_sha="$(git -C "$REPO_ROOT" rev-parse --short HEAD)"

  log "Deploying Helm chart: release=$release_name env=$ENVIRONMENT"

  # Delegate cloud-specific kubeconfig setup
  source "${SCRIPT_DIR}/${CLOUD}.sh"
  setup_kubeconfig

  run_cmd helm upgrade --install "$release_name" "$chart_dir" \
    --namespace agentic-ecommerce \
    --create-namespace \
    --set "global.environment=${ENVIRONMENT}" \
    --set "global.image.registry=${EC_REGISTRY_URL}" \
    --set "global.image.tag=${commit_sha}" \
    --wait \
    --timeout 10m
}

verify_health() {
  if [ "$SKIP_VERIFY" = true ]; then
    log "Skipping health verification (--skip-verify)"
    return 0
  fi

  log "Verifying deployment health..."

  # Delegate cloud-specific health verification
  source "${SCRIPT_DIR}/${CLOUD}.sh"
  verify_deployment_health
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --cloud)      CLOUD="$2"; shift 2 ;;
    --env)        ENVIRONMENT="$2"; shift 2 ;;
    --region)     REGION="$2"; shift 2 ;;
    --dry-run)    DRY_RUN=true; shift ;;
    --skip-terraform) SKIP_TERRAFORM=true; shift ;;
    --skip-build) SKIP_BUILD=true; shift ;;
    --skip-helm)  SKIP_HELM=true; shift ;;
    --skip-verify) SKIP_VERIFY=true; shift ;;
    -h|--help)    usage ;;
    *)            die "Unknown option: $1" ;;
  esac
done

if [ -z "$CLOUD" ] || [ -z "$ENVIRONMENT" ] || [ -z "$REGION" ]; then
  die "Required: --cloud, --env, and --region. Run with --help for usage."
fi

if [[ ! "$CLOUD" =~ ^(gke|eks|oci)$ ]]; then
  die "Unsupported cloud: $CLOUD. Supported: gke, eks, oci"
fi

if [[ ! "$ENVIRONMENT" =~ ^(dev|staging|prod)$ ]]; then
  die "Unsupported environment: $ENVIRONMENT. Supported: dev, staging, prod"
fi

log "=== Agentic E-Commerce Deploy ==="
log "Cloud:       $CLOUD"
log "Environment: $ENVIRONMENT"
log "Region:      $REGION"
log "Dry-run:     $DRY_RUN"
log ""

check_prerequisites
validate_env_vars
apply_terraform
build_and_push
deploy_helm
verify_health

log ""
log "=== Deploy complete ==="
