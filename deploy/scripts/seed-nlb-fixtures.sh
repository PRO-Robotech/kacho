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
#   - VPC Network        (name: kac-nlb-seed-net — placement-FREE, see below)
#   - VPC Subnet         (name: kac-nlb-seed-subnet-<zone>, cidr 10.130.<zone-octet>.0/24,
#                         zone = NLB_ZONE_ID) — populates
#                         `existingSubnetId` (Target.ip_ref tests + INTERNAL
#                         listener subnet binding).
#   - VPC AddressPool EXT (name: kac-nlb-seed-ext-pool; EXTERNAL_PUBLIC, ZONAL,
#                         is_default=true — the IPAM source the suite's ZONE-PINNED
#                         external Addresses (BYO-VIP fixtures) resolve via
#                         GetDefaultForZone. Internal mux only, ban #6).
#   - VPC AddressPool ANYCAST (name: kac-nlb-seed-anycast-pool; EXTERNAL_PUBLIC,
#                         ZONE-INDEPENDENT, is_default=true) — the IPAM source every
#                         EXTERNAL NetworkLoadBalancer auto-VIP resolves. EXTERNAL is
#                         always EXTERNAL_REGIONAL, i.e. REGIONAL/anycast, i.e.
#                         zone-independent by construction, so its VIP is allocated
#                         with NO zone and lands in the zone-independent lane. See
#                         step 3.6 for why a cluster-wide singleton is safe to own.
#   - VPC Address  EXT   (name: kac-nlb-seed-ext-addr-<zone>; v4 allocated from the
#                         pool above — populates `existingExternalAddressId` for
#                         the BYO-VIP test)
#   - VPC Address  EXT v6 (name: kac-nlb-seed-ext-addr6-<zone>; v6 from the same pool)
#                         — populates `existingAddressIPv6Id`, the BYO-IPv6 handle the
#                         nlb family/slot-mismatch negatives (NLB-CR-VAL-ADDRESS-FAMILY-SLOT,
#                         LST-CR-VAL-BYO-IP-VERSION-MISMATCH, XRES-E2E-V4-LISTENER-V6-ADDRESS-
#                         INVALID) and NLB-CR-CRUD-DUALSTACK-MIXED need. Left unseeded it was a
#                         committed PLACEHOLDER id: the family-mismatch negatives were rejected
#                         for "unknown address" instead of the family/slot reason they claim to
#                         verify, and the two guarded cases fell into their tolerant else-branch
#                         (vacuously green).
#   - Compute MachineType (name: kac-nlb-seed-mt; Internal :8081 admin RPC) — the
#                         COMP-1 single sizing channel the Instance body needs.
#   - Compute Instance   (name: kac-nlb-seed-inst-<zone>; COMP-1 shape
#                         instanceKind/machineTypeId/bootSource + NIC spec on the
#                         seed subnet) — populates `existingInstanceId` for
#                         Target.instance_id tests.
#   - VPC NIC            (kac-nlb-seed-nic-<zone> in the seed subnet) — populates
#                         `existingNicId` for Target.nic_id tests. Seeded as a
#                         first-class vpc NetworkInterface because Instance NIC
#                         materialisation is the COMP-2 launch saga (not landed);
#                         the Instance's own NIC is preferred as soon as it exists.
#
# PLACEMENT-COHERENT REUSE (the whole fixture set, not just the pool)
# ------------------------------------------------------------------
# Every reuse probe matches on (name AND placement), never on name alone. A name-only
# probe silently ADOPTS a fixture that lives in a DIFFERENT zone, which is exactly how
# this seed produced an internally INCOHERENT set: with NLB_ZONE_ID=ru-central1-e the
# Instance was (re)created in zone e while the Subnet / Address / AddressPool were
# adopted by name from zone a (a pre-dedication run) — so `existingZoneId`=e pointed at
# a zone-a subnet, and the "dedicated zone" fix did not take effect at all (observed on
# the live stand: kac-nlb-seed-subnet zone=a, kac-nlb-seed-inst zone=e, kac-nlb-seed-
# ext-pool zone=a is_default v4+v6 → vpc ADR-CR-EXT-V6-FAMILY-FALLTHROUGH still red).
# Two different mechanisms, because the resources are two different KINDS of thing:
#
#   * project-scoped fixtures (Subnet / Address / Instance / NIC) carry a ZONE-QUALIFIED
#     NAME (`…-<zone>`) and a zone-derived CIDR. Per-zone fixture sets then coexist BY
#     CONSTRUCTION (UNIQUE(project,name) is per-name; the subnet EXCLUDE is per-CIDR), a
#     mis-zoned legacy twin can no longer even be name-matched, and nothing has to be
#     destroyed to re-seed into a new zone. A name hit whose zone still differs is an
#     anomaly (someone else took our name) → abort loudly, never adopt.
#   * the AddressPool is NOT project-scoped: `is_default for (zone, kind)` is a GLOBAL,
#     cluster-wide slot, and this script is the sole author of the name
#     `kac-nlb-seed-ext-pool`. A pool of ours sitting in a FOREIGN zone therefore keeps
#     occupying that zone's default slot and keeps breaking the suite that owns the zone —
#     leaving it alone is not neutral. So it is RECLAIMED (is_default cleared first — that
#     alone removes the cross-suite harm — then our own addresses in it released, then the
#     pool deleted) and a fresh pool is created in NLB_ZONE_ID. Reclaim is best-effort: a
#     pool still holding OTHER suites' leases cannot be deleted, and deleting foreign
#     leases is not this script's business — the un-default already un-breaks the zone.
#     The pool's CIDR blocks are ZONE-DERIVED (100.102.<octet>.0/24 + 2001:db8:e2e:<octet>::/64,
#     mirroring the per-suite block convention: vpc internal-pool 100.100/16, vpc address
#     100.101/16) precisely so a surviving stale pool cannot block the new one via the
#     GLOBAL `address_pool_cidrs EXCLUDE (kind, block &&)`.
#
# Outputs (idempotent):
#   - .seeded-ids.env at repo root — sourceable KEY=VALUE pairs, used by
#     newman environment-patch scripts (tests/authz-fixtures/patch-env.py
#     family) and ad-hoc CI invocations.
#
# ZONE OWNERSHIP (директива #2, перенесённая на AddressPool): kac-nlb-seed-ext-pool is
# EXTERNAL_PUBLIC + is_default, and "default for (zone, kind)" is a GLOBAL, cluster-wide
# slot — it cannot be isolated by account/project, only by ZONE. The nlb suite therefore
# owns a DEDICATED zone (NLB_ZONE_ID, ru-central1-e on the umbrella stand) and seeds its
# network/subnet/instance/address/pool there. Previously this script took
# `GET /geo/v1/zones?pageSize=1` = ru-central1-a and planted a default pool CARRYING
# 2001:db8:e2e:100::/64 in it, which deterministically broke the vpc case
# ADR-CR-EXT-V6-FAMILY-FALLTHROUGH (it asserts zone a has NO v6-capable pool, so an
# external-v6 Create must fail FailedPrecondition; with our pool present the address
# allocated and the Operation succeeded). Cross-suite fixture collision, not a flake.
# Pinning NLB_ZONE_ID was only HALF the fix: the reuse probe still matched the pool by
# NAME alone, so on any stand that already carried the zone-a pool the seeder logged
# "reusing existing external AddressPool" and kept the zone-a one — the dedicated zone
# never got a pool at all (its EXTERNAL VIP allocations then failed: live stand shows
# nlb-lb-*-v4 addresses with an EMPTY externalIpv4Address). Hence the placement-aware
# probe + reclaim above.
#
# Env:
#   BASE_URL  api-gateway REST endpoint (default http://localhost:28080).
#   NLB_ZONE_ID  zone this suite owns; every seeded resource (subnet/instance/address)
#             and the external AddressPool land there. Unset → falls back to the first
#             zone of the geo catalog (standalone `make seed-nlb` behaviour). The id is
#             validated against the geo catalog: a non-existent zone aborts loudly
#             instead of silently degrading to zone[0] and re-creating the collision.
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

