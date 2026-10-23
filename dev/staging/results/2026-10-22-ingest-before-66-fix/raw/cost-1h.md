# promcost tenant cost report

## Ingester memory

Average over the last 1h, ending 2026-10-22T03:26:56Z. Pool: 3 ingesters, 360.00 EUR/month.

**`monitoring` uses 61.0% of ingester memory** — the equivalent of 1.8 of your 3 ingesters, 219.53 EUR/month.

| Tenant | Active series | Share | ≈ ingesters | EUR/month |
|---|---:|---:|---:|---:|
| monitoring | 106,418 | 61.0% | 1.83 | 219.53 |
| steady | 30,022 | 17.2% | 0.52 | 61.93 |
| cardinal | 20,010 | 11.5% | 0.34 | 41.28 |
| churn | 5,711 | 3.3% | 0.10 | 11.78 |
| dashboards | 4,011 | 2.3% | 0.07 | 8.27 |
| rules | 3,660 | 2.1% | 0.06 | 7.55 |
| ghost | 2,651 | 1.5% | 0.05 | 5.47 |
| latecomer | 2,011 | 1.2% | 0.03 | 4.15 |
| tiny | 20 | 0.0% | 0.00 | 0.04 |
| **Total** | **174,514** | **100%** | **3** | **360.00** |

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_ingester_active_series (active series), averaged over 1h` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `avg_over_time((sum by (user) (max by (ingester_id, user) (label_replace(cortex_ingester_active_series and on (cluster, namespace, job) cortex_partition_ring_partitions, "ingester_id", "$1", "pod", ".*?-(rc-[0-9]+-[0-9]+\|[0-9]+)$"))))[1h:1m])` | `Mimir metrics (queried as tenant monitoring)` |
| write path | `ingest storage` | `detected from which driver query returned data` |
| replication factor | `1` | `ingest storage: one owner per partition, zone replicas collapsed with max, so there is nothing to divide` |
| ingester replicas | `3` | `promcost.yaml inventory` |
| price | `120.00 EUR per ingester-month × 3 = 360.00 EUR/month` | `promcost.yaml inventory` |

## Query path

Total over the last 1h, ending 2026-10-22T03:26:56Z. Pool: 1 × query-frontend, 2 × querier, 160.00 EUR/month.

**`dashboards` uses 92.5% of query path**, 148.07 EUR/month.

| Tenant | Chunk bytes fetched | Share | EUR/month |
|---|---:|---:|---:|
| dashboards | 7.5 GiB | 92.5% | 148.07 |
| rules | 345.5 MiB | 4.2% | 6.67 |
| cardinal | 142.0 MiB | 1.7% | 2.74 |
| steady | 99.6 MiB | 1.2% | 1.92 |
| latecomer | 13.7 MiB | 0.2% | 0.26 |
| ghost | 9.8 MiB | 0.1% | 0.19 |
| churn | 6.8 MiB | 0.1% | 0.13 |
| monitoring | 489.1 KiB | 0.0% | 0.01 |
| tiny | 246.5 KiB | 0.0% | 0.00 |
| **Total** | **8.1 GiB** | **100%** | **160.00** |

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_query_fetched_chunk_bytes_total (chunk bytes fetched), total increase over 1h` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `sum by (user) (increase(cortex_query_fetched_chunk_bytes_total[1h]))` | `Mimir metrics (queried as tenant monitoring)` |
| driver preference | `fetched volume over time: cortex_query_fetched_chunk_bytes_total → cortex_query_samples_processed_total → cortex_query_fetched_series_total → cortex_query_seconds_total; the first with data is used` | `ADR 0003 pool 6, ADR 0002 decision 8` |
| rule queries | `included — rules are evaluated remotely, so every tenant's rule queries are in this pool alongside its user queries, and no label separates the two (inferred: the ruler's job (job=~".*ruler") evaluated rules but has no cortex_distributor_query_duration_seconds_count data, which a ruler evaluating locally always records, so its queries go to the query-frontend)` | `Mimir metrics (queried as tenant monitoring)` |
| query-frontend price | `40.00 EUR per query-frontend-month × 1 = 40.00 EUR/month` | `promcost.yaml inventory` |
| querier price | `60.00 EUR per querier-month × 2 = 120.00 EUR/month` | `promcost.yaml inventory` |

