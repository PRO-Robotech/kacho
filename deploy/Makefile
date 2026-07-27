# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
.PHONY: dev-up dev-down dev-prod-up assert-rollout-ready assert-jobs-healthy assert-production-posture \
        assert-outbox-autovacuum \
        reload-svc logs-svc psql preflight e2e-test helm-lint seed-ipam \
        loadtest-address-allocate loadtest-address-allocate-clean \
        reload-svc-iam psql-iam logs-iam fga-bootstrap build-services openfga-model-json \
        reload-svc-nlb psql-nlb logs-nlb seed-nlb e2e-newman

CLUSTER_NAME := kacho

# KAC-228: build + kind-load all backend service images. The dev deployments
# reference local `kacho-<svc>:dev` images (values.dev.yaml), which kind cannot
# pull from a registry → they MUST be built locally before helm install, else
# pods sit in ImagePullBackOff and `helm --wait` times out (which also skips the
# openfga-bootstrap RBAC, breaking fga-bootstrap). Single-repo builds: each
# Dockerfile is `COPY . .` + `go mod download` (versioned GitHub deps), so the
# build context is the service's OWN dir (kacho-$svc), NOT the parent — a
# workspace-root context would `COPY . .` a rootless tree → `go: no modules`.
SERVICES := iam vpc compute api-gateway nlb geo storage registry
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

# guard-kind-context — АВАРИЙНЫЙ СТОП, если активный контекст не kind-<CLUSTER_NAME>.
#
# Зачем (реальный near-miss 2026-07-15): kind-кластер `kacho` существовал, а его контекст
# в kubeconfig отсутствовал (KUBECONFIG был подменён на прод-кластер). `dev-up` шёл
# `; \`-цепочкой, поэтому упавший `kubectl config use-context kind-kacho` НЕ прерывал
# цель — и весь стенд (namespace, labels, helm upgrade --install kacho-umbrella) поехал
# на ТЕКУЩИЙ контекст, то есть на ПРОД. Прод спасла лишь случайность: helm отверг
# апгрейд из-за pre-created секретов (ownership check) и не создал ревизию.
#
# Дешёвая проверка, которая делает такую ошибку невозможной по построению.
guard-kind-context:
	@ctx="$$(kubectl config current-context 2>/dev/null)"; \
	case "$$ctx" in \
	  kind-$(CLUSTER_NAME)) ;; \
	  *) echo "ABORT: активный kube-контекст '$$ctx' — НЕ kind-$(CLUSTER_NAME)."; \
	     echo "       dev-стенд создаёт/меняет ресурсы и МОГ БЫ поехать на чужой (в т.ч. прод) кластер."; \
	     echo "       Переключись явно:  kubectl config use-context kind-$(CLUSTER_NAME)"; \
	     exit 1 ;; \
	esac

# MTLS_OFF — глушим mTLS у КАЖДОГО сабчарта на фазе 1 (bootstrap чистого кластера).
#
# Зачем: cert-manager — сабчарт этого же umbrella, поэтому его CRD появляются только
# ПОСЛЕ фазы 1. Любой сабчарт, рендерящий Certificate раньше, валит весь релиз:
#   no matches for kind "Certificate" in version "cert-manager.io/v1";
#   ensure CRDs are installed first
# Фаза 2 включает mTLS обратно — CRD к тому моменту уже есть.
#
# Список ОБЯЗАН покрывать все сабчарты, у которых values.dev.yaml включает mtls.enable.
# Прежде здесь не хватало kacho-geo (добавлен позже, а --set-список не обновили) →
# bootstrap с нуля был сломан. Не всплывало годами: kind-кластер переиспользовался, CRD
# в нём уже стояли, и фаза 1 «проходила». Проверяется целью `check-mtls-off-complete`.
MTLS_SUBCHARTS := vpc compute api-gateway kacho-nlb kacho-iam kacho-geo storage registry
# registry AND geo are fail-closed BY DESIGN: their control-plane refuses to start
# without server-mTLS on both listeners UNLESS AUTHZ_BREAKGLASS=true (there is no
# auth.mode=dev relaxation like compute/storage — control-plane w/o authz+mTLS is
# forbidden). Phase-1 forces mtls off (Certificate-before-CRD guard), so each would
# CrashLoop ("mTLS required on both listeners"). Run them breakglass ONLY in phase-1;
# phase-2 (no override) restores breakglass=false + mtls on → real authz.
# NB: geo's breakglass value key is FLAT `kacho-geo.authzBreakglass` (not nested
# `authz.breakglass` like registry) — the two charts differ. Omitting geo here is the
# exact class of bug the check-mtls-off-complete gate guards for mtls.enable: geo was
# added to MTLS_SUBCHARTS (mtls off) but its breakglass override was forgotten →
# fresh-cluster bootstrap hangs phase-1 --wait on geo (regression 2026-07-21).
MTLS_OFF := --set mtls.enabled=false $(foreach s,$(MTLS_SUBCHARTS),--set $(s).mtls.enable=false) \
            --set registry.authz.breakglass=true \
            --set kacho-geo.authzBreakglass=true