# ADMIN_JWT — credential for the cluster-internal AddressPool RPCs ONLY.
# InternalAddressPoolService is authorized on the CLUSTER SINGLETON
# (`cluster:cluster_root`, action vpc.address_pools.{list,create,…}), not on the
# caller's project/account: a project- or account-scoped grantor (e.g. the umbrella's
# jwtAccountAdminA) gets a hard 403 "no authorization path to the resource" on both the
# Create AND the reuse-probe List. When the probe 403s, the script cannot see the pool
# that already exists, concludes "no EXTERNAL_PUBLIC pool in zone" and FATALs — so the
# whole seed aborts and OUT_FILE is never written. Keep project-scoped resources
# (network/subnet/instance) owned by $JWT and use $ADMIN_JWT only for the pool steps.
# Defaults to $JWT so standalone/dev invocations behave exactly as before.
ADMIN_JWT="${ADMIN_JWT:-$JWT}"
admin_auth_args=()
if [ -n "$ADMIN_JWT" ]; then
  admin_auth_args=(-H "Authorization: Bearer $ADMIN_JWT")
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

# curl_admin — PUBLIC listener, but with $ADMIN_JWT. Needed for reads whose
# scope_extractor is the CLUSTER SINGLETON rather than the caller's project — e.g.
# MachineTypeService/List (`viewer` @ cluster): the project-scoped grantor $JWT gets a
# hard 403 there, so probing the catalog with it would look like "no machine types".
curl_admin() {
  local method="$1"; shift
  local path="$1"; shift
  local body="${1:-}"
  if [ -n "$body" ]; then
    vrun curl -sS -X "$method" "$BASE_URL$path" \
      -H 'Content-Type: application/json' \
      "${admin_auth_args[@]}" \
      --data "$body"
  else
    vrun curl -sS -X "$method" "$BASE_URL$path" "${admin_auth_args[@]}"
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
      "${admin_auth_args[@]}" \
      --data "$body"
  else
    vrun curl -sS -X "$method" "$INTERNAL_BASE_URL$path" "${admin_auth_args[@]}"
  fi
}

