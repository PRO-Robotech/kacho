#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Инъекция для `scripts/release/summarize-run.sh` — предмет один: СВОДКА
# ГОВОРИТ О ФАКТЕ НА УДАЛЁННОМ, А НЕ О ТОМ, ЧТО ЕЙ СКАЗАЛИ НА ВХОДЕ.
#
# ЗАЧЕМ. Сводку читает оператор, и читает ПОСЛЕ необратимого шага. Сводка,
# повторяющая вход («просили опубликовать → значит опубликовано»), лжёт ровно в
# том случае, ради которого её читают: гейт отказал, ссылки нет, а строка
# говорит «да». Отличить такую сводку от честной можно только входом, где вход и
# факт РАСХОДЯТСЯ.
#
# ВТОРОЕ УТВЕРЖДЕНИЕ — СВОДКА НЕ РОНЯЕТ ПРОГОН. Она идёт при любом исходе; шаг
# сводки, способный отказать, скрыл бы исход самого выпуска за своим собственным.
#
# ОСИ:
#   1. просили публиковать, ссылки на удалённом НЕТ → «нет», код 0 ← главная ось
#   2. не просили, ссылка ЕСТЬ (осталась от прошлого)→ «да», код 0 ← близнец наоборот
#   3. ссылка есть → печатается команда бампа потребителя
#   4. ссылка есть → называется, что образы по имени версии не соберутся
#   5. удалённый недостижим                          → код 0, «нет»
#   6. позван неверно                                → код 2
#
# ИСХОДЫ: 0 — все утверждения сошлись; 1 — есть провалившиеся; 2 — предмета нет.
set -uo pipefail

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUBJECT="$HERE/summarize-run.sh"
[ -x "$SUBJECT" ] || { echo "предмета нет либо он неисполняем: $SUBJECT" >&2; exit 2; }
command -v git >/dev/null 2>&1 || { echo "инструмента нет: git" >&2; exit 2; }

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
VER="v7.7.7"

passed=0; failed=0; fails=()
pass() { passed=$((passed + 1)); printf '  ✓ %s\n' "$1"; }
fail() { failed=$((failed + 1)); fails+=("$1"); printf '  ✗ %s\n     %s\n' "$1" "${2:-}"; }

# mkfixture <каталог> <есть-ли-ссылка:0|1>
mkfixture() {
    local base="$1" have="$2"
    rm -rf "$base"; mkdir -p "$base"
    git init -q --bare "$base/remote.git"
    git init -q -b main "$base/work"
    ( cd "$base/work"; : > f; git add -A
      git -c user.email=i@l -c user.name=i commit -qm base
      git push -q "$base/remote.git" main:refs/heads/main ) >/dev/null 2>&1
    if [ "$have" = 1 ]; then
        git -C "$base/work" push -q "$base/remote.git" \
            "$(git -C "$base/work" rev-parse HEAD):refs/tags/$VER" >/dev/null 2>&1
    fi
}

# run <каталог> <версия> <публиковали> → RC, SUM (содержимое сводки)
run_subject() {
    local base="$1"; shift
    local sum="$base/summary.md"; : > "$sum"
    ( cd "$base/work" && GITHUB_STEP_SUMMARY="$sum" \
        KACHO_RELEASE_REMOTE="${REMOTE_OVERRIDE:-$base/remote.git}" \
        "$SUBJECT" "$@" ) >/dev/null 2>&1
    RC=$?
    SUM="$(cat "$sum")"
}

assert_rc()  { if [ "$2" = "$3" ]; then pass "$1 → $3"
    else fail "$1: ожидался код $2, получен $3" ""; fi; }
assert_has() { if [[ "$3" == *"$2"* ]]; then pass "$1"
    else fail "$1: в сводке нет '$2'" "$(printf '%s' "$3" | tr '\n' '|' | cut -c1-200)"; fi; }
assert_not() { if [[ "$3" == *"$2"* ]]; then
        fail "$1: в сводке есть '$2', а не должно" "$(printf '%s' "$3" | tr '\n' '|' | cut -c1-200)"
    else pass "$1"; fi; }

echo "инъекция сводки прогона: вход и факт расходятся намеренно"

# ── 1. Просили публиковать, ссылки НЕТ → сводка обязана сказать «нет» ───────
F="$tmp/s1"; mkfixture "$F" 0
REMOTE_OVERRIDE="" run_subject "$F" "$VER" true
assert_rc  "просили публиковать, ссылки нет" 0 "$RC"
assert_has "сводка говорит «нет» вопреки входу" '| ссылка на удалённом | **нет** |' "$SUM"
assert_not "и не обещает бамп потребителю"    'go get github.com/PRO-Robotech/kacho@' "$SUM"

# ── 2. Близнец наоборот: не просили, а ссылка ЕСТЬ ─────────────────────────
F="$tmp/s2"; mkfixture "$F" 1
REMOTE_OVERRIDE="" run_subject "$F" "$VER" false
assert_rc  "не просили, ссылка есть" 0 "$RC"
assert_has "сводка говорит «да» вопреки входу" '| ссылка на удалённом | **да** |' "$SUM"

# ── 3-4. Ссылка есть → сказано, что делать дальше и чего НЕ будет ──────────
assert_has "напечатана команда бампа потребителя" "go get github.com/PRO-Robotech/kacho@$VER" "$SUM"
assert_has "названо, что образы по имени версии не соберутся" 'Образы по имени версии' "$SUM"

# ── 5. Удалённый недостижим → «нет», но прогон не роняется ─────────────────
F="$tmp/s5"; mkfixture "$F" 1
REMOTE_OVERRIDE="$tmp/no-such-remote.git" run_subject "$F" "$VER" true
assert_rc  "удалённый недостижим" 0 "$RC"
assert_has "недостижимый удалённый читается как «нет»" '| ссылка на удалённом | **нет** |' "$SUM"

# ── 6. Позван неверно ──────────────────────────────────────────────────────
F="$tmp/s6"; mkfixture "$F" 0
( cd "$F/work" && "$SUBJECT" "$VER" ) >/dev/null 2>&1; RC=$?
assert_rc "позван без второго довода" 2 "$RC"

printf '\nперепись: утверждений %d, провалено %d\n' "$((passed + failed))" "$failed"
if [ "$((passed + failed))" = "0" ]; then echo "обход пуст" >&2; exit 2; fi
if [ "$failed" -gt 0 ]; then
    printf 'провалены: %s\n' "${fails[*]}" >&2
    echo "сводка не доказана: она может пересказывать вход вместо факта" >&2
    exit 1
fi
echo "сводка читает удалённый, а не свой вход, и прогона не роняет"
exit 0
