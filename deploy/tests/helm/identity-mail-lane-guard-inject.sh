#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# identity-mail-lane-guard-inject.sh — доказательство того, что страж почтовой
# полосы (templates/identity-mail-lane-guard.yaml, место С1 решения Р4а приёмки
# ID-MAIL-1) СПОСОБЕН уронить рендер, называет ручку, и МОЛЧИТ на законных
# близнецах. Без второй половины «зелено» неотличимо от «ничего не проверяет».
#
# ПОЧЕМУ БЛИЗНЕЦОВ ТРИ, А НЕ ОДИН. Отрицаний у стража несколько, и каждое
# рискует накрыть законную форму:
#   · «полосы нет вовсе» законно там, где приёмник выключен (боевая площадка
#     получает ретранслятор слоем учётных данных, которого рендер не видит);
#   · «неявный TLS» (`smtps://`) — такая же защищённая полоса, как STARTTLS,
#     и страж, знающий только одну схему, отверг бы работающую посадку;
#   · неизменённое дерево обязано рендериться.
#
# ИНЪЕКЦИЯ РОНЯЕТ ТОЛЬКО ПРОВЕРЯЕМОЕ: каждая — это `--set` ОДНОЙ величины
# поверх неизменного дерева. Инъекция вида «завести ещё один объект» нарушала бы
# заодно всё, что требуется от объектов вообще, и красное приходило бы от соседа.
#
# Дерево не правится: всё идёт через `--set`.

set -uo pipefail
cd "$(dirname "$0")/../.."

command -v helm >/dev/null || { echo "SKIP: helm не установлен — доказательство не выполнено"; exit 2; }

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
ARGS="$(bash tests/helm/stacks.sh --args dev ./helm/umbrella)"

# ── ПРЕДПОСЫЛКА: ЗАВИСИМОСТИ ЧАРТА СОБРАНЫ ──────────────────────────────────
# Без них `helm template` отказывает ДО первого шаблона — то есть КАЖДАЯ ось
# краснеет по причине, к стражу отношения не имеющей. Красные оси при этом
# выглядят исполненными («ждали RED — получили RED»), и доказательство
# становится вакуумным; от этого спасает только сверка ФРАЗЫ-УЛИКИ, то есть
# случайно. «Условие не создано» — НЕ вердикт о дереве (e2e-flow.md §6): свой
# код возврата, свой текст, и он не зачитывается ни в успех, ни в отказ.
if helm template kacho-umbrella ./helm/umbrella -n kacho >/dev/null 2>"$TMP/dep"; then
  :
elif grep -q 'missing in charts/' "$TMP/dep"; then
  echo "SKIP: зависимости чарта не собраны — доказательство НЕ ВЫПОЛНЕНО (не красное)."
  echo "      создать условие: helm dependency build ./helm/umbrella"
  exit 2
fi

rc=0; red=0; green=0
# assert_rel — то же, но ИМЕНЕМ РЕЛИЗА наружу. Отдельная форма нужна ровно одной
# оси: имя рабочего объекта приёмника складывается из имени релиза, а узел в
# полосе — литерал профиля, и расходятся они молча. Проверить это, не меняя
# релиз, невозможно by construction.
assert_rel() { # релиз · имя · ожидание(RED|GREEN) · фраза-улика · --set …
  local rel="$1" name="$2" want="$3" needle="$4"; shift 4
  local got
  # shellcheck disable=SC2086 -- ARGS это намеренно разбиваемая цепочка -f
  if helm template "$rel" ./helm/umbrella -n kacho $ARGS "$@" >"$TMP/out" 2>&1
  then got=GREEN; else got=RED; fi
  if [ "$got" != "$want" ]; then
    echo "  ОТКАЗ $name → $got, ожидалось $want"; tail -3 "$TMP/out" | sed 's/^/       /'; rc=1; return
  fi
  if [ "$want" = RED ] && ! grep -qF -- "$needle" "$TMP/out"; then
    echo "  ОТКАЗ $name → RED, но вывод не называет $needle"; tail -3 "$TMP/out" | sed 's/^/       /'; rc=1; return
  fi
  echo "  ok   $name → $got"
  [ "$want" = RED ] && red=$((red+1)) || green=$((green+1))
}

# Умолчание — имя релиза, которым стенд поднимают рецепты (`STACK_RELEASE ?=`).
assert() { assert_rel kacho-umbrella "$@"; }

echo "=== контроль: неизменённое дерево рендерится ==="
assert "контроль" GREEN ""

echo "=== инъекции: возвращённый дефект — находка с именем ручки ==="
assert "адрес отправителя снят"        RED "fromAddress"     --set global.kacho.identity.smtp.fromAddress=
assert "узел снят, отправитель остался" RED "connectionURI"  --set global.kacho.identity.smtp.connectionURI=
assert "имя отправителя снято"          RED "fromName"       --set global.kacho.identity.smtp.fromName=
assert "шифрование снято"               RED "disable_starttls" \
  --set 'global.kacho.identity.smtp.connectionURI=smtp://mailhog.kacho.svc:1025/?disable_starttls=true'
assert "проверка сертификата снята"     RED "skip_ssl_verify" \
  --set 'global.kacho.identity.smtp.connectionURI=smtps://h:465/?skip_ssl_verify=true'
assert "неподставленная ссылка"         RED 'подстановки'    \
  --set 'global.kacho.identity.smtp.connectionURI=smtp://${RELAY}:1025/'
assert "чужая схема"                    RED "схема не распознана" \
  --set 'global.kacho.identity.smtp.connectionURI=http://h:25/'
assert "схема без узла"                 RED "узла нет"       \
  --set 'global.kacho.identity.smtp.connectionURI=smtp://'
