#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# KAC-122 — authz-deny suite fixtures bootstrap.
#
# Идемпотентен. Создаёт минимальный набор Account / Project / User / AccessBinding
# чтобы 6-субъектная permission-матрица проверялась стабильно. Также активирует
# invitee per-Account через KAC-125 Invite-flow.
#
# Окружение:
#   BASE_URL         — api-gateway endpoint (default http://localhost:18080)
#   DEV_SECRET       — KACHO_API_GATEWAY_AUTHN_DEV_SECRET (default kacho-dev-jwt-secret-2026)
#   EXP_HOURS        — exp для JWT (default 24)
#   OUT_DIR          — куда писать authz-fixtures.json (default tests/authz-fixtures/out)
#   PATCH_ENV        — если true, патчит environments/*.json во всех 3 newman-suites
#                     (default true)
#   VERBOSE          — true → echo каждый curl
#   RESET_FGA        — KAC-127 RC-1b: если true, удаляет stale OpenFGA-tuples,
#                      указывающие на test-объекты (account:authz-* / project:* /
#                      iam_*:* субъектов матрицы) ПЕРЕД повторным seed'ом, чтобы
#                      прогоны не накапливали дубликаты от прошлых моделей. По
#                      умолчанию false — Write-тuples и так идемпотентны (409 =
#                      success), reset нужен только при смене FGA-модели.
#                      Требует OPENFGA_HTTP (default http://localhost:18081) +
#                      OPENFGA_STORE_ID.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

BASE_URL="${BASE_URL:-http://localhost:18080}"
IAM_INTERNAL_GRPC="${IAM_INTERNAL_GRPC:-localhost:19091}"  # порт-форвард на kacho-iam-internal:9091 (grpcurl)
DEV_SECRET="${DEV_SECRET:-kacho-dev-jwt-secret-2026}"
EXP_HOURS="${EXP_HOURS:-24}"
OUT_DIR="${OUT_DIR:-$SCRIPT_DIR/out}"
PATCH_ENV="${PATCH_ENV:-true}"
VERBOSE="${VERBOSE:-false}"
RESET_FGA="${RESET_FGA:-false}"
OPENFGA_HTTP="${OPENFGA_HTTP:-http://localhost:18081}"
OPENFGA_STORE_ID="${OPENFGA_STORE_ID:-}"
# Cap ожидания готовности bootstrap cluster-admin (system_admin@cluster через
# fga_outbox-reconciler). На реальном стенде с mTLS реконсайл fga_outbox медленнее
# kind, поэтому cap конфигурируем: poll идёт с шагом 2s и выходит рано по готовности,
# так что больший cap не замедляет happy-path, но даёт prod-like стендам сойтись.
CLUSTER_ADMIN_WAIT_SECS="${CLUSTER_ADMIN_WAIT_SECS:-180}"

# Транспорт для grpcurl к kacho-iam-internal. По умолчанию (kind / mTLS-off CI)
# — plaintext. На mTLS-стенде (например fe3455, KACHO_IAM_INTERNAL_SERVER_MTLS_ENABLE=true)
# internal-listener требует client-cert: задай IAM_INTERNAL_GRPC_MTLS_CERT/_KEY
# (PEM client-cert/key, принимаемые ClientCAFiles internal-листенера) — тогда grpcurl
# идёт mTLS. -insecure пропускает проверку server-SAN (порт-форвард меняет hostname),
# client-cert всё равно предъявляется.
GRPCURL_TLS="-plaintext"
if [ -n "${IAM_INTERNAL_GRPC_MTLS_CERT:-}" ] && [ -n "${IAM_INTERNAL_GRPC_MTLS_KEY:-}" ]; then
  GRPCURL_TLS="-insecure -cert ${IAM_INTERNAL_GRPC_MTLS_CERT} -key ${IAM_INTERNAL_GRPC_MTLS_KEY}"
fi

# require grpcurl for InternalUserService.UpsertFromIdentity (нет REST маппинга — KAC-125)
if ! command -v grpcurl >/dev/null 2>&1; then
  echo "[setup] FATAL: grpcurl не найден в PATH. Установи: go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"

log() { echo "[setup] $*" >&2; }
vrun() { if [ "$VERBOSE" = "true" ]; then echo "+ $*" >&2; fi; "$@"; }

# 0) Optional OpenFGA store-tuple reset (KAC-127 RC-1b).
#
# Across repeated `dev-up` / `make authz-test-setup` runs the OpenFGA store
# retains every tuple ever written. Tuple writes are idempotent (409 ==
# success), so a re-seed never duplicates a tuple — but if the FGA *model*
# changed between runs, stale tuples that reference removed relations linger.
# When RESET_FGA=true we delete the tuples the matrix subjects/objects own
# before re-seeding, guaranteeing a clean slate. Default off (safe; the model
# is stable). Requires OPENFGA_HTTP + OPENFGA_STORE_ID.
reset_fga_tuples() {
  if [ "$RESET_FGA" != "true" ]; then
    log "0/8 OpenFGA store-reset skipped (RESET_FGA != true)"
    return 0
  fi
  if [ -z "$OPENFGA_STORE_ID" ]; then
    log "0/8 RESET_FGA=true but OPENFGA_STORE_ID is empty — skipping reset"
    return 0
  fi
  log "0/8 resetting stale OpenFGA tuples for test objects (store=$OPENFGA_STORE_ID)"
  # Read every tuple in the store, then delete the ones that point at
  # authz-test objects. OpenFGA Read with an empty tuple_key returns all.
  local page tuples
  page=$(curl -sS -X POST "$OPENFGA_HTTP/stores/$OPENFGA_STORE_ID/read" \
    -H "Content-Type: application/json" --data '{"page_size":100}' 2>/dev/null || echo '{}')
  tuples=$(echo "$page" | python3 -c '
import sys, json
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
for t in d.get("tuples", []):
    k = t.get("key", {})
    obj, user = k.get("object", ""), k.get("user", "")
    # only delete tuples that touch the matrix fixture objects/subjects.
    if "authz-test" in obj or "authz-test" in user or obj.startswith(("iam_", "account:", "project:")):
        print(json.dumps({"user": user, "relation": k.get("relation", ""), "object": obj}))
' 2>/dev/null || true)
  local n=0
  while IFS= read -r tk; do
    [ -z "$tk" ] && continue
    curl -sS -X POST "$OPENFGA_HTTP/stores/$OPENFGA_STORE_ID/write" \
      -H "Content-Type: application/json" \
      --data "{\"deletes\":{\"tuple_keys\":[$tk]}}" >/dev/null 2>&1 || true
    n=$((n + 1))
  done <<< "$tuples"
  log "    deleted $n stale fixture tuple(s)"
}
reset_fga_tuples

# 1) Mint all 6 JWTs.
log "1/10 minting JWTs (exp=${EXP_HOURS}h)"
JWTS=$(python3 "$SCRIPT_DIR/setup-jwt.py" --secret "$DEV_SECRET" --exp-hours "$EXP_HOURS" --bulk)
echo "$JWTS" > "$OUT_DIR/jwts.json"

JWT_BOOTSTRAP=$(echo "$JWTS" | python3 -c 'import json,sys; print(json.load(sys.stdin)["jwtBootstrap"])')
JWT_NO_BINDINGS=$(echo "$JWTS" | python3 -c 'import json,sys; print(json.load(sys.stdin)["jwtNoBindings"])')
JWT_PA1=$(echo "$JWTS" | python3 -c 'import json,sys; print(json.load(sys.stdin)["jwtProjectAdminA1"])')
JWT_AAA=$(echo "$JWTS" | python3 -c 'import json,sys; print(json.load(sys.stdin)["jwtAccountAdminA"])')
JWT_AAB=$(echo "$JWTS" | python3 -c 'import json,sys; print(json.load(sys.stdin)["jwtAccountAdminB"])')
JWT_INV=$(echo "$JWTS" | python3 -c 'import json,sys; print(json.load(sys.stdin)["jwtInvitee"])')
# Step-up (acr=2) variant of account-admin-A — for RPCs gated by the catalog's
# required_acr_min (RFC 9470), e.g. SAKeyService.Issue/Revoke.
JWT_AAA_STEPUP=$(echo "$JWTS" | python3 -c 'import json,sys; print(json.load(sys.stdin)["jwtAccountAdminAStepUp"])')

# kacho-iam#276 — DEDICATED never-granted leak-guard subject.
#
# `jwtNoBindings`/`userNOBId` is used DOUBLY: (a) as a grant-TARGET — the iam
# access-binding CRUD suites (IAM-ACB-CR-*, iam-flat-authz-vbc, iam-rbac-scope-grant,
# iam-authz-grant-check-propagation, iam-role, the authz-deny matrix) genuinely grant
# userNOB a `view` role on account-A/-B for the duration of their run (that IS their
# test); and (b) as a leak-guard VICTIM — the vpc/compute/iam-user "must see NOTHING"
# scope-filter probes read NOB expecting an empty result. Under the PARALLEL newman
# fan-out the granters' grant window overlaps the victims' reads → NOB is transiently
# authorized via account→project containment → false leak (`expected 1 to equal 0`).
#
# Fix (kacho-iam#276): the see-nothing leak-guards read THIS subject instead — a real,
# authenticated user with NO account membership that NO suite EVER grants (setup.sh
# 4b-cleanup only touches userNOB; every ensure_binding/AddMember below targets other
# principals). Guaranteed binding-free → the guards stay strict (sees 0 → PASS; sees
# anything → a GENUINE over-grant still FAILS honestly, no whitelist, no retry-mask).
# Minted the same way as the bulk subjects (HS256 dev JWT, sub = external_id).
JWT_PURE_NOB=$(python3 "$SCRIPT_DIR/setup-jwt.py" --secret "$DEV_SECRET" \
  --sub "auth-test-pure-no-bindings@example.com" --exp-hours "$EXP_HOURS")

# Helper: curl with bearer; prints body to stdout.
api() {
  local method="$1" path="$2" token="${3:-}" body="${4:-}"
  local hdrs=(-H "Content-Type: application/json" -H "Accept: application/json")
  if [ -n "$token" ]; then hdrs+=(-H "Authorization: Bearer $token"); fi
  if [ -n "$body" ]; then
    vrun curl -sS -X "$method" "${hdrs[@]}" --data "$body" "$BASE_URL$path"
  else
    vrun curl -sS -X "$method" "${hdrs[@]}" "$BASE_URL$path"
  fi
}

# api_status — like api() but emits ONLY the numeric HTTP status code (for
# readiness probes that gate on 200 vs 403). Quiet (no vrun trace).
api_status() {
  local method="$1" path="$2" token="${3:-}"
  local hdrs=(-H "Content-Type: application/json" -H "Accept: application/json")
  if [ -n "$token" ]; then hdrs+=(-H "Authorization: Bearer $token"); fi
  curl -sS -o /dev/null -w '%{http_code}' -X "$method" "${hdrs[@]}" "$BASE_URL$path" 2>/dev/null || echo 000
}

# poll-op — ждёт Operation.done=true и возвращает response.metadata если есть.
poll_op() {
  local op_id="$1" token="$2"
  for _ in $(seq 1 30); do
    local r
    r=$(api GET "/operations/${op_id}" "$token")
    if echo "$r" | grep -q '"done":true'; then echo "$r"; return 0; fi
    sleep 0.3
  done
  echo "$r"
  return 1
}

# 2) Upsert test users via InternalUserService.UpsertFromIdentity (gRPC-direct;
#    REST-маппинг отсутствует — proto не имеет google.api.http аннотации; KAC-125).
log "2/10 upserting test users via grpcurl → $IAM_INTERNAL_GRPC"

# upsert_user_grpc возвращает userId через stdout.
#
# КРИТИЧНО (KAC-127): UpsertFromIdentity возвращает Operation-envelope —
# `metadata.userId` выставлен сразу, НО bootstrap-Account + per-Account
# AccessBinding + FGA grant/hierarchy-tuples этого юзера пишутся в
# Operation-worker'е АСИНХРОННО. Если фикстура двинется к шагу 3
# (Account.Create) до того, как эти tuple'ы закоммичены, api-gateway authz
# Check ещё не видит принципала в OpenFGA → `code 7 permission denied`
# (наблюдалось в newman-e2e: FGA-tuple для AAA записан в .04, Account.Create
# в .21 → race). Поэтому здесь ОБЯЗАТЕЛЬНО дожидаемся Operation.done — после
# чего bootstrap-state юзера (и его FGA-tuple'ы) гарантированно есть.
upsert_user_grpc() {
  local ext_id="$1" email="$2" display="${3:-$email}"
  local body resp op_id user_id attempt
  body=$(printf '{"externalId":"%s","email":"%s","displayName":"%s"}' "$ext_id" "$email" "$display")
  # RETRY-until-non-empty (openfga-bootstrap restart-race): the openfga-bootstrap
  # post-install hook rolling-restarts kacho-iam to load the FGA store-id; the
  # Deployment can report Ready a beat before the new pod's FGA store env is fully
  # warm, so the first UpsertFromIdentity may transiently error → empty id → FATAL
  # (flaked the fleet-wide newman-e2e). UpsertFromIdentity is idempotent by
  # externalId/email, so retrying is safe; ~12×3s ≈ 36s absorbs the warm-up window.
  for attempt in $(seq 1 12); do
    resp=$(grpcurl $GRPCURL_TLS -d "$body" "$IAM_INTERNAL_GRPC" \
      kacho.cloud.iam.v1.InternalUserService/UpsertFromIdentity 2>&1)
    # Дождаться завершения upsert-Operation — её worker создаёт bootstrap-Account
    # и пишет FGA-tuple'ы. Без этого Account.Create ниже ловит authz race.
    op_id=$(echo "$resp" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))' 2>/dev/null || true)
    if [ -n "$op_id" ]; then
      poll_op "$op_id" "$JWT_BOOTSTRAP" >/dev/null 2>&1 || true
    fi
    user_id=$(echo "$resp" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(((d.get("metadata") or {}).get("userId","")))' 2>/dev/null || true)
    if [ -z "$user_id" ]; then
      # PENDING-row может быть активирован — get_by_email через grpc.
      user_id=$(grpcurl $GRPCURL_TLS -d "{\"email\":\"$email\"}" "$IAM_INTERNAL_GRPC" \
        kacho.cloud.iam.v1.InternalIAMService/LookupSubject 2>/dev/null \
        | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d.get("subjectId",""))' 2>/dev/null || true)
    fi
    [ -n "$user_id" ] && break
    sleep 3
  done
  echo "$user_id"
}

USER_BOOT=$(upsert_user_grpc "admin@prorobotech.ru"                  "admin@prorobotech.ru"                  "Bootstrap Admin")
USER_NOB=$(upsert_user_grpc "auth-test-no-bindings@example.com"     "auth-test-no-bindings@example.com"     "AuthZ NoBindings")
# kacho-iam#276 pure leak-guard subject — authenticated, NO membership, NEVER granted
# by any suite (distinct from userNOB which the access-binding suites do grant).
USER_PURE_NOB=$(upsert_user_grpc "auth-test-pure-no-bindings@example.com" "auth-test-pure-no-bindings@example.com" "AuthZ PureNoBindings")
USER_PA1=$(upsert_user_grpc "auth-test-proj-admin-a1@example.com"   "auth-test-proj-admin-a1@example.com"   "AuthZ ProjAdminA1")
USER_AAA=$(upsert_user_grpc "auth-test-account-admin-a@example.com" "auth-test-account-admin-a@example.com" "AuthZ AccountAdminA")
USER_AAB=$(upsert_user_grpc "auth-test-account-admin-b@example.com" "auth-test-account-admin-b@example.com" "AuthZ AccountAdminB")
USER_INV=$(upsert_user_grpc "auth-test-invitee@example.com"         "auth-test-invitee@example.com"         "AuthZ Invitee")
log "    users: BOOT=$USER_BOOT NOB=$USER_NOB PURE_NOB=$USER_PURE_NOB PA1=$USER_PA1 AAA=$USER_AAA AAB=$USER_AAB INV=$USER_INV"

# Fail-fast — a missing user id (grpcurl could not reach kacho-iam-internal, or
# UpsertFromIdentity/LookupSubject errored) silently cascades into empty
# subjectId on every AccessBinding and a stack that "passes" with the wrong
# authz state. Surface it here with an actionable message instead of producing
# a misleading newman run.
for _pair in "BOOT:$USER_BOOT" "NOB:$USER_NOB" "PURE_NOB:$USER_PURE_NOB" "PA1:$USER_PA1" \
             "AAA:$USER_AAA" "AAB:$USER_AAB" "INV:$USER_INV"; do
  if [ -z "${_pair#*:}" ]; then
    echo "[setup] FATAL: user ${_pair%%:*} resolved to an empty id — UpsertFromIdentity/LookupSubject failed." >&2
    echo "[setup]        Check IAM_INTERNAL_GRPC=$IAM_INTERNAL_GRPC is reachable via grpcurl and kacho-iam is up." >&2
    exit 1
  fi
done

# 3) Accounts (idempotent by name).
#
# KAC-127 Phase 3 authz contract: `AccountService.Create` enforces
# `RequireOwnerMatchesPrincipal` (KAC-122 CRIT-3 anti-hijacking) — the
# authenticated principal MUST equal `owner_user_id`. A user can only create
# an Account owned by themselves. Therefore each account is created using the
# *owner's* own JWT, not the bootstrap-admin token. (Previously the fixture
# created every account as BOOT → `code 7 permission denied` on the first
# Account.Create, which aborted the whole fixture run.)
log "3/10 ensuring accounts authz-test-A / authz-test-B"
find_account_by_name() {
  local name="$1" token="$2"
  api GET "/iam/v1/accounts" "$token" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); n=[x for x in (d.get('accounts') or []) if x.get('name')=='$name']; print(n[0].get('id','') if n else '')" 2>/dev/null || true
}

