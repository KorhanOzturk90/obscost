## promcost

Cost attribution and load prediction for recording and alerting rules in multi-tenant Prometheus-compatible stacks (Mimir first).

Not a linter — [pint](https://cloudflare.github.io/pint/) owns rule hygiene. promcost wraps pint and estimates what a rule will cost the backend.


### Planned CLI

| Command | Purpose |
|---|---|
| `scan` | Ranked fleet report from live cluster CRDs or the ruler API |
| `check` | PR gate over changed rule files |
| `explain` | Per-rule cost breakdown for a tenant |
| `rewrite` | Suggested rewrite with warm-up / backfill notes |
| `pint-config` | Per-tenant pint HCL from tenancy discovery |

Implementation will be a single Go binary using upstream `prometheus/promql/parser`.
