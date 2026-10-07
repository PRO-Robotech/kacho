#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# КООРДИНАТЫ ЦЕРЕМОНИИ ДОХОДЯТ ДО КРАЯ ПО HTTP, А НЕ gRPC-ПРОКСИРОВАНИЕМ (kacho#2860).
#
# Ловит конкретный случай: вход края объявлен одним правилом `path: /` с
# протоколом бэкенда GRPCS, и `GET /iam/v1/authorize`, `POST /iam/v1/token`,
# `GET /.well-known/oauth-authorization-server` посредник проксирует gRPC —
# обычный HTTP-запрос клиента OAuth до ретрансляции края не доходит.
#
# На КАЖДОМ стеке таблицы `deploy/stacks.txt` рендерит умбреллу целиком и
# отдаёт рендер разбору `ceremony-ingress-audit.py`, который решает исход
# маршрутизации посредника по разобранным объектам Ingress и утверждает:
#   1. на каждой из трёх координат выигрывает правило ТОЧНОГО совпадения,
#      ведущее к краю на слушатель `tls` протоколом HTTPS;
#   2. отрицательный близнец `/iam/v1/authorize:check` и соседи координат
#      остаются на прежнем правиле (том, что выигрывает `/`);
#   3. прочие пути края — на прежнем правиле;
#   4. второго внешнего входа нет: хост, класс входа и секрет TLS у всех
#      объектов, ведущих к краю, одни, правил сверх прежнего и трёх координат нет.
#
# Стек, в рендере которого входа края нет, — не находка и не успех: он
# печатается счётчиком. Ни одного осмотренного стека — провал, а не чистота.
# Самопроверка разбора — `ceremony-ingress-audit.py --self-test`.

set -uo pipefail

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"

# shellcheck source=deploy/tests/helm/outcome.sh
. "$(dirname "$0")/outcome.sh"
# Цепочки ГЕЙТА рендера — обёрткой lib/render-chain.sh: к `prod` она дописывает
# слой оператора из каталога образцов (поставка не несёт ни узла почты, Д48, ни
# числа доверенных прыжков края, приёмка NTF-2 Р8, Д51). Обёртка подключает
# stacks.sh сама.
# shellcheck source=deploy/tests/helm/lib/render-chain.sh
. "$(dirname "$0")/lib/render-chain.sh"

require_helm
require_python_yaml
require_umbrella_charts "$UMBRELLA"
require_fresh_dep_charts "$UMBRELLA"

AUDIT="$(dirname "$0")/ceremony-ingress-audit.py"
[ -r "$AUDIT" ] || fatal "разборщик $AUDIT не читается — предпосылка исчезла, а не дерево стало чистым"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

JUDGED=0; SKIPPED=0; TOT_COORD=0; TOT_NEAR=0; TOT_OTHER=0

# Код перечня стеков потребован присваиванием: подстановка в списке `for`
# теряет код, и отказ читателя таблицы обошёл бы ноль стеков при нулевом коде.
STACKS="$(stacks_names)" \
  || fatal "перечень стеков не прочитан — обходить нечего, и это не чистое дерево"
for stack in $STACKS; do
  args="$(render_chain_args "$stack" "$UMBRELLA" operator.yaml)" \
    || fatal "стек $stack: цепочка стенда не прочитана — helm без единого -f сел бы на умолчания чарта"
  # shellcheck disable=SC2086
  helm_try kacho-umbrella "$UMBRELLA" $args --namespace kacho
  render_or_fatal "стек $stack → умбрелла целиком"
  render_nonempty_or_fatal "стек $stack → умбрелла целиком"
  printf '%s\n' "$HELM_OUT" >"$TMP/$stack.yaml"

  out="$(python3 "$AUDIT" "$TMP/$stack.yaml")" \
    || fatal "разбор рендера «$stack» отказал — это НЕ находка о дереве"

  seen=0
  while IFS= read -r line; do
    case "$line" in
      FINDING\ *) violation "[$stack] ${line#FINDING }" ;;
      SKIP\ *)
        SKIPPED=$((SKIPPED + 1)); seen=1
        echo "  [$stack] не осмотрен: ${line#SKIP }"
        ;;
      SCOPE\ *)
        read -r _ c nb o <<<"$line"
        [ -n "${o:-}" ] \
          || fatal "разбор стека «$stack» отдал перепись без третьего поля — разборщик и читатель разошлись"
        TOT_COORD=$((TOT_COORD + c)); TOT_NEAR=$((TOT_NEAR + nb)); TOT_OTHER=$((TOT_OTHER + o))
        JUDGED=$((JUDGED + 1)); seen=1
        ok
        echo "  [$stack] координат сверено: $c · соседей и близнецов: $nb · прочих путей: $o"
        ;;
    esac
  done <<<"$out"
  [ "$seen" -eq 1 ] || fatal "разбор стека «$stack» не отдал ни переписи, ни пропуска — разборщик и читатель разошлись"
done

[ "$JUDGED" -gt 0 ] \
  || fail "вход края не осмотрен ни в одном стеке (пропущено $SKIPPED) — «ноль находок» здесь означает «ноль прочитанного»"

findings_verdict "стеков осмотрено $JUDGED, без входа края $SKIPPED; координат сверено $TOT_COORD, соседей и близнецов $TOT_NEAR, прочих путей $TOT_OTHER"
