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
# `kacho.cloud/opa-sidecar=true`. Метка рендерилась БЕЗУСЛОВНО на kacho-iam, при
# том что сайдкар выключен во всех профилях, а values.prod включает эту политику.
# На CNI, который NetworkPolicy энфорсит, kacho-iam остался бы без доступа к
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

SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"

fail() { echo "FAIL: $1"; exit 1; }

command -v python3 >/dev/null || fail "нужен python3"
python3 -c 'import yaml' 2>/dev/null || fail "нужен PyYAML"

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

# render <values-флаги…>
render() {
  local out; out="$(mktemp)"
  # shellcheck disable=SC2086
  helm template kacho-umbrella "$UMBRELLA" "$@" 2>/dev/null > "$out" || { rm -f "$out"; return 1; }
  printf '%s' "$out"
}

if [ "${1:-}" = "--self-test" ]; then
  rc=0
  # (0) профиль, включающий политику, — как есть
  r="$(render -f "$UMBRELLA/values.prod.yaml")" || fail "values.prod не рендерится"
  out="$(check "$r")"
  [ -z "$out" ] && echo "  (0) values.prod как есть                       → МОЛЧИТ" \
                || { echo "  (0) values.prod как есть                       → красный: $out"; rc=1; }
  rm -f "$r"

  # (A) ИНЪЕКЦИЯ: вернуть безусловную метку сайдкара (исходный дефект).
  # Рендерим сабчарт kacho-iam СТАНДАЛОН из каталога-исходника: умбрелла собирает
  # его в `charts/*.tgz`, и правка каталога без `helm dep update` в её рендер не
  # попадает. Проверяемое свойство — «метка соответствует составу пода» —
  # принадлежит именно сабчарту, поэтому и проверяется на нём.
  IAM="$UMBRELLA/charts/kacho-iam"
  dep="$IAM/templates/deployment.yaml"
  # Копию храним ВНЕ templates/: helm рендерит всё, что там лежит, и файл-бэкап
  # превращается во второй под с тем же именем. Поймано на себе — самопроверка
  # тогда «проходила», осматривая не тот под.
  bak="$(mktemp)"; cp "$dep" "$bak"
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
  { helm template kacho-iam "$IAM" 2>/dev/null; echo '---'; cat "$pol"; } > "$ri"
  out="$(check "$ri")"
  if printf '%s' "$out" | grep -q "контейнера OPA в нём нет"; then
    echo "  (A) метка сайдкара безусловна                  → КРАСНЫЙ с координатой"
  else echo "  (A) метка сайдкара безусловна                  → ПРОПУСТИЛ"; rc=1; fi
  cp "$bak" "$dep"; rm -f "$bak"

  # (A-контроль) тот же сабчарт БЕЗ инъекции: метка отсутствует ⇒ политика его не
  # выбирает ⇒ гейт молчит. Без этой половины (A) доказывала бы лишь то, что гейт
  # умеет краснеть.
  { helm template kacho-iam "$IAM" 2>/dev/null; echo '---'; cat "$pol"; } > "$ri"
  out="$(check "$ri")"
  [ -z "$out" ] && echo "  (A-контроль) сабчарт без инъекции             → МОЛЧИТ" \
                || { echo "  (A-контроль) сабчарт без инъекции             → ЛОЖНОЕ СРАБАТЫВАНИЕ: $out"; rc=1; }
  rm -f "$ri" "$pol"

  # (A2) ИНЪЕКЦИЯ: снять правило на :5432 при включённом сайдкаре
  npf="$UMBRELLA/templates/networkpolicy-authz.yaml"
  nbak="$(mktemp)"; cp "$npf" "$nbak"   # тоже ВНЕ templates/ — см. выше
  python3 - "$npf" <<'PY'
import re, sys
p = sys.argv[1]; s = open(p).read()
s = re.sub(r'    - to:\n        - podSelector:\n            \{\{- toYaml \.Values\.opaSidecar\.networkPolicy\.datastorePodSelector[^\n]*\n      ports:\n        - protocol: TCP\n          port: 5432\n', '', s, count=1)
open(p, 'w').write(s)
PY
  r="$(render -f "$UMBRELLA/values.prod.yaml" --set kacho-iam.opaSidecar.enabled=true)" \
    || fail "инъекция A2 сломала рендер"
  out="$(check "$r")"
  if printf '%s' "$out" | grep -q "отрезан от собственной базы"; then
    echo "  (A2) снято правило :5432 (сайдкар включён)     → КРАСНЫЙ с координатой"
  else echo "  (A2) снято правило :5432 (сайдкар включён)     → ПРОПУСТИЛ"; rc=1; fi
  rm -f "$r"; cp "$nbak" "$npf"; rm -f "$nbak"

  # (B) КОНТРОЛЬ той же формы: сайдкар ВКЛЮЧЁН, список полон — законная
  #     конструкция, ради которой политика и написана. Гейт обязан смолчать.
  r="$(render -f "$UMBRELLA/values.prod.yaml" --set kacho-iam.opaSidecar.enabled=true)" \
    || fail "контроль B не рендерится"
  out="$(check "$r")"
  [ -z "$out" ] && echo "  (B) сайдкар включён, список полон             → МОЛЧИТ" \
                || { echo "  (B) сайдкар включён, список полон             → ЛОЖНОЕ СРАБАТЫВАНИЕ: $out"; rc=1; }
  rm -f "$r"

  # (C) КОНТРОЛЬ: профиль, где политика выключена вовсе — молчит
  r="$(render -f "$UMBRELLA/values.dev.yaml")" || fail "контроль C не рендерится"
  out="$(check "$r")"
  [ -z "$out" ] && echo "  (C) политика выключена (dev)                  → МОЛЧИТ" \
                || { echo "  (C) политика выключена (dev)                  → ЛОЖНОЕ СРАБАТЫВАНИЕ: $out"; rc=1; }
  rm -f "$r"

  echo "самопроверка: $( [ $rc -eq 0 ] && echo ПРОЙДЕНА || echo ПРОВАЛЕНА )"
  exit $rc
fi

N=0
# Профили проверяем ВСЕ разворачиваемые: политика включается ровно в одном из них,
# и именно поэтому дефект прожил незамеченным.
for prof in values.dev.yaml values.prod.yaml; do
  r="$(render -f "$UMBRELLA/$prof")" || fail "$prof не рендерится"
  out="$(check "$r")"
  rm -f "$r"
  if [ -n "$out" ]; then
    echo "FAIL: $prof — Egress-политика отрезает то, без чего под не работает:"
    printf '        %s\n' "$out"
    echo "      NetworkPolicy действует на ПОД, а не на контейнер: выбранный под"
    echo "      получает default-deny целиком, вместе с основным контейнером."
    exit 1
  fi
  N=$((N + 1))
done

echo "PASS: $SCRIPT ($N assertions)"
