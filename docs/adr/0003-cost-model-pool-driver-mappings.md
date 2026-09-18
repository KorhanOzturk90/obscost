# 0003: The cost model — mapping tenant behaviour to what you actually pay for

**Status:** Proposed

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

### The second-order effect that makes rules matter more than they look

Pools 1–3 have a feedback loop that a naive reading misses. **A recording
rule does not only consume query capacity — it writes new series, and those
series then cost ingester memory and storage forever.**

A rule that evaluates cheaply but emits 50,000 new series is a far larger
cost event than a slow rule emitting ten. `RuleIngestionRate` in
`/distributor/all_user_stats` measures exactly this, separated from
API-driven ingestion.

This is also what connects the rule attribution already built (ADR 0001,
0002) to the pool that actually dominates the bill. Rules are not a small
slice of cost; they are one of the *inputs* to the largest slice.

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
- Measurements: `dev/mimir-local`, Mimir 3.2.0, 2026-09-16
