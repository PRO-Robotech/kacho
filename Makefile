# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# Makefile — ЕДИНСТВЕННОЕ место, где записаны каноничные тестовые прогоны монорепо.
# На него ссылаются и конвейер (.github/workflows/ci.yaml), и человек, и Makefile'ы
# сервисов. Дублировать команду в другом месте — значит завести вторую истину,
# которая разъедется молча.
#
# ПОЧЕМУ ТАЙМАУТ И ПАРАЛЛЕЛИЗМ ЗАДАНЫ ЯВНО. `go test` без `-timeout` берёт СВОЁ
# умолчание — 600 с на пакет, а без `-p 1` запускает пакеты по числу ядер. На этом
# дереве 34 пакета поднимают Postgres в контейнере, поэтому оба умолчания вводят в
# заблуждение. Замерено на тихой машине, `-p 1`, `-count=1`:
#
#   vpc/internal/repo             410 с без -race — в 600 с укладывается;
#   nlb/internal/repo/kacho/pg    312 с без -race — тоже;
#   те же два пакета под -race при -p 12:  990 с и 1133 с — уже нет.
#
# И на `-p 12` контейнерные пакеты голодают друг у друга: pkg/outbox/drainer выдал
# 6 отказов по сроку за 155 с, а в одиночку проходит за 41 с. То есть красное на
# «том же» прогоне — свойство РАСКЛАДКИ, а не продукта, и умолчание инструмента в
# вердикт не годится. Поэтому бюджет и `-p` заданы здесь, в одном месте.
#
# Стоимость самой фикстуры снята там, где она была наибольшей: vpc/internal/repo и
# nlb/internal/repo/kacho/pg перешли на один Postgres на пакет + клон шаблона на
# тест (410 с → 19 с и 312 с → 13 с). Ещё 31 пакет поднимает контейнер на каждый
# тест — это открытый долг, и явный бюджет его не лечит и не притворяется, что
# лечит.
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

# Интеграционный прогон: бюджет НА ПАКЕТ. 25m — значение, которое CI уже нёс
# инлайном; выбрано оно было по самому дорогому пакету (iam/internal/repo/kacho/pg,
# см. историю в ci.yaml), а не «на глаз». Снижать его следует замером, а не
# ощущением: сейчас запас есть, но 34 пакета всё ещё поднимают контейнер.
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
##
## «НЕЧЕГО ЗАПУСКАТЬ» И «НЕ СМОГ СПРОСИТЬ» — РАЗНЫЕ ИСХОДЫ. Здесь стояло
## `go list … | grep … || true`, и `|| true` покрывал ОБА: сломанная сборка
## роняет `go list`, конвейер получает пустой список, печатает «нет
## integration-пакетов — пропуск» и выходит НУЛЁМ. Джоба зелёная, тестов
## выполнено ноль. Код `go list` поэтому читается отдельно от `grep`, у
## которого «ничего не нашлось» — законный исход (код 1).
test-integration:
ifdef SVC
	@set -o pipefail; \
	all=$$($(GO) list ./services/$(SVC)/...) || { \
	  echo "go list ./services/$(SVC)/... сорвался — состав пакетов НЕ ИЗМЕРЕН." >&2; \
	  echo "Это отказ, а не «нечего запускать»: пустой список здесь означал бы" >&2; \
	  echo "зелёную джобу с нулём выполненных тестов." >&2; exit 1; }; \
	if [ -z "$$all" ]; then echo "у $(SVC) не найдено НИ ОДНОГО пакета — обход пуст, это отказ" >&2; exit 1; fi; \
	pkgs=$$(printf '%s\n' "$$all" | grep -E '/internal/(repo|clients)(/|$$)'); \
	if [ -z "$$pkgs" ]; then echo "нет integration-пакетов у $(SVC) — пропуск (осмотрено пакетов: $$(printf '%s\n' "$$all" | wc -l))"; exit 0; fi; \
	echo "пакетов: $$(echo "$$pkgs" | wc -l) (из осмотренных $$(printf '%s\n' "$$all" | wc -l))"; \
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
