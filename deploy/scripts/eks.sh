#!/usr/bin/env bash
# EKS-specific deployment functions sourced by deploy.sh.
# All account IDs and regions come from env vars (public-repo-gate compliant).

setup_kubeconfig() {
  log "Configuring kubeconfig for EKS (region=${REGION})"
  run_cmd aws eks update-kubeconfig \
    --name "${EC_HELM_RELEASE_NAME:-agentic-ecommerce}" \
    --region "$REGION"
}

verify_deployment_health() {
  log "Verifying EKS deployment health..."

  local ns="agentic-ecommerce"
  local endpoints=(mc-api frontend agent-worker)

  run_cmd kubectl get pods -n "$ns" -o wide

  for ep in "${endpoints[@]}"; do
    local svc_url
    svc_url="$(kubectl get svc "$ep" -n "$ns" -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || echo "")"
    if [ -n "$svc_url" ]; then
      log "Health check: $ep -> http://${svc_url}:8080/healthz"
      run_cmd curl -sf --max-time 10 "http://${svc_url}:8080/healthz" || log "WARN: $ep health check failed"
    else
      log "WARN: No external hostname for $ep"
    fi
  done

  run_cmd kubectl get ingress -n "$ns" 2>/dev/null || true
  log "EKS health verification complete"
}
