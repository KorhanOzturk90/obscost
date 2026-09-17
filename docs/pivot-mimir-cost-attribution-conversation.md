# Mimir Cost Attribution — Design Conversation

## Background

A discussion exploring how to build a product for cost/resource attribution in a multi-cluster, multi-tenanted, self-hosted Grafana Mimir (metrics) solution. Grounded in real platform engineering experience maintaining such a setup.

---

## 1. Cost Attribution in Multi-Tenant Mimir

### Tenant-level metrics from Mimir itself

Mimir exposes per-tenant metrics out of the box:

- `cortex_ingester_ingested_samples_total{user="<tenant>"}` — samples ingested per tenant
- `cortex_distributor_received_samples_total{user="<tenant>"}` — samples received at the distributor
- `cortex_query_frontend_queries_total{user="<tenant>"}` — query volume per tenant
- `cortex_ingester_memory_series{user="<tenant>"}` — active series (correlates with memory/storage cost)

### Active series as the primary cost signal

Active series count is the most reliable proxy for storage and compute cost. Maps directly to ingester memory, long-term object storage writes, and compactor load.

### Practical chargeback model

1. Define a cost unit (e.g. cost per million active series-hours + cost per billion samples ingested)
2. Pull per-tenant metrics into a chargeback dashboard
3. Export a weekly/monthly report

---

## 2. Recording Rules and Alerting Rules as Cost Drivers

Often the hidden cost driver — they run server-side and are easy to overlook.

### Recording rules

Each evaluation is a query on a schedule (typically every 15–60s). Key metrics:

- `cortex_ruler_queries_total{user="<tenant>"}` — rule evaluations fired
- `cortex_ruler_query_duration_seconds{user="<tenant>"}` — time spent evaluating rules
- `cortex_ruler_rules{user="<tenant>", rule_type="recording"}` — number of active recording rules

Recording rule output is **new series written back into the ingester** — adding ingestion load on top of query load.

### Alerting rules

