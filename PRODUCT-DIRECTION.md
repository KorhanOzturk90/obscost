# Obscost Product Direction

## Thesis

Obscost should not become a PromQL linter with a cost score attached.

The long-term product is **metrics infrastructure workload attribution and governance for shared Prometheus-compatible infrastructure**, with Mimir as the first target.

Mimir is the initial implementation target because it exposes useful rule, tenant, query and execution telemetry, but the product direction should remain broader than Mimir. The eventual architecture should support multiple metrics backends/solutions where the required workload signals can be obtained.

The core question is:

> **Where is my metrics infrastructure capacity going, who is consuming it, why, and what can we change?**

The desired hierarchy is:

```text
Infrastructure → Ingestion / Queries → Tenant → Team → Rule / Metric → Workload → Cost
```

The product loop is:

```text
Measure → Attribute → Explain → Recommend → Change → Measure again
```

## Ultimate value proposition

A platform/SRE team should be able to see something like:

```text
Tenant analytics
12% of rules
37% of query/ruler workload
28% of ingested samples

Top query/ruler contributors:
  customer_activity:7d      21%
  funnel_conversion:30d      9%
  user_sessions:24h          4%
  other                      3%

Primary causes:
  - wide range vectors
  - high evaluation frequency
  - high-cardinality dimensions
  - repeated recomputation

Ingestion-side contributors:
  - high sample volume
  - unnecessary labels
  - low-value / low-usage metrics

Recommended actions:
  - introduce intermediate recording rules
  - reduce evaluation frequency/range where semantically safe
  - remove unnecessary dimensions
  - drop/relabel/aggregate low-value raw metrics where justified

Expected workload reduction: ~30%
```

The important transition is from:

> "This PromQL looks expensive."

to:

> "This rule actually consumed a large amount of backend workload, this is why, and this change is likely to reduce it by X%."

And eventually from:

> "This metric is high-cardinality."

to:

> "This metric contributes materially to ingestion/storage cost, is lightly used, belongs to this team, and here is the safest way to reduce its footprint."

## Product scope: two complementary cost surfaces

The complete offering should eventually cover both sides of metrics infrastructure economics.

### 1. Query / ruler workload

```text
Rule / query
    ↓
evaluation frequency
    ↓
series / samples / chunks / bytes
    ↓
execution time / CPU
    ↓
Tenant / team
```

This is the initial differentiation and primary MVP wedge.

### 2. Raw metric ingestion / storage workload

```text
Metric / series
    ↓
sample volume / cardinality / retention / usage
    ↓
Ingestion + storage footprint
    ↓
Tenant / team
```

This area is already well served by several commercial and OSS products. It should therefore be a later expansion rather than the initial positioning.

However, it is strategically important because it makes the platform complete and allows a team to answer the broader question:

> **What is the total cost/workload contribution of this tenant across both data production and data consumption?**

The two surfaces should ultimately be unified. For example, a high-cardinality metric can cause expensive recording rules, meaning ingestion waste and query waste can be causally connected.

## What to measure first

Do **not** start with euros. First establish trustworthy resource/workload attribution using directly observable quantities:

- rule/query evaluations
- samples processed
- series touched/fetched
- chunks fetched
- bytes fetched
- query/rule execution time
- CPU where available
- output samples generated/ingested where useful
- raw metric ingestion volume
- active series/cardinality where available
- query/alert/dashboard usage where available
- retention/storage footprint where available

The product should prefer statements such as:

> `analytics` accounts for 37% of ruler execution workload and 42% of samples processed.

and later:

> `analytics` accounts for 28% of ingested samples and 37% of query/ruler workload.

rather than claiming an exact monetary amount before the cost model is calibrated.

Economic conversion can be layered on later:

```text
Observed workload
      ↓
resource consumption
      ↓
cluster capacity
      ↓
infrastructure economics
```

## Architecture direction

The current static analyzer is a foundation, not the final product.

Target logical layers:

```text
internal/
  analyzer/          # PromQL AST/static analysis
  loader/            # rule discovery
  tenancy/           # tenant and ownership attribution
  meter/             # execution + ingestion telemetry ingestion
  attribution/       # map observations → rule/metric → tenant → team
  cost/              # workload/resource/economic model
  recommendation/    # remediation suggestions
  report/            # CLI/dashboard/report output
```

