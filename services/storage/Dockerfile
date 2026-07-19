# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# Single-repo multi-stage build. kacho-proto / kacho-corelib resolve as VERSIONED
# modules (go.mod carries no `replace` — see .claude/rules/polyrepo.md), so the
# build context is THIS repo only. Build: `docker build -t kacho-storage:dev .`
FROM --platform=$BUILDPLATFORM mirror.gcr.io/library/golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

COPY . .
RUN go mod download
# Two independent binaries:
#   storage        — gRPC API server (`serve`).
#   kacho-migrator — migration CLI (up|down|status); deploy init-container.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /storage ./services/storage/cmd/storage \
 && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /kacho-migrator ./services/storage/cmd/migrator

FROM mirror.gcr.io/library/alpine:3.24
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates
COPY --from=builder /storage /usr/local/bin/storage
COPY --from=builder /kacho-migrator /usr/local/bin/kacho-migrator
USER 65532
ENTRYPOINT ["/usr/local/bin/storage"]
CMD ["serve"]
