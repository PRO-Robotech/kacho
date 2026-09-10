#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Инъекция для `scripts/release/assert-pin-reachable.sh` — предмет один:
# ЗАКРЕПЛЁННОСТЬ ревизии, на которую смотрит пин, ДОЛГОВЕЧНОЙ ссылкой.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ПРОГОНОВ ТРИ, А НЕ ДВА
#
# Инъекция обязана ронять ТОЛЬКО проверяемое. Рядом с этим гейтом уже стоит
# `assert-pin-agrees.sh`, судящий ДРУГОЕ свойство того же пина — согласие копий.
# Поэтому каждая ось спрашивается тремя прогонами:
#
#   контроль          всё цело            -> молчат ОБА гейта
#   инъекция нового   пин не закреплён    -> краснеет ТОЛЬКО этот гейт
#   инъекция старого  копии разошлись     -> краснеет ТОЛЬКО assert-pin-agrees
#
# Без третьего прогона молчание существующего контроля неотличимо от молчания
# МЁРТВОГО: гейт согласия мог бы утратить способность падать, и новая проверка
# этого не показала бы ничем.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАКОННЫЙ БЛИЗНЕЦ НА КАЖДОЙ ОСИ
#
# Ось «ревизия вне ствола» имеет законный близнец, и он не выдуман: в этом
# дереве четыре ссылки `archive/kaname-pin-*` держат ровно такие ревизии
# намеренно. Гейт, краснеющий на них, отключили бы первым — поэтому близнец
# обязан МОЛЧАТЬ, и это утверждается наравне с находкой.
#
# ─────────────────────────────────────────────────────────────────────────────
# ВСЁ ОФФЛАЙН, ВКЛЮЧАЯ СЕТЕВУЮ ПОЛОСУ
#
# Полоса «тег есть ли на удалённом» спрашивается через `git ls-remote`, а он
# работает и по ПУТИ. Поэтому удалённый синтезируется голым репозиторием в
# temp-каталоге: ось проверяется без сети и без чужого состояния.
set -uo pipefail

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUBJECT="$HERE/assert-pin-reachable.sh"
NEIGHBOUR="$HERE/assert-pin-agrees.sh"

if [ ! -x "$SUBJECT" ]; then
    echo "ОТКАЗ: испытуемого нет либо он не исполняем: $SUBJECT" >&2
    echo "  Инъекция без испытуемого вердикта не даёт — это НЕ ВЫПОЛНИЛОСЬ," >&2
    echo "  а не зелёное: утверждений исполнено 0." >&2
    exit 3
fi

tmp="$(mktemp -d "${TMPDIR:-/tmp}/pinreach.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

checked=0
failed=0
pass() { checked=$((checked + 1)); printf '  OK   %s\n' "$1"; }
fail() { checked=$((checked + 1)); failed=$((failed + 1)); printf '  RED  %s\n     %b\n' "$1" "$2"; }

G() { git -c user.email=x@y -c user.name=x -c commit.gpgsign=false "$@"; }

# ─────────────────────────────────────────────────────────────────────────────
# mkfixture <каталог> — синтетический клон платформы.
#
# Раскладка намеренно воспроизводит ту, из которой класс выведен:
#   trunkB   — ревизия НА стволе                       (закреплена стволом)
#   laneS    — ревизия вне ствола, держит её ветка     (НЕ закреплена)
#   tagD     — ревизия вне ствола, держит её ТЕГ       (законный близнец)
# Печатает: "<trunkB> <laneS> <tagD> <trunkTip>".
# ─────────────────────────────────────────────────────────────────────────────
mkfixture() {
    local d="$1"
    mkdir -p "$d" && cd "$d" || return 1
    git init -q -b main . >/dev/null 2>&1 || return 1

    echo base > README.md
    G add -A >/dev/null 2>&1
    G commit -qm "base" >/dev/null 2>&1
    local A; A="$(git rev-parse HEAD)"

    # ревизия вне ствола, которую держит ВРЕМЕННАЯ ГОЛОВА ПОЛОСЫ
    G checkout -q -b lane "$A" >/dev/null 2>&1
    echo lane > lane.txt
    G add -A >/dev/null 2>&1
    G commit -qm "работа полосы" >/dev/null 2>&1
    local S; S="$(git rev-parse HEAD)"

    # ревизия вне ствола, которую держит ТЕГ (ветка снята — как в дереве)
    G checkout -q -b tagged "$A" >/dev/null 2>&1
    echo tagged > tagged.txt
    G add -A >/dev/null 2>&1
    G commit -qm "работа, закреплённая тегом" >/dev/null 2>&1
    local D; D="$(git rev-parse HEAD)"
    G tag "archive/kaname-pin-${D:0:12}" >/dev/null 2>&1

    G checkout -q main >/dev/null 2>&1
    G branch -q -D tagged >/dev/null 2>&1

    echo b > b.txt
    G add -A >/dev/null 2>&1
    G commit -qm "работа ствола" >/dev/null 2>&1
    local B; B="$(git rev-parse HEAD)"

    mkdir -p services/iam
    printf 'module github.com/PRO-Robotech/kaname\n\ngo 1.26.0\n\nrequire (\n\tgithub.com/PRO-Robotech/kacho v0.0.0-20260101000000-%s\n)\n' "${B:0:12}" > services/iam/go.mod
    G add -A >/dev/null 2>&1
    G commit -qm "вершина ствола" >/dev/null 2>&1
    local TIP; TIP="$(git rev-parse HEAD)"

    git update-ref "refs/remotes/origin/main" "$TIP"
    printf '%s %s %s %s\n' "$B" "$S" "$D" "$TIP"
}

