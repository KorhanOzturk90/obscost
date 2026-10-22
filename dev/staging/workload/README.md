# Workload with ground truth

`loadgen` drives a set of synthetic Mimir tenants whose shapes are known in
advance, and records what it **actually** produced. That turns
`dev/staging` from a demo into a test. Calibrating an odometer needs a
track with surveyed distances, and this is the survey.

```
profiles/standard.yaml ─► loadgen ─┬─ remote write  /api/v1/push              ┐
                                   ├─ range queries /prometheus/api/v1/query_range  ├─► Mimir gateway (X-Scope-OrgID per tenant)
                                   ├─ rules         /prometheus/config/v1/rules     ┘
                                   └─ ground truth  ground-truth.json, GET :9100/ground-truth ─► ../validate
```

## The tenants (`profiles/standard.yaml`)

| tenant | shape | live series | samples/s | queries | rules (rec + alert) | `record:` series |
|---|---|---:|---:|---|---:|---:|
| `steady` | large steady ingestion | 30,000 | 2,000 | 1 light panel / 5m | 4 + 2 | 22 |
| `cardinal` | high cardinality, low sample rate (1 metric, 20k series, padded labels, 60s interval) | 20,000 | 333 | — | 1 + 0 | 10 |
| `dashboards` | query-heavy: 3 viewers × 6 panels every 30s over 6h | 4,000 | 267 | 36/min | 2 + 2 | 11 |
| `rules` | rule-heavy: 240 rules (the mimir-mixin's size) every 1m over 2m ranges | 3,000 | 200 | — | 120 + 120 | 660 |
| `tiny` | tiny | 20 | 0.7 | — | 0 + 1 | 0 |
| `churn` | every series replaced every 10m, like a deploy | 2,000 (≈6,000 active) | 133 | — | 2 + 0 | 11 |
| `latecomer` | **starts 90m into the run** (rules pushed when it starts) | 2,000 | 133 | 1 panel / 1m | 2 + 1 | 11 |
| `ghost` | **goes silent 2h into the run** (rules stay loaded) | 3,000 | 200 | — | 2 + 0 | 11 |

Each shape is chosen so every pool has a clear expected answer. Pool 1's
rank order follows active series. `dashboards` dominates pool 6, and it
still does under remote rule evaluation, because its queries read far more
series-minutes than `rules`' evaluations do once an hour of data exists. On a
run younger than that, its 6h ranges have little to read, and the
harness reports the shortfall rather than hiding it. `rules` dominates
pool 7, and tenants that issue no queries must be "not measured" in pool 6
under local evaluation.

Every rule reads only its own tenant's generated metrics, so what it
writes is known in advance. Recording rules alternate between
`sum by (shard) (rate(m[r]))`, which writes one series per shard, and
`sum(rate(m[r]))`, which writes one. Alerting rules are
`… < 0` on a rate, which never fires, so they cost evaluation and write
nothing. Groups hold at most 20 rules, Mimir's default
`-ruler.max-rules-per-rule-group`.

## Ground truth

`ground-truth.json` (schema 1) holds facts, not expectations:

- the profile, and the run's start time (`start_after`/`stop_after` count from it);
- per tenant: series shape and samples/s; churn period; **segments**, meaning when samples were first and last accepted, one per generator lifetime, so a restart resumes the same run; when its rules were accepted by the ruler; rule counts by type and the expected series per `record:` name; query rate, range and step; and counters for samples sent and rejected, queries run and failed, and the last error.

Turning those facts into "what Mimir should report" needs Mimir's own
semantics: the 20m active-series idle timeout, the 1m gauge update, and
rule lookback. That model lives in [`../validate/model.py`](../validate/model.py),
next to the checks that use it, and not here.

## Run it

**Against `dev/mimir-local`** (Docker only):

```bash
make local-up      # dev/mimir-local + local/compose.override.yml, mixin rules into `infra`
make local-run     # loadgen on the host, state in .run/ground-truth.json
```

`local/compose.override.yml` changes three things about the stock rig.
MinIO is gone (its images are no longer on Docker Hub), the ruler store
is writable so rules go through the ruler API as on staging, and Grafana
is dropped. Everything promcost measures is unchanged: monolithic, RF=1,
classic write path, local rule evaluation.

**On the staging cluster** (`dev/staging`, after `make cluster deploy` there):

```bash
export KUBECONFIG=$PWD/../.state/kubeconfig
make image k8s-import k8s-deploy   # image → the k3s node's containerd → namespace loadgen
make k8s-truth                     # snapshot the in-cluster ground truth to .run/ground-truth-k8s.json
```

`make plan` prints the planned ground truth without sending anything.
`make rules` writes the generated rule files, the layout `promcost --dir`
reads.

## Not covered

- Native histograms, exemplars and OTLP are not generated. promcost's pools don't read them yet.
- The generator's own process restarts are recorded (a new segment), but a pod that is *rescheduled* on the cluster starts a new run. Its ground truth lives on an emptyDir, and the harness snapshots it on every run.
