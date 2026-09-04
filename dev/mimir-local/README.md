# Local self-monitoring Mimir

A standalone Docker Compose stack: one Grafana Mimir instance (monolithic,
single-binary mode) monitoring itself, with the [mimir-mixin's compiled
recording/alerting rules and dashboards][mixin] loaded in, backed by MinIO
for object storage and visualized in Grafana.

[mixin]: https://github.com/grafana/mimir/tree/main/operations/mimir-mixin-compiled

This exists to give `promcost` something real to point at: a live Mimir
instance, with a live ruler evaluating a real-world rule set, producing
real metrics — rather than only static fixture files.

**Not yet built here, and deliberately deferred:** the microservices split
(distributor/ingester/querier/etc. as separate processes). That's the
planned next step, moving this into Kubernetes, where each Mimir component
becomes its own Deployment. This rig is single-process on purpose, to keep
the first pass simple — but it **is** genuinely multi-tenant:
`-auth.multitenancy-enabled=true`, every request requires `X-Scope-OrgID`,
and two tenants are provisioned:

- **`infra`** — holds the real self-monitoring data and the mixin's rules.
- **`sandbox`** — deliberately empty (0 rule groups, no data), so you can
  prove tenant isolation directly: query it and get nothing back, even
  though `infra` right next to it is full of data.

## What's running

| service | image | purpose |
|---|---|---|
| `mimir` | `grafana/mimir:3.2.0` | `-target=all,alertmanager` — query, ingest, ruler, alertmanager, compactor, everything in one process |
| `minio` | `minio/minio:RELEASE.2025-09-07T16-13-09Z` | S3-compatible object storage for Mimir's blocks + alertmanager config |
| `alloy` | `grafana/alloy:v1.19.2` | scrapes both Mimir's `/metrics` and its own `/metrics` (job `mimir` and job `alloy`), remote-writing both back into Mimir — Alloy monitors itself too, not just Mimir |
| `grafana` | `grafana/grafana:11.5.2` | Prometheus datasource pointed at Mimir + the mixin's dashboards provisioned |

