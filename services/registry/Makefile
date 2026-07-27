# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

BINARY         := kacho-registry
CMD            := ./cmd/kacho-registry
MIGRATOR_BIN   := kacho-migrator
MIGRATOR_CMD   := ./cmd/migrator
IMAGE          := kacho-registry:dev

.PHONY: build build-migrator test test-short vet lint docker sync-migrations audit-list-filter

build:
	CGO_ENABLED=0 go build -o bin/$(BINARY) $(CMD)

build-migrator:
	CGO_ENABLED=0 go build -o bin/$(MIGRATOR_BIN) $(MIGRATOR_CMD)

# test — unit + integration (testcontainers Postgres). При contention гнать с
# -p 1 (локально DOCKER_HOST=colima).
test:
	go test ./... -race -cover -timeout 900s

test-short:
	go test ./... -race -cover -short -timeout 120s

vet:
	go vet ./...

lint:
	golangci-lint run ./...

docker:
	docker build -f Dockerfile -t $(IMAGE) .

# sync-migrations — синхронизирует common-миграции из kacho-corelib (operations
# table и т. п.), когда они выделены в общий набор. Текущий скелет держит
# operations в 0001_init.sql; цель — анкер под переход на общий набор corelib.
CORELIB_MIGRATIONS ?= ../kacho-corelib/migrations/common
sync-migrations:
	@test -d "$(CORELIB_MIGRATIONS)" || { echo "corelib common migrations не найдены: $(CORELIB_MIGRATIONS)"; exit 0; }
	@mkdir -p internal/migrations/common
	@cp -v "$(CORELIB_MIGRATIONS)"/* internal/migrations/common/ 2>/dev/null || true

# audit-list-filter — CI-гейт listauthz: публичный List обязан отдавать только те
# объекты, которые вызывающему видеть можно.
#
# До этого здесь стоял `echo` и `exit 0`: цель существовала, выглядела рабочей и не
# проверяла ничего — а CI её и не звал (цикл перечислял compute/nlb/storage/vpc).
# Значит каждый документ, ссылавшийся на «зелёный audit-list-filter» реестра,
# ссылался на пустоту. Теперь цель запускает настоящий анализ.
#
# Почему не скрипт-близнец storage. Инвариант общий, место энфорса — нет. В storage
# List проектный: repo сужает строки по project_id, use-case прогоняет прочитанную
# страницу через батч-проверку — и тамошний shell-гейт грепает internal/repo/pg +
# internal/service. В реестре таких директорий нет вовсе (гейт прошёл бы вхолостую):
# грант живёт на registry_registry/registry_repository, репозиторий принадлежит
# реестру, а не проекту, теги/референсы вообще не объекты прав, а список
# репозиториев — объединение durable-overlay и живой проекции zot, где сужать
# нечего. Поэтому решение принимает handler, а гейт разбирает его AST.
#
# Команда набирается как `make -C services/registry audit-list-filter` (корневого
# Makefile в репо нет). Ровно в этой форме её зовёт CI: .github/workflows/ci.yaml,
# job authz-artifacts. Что CI её действительно зовёт — проверяется тестом
# tools/auditlistfilter/ci_wiring_test.go, чтобы утверждение не протухло молча.
# Тот же анализ гоняется внутри обычного `go test ./services/registry/...`
# (tools/auditlistfilter/audit_test.go, TestAnalyzeRealTree).
audit-list-filter:
	@go run ./tools/auditlistfilter/cmd/audit-list-filter

.PHONY: migrate-up migrate-down migrate-status
migrate-up: build-migrator
	KACHO_REGISTRY_DB_PASSWORD=secret bin/$(MIGRATOR_BIN) up

migrate-down: build-migrator
	KACHO_REGISTRY_DB_PASSWORD=secret bin/$(MIGRATOR_BIN) down

migrate-status: build-migrator
	KACHO_REGISTRY_DB_PASSWORD=secret bin/$(MIGRATOR_BIN) status
