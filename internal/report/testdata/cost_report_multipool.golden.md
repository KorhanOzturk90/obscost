# promcost tenant cost report

## Ingester memory

Average over the last 7d, ending 2026-09-18T12:00:00Z. Pool: 3 ingesters, 300.00 EUR/month.

**`analytics` uses 40.9% of ingester memory** — the equivalent of 1.2 of your 3 ingesters, 122.65 EUR/month.

| Tenant | Active series | Share | ≈ ingesters | EUR/month |
|---|---:|---:|---:|---:|
| analytics | 15,005 | 40.9% | 1.23 | 122.65 |
| monitoring | 12,643 | 34.4% | 1.03 | 103.35 |
| infra | 7,743 | 21.1% | 0.63 | 63.29 |
| payments | 1,205 | 3.3% | 0.10 | 9.85 |
| platform | 105 | 0.3% | 0.01 | 0.86 |
| **Total** | **36,701** | **100%** | **3** | **300.00** |

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_ingester_active_series (active series), averaged over 7d` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `avg_over_time(...)` | `Mimir metrics (queried as tenant monitoring)` |
| replication factor | `3` | `rfq` |
| ingester replicas | `3` | `promcost.yaml inventory` |
| price | `100.00 EUR per ingester-month × 3 = 300.00 EUR/month` | `promcost.yaml inventory` |

## Query path

Total over the last 7d, ending 2026-09-18T12:00:00Z. Pool: 2 × querier, 200.00 EUR/month. **Not priced:** query-frontend.

**`analytics` uses 52.1% of query path**, 104.25 EUR/month.

| Tenant | Chunk bytes fetched | Share | EUR/month |
|---|---:|---:|---:|
| analytics | 49.3 GiB | 52.1% | 104.25 |
| infra | 44.3 GiB | 46.8% | 93.62 |
| payments | 972.7 MiB | 1.0% | 2.01 |
| monitoring | 57.5 MiB | 0.1% | 0.12 |
| platform | not measured | not measured | not measured |
| **Total** | **94.6 GiB** | **100%** | **200.00** |

**Read before quoting:**

- the pool is priced from 1 of its 2 components; query-frontend not priced, so the pool cost and every tenant's cost from it are lower bounds. Shares are unaffected

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_query_fetched_chunk_bytes_total (chunk bytes fetched), total increase over 7d` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `sum by (user) (increase(cortex_query_fetched_chunk_bytes_total[7d]))` | `Mimir metrics (queried as tenant monitoring)` |
| query-frontend price | `not supplied — component not priced` | `promcost.yaml inventory` |
| querier price | `100.00 EUR per querier-month × 2 = 200.00 EUR/month` | `promcost.yaml inventory` |

## Ruler CPU

Total over the last 7d, ending 2026-09-18T12:00:00Z. **Not costed**, and no ruler count in the inventory — shares only.

**`analytics` uses 37.5% of ruler CPU**.

| Tenant | Rule-evaluation seconds | Share | ≈ rulers |
|---|---:|---:|---:|
| analytics | 3,578.4 | 37.5% | — |
| infra | 2,815.3 | 29.5% | — |
| payments | 1,649.7 | 17.3% | — |
| platform | 1,494.0 | 15.7% | — |
| **Total** | **9,537.4** | **100%** | **—** |

**Read before quoting:**

- rules are evaluated remotely, so this time is mostly the ruler waiting on the query path

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_prometheus_rule_evaluation_duration_seconds_sum (rule-evaluation seconds), total increase over 7d` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `sum by (user) (increase(cortex_prometheus_rule_evaluation_duration_seconds_sum[7d]))` | `Mimir metrics (queried as tenant monitoring)` |
| rule evaluation | `remote — the ruler sends its rule queries to the query-frontend` | `Mimir metrics (queried as tenant monitoring)` |
| ruler replicas | `not supplied — shares only, no replica equivalents` | `promcost.yaml inventory` |
| price | `not supplied — pool not costed` | `promcost.yaml inventory` |

## Cost across pools

**`analytics` is 45.4% of the 31.2% of platform cost we can price** (ingester memory, query path (without query-frontend); ruler CPU not costed).

| Tenant | Ingester memory EUR/month | Query path EUR/month | Total EUR/month | Share of priced cost |
|---|---:|---:|---:|---:|
| analytics | 122.65 | 104.25 | 226.91 | 45.4% |
| infra | 63.29 | 93.62 | 156.91 | 31.4% |
| monitoring | 103.35 | 0.12 | 103.46 | 20.7% |
| payments | 9.85 | 2.01 | 11.86 | 2.4% |
| platform | 0.86 | not measured | ≥ 0.86 | ≥ 0.2% |

**Read before quoting:**

- a tenant marked ≥ was not measured in every priced pool: what it would owe there is unknown, not zero, and that pool's cost is shared out among the tenants that were measured, so its figure is a lower bound and theirs are inflated

Not modelled yet: ingester write path, object storage, store-gateway, compactor.

Generated 2026-09-18T12:00:00Z.