# ensure_account — create (idempotent) an Account owned by `owner`, acting as
# `owner_token` (the owner's own JWT). owner_token's principal == owner so the
# anti-hijacking guard passes.
ensure_account() {
  local name="$1" desc="$2" owner="$3" owner_token="$4"
  local found
  found=$(find_account_by_name "$name" "$owner_token")
  if [ -n "$found" ]; then echo "$found"; return; fi
  local body op
  # redesign-2026 F1: ownerUserId is OUTPUT-ONLY (derived from the authenticated
  # caller). Supplying ANY value — even the caller's own id — is rejected sync
  # with INVALID_ARGUMENT "Illegal argument ownerUserId (derived from caller)".
  # owner_token's principal IS $owner, so the derived owner equals $owner anyway.
  body=$(printf '{"name":"%s","description":"%s"}' "$name" "$desc")
  op=$(api POST "/iam/v1/accounts" "$owner_token" "$body")
  local op_id; op_id=$(echo "$op" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))' 2>/dev/null)
  if [ -z "$op_id" ]; then echo "[setup] FATAL: Account.Create vernuli no id: $op" >&2; return 1; fi
  poll_op "$op_id" "$owner_token" \
    | python3 -c 'import sys,json; d=json.load(sys.stdin); print((d.get("metadata") or {}).get("accountId",""))'
}
ACCOUNT_A=$(ensure_account "authz-test-a" "KAC-122 fixture (account-admin-A home)" "$USER_AAA" "$JWT_AAA")
ACCOUNT_B=$(ensure_account "authz-test-b" "KAC-122 fixture (account-admin-B home + invitee home)" "$USER_AAB" "$JWT_AAB")
log "    accounts: A=$ACCOUNT_A B=$ACCOUNT_B"

# 4) Projects.
#
# `ProjectService.Create` is gated by the api-gateway authz-mw with
# `required_relation: editor` on `account:<account_id>`. The account owner
# holds `owner` (⊇ `editor`) on their account via the FGA owner-tuple written
# by Account.Create — so projects are created acting as the owning account's
# owner JWT.
log "4/10 ensuring projects A1 / A2 / B1"
find_project_by_name_account() {
  local name="$1" acct="$2" token="$3"
  api GET "/iam/v1/projects?accountId=$acct" "$token" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); n=[x for x in (d.get('projects') or []) if x.get('name')=='$name']; print(n[0].get('id','') if n else '')" 2>/dev/null || true
}
ensure_project() {
  local name="$1" acct="$2" desc="$3" owner_token="$4"
  local found
  found=$(find_project_by_name_account "$name" "$acct" "$owner_token")
  if [ -n "$found" ]; then echo "$found"; return; fi
  local body op op_id done_op pid attempt=0
  body=$(printf '{"accountId":"%s","name":"%s","description":"%s"}' "$acct" "$name" "$desc")
  # Bounded retry over TWO transient cold-start windows:
  #   (1) owner-binding materialization: a FRESH account's owner AccessBinding (→ `editor`
  #       on the account, which iam.projects.create requires) is eventually-consistent, so
  #       the first Project.Create right after Account.Create can 403 AUTHZ_DENIED (no op id).
  #   (2) phantom-project: a Create can finish done:true WITH result.error (transient FGA/DB
  #       blip at cold-start) while metadata.projectId still carries the PRE-ALLOCATED id —
  #       the row never committed. Trusting that id yields a phantom that FGA-binds green but
  #       fails every cross-service ProjectService.Get (NOT_FOUND cascade: "Project prj… not
  #       found"). We MUST assert !op.error before trusting the id (vault: op.error-before-
  #       metadata; testing.md e2e-invariant). The failed row did not commit, so re-Create
  #       won't collide with name-UNIQUE — retry it.
  # ~8 attempts ×0.75s ≈ 6s per window; a NON-transient failure FATALs (never mask a real error).
  while :; do
    op=$(api POST "/iam/v1/projects" "$owner_token" "$body")
    op_id=$(echo "$op" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))')
    if [ -z "$op_id" ]; then
      attempt=$((attempt+1))
      if [ "$attempt" -ge 8 ] || ! echo "$op" | grep -q 'AUTHZ_DENIED\|permission denied'; then
        echo "[setup] FATAL: Project.Create returned no id: $op" >&2; return 1
      fi
      sleep 0.75; continue
    fi
    done_op=$(poll_op "$op_id" "$owner_token")
    pid=$(echo "$done_op" | python3 -c 'import sys,json; d=json.load(sys.stdin); print((d.get("metadata") or {}).get("projectId",""))' 2>/dev/null)
    # Operation oneof result.error (REST maps to top-level `error` or nested `result.error`).
    if echo "$done_op" | python3 -c 'import sys,json
