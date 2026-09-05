#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# prerequisite-secrets-test.sh — обязательная ссылка на секрет обязана быть
# ВЫПОЛНИМОЙ: секрет либо создаёт сам чарт, либо его заводит шаг, который цель
# развёртывания РЕАЛЬНО исполняет.
#
# ЧТО ЛОВИТ. `secretKeyRef` без `optional: true` — жёсткое предусловие: kubelet не
# запустит контейнер, пока секрета нет (CreateContainerConfigError), а `helm --wait`
# при этом просто выстаивает свой таймаут и падает «не дождался», не назвав причины.
# Такое предусловие живёт в манифесте, а исполняется — в Makefile, и эти два места
# расходятся молча.
#
# Реальный случай: values.dev-prod.yaml ссылался на `kaname-hook-token`
# обязательной ссылкой из пода Hydra и её automigrate-Job'а; ни один шаблон его не
# создаёт; заводит только scripts/dev-prod-secrets.sh — которого цель `dev-prod-up`
# не звала. Требование существовало одним комментарием в values. Профиль `dev`
# такой ссылки тогда не нёс, поэтому `dev-up` работал и дефект был не виден.
#
# С задачи #948 её несёт и `dev`: величину обратного вызова провайдер берёт
# обязательной ссылкой уже там (необязательная дала бы пустой заголовок, полосу
# отвергли бы на первом обращении, а стенд выглядел бы поднятым), и посев
# секретов переставлен ПЕРЕД первым прогоном helm. Проверка от этого не меняется —
# меняется только то, что теперь под ней два профиля, а не один.
#
# ЧТО ИМЕННО УТВЕРЖДАЕТСЯ (для каждого разворачиваемого профиля):
#   для КАЖДОЙ обязательной secretKeyRef-ссылки в отрендеренных подах —
#   имя секрета либо встречается среди Secret'ов самого рендера, либо создаётся
#   скриптом, который ЗОВЁТ цель Makefile, разворачивающая этот профиль.
# Второе условие проверяется по ОБОИМ файлам сразу: скрипт заводит секрет И цель
# зовёт скрипт. Одного из двух недостаточно — ровно из-за этого дефект и возник.
#
# ЧЕГО НЕ УТВЕРЖДАЕТ: что содержимое секрета корректно (это рантайм) и что
# `optional: true`-ссылки чем-то обеспечены (они на то и optional).
#
# Офлайновый харнесс над `helm template`, кластер не нужен. Зеркалит tests/helm/*.
# Самопроверка: --self-test (гейт обязан краснеть на внесённом дефекте и молчать
# на законной конструкции той же формы).
set -uo pipefail
# Состав стендов — из ЕДИНСТВЕННОЙ таблицы дерева (deploy/stacks.txt).
# Своей копии цепочек здесь нет: копии разъезжались молча.
. "$(dirname "$0")/stacks.sh"

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
MAKEFILE="$REPO_ROOT/Makefile"
SECRETS_SH="scripts/dev-prod-secrets.sh"

# Три исхода — ОДНОЙ реализацией на весь каталог: 0 зелено · 1 находка о дереве ·
# 2 условие не создано (плюс текст самого helm). Прежде «профиль цели не
# рендерится» и «нет python3/PyYAML» объявлялись находкой — тем же кодом 1, что и
# невыполнимое предусловие стенда, — и вызывающий не мог их различить машинно
# (задача #1214).
# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
require_helm

# Цели Makefile, которые реально разворачивают стенд, и СТЕК, которым каждая это
# делает. Формат: <make-цель>|<стек>. Сама цепочка `-f` берётся из единственной
# таблицы дерева — здесь была её копия, и копия стареет молча: цель может
# получить новый слой, а проверка продолжит рендерить прежний состав и
# отчитываться зелёным.
PROFILES=(
  "dev-up|$(stacks_args dev "$UMBRELLA")"
  "dev-prod-up|$(stacks_args dev-prod "$UMBRELLA")"
)
EXPECTED_ASSERTIONS="${#PROFILES[@]}"

# target_body <make-цель> — рецепт цели: строка объявления + все строки-команды
# (в Makefile они начинаются с таба), до первой строки левого края.
target_body() {
  awk -v t="^$1:" '$0 ~ t {inb=1; print; next} inb && /^[^\t]/ {exit} inb {print}' "$MAKEFILE"
}

