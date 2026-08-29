#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# selftest-assertions-inject.sh — доказательство того, что САМОПРОВЕРКА гейта
# честности утверждений (`selftest-assertions.js`, блок prove) СПОСОБНА упасть, и
# что рядом с каждым дефектом молчит законный близнец той же формы.
#
# Проверяется ПАРА на каждой оси: одностороннее доказательство («сломал — красное»)
# зеленеет на проверке, которая краснеет на всём. Поэтому у каждой инъекции назван
# не только перечень сообщений, которые ОБЯЗАНЫ прозвучать, но и перечень тех, что
# прозвучать НЕ ДОЛЖНЫ, — иначе красное могло бы приходить от соседней оси, а
# проверяемая осталась бы вакуумной.
#
# Оси ветки СВЯЗИ переустроены задачей о каскаде, поэтому здесь заново прогнаны и
# ПРЕЖНИЕ оси предиката A: совпадение переписи переустройство не заверяет.
#
# Дерево НЕ ПРАВИТСЯ ВОВСЕ: мутант живёт во временном каталоге, где `collections`
# — символическая ссылка на настоящие. Прерывание не может оставить исходник
# изменённым, потому что он не открывается на запись ни разу.
set -euo pipefail

cd "$(dirname "$0")/.."
GATE=scripts/selftest-assertions.js
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/scripts"
ln -s "$PWD/collections" "$TMP/collections"
MUT="$TMP/scripts/selftest-assertions.js"

pass=0; fail=0; axes=0
out=""

# mutate <python-выражение над строкой s> — кладёт изменённый гейт в мутанта.
# Замена ОБЯЗАНА состояться: подстановка, не нашедшая себя, дала бы «зелёное»,
# неотличимое от исправного гейта.
mutate() {
  python3 - "$GATE" "$MUT" "$1" "$2" <<'PY'
import sys
src, dst, old, new = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
s = open(src).read()
if old not in s:
    sys.stderr.write('ИНЪЕКЦИЯ НЕ ПРИМЕНЕНА: подстрока не найдена: %r\n' % old)
    sys.exit(3)
open(dst, 'w').write(s.replace(old, new, 1))
PY
}

run_mutant() { out=$(node "$MUT" 2>&1) && return 0 || return 1; }

# assert <ярлык> <want:red|green> [<обязано прозвучать>...] -- [<не должно>...]
assert() {
  local label="$1" want="$2"; shift 2
  local musts=() nots=() bucket=musts
  for a in "$@"; do
    if [ "$a" = "--" ]; then bucket=nots; continue; fi
    if [ "$bucket" = musts ]; then musts+=("$a"); else nots+=("$a"); fi
  done
  axes=$((axes+1))
  local got; if run_mutant; then got=green; else got=red; fi
  if [ "$got" != "$want" ]; then
    printf 'ОТКАЗ  %-52s ожидалось %s, получено %s\n' "$label" "$want" "$got"; fail=$((fail+1)); return
  fi
  local m
  for m in ${musts+"${musts[@]}"}; do
    if ! grep -qF -- "$m" <<<"$out"; then
      printf 'ОТКАЗ  %-52s исход верен, но не назван: %s\n' "$label" "$m"; fail=$((fail+1)); return
    fi
  done
  for m in ${nots+"${nots[@]}"}; do
    if grep -qF -- "$m" <<<"$out"; then
      printf 'ОТКАЗ  %-52s красное пришло от соседней оси: %s\n' "$label" "$m"; fail=$((fail+1)); return
    fi
  done
  printf 'ok     %-52s %s\n' "$label" "$got"; pass=$((pass+1))
}

# Сообщения самопроверки, по одному на ось предиката A.
K_A='не поймал возвращённый дефект (шаг принимает 200 и 409)'
K_B='ошибочно поймал законную форму (200 допущен'
K_V='ошибочно поймал законный толерантный негатив'
K_G='ошибочно поймал законную связанную форму'
K_D='не поймал публикацию исхода, которую ничто не связывает'
K_E='ошибочно поймал заявленную терпимость уборки'
K_ZH='принял маркер терпимости на шаге, который уборкой не является'
K_Z='засчитал КАСКАД за связь'
K_I='обвинил условную публикацию'
K_K='оборвал кейс на шаге-каскаде'

# Сообщения сверки шапки с вердиктом (задача #1429).
K_HDR_MISS='шапка НЕ называет предикат(ы) E'
K_HDR_EXTRA='шапка называет предикат(ы) Z'
K_HDR_BLIND='перечень предикатов из шапки не читается вовсе'

