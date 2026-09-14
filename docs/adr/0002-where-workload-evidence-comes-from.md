# 0002: Where workload evidence comes from — three sources, and what each can actually answer

**Status:** Accepted — decisions 1, 2 and 4 implemented alongside this ADR
(`internal/telemetry/mimirmetrics`, `attribution.AggregateObservations`, the
`pickRankMetric` reorder). Decision 3's persistence work is not yet built.

## Context

ADR 0001 built one way to find out what a tenant's rules cost: parse Mimir's
ruler query-stats log, match each line back to a rule definition, add it up.

Then a fair question came up: **Mimir already publishes some of this as
ordinary Prometheus metrics.** `cortex_prometheus_rule_evaluation_duration_seconds_sum`
is a per-tenant running total of time spent evaluating rules. If Mimir hands
that to you for free, is the log parser redundant?

The short answer is: **for one specific number, yes — and we should stop
pretending otherwise.** For the numbers the product actually promises, no.
This ADR writes down which source owns which question, so we stop treating
one of them as "the" source and the others as backup.

Everything below was measured against `dev/mimir-local` on 2026-09-13, not
inferred from docs.

---

## What we actually have available

There are three places rule workload information can come from. They are not
three flavours of the same thing — they reach different depths and have
different lifespans.

### Source 1 — Mimir's own metrics

Ordinary Prometheus metrics the ruler exposes on `/metrics`. Here is every
rule-related metric, and crucially, **what labels each one carries**, since
the labels are what determine how deep you can drill:

| metric | labels | how deep it goes |
|---|---|---|
| `cortex_prometheus_rule_evaluation_duration_seconds_sum` | `user` | tenant only |
| `cortex_prometheus_rule_group_duration_seconds` | `user` | tenant only — *despite the name* |
| `cortex_prometheus_rule_evaluations_total` | `user`, `rule_group` | rule group |
| `cortex_prometheus_rule_group_last_duration_seconds` | `user`, `rule_group` | rule group, last run only |
| `cortex_prometheus_rule_group_last_rule_duration_sum_seconds` | `user`, `rule_group` | rule group, last run only |
| `cortex_prometheus_rule_group_rules` | `user`, `rule_group` | rule group |

Two things matter here and both are easy to miss:

**It stops at the rule group.** No metric carries a rule name. You can learn
that the `mimir_alerts` group cost something; you can never learn which rule
inside it did.

**There is no measure of data volume at all.** We checked every
`cortex_ruler_*` and `cortex_prometheus_rule_*` metric for anything
describing fetched series, chunks, or bytes. The only match is
`cortex_ruler_queries_zero_fetched_series_total` — a *count of queries that
fetched nothing*, with no volume attached. So the metrics can tell you how
long rules took, and how often they ran. They cannot tell you how much data
they moved.

### Source 2 — the ruler API (`/prometheus/api/v1/rules`)

This one does reach individual rules. A real response from the rig:

```json
{
  "name": "MimirAlertmanagerSyncConfigsFailing",
  "type": "alerting",
  "health": "ok",
  "lastEvaluation": "2026-09-13T22:13:05.845947597Z",
  "evaluationTime": 0.002739083
}
```

`evaluationTime` is genuine per-rule timing — better granularity than the
metrics give. But note `lastEvaluation`: this is **a snapshot of the most
recent run only**. It is not a history. To build a picture over time you
would have to poll this endpoint continuously and store the answers
yourself, and you can never ask it about the past.

### Source 3 — the ruler query-stats log

What ADR 0001 already parses. One real line, all fields:

```
ts=2026-09-13T22:08:50.95Z user=infra component=ruler query="..."
query_wall_time_seconds=0.007962666
fetched_series_count=0 fetched_chunk_bytes=0 fetched_chunks_count=0
sharded_queries=0 result_series_count=0
```

This is the only source carrying `fetched_series_count`,
`fetched_chunk_bytes`, `fetched_chunks_count` and `sharded_queries`. It has
no rule name — ADR 0001 decision 5 recovers that by matching query text,
and deliberately refuses the cases where two rules share an expression
rather than guessing between them.

