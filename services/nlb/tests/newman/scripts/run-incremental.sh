#!/usr/bin/env bash

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# tests/newman/scripts/run-incremental.sh — quota-safe PER-FOLDER newman runner.
#
# The same suite run.sh runs, executed one Postman folder (= one case) at a time so a
# stand with tight per-RPC quotas (e.g. an AddressPool too small to serve the whole
# suite in one shot) still gets through it. The trade-off is wall time, not coverage:
# the collection set below is the suite's full set, and the folder set is enumerated
# from the collections themselves.
#
# Usage:
#   ./scripts/run-incremental.sh                          # every generated collection
#   ./scripts/run-incremental.sh --service load-balancer
#   ./scripts/run-incremental.sh --resume                 # skip cases already in out/inc/*.json
#   ./scripts/run-incremental.sh --env environments/kind-stand.postman_environment.json
#
# Exit code: 0 ONLY if every folder this run is accountable for produced a report AND
# that report has assertions.failed==0 AND newman's own exit code for it was 0. The
# verdict is derived from REPORT CONTENT (out/inc/<stem>.json + out/inc/<stem>.rc),
# never from "the run happened". Previously each newman invocation ended in `|| true`,
# no verdict was computed at all, and the script exited 0 whatever happened — a red
# case was indistinguishable from a green one to anything reading the exit code, and
# the printed table counted whatever files were lying in out/inc/, including reports
# left by an earlier run. The table is printed ALWAYS, including on red: losing it is
# what motivated the swallow in the first place.
#
# Outputs:
#   out/inc/<service>__<folder>.json — newman JSON reporter (one per case)
#   out/inc/<service>__<folder>.rc   — newman's exit code for that case
#   out/inc-summary.txt              — per-case table + verdict

set -euo pipefail
cd "$(dirname "$0")/.."
NEWMAN_DIR="$PWD"

# ОТБОР КОЛЛЕКЦИЙ — ИЗ ОБЩЕГО СЛОЯ, А НЕ РУКОПИСНЫМ МАССИВОМ.
#
# Здесь стоял массив `COLLECTIONS=(...)` — второе место об одном предмете.
# Перечень коллекций объявлен деревом (`gen.py` эмитит коллекцию на каждый
# `cases/<имя>.py`), и рукописная копия расходится с ним молча, ровно в одну
# сторону: новый модуль кейсов появляется, коллекция генерируется, а массив о
# ней не знает. У registry это уже случилось, и подхват печатал предупреждение
# на КАЖДОМ штатном прогоне — а такое предупреждение перестают читать вместе с
# настоящими.
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
      printf '%s\n' "$d/tests/newman/kacholib/stems.sh"
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


SERVICE=""
ENV_DEFAULT="environments/local.postman_environment.json"
ENV="$ENV_DEFAULT"
RESUME=0
DELAY="15"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --service) SERVICE="$2"; shift 2 ;;
    --env)     ENV="$2"; shift 2 ;;
    --resume)  RESUME=1; shift ;;
    --delay)   DELAY="$2"; shift 2 ;;
    *) echo "unknown arg: $1"; exit 1 ;;
  esac
done

# Materialize the default env from its committed template (same rule as run.sh): the
# env file is gitignored because the fixture seed writes live tokens into it, and
# nothing else in the tree creates it. An existing file is NEVER overwritten — it may
# hold the live session of the current run. A caller-supplied --env is taken literally.
if [[ ! -f "$ENV" && "$ENV" == "$ENV_DEFAULT" && -f "${ENV%.json}.template.json" ]]; then
  cp "${ENV%.json}.template.json" "$ENV"
  echo "materialized $ENV from ${ENV%.json}.template.json (fixture seed will fill in credentials)" >&2
fi
[[ -f "$ENV" ]] || { echo "missing env: $ENV"; exit 1; }

mkdir -p out/inc

# out_json_for — report path for one (service, folder) pair.
out_json_for() {
  local svc="$1" folder="$2"
  echo "out/inc/${svc}__${folder//[^a-zA-Z0-9_-]/_}.json"
}

# run_folder — run ONE case-folder. Writes out/inc/<stem>.json and out/inc/<stem>.rc.
run_folder() {
  local svc="$1" folder="$2"
  local out_json; out_json="$(out_json_for "$svc" "$folder")"
  local rc_file="${out_json%.json}.rc"
  if [[ "$RESUME" == "1" && -f "$out_json" ]]; then
    echo "[skip-resume] $svc / $folder"
    return 0
  fi
  echo "--- $svc / $folder ---"
  # errexit is dropped around the call so one red case does not abort the sweep — the
  # whole point of the incremental runner. The exit code is RECORDED rather than
  # discarded; `|| true` is what made every verdict meaningless.
  set +e
  newman run "collections/${svc}.postman_collection.json" \
    -e "$ENV" \
    --folder "$folder" \
    --delay-request "$DELAY" \
    --reporters cli,json \
    --reporter-json-export "$out_json"
  local rc=$?
  set -e
  echo "$rc" > "$rc_file"
  return 0
}

list_folders() {
  jq -r '.item[].name' "$1"
}

# services_to_run — набор, ВЫВЕДЕННЫЙ из дерева общим слоем: объединение
# ожидаемых коллекций (по модулю кейсов на каждую) и фактически сгенерированных.
# Ни одна коллекция не пропускается молча, а ожидаемая-но-отсутствующая доезжает
# до вердикта как MISSING (см. `expected` ниже).
services_to_run=()
if [[ -n "$SERVICE" ]]; then
  services_to_run+=("$SERVICE")
