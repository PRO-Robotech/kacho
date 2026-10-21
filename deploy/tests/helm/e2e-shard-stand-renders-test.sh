#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# e2e-shard-stand-renders-test.sh — стенд КАЖДОГО шарда e2e-newman рендерится
# той строкой выключений, которую получит его задание (kacho#3274).
#
# Предмет. Задание шарда поднимает стенд `make dev-up DEV_UP_VALUES=<helm>`, где
# `<helm>` выведен из deploy/e2e-shards.json (deploy/scripts/e2e-shard-matrix.py
# — единственный вывод; проба зовёт его же, копии логики нет). Стражи рендера
# зонта судят сочетания значений, а шард сочетание МЕНЯЕТ: выключает
# компоненты, которых не называет. Ни одна проба профилей этого сочетания не
# видела, и отказ всплывал лишь на подъёме стенда в конвейере, тремя шардами
# сразу (сборка w13: страж анонимного субъекта при registry.enabled=false).
#
# Утверждения, на шард:
#   • обе фазы подъёма (цепочки `dev` и `dev-prod`, как в рецепте dev-up, и
#     `--set cert-manager.enabled=false` рецепта) рендерятся, код 0, вывод не пуст;
#   • реестр выключен шардом — пода реестра нет, и анонимного потока нет ни на
#     одной стороне (у службы доступа нет `anonymous-client-id`): приёмка
#     REG-PUB-1 ред. 13, Р9 п. 5 и близнец 53 (г) — «G пусто при выключенном
#     реестре законно»;
#   • реестр шардом включён — поток включён на обеих сторонах одним значением
#     (значение профиля не снято);
# и близнец, отличный РОВНО одним фактом: строка шарда без реестра плюс
# возвращённое общее значение — отказ стража зонта по его причине. Страж цел;
# зелёным шард делает спутник выключения, а не ослабление стража.
#
# Исходы — tests/helm/outcome.sh: 0 зелено, 1 находка о дереве, 2 условие не
# создано. Кластера не требует; в дерево не пишет.

set -euo pipefail
SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="${E2E_SHARD_UMBRELLA:-$REPO_ROOT/helm/umbrella}"
STACKS="$(dirname "$0")/stacks.sh"
MATRIX="$REPO_ROOT/scripts/e2e-shard-matrix.py"

# shellcheck source=deploy/tests/helm/outcome.sh
. "$(dirname "$0")/outcome.sh"

G_KEY="global.kacho.registry.anonymousSubjectId"
SUBJ="registry-anonymous"
WHY_OFF="задан при выключенном реестре (registry.enabled=false)"
# Опции рецепта dev-up, общие всем фазам (UMBRELLA_OPTS в deploy/Makefile).
RECIPE_OPTS=(--set cert-manager.enabled=false)

require_helm
require_mikefarah_yq
command -v python3 >/dev/null 2>&1 || fatal "нужен python3 — вывести строку шарда нечем"
[ -f "$UMBRELLA/Chart.yaml" ] || fatal "чарта зонта нет на диске ($UMBRELLA)"
[ -f "$MATRIX" ] || fatal "вывода матрицы шардов нет на диске ($MATRIX)"
# Зависимости зонта — предпосылка, а не предмет: без них КАЖДЫЙ рендер отказал бы
# по причине, к шардам не относящейся, и отказ ушёл бы находкой о дереве (код 1)
# там, где условие не создано (код 2) — класс #1214, его судит
# three-outcomes-distinguishable-test.sh. Проверка ДО первого рендера.
require_umbrella_charts "$UMBRELLA"
require_fresh_dep_charts "$UMBRELLA"

# render <стек> [доводы helm…] — рендер цепочки стека; код — в HELM_RC.
render() {
  local stack="$1"; shift
  local chain
  chain="$(bash "$STACKS" --args "$stack" "$UMBRELLA")" \
    || fatal "цепочка стека '$stack' не читается из deploy/stacks.txt"
  [ -n "$chain" ] || fatal "цепочка стека '$stack' прочиталась пустой"
  # shellcheck disable=SC2086 # цепочка — слова `-f <файл>`, разбор по словам и нужен
  helm_try kacho-umbrella "$UMBRELLA" -n kacho $chain "${RECIPE_OPTS[@]}" "$@"
}

