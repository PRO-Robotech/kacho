.PHONY: dev-up dev-down reload-svc logs-svc psql preflight e2e-test helm-lint seed-ipam \
        loadtest-address-allocate loadtest-address-allocate-clean \
        reload-svc-iam psql-iam logs-iam fga-bootstrap build-ui build-services openfga-model-json \
        reload-svc-nlb psql-nlb logs-nlb seed-nlb e2e-newman

CLUSTER_NAME := kacho

# KAC-127: build + kind-load kacho-ui:dev.
# kacho-ui — standalone Vite+nginx multi-stage build (контекст — сам kacho-ui/,
# без COPY sibling-репо). В отличие от Go-сервисов, чьи `:dev`-образы собираются
# отдельным CI-флоу, kacho-ui раньше не билдился вовсе → ImagePullBackOff на
# `kacho-ui:dev`. Этот target закрывает blocker; вызывается из `dev-up`.
build-ui:
	@echo "=== build kacho-ui:dev ==="
	docker build -t kacho-ui:dev ../../kacho-ui
	kind load docker-image kacho-ui:dev --name $(CLUSTER_NAME)

# KAC-228: build + kind-load all backend service images. The dev deployments
# reference local `kacho-<svc>:dev` images (values.dev.yaml), which kind cannot
# pull from a registry → they MUST be built locally before helm install, else
# pods sit in ImagePullBackOff and `helm --wait` times out (which also skips the
# openfga-bootstrap RBAC, breaking fga-bootstrap). Single-repo builds: each
# Dockerfile is `COPY . .` + `go mod download` (versioned GitHub deps), so the
# build context is the service's OWN dir (kacho-$svc), NOT the parent — a
# workspace-root context would `COPY . .` a rootless tree → `go: no modules`.
SERVICES := iam vpc compute api-gateway nlb
build-services:
	@for svc in $(SERVICES); do \
	  echo "=== build kacho-$$svc:dev ==="; \
	  d=$$(test "$$svc" = api-gateway && echo gateway || echo services/$$svc); \
	  ( cd .. && docker build -f $$d/Dockerfile -t kacho-$$svc:dev . ) || exit 1; \
	  kind load docker-image kacho-$$svc:dev --name $(CLUSTER_NAME) || exit 1; \
	done

# KAC-127: CI docker-compose stack (ci-images / ci-up / ci-seed / ci-down /
# ci-logs) удалён — он был построен на упразднённом kacho-resource-manager
# (ci/docker-compose.yml + ci/seed.sh удалены). newman E2E гоняется против
# kind+helm dev-stand (`make dev-up`).

preflight:
	@command -v docker >/dev/null || { echo "ERROR: docker not installed"; exit 1; }
	@command -v kind >/dev/null || { echo "ERROR: kind not installed (install from https://kind.sigs.k8s.io/)"; exit 1; }
	@command -v kubectl >/dev/null || { echo "ERROR: kubectl not installed"; exit 1; }
	@command -v helm >/dev/null || { echo "ERROR: helm not installed"; exit 1; }
	@docker info >/dev/null 2>&1 || { echo "ERROR: docker daemon is not running"; exit 1; }
	@grep -q "api.kacho.local" /etc/hosts || echo "WARN: '127.0.0.1 api.kacho.local' missing in /etc/hosts — ingress will not resolve from host"
	@echo "preflight OK"

