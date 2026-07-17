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
#   - VPC AddressPool EXT (name: kac-nlb-seed-ext-pool; EXTERNAL_PUBLIC, zonal,
#                         is_default=true — the IPAM source every EXTERNAL nlb
#                         auto-VIP + zonal external Address resolves via
#                         GetDefaultForZone. Internal mux only, ban #6).
#   - VPC Address  EXT   (name: kac-nlb-seed-ext-addr; allocated from the pool
#                         above — populates `existingExternalAddressId` for
#                         BYO-VIP test).
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
# INTERNAL_BASE_URL — api-gateway cluster-internal REST listener (:8081). The
# InternalAddressPoolService (admin IPAM) is exposed ONLY there (ban #6) — never on
# the public {{baseUrl}}. Default mirrors the BASE_URL host with the internal port
# so an operator running `make seed-nlb` after a `port-forward svc/api-gateway
# 28081:8081` gets external-pool provisioning out of the box; the umbrella
# (newman-e2e.sh) overrides it to the port it forwards (:18081).
INTERNAL_BASE_URL="${INTERNAL_BASE_URL:-http://localhost:28081}"
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

# curl_internal — same as curl_json but against the cluster-internal REST listener
# (Internal*-RPC live there only, ban #6).
curl_internal() {
  local method="$1"; shift
  local path="$1"; shift
  local body="${1:-}"
  if [ -n "$body" ]; then
    vrun curl -sS -X "$method" "$INTERNAL_BASE_URL$path" \
      -H 'Content-Type: application/json' \
      "${auth_args[@]}" \
      --data "$body"
  else
    vrun curl -sS -X "$method" "$INTERNAL_BASE_URL$path" "${auth_args[@]}"
  fi
}

# wait_op <operation-id> — poll OperationService.Get until done=true.
# Returns the operation JSON on stdout. Times out after 60s.
wait_op() {
  local op_id="$1"
  # Fast-fail on empty id: a Create that returned an error envelope (e.g.
  # ALREADY_EXISTS, or a validation reject) has no operation id — polling it
  # would just burn the full 60s deadline before FATAL. Surface it immediately
  # so the caller's `|| true` / blank-id guard can proceed.
  if [ -z "$op_id" ]; then
    log "wait_op: empty operation id (create returned an error, not an Operation) — skipping"
    return 1
  fi
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
NET_LIST=$(curl_json GET "/vpc/v1/networks?projectId=$PROJECT_ID&pageSize=200")
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
  body='{"projectId":"'"$PROJECT_ID"'","networkId":"'"$NET_ID"'","name":"kac-nlb-seed-subnet","zoneId":"'"$ZONE_ID"'","placementType":"ZONAL","v4CidrBlocks":["10.130.0.0/24"]}'
  op=$(curl_json POST "/vpc/v1/subnets" "$body")
  op_id=$(printf '%s' "$op" | extract "id")
  SUBNET_ID=$(wait_op "$op_id" | extract "metadata.subnetId")
else
  log "3/5 reusing existing Subnet $SUBNET_ID"
fi

# ─── 3.5) Ensure External AddressPool (IPAM source for external VIPs) --------
# The nlb EXTERNAL suites auto-allocate a public VIP (v4Source:{public:{}}) and
# self-provision a ZONAL external vpc Address (externalIpv4AddressSpec.zoneId =
# existingZoneId). Both resolve their pool via GetDefaultForZone(zone, EXTERNAL_PUBLIC)
# = `WHERE zone_id=$zone AND kind='EXTERNAL_PUBLIC' AND is_default=true` (vpc
# address_pool.go). Without a DEFAULT external pool in the zone that query returns
# NotFound → Address.Create / EXTERNAL LB.Create fails ("zone_id is empty" / no VIP)
# → whole external-nlb chain reds. seed-ipam is a deliberate NOOP (admin-explicit),
# so provision it here. AddressPool is InternalAddressPoolService → internal mux only
# (ban #6), returns the resource DIRECTLY (not an Operation). Idempotent by name;
# best-effort (|| true) so a stand without the internal port-forward degrades to the
# pre-existing behaviour instead of aborting the whole seed.
POOL_LIST=$(curl_internal GET "/vpc/v1/addressPools?pageSize=200" 2>/dev/null || echo '{}')
POOL_ID=$(printf '%s' "$POOL_LIST" | python3 -c '
import sys, json
try: d=json.load(sys.stdin)
except Exception: sys.exit(0)
for p in d.get("pools", []):
    if p.get("name") == "kac-nlb-seed-ext-pool":
        print(p.get("id","")); sys.exit(0)
print("")
')
if [ -z "$POOL_ID" ]; then
  log "3.5/5 creating external AddressPool kac-nlb-seed-ext-pool (EXTERNAL_PUBLIC, zone=$ZONE_ID)"
  # 198.51.100.0/24 = TEST-NET-2 (RFC 5737) — the documented production external
  # CIDR (see `make seed-ipam`); on a fresh stand no EXTERNAL_PUBLIC pool exists so
  # the address_pool_cidrs EXCLUDE (kind, block &&) does not conflict.
  pbody='{"name":"kac-nlb-seed-ext-pool","description":"KAC-NLB seed external VIP pool","kind":"EXTERNAL_PUBLIC","zoneId":"'"$ZONE_ID"'","v4CidrBlocks":["198.51.100.0/24"],"v6CidrBlocks":[]}'
  POOL_ID=$(curl_internal POST "/vpc/v1/addressPools" "$pbody" | extract "id" || true)
  if [ -n "$POOL_ID" ]; then
    # Allocation picks the pool ONLY when is_default=true for (zone, kind); the
    # Create RPC has no isDefault field, so flip it via Update (update_mask=isDefault).
    curl_internal PATCH "/vpc/v1/addressPools/$POOL_ID" \
      '{"updateMask":"isDefault","isDefault":true}' >/dev/null 2>&1 || \
      log "    could not set is_default on $POOL_ID (a default pool for this zone/kind may already exist)"
  else
    log "    AddressPool.Create did not return an id (internal mux unreachable at $INTERNAL_BASE_URL, or insufficient admin tier) — external VIP allocation may fail; whitelist non-T31 nlb external-create cases if so"
  fi
else
  log "3.5/5 reusing existing external AddressPool $POOL_ID"
fi

# ─── 4) Ensure External Address (BYO VIP) ----------------------------------
ADDR_LIST=$(curl_json GET "/vpc/v1/addresses?projectId=$PROJECT_ID&pageSize=200")
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
  body='{"projectId":"'"$PROJECT_ID"'","name":"kac-nlb-seed-ext-addr","externalIpv4Address":{"regionId":"'"$REGION_ID"'"}}'
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
INST_LIST=$(curl_json GET "/compute/v1/instances?projectId=$PROJECT_ID&pageSize=200")
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
  "projectId":"$PROJECT_ID",
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
existingExternalPoolId=$POOL_ID
existingExternalAddressId=$EXT_ADDR_ID
existingInstanceId=$INSTANCE_ID
existingNicId=$NIC_ID
EOF

log "done"
cat "$OUT_FILE"
