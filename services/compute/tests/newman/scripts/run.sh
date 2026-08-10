#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# tests/newman/scripts/run.sh — прогон newman коллекций kacho-compute.
#
# Usage:
#   ./scripts/run.sh                       # все коллекции, сводный отчёт
#   ./scripts/run.sh --service disk        # одна коллекция
#   ./scripts/run.sh --service disk --bail # прерывать после первого fail
#   ./scripts/run.sh --delay 100           # задержка между запросами (ms)
#
# Exit-код: 0 ТОЛЬКО если каждая ожидаемая коллекция произвела отчёт И в нём
# assertions.failed==0, requests.failed==0 (ни одного запроса без ответа),
# assertions.total>0 (суита не немая) И собственный exit-код newman==0. Вердикт
# выводится из СОДЕРЖИМОГО отчётов (out/<stem>.json + out/<stem>.rc), а не из факта
# запуска, и печатается ЧИСЛАМИ: коллекций с отчётом из скольких, запросов,
# утверждений, упавших утверждений, запросов без ответа, немых отчётов.
# Сводка печатается ВСЕГДА, в том числе на красном — её потеря и породила прежний
# `| tee … || true`, который глотал код возврата newman (суита печатала GREEN при
# упавших проверках).
#
# Outputs:
#   out/<resource>.json — newman JSON reporter (для агрегации)
#   out/<resource>.cli  — newman cli-вывод
#   out/<resource>.rc   — exit-код newman конкретной коллекции
#   out/summary.txt     — итоговая сводка
#
# Требует: api-gateway доступен по baseUrl из env (локально — port-forward на 18080);
#          newman установлен (`npm install -g newman`); jq для сводки.

set -euo pipefail
cd "$(dirname "$0")/.."

# COLLECTIONS — собственный ожидаемый набор суиты (gen.py эмитит 1:1 из cases/*.py).
# NB: legacy `instance` RETIRED — вытеснен `instance-redesign` (COMP-1 Instance core:
# instanceKind/machineTypeId/bootSource/vm|container spec). Legacy-суита была целиком
# построена на tombstoned platformId/resourcesSpec/bootDiskSpec create-поверхности
# (RESERVED в CreateInstanceRequest, ban #2) и не могла пройти.
# NB: `disk` / `image` / `snapshot` / `disk-type` RETIRED вместе с дублем блочного
# хранения (625f6f05, a52f0b87) — владелец один, kacho-storage; их cases/ и
# collections/ удалены, суиты живут в services/storage/tests/newman. Этот список —
# ОЖИДАЕМЫЙ набор, и `run_one` на отсутствующий файл пишет rc="missing" → строка
# MISSING → bad=1. Ретайрнутые стемы, оставленные здесь, делали вердикт compute
# КРАСНЫМ безусловно, независимо от утверждений (drift-петля ниже только ДОБАВЛЯЕТ
# стемы, никогда не вычищает).
COLLECTIONS=(instance-redesign machine-type operation list-filter authz-deny sec-d instance-nic-attach)

SERVICE=""
BAIL=""
DELAY="100"
EXTRA=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --service) SERVICE="$2"; shift 2 ;;
    --bail)    BAIL="--bail"; shift ;;
    --delay)   DELAY="$2"; shift 2 ;;
    # --jobs: принят для паритета с newman-parallel.sh (директива #1) — consume-and-
    # ignore, НЕ пробрасывать в `newman run` (иначе `unknown option '--jobs'` → часть
    # коллекций без отчёта → ложный no-report/false-green). compute-суита серийна.
    --jobs)    shift 2 ;;
    *)         EXTRA+=("$1"); shift ;;
  esac
done

ENV="environments/local.postman_environment.json"
# Env-файл gitignore-ится (fixture-seed пишет в него живые токены), но НИКТО его не
# создаёт: patch-env.py/setup.sh только ПАТЧАТ уже существующий и молча пропускают
# отсутствующий. На свежем клоне суита падала здесь ещё до вызова newman. Поэтому
# материализация — часть пути прогона, а не ручной шаг: копируем закоммиченный
# шаблон, креды/id допишет fixture-seed. Существующий файл НЕ перетираем — в нём
# может лежать живая сессия текущего прогона.
if [[ ! -f "$ENV" && -f "${ENV%.json}.template.json" ]]; then
  cp "${ENV%.json}.template.json" "$ENV"
  echo "создан $ENV из ${ENV%.json}.template.json (креды допишет fixture-seed)" >&2
fi
[[ -f "$ENV" ]] || { echo "missing env: $ENV"; exit 1; }

# run_one — прогон одной коллекции. Пишет out/<res>.json|.cli|.rc.
run_one() {
  local res="$1"
  local col="collections/${res}.postman_collection.json"
  if [[ ! -f "$col" ]]; then
    # Ожидаемая коллекция не сгенерирована = молчаливая потеря покрытия, не skip.
    echo "[missing] ${res} — нет коллекции ${col}"
    echo "missing" > "out/${res}.rc"
    return 0
  fi
  echo "===== ${res} ====="
  # Берём PIPESTATUS[0] (newman), а НЕ статус tee и НЕ `|| true`: проглоченный
  # код возврата newman — то, из-за чего суита печатала GREEN при красных проверках.
  set +e
  newman run "$col" \
    -e "$ENV" \
    --delay-request "$DELAY" \
    $BAIL \
    --reporters cli,json \
    --reporter-json-export "out/${res}.json" \
    ${EXTRA[@]+"${EXTRA[@]}"} 2>&1 | tee "out/${res}.cli"
  local rc=${PIPESTATUS[0]}
  set -e
  echo "$rc" > "out/${res}.rc"
  return 0
}