d=json.load(sys.stdin)
sys.exit(0 if (d.get("error") or (d.get("result") or {}).get("error")) else 1)' 2>/dev/null; then
      attempt=$((attempt+1))
      if [ "$attempt" -ge 8 ]; then
        echo "[setup] FATAL: Project.Create op finished with error (phantom id $pid): $(echo "$done_op" | head -c 200)" >&2; return 1
      fi
      sleep 0.75; continue
    fi
    echo "$pid"; return
  done
}
PROJECT_A1=$(ensure_project "authz-test-a1" "$ACCOUNT_A" "KAC-122 fixture (project-admin-A1 home)" "$JWT_AAA")
PROJECT_A2=$(ensure_project "authz-test-a2" "$ACCOUNT_A" "KAC-122 fixture (cross-project in same account)" "$JWT_AAA")
PROJECT_B1=$(ensure_project "authz-test-b1" "$ACCOUNT_B" "KAC-122 fixture (cross-account)" "$JWT_AAB")
log "    projects: A1=$PROJECT_A1 A2=$PROJECT_A2 B1=$PROJECT_B1"

# fixture-sync guard (diagnostic, NON-fatal). ensure_project extracts
# metadata.projectId from the completed Create Operation WITHOUT checking op.error: a
# Create can finish done:true WITH an error (transient FGA/DB blip at cold-start) while
# metadata.projectId still carries the pre-allocated id. That id then gets FGA binding
# tuples (ensure_binding below → gateway authz passes on the tuple), but the project ROW
# never committed, so the cross-service peer-check (vpc/compute → iam ProjectService.Get)
# returns NOT_FOUND → label-revoke-{vpc,compute} cascaded RED (ci round-3 root cause:
# "Project prj… not found" / "Folder with id prj… not found"). The label-revoke suites
# now SELF-SEED their own project per case (cases/label-revoke-{vpc,compute}.py
# create_suite_project), so a phantom projectA1 no longer breaks them — this block only
# surfaces the phantom loudly instead of letting it hide behind green FGA tuples. GET is
# read-only; python takes the id via argv (no shell-into-code interpolation); every step
# is `|| echo 0`/`2>/dev/null` guarded so it can never abort the fixture under set -e.
if [ -n "$PROJECT_A1" ]; then
  _pa1_ok=$(api GET "/iam/v1/projects/$PROJECT_A1" "$JWT_AAA" 2>/dev/null \
    | python3 -c 'import sys,json
try:
    print("1" if json.load(sys.stdin).get("id") == sys.argv[1] else "0")
except Exception:
    print("0")' "$PROJECT_A1" 2>/dev/null || echo 0)
  if [ "$_pa1_ok" != "1" ]; then
    log "WARN: projectA1 ($PROJECT_A1) does NOT resolve via ProjectService.Get — PHANTOM (IAM row never committed). Shared-tenant suites keyed on {{projectA1Id}} would see cross-service NOT_FOUND; re-check ensure_project op.error handling."
  fi
fi

# 4b) KAC-132: Clean up stale NOB viewer bindings on account-A and account-B.
#
# The authz-deny newman suite's AB-CR ALLOW cases (AB-CR-A-AAA, AB-CR-B-AAB,
# AB-CR-B-INV) grant userNOB a `viewer` role on account-A and account-B to
# test AccessBinding.Create authorization.  Those bindings are written to
# OpenFGA as tuples; since OpenFGA is backed by PostgreSQL they persist across
# runs. On the next run the matrix still expects NOB → DENY on those accounts,
# but OpenFGA sees the lingering tuple and returns ALLOW → 24 assertion
# failures (ACCT-GT, PRJ-GT, PRJ-LS, GRP-LS sub-cases for NOB).
#
# Fix: delete any AccessBindings for NOB on account-A and account-B before
# re-seeding so each run starts with a clean NOB state. The delete is
# best-effort (if binding doesn't exist, the DELETE will 404 via
# ListBySubject → no bindings found → nothing to delete; harmless).
#
# We use ListBySubject to discover any existing NOB bindings on those two
# accounts, then DELETE each (wait for ops to complete so FGA tuples are
# removed before the newman run starts).
log "4b/10 cleaning up stale NOB viewer bindings on account-A / account-B (KAC-132)"

delete_binding_if_exists() {
  local subject_id="$1" resource_type="$2" resource_id="$3" grantor_token="$4"
  # ListByScope находит binding'и скоупа, дальше фильтруем по субъекту клиентски
  # (плоского GET /accessBindings у AccessBindingService нет — только :listByScope
  #  и :listBySubject).
  #
  # НЕ :listByResource — этот роут УДАЛЁН (RPC переименован в ListByScope, wire-имя
  # снято; kacho#1 локает это кейсом IAM-ACB-F50-ROUTES-REMOVED: legacy-путь обязан
  # отдавать 403 catalog-miss). Обращение к нему возвращало 403 с ВАЛИДНЫМ JSON'ом
  # ошибки, python его успешно парсил, `.get('accessBindings', [])` давал пустой
  # список — и очистка молча не удаляла НИЧЕГО. Мусорные binding'и от прошлых
  # прогонов копились, ломая кейсы, которые ждут чистое состояние.
  local resp ab_ids
  resp=$(api GET "/iam/v1/accessBindings:listByScope?resourceType=${resource_type}&resourceId=${resource_id}" "$grantor_token" 2>/dev/null || true)
  ab_ids=$(echo "$resp" | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
for ab in d.get('accessBindings', []):
    if ab.get('subjectId', '') == '$subject_id':
        abid = ab.get('id', '')
        if abid:
            print(abid)
" 2>/dev/null || true)
  if [ -z "$ab_ids" ]; then
    return 0  # nothing to delete
  fi
  while IFS= read -r ab_id; do
    [ -z "$ab_id" ] && continue
    local del_resp del_op_id
    del_resp=$(api DELETE "/iam/v1/accessBindings/${ab_id}" "$grantor_token" 2>/dev/null || true)
    del_op_id=$(echo "$del_resp" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))' 2>/dev/null || true)
    if [ -n "$del_op_id" ]; then
      poll_op "$del_op_id" "$grantor_token" >/dev/null 2>&1 || true
      log "    deleted stale NOB binding $ab_id on ${resource_type}:${resource_id}"
    fi
  done <<< "$ab_ids"
}

# Delete any NOB viewer bindings on account-A (grantor = AAA, who owns A).
delete_binding_if_exists "$USER_NOB" "account" "$ACCOUNT_A" "$JWT_AAA"
# Delete any NOB viewer bindings on account-B (grantor = AAB, who owns B).
delete_binding_if_exists "$USER_NOB" "account" "$ACCOUNT_B" "$JWT_AAB"

# 5) AccessBindings (idempotent via 5-tuple per KAC-112 §13.4).
#
# `AccessBindingService.Create` enforces `requireGrantAuthority`: the caller
# must own the owning Account of the grant scope (or hold FGA `admin` on it).
# Bindings are therefore created acting as the owning account's owner JWT:
#   - scope account/project under account A → JWT_AAA (owner of A)
#   - scope account/project under account B → JWT_AAB (owner of B)
log "5/10 ensuring access bindings"
# ensure_binding — create an AccessBinding and **wait for its Operation to
# finish**. AccessBinding.Create is async (returns an Operation); the FGA
# relation tuple (`<scope>#<relation>@<subject>`) that the api-gateway authz
# Check resolves against is written inside the Operation worker. Without
# polling, the newman matrix could race the tuple-write. We also surface a
# non-Operation response (e.g. a 403 from requireGrantAuthority) instead of
# silently dropping it — a missing binding makes the granted subject look
# unauthorised (`no path`).
ensure_binding() {
  local subject_id="$1" role_id="$2" resource_type="$3" resource_id="$4" grantor_token="$5"
  local body resp op_id
  # redesign-2026: AccessBinding.Create renamed resource_type/resource_id →
  # scope_type/scope_id (scope_type must be DOTTED: iam.account/iam.project/
  # iam.cluster) and added a REQUIRED `target`. Fixtures grant whole-anchor
  # access → target.allInScope{}. Callers pass bare project/account → dot-prefix.
  local scope_type_dotted="iam.${resource_type}"
  body=$(printf '{"subjectType":"user","subjectId":"%s","roleId":"%s","scopeType":"%s","scopeId":"%s","target":{"allInScope":{}}}' \
    "$subject_id" "$role_id" "$scope_type_dotted" "$resource_id")
  resp=$(api POST "/iam/v1/accessBindings" "$grantor_token" "$body")
  op_id=$(echo "$resp" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))' 2>/dev/null || true)
  if [ -z "$op_id" ]; then
    echo "[setup] WARN AccessBinding.Create ($subject_id $role_id $resource_type:$resource_id) no Operation: $(echo "$resp" | head -c 200)" >&2
    return 0
  fi
  local done_op err_msg
  done_op=$(poll_op "$op_id" "$grantor_token" 2>/dev/null || true)
  err_msg=$(echo "$done_op" | python3 -c 'import sys,json
try:
    d=json.load(sys.stdin)
except Exception:
    sys.exit(0)
e=d.get("error") or (d.get("result") or {}).get("error") or {}
if e:
    print("code=%s message=%s" % (e.get("code"), e.get("message")))' 2>/dev/null || true)
  if [ -n "$err_msg" ]; then
    echo "[setup] WARN AccessBinding.Create op $op_id error ($subject_id $role_id $resource_type:$resource_id): $err_msg" >&2
  fi
}
# System role IDs — source of truth = migration kacho_iam `0008_role_catalog_kac122.sql`.
# That migration DELETEs the legacy `rol00000000000000<tail>` roles seeded by 0001
# and re-seeds the catalog with deterministic ids `rol` + substr(md5(<name>),1,17).
# An AccessBinding.Create with a non-existent role_id fails the worker with
# `FAILED_PRECONDITION Role <id> not found` (FK access_bindings_role_fk), so the
# fixture MUST use the post-0008 ids — see KAC-127.
#
# The fixture grants are domain-wide "account admin" / "project editor"; the
# post-0008 catalog has no domain-wide roles, so we map to the 3 global
# wildcard roles (acceptance-equivalent — admin ⊇ editor ⊇ viewer cascade):
#   admin = rol+md5('admin')[:17]  → FGA relation `admin`
#   edit  = rol+md5('edit')[:17]   → FGA relation `editor`
#   view  = rol+md5('view')[:17]   → FGA relation `viewer`
ROLE_ADMIN="rol21232f297a57a5a74"   # md5('admin')[:17]  — global super-admin
ROLE_EDIT="rolde95b43bceeb4b998"    # md5('edit')[:17]   — global edit-only
ROLE_VIEW="rol1bda80f2be4d3658e"    # md5('view')[:17]   — global read-only

# PA1 — project-A1 editor. Grantor = AAA (owns A → A1).
ensure_binding "$USER_PA1" "$ROLE_EDIT" "project" "$PROJECT_A1" "$JWT_AAA"
# AAA — account-A admin. Grantor = AAA (owner of A — self-grant on own account).
ensure_binding "$USER_AAA" "$ROLE_ADMIN" "account" "$ACCOUNT_A" "$JWT_AAA"
# AAA — EXPLICIT project-A1 editor (deterministic precondition for cross-service
# label-revoke). AAA owns account-A (owner ⊇ editor on account:A) and the FGA
# account-owner → project-editor hierarchy SHOULD cascade `editor` onto project:A1.
# But the umbrella label-revoke suites (label-revoke-{vpc,compute,nlb}) create
# cross-service resources in project-A1 as AAA — vpc.networks.create /
# compute.disks.create — which the gateway authz-mw gates with
# required_relation=editor on project:A1. Relying on the HIERARCHICAL cascade for
# AAA flaked to a persistent 403 in umbrella CI ("subject user:<AAA> lacks relation
# \"editor\" on project:<A1> ... no direct relations granted" — observed on 465/465
# create attempts across a full run, i.e. NOT a materialization tail) → the whole
# label-revoke flow cascaded RED on an unset resource var. Guarantee the precondition
# EXPLICITLY (idempotent — same belt-and-suspenders as the PA1 line above and the INV
# project-A1 editor below) so the label-revoke ASSERTIONS (the actual subject-under-
# test: ARM_LABELS grant revoke-on-label-change) run instead of dying on the create
# precondition. An EXPLICIT project binding materializes reliably (proven by PA1/INV);
# whether the account-owner→project-editor cascade itself is a product gap is tracked
# separately (does not block the label-revoke regression signal).
ensure_binding "$USER_AAA" "$ROLE_EDIT" "project" "$PROJECT_A1" "$JWT_AAA"
# AAB — account-B admin. Grantor = AAB (owner of B).
ensure_binding "$USER_AAB" "$ROLE_ADMIN" "account" "$ACCOUNT_B" "$JWT_AAB"
# INV — owner-of-B (his home) — admin in account-B. Grantor = AAB (owner of B).
ensure_binding "$USER_INV" "$ROLE_ADMIN" "account" "$ACCOUNT_B" "$JWT_AAB"

# 5b) BOOT — cluster-admin via the PRODUCT bootstrap reconciler (no SQL backdoor).
#
# AccessBindingService.Create requires the caller to already hold `system_admin`
# on the cluster scope (CLAUDE.md §«Запреты» #10 — atomic CAS). Bootstrap (the
# very first cluster-admin) is chicken-and-egg via the public API; the
# production path is `seed.RunBootstrapAdmin`, now WIRED from
# `cmd/kacho-iam/serve.go` as a startup reconciler driven by
# KACHO_IAM_BOOTSTRAP_ROOT_EMAIL=admin@prorobotech.ru (Bug B fix). It grants
# `system_admin@cluster_kacho_root` and enqueues the FGA tuple through the
# transactional fga_outbox → drainer → OpenFGA, the same path every
# AccessBinding uses (no raw INSERT that bypasses the drainer).
#
# The bootstrap user row appears only after the upsert above (step ~4); the
# reconciler retries on a 10s interval and converges shortly after. We poll the
# FGA tuple's effect via a cluster-scope readiness probe so the cluster newman
# cases don't race the reconciler.
log "5b/10 awaiting product bootstrap reconciler (system_admin@cluster via fga_outbox)"
if [ -n "$JWT_BOOTSTRAP" ] && [ -n "$ACCOUNT_A" ]; then
  boot_ok=""
  boot_iters=$(( CLUSTER_ADMIN_WAIT_SECS / 2 )); [ "$boot_iters" -lt 1 ] && boot_iters=1
  for i in $(seq 1 "$boot_iters"); do
    # :listByScope(cluster) от bootstrap-админа отдаёт 200, как только tuple
    # system_admin@cluster от reconciler'а долетел до OpenFGA.
    #
    # НЕ :listByResource — роут УДАЛЁН (см. delete_binding_if_exists выше). Проба по
    # нему получала 403 catalog-miss ВСЕГДА, независимо от готовности → выжигала весь
    # бюджет CLUSTER_ADMIN_WAIT_SECS (180с) на КАЖДОМ прогоне и заканчивалась
    # «WARN: cluster cases may fail». Проба, которая не может стать зелёной, не
    # проверяет готовность — она измеряет только собственный таймаут.
    code=$(api_status GET "/iam/v1/accessBindings:listByScope?resourceType=cluster&resourceId=cluster_kacho_root" "$JWT_BOOTSTRAP" 2>/dev/null || echo 000)
    if [ "$code" = "200" ]; then boot_ok=1; log "    bootstrap cluster-admin ready (${i}x2s, code=200)"; break; fi
    sleep 2
  done
  [ -z "$boot_ok" ] && log "    WARN: bootstrap cluster-admin not ready after ${CLUSTER_ADMIN_WAIT_SECS}s (last code=$code) — cluster cases may fail"
else
  log "    WARN: skipping bootstrap readiness probe (JWT_BOOTSTRAP or ACCOUNT_A empty)"
fi

# 5c) CIL0: super-admin фикстуры должен держать и system_viewer@cluster.
# `system_viewer` (cluster-system read) НЕ выводится из `system_admin` в FGA-модели
# (намеренно: без wildcard user:*), продуктового reconciler'а для него нет — сидим
# fixture-side через fga_outbox (drainer применит). Нужно для internal-RPC,
# гейтящихся read-tier system_viewer (vpc InternalNetworkService.GetNetwork → vrf_id).
SV_NS="${SETUP_NS:-kacho}"
SV_PG_POD="${PG_POD:-kacho-umbrella-pg-iam-0}"
SV_PG_PW=$(kubectl -n "$SV_NS" get secret kacho-umbrella-pg-iam -o jsonpath='{.data.password}' 2>/dev/null | base64 -d || true)
if [ -n "$SV_PG_PW" ] && [ -n "$USER_BOOT" ]; then
  kubectl -n "$SV_NS" exec "$SV_PG_POD" -- env PGPASSWORD="$SV_PG_PW" psql -h localhost -U iam -d kacho_iam -tAc "
    INSERT INTO kacho_iam.fga_outbox (event_type, payload, created_at)
    SELECT 'fga.tuple.write',
           jsonb_build_object('user','user:$USER_BOOT','relation','system_viewer','object','cluster:cluster_kacho_root'),
           now()
    WHERE NOT EXISTS (
      SELECT 1 FROM kacho_iam.fga_outbox
       WHERE payload->>'user'='user:$USER_BOOT'
         AND payload->>'relation'='system_viewer'
         AND payload->>'object'='cluster:cluster_kacho_root'
    );
  " >/dev/null 2>&1 || log "    WARN: system_viewer seed failed (idempotent or schema mismatch)"
  log "    BOOT($USER_BOOT) → system_viewer@cluster:cluster_kacho_root (fga_outbox seed)"
else
  log "    WARN: skipping system_viewer seed (PG access or USER_BOOT empty)"
fi

# 6) INV invite-flow (KAC-125): AAA invites INV into account-A as editor on project-A1.
log "6/10 invite INV to account-A (KAC-125)"
invite_body=$(printf '{"accountId":"%s","email":"auth-test-invitee@example.com","roleId":"%s","projectId":"%s"}' "$ACCOUNT_A" "$ROLE_EDIT" "$PROJECT_A1")
invite_resp=$(api POST "/iam/v1/users:invite" "$JWT_AAA" "$invite_body" 2>&1 || true)
if echo "$invite_resp" | grep -q '"id":'; then
  invite_op_id=$(echo "$invite_resp" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))' 2>/dev/null || true)
  if [ -n "$invite_op_id" ]; then poll_op "$invite_op_id" "$JWT_AAA" >/dev/null || true; fi
