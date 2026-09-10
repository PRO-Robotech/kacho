#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# assert-pin-reachable.sh — РЕВИЗИЯ, НА КОТОРУЮ СМОТРИТ ПИН, ЗАКРЕПЛЕНА
#                           ДОЛГОВЕЧНОЙ ССЫЛКОЙ: СТВОЛОМ ЛИБО ТЕГОМ.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ — И ЧЕМ ОН ОТЛИЧАЕТСЯ ОТ СОСЕДНЕГО ГЕЙТА
#
# Рядом стоит `assert-pin-agrees.sh`. Он судит СОГЛАСИЕ копий пина: одну ли
# версию называют деревья. Согласие ничего не говорит о том, ЖИВА ли названная
# ревизия: три дерева могут дружно называть коммит, который держит одна
# временная голова полосы. Этот гейт судит другое свойство — ЗАКРЕПЛЁННОСТЬ.
#
# Псевдоверсия `v0.0.0-<время>-<ревизия>` резолвится ровно пока ревизия
# достижима с какой-нибудь ссылки удалённого. Уборка веток — штатная операция
# с механизмом (`scripts/branch-audit.sh --prune-merged`) и нормой, требующей
# снимать ссылку тем же заходом, каким влита работа. Пока пин смотрит на
# ревизию, которую держит только голова полосы, эта норма УНИЧТОЖАЕТ
# воспроизводимость: `GOPROXY=direct` перестаёт резолвить, и «какой код внутри»
# знает только чужой кэш прокси (#2243).
#
# ДОЛГОВЕЧНЫ ровно две ссылки: ствол и тег. Накопительная линия долговечной НЕ
# является — её снимают по вливании, поэтому она здесь ИСТОЧНИК пинов, но
# никогда ЯКОРЬ.
#
# ─────────────────────────────────────────────────────────────────────────────
# ИСХОДОВ ЧЕТЫРЕ, И ТРЕТИЙ НЕСУЩИЙ
#
#   0 — каждый осмотренный пин закреплён стволом либо тегом;
#   1 — НАХОДКА: пин смотрит на ревизию, которую не держит ни ствол, ни тег;
#   2 — скрипт позван неверно;
#   3 — ВЕРДИКТА НЕТ: судить было нечего либо нечем — пинов ноль, якорей ноль,
#       либо ревизии пина в этом клоне нет.
#
# Третий отделён от нулевого НАМЕРЕННО. Ревизия, отсутствующая в клоне,
# закреплённой не считается: «не знаю» не выдаётся за «да». А находка
# объявляется ПЕРВОЙ — прогон, где есть и находка, и неспрошенное, отдаёт 1,
# иначе неспрошенное маскировало бы находку.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЯКОРЬ ОБЯЗАН БЫТЬ ДОЛГОВЕЧНЫМ, А НЕ ПРОСТО СУЩЕСТВОВАТЬ
#
# Локальный тег — не якорь: он живёт в одном клоне и снимается вместе с ним.
# По ручке `KACHO_PIN_REACH_NETWORK=1` перечень тегов сверяется с УДАЛЁННЫМ, и
# тег, которого там нет, закрепляющим не считается. Перепись называет состояние
# этой полосы НА КАЖДОМ прогоне: «сверено с удалённым: нет» не должно выглядеть
# как «сверено».
#
# ─────────────────────────────────────────────────────────────────────────────
# ПЕРЕЧЕНЬ ИСТОЧНИКОВ ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ
#
# Пины берутся из состава ссылок клона и из истории ствола. Выписанный перечень
# разошёлся бы с деревом на первой же новой линии — молча, потому что скрипт
# продолжал бы быть зелёным.
set -uo pipefail

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX

PLATFORM_MODULE="${KACHO_RELEASE_MODULE:-github.com/PRO-Robotech/kacho}"
SERVICE_GOMOD="${KACHO_SERVICE_GOMOD:-services/iam/go.mod}"
SERVICE_REPO="${KACHO_SERVICE_REPO:-PRO-Robotech/kaname}"
SERVICE_REPO_GOMOD="${KACHO_SERVICE_REPO_GOMOD:-go.mod}"
REMOTE="${KACHO_RELEASE_REMOTE:-origin}"
TRUNK="${KACHO_RELEASE_TRUNK:-main}"
WITH_HISTORY=1