### Stable rule identity

Every rule should have a stable identity composed of the fields needed to join static definitions with runtime observations:

```go
type RuleID struct {
    Tenant    string
    Namespace string
    Group     string
    Name      string
}
```

This identity should become the primary join key through the rule attribution pipeline.

### Metering model

The meter should collect raw observations without deciding what they mean.

For runtime query/ruler observations, the initial conceptual shape remains:

```go
type RuleExecution struct {
    Tenant           string
    Namespace        string
    Group            string
    RuleName         string
    Timestamp        time.Time

    DurationSeconds  float64
    SamplesProcessed uint64
    FetchedSeries    uint64
    FetchedChunks    uint64
    FetchedBytes     uint64
}
```

Later, introduce a backend-neutral observation model where fields are optional and source-specific metadata can be retained without contaminating the core attribution model.

## Runtime evaluation statistics and Mimir logs

Mimir should be treated as a first-class high-value telemetry source, not merely as a source of rule definitions.

Mimir can emit **query/evaluation statistics** from components involved in query execution. Current Mimir configuration includes query-stat logging/metrics for ruler evaluations, and Mimir's query-frontend can expose query statistics such as estimated series count, fetched chunk/series/bytes information, query wall time, queue time, response time, cache hits/misses, split/shard counts, etc. citehttps://grafana.com/docs/mimir/latest/references/http-api/https://grafana.com/docs/mimir/latest/configure/configuration-parameters/

The Mimir HTTP API supports requesting query statistics via `X-Mimir-Response-Query-Stats`, with results returned in the `Server-Timing` header. citehttps://grafana.com/docs/mimir/latest/references/http-api/

Mimir also supports ruler query statistics via `-ruler.query-stats-enabled`, which reports ruler-query wall time as a per-tenant metric and an info-level log message. citehttps://grafana.com/docs/mimir/latest/configure/configuration-parameters/

### Why this matters to Obscost

These logs/statistics are potentially one of the most valuable inputs to the attribution engine because they can bridge:

```text
exact query / rule identity
        ↓
execution statistics
        ↓
observed workload
        ↓
tenant / team attribution
```

They may contain substantially richer information than a generic application log or high-level infrastructure metric.

### Loki is a transport/storage detail, not a product dependency

Mimir statistics may be shipped into Grafana Loki or another log aggregation system. Obscost should therefore **not depend on Loki as the canonical architecture**.

Instead, the abstraction should be:

```text
Mimir / Prometheus logs & stats
        ↓
source adapter
        ↓
normalized execution observation
        ↓
attribution engine
```

A Loki adapter can be one implementation of the source interface because Mimir's own dashboards use Loki for slow-query logs. citehttps://grafana.com/docs/mimir/latest/manage/monitor-grafana-mimir/requirements/

### Query text and query hash

When available, retain both:

- exact query text
- stable query hash / source correlation identifier

The exact query is valuable for explanation, static AST analysis, and remediation. A query hash is useful for correlating frontend and downstream/querier observations without relying solely on timestamps or string matching.

### Be precise about "estimated bytes"

Do not assume that one field called `estimated_bytes` exists or means "the exact bytes required to execute the query".

Mimir exposes multiple size/cost-related statistics, including estimated series count and fetched byte/chunk/index statistics depending on the API/log source. These are different concepts and should remain distinct in the normalized data model. citehttps://grafana.com/docs/mimir/latest/references/http-api/

The data model should therefore preserve the original statistic name/meaning rather than collapsing everything into a generic byte estimate.

### Internal vs remote ruler evaluation

This matters for attribution design. Mimir supports internal ruler evaluation, where the ruler runs its own querier, and remote evaluation, where the ruler delegates evaluation to the query-frontend. Remote evaluation can use query acceleration such as sharding. citehttps://grafana.com/docs/mimir/latest/references/architecture/components/ruler/

Therefore the same logical rule can generate different execution telemetry depending on evaluation mode.

Obscost should attribute at the **logical rule level** while retaining component/source information so it can avoid double-counting the same work when frontend and querier/ruler observations represent different stages of the same execution.

This is an important early design constraint.

## Data sources / ingestion strategy

Support multiple sources over time; avoid making the product depend on one telemetry format.

