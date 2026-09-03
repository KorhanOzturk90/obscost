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
(distributor/ingester/querier/etc. as separate processes) and real
multi-tenancy. Both are the planned next step, moving this into
Kubernetes, where each Mimir component becomes its own Deployment and
`-auth.multitenancy-enabled=true` becomes real. This rig is single-tenant
(everything lands in Mimir's implicit `anonymous` tenant) and
single-process on purpose, to keep the first pass simple.

## What's running

| service | image | purpose |
|---|---|---|
| `mimir` | `grafana/mimir:3.2.0` | `-target=all,alertmanager` — query, ingest, ruler, alertmanager, compactor, everything in one process |
| `minio` | `minio/minio:RELEASE.2025-09-07T16-13-09Z` | S3-compatible object storage for Mimir's blocks + alertmanager config |
| `alloy` | `grafana/alloy:v1.19.2` | scrapes Mimir's own `/metrics` and remote-writes the samples back into Mimir — this is the "self-monitoring" loop |
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
`mixin/rules/anonymous/*.yaml` directly, rather than S3. Mimir explicitly
supports and documents this for a static, hand-provisioned rule set — it
avoids needing `mimirtool` to push rules into an object-storage bucket
just to load a fixed mixin. Revisit this if/when rule management needs to
go through the ruler API instead.

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

- Grafana: <http://localhost:3000> (`admin` / `admin` — change or ignore, this is local-only; anonymous viewer access is also enabled)
- Mimir HTTP API: <http://localhost:8080> (Prometheus-compatible query API under `/prometheus`, ruler under `/prometheus/api/v1/rules`, alertmanager under `/alertmanager`)
- MinIO console: <http://localhost:9001> (`mimir` / `supersecret123`)
- Alloy UI (scrape/remote_write debugging): <http://localhost:12345>

All credentials above are dev-only defaults, hardcoded in
`docker-compose.yml` — never reuse them anywhere internet-facing.

## Verifying it's working

```bash
# self-monitoring loop: Mimir ingesting metrics about itself
curl -s 'http://localhost:8080/prometheus/api/v1/query?query=up' | jq

# ruler picked up the mixin's rules (expect ~33 groups, ~244 rules, 0 errors)
curl -s 'http://localhost:8080/prometheus/api/v1/rules' | jq '.data.groups | length'

# a recording rule producing real output (give it ~2 scrape/eval cycles after startup)
curl -sG 'http://localhost:8080/prometheus/api/v1/query' \
  --data-urlencode 'query=cluster_job:cortex_request_duration_seconds:99quantile' | jq

# alertmanager reachable and has loaded the fallback config
curl -s http://localhost:8080/alertmanager/api/v2/status | jq '.cluster.status'
```

In Grafana, open the **Mimir Mixin** folder — e.g. "Mimir / Overview" —
and confirm panels render (may take a couple of minutes after first
startup for enough scrape history to populate).

## Stopping / resetting

```bash
docker compose down          # stop, keep data (blocks, dashboards config, etc.)
docker compose down -v       # stop and wipe all state — start completely fresh next time
```

## Known limitations of this pass

- Single tenant only (`-auth.multitenancy-enabled=false`, everything under
  the implicit `anonymous` tenant).
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
