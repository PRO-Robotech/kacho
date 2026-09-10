#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# inject-domain-generation-defects.sh — доказательство, что гейт разреза
# (`check-domain-generation.sh`) и утверждения самих генераторов СПОСОБНЫ упасть,
# и что законное дерево они пропускают.
#
# Доказывается не чтением, а возвратом дефекта: по одной оси за раз дефект
# вносится, названная проверка обязана покраснеть И НАЗВАТЬ КООРДИНАТУ. Рядом с
# каждой осью стоит ЗАКОННЫЙ БЛИЗНЕЦ той же формы — правка, которая предмет не
# ломает; на нём проверка обязана молчать. Без близнеца «краснеет на дефекте»
# неотличимо от «краснеет на всём».
#
# Исходов ТРИ, и третий не вычитается из вердикта: 0 — доказано · 1 — проверка не
# упала на возвращённом дефекте либо упала на законной правке · 2 — прогон не
# выполнен (нет buf/go, дерево грязное, правка не применилась).

set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GW="$ROOT/gateway"
LIB="$GW/scripts/lib/stage-proto-tree.sh"
GEN_CAT="$GW/scripts/gen-permission-catalog.sh"
PLUGIN="$GW/cmd/protoc-gen-kacho-permissions/main.go"
GATE="$GW/scripts/check-domain-generation.sh"

for f in "$LIB" "$GEN_CAT" "$PLUGIN" "$GATE"; do
    [ -f "$f" ] || { echo "ОТКАЗ: нет $f — предмета инъекции не существует" >&2; exit 2; }
done
command -v buf >/dev/null || { echo "ОТКАЗ: buf не установлен — прогон не выполнен" >&2; exit 2; }
command -v go  >/dev/null || { echo "ОТКАЗ: go не установлен — прогон не выполнен"  >&2; exit 2; }

if [ -n "$(git -C "$ROOT" status --porcelain -- "$LIB" "$GEN_CAT" "$PLUGIN" 2>/dev/null)" ]; then
    echo "ПРЕДУПРЕЖДЕНИЕ: предметы инъекции уже правлены — восстановление вернёт их" \
         "к состоянию НА МОМЕНТ ЗАПУСКА, а не к HEAD." >&2
fi

BACKUP="$(mktemp -d)"
cp "$LIB" "$BACKUP/lib.sh"; cp "$GEN_CAT" "$BACKUP/gen.sh"; cp "$PLUGIN" "$BACKUP/plugin.go"
restore() { cp "$BACKUP/lib.sh" "$LIB"; cp "$BACKUP/gen.sh" "$GEN_CAT"; cp "$BACKUP/plugin.go" "$PLUGIN"; }
trap 'restore; rm -rf "$BACKUP"' EXIT

fails=0
axes=0
LOG="$(mktemp)"

# pyedit — внесение правки предмета инъекции.
#
# ЗДЕСЬ БЫЛ ГОЛЫЙ `python3 - "$FILE"`, и у него был тихий отказ. Точка инъекции
# опознаётся ЛИТЕРАЛОМ (`assert old in s`); литерал стареет вместе с деревом, и
# тогда правка НЕ применяется, python падает — а харнесс идёт под `set -uo
# pipefail` БЕЗ `-e`, поэтому код возврата никто не читал. Ось прогонялась на
# НЕТРОНУТОМ дереве, получала зелёное и печатала ✓: доказательство докладывало
# об успехе ровно там, где не состоялось.
#
# Наблюдалось на «близнеце 9»: точка `KACHO_PROTO_ROOTS=(kacho kaname)` пережила
# появление третьего корня (`corelib`), assert падал, а ось печатала ✓ — при том
# что харнесс объявлял «осей прогнано 20, провалов 1» и этой оси среди провалов
# не было.
#
# Теперь отказ правки — ПРОВАЛ с именем оси: точек инъекции четырнадцать, и
# каждая может устареть тем же способом.
pyedit() {
    local what="$1"; shift
    if ! python3 - "$@"; then
        echo "  ✗ $what: точка инъекции УСТАРЕЛА — правка не применилась, ось беспредметна" >&2
        fails=$((fails + 1))
        return 1
    fi
    return 0
}

