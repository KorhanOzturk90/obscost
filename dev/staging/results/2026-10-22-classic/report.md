# promcost validation — `staging-classic`, profile `short`

**87 passed, 0 failed, 0 skipped** across 3 window(s) (1h, 30m, 10m). Generated 2026-10-22 08:52 UTC.

No check failed.

## What this was run against

| | |
|---|---|
| environment | `staging-classic` |
| Mimir | `http://localhost:8091`, Mimir's own metrics in tenant `monitoring` |
| write path | expected `classic`, detected `classic` |
| replication factor | `3` (from `max(max_over_time(cortex_distributor_replication_factor[1h]))`) |
| rule evaluation | expected `remote` |
| promcost | `promcost @ 5a9df1f (launch/fixes)` |
| workload | `loadgen`, profile `short`, started 2026-10-22 07:25 UTC |
| operational events | ingester-rolling-restart (2026-10-22T07:58:24Z → 2026-10-22T08:02:36Z) |

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

## Window 1h, ending 2026-10-22 08:52 UTC

> overlaps ingester-rolling-restart: pool 1 is checked against a band widened to ±15%

| result | check | expected | measured | tolerance |
|---|---|---|---|---|
| ✅ pass | pool 1 (ingester memory): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 6 (query path): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 7 (ruler CPU): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 1 (ingester memory): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 0 unmeasured: none | exact |
| ✅ pass | pool 6 (query path): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 0 unmeasured: none | exact |
| ✅ pass | pool 7 (ruler CPU): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 0 unmeasured: none | exact |
| ✅ pass | pool 1: `cardinal` active series (after the replication divisor) | 20,010–20,010 | 19,676 | band [20,010, 20,010] ±15% |
| ✅ pass | pool 1: `churn` active series (after the replication divisor) | 4,944–6,211 | 5,122 | band [4,944, 6,211] ±15% |
| ✅ pass | pool 1: `dashboards` active series (after the replication divisor) | 4,011–4,011 | 3,944 | band [4,011, 4,011] ±15% |
| ✅ pass | pool 1: `ghost` active series (after the replication divisor) | 1,908–1,958 | 1,875 | band [1,908, 1,958] ±15% |
| ✅ pass | pool 1: `latecomer` active series (after the replication divisor) | 1,877–1,910 | 1,854 | band [1,877, 1,910] ±15% |
| ✅ pass | pool 1: `rules` active series (after the replication divisor) | 3,660–3,660 | 3,599 | band [3,660, 3,660] ±15% |
| ✅ pass | pool 1: `steady` active series (after the replication divisor) | 30,022–30,022 | 29,522 | band [30,022, 30,022] ±15% |
| ✅ pass | pool 1: `tiny` active series (after the replication divisor) | 20.00–20.00 | 19.67 | band [20.00, 20.00] ±15% |
| ✅ pass | pool 1: tenant rank order matches the workload shapes | steady > cardinal > churn > dashboards > rules > ghost > latecomer > tiny | steady > cardinal > churn > dashboards > rules > ghost > latecomer > tiny | pairs whose expected figures are within 10% of each other are not ranked |
| ✅ pass | pool 6 (query path): `dashboards` dominates | `dashboards` first, with at least 50% | dashboards 93.2%; rules 3.6%; cardinal 1.5%; steady 1.3% | rank 1 and ≥50% |
| ✅ pass | pool 7 (ruler CPU): `rules` dominates | `rules` first, with at least 50% | rules 87.3%; steady 3.9%; cardinal 3.3%; dashboards 2.8% | rank 1 and ≥50% |
| ✅ pass | ruler evaluation mode detected correctly | remote | pool 6 (query path): remote, pool 7 (ruler CPU): remote | exact |
| ✅ pass | rule outputs: `cardinal` record: series | 10 series over 1 record: names (present) | 10 series (1 output metrics) | exact |
| ✅ pass | rule outputs: `churn` record: series | 11 series over 2 record: names (present) | 11 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `dashboards` record: series | 11 series over 2 record: names (present) | 11 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `ghost` record: series | 0 series over 2 record: names (absent) | 0 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `latecomer` record: series | 11 series over 2 record: names (present) | 11 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `rules` record: series | 660 series over 120 record: names (present) | 660 series (120 output metrics) | exact |
| ✅ pass | rule outputs: `steady` record: series | 22 series over 4 record: names (present) | 22 series (4 output metrics) | exact |
| ✅ pass | rule outputs: `tiny` record: series | 0 series over 0 record: names (absent) | 0 series (0 output metrics) | exact |
| ✅ pass | pool 1: promcost equals Mimir's own gauge, read independently | every tenant within 0.5% (or 1 series) of the direct read | 9 of 9 tenants equal | 0.5% or 1 series |
| ✅ pass | write path detected correctly | classic | classic | exact |
| ✅ pass | counter pools stay sane across ingester-rolling-restart | every per-tenant figure finite and ≥ 0 | all finite and ≥ 0 | exact |

