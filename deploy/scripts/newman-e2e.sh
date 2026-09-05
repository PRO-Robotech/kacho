#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# newman-e2e.sh — REPRODUCIBLE newman e2e flow against the running dev stand.
#
# Replaces the manual "seed tokens by hand" path with a deterministic,
# committed flow:
#   1. port-forward api-gateway public (:18080) + internal-rest (:18081) +
#      kaname-internal (:19091)
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
GW_PORT="${GW_PORT:-18080}"
GW_INTERNAL_PORT="${GW_INTERNAL_PORT:-18081}"   # api-gateway internal-rest :8081 (Internal*-RPC)
# api-gateway EXTERNAL TLS listener :8443 (advertised as api.kacho.local:443). The ban-#6
# negatives address it here rather than by its advertised hostname: that name does not
# resolve on a developer box, adding it needs root, kind publishes only node:80, and the
# Ingress in front of it speaks GRPCS so every REST path through it answers 502. Ban #6 is
# about which routes the LISTENER serves, not about the name used to find it.
GW_TLS_PORT="${GW_TLS_PORT:-18443}"
IAM_INTERNAL_PORT="${IAM_INTERNAL_PORT:-19091}"
# Адреса ПОЛОСЫ ФАСАДА (#59, iam-token-facade-conformance). Кейсы IBT-* спрашивают
# сами слушатели — иначе «проверка подписи идёт через фасад» останется утверждением
# о конфигурации, а не о поведении.
IAM_JWKS_PORT="${IAM_JWKS_PORT:-19097}"         # iam JWKS-proxy :9097 (server-TLS)
IAM_REGTOKEN_PORT="${IAM_REGTOKEN_PORT:-19096}" # iam docker-token handle :9096 (server-TLS)
# Порт data plane реестра здесь не объявляется: адресат — компонент, и порт вместе
# с портовой ручкой живёт в deploy/e2e-shards.json (`optional_transports`).

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

echo "[e2e] port-forward api-gateway :$GW_PORT (public) / :$GW_INTERNAL_PORT (internal-rest) / :$GW_TLS_PORT (external TLS) + kaname-internal :$IAM_INTERNAL_PORT"
kubectl -n "$NS" port-forward svc/api-gateway "$GW_PORT:8080" >/tmp/e2e-pf-gw.log 2>&1 &
PF_PIDS+=($!)
# internal-rest (:8081) — ОТДЕЛЬНЫЙ листенер для Internal*-RPC. На публичном :8080 их
# нет и быть не должно (ban #6: Internal.* не публикуется на external endpoint), поэтому
# коллекции internal-* обязаны ходить сюда через {{internalBaseUrl}}, иначе получают
# закономерный 404. iam-набор так и делает; vpc-набор — ещё нет (см. README/issue).
kubectl -n "$NS" port-forward svc/api-gateway "$GW_INTERNAL_PORT:8081" >/tmp/e2e-pf-gw-internal.log 2>&1 &
PF_PIDS+=($!)
# external TLS (:8443) — то, что рекламируется наружу. Сюда ходят ban-#6 негативы
# ({{externalBaseUrl}}): Internal*-пути обязаны быть недостижимы на нём.
kubectl -n "$NS" port-forward svc/api-gateway "$GW_TLS_PORT:8443" >/tmp/e2e-pf-gw-tls.log 2>&1 &
PF_PIDS+=($!)
kubectl -n "$NS" port-forward svc/kaname-internal "$IAM_INTERNAL_PORT:9091" >/tmp/e2e-pf-iam.log 2>&1 &
PF_PIDS+=($!)
# Hydra public — POST target of the OAuth2 client_credentials exchange that turns an
# iam-issued SA key into the RS256 Bearer a production-posture stand accepts. ClusterIP
# with no ingress route here. Unused in dev; required in production.
kubectl -n "$NS" port-forward svc/kacho-umbrella-hydra-public "${HYDRA_PUBLIC_PORT:-14444}:4444" >/tmp/e2e-pf-hydra.log 2>&1 &
PF_PIDS+=($!)
# Полоса фасада (#59): JWKS-прокси iam, ручка docker-токена iam и data-plane реестра.
kubectl -n "$NS" port-forward svc/kaname-internal "$IAM_JWKS_PORT:9097" >/tmp/e2e-pf-iam-jwks.log 2>&1 &
PF_PIDS+=($!)
kubectl -n "$NS" port-forward svc/kaname "$IAM_REGTOKEN_PORT:9096" >/tmp/e2e-pf-iam-regtoken.log 2>&1 &
PF_PIDS+=($!)