# setpin <ревизия|версия> — переписать пин в РАБОЧЕМ дереве и в вершине ствола,
# чтобы деревья были СОГЛАСНЫ: иначе инъекция ломала бы сразу два свойства, и
# красное нельзя было бы приписать проверяемому.
setpin() {
    local v="$1"
    printf 'module github.com/PRO-Robotech/kaname\n\ngo 1.26.0\n\nrequire (\n\tgithub.com/PRO-Robotech/kacho %s\n)\n' "$v" > services/iam/go.mod
    G add -A >/dev/null 2>&1
    G commit -qm "пин -> $v" >/dev/null 2>&1
    git update-ref "refs/remotes/origin/main" "$(git rev-parse HEAD)"
}

pseudo() { printf 'v0.0.0-20260101000000-%s' "${1:0:12}"; }

run_subject()   { ( cd "$1" && "$SUBJECT" "${@:2}" 2>&1 ); }
rc_subject()    { ( cd "$1" && "$SUBJECT" "${@:2}" >/dev/null 2>&1; echo $? ); }
rc_neighbour()  { ( cd "$1" && "$NEIGHBOUR" >/dev/null 2>&1; echo $? ); }

echo "инъекция: закреплённость пина долговечной ссылкой"
echo

# ═════════════════════════════════════════════════════════════════════════════
# Ось 1. КОНТРОЛЬ — пин на ревизию ствола: молчат ОБА гейта
# ═════════════════════════════════════════════════════════════════════════════
F1="$tmp/ax1"
read -r B1 S1 D1 _TIP1 <<< "$(mkfixture "$F1")"
RC="$(rc_subject "$F1")"; RCN="$(rc_neighbour "$F1")"
if [ "$RC" = "0" ]; then pass "1  контроль: пин на ревизию ствола — гейт молчит"
else fail "1  контроль" "ожидался 0, получен $RC:\n$(run_subject "$F1")"; fi
if [ "$RCN" = "0" ]; then pass "1' контроль: гейт СОГЛАСИЯ на том же дереве тоже молчит"
else fail "1' контроль соседа" "ожидался 0, получен $RCN"; fi

OUT="$(run_subject "$F1")"
if printf '%s' "$OUT" | grep -q 'перепись'; then
    pass "1'' перепись объёма напечатана: «осмотрено 0» отличимо от «осмотрено»"
else fail "1'' перепись" "в выводе её нет:\n$OUT"; fi

# ═════════════════════════════════════════════════════════════════════════════
# Ось 2. ИНЪЕКЦИЯ НОВОГО — ревизию держит ВРЕМЕННАЯ ГОЛОВА: краснеет ТОЛЬКО этот
# ═════════════════════════════════════════════════════════════════════════════
F2="$tmp/ax2"
read -r _B2 S2 _D2 _T2 <<< "$(mkfixture "$F2")"
( cd "$F2" && setpin "$(pseudo "$S2")" )
RC="$(rc_subject "$F2")"; RCN="$(rc_neighbour "$F2")"
OUT="$(run_subject "$F2")"
if [ "$RC" = "1" ]; then pass "2  находка: ревизия вне ствола и без тега — гейт краснеет"
else fail "2  находка" "ожидался 1, получен $RC:\n$OUT"; fi
if printf '%s' "$OUT" | grep -q "${S2:0:12}"; then
    pass "2' находка НАЗЫВАЕТ координату — ревизию ${S2:0:12}"
else fail "2' координата в находке" "ревизии ${S2:0:12} в выводе нет:\n$OUT"; fi
if [ "$RCN" = "0" ]; then
    pass "2'' инъекция уронила ТОЛЬКО проверяемое: гейт согласия молчит"
