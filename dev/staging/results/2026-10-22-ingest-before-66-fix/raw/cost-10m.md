# promcost tenant cost report

## Ingester memory

Average over the last 10m, ending 2026-10-22T03:26:58Z. Pool: 3 ingesters, 360.00 EUR/month.

**`monitoring` uses 61.4% of ingester memory** — the equivalent of 1.8 of your 3 ingesters, 221.06 EUR/month.

| Tenant | Active series | Share | ≈ ingesters | EUR/month |
|---|---:|---:|---:|---:|
| monitoring | 104,600 | 61.4% | 1.84 | 221.06 |
| steady | 30,022 | 17.6% | 0.53 | 63.45 |
| cardinal | 20,010 | 11.7% | 0.35 | 42.29 |
| churn | 6,011 | 3.5% | 0.11 | 12.70 |
| dashboards | 4,011 | 2.4% | 0.07 | 8.48 |
| rules | 3,660 | 2.1% | 0.06 | 7.73 |
| latecomer | 2,011 | 1.2% | 0.04 | 4.25 |
| tiny | 20 | 0.0% | 0.00 | 0.04 |
| ghost | not measured | not measured | not measured | not measured |
| **Total** | **170,345** | **100%** | **3** | **360.00** |

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_ingester_active_series (active series), averaged over 10m` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `avg_over_time((sum by (user) (max by (ingester_id, user) (label_replace(cortex_ingester_active_series and on (cluster, namespace, job) cortex_partition_ring_partitions, "ingester_id", "$1", "pod", ".*?-(rc-[0-9]+-[0-9]+\|[0-9]+)$"))))[10m:1m])` | `Mimir metrics (queried as tenant monitoring)` |
| write path | `ingest storage` | `detected from which driver query returned data` |
| replication factor | `1` | `ingest storage: one owner per partition, zone replicas collapsed with max, so there is nothing to divide` |
| ingester replicas | `3` | `promcost.yaml inventory` |
| price | `120.00 EUR per ingester-month × 3 = 360.00 EUR/month` | `promcost.yaml inventory` |

## Query path

Total over the last 10m, ending 2026-10-22T03:26:58Z. Pool: 1 × query-frontend, 2 × querier, 160.00 EUR/month.

**`dashboards` uses 94.8% of query path**, 151.63 EUR/month.

| Tenant | Chunk bytes fetched | Share | EUR/month |
|---|---:|---:|---:|
| dashboards | 1.5 GiB | 94.8% | 151.63 |
| rules | 36.2 MiB | 2.3% | 3.65 |
| cardinal | 31.8 MiB | 2.0% | 3.20 |
| steady | 9.6 MiB | 0.6% | 0.97 |
| latecomer | 3.8 MiB | 0.2% | 0.39 |
| churn | 1.2 MiB | 0.1% | 0.12 |
| monitoring | 312.7 KiB | 0.0% | 0.03 |
| tiny | 57.5 KiB | 0.0% | 0.01 |
| ghost | 0 B | 0.0% | 0.00 |
| **Total** | **1.5 GiB** | **100%** | **160.00** |

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

Total over the last 10m, ending 2026-10-22T03:26:59Z. Pool: 1 ruler, 60.00 EUR/month.

**`rules` uses 90.1% of ruler CPU** — the equivalent of 0.9 of your 1 ruler, 54.04 EUR/month.

| Tenant | Rule-evaluation seconds | Share | ≈ rulers | EUR/month |
|---|---:|---:|---:|---:|
| rules | 110.2 | 90.1% | 0.90 | 54.04 |
| steady | 4.1 | 3.3% | 0.03 | 2.00 |
| dashboards | 2.2 | 1.8% | 0.02 | 1.10 |
| cardinal | 2.2 | 1.8% | 0.02 | 1.09 |
| latecomer | 1.7 | 1.4% | 0.01 | 0.84 |
| churn | 1.5 | 1.2% | 0.01 | 0.74 |
| ghost | 0.3 | 0.2% | 0.00 | 0.12 |
| tiny | 0.1 | 0.1% | 0.00 | 0.07 |
| **Total** | **122.4** | **100%** | **1** | **60.00** |

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

**`monitoring` is 38.1% of the cost of the priced pools** (ingester memory, query path, ruler CPU). What fraction of your platform cost that is stays unknown until `inventory.platform_cost_month` is set.

| Tenant | Ingester memory EUR/month | Query path EUR/month | Ruler CPU EUR/month | Total EUR/month | Share of priced cost |
|---|---:|---:|---:|---:|---:|
| monitoring | 221.06 | 0.03 | not measured | ≥ 221.09 | ≥ 38.1% |
| dashboards | 8.48 | 151.63 | 1.10 | 161.21 | 27.8% |
| steady | 63.45 | 0.97 | 2.00 | 66.42 | 11.5% |
| rules | 7.73 | 3.65 | 54.04 | 65.42 | 11.3% |
| cardinal | 42.29 | 3.20 | 1.09 | 46.58 | 8.0% |
| churn | 12.70 | 0.12 | 0.74 | 13.56 | 2.3% |
| latecomer | 4.25 | 0.39 | 0.84 | 5.47 | 0.9% |
| ghost | not measured | 0.00 | 0.12 | ≥ 0.12 | ≥ 0.0% |
| tiny | 0.04 | 0.01 | 0.07 | 0.12 | 0.0% |

**Read before quoting:**

- rules are evaluated remotely, so a tenant's rule queries drive its share of both the query path (the work) and the ruler (the time spent waiting for it). Each pool's cost is split once, so nothing is counted twice, but a rule-heavy tenant's total weighs the same queries in both
- a tenant marked ≥ was not measured in every priced pool: what it would owe there is unknown, not zero, and that pool's cost is shared out among the tenants that were measured, so its figure is a lower bound and theirs are inflated

Not modelled yet: ingester write path, object storage, store-gateway, compactor.

Generated 2026-10-22T03:26:59Z.
