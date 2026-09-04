# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

BINARY_API     := kacho-loadbalancer
BINARY_MIG     := kacho-migrator
CMD_API        := ./cmd/kacho-loadbalancer
CMD_MIG        := ./cmd/migrator
IMAGE          := kacho-nlb:dev

# Ревизия дерева уезжает в образ аргументом сборки (IMAGE_BUILD_ARGS).
# Объявление одно на дерево — см. разбор в самом файле.
include ../../provenance.mk

.PHONY: build build-api build-migrator test test-short vet lint docker helm-lint helm-render-guard audit-list-filter proto-install-plugins proto-vendor proto-lint proto-gen

build: build-api build-migrator

build-api:
	CGO_ENABLED=0 go build -o bin/$(BINARY_API) $(CMD_API)

build-migrator:
	CGO_ENABLED=0 go build -o bin/$(BINARY_MIG) $(CMD_MIG)

# Каноничная команда живёт в КОРНЕВОМ Makefile — здесь только делегация: у флагов
# и бюджета прогона должна быть ОДНА истина. Собственный `-timeout` в этом файле
# ни с CI, ни с соседними сервисами не сверялся ничем и разъехался (300s/900s
# вразнобой, и там где мало — молча недостижимо).
test:
	$(MAKE) -C ../.. test-service SVC=nlb

test-short:
	$(MAKE) -C ../.. test-service-short SVC=nlb

vet:
	go vet ./...

# Кэш линтера — СВОЙ у каждой рабочей копии. Умолчание (~/.cache/golangci-lint)
# общее на машину, а запись в нём ключуется содержимым пакета, не деревом: соседняя
# копия того же модуля возвращает сюда разбор ЧУЖИХ файлов — с чужими путями, мимо
# якорных исключений .golangci.yml (^pkg/api/), которые такой путь не матчат.
# Гейт: internal/repohygiene/lintverdict_test.go
# Присваивание жёсткое (`:=`), а не `?=`: с `?=` унаследованная из окружения
# переменная молча вернула бы общий кэш, и защита осталась бы на вид на месте.
# Переопределить по-прежнему можно — аргументом make, то есть заявив это явно.
GOLANGCI_LINT_CACHE := $(abspath $(CURDIR)/../..)/.cache/golangci-lint
export GOLANGCI_LINT_CACHE

lint:
	golangci-lint run ./...

# proto-install-plugins — ставит protoc-плагины в $GOBIN (lookup через $PATH для buf).
# Доменный proto kacho-nlb генерируется этими тремя плагинами; permission-catalog для nlb —
# hand-written (internal/check/permission_map.go), buf-catalog-плагин не нужен.
proto-install-plugins:
	go install google.golang.org/protobuf/cmd/protoc-gen-go
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc
	go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway

# proto-vendor — подтягивает corelib-owned инфра-протосы (operation/validation/
# authz_options/cloud-api + google/*) из kacho-corelib в proto/ ТОЛЬКО для buf-резолва
# импортов доменного proto. Единственный источник этих файлов — kacho-corelib; здесь они
# gitignored и не коммитятся (их Go-stubs тоже живут в kacho-corelib, дублей нет). Цель
# подтягивает их локально перед buf lint/generate.
CORELIB_PROTO ?= ../kacho-corelib/proto
VENDORED_INFRA_PROTOS = \
	google/api/annotations.proto \
	google/api/http.proto \
	google/rpc/status.proto \
	kacho/cloud/api/operation.proto \
	kacho/cloud/operation/operation.proto \
	kacho/cloud/validation.proto \
	kacho/iam/authz/v1/authz_options.proto

proto-vendor:
	@for f in $(VENDORED_INFRA_PROTOS); do \
		mkdir -p "proto/$$(dirname $$f)"; \
		cp "$(CORELIB_PROTO)/$$f" "proto/$$f"; \
	done

proto-lint: proto-vendor
	cd proto && buf lint

# proto-gen — регенерация Go-stubs доменного proto nlb (kacho/cloud/loadbalancer/v1) из
# proto/. Универсальная ИНФРА (operation/validation/authz_options/cloud-api/google)
# подтягивается proto-vendor только для buf-резолва импортов и НЕ генерируется (Go-stubs
# живут в kacho-corelib / canonical genproto) — см. proto/buf.gen.yaml inputs.paths.
proto-gen: proto-vendor
	cd proto && buf generate