else fail "2'' чистота инъекции" "гейт согласия тоже покраснел ($RCN) — красное пришло от соседа"; fi

# ═════════════════════════════════════════════════════════════════════════════
# Ось 3. ЗАКОННЫЙ БЛИЗНЕЦ — ревизию вне ствола держит ТЕГ: гейт МОЛЧИТ
# ═════════════════════════════════════════════════════════════════════════════
F3="$tmp/ax3"
read -r _B3 _S3 D3 _T3 <<< "$(mkfixture "$F3")"
( cd "$F3" && setpin "$(pseudo "$D3")" )
RC="$(rc_subject "$F3")"
if [ "$RC" = "0" ]; then
    pass "3  законный близнец: ревизия вне ствола, но закреплена тегом — молчит"
else fail "3  законный близнец" "ожидался 0, получен $RC — гейт краснеет на НАМЕРЕННОЙ практике archive/kaname-pin-*:\n$(run_subject "$F3")"; fi

# ═════════════════════════════════════════════════════════════════════════════
# Ось 4. ИНЪЕКЦИЯ СУЩЕСТВУЮЩЕГО — копии разошлись: краснеет ТОЛЬКО сосед
# ═════════════════════════════════════════════════════════════════════════════
F4="$tmp/ax4"
read -r B4 _S4 D4 _T4 <<< "$(mkfixture "$F4")"
# оба пина ЗАКРЕПЛЕНЫ, но деревья называют разное: предмет соседа, не мой
( cd "$F4" && setpin "$(pseudo "$D4")" \
  && printf 'module github.com/PRO-Robotech/kaname\n\ngo 1.26.0\n\nrequire (\n\tgithub.com/PRO-Robotech/kacho %s\n)\n' "$(pseudo "$B4")" > services/iam/go.mod )
RC="$(rc_subject "$F4")"; RCN="$(rc_neighbour "$F4")"
if [ "$RCN" = "1" ]; then pass "4  существующий контроль ЖИВ: расхождение копий краснеет у соседа"
else fail "4  существующий контроль" "гейт согласия дал $RCN — его молчание неотличимо от мёртвого"; fi
if [ "$RC" = "0" ]; then pass "4' мой гейт на чужом предмете молчит: оба пина закреплены"
else fail "4' разделение предметов" "ожидался 0, получен $RC:\n$(run_subject "$F4")"; fi

# ═════════════════════════════════════════════════════════════════════════════
# Ось 5. ВЕРДИКТА НЕТ — пустой обход не выдаётся за зелёное
# ═════════════════════════════════════════════════════════════════════════════
# Дерево, которое службу НЕ объявляло НИКОГДА. Снять файл вершиной ствола
# недостаточно: обход истории законно найдёт пин прошлого коммита, и пинов
# окажется не ноль — это верное поведение, а не пустой обход.
F5="$tmp/ax5"
mkdir -p "$F5"
( cd "$F5" && git init -q -b main . >/dev/null 2>&1 \
  && echo base > README.md && G add -A >/dev/null 2>&1 \
  && G commit -qm "служба не объявлена" >/dev/null 2>&1 \
  && git update-ref refs/remotes/origin/main "$(git rev-parse HEAD)" \
  && G tag anchor-exists >/dev/null 2>&1 )
RC="$(rc_subject "$F5")"
if [ "$RC" = "3" ]; then pass "5  пинов ноль -> ВЕРДИКТА НЕТ (3), а не зелёное"
else fail "5  пустой обход" "ожидался 3, получен $RC — «осмотрено 0» выдано за «находок 0»:\n$(run_subject "$F5")"; fi

F5b="$tmp/ax5b"
read -r _B6 _S6 _D6 _T6 <<< "$(mkfixture "$F5b")"
( cd "$F5b" && git update-ref -d refs/remotes/origin/main \
  && for t in $(git tag -l); do git tag -d "$t" >/dev/null 2>&1; done )
RC="$(rc_subject "$F5b")"
if [ "$RC" = "3" ]; then pass "5' якорей ноль -> ВЕРДИКТА НЕТ (3): закреплять не с чем"
else fail "5' нет якорей" "ожидался 3, получен $RC — без ствола и тегов гейт обязан молчать о вердикте:\n$(run_subject "$F5b")"; fi

