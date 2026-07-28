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
# AddressPool (kac-nlb-seed-ext-pool, a ZONE-DERIVED /24 = 254 addrs — 100.102.<zone-octet>.0/24,
# see deploy/scripts/seed-nlb-fixtures.sh). Under >1 concurrent
# collection that pool is transiently exhausted mid-run (`could not allocate load balancer
# address`) even though the VIP is correctly recycled on LB delete (release VIP =
# ClearReference→FreeIP + free_ip_runner self-heal, delete.go) — it is bursty concurrent
# HOLD, not a pool leak. Serial collections keep the peak concurrent VIP hold tiny (each
# EXTERNAL case creates then deletes its LB before the next) → no exhaustion, no masking.
# The external pool's (kind, block) EXCLUDE is GLOBAL (blind to name AND zone), which is why
# the seed now derives its block from the zone (100.102.<octet>.0/24) instead of sharing one
# fixed /24 with every other zone/suite. Capacity per zone is unchanged (254), so serialization
# remains the reliable, self-contained answer to concurrent VIP hold.
#   ./scripts/run.sh --env environments/kind-stand.postman_environment.json
#
# Each collection is isolated via {{runId}}-suffixed resource names within a
# shared pre-allocated existingProjectId, so parallel execution is safe.
#
# Exit code: 0 ONLY if every expected collection produced a report AND that report
# has assertions.failed==0 AND newman's own exit code was 0. The verdict is derived
# from the REPORT CONTENT (out/<stem>.json + out/<stem>.rc), never from "the run
# happened". The summary table is printed ALWAYS — including on red — because losing
# it is exactly what motivated the old `| tee … || true` that swallowed newman's exit
# code and let this suite print GREEN with 94 failed assertions.
#
# Outputs:
#   out/<service>.json — newman JSON reporter (for aggregation)
#   out/<service>.cli  — newman cli output
#   out/<service>.rc   — newman's exit code for that collection
#   out/summary.txt    — overall summary

set -euo pipefail
cd "$(dirname "$0")/.."

# COLLECTIONS — the suite's own expected set (gen.py emits 1:1 from cases/*.py).
COLLECTIONS=(load-balancer listener target-group targets operation authz-deny cross-resource list-filter placement-coherence)

SERVICE=""
BAIL=""
DELAY="15"
JOBS="1"   # serial collections: shared external AddressPool contention (see header)
ENV_DEFAULT="environments/local.postman_environment.json"
ENV="$ENV_DEFAULT"
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

# The env file is gitignored (the fixture seed writes live tokens into it) and NOTHING
# in the tree creates it: patch-env.py/setup.sh only PATCH a file that already exists
# and silently skip a missing one. On a fresh clone the suite died right here, before
# newman was ever invoked. So materialization belongs to the run path, not to a manual
# step: copy the committed template and let the seed fill in credentials/ids. Only the
# DEFAULT env is materialized — a --env path given by the caller is taken literally.
# An existing file is NEVER overwritten: it may hold the live session of the current run.
if [[ ! -f "$ENV" && "$ENV" == "$ENV_DEFAULT" && -f "${ENV%.json}.template.json" ]]; then
  cp "${ENV%.json}.template.json" "$ENV"
  echo "materialized $ENV from ${ENV%.json}.template.json (fixture seed will fill in credentials)" >&2
fi
[[ -f "$ENV" ]] || { echo "missing env: $ENV"; exit 1; }