# wait_op <operation-id> [channel] — poll OperationService.Get until done=true.
# Returns the operation JSON on stdout. Times out after 60s.
#
# channel ∈ {public (default) | internal} — POLL THE OPERATION THROUGH THE SAME CHANNEL
# THAT CREATED IT, i.e. same listener AND same credential (`public` = BASE_URL + $JWT,
# `internal` = INTERNAL_BASE_URL + $ADMIN_JWT — see curl_json / curl_internal).
#
# Why this matters (measured on the live stand, operation epd6rpv73d2r8fa76a4f = the
# MachineType create): OperationService HIDES another principal's operation as
# `404 {"code":5,"message":"operation … not found"}` (existence-hiding, not a 403).
# The MachineType is created with $ADMIN_JWT on the internal mux, but wait_op polled with
# curl_json = $JWT (the project grantor) — that principal gets the 404 FOREVER, so the loop
# could never observe done=true. Measured, same op id:
#     ADMIN_JWT  → public 200 done:true   internal 200 done:true
#     JWT        → public 404             internal 404
# i.e. the discriminator is the PRINCIPAL, not the port. The old code therefore burned the
# whole 60s budget on every seed run and logged the misleading
# `FATAL: operation … did not finish in 60s` for a MachineType that had in fact been
# created a second earlier (the by-name re-probe below then found it, which is why the
# failure looked cosmetic while costing a minute — and hid a real op.error if there was one).
wait_op() {
  local op_id="$1" channel="${2:-public}"
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
    if [ "$channel" = "internal" ]; then
      op=$(curl_internal GET "/operations/$op_id")
    else
      op=$(curl_json GET "/operations/$op_id")
    fi
    if [ "$(printf '%s' "$op" | python3 -c 'import sys,json
try: d=json.load(sys.stdin); print("1" if d.get("done") else "")
except Exception: print("")')" = "1" ]; then
      printf '%s' "$op"
      return 0
    fi
    sleep 1
  done
  log "FATAL: operation $op_id did not finish in 60s (polled on the $channel listener)"
  return 1
}

# wait_op_field <operation-id> <metadata-field> — poll to done, REJECT on op.error, then
# print metadata.<field>. Kachō pre-allocates the resource id in Operation.metadata even
# on a done+error Operation (the id is minted before the async failure), so extracting it
# without checking `error` yields a PHANTOM id for a resource that does not exist — the
# env then points at nothing and every downstream cross-service peer-check 404s
# (.claude/rules/testing.md, fixture-seed rule). Prints nothing + returns 1 on error.
wait_op_field() {
  local op_id="$1" field="$2" channel="${3:-public}" op err
  op=$(wait_op "$op_id" "$channel") || return 1
  err=$(printf '%s' "$op" | extract "error")
  if [ -n "$err" ] && [ "$err" != "null" ] && [ "$err" != "{}" ]; then
    log "    operation $op_id finished with error: $err"
    return 1
  fi
  printf '%s' "$op" | extract "metadata.$field"
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

# api_err <response-body> — prints the gRPC error when the body is an error envelope
# ({"code":N,"message":"…"}), prints nothing on success. curl exits 0 on 4xx/5xx (no -f),
# so `curl … >/dev/null || log "failed"` NEVER fires and reports success for a refused
# call — which is how the reclaim below claimed "deleted stale pool" while the pool was
# still there (it was refused: FailedPrecondition, leases still allocated). Grade the
# BODY, not the process exit status.
api_err() {
  printf '%s' "$1" | python3 -c '
import sys, json
raw = sys.stdin.read().strip()
if not raw:                      # empty body = success (Delete → google.protobuf.Empty)
    sys.exit(0)
try:
    d = json.loads(raw)
except Exception:
    print("non-JSON response: " + raw[:120]); sys.exit(0)
if isinstance(d, dict) and isinstance(d.get("code"), int) and d.get("message"):
    print("%s (code %s)" % (d["message"], d["code"]))
'
}

# probe_fixture <list-key> <want-name> <want-zone> <zone-path> — PLACEMENT-AWARE reuse probe.
# Reads a List response on stdin, prints "<status>\t<id>\t<zone>" where
#   match     — a resource with that name AND that placement exists → safe to reuse
#   mismatch  — the name exists, but in ANOTHER zone → our own stale/foreign artifact,
#               NEVER reuse (adopting it is what silently defeated NLB_ZONE_ID)
#   none      — no such name (or the list was unreadable) → create it
# zone-path is a dotted JSON path, or several '|'-separated alternatives (an Address
# carries its zone under externalIpv4Address.zoneId OR externalIpv6Address.zoneId). An
# EMPTY zone-path declares the resource placement-FREE (vpc Network): name-only reuse is
# then correct, not sloppy — there is no placement to disagree about.
probe_fixture() {
  LIST_KEY="$1" WANT_NAME="$2" WANT_ZONE="$3" ZONE_PATH="$4" python3 -c '
import os, sys, json
try:
    d = json.load(sys.stdin)
except Exception:
    print("none\t\t"); sys.exit(0)
items = d.get(os.environ["LIST_KEY"]) or []
want_name = os.environ["WANT_NAME"]
want_zone = os.environ["WANT_ZONE"]
paths = [p for p in os.environ["ZONE_PATH"].split("|") if p]

def zone_of(it):
    for p in paths:
        cur = it
        for seg in p.split("."):
            if not isinstance(cur, dict):
                cur = None
                break
            cur = cur.get(seg)
        if cur:
            return cur
    return ""

hits = [it for it in items if it.get("name") == want_name]
if not hits:
    print("none\t\t"); sys.exit(0)
if not paths:                      # placement-free resource — name is the whole identity
    print("match\t%s\t" % hits[0].get("id", "")); sys.exit(0)
same = [it for it in hits if zone_of(it) == want_zone]
if same:
    print("match\t%s\t%s" % (same[0].get("id", ""), zone_of(same[0]))); sys.exit(0)
print("mismatch\t%s\t%s" % (hits[0].get("id", ""), zone_of(hits[0])))
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
log "1/6 project_id=$PROJECT_ID"

# Geography (Region/Zone) is owned by kacho-geo in the redesign — compute dropped its
# zones table. Read the axis from the geo public catalog (project-scope EXEMPT, authN-
# only) so the resolved zone actually exists for the AddressPool peer-validate below.
#
# NLB_ZONE_ID (the suite's DEDICATED zone — see the header) wins when set; it is
# EXISTENCE-CHECKED against the catalog rather than trusted blindly, because a silent
# fallback to zone[0] is exactly how the default external pool ends up in a zone another
# suite makes assertions about.
if [ -n "${NLB_ZONE_ID:-}" ]; then
  if [ "$(curl_json GET "/geo/v1/zones/$NLB_ZONE_ID" | extract "id")" = "$NLB_ZONE_ID" ]; then
    ZONE_ID="$NLB_ZONE_ID"
    log "    using nlb-dedicated zone NLB_ZONE_ID=$ZONE_ID"
  else
    log "FATAL: NLB_ZONE_ID='$NLB_ZONE_ID' is not in the geo catalog."
    log "       Seed it first (tests/authz-fixtures/setup.sh → prodseed_matrix.py) — refusing to fall back to"
    log "       zone[0], which would plant this suite's default EXTERNAL_PUBLIC pool in a zone owned"
    log "       by another suite (vpc ADR-CR-EXT-V6-FAMILY-FALLTHROUGH / IPL-RESOLVE-NETWORK-DEFAULT-FAMILY-SKIP)."
    exit 1
  fi
else
  ZONE_ID=$(curl_json GET "/geo/v1/zones?pageSize=1" | extract "zones.0.id")
  [ -n "$ZONE_ID" ] || ZONE_ID="ru-central1-a"
fi
log "    zone_id=$ZONE_ID"

REGION_ID=$(curl_json GET "/geo/v1/zones/$ZONE_ID" | extract "regionId")
[ -n "$REGION_ID" ] || REGION_ID="ru-central1"
log "    region_id=$REGION_ID"

# ─── 1.5) Zone-derived fixture identity --------------------------------------
# Placement-scoped fixtures are NAMED and ADDRESSED per zone, so a set seeded for zone X
# can never be confused with (or blocked by) a set left behind for zone Y — see the
# PLACEMENT-COHERENT REUSE note in the header. ZOCT is a stable per-zone octet (1..254,
# never 0 → never collides with the legacy zone-a 10.130.0.0/24 seed subnet).
ZTAG=$(printf '%s' "$ZONE_ID" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9-' '-')
ZOCT=$(ZONE="$ZONE_ID" python3 -c 'import hashlib, os; print(int(hashlib.md5(os.environ["ZONE"].encode()).hexdigest(), 16) % 254 + 1)')

NET_NAME="kac-nlb-seed-net"                    # placement-FREE (a vpc Network carries no zone)
# Declared address plan of the seed network. Wide on purpose: the subnets carved in it
# are per-zone (10.130.$ZOCT.0/24 here) and per-case (10.x.y.0/24 / fdXX::/64 drawn from
# run entropy by the nlb suites), so a narrow plan would hold a second copy of knowledge
# that lives in those suites and would drift from it silently. The subject of the nlb
# fixtures is the balancer, not the boundaries of the address plan.
NET_SUPERNET_V4="10.0.0.0/8"
NET_SUPERNET_V6="fd00::/8"
SUBNET_NAME="kac-nlb-seed-subnet-$ZTAG"
SUBNET_CIDR="10.130.$ZOCT.0/24"                # per-network EXCLUDE (network_id, cidr &&)
POOL_NAME="kac-nlb-seed-ext-pool"              # GLOBAL default slot — one at a time, reclaimed
POOL_V4="100.102.$ZOCT.0/24"                   # GLOBAL EXCLUDE (kind, block &&) → per-zone block
POOL_V6=$(printf '2001:db8:e2e:%x::/64' "$ZOCT")
ADDR_NAME="kac-nlb-seed-ext-addr-$ZTAG"
ADDR6_NAME="kac-nlb-seed-ext-addr6-$ZTAG"
INST_NAME="kac-nlb-seed-inst-$ZTAG"
NIC_NAME="kac-nlb-seed-nic-$ZTAG"
log "    fixture identity: subnet=$SUBNET_NAME ($SUBNET_CIDR) pool=$POOL_NAME ($POOL_V4 + $POOL_V6)"

# refuse_mismatch <kind> <name> <id> <found-zone> — a placement-scoped fixture carrying OUR
# zone-qualified name but living in ANOTHER zone. Never adopt it (that is the bug this
# whole block exists to kill) and never silently create a twin (UNIQUE(project,name) would
# reject it anyway): stop with an actionable message.
refuse_mismatch() {
  log "FATAL: $1 '$2' already exists as $3 in zone '$4', but this seed targets zone '$ZONE_ID'."
  log "       The name is zone-qualified, so this can only mean the name was taken by something"
  log "       else (or the zone was re-created under the same id). REFUSING to adopt a fixture from"
  log "       a foreign zone — that is exactly how existingZoneId ended up disagreeing with the"
  log "       seeded subnet/address. Delete $3 (or re-point NLB_ZONE_ID) and re-run."
  exit 1
}

# ─── 2) Ensure VPC Network ---------------------------------------------------
# Placement-free: a vpc Network has no zone_id (its SUBNETS carry placement), so name-only
# reuse is coherent here — the per-zone subnets simply live side by side inside it.
NET_LIST=$(curl_json GET "/vpc/v1/networks?projectId=$PROJECT_ID&pageSize=200")
NET_SEL=$(printf '%s' "$NET_LIST" | probe_fixture networks "$NET_NAME" "" "")
NET_ID=$(printf '%s' "$NET_SEL" | cut -f2)
if [ -z "$NET_ID" ]; then
  log "2/6 creating Network $NET_NAME"
  body='{"projectId":"'"$PROJECT_ID"'","name":"'"$NET_NAME"'","description":"KAC-NLB seed fixture",'
  body="$body"'"ipv4CidrBlocks":["'"$NET_SUPERNET_V4"'"],"ipv6CidrBlocks":["'"$NET_SUPERNET_V6"'"]}'
  op=$(curl_json POST "/vpc/v1/networks" "$body")
  op_id=$(printf '%s' "$op" | extract "id")
  NET_ID=$(wait_op "$op_id" | extract "metadata.networkId")
else
  log "2/6 reusing existing Network $NET_ID (placement-free)"
fi

# The address plan is asserted on BOTH branches, and by growing it rather than by
# reading it.
#
# A network that declares no supernet of a family refuses subnets of that family
# outright (sync 400 — there is nothing to carve from). The create branch above now
# declares one; the REUSE branch would otherwise adopt whatever is already there,
# including a network seeded before the plan was required — and every subnet, address,
# NIC and load balancer standing on it would fail far from the cause.
#
# :add-cidr-blocks is idempotent by contract (re-adding a declared block is a no-op,
# not a duplicate), so one unconditional call covers "just created" and "adopted"
# without branching on state that could change between the read and the write. It is
# also the exact path the refusal message points the operator at.
log "2/6 ensuring the address plan on Network $NET_ID ($NET_SUPERNET_V4 + $NET_SUPERNET_V6)"
plan_body='{"ipv4CidrBlocks":["'"$NET_SUPERNET_V4"'"],"ipv6CidrBlocks":["'"$NET_SUPERNET_V6"'"]}'
plan_op=$(curl_json POST "/vpc/v1/networks/$NET_ID:add-cidr-blocks" "$plan_body")
plan_op_id=$(printf '%s' "$plan_op" | extract "id")
wait_op "$plan_op_id" >/dev/null

# ─── 3) Ensure VPC Subnet ----------------------------------------------------
SUB_LIST=$(curl_json GET "/vpc/v1/networks/$NET_ID/subnets?pageSize=200")
SUB_SEL=$(printf '%s' "$SUB_LIST" | probe_fixture subnets "$SUBNET_NAME" "$ZONE_ID" "zoneId")
SUB_STATUS=$(printf '%s' "$SUB_SEL" | cut -f1)
SUBNET_ID=$(printf '%s' "$SUB_SEL" | cut -f2)
case "$SUB_STATUS" in
  mismatch) refuse_mismatch "Subnet" "$SUBNET_NAME" "$SUBNET_ID" "$(printf '%s' "$SUB_SEL" | cut -f3)" ;;
  match)    log "3/6 reusing existing Subnet $SUBNET_ID (zone $ZONE_ID)" ;;
  *)
    SUBNET_ID=""
    log "3/6 creating Subnet $SUBNET_NAME (zone=$ZONE_ID, cidr=$SUBNET_CIDR)"
    # placement_type is server-derived from zoneId (ZONAL) — do NOT send it (redesign
    # placement-coherence: sending placement_type → InvalidArgument).
    body='{"projectId":"'"$PROJECT_ID"'","networkId":"'"$NET_ID"'","name":"'"$SUBNET_NAME"'","zoneId":"'"$ZONE_ID"'","ipv4CidrPrimary":"'"$SUBNET_CIDR"'"}'
    op=$(curl_json POST "/vpc/v1/subnets" "$body")
    op_id=$(printf '%s' "$op" | extract "id")
    SUBNET_ID=$(wait_op_field "$op_id" "subnetId" || true)
    [ -n "$SUBNET_ID" ] || log "    Subnet.Create rejected — leaving existingSubnetId blank (see the operation error above)"
    ;;
esac

# ─── 3.5) Ensure External AddressPool (IPAM source for external VIPs) --------
# The nlb EXTERNAL suites auto-allocate a public VIP (v4Source:{public:{}}) and
# self-provision a ZONAL external vpc Address (externalIpv4AddressSpec.zoneId =
# existingZoneId). Both resolve their pool via GetDefaultForZone(zone, EXTERNAL_PUBLIC)
# = `WHERE zone_id=$zone AND kind='EXTERNAL_PUBLIC' AND is_default=true` (vpc
# address_pool.go). Without a DEFAULT external pool in the zone that query returns
# NotFound → Address.Create / EXTERNAL LB.Create fails ("zone_id is empty" / no VIP)
# → whole external-nlb chain reds. Nothing seeds a pool earlier - creating one is an
# explicit admin act, not part of bringing a stand up - so provision it here. AddressPool is InternalAddressPoolService → internal mux only
# (ban #6), returns the resource DIRECTLY (not an Operation). Idempotent by name;
# best-effort (|| true) so a stand without the internal port-forward degrades to the
# pre-existing behaviour instead of aborting the whole seed.
POOL_LIST=$(curl_internal GET "/vpc/v1/addressPools?pageSize=200" 2>/dev/null || echo '{}')
POOL_SEL=$(printf '%s' "$POOL_LIST" | probe_fixture pools "$POOL_NAME" "$ZONE_ID" "zoneId")
POOL_STATUS=$(printf '%s' "$POOL_SEL" | cut -f1)
POOL_ID=$(printf '%s' "$POOL_SEL" | cut -f2)
POOL_FOUND_ZONE=$(printf '%s' "$POOL_SEL" | cut -f3)
POOL_HAS_V6=""
if [ "$POOL_STATUS" = "match" ]; then
  POOL_HAS_V6=$(printf '%s' "$POOL_LIST" | POOL="$POOL_ID" python3 -c '
import os, sys, json
try: d = json.load(sys.stdin)
except Exception: sys.exit(0)
for p in d.get("pools", []):
    if p.get("id") == os.environ["POOL"]:
        print("v6" if p.get("v6CidrBlocks") else "nov6")
        break
')
fi

if [ "$POOL_STATUS" = "mismatch" ]; then
  # OUR name, FOREIGN zone: a pool this very script planted before the zone-dedication.
  # It is not inert — it holds the GLOBAL `is_default for (zone, kind)` slot of a zone
  # another suite makes assertions about (vpc ADR-CR-EXT-V6-FAMILY-FALLTHROUGH asserts
  # zone a has no v6-capable default). Adopting it (the old name-only probe) left this
  # suite's dedicated zone without any pool AND kept the foreign zone broken. Reclaim it.
  log "3.5/6 RECLAIM: $POOL_NAME ($POOL_ID) lives in zone '$POOL_FOUND_ZONE', this seed owns zone '$ZONE_ID'"
  log "    (not adopting it — that is what silently defeated NLB_ZONE_ID and kept zone '$POOL_FOUND_ZONE' broken)"
  # (a) drop the default flag FIRST — this alone releases the foreign zone's global slot,
  #     and unlike the delete it can never be refused by allocated leases.
  reclaim_err=$(api_err "$(curl_internal PATCH "/vpc/v1/addressPools/$POOL_ID" \
       '{"updateMask":"isDefault","isDefault":false}' 2>/dev/null || true)")
  if [ -z "$reclaim_err" ]; then
    log "    cleared is_default on $POOL_ID → zone '$POOL_FOUND_ZONE' no longer resolves it via GetDefaultForZone"
  else
    log "    WARNING: could not clear is_default on $POOL_ID ($reclaim_err) — zone '$POOL_FOUND_ZONE' still resolves this pool"
  fi
  # (b) release OUR OWN addresses in that zone (they, and only they, are ours to free);
  #     an AddressPool with allocated IPs is Delete-refused (FailedPrecondition).
  STALE_ADDRS=$(curl_json GET "/vpc/v1/addresses?projectId=$PROJECT_ID&pageSize=200" 2>/dev/null \
    | ZONE="$POOL_FOUND_ZONE" python3 -c '
import os, sys, json
try: d = json.load(sys.stdin)
except Exception: sys.exit(0)
zone = os.environ["ZONE"]
for a in d.get("addresses", []) or []:
    if not (a.get("name") or "").startswith("kac-nlb-seed-ext-addr"):
        continue
    z = ((a.get("externalIpv4Address") or {}).get("zoneId")
         or (a.get("externalIpv6Address") or {}).get("zoneId") or "")
    if z == zone:
        print(a.get("id", ""))
' || true)
  for sa in $STALE_ADDRS; do
    [ -n "$sa" ] || continue
    op=$(curl_json DELETE "/vpc/v1/addresses/$sa" 2>/dev/null || true)
    reclaim_err=$(api_err "$op")
    if [ -n "$reclaim_err" ]; then
      log "    could not release our stale Address $sa: $reclaim_err"
      continue
    fi
    op_id=$(printf '%s' "$op" | extract "id")
    if [ -n "$op_id" ] && wait_op "$op_id" >/dev/null 2>&1; then
      log "    released our stale Address $sa (zone $POOL_FOUND_ZONE)"
    else
      log "    stale Address $sa delete did not confirm (op $op_id) — pool delete below may be refused"
    fi
  done
  # (c) delete the pool. Best-effort BY DESIGN: leases held by OTHER suites' resources
  #     (nlb-lb-*-v4 VIPs from earlier runs) legitimately block it and are not ours to
  #     reap. The un-default in (a) already removed the cross-suite harm, and the new
  #     pool's zone-derived CIDR does not overlap this one, so a surviving stale pool
  #     cannot block the fresh Create either.
  reclaim_err=$(api_err "$(curl_internal DELETE "/vpc/v1/addressPools/$POOL_ID" 2>/dev/null || true)")
  if [ -z "$reclaim_err" ]; then
    log "    deleted stale pool $POOL_ID"
  else
    log "    NOTE: stale pool $POOL_ID NOT deleted — $reclaim_err"
    log "          (it still holds leases that are not ours to reap — typically nlb-lb-*-v4 VIPs from"
    log "          earlier runs). Left in place but NO LONGER DEFAULT for zone '$POOL_FOUND_ZONE', which"
    log "          is the part that broke the other suite; the new pool's zone-derived CIDR does not"
    log "          overlap it, so the fresh Create below is unaffected. Drop it once those leases are gone."
  fi
  POOL_ID=""      # fall through to the create-in-$ZONE_ID branch below
  POOL_STATUS="none"
fi

if [ -z "$POOL_ID" ]; then
  log "3.5/6 creating external AddressPool $POOL_NAME (EXTERNAL_PUBLIC, zone=$ZONE_ID, v4=$POOL_V4 v6=$POOL_V6)"
  # ZONE-DERIVED blocks. `address_pool_cidrs` EXCLUDE is (kind, block &&) — GLOBAL per
  # kind, i.e. blind to name AND zone — so a fixed block makes the pool of zone X block
  # the pool of zone Y forever (that is precisely the state a stale pool leaves behind).
  # 100.102.<zone-octet>.0/24 continues the per-suite block convention (vpc internal-pool
  # 100.100/16, vpc address 100.101/16) and is disjoint from both, and from the legacy
  # 198.51.100.0/24 a pre-dedication pool may still be sitting on. Capacity is unchanged
  # (a /24 = 254 leases), so the nlb suite's `--jobs 1` pool-contention rule still holds.
  # The v6 half lives in the SAME pool because an EXTERNAL LB with `v6Source:{public:{}}`
  # resolves the very same GetDefaultForZone(zone, EXTERNAL_PUBLIC) pool and then asks it
  # for a v6 block; a v4-only pool answers `address pool %s has no v6_cidr_blocks`, which
  # the nlb use-case collapses into the capacity-opaque "could not allocate load balancer
  # address" — XRES-E2E-EXTERNAL-IPV6-VIP was VACUOUSLY green that way (CI 30135586348:
  # 0/1 v6 allocations vs 36/39 v4). 2001:db8::/32 = RFC 3849 documentation prefix.
  pbody='{"name":"'"$POOL_NAME"'","description":"KAC-NLB seed external VIP pool","kind":"EXTERNAL_PUBLIC","zoneId":"'"$ZONE_ID"'","v4CidrBlocks":["'"$POOL_V4"'"],"v6CidrBlocks":["'"$POOL_V6"'"]}'
  POOL_ID=$(curl_internal POST "/vpc/v1/addressPools" "$pbody" | extract "id" || true)
  if [ -z "$POOL_ID" ]; then
    # Create returned no id. The most common cause on a re-run / shared vpc DB is the
    # address_pool_cidrs EXCLUDE (kind, block &&) — keyed on (kind, block) GLOBALLY,
    # ignoring name and zone — already holding one of our blocks for EXTERNAL_PUBLIC
    # from a prior seed run or the vpc newman suite (which seeds the same CIDR).
    # Idempotency-by-name (above) can't detect that pool, so fall back to REUSING one.
    #
    # Reuse is FAMILY- AND PURPOSE-CHECKED, never "any pool in the zone": a pool that
    # is EXTERNAL_PUBLIC and in $ZONE_ID can still be v4-only (or v6-only — the vpc
    # suite seeds such pools). Silently adopting it makes GetDefaultForZone resolve to
    # a pool that cannot serve the requested family, and the allocation fails with the
    # same capacity-opaque text as a genuinely exhausted pool — the failure then looks
    # like a product bug instead of a seeding gap. So: require BOTH families, prefer
    # the richer candidate, and if none qualifies say so loudly.
    POOL_SELECTION=$(curl_internal GET "/vpc/v1/addressPools?pageSize=200" 2>/dev/null | ZONE="$ZONE_ID" python3 -c '
import os, sys, json
# stdout: "<status>\t<pool-id>\t<detail>"; status ∈ {ok, partial, none, unreadable}
try:
    d = json.load(sys.stdin)
except Exception:
    print("unreadable\t\t"); sys.exit(0)
zone = os.environ.get("ZONE", "")
cands = [p for p in d.get("pools", [])
         if p.get("kind") == "EXTERNAL_PUBLIC" and p.get("zoneId") == zone]
if not cands:
    print("none\t\tno EXTERNAL_PUBLIC pool in zone " + zone); sys.exit(0)
def fams(p):
    return (bool(p.get("v4CidrBlocks")), bool(p.get("v6CidrBlocks")))
both = [p for p in cands if all(fams(p))]
if both:
    print("ok\t%s\tv4+v6" % both[0].get("id", "")); sys.exit(0)
# Nothing carries both families — report what IS there so the operator can see the gap.
desc = ", ".join("%s(%s)" % (p.get("id", "?"),
                             "v4" if fams(p)[0] else ("v6" if fams(p)[1] else "empty"))
                 for p in cands)
v4 = [p for p in cands if fams(p)[0]]
if v4:
    print("partial\t%s\t%s" % (v4[0].get("id", ""), desc)); sys.exit(0)
print("none\t\t%s" % desc)
' || printf 'unreadable\t\t')
    POOL_STATUS=$(printf '%s' "$POOL_SELECTION" | cut -f1)
    POOL_ID=$(printf '%s' "$POOL_SELECTION" | cut -f2)
    POOL_DETAIL=$(printf '%s' "$POOL_SELECTION" | cut -f3)
    case "$POOL_STATUS" in
      ok)
        log "3.5/6 AddressPool.Create conflicted (CIDR overlap?); reusing EXTERNAL_PUBLIC pool $POOL_ID in zone $ZONE_ID ($POOL_DETAIL)"
        ;;
      partial)
        # v4 works, v6 does not. Do NOT pretend the seed is complete: the external-v6
        # lane will fail, and it must be attributable to this line, not to nlb.
        log "3.5/6 AddressPool.Create conflicted; reusing v4-only EXTERNAL_PUBLIC pool $POOL_ID in zone $ZONE_ID"
        log "    WARNING: no EXTERNAL_PUBLIC pool in zone $ZONE_ID carries v6 blocks (candidates: $POOL_DETAIL)"
        log "    → EXTERNAL LoadBalancers with v6Source:{public:{}} CANNOT allocate; the v6 e2e lane will fail."
        log "    Fix the stand (drop the conflicting pool or add a v6 block to $POOL_ID), do not whitelist the case."
        ;;
      unreadable)
        log "    AddressPool list unreadable at $INTERNAL_BASE_URL (internal mux unreachable, or insufficient admin tier) — external VIP allocation may fail"
        POOL_ID=""
        ;;
      *)
        # Reachable, listed, and nothing usable: this is a seeding failure, not a
        # degraded stand. Fail loudly rather than leaving every EXTERNAL suite to red
        # with an opaque allocation error 20 minutes later.
        log "FATAL: AddressPool.Create failed and no usable EXTERNAL_PUBLIC pool exists in zone $ZONE_ID ($POOL_DETAIL)."
        log "       Every EXTERNAL nlb auto-VIP resolves GetDefaultForZone($ZONE_ID, EXTERNAL_PUBLIC); without it the whole external lane fails"
        log "       with the capacity-opaque 'could not allocate load balancer address'. Clean the conflicting pool and re-seed."
        exit 1
        ;;
    esac
  fi
  if [ -n "$POOL_ID" ]; then
    # Allocation picks the pool ONLY when is_default=true for (zone, kind); the
    # Create RPC has no isDefault field, so flip it via Update (update_mask=isDefault).
    # Idempotent: PATCH on an already-default pool is a no-op.
    # Graded on the RESPONSE BODY (curl exits 0 on 4xx — see api_err): a refused PATCH
    # here means the pool exists but nothing resolves it, i.e. every EXTERNAL VIP in this
    # zone fails later with a capacity-opaque error. That must be visible now.
    pool_err=$(api_err "$(curl_internal PATCH "/vpc/v1/addressPools/$POOL_ID" \
      '{"updateMask":"isDefault","isDefault":true}' 2>/dev/null || true)")
    [ -z "$pool_err" ] || \
      log "    WARNING: could not set is_default on $POOL_ID ($pool_err) — another default pool may already hold the ($ZONE_ID, EXTERNAL_PUBLIC) slot; external VIP allocation in this zone will fail"
  else
    log "    AddressPool.Create did not return an id and no EXTERNAL_PUBLIC pool exists in zone $ZONE_ID (internal mux unreachable at $INTERNAL_BASE_URL, or insufficient admin tier) — external VIP allocation may fail; whitelist non-T31 nlb external-create cases if so"
  fi