else
  log "    WARN: Invite RPC response unexpected (KAC-125 REST mapping может отсутствовать): $(echo "$invite_resp" | head -c 200)"
  log "    INV получит admin@accountB напрямую через AccessBinding (вместо invite через project-A1)"
fi
# Re-upsert INV by external-id to activate PENDING-row (KAC-125 D-7).
upsert_user_grpc "auth-test-invitee@example.com" "auth-test-invitee@example.com" "AuthZ Invitee" >/dev/null
# Deterministic fixture state for the AUTHZ-PRJ-UP-A1-INV matrix ALLOW case: INV must
# hold editor on project-A1. The invite above is the intended mechanism, but its REST
# mapping is best-effort (KAC-125) — so guarantee the required binding explicitly
# (idempotent: if the invite already granted it, this returns "already granted"). The
# invite MECHANISM itself is covered separately by the iam-invite-grant-fga collection.
[ -n "$USER_INV" ] && ensure_binding "$USER_INV" "$ROLE_EDIT" "project" "$PROJECT_A1" "$JWT_AAA"

# 7) Seed VPC networks in A1 + B1.
#
# `vpc.NetworkService.Create` is gated by the api-gateway authz-mw with
# `required_relation: editor` on `project:<project_id>`. The owning account's
# owner cascades to `editor` on every project of the account (FGA hierarchy
# project→account + account#owner). Networks are therefore seeded acting as
# the owning account's owner JWT (A1 → AAA, B1 → AAB).
log "7/10 ensuring seed VPC networks"
ensure_network() {
  local proj="$1" name="$2" token="$3"
  local found
  found=$(api GET "/vpc/v1/networks?projectId=$proj" "$token" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); n=[x for x in (d.get('networks') or []) if x.get('name')=='$name']; print(n[0].get('id','') if n else '')" 2>/dev/null || true)
  if [ -n "$found" ]; then echo "$found"; return; fi
  local body op op_id
  body=$(printf '{"projectId":"%s","name":"%s","description":"KAC-122 seed for GET probes"}' "$proj" "$name")
  op=$(api POST "/vpc/v1/networks" "$token" "$body")
  op_id=$(echo "$op" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))' 2>/dev/null)
  if [ -z "$op_id" ]; then echo "[setup] WARN Network.Create failed: $op" >&2; echo ""; return; fi
  poll_op "$op_id" "$token" \
    | python3 -c 'import sys,json; d=json.load(sys.stdin); print((d.get("metadata") or {}).get("networkId",""))'
}
SEED_NET_A1=$(ensure_network "$PROJECT_A1" "authz-seed-net-a1" "$JWT_AAA")
SEED_NET_B1=$(ensure_network "$PROJECT_B1" "authz-seed-net-b1" "$JWT_AAB")
log "    seed networks: A1=$SEED_NET_A1 B1=$SEED_NET_B1"

# 9) KAC-127 model 5-6 — ServiceAccounts + SA-keys (Hydra OAuth clients).
#
# ServiceAccounts/SA-keys live under account A → created acting as AAA
# (`ServiceAccountService.Create` needs `editor` on `account:A`; `SAKeyService`
# needs `editor` on `iam_service_account:<sva>`, which cascades from the
# owning account's owner).
log "9/10 ensuring ServiceAccounts + SA-keys (KAC-127 models 5-6)"

# SA-A — granted SA (vpc-editor on project-A1). SANG — no-grant SA.
find_sa_by_name() {
  local name="$1" acct="$2" token="$3"
  api GET "/iam/v1/serviceAccounts?accountId=$acct" "$token" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); n=[x for x in (d.get('serviceAccounts') or []) if x.get('name')=='$name']; print(n[0].get('id','') if n else '')" 2>/dev/null || true
}
ensure_sa() {
  local name="$1" acct="$2" token="$3"
  local found
  found=$(find_sa_by_name "$name" "$acct" "$token")
  if [ -n "$found" ]; then echo "$found"; return; fi
  local body op op_id
  body=$(printf '{"accountId":"%s","name":"%s","description":"KAC-127 authz fixture"}' "$acct" "$name")
  op=$(api POST "/iam/v1/serviceAccounts" "$token" "$body")
  op_id=$(echo "$op" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))' 2>/dev/null)
  if [ -z "$op_id" ]; then echo "[setup] WARN ServiceAccount.Create failed: $op" >&2; echo ""; return; fi
  poll_op "$op_id" "$token" \
    | python3 -c 'import sys,json; d=json.load(sys.stdin); print((d.get("metadata") or {}).get("serviceAccountId",""))'
}
SVA_A=$(ensure_sa "authz-sa-a" "$ACCOUNT_A" "$JWT_AAA")
SVA_NOGRANT=$(ensure_sa "authz-sa-nogrant" "$ACCOUNT_A" "$JWT_AAA")
log "    service accounts: A=$SVA_A NOGRANT=$SVA_NOGRANT"

