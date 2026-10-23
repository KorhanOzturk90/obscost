#!/usr/bin/env bash
# Interim tenant traffic, until the validation lane's dev/staging/workload/
# replaces it: dev/mimir-k8s's tenants and their per-tenant Alloy agent.
set -euo pipefail
source "$(dirname "$0")/../scripts/lib.sh"
"$(dirname "$0")/workloads.sh" | kubectl apply -f - >/dev/null
render_tenant_agent_config
helm_install tenant_agent_release --wait --timeout 5m
kubectl -n "$TENANTS_NS" rollout status deploy --timeout=180s >/dev/null
log "interim tenants running: $(kubectl -n "$TENANTS_NS" get deploy -l app=avalanche --no-headers | wc -l) generators"