# Сообщения предиката F (задачи #1420, #1468).
K_F_A='не поймал возвращённый дефект (исчерпание бюджета прочего вида бесследно)'
K_F_B='ошибочно поймал законную форму прочего вида'
K_F_V='не поймал безусловный счётчик прочего вида'
K_F_G='не заметил, что объявленная полоса повтора вида не та'
K_F_D='не признал исправным ожидание сходимости состояния'
K_F_CROSS='судил ожидание чужим описанием вида вместо отказа'
K_F_KIND='вид ожидания не опознан'

echo "── контроль: гейт БЕЗ инъекции ──"
cp "$GATE" "$MUT"
assert 'неизменённый гейт — зелёный' green

echo "── ось 1 (переустроенная): второй вопрос снят — связью считается ЛЮБОЕ возражение ──"
mutate 'if (!stepObjectsInEveryWorld(st, before)) return true;' 'return true;'
assert 'каскад засчитан за связь — краснеет и называет' red "$K_Z" -- "$K_G" "$K_I" "$K_K" "$K_D" "$K_A"

echo "── ось 1, законный близнец: вопрос задан НЕ ТОМУ шагу (целевому вместо возразившего) ──"
mutate 'if (!stepObjectsInEveryWorld(st, before)) return true;' 'if (!stepObjectsInEveryWorld(target, before)) return true;'
assert 'чувствительность спрошена у цели — тот же каскад' red "$K_Z" -- "$K_G" "$K_I" "$K_K"

echo "── ось 1, вторая половина: кейс обрывается на первом же каскаде ──"
mutate 'if (!stepObjectsInEveryWorld(st, before)) return true;' 'if (!stepObjectsInEveryWorld(st, before)) return true;
      break;'
assert 'связь ЗА каскадом не увидена — краснеет' red "$K_K" -- "$K_Z" "$K_G" "$K_I" "$K_D"

echo "── ось 2: ветка связи снята вовсе — законная связь обвиняется ──"
mutate 'if (caseObjectsToTheRefusedOutcome(steps, st, ref)) {' 'if (false) {'
assert 'связь и «условная, но названная» обвинены' red "$K_G" "$K_I" "$K_K" -- "$K_Z" "$K_D"

echo "── ось 2 с другой стороны: всякое возражение объявлено каскадом ──"
mutate 'function stepObjectsInEveryWorld(step, envSnapshot) {' 'function stepObjectsInEveryWorld(step, envSnapshot) { return true;'
assert 'то же наблюдаемое, другой механизм' red "$K_G" "$K_I" "$K_K" -- "$K_Z" "$K_D"

echo "── ось 3 (прежняя): маркер терпимости признаётся на ЛЮБОМ шаге ──"
mutate "const TEARDOWN_STEP = /^(cleanup|teardown)/i;" "const TEARDOWN_STEP = /^/;"
assert 'маркер вне уборки — краснеет' red "$K_ZH" -- "$K_E" "$K_G" "$K_I" "$K_K" "$K_Z"

echo "── ось 3, законный близнец: маркер терпимости не признаётся вовсе ──"
mutate 'const declared = isTeardown && r.executed.some((n) => /best-effort/i.test(n));' 'const declared = false;'
assert 'уборка обвинена — краснеет' red "$K_E" -- "$K_ZH" "$K_G" "$K_I" "$K_K" "$K_Z"

echo "── ось 4 (прежняя): предикат A ослеп целиком ──"
mutate 'function checkCaseA(caseItem, steps) {' 'function checkCaseA(caseItem, steps) { if (steps) return null;'
assert 'все дефектные оси названы' red "$K_A" "$K_D" "$K_Z" "$K_ZH" -- "$K_G" "$K_I" "$K_K" "$K_B" "$K_V" "$K_E" "$K_HDR_MISS" "$K_HDR_EXTRA" "$K_HDR_BLIND" "$K_F_A" "$K_F_B" "$K_F_V" "$K_F_KIND"

echo "── ось 4 с другой стороны: мир успеха всегда объявлен различающим ──"
mutate 'let distinguishes = false;' 'let distinguishes = true;'
assert 'те же дефектные оси названы' red "$K_A" "$K_D" "$K_Z" "$K_ZH" -- "$K_G" "$K_I" "$K_K" "$K_B" "$K_V" "$K_E" "$K_HDR_MISS" "$K_HDR_EXTRA" "$K_HDR_BLIND" "$K_F_A" "$K_F_B" "$K_F_V" "$K_F_KIND"

