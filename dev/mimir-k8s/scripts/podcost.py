#!/usr/bin/env python3
"""Price Mimir's components from node cost, per ADR 0006 decision 3.

    ./scripts/podcost.py [--window 1h] [--cpu-hour 0.04] [--gib-hour 0.005]

The question this answers is the one an operator on a shared cluster
actually faces: nobody bills them for "ingesters", they are billed for
nodes. So for every pod, over the window:

    pod_cost = Σ over {cpu, memory} of
               price_dim × max(request, usage)_dim / node_capacity_dim

max(request, usage) because a reservation costs money whether it is used or
not, and usage above a request is real consumption. Costs are then summed
per component (ingester, querier, …) — those sums are what ADR 0003 calls a
pool's cost, and what promcost's inventory asks the operator to supply.

Whatever the nodes cost that is *not* claimed by any pod is idle capacity.
It is printed as its own line and never spread across components: a cluster
kept at 40% utilisation for headroom has cost that no tenant caused (ADR
0006 decision 4).

Default prices are a made-up but plausible on-demand shape (~$0.04 per
vCPU-hour, ~$0.005 per GiB-hour). Replace them with real ones before
quoting any figure; the point here is the allocation, not the currency.

Usage comes from cAdvisor in the `monitoring` tenant, requests and
capacities from the Kubernetes API.
"""
import argparse
import json
import subprocess
import sys
import urllib.parse
import urllib.request
from collections import defaultdict

MIMIR = "http://localhost:8090"
NANO = 1_000_000_000
GIB = 1024 ** 3


