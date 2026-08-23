#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# inject-session-cutoff-defects.sh — доказательство, что пробы полосы отзыва на
# БРАУЗЕРНОЙ сессии СПОСОБНЫ упасть, и что законное дерево они пропускают.
#
# Проба поведения доказывается не чтением, а возвратом дефекта: по одной оси за
# раз дефект вносится в прод-код, названная проба обязана покраснеть И назвать
# предмет. Контроль в обратную сторону — нетронутое дерево — обязан быть зелёным;
# без него «краснеет на всём» неотличимо от «краснеет на дефекте».
#
# Исходов ТРИ, и третий не вычитается из вердикта: 0 — доказано · 1 — проба не
# упала на возвращённом дефекте (или упала на законном дереве) · 2 — прогон не
# выполнен (дерево грязное, инструмента нет, правка не применилась).
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LANE="$ROOT/gateway/internal/middleware/auth_session_cutoff.go"
AUTH="$ROOT/gateway/internal/middleware/auth.go"
ME="$ROOT/gateway/internal/middleware/session_identity_handler.go"
PKG="./gateway/internal/middleware/"

for f in "$LANE" "$AUTH" "$ME"; do
    [ -f "$f" ] || { echo "ОТКАЗ: нет $f — предмета инъекции не существует" >&2; exit 2; }
done

# Грязное дерево — не «инъекция не сработала», а «вердикт о чужой правке».
if [ -n "$(git -C "$ROOT" status --porcelain -- "$LANE" "$AUTH" "$ME")" ]; then
    DIRTY=1
else
    DIRTY=0
fi

BACKUP="$(mktemp -d)"
cp "$LANE" "$BACKUP/lane.go"; cp "$AUTH" "$BACKUP/auth.go"; cp "$ME" "$BACKUP/me.go"
restore() { cp "$BACKUP/lane.go" "$LANE"; cp "$BACKUP/auth.go" "$AUTH"; cp "$BACKUP/me.go" "$ME"; }
trap 'restore; rm -rf "$BACKUP"' EXIT

fails=0
axes=0

# run_axis <имя> <ожидаемый исход: red|green> <проба> — прогон одной оси.
run_axis() {
    local name="$1" want="$2" run="$3"
    axes=$((axes + 1))
    local out rc
    out="$(cd "$ROOT" && go test "$PKG" -run "$run" -count=1 2>&1)"; rc=$?
    if [ "$want" = red ] && [ "$rc" -eq 0 ]; then
        echo "  ✗ $name: дефект возвращён, а проба ЗЕЛЁНАЯ — она не способна упасть" >&2
        fails=$((fails + 1)); return
    fi
    if [ "$want" = green ] && [ "$rc" -ne 0 ]; then
        echo "  ✗ $name: законное дерево, а проба КРАСНАЯ — она ловит форму, а не существо" >&2
        echo "$out" | tail -5 >&2
        fails=$((fails + 1)); return
    fi
    echo "  ✓ $name"
}

echo "== контроль в обратную сторону: нетронутое дерево"
run_axis "законное дерево молчит" green 'TestCookieLane|TestBrowserSessionLanes'

echo "== ось 1: полоса личности перестаёт спрашивать про отзыв"
sed -i 's|^\tif a.sessionCutoff == nil {$|\tif true \|\| a.sessionCutoff == nil {|' "$LANE"
grep -q 'if true ||' "$LANE" || { echo "ОТКАЗ: правка оси 1 не применилась" >&2; exit 2; }
run_axis "отозванная сессия проходит" red 'TestCookieLane_SessionAtOrBeforeCutoffIsRefused'
restore

echo "== ось 2: отказ перестаёт заканчивать носителя"
python3 - "$AUTH" <<'PY'
import sys
p=sys.argv[1]; s=open(p,encoding='utf-8').read()
old="\t\tendSessionCarrier(w)\n"
assert s.count(old)==1, "якорь оси 2 не найден"
open(p,'w',encoding='utf-8').write(s.replace(old,""))
PY
run_axis "стоящий отказ не отличим от выхода" red 'TestCookieLane_SessionAtOrBeforeCutoffIsRefused'
restore

echo "== ось 3: молчащий авторитет становится мягким проходом"
sed -i 's|^\t\treturn sessionCutoffUnanswered$|\t\treturn sessionCutoffLive|' "$LANE"
grep -q 'return sessionCutoffLive$' "$LANE" || { echo "ОТКАЗ: правка оси 3 не применилась" >&2; exit 2; }
run_axis "неотвеченный вопрос пропускает" red 'TestCookieLane_UnansweredAuthorityRefusesButKeepsCarrier'
restore

echo "== ось 4: граница отсечки становится исключающей"
sed -i 's|if sess.AuthenticatedAt.After(cutoff) {|if !sess.AuthenticatedAt.Before(cutoff) {|' "$LANE"
grep -q '!sess.AuthenticatedAt.Before(cutoff)' "$LANE" || { echo "ОТКАЗ: правка оси 4 не применилась" >&2; exit 2; }
run_axis "совпадение моментов проходит" red 'TestCookieLane_SessionExactlyAtCutoffIsRefused'
restore

echo "== ось 5: маршрут «кто я» перестаёт спрашивать (полосы расходятся)"
python3 - "$ME" <<'PY'
import sys
p=sys.argv[1]; s=open(p,encoding='utf-8').read()
old="\tif h.sessionCutoff == nil || subj.Type != \"user\" || subj.ID == \"\" {\n"
assert s.count(old)==1, "якорь оси 5 не найден"
open(p,'w',encoding='utf-8').write(s.replace(old,"\tif true {\n",1))
PY
run_axis "полосы расходятся молча" red 'TestBrowserSessionLanesAgree'
restore

echo "== ось 6: окно раската схлопывается в отказ (консоль лежит весь раскат)"
sed -i 's|^\t\treturn sessionCutoffUnsupported$|\t\treturn sessionCutoffUnanswered|' "$LANE"
grep -q 'предикат' "$LANE" || { echo "ОТКАЗ: файл оси 6 неузнаваем" >&2; exit 2; }
grep -q 'return sessionCutoffUnsupported$' "$LANE" && { echo "ОТКАЗ: правка оси 6 не применилась" >&2; exit 2; }
run_axis "расхождение версий читается как отказ" red 'TestCookieLane_UnsupportedAuthorityPassesLoudly'
restore

echo
echo "перепись: осей осмотрено $axes · не подтвердилось $fails"
if [ "$axes" -eq 0 ]; then
    echo "ОТКАЗ: осмотрено ноль осей — доказывать было нечего" >&2
    exit 2
fi
if [ "$DIRTY" -eq 1 ]; then
    echo "ОТКАЗ: дерево было грязным до прогона — вердикт относится к чужой правке" >&2
    exit 2
fi
[ "$fails" -eq 0 ] || exit 1
echo "доказано: каждая ось краснеет на возвращённом дефекте, законное дерево молчит"