<details><summary>Pool 1 per tenant: expected vs measured</summary>

| tenant | expected (time-weighted band) | promcost | Mimir, read directly | points present (Mimir / model) |
|---|---:|---:|---:|---:|
| `cardinal` | 20,010 – 20,010 | 19,676 | 19,676 | 60 / 60 of 60 |
| `churn` | 4,944 – 6,211 | 5,122 | 5,122 | 60 / 60 of 60 |
| `dashboards` | 4,011 – 4,011 | 3,944 | 3,944 | 60 / 60 of 60 |
| `ghost` | 1,908 – 1,958 | 1,875 | 1,875 | 44 / 44 of 60 |
| `latecomer` | 1,877 – 1,910 | 1,854 | 1,854 | 57 / 57 of 60 |
| `monitoring` | not controlled | 111,525 | 111,525 | 60 / — of 60 |
| `rules` | 3,660 – 3,660 | 3,599 | 3,599 | 60 / 60 of 60 |
| `steady` | 30,022 – 30,022 | 29,522 | 29,522 | 60 / 60 of 60 |
| `tiny` | 20.00 – 20.00 | 19.67 | 19.67 | 60 / 60 of 60 |

</details>

<details><summary>pool 6 (query path): shares (chunk bytes fetched)</summary>

| tenant | driver | share |
|---|---:|---:|
| `dashboards` | 6,400,737,722 | 93.22% |
| `rules` | 246,249,013 | 3.59% |
| `cardinal` | 100,309,513 | 1.46% |
| `steady` | 87,157,174 | 1.27% |
| `latecomer` | 18,278,760 | 0.27% |
| `churn` | 7,600,753 | 0.11% |
| `ghost` | 5,737,452 | 0.08% |
| `tiny` | 187,162 | 0.00% |

</details>

<details><summary>pool 7 (ruler CPU): shares (rule-evaluation seconds)</summary>

| tenant | driver | share |
|---|---:|---:|
| `rules` | 318 | 87.28% |
| `steady` | 14.06 | 3.86% |
| `cardinal` | 12.09 | 3.32% |
| `dashboards` | 10.10 | 2.77% |
| `latecomer` | 3.52 | 0.96% |
| `churn` | 3.43 | 0.94% |
| `ghost` | 2.21 | 0.61% |
| `tiny` | 0.96 | 0.26% |

</details>

<details><summary>Why each tolerance, and details</summary>

