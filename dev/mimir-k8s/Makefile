# Microservices Mimir on k3d, with simulated tenants. See README.md.
#
#   make up                 # classic architecture (distributors -> ingesters, RF=3)
#   make up ARCH=ingest     # Mimir 3.x default: ingest storage via Kafka
#   make status             # pods, and per-tenant active series as Mimir sees them
#   make cost               # run promcost cost against the rig
#   make down

CLUSTER        := obscost
MIMIR_CHART    := grafana/mimir-distributed
MIMIR_VERSION  := 6.2.0
ALLOY_VERSION  := 1.12.1
ARCH           ?= classic
# CPU cores the k3d node may use. Docker Desktop's own limit is usually
# every core on the machine, and an unbounded Mimir + Kafka + generators
# will take them all and make the laptop lag. `docker update` caps just
# this container, whatever Docker Desktop is set to.
CPUS           ?= 4
GRAFANA_VERSION := 10.5.15
OPENCOST_VERSION := 2.5.31
MIMIR_URL      := http://localhost:8090
PROMCOST       ?= ../../bin/promcost

MIMIR_VALUES := -f values/mimir.yaml
ifeq ($(ARCH),ingest)
MIMIR_VALUES += -f values/ingest-storage.yaml
endif

.PHONY: up down cluster mimir alloy grafana tenants rules mixin-rules status cost podcost calibrate-series calibrate-query opencost scale rollout-ingesters scenario-expensive-rule scenario-expensive-rule-off

# `make up MIXIN_RULES=1` also loads the mimir-mixin's ~120 recording rules
# into the monitoring tenant. They power several mixin dashboard panels but
# evaluate every minute through the query path, which is a noticeable share
# of a laptop rig's CPU — off by default.
MIXIN_RULES ?=

up: cluster mimir alloy grafana tenants $(if $(MIXIN_RULES),mixin-rules,) rules status

cluster:
	@k3d cluster list $(CLUSTER) >/dev/null 2>&1 || k3d cluster create --config k3d.yaml
	@kubectl config use-context k3d-$(CLUSTER) >/dev/null
	@docker update --cpus $(CPUS) k3d-$(CLUSTER)-server-0 >/dev/null && echo "k3d node capped at $(CPUS) CPUs"

mimir:
	helm repo add grafana https://grafana.github.io/helm-charts >/dev/null 2>&1 || true
	helm upgrade --install mimir $(MIMIR_CHART) --version $(MIMIR_VERSION) \
	  --namespace mimir --create-namespace $(MIMIR_VALUES) --wait --timeout 15m

alloy:
	helm upgrade --install alloy grafana/alloy --version $(ALLOY_VERSION) \
	  --namespace mimir -f alloy/values.yaml \
	  --set-file alloy.configMap.content=alloy/config.alloy --wait
	kubectl apply -f alloy/rbac.yaml

grafana:
	helm upgrade --install grafana grafana/grafana --version $(GRAFANA_VERSION) \
	  --namespace mimir -f grafana/values.yaml --wait
	kubectl apply -f grafana/obscost-dashboard.yaml
	@echo "Grafana: http://localhost:3090"

# The mixin's recording rules, which several mixin dashboards read, taken
# from the same chart version so they match the deployed Mimir. Written to
# rules/monitoring/ (gitignored) and synced with every other tenant's.
mixin-rules:
	@rm -rf .cache/chart && mkdir -p .cache/chart
	@helm pull $(MIMIR_CHART) --version $(MIMIR_VERSION) --untar -d .cache/chart
	@{ echo "namespace: mimir-mixin"; cat .cache/chart/mimir-distributed/mixins/rules.yaml; } > rules/monitoring/mixin-rules.yaml
	./scripts/sync-rules.sh monitoring

tenants:
	kubectl apply -f tenants/workloads.yaml
	kubectl -n tenants rollout status deploy --timeout=180s

rules:
	./scripts/sync-rules.sh

status:
	@kubectl -n mimir get pods -o wide | awk '{print $$1, $$2, $$3, $$4}' | column -t
	@echo
	@echo "active series per tenant (Mimir's view, before promcost):"
	@./scripts/tenant-series.py $(MIMIR_URL)

cost:
	$(PROMCOST) cost --metrics-tenant monitoring --config promcost.yaml \
	  --tenant analytics,infra,payments,platform,monitoring --since $(or $(SINCE),1h)

# OpenCost, as a cross-check on podcost.py's arithmetic (ADR 0006). Not part
# of `make up`: one more pod, and only useful when comparing.
opencost:
	helm repo add opencost https://opencost.github.io/opencost-helm-chart >/dev/null 2>&1 || true
	kubectl apply -f opencost/proxy.yaml
	kubectl -n mimir rollout status deploy/mimir-monitoring-proxy --timeout=120s
	helm upgrade --install opencost opencost/opencost --version $(OPENCOST_VERSION) \
	  --namespace mimir -f opencost/values.yaml --wait
	$(MAKE) alloy
	@echo "OpenCost installed; compare with: ./scripts/compare-opencost.py"

# What the components cost, derived from node prices the way an operator on
# a shared cluster has to derive them (ADR 0006 decision 3), with idle
# capacity on its own line.
podcost:
	./scripts/podcost.py --window $(or $(WINDOW),1h)

# Calibration sweeps: coefficients, not yes/no answers. ~45m and ~30m.
calibrate-series:
	./scripts/calibrate.py series

calibrate-query:
	./scripts/calibrate.py query

# --- scenarios: each changes one thing whose effect on the report is predictable

# make scale TARGET=avalanche-analytics-bi REPLICAS=2 — every replica adds its own
# copy of the series (Alloy labels each with its pod), so replicas multiply them.
scale:
	kubectl -n tenants scale deploy/$(TARGET) --replicas=$(REPLICAS)

rollout-ingesters:
	kubectl -n mimir rollout restart statefulset/mimir-ingester
	kubectl -n mimir rollout status statefulset/mimir-ingester --timeout=15m

scenario-expensive-rule:
	./scripts/sync-rules.sh analytics rules/scenarios/analytics-expensive.yaml

scenario-expensive-rule-off:
	./scripts/sync-rules.sh analytics

down:
	k3d cluster delete $(CLUSTER)
