# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository status

Milestone 1 (spec §8.1: parser + static tier `PC-S01..PC-S06`, golden-corpus tests, `promcost check --offline`) is implemented. Module path: `github.com/KorhanOzturk90/obscost`. Live tier (`PC-L0x`, the `Meter` interface), fleet tier (`PC-F0x`), and the `scan`/`explain`/`rewrite`/`pint-config` subcommands are not yet built — see `internal/meter` and the doc comment on `internal/cli/check.go` for the extension points.

The product direction has since expanded beyond the original static-analyzer spec — see `PRODUCT-DIRECTION.md` (the current thesis: observed workload attribution, not just PromQL linting) and `docs/runtime-telemetry-notes.md`. `promcost report` (`internal/cli/report.go`, flag `--telemetry-format ndjson|mimirlogs`) is the first slice of that: it joins observed `RuleExecution` telemetry (`internal/rule/execution.go`'s `RuleID`/`RuleExecution` — the five workload-stat fields are pointers, since a source not measuring a stat is a different fact from measuring it as zero) against loaded rule definitions in `internal/attribution`, producing ranked tenant/rule workload shares. Two `internal/telemetry.Source` implementations exist: `internal/telemetry/ndjson` (portable, self-contained, the default) and `internal/telemetry/mimirlogs` (a real parser for Mimir's own `-ruler.query-stats-enabled` logfmt output — read that package's doc comment before touching it, it documents two real, load-bearing limits of that log format: no rule name/group field at all, and no `samples_processed` stat, ever). This whole layer is fully independent of `check`/`internal/analyzer` — neither reads the other's output yet (that join is a later milestone per `PRODUCT-DIRECTION.md`, tracked as GitHub issue #9).

### Commands

```
go build ./...              # build all packages
go build -o bin/promcost ./cmd/promcost   # or: make build
go test ./...                # run all tests, including the golden-corpus harness
go vet ./...
golangci-lint run ./...      # or: make lint
go mod tidy                  # or: make tidy
```

Run a single test: `go test ./internal/analyzer/checks/ -run TestPCS01_Positive_WarnJustAboveThreshold -v`.

Regenerate the md-report golden fixture after intentionally changing `internal/report`'s template: `UPDATE_GOLDEN=1 go test ./internal/report/...`.

Try it against a fixture: `./bin/promcost check --dir testdata/corpus/positive/pcs01_heavy_subquery/rules --config testdata/corpus/positive/pcs01_heavy_subquery/promcost.yaml`.

## What promcost is

Cost attribution and load prediction for recording/alerting rules in multi-tenant Prometheus-compatible stacks (Mimir first). It is explicitly **not** a linter — pint owns rule hygiene (annotations, `for:`, owners, naming); promcost wraps pint and never re-implements a check pint already has (§6). Non-goals for v0: no UI, no daemon/SaaS control plane, no ServiceMonitor/PodMonitor cost prediction, no VictoriaMetrics/MetricsQL support.

## Core architecture (§1)

```
rule sources (cluster CRDs, ruler API, dir) ──► loader ──► analyzer ──► reporter ──► md/json/html + exit code
                                                                │
                                                              Meter (only I/O)
                                                                │
                                                          backend client (instant queries only, tenant-scoped)
```

Non-negotiable design rules — preserve these when implementing:

1. **The analyzer is pure.** It operates on an annotated PromQL AST (upstream `prometheus/promql/parser`) plus injected interfaces. All network I/O goes through a single `Meter` interface (§5) so every estimation function is unit-testable with a `FakeMeter`. Never let analyzer code reach for a network client directly.
2. **The engine never executes a tenant's expression.** Only cheap metering primitives are allowed: counts, sampled `count_over_time`, series API. The sole exception is the opt-in truncated dry-run (`PC-L06`), off by default and documented as calibration, not gating.
3. **Every finding carries:** check ID, severity, tenant, source location (file/CRD/ruler group), the numbers behind it, and where possible a remediation diff.

### The `Meter` interface (§5)

```go
type Meter interface {
    SeriesCount(tenant, selector string) (int64, error)               // count(last_over_time(sel[W]))
    GroupedCount(tenant, selector string, by []string) (int64, error) // count(count by(...)(last_over_time(sel[W])))
    SampleSeries(tenant, selector string, k int) ([]Labels, error)    // series API w/ limit, fallback topk(k, sel)
    RangeSampleCount(tenant string, exact Labels, window time.Duration) (int64, error) // count_over_time
}
```

`W` = `presence_window` (config, default 1h) — aggregations take instant vectors, so range vectors pass through `last_over_time` first (`count(sel[W])` is not valid PromQL). The window exists specifically so cold series (cronjobs, batch jobs) aren't missed by a bare 5m instant lookback — don't shrink it without preserving that guarantee.

Two distinct cost quantities are tracked per AST node (§5.3), and they must stay distinct because they stress different backend components:

* **fetched_samples** — data read from ingesters/store-gateways (I/O pressure)
* **processed_samples** — sample-evaluations in the querier (CPU pressure)

`store_fraction(R)` (§5.4) splits fetched cost between ingester-memory reads (inside `query_store_after`) and store-gateway/object-store reads (beyond it) — a `[30d]` rule pays store prices on ~98% of reads, a `[5m]` rule on none. Get this split right; it's the main cost-model lever.

## CLI surface (§2)

Five subcommands, each with a distinct contract — don't blur them:

| mode | input | network | zero-adoption? | purpose |
|---|---|---|---|---|
| `scan` | live cluster CRDs or ruler API | yes (metering) | yes — platform team runs it alone | ranked fleet report; the demo; the wedge |
| `check` | rule files in a repo, diff-aware | optional | needs repo integration | PR gate; wraps changed rules only |
| `explain` | one rule | yes | yes | the artifact you paste to a tenant |
| `rewrite` | one rule/group | yes | yes | closes the "tenants never implement suggestions" loop |
| `pint-config` | tenancy discovery | no | — | renders per-tenant pint HCL |

`scan` ships first. `check` is a thin wrapper over the same analyzer with git-diff selection (steal pint's `ci` semantics: only analyze rules changed vs `--base`).

Exit codes are meaningful and must stay stable: `0` clean · `1` config/usage error · `2` findings at or above `--fail-on` · `3` backend unreachable (check mode degrades to static-only with a warning rather than failing, unless `--strict`).

## Check catalogue (§4)

Checks are organized into three tiers by cost/network requirement — new checks must be placed in the correct tier and follow the `PC-<tier><NN>` ID scheme:

* **Static tier (`PC-S0x`)** — no network, always run. E.g. `PC-S01` subquery-density (`steps = ceil(R/s)`, flag on threshold), `PC-S05` ruler-count-limits (works offline from the limits file alone — "the most trivially checkable failure that nothing checks today").
* **Live tier (`PC-L0x`)** — cheap metering only, instant queries, tenant-scoped. E.g. `PC-L02` query-limit-headroom (names the exact Mimir error the rule will produce, like `err-mimir-max-series-per-query`), `PC-L06` truncated-dry-run (opt-in, off by default, requires cache-busting per the cache guard below).
* **Fleet tier (`PC-F0x`)** — scan mode only. E.g. `PC-F01` top-expensive-rules (the demo report), `PC-F02` expensive-and-unused (joins with `mimirtool analyze` output via `promcost analyze-join`).

**Cache guard for `PC-L06`:** must use unaligned, current-time windows and send `Cache-Control: no-store` — a query served from the query-frontend results cache returns without engine stats and would silently poison calibration.

## Rewrite engine composability guard (§7)

The rewrite engine only auto-applies chained-window rewrites for aggregations that compose exactly: `sum`, `min`, `max`, `count`; `avg` via a sum/count pair. It must **refuse** `quantile_over_time` and quantile-over-window patterns (a quantile of quantiles is not a quantile) unless it can rewrite via histogram-bucket sums — refuse loudly rather than emit a silently-wrong SLO rewrite. This is a hard product-credibility constraint, not a nice-to-have.

Rewrite output must also state the **warm-up gap**: a freshly deployed chain has no history, so results are wrong for up to the full window duration. The diff must ship the fix pre-filled (`promtool tsdb create-blocks-from rules` to backfill, then `mimirtool backfill` to upload per tenant).

## Configuration (`promcost.yaml`, §3)

Key structural point: the `tenancy` block is what pint structurally lacks — dynamic namespace/annotation → `X-Scope-OrgID` resolution, so every metering query and existence check runs against the *right* tenant's view. `limits.sources` are tried in order with first-success-wins, and resolved endpoints (`user_limits_endpoint`) are preferred over static files because a file can lie about what's actually loaded. Limit key names vary by Mimir version — the loader must tolerate both CLI-flag-style and YAML-style names.

Cost model constants (`eur_per_million_active_series_month`, etc.) are explicitly placeholders — real values come from calibration against a design partner's actual infra bill (§8.6), not from invention.

## Testing strategy (§5.5, §8)

Once code exists, the intended testing approach is:

* **Golden-corpus unit tests**: `(rule yaml, FakeMeter fixture) → expected findings json`. Every check and formula gets exercised with zero infrastructure via `FakeMeter`.
* Every estimate in a report must carry its assumption trail (e.g. `N=count(sel) @ scan time; sps=median of 5`) — error-bar honesty is a design requirement, not a nice-to-have.
* L06 calibration tests assert estimates within ±30% of `?stats=all` ground truth — a stated design contract.
* Test corpus sources (§8.1): kubernetes-mixin, node-exporter mixin, kube-prometheus rules, mimir-mixin (the tool's first target is itself), awesome-prometheus-alerts, Sloth/Pyrra examples (negative control — should produce zero `PC-S01`/`PC-S02` findings).
* Multi-tenant integration testing requires a simulated rig (§8.3) since no public multi-tenant Mimir exists — Grafana's play-with-Mimir compose setup with fake per-tenant Alloy writers, each exercising a distinct misbehavior scenario (baseline/cardinality/churn/heavy-rules/limit-hugger).
