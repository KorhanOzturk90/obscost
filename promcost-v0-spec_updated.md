# promcost — v0 Specification

*Working name only. Naming is a launch decision, not a build decision.*

*Rev 0.1 — amended after external review: limits sourcing, cold-series presence window, cache-aware calibration, rewrite composability/warm-up guards, split read-cost pricing.*

**Positioning (one line):** Cost attribution and load prediction for rules in multi-tenant Prometheus-compatible stacks (Mimir first). Not a linter — pint is the linter, and promcost wraps it.

**Non-goals for v0:** no UI, no daemon/SaaS control plane, no ServiceMonitor/PodMonitor cost prediction (phase 2 — needs the observed corpus), no VictoriaMetrics/MetricsQL support (PromQL-pure design keeps the door open), no rule hygiene checks that pint already owns.

---



## 1. Architecture

```
                    ┌──────────────────────────────────────────┐
                    │                 engine                    │
                    │  ┌─────────┐  ┌──────────┐  ┌─────────┐  │
 rule sources ────► │  │ loader  │─►│ analyzer │─►│ reporter│  │ ──► md / json / html
 (cluster CRDs,     │  └─────────┘  └────┬─────┘  └─────────┘  │      + exit code
  ruler API, dir)   │                    │ Meter (only I/O)     │
                    │               ┌────▼─────┐               │
                    │               │ backend  │  instant qs   │
                    │               │ client   │  only, tenant │
                    │               └──────────┘  scoped       │
                    └──────────────────────────────────────────┘
        frontends: CLI scan / CLI check (CI) / GitHub Action / Kyverno audit (later)
```

Design rules:

1. **The analyzer is pure.** It operates on an annotated PromQL AST (upstream `prometheus/promql/parser`) plus injected interfaces. All network I/O goes through a single `Meter` interface so every estimation function is unit-testable with a `FakeMeter`.
2. **The engine never executes a tenant's expression.** Only cheap metering primitives (counts, sampled `count_over_time`, series API). The one exception is the opt-in truncated dry-run (L06), off by default.
3. **Every finding carries:** check ID, severity, tenant, source location (file/CRD/ruler group), the numbers behind it, and where possible a remediation diff.

---



## 2. CLI surface

```
promcost scan    [--cluster | --ruler-url URL | --dir PATH]
                 [--tenant NAME|all] [--config promcost.yaml]
                 [--top N] [--format md|json|html] [--with-pint]

promcost check   --dir PATH [--base GIT_REF] [--config promcost.yaml]
                 [--offline] [--fail-on warn|error] [--with-pint]

promcost explain RULE_REF        # deep-dive one rule: full cost breakdown tree
promcost rewrite RULE_REF        # emit optimized rule group as a unified diff
                 [--style chained|sloth]
promcost pint-config             # render per-tenant pint HCL from tenancy discovery
promcost analyze-join --mimirtool-output FILE   # merge mimirtool analyze results (F02)
```

`RULE_REF` = `path/file.yaml:group:rule` or `tenant/namespace/crdname:group:rule`.

**Exit codes:** `0` clean · `1` config/usage error · `2` findings at or above `--fail-on` · `3` backend unreachable (check mode degrades to static-only with a warning rather than failing, unless `--strict`).

**Modes and their contracts:**


| mode    | input                            | network        | zero-adoption?                    | purpose                                               |
| ------- | -------------------------------- | -------------- | --------------------------------- | ----------------------------------------------------- |
| scan    | live cluster CRDs or ruler API   | yes (metering) | yes — platform team runs it alone | ranked fleet report; the demo; the wedge              |
| check   | rule files in a repo, diff-aware | optional       | needs repo integration            | PR gate; wraps changed rules only                     |
| explain | one rule                         | yes            | yes                               | the artifact you paste to a tenant                    |
| rewrite | one rule/group                   | yes            | yes                               | closes the "tenants never implement suggestions" loop |


