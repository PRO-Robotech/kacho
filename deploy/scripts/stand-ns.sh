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
#   stand-ns.sh run NS --runner newman|browser [--out DIR] … -- КОМАНДА…
#                                     то же, но КОМАНДА идёт в pod пространства стенда, а
#                                     не на машине (раздел «РАННЕР В POD СТЕНДА», kacho#3346)
#   stand-ns.sh pod-forward NS        forward изнутри pod раннера (вне pod — отказ)
#   stand-ns.sh census [--expired]    перечень тестовых пространств; --expired снимает просроченные
#   stand-ns.sh forward NS            проброс консоли, края и приёмника писем, дверь чтения писем
#                                     (stand-mailbox-door.py) + файл окружения проб
#   stand-ns.sh unforward NS          снять проброс
#   stand-ns.sh reload NS SVC [REF]   перекатить ОДНУ службу своего стенда на образы ревизии
#                                     REF (умолчание HEAD), не пересоздавая стенд; провенанс
#                                     каждого пода службы обязан сойтись с REF
#   stand-ns.sh self-test             проба правил имени, срока и порта (кластера не требует)
#
#   STAND_KANAME_BRANCH=<ветка> [STAND_KANAME_REF=<sha40>] (up, run) — служба доступа
#   ставится образом этой ветки, а не пином профиля (kacho#3312; раздел «ОБРАЗ
#   ВЕТКИ СЛУЖБЫ ДОСТУПА» ниже)
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
#                 плоскости данных хранилища нет — вид пуст, кластера блочного
#                 хранения у стенда нет (kacho#3222); окно источника полосы входа —
#                 окно стенда наборов конвейера, выведенное из его цепочки: пробы
#                 консоли регистрируют людей с одного источника (kacho#3223);
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
#
# Файл профиля кластера НЕ доказывает: его могут заменить. Решение владельца
# 2026-10-10 дословно: «По префиксу нод можешь понять тот кластер или нет, куб
# конфет мог меняться». Кластер принимается по узлам — все `<профиль>-client-…`,
# не меньше одного (scripts/client-context.sh, client_context_nodes) — в
# guard_context до первого обращения с записью и в переписи, и заново перед
# каждой фазой записи (stand_nodes): между фазами файл мог смениться.
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
# Ветки, сборкой которых могли быть опубликованы образы ревизии. Заданы явно —
# спрашиваются только они; не заданы — выводятся из истории (image_branch_candidates).
IMAGE_BRANCHES="${STAND_IMAGE_BRANCHES:-}"
IMAGE_PROBE_MAX="${STAND_IMAGE_PROBE_MAX:-20}"
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
  # Ключи доступа стенда — происхождение ЕГО консоли (kaname#677): перечень
  # выводит чарт из appBaseURL слоя, перечень цепочки, объявленный литералом
  # («никого» у prorobotech, kacho#3068), снят значением null. Близнецы — слой без снятия перечня площадки и слой без
  # вывода из консоли: первый роняет рендер двумя объявлениями, второй оставляет
  # на стенде «никого», и церемония ключа неисполнима.
  local ak
  ak='
import sys, yaml
d = yaml.safe_load(sys.stdin)
i = d["global"]["kacho"]["identity"]
k = d["kaname"]["config"]["authn"]["accessKeys"]
assert k.get("originFromConsole") is True, k
assert "origins" in k and k["origins"] is None, k
assert i["appBaseURL"] == "https://%s:20001" % i["webauthnRpId"], i
'
  if stand_overlay t3102-probe cm-ns on 20001 | python3 -c "$ak"; then
    ok "слой стенда: ключи доступа — происхождение консоли стенда, перечень площадки снят"
  else bad "слой стенда: происхождение ключей доступа не из консоли стенда"; fi
  if stand_overlay t3102-probe cm-ns on 20001 | sed '/^        origins: null$/d' | python3 -c "$ak" 2>/dev/null; then
    bad "слой без снятия перечня площадки принят разбором"; else ok "слой без снятия перечня площадки разбором отвергнут"; fi
  if stand_overlay t3102-probe cm-ns on 20001 | sed 's/^        originFromConsole: true$/        originFromConsole: false/' | python3 -c "$ak" 2>/dev/null; then
    bad "слой без вывода происхождения из консоли принят разбором"; else ok "слой без вывода происхождения из консоли разбором отвергнут"; fi
  # Издатель платформы стенда проб (kacho#3113): цепочка, чей издатель —
  # заглушка площадки (a8f60d), получает издателем происхождение консоли стенда
  # во всех семи местах; цепочка с именем разработки (dev-prod) — нет, слой
  # стенда издателя не трогает. Разбором слоя, а не подстрокой.
  local iss
  iss='
import sys, yaml
d = yaml.safe_load(sys.stdin)
o = "https://%s:20001" % d["global"]["kacho"]["identity"]["webauthnRpId"]
ks = o + "=https://kaname-internal.kacho.svc:9097/.well-known/kaname/jwks.json"
assert d["kaname"]["config"]["authn"]["tokenSigning"]["issuer"] == o, d["kaname"]
assert d["kaname"]["config"]["authn"]["accessKeys"]["originFromConsole"] is True
for c in ("api-gateway", "registry"):
    t = d[c]["tokenAcceptance"]
    assert t == {"issuers": o, "issuerKeySets": ks, "platformIssuer": o}, (c, t)
'
  local noiss
  noiss='
