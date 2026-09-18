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
  Resources). The classic stack settles around 4–5 GB; ingest storage adds
  Kafka.
- `brew install k3d helm` (kubectl is assumed).

## Use

```bash
make up                 # cluster + Mimir + Alloy + tenants + rules (~5–10 min first time)
make status             # pods, and per-tenant active series as Mimir sees them
make cost               # promcost cost against the rig (build ../../bin/promcost first)
make down               # delete the whole cluster
```

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

## Scenarios

Each changes one thing, so its effect on a promcost report can be predicted
before it is measured. That prediction is the test.

| command | changes | expected effect |
|---|---|---|
| `make scale TARGET=avalanche-analytics-bi REPLICAS=2` | +8,000 analytics series | analytics' share of ingester memory rises; `by-team` shows it is `bi` |
| `make rollout-ingesters` | every ingester pod replaced | a window spanning it must **not** show a jump in active series (sum-then-average query) |
| `make scenario-heavy-rule` / `-off` | one recording rule re-emitting every analytics series | analytics roughly doubles; `RuleIngestionRate` share rises; #37 should name the rule |
| `make up ARCH=ingest` (after `make down`) | ingest storage | the classic-only active-series query finds nothing — see below |

## Findings so far

First runs, 2026-09-18, Mimir 3.2.0 / chart 6.2.0:

- **The chart's default architecture is ingest storage, not classic.**
  Under it every ingester exports `cortex_partition_ring_partitions`, so
  the classic active-series query (whose guard excludes exactly those
  ingesters) returns **nothing**, and `cortex_distributor_replication_factor`
  is not exported at all. Per-tenant series need the mixin's other branch:
  max across zone replicas of a partition, summed across partitions, no
  divisor. promcost's pool-1 query now detects this (PR #40).
- **Classic, RF=3 works end to end**: raw per-tenant sums were exactly 3×
  the generated series (analytics 39,030 raw → 13,010), and `promcost cost`
  reported 13,010.
- **Cost-attribution metrics are also pre-replication** under classic:
  `cortex_ingester_attributed_active_series{team="bi"}` reads ~24,000 for
  8,000 real series, summed across ingesters. The sub-tenant tier needs the
  same divisor as pool 1.
- **The `monitoring` tenant is not small**: ~10k real series, 25–30% of
  ingester memory here. Costing only "customer" tenants would hand its
  share to everyone else.
- **Ingester working set** on the classic run: ~635 MiB across 3 pods for
  ~99k in-memory series (33k per pod, replicas included) — the first
  data point for calibrating memory-per-series (#34); a laptop is not where
  that number should be finalized.

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
