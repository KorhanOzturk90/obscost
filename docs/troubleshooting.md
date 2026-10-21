# Troubleshooting

Each entry gives the symptom, what it means, and what to do. The error messages are the binary's own, copied from real runs.

## Everything is "not measured"

```
No tenant reported any active series in this window.
| analytics | not measured | not measured | not measured |
```

**Most likely:** `--metrics-tenant` names a tenant that doesn't hold Mimir's own metrics. The run still exits 0. promcost reports what it found, which here is nothing.

**Check:** query the metrics tenant directly. It should list your real tenants:

```sh
curl -s -H 'X-Scope-OrgID: <metrics tenant>' 'http://<mimir>/prometheus/api/v1/query' \
  --data-urlencode 'query=count by (user) (cortex_ingester_active_series)'
```

- **Empty:** try the tenant your Alloy or Prometheus `remote_write` sends with. See [Prerequisites → metrics tenant](prerequisites.md#mimirs-own-metrics-and-the-metrics-tenant).
- **Not empty, but some tenants are missing:** those tenants had no active series in the window, or the ingesters that hold them aren't scraped.

Two known display bugs in this case: the query-path and ruler totals render as `0 B` and `0.0` (#52), and pool 1's assumptions still say `write path: classic` (#53). Read both as "not measured".

## One tenant is "not measured" in one pool

This is normal, and it isn't zero. It means Mimir has no series for that tenant's driver in this window:

| Pool | Common reason |
|---|---|
| query path | The tenant sent no queries through the query-frontend. Under **local** rule evaluation, a tenant that only runs rules has no query-path series at all. |
| ruler CPU | The tenant has no rules. |
| ingester memory | The tenant had no active series in the window. |

The tenant is left out of that pool's denominator. In the cross-pool table its total reads `≥ …` (a lower bound), and the measured tenants' figures are inflated by the same amount. The report says this under **Read before quoting**.

Pass `--tenant a,b,c` so expected tenants show up as `not measured`, instead of being silently missing.

## Rule evaluation mode is "unknown"

```
rule evaluation | unknown (neither cortex_prometheus_rule_evaluation_duration_seconds_sum nor
cortex_ruler_query_seconds_total has data in this window: no tenant evaluated rules, or the
metrics tenant does not scrape the ruler)
```

Either no tenant evaluated rules in the window, or the ruler pods aren't scraped into the metrics tenant. Scrape the ruler's `/metrics`, or widen `--since` if rules are rare. promcost doesn't guess the mode, and in v0.1 there's no config override. The mode is inferred, as [Prerequisites → rule evaluation](prerequisites.md#rule-evaluation-local-remote-or-unknown) explains.

## The query path says "no data for any query-path metric"

```
Mimir returned no data for any query-path metric (...): no tenant ran a query in this window,
or the metrics tenant does not scrape the query-frontend
```

Scrape the query-frontend. If you query Mimir through a gateway, the query-frontend pods still have to be scraped themselves.

## The query path driver is "query seconds"

When none of the fetched-volume metrics has data, promcost falls back to `cortex_query_seconds_total`, and the report says so. Time depends on load as well as on what a tenant asked for, so treat these shares as rougher. The assumptions name the skipped drivers and say why each was skipped (`no data` or `all zero`).

## A query times out

```
error: pool ingester_memory: active series query: query gave up after the client timeout of 2m0s;
a heavy query (a long --since, a large cluster under load) can need longer, so raise
backend.timeout in promcost.yaml: ...
```

Raise `backend.timeout` (e.g. `timeout: 10m`), or shorten `--since`. Mimir enforces its own limit too, `-querier.timeout` (default 2m). If Mimir's limit is the one being hit, you'll see an error from Mimir instead, and raising `backend.timeout` alone won't help.

## Auth and tenant header errors

| Message | Meaning |
|---|---|
| `unexpected status 401: ...` | No credentials, or wrong ones. If you set `backend.auth.bearer_token_env`, check that the variable is exported in this shell: when it's empty, promcost sends no token at all and doesn't warn you (#55). Basic auth isn't supported in v0.1. |
| `unexpected status 401: no org id`, or a similar message about a missing tenant | The tenant header didn't arrive. A proxy may be stripping it, or it expects a different header name. Set `tenancy.header`. |
| `unexpected status 403: ...` | Your credentials can't read that tenant. The metrics tenant needs read access. With `--rule-outputs`, so does each costed tenant. |
| `unexpected status 404: ...` | `backend.url` is wrong. It's the base URL: promcost adds `/prometheus/api/v1/query` itself, so don't end it in `/prometheus`. |

## Config errors

All of these fail before any query runs:

| Message | Fix |
|---|---|
| `config's backend.url is empty` | Set `backend.url`. |
| `--metrics-tenant is required` | Pass the tenant that holds Mimir's own metrics. |
| `field X not found in type config.…` | A typo, or a key that doesn't exist. See [Configuration](configuration.md). |
| `inventory.pools.X: unknown pool (known: ingester_memory, query_path, ruler_cpu)` | Use one of the known pool keys. |
| `... has a price but inventory.currency is empty` | Add `inventory.currency`. |
| `inventory.pools.query_path sets both pool-level replicas/cost_per_replica_month and components` | Use one form, not both. |
| `section "cost_model" was removed along with the static analyzer` | Delete that section. It was used by the old `promcost check`. |
| `--rule-outputs is a drill-down under pool ingester_memory` | Add `ingester_memory` to `--pool`, or drop `--rule-outputs`. |
| `--rule-outputs needs rule definitions: give --dir, or --tenant` | Pass one of them. |

## The replication factor looks wrong

Look at pool 1's Assumptions table. It shows the replication factor and its source:

| Source shown | What to check |
|---|---|
| `max(max_over_time(cortex_distributor_replication_factor[...]))` | Read from your distributors. If it's wrong, the distributors are configured that way. During a change in the window, the higher value wins. |
| `promcost.yaml inventory` | Your `inventory.replication_factor`. If Mimir reports something else, a note under **Read before quoting** says so. |
| `not reported by Mimir and not in the inventory` | The distributors aren't scraped. Absolute active series are withheld ("withheld (no RF)"), and shares are still correct. Scrape the distributors, or set `inventory.replication_factor`. |
| write path `ingest storage` | No divisor is used, because the query counts each series once. Any `replication_factor` you set is ignored, with a note. |

**Active series look about RF× too high?** Check the write path line. If your cluster runs ingest storage but the line says `classic`, some ingesters may not export `cortex_partition_ring_partitions`, or the scrape dropped the `job`, `namespace` or `cluster` labels the guard matches on.

**Figures look like two clusters added together?** promcost doesn't filter by `cluster`. Run it against a metrics tenant that holds one Mimir cluster's metrics (#54).

## `--format json` fails with `json: unsupported value: NaN`

This happens when a pool is priced at `cost_per_replica_month: 0` (#57). Use a positive price, or leave the price out so the pool is not costed.

## Still stuck?

Open an issue with:

- the command
- the Assumptions table for the pool that looks wrong (or the `--format json` output)
- your Mimir version, write path and rule evaluation mode

Remove tenant names if they're sensitive.
