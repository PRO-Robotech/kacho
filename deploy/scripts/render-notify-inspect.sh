#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# render-notify-inspect.sh — КОПИЯ ОСМОТРА чарта notify (замысел З28 «Копия
# осмотра», R37-1; решения Д80, Д81).
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ
#
# Объекты чарта `deploy/helm/notify/` рендерятся только при непустом выведенном
# перечне источников (NTF1-N04), а таблица подключаемых источников в дереве до
# полосы D2 ПУСТА. Настоящий чарт поэтому рендерит ноль объектов в каждой
# цепочке, и гейт, утверждающий что-либо об объектах notify (тома, привязка к
# содержимому образа, IaC-скан), на нём не осмотрел бы ничего — его «ноль
# находок» значил бы «ноль прочитанного».
#
# Копия осмотра — каталог чарта во временном каталоге, в котором РОВНО ОДИН файл
# (`templates/_sources.tpl`) заменён фикстурой `deploy/testdata/notify-inspect/
# _sources.tpl` с таблицей из одной строки. Гейты судят объекты копии, а каталог
# чарта печатают отдельной строкой «перечень [], объектов 0 (Д76)».
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ОБЁРТКА УТВЕРЖДАЕТ, ПРЕЖДЕ ЧЕМ ОТДАТЬ КОПИЮ
#
#   1. таблица модулей каталога чарта ПУСТА. Ветку включает пустая таблица, а не
#      ноль объектов (Д80): непустая таблица — отказ «копия осмотра пережила
#      предмет» (код 1), копию снимает D2 тем же изменением, что наполняет
#      таблицу. Пустая таблица при непустом перечне или ненулевом числе объектов
#      каталога — тоже код 1: ветка Д80 к такому дереву неприменима;
#   2. копия отличается от каталога чарта РОВНО файлом `templates/_sources.tpl`,
#      иначе «НЕ ВЫПОЛНИЛОСЬ» (код 2) с перечнем различий;
#   3. множества имён `define` фикстуры и `_sources.tpl` чарта равны, иначе
#      красный (код 1) с обоими множествами — копия судила бы не тот чарт;
#   4. объектов копии ≥ 1, иначе «НЕ ВЫПОЛНИЛОСЬ: копия осмотра пуста» (код 2).
#
# Печатает путь копии, отпечаток фикстуры (sha256), разницу копии с каталогом,
# строку каталога чарта и число объектов копии.
#
# ─────────────────────────────────────────────────────────────────────────────
# ВЫЗОВ
#
#   render-notify-inspect.sh --table               число строк таблицы модулей
#                                                  каталога (одно число в stdout)
#   render-notify-inspect.sh --into <каталог>      построить копию в <каталог>/notify
#                                                  и проверить её (печать — stdout)
#   render-notify-inspect.sh --self-test           инъекции в обе стороны
#
#   --chart <каталог>  — каталог чарта вместо `deploy/helm/notify` (самопроверка и
#                        инъекции гейтов-потребителей).
#   --fixture <файл>   — фикстура таблицы вместо `deploy/testdata/notify-inspect/
#                        _sources.tpl` (самопроверка).
#
# Пути — от каталога `deploy/`, найденного по месту ЭТОГО файла: копия
# развёртывания, запущенная из временного каталога (самопроверки гейтов), судит
# свою копию, а не живое дерево.
#
# Ноги рендера — те же, что у проб чарта notify (deploy/notify_secret_layout_test.go):
# собственные значения ноги без зонтика и образец узла почты оператора.
#
# Исходы: 0 — копия построена и проверена; 1 — находка о дереве (в том числе
# «пережила предмет»); 2 — не выполнилось (рендер не прошёл, инструмента нет).
set -uo pipefail

SCRIPT="${0##*/}"
DEPLOY="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOP="$(cd "$DEPLOY/.." && pwd)"
CHART="$DEPLOY/helm/notify"
FIXTURE="$DEPLOY/testdata/notify-inspect/_sources.tpl"
LEG_VALUES=("$DEPLOY/testdata/notify-standalone/values.yaml" "$DEPLOY/testdata/mail-node/operator.yaml")
RELEASE="kacho-notify"

die2() { echo "НЕ ВЫПОЛНИЛОСЬ: $*" >&2; exit 2; }