# gate_axis <имя> <red|green> [<обязательная подстрока находки>]
gate_axis() {
    local name="$1" want="$2" needle="${3:-}" rc
    axes=$((axes + 1))
    # shellcheck disable=SC2086 — отбор намеренно разбивается на слова
    ( cd "$GW" && ./scripts/check-domain-generation.sh ${GATE_DOMAINS:-} ) >"$LOG" 2>&1; rc=$?
    if [ "$rc" -eq 2 ]; then
        echo "  ✗ $name: гейт вышел БЕЗ ПРЕДМЕТА (2) — это не вердикт" >&2
        tail -3 "$LOG" >&2; fails=$((fails + 1)); return
    fi
    if [ "$want" = red ] && [ "$rc" -eq 0 ]; then
        echo "  ✗ $name: дефект возвращён, а гейт ЗЕЛЁНЫЙ — он не способен упасть" >&2
        fails=$((fails + 1)); return
    fi
    if [ "$want" = green ] && [ "$rc" -ne 0 ]; then
        echo "  ✗ $name: законная правка, а гейт КРАСНЫЙ — он ловит форму, а не существо" >&2
        grep 'НАХОДКА' "$LOG" | head -3 >&2; fails=$((fails + 1)); return
    fi
    if [ "$want" = red ] && [ -n "$needle" ] && ! grep -qF "$needle" "$LOG"; then
        echo "  ✗ $name: гейт покраснел, но НЕ НАЗВАЛ координату «$needle»" >&2
        grep 'НАХОДКА' "$LOG" | head -5 >&2; fails=$((fails + 1)); return
    fi
    echo "  ✓ $name"
}

# gen_axis <имя> <ожидаемый код: 0|1> <отбор> [<обязательная подстрока>] —
# прогон САМОГО генератора, минуя гейт: утверждение генератора — свой предмет.
gen_axis() {
    local name="$1" want_rc="$2" domains="$3" needle="${4:-}" rc out
    axes=$((axes + 1))
    out="$(cd "$GW" && KACHO_GEN_DOMAINS="$domains" ./scripts/gen-permission-catalog.sh "$(mktemp -u)/cat.json" 2>&1)"; rc=$?
    if [ "$rc" -ne "$want_rc" ]; then
        echo "  ✗ $name: код возврата $rc, ожидался $want_rc" >&2
        echo "$out" | tail -5 >&2; fails=$((fails + 1)); return
    fi
    if [ -n "$needle" ] && ! grep -qF "$needle" <<<"$out"; then
        echo "  ✗ $name: код верный, но вывод НЕ НАЗЫВАЕТ «$needle»" >&2
        echo "$out" | tail -5 >&2; fails=$((fails + 1)); return
    fi
    echo "  ✓ $name"
}

echo "== контроль: нетронутое дерево =="
gate_axis "нетронутое дерево — зелено" green

echo "== ось 1: отбор доменов не сужает =="
pyedit "отбор выброшен — гейт краснеет и называет ось" "$LIB" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='  local selection_raw=${4:-}'
assert old in s, 'ось 1: место инъекции не найдено'
open(p,'w').write(s.replace(old,'  local selection_raw=""  # ИНЪЕКЦИЯ: отбор выброшен',1))
PY
gate_axis "отбор выброшен — гейт краснеет и называет ось" red "урезанное дерево несёт ВСЕ домены"
restore
pyedit "близнец 1: тот же набор доменов в другом порядке — гейт молчит" "$GATE" <<'PY'
import sys
# законный близнец: тот же НАБОР доменов, другой порядок
print('порядок отбора переставлен для контроля')
PY
axes=$((axes + 1))
if ( cd "$GW" && ./scripts/check-domain-generation.sh quota operation iam ) >"$LOG" 2>&1; then
    echo "  ✓ близнец 1: тот же набор доменов в другом порядке — гейт молчит"
else
    echo "  ✗ близнец 1: перестановка отбора покраснела — гейт судит порядок, а не множество" >&2
    grep 'НАХОДКА' "$LOG" | head -3 >&2; fails=$((fails + 1))
fi

