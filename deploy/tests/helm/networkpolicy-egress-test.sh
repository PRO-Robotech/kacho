#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# networkpolicy-egress-test.sh — политика исходящего трафика обязана СУЖАТЬ, а не
# ОТРЕЗАТЬ.
#
# ЧТО ЛОВИТ. NetworkPolicy выбирает ПОД, а не контейнер. Как только под попал под
# политику с `policyTypes: [Egress]`, его исходящий трафик становится default-deny,
# и список правил governs ОСНОВНОЙ контейнер тоже — не только тот сайдкар, ради
# которого политику писали. Поэтому «политика для сайдкара» — конструкция, которой
# не существует: пропуск в списке означает не «строже», а «сервис отрезан».
#
# Реальный случай: `opa-sidecar-egress-allowlist` выбирает поды по метке
# `kacho.cloud/opa-sidecar=true`. Метка рендерилась БЕЗУСЛОВНО на kaname, при
# том что сайдкар выключен во всех профилях, а values.prod включает эту политику.
# На CNI, который NetworkPolicy энфорсит, kaname остался бы без доступа к
# СОБСТВЕННОЙ Postgres и к Hydra — то есть весь authN/authZ-ярус лёг бы. Дефект
# латентный: единственный профиль, включающий политику, ни разу не поднимали, а
# оверлей боевого кластера её выключает.
#
# ЧТО УТВЕРЖДАЕТСЯ (для каждого профиля, где политика включена):
#   1. Каждый под, выбранный Egress-политикой, ФАКТИЧЕСКИ несёт тот сайдкар,
#      ради которого политика написана (метка не врёт о составе пода).
#   2. Каждый такой под сохраняет исходящий доступ к своей Postgres :5432 —
#      отрезать сервис от его базы политика не вправе ни при каких настройках.
#   3. DNS остаётся разрешён (без него не резолвится ни одно имя Service).
#
# ЧЕГО НЕ УТВЕРЖДАЕТ: полноту списка по всем прочим зависимостям — это неразрешимо
# из манифеста. Проверяются те, чьё отсутствие гарантированно кладёт сервис.
#
# Офлайновый харнесс над `helm template`, кластер не нужен. Зеркалит tests/helm/*.
# Самопроверка: --self-test.
set -uo pipefail
# Состав стендов — из ЕДИНСТВЕННОЙ таблицы дерева (deploy/stacks.txt).
# Своей копии цепочек здесь нет: копии разъезжались молча.
. "$(dirname "$0")/stacks.sh"

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"

# Три исхода — ОДНОЙ реализацией на весь каталог: 0 зелено · 1 находка о дереве ·
# 2 условие не создано (плюс текст самого helm). Прежде «<стек> не рендерится» и
# нехватка инструмента объявлялись находкой — тем же кодом 1, что и под, отрезанный
# политикой от своей базы, — и вызывающий не мог их различить машинно (#1214).
# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
require_helm

command -v python3 >/dev/null || fatal "нужен python3 — разбирать рендер нечем"
python3 -c 'import yaml' 2>/dev/null || fatal "нужен PyYAML — разбирать рендер нечем"

