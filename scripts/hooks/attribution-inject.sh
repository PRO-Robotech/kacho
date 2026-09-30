#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# attribution-inject.sh — проба правила атрибуции (kacho-workspace#861): его
# предиката (scripts/hooks/attribution-rule.sh) и двух потребителей — хука
# коммита (scripts/hooks/commit-msg) и стража отправки
# (scripts/hooks/prepush-attribution.sh, его зовёт scripts/hooks/pre-push).
#
# Каждое свойство — парой: дефект → коммит не записан / ссылка не доехала, код
# не 0, строка и sha названы; законный близнец, отличный в один факт, → записан /
# доехала, код 0. Коммиты — настоящий `git commit` сквозь переходник, который
# кладёт `bash scripts/hooks/install.sh install` (цель `make install-hooks`);
# отправки — настоящий `git push` в голый репозиторий. Файлы под пробой — из
# ЭТОЙ рабочей копии, в синтетический клон во временном каталоге.
#
# НАСТОЯЩИЙ ВХОД — сообщения трёх коммитов, записанных с трейлерами (ветка
# 2840-trailered-b81c695: d838c9ce779, 82f0e455536, b81c69568a9); адрес сессии в
# фикстуре заменён. Близнец — то же сообщение без блока трейлеров, выведенный из
# него же.
#
# ОТПРАВКИ ИДУТ С KACHO_SKIP_PREPUSH=1: обход снимает проверки дерева (сборку и
# пробы, которых в фикстуре нет), а страж атрибуции идёт до него и им не
# снимается — это отдельное утверждение.
#
# КОНТРОЛЬ — слепой предикат (attribution_line всегда «нет»): тот же вход
# записывается и доезжает. Без него нечем показать, что утверждения держатся
# предикатом, а не устройством фикстуры.
#
# ИСХОДЫ: 0 — все утверждения сошлись; 1 — хоть одно разошлось; 2 — не
# выполнилось (нет git, фикстура не собрана, ни одного утверждения).
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
tree="$(cd "$here/../.." && pwd -P)"

void() { echo "attribution-inject: НЕ ВЫПОЛНИЛОСЬ — $*" >&2; exit 2; }
for t in git sed awk grep make mktemp; do command -v "$t" > /dev/null 2>&1 || void "нет $t в PATH"; done

work="$(mktemp -d)" || void "нет временного каталога"
trap 'rm -rf "$work"' EXIT
work="$(cd "$work" && pwd -P)"

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_COMMON_DIR \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_PREFIX GIT_CONFIG_GLOBAL GIT_CONFIG_PARAMETERS \
      GIT_CONFIG_COUNT GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_AUTHOR_DATE \
      GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL GIT_COMMITTER_DATE GIT_EDITOR EDITOR VISUAL \
      KACHO_SKIP_PREPUSH GIT_SSH_COMMAND GIT_SSH
export HOME="$work/home" XDG_CONFIG_HOME="$work/home/.config" GIT_CONFIG_NOSYSTEM=1 \
       GIT_TERMINAL_PROMPT=0
mkdir -p "$HOME" "$XDG_CONFIG_HOME"
printf '[user]\n\tname = probe\n\temail = probe@example.invalid\n[init]\n\tdefaultBranch = main\n[commit]\n\tgpgsign = false\n[advice]\n\tdetachedHead = false\n' > "$HOME/.gitconfig"

pass=0
fail=0
ok()  { pass=$((pass + 1)); echo "  сошлось    $1"; }
bad() { fail=$((fail + 1)); echo "  РАЗОШЛОСЬ  $1"; }
fact() { local name="$1"; shift; if "$@"; then ok "$name"; else bad "$name"; fi; }
# shellcheck disable=SC2329  # зовётся через fact
not() { ! "$@"; }

# ── ПРЕДПОСЫЛКА: файлы правила в дереве и цель провязки ─────────────────────
echo "== провязка в дереве"
for f in commit-msg attribution-rule.sh prepush-attribution.sh pre-push install.sh; do
    fact "scripts/hooks/$f отслеживается git" \
        test -n "$(git -C "$tree" ls-files -- "scripts/hooks/$f" 2> /dev/null)"
