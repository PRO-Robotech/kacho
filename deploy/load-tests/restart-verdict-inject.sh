#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# restart-verdict-inject.sh — доказательство инъекцией для вердикта о перезапуске.
#
# Проверяет ОБЕ стороны по каждой оси: дефект обязан быть назван ПОИМЁННО, а
# законный близнец — пройти молча. Без второй стороны вердикт, красящий всё
# подряд, был бы неотличим от работающего — а именно такой и стоял здесь до
# 2026-08-21: он браковал каждую ступень прогона на трёх репликах, при том что
# счётчик перезапусков базы был нулём.
set -uo pipefail
cd "$(dirname "$0")" || exit 1
. lib/restart-verdict.sh

tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
pass=0; fail=0
snap() { printf '%s\n' "$@" > "$tmp/$1.txt"; }   # первый аргумент — имя файла

# check — для функции, отвечающей ВЫВОДОМ (restart_grew): «есть» значит непусто.
check() { # имя, ожидание(«есть»/«нет»), фактический вывод
  local name=$1 want=$2 got=$3
  if { [ "$want" = "есть" ] && [ -n "$got" ]; } || { [ "$want" = "нет" ] && [ -z "$got" ]; }; then
    pass=$((pass+1)); echo "  [ok] $name"
  else
    fail=$((fail+1)); echo "  [ПРОВАЛ] $name — ждали «$want», получили «${got:-<пусто>}»"
  fi
}

# check_eq — для функции, отвечающей КОДОМ ВОЗВРАТА (composition_changed).
#
# Функции нужны разные, и это не педантство: первая редакция подавала слово
# «есть»/«нет» в check, который смотрит на ПУСТОТУ, — и обе проверки состава
# проходили при любом исходе, потому что оба слова непусты. Проба формы без
# содержания в самой пробе; поймана тем, что третий случай ждал «нет» и всё
# равно оказался провальным.
check_eq() { # имя, ожидание, факт
  local name=$1 want=$2 got=$3
  if [ "$want" = "$got" ]; then
    pass=$((pass+1)); echo "  [ok] $name"
  else
    fail=$((fail+1)); echo "  [ПРОВАЛ] $name — ждали «$want», получили «$got»"
  fi
}

printf '%s\n' 'iam-a=0' 'iam-b=0' 'pg-iam-0=1' > "$tmp/base.txt"

# ── (а) ДЕФЕКТ: счётчик выжившего пода вырос — это перезапуск участника.
printf '%s\n' 'iam-a=0' 'iam-b=0' 'pg-iam-0=2' > "$tmp/restarted.txt"
got=$(restart_grew "$tmp/base.txt" "$tmp/restarted.txt")
check "перезапуск базы назван поимённо" есть "$got"
[ "$got" = "pg-iam-0" ] || { echo "  [ПРОВАЛ] назван не тот под: $got"; fail=$((fail+1)); }

# ── (б) ЗАКОННЫЙ БЛИЗНЕЦ: ничего не менялось.
got=$(restart_grew "$tmp/base.txt" "$tmp/base.txt")
check "неизменный снимок молчит" нет "$got"

# ── (в) ЗАКОННЫЙ БЛИЗНЕЦ: состав вырос (масштабирование), счётчики те же.
printf '%s\n' 'iam-a=0' 'iam-b=0' 'iam-c=0' 'pg-iam-0=1' > "$tmp/scaled.txt"
got=$(restart_grew "$tmp/base.txt" "$tmp/scaled.txt")
check "масштабирование НЕ считается перезапуском" нет "$got"

# ── (г) ЗАКОННЫЙ БЛИЗНЕЦ: старый под дотерминировался и исчез.
printf '%s\n' 'iam-b=0' 'pg-iam-0=1' > "$tmp/shrunk.txt"
got=$(restart_grew "$tmp/base.txt" "$tmp/shrunk.txt")
check "исчезновение пода НЕ считается перезапуском" нет "$got"

# ── (д) ДЕФЕКТ + СОСТАВ ОДНОВРЕМЕННО: рост счётчика обязан быть виден и тогда,
#        когда состав тоже изменился. Иначе смена состава маскировала бы падение.
printf '%s\n' 'iam-b=0' 'iam-c=0' 'pg-iam-0=3' > "$tmp/both.txt"
got=$(restart_grew "$tmp/base.txt" "$tmp/both.txt")
check "рост счётчика виден и при смене состава" есть "$got"

# ── (е) СМЕНА СОСТАВА: распознаётся в обе стороны и молчит на равенстве.
composition_changed "$tmp/base.txt" "$tmp/scaled.txt" && r=есть || r=нет
check_eq "рост состава распознан" есть "$r"
composition_changed "$tmp/base.txt" "$tmp/shrunk.txt" && r=есть || r=нет
check_eq "убыль состава распознана" есть "$r"
composition_changed "$tmp/base.txt" "$tmp/restarted.txt" && r=есть || r=нет
check_eq "перезапуск БЕЗ смены состава не выдаётся за смену состава" нет "$r"

echo
echo "инъекция вердикта: утверждений $((pass+fail)), пройдено $pass, провалено $fail"
[ "$fail" -eq 0 ] || exit 1
