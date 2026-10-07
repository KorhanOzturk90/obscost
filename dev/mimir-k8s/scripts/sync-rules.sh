#!/usr/bin/env bash
# Sync each tenant's rules into the ruler with mimirtool, from inside the
# cluster (a one-shot pod), so nothing needs installing on the host.
#
#   ./scripts/sync-rules.sh                 # every tenant under rules/<tenant>/
#   ./scripts/sync-rules.sh analytics extra.yaml   # tenant + extra files (scenarios)
#
# `rules sync` makes the ruler match the given files exactly: namespaces
# absent from them are deleted. So a scenario file is synced *together
# with* the tenant's baseline files, and syncing the baseline alone removes
# the scenario again.
set -euo pipefail
cd "$(dirname "$0")/.."

MIMIRTOOL_IMAGE=grafana/mimirtool:2.17.0
ADDRESS=http://mimir-gateway.mimir.svc

sync_tenant() {
  local tenant=$1; shift
  local files=(rules/"$tenant"/*.yaml "$@")
  local cm="rules-$tenant"
  kubectl -n tenants create configmap "$cm" $(printf -- '--from-file=%s ' "${files[@]}") \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n tenants delete pod "mimirtool-$tenant" --ignore-not-found >/dev/null
  # Files are passed explicitly rather than with --rule-dirs: a ConfigMap
  # mount also contains a hidden ..data/ copy of every file, which
  # --rule-dirs walks into and then rejects as duplicate namespaces.
  local args='"rules","sync","--address='"$ADDRESS"'","--id='"$tenant"'"'
  local f
  for f in "${files[@]}"; do args+=',"/rules/'"$(basename "$f")"'"'; done
  kubectl -n tenants run "mimirtool-$tenant" --restart=Never --image="$MIMIRTOOL_IMAGE" \
    --overrides="$(cat <<JSON
{"spec":{"volumes":[{"name":"rules","configMap":{"name":"$cm"}}],
 "containers":[{"name":"mimirtool","image":"$MIMIRTOOL_IMAGE","args":[$args],
   "volumeMounts":[{"name":"rules","mountPath":"/rules"}]}]}}
JSON
)" >/dev/null
  local phase=""
  for _ in $(seq 120); do
    phase=$(kubectl -n tenants get pod "mimirtool-$tenant" -o jsonpath='{.status.phase}')
    [[ $phase == Succeeded || $phase == Failed ]] && break
    sleep 1
  done
  if [[ $phase != Succeeded ]]; then
    echo "mimirtool for $tenant ended in phase '$phase':" >&2
    kubectl -n tenants logs "mimirtool-$tenant" >&2
    exit 1
  fi
  echo "synced $tenant: $(kubectl -n tenants logs "mimirtool-$tenant" | tail -1)"
  kubectl -n tenants delete pod "mimirtool-$tenant" >/dev/null
}

if [[ $# -gt 0 ]]; then
  tenant=$1; shift
  sync_tenant "$tenant" "$@"
else
  for dir in rules/*/; do
    tenant=$(basename "$dir")
    # scenarios/ holds opt-in rules; monitoring's mixin rules are synced
    # only by `make mixin-rules`, which names the tenant explicitly.
    [[ $tenant == scenarios || $tenant == monitoring ]] && continue
    sync_tenant "$tenant"
  done
fi
