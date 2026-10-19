#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# registry-anonymous-subject-test.sh — анонимный субъект плоскости данных реестра:
# одно общее значение на две стороны, исходы рендера зонта (kacho#3274,
# приёмка REG-PUB-1 ред. 13: Р9 п. 2, 3, 5; сценарии 52, 53; DoD S3, S4).
#
# Обозначения приёмки: G — `global.kacho.registry.anonymousSubjectId` (общее
# значение), K — `kaname.config.apiServer.registryToken.anonymousClientId`
# (собственного ключа у службы доступа нет), R — `registry.anonymousSubjectId`
# (собственный ключ реестра для одиночной установки). Стороны: служба доступа
# выводит `anonymous-client-id` в настройки, реестр —
# `KACHO_REGISTRY_ANONYMOUS_SUBJECT_ID` в окружение пода.
#
# Каждое сочетание — явным перекрытием поверх цепочки стека, поэтому «Дано» не
# зависит от значения профиля. Исключение одно — перепись S4: она судит именно
# профили, без перекрытия, по каждому стеку deploy/stacks.txt.
#
# Отказы утверждаются через render_must_fail_because по подстроке ПРИЧИНЫ: отказ
# по другой причине (несобранные зависимости) не засчитывается. Подстрока отказа
# при выключенном реестре обязана не встречаться в отказах (а)–(в) — иначе одна
# подстрока засчитала бы чужой отказ за свой.
#
# Исходы — tests/helm/outcome.sh: 0 зелено, 1 находка о дереве, 2 условие не
# создано. Кластера не требует; в дерево не пишет.

set -euo pipefail
SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="${REGISTRY_ANON_UMBRELLA:-$REPO_ROOT/helm/umbrella}"
STACKS="$(dirname "$0")/stacks.sh"

. "$(dirname "$0")/outcome.sh"
EXPECTED_ASSERTIONS=14

G_KEY="global.kacho.registry.anonymousSubjectId"
K_KEY="kaname.config.apiServer.registryToken.anonymousClientId"
R_KEY="registry.anonymousSubjectId"
SUBJ="registry-anonymous"
FORM='^[a-z][a-z0-9]*(-[a-z0-9]+)+$'

# Подстроки причин: (а), (б) — страж службы доступа; (в) — страж реестра;
# (г) — страж зонта (templates/registry-anonymous-subject-guard.yaml).
WHY_K="kaname: config.apiServer.registryToken.anonymousClientId="
WHY_R="registry: анонимный субъект плоскости данных объявлен ДВАЖДЫ"
WHY_OFF="задан при выключенном реестре (registry.enabled=false)"
# Как ключи названы в текстах отказов. Ключ K — именем в контексте подчарта
# службы доступа (там он и задаётся). Ключ R — с пробелом впереди: голое
# `registry.anonymousSubjectId` входит и в имя общего ключа G, и подстрокой
# назвало бы G вместо R.
K_IN_TEXT="config.apiServer.registryToken.anonymousClientId"
R_IN_TEXT=" $R_KEY="

require_helm
require_mikefarah_yq
[ -f "$UMBRELLA/Chart.yaml" ] || fatal "чарта зонта нет на диске ($UMBRELLA)"

# render <стек> [доводы helm…] — рендер цепочки стека; код — в HELM_RC.
render() {
  local stack="$1"; shift
  local chain
  chain="$(bash "$STACKS" --args "$stack" "$UMBRELLA")" \
    || fatal "цепочка стека '$stack' не читается из deploy/stacks.txt"
  [ -n "$chain" ] || fatal "цепочка стека '$stack' прочиталась пустой"
  # shellcheck disable=SC2086 # цепочка — слова `-f <файл>`, разбор по словам и нужен
  helm_try kacho-umbrella "$UMBRELLA" -n kacho $chain "$@"
}