import sys, yaml
d = yaml.safe_load(sys.stdin)
assert "tokenSigning" not in d["kaname"]["config"]["authn"], d["kaname"]
assert "api-gateway" not in d and "registry" not in d, sorted(d)
'
  if stand_overlay t3102-probe cm-ns on 20001 "https://t3102-probe.$HOST_SUFFIX:20001" | python3 -c "$iss"; then
    ok "слой стенда: издатель платформы — происхождение консоли стенда в семи местах"
  else bad "слой стенда: издатель платформы не на происхождении консоли стенда"; fi
  if stand_overlay t3102-probe cm-ns on 20001 | python3 -c "$noiss"; then
    ok "слой стенда без издателя: чеканка и приём не тронуты"
  else bad "слой стенда без издателя тронул чеканку либо приём"; fi
  if stand_overlay t3102-probe cm-ns on 20001 "https://t3102-probe.$HOST_SUFFIX:20001" | sed '/^    platformIssuer: /d' | python3 -c "$iss" 2>/dev/null; then
    bad "слой без издателя потребителей принят разбором"; else ok "слой без издателя потребителей разбором отвергнут"; fi
  local chain_a chain_d f la ld
  chain_a=(); chain_d=()
  # Перечень слоёв читается в переменную с проверкой кода: в `for … in $(…)`
  # отказ читателя таблицы терялся бы, и ось судила бы пустую цепочку.
  la="$(bash "$DEPLOY_ROOT/tests/helm/stacks.sh" --chain a8f60d ' ')" || la=""
  ld="$(bash "$DEPLOY_ROOT/tests/helm/stacks.sh" --chain dev-prod ' ')" || ld=""
  for f in $la; do chain_a+=("$UMBRELLA/$f"); done
  for f in $ld; do chain_d+=("$UMBRELLA/$f"); done
  local rca=0 rcd=0
  chain_issuer_placeholder "${chain_a[@]}" || rca=$?
  chain_issuer_placeholder "${chain_d[@]}" || rcd=$?
  if [ "${#chain_a[@]}" -gt 0 ] && [ "${#chain_d[@]}" -gt 0 ] && [ "$rca" -eq 0 ] && [ "$rcd" -eq 1 ]; then
    ok "издатель цепочки a8f60d — заглушка площадки (слой стенда его заменит), dev-prod — нет"
  else bad "издатель цепочки: a8f60d код $rca (ждали 0, слоёв ${#chain_a[@]}), dev-prod код $rcd (ждали 1)"; fi
  # Секрет первого администратора облака выводится из рендера: ссылка
  # KANAME_BOOTSTRAP_ROOT_EMAIL → secretKeyRef. Близнец — рендер без ссылки: пусто.
  local rd; rd="$(mktemp)"
  printf '%s\n' 'kind: Deployment' 'metadata: {name: kaname}' 'spec: {template: {spec: {containers: [{name: kaname, env: [{name: KANAME_BOOTSTRAP_ROOT_EMAIL, valueFrom: {secretKeyRef: {name: stand-cloud-admin, key: email}}}]}]}}}' >"$rd"
  if [ "$(bootstrap_secret_of "$rd")" = stand-cloud-admin ]; then ok "секрет администратора облака выведен из рендера"; else bad "секрет администратора облака из рендера не выведен"; fi
  printf '%s\n' 'kind: Deployment' 'metadata: {name: kaname}' 'spec: {template: {spec: {containers: [{name: kaname, env: [{name: KANAME_BOOTSTRAP_ROOT_EMAIL, value: ""}]}]}}}' >"$rd"
  if [ -z "$(bootstrap_secret_of "$rd")" ]; then ok "рендер без ссылки на секрет — шага администратора нет"; else bad "секрет администратора выведен из рендера без ссылки"; fi
  rm -f "$rd"
  # Дверь чтения писем, которую поднимает forward (kaname#684): обе формы, отказы
  # и 502 на оборванном приёмнике — её собственной самопроверкой.
  if python3 "$HERE/stand-mailbox-door.py" --self-test >/dev/null 2>&1; then
    ok "дверь чтения писем стенда: /messages и /codes в форме приёмника службы, отказы и обрыв приёмника"
  else bad "дверь чтения писем стенда: самопроверка провалена (python3 scripts/stand-mailbox-door.py --self-test)"; fi
  # Проба документа обнаружения на внешнем входе консоли (kacho#3113): красная на
  # имени разработки, чужом имени и порту, на нуле адресов; молчит на близнеце.
  if python3 "$HERE/probe-discovery-origin.py" --self-test >/dev/null 2>&1; then
    ok "проба документа обнаружения: N из N адресов на происхождении консоли, имя разработки и ноль адресов — находки"
  else bad "проба документа обнаружения: самопроверка провалена (python3 scripts/probe-discovery-origin.py --self-test)"; fi
  # Раннер в pod стенда (kacho#3346): суд формы — на настоящем выводе runner_objects
  # молчит (с секретом администратора и без), на каждом близнеце с дефектом — краснеет.
  local ro rj twin
  ro="$(runner_objects t3102-probe browser img:1 stand-cloud-admin ui-future/e2e 'npx playwright test' v1.35.7 6 1.9.3)"
  if runner_judge t3102-probe <<<"$ro" >/dev/null &&
     runner_judge t3102-probe <<<"$(runner_objects t3102-probe newman img:1 '' deploy true v1.35.7 6 1.9.3)" >/dev/null; then
    ok "раннер: учётка, роль, привязка и pod — в пространстве стенда, роль без звезды, pod без host*, имя консоли на петлю"
  else bad "раннер: суд формы краснеет на настоящем выводе runner_objects ($(runner_judge t3102-probe <<<"$ro" | head -n 1))"; fi
  for twin in \
    '(.items[] | select(.kind == "Role")) |= (.kind = "ClusterRole")|ClusterRole' \
    '(.items[] | select(.kind == "Pod") | .spec.hostNetwork) = true|hostNetwork' \
    '(.items[] | select(.kind == "Pod") | .spec.containers[0].securityContext.privileged) = true|privileged' \
    '(.items[] | select(.kind == "Role") | .rules[0].verbs) += ["delete"]|delete' \
    '(.items[] | select(.kind == "Pod") | .spec.hostAliases[0].hostnames) = ["t3102-probe.example"]|чужое имя' \
    '(.items[] | select(.kind == "Pod") | .spec.containers[0].env[] | select(.valueFrom) | .valueFrom.secretKeyRef.optional) = false|обязательный секрет' \
    '(.items[] | select(.kind == "Pod") | .metadata.namespace) = "kacho"|пространство kacho' \
    '(.items[] | select(.kind == "Pod") | .spec.serviceAccountName) = "default"|serviceAccountName default' \
    '(.items[] | select(.kind == "RoleBinding") | .roleRef.name) = "admin"|roleRef.name чужой роли' \
    '(.items[] | select(.kind == "Pod") | .spec.nodeName) = "node-a"|nodeName'; do
    rj="$(jq "${twin%|*}" <<<"$ro" | runner_judge t3102-probe)" && bad "раннер: близнец «${twin##*|}» прошёл суд" ||
      { [ -n "$rj" ] && ok "раннер: близнец «${twin##*|}» — находка"; } || bad "раннер: близнец «${twin##*|}» — код без находки"
  done
  # Законный близнец связей: все имена раннера сменены согласованно — связи целы, суд молчит.
  local legal='(.items[].metadata.name) = "runner-x" | (.items[] | select(.kind == "RoleBinding") | .roleRef.name, .subjects[0].name) = "runner-x" | (.items[] | select(.kind == "Pod") | .spec.serviceAccountName) = "runner-x"'
  if rj="$(jq "$legal" <<<"$ro" | runner_judge t3102-probe)"; then
    ok "раннер: законный близнец (имена учётки, роли и привязки сменены согласованно) — суд молчит"
  else bad "раннер: законный близнец (согласованная смена имён) — находка: $rj"; fi
  # Забор отчётов из pod (⛔ приёмки ws#1303: путей 17 из 35, код 0): перечень pod и сверка на
  # машине. Pod — каталоги-фикстура, поток — те же тексты; обрыв — поток tar, усечённый
  # посередине (head -c), в первых CUT попытках. Вход детерминирован (seq), не случаен.
  local fx frc fo
  fx="$(mktemp -d)"
  mkdir -p "$fx/src/a/tests/newman/out/sub" "$fx/work"
  seq 1 60000 >"$fx/src/a/tests/newman/out/big.json"
  printf '{}' >"$fx/src/a/tests/newman/out/sub/small.json"
  printf 'x' >"$fx/src/a/tests/newman/out/sub/with space.json"
  # fetch_probe CUT TRIES [manifest-fail] — код runner_fetch; вывод — в $fx/fetch.out.
  fetch_probe() {
    rm -rf "$fx/out"; mkdir -p "$fx/out"; : >"$fx/cuts"
    ( RUNNER_SRC="$fx/src" RUNNER_WORK="$fx/work" RUN_RUNNER=newman STAND_RUNNER_FETCH_TRIES="$2" CUT="$1" MFAIL="${3:-}"
      runner_put() { sh -c 'cat > "$1"' put "$2"; }
      runner_exec() {
        shift
        case "$3" in
          *"tar -c"*) if [ "$(wc -l <"$fx/cuts")" -lt "$CUT" ]; then echo >>"$fx/cuts"; "$@" | head -c "${CUTB:-100000}"; else "$@"; fi ;;
          *) [ -z "$MFAIL" ] || return 1; "$@" ;;
        esac
      }
      runner_fetch t3102-probe "$fx/out" ) >"$fx/fetch.out" 2>&1
  }
  frc=0; fetch_probe 0 3 || frc=$?
  if [ "$frc" -eq 0 ] && grep -q 'доставлено 3 из 3' "$fx/fetch.out" && cmp -s "$fx/src/a/tests/newman/out/big.json" "$fx/out/a/tests/newman/out/big.json" &&
     [ -f "$fx/out/a/tests/newman/out/sub/with space.json" ]; then
    ok "забор отчётов: целый поток — код 0, «доставлено 3 из 3», файлы равны исходным"
  else bad "забор отчётов: целый поток — код $frc: $(tr '\n' ' ' <"$fx/fetch.out")"; fi
  frc=0; fetch_probe 99 3 || frc=$?
  fo="$(cat "$fx/fetch.out")"
  if [ "$frc" -ne 0 ] && [[ "$fo" == *"ОТЧЁТЫ ДОСТАВЛЕНЫ НЕПОЛНО: 0 из 3 после 3 попыток"* ]] && [[ "$fo" == *"sub/with space.json"* ]] &&
     [[ "$fo" == *"a/tests/newman/out/big.json"* ]]; then
    ok "забор отчётов: обрыв потока посередине в каждой попытке — код $frc, «доставлено неполно» с перечнем недоставленного"
  else bad "забор отчётов: обрыв потока посередине прошёл (код $frc): $(tr '\n' ' ' <<<"$fo")"; fi
  frc=0; fetch_probe 1 3 || frc=$?
  fo="$(cat "$fx/fetch.out")"
  if [ "$frc" -eq 0 ] && [[ "$fo" == *"попытка 1 из 3 — доставлено 0 из 3"*"повтор"* ]] && [[ "$fo" == *"доставлено 3 из 3"*"попытка 2 из 3"* ]]; then
    ok "забор отчётов: обрыв первой попытки — назван, повтор доставил 3 из 3"
  else bad "забор отчётов: обрыв первой попытки (код $frc): $(tr '\n' ' ' <<<"$fo")"; fi
  # Обрыв после первого файла: повтор тянет лишь два недоставленных, сверенный не трогает.
  frc=0; CUTB=350000 fetch_probe 1 3 || frc=$?
  fo="$(cat "$fx/fetch.out")"
  if [ "$frc" -eq 0 ] && [[ "$fo" == *"попытка 1 из 3 — доставлено 1 из 3"*"повтор недоставленного (2)"* ]] &&
     [ "$(tr '\0' '\n' <"$fx/work/.fetch.retry0" | sort | tr '\n' '|')" = "a/tests/newman/out/sub/small.json|a/tests/newman/out/sub/with space.json|" ] &&
     cmp -s "$fx/src/a/tests/newman/out/big.json" "$fx/out/a/tests/newman/out/big.json"; then
    ok "забор отчётов: повтор — лишь недоставленное (2 из 3), доставлено 3 из 3"
  else bad "забор отчётов: повтор недоставленного (код $frc): $(tr '\n' ' ' <<<"$fo")"; fi
  # Отчёт, которого прогон не пишет (шаблон без звезды на отсутствующий путь), — не отказ перечня.
  frc=0; ( RUNNER_SRC="$fx/src" RUNNER_WORK="$fx/work" bash -c "$RUNNER_MANIFEST_SH" manifest "$fx/src" "$fx/work" \
      'a/tests/newman/out' 'a/selection.json' 'a/playwright-report' >"$fx/man.out" 2>&1 ) || frc=$?
  if [ "$frc" -eq 0 ] && [ "$(wc -l <"$fx/man.out")" -eq 3 ]; then
    ok "перечень отчётов: отсутствующие selection.json и отчёт html — не в перечне, не отказ (3 файла)"
  else bad "перечень отчётов: отсутствующий путь шаблона — код $frc: $(tr '\n' ' ' <"$fx/man.out")"; fi
  frc=0; fetch_probe 0 3 manifest-fail || frc=$?
  if [ "$frc" -ne 0 ] && grep -q 'перечень отчётов в pod не собран' "$fx/fetch.out"; then
    ok "забор отчётов: перечень pod не собран — код $frc, а не «путей 0»"
  else bad "забор отчётов: перечень pod не собран — код $frc: $(tr '\n' ' ' <"$fx/fetch.out")"; fi
  rm -rf "$fx"
  # Шаг условия машины (kacho#3348): просьба pod исполняется командой строки ведомости с
  # исполнителем machine; инъекция — строка убрана (либо исполнитель pod, либо аргумент с
  # «-») → 125 без исполнения: проба прочтёт «условие не создано», а не зелёное.
  local cx crc co
  cx="$(mktemp -d)"
  mkdir -p "$cx/work/cond" "$cx/repo/s" "$cx/stand"
  printf '#!/usr/bin/env bash\necho "ran ns=$KACHO_STAND_NS state=$KACHO_STAND_COND_STATE args=$*"\n' >"$cx/repo/s/cond.sh"
  # cond_probe ВЕДОМОСТЬ ПРОСЬБА — код ответа pod; вывод ответа — в $cx/out.
  cond_probe() {
    rm -f "$cx/work/cond/"*; printf '%b\n' "$2" >"$cx/work/cond/r1.req"
    printf '%b\n' "$1" >"$cx/ledger"
    ( RUNNER_WORK="$cx/work" REPO_ROOT="$cx/repo" WORK_ROOT="$cx/stand" STAND_CONDITIONS="$cx/ledger"
      runner_exec() { shift; "$@"; }
      runner_put() { sh -c 'cat > "$1.tmp" && mv "$1.tmp" "$1"' put "$2"; }
      runner_conds t3102-probe ) >"$cx/log" 2>&1
    cat "$cx/work/cond/r1.out" >"$cx/out" 2>/dev/null
    cat "$cx/work/cond/r1.rc" 2>/dev/null || echo none
  }
  crc="$(cond_probe 'rot\tmachine\twrite: x\ts/cond.sh\tr' 'rot\twrite\tset\tK2,K1\tkacho-wrap-rotate-ab12')"; co="$(cat "$cx/out")"
  if [ "$crc" = 0 ] && [ "$co" = "ran ns=t3102-probe state=$cx/stand/t3102-probe/cond/rot args=write set K2,K1 kacho-wrap-rotate-ab12" ] &&
     [ ! -e "$cx/work/cond/r1.req" ]; then
    ok "шаг условия машины: строка ведомости machine — исполнена своим пространством и каталогом, ответ 0 в pod"
  else bad "шаг условия машины: строка machine — код $crc, ответ «$co» ($(tr '\n' ' ' <"$cx/log"))"; fi
  for twin in 'other\tmachine\tw\ts/cond.sh\tr|строка убрана' 'rot\tpod\tw\ts/cond.sh\tr|исполнитель pod'; do
    crc="$(cond_probe "${twin%|*}" 'rot\twrite\tset\tK2,K1\tk')"; co="$(cat "$cx/out")"
    if [ "$crc" = 125 ] && [[ "$co" == *"не объявлен в ведомости"* ]]; then
      ok "шаг условия машины: инъекция «${twin##*|}» — 125 без исполнения (условие не создано, не зелёное)"
    else bad "шаг условия машины: инъекция «${twin##*|}» — код $crc, ответ «$co»"; fi
  done
  crc="$(cond_probe 'rot\tmachine\tw\ts/cond.sh\tr' 'rot\twrite\tset\tK2\t--ns\tkacho')"; co="$(cat "$cx/out")"
  if [ "$crc" = 125 ] && [[ "$co" == *"вне формы слова"* ]] && [[ "$co" != *ran* ]]; then
    ok "шаг условия машины: аргумент «--ns» из просьбы pod — 125 без исполнения"
  else bad "шаг условия машины: аргумент «--ns» — код $crc, ответ «$co»"; fi
  rm -rf "$cx"
  co="$(STAND_CONDITIONS="$HERE/stand-conditions.tsv" cond_command wrap-rotate)"
  if [ -f "$REPO_ROOT/$co" ] && [ "$co" = deploy/scripts/stand-second-factor-wrap-rotate.sh ]; then
    ok "ведомость условий: wrap-rotate — шаг машины, команда есть в дереве"
  else bad "ведомость условий: wrap-rotate не шаг машины либо команды «$co» нет"; fi
  if jq -e '[.items[] | select(.kind == "Pod") | .spec.containers[0].env[] | select(.name == "KACHO_STAND_COND_DIR") | .value] == ["/work/cond"]' \
      <<<"$(runner_objects t3102-probe browser img:1 '' ui-future/e2e true v1.35.7 6 1.9.3)" >/dev/null &&
     [[ "$RUNNER_POD_SH" == *'mkdir -p "$HOME" "$KACHO_STAND_NS_WORKDIR" "$KACHO_STAND_COND_DIR"'* ]]; then
    ok "раннер: каталог просьб машине /work/cond — в окружении pod и заведён текстом pod"
  else bad "раннер: каталога просьб машине нет в окружении либо тексте pod"; fi
  if jq -e '[.items[] | select(.kind == "Pod") | .spec.containers[0].env[] | select(.name | startswith("KACHO_CLOUD_ADMIN"))] | length == 0' \
      <<<"$(runner_objects t3102-probe newman img:1 '' deploy true v1.35.7 6 1.9.3)" >/dev/null; then
    ok "раннер: цепочка без администратора — его окружения в pod нет"; else bad "раннер: окружение администратора без секрета"; fi
  local vers
  if vers="$(runner_versions HEAD 2>&1)" && [[ "$vers" =~ ^[0-9]+\ [0-9]+\ [0-9]+\.[0-9]+\.[0-9]+\ [0-9]+\.[0-9]+\.[0-9]+\ [0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    ok "раннер: версии node, newman, grpcurl, playwright, buf выведены из дерева HEAD ($vers)"
  else bad "раннер: версии из дерева HEAD не выведены: $vers"; fi
  # buf раннера newman (kacho#3346: в pod не было buf — проба ban #6 и наборы, собирающие
  # дескрипторы `buf build proto`, не исполнялись). Версия — ключ `version:` шага
  # bufbuild/buf-setup-action; шаг кончается на следующем «- », чужой `version:` не берётся.
  local bstep bv brc q="'"
  bstep='      - uses: bufbuild/buf-setup-action@3e33fea8596569a62fcaa33eb3880b6ac7098941  # v1.9.0
        with:
          version: 1.72.0
          github_token: x
      - uses: azure/setup-kubectl@829323503d1be3d00ca8346e5391ca0b07a9ab0d  # v5
        with:
          version: v1.35.0'
  for twin in "0|1.72.0|шаг конвейера" "0|1.72.0|версия в кавычках" "1||ключа version у шага нет (version соседнего шага не берётся)" \
              "1||второй шаг buf другой версии" "1||версия не X.Y.Z"; do
    IFS='|' read -r want wv what <<<"$twin"
    case "$what" in
      "шаг конвейера") bv="$bstep" ;;
      "версия в кавычках") bv="${bstep/version: 1.72.0/version: ${q}1.72.0${q}}" ;;
      "ключа version"*) bv="${bstep/version: 1.72.0/}" ;;
      "второй шаг"*) bv="$bstep"$'\n'"${bstep/1.72.0/1.71.0}" ;;
      "версия не"*) bv="${bstep/1.72.0/1.72}" ;;
    esac
    brc=0; bv="$(buf_version <<<"$bv" 2>/dev/null)" || brc=$?
    if [ "$brc" -eq "$want" ] && [ "$bv" = "$wv" ]; then ok "раннер: версия buf — $what: код $brc «$bv»"
    else bad "раннер: версия buf — $what: код $brc «$bv», ждали $want «$wv»"; fi
  done
  if [ "$(runner_objects t3102-probe newman img:1 '' deploy true v1.35.7 6 1.9.3 abc 1.72.0 |
          jq -r '.items[] | select(.kind == "Pod") | .spec.containers[0].env[] | select(.name == "RUNNER_BUF") | .value')" = 1.72.0 ]; then
    ok "раннер: версия buf — в окружении pod (RUNNER_BUF)"; else bad "раннер: RUNNER_BUF нет в окружении pod"; fi
  # Установка — функцией runner_buf ИЗ ТЕКСТА POD (первая строка RUNNER_POD_SH), загрузка
  # подменена: выпуск — каталог-фикстура. Не поставлен — код 1, имя инструмента, в КАТАЛОГЕ пусто.
  local bx bo
  bx="$(mktemp -d)"
  mkdir -p "$bx/rel" "$bx/stub" "$bx/bin"
  # shellcheck disable=SC2016 # текст заглушки — её собственный
  printf '#!/bin/sh\necho "${BUF_STUB_SAYS:-1.72.0}"\n' >"$bx/rel/buf-Linux-$(uname -m)"
  ( cd "$bx/rel" && sha256sum "buf-Linux-$(uname -m)" ) >"$bx/rel/sha256.good"
  sed 's/^[0-9a-f]*/'"$(printf '0%.0s' $(seq 64))"'/' "$bx/rel/sha256.good" >"$bx/rel/sha256.bad"
  # shellcheck disable=SC2016 # текст заглушки — её собственный
  printf '%s\n' '#!/bin/bash' 'o="" u=""' \
    'while [ $# -gt 0 ]; do case "$1" in -o) o="$2"; shift 2 ;; https://*) u="$1"; shift ;; *) shift ;; esac; done' \
    'f="${u##*/}"; [ "$f" != sha256.txt ] || f="$BUF_STUB_SUMS"' \
    '[ -z "${BUF_STUB_GONE:-}" ] || [ "$f" = "$BUF_STUB_SUMS" ] || exit 22' \
    '[ -f "$BUF_STUB_REL/$f" ] || exit 22' \
    'if [ -n "$o" ]; then cat "$BUF_STUB_REL/$f" >"$o"; else cat "$BUF_STUB_REL/$f"; fi' >"$bx/stub/curl"
  chmod +x "$bx/stub/curl"
  # buf_probe ВЕРСИЯ [ОКРУЖЕНИЕ…] — код runner_buf из текста pod; вывод — $bx/out.
  buf_probe() {
    rm -f "$bx/bin/buf"
    ( eval "$(head -n 1 <<<"$RUNNER_POD_SH")"
      export PATH="$bx/stub:$PATH" BUF_STUB_REL="$bx/rel" BUF_STUB_SUMS=sha256.good
      local e; for e in "${@:2}"; do export "${e?}"; done
      runner_buf "$1" "$bx/bin" ) >"$bx/out" 2>&1
  }
  brc=0; buf_probe 1.72.0 || brc=$?
  if [ "$brc" -eq 0 ] && [ "$("$bx/bin/buf" --version 2>&1)" = 1.72.0 ] && grep -qx 'buf 1.72.0' "$bx/out"; then
    ok "раннер: buf выпуска с сошедшейся суммой — поставлен, называет 1.72.0"
  else bad "раннер: buf с сошедшейся суммой — код $brc: $(tr '\n' ' ' <"$bx/out")"; fi
  for twin in "BUF_STUB_SUMS=sha256.bad|сумма не сошлась" "BUF_STUB_GONE=1|выпуска нет" "BUF_STUB_SAYS=1.71.0|бинарь называет другую версию"; do
    brc=0; buf_probe 1.72.0 "${twin%|*}" || brc=$?
    bo="$(tr '\n' ' ' <"$bx/out")"
    if [ "$brc" -ne 0 ] && [[ "$bo" == *"buf 1.72.0: "* ]] && [ ! -e "$bx/bin/buf" ]; then ok "раннер: buf — ${twin##*|}: код $brc, «$bo», в каталоге пусто"
    else bad "раннер: buf — ${twin##*|}: код $brc, «$bo», в каталоге $(ls "$bx/bin")"; fi
  done
  brc=0; buf_probe 1.72 || brc=$?
  if [ "$brc" -ne 0 ] && grep -q '^buf: версия конвейера' "$bx/out" && [ ! -e "$bx/bin/buf" ]; then ok "раннер: buf — версия не X.Y.Z: код $brc, загрузки нет"
  else bad "раннер: buf — версия не X.Y.Z: код $brc, $(tr '\n' ' ' <"$bx/out")"; fi
  rm -rf "$bx"
  # Вызов в тексте pod: вид newman ставит buf, неуспех — «не выполнилось» с именем инструмента.
  # shellcheck disable=SC2016 # образец — дословный текст pod
  if [[ "$RUNNER_POD_SH" == *'runner_buf "$RUNNER_BUF" /usr/local/bin > /work/buf.log 2>&1 || { tail -n 3 /work/buf.log; unmet "buf $RUNNER_BUF"; }'* ]]; then
    ok "раннер: текст pod вида newman ставит buf, неуспех — unmet «buf <версия>» (125)"
  else bad "раннер: текст pod не ставит buf либо неуспех не назван unmet"; fi
  local prc=0
  ( env -u KUBERNETES_SERVICE_HOST bash "$HERE/stand-ns.sh" pod-forward t3102-probe ) >/dev/null 2>&1 || prc=$?
  if [ "$prc" -eq 2 ]; then ok "pod-forward вне pod — отказ 2 до обращения к кластеру"; else bad "pod-forward вне pod: код $prc, ждали 2"; fi
  # Инструменты подъёма (kacho#3353): собирает pod кластера, `go` на машине — ловушка, которая
  # пишет вызов в файл и отвечает 97. Pod — каталог-фикстура (бинари, суммы, код), обращения к
  # кластеру подменены; исходники идут тем же потоком tar|gzip, что в pod.
  local tx trc; tx="$(mktemp -d)"
  mkdir -p "$tx/bin"
  printf '#!/bin/sh\necho "go $*" >> "%s/trap"\nexit 97\n' "$tx" >"$tx/bin/go" && chmod +x "$tx/bin/go"
  : >"$tx/trap"
  # tool_probe РЕЖИМ — код build_tool на фикстуре pod; вывод — $tx/out.
  tool_probe() {
    rm -rf "$tx/work" "$tx/st"; mkdir -p "$tx/work/out" "$tx/st"
    printf 'standns-bin\n' >"$tx/work/out/stand-ns"; printf 'producer-bin\n' >"$tx/work/out/module-manifests-configmap"
    printf '[]\n' >"$tx/work/out/godeps.json"; printf 'names-bin\n' >"$tx/work/out/product-names"
    ( cd "$tx/work/out" && sha256sum -- * >"$tx/work/sums" )
    case "$1" in
      ok) echo 0 >"$tx/work/.rc" ;;
      build-fail) echo 1 >"$tx/work/.rc"; echo "сборка: пакет не найден" >"$tx/work/build.log" ;;
      sum-bad) echo 0 >"$tx/work/.rc"; sed -i 's/^[0-9a-f]\{4\}/0000/' "$tx/work/sums" ;;
    esac
    # shellcheck disable=SC2030,SC2031 # подмены живут лишь в подоболочке пробы
    ( PATH="$tx/bin:$PATH" TOOL_WORK="$tx/work" TOOL_WAIT=10
      tool_apply() { cat >/dev/null; }
      tool_ready() { return 0; }
      tool_phase() { echo Running; }
      # kubectl — по форме настоящего: exec без -i не передаёт stdin команде pod (tool_put обязан
      # нести -i), забор (tool_exec) идёт без него.
      kubectl() {
        [ "$1 $2" != "auth can-i" ] || { echo yes; return 0; }
        [ "$1" = -n ] && [ "$3" = exec ] || return 0
        local pass=0; shift 3
        while [ "$1" != -- ]; do [ "$1" != -i ] || pass=1; shift; done; shift
        if [ "$pass" = 1 ]; then "$@"; else "$@" </dev/null; fi
      }
      stand_nodes() { :; }
      sleep() { :; }
      # Близнецам дерево целиком не нужно: перечень — один go.mod (законный случай несёт всё).
      [ "$1" = ok ] || tool_lists() { printf 'go.mod\n' >"$1/tracked.txt" && : >"$1/untracked.txt"; }
      build_tool "$tx/st" t3102-probe 3102 && [ -x "$STAND_NS_BIN" ] && [ -x "$MM_PRODUCER" ] ) >"$tx/out" 2>&1
  }
  trc=0; tool_probe ok || trc=$?
  # Перечень в pod сверяется безусловно: сверка, исполняемая лишь при наличии файла, молчала бы без него.
  local tsame=0; cmp -s "$tx/work/tracked.txt" <(git -C "$REPO_ROOT" ls-files) || tsame=$?
  if [ "$trc" -eq 0 ] && [ "$tsame" -eq 0 ] && [ "$(cat "$tx/st/bin/stand-ns")" = standns-bin ] &&
     [ "$(cat "$tx/st/bin/module-manifests-configmap")" = producer-bin ] && [ "$(cat "$tx/st/bin/godeps.json")" = '[]' ] &&
     [ "$(cat "$tx/st/bin/product-names")" = names-bin ] && [ ! -s "$tx/trap" ] && [ -s "$tx/work/src/go.mod" ] && [ -e "$tx/work/untracked.txt" ]; then
    ok "инструменты подъёма: собраны в pod кластера, суммы сверены, go на машине (ловушка) не звался, дерево и перечни git ls-files — в pod, запись замыкания забрана"
  else bad "инструменты подъёма: код $trc, ловушка «$(tr '\n' ' ' <"$tx/trap")», $(tail -n 2 "$tx/out" | tr '\n' ' ')"; fi
  for twin in "build-fail|сборка в pod отказала|не собраны" "sum-bad|сумма забранного не сумма pod|не доставлен целым"; do
    trc=0; tool_probe "${twin%%|*}" || trc=$?
    if [ "$trc" -ne 0 ] && [ ! -e "$tx/st/bin/stand-ns" ] && grep -q "${twin##*|}" "$tx/out" && [ ! -s "$tx/trap" ]; then
      ok "инструменты подъёма: близнец «$(cut -d'|' -f2 <<<"$twin")» — отказ $trc без бинаря"
    else bad "инструменты подъёма: близнец «$(cut -d'|' -f2 <<<"$twin")» — код $trc: $(tail -n 2 "$tx/out" | tr '\n' ' ')"; fi
  done
  printf 'module x\n\ngo 1.26.0\n\ntoolchain go1.26.9\n' >"$tx/a.mod"; printf 'module x\n\ngo 1.26.0\n' >"$tx/b.mod"; printf 'module x\n' >"$tx/c.mod"
  if [ "$(tool_go_version "$tx/a.mod")" = 1.26.9 ] && [ "$(tool_go_version "$tx/b.mod")" = 1.26.0 ] && ! tool_go_version "$tx/c.mod" >/dev/null 2>&1; then
    ok "инструменты подъёма: версия Go — toolchain go.mod, иначе go, нет обеих — отказ"
  else bad "инструменты подъёма: версия Go из go.mod выведена неверно"; fi
  local to; to="$(tool_objects t3102-tool-abc123 3102 t3102-probe mirror.gcr.io/library/golang:1.26.9 amd64 2999-01-01T00:00:00Z)"
  if jq -e --arg ls "$LABEL_STAND" '
      ([.items[].kind] == ["Namespace", "Pod"])
      and (.items[0].metadata.labels[$ls] == "test") and (.items[0].metadata.labels["pod-security.kubernetes.io/enforce"] == "restricted")
      and (.items[1].metadata.namespace == "t3102-tool-abc123")
      and (.items[1].spec | .automountServiceAccountToken == false and .securityContext.runAsNonRoot == true
           and ([.volumes[] | select(has("hostPath"))] | length == 0) and (has("hostNetwork") | not))
      and (.items[1].spec.containers[0] | .securityContext.capabilities.drop == ["ALL"] and .securityContext.allowPrivilegeEscalation == false
           and (.resources.limits.memory != null) and ([.env[] | select(.name == "GOTOOLCHAIN") | .value] == ["local"]))' <<<"$to" >/dev/null; then
    ok "инструменты подъёма: пространство помечено стендом проб, pod restricted — не root, без токена учётки, без возможностей, с пределом памяти"
  else bad "инструменты подъёма: форма пространства или pod сборки"; fi
  rm -rf "$tx"
  printf 'самопроверка stand-ns: прошло %d, провалено %d\n' "$pass" "$fail"
  [ "$fail" -eq 0 ]
}

