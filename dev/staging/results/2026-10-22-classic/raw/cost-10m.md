# promcost tenant cost report

## Ingester memory

Average over the last 10m, ending 2026-10-22T08:52:45Z. Pool: 3 ingesters, 360.00 EUR/month.

**`monitoring` uses 63.1% of ingester memory** — the equivalent of 1.9 of your 3 ingesters, 227.19 EUR/month.

| Tenant | Active series | Share | ≈ ingesters | EUR/month |
|---|---:|---:|---:|---:|
| monitoring | 112,358 | 63.1% | 1.89 | 227.19 |
| steady | 30,022 | 16.9% | 0.51 | 60.71 |
| cardinal | 20,010 | 11.2% | 0.34 | 40.46 |
| churn | 5,944 | 3.3% | 0.10 | 12.02 |
| dashboards | 4,011 | 2.3% | 0.07 | 8.11 |
| rules | 3,660 | 2.1% | 0.06 | 7.40 |
| latecomer | 2,011 | 1.1% | 0.03 | 4.07 |
| tiny | 20 | 0.0% | 0.00 | 0.04 |
| ghost | not measured | not measured | not measured | not measured |
| **Total** | **178,036** | **100%** | **3** | **360.00** |

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_ingester_active_series (active series), averaged over 10m` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `sum_over_time((sum by (user) (cortex_ingester_active_series unless on (cluster, namespace, job) cortex_partition_ring_partitions))[10m:1m]) / scalar(count_over_time((sum(sum by (user) (cortex_ingester_active_series unless on (cluster, namespace, job) cortex_partition_ring_partitions)))[10m:1m]))` | `Mimir metrics (queried as tenant monitoring)` |
| averaging | `averaged over the whole window; points where the tenant had no series count as 0; points where Mimir's own metrics were missing are excluded (10 of 10)` | `count_over_time((sum(sum by (user) (cortex_ingester_active_series unless on (cluster, namespace, job) cortex_partition_ring_partitions)))[10m:1m])` |
| write path | `classic` | `detected from which driver query returned data` |
| replication factor | `3` | `max(max_over_time(cortex_distributor_replication_factor[10m]))` |
| ingester replicas | `3` | `promcost.yaml inventory` |
| price | `120.00 EUR per ingester-month × 3 = 360.00 EUR/month` | `promcost.yaml inventory` |

## Query path

Total over the last 10m, ending 2026-10-22T08:52:45Z. Pool: 1 × query-frontend, 2 × querier, 160.00 EUR/month.

**`dashboards` uses 95.1% of query path**, 152.23 EUR/month.

