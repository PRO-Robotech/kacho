#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# cert-issuance-policy-render-test.sh — ПОЛИТИКА ВЫПУСКА СЕРТИФИКАТОВ СЛУЖБ В
# РЕНДЕРЕ КАЖДОЙ ЦЕПОЧКИ deploy/stacks.txt (замысел З30, шаблон
# helm/umbrella/templates/cert-issuance-policy.yaml).
#
# Поведение политики судит живая проба (deploy/tests/cluster/
# cert-issuance-policy-test.sh, NTF1-J01/J02) и секция E гейта посадки. Здесь —
# то, что видно ДО подъёма и что живая проба увидела бы только поломкой стенда:
#
#   1. политика рендерится везде, где УЦ наш; выключена — ТОЛЬКО там, где
#      cert-manager чужой (caNamespace указывает на пространство оператора
#      площадки, не на пространство релиза). Выключение на своём cert-manager —
#      находка: стенд молча остался бы без политики;
#   2. обе ветви — `workload` и `certificates` — есть и смотрят на внутренний УЦ
#      цепочки;
#   3. ветвь `certificates` открыта ровно учётке контроллера cert-manager, имя
#      которой производит релиз CERT_MANAGER_RELEASE (deploy/Makefile): иначе
#      ни один листовой сертификат служб не выпустится;
#   4. каждый листовой сертификат внутреннего УЦ, который рендерит цепочка,
#      политике ВЫПУСКАЕМ: URI SAN называет пространство самого сертификата,
#      DNS-имена с точкой — того же пространства, общего имени и IP нет. Ровно
#      этот класс прятался в storage: его SAN называл `ns/kacho-storage`, а
#      служба живёт в `kacho` — под политикой у неё не было бы сертификата.
#      Правила здесь — зеркало CEL-выражений ветви `certificates` для двух
#      признаков (пространство URI и DNS); авторитетна живая проба.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/../.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"
. "$HERE/outcome.sh"

require_helm
require_mikefarah_yq
require_python3
require_fresh_dep_charts "$UMBRELLA"

REL="$(sed -n 's/^CERT_MANAGER_RELEASE[[:space:]]*:=[[:space:]]*//p' "$DEPLOY_ROOT/Makefile")"
[ -n "$REL" ] || fatal "CERT_MANAGER_RELEASE не прочитан из deploy/Makefile — учётку контроллера сверять не с чем"
# Имя учётки контроллера = полное имя релиза чарта cert-manager: релиз, в имени
# которого уже есть имя чарта, его и даёт (шаблон cert-manager.fullname).
case "$REL" in
  *cert-manager*) CM_SA="$REL" ;;
  *) CM_SA="$REL-cert-manager" ;;
esac

NAMES="$(bash "$HERE/stacks.sh" --names)" || fatal "состав цепочек не прочитан (stacks.sh)"
[ -n "$NAMES" ] || fatal "цепочек ноль — судить не о чем"

stacks=0 enabled=0 disabled=0 certs=0
for stack in $NAMES; do
  args="$(bash "$HERE/stacks.sh" --args "$stack" "$UMBRELLA")" || fatal "цепочка $stack не прочитана"
  # shellcheck disable=SC2086
  helm_try kacho-umbrella "$UMBRELLA" -n kacho $args --set cert-manager.enabled=false
  render_or_fatal "цепочка $stack"
  stacks=$((stacks + 1))
  out="$(printf '%s' "$HELM_OUT" | python3 "$HERE/cert-issuance-policy-render.py" "$stack" "$CM_SA" kacho)" \
    || fatal "разбор рендера цепочки $stack не состоялся: $out"
  while IFS='|' read -r kind a b; do
    case "$kind" in
      ENABLED) enabled=$((enabled + 1)); ok ;;
      DISABLED) disabled=$((disabled + 1)); ok; echo "  · $stack: политика выключена — cert-manager чужой ($a)" ;;
      CERT) certs=$((certs + 1)) ;;
      OK) ok; echo "  ✓ $stack: $a" ;;
      BAD) ok; violation "$stack: $a" ;;
      *) fatal "разбор вернул неузнанную строку «$kind|$a|$b»" ;;
    esac
  done <<<"$out"
done

echo "  перепись: цепочек $stacks (политика включена $enabled, выключена $disabled), листовых сертификатов внутреннего УЦ осмотрено $certs"
[ "$certs" -gt 0 ] || fail "листовых сертификатов внутреннего УЦ ноль — правило 4 не осмотрело ничего"
[ "$enabled" -gt 0 ] || fail "политика не включена ни в одной цепочке"
findings_verdict "цепочек $stacks, сертификатов $certs"
