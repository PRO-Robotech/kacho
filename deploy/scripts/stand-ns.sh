#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# stand-ns.sh — стенд проб в СВОЁМ пространстве имён общего кластера (kacho#3102).
#
#   stand-ns.sh up NS [STACK] [REF]   поднять стек STACK (умолчание dev-prod) от ревизии REF
#   stand-ns.sh down NS               снять пространство целиком; идемпотентно
#   stand-ns.sh run NS [--keep] [--stack S] [--ref R] -- КОМАНДА…
#                                     up → проброс → КОМАНДА (CONSOLE_BASE, EDGE_BASE и
#                                     окружение проб) → down ВСЕГДА: успех, падение, kill;
#                                     код выхода — код КОМАНДЫ; --keep оставляет стенд для
#                                     разбора на срок STAND_TTL_HOURS (≤ 12 ч)
#   stand-ns.sh census [--expired]    перечень тестовых пространств; --expired снимает просроченные
#   stand-ns.sh forward NS            проброс консоли, края и приёмника писем + файл окружения проб
#   stand-ns.sh unforward NS          снять проброс
#   stand-ns.sh self-test             проба правил имени, срока и порта (кластера не требует)
#
# Зовут его цели `make -C deploy stand-ns-up | stand-ns-down | stand-ns-run | stand-ns-census`.
#
# ── ПОРЯДОК ПОДЪЁМА И СНЯТИЕ НА НЕУСПЕХЕ ──────────────────────────────────────
#
#   1. всё, что можно проверить БЕЗ записи в кластер: имя, кластер, цепочка,
#      ревизия (правило входов сборки — tools/standns/inputs.go), образы ревизии в
#      реестре, право завести пространство, квоту и пределы, предрендер;
#   2. первая запись — ОДНИМ применением: пространство с метками и сроком, затем
#      квота и пределы (квота — первый объект пространства);
#   3. с этого момента взведена ловушка EXIT/INT/TERM: любой неуспех и любой kill
#      (кроме SIGKILL — его ловить нечем, остаток снимет `census --expired` по сроку)
#      снимает созданное этим вызовом. Пространство, существовавшее ДО вызова
#      (повторный up), ловушка не снимает — оно не этого вызова.
#
# ── ЗАЧЕМ ──────────────────────────────────────────────────────────────────────
#
# Сквозные пробы консоли и стендовые пробы поднимали локальный kind: стек `dev-up`
# ставится на кластер ЦЕЛИКОМ, в пространство `kacho`, и второй экземпляр рядом с
# рабочим стендом поставить было нечем. Решение владельца 2026-10-08: стенды проб —
# в своих пространствах имён внешнего кластера, и задача, их поднявшая, их же и
# снимает.
#
# ── ЧТО ЗНАЧИТ «ТОТ ЖЕ СТЕК» ───────────────────────────────────────────────────
#
# Цепочка — из deploy/stacks.txt (умолчание `dev-prod`, тот же состав, что
# поднимает конвейерный «свой стенд» последней фазой `make dev-up`). Сверх неё
# стенд накладывает ровно три слоя, и ни один не трогает посадку:
#
#   images.yaml   образы частей дерева — ОПУБЛИКОВАННЫЕ для ревизии REF
#                 (узлов kind нет, исполнить можно только то, что лежит в реестре);
#   stand.yaml    приспособление к кластеру, ИЗМЕРЕННОЕ у него: cert-manager не
#                 ставится (переиспользуется существующий), контроллер входа не
#                 ставится, тома — только если у кластера есть класс по умолчанию;
#                 адрес консоли этого стенда и её вход под именем, которое
#                 пробрасывает forward; почтовая полоса — в приёмник СТЕНДА
#                 (STARTTLS с якорем его листа), а не в ретранслятор площадки:
#                 письмо пробы читается из ящика стенда, и наружу оно не уходит
#                 (kacho#3065 — цепочка a8f60d объявляет ретранслятор площадки);
#   перенос       пост-обработчик рендера (tools/standns): адреса соседей,
#                 издатели и корень внутреннего CA — в пространство стенда.
#
# Общекластерного стенд не ставит и не снимает: перенос отказывает на любом
# общекластерном виде, кроме СВОИХ издателей CA (они помечены и снимаются вместе
# со стендом).
#
# ── ИМЯ, МЕТКИ, СРОК ──────────────────────────────────────────────────────────
#
#   имя        t<номер задачи>-<коротко>, например t3102-probe; не длиннее 40
#              знаков (суффикс имён корня CA — само имя, предел имени — 63)
#   метки      kacho.io/task=<номер>, kacho.io/stand=test
#   аннотации  kacho.io/expires=<RFC3339> (умолчание — через STAND_TTL_HOURS=12 ч),
#              kacho.io/ref=<коммит образов>, kacho.io/stack=<цепочка>
#
# Пространство `kacho` — рабочий стенд: имя не проходит правила, и это проверяется
# ДО обращения к кластеру.
#
# ── КЛАСТЕР НАЗЫВАЕТСЯ ЯВНО ──────────────────────────────────────────────────
#
# Все режимы, обращающиеся к кластеру (up, run, down, census, forward), говорят
# с контекстом -client явно названного файла профиля — STAND_KUBECONFIG либо
# одиночный KUBECONFIG; current-context файла не читается и не меняется
# (guard_context ниже, kacho#3065). STAND_APISERVER выводится из выбранного
# контекста; объявленный при вызове — второй пин, обязан совпасть.
# Подтверждения человеком НЕ спрашивается: пространство своё, помеченное, и
# пишется только в него и в помеченные свои объекты.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/.." && pwd)"
REPO_ROOT="$(cd "$DEPLOY_ROOT/.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"
RELEASE="${STACK_RELEASE:-kacho-umbrella}"
WORK_ROOT="${KACHO_STAND_NS_WORKDIR:-$DEPLOY_ROOT/.stand-ns}"
TTL_HOURS="${STAND_TTL_HOURS:-12}"
TTL_MAX_HOURS=12
IMAGE_PREFIX="${STAND_IMAGE_PREFIX:-docker.io/prorobotech}"
IMAGE_BRANCHES="${STAND_IMAGE_BRANCHES:-1266 main}"
UP_TIMEOUT="${STAND_TIMEOUT:-15m}"
HOST_SUFFIX="kacho.test"
LABEL_STAND="kacho.io/stand"
LABEL_TASK="kacho.io/task"
LABEL_STAND_NS="kacho.io/stand-ns"
ANN_EXPIRES="kacho.io/expires"

log()  { printf '=== stand-ns: %s\n' "$*"; }
warn() { printf 'stand-ns: %s\n' "$*" >&2; }
die()  { printf 'ABORT: stand-ns — %s\n' "$1" >&2; exit "${2:-1}"; }

# ── ПРАВИЛА БЕЗ КЛАСТЕРА ─────────────────────────────────────────────────────

