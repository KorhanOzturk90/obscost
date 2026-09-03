#!/usr/bin/env bash
# Fetches the compiled mimir-mixin (recording/alerting rules + dashboards)
# from grafana/mimir at MIXIN_REF and lays it out the way this rig's
# docker-compose.yml expects it. Not vendored into git on purpose — run
# this once before `docker compose up`, and re-run any time you bump
# MIXIN_REF or MIMIR_IMAGE_TAG in docker-compose.yml (keep them in sync:
# the mixin's recording rules reference metric names that can drift
# between Mimir releases).
set -euo pipefail

# Defaults to the same tag pinned for the mimir image in docker-compose.yml.
MIXIN_REF="${MIXIN_REF:-mimir-3.2.0}"

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT="$DIR/mixin"
RAW="https://raw.githubusercontent.com/grafana/mimir/$MIXIN_REF/operations/mimir-mixin-compiled"
API="https://api.github.com/repos/grafana/mimir/contents/operations/mimir-mixin-compiled/dashboards?ref=$MIXIN_REF"

echo "Fetching mimir-mixin-compiled @ $MIXIN_REF into $OUT"
rm -rf "$OUT"
mkdir -p "$OUT/rules/anonymous" "$OUT/dashboards"

curl -fsSL "$RAW/rules.yaml" -o "$OUT/rules/anonymous/recording-rules.yaml"
curl -fsSL "$RAW/alerts.yaml" -o "$OUT/rules/anonymous/alerts.yaml"

# The dashboards/ directory has to be listed via the GitHub API (it's not a
# single file we can curl directly); download each *.json it contains.
names=$(curl -fsSL "$API" | grep -o '"name": *"[^"]*\.json"' | sed -E 's/"name": *"([^"]*)"/\1/')
if [ -z "$names" ]; then
  echo "error: no dashboard JSON files found at $API — mixin-compiled layout may have changed" >&2
  exit 1
fi
count=0
while IFS= read -r name; do
  curl -fsSL "$RAW/dashboards/$name" -o "$OUT/dashboards/$name"
  count=$((count + 1))
done <<< "$names"

echo "Fetched $(wc -l < "$OUT/rules/anonymous/recording-rules.yaml") lines of recording rules,"
echo "        $(wc -l < "$OUT/rules/anonymous/alerts.yaml") lines of alerting rules,"
echo "        $count dashboards."
