# promcost check report

Generated: 2026-01-02T15:04:05Z
Rules scanned: 3
Tenants: payments, platform

## Summary

| severity | count |
|---|---|
| error | 2 |

## Findings

### [error] PC-S01 — rules.yaml:heavy:HeavySubquery

subquery [720h0m0s:5m0s] evaluates 8640 steps per invocation

Tenant: payments

| key | value |
|---|---|
| range_seconds | 2.592e+06 |
| steps | 8640 |

Remediation: reduce the subquery range, increase its step, or rewrite as a chained recording-rule window (see `promcost rewrite`)

---

### [error] PC-S05 — rules.yaml:many-rules:

group "many-rules" has 3 rules, above tenant platform's limit of 2

Tenant: platform

---

