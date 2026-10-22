# promcost validation — `staging-ingest`, profile `short`

**79 passed, 4 failed, 0 skipped** across 3 window(s) (1h, 30m, 10m). Generated 2026-10-22 03:26 UTC.

Failures, grouped by cause:

- **pool 1 averages a tenant over its own presence, not the window (mid-run start and silence overstated)** (#66) — 4 check(s), window(s) 1h, 30m.

## What this was run against

| | |
|---|---|
| environment | `staging-ingest` |
| Mimir | `http://localhost:8091`, Mimir's own metrics in tenant `monitoring` |
| write path | expected `ingest storage`, detected `ingest storage` |
| replication factor | `1` (from `ingest storage: one owner per partition, zone replicas collapsed with max, so there is nothing to divide`) |
| rule evaluation | expected `remote` |
| promcost | `/home/user/promcost-bin/promcost (no version command)` |
| workload | `loadgen`, profile `short`, started 2026-10-22 02:05 UTC |
| operational events | ingester-rolling-restart (2026-10-22T01:28:19Z → 2026-10-22T01:32:41Z); ingester-rolling-restart (2026-10-22T02:38:10Z → 2026-10-22T02:42:36Z) |

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

## Window 1h, ending 2026-10-22 03:26 UTC

> overlaps ingester-rolling-restart

| result | check | expected | measured | tolerance |
|---|---|---|---|---|
| ✅ pass | pool 1 (ingester memory): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 6 (query path): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 7 (ruler CPU): shares sum to 100% | 100% | 100.0000% | ±0.1pp |
| ✅ pass | pool 1 (ingester memory): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 0 unmeasured: none | exact |
| ✅ pass | pool 6 (query path): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 0 unmeasured: none | exact |
| ✅ pass | pool 7 (ruler CPU): unmeasured tenants are "not measured", never 0 | every cell of each unmeasured tenant's row reads "not measured" | 0 unmeasured: none | exact |
| ✅ pass | pool 1: `cardinal` active series (after the replication divisor) | 20,010–20,010 | 20,010 | band [20,010, 20,010] ±15% |
| ✅ pass | pool 1: `churn` active series (after the replication divisor) | 5,811–6,211 | 5,711 | band [5,811, 6,211] ±15% |
| ✅ pass | pool 1: `dashboards` active series (after the replication divisor) | 4,011–4,011 | 4,011 | band [4,011, 4,011] ±15% |
| ❌ fail | pool 1: `ghost` active series (after the replication divisor) | 2,209–2,259 | 2,651 | band [2,209, 2,259] ±15% |
| ❌ fail | pool 1: `latecomer` active series (after the replication divisor) | 1,676–1,709 | 2,011 | band [1,676, 1,709] ±15% |
| ✅ pass | pool 1: `rules` active series (after the replication divisor) | 3,660–3,660 | 3,660 | band [3,660, 3,660] ±15% |
| ✅ pass | pool 1: `steady` active series (after the replication divisor) | 30,022–30,022 | 30,022 | band [30,022, 30,022] ±15% |
| ✅ pass | pool 1: `tiny` active series (after the replication divisor) | 20.00–20.00 | 20.00 | band [20.00, 20.00] ±15% |
| ✅ pass | pool 1: tenant rank order matches the workload shapes | steady > cardinal > churn > dashboards > rules > ghost > latecomer > tiny | steady > cardinal > churn > dashboards > rules > ghost > latecomer > tiny | pairs whose expected figures are within 10% of each other are not ranked |
| ✅ pass | pool 6 (query path): `dashboards` dominates | `dashboards` first, with at least 50% | dashboards 92.5%; rules 4.2%; cardinal 1.7%; steady 1.2% | rank 1 and ≥50% |
| ✅ pass | pool 7 (ruler CPU): `rules` dominates | `rules` first, with at least 50% | rules 90.3%; steady 3.2%; cardinal 1.7%; dashboards 1.7% | rank 1 and ≥50% |
| ✅ pass | ruler evaluation mode detected correctly | remote | pool 6 (query path): remote, pool 7 (ruler CPU): remote | exact |
| ✅ pass | rule outputs: `cardinal` record: series | 10 series over 1 record: names (present) | 10 series (1 output metrics) | exact |
| ✅ pass | rule outputs: `churn` record: series | 11 series over 2 record: names (present) | 11 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `dashboards` record: series | 11 series over 2 record: names (present) | 11 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `ghost` record: series | 0 series over 2 record: names (absent) | 0 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `latecomer` record: series | 11 series over 2 record: names (present) | 11 series (2 output metrics) | exact |
| ✅ pass | rule outputs: `rules` record: series | 660 series over 120 record: names (present) | 660 series (120 output metrics) | exact |
| ✅ pass | rule outputs: `steady` record: series | 22 series over 4 record: names (present) | 22 series (4 output metrics) | exact |
| ✅ pass | rule outputs: `tiny` record: series | 0 series over 0 record: names (absent) | 0 series (0 output metrics) | exact |
| ✅ pass | write path detected correctly | ingest storage | ingest storage | exact |
| ✅ pass | counter pools stay sane across ingester-rolling-restart | every per-tenant figure finite and ≥ 0 | all finite and ≥ 0 | exact |

<details><summary>Pool 1 per tenant: expected vs measured</summary>

| tenant | expected (time-weighted band) | promcost | points present (Mimir / model) |
|---|---:|---:|---:|
| `cardinal` | 20,010 – 20,010 | 20,010 | 60 / 60 of 60 |
| `churn` | 5,811 – 6,211 | 5,711 | 60 / 60 of 60 |
| `dashboards` | 4,011 – 4,011 | 4,011 | 60 / 60 of 60 |
| `ghost` | 2,209 – 2,259 | 2,651 | 50 / 50 of 60 |
| `latecomer` | 1,676 – 1,709 | 2,011 | 51 / 51 of 60 |
| `monitoring` | not controlled | 106,418 | 60 / — of 60 |
| `rules` | 3,660 – 3,660 | 3,660 | 60 / 60 of 60 |
| `steady` | 30,022 – 30,022 | 30,022 | 60 / 60 of 60 |
| `tiny` | 20.00 – 20.00 | 20.00 | 60 / 60 of 60 |

</details>

<details><summary>pool 6 (query path): shares (chunk bytes fetched)</summary>

| tenant | driver | share |
|---|---:|---:|
| `dashboards` | 8,045,325,574 | 92.55% |
| `rules` | 362,244,593 | 4.17% |
| `cardinal` | 148,862,770 | 1.71% |
| `steady` | 104,440,598 | 1.20% |
| `latecomer` | 14,360,552 | 0.17% |
| `ghost` | 10,233,802 | 0.12% |
| `churn` | 7,136,094 | 0.08% |
| `monitoring` | 500,836 | 0.01% |
| `tiny` | 252,458 | 0.00% |

</details>

<details><summary>pool 7 (ruler CPU): shares (rule-evaluation seconds)</summary>

| tenant | driver | share |
|---|---:|---:|
| `rules` | 677 | 90.29% |
| `steady` | 23.72 | 3.16% |
| `cardinal` | 12.88 | 1.72% |
| `dashboards` | 12.68 | 1.69% |
| `churn` | 9.39 | 1.25% |
| `latecomer` | 8.25 | 1.10% |
| `ghost` | 5.04 | 0.67% |
| `tiny` | 0.91 | 0.12% |

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
  - subquery points with pipeline data: 60; points this tenant was present (model): 50; (Mimir): 50
  - matches the presence-weighted average [2,651, 2,711]: the figure is averaged over only the points where the tenant had series, not over the whole window
- **pool 1: `latecomer` active series (after the replication divisor)** — the window overlaps ingester-rolling-restart: during an ingester rollout the sum-then-average query reads low while a replica is down (measured 9% on dev/mimir-k8s), so the band is widened to ±15%
  - subquery points with pipeline data: 60; points this tenant was present (model): 51; (Mimir): 51
  - matches the presence-weighted average [1,971, 2,011]: the figure is averaged over only the points where the tenant had series, not over the whole window
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
- **write path detected correctly** — the environment's architecture is known
- **counter pools stay sane across ingester-rolling-restart** — increase() compensates for counter resets; a negative or infinite figure would mean it did not

</details>

## Window 30m, ending 2026-10-22 03:26 UTC

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
| ❌ fail | pool 1: `ghost` active series (after the replication divisor) | 1,407–1,507 | 2,111 | band [1,407, 1,507] ±1% |
| ✅ pass | pool 1: `latecomer` active series (after the replication divisor) | 2,011–2,011 | 2,011 | band [2,011, 2,011] ±1% |
| ✅ pass | pool 1: `rules` active series (after the replication divisor) | 3,660–3,660 | 3,660 | band [3,660, 3,660] ±1% |
| ✅ pass | pool 1: `steady` active series (after the replication divisor) | 30,022–30,022 | 30,022 | band [30,022, 30,022] ±1% |
| ✅ pass | pool 1: `tiny` active series (after the replication divisor) | 20.00–20.00 | 20.00 | band [20.00, 20.00] ±1% |
| ❌ fail | pool 1: tenant rank order matches the workload shapes | steady > cardinal > churn > dashboards > rules > latecomer > ghost > tiny | steady > cardinal > churn > dashboards > rules > ghost > latecomer > tiny | pairs whose expected figures are within 10% of each other are not ranked |
| ✅ pass | pool 6 (query path): `dashboards` dominates | `dashboards` first, with at least 50% | dashboards 94.2%; rules 2.7%; cardinal 1.8%; steady 0.9% | rank 1 and ≥50% |
| ✅ pass | pool 7 (ruler CPU): `rules` dominates | `rules` first, with at least 50% | rules 90.4%; steady 3.2%; cardinal 1.8%; dashboards 1.7% | rank 1 and ≥50% |
| ✅ pass | ruler evaluation mode detected correctly | remote | pool 6 (query path): remote, pool 7 (ruler CPU): remote | exact |
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
| `cardinal` | 20,010 – 20,010 | 20,010 | 30 / 30 of 30 |
| `churn` | 5,811 – 6,211 | 6,011 | 30 / 30 of 30 |
| `dashboards` | 4,011 – 4,011 | 4,011 | 30 / 30 of 30 |
| `ghost` | 1,407 – 1,507 | 2,111 | 20 / 20 of 30 |
| `latecomer` | 2,011 – 2,011 | 2,011 | 30 / 30 of 30 |
| `monitoring` | not controlled | 105,476 | 30 / — of 30 |
| `rules` | 3,660 – 3,660 | 3,660 | 30 / 30 of 30 |
| `steady` | 30,022 – 30,022 | 30,022 | 30 / 30 of 30 |
| `tiny` | 20.00 – 20.00 | 20.00 | 30 / 30 of 30 |

</details>

<details><summary>pool 6 (query path): shares (chunk bytes fetched)</summary>

| tenant | driver | share |
|---|---:|---:|
| `dashboards` | 4,468,918,585 | 94.17% |
| `rules` | 130,465,074 | 2.75% |
| `cardinal` | 87,752,809 | 1.85% |
| `steady` | 43,200,569 | 0.91% |
| `latecomer` | 10,987,683 | 0.23% |
| `churn` | 3,606,698 | 0.08% |
| `monitoring` | 316,115 | 0.01% |
| `tiny` | 153,407 | 0.00% |
| `ghost` | 0.00 | 0.00% |

</details>

<details><summary>pool 7 (ruler CPU): shares (rule-evaluation seconds)</summary>

| tenant | driver | share |
|---|---:|---:|
| `rules` | 336 | 90.42% |
| `steady` | 11.86 | 3.19% |
| `cardinal` | 6.60 | 1.78% |
| `dashboards` | 6.35 | 1.71% |
| `latecomer` | 4.97 | 1.34% |
| `churn` | 4.64 | 1.25% |
| `ghost` | 0.76 | 0.21% |
| `tiny` | 0.46 | 0.12% |

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
  - subquery points with pipeline data: 30; points this tenant was present (model): 20; (Mimir): 20
  - matches the presence-weighted average [2,111, 2,261]: the figure is averaged over only the points where the tenant had series, not over the whole window
- **pool 1: `latecomer` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 30; points this tenant was present (model): 30; (Mimir): 30
- **pool 1: `rules` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 30; points this tenant was present (model): 30; (Mimir): 30
- **pool 1: `steady` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 30; points this tenant was present (model): 30; (Mimir): 30
- **pool 1: `tiny` active series (after the replication divisor)** — the ingester recomputes active series every 1m and the gauge is scraped every 15s, so each point of the average describes the ingester up to 75s earlier, and idle series are purged up to 1m late; the band is the model evaluated at both extremes, plus ±1% for anything else
  - subquery points with pipeline data: 30; points this tenant was present (model): 30; (Mimir): 30
- **pool 1: tenant rank order matches the workload shapes** — a near-tie can legitimately flip on gauge lag; everything else is a difference of shape
  - latecomer (expected 2,011, got 2,011) should outrank ghost (expected 1,457, got 2,111)
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
- **write path detected correctly** — the environment's architecture is known

</details>

## Window 10m, ending 2026-10-22 03:26 UTC

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
| ✅ pass | pool 6 (query path): `dashboards` dominates | `dashboards` first, with at least 50% | dashboards 94.8%; rules 2.3%; cardinal 2.0%; steady 0.6% | rank 1 and ≥50% |
| ✅ pass | pool 7 (ruler CPU): `rules` dominates | `rules` first, with at least 50% | rules 90.1%; steady 3.3%; dashboards 1.8%; cardinal 1.8% | rank 1 and ≥50% |
| ✅ pass | ruler evaluation mode detected correctly | remote | pool 6 (query path): remote, pool 7 (ruler CPU): remote | exact |
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
| `churn` | 5,811 – 6,211 | 6,011 | 10 / 10 of 10 |
| `dashboards` | 4,011 – 4,011 | 4,011 | 10 / 10 of 10 |
| `ghost` | 0.00 – 0.00 | not measured | 0 / 0 of 10 |
| `latecomer` | 2,011 – 2,011 | 2,011 | 10 / 10 of 10 |
| `monitoring` | not controlled | 104,600 | 10 / — of 10 |
| `rules` | 3,660 – 3,660 | 3,660 | 10 / 10 of 10 |
| `steady` | 30,022 – 30,022 | 30,022 | 10 / 10 of 10 |
| `tiny` | 20.00 – 20.00 | 20.00 | 10 / 10 of 10 |

</details>

<details><summary>pool 6 (query path): shares (chunk bytes fetched)</summary>

| tenant | driver | share |
|---|---:|---:|
| `dashboards` | 1,575,464,049 | 94.77% |
| `rules` | 37,919,948 | 2.28% |
| `cardinal` | 33,297,018 | 2.00% |
| `steady` | 10,106,589 | 0.61% |
| `latecomer` | 4,030,142 | 0.24% |
| `churn` | 1,255,596 | 0.08% |
| `monitoring` | 319,849 | 0.02% |
| `tiny` | 58,914 | 0.00% |
| `ghost` | 0.00 | 0.00% |

</details>

<details><summary>pool 7 (ruler CPU): shares (rule-evaluation seconds)</summary>

| tenant | driver | share |
|---|---:|---:|
| `rules` | 110 | 90.06% |
| `steady` | 4.09 | 3.34% |
| `dashboards` | 2.25 | 1.84% |
| `cardinal` | 2.22 | 1.82% |
| `latecomer` | 1.71 | 1.39% |
| `churn` | 1.51 | 1.23% |
| `ghost` | 0.25 | 0.20% |
| `tiny` | 0.14 | 0.12% |

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
