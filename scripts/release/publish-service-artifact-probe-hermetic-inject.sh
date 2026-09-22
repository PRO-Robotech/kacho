#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# publish-service-artifact-probe-hermetic-inject.sh — доказательство, что вердикт
# пробы производителя поставки НЕ зависит от настройки git того, кто её зовёт.
#
# ПРЕДМЕТ (#2800). Случай I пробы `publish-service-artifact-inject.sh` создаёт
# условие «личность коммитящего не установлена». Прежняя редакция снимала
# личность только в ЛОКАЛЬНОЙ настройке своего временного репозитория — и на
# машине, где личность задана в `~/.gitconfig`, условие не создавалось: git
# отдавал корневую личность, производитель законно её переносил, а проба
# объявляла это дефектом производителя («код 0, ожидался 3»). Хук отправки
# краснел на содержимом ствола у каждого, у кого корневая личность есть; в
# конвейере её нет, и там проба зеленела. Класс был виден там, где с ним ничего
# нельзя сделать, и не виден там, где его ловят до слияния.
#
# ПОЧЕМУ ОТДЕЛЬНОЕ ДОКАЗАТЕЛЬСТВО, А НЕ СЛУЧАЙ ВНУТРИ ПРОБЫ. Предмет — окружение
# ВЫЗЫВАЮЩЕГО, а изнутри пробы его уже не видно: изоляция стоит у неё первой.
# Поэтому условие «у вызывающего задана личность» создаётся здесь, снаружи, и
# создаётся в конвейере тоже — без этого файла снятие изоляции прошло бы там
# зелёным до первого человека с `~/.gitconfig`.
#
# ФОРМ, КОТОРЫМИ ВЫЗЫВАЮЩИЙ ПЕРЕДАЁТ ЛИЧНОСТЬ, ПЯТЬ, и контроль несёт все разом:
#   корневой файл `~/.gitconfig` · файл XDG `$XDG_CONFIG_HOME/git/config` ·
#   системный файл (`GIT_CONFIG_SYSTEM`) · переменные `GIT_CONFIG_COUNT/KEY/VALUE` ·
#   `GIT_CONFIG_PARAMETERS` — так git передаёт `git -c …` всему, что запускает,
#   в том числе хуку отправки.
# Каждая форма несёт СВОЁ значение, и предпосылка контроля спрашивает у git, что
# он видит все пять: форма, не доставившая личность, сделала бы контроль холостым
# по ней. Зелёный контроль при пяти доставленных формах и есть доказательство
# изоляции КАЖДОЙ; утечку любой одной предпосылка пробы называет поимённо.
#
# ПРОГОНОВ ЧЕТЫРЕ, и каждый меняет против контроля РОВНО ОДИН факт:
#   1. контроль — у вызывающего личность есть во всех пяти формах: проба
#      зелёная, и случай I ИСПОЛНЕН, а не пропущен;
#   2. законный близнец — у вызывающего личности нет ни в одной: тот же исход и
#      та же перепись;
#   3. дефект ИСПЫТУЕМОГО — производитель без отказа по личности: случай I
#      краснеет, и краснеет ТОЛЬКО он — изоляция не ослепила пробу к её предмету;
#   4. дефект ПРОБЫ — изоляция снята: случай I объявлен «НЕ ВЫПОЛНИЛОСЬ» с
#      названными формами утечки, а не находкой о производителе.
# Прогоны независимы и идут параллельно: каждый — полная проба, и
# последовательно их цена легла бы на прогон вчетверо.
#
# Исходы: 0 — доказано; 1 — провалено утверждение; 3 — вердикта нет (условие
# прогона не создано, дефект не внесён, утверждений ноль).
set -uo pipefail
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROBE_NAME="publish-service-artifact-inject.sh"
SUT_NAME="publish-service-artifact.sh"
for f in "$PROBE_NAME" "$SUT_NAME"; do
    [ -r "$HERE/$f" ] || { echo "нет $HERE/$f — доказывать нечего, это НЕ ВЫПОЛНИЛОСЬ" >&2; exit 3; }
done
command -v go >/dev/null 2>&1 || { echo "нет go — проба не исполнима, доказать нечем" >&2; exit 3; }