# ── КЛАСТЕР ──────────────────────────────────────────────────────────────────

need_tools() {
  local t tools="kubectl helm python3 openssl jq curl git tar gzip sha256sum"
  # Go на машине не нужен: инструменты подъёма собирает кластер (build_tool, kacho#3353).
  # В pod раннера исполняется лишь forward: ему не нужны helm и git.
  [ "$STAND_IN_POD" != 1 ] || tools="kubectl python3 openssl curl"
  for t in $tools; do
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
STAND_CTX="" STAND_CTX_FILE="" STAND_IN_POD=0

# kube_argv — argv kubectl без обёртки (для nohup и для строки исполнителя записи):
# на машине — файл профиля и контекст -client, в pod раннера — учётка pod.
kube_argv() {
  if [ "$STAND_IN_POD" = 1 ]; then KUBE_ARGV=("$(type -P kubectl)")
  else KUBE_ARGV=("$(type -P kubectl)" --kubeconfig "$STAND_KUBECONFIG" --context "$STAND_CTX"); fi
}

kubectl() {
  [ -n "$STAND_CTX" ] || { warn "kubectl до выбора контекста — отказ (guard_context не пройден)"; return 2; }
  if [ "$STAND_IN_POD" = 1 ]; then command kubectl "$@"; return; fi
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
  [ "$STAND_IN_POD" != 1 ] || die "в pod раннера контекст выбирает pod_guard — guard_context не зовётся" 2
  local file ctx
  client_context_pick || exit 2
  file="$CC_FILE" ctx="$CC_CTX"
  # Всё, что судится по файлу, — первым (адрес контекста против объявленного);
  # затем кластер — узлами: файл профиля его не доказывает.
  client_context_server || exit 2
  client_context_profile || exit 2
  client_context_nodes || exit 2
  export STAND_PROFILE="$CC_PROFILE"
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

# stand_nodes — кластер заново доказывается узлами перед фазой записи: файл
# профиля, которым говорит каждый kubectl и helm, мог смениться с прошлой фазы.
stand_nodes() {
  CC_FILE="$STAND_KUBECONFIG" CC_CTX="$STAND_CTX"
  client_context_nodes >/dev/null || die "кластер перед фазой записи «$1» не доказан узлами (выше) — запись не начинается" 2
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

# ── ИНСТРУМЕНТЫ ПОДЪЁМА — СБОРКОЙ ВО ВНЕШНЕМ КЛАСТЕРЕ (kacho#3353) ─────────────
#
# Решение владельца 2026-10-22 (kacho-workspace#1301) дословно: «запиши в рул что бы не
# использовал мою локальную тачку. использовать только в одном случае если доступа к
# кластеру нету». Подъёму нужен Go четырежды: перенос рендера (tools/standns), производитель
# манифестов модулей (tools/modulemanifests, цель module-manifests-configmap получает его
# готовым через MODULE_MANIFESTS_PRODUCER), читатель имён частей продукта
# (tools/productnames, lib/product-names.sh и все шаги подъёма получают его готовым через
# KACHO_PRODUCT_NAMES_BIN) и замыкание пакетов образов go (`go list -deps`, суд входов
# ref_guard — `stand-ns inputs -godeps`). `go` на машине не запускается: всё это
# делает короткий pod кластера, на машине — kubectl, git, tar, gzip и сверка сумм.
#
#   пространство  своё, `t<задача>-tool-<6 hex>`: пространства стенда ещё нет (фаза 1 подъёма
#                 в него не пишет), рабочий стенд не трогается. Помечено как стенд проб
#                 (kacho.io/stand=test, задача, срок TOOL_TTL_MIN мин) — перепись его видит,
#                 `census --expired` снимает; снимается сразу после забора и ловушкой подъёма;
#   образ         golang версии toolchain (иначе go) go.mod рабочей копии — её же ставит
#                 конвейер (setup-go, go-version-file: go.mod), зеркалом Docker Hub; GOTOOLCHAIN=local;
#   pod           Pod Security restricted: не root, токен учётки не монтируется, возможности
#                 сброшены, предел памяти; срок — STAND_TOOL_DEADLINE;
#   исходники     файлы рабочей копии — отслеживаемые и новые неигнорируемые, ровно перечни
#                 `git ls-files` машины (их же читает суд входов), потоком tar|gzip; сами
#                 перечни — файлами рядом (в pod нет git);
#   сборка        CGO_ENABLED=0, GOOS/GOARCH машины, -trimpath; модули — из сети pod;
#   замыкание     `stand-ns godeps` над тем же деревом и перечнями — запись godeps.json: её
#                 читает `stand-ns inputs -godeps` на машине вместо `go list`;
#   забор         каждый бинарь gzip-потоком; сумма посчитана в pod ДО передачи и сверена на
#                 машине; не сошлась — повтор (STAND_TOOL_FETCH_TRIES), затем отказ без бинаря.
TOOL_POD=stand-tool
TOOL_WORK=/work
TOOL_DEADLINE="${STAND_TOOL_DEADLINE:-1800}"
TOOL_WAIT="${STAND_TOOL_WAIT:-900}"
TOOL_TTL_MIN=60
TOOL_NS=""
# «имя=пакет» — что собирается и под каким именем ложится в каталог bin стенда.
STAND_TOOLS="stand-ns=./tools/standns/cmd/stand-ns module-manifests-configmap=./tools/modulemanifests/cmd/module-manifests-configmap product-names=./tools/productnames/cmd/product-names"
MM_PRODUCER="" STAND_GODEPS=""

# Текст pod сборки: ждёт исходники (метка .go), собирает STAND_TOOLS, пишет суммы и код в
# /work/.rc и ждёт снятия пространства (срок pod — activeDeadlineSeconds).
read -r -d '' TOOL_POD_SH <<'POD'
set -uo pipefail
w="$TOOL_WORK"
mkdir -p "$w/src" "$w/out" "$w/go" "$w/cache" || exit 1
until [ -e "$w/.go" ]; do sleep 1; done
( cd "$w/src" && go version && for t in $STAND_TOOLS; do
    go build -trimpath -o "$w/out/${t%%=*}" "${t#*=}" || exit 1
  done &&
  # Запись замыкания — тем же инструментом, собранным под узел pod (бинарь машины — под машину).
  env -u GOOS -u GOARCH go build -o "$w/stand-ns-pod" ./tools/standns/cmd/stand-ns &&
  "$w/stand-ns-pod" godeps -root "$w/src" -tracked "$w/tracked.txt" -untracked "$w/untracked.txt" > "$w/out/godeps.json"
) > "$w/build.log" 2>&1
rc=$?
[ "$rc" -ne 0 ] || ( cd "$w/out" && sha256sum -- * > "$w/sums" ) || rc=1
echo "$rc" > "$w/.rc.tmp" && mv "$w/.rc.tmp" "$w/.rc"
exec sleep infinity
POD

# tool_go_version GO.MOD — версия Go сборки: строка toolchain, иначе go; не прочитана — код 1.
tool_go_version() {
  local v
  v="$(sed -n 's/^toolchain go\([0-9][0-9.]*\)$/\1/p' "$1" | head -n 1)"
  [ -n "$v" ] || v="$(sed -n 's/^go \([0-9][0-9.]*\)$/\1/p' "$1" | head -n 1)"
  [[ "$v" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]] || { warn "версия Go в $1 не прочитана («$v»)"; return 1; }
  printf '%s\n' "$v"
}

# tool_arch — GOARCH машины: бинарь исполняется здесь, собирается там.
tool_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) warn "архитектура машины $(uname -m) — GOARCH не выводится"; return 1 ;;
  esac
}

# tool_objects NS ЗАДАЧА СТЕНД ОБРАЗ GOARCH СРОК — пространство и pod сборки (список JSON).
tool_objects() {
  jq -n --arg ns "$1" --arg task "$2" --arg stand "$3" --arg image "$4" --arg arch "$5" --arg exp "$6" \
    --arg name "$TOOL_POD" --arg sh "$TOOL_POD_SH" --arg tools "$STAND_TOOLS" --arg w "$TOOL_WORK" \
    --arg ls "$LABEL_STAND" --arg lt "$LABEL_TASK" --arg lsn "$LABEL_STAND_NS" --arg ae "$ANN_EXPIRES" \
    --argjson deadline "$TOOL_DEADLINE" '
    def env($n; $v): {name: $n, value: $v};
    {apiVersion: "v1", kind: "List", items: [
      {apiVersion: "v1", kind: "Namespace", metadata: {name: $ns,
        labels: {($ls): "test", ($lt): $task, ($lsn): $stand,
                 "pod-security.kubernetes.io/enforce": "restricted", "pod-security.kubernetes.io/enforce-version": "latest"},
        annotations: {($ae): $exp}}},
      {apiVersion: "v1", kind: "Pod", metadata: {name: $name, namespace: $ns, labels: {($ls): "test", "app.kubernetes.io/name": $name}},
       spec: {restartPolicy: "Never", activeDeadlineSeconds: $deadline, automountServiceAccountToken: false,
         enableServiceLinks: false, terminationGracePeriodSeconds: 5,
         securityContext: {runAsNonRoot: true, runAsUser: 65532, runAsGroup: 65532, fsGroup: 65532, seccompProfile: {type: "RuntimeDefault"}},
         containers: [{name: "build", image: $image, command: ["bash", "-c", $sh], workingDir: $w,
           env: [env("TOOL_WORK"; $w), env("STAND_TOOLS"; $tools), env("HOME"; $w), env("GOPATH"; ($w + "/go")),
                 env("GOCACHE"; ($w + "/cache")), env("GOTOOLCHAIN"; "local"), env("GOFLAGS"; "-mod=readonly"),
                 env("CGO_ENABLED"; "0"), env("GOOS"; "linux"), env("GOARCH"; $arch)],
           resources: {requests: {cpu: "1", memory: "1Gi"}, limits: {memory: "4Gi"}},
           securityContext: {allowPrivilegeEscalation: false, capabilities: {drop: ["ALL"]}},
           volumeMounts: [{name: "work", mountPath: $w}]}],
         volumes: [{name: "work", emptyDir: {sizeLimit: "6Gi"}}]}}]}'
}

# tool_lists DIR — перечни путей рабочей копии, которые читает суд входов (`stand-ns inputs`):
# DIR/tracked.txt (`git ls-files`) и DIR/untracked.txt (новые неигнорируемые).
tool_lists() {
  git -C "$REPO_ROOT" ls-files >"$1/tracked.txt" &&
    git -C "$REPO_ROOT" ls-files --others --exclude-standard >"$1/untracked.txt"
}

# tool_sources DIR — поток tar существующих файлов перечней DIR (состав — шапка раздела).
tool_sources() {
  cat "$1/tracked.txt" "$1/untracked.txt" |
    while IFS= read -r f; do [ -f "$REPO_ROOT/$f" ] && [ ! -L "$REPO_ROOT/$f" ] && printf '%s\0' "$f"; done |
    tar -c -C "$REPO_ROOT" --null --verbatim-files-from -T -
}

# Обращения к кластеру сборки — функциями: самопроверка подменяет их фикстурой.
# tool_put — команда pod, читающая stdin машины (поток исходников, перечни): с -i, иначе
# stdin pod пуст. tool_exec — без -i: на exec -i с пустым stdin вывод pod приходит усечённым
# (замер kacho#3353 на внешнем кластере: три забора бинаря подряд — три разные суммы).
tool_exec()  { local ns="$1"; shift; kubectl -n "$ns" exec "$TOOL_POD" -c build -- "$@"; }
tool_put()   { local ns="$1"; shift; kubectl -n "$ns" exec -i "$TOOL_POD" -c build -- "$@"; }
tool_apply() { kubectl apply -f - >/dev/null; }
tool_ready() {
  local ns="$1" sa=0
  # Учётка default пространства заводится контроллером не сразу; без неё pod не допускается.
  for _ in $(seq 1 30); do kubectl -n "$ns" get serviceaccount default -o name --request-timeout=30s >/dev/null 2>&1 && { sa=1; break; }; sleep 2; done
  [ "$sa" = 1 ] || { warn "учётка default пространства $ns не заведена за 60 с"; return 1; }
  jq '.items[1]' <<<"$2" | tool_apply || { warn "pod сборки не применён"; return 1; }
  kubectl -n "$ns" wait --for=condition=Ready "pod/$TOOL_POD" --timeout="${STAND_TOOL_READY_WAIT:-600}s" >/dev/null || {
    warn "pod сборки не готов: $(kubectl -n "$ns" get pod "$TOOL_POD" -o jsonpath='{.status.phase} {range .status.containerStatuses[*]}{.state}{end}' 2>/dev/null | cut -c1-300)"
    return 1; }
}
tool_phase() { kubectl -n "$1" get pod "$TOOL_POD" -o jsonpath='{.status.phase}' --request-timeout=30s 2>/dev/null; }
tool_drop() {
  [ -n "$TOOL_NS" ] || return 0
  kubectl delete namespace "$TOOL_NS" --ignore-not-found --wait=true --timeout=180s >/dev/null 2>&1 ||
    warn "пространство сборки $TOOL_NS не снято за 180 с — снимет census --expired по сроку"
  TOOL_NS=""
}

# build_tool DIR NS ЗАДАЧА — инструменты подъёма в DIR/bin (снимается вместе с рабочим
# каталогом стенда), собранные во внешнем кластере (раздел выше). Код 1 — не собраны.
build_tool() {
  local out="$1/bin" stand="$2" task="$3" ver arch image tns objs i rc="" phase t name want got try
  mkdir -p "$out" || return 1
  ver="$(tool_go_version "$REPO_ROOT/go.mod")" || return 1
  arch="$(tool_arch)" || return 1
  image="mirror.gcr.io/library/golang:$ver"
  tns="t$task-tool-$(printf '%s %s %s' "$stand" "$$" "$(date +%s%N)" | sha256sum | cut -c1-6)"
  ns_valid "$tns" >/dev/null || { warn "имя пространства сборки «$tns» не по правилу"; return 1; }
  for i in "create namespaces" "delete namespaces" "create pods -n $tns" "create pods --subresource=exec -n $tns"; do
    # shellcheck disable=SC2086 # «глагол ресурс [-n ns]» — разбор по словам и нужен
    [ "$(kubectl auth can-i $i --request-timeout=30s 2>/dev/null)" = yes ] || { warn "у учётки нет права «$i» — собрать инструменты негде"; return 1; }
  done
  stand_nodes "сборка инструментов подъёма"
  objs="$(tool_objects "$tns" "$task" "$stand" "$image" "$arch" "$(date -u -d "+$TOOL_TTL_MIN minutes" +%Y-%m-%dT%H:%M:%SZ)")" ||
    { warn "объекты сборки не построены"; return 1; }
  TOOL_NS="$tns"
  log "инструменты подъёма: сборка во внешнем кластере — пространство $tns, образ $image, GOARCH=$arch (go на машине не запускается)"
  jq '.items[0]' <<<"$objs" | tool_apply || { warn "пространство сборки $tns не заведено"; tool_drop; return 1; }
  tool_ready "$tns" "$objs" || { tool_drop; return 1; }
  tool_lists "$out" || { warn "перечни путей рабочей копии не прочитаны"; tool_drop; return 1; }
  for try in 1 2 3; do
    if tool_sources "$out" | gzip -1 | tool_put "$tns" sh -c 'rm -rf "$1/src" && mkdir -p "$1/src" && tar -xz -C "$1/src"' put "$TOOL_WORK" &&
       tool_put "$tns" sh -c 'cat > "$1"' put "$TOOL_WORK/tracked.txt" <"$out/tracked.txt" &&
       tool_put "$tns" sh -c 'cat > "$1"' put "$TOOL_WORK/untracked.txt" <"$out/untracked.txt"; then
      break
    fi
    warn "исходники в pod сборки не доставлены (попытка $try из 3)"
    [ "$try" -lt 3 ] || { tool_drop; return 1; }
  done
  tool_exec "$tns" touch "$TOOL_WORK/.go" </dev/null || { warn "pod сборки не принял сигнал старта"; tool_drop; return 1; }
  for i in $(seq 1 $(( TOOL_WAIT / 5 ))); do
    rc="$(tool_exec "$tns" cat "$TOOL_WORK/.rc" </dev/null 2>/dev/null)"
    [[ "$rc" =~ ^[0-9]+$ ]] && break
    rc=""
    phase="$(tool_phase "$tns")"
    case "$phase" in Running|Pending|"") ;; *) warn "pod сборки вышел из работы (фаза «$phase»)"; break ;; esac
    sleep 5
  done
  if [ "$rc" != 0 ]; then
    tool_exec "$tns" tail -n 30 "$TOOL_WORK/build.log" </dev/null >&2 2>/dev/null
    warn "инструменты подъёма в кластере не собраны (код «${rc:-нет за $TOOL_WAIT с}», хвост журнала сборки выше)"
    tool_drop; return 1
  fi
  local sums
  sums="$(tool_exec "$tns" cat "$TOOL_WORK/sums" </dev/null)" || { warn "суммы сборки из pod не прочитаны"; tool_drop; return 1; }
  for t in $STAND_TOOLS godeps.json=; do
    name="${t%%=*}"
    want="$(awk -v n="$name" '$2 == n && $1 ~ /^[0-9a-f]{64}$/ { print $1 }' <<<"$sums")"
    [ -n "$want" ] || { warn "суммы $name в перечне pod нет"; tool_drop; return 1; }
    got=""
    for try in $(seq 1 "${STAND_TOOL_FETCH_TRIES:-3}"); do
      rm -f "$out/$name.part"
      tool_exec "$tns" gzip -1 -c "$TOOL_WORK/out/$name" </dev/null | gzip -dc >"$out/$name.part" 2>/dev/null
      got="$(sha256sum <"$out/$name.part" | cut -d' ' -f1)"
      [ "$got" = "$want" ] && break
      warn "$name: сумма забранного ($got) не сумма pod ($want) — попытка $try"
    done
    [ "$got" = "$want" ] || { rm -f "$out/$name.part"; warn "$name из pod не доставлен целым — инструмента нет"; tool_drop; return 1; }
    chmod "$([ -n "${t#*=}" ] && echo 0755 || echo 0644)" "$out/$name.part" && mv "$out/$name.part" "$out/$name" || { tool_drop; return 1; }
    log "$([ -n "${t#*=}" ] && echo инструмент || echo запись) $name: из $tns, сумма $want сверена с pod"
  done
  tool_drop
  STAND_NS_BIN="$out/stand-ns"
  MM_PRODUCER="$out/module-manifests-configmap"
  STAND_GODEPS="$out/godeps.json"
  export KACHO_PRODUCT_NAMES_BIN="$out/product-names"
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
#
# Процессор (kacho#3291): слой стенда даёт службе доступа запрос 500m при
# пределе 2 (stand_overlay). Запросы стенда dev-prod под слоем — 2270m по
# рендеру (было 1870m при запросе службы 100m); перекат добавляет поды
# развёртываний сверх реплик (maxSurge, StatefulSet — без прибавки) — 1470m,
# итого 3740m в квоте 4. Что квота держит перекат, утверждает
# deploy/kaname_cpu_supply_render_test.go.
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
  # Пространство сборки инструментов, прерванной сигналом либо отказом, — своё: снимается всегда.
  tool_drop
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

# chain_issuer_placeholder СЛОЙ… — 0, если издатель чеканки, сложенный по слоям
# цепочки (kaname.config.authn.tokenSigning.issuer), — заглушка площадки (то же
# правило, что у стража слоя площадки, templates/site-layer-guard.yaml); 1 — нет;
# 2 — слои не прочитаны.
chain_issuer_placeholder() {
  python3 - "$@" <<'PY'
import re, sys, yaml
def merge(dst, src):
    for k, v in (src or {}).items():
        if isinstance(v, dict) and isinstance(dst.get(k), dict):
            merge(dst[k], v)
        else:
            dst[k] = v
    return dst
tree = {}
try:
    for f in sys.argv[1:]:
        merge(tree, yaml.safe_load(open(f)) or {})
except Exception as e:
    print("слои цепочки не прочитаны: %s" % e, file=sys.stderr)
    sys.exit(2)
cur = tree
for k in ("kaname", "config", "authn", "tokenSigning", "issuer"):
    cur = cur.get(k) if isinstance(cur, dict) else None
host = re.sub(r"^[A-Za-z][A-Za-z0-9+.-]*://", "", str(cur or "")).split("/")[0].split(":")[0]
placeholder = r"(?i)^(.*\.)?(invalid|example|example\.com|example\.net|example\.org)\.?$"
sys.exit(0 if re.match(placeholder, host) else 1)
PY
}

# issuer_overlay ORIGIN — издатель платформы у обоих потребителей (kacho#3113):
# приём края и реестра; издатель чеканки — в блоке kaname слоя стенда. Слой
# стенда — слой площадки стенда проб: цепочка, чей издатель — заглушка площадки,
# получает издателем происхождение консоли СТЕНДА, иначе документ обнаружения
# публиковал бы адреса в никуда, а потребители судили бы `iss` заглушки.
issuer_overlay() {
  local o="$1" ks="$1=https://kaname-internal.kacho.svc:9097/.well-known/kaname/jwks.json"
  cat <<EOF
api-gateway:
  tokenAcceptance:
    issuers: "$o"
    issuerKeySets: "$ks"
    platformIssuer: "$o"
registry:
  tokenAcceptance:
    issuers: "$o"
    issuerKeySets: "$ks"
    platformIssuer: "$o"
EOF
}

# SUITE_STACK — стек стенда наборов конвейера (`make dev-up`, тот же состав, что
# умолчание подъёма здесь). Окно источника стенда проб выводится из его цепочки.
SUITE_STACK=dev-prod

# suite_source_attempts — счёт окна источника полосы входа
# (kaname.config.authn.login.sourceAttempts) в слитых значениях цепочки стенда
# наборов конвейера; не выведен — код 2, «не прочитал» не равно «величины нет».
suite_source_attempts() {
  local files f layers=("$UMBRELLA/values.yaml")
  files="$(bash "$DEPLOY_ROOT/tests/helm/stacks.sh" --chain "$SUITE_STACK" ' ')" && [ -n "$files" ] || {
    warn "цепочка стенда наборов «$SUITE_STACK» не прочитана"; return 2; }
  for f in $files; do layers+=("$UMBRELLA/$f"); done
  python3 - "${layers[@]}" <<'PY'
import sys, yaml
def merge(dst, src):
    for k, v in (src or {}).items():
        if isinstance(v, dict) and isinstance(dst.get(k), dict):
            merge(dst[k], v)
        else:
            dst[k] = v
    return dst
tree = {}
try:
    for f in sys.argv[1:]:
        merge(tree, yaml.safe_load(open(f)) or {})
except Exception as e:
    print("stand-ns: слои цепочки стенда наборов не прочитаны: %s" % e, file=sys.stderr)
    sys.exit(2)
cur = tree
for k in ("kaname", "config", "authn", "login", "sourceAttempts"):
    cur = cur.get(k) if isinstance(cur, dict) else None
if type(cur) is not int or cur <= 0:
    print("stand-ns: окно источника стенда наборов не объявлено целым счётом: %r" % (cur,), file=sys.stderr)
    sys.exit(2)
print(cur)
PY
}

# stand_overlay NS CA_NS PERSIST PORT [ISSUER] — ISSUER непуст, когда издатель
# цепочки — заглушка площадки (chain_issuer_placeholder): тогда он равен
# происхождению консоли стенда во всех семи местах.
stand_overlay() {
  local ns="$1" cans="$2" persist="$3" port="$4" issuer="${5:-}"
  local host; host="$(ns_host "$ns")"
  local attempts; attempts="$(suite_source_attempts)" || return 2
  local signing=""
  [ -z "$issuer" ] || signing="
      # Издатель чеканки — происхождение консоли стенда (kacho#3113, issuer_overlay).
      tokenSigning:
        issuer: \"$issuer\""
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
kaname:
  # Процессор службы доступа (kacho#3291, вслед kaname#891; довод — у блока
  # службы в values.prod.yaml). Предел — боевой, два целых ядра: GOMAXPROCS 2,
  # мест вычисления проверяющего 2. Запрос — 500m, а не боевые 2: два ядра
  # запроса на каждое пространство общий кластер не вмещает (параллельных
  # стендов 2 вместо 6, решение диспетчера). 500m — выше порога, при котором
  # очередь всей ёмкости проверяющего укладывается в срок операции церемонии
  # (П2′, deploy/kaname_cpu_supply_test.go: при боевой ёмкости 8 порог 140m,
  # очередь 1808 мс в сроке 6488 мс; при 100m — 9040 мс, срок не держит).
  # Цепочка стенда наборов несёт величину узла kind ранера (100m, 1000m).
  # Квота пространства (ns_objects) держит перекат с этой величиной.
  resources:
    requests:
      cpu: 500m
    limits:
      cpu: "2"
  config:
    authn:
      # Окно источника стенда проб — окно стенда наборов конвейера (цепочка
      # $SUITE_STACK, kacho#3223), выведенное из неё, а не выписанное: сквозные
      # пробы консоли гоняются здесь тем же бегуном с ОДНОГО источника, и каждая
      # регистрация списывается с этого окна (kaname#456). На величине продукта
      # набор обрывался ранней остановкой на регистрации сверх окна. Срок окна —
      # продукта: меняется один факт, счёт. Рубеж перебора паролей площадки это
      # не трогает: слой ставится только в пространство стенда проб (вход —
      # пробросом, ClusterIP, срок ≤ $TTL_MAX_HOURS ч), а таблица стеков стоит на
      # величине продукта (deploy/registration_source_window_stands_test.go).
      login:
        sourceAttempts: $attempts
      # Ключи доступа стенда проб — происхождение ЕГО консоли (appBaseURL выше),
      # выведенное чартом («originFromConsole»), а не выписанное второй строкой
      # рядом с адресом: два объявления одного адреса расходились бы молча.
      # Цепочка площадки может объявлять перечень литералом («никого» у
      # prorobotech, «origins: []», kacho#3068) — это решение о происхождении
      # ПЛОЩАДКИ, и консоль стенда в нём не названа: церемония ключа на стенде
      # была бы неисполнима, и пробы ключей доступа стояли бы «условие не
      # создано». Перечень цепочки снимается здесь («null» — не объявление),
      # иначе рядом с «originFromConsole» он — второе объявление, и рендер
      # отказывает. Цепочка a8f60d перечня не объявляет и выводит его из своей
      # консоли (kacho#3229) — снятие ей безвредно. Пространство kacho этот слой
      # не видит.
      accessKeys:
        originFromConsole: true
        origins: null$signing
# Плоскости данных хранилища у стенда проб НЕТ, и слой говорит это (kacho#3222).
# Боевой слой объявляет CEPH_RBD, а кластера блочного хранения, его файла
# конфигурации и ключа к нему у стенда нет, и чарт службы тома под каталог ссылок
# не монтирует: сверщик каждый проход брал бы том и отвечал «credentials
# reference … is not resolvable», и том стоял бы в CREATING. Ключ, положенный
# посевом, отказ лишь перенёс бы на следующее звено — адаптер читает и
# конфигурацию кластера. Пустой вид — посадка продукта только управляющей
# плоскости, как у стенда наборов конвейера (values.dev-prod.yaml): страж старта
# хранилища её принимает, ни режима, ни mTLS, ни sslmode слой не трогает.
storage:
  config:
    blockBackend:
      kind: ""
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
  [ -z "$issuer" ] || issuer_overlay "$issuer"
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

# ── ОБРАЗЫ РЕВИЗИИ: ВЕТКА ПУБЛИКАЦИИ СПРАШИВАЕТСЯ У РЕЕСТРА (kacho#3260) ────
#
# Тег образа — «<ветка>-<коммит>», и ветку в нём пишет СБОРКА: та, чьей головой
# коммит был в момент сборки (`push` в main либо ручной запуск docker-build.yml
# и ui.yml на любой ветке). Прежняя редакция знала две ветки (`1266 main`) и
# требовала членства коммита в них — коммит ветки запроса образов «не имел», хотя
# ручной запуск их публиковал (полоса kacho#3259 обходила это веткой-тёзкой и
# STAND_IMAGE_BRANCHES). Теперь вопрос «опубликован ли» задаётся РЕЕСТРУ, а
# история клона лишь называет, у каких веток спрашивать.

# image_branch_candidates SHA — ветки, сборка которых могла опубликовать образы
# SHA, по убыванию правдоподобия и без повторов: ветки origin, чья голова — SHA
# (ручной запуск собирает голову); местные ветки с той же головой; main; прочие
# ветки origin, содержащие SHA (голова ушла вперёд после сборки). Заданный
# STAND_IMAGE_BRANCHES — ровно он.
image_branch_candidates() {
  local sha="$1"
  if [ -n "$IMAGE_BRANCHES" ]; then
    tr -s ' \t' '\n' <<<"$IMAGE_BRANCHES" | awk 'NF && !seen[$0]++'
    return 0
  fi
  {
    git -C "$REPO_ROOT" for-each-ref --points-at "$sha" --format='%(refname:lstrip=3)' refs/remotes/origin
    git -C "$REPO_ROOT" for-each-ref --points-at "$sha" --format='%(refname:lstrip=2)' refs/heads
    echo main
    git -C "$REPO_ROOT" for-each-ref --contains "$sha" --format='%(refname:lstrip=3)' refs/remotes/origin
  } | awk 'NF && $0 != "HEAD" && !seen[$0]++'
}

# build_hint SHA ОБРАЗ… — команда, которой образы ревизии собираются ТЕМ ЖЕ
# способом, что у конвейера: ручной запуск сборки на ветке, чья голова — SHA.
# Семьи образов собирают две сборки, и названа та, чьих образов нет.
build_hint() {
  local sha="$1" slug branch img svc=0 ui=0; shift
  for img; do case "$img" in kacho-ui-future-*) ui=1 ;; *) svc=1 ;; esac; done
  slug="$(git -C "$REPO_ROOT" remote get-url origin 2>/dev/null | sed -E 's#^(git@[^:]+:|[a-z]+://([^@/]+@)?[^/]+/)##; s#\.git$##')"
  [ -n "$slug" ] || slug="<владелец>/<репозиторий>"
  branch="$( { git -C "$REPO_ROOT" for-each-ref --points-at "$sha" --format='%(refname:lstrip=3)' refs/remotes/origin
               git -C "$REPO_ROOT" for-each-ref --points-at "$sha" --format='%(refname:lstrip=2)' refs/heads; } |
             awk 'NF && $0 != "HEAD" { print; exit }')"
  [ -n "$branch" ] || branch="<ветка на origin, чья голова — ${sha:0:8}>"
  warn "собрать тем же способом, что конвейер, — ручным запуском сборки на ветке, чья ГОЛОВА $sha (ветка отправлена на origin):"
  [ "$svc" -eq 0 ] || warn "  gh workflow run docker-build.yml -R $slug --ref $branch"
  [ "$ui" -eq 0 ]  || warn "  gh workflow run ui.yml -R $slug --ref $branch"
  warn "ручной запуск собирает голову ветки в момент запуска; когда сборка завершится — повтори с REF=$sha BRANCH=$branch"
}

