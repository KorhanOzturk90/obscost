#!/usr/bin/env bash
# Apply the Prometheus Operator CRDs bundled with kube-prometheus-stack,
# server-side (they are too large for client-side apply's annotation).
# Re-running upgrades them, which `helm install` alone never does.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
tar xzf "$CHARTS/kube-prometheus-stack-$KPS_CHART_VERSION.tgz" -C "$tmp" kube-prometheus-stack/charts/crds/crds
kubectl apply --server-side --force-conflicts -f "$tmp/kube-prometheus-stack/charts/crds/crds/" >/dev/null
for _ in $(seq 30); do
  kubectl wait --for=condition=Established crd/servicemonitors.monitoring.coreos.com crd/prometheusrules.monitoring.coreos.com --timeout=10s >/dev/null 2>&1 && break
  sleep 2
done
log "Prometheus Operator CRDs applied"