Cheaper on ingestion (don't write series back), but still generate ruler query load:

- `cortex_ruler_rules{user="<tenant>", rule_type="alerting"}`
- `cortex_ruler_queries_failed_total{user="<tenant>"}`

### Composite query cost signal

```promql
sum by (user) (
  rate(cortex_query_frontend_queries_total[1h])
  + rate(cortex_ruler_queries_total[1h])
)
```

---

## 3. Building a Product Around This

### Core insight: it's a data pipeline problem, not a dashboard problem

Building a Grafana dashboard and calling it chargeback breaks the moment you have multiple clusters, federated tenants, or cost that doesn't map 1:1 to a single metric. The real product is a **cost data platform with a UI on top**.

### Architecture: three layers

#### Collection layer
- A per-cluster scraper/agent pulling ruler, ingester, distributor, compactor, and query-frontend metrics per tenant
- Normalizes into a canonical cost-event schema: `{tenant, cluster, component, metric_type, value, timestamp}`
- Ships to a central store (BigQuery preferred)
- Also captures actual CPU/memory/storage costs per cluster node to convert metric units into dollar figures

#### Attribution engine
- Configurable cost model with weights per signal (series, samples/sec, ruler queries, compactor bytes)
- Amortization rules for shared infrastructure costs
- Overrides for SLA-tiered infrastructure (dedicated ingesters, etc.)
- Anomaly flagging for sudden cost spikes

#### Surface layer
- Per-team cost dashboard (embeddable in Grafana or standalone)
- Monthly report with trend lines and budget vs. actual
- Alerting: "Your team's ingestion cost increased 40% this week"
- Admin view for platform teams across all clusters

### Differentiation angles

- **Multi-stack support** — Mimir, Thanos, Cortex, VictoriaMetrics share the Prometheus data model; collection layer is largely reusable
- **Root cause linking** — surface *why* cost changed, not just that it did
- **Budgets and policy enforcement** — feed limits back into Mimir's `max_global_series_per_user`
- **Integrations** — Backstage plugin, Slack digest, Jira ticket on budget breach

### Biggest risks

- Tenant mapping is messy — `X-Scope-OrgID` often doesn't map cleanly to a single team; needs a config layer that rots fast
- Attribution is political — shared infra costs are hard to split fairly; needs a defensible methodology
- Adoption — dev teams won't look at it unless data is pushed where they already live

---

## 4. Live Snapshot vs. Stored Data

### The case for live/instantaneous

For a v1 or internal tool, querying live Mimir metrics is totally reasonable. A Grafana dashboard doing this live is sufficient for a small number of clusters and tenants.

### Where live breaks down

- **Trend analysis and budgeting** — "How has this team's cost changed over the last 90 days?" Mimir's default retention is often 7–30 days; extending it is expensive
- **Cross-cluster aggregation** — querying live across 10 clusters means federation latency and partial failures
- **Cost model computation** — need to join usage signals with actual infrastructure cost from AWS/GCP billing; inherently a batch/store problem
- **Audit and chargeback** — finance needs immutable monthly records that survive cluster rebuilds
- **Anomaly detection** — detecting spikes requires a historical baseline

### Decision

Daily aggregated rollups hit the sweet spot:

| Tier | Granularity | Retention | Purpose |
|------|------------|-----------|---------|
| Raw | 5-minute rollups | 30 days | Spike detection, incident correlation |
| Aggregate | Daily | Indefinite | Trend analysis, budgeting, billing |

---

## 5. Granular Tenant Metadata and CRD Event Capture

### Why CRD events matter

In GitOps-managed Mimir setups, cost spikes are almost always traceable to a specific `PrometheusRule` or `PodMonitor` change. Without capturing this metadata, you're left with "team-payments costs spiked Tuesday" but no causal link.

### Configuration state snapshot schema

```json
{
  "timestamp": "2026-09-16T14:32:00Z",
  "cluster": "prod-eu-west1",
  "tenant": "team-payments",
  "source": "prometheusrule_crd",
  "event": "rule_group_updated",
  "rule_group": "payments-slos",
  "recording_rules_count_delta": +12,
  "alerting_rules_count_delta": +3,
  "estimated_new_series": 840
}
```

### What to watch from the Kubernetes control plane

- `PrometheusRule` CRDs — track rule group changes per namespace
- `PodMonitor` / `ServiceMonitor` CRDs — new monitors mean new scrape targets and new series
- Namespace → tenant mapping (via `X-Scope-OrgID` label or namespace annotation)
- Capture diffs, not just state — what changed is more actionable than what exists

### Self-onboarding clusters: pre-flight cost estimation

When teams onboard their own scrape targets via `PodMonitor` CRDs without platform review, cardinality explosions can go unnoticed. The collection agent could power a **pre-flight cost estimate**: "this new `PodMonitor` will add approximately N series based on label cardinality" before it's applied. Shifts from reactive chargeback to proactive cost governance.

---

## 6. Shared Infrastructure Attribution

### Store-gateway and compactor

Attribute proportionally by `(tenant_active_series / cluster_total_series)` weighted with `samples_ingested`. Both components scale with data volume — a reasonable proxy without component-level profiling.

### Query components

Lean on meta-metrics directly:
- `cortex_query_frontend_queries_total`
- `cortex_ruler_queries_total`
- `cortex_query_scheduler_queue_duration_seconds`

### Ruler vs. dashboard queries

Ruler queries tend to be heavier (frequent, regular, often high-cardinality ranges). However, pricing them differently adds model complexity that's hard to explain to team leads. A single "query cost unit" aggregating both is the right default, with per-type breakdown available as a drill-down.

### Cost model calibration

The weighting question (how much does a series cost vs. a sample vs. a ruler query) needs empirical grounding from real cluster data. Calibration approach: point the product at a cluster, pull a billing export, and fit the weights to match actual spend. Component-level resource utilization (CPU/memory per ingester, ruler, compactor) alongside tenant metrics provides the regression inputs.

---

## 7. Collection Technology

### Per-cluster agent model

A per-cluster collector deployed as a Kubernetes `CronJob` (or lightweight Deployment). Three data paths:

**Metrics path** (every 5 minutes for raw, daily for rollups):
1. Query Mimir's metrics endpoint or HTTP admin API for tenant stats
2. Compute rollup in-process
3. Stream rows to BigQuery via streaming insert API

**CRD event path** (continuous):
1. Watch `PrometheusRule`, `PodMonitor`, `ServiceMonitor` via Kubernetes watch API
2. On each ADDED/MODIFIED/DELETED event, write a configuration event row to BigQuery

**Log ingestion path** (continuous, when `query_stats_enabled: true`):
1. Ship querier and ruler logs to Cloud Logging or Loki
2. Parse `query_stats` lines on ingest
3. Write structured rows to `query_log_events` table in BigQuery

### Why per-cluster (not centralized pull)

- No inbound firewall rules needed into production clusters
- Each agent only needs outbound access to BigQuery
- Easier security conversation with infra teams

### Technology choice for the agent

**Go** is the natural fit:
- `client-go` for CRD watching
- Mature Prometheus HTTP client libraries
- `google-cloud-bigquery` client for streaming inserts
- Single static binary, easy to deploy as a CronJob

Python works for faster early iteration.

### Why not pre-aggregate before writing?

At 250 tenants × 288 five-minute intervals = ~72,000 rows/day, BigQuery cost is negligible. Keeping raw events gives flexibility to redefine the cost model retroactively without reprocessing — important while calibrating weights.

---

## 8. Query Stats Log Capture

When `query_stats_enabled: true` (default), querier and ruler emit structured log lines:

```
msg="query stats" component=ruler query="..." query_wall_time_seconds=0.02
fetched_series_count=1 fetched_chunk_bytes=0 fetched_chunks_count=0
sharded_queries=0 result_series_count=0
```

### What this gives you that metrics don't

- **The actual query** — fingerprint/hash to group expensive queries across tenants
- **`fetched_series_count` / `fetched_chunks_count`** — actual data touched, not just a query count
- **`query_wall_time_seconds`** — real latency for weighted cost attribution
- **`sharded_queries`** — signals queries expensive enough to trigger Mimir's query sharding

### Query fingerprinting

Normalize and hash the raw query string before storing. Group by `query_fingerprint` across tenants and time — surfaces "top 10 most expensive queries across the platform," useful for rule hygiene.

### Log volume consideration

At scale, querier/ruler log volume can be significant. Aggregate to per-tenant, per-fingerprint, per-5-minute buckets before writing to BigQuery rather than streaming every individual log line.