- **pool 1 (ingester memory): shares sum to 100%** — shares are computed exactly from one set of drivers; only float rounding may separate them from 100%
- **pool 6 (query path): shares sum to 100%** — shares are computed exactly from one set of drivers; only float rounding may separate them from 100%
- **pool 7 (ruler CPU): shares sum to 100%** — shares are computed exactly from one set of drivers; only float rounding may separate them from 100%
- **pool 1 (ingester memory): unmeasured tenants are "not measured", never 0** — ADR 0003 decision 5: absence is a different fact from zero
- **pool 6 (query path): unmeasured tenants are "not measured", never 0** — ADR 0003 decision 5: absence is a different fact from zero
- **pool 7 (ruler CPU): unmeasured tenants are "not measured", never 0** — ADR 0003 decision 5: absence is a different fact from zero
- **pool 1: `cardinal` active series (after the replication divisor)** — the window overlaps ingester-rolling-restart: during an ingester rollout the sum-then-average query reads low while a replica is down (measured 9% on dev/mimir-k8s), so the band is widened to ±15%
  - subquery points with pipeline data: 60; points this tenant was present (model): 60; (Mimir): 60
- **pool 1: `churn` active series (after the replication divisor)** — the window overlaps ingester-rolling-restart: during an ingester rollout the sum-then-average query reads low while a replica is down (measured 9% on dev/mimir-k8s), so the band is widened to ±15%
  - subquery points with pipeline data: 60; points this tenant was present (model): 60; (Mimir): 60
- **pool 1: `dashboards` active series (after the replication divisor)** — the window overlaps ingester-rolling-restart: during an ingester rollout the sum-then-average query reads low while a replica is down (measured 9% on dev/mimir-k8s), so the band is widened to ±15%
  - subquery points with pipeline data: 60; points this tenant was present (model): 60; (Mimir): 60
- **pool 1: `ghost` active series (after the replication divisor)** — the window overlaps ingester-rolling-restart: during an ingester rollout the sum-then-average query reads low while a replica is down (measured 9% on dev/mimir-k8s), so the band is widened to ±15%
  - subquery points with pipeline data: 60; points this tenant was present (model): 44; (Mimir): 44
- **pool 1: `latecomer` active series (after the replication divisor)** — the window overlaps ingester-rolling-restart: during an ingester rollout the sum-then-average query reads low while a replica is down (measured 9% on dev/mimir-k8s), so the band is widened to ±15%
  - subquery points with pipeline data: 60; points this tenant was present (model): 57; (Mimir): 57
- **pool 1: `rules` active series (after the replication divisor)** — the window overlaps ingester-rolling-restart: during an ingester rollout the sum-then-average query reads low while a replica is down (measured 9% on dev/mimir-k8s), so the band is widened to ±15%
  - subquery points with pipeline data: 60; points this tenant was present (model): 60; (Mimir): 60
- **pool 1: `steady` active series (after the replication divisor)** — the window overlaps ingester-rolling-restart: during an ingester rollout the sum-then-average query reads low while a replica is down (measured 9% on dev/mimir-k8s), so the band is widened to ±15%
  - subquery points with pipeline data: 60; points this tenant was present (model): 60; (Mimir): 60
- **pool 1: `tiny` active series (after the replication divisor)** — the window overlaps ingester-rolling-restart: during an ingester rollout the sum-then-average query reads low while a replica is down (measured 9% on dev/mimir-k8s), so the band is widened to ±15%
  - subquery points with pipeline data: 60; points this tenant was present (model): 60; (Mimir): 60
- **pool 1: tenant rank order matches the workload shapes** — a near-tie can legitimately flip on gauge lag; everything else is a difference of shape
- **pool 6 (query path): `dashboards` dominates** — the workload makes `dashboards` this pool's driver by a wide margin by construction; a simple majority is a weaker claim than the margin built in, so it should hold under noise
  - driver: chunk bytes fetched (cortex_query_fetched_chunk_bytes_total)
- **pool 7 (ruler CPU): `rules` dominates** — the workload makes `rules` this pool's driver by a wide margin by construction; a simple majority is a weaker claim than the margin built in, so it should hold under noise
  - driver: rule-evaluation seconds (cortex_prometheus_rule_evaluation_duration_seconds_sum)
