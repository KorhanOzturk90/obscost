# promcost validation — `staging-ingest`, profile `short`

> **Correction (added after the run).** The one failure, `churn` at 5,611 against 5,811–6,211, is not promcost's. `make mimir RULER_EVAL=local` rolled every ingester (03:27:45–03:30:39Z, from `process_start_time_seconds`), so this "clean" window sat inside the restart transient: a restarted ingester re-learns only series that receive new samples, and `churn`'s already-replaced generations were not re-counted. Mimir's own gauge read the same as promcost. The model now accounts for ingester restarts (commit 1030dd7); this report was produced before that and is left as it ran.

**33 passed, 1 failed, 0 skipped** across 1 window(s) (10m). Generated 2026-10-22 03:48 UTC.

Failures, grouped by cause:

- **pool 1: `churn` active series (after the replication divisor)** — 1 check(s), window(s) 10m.

## What this was run against

| | |
|---|---|
| environment | `staging-ingest` |
| Mimir | `http://localhost:8091`, Mimir's own metrics in tenant `monitoring` |
| write path | expected `ingest storage`, detected `ingest storage` |
| replication factor | `1` (from `ingest storage: one owner per partition, zone replicas collapsed with max, so there is nothing to divide`) |
| rule evaluation | expected `local` |
| promcost | `/home/user/promcost-bin/promcost (no version command)` |
| workload | `loadgen`, profile `short`, started 2026-10-22 02:05 UTC |
| operational events | ingester-rolling-restart (2026-10-22T01:28:19Z → 2026-10-22T01:32:41Z); ingester-rolling-restart (2026-10-22T02:38:10Z → 2026-10-22T02:42:36Z); query-path restart after switching the ruler to local evaluation (query-frontend and queriers; the switch had already removed the ruler's own query path) (2026-10-22T03:29:41Z → 2026-10-22T03:31:26Z) |

Every tenant below except the metrics tenant is synthetic, and its ground truth is what the generator *actually* delivered: when its samples started and stopped arriving, how many were accepted, when its rules were pushed. See [`dev/staging/workload`](../../workload/README.md). The expected pool-1 figures apply Mimir's own semantics to those facts (the 20m active-series idle timeout, the 1m gauge update); [`model.py`](../../validate/model.py) states each one where it is used.

| tenant | shape | live series | samples/s | queries/min | rules (rec + alert) | record: series |
|---|---|---:|---:|---:|---:|---:|
| `cardinal` | high cardinality, low sample rate | 20,000 | 333.3 | 0 | 1 + 0 | 10 |
| `churn` | churny (every series replaced each 10m, like a deploy) | 2,000 (replaced every 10m) | 133.3 | 0 | 2 + 0 | 11 |
| `dashboards` | query-heavy (dashboard range queries through the query-frontend) | 4,000 | 266.7 | 36 | 2 + 2 | 11 |
| `ghost` | goes silent mid-run (rules stay loaded, as when a team stops sending but nobody offboards it); silent 45m in | 3,000 | 200.0 | 0 | 2 + 0 | 11 |
| `latecomer` | starts mid-run; starts 30m in | 2,000 | 133.3 | 1 | 2 + 1 | 11 |
| `rules` | rule-heavy (mixin-sized corpus) | 3,000 | 200.0 | 0 | 120 + 120 | 660 |
| `steady` | large steady ingestion | 30,000 | 2,000.0 | 0.2 | 4 + 2 | 22 |
| `tiny` | tiny | 20 | 0.7 | 0 | 0 + 1 | 0 |

Delivery: every sample and query the generator sent was accepted.

## Window 10m, ending 2026-10-22 03:48 UTC

| result | check | expected | measured | tolerance |
|---|---|---|---|---|
| ✅ pass | pool 1 (ingester memory): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 6 (query path): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 7 (ruler CPU): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 1 (ingester memory): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 1 unmeasured: ghost | exact |
| ✅ pass | pool 6 (query path): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 5 unmeasured: cardinal, churn, ghost, rules, tiny | exact |
| ✅ pass | pool 7 (ruler CPU): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 0 unmeasured: none | exact |
| ✅ pass | pool 1: `cardinal` active series (after the replication divisor) | 20,010–20,010 | 20,010 | band [20,010, 20,010] ±1% |
| ❌ fail | pool 1: `churn` active series (after the replication divisor) | 5,811–6,211 | 5,611 | band [5,811, 6,211] ±1% |
| ✅ pass | pool 1: `dashboards` active series (after the replication divisor) | 4,011–4,011 | 4,011 | band [4,011, 4,011] ±1% |
| ✅ pass | pool 1: `ghost` active series | no active series in this window (absent or 0) | not measured | exact |
| ✅ pass | pool 1: `latecomer` active series (after the replication divisor) | 2,011–2,011 | 2,011 | band [2,011, 2,011] ±1% |
| ✅ pass | pool 1: `rules` active series (after the replication divisor) | 3,660–3,660 | 3,660 | band [3,660, 3,660] ±1% |
| ✅ pass | pool 1: `steady` active series (after the replication divisor) | 30,022–30,022 | 30,022 | band [30,022, 30,022] ±1% |
| ✅ pass | pool 1: `tiny` active series (after the replication divisor) | 20.00–20.00 | 20.00 | band [20.00, 20.00] ±1% |
| ✅ pass | pool 1: tenant rank order matches the workload shapes | steady > cardinal > churn > dashboards > rules > latecomer > tiny | steady > cardinal > churn > dashboards > rules > latecomer > tiny | pairs whose expected figures are within 10% of each other are not ranked |
| ✅ pass | pool 6 (query path): `dashboards` dominates | `dashboards` first, with at least 50% | dashboards 99.8%; latecomer 0.1%; steady 0.1% | rank 1 and ≥50% |
| ✅ pass | pool 7 (ruler CPU): `rules` dominates | `rules` first, with at least 50% | rules 88.5%; cardinal 3.6%; steady 3.2%; churn 1.7% | rank 1 and ≥50% |
| ✅ pass | ruler evaluation mode detected correctly | local | pool 6 (query path): local, pool 7 (ruler CPU): local | exact |
| ✅ pass | pool 6: `cardinal` (never queries) is not measured | not measured | not measured | exact |
| ✅ pass | pool 6: `churn` (never queries) is not measured | not measured | not measured | exact |
| ✅ pass | pool 6: `ghost` (never queries) is not measured | not measured | not measured | exact |
| ✅ pass | pool 6: `rules` (never queries) is not measured | not measured | not measured | exact |
| ✅ pass | pool 6: `tiny` (never queries) is not measured | not measured | not measured | exact |
| ✅ pass | `ghost` after it went silent: 0 or not measured | 0 active series, or not measured | not measured | exact |
| ✅ pass | rule outputs: `cardinal` record: series | 10 series over 1 record: names (present) | 10 series (1 output metrics) | exact |
| ✅ pass | rule outputs: `churn` record: series | 11 series over 2 record: names (present) | 11 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `dashboards` record: series | 11 series over 2 record: names (present) | 11 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `ghost` record: series | 0 series over 2 record: names (absent) | 0 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `latecomer` record: series | 11 series over 2 record: names (present) | 11 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `rules` record: series | 660 series over 120 record: names (present) | 660 series (120 output metrics) | exact |
| ✅ pass | rule outputs: `steady` record: series | 22 series over 4 record: names (present) | 22 series (4 output metrics) | exact |
| ✅ pass | rule outputs: `tiny` record: series | 0 series over 0 record: names (absent) | 0 series (0 output metrics) | exact |
| ✅ pass | write path detected correctly | ingest storage | ingest storage | exact |

<details><summary>Pool 1 per tenant: expected vs measured</summary>

| tenant | expected (time-weighted band) | promcost | points present (Mimir / model) |
|---|---:|---:|---:|
| `cardinal` | 20,010 – 20,010 | 20,010 | 10 / 10 of 10 |
| `churn` | 5,811 – 6,211 | 5,611 | 10 / 10 of 10 |
| `dashboards` | 4,011 – 4,011 | 4,011 | 10 / 10 of 10 |
| `ghost` | 0.00 – 0.00 | not measured | 0 / 0 of 10 |
| `latecomer` | 2,011 – 2,011 | 2,011 | 10 / 10 of 10 |
| `monitoring` | not controlled | 124,972 | 10 / — of 10 |
| `rules` | 3,660 – 3,660 | 3,660 | 10 / 10 of 10 |
| `steady` | 30,022 – 30,022 | 30,022 | 10 / 10 of 10 |
| `tiny` | 20.00 – 20.00 | 20.00 | 10 / 10 of 10 |

</details>

<details><summary>pool 6 (query path): shares (chunk bytes fetched)</summary>

| tenant | driver | share |
|---|---:|---:|
| `dashboards` | 1,793,653,369 | 99.81% |
| `latecomer` | 1,917,516 | 0.11% |
| `steady` | 1,538,947 | 0.09% |
| `cardinal` | not measured | not measured |
| `churn` | not measured | not measured |
| `ghost` | not measured | not measured |
| `rules` | not measured | not measured |
| `tiny` | not measured | not measured |

</details>

<details><summary>pool 7 (ruler CPU): shares (rule-evaluation seconds)</summary>

| tenant | driver | share |
|---|---:|---:|
| `rules` | 70.71 | 88.48% |
| `cardinal` | 2.90 | 3.62% |
| `steady` | 2.53 | 3.16% |
| `churn` | 1.37 | 1.71% |
| `latecomer` | 1.31 | 1.64% |
| `dashboards` | 1.04 | 1.31% |
| `ghost` | 0.03 | 0.04% |
| `tiny` | 0.03 | 0.03% |

</details>

<details><summary>Why each tolerance, and details</summary>

- **pool 1 (ingester memory): shares sum to 100%** — shares are computed exactly from one set of drivers; only float rounding may separate them from 100%
- **pool 6 (query path): shares sum to 100%** — shares are computed exactly from one set of drivers; only float rounding may separate them from 100%
- **pool 7 (ruler CPU): shares sum to 100%** — shares are computed exactly from one set of drivers; only float rounding may separate them from 100%
- **pool 1 (ingester memory): unmeasured tenants are "not measured", never 0** — ADR 0003 decision 5: absence is a different fact from zero
- **pool 6 (query path): unmeasured tenants are "not measured", never 0** — ADR 0003 decision 5: absence is a different fact from zero
- **pool 7 (ruler CPU): unmeasured tenants are "not measured", never 0** — ADR 0003 decision 5: absence is a different fact from zero
- **pool 1: `cardinal` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 10; points this tenant was present (model): 10; (Mimir): 10
- **pool 1: `churn` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 10; points this tenant was present (model): 10; (Mimir): 10
- **pool 1: `dashboards` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 10; points this tenant was present (model): 10; (Mimir): 10
- **pool 1: `ghost` active series** — the tenant wrote no sample within the idle timeout of any point
- **pool 1: `latecomer` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 10; points this tenant was present (model): 10; (Mimir): 10
- **pool 1: `rules` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 10; points this tenant was present (model): 10; (Mimir): 10
- **pool 1: `steady` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 10; points this tenant was present (model): 10; (Mimir): 10
- **pool 1: `tiny` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 10; points this tenant was present (model): 10; (Mimir): 10
- **pool 1: tenant rank order matches the workload shapes** — a near-tie can legitimately flip on gauge lag; everything else is a difference of shape
- **pool 6 (query path): `dashboards` dominates** — the workload makes `dashboards` this pool's driver by a wide margin by construction; a simple majority is a weaker claim than the margin built in, so it should hold under noise
  - driver: chunk bytes fetched (cortex_query_fetched_chunk_bytes_total)
- **pool 7 (ruler CPU): `rules` dominates** — the workload makes `rules` this pool's driver by a wide margin by construction; a simple majority is a weaker claim than the margin built in, so it should hold under noise
  - driver: rule-evaluation seconds (cortex_prometheus_rule_evaluation_duration_seconds_sum)
- **ruler evaluation mode detected correctly** — the environment's ruler configuration is known
- **pool 6: `cardinal` (never queries) is not measured** — the tenant issues no queries and rules are evaluated locally
- **pool 6: `churn` (never queries) is not measured** — the tenant issues no queries and rules are evaluated locally
- **pool 6: `ghost` (never queries) is not measured** — the tenant issues no queries and rules are evaluated locally
- **pool 6: `rules` (never queries) is not measured** — the tenant issues no queries and rules are evaluated locally
- **pool 6: `tiny` (never queries) is not measured** — the tenant issues no queries and rules are evaluated locally
- **`ghost` after it went silent: 0 or not measured** — its last sample, and its recording rules' last output, are more than the 20m idle timeout before the window opens
  - last sample 45m into the run
- **rule outputs: `cardinal` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `churn` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `dashboards` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `ghost` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `latecomer` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `rules` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `steady` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `tiny` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **write path detected correctly** — the environment's architecture is known

</details>

## What this run does not show

- **Resource calibration.** It checks that promcost reads Mimir's per-tenant drivers correctly. Whether active series are *the* driver of ingester memory is ADR 0003 decision 6's question, and is not answered here.
- **Pools 6 and 7 against absolute truth.** Fetched chunk bytes and rule-evaluation seconds depend on chunk encoding, caches and CPU contention, so only their ranking is checked, not their values.
- **Anything about tenants it did not generate.** The metrics tenant is real and uncontrolled: it is in every share and every sum, but no figure for it is checked.

Raw outputs (every promcost run as JSON and markdown, the ground truth, every check as JSON) are in [`raw/`](raw/) and [`checks.json`](checks.json).