# ═════════════════════════════════════════════════════════════════════════════
# Ось 6. ОПУБЛИКОВАННАЯ версия закрепляется СВОИМ тегом, а не ревизией
# ═════════════════════════════════════════════════════════════════════════════
F6="$tmp/ax6"
read -r _B7 _S7 _D7 _T7 <<< "$(mkfixture "$F6")"
( cd "$F6" && setpin "v0.1.0" && G tag v0.1.0 >/dev/null 2>&1 )
RC="$(rc_subject "$F6")"
if [ "$RC" = "0" ]; then pass "6  пин на выпущенную версию + тег есть -> молчит"
else fail "6  выпущенная версия" "ожидался 0, получен $RC:\n$(run_subject "$F6")"; fi

F6b="$tmp/ax6b"
read -r _B8 _S8 _D8 _T8 <<< "$(mkfixture "$F6b")"
( cd "$F6b" && setpin "v0.1.0" )
RC="$(rc_subject "$F6b")"
if [ "$RC" = "1" ]; then pass "6' пин на версию, ТЕГА которой нет -> находка"
else fail "6' версия без тега" "ожидался 1, получен $RC — пин на несуществующую ссылку зелёным быть не может:\n$(run_subject "$F6b")"; fi

# ═════════════════════════════════════════════════════════════════════════════
# Ось 7. ЯКОРЬ ОБЯЗАН БЫТЬ ДОЛГОВЕЧНЫМ — локальный тег не закрепляет
#
# Оба прогона оффлайн: удалённый — голый репозиторий по ПУТИ, `git ls-remote`
# работает по пути так же, как по адресу.
# ═════════════════════════════════════════════════════════════════════════════
F7="$tmp/ax7"
read -r _B9 _S9 D9 _T9 <<< "$(mkfixture "$F7")"
BARE="$tmp/ax7-bare.git"
git init -q --bare "$BARE" >/dev/null 2>&1
( cd "$F7" && setpin "$(pseudo "$D9")" \
  && git remote add origin "$BARE" >/dev/null 2>&1 \
  && G push -q origin main >/dev/null 2>&1 \
  && G push -q origin "refs/tags/archive/kaname-pin-${D9:0:12}" >/dev/null 2>&1 \
  && git fetch -q origin >/dev/null 2>&1 )
RC="$(KACHO_PIN_REACH_NETWORK=1 rc_subject "$F7")"
if [ "$RC" = "0" ]; then pass "7  тег ЕСТЬ на удалённом -> закреплён (сетевая полоса, оффлайн)"
else fail "7  долговечный якорь" "ожидался 0, получен $RC:\n$(cd "$F7" && KACHO_PIN_REACH_NETWORK=1 "$SUBJECT" 2>&1)"; fi

F7b="$tmp/ax7b"
read -r _BA _SA DA _TA <<< "$(mkfixture "$F7b")"
BARE2="$tmp/ax7b-bare.git"
git init -q --bare "$BARE2" >/dev/null 2>&1
( cd "$F7b" && setpin "$(pseudo "$DA")" \
  && git remote add origin "$BARE2" >/dev/null 2>&1 \
  && G push -q origin main >/dev/null 2>&1 \
  && git fetch -q origin >/dev/null 2>&1 )
RC="$(KACHO_PIN_REACH_NETWORK=1 rc_subject "$F7b")"
if [ "$RC" = "1" ]; then pass "7' тега на удалённом НЕТ -> локальный якорь не закрепляет (находка)"
else fail "7' недолговечный якорь" "ожидался 1, получен $RC — локальный тег снимут, и пин осиротеет:\n$(cd "$F7b" && KACHO_PIN_REACH_NETWORK=1 "$SUBJECT" 2>&1)"; fi

# ═════════════════════════════════════════════════════════════════════════════
# Ось 8. ОБЪЕКТА НЕТ В КЛОНЕ — это «не спрошено», а не «закреплено»
# ═════════════════════════════════════════════════════════════════════════════
F8="$tmp/ax8"
read -r _BB _SB _DB _TB <<< "$(mkfixture "$F8")"
( cd "$F8" && setpin "v0.0.0-20260101000000-deadbeefcafe" )
OUT="$(run_subject "$F8")"
RC="$(rc_subject "$F8")"
if [ "$RC" != "0" ]; then
    pass "8  ревизии нет в клоне -> зелёным не считается (получен $RC)"
else fail "8  отсутствующий объект" "получен 0 — «не знаю» выдано за «да»:\n$OUT"; fi

printf '\nперепись: утверждений исполнено %d, провалено %d\n' "$checked" "$failed"
if [ "$checked" = "0" ]; then
    echo "ВЕРДИКТА НЕТ: утверждений исполнено 0 — инъекция беспредметна" >&2
    exit 3
fi
[ "$failed" = "0" ] || exit 1
echo "инъекция сошлась: гейт способен упасть по каждой оси и молчать на законных близнецах"