done
fact "make install-hooks зовёт bash scripts/hooks/install.sh install" \
    grep -qF "bash scripts/hooks/install.sh install" <<< "$(make -C "$tree" --no-print-directory -n install-hooks 2>&1)"

# ── НАСТОЯЩИЙ ВХОД ───────────────────────────────────────────────────────────
cat > "$work/real.all" <<'REAL'
#2840 deploy: проба порядка cert-manager исполняема в индексе

TestShebangScriptsAreExecutable на голове 82f0e455536: неисполняемых 1
(проба заведена с режимом 100644, в чистом клоне не запустится). После
git add --chmod=+x: неисполняемых 0 из 337 файлов с shebang.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01fixture
%%
#2840 deploy: состояние релиза cert-manager спрашивается без helm list -a

Живой stack-up на kind (helm v4.2.4) показал: у helm v4 флага -a нет,
и ветка «наш релиз» отказывала бы на каждом повторном подъёме. Проба
этого не видела: подставной helm принимал любой флаг.

Подставные kubectl и helm теперь сперва разбирают флаги настоящим
инструментом (<args> --help) и отказывают его текстом. До правки
рецепта: 10 из 10, находок 2 (Б2, Б3 — unknown shorthand flag 'a');
после: 10 из 10, находок 0.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01fixture
%%
#2840 deploy: stack-up ставит cert-manager тем же местом, что dev-up

Порядок «cert-manager отдельным релизом → его вебхук → продукт» жил
строками внутри dev-up; stack-up применял умбреллу с
cert-manager.enabled=false и релиза не ставил, поэтому на чистом
кластере цепочка упиралась в отсутствие CRD Certificate/Issuer.

Порядок вынесен в цель cert-manager-up (страж guard-declared-context),
её зовут оба пути подъёма раньше продукта. Исходы по владельцу CRD:
нет — ставит; наш той же версии — не переставляет; наш другой версии —
доводит; чужой (a8f60d) — не трогает; не прочитано — отказ.

Проба tests/helm/cert-manager-release-before-product-test.sh: до правки
10 из 10 исполнено, 9 находок; после — 10 из 10, находок 0.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01fixture
REAL
awk -v d="$work" '/^%%$/ { n++; next } { print > (d "/real-" (n + 1) ".msg") }' "$work/real.all"
real_n=0
for m in "$work"/real-*.msg; do
    [ -f "$m" ] || continue
    real_n=$((real_n + 1))
    sed '/^Co-Authored-By:/,$d' "$m" > "${m%.msg}.twin"
done
[ "$real_n" = 3 ] || void "настоящий вход не разобран ($real_n из 3)"
real_trailer="Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"

# ── ФИКСТУРА ─────────────────────────────────────────────────────────────────
# kit <каталог> [blind] — набор оснастки под судом из этой рабочей копии; blind
# — с воссозданным дефектом «слепой предикат».
kit() {
    mkdir -p "$1"
    local f
    for f in commit-msg attribution-rule.sh prepush-attribution.sh pre-push install.sh; do
        [ -f "$here/$f" ] && cp "$here/$f" "$1/"
    done
    if [ "${2:-}" = blind ] && [ -f "$1/attribution-rule.sh" ]; then
        printf '\nattribution_line() { return 1; }\n' >> "$1/attribution-rule.sh"
    fi
    return 0
}