echo "== ось 2: корень контрактов игнорируется =="
pyedit "корень контрактов прибит — гейт краснеет и называет ось" "$GEN_CAT" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='PROTO_ROOT="${KACHO_PROTO_ROOT:-${MONOREPO_ROOT}/proto}"'
assert old in s, 'ось 2: место инъекции не найдено'
open(p,'w').write(s.replace(old,'PROTO_ROOT="${MONOREPO_ROOT}/proto"  # ИНЪЕКЦИЯ: ручка игнорируется',1))
PY
gate_axis "корень контрактов прибит — гейт краснеет и называет ось" red "ручка KACHO_PROTO_ROOT не читается"
restore
pyedit "близнец 2: та же величина через промежуточную переменную — гейт молчит" "$GEN_CAT" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='PROTO_ROOT="${KACHO_PROTO_ROOT:-${MONOREPO_ROOT}/proto}"'
# близнец: та же величина, объявленная через промежуточную переменную
new='KACHO_PROTO_ROOT_DEFAULT="${MONOREPO_ROOT}/proto"\nPROTO_ROOT="${KACHO_PROTO_ROOT:-${KACHO_PROTO_ROOT_DEFAULT}}"'
assert old in s
open(p,'w').write(s.replace(old,new,1))
PY
gate_axis "близнец 2: та же величина через промежуточную переменную — гейт молчит" green
restore

echo "== ось 3: перепись входов снята =="
pyedit "перепись входов снята — гейт краснеет и называет пропавшее" "$GEN_CAT" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='echo "корень контрактов: ${PROTO_ROOT}"\n'
assert old in s, 'ось 3: место инъекции не найдено'
open(p,'w').write(s.replace(old,'',1))
PY
gate_axis "перепись входов снята — гейт краснеет и называет пропавшее" red "не называет «корень контрактов»"
restore
pyedit "близнец 3: перепись дополнена лишней строкой — гейт молчит" "$GEN_CAT" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='echo "корень контрактов: ${PROTO_ROOT}"\n'
assert old in s
open(p,'w').write(s.replace(old,old+'echo "выход: ${OUT}"\n',1))
PY
gate_axis "близнец 3: перепись дополнена лишней строкой — гейт молчит" green
restore

echo "== ось 4: замыкание импортов снято =="
# Отбор здесь НЕ умолчательный, и это несущее решение, а не вкус.
#
# Инъекция оси выбрасывает замыкание импортов — значит её предмет есть ровно то,
# что замыкание ДОБИРАЕТ сверх объявленного отбора. У умолчательного отбора
# (`iam operation`) замыкание не добирает НИЧЕГО, поэтому выброс замыкания там
# ничего не меняет, гейт законно зелен, а харнесс объявлял «дефект возвращён, а
# гейт ЗЕЛЁНЫЙ — он не способен упасть»: ЛОЖНОЕ обвинение проверке, у инъекции
# которой не было предмета.
#
# Замер, которым выбран отбор (перемеряй, а не помни):
#   for sel in iam "iam operation" vpc compute registry geo; do
#     KACHO_GEN_DOMAINS="$sel" gateway/scripts/gen-permission-catalog.sh /dev/null 2>&1 |
#       grep 'добрано замыканием'
#   done
# `iam` → operation · `iam operation` → (нечего) · `vpc` → operation quota reference.
# `iam` в одиночку не годится ДРУГИМ концом: замыкание добирает им `operation`, а
# он ЭМИТИРУЕТ, и на чистом дереве краснеет A7 («необъявленных доменов 1») — ось
# была бы красной без всякой инъекции. У `vpc operation` замыкание добирает
# `quota` и `reference`, оба не эмитируют ни одной записи каталога: чистое дерево
# зелено, а предмет у инъекции есть.
#
# ПРЕДМЕТ ПРОВЕРЯЕТСЯ ЗДЕСЬ ЖЕ: отбор, чьё замыкание опустело, обязан уронить
# харнесс, а не тихо превратить ось в вакуумную.
AXIS4_DOMAINS="vpc operation"
axes=$((axes + 1))
# Вердикт НЕ берётся из трубы: харнесс идёт под `pipefail`, а `grep -q` закрывает
# трубу на первом совпадении — пишущий получает SIGPIPE, код пайплайна 141, и
# УСПЕШНОЕ совпадение читается как отказ. Тот же класс уже чинился на оси A10
# самого гейта; здесь он был воспроизведён заново.
KACHO_GEN_DOMAINS="$AXIS4_DOMAINS" "$GEN_CAT" "$(mktemp -d)/probe.json" >"$LOG" 2>&1 || true
if grep -q 'добрано замыканием импортов: [^(]' "$LOG"; then
    echo "  ✓ предмет оси 4: замыкание отбора '$AXIS4_DOMAINS' непусто"