# ns_valid NS — 0, если имя по правилу t<номер>-<коротко>; печатает номер задачи.
ns_valid() {
  local ns="$1"
  [ "${#ns}" -le 40 ] || return 1
  [[ "$ns" =~ ^t([0-9]+)-[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || return 1
  printf '%s' "${BASH_REMATCH[1]}"
}

# ns_port NS — первый из трёх портов проброса (консоль, край, приёмник писем).
# Выводится из имени, а не выбирается: он входит в происхождение консоли, а
# происхождение запекается в настройки службы доступа при подъёме.
#
ns_port() {
  local sum
  sum="$(printf '%s' "$1" | cksum | awk '{print $1}')"
  printf '%s' "$(( 20000 + (sum % 6000) * 3 ))"
}

ns_host() { printf '%s.%s' "$1" "$HOST_SUFFIX"; }

# ttl_valid H — срок стенда в часах: целое 1..TTL_MAX_HOURS.
ttl_valid() { [[ "$1" =~ ^[0-9]+$ ]] && [ "$1" -ge 1 ] && [ "$1" -le "$TTL_MAX_HOURS" ]; }

# expired_at RFC3339 NOW_EPOCH — 0, если срок истёк; 2, если срок не читается.
expired_at() {
  local t
  t="$(date -u -d "$1" +%s 2>/dev/null)" || return 2
  [ "$t" -le "$2" ]
}

self_test() {
  local pass=0 fail=0
  ok()  { pass=$((pass + 1)); printf '  ok   %s\n' "$1"; }
  bad() { fail=$((fail + 1)); printf '  FAIL %s\n' "$1"; }
  if [ "$(ns_valid t3102-probe)" = 3102 ]; then ok "t3102-probe → задача 3102"; else bad "t3102-probe не принят"; fi
  for n in kacho t-probe t3102 T3102-probe t3102-Probe t3102-probe- "t3102-$(printf 'x%.0s' {1..40})" kacho-t1-x; do
    if ns_valid "$n" >/dev/null; then bad "«$n» принят"; else ok "«$n» отвергнут"; fi
  done
  local p; p="$(ns_port t3102-probe)"
  if [ "$p" -ge 20000 ] && [ $((p + 2)) -le 37999 ] && [ "$p" = "$(ns_port t3102-probe)" ]; then
    ok "порт t3102-probe = $p, устойчив"; else bad "порт $p вне [20000, 37999]"; fi
  if [ "$(ns_port t3102-probe)" != "$(ns_port t896-heavy-probe)" ]; then ok "соседние имена — разные порты"; else bad "порты совпали"; fi
  local now; now="$(date -u +%s)"
  if expired_at "2000-01-01T00:00:00Z" "$now"; then ok "срок в прошлом — истёк"; else bad "прошлый срок не истёк"; fi
  if expired_at "2999-01-01T00:00:00Z" "$now"; then bad "будущий срок истёк"; else ok "срок в будущем — жив"; fi
  local rc=0; expired_at "не дата" "$now" || rc=$?
  if [ "$rc" -eq 2 ]; then ok "нечитаемый срок — код 2, не «истёк»"; else bad "нечитаемый срок прочитан (код $rc)"; fi
  if ttl_valid 12 && ttl_valid 1 && ! ttl_valid 13 && ! ttl_valid 0 && ! ttl_valid x; then
    ok "срок 1..$TTL_MAX_HOURS ч принят, 0, 13 и не число — отвергнуты"; else bad "правило срока"; fi
  # Первая запись подъёма: Pod Security и сетевая изоляция судятся по разобранному
  # манифесту. Близнец — тот же разбор на манифесте с впущенным `kacho`: отказ.
  local iso
  iso='
import sys, yaml
docs = [d for d in yaml.safe_load_all(sys.stdin) if d]
ns = next(d for d in docs if d["kind"] == "Namespace")
pol = {d["metadata"]["name"]: d["spec"] for d in docs if d["kind"] == "NetworkPolicy"}
lab = ns["metadata"]["labels"]
assert lab.get("pod-security.kubernetes.io/enforce") == "baseline", "enforce не baseline"
i, e = pol["stand-ingress"], pol["stand-egress"]
assert i["podSelector"] == {} and i["policyTypes"] == ["Ingress"], "вход не на все поды"
assert i["ingress"] == [{"from": [{"podSelector": {}}]}], "вход шире своего пространства"
assert e["podSelector"] == {} and e["policyTypes"] == ["Egress"], "выход не на все поды"
rules = e["egress"]
assert all(r.get("to") for r in rules), "правило выхода без адресата — «куда угодно»"
nsname = lambda p: p["namespaceSelector"]["matchLabels"]["kubernetes.io/metadata.name"]
names = sorted(nsname(p) for r in rules for p in r["to"] if "namespaceSelector" in p)
assert names == sorted(sys.argv[1:3]), "выход в пространства %s, ждали только %s" % (names, sys.argv[1:3])
assert all(set(p) <= {"podSelector", "namespaceSelector", "ipBlock"} for r in rules for p in r["to"])
assert [p for r in rules for p in r["to"] if "podSelector" in p and "namespaceSelector" not in p] == [{"podSelector": {}}]
ports = lambda r: sorted((q["protocol"], q["port"], q.get("endPort", q["port"])) for q in r.get("ports", []))
has53 = lambda r: not r.get("ports") or any(lo <= 53 <= hi for _, lo, hi in ports(r))
world = [r for r in rules if any("ipBlock" in p for p in r["to"])]
assert world and not any(has53(r) for r in world), "порт 53 открыт вне кластера: %s" % world
dns = [r for r in rules if any("namespaceSelector" in p and nsname(p) == sys.argv[2] for p in r["to"])]
assert len(dns) == 1 and ports(dns[0]) == [("TCP", 53, 53), ("UDP", 53, 53)], "DNS кластера — не одно правило на 53: %s" % dns
assert [nsname(p) for p in dns[0]["to"]] == [sys.argv[2]] and all(p.get("podSelector", {}).get("matchLabels") for p in dns[0]["to"]), \
    "DNS — не поды резолвера в его пространстве: %s" % dns[0]["to"]
'
  local sel='{"k8s-app":"dns-probe"}'
  if ns_objects t3102-probe 3102 2999-01-01T00:00:00Z dev-prod deadbeef cm-ns dns-ns "$sel" | python3 -c "$iso" cm-ns dns-ns; then
    ok "пространство: enforce=baseline, вход — свои поды, выход — свои, DNS кластера, cert-manager, вне кластера кроме :53"
  else bad "Pod Security или сетевая изоляция пространства не по правилу"; fi
  if ns_objects t3102-probe 3102 2999-01-01T00:00:00Z dev-prod deadbeef kacho dns-ns "$sel" | python3 -c "$iso" cm-ns dns-ns 2>/dev/null; then
    bad "выход в «kacho» принят разбором"; else ok "выход в «kacho» разбором отвергнут"; fi
  if ns_objects t3102-probe 3102 2999-01-01T00:00:00Z dev-prod deadbeef cm-ns dns-ns '{}' | python3 -c "$iso" cm-ns dns-ns 2>/dev/null; then
    bad "DNS на все поды пространства резолвера принят разбором"; else ok "DNS без меток подов резолвера разбором отвергнут"; fi
  if ns_objects t3102-probe 3102 2999-01-01T00:00:00Z dev-prod deadbeef cm-ns dns-ns "$sel" | sed 's/port: 54, endPort/port: 53, endPort/' | python3 -c "$iso" cm-ns dns-ns 2>/dev/null; then
    bad "порт 53 вне кластера принят разбором"; else ok "порт 53 вне кластера разбором отвергнут"; fi
  local pick; pick="$(STAND_DNS_NAMESPACE=dns-ns STAND_DNS_SELECTOR='{"a":"b"}' dns_peer 2>/dev/null)"
  if [ "$pick" = 'dns-ns {"a":"b"}' ] && ! STAND_DNS_NAMESPACE=dns-ns dns_peer >/dev/null 2>&1 &&
     ! STAND_DNS_NAMESPACE=dns-ns STAND_DNS_SELECTOR='{}' dns_peer >/dev/null 2>&1; then
    ok "объявленная пара DNS принята, половина пары и пустой селектор — отвергнуты"
  else bad "объявление пары DNS: «$pick»"; fi
  # Слой стенда поверх цепочки площадки (a8f60d, kacho#3065): почта стенда — в
  # его приёмник, вход консоли — под именем, которое пробрасывает forward.
  # Судится разобранным слоем, а не подстрокой.
  local ov
  ov='
import sys, yaml
d = yaml.safe_load(sys.stdin)
m = d["global"]["kacho"]["identity"]["smtp"]
assert m["connectionURI"] == "smtp://{{ .Release.Name }}-mailpit:1025/", m["connectionURI"]
assert m["credentialSecret"] == {"name": "", "key": ""}, m["credentialSecret"]
assert m["trustAnchorSecret"] == {"name": "kacho-mailpit-tls", "key": "ca.crt"}, m["trustAnchorSecret"]
assert d["uif"]["publicFront"]["service"]["name"] == "", "имя входа консоли не сброшено"
assert d["uif"]["publicFront"]["service"]["type"] == "ClusterIP"
'
  if stand_overlay t3102-probe cm-ns on 20001 | python3 -c "$ov"; then
    ok "слой стенда: почта — приёмник стенда с якорем его листа, удостоверения ретранслятора нет; вход консоли — ui-public, ClusterIP"
  else bad "слой стенда: почтовая полоса или имя входа консоли не по правилу"; fi
  # Секрет первого администратора облака выводится из рендера: ссылка
  # KANAME_BOOTSTRAP_ROOT_EMAIL → secretKeyRef. Близнец — рендер без ссылки: пусто.
  local rd; rd="$(mktemp)"
  printf '%s\n' 'kind: Deployment' 'metadata: {name: kaname}' 'spec: {template: {spec: {containers: [{name: kaname, env: [{name: KANAME_BOOTSTRAP_ROOT_EMAIL, valueFrom: {secretKeyRef: {name: stand-cloud-admin, key: email}}}]}]}}}' >"$rd"
  if [ "$(bootstrap_secret_of "$rd")" = stand-cloud-admin ]; then ok "секрет администратора облака выведен из рендера"; else bad "секрет администратора облака из рендера не выведен"; fi
  printf '%s\n' 'kind: Deployment' 'metadata: {name: kaname}' 'spec: {template: {spec: {containers: [{name: kaname, env: [{name: KANAME_BOOTSTRAP_ROOT_EMAIL, value: ""}]}]}}}' >"$rd"
  if [ -z "$(bootstrap_secret_of "$rd")" ]; then ok "рендер без ссылки на секрет — шага администратора нет"; else bad "секрет администратора выведен из рендера без ссылки"; fi
  rm -f "$rd"
  printf 'самопроверка stand-ns: прошло %d, провалено %d\n' "$pass" "$fail"
  [ "$fail" -eq 0 ]
}

# ── КЛАСТЕР ──────────────────────────────────────────────────────────────────

need_tools() {
  local t
  for t in kubectl helm python3 openssl jq curl go git; do
    # type -P, а не command -v: kubectl и helm ниже — функции-обёртки, и
    # command -v нашёл бы обёртку там, где исполняемого файла нет.
    type -P "$t" >/dev/null 2>&1 || die "нет '$t' — исполнить нечем (условие прогона, а не находка)" 2
  done
}

# ── КЛАСТЕР НАЗЫВАЕТСЯ ФАЙЛОМ ПРОФИЛЯ И ЕГО КОНТЕКСТОМ -client (kacho#3065) ───
#
# Активный контекст файла профиля — НЕ выбор стенда: файл площадки несёт два
# контекста одного кластера, `…-client` и `…-infra`, и его current-context
# переключает человек под свою работу. Стенд проб ставится ТОЛЬКО в client
# (решение владельца 2026-10-09), поэтому контекст выбирается явно:
#
#   файл профиля  STAND_KUBECONFIG, иначе KUBECONFIG — ровно один файл;
#   контекст      ровно один контекст файла с суффиксом -client; ноль или больше
#                 одного — отказ ДО первого обращения к кластеру. Переопределение
#                 — только STAND_CONTEXT, и только именем, оканчивающимся на -client.
#
# current-context файла не читается и не меняется. Каждый kubectl и helm этого
# скрипта идёт с --context/--kube-context (обёртки ниже: без выбранного
# контекста они отказывают, а не идут в активный). Дочерним шагам (make, скрипты
# посева и посадки) достаётся KUBECONFIG из двух файлов: первым — файл из одной
# строки current-context=<client> (без адресов и удостоверений, первый файл,
# объявивший current-context, при слиянии побеждает), вторым — файл профиля; и
# HELM_KUBECONTEXT. Так и шаги, читающие активный контекст, говорят с client.
#
# STAND_APISERVER выводится из выбранного контекста и уходит дочерним шагам
# (гейт посадки пинит им кластер, scripts/stand-cluster-pin.sh); объявленный
# при вызове — обязан совпасть с адресом контекста -client.
STAND_CTX="" STAND_CTX_FILE=""

kubectl() {
  [ -n "$STAND_CTX" ] || { warn "kubectl до выбора контекста — отказ (guard_context не пройден)"; return 2; }
  command kubectl --kubeconfig "$STAND_KUBECONFIG" --context "$STAND_CTX" "$@"
}
helm() {
  [ -n "$STAND_CTX" ] || { warn "helm до выбора контекста — отказ (guard_context не пройден)"; return 2; }
  command helm --kubeconfig "$STAND_KUBECONFIG" --kube-context "$STAND_CTX" "$@"
}

# Выбор файла профиля и контекста -client — ЕДИНСТВЕННЫЙ на все пути выкатки:
# scripts/client-context.sh (им же закрепляется `stack-up`). Здесь — только
# закрепление выбранного для этого скрипта и его потомков.
# shellcheck source=deploy/scripts/client-context.sh
. "$HERE/client-context.sh"

ctx_cleanup() { [ -z "$STAND_CTX_FILE" ] || rm -f "$STAND_CTX_FILE"; }

guard_context() {
  [ -z "$STAND_CTX" ] || return 0
  local file ctx
  client_context_pick || exit 2
  file="$CC_FILE" ctx="$CC_CTX"
  mkdir -p "$WORK_ROOT" || die "рабочий каталог $WORK_ROOT не заведён" 2
  STAND_CTX_FILE="$(mktemp "$WORK_ROOT/.context.XXXXXX")" || die "файл выбора контекста не заведён" 2
  client_context_file "$STAND_CTX_FILE" || exit 2
  trap ctx_cleanup EXIT
  STAND_KUBECONFIG="$file" STAND_CONTEXT="$ctx" STAND_CTX="$ctx"
  export STAND_KUBECONFIG STAND_CONTEXT KUBECONFIG="$STAND_CTX_FILE:$file" HELM_KUBECONTEXT="$ctx" KACHO_CLIENT_CONTEXT="$ctx"
  # Адрес apiserver'а — из ВЫБРАННОГО контекста (чтение файла, кластер не
  # спрашивается). Объявлен STAND_APISERVER — обязан совпасть. Дальше адрес
  # уходит дочерним шагам: гейт посадки пинит им кластер (stand-cluster-pin.sh),
  # и тот же страж здесь сверяет, что активный для них контекст — выбранный.
  local srv
  srv="$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null)"
  [ -n "$srv" ] || die "контекст -client файла профиля не отдал адрес apiserver'а — пинить кластер нечем" 2
  [ -z "${STAND_APISERVER:-}" ] || [ "$STAND_APISERVER" = "$srv" ] ||
    die "объявленный STAND_APISERVER не адрес контекста -client файла профиля — стенд ушёл бы не туда" 2
  export STAND_APISERVER="$srv"
  bash "$HERE/stand-cluster-pin.sh" >/dev/null || die "активный для дочерних шагов кластер не кластер контекста -client" 2
  log "кластер: контекст -client файла профиля (активный контекст файла не читается)"
}

# ns_state NS — печатает absent | test | foreign; код 2 — кластер не ответил.
ns_state() {
  local out
  if out="$(kubectl get namespace "$1" -o json --request-timeout=30s 2>&1)"; then
    if [ "$(jq -r --arg l "$LABEL_STAND" '.metadata.labels[$l] // ""' <<<"$out")" = test ]; then
      echo test
    else
      echo foreign
    fi
    return 0
  fi
  case "$out" in *"(NotFound)"*) echo absent; return 0 ;; esac
  printf '%s\n' "$out" >&2
  return 2
}

# build_tool DIR — двоичный файл кладётся в рабочий каталог стенда и снимается
# вместе с ним (down, ловушка подъёма), а не остаётся в общем TMPDIR.
build_tool() {
  local out="$1/bin"
  mkdir -p "$out" || return 1
  ( cd "$REPO_ROOT" && go build -o "$out/stand-ns" ./tools/standns/cmd/stand-ns ) >&2 || {
    warn "tools/standns не собрался — переносить рендер нечем"; return 1; }
  STAND_NS_BIN="$out/stand-ns"
}

# ca_namespace — пространство ресурсов cert-manager, ИЗМЕРЕННОЕ у его контроллера.
ca_namespace() {
  local js
  js="$(kubectl get deploy -A -l app.kubernetes.io/name=cert-manager,app.kubernetes.io/component=controller -o json --request-timeout=30s)" || return 2
  jq -r '
    [.items[] | . as $d
     | ([.spec.template.spec.containers[].args[]? | select(startswith("--cluster-resource-namespace="))
         | sub("^--cluster-resource-namespace="; "")][0] // "$(POD_NAMESPACE)") as $v
     | (if $v == "$(POD_NAMESPACE)" then $d.metadata.namespace else $v end)] | unique | .[]' <<<"$js"
}

# dns_peer — пространство и метки подов резолвера кластера, ИЗМЕРЕННЫЕ у его
# службы: единственная служба кластера с портом 53/UDP, её пространство и её
# селектор подов. Печатает «<пространство> <селектор JSON>». Имя пространства
# резолвера — свойство площадки, а не дерева (на управляемом кластере оно не
# kube-system), поэтому оно не выписано. Служб ноль или больше одной —
# неоднозначно, и стенд не поднимается, пока оператор не объявит пару сам:
# STAND_DNS_NAMESPACE и STAND_DNS_SELECTOR (JSON меток подов). Код 2 — кластер
# не ответил либо пара не установлена.
dns_peer() {
  if [ -n "${STAND_DNS_NAMESPACE:-}${STAND_DNS_SELECTOR:-}" ]; then
    if [ -z "${STAND_DNS_NAMESPACE:-}" ] || ! jq -e 'type == "object" and length > 0 and all(.[]; type == "string")' \
      <<<"${STAND_DNS_SELECTOR:-}" >/dev/null 2>&1; then
      warn "объявлена половина пары DNS: STAND_DNS_NAMESPACE=«${STAND_DNS_NAMESPACE:-}», STAND_DNS_SELECTOR=«${STAND_DNS_SELECTOR:-}» (нужны оба, селектор — непустой JSON меток)"
      return 2
    fi
    printf '%s %s\n' "$STAND_DNS_NAMESPACE" "$(jq -c . <<<"$STAND_DNS_SELECTOR")"
    return 0
  fi
  local js out
  js="$(kubectl get svc -A -o json --request-timeout=30s)" || return 2
  out="$(jq -r '[.items[] | select(any(.spec.ports[]?; .port == 53 and (.protocol // "TCP") == "UDP"))
                 | "\(.metadata.namespace) \((.spec.selector // {}) | tojson)"] | .[]' <<<"$js")"
  if [ "$(grep -c . <<<"$out")" != 1 ] || [[ "$out" == *" {}" ]]; then
    warn "служба DNS кластера не установлена однозначно (служб с 53/UDP: $(grep -c . <<<"$out"), селектор пуст — пары не выбрать):"
    warn "$out"
    warn "объяви пару: STAND_DNS_NAMESPACE=<пространство резолвера> STAND_DNS_SELECTOR='{\"<метка>\":\"<значение>\"}'"
    return 2
  fi
  printf '%s\n' "$out"
}

has_default_storage_class() {
  kubectl get storageclass -o json --request-timeout=30s | jq -e \
    '[.items[] | select(.metadata.annotations["storageclass.kubernetes.io/is-default-class"] == "true")] | length > 0' >/dev/null
}

# ── ПОДЪЁМ ───────────────────────────────────────────────────────────────────

# ns_objects NS TASK EXPIRES STACK SHA CANS DNSNS DNSSEL — ПЕРВАЯ запись подъёма одним применением:
# пространство уже с метками и сроком (kill между «создал» и «пометил» оставил бы
# непомеченное пространство, которое down по праву счёл бы чужим), затем квота,
# пределы и сетевая изоляция — первые объекты пространства, до любой нагрузки.
#
# Pod Security — enforce=baseline (замер kacho#3102: стенд dev-prod под ним
# поднимается целиком), warn/audit=restricted — видимость остатка до restricted.
#
# Сетевая изоляция стенда — две политики на все поды пространства:
#   stand-ingress  вход только от подов своего пространства. Проброс порта приходит
#                  в под с петли, пробы готовности — от узла; политикой они не
#                  судятся. Политики чарта ДОБАВЛЯЮТ разрешённое (NetworkPolicy
#                  только объединяет): порт сбора величин края чарт открывает
#                  всем пространствам (networkpolicy-api-gateway.yaml), и стенд
#                  его не сужает — вычитать политикой нечем;
#   stand-egress   выход: свои поды · DNS кластера — порт 53 ТОЛЬКО к подам
#                  резолвера кластера (пространство и метки ИЗМЕРЕНЫ у его
#                  службы, dns_peer) · пространство ресурсов cert-manager ·
#                  адреса вне кластера (реестры, зеркала, apiserver управляемой
#                  площадки) блоком 0.0.0.0/0 и ::/0 на всех портах, КРОМЕ 53:
#                  чужой резолвер — канал наружу в обход DNS кластера, и стенду
#                  он не нужен. Пространства кластера, кроме названных, не
#                  выбраны ничем — `kacho` и чужие стенды закрыты. Блок адресов
#                  узлы и поды кластера на CNI с идентичностью конечных точек
#                  (cilium) не выбирает; что это так на кластере стенда,
#                  утверждает scripts/stand-ns-isolation-probe.sh (к краю `kacho`
#                  — отказ, к своему — проходит; имена кластера разрешаются,
#                  запрос к внешнему резолверу на :53 — отказ).
#
# Числа квоты — по замеру стенда dev-prod на этом кластере (kacho#3102,
# комментарий задачи «замер подъёма»): сумма запросов поднятого стенда и пик
# потребления, с запасом на один перекат (helm --wait поднимает новый под рядом
# со старым). Балансировщиков ноль: вход стенда пробрасывается, площадка его не
# публикует.
ns_objects() {
  cat <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: $1
  labels:
    $LABEL_STAND: test
    $LABEL_TASK: "$2"
    pod-security.kubernetes.io/enforce: baseline
    pod-security.kubernetes.io/enforce-version: latest
    pod-security.kubernetes.io/warn: restricted
    pod-security.kubernetes.io/warn-version: latest
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/audit-version: latest
  annotations:
    $ANN_EXPIRES: "$3"
    kacho.io/stack: "$4"
    kacho.io/ref: "$5"
---
apiVersion: v1
kind: ResourceQuota
metadata:
  name: stand
  namespace: $1
  labels: {$LABEL_STAND: test}
spec:
  hard:
    requests.cpu: "4"
    requests.memory: 10Gi
    limits.memory: 40Gi
    pods: "60"
    services.loadbalancers: "0"
    services.nodeports: "0"
    persistentvolumeclaims: "20"
    requests.storage: 40Gi
---
apiVersion: v1
kind: LimitRange
metadata:
  name: stand
  namespace: $1
  labels: {$LABEL_STAND: test}
spec:
  limits:
    - type: Container
      defaultRequest: {cpu: 10m, memory: 32Mi}
      default: {memory: 512Mi}
      max: {memory: 8Gi}
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: stand-ingress
  namespace: $1
  labels: {$LABEL_STAND: test}
spec:
  podSelector: {}
  policyTypes: [Ingress]
  ingress:
    - from:
        - podSelector: {}
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: stand-egress
  namespace: $1
  labels: {$LABEL_STAND: test}
spec:
  podSelector: {}
  policyTypes: [Egress]
  egress:
    - to:
        - podSelector: {}
    - to:
        - namespaceSelector:
            matchLabels: {kubernetes.io/metadata.name: "$7"}
          podSelector:
            matchLabels: $8
      ports:
        - {protocol: UDP, port: 53}
        - {protocol: TCP, port: 53}
    - to:
        - namespaceSelector:
            matchLabels: {kubernetes.io/metadata.name: "$6"}
    - to:
        - ipBlock: {cidr: 0.0.0.0/0}
        - ipBlock: {cidr: "::/0"}
      ports:
        - {protocol: TCP, port: 1, endPort: 52}
        - {protocol: TCP, port: 54, endPort: 65535}
        - {protocol: UDP, port: 1, endPort: 52}
        - {protocol: UDP, port: 54, endPort: 65535}
EOF
}

# can_write NS — право завести пространство, квоту и пределы, спрошенное ДО
# записи: отказ на середине оставил бы пространство без квоты.
can_write() {
  local verb_res
  for verb_res in "create namespaces" "create resourcequotas -n $1" "create limitranges -n $1" "create networkpolicies -n $1" "delete namespaces"; do
    # shellcheck disable=SC2086 # «глагол ресурс [-n ns]» — разбор по словам и нужен
    [ "$(kubectl auth can-i $verb_res --request-timeout=30s 2>/dev/null)" = yes ] || {
      warn "у учётки нет права «$verb_res»"; return 1; }
  done
}

# kill_tree PID — сигнал процессу и всем его потомкам (helm под tee, npx под
# браузером) и ОЖИДАНИЕ их конца: `kill` одного родителя оставил бы детей писать
# в кластер, а снятие, начатое до конца helm, шло бы наперегонки с его записью.
# Не вышедшие за 60 с получают SIGKILL.
descendants() {
  local c
  for c in $(pgrep -P "$1" 2>/dev/null); do descendants "$c"; printf '%s\n' "$c"; done
}
kill_tree() {
  local pids p alive
  pids="$(descendants "$1") $1"
  for p in $pids; do kill -TERM "$p" 2>/dev/null || true; done
  for _ in $(seq 1 60); do
    alive=""
    for p in $pids; do
      # Зомби (вышел, не прибран родителем) живым не считается.
      case "$(ps -o stat= -p "$p" 2>/dev/null)" in ''|Z*) ;; *) alive="$alive $p" ;; esac
    done
    [ -n "$alive" ] || return 0
    sleep 1
  done
  warn "не вышли за 60 с после TERM:$alive — SIGKILL"
  for p in $alive; do kill -KILL "$p" 2>/dev/null || true; done
}