# pick_image_tags SHA ИМЕНА — выбрать ветку публикации и проверить КАЖДЫЙ образ.
# Ветка — первая кандидатка, под которой образ первого имени лежит в реестре.
# Итог — в PICKED_TAGS («образ<TAB>тег» по строке) и IMAGE_BRANCH; функция зовётся
# без подоболочки, иначе итог умер бы вместе с ней.
#   0 — ветка выбрана, каждый образ найден;
#   1 — образов ревизии в реестре нет (ни под одной кандидаткой, либо части нет
#       под выбранной) — напечатана команда сборки;
#   2 — не выполнилось: реестр не ответил либо тег не выведен. Это НЕ «образа нет».
PICKED_TAGS="" IMAGE_BRANCH=""
pick_image_tags() {
  local sha="$1" names="$2" first branch tags tag img rc probed=0 tried="" missing=""
  PICKED_TAGS="" IMAGE_BRANCH=""
  first="$(head -1 <<<"$names")"
  [ -n "$first" ] || { warn "образов частей дерева в цепочке ноль — выбирать ветку не для чего"; return 2; }
  while IFS= read -r branch; do
    if [ "$probed" -ge "$IMAGE_PROBE_MAX" ]; then
      warn "кандидаток больше STAND_IMAGE_PROBE_MAX=$IMAGE_PROBE_MAX — остальные не спрошены; назови ветку явно: BRANCH=<ветка>"
      break
    fi
    probed=$((probed + 1)); tried="$tried $branch"
    tags="$(bash "$HERE/gen-managed-image-pins.sh" --tag-of "$(tr '\n' ',' <<<"$names" | sed 's/,$//')" --ref "$sha" --branch "$branch")" ||
      { warn "тег образов для ветки «$branch» не выведен (выше)"; return 2; }
    tag="$(awk -F'\t' -v i="$first" '$1 == i { print $2; exit }' <<<"$tags")"
    reachable "$IMAGE_PREFIX/$first" "$tag"; rc=$?
    case "$rc" in
      0) IMAGE_BRANCH="$branch"; break ;;
      1) ;;
      *) warn "реестр не ответил о $IMAGE_PREFIX/$first:$tag — «не ответил» не равно «образа нет»"; return 2 ;;
    esac
  done < <(image_branch_candidates "$sha")
  if [ -z "$IMAGE_BRANCH" ]; then
    warn "образа $first ревизии $sha нет в реестре ни под одной веткой-кандидаткой (спрошено $probed:$tried)"
    # shellcheck disable=SC2086 # имена — по строке, дробление и нужно
    build_hint "$sha" $names
    return 1
  fi
  while IFS=$'\t' read -r img tag; do
    [ -n "$img" ] || continue
    reachable "$IMAGE_PREFIX/$img" "$tag"; rc=$?
    case "$rc" in
      0) ;;
      1) missing="$missing $img"; warn "образ $IMAGE_PREFIX/$img:$tag в реестре НЕ найден — стенд ушёл бы в ImagePullBackOff" ;;
      *) warn "реестр не ответил о $IMAGE_PREFIX/$img:$tag — «не ответил» не равно «образа нет»"; return 2 ;;
    esac
  done <<<"$tags"
  if [ -n "$missing" ]; then
    # shellcheck disable=SC2086
    build_hint "$sha" $missing
    return 1
  fi
  PICKED_TAGS="$tags"
}

images_overlay() {
  local sha="$1" out="$2"; shift 2
  local names refs="" img tag
  names="$("$STAND_NS_BIN" images -list "$@")" || return 2
  pick_image_tags "$sha" "$names" || return $?
  while IFS=$'\t' read -r img tag; do
    [ -n "$img" ] || continue
    refs="$refs $img=$IMAGE_PREFIX/$img:$tag"
  done <<<"$PICKED_TAGS"
  log "образы ревизии $sha (ветка публикации $IMAGE_BRANCH): $(wc -l <<<"$PICKED_TAGS") частей дерева, каждая найдена в реестре"
  "$STAND_NS_BIN" images -refs "$refs" "$@" >"$out"
}

