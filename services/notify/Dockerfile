# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

FROM --platform=$BUILDPLATFORM mirror.gcr.io/library/golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

# Сборка монорепо: единый Go-модуль github.com/PRO-Robotech/kacho, контекст —
# КОРЕНЬ монорепо (как у соседних служб; разбор кэша и сетевой поверхности
# сборки — в services/geo/Dockerfile, здесь не повторяется).
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    go list -deps ./services/notify/cmd/notify >/dev/null

# Один бинарь: kacho-notify (`serve`). Накатчика миграций в образе нет — у
# notify в этой под-фазе нет цепочки миграций: схему `kacho_notify` и её
# накат заводит полоса базы (NTF-1 N7), вместе со своим бинарём.
#
# Штамп сборки — из тех же аргументов, что и клеймо образа ниже; объявлены
# в этой ступени повторно, потому что область видимости аргумента — ступень.
ARG OCI_IMAGE_REVISION=""
ARG OCI_IMAGE_VERSION=""
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags "-X main.buildVersion=$OCI_IMAGE_VERSION -X main.buildCommit=$OCI_IMAGE_REVISION" -o /kacho-notify ./services/notify/cmd/notify

FROM mirror.gcr.io/library/alpine:3.24
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates
COPY --from=builder /kacho-notify /usr/local/bin/kacho-notify
# Провенанс образа: клеймо и файл берут величину из ОДНОГО аргумента
# (разбор — deploy/scripts/stand-provenance.sh).
ARG OCI_IMAGE_REVISION=""
ARG OCI_IMAGE_VERSION=""
LABEL org.opencontainers.image.revision="$OCI_IMAGE_REVISION" \
      org.opencontainers.image.version="$OCI_IMAGE_VERSION"
RUN mkdir -p /etc/kacho && printf '%s\n' "$OCI_IMAGE_REVISION" > /etc/kacho/image-revision

USER 65532
ENTRYPOINT ["/usr/local/bin/kacho-notify"]
CMD ["serve"]
