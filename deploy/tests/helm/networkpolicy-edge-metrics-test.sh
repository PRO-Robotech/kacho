#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# networkpolicy-edge-metrics-test.sh — порт сбора величин КРАЯ открыт ровно
# пространству имён мониторинга, а не «любому пространству» (kacho#3111).
#
# ЧТО ЛОВИТ. Политика входа к поду края (templates/networkpolicy-api-gateway.yaml)
# держала правило порта сбора величин без `from` — то есть впускала на него
# любой под любого пространства кластера. Политики складываются: более узкая
# политика рядом (изоляция стенда в своём пространстве, kacho#3102) широкое
# правило не отменяет, и посторонний под доходил до порта метрик края.
#
# ЧТО УТВЕРЖДАЕТСЯ (каждая цепочка deploy/stacks.txt, где политика края
# рендерится и сбор включён):
#   1. у правила, открывающего порт сбора величин, `from` есть и в нём РОВНО
#      один отправитель;
#   2. отправитель — `namespaceSelector` по метке `kubernetes.io/metadata.name`
#      с непустым значением и ничем больше в matchLabels: эту метку ставит сервер
#      API по имени пространства, своему пространству арендатор её не навесит;
#      пустой селектор (`{}`) — «все пространства», тот же дефект;
#   3. `ipBlock` в отправителе нет (адрес — не пространство).
# И отказ рендера: при включённом сборе и НЕзаданном пространстве мониторинга
# рендер отказывает, называя ключ, — пусто значит «не сужаем», а не «никому».
#
# ЗНАМЕНАТЕЛЬ — НА ЦЕПОЧКУ. Цепочка, где политика края рендерится, а правила
# порта сбора в ней ноль, — находка «осматривать нечего» (пропала аннотация
# prometheus.io/port, порт стал именем). Законный ноль — только у цепочки, где
# под края объявляет причину выключенного сбора
# (kacho.cloud/metrics-scrape-disabled-because), либо политика края не
# рендерится вовсе (профиль dev).
#
# ЧЕГО НЕ УТВЕРЖДАЕТ: что CNI кластера исполняет политику. Это утверждает живая
# пара проб «из своего пространства — проходит / из постороннего — отказ» в
# своём пространстве прогона (deploy/scripts/stand-ns-isolation-probe.sh).
#
# Три исхода — outcome.sh: 0 зелено · 1 находка о дереве · 2 условие не создано.
# Самопроверка: --self-test (синтетика кормит ТУ ЖЕ функцию check, плюс
# инъекция незаданного пространства в настоящий рендер).
set -uo pipefail
. "$(dirname "$0")/stacks.sh"

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
KNOB="api-gateway.networkPolicy.metricsScrapeFrom.namespace"

# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
require_helm
require_python_yaml

# check <render-файл> — печатает находки в stdout (пусто = чисто), перепись в stderr.
check() {
  python3 - "$1" <<'PY'
import sys, yaml

docs = [d for d in yaml.safe_load_all(open(sys.argv[1])) if d]

# Порт сбора величин края — из объявления сбора НА ПОДЕ края (аннотация
# prometheus.io/port), а не номером в этом файле: номер не объявление.
scrape_ports = set()
scrape_off = False
for d in docs:
    if d.get("kind") != "Deployment" or d["metadata"]["name"] != "api-gateway":
        continue
    ann = d["spec"]["template"]["metadata"].get("annotations") or {}
    if ann.get("prometheus.io/scrape") == "true" and ann.get("prometheus.io/port"):
        scrape_ports.add(str(ann["prometheus.io/port"]))
    if str(ann.get("kacho.cloud/metrics-scrape-disabled-because") or "").strip():
        scrape_off = True

policies = rules = 0
for d in docs:
    if d.get("kind") != "NetworkPolicy":
        continue
    sel = ((d["spec"].get("podSelector") or {}).get("matchLabels") or {})
    if sel.get("app.kubernetes.io/name") != "api-gateway" and sel.get("app") != "api-gateway":
        continue
    policies += 1
    name = d["metadata"]["name"]
    for r in d["spec"].get("ingress") or []:
        ports = {str(p.get("port")) for p in (r.get("ports") or [])}
        if not (ports & scrape_ports):
            continue
        rules += 1
        frm = r.get("from")
        if not frm:
            print(f"{name}: правило порта сбора {sorted(ports)} без `from` — открыт всем пространствам кластера")
            continue
        if len(frm) != 1:
            print(f"{name}: у правила порта сбора отправителей {len(frm)} — элементы `from` складываются ИЛИ, ждали ровно один")
            continue
        peer = frm[0]
        if "ipBlock" in peer:
            print(f"{name}: отправитель порта сбора — ipBlock {peer['ipBlock']}, а не пространство мониторинга")
            continue
        ns = peer.get("namespaceSelector")
        if ns is None:
            print(f"{name}: отправитель порта сбора без namespaceSelector — под СВОЕГО пространства, а не мониторинг")
            continue
        ml = ns.get("matchLabels") or {}
        if ns.get("matchExpressions") or set(ml) != {"kubernetes.io/metadata.name"} or not str(ml.get("kubernetes.io/metadata.name") or "").strip():
            print(f"{name}: namespaceSelector порта сбора {ns} — ждали ровно kubernetes.io/metadata.name=<пространство мониторинга>; "
                  "пустой селектор означает «все пространства», произвольную метку арендатор навесит своему")
# Знаменатель цепочки: политика края есть, сбор не выключен с причиной, а
# правил порта сбора ноль — судить нечего, и «чисто» здесь было бы ложью.
if policies and not rules and not scrape_off:
    print(f"политик края {policies}, а правил порта сбора 0 (портов сбора на поде края {len(scrape_ports)}) — "
          "осматривать нечего: порт сбора не объявлен аннотацией prometheus.io/port либо правило его не называет")
print(f"SCOPE портов сбора края {len(scrape_ports)}, политик края {policies}, правил порта сбора {rules}", file=sys.stderr)
PY
}