kaname_side_count() {
  printf '%s\n' "$HELM_OUT" | grep -cE '^[[:space:]]*anonymous-client-id:' || true
}
kaname_side() {
  printf '%s\n' "$HELM_OUT" | sed -n 's/^[[:space:]]*anonymous-client-id:[[:space:]]*"\{0,1\}\([^"]*\)"\{0,1\}[[:space:]]*$/\1/p'
}
registry_side() {
  printf '%s\n' "$HELM_OUT" | yq -N 'select(.kind == "Deployment" and .metadata.name == "registry")
    | .spec.template.spec.containers[].env[]? | select(.name == "KACHO_REGISTRY_ANONYMOUS_SUBJECT_ID") | .value'
}
registry_pods() {
  printf '%s\n' "$HELM_OUT" | yq -N 'select(.kind == "Deployment" and .metadata.name == "registry") | .metadata.name' | grep -c . || true
}

ids="$(python3 "$MATRIX" --ids)" || fatal "идентификаторы шардов не выводятся из deploy/e2e-shards.json"
shards=0; off_shards=0
while IFS= read -r id; do
  [ -n "$id" ] || continue
  shards=$((shards + 1))
  helm_line="$(python3 "$MATRIX" --helm "$id")" || fatal "строка helm шарда '$id' не выводится"
  # shellcheck disable=SC2206 # строка уходит в make без кавычек — разбор по словам и есть её смысл
  words=($helm_line)
  registry_off=0
  case " $helm_line " in *" --set registry.enabled=false "*) registry_off=1 ;; esac

  for stack in dev dev-prod; do
    what="шард $id, фаза $stack: $helm_line"
    render "$stack" "${words[@]}"
    if [ "$HELM_RC" -ne 0 ]; then
      # Отказ о зависимостях — условие прогона (код 2), а не свойство шарда:
      # подсказку и код решает общая реализация.
      case "$HELM_ERR" in
        *"missing in charts/ directory"*|*"dependency build"*) render_or_fatal "$what" ;;
      esac
      helm_said "$what"
      fail "$what — стенд шарда не рендерится; задание шарда упадёт на подъёме"
    fi
    render_nonempty_or_fatal "$what"
    if [ "$registry_off" -eq 1 ]; then
      [ "$(registry_pods)" -eq 0 ] || fail "$what: реестр выключен шардом, а под реестра в рендере есть"
      [ "$(kaname_side_count)" -eq 0 ] || fail "$what: реестр выключен, а служба доступа несёт anonymous-client-id — выдающая сторона без узнающей"
    else
      [ "$(registry_pods)" -eq 1 ] || fail "$what: реестр шардом включён, а пода реестра в рендере нет"
      k="$(kaname_side)"; r="$(registry_side)"
      [ -n "$k" ] && [ "$k" = "$r" ] || fail "$what: при включённом реестре обе стороны обязаны нести одно значение (служба доступа '$k', реестр '$r')"
    fi
    ok
  done

  if [ "$registry_off" -eq 1 ]; then
    off_shards=$((off_shards + 1))
    what="близнец шарда $id: строка шарда и $G_KEY=$SUBJ"
    render dev "${words[@]}" --set-string "$G_KEY=$SUBJ"
    render_must_fail_because "$WHY_OFF" "$what" "$what: рендер ПРОШЁЛ — страж зонта при выключенном реестре больше не отказывает"
    ok
  fi
done <<<"$ids"

[ "$shards" -gt 0 ] || fatal "шардов ноль — проба по пустому перечню не вердикт"
[ "$off_shards" -gt 0 ] || fatal "ни один шард не выключает реестр — близнецу стража не на чем стоять"
EXPECTED_ASSERTIONS=$((shards * 2 + off_shards))
outcome_verdict "шардов $shards, из них без реестра $off_shards"