else
  log "3.5/6 reusing existing external AddressPool $POOL_ID (zone $ZONE_ID — placement verified, not name-only)"
  # Re-assert the default flag: a reused pool that lost it (reclaimed by an earlier run
  # targeting another zone, or cleared by hand) resolves nothing via GetDefaultForZone.
  pool_err=$(api_err "$(curl_internal PATCH "/vpc/v1/addressPools/$POOL_ID" \
    '{"updateMask":"isDefault","isDefault":true}' 2>/dev/null || true)")
  [ -z "$pool_err" ] || \
    log "    WARNING: could not (re-)set is_default on $POOL_ID ($pool_err) — another default pool may already hold the ($ZONE_ID, EXTERNAL_PUBLIC) slot"
  if [ "$POOL_HAS_V6" != "v6" ]; then
    # A pool seeded by an older revision of this script is v4-only, so
    # `v6Source:{public:{}}` cannot allocate from it. Blocks are immutable via
    # Update by design — top up through the dedicated :addCidrBlocks action.
    log "    pool has no v6 blocks (pre-v6 seed) — adding $POOL_V6 via :addCidrBlocks"
    pool_err=$(api_err "$(curl_internal POST "/vpc/v1/addressPools/$POOL_ID:addCidrBlocks" \
        '{"v6CidrBlocks":["'"$POOL_V6"'"]}' 2>/dev/null || true)")
    [ -z "$pool_err" ] || \
      log "    WARNING: could not add the v6 block to $POOL_ID ($pool_err) — EXTERNAL v6 auto-VIP will fail (v6 e2e lane)."
  fi