else
    echo "  ✗ предмет оси 4: замыкание отбора '$AXIS4_DOMAINS' ПУСТО — инъекции нечего ломать," \
         "ось стала бы вакуумной; выбери отбор с непустым замыканием" >&2
    fails=$((fails + 1))
fi
pyedit "замыкание импортов снято — гейт краснеет" "$LIB" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='  for name in "${closure_order[@]}"; do'
assert old in s, 'ось 4: место инъекции не найдено'
open(p,'w').write(s.replace(old,'  for name in "${selected[@]}"; do  # ИНЪЕКЦИЯ: замыкание выброшено',1))
PY
GATE_DOMAINS="$AXIS4_DOMAINS" \
  gate_axis "замыкание импортов снято — гейт краснеет" red "не замкнута по импортам"
restore
pyedit "близнец 4: то же замыкание в обратном порядке копирования — гейт молчит" "$LIB" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='  for name in "${closure_order[@]}"; do'
new='  for name in $(printf \'%s\\n\' "${closure_order[@]}" | sort -r); do  # близнец: то же множество, обратный порядок'
assert old in s
open(p,'w').write(s.replace(old,new,1))
PY
GATE_DOMAINS="$AXIS4_DOMAINS" \
  gate_axis "близнец 4: то же замыкание в обратном порядке копирования — гейт молчит" green
restore

echo "== ось 5: побайтовое равенство с вшитым =="
pyedit "отступ вывода плагина сменён — гейт краснеет на побайтовой сверке" "$PLUGIN" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='json.MarshalIndent(entries, "", "  ")'
assert old in s, 'ось 5: место инъекции не найдено'
open(p,'w').write(s.replace(old,'json.MarshalIndent(entries, "", "   ")',1))
PY
gate_axis "отступ вывода плагина сменён — гейт краснеет на побайтовой сверке" red "разошёлся с вшитым"
restore
pyedit "близнец 5: комментарий в плагине — гейт молчит" "$PLUGIN" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='func main() {'
assert old in s
open(p,'w').write(s.replace(old,'// близнец: комментарий, не меняющий вывода.\nfunc main() {',1))
PY
gate_axis "близнец 5: комментарий в плагине — гейт молчит" green
restore

echo "== ось 6: утверждение генератора об объявленном домене (свой предмет) =="
gen_axis "отбор 'vpc' тянет замыканием эмитирующие домены — генератор отказывает и называет их" \
    1 "vpc" "эмитированы домены вне отбора:"
gen_axis "близнец 6: те же домены ОБЪЯВЛЕНЫ — генератор проходит" \
    0 "vpc iam operation quota"
gen_axis "отбор называет несуществующий домен — генератор отказывает" \
    1 "iam nosuchdomain" "выбранного домена 'nosuchdomain' нет в дереве контрактов"

echo "== ось 7: утверждение снято ИЗ ГЕНЕРАТОРА — ловит ли гейт =="
pyedit "ось 7: утверждение снято ИЗ ГЕНЕРАТОРА — ловит ли гейт" "$GEN_CAT" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='if [[ -n "${GEN_DOMAINS// /}" ]]; then\n  undeclared=""'
assert old in s, 'ось 7: место инъекции не найдено'
open(p,'w').write(s.replace(old,'if false; then  # ИНЪЕКЦИЯ: утверждение генератора снято\n  undeclared=""',1))
PY
axes=$((axes + 1))
if ( cd "$GW" && ./scripts/check-domain-generation.sh vpc ) >"$LOG" 2>&1; then
    echo "  ✗ ось 7: утверждение снято, гейт ЗЕЛЁНЫЙ на отборе 'vpc' — предмет никем не держится" >&2
    fails=$((fails + 1))
elif grep -qF "необъявленных доменов" "$LOG"; then
    echo "  ✓ ось 7: гейт ловит необъявленный домен и БЕЗ утверждения генератора"
else
    echo "  ✗ ось 7: гейт покраснел, но не назвал необъявленный домен" >&2
    grep 'НАХОДКА' "$LOG" | head -3 >&2; fails=$((fails + 1))
