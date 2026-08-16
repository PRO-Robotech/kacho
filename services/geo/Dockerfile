# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

FROM --platform=$BUILDPLATFORM mirror.gcr.io/library/golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

# Monorepo build: единый Go-модуль github.com/PRO-Robotech/kacho — proto/corelib НЕ
# внешние зависимости, а пакеты этого же модуля. Build-context — КОРЕНЬ монорепы.
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
#   kacho-geo      — gRPC API-сервер (`serve`).
#   kacho-migrator — CLI миграций (up|down|status); запускается deploy-init-контейнером.
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /kacho-geo ./services/geo/cmd/kacho-geo \
 && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /kacho-migrator ./services/geo/cmd/migrator

FROM mirror.gcr.io/library/alpine:3.24
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates
COPY --from=builder /kacho-geo /usr/local/bin/kacho-geo
COPY --from=builder /kacho-migrator /usr/local/bin/kacho-migrator
USER 65532
ENTRYPOINT ["/usr/local/bin/kacho-geo"]
CMD ["serve"]