# reachable REPO TAG — манифест образа в реестре: 0 — есть, 1 — реестр ответил
# «нет такого» (404), 2 — ответа нет (сеть, отказ в учётке, предел частоты):
# «не ответил» не равно «нет». Проверяется только docker.io: у прочих реестров
# вопрос задаёт сам кластер при подъёме, и это печатается.
reachable() {
  local repo="$1" tag="$2" path tok code
  case "$repo" in
    docker.io/*) path="${repo#docker.io/}" ;;
    *) warn "досягаемость $repo:$tag НЕ проверена (не docker.io) — ответит кластер"; return 0 ;;
  esac
  tok="$(curl -fsS --max-time 20 "https://auth.docker.io/token?service=registry.docker.io&scope=repository:$path:pull" | jq -r .token)" || return 2
  [ -n "$tok" ] && [ "$tok" != null ] || return 2
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 -I -H "Authorization: Bearer $tok" \
    -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json' \
    "https://registry-1.docker.io/v2/$path/manifests/$tag")"
  case "$code" in
    200) return 0 ;;
    404) return 1 ;;
    *)   return 2 ;;
  esac
}

# ── ОБРАЗ ВЕТКИ СЛУЖБЫ ДОСТУПА ВМЕСТО ПИНА (kacho#3312, вслед kaname#916) ────
#
# Служба доступа — вынесенная часть: её образ собирает конвейер ЕЁ репозитория, и
# стенд ставит его по пину профиля (`kaname.image` в values.yaml зонта). Прогон
# ветки службы на внешнем кластере требует её образа, а пин его не называет.
# Конвейер службы публикует образ ветки задачи под `task-<ветка>-<8 знаков
# ревизии>`, посаженное (ствол, линия) — под `<ветка>-<8 знаков>`; косая черта
# ветки — дефис (шаг `subject` docker-build.yml службы). STAND_KANAME_BRANCH
# (make KANAME_BRANCH=…) ставит образ ветки вместо пина:
#   1. ревизия — голова ветки на origin службы, либо STAND_KANAME_REF (sha40);
#      origin выводится из go.mod (модуль, чьё последнее звено — имя образа);
#   2. тег выводится из ветки и ревизии и спрашивается у реестра той же
#      `reachable`, что и образы дерева (kacho#3260); нет образа — отказ с именем
#      тега и командой сборки, без отката на пин;
#   3. провенанс: ссылка контейнера — ссылка ветки, ревизия из файла образа —
#      голова ветки (`stand-provenance.sh --pin --pin-revision`); иначе стенд не
#      объявляется поднятым.
# Без параметра — прежнее поведение: накладки нет, служба ставится по пину.
KANAME_BRANCH="${STAND_KANAME_BRANCH:-}"
KANAME_REF="${STAND_KANAME_REF:-}"
KANAME_IMAGE_REF="" KANAME_IMAGE_NAME="" KANAME_SHA="" KANAME_TAG=""

# kaname_tags ВЕТКА SHA40 — теги образа ветки по убыванию правдоподобия: форма
# задачи, затем форма посаженного. Какой опубликован, отвечает реестр.
kaname_tags() {
  local branch="$1" sha="$2" slug
  [ -n "$branch" ] || { warn "ветка службы доступа пуста — тег образа выводить не из чего"; return 2; }
  [[ "$sha" =~ ^[0-9a-f]{40}$ ]] || { warn "ревизия ветки «$sha» не sha40 — тег по восьми знакам вывести не из чего"; return 2; }
  slug="${branch//\//-}"
  printf 'task-%s-%s\n%s-%s\n' "$slug" "${sha:0:8}" "$slug" "${sha:0:8}"
}

# kaname_remote ИМЯ — origin службы: STAND_KANAME_REMOTE либо модуль go.mod,
# чьё последнее звено — имя её образа (тот же модуль, что пинит дерево).
kaname_remote() {
  if [ -n "${STAND_KANAME_REMOTE:-}" ]; then printf '%s\n' "$STAND_KANAME_REMOTE"; return 0; fi
  local mod
  mod="$(awk -v n="$1" '{ p = ($1 == "require") ? $2 : $1 } p ~ ("/" n "$") { print p; exit }' "$REPO_ROOT/go.mod")"
  [ -n "$mod" ] || { warn "модуля «…/$1» в go.mod нет — репозиторий службы не выведен (STAND_KANAME_REMOTE)"; return 2; }
  printf 'https://%s\n' "$mod"
}

# kaname_branch_head ORIGIN ВЕТКА — голова ветки на origin службы: 0 — напечатана,
# 1 — ветки нет, 2 — origin не ответил. Спрашивается ровно refs/heads/<ветка>:
# образец подмешал бы тёзку-приставку.
kaname_branch_head() {
  local remote="$1" branch="$2" out
  out="$(git ls-remote --heads "$remote" "refs/heads/$branch" 2>&1)" || {
    warn "origin службы не ответил о ветке «$branch»: $(head -1 <<<"$out")"; return 2; }
  out="$(awk -v r="refs/heads/$branch" '$2 == r { print $1 }' <<<"$out")"
  [ -n "$out" ] || { warn "ветки «$branch» на origin службы нет"; return 1; }
  printf '%s\n' "$out"
}

# kaname_pick_tag РЕПОЗИТОРИЙ ВЕТКА SHA40 СЛАГ — первый тег kaname_tags, лежащий в
# реестре, в KANAME_TAG (зовётся без подоболочки). 0 — найден; 1 — ни одного:
# названы спрошенные теги и команда сборки; 2 — реестр не ответил либо тег не выведен.
kaname_pick_tag() {
  local repo="$1" branch="$2" sha="$3" slug="$4" tags tag rc tried=""
  KANAME_TAG=""
  tags="$(kaname_tags "$branch" "$sha")" || return 2
  while IFS= read -r tag; do
    reachable "$repo" "$tag"; rc=$?
    case "$rc" in
      0) KANAME_TAG="$tag"; return 0 ;;
      1) tried="$tried $repo:$tag" ;;
      *) warn "реестр не ответил о $repo:$tag — «не ответил» не равно «образа нет»"; return 2 ;;
    esac
  done <<<"$tags"
  warn "образа ветки «$branch» службы доступа (ревизия $sha) в реестре НЕТ — спрошено:$tried"
  warn "собрать тем же способом, что конвейер службы, — ручным запуском сборки на ветке, чья голова $sha:"
  warn "  gh workflow run docker-build.yml -R $slug --ref $branch"
  warn "пин профиля вместо ветки НЕ ставится: стенд назывался бы стендом ветки, а исполнял бы пин"
  return 1
}

# kaname_repository ИМЯ СЛОЙ… — репозиторий образа службы после сложения слоёв
# цепочки: тег ветки ставится в тот же репозиторий, что и пин.
kaname_repository() {
  python3 - "$@" <<'PY'
import sys, yaml
def merge(a, b):
    if isinstance(a, dict) and isinstance(b, dict):
        out = dict(a)
        for k, v in b.items():
            out[k] = merge(a.get(k), v)
        return out
    return b
name, d = sys.argv[1], {}
for f in sys.argv[2:]:
    d = merge(d, yaml.safe_load(open(f)) or {})
repo = (((d.get(name) or {}).get("image") or {}).get("repository")) or ""
if not repo:
    sys.exit("%s.image.repository в слоях цепочки не объявлен — тег ветки ставить некуда" % name)
print(repo)
PY
}

# kaname_overlay ВЫВОД СЛОЙ… — накладка «образ ветки службы вместо пина». Без
# STAND_KANAME_BRANCH — ничего не пишет, код 0 (служба по пину). Итог — в
# KANAME_IMAGE_REF, KANAME_IMAGE_NAME, KANAME_SHA (зовётся без подоболочки).
# Коды: 0 — накладка написана либо не нужна; 1 — ветки либо образа нет; 2 — не выполнилось.
kaname_overlay() {
  local dst="$1"; shift
  local img remote slug sha repo rc
  KANAME_IMAGE_REF="" KANAME_IMAGE_NAME="" KANAME_SHA=""
  if [ -z "$KANAME_BRANCH" ]; then
    [ -z "$KANAME_REF" ] || { warn "STAND_KANAME_REF задан без STAND_KANAME_BRANCH — тег образа без ветки не выводится"; return 2; }
    return 0
  fi
  # shellcheck source=deploy/scripts/lib/product-names.sh
  . "$HERE/lib/product-names.sh"
  img="$(product_image_name iam)" || { warn "имя образа службы доступа не прочитано (выше)"; return 2; }
  remote="$(kaname_remote "$img")" || return 2
  slug="$(sed -E 's#^(git@[^:]+:|[a-z]+://([^@/]+@)?[^/]+/)##; s#\.git$##' <<<"$remote")"
  if [ -n "$KANAME_REF" ]; then
    [[ "$KANAME_REF" =~ ^[0-9a-f]{40}$ ]] || { warn "STAND_KANAME_REF «$KANAME_REF» не sha40"; return 2; }
    sha="$KANAME_REF"
  else
    sha="$(kaname_branch_head "$remote" "$KANAME_BRANCH")" || return $?
  fi
  repo="$(kaname_repository "$img" "$@")" || return 2
  kaname_pick_tag "$repo" "$KANAME_BRANCH" "$sha" "$slug"; rc=$?
  [ "$rc" -eq 0 ] || return "$rc"
  printf '%s:\n  image:\n    repository: %s\n    tag: %s\n    digest: ""\n' "$img" "$repo" "$KANAME_TAG" >"$dst" || return 2
  KANAME_IMAGE_NAME="$img" KANAME_SHA="$sha" KANAME_IMAGE_REF="$repo:$KANAME_TAG"
  log "служба $img — образ ветки $KANAME_BRANCH (ревизия $sha): $KANAME_IMAGE_REF найден в реестре; пин профиля не ставится"
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
  out="$("$STAND_NS_BIN" inputs -root "$REPO_ROOT" -base "$sha" -godeps "$STAND_GODEPS")"; rc=$?
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

  # ── 1. Без записи в кластер стенда (пространство сборки инструментов — своё, снимается тем же шагом) ──
  UP_STATE="$(ns_state "$ns")" || die "состояние пространства $ns НЕ ПРОЧИТАНО — кластер не ответил" 2
  [ "$UP_STATE" != foreign ] || die "пространство $ns существует и НЕ помечено $LABEL_STAND=test — оно чужое, стенд в него не ставится" 1
  chain="$(bash "$DEPLOY_ROOT/tests/helm/stacks.sh" --args "$stack" "$UMBRELLA")" && [ -n "$chain" ] ||
    die "цепочка «$stack» в deploy/stacks.txt не прочитана — стенд без цепочки сел бы на умолчания чарта" 2
  sha="$(git -C "$REPO_ROOT" rev-parse --verify "${ref}^{commit}" 2>/dev/null)" || die "ревизия «$ref» не разрешается в коммит" 2
  mkdir -p "$work" || die "рабочий каталог $work не заведён" 2
  build_tool "$work" "$ns" "$task" || die "инструменты подъёма (перенос рендера, производитель манифестов модулей) во внешнем кластере не собраны (выше)" 2
  ref_guard "$sha" || die "ревизия образов и рабочая копия расходятся (выше)" 2
  cans="$(ca_namespace)" || die "контроллер cert-manager на кластере не прочитан" 2
  [ "$(wc -w <<<"$cans")" = 1 ] || die "пространство ресурсов cert-manager не установлено однозначно («$cans»): стенд cert-manager не ставит, он переиспользует ровно один существующий" 2
  dns="$(dns_peer)" || die "резолвер кластера не установлен (выше) — выход DNS стенда сузить не до чего" 2
  dnsns="${dns%% *}"; dnssel="${dns#* }"
  if has_default_storage_class; then persist=on; else persist=off; fi
  port="$(ns_port "$ns")"
  log "кластер: cert-manager переиспользуется (ресурсы в ns $cans); DNS — поды $dnssel в ns $dnsns; класс томов по умолчанию: $persist; контроллер входа не ставится"

  subchart_defaults >"$work/subchart-defaults.yaml" || die "умолчания подчартов не прочитаны" 2
  local layers=("$work/subchart-defaults.yaml" "$UMBRELLA/values.yaml") f files
  files="$(bash "$DEPLOY_ROOT/tests/helm/stacks.sh" --chain "$stack" ' ')" && [ -n "$files" ] ||
    die "слои цепочки «$stack» не прочитаны — накладку образов выводить не из чего" 2
  for f in $files; do layers+=("$UMBRELLA/$f"); done
  # Издатель платформы (kacho#3113): заглушка площадки в цепочке заменяется
  # происхождением консоли стенда; имя разработки слой стенда не трогает.
  local issuer="" irc=0
  chain_issuer_placeholder "${layers[@]}" || irc=$?
  case "$irc" in
    0) issuer="https://$(ns_host "$ns"):$port"
       log "издатель цепочки $stack — заглушка площадки: издатель стенда — происхождение его консоли" ;;
    1) ;;
    *) die "издатель цепочки «$stack» не прочитан (код $irc) — «не прочитан» не равно «не заглушка»" 2 ;;
  esac
  stand_overlay "$ns" "$cans" "$persist" "$port" "$issuer" >"$work/stand.yaml" ||
    die "слой стенда не выведен (выше)" 2
  images_overlay "$sha" "$work/images.yaml" "${layers[@]}" || die "накладка образов ревизии $sha не выведена" 2
  local krc=0
  kaname_overlay "$work/kaname.yaml" "${layers[@]}" || krc=$?
  case "$krc" in
    0) ;;
    1) die "образа ветки «$KANAME_BRANCH» службы доступа нет (выше) — стенд на пине вместо ветки не поднимается" 2 ;;
    *) die "образ ветки «$KANAME_BRANCH» службы доступа не выбран (код $krc, выше) — «не выбран» не равно «нет»" 2 ;;
  esac

  expires="$(date -u -d "+${TTL_HOURS} hours" +%Y-%m-%dT%H:%M:%SZ)"
  ns_objects "$ns" "$task" "$expires" "$stack" "$sha" "$cans" "$dnsns" "$dnssel" >"$work/ns.yaml"
  kubectl apply --dry-run=client -f "$work/ns.yaml" >/dev/null || die "пространство, квота, пределы и сетевая изоляция не проходят проверку манифеста" 2
  can_write "$ns" || die "завести пространство с квотой нечем (права выше) — ни одной записи не сделано" 2

  bash "$HERE/helm-umbrella-deps.sh" >/dev/null || die "зависимости зонтичного чарта не материализованы" 2
  local extra_base=(-f "$work/images.yaml" -f "$work/stand.yaml")
  [ -z "$KANAME_IMAGE_REF" ] || extra_base+=(-f "$work/kaname.yaml")
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
  stand_nodes "пространство $ns"
  [ "$UP_STATE" = absent ] && UP_CREATED=1
  kubectl apply -f "$work/ns.yaml" >/dev/null || die "пространство $ns с квотой не заведено" 1
  log "пространство $ns: задача #$task, срок $expires; квота, пределы, Pod Security baseline и сетевая изоляция применены"

  # ── 3. Стенд ──
  stand_nodes "манифесты модулей и секреты"
  bg make -C "$DEPLOY_ROOT" --no-print-directory module-manifests-configmap \
    MODULE_MANIFESTS_STACK="$stack" STACK_NAMESPACE="$ns" EXPECT_CONTEXT="$STAND_CTX" \
    MODULE_MANIFESTS_PRODUCER="$MM_PRODUCER" || die "манифесты модулей не доставлены" 1
  local mm="$UMBRELLA/values.module-manifests.yaml"
  local extra=("${extra_base[@]}" -f "$mm" --set cert-manager.enabled=false)
  local args=("${chain_args[@]}" "${extra[@]}")
  STACK_NAMESPACE="$ns" STACK_RELEASE="$RELEASE" bg bash "$HERE/stack-secrets.sh" "$stack" "${extra[@]}" ||
    die "предусловные секреты стенда не созданы" 1

  stand_nodes "helm upgrade --install"
  log "helm: цепочка $stack + образы $sha + стенд, перенос в $ns"
  bg helm_install "${args[@]}" \
      --post-renderer kacho-stand-ns \
      --post-renderer-args "-namespace=$ns" --post-renderer-args "-ca-namespace=$cans" ||
    die "helm upgrade --install отказал (журнал $work/helm.log)" 1

  # Прокси API-сервера на управляемом кластере до подов не доходит — вопрос
  # задаётся пробросом (scripts/wait-edge-ready.sh, EDGE_READY_VIA).
  EDGE_READY_VIA=port-forward bg bash "$HERE/wait-edge-ready.sh" "$ns" api-gateway 90 2 3 || die "край стенда не ответил готовностью" 1
  stand_nodes "посев"
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
  local prc=0 pv=(--namespace "$ns" --expect "$sha")
  # Образ ветки службы доступа сверяется с ЕЁ ссылкой и головой ветки, а не с пином.
  [ -z "$KANAME_IMAGE_REF" ] || pv+=(--pin "$KANAME_IMAGE_NAME=$KANAME_IMAGE_REF" --pin-revision "$KANAME_IMAGE_NAME=$KANAME_SHA")
  bg tee_to "$work/provenance.log" bash "$HERE/stand-provenance.sh" "${pv[@]}" || prc=$?
  [ "$prc" -eq 0 ] || die "провенанс стенда не сходится с ревизией $sha${KANAME_IMAGE_REF:+ и веткой службы $KANAME_BRANCH@$KANAME_SHA} (код $prc, журнал $work/provenance.log)" 1

  UP_DONE=1
  trap - INT TERM
  trap ctx_cleanup EXIT
  log "стенд $ns ПОДНЯТ за $(( $(date +%s) - start )) с: цепочка $stack, образы $sha, срок $expires"
  [ -z "$KANAME_IMAGE_REF" ] || log "служба $KANAME_IMAGE_NAME — образ ветки $KANAME_BRANCH: $KANAME_IMAGE_REF, ревизия $KANAME_SHA сверена провенансом"
  log "консоль https://$(ns_host "$ns"):$port · край https://127.0.0.1:$((port + 1)) · приёмник http://127.0.0.1:$((port + 2))"
  log "проброс и окружение проб:  bash deploy/scripts/stand-ns.sh forward $ns"
}

# ── ПРОГОН: ПОДЪЁМ → КОМАНДА → СНЯТИЕ ВСЕГДА ─────────────────────────────────
#
# Код выхода — код КОМАНДЫ. Если стенд не поднялся или проброс не встал, команда
# не исполнялась: это «не выполнилось», а не красное, и код — RUN_UNMET (125),
# которого команда сама не даёт (код 125 зарезервирован этим смыслом).
RUN_UNMET=125
RUN_NS="" RUN_KEEP=0 RUN_UP=0 RUN_CHILD="" RUN_RUNNER="" RUN_OUT="" RUN_LOGS=""
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
  [ -z "$RUN_LOGS" ] || { kill "$RUN_LOGS" 2>/dev/null; wait "$RUN_LOGS" 2>/dev/null; }
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
      --runner) RUN_RUNNER="${2:?--runner без значения}"; shift
               [[ " $STAND_RUNNER_KINDS " == *" $RUN_RUNNER "* ]] || die "--runner «$RUN_RUNNER» — не из $STAND_RUNNER_KINDS" 2 ;;
      --out)   RUN_OUT="${2:?--out без значения}"; shift ;;
      --)      shift; break ;;
      *) die "run: неизвестный аргумент «$1» (команда — после --)" 2 ;;
    esac
    shift
  done
  [ $# -gt 0 ] || die "run: команда не названа (stand-ns.sh run NS [--keep] [--stack S] [--ref R] [--runner newman|browser] [--out DIR] -- КОМАНДА…)" 2
  [ -z "$RUN_OUT" ] || [ -n "$RUN_RUNNER" ] || die "run: --out — лишь с --runner (без раннера отчёты пишет команда на машине сама)" 2
  ns_valid "$ns" >/dev/null || die "имя «$ns» не по правилу t<номер задачи>-<коротко>; пространство kacho цель не трогает" 2
  ttl_valid "$TTL_HOURS" || die "STAND_TTL_HOURS=«$TTL_HOURS» вне 1..$TTL_MAX_HOURS" 2
  [ -z "$RUN_RUNNER" ] || [ -z "$(git -C "$REPO_ROOT" status --porcelain --untracked-files=no 2>/dev/null)" ] ||
    die "run --runner: в рабочей копии незакоммиченные правки — pod исполняет коммит (git archive HEAD), закоммить их" 2
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
  if [ -n "$RUN_RUNNER" ]; then
    RUN_UP=1
    runner_run "$ns" "$ref" "$@"; rc=$?
    log "прогон в pod раннера $RUN_RUNNER: код $rc (125 — команда не исполнилась либо её отчёты не доставлены целиком)"
    exit "$rc"
  fi
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

# ── РАННЕР В POD СТЕНДА (kacho#3346, kacho-workspace#1303) ─────────────────────
#
# Решение владельца 2026-10-22 (kacho-workspace#1301) дословно: «запиши в рул что бы не
# использовал мою локальную тачку. использовать только в одном случае если доступа к
# кластеру нету». Прогон с --runner исполняет КОМАНДУ не на машине, а в pod своего
# пространства стенда; на машине — kubectl, git и tar.
#
#   pod и учётка  `stand-runner` в пространстве стенда; роль — ТОЛЬКО в нём: чтение
#                 объектов, проброс и exec (пробы ходят к службам тем же путём, что с
#                 машины: kubectl port-forward, exec в базу службы личности);
#   образ         по виду: newman — node мажорной версии setup-node и newman мажорной
#                 `npm i -g newman@N` из .github/workflows/e2e-newman.yml; browser —
#                 образ playwright версии пина `@playwright/test` ui-future/e2e (chromium —
#                 его). Версии читаются из ДЕРЕВА головы (runner_versions), второго
#                 объявления нет; kubectl pod — клиент машины тем же потоком (той же
#                 архитектуры), иначе загрузка версии сервера кластера с суммой; newman —
#                 ещё buf версии шага bufbuild/buf-setup-action того же файла (наборы и
#                 проба ban #6 берут дескрипторы из `buf build proto`) с суммой выпуска
#                 (RUNNER_BUF_SH); не поставлен — 125 с именем инструмента;
#   исходники     `git archive` головы рабочей копии потоком в pod (образы — ревизии REF, их
#                 расхождение судит ref_guard подъёма); незакоммиченная правка — отказ 2 до подъёма;
#   окружение     тем же cmd_forward изнутри pod (`pod-forward`): происхождение консоли
#                 `https://<ns>.kacho.test:<порт>` — то же, что запечено при подъёме, имя —
#                 на петлю pod (hostAliases), сам проброс — kubectl учёткой pod;
#   администратор из секрета стенда в окружение pod (secretKeyRef, optional): величины на
#                 диск машины и в её журнал не ложатся (kacho#3338);
#   запись        условия, которому нужна запись на стенде (Ф12-35: секрет обёртки и перекат
#                 службы доступа), учётка pod не получает: pod просит машину файлом в
#                 /work/cond, машина исполняет строку ведомости stand-conditions.tsv с
#                 исполнителем machine своим контекстом -client (runner_conds), pod судит
#                 исход сам; строки нет — 125, «условие не создано» (kacho#3348);
#   отчёты        файлами из pod в --out (умолчание .stand-ns/out/<ns>/ — каталог стенда
#                 снимает down, отчёт переживает его): newman — каталоги out наборов,
#                 browser — results*.json, selection.json, test-results (trace), playwright-report;
#                 Доставка сверяется: перечень «размер сумма путь» собран в pod до передачи,
#                 каждый путь сверен на машине; неполная — повтор, затем 125 с перечнем
#                 недоставленного (runner_fetch);
#   код           код КОМАНДЫ; 125 — команда не исполнялась (образ не встал, инструменты не
#                 поставились, проброс не поднялся, pod исчез) либо её отчёты не доставлены
#                 целиком — «не выполнилось», не красное.
# Срок pod — STAND_RUNNER_DEADLINE (с, умолчание 10800), готовности — STAND_RUNNER_READY_WAIT.
# ГРАНИЦА: инструменты pod ставятся из сети на каждом прогоне (apt, dl.k8s.io с суммой,
# npm, выпуски grpcurl и buf) — 1–3 мин; образ раннера с ними — отдельный предмет.
STAND_RUNNER_KINDS="newman browser"
RUNNER_POD=stand-runner
RUNNER_DEADLINE="${STAND_RUNNER_DEADLINE:-10800}"

# Текст pod: buf версии конвейера в КАТАЛОГ (аргументы: ВЕРСИЯ КАТАЛОГ). Сумма — строка
# бинаря из sha256.txt того же выпуска (та же форма, что у тяжёлого слота воркспейса);
# поставленный бинарь обязан назвать ровно эту версию. Код 1 — не поставлен, причина — с
# именем инструмента, в КАТАЛОГЕ ничего не остаётся. Отдельным текстом — ради самопроверки,
# которая исполняет его с подменённой загрузкой.
read -r -d '' RUNNER_BUF_SH <<'POD'
set -uo pipefail
v="$1" bin="$2"
[[ "$v" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "buf: версия конвейера «$v» не в форме X.Y.Z"; exit 1; }
case "$(uname -m)" in x86_64) a=x86_64 ;; aarch64) a=aarch64 ;; *) echo "buf: архитектура $(uname -m)"; exit 1 ;; esac
f="buf-Linux-$a" u="https://github.com/bufbuild/buf/releases/download/v$v"
d="$(mktemp -d)" || { echo "buf: каталог загрузки не заведён"; exit 1; }
trap 'rm -rf "$d"' EXIT
( cd "$d" && curl -fsSL --retry 3 --speed-limit 50000 --speed-time 60 -o "$f" "$u/$f" &&
  curl -fsSL --retry 3 --max-time 60 -o sums "$u/sha256.txt" &&
  grep -E "^[0-9a-f]{64}  $f\$" sums | sha256sum -c --quiet --strict - ) ||
  { echo "buf $v: выпуск $f не получен либо сумма не сошлась с sha256.txt выпуска"; exit 1; }
chmod 0755 "$d/$f" || { echo "buf $v: бинарь выпуска не исполним"; exit 1; }
got="$("$d/$f" --version 2>&1 | head -n 1)"
[ "$got" = "$v" ] || { echo "buf $v: бинарь выпуска называет версию «$got»"; exit 1; }
install -m 0755 "$d/$f" "$bin/buf" || { echo "buf $v: не поставлен в $bin"; exit 1; }
echo "buf $v"
POD

# Текст, который исполняет pod. Ждёт исходники (/work/.go), ставит инструменты, поднимает
# проброс изнутри, исполняет команду, пишет код в /work/.rc и ждёт, пока машина заберёт
# отчёты (/work/.fetched). Метки — вне /src: дерево исходников становится репозиторием.
read -r -d '' RUNNER_POD_SH <<'POD'
set -uo pipefail
unset KUBECONFIG
say() { printf '=== stand-runner: %s\n' "$*"; }
# Ждёт, пока машина заберёт отчёты (/work/.fetched). Срок — 600 с от последнего признака
# забора, а не от кода: пока жив tar текущей попытки (/work/.fetching.pid), pod не выходит и
# поток не рвётся посередине (ws#1303, ⛔ приёмки: «выходит через 600 с независимо от забора»).
fetched() {
  local last now p
  last="$(date +%s)"
  while [ ! -e /work/.fetched ]; do
    now="$(date +%s)"
    p="$(cat /work/.fetching.pid 2>/dev/null)"
    [ -z "$p" ] || [ ! -e "/proc/$p" ] || last="$now"
    [ $((now - last)) -lt 600 ] || { say "отчёты не забраны за 600 с после последней попытки забора — pod выходит"; return 0; }
    sleep 2
  done
}
finish() { echo "$1" > /work/.rc.tmp && mv /work/.rc.tmp /work/.rc; fetched; exit "$1"; }
unmet() { say "УСЛОВИЕ НЕ СОЗДАНО: $* — команда НЕ исполнялась (125)"; finish 125; }
mkdir -p "$HOME" "$KACHO_STAND_NS_WORKDIR" "$KACHO_STAND_COND_DIR"
for _ in $(seq 1 300); do [ -e /work/.go ] && break; sleep 2; done
[ -e /work/.go ] || { say "исходники не доставлены за 600 с"; exit 125; }
case "$(uname -m)" in x86_64) ka=amd64 ga=x86_64 ;; aarch64) ka=arm64 ga=arm64 ;; *) unmet "архитектура $(uname -m)" ;; esac
export DEBIAN_FRONTEND=noninteractive
say "инструменты: apt"
{ apt-get update -q && apt-get install -y -q --no-install-recommends ca-certificates curl git jq openssl tar gzip \
    python3 python3-yaml python3-jwt python3-cryptography; } > /work/apt.log 2>&1 || { tail -n 5 /work/apt.log; unmet "apt-get"; }
# kubectl — клиент машины, которым говорит сам stand-ns (тот же поток, что исходники): выход
# узла к dl.k8s.io бывал ~25 КБ/с (замер kacho#3346 — 2,5 МБ из 57 за 90 с). Его нет либо он
# не исполняется здесь — загрузка версии сервера с суммой, с пределом скорости.
if [ -x /work/bin/kubectl ] && /work/bin/kubectl version --client > /dev/null 2>&1; then
  install -m 0755 /work/bin/kubectl /usr/local/bin/kubectl || unmet "kubectl машины не поставлен"
  say "инструменты: kubectl — клиент машины ($(kubectl version --client 2>/dev/null | head -n 1))"
else
  say "инструменты: kubectl $RUNNER_KUBECTL — загрузкой"
  ku="https://dl.k8s.io/release/$RUNNER_KUBECTL/bin/linux/$ka/kubectl"
  { curl -fsSL --retry 3 --speed-limit 200000 --speed-time 60 -o /usr/local/bin/kubectl "$ku" &&
    echo "$(curl -fsSL --retry 3 --max-time 60 "$ku.sha256")  /usr/local/bin/kubectl" | sha256sum -c --quiet - &&
    chmod +x /usr/local/bin/kubectl; } || unmet "kubectl $RUNNER_KUBECTL не получен либо сумма не сошлась"
fi
case "$RUNNER_KIND" in
  newman)
    say "инструменты: grpcurl $RUNNER_GRPCURL, newman@$RUNNER_NEWMAN, buf $RUNNER_BUF"
    curl -fsSL --retry 3 --speed-limit 50000 --speed-time 60 "https://github.com/fullstorydev/grpcurl/releases/download/v$RUNNER_GRPCURL/grpcurl_${RUNNER_GRPCURL}_linux_$ga.tar.gz" |
      tar -xz -C /usr/local/bin grpcurl || unmet "grpcurl $RUNNER_GRPCURL"
    runner_buf "$RUNNER_BUF" /usr/local/bin > /work/buf.log 2>&1 || { tail -n 3 /work/buf.log; unmet "buf $RUNNER_BUF"; }
    say "buf $(buf --version 2>&1 | head -n 1)"
    npm i -g "newman@$RUNNER_NEWMAN" > /work/npm.log 2>&1 || { tail -n 5 /work/npm.log; unmet "newman@$RUNNER_NEWMAN"; }
    say "newman $(newman --version)" ;;
  browser)
    say "инструменты: npm ci ui-future/e2e"
    (cd /src/ui-future/e2e && npm ci) > /work/npm.log 2>&1 || { tail -n 5 /work/npm.log; unmet "npm ci ui-future/e2e"; } ;;
  *) unmet "вид раннера «$RUNNER_KIND»" ;;
esac
# Скрипты наборов читают состав дерева индексом git (git ls-files), а архив — не клон: дерево
# становится репозиторием одним коммитом; ревизия исходников — RUNNER_SHA и сообщение коммита.
say "исходники $RUNNER_SHA — репозиторием git (наборы читают состав индексом)"
( cd /src && git init -q && git add -A &&
  git -c user.name=stand-runner -c user.email=stand-runner@kacho.invalid commit -q -m "stand-runner: исходники $RUNNER_SHA" ) > /work/git.log 2>&1 ||
  { tail -n 5 /work/git.log; unmet "дерево исходников не стало репозиторием git"; }
cd /src || unmet "каталог исходников /src"
say "проброс и окружение проб — изнутри pod"
bash /src/deploy/scripts/stand-ns.sh pod-forward "$KACHO_STAND_NS" || unmet "проброс"
# shellcheck disable=SC1090
. "$KACHO_STAND_NS_WORKDIR/$KACHO_STAND_NS/probe.env" || unmet "окружение проб"
export CONSOLE_BASE="$KACHO_CONSOLE_URL" EDGE_BASE="$KACHO_EDGE_URL"
cd "/src/$RUNNER_DIR" || unmet "каталог команды «$RUNNER_DIR»"
say "команда: $RUNNER_CMD"
bash -c "$RUNNER_CMD"; rc=$?
say "команда завершилась кодом $rc"
bash /src/deploy/scripts/stand-ns.sh unforward "$KACHO_STAND_NS" > /dev/null 2>&1
finish "$rc"
POD
# runner_buf — тот же текст RUNNER_BUF_SH, что исполняет самопроверка: pod получает его функцией.
RUNNER_POD_SH="runner_buf() { bash -c $(printf '%q' "$RUNNER_BUF_SH") runner-buf \"\$@\"; }"$'\n'"$RUNNER_POD_SH"

# buf_pin — версия buf из текста e2e-newman.yml (stdin): ключ `version:` шага
# bufbuild/buf-setup-action — до начала следующего шага; печатает различные значения.
buf_pin() {
  awk '/uses:[[:space:]]*bufbuild\/buf-setup-action@/ { s = 1; next }
       s && /^[[:space:]]*-[[:space:]]/ { s = 0 }
       s && /^[[:space:]]*version:/ { v = $2; gsub(/["\047]/, "", v); print v }' | sort -u
}

# buf_version — buf_pin, ровно одна версия формы X.Y.Z, иначе отказ (код 1) с причиной.
buf_version() {
  local v
  v="$(buf_pin)"
  [[ "$v" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { warn "buf конвейера (version шага bufbuild/buf-setup-action) — не одна версия X.Y.Z: «$(tr '\n' ' ' <<<"$v")»"; return 1; }
  printf '%s\n' "$v"
}

# runner_versions SHA — «node newman grpcurl playwright buf» из дерева ревизии: каждая величина
# ровно одна и в своей форме, иначе отказ (код 1) с названной причиной.
runner_versions() {
  local sha="$1" wf pkg node newman grpc pw buf
  wf="$(git -C "$REPO_ROOT" show "$sha:.github/workflows/e2e-newman.yml" 2>/dev/null)" || { warn "e2e-newman.yml ревизии $sha не прочитан"; return 1; }
  pkg="$(git -C "$REPO_ROOT" show "$sha:ui-future/e2e/package.json" 2>/dev/null)" || { warn "ui-future/e2e/package.json ревизии $sha не прочитан"; return 1; }
  node="$(grep -oE "node-version: *['\"]?[0-9]+['\"]?" <<<"$wf" | grep -oE '[0-9]+' | sort -u)"
  newman="$(grep -oE 'newman@[0-9]+' <<<"$wf" | cut -d@ -f2 | sort -u)"
  grpc="$(grep -oE 'grpcurl/releases/download/v[0-9]+\.[0-9]+\.[0-9]+/' <<<"$wf" | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | sort -u)"
  pw="$(jq -r '(.devDependencies // {})["@playwright/test"] // (.dependencies // {})["@playwright/test"] // empty' <<<"$pkg")"
  buf="$(buf_version <<<"$wf")" || return 1
  [[ "$node" =~ ^[0-9]+$ ]] || { warn "node конвейера newman — не одна мажорная версия: «$(tr '\n' ' ' <<<"$node")»"; return 1; }
  [[ "$newman" =~ ^[0-9]+$ ]] || { warn "newman конвейера — не одна мажорная версия: «$(tr '\n' ' ' <<<"$newman")»"; return 1; }
  [[ "$grpc" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { warn "grpcurl конвейера — не одна версия: «$(tr '\n' ' ' <<<"$grpc")»"; return 1; }
  [[ "$pw" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { warn "пин @playwright/test — не точная версия: «$pw»"; return 1; }
  printf '%s %s %s %s %s\n' "$node" "$newman" "$grpc" "$pw" "$buf"
}

# runner_image ВИД NODE PLAYWRIGHT — образ раннера.
runner_image() {
  case "$1" in
    newman)  printf 'mirror.gcr.io/library/node:%s-trixie' "$2" ;;
    browser) printf 'mcr.microsoft.com/playwright:v%s-noble' "$3" ;;
    *) return 1 ;;
  esac
}

# runner_globs ВИД — отчёты, которые машина забирает из pod (пути от корня дерева).
runner_globs() {
  case "$1" in
    newman)  printf '%s' '**/tests/newman/out' ;;
    browser) printf '%s' 'ui-future/e2e/results*.json ui-future/e2e/selection.json ui-future/e2e/test-results ui-future/e2e/playwright-report' ;;
  esac
}

