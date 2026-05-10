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

  local mem0_pod
  mem0_pod="$(kubectl get pods -n "$ns" -l app=mem0 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")"
  if [ -n "$mem0_pod" ]; then
    log "mem0 pod found: $mem0_pod"
    run_cmd kubectl exec -n "$ns" "$mem0_pod" -- curl -sf http://localhost:8080/healthz || log "WARN: mem0 health check failed"
  fi

  local qdrant_pod
  qdrant_pod="$(kubectl get pods -n "$ns" -l app=qdrant -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")"
  if [ -n "$qdrant_pod" ]; then
    log "Qdrant pod found: $qdrant_pod"
    run_cmd kubectl exec -n "$ns" "$qdrant_pod" -- curl -sf http://localhost:6333/healthz || log "WARN: Qdrant health check failed"
  fi

  log "OCI health verification complete"
}