- **ruler evaluation mode detected correctly** — the environment's ruler configuration is known
- **rule outputs: `cardinal` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `churn` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `dashboards` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `ghost` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `latecomer` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `rules` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `steady` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `tiny` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **pool 1: promcost equals Mimir's own gauge, read independently** — the direct read applies the same de-duplication and averaging to the same points, so only rounding and the instant of each read may differ
  - cardinal: promcost 19,676, Mimir 19,676
  - churn: promcost 5,122, Mimir 5,122
  - dashboards: promcost 3,944, Mimir 3,944
  - ghost: promcost 1,875, Mimir 1,875
  - latecomer: promcost 1,854, Mimir 1,854
  - monitoring: promcost 111,525, Mimir 111,525
  - rules: promcost 3,599, Mimir 3,599
  - steady: promcost 29,522, Mimir 29,522
  - tiny: promcost 19.67, Mimir 19.67
- **write path detected correctly** — the environment's architecture is known
- **counter pools stay sane across ingester-rolling-restart** — increase() compensates for counter resets; a negative or infinite figure would mean it did not

</details>

## Window 30m, ending 2026-10-22 08:52 UTC

> clean window: no operational event inside it, so pool 1 is checked at the normal ±1%

| result | check | expected | measured | tolerance |
|---|---|---|---|---|
| ✅ pass | pool 1 (ingester memory): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 6 (query path): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 7 (ruler CPU): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 1 (ingester memory): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 0 unmeasured: none | exact |
| ✅ pass | pool 6 (query path): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 0 unmeasured: none | exact |
| ✅ pass | pool 7 (ruler CPU): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 0 unmeasured: none | exact |
| ✅ pass | pool 1: `cardinal` active series (after the replication divisor) | 20,010–20,010 | 20,010 | band [20,010, 20,010] ±1% |
| ✅ pass | pool 1: `churn` active series (after the replication divisor) | 5,811–6,211 | 5,944 | band [5,811, 6,211] ±1% |
| ✅ pass | pool 1: `dashboards` active series (after the replication divisor) | 4,011–4,011 | 4,011 | band [4,011, 4,011] ±1% |
| ✅ pass | pool 1: `ghost` active series (after the replication divisor) | 805–905 | 838 | band [805, 905] ±1% |
| ✅ pass | pool 1: `latecomer` active series (after the replication divisor) | 2,011–2,011 | 2,011 | band [2,011, 2,011] ±1% |
| ✅ pass | pool 1: `rules` active series (after the replication divisor) | 3,660–3,660 | 3,660 | band [3,660, 3,660] ±1% |
| ✅ pass | pool 1: `steady` active series (after the replication divisor) | 30,022–30,022 | 30,022 | band [30,022, 30,022] ±1% |
| ✅ pass | pool 1: `tiny` active series (after the replication divisor) | 20.00–20.00 | 20.00 | band [20.00, 20.00] ±1% |
| ✅ pass | pool 1: tenant rank order matches the workload shapes | steady > cardinal > churn > dashboards > rules > latecomer > ghost > tiny | steady > cardinal > churn > dashboards > rules > latecomer > ghost > tiny | pairs whose expected figures are within 10% of each other are not ranked |
| ✅ pass | pool 6 (query path): `dashboards` dominates | `dashboards` first, with at least 50% | dashboards 94.9%; rules 2.5%; cardinal 1.4%; steady 0.9% | rank 1 and ≥50% |
| ✅ pass | pool 7 (ruler CPU): `rules` dominates | `rules` first, with at least 50% | rules 87.1%; steady 3.9%; cardinal 3.5%; dashboards 2.8% | rank 1 and ≥50% |
| ✅ pass | ruler evaluation mode detected correctly | remote | pool 6 (query path): remote, pool 7 (ruler CPU): remote | exact |
| ✅ pass | rule outputs: `cardinal` record: series | 10 series over 1 record: names (present) | 10 series (1 output metrics) | exact |
| ✅ pass | rule outputs: `churn` record: series | 11 series over 2 record: names (present) | 11 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `dashboards` record: series | 11 series over 2 record: names (present) | 11 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `ghost` record: series | 0 series over 2 record: names (absent) | 0 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `latecomer` record: series | 11 series over 2 record: names (present) | 11 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `rules` record: series | 660 series over 120 record: names (present) | 660 series (120 output metrics) | exact |
| ✅ pass | rule outputs: `steady` record: series | 22 series over 4 record: names (present) | 22 series (4 output metrics) | exact |
| ✅ pass | rule outputs: `tiny` record: series | 0 series over 0 record: names (absent) | 0 series (0 output metrics) | exact |
| ✅ pass | pool 1: promcost equals Mimir's own gauge, read independently | every tenant within 0.5% (or 1 series) of the direct read | 9 of 9 tenants equal | 0.5% or 1 series |
| ✅ pass | write path detected correctly | classic | classic | exact |

