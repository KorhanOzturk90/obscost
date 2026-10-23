#!/usr/bin/env bash
# Install or upgrade kube-prometheus-stack (meta-monitoring).
set -euo pipefail
source "$(dirname "$0")/lib.sh"
helm_install kps_release --wait --timeout 10m
kubectl -n "$MONITORING_NS" rollout status statefulset/prometheus-kps-prometheus --timeout=5m >/dev/null
log "meta-monitoring deployed; Prometheus remote-writes to tenant 'monitoring'"