# Стороны в успешном рендере. Пустая строка — ключа нет.
kaname_side() {
  printf '%s\n' "$HELM_OUT" | sed -n 's/^[[:space:]]*anonymous-client-id:[[:space:]]*"\{0,1\}\([^"]*\)"\{0,1\}[[:space:]]*$/\1/p'
}
kaname_side_count() { kaname_side | grep -c . || true; }
registry_side() {
  printf '%s\n' "$HELM_OUT" | yq -N 'select(.kind == "Deployment" and .metadata.name == "registry")
    | .spec.template.spec.containers[].env[]? | select(.name == "KACHO_REGISTRY_ANONYMOUS_SUBJECT_ID") | .value'
}
registry_pods() {
  printf '%s\n' "$HELM_OUT" | yq -N 'select(.kind == "Deployment" and .metadata.name == "registry") | .metadata.name' | grep -c . || true
}

# both_sides_equal <что> <ожидаемое> — обе стороны есть, по одной, и равны.
both_sides_equal() {
  local what="$1" want="$2" k r kn
  render_nonempty_or_fatal "$what"
  [ "$(registry_pods)" -eq 1 ] || fail "$what: пода реестра в рендере нет — сторону реестра судить не на чем"
  kn="$(kaname_side_count)"; k="$(kaname_side)"; r="$(registry_side)"
  [ "$kn" -eq 1 ] || fail "$what: anonymous-client-id у службы доступа встречается $kn раз, ожидается 1"
  [ "$k" = "$want" ] || fail "$what: anonymous-client-id службы доступа = '$k', ожидается '$want'"
  [ "$r" = "$want" ] || fail "$what: KACHO_REGISTRY_ANONYMOUS_SUBJECT_ID реестра = '$r', ожидается '$want'"
}

# refused <подстрока> <что> — отказ по названной причине; текст называет оба
# ключа; подстрока отказа выключенного реестра в чужом отказе не встречается.
refused() {
  local why="$1" what="$2" key1="$3" key2="$4"
  render_must_fail_because "$why" "$what" "$what: рендер ПРОШЁЛ, а обязан отказать"
  case "$HELM_ERR" in *"$key1"*) ;; *) helm_said "$what"; fail "$what: текст отказа не называет $key1" ;; esac
  case "$HELM_ERR" in *"$key2"*) ;; *) helm_said "$what"; fail "$what: текст отказа не называет $key2" ;; esac
  if [ "$why" != "$WHY_OFF" ]; then
    case "$HELM_ERR" in *"$WHY_OFF"*) helm_said "$what"; fail "$what: в чужом отказе стоит подстрока отказа выключенного реестра — она перестала различать причины" ;; esac
  fi
}

# ── 1. S4: значение профилей — во ВСЕХ стеках, обе стороны, одно, закрытой формы
names="$(bash "$STACKS" --names)" || fatal "имена стеков не читаются из deploy/stacks.txt"
total=0; good=0
while IFS= read -r stack; do
  [ -n "$stack" ] || continue
  total=$((total + 1))
  render "$stack"
  render_nonempty_or_fatal "стек $stack без перекрытия"
  k="$(kaname_side)"; r="$(registry_side)"; kn="$(kaname_side_count)"
  if [ "$kn" -eq 1 ] && [ -n "$k" ] && [ "$k" = "$r" ] && [[ "$k" =~ $FORM ]] && [ "${#k}" -le 63 ]; then
    good=$((good + 1))
  else
    violation "стек $stack: anonymous-client-id='$k' (раз: $kn), KACHO_REGISTRY_ANONYMOUS_SUBJECT_ID='$r' — обе стороны обязаны нести одно значение закрытой формы SC-39"
  fi
done <<<"$names"
[ "$total" -gt 0 ] || fatal "стеков в deploy/stacks.txt ноль — перепись по пустому перечню не вердикт"
echo "  перепись S4: стеков $total · обе стороны с одним значением закрытой формы $good из $total"
[ "$good" -eq "$total" ] || fail "S4: значение закрытой формы на обеих сторонах в $good стеках из $total"
ok

# ── 2. REG-PUB-1-52 (а): G пусто, своих ключей нет — ни одной стороны
render prod --set-string "$G_KEY="
render_nonempty_or_fatal "52 (а): prod, G пусто"
[ "$(kaname_side_count)" -eq 0 ] || fail "52 (а): при пустом общем значении у службы доступа есть anonymous-client-id"
[ -z "$(registry_side)" ] || fail "52 (а): при пустом общем значении у реестра есть KACHO_REGISTRY_ANONYMOUS_SUBJECT_ID"
ok