else
  while IFS= read -r s; do
    [[ -n "$s" ]] && services_to_run+=("$s")
  done < <(newman_all_stems "$NEWMAN_DIR")
  if [[ "${#services_to_run[@]}" -eq 0 ]]; then
    echo "FAIL: коллекций не найдено — прогон без предмета не может быть зелёным." >&2
    exit 1
  fi
  echo "[stems] коллекций к прогону: ${#services_to_run[@]} (выведены из дерева, не выписаны): ${services_to_run[*]}"
fi

# expected — every stem this run is accountable for. The verdict below reads THIS
# list, not the directory listing: a report left behind by an earlier run must never
# stand in for a case that did not execute now.
expected=()
for svc in "${services_to_run[@]}"; do
  col="collections/${svc}.postman_collection.json"
  if [[ ! -f "$col" ]]; then
    # An expected collection gen.py did not emit is coverage LOSS, not a skip.
    echo "[missing] ${svc} — no collection ${col}"
    expected+=("MISSING-COLLECTION:${svc}")
    continue
  fi
  while IFS= read -r folder; do
    [[ -z "$folder" ]] && continue
    expected+=("$(out_json_for "$svc" "$folder")")
    run_folder "$svc" "$folder"
  done < <(list_folders "$col")
done

# aggregate_verdict — pure verdict over the reports this run is accountable for.
# Returns 1 on any missing report, any failed assertion, any request that got NO
# ANSWER, any report with zero assertions, or any non-zero newman rc.
#
# THE LAST TWO WERE MISSING, and both read as a clean case. A request ends one of
# four ways and this verdict had names for two:
#   * UNANSWERED (`.run.stats.requests.failed`) — attempted, nothing came back
#     (DNS / refused / TLS / timeout). It is not an assertion failure (nothing was
#     asserted) and not a passing check: the check did not happen. The jq below
#     did not even read the field, so an entire folder that could not be reached
#     scored `0 failed` and passed.
#   * MUTE (`assertions.total == 0`) — the value was read and then never consulted.
#     A folder that asked nothing is indistinguishable from a folder that asked
#     everything and agreed, if the only number you look at is "failed".
# The sibling runner in this very suite (scripts/run.sh) carries both, and its
# header says why. These two drifted apart in silence.
# Held by deploy/scripts/assert-verdict-aggregators-honest.sh, which executes this
# function against synthetic reports (a clean one must return 0; each of the five
# non-clean shapes must return non-zero).
aggregate_verdict() {
  local bad=0 stem json rc total failed requests unanswered name note
  printf "%-62s %8s %8s %9s %12s %8s  %s\n" "CASE" "ASSERT" "FAILED" "REQUESTS" "UNANSWERED" "RC" "NOTE"
  for stem in "$@"; do
    if [[ "$stem" == MISSING-COLLECTION:* ]]; then
      printf "%-62s %8s %8s %9s %12s %8s  %s\n" "${stem#MISSING-COLLECTION:} (whole collection)" "-" "-" "-" "-" "MISSING" ""
      bad=1
      continue
    fi
    json="$stem"
    name="$(basename "$json" .json)"
    rc="n/a"
    [[ -f "${json%.json}.rc" ]] && rc="$(cat "${json%.json}.rc")"
    if [[ ! -f "$json" ]]; then
      printf "%-62s %8s %8s %9s %12s %8s  %s\n" "$name" "-" "-" "-" "-" "MISSING" ""
      bad=1
      continue
    fi
    total=0; failed=0; requests=0; unanswered=0
    read -r total failed requests unanswered < <(
      jq -r '"\(.run.stats.assertions.total) \(.run.stats.assertions.failed) \(.run.stats.requests.total) \(.run.stats.requests.failed // 0)"' \
        "$json" 2>/dev/null || echo "0 0 0 0"
    )
    [[ "$total" =~ ^[0-9]+$ ]]      || total=0
    [[ "$failed" =~ ^[0-9]+$ ]]     || failed=0
    [[ "$requests" =~ ^[0-9]+$ ]]   || requests=0
    [[ "$unanswered" =~ ^[0-9]+$ ]] || unanswered=0
    note=""
    if [[ "$total" -eq 0 ]]; then
      note="NOTHING RAN — 0 assertions; a case that asked nothing does not pass"
      bad=1
    fi
    if [[ "$unanswered" -gt 0 ]]; then
      note="${note:+$note; }UNANSWERED — never subtracted, never explained away"
      bad=1
    fi
    printf "%-62s %8s %8s %9s %12s %8s  %s\n" "$name" "$total" "$failed" "$requests" "$unanswered" "$rc" "$note"
    if [[ "$unanswered" -gt 0 ]]; then
      jq -r '[.run.failures[]? | select((.error.name? // "") != "AssertionError")
              | "    NOT EXECUTED: \(.source.name? // "?") <- \(.error.message? // "no response")"]
             | unique | .[]' "$json" 2>/dev/null || true
    fi
    if [[ "$failed" -gt 0 ]]; then bad=1; fi
    # --resume carries over a case from an earlier run and leaves no fresh .rc; its
    # report still decides. Any other non-zero rc is red.
    if [[ "$rc" != "0" && "$rc" != "n/a" ]]; then bad=1; fi
  done
  return "$bad"
}

echo
echo "===== Per-case summary ====="
# pipefail carries the non-zero verdict through tee — the table is still printed.
if aggregate_verdict "${expected[@]}" | tee out/inc-summary.txt; then
  echo "OK: all nlb cases green."
else
  echo "FAIL: one or more nlb cases failed / missing (see the table above)." >&2
  exit 1
fi
