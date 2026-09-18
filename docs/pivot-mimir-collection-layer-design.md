# Collection Layer — Design Decisions & Implementation Plan

## Overview

The collection layer is a per-cluster agent responsible for gathering all signals needed to attribute infrastructure cost to Mimir tenants. It runs inside each Kubernetes cluster and ships data outward to BigQuery. It has three independent data paths: metrics scraping, Kubernetes CRD watching, and log parsing.

---

## Design Decisions

### 1. Per-cluster agent, not centralized pull

**Decision:** Deploy one agent per cluster, not a central service that reaches into clusters.

**Rationale:**
- No inbound firewall rules needed into production clusters
- Outbound-only network requirement (to BigQuery) is an easier security conversation
- Failure domain is per-cluster — one cluster's agent failing doesn't affect others
- Simpler auth model: Workload Identity per cluster, not a central service account with cross-cluster access

### 2. Deployment as a Kubernetes CronJob + Deployment hybrid

**Decision:** CronJob for the metrics scrape path, long-running Deployment for the CRD watch and log tail paths.

**Rationale:**
- Metrics scraping is periodic (every 5 minutes) — CronJob is the natural fit
- CRD watching requires a persistent connection to the Kubernetes API server — needs a Deployment
- Can be packaged as a single binary with two modes (`--mode=scraper` and `--mode=watcher`)

### 3. BigQuery as the destination store

**Decision:** BigQuery over Postgres, ClickHouse, or a second Mimir tenant.

**Rationale:**
- Native support for append-only streaming inserts at low cost
- Built-in BigQuery ML for time-series forecasting (`ARIMA_PLUS`) — useful for budget projection
- Managed, no operational overhead
- GCP billing export lands in BigQuery natively — simplifies the cost mapping join
- SQL interface familiar to most platform and data teams

### 4. Raw events over pre-aggregated rollups

**Decision:** Write raw 5-minute metric snapshots and raw CRD/log events to BigQuery. Compute rollups in SQL views, not in the agent.

**Rationale:**
- Data volume is negligible (~72,000 rows/day at 50 tenants, 5-minute intervals)
- Retaining raw events allows retroactive cost model changes without reprocessing
- Rollup logic in SQL views is easier to iterate on than agent code
- Daily rollup views can be materialized for query performance

### 5. Go as the implementation language

**Decision:** Go over Python.

**Rationale:**
- `client-go` is the first-class Kubernetes client library — CRD watching is well-supported
- Prometheus HTTP client libraries are mature
- Single static binary with no runtime dependencies — easy to deploy as a CronJob
- Low memory footprint suitable for a sidecar/agent workload

Python is acceptable for a prototype phase.

### 6. Query stats log capture via structured log parsing

**Decision:** Parse Mimir querier and ruler `query_stats` log lines as a third data path, not rely solely on metrics.

**Rationale:**
- Log lines capture the actual PromQL query, fetched series count, chunk bytes, and wall time — richer than counter metrics alone
- Enables query fingerprinting and "top N expensive queries" views
- `query_stats_enabled` is true by default in Mimir — no config change needed in most clusters

---

## BigQuery Schema

### Table: `tenant_metrics`

Raw per-tenant metric snapshots. Append-only.

```sql
cluster                    STRING    NOT NULL,
tenant                     STRING    NOT NULL,
timestamp                  TIMESTAMP NOT NULL,
active_series              INT64,
max_active_series          INT64,
samples_ingested           INT64,
ruler_query_count          INT64,
ruler_recording_series     INT64,
compactor_bytes_processed  INT64,
distributor_samples_total  INT64
```

Partition by `DATE(timestamp)`. Cluster by `cluster, tenant`.

---

### Table: `config_events`

CRD watch stream — one row per ADDED/MODIFIED/DELETED event.

```sql
cluster                    STRING    NOT NULL,
tenant                     STRING    NOT NULL,
timestamp                  TIMESTAMP NOT NULL,
event_type                 STRING,   -- ADDED | MODIFIED | DELETED
resource_kind              STRING,   -- PrometheusRule | PodMonitor | ServiceMonitor
namespace                  STRING,
name                       STRING,
rule_group                 STRING,
recording_rules_delta      INT64,
alerting_rules_delta       INT64,
estimated_new_series       INT64,
raw_diff                   JSON
```

Partition by `DATE(timestamp)`. Cluster by `cluster, tenant`.

---

### Table: `query_log_events`

Parsed `query_stats` log lines from querier and ruler.

```sql
cluster                    STRING    NOT NULL,
tenant                     STRING    NOT NULL,
timestamp                  TIMESTAMP NOT NULL,
component                  STRING,   -- ruler | querier
query_fingerprint          STRING,   -- SHA256 of normalized query
query_wall_time_seconds    FLOAT64,
fetched_series_count       INT64,
fetched_chunks_count       INT64,
fetched_chunk_bytes        INT64,
sharded_queries            INT64,
result_series_count        INT64,
raw_query                  STRING
```

Partition by `DATE(timestamp)`. Cluster by `cluster, tenant, query_fingerprint`.