# bg КОМАНДА… — исполнить в фоне и ждать. Ловушку на сигнал bash исполняет
# только ПОСЛЕ конца переднего процесса — kill посреди `helm --wait` ждал бы
# пятнадцать минут; `wait` же сигналом прерывается сразу.
CHILD=""
bg() {
  "$@" & CHILD=$!
  wait "$CHILD"; local rc=$?
  CHILD=""
  return "$rc"
}

UP_NS="" UP_WORK="" UP_CREATED=0 UP_DONE=0
up_on_signal() {
  trap - INT TERM
  warn "подъём $UP_NS прерван сигналом $1"
  exit "$2"
}
up_on_exit() {
  local rc=$?
  trap - EXIT INT TERM
  [ -z "$CHILD" ] || { kill_tree "$CHILD"; wait "$CHILD" 2>/dev/null; }
  [ "$UP_DONE" = 1 ] && { ctx_cleanup; exit "$rc"; }
  [ "$rc" -ne 0 ] || rc=1
  if [ "$UP_CREATED" = 1 ]; then
    warn "подъём $UP_NS не завершён (код $rc) — снимаю созданное этим вызовом"
    ( cmd_down "$UP_NS" ) || warn "СНЯТИЕ НЕ ПРОШЛО — остаток снимается: make -C deploy stand-ns-down NS=$UP_NS"
  elif [ "${UP_STATE:-}" = absent ]; then
    rm -rf "${UP_WORK:?}"
    warn "подъём $UP_NS отказал (код $rc) ДО первой записи в кластер — снимать в кластере нечего"
  else
    # Пространство было до вызова (повторный up) либо его состояние не прочитано:
    # ни его, ни рабочий каталог стоящего стенда ловушка не трогает — только своё.
    [ -z "$UP_WORK" ] || rm -rf "${UP_WORK:?}/bin"
    [ "${UP_STATE:-}" != test ] ||
      warn "пространство $UP_NS существовало до вызова — ловушка его не снимает (make -C deploy stand-ns-down NS=$UP_NS)"
  fi
  ctx_cleanup
  exit "$rc"
}

