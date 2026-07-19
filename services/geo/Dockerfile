# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

FROM --platform=$BUILDPLATFORM mirror.gcr.io/library/golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

# Monorepo build: единый Go-модуль github.com/PRO-Robotech/kacho — proto/corelib НЕ
# внешние зависимости, а пакеты этого же модуля. Build-context — КОРЕНЬ монорепы.
COPY . .
RUN go mod download
# Два независимых бинаря в одном образе:
#   kacho-geo      — gRPC API-сервер (`serve`).
#   kacho-migrator — CLI миграций (up|down|status); запускается deploy-init-контейнером.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /kacho-geo ./services/geo/cmd/kacho-geo \
 && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /kacho-migrator ./services/geo/cmd/migrator

FROM mirror.gcr.io/library/alpine:3.24
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates
COPY --from=builder /kacho-geo /usr/local/bin/kacho-geo
COPY --from=builder /kacho-migrator /usr/local/bin/kacho-migrator
USER 65532
ENTRYPOINT ["/usr/local/bin/kacho-geo"]
CMD ["serve"]