## Ruler CPU

Total over the last 1h, ending 2026-10-22T03:26:56Z. Pool: 1 ruler, 60.00 EUR/month.

**`rules` uses 90.3% of ruler CPU** — the equivalent of 0.9 of your 1 ruler, 54.17 EUR/month.

| Tenant | Rule-evaluation seconds | Share | ≈ rulers | EUR/month |
|---|---:|---:|---:|---:|
| rules | 677.4 | 90.3% | 0.90 | 54.17 |
| steady | 23.7 | 3.2% | 0.03 | 1.90 |
| cardinal | 12.9 | 1.7% | 0.02 | 1.03 |
| dashboards | 12.7 | 1.7% | 0.02 | 1.01 |
| churn | 9.4 | 1.3% | 0.01 | 0.75 |
| latecomer | 8.2 | 1.1% | 0.01 | 0.66 |
| ghost | 5.0 | 0.7% | 0.01 | 0.40 |
| tiny | 0.9 | 0.1% | 0.00 | 0.07 |
| **Total** | **750.2** | **100%** | **1** | **60.00** |

**Read before quoting:**

- rules are evaluated remotely, so this time is mostly the ruler waiting on the query path, not ruler CPU: it shows how long each tenant's rules occupy the ruler, and the work behind it is already in the query path pool. It also depends on load — a tenant's rules run slower when a neighbour loads the cluster

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_prometheus_rule_evaluation_duration_seconds_sum (rule-evaluation seconds), total increase over 1h` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `sum by (user) (increase(cortex_prometheus_rule_evaluation_duration_seconds_sum[1h]))` | `Mimir metrics (queried as tenant monitoring)` |
| rule evaluation | `remote (inferred: the ruler's job (job=~".*ruler") evaluated rules but has no cortex_distributor_query_duration_seconds_count data, which a ruler evaluating locally always records, so its queries go to the query-frontend): the rule queries' work is priced in the query path` | `Mimir metrics (queried as tenant monitoring)` |
| ruler replicas | `1` | `promcost.yaml inventory` |
| price | `60.00 EUR per ruler-month × 1 = 60.00 EUR/month` | `promcost.yaml inventory` |

## Cost across pools

**`monitoring` is 37.9% of the cost of the priced pools** (ingester memory, query path, ruler CPU). What fraction of your platform cost that is stays unknown until `inventory.platform_cost_month` is set.

| Tenant | Ingester memory EUR/month | Query path EUR/month | Ruler CPU EUR/month | Total EUR/month | Share of priced cost |
|---|---:|---:|---:|---:|---:|
| monitoring | 219.53 | 0.01 | not measured | ≥ 219.54 | ≥ 37.9% |
| dashboards | 8.27 | 148.07 | 1.01 | 157.36 | 27.1% |
| rules | 7.55 | 6.67 | 54.17 | 68.39 | 11.8% |
| steady | 61.93 | 1.92 | 1.90 | 65.75 | 11.3% |
| cardinal | 41.28 | 2.74 | 1.03 | 45.05 | 7.8% |
| churn | 11.78 | 0.13 | 0.75 | 12.66 | 2.2% |
| ghost | 5.47 | 0.19 | 0.40 | 6.06 | 1.0% |
| latecomer | 4.15 | 0.26 | 0.66 | 5.07 | 0.9% |
| tiny | 0.04 | 0.00 | 0.07 | 0.12 | 0.0% |

**Read before quoting:**

- rules are evaluated remotely, so a tenant's rule queries drive its share of both the query path (the work) and the ruler (the time spent waiting for it). Each pool's cost is split once, so nothing is counted twice, but a rule-heavy tenant's total weighs the same queries in both
- a tenant marked ≥ was not measured in every priced pool: what it would owe there is unknown, not zero, and that pool's cost is shared out among the tenants that were measured, so its figure is a lower bound and theirs are inflated

Not modelled yet: ingester write path, object storage, store-gateway, compactor.

Generated 2026-10-22T03:26:56Z.
