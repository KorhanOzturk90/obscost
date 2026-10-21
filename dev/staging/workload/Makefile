# loadgen: the validation workload. See README.md.
#
#   make build                      # .run/loadgen
#   make local-up local-run         # dev/mimir-local (+ local/compose.override.yml), then the workload on the host
#   make image k8s-import k8s-deploy  # the staging cluster (dev/staging: KUBECONFIG=../.state/kubeconfig)

PROFILE     ?= standard
# Where the host-side runner reaches Mimir (dev/mimir-local).
GATEWAY     ?= http://localhost:8080
# Where the in-cluster runner reaches it: dev/staging's gateway (scripts/lib.sh MIMIR_GATEWAY).
K8S_GATEWAY ?= http://mimir-gateway.mimir.svc
KUBECTL     ?= kubectl
# dev/staging runs k3s as a plain container named <cluster>-k3s (cluster/lib.sh NODE).
K3S_CONTAINER ?= staging-k3s
IMAGE       ?= promcost-loadgen:dev
RUN         := .run

.PHONY: build test plan rules image k8s-import k8s-deploy k8s-truth local-up local-run local-stop local-down

build:
	go build -o $(RUN)/loadgen ./cmd/loadgen

test:
	go test ./...

plan: build
	$(RUN)/loadgen plan --profile profiles/$(PROFILE).yaml

rules: build
	$(RUN)/loadgen rules --profile profiles/$(PROFILE).yaml --out $(RUN)/rules

# Docker Hub rate-limits direct pulls here; mirror.gcr.io serves the same images.
image:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags=-s -o $(RUN)/loadgen-linux-amd64 ./cmd/loadgen
	@docker image inspect alpine:3.22 >/dev/null 2>&1 || \
	  { docker pull mirror.gcr.io/library/alpine:3.22 && docker tag mirror.gcr.io/library/alpine:3.22 alpine:3.22; }
	docker build -t $(IMAGE) .

k8s-import:
	docker save $(IMAGE) | docker exec -i $(K3S_CONTAINER) ctr -n k8s.io images import -

k8s-deploy:
	sed -e 's|GATEWAY_PLACEHOLDER|$(K8S_GATEWAY)|' -e 's|PROFILE_PLACEHOLDER|$(PROFILE)|' k8s/loadgen.yaml | $(KUBECTL) apply -f -
	$(KUBECTL) -n loadgen rollout status deploy/loadgen --timeout=120s

# Snapshot the in-cluster ground truth to a file the harness can read.
k8s-truth:
	@mkdir -p $(RUN)
	$(KUBECTL) -n loadgen exec deploy/loadgen -- wget -qO- localhost:9100/ground-truth > $(RUN)/ground-truth-k8s.json
	@echo "$(RUN)/ground-truth-k8s.json"

local-up:
	./local/up.sh

local-run: build
	cd $(RUN) && nohup ./loadgen run --profile ../profiles/$(PROFILE).yaml --gateway $(GATEWAY) \
	  --state ground-truth.json --listen 127.0.0.1:9100 >> loadgen.log 2>&1 &
	@echo "loadgen running; log: $(RUN)/loadgen.log, ground truth: $(RUN)/ground-truth.json"

local-stop:
	-pkill -f 'loadgen run --profile ../profiles/'

local-down: local-stop
	./local/up.sh down