dev-up: preflight
	@start=$$(date +%s); \
	kind get clusters | grep -q "^$(CLUSTER_NAME)$$" || ./kind/create-cluster.sh; \
	kubectl config use-context kind-$(CLUSTER_NAME); \
	kubectl create namespace kacho --dry-run=client -o yaml | kubectl apply -f - >/dev/null; \
	kubectl label namespace kacho \
	  pod-security.kubernetes.io/warn=restricted pod-security.kubernetes.io/warn-version=latest \
	  pod-security.kubernetes.io/audit=restricted pod-security.kubernetes.io/audit-version=latest \
	  --overwrite >/dev/null; \
	$(MAKE) build-ui; \
	$(MAKE) build-services; \
	./scripts/gen-tls-cert.sh; \
	cd helm/umbrella && helm dep update >/dev/null && cd ../..; \
	echo "=== helm phase 1: cert-manager + control-plane (insecure) ==="; \
	helm upgrade --install kacho-umbrella ./helm/umbrella -n kacho --create-namespace \
	  -f ./helm/umbrella/values.dev.yaml \
	  --set mtls.enabled=false \
	  --set vpc.mtls.enable=false --set compute.mtls.enable=false \
	  --set api-gateway.mtls.enable=false --set kacho-nlb.mtls.enable=false \
	  --set kacho-iam.mtls.enable=false \
	  --wait --timeout 10m; \
	echo "=== waiting for cert-manager webhook (before applying Issuer/Certificate CRs) ==="; \
	kubectl -n kacho rollout status deploy/kacho-umbrella-cert-manager-webhook --timeout=180s; \
	echo "=== helm phase 2: enable cluster-internal mTLS (internal-CA + per-service certs) ==="; \
	helm upgrade kacho-umbrella ./helm/umbrella -n kacho \
	  -f ./helm/umbrella/values.dev.yaml \
	  --wait --timeout 10m; \
	echo "Waiting for ingress-nginx admission webhook..."; \
	kubectl -n kacho wait --for=condition=ready pod -l app.kubernetes.io/component=controller --timeout=60s; \
	echo "=== fga-bootstrap (KAC-228: provision OpenFGA store + model + cluster viewer:* seed) ==="; \
	echo "    NB: must run before any user signs up — else account/project FGA tuples"; \
	echo "    are written best-effort against a missing store and lost (→ 503 on List)."; \
	$(MAKE) fga-bootstrap; \
	kubectl -n kacho rollout status deploy/kacho-iam --timeout=180s || true; \
	kubectl -n kacho rollout status deploy/api-gateway --timeout=180s || true; \
	end=$$(date +%s); \
	echo "dev-up complete in $$((end-start))s"; \
	echo; \
	echo "API endpoint:"; \
	echo "  REST   http://api.kacho.local         (add '127.0.0.1 api.kacho.local' to /etc/hosts if missing)"; \
	echo "  TLS    https://api.kacho.local        (cert = /tmp/kacho-tls/tls.crt)"; \
	echo "  Local  https://localhost:18443        (через kubectl port-forward svc/api-gateway 18443:8443)"

dev-down:
	kind delete cluster --name $(CLUSTER_NAME) || true

helm-lint:
	cd helm/umbrella && helm dep update >/dev/null && helm lint -f values.dev.yaml