fi

# ─── 3.6) Ensure the ZONE-INDEPENDENT (anycast) external AddressPool ---------
#
# This is where every EXTERNAL NetworkLoadBalancer VIP comes from. `placement=
# EXTERNAL_REGIONAL` is the ONLY external placement, so an external LB is ALWAYS
# REGIONAL/anycast, and a REGIONAL resource is zone-independent BY CONSTRUCTION
# (data-integrity.md §Placement-coherence). nlb therefore allocates its public VIP
# with NO zone, and vpc resolves the zone-independent pool (`zone_id IS NULL`).
#
# It used to derive a zone (`sort(zones)[0]`, i.e. always ru-central1-a) and consume
# THAT zone's pool — an "anycast" VIP pinned to one zone's prefix and failure domain,
# which additionally only worked by accident whenever zone a happened to own a pool of
# the requested family. It never did for IPv6: zero v6 VIPs were ever allocated.
#
# BLOCK CONVENTION (per-suite, global `address_pool_cidrs EXCLUDE (kind, block &&)`):
#   100.100/16 vpc internal-pool · 100.101/16 vpc address · 100.102/16 nlb ZONAL pool
#   → 100.103/16 nlb ZONE-INDEPENDENT pool. The v6 block sits OUTSIDE the zonal band
#   `2001:db8:e2e:{01..fe}::/64` (that band is indexed by the per-zone octet) so it can
#   never collide with a zonal seed.
#
# NOT zone-derived, and deliberately so: `is_default for (zone_id IS NULL, kind)` is a
# CLUSTER-WIDE singleton (partial UNIQUE on `(COALESCE(zone_id,''), kind)`), one per
# cluster. It is safe to own precisely because the zonal and anycast lanes are disjoint:
# a zone-pinned request is served from its OWN zone or not at all, so this pool cannot
# leak into the zones whose (deliberate) absence of a family other suites assert on —
# vpc ADR-CR-EXT-V6-FAMILY-FALLTHROUGH (zone a has no v6), ADR-CR-EXT-FALLTHROUGH-V4
# and IPL-RESOLVE-NETWORK-DEFAULT-FAMILY-SKIP (zone b has no v4) all stay honest.
# АВТОР ЭТОЙ СТРОКИ — ПОДЪЁМ СТЕНДА, а не этот скрипт (с 2026-08-17).
# `make dev-up` сеет её SQL-ом (deploy/scripts/vpc-address-pool-baseline.sql,
# цель `seed-vpc-pools`): полоса аникаста нужна не набору nlb, а СТЕНДУ — без неё
# ни один внешний балансировщик не создаётся ни в консоли, ни у разработчика, а
# слот `is_default` для (zone_id IS NULL, kind) — кластерный синглтон, и второй
# его автор разошёлся бы с первым молча.
#
# Здесь поэтому штатно срабатывает ветка «reusing … placement verified» ниже — та
# же, которую скрипт берёт на любом повторном прогоне. Ветка создания остаётся
# для стендов, поднятых без посева (standalone `make seed-nlb`, боевой прогон).
#
# ИМЯ ИСТОРИЧЕСКОЕ: оно называет прежнего автора. Переименование потребует
# согласованной правки двух объявлений (здесь и в SQL) и заведено отдельным
# предметом; согласие объявлений держит гейт
# internal/repohygiene TestStandAnycastPoolBaselineMatchesTheNlbSeeder.
ANY_POOL_NAME="kac-nlb-seed-anycast-pool"
ANY_POOL_V4="100.103.0.0/22"
ANY_POOL_V6="2001:db8:e2e:1ac::/64"

