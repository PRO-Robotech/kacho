#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Инъекция для `scripts/release/assert-pin-agrees.sh` — предмет один: СВЕРКА
# МЕНЯЕТ ЦВЕТ ОТ СОГЛАСИЯ ДЕРЕВЬЕВ, А НЕ ОТ ЧЕГО-ТО ЕЩЁ.
#
# ЗАЧЕМ. Сегодня сверка красная — три дерева называют три версии, — и красной
# останется до шагов 6-7 порядка выпуска. Красное, которое не может позеленеть,
# неотличимо от красного, которое просто всегда красное: утверждение «зеленеет
# после сведения пина» без доказательства есть обещание. Свести настоящие
# деревья ради доказательства нельзя — одно из них чужой репозиторий.
#
# КАК ЭТО ДОКАЗЫВАЕТСЯ. Сверка читает объявление модуля из ссылок клона и о
# природе клона ничего не знает. Значит достаточно СИНТЕТИЧЕСКОГО репозитория, в
# котором меняется РОВНО ОДИН факт — совпадают ли версии. Тот же скрипт, тот же
# вход, разный исход.
#
# ЧЕТВЁРТАЯ ОСЬ ВАЖНЕЕ ТРЁХ ПЕРВЫХ. Дерево, о котором спросить не удалось,
# «согласным» не считается: иначе в клоне без второй ссылки сверка печатала бы
# зелёное, сверив одно дерево с самим собой, — и настоящая находка не нашлась бы
# никогда.
#
# ИСХОДЫ: 0 — все утверждения сошлись; 1 — есть провалившиеся; 2 — предмета нет.
set -uo pipefail

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUBJECT="$HERE/assert-pin-agrees.sh"
[ -r "$SUBJECT" ] || { echo "предмета нет: $SUBJECT" >&2; exit 2; }
command -v git >/dev/null 2>&1 || { echo "инструмента нет: git" >&2; exit 2; }

# Временный каталог — ВНЕ всякого репозитория: синтетическое дерево, заведённое
# внутри объемлющего, нашло бы его индекс подъёмом вверх, и вердикт стал бы
# свойством рабочего каталога, а не входа.
tmp="$(mktemp -d "${TMPDIR:-/tmp}/pinagree.XXXXXX")"
trap 'chmod -R u+w "$tmp" 2>/dev/null; rm -rf "$tmp"' EXIT

MOD="example.com/platform"
V1="v0.0.0-20260101000000-aaaaaaaaaaaa"
V2="v0.0.0-20260202000000-bbbbbbbbbbbb"
REL="v0.1.0"

checked=0; failed=0
pass() { checked=$((checked+1)); printf '  ok   %s\n' "$1"; }
fail() { checked=$((checked+1)); failed=$((failed+1)); printf '  FAIL %s\n     %b\n' "$1" "$2"; }

# mkworld <каталог> <версия-рабочего-дерева> <версия-ствола|-> 
#
# Заводит «платформу» с ссылкой origin/main. Прочерк вместо второй версии
# означает «ствола нет вовсе» — вход для оси «спрошено меньше двух».
mkworld() {
    local d="$1" wt="$2" trunk="$3"
    local up="$d/upstream" work="$d/work"
    rm -rf "$d"; mkdir -p "$up"
    ( cd "$up" && git init -q -b main . \
      && mkdir -p services/iam \
      && printf 'module example.com/service\n\ngo 1.26.0\n\nrequire (\n\t%s %s\n)\n' "$MOD" "${trunk:--}" > services/iam/go.mod \
      && git add -A && git -c user.email=x@y -c user.name=x commit -qm trunk )
    git clone -q "$up" "$work"
    printf 'module example.com/service\n\ngo 1.26.0\n\nrequire (\n\t%s %s\n)\n' "$MOD" "$wt" > "$work/services/iam/go.mod"
    printf '%s' "$work"
}

run() { # run <каталог-работы> [аргументы] -> печатает вывод, возвращает код
    ( cd "$1" && shift && KACHO_RELEASE_MODULE="$MOD" \
        KACHO_PIN_CROSSTREE_NETWORK=0 "$SUBJECT" "$@" 2>&1 )
}

echo "инъекция: сверка пина платформы"

# ── ось 1: КОНТРОЛЬ — деревья согласны ───────────────────────────────────────
w="$(mkworld "$tmp/agree" "$V1" "$V1")"
OUT="$(run "$w")"; RC=$?
if [ "$RC" = "0" ]; then pass "1 согласие двух деревьев -> код 0"
else fail "1 согласие двух деревьев" "ожидался 0, получен $RC:\n$OUT"; fi
if [[ "$OUT" == *"сверено деревьев 2"* ]]; then
    pass "1' перепись называет число сверенных: «сверено 0» отличимо от «сверено»"
else fail "1' перепись сверенных" "в выводе её нет:\n$OUT"; fi