echo "── ось 5 (шапка ↔ вердикт): из перечня снята запись предиката E ──"
mutate '//   E (ведомость ожидания окна прав)' '//   (ведомость ожидания окна прав)'
assert 'предикат вне перечня — краснеет и называет букву' red "$K_HDR_MISS" \
  -- "$K_HDR_EXTRA" "$K_HDR_BLIND" "$K_A" "$K_D" "$K_Z" "$K_ZH"

echo "── ось 5, законный близнец: переписан ТЕКСТ записи, буква на месте ──"
mutate '//   E (ведомость ожидания окна прав)' '//   E (след исчерпания бюджета ожидания)'
assert 'переписанное описание — сверка молчит' green -- "$K_HDR_MISS" "$K_HDR_EXTRA" "$K_HDR_BLIND"

echo "── ось 5 с другой стороны: в перечне запись, которой нет в вердикте ──"
mutate '//   E (ведомость ожидания окна прав)' '//   Z (запись без предиката) — такого накопителя вердикт не несёт
//   E (ведомость ожидания окна прав)'
assert 'запись пережила предмет — краснеет и называет букву' red "$K_HDR_EXTRA" \
  -- "$K_HDR_MISS" "$K_HDR_BLIND" "$K_A" "$K_Z"

echo "── ось 5, предпосылка: форма записи перечня сменилась — судить вслепую нельзя ──"
mutate 'const HEADER_ENTRY_RE = /^\/\/ {3}([A-Z]) \(/gm;' 'const HEADER_ENTRY_RE = /^\/\/ {9}([A-Z]) \(/gm;'
assert 'перечень не читается — отказ, а не тишина' red "$K_HDR_BLIND" \
  -- "$K_HDR_MISS" "$K_HDR_EXTRA"

echo "── ось 6 (предикат F): ослеп целиком — всякий вид объявлен исправным ──"
mutate 'function checkStepF(step, kind) {' 'function checkStepF(step, kind) { if (kind) return { verdict: 0 || "ok" };'
assert 'дефектные оси F названы' red "$K_F_A" "$K_F_V" "$K_F_G" "$K_F_CROSS" \
  -- "$K_F_B" "$K_F_D" "$K_HDR_MISS" "$K_A" "$K_Z" "$K_ZH"

echo "── ось 6, вторая половина: снята СПОКОЙНАЯ проба — ловится только исчерпание ──"
mutate 'const calm = ledgerAfter(step, kind.quiet, kind);' "const calm = { retriedFirst: false, count: 0, steps: '' };"
assert 'безусловный счётчик проходит — краснеет и называет' red "$K_F_V" \
  -- "$K_F_A" "$K_F_B" "$K_F_G" "$K_F_D" "$K_F_CROSS" "$K_HDR_MISS" "$K_A"

echo "── ось 7: у вида ожидания нет описания — отказ, а не тихий пропуск ──"
mutate 'const kind = waitKindOf(st);' "const kind = /-st\\d+\$/.test(st.name || '') ? undefined : waitKindOf(st);"
assert 'вид без описания назван по имени шага' red "$K_F_KIND" \
  -- "$K_F_A" "$K_F_B" "$K_F_V" "$K_F_G" "$K_F_D" "$K_HDR_MISS"

echo "── ось 7 с другой стороны: описание вида ЛОЖНО — спокойный ответ на деле переходный ──"
mutate '    quiet: { code: 409, body: ABORTED_BODY },' '    quiet: { code: 403, body: DENY_BODY },'
assert 'ложное описание — отказ судить, а не обвинение' red "$K_F_D" \
  -- "$K_F_A" "$K_F_B" "$K_F_V" "$K_F_CROSS" "$K_F_KIND" "$K_HDR_MISS"

echo "── ось 7, законный близнец: переименована МЕТКА вида, механика та же ──"
mutate "label: 'сходимость состояния'," "label: 'сходимость наблюдаемого состояния',"
assert 'метка вида — не предмет: F молчит' green -- "$K_F_A" "$K_F_B" "$K_F_V" "$K_F_G" "$K_F_D" "$K_F_CROSS" "$K_F_KIND"

echo
printf 'инъекция самопроверки: осей %d, подтверждено %d, отказов %d\n' "$axes" "$pass" "$fail"
if [ "$axes" -eq 0 ]; then
  echo 'ОТКАЗ: ни одной оси не прогнано — доказывать нечего'
  exit 2
fi
[ "$fail" -eq 0 ] || exit 1
