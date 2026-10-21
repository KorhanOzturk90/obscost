## promcost

![CI](https://github.com/KorhanOzturk90/obscost/actions/workflows/ci.yml/badge.svg)

**What each tenant of your self-hosted Grafana Mimir costs you, split by the thing that actually makes each component grow.**

`promcost cost` reads Mimir's own metrics and splits three shared cost pools across tenants. Ingester memory is split by active series, the query path by the data each tenant's queries fetch, and ruler CPU by the time each tenant's rules take to evaluate. By default the answer is in resources ("`analytics` uses the equivalent of 5.0 of your 8 ingesters"). It's in currency only if you add your own prices to `promcost.yaml`. Every figure comes with the query, the replication factor and the price it rests on, and anything promcost couldn't measure says **not measured**, never 0.

It's a single read-only binary. It needs a Mimir URL and the tenant your Mimir's own metrics are scraped into. It doesn't need an agent or a database, and it never writes to Mimir.

### What it looks like

This excerpt is from the test fixture [`internal/report/testdata/cost_report_multipool.golden.md`](internal/report/testdata/cost_report_multipool.golden.md), not a production cluster. The fixture prices ingester memory and the querier, and leaves the ruler and the query-frontend unpriced:

```
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

... query path and ruler CPU sections, each with its own assumption table ...

## Cost across pools

**`analytics` is 45.4% of the 31.2% of platform cost we can price** (ingester memory, query path (without query-frontend); ruler CPU not costed).

| Tenant | Ingester memory EUR/month | Query path EUR/month | Total EUR/month | Share of priced cost |
|---|---:|---:|---:|---:|
| analytics | 122.65 | 104.25 | 226.91 | 45.4% |
| infra | 63.29 | 93.62 | 156.91 | 31.4% |
| monitoring | 103.35 | 0.12 | 103.46 | 20.7% |
| payments | 9.85 | 2.01 | 11.86 | 2.4% |
| platform | 0.86 | not measured | ≥ 0.86 | ≥ 0.2% |

Not modelled yet: ingester write path, object storage, store-gateway, compactor.
```

Note the headline: it says *45.4% of the 31.2% we can price*, not "45.4% of your Mimir bill". A total across pools always says what it covers. `platform` sent no queries through the query frontend in this window, so it's "not measured" there and its total is marked as a lower bound (≥).

<!-- TODO(launch/validation): once the staging validation report is committed, link it here as the evidence that shares reconcile against ground truth. Don't quote a number until then. -->

### Why this exists, and how it differs from what you already have

On a shared Mimir cluster, the ingesters, queriers and rulers serve every tenant at once. The question is what share of each shared component a tenant is responsible for. The tools below answer different questions:

| Tool | What it answers | Why it doesn't answer this |
|---|---|---|
| **Mimir's `mimir-top-tenants` dashboard** | Who is biggest on each axis: series, samples, rule time and so on | It's 14 separate `topk` leaderboards in raw units. None of them is a share of a total, they aren't combined, and they don't say what a series costs. |
| **Mimir's cost attribution** (`-validation.cost-attribution-trackers`, experimental in 3.x) | Splits samples and active series *inside* a tenant by a label, e.g. `team` | It works within a tenant on the write side, and it doesn't price anything. promcost works *across* tenants, and also covers the query path and the ruler. The two are complementary. |
| **OpenCost / Kubecost** | What each pod, namespace or label costs in Kubernetes | They can tell you the ingester StatefulSet costs X a month. They can't tell you which Mimir tenant uses those ingesters, because every tenant shares the same pods. promcost takes that pod cost as an input and splits it. |
| **Grafana Cloud's cost management and attribution** | Usage and spend on Grafana Cloud, attributed by label | It's for Grafana Cloud's own billing, not for a Mimir you run yourself. |

promcost does one thing: it splits the cost of **shared** Mimir components per tenant, by the physical driver of each component. It reads that driver from metrics Mimir already exposes. [How it works](docs/how-it-works.md) explains the model, including what it refuses to do: fit weights by regression, or spread the cost of unpriced and unmodelled components across tenants.

### Install

<!-- TODO(launch/release): replace this block with the exact snippet from the launch/release PR's "For the docs lane" section. -->

> **Placeholder until v0.1.0 is tagged.** The release binaries, the image name and the tag below aren't published yet.

```sh
# Binary (linux/darwin, amd64/arm64) from GitHub Releases
# RELEASE-SNIPPET-PLACEHOLDER: download URL and archive name from launch/release

# Container image
# RELEASE-SNIPPET-PLACEHOLDER: image reference from launch/release

# From source (Go 1.27+)
go install github.com/KorhanOzturk90/obscost/cmd/promcost@latest
```

### Quickstart

You need a Mimir whose own metrics (`cortex_*`) are scraped into one of its tenants. Here that tenant is called `meta`. If you don't have one, see [Prerequisites](docs/prerequisites.md).

```sh
curl -sSLO https://raw.githubusercontent.com/KorhanOzturk90/obscost/main/examples/promcost.yaml
$EDITOR promcost.yaml                                            # set backend.url; inventory is optional
promcost cost --config promcost.yaml --metrics-tenant meta --since 24h
promcost cost --config promcost.yaml --metrics-tenant meta --since 7d --format json > cost.json
```

`--metrics-tenant` is the tenant that **scraped** Mimir, not a tenant you want to cost. This is the most common setup mistake. [Getting started](docs/getting-started.md) walks through a first report, step by step.

### What it measures and what it doesn't

| Pool | Split by | Status in v0.1 |
|---|---|---|
| 1. Ingester memory | active series (`cortex_ingester_active_series`), averaged over the window and divided by the replication factor | measured |
| 6. Query path (query-frontend + querier) | fetched chunk bytes, falling back to samples processed, then series fetched, then query seconds | measured |
| 7. Ruler CPU | rule-evaluation seconds | measured |
| 2. Ingester write path, 3. object storage, 4. store-gateway, 5. compactor | — | **not modelled yet**, and listed as such in every report |

Pools 2 to 5 aren't in v0.1, and the report says so. It doesn't treat them as zero. The rest of the model behaves the same way:

- **Shares cover every tenant**, not a top N, and they sum to 100% within each pool.
- **Currency is opt-in.** With no price, you get shares and "≈ N replicas". An unpriced pool is "not costed", not free.
- **Cross-pool totals always say what they cover**, and promcost won't show a blended total when nothing is priced.
- **Under remote rule evaluation**, the ruler's queries are counted in the query path. The report detects the evaluation mode from metrics and says which one it found, and when it can't tell, it says "unknown".
- **It's a snapshot over one window.** It doesn't compare windows or store history yet.

There are also known limitations, such as a metrics tenant that holds several Mimir clusters, or auth that isn't a bearer token. See [Prerequisites](docs/prerequisites.md#known-limitations-in-v01).

### Drill-downs

- **`promcost cost --rule-outputs`** names the recording rules that write a tenant's series, and their share of that tenant's active series.
- **`promcost report`** attributes ruler workload to rule groups, using Mimir's rule metrics. It can attribute to individual rules if you give it Mimir's ruler query-stats log (`-ruler.query-stats-enabled`). That's the layer that tells a tenant *what to change*. See [Getting started → drill down](docs/getting-started.md#5-drill-down-which-rules).

### Docs

- [Getting started](docs/getting-started.md): from nothing to a first `cost` report
- [Prerequisites](docs/prerequisites.md): Mimir versions, meta-monitoring, `--metrics-tenant`, replication factor, write path, rule evaluation mode, permissions
- [Configuration](docs/configuration.md): the full `promcost.yaml` reference
- [How it works](docs/how-it-works.md): the cost model on one page
- [Troubleshooting](docs/troubleshooting.md)
- [`docs/adr/`](docs/adr): the design decisions behind the model, with the measurements that support them

### Status

**v0.1** covers three pools (ingester memory, query path, ruler CPU) as a one-off snapshot, in resources and optional currency. It's developed against Mimir 3.2.0 (Helm chart `mimir-distributed` 6.2.0) in microservices mode, on a local Kubernetes rig, with both the classic and the ingest-storage write path.

<!-- TODO(launch/staging-env, launch/validation): state which topologies the staging validation covered once its report is committed. -->

Next, in the order set out in [ADR 0004](docs/adr/0004-finops-pivot-scope-and-sequencing.md):

- comparing windows ("up 4pp on last week, and why")
- daily rollups to a store you own
- a change timeline built from ruler API snapshots
- the remaining pools

Everything is tracked on the [issues board](https://github.com/KorhanOzturk90/obscost/issues).

### Contributing

Issues and pull requests are welcome. Bug reports are most useful when they include the `--format json` output, or the assumption table for the figure that looks wrong, plus your Mimir version and write path.

```sh
make build   # go build -o bin/promcost ./cmd/promcost
make test    # go test ./...
make lint    # golangci-lint run ./...
```

Read the [ADRs](docs/adr) before proposing a change to the cost model. [`AGENTS.md`](AGENTS.md) has repository conventions, including for AI coding agents.

### License

Apache-2.0. See [`LICENSE`](LICENSE).