# ПРОБРОС К КОМПОНЕНТУ — ПО СПРОСУ, ТЕМ ЖЕ ПРЕДИКАТОМ, ЧТО У newman-parallel.sh.
#
# Все пробросы выше ведут к ЯДРУ и есть на любом стенде. Адресат такого проброса —
# переключаемый компонент, которого на стенде может не быть; `kubectl port-forward
# svc/<нет такого>` не встаёт и ЗАВЕРШАЕТСЯ. У этого скрипта проверки живости
# пробросов нет вовсе, поэтому здесь это давало не «прогон недействителен», а
# мёртвый порт и отказ на сотню строк ниже — про чужой предмет.
#
# Спрос выводится ИЗ ДЕРЕВА для ТОЙ суиты, которую этот запуск и гоняет, и тем же
# модулем, что у прогонщика шардов: два предиката об одном разошлись бы молча.
OPT_ENV_ARGS=()
while IFS='|' read -r _ovar _osvc _otport _oportenv _odport _oscheme _owhy; do
  [ -n "${_ovar:-}" ] || continue
  _oport="${!_oportenv:-$_odport}"
  kubectl -n "$NS" port-forward "svc/$_osvc" "$_oport:$_otport" >"/tmp/e2e-pf-opt-$_ovar.log" 2>&1 &
  PF_PIDS+=($!)
  OPT_ENV_ARGS+=(--env-var "$_ovar=$_oscheme://localhost:$_oport")
  echo "[e2e] транспорт компонента: $_ovar → svc/$_osvc :$_otport на localhost:$_oport ($_owhy)"
done < <(python3 "$(dirname "${BASH_SOURCE[0]}")/e2e-optional-transports.py" --suites "$SVC" --census)
sleep 4

