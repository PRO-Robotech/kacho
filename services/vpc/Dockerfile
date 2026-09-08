# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

FROM --platform=$BUILDPLATFORM mirror.gcr.io/library/golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

# Single-repo build: зависимости (kacho-corelib, kaname, kacho-geo) тянутся
# как versioned-модули из GitHub (go.mod без replace), build-context — этот репо.
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

# СЕТЕВАЯ ПОВЕРХНОСТЬ СБОРКИ РАВНА ЕЁ ГРАФУ ИМПОРТОВ.
#
# Здесь стоял `go mod download` без аргументов. Он тянет модули, ОБЪЯВЛЕННЫЕ в
# go.mod (142 на момент правки), тогда как компилируется только импортируемое:
# по образам этого дерева — от 27 до 82 модулей. То есть от 60 до 115 модулей
# на каждый образ загружались, чтобы не попасть ни в один бинарь.
#
# Цена не в трафике, а в хрупкости: каждая лишняя загрузка — ещё один способ
# уронить сборку по причине, к продукту отношения не имеющей. Так и вышло —
# образ упал на модуле поставщика инструментов инфраструктуры, которого его
# бинарь не компилирует (hashicorp в графе импортов ЛЮБОГО из восьми: 0).
#
# `go list -deps` на ТЕХ ЖЕ пакетах, что собираются ниже, разрешает и укладывает
# в кэш ровно их транзитивные модули. Список намеренно повторяет строку сборки:
# разойдясь, он не сломает сборку (недостающее догрузит `go build`), но
# расхождение видно глазом в соседних строках.
RUN --mount=type=cache,target=/go/pkg/mod \
    go list -deps ./services/vpc/cmd/vpc ./services/vpc/cmd/migrator >/dev/null
# Два независимых binary в одном образе:
# kacho-vpc — gRPC API-сервер (только `serve`).
# kacho-migrator — CLI миграций (cobra: up|down|status|create), используется
# init-container'ом перед стартом основного pod'а.
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /kacho-vpc ./services/vpc/cmd/vpc \
 && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /kacho-migrator ./services/vpc/cmd/migrator

FROM mirror.gcr.io/library/alpine:3.24
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates
COPY --from=builder /kacho-vpc /usr/local/bin/kacho-vpc
COPY --from=builder /kacho-migrator /usr/local/bin/kacho-migrator
# ПРОВЕНАНС ОБРАЗА — из какого дерева он собран. Клеймо и файл берут величину из
# ОДНОГО `ARG`, поэтому разойтись не могут. Файл нужен потому, что у РАБОТАЮЩЕГО
# контейнера клеймо не прочесть ничем, кроме демона хоста или реестра, — а у
# управляемого кластера нет ни того, ни другого, тогда как `kubectl exec … cat`
# есть всегда. Пустое умолчание намеренно: сборка руками ревизии знать не обязана,
# а цель провенанса скажет словами, что величина не проставлена.
# Разбор целиком — deploy/scripts/stand-provenance.sh; что величину проставляет
# КАЖДАЯ сборка дерева — deploy/stand_provenance_declaration_test.go.
ARG KACHO_IMAGE_REVISION=""
ARG KACHO_IMAGE_VERSION=""
LABEL org.opencontainers.image.revision="$KACHO_IMAGE_REVISION" \
      org.opencontainers.image.version="$KACHO_IMAGE_VERSION"
RUN mkdir -p /etc/kacho && printf '%s\n' "$KACHO_IMAGE_REVISION" > /etc/kacho/image-revision

USER 65532
ENTRYPOINT ["/usr/local/bin/kacho-vpc"]
