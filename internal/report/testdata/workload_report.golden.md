# promcost workload report — last 7d (observed)

Generated: 2026-01-02T15:04:05Z
Total executions: 2463
Total samples processed: 9800000
Rule definitions loaded: 2521

## Tenant summary

| tenant | rules | executions | execution % | samples | sample % |
|---|---|---|---|---|---|
| analytics | 1842 | 1842 | 37.2% | 4096400 | 41.8% |
| payments | 621 | 621 | 18.4% | 2068000 | 21.1% |

## analytics

| rule | kind | executions | execution % | samples | sample % |
|---|---|---|---|---|---|
| analytics/analytics/rules.yaml/g/customer_activity:7d | recording | 1440 | 78.2% | 3200000 | 78.1% |

1 unmatched execution(s) (500 samples) — see "Unmatched executions" below.

## payments

No matched rule executions.

## Unmatched executions

Executions whose rule identity didn't match any loaded rule definition (deleted rule, drifted namespace, or a telemetry source that disagrees with --dir).

| rule id | timestamp | samples |
|---|---|---|
| analytics/analytics/rules.yaml/g/deleted_rule | 2026-01-02T12:00:00Z | 500 |
