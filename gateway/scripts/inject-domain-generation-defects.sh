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

# gate_axis <имя> <red|green> [<обязательная подстрока находки>]
gate_axis() {
    local name="$1" want="$2" needle="${3:-}" rc
    axes=$((axes + 1))
    ( cd "$GW" && ./scripts/check-domain-generation.sh ) >"$LOG" 2>&1; rc=$?
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
python3 - "$LIB" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='  local selection_raw=${4:-}'
assert old in s, 'ось 1: место инъекции не найдено'
open(p,'w').write(s.replace(old,'  local selection_raw=""  # ИНЪЕКЦИЯ: отбор выброшен',1))
PY
gate_axis "отбор выброшен — гейт краснеет и называет ось" red "урезанное дерево несёт ВСЕ домены"
restore
python3 - "$GATE" <<'PY'
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
python3 - "$GEN_CAT" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='PROTO_ROOT="${KACHO_PROTO_ROOT:-${MONOREPO_ROOT}/proto}"'
assert old in s, 'ось 2: место инъекции не найдено'
open(p,'w').write(s.replace(old,'PROTO_ROOT="${MONOREPO_ROOT}/proto"  # ИНЪЕКЦИЯ: ручка игнорируется',1))
PY
gate_axis "корень контрактов прибит — гейт краснеет и называет ось" red "ручка KACHO_PROTO_ROOT не читается"
restore
python3 - "$GEN_CAT" <<'PY'
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
python3 - "$GEN_CAT" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='echo "корень контрактов: ${PROTO_ROOT}"\n'
assert old in s, 'ось 3: место инъекции не найдено'
open(p,'w').write(s.replace(old,'',1))
PY
gate_axis "перепись входов снята — гейт краснеет и называет пропавшее" red "не называет «корень контрактов»"
restore
python3 - "$GEN_CAT" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='echo "корень контрактов: ${PROTO_ROOT}"\n'
assert old in s
open(p,'w').write(s.replace(old,old+'echo "выход: ${OUT}"\n',1))
PY
gate_axis "близнец 3: перепись дополнена лишней строкой — гейт молчит" green
restore

echo "== ось 4: замыкание импортов снято =="
python3 - "$LIB" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='  for name in "${closure_order[@]}"; do'
assert old in s, 'ось 4: место инъекции не найдено'
open(p,'w').write(s.replace(old,'  for name in "${selected[@]}"; do  # ИНЪЕКЦИЯ: замыкание выброшено',1))
PY
gate_axis "замыкание импортов снято — гейт краснеет" red "НАХОДКА"
restore
python3 - "$LIB" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='  for name in "${closure_order[@]}"; do'
new='  for name in $(printf \'%s\\n\' "${closure_order[@]}" | sort -r); do  # близнец: то же множество, обратный порядок'
assert old in s
open(p,'w').write(s.replace(old,new,1))
PY
gate_axis "близнец 4: то же замыкание в обратном порядке копирования — гейт молчит" green
restore

echo "== ось 5: побайтовое равенство с вшитым =="
python3 - "$PLUGIN" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
old='json.MarshalIndent(entries, "", "  ")'
assert old in s, 'ось 5: место инъекции не найдено'
open(p,'w').write(s.replace(old,'json.MarshalIndent(entries, "", "   ")',1))
PY
gate_axis "отступ вывода плагина сменён — гейт краснеет на побайтовой сверке" red "разошёлся с вшитым"
restore
python3 - "$PLUGIN" <<'PY'
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
python3 - "$GEN_CAT" <<'PY'
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

echo "== контроль в обратную сторону: дерево восстановлено =="
gate_axis "восстановленное дерево — снова зелено" green

rm -f "$LOG"
echo "инъекция: осей прогнано ${axes}, провалов ${fails}"
[ "$fails" -eq 0 ] || exit 1
echo "инъекция: доказано — гейт и утверждения генератора способны упасть и молчат на законном"
