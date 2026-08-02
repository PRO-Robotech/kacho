#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# KAC-122 — authz-deny suite fixtures: ВХОД. Один вопрос, один ответ.
#
# Этот файл решает РОВНО одно: какую посадку сообщает живой край, и вправе ли харнесс
# на неё сеять. Сам он не создаёт ни одной сущности и не чеканит ни одного токена —
# опознанная посадка отдаётся `prodseed_all.py`, где каждый Bearer выдаёт iam
# (MintBootstrapToken → SAKeyService.Issue → OAuth2 client_credentials).
#
# ПОЧЕМУ ЗДЕСЬ БОЛЬШЕ НЕТ ВЕТКИ СИММЕТРИЧНОЙ ЧЕКАНКИ. Она чеканила HS256-Bearer'ы
# общим ключом, лежавшим тут же в дереве. Общий ключ в публичном репозитории держат
# все, и «сменить значение» ничего не меняет — новое станет общим тем же коммитом.
# Профили развёртывания его больше не рендерят, а край отказывается стартовать, если
# он до него доезжает (`gateway/cmd/api-gateway/authz_validation.go`), — то есть
# ветка обслуживала стенд, которого не бывает. README объявил её удаление отдельным
# шагом «вместе с текстом, чтобы описание и код не разъехались»; это он и есть.
#
# Окружение читает `prodseed_all.py` (BASE_URL / INTERNAL_BASE_URL / IAM_INTERNAL_GRPC /
# SETUP_NS / HYDRA_* и прочее — значения по умолчанию объявлены там же, в ОДНОМ месте).
# Здесь нужны только два:
#   OUT_DIR            — куда лечь `seed-posture` и набору фикстур
#                        (default tests/authz-fixtures/out)
#   IAM_MTLS_CERT_DIR  — куда положить извлечённый клиентский сертификат
#                        (default /tmp/iam-mtls); тот же путь читает prodseed_matrix.py
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

OUT_DIR="${OUT_DIR:-$SCRIPT_DIR/out}"

# Транспорт для grpcurl к kacho-iam-internal. По умолчанию (kind / mTLS-off CI)
# — plaintext. На mTLS-стенде (например fe3455, KACHO_IAM_INTERNAL_SERVER_MTLS_ENABLE=true)
# internal-listener требует client-cert: задай IAM_INTERNAL_GRPC_MTLS_CERT/_KEY
# (PEM client-cert/key, принимаемые ClientCAFiles internal-листенера) — тогда grpcurl
# идёт mTLS. -insecure пропускает проверку server-SAN (порт-форвард меняет hostname),
# client-cert всё равно предъявляется.
#
# SELF-PROVISIONING (not just "if the caller passed one"): kacho-iam's internal listener
# requires a verified client certificate in EVERY posture — security.md is explicit that
# :9091 is NOT exempt and that «internal = trusted» is a forbidden assumption. A seed
# that only carries a cert when its driver happened to export one works from
# newman-parallel.sh and fails from a bare `bash setup.sh`, and the failure looks like an
# authz problem rather than a missing credential. So: pull the leaf the cluster already
# issued (api-gateway-client-tls — the identity iam admits for the ordinary internal
# RPCs; the bootstrap-token MINT deliberately does NOT accept it) into a 0600 file under
# /tmp. Never generated, never written into the repo, re-extracted every run because
# cert-manager regenerates the internal CA on a fresh dev-up and a stale leaf is signed
# by the OLD CA (a permanent handshake failure that a retry only repeats).
IAM_MTLS_DIR="${IAM_MTLS_CERT_DIR:-/tmp/iam-mtls}"
if [ -z "${IAM_INTERNAL_GRPC_MTLS_CERT:-}" ] && [ -z "${IAM_INTERNAL_GRPC_MTLS_KEY:-}" ] \
   && command -v kubectl >/dev/null 2>&1 \
   && kubectl -n "${SETUP_NS:-kacho}" get secret api-gateway-client-tls >/dev/null 2>&1; then
  mkdir -p "$IAM_MTLS_DIR"
  if kubectl -n "${SETUP_NS:-kacho}" get secret api-gateway-client-tls -o jsonpath='{.data.tls\.crt}' 2>/dev/null | base64 -d > "$IAM_MTLS_DIR/client.crt" \
     && kubectl -n "${SETUP_NS:-kacho}" get secret api-gateway-client-tls -o jsonpath='{.data.tls\.key}' 2>/dev/null | base64 -d > "$IAM_MTLS_DIR/client.key" \
     && [ -s "$IAM_MTLS_DIR/client.crt" ] && [ -s "$IAM_MTLS_DIR/client.key" ]; then
    chmod 600 "$IAM_MTLS_DIR/client.crt" "$IAM_MTLS_DIR/client.key"
    IAM_INTERNAL_GRPC_MTLS_CERT="$IAM_MTLS_DIR/client.crt"
    IAM_INTERNAL_GRPC_MTLS_KEY="$IAM_MTLS_DIR/client.key"
  fi
