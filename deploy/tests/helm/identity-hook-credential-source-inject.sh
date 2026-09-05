#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# identity-hook-credential-source-inject.sh — доказательство того, что проба
# `identity-hook-credential-source-test.sh` СПОСОБНА упасть, и что она молчит на
# законном близнеце. Без этого «зелено» неотличимо от «ничего не проверяет».
#
# Инъекции идут по временным копиям профиля; дерево не трогается.

set -euo pipefail
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

CHART=./helm/umbrella
# Синтетические профили живут во ВРЕМЕННОМ каталоге, а не в дереве чарта:
# проба не имеет права писать туда, откуда запущена — иначе она портит
# состояние, которое читают соседние проверки, и её собственные находки
# становятся неотличимы от чужих. Гейт этот класс держит и поймал первую
# редакцию здесь же.
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

rc=0
# RED здесь означает РОВНО код 1 — «находка о дереве», — а не «ненулевой код».
# Различие не педантское: у пробы теперь три исхода (задача #1214), и код 2
# («условие не создано»: зависимости умбреллы не собраны, нет helm) приходит на
# ЛЮБОМ профиле, включая законных близнецов. Пока «RED» значило «не ноль»,
# доказательство способности упасть засчитывало бы отказ, к внесённому дефекту
# отношения не имеющий, — то есть было бы доказательством ни о чём.
assert() { # имя · ожидание(RED|GREEN) · файл профиля
  local name="$1" want="$2" prof="$3" got code
  IDENTITY_SOURCE_PROFILES="$prof" bash tests/helm/identity-hook-credential-source-test.sh \
    >"$TMP/out" 2>&1 && code=0 || code=$?
  case "$code" in
    0) got=GREEN ;;
    1) got=RED ;;
    *) got="УСЛОВИЕ-НЕ-СОЗДАНО(код=$code)" ;;
  esac
  if [ "$got" = "$want" ]; then
    echo "  ok   $name → $got"
  else
    echo "  ОТКАЗ $name → $got, ожидалось $want"; sed 's/^/       /' "$TMP/out"; rc=1
  fi
}

# (1) законный близнец: дерево как есть — проба обязана МОЛЧАТЬ.
assert "дерево как есть" GREEN values.dev.yaml

# (2) дефект возвращён: подстановки нет вовсе — ссылка остаётся без источника.
python3 - "$CHART/values.dev.yaml" "$TMP/values.inject-noinit.yaml" <<'PY'
import io,sys,re
s=io.open(sys.argv[1],encoding="utf-8").read()
i=s.index("    extraInitContainers: |")
j=s.index("    extraVolumes:", i)
io.open(sys.argv[2],"w",encoding="utf-8").write(s[:i]+s[j:])
PY
assert "подстановки нет" RED "$TMP/values.inject-noinit.yaml"

# (3) процесс читает ШАБЛОН, а не отрендеренное — путь --config возвращён назад.
sed 's|/etc/kacho-identity-rendered/kratos.yaml|/etc/kacho-identity/kratos.yaml|' \
  "$CHART/values.dev.yaml" > "$TMP/values.inject-oldpath.yaml"
assert "читает шаблон" RED "$TMP/values.inject-oldpath.yaml"

# (4) отказ перестал быть закрытым: обязательность перечня имён, которыми шаг
#     владеет, снята — тогда пустая и недоехавшая величина проходят молча.
python3 - "$CHART/values.dev.yaml" "$TMP/values.inject-open.yaml" <<'PY'
import io,sys
s=io.open(sys.argv[1],encoding="utf-8").read()
s=s.replace('{{- include "kacho.identity.configRenderInitContainer" . | nindent 0 }}',
            '{{- include "kacho.identity.configRenderInitContainer" . | nindent 0 | replace ":?перечень" ":-перечень" }}')
io.open(sys.argv[2],"w",encoding="utf-8").write(s)
PY
assert "отказ не закрытый" RED "$TMP/values.inject-open.yaml"

# (4a) шаг снова судит остаток ПО ИМЕНИ, а не по форме — ровно дефект #1677.
#      Класс символов в поиске заменён конкретным именем: перечень имён растёт
#      вместе с конфигурацией и не растёт вместе с деревом.
python3 - "$CHART/values.dev.yaml" "$TMP/values.inject-byname.yaml" <<'PY'
import io,sys
s=io.open(sys.argv[1],encoding="utf-8").read()
s=s.replace('{{- include "kacho.identity.configRenderInitContainer" . | nindent 0 }}',
            '{{- include "kacho.identity.configRenderInitContainer" . | nindent 0 | replace "[A-Za-z_][A-Za-z0-9_]*" "KANAME_HOOK_TOKEN" }}')
io.open(sys.argv[2],"w",encoding="utf-8").write(s)
PY
assert "остаток судится по имени" RED "$TMP/values.inject-byname.yaml"

# (5) законный близнец второго рода: профиль БЕЗ службы личности — не находка,
#     а отсутствие предмета. Проба обязана пройти, сказав об этом.
python3 - "$CHART/values.dev.yaml" "$TMP/values.inject-nokratos.yaml" <<'PY'
import io,sys,re
s=io.open(sys.argv[1],encoding="utf-8").read()
s=re.sub(r"(?m)^(kratos:\n(?:  #[^\n]*\n)*  )enabled: true", r"\1enabled: false", s, count=1)
io.open(sys.argv[2],"w",encoding="utf-8").write(s)
PY
assert "службы личности нет" GREEN "$TMP/values.inject-nokratos.yaml"

[ "$rc" -eq 0 ] && echo "OK: проба падает на возвращённом дефекте и молчит на законном"
exit "$rc"
