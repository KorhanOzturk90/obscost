#!/usr/bin/env bash
# Install or upgrade Mimir in the selected PROFILE.
set -euo pipefail
source "$(dirname "$0")/lib.sh"

# A cluster's write path cannot be switched in place (not a supported
# Mimir migration); refuse rather than leave a half-converted cluster.
if helm -n "$MIMIR_NS" status mimir >/dev/null 2>&1; then
  if kubectl -n "$MIMIR_NS" get statefulset mimir-kafka >/dev/null 2>&1; then current=ingest; else current=classic; fi
  if [[ $current != "$PROFILE" ]]; then
    echo "Mimir is deployed with PROFILE=$current; switching to $PROFILE in place is not supported. 'make undeploy' first." >&2
    exit 1
  fi
fi

kubectl create namespace "$MIMIR_NS" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
# The bucket credentials, in the env-var form values/mimir.yaml expands.
kubectl -n "$MIMIR_NS" create secret generic mimir-bucket \
  --from-literal=BUCKET_ACCESS_KEY_ID=staging \
  --from-literal=BUCKET_SECRET_ACCESS_KEY=staging-not-a-secret \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl create namespace "$MONITORING_NS" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

helm_install mimir_release --wait --timeout 20m
kubectl -n "$MIMIR_NS" label configmap mimir-config staging.obscost/profile="$PROFILE" --overwrite >/dev/null 2>&1 || true
log "Mimir deployed (PROFILE=$PROFILE)"
