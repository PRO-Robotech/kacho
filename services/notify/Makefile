# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

BINARY := kacho-notify
CMD    := ./cmd/notify
IMAGE  := kacho-notify:dev

# Ревизия дерева уезжает в образ аргументом сборки (IMAGE_BUILD_ARGS).
include ../../provenance.mk

.PHONY: build test test-short vet lint docker

build:
	CGO_ENABLED=0 go build -o bin/$(BINARY) $(CMD)

# Каноничная команда прогона — в КОРНЕВОМ Makefile: у флагов и бюджета одна истина.
test:
	$(MAKE) -C ../.. test-service SVC=notify

test-short:
	$(MAKE) -C ../.. test-service-short SVC=notify

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