# provisioned_by <make-цель> — имена секретов, которые цель ЗАВЕДЁТ при исполнении.
# Пусто, если ни цель, ни её make-предусловия не зовут скрипт секретов: тогда всё,
# чего не создал чарт, — дыра.
provisioned_by() {
  local body deps d
  body="$(target_body "$1")"
  # `dev-prod-up: dev-up` — цель исполняет рецепты своих make-предусловий.
  deps="$(printf '%s\n' "$body" | head -1 | sed -E 's/^[^:]*:[[:space:]]*//')"
  for d in $deps; do body="$body"$'\n'"$(target_body "$d")"; done
  [[ "$body" == *"$SECRETS_SH"* ]] || return 0
  # Скрипт заводит ровно те секреты, которые в нём создаются `create secret generic`.
  grep -oE 'create secret generic +[a-z0-9-]+' "$REPO_ROOT/$SECRETS_SH" | awk '{print $4}'
}

# unmet <render-файл> <список-заводимых-секретов> → строки «<секрет> <потребитель>»
unmet() {
  python3 - "$1" "$2" <<'PY'
import sys, yaml
provisioned = set(sys.argv[2].split())
docs = [d for d in yaml.safe_load_all(open(sys.argv[1])) if d]
provided = {d['metadata']['name'] for d in docs if d.get('kind') == 'Secret'}
seen = set()
for d in docs:
    spec = d.get('spec') or {}
    tpl = spec.get('template') or (spec.get('jobTemplate') or {}).get('spec', {}).get('template')
    if not tpl:
        continue
    pod = tpl.get('spec', {})
    for c in pod.get('containers', []) + pod.get('initContainers', []):
        for e in c.get('env', []):
            r = (e.get('valueFrom') or {}).get('secretKeyRef')
            if r and not r.get('optional', False):
                name = r['name']
                if name in provided or name in provisioned:
                    continue
                key = (name, d['metadata']['name'])
                if key not in seen:
                    seen.add(key)
                    print(f"{name} {d['metadata']['name']}")
PY
}

self_test() {
  local rc=0 render prov
  render="$(mktemp)"; trap 'rm -f "$render"' RETURN
  # Рендер — ПРЕДПОСЫЛКА самопроверки. Прежде его отказ уезжал в пустой файл, и
  # самопроверка «проходила» по нулю документов: инъекция (A) не нашла бы своего
  # секрета и объявила бы гейт пропустившим дефект — то есть красный вердикт о
  # дереве по причине, к дереву отношения не имеющей.
  # shellcheck disable=SC2046,SC2086
  helm_try kacho-umbrella "$UMBRELLA" $(stacks_args dev-prod "$UMBRELLA")
  render_or_fatal "стек dev-prod (самопроверка)"
  printf '%s\n' "$HELM_OUT" > "$render"
  prov="$(provisioned_by dev-prod-up)"

  echo "  (0) дерево как есть                    → $( [ -z "$(unmet "$render" "$prov")" ] && echo МОЛЧИТ || { echo "красный:"; unmet "$render" "$prov"; rc=1; } )"

  # (A) ИНЪЕКЦИЯ: цель перестала звать скрипт секретов (ровно исходный дефект)
  local out; out="$(unmet "$render" "")"
  if [[ "$out" == *'kaname-hook-token'* ]]; then
    echo "  (A) цель не зовёт scripts/dev-prod-secrets.sh → КРАСНЫЙ с координатой: $(printf '%s' "$out" | tr '\n' ';')"
  else
    echo "  (A) цель не зовёт scripts/dev-prod-secrets.sh → ПРОПУСТИЛ"; rc=1
  fi

  # (B) КОНТРОЛЬ той же формы: ОБЯЗАТЕЛЬНАЯ ссылка, но секрет создаёт САМ рендер —
  #     гейт обязан смолчать, хотя никакой скрипт не позван. То есть краснота (A)
  #     вызвана невыполнимым предусловием, а не самим фактом «скрипт не позван».
  #
  #     ЗДЕСЬ БЫЛ ДРУГОЙ БЛИЗНЕЦ, И ОН ИСТЁК ВМЕСТЕ СО СВОИМ ПРЕДМЕТОМ. Прежняя
  #     редакция брала близнецом профиль `dev` — на том основании, что он
  #     обязательных ссылок не несёт вовсе. С задачи #948 несёт: величину обратного
  #     вызова провайдер берёт обязательной ссылкой уже там, потому что
  #     необязательная дала бы пустой заголовок и полосу отвергли бы на первом
  #     обращении. Близнец, привязанный к отсутствию ссылки, перестал быть законным
  #     в ту же минуту — и покраснел на исправном дереве.
  #
  #     Новый близнец не зависит ни от одного профиля и вдобавок проверяет ВТОРУЮ
  #     ветку предиката («секрет встречается среди Secret'ов самого рендера»),
  #     которую не проверял никто.
  local ownr; ownr="$(mktemp)"
  cat > "$ownr" <<'YAML'
apiVersion: v1
kind: Secret
metadata: {name: secret-the-chart-creates}
type: Opaque
data: {}
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: probe}
spec:
  template:
    spec:
      containers:
        - name: probe
          env:
            - name: X
              valueFrom: {secretKeyRef: {name: secret-the-chart-creates, key: k}}
