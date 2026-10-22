# promcost tenant cost report

## Ingester memory

Average over the last 30m, ending 2026-10-22T03:26:57Z. Pool: 3 ingesters, 360.00 EUR/month.

**`monitoring` uses 60.9% of ingester memory** — the equivalent of 1.8 of your 3 ingesters, 219.07 EUR/month.

| Tenant | Active series | Share | ≈ ingesters | EUR/month |
|---|---:|---:|---:|---:|
| monitoring | 105,476 | 60.9% | 1.83 | 219.07 |
| steady | 30,022 | 17.3% | 0.52 | 62.35 |
| cardinal | 20,010 | 11.5% | 0.35 | 41.56 |
| churn | 6,011 | 3.5% | 0.10 | 12.48 |
| dashboards | 4,011 | 2.3% | 0.07 | 8.33 |
| rules | 3,660 | 2.1% | 0.06 | 7.60 |
| ghost | 2,111 | 1.2% | 0.04 | 4.38 |
| latecomer | 2,011 | 1.2% | 0.03 | 4.18 |
| tiny | 20 | 0.0% | 0.00 | 0.04 |
| **Total** | **173,332** | **100%** | **3** | **360.00** |

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_ingester_active_series (active series), averaged over 30m` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `avg_over_time((sum by (user) (max by (ingester_id, user) (label_replace(cortex_ingester_active_series and on (cluster, namespace, job) cortex_partition_ring_partitions, "ingester_id", "$1", "pod", ".*?-(rc-[0-9]+-[0-9]+\|[0-9]+)$"))))[30m:1m])` | `Mimir metrics (queried as tenant monitoring)` |
| write path | `ingest storage` | `detected from which driver query returned data` |
| replication factor | `1` | `ingest storage: one owner per partition, zone replicas collapsed with max, so there is nothing to divide` |
| ingester replicas | `3` | `promcost.yaml inventory` |
| price | `120.00 EUR per ingester-month × 3 = 360.00 EUR/month` | `promcost.yaml inventory` |

## Query path

Total over the last 30m, ending 2026-10-22T03:26:57Z. Pool: 1 × query-frontend, 2 × querier, 160.00 EUR/month.

**`dashboards` uses 94.2% of query path**, 150.68 EUR/month.

| Tenant | Chunk bytes fetched | Share | EUR/month |
|---|---:|---:|---:|
| dashboards | 4.2 GiB | 94.2% | 150.68 |
| rules | 124.4 MiB | 2.7% | 4.40 |
| cardinal | 83.7 MiB | 1.8% | 2.96 |
| steady | 41.2 MiB | 0.9% | 1.46 |
| latecomer | 10.5 MiB | 0.2% | 0.37 |
| churn | 3.4 MiB | 0.1% | 0.12 |
| monitoring | 309.7 KiB | 0.0% | 0.01 |
| tiny | 149.8 KiB | 0.0% | 0.01 |
| ghost | 0 B | 0.0% | 0.00 |
| **Total** | **4.4 GiB** | **100%** | **160.00** |

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

Total over the last 30m, ending 2026-10-22T03:26:57Z. Pool: 1 ruler, 60.00 EUR/month.

**`rules` uses 90.4% of ruler CPU** — the equivalent of 0.9 of your 1 ruler, 54.25 EUR/month.

| Tenant | Rule-evaluation seconds | Share | ≈ rulers | EUR/month |
|---|---:|---:|---:|---:|
| rules | 336.4 | 90.4% | 0.90 | 54.25 |
| steady | 11.9 | 3.2% | 0.03 | 1.91 |
| cardinal | 6.6 | 1.8% | 0.02 | 1.07 |
| dashboards | 6.4 | 1.7% | 0.02 | 1.02 |
| latecomer | 5.0 | 1.3% | 0.01 | 0.80 |
| churn | 4.6 | 1.2% | 0.01 | 0.75 |
| ghost | 0.8 | 0.2% | 0.00 | 0.12 |
| tiny | 0.5 | 0.1% | 0.00 | 0.07 |
| **Total** | **372.0** | **100%** | **1** | **60.00** |

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

**`monitoring` is 37.8% of the cost of the priced pools** (ingester memory, query path, ruler CPU). What fraction of your platform cost that is stays unknown until `inventory.platform_cost_month` is set.

| Tenant | Ingester memory EUR/month | Query path EUR/month | Ruler CPU EUR/month | Total EUR/month | Share of priced cost |
|---|---:|---:|---:|---:|---:|
| monitoring | 219.07 | 0.01 | not measured | ≥ 219.08 | ≥ 37.8% |
| dashboards | 8.33 | 150.68 | 1.02 | 160.03 | 27.6% |
| rules | 7.60 | 4.40 | 54.25 | 66.25 | 11.4% |
| steady | 62.35 | 1.46 | 1.91 | 65.72 | 11.3% |
| cardinal | 41.56 | 2.96 | 1.07 | 45.58 | 7.9% |
| churn | 12.48 | 0.12 | 0.75 | 13.35 | 2.3% |
| latecomer | 4.18 | 0.37 | 0.80 | 5.35 | 0.9% |
| ghost | 4.38 | 0.00 | 0.12 | 4.51 | 0.8% |
| tiny | 0.04 | 0.01 | 0.07 | 0.12 | 0.0% |

**Read before quoting:**

- rules are evaluated remotely, so a tenant's rule queries drive its share of both the query path (the work) and the ruler (the time spent waiting for it). Each pool's cost is split once, so nothing is counted twice, but a rule-heavy tenant's total weighs the same queries in both
- a tenant marked ≥ was not measured in every priced pool: what it would owe there is unknown, not zero, and that pool's cost is shared out among the tenants that were measured, so its figure is a lower bound and theirs are inflated

Not modelled yet: ingester write path, object storage, store-gateway, compactor.

Generated 2026-10-22T03:26:57Z.