# check-mtls-off-complete — CI-гейт: MTLS_SUBCHARTS не отстаёт от values.dev.yaml.
# Новый сервис с mtls.enable=true, забытый в MTLS_OFF, снова сломает bootstrap с нуля —
# и снова незаметно, если кластер переиспользуют. Пусть падает здесь.
check-mtls-off-complete:
	@python3 -c "import yaml,sys; \
	d=yaml.safe_load(open('helm/umbrella/values.dev.yaml')); \
	want={k for k,v in d.items() if isinstance(v,dict) and isinstance(v.get('mtls'),dict) and v['mtls'].get('enable')}; \
	have=set('$(MTLS_SUBCHARTS)'.split()); \
	miss=want-have; \
	sys.exit('ERROR: mtls включён в values.dev.yaml, но НЕ заглушен в MTLS_SUBCHARTS: '+', '.join(sorted(miss))+chr(10)+'       Фаза 1 (до CRD cert-manager) упадёт на Certificate.' ) if miss else print('mtls-off complete:', ' '.join(sorted(have)))"

# `set -e` — ОБЯЗАТЕЛЕН, а не косметика. Рецепт — одна `; \`-цепочка, т.е. упавшая
# команда НЕ прерывает цель: следующая просто выполняется. Реально наблюдалось
# 2026-07-16 в CI — `helm phase 2` упал с UPGRADE FAILED, а цель дошла до конца и
# напечатала «dev-up complete», отдав exit 0. Стенд был собран наполовину (mTLS-фаза не
# применена, fga-bootstrap без SA), а CI считал его поднятым и шёл гонять newman —
# краснота вылезла позже и совсем в другом месте.
# Тот же класс, что near-miss 2026-07-15 (см. guard-kind-context ниже): упавший
# `use-context` не прервал цепочку, и стенд поехал на ПРОД.
dev-up: preflight
	@set -e; start=$$(date +%s); \
	kind get clusters | grep -q "^$(CLUSTER_NAME)$$" || ./kind/create-cluster.sh; \
	kubectl config use-context kind-$(CLUSTER_NAME) || { \
	  echo "ERROR: нет kubectl-контекста kind-$(CLUSTER_NAME)."; \
	  echo "       Кластер kind может существовать, а контекст в kubeconfig — отсутствовать"; \
	  echo "       (напр. KUBECONFIG подменён на другой кластер). Восстанови:"; \
	  echo "         kind export kubeconfig --name $(CLUSTER_NAME)"; \
	  exit 1; \
	}; \
	$(MAKE) --no-print-directory guard-kind-context; \
	kubectl create namespace kacho --dry-run=client -o yaml | kubectl apply -f - >/dev/null; \
	kubectl label namespace kacho \
	  pod-security.kubernetes.io/warn=restricted pod-security.kubernetes.io/warn-version=latest \
	  pod-security.kubernetes.io/audit=restricted pod-security.kubernetes.io/audit-version=latest \
	  --overwrite >/dev/null; \
	$(MAKE) build-services; \
	./scripts/gen-tls-cert.sh; \
	cd helm/umbrella && helm dep update >/dev/null && cd ../..; \
	echo "=== helm phase 1: cert-manager + control-plane (insecure) ==="; \
	helm upgrade --install kacho-umbrella ./helm/umbrella -n kacho --create-namespace \
	  -f ./helm/umbrella/values.dev.yaml \
	  $(MTLS_OFF) \
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