RENDER_FILE=""
render() {
  local what="$1"; shift
  local out; out="$(mktemp)"
  helm_try kacho-umbrella "$UMBRELLA" "$@"
  render_or_fatal "$what"
  printf '%s\n' "$HELM_OUT" > "$out"
  RENDER_FILE="$out"
}

synth() { # <from-блок YAML с отступом 8, либо пусто>
  cat <<EOF
apiVersion: apps/v1
kind: Deployment
metadata: {name: api-gateway}
spec:
  template:
    metadata:
      annotations: {prometheus.io/scrape: "true", prometheus.io/port: "9095"}
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: {name: api-gateway}
spec:
  podSelector: {matchLabels: {app.kubernetes.io/name: api-gateway}}
  policyTypes: [Ingress]
  ingress:
    - ports: [{protocol: TCP, port: 9095}]
$1
EOF
}

if [ "${1:-}" = "--self-test" ]; then
  rc=0
  axis() { # имя · RED|GREEN · документ
    local f out got
    f="$(mktemp)"; printf '%s\n' "$3" > "$f"
    out="$(check "$f" 2>/dev/null)"; rm -f "$f"
    if [ -n "$out" ]; then got=RED; else got=GREEN; fi
    if [ "$got" = "$2" ]; then printf '  ✓ %-60s → %s\n' "$1" "$2"
    else printf '  ✗ %-60s → ждали %s, получили %s\n' "$1" "$2" "$got"; [ -n "$out" ] && printf '      %s\n' "$out"; rc=1; fi
  }
  axis "правило без from" RED "$(synth "")"
  axis "близнец: from — пространство мониторинга по имени" GREEN \
    "$(synth '      from: [{namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: monitoring}}}]')"
  axis "пустой namespaceSelector ({} = все пространства)" RED \
    "$(synth '      from: [{namespaceSelector: {}}]')"
  axis "произвольная метка пространства вместо имени" RED \
    "$(synth '      from: [{namespaceSelector: {matchLabels: {team: monitoring}}}]')"
  axis "два элемента from (ИЛИ): пространство и поды" RED \
    "$(synth '      from: [{namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: monitoring}}}, {podSelector: {}}]')"
  axis "ipBlock вместо пространства" RED \
    "$(synth '      from: [{ipBlock: {cidr: 0.0.0.0/0}}]')"
  axis "под своего пространства вместо мониторинга" RED \
    "$(synth '      from: [{podSelector: {}}]')"
  # Знаменатель цепочки: политика края есть, порт сбора на поде объявлен, а
  # правило называет другой порт — правил порта сбора 0 в этой цепочке.
  axis "знаменатель: политика края без правила порта сбора" RED \
    "$(synth '      from: [{namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: monitoring}}}]' | sed 's/port: 9095}/port: 9096}/')"
  axis "близнец знаменателя: сбор выключен с причиной — законный ноль" GREEN \
    "$(synth '      from: [{namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: monitoring}}}]' | sed -e 's/port: 9095}/port: 9096}/' \
       -e 's|annotations: {prometheus.io/scrape: "true", prometheus.io/port: "9095"}|annotations: {kacho.cloud/metrics-scrape-disabled-because: "проба"}|')"
  # Инъекция в настоящий рендер: незаданное пространство мониторинга — отказ
  # рендера с именем ключа; близнец — то же с заданным — рендер проходит.
  stack_args="$(stacks_args prod "$UMBRELLA")"
  # shellcheck disable=SC2086
  helm_try kacho-umbrella "$UMBRELLA" $stack_args --set "$KNOB="
  if [ "$HELM_RC" -ne 0 ] && [[ "$HELM_ERR" == *"$KNOB"* ]]; then
    echo "  ✓ инъекция: пространство мониторинга не задано → рендер отказал, назвав ключ"
  else
    echo "  ✗ инъекция: пространство мониторинга не задано → рендер код $HELM_RC, ключ не назван"; rc=1
  fi
  # shellcheck disable=SC2086
  helm_try kacho-umbrella "$UMBRELLA" $stack_args --set "$KNOB=monitoring"
  if [ "$HELM_RC" -eq 0 ]; then echo "  ✓ близнец: пространство задано → рендер проходит"
  else helm_said "близнец инъекции"; echo "  ✗ близнец: пространство задано → рендер отказал"; rc=1; fi
  exit "$rc"
fi

judged=0
while IFS= read -r stack; do
  # shellcheck disable=SC2046
  render "цепочка $stack" $(stacks_args "$stack" "$UMBRELLA")
  out="$(check "$RENDER_FILE" 2>"$RENDER_FILE.scope")"
  scope="$(cat "$RENDER_FILE.scope")"; rm -f "$RENDER_FILE" "$RENDER_FILE.scope"
  echo "  цепочка $stack: $scope"
  [[ "$scope" =~ правил\ порта\ сбора\ ([0-9]+) ]] && judged=$((judged + BASH_REMATCH[1]))
  if [ -n "$out" ]; then
    while IFS= read -r l; do violation "цепочка $stack: $l"; done <<<"$out"
  fi
  ok
done < <(stacks_names)
[ "$judged" -gt 0 ] || fail "ни в одной цепочке нет правила порта сбора края — «находок ноль» значило бы «осматривать нечего»"
findings_verdict "правил порта сбора осмотрено $judged"