fi
restore

echo "== ось 8: разбор импортов ослеп — замыкание молча пусто =="
# Возвращается ИСХОДНЫЙ дефект: перечень корней склеен знаком `|` и подставлен
# в s-команду sed с тем же знаком в разделителе. sed умирает на каждом вызове,
# замыкание возвращает пустоту, стадия собирается неполной — а перепись честно
# печатает «добрано: (нечего)», то есть форму, НЕОТЛИЧИМУЮ от «ничего не нужно».
pyedit "разбор импортов ослеп — гейт краснеет и называет ПРИЧИНУ, а не симптом" "$LIB" <<'PYX'
import sys
p = sys.argv[1]; s = open(p, encoding='utf-8').read()
old = '    | awk -F\'"\' \'{ n = split($2, seg, "/"); if (n >= 4 && seg[2] == "cloud" && seg[3] != "") print seg[3] }\' \\'
new = '    | sed -E "s|.*\\"(${alt})/cloud/([^/\\"]+)/.*|\\\\2|" \\'
assert old in s, 'ось 8: место инъекции не найдено'
open(p, 'w', encoding='utf-8').write(s.replace(old, new, 1))
PYX
gate_axis "разбор импортов ослеп — гейт краснеет и называет ПРИЧИНУ, а не симптом" \
    red "разбор импортов дерева"
restore
pyedit "близнец 8: то же извлечение другой записью — гейт молчит" "$LIB" <<'PYX'
import sys
p = sys.argv[1]; s = open(p, encoding='utf-8').read()
old = '    | awk -F\'"\' \'{ n = split($2, seg, "/"); if (n >= 4 && seg[2] == "cloud" && seg[3] != "") print seg[3] }\' \\'
new = '    | awk -F\'"\' \'{ print $2 }\' | cut -d/ -f3 \\'
assert old in s, 'близнец 8: место правки не найдено'
open(p, 'w', encoding='utf-8').write(s.replace(old, new, 1))
PYX
gate_axis "близнец 8: то же извлечение другой записью — гейт молчит" green
restore

echo "== ось 9: распознаватель эмитированных доменов слеп на втором корне =="
# Возвращается ИСХОДНЫЙ дефект: корень сверяется с ЛИТЕРАЛОМ `kacho` вместо
# объявленного перечня. Домены второго корня выпадают из перечня ЦЕЛИКОМ, и
# утверждение «все эмитированные объявлены» (A7) выполняется тем вернее, чем
# шире слепота, — поэтому ловит это только A10, у которой независимая половина.
pyedit "распознаватель прибит к одному корню — гейт краснеет и называет корень" "$LIB" <<'PYX'
import sys
p = sys.argv[1]; s = open(p, encoding='utf-8').read()
old = '        { m = split($0, seg, "."); if (m >= 3 && (seg[1] in known) && seg[2] == "cloud" && seg[3] != "") print seg[1] "\\t" seg[3] }'
new = '        { m = split($0, seg, "."); if (m >= 3 && seg[1] == "kacho" && seg[2] == "cloud" && seg[3] != "") print seg[1] "\\t" seg[3] }'
assert old in s, 'ось 9: место инъекции не найдено'
open(p, 'w', encoding='utf-8').write(s.replace(old, new, 1))
PYX
gate_axis "распознаватель прибит к одному корню — гейт краснеет и называет корень" \
    red "распознаватель на нём слеп"
restore
pyedit "близнец 9: тот же набор корней в другом порядке — гейт молчит" "$LIB" <<'PYX'
import sys
p = sys.argv[1]; s = open(p, encoding='utf-8').read()
# Точка инъекции ВЫВОДИТСЯ из объявления, а не выписывается литералом: прежняя
# редакция матчила `KACHO_PROTO_ROOTS=(kacho kaname)` и пережила появление
# третьего корня — assert падал, правка не применялась, ось печатала ✓.
import re
m = re.search(r'^KACHO_PROTO_ROOTS=\(([^)]*)\)$', s, re.M)
assert m, 'близнец 9: объявление KACHO_PROTO_ROOTS не найдено'
roots = m.group(1).split()
assert len(roots) > 1, 'близнец 9: корень один — перестановке нечего менять, ось беспредметна'
new = 'KACHO_PROTO_ROOTS=(%s)' % ' '.join(reversed(roots))
open(p, 'w', encoding='utf-8').write(s.replace(m.group(0), new, 1))
PYX
gate_axis "близнец 9: тот же набор корней в другом порядке — гейт молчит" green
restore