# ─────────────────────────────────────────────────────────────────────────────
# dev-prod-up — стенд в PRODUCTION-security-posture + СТРОГАЯ проверка, что он
# действительно поднялся.
#
# ЗАЧЕМ ОТДЕЛЬНАЯ ЦЕЛЬ. `helm lint`/`helm template` НЕ являются доказательством
# работоспособности production-профиля: production boot-guard'ы (Config.Validate →
# refuse-to-start при insecure config) срабатывают в РАНТАЙМЕ, а шаблон при этом
# рендерится идеально. Реальный инцидент: chart api-gateway рендерился зелёным, но
# не нёс knob'а для mTLS+SPIFFE на internal :9091 → под уходил в crash-loop ровно на
# values.prod. Единственный способ это поймать — поднять и дождаться rollout.
#
# ЧТО ЭТО ДОКАЗЫВАЕТ: каждый сервис СТАРТУЕТ с production-конфигом (authMode=
# production/-strict, mTLS везде, sslmode=require, breakglass off) и доходит до
# Ready. То есть чарты несут все knob'ы, которых требуют их boot-guard'ы.
# ЧЕГО НЕ ДОКАЗЫВАЕТ: функциональную корректность authz/токенов — это e2e-newman.
#
# ПОЧЕМУ ПОВЕРХ dev-up, а не отдельный fresh-install: bootstrap чистого кластера
# двухфазный (cert-manager CRD появляются только после фазы 1, см. MTLS_OFF), плюс
# fga-bootstrap. Дублировать этот рецепт значит завести вторую копию, которая
# разъедется с оригиналом. Здесь мы переиспользуем проверенный bring-up и затем
# ПЕРЕВОДИМ стенд в production-posture — каждый под перезапускается с production-
# конфигом и обязан снова стать Ready. Именно это и есть boot-guard-тест.
dev-prod-up: dev-up
	@set -e; \
	echo "=== helm phase 3: PRODUCTION posture (values.dev-prod.yaml поверх dev) ==="; \
	helm upgrade kacho-umbrella ./helm/umbrella -n kacho \
	  -f ./helm/umbrella/values.dev.yaml \
	  -f ./helm/umbrella/values.dev-prod.yaml \
	  --wait --timeout 12m; \
	$(MAKE) --no-print-directory assert-rollout-ready; \
	$(MAKE) --no-print-directory assert-production-posture; \
	$(MAKE) --no-print-directory assert-outbox-autovacuum; \
	echo "production-posture stand is UP and READY"

# assert-rollout-ready — КАЖДЫЙ workload в ns kacho обязан быть rollout-complete.
#
# Без `|| true`, в отличие от финальных проверок dev-up: там это удобство (стенд для
# человека), здесь — гейт. Недобранный rollout ОБЯЗАН валить цель, иначе «зелёный»
# прогон означал бы ровно то, что мы и ловим — сервис не поднялся в production.
# На провале печатаем диагностику (pods + describe + логи не-Ready подов), чтобы
# красный CI был actionable, а не «timed out».
assert-rollout-ready:
	@set -e; \
	fail=0; \
	for kind in deployment statefulset; do \
	  for w in $$(kubectl -n kacho get $$kind -o name 2>/dev/null); do \
	    echo "--- rollout: $$w"; \
	    kubectl -n kacho rollout status "$$w" --timeout=300s || fail=1; \
	  done; \
	done; \
	if [ "$$fail" -ne 0 ]; then \
	  echo; echo "!!! ROLLOUT INCOMPLETE — production posture did NOT come up"; \
	  echo "=== pods ==="; kubectl -n kacho get pods -o wide; \
	  echo "=== not-ready pods: describe + logs ==="; \
	  for p in $$(kubectl -n kacho get pods -o json \
	      | jq -r '.items[] | select(((.status.containerStatuses // []) | length) == 0 or ((.status.containerStatuses // []) | map(.ready) | all | not)) | .metadata.name'); do \
	    echo "--- describe $$p"; kubectl -n kacho describe pod "$$p" | tail -40; \
	    echo "--- logs $$p (все контейнеры, last 60)"; \
	    kubectl -n kacho logs "$$p" --all-containers --tail=60 --prefix 2>&1 | tail -80 || true; \
	    echo "--- logs $$p (previous — boot-guard Fatalf виден именно тут)"; \
	    kubectl -n kacho logs "$$p" --all-containers --previous --tail=60 --prefix 2>&1 | tail -80 || true; \
	  done; \
	  exit 1; \
	fi; \
	echo "all workloads rollout-complete"; \
	$(MAKE) --no-print-directory assert-jobs-healthy

