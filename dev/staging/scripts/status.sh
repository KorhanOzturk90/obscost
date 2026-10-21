#!/usr/bin/env bash
# Pods, the rings that matter to cost attribution, and per-tenant active
# series as Mimir sees them (before any CLI arithmetic).
set -euo pipefail
source "$(dirname "$0")/lib.sh"

profile=$(kubectl -n "$MIMIR_NS" get configmap mimir-config -o jsonpath='{.metadata.labels.staging\.obscost/profile}' 2>/dev/null || true)
echo "context: $(kubectl config current-context)   profile: ${profile:-unknown}"
echo
for ns in "$MIMIR_NS" "$MONITORING_NS" "$TENANTS_NS" object-store; do
  kubectl -n "$ns" get pods --no-headers 2>/dev/null |
    awk -v ns="$ns" '{printf "%-13s %-58s %-6s %-18s %s\n", ns, $1, $2, $3, $4}'
done
echo

# Query the monitoring tenant through the gateway from inside the
# cluster, so this works without a port-forward.
q() {
  kubectl -n "$MIMIR_NS" exec deploy/mimir-gateway -- \
    wget -qO- --header "X-Scope-OrgID: monitoring" \
    "http://localhost:8080/prometheus/api/v1/query?query=$(jq -rn --arg q "$1" '$q|@uri')" 2>/dev/null
}
echo "active series per tenant (monitoring tenant's view, one copy per series):"
if [[ $profile == classic ]]; then
  query='sum by (user) (cortex_ingester_active_series) / scalar(max(cortex_distributor_replication_factor))'
else
  query='sum by (user) (max by (ingester_id, user) (label_replace(cortex_ingester_active_series, "ingester_id", "$1", "pod", ".*-([0-9]+)$")))'
fi
rows=$(q "$query" | jq -r '.data.result[]? | "  \(.metric.user)\t\(.value[1] | tonumber | floor)"' | sort) || true
# Right after a deploy there is nothing yet: Prometheus has not scraped
# the ingesters, and ingesters report cortex_ingester_active_series_loading
# for a few minutes after they start.
echo "${rows:-  (no data yet: try again in a few minutes)}"
