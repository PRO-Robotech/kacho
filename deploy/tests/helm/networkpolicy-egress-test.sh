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
policies = judged = 0
for d in docs:
    if d.get('kind') != 'NetworkPolicy':
        continue
    if 'Egress' not in (d['spec'].get('policyTypes') or []):
        continue
    policies += 1
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
        judged += 1

# ОБЪЁМ ОСМОТРЕННОГО — ОТДЕЛЬНОЙ СТРОКОЙ, И ОН НЕ НАХОДКА.
#
# Все три утверждения выше ОТРИЦАТЕЛЬНЫЕ: каждое срабатывает на паре
# «Egress-политика × выбранный ею под». Пар ноль — и все три молчат, будучи
# совершенно исправными; «находок нет» становится неотличимо от «осматривать
# было нечего». Наблюдалось ровно это: единственная Egress-политика дерева
# снята вместе со своим предметом (#2141), и гейт продолжил печатать зелёное,
# не осмотрев ни одной пары.
#
# Строка идёт в stderr: находки вызывающий читает из stdout, и перепись,
# попав туда, была бы принята за нарушение.
print(f"SCOPE политик Egress {policies}, пар «политика × под» {judged}", file=sys.stderr)
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
  # ── ВХОД САМОПРОВЕРКИ — СИНТЕТИЧЕСКИЙ, А НЕ ИНЪЕКЦИЯ В ШАБЛОН ──────────────
  #
  # Здесь инъекция шла ПРАВКОЙ живых шаблонов: она снимала условие у метки
  # сайдкара и вырезала правило :5432 из `templates/networkpolicy-authz.yaml`.
  # Оба шаблона сняты вместе со своим потребителем (#2141) — наложение правил
  # пережило его и было убрано из поставки, — и самопроверка разом потеряла
  # предмет: две её оси печатали «ПРОПУСТИЛ», а `cp` отказывал на несуществующем
  # файле.
  #
  # Восстановлена она НЕ возвратом шаблонов, а сменой входа: предмет `check` —
  # не конкретная политика дерева, а СВОЙСТВО любой Egress-политики («сужать, а
  # не отрезать»). Свойство переживает снятие первой своей политики, а инъекция
  # в живой шаблон — нет: она доказывала способность падать ровно до тех пор,
  # пока в дереве стоял тот самый шаблон.
  #
  # Синтетика кормит ТУ ЖЕ функцию `check`, которой судятся настоящие рендеры,
  # поэтому доказанное здесь верно для дерева. Каждая ось меняет РОВНО ОДИН факт
  # против своего близнеца.

  # synth <контейнеры> <порты> [метка] — документ из пары «под × Egress-политика».
  # Политика ВСЕГДА выбирает этот под: предмет проверки — что она ему разрешает,
  # а не кого выбирает.
  synth() {
    local ctrs="$1" ports="$2" label="${3:-}"
    {
      echo "apiVersion: apps/v1"
      echo "kind: Deployment"
      echo "metadata:"
      echo "  name: synth-workload"
      echo "spec:"
      echo "  template:"
      echo "    metadata:"
      echo "      labels:"
      echo "        app: synth"
      [ -n "$label" ] && echo "        $label"
      echo "    spec:"
      echo "      containers:"
      local c
      for c in $ctrs; do echo "        - name: $c"; done
      echo "---"
      echo "apiVersion: networking.k8s.io/v1"
      echo "kind: NetworkPolicy"
      echo "metadata:"
      echo "  name: synth-egress"
      echo "spec:"
      echo "  podSelector:"
      echo "    matchLabels:"
      echo "      app: synth"
      echo "  policyTypes:"
      echo "    - Egress"
      echo "  egress:"
      local prt
      for prt in $ports; do
        echo "    - to: []"
        echo "      ports:"
        echo "        - protocol: TCP"
        echo "          port: $prt"
      done
    }
  }

  axis() { # имя · RED|GREEN · документ
    local name="$1" want="$2" doc="$3" f out got
    f="$(mktemp)"; printf '%s\n' "$doc" > "$f"
    out="$(check "$f")"; rm -f "$f"
    if [ -n "$out" ]; then got=RED; else got=GREEN; fi
    if [ "$got" = "$want" ]; then
      printf '  ✓ %-52s → ждали %s\n' "$name" "$want"
    else
      printf '  ✗ %-52s → ждали %s, получили %s\n' "$name" "$want" "$got"
      [ -n "$out" ] && printf '      %s\n' "$out"
      rc=1
    fi
  }

  # (0) ДЕРЕВО КАК ЕСТЬ — положительный контроль на настоящем рендере.
  render "контроль 0 (боевой профиль как есть)" -f "$UMBRELLA/values.prod.yaml"
  r="$RENDER_FILE"
  out="$(check "$r")"
  [ -z "$out" ] && echo "  ✓ (0) боевой профиль как есть                         → МОЛЧИТ" \
                || { echo "  ✗ (0) боевой профиль как есть → ЛОЖНОЕ СРАБАТЫВАНИЕ: $out"; rc=1; }
  rm -f "$r"

  # (A) метка объявляет сайдкар, которого в поде НЕТ.
  axis "метка сайдкара при отсутствующем контейнере" RED \
    "$(synth "kaname" "53 5432" 'kacho.cloud/opa-sidecar: "true"')"
  axis "близнец: та же метка, контейнер НА МЕСТЕ" GREEN \
    "$(synth "kaname opa" "53 5432" 'kacho.cloud/opa-sidecar: "true"')"

  # (A2) под отрезан от собственной базы.
  axis "выбран Egress-политикой, :5432 НЕ разрешён" RED "$(synth "kaname" "53")"
  axis "близнец: :5432 разрешён" GREEN "$(synth "kaname" "53 5432")"

  # (A3) под отрезан от разрешения имён.
  axis "выбран Egress-политикой, DNS :53 НЕ разрешён" RED "$(synth "kaname" "5432")"
  axis "близнец: DNS разрешён" GREEN "$(synth "kaname" "53 5432")"

  # (C) АНТИМАСКА: пар нет вовсе — молчание ЗАКОННО, но обязано быть ВИДНО.
  #
  # Ось спрашивает не «чисто ли», а «различимо ли пустое от чистого»: без строки
  # переписи оба состояния печатают одно и то же, и гейт, потерявший предмет,
  # выглядит исправным. Ровно это и случилось после снятия наложения правил.
  scope="$(check <(printf 'apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: nothing\n') 2>&1 >/dev/null)"
  # Сравнение БЕЗ внешнего процесса: под `pipefail` вердикт из трубы с `grep -q`
  # лжёт НА СОВПАДЕНИИ — `grep` выходит до конца входа, писатель слева получает
  # SIGPIPE, и статус конвейера объявляет найденное ненайденным (#658). Здесь это
  # особенно коварно: ось — АНТИМАСКА, её ложное красное читалось бы как дефект
  # переписи. Поймано гейтом дерева, а не глазом.
  if [[ $scope == *"SCOPE политик Egress 0, пар «политика × под» 0"* ]]; then
    echo "  ✓ (C) Egress-политик нет                              → МОЛЧИТ, и перепись это НАЗЫВАЕТ"
  else
    echo "  ✗ (C) Egress-политик нет: перепись не назвала пустой обход: $scope"; rc=1
  fi

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
TOT_POL=0
TOT_PAIRS=0
for stack in $STACKS; do
  prof="$(stacks_chain "$stack" ' ')"
  # shellcheck disable=SC2046,SC2086
  render "стек $stack" $(stacks_args "$stack" "$UMBRELLA")
  r="$RENDER_FILE"
  scope_f="$(mktemp)"
  out="$(check "$r" 2>"$scope_f")"
  # Перепись СУММИРУЕТСЯ по стекам и печатается в вердикте: три утверждения
  # `check` отрицательные, поэтому на нуле пар они молчат исправными, и «находок
  # нет» без этой величины неотличимо от «осматривать было нечего».
  p_n="$(sed -n 's/^SCOPE политик Egress \([0-9]*\).*/\1/p' "$scope_f" | head -1)"
  q_n="$(sed -n 's/^SCOPE политик Egress [0-9]*, пар «политика × под» \([0-9]*\)$/\1/p' "$scope_f" | head -1)"
  TOT_POL=$((TOT_POL + ${p_n:-0}))
  TOT_PAIRS=$((TOT_PAIRS + ${q_n:-0}))
  rm -f "$scope_f" "$r"
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

outcome_verdict "стеков осмотрено: $N; Egress-политик в рендерах $TOT_POL, пар «политика × под» $TOT_PAIRS (ноль означает, что предмета в дереве сейчас нет — не то же самое, что «нарушений нет»)"
