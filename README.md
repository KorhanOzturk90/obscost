## promcost

![CI](https://github.com/KorhanOzturk90/obscost/actions/workflows/ci.yml/badge.svg)

**What each tenant of your self-hosted Grafana Mimir costs you, split by the thing that actually makes each component grow.**

`promcost cost` reads Mimir's own metrics and splits three shared cost pools across tenants. Ingester memory is split by active series, the query path by the data each tenant's queries fetch, and ruler CPU by the time each tenant's rules take to evaluate. By default the answer is in resources ("`analytics` uses the equivalent of 5.0 of your 8 ingesters"). It's in currency only if you add your own prices to `promcost.yaml`. Every figure comes with the query, the replication factor and the price it rests on, and anything promcost couldn't measure says **not measured**, never 0.

It's a single read-only binary. It needs a Mimir URL and the tenant your Mimir's own metrics are scraped into. It doesn't need an agent or a database, and it never writes to Mimir.

### What it looks like

This is real output, trimmed, from the staging validation run: Mimir 3.2.0 with the classic write path at replication factor 3, synthetic tenants, and a 30-minute window ([full report](dev/staging/results/2026-10-22-classic/raw/cost-30m.md)). The inventory prices all three pools with illustrative prices. `monitoring` is the metrics tenant itself, and it shows up because its own series sit in the ingesters too:

```
## Ingester memory

Average over the last 30m, ending 2026-10-22T08:52:44Z. Pool: 3 ingesters, 360.00 EUR/month.

**`monitoring` uses 62.8% of ingester memory** — the equivalent of 1.9 of your 3 ingesters, 226.15 EUR/month.

| Tenant | Active series | Share | ≈ ingesters | EUR/month |
|---|---:|---:|---:|---:|
| monitoring | 112,386 | 62.8% | 1.88 | 226.15 |
| steady | 30,022 | 16.8% | 0.50 | 60.41 |
| cardinal | 20,010 | 11.2% | 0.34 | 40.27 |
| churn | 5,944 | 3.3% | 0.10 | 11.96 |
| dashboards | 4,011 | 2.2% | 0.07 | 8.07 |
| rules | 3,660 | 2.0% | 0.06 | 7.36 |
| latecomer | 2,011 | 1.1% | 0.03 | 4.05 |
| ghost | 838 | 0.5% | 0.01 | 1.69 |
| tiny | 20 | 0.0% | 0.00 | 0.04 |
| **Total** | **178,903** | **100%** | **3** | **360.00** |

... query path and ruler CPU sections, each with its own assumption table ...

## Cost across pools

**`monitoring` is 39.0% of the cost of the priced pools** (ingester memory, query path, ruler CPU). What fraction of your platform cost that is stays unknown until `inventory.platform_cost_month` is set.

| Tenant | Ingester memory EUR/month | Query path EUR/month | Ruler CPU EUR/month | Total EUR/month | Share of priced cost |
|---|---:|---:|---:|---:|---:|
| monitoring | 226.15 | not measured | not measured | ≥ 226.15 | ≥ 39.0% |
| dashboards | 8.07 | 151.77 | 1.66 | 161.50 | 27.8% |
| steady | 60.41 | 1.39 | 2.34 | 64.15 | 11.1% |
| rules | 7.36 | 4.03 | 52.29 | 63.69 | 11.0% |
| cardinal | 40.27 | 2.20 | 2.08 | 44.54 | 7.7% |
| churn | 11.96 | 0.14 | 0.56 | 12.67 | 2.2% |
| latecomer | 4.05 | 0.46 | 0.59 | 5.10 | 0.9% |
| ghost | 1.69 | 0.00 | 0.31 | 1.99 | 0.3% |
| tiny | 0.04 | 0.00 | 0.17 | 0.21 | 0.0% |

**Read before quoting:**

- rules are evaluated remotely, so a tenant's rule queries drive its share of both the query path (the work) and the ruler (the time spent waiting for it). Each pool's cost is split once, so nothing is counted twice, but a rule-heavy tenant's total weighs the same queries in both
- a tenant marked ≥ was not measured in every priced pool: what it would owe there is unknown, not zero, and that pool's cost is shared out among the tenants that were measured, so its figure is a lower bound and theirs are inflated

Not modelled yet: ingester write path, object storage, store-gateway, compactor.
```

Note the headline: it says *39.0% of the cost of the priced pools*, not "of your Mimir bill". A total across pools always says what it covers, and here `platform_cost_month` wasn't set. `monitoring` sent no queries and ran no rules, so it's "not measured" in those pools, and its total is marked as a lower bound (≥).

**Checked against known ground truth.** On a staging Mimir 3.2.0 with synthetic tenants, run once with each write path, promcost passed all 87 checks in each run. The checks covered: every pool's shares sum to 100%; each tenant's active series match both the generated ground truth and Mimir's own gauge after dividing by the replication factor; the write path and the rule evaluation mode are detected correctly; and unmeasured tenants read "not measured". Pools 6 and 7 were checked for ranking only, not absolute values. Windows were at most 1h, on a single container. [Results, and what they don't show](dev/staging/results/README.md).

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

Binaries for linux and darwin (amd64, arm64) and windows (amd64) are on [GitHub Releases](https://github.com/KorhanOzturk90/obscost/releases), with a `checksums.txt`:

```sh
# Download and verify (Linux; on macOS use OS=darwin and `shasum -a 256 -c`)
VERSION=0.1.0 OS=linux ARCH=amd64   # or arm64
BASE=https://github.com/KorhanOzturk90/obscost/releases/download/v${VERSION}
curl -sSLO "$BASE/promcost_${VERSION}_${OS}_${ARCH}.tar.gz"
curl -sSLO "$BASE/checksums.txt"
grep "promcost_${VERSION}_${OS}_${ARCH}.tar.gz" checksums.txt | sha256sum -c
tar xzf "promcost_${VERSION}_${OS}_${ARCH}.tar.gz" promcost
./promcost --version

# Or with Go 1.27.1+
go install github.com/KorhanOzturk90/obscost/cmd/promcost@v0.1.0
```

Or run the multi-arch container image:

```sh
# Container (multi-arch). A promcost.yaml pointing at localhost needs --network host (Linux);
# otherwise use an address the container can reach.
docker run --rm -v "$PWD/promcost.yaml:/etc/promcost/promcost.yaml:ro" \
  ghcr.io/korhanozturk90/promcost:0.1.0 \
  cost --config /etc/promcost/promcost.yaml --metrics-tenant <tenant-with-mimir-self-metrics>
```

To run it in Kubernetes as a one-off Job or a daily CronJob, see [`deploy/kubernetes/`](deploy/kubernetes/README.md).

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
- **Under remote rule evaluation**, the ruler's queries are counted in the query path. The report infers the evaluation mode from metrics and says which one it found. When it can't tell, it says "unknown", and you can state the mode in config.
- **It's a snapshot over one window.** It doesn't compare windows or store history yet.

If your metrics tenant holds several Mimir clusters, pick one with `--selector '{cluster="..."}'`. promcost warns you when you need to. See [Prerequisites](docs/prerequisites.md#known-limitations-in-v01) for the known limitations.

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

**v0.1** covers three pools (ingester memory, query path, ruler CPU) as a one-off snapshot, in resources and optional currency. It's tested against Mimir 3.2.0 (Helm chart 6.2.0), with ingest storage and classic, zone-aware ingesters, and remote rule evaluation through the dedicated ruler query path. A separate run checked local evaluation. See the [validation results](dev/staging/results/README.md).

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
