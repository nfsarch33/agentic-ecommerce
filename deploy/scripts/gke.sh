#!/usr/bin/env bash
# GKE-specific deployment functions sourced by deploy.sh.
# All project IDs and regions come from env vars (public-repo-gate compliant).

setup_kubeconfig() {
  log "Configuring kubeconfig for GKE Autopilot (project=${EC_PROJECT_ID}, region=${REGION})"
  run_cmd gcloud container clusters get-credentials \
    "${EC_HELM_RELEASE_NAME:-agentic-ecommerce}" \
    --region "$REGION" \
    --project "$EC_PROJECT_ID"
}

verify_deployment_health() {
  log "Verifying GKE deployment health..."

  local ns="agentic-ecommerce"
  local endpoints=(mc-api frontend agent-worker)

  run_cmd kubectl get pods -n "$ns" -o wide

  for ep in "${endpoints[@]}"; do
    local svc_url
    svc_url="$(kubectl get svc "$ep" -n "$ns" -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "")"
    if [ -n "$svc_url" ]; then
      log "Health check: $ep -> http://${svc_url}:8080/healthz"
      run_cmd curl -sf --max-time 10 "http://${svc_url}:8080/healthz" || log "WARN: $ep health check failed"
    else
      log "WARN: No external IP for $ep (may use ClusterIP or Ingress)"
    fi
  done

  run_cmd kubectl get ingress -n "$ns" 2>/dev/null || true
  log "GKE health verification complete"
}
