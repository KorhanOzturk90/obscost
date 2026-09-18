# 0006: Remove the static-analysis side (`promcost check`, PC-S01–PC-S06)

**Status:** Accepted

## Context

promcost began as a static PromQL analyzer. `promcost check` loaded rule
files and ran six structural checks (`PC-S01`–`PC-S06`: subquery density,
heavy range selectors, interval ratio, output labels, ruler limits and
tenant resolution) against a golden corpus. The v0 spec also planned a
live tier (`PC-L0x`, behind a `Meter` interface), a fleet tier (`PC-F0x`)
and `scan`/`explain`/`rewrite`/`pint-config`. None of those were built.

[ADR 0004](0004-finops-pivot-scope-and-sequencing.md) moved the product to
cost allocation for self-hosted Mimir. After that, `AGENTS.md` described the
static side as **frozen**: it still built and ran, but got no new checks, and
was expected to come back later for pull-request forecasting and for
explaining why a rule is expensive.

Keeping it frozen had real costs:

- **It was most of the config.** `promcost.yaml` had `checks`, `pint`,
  `limits` and `cost_model` sections, plus `backend.type` and
  `backend.max_concurrent_queries`. Only `check` read any of them, and
  nothing at all read `cost_model`. The v0 per-unit EUR coefficients in
  `cost_model` contradict ADR 0003 decision 3 ("never invent
  coefficients").
- **It dragged in extra packages and CI.** `internal/analyzer`,
  `internal/limits`, `internal/meter` (an interface with no
  implementation), the findings renderers, `rule.Finding`, the
  `testdata/corpus` golden corpus, and two CI gates over that corpus.
- **It pulled the product towards linting.** pint already owns rule
  hygiene. Every reader of the repo had to learn that `check` exists,
  that it doesn't matter, and that it shouldn't be extended.

## Decision

Remove the static side outright rather than keeping it frozen:

- Remove the `check` command, `internal/analyzer` (including `checks/`),
  `internal/limits`, `internal/meter`, `rule.Finding`/`Severity`, the
  findings md/json renderers, the golden corpus and its CI gates.
- Remove the config sections and fields that only the static side read. A
  config that still contains one fails to load with an error naming it,
  so the problem is obvious rather than a generic unknown-field error.
- `tenancy.unmapped: error` used to mean "keep the rule with an empty
  tenant and let PC-S06 report it". It now makes the directory loader
  return a load error for that rule file, which `report` treats as fatal.
  Without PC-S06 the old meaning would have silently done nothing.
- The last commit on `main` that has all of this is tagged
  **`v0-static-analyzer`**. The design stays in
  [`docs/archive/`](../archive/).

## Consequences

- If PR workload forecasting or a static "why is this rule expensive"
  explanation comes back, it starts from the tag and is designed against
  the cost model. It would not reuse `check`'s finding and severity shape.
  Issues #9 (join workload with static analysis) and #12 (PR forecasting
  Action) are closed as not planned.
- Issue #37 (rule → output series) planned to use `Meter.SeriesCount`. It
  should use the PromQL client in `internal/promapi` instead.
- `promcost report` is the only command until the cost commands land. Its
  output is unchanged for configs that don't use a removed section.