Note on `-target=all,alertmanager`: Mimir's `-target=all` deliberately
**excludes** the alertmanager component (its own docs describe `all` as
"components required to form a functional instance," and alertmanager
isn't considered required) — you have to opt in explicitly. Easy to miss;
cost some debugging time here.

**Object storage**: MinIO (S3-compatible) for Mimir's blocks and
alertmanager config storage — chosen over the local-filesystem backend
specifically so this config carries over to a real S3-compatible store on
Kubernetes later. **Ruler storage is the one exception**: it uses Mimir's
`local` backend (`-ruler-storage.backend=local`), scanning
`mixin/rules/<tenant>/*.yaml` (`infra/` and `sandbox/`, per-tenant
subdirectories — this is how Mimir's ruler expects local rule storage to
be laid out once multi-tenancy is on) directly, rather than S3. Mimir
explicitly supports and documents this for a static, hand-provisioned rule
set — it avoids needing `mimirtool` to push rules into an object-storage
bucket just to load a fixed mixin. Revisit this if/when rule management
needs to go through the ruler API instead.

## First-time setup

```bash
./fetch-mixin.sh          # downloads mimir-mixin-compiled (rules + dashboards) into ./mixin/ — gitignored, not vendored
docker compose up -d
```

`fetch-mixin.sh` defaults to pulling the mixin at the same release tag as
the pinned Mimir image (`mimir-3.2.0`) — override with `MIXIN_REF=... ./fetch-mixin.sh`
if you bump `MIMIR_IMAGE_TAG` in `docker-compose.yml`, so the mixin's
recording rules don't drift against metric names from a different Mimir
version.

Mimir takes ~15-30s after container start to report ready (its ingester
has a startup grace period). There's no Docker-level healthcheck on the
`mimir` service — the `grafana/mimir` image is distroless (no shell, no
wget/curl), so there's nothing to run a `CMD` healthcheck with. Poll
`http://localhost:8080/ready` from the host instead:

```bash
until [ "$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/ready)" = "200" ]; do sleep 5; done
```

## URLs

- Grafana: <http://localhost:3000> (`admin` / `admin` — change or ignore, this is local-only; anonymous access is also enabled, as **Editor**, not Grafana's usual anonymous-Viewer default — Viewer doesn't get the `datasources:explore` RBAC permission by default, so Explore would otherwise be invisible without logging in). Its default datasource is the `infra` tenant; a second, non-default datasource points at `sandbox` — switch to it in Explore or a dashboard's datasource picker (the mixin's dashboards all expose one) to see the isolation firsthand.
- Mimir HTTP API: <http://localhost:8080> (Prometheus-compatible query API under `/prometheus`, ruler under `/prometheus/api/v1/rules`, alertmanager under `/alertmanager`) — **every request needs an `X-Scope-OrgID: infra` or `X-Scope-OrgID: sandbox` header**, multi-tenancy is enforced (a request with no header gets `401`).
- MinIO console: <http://localhost:9001> (`mimir` / `supersecret123`)
- Alloy UI (scrape/remote_write debugging): <http://localhost:12345>

All credentials above are dev-only defaults, hardcoded in
`docker-compose.yml` — never reuse them anywhere internet-facing.

## Verifying it's working

```bash
# no tenant header -> rejected (multi-tenancy is enforced)
curl -s -o /dev/null -w '%{http_code}\n' 'http://localhost:8080/prometheus/api/v1/query?query=up'   # expect 401

# self-monitoring loop: Mimir ingesting metrics about itself, landed in "infra"
curl -s -H 'X-Scope-OrgID: infra' 'http://localhost:8080/prometheus/api/v1/query?query=up' | jq
# expect two results here: up{job="mimir-local/mimir"} and up{job="alloy"} — Alloy scrapes itself too

# tenant isolation: the exact same query against "sandbox" returns nothing
curl -s -H 'X-Scope-OrgID: sandbox' 'http://localhost:8080/prometheus/api/v1/query?query=up' | jq '.data.result'   # expect []

# ruler picked up the mixin's rules into "infra" (expect ~33 groups, ~244 rules, 0 errors)
curl -s -H 'X-Scope-OrgID: infra' 'http://localhost:8080/prometheus/api/v1/rules' | jq '.data.groups | length'

# "sandbox" has zero rule groups, on purpose
curl -s -H 'X-Scope-OrgID: sandbox' 'http://localhost:8080/prometheus/api/v1/rules' | jq '.data.groups | length'   # expect 0

# a recording rule producing real output (give it ~2 scrape/eval cycles after startup)
curl -sG -H 'X-Scope-OrgID: infra' 'http://localhost:8080/prometheus/api/v1/query' \
  --data-urlencode 'query=cluster_job:cortex_request_duration_seconds:99quantile' | jq

# alertmanager reachable and has loaded the fallback config (also tenant-scoped)
curl -s -H 'X-Scope-OrgID: infra' http://localhost:8080/alertmanager/api/v2/status | jq '.cluster.status'

# ruler query-stats logging (-ruler.query-stats-enabled=true): one logfmt
# "query stats" line per rule evaluation on Mimir's stdout — this is the
# real telemetry format internal/telemetry/mimirlogs parses. Expect one
# line per rule per eval cycle, tagged user=infra, none for sandbox.
docker compose logs mimir --since 2m | grep 'component=ruler' | grep 'msg="query stats"' | head -3
```

In Grafana, open the **Mimir Mixin** folder — e.g. "Mimir / Overview" —
and confirm panels render (may take a couple of minutes after first
startup for enough scrape history to populate) using the default `infra`
datasource; switch the dashboard's `datasource` variable to the `sandbox`
one to see the same panels come back empty.

## Stopping / resetting

```bash
docker compose down          # stop, keep data (blocks, dashboards config, etc.)
docker compose down -v       # stop and wipe all state — start completely fresh next time
```

## Known limitations of this pass

- Only two tenants, one of them intentionally empty — not the full
  scenario matrix (distinct synthetic load per tenant: cardinality, churn,
  limit-hugging, etc.) that a real multi-tenant test rig would want. That
  fuller matrix is still a separate, later task if/when it's needed.
- Single process (`-target=all,alertmanager`) — no sharding, no real
  replication (`replication-factor=1` everywhere), not representative of
  how Mimir behaves under real multi-instance failure/rebalancing
  scenarios.
- Alertmanager fallback config (`config/alertmanager-fallback.yaml`) has a
  bare default receiver — no real notification integration. Good enough to
  prove alerts reach "firing" and route somewhere; not useful for actually
  being notified of anything.
- `promcost` itself isn't pointed at this yet — that's a deliberate
  follow-up, not done here.
- The mixin dashboards are built for Kubernetes-SD-style labeling: they
  populate their `$cluster`/`$namespace` template variables from
  `cortex_build_info`'s `cluster`/`namespace` labels, and every panel query
  filters on `job=~"($namespace)/(component-regex)"` — i.e. `job` is
  expected to already be namespace-prefixed. Alloy has no Kubernetes SD
  here to supply any of that automatically, so `config/alloy/config.alloy`
  sets it by hand (`cluster="local"`, `namespace="mimir-local"`,
  `job_name="mimir-local/mimir"`). Without it, every dashboard panel shows
  "No data" even though Mimir is ingesting and querying fine — a mismatch
  between exported labels and mixin assumptions, not a broken pipeline.
