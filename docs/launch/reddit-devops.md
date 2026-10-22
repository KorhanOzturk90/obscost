# r/devops draft

Check the subreddit's self-promotion rules on the day. Post it as a text post, not a link post, and say plainly that it's your project.

## Title

```
I built a CLI that splits a shared Grafana Mimir's cost across tenants (open source, read-only)
```

## Body

---

If you run one Mimir cluster for several teams, you've probably been asked "how much of this is us?" and had no good answer. [TODO(owner): confirm or rewrite. Don't publish an experience the author didn't have.] I hadn't, so I built PRODUCTNAME. It's my project, Apache-2.0: https://github.com/OWNER/REPO

**What it does**

It reads Mimir's own metrics (the `cortex_*` series your meta-monitoring already scrapes) and splits three shared components across every tenant:

| Component | Split by |
|---|---|
| Ingester memory | active series |
| Query path | data each tenant's queries fetch |
| Ruler CPU | rule-evaluation time |

Output looks like this (from a test fixture, not a real cluster):

```
**`analytics` uses 40.9% of ingester memory** — the equivalent of 1.2 of your 3 ingesters, 122.65 EUR/month.

| Tenant | Active series | Share | ≈ ingesters | EUR/month |
|---|---:|---:|---:|---:|
| analytics | 15,005 | 40.9% | 1.23 | 122.65 |
| monitoring | 12,643 | 34.4% | 1.03 | 103.35 |
```

The EUR column only appears because the config has a price per ingester. Without prices you get shares and replica counts.

**How you run it**

```sh
PRODUCTNAME cost --config PRODUCTNAME.yaml --metrics-tenant meta --since 24h
```

`--metrics-tenant` is the tenant that holds Mimir's own metrics, not one you want to cost. That's the most common setup mistake. It's a single binary or container image, it only sends `GET` requests to Mimir, and there's a Kubernetes Job/CronJob example if you want a daily report.

**What it won't do**

- Make up a number. Anything it couldn't measure says "not measured", and anything you didn't price says "not costed".
- Cover everything. Write path, object storage, store-gateway and compactor aren't modelled yet, and every report says so.
- Compare windows or keep history. It's a snapshot for now.

**How it was tested**

It was tested on a staging Mimir 3.2.0 with 8 fake tenants, where we knew exactly what each one sent. There was an ingester rolling restart during each run. It passes 87 of 87 checks on both write paths (ingest storage, and classic with RF=3). Active series match what each tenant actually sent, and shares sum to 100%.

The first run didn't pass. It scored 79 of 83, and the 4 failures were one bug: tenants that joined or left mid-window were overstated. It's fixed, and the 87/87 runs are on the fix. Reports, failures included: https://github.com/OWNER/REPO/blob/main/dev/staging/results/README.md

The limits: staging is a single cloud container (k3s in Docker), not a multi-node cluster, and the tenants are synthetic. Only Mimir 3.2.0 is tested so far. If you run another version and try it, I'd like to know what breaks.

---