### 1. Mimir evaluation/query logs — preferred first runtime source

Use Mimir query/evaluation statistics as the first serious runtime source when available.

Desired pipeline:

```text
Mimir stats/logs
  ↓
source adapter
  ↓
normalized execution observation
  ↓
RuleID / query identity
  ↓
attribution
  ↓
daily aggregates
```

This should be explored before building an elaborate generic telemetry collector because it may provide unusually rich data for the target backend.

### 2. Generic query logs — first portable fallback

Support Prometheus-compatible query logs and other backend-specific execution logs where they provide rule/query identity and useful statistics.

### 3. Mimir query statistics APIs

Use Mimir APIs and response statistics where practical to enrich observations or calibrate the log-derived data.

### 4. Mimir / Prometheus metrics — infrastructure validation

Collect infrastructure-level signals such as ruler and querier CPU, memory, queueing, errors and related capacity metrics.

The purpose is to validate that rule-level attribution correlates with real cluster resource consumption.

### 5. Raw metric telemetry — later expansion

Add ingestion/cardinality/usage sources after the query/ruler attribution path is proven.

The raw-metric source layer should expose neutral observations such as:

```text
MetricIdentity
Tenant
Timestamp
SamplesIngested
ActiveSeries
Cardinality
BytesStored / estimated storage
Retention
UsageSignals
```

Only introduce fields that can actually be obtained from the target backend.

## Product milestones

### Milestone A — observed workload attribution

Highest priority.

Input:

```text
rule definitions + execution telemetry
```

Output:

```text
cluster
  → tenant
    → rule group
      → rule
```

For each level report:

- workload share
- samples processed
- execution time
- change over time
- top contributors

First useful command should be conceptually:

```bash
promcost report --since 7d
```

The report should answer:

> **Which tenants and rules consume the most ruler/query workload?**

### Milestone B — join observed workload with static analysis

For each rule, combine:

```text
actual observed workload
+
AST-derived characteristics
```

Current PC-S01–PC-S06 checks become explanatory features rather than the product itself.

Example:

```text
customer_activity:7d

Observed:
  45B samples/day
  3,800 execution-sec/day

Contributing factors:
  HIGH  7d range
  HIGH  1m evaluation interval
  MED   high-cardinality output
  MED   regex matcher
```

A major validation goal is to discover which static signals actually predict observed workload. Some existing heuristics may prove weak; that is valuable calibration data.

### Milestone C — deterministic recommendations

Start with a small set of high-confidence remediation patterns:

1. wide range + frequent evaluation → consider intermediate recording rules or a smaller semantically safe window
2. unnecessary output dimensions → reduce cardinality after checking consumers
3. repeated expensive expressions → precompute once and reuse
4. other patterns only after evidence from observed workload

Recommendations must be conservative and explainable.

Every recommendation should show:

```text
Observed workload
Expected workload
Estimated change
Confidence
Reason
```

Do not claim guaranteed euro savings.

### Milestone D — PR forecasting / GitHub Action

A pull request that changes rules should receive a workload impact estimate based on observed production characteristics.

Conceptually:

```diff
+ customer_activity:7d
+ evaluate every 1m
```

becomes:

```text
PROMCOST

New workload:
  +43B samples/day
  +3,100 execution-sec/day

Tenant impact:
  analytics +17%

Cluster impact:
  +4.8%

Risk:
  HIGH

Reason:
  wide range evaluated frequently

Suggested alternative:
  ...
```

This is likely more valuable than a standalone dashboard because it moves cost governance into the engineering workflow.

### Milestone E — before/after verification

After a change is deployed, compare observed workload before and after:

```text
Before
  42B samples/day
  4,800 execution-sec/day

After
   9B samples/day
  1,100 execution-sec/day

Impact
  -79% samples
  -77% execution time
```

This creates the proof loop that makes recommendations trustworthy.

### Milestone F — ownership / team attribution

Map tenants and rules to organizational ownership:

```yaml
ownership:
  analytics:
    team: data-platform
    repository: analytics-observability
```

The product should eventually answer:

> **Which team owns 37% of our Mimir ruler workload?**

not merely:

> Which tenant owns it?

### Milestone G — raw metric ingestion/cardinality attribution

