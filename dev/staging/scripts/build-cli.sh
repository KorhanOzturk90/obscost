#!/usr/bin/env bash
# Build the product CLI from a git ref into $CLI, without touching the
# working tree: a throwaway worktree, removed afterwards.
#   scripts/build-cli.sh origin/pools-6-7-cost
set -euo pipefail
source "$(dirname "$0")/lib.sh"
ref=${1:-HEAD}
: "${CLI:=$REPO_ROOT/bin/promcost}" "${CLI_NAME:=$(basename "$CLI")}"
tree=$(mktemp -d)
git -C "$REPO_ROOT" fetch -q origin 2>/dev/null || true
git -C "$REPO_ROOT" worktree add -q --detach "$tree" "$ref"
trap 'git -C "$REPO_ROOT" worktree remove --force "$tree"' EXIT
mkdir -p "$(dirname "$CLI")"
(cd "$tree" && go build -o "$CLI" "./cmd/$CLI_NAME")
log "built $CLI from $ref ($(git -C "$tree" rev-parse --short HEAD))"