# Offline helm manifest-assertion suite (tests/helm/). Asserts kept-stack wiring
# against the rendered umbrella — e.g. hydra-jwks-url-test.sh checks that
# api-gateway points at the in-cluster Hydra JWKS endpoint. No kind cluster needed.
helm-manifest-test:
	@for t in tests/helm/*-test.sh; do \
		echo "=== $$t ==="; \
		bash "$$t" || exit 1; \
	done
	@echo "helm-manifest-test: all green"

reload-svc:
ifndef SVC
	$(error SVC variable is required, e.g. make reload-svc SVC=compute)
endif
	@if [ "$(SVC)" != "vpc" ] && [ "$(SVC)" != "compute" ] && [ "$(SVC)" != "loadbalancer" ] && [ "$(SVC)" != "api-gateway" ] && [ "$(SVC)" != "iam" ] && [ "$(SVC)" != "ui" ] && [ "$(SVC)" != "nlb" ]; then \
		echo "ERROR: unknown service '$(SVC)'"; exit 1; \
	fi; \
	DEPLOY_NAME=$(SVC); \
	if [ "$(SVC)" = "iam" ]; then DEPLOY_NAME=kacho-iam; fi; \
	if [ "$(SVC)" = "ui" ]; then DEPLOY_NAME=ui; fi; \
	if [ "$(SVC)" = "nlb" ]; then DEPLOY_NAME=kacho-nlb; fi; \
	if ! kubectl -n kacho get deploy $$DEPLOY_NAME >/dev/null 2>&1; then \
		echo "WARN: service '$(SVC)' (deployment '$$DEPLOY_NAME') is not deployed yet (planned for sub-phase 0.X — see roadmap)"; \
		exit 0; \
	fi; \
	if [ "$(SVC)" = "ui" ]; then \
		docker build -t kacho-ui:dev ../../kacho-ui; \
	else \
		d=$$(test "$(SVC)" = api-gateway && echo gateway || echo services/$(SVC)); \
		cd .. && docker build -f $$d/Dockerfile -t kacho-$(SVC):dev .; \
	fi && \
	kind load docker-image kacho-$(SVC):dev --name $(CLUSTER_NAME) && \
	kubectl rollout restart -n kacho deployment/$$DEPLOY_NAME

logs-svc:
ifndef SVC
	$(error SVC variable is required)
endif
	kubectl logs -n kacho -f deploy/$(SVC)

psql:
ifndef SVC
	$(error SVC variable is required)
endif
	kubectl exec -it -n kacho statefulset/pg-$(SVC) -- psql -U $(SVC) -d kacho_$(SVC)

seed-ipam:
	@echo "seed-ipam: NOOP. Auto-seeding отключён — admin должен явно создать AddressPool через kachoctl-ipam."
	@echo
	@echo "  cd ../services/vpc && make build-ipam"
	@echo "  kubectl -n kacho port-forward svc/vpc 19091:9091 &"
	@echo "  ./bin/kachoctl-ipam -addr localhost:19091 pool create \\"
	@echo "    --folder <real_folder_id_from_resource_manager> \\"
	@echo "    --kind EXTERNAL_PUBLIC \\"
	@echo "    --region-id ru-central1 \\"
	@echo "    --cidr 198.51.100.0/24 \\"
	@echo "    --is-default \\"
	@echo "    --name production-pool"

e2e-test:
	@for sh in e2e/0.1/*.sh; do \
		echo "=== $$sh ==="; \
		bash "$$sh" || exit 1; \
	done

# e2e-newman — REPRODUCIBLE newman e2e: port-forward + seed authz fixtures
# (non-expiring dev JWTs, users, projects, cluster-admin, patched newman env) +
# run a service's newman suite against the running dev stand. Not a manual
# side-step — one command, deterministic. Requires: dev-up complete; kubectl,
# python3, newman, grpcurl in PATH.
#   make e2e-newman SVC=vpc
#   make e2e-newman SVC=vpc COLLECTION=internal-network
e2e-newman:
	@bash ./scripts/newman-e2e.sh "$(SVC)" "$(COLLECTION)"

# ─── Load testing ────────────────────────────────────────────────

loadtest-address-allocate:
	@kubectl -n kacho delete job k6-address-allocate --ignore-not-found
	@kubectl -n kacho apply -f load-tests/k6-address-allocate.yaml
	@echo "→ Job created, waiting for completion (max 600s)…"
	@kubectl -n kacho wait --for=condition=complete job/k6-address-allocate --timeout=600s || \
	  kubectl -n kacho wait --for=condition=failed job/k6-address-allocate --timeout=10s
	@kubectl -n kacho logs -l job-name=k6-address-allocate --tail=-1

loadtest-address-allocate-clean:
	@kubectl -n kacho delete job k6-address-allocate --ignore-not-found
	@kubectl -n kacho delete cm k6-address-allocate --ignore-not-found

# ─── KAC-105: IAM stack (kacho-iam + OpenFGA; KAC-127: Zitadel → Ory) ──────────────

# alias для reload-svc SVC=iam — пересобрать и перезагрузить kacho-iam.
reload-svc-iam:
	@$(MAKE) reload-svc SVC=iam

# psql в kacho_iam-БД (база, пользователь, схема — все kacho_iam).
psql-iam:
	kubectl exec -it -n kacho statefulset/kacho-umbrella-pg-iam -- psql -U iam -d kacho_iam

# KAC-125: greenfield wipe kacho_iam schema + drop goose state.
# Используется ДО helm upgrade с breaking migration (e.g. 0009_user_per_account_invite_kac125).
# Воспроизводимый replacement для manual `DROP SCHEMA … CASCADE`.
# ВНИМАНИЕ: уничтожает все данные IAM (accounts/projects/users/...). Только для dev.
wipe-iam-db:
	@echo "⚠️  WIPE IAM DB on $$KUBECONFIG"
	@kubectl exec -n kacho kacho-umbrella-pg-iam-0 -- \
	    env PGPASSWORD=dev-iam-password psql -U iam -d kacho_iam \
	    -c "DROP SCHEMA IF EXISTS kacho_iam CASCADE; CREATE SCHEMA kacho_iam;"
	@kubectl exec -n kacho kacho-umbrella-pg-iam-0 -- \
	    env PGPASSWORD=dev-iam-password psql -U iam -d kacho_iam \
	    -c "DROP TABLE IF EXISTS public.goose_db_version;" || true
	@echo "✓ IAM schema wiped; goose state dropped. Next: kubectl rollout restart deploy/kacho-iam"

# Логи kacho-iam deployment.
logs-iam:
	kubectl logs -n kacho -f deploy/kacho-iam

# KAC-127: zitadel-admin target удалён — Zitadel заменён на Ory Kratos + Hydra.

# Вручную запустить openfga-bootstrap-job (для дебага / повторного применения model).
# Helm hook удаляет старый Job при post-install/upgrade, поэтому здесь используем
# `helm template` для рендера манифеста + apply.
fga-bootstrap:
	@echo "Запускаем openfga-bootstrap Job (idempotent):"
	@kubectl -n kacho delete job openfga-bootstrap --ignore-not-found
	@helm template kacho-umbrella ./helm/umbrella -n kacho -f ./helm/umbrella/values.dev.yaml --show-only templates/openfga-bootstrap-job.yaml | kubectl -n kacho apply -f -
	@echo "Ждём завершения…"
	@kubectl -n kacho wait --for=condition=complete job/openfga-bootstrap --timeout=300s || \
	 kubectl -n kacho wait --for=condition=failed job/openfga-bootstrap --timeout=10s
	@kubectl -n kacho logs -l job-name=openfga-bootstrap --tail=-1

# KAC-127 (deploy#38): regenerate the pre-transformed `model.json` ConfigMap key
# from the `model.fga` DSL. Run after any edit of the DSL block in
# helm/umbrella/templates/openfga-model-stub-configmap.yaml. The openfga/cli
# image is distroless (no shell), so the transform is done here at commit-time
# instead of in a runtime init-container — see openfga-bootstrap-job.yaml.
OPENFGA_CLI_IMAGE ?= openfga/cli:v0.7.13
# KAC-127 RC-2b: canonical FGA model source. The ConfigMap below is GENERATED —
# both the `model.fga` block (byte-identical copy) and the `model.json` block
# (openfga/cli transform) are derived from this single artifact.
OPENFGA_CANONICAL_FGA ?= ../proto/kacho/cloud/iam/v1/fga_model.fga
openfga-model-json:
	@echo "Regenerating openfga-model-stub configmap from canonical fga_model.fga..."
	python3 scripts/gen-openfga-model-configmap.py \
	  helm/umbrella/charts/openfga-bootstrap/templates/openfga-model-stub-configmap.yaml \
	  $(OPENFGA_CANONICAL_FGA) $(OPENFGA_CLI_IMAGE)

# ─── KAC-127 Phase 10 — SPIRE / Cilium mesh / cosign ─────────────────────
# Targets для bootstrap, dry-run проверки, и emergency operations.

# Bootstrap SPIRE Server + Agent + регистрация всех Kachō SPIFFE-IDs.
# Применяется только когда umbrella values имеет spire-server.enabled=true.
spire-bootstrap:
	@echo "Phase 10: bootstrapping SPIRE Server + Agent..."
	@kubectl create namespace spire-system --dry-run=client -o yaml | kubectl apply -f -
	@helm template kacho-umbrella ./helm/umbrella -f ./helm/umbrella/values.prod.yaml \
		--set spire-server.enabled=true \
		--set spire-agent.enabled=true \
		--show-only charts/spire-server/templates/* \
		--show-only charts/spire-agent/templates/* | \
		kubectl apply -n spire-system -f -
	@echo "Waiting for SPIRE Server replicas to be ready..."
	@kubectl -n spire-system rollout status statefulset/kacho-umbrella-spire-server --timeout=600s
	@kubectl -n spire-system rollout status daemonset/kacho-umbrella-spire-agent --timeout=600s
	@echo "✓ SPIRE bootstrap complete. Registration entries applied via post-install Job."

# Rotate trust domain — emergency runbook operation (P10-D19).
# Requires confirmation; invalidates entire trust domain key material.
spire-rotate-trust-domain:
	@echo "DANGER: rotating SPIRE trust domain — all SVIDs will be invalidated."
	@read -p "Type 'ROTATE' to confirm: " confirm && \
		[ "$$confirm" = "ROTATE" ] || (echo "Aborted." && exit 1)
	@kubectl -n spire-system delete pod -l app.kubernetes.io/name=spire-server
	@echo "✓ SPIRE Server pods restarting; new root CA will be issued."

# Dry-run Cilium policies — render и валидация без apply.
cilium-policy-dry-run:
	@echo "Phase 10: rendering CiliumNetworkPolicy manifests..."
	@helm template kacho-umbrella ./helm/umbrella -f ./helm/umbrella/values.prod.yaml \
		--set cilium.enabled=true \
		--show-only charts/cilium/templates/network-policies.yaml \
		--show-only charts/cilium/templates/cilium-mtls-enforce.yaml > /tmp/cilium-policies.yaml
	@echo "✓ Rendered to /tmp/cilium-policies.yaml ($$(wc -l < /tmp/cilium-policies.yaml) lines)"
	@command -v kubeconform > /dev/null && \
		kubeconform -skip CiliumNetworkPolicy,CiliumClusterwideNetworkPolicy /tmp/cilium-policies.yaml \
		|| echo "(kubeconform not installed; install via: brew install kubeconform)"

# Lint все Phase 10 charts.
spire-lint:
	helm lint ./helm/umbrella/charts/spire-server
	helm lint ./helm/umbrella/charts/spire-agent
	helm lint ./helm/umbrella/charts/cilium
	helm lint ./helm/umbrella/charts/cosign-policy-controller
	helm lint ./helm/umbrella/charts/spiffe-csi-driver
	@echo "✓ All Phase 10 charts lint clean"

# ─── KAC-NLB: kacho-nlb (L4 NLB control-plane) ─────────────────────────
#
# Sub-chart at project/kacho-nlb/deploy/ — wired into umbrella via
# `file://../../../kacho-nlb/deploy` (helm/umbrella/Chart.yaml). Deployment
# name `kacho-nlb`, Postgres StatefulSet `kacho-umbrella-pg-nlb`.

# alias для reload-svc SVC=nlb — пересобрать и перезагрузить kacho-nlb.
reload-svc-nlb:
	@$(MAKE) reload-svc SVC=nlb

# psql прямо в kacho_nlb-БД. Bitnami pg-nlb StatefulSet exposes
# `kacho-umbrella-pg-nlb` Service; user / db = `kacho_nlb` (see
# helm/umbrella/values.dev.yaml pg-nlb.auth).
psql-nlb:
	kubectl exec -it -n kacho statefulset/kacho-umbrella-pg-nlb -- \
	    env PGPASSWORD=dev-nlb-password psql -U kacho_nlb -d kacho_nlb

# Логи kacho-nlb deployment (main container — no migrator init-container
# logs unless the pod is failing; `kubectl logs … -c migrate` for that).
logs-nlb:
	kubectl logs -n kacho -f deploy/kacho-nlb

# Seed kacho-nlb test fixtures (subnet/instance/nic/external-address) into
# the running dev stack. Idempotent — re-runs reuse existing resources by
# name. Output: writes .seeded-ids.env at repo root for downstream newman.
#
# Requires: dev-up complete, api-gateway reachable at $$BASE_URL (default
# http://localhost:28080 for kind port-forward; CI uses
# http://api.kacho.local:28080).
seed-nlb:
	@BASE_URL="$${BASE_URL:-http://localhost:28080}" \
	 bash ./scripts/seed-nlb-fixtures.sh