YAML
  out="$(unmet "$ownr" "")"
  if [ -z "$out" ]; then echo "  (B) обязательная ссылка, секрет из чарта → МОЛЧИТ"
  else echo "  (B) обязательная ссылка, секрет из чарта → ЛОЖНОЕ СРАБАТЫВАНИЕ: $out"; rc=1; fi
  rm -f "$ownr"

  # (C) КОНТРОЛЬ той же формы: ссылка на тот же отсутствующий секрет, но optional —
  #     законная конструкция (dev-профиль так и делает), гейт обязан смолчать.
  local optr; optr="$(mktemp)"
  cat > "$optr" <<'YAML'
apiVersion: apps/v1
kind: Deployment
metadata: {name: probe}
spec:
  template:
    spec:
      containers:
        - name: probe
          env:
            - name: X
              valueFrom: {secretKeyRef: {name: secret-which-nobody-creates, key: k, optional: true}}
YAML
  out="$(unmet "$optr" "")"
  if [ -z "$out" ]; then echo "  (C) та же ссылка, но optional: true     → МОЛЧИТ"
  else echo "  (C) та же ссылка, но optional: true     → ЛОЖНОЕ СРАБАТЫВАНИЕ: $out"; rc=1; fi

  # (D) ИНЪЕКЦИЯ: та же ссылка БЕЗ optional и без создателя — обязан краснеть
  sed 's/, optional: true//' "$optr" > "$optr.hard"
  out="$(unmet "$optr.hard" "")"
  if [[ "$out" == *'secret-which-nobody-creates'* ]]; then
    echo "  (D) та же ссылка обязательной формой    → КРАСНЫЙ с координатой"
  else echo "  (D) та же ссылка обязательной формой    → ПРОПУСТИЛ"; rc=1; fi

  rm -f "$optr" "$optr.hard"
  echo "самопроверка: $( [ $rc -eq 0 ] && echo ПРОЙДЕНА || echo ПРОВАЛЕНА )"
  return $rc
}

[ "${1:-}" = "--self-test" ] && { self_test; exit $?; }

command -v python3 >/dev/null || fatal "нужен python3 (разбор рендера) — судить нечем"
python3 -c 'import yaml' 2>/dev/null || fatal "нужен PyYAML (python3 -c 'import yaml') — судить нечем"

for entry in "${PROFILES[@]}"; do
  target="${entry%%|*}"; flags="${entry#*|}"
  render="$(mktemp)"
  # Отказ рендера — УСЛОВИЕ прогона (несобранные зависимости умбреллы, нет helm),
  # а не невыполнимое предусловие стенда, о котором эта проверка и написана.
  # shellcheck disable=SC2086  # flags — намеренно раскрываемый набор -f
  helm_try kacho-umbrella "$UMBRELLA" $flags
  render_or_fatal "профиль цели $target"
  printf '%s\n' "$HELM_OUT" > "$render"
  prov="$(provisioned_by "$target")"
  out="$(unmet "$render" "$prov")"
  if [ -n "$out" ]; then
    rm -f "$render"
    printf '%s\n' "$out" | while read -r s c; do
      echo "        секрет '$s' обязателен для '$c', но его не создаёт ни чарт, ни цель" >&2
    done
    echo "      Под не стартует (CreateContainerConfigError), а \`helm --wait\` выстоит" >&2
    echo "      таймаут и упадёт «не дождался», не назвав причины." >&2
    echo "      Либо создай секрет в чарте, либо заведи его в $SECRETS_SH и позови" >&2
    echo "      этот скрипт из цели $target. Комментарий в values предусловием не является." >&2
    fail "цель $target разворачивает профиль с НЕВЫПОЛНИМЫМ предусловием (перечень выше)"
  fi
  ok
  rm -f "$render"
done

outcome_verdict "целей развёртки осмотрено: $N"
