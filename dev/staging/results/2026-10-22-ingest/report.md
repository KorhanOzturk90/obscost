# promcost validation — `staging-ingest`, profile `short`

**87 passed, 0 failed, 0 skipped** across 3 window(s) (1h, 30m, 10m). Generated 2026-10-22 07:20 UTC.

No check failed.

## What this was run against

| | |
|---|---|
| environment | `staging-ingest` |
| Mimir | `http://localhost:8091`, Mimir's own metrics in tenant `monitoring` |
| write path | expected `ingest storage`, detected `ingest storage` |
| replication factor | `1` (from `ingest storage: one owner per partition, zone replicas collapsed with max, so there is nothing to divide`) |
| rule evaluation | expected `remote` |
| promcost | `promcost @ 5a9df1f (launch/fixes)` |
| workload | `loadgen`, profile `short`, started 2026-10-22 05:49 UTC |
| operational events | ingester-rolling-restart (2026-10-22T01:28:19Z → 2026-10-22T01:32:41Z); ingester-rolling-restart (2026-10-22T02:38:10Z → 2026-10-22T02:42:36Z); ingester-rolling-restart (2026-10-22T06:23:01Z → 2026-10-22T06:27:26Z) |

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

## Window 1h, ending 2026-10-22 07:20 UTC

> overlaps ingester-rolling-restart: pool 1 is checked against a band widened to ±15%

| result | check | expected | measured | tolerance |
|---|---|---|---|---|
| ✅ pass | pool 1 (ingester memory): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 6 (query path): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 7 (ruler CPU): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 1 (ingester memory): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 0 unmeasured: none | exact |
| ✅ pass | pool 6 (query path): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 0 unmeasured: none | exact |
| ✅ pass | pool 7 (ruler CPU): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 0 unmeasured: none | exact |
| ✅ pass | pool 1: `cardinal` active series (after the replication divisor) | 20,010–20,010 | 20,010 | band [20,010, 20,010] ±15% |
| ✅ pass | pool 1: `churn` active series (after the replication divisor) | 5,344–6,211 | 5,344 | band [5,344, 6,211] ±15% |
| ✅ pass | pool 1: `dashboards` active series (after the replication divisor) | 4,011–4,011 | 4,011 | band [4,011, 4,011] ±15% |
| ✅ pass | pool 1: `ghost` active series (after the replication divisor) | 1,758–1,758 | 1,758 | band [1,758, 1,758] ±15% |
| ✅ pass | pool 1: `latecomer` active series (after the replication divisor) | 1,977–2,011 | 2,011 | band [1,977, 2,011] ±15% |
| ✅ pass | pool 1: `rules` active series (after the replication divisor) | 3,660–3,660 | 3,660 | band [3,660, 3,660] ±15% |
| ✅ pass | pool 1: `steady` active series (after the replication divisor) | 30,022–30,022 | 30,022 | band [30,022, 30,022] ±15% |
| ✅ pass | pool 1: `tiny` active series (after the replication divisor) | 20.00–20.00 | 20.00 | band [20.00, 20.00] ±15% |
| ✅ pass | pool 1: tenant rank order matches the workload shapes | steady > cardinal > churn > dashboards > rules > latecomer > ghost > tiny | steady > cardinal > churn > dashboards > rules > latecomer > ghost > tiny | pairs whose expected figures are within 10% of each other are not ranked |
| ✅ pass | pool 6 (query path): `dashboards` dominates | `dashboards` first, with at least 50% | dashboards 97.4%; rules 1.3%; cardinal 0.8%; steady 0.4% | rank 1 and ≥50% |
| ✅ pass | pool 7 (ruler CPU): `rules` dominates | `rules` first, with at least 50% | rules 90.4%; steady 3.1%; dashboards 1.6%; cardinal 1.6% | rank 1 and ≥50% |
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
| ✅ pass | write path detected correctly | ingest storage | ingest storage | exact |
| ✅ pass | counter pools stay sane across ingester-rolling-restart | every per-tenant figure finite and ≥ 0 | all finite and ≥ 0 | exact |

<details><summary>Pool 1 per tenant: expected vs measured</summary>