| Tenant | Chunk bytes fetched | Share | EUR/month |
|---|---:|---:|---:|
| dashboards | 1.3 GiB | 95.1% | 152.23 |
| rules | 31.0 MiB | 2.3% | 3.65 |
| cardinal | 20.8 MiB | 1.5% | 2.45 |
| steady | 9.2 MiB | 0.7% | 1.08 |
| latecomer | 3.8 MiB | 0.3% | 0.45 |
| churn | 1.2 MiB | 0.1% | 0.14 |
| tiny | 36.6 KiB | 0.0% | 0.00 |
| ghost | 0 B | 0.0% | 0.00 |
| **Total** | **1.3 GiB** | **100%** | **160.00** |

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_query_fetched_chunk_bytes_total (chunk bytes fetched), total increase over 10m` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `sum by (user) (increase(cortex_query_fetched_chunk_bytes_total[10m]))` | `Mimir metrics (queried as tenant monitoring)` |
| driver preference | `fetched volume over time: cortex_query_fetched_chunk_bytes_total → cortex_query_samples_processed_total → cortex_query_fetched_series_total → cortex_query_seconds_total; the first with data is used` | `ADR 0003 pool 6, ADR 0002 decision 8` |
| rule queries | `included — rules are evaluated remotely, so every tenant's rule queries are in this pool alongside its user queries, and no label separates the two (inferred: the ruler's job (job=~".*ruler") evaluated rules but has no cortex_distributor_query_duration_seconds_count data, which a ruler evaluating locally always records, so its queries go to the query-frontend)` | `Mimir metrics (queried as tenant monitoring)` |
| query-frontend price | `40.00 EUR per query-frontend-month × 1 = 40.00 EUR/month` | `promcost.yaml inventory` |
| querier price | `60.00 EUR per querier-month × 2 = 120.00 EUR/month` | `promcost.yaml inventory` |

## Ruler CPU

Total over the last 10m, ending 2026-10-22T08:52:45Z. Pool: 1 ruler, 60.00 EUR/month.

**`rules` uses 87.3% of ruler CPU** — the equivalent of 0.9 of your 1 ruler, 52.36 EUR/month.

| Tenant | Rule-evaluation seconds | Share | ≈ rulers | EUR/month |
|---|---:|---:|---:|---:|
| rules | 51.7 | 87.3% | 0.87 | 52.36 |
| cardinal | 2.2 | 3.7% | 0.04 | 2.24 |
| steady | 2.1 | 3.6% | 0.04 | 2.17 |
| dashboards | 1.5 | 2.6% | 0.03 | 1.55 |
| churn | 0.6 | 1.0% | 0.01 | 0.60 |
| latecomer | 0.6 | 1.0% | 0.01 | 0.59 |
| ghost | 0.3 | 0.5% | 0.01 | 0.31 |
| tiny | 0.2 | 0.3% | 0.00 | 0.19 |
| **Total** | **59.2** | **100%** | **1** | **60.00** |

**Read before quoting:**

- rules are evaluated remotely, so this time is mostly the ruler waiting on the query path, not ruler CPU: it shows how long each tenant's rules occupy the ruler, and the work behind it is already in the query path pool. It also depends on load — a tenant's rules run slower when a neighbour loads the cluster

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_prometheus_rule_evaluation_duration_seconds_sum (rule-evaluation seconds), total increase over 10m` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `sum by (user) (increase(cortex_prometheus_rule_evaluation_duration_seconds_sum[10m]))` | `Mimir metrics (queried as tenant monitoring)` |
| rule evaluation | `remote (inferred: the ruler's job (job=~".*ruler") evaluated rules but has no cortex_distributor_query_duration_seconds_count data, which a ruler evaluating locally always records, so its queries go to the query-frontend): the rule queries' work is priced in the query path` | `Mimir metrics (queried as tenant monitoring)` |
| ruler replicas | `1` | `promcost.yaml inventory` |
| price | `60.00 EUR per ruler-month × 1 = 60.00 EUR/month` | `promcost.yaml inventory` |

## Cost across pools

**`monitoring` is 39.2% of the cost of the priced pools** (ingester memory, query path, ruler CPU). What fraction of your platform cost that is stays unknown until `inventory.platform_cost_month` is set.

| Tenant | Ingester memory EUR/month | Query path EUR/month | Ruler CPU EUR/month | Total EUR/month | Share of priced cost |
|---|---:|---:|---:|---:|---:|
| monitoring | 227.19 | not measured | not measured | ≥ 227.19 | ≥ 39.2% |
| dashboards | 8.11 | 152.23 | 1.55 | 161.89 | 27.9% |
| steady | 60.71 | 1.08 | 2.17 | 63.96 | 11.0% |
| rules | 7.40 | 3.65 | 52.36 | 63.40 | 10.9% |
| cardinal | 40.46 | 2.45 | 2.24 | 45.15 | 7.8% |
| churn | 12.02 | 0.14 | 0.60 | 12.76 | 2.2% |
| latecomer | 4.07 | 0.45 | 0.59 | 5.10 | 0.9% |
| ghost | not measured | 0.00 | 0.31 | ≥ 0.31 | ≥ 0.1% |
| tiny | 0.04 | 0.00 | 0.19 | 0.23 | 0.0% |

**Read before quoting:**

- rules are evaluated remotely, so a tenant's rule queries drive its share of both the query path (the work) and the ruler (the time spent waiting for it). Each pool's cost is split once, so nothing is counted twice, but a rule-heavy tenant's total weighs the same queries in both
- a tenant marked ≥ was not measured in every priced pool: what it would owe there is unknown, not zero, and that pool's cost is shared out among the tenants that were measured, so its figure is a lower bound and theirs are inflated

Not modelled yet: ingester write path, object storage, store-gateway, compactor.

Generated 2026-10-22T08:52:45Z.
