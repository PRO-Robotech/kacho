#!/usr/bin/env bash
# kacho-deploy/e2e/geography-move.sh — integration test for epic KAC-15
# (Region/Zone moved kacho-vpc -> kacho-compute). Runs against a deployed stack
# via the api-gateway REST endpoint ($BASE_URL).
#
# Covers the Given-When-Then scenarios in
# kacho-workspace/docs/specs/sub-phase-geography-to-compute-acceptance.md
# (the API-observable ones; schema/code-inspection scenarios 1/5 are checked by
# the per-repo CI builds + `grep`, see the notes at the bottom).
#
# Prereqs (must already be merged & deployed):
#   kacho-proto KAC-19, kacho-compute KAC-20, kacho-vpc KAC-21, kacho-api-gateway KAC-22,
#   kacho-deploy KAC-24 (seed.sh + compose env). Stack up; ci/seed.sh has run.
#
# Usage: BASE_URL=http://localhost:28080 ./e2e/geography-move.sh
set -uo pipefail

BASE_URL="${BASE_URL:-http://localhost:28080}"
PASS=0 FAIL=0
ok()   { echo "  PASS: $1"; PASS=$((PASS+1)); }
bad()  { echo "  FAIL: $1"; FAIL=$((FAIL+1)); }
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
body() { curl -s "$@"; }
jq_has() { python3 -c "import sys,json
try:
  d=json.load(sys.stdin)
  print('1' if $1 else '')
except Exception: print('')"; }

echo "== KAC-15 geography-move e2e against $BASE_URL =="

# --- Scenario 2: public read-only Region/Zone on /compute/v1; /vpc/v1 gone ---
echo "[S2] /compute/v1/regions, /compute/v1/zones present; /vpc/v1/{regions,zones} -> 404"
c=$(code "$BASE_URL/compute/v1/regions");           [[ "$c" == 200 ]] && ok "GET /compute/v1/regions = 200" || bad "GET /compute/v1/regions = $c (want 200)"
c=$(code "$BASE_URL/compute/v1/zones");              [[ "$c" == 200 ]] && ok "GET /compute/v1/zones = 200"   || bad "GET /compute/v1/zones = $c (want 200)"
c=$(code "$BASE_URL/compute/v1/zones/ru-central1-a");[[ "$c" == 200 ]] && ok "GET /compute/v1/zones/ru-central1-a = 200" || bad "= $c (want 200)"
has=$(body "$BASE_URL/compute/v1/zones" | jq_has "any(z.get('id')=='ru-central1-a' for z in d.get('zones') or [])")
[[ -n "$has" ]] && ok "zones list contains ru-central1-a" || bad "zones list missing ru-central1-a"
c=$(code "$BASE_URL/vpc/v1/regions"); [[ "$c" == 404 ]] && ok "GET /vpc/v1/regions = 404" || bad "GET /vpc/v1/regions = $c (want 404)"
c=$(code "$BASE_URL/vpc/v1/zones");   [[ "$c" == 404 ]] && ok "GET /vpc/v1/zones = 404"   || bad "GET /vpc/v1/zones = $c (want 404)"

# --- Scenario 3: admin CRUD Region/Zone on /compute/v1; Region.Delete blocked w/ zones ---
echo "[S3] admin CRUD on /compute/v1/{regions,zones}; Region.Delete RESTRICT"
RT="kac15-test-$RANDOM"
c=$(code -X POST "$BASE_URL/compute/v1/regions" -H 'Content-Type: application/json' -d "{\"id\":\"$RT\",\"name\":\"KAC-15 test region\"}")
[[ "$c" == 200 ]] && ok "POST region $RT = 200" || bad "POST region = $c (want 200)"
c=$(code -X POST "$BASE_URL/compute/v1/zones" -H 'Content-Type: application/json' -d "{\"id\":\"$RT-a\",\"regionId\":\"$RT\",\"name\":\"$RT-a\",\"status\":\"UP\"}")
[[ "$c" == 200 ]] && ok "POST zone $RT-a = 200" || bad "POST zone = $c (want 200)"
c=$(code -X DELETE "$BASE_URL/compute/v1/regions/$RT")
[[ "$c" == 409 || "$c" == 400 ]] && ok "DELETE region with zones -> $c (FailedPrecondition)" || bad "DELETE region with zones = $c (want 409/400)"
c=$(code -X DELETE "$BASE_URL/compute/v1/zones/$RT-a"); [[ "$c" == 200 ]] && ok "DELETE zone = 200" || bad "DELETE zone = $c"
c=$(code -X DELETE "$BASE_URL/compute/v1/regions/$RT"); [[ "$c" == 200 ]] && ok "DELETE empty region = 200" || bad "DELETE empty region = $c"

# --- Scenario 4: kacho-vpc validates zone_id via kacho-compute ---
echo "[S4] vpc Subnet.Create validates zone via compute"
# need a network in the default folder; reuse the seed fixture if present
FOLDER_ID=$(body "$BASE_URL/resource-manager/v1/folders" | python3 -c 'import sys,json;
try: print((json.load(sys.stdin).get("folders") or [{}])[0].get("id",""))
except Exception: print("")')
NET_ID=""
if [[ -n "$FOLDER_ID" ]]; then
  NET_ID=$(body "$BASE_URL/vpc/v1/networks?folderId=$FOLDER_ID" | python3 -c 'import sys,json;
try: print((json.load(sys.stdin).get("networks") or [{}])[0].get("id",""))
except Exception: print("")')
fi
if [[ -n "$NET_ID" ]]; then
  c=$(code -X POST "$BASE_URL/vpc/v1/subnets" -H 'Content-Type: application/json' \
        -d "{\"folderId\":\"$FOLDER_ID\",\"name\":\"kac15-subn-$RANDOM\",\"networkId\":\"$NET_ID\",\"zoneId\":\"no-such-zone-xyz\",\"v4CidrBlocks\":[\"10.231.0.0/24\"]}")
  [[ "$c" == 400 ]] && ok "Subnet.Create with bogus zone -> 400 (vpc asked compute, got NotFound)" || bad "bogus-zone Subnet.Create = $c (want 400)"
  c=$(code -X POST "$BASE_URL/vpc/v1/subnets" -H 'Content-Type: application/json' \
        -d "{\"folderId\":\"$FOLDER_ID\",\"name\":\"kac15-subn-ok-$RANDOM\",\"networkId\":\"$NET_ID\",\"zoneId\":\"ru-central1-a\",\"v4CidrBlocks\":[\"10.232.0.0/24\"]}")
  [[ "$c" == 200 ]] && ok "Subnet.Create with valid zone -> 200 (Operation)" || bad "valid-zone Subnet.Create = $c (want 200)"
else
  echo "  SKIP S4: no VPC network in the default folder (run ci/seed.sh first)"
fi

echo
echo "== result: PASS=$PASS FAIL=$FAIL =="
echo
echo "Not covered here (require DB/code inspection — done by per-repo CI + manual check):"
echo "  S1: no regions/zones tables in schema kacho_vpc; they exist in kacho_compute (FK zones->regions)."
echo "  S5: kacho-compute has no compute->vpc zone proxy (grep: no VPCClient.GetZone/ListZones, no skipPeer in ZoneService)."
echo "  S7: delete a zone in compute that a vpc subnet uses -> Subnet.Get still 200 (dangling-ref grace)."
echo "  S8: stop kacho-compute -> Subnet.Create -> Unavailable; Subnet.Get of existing -> 200."
echo "  S10: workspace/vpc/compute CLAUDE.md carry the cross-domain-refs regulation."
[[ "$FAIL" == 0 ]] || exit 1