fi

# require grpcurl — им ходит prodseed_matrix.py / prodseed_network.py на iam :9091
# (у InternalUserService.UpsertFromIdentity нет REST-маппинга, KAC-125). Проверка
# стоит ЗДЕСЬ, до опознания посадки: отсутствующий инструмент — это отказ входа, а не
# отказ посева, и путать их незачем.
if ! command -v grpcurl >/dev/null 2>&1; then
  echo "[setup] FATAL: grpcurl не найден в PATH. Установи: go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"

log() { echo "[setup] $*" >&2; }

# ── POSTURE GATE ────────────────────────────────────────────────────────────
#
# The gate answers one question — is this a stand whose ISSUER hands out bearers — and
# the only way past it is delegation to prodseed_all.py, which asks iam for every one of
# them (MintBootstrapToken → SAKeyService.Issue → OAuth2 client_credentials).
#
# Detection reads the ONE line the process itself logs after its boot-guards
# (`msg="boot security posture"`, pkg/observability/bootposture.go) — the same
# observable deploy/scripts/assert-production-posture.sh grades on. Deliberately NOT the
# ConfigMap/values: security knobs arrive via `envFrom` and are read once at start, so a
# stored value can say `production` while the running process is still `dev` (the
# 2026-07-25 storage incident). A pod that never rolled reports its OLD posture — which
# is exactly the truth we want to seed against.
#
# SEED_POSTURE=production (or production-strict) forces it — CI without cluster read
# access, or a deliberate rehearsal. `auto` (default) asks the stand.
#
# SEED_POSTURE=dev IS REFUSED, and the refusal is the point rather than a nicety. No
# deployment profile puts the edge on the relaxed posture any more — the chart offers no
# knob for a shared signing key and the process refuses to start if one reaches it — so
# the value selects a stand that cannot exist. This is an exception whose subject is
# gone; it says so out loud instead of waiting to mislead somebody.
#
# ANYTHING ELSE IS ALSO REFUSED, and that is not tidiness. The classification used to
# have no such bucket, so an unrecognised value walked past the gate into the branch that
# forged symmetric bearers — `production-strict` among them, i.e. naming the strictest
# posture selected the opposite of what it says.
SETUP_NS="${SETUP_NS:-kacho}"
SEED_POSTURE="${SEED_POSTURE:-auto}"

# refuse_relaxed_posture <how-it-was-selected>
#
# One refusal for both routes — the forced value and the value read off the stand — so
# the two cannot drift into meaning different things.
refuse_relaxed_posture() {
  echo "[setup] FATAL: the relaxed api-gateway posture is not a stand this harness seeds ($1)." >&2
  echo "[setup]        No deployment profile produces it: the chart offers no shared signing" >&2
  echo "[setup]        key and the gateway refuses to start if one reaches it, so a bearer" >&2
  echo "[setup]        signed here is rejected at the edge by construction." >&2
  echo "[setup]        If a stand really reports it, that stand is behind — roll it forward" >&2
  echo "[setup]        (deploy/Makefile 'dev-up') rather than seeding around it." >&2
  exit 1
}

