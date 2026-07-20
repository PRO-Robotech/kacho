# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

BINARY := api-gateway
CMD    := ./cmd/api-gateway
IMAGE  := kacho-api-gateway:dev

# Permission catalog — рантайм source-of-truth для per-RPC authz-middleware.
# Генерируется ЗДЕСЬ (api-gateway импортирует proto всех доменов) скриптом
# scripts/gen-permission-catalog.sh из единого внутрирепозиторного proto-дерева
# монорепо (proto/ в корне: iam / vpc / compute / geo / loadbalancer / registry /
# storage + общая инфраструктура operation / validation / authz_options).
# Рантайм использует вшитую копию ниже.
PERMISSION_CATALOG_TARGET := internal/middleware/embed/permission_catalog.json
PERMISSION_CATALOG_BUILD  := build/permission_catalog.json

# REST route table — static REST-path -> gRPC-FQN map used by the same authz
# middleware to translate an incoming REST request into its catalog key.
# Generated ЗДЕСЬ by scripts/gen-rest-route-table.sh from the `google.api.http`
# annotations across the same in-repo proto/ tree. Unlike the catalog it is a
# committed .go source (not an embed) — the runtime compiles it directly.
REST_ROUTE_TABLE_TARGET := internal/middleware/rest_route_table_gen.go
REST_ROUTE_TABLE_BUILD  := build/rest_route_table_gen.go

.PHONY: build test vet lint docker helm-lint permission-catalog permission-catalog-apply \
        rest-route-table rest-route-table-apply

build:
	CGO_ENABLED=0 go build -o bin/$(BINARY) $(CMD)

test:
	go test ./... -race -cover -timeout 300s

vet:
	go vet ./...

lint:
	golangci-lint run ./...

docker:
	cd .. && docker build -f kacho-api-gateway/Dockerfile -t $(IMAGE) .

helm-lint:
	helm lint deploy/

# Регенерация каталога из proto всех доменов в $(PERMISSION_CATALOG_BUILD)
# (не перезаписывает вшитый embed) + diff против текущего рантайм-каталога.
# Требует полный checkout монорепо (внутрирепозиторное proto/) + buf.
permission-catalog:
	./scripts/gen-permission-catalog.sh $(PERMISSION_CATALOG_BUILD)
	@echo "--- diff $(PERMISSION_CATALOG_TARGET) (embedded) vs regenerated ---"
	@diff -u $(PERMISSION_CATALOG_TARGET) $(PERMISSION_CATALOG_BUILD) || true

# Принять регенерированный каталог как новый вшитый source-of-truth.
# Осознанное действие (меняет рантайм-authz-контракт) — после ревью diff'а.
permission-catalog-apply: permission-catalog
	cp $(PERMISSION_CATALOG_BUILD) $(PERMISSION_CATALOG_TARGET)
	@echo "Applied regenerated catalog to $(PERMISSION_CATALOG_TARGET)."

# CI GATE: fail if the embedded catalog is STALE vs the proto surface. Catches the
# class of regression where a new/renamed RPC ships without a catalog entry → the
# per-RPC authz middleware returns "catalog: no entry for method" (AUTHZ_DENIED) at
# runtime. Requires a full monorepo checkout (in-repo proto/) + buf. The IAM embedded
# copy MUST byte-match this one (single source of truth) — also asserted here.
IAM_CATALOG := ../services/iam/internal/apps/kacho/seed/embedded/permission_catalog.json
.PHONY: permission-catalog-check
permission-catalog-check:
	./scripts/gen-permission-catalog.sh $(PERMISSION_CATALOG_BUILD)
	@diff -u $(PERMISSION_CATALOG_TARGET) $(PERMISSION_CATALOG_BUILD) \
	  || { echo "::error::permission_catalog.json (api-gateway embed) is STALE vs proto — run 'make permission-catalog-apply' + sync IAM copy, then commit"; exit 1; }
	@if [ -f $(IAM_CATALOG) ]; then diff -u $(IAM_CATALOG) $(PERMISSION_CATALOG_TARGET) \
	  || { echo "::error::IAM embedded catalog drifted from api-gateway copy — re-sync both from the regenerated catalog"; exit 1; }; fi
	@echo "permission catalog is complete and both copies are in sync."

# Регенерация REST route-table из `google.api.http`-аннотаций proto в
# $(REST_ROUTE_TABLE_BUILD) + diff против текущего рантайм-файла.
# Требует полный checkout монорепо (внутрирепозиторное proto/) + buf.
rest-route-table:
	./scripts/gen-rest-route-table.sh $(REST_ROUTE_TABLE_BUILD)
	@echo "--- diff $(REST_ROUTE_TABLE_TARGET) (committed) vs regenerated ---"
	@diff -u $(REST_ROUTE_TABLE_TARGET) $(REST_ROUTE_TABLE_BUILD) || true

# Принять регенерированную таблицу как новый committed source-of-truth.
rest-route-table-apply:
	./scripts/gen-rest-route-table.sh $(REST_ROUTE_TABLE_TARGET)
	@echo "Applied regenerated route table to $(REST_ROUTE_TABLE_TARGET)."

# CI GATE: fail if the committed route table is STALE vs the proto HTTP surface.
# Catches the class of regression where a new/renamed REST binding ships without a
# route entry → the authz middleware never maps the REST path to its FQN and every
# call to that endpoint is denied (e.g. the 6 registry Repository routes that had
# drifted out). Requires a full monorepo checkout (in-repo proto/) + buf.
.PHONY: rest-route-table-check
rest-route-table-check:
	./scripts/gen-rest-route-table.sh $(REST_ROUTE_TABLE_BUILD)
	@diff -u $(REST_ROUTE_TABLE_TARGET) $(REST_ROUTE_TABLE_BUILD) \
	  || { echo "::error::rest_route_table_gen.go is STALE vs proto — run 'make rest-route-table-apply', then commit"; exit 1; }
	@echo "REST route table is complete and in sync with proto."
