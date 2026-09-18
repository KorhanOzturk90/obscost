# promcost tenant cost report

## Ingester memory

Average over the last 7d, ending 2026-09-18T12:00:00Z. Pool: 8 ingesters. **Not costed** — no price in the inventory.

**`analytics` uses 62.6% of ingester memory** — the equivalent of 5.0 of your 8 ingesters.

| Tenant | Active series | Share | ≈ ingesters |
|---|---:|---:|---:|
| analytics | withheld (no RF) | 62.6% | 5.01 |
| infra | withheld (no RF) | 32.3% | 2.59 |
| payments | withheld (no RF) | 5.0% | 0.40 |
| platform | not measured | not measured | not measured |
| **Total** | **withheld (no RF)** | **100%** | **8** |

**Read before quoting:**

- no replication factor is known, so absolute active series figures are withheld; shares are unaffected because a uniform replication factor cancels in a ratio

### Assumptions

| Input | Value | Source |
|---|---|---|
| driver | `cortex_ingester_active_series (active series), averaged over 7d` | `Mimir metrics (queried as tenant monitoring)` |
| driver query | `q` | `Mimir metrics (queried as tenant monitoring)` |
| replication factor | `unknown — absolute figures withheld, shares unaffected` | `not reported by Mimir and not in the inventory` |
| ingester replicas | `8` | `promcost.yaml inventory` |
| price | `not supplied — pool not costed` | `promcost.yaml inventory` |

Generated 2026-09-18T12:00:00Z.
