# promcost tenant cost report

## Ingester memory

Average over the last 10m, ending 2026-10-22T07:20:05Z. Pool: 3 ingesters, 360.00 EUR/month.

**`monitoring` uses 62.0% of ingester memory** — the equivalent of 1.9 of your 3 ingesters, 223.11 EUR/month.

| Tenant | Active series | Share | ≈ ingesters | EUR/month |
|---|---:|---:|---:|---:|
| monitoring | 107,158 | 62.0% | 1.86 | 223.11 |
| steady | 30,022 | 17.4% | 0.52 | 62.51 |
| cardinal | 20,010 | 11.6% | 0.35 | 41.66 |
| churn | 6,011 | 3.5% | 0.10 | 12.52 |
| dashboards | 4,011 | 2.3% | 0.07 | 8.35 |
| rules | 3,660 | 2.1% | 0.06 | 7.62 |
| latecomer | 2,011 | 1.2% | 0.03 | 4.19 |
| tiny | 20 | 0.0% | 0.00 | 0.04 |
| ghost | not measured | not measured | not measured | not measured |
| **Total** | **172,903** | **100%** | **3** | **360.00** |

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_ingester_active_series (active series), averaged over 10m` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `sum_over_time((sum by (user) (max by (ingester_id, user) (label_replace(cortex_ingester_active_series and on (cluster, namespace, job) cortex_partition_ring_partitions, "ingester_id", "$1", "pod", ".*?-(rc-[0-9]+-[0-9]+\|[0-9]+)$"))))[10m:1m]) / scalar(count_over_time((sum(sum by (user) (max by (ingester_id, user) (label_replace(cortex_ingester_active_series and on (cluster, namespace, job) cortex_partition_ring_partitions, "ingester_id", "$1", "pod", ".*?-(rc-[0-9]+-[0-9]+\|[0-9]+)$")))))[10m:1m]))` | `Mimir metrics (queried as tenant monitoring)` |
| averaging | `averaged over the whole window; points where the tenant had no series count as 0; points where Mimir's own metrics were missing are excluded (10 of 10)` | `count_over_time((sum(sum by (user) (max by (ingester_id, user) (label_replace(cortex_ingester_active_series and on (cluster, namespace, job) cortex_partition_ring_partitions, "ingester_id", "$1", "pod", ".*?-(rc-[0-9]+-[0-9]+\|[0-9]+)$")))))[10m:1m])` |
| write path | `ingest storage` | `detected from which driver query returned data` |
| replication factor | `1` | `ingest storage: one owner per partition, zone replicas collapsed with max, so there is nothing to divide` |
| ingester replicas | `3` | `promcost.yaml inventory` |
| price | `120.00 EUR per ingester-month × 3 = 360.00 EUR/month` | `promcost.yaml inventory` |

## Query path

Total over the last 10m, ending 2026-10-22T07:20:05Z. Pool: 1 × query-frontend, 2 × querier, 160.00 EUR/month.

**`dashboards` uses 97.6% of query path**, 156.10 EUR/month.

| Tenant | Chunk bytes fetched | Share | EUR/month |
|---|---:|---:|---:|
| dashboards | 2.7 GiB | 97.6% | 156.10 |
| cardinal | 28.2 MiB | 1.0% | 1.61 |
| rules | 27.6 MiB | 1.0% | 1.58 |
| steady | 7.6 MiB | 0.3% | 0.44 |
| latecomer | 3.5 MiB | 0.1% | 0.20 |
| churn | 1.2 MiB | 0.0% | 0.07 |
| tiny | 15.7 KiB | 0.0% | 0.00 |
| ghost | 0 B | 0.0% | 0.00 |
| **Total** | **2.7 GiB** | **100%** | **160.00** |

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

Total over the last 10m, ending 2026-10-22T07:20:05Z. Pool: 1 ruler, 60.00 EUR/month.

**`rules` uses 90.6% of ruler CPU** — the equivalent of 0.9 of your 1 ruler, 54.38 EUR/month.

| Tenant | Rule-evaluation seconds | Share | ≈ rulers | EUR/month |
|---|---:|---:|---:|---:|
| rules | 110.8 | 90.6% | 0.91 | 54.38 |
| steady | 3.5 | 2.9% | 0.03 | 1.73 |
| cardinal | 2.2 | 1.8% | 0.02 | 1.09 |
| dashboards | 1.9 | 1.5% | 0.02 | 0.91 |
| latecomer | 1.7 | 1.4% | 0.01 | 0.86 |
| churn | 1.7 | 1.4% | 0.01 | 0.83 |
| ghost | 0.3 | 0.2% | 0.00 | 0.13 |
| tiny | 0.2 | 0.1% | 0.00 | 0.08 |
| **Total** | **122.3** | **100%** | **1** | **60.00** |

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

**`monitoring` is 38.5% of the cost of the priced pools** (ingester memory, query path, ruler CPU). What fraction of your platform cost that is stays unknown until `inventory.platform_cost_month` is set.

| Tenant | Ingester memory EUR/month | Query path EUR/month | Ruler CPU EUR/month | Total EUR/month | Share of priced cost |
|---|---:|---:|---:|---:|---:|
| monitoring | 223.11 | not measured | not measured | ≥ 223.11 | ≥ 38.5% |
| dashboards | 8.35 | 156.10 | 0.91 | 165.36 | 28.5% |
| steady | 62.51 | 0.44 | 1.73 | 64.68 | 11.2% |
| rules | 7.62 | 1.58 | 54.38 | 63.58 | 11.0% |
| cardinal | 41.66 | 1.61 | 1.09 | 44.37 | 7.6% |
| churn | 12.52 | 0.07 | 0.83 | 13.41 | 2.3% |
| latecomer | 4.19 | 0.20 | 0.86 | 5.24 | 0.9% |
| ghost | not measured | 0.00 | 0.13 | ≥ 0.13 | ≥ 0.0% |
| tiny | 0.04 | 0.00 | 0.08 | 0.12 | 0.0% |

**Read before quoting:**

- rules are evaluated remotely, so a tenant's rule queries drive its share of both the query path (the work) and the ruler (the time spent waiting for it). Each pool's cost is split once, so nothing is counted twice, but a rule-heavy tenant's total weighs the same queries in both
- a tenant marked ≥ was not measured in every priced pool: what it would owe there is unknown, not zero, and that pool's cost is shared out among the tenants that were measured, so its figure is a lower bound and theirs are inflated

Not modelled yet: ingester write path, object storage, store-gateway, compactor.

Generated 2026-10-22T07:20:05Z.
