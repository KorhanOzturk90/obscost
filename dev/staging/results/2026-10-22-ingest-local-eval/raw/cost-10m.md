# promcost tenant cost report

## Ingester memory

Average over the last 10m, ending 2026-10-22T03:48:04Z. Pool: 3 ingesters, 360.00 EUR/month.

**`monitoring` uses 65.7% of ingester memory** — the equivalent of 2.0 of your 3 ingesters, 236.39 EUR/month.

| Tenant | Active series | Share | ≈ ingesters | EUR/month |
|---|---:|---:|---:|---:|
| monitoring | 124,972 | 65.7% | 1.97 | 236.39 |
| steady | 30,022 | 15.8% | 0.47 | 56.79 |
| cardinal | 20,010 | 10.5% | 0.32 | 37.85 |
| churn | 5,611 | 2.9% | 0.09 | 10.61 |
| dashboards | 4,011 | 2.1% | 0.06 | 7.59 |
| rules | 3,660 | 1.9% | 0.06 | 6.92 |
| latecomer | 2,011 | 1.1% | 0.03 | 3.80 |
| tiny | 20 | 0.0% | 0.00 | 0.04 |
| ghost | not measured | not measured | not measured | not measured |
| **Total** | **190,317** | **100%** | **3** | **360.00** |

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

Total over the last 10m, ending 2026-10-22T03:48:04Z. Pool: 1 × query-frontend, 2 × querier, 160.00 EUR/month.

**`dashboards` uses 99.8% of query path**, 159.69 EUR/month.

| Tenant | Chunk bytes fetched | Share | EUR/month |
|---|---:|---:|---:|
| dashboards | 1.7 GiB | 99.8% | 159.69 |
| latecomer | 1.8 MiB | 0.1% | 0.17 |
| steady | 1.5 MiB | 0.1% | 0.14 |
| cardinal | not measured | not measured | not measured |
| churn | not measured | not measured | not measured |
| ghost | not measured | not measured | not measured |
| rules | not measured | not measured | not measured |
| tiny | not measured | not measured | not measured |
| **Total** | **1.7 GiB** | **100%** | **160.00** |

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_query_fetched_chunk_bytes_total (chunk bytes fetched), total increase over 10m` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `sum by (user) (increase(cortex_query_fetched_chunk_bytes_total[10m]))` | `Mimir metrics (queried as tenant monitoring)` |
| driver preference | `fetched volume over time: cortex_query_fetched_chunk_bytes_total → cortex_query_samples_processed_total → cortex_query_fetched_series_total → cortex_query_seconds_total; the first with data is used` | `ADR 0003 pool 6, ADR 0002 decision 8` |
| rule queries | `not included — rules are evaluated inside the ruler, so this pool is user and API queries only; rule work is in the ruler CPU pool (inferred: the ruler's job (job=~".*ruler") has cortex_distributor_query_duration_seconds_count data, which only a ruler querying ingesters itself records)` | `Mimir metrics (queried as tenant monitoring)` |
| query-frontend price | `40.00 EUR per query-frontend-month × 1 = 40.00 EUR/month` | `promcost.yaml inventory` |
| querier price | `60.00 EUR per querier-month × 2 = 120.00 EUR/month` | `promcost.yaml inventory` |

## Ruler CPU

Total over the last 10m, ending 2026-10-22T03:48:05Z. Pool: 1 ruler, 60.00 EUR/month.

**`rules` uses 88.5% of ruler CPU** — the equivalent of 0.9 of your 1 ruler, 53.09 EUR/month.

| Tenant | Rule-evaluation seconds | Share | ≈ rulers | EUR/month |
|---|---:|---:|---:|---:|
| rules | 70.7 | 88.5% | 0.88 | 53.09 |
| cardinal | 2.9 | 3.6% | 0.04 | 2.17 |
| steady | 2.5 | 3.2% | 0.03 | 1.90 |
| churn | 1.4 | 1.7% | 0.02 | 1.02 |
| latecomer | 1.3 | 1.6% | 0.02 | 0.98 |
| dashboards | 1.0 | 1.3% | 0.01 | 0.78 |
| ghost | 0.0 | 0.0% | 0.00 | 0.03 |
| tiny | 0.0 | 0.0% | 0.00 | 0.02 |
| **Total** | **79.9** | **100%** | **1** | **60.00** |

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_prometheus_rule_evaluation_duration_seconds_sum (rule-evaluation seconds), total increase over 10m` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `sum by (user) (increase(cortex_prometheus_rule_evaluation_duration_seconds_sum[10m]))` | `Mimir metrics (queried as tenant monitoring)` |
| rule evaluation | `local (inferred: the ruler's job (job=~".*ruler") has cortex_distributor_query_duration_seconds_count data, which only a ruler querying ingesters itself records): all of this time is ruler CPU, and none of it is in the query path (the ruler's query time is part of it, not added to it)` | `Mimir metrics (queried as tenant monitoring)` |
| ruler replicas | `1` | `promcost.yaml inventory` |
| price | `60.00 EUR per ruler-month × 1 = 60.00 EUR/month` | `promcost.yaml inventory` |

## Cost across pools

**`monitoring` is 40.8% of the cost of the priced pools** (ingester memory, query path, ruler CPU). What fraction of your platform cost that is stays unknown until `inventory.platform_cost_month` is set.

| Tenant | Ingester memory EUR/month | Query path EUR/month | Ruler CPU EUR/month | Total EUR/month | Share of priced cost |
|---|---:|---:|---:|---:|---:|
| monitoring | 236.39 | not measured | not measured | ≥ 236.39 | ≥ 40.8% |
| dashboards | 7.59 | 159.69 | 0.78 | 168.06 | 29.0% |
| rules | 6.92 | not measured | 53.09 | ≥ 60.01 | ≥ 10.3% |
| steady | 56.79 | 0.14 | 1.90 | 58.82 | 10.1% |
| cardinal | 37.85 | not measured | 2.17 | ≥ 40.02 | ≥ 6.9% |
| churn | 10.61 | not measured | 1.02 | ≥ 11.64 | ≥ 2.0% |
| latecomer | 3.80 | 0.17 | 0.98 | 4.96 | 0.9% |
| tiny | 0.04 | not measured | 0.02 | ≥ 0.06 | ≥ 0.0% |
| ghost | not measured | not measured | 0.03 | ≥ 0.03 | ≥ 0.0% |

**Read before quoting:**

- a tenant marked ≥ was not measured in every priced pool: what it would owe there is unknown, not zero, and that pool's cost is shared out among the tenants that were measured, so its figure is a lower bound and theirs are inflated

Not modelled yet: ingester write path, object storage, store-gateway, compactor.

Generated 2026-10-22T03:48:05Z.
