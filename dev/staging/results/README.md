# Validation results

Each directory is one run of [`../validate`](../validate) against a known
environment, with the [`../workload`](../workload) generator providing the
ground truth. Each holds `report.md` (start there), `checks.json`, and
`raw/`, which has every promcost output, the ground truth, the events, and
the `promcost.yaml` used.

**Current evidence: the two runs on promcost @ `5a9df1f`.**

| run | Mimir write path | rule evaluation | promcost | checks | notes |
|---|---|---|---|---|---|
| [`2026-10-22-ingest`](2026-10-22-ingest/report.md) | ingest storage (Kafka, 3 zones) | remote (dedicated ruler query path) | `5a9df1f` (`launch/fixes`, with the #66 fix) | **87 / 87 pass** | 1h window contains an ingester rolling restart (±15% band); the 30m and 10m windows are clean (±1%). promcost equals Mimir's own gauge for every tenant. |
| [`2026-10-22-classic`](2026-10-22-classic/report.md) | classic (RF=3, 3 zones) | remote (dedicated ruler query path) | `5a9df1f` | **87 / 87 pass** | Replication factor 3, read from `cortex_distributor_replication_factor` and divided out: `steady` reads 30,022 against 30,022. The 1h window contains an ingester rolling restart; the 30m and 10m windows are clean. promcost equals Mimir's own gauge for every tenant. |
| [`2026-10-22-ingest-before-66-fix`](2026-10-22-ingest-before-66-fix/report.md) | ingest storage | remote | `b680c2a` | 79 / 83 | Superseded. Its 4 failures are #66 (pool 1 averaged a tenant over its own presence), and are the evidence for that bug. |
| [`2026-10-22-ingest-local-eval`](2026-10-22-ingest-local-eval/report.md) | ingest storage | **local** (`RULER_EVAL=local`) | `b680c2a` | 33 / 34 | Shows evaluation mode detected as `local` (#65), and tenants that never query reported as "not measured" in the query-path pool, never 0. The one failure (`churn`) is a Mimir ingester-restart transient, not promcost. The report explains it at the top. |

All runs: dev/staging (`launch/staging-env` @ `28332c5`) on one cloud
container (k3s in Docker, 4 vCPU), Mimir 3.2.0, the 8-tenant `short`
profile, and windows of at most 1h. Each report's "What this run does not
show" section lists what that leaves out.

Findings filed from these runs: **#66**, fixed in `5a9df1f` and verified
above, and **#61**, a documented known issue. The harness skips the checks
it would otherwise fail.