# assert-jobs-healthy — периодические/разовые задания тоже часть готовности стенда.
#
# Зачем (реальный случай: `kacho-iam-jwks-rotator` падал в 100% запусков —
# CronJob звал несуществующий subcommand `kacho-iam jwks rotate`): гейт смотрел
# ТОЛЬКО на Deployment/StatefulSet, поэтому сорванное задание не красило его
# вообще никак — стенд считался готовым, а задание тихо копило по одному
# упавшему Job в день. Ровно та невидимость, ради которой эта цель и заведена.
#
# Проверяем два НЕЗАВИСИМЫХ признака; второй ловит случай, когда история упавших
# Job'ов уже вытеснена `failedJobsHistoryLimit` и по Job'ам улик не осталось:
#   1. любой Job с condition Failed=True (BackoffLimitExceeded / DeadlineExceeded);
#   2. CronJob, который УЖЕ запускался (есть lastScheduleTime), но НИ РАЗУ не
#      отработал успешно (нет lastSuccessfulTime) и сейчас не выполняется.
#
# ВАЖНО — «ещё ни разу не выполнялся» это НЕ провал. Свежесозданный CronJob
# (lastScheduleTime отсутствует) обязан быть зелёным, иначе гейт краснел бы на
# каждом только что поднятом стенде до первого тика по расписанию. Ровно так же
# `active`-джоб в полёте не считается провалом — у него нет condition Failed.
assert-jobs-healthy:
	@set -e; \
	failed=$$(kubectl -n kacho get jobs -o json 2>/dev/null \
	  | jq -r '.items[] | select((.status.conditions // []) | map(select(.type=="Failed" and .status=="True")) | length > 0) | .metadata.name'); \
	stuck=$$(kubectl -n kacho get cronjobs -o json 2>/dev/null \
	  | jq -r '.items[] | select(.status.lastScheduleTime != null and .status.lastSuccessfulTime == null and ((.status.active // []) | length) == 0) | .metadata.name'); \
	if [ -n "$$failed" ] || [ -n "$$stuck" ]; then \
	  echo; echo "!!! JOBS UNHEALTHY — периодические задания сорваны"; \
	  for j in $$failed; do \
	    echo "--- Job провалился: $$j"; \
	    kubectl -n kacho get job "$$j" -o wide 2>&1 || true; \
	    kubectl -n kacho get job "$$j" -o json 2>/dev/null \
	      | jq -r '.status.conditions[]? | select(.type=="Failed") | "    reason: \(.reason)\n    message: \(.message // "-")"' || true; \
	    echo "--- logs $$j (last 40; под мог быть уже вытеснен — тогда пусто)"; \
	    kubectl -n kacho logs -l batch.kubernetes.io/job-name="$$j" --all-containers --tail=40 --prefix 2>&1 | tail -60 || true; \
	  done; \
	  for c in $$stuck; do \
	    echo "--- CronJob запускался, но НИ РАЗУ не отработал успешно: $$c"; \
	    kubectl -n kacho get cronjob "$$c" -o wide 2>&1 || true; \
	  done; \
	  exit 1; \
	fi; \
	echo "all jobs/cronjobs healthy (ещё не запускавшиеся — не провал)"

# assert-production-posture — стенд обязан РАБОТАТЬ в production-режиме.
#
# Реализация вынесена в scripts/assert-production-posture.sh — там же подробный
# разбор, почему прежняя inline-версия пропустила kacho-storage (2026-07-25).
# Коротко: она читала СОХРАНЁННЫЕ НАСТРОЙКИ (env подов + ConfigMap'ы), то есть
# проверяла НАМЕРЕНИЕ, а не ИСХОД, и вдобавок обходила только индивидуально
# объявленные переменные — массовый импорт `envFrom` был ей не виден вовсе.
# Сервис работал `dev`+plaintext-DB, его настройки читались `production`+`require`,
# и гейт был зелёным.
#
# Новая версия проверяет ЖИВОЙ ПРОЦЕСС: (A) самоотчёт о посадке из лога текущего
# контейнера, (B) независимое подтверждение шифрования от САМОЙ БАЗЫ
# (pg_stat_ssl), (C) сохранённые настройки по ВСЕМ путям доставки как backstop.
# Устойчива к тому, что под не перезапускался — «Ready» больше ничего не «доказывает».
#
# guard-kind-context: гейт читает логи и exec'ается в поды — на чужом (в т.ч.
# прод) контексте это как минимум утечка диагностики.
assert-production-posture: guard-kind-context
	@bash scripts/assert-production-posture.sh

