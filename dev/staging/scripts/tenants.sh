#!/usr/bin/env bash
# The tenants to cost, comma-separated: every tenant dev/mimir-k8s's Alloy
# config writes to - the interim tenants and `monitoring`, which is a
# tenant on the cluster like any other (its series cost ingester memory
# too).
set -euo pipefail
source "$(dirname "$0")/lib.sh"
grep -o 'X-Scope-OrgID" = "[^"]*"' "$REPO_ROOT/dev/mimir-k8s/alloy/config.alloy" | cut -d'"' -f3 | sort -u | paste -sd, -
