#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Инъекция для `scripts/release/breaking-since-release.sh` — предмет один: ТОЧКА
# ОТСЧЁТА РАЗЛИЧАЕТ «РАЗРЫВОВ НЕТ», «РАЗРЫВЫ ЕСТЬ» И «СРАВНИТЬ НЕ С ЧЕМ».
#
# ЗАЧЕМ. Сегодня опубликованных версий ноль, поэтому предмет на живом дереве
# отвечает «не выполнилось» и другого исхода показать не может. Утверждение
# «а когда версия появится, он различит разрыв» без доказательства есть
# обещание. Публиковать ради доказательства нельзя — действие необратимо.
#
# ПОДСТАВНОЙ ВХОД — ДВА, ПО ЧИСЛУ ИСТОЧНИКОВ, КОТОРЫЕ ПРЕДМЕТ СПРАШИВАЕТ:
#   * ФАЙЛОВЫЙ ПРОКСИ вместо модуля-прокси — отвечает на «какая версия старшая»;
#   * СИНТЕТИЧЕСКИЙ РЕПОЗИТОРИЙ вместо адреса на площадке — отвечает на «что
#     было в контрактах на той версии».
# Гоняется НАСТОЯЩИЙ скрипт целиком; в каждой оси меняется РОВНО ОДИН факт.
#
# ОСИ (у каждой отрицательной есть законный близнец, который обязан молчать):
#   1. база есть, разрывов нет                 → 0, вердикт patch
#   2. база есть, поле снято                   → 0, вердикт minor
#   3. опубликованных версий НОЛЬ              → 3  ← главная ось: «не знаю» ≠ «нет»
#   4. базы с таким именем в репозитории нет   → 3, НЕ 0 и НЕ 1
#   5. требуется minor, объявлен патч          → 1, назван требуемый бамп
#   6. требуется minor, объявлен минор         → 0  ← близнец оси 5
#   7. требуется patch, объявлена та же версия → 1  ← монотонность не обходится
#   8. требуется patch, объявлен патч          → 0  ← близнец оси 7
#   9. база названа аргументом                 → прокси не спрашивается вовсе
#
# ИСХОДЫ: 0 — все утверждения сошлись; 1 — есть провалившиеся; 2 — предмета нет.
set -uo pipefail

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUBJECT="$HERE/breaking-since-release.sh"
[ -x "$SUBJECT" ] || { echo "предмета нет либо он неисполняем: $SUBJECT" >&2; exit 2; }
command -v buf     >/dev/null 2>&1 || { echo "инструмента нет: buf" >&2; exit 2; }
command -v go      >/dev/null 2>&1 || { echo "инструмента нет: go" >&2; exit 2; }
command -v git     >/dev/null 2>&1 || { echo "инструмента нет: git" >&2; exit 2; }
command -v python3 >/dev/null 2>&1 || { echo "инструмента нет: python3" >&2; exit 2; }

tmp="$(mktemp -d)"
trap 'chmod -R u+w "$tmp" 2>/dev/null; rm -rf "$tmp"' EXIT

MOD="example.com/platform"
BASEV="v1.4.7"

passed=0; failed=0; fails=()
pass() { passed=$((passed + 1)); printf '  ✓ %s\n' "$1"; }
fail() { failed=$((failed + 1)); fails+=("$1"); printf '  ✗ %s\n     %s\n' "$1" "${2:-}"; }

# ── Синтетический репозиторий контрактов ────────────────────────────────────
# Помеченная ревизия несёт ДВА поля; рабочее дерево правится в каждой оси и
# отличается от неё ровно одним фактом.
REPO="$tmp/platform"
mkdir -p "$REPO/proto/x/v1"
cat > "$REPO/proto/buf.yaml" <<'Y'
version: v1
breaking:
  use: [FILE]
Y
cat > "$REPO/proto/x/v1/a.proto" <<'P'
syntax = "proto3";
package x.v1;
message A {
  string one = 1;
  string two = 2;
}
P
git -C "$REPO" init -q -b main
git -C "$REPO" add -A
git -C "$REPO" -c user.email=inject@local -c user.name=inject commit -qm "база контрактов"
git -C "$REPO" tag "$BASEV"

# ── Файловый прокси ─────────────────────────────────────────────────────────
# Заводится ПОЛНОСТЬЮ (list/.info/.mod): урезанный отвечал бы отказом по своей
# неполноте, и «не выполнилось» приходило бы от фикстуры, а не от предмета.
mkproxy() { # mkproxy <каталог> <версии-через-пробел|пусто>
    local dir="$1"; shift
    python3 - "$dir" "$MOD" "$*" <<'PY'
import os, sys
dir_, mod, vers = sys.argv[1], sys.argv[2], sys.argv[3].split()
vdir = os.path.join(dir_, *mod.split("/"), "@v")
os.makedirs(vdir, exist_ok=True)
open(os.path.join(vdir, "list"), "w").write("".join(v + "\n" for v in vers))
for v in vers:
    open(os.path.join(vdir, v + ".mod"), "w").write("module %s\n\ngo 1.21\n" % mod)
    open(os.path.join(vdir, v + ".info"), "w").write(
        '{"Version":"%s","Time":"2000-01-01T00:00:00Z"}' % v)
PY
}
PROXY_FULL="$tmp/proxy-full"; mkproxy "$PROXY_FULL" "$BASEV"
PROXY_EMPTY="$tmp/proxy-empty"; mkproxy "$PROXY_EMPTY" ""