# assert-outbox-autovacuum — базы с очередями обязаны РАБОТАТЬ со свежей
# статистикой, а не просто быть так описаны в values.
#
# Дренаж клеймит строки по `sent_at IS NULL`, и план для этого предиката берётся
# из последнего ANALYZE. Очередь почти всегда пуста → последний ANALYZE снят на
# пустом backlog'е → во всплеск планировщик входит с оценкой `rows=1`, отбрасывает
# частичные индексы клейма, и claim дорожает с ~0.2 мс до ~125 мс. Держат свежесть
# ДВЕ настройки: табличные autovacuum_analyze_* (миграции) и кластерный
# autovacuum_naptime (чарт) — при дефолтной минуте именно он ограничивающий, и это
# остаточный минутный всплеск в замерах «после» claim-throughput фикса.
#
# Гейт спрашивает ЖИВОЙ postmaster (`pg_settings`), а не ConfigMap: настройка,
# не доехавшая до процесса, — не настройка (см. инцидент kacho-storage 2026-07-25).
# guard-kind-context — как и у соседнего гейта: exec в поды на чужом контексте
# недопустим.
assert-outbox-autovacuum: guard-kind-context
	@bash scripts/assert-outbox-autovacuum.sh

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
	@if [ "$(SVC)" != "vpc" ] && [ "$(SVC)" != "compute" ] && [ "$(SVC)" != "storage" ] && [ "$(SVC)" != "loadbalancer" ] && [ "$(SVC)" != "api-gateway" ] && [ "$(SVC)" != "iam" ] && [ "$(SVC)" != "ui" ] && [ "$(SVC)" != "nlb" ]; then \
		echo "ERROR: unknown service '$(SVC)'"; exit 1; \
	fi; \
	DEPLOY_NAME=$(SVC); \
	if [ "$(SVC)" = "iam" ]; then DEPLOY_NAME=kacho-iam; fi; \
	if [ "$(SVC)" = "ui" ]; then DEPLOY_NAME=ui; fi; \
	if [ "$(SVC)" = "nlb" ]; then DEPLOY_NAME=kacho-nlb; fi; \
	if [ "$(SVC)" = "storage" ]; then DEPLOY_NAME=kacho-storage; fi; \
	if ! kubectl -n kacho get deploy $$DEPLOY_NAME >/dev/null 2>&1; then \
		echo "WARN: service '$(SVC)' (deployment '$$DEPLOY_NAME') is not deployed yet (planned for sub-phase 0.X — see roadmap)"; \
		exit 0; \
	fi; \
	if [ "$(SVC)" = "ui" ]; then \
		: ; \
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
	# Две правки против прежней версии (обе ломали bootstrap с нуля):
	#
	# 1. --show-only принимает путь ОТНОСИТЕЛЬНО корня umbrella → для шаблона САБЧАРТА
	#    нужен префикс charts/<subchart>/. Было `templates/openfga-bootstrap-job.yaml`
	#    (корневой путь) → helm падал «could not find template … in chart», kubectl
	#    получал пустой stdin → «no objects passed to apply» → Job НЕ запускался.
	#    Bootstrap уехал в сабчарт, а путь не поправили (соседний spire-таргет ниже уже
	#    использует правильную форму `charts/…`).
	#
	# 2. Рендерим И RBAC, И Job. В релизе они — helm-ХУКИ (post-install/post-upgrade,
	#    SA weight=0 → Job weight=10), т.е. при штатном `helm upgrade` создаются сами.
	#    Но этот таргет применяет их ВРУЧНУЮ (нужен, когда апгрейд упал и хуки не
	#    отработали), и рендерил ТОЛЬКО Job — без SA. Итог:
	#      pods "openfga-bootstrap-" is forbidden: serviceaccount "openfga-bootstrap" not found
	#    Job висел FailedCreate, kacho-iam-openfga-store не создавался, iam оставался с
	#    «openfga: HTTP client not configured» → каждый Check «code 14 authz service
	#    unavailable» → newman дох на Project.Create.
	@helm template kacho-umbrella ./helm/umbrella -n kacho -f ./helm/umbrella/values.dev.yaml \
	  --show-only charts/openfga-bootstrap/templates/openfga-bootstrap-rbac.yaml \
	  --show-only charts/openfga-bootstrap/templates/openfga-bootstrap-job.yaml | kubectl -n kacho apply -f -
	@echo "Ждём завершения…"
	@kubectl -n kacho wait --for=condition=complete job/openfga-bootstrap --timeout=300s || \
	 kubectl -n kacho wait --for=condition=failed job/openfga-bootstrap --timeout=10s
	@kubectl -n kacho logs -l job-name=openfga-bootstrap --tail=-1

# KAC-127 (deploy#38): regenerate the openfga-model-stub ConfigMap from the
# CANONICAL model. Run after any edit of $(OPENFGA_CANONICAL_FGA) — never edit the
# DSL or the JSON inside the ConfigMap, both blocks are generated output and the
# gate services/iam/internal/authzmap/fga_model_configmap_identity_test.go fails
# the build if they drift from the canonical file. The openfga/cli image is
# distroless (no shell), so the transform is done here at commit-time instead of
# in a runtime init-container — see openfga-bootstrap-job.yaml.
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