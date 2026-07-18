#!/usr/bin/env bash

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# tests/newman/scripts/run.sh — newman runner for kacho-nlb regression suites.
#
# Usage:
#   ./scripts/run.sh                          # all collections, summary report
#   ./scripts/run.sh --service load-balancer  # single collection
#   ./scripts/run.sh --service listener --bail
#   ./scripts/run.sh --delay 100              # inter-request delay (ms)
#   ./scripts/run.sh --jobs 2                 # max parallel collections (default 1)
#
# --jobs default is 1 (serial collections) — the nlb load-balancer / cross-resource /
# listener collections all draw EXTERNAL auto-VIPs from the SINGLE seeded external
# AddressPool (kac-nlb-seed-ext-pool, 198.51.100.0/24 = 254 addrs). Under >1 concurrent
# collection that pool is transiently exhausted mid-run (`could not allocate load balancer
# address`) even though the VIP is correctly recycled on LB delete (release VIP =
# ClearReference→FreeIP + free_ip_runner self-heal, delete.go) — it is bursty concurrent
# HOLD, not a pool leak. Serial collections keep the peak concurrent VIP hold tiny (each
# EXTERNAL case creates then deletes its LB before the next) → no exhaustion, no masking.
# The external pool's (kind, block) EXCLUDE is GLOBAL and shared with the vpc seed
# (same 198.51.100.0/24), so simply enlarging THIS seed's CIDR would collide with vpc's
# /24 and fall back to reusing the /24 — serialization is the reliable, self-contained fix.
#   ./scripts/run.sh --env environments/kind-stand.postman_environment.json
#
# Each collection is isolated via {{runId}}-suffixed resource names within a
# shared pre-allocated existingProjectId, so parallel execution is safe.
#
# Outputs:
#   out/<service>.json — newman JSON reporter (for aggregation)
#   out/<service>.cli  — newman cli output
#   out/summary.txt    — overall summary

set -euo pipefail
cd "$(dirname "$0")/.."

SERVICE=""
BAIL=""
DELAY="15"
JOBS="1"   # serial collections: shared external AddressPool contention (see header)
ENV="environments/local.postman_environment.json"
EXTRA=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --service) SERVICE="$2"; shift 2 ;;
    --bail)    BAIL="--bail"; shift ;;
    --delay)   DELAY="$2"; shift 2 ;;
    --jobs)    JOBS="$2"; shift 2 ;;
    --env)     ENV="$2"; shift 2 ;;
    *)         EXTRA+=("$1"); shift ;;
  esac
done

[[ -f "$ENV" ]] || { echo "missing env: $ENV"; exit 1; }

run_one() {
  local svc="$1"
  local col="collections/${svc}.postman_collection.json"
  if [[ ! -f "$col" ]]; then
    echo "[skip] $svc — no collection"
    return 0
  fi
  echo "===== ${svc} ====="
  newman run "$col" \
    -e "$ENV" \
    --delay-request "$DELAY" \
    $BAIL \
    --reporters cli,json \
    --reporter-json-export "out/${svc}.json" \
    "${EXTRA[@]}" 2>&1 | tee "out/${svc}.cli" || true
}

mkdir -p out

if [[ -n "$SERVICE" ]]; then
  run_one "$SERVICE"
else
  for svc in load-balancer listener target-group targets operation authz-deny cross-resource list-filter placement-coherence; do
    while [[ "$(jobs -rp | wc -l)" -ge "$JOBS" ]]; do wait -n; done
    run_one "$svc" &
  done
  wait
fi

echo
echo "===== Summary ====="
{
  printf "%-25s %10s %10s %10s\n" "SERVICE" "ASSERT" "FAILED" "REQUESTS"
  for f in out/*.json; do
    [[ -f "$f" ]] || continue
    name=$(basename "$f" .json)
    stats=$(jq -r '"\(.run.stats.assertions.total) \(.run.stats.assertions.failed) \(.run.stats.requests.total)"' "$f" 2>/dev/null || echo "0 0 0")
    set -- $stats
    printf "%-25s %10s %10s %10s\n" "$name" "$1" "$2" "$3"
  done
} | tee out/summary.txt