# Grant SA-A vpc-editor on project-A1 (subject_type=service_account).
# Grantor = AAA (owner of account A → A1).
ensure_sa_binding() {
  local subject_id="$1" role_id="$2" resource_type="$3" resource_id="$4" grantor_token="$5"
  local body resp op_id
  # redesign-2026 scope_type(dotted)/scope_id/target — see ensure_binding above.
  local scope_type_dotted="iam.${resource_type}"
  body=$(printf '{"subjectType":"service_account","subjectId":"%s","roleId":"%s","scopeType":"%s","scopeId":"%s","target":{"allInScope":{}}}' \
    "$subject_id" "$role_id" "$scope_type_dotted" "$resource_id")
  resp=$(api POST "/iam/v1/accessBindings" "$grantor_token" "$body")
  op_id=$(echo "$resp" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))' 2>/dev/null || true)
  if [ -z "$op_id" ]; then
    echo "[setup] WARN SA AccessBinding.Create no Operation: $(echo "$resp" | head -c 200)" >&2
    return 0
  fi
  poll_op "$op_id" "$grantor_token" >/dev/null 2>&1 || true
}
ensure_sa_binding "$SVA_A" "$ROLE_EDIT" "project" "$PROJECT_A1" "$JWT_AAA"
# SVA_NOGRANT — intentionally NO bindings (model 5 negative).

# Issue SA-key (Hydra OAuth client) for SA-A via SAKeyService.Issue.
# `client_secret` returned ONCE; не персистится, в env не кладётся.
# Acting as AAA — `editor` on `iam_service_account:SVA_A` cascades from
# AAA's ownership of account A.
issue_sa_key() {
  local sva_id="$1"
  local body op op_id
  body=$(printf '{"serviceAccountId":"%s","description":"KAC-127 authz fixture key","createdByUserId":"%s"}' \
    "$sva_id" "$USER_AAA")
  op=$(api POST "/iam/v1/serviceAccounts/${sva_id}/keys" "$JWT_AAA" "$body")
  op_id=$(echo "$op" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))' 2>/dev/null || true)
  if [ -z "$op_id" ]; then
    log "    WARN SAKeyService.Issue вернул не Operation (proto может быть не зарегистрирован): $(echo "$op" | head -c 160)"
    echo ""
    return
  fi
  poll_op "$op_id" "$JWT_AAA" \
    | python3 -c 'import sys,json; d=json.load(sys.stdin); print((d.get("metadata") or {}).get("keyId",""))' 2>/dev/null || true
}
SA_KEY_A=$(issue_sa_key "$SVA_A")
log "    SA-key for SA-A: keyId=$SA_KEY_A (client_secret returned once — НЕ персистится)"

# 10) Mint SA + API tokens (dev-mode HS256 equivalents of Hydra-issued JWTs).
#     Реальный client_credentials grant — Hydra /oauth2/token; на стенде
#     api-gateway authn dev-mode принимает HS256 dev-secret JWT, поэтому
#     SA-токен моделируется minter'ом с kacho_principal_type=service_account.
log "10/10 minting SA + API tokens (KAC-127 models 5-6)"
EXP_SECONDS=$((EXP_HOURS * 3600))
JWT_SAA=$(python3 "$SCRIPT_DIR/setup-jwt.py" --secret "$DEV_SECRET" --sa "$SVA_A" --exp-seconds "$EXP_SECONDS")
JWT_SANG=$(python3 "$SCRIPT_DIR/setup-jwt.py" --secret "$DEV_SECRET" --sa "$SVA_NOGRANT" --exp-seconds "$EXP_SECONDS")
API_TOKEN_VALID=$(python3 "$SCRIPT_DIR/setup-jwt.py" --secret "$DEV_SECRET" --api-token "$SVA_A" \
  --scope "vpc.* project:$PROJECT_A1" --exp-seconds "$EXP_SECONDS")
# Expired API token — exp 1h в прошлом.
API_TOKEN_EXPIRED=$(python3 "$SCRIPT_DIR/setup-jwt.py" --secret "$DEV_SECRET" --api-token "$SVA_A" \
  --scope "vpc.* project:$PROJECT_A1" --exp-seconds "-3600")
# Revoked API token (KAC-127 Bug-3).
#
# A real revoked token, in production, is one whose backing SA-key was deleted
# via SAKeyService.Revoke; the api-gateway rejects it at the authn layer (Hydra
# token introspection reports the token inactive → 401). The dev stand does
# NOT run Hydra introspection — its api-gateway authn is HS256 dev-secret JWT
# verification (a stand-in for Hydra-issued tokens). The faithful dev-stand
# model of "this token is no longer accepted" is therefore a token the gateway
# rejects at that same authn layer: an HS256 JWT whose signature does not
# verify against the gateway dev-secret → validateJWT fails → 401
# UNAUTHENTICATED — exactly the matrix's revoked-token contract (authN fails
# before authZ, same outcome class as a real revoked token). It is minted with
# a DIFFERENT signing key so the signature is genuinely invalid.
#
# This is NOT a weakening of authn: a revoked token must not authenticate, and
# signature rejection is precisely the authn-layer outcome. The real SA-key
# Issue+Revoke below still runs — it exercises the SAKeyService RPC path and
# leaves the store in the post-revoke state the production model describes.
API_TOKEN_REVOKED=$(python3 "$SCRIPT_DIR/setup-jwt.py" --secret "${DEV_SECRET}-revoked-not-the-signing-key" \
  --api-token "$SVA_A" --scope "vpc.* project:$PROJECT_A1" --exp-seconds "$EXP_SECONDS")
if [ -n "$SA_KEY_A" ]; then
  # Revoke acts as AAA — `editor` on `iam_service_account:SVA_A` (cascade
  # from account A ownership), same authority as Issue. Exercises the real
  # SAKeyService.Revoke RPC (key row + Hydra client deletion).
  api DELETE "/iam/v1/serviceAccounts/${SVA_A}/keys/${SA_KEY_A}" "$JWT_AAA" >/dev/null 2>&1 || true
  log "    SA-key $SA_KEY_A revoked via SAKeyService.Revoke (apiTokenRevoked → expect 401)"
fi
# Malformed token — синтаксически битый JWS (2 сегмента вместо 3).
API_TOKEN_MALFORMED="eyJhbGciOiJIUzI1NiJ9.bm90LWEtcmVhbC10b2tlbg"

# ===========================================================================
# 11-13) Phase B — per-service seed blocks (compute / vpc-list-filter-d / nlb).
#
# Historically only iam+vpc got real fixtures; compute/nlb newman env-файлы
# несли ХАРДКОД placeholder-id (`b1gc03…`, `e9bcomputeseedsub001`, пустые
# nlb subject-JWT), которых на чистом kind НИКТО не создаёт → compute
# create-наборы упирались в NOT_FOUND/UNAVAILABLE, nlb authz-наборы — в 401.
# Ниже мы создаём РЕАЛЬНЫЕ ресурсы под каждый сервис и патчим ТОЛЬКО его env
# (таргетно, чтобы `existingProjectId` одного сервиса не затирал другой).
# Всё идемпотентно (find-by-name / upsert / WHERE NOT EXISTS).
# ===========================================================================

# --- shared helpers for Phase B blocks -------------------------------------

# ensure_subnet <project> <network> <name> <zone> <cidr> <token> → subnetId.
# redesign placement-coherence: placement_type is SERVER-DERIVED — set zoneId
# (→ ZONAL) or regionId (→ REGIONAL), never placement_type itself (sending it →
# InvalidArgument "placement_type is server-derived; set zone_id or region_id").
ensure_subnet() {
  local proj="$1" net="$2" name="$3" zone="$4" cidr="$5" token="$6"
  local found
  found=$(api GET "/vpc/v1/subnets?projectId=$proj&pageSize=1000" "$token" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); n=[x for x in (d.get('subnets') or []) if x.get('name')=='$name']; print(n[0].get('id','') if n else '')" 2>/dev/null || true)
  if [ -n "$found" ]; then echo "$found"; return; fi
  local body op op_id
  body=$(printf '{"projectId":"%s","networkId":"%s","name":"%s","zoneId":"%s","ipv4CidrPrimary":"%s"}' \
    "$proj" "$net" "$name" "$zone" "$cidr")
  op=$(api POST "/vpc/v1/subnets" "$token" "$body")
  op_id=$(echo "$op" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))' 2>/dev/null)
  if [ -z "$op_id" ]; then echo "[setup] WARN Subnet.Create failed: $(echo "$op"|head -c 160)" >&2; echo ""; return; fi
  poll_op "$op_id" "$token" \
    | python3 -c 'import sys,json; d=json.load(sys.stdin); print((d.get("metadata") or {}).get("subnetId",""))'
}

# ensure_sg <project> <network> <name> <token> → securityGroupId.
ensure_sg() {
  local proj="$1" net="$2" name="$3" token="$4"
  local found
  found=$(api GET "/vpc/v1/securityGroups?projectId=$proj&pageSize=1000" "$token" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); n=[x for x in (d.get('securityGroups') or []) if x.get('name')=='$name']; print(n[0].get('id','') if n else '')" 2>/dev/null || true)
  if [ -n "$found" ]; then echo "$found"; return; fi
  local body op op_id
  body=$(printf '{"projectId":"%s","networkId":"%s","name":"%s","description":"Phase B seed"}' "$proj" "$net" "$name")
  op=$(api POST "/vpc/v1/securityGroups" "$token" "$body")
  op_id=$(echo "$op" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))' 2>/dev/null)
  if [ -z "$op_id" ]; then echo "[setup] WARN SecurityGroup.Create failed: $(echo "$op"|head -c 160)" >&2; echo ""; return; fi
  poll_op "$op_id" "$token" \
    | python3 -c 'import sys,json; d=json.load(sys.stdin); print((d.get("metadata") or {}).get("securityGroupId",""))'
}

# fga_write <user> <relation> <object> — идемпотентный прямой FGA-tuple через
# kacho_iam.fga_outbox (drainer применит в OpenFGA за ~1s). Тот же механизм,
# что и seed system_viewer выше. Нужен для per-object list-filter grant'ов,
# которые НЕ выражаются публичным AccessBinding'ом (scope=PROJECT-гвард
# отвергает resource_type=vpc_subnet — см. domain/access_binding_scope.go:
# DeriveFromResourceType(vpc_subnet)→ScopeProject→ValidateAgainst fail).
fga_write() {
  local user="$1" relation="$2" object="$3"
  [ -z "$SV_PG_PW" ] && { log "    WARN fga_write skipped (no PG access): $user#$relation@$object"; return 0; }
  kubectl -n "$SV_NS" exec "$SV_PG_POD" -- env PGPASSWORD="$SV_PG_PW" psql -h localhost -U iam -d kacho_iam -tAc "
    INSERT INTO kacho_iam.fga_outbox (event_type, payload, created_at)
    SELECT 'fga.tuple.write',
           jsonb_build_object('user','$user','relation','$relation','object','$object'),
           now()
    WHERE NOT EXISTS (
      SELECT 1 FROM kacho_iam.fga_outbox
       WHERE payload->>'user'='$user' AND payload->>'relation'='$relation' AND payload->>'object'='$object'
    );
  " >/dev/null 2>&1 || log "    WARN fga_write failed (idempotent or schema): $user#$relation@$object"
}

