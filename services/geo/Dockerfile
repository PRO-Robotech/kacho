# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

FROM --platform=$BUILDPLATFORM mirror.gcr.io/library/golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

# Single-repo build: зависимости (kacho-proto, kacho-corelib) тянутся как
# versioned-модули из GitHub (go.mod без replace), build-context — этот репо.
COPY . .
RUN go mod download
# Два независимых бинаря в одном образе:
#   kacho-geo      — gRPC API-сервер (`serve`).
#   kacho-migrator — CLI миграций (up|down|status); запускается deploy-init-контейнером.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /kacho-geo ./cmd/kacho-geo \
 && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /kacho-migrator ./cmd/migrator

FROM mirror.gcr.io/library/alpine:3.20
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates
COPY --from=builder /kacho-geo /usr/local/bin/kacho-geo
COPY --from=builder /kacho-migrator /usr/local/bin/kacho-migrator
USER 65532
ENTRYPOINT ["/usr/local/bin/kacho-geo"]
CMD ["serve"]
