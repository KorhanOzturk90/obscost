#!/usr/bin/env bash
# Bring dev/mimir-local up with this directory's overlay (no MinIO, writable
# ruler store), and load the mimir-mixin's rules into `infra` through the
# ruler API. `./up.sh down` removes it, volumes included.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RIG="$HERE/../../../mimir-local"
RUN="$HERE/../.run"
PROJECT=promcost-validation
MIMIR=http://localhost:8080
compose() { docker compose -p "$PROJECT" -f "$RIG/docker-compose.yml" -f "$HERE/compose.override.yml" "$@"; }

if [ "${1:-}" = down ]; then
  compose down -v
  exit 0
fi

docker info >/dev/null 2>&1 || { echo "docker daemon not reachable (start it: dockerd > /tmp/dockerd.log 2>&1 &)" >&2; exit 1; }

# Docker Hub rate-limits direct pulls here; mirror.gcr.io serves the same images.
for img in grafana/mimir:3.2.0 grafana/alloy:v1.19.2 grafana/mimirtool:3.2.0; do
  docker image inspect "$img" >/dev/null 2>&1 || { docker pull -q "mirror.gcr.io/$img" && docker tag "mirror.gcr.io/$img" "$img"; }
done

compose up -d mimir alloy
echo "waiting for $MIMIR/ready …"
until [ "$(curl -s -o /dev/null -w '%{http_code}' "$MIMIR/ready")" = 200 ]; do sleep 3; done

mkdir -p "$RUN/mixin"
for f in rules alerts; do
  [ -s "$RUN/mixin/$f.yaml" ] || curl -fsSL "https://raw.githubusercontent.com/grafana/mimir/mimir-3.2.0/operations/mimir-mixin-compiled/$f.yaml" -o "$RUN/mixin/$f.yaml"
done
docker run --rm --network host -v "$(cd "$RUN/mixin" && pwd)":/rules:ro grafana/mimirtool:3.2.0 \
  rules load --address="$MIMIR" --id=infra /rules/rules.yaml /rules/alerts.yaml
echo "mimir-local up at $MIMIR; mixin rules loaded into infra"
