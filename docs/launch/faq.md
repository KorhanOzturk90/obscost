# Launch FAQ

Straight answers to the hard questions we expect in the launch threads. Each answer leads with the short version and links its source. If a thread asks something that isn't here, answer from the repo or say you don't know yet.

### Why not OpenCost or Kubecost?

They answer a different question. OpenCost can tell you the ingester StatefulSet costs X a month. It can't tell you which Mimir tenant is using those ingesters, because every tenant shares the same pods. PRODUCTNAME takes the pod or node cost as an input and splits it by tenant. An adapter that reads pool prices from OpenCost is planned as an option, so we don't rebuild node-level cost collection. Source: [ADR 0004, decision 2](../adr/0004-finops-pivot-scope-and-sequencing.md#2-allocate-by-pool-and-driver-do-not-fit-weights-by-regression).

### Why not Mimir's cost-attribution trackers?

They're complementary. `-validation.cost-attribution-trackers` (experimental in Mimir 3.x) splits samples and active series *inside* a tenant by a label, such as `team`. It covers the write side and doesn't price anything. PRODUCTNAME works *across* tenants, covers the query path and ruler CPU as well as ingester memory, and turns shares into replicas or currency. Use cost attribution for "which team inside this tenant" and PRODUCTNAME for "which tenant". Neither can name the rule that's expensive, because evaluation cost and output series don't carry a rule's identity. That's what the `report` and `--rule-outputs` drill-downs are for. Source: [ADR 0003](../adr/0003-cost-model-pool-driver-mappings.md).

### Why not just use the `mimir-top-tenants` dashboard?

It's 14 separate `topk` leaderboards in raw units. They tell you who's biggest on each axis. None of them is a share of a total, they aren't combined, and they don't say what a series costs. PRODUCTNAME gives exhaustive shares that sum to 100% across every tenant, per pool. It uses the same aggregation rules as the mixin (sum by user, then average, then divide by the replication factor).

### Is it accurate?

Short version: the arithmetic is checked against known ground truth, and the model is a choice you can inspect.

What's checked: on a staging Mimir 3.2.0 with 8 synthetic tenants of known shape, both write paths pass 87 of 87 checks: ingest storage, and classic with RF=3 ([results index](../../dev/staging/results/README.md)). In the clean 30m and 10m windows, checked at ±1%, every tenant's active series is inside its ground-truth band, including the tenants that start late or go silent. The figure equals Mimir's own gauge, read independently, for all 9 tenants. Shares sum to 100% in every pool. Rule-output counts are exact. The query-heavy and rule-heavy tenants get 93.2–97.6% of the query path and 87.1–90.7% of ruler CPU. The first run scored 79 of 83, and all four failures were one bug (#66), which was fixed and re-validated.

What's not checked:
- Whether active series tracks ingester memory closely on *your* cluster. It's the driver ADR 0003 chose, and validation checks that PRODUCTNAME reads it correctly, not that it's the right proxy.
- Absolute values for the query path and ruler. Fetched chunk bytes and evaluation seconds depend on chunk encoding, caches and CPU contention, so only their ranking is checked ([#10](https://github.com/OWNER/REPO/issues/10)).
- A multi-node cluster. Staging is one cloud container (k3s in Docker, 4 vCPU), so node loss, network partitions and real cross-host contention aren't exercised.
- Real tenants. All costed tenants are synthetic, with no native histograms, OTLP ingestion or bursty queries.
- Windows longer than 1h, and any Mimir version other than 3.2.0.

During an ingester rollout, the active-series average reads low while a replica is down. We measured about 9% on the dev rig.

Every figure in a report comes with its query, replication factor and price, so you can check it yourself.

### Why not fit weights to the bill?

Because the weights would be noise, and nobody could check them. Series, samples and queries move together, so a regression over a few clusters gives collinear weights that can change sign between months. "Your share of ingester memory is 62%" can be checked and explained to the team being charged; a fitted coefficient can't. A bill is useful for **checking** the model (do the pool costs add up to the invoice?), not fitting it. Source: [ADR 0004, decision 2](../adr/0004-finops-pivot-scope-and-sequencing.md#2-allocate-by-pool-and-driver-do-not-fit-weights-by-regression).

### Where do the prices come from?

From you. Currency only appears if you put a price per replica for a pool in `PRODUCTNAME.yaml`. There's no built-in pricing database and no fitted coefficient. An unpriced pool says "not costed", not 0. A cross-pool total only covers priced pools and says which ones: "45.4% of the 31.2% of platform cost we can price" (an example from a test fixture). If nothing is priced, there's no total.

### What about the pools you don't model?

The ingester write path, object storage, store-gateway and compactor aren't in v0.1. Every report lists them as not modelled, and their cost isn't spread across tenants. ADR 0003 expects object storage to be a small share, because storage is cheap per byte. We haven't measured the other three. So a cross-pool total is only as complete as the pools you price, and it says which ones those are.

### What about Grafana Cloud?

PRODUCTNAME is for a Mimir you run yourself. Grafana Cloud has its own cost management and attribution for its own billing, and you should use that there.

### What about VictoriaMetrics, Thanos or Cortex?

Not supported. v0.1 reads Mimir-specific `cortex_*` metrics and has only been tested on Mimir 3.2.0. VictoriaMetrics support is out of scope for now ([AGENTS.md](../../AGENTS.md), ADR 0004). Mimir's metrics keep the `cortex_` prefix, but nobody has tried PRODUCTNAME on Cortex. Reports are welcome as issues.

### Does it need write access?

No. It only sends `GET` requests: `/prometheus/api/v1/query` as the metrics tenant, plus `/prometheus/api/v1/rules` and per-tenant count queries if you use `--rule-outputs` or `report` without `--dir`. It never writes to Mimir, and it talks to nothing except `backend.url`. It sends no telemetry. Source: [`docs/prerequisites.md`, Permissions and headers](../prerequisites.md#permissions-and-headers).

It does need to read as each costed tenant for `--rule-outputs`. Those queries are then charged to that tenant's query path in later windows (#61), and the report says so.

### What does it cost to run? How much load does it put on Mimir?

A `cost` run is a few instant queries per pool against the metrics tenant. They don't touch the tenants you're costing, unless you use `--rule-outputs`. The heaviest is pool 1's averaging subquery. Its step is the window divided by 1440, with a 1-minute minimum, so a long window never asks for more than 1440 points per tenant. Source: `subqueryStep` in [`internal/cost/mimirdrivers/mimirdrivers.go`](../../internal/cost/mimirdrivers/mimirdrivers.go).

TODO(evidence: we haven't measured the query cost of a run (wall time, fetched bytes) on staging. If someone asks for a number, say it hasn't been measured yet. Don't estimate it.)

### Does it need an agent, a database or a sidecar?

No. It's one binary or container image, run when you want a report. There's a Kubernetes Job/CronJob example for a daily run. A per-cluster agent is planned only for when someone needs history beyond Mimir's own retention ([ADR 0004](../adr/0004-finops-pivot-scope-and-sequencing.md)).

### Can it tell me *why* a tenant's cost went up?

Not yet. v0.1 is a snapshot over one window. Comparing windows is next, then a change timeline built from ruler-API snapshots. What it can do today is name the rule groups (or single rules, with the query-stats log) behind a tenant's ruler cost, and the recording rules that write its series.

### Aren't recording rules the main cost?

Not for ingestion, on the corpus we measured. On the dev rig, tenant `infra`'s 122 recording rules produced about 3.6% of its ingested samples ([#37](https://github.com/OWNER/REPO/issues/37)). On that corpus, evaluating the rules (ruler CPU) was the bigger cost. A recording rule that keeps a high-cardinality label is the exception, and that's what `--rule-outputs` is for.

### My metrics tenant scrapes several Mimir clusters. Does that break it?

Not if you pick one. Use `--selector '{cluster="..."}'` (or `backend.selector`). PRODUCTNAME warns you when it looks like you need to.

### Why is it called PRODUCTNAME, and why is the module path different?

TODO(owner): fill in after the rename, or delete this question.