That refusal rate is not a constant; it depends on how much a tenant's rule
corpus repeats itself. ADR 0001 measured ~4% refused on a 290-line sample.
The larger captures taken for this ADR both landed at ~3% (120 of 4,014
lines, and 64 of 2,142 in a second run) — so ~97% matched is what the
figures below refer to, measured here rather than inherited from ADR 0001.
And all of it only exists if somebody was capturing the log at the time.

### Side by side

| | metrics | ruler API | query-stats log |
|---|---|---|---|
| per tenant | yes | yes | yes |
| per rule group | counts + last-duration | yes | yes |
| **per rule** | **no** | yes | yes (~97%, via text matching) |
| wall time | yes | yes | yes |
| **data volume fetched** | **no** | **no** | **yes** |
| covers the past | yes, *if* it was being stored | no — snapshot only | only what was captured |
| exact, no guessing | yes | yes | ~97%, rest refused |
| needs setup to work | scraping + storage | none | log capture |

---

## Finding 1: wall time is a poor stand-in for what rules actually cost

This is the finding that most changes what we should build.

We captured 5 minutes of real rule evaluations from the `infra` tenant —
240 distinct rules, 1,227 executions — and ranked them two ways: by time
spent, and by data fetched. **If those two rankings agreed, wall time would
be a fine proxy and the metrics would be nearly sufficient.** They do not
agree:

| rank by wall time | rank by data fetched | share of wall time | share of series fetched | rule |
|---|---|---|---|---|
| **4** | **1** | 3.0% | 14.9% | `histogram_quantile(0.99, sum by (le, cluster, job, route) (rate(cortex_request_duration...)))` |
| **22** | **2** | 0.9% | 14.9% | `histogram_quantile(0.5, sum by (le, cluster, job, route) (...))` |
| **15** | **3** | 1.1% | 14.9% | `sum by (le, cluster, job, route) (rate(cortex_request_duration...))` |
| **1** | 185 | 3.7% | 0.0% | `(label_replace((kube_horizontalpodautoscaler_status_condition...)))` |
| **2** | 80 | 3.5% | 0.0% | `(max by (cluster, namespace, rollout_group) (...))` |
| **3** | 58 | 3.5% | 0.0% | `(quantile by (cluster, namespace) (0.9, ...))` |

Read the second-from-top row: a rule pulling **14.9% of all data fetched**
by this tenant sits at rank **22** when sorted by time. Ranked by the clock,
it looks like a rounding error. Ranked by what it pulls off the ingesters,
it's one of the top three things this tenant does.

And the headline number:

> **186 of 240 rules fetched zero series — yet they consumed 68.4% of total
> query wall time.**

Two thirds of the measured "workload" in this environment is time spent on
rules that touch no data whatsoever. A report ranked by wall time would put
those at the top and call them the tenant's biggest cost drivers. They
aren't; they are mostly query planning over metrics that don't exist here.

This matters beyond a ranking nicety. Time spent is felt by the ruler's own
CPU. Data fetched is felt by ingesters and store-gateways — which is where
Mimir capacity is usually actually strained. **These are different costs
with different owners, and the source that can see the second one is the
log.**

## Finding 2: "the metrics give you history" has a real catch

We claimed metrics are better than logs partly because they're already
stored as time series, so you get the past for free. That claim needs a
qualifier, and the rig proved it accidentally.

`dev/mimir-local` runs Alloy as the component that scrapes metrics and
writes them into Mimir. **Alloy had been dead for 8 days** (exit code 137 —
killed, most likely out of memory). Nobody noticed. During those 8 days:

| | state during the outage |
|---|---|
| ruler kept evaluating rules | yes — 5,352 evaluations in a 20-minute sample |
| `/metrics` still showed ruler counters | yes — counters kept climbing |
| those numbers stored anywhere queryable | **no** |
| tenants with any data at all | **0 of 4 — every database empty** |
| history recoverable afterwards | **no, permanently gone** |

After restarting Alloy, we asked Mimir for 24 hours of ruler history:

```
datapoints in last 24h: 1 (of 48 possible at 30m steps)
earliest: 2026-09-13 22:33 UTC   <- the moment Alloy came back
```

So the precise statement is: **a metric is a running total, not a history.**
`cortex_prometheus_rule_evaluation_duration_seconds_sum` is one number that
counts up since the ruler started. It becomes history only if something
scrapes it on a schedule and stores it. If that pipeline breaks, the past is
unrecoverable — exactly like an uncaptured log, just quieter about it.

This also means every workload figure this rig produced for the last 8 days
was measuring rules running against **empty databases**. Rules still take
time to plan and execute against nothing, which is why the numbers looked
plausible rather than obviously broken.

## Finding 3: the two sources do agree where they overlap

Worth stating, because it is the reassuring half. Comparing our
log-derived tenant shares against Mimir's own metric, over different time
windows and derived completely independently:

| tenant | promcost, from logs | Mimir's own metric | difference |
|---|---|---|---|
| infra | 87.7% | 86.9% | +0.8pp |
| analytics | 8.5% | 9.4% | −0.9pp |
| payments | 3.6% | 3.4% | +0.2pp |
| platform | 0.3% | 0.3% | ~0 |

Within about one percentage point everywhere. The log-matching approach from
ADR 0001 is sound. The issue was never that it's inaccurate — it's that for
*this particular number*, there was a cheaper and exact way to get it.

---

## Decisions

### 1. Three sources, each owning the question it's actually best at

Stop treating the log as the primary source with others as validation.
Assign each question to whichever source answers it best:

| question | source that owns it | why |
|---|---|---|
| What share of ruler time does each tenant use? | **metrics** | exact, free, no capture needed |
| Which rule groups run most, and how often? | **metrics** | exact, per-group labels exist |
| What did this look like last week? | **metrics** (if stored) | the only source with a past |
| **Which individual rules explain a tenant's load?** | **log** | metrics cannot name a rule |
| **How much data does a rule pull?** | **log** | nothing else measures it |
| What is this rule doing right now? | **ruler API** | no capture setup needed |

The product promise has two halves — *"tenant analytics is 37% of ruler
workload, **and these rules explain 82% of it**"*. Metrics own the first
half better than we do. Only the log can deliver the second.

### 2. Rank by data volume before wall time

`attribution.Aggregate` currently picks its ranking metric in this order:

```
wall time  →  fetched bytes  →  fetched series  →  fetched chunks  →  samples  →  executions
```

Wall time first is wrong, for two reasons Finding 1 makes concrete: it is
the dimension the metrics already give away for free (so ranking by it adds
nothing the log is needed for), and 68.4% of it here was spent by rules that
did no data work at all.

**New order:**

```
fetched bytes  →  fetched chunks  →  fetched series  →  wall time  →  samples  →  executions
```

Wall time stays in the chain — it is still real, still the right answer when
no volume stats were measured, and still what ruler CPU pressure looks like.
It simply stops being the default story when something better was measured.

### 3. Persist only what Mimir doesn't already keep

There's an appealing idea that this should grow into a long-running
collector that stores workload history. Findings 1 and 2 argue for something
much smaller. Mimir already stores most of this; the honest question is
**what does Mimir not keep that we'd lose?**

| information | already kept by Mimir? |
|---|---|
| per-tenant evaluation time over time | yes (if scraping is healthy) |
| per-group evaluation counts over time | yes |
| per-rule resource stats (`fetched_*`) | **no — log only, then gone** |
| what the rules *were* at a past point in time | **no — API shows only "now"** |

Only two things need storing, and the second is easy to overlook: **the
ruler API tells you what rules exist right now, and git tells you what the
files say, but nothing records when a rule definition actually reached
Mimir.** Any "this change made it worse" claim needs that timeline.

**Decision:** persist only those two. And publish the first back **into
Mimir as new series via remote-write**, rather than into a private store —
so promcost's own output is queryable in Grafana, alertable, and retained
by whatever policy the team already runs.

