#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Инъекция для `scripts/release/publish-tag.sh` — предмет один: ПРОИЗВОДИТЕЛЬ
# ОТКАЗЫВАЕТ И НИЧЕГО НЕ СОЗДАЁТ, А ПУБЛИКУЕТ РОВНО ТОГДА, КОГДА ПОЗВАН.
#
# ЗАЧЕМ ИМЕННО СПОСОБНОСТЬ ОТКАЗАТЬ. Шаг необратим: модуль-прокси кэширует
# опубликованную версию и суммирует её в `go.sum` каждого потребителя. На
# исправном входе выродившаяся предпосылка выглядит РОВНО так же, как
# работающая, — разница видна только на входе, который обязан быть отвергнут.
#
# ПОДСТАВНЫЕ ВХОДЫ — ТРИ, И ГОНЯЕТСЯ НАСТОЯЩИЙ ОРКЕСТРАТОР:
#   * КАТАЛОГ-БЛИЗНЕЦ: настоящий `publish-tag.sh` копируется рядом с заглушками
#     трёх гейтов, каждая отвечает объявленным кодом. Оркестратор ищет соседей
#     по своему каталогу, поэтому подменяются ровно они, а он сам — нет;
#   * ГОЛЫЙ РЕПОЗИТОРИЙ вместо удалённого: на нём СЧИТАЮТСЯ ССЫЛКИ, то есть
#     утверждается не «скрипт сказал, что не создал», а «не создал»;
#   * ПОДСТАВНОЙ `gh` для заметки о выпуске.
#
# ОСИ (у каждой отрицательной есть законный близнец, который обязан молчать):
#   1. подтверждение не совпало            → 2, ссылок 0
#   2. гейт предпосылок красен             → 1, ссылок 0
#   3. гейт зелени ствола красен           → 1, ссылок 0
#   4. дельта контрактов красна            → 1, ссылок 0
#   5. гейт «не выполнилось»               → 3, ссылок 0  ← «не знаю» ≠ «да»
#   6. дельта не спрошена, версий НОЛЬ     → 0            ← первый выпуск законен
#   7. дельта не спрошена, версии ЕСТЬ     → 3, новых ссылок 0 ← близнец оси 6
#   8. всё зелено, ключа --publish нет     → 0, ссылок 0  ← умолчание не публикует
#   9. всё зелено, --publish               → 0, ссылок 1, и это refs/tags/<v> на HEAD
#  10. после публикации ЛОКАЛЬНОЙ ссылки нет            ← ради чего выбрана форма
#
# ИСХОДЫ: 0 — все утверждения сошлись; 1 — есть провалившиеся; 2 — предмета нет.
set -uo pipefail

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUBJECT="$HERE/publish-tag.sh"
[ -x "$SUBJECT" ] || { echo "предмета нет либо он неисполняем: $SUBJECT" >&2; exit 2; }
command -v git >/dev/null 2>&1 || { echo "инструмента нет: git" >&2; exit 2; }

tmp="$(mktemp -d)"
trap 'chmod -R u+w "$tmp" 2>/dev/null; rm -rf "$tmp"' EXIT

VER="v3.2.1"

passed=0; failed=0; fails=()
pass() { passed=$((passed + 1)); printf '  ✓ %s\n' "$1"; }
fail() { failed=$((failed + 1)); fails+=("$1"); printf '  ✗ %s\n     %s\n' "$1" "${2:-}"; }

# ── Каталог-близнец: настоящий предмет + заглушки трёх гейтов ───────────────
# Заглушка отвечает кодом из окружения и печатает своё имя: молчаливая заглушка
# сделала бы неотличимым «гейт позван и согласился» от «гейт не позван вовсе».
BIN="$tmp/bin"; mkdir -p "$BIN"
cp "$SUBJECT" "$BIN/publish-tag.sh"; chmod +x "$BIN/publish-tag.sh"
mkstub() { # mkstub <имя> <переменная-кода>
    cat > "$BIN/$1" <<STUB
#!/usr/bin/env bash
echo "заглушка $1 позвана: \$*"
exit \${$2:-0}
STUB
    chmod +x "$BIN/$1"
}
mkstub publish-version.sh        STUB_RC_PREREQ
mkstub assert-trunk-green.sh     STUB_RC_GREEN
mkstub breaking-since-release.sh STUB_RC_DELTA