| tenant | expected (time-weighted band) | promcost | Mimir, read directly | points present (Mimir / model) |
|---|---:|---:|---:|---:|
| `cardinal` | 20,010 – 20,010 | 20,010 | 20,010 | 60 / 60 of 60 |
| `churn` | 5,344 – 6,211 | 5,344 | 5,344 | 60 / 60 of 60 |
| `dashboards` | 4,011 – 4,011 | 4,011 | 4,011 | 60 / 60 of 60 |
| `ghost` | 1,758 – 1,758 | 1,758 | 1,758 | 41 / 41 of 60 |
| `latecomer` | 1,977 – 2,011 | 2,011 | 2,011 | 60 / 60 of 60 |
| `monitoring` | not controlled | 108,956 | 108,956 | 60 / — of 60 |
| `rules` | 3,660 – 3,660 | 3,660 | 3,660 | 60 / 60 of 60 |
| `steady` | 30,022 – 30,022 | 30,022 | 30,022 | 60 / 60 of 60 |
| `tiny` | 20.00 – 20.00 | 20.00 | 20.00 | 60 / 60 of 60 |

</details>

<details><summary>pool 6 (query path): shares (chunk bytes fetched)</summary>

| tenant | driver | share |
|---|---:|---:|
| `dashboards` | 15,574,608,108 | 97.36% |
| `rules` | 202,684,544 | 1.27% |
| `cardinal` | 127,488,403 | 0.80% |
| `steady` | 63,858,202 | 0.40% |
| `latecomer` | 16,213,821 | 0.10% |
| `churn` | 7,119,529 | 0.04% |
| `ghost` | 4,273,412 | 0.03% |
| `tiny` | 166,312 | 0.00% |
| `monitoring` | 0.00 | 0.00% |

</details>

<details><summary>pool 7 (ruler CPU): shares (rule-evaluation seconds)</summary>

| tenant | driver | share |
|---|---:|---:|
| `rules` | 656 | 90.38% |
| `steady` | 22.49 | 3.10% |
| `dashboards` | 11.78 | 1.62% |
| `cardinal` | 11.71 | 1.61% |
| `latecomer` | 9.75 | 1.34% |
| `churn` | 9.10 | 1.25% |
| `ghost` | 4.09 | 0.56% |
| `tiny` | 0.91 | 0.13% |

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
  - subquery points with pipeline data: 60; points this tenant was present (model): 41; (Mimir): 41
- **pool 1: `latecomer` active series (after the replication divisor)** — the window overlaps ingester-rolling-restart: during an ingester rollout the sum-then-average query reads low while a replica is down (measured 9% on dev/mimir-k8s), so the band is widened to ±15%
  - subquery points with pipeline data: 60; points this tenant was present (model): 60; (Mimir): 60
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
  - cardinal: promcost 20,010, Mimir 20,010
  - churn: promcost 5,344, Mimir 5,344
  - dashboards: promcost 4,011, Mimir 4,011
  - ghost: promcost 1,758, Mimir 1,758
  - latecomer: promcost 2,011, Mimir 2,011
  - monitoring: promcost 108,956, Mimir 108,956
  - rules: promcost 3,660, Mimir 3,660
  - steady: promcost 30,022, Mimir 30,022
  - tiny: promcost 20.00, Mimir 20.00
- **write path detected correctly** — the environment's architecture is known
- **counter pools stay sane across ingester-rolling-restart** — increase() compensates for counter resets; a negative or infinite figure would mean it did not

</details>

