#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# tests/newman/scripts/run.sh — прогон newman-коллекций kacho-geo с честным
# exit-кодом и защитой от false-green.
#
# Usage:
#   ./scripts/run.sh                          # все коллекции, сводный отчёт
#   ./scripts/run.sh --service region         # одна коллекция
#   ./scripts/run.sh --service region --bail   # прерывать после первого fail
#   ./scripts/run.sh --delay 100              # задержка между запросами (ms)
#   ./scripts/run.sh --jobs 2                 # cap параллельных коллекций (default 4)
#   ./scripts/run.sh --env-var baseUrl=http://localhost:18080  # проброс в newman
#
# Набор коллекций = объединение source-of-truth cases/*.py (gen.py делает 1:1
# коллекцию на каждый case-файл) и реально присутствующих collections/*.json. Так
# ни одна коллекция не пропускается молча, а отсутствие ожидаемой (cases/<x>.py
# есть, collections/<x>.json нет) фиксируется как MISSING и валит прогон
# (false-green guard).
#
# --jobs НЕ пробрасывается в `newman run` (иначе `unknown option '--jobs'` →
# коллекции без отчёта → ложный no-report/false-green, инцидент compute run.sh) —
# он используется только как cap параллельного пула коллекций.
#
# Exit-код: 0 только если у КАЖДОЙ коллекции assertions.failed==0, requests.failed==0
# (ни одного запроса без ответа), assertions.total>0 (суита не немая), rc newman==0 и
# коллекция присутствует. Любой провал/краш/таймаут/отсутствие → exit 1. Вердикт
# печатается ЧИСЛАМИ: коллекций с отчётом из скольких, запросов, утверждений, упавших
# утверждений, запросов без ответа, немых отчётов.
#
# Outputs:
#   out/<service>.json — newman JSON reporter (для агрегации)
#   out/<service>.cli  — newman cli-вывод
#   out/<service>.rc   — exit-код newman конкретной коллекции
#   out/summary.txt    — итоговая сводка

NEWMAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# expected_stems — ожидаемый набор коллекций: basename каждого cases/*.py.
expected_stems() {
  local f stem
  for f in "$NEWMAN_DIR"/cases/*.py; do
    [[ -e "$f" ]] || continue
    stem="$(basename "$f" .py)"
    case "$stem" in __init__|__main__) continue ;; esac
    printf '%s\n' "$stem"
  done
}

# present_stems — фактически сгенерированные коллекции collections/*.json.
present_stems() {
  local f stem
  for f in "$NEWMAN_DIR"/collections/*.postman_collection.json; do
    [[ -e "$f" ]] || continue
    stem="$(basename "$f" .postman_collection.json)"
    printf '%s\n' "$stem"
  done
}

# run_one — прогон одной коллекции. Пишет out/<svc>.json|.cli|.rc.
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
  # фоновый сабшелл до фиксации exit-кода. Берём PIPESTATUS[0] (newman), не tee.
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
      echo "  ^ ${stem}: ${unanswered} запрос(ов) без ответа — проверка НЕ выполнилась:" >&2
      jq -r '[.run.failures[]? | select((.error.name? // "") != "AssertionError")
              | "      НЕ ВЫПОЛНЕН: \(.source.name? // "?") <- \(.error.message? // "нет ответа")"]
             | unique | .[]' "$json" 2>/dev/null || true
      bad=1
    fi
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
      # --jobs: cap параллельного пула. Consume-and-ignore для `newman run` (НЕ
      # пробрасывать — иначе unknown option → no-report → false-green).
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
  # выпавшую коллекцию (false-green guard).
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

  # serial-collections.txt (optional, one stem per line, '#'-comments ok): коллекции,
  # которые НЕЛЬЗЯ гонять одновременно с остальными, потому что мутируют GLOBAL
  # backend-состояние, не изолируемое {{runId}}-суффиксом. Для geo это САМ КАТАЛОГ:
  # Region/Zone — cluster-singleton'ы (нет projectId, нет per-object listauthz), у них
  # ОДНО глобальное id-пространство и один общий List на весь стенд. runId-суффикс
  # разводит писателей по id, но НЕ изолирует общий список: читающие коллекции
  # (region/zone/authz-deny) делают "List → pick → Get", и строка, выбранная на List,
  # может быть удалена конкурентным писателем до Get (404 → undefined-field asserts),
  # а `?pageSize=100`-кейсы (REG-LST-CONF-NO-RAW-STATUS и зональный аналог) итерируют
  # по КАЖДОМУ элементу каталога, включая полу-видимые чужие qa-строки. Фильтр
  # `find(!id.startsWith('qa')) || [0]` в region/zone — частичное смягчение (предпочесть
  # стабильную seed-строку) с fallback'ом на [0], а не граница изоляции.
  # Поэтому catalog-мутирующие коллекции гоняются ПОСЛЕ параллельного пула, строго по
  # одной. Файла нет → поведение прежнее (весь набор параллельно).
  local -a serial_list=()
  if [[ -f "$NEWMAN_DIR/serial-collections.txt" ]]; then
    local line
    while IFS= read -r line; do
      line="${line%%#*}"; line="${line//[[:space:]]/}"
      [[ -n "$line" ]] && serial_list+=("$line")
    done < "$NEWMAN_DIR/serial-collections.txt"
  fi
  _is_serial() { local x; for x in "${serial_list[@]:-}"; do [[ "$x" == "$1" ]] && return 0; done; return 1; }

  # Параллельный прогон с cap=$JOBS: все коллекции, КРОМЕ serial-listed (эти —
  # deferred, см. ниже). Явный `--service <stem>` — escape hatch: одиночный прогон ни
  # с кем не конкурирует, поэтому идёт на переднем плане без deferral.
  local svc
  local -a deferred=()
  for svc in "${stems[@]}"; do
    if [[ -z "${SERVICE:-}" ]]; then
      if _is_serial "$svc"; then deferred+=("$svc"); continue; fi
      while [[ "$(jobs -rp | wc -l)" -ge "$JOBS" ]]; do wait -n; done
      run_one "$svc" &
    else
      run_one "$svc"
    fi
  done
  wait
  # serial-listed — строго по одной (не конкурируют ни между собой, ни с пулом).
  # Вердикт ниже строится по "${stems[@]}" (полный набор), поэтому deferred-коллекции
  # остаются в таблице и false-green guard на них продолжает действовать.
  for svc in "${deferred[@]:-}"; do
    [[ -n "$svc" ]] || continue
    run_one "$svc"
  done

  echo
  echo "===== Summary ====="
  if aggregate_verdict "out" "${stems[@]}" | tee out/summary.txt; then
    echo "OK: все коллекции зелёные."
  else
    echo "FAIL: одна или несколько коллекций провалены / отсутствуют (см. таблицу выше)." >&2
    exit 1
  fi
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  main "$@"
fi