# need_tools — КАЖДЫЙ внешний инструмент обёртки спрошен ДО первого шага.
#
# Нехватка инструмента внутри подстановки не роняет обёртку сама: `tr`,
# отсутствующий в `define_names … | tr …`, давал ПУСТЫЕ множества имён у фикстуры
# и у чарта, и сверка п. 3 «множества равны» проходила на двух пустых — то есть
# копия объявлялась проверенной, не прочитав ни одного имени (замер 2026-10-03 под
# PATH сканирующей пробы RG1.1: без `tr`, `sha256sum`, `cut` обёртка выходила
# кодом 0). Перечень ниже — все внешние команды файла; отсутствие любой —
# «не выполнилось» с её именем.
need_tools() {
  local t
  for t in helm python3 mktemp mkdir cp rm cat sha256sum cut tr head sed; do
    command -v "$t" >/dev/null 2>&1 || die2 "$t не в PATH — обёртке копии осмотра нечем исполнить свой шаг"
  done
  python3 -c 'import yaml' 2>/dev/null || die2 "python3 с модулем yaml недоступен — рендер разобрать нечем"
}

# count_objects <файл рендера> — число документов с полем kind.
count_objects() {
  python3 - "$1" <<'PY'
import sys, yaml
with open(sys.argv[1], encoding="utf-8") as fh:
    print(sum(1 for d in yaml.safe_load_all(fh) if isinstance(d, dict) and d.get("kind")))
PY
}

# define_names <файл> — отсортированные имена `define`, по одному в строке.
define_names() {
  python3 - "$1" <<'PY'
import re, sys
text = open(sys.argv[1], encoding="utf-8").read()
for n in sorted(set(re.findall(r'\{\{-?\s*define\s+"([^"]+)"', text))):
    print(n)
PY
}

# table_and_roster <каталог чарта> — «<строк таблицы> <перечень JSON>» каталога.
#
# Помощники вычисляются САМИМ helm на чарте из одного файла `_sources.tpl`
# каталога и одного шаблона-вопроса: разбор текста таблицы ответил бы про
# сегодняшнюю форму записи, а не про то, что таблица ДАЁТ. Вход помощников — тот
# же, что у чарта (`dict "global" .Values.global`), значения — значения чарта и
# ноги.
table_and_roster() {
  local chart="$1" q out rc
  q="$(mktemp -d)"
  mkdir -p "$q/templates"
  printf 'apiVersion: v2\nname: notify-table-question\nversion: 0.0.0\n' >"$q/Chart.yaml"
  cp "$chart/templates/_sources.tpl" "$q/templates/_sources.tpl" || { rm -rf "$q"; die2 "нет $chart/templates/_sources.tpl"; }
  cat >"$q/templates/question.yaml" <<'TPL'
apiVersion: v1
kind: ConfigMap
metadata:
  name: notify-table-question
data:
  table: {{ include "notify.pluggableSources" (dict "global" .Values.global) | quote }}
  roster: {{ include "notify.sourceRoster" (dict "global" .Values.global) | quote }}
TPL
  out="$(helm template q "$q" -f "$chart/values.yaml" -f "${LEG_VALUES[0]}" -f "${LEG_VALUES[1]}" 2>&1)"; rc=$?
  rm -rf "$q"
  [ "$rc" -eq 0 ] || die2 "помощники таблицы каталога $chart не вычислились: $(printf '%s' "$out" | head -3)"
  printf '%s' "$out" | python3 -c '
import json, sys, yaml
doc = next(d for d in yaml.safe_load_all(sys.stdin) if isinstance(d, dict) and d.get("kind"))
t = json.loads(doc["data"]["table"]); r = json.loads(doc["data"]["roster"])
print(len(t), json.dumps(r, separators=(",", ":")))
' || die2 "ответ помощников таблицы каталога $chart не разобран"
}

