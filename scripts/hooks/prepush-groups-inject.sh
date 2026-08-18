#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Инъекция для prepush-groups.sh. Проверяет ОБЕ способности: добавить группу
# там, где предмет есть, и НЕ добавить там, где его нет. Отрицание без
# положительного контроля зеленеет на всём сломанном — производитель, никогда
# не добавляющий ui-types, прошёл бы одностороннюю пробу.
#
# ПРОБА ДОКАЗЫВАЕТ СВОЮ СПОСОБНОСТЬ УПАСТЬ САМА. Тот же набор утверждений
# гоняется дважды: против настоящего производителя (ждём ноль провалов) и
# против воссозданного дефекта (ждём хотя бы один). Без второго прогона
# зелёная проба неотличима от пробы, ничего не проверяющей: первая её
# редакция была именно такой — воспроизводила НЕ ТУ форму дефекта и оставалась
# зелёной на возвращённом.
#
# ВОССОЗДАВАЕМЫЙ ДЕФЕКТ — база «вышестоящая ветка» вместо точки ветвления от
# ствола. Накопительная линия догоняет ствол, и относительно прежней СВОЕЙ
# отправки ствольные правки читаются как её собственные.
#
# ИЗОЛЯЦИЯ ОБЯЗАТЕЛЬНА. Окружение git обрывается: унаследованный GIT_DIR
# сильнее рабочего каталога, и тогда `git add` фикстуры пишет в индекс той
# копии, из которой проба запущена. Индекс схлопывается, а падают потом чужие
# гейты, читающие состав дерева, — виновник остаётся невидимым.
set -uo pipefail

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PRODUCER="$HERE/prepush-groups.sh"
[ -x "$PRODUCER" ] || { echo "производителя нет: $PRODUCER" >&2; exit 2; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# Дефектный вариант: база — вышестоящая ветка. Ровно та форма, что стояла в
# хуке до issue #690.
BROKEN="$tmp/prepush-groups-broken.sh"
sed 's|changed="$(git diff --name-only "$trunk"\.\.\.HEAD 2> /dev/null \|\| true)"|base="$(git rev-parse --abbrev-ref --symbolic-full-name "@{upstream}" 2> /dev/null \|\| echo "$trunk")"; changed="$(git diff --name-only "$base"...HEAD 2> /dev/null \|\| true)"|' \
    "$PRODUCER" > "$BROKEN"
grep -q '@{upstream}' "$BROKEN" || { echo "дефект не воссоздан — подмена не сработала" >&2; exit 2; }
chmod +x "$BROKEN"

repo="$tmp/repo"; mkdir -p "$repo"
g() { git -C "$repo" -c user.email=p@i -c user.name=p -c commit.gpgsign=false "$@"; }

g init -q -b main
mkdir -p "$repo/services" "$repo/ui-future/vpc"
echo core > "$repo/services/a.go"; echo ui > "$repo/ui-future/vpc/a.ts"
g add -A; g commit -qm "основание"
g branch trunk                                   # роль origin/main

# Линия: своя правка только на сервере, затем догон ствола, правившего консоль.
# Вышестоящая ветка указывает на состояние ДО догона — так и бывает у
# накопительной линии, отправленной раньше.
g checkout -q -b line-nonui trunk
echo 'package a' >> "$repo/services/a.go"; g add -A; g commit -qm "линия правит сервер"
# Вышестоящая ветка задаётся ПОЛНОСТЬЮ: ссылка, объявленный remote и связь.
# Одной ссылки мало — без объявленного remote `@{upstream}` не резолвится, и
# дефектный вариант молча уходит на запасной путь, то есть дефекта не
# воспроизводит. Первая редакция пробы была именно такой и оставалась зелёной
# на возвращённом дефекте.
g remote add origin "$repo" 2> /dev/null || true
g update-ref refs/remotes/origin/line-nonui "$(g rev-parse HEAD)"
g config branch.line-nonui.remote origin
g config branch.line-nonui.merge refs/heads/line-nonui
g rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' > /dev/null 2>&1 || {
    echo "фикстура не создала условие: вышестоящая ветка не резолвится" >&2; exit 2; }

