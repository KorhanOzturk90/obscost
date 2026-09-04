# Architecture Decision Records

This folder holds ADRs: short documents that explain a significant design
decision, why it was made, and what it costs — written down at the time,
not reconstructed later from code or memory.

Write one when a decision is non-obvious, hard to reverse, or the kind of
thing a future contributor (or future you) would otherwise have to
rediscover by reading a diff and guessing. Don't write one for a decision
that's just "the obvious way to do it."

## Convention

- Files are numbered sequentially: `0001-short-title.md`, `0002-...`.
- Numbers are never reused, even if a decision is later superseded — add a
  new ADR that says so instead of editing history.
- Each ADR has a **Status**: `Proposed`, `Accepted`, `Superseded by
  ADR-000N`, or `Deprecated`.
- Keep it plain English. Prefer a diagram over a paragraph when a diagram
  would actually make the mechanism clearer, not as decoration.

## Index

| # | Title | Status |
|---|---|---|
| [0001](0001-observed-workload-attribution-layer.md) | Observed workload attribution layer | Accepted |