gateway_boot_auth_mode() {
  command -v kubectl >/dev/null 2>&1 || return 1
  local pod
  pod=$(kubectl -n "$SETUP_NS" get pods -o name 2>/dev/null | grep -m1 '^pod/api-gateway-' | cut -d/ -f2)
  [ -n "$pod" ] || return 1
  kubectl -n "$SETUP_NS" logs "$pod" 2>/dev/null | python3 -c '
import json, sys
for line in sys.stdin:
    line = line.strip()
    if "boot security posture" not in line:
        continue
    try:
        d = json.loads(line)
    except ValueError:
        continue
    if d.get("msg") == "boot security posture":
        print(d.get("auth_mode", ""))
        break
'
}

# classify_posture <value> <how-it-was-selected>
#
# ONE classifier for both routes — the value forced through SEED_POSTURE and the value
# read off the live gateway — because they answer the same question and must not drift
# into meaning different things. It either sets SEED_POSTURE to the single value this
# script proceeds on, or it exits.
#
# There is NO "everything else" bucket, and its absence is the whole point of the
# function. The classification used to live in three separate `if`s (dev / auto /
# otherwise), and the third one asserted nothing: it printed the value and fell through
# to a later `= production` comparison. So every value except three fell PAST the gate
# into the branch that forged symmetric bearers — including `production-strict`, the
# name of the strictest posture, which the auto route deliberately maps to `production`
# while the forced route did not. Naming the strictest posture selected the opposite of
# what it says; so did any typo (`prod`, `Production`).
classify_posture() {
  case "$1" in
    production|production-strict)
      SEED_POSTURE="production"
      ;;
    dev)
      refuse_relaxed_posture "$2"
      ;;
    *)
      # Unrecognised is its own outcome, distinct from both of the above: proceeding on
      # a value nobody classified produces a run whose failure lands somewhere else
      # entirely and reads like a product bug. Say exactly what was not understood and
      # stop.
      echo "[setup] FATAL: unrecognised api-gateway posture '${1:-<none>}' ($2)." >&2
      echo "[setup]        The seed obtains every bearer through iam and needs to know it is" >&2
      echo "[setup]        talking to a stand that issues them. Accepted values:" >&2
      echo "[setup]        production, production-strict (both proceed) and dev (refused)." >&2
      echo "[setup]        Check kubectl access to ns/$SETUP_NS, or set SEED_POSTURE=production." >&2
      exit 1
      ;;
  esac
}

if [ "$SEED_POSTURE" = "auto" ]; then
  GW_AUTH_MODE="$(gateway_boot_auth_mode || true)"
  classify_posture "$GW_AUTH_MODE" "the live api-gateway reports auth_mode='${GW_AUTH_MODE:-<none>}'"
  log "posture: $SEED_POSTURE (api-gateway reports auth_mode=$GW_AUTH_MODE)"
else
  classify_posture "$SEED_POSTURE" "forced via SEED_POSTURE=$SEED_POSTURE"
  log "posture: $SEED_POSTURE (forced via SEED_POSTURE)"
fi

# Record the posture this seed ran in — a PROVENANCE note for whoever later reads this
# out/ directory and needs to know what the fixture set beside it means.
#
# NOT a switch, and no longer described as one. Two drivers used to branch on it, and
# both branches were dead for the same reason the delegation below is unconditional:
# `production` is the only value that reaches this line, so a comparison against it is a
# constant. The comment here claimed drivers branch on it, which made the dead guards
# read as load-bearing. deploy/scripts/assert-posture-branches-can-be-taken.py now fails
# on any branch taken on this value while the classifier leaves one value standing.
echo "$SEED_POSTURE" > "$OUT_DIR/seed-posture"

# Unconditional: `production` is the only value classify_posture leaves standing, so a
# guarded delegation here would be a branch that cannot be false — the shape that let a
# whole symmetric-minting region look reachable for as long as it did.
log "delegating to prodseed_all.py — tokens issued BY iam, none minted here"
exec python3 "$SCRIPT_DIR/prodseed_all.py"
