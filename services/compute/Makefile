# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

BINARY         := kacho-compute
CMD            := ./cmd/compute
IMAGE          := kacho-compute:dev

.PHONY: build test test-short vet lint docker audit-list-filter proto-install-plugins proto-lint proto-vendor proto-gen

build:
	CGO_ENABLED=0 go build -o bin/$(BINARY) $(CMD)
	CGO_ENABLED=0 go build -o bin/kacho-migrator ./cmd/migrator

test:
	go test ./... -race -cover -timeout 300s

test-short:
	go test ./... -race -cover -short -timeout 120s

vet:
	go vet ./...

# audit-list-filter — CI gate for the public List<Resource> surface: a caller must
# receive only the rows it may see (per-object, kacho-iam BatchCheck), not every row
# of its project. The check parses the tree, so a resource is recognised by what its
# declaration IS — a `List` method on a `<X>Handler` type — and never by what the
# receiver variable is called; see tools/listfiltergate for the whole contract.
#
# The run always prints its census (files, resources, checked, whitelisted): "zero
# findings" must be distinguishable from "zero read", so a tree the gate could not
# open is a finding, not an OK. Exclusions are named in tools/audit-list-filter.sh
# and an exclusion with nothing left to exclude is a finding too.
#
# Invoked by CI as `make -C services/compute audit-list-filter`; the gate's own
# detection logic is locked by services/compute/tools/audit_list_filter_test.go,
# which runs it against the real tree through this same wrapper.
audit-list-filter:
	@./tools/audit-list-filter.sh

# Сверщик переписи кейсов сюиты: дублей нет, числа в шапке docs/CASES-INDEX.md
# совпадают с деревом, ни один заголовок не называет удалённый файл кейсов. Здесь его
# не было — и там же выжили раздел про `cases/instance.py` (файла нет с 2026-07-21) и
# перепись 111/43 против 117/49. Конвейер зовёт сверщиков обходом дерева (шаг
# «перепись кейсов сюиты совпадает с деревом»); эта цель — для локального прогона.
.PHONY: validate-newman
validate-newman:
	cd tests/newman && python3 scripts/validate-cases.py

lint:
	golangci-lint run ./...

docker:
	cd .. && docker build -f kacho-compute/Dockerfile -t $(IMAGE) .

# proto-install-plugins — ставит protoc-плагины в $GOBIN (lookup через $PATH для buf).
# Доменный proto compute генерируется этими тремя плагинами; permission-catalog для compute —
# hand-written (internal/check/permission_map.go), buf-catalog-плагин не нужен.
proto-install-plugins:
	go install google.golang.org/protobuf/cmd/protoc-gen-go
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc
	go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway

proto-lint:
	cd proto && buf lint

# proto-vendor — подтягивает corelib-owned инфра-протосы
# (operation/validation/authz_options/cloud-api + vendored google) из ../kacho-corelib/proto
# в proto/ для buf-резолва импортов. Единственный источник истины этих файлов —
# kacho-corelib; здесь они gitignored и в git не коммитятся (Go-stubs тоже single-source
# corelib). Запускается перед proto-gen.
proto-vendor:
	@set -e; src=../kacho-corelib/proto; \
	for f in google/api/annotations.proto google/api/field_behavior.proto \
	         google/api/http.proto google/rpc/status.proto \
	         kacho/cloud/api/operation.proto kacho/cloud/operation/operation.proto \
	         kacho/cloud/validation.proto kacho/iam/authz/v1/authz_options.proto; do \
	  mkdir -p "proto/$$(dirname $$f)"; \
	  cp "$$src/$$f" "proto/$$f"; \
	done

# proto-gen — регенерация Go-stubs доменного proto compute (kacho/cloud/compute/v1,
# kacho/cloud/access, kacho/cloud/maintenance/v2) из proto/. Универсальная ИНФРА
# (operation/validation/authz_options/cloud-api/google) подтягивается proto-vendor только
# для buf-резолва импортов и НЕ генерируется (Go-stubs живут в kacho-corelib / canonical
# genproto) — см. proto/buf.gen.yaml inputs.paths.
proto-gen: proto-vendor
	cd proto && buf generate

.PHONY: migrate-up migrate-down migrate-status
migrate-up:
	KACHO_COMPUTE_DB_PASSWORD=secret bin/kacho-migrator up

migrate-down:
	KACHO_COMPUTE_DB_PASSWORD=secret bin/kacho-migrator down

migrate-status:
	KACHO_COMPUTE_DB_PASSWORD=secret bin/kacho-migrator status
