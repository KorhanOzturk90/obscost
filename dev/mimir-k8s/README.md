# Microservices Mimir on Kubernetes (k3d)

A dev-like Grafana Mimir for testing promcost's cost model against things
the docker-compose rig in [`../mimir-local`](../mimir-local) structurally
cannot show:

| | `mimir-local` (compose) | `mimir-k8s` (this) |
|---|---|---|
| Mimir processes | 1 (monolithic) | one pod per component |
| Replication factor | 1 | 3, across 3 ingesters |
| Per-component CPU / memory | not separable | cAdvisor `container_*` per pod |
| Ruler evaluation | local | remote, through the query-frontend |
| Write path | classic | classic **or** ingest storage (Kafka) |
| Tenants | `infra` (+ empty `sandbox`) | `analytics` (2 teams), `infra`, `payments`, `platform`, `monitoring` |
| Rollouts, scaling | — | `kubectl` |

Both can run at once: this one is on `localhost:8090`, the compose rig on
`8080`.

## Requirements

- Docker with **at least 8 GB** of memory (Docker Desktop → Settings →
  Resources). The classic stack measured ~2.3 GB for the k3d node on
  first run; ingest storage adds Kafka.
- `brew install k3d helm` (kubectl is assumed).
- CPU is capped separately: `make up` limits the k3d node to 4 cores with
  `docker update` (`make up CPUS=6` to change it). Docker Desktop usually
  allows every core, and uncapped the stack makes a laptop lag.

## Use

```bash
make up                 # cluster + Mimir + Alloy + Grafana + tenants + rules (~5–10 min first time)
make up MIXIN_RULES=1   # also sync the mimir-mixin's recording rules (see Grafana, below)
make status             # pods, and per-tenant active series as Mimir sees them
make cost               # promcost cost against the rig (build ../../bin/promcost first)
make down               # delete the whole cluster
k3d cluster stop obscost   # pause cleanly (frees CPU/memory, keeps data); `k3d cluster start obscost` resumes
```

**Grafana: <http://localhost:3090>** (no login). Start with
*obscost → obscost — tenant cost drivers*: each ADR 0003 pool's driver per
tenant, next to what the Mimir components actually use (cAdvisor). The
*Mimir Dashboards* folder has the full mimir-mixin set from the same chart
version — *Tenants* and *Top tenants* are the useful ones here. Several of
their panels read the mixin's ~120 recording rules, which are **not** loaded
by default: they evaluate every minute through the query path, a noticeable
share of a laptop rig's CPU. `make up MIXIN_RULES=1` syncs them into the
`monitoring` tenant; without it those panels stay empty. There is one
datasource per tenant (`Mimir (analytics)`, …) for Explore.

`make up ARCH=ingest` deploys Mimir 3.x's default **ingest-storage**
architecture instead (distributors → Kafka → ingesters). Switching an
existing cluster between the two is not a supported Mimir migration —
`make down` first.

## What's running

**Namespace `mimir`** — `mimir-distributed` 6.2.0 (Mimir 3.2.0) with
[`values/mimir.yaml`](values/mimir.yaml): 3 ingesters, 1 of every other
component, MinIO for object storage, nginx gateway on NodePort 30090. No
zone awareness, no rollout-operator, no memcached — each would cost memory
without changing anything promcost measures. Distributors and ingesters
run Mimir's `by-team` cost-attribution tracker; the cardinality API is on.

**Alloy** ([`alloy/config.alloy`](alloy/config.alloy)) writes two kinds of
data:

- into tenant **`monitoring`**: every Mimir pod's `/metrics`, the
  cost-attribution registry (`/usage-metrics`), and cAdvisor CPU / memory
  for the `mimir` namespace. This is the `--metrics-tenant`.
- into each simulated tenant: its avalanche pods, labelled with `team`.

**Namespace `tenants`** — [avalanche](https://github.com/prometheus-community/avalanche)
generators, sized in [`scripts/gen-tenants.py`](scripts/gen-tenants.py):

| tenant | team | active series (real, before RF) |
|---|---|---:|
| analytics | bi | 8,000 |
| analytics | ml | 5,000 |
| infra | sre | 7,000 |
| payments | payments | 1,200 |
| platform | platform | 100 |

`monitoring` is a tenant too, and a real one — Mimir's own metrics are
usually the biggest single tenant on small clusters, and a cost report that
leaves it out is wrong.

**Rules** ([`rules/<tenant>/`](rules)) are pushed with `mimirtool rules
sync` from a one-shot pod ([`scripts/sync-rules.sh`](scripts/sync-rules.sh)).
They are shaped to separate the pools: `analytics` has cheap rollups,
`payments` has few rules but expensive long-range quantiles (ruler / query
CPU, not memory), `platform` has almost nothing.

## Costing and calibration

```bash
make podcost                 # what each component costs, derived from node prices
make calibrate-series        # coefficient: ingester memory per active series (~1h)
make calibrate-query         # what query load costs ingesters and queriers (~30m)
```

[`scripts/podcost.py`](scripts/podcost.py) implements [ADR 0006](../../docs/adr/0006-pricing-pools-on-a-shared-cluster.md)
decision 3 — `price × max(request, usage) / node capacity`, per pod, summed
per component — because on a shared cluster nobody is billed for
"ingesters". Idle capacity is printed as its own line and never spread
across components.

[`scripts/calibrate.py`](scripts/calibrate.py) sweeps one input across
several values and fits a line, where a scenario changes it once and checks
a prediction. The output is a coefficient and an R²: "ingester memory is
driven by active series" is only useful once it reads "N KiB per series,
R² = 0.9x".

## Scenarios

`./scripts/scenarios.py <path/to/promcost>` runs all three in order and
logs prediction vs measurement (~45 min).

Each changes one thing, so its effect on a promcost report can be predicted
before it is measured. That prediction is the test.

| command | changes | expected effect |
|---|---|---|
| `make scale TARGET=avalanche-analytics-bi REPLICAS=2` | +8,000 analytics series | analytics' share of ingester memory rises; `by-team` shows it is `bi` |
| `make rollout-ingesters` | every ingester pod replaced | a window spanning it must **not** show a jump in active series (sum-then-average query) |
| `make scenario-expensive-rule` / `-off` | two analytics rules with one output series each but expensive queries (6h quantile subquery, 1h/15s subquery) | analytics' rule-evaluation and query time jump; querier CPU rises; active series +2, so `promcost cost` (pool 1 only) does **not** move — the ADR 0003 [A2] case |
| `make up ARCH=ingest` (after `make down`) | ingest storage | the classic-only active-series query finds nothing — see below |

## Findings so far

Mimir 3.2.0 / chart 6.2.0. Scenario numbers are from `scripts/scenarios.py`
on the classic write path, 2026-09-20.

**Scenario 1 — rolling the ingesters (4m19s), over a 7m window**

| tenant | before | promcost (sum, then average) | average each series, then sum | lowest point |
|---|---:|---:|---:|---:|
| analytics | 13,016 | 11,776 | **26,030** | 8,676 |
| infra | 7,007 | 6,340 | **14,014** | 4,671 |

Averaging each series before summing reads **exactly double**: a restarted
pod gets a new IP, so `cortex_ingester_active_series` becomes a *new*
series, and both the old and the new one contribute their own average.
promcost's sum-then-average reads 9% low instead (the sum dips to ~2/3
while one ingester is down) and recovers. This is the ADR 0003 [A]
aggregation rule paying for itself.