# check <render-файл> → печатает нарушения (пусто = чисто)
check() {
  python3 - "$1" <<'PY'
import sys, yaml

docs = [d for d in yaml.safe_load_all(open(sys.argv[1])) if d]

def matches(labels, sel):
    for k, v in (sel.get('matchLabels') or {}).items():
        if labels.get(k) != v:
            return False
    for e in (sel.get('matchExpressions') or []):
        val = labels.get(e['key'])
        if e['operator'] == 'In' and val not in e['values']:
            return False
        if e['operator'] == 'NotIn' and val in e['values']:
            return False
    return True

# СПИСОК, а не словарь по имени: одно имя может встретиться в рендере дважды
# (разные kind, дубль из подчарта), и словарь молча оставил бы последний —
# проверка тогда осматривает не тот под и МОЛЧИТ. Проверяем каждый.
pods = []
for d in docs:
    if d.get('kind') not in ('Deployment', 'StatefulSet'):
        continue
    tpl = d['spec']['template']
    pods.append((
        d['metadata']['name'],
        tpl['metadata'].get('labels', {}),
        [c['name'] for c in tpl['spec'].get('containers', [])],
    ))

# на что политика разрешает выходить: множество (порт) и наличие DNS
for d in docs:
    if d.get('kind') != 'NetworkPolicy':
        continue
    if 'Egress' not in (d['spec'].get('policyTypes') or []):
        continue
    np = d['metadata']['name']
    sel = d['spec'].get('podSelector') or {}
    rules = d['spec'].get('egress') or []
    ports = set()
    for r in rules:
        for p in (r.get('ports') or []):
            ports.add(int(p['port']))

    for name, lbl, ctrs in pods:
        if not matches(lbl, sel):
            continue
        # 1. метка про сайдкар обязана соответствовать составу пода
        if lbl.get('kacho.cloud/opa-sidecar') == 'true' and not any('opa' in c for c in ctrs):
            print(f"{np}: выбирает под '{name}' по метке opa-sidecar=true, "
                  f"но контейнера OPA в нём нет (контейнеры: {', '.join(ctrs)})")
        # 2. доступ к своей Postgres
        if 5432 not in ports:
            print(f"{np}: под '{name}' выбран Egress-политикой, но :5432 не разрешён — "
                  f"сервис отрезан от собственной базы")
        # 3. DNS
        if 53 not in ports:
            print(f"{np}: под '{name}' выбран Egress-политикой, но DNS (:53) не разрешён")
PY
}

# render <метка> <values-флаги…> → путь к файлу с манифестом.
# Отказ рендера — УСЛОВИЕ прогона, а не свойство политики: код 2 и текст helm.
# Вызов идёт ВНЕ подстановки — иначе `render_or_fatal` вышел бы из ПОДОБОЛОЧКИ,
# и её код с текстом до вызывающего не доехали бы (см. шапку outcome.sh).
RENDER_FILE=""
render() {
  local what="$1"; shift
  local out; out="$(mktemp)"
  # shellcheck disable=SC2086
  helm_try kacho-umbrella "$UMBRELLA" "$@"
  render_or_fatal "$what"
  printf '%s\n' "$HELM_OUT" > "$out"
  RENDER_FILE="$out"
}

