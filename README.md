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

Implementation is a single Go binary using upstream `prometheus/promql/parser`.

### Status

Milestone 1 is implemented: the parser and static-tier checks (`PC-S01`–`PC-S06`), with golden-corpus tests, behind `promcost check`. Live-tier metering, fleet-tier reports, and the other subcommands above are not yet built.

### Building

```
go build -o bin/promcost ./cmd/promcost   # or: make build
go test ./...                              # or: make test
```

```
./bin/promcost check --dir path/to/rules [--config promcost.yaml] [--offline] [--fail-on warn|error]
```
