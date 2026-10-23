#!/usr/bin/env bash
# Check (and where possible install) the client tools: kubectl, helm, jq.
# helm's own download host (get.helm.sh) is denied in the cloud container,
# so helm falls back to `go install` through the Go module proxy.
set -euo pipefail
HELM_VERSION=${HELM_VERSION:-v3.19.0}
KUBECTL_VERSION=${KUBECTL_VERSION:-v1.33.4}
BIN=${TOOLS_BIN:-/usr/local/bin}

if ! command -v kubectl >/dev/null; then
  echo "installing kubectl $KUBECTL_VERSION into $BIN" >&2
  curl -fsSL -o "$BIN/kubectl" "https://dl.k8s.io/release/$KUBECTL_VERSION/bin/linux/amd64/kubectl"
  chmod +x "$BIN/kubectl"
fi

if ! command -v helm >/dev/null; then
  echo "installing helm $HELM_VERSION into $BIN" >&2
  tmp=$(mktemp -d)
  if curl -fsSL "https://get.helm.sh/helm-$HELM_VERSION-linux-amd64.tar.gz" | tar xz -C "$tmp" 2>/dev/null; then
    install "$tmp/linux-amd64/helm" "$BIN/helm"
  else
    echo "get.helm.sh unreachable; building helm with go install (a few minutes)" >&2
    (cd "$tmp" && GOBIN=$BIN go install "helm.sh/helm/v3/cmd/helm@$HELM_VERSION")
  fi
  rm -rf "$tmp"
fi

for t in jq curl; do
  command -v $t >/dev/null || { echo "missing: $t" >&2; exit 1; }
done
