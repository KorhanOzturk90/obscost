# Running promcost in Kubernetes

promcost reads Mimir's HTTP API, so it usually runs where Mimir is reachable: in the same cluster. These manifests run the container image as a one-off `Job` (Markdown report) or a daily `CronJob` (JSON on stdout). There is no Deployment, because promcost isn't a service; each run is a snapshot.

| File | What it is |
|---|---|
| `configmap.yaml` | `promcost.yaml`: Mimir's URL and your inventory (replica counts, optional prices) |
| `job.yaml` | `promcost cost --format md`, once |
| `cronjob.yaml` | `promcost cost --format json`, daily at 06:17 |

## Before you apply

1. **`backend.url`** in `configmap.yaml`: your Mimir gateway, nginx or query-frontend Service. Leave off `/prometheus`, since promcost adds it.
2. **`--metrics-tenant`** in `job.yaml` / `cronjob.yaml`: the tenant your Mimir's *own* metrics (`cortex_*`) are scraped into, often a monitoring tenant. It is not a tenant being costed.
3. **`inventory`**: your real replica counts. Add `cost_per_replica_month` and `currency` only if you want currency figures.
4. Optional bearer token: uncomment `backend.auth.bearer_token_env` in the ConfigMap, then
   `kubectl create secret generic promcost --from-literal=bearer-token=<token>`.

## Run it

```sh
kubectl apply -f configmap.yaml -f job.yaml
kubectl wait --for=condition=complete job/promcost-cost --timeout=5m
kubectl logs job/promcost-cost
```

For the daily JSON snapshot:

```sh
kubectl apply -f configmap.yaml -f cronjob.yaml
kubectl create job --from=cronjob/promcost-daily promcost-now   # run once now
kubectl logs job/promcost-now | jq .
```

stdout carries only the report; errors and warnings go to stderr. `kubectl logs` shows both streams, so a failed run's log ends in an `error:` line rather than JSON. A run that fails exits non-zero, which marks the Job failed.

The pods run as non-root (uid 65532) with a read-only root filesystem, no capabilities and no service-account token, because promcost never calls the Kubernetes API. Set `--since` to change the window (default `1h` without the flag; these examples use `24h`), and use `--tenant a,b,c` to list tenants you expect to see so missing ones are reported as "not measured".
