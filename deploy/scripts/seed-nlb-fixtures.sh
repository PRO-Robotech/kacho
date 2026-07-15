#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# kacho-deploy/scripts/seed-nlb-fixtures.sh — KAC-NLB
#
# Seeds the resources the kacho-nlb newman / e2e suites need to exist BEFORE
# they run. Idempotent: re-runs reuse resources discovered by name; new ids
# overwrite the previous .seeded-ids.env at repo root.
#
# Resources created (all in `existingProjectId` resolved from
# tests/authz-fixtures/out/authz-fixtures.json, falling back to the first
# project returned by /iam/v1/projects):
#
#   - VPC Network        (name: kac-nlb-seed-net)
#   - VPC Subnet         (name: kac-nlb-seed-subnet, cidr 10.130.0.0/24,
#                         existingZoneId from compute) — populates
#                         `existingSubnetId` (Target.ip_ref tests + INTERNAL
#                         listener subnet binding).
#   - VPC Address  EXT   (name: kac-nlb-seed-ext-addr; external pool —
#                         populates `existingExternalAddressId` for BYO-VIP
#                         test).
#   - Compute Instance   (name: kac-nlb-seed-inst; minimal NIC on the seed
#                         subnet) — populates `existingInstanceId` for
#                         Target.instance_id tests.
#   - VPC NIC            (the primary NetworkInterface created by Instance —
#                         id discovered post-create) — populates
#                         `existingNicId` for Target.nic_id tests.
#
# Outputs (idempotent):
#   - .seeded-ids.env at repo root — sourceable KEY=VALUE pairs, used by
#     newman environment-patch scripts (tests/authz-fixtures/patch-env.py
#     family) and ad-hoc CI invocations.
#
# Env:
#   BASE_URL  api-gateway REST endpoint (default http://localhost:28080).
#   JWT       Bearer to use for the Create calls. Empty → anonymous (works
#             only on dev stand with authn=dev + authz disabled). CI passes
#             $jwtAccountAdminA from authz-fixtures.json.
#   OUT_FILE  path to seeded-ids.env (default <repo-root>/.seeded-ids.env).
#   VERBOSE   true → echo every curl.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

BASE_URL="${BASE_URL:-http://localhost:28080}"
JWT="${JWT:-}"
OUT_FILE="${OUT_FILE:-$REPO_ROOT/.seeded-ids.env}"
VERBOSE="${VERBOSE:-false}"

log() { echo "[seed-nlb] $*" >&2; }
vrun() { if [ "$VERBOSE" = "true" ]; then echo "+ $*" >&2; fi; "$@"; }

# Auth header — only emit when JWT is non-empty so dev stands without authn
# work out of the box.
auth_args=()
if [ -n "$JWT" ]; then
  auth_args=(-H "Authorization: Bearer $JWT")
fi

curl_json() {
  local method="$1"; shift
  local path="$1"; shift
  local body="${1:-}"
  if [ -n "$body" ]; then
    vrun curl -sS -X "$method" "$BASE_URL$path" \
      -H 'Content-Type: application/json' \
      "${auth_args[@]}" \
      --data "$body"
  else
    vrun curl -sS -X "$method" "$BASE_URL$path" "${auth_args[@]}"
  fi
}

