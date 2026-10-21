#!/usr/bin/env bash
# Preload every image `make deploy` will run into the local k3s node.
# The list is derived, not maintained: each release is rendered with the
# exact arguments deploy uses (scripts/lib.sh), plus the plain manifests.
set -euo pipefail
cd "$(dirname "$0")"
source ./lib.sh
source ../scripts/lib.sh

render_tenant_agent_config
images=$(
  {
    helm_render mimir_release
    helm_render kps_release
    helm_render tenant_agent_release
    cat ../manifests/*.yaml
    ../interim-tenants/workloads.sh
    # Started by scripts outside any manifest.
    echo "image: grafana/mimirtool:2.17.0"   # dev/mimir-k8s/scripts/sync-rules.sh
  } | grep -E '^\s*(- )?image:|config-reloader=' | grep -v '^\s*#' |
    grep -oE '(image: *"?|config-reloader=)[^" ]+' | sed -E 's/^(image: *"?|config-reloader=)//' |
    grep -v '{{' | sort -u
)
log "preloading $(wc -l <<<"$images") images into $NODE"
# shellcheck disable=SC2086
preload $images
