#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# assert-verdict-aggregators-honest.sh — доказать ИНЪЕКЦИЕЙ, что вердикт каждой
# newman-суиты берётся из ЧИСЕЛ отчёта, а не из факта завершения команды.
#
# ЗАЧЕМ ОН ЕСТЬ, и почему одного экземпляра было мало.
# Класс «прогонщик печатает GREEN при упавших проверках» в этом дереве уже ловили
# и чинили: `| tee … || true` глотал код возврата newman, и суита nlb печатала
# GREEN с 94 упавшими утверждениями. Починка разошлась по восьми агрегаторам —
# у каждой суиты своя копия `aggregate_verdict`, и все восемь сегодня читают
# `.run.stats.assertions.failed`.
#
# Но ДОКАЗАНА эта честность была ровно у ОДНОГО из восьми. Замер по дереву:
#   * 8 суит определяют `aggregate_verdict()` с одной и той же сигнатурой
#     `aggregate_verdict <out_dir> <stem…>` → 0/1;
#   * инъекционная самопроверка есть у 1 из 8 (services/vpc/…/run_selftest.sh);
#   * подгрузить функцию можно было у 3 из 8 — у остальных пяти нет guard'а
#     `BASH_SOURCE[0] == $0`, то есть попытка source запустила бы весь прогон.
# Семь агрегаторов держались на прочтении диффа. Починка распространяется ровно
# настолько, насколько хватает её гейта, — поэтому гейт здесь один на дерево.
#
# КАК ОН ДОКАЗЫВАЕТ. Не грепом по тексту (слова `assertions.failed` в этих файлах
# лежат в комментариях чаще, чем в коде: замер по восьми файлам — 1 вхождение в
# исполняемой части против 2 в комментариях), а ИСПОЛНЕНИЕМ: функция извлекается
# из реального файла, подгружается и запускается на синтетических отчётах.
#
# ПАРА В ОБЕ СТОРОНЫ. Пять отрицательных входов (упавшее утверждение, запрос без
# ответа, немой отчёт, отсутствующий отчёт, ненулевой код newman) обязаны дать
# ненулевой вердикт; ЗАКОННЫЙ чистый отчёт обязан дать нулевой. Без второй
# половины гейт зеленел бы на агрегаторе, который всегда говорит «плохо».
#
# ЧТО ОН НЕ ДЕЛАЕТ. Не прощает. Нет jq, не извлеклась функция, не нашлось ни
# одного агрегатора — это ОТКАЗ (код 2), а не пропуск: «не выполнилось» никогда
# не засчитывается «прошло», и именно этот класс он проверяет у других.
#
# Запуск:
#   deploy/scripts/assert-verdict-aggregators-honest.sh          # гейт
#   deploy/scripts/assert-verdict-aggregators-honest.sh --self-test  # доказать, что он ловит дефект

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
MODE="${1:-gate}"

if ! command -v jq >/dev/null 2>&1; then
  echo "ОТКАЗ: нет jq — доказать честность вердиктов НЕЧЕМ." >&2
  echo "  Это не пропуск: гейт, который сам себя засчитывает при отсутствии" >&2
  echo "  инструмента, — экземпляр того класса, ради которого он написан." >&2
  exit 2
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# ─── синтетические отчёты (форма — реальный newman json-reporter) ─────────────
mk_report() { # <dir> <stem> <ass_total> <ass_failed> <req_total> <req_unanswered> <newman_rc>
  local d="$1" stem="$2"
  printf '{"run":{"stats":{"assertions":{"total":%s,"failed":%s},"requests":{"total":%s,"failed":%s}},"failures":[]}}\n' \
    "$3" "$4" "$5" "$6" > "$d/${stem}.json"
  printf '%s\n' "$7" > "$d/${stem}.rc"
}

