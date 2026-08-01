# Makefile — ЕДИНСТВЕННОЕ место, где записаны каноничные тестовые прогоны монорепо.
# На него ссылаются и конвейер (.github/workflows/ci.yaml), и человек, и Makefile'ы
# сервисов. Дублировать команду в другом месте — значит завести вторую истину,
# которая разъедется молча.
#
# ПОЧЕМУ ТАЙМАУТ ЗАДАН ЯВНО. `go test` без `-timeout` берёт СВОЁ умолчание — 600 с
# на пакет. Это произвольное умолчание инструмента, а не свойство продукта, и на
# этом дереве оно недостижимо by construction: два пакета поднимают Postgres в
# контейнере ПОД КАЖДЫЙ тест и накатывают на него всю цепочку миграций —
#
#   services/vpc/internal/repo            140 тестов, 199 подъёмов контейнера
#   services/nlb/internal/repo/kacho/pg   115 тестов,  86 подъёмов контейнера
#
# — поэтому «go test ./... -count=1» ронялся не нагрузкой, а арифметикой: под
# меньшим параллелизмом пакет занимает БОЛЬШЕ, а не меньше. Стоимость фикстуры
# (контейнер + миграции на тест) — отдельный долг со своим тикетом; таймаут её не
# лечит и не притворяется, что лечит: он лишь перестаёт выдавать чужое умолчание
# за вердикт о продукте.
#
# ЧТО СЧИТАЕТСЯ «ЗЕЛЁНЫМ СТВОЛОМ»: `make test` — тот же объём, что проверяет CI, и
# ничем не меньше. Разница только в раскладке: CI гонит сервисы матрицей (wall-clock
# = самый долгий), `make test` — по очереди (wall-clock = сумма). Одному сервису —
# `make test-integration SVC=<имя>`.

SHELL := bash
GO ?= go

# Юнит-прогон: `-short` отсекает пакеты с testcontainers (их гоняет test-integration).
# 5m хватает с запасом: без контейнеров всё дерево укладывается в десятки секунд.
UNIT_TIMEOUT ?= 5m

# Интеграционный прогон: бюджет НА ПАКЕТ. 25m выбраны по самому дорогому пакету
# (iam/internal/repo/kacho/pg — 398 тестов против testcontainers-Postgres, под
# -race в одиночку выбирает больше 15m), а не «на глаз».
INTEGRATION_TIMEOUT ?= 25m

# Сервисы, у которых есть интеграционные пакеты. Совпадает с матрицей CI.
SERVICES ?= iam vpc compute geo nlb storage registry

.PHONY: test test-unit test-integration test-service test-service-short help

## test — всё, что проверяет CI: юниты + интеграция по всем сервисам.
test: test-unit test-integration

## test-unit — юниты всего дерева под -race.
test-unit:
	$(GO) test ./... -race -short -count=1 -timeout $(UNIT_TIMEOUT)

## test-integration — интеграция. SVC=<сервис> для одного, иначе все по очереди.
##
## -p 1 ВНУТРИ сервиса: пакеты сериализуются. Под -race + Docker-contention
## параллельные testcontainers-пакеты голодают друг у друга ресурсы, и
## concurrent-кейсы (CAS/EXCLUDE/SKIP-LOCKED) флакают.
test-integration:
ifdef SVC
	@pkgs=$$($(GO) list ./services/$(SVC)/... | grep -E '/internal/(repo|clients)(/|$$)' || true); \
	if [ -z "$$pkgs" ]; then echo "нет integration-пакетов у $(SVC) — пропуск"; exit 0; fi; \
	echo "пакетов: $$(echo "$$pkgs" | wc -l)"; \
	echo "$$pkgs" | xargs $(GO) test -race -count=1 -timeout $(INTEGRATION_TIMEOUT) -p 1
else
	@set -e; for svc in $(SERVICES); do \
		echo "=== integration: $$svc ==="; \
		$(MAKE) --no-print-directory test-integration SVC=$$svc; \
	done
endif

## test-service — ВСЁ одного сервиса (юниты + интеграция). SVC обязателен.
## Сюда делегируют `make test` в services/<svc>/Makefile.
test-service:
	@test -n "$(SVC)" || { echo "нужен SVC=<сервис>"; exit 2; }
	$(GO) test ./$(SVC_PATH)/... -race -cover -count=1 -timeout $(INTEGRATION_TIMEOUT) -p 1

## test-service-short — то же без контейнеров.
test-service-short:
	@test -n "$(SVC)" || { echo "нужен SVC=<сервис>"; exit 2; }
	$(GO) test ./$(SVC_PATH)/... -race -cover -short -count=1 -timeout $(UNIT_TIMEOUT)

# SVC_PATH — где лежит сервис. gateway лежит в корне, остальные под services/.
SVC_PATH = $(if $(filter gateway,$(SVC)),gateway,services/$(SVC))

help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'
