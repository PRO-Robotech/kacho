#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# newman-e2e.sh — REPRODUCIBLE newman e2e flow against the running dev stand.
#
# Replaces the manual "seed tokens by hand" path with a deterministic,
# committed flow:
#   1. port-forward api-gateway public (:18080) + internal-rest (:18081) +
#      kacho-iam-internal (:19091)
#   2. seed auth fixtures via tests/authz-fixtures/setup.sh (idempotent):
#      mints non-expiring dev JWTs, upserts users, accounts/projects, grants
#      cluster-admin (SQL backdoor), seeds VPC networks, and PATCHES every
#      service's newman environment (existingProjectId, jwt*, …).
#   3. run the requested service's newman collection(s): {{baseUrl}} → :18080,
#      {{internalBaseUrl}} → :18081 (Internal*-RPC живут ТОЛЬКО там — ban #6).
#   4. tear down the port-forwards on exit.
#
# Usage (after `make dev-up`):
#   make e2e-newman SVC=vpc                      # whole vpc suite
#   make e2e-newman SVC=vpc COLLECTION=internal-network
#   ./scripts/newman-e2e.sh vpc internal-network
#
# Prereqs (fail-fast): kubectl, python3, newman, grpcurl. The flow is
# environment-agnostic — same script seeds + runs in CI and locally.
set -euo pipefail

SVC="${1:-${SVC:-vpc}}"
COLLECTION="${2:-${COLLECTION:-}}"
NS="${SETUP_NS:-kacho}"
DEV_SECRET="${DEV_SECRET:-kacho-dev-jwt-secret-2026}"
GW_PORT="${GW_PORT:-18080}"
GW_INTERNAL_PORT="${GW_INTERNAL_PORT:-18081}"   # api-gateway internal-rest :8081 (Internal*-RPC)
IAM_INTERNAL_PORT="${IAM_INTERNAL_PORT:-19091}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Монорепа: deploy/scripts → корень репо на два уровня выше. Раскладка — services/<svc>,
# кроме api-gateway (gateway/). Раньше было
# "$WORKSPACE_DIR/project/kacho-$SVC/tests/newman" — polyrepo-путь к sibling-репо.
# authz-фикстуры тоже переехали: были в kacho-workspace/tests/, теперь tests/ монорепы.
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
if [ "$SVC" = "api-gateway" ]; then
  NEWMAN_DIR="$REPO_ROOT/gateway/tests/newman"
else
  NEWMAN_DIR="$REPO_ROOT/services/$SVC/tests/newman"
fi

for tool in kubectl python3 newman grpcurl; do
  command -v "$tool" >/dev/null 2>&1 || { echo "FATAL: '$tool' not found in PATH" >&2; exit 1; }
done
[ -d "$NEWMAN_DIR" ] || { echo "FATAL: no newman dir for SVC=$SVC ($NEWMAN_DIR)" >&2; exit 1; }

PF_PIDS=()
TMP_DIRS=()
# Чистим и порт-форварды, и временные каталоги: в них лежит ПРИВАТНЫЙ КЛЮЧ
# client-cert'а — оставлять его в /tmp после прогона нельзя.
cleanup() {
  for p in "${PF_PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
  for d in "${TMP_DIRS[@]:-}"; do [ -n "$d" ] && rm -rf "$d"; done
}
trap cleanup EXIT

echo "[e2e] port-forward api-gateway :$GW_PORT (public) / :$GW_INTERNAL_PORT (internal-rest) + kacho-iam-internal :$IAM_INTERNAL_PORT"
kubectl -n "$NS" port-forward svc/api-gateway "$GW_PORT:8080" >/tmp/e2e-pf-gw.log 2>&1 &
PF_PIDS+=($!)
# internal-rest (:8081) — ОТДЕЛЬНЫЙ листенер для Internal*-RPC. На публичном :8080 их
# нет и быть не должно (ban #6: Internal.* не публикуется на external endpoint), поэтому
# коллекции internal-* обязаны ходить сюда через {{internalBaseUrl}}, иначе получают
# закономерный 404. iam-набор так и делает; vpc-набор — ещё нет (см. README/issue).
kubectl -n "$NS" port-forward svc/api-gateway "$GW_INTERNAL_PORT:8081" >/tmp/e2e-pf-gw-internal.log 2>&1 &
PF_PIDS+=($!)
kubectl -n "$NS" port-forward svc/kacho-iam-internal "$IAM_INTERNAL_PORT:9091" >/tmp/e2e-pf-iam.log 2>&1 &
PF_PIDS+=($!)
sleep 4