# Подставной `gh`: заметка о выпуске обязана быть попыткой, а не тишиной.
cat > "$BIN/gh" <<'GH'
#!/usr/bin/env bash
echo "подставной gh: $*"
exit 0
GH
chmod +x "$BIN/gh"

# ── Фикстура репозиториев: голый «удалённый» + рабочая копия ────────────────
# mkfixture <каталог> <сколько-версий-уже-опубликовано>
mkfixture() {
    local base="$1" pre="$2"
    rm -rf "$base"; mkdir -p "$base"
    git init -q --bare "$base/remote.git"
    git init -q -b main "$base/work"
    ( cd "$base/work"
      printf 'module example.com/x\n\ngo 1.21\n' > go.mod
      git add -A
      git -c user.email=inject@local -c user.name=inject commit -qm base
      git push -q "$base/remote.git" main:refs/heads/main ) >/dev/null 2>&1
    if [ "$pre" -gt 0 ]; then
        local sha; sha="$(git -C "$base/work" rev-parse HEAD)"
        git -C "$base/work" push -q "$base/remote.git" "$sha:refs/tags/v0.0.1" >/dev/null 2>&1
    fi
}

refs_on_remote() { git ls-remote --tags "$1/remote.git" 2>/dev/null | grep -c . || true; }

# run <каталог-фикстуры> <аргументы предмета...> → RC, OUT
run_subject() {
    local base="$1"; shift
    OUT="$(cd "$base/work" && PATH="$BIN:$PATH" \
        KACHO_RELEASE_REMOTE="$base/remote.git" \
        STUB_RC_PREREQ="${RC_PREREQ:-0}" \
        STUB_RC_GREEN="${RC_GREEN:-0}" \
        STUB_RC_DELTA="${RC_DELTA:-0}" \
        "$BIN/publish-tag.sh" "$@" 2>&1)"
    RC=$?
}

assert_rc() { if [ "$2" = "$3" ]; then pass "$1 → $3"
    else fail "$1: ожидался код $2, получен $3" "$(printf '%s' "$4" | tail -6 | tr '\n' '|')"; fi; }
assert_refs() { if [ "$2" = "$3" ]; then pass "$1: ссылок на удалённом $3"
    else fail "$1: ожидалось ссылок $2, на удалённом $3" ""; fi; }

echo "инъекция производителя версии: настоящий оркестратор, подставные гейты и удалённый"

# ── 1. Подтверждение не совпало ────────────────────────────────────────────
F="$tmp/f1"; mkfixture "$F" 0
RC_PREREQ=0 RC_GREEN=0 RC_DELTA=0 run_subject "$F" "$VER" --confirm "v9.9.9" --publish
assert_rc   "подтверждение не совпало" 2 "$RC" "$OUT"
assert_refs "подтверждение не совпало" 0 "$(refs_on_remote "$F")"
if printf '%s' "$OUT" | grep -q 'заглушка'; then
    fail "отказ по подтверждению обязан наступать ДО гейтов" "заглушка была позвана"
else
    pass "отказ по подтверждению наступает до единого вызова гейтов"
fi

# ── 2. Гейт предпосылок красен ─────────────────────────────────────────────
F="$tmp/f2"; mkfixture "$F" 0
RC_PREREQ=1 RC_GREEN=0 RC_DELTA=0 run_subject "$F" "$VER" --confirm "$VER" --publish
assert_rc   "гейт предпосылок красен" 1 "$RC" "$OUT"
assert_refs "гейт предпосылок красен" 0 "$(refs_on_remote "$F")"

# ── 3. Гейт зелени ствола красен ───────────────────────────────────────────
F="$tmp/f3"; mkfixture "$F" 0
RC_PREREQ=0 RC_GREEN=1 RC_DELTA=0 run_subject "$F" "$VER" --confirm "$VER" --publish
assert_rc   "гейт зелени красен" 1 "$RC" "$OUT"
assert_refs "гейт зелени красен" 0 "$(refs_on_remote "$F")"