<details><summary>Pool 1 per tenant: expected vs measured</summary>

| tenant | expected (time-weighted band) | promcost | Mimir, read directly | points present (Mimir / model) |
|---|---:|---:|---:|---:|
| `cardinal` | 20,010 – 20,010 | 20,010 | 20,010 | 30 / 30 of 30 |
| `churn` | 5,811 – 6,211 | 5,944 | 5,944 | 30 / 30 of 30 |
| `dashboards` | 4,011 – 4,011 | 4,011 | 4,011 | 30 / 30 of 30 |
| `ghost` | 805 – 905 | 838 | 838 | 14 / 14 of 30 |
| `latecomer` | 2,011 – 2,011 | 2,011 | 2,011 | 30 / 30 of 30 |
| `monitoring` | not controlled | 112,386 | 112,386 | 30 / — of 30 |
| `rules` | 3,660 – 3,660 | 3,660 | 3,660 | 30 / 30 of 30 |
| `steady` | 30,022 – 30,022 | 30,022 | 30,022 | 30 / 30 of 30 |
| `tiny` | 20.00 – 20.00 | 20.00 | 20.00 | 30 / 30 of 30 |

</details>

<details><summary>pool 6 (query path): shares (chunk bytes fetched)</summary>

| tenant | driver | share |
|---|---:|---:|
| `dashboards` | 3,784,141,309 | 94.86% |
| `rules` | 100,597,836 | 2.52% |
| `cardinal` | 54,742,373 | 1.37% |
| `steady` | 34,759,921 | 0.87% |
| `latecomer` | 11,400,992 | 0.29% |
| `churn` | 3,589,932 | 0.09% |
| `tiny` | 91,113 | 0.00% |
| `ghost` | 0.00 | 0.00% |

</details>

<details><summary>pool 7 (ruler CPU): shares (rule-evaluation seconds)</summary>

| tenant | driver | share |
|---|---:|---:|
| `rules` | 157 | 87.15% |
| `steady` | 7.02 | 3.91% |
| `cardinal` | 6.23 | 3.47% |
| `dashboards` | 4.97 | 2.76% |
| `latecomer` | 1.78 | 0.99% |
| `churn` | 1.69 | 0.94% |
| `ghost` | 0.92 | 0.51% |
| `tiny` | 0.50 | 0.28% |

</details>

<details><summary>Why each tolerance, and details</summary>

