# AGENTS.md

Guidance for AI coding agents (Claude Code, Codex, or anything else reading this) working in this repository. This is the single source of truth — `CLAUDE.md` is a short pointer at this file, not a second copy; edit this one.

## Working with multiple agent sessions

This repo regularly has more than one agent session working on it at once. A single checkout has exactly one `HEAD` and one working tree — two sessions sharing it fight over the same current branch and the same uncommitted files. This has genuinely caused lost time and confusing git state here before (concurrent sessions colliding on `git checkout`, one session's uncommitted work looking like a conflict to another).

**Before starting any new piece of work, create a dedicated git worktree rather than working directly in the shared checkout:**

```sh
git worktree add ../obscost-<short-topic> -b <branch-name> main   # off main, explicitly
```

Then treat `../obscost-<short-topic>` as your project root for that session — build, test, and commit from there, not from the shared `obscost/` checkout. Commits are visible across all worktrees immediately (same underlying `.git` object store), so there's no push/fetch dance needed to see another session's work land.

When done:

```sh
git worktree remove ../obscost-<short-topic>   # after merging/deleting the branch
```

`git worktree list` shows every active worktree and which branch it's on — check this before assuming the shared main checkout reflects only your own work.

**One exception:** `dev/mimir-local`'s running Docker containers are bound to whatever's on disk in the directory they were started from (currently the shared `obscost/` checkout, on `dev-mimir-local-rig`) — host ports aren't worktree-scoped, so don't start a second copy of that rig from a different worktree without remapping ports/project name first.

## Repository status

Milestone 1 (spec §8.1: parser + static tier `PC-S01..PC-S06`, golden-corpus tests, `promcost check --offline`) is implemented. Module path: `github.com/KorhanOzturk90/obscost`. Live tier (`PC-L0x`, the `Meter` interface), fleet tier (`PC-F0x`), and the `scan`/`explain`/`rewrite`/`pint-config` subcommands were never built, and are now frozen rather than planned — see "The frozen static-analysis side" below and [`docs/archive/v0-static-analyzer-design.md`](docs/archive/v0-static-analyzer-design.md).

The product direction has since expanded beyond the original static-analyzer spec — see `PRODUCT-DIRECTION.md` (the current thesis: observed workload attribution, not just PromQL linting) and `docs/runtime-telemetry-notes.md`. `promcost report` (`internal/cli/report.go`, flag `--telemetry-format ndjson|mimirlogs`, `--format md|json|html`) is the first slice of that: it joins observed `RuleExecution` telemetry (`internal/rule/execution.go`'s `RuleID`/`RuleExecution` — the five workload-stat fields are pointers, since a source not measuring a stat is a different fact from measuring it as zero) against loaded rule definitions in `internal/attribution`, producing ranked tenant/rule workload shares. Ranking is not hardcoded to sample count: `attribution.Aggregate` picks one report-wide `RankMetric` (query wall time > fetched bytes > fetched series > fetched chunks > samples processed > executions, whichever the telemetry source actually measured — see `docs/adr/0001-observed-workload-attribution-layer.md` decision 8) and every aggregate carries per-stat `Observed` flags so a metric that was never measured renders as "not measured", never a fabricated 0. Two `internal/telemetry.Source` implementations exist: `internal/telemetry/ndjson` (portable, self-contained, the default) and `internal/telemetry/mimirlogs` (a real parser for Mimir's own `-ruler.query-stats-enabled` logfmt output — read that package's doc comment before touching it, it documents two real, load-bearing limits of that log format: no rule name/group field at all, and no `samples_processed` stat, ever — which is exactly the case the adaptive `RankMetric` exists to handle, ranking by wall time instead). This whole layer is fully independent of `check`/`internal/analyzer` — neither reads the other's output yet (that join is a later milestone per `PRODUCT-DIRECTION.md`, tracked as GitHub issue #9).

A third workload source bypasses execution telemetry entirely: `internal/telemetry/mimirmetrics` queries Mimir's own `cortex_prometheus_rule_*` metrics over the PromQL API, so `promcost report --metrics-tenant <t>` runs against a Mimir URL with no log capture and no rule definitions at all. It is exact, cheap, and has history — but it stops at the rule group, because those metrics carry no rule name and no measure of data volume whatsoever. `attribution.Report.Granularity` records that limit so renderers describe an absent rule tier as a property of the source rather than as "no matched rule executions", and `attribution.AggregateObservations` is the constructor for this path (as opposed to `Aggregate`, which needs executions). Read `docs/adr/0002-where-workload-evidence-comes-from.md` before extending any of this — it sets out which of the three sources owns which question, and why ranking prefers fetched volume over wall time. Note `--metrics-tenant` names the tenant that *scraped* Mimir (typically a monitoring tenant), not a tenant being reported on; that is the most confusing thing about this source.

Rule *definitions* (as opposed to executions) come from `--dir` (a local rule-file checkout, via `internal/loader/dir` — same as `check`) or, when `--dir` is omitted, directly from Mimir's own ruler API (`internal/loader/rulerapi`, `GET /prometheus/api/v1/rules`, tenant-scoped) for an explicit `--tenant a,b,c` list — added specifically because each tenant's rules typically live in a separate repository promcost has no access to, and asking Mimir what it's actually evaluating is authoritative where a checkout might be stale. Tenants are explicit, not auto-discovered, by deliberate choice — see the doc comment on `newDefinitionsSource` in `internal/cli/report.go` for why.

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

Regenerate the report golden fixtures (md and html) after intentionally changing one of `internal/report`'s templates: `UPDATE_GOLDEN=1 go test ./internal/report/...`.

Try it against a fixture: `./bin/promcost check --dir testdata/corpus/positive/pcs01_heavy_subquery/rules --config testdata/corpus/positive/pcs01_heavy_subquery/promcost.yaml`.

## What promcost is

promcost attributes the resource use and cost of a shared, multi-tenant metrics platform (Grafana Mimir first) down to individual tenants — see `README.md` and `PRODUCT-DIRECTION.md` for the full thesis. It is explicitly **not** a linter: pint owns rule hygiene (annotations, `for:`, owners, naming), and `check`'s static analysis wraps pint rather than re-implementing checks it already has.

Since [ADR 0004](docs/adr/0004-finops-pivot-scope-and-sequencing.md) (accepted 2026-09-17) the product is **cost allocation for self-hosted Mimir**: what each tenant costs, in resources first and currency only when the operator supplies prices. Rule-level workload attribution (`report`, ADRs 0001–0002) is no longer the headline — it is the drill-down that explains the ruler and query pools of the cost model, and the only thing that can tell a tenant *what to change*.

**Read the ADRs before designing anything here.** They are the current source of truth for this layer, in order: [0001](docs/adr/0001-observed-workload-attribution-layer.md) (observed workload attribution), [0002](docs/adr/0002-where-workload-evidence-comes-from.md) (which telemetry source answers which question), 0003 (the pool→driver cost model — in review, PR #26), [0004](docs/adr/0004-finops-pivot-scope-and-sequencing.md) (the pivot's scope and build order).

### Scope now

In scope, in ADR 0004's build order: tenant showback in `promcost report` (resource-denominated, operator-supplied inventory) → daily rollups behind a pluggable sink → a change timeline from ruler-API snapshot diffs → budgets and digests. A per-cluster agent is adopted in principle but deliberately deferred until a real user needs history beyond Mimir's retention; anything it would report must be producible by a one-off `promcost report` run first.

Out of scope for now: writing limits back into Mimir (`max_global_series_per_user` as enforcement), a Kubernetes admission webhook, a UI, Backstage/Jira integrations, VictoriaMetrics/MetricsQL support. Currency figures are opt-in and must trace back to an operator-supplied price — never a built-in pricing database, never a fitted coefficient.

The v0 non-goals "no daemon/SaaS control plane" and "no ServiceMonitor/PodMonitor cost prediction" were reversed by ADR 0004 — they are sequencing decisions now, not prohibitions. "Do not lead with euros" survives, in the sharper form above.

### The frozen static-analysis side

`check` and `PC-S01`–`PC-S06` still build, still pass, and are still useful — but they are **frozen**: no new checks until the cost-allocation core exists. They come back for pull-request forecasting and for explaining why a rule is expensive. The original v0 design for that side (the `Meter` live tier, `scan`/`explain`/`rewrite`/`pint-config`, the `PC-L0x`/`PC-F0x` tiers, the rewrite engine) now lives in [`docs/archive/v0-static-analyzer-design.md`](docs/archive/v0-static-analyzer-design.md), alongside the full original spec ([`docs/archive/promcost-v0-spec.md`](docs/archive/promcost-v0-spec.md)). Reference material, not a to-do list.

Note that `internal/meter` is **not** dead code despite having no implementation: `analyzer.CheckContext` embeds the `Meter` interface, and ADR 0003's sub-tenant work may revive `SeriesCount` to attribute rule-created output series. Leave it alone.

