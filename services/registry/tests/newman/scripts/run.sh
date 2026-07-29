#!/usr/bin/env bash

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# tests/newman/scripts/run.sh — newman runner for kacho-registry regression suites.
#
# Usage:
#   ./scripts/run.sh                          # all collections, summary report
#   ./scripts/run.sh --service registry       # single collection
#   ./scripts/run.sh --service registry --bail
#   ./scripts/run.sh --delay 100              # inter-request delay (ms)
#   ./scripts/run.sh --jobs 2                 # max parallel collections (default 4)
#   ./scripts/run.sh --env environments/kind-stand.postman_environment.json
#
# Each collection is isolated via {{runId}}-suffixed resource names within a
# shared pre-allocated existingProjectId, so parallel execution is safe.
#
# Exit code: 0 ONLY if every expected collection produced a report AND that report
# has assertions.failed==0, requests.failed==0 (no request left unanswered),
# assertions.total>0 (the suite is not mute) AND newman's own exit code was 0. The
# verdict is printed IN NUMBERS (collections reported out of how many, requests,
# assertions, failed assertions, unanswered requests, mute reports) and comes from
# the REPORT CONTENT (out/<stem>.json + out/<stem>.rc), never from "the run happened".
# The summary is printed ALWAYS, including on red — losing it is what motivated the
# old `| tee … || true` that swallowed newman's exit code.
#
# Outputs:
#   out/<service>.json — newman JSON reporter (for aggregation)
#   out/<service>.cli  — newman cli output
#   out/<service>.rc   — newman's exit code for that collection
#   out/summary.txt    — overall summary

set -euo pipefail
cd "$(dirname "$0")/.."

# COLLECTIONS — the suite's own expected set (gen.py emits 1:1 from cases/*.py).
COLLECTIONS=(registry registry-redesign registry-repository registry-authz)

SERVICE=""
BAIL=""
DELAY="15"
JOBS="4"
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
  # background subshell before its exit code is recorded. Take PIPESTATUS[0] (newman),
  # NOT tee and NOT `|| true` — swallowing newman's rc is what produced false GREEN.
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

# aggregate_verdict — the verdict FROM THE REPORTS, IN NUMBERS.
#
# THREE OUTCOMES, THREE NAMES. A request ends one of three ways, and the verdict
# reports each separately, because folding any of them into another is how a suite
# goes quiet:
#   answered + assertion passed — the check ran and said yes;
#   answered + assertion failed — the check ran and said no        (FAILED);
#   NOT ANSWERED                — the check did not run at all      (UNANSWERED).
# Plus the degenerate case of a report with no assertions at all, printed as MUTE
# rather than allowed to read as "nothing failed". UNANSWERED is never subtracted,
# whitelisted or explained away.
#
# Returns 1 when for ANY stem: out/<stem>.json is absent (MISSING),
# assertions.failed>0, requests.failed>0 (UNANSWERED), assertions.total==0 (MUTE),
# or rc!=0. Shape mirrors services/iam/tests/newman/scripts/assert-suites-green.sh.
aggregate_verdict() {
  local out_dir="$1"; shift
  local bad=0 stem json rcfile rc total failed requests unanswered
  local n_total=$# reported=0 t_req=0 t_ass=0 t_fail=0 t_unans=0 t_mute=0
  printf "%-25s %10s %10s %10s %12s %8s\n" "COLLECTION" "ASSERT" "FAILED" "REQUESTS" "UNANSWERED" "RC"
  for stem in "$@"; do
    json="${out_dir}/${stem}.json"
    rcfile="${out_dir}/${stem}.rc"
    rc="n/a"
    [[ -f "$rcfile" ]] && rc="$(cat "$rcfile")"
    if [[ ! -f "$json" ]]; then
      printf "%-25s %10s %10s %10s %12s %8s\n" "$stem" "-" "-" "-" "-" "MISSING"
      bad=1
      continue
    fi
    total=0; failed=0; requests=0; unanswered=0
    read -r total failed requests unanswered < <(
      jq -r '"\(.run.stats.assertions.total) \(.run.stats.assertions.failed) \(.run.stats.requests.total) \(.run.stats.requests.failed)"' \
        "$json" 2>/dev/null || echo "0 0 0 0"
    )
    [[ "$total" =~ ^[0-9]+$ ]]      || total=0
    [[ "$failed" =~ ^[0-9]+$ ]]     || failed=0
    [[ "$requests" =~ ^[0-9]+$ ]]   || requests=0
    [[ "$unanswered" =~ ^[0-9]+$ ]] || unanswered=0
    printf "%-25s %10s %10s %10s %12s %8s\n" "$stem" "$total" "$failed" "$requests" "$unanswered" "$rc"
    reported=$((reported + 1))
    t_req=$((t_req + requests)); t_ass=$((t_ass + total))
    t_fail=$((t_fail + failed)); t_unans=$((t_unans + unanswered))
    if [[ "$failed" -gt 0 ]]; then bad=1; fi
    if [[ "$rc" != "0" ]];    then bad=1; fi
    if [[ "$unanswered" -gt 0 ]]; then
      echo "  ^ ${stem}: ${unanswered} request(s) got NO response — the check did not run:" >&2
      jq -r '[.run.failures[]? | select((.error.name? // "") != "AssertionError")
              | "      NOT EXECUTED: \(.source.name? // "?") <- \(.error.message? // "no response")"]
             | unique | .[]' "$json" 2>/dev/null || true
      bad=1
    fi
    if [[ "$total" -eq 0 ]]; then
      echo "  ^ ${stem}: MUTE — 0 assertions; a suite that asked nothing cannot be green" >&2
      t_mute=$((t_mute + 1))
      bad=1
    fi
  done
  # The verdict in numbers, on one line. Read the FIRST pair first: a run that
  # stopped early leaves every other counter looking healthy.
  echo "TOTAL: ${reported}/${n_total} collection(s) reported, ${t_req} request(s), ${t_ass} assertion(s), ${t_fail} failed, ${t_unans} UNANSWERED, ${t_mute} mute report(s)"
  return "$bad"
}

mkdir -p out
# Fresh run: a stale report must not stand in for a collection that did not execute
# this time. Targeted (not `rm -rf out`) so an open out/suite.log survives.
rm -f out/*.json out/*.cli out/*.rc out/summary.txt 2>/dev/null || true

# stems — what the verdict covers: the explicit set plus any generated collection
# outside it (drift guard against a silently skipped collection).
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
  echo "OK: all registry collections green."
else
  echo "FAIL: one or more registry collections failed / missing (see the table above)." >&2
  exit 1
fi
