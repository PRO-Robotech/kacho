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
        rest-route-table rest-route-table-apply \
        domain-generation-check domain-generation-inject \
        catalog-splice-check catalog-splice-inject

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

# ЗДЕСЬ СТОЯЛА СВЕРКА ДВУХ ВШИТЫХ КОПИЙ КАТАЛОГА ПРАВ — СНЯТА ВМЕСТЕ СО ВТОРОЙ.
#
# Копий было две: у края и у службы доступа. Служба вынесена отдельным продуктом,
# её копия уехала вместе с каталогом, и в этом дереве осталась ОДНА. Сверять
# нечего не потому, что решили не сверять, а потому, что второго операнда нет.
#
# Цель называла два законных исхода, и выбран второй из них: снять сверку вместе
# с копией. Первый — переопределить операнд на путь в соседнем клоне — отвергнут
# замером формы, а не вкусом: вердикт зависел бы от того, выложен ли рядом чужой
# репозиторий, то есть «сверено» стало бы неотличимо от «нечего было сверять» у
# всякого, у кого клона нет. Ровно тот класс, ради которого цель и заводилась
# безусловной.
#
# ЧТО ПРИ ЭТОМ УТРАЧЕНО, названо числом: расхождение копии края с копией службы
# больше не ловится НИЧЕМ в этом дереве — обе стороны равенства обязаны лежать в
# одном дереве, а здесь их одна. Свойство «каталог края порождён из аннотаций»
# держится по-прежнему (permission-catalog-check); свойство «служба исполняет ту
# же копию» стало предметом её репозитория.
#
# КЛАСС, ради которого цель была отдельной, НЕ утрачен: гейт
# internal/repohygiene/catalogcopyparity_test.go держит его по ВСЕМУ дереву — ни
# одна сверка ни в одном Makefile не смеет исполняться условно по наличию файла.
# Он читает дерево, а не эту цель, и от её снятия не слепнет.

# CI GATE: fail if the embedded catalog is STALE vs the proto surface. Catches the
# class of regression where a new/renamed RPC ships without a catalog entry → the
# per-RPC authz middleware returns "catalog: no entry for method" (AUTHZ_DENIED) at
# runtime. Requires a full monorepo checkout (in-repo proto/) + buf.
#
# Копия в этом дереве ОДНА — второй операнд уехал вместе с вынесенной службой
# доступа (см. блок выше). Требование «обе копии байт-в-байт» здесь больше не
# утверждается: утверждать его нечем, а утверждение без предмета читалось бы как
# исполняемое.
.PHONY: permission-catalog-check
permission-catalog-check:
	./scripts/gen-permission-catalog.sh $(PERMISSION_CATALOG_BUILD)
	@diff -u $(PERMISSION_CATALOG_TARGET) $(PERMISSION_CATALOG_BUILD) \
	  || { echo "::error::permission_catalog.json (api-gateway embed) is STALE vs proto — run 'make permission-catalog-apply', then commit"; exit 1; }
	@echo "permission catalog is complete."

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

# CI GATE разреза на отдельные продукты: порождение каталога прав и таблицы
# маршрутов ДЛЯ ОБЪЯВЛЕННОГО ПЕРЕЧНЯ ДОМЕНОВ выполнимо вне дерева монорепо и даёт
# ПОБАЙТОВО то же, что даёт порождение по всему дереву. Ловит класс, при котором
# вынесенный в свой репозиторий домен не может породить своего каталога — а без
# записи каталога средний слой отвечает «catalog: no entry for method», то есть
# отказом в доступе в рантайме. Коды: 0 — находок нет · 1 — находка · 2 — предмета
# нет (нет buf/go/дерева контрактов).
#
# ЗОВЁТ ЕЁ КОНВЕЙЕР — шаг «разрез …» в job `permission-catalog · rest-route-table`
# (.github/workflows/ci.yaml), рабочим каталогом gateway. До kacho#1110 не звал
# НИКТО: цель была объявлена, названа здесь CI-гейтом и не исполнялась ни разу.
# Провязка залочена internal/repohygiene/domaingenerationwiring_test.go — снимут
# шаг, и цель останется исполнимой, перестав исполняться, причём молча.
domain-generation-check:
	./scripts/check-domain-generation.sh

# Доказательство, что гейт выше СПОСОБЕН упасть: по одной оси за раз дефект
# возвращается, рядом стоит законный близнец той же формы. Правя гейт или
# генераторы, зови эту цель РУКАМИ — способность падать в диффе не видна.
domain-generation-inject:
	./scripts/inject-domain-generation-defects.sh

# CI GATE склейки (kacho#1110, вторая половина): каталог прав и таблица
# маршрутов, СОБРАННЫЕ ИЗ ДВУХ ЧАСТЕЙ (платформа `kacho` + служба `kaname`),
# дают ПОБАЙТОВО то же множество записей, что вшитые артефакты, собранные по
# всему дереву. `domain-generation-check` выше доказывает, что доля службы
# порождается в одиночку; эта цель доказывает другое — что доля службы,
# сложенная с долей платформы, восстанавливает вшитый каталог без пропуска и
# без лишнего. Пропавшая при склейке запись — тот же `catalog: no entry for
# method` → AUTHZ_DENIED, но на КОНКРЕТНОМ RPC, независимо от прав.
#
# ЗОВЁТ ЕЁ КОНВЕЙЕР — шаг «склейка …» в job `permission-catalog · rest-route-table`
# (.github/workflows/ci.yaml), рабочим каталогом gateway. Провязка залочена
# internal/repohygiene/catalogsplicewiring_test.go.
catalog-splice-check:
	./scripts/check-catalog-splice.sh

# Доказательство, что гейт склейки СПОСОБЕН упасть — на НАСТОЯЩЕЙ пропаже
# домена из дерева контрактов, не только на своих внутренних самотестах
# (S6/S7). Правя гейт или раскладку доменов, зови эту цель РУКАМИ.
catalog-splice-inject:
	./scripts/inject-catalog-splice-defects.sh
