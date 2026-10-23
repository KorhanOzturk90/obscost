# promcost tenant cost report

## Ingester memory

Average over the last 30m, ending 2026-10-22T07:20:04Z. Pool: 3 ingesters, 360.00 EUR/month.

**`monitoring` uses 61.8% of ingester memory** — the equivalent of 1.9 of your 3 ingesters, 222.48 EUR/month.

| Tenant | Active series | Share | ≈ ingesters | EUR/month |
|---|---:|---:|---:|---:|
| monitoring | 107,179 | 61.8% | 1.85 | 222.48 |
| steady | 30,022 | 17.3% | 0.52 | 62.32 |
| cardinal | 20,010 | 11.5% | 0.35 | 41.54 |
| churn | 6,011 | 3.5% | 0.10 | 12.48 |
| dashboards | 4,011 | 2.3% | 0.07 | 8.33 |
| rules | 3,660 | 2.1% | 0.06 | 7.60 |
| latecomer | 2,011 | 1.2% | 0.03 | 4.17 |
| ghost | 504 | 0.3% | 0.01 | 1.05 |
| tiny | 20 | 0.0% | 0.00 | 0.04 |
| **Total** | **173,428** | **100%** | **3** | **360.00** |

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_ingester_active_series (active series), averaged over 30m` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `sum_over_time((sum by (user) (max by (ingester_id, user) (label_replace(cortex_ingester_active_series and on (cluster, namespace, job) cortex_partition_ring_partitions, "ingester_id", "$1", "pod", ".*?-(rc-[0-9]+-[0-9]+\|[0-9]+)$"))))[30m:1m]) / scalar(count_over_time((sum(sum by (user) (max by (ingester_id, user) (label_replace(cortex_ingester_active_series and on (cluster, namespace, job) cortex_partition_ring_partitions, "ingester_id", "$1", "pod", ".*?-(rc-[0-9]+-[0-9]+\|[0-9]+)$")))))[30m:1m]))` | `Mimir metrics (queried as tenant monitoring)` |
| averaging | `averaged over the whole window; points where the tenant had no series count as 0; points where Mimir's own metrics were missing are excluded (30 of 30)` | `count_over_time((sum(sum by (user) (max by (ingester_id, user) (label_replace(cortex_ingester_active_series and on (cluster, namespace, job) cortex_partition_ring_partitions, "ingester_id", "$1", "pod", ".*?-(rc-[0-9]+-[0-9]+\|[0-9]+)$")))))[30m:1m])` |
| write path | `ingest storage` | `detected from which driver query returned data` |
| replication factor | `1` | `ingest storage: one owner per partition, zone replicas collapsed with max, so there is nothing to divide` |
| ingester replicas | `3` | `promcost.yaml inventory` |
| price | `120.00 EUR per ingester-month × 3 = 360.00 EUR/month` | `promcost.yaml inventory` |

## Query path

Total over the last 30m, ending 2026-10-22T07:20:04Z. Pool: 1 × query-frontend, 2 × querier, 160.00 EUR/month.

**`dashboards` uses 97.3% of query path**, 155.74 EUR/month.

