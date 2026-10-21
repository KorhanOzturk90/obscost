#!/usr/bin/env bash
# The tenants to cost, comma-separated, always including `monitoring` (a
# tenant on the cluster like any other: its series cost ingester memory
# too). The rest come from whatever traffic is deployed:
#   - the validation lane's loadgen (namespace loadgen), if running: the
#     tenants in its ground truth, so the names always match its profile
#     (steady, cardinal, dashboards, ...) with nothing to remap;
#   - otherwise the interim tenants dev/mimir-k8s's Alloy config writes to.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
{
  echo monitoring
  if kubectl -n loadgen get service loadgen >/dev/null 2>&1; then
    kubectl get --raw /api/v1/namespaces/loadgen/services/loadgen:http/proxy/ground-truth | jq -r '.tenants | keys[]'
  else
    grep -o 'X-Scope-OrgID" = "[^"]*"' "$REPO_ROOT/dev/mimir-k8s/alloy/config.alloy" | cut -d'"' -f3
  fi
} | sort -u | paste -sd, -
