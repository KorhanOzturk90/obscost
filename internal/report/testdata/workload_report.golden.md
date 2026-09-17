# promcost workload report — last 7d (observed)

Generated: 2026-01-02T15:04:05Z
Observed window: 2026-01-01 08:00 UTC – 2026-01-02 14:30 UTC
Ranked by: samples processed
Total executions: 2,463
Rule definitions loaded: 2,521

Attribution coverage: 2462 matched / 2475 captured (99.5%), 1 unmatched, 12 skipped before matching

## Tenant summary

| tenant | rules | executions | samples processed | share |
|---|---|---|---|---|
| analytics | 1842 | 1,842 | 4,096,400 | 41.8% |
| payments | 621 | 621 | 2,068,000 | 21.1% |

## analytics

### g (analytics/rules.yaml)

1,440 executions, 3,200,000 (78.1% of tenant's reported groups by samples processed)

| rule | kind | executions | samples processed | share |
|---|---|---|---|---|
| customer_activity:7d | recording | 1,440 | not measured | — |

1 unmatched execution(s) (500 samples) — see "Unmatched executions" below.

## payments

No matched rule executions.

## Unmatched executions

Executions whose rule identity didn't match any loaded rule definition (deleted rule, drifted namespace, or a telemetry source that disagrees with --dir).

| rule id | timestamp | samples |
|---|---|---|
| analytics/analytics/rules.yaml/g/deleted_rule | 2026-01-02T12:00:00Z | 500 |