# mint_user_jwt <sub> → HS256 dev JWT for that subject (exp = EXP_HOURS).
mint_user_jwt() {
  python3 "$SCRIPT_DIR/setup-jwt.py" --secret "$DEV_SECRET" --sub "$1" --exp-hours "$EXP_HOURS"
}

# ---------------------------------------------------------------------------
# Директива #2 — per-service ISOLATION. Root cause of cross-suite collision #276:
# vpc-CRUD + nlb + compute all shared account-A / projectA1 / projectA2 for their
# resource suites, so a binding grant/revoke or a listed resource from one suite
# leaked into another's expectations (whitelisted NOB/AAB, random interference) —
# and made a PARALLEL run (директива #1) unsafe. Fix: each resource suite gets its
# OWN account + home/cross projects, seeded here and patched into ONLY that
# service's env. The 6-subject authz-deny MATRIX keeps the shared account-A/proj
# (it IS the shared-tenant contract) — only the resource-CRUD scope is isolated.
# All idempotent (find-by-name), owner = AAA (can own many accounts).
log "10b/13 seeding per-service isolated accounts + projects (директива #2)"
ACCOUNT_VPC=$(ensure_account "authz-vpc" "директива #2 — vpc-only resource-CRUD home" "$USER_AAA" "$JWT_AAA")
ACCOUNT_NLB=$(ensure_account "authz-nlb" "директива #2 — nlb-only resource-CRUD home" "$USER_AAA" "$JWT_AAA")
ACCOUNT_CMP=$(ensure_account "authz-compute" "директива #2 — compute-only resource-CRUD home" "$USER_AAA" "$JWT_AAA")
# storage/registry accounts created HERE with the others (not inline before their
# projects) so the owner-binding has a materialization window before ensure_project
# needs `editor` on the account (fresh-account read-your-writes → else 403 FATAL).
ACCOUNT_STO=$(ensure_account "authz-storage" "директива #2 — storage-only resource-CRUD home" "$USER_AAA" "$JWT_AAA")
ACCOUNT_REG=$(ensure_account "authz-registry" "директива #2 — registry-only resource-CRUD home" "$USER_AAA" "$JWT_AAA")
# vpc: home + cross, default suite JWT = jwtProjectAdminA1 (PA1) → grant PA1 editor
# on BOTH so create-in-home + list-in-cross (isolation case) both authorize.
VPC_HOME=$(ensure_project "authz-vpc-home"  "$ACCOUNT_VPC" "vpc suite home"  "$JWT_AAA")
VPC_CROSS=$(ensure_project "authz-vpc-cross" "$ACCOUNT_VPC" "vpc suite cross" "$JWT_AAA")
[ -n "$USER_PA1" ] && [ -n "$VPC_HOME" ]  && ensure_binding "$USER_PA1" "$ROLE_EDIT" "project" "$VPC_HOME"  "$JWT_AAA"
[ -n "$USER_PA1" ] && [ -n "$VPC_CROSS" ] && ensure_binding "$USER_PA1" "$ROLE_EDIT" "project" "$VPC_CROSS" "$JWT_AAA"
log "    vpc isolation: acct=$ACCOUNT_VPC home=$VPC_HOME cross=$VPC_CROSS (PA1 editor on both)"

# storage + registry: same директива-#2 isolation. Their newman default-Bearer is
# jwtBootstrap (system_admin — storage/registry perms aren't all in the project edit
# role), granted project-editor here for LIST visibility (per-object list-filter needs
# the tuple; system_admin@cluster has v_get but not project v_list). Previously these
# two suites got NO per-service fixtures at all (env existingProjectId was a stale
# committed placeholder, ungranted → 401/403 on every mutation).
# Projects created + granted AS system_admin (JWT_BOOTSTRAP): iam.projects.create is
# cluster-wide for system_admin, so it does NOT depend on the fresh account's owner
# AccessBinding materialising `editor` on the account (which is the eventually-
# consistent hop that 403'd the account-owner path). bootstrap thereby OWNS the
# projects (the newman default-Bearer) and self-grants editor for LIST visibility.
STORAGE_HOME=$(ensure_project "authz-storage-home"  "$ACCOUNT_STO" "storage suite home"  "$JWT_BOOTSTRAP")
STORAGE_CROSS=$(ensure_project "authz-storage-cross" "$ACCOUNT_STO" "storage suite cross" "$JWT_BOOTSTRAP")
[ -n "$USER_BOOT" ] && [ -n "$STORAGE_HOME" ]  && ensure_binding "$USER_BOOT" "$ROLE_EDIT" "project" "$STORAGE_HOME"  "$JWT_BOOTSTRAP"
[ -n "$USER_BOOT" ] && [ -n "$STORAGE_CROSS" ] && ensure_binding "$USER_BOOT" "$ROLE_EDIT" "project" "$STORAGE_CROSS" "$JWT_BOOTSTRAP"
log "    storage isolation: acct=$ACCOUNT_STO home=$STORAGE_HOME cross=$STORAGE_CROSS (bootstrap owner+editor)"

REGISTRY_HOME=$(ensure_project "authz-registry-home"  "$ACCOUNT_REG" "registry suite home"  "$JWT_BOOTSTRAP")
REGISTRY_CROSS=$(ensure_project "authz-registry-cross" "$ACCOUNT_REG" "registry suite cross" "$JWT_BOOTSTRAP")
[ -n "$USER_BOOT" ] && [ -n "$REGISTRY_HOME" ]  && ensure_binding "$USER_BOOT" "$ROLE_EDIT" "project" "$REGISTRY_HOME"  "$JWT_BOOTSTRAP"
[ -n "$USER_BOOT" ] && [ -n "$REGISTRY_CROSS" ] && ensure_binding "$USER_BOOT" "$ROLE_EDIT" "project" "$REGISTRY_CROSS" "$JWT_BOOTSTRAP"
log "    registry isolation: acct=$ACCOUNT_REG home=$REGISTRY_HOME cross=$REGISTRY_CROSS (bootstrap owner+editor)"

# ---------------------------------------------------------------------------
# 11) COMPUTE — real project + cross-project + network + subnet + sg.
#     compute newman env references existingProjectId (→ _suiteFolderId),
#     existingProjectCrossId (→ _suiteFolderCrossId), existingNetworkId,
#     existingSubnetId (33× — instance NIC), existingSgId. existingZoneId
#     stays ru-central1-a (real geo zone). Created as AAA (account-A owner);
#     compute suite default-Bearer = jwtBootstrap (cluster-admin) may operate
#     in any project, so ownership only needs to be a real, resolvable project.
# ---------------------------------------------------------------------------
log "11/13 seeding compute fixtures (project + cross + network + subnet + sg)"
# Директива #2: compute home+cross live in the compute-only account (was ACCOUNT_A /
# shared PROJECT_A2). compute suite default = jwtBootstrap, which sees its OWN creates
# via per-object creator-tuples regardless of account → isolation is safe here.
COMPUTE_PROJ=$(ensure_project "authz-test-compute" "$ACCOUNT_CMP" "compute suite home (директива #2)" "$JWT_AAA")
COMPUTE_CROSS=$(ensure_project "authz-compute-cross" "$ACCOUNT_CMP" "compute suite cross (директива #2)" "$JWT_AAA")
# compute suite default JWT = jwtBootstrap. compute visibility is PROJECT-HIERARCHY (not
# creator-tuple): system_admin@cluster gets v_get cluster-wide (Get works) but NOT project
# v_list — so WITHOUT this grant bootstrap's LIST returns EMPTY (per-object list-filter has
# no ids) and every list-includes/list-filtered case is RED. Grant editor on BOTH compute
# projects so bootstrap can LIST its own creates (same pattern as vpc PA1 / nlb subjects).
[ -n "$USER_BOOT" ] && ensure_binding "$USER_BOOT" "$ROLE_EDIT" "project" "$COMPUTE_PROJ"  "$JWT_AAA"
[ -n "$USER_BOOT" ] && [ -n "$COMPUTE_CROSS" ] && ensure_binding "$USER_BOOT" "$ROLE_EDIT" "project" "$COMPUTE_CROSS" "$JWT_AAA"
COMPUTE_NET=$(ensure_network "$COMPUTE_PROJ" "authz-compute-net" "$JWT_AAA")
COMPUTE_SUBNET=""; COMPUTE_SG=""
if [ -n "$COMPUTE_NET" ]; then
  COMPUTE_SUBNET=$(ensure_subnet "$COMPUTE_PROJ" "$COMPUTE_NET" "authz-compute-subnet" "ru-central1-a" "10.192.0.0/24" "$JWT_AAA")
  COMPUTE_SG=$(ensure_sg "$COMPUTE_PROJ" "$COMPUTE_NET" "authz-compute-sg" "$JWT_AAA")
fi
log "    compute: proj=$COMPUTE_PROJ cross=$COMPUTE_CROSS net=$COMPUTE_NET subnet=$COMPUTE_SUBNET sg=$COMPUTE_SG"

# ---------------------------------------------------------------------------
# 12) VPC list-filter-d — per-object filtered List fixtures.
#     Subject S (subset-viewer): project#viewer (проходит method-gate List) +
#     per-object viewer/v_get/v_list на ОДНОМ subnet'е (visible). Subject N
#     (no-subnet-grant): только project#viewer → List возвращает 200 пустой
#     (explicit-model: project-tier НЕ каскадит visibility на subnet'ы).
#     Grant'ы — прямыми FGA-tuple'ами (public AccessBinding не умеет vpc_subnet
#     scope). subnetHidden НИКОМУ не грантится (no-leak). См. cases/list-filter-d.py.
# ---------------------------------------------------------------------------
log "12/13 seeding vpc list-filter-d fixtures (subset-viewer + no-grant + visible/hidden subnets)"
LF_PROJ=$(ensure_project "authz-test-listfilter" "$ACCOUNT_A" "Phase B vpc list-filter-d home" "$JWT_AAA")
LF_NET=$(ensure_network "$LF_PROJ" "authz-lf-net" "$JWT_AAA")
LF_SUB_VISIBLE=""; LF_SUB_HIDDEN=""
if [ -n "$LF_NET" ]; then
  LF_SUB_VISIBLE=$(ensure_subnet "$LF_PROJ" "$LF_NET" "authz-lf-subnet-visible" "ru-central1-a" "10.193.0.0/24" "$JWT_AAA")
  LF_SUB_HIDDEN=$(ensure_subnet "$LF_PROJ" "$LF_NET" "authz-lf-subnet-hidden" "ru-central1-a" "10.193.1.0/24" "$JWT_AAA")