- **pool 1 (ingester memory): shares sum to 100%** — shares are computed exactly from one set of drivers; only float rounding may separate them from 100%
- **pool 6 (query path): shares sum to 100%** — shares are computed exactly from one set of drivers; only float rounding may separate them from 100%
- **pool 7 (ruler CPU): shares sum to 100%** — shares are computed exactly from one set of drivers; only float rounding may separate them from 100%
- **pool 1 (ingester memory): unmeasured tenants are "not measured", never 0** — ADR 0003 decision 5: absence is a different fact from zero
- **pool 6 (query path): unmeasured tenants are "not measured", never 0** — ADR 0003 decision 5: absence is a different fact from zero
- **pool 7 (ruler CPU): unmeasured tenants are "not measured", never 0** — ADR 0003 decision 5: absence is a different fact from zero
- **pool 1: `cardinal` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 30; points this tenant was present (model): 30; (Mimir): 30
- **pool 1: `churn` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 30; points this tenant was present (model): 30; (Mimir): 30
- **pool 1: `dashboards` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 30; points this tenant was present (model): 30; (Mimir): 30
- **pool 1: `ghost` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 30; points this tenant was present (model): 14; (Mimir): 14
- **pool 1: `latecomer` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 30; points this tenant was present (model): 30; (Mimir): 30
- **pool 1: `rules` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 30; points this tenant was present (model): 30; (Mimir): 30
- **pool 1: `steady` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 30; points this tenant was present (model): 30; (Mimir): 30
- **pool 1: `tiny` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 30; points this tenant was present (model): 30; (Mimir): 30
- **pool 1: tenant rank order matches the workload shapes** — a near-tie can legitimately flip on gauge lag; everything else is a difference of shape
- **pool 6 (query path): `dashboards` dominates** — the workload makes `dashboards` this pool's driver by a wide margin by construction; a simple majority is a weaker claim than the margin built in, so it should hold under noise
  - driver: chunk bytes fetched (cortex_query_fetched_chunk_bytes_total)
- **pool 7 (ruler CPU): `rules` dominates** — the workload makes `rules` this pool's driver by a wide margin by construction; a simple majority is a weaker claim than the margin built in, so it should hold under noise
  - driver: rule-evaluation seconds (cortex_prometheus_rule_evaluation_duration_seconds_sum)
- **ruler evaluation mode detected correctly** — the environment's ruler configuration is known
- **rule outputs: `cardinal` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `churn` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `dashboards` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `ghost` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `latecomer` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `rules` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `steady` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **rule outputs: `tiny` record: series** — each rule is sum by (shard) or sum over a metric with a known shard count, so its output series are known in advance; the count is an instant query
- **pool 1: promcost equals Mimir's own gauge, read independently** — the direct read applies the same de-duplication and averaging to the same points, so only rounding and the instant of each read may differ
  - cardinal: promcost 20,010, Mimir 20,010
  - churn: promcost 5,944, Mimir 5,944
  - dashboards: promcost 4,011, Mimir 4,011
  - ghost: promcost 838, Mimir 838
  - latecomer: promcost 2,011, Mimir 2,011
  - monitoring: promcost 112,386, Mimir 112,386
  - rules: promcost 3,660, Mimir 3,660
  - steady: promcost 30,022, Mimir 30,022
  - tiny: promcost 20.00, Mimir 20.00
- **write path detected correctly** — the environment's architecture is known

</details>

## Window 10m, ending 2026-10-22 08:52 UTC

> clean window: no operational event inside it, so pool 1 is checked at the normal ±1%

