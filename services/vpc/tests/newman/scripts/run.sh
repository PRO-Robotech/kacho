#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# tests/newman/scripts/run.sh — прогон newman-коллекций с честным exit-кодом.
#
# Usage:
#   ./scripts/run.sh                          # все коллекции, сводный отчет
#   ./scripts/run.sh --service network        # одна коллекция
#   ./scripts/run.sh --service network --bail # прерывать после первого fail
#   ./scripts/run.sh --delay 100              # задержка между запросами (ms)
#   ./scripts/run.sh --jobs 2                 # макс. параллельных коллекций (default 4)
#
# Набор коллекций определяется автоматически: объединение source-of-truth
# cases/*.py (gen.py делает 1:1 коллекцию на каждый case-файл) и реально
# присутствующих collections/*.postman_collection.json. Так ни одна коллекция
# не пропускается молча, а отсутствие ожидаемой (cases/<x>.py есть, а
# collections/<x>.json нет) фиксируется как MISSING и валит прогон.
#
# Per-service коллекции гоняются параллельно (cap --jobs, default 4): каждая
# коллекция изолирует свои ресурсы по {{runId}}-суффиксам внутри общего
# existingProjectId, так что параллельный прогон безопасен.
#
# Exit-код: 0 только если КАЖДАЯ коллекция отчиталась, её exit-код newman==0,
# assertions.failed==0, requests.failed==0 (ни одного запроса без ответа) и
# assertions.total>0 (что-то действительно выполнилось). Любой провал / краш /
# таймаут / отсутствие отчёта / «ничего не выполнилось» → exit 1. Сводка
# печатается всегда (out/summary.txt) и называет числа, а не слово.
#
# Outputs:
#   out/<service>.json — newman JSON reporter (для агрегации)
#   out/<service>.cli  — newman cli-вывод
#   out/<service>.rc   — exit-код newman конкретной коллекции
#   out/summary.txt    — итоговая сводка

# Каталог tests/newman (на уровень выше scripts/). Считается при загрузке файла,
# поэтому одинаково корректен и при прямом запуске, и при `source` из self-test.
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