echo "== ось 10: нейтральный корень стадии неполон (A12) =="
# Пара строится ВОКРУГ ТРЕТЬЕГО ПОДДЕРЕВА нейтрального корня, потому что
# изолировать A12 иначе нельзя: оба сегодняшних поддерева (`corelib/authz`,
# `corelib/api`) КТО-ТО ИМПОРТИРУЕТ, и выброс любого из них уронил бы заодно
# A11 и A1 — красное пришло бы от соседей, а способность A12 падать осталась бы
# недоказанной («инъекция обязана ронять ТОЛЬКО проверяемое»).
#
# Третье поддерево, которого никто не импортирует, разводит оси начисто:
#   · раскладчик выведен из дерева → поддерево в стадии → молчат ВСЕ оси;
#   · раскладчик вернулся к рукописному перечню → поддерева в стадии нет →
#     краснеет РОВНО A12 и называет координату, а A11/A1/A4 молчат, потому что
#     импорта нет и выход не изменился.
#
# Второй конец того же опыта (поддерево ИМПОРТИРУЕТСЯ) здесь не воспроизводится:
# он показывает не способность A12 падать, а цену её отсутствия — 8 находок по
# четырём осям, где A1 говорит «порождение полного каталога отказало». Эта
# половина записана в комментарии оси A12 самого гейта.
PROBE_TREE="$ROOT/proto/corelib/zz_injection_probe"
cleanup_probe() { rm -rf "$PROBE_TREE"; }
trap 'restore; cleanup_probe; rm -rf "$BACKUP"' EXIT

mkdir -p "$PROBE_TREE/v1"
cat > "$PROBE_TREE/v1/probe.proto" <<'PROTO'
syntax = "proto3";
package corelib.zz_injection_probe.v1;
option go_package = "github.com/PRO-Robotech/kacho/pkg/api/corelib/zz_injection_probe/v1;probev1";
// Синтетический предмет инъекции: поддерево нейтрального корня, которого никто
// не импортирует. Заводится и снимается harness'ом; в дереве не остаётся.
message InjectionProbe { string note = 1; }
PROTO
if [ ! -f "$PROBE_TREE/v1/probe.proto" ]; then
    echo "  ✗ ось 10: синтетическое поддерево не заведено — предмета инъекции нет" >&2
    fails=$((fails + 1))
fi

# законный близнец идёт ПЕРВЫМ: он утверждает, что само по себе третье поддерево
# зелёное, — иначе красное следующей проверки было бы неотличимо от «харнесс
# сломал дерево».
gate_axis "близнец 10: третье поддерево нейтрального корня в дереве и в стадии — гейт молчит" green

pyedit "ось 10: рукописный перечень нейтральных поддеревьев вернулся" "$LIB" <<'PYX'
import sys
p = sys.argv[1]; s = open(p, encoding='utf-8').read()
old = (
    '  local _neutral _staged_neutral=0\n'
    '  for _neutral in "${KACHO_PROTO_ROOTS[@]}"; do\n'
    '    [[ -d "${proto_root}/${_neutral}" ]]       || continue\n'
    '    [[ -d "${proto_root}/${_neutral}/cloud" ]] && continue\n'
    '    rm -rf "${stage:?}/${_neutral}"\n'
    '    cp -R "${proto_root}/${_neutral}" "${stage}/${_neutral}"\n'
    '    _staged_neutral=$((_staged_neutral + 1))\n'
    '  done'
)
assert old in s, 'ось 10: вывод перечня нейтральных поддеревьев не найден'
# ИНЪЕКЦИЯ: перечень снова ВЫПИСАН — ровно две строки, как было до kacho#1110
new = (
    '  local _neutral _staged_neutral=1\n'
    '  mkdir -p "${stage}/corelib/authz" "${stage}/corelib/api"\n'
    '  cp -R "${proto_root}/corelib/authz/v1" "${stage}/corelib/authz/v1"\n'
    '  cp -R "${proto_root}/corelib/api/v1"   "${stage}/corelib/api/v1"'
)
open(p, 'w', encoding='utf-8').write(s.replace(old, new, 1))
PYX
gate_axis "рукописный перечень вернулся — A12 краснеет и называет поддерево" \
    red "стадия не несёт поддеревьев нейтрального корня"
