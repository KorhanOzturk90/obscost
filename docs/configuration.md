# Configuration: `promcost.yaml`

Pass the file with `--config`. Only `backend.url` is required. Every other key is optional, and a partial `inventory` is the normal case: anything you leave out is reported as "shares only" or "not costed", and never guessed.

The loader is strict:

- **Unknown keys are an error.** A typo like `timout:` fails with `field timout not found in type config.BackendConfig`, instead of being silently ignored.
- **Sections from the removed static analyzer** (`checks`, `pint`, `limits`, `cost_model`, `backend.type`, `backend.max_concurrent_queries`) fail with a message telling you to delete them.

For a commented starting point, see [`examples/promcost.yaml`](../examples/promcost.yaml).

## Overview

```yaml
backend:
  url: http://mimir-gateway.mimir.svc:8080
  auth:
    bearer_token_env: MIMIR_TOKEN
  timeout: 5m

tenancy:
  header: X-Scope-OrgID
  discovery:            # only for --dir rule loading
    - source: static
      map: {analytics-rules: analytics}
  unmapped: error

inventory:
  currency: EUR
  platform_cost_month: 4000
  replication_factor: 3
  pools:
    ingester_memory: {replicas: 8, cost_per_replica_month: 310}
    query_path:
      components:
        query-frontend: {replicas: 2, cost_per_replica_month: 60}
        querier:        {replicas: 6, cost_per_replica_month: 180}
    ruler_cpu: {replicas: 2, cost_per_replica_month: 120}
```

## `backend`

| Key | Type | Default | Meaning |
|---|---|---|---|
| `url` | string | none (**required** by `cost`, and by `report` unless it uses `--dir` and `--telemetry`) | Base URL of Mimir's HTTP API, usually the gateway or the query-frontend. promcost appends `/prometheus/api/v1/query` and `/prometheus/api/v1/rules`, so don't include `/prometheus` yourself. |
| `auth.bearer_token_env` | string | unset | The **name** of an environment variable holding a bearer token. promcost sends it as `Authorization: Bearer <token>`. If the variable is empty, no header is sent (#55). This is the only auth method in v0.1. |
| `timeout` | Go duration (`90s`, `5m`, `1h`) | `2m` | Client-side limit on each query. `2m` matches Mimir's default `-querier.timeout`. Raise it for long `--since` windows on large clusters. Day units (`1d`) aren't accepted here, unlike `--since`. |

## `tenancy`

| Key | Type | Default | Meaning |
|---|---|---|---|
| `header` | string | `X-Scope-OrgID` | The HTTP header that carries the tenant ID on every request. Change it only if a proxy in front of Mimir expects a different header. |
| `discovery` | list | empty | Maps rule files loaded with `--dir` to tenants. Only used by `report --dir` and `cost --rule-outputs --dir`. |
| `unmapped` | `error` \| `skip` \| `tenant:<name>` | `error` | What to do with a rule file under `--dir` that no discovery entry maps. `error` fails the load. `skip` drops the file. `tenant:<name>` assigns it to `<name>`. |

### `tenancy.discovery`

Each entry has a `source`. In v0.1 only `source: static` does anything:

```yaml
tenancy:
  discovery:
    - source: static
      map:
        team-a: analytics   # rules under <--dir>/team-a/... belong to tenant analytics
        team-b: payments
```

The key is the **first directory under `--dir`**. Other `source` values and the `key` and `transform` fields are accepted by the parser, but they never match anything in v0.1.

Without `--dir`, rule definitions come from Mimir's ruler API for the tenants in `--tenant`, and `discovery` isn't used.

## `inventory`

What you know about your own deployment. promcost never ships a default, a price list, or an estimate for any of these.

| Key | Type | Rules | Meaning |
|---|---|---|---|
| `currency` | string | **Required** as soon as any price or `platform_cost_month` is set | A label printed next to every figure (`EUR`, `USD`, `credits`). promcost doesn't convert anything. |
| `platform_cost_month` | number | > 0. Needs `currency` | What the whole Mimir platform costs per month, including the pools promcost doesn't model. It's the only thing that lets the cross-pool headline say "X% of the **Y%** of platform cost we can price". Without it, that fraction is reported as unknown. |
| `replication_factor` | integer | ≥ 1 | Overrides `cortex_distributor_replication_factor`. Use it when that metric isn't in your metrics tenant. It affects only pool 1's absolute figures (shares cancel it), and it's ignored under ingest storage, where a note says so if it isn't 1. If it disagrees with what Mimir reports, the report says so. |
| `pools` | map | keys: `ingester_memory`, `query_path`, `ruler_cpu` | Replica counts and prices per pool. Any other key is an error that lists the known pools. |

### `inventory.pools.<pool>`

A pool takes **either** the flat form **or**, for `query_path` only, `components`. Setting both on the same pool is an error.

| Key | Type | Rules | What it unlocks |
|---|---|---|---|
| `replicas` | integer | ≥ 1 | The "≈ N replicas" column: "the equivalent of 3.3 of your 8 ingesters" |
| `cost_per_replica_month` | number | ≥ 0. Needs `currency` | With `replicas`, the pool cost (`replicas × price`) and the per-tenant currency column. A price **without** `replicas` leaves the pool not costed, and the report says why. Use a positive price: a price of 0 currently produces `NaN%` (#57). |
| `components` | map | `query_path` only. Keys: `query-frontend`, `querier` | Prices the query path per component, each with its own `replicas` and `cost_per_replica_month` |

| Pool key | ADR 0003 pool | Replica noun in the report |
|---|---|---|
| `ingester_memory` | 1 | ingesters |
| `query_path` | 6 | query-frontend and querier components |
| `ruler_cpu` | 7 | rulers |

**Partial component lists.** If you price only `querier`, the pool cost is the querier's. The report then says the pool is "priced from 1 of its 2 components", and that its cost and every tenant's cost are lower bounds. Shares are unaffected.

**Components not in the list.** The query-scheduler, and the dedicated ruler query-frontend and queriers, have no key of their own. To include them, add their cost to the `query-frontend` or `querier` price.

**What a price should be.** The monthly cost of one replica, however you account for it: the instance price for a dedicated node, or the pod's share of a shared node (for example from OpenCost). promcost multiplies it by the replica count and splits the result by share. It never checks the price against anything.

## Flags that interact with the config

| Flag | Config it relies on |
|---|---|
| `--metrics-tenant` | sent in `tenancy.header` to `backend.url` |
| `--pool` | restricts which `inventory.pools` entries are used. The rest are reported as "not read in this run" |
| `--rule-outputs --dir` | `tenancy.discovery` and `tenancy.unmapped` |
| `--rule-outputs --tenant`, or `report` without `--dir` | `backend.url`, read via the ruler API as each tenant |

Run `promcost cost --help` and `promcost report --help` for the full flag lists.