## Window 30m, ending 2026-10-22 07:20 UTC

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
| ✅ pass | pool 1: `churn` active series (after the replication divisor) | 5,811–6,211 | 6,011 | band [5,811, 6,211] ±1% |
| ✅ pass | pool 1: `dashboards` active series (after the replication divisor) | 4,011–4,011 | 4,011 | band [4,011, 4,011] ±1% |
| ✅ pass | pool 1: `ghost` active series (after the replication divisor) | 504–504 | 504 | band [504, 504] ±1% |
| ✅ pass | pool 1: `latecomer` active series (after the replication divisor) | 2,011–2,011 | 2,011 | band [2,011, 2,011] ±1% |
| ✅ pass | pool 1: `rules` active series (after the replication divisor) | 3,660–3,660 | 3,660 | band [3,660, 3,660] ±1% |
| ✅ pass | pool 1: `steady` active series (after the replication divisor) | 30,022–30,022 | 30,022 | band [30,022, 30,022] ±1% |
| ✅ pass | pool 1: `tiny` active series (after the replication divisor) | 20.00–20.00 | 20.00 | band [20.00, 20.00] ±1% |
| ✅ pass | pool 1: tenant rank order matches the workload shapes | steady > cardinal > churn > dashboards > rules > latecomer > ghost > tiny | steady > cardinal > churn > dashboards > rules > latecomer > ghost > tiny | pairs whose expected figures are within 10% of each other are not ranked |
| ✅ pass | pool 6 (query path): `dashboards` dominates | `dashboards` first, with at least 50% | dashboards 97.3%; rules 1.2%; cardinal 0.9%; steady 0.4% | rank 1 and ≥50% |
| ✅ pass | pool 7 (ruler CPU): `rules` dominates | `rules` first, with at least 50% | rules 90.7%; steady 3.0%; cardinal 1.7%; dashboards 1.6% | rank 1 and ≥50% |
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
| ✅ pass | write path detected correctly | ingest storage | ingest storage | exact |

<details><summary>Pool 1 per tenant: expected vs measured</summary>

| tenant | expected (time-weighted band) | promcost | Mimir, read directly | points present (Mimir / model) |
|---|---:|---:|---:|---:|
| `cardinal` | 20,010 – 20,010 | 20,010 | 20,010 | 30 / 30 of 30 |
| `churn` | 5,811 – 6,211 | 6,011 | 6,011 | 30 / 30 of 30 |
| `dashboards` | 4,011 – 4,011 | 4,011 | 4,011 | 30 / 30 of 30 |
| `ghost` | 504 – 504 | 504 | 504 | 11 / 11 of 30 |
| `latecomer` | 2,011 – 2,011 | 2,011 | 2,011 | 30 / 30 of 30 |
| `monitoring` | not controlled | 107,179 | 107,179 | 30 / — of 30 |
| `rules` | 3,660 – 3,660 | 3,660 | 3,660 | 30 / 30 of 30 |
| `steady` | 30,022 – 30,022 | 30,022 | 30,022 | 30 / 30 of 30 |
| `tiny` | 20.00 – 20.00 | 20.00 | 20.00 | 30 / 30 of 30 |

</details>

<details><summary>pool 6 (query path): shares (chunk bytes fetched)</summary>

| tenant | driver | share |
|---|---:|---:|
| `dashboards` | 8,267,240,187 | 97.33% |
| `rules` | 102,760,467 | 1.21% |
| `cardinal` | 76,978,983 | 0.91% |
| `steady` | 32,968,334 | 0.39% |
| `latecomer` | 9,991,718 | 0.12% |
| `churn` | 3,589,932 | 0.04% |
| `tiny` | 87,325 | 0.00% |
| `ghost` | 0.00 | 0.00% |

</details>

<details><summary>pool 7 (ruler CPU): shares (rule-evaluation seconds)</summary>

| tenant | driver | share |
|---|---:|---:|
| `rules` | 329 | 90.70% |
| `steady` | 11.00 | 3.03% |
| `cardinal` | 6.10 | 1.68% |
| `dashboards` | 5.72 | 1.58% |
| `latecomer` | 5.00 | 1.38% |
| `churn` | 4.69 | 1.29% |
| `ghost` | 0.78 | 0.22% |
| `tiny` | 0.46 | 0.13% |

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
  - subquery points with pipeline data: 30; points this tenant was present (model): 11; (Mimir): 11
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
  - churn: promcost 6,011, Mimir 6,011
  - dashboards: promcost 4,011, Mimir 4,011
  - ghost: promcost 504, Mimir 504
  - latecomer: promcost 2,011, Mimir 2,011
  - monitoring: promcost 107,179, Mimir 107,179
  - rules: promcost 3,660, Mimir 3,660
  - steady: promcost 30,022, Mimir 30,022
  - tiny: promcost 20.00, Mimir 20.00
- **write path detected correctly** — the environment's architecture is known

</details>

