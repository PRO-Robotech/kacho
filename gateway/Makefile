# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

BINARY := api-gateway
CMD    := ./cmd/api-gateway
IMAGE  := kacho-api-gateway:dev

# Ревизия дерева уезжает в образ аргументом сборки (IMAGE_BUILD_ARGS).
# Объявление одно на дерево — см. разбор в самом файле.
include ../provenance.mk

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

# Каноничная команда живёт в КОРНЕВОМ Makefile — здесь только делегация: у флагов
# и бюджета прогона должна быть ОДНА истина. Собственный `-timeout` в этом файле
# ни с CI, ни с соседними сервисами не сверялся ничем и разъехался (300s/900s
# вразнобой, и там где мало — молча недостижимо).
test:
	$(MAKE) -C .. test-service SVC=gateway

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
GOLANGCI_LINT_CACHE := $(abspath $(CURDIR)/..)/.cache/golangci-lint
export GOLANGCI_LINT_CACHE

lint:
	golangci-lint run ./...

docker:
	cd .. && docker build $(IMAGE_BUILD_ARGS) -f kacho-api-gateway/Dockerfile -t $(IMAGE) .

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

IAM_CATALOG := ../services/iam/internal/apps/kacho/seed/embedded/permission_catalog.json

# Сверка ДВУХ вшитых копий каталога прав — отдельная цель, и исполняется она
# БЕЗУСЛОВНО.
#
# Здесь стояло `if [ -f $(IAM_CATALOG) ]; then diff …; fi`, и это было
# послаблением, которое НЕ ИСТЕЧЁТ САМО: пока копия iam на месте, обёртка ничего
# не пропускает, но в день, когда копия уедет — а её уезд ЗАПЛАНИРОВАН выносом
# iam отдельным продуктом, — условие становится ложным навсегда. Сверка перестаёт исполняться вовсе, а цель остаётся зелёной и
# неотличимой от исправной. Цена расхождения копий не косметическая: RPC без
# записи в каталоге даёт `catalog: no entry for method` → AUTHZ_DENIED на КАЖДЫЙ
# вызов, независимо от выданных прав.
#
# Зеркальная цель у iam (`make -C services/iam sync-permission-catalog`)
# отказывала явно с самого начала. Односторонняя строгость на одном шве и была тем
# перекосом, по которому класс виден: один и тот же вопрос две стороны решали
# по-разному, и никто этого не решал.
#
# ОТДЕЛЬНАЯ цель, а не строка внутри permission-catalog-check, — намеренно:
# сравнение двух закоммиченных файлов не требует ни buf, ни proto-дерева, поэтому
# исполнимо и там, где генерация недоступна, а его поведение проверяется
# ЗАПУСКОМ (internal/repohygiene/catalogcopyparity_test.go), а не прочтением
# рецепта. Тот же гейт держит КЛАСС обёртки по всему дереву: ни одна сверка ни в
# одном Makefile не смеет исполняться условно по наличию файла.
#
# После выноса iam у этой цели ровно два законных исхода, и оба требуют решения
# человека: переопределить операнд (`make … IAM_CATALOG=<путь к копии вынесенного
# iam>`) либо снять сверку вместе с копией. Третьего, молчаливого — «условие
# перестало выполняться» — больше нет.
#
# Переопределять операнд можно АРГУМЕНТОМ make, но не переменной окружения:
# присваивание жёсткое (`:=`), а не `?=`. С `?=` унаследованное из окружения
# значение молча увело бы сверку на чужой файл, а такое решение обязано быть
# заявлено явно.
.PHONY: permission-catalog-copies-in-sync
permission-catalog-copies-in-sync:
	@test -f "$(PERMISSION_CATALOG_TARGET)" || { echo "::error::нет копии края: $(PERMISSION_CATALOG_TARGET) — сверять не с чем. Нужен полный чекаут монорепо; пропустить сверку нельзя — расхождение копий даёт AUTHZ_DENIED на каждый вызов"; exit 1; }
	@test -f "$(IAM_CATALOG)" || { echo "::error::нет копии iam: $(IAM_CATALOG) — сверять не с чем. Нужен полный чекаут монорепо; если копия уехала с выносом iam отдельным продуктом — переопредели IAM_CATALOG=<путь> либо сними сверку вместе с копией, но НЕ оставляй её пропускаемой"; exit 1; }
	@diff -u "$(IAM_CATALOG)" "$(PERMISSION_CATALOG_TARGET)" \
	  || { echo "::error::IAM embedded catalog drifted from api-gateway copy — re-sync both from the regenerated catalog"; exit 1; }
	@entries=$$(grep -c '"fqn"' "$(PERMISSION_CATALOG_TARGET)" || true); \
	  test "$$entries" -gt 0 || { echo "::error::в каталоге ноль записей ($(PERMISSION_CATALOG_TARGET)) — копии равны, но сверять было НЕЧЕГО; пустой каталог неотличим от сошедшихся копий"; exit 1; }; \
	  echo "перепись: копий каталога сверено 2 · записей $$entries · байт $$(wc -c < "$(PERMISSION_CATALOG_TARGET)") · побайтово равны"

# CI GATE: fail if the embedded catalog is STALE vs the proto surface. Catches the
# class of regression where a new/renamed RPC ships without a catalog entry → the
# per-RPC authz middleware returns "catalog: no entry for method" (AUTHZ_DENIED) at
# runtime. Requires a full monorepo checkout (in-repo proto/) + buf. The IAM embedded
# copy MUST byte-match this one (single source of truth) — asserted by the
# permission-catalog-copies-in-sync prerequisite above, unconditionally.
.PHONY: permission-catalog-check
permission-catalog-check: permission-catalog-copies-in-sync
	./scripts/gen-permission-catalog.sh $(PERMISSION_CATALOG_BUILD)
	@diff -u $(PERMISSION_CATALOG_TARGET) $(PERMISSION_CATALOG_BUILD) \
	  || { echo "::error::permission_catalog.json (api-gateway embed) is STALE vs proto — run 'make permission-catalog-apply' + sync IAM copy, then commit"; exit 1; }
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
