# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

BINARY         := kacho-vpc
CMD            := ./cmd/vpc
# Миграции исполняет отдельный binary мигратора (не subcommand основного сервера).
MIGRATOR_BIN   := kacho-migrator
MIGRATOR_CMD   := ./cmd/migrator
IMAGE          := kacho-vpc:dev

# Ревизия дерева уезжает в образ аргументом сборки (IMAGE_BUILD_ARGS).
# Объявление одно на дерево — см. разбор в самом файле.
include ../../provenance.mk

.PHONY: build build-migrator test test-short vet lint docker audit-list-filter helm-render-guard

build:
	CGO_ENABLED=0 go build -o bin/$(BINARY) $(CMD)

build-migrator:
	CGO_ENABLED=0 go build -o bin/$(MIGRATOR_BIN) $(MIGRATOR_CMD)

# Каноничная команда живёт в КОРНЕВОМ Makefile — здесь только делегация: у флагов
# и бюджета прогона должна быть ОДНА истина. Собственный `-timeout` в этом файле
# ни с CI, ни с соседними сервисами не сверялся ничем и разъехался (300s/900s
# вразнобой, и там где мало — молча недостижимо).
test:
	$(MAKE) -C ../.. test-service SVC=vpc

test-short:
	$(MAKE) -C ../.. test-service-short SVC=vpc

vet:
	go vet ./...

# audit-list-filter — CI gate for the public List<Resource> surface: a caller must
# receive only the rows it may see (per-object, kaname AuthorizeService.BatchCheck
# over the page just read), not every row of its project.
#
# The check parses the tree, so a resource is recognised by what its declaration IS —
# a package declaring `List` on the transport type — and never by which file holds
# it; see corelib/listfiltergate for the whole contract.
#
# The run always prints its census (files, packages, resources, checked,
# whitelisted): "zero findings" must be distinguishable from "zero read", so a tree
# the gate could not open is a finding, not an OK. The addresspool exclusion lives in
# tools/audit-list-filter.sh next to the reason for it, and an exclusion with nothing
# left to exclude is a finding too.
#
# Invoked by CI as `make -C services/vpc audit-list-filter`; the gate's own detection
# logic is locked by services/vpc/tools/audit_list_filter_test.go, which runs it
# against the real tree through this same wrapper.
audit-list-filter:
	@./tools/audit-list-filter.sh

# helm-render-guard — офлайновый страж рендера чарта: ребро vpc→geo обязано
# набирать имя Service `kacho-geo` (его SAN сертификата это имя покрывает), а не
# голый хост `geo.kacho…`, который не резолвится и не проходит проверку
# serverName. Чистый `helm template`, кластер не нужен.
#
# ЦЕЛИ НЕ БЫЛО ВОВСЕ: скрипт лежал в дереве, и его не звал ни один конвейер, ни
# один Makefile. Утверждения о провязке ребра существовали как текст и ни разу не
# проверялись. Теперь цель вызывается из CI (.github/workflows/ci.yaml, job
# `helm`), а то, что она вызывается, залочено гейтом
# internal/repohygiene/artifactgates/renderguard_test.go.
helm-render-guard:
	bash deploy/render-guard.sh

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
	cd .. && docker build $(IMAGE_BUILD_ARGS) -f kacho-vpc/Dockerfile -t $(IMAGE) .

.PHONY: migrate-up migrate-down migrate-status
# migrate-* дергают отдельный binary `bin/kacho-migrator`.
# Зависимость на build-migrator гарантирует, что bin/ актуальный.
migrate-up: build-migrator
	KACHO_VPC_DB_PASSWORD=secret bin/$(MIGRATOR_BIN) up

migrate-down: build-migrator
	KACHO_VPC_DB_PASSWORD=secret bin/$(MIGRATOR_BIN) down

migrate-status: build-migrator
	KACHO_VPC_DB_PASSWORD=secret bin/$(MIGRATOR_BIN) status