usage() {
    cat >&2 <<USAGE
употребление: $0 [--no-history]

  Требует, чтобы КАЖДЫЙ пин платформы ($PLATFORM_MODULE), объявленный в
  $SERVICE_GOMOD в любом дереве этого клона, смотрел на ревизию, закреплённую
  ДОЛГОВЕЧНОЙ ссылкой: стволом $REMOTE/$TRUNK либо тегом.

  --no-history  не обходить историю ствола (быстрее; охват уже)

переменные:
  KACHO_PIN_REACH_NETWORK=1      сверять теги с удалённым $REMOTE (долговечность якоря)
  KACHO_PIN_CROSSTREE_NETWORK=1  спрашивать дерево $SERVICE_REPO (источник пина)
  KACHO_RELEASE_MODULE       путь модуля платформы
  KACHO_SERVICE_GOMOD        объявление модуля службы в дереве платформы
  KACHO_SERVICE_REPO         репозиторий опубликованной службы
  KACHO_RELEASE_REMOTE       имя удалённого (умолчание: origin)
  KACHO_RELEASE_TRUNK        имя ствола (умолчание: main)

исходы: 0 закреплены · 1 находка · 2 позван неверно · 3 вердикта нет
USAGE
}

while [ $# -gt 0 ]; do
    case "$1" in
        --no-history) WITH_HISTORY=0 ;;
        -h|--help) usage; exit 2 ;;
        *) echo "неизвестный аргумент: $1" >&2; usage; exit 2 ;;
    esac
    shift
done

if ! command -v git >/dev/null 2>&1 || ! git rev-parse --git-dir >/dev/null 2>&1; then
    echo "ВЕРДИКТА НЕТ: git недоступен либо это не репозиторий — судить нечем" >&2
    exit 3
fi

# pinOf — версия платформы из текста объявления модуля.
#
# Разбор судит ДИРЕКТИВУ, а не строку: путь модуля встречается и в комментариях
# объявления, поэтому берётся первое поле строки, а не подстрока. Иначе скрипт
# считал бы пином собственное объяснение — класс, который корпус ловит у
# распознавателей.
pinOf() {
    awk -v mod="$PLATFORM_MODULE" '
        { sub(/\/\/.*/, "") }
        $1 == mod && $2 ~ /^v/ { print $2; found = 1; exit }
        END { if (!found) exit 1 }
    '
}

# revOf — ревизия из псевдоверсии, либо пусто, если версия выпущенная.
#
# Формы псевдоверсии, которые Go производит, различаются НАЧАЛОМ (`v0.0.0-`,
# `vX.Y.Z-pre.0.`, `vX.Y.Z-0.`), но сходятся хвостом: <14 цифр>-<12 hex>.
# Судится хвост — иначе одна из законных форм молча ушла бы из наблюдения.
revOf() {
    [[ "$1" =~ -([0-9]{14})-([0-9a-f]{12})$ ]] && printf '%s' "${BASH_REMATCH[2]}"
}

declare -a SRC_NAME=() SRC_PIN=() UNASKED=()

note() { # note <имя-источника> <версия|пусто> <причина-если-пусто>
    if [ -n "$2" ]; then
        SRC_NAME+=("$1"); SRC_PIN+=("$2")
        printf '  источник  %-44s -> %s\n' "$1" "$2"
    else
        UNASKED+=("$1")
        printf '  НЕ спрошен %-43s -- %s\n' "$1" "$3"
    fi
}

echo "закреплённость пина платформы $PLATFORM_MODULE, объявленного в $SERVICE_GOMOD"

# ── источник 1: рабочее дерево ───────────────────────────────────────────────
if [ -f "$SERVICE_GOMOD" ]; then
    v="$(pinOf < "$SERVICE_GOMOD" || true)"
    note "рабочее дерево" "$v" "объявление есть, а строки модуля платформы в нём нет"
else
    note "рабочее дерево" "" "нет файла $SERVICE_GOMOD — служба в этом дереве не объявлена"
fi

# ── источник 2..N: ссылки клона (ствол и накопительные линии) ────────────────
TRUNK_REF="refs/remotes/$REMOTE/$TRUNK"
TRUNK_SHA="$(git rev-parse -q --verify "$TRUNK_REF" 2>/dev/null || true)"