# fixture <каталог> <набор> — клон с коммитом оснастки, провязанный штатной
# командой, и голый удалённый; на удалённом — main с опубликованным коммитом,
# несущим трейлер (опубликованная история не судится — предикат п. 6).
fixture() {
    local F="$1" K="$2"
    rm -rf "$F" "$F.git"
    mkdir -p "$F/scripts/hooks"
    cp "$K"/* "$F/scripts/hooks/" 2> /dev/null
    chmod +x "$F/scripts/hooks/"* 2> /dev/null
    printf 'install-hooks:\n\t@bash scripts/hooks/install.sh install\n' > "$F/Makefile"
    git -C "$F" init -q && git -C "$F" add -A && git -C "$F" commit -q --no-verify -m "#1 оснастка" || return 1
    git -C "$F" commit -q --allow-empty --no-verify -m "#1 опубликовано до правила" -m "$real_trailer" || return 1
    git init -q --bare "$F.git" && git -C "$F" remote add origin "$F.git" || return 1
    git -C "$F" push -q --no-verify origin main 2> /dev/null || return 1
    (cd "$F" && make --no-print-directory install-hooks) > "$work/install.out" 2>&1
}

F="$work/f"
kit "$work/kit-real"
fixture "$F" "$work/kit-real" || void "фикстура не собрана"

attempt() { # <команда…> в фикстуре: код в rc, вывод в out, записан ли коммит — made
    local before after
    before="$(git -C "$F" rev-parse -q --verify HEAD)"
    out="$(cd "$F" && "$@" 2>&1 < /dev/null)"
    rc=$?
    after="$(git -C "$F" rev-parse -q --verify HEAD)"
    made=0
    [ "$before" = "$after" ] || made=1
}
has() { [[ "$out" == *"$1"* ]]; }
refused() { # <имя> <образец…>
    local name="$1" p miss=""
    shift
    for p in "$@"; do has "$p" || miss="$miss «$p»"; done
    if [ "$rc" -ne 0 ] && [ "$made" = 0 ] && [ -z "$miss" ]; then ok "$name (отказ, код $rc)"
    else bad "$name: код $rc, записан $made; нет образцов:${miss:- —}"; printf '%s\n' "$out" | sed 's/^/      | /'; fi
}
accepted() { # <имя> — записан, код 0, хук промолчал
    if [ "$rc" -eq 0 ] && [ "$made" = 1 ] && [ -z "$out" ]; then ok "$1 (записан, код 0)"
    else bad "$1: код $rc, записан $made"; printf '%s\n' "$out" | sed 's/^/      | /'; fi
}
on() { git -C "$F" checkout -q -f -B "$1" "${2:-main}" > /dev/null 2>&1 || void "ветка $1 не встала"; }
commit() { attempt git commit -q --allow-empty "$@"; }

echo "== провязка штатной командой (make install-hooks в фикстуре)"
fact "make install-hooks провязал commit-msg переходником" test -x "$F/.git/hooks/commit-msg"
fact "make install-hooks провязал pre-push переходником" test -x "$F/.git/hooks/pre-push"

# ── ХУК КОММИТА ──────────────────────────────────────────────────────────────
echo "== хук коммита: настоящий вход и близнецы"
for i in 1 2 3; do
    on 2840; commit -F "$work/real-$i.msg"
    refused "настоящий вход $i — отказ, названа строка" "commit-msg ОТКАЗ" "«$real_trailer»" "как правильно"
    on 2840; commit -F "$work/real-$i.twin"
    accepted "близнец настоящего входа $i без блока трейлеров"
done

echo "== хук коммита: по мутанту на ключ, близнец — ключ в строке прозы"
while IFS='|' read -r label trailer; do
    [ -n "$label" ] || continue
    on 7; commit -m "#7 x" -m "тело" -m "$trailer"
    refused "мутант: $label" "«$trailer»"
done << 'FORMS'
Co-Authored-By с моделью|Co-Authored-By: Claude Opus <noreply@example.invalid>
Co-Authored-By соавтора-человека — запрещён ключ, а не значение|Co-authored-by: Иван Петров <ivan@example.org>
Co-Authored-By с отступом|   Co-Authored-By: Someone <someone@example.org>
Claude-Session:|Claude-Session: https://example.invalid/session_x
строка Generated with Claude Code|Generated with [Claude Code](https://example.invalid)
строка Generated with Claude Code со значком|🤖 Generated with [Claude Code](https://example.invalid)
ссылка claude.ai/code|https://claude.ai/code/session_x
FORMS
while IFS='|' read -r label trailer; do
    [ -n "$label" ] || continue
    on 7; commit -m "#7 x" -m "Снята строка шаблона $trailer — подставлялась"
    accepted "близнец: «$label» в середине строки прозы — упоминание"
done << 'MENTIONS'
Co-Authored-By|Co-Authored-By: Claude Opus <noreply@example.invalid>
Co-Authored-By соавтора-человека|Co-authored-by: Иван Петров <ivan@example.org>
Claude-Session:|Claude-Session: https://example.invalid/session_x
MENTIONS
on 7; printf 'x\nCo-Authored-By: из файла\ny\n' > "$F/ctx.txt"; git -C "$F" add ctx.txt; git -C "$F" commit -q --no-verify -m "#7 файл"
sed -i 's/^y$/z/' "$F/ctx.txt"; git -C "$F" add ctx.txt
attempt env GIT_EDITOR=true git commit -q -v -e -m "правка файла, редактором и с -v"
accepted "близнец: строка контекста ниже линии ножниц (-v) — дифф, а не сообщение"

echo "== хук коммита: судить нечем — не зелёное"
: > "$work/empty.msg"
attempt bash scripts/hooks/commit-msg "$work/empty.msg"
refused "пустое сообщение — отказ" "сообщение пусто"
attempt bash scripts/hooks/commit-msg
refused "нет файла сообщения — отказ" "файла сообщения нет"
# Файлы оснастки в фикстуре отслеживаются: снимаются ПОСЛЕ смены ветки, иначе
# `checkout -f` вернул бы их на место.
on 7; mv "$F/scripts/hooks/attribution-rule.sh" "$work/rule.away"
commit -m "#7 x"
refused "предиката нет рядом с хуком — отказ, а не молчание" "предиката нет"
mv "$work/rule.away" "$F/scripts/hooks/attribution-rule.sh"
on 7; commit --no-verify -m "#7 x" -m "$real_trailer"
fact "граница: git commit --no-verify минует хук коммита — записан (его ловит отправка)" test "$rc$made" = 01

# ── ОТПРАВКА ─────────────────────────────────────────────────────────────────
echo "== отправка: страж атрибуции сквозь pre-push, настоящий git push"
remote_at() { git -C "$F" ls-remote origin "$1" | cut -f1; }
push() { attempt env KACHO_SKIP_PREPUSH=1 git push -q origin "$@"; }
delivered() { # <имя> <ссылка>
    local want
    want="$(git -C "$F" rev-parse HEAD)"
    if [ "$rc" -eq 0 ] && [ "$(remote_at "$2")" = "$want" ] && has "нарушений нет"; then ok "$1 (доехала, код 0)"
    else bad "$1: код $rc, на удалённом «$(remote_at "$2")», ждали $want"; printf '%s\n' "$out" | sed 's/^/      | /'; fi
}
stopped() { # <имя> <ссылка> <образец…>
    local name="$1" ref="$2" p miss=""
    shift 2
    for p in "$@"; do has "$p" || miss="$miss «$p»"; done
    if [ "$rc" -ne 0 ] && [ -z "$(remote_at "$ref")" ] && [ -z "$miss" ]; then ok "$name (остановлена, код $rc)"
    else bad "$name: код $rc, на удалённом «$(remote_at "$ref")»; нет образцов:${miss:- —}"; printf '%s\n' "$out" | sed 's/^/      | /'; fi
}
nv() { git -C "$F" commit -q --allow-empty --no-verify "$@"; }
sha10() { git -C "$F" rev-parse --short=10 HEAD; }

on 101; nv -F "$work/real-1.msg"; s="$(sha10)"; push 101
stopped "настоящий вход мимо хука коммита — отказ, назван sha" refs/heads/101 \
    "$s атрибуция в сообщении: «$real_trailer»" "pre-push ОТКАЗ"
fact "обход KACHO_SKIP_PREPUSH стража не снимает" not has "пропущен по KACHO_SKIP_PREPUSH"
on 102; nv -F "$work/real-1.twin"; push 102
delivered "близнец: то же сообщение без трейлеров — доехало" refs/heads/102
on 103
c="$(git -C "$F" commit-tree -p HEAD -m "#103 x" -m "Co-authored-by: Иван Петров <ivan@example.org>" "HEAD^{tree}")"
git -C "$F" update-ref refs/heads/103 "$c"; s="${c:0:10}"; push 103
stopped "соавтор-человек через commit-tree — отказ, назван sha" refs/heads/103 \
    "$s атрибуция в сообщении: «Co-authored-by: Иван Петров <ivan@example.org>»"
on 104; nv -m "#104 x" -m "Снята строка шаблона Co-Authored-By: Claude Opus — подставлялась"; push 104
delivered "близнец: ключ в середине строки прозы — доехало" refs/heads/104
on 105; nv -m "#105 x"; push 105
delivered "близнец диапазона: опубликованный коммит с трейлером в истории не судится" refs/heads/105
on 106; nv -m "#106 x"; nv -m "#106 y" -m "Claude-Session: https://example.invalid/s"; s="$(sha10)"; nv -m "#106 z"
push 106
stopped "трейлер в середине диапазона отправки — отказ, назван sha" refs/heads/106 "$s атрибуция в сообщении"
on wip/107; nv -m "#107 x" -m "$real_trailer"; push wip/107
stopped "черновик wip/* с трейлером — отказ: черновик на удалённом публичен" refs/heads/wip/107 "атрибуция в сообщении"
on 108; nv -m "#108 x"; mv "$F/scripts/hooks/prepush-attribution.sh" "$work/guard.away"
push 108
stopped "стража нет рядом с хуком — отказ, а не молчание" refs/heads/108 "стража атрибуции нет"
mv "$work/guard.away" "$F/scripts/hooks/prepush-attribution.sh"

echo "== страж напрямую: пустой вход и одни снятия"
on 109; nv -m "#109 x" -m "$real_trailer"
attempt bash scripts/hooks/prepush-attribution.sh
refused "пустой вход — судятся неопубликованные коммиты HEAD, трейлер найден" "вход отправки пуст" "атрибуция в сообщении"
on 110 main
attempt bash scripts/hooks/prepush-attribution.sh
fact "пустой вход, HEAD опубликован — код 0 и сказано, что судилось" \
    test "$rc" = 0 -a -n "$(printf '%s' "$out" | grep -F 'вход отправки пуст')"
out="$(cd "$F" && printf 'refs/heads/x 0000000000000000000000000000000000000000 refs/heads/x %s\n' "$(git rev-parse main)" |
    bash scripts/hooks/prepush-attribution.sh 2>&1)"; rc=$?
fact "одни снятия — код 0, сказано «судить нечего»" test "$rc" = 0 -a -n "$(printf '%s' "$out" | grep -F 'судить нечего')"
out="$(cd "$F" && printf 'refs/heads/x %s refs/heads/x 0000000000000000000000000000000000000000\n' "deadbeef" |
    bash scripts/hooks/prepush-attribution.sh 2>&1)"; rc=$?
fact "объект не разрешается — код 2, не зелёное" test "$rc" = 2

# ── КОНТРОЛЬ: слепой предикат ────────────────────────────────────────────────
echo "== контроль: слепой предикат — тот же вход проходит"
kit "$work/kit-blind" blind
G="$work/g"
fixture "$G" "$work/kit-blind" || void "контрольная фикстура не собрана"
out="$(cd "$G" && git checkout -q -b 2840 && git commit -q --allow-empty -F "$work/real-1.msg" 2>&1)"; rc=$?
fact "контроль: со слепым предикатом настоящий вход записан — утверждения выше держатся предикатом" test "$rc" = 0
out="$(cd "$G" && KACHO_SKIP_PREPUSH=1 git push -q origin 2840 2>&1)"; rc=$?
fact "контроль: со слепым предикатом настоящий вход доехал" test "$rc" = 0

echo ""
total=$((pass + fail))
echo "== attribution-inject: утверждений $total · сошлось $pass · разошлось $fail"
[ "$total" -gt 0 ] || void "не проверено ни одного утверждения"
[ "$fail" -eq 0 ] || exit 1
exit 0
