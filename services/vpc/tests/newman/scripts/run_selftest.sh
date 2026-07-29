#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# tests/newman/scripts/run_selftest.sh — самопроверка честности агрегатора run.sh.
#
# Доказывает: коллекция с проваленным ассертом (assertions.failed>0), ненулевым
# exit-кодом newman или отсутствующая коллекция ОБЯЗАНА давать ненулевой вердикт.
# Гоняет реальную функцию aggregate_verdict из run.sh на синтетических
# newman-JSON — без установленного newman и без стенда.
#
# Дополнительно показывает RED→GREEN: прежнее поведение (печать сводки без
# exit-кода) маскировало провал (вердикт 0), новое — ловит его (вердикт 1).

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=run.sh
source "$HERE/run.sh"   # main() не запускается (guard по BASH_SOURCE)

command -v jq >/dev/null 2>&1 || { echo "SELFTEST SKIP: нет jq"; exit 0; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Синтетический newman JSON-reporter с заданным числом проваленных ассертов.
mk_json() { # <stem> <failed>
  local stem="$1" failed="$2"
  printf '{"run":{"stats":{"assertions":{"total":10,"failed":%s},"requests":{"total":7,"failed":0}}}}\n' \
    "$failed" > "$TMP/${stem}.json"
}

# mk_json_full — отчёт с произвольными числами по всем четырём измерениям.
mk_json_full() { # <stem> <assert_total> <assert_failed> <req_total> <req_unanswered>
  local stem="$1"
  printf '{"run":{"stats":{"assertions":{"total":%s,"failed":%s},"requests":{"total":%s,"failed":%s}}}}\n' \
    "$2" "$3" "$4" "$5" > "$TMP/${stem}.json"
}

# legacy_aggregate_verdict — реконструкция ПРЕЖНЕГО вердикта: он читал только
# assertions.failed и exit-код newman, а колонки ASSERT/REQUESTS печатал, но не
# оценивал. Нужна только для контраста RED→GREEN на двух измерениях, которых у
# вердикта не было: «ничего не выполнилось» и «ответа не пришло».
legacy_aggregate_verdict() { # <out_dir> <stem>...
  local out_dir="$1"; shift
  local bad=0 stem json rc failed
  for stem in "$@"; do
    json="${out_dir}/${stem}.json"
    [[ -f "$json" ]] || { bad=1; continue; }
    failed="$(jq -r '.run.stats.assertions.failed' "$json" 2>/dev/null || echo 0)"
    rc="$(cat "${out_dir}/${stem}.rc" 2>/dev/null || echo n/a)"
    [[ "$failed" -gt 0 ]] && bad=1
    [[ "$rc" == "0" ]]    || bad=1
  done
  return "$bad"
}

# Реконструкция ПРЕЖНЕГО хвоста run.sh: печатает сводку и всегда возвращает 0
# (именно так провал маскировался). Нужна только для контраста RED→GREEN.
legacy_print_summary() { # <out_dir> <stem>...
  local out_dir="$1"; shift
  local stem f
  for stem in "$@"; do
    f="${out_dir}/${stem}.json"
    [[ -f "$f" ]] || continue
    jq -r '"\(.run.stats.assertions.failed) failed"' "$f" >/dev/null 2>&1 || true
  done
  return 0
}

fails=0
expect() { # <desc> <want-rc> <got-rc>
  if [[ "$2" == "$3" ]]; then
    printf 'ok   — %s (rc=%s)\n' "$1" "$3"
  else
    printf 'FAIL — %s (ожидался rc=%s, получили rc=%s)\n' "$1" "$2" "$3"
    fails=1
  fi
}

# Фикстуры.
mk_json green 0; echo 0 > "$TMP/green.rc"
mk_json bad   3; echo 0 > "$TMP/bad.rc"
mk_json crash 0; echo 1 > "$TMP/crash.rc"

echo "--- RED→GREEN на провальной коллекции (failed=3) ---"
legacy_print_summary "$TMP" bad; rc=$?
expect "RED: прежний summary-only маскирует провал (verdict=0)" 0 "$rc"
aggregate_verdict "$TMP" bad >/dev/null; rc=$?
expect "GREEN: новый aggregate_verdict ловит провал (verdict=1)" 1 "$rc"

echo "--- свойства вердикта ---"
aggregate_verdict "$TMP" green >/dev/null; rc=$?
expect "all-green → 0" 0 "$rc"
aggregate_verdict "$TMP" crash >/dev/null; rc=$?
expect "ненулевой exit newman → 1" 1 "$rc"
aggregate_verdict "$TMP" absent >/dev/null; rc=$?
expect "отсутствующая коллекция → 1" 1 "$rc"
aggregate_verdict "$TMP" green bad >/dev/null; rc=$?
expect "mixed green+bad → 1" 1 "$rc"

# --- вердикт по числам: «ничего не выполнилось» и «ответа не пришло» ---------
#
# Оба случая читаются как безупречный прогон, если смотреть только на число
# упавших утверждений: провалов ноль, потому что проверять было нечего. Именно
# так коллекция, не выполнившая ни одного запроса, засчитывалась зелёной.
mk_json_full nothing 0 0 0 0;      echo 0 > "$TMP/nothing.rc"
mk_json_full unanswered 4 0 9 9;   echo 0 > "$TMP/unanswered.rc"
mk_json_full partial 6 0 9 3;      echo 0 > "$TMP/partial.rc"

echo "--- RED→GREEN: отчёт без запросов ---"
legacy_aggregate_verdict "$TMP" nothing; rc=$?
expect "RED: прежний вердикт засчитывал пустой отчёт зелёным" 0 "$rc"
aggregate_verdict "$TMP" nothing >/dev/null; rc=$?
expect "GREEN: отчёт без утверждений валит вердикт (NOTHING RAN)" 1 "$rc"

echo "--- RED→GREEN: запросы без ответа ---"
legacy_aggregate_verdict "$TMP" unanswered; rc=$?
expect "RED: прежний вердикт не видел неотвеченных запросов" 0 "$rc"
aggregate_verdict "$TMP" unanswered >/dev/null; rc=$?
expect "GREEN: неотвеченные запросы валят вердикт" 1 "$rc"
aggregate_verdict "$TMP" partial >/dev/null; rc=$?
expect "GREEN: часть без ответа — тоже провал (не вычитается)" 1 "$rc"

echo "--- контроль: настоящий зелёный прогон остаётся зелёным ---"
# Без этого случая вердикт мог бы просто всегда падать — «ловит форму, а не
# существо». Отчёт с выполненными запросами, полученными ответами и нулём
# провалов обязан проходить.
mk_json_full honest 10 0 7 0; echo 0 > "$TMP/honest.rc"
aggregate_verdict "$TMP" honest >/dev/null; rc=$?
expect "полный отчёт без провалов и без неотвеченных → 0" 0 "$rc"

echo "--- сводка называет числа, а не слово ---"
summary="$(aggregate_verdict "$TMP" honest nothing 2>&1 || true)"
case "$summary" in
  *"TOTAL: 2/2 коллекций отчиталось"*) printf 'ok   — сводка называет, сколько коллекций отчиталось из скольких\n' ;;
  *) printf 'FAIL — сводка не называет числа:\n%s\n' "$summary"; fails=1 ;;
esac
case "$summary" in
  *"NOTHING RAN"*) printf 'ok   — «ничего не выполнилось» названо отдельно от «ничего не упало»\n' ;;
  *) printf 'FAIL — пустой отчёт не отмечен как NOTHING RAN:\n%s\n' "$summary"; fails=1 ;;
esac

echo
if [[ "$fails" == 0 ]]; then
  echo "SELFTEST: PASS"
  exit 0
else
  echo "SELFTEST: FAIL"
  exit 1
fi