**Scenario 2 — one more replica for `analytics`/`bi`**

+8,009 series against a predicted +8,000, all of it under team `bi` in the
cost-attribution tracker (8,007 → 16,012 real series) with `ml` unmoved.
Scaling back down did **not** reduce the count for 16 minutes and had only
partly decayed at 24: Mimir keeps a series active for 20 minutes after its
last sample, so removing load does not reduce a tenant's memory share
until that expires.

**Scenario 3 — two expensive rules that write one series each**

| | before | after |
|---|---:|---:|
| analytics rule evaluation (5m) | 3.23s | **15.22s** |
| analytics query time (5m) | 16.01s | **76.33s** |
| querier CPU | 0.052 cores | 0.119 cores |
| analytics active series | 13,014 | **13,016** |

Exactly the ADR 0003 [A2] case: the tenant's cost on pools 6 and 7 roughly
quintuples while pool 1 moves by two series, so `promcost cost` as it
stands reports nothing at all.

**And a finding nobody predicted: contention leaks into wall time.**
`infra`, which was not touched, also doubled — evaluation 3.94s → 8.89s,
query time 25.44s → 58.38s — because analytics' rules were monopolising
the shared querier. Any attribution that ranks by *wall time* charges a
tenant for its neighbours' behaviour; data volume fetched does not move
like this. ADR 0002 decision 8 already prefers fetched volume over wall
time; this is the first measurement showing why it matters for cost, not
just for ranking.

**Earlier findings**

- **The chart's default architecture is ingest storage, not classic.**
  Under it every ingester exports `cortex_partition_ring_partitions`, so
  the classic active-series query (whose guard excludes exactly those
  ingesters) returns **nothing**, and `cortex_distributor_replication_factor`
  is not exported at all. promcost's pool-1 query now detects this (#40).
- **Classic, RF=3 works end to end**: raw per-tenant sums were exactly 3×
  the generated series, and `promcost cost` reported the real figures.
- **Cost-attribution metrics are also pre-replication** under classic:
  the `by-team` tracker reads 3× the real series count.
- **The `monitoring` tenant is not small** — 25–35% of ingester memory
  here. Costing only "customer" tenants hands its share to everyone else.
- **`/distributor/all_user_stats` under ingest storage** reported 39,014
  series for analytics, 3× the real 13,010, with no replication at all.
  Unexplained; one suspect is `ingester.ring.replication_factor: 3` left
  set by values/mimir.yaml, which the ingest overlay does not reset.
- **A scenario is only as good as its baseline.** The first run started
  while the rig was still filling up and read +10,608 where the change was
  +8,000. `scripts/scenarios.py` now waits for active series to stop
  moving before and after each scenario.
- **Resource use, once tuned** (see values/mimir.yaml): 0.51 cores and
  1.43 GiB across all Mimir containers, 2.55 GiB for the k3d node.
  Untuned it was 4.4 cores and 5.8 GiB, with the Kubernetes API timing
  out — no memory limits, the chart's 1 GiB ruler ballast, the mixin's
  ~120 recording rules, and 15s scrapes.

## Laptop vs VM

This runs inside Docker, so on a Mac it already runs in a VM (Docker
Desktop's). A *local* VM (UTM, Lima, Multipass) adds isolation but no
capacity. A **cloud VM** is worth it when you need any of:

- windows longer than a laptop stays awake (24h / 7d averages, daily
  rollups for #32);
- resource numbers that mean something for calibration (#34) — real CPUs,
  no laptop contention, and a real instance price for the inventory;
- more ingesters, zone-aware replication (needs the rollout-operator), or
  a bigger tenant mix than 8 GB allows.

Nothing here is laptop-specific: on a VM, install Docker + k3d + helm and
run the same `make up` (a 4 vCPU / 16 GB machine is plenty), or point
`helm` at a real k3s / managed cluster and skip `make cluster`. Reach it
with an SSH tunnel: `ssh -L 8090:localhost:8090 <vm>`.
