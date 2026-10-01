#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# machine-credential-posture-inject.sh — доказательство того, что
# `machine-credential-posture-test.sh` СПОСОБНА упасть, и что она молчит на
# законном близнеце. Без этого «зелено» неотличимо от «ничего не проверяет».
#
# ПОЧЕМУ БЛИЗНЕЦ ЗДЕСЬ НЕ УКРАШЕНИЕ. Предмет секции 6 — ПРИСУТСТВИЕ снятого
# ключа `kaname.platform.iam.saKey.bindDpop`, а не его значение и не контур:
# выдачу он не менял ни на одном контуре (kacho#2949). Поэтому инъекции идут на
# обоих контурах и со значением `false`, а законный близнец — тот же
# синтетический профиль без ключа: от инъекции он отличается ровно одним фактом.
# Ось 7 снимает сам отказ с копии чарта: без неё секция 6 судила бы профили
# дерева и не доказывала бы, что отказывает ЧАРТ.
#
# Инъекции идут по временным копиям профилей и чарта; дерево не трогается.

set -uo pipefail
# Каталог доказательства — АБСОЛЮТНЫЙ и снятый ДО смены рабочего: `$0`
# относителен вызывающему, поэтому после `cd` он указывает уже не туда, и
# подключение библиотеки по нему МОЛЧА не происходит (`set -e` тут нет).
INJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$INJECT_DIR/../.."

# ── ПРЕДПОСЫЛКА: ЗАВИСИМОСТИ УМБРЕЛЛЫ МАТЕРИАЛИЗОВАНЫ (задача #1769) ─────────
# Без них рендер отказывает ДО первого шаблона, то есть КАЖДАЯ ось краснеет по
# причине, к проверяемому отношения не имеющей, а выглядит исполненной («ждали
# RED — получили RED»). «Условие не создано» — НЕ вердикт (e2e-flow.md §6):
# свой код возврата, свой текст, не зачитывается ни в успех, ни в отказ.
# Предикат ОДИН на всё семейство: копия у каждого разошлась бы молча.
# shellcheck source=tests/helm/premise.sh
. "$INJECT_DIR/premise.sh" || { echo "ОТКАЗ: библиотека предпосылки не подключилась — молчаливый пропуск предпосылки хуже её отсутствия"; exit 1; }
premise_chart_deps

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

rc=0
assert() { # имя · ожидание(RED|GREEN) · каталог профилей
  local name="$1" want="$2" dir="$3" got
  if MACHINE_POSTURE_PROFILE_DIR="$dir" bash tests/helm/machine-credential-posture-test.sh >"$TMP/out" 2>&1
  then got=GREEN; else got=RED; fi
  if [ "$got" = "$want" ]; then
    echo "  ok   $name → $got"
  else
    echo "  ОТКАЗ $name → $got, ожидалось $want"; sed 's/^/       /' "$TMP/out"; rc=1
  fi
}

# say <файл> <фраза> — вывод последнего прогона обязан НАЗЫВАТЬ виновника.
# Красное без имени профиля читателю бесполезно: у него десять файлов.
says() {
  if grep -qF -- "$1" "$TMP/out"; then
    echo "  ok   вывод называет $1"
  else
    echo "  ОТКАЗ вывод не называет $1"; sed 's/^/       /' "$TMP/out"; rc=1
  fi
}

mkprofiles() { # каталог ← копия профилей дерева
  mkdir -p "$1"
  cp helm/umbrella/values*.yaml "$1"/
}

# setval <файл-во-временном-каталоге> <выражение> — правка ВРЕМЕННОЙ копии.
#
# Пишет через перенаправление, а не правкой на месте. Правка на месте берёт
# целью ПЕРВЫЙ аргумент, а он у этого инструмента — выражение; всякий, кто такую
# строку читает — человек и гейт `TestShellProbesDoNotWriteIntoTheTreeTheyRunFrom`
# одинаково, — видит запись по пути, производному от корня ЖИВОГО дерева. Форма
# с перенаправлением называет цель прямо, и цель эта — временный каталог.
setval() {
  local f="$1" expr="$2"
  yq "$expr" "$f" > "$TMP/edited.yaml"
  mv "$TMP/edited.yaml" "$f"
}