# only_sources_differs <каталог чарта> <копия> — 0, если копия отличается от
# каталога РОВНО файлом templates/_sources.tpl; иначе 1. Различия — в stdout.
#
# Различия считаются ОТНОСИТЕЛЬНЫМИ путями обхода обоих каталогов, а не сверкой
# строки вывода `diff -rq` с ожидаемой: `diff` заключает путь с пробелом в
# кавычки, и строка «ровно один файл» не совпадала на каталоге копии с пробелом в
# пути — копия, отличающаяся ровно таблицей, объявлялась отличающейся не тем
# (замер 2026-10-03: TMPDIR `scratch with spaces` сканирующей пробы RG1.1).
only_sources_differs() {
  python3 - "$1" "$2" <<'PY'
import filecmp, os, sys
a, b = sys.argv[1], sys.argv[2]
def files(root):
    out = set()
    for d, _, fs in os.walk(root):
        for f in fs:
            out.add(os.path.relpath(os.path.join(d, f), root))
    return out
fa, fb = files(a), files(b)
diffs = sorted("только в каталоге: " + p for p in fa - fb) \
    + sorted("только в копии: " + p for p in fb - fa) \
    + sorted("различается: " + p for p in fa & fb
             if not filecmp.cmp(os.path.join(a, p), os.path.join(b, p), shallow=False))
print("\n".join(diffs))
sys.exit(0 if diffs == ["различается: templates/_sources.tpl"] else 1)
PY
}

render_chart() { # <каталог чарта> <куда> → код helm
  helm template "$RELEASE" "$1" -n kacho -f "${LEG_VALUES[0]}" -f "${LEG_VALUES[1]}" >"$2" 2>"$2.err"
}

cmd_table() {
  need_tools
  local tr
  tr="$(table_and_roster "$CHART")" || exit 2
  printf '%s\n' "${tr%% *}"
}

cmd_into() {
  local into="$1" tr table roster cat_out cat_n copy diffs fx_names ch_names copy_n
  need_tools
  [ -f "$FIXTURE" ] || die2 "фикстуры копии нет: $FIXTURE"
  [ -d "$CHART/templates" ] || die2 "каталога чарта нет: $CHART"
  mkdir -p "$into" || die2 "каталог копии $into не создан"

  tr="$(table_and_roster "$CHART")" || exit 2
  table="${tr%% *}"; roster="${tr#* }"
  cat_out="$into/catalog.yaml"
  render_chart "$CHART" "$cat_out" || die2 "рендер каталога чарта $CHART отказал: $(head -3 "$cat_out.err")"
  cat_n="$(count_objects "$cat_out")" || die2 "рендер каталога не разобран"
  echo "${CHART#"$TOP/"}/: перечень $roster, объектов $cat_n (Д76); строк таблицы модулей $table"

  if [ "$table" -ne 0 ]; then
    echo "ОТКАЗ: копия осмотра пережила предмет — таблица модулей каталога чарта непуста ($table строк, перечень $roster):"
    echo "       объекты notify рендерит сам чарт, и гейты обязаны судить его, а не копию (Д80, Д81)."
    echo "       Копию и фикстуру $FIXTURE снимает изменение, наполнившее таблицу (D2)."
    exit 1
  fi
  if [ "$roster" != "[]" ] || [ "$cat_n" -ne 0 ]; then
    echo "КРАСНЫЙ: таблица модулей пуста, а перечень каталога $roster, объектов $cat_n —"
    echo "         ветка Д80 к этому дереву неприменима: перечень выводится не из таблицы."
    exit 1
  fi

  fx_names="$(define_names "$FIXTURE" | tr '\n' ' ')" || die2 "имена define фикстуры не прочитаны"
  ch_names="$(define_names "$CHART/templates/_sources.tpl" | tr '\n' ' ')" || die2 "имена define чарта не прочитаны"
  # Два ПУСТЫХ множества равны — и сверка ниже прошла бы, не прочитав ни имени.
  # У фикстуры define есть by construction (она подменяет таблицу помощников).
  [ -n "${fx_names// /}" ] || die2 "у фикстуры $FIXTURE не прочитано ни одного имени define — сверять множества не с чем"
  if [ "$fx_names" != "$ch_names" ]; then
    echo "КРАСНЫЙ: имена define фикстуры и чарта различаются — копия судила бы не тот чарт:"
    echo "         фикстура: ${fx_names:-—}"
    echo "         чарт:     ${ch_names:-—}"
    exit 1
  fi

  copy="$into/notify"
  rm -rf "$copy"
  cp -r --preserve=timestamps "$CHART" "$copy" || die2 "копия каталога чарта не снята"
  cp "$FIXTURE" "$copy/templates/_sources.tpl" || die2 "фикстура в копию не легла"
  if ! diffs="$(only_sources_differs "$CHART" "$copy")"; then
    echo "НЕ ВЫПОЛНИЛОСЬ: копия отличается от каталога чарта не ровно файлом templates/_sources.tpl:"
    printf '%s\n' "${diffs:-(различий нет — фикстура равна таблице каталога)}" | sed 's/^/  /'
    exit 2
  fi

  render_chart "$copy" "$into/copy.yaml" || die2 "рендер копии осмотра отказал: $(head -3 "$into/copy.yaml.err")"
  copy_n="$(count_objects "$into/copy.yaml")" || die2 "рендер копии не разобран"
  echo "копия осмотра: $copy"
  echo "  фикстура: ${FIXTURE#"$TOP/"} sha256 $(sha256sum "$FIXTURE" | cut -d' ' -f1)"
  echo "  разница с каталогом: ровно templates/_sources.tpl"
  echo "  имена define: $fx_names"
  echo "  объектов копии: $copy_n"
  if [ "$copy_n" -lt 1 ]; then
    echo "НЕ ВЫПОЛНИЛОСЬ: копия осмотра пуста — объектов 0 при таблице из одной строки"
    exit 2
  fi
  return 0
}