fi
USER_LF_SV=$(upsert_user_grpc "authz-lf-subset-viewer@example.com" "authz-lf-subset-viewer@example.com" "AuthZ LF SubsetViewer")
USER_LF_NG=$(upsert_user_grpc "authz-lf-no-subnet-grant@example.com" "authz-lf-no-subnet-grant@example.com" "AuthZ LF NoSubnetGrant")
JWT_LF_SV=$(mint_user_jwt "authz-lf-subset-viewer@example.com")
JWT_LF_NG=$(mint_user_jwt "authz-lf-no-subnet-grant@example.com")
if [ -n "$USER_LF_SV" ] && [ -n "$LF_PROJ" ]; then
  # method-gate: api-gateway permission-catalog гейтит SubnetService.List
  # verb-relation'ом `v_list` НА project (explicit-RBAC model — не tier `viewer`;
  # deny: «lacks relation v_list on project ... action vpc.subnetses.list»).
  # ОБА субъекта получают project#v_list → List отдаёт 200; но visibility
  # per-object (ниже) не каскадит с project-уровня → no-grant видит пусто.
  fga_write "user:$USER_LF_SV" "v_list" "project:$LF_PROJ"
  fga_write "user:$USER_LF_NG" "v_list" "project:$LF_PROJ"
  # per-object visibility: List-filter = `viewer ∪ v_list` на vpc_subnet →
  # v_list делает visible-subnet виден в List; v_get даёт Get==enforce (200).
  # ТОЛЬКО на visible для S. Hidden — никому (no-leak: List-absent + Get→404).
  if [ -n "$LF_SUB_VISIBLE" ]; then
    fga_write "user:$USER_LF_SV" "v_list" "vpc_subnet:$LF_SUB_VISIBLE"
    fga_write "user:$USER_LF_SV" "v_get"  "vpc_subnet:$LF_SUB_VISIBLE"
  fi
fi
log "    list-filter-d: proj=$LF_PROJ visible=$LF_SUB_VISIBLE hidden=$LF_SUB_HIDDEN SV=$USER_LF_SV NG=$USER_LF_NG"

# ---------------------------------------------------------------------------
# 13) NLB — subject JWTs + IAM/FGA grants + real existing* resources.
#     STRICT subjects (must be granted correctly, else suite RED):
#       jwtProjectEditorA  editor @ project:A1 (suite default author)
#       jwtProjectEditorB  editor @ project:A2 (cross) ONLY — cross-tenant Move P0
#       jwtProjectViewerA  viewer @ project:A1
#       jwtProjectOwnerA   admin  @ project:A1
#       jwtStranger        valid JWT, NO bindings (hide-existence)
#     TOLERANT subjects (cases assert oneOf([200,403[,404]])): SA-editor
#     (properly seeded), group-member / 2 custom-roles (best-effort; a valid
#     but ungranted JWT already satisfies the tolerant deny). existingProjectId
#     = A1, existingProjectCrossId = A2 (both account A → grantor AAA).
# ---------------------------------------------------------------------------
log "13/13 seeding nlb fixtures (5 strict subjects + SA + group + 2 custom-roles + existing* resources)"
# Директива #2: nlb home+cross live in the nlb-only account (was the SHARED PROJECT_A1
# / PROJECT_A2 — the primary #276 collision: nlb grants 5+ subjects + SA + group +
# 2 custom-roles at PROJECT scope, which polluted the iam-matrix's account-A/proj
# expectations under interleaved/parallel runs). Strict subjects below are bound on
# these dedicated projects (grantor = AAA, owner of ACCOUNT_NLB → owner-cascade).
NLB_PROJ=$(ensure_project "authz-nlb-home"  "$ACCOUNT_NLB" "nlb suite home (директива #2)"  "$JWT_AAA")
NLB_CROSS=$(ensure_project "authz-nlb-cross" "$ACCOUNT_NLB" "nlb suite cross (директива #2)" "$JWT_AAA")

# 13a) strict user subjects + project-scoped tier bindings (grantor AAA).
USER_NLB_EA=$(upsert_user_grpc "authz-nlb-editor-a@example.com" "authz-nlb-editor-a@example.com" "AuthZ NLB EditorA")
USER_NLB_EB=$(upsert_user_grpc "authz-nlb-editor-b@example.com" "authz-nlb-editor-b@example.com" "AuthZ NLB EditorB")
USER_NLB_VA=$(upsert_user_grpc "authz-nlb-viewer-a@example.com" "authz-nlb-viewer-a@example.com" "AuthZ NLB ViewerA")
USER_NLB_OA=$(upsert_user_grpc "authz-nlb-owner-a@example.com"  "authz-nlb-owner-a@example.com"  "AuthZ NLB OwnerA")
USER_NLB_STR=$(upsert_user_grpc "authz-nlb-stranger@example.com" "authz-nlb-stranger@example.com" "AuthZ NLB Stranger")
JWT_NLB_EA=$(mint_user_jwt "authz-nlb-editor-a@example.com")
JWT_NLB_EB=$(mint_user_jwt "authz-nlb-editor-b@example.com")
JWT_NLB_VA=$(mint_user_jwt "authz-nlb-viewer-a@example.com")
JWT_NLB_OA=$(mint_user_jwt "authz-nlb-owner-a@example.com")
JWT_NLB_STR=$(mint_user_jwt "authz-nlb-stranger@example.com")
[ -n "$USER_NLB_EA" ]  && ensure_binding "$USER_NLB_EA" "$ROLE_EDIT"  "project" "$NLB_PROJ"  "$JWT_AAA"
[ -n "$USER_NLB_EB" ]  && ensure_binding "$USER_NLB_EB" "$ROLE_EDIT"  "project" "$NLB_CROSS" "$JWT_AAA"
[ -n "$USER_NLB_VA" ]  && ensure_binding "$USER_NLB_VA" "$ROLE_VIEW"  "project" "$NLB_PROJ"  "$JWT_AAA"
[ -n "$USER_NLB_OA" ]  && ensure_binding "$USER_NLB_OA" "$ROLE_ADMIN" "project" "$NLB_PROJ"  "$JWT_AAA"
# stranger: NO bindings by design.

# 13b) service-account editor subject (kacho_principal_type=service_account).
SVA_NLB=$(ensure_sa "authz-nlb-sa" "$ACCOUNT_A" "$JWT_AAA")
[ -n "$SVA_NLB" ] && ensure_sa_binding "$SVA_NLB" "$ROLE_EDIT" "project" "$NLB_PROJ" "$JWT_AAA"
JWT_NLB_SA=""
[ -n "$SVA_NLB" ] && JWT_NLB_SA=$(python3 "$SCRIPT_DIR/setup-jwt.py" --secret "$DEV_SECRET" --sa "$SVA_NLB" --exp-seconds "$((EXP_HOURS * 3600))")

# 13c) group-member editor (best-effort — case tolerant oneOf([200,403])).
USER_NLB_GM=$(upsert_user_grpc "authz-nlb-group-member@example.com" "authz-nlb-group-member@example.com" "AuthZ NLB GroupMember")
JWT_NLB_GM=$(mint_user_jwt "authz-nlb-group-member@example.com")
NLB_GROUP=""
if [ -n "$USER_NLB_GM" ]; then
  gresp=$(api POST "/iam/v1/groups" "$JWT_AAA" "$(printf '{"accountId":"%s","name":"authz-nlb-editors","description":"Phase B nlb group"}' "$ACCOUNT_A")" 2>/dev/null || true)
  gop=$(echo "$gresp" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))' 2>/dev/null || true)
  if [ -n "$gop" ]; then
    NLB_GROUP=$(poll_op "$gop" "$JWT_AAA" | python3 -c 'import sys,json; d=json.load(sys.stdin); print((d.get("metadata") or {}).get("groupId",""))' 2>/dev/null || true)
  else
    NLB_GROUP=$(api GET "/iam/v1/groups?accountId=$ACCOUNT_A" "$JWT_AAA" | python3 -c "import sys,json; d=json.load(sys.stdin); n=[x for x in (d.get('groups') or []) if x.get('name')=='authz-nlb-editors']; print(n[0].get('id','') if n else '')" 2>/dev/null || true)
  fi
  if [ -n "$NLB_GROUP" ]; then
    api POST "/iam/v1/groups/${NLB_GROUP}:addMember" "$JWT_AAA" "$(printf '{"groupId":"%s","memberType":"user","memberId":"%s"}' "$NLB_GROUP" "$USER_NLB_GM")" >/dev/null 2>&1 || true
    # bind the GROUP editor on project:A1 (subjectType=group).
    # redesign-2026 scope_type(dotted)/scope_id/target — see ensure_binding above.
    api POST "/iam/v1/accessBindings" "$JWT_AAA" "$(printf '{"subjectType":"group","subjectId":"%s","roleId":"%s","scopeType":"iam.project","scopeId":"%s","target":{"allInScope":{}}}' "$NLB_GROUP" "$ROLE_EDIT" "$NLB_PROJ")" >/dev/null 2>&1 || true
  fi
fi

# 13d) custom-role subjects (best-effort — cases tolerant).
ensure_custom_role() {
  local name="$1" module="$2" resources="$3" verbs="$4"
  local found body op op_id
  found=$(api GET "/iam/v1/roles?accountId=$ACCOUNT_A&pageSize=1000" "$JWT_AAA" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); n=[x for x in (d.get('roles') or []) if x.get('name')=='$name']; print(n[0].get('id','') if n else '')" 2>/dev/null || true)
  if [ -n "$found" ]; then echo "$found"; return; fi
  body=$(printf '{"accountId":"%s","name":"%s","description":"Phase B nlb custom role","rules":[{"module":"%s","resources":[%s],"verbs":[%s]}]}' \
    "$ACCOUNT_A" "$name" "$module" "$resources" "$verbs")
  op=$(api POST "/iam/v1/roles" "$JWT_AAA" "$body" 2>/dev/null || true)
  op_id=$(echo "$op" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("id",""))' 2>/dev/null || true)
  [ -z "$op_id" ] && { echo ""; return; }
  poll_op "$op_id" "$JWT_AAA" | python3 -c 'import sys,json; d=json.load(sys.stdin); print((d.get("metadata") or {}).get("roleId",""))' 2>/dev/null || true
}
USER_NLB_CRO=$(upsert_user_grpc "authz-nlb-cr-operator@example.com" "authz-nlb-cr-operator@example.com" "AuthZ NLB CustomRoleOperator")
USER_NLB_CRT=$(upsert_user_grpc "authz-nlb-cr-targetmgr@example.com" "authz-nlb-cr-targetmgr@example.com" "AuthZ NLB CustomRoleTargetMgr")
JWT_NLB_CRO=$(mint_user_jwt "authz-nlb-cr-operator@example.com")
JWT_NLB_CRT=$(mint_user_jwt "authz-nlb-cr-targetmgr@example.com")
ROLE_NLB_OP=$(ensure_custom_role "authz-nlb-operator" "loadbalancer" '"networkLoadBalancers"' '"start","stop"')
ROLE_NLB_TM=$(ensure_custom_role "authz-nlb-targetmgr" "loadbalancer" '"targetGroups"' '"addTargets","removeTargets"')
[ -n "$USER_NLB_CRO" ] && [ -n "$ROLE_NLB_OP" ] && ensure_binding "$USER_NLB_CRO" "$ROLE_NLB_OP" "project" "$NLB_PROJ" "$JWT_AAA"
[ -n "$USER_NLB_CRT" ] && [ -n "$ROLE_NLB_TM" ] && ensure_binding "$USER_NLB_CRT" "$ROLE_NLB_TM" "project" "$NLB_PROJ" "$JWT_AAA"