# ─── перечень агрегаторов — ВЫВОДИТСЯ из дерева, не выписывается ─────────────
# Рукописный перечень уже разъезжался с деревом в этом репозитории (три копии
# одного списка репозиториев, в одной не хватало сервиса). Здесь перечень —
# результат обхода, и объём осмотренного объявляется отдельным числом, чтобы
# «ноль находок» было отличимо от «ноль прочитанного».
#
# Признак прогонщика — ИСПОЛНЯЕМАЯ часть файла, не текст. Слова `newman run` и
# `assertions.failed` в этих файлах чаще лежат в комментариях, чем в коде (замер:
# 2 вхождения в комментариях против 1 в коде на файл), поэтому строки-комментарии
# снимаются до проверки. Гейт, читающий текст вместо кода, зеленел бы на
# комментарии, объясняющем защиту, которой уже нет.
_is_runner() { # <abs-path>
  local code; code="$(grep -vE '^[[:space:]]*#' "$1")"
  grep -qE 'newman[[:space:]]+run' <<< "$code" && return 0
  grep -qE '^aggregate_verdict\(\) \{' "$1" && return 0
  return 1
}
mapfile -t RUNNERS < <(
  cd "$ROOT" && git ls-files | grep -E '(^|/)tests/newman/scripts/.*\.sh$' | sort \
  | while IFS= read -r rel; do _is_runner "$ROOT/$rel" && echo "$rel"; done
)

if [ "${#RUNNERS[@]}" -eq 0 ]; then
  echo "ОТКАЗ: не найдено ни одного прогонщика суиты (*/tests/newman/scripts/run.sh)." >&2
  echo "  Осмотрено ноль файлов — это не 'всё чисто', это 'ничего не прочитано'." >&2
  exit 2
fi

echo "=== агрегаторы вердикта: инъекция в обе стороны ==="
echo "прогонщиков найдено: ${#RUNNERS[@]} (git ls-files, не перечень в тексте)"
echo

n_with_agg=0
n_extracted=0
n_ok=0
bad_files=()
no_agg_files=()

# Матрица инъекции. Первая строка — ЗАКОННЫЙ вход (положительный контроль):
# без неё гейт зеленел бы на агрегаторе, который отвергает всё подряд.
#          имя                       ass_total ass_failed req_total unanswered rc  ожидаемый_вердикт
CASES=(
  "чистый отчёт (контроль)|10|0|7|0|0|0"
  # Код newman здесь ЧИСТЫЙ намеренно. Прежняя редакция ставила rc=1 вместе с
  # упавшим утверждением, и тогда вход не различал «агрегатор читает
  # assertions.failed» и «агрегатор читает только код возврата»: слепой к числам
  # агрегатор проходил за счёт rc. Именно эта пара — числа красные, код чистый —
  # и есть форма исходного инцидента, где код возврата newman терялся в конвейере
  # (`| tee … || true`), а краснота оставалась только в отчёте.
  "упавшее утверждение при ЧИСТОМ коде newman|10|3|7|0|0|1"
  "запрос без ответа (UNANSWERED)|10|0|7|2|0|1"
  "немой отчёт (0 утверждений)|0|0|0|0|0|1"
  "ненулевой код newman при чистых числах|10|0|7|0|1|1"
)

# СИГНАТУРА ОПРЕДЕЛЯЕТСЯ, А НЕ ПРЕДПОЛАГАЕТСЯ.
# Первая редакция этого гейта звала все агрегаторы как `<out_dir> <stem…>` —
# форма, проверенная на восьми `run.sh`. Стоило расширить перечень до девятого
# файла с другой формой (`<путь-к-json>…`), и он объявил ЕГО дефектным: каталог
# читался как несуществующий отчёт, поэтому пять отрицательных входов из шести
# «прошли» — по неверной причине, а положительный контроль упал. Предикат,
# лгущий уверенно, опаснее отсутствующего, поэтому форма читается из текста
# функции, и обе формы обязаны пройти ОДИНАКОВУЮ инъекцию.
_agg_argform() { # <agg_file> → "dir" | "path"
  if grep -qE 'local out_dir="\$1"; *shift' "$1"; then echo dir; else echo path; fi
}

