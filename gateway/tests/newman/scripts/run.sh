#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# tests/newman/scripts/run.sh — run the api-gateway newman collections with an
# HONEST exit code and false-green guards. Mirrors services/geo/tests/newman/
# scripts/run.sh so `deploy/scripts/newman-parallel.sh` can drive every suite
# through one identical interface.
#
# Usage:
#   ./scripts/run.sh                                   # all collections
#   ./scripts/run.sh --service cluster_admin           # one collection
#   ./scripts/run.sh --delay 100                       # per-request delay (ms)
#   ./scripts/run.sh --jobs 2                          # cap on parallel collections
#   ./scripts/run.sh --env-var baseUrl=http://localhost:18080
#
# The collection set is the UNION of the source-of-truth cases/*.py (gen.py emits
# exactly one collection per case file) and the collections/*.json actually on
# disk. So no collection is skipped silently, and an expected-but-absent one
# (cases/<x>.py exists, collections/<x>.json does not) is recorded as MISSING and
# FAILS the run — a suite that did not execute is a failed suite, not a green one.
#
# --jobs is deliberately NOT forwarded to `newman run` (it would be an unknown
# option → the collection produces no report → false-green no-report). It only
# caps the local parallel pool.
#
# Exit code: 0 only if EVERY collection has assertions.failed==0, newman rc==0 and
# a report present. Any failure / crash / timeout / absence → exit 1.
#
# Outputs:
#   out/<collection>.json — newman JSON reporter (aggregated by the CI gate)
#   out/<collection>.cli  — newman CLI output
#   out/<collection>.rc   — newman exit code for that collection
#   out/summary.txt       — final table

NEWMAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# expected_stems — the collections that SHOULD exist: basename of each cases/*.py.
expected_stems() {
  local f stem
  for f in "$NEWMAN_DIR"/cases/*.py; do
    [[ -e "$f" ]] || continue
    stem="$(basename "$f" .py)"
    case "$stem" in __init__|__main__) continue ;; esac
    printf '%s\n' "$stem"
  done
}

# present_stems — the collections actually generated.
present_stems() {
  local f stem
  for f in "$NEWMAN_DIR"/collections/*.postman_collection.json; do
    [[ -e "$f" ]] || continue
    stem="$(basename "$f" .postman_collection.json)"
    printf '%s\n' "$stem"
  done
}

# run_one — run a single collection. Writes out/<stem>.json|.cli|.rc.
run_one() {
  local svc="$1"
  local col="collections/${svc}.postman_collection.json"
  if [[ ! -f "$col" ]]; then
    echo "[missing] ${svc} — no collection ${col} (run scripts/gen.py)"
    echo "missing" > "out/${svc}.rc"
    return 0
  fi
  echo "===== ${svc} ====="
  # errexit is lifted around the pipe so a newman failure (via pipefail) cannot
  # kill the background subshell before the exit code is recorded. PIPESTATUS[0]
  # is newman's code, not tee's.
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

# aggregate_verdict — returns 1 if for ANY stem: out/<stem>.json is absent
# (MISSING), assertions.failed>0, or rc!=0.
aggregate_verdict() {
  local out_dir="$1"; shift
  local bad=0 stem json rcfile rc total failed requests
  printf "%-28s %10s %10s %10s %8s\n" "COLLECTION" "ASSERT" "FAILED" "REQUESTS" "RC"
  for stem in "$@"; do
    json="${out_dir}/${stem}.json"
    rcfile="${out_dir}/${stem}.rc"
    rc="n/a"
    [[ -f "$rcfile" ]] && rc="$(cat "$rcfile")"
    if [[ ! -f "$json" ]]; then
      printf "%-28s %10s %10s %10s %8s\n" "$stem" "-" "-" "-" "MISSING"
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
    printf "%-28s %10s %10s %10s %8s\n" "$stem" "$total" "$failed" "$requests" "$rc"
    [[ "$failed" -gt 0 ]] && bad=1
    [[ "$rc" == "0" ]]    || bad=1
    # A collection that ran but asserted NOTHING is not green, it is mute — the
    # same false-green class as a missing report, one level down.
    if [[ "$total" -eq 0 ]]; then
      echo "  ^ ${stem}: 0 assertions executed — treated as FAILED (a suite that" >&2
      echo "    checks nothing cannot be green; check the env file and --env-var)." >&2
      bad=1
    fi
  done
  return "$bad"
}

main() {
  set -euo pipefail
  cd "$NEWMAN_DIR"

  SERVICE=""
  BAIL=""
  DELAY="15"
  JOBS="4"
  EXTRA=()

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --service) SERVICE="$2"; shift 2 ;;
      --bail)    BAIL="--bail"; shift ;;
      --delay)   DELAY="$2"; shift 2 ;;
      # --jobs: local pool cap only. Consume-and-ignore for `newman run` (must NOT
      # be forwarded — unknown option → no report → false-green).
      --jobs)    JOBS="$2"; shift 2 ;;
      *)         EXTRA+=("$1"); shift ;;
    esac
  done

  ENV="environments/local.postman_environment.json"
  [[ -f "$ENV" ]] || { echo "missing env: $ENV" >&2; exit 1; }

  mkdir -p out
  # Fresh run: drop previous artifacts so a stale json cannot mask a collection
  # that dropped out of this run (false-green guard).
  rm -f out/*.json out/*.cli out/*.rc out/summary.txt 2>/dev/null || true

  local -a stems=()
  if [[ -n "$SERVICE" ]]; then
    stems=("$SERVICE")
  else
    local s
    while IFS= read -r s; do
      [[ -n "$s" ]] && stems+=("$s")
    done < <( { expected_stems; present_stems; } | sort -u )
  fi

  if [[ "${#stems[@]}" -eq 0 ]]; then
    echo "FAIL: no collections and no case files — nothing to run." >&2
    exit 1
  fi

  # The cluster-admin collection mutates the cluster-admin roster (grant → revoke
  # of its own run-scoped subject). Its cases are ordered and share state through
  # environment variables, so a collection is never split; collections themselves
  # are independent and may run in parallel up to $JOBS.
  local svc
  for svc in "${stems[@]}"; do
    if [[ -z "${SERVICE:-}" ]]; then
      while [[ "$(jobs -rp | wc -l)" -ge "$JOBS" ]]; do wait -n; done
      run_one "$svc" &
    else
      run_one "$svc"
    fi
  done
  wait

  echo
  echo "===== Summary ====="
  if aggregate_verdict "out" "${stems[@]}" | tee out/summary.txt; then
    echo "OK: all collections green."
  else
    echo "FAIL: one or more collections failed / missing / mute (see table above)." >&2
    exit 1
  fi
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  main "$@"
fi