# 13e) real existing* resources for non-authz nlb cases (network/subnet/instance
#      /nic/address) via seed-nlb-fixtures.sh — passing an explicit project +
#      grantor JWT so it does not depend on its (polyrepo-era) fixture-path probe.
# keep seed output inside the gitignored out/ dir (not repo root — avoids a
# stray untracked .seeded-ids.env runtime artifact).
NLB_SEEDED_IDS="$OUT_DIR/nlb-seeded-ids.env"
if [ -f "$WORKSPACE_DIR/deploy/scripts/seed-nlb-fixtures.sh" ]; then
  log "    invoking seed-nlb-fixtures.sh (project=$NLB_PROJ)"
  BASE_URL="$BASE_URL" JWT="$JWT_AAA" existingProjectId="$NLB_PROJ" OUT_FILE="$NLB_SEEDED_IDS" \
    bash "$WORKSPACE_DIR/deploy/scripts/seed-nlb-fixtures.sh" >/dev/null 2>&1 || log "    WARN seed-nlb-fixtures.sh partial/failed (existing* ids may be blank)"
fi
NLB_NET=""; NLB_SUBNET=""; NLB_INSTANCE=""; NLB_NIC=""; NLB_ADDR=""
if [ -f "$NLB_SEEDED_IDS" ]; then
  # shellcheck disable=SC1090
  NLB_NET=$(grep -E '^existingNetworkId=' "$NLB_SEEDED_IDS" | cut -d= -f2- || true)
  NLB_SUBNET=$(grep -E '^existingSubnetId=' "$NLB_SEEDED_IDS" | cut -d= -f2- || true)
  NLB_INSTANCE=$(grep -E '^existingInstanceId=' "$NLB_SEEDED_IDS" | cut -d= -f2- || true)
  NLB_NIC=$(grep -E '^existingNicId=' "$NLB_SEEDED_IDS" | cut -d= -f2- || true)
  NLB_ADDR=$(grep -E '^existingExternalAddressId=' "$NLB_SEEDED_IDS" | cut -d= -f2- || true)
fi
log "    nlb subjects: EA=$USER_NLB_EA EB=$USER_NLB_EB VA=$USER_NLB_VA OA=$USER_NLB_OA STR=$USER_NLB_STR SA=$SVA_NLB"
log "    nlb resources: net=$NLB_NET subnet=$NLB_SUBNET instance=$NLB_INSTANCE nic=$NLB_NIC addr=$NLB_ADDR"

# Write authz-fixtures.json + patch env-files.
log "writing $OUT_DIR/authz-fixtures.json"
cat > "$OUT_DIR/authz-fixtures.json" <<EOF
{
  "baseUrl": "$BASE_URL",
  "jwtBootstrap": "$JWT_BOOTSTRAP",
  "jwtNoBindings": "$JWT_NO_BINDINGS",
  "jwtPureNoBindings": "$JWT_PURE_NOB",
  "jwtProjectAdminA1": "$JWT_PA1",
  "jwtAccountAdminA": "$JWT_AAA",
  "jwtAccountAdminAStepUp": "$JWT_AAA_STEPUP",
  "jwtAccountAdminB": "$JWT_AAB",
  "jwtInvitee": "$JWT_INV",
  "accountAId": "$ACCOUNT_A",
  "accountBId": "$ACCOUNT_B",
  "projectA1Id": "$PROJECT_A1",
  "projectA2Id": "$PROJECT_A2",
  "projectB1Id": "$PROJECT_B1",
  "seedNetworkA1Id": "$SEED_NET_A1",
  "seedNetworkB1Id": "$SEED_NET_B1",
  "userNOBId": "$USER_NOB",
  "userPureNoBindingsId": "$USER_PURE_NOB",
  "userPA1Id": "$USER_PA1",
  "userAAAId": "$USER_AAA",
  "userAABId": "$USER_AAB",
  "userINVId": "$USER_INV",
  "svaAId": "$SVA_A",
  "svaNoGrantId": "$SVA_NOGRANT",
  "jwtSAA": "$JWT_SAA",
  "jwtSANoGrant": "$JWT_SANG",
  "apiTokenValid": "$API_TOKEN_VALID",
  "apiTokenRevoked": "$API_TOKEN_REVOKED",
  "apiTokenExpired": "$API_TOKEN_EXPIRED",
  "apiTokenMalformed": "$API_TOKEN_MALFORMED"
}
EOF

if [ "$PATCH_ENV" = "true" ]; then
  # Монорепа: env-файлы ищем ГЛОБОМ по фактической раскладке (services/<svc>, gateway),
  # а не захардкоженным списком. Раньше было три пути вида
  # "$WORKSPACE_DIR/project/kacho-<svc>/tests/newman/..." — polyrepo-раскладка, которой
  # в монорепе нет (WORKSPACE_DIR тут = корень репо, и путь бил в
  # kacho/project/kacho-compute/... → patch-env печатал SKIP (missing) и env оставался
  # непропатченным). Плюс список отставал от жизни: «all 3 services» при семи сервисах —
  # newman-наборы iam/vpc/compute/nlb/storage/registry просто не получали фикстуры.
  # Глоб держит это в синхроне сам: новый сервис с newman-набором подхватится без правок.
  ENV_FILES=()
  for e in "$WORKSPACE_DIR"/services/*/tests/newman/environments/local.postman_environment.json \
           "$WORKSPACE_DIR"/gateway/tests/newman/environments/local.postman_environment.json; do
    [ -f "$e" ] && ENV_FILES+=("$e")
  done
  if [ ${#ENV_FILES[@]} -eq 0 ]; then
    log "    WARN: newman env-файлов не найдено — пропускаю patch-env"
  else
    log "    patching newman env files (${#ENV_FILES[@]} шт.)"
    python3 "$SCRIPT_DIR/patch-env.py" "$OUT_DIR/authz-fixtures.json" "${ENV_FILES[@]}"
  fi

  # --- Phase B targeted per-service patches ---------------------------------
  # `existingProjectId`/`existingSubnetId`/subject-JWT семантически РАЗНЫЕ у
  # каждого сервиса, поэтому патчим ТОЧЕЧНО (compute-fixtures → compute env и
  # т.д.), а не общим глобом — иначе один existingProjectId затёр бы другой.
  # patch_one <fixtures.json> <env-path> — патчит, предварительно ВЫКИНУВ
  # ключи с пустым значением: неудавшийся под-seed (напр. instance create
  # rejected) НЕ должен затирать committed-плейсхолдер пустой строкой.
  patch_one() {
    [ -f "$2" ] || return 0
    local filtered="${1%.json}.nonempty.json"
    python3 -c "
import json
d=json.load(open('$1'))
json.dump({k:v for k,v in d.items() if v}, open('$filtered','w'), indent=2)
"
    python3 "$SCRIPT_DIR/patch-env.py" "$filtered" "$2" || true
  }
  COMPUTE_ENV="$WORKSPACE_DIR/services/compute/tests/newman/environments/local.postman_environment.json"
  VPC_ENV="$WORKSPACE_DIR/services/vpc/tests/newman/environments/local.postman_environment.json"
  NLB_ENV="$WORKSPACE_DIR/services/nlb/tests/newman/environments/local.postman_environment.json"

  # compute — только непустые ключи (пустой не должен затирать committed-default).
  cat > "$OUT_DIR/compute-fixtures.json" <<EOF
{
  "existingProjectId": "$COMPUTE_PROJ",
  "existingProjectCrossId": "$COMPUTE_CROSS",
  "existingNetworkId": "$COMPUTE_NET",
  "existingSubnetId": "$COMPUTE_SUBNET",
  "existingSgId": "$COMPUTE_SG"
}
EOF
  patch_one "$OUT_DIR/compute-fixtures.json" "$COMPUTE_ENV"

  # vpc list-filter-d subjects + subnets (additive to the shared vpc patch) +
  # директива #2 dedicated home/cross projects. existingProjectId/CrossId drive the
  # CRUD-suite _suiteProjectId/_suiteProjectCrossId (gen.py prelude prefers them);
  # the authz-deny matrix keeps the shared projectA1Id/B1Id (patched by the shared JSON).
  cat > "$OUT_DIR/vpc-listfilter-fixtures.json" <<EOF
{
  "jwtSubnetSubsetViewer": "$JWT_LF_SV",
  "jwtNoSubnetGrant": "$JWT_LF_NG",
  "listFilterProjectId": "$LF_PROJ",
  "subnetVisibleId": "$LF_SUB_VISIBLE",
  "subnetHiddenId": "$LF_SUB_HIDDEN",
  "existingProjectId": "$VPC_HOME",
  "existingProjectCrossId": "$VPC_CROSS"
}
EOF
  patch_one "$OUT_DIR/vpc-listfilter-fixtures.json" "$VPC_ENV"

  # storage — suite home/cross project (PA1-granted). existingZoneId (ru-central1-a)
  # + existingDiskTypeId (block-balanced) stay committed-defaults (real geo zone +
  # seeded storage disk-type). Only непустые ключи patched.
  STORAGE_ENV="$WORKSPACE_DIR/services/storage/tests/newman/environments/local.postman_environment.json"
  cat > "$OUT_DIR/storage-fixtures.json" <<EOF
{
  "existingProjectId": "$STORAGE_HOME",
  "existingProjectCrossId": "$STORAGE_CROSS"
}
EOF
  patch_one "$OUT_DIR/storage-fixtures.json" "$STORAGE_ENV"

  # registry — suite home/cross project (PA1-granted).
  REGISTRY_ENV="$WORKSPACE_DIR/services/registry/tests/newman/environments/local.postman_environment.json"
  cat > "$OUT_DIR/registry-fixtures.json" <<EOF
{
  "existingProjectId": "$REGISTRY_HOME",
  "existingProjectCrossId": "$REGISTRY_CROSS"
}
EOF
  patch_one "$OUT_DIR/registry-fixtures.json" "$REGISTRY_ENV"

  # nlb subjects + existing* resources.
  cat > "$OUT_DIR/nlb-fixtures.json" <<EOF
{
  "existingProjectId": "$NLB_PROJ",
  "existingProjectCrossId": "$NLB_CROSS",
  "existingRegionId": "ru-central1",
  "existingZoneId": "ru-central1-a",
  "existingNetworkId": "$NLB_NET",
  "existingSubnetId": "$NLB_SUBNET",
  "existingInstanceId": "$NLB_INSTANCE",
  "existingNicId": "$NLB_NIC",
  "existingAddressId": "$NLB_ADDR",
  "jwtProjectEditorA": "$JWT_NLB_EA",
  "jwtProjectEditorB": "$JWT_NLB_EB",
  "jwtProjectViewerA": "$JWT_NLB_VA",
  "jwtProjectOwnerA": "$JWT_NLB_OA",
  "jwtStranger": "$JWT_NLB_STR",
  "jwtServiceAccountEditor": "$JWT_NLB_SA",
  "jwtGroupMemberEditor": "$JWT_NLB_GM",
  "jwtCustomRoleOperator": "$JWT_NLB_CRO",
  "jwtCustomRoleTargetManager": "$JWT_NLB_CRT"
}
EOF
  patch_one "$OUT_DIR/nlb-fixtures.json" "$NLB_ENV"
fi

log "DONE — fixtures saved to $OUT_DIR/authz-fixtures.json"