stand_overlay() {
  local ns="$1" cans="$2" persist="$3" port="$4"
  local host; host="$(ns_host "$ns")"
  cat <<EOF
# generated by deploy/scripts/stand-ns.sh — не редактировать, не коммитить
# Приспособление цепочки к кластеру, ИЗМЕРЕННОЕ у него, и адрес консоли стенда $ns.
cert-manager:
  enabled: false
ingress-nginx:
  enabled: false
mtls:
  internalCA:
    caNamespace: $cans
global:
  kacho:
    identity:
      appBaseURL: "https://$host:$port"
      webauthnRpId: "$host"
      # Почта стенда — его приёмнику (блок mailpit цепочки), шифрование не
      # снимается: STARTTLS, якорь — ca.crt листа приёмника. Удостоверения
      # ретранслятора площадки у стенда нет и быть не должно.
      smtp:
        connectionURI: 'smtp://{{ .Release.Name }}-mailpit:1025/'
        fromAddress: "noreply@mail.kacho.test"
        credentialSecret:
          name: ""
          key: ""
        trustAnchorSecret:
          name: kacho-mailpit-tls
          key: ca.crt
uif:
  # Первый лист внешнего входа консоли — ВРЕМЕННЫЙ (certificate-public.yaml,
  # issue-temporary-certificate), настоящий раздача перечитывает сторожем раз в
  # tlsReload.intervalSeconds (умолчание 300 с). Стенд проб живёт часы и
  # используется сразу после подъёма: пять минут временного листа, которому
  # браузер проб не доверяет, — это пять минут «не выполнилось». Период — только
  # каденция перечтения, посадку он не трогает.
  tlsReload:
    intervalSeconds: 10
  subscriptionStream:
    ingress:
      enabled: true
  publicFront:
    enabled: true
    service:
      # Имя входа — умолчание чарта (<раздача>-public): его пробрасывает
      # forward. Имя балансировщика площадки к стенду отношения не имеет.
      name: ""
      type: ClusterIP
    tls:
      secretName: console-public-tls
      certificate:
        create: true
        namesFromOrigin: true
        issuerRef:
          name: kacho-internal-ca
          kind: ClusterIssuer
    acme:
      enabled: false
EOF
  if [ "$persist" = off ]; then
    # Класса томов по умолчанию у кластера нет: заявка на том висела бы в Pending
    # вечно. Базы — псевдонимы подчарта postgresql в Chart.yaml зонтика (перечень
    # выводится, а не выписывается); хранилище слоёв реестра — своей ручкой.
    # Пропущенную заявку ловит предрендер ниже (persistent_claims).
    local pg
    for pg in $(python3 -c 'import sys,yaml; [print(d.get("alias") or d["name"]) for d in yaml.safe_load(open(sys.argv[1]))["dependencies"] if d["name"]=="postgresql"]' "$UMBRELLA/Chart.yaml"); do
      printf '%s:\n  primary:\n    persistence:\n      enabled: false\n  readReplicas:\n    persistence:\n      enabled: false\n' "$pg"
    done
    printf 'registry:\n  zot:\n    storage:\n      persistence: false\n'
  fi
}

