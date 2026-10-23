# Shared by scripts/*.sh, interim-tenants/*.sh and events/*.sh.
# Every helm invocation is defined here once, so `make deploy` and
# `make images` (which renders the same releases to list their images)
# cannot drift apart.

STAGING_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
REPO_ROOT=$(cd "$STAGING_DIR/../.." && pwd)
: "${STATE:=$STAGING_DIR/.state}"
: "${PROFILE:=ingest}" "${RULER_EVAL:=remote}" "${DOCKERHUB_ONLY:=1}"
: "${MIMIR_CHART_VERSION:=6.2.0}" "${KPS_CHART_VERSION:=77.0.0}" "${ALLOY_CHART_VERSION:=1.12.1}"
: "${MIMIR_PORT:=8091}" "${GRAFANA_PORT:=3091}"
CHARTS=$STATE/charts
MIMIR_NS=mimir
MONITORING_NS=monitoring
TENANTS_NS=tenants
# In-cluster address of the Mimir gateway (release `mimir`).
MIMIR_GATEWAY=http://mimir-gateway.$MIMIR_NS.svc

log() { printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }

case $RULER_EVAL in
  remote | local) ;;
  *) echo "RULER_EVAL must be remote or local, not '$RULER_EVAL'" >&2; exit 2 ;;
esac

case $PROFILE in
  ingest | classic) ;;
  *) echo "PROFILE must be ingest or classic, not '$PROFILE'" >&2; exit 2 ;;
esac

# <release> <namespace> <chart tgz> <args...> for each release. `helm
# upgrade --install` and `helm template` both take this shape.
mimir_release() {
  local args=(mimir "$MIMIR_NS" "$CHARTS/mimir-distributed-$MIMIR_CHART_VERSION.tgz" -f "$STAGING_DIR/values/mimir.yaml")
  [[ $PROFILE == classic ]] && args+=(-f "$STAGING_DIR/values/profile-classic.yaml")
  [[ $RULER_EVAL == local ]] && args+=(-f "$STAGING_DIR/values/ruler-local.yaml")
  printf '%s\n' "${args[@]}"
}

kps_release() {
  local args=(kps "$MONITORING_NS" "$CHARTS/kube-prometheus-stack-$KPS_CHART_VERSION.tgz" --skip-crds -f "$STAGING_DIR/values/kube-prometheus-stack.yaml")
  [[ $DOCKERHUB_ONLY == 1 ]] && args+=(-f "$STAGING_DIR/values/dockerhub/kube-prometheus-stack.yaml")
  printf '%s\n' "${args[@]}"
}

tenant_agent_release() {
  printf '%s\n' tenant-agent "$TENANTS_NS" "$CHARTS/alloy-$ALLOY_CHART_VERSION.tgz" \
    -f "$STAGING_DIR/interim-tenants/alloy-values.yaml" \
    --set-file "alloy.configMap.content=$STATE/tenant-agent.alloy"
}

# helm_install <release fn> [extra helm args...]
helm_install() {
  local fn=$1; shift
  local spec; mapfile -t spec < <("$fn")
  log "helm upgrade --install ${spec[0]} (namespace ${spec[1]})"
  helm upgrade --install "${spec[0]}" "${spec[2]}" --namespace "${spec[1]}" --create-namespace "${spec[@]:3}" "$@"
}

# helm_render <release fn>: the release's manifests on stdout.
helm_render() {
  local spec; mapfile -t spec < <("$1")
  local kv; kv=$(kubectl version -o json 2>/dev/null | jq -r '.serverVersion.gitVersion // empty' | sed 's/^v//; s/+.*//')
  helm template "${spec[0]}" "${spec[2]}" --namespace "${spec[1]}" --kube-version "${kv:-1.33.4}" "${spec[@]:3}"
}

# The tenants' Alloy pipeline: the simulated-tenants half of
# dev/mimir-k8s/alloy/config.alloy, used as is (read, not copied, so the
# two rigs cannot drift).
render_tenant_agent_config() {
  mkdir -p "$STATE"
  sed -n '/simulated tenants/,$p' "$REPO_ROOT/dev/mimir-k8s/alloy/config.alloy" > "$STATE/tenant-agent.alloy"
}