# mTLS для grpcurl → kacho-iam-internal:9091.
#
# dev-стенд идёт с mtls.enabled=true (фаза 2 dev-up), поэтому internal-листенер iam
# требует client-cert: plaintext-grpcurl просто ВИСНЕТ на хендшейке (не падает внятно —
# «context deadline exceeded»), и setup.sh замирает на шаге «upserting test users».
# setup.sh это поддерживает через IAM_INTERNAL_GRPC_MTLS_CERT/_KEY (дефолт — plaintext,
# рассчитан на mTLS-off CI), но кто-то должен ему серт ДАТЬ. Достаём из секрета,
# который выпустил cert-manager: iam принимает любой client-cert, подписанный
# internal-CA (KACHO_IAM_INTERNAL_SERVER_MTLS_CLIENTCAFILES = ca.crt того же CA).
MTLS_ENV=()
if kubectl -n "$NS" get secret api-gateway-client-tls >/dev/null 2>&1; then
  CERT_DIR="$(mktemp -d)"; TMP_DIRS+=("$CERT_DIR")
  kubectl -n "$NS" get secret api-gateway-client-tls -o jsonpath='{.data.tls\.crt}' | base64 -d > "$CERT_DIR/client.crt"
  kubectl -n "$NS" get secret api-gateway-client-tls -o jsonpath='{.data.tls\.key}' | base64 -d > "$CERT_DIR/client.key"
  chmod 600 "$CERT_DIR"/*
  # setup.sh ходит grpcurl'ом с -insecure (server-cert не пинится), поэтому нужны
  # только client cert/key — CA не передаём.
  MTLS_ENV=(IAM_INTERNAL_GRPC_MTLS_CERT="$CERT_DIR/client.crt"
            IAM_INTERNAL_GRPC_MTLS_KEY="$CERT_DIR/client.key")
  echo "[e2e] iam-internal: mTLS client-cert взят из secret/api-gateway-client-tls"
else
  echo "[e2e] iam-internal: mTLS-секрета нет — grpcurl пойдёт plaintext (mTLS-off стенд)"
fi

echo "[e2e] seeding auth fixtures (idempotent) + patching newman envs"
env BASE_URL="http://localhost:$GW_PORT" \
IAM_INTERNAL_GRPC="localhost:$IAM_INTERNAL_PORT" \
DEV_SECRET="$DEV_SECRET" PATCH_ENV=true SETUP_NS="$NS" \
"${MTLS_ENV[@]}" \
  bash "$REPO_ROOT/tests/authz-fixtures/setup.sh"

# nlb EXTERNAL suites auto-allocate a public VIP + self-provision a zonal external
# vpc Address; both resolve GetDefaultForZone(zone, EXTERNAL_PUBLIC) → need a DEFAULT
# EXTERNAL_PUBLIC AddressPool in the zone. seed-ipam is a deliberate NOOP, so provision
# it here (idempotent, best-effort) via the already-up internal-rest port-forward.
# Only for nlb — no other suite needs the external pool. `|| true`: a failure degrades
# to the pre-seed behaviour (external-create cases red → whitelist), never aborts the run.
if [ "$SVC" = "nlb" ]; then
  echo "[e2e] seeding nlb external-VIP AddressPool (idempotent, best-effort)"
  SEED_JWT=$(python3 "$REPO_ROOT/tests/authz-fixtures/setup-jwt.py" --secret "$DEV_SECRET" --exp-hours 24 --bulk 2>/dev/null \
    | python3 -c 'import json,sys; print(json.load(sys.stdin).get("jwtBootstrap",""))' 2>/dev/null || true)
  env BASE_URL="http://localhost:$GW_PORT" \
      INTERNAL_BASE_URL="http://localhost:$GW_INTERNAL_PORT" \
      JWT="$SEED_JWT" \
    bash "$REPO_ROOT/deploy/scripts/seed-nlb-fixtures.sh" || true
fi

echo "[e2e] regenerating newman collections"
( cd "$NEWMAN_DIR" && python3 scripts/gen.py >/dev/null )

echo "[e2e] running newman (SVC=$SVC COLLECTION=${COLLECTION:-<all>})"
cd "$NEWMAN_DIR"
if [ -n "$COLLECTION" ]; then
  newman run "collections/${COLLECTION}.postman_collection.json" \
    -e environments/local.postman_environment.json \
    --env-var "baseUrl=http://localhost:$GW_PORT" \
    --env-var "internalBaseUrl=http://localhost:$GW_INTERNAL_PORT" \
    --delay-request 15 --reporters cli
else
  # run.sh НЕ читает BASE_URL/INTERNAL_BASE_URL из окружения — значения он берёт только
  # из env-файла, а всё неизвестное в argv пробрасывает в newman как есть (массив EXTRA).
  # Поэтому передаём --env-var через argv, а не через env: иначе {{internalBaseUrl}}
  # остаётся пустым и Internal*-шаги молча уходят на публичный порт → 404
  # (internal-pool: 78/0 в одиночном прогоне, но 62/56 в полном — ровно этот разрыв).
  ./scripts/run.sh --service "" --delay 15 \
    --env-var "baseUrl=http://localhost:$GW_PORT" \
    --env-var "internalBaseUrl=http://localhost:$GW_INTERNAL_PORT"
fi
