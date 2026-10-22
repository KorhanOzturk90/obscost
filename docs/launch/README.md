# Launch drafts (v0.1.0)

These are the announcement drafts for the v0.1.0 launch ([#51](https://github.com/OWNER/REPO/issues/51)). They stay in the repo as the record. The owner posts them.

| File | Where it goes |
|---|---|
| [`hacker-news.md`](hacker-news.md) | Show HN title, and the author's first comment |
| [`reddit-devops.md`](reddit-devops.md) | r/devops |
| [`grafana-community.md`](grafana-community.md) | community.grafana.com, Mimir category |
| [`faq.md`](faq.md) | Answers to have ready for the threads above |
| [`../../.github/release-notes/v0.1.0.md`](../../.github/release-notes/v0.1.0.md) | The GitHub Release body |

## Before posting

1. Replace `PRODUCTNAME` and `OWNER/REPO` everywhere.
2. Resolve every `TODO(evidence: …)`. Each one marks a claim waiting on a report that isn't committed yet. If the evidence doesn't arrive, delete the claim. Don't estimate it.
3. Check that every link resolves on `main` (the validation results land with [#58](https://github.com/OWNER/REPO/pull/58)).
4. Re-check the numbers against the linked reports if a validation re-run has replaced them.

## Where each number comes from

| Figure | Source |
|---|---|
| 79/83 checks, 8 tenants, two ingester rolling restarts, 92.5% / 90.3% | [`dev/staging/results/2026-10-22-ingest/report.md`](../../dev/staging/results/2026-10-22-ingest/report.md) |
| 33/34 checks under local rule evaluation; churn 5,611 vs 5,811–6,211 | [`dev/staging/results/2026-10-22-ingest-local-eval/report.md`](../../dev/staging/results/2026-10-22-ingest-local-eval/report.md) |
| Pool 1 reads ~9% low during an ingester rollout (dev rig) | the tolerance notes in the ingest report above |
| Mimir 3.2.0, chart 6.2.0, RF=3, zone-aware | [`docs/prerequisites.md`](../prerequisites.md), the staging env ([#64](https://github.com/OWNER/REPO/pull/64)) |
| `ghost`/`latecomer` overstated (#66) | [#66](https://github.com/OWNER/REPO/issues/66) and the ingest report |
| Rule output ≈3.6% of `infra`'s ingestion on the rig | [#37](https://github.com/OWNER/REPO/issues/37) |
| At most 1440 subquery points per tenant | `subqueryStep` in [`internal/cost/mimirdrivers/mimirdrivers.go`](../../internal/cost/mimirdrivers/mimirdrivers.go) |
| No regression, OpenCost as an input | [ADR 0004, decision 2](../adr/0004-finops-pivot-scope-and-sequencing.md#2-allocate-by-pool-and-driver-do-not-fit-weights-by-regression) |
| Absence is not zero | [ADR 0003, decision 5](../adr/0003-cost-model-pool-driver-mappings.md) |
| "45.4% of the 31.2% we can price" (example output) | the test fixture [`internal/report/testdata/cost_report_multipool.golden.md`](../../internal/report/testdata/cost_report_multipool.golden.md), not a real cluster |
