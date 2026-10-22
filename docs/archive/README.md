# Archive

Documents that are no longer current design, kept for reference.

Nothing in this folder is a plan. Current design lives in `docs/adr/` and
`PRODUCT-DIRECTION.md`.

| file | what it is | why it's here |
|---|---|---|
| [`promcost-v0-spec.md`](promcost-v0-spec.md) | the full original v0 static-analyzer spec (was `promcost-v0-spec_updated.md` in the repo root; earlier ADRs cite it under that name) | the static-analysis side was removed — [ADR 0006](../adr/0006-remove-static-analysis.md); the code is at tag `v0-static-analyzer` |
| [`v0-static-analyzer-design.md`](v0-static-analyzer-design.md) | the summary of that spec that used to sit in `AGENTS.md` | same |
| [`pivot-mimir-cost-attribution-conversation.md`](pivot-mimir-cost-attribution-conversation.md) | the design conversation that proposed the cost-allocation pivot | historical input to [ADR 0004](../adr/0004-finops-pivot-scope-and-sequencing.md), which corrects its metric names and defaults; not a specification |
| [`pivot-mimir-collection-layer-design.md`](pivot-mimir-collection-layer-design.md) | the per-cluster agent and BigQuery collection plan | re-sequenced by ADR 0004: the agent is deferred, and there is no BigQuery dependency |
| [`runtime-telemetry-notes.md`](runtime-telemetry-notes.md) | early notes on what Mimir's own telemetry can tell you | superseded by [ADR 0001](../adr/0001-observed-workload-attribution-layer.md) and [ADR 0002](../adr/0002-where-workload-evidence-comes-from.md) |