# mTLS для grpcurl → kaname-internal:9091.
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
# Bootstrap-operator leaf — a DIFFERENT identity, the only SPIFFE SAN kaname
# allow-lists for MintBootstrapToken (the production seed's entry point). Deliberately
# not the api-gateway one: the gateway fronts tenant traffic, so "is the api-gateway"
# must never be a licence to mint a cluster admin.
if kubectl -n "$NS" get secret kacho-bootstrap-operator-client-tls >/dev/null 2>&1; then
  OP_DIR="$(mktemp -d)"; TMP_DIRS+=("$OP_DIR")
  kubectl -n "$NS" get secret kacho-bootstrap-operator-client-tls -o jsonpath='{.data.tls\.crt}' | base64 -d > "$OP_DIR/client.crt"
  kubectl -n "$NS" get secret kacho-bootstrap-operator-client-tls -o jsonpath='{.data.tls\.key}' | base64 -d > "$OP_DIR/client.key"
  chmod 600 "$OP_DIR"/*
  MTLS_ENV+=(BOOTSTRAP_MINT_MTLS_CERT="$OP_DIR/client.crt"
             BOOTSTRAP_MINT_MTLS_KEY="$OP_DIR/client.key")
fi

echo "[e2e] seeding auth fixtures (idempotent) + patching newman envs"
env BASE_URL="http://localhost:$GW_PORT" \
IAM_INTERNAL_GRPC="localhost:$IAM_INTERNAL_PORT" \
PLATFORM_TOKEN_URL="https://127.0.0.1:$IAM_REGTOKEN_PORT/iam/v1/token" \
PATCH_ENV=true SETUP_NS="$NS" \
"${MTLS_ENV[@]}" \
  bash "$REPO_ROOT/tests/authz-fixtures/setup.sh"

# nlb's external-VIP AddressPool is seeded by setup.sh, and ONLY there.
#
# A second pass used to stand here, guarded by "unless the seed ran in production
# posture". That guard could never be false: the seed's classifier leaves `production` as
# the only value standing, and a seed that does not reach that point aborts this script
# (`set -e`) before the read. So the block was unreachable from the day the classifier
# was narrowed — and its fallback named `dev`, the one posture the classifier REFUSES.
#
# Nothing is lost by removing it: setup.sh delegates to prodseed_all.py, which drives
# deploy/scripts/seed-nlb-fixtures.sh itself and is the sole author of that
# cluster-wide default-pool slot. A second author is what the guard was trying to
# prevent — unconditionally now, by not existing.
#
# deploy/scripts/assert-posture-branches-can-be-taken.py keeps this shape from coming
# back: a branch on the seed posture must be able to go both ways.

echo "[e2e] regenerating newman collections"
( cd "$NEWMAN_DIR" && python3 scripts/gen.py >/dev/null )

echo "[e2e] running newman (SVC=$SVC COLLECTION=${COLLECTION:-<all>})"
cd "$NEWMAN_DIR"
if [ -n "$COLLECTION" ]; then
  newman run "collections/${COLLECTION}.postman_collection.json" \
    -e environments/local.postman_environment.json \
    --env-var "baseUrl=http://localhost:$GW_PORT" \
    --env-var "internalBaseUrl=http://localhost:$GW_INTERNAL_PORT" \
    --env-var "externalBaseUrl=https://127.0.0.1:$GW_TLS_PORT" \
    --env-var "iamJwksBaseUrl=https://127.0.0.1:$IAM_JWKS_PORT" \
    --env-var "providerPublicBaseUrl=http://localhost:${HYDRA_PUBLIC_PORT:-14444}" \
    --env-var "iamRegistryTokenBaseUrl=https://127.0.0.1:$IAM_REGTOKEN_PORT" \
    "${OPT_ENV_ARGS[@]}" \
    --delay-request 15 --reporters cli
else
  # run.sh НЕ читает BASE_URL/INTERNAL_BASE_URL из окружения — значения он берёт только
  # из env-файла, а всё неизвестное в argv пробрасывает в newman как есть (массив EXTRA).
  # Поэтому передаём --env-var через argv, а не через env: иначе {{internalBaseUrl}}
  # остаётся пустым и Internal*-шаги молча уходят на публичный порт → 404
  # (internal-pool: 78/0 в одиночном прогоне, но 62/56 в полном — ровно этот разрыв).
  set +e
  ./scripts/run.sh --service "" --delay 15 \
    --env-var "baseUrl=http://localhost:$GW_PORT" \
    --env-var "internalBaseUrl=http://localhost:$GW_INTERNAL_PORT" \
    --env-var "externalBaseUrl=https://127.0.0.1:$GW_TLS_PORT" \
    --env-var "iamJwksBaseUrl=https://127.0.0.1:$IAM_JWKS_PORT" \
    --env-var "providerPublicBaseUrl=http://localhost:${HYDRA_PUBLIC_PORT:-14444}" \
    --env-var "iamRegistryTokenBaseUrl=https://127.0.0.1:$IAM_REGTOKEN_PORT" \
    "${OPT_ENV_ARGS[@]}"
  RAW_RC=$?
  set -e

  # ── same two-verdict discipline as newman-parallel.sh ──────────────────────
  # RAW = run.sh's own verdict (every failed assertion / non-zero newman rc).
  # GATED = the verdict CI grades with (services/iam/tests/newman/scripts/
  # assert-suites-green.sh), per collection.
  #
  # These two used to differ BY CONSTRUCTION, because the gate deducted a set of
  # cases before deciding and the gap between the blocks was the size of the
  # deduction. That deduction was removed 2026-07-30 (see the gate's own note on
  # why narrowing it could never work), so nothing is subtracted from the verdict
  # any more and nothing is filtered out of the request count either: a request
  # that got no answer is reported as UNANSWERED and fires the gate.
  #
  # RAW therefore stays printed for a different reason than before: it is the
  # roll-up you can read at a glance, and if the two blocks ever disagree, THAT is
  # the finding — something has started subtracting again.
  GATE="${GATE:-true}"
  GATE_SCRIPT="$REPO_ROOT/services/iam/tests/newman/scripts/assert-suites-green.sh"
  echo
  echo "[e2e] RAW verdict (run.sh): rc=$RAW_RC — see out/summary.txt"
  if [ "$GATE" = "true" ] && [ -f "$GATE_SCRIPT" ]; then
    echo "[e2e] GATED verdict (the gate CI runs):"
    set +e
    bash "$GATE_SCRIPT"
    GATE_RC=$?
    set -e
    [ "$GATE_RC" -eq 0 ] && [ "$RAW_RC" -ne 0 ] && \
      echo "[e2e] INCONSISTENT: the gate is green while run.sh is red, and nothing is deducted from the verdict any more — read the raw failures, do not trust this GREEN."
    exit "$GATE_RC"
  fi
  echo "[e2e] CI gate skipped (GATE=$GATE) — grading on RAW"
  exit "$RAW_RC"
fi