if [ "${1:-}" = "--self-test" ]; then
  rc=0
  # ── ИНЪЕКЦИЯ ИДЁТ В КОПИЮ ДЕРЕВА, А НЕ В ЖИВУЮ РАБОЧУЮ КОПИЮ (#696) ──────────
  #
  # Обе инъекции ниже правят чарты на месте. Прежняя редакция правила ЖИВЫЕ,
  # отслеживаемые файлы и возвращала их ловушкой на EXIT/INT/TERM. Ловушка
  # закрывает три пути мимо возврата — сигнал, срок ожидания, собственный `fail`
  # внутри окна, — но НЕ закрывает четвёртый: снятие, которое не перехватывается
  # (`SIGKILL`, нехватка памяти, нехватка места). Проверено прерыванием: после
  # него в дереве оставались `M …/kaname/templates/deployment.yaml` и
  # `M …/templates/networkpolicy-authz.yaml`.
  #
  # Хуже всего то, что остаётся ровно тот дефект, который ЭТА проверка и ловит:
  # следующий прогон краснеет по-настоящему, на закладке, которую сам гейт и
  # оставил.
  #
  # Копия закрывает класс по построению: живого дерева самопроверка не касается
  # вовсе, поэтому её прерывание на ЛЮБОМ шаге не оставляет следов. Ловушка
  # возврата ниже остаётся — но её предмет теперь ПОРЯДОК СЛУЧАЕВ (случай
  # A-контроль обязан видеть неинъектированный сабчарт), а не сохранность дерева.
  WORK="$(mktemp -d)"
  trap 'rm -rf "$WORK"' EXIT
  cp -r "$REPO_ROOT/helm" "$WORK/helm" || fail "копия чартов не собрана — инъекциям некуда идти"
  UMBRELLA="$WORK/helm/umbrella"
  [ -d "$UMBRELLA" ] || fail "в копии нет умбреллы ($UMBRELLA)"

  SELFTEST_RESTORE=()
  restore_injected() {
    local i
    for ((i = ${#SELFTEST_RESTORE[@]} - 2; i >= 0; i -= 2)); do
      [ -f "${SELFTEST_RESTORE[i]}" ] && cp "${SELFTEST_RESTORE[i]}" "${SELFTEST_RESTORE[i+1]}"
      rm -f "${SELFTEST_RESTORE[i]}"
    done
    SELFTEST_RESTORE=()
  }
  trap 'restore_injected; rm -rf "$WORK"' EXIT
  trap 'restore_injected; rm -rf "$WORK"; exit 130' INT TERM
  # inject_backup <файл> — снять копию ВНЕ templates/ (helm рендерит всё,
  # что там лежит), возврат назначается тем же действием, что и копирование.
  # Файл здесь всегда лежит в КОПИИ дерева ($WORK), живого не касаемся.
  inject_backup() {
    local b; b="$(mktemp)"; cp "$1" "$b"
    SELFTEST_RESTORE+=("$b" "$1")
    printf '%s' "$b"
  }
  # (0) профиль, включающий политику, — как есть
  render "профиль values.prod (самопроверка, случай 0)" -f "$UMBRELLA/values.prod.yaml"
  r="$RENDER_FILE"
  out="$(check "$r")"
  [ -z "$out" ] && echo "  (0) values.prod как есть                       → МОЛЧИТ" \
                || { echo "  (0) values.prod как есть                       → красный: $out"; rc=1; }
  rm -f "$r"

  # (A) ИНЪЕКЦИЯ: вернуть безусловную метку сайдкара (исходный дефект).
  # Рендерим сабчарт kaname СТАНДАЛОН из каталога-исходника: умбрелла собирает
  # его в `charts/*.tgz`, и правка каталога без `helm dep update` в её рендер не
  # попадает. Проверяемое свойство — «метка соответствует составу пода» —
  # принадлежит именно сабчарту, поэтому и проверяется на нём.
  IAM="$UMBRELLA/charts/kaname"
  dep="$IAM/templates/deployment.yaml"
  # Копия — через inject_backup: она ложится ВНЕ templates/ (helm рендерит всё,
  # что там лежит, и файл-бэкап превращался во второй под с тем же именем —
  # поймано на себе, самопроверка тогда «проходила», осматривая не тот под) И
  # сразу назначает возврат ловушке.
  inject_backup "$dep" >/dev/null
  python3 - "$dep" <<'PY'
import re, sys
p = sys.argv[1]; s = open(p).read()
s = s.replace('{{- if .Values.opaSidecar.enabled }}\n        # KAC-127', '        # KAC-127', 1)
s = re.sub(r'(kacho\.cloud/opa-sidecar: "true"\n)        \{\{- end \}\}\n', r'\1', s, count=1)
open(p, 'w').write(s)
PY
  # Политика живёт в умбрелле, а под — в сабчарте, поэтому склеиваем оба рендера:
  # проверяемое утверждение связывает именно их.
  pol="$(mktemp)"
  helm template kacho-umbrella "$UMBRELLA" -f "$UMBRELLA/values.prod.yaml" \
    --show-only templates/networkpolicy-authz.yaml 2>/dev/null > "$pol"
  ri="$(mktemp)"
  { helm template kaname "$IAM" 2>/dev/null; echo '---'; cat "$pol"; } > "$ri"
  out="$(check "$ri")"
  if [[ "$out" == *"контейнера OPA в нём нет"* ]]; then
    echo "  (A) метка сайдкара безусловна                  → КРАСНЫЙ с координатой"
  else echo "  (A) метка сайдкара безусловна                  → ПРОПУСТИЛ"; rc=1; fi
  restore_injected

  # (A-контроль) тот же сабчарт БЕЗ инъекции: метка отсутствует ⇒ политика его не
  # выбирает ⇒ гейт молчит. Без этой половины (A) доказывала бы лишь то, что гейт
  # умеет краснеть.
  { helm template kaname "$IAM" 2>/dev/null; echo '---'; cat "$pol"; } > "$ri"
  out="$(check "$ri")"
  [ -z "$out" ] && echo "  (A-контроль) сабчарт без инъекции             → МОЛЧИТ" \
                || { echo "  (A-контроль) сабчарт без инъекции             → ЛОЖНОЕ СРАБАТЫВАНИЕ: $out"; rc=1; }
  rm -f "$ri" "$pol"

  # (A2) ИНЪЕКЦИЯ: снять правило на :5432 при включённом сайдкаре
  npf="$UMBRELLA/templates/networkpolicy-authz.yaml"
  inject_backup "$npf" >/dev/null   # тоже ВНЕ templates/ + возврат под ловушкой
  python3 - "$npf" <<'PY'
import re, sys
p = sys.argv[1]; s = open(p).read()
s = re.sub(r'    - to:\n        - podSelector:\n            \{\{- toYaml \.Values\.opaSidecar\.networkPolicy\.datastorePodSelector[^\n]*\n      ports:\n        - protocol: TCP\n          port: 5432\n', '', s, count=1)
open(p, 'w').write(s)
PY
  render "инъекция A2 (боевой профиль, сайдкар включён)" \
    -f "$UMBRELLA/values.prod.yaml" --set kaname.opaSidecar.enabled=true
  r="$RENDER_FILE"
  out="$(check "$r")"
  if [[ "$out" == *"отрезан от собственной базы"* ]]; then
    echo "  (A2) снято правило :5432 (сайдкар включён)     → КРАСНЫЙ с координатой"
  else echo "  (A2) снято правило :5432 (сайдкар включён)     → ПРОПУСТИЛ"; rc=1; fi
  rm -f "$r"; restore_injected

  # (B) КОНТРОЛЬ той же формы: сайдкар ВКЛЮЧЁН, список полон — законная
  #     конструкция, ради которой политика и написана. Гейт обязан смолчать.
  render "контроль B (боевой профиль, сайдкар включён)" \
    -f "$UMBRELLA/values.prod.yaml" --set kaname.opaSidecar.enabled=true
  r="$RENDER_FILE"
  out="$(check "$r")"
  [ -z "$out" ] && echo "  (B) сайдкар включён, список полон             → МОЛЧИТ" \
                || { echo "  (B) сайдкар включён, список полон             → ЛОЖНОЕ СРАБАТЫВАНИЕ: $out"; rc=1; }
  rm -f "$r"

  # (C) КОНТРОЛЬ: профиль, где политика выключена вовсе — молчит
  render "контроль C (профиль dev)" -f "$UMBRELLA/values.dev.yaml"
  r="$RENDER_FILE"
  out="$(check "$r")"
  [ -z "$out" ] && echo "  (C) политика выключена (dev)                  → МОЛЧИТ" \
                || { echo "  (C) политика выключена (dev)                  → ЛОЖНОЕ СРАБАТЫВАНИЕ: $out"; rc=1; }
  rm -f "$r"

  echo "самопроверка: $( [ $rc -eq 0 ] && echo ПРОЙДЕНА || echo ПРОВАЛЕНА )"
  exit $rc
fi

# Профили проверяем ВСЕ разворачиваемые: политика включается ровно в одном из них,
# и именно поэтому дефект прожил незамеченным. Состав каждого стека — из
# единственной таблицы дерева: здесь стояли два имени файлов, и стенд, чья
# цепочка длиннее одного слоя, рендерился бы не тем составом.
STACKS="$(stacks_names)"
EXPECTED_ASSERTIONS="$(printf '%s\n' "$STACKS" | grep -c . || true)"
[ "$EXPECTED_ASSERTIONS" -ge 1 ] || fatal "таблица стеков не дала ни одного имени — обходить нечего"
for stack in $STACKS; do
  prof="$(stacks_chain "$stack" ' ')"
  # shellcheck disable=SC2046,SC2086
  render "стек $stack" $(stacks_args "$stack" "$UMBRELLA")
  r="$RENDER_FILE"
  out="$(check "$r")"
  rm -f "$r"
  if [ -n "$out" ]; then
    {
      printf '        %s\n' "$out"
      echo "      NetworkPolicy действует на ПОД, а не на контейнер: выбранный под"
      echo "      получает default-deny целиком, вместе с основным контейнером."
    } >&2
    fail "$prof — Egress-политика отрезает то, без чего под не работает (перечень выше)"
  fi
  ok
done

outcome_verdict "стеков осмотрено: $N"
