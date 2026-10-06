# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

BINARY := kacho-notify
CMD    := ./cmd/notify
IMAGE  := kacho-notify:dev

# Ревизия дерева уезжает в образ аргументом сборки (IMAGE_BUILD_ARGS).
include ../../provenance.mk

.PHONY: build test test-short integration vet lint docker bundle bundle-check

# Сборка шаблонов (NTF-1 Р7, NTF1-G17): каталоги шаблонов источников по
# таблице bundle/internal/assemble → копия, встраиваемая в notify
# (bundle/catalog), эталоны .eml (bundle/golden) и превью (bundle/preview).
# bundle пишет, bundle-check сверяет записанное с пересборкой и ничего не
# пишет: расхождение — код 1 с именем пространства и шаблона.
NOTIFYBUNDLE := go run ./bundle/notifybundle -root $(abspath $(CURDIR)/../..) -out $(CURDIR)/bundle

bundle:
	$(NOTIFYBUNDLE)

bundle-check:
	$(NOTIFYBUNDLE) -check

build:
	CGO_ENABLED=0 go build -o bin/$(BINARY) $(CMD)

# Каноничная команда прогона — в КОРНЕВОМ Makefile: у флагов и бюджета одна истина.
test:
	$(MAKE) -C ../.. test-service SVC=notify

test-short:
	$(MAKE) -C ../.. test-service-short SVC=notify

# Интеграция (настоящий Postgres): отбор и вердикт — у корневой цели
# test-integration; ноль исполненных проб — отказ (kacho#2915, Д92).
integration:
	$(MAKE) -C ../.. test-integration SVC=notify

vet:
	go vet ./...

# Кэш линтера — свой у рабочей копии (разбор — services/geo/Makefile;
# гейт — internal/repohygiene/lintverdict_test.go).
GOLANGCI_LINT_CACHE := $(abspath $(CURDIR)/../..)/.cache/golangci-lint
export GOLANGCI_LINT_CACHE

lint:
	golangci-lint run ./...

# Контекст сборки — корень монорепо: образу нужен единый go.mod, pkg/ и proto/.
docker:
	docker build $(IMAGE_BUILD_ARGS) -f Dockerfile -t $(IMAGE) ../..
