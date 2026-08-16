# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# Multi-stage cross-compile build для kacho-registry (api-server + migrator).
#
# NOTE (build-context): go.mod держит `replace github.com/PRO-Robotech/kacho-corelib
# => ../kacho-corelib` (локальная polyrepo-разработка). Standalone `docker build`
# из этого репо требует, чтобы внутренние зависимости резолвились: либо переключить
# go.mod на GitHub-версии (публикационная фаза), либо собирать из parent-context с
# COPY ../kacho-corelib и go.work. По умолчанию цель — single-repo build с
# versioned-модулями (COPY . . + go mod download).
FROM --platform=$BUILDPLATFORM mirror.gcr.io/library/golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

COPY . .
# КЭШ КОМПИЛЯЦИИ — ОБЩИЙ НА ВСЕ ОБРАЗЫ ЭТОГО МОДУЛЯ.
#
# Восемь образов компилируют ОДИН И ТОТ ЖЕ модуль: 3995 собранных пакетов
# суммарно против 879 в объединении, то есть 78% работы — повторная. Кэш
# buildkit принадлежит builder'у, а не образу, поэтому второй и далее образы
# на той же машине берут готовое.
#
# ЭТО НЕ КЛАСС «КЭШ СОХРАНИЛ НЕУДАЧУ»: кэш компиляции Go контент-адресуем —
# недописанная или битая запись не находится по хэшу входов и просто вызывает
# перекомпиляцию. Гейтить сохранение по исходу шага здесь нечего.
#
# Директивы `# syntax=` намеренно нет: встроенный frontend buildkit `--mount`
# понимает (проверено), а `docker/dockerfile:1` — движущийся тег, то есть версия
# менялась бы без правки дерева, плюс сетевая тяга на каждом builder'е. Сборка
# со ВЫКЛЮЧЕННЫМ buildkit падает громко и по имени («requires BuildKit»).
RUN --mount=type=cache,target=/go/pkg/mod go mod download
# Два независимых бинаря в одном образе:
#   kacho-registry — control-plane gRPC-сервер (`serve`).
#   kacho-migrator — CLI миграций (up|down|status); запускается deploy-init-контейнером.
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /kacho-registry ./services/registry/cmd/kacho-registry \
 && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /kacho-migrator ./services/registry/cmd/migrator

FROM mirror.gcr.io/library/alpine:3.24
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates
COPY --from=builder /kacho-registry /usr/local/bin/kacho-registry
COPY --from=builder /kacho-migrator /usr/local/bin/kacho-migrator
USER 65532
ENTRYPOINT ["/usr/local/bin/kacho-registry"]
CMD ["serve"]
