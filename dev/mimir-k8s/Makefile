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
MIMIR_URL      := http://localhost:8090
PROMCOST       ?= ../../bin/promcost

MIMIR_VALUES := -f values/mimir.yaml
ifeq ($(ARCH),ingest)
MIMIR_VALUES += -f values/ingest-storage.yaml
endif

.PHONY: up down cluster mimir alloy grafana tenants rules mixin-rules status cost scale rollout-ingesters scenario-heavy-rule scenario-heavy-rule-off

up: cluster mimir alloy grafana tenants mixin-rules rules status

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

# --- scenarios: each changes one thing whose effect on the report is predictable

# make scale TARGET=avalanche-analytics-bi REPLICAS=2 — every replica adds its own
# copy of the series (Alloy labels each with its pod), so replicas multiply them.
scale:
	kubectl -n tenants scale deploy/$(TARGET) --replicas=$(REPLICAS)

rollout-ingesters:
	kubectl -n mimir rollout restart statefulset/mimir-ingester
	kubectl -n mimir rollout status statefulset/mimir-ingester --timeout=15m

scenario-heavy-rule:
	./scripts/sync-rules.sh analytics rules/scenarios/analytics-heavy.yaml

scenario-heavy-rule-off:
	./scripts/sync-rules.sh analytics

down:
	k3d cluster delete $(CLUSTER)