# ── ось 2: ИНЪЕКЦИЯ — деревья расходятся (меняется РОВНО версия) ─────────────
w="$(mkworld "$tmp/differ" "$V2" "$V1")"
OUT="$(run "$w")"; RC=$?
if [ "$RC" = "1" ]; then pass "2 расхождение двух деревьев -> код 1 (находка)"
else fail "2 расхождение двух деревьев" "ожидался 1, получен $RC:\n$OUT"; fi
if [[ "$OUT" == *"$V1"* && "$OUT" == *"$V2"* ]]; then
    pass "2' находка НАЗЫВАЕТ обе версии, а не только факт расхождения"
else fail "2' находка называет версии" "их нет в выводе:\n$OUT"; fi

# ── ось 3: спрошено меньше двух -> ВЕРДИКТА НЕТ, а не согласие ───────────────
# Ствол без строки модуля: объявление есть, пина в нём нет. Сверять не с чем, и
# «одно дерево, сверенное с самим собой» согласием не является.
w="$(mkworld "$tmp/lonely" "$V1" "")"
OUT="$(run "$w")"; RC=$?
if [ "$RC" = "3" ]; then pass "3 спрошено одно дерево -> код 3 (вердикта нет), НЕ 0"
else fail "3 спрошено одно дерево" "ожидался 3, получен $RC:\n$OUT"; fi

# ── ось 4: сеть выключена ручкой -> источник в «не спрошено», не в согласии ──
w="$(mkworld "$tmp/net" "$V1" "$V1")"
OUT="$(run "$w")"
if [[ "$OUT" == *"НЕ спрошено"*"сетевая часть выключена"* ]]; then
    pass "4 выключенная сеть названа «НЕ спрошено», а не зачтена в согласие"
else fail "4 выключенная сеть" "в выводе нет строки «не спрошено»:\n$OUT"; fi

# ── ось 5: --require-released на ПСЕВДОверсии -> находка ─────────────────────
w="$(mkworld "$tmp/pseudo" "$V1" "$V1")"
OUT="$(run "$w" --require-released)"; RC=$?
if [ "$RC" = "1" ]; then pass "5 --require-released на псевдоверсии -> код 1"
else fail "5 --require-released на псевдоверсии" "ожидался 1, получен $RC:\n$OUT"; fi

# ── ось 5': КОНТРОЛЬ — та же ручка на опубликованной версии молчит ───────────
w="$(mkworld "$tmp/released" "$REL" "$REL")"
OUT="$(run "$w" --require-released)"; RC=$?
if [ "$RC" = "0" ]; then pass "5' --require-released на опубликованной версии -> код 0"
else fail "5' --require-released на опубликованной" "ожидался 0, получен $RC:\n$OUT"; fi

# ── ось 6: КОНТРОЛЬ распознавателя — путь модуля в КОММЕНТАРИИ пином не является
# Без этой оси сверка считала бы пином собственное объяснение и краснела бы на
# согласных деревьях.
w="$(mkworld "$tmp/comment" "$V1" "$V1")"
printf 'module example.com/service\n\ngo 1.26.0\n\n// %s %s — так выглядел пин до сведения\nrequire (\n\t%s %s\n)\n' \
    "$MOD" "$V2" "$MOD" "$V1" > "$w/services/iam/go.mod"
OUT="$(run "$w")"; RC=$?
if [ "$RC" = "0" ]; then pass "6 путь модуля в комментарии пином не считается (контроль)"
else fail "6 комментарий не пин" "ожидался 0, получен $RC:\n$OUT"; fi

# ── ось 7: перечень ссылок ВЫВОДИТСЯ — новая линия попадает в сверку сама ────
w="$(mkworld "$tmp/derived" "$V1" "$V1")"
( cd "$w" && git checkout -q -b release/new origin/main \
  && printf 'module example.com/service\n\ngo 1.26.0\n\nrequire (\n\t%s %s\n)\n' "$MOD" "$V2" > services/iam/go.mod \
  && git add -A && git -c user.email=x@y -c user.name=x commit -qm line \
  && git push -q origin release/new && git checkout -q main && git fetch -q origin ) >/dev/null 2>&1
OUT="$(run "$w")"; RC=$?
if [ "$RC" = "1" ] && [[ "$OUT" == *"release/new"* ]]; then
    pass "7 новая накопительная линия попадает в сверку без правки скрипта"
else fail "7 перечень ссылок выводится" "ожидался 1 с упоминанием release/new, получен $RC:\n$OUT"; fi

printf '\nперепись: утверждений исполнено %d, провалено %d\n' "$checked" "$failed"
if [ "$checked" = "0" ]; then
    echo "обход пуст: не исполнено ни одного утверждения — вердикт беспредметен" >&2; exit 2
fi
if [ "$failed" -gt 0 ]; then
    echo "ИНЪЕКЦИЯ ПРОВАЛЕНА: сверка не доказала способность падать и молчать" >&2; exit 1
fi
echo "инъекция сошлась: сверка краснеет на расхождении и молчит на согласии"
exit 0
