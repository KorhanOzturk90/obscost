# Production-shaped Mimir staging environment. See README.md.
#
#   make cluster            # local k3s-in-Docker cluster (skip it for any other cluster)
#   make deploy             # everything, against whatever KUBECONFIG points at
#   make deploy PROFILE=classic
#   make mimir RULER_EVAL=local  # switch rule evaluation in place (remote is the default)
#   make status             # pods, rings, per-tenant active series
#   make promcost-config    # writes .state/promcost.yaml pointing at this env
#   make cost               # the CLI's `cost` command against the env, md + json
#   make event-<name>       # operational events, see events/ (`make events` lists them)
#   make events-json        # the event log as validate.py --events input
#   make down               # delete the local cluster (or `make undeploy` on a real one)
#
# Cluster bootstrap (`cluster`, `down`) is separate from `deploy` on
# purpose: `deploy` only ever talks to KUBECONFIG, so the same target
# installs onto this container's k3s, a laptop k3d or a real GKE/EKS
# cluster.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c

REPO_ROOT := $(abspath ../..)
STATE     := $(CURDIR)/.state

# The product CLI. The product is being renamed: change CLI_NAME here (or
# pass it) and every target follows. Build it with `make cli`.
CLI_NAME ?= promcost
CLI      ?= $(REPO_ROOT)/bin/$(CLI_NAME)
# Where `make cli` builds from. The cost pools beyond ingester memory are
# on PR #50's branch until it merges; then this becomes HEAD.
CLI_REF  ?= origin/pools-6-7-cost

# ingest  - Mimir 3.x / chart default: distributors write to Kafka (primary)
# classic - distributors replicate to ingesters (RF=3) over gRPC
PROFILE ?= ingest
# remote - rules evaluated through the chart's dedicated ruler read path (default)
# local  - the ruler evaluates rules itself; switchable in place (`make mimir`)
RULER_EVAL ?= remote

CLUSTER     ?= staging
K3S_IMAGE   ?= rancher/k3s:v1.33.4-k3s1
# Port the Mimir gateway is forwarded to on this machine.
MIMIR_PORT  ?= 8091
GRAFANA_PORT ?= 3091

MIMIR_CHART_VERSION ?= 6.2.0
KPS_CHART_VERSION   ?= 77.0.0
ALLOY_CHART_VERSION ?= 1.12.1

# Pull every image through Docker Hub (via mirror.gcr.io) instead of
# quay.io / registry.k8s.io / ghcr.io. Needed while the cloud container's
# network policy denies those registries; harmless elsewhere. 0 to use the
# charts' own image references.
DOCKERHUB_ONLY ?= 1

# deploy's KUBECONFIG: an explicit one wins, then the local cluster's.
ifeq ($(origin KUBECONFIG),undefined)
ifneq ($(wildcard $(STATE)/kubeconfig),)
export KUBECONFIG := $(STATE)/kubeconfig
endif
endif

# Preload images into the cluster's containerd from the host's Docker.
# Only the local k3s-in-Docker cluster needs it (its containerd cannot pull
# through this container's egress proxy); auto-detected.
PRELOAD ?= $(if $(wildcard $(STATE)/kubeconfig),$(if $(filter $(STATE)/kubeconfig,$(KUBECONFIG)),1,),)

export CLUSTER K3S_IMAGE STATE PROFILE RULER_EVAL DOCKERHUB_ONLY MIMIR_PORT GRAFANA_PORT \
       MIMIR_CHART_VERSION KPS_CHART_VERSION ALLOY_CHART_VERSION CLI CLI_NAME

.PHONY: help tools cluster down deploy undeploy charts images object-store monitoring-crds \
        mimir monitoring tenants rules status forward promcost-config cli cost events events-json

help:
	@sed -n '2,17p' Makefile | sed 's/^# \{0,1\}//'

tools:
	./scripts/tools.sh

# ---------------------------------------------------------------- cluster bootstrap

cluster: tools
	./cluster/up.sh
	@echo "export KUBECONFIG=$(STATE)/kubeconfig"

down:
	./cluster/down.sh

# ---------------------------------------------------------------- deploy (any cluster)

# INTERIM_TENANTS=0 leaves out the dev/mimir-k8s tenants, for runs that
# use the validation lane's loadgen (workload/) instead.
INTERIM_TENANTS ?= 1

deploy: tools charts $(if $(PRELOAD),images,) object-store monitoring-crds mimir monitoring $(if $(filter 1,$(INTERIM_TENANTS)),tenants rules,) status

charts:
	./scripts/fetch-charts.sh

images: charts
	./cluster/load-images.sh

object-store:
	kubectl apply -f manifests/object-store.yaml
	kubectl -n object-store rollout status deploy/object-store --timeout=5m

# kube-prometheus-stack's CRDs first: the Mimir chart's ServiceMonitors and
# PrometheusRules need them to exist.
monitoring-crds:
	./scripts/monitoring-crds.sh

mimir:
	./scripts/deploy-mimir.sh

monitoring:
	./scripts/deploy-monitoring.sh

# Interim traffic until the validation lane's workload/ lands: the
# dev/mimir-k8s tenants and their rules, unchanged.
tenants:
	./interim-tenants/deploy.sh

rules:
	./interim-tenants/sync-rules.sh

undeploy:
	./scripts/undeploy.sh

# ---------------------------------------------------------------- use

status:
	./scripts/status.sh

# Port-forward the Mimir gateway (and Grafana) to localhost in the
# background, restarting if a pod moves. Idempotent.
forward:
	./scripts/forward.sh

promcost-config: forward
	./scripts/promcost-config.sh > $(STATE)/promcost.yaml
	@echo "wrote $(STATE)/promcost.yaml"

cli:
	./scripts/build-cli.sh $(CLI_REF)

# TENANTS defaults to scripts/tenants.sh: the running loadgen's tenants,
# else the interim ones, plus `monitoring`.
TENANTS ?=
SINCE   ?= 1h
cost: promcost-config
	./scripts/cost.sh "$(or $(TENANTS),$$(./scripts/tenants.sh))" "$(SINCE)"

# ---------------------------------------------------------------- operational events

events:
	@ls events/*.sh | grep -v lib.sh | sed 's|events/\(.*\)\.sh|make event-\1|'

event-%:
	./events/$*.sh $(ARGS)

# The event log in the validation harness's --events format.
events-json:
	./scripts/events-json.sh > $(STATE)/events.json
	@echo "wrote $(STATE)/events.json"
