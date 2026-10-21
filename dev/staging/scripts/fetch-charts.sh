#!/usr/bin/env bash
# Download the pinned chart archives from their GitHub releases into
# .state/charts. Not `helm repo add`: grafana.github.io (the repo index)
# is denied in the cloud container, while GitHub release assets are not.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
mkdir -p "$CHARTS"

fetch() {
  local file=$1 url=$2
  [[ -s $CHARTS/$file ]] && return 0
  log "fetching $file"
  curl -fsSL --retry 4 -o "$CHARTS/$file.tmp" "$url"
  mv "$CHARTS/$file.tmp" "$CHARTS/$file"
}

fetch "mimir-distributed-$MIMIR_CHART_VERSION.tgz" \
  "https://github.com/grafana/helm-charts/releases/download/mimir-distributed-$MIMIR_CHART_VERSION/mimir-distributed-$MIMIR_CHART_VERSION.tgz"
fetch "alloy-$ALLOY_CHART_VERSION.tgz" \
  "https://github.com/grafana/helm-charts/releases/download/alloy-$ALLOY_CHART_VERSION/alloy-$ALLOY_CHART_VERSION.tgz"
fetch "kube-prometheus-stack-$KPS_CHART_VERSION.tgz" \
  "https://github.com/prometheus-community/helm-charts/releases/download/kube-prometheus-stack-$KPS_CHART_VERSION/kube-prometheus-stack-$KPS_CHART_VERSION.tgz"
