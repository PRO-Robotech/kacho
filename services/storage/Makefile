# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

BINARY         := storage
CMD            := ./cmd/storage
MIGRATOR_BIN   := kacho-migrator
MIGRATOR_CMD   := ./cmd/migrator
IMAGE          := kacho-storage:dev

# Ревизия дерева уезжает в образ аргументом сборки (IMAGE_BUILD_ARGS).
# Объявление одно на дерево — см. разбор в самом файле.
include ../../provenance.mk

# Local polyrepo: go.mod pins kacho-proto/kacho-corelib via `replace ../` → docker
# build-context is the PARENT dir (siblings must be COPY-able). See Dockerfile.
DOCKER_CONTEXT ?= ..

.PHONY: build build-migrator test test-short vet lint audit-list-filter audit-known-failing docker generate-mocks sqlc-gen \
        migrate-up migrate-down migrate-status

build:
	CGO_ENABLED=0 go build -o bin/$(BINARY) $(CMD)

build-migrator:
	CGO_ENABLED=0 go build -o bin/$(MIGRATOR_BIN) $(MIGRATOR_CMD)

# test — unit + integration (testcontainers Postgres). При contention гнать с -p 1.
# Каноничная команда живёт в КОРНЕВОМ Makefile — здесь только делегация: у флагов
# и бюджета прогона должна быть ОДНА истина. Собственный `-timeout` в этом файле
# ни с CI, ни с соседними сервисами не сверялся ничем и разъехался (300s/900s
# вразнобой, и там где мало — молча недостижимо).
test:
	$(MAKE) -C ../.. test-service SVC=storage

test-short:
	$(MAKE) -C ../.. test-service-short SVC=storage

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

# audit-list-filter — CI gate публичного List<Resource>. Проверяет ДВА измерения:
#   (1) project-scope: repo.List сужает строки по project_id + use-case требует
#       непустой projectId (listauthz posture, INV-10 — кросс-проектной утечки нет);
#   (2) per-object видимость: use-case List прогоняет ПРОЧИТАННУЮ страницу через
#       authzfilter.FilterVisiblePage (kaname BatchCheck на `viewer` — том же
#       отношении, что энфорсит Get: видно в списке ровно то, что Get отдаст).
# (1) без (2) — ровно то слепое пятно, с которым storage отгрузил List, показывавший
# любому члену проекта каждый том/снимок/образ. DiskType (cluster-catalog) whitelisted.
#
# Как это запускается. Корневого Makefile в репо НЕТ, поэтому голое
# `make audit-list-filter` из корня отвечает «No rule to make target» — команда
# набирается как `make -C services/storage audit-list-filter`. Ровно в этой форме её
# и вызывает CI: .github/workflows/ci.yaml, job authz-artifacts, шаг
# «listauthz — публичный List фильтрует по объекту (5 сервисов)». Прежняя редакция
# этого комментария утверждала обратное («CI-workflow этот make-таргет не вызывает») —
# верно на момент написания, ложно с тех пор, как шаг появился. Чтобы утверждение не
# протухло снова, оно проверяется: tools/ci_gate_wiring_test.go.
# Дубль в go test (tools/audit_list_filter_test.go) остаётся: он гоняет гейт против
# РЕАЛЬНОГО дерева сервиса внутри обычного `go test ./...`. См. tools/audit-list-filter.sh.
audit-list-filter:
	./tools/audit-list-filter.sh

# audit-known-failing — CI gate двух утверждений сюиты о себе самой:
#   (1) состав (таблица в tests/newman/docs/RESULTS.md) совпадает со сгенерированными
#       коллекциями — имена, счётчики, итог;
#   (2) запись «известное красное» не переживает свой фикс: у неё обязан быть
#       ОТКРЫТЫЙ тикет и кейс с парной `# verifies …/issues/N`-аннотацией (связь в
#       обе стороны). Закрытый тикет = записи больше нечего исключать = находка.
# Повод: обе прежние записи пережили свои фиксы, а ростер отстал на две коллекции и
# 40 кейсов — то есть документ утверждал про продукт неправду в трёх местах сразу.
# Измерение (2) требует трекера; без него гейт НЕ падает, но печатает каждую
# непроверенную запись координатой — «не выполнялось» обязано быть отличимо от
# «прошло». Дубль в go test (tools/audit_known_failing_test.go) гоняет тот же гейт
# против реальной сюиты. См. tools/audit-known-failing.sh.
audit-known-failing:
	./tools/audit-known-failing.sh

# docker — multi-stage образ (storage + kacho-migrator). Контекст — родительский
# каталог, чтобы COPY подтянул siblings kacho-proto/kacho-corelib (replace ../).
docker:
	docker build $(IMAGE_BUILD_ARGS) -f Dockerfile -t $(IMAGE) $(DOCKER_CONTEXT)

# generate-mocks — port-моки hand-written (function-field, internal/repo/repomock);
# таргет компилирует их как guard соответствия портам use-case.
generate-mocks:
	go build ./internal/repo/repomock/...
	@echo "repomock is hand-written (function-field); compile guard OK"

# sqlc-gen — генерация типобезопасного pgx-кода из internal/repo/queries/*.sql в
# internal/repo/pg/gen (sqlc, БЕЗ ORM). Конфиг sqlc.yaml + queries добавляет
# rpc-implementer с первой доменной таблицей.
sqlc-gen:
	@test -f sqlc.yaml || { echo "sqlc.yaml ещё нет — добавит rpc-implementer с первой доменной таблицей"; exit 0; }
	sqlc generate

migrate-up: build-migrator
	KACHO_STORAGE_DB_PASSWORD=secret bin/$(MIGRATOR_BIN) up

migrate-down: build-migrator
	KACHO_STORAGE_DB_PASSWORD=secret bin/$(MIGRATOR_BIN) down

migrate-status: build-migrator
	KACHO_STORAGE_DB_PASSWORD=secret bin/$(MIGRATOR_BIN) status
