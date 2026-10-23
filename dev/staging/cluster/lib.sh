# Shared by cluster/*.sh. Images reach the local cluster from the host's
# Docker, never from the cluster's own containerd: inside this cloud
# container, containerd cannot pull through the egress proxy (it does not
# trust the proxy's CA), and Docker Hub rate-limits direct pulls. So every
# Docker Hub image is pulled through mirror.gcr.io (Google's pull-through
# cache of Docker Hub), falling back to docker.io, then tagged with its
# canonical name and imported into the node's containerd.

NODE=${CLUSTER:-staging}-k3s

log() { printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }

# canonical <ref> -> docker.io/<ns>/<repo>:<tag> for Docker Hub images,
# the ref unchanged for any other registry.
canonical() {
  local ref=$1
  [[ $ref == *:* || $ref == *@* ]] || ref="$ref:latest"
  local first=${ref%%/*}
  if [[ $ref != */* ]]; then
    echo "docker.io/library/$ref"
  elif [[ $first == *.* || $first == *:* || $first == localhost ]]; then
    echo "$ref"
  else
    echo "docker.io/$ref"
  fi
}

retry() {
  local n
  for n in 1 2 3 4; do
    "$@" >/dev/null 2>&1 && return 0
    sleep $((n * 2))
  done
  return 1
}

# host_pull <canonical ref>: make the image present in the host's Docker
# under its canonical name.
host_pull() {
  local ref=$1
  docker image inspect "$ref" >/dev/null 2>&1 && return 0
  if [[ $ref == docker.io/* ]]; then
    local path=${ref#docker.io/}
    if retry docker pull "mirror.gcr.io/$path"; then
      docker tag "mirror.gcr.io/$path" "$ref"
      return 0
    fi
    log "mirror.gcr.io has no $path; trying docker.io directly"
  fi
  retry docker pull "$ref" || { log "FAILED to pull $ref"; return 1; }
}

node_has() {
  docker exec "$NODE" ctr -n k8s.io images ls -q 2>/dev/null | grep -Fxq "$1"
}

# preload <ref>...: pull on the host, import into the node. Idempotent.
preload() {
  local ref c failed=0 todo=()
  for ref in "$@"; do
    c=$(canonical "$ref")
    node_has "$c" && continue
    host_pull "$c" || { failed=1; continue; }
    todo+=("$c")
  done
  if ((${#todo[@]})); then
    log "importing ${#todo[@]} image(s) into $NODE: ${todo[*]}"
    docker save --platform linux/amd64 "${todo[@]}" | docker exec -i "$NODE" ctr -n k8s.io images import - >/dev/null
  fi
  return $failed
}
