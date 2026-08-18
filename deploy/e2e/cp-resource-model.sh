#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# kacho-deploy/e2e/cp-resource-model.sh — integration / e2e test for the public
# NetworkInterface resource + a negative "infra-info leak" audit of the public
# REST surface. Runs against a deployed stack via the api-gateway REST endpoint
# ($BASE_URL).
#
# Scenarios:
#   S1 — Network public projection is lean: it must not carry any infra-sensitive
#        keys.
#   S2 — NetworkInterface public view is lean (id/project/name/.../status, used_by);
#        none of the infra-sensitive keys appear publicly.
#   S3 — freshly-created NIC has empty used_by (public projection). NIC
#        attach/detach RPCs were removed in KAC-266, so no attach lifecycle here.
#   S4 — negative infra-leak audit over a REQUIRED set of public list/get endpoints,
#        asserted free of forbidden infra keys (recursive JSON key walk).
#
# ---------------------------------------------------------------------------
# S4 audits BLOCK STORAGE ON ITS OWNER (kacho-storage), and a missing target is a
# FAILURE — read this before "simplifying" either half back.
#
# The audit used to crawl /compute/v1/disks and /compute/v1/images: the block-storage
# DUPLICATE that kacho-compute still carries while its ownership sits with
# kacho-storage. It also treated a missing route as a reason to SKIP. Those two
# choices combined into a check that would disappear on its own: the moment the
# duplicate is deleted, its routes stop answering, every audited path turns into a
# skip, and the script goes green while auditing nothing. That is not a hypothetical
# — deleting the duplicate is the very next step, and this file was the only thing
# still asserting that block-storage projections carry no infra keys.
#
# So: the audited paths are the OWNER's (/storage/v1/...), and REQUIRED_ENDPOINTS is
# required literally. A target that answers anything other than 200 — route absent,
# denied, backend down — is reported as FAIL, never as SKIP or WARN. If a path is
# retired on purpose, this list is edited on purpose; silence is not a way to retire
# a check.
#
# For the same reason S4 SEEDS a volume in the audited project and asserts it is
# present in the audited payload. Walking an empty list finds no forbidden keys and
# proves nothing; the audit has to be looking at an actual resource projection.
# ---------------------------------------------------------------------------
#
# Prereqs: stack up and seeded (at least one project exists, geo zones/regions and
# storage disk types are seeded), plus a bearer token for an identity allowed to read
# the audited projections.
#
# Usage:
#   BASE_URL=http://localhost:18080 TOKEN="$(…mint…)" ./e2e/cp-resource-model.sh
#   PROJECT_ID=prj… — optional; otherwise the first project readable by TOKEN is used.
set -uo pipefail

BASE_URL="${BASE_URL:-http://localhost:18080}"
TOKEN="${TOKEN:-}"
PROJECT_ID="${PROJECT_ID:-}"
PASS=0 FAIL=0
ok()   { echo "  PASS: $1"; PASS=$((PASS+1)); }
bad()  { echo "  FAIL: $1"; FAIL=$((FAIL+1)); }
warn() { echo "  WARN: $1"; }
skip() { echo "  SKIP: $1"; }

# Every request carries the caller's identity. Without it the whole stack answers 401
# and each assertion below degrades into "could not check" — which is exactly the
# failure mode this file is being hardened against, so an absent token is fatal up
# front rather than a runtime shrug.
AUTH_ARGS=()
if [[ -n "$TOKEN" ]]; then AUTH_ARGS=(-H "Authorization: Bearer $TOKEN"); fi
code() { curl -s -o /dev/null -w '%{http_code}' "${AUTH_ARGS[@]}" "$@"; }
body() { curl -s "${AUTH_ARGS[@]}" "$@"; }

# Forbidden infra-sensitive JSON keys (case-insensitive) — must never appear on the
# public REST surface (see workspace CLAUDE.md §"Инфра-чувствительные данные").
FORBIDDEN_KEYS='sid sidLocator sid_locator'

# leak_keys <json-on-stdin> — prints any forbidden keys found anywhere in the JSON
# (recursive key walk; robust against substring false-positives like "considered").
leak_keys() {
  FORBIDDEN_KEYS="$FORBIDDEN_KEYS" python3 -c '
import sys, json, os
forbidden = set(k.lower() for k in os.environ["FORBIDDEN_KEYS"].split())
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
found = set()
def walk(x):
    if isinstance(x, dict):
        for k, v in x.items():
            if k.lower() in forbidden:
                found.add(k)
            walk(v)
    elif isinstance(x, list):
        for v in x:
            walk(v)
walk(d)
print(" ".join(sorted(found)))
'
}