# run_one — прогон одной коллекции. Пишет out/<svc>.json|.cli|.rc.
# Использует глобальные ENV/DELAY/BAIL/EXTRA, выставленные в main().
run_one() {
  local svc="$1"
  local col="collections/${svc}.postman_collection.json"
  if [[ ! -f "$col" ]]; then
    echo "[missing] ${svc} — нет коллекции ${col}"
    echo "missing" > "out/${svc}.rc"
    return 0
  fi
  echo "===== ${svc} ====="
  # Снимаем errexit вокруг пайпа, чтобы провал newman (через pipefail) не убил
  # фоновый сабшелл до того, как мы зафиксируем его реальный exit-код. Берем
  # именно PIPESTATUS[0] (newman), а НЕ статус tee — иначе провал маскируется.
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

# aggregate_verdict — чистая, тестируемая функция вердикта.
#   aggregate_verdict <out_dir> <stem>...
#
# ВЕРДИКТ ПО ЧИСЛАМ. У запроса три исхода, и раньше у этой функции были имена
# только для двух:
#   ответ пришёл, утверждение сказало «да»  — проверка прошла;
#   ответ пришёл, утверждение сказало «нет» — FAILED;
#   ОТВЕТА НЕ ПРИШЛО                        — UNANSWERED (проверка не выполнилась).
# Третьего исхода в вердикте не было вовсе: колонки ASSERT и REQUESTS печатались,
# но не оценивались, поэтому коллекция, не выполнившая НИ ОДНОГО запроса, читалась
# ровно как безупречная (0 провалов) и засчитывалась зелёной. То же и с прогоном,
# где все запросы остались без ответа: провалов ноль, потому что проверять было
# нечего.
#
# Поэтому теперь:
#   * UNANSWERED (`.run.stats.requests.failed`) — ОТДЕЛЬНАЯ колонка, и она НИКОГДА
#     не вычитается и не объясняется: «не смог достучаться» и «мне отказали» —
#     разные находки, и доказательством является только вторая;
#   * отчёт без утверждений печатается как NOTHING RAN и валит вердикт: суита,
#     которая ничего не утверждает, не может пройти;
#   * итоговая строка называет ЧИСЛА — сколько коллекций отчиталось из скольких
#     ожидавшихся, сколько запросов, сколько без ответа, сколько утверждений и
#     сколько упавших. «Ноль находок» обязано быть отличимо от «ноль прочитанного».
#
# Возвращает 1, если у любого stem: отсутствует out/<stem>.json (MISSING),
# assertions.failed>0, requests.failed>0, assertions.total==0 или rc!=0. Иначе 0.
aggregate_verdict() {
  local out_dir="$1"; shift
  local bad=0 stem json rcfile rc total failed requests unanswered note
  local reported=0 expected=$# sum_req=0 sum_unans=0 sum_assert=0 sum_failed=0
  printf "%-25s %10s %11s %10s %8s %6s  %s\n" \
    "COLLECTION" "REQUESTS" "UNANSWERED" "ASSERT" "FAILED" "RC" "NOTE"
  for stem in "$@"; do
    json="${out_dir}/${stem}.json"
    rcfile="${out_dir}/${stem}.rc"
    rc="n/a"
    [[ -f "$rcfile" ]] && rc="$(cat "$rcfile")"
    if [[ ! -f "$json" ]]; then
      printf "%-25s %10s %11s %10s %8s %6s  %s\n" "$stem" "-" "-" "-" "-" "MISSING" \
        "нет отчёта — коллекция не отработала"
      bad=1
      continue
    fi
    reported=$((reported + 1))
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
    # Отчёт без утверждений — не «чисто», а «ничего не выполнилось». Этот случай
    # читается как идеальный (0 провалов, 0 без ответа), поэтому назван отдельно.
    if [[ "$total" -eq 0 ]]; then
      note="NOTHING RAN — 0 утверждений; суита, которая ничего не утверждает, не проходит"
      bad=1
    fi
    if [[ "$unanswered" -gt 0 ]]; then
      note="${note:+$note; }UNANSWERED — запросы без ответа НЕ вычитаются"
      bad=1
    fi

    printf "%-25s %10s %11s %10s %8s %6s  %s\n" \
      "$stem" "$requests" "$unanswered" "$total" "$failed" "$rc" "$note"

    # Назвать то, что не ответило: счётчик сам по себе не действие.
    if [[ "$unanswered" -gt 0 ]]; then
      jq -r '[.run.failures[]? | select((.error.name? // "") != "AssertionError")
              | "    NOT EXECUTED: \(.source.name? // "?") <- \(.error.message? // "no response")"]
             | unique | .[]' "$json" 2>/dev/null || true
    fi

    sum_req=$((sum_req + requests))
    sum_unans=$((sum_unans + unanswered))
    sum_assert=$((sum_assert + total))
    sum_failed=$((sum_failed + failed))

    [[ "$failed" -gt 0 ]] && bad=1
    [[ "$rc" == "0" ]]    || bad=1
  done
  printf "TOTAL: %d/%d коллекций отчиталось — %d запрос(ов), %d без ответа, %d утверждени(й), %d упавших\n" \
    "$reported" "$expected" "$sum_req" "$sum_unans" "$sum_assert" "$sum_failed"
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
      --jobs)    JOBS="$2"; shift 2 ;;
      *)         EXTRA+=("$1"); shift ;;
    esac
  done

  ENV="environments/local.postman_environment.json"
  # Env-файл gitignore-ится (fixture-seed пишет в него живые токены), но НИКТО его
  # не создаёт: patch-env.py/setup.sh только ПАТЧАТ уже существующий и молча
  # пропускают отсутствующий. На свежем клоне суита падала здесь ещё до вызова
  # newman. Поэтому материализация — часть пути прогона, а не ручной шаг: копируем
  # закоммиченный шаблон, креды/id допишет fixture-seed. Существующий файл НЕ
  # перетираем — в нём может лежать живая сессия текущего прогона.
  if [[ ! -f "$ENV" && -f "${ENV%.json}.template.json" ]]; then
    cp "${ENV%.json}.template.json" "$ENV"
    echo "создан $ENV из ${ENV%.json}.template.json (креды допишет fixture-seed)" >&2
  fi
  [[ -f "$ENV" ]] || { echo "missing env: $ENV" >&2; exit 1; }

  mkdir -p out
  # Свежий прогон: убираем артефакты прошлого, чтобы stale-json не маскировал
  # выпавшую коллекцию (rm -f не падает на отсутствующих файлах).
  rm -f out/*.json out/*.cli out/*.rc out/summary.txt 2>/dev/null

  # Набор stems для прогона и для вердикта.
  local -a stems=()
  if [[ -n "$SERVICE" ]]; then
    stems=("$SERVICE")
  else
    local s
    while IFS= read -r s; do
      [[ -n "$s" ]] && stems+=("$s")
    done < <(newman_all_stems "$NEWMAN_DIR")
  fi

  # serial-collections.txt (optional, one stem per line, '#'-comments ok): коллекции,
  # которые НЕЛЬЗЯ гонять одновременно ДРУГ С ДРУГОМ, потому что делят
  # GLOBAL/semi-global backend-состояние, не изолируемое {{runId}}-суффиксом (напр.
  # EXTERNAL_PUBLIC AddressPool: CIDR-EXCLUDE `(kind, block)` глобален, а
  # is_default-partition `(zone_id, kind)` делится при zone-collapse — только 3 geo-зоны,
  # zoneC≡zoneD). Такие коллекции гоняются ПОСЛЕ параллельного пула, строго по одной.
  # Файла нет → поведение прежнее (весь набор параллельно). Прочие сервисы не затронуты.
  local -a serial_list=()
  if [[ -f "$NEWMAN_DIR/serial-collections.txt" ]]; then
    local line
    while IFS= read -r line; do
      line="${line%%#*}"; line="${line//[[:space:]]/}"
      [[ -n "$line" ]] && serial_list+=("$line")
    done < "$NEWMAN_DIR/serial-collections.txt"
  fi
  _is_serial() { local x; for x in "${serial_list[@]:-}"; do [[ "$x" == "$1" ]] && return 0; done; return 1; }

  # Параллельный прогон с cap=$JOBS: все коллекции, КРОМЕ serial-listed. Каждая
  # runId-scoped → safe.
  local svc
  local -a deferred=()
  for svc in "${stems[@]}"; do
    if [[ -n "${SERVICE:-}" ]]; then :; elif _is_serial "$svc"; then deferred+=("$svc"); continue; fi
    while [[ "$(jobs -rp | wc -l)" -ge "$JOBS" ]]; do wait -n; done
    run_one "$svc" &
  done
  wait
  # serial-listed коллекции — строго по одной (не конкурируют ни между собой, ни с пулом).
  for svc in "${deferred[@]:-}"; do
    [[ -n "$svc" ]] || continue
    run_one "$svc"
  done

  echo
  echo "===== Summary ====="
  # pipefail прокидывает ненулевой вердикт сквозь tee — печать сводки сохранена.
  if aggregate_verdict "out" "${stems[@]}" | tee out/summary.txt; then
    echo "OK: все коллекции зеленые."
  else
    echo "FAIL: одна или несколько коллекций провалены / отсутствуют (см. таблицу выше)." >&2
    exit 1
  fi
}

# main запускается только при прямом вызове; при `source` (self-test) — нет.
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  main "$@"
fi