Remote-write specifically, and this distinction matters: a *recording rule*
cannot do this job. A recording rule only evaluates PromQL over series
Mimir already holds, and the whole point of this data is that it does not
exist in Mimir at all — it is parsed out of a log the ruler emits and then
discards. There is no expression that can conjure it. Getting externally
computed values in means remote-write, or an exporter that Alloy scrapes.

### 4. No collector service yet

Deliberately deferred, consistent with the v0 non-goal "no UI, no
daemon/SaaS control plane" (`promcost-v0-spec_updated.md`, restated in
`AGENTS.md`). Historical attribution and regression detection are buildable
*today* against metrics already in Mimir, with no new infrastructure. Build those first; revisit a collector only if
someone needs a retention window Mimir can't give them.

---

## First actionable step

**Add a metrics-backed source so `promcost report` can run with no log
capture at all.**

Concretely — `promcost report --since 7d` against a Mimir URL, with no
`--telemetry` flag, producing a tenant + rule-group report with real
history, by querying:

- `cortex_prometheus_rule_evaluation_duration_seconds_sum` → per-tenant time
- `cortex_prometheus_rule_evaluations_total` → per-group counts
- `cortex_prometheus_rule_group_last_duration_seconds` → per-group duration

Why this first:

- **It removes the biggest adoption blocker.** Today promcost cannot produce
  anything until someone figures out how to capture ruler logs. This version
  runs against a URL.
- **It's the only path to history**, which every later milestone
  (regression detection, before/after verification) depends on.
- **It's small** — one `telemetry.Source` implementation over the PromQL
  API, no storage, no new architecture.
- **It proves decision 1** by making the split real rather than theoretical.

Two things to fix alongside it, both small:

1. **The ranking reorder (decision 2)** — a change to one priority list,
   justified by Finding 1.
2. **A rig health check.** Finding 2 only surfaced by accident. Something
   should fail loudly when the rig ingests nothing, or every future
   measurement taken against it is quietly meaningless. Alloy's OOM also
   needs a memory limit or restart policy so it doesn't silently die again.

Explicitly **not** in this step: the query-frontend telemetry source (see
`docs/investigation-ruler-remote-query-frontend.md` — needs a real Mimir
architecture change), the static-analysis join (issue #9), and any
before/after experiment work (needs its own methodology ADR — comparing a
rule against its former self across two time windows cannot hold load
constant, and getting that wrong produces confidently wrong numbers).

---

## Consequences

- **The log parser is not redundant, but it is no longer the entry point.**
  It becomes the component that adds per-rule depth and resource dimensions
  on top of a metrics-derived skeleton.
- **Reported "workload share" will change meaning** for anyone using
  mimirlogs telemetry, since ranking switches from time to data volume. The
  report header already names its active metric, so this is visible rather
  than silent — but it will reorder existing output.
- **Coverage reporting gets honest.** Today's coverage block answers "of the
  lines you gave me, how many matched?" With a metrics denominator it can
  answer the question that actually matters: "what fraction of the workload
  Mimir itself reports have we accounted for?"
- **Any number measured against `dev/mimir-local` before 2026-09-13 22:33
  UTC should be treated as suspect** — it was taken while the rig ingested
  nothing.
- **Two sources can disagree, and that becomes a feature.** When metrics say
  a tenant used 30% and attributed rules only explain 18%, the gap is real
  information (uncaptured window, ambiguous matches, remote evaluation)
  rather than an error to hide.

## References

- [ADR 0001](0001-observed-workload-attribution-layer.md) — the log-parsing
  attribution layer this builds on; decision 3 (missing vs zero) and
  decision 5 (recovering rule identity) both carry forward unchanged
- [`docs/investigation-ruler-remote-query-frontend.md`](../investigation-ruler-remote-query-frontend.md)
  — why `samples_processed` is unavailable on the ruler's log line
- `promcost-v0-spec_updated.md` — the "no daemon/SaaS control plane" v0
  non-goal that decision 4 respects (restated in `AGENTS.md`)
- Measurements: `dev/mimir-local`, Mimir 3.2.0, 2026-09-13, tenant `infra`
  (240 distinct rules, 1,227 executions over a 5-minute window)
