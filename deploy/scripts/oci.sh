#!/usr/bin/env bash
# OCI-specific deployment functions sourced by deploy.sh.
# All tenancy/compartment IDs come from env vars (public-repo-gate compliant).

setup_kubeconfig() {
  log "Configuring kubeconfig for OKE (region=${REGION})"
  run_cmd oci ce cluster create-kubeconfig \
    --cluster-id "${EC_OKE_CLUSTER_ID:-placeholder}" \
    --region "$REGION" \
    --file "$HOME/.kube/config" \
    --token-version 2.0.0
}

verify_deployment_health() {
  log "Verifying OCI deployment health..."

  local ns="agentic-ecommerce"

  run_cmd kubectl get pods -n "$ns" -o wide

  log "OCI health verification complete"
}
