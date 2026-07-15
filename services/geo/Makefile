# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

BINARY         := kacho-geo
CMD            := ./cmd/kacho-geo
MIGRATOR_BIN   := kacho-migrator
MIGRATOR_CMD   := ./cmd/migrator
IMAGE          := kacho-geo:dev

.PHONY: build build-migrator test test-short vet lint docker proto-install-plugins proto-vendor proto-lint proto-gen

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

# proto-install-plugins — ставит protoc-плагины в $GOBIN (lookup через $PATH для buf).
# Доменный proto geo генерируется этими тремя плагинами; permission-catalog для geo —
# hand-written (internal/check/permission_map.go), buf-catalog-плагин не нужен.
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