# ── Правка рабочего дерева: тот же файл, один изменяемый факт ───────────────
tree_intact()  { cp "$REPO/proto/x/v1/a.proto" "$tmp/keep"; }
tree_restore() { cp "$tmp/keep" "$REPO/proto/x/v1/a.proto"; }
tree_break()   { sed -i '/string two = 2;/d' "$REPO/proto/x/v1/a.proto"; }
tree_intact

# run <прокси> <аргументы предмета...> → RC, OUT
run_subject() {
    local proxy="$1"; shift
    OUT="$(cd "$REPO" && \
        GOPROXY="file://$proxy" GONOSUMDB='*' GOSUMDB=off GOFLAGS= \
        KACHO_RELEASE_MODULE="$MOD" \
        KACHO_RELEASE_GIT_INPUT="$REPO/.git" \
        KACHO_RELEASE_PROTO_DIR=proto \
        "$SUBJECT" "$@" 2>&1)"
    RC=$?
}

assert_rc() { # assert_rc <имя> <ожидаемый> <полученный> <вывод>
    if [ "$2" = "$3" ]; then pass "$1 → $3"
    else fail "$1: ожидался код $2, получен $3" "$(printf '%s' "$4" | tail -6 | tr '\n' '|')"; fi
}
assert_out() { # assert_out <имя> <подстрока> <вывод>
    if printf '%s' "$3" | grep -Fq -- "$2"; then pass "$1"
    else fail "$1: в выводе нет '$2'" "$(printf '%s' "$3" | tail -6 | tr '\n' '|')"; fi
}

echo "инъекция точки отсчёта совместимости: подставной прокси + синтетические контракты"

# ── 1. База есть, разрывов нет ──────────────────────────────────────────────
tree_restore
run_subject "$PROXY_FULL"
assert_rc  "база есть, разрывов нет" 0 "$RC" "$OUT"
assert_out "вердикт patch напечатан" "release-minimum-bump=patch" "$OUT"

# ── 2. База есть, поле снято ────────────────────────────────────────────────
tree_restore; tree_break
run_subject "$PROXY_FULL"
assert_rc  "база есть, поле снято" 0 "$RC" "$OUT"
assert_out "вердикт minor напечатан" "release-minimum-bump=minor" "$OUT"

# ── 3. Опубликованных версий НОЛЬ → НЕ ВЫПОЛНИЛОСЬ ─────────────────────────
# ГЛАВНАЯ ОСЬ: сравнивать не с чем, и это НЕ «разрывов нет».
tree_restore
run_subject "$PROXY_EMPTY"
assert_rc "опубликованных версий ноль" 3 "$RC" "$OUT"
# КОД ВОЗВРАТА ЗДЕСЬ НЕДОСТАТОЧЕН, и это измерено: тройка достижима ДВУМЯ
# путями — «точки отсчёта нет» и «buf не сделал работы». Подмена, при которой
# отсутствие версий подставляет выдуманную базу, оставляла ось ЗЕЛЁНОЙ: buf
# спотыкался о несуществующий тег и давал ту же тройку. Утверждается ТЕКСТ.
assert_out "тройка пришла именно от отсутствия точки отсчёта" \
           "точки отсчёта не существует" "$OUT"

# ── 4. Базы с таким именем нет → buf не сделал работы → НЕ ВЫПОЛНИЛОСЬ ─────
tree_restore
run_subject "$PROXY_FULL" "v9.9.9"
assert_rc "названной базы в репозитории нет" 3 "$RC" "$OUT"

# ── 5. Требуется minor, объявлен патч → НАХОДКА ────────────────────────────
tree_restore; tree_break
run_subject "$PROXY_FULL" --require "v1.4.8"
assert_rc  "разрыв есть, объявлен патч" 1 "$RC" "$OUT"
assert_out "находка называет требуемое повышение" "minor" "$OUT"

# ── 6. Законный близнец: требуется minor, объявлен минор ───────────────────
tree_restore; tree_break
run_subject "$PROXY_FULL" --require "v1.5.0"
assert_rc "близнец: разрыв есть, объявлен минор" 0 "$RC" "$OUT"

# ── 7. Требуется patch, объявлена ТА ЖЕ версия → НАХОДКА ───────────────────
tree_restore
run_subject "$PROXY_FULL" --require "$BASEV"
assert_rc "разрывов нет, версия не сдвинута" 1 "$RC" "$OUT"

# ── 8. Законный близнец: требуется patch, объявлен патч ────────────────────
tree_restore
run_subject "$PROXY_FULL" --require "v1.4.8"
assert_rc "близнец: разрывов нет, объявлен патч" 0 "$RC" "$OUT"

# ── 9. База названа аргументом → прокси не спрашивается вовсе ──────────────
# Прокси подставляется ПУСТОЙ: если бы предмет всё равно за ним ходил, ось дала
# бы 3. Ноль — доказательство того, что названная база прокси не требует.
tree_restore
run_subject "$PROXY_EMPTY" "$BASEV"
assert_rc "база названа аргументом — прокси не нужен" 0 "$RC" "$OUT"

# ── Итог ───────────────────────────────────────────────────────────────────
printf '\nперепись: утверждений %d, провалено %d\n' "$((passed + failed))" "$failed"
if [ "$((passed + failed))" = "0" ]; then
    echo "обход пуст: не проверено ни одного утверждения" >&2; exit 2
fi
if [ "$failed" -gt 0 ]; then
    printf 'провалены: %s\n' "${fails[*]}" >&2
    echo "точка отсчёта совместимости не доказана" >&2
    exit 1
fi
echo "точка отсчёта различает три состояния и связывает номер версии с разрывом"
exit 0
