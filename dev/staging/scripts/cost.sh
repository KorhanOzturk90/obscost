#!/usr/bin/env bash
# Run the CLI's `cost` command against this env: all pools, with
# --rule-outputs, as md and as json. Output goes to stdout and to
# .state/cost/<timestamp>.{md,json}.
#   scripts/cost.sh <tenants> <since>
set -euo pipefail
source "$(dirname "$0")/lib.sh"
tenants=$1 since=${2:-1h}
: "${CLI:=$REPO_ROOT/bin/promcost}"
[[ -x $CLI ]] || { echo "no CLI at $CLI - run 'make cli' first" >&2; exit 1; }
out=$STATE/cost/$(date -u +%Y%m%dT%H%M%SZ)
mkdir -p "$(dirname "$out")"
args=(cost --metrics-tenant monitoring --config "$STATE/promcost.yaml"
  --tenant "$tenants" --since "$since" --rule-outputs)
log "$(basename "$CLI") ${args[*]}"
"$CLI" "${args[@]}" --format json > "$out.json"
"$CLI" "${args[@]}" --format md | tee "$out.md"
log "saved $out.md and $out.json"