jget() { python3 -c "import sys,json
try:
  d=json.load(sys.stdin)
  for k in '$1'.split('.'):
    d=(d or {}).get(k)
  print(d if d is not None else '')
except Exception: print('')"; }

# wait_op OP_ID -> prints the (done) operation JSON, or '' on timeout
wait_op() {
  local op_id="$1" op done
  for _ in $(seq 1 40); do
    op=$(body "$BASE_URL/operations/$op_id")
    done=$(printf '%s' "$op" | jget done)
    if [[ "$done" == "True" || "$done" == "true" || "$done" == "1" ]]; then
      printf '%s' "$op"; return 0
    fi
    sleep 1
  done
  echo ""
}

echo "== NetworkInterface resource-model e2e against $BASE_URL =="

[[ -n "$TOKEN" ]] || {
  echo "FATAL: TOKEN is empty — every request would be 401 and every check below would"
  echo "       report 'could not verify' instead of a verdict. Pass a bearer token."
  exit 1
}

# --- discover a project the caller can read (IAM is the owner of the hierarchy) ---
if [[ -z "$PROJECT_ID" ]]; then
  PROJECT_ID=$(body "$BASE_URL/iam/v1/projects" | python3 -c 'import sys,json;
try: print((json.load(sys.stdin).get("projects") or [{}])[0].get("id",""))
except Exception: print("")')
fi
echo "[setup] project=$PROJECT_ID"
[[ -n "$PROJECT_ID" ]] || { echo "FATAL: no project readable by TOKEN (seed the stand)"; exit 1; }

# Placement comes from the geo catalog, never from a literal: a hard-coded zone name
# is a guess about someone else's seed and turns into an async "unknown zone" the
# moment that seed changes.
ZONE_ID=$(body "$BASE_URL/geo/v1/zones" | python3 -c 'import sys,json;
try: print((json.load(sys.stdin).get("zones") or [{}])[0].get("id",""))
except Exception: print("")')
echo "[setup] zone=$ZONE_ID"
[[ -n "$ZONE_ID" ]] || { echo "FATAL: no geo zone (seed the stand)"; exit 1; }

# The subnet CIDR is randomised per run. This script shares a stand with the newman
# suites, and a fixed block collides with whatever else is holding it — the collision
# then looks like a flaky product failure rather than a fixture clash.
CIDR_OCT_A=$(( (RANDOM % 200) + 20 ))
CIDR_OCT_B=$(( RANDOM % 256 ))

CREATED_NETS=() CREATED_NICS=() CREATED_ADDRS=() CREATED_SUBNETS=()
SEED_VOL_ID=""
cleanup() {
  # The seeded block-storage resource goes first: it is the one this run adds to a
  # shared stand, and leaving it behind would slowly inflate every other suite's
  # list expectations.
  if [[ -n "${SEED_VOL_ID:-}" ]]; then
    op=$(body -X DELETE "$BASE_URL/storage/v1/volumes/$SEED_VOL_ID" || true)
    op_id=$(printf '%s' "$op" | jget id); [[ -n "$op_id" ]] && wait_op "$op_id" >/dev/null
  fi
  for n in "${CREATED_NICS[@]:-}"; do
    [[ -n "$n" ]] || continue
    op=$(body -X DELETE "$BASE_URL/vpc/v1/networkInterfaces/$n" || true)
    op_id=$(printf '%s' "$op" | jget id); [[ -n "$op_id" ]] && wait_op "$op_id" >/dev/null
  done
  for a in "${CREATED_ADDRS[@]:-}"; do
    [[ -n "$a" ]] || continue
    op=$(body -X DELETE "$BASE_URL/vpc/v1/addresses/$a" || true)
    op_id=$(printf '%s' "$op" | jget id); [[ -n "$op_id" ]] && wait_op "$op_id" >/dev/null
  done
  # Subnets are deleted BEFORE their network: a network with a live subnet refuses to
  # go, which is how repeated runs used to leave a trail of orphaned networks behind
  # on a shared stand.
  for s in "${CREATED_SUBNETS[@]:-}"; do
    [[ -n "$s" ]] || continue
    op=$(body -X DELETE "$BASE_URL/vpc/v1/subnets/$s" || true)
    op_id=$(printf '%s' "$op" | jget id); [[ -n "$op_id" ]] && wait_op "$op_id" >/dev/null
  done
  for net in "${CREATED_NETS[@]:-}"; do [[ -n "$net" ]] && code -X DELETE "$BASE_URL/vpc/v1/networks/$net" >/dev/null || true; done
}
trap cleanup EXIT

# ===========================================================================
echo
echo "[S1] Network public projection is lean (no infra-sensitive keys)"
NET_OP=$(body -X POST "$BASE_URL/vpc/v1/networks" -H 'Content-Type: application/json' \
            -d "{\"projectId\":\"$PROJECT_ID\",\"name\":\"cprm-s1-net-$RANDOM\",\"description\":\"S1\"}")
NET_OP_ID=$(printf '%s' "$NET_OP" | jget id)
NET_ID=""
if [[ -n "$NET_OP_ID" ]]; then
  OP=$(wait_op "$NET_OP_ID")
  NET_ID=$(printf '%s' "$OP" | jget metadata.networkId)
fi
if [[ -n "$NET_ID" ]]; then
  CREATED_NETS+=("$NET_ID")
  ok "Network created ($NET_ID)"
  NET_BODY=$(body "$BASE_URL/vpc/v1/networks/$NET_ID")
  LEAKED=$(printf '%s' "$NET_BODY" | leak_keys)
  if [[ -z "$LEAKED" ]]; then
    ok "GET /vpc/v1/networks/{id} is lean (no infra keys)"
  else
    bad "GET /vpc/v1/networks/{id} LEAKS infra keys: [$LEAKED] body=$NET_BODY"
  fi
else
  bad "could not create a Network for S1 (op=$NET_OP)"
fi

# ===========================================================================
echo
echo "[S2] NetworkInterface — lean public view (no infra-sensitive keys)"
# need a subnet (zone + CIDR both come from setup, not from literals)
SUBNET_ID=""
if [[ -n "$NET_ID" ]]; then
  SUB_OP=$(body -X POST "$BASE_URL/vpc/v1/subnets" -H 'Content-Type: application/json' \
              -d "{\"projectId\":\"$PROJECT_ID\",\"name\":\"cprm-s2-sub-$RANDOM\",\"networkId\":\"$NET_ID\",\"zoneId\":\"$ZONE_ID\",\"v4CidrBlocks\":[\"10.$CIDR_OCT_A.$CIDR_OCT_B.0/24\"]}")
  SUB_OP_ID=$(printf '%s' "$SUB_OP" | jget id)
  [[ -n "$SUB_OP_ID" ]] && SUBNET_ID=$(wait_op "$SUB_OP_ID" | jget metadata.subnetId)
  [[ -n "$SUBNET_ID" ]] && CREATED_SUBNETS+=("$SUBNET_ID")
fi
if [[ -z "$SUBNET_ID" ]]; then
  skip "S2: could not create a subnet — skipping NIC scenario"
else
  ok "subnet created ($SUBNET_ID)"
  # try NIC create with empty address arrays first; if it requires an address, make one
  NIC_NAME="cprm-s2-nic-$RANDOM"
  NIC_OP=$(body -X POST "$BASE_URL/vpc/v1/networkInterfaces" -H 'Content-Type: application/json' \
              -d "{\"projectId\":\"$PROJECT_ID\",\"name\":\"$NIC_NAME\",\"subnetId\":\"$SUBNET_ID\"}")
  NIC_OP_ID=$(printf '%s' "$NIC_OP" | jget id)
  NIC_ID=""
  if [[ -n "$NIC_OP_ID" ]]; then
    OP=$(wait_op "$NIC_OP_ID")
    NIC_ID=$(printf '%s' "$OP" | jget metadata.networkInterfaceId)
    OPERR=$(printf '%s' "$OP" | jget error.message)
    [[ -n "$OPERR" ]] && warn "NIC-create(empty addrs) op error: $OPERR"
  fi
  if [[ -z "$NIC_ID" ]]; then
    # retry: allocate an internal_ipv4 Address in the subnet first
    ADDR_OP=$(body -X POST "$BASE_URL/vpc/v1/addresses" -H 'Content-Type: application/json' \
                 -d "{\"projectId\":\"$PROJECT_ID\",\"name\":\"cprm-s2-addr-$RANDOM\",\"internalIpv4AddressSpec\":{\"subnetId\":\"$SUBNET_ID\"}}")
    ADDR_OP_ID=$(printf '%s' "$ADDR_OP" | jget id)
    ADDR_ID=""
    [[ -n "$ADDR_OP_ID" ]] && ADDR_ID=$(wait_op "$ADDR_OP_ID" | jget metadata.addressId)
    if [[ -n "$ADDR_ID" ]]; then
      CREATED_ADDRS+=("$ADDR_ID")
      NIC_OP=$(body -X POST "$BASE_URL/vpc/v1/networkInterfaces" -H 'Content-Type: application/json' \
                  -d "{\"projectId\":\"$PROJECT_ID\",\"name\":\"$NIC_NAME\",\"subnetId\":\"$SUBNET_ID\",\"v4AddressIds\":[\"$ADDR_ID\"]}")
      NIC_OP_ID=$(printf '%s' "$NIC_OP" | jget id)
      [[ -n "$NIC_OP_ID" ]] && NIC_ID=$(wait_op "$NIC_OP_ID" | jget metadata.networkInterfaceId)
    fi
  fi
  if [[ -z "$NIC_ID" ]]; then
    bad "could not create a NetworkInterface (op=$NIC_OP)"
  else
    CREATED_NICS+=("$NIC_ID")
    ok "NetworkInterface created ($NIC_ID)"
    NIC_BODY=$(body "$BASE_URL/vpc/v1/networkInterfaces/$NIC_ID")
    # public view: must be lean — none of the infra keys
    LEAKED=$(printf '%s' "$NIC_BODY" | leak_keys)
    if [[ -z "$LEAKED" ]]; then
      ok "public NIC view is lean (no infra keys)"
    else
      bad "public NIC view LEAKS infra keys: [$LEAKED] body=$NIC_BODY"
    fi
    # spot-check: must still carry the lean fields it is supposed to have
    for k in id projectId subnetId status; do
      [[ -n "$(printf '%s' "$NIC_BODY" | python3 -c "import sys,json
try:
  d=json.load(sys.stdin); print('1' if '$k' in d else '')
except Exception: print('')")" ]] && ok "public NIC view has '$k'" || bad "public NIC view missing '$k'"
    done

    # -----------------------------------------------------------------------
    echo
    echo "[S3] freshly-created NIC has empty used_by (public projection)"
    # NIC attach/detach RPCs were removed in KAC-266 (NetworkInterface no longer
    # exposes :attach/:detach; instances are created without auto-NICs). We only
    # assert the public used_by projection on a freshly-created, unattached NIC.
    UB=$(printf '%s' "$NIC_BODY" | python3 -c 'import sys,json;
try:
  d=json.load(sys.stdin); print(json.dumps(d.get("usedBy") or {}))
except Exception: print("{}")')
    [[ "$UB" == "{}" || "$UB" == "null" ]] && ok "freshly-created NIC has empty used_by" || warn "fresh NIC used_by not empty: $UB"
  fi
fi

# ===========================================================================
echo
echo "[S4] negative infra-leak audit of the public VPC / Storage / Compute REST surface"

# --- seed a real block-storage resource so the audit has something to look at ----
# An audit that walks an empty collection finds no forbidden keys and concludes
# nothing. The seeded volume makes the storage projections non-empty, and its id is
# asserted to be present in the audited payload below. (SEED_VOL_ID is declared next
# to the cleanup trap so the trap can always see it, even on an early exit.)
DISK_TYPE_ID=$(body "$BASE_URL/storage/v1/diskTypes" | python3 -c 'import sys,json;
try: print((json.load(sys.stdin).get("diskTypes") or [{}])[0].get("id",""))
except Exception: print("")')
if [[ -z "$DISK_TYPE_ID" ]]; then
  bad "S4 seed: could not discover a storage diskType — the storage audit would run against an empty projection"
else
  VOL_OP=$(body -X POST "$BASE_URL/storage/v1/volumes" -H 'Content-Type: application/json' \
              -d "{\"projectId\":\"$PROJECT_ID\",\"name\":\"cprm-s4-vol-$RANDOM\",\"zoneId\":\"$ZONE_ID\",\"diskTypeId\":\"$DISK_TYPE_ID\",\"sizeBytes\":10737418240}")
  VOL_OP_ID=$(printf '%s' "$VOL_OP" | jget id)
  if [[ -n "$VOL_OP_ID" ]]; then
    OP=$(wait_op "$VOL_OP_ID")
    # The operation carries a pre-allocated volume id even when it finished WITH an
    # error, so the error is checked BEFORE the id is used — otherwise the audit
    # would chase a phantom and report a missing resource as a leak-audit problem.
    OP_ERR=$(printf '%s' "$OP" | jget error.message)
    if [[ -n "$OP_ERR" ]]; then
      bad "S4 seed: volume create failed asynchronously: $OP_ERR"
    else
      SEED_VOL_ID=$(printf '%s' "$OP" | jget metadata.volumeId)
    fi
  fi
  [[ -n "$SEED_VOL_ID" ]] && ok "S4 seed volume created ($SEED_VOL_ID)" \
                          || bad "S4 seed: no volume created (op=$VOL_OP) — storage audit would be vacuous"
fi

# --- required audit targets -----------------------------------------------------
# REQUIRED means required. Block storage is audited on its OWNER (kacho-storage);
# compute keeps only what it actually owns (Instance). Anything other than 200 here —
# route absent, denied, backend down — is a FAILURE, because "the path stopped
# answering" is precisely how this audit would otherwise delete itself when the
# compute block-storage duplicate is removed. See the header for the full rationale.
REQUIRED_ENDPOINTS=(
  "/vpc/v1/networks?projectId=$PROJECT_ID"
  "/vpc/v1/subnets?projectId=$PROJECT_ID"
  "/vpc/v1/networkInterfaces?projectId=$PROJECT_ID"
  "/vpc/v1/addresses?projectId=$PROJECT_ID"
  "/vpc/v1/securityGroups?projectId=$PROJECT_ID"
  "/vpc/v1/routeTables?projectId=$PROJECT_ID"
  "/vpc/v1/gateways?projectId=$PROJECT_ID"
  "/storage/v1/volumes?projectId=$PROJECT_ID"
  "/storage/v1/snapshots?projectId=$PROJECT_ID"
  "/storage/v1/images?projectId=$PROJECT_ID"
  "/storage/v1/diskTypes"
  "/compute/v1/instances?projectId=$PROJECT_ID"
)
for ep in "${REQUIRED_ENDPOINTS[@]}"; do
  c=$(code "$BASE_URL$ep")
  if [[ "$c" != 200 ]]; then
    bad "$ep -> HTTP $c — REQUIRED audit target did not answer 200 (route absent / denied / backend down). This is a failure, not a skip: an unanswered path audits nothing."
    continue
  fi
  b=$(body "$BASE_URL$ep")
  leaked=$(printf '%s' "$b" | leak_keys)
  if [[ -z "$leaked" ]]; then
    ok "$ep — no infra keys"
  else
    bad "$ep — LEAKS infra keys: [$leaked]"
  fi
done

# --- non-vacuity: the storage audit must have seen the seeded volume ------------
# Without this, "no forbidden keys found" stays true for an empty list forever, and
# a projection change on a resource nobody listed would sail straight through.
if [[ -n "$SEED_VOL_ID" ]]; then
  b=$(body "$BASE_URL/storage/v1/volumes?projectId=$PROJECT_ID")
  if [[ "$b" == *"$SEED_VOL_ID"* ]]; then
    ok "storage volume audit is non-vacuous (seeded volume present in the audited payload)"
  else
    bad "storage volume audit is VACUOUS: seeded volume $SEED_VOL_ID absent from /storage/v1/volumes — the leak walk had no resource projection to inspect"
  fi
  b=$(body "$BASE_URL/storage/v1/volumes/$SEED_VOL_ID"); leaked=$(printf '%s' "$b" | leak_keys)
  [[ -z "$leaked" ]] && ok "GET storage volume/{id} — no infra keys" \
                     || bad "GET storage volume/{id} LEAKS: [$leaked]"
fi
# also re-check the specific GET-by-id of resources we created (list responses may
# project differently than single-get on some servers)
if [[ -n "${NET_ID:-}" ]]; then
  b=$(body "$BASE_URL/vpc/v1/networks/$NET_ID"); leaked=$(printf '%s' "$b" | leak_keys)
  [[ -z "$leaked" ]] && ok "GET network/{id} — no infra keys" || bad "GET network/{id} LEAKS: [$leaked]"
fi
if [[ -n "${NIC_ID:-}" ]]; then
  b=$(body "$BASE_URL/vpc/v1/networkInterfaces/$NIC_ID"); leaked=$(printf '%s' "$b" | leak_keys)
  [[ -z "$leaked" ]] && ok "GET networkInterface/{id} — no infra keys" || bad "GET networkInterface/{id} LEAKS: [$leaked]"
fi

echo
echo "== result: PASS=$PASS FAIL=$FAIL =="
[[ "$FAIL" == 0 ]] || exit 1