# ── 4. Дельта контрактов красна ────────────────────────────────────────────
F="$tmp/f4"; mkfixture "$F" 1
RC_PREREQ=0 RC_GREEN=0 RC_DELTA=1 run_subject "$F" "$VER" --confirm "$VER" --publish
assert_rc "дельта контрактов красна" 1 "$RC" "$OUT"
assert_refs "дельта контрактов красна" 1 "$(refs_on_remote "$F")"   # только предзаведённая

# ── 5. Гейт «НЕ ВЫПОЛНИЛОСЬ» → 3, а не 0 ───────────────────────────────────
F="$tmp/f5"; mkfixture "$F" 0
RC_PREREQ=3 RC_GREEN=0 RC_DELTA=0 run_subject "$F" "$VER" --confirm "$VER" --publish
assert_rc   "предпосылка не спрошена" 3 "$RC" "$OUT"
assert_refs "предпосылка не спрошена" 0 "$(refs_on_remote "$F")"

# ── 6. Дельта не спрошена, опубликованных версий НОЛЬ → законно ────────────
F="$tmp/f6"; mkfixture "$F" 0
RC_PREREQ=0 RC_GREEN=0 RC_DELTA=3 run_subject "$F" "$VER" --confirm "$VER"
assert_rc "первый выпуск: точки отсчёта нет и версий ноль" 0 "$RC" "$OUT"

# ── 7. Дельта не спрошена, версии ЕСТЬ → 3 (близнец оси 6) ─────────────────
F="$tmp/f7"; mkfixture "$F" 1
before="$(refs_on_remote "$F")"
RC_PREREQ=0 RC_GREEN=0 RC_DELTA=3 run_subject "$F" "$VER" --confirm "$VER" --publish
assert_rc   "близнец: версии есть, а дельта не спрошена" 3 "$RC" "$OUT"
assert_refs "близнец: новых ссылок нет" "$before" "$(refs_on_remote "$F")"

# ── 8. Всё зелено, ключа --publish нет → холостой, ничего не создано ───────
F="$tmp/f8"; mkfixture "$F" 0
RC_PREREQ=0 RC_GREEN=0 RC_DELTA=0 run_subject "$F" "$VER" --confirm "$VER"
assert_rc   "холостой прогон" 0 "$RC" "$OUT"
assert_refs "холостой прогон ничего не создал" 0 "$(refs_on_remote "$F")"

# ── 9-10. Всё зелено, --publish → ровно одна ссылка, и локальной не осталось ─
F="$tmp/f9"; mkfixture "$F" 0
HEAD_SHA="$(git -C "$F/work" rev-parse HEAD)"
RC_PREREQ=0 RC_GREEN=0 RC_DELTA=0 run_subject "$F" "$VER" --confirm "$VER" --publish
assert_rc   "публикация" 0 "$RC" "$OUT"
assert_refs "публикация создала ровно одну ссылку" 1 "$(refs_on_remote "$F")"
GOT="$(git ls-remote --tags "$F/remote.git" 2>/dev/null | awk '{print $1" "$2}')"
if [ "$GOT" = "$HEAD_SHA refs/tags/$VER" ]; then
    pass "ссылка указывает на HEAD и названа refs/tags/$VER"
else
    fail "ссылка не та" "получено: '$GOT', ожидалось: '$HEAD_SHA refs/tags/$VER'"
fi
if git -C "$F/work" rev-parse -q --verify "refs/tags/$VER" >/dev/null 2>&1; then
    fail "после публикации осталась ЛОКАЛЬНАЯ ссылка" "форма <sha>:refs/tags/<v> её оставлять не должна"
else
    pass "локальной ссылки не осталось — побочный push --tags ничего не увезёт"
fi

# ── Итог ───────────────────────────────────────────────────────────────────
printf '\nперепись: утверждений %d, провалено %d\n' "$((passed + failed))" "$failed"
if [ "$((passed + failed))" = "0" ]; then
    echo "обход пуст: не проверено ни одного утверждения" >&2; exit 2
fi
if [ "$failed" -gt 0 ]; then
    printf 'провалены: %s\n' "${fails[*]}" >&2
    echo "производитель версии не доказан" >&2
    exit 1
fi
echo "производитель отказывает без следа и публикует ровно одну ссылку по вызову"
exit 0