# run_one — run one collection. Writes out/<svc>.json|.cli|.rc.
run_one() {
  local svc="$1"
  local col="collections/${svc}.postman_collection.json"
  if [[ ! -f "$col" ]]; then
    # Expected collection not emitted by gen.py = silent coverage loss, not a skip.
    echo "[missing] ${svc} — no collection ${col}"
    echo "missing" > "out/${svc}.rc"
    return 0
  fi
  echo "===== ${svc} ====="
  # Drop errexit around the pipe so a newman failure (via pipefail) cannot kill the
  # background subshell before its real exit code is recorded. Take PIPESTATUS[0]
  # (newman) — NOT tee, and NOT `|| true`: swallowing newman's rc is what let this
  # suite report GREEN with 94 failed assertions.
  set +e
  newman run "$col" \
    -e "$ENV" \
    --delay-request "$DELAY" \
    $BAIL \
    --reporters cli,json \
    --reporter-json-export "out/${svc}.json" \
    ${EXTRA[@]+"${EXTRA[@]}"} 2>&1 | tee "out/${svc}.cli"
  local rc=${PIPESTATUS[0]}
  set -e
  echo "$rc" > "out/${svc}.rc"
  return 0
}

# aggregate_verdict — pure verdict over the REPORTS. Prints the table and returns 1
# when any stem: has no out/<stem>.json (MISSING), assertions.failed>0, or rc!=0.
aggregate_verdict() {
  local out_dir="$1"; shift
  local bad=0 stem json rcfile rc total failed requests
  printf "%-25s %10s %10s %10s %8s\n" "COLLECTION" "ASSERT" "FAILED" "REQUESTS" "RC"
  for stem in "$@"; do
    json="${out_dir}/${stem}.json"
    rcfile="${out_dir}/${stem}.rc"
    rc="n/a"
    [[ -f "$rcfile" ]] && rc="$(cat "$rcfile")"
    if [[ ! -f "$json" ]]; then
      printf "%-25s %10s %10s %10s %8s\n" "$stem" "-" "-" "-" "MISSING"
      bad=1
      continue
    fi
    total=0; failed=0; requests=0
    read -r total failed requests < <(
      jq -r '"\(.run.stats.assertions.total) \(.run.stats.assertions.failed) \(.run.stats.requests.total)"' \
        "$json" 2>/dev/null || echo "0 0 0"
    )
    [[ "$total" =~ ^[0-9]+$ ]]    || total=0
    [[ "$failed" =~ ^[0-9]+$ ]]   || failed=0
    [[ "$requests" =~ ^[0-9]+$ ]] || requests=0
    printf "%-25s %10s %10s %10s %8s\n" "$stem" "$total" "$failed" "$requests" "$rc"
    if [[ "$failed" -gt 0 ]]; then bad=1; fi
    if [[ "$rc" != "0" ]];    then bad=1; fi
  done
  return "$bad"
}

mkdir -p out
# Fresh run: drop the previous artefacts so a stale report cannot stand in for a
# collection that did not execute this time. Targeted (not `rm -rf out`) so an
# already-open out/suite.log from newman-parallel.sh survives.
rm -f out/*.json out/*.cli out/*.rc out/summary.txt 2>/dev/null || true

# stems — what the verdict covers. Explicit suite set, plus any generated collection
# not in it (drift guard: a newly generated collection must not be silently skipped).
stems=()
if [[ -n "$SERVICE" ]]; then
  stems=("$SERVICE")
  run_one "$SERVICE"
else
  stems=("${COLLECTIONS[@]}")
  for f in collections/*.postman_collection.json; do
    [[ -e "$f" ]] || continue
    extra_stem="$(basename "$f" .postman_collection.json)"
    for known in "${COLLECTIONS[@]}"; do
      if [[ "$known" == "$extra_stem" ]]; then continue 2; fi
    done
    echo "[drift] ${extra_stem} — generated but not in COLLECTIONS; running it anyway"
    stems+=("$extra_stem")
  done
  for svc in "${stems[@]}"; do
    while [[ "$(jobs -rp | wc -l)" -ge "$JOBS" ]]; do wait -n; done
    run_one "$svc" &
  done
  wait
fi

echo
echo "===== Summary ====="
# pipefail carries the non-zero verdict through tee — the summary is still printed.
if aggregate_verdict "out" "${stems[@]}" | tee out/summary.txt; then
  echo "OK: all nlb collections green."
else
  echo "FAIL: one or more nlb collections failed / missing (see the table above)." >&2
  exit 1
fi