| result | check | expected | measured | tolerance |
|---|---|---|---|---|
| ✅ pass | pool 1 (ingester memory): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 6 (query path): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 7 (ruler CPU): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 1 (ingester memory): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 1 unmeasured: ghost | exact |
| ✅ pass | pool 6 (query path): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 0 unmeasured: none | exact |
| ✅ pass | pool 7 (ruler CPU): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 0 unmeasured: none | exact |
| ✅ pass | pool 1: `cardinal` active series (after the replication divisor) | 20,010–20,010 | 20,010 | band [20,010, 20,010] ±1% |
| ✅ pass | pool 1: `churn` active series (after the replication divisor) | 5,811–6,211 | 5,944 | band [5,811, 6,211] ±1% |
| ✅ pass | pool 1: `dashboards` active series (after the replication divisor) | 4,011–4,011 | 4,011 | band [4,011, 4,011] ±1% |
| ✅ pass | pool 1: `ghost` active series | no active series in this window (absent or 0) | not measured | exact |
| ✅ pass | pool 1: `latecomer` active series (after the replication divisor) | 2,011–2,011 | 2,011 | band [2,011, 2,011] ±1% |
| ✅ pass | pool 1: `rules` active series (after the replication divisor) | 3,660–3,660 | 3,660 | band [3,660, 3,660] ±1% |
| ✅ pass | pool 1: `steady` active series (after the replication divisor) | 30,022–30,022 | 30,022 | band [30,022, 30,022] ±1% |
| ✅ pass | pool 1: `tiny` active series (after the replication divisor) | 20.00–20.00 | 20.00 | band [20.00, 20.00] ±1% |
| ✅ pass | pool 1: tenant rank order matches the workload shapes | steady > cardinal > churn > dashboards > rules > latecomer > tiny | steady > cardinal > churn > dashboards > rules > latecomer > tiny | pairs whose expected figures are within 10% of each other are not ranked |
| ✅ pass | pool 6 (query path): `dashboards` dominates | `dashboards` first, with at least 50% | dashboards 95.1%; rules 2.3%; cardinal 1.5%; steady 0.7% | rank 1 and ≥50% |
| ✅ pass | pool 7 (ruler CPU): `rules` dominates | `rules` first, with at least 50% | rules 87.3%; cardinal 3.7%; steady 3.6%; dashboards 2.6% | rank 1 and ≥50% |
| ✅ pass | ruler evaluation mode detected correctly | remote | pool 6 (query path): remote, pool 7 (ruler CPU): remote | exact |
| ✅ pass | `ghost` after it went silent: 0 or not measured | 0 active series, or not measured | not measured | exact |
| ✅ pass | rule outputs: `cardinal` record: series | 10 series over 1 record: names (present) | 10 series (1 output metrics) | exact |
| ✅ pass | rule outputs: `churn` record: series | 11 series over 2 record: names (present) | 11 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `dashboards` record: series | 11 series over 2 record: names (present) | 11 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `ghost` record: series | 0 series over 2 record: names (absent) | 0 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `latecomer` record: series | 11 series over 2 record: names (present) | 11 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `rules` record: series | 660 series over 120 record: names (present) | 660 series (120 output metrics) | exact |
| ✅ pass | rule outputs: `steady` record: series | 22 series over 4 record: names (present) | 22 series (4 output metrics) | exact |
| ✅ pass | rule outputs: `tiny` record: series | 0 series over 0 record: names (absent) | 0 series (0 output metrics) | exact |
| ✅ pass | pool 1: promcost equals Mimir's own gauge, read independently | every tenant within 0.5% (or 1 series) of the direct read | 8 of 8 tenants equal | 0.5% or 1 series |
| ✅ pass | write path detected correctly | classic | classic | exact |

<details><summary>Pool 1 per tenant: expected vs measured</summary>

| tenant | expected (time-weighted band) | promcost | Mimir, read directly | points present (Mimir / model) |
|---|---:|---:|---:|---:|
| `cardinal` | 20,010 – 20,010 | 20,010 | 20,010 | 10 / 10 of 10 |
| `churn` | 5,811 – 6,211 | 5,944 | 5,944 | 10 / 10 of 10 |
| `dashboards` | 4,011 – 4,011 | 4,011 | 4,011 | 10 / 10 of 10 |
| `ghost` | 0.00 – 0.00 | not measured | — | 0 / 0 of 10 |
| `latecomer` | 2,011 – 2,011 | 2,011 | 2,011 | 10 / 10 of 10 |
| `monitoring` | not controlled | 112,358 | 112,358 | 10 / — of 10 |
| `rules` | 3,660 – 3,660 | 3,660 | 3,660 | 10 / 10 of 10 |
| `steady` | 30,022 – 30,022 | 30,022 | 30,022 | 10 / 10 of 10 |
| `tiny` | 20.00 – 20.00 | 20.00 | 20.00 | 10 / 10 of 10 |

