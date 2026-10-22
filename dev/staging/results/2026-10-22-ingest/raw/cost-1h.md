# promcost tenant cost report

## Ingester memory

Average over the last 1h, ending 2026-10-22T07:20:03Z. Pool: 3 ingesters, 360.00 EUR/month.

**`monitoring` uses 62.0% of ingester memory** — the equivalent of 1.9 of your 3 ingesters, 223.13 EUR/month.

| Tenant | Active series | Share | ≈ ingesters | EUR/month |
|---|---:|---:|---:|---:|
| monitoring | 108,956 | 62.0% | 1.86 | 223.13 |
| steady | 30,022 | 17.1% | 0.51 | 61.48 |
| cardinal | 20,010 | 11.4% | 0.34 | 40.98 |
| churn | 5,344 | 3.0% | 0.09 | 10.94 |
| dashboards | 4,011 | 2.3% | 0.07 | 8.21 |
| rules | 3,660 | 2.1% | 0.06 | 7.50 |
| latecomer | 2,011 | 1.1% | 0.03 | 4.12 |
| ghost | 1,758 | 1.0% | 0.03 | 3.60 |
| tiny | 20 | 0.0% | 0.00 | 0.04 |
| **Total** | **175,791** | **100%** | **3** | **360.00** |

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_ingester_active_series (active series), averaged over 1h` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `sum_over_time((sum by (user) (max by (ingester_id, user) (label_replace(cortex_ingester_active_series and on (cluster, namespace, job) cortex_partition_ring_partitions, "ingester_id", "$1", "pod", ".*?-(rc-[0-9]+-[0-9]+\|[0-9]+)$"))))[1h:1m]) / scalar(count_over_time((sum(sum by (user) (max by (ingester_id, user) (label_replace(cortex_ingester_active_series and on (cluster, namespace, job) cortex_partition_ring_partitions, "ingester_id", "$1", "pod", ".*?-(rc-[0-9]+-[0-9]+\|[0-9]+)$")))))[1h:1m]))` | `Mimir metrics (queried as tenant monitoring)` |
| averaging | `averaged over the whole window; points where the tenant had no series count as 0; points where Mimir's own metrics were missing are excluded (60 of 60)` | `count_over_time((sum(sum by (user) (max by (ingester_id, user) (label_replace(cortex_ingester_active_series and on (cluster, namespace, job) cortex_partition_ring_partitions, "ingester_id", "$1", "pod", ".*?-(rc-[0-9]+-[0-9]+\|[0-9]+)$")))))[1h:1m])` |
| write path | `ingest storage` | `detected from which driver query returned data` |
| replication factor | `1` | `ingest storage: one owner per partition, zone replicas collapsed with max, so there is nothing to divide` |
| ingester replicas | `3` | `promcost.yaml inventory` |
| price | `120.00 EUR per ingester-month × 3 = 360.00 EUR/month` | `promcost.yaml inventory` |

## Query path

Total over the last 1h, ending 2026-10-22T07:20:03Z. Pool: 1 × query-frontend, 2 × querier, 160.00 EUR/month.

**`dashboards` uses 97.4% of query path**, 155.78 EUR/month.

| Tenant | Chunk bytes fetched | Share | EUR/month |
|---|---:|---:|---:|
| dashboards | 14.5 GiB | 97.4% | 155.78 |
| rules | 193.3 MiB | 1.3% | 2.03 |
| cardinal | 121.6 MiB | 0.8% | 1.28 |
| steady | 60.9 MiB | 0.4% | 0.64 |
| latecomer | 15.5 MiB | 0.1% | 0.16 |
| churn | 6.8 MiB | 0.0% | 0.07 |
| ghost | 4.1 MiB | 0.0% | 0.04 |
| tiny | 162.4 KiB | 0.0% | 0.00 |
| monitoring | 0 B | 0.0% | 0.00 |
| **Total** | **14.9 GiB** | **100%** | **160.00** |

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

Total over the last 1h, ending 2026-10-22T07:20:03Z. Pool: 1 ruler, 60.00 EUR/month.

**`rules` uses 90.4% of ruler CPU** — the equivalent of 0.9 of your 1 ruler, 54.23 EUR/month.

| Tenant | Rule-evaluation seconds | Share | ≈ rulers | EUR/month |
|---|---:|---:|---:|---:|
| rules | 656.3 | 90.4% | 0.90 | 54.23 |
| steady | 22.5 | 3.1% | 0.03 | 1.86 |
| dashboards | 11.8 | 1.6% | 0.02 | 0.97 |
| cardinal | 11.7 | 1.6% | 0.02 | 0.97 |
| latecomer | 9.7 | 1.3% | 0.01 | 0.81 |
| churn | 9.1 | 1.3% | 0.01 | 0.75 |
| ghost | 4.1 | 0.6% | 0.01 | 0.34 |
| tiny | 0.9 | 0.1% | 0.00 | 0.08 |
| **Total** | **726.1** | **100%** | **1** | **60.00** |

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

**`monitoring` is 38.5% of the cost of the priced pools** (ingester memory, query path, ruler CPU). What fraction of your platform cost that is stays unknown until `inventory.platform_cost_month` is set.

| Tenant | Ingester memory EUR/month | Query path EUR/month | Ruler CPU EUR/month | Total EUR/month | Share of priced cost |
|---|---:|---:|---:|---:|---:|
| monitoring | 223.13 | 0.00 | not measured | ≥ 223.13 | ≥ 38.5% |
| dashboards | 8.21 | 155.78 | 0.97 | 164.97 | 28.4% |
| steady | 61.48 | 0.64 | 1.86 | 63.98 | 11.0% |
| rules | 7.50 | 2.03 | 54.23 | 63.75 | 11.0% |
| cardinal | 40.98 | 1.28 | 0.97 | 43.22 | 7.5% |
| churn | 10.94 | 0.07 | 0.75 | 11.77 | 2.0% |
| latecomer | 4.12 | 0.16 | 0.81 | 5.09 | 0.9% |
| ghost | 3.60 | 0.04 | 0.34 | 3.98 | 0.7% |
| tiny | 0.04 | 0.00 | 0.08 | 0.12 | 0.0% |

**Read before quoting:**

- rules are evaluated remotely, so a tenant's rule queries drive its share of both the query path (the work) and the ruler (the time spent waiting for it). Each pool's cost is split once, so nothing is counted twice, but a rule-heavy tenant's total weighs the same queries in both
- a tenant marked ≥ was not measured in every priced pool: what it would owe there is unknown, not zero, and that pool's cost is shared out among the tenants that were measured, so its figure is a lower bound and theirs are inflated

Not modelled yet: ingester write path, object storage, store-gateway, compactor.

Generated 2026-10-22T07:20:03Z.