# bootstrap_secret_of RENDER — имя секрета первого администратора облака, на
# который рендер ссылается из KANAME_BOOTSTRAP_ROOT_EMAIL (secretKeyRef); пусто —
# цепочка администратора секретом не объявляет, и шага нет. Больше одного имени —
# код 1: какой из них читать, не установлено.
bootstrap_secret_of() {
  python3 - "$1" <<'PY'
import sys, yaml
names = set()
for d in yaml.safe_load_all(open(sys.argv[1])):
    if not isinstance(d, dict):
        continue
    spec = ((d.get("spec") or {}).get("template") or {}).get("spec") or {}
    for c in (spec.get("containers") or []) + (spec.get("initContainers") or []):
        for e in c.get("env") or []:
            if e.get("name") == "KANAME_BOOTSTRAP_ROOT_EMAIL":
                ref = ((e.get("valueFrom") or {}).get("secretKeyRef") or {}).get("name")
                if ref:
                    names.add(ref)
if len(names) > 1:
    sys.exit("секретов администратора облака больше одного: %s" % sorted(names))
for n in names:
    print(n)
PY
}

# subchart_defaults — умолчания подчартов-частей дерева под их ключом в зонтике.
# Цепочка `dev` объявляет образы консоли НЕ в слоях, а наследует из умолчаний
# подчарта (`ui-future/deploy/values.yaml`); сложение одних слоёв их не видит, и
# консоль осталась бы на `:dev`, которого в реестре нет. Поэтому первым слоем
# идут умолчания каждого локального подчарта — ровно как их складывает helm.
subchart_defaults() {
  python3 - "$UMBRELLA" <<'PY'
import os, sys, yaml
root = sys.argv[1]
chart = yaml.safe_load(open(os.path.join(root, "Chart.yaml")))
out = {}
for dep in chart.get("dependencies") or []:
    repo = dep.get("repository") or ""
    if repo.startswith("file://"):
        path = os.path.normpath(os.path.join(root, repo[len("file://"):]))
    else:
        path = os.path.join(root, "charts", dep["name"])
    vf = os.path.join(path, "values.yaml")
    if not os.path.isfile(vf):
        continue
    out[dep.get("alias") or dep["name"]] = yaml.safe_load(open(vf)) or {}
if not out:
    sys.exit("ни одного локального подчарта не прочитано — умолчания складывать не из чего")
yaml.safe_dump(out, sys.stdout, sort_keys=True)
PY
}

images_overlay() {
  local sha="$1" out="$2"; shift 2
  local names branch tags refs="" img tag
  names="$("$STAND_NS_BIN" images -list "$@")" || return 2
  for branch in $IMAGE_BRANCHES; do
    if tags="$(bash "$HERE/gen-managed-image-pins.sh" --tag-of "$(tr '\n' ',' <<<"$names")" --ref "$sha" --branch "$branch" 2>/dev/null)"; then
      IMAGE_BRANCH="$branch"; break
    fi
    tags=""
  done
  [ -n "$tags" ] || { warn "коммит $sha не входит ни в одну из веток публикации ($IMAGE_BRANCHES) — образов этой ревизии нет"; return 2; }
  while IFS=$'\t' read -r img tag; do
    [ -n "$img" ] || continue
    refs="$refs $img=$IMAGE_PREFIX/$img:$tag"
    reachable "$IMAGE_PREFIX/$img" "$tag" || { warn "образ $IMAGE_PREFIX/$img:$tag в реестре НЕ найден — стенд ушёл бы в ImagePullBackOff"; return 2; }
  done <<<"$tags"
  log "образы ревизии $sha (ветка публикации $IMAGE_BRANCH): $(wc -l <<<"$tags") частей дерева, каждая найдена в реестре"
  "$STAND_NS_BIN" images -refs "$refs" "$@" >"$out"
}