# audit-list-filter — CI gate for the public List<Resource> surface: a caller must
# receive only the rows it may see (per-object, kacho-iam AuthorizeService.BatchCheck
# over the page just read), not every row of its project.
#
# The backend is BatchCheck, not ListObjects: the older comment here named the latter
# long after it was removed, and it is now a shape the gate refuses — that answer is
# capped server-side with no continuation token, so a tenant's own rows fall outside
# the cap and vanish.
#
# The check parses the tree, so a resource is recognised by what its declaration IS —
# a package declaring `List` on the transport type — and never by which file holds
# the use-case; see pkg/listfiltergate for the whole contract.
#
# The run always prints its census (files, packages, resources, checked,
# whitelisted): "zero findings" must be distinguishable from "zero read", so a tree
# the gate could not open is a finding, not an OK.
#
# Invoked by CI as `make -C services/nlb audit-list-filter`; the gate's own detection
# logic is locked by services/nlb/tools/audit_list_filter_test.go, which runs it
# against the real tree through this same wrapper.
audit-list-filter:
	@./tools/audit-list-filter.sh

docker:
	cd .. && docker build $(IMAGE_BUILD_ARGS) -f kacho-nlb/Dockerfile -t $(IMAGE) .

# ---- Helm deploy-chart render guards (offline `helm template`/`lint`) -------

helm-lint:
	helm lint deploy/ --set db.password=test

# Asserts cross-service peer-edge wiring (vpc/compute/iam/geo) actually renders
# into config.yaml + mTLS. Pure chart render — no cluster contact.
#
# ВЫЗЫВАЕТСЯ ИЗ CI (.github/workflows/ci.yaml, job `helm` — там уже пришпилен
# helm v3.17). До этого её не звал НИКТО: ни один workflow, ни одна другая цель.
# Утверждения о провязке рёбер существовали как текст и ни разу не проверялись —
# ровно то, против чего страж и написан. Что цель вызывается, залочено гейтом
# internal/repohygiene/artifactgates/renderguard_test.go: он находит стражей рендера по дереву
# и требует, чтобы каждый был достижим из конвейера.
helm-render-guard:
	bash deploy/tests/render-guard.sh

# newman-assertion-census — гейт честности утверждений суиты. Стенда не требует:
# исполняет скрипты сгенерированных коллекций против подставленных ответов и смотрит,
# ЧТО они утверждают. Ловит две вещи: кейс, для которого успех и отказ неразличимы
# (упасть на своём предмете он не может), и умолчание авторизации, подменяющее
# незасеянного субъекта суиты другим, более полномочным. Несёт проверку собственной
# предпосылки: по каждому предикату инъекция дефекта ловится, законные конструкции той
# же формы пропускаются. Любая находка — дефект, а не шум гейта.
.PHONY: newman-assertion-census
newman-assertion-census:
	@cd tests/newman && node scripts/selftest-assertions.js

.PHONY: migrate-up migrate-down migrate-status
migrate-up:
	KACHO_NLB_REPOSITORY__POSTGRES__URL=$$DSN bin/$(BINARY_MIG) up --dialect=postgres --dsn=$$DSN

migrate-down:
	KACHO_NLB_REPOSITORY__POSTGRES__URL=$$DSN bin/$(BINARY_MIG) down --dialect=postgres --dsn=$$DSN

migrate-status:
	KACHO_NLB_REPOSITORY__POSTGRES__URL=$$DSN bin/$(BINARY_MIG) status --dialect=postgres --dsn=$$DSN

.PHONY: run
run: build-api
	bin/$(BINARY_API) serve

# ---- Newman regression -----------------------------------------------------

.PHONY: gen-newman test-newman test-newman-incremental validate-newman

gen-newman:
	cd tests/newman && python3 scripts/gen.py

validate-newman:
	cd tests/newman && python3 scripts/validate-cases.py

test-newman: gen-newman
	cd tests/newman && ./scripts/run.sh

test-newman-incremental: gen-newman
	cd tests/newman && ./scripts/run-incremental.sh