| Tenant | Chunk bytes fetched | Share | EUR/month |
|---|---:|---:|---:|
| dashboards | 7.7 GiB | 97.3% | 155.74 |
| rules | 98.0 MiB | 1.2% | 1.94 |
| cardinal | 73.4 MiB | 0.9% | 1.45 |
| steady | 31.4 MiB | 0.4% | 0.62 |
| latecomer | 9.5 MiB | 0.1% | 0.19 |
| churn | 3.4 MiB | 0.0% | 0.07 |
| tiny | 85.3 KiB | 0.0% | 0.00 |
| ghost | 0 B | 0.0% | 0.00 |
| **Total** | **7.9 GiB** | **100%** | **160.00** |

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_query_fetched_chunk_bytes_total (chunk bytes fetched), total increase over 30m` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `sum by (user) (increase(cortex_query_fetched_chunk_bytes_total[30m]))` | `Mimir metrics (queried as tenant monitoring)` |
| driver preference | `fetched volume over time: cortex_query_fetched_chunk_bytes_total → cortex_query_samples_processed_total → cortex_query_fetched_series_total → cortex_query_seconds_total; the first with data is used` | `ADR 0003 pool 6, ADR 0002 decision 8` |
| rule queries | `included — rules are evaluated remotely, so every tenant's rule queries are in this pool alongside its user queries, and no label separates the two (inferred: the ruler's job (job=~".*ruler") evaluated rules but has no cortex_distributor_query_duration_seconds_count data, which a ruler evaluating locally always records, so its queries go to the query-frontend)` | `Mimir metrics (queried as tenant monitoring)` |
| query-frontend price | `40.00 EUR per query-frontend-month × 1 = 40.00 EUR/month` | `promcost.yaml inventory` |
| querier price | `60.00 EUR per querier-month × 2 = 120.00 EUR/month` | `promcost.yaml inventory` |

## Ruler CPU

Total over the last 30m, ending 2026-10-22T07:20:04Z. Pool: 1 ruler, 60.00 EUR/month.

**`rules` uses 90.7% of ruler CPU** — the equivalent of 0.9 of your 1 ruler, 54.42 EUR/month.

| Tenant | Rule-evaluation seconds | Share | ≈ rulers | EUR/month |
|---|---:|---:|---:|---:|
| rules | 329.0 | 90.7% | 0.91 | 54.42 |
| steady | 11.0 | 3.0% | 0.03 | 1.82 |
| cardinal | 6.1 | 1.7% | 0.02 | 1.01 |
| dashboards | 5.7 | 1.6% | 0.02 | 0.95 |
| latecomer | 5.0 | 1.4% | 0.01 | 0.83 |
| churn | 4.7 | 1.3% | 0.01 | 0.78 |
| ghost | 0.8 | 0.2% | 0.00 | 0.13 |
| tiny | 0.5 | 0.1% | 0.00 | 0.08 |
| **Total** | **362.7** | **100%** | **1** | **60.00** |

**Read before quoting:**

- rules are evaluated remotely, so this time is mostly the ruler waiting on the query path, not ruler CPU: it shows how long each tenant's rules occupy the ruler, and the work behind it is already in the query path pool. It also depends on load — a tenant's rules run slower when a neighbour loads the cluster

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_prometheus_rule_evaluation_duration_seconds_sum (rule-evaluation seconds), total increase over 30m` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `sum by (user) (increase(cortex_prometheus_rule_evaluation_duration_seconds_sum[30m]))` | `Mimir metrics (queried as tenant monitoring)` |
| rule evaluation | `remote (inferred: the ruler's job (job=~".*ruler") evaluated rules but has no cortex_distributor_query_duration_seconds_count data, which a ruler evaluating locally always records, so its queries go to the query-frontend): the rule queries' work is priced in the query path` | `Mimir metrics (queried as tenant monitoring)` |
| ruler replicas | `1` | `promcost.yaml inventory` |
| price | `60.00 EUR per ruler-month × 1 = 60.00 EUR/month` | `promcost.yaml inventory` |

## Cost across pools

**`monitoring` is 38.4% of the cost of the priced pools** (ingester memory, query path, ruler CPU). What fraction of your platform cost that is stays unknown until `inventory.platform_cost_month` is set.

| Tenant | Ingester memory EUR/month | Query path EUR/month | Ruler CPU EUR/month | Total EUR/month | Share of priced cost |
|---|---:|---:|---:|---:|---:|
| monitoring | 222.48 | not measured | not measured | ≥ 222.48 | ≥ 38.4% |
| dashboards | 8.33 | 155.74 | 0.95 | 165.01 | 28.4% |
| steady | 62.32 | 0.62 | 1.82 | 64.76 | 11.2% |
| rules | 7.60 | 1.94 | 54.42 | 63.95 | 11.0% |
| cardinal | 41.54 | 1.45 | 1.01 | 44.00 | 7.6% |
| churn | 12.48 | 0.07 | 0.78 | 13.32 | 2.3% |
| latecomer | 4.17 | 0.19 | 0.83 | 5.19 | 0.9% |
| ghost | 1.05 | 0.00 | 0.13 | 1.18 | 0.2% |
| tiny | 0.04 | 0.00 | 0.08 | 0.12 | 0.0% |

**Read before quoting:**

- rules are evaluated remotely, so a tenant's rule queries drive its share of both the query path (the work) and the ruler (the time spent waiting for it). Each pool's cost is split once, so nothing is counted twice, but a rule-heavy tenant's total weighs the same queries in both
- a tenant marked ≥ was not measured in every priced pool: what it would owe there is unknown, not zero, and that pool's cost is shared out among the tenants that were measured, so its figure is a lower bound and theirs are inflated

Not modelled yet: ingester write path, object storage, store-gateway, compactor.

Generated 2026-10-22T07:20:04Z.