---

### View: `daily_tenant_rollup`

Materialized daily view joining all three tables for cost attribution.

```sql
SELECT
  DATE(timestamp)                        AS date,
  cluster,
  tenant,
  AVG(active_series)                     AS avg_active_series,
  MAX(active_series)                     AS max_active_series,
  SUM(samples_ingested)                  AS total_samples_ingested,
  SUM(ruler_query_count)                 AS total_ruler_queries,
  SUM(ruler_recording_series)            AS total_recording_series_written,
  SUM(compactor_bytes_processed)         AS total_compactor_bytes
FROM tenant_metrics
GROUP BY 1, 2, 3
```

---

## Implementation Plan

### Phase 1 — Metrics scraper (CronJob)

**Goal:** Establish the core data pipeline. Tenant metrics flowing into BigQuery daily.

- [ ] Bootstrap Go project, define BigQuery client and schema
- [ ] Implement Mimir HTTP admin API client (tenant stats endpoint)
- [ ] Implement Prometheus metrics scrape client for per-tenant ruler/ingester/distributor metrics
- [ ] Write 5-minute snapshots to `tenant_metrics` table via streaming insert
- [ ] Deploy as a Kubernetes CronJob with Workload Identity for BigQuery auth
- [ ] Create `daily_tenant_rollup` materialized view
- [ ] Validate: run a PromQL cost attribution query against the view

**Deliverable:** Daily rollup data in BigQuery, queryable by tenant and cluster.

---

### Phase 2 — CRD watcher (Deployment)

**Goal:** Capture configuration change events and correlate them with cost spikes.

- [ ] Implement Kubernetes CRD watch loop using `client-go` for `PrometheusRule`, `PodMonitor`, `ServiceMonitor`
- [ ] Implement namespace → tenant ID mapping (via label/annotation config)
- [ ] Compute recording/alerting rule deltas on MODIFIED events
- [ ] Implement `estimated_new_series` heuristic (based on label cardinality of the new monitor target)
- [ ] Write events to `config_events` table
- [ ] Deploy as a long-running Deployment alongside the CronJob
- [ ] Validate: trigger a `PrometheusRule` change and confirm event appears in BigQuery within 60 seconds

**Deliverable:** Causal link between CRD changes and cost spikes visible in BigQuery.

---

### Phase 3 — Query log parser

**Goal:** Capture query-level cost signals from querier and ruler logs.

- [ ] Confirm `query_stats_enabled: true` on target clusters (default, but verify)
- [ ] Implement log tail / Cloud Logging subscriber for querier and ruler pods
- [ ] Parse `query_stats` structured log lines (regex or logfmt parser)
- [ ] Implement query normalization and SHA256 fingerprinting
- [ ] Aggregate to per-tenant, per-fingerprint, per-5-minute buckets before writing
- [ ] Write to `query_log_events` table
- [ ] Build "top 10 most expensive queries by tenant" BigQuery view
- [ ] Validate: identify the most expensive recording rule query per tenant

**Deliverable:** Per-query cost breakdown enabling rule hygiene recommendations.

---

### Phase 4 — Cost model and attribution engine

**Goal:** Convert raw usage signals into dollar figures per tenant.

- [ ] Define cost coefficient schema (cost per million series-day, per billion samples, per million ruler queries)
- [ ] Implement coefficient calibration tool: take a GCP/AWS billing export and fit coefficients to match actual cluster spend
- [ ] Implement shared infra attribution: store-gateway and compactor costs split proportionally by `(tenant_series / cluster_total_series)`
- [ ] Build `tenant_cost_daily` view joining rollup data with cost coefficients
- [ ] Validate: spot-check attributed costs against actual billing for 2–3 tenants

**Deliverable:** Dollar-denominated cost per tenant per day, queryable in BigQuery.

---

### Phase 5 — Pre-flight cost estimation (stretch)

**Goal:** Shift from reactive chargeback to proactive cost governance.

- [ ] Implement a webhook or CLI tool that accepts a `PodMonitor` / `PrometheusRule` manifest
- [ ] Estimate series cardinality from label selectors and existing cluster cardinality data
- [ ] Return estimated monthly cost impact before the resource is applied
- [ ] Optionally integrate as a Kubernetes admission webhook for automated pre-flight checks

**Deliverable:** Teams can estimate the cost of a configuration change before applying it.

---

## Open Questions

1. **Namespace → tenant mapping** — how are tenants identified in each cluster? Is `X-Scope-OrgID` consistently set as a namespace label, or does this need a manual config file per cluster?
2. **Log delivery** — are querier/ruler logs already shipping to Cloud Logging / a central Loki, or does the agent need to tail pod logs directly?
3. **Multi-cloud** — Phase 1 targets BigQuery (GCP). What's the priority for AWS Cost Explorer integration?
4. **Cost coefficient ownership** — who maintains the cost model coefficients as infra changes? Needs an owner and a refresh cadence.
5. **Ruler vs. querier query pricing** — agreed to use a single "query cost unit" for now. Revisit after Phase 3 data is available to see if the split is significant enough to model separately.
