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
# Окружение читает `prodseed_all.py` (INTERNAL_BASE_URL / IAM_INTERNAL_GRPC / SETUP_NS /
# HYDRA_* и прочее — значения по умолчанию объявлены там же). Здесь нужны три:
#   BASE_URL           — адрес края (default http://localhost:18080). Он ЗДЕСЬ же и
#                        экспортируется: этим адресом опознаётся посадка, и он обязан
#                        совпадать с тем, к которому потом пойдёт посев — иначе гейт
#                        оценивал бы край, которым посев не пользуется
#   OUT_DIR            — куда лечь `seed-posture` и набору фикстур
#                        (default tests/authz-fixtures/out)
#   IAM_MTLS_CERT_DIR  — куда положить извлечённый клиентский сертификат
#                        (default /tmp/iam-mtls); тот же путь читает prodseed_matrix.py
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

OUT_DIR="${OUT_DIR:-$SCRIPT_DIR/out}"

# Транспорт для grpcurl к kaname-internal. По умолчанию (kind / mTLS-off CI)
# — plaintext. На mTLS-стенде (например fe3455, KANAME_INTERNAL_SERVER_MTLS_ENABLE=true)
# internal-listener требует client-cert: задай IAM_INTERNAL_GRPC_MTLS_CERT/_KEY
# (PEM client-cert/key, принимаемые ClientCAFiles internal-листенера) — тогда grpcurl
# идёт mTLS. -insecure пропускает проверку server-SAN (порт-форвард меняет hostname),
# client-cert всё равно предъявляется.
#
# SELF-PROVISIONING (not just "if the caller passed one"): kaname's internal listener
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

# require curl — им опознаётся посадка (ниже). Проверка стоит ЗДЕСЬ по той же причине,
# и она не про аккуратность: без неё отсутствующий curl выглядел бы как «край не
# ответил», то есть отказ инструмента читался бы как факт о стенде.
if ! command -v curl >/dev/null 2>&1; then
  echo "[setup] FATAL: curl не найден в PATH — им опознаётся посадка края." >&2
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
# THE EVIDENCE IS AN OUTCOME, NOT A RECORD, and that choice is the whole point of this
# section. Detection used to read the one line the gateway logs about its own posture at
# start-up. That line is a good observable and the deploy-time gate still grades on it —
# but it lives in a CONTAINER LOG, which is an EPHEMERAL artefact: kubelet rotates the
# file at a size limit and `kubectl logs` (without `--previous`) serves only the CURRENT
# one. On a stand older than one rotation the line is simply unreachable, and a
# classifier whose evidence has expired was answering "posture unrecognised", blaming
# missing cluster access (there was none missing) and offering the operator an
# environment variable to force an unverified classification. Three defects in one spot:
# the evidence was perishable, the diagnosis was false, and the override was silent. It
# broke this way twice, so the fix is the SOURCE, not the parsing.
#
# What is asked instead: the edge itself, over the same base URL the seed must reach
# anyway. A request carrying NO credential is either refused or it is not — that answer
# does not rotate, does not depend on how long the pod has been up, and cannot be forged
# by a stored setting, because it is produced by the running process enforcing (or not
# enforcing) authentication.
#
# TWO PROBES, AND THE PAIR IS LOAD-BEARING:
#
#   CONTROL   — same route, deliberately invalid credential. Must answer 401. It proves
#               the probe address reaches the authentication gate at all, and it says
#               NOTHING about posture (an invalid credential is refused in every
#               posture). Without it a renamed route — the edge answers 403 for an
#               unknown method — would read as "this edge served an unauthenticated
#               caller", i.e. the gate would report a relaxed stand because its own
#               address went stale.
#   DECIDING  — same route, NO Authorization header at all. 401 means the edge demands a
#               credential: production-class, seed proceeds. Anything else means a
#               request with no credential got a substantive answer, which is the
#               relaxed pass-through and not a stand this harness seeds.
#
# THREE OUTCOMES, THREE EXIT CODES, and the third is not folded into either neighbour:
#   0 — posture proven, seed delegated to the issuer;
#   1 — posture proven and it is NOT one we seed (relaxed edge), or the invocation was
#       refused outright;
#   3 — posture COULD NOT BE ESTABLISHED. Not a verdict about the stand: the evidence
#       was not obtained. Saying "relaxed" here would be an assertion about a stand
#       nobody looked at.
#
# THERE IS NO WAY TO FORCE THE ANSWER. `SEED_POSTURE` used to do that, justified by "CI
# without cluster read access". The classification above needs no cluster access at all —
# only the edge URL, which the seed cannot do without — so the exemption's subject is
# gone. Setting the variable is REFUSED rather than ignored: a variable that is silently
# ignored reads to whoever set it as "I forced it", and being wrong about that is exactly
# the state this section exists to prevent.
SETUP_NS="${SETUP_NS:-kacho}"

if [ -n "${SEED_POSTURE:-}" ]; then
  echo "[setup] FATAL: SEED_POSTURE is gone — posture is no longer something a caller declares." >&2
  echo "[setup]        It existed so a runner without cluster read access could proceed; the" >&2
  echo "[setup]        classification below asks the edge over BASE_URL and needs no cluster" >&2
  echo "[setup]        access, so the exemption has no subject left. It refuses instead of" >&2
  echo "[setup]        ignoring you, because an ignored variable reads as a forced answer." >&2
  echo "[setup]        Unset SEED_POSTURE and point BASE_URL at the stand." >&2
  exit 1