# aggregate_verdict — чистый вердикт ПО ОТЧЁТАМ, В ЧИСЛАХ.
#
# ТРИ ИСХОДА, ТРИ ИМЕНИ. Запрос кончается одним из трёх способов, и вердикт называет
# каждый отдельно — свернуть любой в соседний значит замолчать суиту:
#   ответ пришёл, утверждение прошло — проверка выполнилась и сказала «да»;
#   ответ пришёл, утверждение упало  — проверка выполнилась и сказала «нет» (FAILED);
#   ОТВЕТА НЕ БЫЛО                   — проверка не выполнилась вовсе (UNANSWERED).
# Плюс вырожденный случай — отчёт вообще без утверждений: печатается как MUTE, а не
# читается как «ничего не упало». UNANSWERED никогда не вычитается и не объясняется.
#
# Возвращает 1, если у любого stem: нет out/<stem>.json (MISSING), assertions.failed>0,
# requests.failed>0 (UNANSWERED), assertions.total==0 (MUTE) или rc!=0. Эталон формы —
# services/iam/tests/newman/scripts/assert-suites-green.sh.
aggregate_verdict() {
  local out_dir="$1"; shift
  local bad=0 stem json rcfile rc total failed requests unanswered
  local n_total=$# reported=0 t_req=0 t_ass=0 t_fail=0 t_unans=0 t_mute=0
  printf "%-22s %10s %10s %10s %12s %8s\n" "RESOURCE" "ASSERT" "FAILED" "REQUESTS" "UNANSWERED" "RC"
  for stem in "$@"; do
    json="${out_dir}/${stem}.json"
    rcfile="${out_dir}/${stem}.rc"
    rc="n/a"
    [[ -f "$rcfile" ]] && rc="$(cat "$rcfile")"
    if [[ ! -f "$json" ]]; then
      printf "%-22s %10s %10s %10s %12s %8s\n" "$stem" "-" "-" "-" "-" "MISSING"
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
    printf "%-22s %10s %10s %10s %12s %8s\n" "$stem" "$total" "$failed" "$requests" "$unanswered" "$rc"
    reported=$((reported + 1))
    t_req=$((t_req + requests)); t_ass=$((t_ass + total))
    t_fail=$((t_fail + failed)); t_unans=$((t_unans + unanswered))
    if [[ "$failed" -gt 0 ]]; then bad=1; fi
    if [[ "$rc" != "0" ]];    then bad=1; fi
    # UNANSWERED — запросы, на которые НЕ пришёл ответ (DNS, отказ в соединении, TLS,
    # таймаут). Счётчика мало: назови их поимённо, иначе по числу нечего делать.
    if [[ "$unanswered" -gt 0 ]]; then
      echo "  ^ ${stem}: ${unanswered} запрос(ов) без ответа — проверка НЕ выполнилась:" >&2
      jq -r '[.run.failures[]? | select((.error.name? // "") != "AssertionError")
              | "      НЕ ВЫПОЛНЕН: \(.source.name? // "?") <- \(.error.message? // "нет ответа")"]
             | unique | .[]' "$json" 2>/dev/null || true
      bad=1
    fi
    # Отчёт без единого утверждения — не чистая суита, а суита, которой не было: до
    # этой строки она приходила сюда неотличимой от идеальной (0 упало, 0 без ответа).
    if [[ "$total" -eq 0 ]]; then
      echo "  ^ ${stem}: MUTE — 0 утверждений; суита, которая ничего не спросила, не может быть зелёной" >&2
      t_mute=$((t_mute + 1))
      bad=1
    fi
  done
  # Вердикт в числах одной строкой. Первая пара читается ПЕРВОЙ: при оборванном прогоне
  # все остальные счётчики выглядят здоровыми.
  echo "TOTAL: ${reported}/${n_total} коллекц. с отчётом, ${t_req} запрос(ов), ${t_ass} утвержд., ${t_fail} упало, ${t_unans} без ответа, ${t_mute} немых отчёт(ов)"
  return "$bad"
}

mkdir -p out
# Свежий прогон: убираем артефакты прошлого, чтобы stale-json не подменял коллекцию,
# которая в этот раз не выполнилась. Точечно (не `rm -rf out`), чтобы уже открытый
# newman-parallel.sh'ем out/suite.log не потерял свой файл.
rm -f out/*.json out/*.cli out/*.rc out/summary.txt 2>/dev/null || true

# stems — что покрывает вердикт. Явный набор суиты + любая сгенерированная коллекция
# вне его (drift-guard: новая коллекция не должна молча пропускаться — иначе
# assert-suites-green.sh видит её как "(no-report)").
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
    echo "[drift] ${extra_stem} — сгенерирована, но нет в COLLECTIONS; прогоняем"
    stems+=("$extra_stem")
  done
  for res in "${stems[@]}"; do
    run_one "$res"
  done
fi

echo
echo "===== Summary ====="
# pipefail прокидывает ненулевой вердикт сквозь tee — печать сводки сохранена.
if aggregate_verdict "out" "${stems[@]}" | tee out/summary.txt; then
  echo "OK: все compute-коллекции зелёные."
else
  echo "FAIL: одна или несколько compute-коллекций провалены / отсутствуют (см. таблицу выше)." >&2
  exit 1
fi
