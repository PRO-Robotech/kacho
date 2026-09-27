#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# makefile-soft-pass-test.sh — ПРОВЕРКА, ОБЁРНУТАЯ НАЛИЧИЕМ ИНСТРУМЕНТА, НЕ ВПРАВЕ
# ГЛУШИТЬ СОБСТВЕННЫЙ ОТКАЗ (задача #2038).
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ЛОВИТ — НА КОНКРЕТНОМ СЛУЧАЕ
#
# Цель валидации политик несла форму
#
#     command -v kubeconform >/dev/null && kubeconform … || echo "(kubeconform not installed …)"
#
# `A && B || C` — не «если-то-иначе»: `C` срабатывает на ненулевой код `B` так же,
# как на отсутствие `A`. Когда инструмент ЕСТЬ, а валидация упала, цель печатала
# «инструмент не установлен» и выходила КОДОМ 0. Отказ назывался не той
# причиной — читатель шёл ставить инструмент, которого не хватало только на
# бумаге, — и проверка, исполняясь, не отказала ни разу за свою жизнь.
#
# Исходов у такой проверки ТРИ, и различать их обязана она сама:
#   инструмента нет  → «условие не создано» (ненулевой код, отдельный от находки);
#   валидация прошла → 0;
#   валидация упала  → ненулевой код с текстом инструмента.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО УТВЕРЖДАЕТСЯ
#
# В рецептах deploy/Makefile нет логической команды (продолжения `\` склеены), в
# которой проверка наличия инструмента (`command -v`) стоит в цепочке `&&`, а
# хвост `|| …` — мягкий: `echo`/`printf`/`true`/`:`. Такой хвост глушит и отказ
# самого инструмента. Законные формы той же темы — `command -v X || { …; exit N; }`
# (отсутствие не прощается) и `if ! command -v X; then …; exit N; fi; X …` —
# находкой не являются.
#
# Объём осмотренного печатается: логических команд · из них с `command -v`.
# «Ноль находок» при нуле команд с `command -v` — не чистота, а слепота предиката.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/../.." && pwd)"
MAKEFILE="${SOFT_PASS_MAKEFILE:-$DEPLOY_ROOT/Makefile}"

# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
require_python3

# scan <makefile> → строки `FIND <строка> <цель>: <команда>` и `CENSUS <команд> <с command -v>`
scan() {
  python3 - "$1" <<'PY'
import re, sys
lines = open(sys.argv[1], encoding="utf-8").read().split("\n")
target, cmds, cur, start = None, [], "", 0
for i, ln in enumerate(lines, 1):
    m = re.match(r"^([A-Za-z0-9_.-]+):(?!=)", ln)
    if m and not ln.startswith("\t"):
        if cur:
            cmds.append((start, target, cur)); cur = ""
        target = m.group(1)
        continue
    if not ln.startswith("\t") or target is None:
        if cur:
            cmds.append((start, target, cur)); cur = ""
        if ln and not ln.startswith("\t") and not ln.startswith("#"):
            target = None
        continue
    body = ln[1:]
    if body.lstrip().startswith("#"):
        continue
    if not cur:
        start = i
    cur += " " + body.rstrip().rstrip("\\").strip()
    if not body.rstrip().endswith("\\"):
        cmds.append((start, target, cur)); cur = ""
if cur:
    cmds.append((start, target, cur))
soft = re.compile(r"command\s+-v\s+\S+[^;|]*&&[^;]*\|\|\s*(echo|printf|true|:)(\s|$|;)")
with_cv = 0
for start, target, cmd in cmds:
    if re.search(r"command\s+-v\s+\S+", cmd):
        with_cv += 1
    if soft.search(cmd):
        print(f"FIND {start} {target}: {cmd.strip()[:160]}")
print(f"CENSUS {len(cmds)} {with_cv}")
PY
}

judge() {
  local out census cmds cv line
  out="$(scan "$1")" || fatal "разбор $1 сорвался — судить не о чем"
  census="$(grep '^CENSUS' <<<"$out")"
  read -r _ cmds cv <<<"$census"
  [ "${cmds:-0}" -gt 0 ] || fatal "в $1 не разобрано НИ ОДНОЙ команды рецепта — предикат слеп"
  [ "${cv:-0}" -gt 0 ] || fatal "в $1 нет НИ ОДНОЙ проверки наличия инструмента — у запрета не осталось предмета либо предикат перестал её узнавать"
  while IFS= read -r line; do
    case "$line" in
      FIND\ *) line="${line#FIND }"
               violation "$(basename "$1"):${line%% *} — проверка наличия инструмента в цепочке «&& … || мягкий хвост»: хвост глушит и отказ самого инструмента (цель ${line#* })" ;;
    esac
  done <<<"$out"
  ok
  echo "  перепись: логических команд рецептов $cmds, из них с проверкой наличия инструмента $cv"
}

self_test() {
  local st rc=0 n=0 out
  st="$(mktemp -d)"; trap 'rm -rf "$st"' RETURN
  probe() { # probe <метка> <ожидаемый код> <подстрока|-> <тело рецепта>
    n=$((n + 1))
    printf 'check:\n%b\n' "$4" >"$st/Makefile"
    out="$(SOFT_PASS_MAKEFILE="$st/Makefile" bash "$0" 2>&1)"; local got=$?
    if [ "$got" = "$2" ] && { [ "$3" = - ] || [[ "$out" == *"$3"* ]]; }; then echo "  ✓ $1 (код $got)"
    else echo "  ✗ $1 — код $got, ждали $2 и «$3»"; printf '%s\n' "$out" | sed 's/^/      /' | tail -6; rc=1; fi
  }
  probe "форма #2038 дословно (многострочная) → находка с координатой" 1 "Makefile:2" \
    '\t@command -v kubeconform > /dev/null && \\\n\t\tkubeconform -skip X /x.yaml \\\n\t\t|| echo "(kubeconform not installed)"'
  probe "та же форма с «|| true» → находка" 1 "Makefile:2" \
    '\t@command -v yq >/dev/null && yq e . x.yaml || true'
  probe "отсутствие не прощается: «|| { …; exit 2; }» → молчит" 0 - \
    '\t@command -v kubeconform >/dev/null || { echo "нет kubeconform"; exit 2; }\n\t@kubeconform x.yaml'
  probe "три исхода через if → молчит" 0 - \
    '\t@if ! command -v kubeconform >/dev/null; then echo "нет"; exit 2; fi; kubeconform x.yaml'
  probe "тернарий на test без проверки инструмента → вне предмета, но предмета нет вовсе → условие" 2 "нет НИ ОДНОЙ проверки" \
    '\t@d=$$(test "$(SVC)" = gw && echo gateway || echo services/$(SVC))'
  echo "случаев исполнено: $n"
  [ "$n" -eq 5 ] || { echo "FAIL: исполнено $n из 5"; rc=1; }
  if [ "$rc" -eq 0 ]; then echo "PASS: $SCRIPT --self-test"; else echo "FAIL: $SCRIPT --self-test"; fi
  return "$rc"
}

if [ "${1:-}" = "--self-test" ]; then
  self_test; exit $?
fi

require_file_present "$MAKEFILE" "Makefile"
judge "$MAKEFILE"
findings_verdict "Makefile $(basename "$MAKEFILE")"