# Каталоги pod: исходники (команда и её отчёты) и метки. Переменные — ради самопроверки
# забора, которая исполняет те же тексты на каталогах-фикстурах.
RUNNER_SRC=/src
RUNNER_WORK=/work

# runner_exec NS АРГУМЕНТЫ… — исполнить в pod раннера. Самопроверка подменяет её фикстурой.
runner_exec() { local ns="$1"; shift; kubectl -n "$ns" exec "$RUNNER_POD" -c run -- "$@"; }
# runner_put NS ПУТЬ — stdin в файл pod (через «.tmp» и mv: читатель не видит половины).
runner_put() { kubectl -n "$1" exec -i "$RUNNER_POD" -c run -- sh -c 'cat > "$1.tmp" && mv "$1.tmp" "$1"' put "$2"; }

# ── УСЛОВИЯ С ЗАПИСЬЮ — ШАГ МАШИНЫ (kacho#3346, kacho#3348) ──────────────────────
# Учётка pod только читает (runner_judge); условие, которому нужна запись, pod просит
# машину: «<id>.req» в /work/cond — строка «имя<TAB>аргументы…». Машина исполняет команду
# строки ведомости STAND_CONDITIONS с исполнителем machine — аргументы просьбы argv-ом,
# без оболочки, каждый в форме слова без «-» впереди; пространство — только своё (env
# KACHO_STAND_NS), каталог условия — в рабочем каталоге стенда (его снимает down). Ответ —
# «<id>.out» (хвост вывода) и «<id>.rc»; строки нет либо аргумент вне формы — 125 без
# исполнения: проба читает «условие не создано», а не зелёное.
STAND_CONDITIONS="${STAND_CONDITIONS:-$HERE/stand-conditions.tsv}"

# cond_command ИМЯ — команда строки ведомости с исполнителем machine; строки нет — код 1.
cond_command() {
  awk -F'\t' -v n="$1" '!/^#/ && NF >= 4 && $1 == n && $2 == "machine" { print $4; f = 1; exit } END { exit !f }' "$STAND_CONDITIONS"
}

