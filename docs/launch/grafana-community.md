# Grafana community forum draft

Category: **Grafana Mimir**. Tags: `mimir`, `multi-tenancy`, `cost`.

## Title

```
PRODUCTNAME: splitting shared Mimir component cost across tenants (open source)
```

## Body

---

Hi all. I've released the first version of PRODUCTNAME, an Apache-2.0 CLI for one question on self-hosted Mimir: **what share of the shared components does each tenant use?** I'd value feedback from people who run Mimir for several teams.

Repo: https://github.com/OWNER/REPO

### How it relates to what Mimir already has

Mimir already has good tools here, and PRODUCTNAME doesn't replace them.

- **Cost attribution** (`-validation.cost-attribution-trackers`, experimental in 3.x) splits samples and active series *inside* a tenant by a label such as `team`. It's the right tool for "which team inside this tenant".
- **The `mimir-top-tenants` dashboard** shows who is biggest on each axis: series, samples, rule time and so on.

PRODUCTNAME answers a different question. It works *across* tenants, and for each shared component it splits that component's cost by the physical driver that makes it grow:

| Pool | Driver |
|---|---|
| Ingester memory | `cortex_ingester_active_series`, averaged over the window, divided by the replication factor |
| Query path (query-frontend + querier) | `cortex_query_fetched_chunk_bytes_total`, falling back to samples processed, series fetched, then query seconds |
| Ruler CPU | `cortex_prometheus_rule_evaluation_duration_seconds_sum` |

The result is a share of each pool that sums to 100% across all tenants, not a `topk`. It also gives a replica equivalent ("the equivalent of 5.0 of your 8 ingesters"), and currency only if you add your own price per replica. The two tools work well together: PRODUCTNAME says which tenant, and cost attribution says which team inside it.

### Details Mimir operators will ask about

- **Replication.** Under the classic write path it divides by `cortex_distributor_replication_factor`. Under ingest storage there's one owner per partition and zone replicas are collapsed with `max`, so there's nothing to divide. It detects which write path you're on and says so in the report.
- **Averaging.** Active series is summed by `user` inside a subquery, then averaged over the window, as the `mimir-top-tenants` mixin does.
- **Remote rule evaluation.** The ruler's queries go through the query-frontend, so they're counted in the query path. The report infers the evaluation mode from metrics and says which one it found. You can also set it in config.
- **Several clusters in one metrics tenant.** Pick one with `--selector '{cluster="..."}'`.
- **What it reads.** Only `GET` requests: `/prometheus/api/v1/query` as the metrics tenant, plus the ruler API and per-tenant count queries if you use the `--rule-outputs` drill-down.

### Validation

It was tested on a staging Mimir 3.2.0 (chart `mimir-distributed` 6.2.0, microservices, zone-aware, RF=3, ingest storage) with 8 synthetic tenants of known shape, and two ingester rolling restarts during the measured windows. The first run passed 79 of 83 checks. Every failure came from one bug, now fixed: `avg_over_time` only averages over the points where a series exists, and the ingester drops a tenant's active-series series once it has none. So a tenant that joined or left mid-window was averaged over its own presence (#66). TODO(evidence: the #66 re-run result and link.) A second run with local rule evaluation passed 33 of 34. TODO(evidence: classic write-path staging report.)

Reports: https://github.com/OWNER/REPO/tree/main/dev/staging/results

### Where I'd like help

- Does the driver choice match your experience? Especially the fetched chunk bytes for the query path.
- Mimir versions other than 3.2.0. The metrics are long-standing, but I've only checked names and labels on 3.2.0.
- Pools I don't model yet: write path, object storage, store-gateway and compactor. If you know a per-tenant driver for the store-gateway or compactor that you trust, I'd like to hear it.

---
