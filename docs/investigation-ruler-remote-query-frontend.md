# Investigation: does `-ruler.query-frontend.address` get promcost `samples_processed`?

**Status:** investigated, not pursued. No code or config in this repo changed as
a result — `dev/mimir-local`'s rig still runs the default, local ruler
evaluation it always has.

## The question

`internal/telemetry/mimirlogs`'s package doc says Mimir's ruler query-stats log
"has no `samples_processed` stat, ever." That looked inconsistent with
[Grafana's own Mimir runbook](https://grafana.com/docs/mimir/latest/manage/mimir-runbooks/#how-to-investigate-3),
which documents `msg="query stats"` as carrying `estimated_series_count` and
`samples_processed`. If Mimir's docs say that stat exists on this log line, is
`mimirlogs`'s claim wrong, or is this rig just not exercising the code path
that produces it?

Two possible fixes were on the table: either `mimirlogs`'s claim needed
correcting, or if the rig genuinely wasn't using a code path that produces
richer stats, reconfiguring it to use one (specifically, `-ruler.query-frontend
.address`, which makes the ruler delegate rule evaluation to a remote
query-frontend instead of evaluating locally) might unlock `samples_processed`
for real.

## Finding 1: there are two different `msg="query stats"` log lines

Mimir logs two structurally different lines under the same `msg="query
stats"`, distinguishable by `component=`. Firing one real ruler evaluation and
one manual `curl` query against the same `dev/mimir-local` rig produced both,
back to back:

**The ruler's own line** (`component=ruler`, `pkg/ruler/compat.go`) — every one
of ~4,000 real rule-evaluation lines captured from the rig looked like this:

```
msg="query stats" component=ruler query="..." query_wall_time_seconds=0.02
fetched_series_count=1 fetched_chunk_bytes=0 fetched_chunks_count=0
sharded_queries=0 result_series_count=0
```

**The query-frontend's line** (`component=query-frontend`,
`pkg/frontend/...` request-stats middleware) — produced only by a direct API
call (`curl .../prometheus/api/v1/query?query=up`):

```
msg="query stats" component=query-frontend method=GET path=/prometheus/api/v1/query
route_name=prometheus_api_v1_query user_agent=curl/8.7.1 status_code=200
... estimated_series_count=0 ... samples_processed=0 equivalent_samples_read=0
physical_samples_read=0 ...
```

The Grafana runbook is documenting the second line — the query-frontend's read-path
stats, logged for any client hitting the query API — not the ruler's own
internal evaluation line. `mimirlogs`'s claim is correct for the log line it
actually parses (`component=ruler`); it was never about Mimir's query-stats
logging in general.

## Finding 2: does `-ruler.query-frontend.address` change this?

Checked directly against `grafana/mimir`'s actual source at the pinned tag
(`mimir-3.2.0`), not guessed — `pkg/ruler/compat.go`'s
`RecordAndReportRuleQueryMetrics`:

```go
logMessage := []interface{}{"msg", "query stats", "component", "ruler", "query", qs}
if !remoteQuerier {
    // These statistics will only be populated when using local rule evaluation
    // (ie. not using a remote query-frontend).
    logMessage = append(logMessage,
        "query_wall_time_seconds", wallTime.Seconds(),
        "fetched_series_count", numSeries,
        "fetched_chunk_bytes", numBytes,
        "fetched_chunks_count", numChunks,
        "sharded_queries", shardedQueries,
    )
}
```

The ruler's own line is emitted unconditionally, local or remote — but when
`remoteQuerier` is true it explicitly *drops* the four fields it normally has,
and it never adds `samples_processed` under any condition. Setting
`-ruler.query-frontend.address` cannot enrich this line; it can only shrink it.

### Live confirmation

Confirmed this empirically too, not just from reading the source — in an
isolated, throwaway single-container Mimir 3.2.0 instance (deliberately *not*
the shared `dev/mimir-local` rig; see "Why an isolated test rig" below), with
one tenant and one trivial recording rule (`expr: vector(1)`):

**Baseline** (no `-ruler.query-frontend.address`):

```
component=ruler query=vector(1) query_wall_time_seconds=0.001115666
fetched_series_count=0 fetched_chunk_bytes=0 fetched_chunks_count=0
sharded_queries=0 result_series_count=1
```

**With `-ruler.query-frontend.address=http://127.0.0.1:8080/prometheus` set**,
same rule, same tenant:

```
component=ruler query=vector(1) result_series_count=1
```

— exactly as predicted: four fields gone, nothing gained. And a new line
appears per rule tick, on `component=query-frontend`:

```
component=query-frontend method=POST path=/prometheus/api/v1/query
route_name=prometheus_api_v1_query user_agent=mimir/3.2.0 status_code=200
... samples_processed=0 equivalent_samples_read=0 physical_samples_read=0 ...
param_query=vector(1)
```

(`samples_processed=0` here is expected and not a negative result — `vector(1)`
has no underlying series to read, local or remote, so it can never report
anything else. The point of this test was the log line's *shape*, not the
number.)

### A better-than-expected bonus finding

`user_agent=mimir/3.2.0` on the query-frontend line (confirmed both from
`pkg/ruler/remotequerier.go`'s `req.Header.Set("User-Agent",
version.UserAgent())` and live in the log above) is a clean, ready-made way to
tell a ruler-originated request apart from a real user's query (a human's
`curl`, `mimirtool`, or Grafana dashboard would carry a different
`user_agent`). That removes what looked like the harder of the two problems
with using this line for attribution.

## What it would actually take to use this

Not a config flip — a small but real feature, with a real tradeoff:

1. **New parsing logic**, in `internal/telemetry/mimirlogs` or a new sibling
   `telemetry.Source`: recognize `component=query-frontend` lines, filter to
   `user_agent=mimir/*` (to exclude real end-user API/dashboard traffic that
   would otherwise get misattributed as rule workload), and parse a
   differently-shaped field set (`param_query` instead of `query`, plus
   `samples_processed`/`estimated_series_count`/etc.).
2. **Rule-identity recovery is still needed** — this line has no rule
   name/group either, same limitation `mimirlogs`'s `exprindex.go` already
   solves for the ruler's own line (parse both sides, compare canonical AST
   form, reject ambiguous matches — see
   [ADR 0001, decision 5](adr/0001-observed-workload-attribution-layer.md#5-recovering-a-rules-identity-from-a-log-that-doesnt-have-one)).
   The same technique applies here unchanged, just pointed at a different field.
3. **A genuine Mimir-architecture change, not just a logging toggle** —
   `-ruler.query-frontend.address` is Mimir's real "remote rule evaluation"
   deployment pattern (built to offload ruler CPU/memory onto queriers). Every
   rule evaluation gains a network hop and a dependency on the query-frontend
   and queriers being healthy. That's a real operational tradeoff a platform
   team would need to weigh, independent of what it does for promcost's
   telemetry.

## Recommendation

Not pursued as part of this investigation — this was a spike to answer "is
this possible and what would it cost," not a decision to build it.
`dev/mimir-local`'s rig was never reconfigured (it doesn't set
`-ruler.query-frontend.address` and still doesn't); there is nothing to revert.
If accurate `samples_processed` attribution becomes a priority later, the next
step is a new `telemetry.Source` implementation consuming the
`component=query-frontend` line as scoped above — worth its own ADR given the
architecture tradeoff in point 3, not a quick patch to `mimirlogs`.

## Why an isolated test rig, not `dev/mimir-local`

This was tested against a throwaway, single-container Mimir 3.2.0 instance
(different ports, different container name, filesystem storage, deleted
afterward) rather than the shared `dev/mimir-local` rig. Reason: at the time of
this investigation, `dev/mimir-local`'s actual running containers turned out
to be owned by a different, concurrently-active worktree
(`rig-v2-sim-tenants`), not this checkout — confirmed via `docker inspect`'s
`com.docker.compose.project.config_files` label. Restarting or reconfiguring
those containers from this checkout's copy of `docker-compose.yml` risked
colliding with whatever that other session was doing with it (exactly the
failure mode `AGENTS.md` warns about for this rig). An isolated instance
answers the same question with the same pinned Mimir version and zero
collision risk.

## References

- Grafana Mimir runbook, "How to investigate `msg=\"query stats\"`" logs (the
  runbook that prompted this investigation): <https://grafana.com/docs/mimir/latest/manage/mimir-runbooks/#how-to-investigate-3>
- `grafana/mimir` source at tag `mimir-3.2.0` (the version this repo's dev rig
  pins): `pkg/ruler/compat.go` (`RecordAndReportRuleQueryMetrics`),
  `pkg/ruler/remotequerier.go`, `pkg/ruler/http_roundtripper.go`
- [`docs/runtime-telemetry-notes.md`](runtime-telemetry-notes.md) — flagged
  this exact ruler-vs-frontend distinction as an open problem before this
  investigation had concrete evidence for it
- [ADR 0001](adr/0001-observed-workload-attribution-layer.md) — the existing
  `mimirlogs` source and its expression-matching rule-identity recovery, which
  this investigation's proposed next step would reuse rather than replace