# contour <каталог> <контур переведён: true|false> [значение снятого ключа] —
# СИНТЕТИЧЕСКИЙ профиль, единственный в каталоге. Без третьего аргумента ключа
# в профиле НЕТ — это законный близнец.
#
# ПОЧЕМУ СИНТЕТИКА, А НЕ ПРОФИЛЬ ДЕРЕВА. Предмет — ключ профиля, а не состояние
# какого-то стенда: профиль дерева меняется своими задачами, и доказательство
# потеряло бы вход вместе с предметом, которого не касалось. Синтетический
# профиль от этого не зависит. Что проба читает профили дерева и называет
# виновника по имени, держат (1), (4) и (6).
contour() {
  mkdir -p "$1"
  {
    printf 'kaname:\n  config:\n    authn:\n      clientToken:\n        enabled: %s\n' "$2"
    [ -n "${3:-}" ] && printf '  platform:\n    iam:\n      saKey:\n        bindDpop: %s\n' "$3"
  } > "$1/values.synthetic-contour.yaml"
}

# (1) законный близнец: профили дерева как есть — проба обязана МОЛЧАТЬ.
mkprofiles "$TMP/asis"
assert "профили дерева как есть" GREEN "$TMP/asis"

# (2) снятый ключ возвращён профилем на ПЕРЕВЕДЁННОМ контуре.
contour "$TMP/retired-translated" true true
assert "снятый ключ на переведённом контуре" RED "$TMP/retired-translated"
says "values.synthetic-contour.yaml"

# (3) ЗАКОННЫЙ БЛИЗНЕЦ: тот же профиль без ключа — проба обязана молчать.
#     От (2) отличается одним фактом — присутствием ключа.
contour "$TMP/twin" true
assert "тот же профиль без снятого ключа" GREEN "$TMP/twin"

# (3а) тот же ключ на НЕпереведённом контуре — тоже находка: выдачу ключ не
#      менял ни на одном контуре, и судится присутствие, а не контур.
contour "$TMP/retired-untranslated" false true
assert "снятый ключ на непереведённом контуре" RED "$TMP/retired-untranslated"
says "values.synthetic-contour.yaml"

# (3б) значение `false` — тоже объявление: судится присутствие, а не значение.
contour "$TMP/retired-false" true false
assert "снятый ключ со значением false" RED "$TMP/retired-false"

# (4) секция 4 способна упасть по ЛЮБОМУ профилю, а не по паре prod/dev:
#     требование связанности взведено в профиле вне этой пары.
mkprofiles "$TMP/order"
setval "$TMP/order/values.fe3455-prod.yaml" '.["api-gateway"].authn.requireMachineTokenBinding = true'
assert "требование связанности в профиле вне пары prod/dev" RED "$TMP/order"
says "values.fe3455-prod.yaml"

# (5) перепись по пустому набору — НЕ зелёный вердикт. «Ноль находок» обязано
#     быть отличимо от «ноль прочитанного».
mkdir -p "$TMP/empty"
assert "профилей ноль" RED "$TMP/empty"

# (6) перепись печатается ВСЕГДА, и её числа читаемы. Без этого предыдущее
#     требование выполнялось бы формально: отказ на пустом наборе есть, а
#     объёма осмотренного на непустом не видно.
MACHINE_POSTURE_PROFILE_DIR="$TMP/asis" bash tests/helm/machine-credential-posture-test.sh >"$TMP/out" 2>&1
says "census: profiles read"

# (7) отказ снят с КОПИИ чарта: строка снятого ключа вычеркнута из перечня
#     `kaname.refuseRetiredKnobs`. Профили — копии дерева, ключа в них нет;
#     краснеть обязан рендер секции 6, и вывод обязан это назвать. Близнец —
#     (1): тот же чарт с отказом молчит.
cp -R helm/umbrella "$TMP/umbrella-no-refusal"
helpers="$TMP/umbrella-no-refusal/charts/kaname/templates/_helpers.tpl"
grep -v '(list "platform.iam.saKey.bindDpop"' "$helpers" > "$TMP/helpers.tpl"
mv "$TMP/helpers.tpl" "$helpers"
if grep -qF 'platform.iam.saKey.bindDpop" ' "$helpers"; then
  echo "  ОТКАЗ инъекция (7) не сняла строку отказа — ось не исполнена"; rc=1
else
  if MACHINE_POSTURE_UMBRELLA="$TMP/umbrella-no-refusal" bash tests/helm/machine-credential-posture-test.sh >"$TMP/out" 2>&1
  then got=GREEN; else got=RED; fi
  if [ "$got" = RED ]; then
    echo "  ok   чарт без отказа по снятому ключу → RED"
  else
    echo "  ОТКАЗ чарт без отказа по снятому ключу → GREEN, ожидалось RED"; sed 's/^/       /' "$TMP/out"; rc=1
  fi
  says "accepts a retired knob"
fi

exit "$rc"