run_case() { # <agg_file> <case_line> ; печатает OK/BAD
  local agg="$1" line="$2"
  local IFS='|'; read -r name at af rt ru rc want <<< "$line"
  local d; d="$(mktemp -d "$TMP/inj.XXXXXX")"
  mk_report "$d" probe "$at" "$af" "$rt" "$ru" "$rc"
  local got args
  case "$(_agg_argform "$agg")" in
    dir)  args="'$d' probe" ;;
    path) args="'$d/probe.json'" ;;
  esac
  # Подоболочка: у извлечённой функции могут быть свои `local`-имена, и мы не
  # хотим, чтобы соседние прогоны видели друг друга.
  bash -c "source '$agg'; aggregate_verdict $args >/dev/null 2>&1; echo \$?" >"$d/verdict" 2>/dev/null
  got="$(cat "$d/verdict" 2>/dev/null || echo "?")"
  # Ненулевой вердикт может быть любым ненулём — важен исход, а не его значение.
  if [ "$want" = "0" ]; then
    [ "$got" = "0" ] && { echo "OK   $name → $got"; return 0; }
  else
    [ "$got" != "0" ] && [ "$got" != "?" ] && { echo "OK   $name → $got"; return 0; }
  fi
  echo "ПЛОХО $name → вердикт $got, ожидался $( [ "$want" = 0 ] && echo 'нулевой' || echo 'ненулевой')"
  return 1
}

missing_report_case() { # <agg_file>
  local agg="$1" d; d="$(mktemp -d "$TMP/inj.XXXXXX")"
  local got args
  case "$(_agg_argform "$agg")" in
    dir)  args="'$d' probe" ;;
    path) args="'$d/probe.json'" ;;
  esac
  bash -c "source '$agg'; aggregate_verdict $args >/dev/null 2>&1; echo \$?" >"$d/verdict" 2>/dev/null
  got="$(cat "$d/verdict" 2>/dev/null || echo "?")"
  if [ "$got" != "0" ] && [ "$got" != "?" ]; then
    echo "OK   отчёта нет вовсе (MISSING) → $got"; return 0
  fi
  echo "ПЛОХО отчёта нет вовсе (MISSING) → вердикт $got, ожидался ненулевой"
  return 1
}

check_runner() { # <path-relative-to-root>
  local rel="$1" abs="$ROOT/$1"
  if ! grep -qE '^aggregate_verdict\(\) \{' "$abs"; then
    no_agg_files+=("$rel")
    return 0
  fi
  n_with_agg=$((n_with_agg + 1))
  local agg="$TMP/agg.$(echo "$rel" | tr '/' '_')"
  awk '/^aggregate_verdict\(\) \{/{c=1} c{print} c&&/^\}$/{exit}' "$abs" > "$agg"
  # Извлечение могло НЕ состояться (функция переименована, закрывающая скобка не
  # в первой колонке). Без этой проверки гейт пошёл бы дальше по неопределённой
  # функции и напечатал бы ноль находок, ничего не проверив.
  if [ ! -s "$agg" ] || ! tail -n 1 "$agg" | grep -qE '^\}$'; then
    echo "--- $rel"
    echo "ПЛОХО не извлеклась функция aggregate_verdict — проверять нечего, и это отказ, а не пропуск"
    bad_files+=("$rel(не извлеклась)")
    return 1
  fi
  if ! bash -c "source '$agg'; declare -F aggregate_verdict >/dev/null"; then
    echo "--- $rel"
    echo "ПЛОХО извлечённый текст не подгружается — синтаксис функции изменился"
    bad_files+=("$rel(не подгрузилась)")
    return 1
  fi
  n_extracted=$((n_extracted + 1))

  echo "--- $rel"
  local ok=1 c
  for c in "${CASES[@]}"; do
    run_case "$agg" "$c" || ok=0
  done
  missing_report_case "$agg" || ok=0
  if [ "$ok" -eq 1 ]; then
    n_ok=$((n_ok + 1))
  else
    bad_files+=("$rel")
  fi
  echo
  return 0
}