restore
cleanup_probe

echo "== ось 11: стадия наследует режим источника (A13) =="
# Пара разводит оси начисто: остальные оси читают ЗАПИСЫВАЕМЫЙ корень (рабочая
# копия), поэтому каталоги стадии у них 755 и снятие chmod-а их не касается —
# краснеет РОВНО A13, и это то, что требует «инъекция обязана ронять ТОЛЬКО
# проверяемое».
pyedit "ось 11: приведение режима стадии снято" "$LIB" <<'PYX'
import sys
p = sys.argv[1]; s = open(p, encoding='utf-8').read()
old = '  find "${stage}" -type d -exec chmod u+w {} +\n'
assert old in s, 'ось 11: приведение режима стадии не найдено'
# ИНЪЕКЦИЯ: стадия остаётся с режимом источника — как было до kacho#1110
open(p, 'w', encoding='utf-8').write(s.replace(old, '', 1))
PYX
gate_axis "режим стадии не приведён — A13 краснеет и называет причину" \
    red "стадия наследует режим источника"
restore
pyedit "близнец 11: то же приведение другой записью — гейт молчит" "$LIB" <<'PYX'
import sys
p = sys.argv[1]; s = open(p, encoding='utf-8').read()
old = '  find "${stage}" -type d -exec chmod u+w {} +\n'
assert old in s
# законный близнец: та же величина другой записью — обход через xargs
new = '  find "${stage}" -type d -print0 | xargs -0 chmod u+w\n'
open(p, 'w', encoding='utf-8').write(s.replace(old, new, 1))
PYX
gate_axis "близнец 11: то же приведение другой записью — гейт молчит" green
restore

echo "== ось 12: вход-выход не проверен до работы (A14) =="
# Инъекция снимает проверку ТОЛЬКО у генератора каталога; у генератора таблицы
# маршрутов она остаётся. Находка обязана назвать генератора поимённо — иначе
# «отказ есть» у одного из двух читалось бы как «есть у обоих» (тот же довод,
# что у A9).
pyedit "ось 12: ранняя проверка выхода снята у генератора каталога" "$GEN_CAT" <<'PYX'
import sys
p = sys.argv[1]; s = open(p, encoding='utf-8').read()
old = 'if ! mkdir -p "${OUT_DIR}" 2>/dev/null || [[ ! -w "${OUT_DIR}" ]]; then'
assert old in s, 'ось 12: ранняя проверка выхода не найдена'
# ИНЪЕКЦИЯ: проверка выхода вырождена в тождественно-ложное условие — код
# остаётся на месте, а отказать не может НИКОГДА (форма без содержания)
open(p, 'w', encoding='utf-8').write(s.replace(old, 'if false; then', 1))
PYX
gate_axis "проверка выхода вырождена — A14 краснеет и называет генератора" \
    red "gen-permission-catalog.sh/dir-absent"
restore
pyedit "близнец 12: то же условие через промежуточную величину — гейт молчит" "$GEN_CAT" <<'PYX'
import sys
p = sys.argv[1]; s = open(p, encoding='utf-8').read()
old = 'if ! mkdir -p "${OUT_DIR}" 2>/dev/null || [[ ! -w "${OUT_DIR}" ]]; then'
assert old in s
# законный близнец: то же условие, разложенное на две величины
new = ('out_dir_made=0\n'
       'mkdir -p "${OUT_DIR}" 2>/dev/null && out_dir_made=1\n'
       'if [[ "${out_dir_made}" -eq 0 || ! -w "${OUT_DIR}" ]]; then')
open(p, 'w', encoding='utf-8').write(s.replace(old, new, 1))
PYX
gate_axis "близнец 12: то же условие через промежуточную величину — гейт молчит" green
restore

echo "== контроль в обратную сторону: дерево восстановлено =="
gate_axis "восстановленное дерево — снова зелено" green

rm -f "$LOG"
echo "инъекция: осей прогнано ${axes}, провалов ${fails}"
[ "$fails" -eq 0 ] || exit 1
echo "инъекция: доказано — гейт и утверждения генератора способны упасть и молчат на законном"
