# ADR 0003 driver verification — raw evidence (issue #29)

This is the evidence behind the **[V]** amendment in
[ADR 0003](adr/0003-cost-model-pool-driver-mappings.md). Every claim in that
amendment points at a section here, and every section here is a command
that was actually run, with a trimmed excerpt of what it returned. Where
something could not be checked, it says so, under
[What was not verified](#what-was-not-verified).

Run 2026-09-18 22:15 – 2026-09-19 UTC, Mimir 3.2.0 throughout.

## The two rigs

| | k3d (`dev/mimir-k8s`, PR #41) | compose (`dev/mimir-local`) |
|---|---|---|
| Mimir | chart 6.2.0 / Mimir 3.2.0, one pod per component, **classic** write path | `grafana/mimir:3.2.0`, single process (`-target=all,alertmanager`) |
| ingesters / RF | 3 ingesters, `replication_factor: 3` | 1, RF=1 |
| store-gateways / compactors | 1 / 1 (store-gateway sharding RF 3, so effectively 1 copy) | 1 / 1 |
| ruler | **remote** evaluation via the query-frontend | **local** evaluation |
| cardinality API | enabled (`limits.cardinality_analysis_enabled: true`) | default (off) |
| endpoint | `http://localhost:8090`, meta-monitoring tenant `monitoring` | `http://localhost:8080`, tenant `infra` |

The k3d cluster was already running (started ~22:13 UTC by another
session) and was only read from. The compose rig was started for this
check from a separate worktree as project `obscost-verify`.

PromQL was run with a small wrapper; every excerpt below is
`curl -s -H 'X-Scope-OrgID: <tenant>' <url>/prometheus/api/v1/query --data-urlencode 'query=<promql>'`,
flattened to `labels => value`. Raw `/metrics` on k3d was read through the
API-server pod proxy:
`kubectl get --raw /api/v1/namespaces/mimir/pods/<pod>:8080/proxy/metrics`.

---

## E1. Which drivers exist with a `user` label

**k3d**, listing every `cortex_*` family with a `user` label (excerpt, only
the families ADR 0003 names or needs):

```
$ q 'count by (__name__)({__name__=~"cortex_.+", user!=""})'      # tenant monitoring, 22:18 UTC
cortex_distributor_received_samples_total
cortex_distributor_received_bytes_total
cortex_ingester_active_series
cortex_ingester_ingested_samples_total
cortex_ingester_memory_series_created_total
cortex_ingester_memory_series_removed_total
cortex_ingester_tsdb_storage_blocks_bytes
cortex_prometheus_rule_evaluation_duration_seconds{,_sum,_count}
cortex_query_fetched_chunk_bytes_total
cortex_query_samples_processed_total
cortex_query_seconds_total
cortex_ruler_queries_total
...                                   (104 families in total)
```

Absent from that list: `cortex_ruler_query_seconds_total` (see E7),
`cortex_bucket_store_blocks_loaded_size_bytes`, `cortex_bucket_blocks_count`,
`cortex_bucket_index_estimated_compaction_jobs` (no blocks had been
shipped yet on a 5-minute-old cluster; see E5).

`cortex_ingester_memory_series` has no `user` label (re-confirmed):

```
$ q 'cortex_ingester_memory_series'
...,pod=mimir-ingester-0 => 37220        # labels: cluster, container, instance, job, namespace, pod — no user
```

**compose**, after a forced flush produced a block (E5), raw `/metrics`:

```
$ curl -s localhost:8080/metrics | grep -E '^cortex_[a-z_]+\{[^}]*user="' | sed 's/{.*//' | sort -u
cortex_bucket_blocks_count
cortex_bucket_index_estimated_compaction_jobs
cortex_bucket_store_blocks_loaded_size_bytes
cortex_distributor_received_samples_total
cortex_ingester_active_series
cortex_ingester_tsdb_storage_blocks_bytes
cortex_prometheus_rule_evaluation_duration_seconds_sum
cortex_query_fetched_chunk_bytes_total      # only because of one manual query, see E6
cortex_query_samples_processed_total        # idem
cortex_query_seconds_total                  # idem
cortex_ruler_query_seconds_total
...
```

So every driver in the mappings table exists with a `user` label on 3.2.0,
**with conditions**: the three bucket metrics only once a tenant has a
block in object storage, the `cortex_query_*` family only once a tenant has
query-path traffic, and `cortex_ruler_query_seconds_total` only under local
rule evaluation.

## E2. Counter or gauge

From `# TYPE` lines in each k3d pod's `/metrics` (and the compose rig for
the bucket metrics, which k3d did not yet expose):

```
# TYPE cortex_ingester_active_series gauge                         (ingester)
# TYPE cortex_distributor_received_samples_total counter           (distributor AND ruler)
# TYPE cortex_ingester_tsdb_storage_blocks_bytes gauge             (ingester)
# TYPE cortex_bucket_blocks_count gauge                            (compose; compactor module)
# TYPE cortex_bucket_store_blocks_loaded_size_bytes gauge          (compose; store-gateway module)
# TYPE cortex_bucket_index_estimated_compaction_jobs gauge         (compose; compactor module)
# TYPE cortex_query_fetched_chunk_bytes_total counter              (query-frontend only)
# TYPE cortex_query_samples_processed_total counter                (query-frontend only)
# TYPE cortex_query_seconds_total counter                          (query-frontend only)
# TYPE cortex_prometheus_rule_evaluation_duration_seconds summary  (ruler)
# TYPE cortex_ruler_query_seconds_total counter                    (compose only, see E7)
# TYPE cortex_distributor_replication_factor gauge                 (distributor, querier, ruler)
```

`cortex_prometheus_rule_evaluation_duration_seconds` is a **summary**; its
`_sum` and `_count` behave as counters.

Extra labels that matter for aggregation: `cortex_query_seconds_total`
carries `sharded="true|false"`; `cortex_bucket_index_estimated_compaction_jobs`
carries `type="merge|split"`; `cortex_query_frontend_queries_total` carries
`op` (see E6).

## E3. Active series across replicas (pool 1) — k3d, classic, RF=3

```
$ q 'cortex_ingester_active_series{user="analytics"}'
...,pod=mimir-ingester-0,user=analytics => 13014
...,pod=mimir-ingester-1,user=analytics => 13014
...,pod=mimir-ingester-2,user=analytics => 13014

$ q 'sum by (user) (cortex_ingester_active_series)'
user=analytics => 39042   user=infra => 21021   user=monitoring => 45251   user=payments => 3975   user=platform => 315
```

The raw sum is exactly 3× the real series count.

**Both literal readings of ADR 0003's [A] table ("`sum by (user)`, then
divide by `max(cortex_distributor_replication_factor)`") return nothing:**

```
$ q 'sum by (user) (cortex_ingester_active_series) / max(cortex_distributor_replication_factor)'
(empty result)
$ q 'sum by (user) (cortex_ingester_active_series unless on (cluster, namespace, job) cortex_partition_ring_partitions)
     / on (cluster, namespace) group_left() max by (cluster, namespace) (cortex_distributor_replication_factor)'
(empty result)
```

The first divides a `{user}` vector by a label-less vector, so no series
match. In the second, `sum by (user)` drops `cluster` and `namespace`, so
`on (cluster, namespace)` has nothing to match. The mixin sums `by (cluster, namespace, user)`, and
that works:

```
$ q 'sum by (cluster, namespace, user) (cortex_ingester_active_series unless on (cluster, namespace, job) cortex_partition_ring_partitions)
     / on (cluster, namespace) group_left() max by (cluster, namespace) (cortex_distributor_replication_factor)'
cluster=obscost-k3d,namespace=mimir,user=analytics => 13014
cluster=obscost-k3d,namespace=mimir,user=infra => 7007
cluster=obscost-k3d,namespace=mimir,user=monitoring => 15083.67
cluster=obscost-k3d,namespace=mimir,user=payments => 1325
cluster=obscost-k3d,namespace=mimir,user=platform => 105

$ q 'sum by (user) (cortex_ingester_active_series) / scalar(max(cortex_distributor_replication_factor))'   # single-cluster equivalent
user=analytics => 13014 ...
```

13,014 is also what the cardinality API reports for `analytics` (E9), and
matches its per-team split there (`bi` 8,007 + `ml` 5,007). The
`unless … cortex_partition_ring_partitions` guard is a no-op on a classic
cluster: the family does not exist there
(`count by (__name__)({__name__="cortex_partition_ring_partitions"})` → empty).

`cortex_distributor_replication_factor` is exported by three jobs, all
agreeing:

```
$ q 'max by (job) (cortex_distributor_replication_factor)'
job=mimir/distributor => 3    job=mimir/ruler => 3    job=mimir/querier => 3
```

Window average, summing across ingesters *before* averaging over time:

```
$ q 'avg_over_time((sum by (user) (cortex_ingester_active_series) / scalar(max(cortex_distributor_replication_factor)))[30m:1m])'
user=analytics => 13013.54   user=infra => 7006.85   user=monitoring => 15951.48   user=payments => 1320.38   user=platform => 105.13
```

`/distributor/all_user_stats` uses the same pre-replication count, and the
page says so:

```
$ curl -s -H 'Accept: application/json' localhost:8090/distributor/all_user_stats
[{"userID":"monitoring","ingestionRate":2710.75,"numSeries":47307,"APIIngestionRate":2618.18,"RuleIngestionRate":92.58},
 {"userID":"analytics","ingestionRate":2602.20,"numSeries":39042,"APIIngestionRate":2602,"RuleIngestionRate":0.20}, ...]
$ curl -s localhost:8090/distributor/all_user_stats | grep -o 'NB[^<]*'
NB stats do not account for replication factor, which is currently set to 3
```

`numSeries` 39,042 = 3 × 13,014, and `ingestionRate` 2,602/s is the
**ingester-side** sample rate (E4), also ×3.

## E4. Received samples across replicas (pool 2) — k3d

```
$ q 'sum by (job, user) (rate(cortex_distributor_received_samples_total[5m]))'     # 22:21:59 UTC
job=mimir/distributor,user=analytics => 860.32
job=mimir/distributor,user=infra => 467.00
job=mimir/distributor,user=monitoring => 834.81
job=mimir/distributor,user=payments => 80.33
job=mimir/distributor,user=platform => 7.00
job=mimir/ruler,user=analytics => 0.029
job=mimir/ruler,user=infra => 0.014
job=mimir/ruler,user=monitoring => 28.86
job=mimir/ruler,user=payments => 1.67

$ q 'sum by (user) (rate(cortex_ingester_ingested_samples_total[5m]))'
user=analytics => 2602.14   user=infra => 1401.07   user=monitoring => 2605.93   user=payments => 247.32   user=platform => 21.00
```

- The distributor counter is **pre-replication**: `analytics` 860/s is
  13,014 series at a 15 s scrape interval (867/s). No divisor applies.
- The ingester counter is ×RF: `payments` 3 × (80.33 + 1.67) = 246 vs
  247.3; `monitoring` 3 × (834.8 + 28.9) = 2,591 vs 2,606.
- **Rule output is counted by the ruler's own `cortex_distributor_*`
  metrics**, under `job="mimir/ruler"`, not by the distributor pods. The raw
  `/metrics` of each pod confirm it:

  ```
  == mimir-ruler-...:        cortex_distributor_received_samples_total{user="monitoring"} 2672
                             cortex_distributor_received_samples_total{user="payments"} 240
  == mimir-distributor-...:  cortex_distributor_received_samples_total{user="analytics"} 117090  (… all five tenants)
  ```

  On the single-process compose rig there is one counter covering both:
  `cortex_distributor_received_samples_total{user="infra"} 28815` =
  `cortex_ingester_ingested_samples_total{user="infra"} 28815` at RF=1.

The mixin's samples panels filter `job=~"($namespace)/((distributor.*|cortex|mimir))"`
(E10), so on a microservices deployment **they exclude rule output**. ADR
0003's `sum by (user)` with no job matcher includes it. Both are
defensible; they are different numbers, and the ADR has to pick one.

## E5. Storage, store-gateway and compactor (pools 3, 4, 5)

HELP text, k3d ingester:

```
# HELP cortex_ingester_tsdb_storage_blocks_bytes The number of bytes that are currently used for local storage by all blocks.
```

It is the **ingester's local disk**, not object storage. The mixin's only
panel on it is titled *"Space used by local blocks"* (E10).

No blocks had been shipped on k3d during the session (first head
compaction is ~3 h after start), so pools 3–5 were checked on the compose
rig, after forcing a block out of my own instance:

```
$ curl -X POST localhost:8080/ingester/flush                       # 22:30:09 UTC
[http 204]
... msg="finished uploading new block to long-term storage" block=01M2VA8D7HRS9J26FG6J6CXF6K

$ mc ls -r l/mimir-blocks/infra/ ; mc du l/mimir-blocks/infra/
549KiB  01M2VA8D7HRS9J26FG6J6CXF6K/chunks/000001
628KiB  01M2VA8D7HRS9J26FG6J6CXF6K/index
  652B  01M2VA8D7HRS9J26FG6J6CXF6K/meta.json
  234B  bucket-index.json.gz
1.2MiB  4 objects  mimir-blocks/infra

$ curl -s localhost:8080/metrics | grep -E '^cortex_(ingester_tsdb_storage_blocks_bytes|bucket_blocks_count|bucket_index_estimated_compaction_jobs)\{'
cortex_ingester_tsdb_storage_blocks_bytes{user="infra"} 1.205648e+06
cortex_bucket_blocks_count{user="infra"} 1                                   # 22:44 UTC, after the compactor's cleanup cycle
cortex_bucket_index_estimated_compaction_jobs{type="merge",user="infra"} 0
cortex_bucket_index_estimated_compaction_jobs{type="split",user="infra"} 0
```

The store-gateway reported the tenant but **0 bytes**, because it ignores
blocks younger than `-blocks-storage.bucket-store.ignore-blocks-within`
(default 10h, per `mimir -help-all`; k3d's `/config` shows
`ignore_blocks_within: 10h0m0s`):

```
cortex_bucket_store_blocks_loaded_size_bytes{component="store-gateway",user="infra"} 0
```

After restarting **my own** compose Mimir with
`-blocks-storage.bucket-store.ignore-blocks-within=0` (a compose override
in a scratch directory, not committed):

```
cortex_bucket_store_blocks_loaded_size_bytes{component="store-gateway",user="infra"} 124171    # 22:46 UTC
cortex_bucket_store_blocks_loaded{component="store-gateway"} 1
```

124,171 bytes loaded for a block that is 1.2 MiB in the bucket (index alone
628 KiB). So `cortex_bucket_store_blocks_loaded_size_bytes` measures the
**store-gateway's local footprint** for a tenant, not the tenant's bytes in
object storage. That matches its HELP (*"Size in bytes used by loaded blocks
of discovered tenants"*) and the mixin panel title *"Top users by
per-store-gateway disk utilization"*.

No per-tenant metric of **object-storage bytes** exists on this rig. Every
per-user byte-denominated family the monolith exposes:

```
$ curl -s localhost:8080/metrics | grep -E '^cortex_[a-z_]*(bytes|size)[a-z_]*\{[^}]*user=' | sed 's/{.*//' | sort -u
cortex_bucket_store_blocks_loaded_size_bytes
cortex_distributor_received_bytes_total
cortex_distributor_uncompressed_request_body_size_bytes_{bucket,count,sum}
cortex_ingester_tsdb_storage_blocks_bytes
cortex_ingester_tsdb_symbol_table_size_bytes
```

`cortex_bucket_blocks_count` is the only per-tenant bucket metric, and it
counts blocks, not bytes. Its HELP: *"Total number of blocks in the bucket.
Includes blocks marked for deletion, but not partial blocks."*

## E6. Query-path metrics under local vs remote rule evaluation (pool 6)

**Local evaluation (compose).** Two raw `/metrics` snapshots 5.5 minutes
apart. There was no query-path request in between; the only one, before
t0, was my own `count(up)`:

```
== t0 22:22:19 UTC
cortex_prometheus_rule_evaluation_duration_seconds_count{user="infra"} 339
cortex_ruler_queries_total{user="infra"} 339
cortex_ruler_query_seconds_total{user="infra"} 0.858
cortex_query_frontend_queries_total{op="query",user="infra"} 1
cortex_query_seconds_total{sharded="true",user="infra"} 0.215
cortex_query_samples_processed_total{user="infra"} 2
cortex_query_fetched_chunk_bytes_total{user="infra"} 76
== t1 22:27:51 UTC
cortex_prometheus_rule_evaluation_duration_seconds_count{user="infra"} 1715
cortex_ruler_queries_total{user="infra"} 1715
cortex_ruler_query_seconds_total{user="infra"} 11.385
cortex_query_frontend_queries_total{op="query",user="infra"} 1          # unchanged
cortex_query_seconds_total{sharded="true",user="infra"} 0.215           # unchanged
cortex_query_samples_processed_total{user="infra"} 2                    # unchanged
cortex_query_fetched_chunk_bytes_total{user="infra"} 76                 # unchanged
```

1,376 rule evaluations, zero movement in the `cortex_query_*` family. Under
local evaluation pool 6 contains no rule work at all; a tenant with only
rules has no pool-6 series.

**Remote evaluation (k3d).** The same family is populated for every tenant,
and for tenants with no human traffic it is *entirely* rule traffic:

```
$ q 'sum by (user) (increase(cortex_ruler_queries_total[10m]))'            # 22:29:47 UTC
user=analytics => 41.03   user=infra => 61.54   user=payments => 20.51   user=platform => 10.26   user=monitoring => 1242.12
$ q 'sum by (user, op) (increase(cortex_query_frontend_queries_total[10m]))'
op=query,user=analytics => 41.03   op=query,user=infra => 61.54   op=query,user=payments => 20.51
op=query,user=platform => 10.26    op=query,user=monitoring => 1262.65
op=cardinality,user=monitoring => 2.09        # my own cardinality-API calls (E9)
```

```
$ q 'sum by (job, user) (increase(cortex_query_seconds_total[10m]))'       # 22:20:39 UTC
job=mimir/query-frontend,user=analytics => 9.32   user=infra => 19.06   user=monitoring => 31.65 ...
```

No label on `cortex_query_seconds_total` (`sharded`, `user` only) or
`cortex_query_frontend_queries_total` (`op`, `user`) separates ruler
traffic from user traffic. Every `cortex_query_*` counter is exported by the
query-frontend only.

## E7. Ruler metrics under local vs remote evaluation (pool 7)

`cortex_ruler_query_seconds_total` **does not exist under remote
evaluation**. Every `cortex_ruler_*` family on the k3d ruler:

```
$ grep '^# TYPE cortex_ruler_' mimir-ruler-*.txt
cortex_ruler_client_request_duration_seconds histogram
cortex_ruler_clients gauge
cortex_ruler_config_last_reload_successful gauge
cortex_ruler_config_last_reload_successful_seconds gauge
cortex_ruler_config_updates_total counter
cortex_ruler_list_rules_seconds histogram
cortex_ruler_load_rule_groups_seconds histogram
cortex_ruler_managers_total gauge
cortex_ruler_queries_failed_total counter
cortex_ruler_queries_total counter
cortex_ruler_ring_check_errors_total counter
cortex_ruler_sync_rules_duration_seconds histogram
cortex_ruler_sync_rules_total counter
cortex_ruler_write_requests_total counter
```

Under local evaluation it exists (E6, compose). Over the 5.5-minute window
measured there, `cortex_ruler_query_seconds_total` rose by less than
evaluation duration (10.53 s vs 12.97 s), and every cumulative snapshot
shows the same ordering (e.g. 12.32 s vs 15.20 s, E8). That is consistent with query time
being part of evaluation time, so pool 7's two metrics must not be summed.
The overlap was not proven from source.

`cortex_prometheus_rule_evaluation_duration_seconds_sum` exists under both
modes:

```
$ q 'sum by (job, user) (increase(cortex_prometheus_rule_evaluation_duration_seconds_sum[10m]))'   # k3d, remote
job=mimir/ruler,user=analytics => 1.63   user=infra => 3.53   user=monitoring => 10.21   user=payments => 0.76   user=platform => 0.04
```

Under remote evaluation that ruler-side wall time and the query-frontend's
`cortex_query_seconds_total` (E6) are two measurements of the same rule
queries. Adding pool 6 and pool 7 double-counts them. This is the metric
form of the double-count ADR 0004 found for query counts.

## E8. Counters reset on restart

Restarting **my own** compose Mimir (`docker restart obscost-verify-mimir-1`):

```
22:28:21 UTC  cortex_distributor_received_samples_total{user="infra"} 143094
              cortex_ruler_query_seconds_total{user="infra"} 12.32
              cortex_prometheus_rule_evaluation_duration_seconds_sum{user="infra"} 15.20
              cortex_ingester_active_series{user="infra"} 5276
22:29:31 UTC  cortex_distributor_received_samples_total{user="infra"} 20081
              cortex_ruler_query_seconds_total{user="infra"} 1.22
              cortex_prometheus_rule_evaluation_duration_seconds_sum{user="infra"} 1.45
              cortex_ingester_active_series{user="infra"} 5719
```

Over a window containing restarts, a bare difference understates:

```
$ q 'cortex_distributor_received_samples_total{user="infra"} - cortex_distributor_received_samples_total{user="infra"} offset 20m'   # 22:46 UTC, tenant infra
=> 183369
$ q 'increase(cortex_distributor_received_samples_total{user="infra"}[20m])'
=> 323479.03
$ q 'resets(cortex_distributor_received_samples_total{user="infra"}[30m])'
=> 1
```

## E9. The cardinality API

**Default is off.** Compose rig, no override:

```
$ curl -s 'localhost:8080/runtime_config?mode=defaults' | grep cardinality_analysis_enabled
    cardinality_analysis_enabled: false
$ curl -H 'X-Scope-OrgID: infra' 'localhost:8080/prometheus/api/v1/cardinality/label_names?limit=2'
cardinality analysis is disabled for the tenant: infra                                [http 400]
$ curl -H 'X-Scope-OrgID: infra' 'localhost:8080/prometheus/api/v1/cardinality/active_series?selector={job=~".+"}'
{"data":[],"status":"error","error":"error merging partial responses: ... status 400, body: cardinality analysis is disabled for the tenant: infra\n"}   [http 200]
```

The `active_series` endpoint reports "disabled" with **HTTP 200** and
`"status":"error"` in the body. A client must check the body, not the
status code.

It is a per-tenant **limit** (it appears under `limits:` in `/config`,
and the error names the tenant), so it can be turned on for some tenants
through runtime overrides. Only the global setting was exercised here.
`mimir -help-all`:

```
-querier.cardinality-analysis-enabled            Enables endpoints used for cardinality analysis.   (no default shown → false)
-querier.cardinality-api-max-series-limit int    [experimental] Maximum number of series that can be requested in a single cardinality API request. (default 500)
-querier.label-values-max-cardinality-label-names-per-request int   (default 100)
-querier.active-series-results-max-size-bytes int   Maximum size of an active series ... request result shard in bytes. (default 419430400)
```

**`label_names`** (k3d, enabled) returns, per tenant, how many distinct
values each label name has, sorted by that count. It does not return series
counts:

```
$ curl -H 'X-Scope-OrgID: analytics' 'localhost:8090/prometheus/api/v1/cardinality/label_names?limit=5'
{"label_values_count_total":540,"label_names_count":12,"cardinality":[{"label_name":"series_id","label_values_count":500},
 {"label_name":"__name__","label_values_count":27},{"label_name":"instance","label_values_count":2},{"label_name":"pod","label_values_count":2},
 {"label_name":"team","label_values_count":2}]}                                     [http 200, 0.024 s, 311 bytes]
$ ... X-Scope-OrgID: monitoring ... label_names?limit=3
{"label_values_count_total":1690,"label_names_count":86,"cardinality":[{"label_name":"__name__","label_values_count":977}, ...]}   [0.046 s, 216 bytes]
```

**`label_values`** returns **series counts per value**, already
de-replicated (13,014 here, not 39,042):

```
$ curl -H 'X-Scope-OrgID: analytics' 'localhost:8090/prometheus/api/v1/cardinality/label_values?label_names[]=team'
{"series_count_total":13014,"labels":[{"label_name":"team","label_values_count":2,"series_count":13014,
 "cardinality":[{"label_value":"bi","series_count":8007},{"label_value":"ml","series_count":5007}]}]}      [http 200, 0.052 s, 203 bytes]
$ ... label_values?label_names[]=__name__&limit=1000      (monitoring)
'limit' param cannot be greater than '500'                                                              [http 400]
$ ... label_values?label_names[]=__name__&limit=500       (monitoring)
series_count_total 16256, 500 of label_values_count 977 returned
```

Its count tracks **in-memory** series, not active series. For `monitoring`
at the same moment:

```
label_values?label_names[]=job&count_method=inmemory   series_count_total 16314
label_values?label_names[]=job&count_method=active     series_count_total 16314
sum(cortex_ingester_active_series{user="monitoring"}) / RF                          => 15996
sum(memory_series_created_total - memory_series_removed_total){user="monitoring"} / RF => 16286.67
```

`count_method=active` returned the same number as `inmemory` here. Why is
not understood; see "not verified".

**`active_series`** requires a `selector` (without one:
`{"status":"error","errorType":"bad_data","error":"selector parameter is required"}`).
It returns **every matching series' full label set**, de-replicated. The
cost is proportional to series × label size:

```
$ curl -H 'X-Scope-OrgID: analytics' 'localhost:8090/prometheus/api/v1/cardinality/active_series?selector={team="bi"}'
{"data":[{"__name__":"avalanche_gauge_metric_mmmmm_0_9","cycle_id":"0","instance":"10.42.0.33:9001", ... ,"team":"bi"}, ...]}
8007 series                                                  [http 200, 0.230 s, 3,170,692 bytes]
$ ... active_series?selector={__name__=~".+"}   (analytics)  13014 series   [http 200, 0.244 s, 5,140,464 bytes]
$ ... active_series?selector={job=~".+"}        (monitoring) 15976 series   [http 200, 0.921 s, 4,118,227 bytes]
```

That is roughly 250–400 bytes of response per series on this rig. Every
call also counts as query-frontend traffic for the tenant: it shows up as
`cortex_query_frontend_queries_total{op="cardinality"|"active_series"}`
(E6).

## E10. The mixin's own queries (from the repo alone, `mimir-3.2.0` tag)

Fetched with the same URL scheme as `dev/mimir-local/fetch-mixin.sh`:

```
$ curl -fsSL https://raw.githubusercontent.com/grafana/mimir/mimir-3.2.0/operations/mimir-mixin-compiled/dashboards/mimir-top-tenants.json -o mimir-top-tenants.json
$ jq '[.. | objects | select(has("expr"))] | length' mimir-top-tenants.json
14
```

Every panel across all 27 dashboards that reads a driver metric, as
`title :: expr` (`cluster=~"$cluster"` matchers trimmed):

```
Top $limit users by per-store-gateway disk utilization :: topk($limit, max by (user) (sum by (user, pod) (cortex_bucket_store_blocks_loaded_size_bytes{job=~"($namespace)/((store-gateway.*|cortex|mimir))"})))
Top $limit users by estimated compaction jobs from bucket-index :: topk($limit, sum by (user) (cortex_bucket_index_estimated_compaction_jobs{job=~".../compactor.*"}) and ignoring(user) (sum(rate(cortex_bucket_index_estimated_compaction_jobs_errors_total[...])) == 0))
Blocks :: max by (user) (cortex_bucket_blocks_count{job=~".../compactor.*", user="$user"})
Tenants with largest number of blocks :: topk(10, max by(user) (cortex_bucket_blocks_count{job=~".../compactor.*"}))
Total number of blocks in the storage :: sum(max by(user) (max_over_time(cortex_bucket_blocks_count{job=~".../compactor.*"}[15m])))
Space used by local blocks :: sum by (job) (cortex_ingester_tsdb_storage_blocks_bytes{job=~".../ingester.*", user="$user"})
Latency :: sum by(user) (rate(cortex_prometheus_rule_evaluation_duration_seconds_sum{job=~".../ruler..."}[...])) / sum by(user) (rate(..._count[...]))
```

And from `mimir-top-tenants.json`:

- **Active series** has two branches joined by `or`. *Classic*:
  `sum by (cluster, namespace, user)(cortex_ingester_active_series unless on (cluster, namespace, job) cortex_partition_ring_partitions) / on (cluster, namespace) group_left() max by (cluster, namespace)(cortex_distributor_replication_factor{job=~".../distributor.*"})`.
  *Ingest storage*:
  `sum by (cluster, namespace, user)(max by (ingester_id, cluster, namespace, user)(label_replace(cortex_ingester_active_series, "ingester_id", "$1", "pod", ".*?-(rc-[0-9]+-[0-9]+|[0-9]+)$")))`.
  That is a max across the replicas of each partition, summed over
  partitions, with **no** replication divisor.
- **In-memory series** is `memory_series_created_total - memory_series_removed_total`,
  using the same two branches.
- **Samples rate** is `sum by (user) (rate(cortex_distributor_received_samples_total{job=~".../distributor.*"}[5m]))`,
  distributor job only and no divisor (E4).
- **Rule group size / eval time** are `cortex_prometheus_rule_group_rules`
  and the gauge `cortex_prometheus_rule_group_last_duration_seconds`, not
  the evaluation-duration summary.
- None of the 14 panels reads a `cortex_query_*` counter or
  `cortex_ruler_query_seconds_total`. The only per-tenant query-volume panels
  in the mixin (`Fetched chunks`, `P99 fetched chunks`) read the
  query-frontend's **Loki log** (`|= "query stats" | logfmt | unwrap fetched_chunk_bytes`),
  not metrics.

---

## What was not verified

- **The ingest-storage branch.** Both rigs run the classic write path, and
  `cortex_partition_ring_partitions` is absent on both. PR #41's README
  reports that under ingest storage the classic query returns nothing and
  `cortex_distributor_replication_factor` is not exported. That was not
  re-run here.
- **`max` vs `sum` across store-gateway replicas.** Both rigs run one
  store-gateway, so any aggregation returns the same number. The mixin's
  `max by (user)(sum by (user, pod)(…))` is documented by its panel title
  as *per-store-gateway* utilisation (the worst single pod). Whether `max`,
  `sum`, or `sum / store-gateway RF` is the right *pool-4 share* driver
  with N > RF store-gateways is an open question, not a finding.
- **Aggregation across multiple compactors** for `cortex_bucket_blocks_count`
  and `cortex_bucket_index_estimated_compaction_jobs`. There was one
  compactor on each rig. The mixin uses `max by (user)` for the first and
  `sum by (user)` for the second.
- **The bucket metrics on k3d.** No block had been shipped by the time of
  writing, so `cortex_ingester_tsdb_storage_blocks_bytes` was 0 on all
  three ingesters. The ×RF behaviour of that gauge (three ingesters each
  holding a copy of the block) is expected but was not observed.
- **Rollouts.** The k3d ingesters were not restarted, because this session
  did not own that cluster. How a gauge window average behaves across a
  rollout (ADR 0003's third consequence) is unmeasured.
- **Any calibration** of driver against resource (ADR 0003 decision 6).
  Out of scope here.
- **Why `count_method=active` equals `inmemory`** in `label_values` on this
  rig.
- **Cardinality API cost at scale.** Response size and latency were
  measured at 8k–16k series. Behaviour against the 400 MiB
  `active-series-results-max-size-bytes` cap, or the querier CPU cost, was
  not measured.
