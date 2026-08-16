FROM --platform=$BUILDPLATFORM mirror.gcr.io/library/golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

# Single-repo build: зависимости (kacho-corelib, kacho-iam, kacho-geo, kacho-vpc) —
# versioned-модули с GitHub (go.mod без replace), build-context — этот репо.
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
# Два независимых binary в одном образе:
# kacho-compute  — gRPC API-сервер (только `serve`; не несёт миграции).
# kacho-migrator — CLI миграций ({up|down|status}), используется init-container'ом
# перед стартом основного pod'а (least-privilege: serve-образ не может менять схему).
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /kacho-compute ./services/compute/cmd/compute \
 && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /kacho-migrator ./services/compute/cmd/migrator

FROM mirror.gcr.io/library/alpine:3.24
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates
COPY --from=builder /kacho-compute /usr/local/bin/kacho-compute
COPY --from=builder /kacho-migrator /usr/local/bin/kacho-migrator
USER 65532
ENTRYPOINT ["/usr/local/bin/kacho-compute"]
