#!/usr/bin/env bash
# The dev/mimir-k8s simulated tenants (avalanche generators), read from
# that rig unchanged except for the image: quay.io is denied in the cloud
# container, and avalanche publishes the same image to Docker Hub.
set -euo pipefail
sed 's#quay.io/prometheuscommunity/avalanche#docker.io/prometheuscommunity/avalanche#' \
  "$(dirname "$0")/../../mimir-k8s/tenants/workloads.yaml"