g checkout -q trunk
echo 'export const trunkOnly = 1' >> "$repo/ui-future/vpc/a.ts"
g add -A; g commit -qm "ствол правит консоль"

g checkout -q line-nonui
g merge -q --no-ff trunk -m "догон ствола" > /dev/null 2>&1

# Вторая линия: своя правка консоли — положительный контроль.
g checkout -q -b line-ui trunk~1
echo 'export const mine = 1' >> "$repo/ui-future/vpc/a.ts"
g add -A; g commit -qm "линия правит консоль"

run() { (cd "$repo" && KACHO_TRUNK_REF=trunk bash "$1"); }

# Набор утверждений. Текст идёт читателю, ЧИСЛО провалов — в файл $3.
# Один поток на то и другое не годится: `tail -c` режет по байтам и рвёт
# многобайтные символы — на этом сама проба и упала при первой сборке.
assert_all() { # $1 — производитель, $2 — метка прогона, $3 — файл для числа
    local p="$1" tag="$2" out="$3" f=0 got
    g checkout -q line-ui
    got="$(run "$p")"
    if [ "$got" = "proto go ui-types" ]; then printf '  ok   [%s] своя правка консоли добавляет ui-types\n' "$tag"
    else printf '  FAIL [%s] своя правка консоли: ждали «proto go ui-types», получили «%s»\n' "$tag" "$got"; f=$((f+1)); fi

    g checkout -q line-nonui
    got="$(run "$p")"
    if [ "$got" = "proto go" ]; then printf '  ok   [%s] догон ствола НЕ добавляет ui-types\n' "$tag"
    else printf '  FAIL [%s] догон ствола: ждали «proto go», получили «%s»\n' "$tag" "$got"; f=$((f+1)); fi

    got="$(cd "$repo" && KACHO_TRUNK_REF=trunk KACHO_PREPUSH_GROUP=go bash "$p")"
    if [ "$got" = "go" ]; then printf '  ok   [%s] названный набор сильнее вывода\n' "$tag"
    else printf '  FAIL [%s] названный набор: получили «%s»\n' "$tag" "$got"; f=$((f+1)); fi

    got="$(cd "$repo" && KACHO_TRUNK_REF=refs/heads/нет-такой bash "$p")"
    if [ "$got" = "proto go" ]; then printf '  ok   [%s] нерезолвимый ствол не сужает набор\n' "$tag"
    else printf '  FAIL [%s] нерезолвимый ствол: получили «%s»\n' "$tag" "$got"; f=$((f+1)); fi

    printf '%s' "$f" > "$out"
}

echo "── прогон против настоящего производителя (ждём ноль провалов)"
assert_all "$PRODUCER" настоящий "$tmp/real.n"; real_fails="$(cat "$tmp/real.n")"
echo
echo "── прогон против воссозданного дефекта (ждём хотя бы один провал)"
assert_all "$BROKEN" дефект "$tmp/broken.n"; broken_fails="$(cat "$tmp/broken.n")"

echo
printf 'prepush-groups-inject: утверждений на прогон 4, прогонов 2\n'
printf '  провалов у настоящего: %s (норма 0)\n' "$real_fails"
printf '  провалов у дефекта:    %s (норма ≥1 — иначе проба ничего не проверяет)\n' "$broken_fails"

rc=0
[ "$real_fails" = "0" ]  || { echo "ОТКАЗ: настоящий производитель не проходит собственных утверждений" >&2; rc=1; }
[ "${broken_fails:-0}" -ge 1 ] || { echo "ОТКАЗ: проба ЗЕЛЁНАЯ на возвращённом дефекте — она не проверяет свой предмет" >&2; rc=1; }
exit "$rc"