ANY_LIST=$(curl_internal GET "/vpc/v1/addressPools?pageSize=200" 2>/dev/null || echo '{}')
ANY_SEL=$(printf '%s' "$ANY_LIST" | probe_fixture pools "$ANY_POOL_NAME" "" "zoneId")
ANY_STATUS=$(printf '%s' "$ANY_SEL" | cut -f1)
ANY_POOL_ID=$(printf '%s' "$ANY_SEL" | cut -f2)
ANY_FOUND_ZONE=$(printf '%s' "$ANY_SEL" | cut -f3)
ANY_HAS_V6=""
if [ "$ANY_STATUS" = "match" ]; then
  ANY_HAS_V6=$(printf '%s' "$ANY_LIST" | POOL="$ANY_POOL_ID" python3 -c '
import sys, json, os
try: d = json.load(sys.stdin)
except Exception: sys.exit(0)
for p in d.get("pools") or []:
    if p.get("id") == os.environ["POOL"]:
        print("v6" if (p.get("v6CidrBlocks") or []) else "")
        break
' 2>/dev/null || echo "")
fi

if [ "$ANY_STATUS" = "mismatch" ]; then
  # Our name, but the pool carries a ZONE — it is a zonal pool masquerading as the
  # anycast one (hand-edited, or an older revision of this script). Adopting it would
  # re-pin every EXTERNAL VIP to one zone, i.e. reintroduce the exact defect. Refuse.
  log "FATAL: $ANY_POOL_NAME ($ANY_POOL_ID) is pinned to zone '$ANY_FOUND_ZONE'; the anycast pool must be zone-independent."
  log "       Delete or rename it, then re-seed. Do NOT adopt it: an EXTERNAL LB VIP taken from a zonal pool is"
  log "       pinned to that zone's failure domain and silently fails for families that zone does not serve."
  exit 1
fi

if [ -z "$ANY_POOL_ID" ]; then
  log "3.6/6 creating zone-independent AddressPool $ANY_POOL_NAME (EXTERNAL_PUBLIC, no zone, v4=$ANY_POOL_V4 v6=$ANY_POOL_V6)"
  anybody='{"name":"'"$ANY_POOL_NAME"'","description":"KAC-NLB seed anycast (zone-independent) external VIP pool","kind":"EXTERNAL_PUBLIC","v4CidrBlocks":["'"$ANY_POOL_V4"'"],"v6CidrBlocks":["'"$ANY_POOL_V6"'"]}'
  ANY_CREATE=$(curl_internal POST "/vpc/v1/addressPools" "$anybody" 2>/dev/null || true)
  ANY_POOL_ID=$(printf '%s' "$ANY_CREATE" | extract "id" || true)
  if [ -z "$ANY_POOL_ID" ]; then
    log "FATAL: could not create the zone-independent AddressPool ($(api_err "$ANY_CREATE"))."
    log "       Without it EVERY external NetworkLoadBalancer VIP fails with the capacity-opaque"
    log "       'could not allocate load balancer address' (the anycast lane has no pool to resolve)."
    exit 1
  fi
else
  log "3.6/6 reusing zone-independent AddressPool $ANY_POOL_ID (no zone — placement verified, not name-only)"
  if [ "$ANY_HAS_V6" != "v6" ]; then
    log "    pool has no v6 blocks — adding $ANY_POOL_V6 via :addCidrBlocks"
    any_err=$(api_err "$(curl_internal POST "/vpc/v1/addressPools/$ANY_POOL_ID:addCidrBlocks" \
        '{"v6CidrBlocks":["'"$ANY_POOL_V6"'"]}' 2>/dev/null || true)")
    [ -z "$any_err" ] || \
      log "    WARNING: could not add the v6 block to $ANY_POOL_ID ($any_err) — EXTERNAL v6 auto-VIP will fail."
  fi
fi

# Resolution picks the pool ONLY when is_default=true; Create has no isDefault field.
# Graded on the RESPONSE BODY (curl exits 0 on 4xx): a refused PATCH means another pool
# already holds the cluster-wide (NULL-zone, EXTERNAL_PUBLIC) slot, and every EXTERNAL
# VIP would then fail capacity-opaque. That must be visible now, not at case time.
any_err=$(api_err "$(curl_internal PATCH "/vpc/v1/addressPools/$ANY_POOL_ID" \
  '{"updateMask":"isDefault","isDefault":true}' 2>/dev/null || true)")
[ -z "$any_err" ] || \
  log "    WARNING: could not set is_default on $ANY_POOL_ID ($any_err) — another pool holds the cluster-wide (zone-independent, EXTERNAL_PUBLIC) slot; EXTERNAL LB VIP allocation will fail"

# ─── 4) Ensure External Addresses (BYO VIP: v4 + v6) ------------------------
# Both are placement-scoped (ExternalIpv{4,6}AddressSpec carries zoneId) and both are
# probed by (name AND zone): an address adopted from another zone would be allocated out
# of THAT zone's pool, so a "BYO VIP" fixture would silently point at a lease the suite's
# own zone cannot serve.
ADDR_LIST=$(curl_json GET "/vpc/v1/addresses?projectId=$PROJECT_ID&pageSize=200")
ADDR_ZONE_PATH="externalIpv4Address.zoneId|externalIpv6Address.zoneId"

ADDR_SEL=$(printf '%s' "$ADDR_LIST" | probe_fixture addresses "$ADDR_NAME" "$ZONE_ID" "$ADDR_ZONE_PATH")
ADDR_STATUS=$(printf '%s' "$ADDR_SEL" | cut -f1)
EXT_ADDR_ID=$(printf '%s' "$ADDR_SEL" | cut -f2)
case "$ADDR_STATUS" in
  mismatch) refuse_mismatch "Address" "$ADDR_NAME" "$EXT_ADDR_ID" "$(printf '%s' "$ADDR_SEL" | cut -f3)" ;;
  match)    log "4/6 reusing existing Address $EXT_ADDR_ID (zone $ZONE_ID)" ;;
  *)
    EXT_ADDR_ID=""
    log "4/6 creating external Address $ADDR_NAME (ZONAL v4, zone=$ZONE_ID)"
    # External Address IPAM is ZONE-scoped: the request field is
    # `externalIpv4AddressSpec` (not `externalIpv4Address`, which is a field on the
    # Address *resource*), and ExternalIpv4AddressSpec carries only zoneId — there is
    # NO regionId on it (proto address_service.proto). The resolver keys the default
    # pool by zone (address_pool.go GetDefaultForZone($zone, EXTERNAL_PUBLIC)), so a
    # zoneId that matches the ZONAL pool seeded in 3.5 is required; a region-scoped /
    # zone-less spec would only match a GLOBAL (zone_id IS NULL) pool and 404 here.
    # This mirrors the passing newman body ADR-CR-CRUD-EXT (address.py).
    body='{"projectId":"'"$PROJECT_ID"'","name":"'"$ADDR_NAME"'","externalIpv4AddressSpec":{"zoneId":"'"$ZONE_ID"'"}}'
    op=$(curl_json POST "/vpc/v1/addresses" "$body")
    op_id=$(printf '%s' "$op" | extract "id")
    EXT_ADDR_ID=$(wait_op_field "$op_id" "addressId" || true)
    if [ -z "$EXT_ADDR_ID" ]; then
      log "    Address.Create rejected (no AddressPool seeded in $ZONE_ID?) — leaving existingExternalAddressId blank"
    fi
    ;;