# ─── режим --self-test: гейт обязан ПОЙМАТЬ внесённый дефект ────────────────
# Без этого гейт доказывает только то, что сегодня всё зелено, — и остаётся
# неотличим от гейта, который не умеет краснеть.
if [ "$MODE" = "--self-test" ]; then
  echo "=== доказательство: внесённый дефект ловится, законный близнец — молчит ==="
  # (а) нечестный агрегатор — всегда говорит «хорошо»
  cat > "$TMP/dishonest.sh" <<'EOF'
aggregate_verdict() {
  local out_dir="$1"; shift
  echo "все коллекции зелёные"
  return 0
}
EOF
  fail_caught=0
  for c in "${CASES[@]}"; do
    case "$c" in "чистый отчёт (контроль)"*) continue ;; esac
    run_case "$TMP/dishonest.sh" "$c" >/dev/null 2>&1 || fail_caught=$((fail_caught + 1))
  done
  missing_report_case "$TMP/dishonest.sh" >/dev/null 2>&1 || fail_caught=$((fail_caught + 1))
  echo "(а) нечестный агрегатор (return 0 всегда): пойман на $fail_caught из 5 отрицательных входов"

  # (б) законный близнец той же формы — обязан пройти ВСЕ входы, включая контроль
  cat > "$TMP/honest.sh" <<'EOF'
aggregate_verdict() {
  local out_dir="$1"; shift
  local bad=0 stem json rcfile rc total failed unanswered
  for stem in "$@"; do
    json="${out_dir}/${stem}.json"; rcfile="${out_dir}/${stem}.rc"
    if [ ! -f "$json" ]; then bad=1; continue; fi
    rc="$(cat "$rcfile" 2>/dev/null || echo n/a)"
    total=$(jq -r '.run.stats.assertions.total // 0' "$json")
    failed=$(jq -r '.run.stats.assertions.failed // 0' "$json")
    unanswered=$(jq -r '.run.stats.requests.failed // 0' "$json")
    [ "$failed" -gt 0 ] && bad=1
    [ "$unanswered" -gt 0 ] && bad=1
    [ "$total" -eq 0 ] && bad=1
    [ "$rc" != "0" ] && bad=1
  done
  return "$bad"
}
EOF
  twin_bad=0
  for c in "${CASES[@]}"; do
    run_case "$TMP/honest.sh" "$c" >/dev/null 2>&1 || twin_bad=$((twin_bad + 1))
  done
  missing_report_case "$TMP/honest.sh" >/dev/null 2>&1 || twin_bad=$((twin_bad + 1))
  echo "(б) законный агрегатор той же формы: не прошёл $twin_bad из 6 входов (ожидается 0)"
  echo
  if [ "$fail_caught" -eq 5 ] && [ "$twin_bad" -eq 0 ]; then
    echo "ДОКАЗАНО: гейт краснеет на внесённом дефекте и молчит на законной конструкции той же формы."
    exit 0
  fi
  echo "ГЕЙТ НЕГОДЕН: инъекция не подтвердила контроль в обе стороны." >&2
  exit 1
fi

for rel in "${RUNNERS[@]}"; do
  check_runner "$rel"
done

echo "===== ИТОГ ====="
echo "прогонщиков осмотрено: ${#RUNNERS[@]}"
echo "из них с агрегатором aggregate_verdict: $n_with_agg"
echo "агрегаторов извлечено и исполнено: $n_extracted"
echo "прошли инъекцию (6 входов: 1 законный + 5 отрицательных): $n_ok"

if [ "${#no_agg_files[@]}" -gt 0 ]; then
  echo
  echo "БЕЗ АГРЕГАТОРА — вердикт этих прогонщиков берётся откуда-то ещё, и здесь он НЕ доказан:"
  for f in "${no_agg_files[@]}"; do echo "    $f"; done
  echo "  Это не разрешение: это перечень того, что данный гейт не покрывает."