def query(promql):
    req = urllib.request.Request(
        MIMIR + "/prometheus/api/v1/query?" + urllib.parse.urlencode({"query": promql}),
        headers={"X-Scope-OrgID": "monitoring"},
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.load(resp)["data"]["result"]


def kubectl_json(*args):
    out = subprocess.run(["kubectl", *args, "-o", "json"], check=True, capture_output=True, text=True).stdout
    return json.loads(out)


def parse_cpu(value):
    """Kubernetes CPU quantity -> cores."""
    if not value:
        return 0.0
    if value.endswith("m"):
        return float(value[:-1]) / 1000
    if value.endswith("n"):
        return float(value[:-1]) / 1e9
    return float(value)


def parse_mem(value):
    """Kubernetes memory quantity -> bytes."""
    if not value:
        return 0.0
    units = {"Ki": 1024, "Mi": 1024 ** 2, "Gi": 1024 ** 3, "Ti": 1024 ** 4,
             "K": 1e3, "M": 1e6, "G": 1e9, "T": 1e12}
    for suffix, factor in units.items():
        if value.endswith(suffix):
            return float(value[:-len(suffix)]) * factor
    return float(value)


def pod_facts():
    """{(namespace, pod): (cpu cores, memory bytes, workload name)}.

    The workload name comes from the pod's ownerReferences, not from
    trimming its name: a Deployment pod carries a ReplicaSet hash and a
    random suffix (mimir-ruler-679b8b6d6d-lgtqk) while a StatefulSet pod
    carries an ordinal (mimir-ingester-0), and guessing which is which
    from the string gets it wrong often enough to matter in a cost table.
    """
    out = {}
    for pod in kubectl_json("get", "pods", "--all-namespaces")["items"]:
        cpu = mem = 0.0
        for container in pod["spec"]["containers"]:
            requests = (container.get("resources") or {}).get("requests") or {}
            cpu += parse_cpu(requests.get("cpu"))
            mem += parse_mem(requests.get("memory"))
        meta = pod["metadata"]
        owners = meta.get("ownerReferences") or [{}]
        kind, name = owners[0].get("kind", ""), owners[0].get("name", meta["name"])
        if kind == "ReplicaSet":
            name = name.rsplit("-", 1)[0]  # ReplicaSet -> Deployment
        out[(meta["namespace"], meta["name"])] = (cpu, mem, name)
    return out


def node_capacity():
    """Allocatable cores and bytes summed across nodes."""
    cpu = mem = 0.0
    for node in kubectl_json("get", "nodes")["items"]:
        allocatable = node["status"]["allocatable"]
        cpu += parse_cpu(allocatable["cpu"])
        mem += parse_mem(allocatable["memory"])
    return cpu, mem


def usage(window):
    """{(namespace, pod): (cores, bytes)} averaged over the window."""
    cpu = {}
    for row in query(f'sum by (namespace, pod) (rate(container_cpu_usage_seconds_total[{window}]))'):
        cpu[(row["metric"]["namespace"], row["metric"]["pod"])] = float(row["value"][1])
    mem = {}
    for row in query(f'sum by (namespace, pod) (avg_over_time(container_memory_working_set_bytes[{window}]))'):
        mem[(row["metric"]["namespace"], row["metric"]["pod"])] = float(row["value"][1])
    return {key: (cpu.get(key, 0.0), mem.get(key, 0.0)) for key in set(cpu) | set(mem)}


def workload_from_pod_name(pod):
    """Best-effort workload name for a pod the API no longer knows about.

    cAdvisor keeps reporting a pod for a while after it is deleted, and
    those pod-hours are real cost in the window, so they are kept rather
    than dropped — but without ownerReferences the workload has to be
    recovered from the name: Deployment pods carry two generated segments
    (alloy-5dc5cfdb85-7cs89), StatefulSet pods carry an ordinal.
    """
    parts = pod.split("-")
    if len(parts) >= 3 and len(parts[-1]) == 5 and len(parts[-2]) >= 8:
        return "-".join(parts[:-2])
    if len(parts) >= 2 and parts[-1].isdigit():
        return "-".join(parts[:-1])
    return pod


def component_of(namespace, workload):
    """Group pods the way the cost model groups them: by Mimir component."""
    if namespace == "tenants":
        return "tenants (load generators)"
    if namespace != "mimir":
        return f"{namespace} (cluster overhead)"
    return workload[len("mimir-"):] if workload.startswith("mimir-") else workload


def allocate(window, cpu_hour, gib_hour):
    """The ADR 0006 decision 3 allocation, shared with compare-opencost.py.

    Returns (node_cost_per_hour, {component: {...}}, idle_cost_per_hour).
    """
    cap_cpu, cap_mem = node_capacity()
    node_cost_hour = cap_cpu * cpu_hour + cap_mem / GIB * gib_hour
    facts = pod_facts()
    used = usage(window)

    by_component = defaultdict(lambda: {"cost": 0.0, "cpu": 0.0, "mem": 0.0, "pods": 0})
    for key in set(facts) | set(used):
        req_cpu, req_mem, workload = facts.get(key, (0.0, 0.0, workload_from_pod_name(key[1])))
        use_cpu, use_mem = used.get(key, (0.0, 0.0))
        cpu, mem = max(req_cpu, use_cpu), max(req_mem, use_mem)
        row = by_component[component_of(key[0], workload)]
        row["cost"] += cpu * cpu_hour + mem / GIB * gib_hour
        row["cpu"] += cpu
        row["mem"] += mem
        row["pods"] += 1

    allocated = sum(row["cost"] for row in by_component.values())
    return node_cost_hour, dict(by_component), node_cost_hour - allocated


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--window", default="1h", help="averaging window (PromQL range, default 1h)")
    ap.add_argument("--cpu-hour", type=float, default=0.04, help="price per vCPU-hour")
    ap.add_argument("--gib-hour", type=float, default=0.005, help="price per GiB-hour")
    ap.add_argument("--json", action="store_true", help="emit JSON instead of a table")
    args = ap.parse_args()

    cap_cpu, cap_mem = node_capacity()
    node_cost_hour, by_component, idle = allocate(args.window, args.cpu_hour, args.gib_hour)
    allocated = node_cost_hour - idle


    if args.json:
        json.dump({"window": args.window, "node_cost_hour": node_cost_hour,
                   "idle_cost_hour": idle, "components": by_component}, sys.stdout, indent=2)
        print()
        return

    print(f"Node cost basis: {cap_cpu:.1f} cores, {cap_mem / GIB:.1f} GiB allocatable"
          f" -> {node_cost_hour:.4f}/hour at {args.cpu_hour}/vCPU-h and {args.gib_hour}/GiB-h")
    print(f"Allocation over the last {args.window}, by max(request, usage):\n")
    print(f"  {'component':<28} {'pods':>4} {'cores':>7} {'GiB':>7} {'cost/h':>9} {'share':>7}")
    for name, row in sorted(by_component.items(), key=lambda kv: -kv[1]["cost"]):
        print(f"  {name:<28} {row['pods']:>4} {row['cpu']:>7.2f} {row['mem'] / GIB:>7.2f} "
              f"{row['cost']:>9.4f} {row['cost'] / node_cost_hour * 100:>6.1f}%")
    print(f"  {'-' * 68}")
    print(f"  {'allocated':<28} {'':>4} {'':>7} {'':>7} {allocated:>9.4f} {allocated / node_cost_hour * 100:>6.1f}%")
    print(f"  {'idle / headroom':<28} {'':>4} {'':>7} {'':>7} {idle:>9.4f} {idle / node_cost_hour * 100:>6.1f}%")
    print("\nIdle is reported, never spread across components (ADR 0006 decision 4).")
    print("Mimir's own pools are the 'mimir' rows; the rest is what shares the cluster.")


if __name__ == "__main__":
    main()
