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
# Exit-код: 0 только если у КАЖДОЙ коллекции assertions.failed==0, rc newman==0 и
# коллекция присутствует. Любой провал/краш/таймаут/отсутствие → exit 1.
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

# aggregate_verdict — чистый вердикт. Возвращает 1, если у любого stem: отсутствует
# out/<stem>.json (MISSING), assertions.failed>0 или rc!=0.
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
    [[ "$failed" -gt 0 ]] && bad=1
    [[ "$rc" == "0" ]]    || bad=1
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
      # --jobs: cap параллельного пула. Consume-and-ignore для `newman run` (НЕ
      # пробрасывать — иначе unknown option → no-report → false-green).
      --jobs)    JOBS="$2"; shift 2 ;;
      *)         EXTRA+=("$1"); shift ;;
    esac
  done

  ENV="environments/local.postman_environment.json"
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

  # Параллельный прогон с cap=$JOBS. Каждая коллекция runId-scoped → safe (geo
  # каталог глобален, но кейсы адресуют СВОИ qa-*-{{runId}} ресурсы и негативы по
  # фиксированным absent id — общего мутабельного state между коллекциями нет).
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
    echo "OK: все коллекции зелёные."
  else
    echo "FAIL: одна или несколько коллекций провалены / отсутствуют (см. таблицу выше)." >&2
    exit 1
  fi
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  main "$@"
fi