fi

if [ "$n_extracted" -eq 0 ]; then
  echo
  echo "ОТКАЗ: не исполнено НИ ОДНОГО агрегатора — прочитано ноль, а не найдено ноль." >&2
  exit 2
fi

if [ "${#bad_files[@]}" -gt 0 ]; then
  echo
  echo "ПРОВАЛ: вердикт не доказан у: ${bad_files[*]}" >&2
  echo "  Агрегатор обязан отвечать ненулём на упавшее утверждение, запрос без ответа," >&2
  echo "  немой отчёт, отсутствующий отчёт и ненулевой код newman — и нулём на чистом." >&2
  exit 1
fi

# ─── ВТОРАЯ ПОЛОВИНА: вердикт обязан ДОЙТИ ДО КОДА ВОЗВРАТА ─────────────────
#
# Честный агрегатор ничего не стоит, если его ответ теряется по дороге. Так и
# выглядел исходный инцидент: числа в отчёте красные, а прогонщик печатает
# GREEN, потому что код возврата умер в конвейере.
#
# Все агрегаторы этого дерева зовутся ОДИНАКОВО — через конвейер:
#     if aggregate_verdict "out" "${stems[@]}" | tee out/summary.txt; then
# В bash статус конвейера — статус ПОСЛЕДНЕЙ команды, то есть `tee`, а он
# практически всегда нулевой. Ответ агрегатора доходит до `if` ТОЛЬКО при
# включённом pipefail. Сегодня он включён у всех — и это не держалось ничем:
# снятие защиты не роняло в дереве ни одной проверки (гейт класса
# internal/repohygiene/pipefailguard_test.go разбирает шаги workflow'ов, а не
# прогонщики суит; проверено снятием — осталось зелено). Свойство, истинное по
# совпадению, — не свойство.
echo
echo "=== вердикт доходит до кода возврата (конвейер + pipefail) ==="
piped_total=0
piped_bad=()
for rel in "${RUNNERS[@]}"; do
  abs="$ROOT/$rel"
  # только ИСПОЛНЯЕМЫЕ строки: слово aggregate_verdict встречается и в прозе
  code="$(grep -vE '^[[:space:]]*#' "$abs")"
  if ! grep -qE 'aggregate_verdict[^|]*\|' <<< "$code"; then
    continue
  fi
  piped_total=$((piped_total + 1))
  if ! grep -qE '^[[:space:]]*set[[:space:]]+-[a-zA-Z]*o?[[:space:]]*.*pipefail' <<< "$code"; then
    piped_bad+=("$rel")
  fi
done
echo "прогонщиков, отправляющих вердикт в конвейер: $piped_total"
if [ "$piped_total" -eq 0 ]; then
  echo "ОТКАЗ: ни одного конвейерного вызова не найдено — предпосылка этой проверки" >&2
  echo "  изменилась (форму вызова переписали). Молчание здесь было бы ложным:" >&2
  echo "  пересмотри признак, а не снимай проверку." >&2
  exit 2
fi
if [ "${#piped_bad[@]}" -gt 0 ]; then
  echo
  echo "ПРОВАЛ: вердикт уходит в конвейер БЕЗ pipefail у: ${piped_bad[*]}" >&2
  echo "  Статус конвейера — статус tee, а не агрегатора: прогонщик напечатает GREEN" >&2
  echo "  при упавших утверждениях. Это ровно тот инцидент, ради которого написан весь" >&2
  echo "  этот гейт. Чинится строкой 'set -o pipefail' в прогонщике." >&2
  exit 1
fi
echo "у всех $piped_total включён pipefail — ответ агрегатора доходит до 'if'"

echo
echo "Все $n_ok агрегатора(ов) выносят вердикт из чисел отчёта — доказано исполнением."