# reachable REPO TAG — манифест образа отвечает. Проверяется только docker.io:
# у прочих реестров вопрос задаёт сам кластер при подъёме, и это печатается.
reachable() {
  local repo="$1" tag="$2" path tok code
  case "$repo" in
    docker.io/*) path="${repo#docker.io/}" ;;
    *) warn "досягаемость $repo:$tag НЕ проверена (не docker.io) — ответит кластер"; return 0 ;;
  esac
  tok="$(curl -fsS --max-time 20 "https://auth.docker.io/token?service=registry.docker.io&scope=repository:$path:pull" | jq -r .token)" || return 1
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 -I -H "Authorization: Bearer $tok" \
    -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json' \
    "https://registry-1.docker.io/v2/$path/manifests/$tag")"
  [ "$code" = 200 ]
}

# ref_guard SHA — образы ревизии SHA вправе исполнять чарт рабочей копии, только
# если между ними не менялся ни один ВХОД СБОРКИ ОБРАЗОВ: иначе стенд назывался бы
# стендом этой копии, а исполнял бы другой код. Что вход, а что нет, выводится из
# Dockerfile дерева (tools/standns/inputs.go, правило в шапке): оснастка стенда,
# чарты и проверки, не доезжающие ни до одного образа, отличием не считаются;
# продуктовый код в замыкании `go build`, исходник консоли в контексте её образа,
# Dockerfile и go.mod — считаются всегда. Сравнивается рабочее дерево (вместе с
# незакоммиченным и неотслеживаемым), а не только HEAD.
ref_guard() {
  local sha="$1" head out rc
  head="$(git -C "$REPO_ROOT" rev-parse HEAD)"
  git -C "$REPO_ROOT" merge-base --is-ancestor "$sha" "$head" || {
    warn "ревизия образов $sha не предок рабочей копии $head — чарт и образы из разных историй"; return 1; }
  out="$("$STAND_NS_BIN" inputs -root "$REPO_ROOT" -base "$sha")"; rc=$?
  case "$rc" in
    0) log "чарт рабочей копии $head поверх образов $sha: $(tail -1 <<<"$out" | sed 's/^stand-ns inputs: //')" ;;
    1) warn "между ревизией образов $sha и рабочей копией менялись входы сборки образов:"
       grep -E '^  ВХОД' <<<"$out" >&2
       warn "образы ревизии этого кода не несут; подними стенд от ревизии, для которой они опубликованы"
       return 1 ;;
    *) warn "входы сборки не выведены (код $rc) — «не вывел» не равно «не менялись»"; printf '%s\n' "$out" >&2; return 1 ;;
  esac
}

helm_install() {
  HELM_PLUGINS="$DEPLOY_ROOT/helm/plugins" KACHO_STAND_NS_BIN="$STAND_NS_BIN" \
    helm upgrade --install "$RELEASE" "$UMBRELLA" -n "$UP_NS" "$@" \
      --wait --timeout "$UP_TIMEOUT" 2>&1 | tee "$UP_WORK/helm.log"
  return "${PIPESTATUS[0]}"
}

# tee_to FILE КОМАНДА… — вывод на экран и в журнал, код — команды.
tee_to() {
  local f="$1"; shift
  "$@" 2>&1 | tee "$f"
  return "${PIPESTATUS[0]}"
}

cmd_up() {
  local ns="$1" stack="${2:-dev-prod}" ref="${3:-HEAD}" task cans dns dnsns dnssel persist port sha start work chain expires
  task="$(ns_valid "$ns")" || die "имя «$ns» не по правилу t<номер задачи>-<коротко> (≤ 40 знаков, DNS-1123). Пространство kacho — рабочий стенд, цель его не трогает" 2
  ttl_valid "$TTL_HOURS" || die "STAND_TTL_HOURS=«$TTL_HOURS» вне 1..$TTL_MAX_HOURS — стенд проб без срока не живёт" 2
  need_tools; guard_context
  start="$(date +%s)"
  work="$WORK_ROOT/$ns"
  UP_NS="$ns"; UP_WORK="$work"
  trap 'up_on_exit' EXIT
  trap 'up_on_signal INT 130' INT
  trap 'up_on_signal TERM 143' TERM

  # ── 1. Без записи в кластер ──
  UP_STATE="$(ns_state "$ns")" || die "состояние пространства $ns НЕ ПРОЧИТАНО — кластер не ответил" 2
  [ "$UP_STATE" != foreign ] || die "пространство $ns существует и НЕ помечено $LABEL_STAND=test — оно чужое, стенд в него не ставится" 1
  chain="$(bash "$DEPLOY_ROOT/tests/helm/stacks.sh" --args "$stack" "$UMBRELLA")" && [ -n "$chain" ] ||
    die "цепочка «$stack» в deploy/stacks.txt не прочитана — стенд без цепочки сел бы на умолчания чарта" 2
  sha="$(git -C "$REPO_ROOT" rev-parse --verify "${ref}^{commit}" 2>/dev/null)" || die "ревизия «$ref» не разрешается в коммит" 2
  mkdir -p "$work" || die "рабочий каталог $work не заведён" 2
  build_tool "$work" || die "инструмент переноса не собран" 2
  ref_guard "$sha" || die "ревизия образов и рабочая копия расходятся (выше)" 2
  cans="$(ca_namespace)" || die "контроллер cert-manager на кластере не прочитан" 2
  [ "$(wc -w <<<"$cans")" = 1 ] || die "пространство ресурсов cert-manager не установлено однозначно («$cans»): стенд cert-manager не ставит, он переиспользует ровно один существующий" 2
  dns="$(dns_peer)" || die "резолвер кластера не установлен (выше) — выход DNS стенда сузить не до чего" 2
  dnsns="${dns%% *}"; dnssel="${dns#* }"
  if has_default_storage_class; then persist=on; else persist=off; fi
  port="$(ns_port "$ns")"
  log "кластер: cert-manager переиспользуется (ресурсы в ns $cans); DNS — поды $dnssel в ns $dnsns; класс томов по умолчанию: $persist; контроллер входа не ставится"

  stand_overlay "$ns" "$cans" "$persist" "$port" >"$work/stand.yaml"
  subchart_defaults >"$work/subchart-defaults.yaml" || die "умолчания подчартов не прочитаны" 2
  local layers=("$work/subchart-defaults.yaml" "$UMBRELLA/values.yaml") f files
  files="$(bash "$DEPLOY_ROOT/tests/helm/stacks.sh" --chain "$stack" ' ')" && [ -n "$files" ] ||
    die "слои цепочки «$stack» не прочитаны — накладку образов выводить не из чего" 2
  for f in $files; do layers+=("$UMBRELLA/$f"); done
  images_overlay "$sha" "$work/images.yaml" "${layers[@]}" || die "накладка образов ревизии $sha не выведена" 2

  expires="$(date -u -d "+${TTL_HOURS} hours" +%Y-%m-%dT%H:%M:%SZ)"
  ns_objects "$ns" "$task" "$expires" "$stack" "$sha" "$cans" "$dnsns" "$dnssel" >"$work/ns.yaml"
  kubectl apply --dry-run=client -f "$work/ns.yaml" >/dev/null || die "пространство, квота, пределы и сетевая изоляция не проходят проверку манифеста" 2
  can_write "$ns" || die "завести пространство с квотой нечем (права выше) — ни одной записи не сделано" 2

  bash "$HERE/helm-umbrella-deps.sh" >/dev/null || die "зависимости зонтичного чарта не материализованы" 2
  local extra_base=(-f "$work/images.yaml" -f "$work/stand.yaml")
  # shellcheck disable=SC2206 # цепочка — слова `-f <файл>`, разбор по словам и нужен
  local chain_args=($chain)
  # Предрендер ДО записи: отказ переноса и заявка на том без класса видны раньше,
  # чем появится пространство. Отпечаток доставки манифестов модулей становится
  # известен только после доставки — на предрендере он заглушка (helm template
  # кластера не спрашивает, на форму рендера отпечаток не влияет).
  printf 'global:\n  kachoModuleManifests:\n    digest: "prerender"\n' >"$work/mm-prerender.yaml"
  HELM_PLUGINS="$DEPLOY_ROOT/helm/plugins" KACHO_STAND_NS_BIN="$STAND_NS_BIN" \
    helm template "$RELEASE" "$UMBRELLA" -n "$ns" "${chain_args[@]}" "${extra_base[@]}" -f "$work/mm-prerender.yaml" \
      --set cert-manager.enabled=false \
      --post-renderer kacho-stand-ns \
      --post-renderer-args "-namespace=$ns" --post-renderer-args "-ca-namespace=$cans" >"$work/render.yaml" ||
    die "предрендер стенда отказал (текст выше)" 2
  if [ "$persist" = off ]; then
    local claims
    claims="$(python3 -c '
import sys, yaml
for d in yaml.safe_load_all(open(sys.argv[1])):
    if not d: continue
    if d.get("kind") == "PersistentVolumeClaim" or (d.get("spec") or {}).get("volumeClaimTemplates"):
        print("  %s/%s" % (d["kind"], d["metadata"]["name"]))' "$work/render.yaml")"
    [ -z "$claims" ] || die "у кластера нет класса томов по умолчанию, а рендер заявляет тома — их заявки висели бы в Pending:
$claims" 2
  fi
  log "проверено без записи в кластер: ревизия, образы, право на квоту, предрендер"

  # ── 2. Первая запись: пространство с метками и сроком, квота, пределы ──
  [ "$UP_STATE" = absent ] && UP_CREATED=1
  kubectl apply -f "$work/ns.yaml" >/dev/null || die "пространство $ns с квотой не заведено" 1
  log "пространство $ns: задача #$task, срок $expires; квота, пределы, Pod Security baseline и сетевая изоляция применены"

  # ── 3. Стенд ──
  bg make -C "$DEPLOY_ROOT" --no-print-directory module-manifests-configmap \
    MODULE_MANIFESTS_STACK="$stack" STACK_NAMESPACE="$ns" EXPECT_CONTEXT="$STAND_CTX" || die "манифесты модулей не доставлены" 1
  local mm="$UMBRELLA/values.module-manifests.yaml"
  local extra=("${extra_base[@]}" -f "$mm" --set cert-manager.enabled=false)
  local args=("${chain_args[@]}" "${extra[@]}")
  STACK_NAMESPACE="$ns" STACK_RELEASE="$RELEASE" bg bash "$HERE/stack-secrets.sh" "$stack" "${extra[@]}" ||
    die "предусловные секреты стенда не созданы" 1

  log "helm: цепочка $stack + образы $sha + стенд, перенос в $ns"
  bg helm_install "${args[@]}" \
      --post-renderer kacho-stand-ns \
      --post-renderer-args "-namespace=$ns" --post-renderer-args "-ca-namespace=$cans" ||
    die "helm upgrade --install отказал (журнал $work/helm.log)" 1

  # Прокси API-сервера на управляемом кластере до подов не доходит — вопрос
  # задаётся пробросом (scripts/wait-edge-ready.sh, EDGE_READY_VIA).
  EDGE_READY_VIA=port-forward bg bash "$HERE/wait-edge-ready.sh" "$ns" api-gateway 90 2 3 || die "край стенда не ответил готовностью" 1
  KACHO_NS="$ns" bg bash "$HERE/seed-geo-baseline.sh" || die "посев каталога geo не прошёл" 1
  KACHO_NS="$ns" POSTURE_SKIP="${POSTURE_SKIP:-}" bg bash "$HERE/seed-storage-catalog.sh" || die "посев каталога хранения не прошёл" 1
  KACHO_NS="$ns" POSTURE_SKIP="${POSTURE_SKIP:-}" bg bash "$HERE/seed-vpc-address-pools.sh" || die "посев полосы адресов не прошёл" 1
  # Первый администратор облака — посевом путём продукта (регистрация, код из
  # ящика стенда, подтверждение; kacho#2878), если цепочка объявляет его
  # секретом. Секрет на стенде проб чеканит stack-secrets.sh. Шаг — до гейта
  # посадки: стенд не объявляется поднятым без входа администратора.
  local admin_secret
  admin_secret="$(bootstrap_secret_of "$work/render.yaml")" || die "секрет администратора облака из рендера не выведен (выше)" 1
  if [ -n "$admin_secret" ]; then
    STACK_NAMESPACE="$ns" STACK_RELEASE="$RELEASE" KACHO_CLOUD_ADMIN_SECRET="$admin_secret" \
      bg tee_to "$work/cloud-admin.log" bash "$HERE/bootstrap-cloud-admin.sh" ||
      die "администратор облака не заведён либо не входит (журнал $work/cloud-admin.log)" 1
  else
    log "цепочка $stack администратора облака секретом не объявляет — шага нет"
  fi
  NS="$ns" POSTURE_SKIP="${POSTURE_SKIP:-}" POSTURE_PROFILE=production \
    bg tee_to "$work/posture.log" bash "$HERE/assert-production-posture.sh" || die "боевая посадка стенда не доказана (журнал $work/posture.log)" 1
  local prc=0
  bg tee_to "$work/provenance.log" bash "$HERE/stand-provenance.sh" --namespace "$ns" --expect "$sha" || prc=$?
  [ "$prc" -eq 0 ] || die "провенанс стенда не сходится с ревизией $sha (код $prc, журнал $work/provenance.log)" 1

  UP_DONE=1
  trap - INT TERM
  trap ctx_cleanup EXIT
  log "стенд $ns ПОДНЯТ за $(( $(date +%s) - start )) с: цепочка $stack, образы $sha, срок $expires"
  log "консоль https://$(ns_host "$ns"):$port · край https://127.0.0.1:$((port + 1)) · приёмник http://127.0.0.1:$((port + 2))"
  log "проброс и окружение проб:  bash deploy/scripts/stand-ns.sh forward $ns"
}

# ── ПРОГОН: ПОДЪЁМ → КОМАНДА → СНЯТИЕ ВСЕГДА ─────────────────────────────────
#
# Код выхода — код КОМАНДЫ. Если стенд не поднялся или проброс не встал, команда
# не исполнялась: это «не выполнилось», а не красное, и код — RUN_UNMET (125),
# которого команда сама не даёт (код 125 зарезервирован этим смыслом).
RUN_UNMET=125
RUN_NS="" RUN_KEEP=0 RUN_UP=0 RUN_CHILD=""
run_on_signal() {
  trap - INT TERM
  warn "прогон на $RUN_NS прерван сигналом $1"
  if [ -n "$RUN_CHILD" ]; then
    # Подъём снимает своё сам (его ловушка); команде — сигнал всему дереву.
    if [ "$RUN_UP" = 1 ]; then kill_tree "$RUN_CHILD"; else kill -TERM "$RUN_CHILD" 2>/dev/null; fi
    wait "$RUN_CHILD" 2>/dev/null
    RUN_CHILD=""
  fi
  exit "$2"
}
run_on_exit() {
  local rc=$? exp
  trap - EXIT INT TERM
  [ -z "$RUN_CHILD" ] || { kill_tree "$RUN_CHILD"; wait "$RUN_CHILD" 2>/dev/null; }
  # Журналы проброса уходят вместе с рабочим каталогом стенда — на неуспехе они
  # печатаются ДО снятия: обрыв проброса отличим от дефекта продукта только по ним.
  if [ "$rc" -ne 0 ] && [ "$RUN_UP" = 1 ]; then
    local f
    for f in "$WORK_ROOT/$RUN_NS"/forward-*.log; do
      [ -f "$f" ] || continue
      warn "── $(basename "$f"): ошибок проброса $(grep -c '^E[0-9]' "$f")"
      grep '^E[0-9]' "$f" | tail -n 5 >&2
    done
  fi
  bash "$HERE/stand-ns.sh" unforward "$RUN_NS" >/dev/null 2>&1
  if [ "$RUN_KEEP" = 1 ] && [ "$RUN_UP" = 1 ]; then
    exp="$(date -u -d "+${TTL_HOURS} hours" +%Y-%m-%dT%H:%M:%SZ)"
    kubectl annotate namespace "$RUN_NS" --overwrite "$ANN_EXPIRES=$exp" >/dev/null ||
      warn "срок $RUN_NS не продлён — снимет census --expired по прежнему"
    log "--keep: стенд $RUN_NS ОСТАВЛЕН для разбора до $exp; снять: make -C deploy stand-ns-down NS=$RUN_NS"
  else
    bash "$HERE/stand-ns.sh" down "$RUN_NS" || {
      warn "СНЯТИЕ $RUN_NS НЕ ПРОШЛО — остаток: make -C deploy stand-ns-down NS=$RUN_NS"
      [ "$rc" -ne 0 ] || rc=1
    }
  fi
  log "прогон на $RUN_NS: код $rc"
  ctx_cleanup
  exit "$rc"
}

cmd_run() {
  local ns="$1" stack=dev-prod ref=HEAD state rc
  shift
  while [ $# -gt 0 ]; do
    case "$1" in
      --keep)  RUN_KEEP=1 ;;
      --stack) stack="${2:?--stack без значения}"; shift ;;
      --ref)   ref="${2:?--ref без значения}"; shift ;;
      --)      shift; break ;;
      *) die "run: неизвестный аргумент «$1» (команда — после --)" 2 ;;
    esac
    shift
  done
  [ $# -gt 0 ] || die "run: команда не названа (stand-ns.sh run NS [--keep] [--stack S] [--ref R] -- КОМАНДА…)" 2
  ns_valid "$ns" >/dev/null || die "имя «$ns» не по правилу t<номер задачи>-<коротко>; пространство kacho цель не трогает" 2
  ttl_valid "$TTL_HOURS" || die "STAND_TTL_HOURS=«$TTL_HOURS» вне 1..$TTL_MAX_HOURS" 2
  need_tools; guard_context
  state="$(ns_state "$ns")" || die "состояние пространства $ns НЕ ПРОЧИТАНО — кластер не ответил" 2
  [ "$state" = absent ] || die "пространство $ns уже есть ($state): прогон поднимает СВОЙ стенд и его же снимает — возьми другое имя" 1
  RUN_NS="$ns"
  trap 'run_on_exit' EXIT
  trap 'run_on_signal INT 130' INT
  trap 'run_on_signal TERM 143' TERM

  bash "$HERE/stand-ns.sh" up "$ns" "$stack" "$ref" & RUN_CHILD=$!
  wait "$RUN_CHILD"; rc=$?; RUN_CHILD=""
  [ "$rc" -eq 0 ] || { warn "стенд $ns не поднят (код $rc) — команда НЕ исполнялась: условие прогона не создано"; exit "$RUN_UNMET"; }
  bash "$HERE/stand-ns.sh" forward "$ns" || { warn "проброс $ns не поднят — команда НЕ исполнялась"; exit "$RUN_UNMET"; }
  # shellcheck disable=SC1090,SC1091 # файл окружения пишет cmd_forward
  . "$WORK_ROOT/$ns/probe.env"
  export CONSOLE_BASE="$KACHO_CONSOLE_URL" EDGE_BASE="$KACHO_EDGE_URL" KACHO_STAND_NS="$ns"
  RUN_UP=1
  log "команда на $ns: $*"
  "$@" & RUN_CHILD=$!
  wait "$RUN_CHILD"; rc=$?; RUN_CHILD=""
  log "команда завершилась кодом $rc"
  exit "$rc"
}

# ── ПРОБРОС ──────────────────────────────────────────────────────────────────

cmd_forward() {
  local ns="$1" port work host lp
  ns_valid "$ns" >/dev/null || die "имя «$ns» не по правилу" 2
  need_tools; guard_context
  [ "$(ns_state "$ns")" = test ] || die "тестового пространства $ns нет" 1
  port="$(ns_port "$ns")"; host="$(ns_host "$ns")"; work="$WORK_ROOT/$ns"; mkdir -p "$work"
  cmd_unforward "$ns" >/dev/null 2>&1
  kubectl -n "$ns" get secret console-public-tls -o 'jsonpath={.data.ca\.crt}' | base64 -d >"$work/console-ca.pem"
  grep -q 'BEGIN CERTIFICATE' "$work/console-ca.pem" || die "лист консоли (console-public-tls) не выпущен — проброс бессмыслен" 1
  # Браузер проб доверяет ЛИСТУ (отпечаток SPKI, ui-future/e2e/stand-tls-trust.ts),
  # node — корню: у стенда это разные сертификаты, лист выпущен своим CA стенда.
  kubectl -n "$ns" get secret console-public-tls -o 'jsonpath={.data.tls\.crt}' | base64 -d |
    awk '/BEGIN CERTIFICATE/{n++} n==1' >"$work/console-leaf.pem"
  grep -q 'END CERTIFICATE' "$work/console-leaf.pem" || die "лист консоли не прочитан из console-public-tls" 1
  local pair svc rport
  for pair in "ui-public:443:$port" "api-gateway:8443:$((port + 1))" "$RELEASE-mailpit:8025:$((port + 2))"; do
    IFS=: read -r svc rport lp <<<"$pair"
    nohup "$(type -P kubectl)" --kubeconfig "$STAND_KUBECONFIG" --context "$STAND_CTX" -n "$ns" port-forward "svc/$svc" "$lp:$rport" >"$work/forward-$svc.log" 2>&1 &
    echo $! >>"$work/forward.pids"
  done
  # Спрашивается оболочка консоли (`/`), а не `/healthz`: на порту внешнего входа
  # точка здоровья намеренно отвечает 404 (ui-future/deploy, configmap-nginx.yaml) —
  # её читают пробы кубелета на внутреннем порту, не пользователь.
  #
  # Ждётся ответ 200 под корнем стенда, то есть раздача отдаёт ВЫПУЩЕННЫЙ лист, а
  # не временный первого выпуска (certificate-public.yaml): его сменяет сторож
  # раздачи после того, как кубелет обновит смонтированный секрет. Предел —
  # синхронизация секрета кубелетом (до ~2 мин) и период сторожа, с запасом;
  # ручка STAND_FORWARD_WAIT (секунды).
  local code="" deadline=$(( $(date +%s) + ${STAND_FORWARD_WAIT:-300} ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 --cacert "$work/console-ca.pem" \
      --resolve "$host:$port:127.0.0.1" "https://$host:$port/" || true)"
    [ "$code" = 200 ] && break
    sleep 3
  done
  if [ "$code" != 200 ]; then
    local f served want
    for f in "$work"/forward-*.log; do warn "── $(basename "$f")"; tail -n 5 "$f" >&2; done
    served="$(openssl s_client -connect "127.0.0.1:$port" -servername "$host" </dev/null 2>/dev/null |
      openssl x509 -noout -issuer -fingerprint -sha256 2>/dev/null | tr '\n' ' ')"
    want="$(openssl x509 -in "$work/console-leaf.pem" -noout -issuer -fingerprint -sha256 | tr '\n' ' ')"
    warn "раздача отдаёт лист: ${served:-<не прочитан>}"
    warn "в секрете выпущен:   $want"
    cmd_unforward "$ns" >/dev/null 2>&1
    die "консоль через проброс не ответила за ${STAND_FORWARD_WAIT:-300} с (последний код $code; журналы и листы выше)" 1
  fi
  cat >"$work/probe.env" <<EOF
export KACHO_CONSOLE_URL=https://$host:$port
export KACHO_CONSOLE_HOST_IP=127.0.0.1
export KACHO_CONSOLE_CA=$work/console-leaf.pem
export NODE_EXTRA_CA_CERTS=$work/console-ca.pem
export KACHO_CONSOLE_MAILBOX_URL=http://127.0.0.1:$((port + 2))
export KACHO_EDGE_URL=https://127.0.0.1:$((port + 1))
EOF
  log "проброс поднят; окружение проб: . $work/probe.env"
}

cmd_unforward() {
  local ns="$1" work="$WORK_ROOT/$1" pid
  [ -f "$work/forward.pids" ] || return 0
  while read -r pid; do kill "$pid" 2>/dev/null || true; done <"$work/forward.pids"
  rm -f "$work/forward.pids"
  log "проброс $ns снят"
}

# ── СНЯТИЕ ───────────────────────────────────────────────────────────────────

# leftovers NS — помеченные объекты стенда ВНЕ его пространства (издатели CA,
# корень CA и его секрет в пространстве ресурсов cert-manager).
leftovers() {
  local cans
  kubectl get clusterissuer -l "$LABEL_STAND_NS=$1" -o name --request-timeout=30s 2>/dev/null
  for cans in $(ca_namespace 2>/dev/null); do
    kubectl -n "$cans" get certificate,secret -l "$LABEL_STAND_NS=$1" -o name --request-timeout=30s 2>/dev/null | sed "s|^|-n $cans |"
  done
}

cmd_down() {
  local ns="$1" state left
  ns_valid "$ns" >/dev/null || die "имя «$ns» не по правилу t<номер>-<коротко>; пространство kacho цель не снимает никогда" 2
  need_tools; guard_context
  cmd_unforward "$ns" >/dev/null 2>&1
  state="$(ns_state "$ns")" || die "состояние пространства $ns НЕ ПРОЧИТАНО — кластер не ответил; «не прочитал» не равно «нет»" 2
  [ "$state" != foreign ] || die "пространство $ns НЕ помечено $LABEL_STAND=test — оно не стенд проб, и снимать его эта цель не вправе" 1
  if [ "$state" = test ] && helm status "$RELEASE" -n "$ns" >/dev/null 2>&1; then
    log "helm uninstall $RELEASE -n $ns (уносит свои издатели CA и корень CA)"
    helm uninstall "$RELEASE" -n "$ns" --wait --timeout 5m || warn "helm uninstall отказал — помеченное снимается ниже по метке"
  fi
  left="$(leftovers "$ns")"
  if [ -n "$left" ]; then
    log "помеченное вне пространства: $(wc -l <<<"$left") объект(ов) — снимаю"
    while read -r line; do
      [ -n "$line" ] || continue
      # shellcheck disable=SC2086 # строка — «[-n ns] вид/имя», разбор по словам и нужен
      kubectl delete $line --ignore-not-found --wait=true --timeout=120s >/dev/null || warn "не снят: $line"
    done <<<"$left"
  fi
  if [ "$state" = test ]; then
    log "kubectl delete namespace $ns (тома и всё прочее — вместе с ним)"
    kubectl delete namespace "$ns" --wait=true --timeout=10m >/dev/null || warn "удаление $ns не завершилось за 10 мин"
  fi
  for _ in $(seq 1 60); do
    state="$(ns_state "$ns")" || state=unknown
    [ "$state" = absent ] && break
    sleep 5
  done
  [ "$state" = absent ] || die "пространство $ns не исчезло (состояние: $state)" 1
  left="$(leftovers "$ns")"
  [ -z "$left" ] || die "после снятия осталось помеченное вне пространства:
$left" 1
  rm -rf "${WORK_ROOT:?}/$ns"
  log "стенд $ns снят: пространства нет, помеченного вне него 0"
}

# ── ПЕРЕПИСЬ ─────────────────────────────────────────────────────────────────

cmd_census() {
  local expired_mode=0 js now rows n=0 nexp=0 name task exp age state
  [ "${1:-}" = --expired ] && expired_mode=1
  need_tools; guard_context
  js="$(kubectl get namespace -l "$LABEL_STAND=test" -o json --request-timeout=30s)" || die "перечень пространств НЕ ПРОЧИТАН — кластер не ответил" 2
  now="$(date -u +%s)"
  rows="$(jq -r --arg t "$LABEL_TASK" --arg e "$ANN_EXPIRES" \
    '.items[] | [.metadata.name, (.metadata.labels[$t] // "-"), (.metadata.annotations[$e] // "-"), .metadata.creationTimestamp] | @tsv' <<<"$js")" ||
    die "перечень пространств не разобран — «не разобрал» не равно «ноль»" 2
  printf '%-32s %-7s %-8s %-22s %s\n' ПРОСТРАНСТВО ЗАДАЧА ВОЗРАСТ СРОК СОСТОЯНИЕ
  local doomed=""
  while IFS=$'\t' read -r name task exp created; do
    [ -n "$name" ] || continue
    n=$((n + 1))
    age="$(( (now - $(date -u -d "$created" +%s)) / 60 ))м"
    expired_at "$exp" "$now"
    case $? in
      0) state="ИСТЁК"; nexp=$((nexp + 1)); doomed="$doomed $name" ;;
      1) state="жив" ;;
      *) state="СРОК НЕ ЧИТАЕТСЯ — не снимается автоматически" ;;
    esac
    printf '%-32s %-7s %-8s %-22s %s\n' "$name" "$task" "$age" "$exp" "$state"
  done <<<"$rows"
  local orphans
  orphans="$(kubectl get clusterissuer -o json --request-timeout=30s | jq -r --arg l "$LABEL_STAND_NS" \
    '.items[] | select(.metadata.labels[$l] != null) | "\(.metadata.labels[$l]) clusterissuer/\(.metadata.name)"')"
  local o oc=0
  while read -r o; do
    [ -n "$o" ] || continue
    [ "$(ns_state "${o%% *}" 2>/dev/null)" = absent ] || continue
    oc=$((oc + 1)); warn "сирота: ${o#* } (стенд ${o%% *} снят, объект остался)"
  done <<<"$orphans"
  echo "тестовых пространств $n, истёкших $nexp, сирот вне пространств $oc"
  if [ "$expired_mode" = 1 ]; then
    for name in $doomed; do ( cmd_down "$name" ) || warn "истёкший $name не снят"; done
  fi
}

case "${1:-}" in
  up)        shift; [ -n "${1:-}" ] || die "up: не названо пространство (NS=t<задача>-<коротко>)" 2; cmd_up "$@" ;;
  run)       shift; [ -n "${1:-}" ] || die "run: не названо пространство" 2; cmd_run "$@" ;;
  down)      shift; [ -n "${1:-}" ] || die "down: не названо пространство" 2; cmd_down "$1" ;;
  census)    shift; cmd_census "$@" ;;
  forward)   shift; [ -n "${1:-}" ] || die "forward: не названо пространство" 2; cmd_forward "$1" ;;
  unforward) shift; [ -n "${1:-}" ] || die "unforward: не названо пространство" 2; cmd_unforward "$1" ;;
  self-test) self_test ;;
  *) die "режим не назван: up | down | run | census | forward | unforward | self-test" 2 ;;
esac