self_test() {
  local work rc out fails=0 n=0
  need_tools
  work="$(mktemp -d)"; trap 'rm -rf "$work"' RETURN
  check() { # <имя> <ждём код> <подстрока> <код> <вывод>
    n=$((n + 1))
    if [ "$4" -eq "$2" ] && [[ "$5" == *"$3"* ]]; then
      echo "  ОК  $1 (код $4, «$3»)"
    else
      echo "  ПРОВАЛ  $1: ждали код $2 и «$3», получили код $4"
      printf '%s\n' "$5" | tail -6 | sed 's/^/        /'
      fails=$((fails + 1))
    fi
  }
  echo "== $SCRIPT --self-test =="

  # Близнец: каталог чарта как есть → копия строится.
  out="$(bash "$0" --into "$work/twin" 2>&1)"; rc=$?
  check "каталог чарта как есть → копия построена" 0 "объектов копии:" "$rc" "$out"

  # Инъекция: в таблицу каталога внесена строка → отказ «пережила предмет».
  cp -r "$CHART" "$work/filled"
  cp "$FIXTURE" "$work/filled/templates/_sources.tpl"
  out="$(bash "$0" --chart "$work/filled" --into "$work/inj1" 2>&1)"; rc=$?
  check "каталог с внесённой строкой таблицы → отказ" 1 "копия осмотра пережила предмет" "$rc" "$out"

  # Инъекция: в каталоге появился define, которого фикстура не несёт (форма D2 —
  # `sourceLimits`) → красный с обоими множествами.
  cp -r "$CHART" "$work/renamed"
  printf '\n{{- define "notify.sourceLimits" -}}{{- dict | toJson -}}{{- end -}}\n' \
    >>"$work/renamed/templates/_sources.tpl"
  out="$(bash "$0" --chart "$work/renamed" --into "$work/inj2" 2>&1)"; rc=$?
  check "у каталога define, которого нет у фикстуры → красный с обоими" 1 "имена define фикстуры и чарта различаются" "$rc" "$out"

  # Предикат «копия отличается ровно одним файлом» — в обе стороны на паре
  # каталог/копия: близнец (подменён только _sources.tpl) → да; копия с лишним
  # шаблоном → нет, и лишний файл назван; копия без подмены → нет.
  mkdir -p "$work/pair"
  cp -r --preserve=timestamps "$CHART" "$work/pair/one"
  cp "$FIXTURE" "$work/pair/one/templates/_sources.tpl"
  cp -r --preserve=timestamps "$work/pair/one" "$work/pair/extra"
  echo "# лишний" >"$work/pair/extra/templates/extra.yaml"
  cp -r --preserve=timestamps "$CHART" "$work/pair/same"
  out="$(only_sources_differs "$CHART" "$work/pair/one")"; rc=$?
  check "копия с подменённой таблицей → ровно один файл" 0 "_sources.tpl" "$rc" "$out"
  out="$(only_sources_differs "$CHART" "$work/pair/extra")"; rc=$?
  check "копия с лишним шаблоном → не ровно один файл, лишний назван" 1 "extra.yaml" "$rc" "$out"
  out="$(only_sources_differs "$CHART" "$work/pair/same")"; rc=$?
  check "копия без подмены → не ровно один файл" 1 "" "$rc" "$out"

  # Инъекция: имена define записаны сырой строкой (`define \`имя\``) — helm её
  # принимает, а читатель имён (образец с двойными кавычками) не видит. У чарта и
  # у фикстуры множества читаются ПУСТЫМИ, и сверка «множества равны» прошла бы
  # на двух пустых. Ждём «не выполнилось» с причиной, а не построенную копию.
  cp -r "$CHART" "$work/raw"
  sed -i 's/define "\([^"]*\)"/define `\1`/' "$work/raw/templates/_sources.tpl"
  sed 's/define "\([^"]*\)"/define `\1`/' "$FIXTURE" >"$work/raw-fixture.tpl"
  out="$(bash "$0" --chart "$work/raw" --fixture "$work/raw-fixture.tpl" --into "$work/inj3" 2>&1)"; rc=$?
  check "имена define не прочитаны ни у чарта, ни у фикстуры → не выполнилось" 2 "не прочитано ни одного имени define" "$rc" "$out"

  # Близнец по месту: копия в каталоге с ПРОБЕЛОМ в пути строится так же — предикат
  # «ровно один файл» судит относительные пути, а не текст вывода `diff`.
  out="$(bash "$0" --into "$work/with space/twin" 2>&1)"; rc=$?
  check "каталог копии с пробелом в пути → копия построена" 0 "объектов копии:" "$rc" "$out"

  # Инъекция: PATH без одного внешнего инструмента (`tr`) при прочих на месте →
  # «не выполнилось» с его именем, а не копия, объявленная проверенной на двух
  # пустых множествах имён. Близнец — тот же собранный PATH со всеми
  # инструментами: копия строится (иначе красное могло прийти от самой сборки PATH).
  mkdir -p "$work/bin-all" "$work/bin-no-tr"
  local t p
  for t in bash helm python3 mktemp mkdir cp rm cat sha256sum cut tr head sed dirname; do
    p="$(command -v "$t")" || continue
    ln -s "$p" "$work/bin-all/$t"
    [ "$t" = tr ] || ln -s "$p" "$work/bin-no-tr/$t"
  done
  out="$(PATH="$work/bin-all" "$work/bin-all/bash" "$0" --into "$work/path-twin" 2>&1)"; rc=$?
  check "собранный PATH со всеми инструментами → копия построена" 0 "объектов копии:" "$rc" "$out"
  out="$(PATH="$work/bin-no-tr" "$work/bin-no-tr/bash" "$0" --into "$work/path-inj" 2>&1)"; rc=$?
  check "PATH без tr → не выполнилось, инструмент назван" 2 "tr не в PATH" "$rc" "$out"

  echo "утверждений: $n, провалов: $fails"
  [ "$n" -gt 0 ] || { echo "НЕ ВЫПОЛНИЛОСЬ: ни одного утверждения"; return 2; }
  [ "$fails" -eq 0 ]
}

mode=""; into=""
while [ $# -gt 0 ]; do
  case "$1" in
    --table) mode=table ;;
    --into) mode=into; into="${2:-}"; shift ;;
    --chart) CHART="$(cd "${2:-}" && pwd)" || die2 "каталога чарта ${2:-} нет"; shift ;;
    --fixture) FIXTURE="${2:-}"; shift ;;
    --self-test) mode=self ;;
    *) echo "использование: $SCRIPT --table | --into <каталог> [--chart <каталог>] [--fixture <файл>] | --self-test" >&2; exit 2 ;;
  esac
  shift
done

case "$mode" in
  table) cmd_table ;;
  into) [ -n "$into" ] || die2 "--into без каталога"; cmd_into "$into" ;;
  self) self_test; exit $? ;;
  *) echo "использование: $SCRIPT --table | --into <каталог> [--chart <каталог>] | --self-test" >&2; exit 2 ;;
esac
