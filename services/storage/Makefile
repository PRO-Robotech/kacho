# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

BINARY         := storage
CMD            := ./cmd/storage
MIGRATOR_BIN   := kacho-migrator
MIGRATOR_CMD   := ./cmd/migrator
IMAGE          := kacho-storage:dev

# Local polyrepo: go.mod pins kacho-proto/kacho-corelib via `replace ../` → docker
# build-context is the PARENT dir (siblings must be COPY-able). See Dockerfile.
DOCKER_CONTEXT ?= ..
CORELIB_COMMON ?= ../kacho-corelib/migrations/common

.PHONY: build build-migrator test test-short vet lint audit-list-filter docker sync-migrations generate-mocks sqlc-gen \
        migrate-up migrate-down migrate-status

build:
	CGO_ENABLED=0 go build -o bin/$(BINARY) $(CMD)

build-migrator:
	CGO_ENABLED=0 go build -o bin/$(MIGRATOR_BIN) $(MIGRATOR_CMD)

# test — unit + integration (testcontainers Postgres). При contention гнать с -p 1.
test:
	go test ./... -race -cover -timeout 900s

test-short:
	go test ./... -race -cover -short -timeout 120s

vet:
	go vet ./...

lint:
	golangci-lint run ./...

# audit-list-filter — CI gate: публичный project-scoped List<Resource> сужает строки
# по project_id (listauthz posture, INV-10; кросс-проектной утечки нет by construction).
# DiskType (cluster-catalog) whitelisted. См. tools/audit-list-filter.sh.
audit-list-filter:
	./tools/audit-list-filter.sh

# docker — multi-stage образ (storage + kacho-migrator). Контекст — родительский
# каталог, чтобы COPY подтянул siblings kacho-proto/kacho-corelib (replace ../).
docker:
	docker build -f Dockerfile -t $(IMAGE) $(DOCKER_CONTEXT)

# sync-migrations — подтягивает общую LRO-таблицу operations из corelib в
# internal/migrations/common/. Источник истины — kacho-corelib; rpc-implementer
# встраивает синканные файлы при реализации первого async-RPC.
sync-migrations:
	@test -d "$(CORELIB_COMMON)" || { echo "corelib common не найден: $(CORELIB_COMMON) (переопредели CORELIB_COMMON=...)"; exit 1; }
	mkdir -p internal/migrations/common
	cp $(CORELIB_COMMON)/*.sql internal/migrations/common/
	@echo "synced corelib operations migrations → internal/migrations/common/"

# generate-mocks — port-моки hand-written (function-field, internal/ports/portmock);
# таргет компилирует их как guard соответствия портам use-case.
generate-mocks:
	go build ./internal/ports/portmock/...
	@echo "portmock is hand-written (function-field); compile guard OK"

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