PASS=0; FAIL=0; NOTRUN=0
ok()     { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad()    { FAIL=$((FAIL+1)); printf '  FAIL %s\n     %s\n' "$1" "$2"; }
notrun() { NOTRUN=$((NOTRUN+$1)); printf '  НЕ ВЫПОЛНИЛОСЬ %s\n     %s\n' "$2" "$3"; }

T="$(mktemp -d)" || exit 3
trap 'rm -rf "$T"' EXIT

# ── Вызывающий: два окружения одного устройства ─────────────────────────────
# Устройство одно и то же — свой дом, свой каталог XDG, свой системный файл, —
# различается только СОДЕРЖИМОЕ: у одного в каждой форме есть личность, у
# другого нет ни в одной.
#
# Дом подменяется, а go от этого не должен измениться ничем: его кэши, модули и
# собственная настройка (`go env -w`) лежат под домом, и без закрепления прогон
# стал бы холодным и читал бы другую настройку go — второй внесённый факт.
GO_PINS=(
    "GOPATH=$(go env GOPATH)"
    "GOMODCACHE=$(go env GOMODCACHE)"
    "GOCACHE=$(go env GOCACHE)"
    "GOENV=$(go env GOENV)"
)
FORMS="via-home via-xdg via-system via-count via-parameters"

mkdir -p "$T/with/home" "$T/with/xdg/git" "$T/without/home" "$T/without/xdg/git"
printf '[user]\n\tname = via-home\n\temail = via-home@invalid\n'     > "$T/with/home/.gitconfig"
printf '[user]\n\tname = via-xdg\n\temail = via-xdg@invalid\n'       > "$T/with/xdg/git/config"
printf '[user]\n\tname = via-system\n\temail = via-system@invalid\n' > "$T/with/system.gitconfig"
: > "$T/without/home/.gitconfig"
: > "$T/without/xdg/git/config"
: > "$T/without/system.gitconfig"

caller_with() {  # caller_with <команда...>
    env -u GIT_CONFIG_GLOBAL -u GIT_CONFIG_NOSYSTEM -u GIT_CONFIG \
        HOME="$T/with/home" XDG_CONFIG_HOME="$T/with/xdg" \
        GIT_CONFIG_SYSTEM="$T/with/system.gitconfig" \
        GIT_CONFIG_COUNT=2 \
        GIT_CONFIG_KEY_0=user.name  GIT_CONFIG_VALUE_0=via-count \
        GIT_CONFIG_KEY_1=user.email GIT_CONFIG_VALUE_1=via-count@invalid \
        GIT_CONFIG_PARAMETERS="'user.name'='via-parameters' 'user.email'='via-parameters@invalid'" \
        "${GO_PINS[@]}" "$@"
}
caller_without() {  # caller_without <команда...>
    env -u GIT_CONFIG_GLOBAL -u GIT_CONFIG_NOSYSTEM -u GIT_CONFIG \
        -u GIT_CONFIG_COUNT -u GIT_CONFIG_PARAMETERS \
        HOME="$T/without/home" XDG_CONFIG_HOME="$T/without/xdg" \
        GIT_CONFIG_SYSTEM="$T/without/system.gitconfig" \
        "${GO_PINS[@]}" "$@"
}

echo "── предпосылка: что вызывающий передаёт на самом деле"
# Спрошено у git, а не выведено из того, что записано в файлы: форма, которую
# git не читает, сделала бы контроль холостым по ней — зелёным без предмета.
git init --quiet "$T/look"
SEEN_WITH="$( cd "$T/look" && caller_with git config --show-origin --get-all user.name 2>/dev/null )"
SEEN_WITHOUT="$( cd "$T/look" && caller_without git config --show-origin --get-all user.name 2>/dev/null )"
MISSING=""
for v in $FORMS; do
    [[ "$SEEN_WITH" == *"$v"* ]] || MISSING="$MISSING $v"
done
WITH_OK=1; WITHOUT_OK=1
if [ -n "$MISSING" ]; then
    WITH_OK=0
    printf '   контроль НЕ создан: git не видит форм:%s\n' "$MISSING"
else
    printf '   контроль: личность видна в %d формах —%s\n' "$(printf '%s\n' "$SEEN_WITH" | grep -c .)" " $FORMS"
fi
if [ -n "$SEEN_WITHOUT" ]; then
    WITHOUT_OK=0
    printf '   близнец НЕ создан: git видит личность: %s\n' "$(printf '%s' "$SEEN_WITHOUT" | tr '\n\t' '; ')"
else
    echo "   близнец: личность не видна ни в одной форме"
fi

# ── Внесение дефектов — в КОПИИ, и каждое проверяет, что внеслось ───────────
# Строка ищется ЦЕЛИКОМ, а не образцом: образец задел бы соседнюю строку той же
# формы. Не нашлась ровно та строка — дефект не внесён, и прогон по нему вердикта
# не даёт: тогда это «НЕ ВЫПОЛНИЛОСЬ», а не зелёное по несуществующему дефекту.
# Строка и замена передаются окружением, а не `awk -v`: тот разворачивает
# обратную косую черту в значении, и сличение шло бы не с той строкой.
replace_line() {  # replace_line <файл> <строка> <замена|-> <сколько раз ожидается>
    local file="$1" want="$4" n
    RL_LINE="$2" RL_WITH="$3" RL_COUNT="$file.count" awk '
        $0 == ENVIRON["RL_LINE"] { n++; if (ENVIRON["RL_WITH"] != "-") print ENVIRON["RL_WITH"]; next }
        { print }
        END { print n + 0 > ENVIRON["RL_COUNT"] }' "$file" > "$file.new" \
        || { rm -f "$file.new" "$file.count"; return 1; }
    n="$(cat "$file.count" 2>/dev/null)"; rm -f "$file.count"
    [ "$n" = "$want" ] || { rm -f "$file.new"; return 1; }
    mv "$file.new" "$file" && chmod +x "$file"
}

mkdir -p "$T/sut-defect" "$T/probe-defect"
cp "$HERE/$PROBE_NAME" "$HERE/$SUT_NAME" "$T/sut-defect/"
cp "$HERE/$PROBE_NAME" "$HERE/$SUT_NAME" "$T/probe-defect/"

# Дефект испытуемого: отказ по неустановленной личности снят, всё прочее цело.
SUT_DEFECT=1
replace_line "$T/sut-defect/$SUT_NAME" \
    'if [ -z "$WHO_NAME" ] || [ -z "$WHO_MAIL" ]; then' 'if false; then' 1 || SUT_DEFECT=0

# Дефект пробы: изоляция настройки вызывающего снята целиком — обе её строки.
PROBE_DEFECT=1
replace_line "$T/probe-defect/$PROBE_NAME" \
    'export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1' - 1 \
    && replace_line "$T/probe-defect/$PROBE_NAME" \
    'unset GIT_CONFIG GIT_CONFIG_PARAMETERS GIT_CONFIG_COUNT' - 1 || PROBE_DEFECT=0

# ── Четыре прогона, параллельно ─────────────────────────────────────────────
run_probe() {  # run_probe <имя> <вызывающий> <каталог>
    local name="$1" caller="$2" dir="$3" rc
    ( cd "$T" && "$caller" bash "$dir/$PROBE_NAME" ) > "$T/$name.out" 2>&1; rc=$?
    printf '%s' "$rc" > "$T/$name.rc"
}
[ "$WITH_OK" = 1 ]                             && run_probe control caller_with    "$HERE" &
[ "$WITHOUT_OK" = 1 ]                          && run_probe twin    caller_without "$HERE" &
[ "$WITH_OK" = 1 ] && [ "$SUT_DEFECT" = 1 ]    && run_probe sut     caller_with    "$T/sut-defect" &
[ "$WITH_OK" = 1 ] && [ "$PROBE_DEFECT" = 1 ]  && run_probe probe   caller_with    "$T/probe-defect" &
wait

out()     { cat "$T/$1.out" 2>/dev/null; }
rc_of()   { cat "$T/$1.rc" 2>/dev/null || echo "нет"; }
census()  { out "$1" | grep '^перепись доказательства:' | tail -1; }
# Имена проваленных утверждений. Форма строки провала у пробы одна —
# `  FAIL <имя> <текст>`, её печатает единственная функция `bad`.
failed()  { out "$1" | sed -n 's/^  FAIL \([^ ]*\) .*/\1/p' | tr '\n' ' ' | sed 's/ $//'; }

echo "── 1. контроль: у вызывающего личность есть во всех формах"
if [ "$WITH_OK" = 1 ]; then
    OUT="$(out control)"; RC="$(rc_of control)"
    if [ "$RC" = 0 ]; then ok "1a проба зелёная"
    else bad "1a проба зелёная" "код $RC; провалены: $(failed control); $(census control)"; fi
    if [[ "$OUT" == *"  ok   I1 "* ]] && [[ "$OUT" == *"  ok   I2 "* ]]; then
        ok "1b случай I исполнен и прошёл, а не пропущен"
    else
        bad "1b случай I исполнен и прошёл, а не пропущен" \
            "$(printf '%s\n' "$OUT" | sed -n '/── I\./,/── J\./p' | sed '$d' | tr '\n' '|')"
    fi
    # Спрашивается СТРОКА третьей категории, а не слово: слово стоит и в именах
    # законных утверждений пробы («C5 … — НЕ ВЫПОЛНИЛОСЬ, а не находка»), и поиск
    # по подстроке краснел бы на них — проверено на выводе пробы до правки.
    NOTRUN_LINES="$(printf '%s\n' "$OUT" | grep -A1 '^  НЕ ВЫПОЛНИЛОСЬ ')"
    if [ -z "$NOTRUN_LINES" ]; then ok "1c строк третьей категории в выводе нет"
    else bad "1c строк третьей категории в выводе нет" "$(printf '%s' "$NOTRUN_LINES" | tr '\n' '|')"; fi
else
    notrun 3 "1a–1c" "контроль не создан: git не видит форм:$MISSING"
fi

echo "── 2. законный близнец: личности у вызывающего нет ни в одной форме"
if [ "$WITHOUT_OK" = 1 ]; then
    RC="$(rc_of twin)"
    if [ "$RC" = 0 ]; then ok "2a проба зелёная"
    else bad "2a проба зелёная" "код $RC; провалены: $(failed twin); $(census twin)"; fi
    if [ "$WITH_OK" = 1 ] && [ -n "$(census twin)" ] && [ "$(census twin)" = "$(census control)" ]; then
        ok "2b перепись та же, что у контроля: $(census twin)"
    else
        bad "2b перепись та же, что у контроля" "близнец: '$(census twin)' · контроль: '$(census control)'"
    fi
else
    notrun 2 "2a–2b" "близнец не создан: git видит личность"
fi

echo "── 3. дефект испытуемого: отказа по личности нет"
if [ "$WITH_OK" = 1 ] && [ "$SUT_DEFECT" = 1 ]; then
    RC="$(rc_of sut)"
    if [ "$RC" = 1 ]; then ok "3a проба красная"
    else bad "3a проба красная" "код $RC, ожидался 1; $(census sut)"; fi
    if [ "$(failed sut)" = "I1 I2" ]; then ok "3b краснеет ровно случай I: I1 I2"
    else bad "3b краснеет ровно случай I: I1 I2" "провалены: '$(failed sut)'"; fi
else
    notrun 2 "3a–3b" "дефект не внесён: строки отказа по личности нет в испытуемом ровно одной (либо контроль не создан)"
fi

echo "── 4. дефект пробы: изоляция настройки вызывающего снята"
if [ "$WITH_OK" = 1 ] && [ "$PROBE_DEFECT" = 1 ]; then
    OUT="$(out probe)"; RC="$(rc_of probe)"
    if [ "$RC" = 3 ]; then ok "4a вердикта нет — код 3, а не находка"
    else bad "4a вердикта нет — код 3, а не находка" "код $RC; провалены: '$(failed probe)'; $(census probe)"; fi
    if [ -z "$(failed probe)" ]; then ok "4b о производителе не объявлено ни одной находки"
    else bad "4b о производителе не объявлено ни одной находки" "провалены: '$(failed probe)'"; fi
    SAID="$(printf '%s\n' "$OUT" | grep -A1 '^  НЕ ВЫПОЛНИЛОСЬ I' )"
    for v in $FORMS; do
        if [[ "$SAID" == *"$v"* ]]; then ok "4c утечка названа по форме: $v"
        else bad "4c утечка названа по форме: $v" "текст: $(printf '%s' "$SAID" | tr '\n' '|')"; fi
    done
else
    notrun 7 "4a–4c" "дефект не внесён: строк изоляции нет в пробе ровно по одной (либо контроль не создан)"
fi

echo
printf 'перепись доказательства: утверждений %d, прошло %d, провалено %d, не выполнено %d\n' \
    "$((PASS+FAIL))" "$PASS" "$FAIL" "$NOTRUN"
[ "$FAIL" = "0" ] || exit 1
[ "$NOTRUN" = "0" ] || { echo "ВЕРДИКТА НЕТ по $NOTRUN утверждениям — это не «доказано»" >&2; exit 3; }
[ "$PASS" -gt 0 ] || { echo "утверждений ноль — доказательство беспредметно" >&2; exit 3; }
exit 0
