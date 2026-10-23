# promcost tenant cost report

## Ingester memory

Average over the last 30m, ending 2026-10-22T08:52:44Z. Pool: 3 ingesters, 360.00 EUR/month.

**`monitoring` uses 62.8% of ingester memory** — the equivalent of 1.9 of your 3 ingesters, 226.15 EUR/month.

| Tenant | Active series | Share | ≈ ingesters | EUR/month |
|---|---:|---:|---:|---:|
| monitoring | 112,386 | 62.8% | 1.88 | 226.15 |
| steady | 30,022 | 16.8% | 0.50 | 60.41 |
| cardinal | 20,010 | 11.2% | 0.34 | 40.27 |
| churn | 5,944 | 3.3% | 0.10 | 11.96 |
| dashboards | 4,011 | 2.2% | 0.07 | 8.07 |
| rules | 3,660 | 2.0% | 0.06 | 7.36 |
| latecomer | 2,011 | 1.1% | 0.03 | 4.05 |
| ghost | 838 | 0.5% | 0.01 | 1.69 |
| tiny | 20 | 0.0% | 0.00 | 0.04 |
| **Total** | **178,903** | **100%** | **3** | **360.00** |

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_ingester_active_series (active series), averaged over 30m` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `sum_over_time((sum by (user) (cortex_ingester_active_series unless on (cluster, namespace, job) cortex_partition_ring_partitions))[30m:1m]) / scalar(count_over_time((sum(sum by (user) (cortex_ingester_active_series unless on (cluster, namespace, job) cortex_partition_ring_partitions)))[30m:1m]))` | `Mimir metrics (queried as tenant monitoring)` |
| averaging | `averaged over the whole window; points where the tenant had no series count as 0; points where Mimir's own metrics were missing are excluded (30 of 30)` | `count_over_time((sum(sum by (user) (cortex_ingester_active_series unless on (cluster, namespace, job) cortex_partition_ring_partitions)))[30m:1m])` |
| write path | `classic` | `detected from which driver query returned data` |
| replication factor | `3` | `max(max_over_time(cortex_distributor_replication_factor[30m]))` |
| ingester replicas | `3` | `promcost.yaml inventory` |
| price | `120.00 EUR per ingester-month × 3 = 360.00 EUR/month` | `promcost.yaml inventory` |

## Query path

Total over the last 30m, ending 2026-10-22T08:52:44Z. Pool: 1 × query-frontend, 2 × querier, 160.00 EUR/month.

**`dashboards` uses 94.9% of query path**, 151.77 EUR/month.

| Tenant | Chunk bytes fetched | Share | EUR/month |
|---|---:|---:|---:|
| dashboards | 3.5 GiB | 94.9% | 151.77 |
| rules | 95.9 MiB | 2.5% | 4.03 |
| cardinal | 52.2 MiB | 1.4% | 2.20 |
| steady | 33.1 MiB | 0.9% | 1.39 |
| latecomer | 10.9 MiB | 0.3% | 0.46 |
| churn | 3.4 MiB | 0.1% | 0.14 |
| tiny | 89.0 KiB | 0.0% | 0.00 |
| ghost | 0 B | 0.0% | 0.00 |
| **Total** | **3.7 GiB** | **100%** | **160.00** |

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

Total over the last 30m, ending 2026-10-22T08:52:44Z. Pool: 1 ruler, 60.00 EUR/month.

**`rules` uses 87.1% of ruler CPU** — the equivalent of 0.9 of your 1 ruler, 52.29 EUR/month.

| Tenant | Rule-evaluation seconds | Share | ≈ rulers | EUR/month |
|---|---:|---:|---:|---:|
| rules | 156.5 | 87.1% | 0.87 | 52.29 |
| steady | 7.0 | 3.9% | 0.04 | 2.34 |
| cardinal | 6.2 | 3.5% | 0.03 | 2.08 |
| dashboards | 5.0 | 2.8% | 0.03 | 1.66 |
| latecomer | 1.8 | 1.0% | 0.01 | 0.59 |
| churn | 1.7 | 0.9% | 0.01 | 0.56 |
| ghost | 0.9 | 0.5% | 0.01 | 0.31 |
| tiny | 0.5 | 0.3% | 0.00 | 0.17 |
| **Total** | **179.6** | **100%** | **1** | **60.00** |

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

**`monitoring` is 39.0% of the cost of the priced pools** (ingester memory, query path, ruler CPU). What fraction of your platform cost that is stays unknown until `inventory.platform_cost_month` is set.

| Tenant | Ingester memory EUR/month | Query path EUR/month | Ruler CPU EUR/month | Total EUR/month | Share of priced cost |
|---|---:|---:|---:|---:|---:|
| monitoring | 226.15 | not measured | not measured | ≥ 226.15 | ≥ 39.0% |
| dashboards | 8.07 | 151.77 | 1.66 | 161.50 | 27.8% |
| steady | 60.41 | 1.39 | 2.34 | 64.15 | 11.1% |
| rules | 7.36 | 4.03 | 52.29 | 63.69 | 11.0% |
| cardinal | 40.27 | 2.20 | 2.08 | 44.54 | 7.7% |
| churn | 11.96 | 0.14 | 0.56 | 12.67 | 2.2% |
| latecomer | 4.05 | 0.46 | 0.59 | 5.10 | 0.9% |
| ghost | 1.69 | 0.00 | 0.31 | 1.99 | 0.3% |
| tiny | 0.04 | 0.00 | 0.17 | 0.21 | 0.0% |

**Read before quoting:**

- rules are evaluated remotely, so a tenant's rule queries drive its share of both the query path (the work) and the ruler (the time spent waiting for it). Each pool's cost is split once, so nothing is counted twice, but a rule-heavy tenant's total weighs the same queries in both
- a tenant marked ≥ was not measured in every priced pool: what it would owe there is unknown, not zero, and that pool's cost is shared out among the tenants that were measured, so its figure is a lower bound and theirs are inflated

Not modelled yet: ingester write path, object storage, store-gateway, compactor.

Generated 2026-10-22T08:52:44Z.
