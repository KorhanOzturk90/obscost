# Obscost Product Direction

## Thesis

Obscost should not become a PromQL linter with a cost score attached.

The long-term product is **observability workload attribution and governance for shared Prometheus-compatible infrastructure**, with Mimir as the first target.

The core question is:

> **Where is my Mimir capacity going, who is consuming it, why, and what can we change?**

The desired hierarchy is:

```text
Infrastructure → Tenant → Team → Rule group → Rule → PromQL expression → Workload
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
37% of ruler workload

Top contributors:
  customer_activity:7d      21%
  funnel_conversion:30d      9%
  user_sessions:24h          4%
  other                      3%

Primary causes:
  - wide range vectors
  - high evaluation frequency
  - high-cardinality dimensions
  - repeated recomputation

Recommended actions:
  - introduce intermediate recording rules
  - reduce evaluation frequency/range where semantically safe
  - remove unnecessary dimensions

Expected workload reduction: ~30%
```

The important transition is from:

> "This PromQL looks expensive."

to:

> "This rule actually consumed a large amount of backend workload, this is why, and this change is likely to reduce it by X%."

## What to measure first

Do **not** start with euros. First establish trustworthy resource/workload attribution using directly observable quantities:

- rule evaluations
- samples processed
- series touched/fetched
- chunks fetched
- bytes fetched
- query/rule execution time
- CPU where available
- output samples generated/ingested where useful

The product should prefer statements such as:

> `analytics` accounts for 37% of ruler execution workload and 42% of samples processed.

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
  meter/             # execution telemetry ingestion
  attribution/       # map observations → rule → tenant → team
  cost/              # workload/resource/economic model
  recommendation/    # remediation suggestions
  report/            # CLI/dashboard/report output
```

### Stable rule identity

Every rule should have a stable identity composed of the fields needed to join rule definitions with runtime observations:

```go
type RuleID struct {
    Tenant    string
    Namespace string
    Group     string
    Name      string
}
```

This identity should become the primary join key through the attribution pipeline.

### Metering model

The meter should collect raw execution observations without deciding what they mean:

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

The attribution layer turns these observations into daily/rolling aggregates at rule, tenant and team level.

## Data sources / ingestion strategy

Support multiple sources over time; avoid making the product depend on one telemetry format.

### 1. Query logs — first prototype

Use query logs as the simplest way to bootstrap real observed execution data. Prometheus query logs identify recording/alerting rule groups and expose query execution information.

Desired pipeline:

```text
query log
  ↓
parser
  ↓
RuleExecution
  ↓
attribution
  ↓
daily aggregates
```

### 2. Mimir query statistics — richer observed cost

Use Mimir's query statistics to enrich attribution with dimensions such as samples processed, fetched series/chunks/bytes and query timing.

This should be the richer source for cost/workload modelling.

### 3. Mimir / Prometheus metrics — infrastructure validation

Collect infrastructure-level signals such as ruler and querier CPU, memory, queueing, errors and related capacity metrics.

The purpose is to validate that rule-level attribution correlates with real cluster resource consumption.

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

Then empirically calibrate relationships such as:

```text
samples processed
      ↓
execution time / CPU
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

The strongest signal is a customer discovering something like:

> **"We did not realize this tenant was consuming that much of our Mimir capacity."**

## 12-week practical path for a solo developer

### Weeks 1–2

Build execution telemetry ingestion, preferably from query logs first. Store real observations locally and make them queryable.

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

## Success metric for the next phase

The most important near-term milestone is not adding PC-S07.

It is being able to reliably produce:

> **Tenant X accounts for 37% of ruler workload, and these 12 rules explain 82% of it.**

Once that works on real infrastructure, the project has moved from an interesting static analyzer toward a genuine workload attribution product.