Scan ships first. Check is a thin wrapper over the same analyzer with git-diff selection (steal pint's `ci` semantics: analyze only rules changed vs `--base`).

---



## 3. Configuration schema (`promcost.yaml`)

```yaml
backend:
  type: mimir                      # mimir | prometheus   (v0)
  url: https://mimir.internal/prometheus
  auth:
    bearer_token_env: PROMCOST_TOKEN
  timeout: 10s
  max_concurrent_queries: 4        # politeness cap for metering

tenancy:
  header: X-Scope-OrgID
  discovery:                       # first match wins, evaluated in order
    - source: crd_annotation
      key: obs.example.com/tenant
    - source: namespace
      transform: "s/^team-//"      # regex replace, sed syntax
    - source: static
      map:
        monitoring: platform
        kube-system: platform
  unmapped: error                  # error | skip | tenant:<name>

limits:
  # sources tried in order; first success wins. resolved endpoints beat files
  # (a file can lie about what's actually loaded).
  sources:
    - type: user_limits_endpoint   # GET /api/v1/user_limits per tenant — resolved
                                   # limits as Mimir sees them (experimental; verify
                                   # availability against target Mimir versions)
    - type: runtime_config_endpoint
      url: https://mimir.internal/runtime_config   # ?mode=diff → non-default overrides only
    - type: configmap              # k8s-native: read via current kube context
      name: mimir-runtime
      key: runtime.yaml
    - type: file
      path: ./runtime-overrides.yaml   # offline fallback
  # keys read per tenant (fall back to defaults section):
  #   max_fetched_series_per_query, max_fetched_chunk_bytes_per_query,
  #   ruler_max_rules_per_rule_group, ruler_max_rule_groups_per_tenant,
  #   max_global_series_per_user, ingestion_rate
  # NOTE: exact key names vary by Mimir version — loader must tolerate
  # both CLI-flag style and YAML style names.

cost_model:
  currency: EUR
  eur_per_million_active_series_month: 85    # CALIBRATE — see §8.6
  eur_per_billion_processed_samples: 0.40    # querier CPU — CALIBRATE
  eur_per_billion_fetched_store_samples: 0.15 # store-gateway/object-store reads — CALIBRATE
  store_after: 12h                           # Mimir query_store_after boundary
  bytes_per_sample: 1.5                      # chunk-bytes estimate; calibrate via L06

checks:
  disable: []                      # e.g. [PC-S04]
  thresholds:
    subquery_steps_warn: 500
    subquery_steps_error: 2000
    recording_range_warn: 24h
    limit_headroom_warn_pct: 60
    limit_headroom_error_pct: 90
    output_cardinality_warn: 10000
    presence_window: 1h              # lookback for all count/existence metering;
                                     # guards against cold series (cronjobs, batch)
                                     # falling outside the 5m instant lookback

pint:
  enabled: true
  binary: pint                     # delegate execution
  config_template: ./pint.tpl.hcl # rendered per tenant by `promcost pint-config`
```

The tenancy block is the piece pint structurally lacks: dynamic namespace/annotation → `X-Scope-OrgID` resolution, so every metering query and every existence check runs against the *right tenant's* view.

---



## 4. Check catalogue (v0)

Severity: `info` < `warn` < `error`. Each check documents its remediation.

### Static tier — no network, always run


| ID     | name                           | logic                                                                                                                                                                                                                     | default severity    |
| ------ | ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------- |
| PC-S01 | subquery-density               | For every subquery `[R:s]`: `steps = ceil(R/s)`. Flag at `steps > warn/error` thresholds. `[30d:5m]` = 8,640 steps → error.                                                                                               | warn/error by steps |
| PC-S02 | heavy-range-in-recording-rule  | Range selector or subquery span > `recording_range_warn` inside a recording rule. Remediation: chained-window rewrite (see `rewrite`).                                                                                    | warn                |
| PC-S03 | interval-to-range-ratio        | Rule group `interval` re-evaluates a window `R` where `R/interval > 10,000` (e.g. 30d window every 30s). The work-per-new-data ratio, i.e. how much history is recomputed to incorporate one new sample.                  | warn                |
| PC-S04 | high-cardinality-output-labels | Recording rule aggregates `by`/`without` leaving configurable known-explosive labels in output (`pod`, `instance`, `id`, `path`, `le` retained un-aggregated…). Static wordlist; L03 upgrades it with live numbers.       | info                |
| PC-S05 | ruler-count-limits             | Rules per group vs `ruler_max_rules_per_rule_group`; groups per tenant vs `ruler_max_rule_groups_per_tenant`. Needs only the limits file — works offline. The most trivially checkable failure that nothing checks today. | error               |
| PC-S06 | tenant-resolution              | Every rule source must resolve to exactly one tenant via the tenancy block. Unmapped → per `unmapped:` policy.                                                                                                            | error               |




### Live tier — cheap metering only (instant queries, tenant-scoped)


| ID     | name                         | logic                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    | default severity  |
| ------ | ---------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------- |
| PC-L01 | selector-cardinality         | `count(last_over_time(sel[presence_window]))` per unique selector — windowed so cold series (cronjobs, batch) still count. Feeds every formula below. Trade-off: decodes chunks across the window; bounded, still never executes tenant expressions.                                                                                                                                                                                                                                                                                                                                     | info (data)       |
| PC-L02 | query-limit-headroom         | `est_fetched_series / max_fetched_series_per_query` and `est_fetched_chunk_bytes / max_fetched_chunk_bytes_per_query` vs headroom thresholds. Message names the exact Mimir error the rule will produce (e.g. `err-mimir-max-series-per-query`).                                                                                                                                                                                                                                                                                                                                         | warn/error by pct |
| PC-L03 | output-cardinality           | For `agg by (l1..ln)(...sel...)`: `count(count by (l1..ln)(last_over_time(sel[presence_window])))` — **grouping over the raw selector, never executing the full expression**. Estimates permanent new series a recording rule creates; the window prevents zero-cost false negatives on intermittent workloads.                                                                                                                                                                                                                                                                          | warn at threshold |
| PC-L04 | tenant-scoped-existence      | pint-style series existence, but with the tenant's `X-Scope-OrgID`. pint against the wrong tenant lies; this can't. Presence window applies — a cronjob's series count as existing.                                                                                                                                                                                                                                                                                                                                                                                                      | warn              |
| PC-L05 | samples-rate-estimate        | Measured samples/series/hour via sampled `count_over_time` (§5.2) — replaces the scrape-interval knowledge Mimir cannot provide (no scrape config to read; intervals live in tenant-side CRDs).                                                                                                                                                                                                                                                                                                                                                                                          | info (data)       |
| PC-L06 | truncated-dry-run *(opt-in)* | AST-rewrite all ranges > 1h down to 1h, execute once with `?stats=all`, scale results by the steps ratio. Calibrates §5 constants. Off by default; documented as calibration, not gating. **Cache guard:** use unaligned, current-time windows and send `Cache-Control: no-store` (verify per version) — a query served from the query-frontend results cache returns without engine stats and would silently poison calibration. Note also that chunk/index caches make repeat fetches cheaper; fetched estimates deliberately model cold-read cost as the conservative capacity basis. | info              |




### Fleet tier — scan mode only


| ID     | name                    | output                                                                                                                                                                                                                                                                                                                                                                                  |
| ------ | ----------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| PC-F01 | top-expensive-rules     | Ranked table: rule, tenant, est. processed samples/day, est. new series, currency/day. The demo report.                                                                                                                                                                                                                                                                                 |
| PC-F02 | expensive-and-unused    | Join PC-F01 with `mimirtool analyze` output (`analyze-join`). Quadrant: expensive×unused = kill list; expensive×used = rewrite candidates.                                                                                                                                                                                                                                              |
| PC-F03 | tenant-share report     | Per tenant, from Mimir's own `user`-labelled internals (received samples, discarded samples, active series, query volume — verify exact metric names against the running Mimir version): absolute, **share of configured limit**, **share of cluster capacity**, 7-day delta. Seed of the phase-2 noisy-neighbour product. Read from ops/meta-monitoring, never the primary under load. |
| PC-F04 | cross-tenant-duplicates | Normalized-AST equality across tenants' recording rules: N tenants computing the same expensive expression → platform-level rule candidate.                                                                                                                                                                                                                                             |


---



## 5. Estimation functions

All pure over `(ast, Meter, Limits, CostModel)`. The `Meter` interface is the only I/O:

```go
type Meter interface {
    // W = presence_window. Aggregations take instant vectors, so range vectors
    // pass through last_over_time first — count(sel[W]) is not valid PromQL.
    SeriesCount(tenant, selector string) (int64, error)          // count(last_over_time(sel[W]))
    GroupedCount(tenant, selector string, by []string) (int64, error) // count(count by(...)(last_over_time(sel[W])))
    SampleSeries(tenant, selector string, k int) ([]Labels, error)    // series API w/ limit, fallback topk(k, sel)
    RangeSampleCount(tenant string, exact Labels, window time.Duration) (int64, error) // count_over_time
}
```



### 5.1 Core arithmetic

```
steps(R, s)                 = ceil(R / s)
evals_per_day(interval)     = 86400 / interval_seconds
```



### 5.2 Measured sample rate (replaces unknowable scrape interval)

```
sps(sel) — samples per series per hour:
  S  = SampleSeries(sel, k=5)
  r_i = RangeSampleCount(exact_labels(S_i), 1h)
  sps = median(r_i)                    # median: robust to a stale series in the sample
```

Bounded cost: k series, k+1 queries. Cache per selector per run.

### 5.3 Per-node costs (post-order AST walk)

Two distinct quantities, because they stress different components:

- **fetched_samples** — data read from ingesters/store-gateways (I/O pressure)
- **processed_samples** — sample-evaluations in the querier (CPU pressure)

```
VectorSelector sel (instant ctx):
  fetched   = N(sel)                      # one point per series (lookback)
  processed = N(sel)

MatrixSelector sel[R]:
  fetched   = N(sel) × sps(sel) × R_hours
  processed = fetched

Subquery inner[R:s]:
  fetched   ≈ cost(inner).fetched over R      # data read ~once across the window
  processed = steps(R,s) × cost(inner as instant).processed   # engine evaluates per step

Aggregation agg by(L)(e):
  out_series = GroupedCount(sel(e), L)
  processed += cost(e).processed          # pass-through + grouping

BinaryExpr a OP b:
  processed = cost(a).processed + cost(b).processed + match_cost(≈ max(N_a, N_b))

Function calls: pass-through of arg costs (rate/increase etc. are O(processed)).
```



### 5.4 Rule- and fleet-level

```
rule_daily_processed  = processed(expr) × evals_per_day(group.interval)
rule_daily_fetched    = fetched(expr)   × evals_per_day(group.interval)
rule_new_series       = out_series(expr)          # recording rules only; 0 for alerts
est_chunk_bytes       = fetched(expr) × bytes_per_sample

headroom_series_pct   = fetched_series(expr) / max_fetched_series_per_query × 100
headroom_bytes_pct    = est_chunk_bytes / max_fetched_chunk_bytes_per_query × 100

store_fraction(R)     = max(0, (R - store_after) / R)
                        # reads inside query_store_after come from ingester memory;
                        # reads beyond it hit store-gateways → object-store GETs + bandwidth.
                        # A [30d] rule pays store prices on ~98% of reads; a [5m] rule on none.

daily_cost_currency   = rule_new_series × rate_series_month / 30
                      + rule_daily_processed × rate_processed_per_billion / 1e9
                      + rule_daily_fetched × store_fraction(R_max) × rate_fetched_store_per_billion / 1e9
```

**Worked example (the motivating pathology).** `avg_over_time((sum(rate(m[5m])))[30d:5m])` on a selector matching 5,000 series at sps=60, in a group with `interval: 1m`:
steps = 8,640; inner instant processed ≈ 5,000; processed/eval ≈ 43.2M; daily processed ≈ **62.2 billion sample-evaluations** for one rule. That number, in the report, next to the tenant's name, is the product.

### 5.5 Test strategy for the analyzer

Golden-corpus unit tests: `(rule yaml, FakeMeter fixture) → expected findings json`. FakeMeter returns canned counts; every check and every formula gets exercised with zero infrastructure. Error-bar honesty: every estimate in reports carries the assumption trail (`N=count(sel) @ scan time; sps=median of 5`), and L06 calibration runs in the sim environment (§8) assert estimates within ±30% of `?stats=all` ground truth — that tolerance is a stated design contract, not an aspiration of precision.

---



## 6. pint integration contract

- `promcost pint-config` renders per-tenant pint HCL (one `prometheus` block per tenant, `X-Scope-OrgID` header injected, path scoping from tenancy discovery) — automating the "forty hand-maintained config blocks" workaround out of existence.
- `--with-pint` runs pint as a subprocess on the same file set, merges its findings into the report under a `pint/` prefix.
- promcost never re-implements a pint check. Overlap policy: if pint adds a check we have, we deprecate ours. README states this. Hygiene (annotations, `for:`, owners, naming) is permanently pint's.

---



## 7. Report formats

- **md** (default): human report — fleet summary, top-N table, per-finding detail with remediation. Designed to be pasted into a tenant's ticket verbatim.
- **json**: stable schema for CI annotation and future UI.
- **html**: single self-contained file, no external assets, shareable ("run this air-gapped, send me nothing" trust posture).
- `rewrite` output: unified diff + before/after cost table + explicit equivalence notes. The notes must name the specific approximations: step alignment (subquery steps align to absolute time; ruler evaluations follow the ruler's schedule with jitter) and gap weighting (avg-of-bucket-averages weights sparse periods differently than avg-over-all-samples).
- **Composability guard.** The rewrite engine only auto-applies chains for aggregations that compose exactly: `sum`, `min`, `max`, `count`; `avg` via a sum/count pair. It refuses `quantile_over_time` and quantile-over-window patterns (a quantile of quantiles is not a quantile) unless it can rewrite via histogram-bucket sums. One silently-wrong SLO rewrite permanently ends the tool's credibility — refuse loudly instead.
- **Warm-up gap.** A freshly deployed chain has no history: the 30-day result is wrong for up to 30 days. The diff must state this and ship the fix pre-filled: `promtool tsdb create-blocks-from rules` to backfill the intermediate rule from existing raw data, then `mimirtool backfill` to upload the blocks per tenant.

---



## 8. Testing plan



### 8.1 Week 0–1 — corpus, no infrastructure

Harvest real rule files into `testdata/corpus/`:

- kubernetes-mixin, node-exporter mixin, kube-prometheus rules — the default estate everyone runs.
- **mimir-mixin** — Mimir's own recording rules; pleasingly, the tool's first target can be itself.
- awesome-prometheus-alerts (hundreds of community alert rules).
- Sloth and Pyrra example outputs — these are the *good* SLO pattern; the rewrite engine's target shape and a negative-control set (should produce zero PC-S01/S02 findings).
- GitHub code search: `kind: PrometheusRule` for real CRD structure; regex search for `[30d:`, `[7d:`, `[90d:` in YAML to harvest genuine pathological subqueries people actually shipped. Save originals with source links.

Deliverable: parser + static tier (PC-S01..S06) at 100% on golden tests. This alone, as `promcost check --offline`, is already a shippable GitHub Action.

### 8.2 Week 1–2 — live metering against public instances

Public, intentionally-open demo Prometheus servers exist (the Prometheus community demo instance and the PromLabs demo instance are the two commonly used ones — verify current URLs/status at build time). They have node-exporter-class metrics and some rules. Use them to integration-test the Meter primitives (counts, series API + limit-param fallback, sampled `count_over_time`).

Etiquette constraint, which is conveniently also the product constraint: metering primitives only, low concurrency, never the dry-run. If the tool is impolite to a public demo instance, that's a failed design test, not a nuisance.

What public instances cannot give you: multi-tenancy, Mimir limits, a ruler. No public multi-tenant Mimir exists. Hence:

### 8.3 Week 2–4 — the multi-tenant simulation (the core rig)

Base: Grafana's official *play-with-Mimir* docker-compose (Mimir monolithic ×3, MinIO as S3, Grafana). On top:

- **Tenants**: 4–6 fake tenants, each an Alloy instance remote-writing with its own `X-Scope-OrgID` header (Alloy over raw avalanche remote-write because per-tenant headers are first-class, and it mirrors your previous stack's Alloy-pool pattern).
- **Load**: each Alloy scrapes one or more `avalanche` instances configured per scenario.
- **Scenario matrix** (each tenant is one misbehaviour, deterministically):


| tenant         | avalanche/rig config                                                     | exercises                  |
| -------------- | ------------------------------------------------------------------------ | -------------------------- |
| `baseline`     | modest stable series                                                     | control                    |
| `cardinality`  | high label-count/value-count                                             | F03 shares, S04/L03        |
| `churn`        | short series-interval (constant new series)                              | F03 churn column           |
| `heavy-rules`  | normal ingest + pathological ruler rules incl. `[30d:5m]` SLO subqueries | S01–S03, L02, F01, rewrite |
| `limit-hugger` | ingest tuned to ~90% of its configured limits                            | L02, F03 headroom          |


- **Limits**: a real `runtime-overrides.yaml` with deliberately tight, per-tenant-different limits, so PC-L02/S05 have real teeth in tests.

**The backfill recipe (makes** `[30d:5m]` **actually evaluable locally).** A fresh sim has hours of data; a 30-day subquery over it is vacuous. Manufacture history: generate synthetic OpenMetrics text spanning 30–45 days (small generator script, a few series families with realistic rates) → `promtool tsdb create-blocks-from openmetrics` → upload per tenant with `mimirtool backfill` (requires enabling Mimir's block-upload on the compactor). Now heavy SLO rules evaluate over real 30-day ranges, `?stats=all` returns ground truth, and L06 calibration tests become possible. This recipe is itself a corpus asset: keep the generator deterministic (seeded) so CI is reproducible.

- **CI**: nightly job spins the compose rig, backfills (cached blocks as artifacts), runs `promcost scan`, snapshot-compares the report. Estimate-vs-stats assertions (±30%) live here.



### 8.4 Month 2 — Kubernetes-native path

kind (or k3d) + kube-prometheus-stack: tests CRD discovery, tenancy resolution from namespaces/annotations (create `team-*` namespaces with PrometheusRule CRDs), and later the Kyverno-audit frontend. This rig needs no Mimir — CRD loading and tenant mapping are backend-independent.

### 8.5 Month 2+ — scale and realism

- Small spot-instance EKS/GKE cluster, `mimir-distributed` helm (small.yaml), avalanche scaled to 1–5M active series across tenants: performance of scan mode against a non-toy ruler (hundreds of groups), metering-cost measurement (scan must be provably cheap — publish its own query bill in the report footer; the tool that audits cost should audit itself).
- Teardown-by-default; this fits a modest monthly experimentation budget only if it's ephemeral.



### 8.6 The only real test — design partners

Synthetic rigs validate mechanics, not heuristics. Real rule corpora are the scarce asset:

1. **Ex-employer first.** They still run the estate you built; a read-only scan in exchange for the report is a fair trade and your fastest calibration of the cost model (their infra bill ÷ their active series gives you real `eur_per_million_active_series_month`).
2. **Community offer**: CNCF Slack (#mimir, #prometheus), r/PrometheusMonitoring, Mimir community calls — "free top-10 most-expensive-rules report; runs read-only; air-gapped mode; you see everything it sends (nothing)."
3. Target: 5 real scans in the first 8 weeks after v0. Their reaction to the report — which finding they act on, whether they ask for `rewrite` or `check` next — is the roadmap oracle for phase 2.

---



## 9. Phase map (against the original pain list)


| pain                                      | phase                                                                                                                   |
| ----------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| heavy SLO recording rules (`[30d:5m]`)    | **v0** (S01–S03, F01, rewrite)                                                                                          |
| unknown load before rules ship            | **v0** (check mode)                                                                                                     |
| rule failures invisible to tenants        | v0 partial (L02 predicts limit-hits) → phase 2: tenant-facing ruler health from ruler API per-rule `health`/`lastError` |
| unknown load before ServiceMonitors ship  | phase 2: observed attribution via `scrape_samples_post_metric_relabeling`, then prediction from corpus                  |
| hidden tenant behaviour / noisy neighbour | phase 2: F03 grows into the tenant behaviour product                                                                    |
| cardinality explosions, slow remediation  | v0 rewrite closes the rules half; phase 2 attribution closes the scrape half                                            |
| stage ≠ prod                              | design property: all metering runs against prod, safely, by construction                                                |
| pre-event capacity, capacity planning     | phase 3, consulting-shaped, powered by accumulated scan data                                                            |


---



## 10. Open decisions (yours)

1. **Name** — before the repo exists; positioning word is "cost"/"tenancy", never "lint".
2. **License** — recommendation: Apache-2.0 for the engine/CLI (matches pint and the Prometheus ecosystem, maximizes wrap-don't-fight credibility); keep future control-plane/SaaS proprietary. AGPL on a CLI protects against nothing that matters here.
3. **Cost model defaults** — placeholders above are invented; calibrate on the first real scan (§8.6.1).
4. **Language** — Go is strongly indicated (upstream PromQL parser, Mimir/pint ecosystem, single-binary distribution for the air-gapped trust story).