fi

# The base URL is settled HERE and EXPORTED, so the address the gate probed is the same
# address the seed then talks to. Two literals in two files would be free to drift, and
# then the gate would be grading an edge the seed never uses.
BASE_URL="${BASE_URL:-http://localhost:18080}"
export BASE_URL

# The probe route belongs to the very service the seed depends on for every bearer, and
# it is a plain read. It must be a route that requires authentication — never one on the
# pre-auth allowlist, whose whole purpose is to answer without a credential.
POSTURE_PROBE_PATH="${POSTURE_PROBE_PATH:-/iam/v1/projects}"
POSTURE_PROBE_URL="$BASE_URL$POSTURE_PROBE_PATH"
POSTURE_PROBE_TIMEOUT="${POSTURE_PROBE_TIMEOUT:-10}"
# Retries cover a port-forward that has just been started, and NOTHING else: they apply
# only when the transport produced no answer. A decided status is never retried — that
# would turn a verdict into a wait.
POSTURE_PROBE_RETRIES="${POSTURE_PROBE_RETRIES:-5}"
POSTURE_PROBE_RETRY_DELAY="${POSTURE_PROBE_RETRY_DELAY:-2}"

# refuse_relaxed_posture <what-was-observed>
refuse_relaxed_posture() {
  echo "[setup] FATAL: this edge does not require a credential ($1)." >&2
  echo "[setup]        A request with no Authorization header got a substantive answer, so" >&2
  echo "[setup]        the edge is in the relaxed pass-through posture. Bearers issued by" >&2
  echo "[setup]        iam are not what that stand is checking, and seeding it would prove" >&2
  echo "[setup]        nothing about the posture we ship." >&2
  echo "[setup]        Roll the stand forward (deploy/Makefile 'dev-up') rather than" >&2
  echo "[setup]        seeding around it." >&2
  exit 1
}

# evidence_missing <what-happened> — the THIRD outcome.
#
# Deliberately not phrased as a statement about the stand. Nothing was learned about the
# posture, and a refusal that pretends otherwise sends the reader to fix the wrong thing —
# which is precisely how the previous classifier misled: it turned "my evidence expired"
# into "your cluster access is broken".
evidence_missing() {
  echo "[setup] NOT DETERMINED: the edge posture could not be established ($1)." >&2
  echo "[setup]        This is not a verdict about the stand — no evidence was obtained," >&2
  echo "[setup]        so nothing is being claimed about it either way." >&2
  echo "[setup]        Probe address: $POSTURE_PROBE_URL (override with BASE_URL /" >&2
  echo "[setup]        POSTURE_PROBE_PATH). Check that the edge is reachable there — a" >&2
  echo "[setup]        port-forward that died takes the whole seed with it anyway." >&2
  exit 3
}

# probe_edge [curl-args…] — one observation token on stdout:
#   http:<code>  the edge answered with an HTTP status
#   silent       nothing answered within the retry budget
#
# The token is the ONLY thing the classifier sees, which is what makes the classifier
# testable on a synthetic observation without a cluster
# (internal/repohygiene/seedposturegate_test.go drives every outcome that way).
probe_edge() {
  local code rc attempt=1
  while :; do
    if code="$(curl -s -o /dev/null -m "$POSTURE_PROBE_TIMEOUT" -w '%{http_code}' "$@" "$POSTURE_PROBE_URL" 2>/dev/null)"; then
      rc=0
    else
      rc=$?
    fi
    # curl prints 000 and exits non-zero when nothing answered; treat both as silence so
    # a stub or a proxy that reports one without the other cannot be read as an answer.
    if [ "$rc" -eq 0 ] && [ -n "$code" ] && [ "$code" != "000" ]; then
      printf 'http:%s' "$code"
      return 0
    fi
    if [ "$attempt" -ge "$POSTURE_PROBE_RETRIES" ]; then
      printf 'silent'
      return 0
    fi
    attempt=$((attempt + 1))
    if [ "$POSTURE_PROBE_RETRY_DELAY" -gt 0 ]; then sleep "$POSTURE_PROBE_RETRY_DELAY"; fi
  done
}

# The control runs FIRST. Its failure must not be describable as a posture, so it has to
# be settled before the deciding observation is even interpreted.
POSTURE_CONTROL="$(probe_edge -H 'Authorization: Bearer not-a-token')"
case "$POSTURE_CONTROL" in
  http:401) : ;;
  silent)
    evidence_missing "the control probe got no answer" ;;
  *)
    evidence_missing "the control probe answered ${POSTURE_CONTROL#http:} where 401 was required; \
an invalid credential is refused in every posture, so this address is not reaching the \
authentication gate — most likely the route moved" ;;
esac

POSTURE_DECIDING="$(probe_edge)"
case "$POSTURE_DECIDING" in
  http:401)
    # The edge demanded a credential. production and production-strict are indistinguishable
    # here and always were treated alike; the artefact records the single value this script
    # proceeds on.
    SEED_POSTURE="production"
    ;;
  silent)
    evidence_missing "the deciding probe got no answer although the control did" ;;
  *)
    refuse_relaxed_posture "answered ${POSTURE_DECIDING#http:} to a request carrying no credential" ;;
esac

log "posture: $SEED_POSTURE (edge refuses an uncredentialed request: control 401, deciding 401 at $POSTURE_PROBE_URL)"

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
