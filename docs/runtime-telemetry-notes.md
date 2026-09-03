# Runtime telemetry notes

## Why Mimir evaluation statistics matter

Mimir can emit query/evaluation statistics from the components involved in executing queries and ruler evaluations. These records can contain much richer information than high-level component metrics and may include the query, query hash, timing, estimated series count, fetched chunks/series/bytes, cache information, split/shard information and related execution statistics.

The first runtime attribution prototype should investigate these records before building a generic telemetry collector.

## Proposed ingestion boundary

Keep the source-specific parser separate from the attribution model:

```text
Mimir logs / stats
       ↓
Mimir source adapter
       ↓
normalized execution observation
       ↓
RuleID / QueryID correlation
       ↓
attribution
```

Loki is useful as a storage/query transport for these logs, but it should not be a hard dependency of the core product. A Loki adapter can be added because Grafana's Mimir monitoring dashboards use Loki for slow-query logs.

## Important attribution problem

Mimir can evaluate rules internally in the ruler or remotely through the query-frontend. In remote mode, the same logical evaluation can produce observations at multiple components. Frontend, ruler and querier records may therefore represent stages of the same execution rather than independent work.

The implementation must explicitly distinguish:

- logical rule evaluation
- frontend request
- downstream querier execution
- subquery/shard/split execution

and must avoid double-counting these when calculating tenant/rule workload.

## Data semantics

Do not collapse all size-related fields into an invented `estimated_bytes` metric.

Keep distinct fields for values such as:

- estimated series count
- fetched series count
- fetched chunk count
- fetched chunk bytes
- fetched index bytes
- response size
- query wall time
- queue time
- cache hits/misses
- split/shard counts

Retain source metadata where practical so calculations can be revisited as the model improves.

## Query text

When the telemetry source provides the exact query expression, retain it. It enables:

- AST/static analysis
- explanation
- correlation with rule definitions
- future recommendation generation

Retain a query hash as well when available. It can be used to correlate frontend and downstream execution observations.

## First implementation goal

Given real Mimir telemetry, reliably produce:

```text
Tenant analytics
  37% of ruler/query workload

Top rules:
  customer_activity:7d       21%
  funnel_conversion:30d       9%
  ...
```

before attempting to translate workload into euros.

## References

- Mimir query API: https://grafana.com/docs/mimir/latest/references/http-api/
- Mimir configuration/query stats: https://grafana.com/docs/mimir/latest/configure/configuration-parameters/
- Mimir ruler architecture: https://grafana.com/docs/mimir/latest/references/architecture/components/ruler/
- Mimir monitoring dashboard requirements / Loki slow-query logs: https://grafana.com/docs/mimir/latest/manage/monitor-grafana-mimir/requirements/
