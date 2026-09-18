# 0003: The cost model — mapping tenant behaviour to what you actually pay for

**Status:** Proposed — amended 2026-09-18 after the review on PR #26 (issue
#36). The **change-attribution layer is deliberately out of scope here**:
this is a snapshot model, answering what each tenant costs over one window.
Deltas, their decomposition and naming causes are specified separately in
ADR 0005 (issue #31). Amendments are marked **[A]**, and each says what was
measured rather than asserted.

## Context

Everything built so far measures *usage*: this tenant ran that many rule
evaluations, fetched that many bytes. Useful, but it is not the question a
platform team actually has, which is:

> **What does each tenant cost me, and what would I save if they changed?**

The obvious objection is that Grafana already ships this. Its
`mimir-top-tenants` dashboard has 14 panels covering active series, in-memory
series and their growth, samples rate and its growth, discarded samples,
exemplars, query expression length, rule group size, rule evaluation time,
compaction jobs, and store-gateway disk. That is genuinely a lot, and any
tool that just reprints those numbers in a terminal is redundant.

But pulling apart the dashboard's own queries shows what it structurally
cannot do:

```
total queries:              14
using topk():               14   ← every panel is a leaderboard
computing a share of total:  0   ← nothing is normalised
```

Three consequences follow, and they are the whole reason this ADR exists:

**It is fourteen separate leaderboards, never combined.** If a tenant is #1
by series, #3 by samples rate and #5 by rule time, nothing says what their
*total* share is. Combining axes requires knowing how much each axis costs —
which is a fact about your infrastructure, not about Mimir.

**Nothing is a share of anything.** `topk` also hides the long tail: if
tenants 11–200 collectively outweigh your top 10, no panel will say so. Cost
attribution has to be exhaustive and sum to 100%.

**They are resource counters, not cost.** "15,005 active series" is a fact,
not a decision. Turning it into one requires knowing that active series
drive ingester *memory*, and how many ingesters you run.

**[A] The strongest version of the prior art, stated fairly.** Three of
those 14 panels are not plain leaderboards: in-memory series growth,
received-samples-rate growth and discarded-samples-rate growth each compute
a delta across the dashboard window using `@ start()` / `@ end()`
(re-counted 2026-09-18 against `dev/mimir-local/mixin/dashboards/mimir-top-tenants.json`:
14 queries, 14 using `topk`, 0 computing a share, 3 computing growth). Those
growth panels are the nearest existing thing to a change report, so the
argument is *not* "nobody shows change". It is that they show change
**per axis, as a leaderboard, in resource units, with no cause** — a tenant
can top the series-growth panel and the samples-growth panel without anything
saying what that cost, which of them matters more, or what changed to cause
it. That gap is what this ADR and ADR 0005 close between them.

So the gap is not data collection. It is **the conversion** — and the
conversion needs two things Mimir cannot supply: which resource pool each
behaviour consumes (domain knowledge, this ADR) and what that pool costs
(operator knowledge, supplied as config).

---

## What Mimir actually exposes per tenant

Verified against `dev/mimir-local` on 2026-09-16 by listing every
`cortex_*` metric carrying a `user=` label, then checking that the
interesting ones hold real values. The useful ones, grouped by the pool they
speak to:

| pool it speaks to | per-tenant metric | real sample value |
|---|---|---|
| ingester memory | `cortex_ingester_active_series` | `analytics 15005` |
| ingester write path | `cortex_distributor_received_samples_total` | `analytics 60020` |
| ingester write path | `cortex_distributor_received_bytes_total` | `infra 1.36e9` |
| storage | `cortex_ingester_tsdb_storage_blocks_bytes` | `analytics 3.76e7` |
| store-gateway | `cortex_bucket_store_blocks_loaded_size_bytes` | present |
| store-gateway | `cortex_bucket_blocks_count` | present |
| compactor | `cortex_bucket_index_estimated_compaction_jobs` | present |
| query path | `cortex_query_fetched_chunk_bytes_total` | `infra 510321` |
| query path | `cortex_query_samples_processed_total` | `infra 701464` |
| query path | `cortex_query_seconds_total` | `infra 3.87` |
| ruler | `cortex_prometheus_rule_evaluation_duration_seconds_sum` | `infra 292.05` |
| ruler | `cortex_ruler_query_seconds_total` | `infra 221.65` |

Plus the admin endpoint `/distributor/all_user_stats`, which returns per
tenant in one call:

```json
{"userID":"analytics","ingestionRate":1451.5,"numSeries":15005,
 "APIIngestionRate":1451.5,"RuleIngestionRate":0}
```

### A correction to ADR 0002

ADR 0002's side-by-side table has a row reading *"data volume fetched:
metrics **no**, log **yes**"*. Scoped to the ruler subsystem that is
correct — no `cortex_ruler_*` or `cortex_prometheus_rule_*` metric carries a
volume figure, which is what that ADR checked. But stated generally it is
misleading: **`cortex_query_fetched_chunk_bytes_total{user}` exists and
carries real values.**

The distinction that reconciles them is the evaluation path. Those
`cortex_query_*` metrics are recorded by the *query path* — query-frontend
and querier. A ruler evaluating rules **locally** (Mimir's default, and what
this rig runs) never touches that path, which is why on this rig only
`infra` has a value, and only because queries were issued against it by
hand. All four tenants evaluate rules constantly and none of that shows up
there.

So: for user-facing queries, volume is available as a metric. For locally
evaluated rules, the ruler log remains the only source — ADR 0002's
conclusion stands, its table row was over-broad.

---

## The mappings

This is the core of the ADR. Each row says: what you write a cheque for,
what physically makes it grow, and which per-tenant number tracks it.

| # | Resource pool | What makes it grow | Per-tenant driver | Confidence |
|---|---|---|---|---|
| 1 | **Ingester memory** | every active series is held in memory | `cortex_ingester_active_series` | **high** — this is the well-understood dominant cost of Mimir |
| 2 | **Ingester CPU / write path** | samples appended per second | rate of `cortex_distributor_received_samples_total` | **high** |
| 3 | **Object storage** | bytes of blocks retained × retention | `cortex_ingester_tsdb_storage_blocks_bytes`, `cortex_bucket_blocks_count` | **medium** — storage is cheap per byte; usually a small share |
| 4 | **Store-gateway memory + disk** | blocks that must be loaded to serve historical reads | `cortex_bucket_store_blocks_loaded_size_bytes` | **medium** |
| 5 | **Compactor CPU** | number of compaction jobs, which follows block count | `cortex_bucket_index_estimated_compaction_jobs` | **low** — plausible, not yet validated |
| 6 | **Query path (frontend + querier)** | data actually fetched to answer queries | `cortex_query_fetched_chunk_bytes_total`, `cortex_query_samples_processed_total`, `cortex_query_seconds_total` | **medium-high** — but only covers query-path traffic (see correction above) |
| 7 | **Ruler CPU** | time spent evaluating rules | `cortex_prometheus_rule_evaluation_duration_seconds_sum`, `cortex_ruler_query_seconds_total` | **high** |

### [A] How each driver must be read

Naming a metric is not enough to use it. Each driver also has a *type*
(which decides how it is turned into a figure over a window) and an
*aggregation* (which decides what is summed, divided or maxed first). Both
were missing, and neither is obvious — the mixin's own queries are the
evidence:

| # | Driver | Type | Aggregation over the window | Aggregation across replicas |
|---|---|---|---|---|
| 1 | `cortex_ingester_active_series` | gauge | average (or a quantile) over the window; a max flatters spikes | `sum by (user)`, **then divide by `max(cortex_distributor_replication_factor)`**, and exclude ingest-storage instances with `unless on (cluster, namespace, job) cortex_partition_ring_partitions` |
| 2 | `cortex_distributor_received_samples_total` | counter | `increase()` over the window (never a bare difference — counters reset) | `sum by (user)` |
| 3 | `cortex_ingester_tsdb_storage_blocks_bytes`, `cortex_bucket_blocks_count` | gauge | average over the window | `sum by (user)`, replication divisor applies to the ingester-side metric |
| 4 | `cortex_bucket_store_blocks_loaded_size_bytes` | gauge | average over the window | **`max by (user)` of `sum by (user, pod)`** — blocks are replicated across store-gateways, so summing counts a block once per replica |
| 5 | `cortex_bucket_index_estimated_compaction_jobs` | gauge | average over the window | `sum by (user)` |
| 6 | `cortex_query_*_total` | counter | `increase()` over the window | `sum by (user)` |
| 7 | `cortex_prometheus_rule_evaluation_duration_seconds_sum`, `cortex_ruler_query_seconds_total` | counter | `increase()` over the window | `sum by (user)` |

Three consequences worth stating plainly:

- **A uniform replication factor cancels in a share, and does not cancel in
  anything else.** "5 of your 8 ingesters", any absolute figure, and every
  currency figure are wrong by exactly the replication factor if it is
  skipped. `/distributor/all_user_stats` says so itself — the admin page
  prints *"NB stats do not account for replication factor"* — and the rig
  hides the problem by running RF=1.
- **Counters and gauges cannot share a code path.** Pools 2, 6 and 7 need
  `increase()`; the rest need an average. A snapshot model can blur this; a
  delta model (ADR 0005) cannot.
- **An ingester rollout changes the scrape set**, which moves a gauge
  average for reasons that have nothing to do with tenant behaviour. That is
  ADR 0005's problem to handle, but it starts here.

### [A] The second-order effect that makes rules matter — with its magnitude measured

Pools 1–3 have a feedback loop that a naive reading misses. **A recording
rule does not only consume query capacity — it writes new series, and those
series then cost ingester memory and storage for as long as they are
retained.**

The original text went further and called rules "one of the *inputs* to the
largest slice". That was asserted, not measured. Measured on the rig
2026-09-18, tenant `infra`:

```
/distributor/all_user_stats:  APIIngestionRate  353.35 samples/s
                              RuleIngestionRate  13.33 samples/s   → 3.6%
ruler API:                    122 recording rules, 122 alerting rules
ruler CPU over the same period: rule evaluation 38.3s, ruler queries 31.9s
```

So on a mixin-shaped corpus, 122 recording rules account for under 4% of
ingestion, while evaluating all 244 rules is real ruler CPU. **Rule output
is a driver of pools 1–3 whose magnitude is corpus-dependent, not a
presumed dominant cost.** The case that matters is the opposite one — a
tenant whose recording rules `sum by (...)` over high-cardinality data can
invert this ratio, and catching that is exactly the product's job.

Two cautions on the evidence:

- **`RuleIngestionRate` vs `APIIngestionRate` is a two-way scalar split.**
  It can corroborate *"the growth came from rules"*. It can never say
  **which** rule, and no amount of aggregation makes it able to.
- Naming the rule needs the rule definitions, which is promcost's own
  advantage: a recording rule's `record:` field is its output metric name,
  so its series are countable directly (issue #37). Label-based attribution
  — Mimir's cost-attribution trackers, or Grafana Cloud's product built on
  them — structurally cannot do this, because rule output carries whatever
  labels the expression produced, not the identity of the rule.

### How a share becomes a cost

For each pool, a tenant's share is its driver over the sum of all tenants'
drivers — exhaustive, summing to 100%, no `topk`:

```
share(tenant, pool) = driver(tenant, pool) / Σ driver(*, pool)
cost(tenant)        = Σ over pools [ share(tenant, pool) × cost(pool) ]
```

`cost(pool)` is the only thing promcost does not measure. It comes from the
operator.

**Worked example**, using this rig's real active-series figures:

| tenant | active series | share of pool 1 | if you run 8 ingesters |
|---|---|---|---|
| analytics | 15,005 | 62.4% | ≈ 5.0 ingesters |
| infra | 7,743 | 32.2% | ≈ 2.6 ingesters |
| payments | 1,205 | 5.0% | ≈ 0.4 ingesters |
| platform | 105 | 0.4% | ≈ 0.03 ingesters |

*"`analytics` is consuming the equivalent of 5 of your 8 ingesters"* is a
sentence a platform lead can take into a budget conversation. No dashboard
produces it, and it converts to currency the moment they supply an instance
cost — but it is already actionable without one.

#### [A] What a cross-pool total may claim

`cost(tenant)` above sums over pools, and decision 3 says an unpriced pool
is reported as **not costed**, never zero. Those two facts collide: a single
blended *"X is 40% of your Mimir cost"* silently claims a completeness the
model explicitly refuses to claim, because the denominator only contains the
pools that happened to be priced.

**Rule: a cross-pool total always names its own coverage, or it is not
rendered.** The reportable form is

> `analytics` is **40% of the 78% of platform cost we can currently price**
> (ingester memory, write path, ruler; storage and compactor not costed).

with the priced fraction computed from the operator's inventory, not
assumed. A per-pool share needs no such qualifier — it is complete within
its pool by construction. When no pool is priced at all, the report shows
resource shares only and says so, rather than showing a percentage of
nothing.

---

## Decisions

### 1. Attribute by share-of-driver within a pool, never by leaderboard

Every figure is a share of a pool total, computed across *all* tenants, and
the shares sum to 100%. A "top 10" view may be rendered, but it is always a
view over exhaustive data, never the data itself. This is the difference
between "who is biggest" and "what does each cost".

### 2. Denominate in resources first; currency is opt-in

The primary output is *"5 of your 8 ingesters"*, not *"€4,200/month"*. The
resource form is verifiable by the operator against their own console; the
currency form is the same number multiplied by a figure they supplied.

This is the practical reading of `PRODUCT-DIRECTION.md`'s "do not start with
euros": not a ban on cost, but a ban on cost figures that cannot be traced
back to something measured.

### 3. Never invent coefficients — ask for a short inventory

promcost does not ship a pricing database and does not guess instance
shapes. The operator supplies a small block of things they already know:
replica counts per component, instance type or hourly cost, object storage
rate, retention.

Anything not supplied means the pools that depend on it are reported as
**not costed**, not as zero. The report is explicit about which pools were
priced and which were not.

### 4. Every figure carries its assumption trail

A cost figure must state the pool it came from, the driver used, the share
computed, and the operator-supplied constant applied. If the operator
disputes a coefficient they can change it and watch the answer move. A
number nobody can interrogate is not usable in a budget argument, which is
the only place these numbers matter.

This is `PRODUCT-DIRECTION.md`'s existing requirement ("every estimate in a
report must carry its assumption trail") applied to cost specifically.

### 5. A missing driver is "not measured", never zero

Per-tenant driver coverage is genuinely uneven, and this was verified rather
than assumed: on this rig all four tenants evaluate rules continuously, yet
only `infra` has a value for `cortex_ruler_query_seconds_total`, and only
`infra` appears in the `cortex_query_*` family at all.

A tenant missing a driver must be excluded from that pool's denominator and
reported as unmeasured for it — never folded in as a zero, which would both
understate them and inflate everyone else's share. This extends the
missing-vs-zero principle (ADR 0001 decision 3) from stats to cost.

#### [A] …and that exclusion does not survive a second window

Excluding an unmeasured tenant is correct within one window and a trap
across two. If `payments` is unmeasured on Monday and measured on Tuesday,
the denominator gains a member, so **every other tenant's share falls** for
reasons unrelated to anything they did. Worse, decision 1 guarantees the
shares sum to 100% on both days, so the output looks perfectly consistent
and the artefact is invisible.

**Amendment: a comparison is only valid over a stable measured set.** When
two windows are compared:

1. the comparison is computed over the tenants measured in **both** windows;
2. tenants that entered or left the measured set are reported as their own
   line item — *"`payments` became measurable on Tuesday; it holds 6% of
   pool 1 and is excluded from the day-over-day figures above"*;
3. the same rule applies to a pool entering or leaving the priced set.

A share change caused by denominator membership is never folded into
another tenant's delta. ADR 0005 (issue #31) builds the rest of the delta
model on top of this; it is stated here because it is a correction to
*this* decision.

### 6. Mappings must be calibrated, not asserted — and this rig cannot do it

The confidence column above is a set of hypotheses, not measurements. The
honest test is: does a pool's actual resource consumption track the driver
we claim drives it? That means correlating, say, ingester memory against
active series across real load.

**This is not possible on `dev/mimir-local` as it stands.** It runs Mimir as
a single monolithic process, so there is exactly one
`process_resident_memory_bytes` series with no component label — ingester,
querier, compactor and ruler memory are indistinguishable. Calibration
needs a deployment where components are separate processes.

That makes the microservices/k3d test environment a prerequisite for
decisions 1–4 being *defensible*, not merely a nice-to-have. Until then the
mappings ship as clearly labelled hypotheses, and the low-confidence rows
(5, and arguably 3 and 4) should be presented as such.

This is issue #10 ("validate workload attribution against Mimir
infrastructure metrics"), which turns out to be the core of the product
rather than a QA task.

---

## What this explicitly does not do

- **No per-query or per-request billing.** Attribution is over a window, per
  tenant, per pool. Costing an individual query needs data Mimir does not
  retain.
- **No network or egress modelling.** Real, but not attributable per tenant
  from anything Mimir exposes.
- **No pricing database.** No cloud SKUs, no assumed instance costs.
- **No claim of accounting precision.** These are engineering-grade
  attributions for capacity and chargeback conversations, not invoices.

---

## First actionable step

**Ship pool 1 alone, end to end** — ingester memory, driven by active
series, with an operator-supplied ingester inventory.

Why just one pool:

- It is the **highest-confidence mapping** and usually the dominant cost, so
  it is the most useful single answer.
- The driver needs **no logs, no parsing, no matching** — one metric already
  verified present for all four tenants.
- It exercises **every piece of machinery the full model needs**: exhaustive
  shares, operator inventory config, the assumption trail, and the
  not-measured path. If that shape is wrong, it is much cheaper to discover
  with one pool than seven.
- It produces the target sentence immediately: *"analytics is consuming the
  equivalent of 5 of your 8 ingesters."*

Then add pools in confidence order (2 → 6/7 → 3/4 → 5), and stand up the
microservices test environment before any of the low-confidence mappings are
presented as anything other than hypotheses.

### [A] Acceptance criteria, restated

The criteria above are all *level* statements, and they under-test the
model. Two additions, neither of which is much extra work:

- **Aggregation is part of the acceptance test, not an implementation
  detail.** Pool 1 is only correct if the replication divisor and the
  ingest-storage guard are applied (see "How each driver must be read"). On
  the rig, RF=1 makes a wrong implementation indistinguishable from a right
  one, so this needs a unit test with RF>1 fixtures rather than a rig check.
- **Ship it as a change statement if ADR 0005 lands first.** Per the review,
  and per the finding that day-over-day figures need no stored history —
  these are meta-monitoring metrics already in a Prometheus/Mimir, so
  "yesterday" is a range query, not a sink. The target sentence then becomes
  *"`analytics` holds 62% of ingester memory, up 4pp on yesterday: 3pp its
  own growth, 1pp `payments` shrinking"* — same pool, same driver, one more
  query, and it exercises the decomposition before the report shape is
  fixed.

---

## Consequences

- **Rules stop being the headline and become one input.** The work in ADR
  0001/0002 keeps its value — per-rule attribution is still the only way to
  tell a tenant *what to change* — but it now sits under a cost model rather
  than standing in for one.
- **The tool starts needing configuration it did not need before.** Up to
  now `report` ran read-only with a URL. Costing needs an inventory block.
  That is a real adoption cost, and it is why resource-denominated output
  must work with a partial inventory rather than demanding a complete one.
- **The product claim gets sharper and more falsifiable.** "Which tenant is
  responsible for what share of ruler workload" becomes "what each tenant
  costs you and why" — better, but it invites "prove it" in a way the old
  claim did not. Decisions 4, 5 and 6 exist to survive that question.
- **Calibration becomes a standing obligation.** A mapping that was right
  for Mimir 3.2 on one topology may drift. The assumption trail is what
  makes drift visible rather than silent.
- **[A] This model answers "what does each tenant cost", and only that.**
  Every question of the form "…and why did it change" needs the delta layer
  in ADR 0005. The amendments above are the parts of that layer which are
  corrections to *this* ADR rather than additions on top of it: how drivers
  aggregate over a window, and what happens to a share when the measured set
  changes.

## References

- [ADR 0001](0001-observed-workload-attribution-layer.md) — observed
  workload attribution; decision 3 (missing vs zero) extends into decision 5
  here
- [ADR 0002](0002-where-workload-evidence-comes-from.md) — which telemetry
  source answers which question; corrected above on per-tenant fetched
  volume
- `PRODUCT-DIRECTION.md` — "do not start with euros", and the assumption
  trail requirement that decision 4 implements
- Grafana `mimir-top-tenants` dashboard —
  `operations/mimir-mixin-compiled/dashboards/mimir-top-tenants.json`, the
  14-panel prior art this ADR is positioned against
- GitHub issue #10 — infrastructure-metric validation, reclassified here
  from QA task to core calibration layer
- [ADR 0004](0004-finops-pivot-scope-and-sequencing.md) — the pivot that
  adopts this cost model (decision 2) and sequences the work around it
- The review on PR #26, tracked as issue #36 — the source of every **[A]**
  amendment; issue #31 (ADR 0005, change attribution), #29 (driver
  verification), #37 (rule → output-series join)
- Measurements: `dev/mimir-local`, Mimir 3.2.0 — original 2026-09-16;
  amendments 2026-09-18 (mixin query counts, `/distributor/all_user_stats`
  rule-vs-API ingestion split, ruler CPU, rule counts from the ruler API)