# ── 3. Близнец 52 и 53 (а), (б): G задано, K нет — обе стороны, одно значение
render prod --set-string "$G_KEY=$SUBJ"
both_sides_equal "близнец 53 (а): prod, G=$SUBJ" "$SUBJ"
ok

# ── 4. REG-PUB-1-53 (а): G пусто, K задан — отказ службы доступа
render prod --set-string "$G_KEY=" --set-string "$K_KEY=$SUBJ"
refused "$WHY_K" "53 (а): prod, G пусто, K=$SUBJ" "$K_IN_TEXT" "$G_KEY"
ok

# ── 5. REG-PUB-1-53 (б): K рядом с равным ему G — тот же отказ
render prod --set-string "$G_KEY=$SUBJ" --set-string "$K_KEY=$SUBJ"
refused "$WHY_K" "53 (б): prod, G=K=$SUBJ" "$K_IN_TEXT" "$G_KEY"
ok

# ── 6. REG-PUB-1-53 (в): R отличен от G — отказ реестра
render dev --set-string "$G_KEY=$SUBJ" --set-string "$R_KEY=other-anonymous"
refused "$WHY_R" "53 (в): dev, G=$SUBJ, R=other-anonymous" "$G_KEY" "$R_IN_TEXT"
ok

# ── 7. Близнец 53 (в): R равен G — обе стороны, одно значение
render dev --set-string "$G_KEY=$SUBJ" --set-string "$R_KEY=$SUBJ"
both_sides_equal "близнец 53 (в): dev, G=R=$SUBJ" "$SUBJ"
ok

# ── 8. Р9 п. 5: R при явно пустом G — только узнающая сторона
render dev --set-string "$G_KEY=" --set-string "$R_KEY=$SUBJ"
render_nonempty_or_fatal "Р9 п. 5: dev, G пусто, R=$SUBJ"
[ "$(registry_side)" = "$SUBJ" ] || fail "Р9 п. 5: собственный ключ реестра не доехал до KACHO_REGISTRY_ANONYMOUS_SUBJECT_ID"
[ "$(kaname_side_count)" -eq 0 ] || fail "Р9 п. 5: служба доступа чеканила бы анонимный токен по ключу реестра, которого она не видит"
ok

# ── 9–10. REG-PUB-1-53 (г): G задано, реестр выключен — отказ зонта, оба стека
for stack in dev prod; do
  render "$stack" --set-string "$G_KEY=$SUBJ" --set registry.enabled=false
  refused "$WHY_OFF" "53 (г): $stack, G=$SUBJ, registry.enabled=false" "$G_KEY" "registry.enabled"
  ok
done

# ── 11–12. Близнец (г): G пусто при выключенном реестре — законно, ни ключа, ни пода
for stack in dev prod; do
  render "$stack" --set-string "$G_KEY=" --set registry.enabled=false
  render_nonempty_or_fatal "близнец 53 (г): $stack, G пусто, registry.enabled=false"
  [ "$(kaname_side_count)" -eq 0 ] || fail "близнец 53 (г) $stack: при пустом общем значении у службы доступа есть anonymous-client-id"
  [ "$(registry_pods)" -eq 0 ] || fail "близнец 53 (г) $stack: registry.enabled=false, а под реестра в рендере есть"
  ok
done

# ── 13. Второй близнец (г): реестр включён явно — обе стороны
render dev --set-string "$G_KEY=$SUBJ" --set registry.enabled=true
both_sides_equal "второй близнец 53 (г): dev, G=$SUBJ, registry.enabled=true" "$SUBJ"
ok

# ── 14. Ключа registry.enabled нет вовсе: подчарт включён (условие Chart.yaml
# не разрешается — helm его не применяет), отказа нет, обе стороны
render dev --set-string "$G_KEY=$SUBJ" --set registry.enabled=null
both_sides_equal "ключа registry.enabled нет: dev, G=$SUBJ" "$SUBJ"
ok

outcome_verdict "стеков в переписи S4: $total"
