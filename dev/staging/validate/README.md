# Validation harness

`validate.py` runs `promcost cost` over several windows and checks every
figure it can against [`../workload`](../workload)'s ground truth and
against the invariants promcost promises. Each check is reported as pass or
fail, with its tolerance and **why that tolerance**. The report is written
for a skeptical Mimir operator and is launch evidence.

```bash
export PROMCOST=/path/to/promcost   # the one place the binary is named (the product is being renamed)
./validate.py --env local --url http://localhost:8080 --metrics-tenant infra \
    --ground-truth ../workload/.run/ground-truth.json \
    --expect-eval local --expect-write-path classic --windows 1h,6h
```

Python 3.11+, standard library only. The unit tests run with
`python3 -m unittest discover -s dev/staging/validate`.

## What is checked, per window

| check | tolerance | why that tolerance |
|---|---|---|
| each pool's shares sum to 100% | ±0.1pp | shares are exact divisions of one set of drivers |
| an unmeasured tenant is in `unmeasured`, absent from `tenants`, and its markdown row reads "not measured" in every cell | exact | ADR 0003 decision 5 |
| pool 1 per tenant (after the replication divisor) is within the ground-truth band | model band ±1% (±15% if the window overlaps an operational event) | the gauge lags by up to 75s (1m update + 15s scrape) and idle series are purged up to 1m late; the band is the model at both extremes |
| pool 1 rank order matches the shapes | pairs within 10% are not ranked | a near-tie can flip on gauge lag |
| `dashboards` dominates pool 6, `rules` dominates pool 7 | rank 1 and ≥50% | a margin built in by construction should survive noise |
| ruler evaluation mode, and write path, detected correctly | exact | the environment is known |
| tenants that never query are "not measured" in pool 6 (local evaluation) | exact | they put nothing in that pool |
| `latecomer` before it starts, `ghost` long after it stops | exact | "not measured", or 0 once its series have expired |
| `--rule-outputs` rule counts and series per `record:` name | exact (skipped within one lookback of a start/stop/push) | the outputs are deterministic |
| across an operational event: every counter-pool figure finite and ≥0 | exact | `increase()` must absorb counter resets |

**What "expected" means for pool 1.** The figure is a time-weighted average
over the window. A tenant with no active series at some point contributes 0
there, so a tenant present for a quarter of the window is expected at about
a quarter of its size. Points where the metrics pipeline itself had no data
(before the environment existed) are left out for everyone alike. When
promcost's figure instead matches the average over only the points where the
tenant was present, the check says so.

**promcost's own traffic.** `--rule-outputs` queries each tenant to count
series. That is query-path traffic charged to the tenant. The harness logs
these queries (`observer.jsonl` next to the ground truth) so a pool-6 result
caused by them is labelled as such.

## Output

`../results/<date>-<env>/`:

- `report.md` — the report.
- `checks.json` — every check, machine-readable.
- `raw/` — every promcost run (`cost-<w>.json`, `.md`, `-rule-outputs.json`), the ground-truth snapshot, the generated `promcost.yaml`, and the events file.

## Operational events

`--events events.json` takes a list of `{"name", "start", "end"}` (ISO 8601).
A window overlapping an event gets the wider pool-1 band and the
counter-sanity check, and the report names the event.