# wait_op <operation-id> — poll OperationService.Get until done=true.
# Returns the operation JSON on stdout. Times out after 60s.
wait_op() {
  local op_id="$1"
  local deadline=$(( $(date +%s) + 60 ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    local op
    op=$(curl_json GET "/operations/$op_id")
    if [ "$(printf '%s' "$op" | python3 -c 'import sys,json
try: d=json.load(sys.stdin); print("1" if d.get("done") else "")
except Exception: print("")')" = "1" ]; then
      printf '%s' "$op"
      return 0
    fi
    sleep 1
  done
  log "FATAL: operation $op_id did not finish in 60s"
  return 1
}

# extract <jq-like-path> <json-on-stdin>
extract() {
  PYPATH="$1" python3 -c '
import sys, json, os
path = os.environ["PYPATH"].split(".")
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
for p in path:
    if isinstance(d, dict):
        d = d.get(p)
    elif isinstance(d, list):
        try: d = d[int(p)]
        except Exception: d = None
    if d is None: break
if d is None:
    print("")
elif isinstance(d, (str, int, bool)):
    print(d)
else:
    print(json.dumps(d))
'
}

# ─── 1) Resolve project + zone -----------------------------------------------
PROJECT_ID="${existingProjectId:-}"
if [ -z "$PROJECT_ID" ] && [ -f "$REPO_ROOT/../../tests/authz-fixtures/out/authz-fixtures.json" ]; then
  PROJECT_ID=$(python3 -c "
import json
with open('$REPO_ROOT/../../tests/authz-fixtures/out/authz-fixtures.json') as f:
    d = json.load(f)
print(d.get('projectA1Id', ''))
")
fi
if [ -z "$PROJECT_ID" ]; then
  PROJECT_ID=$(curl_json GET "/iam/v1/projects?pageSize=1" | extract "projects.0.id")
fi
if [ -z "$PROJECT_ID" ]; then
  log "FATAL: cannot resolve a projectId (no fixtures, no projects in /iam/v1/projects). Run tests/authz-fixtures/setup.sh first."
  exit 1
fi
log "1/5 project_id=$PROJECT_ID"

ZONE_ID=$(curl_json GET "/compute/v1/zones?pageSize=1" | extract "zones.0.id")
[ -n "$ZONE_ID" ] || ZONE_ID="ru-central1-a"
log "    zone_id=$ZONE_ID"

REGION_ID=$(curl_json GET "/compute/v1/zones/$ZONE_ID" | extract "regionId")
[ -n "$REGION_ID" ] || REGION_ID="ru-central1"
log "    region_id=$REGION_ID"

# ─── 2) Ensure VPC Network ---------------------------------------------------
NET_LIST=$(curl_json GET "/vpc/v1/networks?folderId=$PROJECT_ID&pageSize=200")
NET_ID=$(printf '%s' "$NET_LIST" | python3 -c '
import sys, json
try: d=json.load(sys.stdin)
except Exception: sys.exit(0)
for n in d.get("networks", []):
    if n.get("name") == "kac-nlb-seed-net":
        print(n.get("id","")); sys.exit(0)
print("")
')
if [ -z "$NET_ID" ]; then
  log "2/5 creating Network kac-nlb-seed-net"
  body='{"projectId":"'"$PROJECT_ID"'","name":"kac-nlb-seed-net","description":"KAC-NLB seed fixture"}'
  op=$(curl_json POST "/vpc/v1/networks" "$body")
  op_id=$(printf '%s' "$op" | extract "id")
  NET_ID=$(wait_op "$op_id" | extract "metadata.networkId")
else
  log "2/5 reusing existing Network $NET_ID"
fi

# ─── 3) Ensure VPC Subnet ----------------------------------------------------
SUB_LIST=$(curl_json GET "/vpc/v1/networks/$NET_ID/subnets?pageSize=200")
SUBNET_ID=$(printf '%s' "$SUB_LIST" | python3 -c '
import sys, json
try: d=json.load(sys.stdin)
except Exception: sys.exit(0)
for n in d.get("subnets", []):
    if n.get("name") == "kac-nlb-seed-subnet":
        print(n.get("id","")); sys.exit(0)
print("")
')
if [ -z "$SUBNET_ID" ]; then
  log "3/5 creating Subnet kac-nlb-seed-subnet"
  body='{"folderId":"'"$PROJECT_ID"'","networkId":"'"$NET_ID"'","name":"kac-nlb-seed-subnet","zoneId":"'"$ZONE_ID"'","v4CidrBlocks":["10.130.0.0/24"]}'
  op=$(curl_json POST "/vpc/v1/subnets" "$body")
  op_id=$(printf '%s' "$op" | extract "id")
  SUBNET_ID=$(wait_op "$op_id" | extract "metadata.subnetId")
else
  log "3/5 reusing existing Subnet $SUBNET_ID"
fi

# ─── 4) Ensure External Address (BYO VIP) ----------------------------------
ADDR_LIST=$(curl_json GET "/vpc/v1/addresses?folderId=$PROJECT_ID&pageSize=200")
EXT_ADDR_ID=$(printf '%s' "$ADDR_LIST" | python3 -c '
import sys, json
try: d=json.load(sys.stdin)
except Exception: sys.exit(0)
for a in d.get("addresses", []):
    if a.get("name") == "kac-nlb-seed-ext-addr":
        print(a.get("id","")); sys.exit(0)
print("")
')
if [ -z "$EXT_ADDR_ID" ]; then
  log "4/5 creating external Address kac-nlb-seed-ext-addr"
  body='{"folderId":"'"$PROJECT_ID"'","name":"kac-nlb-seed-ext-addr","externalIpv4Address":{"regionId":"'"$REGION_ID"'"}}'
  op=$(curl_json POST "/vpc/v1/addresses" "$body")
  op_id=$(printf '%s' "$op" | extract "id")
  EXT_ADDR_ID=$(wait_op "$op_id" | extract "metadata.addressId" || true)
  if [ -z "$EXT_ADDR_ID" ]; then
    log "    Address.Create rejected (no AddressPool seeded?) — leaving existingExternalAddressId blank"
  fi
else
  log "4/5 reusing existing Address $EXT_ADDR_ID"
fi

# ─── 5) Ensure Compute Instance + discover its NIC -------------------------
INST_LIST=$(curl_json GET "/compute/v1/instances?folderId=$PROJECT_ID&pageSize=200")
INSTANCE_ID=$(printf '%s' "$INST_LIST" | python3 -c '
import sys, json
try: d=json.load(sys.stdin)
except Exception: sys.exit(0)
for i in d.get("instances", []):
    if i.get("name") == "kac-nlb-seed-inst":
        print(i.get("id","")); sys.exit(0)
print("")
')
if [ -z "$INSTANCE_ID" ]; then
  log "5/5 creating Instance kac-nlb-seed-inst"
  body=$(cat <<EOF
{
  "folderId":"$PROJECT_ID",
  "zoneId":"$ZONE_ID",
  "name":"kac-nlb-seed-inst",
  "resourcesSpec":{"memory":"1073741824","cores":"1"},
  "networkInterfaceSpecs":[{"subnetId":"$SUBNET_ID","primaryV4AddressSpec":{}}]
}
EOF
  )
  op=$(curl_json POST "/compute/v1/instances" "$body")
  op_id=$(printf '%s' "$op" | extract "id")
  INSTANCE_ID=$(wait_op "$op_id" | extract "metadata.instanceId" || true)
  if [ -z "$INSTANCE_ID" ]; then
    log "    Instance.Create rejected (acceptance-gate / missing image?) — leaving existingInstanceId blank"
  fi
else
  log "5/5 reusing existing Instance $INSTANCE_ID"
fi

NIC_ID=""
if [ -n "$INSTANCE_ID" ]; then
  inst=$(curl_json GET "/compute/v1/instances/$INSTANCE_ID")
  NIC_ID=$(printf '%s' "$inst" | python3 -c '
import sys, json
try: d=json.load(sys.stdin)
except Exception: sys.exit(0)
nics = d.get("networkInterfaces") or []
if nics:
    print(nics[0].get("id") or nics[0].get("networkInterfaceId",""))
else:
    print("")
')
fi

# ─── Write .seeded-ids.env --------------------------------------------------
log "writing $OUT_FILE"
cat >"$OUT_FILE" <<EOF
# Auto-generated by scripts/seed-nlb-fixtures.sh — do not edit.
existingProjectId=$PROJECT_ID
existingRegionId=$REGION_ID
existingZoneId=$ZONE_ID
existingNetworkId=$NET_ID
existingSubnetId=$SUBNET_ID
existingExternalAddressId=$EXT_ADDR_ID
existingInstanceId=$INSTANCE_ID
existingNicId=$NIC_ID
EOF

log "done"
cat "$OUT_FILE"
