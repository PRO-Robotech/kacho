#!/usr/bin/env bash
# shellcheck disable=SC2016  # выражения в кавычках — ТЕЛО дефектной копии, раскрывать их нельзя
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

# Переменные САМОГО механизма обрываются наравне с git-окружением, и по той же
# причине: под `git push` их выставляет хук — настоящим диапазоном настоящей
# отправки. Внутри синтетического репозитория такой диапазон не резолвится, и
# производитель законно возвращает ПОЛНЫЙ набор («нерезолвимый ствол не сужает»),
# из-за чего утверждение про правку вне диапазона получает лишнюю группу и
# краснеет — на исправном производителе.
#
# Наблюдалось ровно в той форме, которая хуже всего читается: запущенная РУКАМИ
# проба проходит, а та же проба под отправкой падает, и её вывод обвиняет
# производителя. Проба обязана строить своё окружение целиком, а не полагаться
# на то, что снаружи пусто.
unset KACHO_PUSH_RANGE KACHO_TRUNK_REF KACHO_PREPUSH_GROUP

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

# Второй дефектный вариант: диапазон отправки ИГНОРИРУЕТСЯ, база всегда ствол.
# Ровно та форма, что стояла до issue #719. Свой дефект нужен каждому свойству:
# на первом дефекте новые утверждения проехали бы, потому что он диапазон читает.
BROKEN_RANGE="$tmp/prepush-groups-ignores-range.sh"
sed 's|if \[ -n "${KACHO_PUSH_RANGE:-}" \]; then|if false; then|' "$PRODUCER" > "$BROKEN_RANGE"
grep -q 'if false; then' "$BROKEN_RANGE" || { echo "дефект диапазона не воссоздан — подмена не сработала" >&2; exit 2; }
chmod +x "$BROKEN_RANGE"

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

# Третья линия — НАКОПИТЕЛЬНАЯ: чужая правка консоли уже в её истории, а эта
# отправка несёт только серверный коммит. Именно здесь прежняя база лгала:
# ветка «трогает консоль», хотя отправляемый диапазон её не касается.
g checkout -q -b line-accum trunk~1
echo 'export const foreign = 1' >> "$repo/ui-future/vpc/a.ts"
g add -A; g commit -qm "чужая линия правит консоль"
accum_pushed="$(g rev-parse HEAD)"          # то, что УЖЕ на origin
echo 'package b' >> "$repo/services/b.go"
g add -A; g commit -qm "моя правка сервера"

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

    g checkout -q line-accum
    got="$(cd "$repo" && KACHO_TRUNK_REF=trunk KACHO_PUSH_RANGE="$accum_pushed..HEAD" bash "$p")"
    if [ "$got" = "proto go" ]; then printf '  ok   [%s] чужая правка консоли ВНЕ диапазона отправки не добавляет ui-types\n' "$tag"
    else printf '  FAIL [%s] чужая правка вне диапазона: ждали «proto go», получили «%s»\n' "$tag" "$got"; f=$((f+1)); fi

    got="$(cd "$repo" && KACHO_TRUNK_REF=trunk KACHO_PUSH_RANGE="${accum_pushed}~1..HEAD" bash "$p")"
    if [ "$got" = "proto go ui-types" ]; then printf '  ok   [%s] консоль ВНУТРИ диапазона добавляет ui-types\n' "$tag"
    else printf '  FAIL [%s] консоль внутри диапазона: ждали «proto go ui-types», получили «%s»\n' "$tag" "$got"; f=$((f+1)); fi

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
echo "── прогон против дефекта «диапазон игнорируется» (ждём хотя бы один провал)"
assert_all "$BROKEN_RANGE" дефект-диапазона "$tmp/brange.n"; brange_fails="$(cat "$tmp/brange.n")"

echo
printf 'prepush-groups-inject: утверждений на прогон 6, прогонов 3\n'
printf '  провалов у настоящего: %s (норма 0)\n' "$real_fails"
printf '  провалов у дефекта базы:      %s (норма ≥1 — иначе проба ничего не проверяет)\n' "$broken_fails"
printf '  провалов у дефекта диапазона: %s (норма ≥1 — своё свойство, свой дефект)\n' "$brange_fails"

rc=0
[ "$real_fails" = "0" ]  || { echo "ОТКАЗ: настоящий производитель не проходит собственных утверждений" >&2; rc=1; }
[ "${broken_fails:-0}" -ge 1 ] || { echo "ОТКАЗ: проба ЗЕЛЁНАЯ на возвращённом дефекте базы — она не проверяет свой предмет" >&2; rc=1; }
[ "${brange_fails:-0}" -ge 1 ] || { echo "ОТКАЗ: проба ЗЕЛЁНАЯ на дефекте «диапазон игнорируется» — новое свойство не проверяется" >&2; rc=1; }
exit "$rc"