REFS="$(git for-each-ref --format='%(refname:short)' \
    "$TRUNK_REF" "refs/remotes/$REMOTE/release/*" 2>/dev/null || true)"
if [ -z "$REFS" ]; then
    note "ссылки платформы ($REMOTE)" "" "ни ствола, ни накопительных линий в этом клоне нет"
else
    while IFS= read -r ref; do
        [ -n "$ref" ] || continue
        if body="$(git show "$ref:$SERVICE_GOMOD" 2>/dev/null)" && [ -n "$body" ]; then
            v="$(printf '%s\n' "$body" | pinOf || true)"
            note "$ref" "$v" "объявление есть, а строки модуля платформы в нём нет"
        else
            note "$ref" "" "объявления $SERVICE_GOMOD на этой ссылке нет"
        fi
    done <<< "$REFS"
fi

# ── источник N+1: ИСТОРИЯ ствола ─────────────────────────────────────────────
# Воспроизводимость ИЗ ИСХОДНИКОВ требует, чтобы резолвился и пин прошлого
# коммита ствола, а не только сегодняшний: проверивший старую ревизию соберёт
# её лишь тогда, когда её пин ещё жив.
HIST_N=0
if [ "$WITH_HISTORY" = "1" ] && [ -n "$TRUNK_SHA" ]; then
    while IFS= read -r c; do
        [ -n "$c" ] || continue
        body="$(git show "$c:$SERVICE_GOMOD" 2>/dev/null)" || continue
        [ -n "$body" ] || continue
        v="$(printf '%s\n' "$body" | pinOf || true)"
        [ -n "$v" ] || continue
        HIST_N=$((HIST_N + 1))
        SRC_NAME+=("история $TRUNK @ $(printf '%.12s' "$c")"); SRC_PIN+=("$v")
    done < <(git log --first-parent --format='%H' "$TRUNK_REF" -- "$SERVICE_GOMOD" 2>/dev/null)
    printf '  источник  %-44s -> пинов %d\n' "история ствола $TRUNK_REF" "$HIST_N"
elif [ "$WITH_HISTORY" = "1" ]; then
    note "история ствола" "" "ствола $TRUNK_REF в этом клоне нет"
fi

# ── источник N+2: дерево опубликованной службы (сеть, по ручке) ──────────────
NET_NAME="$SERVICE_REPO:$SERVICE_REPO_GOMOD"
if [ "${KACHO_PIN_CROSSTREE_NETWORK:-0}" != "1" ]; then
    note "$NET_NAME" "" "чужое дерево не спрашивается (KACHO_PIN_CROSSTREE_NETWORK=1 включает)"
elif ! command -v gh >/dev/null 2>&1; then
    note "$NET_NAME" "" "инструмента нет — gh не найден"
else
    if body="$(gh api "repos/$SERVICE_REPO/contents/$SERVICE_REPO_GOMOD" \
                  --jq .content 2>/dev/null | base64 -d 2>/dev/null)" && [ -n "$body" ]; then
        v="$(printf '%s\n' "$body" | pinOf || true)"
        note "$NET_NAME" "$v" "объявление получено, а строки модуля платформы в нём нет"
    else
        note "$NET_NAME" "" "спросить не удалось: репозиторий недостижим либо нет права"
    fi
fi

# ─────────────────────────────────────────────────────────────────────────────
# ЯКОРИ: ствол и теги. Накопительные линии якорями НЕ являются — см. шапку.
# ─────────────────────────────────────────────────────────────────────────────
LOCAL_TAGS="$(git tag -l 2>/dev/null || true)"
LOCAL_TAG_N="$(printf '%s' "$LOCAL_TAGS" | grep -c . || true)"

NET_TAGS_STATE="нет"
DURABLE_TAGS="$LOCAL_TAGS"
if [ "${KACHO_PIN_REACH_NETWORK:-0}" = "1" ]; then
    if REMOTE_TAGS="$(git ls-remote --tags "$REMOTE" 2>/dev/null)"; then
        REMOTE_TAG_NAMES="$(printf '%s\n' "$REMOTE_TAGS" | grep -v '\^{}' \
            | awk '{print $2}' | sed 's|^refs/tags/||' | sed '/^$/d' | sort -u)"
        DURABLE_TAGS="$(printf '%s\n' "$LOCAL_TAGS" | sed '/^$/d' | sort -u \
            | comm -12 - <(printf '%s\n' "$REMOTE_TAG_NAMES"))"
        NET_TAGS_STATE="да"
    else
        NET_TAGS_STATE="спросить не удалось — теги считаются НЕдолговечными"
        DURABLE_TAGS=""
    fi
fi
DURABLE_TAG_N="$(printf '%s' "$DURABLE_TAGS" | grep -c . || true)"

# Сравнение без внешнего процесса: `grep -q` выходит до конца входа, писатель
# слева получает SIGPIPE, и `pipefail` поднимает это до статуса конвейера —
# найденный тег объявлялся бы НЕнайденным, то есть закреплённый пин — висящим.
isDurableTag() { [[ $'\n'"$DURABLE_TAGS"$'\n' == *$'\n'"$1"$'\n'* ]]; }

# anchorOf — чем закреплена ревизия: печатает якорь либо пусто.
anchorOf() {
    local r="$1"
    if [ -n "$TRUNK_SHA" ] && git merge-base --is-ancestor "$r" "$TRUNK_SHA" 2>/dev/null; then
        printf 'ствол %s' "$REMOTE/$TRUNK"; return 0
    fi
    local t
    while IFS= read -r t; do
        [ -n "$t" ] || continue
        isDurableTag "$t" || continue
        printf 'тег %s' "$t"; return 0
    done < <(git tag --contains "$r" 2>/dev/null)
    return 1
}

# ─────────────────────────────────────────────────────────────────────────────
# ЯКОРЕЙ НОЛЬ — судить НЕЧЕМ, и это не находка
#
# Отсутствие ствола и тегов есть НАША неспособность ответить, а не свойство
# пина. Признать здесь находку значило бы обвинить дерево в том, чего мы не
# спросили; признать зелёное — выдать «не знаю» за «да».
# ─────────────────────────────────────────────────────────────────────────────
if [ -z "$TRUNK_SHA" ] && [ "$DURABLE_TAG_N" = "0" ]; then
    printf '\nперепись: источников спрошено %d, НЕ спрошено %d; якорей 0 (ствол НЕТ, тегов долговечных 0 из %d)\n' \
        "${#SRC_PIN[@]}" "${#UNASKED[@]}" "$LOCAL_TAG_N"
    cat >&2 <<VOID

ВЕРДИКТА НЕТ: якорей ноль — ни ствола $TRUNK_REF, ни долговечных тегов.
Закреплять не с чем; «нечем судить» не выдаётся ни за находку, ни за зелёное.
VOID
    exit 3
fi

# ─────────────────────────────────────────────────────────────────────────────
# Вердикт по КАЖДОМУ различному пину
#
# Обход идёт по ЗНАЧЕНИЯМ, приведённым к различным, а не по ключам
# ассоциативного массива: версия — произвольная строка из чужого файла, и
# индексировать ею массив небезопасно (под `set -u` это роняет прогон целиком,
# то есть превращает вердикт в отказ шелла).
# ─────────────────────────────────────────────────────────────────────────────
UNIQUE_PINS="$(printf '%s\n' ${SRC_PIN[@]+"${SRC_PIN[@]}"} | sed '/^$/d' | sort -u)"
PIN_N="$(printf '%s' "$UNIQUE_PINS" | grep -c . || true)"

whoNamed() { # whoNamed <версия> — какие источники её назвали
    local want="$1" i out=""
    for i in ${SRC_PIN[@]+"${!SRC_PIN[@]}"}; do
        [ "${SRC_PIN[$i]}" = "$want" ] || continue
        if [ -z "$out" ]; then out="${SRC_NAME[$i]}"; else out="$out, ${SRC_NAME[$i]}"; fi
    done
    printf '%s' "$out"
}

ANCHORED=0; ORPHAN=0; UNJUDGED=0
declare -a FINDINGS=() VOIDS=()

echo
while IFS= read -r p; do
    [ -n "$p" ] || continue
    who="$(whoNamed "$p")"
    rev="$(revOf "$p" || true)"
    if [ -z "$rev" ]; then
        # Выпущенная версия закрепляется СВОИМ тегом: ссылка и есть якорь.
        if isDurableTag "$p"; then
            ANCHORED=$((ANCHORED + 1))
            printf '  ЗАКРЕПЛЁН    %-41s тег %s\n' "$p" "$p"
        else
            ORPHAN=$((ORPHAN + 1))
            FINDINGS+=("$p — выпущенная версия, а тега $p среди долговечных нет; назвали: $who")
            printf '  НЕ ЗАКРЕПЛЁН %-41s тега нет\n' "$p"
        fi
        continue
    fi
    if ! git cat-file -e "${rev}^{commit}" 2>/dev/null; then
        UNJUDGED=$((UNJUDGED + 1))
        VOIDS+=("$p — ревизии $rev в этом клоне нет: судить нечем; назвали: $who")
        printf '  НЕ СУЖДЕН    %-41s ревизии нет в клоне\n' "$p"
        continue
    fi
    if a="$(anchorOf "$rev")"; then
        ANCHORED=$((ANCHORED + 1))
        printf '  ЗАКРЕПЛЁН    %-41s %s\n' "$p" "$a"
    else
        ORPHAN=$((ORPHAN + 1))
        holders="$(git for-each-ref --contains "$rev" --format='%(refname:short)' 2>/dev/null | head -3 | tr '\n' ' ')"
        FINDINGS+=("$p (ревизия $rev) — держат только: ${holders:-ничего}; назвали: $who")
        printf '  НЕ ЗАКРЕПЛЁН %-41s держат: %s\n' "$p" "${holders:-ничего}"
    fi
done <<< "$UNIQUE_PINS"


printf '\nперепись: источников спрошено %d, НЕ спрошено %d, различных пинов %d;\n' \
    "${#SRC_PIN[@]}" "${#UNASKED[@]}" "$PIN_N"
printf '          из них закреплено %d, НЕ закреплено %d, не суждено %d;\n' \
    "$ANCHORED" "$ORPHAN" "$UNJUDGED"
printf '          якоря: ствол %s, тегов долговечных %d из %d локальных (сверено с удалённым: %s)\n' \
    "${TRUNK_SHA:-НЕТ}" "$DURABLE_TAG_N" "$LOCAL_TAG_N" "$NET_TAGS_STATE"
[ "${#UNASKED[@]}" -gt 0 ] && printf '  не спрошены: %s\n' "${UNASKED[*]}"

# Находка объявляется ПЕРВОЙ: иначе неспрошенное маскировало бы её.
if [ "$ORPHAN" -gt 0 ]; then
    {
        echo
        echo "НАХОДКА: пинов, не закреплённых долговечной ссылкой — $ORPHAN из $PIN_N."
        echo "  Такой пин резолвится, пока жива ВРЕМЕННАЯ ссылка. Уборка веток —"
        echo "  штатная операция, и она уничтожает воспроизводимость из исходников:"
        echo "  GOPROXY=direct перестаёт резолвить, а «какой код внутри» знает только"
        echo "  чужой кэш прокси, который не наш и срок которого не объявлен (#2243)."
        for f in "${FINDINGS[@]}"; do echo "    $f"; done
        echo "  ЧТО ДЕЛАТЬ — одно из двух, и первое предпочтительнее:"
        echo "    1) перевести пин на ВЫПУЩЕННУЮ версию платформы (порядок выпуска, шаги 3-4):"
        echo "         go get -C \"\$(dirname $SERVICE_GOMOD)\" $PLATFORM_MODULE@<версия>"
        echo "    2) закрепить ревизию тегом и ОТПРАВИТЬ тег на $REMOTE:"
        echo "         git tag archive/kaname-pin-<ревизия> <ревизия> && git push $REMOTE <тег>"
    } >&2
    exit 1
fi

if [ "$PIN_N" = "0" ]; then
    cat >&2 <<VOID

ВЕРДИКТА НЕТ: различных пинов 0 — судить было нечего. «Осмотрено 0» не
читается как «находок 0»: проверьте, что $SERVICE_GOMOD существует и объявляет
$PLATFORM_MODULE.
VOID
    exit 3
fi

if [ "$UNJUDGED" -gt 0 ]; then
    {
        echo
        echo "ВЕРДИКТА НЕТ по $UNJUDGED пину(ам) из $PIN_N: ревизии в этом клоне нет."
        echo "  Закреплённой она не считается — «не знаю» не выдаётся за «да». Отсутствие"
        echo "  в ЭТОМ клоне доказательством отсутствия на $REMOTE не является:"
        echo "  дотяните ссылки (git fetch --tags $REMOTE) и спросите снова."
        for v in "${VOIDS[@]}"; do echo "    $v"; done
    } >&2
    exit 3
fi

echo
echo "ЗАКРЕПЛЕНЫ: все $PIN_N различных пинов держатся стволом либо тегом"
if [ "$NET_TAGS_STATE" = "нет" ]; then
    cat >&2 <<PARTIAL
ОГОВОРКА: теги НЕ сверены с удалённым (KACHO_PIN_REACH_NETWORK=1 включает).
Локальный тег живёт в одном клоне: закрепление, опирающееся только на него,
исчезнет вместе с клоном.
PARTIAL
fi
if [ "${#UNASKED[@]}" -gt 0 ]; then
    cat >&2 <<PARTIAL2
ОГОВОРКА: вердикт относится к СПРОШЕННЫМ источникам (${#SRC_PIN[@]}), а не ко всем.
Не спрошенный источник закреплённым не считается — см. перепись выше.
PARTIAL2
fi
exit 0