</details>

<details><summary>pool 6 (query path): shares (chunk bytes fetched)</summary>

| tenant | driver | share |
|---|---:|---:|
| `dashboards` | 1,357,191,242 | 95.14% |
| `rules` | 32,519,820 | 2.28% |
| `cardinal` | 21,835,789 | 1.53% |
| `steady` | 9,641,716 | 0.68% |
| `latecomer` | 3,992,825 | 0.28% |
| `churn` | 1,238,632 | 0.09% |
| `tiny` | 37,512 | 0.00% |
| `ghost` | 0.00 | 0.00% |

</details>

<details><summary>pool 7 (ruler CPU): shares (rule-evaluation seconds)</summary>

| tenant | driver | share |
|---|---:|---:|
| `rules` | 51.66 | 87.26% |
| `cardinal` | 2.21 | 3.73% |
| `steady` | 2.14 | 3.61% |
| `dashboards` | 1.53 | 2.59% |
| `churn` | 0.59 | 1.00% |
| `latecomer` | 0.58 | 0.98% |
| `ghost` | 0.31 | 0.52% |
| `tiny` | 0.19 | 0.31% |

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
- **pool 1: promcost equals Mimir's own gauge, read independently** — the direct read applies the same de-duplication and averaging to the same points, so only rounding and the instant of each read may differ
  - cardinal: promcost 20,010, Mimir 20,010
  - churn: promcost 5,944, Mimir 5,944
  - dashboards: promcost 4,011, Mimir 4,011
  - latecomer: promcost 2,011, Mimir 2,011
  - monitoring: promcost 112,358, Mimir 112,358
  - rules: promcost 3,660, Mimir 3,660
  - steady: promcost 30,022, Mimir 30,022
  - tiny: promcost 20.00, Mimir 20.00
- **write path detected correctly** — the environment's architecture is known

</details>

## What this run does not show

- **Resource calibration.** It checks that promcost reads Mimir's per-tenant drivers correctly. Whether active series are *the* driver of ingester memory is ADR 0003 decision 6's question, and is not answered here.
- **Pools 6 and 7 against absolute truth.** Fetched chunk bytes and rule-evaluation seconds depend on chunk encoding, caches and CPU contention, so only their ranking is checked, not their values.
- **Real tenants.** Every costed tenant is synthetic (`loadgen`), so its ground truth is known. Real workloads have shapes this profile does not: native histograms, exemplars, OTLP ingestion, bursty queries, rule groups that time out.
- **A production-sized cluster.** The environment is one cloud container running k3s in Docker on 4 vCPUs: Mimir's microservices with zone-aware ingesters (3 zones, one ingester each), Kafka for ingest storage or RF=3 for classic, a dedicated ruler query path, kube-prometheus-stack for meta-monitoring. It is not multi-node, so nothing here exercises network partitions, node loss or real CPU contention between hosts.
- **Upstream images as shipped by their registries.** Every image is pulled from Docker Hub through `mirror.gcr.io`, because quay.io, registry.k8s.io and ghcr.io are not reachable from the container. The staging lane replaced the charts' non-Docker-Hub images with Docker Hub equivalents (see its README's deviations table), and MinIO with a pullable S3-compatible store. Mimir itself is the upstream `grafana/mimir:3.2.0`.
- **Long windows.** Windows reach at most 1h, on a 45-minute workload timeline (`profiles/short.yaml`): the container is reclaimed whenever the session idles. There is no 24h soak, so nothing here covers compaction, block upload to the store-gateway, or day-scale counter behaviour.
- **Anything about tenants it did not generate.** The metrics tenant is real and uncontrolled: it is in every share and every sum, but no figure for it is checked.

Raw outputs (every promcost run as JSON and markdown, the ground truth, every check as JSON) are in [`raw/`](raw/) and [`checks.json`](checks.json).