## Window 10m, ending 2026-10-22 07:20 UTC

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
| ✅ pass | pool 1: `churn` active series (after the replication divisor) | 5,811–6,211 | 6,011 | band [5,811, 6,211] ±1% |
| ✅ pass | pool 1: `dashboards` active series (after the replication divisor) | 4,011–4,011 | 4,011 | band [4,011, 4,011] ±1% |
| ✅ pass | pool 1: `ghost` active series | no active series in this window (absent or 0) | not measured | exact |
| ✅ pass | pool 1: `latecomer` active series (after the replication divisor) | 2,011–2,011 | 2,011 | band [2,011, 2,011] ±1% |
| ✅ pass | pool 1: `rules` active series (after the replication divisor) | 3,660–3,660 | 3,660 | band [3,660, 3,660] ±1% |
| ✅ pass | pool 1: `steady` active series (after the replication divisor) | 30,022–30,022 | 30,022 | band [30,022, 30,022] ±1% |
| ✅ pass | pool 1: `tiny` active series (after the replication divisor) | 20.00–20.00 | 20.00 | band [20.00, 20.00] ±1% |
| ✅ pass | pool 1: tenant rank order matches the workload shapes | steady > cardinal > churn > dashboards > rules > latecomer > tiny | steady > cardinal > churn > dashboards > rules > latecomer > tiny | pairs whose expected figures are within 10% of each other are not ranked |
| ✅ pass | pool 6 (query path): `dashboards` dominates | `dashboards` first, with at least 50% | dashboards 97.6%; cardinal 1.0%; rules 1.0%; steady 0.3% | rank 1 and ≥50% |
| ✅ pass | pool 7 (ruler CPU): `rules` dominates | `rules` first, with at least 50% | rules 90.6%; steady 2.9%; cardinal 1.8%; dashboards 1.5% | rank 1 and ≥50% |
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
| ✅ pass | write path detected correctly | ingest storage | ingest storage | exact |

<details><summary>Pool 1 per tenant: expected vs measured</summary>

| tenant | expected (time-weighted band) | promcost | Mimir, read directly | points present (Mimir / model) |
|---|---:|---:|---:|---:|
| `cardinal` | 20,010 – 20,010 | 20,010 | 20,010 | 10 / 10 of 10 |
| `churn` | 5,811 – 6,211 | 6,011 | 6,011 | 10 / 10 of 10 |
| `dashboards` | 4,011 – 4,011 | 4,011 | 4,011 | 10 / 10 of 10 |
| `ghost` | 0.00 – 0.00 | not measured | — | 0 / 0 of 10 |
| `latecomer` | 2,011 – 2,011 | 2,011 | 2,011 | 10 / 10 of 10 |
| `monitoring` | not controlled | 107,158 | 107,158 | 10 / — of 10 |
| `rules` | 3,660 – 3,660 | 3,660 | 3,660 | 10 / 10 of 10 |
| `steady` | 30,022 – 30,022 | 30,022 | 30,022 | 10 / 10 of 10 |
| `tiny` | 20.00 – 20.00 | 20.00 | 20.00 | 10 / 10 of 10 |

</details>

<details><summary>pool 6 (query path): shares (chunk bytes fetched)</summary>

| tenant | driver | share |
|---|---:|---:|
| `dashboards` | 2,856,777,779 | 97.56% |
| `cardinal` | 29,551,651 | 1.01% |
| `rules` | 28,978,413 | 0.99% |
| `steady` | 8,007,558 | 0.27% |
| `latecomer` | 3,658,218 | 0.12% |
| `churn` | 1,238,723 | 0.04% |
| `tiny` | 16,043 | 0.00% |
| `ghost` | 0.00 | 0.00% |

</details>

<details><summary>pool 7 (ruler CPU): shares (rule-evaluation seconds)</summary>

| tenant | driver | share |
|---|---:|---:|
| `rules` | 111 | 90.63% |
| `steady` | 3.53 | 2.89% |
| `cardinal` | 2.22 | 1.82% |
| `dashboards` | 1.85 | 1.51% |
| `latecomer` | 1.75 | 1.43% |
| `churn` | 1.69 | 1.38% |
| `ghost` | 0.27 | 0.22% |
| `tiny` | 0.15 | 0.13% |

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
  - churn: promcost 6,011, Mimir 6,011
  - dashboards: promcost 4,011, Mimir 4,011
  - latecomer: promcost 2,011, Mimir 2,011
  - monitoring: promcost 107,158, Mimir 107,158
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
