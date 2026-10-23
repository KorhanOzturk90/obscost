# Show HN draft

## Title

```
Show HN: PRODUCTNAME – which tenant costs what on a shared Grafana Mimir
```

That's 72 characters with the 11-character placeholder. HN allows 80, so a product name of up to 19 characters fits. If the name is longer, use the fallback:

```
Show HN: PRODUCTNAME – per-tenant cost split for self-hosted Mimir
```

**Link:** `https://github.com/OWNER/REPO`

## First comment (post it as the author, straight after submitting)

---

Hi HN. [TODO(owner): confirm or rewrite. Don't publish an experience the author didn't have.] I built this because of a question I couldn't answer: we run one Mimir cluster for many teams, each team is a tenant, and nobody could say who costs what.

The ingesters, queriers and rulers are shared. Every tenant uses the same pods, so Kubernetes cost tools stop at "the ingester StatefulSet costs X a month". Mimir's `mimir-top-tenants` dashboard shows who is biggest on each axis, but it's 14 separate `topk` panels in raw units. None of them is a share of a total, and none of them says what a series costs.

PRODUCTNAME is a read-only CLI. It reads Mimir's own `cortex_*` metrics from the tenant your meta-monitoring writes into, and splits three shared pools across every tenant:

- ingester memory, by active series (averaged over the window, divided by the replication factor)
- the query path, by fetched chunk bytes
- ruler CPU, by rule-evaluation seconds

By default the output is in resources: "`analytics` uses the equivalent of 5.0 of your 8 ingesters". You get currency only if you write your own price per replica into the config. There's no pricing database.

What it deliberately doesn't do:

- **It doesn't fit weights to your bill.** Series, samples and queries move together, so a regression gives you collinear weights that can flip sign month to month, and you can't explain a fitted coefficient to the team you're charging. A bill is for checking the model, not fitting it.
- **It doesn't spread what it can't measure.** Pools it doesn't model yet (write path, object storage, store-gateway, compactor) are listed as not modelled. Unpriced pools say "not costed". A tenant with no data says "not measured", never 0. A total across pools says what it covers: "45.4% of the 31.2% of platform cost we can price", not "45.4% of your bill". (That example is from a test fixture, not a real cluster.)
- **It doesn't write to Mimir**, run an agent or need a database. It's one binary that sends `GET` requests.

How it was validated: on a staging Mimir 3.2.0 (Helm chart 6.2.0, microservices, 3 zones) with a load generator driving 8 synthetic tenants. The generator records exactly what each tenant delivered. Each one has a shape: high cardinality, churny, query-heavy, rule-heavy, one that starts late, one that goes silent. Each run had an ingester rolling restart inside its 1h window. The 30m and 10m windows are clean, and they're checked at ±1%.

Both write paths pass 87 of 87 checks: ingest storage, and classic with RF=3 (the replication factor is read from Mimir and divided out: 30,022 active series reported for a tenant that sent 30,022). Shares sum to 100% in every pool and window. Active series match ground truth, including for the tenants that start late or go silent, and match Mimir's own gauge for all 9 tenants. The query-heavy tenant gets 93–98% of the query path and the rule-heavy tenant 87–91% of ruler CPU, which is what the workload was built to produce. Rule-output counts are exact. The write path and rule evaluation mode are detected correctly.

The first run didn't pass. It scored 79 of 83, and all four failures were one real bug. Pool 1 used `avg_over_time`, which averages a tenant over the points where its series exists, not over the whole window. Mimir also drops a tenant's active-series gauge once it has no series, rather than reporting 0. So a team that joined an hour ago showed its full size in a 24h report, and a team that left kept its old average. The fix averages over the whole window and counts absence as zero (#66). The 87/87 runs are on the fix, and the failing run is kept in the repo as the bug's evidence.

Results index, failures included: https://github.com/OWNER/REPO/blob/main/dev/staging/results/README.md

Honest limits:

- Only Mimir 3.2.0 is tested.
- Staging is one cloud container (k3s in Docker, 4 vCPU), not a multi-node cluster. No node loss, no network partitions, and no day-long soak: windows are 1h at most.
- Every costed tenant is synthetic, so the ground truth is known, but real workloads have shapes it doesn't cover (native histograms, OTLP, bursty queries).
- The check is that it reads Mimir's per-tenant drivers correctly, not that active series is the right proxy for ingester memory. The query path and ruler are checked by ranking, not absolute value.
- Three of the seven pools in the cost model are covered. The write path, object storage, store-gateway and compactor aren't in v0.1, and every report says so.
- During an ingester rollout, the active-series average reads low while a replica is down (about 9% on the dev rig).
- It's a snapshot over one window. No comparisons or history yet.
- The `--rule-outputs` drill-down runs count queries as each tenant, so they show up in that tenant's query path in later windows (#61). The report says so.

What I'd most like to hear: whether the split matches your intuition on a real cluster, and on which Mimir version or topology it breaks.

---

## Notes for the author

- Reply to every top-level comment in the first two hours. Use [`faq.md`](faq.md) as the source; paste answers, don't paraphrase them into stronger claims.
- If someone asks "is it accurate?", lead with what was checked and what wasn't (see the FAQ). Don't say "accurate".
- Don't post from a new account, and don't ask anyone to upvote.
