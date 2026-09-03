# Testing against Grafana Cloud's free tier

promcost's `Meter` (`internal/meter`) can talk to any Mimir/Prometheus-compatible
instant-query API. Grafana Cloud's free tier (10k active series, hosted Mimir,
no credit card) is a convenient real backend for exercising it during
development, since no open public multi-tenant Mimir instance exists to test
against.

This is a manual setup — get exact click-paths from the live portal, not from
this doc; Grafana Cloud's UI changes over time.

## 1. Create a stack and an access policy token

1. Sign up for the free tier at https://grafana.com/products/cloud/free-tier/
   and create a stack.
2. In the stack's Cloud Portal, create a **Cloud Access Policy** scoped to at
   least `metrics:read` (add `metrics:write` too now — a later
   self-instrumentation pass will need it, and Access Policy scopes aren't
   worth re-doing).
3. Generate a token for that policy. Treat it like a password — never commit
   it, never put it inline in `promcost.yaml`.

## 2. Find your instance ID and query URL

From the stack's Prometheus/Mimir data source details page, note:

- **Metrics instance ID** — this is the HTTP Basic Auth *username*.
- **Query API URL** — this is `backend.url` in `promcost.yaml` (a
  `.../api/prom`-shaped URL specific to your stack; don't reuse an example
  from anywhere else).
- The Access Policy token from step 1 is the HTTP Basic Auth *password*.

## 3. Configure promcost

Export the two credentials as env vars (names must match `promcost.yaml`'s
`backend.auth.username_env`/`password_env`):

```sh
export PROMCOST_GC_INSTANCE_ID=<your instance ID>
export PROMCOST_GC_TOKEN=<your access policy token>
```

Fill in `backend.url` in `promcost.yaml` with your stack's actual query URL
(it ships as a `<cluster>.grafana.net`-shaped placeholder).

Then:

```sh
./bin/promcost check --dir testdata/mimir-rec-rules --config promcost.yaml
```

A reachable, correctly authenticated backend runs silently past the Meter
construction/probe step. An unreachable or misconfigured one prints a
`warning: backend unreachable, ...` and degrades to static-only findings
(exit 0) — add `--strict` to make that a hard failure (exit 3) instead, e.g.
for a CI job that should fail loudly on a broken backend rather than
silently losing live-tier coverage.

## Note: the Mimir mixin rules won't produce real data here

`testdata/mimir-rec-rules/mimir_rec_rules.yaml` queries Mimir's own internal
`cortex_*` engine metrics. A Grafana Cloud tenant doesn't expose Grafana's
own infrastructure metrics into your queryable namespace, so pushing this
rule set to your tenant's ruler (via `mimirtool rules load`/`sync` — a
manual, external-tool step promcost's code doesn't do) will produce recording
rules that evaluate to no data. It's still useful for exercising the
ruler-push mechanics themselves, just not as a source of real series.

## Cost-model calibration reference (later)

CLAUDE.md §8.6 notes promcost's cost-model constants
(`eur_per_million_active_series_month` etc.) need calibrating against a real
infra bill, not invented values. Grafana Cloud's **Cardinality Management**
and **usage & billing** dashboards (in the Cloud Portal) show real per-metric
series counts and cost against your test tenant — worth using as a reference
point once that calibration work starts. No promcost code depends on this
today; it's just where to look.