esac

# 4b) External IPv6 Address — `existingAddressIPv6Id`. Four nlb cases need a REAL v6
# address handle: the family/slot negatives (NLB-CR-VAL-ADDRESS-FAMILY-SLOT,
# LST-CR-VAL-BYO-IP-VERSION-MISMATCH, XRES-E2E-V4-LISTENER-V6-ADDRESS-INVALID) claim to
# verify "v4 slot referencing an IPv6 address → Illegal argument addressId", and
# NLB-CR-CRUD-DUALSTACK-MIXED links it as the v6 leg. With the id left as a committed
# placeholder those negatives were satisfied by "unknown address" instead of the family
# mismatch they name, and the two guarded cases took their tolerant else-branch. Allocated
# from the same seed pool (its v6 half), so it is coherent with the suite's zone.
ADDR6_SEL=$(printf '%s' "$ADDR_LIST" | probe_fixture addresses "$ADDR6_NAME" "$ZONE_ID" "$ADDR_ZONE_PATH")
ADDR6_STATUS=$(printf '%s' "$ADDR6_SEL" | cut -f1)
EXT_ADDR6_ID=$(printf '%s' "$ADDR6_SEL" | cut -f2)
case "$ADDR6_STATUS" in
  mismatch) refuse_mismatch "Address(v6)" "$ADDR6_NAME" "$EXT_ADDR6_ID" "$(printf '%s' "$ADDR6_SEL" | cut -f3)" ;;
  match)    log "    reusing existing IPv6 Address $EXT_ADDR6_ID (zone $ZONE_ID)" ;;
  *)
    EXT_ADDR6_ID=""
    log "    creating external Address $ADDR6_NAME (ZONAL v6, zone=$ZONE_ID)"
    body6='{"projectId":"'"$PROJECT_ID"'","name":"'"$ADDR6_NAME"'","externalIpv6AddressSpec":{"zoneId":"'"$ZONE_ID"'"}}'
    op=$(curl_json POST "/vpc/v1/addresses" "$body6")
    op_id=$(printf '%s' "$op" | extract "id")
    EXT_ADDR6_ID=$(wait_op_field "$op_id" "addressId" || true)
    if [ -z "$EXT_ADDR6_ID" ]; then
      log "    IPv6 Address.Create rejected (pool has no v6 block in $ZONE_ID?) — leaving existingAddressIPv6Id blank;"
      log "    the nlb family/slot negatives then fall back to their unseeded branch instead of testing family mismatch."
    fi
    ;;
esac

