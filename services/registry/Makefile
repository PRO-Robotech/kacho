# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

BINARY         := kacho-registry
CMD            := ./cmd/kacho-registry
MIGRATOR_BIN   := kacho-migrator
MIGRATOR_CMD   := ./cmd/migrator
IMAGE          := kacho-registry:dev

.PHONY: build build-migrator test test-short vet lint docker audit-list-filter

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
	$(MAKE) -C ../.. test-service SVC=registry

test-short:
	$(MAKE) -C ../.. test-service-short SVC=registry

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
	docker build -f Dockerfile -t $(IMAGE) .

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
# Вывод начинается с ПЕРЕПИСИ осмотренного: сколько файлов handler'а и composition
# root'а прочитано, сколько List-RPC найдено, сколько судимо и сколько осталось без
# приговора. Раньше цель печатала ровно `audit-list-filter: OK` — по этой строке
# нельзя было отличить «пять RPC судили» от «дерева не нашли вовсе». Ноль
# прочитанного теперь отказ, а не успех.
#
# Команда набирается как `make -C services/registry audit-list-filter` (корневого
# Makefile в репо нет). Ровно в этой форме её зовёт CI: .github/workflows/ci.yaml,
# job authz-artifacts. Что CI её действительно зовёт — проверяется тестом
# tools/auditlistfilter/ci_wiring_test.go, чтобы утверждение не протухло молча.
# Тот же анализ гоняется внутри обычного `go test ./services/registry/...`
# (tools/auditlistfilter/audit_test.go, TestAnalyzeRealTree), а сама эта команда —
# в tools/auditlistfilter/census_test.go, с инъекциями на копиях реального дерева.
audit-list-filter:
	@go run ./tools/auditlistfilter/cmd/audit-list-filter

# dataplane-e2e-selftest — самопроверка ЧЕСТНОСТИ вердикта сквозного прогона плоскости
# данных. Стенда не требует: гоняет настоящий dataplane-e2e.sh против заглушки на
# петлевом интерфейсе и утверждает КОД ВОЗВРАТА в обе стороны — исправный поток даёт 0,
# а отказ на инициализации загрузки (снимающий с прогона пять шагов) даёт НЕ 0.
.PHONY: dataplane-e2e-selftest
dataplane-e2e-selftest:
	@bash tests/newman/scripts/dataplane-e2e-selftest.sh

# newman-assertion-census — гейт: ни один шаг суиты не имеет права исполнить запрос и
# не спросить ничего. Стенда не требует: исполняет скрипты сгенерированных коллекций
# против подставленных ответов и СЧИТАЕТ исполненные утверждения. Несёт собственную
# проверку предпосылки (инъекция дефекта + две законные конструкции той же формы).
.PHONY: newman-assertion-census
newman-assertion-census:
	@cd tests/newman && node scripts/selftest-assertions.js

.PHONY: migrate-up migrate-down migrate-status
migrate-up: build-migrator
	KACHO_REGISTRY_DB_PASSWORD=secret bin/$(MIGRATOR_BIN) up

migrate-down: build-migrator
	KACHO_REGISTRY_DB_PASSWORD=secret bin/$(MIGRATOR_BIN) down

migrate-status: build-migrator
	KACHO_REGISTRY_DB_PASSWORD=secret bin/$(MIGRATOR_BIN) status