Extend the same attribution engine to raw metric production and storage.

Initial output should answer:

```text
Tenant analytics

Ingested samples:       28% of cluster
Active series:          31% of cluster
Storage footprint:      24% of cluster
Low-use metrics:        17% of tenant samples
```

The first implementation should remain deliberately modest: identify high-impact raw metric sources, attribute them to tenants/teams, and surface usage/cardinality evidence.

### Milestone H — unified metrics infrastructure economics

Eventually provide a unified view:

```text
TOTAL METRICS INFRASTRUCTURE

Ingestion / storage       57%
Query / ruler compute     43%

analytics
  ingestion/storage       11.2%
  query/ruler              8.4%
  total                   19.6%
```

Only at this stage should a mature economic model become a major product surface.

## Cost model principles

### Do not lead with euros

Avoid a simplistic:

```text
query duration → euros
```

Query duration alone is not a sufficient cost model.

Prefer a multidimensional workload model using:

- samples processed
- fetched series
- chunks
- bytes
- execution time
- CPU where available
- evaluation frequency
- ingestion volume
- active series/cardinality
- storage/retention where observable

Then empirically calibrate relationships such as:

```text
samples / bytes / series
      ↓
execution time / CPU / storage
      ↓
cluster capacity
      ↓
economic cost
```

Different clusters can have materially different relationships because of hardware, caching, concurrency, sharding and query execution architecture.

Economic numbers should therefore be labelled as estimates and backed by observed cluster calibration.

## What the product is not

### Not just a PromQL linter

Static linting remains useful, but existing ecosystem tooling already covers rule hygiene. The differentiator is attribution against observed workload.

### Not a standalone cardinality manager

Raw metric/cardinality optimisation is deliberately a later pillar because the market already contains strong offerings. Obscost should add this capability through the same workload attribution and ownership layer rather than becoming another clone of an existing cardinality explorer.

### Not an automatic PromQL rewriter

Automated rewriting is dangerous if semantics or downstream consumers are not understood.

Recommendations should initially be conservative, reviewable and measurable.

### Not a billing system

Infrastructure cost attribution is the eventual economic layer, not the first problem to solve.

## Trust / credibility principle

The product must be:

**Conservative, explainable, calibrated.**

A wrong recommendation or untrustworthy cost number is more damaging than missing a finding.

When uncertain:

```text
show the observation
show the evidence
show the confidence
avoid pretending to know
```

## Design-partner validation

Before treating this as a company-scale product, validate against 3–5 real self-hosted Mimir users/design partners.

The most important evidence is not praise for the CLI. It is whether operators will provide enough telemetry to demonstrate:

1. meaningful tenant/rule workload differences
2. credible attribution accuracy
3. useful recommendations
4. measurable before/after savings
5. whether raw-metric attribution materially improves the value of the unified product

The strongest signal is a customer discovering something like:

> **"We did not realize this tenant was consuming that much of our Mimir capacity."**

## 12-week practical path for a solo developer

### Weeks 1–2

Build execution telemetry ingestion, with Mimir evaluation/query logs and stats as the preferred first source. Store real observations locally and make them queryable.

### Weeks 3–4

Build attribution and daily aggregation. Ship the first `report`-style output showing workload by tenant and rule.

### Weeks 5–6

Join observed workload with the existing AST analyzer. Measure which PC-S01–PC-S06 signals correlate with expensive rules.

### Weeks 7–8

Add only a few high-confidence recommendations with explicit confidence and expected workload impact.

### Weeks 9–10

Build the GitHub Action / PR report for changed rules.

### Weeks 11–12

Run against 3–5 real Mimir environments and use those results to calibrate the model and product positioning.

Raw metric/cardinality attribution should begin only after the query/ruler attribution path produces reliable results in at least one real environment.

## Success metric for the next phase

The most important near-term milestone is not adding PC-S07.

It is being able to reliably produce:

> **Tenant X accounts for 37% of ruler workload, and these 12 rules explain 82% of it.**

The next strategic milestone is to extend that same accounting model to ingestion/storage:

> **Tenant X accounts for 37% of query/ruler workload and 28% of ingestion/storage workload.**

Once both statements are trustworthy, Obscost is moving toward a complete metrics-infrastructure cost governance product rather than an expensive-query analyzer.