# ─── 5) Ensure MachineType (sizing channel for the seed Instance) -----------
# COMP-1 redesign: CreateInstanceRequest lost platformId/resourcesSpec/bootDiskSpec
# (RESERVED, ban #2) and gained instanceKind + machineTypeId + bootSource. The old body
# here still sent `resourcesSpec` and no instanceKind → the live gateway answered
# `400 instanceKind is required`, so existingInstanceId/existingNicId were ALWAYS blank
# (the compute suite itself was migrated; this seeder was missed — cases/list-filter.py
# documents exactly this error). machineTypeId resolves against the compute catalog
# (mt- slug OR stable name; unknown → FailedPrecondition), so a MachineType must exist.
# Catalog reads are CLUSTER-scoped (`viewer` @ cluster) and Create is Internal :8081
# system_admin (ban #6) → both go through $ADMIN_JWT, not the project grantor.
# NB: available_zones is catalog metadata; Instance.Create resolves the type by id/name
# and only rejects RETIRED — it does NOT filter by zone. We still list the seed zone so
# the catalog entry is truthful about where this fixture type is bookable.
MT_NAME="kac-nlb-seed-mt"
MT_ID=$(curl_admin GET "/compute/v1/machineTypes?pageSize=200" 2>/dev/null | MT_NAME="$MT_NAME" python3 -c '
import os, sys, json
try: d=json.load(sys.stdin)
except Exception: sys.exit(0)
want = os.environ["MT_NAME"]
for m in d.get("machineTypes", []):
    if m.get("name") == want and m.get("status") != "RETIRED":
        print(m.get("id","")); sys.exit(0)
# no fixture type — fall back to ANY non-retired catalog entry before seeding a new one
for m in d.get("machineTypes", []):
    if m.get("status") not in ("RETIRED",):
        print(m.get("id","")); sys.exit(0)
print("")
' || true)
if [ -z "$MT_ID" ]; then
  log "5/6 creating MachineType $MT_NAME (STANDARD 2 vCPU / 4096 MiB, zone=$ZONE_ID)"
  mtbody=$(cat <<EOF
{
  "name":"$MT_NAME",
  "description":"KAC-NLB seed machine type",
  "family":"STANDARD",
  "effectiveResources":{"vCpu":2,"memoryMib":4096,"gpus":0},
  "availableZones":["$ZONE_ID"],
  "status":"AVAILABLE"
}
EOF
  )
  op=$(curl_internal POST "/compute/v1/internal/machineTypes" "$mtbody")
  op_id=$(printf '%s' "$op" | extract "id")
  # Poll through the SAME channel that created it (internal mux + $ADMIN_JWT). The create
  # is an Internal admin RPC authorized on the cluster singleton; polling it as the project
  # grantor $JWT (what the default public channel does) returns a permanent existence-hiding
  # 404, so the loop never converged — 60s burned + a misleading FATAL. See wait_op.
  MT_ID=$(wait_op_field "$op_id" "machineTypeId" internal || true)
  if [ -z "$MT_ID" ]; then
    # UNIQUE(name) re-run, or the internal mux is unreachable — re-probe by name so an
    # AlreadyExists does NOT degrade into "no machine type" and blank the Instance.
    MT_ID=$(curl_admin GET "/compute/v1/machineTypes?pageSize=200" 2>/dev/null | MT_NAME="$MT_NAME" python3 -c '
import os, sys, json
try: d=json.load(sys.stdin)
except Exception: sys.exit(0)
want = os.environ["MT_NAME"]
for m in d.get("machineTypes", []):
    if m.get("name") == want:
        print(m.get("id","")); sys.exit(0)
print("")
' || true)
  fi
else
  log "5/6 reusing MachineType $MT_ID"
fi
[ -n "$MT_ID" ] || log "    WARNING: no MachineType available — Instance.Create will fail 'machine type  not found'"

# ─── 6) Ensure Compute Instance + its NIC ----------------------------------
# Zone-aware, like everything else: an Instance is ZONAL, and its NIC-spec subnet must be
# in the same zone (placement-coherence). Reusing a same-named Instance from another zone
# is what made the fixture set self-contradictory (instance in zone e, NIC/subnet in a).
INST_LIST=$(curl_json GET "/compute/v1/instances?projectId=$PROJECT_ID&pageSize=200")
INST_SEL=$(printf '%s' "$INST_LIST" | probe_fixture instances "$INST_NAME" "$ZONE_ID" "zoneId")
INST_STATUS=$(printf '%s' "$INST_SEL" | cut -f1)
INSTANCE_ID=$(printf '%s' "$INST_SEL" | cut -f2)
if [ "$INST_STATUS" = "mismatch" ]; then
  refuse_mismatch "Instance" "$INST_NAME" "$INSTANCE_ID" "$(printf '%s' "$INST_SEL" | cut -f3)"
fi
[ "$INST_STATUS" = "match" ] || INSTANCE_ID=""
if [ -z "$INSTANCE_ID" ] && [ -n "$MT_ID" ] && [ -n "$SUBNET_ID" ]; then
  log "6/6 creating Instance $INST_NAME (COMP-1 shape: instanceKind/machineTypeId/bootSource, zone=$ZONE_ID)"
  body=$(cat <<EOF
{
  "projectId":"$PROJECT_ID",
  "zoneId":"$ZONE_ID",
  "name":"$INST_NAME",
  "instanceKind":"VM",
  "machineTypeId":"$MT_ID",
  "bootSource":{"type":"storage.image","id":"img-9k2m4x7q1n8p:22.04-lts"},
  "vmSpec":{"userData":"#cloud-config\n{}","metadataOptions":{"metadataEndpoint":"ENABLED","metadataTokenRequired":true}},
  "networkInterfaceSpecs":[{"subnetId":"$SUBNET_ID","primaryV4AddressSpec":{}}],
  "sshPublicKeys":["ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIexampledeadbeefkey nlb@seed"]
}
EOF
  )
  op=$(curl_json POST "/compute/v1/instances" "$body")
  op_id=$(printf '%s' "$op" | extract "id")
  INSTANCE_ID=$(wait_op_field "$op_id" "instanceId" || true)
  if [ -z "$INSTANCE_ID" ]; then
    log "    Instance.Create rejected — leaving existingInstanceId blank (see the operation error above)"
  fi
elif [ -n "$INSTANCE_ID" ]; then
  log "6/6 reusing existing Instance $INSTANCE_ID (zone $ZONE_ID)"
else
  log "6/6 skipping Instance $INST_NAME — no MachineType and/or no Subnet in zone $ZONE_ID to place it on"
fi

# NIC. Instance.networkInterfaces is an OUTPUT-ONLY mirror materialised by the COMP-2
# launch saga, which is not landed yet: doCreate persists the Instance at PROVISIONING
# and does NOT create the vpc NIC, so scraping the id off the Instance yields nothing
# today. The nlb Target.nic_id cases only need a NIC that exists in the seed subnet, so
# seed a first-class vpc NetworkInterface directly (public /vpc/v1/networkInterfaces,
# `editor` @ project → the project grantor $JWT). Prefer the Instance's own NIC as soon
# as COMP-2 materialises one.
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
if [ -z "$NIC_ID" ] && [ -n "$SUBNET_ID" ]; then
  # A NIC carries no zone of its own — it INHERITS placement from its subnet. So the
  # coherence predicate here is `subnetId == the subnet we just resolved for THIS zone`,
  # not a zoneId field: a NIC named after us but hanging off another zone's subnet is
  # exactly the artifact that made existingNicId contradict existingZoneId.
  NIC_LIST=$(curl_json GET "/vpc/v1/networkInterfaces?projectId=$PROJECT_ID&pageSize=200" 2>/dev/null || echo '{}')
  NIC_SEL=$(printf '%s' "$NIC_LIST" | probe_fixture networkInterfaces "$NIC_NAME" "$SUBNET_ID" "subnetId")
  NIC_STATUS=$(printf '%s' "$NIC_SEL" | cut -f1)
  NIC_ID=$(printf '%s' "$NIC_SEL" | cut -f2)
  if [ "$NIC_STATUS" = "mismatch" ]; then
    refuse_mismatch "NetworkInterface" "$NIC_NAME" "$NIC_ID" "subnet $(printf '%s' "$NIC_SEL" | cut -f3)"
  fi
  [ "$NIC_STATUS" = "match" ] || NIC_ID=""
  if [ -z "$NIC_ID" ]; then
    log "    creating standalone vpc NIC $NIC_NAME on subnet $SUBNET_ID (Instance NIC materialisation is COMP-2)"
    nicbody='{"projectId":"'"$PROJECT_ID"'","subnetId":"'"$SUBNET_ID"'","name":"'"$NIC_NAME"'"}'
    op=$(curl_json POST "/vpc/v1/networkInterfaces" "$nicbody")
    op_id=$(printf '%s' "$op" | extract "id")
    NIC_ID=$(wait_op_field "$op_id" "networkInterfaceId" || true)
    [ -n "$NIC_ID" ] || log "    NIC.Create rejected — leaving existingNicId blank"
  else
    log "    reusing existing NIC $NIC_ID (subnet $SUBNET_ID)"
  fi
fi

# ─── Write .seeded-ids.env --------------------------------------------------
log "writing $OUT_FILE"
cat >"$OUT_FILE" <<EOF
# Auto-generated by scripts/seed-nlb-fixtures.sh — do not edit.
existingProjectId=$PROJECT_ID
existingRegionId=$REGION_ID
existingZoneId=$ZONE_ID
existingNetworkId=$NET_ID
# Объявленный ПЛАН публикуется рядом с сетью, потому что резать подсеть можно только
# внутри него. Кейс, выводящий адрес сам, попадает в план или мимо в зависимости от
# того, чей посев создал сеть, — а посевов у этого набора два, и планы у них разные.
# Помощник `carve_cidr_pre` (services/nlb/tests/newman/scripts/gen.py) режет ВНУТРИ
# опубликованного; гейт `TestOutOfCaseCarveTakesItsCidrFromThePublishedPlan` не даёт
# завести нарезку мимо него.
existingNetworkV4Plan=$NET_SUPERNET_V4
existingNetworkV6Plan=$NET_SUPERNET_V6
existingSubnetId=$SUBNET_ID
existingExternalPoolId=$POOL_ID
existingAnycastPoolId=$ANY_POOL_ID
existingExternalAddressId=$EXT_ADDR_ID
existingAddressIPv6Id=$EXT_ADDR6_ID
existingInstanceId=$INSTANCE_ID
existingNicId=$NIC_ID
EOF

# Placement self-check — the whole point of this revision. Every id written above must
# belong to $ZONE_ID; a blank id is "not seeded" (loudly logged upstream), never a
# mismatch. Printing the coherence verdict here means a set that silently drifts (the
# state this script shipped in: zone-a subnet under an existingZoneId=e env) is visible
# in the seed log itself instead of surfacing 20 minutes later as an nlb/vpc red.
log "placement self-check (all fixtures must be in $ZONE_ID):"
log "    subnet=${SUBNET_ID:-<unseeded>} address=${EXT_ADDR_ID:-<unseeded>} address6=${EXT_ADDR6_ID:-<unseeded>}"
log "    instance=${INSTANCE_ID:-<unseeded>} nic=${NIC_ID:-<unseeded>} pool=${POOL_ID:-<unseeded>}"
log "    zone-independent (anycast) pool, source of every EXTERNAL LB VIP: ${ANY_POOL_ID:-<unseeded>}"

log "done"
cat "$OUT_FILE"
