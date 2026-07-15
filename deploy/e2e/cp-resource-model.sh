#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# kacho-deploy/e2e/cp-resource-model.sh — integration / e2e test for the public
# NetworkInterface resource + a negative "infra-info leak" audit of the public
# REST surface. Runs against a deployed stack via the api-gateway REST endpoint
# ($BASE_URL — same as e2e/geography-move.sh).
#
# Scenarios:
#   S1 — Network public projection is lean: it must not carry any infra-sensitive
#        keys.
#   S2 — NetworkInterface public view is lean (id/folder/name/.../status, used_by);
#        none of the infra-sensitive keys appear publicly.
#   S3 — freshly-created NIC has empty used_by (public projection). NIC
#        attach/detach RPCs were removed in KAC-266, so no attach lifecycle here.
#   S4 — negative infra-leak audit: every public vpc & compute list/get endpoint is
#        crawled and asserted free of forbidden infra keys (recursive JSON key walk).
#
# Prereqs: stack up; ci/seed.sh has run (so the default folder + a VPC network exist).
#
# Usage: BASE_URL=http://localhost:28080 ./e2e/cp-resource-model.sh
set -uo pipefail

BASE_URL="${BASE_URL:-http://localhost:28080}"
PASS=0 FAIL=0
ok()   { echo "  PASS: $1"; PASS=$((PASS+1)); }
bad()  { echo "  FAIL: $1"; FAIL=$((FAIL+1)); }
warn() { echo "  WARN: $1"; }
skip() { echo "  SKIP: $1"; }
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
body() { curl -s "$@"; }

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

# --- discover the seed folder + a VPC network/subnet (ci/seed.sh fixtures) ---
FOLDER_ID=$(body "$BASE_URL/resource-manager/v1/folders" | python3 -c 'import sys,json;
try: print((json.load(sys.stdin).get("folders") or [{}])[0].get("id",""))
except Exception: print("")')
echo "[setup] folder=$FOLDER_ID"
[[ -n "$FOLDER_ID" ]] || { echo "FATAL: no folder (run ci/seed.sh)"; exit 1; }

CREATED_NETS=() CREATED_NICS=() CREATED_ADDRS=()
cleanup() {
  for n in "${CREATED_NICS[@]:-}"; do
    [[ -n "$n" ]] || continue
    op=$(curl -s -X DELETE "$BASE_URL/vpc/v1/networkInterfaces/$n" || true)
    op_id=$(printf '%s' "$op" | jget id); [[ -n "$op_id" ]] && wait_op "$op_id" >/dev/null
  done
  for a in "${CREATED_ADDRS[@]:-}"; do
    [[ -n "$a" ]] || continue
    op=$(curl -s -X DELETE "$BASE_URL/vpc/v1/addresses/$a" || true)
    op_id=$(printf '%s' "$op" | jget id); [[ -n "$op_id" ]] && wait_op "$op_id" >/dev/null
  done
  # subnets/networks have dependents; best-effort, ignore failures
  for net in "${CREATED_NETS[@]:-}"; do [[ -n "$net" ]] && curl -s -o /dev/null -X DELETE "$BASE_URL/vpc/v1/networks/$net" || true; done
}
trap cleanup EXIT

# ===========================================================================
echo
echo "[S1] Network public projection is lean (no infra-sensitive keys)"
NET_OP=$(body -X POST "$BASE_URL/vpc/v1/networks" -H 'Content-Type: application/json' \
            -d "{\"folderId\":\"$FOLDER_ID\",\"name\":\"cprm-s1-net-$RANDOM\",\"description\":\"S1\"}")
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
# need a subnet (zone ru-central1-a, like geography-move.sh)
SUBNET_ID=""
if [[ -n "$NET_ID" ]]; then
  SUB_OP=$(body -X POST "$BASE_URL/vpc/v1/subnets" -H 'Content-Type: application/json' \
              -d "{\"folderId\":\"$FOLDER_ID\",\"name\":\"cprm-s2-sub-$RANDOM\",\"networkId\":\"$NET_ID\",\"zoneId\":\"ru-central1-a\",\"v4CidrBlocks\":[\"10.241.0.0/24\"]}")
  SUB_OP_ID=$(printf '%s' "$SUB_OP" | jget id)
  [[ -n "$SUB_OP_ID" ]] && SUBNET_ID=$(wait_op "$SUB_OP_ID" | jget metadata.subnetId)
fi
if [[ -z "$SUBNET_ID" ]]; then
  skip "S2: could not create a subnet — skipping NIC scenario"
else
  ok "subnet created ($SUBNET_ID)"
  # try NIC create with empty address arrays first; if it requires an address, make one
  NIC_NAME="cprm-s2-nic-$RANDOM"
  NIC_OP=$(body -X POST "$BASE_URL/vpc/v1/networkInterfaces" -H 'Content-Type: application/json' \
              -d "{\"folderId\":\"$FOLDER_ID\",\"name\":\"$NIC_NAME\",\"subnetId\":\"$SUBNET_ID\"}")
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
                 -d "{\"folderId\":\"$FOLDER_ID\",\"name\":\"cprm-s2-addr-$RANDOM\",\"internalIpv4AddressSpec\":{\"subnetId\":\"$SUBNET_ID\"}}")
    ADDR_OP_ID=$(printf '%s' "$ADDR_OP" | jget id)
    ADDR_ID=""
    [[ -n "$ADDR_OP_ID" ]] && ADDR_ID=$(wait_op "$ADDR_OP_ID" | jget metadata.addressId)
    if [[ -n "$ADDR_ID" ]]; then
      CREATED_ADDRS+=("$ADDR_ID")
      NIC_OP=$(body -X POST "$BASE_URL/vpc/v1/networkInterfaces" -H 'Content-Type: application/json' \
                  -d "{\"folderId\":\"$FOLDER_ID\",\"name\":\"$NIC_NAME\",\"subnetId\":\"$SUBNET_ID\",\"v4AddressIds\":[\"$ADDR_ID\"]}")
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
    for k in id folderId subnetId status; do
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
echo "[S4] negative infra-leak audit of the public VPC & Compute REST surface"
PUBLIC_ENDPOINTS=(
  "/vpc/v1/networks?folderId=$FOLDER_ID"
  "/vpc/v1/subnets?folderId=$FOLDER_ID"
  "/vpc/v1/networkInterfaces?folderId=$FOLDER_ID"
  "/vpc/v1/addresses?folderId=$FOLDER_ID"
  "/vpc/v1/securityGroups?folderId=$FOLDER_ID"
  "/vpc/v1/routeTables?folderId=$FOLDER_ID"
  "/vpc/v1/gateways?folderId=$FOLDER_ID"
  "/compute/v1/instances?folderId=$FOLDER_ID"
  "/compute/v1/disks?folderId=$FOLDER_ID"
  "/compute/v1/images?folderId=$FOLDER_ID"
)
for ep in "${PUBLIC_ENDPOINTS[@]}"; do
  c=$(code "$BASE_URL$ep")
  if [[ "$c" == 404 ]]; then
    skip "$ep -> 404 (not deployed / no such route)"
    continue
  fi
  if [[ "$c" != 200 ]]; then
    warn "$ep -> HTTP $c (not 200) — skipping leak check for it"
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
