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
# has assertions.failed==0, requests.failed==0 (no request left unanswered),
# assertions.total>0 (the suite is not mute) AND newman's own exit code was 0. The
# verdict is printed IN NUMBERS (collections reported out of how many, requests,
# assertions, failed assertions, unanswered requests, mute reports) and is derived
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

# ПРЕДПОЛЁТНАЯ САМОПРОВЕРКА ЧЕСТНОСТИ УТВЕРЖДЕНИЙ (#1427).
#
# Гейт, которого никто не зовёт, неотличим от отсутствующего: он не краснеет не
# потому, что суита исправна, а потому, что его не спрашивали. Самопроверка
# находит классы, которых прогон суиты не находит by construction — кейс, для
# которого успех и отказ неразличимы; опрос операции чужим субъектом; обёртка
# ожидания без ведомости, — и пока её не зовут, они въезжают в ствол молча.
#
# Место вызова — ПЕРЕД стендом и сетью: самопроверка судит сгенерированные
# коллекции, стенда не требует и стоит секунды, а её отказ означает «прогон не
# вынесет вердикта о продукте». Тратить на это минуты подъёма незачем.
#
# Достижимость стережёт гейт дерева `TestNewmanSelftestIsReachableFromItsRunner`
# (internal/repohygiene): снять этот вызов молча нельзя. node здесь заведомо
# есть — newman на нём и работает.
if ! node scripts/selftest-assertions.js; then
  echo "FAIL: предполётная самопроверка утверждений не прошла — прогон не выносит вердикт о продукте." >&2
  exit 1
fi

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
# ВЕДОМОСТЬ ОЖИДАНИЯ — ЧЕТВЁРТАЯ ВЕЛИЧИНА, И ОНА НЕ ВЕРДИКТ (задача #1251).
#
# Обёртки ожидания (окно материализации прав, видимость соседа, наличие в списке,
# сходимость состояния, повтор создания) записывают в окружение, сколько раз бюджет
# ожидания был ИСЧЕРПАН и какое наибольшее число попыток понадобилось. До этого оба
# исхода — «прогреть не удалось» и «прогрев не понадобился» — давали одинаковый след,
# то есть никакого, и разбор красного начинался с гипотезы.
#
# ПЕЧАТАЕТСЯ ВСЕГДА, В ТОМ ЧИСЛЕ НУЛЁМ: «ноль исчерпаний» обязано быть отличимо от
# «не измеряли» — иначе величина, ради которой всё и заведено, снова становится
# ненаблюдаемой. Красным она НЕ делает: fail-open заведён затем, чтобы настоящий
# отказ падал на своём шаге и по своему предмету, а шаг, чьё окно закрылось на
# попытку позже бюджета, исправен по существу. Ненулевое значение — сигнал о том,
# что бюджет выбран на грани, и повод посмотреть на названные шаги.
aggregate_verdict() {
  local out_dir="$1"; shift
  local bad=0 stem json rcfile rc total failed requests unanswered warm warmmax
  local n_total=$# reported=0 t_req=0 t_ass=0 t_fail=0 t_unans=0 t_mute=0
  local t_warm=0 t_warmmax=0
  printf "%-25s %10s %10s %10s %12s %9s %8s\n" \
    "COLLECTION" "ASSERT" "FAILED" "REQUESTS" "UNANSWERED" "WARM-EXH" "RC"
  for stem in "$@"; do
    json="${out_dir}/${stem}.json"
    rcfile="${out_dir}/${stem}.rc"
    rc="n/a"
    [[ -f "$rcfile" ]] && rc="$(cat "$rcfile")"
    if [[ ! -f "$json" ]]; then
      printf "%-25s %10s %10s %10s %12s %9s %8s\n" "$stem" "-" "-" "-" "-" "-" "MISSING"
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
    # Ведомость лежит в ИТОГОВОМ окружении отчёта: newman сериализует его целиком,
    # поэтому величина, записанная шагом, доезжает сюда без отдельного репортёра.
    warm=0; warmmax=0
    read -r warm warmmax < <(
      jq -r '(.environment.values // []) as $v
             | [($v[] | select(.key == "warmBudgetExhausted") | .value // 0)][0] // 0
             | tostring
             | . + " " + ((($v[] | select(.key == "warmRetryMaxAttempts") | .value // 0)) // 0 | tostring)' \
        "$json" 2>/dev/null || echo "0 0"
    )
    [[ "$warm" =~ ^[0-9]+$ ]]    || warm=0
    [[ "$warmmax" =~ ^[0-9]+$ ]] || warmmax=0
    printf "%-25s %10s %10s %10s %12s %9s %8s\n" \
      "$stem" "$total" "$failed" "$requests" "$unanswered" "$warm" "$rc"
    t_warm=$((t_warm + warm))
    if [[ "$warmmax" -gt "$t_warmmax" ]]; then t_warmmax="$warmmax"; fi
    if [[ "$warm" -gt 0 ]]; then
      # НЕ отказ: величина названа, чтобы разбор красного начинался с факта.
      echo "  ~ ${stem}: бюджет ожидания исчерпан ${warm} раз(а) — шаги:" >&2
      jq -r '(.environment.values // [])[]
             | select(.key == "warmBudgetExhaustedSteps") | .value' "$json" 2>/dev/null \
        | tr " " "\n" | sed "s/^/      /" || true
    fi
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
  # Печатается и нулём — иначе «ни разу не исчерпан» неотличимо от «не измеряли».
  echo "WAIT-BUDGET: исчерпан ${t_warm} раз(а); наибольшее число потраченных попыток ${t_warmmax} (не вердикт — величина)"
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
  while IFS= read -r s; do
    [[ -n "$s" ]] && stems+=("$s")
  done < <(newman_all_stems "$NEWMAN_DIR")
  if [[ "${#stems[@]}" -eq 0 ]]; then
    echo "FAIL: коллекций не найдено — прогон без предмета не может быть зелёным." >&2
    exit 1
  fi
  echo "[stems] коллекций к прогону: ${#stems[@]} (выведены из дерева, не выписаны): ${stems[*]}"
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
