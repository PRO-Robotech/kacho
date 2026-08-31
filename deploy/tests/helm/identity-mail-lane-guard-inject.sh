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

rc=0; red=0; green=0
assert() { # имя · ожидание(RED|GREEN) · фраза-улика · --set …
  local name="$1" want="$2" needle="$3"; shift 3
  local got
  # shellcheck disable=SC2086 -- ARGS это намеренно разбиваемая цепочка -f
  if helm template kacho-umbrella ./helm/umbrella -n kacho $ARGS "$@" >"$TMP/out" 2>&1
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

echo "=== законные близнецы: страж обязан молчать ==="
assert "неявный TLS вместо STARTTLS"    GREEN "" \
  --set 'global.kacho.identity.smtp.connectionURI=smtps://kacho-umbrella-mailpit:465/'
assert "приёмник выключен, полосы нет"  GREEN "" \
  --set mailpit.enabled=false \
  --set global.kacho.identity.smtp.connectionURI= \
  --set global.kacho.identity.smtp.fromAddress= \
  --set global.kacho.identity.smtp.fromName=

echo "перепись: инъекций красных $red · законных близнецов зелёных $green"
[ "$red" -ge 12 ] || { echo "ОТКАЗ: красных инъекций $red — доказательство неполно"; rc=1; }
[ "$green" -ge 3 ] || { echo "ОТКАЗ: зелёных близнецов $green — отрицание не проверено в обратную сторону"; rc=1; }
[ "$rc" = 0 ] && echo "ИТОГ: страж почтовой полосы способен упасть и способен смолчать" \
              || echo "ИТОГ: ОТКАЗ"
exit "$rc"