# runner_conds NS — исполнить просьбы pod, лежащие сейчас; печатает исход каждой.
runner_conds() {
  local ns="$1" id line cmd rc out a
  local -a ids f
  mapfile -t ids < <(runner_exec "$ns" sh -c 'cd "$1" 2>/dev/null || exit 0; for f in *.req; do [ -e "$f" ] && echo "${f%.req}"; done' ls "$RUNNER_WORK/cond" </dev/null 2>/dev/null)
  for id in "${ids[@]}"; do
    [[ "$id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]] || { warn "просьба pod с именем вне формы «$id» — пропущена"; continue; }
    line="$(runner_exec "$ns" cat "$RUNNER_WORK/cond/$id.req" </dev/null 2>/dev/null)" || continue
    runner_exec "$ns" mv "$RUNNER_WORK/cond/$id.req" "$RUNNER_WORK/cond/$id.taken" </dev/null >/dev/null 2>&1 || continue
    IFS=$'\t' read -r -a f <<<"$line"
    rc=0 out=""
    if [ "${#f[@]}" -lt 1 ] || ! cmd="$(cond_command "${f[0]}")"; then
      rc=125 out="шаг условия «${f[0]:-}» не объявлен в ведомости $(basename "$STAND_CONDITIONS") с исполнителем machine — не исполнялся"
    else
      for a in "${f[@]:1}"; do
        [[ "$a" =~ ^[A-Za-z0-9][A-Za-z0-9.,_:=-]{0,127}$ ]] || { rc=125 out="аргумент просьбы «$a» вне формы слова — шаг ${f[0]} не исполнялся"; break; }
      done
      if [ "$rc" -eq 0 ]; then
        if mkdir -p "$WORK_ROOT/$ns/cond/${f[0]}" && chmod 700 "$WORK_ROOT/$ns/cond" "$WORK_ROOT/$ns/cond/${f[0]}"; then
          log "шаг условия машины по просьбе pod: ${f[*]}"
          out="$(KACHO_STAND_NS="$ns" KACHO_STAND_COND_STATE="$WORK_ROOT/$ns/cond/${f[0]}" \
            timeout "${STAND_COND_TIMEOUT:-900}" bash "$REPO_ROOT/$cmd" "${f[@]:1}" </dev/null 2>&1)"; rc=$?
        else
          rc=125 out="каталог условия ${f[0]} в рабочем каталоге стенда не заведён — шаг не исполнялся"
        fi
      fi
    fi
    log "шаг условия ${f[0]:-?} (просьба $id): код $rc"
    { printf '%s\n' "$out" | tail -n 40 | runner_put "$ns" "$RUNNER_WORK/cond/$id.out" &&
      printf '%s\n' "$rc" | runner_put "$ns" "$RUNNER_WORK/cond/$id.rc"; } >/dev/null 2>&1 ||
      warn "ответ на просьбу $id в pod не доставлен — pod прочтёт «условие не создано» по сроку"
  done
}

# Текст pod: перечень отчётов «размер сумма путь» — то, что машина обязана получить. Собирается
# ДО передачи, из файлов по шаблонам runner_globs (раскрывает оболочка pod); тот же состав
# (NUL-разделённый) — вход tar следующего шага. Аргументы: SRC WORK ШАБЛОН…
read -r -d '' RUNNER_MANIFEST_SH <<'POD'
set -uo pipefail
src="$1" work="$2"; shift 2
cd "$src" || exit 1
shopt -s globstar nullglob
pats=("$@"); set --
# Шаблон без звезды раскрывается в себя и при отсутствии файла: отчёта, которого прогон не
# пишет (selection.json — лишь при отборе, отчёт html — не у каждого набора), в перечне нет.
for g in "${pats[@]}"; do for p in $g; do [ -e "$p" ] && set -- "$@" "$p"; done; done
{ [ $# -eq 0 ] || find "$@" -type f -print0; } | sort -zu > "$work/.fetch.files0" || exit 1
: > "$work/.manifest" || exit 1
while IFS= read -r -d '' f; do
  case "$f" in *$'\n'*) printf 'путь отчёта с переводом строки — перечнем не выразим: %q\n' "$f" >&2; exit 1 ;; esac
  printf '%s %s %s\n' "$(stat -c %s -- "$f")" "$(sha256sum < "$f" | cut -d' ' -f1)" "$f" >> "$work/.manifest" || exit 1
done < "$work/.fetch.files0"
cat "$work/.manifest"
POD

# Текст pod: поток tar ровно состава перечня. Номер процесса — признак «забор идёт» для fetched().
read -r -d '' RUNNER_TAR_SH <<'POD'
cd "$1" || exit 1
echo $$ > "$2/.fetching.pid"
exec tar -c --null --verbatim-files-from -T "${3:-$2/.fetch.files0}"
POD

# runner_fetch NS OUT — отчёты из pod в OUT со сверкой полноты (⛔ приёмки ws#1303: забор терял
# файлы молча — путей 17 из 35, целых отчётов 5 из 10, код 0). Перечень собран в pod до
# передачи; после каждой попытки каждый его путь сверяется на машине по размеру и сумме.
# Неполная доставка — повтор (STAND_RUNNER_FETCH_TRIES, умолчание 3) ЛИШЬ недоставленного:
# поток через API-сервер рвётся на длинном потоке (прогон w14: 226 и 231 из 237 при повторе
# целиком), сверенное повторно не тянется; не сошлось и после — код 1 и перечень
# недоставленного. Исход печатается всегда: «доставлено N из M».
runner_fetch() {
  local ns="$1" out="$2" tries="${STAND_RUNNER_FETCH_TRIES:-3}" man line path f size sum try n=0 m=0 prc xrc
  local -a globs lost=() st
  [[ "$tries" =~ ^[1-9][0-9]?$ ]] || { warn "STAND_RUNNER_FETCH_TRIES=«$tries» — не число 1..99"; return 1; }
  man="$out/.manifest"
  read -r -a globs <<<"$(runner_globs "$RUN_RUNNER")"
  runner_exec "$ns" bash -c "$RUNNER_MANIFEST_SH" manifest "$RUNNER_SRC" "$RUNNER_WORK" "${globs[@]}" >"$man" ||
    { warn "перечень отчётов в pod не собран — доставку сверить не с чем"; return 1; }
  while IFS= read -r line; do
    [[ "$line" =~ ^[0-9]+\ [0-9a-f]{64}\ .+$ ]] || { warn "перечень pod не в форме «размер сумма путь»: «$line»"; return 1; }
    path="${line#* * }"
    case "/$path/" in //*|*/../*) warn "путь перечня pod вне каталога отчётов: «$path»"; return 1 ;; esac
    m=$((m + 1))
  done <"$man"
  : >"$out/.fetched.list"
  if [ "$m" -eq 0 ]; then
    log "отчёты раннера: в pod файлов по шаблонам ${globs[*]} — 0, доставлять нечего"
    return 0
  fi
  local -a want=()
  for try in $(seq 1 "$tries"); do
    if [ "$try" -eq 1 ]; then
      while IFS= read -r line; do rm -f -- "$out/${line#* * }"; done <"$man"
      runner_exec "$ns" bash -c "$RUNNER_TAR_SH" fetch "$RUNNER_SRC" "$RUNNER_WORK" | tar -x -C "$out" 2>"$out/.fetch.err"
      st=("${PIPESTATUS[@]}")
    else
      local lf="$RUNNER_WORK/.fetch.retry0"
      for path in "${want[@]}"; do rm -f -- "$out/$path"; done
      printf '%s\0' "${want[@]}" | runner_put "$ns" "$lf" >/dev/null 2>&1 ||
        { warn "перечень повтора в pod не передан — повтор целиком"; lf="$RUNNER_WORK/.fetch.files0"; }
      runner_exec "$ns" bash -c "$RUNNER_TAR_SH" fetch "$RUNNER_SRC" "$RUNNER_WORK" "$lf" | tar -x -C "$out" 2>"$out/.fetch.err"
      st=("${PIPESTATUS[@]}")
    fi
    prc="${st[0]}"; xrc="${st[1]}"
    n=0; lost=(); : >"$out/.fetched.list"
    while IFS= read -r line; do
      read -r size sum _ <<<"$line"; path="${line#* * }"; f="$out/$path"
      if [ -f "$f" ] && [ ! -L "$f" ] && [ "$(stat -c %s -- "$f")" = "$size" ] && [ "$(sha256sum <"$f" | cut -d' ' -f1)" = "$sum" ]; then
        n=$((n + 1)); printf '%s\n' "$path" >>"$out/.fetched.list"
      else
        lost+=("$path")
      fi
    done <"$man"
    if [ "$n" -eq "$m" ]; then
      log "отчёты раннера: доставлено $n из $m (размер и сумма каждого сверены с перечнем pod; попытка $try из $tries) в $out"
      return 0
    fi
    warn "отчёты раннера: попытка $try из $tries — доставлено $n из $m (код tar в pod $prc, на машине $xrc: $(tail -n 1 "$out/.fetch.err"))$([ "$try" -lt "$tries" ] && printf ' — повтор недоставленного (%d)' "${#lost[@]}")"
    want=("${lost[@]}")
  done
  warn "ОТЧЁТЫ ДОСТАВЛЕНЫ НЕПОЛНО: $n из $m после $tries попыток; недоставлено либо расходится с перечнем pod (${#lost[@]}):"
  printf '  %s\n' "${lost[@]}" >&2
  return 1
}

# runner_objects NS ВИД ОБРАЗ СЕКРЕТ КАТАЛОГ КОМАНДА KUBECTL NEWMAN GRPCURL [SHA [BUF]] — учётка, роль,
# привязка и pod раннера (список JSON для kubectl apply). Всё — в пространстве NS.
runner_objects() {
  local mem=3Gi
  [ "$2" != browser ] || mem=6Gi
  jq -n --arg ns "$1" --arg kind "$2" --arg image "$3" --arg secret "$4" --arg dir "$5" --arg cmd "$6" \
    --arg kver "$7" --arg newman "$8" --arg grpc "$9" --arg sha "${10:-}" --arg buf "${11:-}" --arg host "$(ns_host "$1")" --arg sh "$RUNNER_POD_SH" \
    --arg name "$RUNNER_POD" --arg ls "$LABEL_STAND" --argjson deadline "$RUNNER_DEADLINE" --arg mem "$mem" '
    def meta: {name: $name, namespace: $ns, labels: {($ls): "test", "app.kubernetes.io/name": $name}};
    def env($n; $v): {name: $n, value: $v};
    def adm($n; $k): {name: $n, valueFrom: {secretKeyRef: {name: $secret, key: $k, optional: true}}};
    {apiVersion: "v1", kind: "List", items: [
      {apiVersion: "v1", kind: "ServiceAccount", metadata: meta},
      {apiVersion: "rbac.authorization.k8s.io/v1", kind: "Role", metadata: meta, rules: [
        {apiGroups: [""], resources: ["pods", "services", "endpoints", "secrets", "configmaps", "persistentvolumeclaims", "events", "serviceaccounts"], verbs: ["get", "list", "watch"]},
        {apiGroups: [""], resources: ["pods/portforward", "pods/exec"], verbs: ["create", "get"]},
        {apiGroups: [""], resources: ["pods/log"], verbs: ["get"]},
        {apiGroups: ["apps"], resources: ["deployments", "statefulsets", "replicasets", "daemonsets"], verbs: ["get", "list", "watch"]},
        {apiGroups: ["batch"], resources: ["jobs", "cronjobs"], verbs: ["get", "list", "watch"]},
        {apiGroups: ["cert-manager.io"], resources: ["certificates", "issuers"], verbs: ["get", "list"]}]},
      {apiVersion: "rbac.authorization.k8s.io/v1", kind: "RoleBinding", metadata: meta,
        roleRef: {apiGroup: "rbac.authorization.k8s.io", kind: "Role", name: $name},
        subjects: [{kind: "ServiceAccount", name: $name, namespace: $ns}]},
      {apiVersion: "v1", kind: "Pod", metadata: meta, spec: {
        serviceAccountName: $name, restartPolicy: "Never", activeDeadlineSeconds: $deadline,
        enableServiceLinks: false, terminationGracePeriodSeconds: 10,
        hostAliases: [{ip: "127.0.0.1", hostnames: [$host]}],
        containers: [{name: "run", image: $image, command: ["bash", "-c", $sh], workingDir: "/src",
          env: ([env("RUNNER_KIND"; $kind), env("RUNNER_DIR"; $dir), env("RUNNER_CMD"; $cmd),
                 env("RUNNER_KUBECTL"; $kver), env("RUNNER_NEWMAN"; $newman), env("RUNNER_GRPCURL"; $grpc), env("RUNNER_BUF"; $buf), env("RUNNER_SHA"; $sha),
                 env("KACHO_STAND_NS"; $ns), env("KACHO_NAMESPACE"; $ns), env("KACHO_STAND_NS_WORKDIR"; "/work/stand"), env("KACHO_STAND_COND_DIR"; "/work/cond"),
                 env("HOME"; "/work/home"), env("CI"; "true")]
                + (if $secret == "" then [] else [adm("KACHO_CLOUD_ADMIN_EMAIL"; "email"), adm("KACHO_CLOUD_ADMIN_PASSWORD"; "password")] end)),
          resources: {requests: {cpu: "500m", memory: "1Gi"}, limits: {memory: $mem}},
          securityContext: {allowPrivilegeEscalation: false},
          volumeMounts: [{name: "src", mountPath: "/src"}, {name: "work", mountPath: "/work"}, {name: "dshm", mountPath: "/dev/shm"}]}],
        volumes: [{name: "src", emptyDir: {}}, {name: "work", emptyDir: {}}, {name: "dshm", emptyDir: {medium: "Memory", sizeLimit: "1Gi"}}]}}]}'
}

# runner_judge — суд формы объектов раннера (stdin — их список): то, что держит стенд от
# раннера. Печатает находки; код 1 — есть находки. Его зовут runner_run до применения и
# self-test (на настоящем выводе runner_objects и на близнецах с дефектом).
runner_judge() {
  python3 -c '
import json, sys
ns, host = sys.argv[1], sys.argv[2]
items = json.load(sys.stdin)["items"]
bad = []
kinds = sorted(i["kind"] for i in items)
if kinds != ["Pod", "Role", "RoleBinding", "ServiceAccount"]:
    bad.append("виды объектов %s, ждали Pod, Role, RoleBinding, ServiceAccount" % kinds)
for i in items:
    if i["metadata"].get("namespace") != ns:
        bad.append("%s/%s вне пространства стенда" % (i["kind"], i["metadata"].get("name")))
# Связи судятся по ИМЕНАМ объектов списка: роль привязки — роль раннера, субъект и учётка pod —
# учётка раннера. Иначе узкая роль лежит рядом, а права даёт чужая роль пространства либо pod
# идёт под чужой учёткой (default, учётка чарта) — суд формы о ней молчал бы (⛔ приёмки ws#1303).
sas = [i["metadata"].get("name") for i in items if i["kind"] == "ServiceAccount"]
roles = [i["metadata"].get("name") for i in items if i["kind"] == "Role"]
for i in items:
    if i["kind"] == "RoleBinding":
        if i["roleRef"].get("kind") != "Role":
            bad.append("привязка к %s, а не к роли пространства" % i["roleRef"].get("kind"))
        elif i["roleRef"].get("name") not in roles:
            bad.append("привязка к роли %s, а не к роли раннера %s" % (i["roleRef"].get("name"), roles))
        for s in i["subjects"]:
            if s.get("kind") != "ServiceAccount" or s.get("namespace") != ns:
                bad.append("субъект привязки вне учётки pod пространства: %s" % s)
            elif s.get("name") not in sas:
                bad.append("субъект привязки — учётка %s, а не учётка раннера %s" % (s.get("name"), sas))
    if i["kind"] == "Role":
        for r in i["rules"]:
            extra = set(r["verbs"]) - {"get", "list", "watch", "create"}
            if extra:
                bad.append("роль даёт %s на %s" % (sorted(extra), r["resources"]))
            if "create" in r["verbs"] and set(r["resources"]) - {"pods/portforward", "pods/exec"}:
                bad.append("create — шире проброса и exec: %s" % r["resources"])
            if "*" in r["resources"] or "*" in r["verbs"] or "*" in r.get("apiGroups", []):
                bad.append("роль со звездой: %s" % r)
    if i["kind"] == "Pod":
        sp = i["spec"]
        if sp.get("serviceAccountName") not in sas:
            bad.append("pod под учёткой %s, а не учёткой раннера %s" % (sp.get("serviceAccountName"), sas))
        if "nodeName" in sp:
            bad.append("pod прибит к узлу %s мимо планировщика" % sp["nodeName"])
        for k in ("hostNetwork", "hostPID", "hostIPC"):
            if sp.get(k):
                bad.append("pod: %s" % k)
        for v in sp.get("volumes", []):
            if "hostPath" in v:
                bad.append("pod: том hostPath %s" % v.get("name"))
        if sp.get("hostAliases") != [{"ip": "127.0.0.1", "hostnames": [host]}]:
            bad.append("имя консоли не на петлю pod: %s" % sp.get("hostAliases"))
        for c in sp.get("containers", []) + sp.get("initContainers", []):
            sc = c.get("securityContext") or {}
            if sc.get("privileged") or (sc.get("capabilities") or {}).get("add"):
                bad.append("контейнер %s: privileged либо добавленные возможности" % c["name"])
            if "limits" not in (c.get("resources") or {}) or "memory" not in c["resources"]["limits"]:
                bad.append("контейнер %s без предела памяти" % c["name"])
            for e in c.get("env", []):
                ref = (e.get("valueFrom") or {}).get("secretKeyRef")
                if ref is not None and ref.get("optional") is not True:
                    bad.append("секрет %s обязателен — цепочка без администратора не подняла бы pod" % ref.get("name"))
for b in bad:
    print("RUNNER-FORM\t" + b)
sys.exit(1 if bad else 0)
' "$1" "$(ns_host "$1")"
}

# pod_guard NS — pod-forward исполняется лишь изнутри pod раннера своего пространства:
# учётка pod есть и её пространство — NS. Дальше kubectl скрипта идёт учёткой pod.
pod_guard() {
  local ns="$1" own f=/var/run/secrets/kubernetes.io/serviceaccount/namespace
  ns_valid "$ns" >/dev/null || die "имя «$ns» не по правилу" 2
  [ -n "${KUBERNETES_SERVICE_HOST:-}" ] && [ -r "$f" ] || die "pod-forward — лишь изнутри pod раннера (учётки pod нет); с машины — forward" 2
  own="$(cat "$f")"
  [ "$own" = "$ns" ] || die "pod раннера в пространстве «$own», а не «$ns»" 2
  STAND_IN_POD=1 STAND_CTX=in-pod
}

runner_explain() {
  local ns="$1"
  warn "pod раннера: $(kubectl -n "$ns" get pod "$RUNNER_POD" -o jsonpath='{.status.phase} {range .status.containerStatuses[*]}{.state}{end}' 2>/dev/null | cut -c1-400)"
  kubectl -n "$ns" get events --field-selector "involvedObject.name=$RUNNER_POD" --sort-by=.lastTimestamp 2>/dev/null | tail -n 5 >&2
}

# runner_run NS REF КОМАНДА… — исполнить команду в pod раннера (раздел выше). Код — код
# команды; 125 — не исполнялась.
runner_run() {
  local ns="$1" ref="$2" sha vers node newman grpc pw buf image kver rel secret out rc="" phase miss=0
  shift 2
  # Исходники — коммит рабочей копии (пробы и их окружение — его), образы — ревизии REF
  # (правило их расхождения — ref_guard подъёма); правки вне коммита pod не увидел бы — их
  # отклонил cmd_run до подъёма.
  sha="$(git -C "$REPO_ROOT" rev-parse --verify 'HEAD^{commit}' 2>/dev/null)" || { warn "голова рабочей копии не разрешается"; return "$RUN_UNMET"; }
  vers="$(runner_versions "$sha")" || return "$RUN_UNMET"
  read -r node newman grpc pw buf <<<"$vers"
  kver="$(kubectl version -o json --request-timeout=30s 2>/dev/null | jq -r '.serverVersion.gitVersion // empty')"
  [[ "$kver" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { warn "версия сервера кластера не прочитана («$kver») — kubectl pod не выбрать"; return "$RUN_UNMET"; }
  image="$(runner_image "$RUN_RUNNER" "$node" "$pw")"
  rel="$(realpath --relative-to="$REPO_ROOT" "$PWD")"
  case "$rel" in ..*|/*) warn "каталог команды вне клона ($PWD)"; return "$RUN_UNMET" ;; .) rel="" ;; esac
  secret="$(bootstrap_secret_of "$WORK_ROOT/$ns/render.yaml" 2>/dev/null)" || secret=""
  out="${RUN_OUT:-$WORK_ROOT/out/$ns}"
  mkdir -p "$out" || { warn "каталог отчётов $out не заведён"; return "$RUN_UNMET"; }
  local objs
  objs="$(runner_objects "$ns" "$RUN_RUNNER" "$image" "$secret" "$rel" "$(printf '%q ' "$@")" "$kver" "$newman" "$grpc" "$sha" "$buf")" ||
    { warn "объекты раннера не построены"; return "$RUN_UNMET"; }
  runner_judge "$ns" <<<"$objs" >&2 || { warn "форма объектов раннера не прошла суд (выше)"; return "$RUN_UNMET"; }
  stand_nodes "раннер $RUN_RUNNER"
  kubectl apply -f - <<<"$objs" >/dev/null || { warn "объекты раннера не применены"; return "$RUN_UNMET"; }
  log "раннер $RUN_RUNNER: образ $image, kubectl $kver, исходники $sha, образы стенда — ревизии $ref${secret:+, администратор облака — секрет $secret}"
  kubectl -n "$ns" wait --for=condition=Ready "pod/$RUNNER_POD" --timeout="${STAND_RUNNER_READY_WAIT:-600}s" >/dev/null ||
    { runner_explain "$ns"; warn "pod раннера не готов — команда НЕ исполнялась"; return "$RUN_UNMET"; }
  # Сжатием: дерево — ~150 МБ tar, поток идёт через API-сервер (замер kacho#3346: 158 МБ tar → 33 МБ gzip -1).
  git -C "$REPO_ROOT" archive --format=tar "$sha" | gzip -1 | kubectl -n "$ns" exec -i "$RUNNER_POD" -c run -- tar -xz -C /src ||
    { warn "исходники ревизии $sha в pod не доставлены"; return "$RUN_UNMET"; }
  # kubectl машины — тем же потоком (шапка текста pod): лишь при той же архитектуре узла.
  if [ "$(kubectl -n "$ns" exec "$RUNNER_POD" -c run -- uname -m 2>/dev/null)" = "$(uname -m)" ]; then
    gzip -1 <"$(type -P kubectl)" | kubectl -n "$ns" exec -i "$RUNNER_POD" -c run -- sh -c 'mkdir -p /work/bin && gzip -dc > /work/bin/kubectl && chmod 0755 /work/bin/kubectl' ||
      warn "kubectl машины в pod не доставлен — pod загрузит версию сервера"
  fi
  kubectl -n "$ns" exec "$RUNNER_POD" -c run -- touch /work/.go || { warn "pod не принял сигнал старта"; return "$RUN_UNMET"; }
  kubectl -n "$ns" logs -f "pod/$RUNNER_POD" -c run & RUN_LOGS=$!
  while :; do
    rc="$(kubectl -n "$ns" exec "$RUNNER_POD" -c run -- cat /work/.rc 2>/dev/null)"
    [[ "$rc" =~ ^[0-9]+$ ]] && break
    rc=""
    runner_conds "$ns"
    phase="$(kubectl -n "$ns" get pod "$RUNNER_POD" -o jsonpath='{.status.phase}' 2>/dev/null)"
    case "$phase" in
      Running|Pending) miss=0 ;;
      "") miss=$((miss + 1)); [ "$miss" -lt 30 ] || { warn "состояние pod не читается 30 опросов подряд"; break; } ;;
      *) warn "pod раннера вышел из работы (фаза «$phase») до кода команды"; break ;;
    esac
    sleep 10
  done
  local fetch=0
  runner_fetch "$ns" "$out" || fetch=1
  kubectl -n "$ns" exec "$RUNNER_POD" -c run -- touch /work/.fetched >/dev/null 2>&1
  for _ in 1 2 3 4 5 6 7 8 9 10; do kill -0 "$RUN_LOGS" 2>/dev/null || break; sleep 1; done
  kill "$RUN_LOGS" 2>/dev/null; wait "$RUN_LOGS" 2>/dev/null; RUN_LOGS=""
  [ -n "$rc" ] || { runner_explain "$ns"; warn "кода команды из pod нет — команда не исполнилась до конца: не выполнилось"; return "$RUN_UNMET"; }
  [ "$fetch" -eq 0 ] || { warn "команда в pod завершилась кодом $rc, но её отчёты доставлены неполно (выше) — вердикт по ним не читается: не выполнилось (125)"; return "$RUN_UNMET"; }
  return "$rc"
}

# ── ПРОБРОС ──────────────────────────────────────────────────────────────────

cmd_forward() {
  local ns="$1" port work host lp
  ns_valid "$ns" >/dev/null || die "имя «$ns» не по правилу" 2
  need_tools; guard_context
  # Учётка pod раннера пространств не читает (роль — в своём пространстве), а что она
  # в нём, доказал pod_guard.
  [ "$STAND_IN_POD" = 1 ] || [ "$(ns_state "$ns")" = test ] || die "тестового пространства $ns нет" 1
  port="$(ns_port "$ns")"; host="$(ns_host "$ns")"; work="$WORK_ROOT/$ns"; mkdir -p "$work"
  local KUBE_ARGV; kube_argv
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
    nohup "${KUBE_ARGV[@]}" -n "$ns" port-forward "svc/$svc" "$lp:$rport" >"$work/forward-$svc.log" 2>&1 &
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
  # Дверь чтения писем в форме приёмника собственного стенда службы личности
  # (`/messages`, `/codes`): её читают наборы и посевы службы (kaname#684). Порт —
  # свободный, а не четвёртый подряд: порты стендов выделены по три, следующий —
  # консоль соседнего стенда.
  local door_port="" door_pf="$work/mailbox-door.port"
  rm -f "$door_pf"
  nohup python3 "$HERE/stand-mailbox-door.py" "http://127.0.0.1:$((port + 2))" --port-file "$door_pf" \
    >"$work/mailbox-door.log" 2>&1 &
  echo $! >>"$work/forward.pids"
  for _ in $(seq 1 50); do [ -s "$door_pf" ] && break; sleep 0.2; done
  door_port="$(cat "$door_pf" 2>/dev/null)"
  [[ "$door_port" =~ ^[0-9]+$ ]] &&
    [ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://127.0.0.1:$door_port/healthz")" = 200 ] || {
    tail -n 5 "$work/mailbox-door.log" >&2
    cmd_unforward "$ns" >/dev/null 2>&1
    die "дверь чтения писем стенда не поднялась (журнал $work/mailbox-door.log)" 1
  }
  # Исполнитель записи в базу службы личности стенда — «до psql включительно»,
  # форма --store-exec её посевов (seed_key_person, seed_stored_value): «Дано»,
  # строящиеся записью, на этом стенде иначе не строятся (kaname#684). Только
  # пространство стенда: имя kacho сюда не доходит (ns_valid выше).
  local store_exec
  if [ "$STAND_IN_POD" = 1 ]; then
    store_exec="$(printf '%q ' kubectl -n "$ns" exec -i "statefulset/$RELEASE-pg-iam" -- psql -U iam -d kaname)"
  else
    store_exec="$(printf '%q ' kubectl --kubeconfig "$STAND_KUBECONFIG" --context "$STAND_CTX" -n "$ns" \
      exec -i "statefulset/$RELEASE-pg-iam" -- psql -U iam -d kaname)"
  fi
  cat >"$work/probe.env" <<EOF
export KACHO_CONSOLE_URL=https://$host:$port
export KACHO_CONSOLE_HOST_IP=127.0.0.1
export KACHO_CONSOLE_CA=$work/console-leaf.pem
export NODE_EXTRA_CA_CERTS=$work/console-ca.pem
export KACHO_CONSOLE_MAILBOX_URL=http://127.0.0.1:$((port + 2))
export KACHO_STAND_MAILBOX_URL=http://127.0.0.1:$door_port
export KACHO_EDGE_URL=https://127.0.0.1:$((port + 1))
EOF
  # Значение — слова команды, каждое в форме оболочки (%q): файл окружения читают
  # «.», а посев режет значение по правилам оболочки (shlex.split).
  printf 'export KACHO_KANAME_STORE_EXEC=%q\n' "${store_exec% }" >>"$work/probe.env"
  log "проброс поднят; окружение проб: . $work/probe.env"
}

cmd_unforward() {
  local ns="$1" work="$WORK_ROOT/$1" pid
  [ -f "$work/forward.pids" ] || return 0
  while read -r pid; do kill "$pid" 2>/dev/null || true; done <"$work/forward.pids"
  rm -f "$work/forward.pids"
  log "проброс $ns снят"
}

# ── ПЕРЕКАТКА ОДНОЙ СЛУЖБЫ (kacho#3260) ──────────────────────────────────────
#
# Правка ветки запроса чаще всего меняет код ОДНОЙ службы, а пересоздание стенда
# стоит подъёма целиком (посев, администратор облака, гейт посадки). Перекатка
# меняет образ ровно у тех контейнеров своего стенда, чей образ — образ этой
# службы (включая init-контейнер миграций: у служб он того же образа), ждёт
# раскатки и доказывает провенанс КАЖДОГО пода службы против новой ревизии.
#
# Границы, названные вслух:
#   * чарт не перерисовывается: менялся между ревизией стенда и новой ревизией
#     чарт (deploy/helm, `<часть>/deploy/` — шаблоны, values, Chart) — отказ,
#     перекатке подлежит только образ, а стенд с новым образом и старым чартом
#     исполнял бы смесь; подними стенд заново (`stand-ns-up` поверх своего
#     пространства перекатывает релиз);
#   * образ, приезжающий пином (часть с вынесенными исходниками), не перекатывается:
#     его собирает конвейер своего репозитория;
#   * контейнеры выбираются по ИМЕНИ ОБРАЗА (последний сегмент без тега), а не по
#     имени развёртывания: рукописная цепочка «служба → развёртывание» уже
#     расходилась с чартом (kacho#2122).

# reload_targets ОБРАЗ — по JSON рабочих нагрузок на входе печатает
# «вид/имя<TAB>контейнер» каждого контейнера (и init-контейнера), чей образ
# называется ровно ОБРАЗ. Сравнение — по имени целиком: `kacho-vpc-extras` и
# `kacho-ui-future-vpc` образом `kacho-vpc` не являются.
reload_targets() {
  jq -r --arg img "$1" '
    def name_of: sub("@.*$"; "") | split("/") | last | sub(":.*$"; "");
    .items[] | . as $w
    | ((($w.spec.template.spec.initContainers // []) + ($w.spec.template.spec.containers // []))[])
    | select((.image | name_of) == $img)
    | "\($w.kind | ascii_downcase)/\($w.metadata.name)\t\(.name)"'
}

# reload_chart_changed OLD NEW — 0: чарт между ревизиями менялся (пути напечатаны),
# 1: не менялся, 2: сравнить не удалось. Предмет — то, что рисует манифест:
# зонтик целиком и `deploy/` каждой части (шаблоны, values, Chart).
reload_chart_changed() {
  local out
  out="$(git -C "$REPO_ROOT" diff --name-only "$1" "$2" -- 'deploy/helm' \
    ':(glob)**/deploy/templates/**' ':(glob)**/deploy/values*.yaml' \
    ':(glob)**/deploy/Chart.yaml' ':(glob)**/deploy/Chart.lock' 2>&1)" || { printf '%s\n' "$out" >&2; return 2; }
  [ -n "$out" ] || return 1
  printf '%s\n' "$out"
  return 0
}

# normalize_image REF — ссылка без умолчаний реестра (их дописывает containerd).
normalize_image() { local r="${1#docker.io/}"; r="${r#index.docker.io/}"; printf '%s' "${r#library/}"; }

cmd_reload() {
  local ns="$1" svc="${2:-}" ref="${3:-HEAD}" sha img state was js targets t c kinds
  local -a args
  ns_valid "$ns" >/dev/null || die "имя «$ns» не по правилу t<номер>-<коротко>; пространство kacho перекатка не трогает" 2
  [ -n "$svc" ] || die "reload: служба не названа (SVC=<служба>)" 2
  sha="$(git -C "$REPO_ROOT" rev-parse --verify "${ref}^{commit}" 2>/dev/null)" || die "ревизия «$ref» не разрешается в коммит" 2
  # shellcheck source=deploy/scripts/lib/product-names.sh
  . "$HERE/lib/product-names.sh"
  img="$(product_image_name "$svc")" || die "имя образа службы «$svc» не прочитано (выше)" 2
  product_pins_load >/dev/null || die "ведомость пинов не прочитана — приезжает ли $img пином, неизвестно" 2
  if product_image_is_pinned "$img"; then
    die "образ $img приезжает пином: исходников в этом дереве нет, собирает его конвейер своего репозитория — перекатывать нечего" 2
  fi
  need_tools; guard_context
  state="$(ns_state "$ns")" || die "состояние пространства $ns НЕ ПРОЧИТАНО — кластер не ответил" 2
  [ "$state" = test ] || die "пространство $ns — «$state», а не стенд проб: перекатывать нечего либо не вправе" 2
  was="$(kubectl get namespace "$ns" -o json --request-timeout=30s | jq -r '.metadata.annotations["kacho.io/ref"] // ""')"
  [ -n "$was" ] || die "у пространства $ns нет kacho.io/ref — ревизия стенда неизвестна, сравнивать чарт не с чем" 2
  local crc=0 changed
  changed="$(reload_chart_changed "$was" "$sha")" || crc=$?
  case "$crc" in
    1) ;;
    0) die "между ревизией стенда $was и $sha менялся чарт — перекатка образа его не применит:
$changed
подними стенд заново: make -C deploy stand-ns-up NS=$ns REF=$sha" 2 ;;
    *) die "чарт ревизий $was и $sha не сравнён (выше) — «не сравнил» не равно «не менялся»" 2 ;;
  esac
  pick_image_tags "$sha" "$img" || die "образа $img ревизии $sha нет (выше) — перекатывать не на что" 2
  local tag ref_new
  tag="$(awk -F'\t' -v i="$img" '$1 == i { print $2; exit }' <<<"$PICKED_TAGS")"
  ref_new="$IMAGE_PREFIX/$img:$tag"

  js="$(kubectl -n "$ns" get deployment,statefulset,daemonset -o json --request-timeout=30s)" ||
    die "рабочие нагрузки $ns не прочитаны — кластер не ответил" 2
  targets="$(reload_targets "$img" <<<"$js")" || die "рабочие нагрузки $ns не разобраны" 2
  [ -n "$targets" ] || die "в $ns нет контейнера с образом $img — служба «$svc» на стенде не развёрнута" 2
  kinds="$(cut -f1 <<<"$targets" | sort -u)"
  log "перекатка $svc в $ns: $was → $sha, образ $ref_new (ветка публикации $IMAGE_BRANCH); контейнеров $(wc -l <<<"$targets") в нагрузках: $(tr '\n' ' ' <<<"$kinds")"

  stand_nodes "перекатка $svc"
  for t in $kinds; do
    args=()
    while IFS=$'\t' read -r _ c; do args+=("$c=$ref_new"); done < <(awk -F'\t' -v t="$t" '$1 == t' <<<"$targets")
    kubectl -n "$ns" set image "$t" "${args[@]}" >/dev/null || die "образ $t не заменён" 1
    kubectl -n "$ns" annotate "$t" --overwrite "kacho.io/ref=$sha" >/dev/null || die "ревизия $t не записана" 1
  done
  for t in $kinds; do
    kubectl -n "$ns" rollout status "$t" --timeout=5m || die "$t не раскатился за 5 мин" 1
  done

  # Провенанс — у работающих контейнеров, файлом образа (stand-provenance.sh,
  # шапка): ссылка пода говорит, ЧТО просили, файл — из чего собрано.
  local pods p cname cimg rev agree=0 bad=0 stale=0
  pods="$(kubectl -n "$ns" get pods -o json --request-timeout=30s | jq -r --arg img "$img" '
    def name_of: sub("@.*$"; "") | split("/") | last | sub(":.*$"; "");
    .items[] | select(.status.phase == "Running" and .metadata.deletionTimestamp == null) | . as $p
    | .status.containerStatuses[]? | select((.image | name_of) == $img)
    | "\($p.metadata.name)\t\(.name)\t\(.image)"')" || die "поды $ns не прочитаны" 1
  while IFS=$'\t' read -r p cname cimg; do
    [ -n "$p" ] || continue
    if [ "$(normalize_image "$cimg")" != "$(normalize_image "$ref_new")" ]; then
      stale=$((stale + 1)); warn "под $p/$cname исполняет $cimg, а не $ref_new"; continue
    fi
    rev="$(kubectl -n "$ns" exec "$p" -c "$cname" --request-timeout=30s -- cat /etc/kacho/image-revision 2>/dev/null | tr -d '\r\n')"
    if [ "${rev%-dirty}" = "$sha" ]; then agree=$((agree + 1))
    else bad=$((bad + 1)); warn "под $p/$cname: ревизия «${rev:-не прочитана}», ждали $sha"; fi
  done <<<"$pods"
  log "провенанс $svc: контейнеров с $img — сходится $agree, расходится $bad, на прежнем образе $stale"
  { [ "$agree" -gt 0 ] && [ "$bad" -eq 0 ] && [ "$stale" -eq 0 ]; } ||
    die "провенанс перекатки $svc не сошёлся с $sha" 3
  log "служба $svc в $ns перекачена на $sha; прочие части стенда не тронуты"
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
  stand_nodes "снятие $ns"
  cmd_unforward "$ns" >/dev/null 2>&1
  state="$(ns_state "$ns")" || die "состояние пространства $ns НЕ ПРОЧИТАНО — кластер не ответил; «не прочитал» не равно «нет»" 2
  [ "$state" != foreign ] || die "пространство $ns НЕ помечено $LABEL_STAND=test — оно не стенд проб, и снимать его эта цель не вправе" 1
  if [ "$state" = test ] && helm status "$RELEASE" -n "$ns" >/dev/null 2>&1; then
    log "helm uninstall $RELEASE -n $ns (уносит свои издатели CA и корень CA)"
    helm uninstall "$RELEASE" -n "$ns" --wait --timeout 5m || warn "helm uninstall отказал — помеченное снимается ниже по метке"
    stand_nodes "снятие $ns: помеченное и пространство"
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

# Подключённый (`. stand-ns.sh`) — только определения: доказательство инъекцией
# (stand-pr-images-inject.sh) зовёт функции напрямую, без кластера. Из потока
# ввода (`bash -s`, пробы слоя стенда в deploy/*_test.go) пути у текста нет и
# BASH_SOURCE пуст: под `set -u` голое обращение роняло текст на этой строке
# (сведение kacho#3260 с пробами ветки эпика) — пустое читается как «исполнен».
[ "${BASH_SOURCE[0]:-$0}" = "$0" ] || return 0

case "${1:-}" in
  up)        shift; [ -n "${1:-}" ] || die "up: не названо пространство (NS=t<задача>-<коротко>)" 2; cmd_up "$@" ;;
  run)       shift; [ -n "${1:-}" ] || die "run: не названо пространство" 2; cmd_run "$@" ;;
  down)      shift; [ -n "${1:-}" ] || die "down: не названо пространство" 2; cmd_down "$1" ;;
  census)    shift; cmd_census "$@" ;;
  forward)   shift; [ -n "${1:-}" ] || die "forward: не названо пространство" 2; cmd_forward "$1" ;;
  pod-forward) shift; [ -n "${1:-}" ] || die "pod-forward: не названо пространство" 2; pod_guard "$1"; cmd_forward "$1" ;;
  unforward) shift; [ -n "${1:-}" ] || die "unforward: не названо пространство" 2; cmd_unforward "$1" ;;
  reload)    shift; [ -n "${1:-}" ] || die "reload: не названо пространство" 2; cmd_reload "$@" ;;
  self-test) self_test ;;
  *) die "режим не назван: up | down | run | census | forward | pod-forward | unforward | reload | self-test" 2 ;;
esac
