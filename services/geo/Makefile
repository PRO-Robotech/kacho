# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

BINARY         := kacho-geo
CMD            := ./cmd/kacho-geo
MIGRATOR_BIN   := kacho-migrator
MIGRATOR_CMD   := ./cmd/migrator
IMAGE          := kacho-geo:dev

# Ревизия дерева уезжает в образ аргументом сборки (IMAGE_BUILD_ARGS).
# Объявление одно на дерево — см. разбор в самом файле.
include ../../provenance.mk

.PHONY: build build-migrator test test-short vet lint docker proto-install-plugins proto-vendor proto-lint proto-gen audit-list-filter

build:
	CGO_ENABLED=0 go build -o bin/$(BINARY) $(CMD)

build-migrator:
	CGO_ENABLED=0 go build -o bin/$(MIGRATOR_BIN) $(MIGRATOR_CMD)

# test — unit + integration (testcontainers Postgres). При contention гнать с
# -p 1 (локально DOCKER_HOST=colima).
# Каноничная команда живёт в КОРНЕВОМ Makefile — здесь только делегация: у флагов
# и бюджета прогона должна быть ОДНА истина. Собственный `-timeout` в этом файле
# ни с CI, ни с соседними сервисами не сверялся ничем и разъехался (300s/900s
# вразнобой, и там где мало — молча недостижимо).
test:
	$(MAKE) -C ../.. test-service SVC=geo

test-short:
	$(MAKE) -C ../.. test-service-short SVC=geo

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

docker:
	docker build $(IMAGE_BUILD_ARGS) -f Dockerfile -t $(IMAGE) .

# proto-install-plugins — ставит protoc-плагины в $GOBIN (lookup через $PATH для buf).
# Доменный proto geo генерируется этими тремя плагинами; buf-catalog-плагин не нужен —
# отношения geo объявлены аннотациями в самом proto (`permission` / `required_relation`
# в internal_catalog_service.proto), рукописной карты RPC→объект у сервиса нет: здесь
# стоял путь в `internal/check`, каталога с таким именем у geo ноль файлов.
proto-install-plugins:
	go install google.golang.org/protobuf/cmd/protoc-gen-go
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc
	go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway

# proto-vendor — подтягивает corelib-owned инфра-протосы в proto/ для buf-резолва
# импортов доменного geo. Единственный источник истины — kacho-corelib; локальные
# копии gitignored и НЕ коммитятся (дубля .proto в git нет). Запускается перед
# buf lint/generate. WKT google/protobuf/* резолвит сам buf (вендорить не нужно).
CORELIB_PROTO ?= ../kacho-corelib/proto
VENDORED_PROTOS := \
	google/api/annotations.proto \
	google/api/field_behavior.proto \
	google/api/http.proto \
	google/rpc/status.proto \
	kacho/cloud/api/operation.proto \
	kacho/cloud/operation/operation.proto \
	kacho/cloud/validation.proto \
	kacho/iam/authz/v1/authz_options.proto

proto-vendor:
	@test -d "$(CORELIB_PROTO)" || { echo "corelib proto не найден: $(CORELIB_PROTO) (переопредели CORELIB_PROTO=...)"; exit 1; }
	@for f in $(VENDORED_PROTOS); do \
		mkdir -p "proto/$$(dirname $$f)"; \
		cp "$(CORELIB_PROTO)/$$f" "proto/$$f"; \
	done

proto-lint: proto-vendor
	cd proto && buf lint

# proto-gen — регенерация Go-stubs доменного proto geo (kacho/cloud/geo/v1) из proto/.
# Универсальная инфра (operation/validation/authz_options/cloud-api/google) вендорится
# таргетом proto-vendor только для buf-резолва импортов и НЕ генерируется (Go-stubs
# живут в kacho-corelib / canonical genproto) — см. proto/buf.gen.yaml inputs.paths.
proto-gen: proto-vendor
	cd proto && buf generate

.PHONY: migrate-up migrate-down migrate-status
migrate-up: build-migrator
	KACHO_GEO_DB_PASSWORD=secret bin/$(MIGRATOR_BIN) up

migrate-down: build-migrator
	KACHO_GEO_DB_PASSWORD=secret bin/$(MIGRATOR_BIN) down

migrate-status: build-migrator
	KACHO_GEO_DB_PASSWORD=secret bin/$(MIGRATOR_BIN) status

# audit-list-filter — CI gate for kacho-geo's listing surface: every method that
# hands a page to a caller must narrow it, and must declare HOW. What is checked
# lives in pkg/listfiltergate; how this service is laid out lives in
# services/geo/tools/auditlistfilter.
#
# This target is NEW: geo had no gate of this class, and nothing was red because the
# set of services to analyse was written by hand and geo was in neither the CI loop
# nor the set of directories anyone remembered to create. That set is now derived
# from the committed tree by pkg/listfiltergate/coverage_test.go, which reports an
# unanalysed service as a finding.
#
# Invoked by CI as `make -C services/geo audit-list-filter`.
audit-list-filter:
	@./tools/audit-list-filter.sh
