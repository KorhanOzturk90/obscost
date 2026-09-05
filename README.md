## promcost

![CI](https://github.com/KorhanOzturk90/obscost/actions/workflows/ci.yml/badge.svg)

**Where is your metrics infrastructure capacity going, and which tenant is responsible for it?**

promcost attributes the resource use and cost of a shared, multi-tenant metrics platform — Grafana Mimir first — down to individual tenants, starting with the recording and alerting rules running on their behalf. The goal is a platform team being able to say, with real evidence, not a guess:

> Tenant `analytics` accounts for 37% of ruler workload, and these rules explain 82% of it.

Rules are the first wedge because they're the clearest, most directly attributable source of ruler/query load on a shared Mimir cluster. Raw metric ingestion and cardinality attribution are a deliberate later phase — see [`PRODUCT-DIRECTION.md`](PRODUCT-DIRECTION.md) for the full thesis and roadmap.

### See it working

```
# promcost workload report — last 7d (observed)

## Tenant summary

| tenant    | rules | executions | execution % | samples | sample % |
|-----------|-------|------------|--------------|---------|----------|
| analytics | 1842  | 1842       | 37.2%        | 4096400 | 41.8%    |
| payments  | 621   | 621        | 18.4%        | 2068000 | 21.1%    |

## analytics

| rule                          | kind      | executions | execution % | samples | sample % |
|--------------------------------|-----------|------------|--------------|---------|----------|
| customer_activity:7d           | recording | 1440       | 78.2%        | 3200000 | 78.1%    |
```

This is real, tested output (`internal/report/testdata/workload_report.golden.md`), not a mockup. It's produced by joining actual observed rule-execution telemetry against a tenant's loaded rule definitions and ranking the result — every number is something that was measured, never estimated. See [`docs/adr/0001-observed-workload-attribution-layer.md`](docs/adr/0001-observed-workload-attribution-layer.md) for how that pipeline is built and the real bugs that testing it against a live Mimir instance surfaced along the way.

### What's built today

| command | does | telemetry source |
|---|---|---|
| `promcost report` | **The main event.** Joins observed rule-execution telemetry against loaded rule definitions and produces the ranked tenant/rule workload report above. | a portable NDJSON format, or a real parser for Mimir's own `-ruler.query-stats-enabled` ruler log (`--telemetry-format mimirlogs`) |
| `promcost check` | Static PromQL analysis — flags rules that *look* structurally expensive (wide subqueries, heavy evaluation frequency, high-cardinality output labels, limit-related issues) before anything runs. No telemetry needed. | n/a |

`check` and `report` are intentionally independent right now — `check` never sees observed data, `report` never reads static findings. Joining the two ("this rule is expensive in production, and these PromQL characteristics explain why") is explicit future work, tracked in [issue #9](https://github.com/KorhanOzturk90/obscost/issues/9).

### Quick start

```sh
go build -o bin/promcost ./cmd/promcost   # or: make build
go test ./...                              # or: make test
```

```sh
# static analysis — no telemetry required
./bin/promcost check --dir path/to/rules [--config promcost.yaml] [--offline] [--fail-on warn|error]

# observed workload attribution — rule definitions from a local checkout
./bin/promcost report --dir path/to/rules --telemetry ruler.log \
  --telemetry-format mimirlogs --config promcost.yaml [--since 7d] [--format md|json]

# ...or, with no local checkout at all: fetch rule definitions live from
# Mimir's own ruler API instead (needs backend.url in promcost.yaml)
./bin/promcost report --tenant analytics,payments --telemetry ruler.log \
  --telemetry-format mimirlogs --config promcost.yaml
```

Want to see it running against a real Mimir instance rather than a fixture? [`dev/mimir-local`](dev/mimir-local) is a self-contained, self-monitoring Docker Compose Mimir rig with `-ruler.query-stats-enabled` already on — spin it up and point `report --telemetry-format mimirlogs` at its actual ruler logs.

### Status and roadmap

Two things are real and tested today: static analysis (`check`, spec §8.1's parser + static-tier checks `PC-S01`–`PC-S06`) and observed workload attribution (`report`, closing issues #4–#8). Everything past that — joining the two, validating attribution against real infrastructure metrics, deterministic recommendations, PR workload forecasting, before/after verification, team ownership — is open and tracked on the [issues board](https://github.com/KorhanOzturk90/obscost/issues). `PRODUCT-DIRECTION.md` has the full 8-milestone map.

### Docs

- [`PRODUCT-DIRECTION.md`](PRODUCT-DIRECTION.md) — the product thesis, what's explicitly *not* being built yet, and why.
- [`docs/adr/`](docs/adr) — architecture decision records: the real design calls behind what's built, including the trade-offs and bugs found testing against live infrastructure.
- [`docs/runtime-telemetry-notes.md`](docs/runtime-telemetry-notes.md) — what Mimir's own telemetry can and can't tell you, and why it's the preferred first data source.
- [`CLAUDE.md`](CLAUDE.md) — repository guidance for AI coding agents working in this codebase.

### Demo

A video walkthrough is coming — for now, the [example above](#see-it-working) and `dev/mimir-local` are the fastest way to see it working end to end.

### Contributing

Contribution guidelines are coming soon. In the meantime, issues and pull requests are welcome — the [issues board](https://github.com/KorhanOzturk90/obscost/issues) is the current source of truth for what's planned next.