assert "образ приёмника не прибит"      RED "digest"         --set mailpit.image.digest=
assert "дайджест — не дайджест"         RED "не дайджест"    --set mailpit.image.digest=v1.31.0
assert "приёмник без внутреннего CA"    RED "mtls.enabled"   --set mtls.enabled=false
assert "узел поднят, полосы нет"        RED "писать в него некому" \
  --set global.kacho.identity.smtp.connectionURI= \
  --set global.kacho.identity.smtp.fromAddress= \
  --set global.kacho.identity.smtp.fromName=
assert "полоса названа на приёмник, а он выключен" RED "приёмник ВЫКЛЮЧЕН" \
  --set mailpit.enabled=false
assert_rel kacho "полоса названа на ЧУЖОЙ приёмник"  RED "поднимает приёмник под именем"
assert_rel stand-a "то же, имя релиза иное вовсе"    RED "поднимает приёмник под именем"

# ── УДОСТОВЕРЕНИЕ: ИСТОЧНИК ОБЪЯВЛЯЕТСЯ, ВЕЛИЧИНА — НЕТ (решение Р6) ────────
#
# Каждая ось двигает ОДНУ величину поверх неизменного дерева: инъекция вида
# «завести ещё один объект» нарушала бы заодно всё, что требуется от объектов
# вообще, и красное приходило бы от соседа (testing.md §«Гейт на класс», п. 2в).
#
# ЗДЕСЬ НЕТ НИ ОДНОГО НАСТОЯЩЕГО УДОСТОВЕРЕНИЯ: пароль в оси «литеральное
# удостоверение» — заведомо негодная строка, и она нужна лишь затем, чтобы в
# части адреса до «@» появилось двоеточие.
echo "=== удостоверение: источник и его половины ==="
assert "литеральное удостоверение в адресе" RED "УДОСТОВЕРЕНИЕ внутри адреса" \
  --set 'global.kacho.identity.smtp.connectionURI=smtp://noreply%40kacho.cloud:not-a-secret@kacho-umbrella-mailpit:1025/'
assert "узел требует удостоверения, источника нет" RED "ИСТОЧНИК удостоверения не объявлен" \
  --set 'global.kacho.identity.smtp.connectionURI=smtp://noreply%40kacho.cloud@kacho-umbrella-mailpit:1025/'
assert "источник объявлен, имени пользователя в адресе нет" RED "имени пользователя НЕ несёт" \
  --set global.kacho.identity.smtp.credentialSecret.name=kacho-identity-smtp \
  --set global.kacho.identity.smtp.credentialSecret.key=password
assert "источник объявлен, узла нет" RED "узел НЕ задан" \
  --set mailpit.enabled=false \
  --set global.kacho.identity.smtp.connectionURI= \
  --set global.kacho.identity.smtp.fromAddress= \
  --set global.kacho.identity.smtp.fromName= \
  --set global.kacho.identity.smtp.credentialSecret.name=kacho-identity-smtp \
  --set global.kacho.identity.smtp.credentialSecret.key=password
# ФРАЗА-УЛИКА БЕЗ ОБРАТНЫХ КАВЫЧЕК: внутри двойных они исполняются оболочкой, и
# улика молча превратилась бы в пустую строку — то есть ось перестала бы сверять
# ТЕКСТ отказа, оставаясь на вид исполненной.
assert "источник объявлен наполовину: имя без ключа" RED 'задано, а' \
  --set global.kacho.identity.smtp.credentialSecret.name=kacho-identity-smtp
assert "источник объявлен наполовину: ключ без имени" RED 'задан, а' \
  --set global.kacho.identity.smtp.credentialSecret.key=password

echo "=== законные близнецы: страж обязан молчать ==="
# ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ К ОСЯМ УДОСТОВЕРЕНИЯ. Без него отрицания выше зеленели бы
# на страже, отвергающем ЛЮБОЙ адрес с «@», — то есть на сломанном.
assert "имя пользователя и объявленный источник — законная пара" GREEN "" \
  --set 'global.kacho.identity.smtp.connectionURI=smtp://noreply%40kacho.cloud@kacho-umbrella-mailpit:1025/' \
  --set global.kacho.identity.smtp.credentialSecret.name=kacho-identity-smtp \
  --set global.kacho.identity.smtp.credentialSecret.key=password
assert "неявный TLS вместо STARTTLS"    GREEN "" \
  --set 'global.kacho.identity.smtp.connectionURI=smtps://kacho-umbrella-mailpit:465/'
assert "приёмник выключен, полосы нет"  GREEN "" \
  --set mailpit.enabled=false \
  --set global.kacho.identity.smtp.connectionURI= \
  --set global.kacho.identity.smtp.fromAddress= \
  --set global.kacho.identity.smtp.fromName=

assert "полное имя службы приёмника"    GREEN "" \
  --set 'global.kacho.identity.smtp.connectionURI=smtp://kacho-umbrella-mailpit.kacho.svc:1025/'
assert "внешний ретранслятор"           GREEN "" \
  --set 'global.kacho.identity.smtp.connectionURI=smtps://smtp.example.com:465/'

echo "перепись: инъекций красных $red · законных близнецов зелёных $green"
[ "$red" -ge 21 ] || { echo "ОТКАЗ: красных инъекций $red — доказательство неполно"; rc=1; }
[ "$green" -ge 6 ] || { echo "ОТКАЗ: зелёных близнецов $green — отрицание не проверено в обратную сторону"; rc=1; }
[ "$rc" = 0 ] && echo "ИТОГ: страж почтовой полосы способен упасть и способен смолчать" \
              || echo "ИТОГ: ОТКАЗ"
exit "$rc"
