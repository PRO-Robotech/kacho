#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# posture-listener-form-test.sh — вердикт гейта посадки судит ФОРМУ СЛУШАТЕЛЯ,
# которую процесс о себе объявил (`listener_form`), и не путает «слушателя нет»
# с «слушатель без защиты».
#
# ПРЕДМЕТ. Гейт посадки (scripts/assert-production-posture.sh) требовал mTLS
# публичного слушателя и проверку прав на вызове у КАЖДОЙ строки перечня. У
# процессов notify это неверно в обе стороны: отправитель не служит ни одного
# gRPC-сервиса (`listener_form=none`), API notify и проба поднимают только
# внутренний слушатель (`internal_only`). Строка «public_mtls=false» у них
# говорит «публичного слушателя нет», и гейт, не читающий формы, либо красил бы
# их за отсутствие механизма, либо — если бы их исключили из перечня — не видел
# бы вовсе. Здесь доказывается, что вердикт различает формы и при этом НЕ
# ослабляет ни одного измерения у пары.
#
# ЧТО УТВЕРЖДАЕТСЯ (программа вердикта вынимается из гейта, не переписывается):
#   1. пара без mTLS публичного слушателя — отказ (близнец: пара с mTLS — проход);
#   2. ключа формы нет — форма пара: отказ без mTLS (отсутствие не ослабляет);
#   3. internal_only без mTLS публичного — проход; без проверки прав — отказ;
#   4. none без публичного mTLS и без проверки прав — проход;
#   5. форма вне перечня (`<invalid>` фундамента) — отказ с именем измерения;
#   6. перечень гейта несёт три процесса notify и пробу с их базами, а круг
#      отправителей судится у двух принимающих пересылку.
#
# ЧЕГО НЕ ДЕЛАЕТ: не поднимает стенд — живые процессы судит сам гейт на подъёме;
# не сверяет перечень с рендером — это deploy/stand_posture_gate_census_test.go.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/../.." && pwd)"
GATE="$DEPLOY_ROOT/scripts/assert-production-posture.sh"

. "$HERE/outcome.sh"
EXPECTED_ASSERTIONS=11

command -v jq >/dev/null 2>&1 \
  || fatal "нужен jq — вердикт гейта посадки прогоняется его же программой, переписать её здесь нельзя"
[ -f "$GATE" ] || fatal "гейта посадки нет по пути $GATE — судить не о чем"

# Та же выемка, что у соседних проверок круга отправителей: копия программы
# разошлась бы с оригиналом, и проверка стала бы формой без содержания.
JQPROG="$(awk '/verdict="\$\(printf/{f=1} f{print} /join\(", "\)/{if(f) exit}' "$GATE" \
  | sed "1s/.*jq -r --argjson need_fwd \"\\\$need_fwd\" '//")"
JQPROG="${JQPROG%\')\"}"
[ -n "$JQPROG" ] || fatal "не удалось вынуть программу вердикта из $GATE (гейт изменил форму — обнови проверку)"

# line <фрагмент> — самоотчёт, в котором заявлены все прочие измерения в
# проходящем значении: краснеть строка обязана только по предмету проверки.
line() {
  printf '{"msg":"boot security posture","service":"probe","auth_mode":"production",'
  printf '"db_sslmode":"require","internal_mtls":"true","trusted_forwarders":true,'
  printf '"identity_provider":"n/a","own_rest_public_tls":"n/a","own_rest_internal_tls":"n/a",%s}' "$1"
}
verdict() { line "$1" | jq -r --argjson need_fwd true "$JQPROG"; }

expect_pass() { # expect_pass <фрагмент> <что это>
  local v; v="$(verdict "$1")" || fatal "программа вердикта не исполнилась на «$2»"
  [ -z "$v" ] || fail "$2 — забраковано вердиктом: '$v'"
  ok
}
expect_refusal() { # expect_refusal <фрагмент> <измерение> <что это>
  local v; v="$(verdict "$1")" || fatal "программа вердикта не исполнилась на «$3»"
  case "$v" in *"$2="*) ;; *) fail "$3 — вердикт не назвал $2: '$v'";; esac
  ok
}

# 1. пара: mTLS публичного обязателен.
expect_pass    '"listener_form":"pair","public_mtls":true,"authz_check":true' "пара под mTLS"
expect_refusal '"listener_form":"pair","public_mtls":false,"authz_check":true' public_mtls "пара без mTLS публичного"
# 2. ключа формы нет — пара.
expect_refusal '"public_mtls":false,"authz_check":true' public_mtls "самоотчёт без формы и без mTLS публичного"
# 3. только внутренний слушатель.
expect_pass    '"listener_form":"internal_only","public_mtls":false,"authz_check":true' "только внутренний слушатель"
expect_refusal '"listener_form":"internal_only","public_mtls":false,"authz_check":false' authz_check "внутренний слушатель без проверки прав"
expect_refusal '"listener_form":"internal_only","public_mtls":false,"authz_check":true,"internal_mtls":"false"' internal_mtls "внутренний слушатель без mTLS"
# 4. слушателей нет.
expect_pass    '"listener_form":"none","public_mtls":false,"authz_check":false,"internal_mtls":"n/a"' "процесс без слушателей"
# 5. форма вне перечня.
expect_refusal '"listener_form":"<invalid>","public_mtls":true,"authz_check":true' listener_form "форма вне перечня"

# 6. перечень гейта.
rows="$(awk '/^SERVICES="/{f=1; next} f && /^"/{exit} f && NF {print}' "$GATE")"
[ -n "$rows" ] || fatal "перечень SERVICES в $GATE не прочитан — гейт изменил форму"
for want in "kacho-notify|notify|kacho-umbrella-pg-notify" \
            "kacho-notify-api|notify-api|kacho-umbrella-pg-notify"; do
  grep -qxF -- "$want" <<<"$rows" || fail "в перечне гейта нет строки $want"
done
ok
grep -qxF -- "kacho-notify-probe|notify-probe|kacho-umbrella-pg-notifyprobe" <<<"$rows" \
  || fail "в перечне гейта нет строки пробы notify-probe с её базой"
ok
fwd="$(sed -n 's/^FORWARDER_NARROWING_REQUIRED="\${FORWARDER_NARROWING_REQUIRED:-\(.*\)}"$/\1/p' "$GATE")"
[ -n "$fwd" ] || fatal "перечень круга отправителей в $GATE не прочитан — гейт изменил форму"
for s in notify-api notify-probe; do
  [[ " $fwd " == *" $s "* ]] || fail "$s принимает пересланную личность, а круг отправителей у него не судится"
done
ok

outcome_verdict "строк перечня гейта: $(printf '%s\n' "$rows" | grep -c .)"
