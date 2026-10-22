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

Hi HN. I built this because of a question I couldn't answer: we run one Mimir cluster for many teams, each team is a tenant, and nobody could say who costs what.

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

How it was validated: I built a production-shaped staging Mimir (3.2.0, Helm chart 6.2.0, microservices, zone-aware, RF=3, ingest-storage write path) and a load generator with 8 synthetic tenants. The generator records exactly what each tenant delivered. Each one has a shape: high cardinality, churny, query-heavy, rule-heavy, one that starts late, one that goes silent. I ran two ingester rolling restarts inside the measured windows.

The first run passed 79 of 83 checks. Shares summed to 100% in every pool and every window. Active series matched ground truth exactly for the five tenants with a constant series count. The query-heavy tenant had 92.5% of the query path and the rule-heavy tenant had 90.3% of ruler CPU, which is what the workload was built to produce. Rule-output counts were exact for all 8 tenants. The write path and rule evaluation mode were detected correctly.

All four failures had one cause, and it was a real bug. Pool 1 averaged a tenant over the points where it existed, not over the whole window. So a team that joined an hour ago showed its full size in a 24h report, and a team that left kept its old average. Mimir also drops a tenant's active-series gauge when it goes to zero, rather than reporting 0, which made it worse. It's fixed (#66). TODO(evidence: the #66 re-run result and link. Until it exists, say "fixed, re-validation pending".)

The full reports, including the failures, are in the repo: https://github.com/OWNER/REPO/tree/main/dev/staging/results

Honest limits:

- Only Mimir 3.2.0 is tested. TODO(evidence: classic write-path staging report. Until then: "the classic write path is tested on a local rig, not on staging".)
- Three of the seven pools in the cost model are covered. The write path, object storage, store-gateway and compactor aren't in v0.1, and every report says so.
- During an ingester rollout, the active-series average reads low while a replica is down (about 9% on my dev rig).
- It's a snapshot over one window. No comparisons or history yet.
- The `--rule-outputs` drill-down runs count queries as each tenant, so they show up in that tenant's query path in later windows (#61). The report says so.

What I'd most like to hear: whether the split matches your intuition on a real cluster, and on which Mimir version or topology it breaks.

---

## Notes for the author

- Reply to every top-level comment in the first two hours. Use [`faq.md`](faq.md) as the source; paste answers, don't paraphrase them into stronger claims.
- If someone asks "is it accurate?", lead with what was checked and what wasn't (see the FAQ). Don't say "accurate".
- Don't post from a new account, and don't ask anyone to upvote.
