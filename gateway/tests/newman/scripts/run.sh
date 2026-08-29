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
# Exit code: 0 only if EVERY collection has assertions.failed==0, requests.failed==0
# (no request left unanswered), assertions.total>0 (the suite is not mute), newman
# rc==0 and a report present. Any failure / crash / timeout / absence → exit 1. The
# verdict is printed IN NUMBERS: collections reported out of how many, requests,
# assertions, failed assertions, unanswered requests, mute reports.
#
# Outputs:
#   out/<collection>.json — newman JSON reporter (aggregated by the CI gate)
#   out/<collection>.cli  — newman CLI output
#   out/<collection>.rc   — newman exit code for that collection
#   out/summary.txt       — final table

NEWMAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# ОТБОР КОЛЛЕКЦИЙ — ИЗ ОБЩЕГО СЛОЯ, А НЕ СВОЕЙ КОПИЕЙ.
#
# Здесь стояли `expected_stems`/`present_stems` — побайтово те же, что у двух
# соседних прогонщиков. Правка правила отбора стоила трёх правок, а «поправил у
# себя» было неотличимо от «поправил везде»; правило при этом уже разошлось с
# генератором: копии пропускали `__init__`/`__main__`, генератор — ЛЮБОЕ имя с
# ведущим подчёркиванием. У vpc/geo/gateway модулей с подчёркиванием нет, поэтому
# расхождение было невидимо ИМЕННО ТАМ, где его писали; у nlb и registry такой
# модуль есть, и по узкому правилу `_helpers` стал бы ожидаемой коллекцией.
#
# Общий слой ищется ВВЕРХ ОТ ЭТОГО ФАЙЛА, а не от cwd: прогонщик зовут из
# каталога набора, и путь, выведенный из текущего каталога, был бы свойством
# того, ОТКУДА позвали. Поиск — тот же бутстрап, что у `_kacholib_dir()` в
# gen.py, и по той же причине неустраним: общий слой нельзя найти его же
# средствами.
_stems_lib() {
  local d="$NEWMAN_DIR"
  while [[ "$d" != "/" ]]; do
    if [[ -f "$d/tests/newman/kacholib/stems.sh" ]]; then
      printf '%s
' "$d/tests/newman/kacholib/stems.sh"
      return 0
    fi
    d="$(dirname "$d")"
  done
  echo "общий слой отбора не найден: ожидается <корень>/tests/newman/kacholib/stems.sh" >&2
  echo "Это ОТКАЗ, а не пропуск: без него прогонщик выбрал бы коллекции молча и не те." >&2
  return 1
}
_STEMS_LIB="$(_stems_lib)"
# shellcheck source=/dev/null
. "$_STEMS_LIB"

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
  printf "%-28s %10s %10s %10s %12s %8s\n" "COLLECTION" "ASSERT" "FAILED" "REQUESTS" "UNANSWERED" "RC"
  for stem in "$@"; do
    json="${out_dir}/${stem}.json"
    rcfile="${out_dir}/${stem}.rc"
    rc="n/a"
    [[ -f "$rcfile" ]] && rc="$(cat "$rcfile")"
    if [[ ! -f "$json" ]]; then
      printf "%-28s %10s %10s %10s %12s %8s\n" "$stem" "-" "-" "-" "-" "MISSING"
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
    printf "%-28s %10s %10s %10s %12s %8s\n" "$stem" "$total" "$failed" "$requests" "$unanswered" "$rc"
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
  # The env file is gitignored (the fixture seed writes live tokens into it) and
  # NOTHING in the tree creates it: patch-env.py/setup.sh only PATCH a file that
  # already exists and silently skip a missing one. On a fresh clone the suite died
  # right here, before newman was ever invoked. So materialization belongs to the run
  # path, not to a manual step: copy the committed template and let the seed fill in
  # credentials/ids. An existing file is NEVER overwritten — it may hold the live
  # session of the current run.
  if [[ ! -f "$ENV" && -f "${ENV%.json}.template.json" ]]; then
    cp "${ENV%.json}.template.json" "$ENV"
    echo "materialized $ENV from ${ENV%.json}.template.json (fixture seed will fill in credentials)" >&2
  fi
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
    done < <(newman_all_stems "$NEWMAN_DIR")
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
