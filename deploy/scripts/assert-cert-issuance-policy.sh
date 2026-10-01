#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# СЕКЦИЯ E ГЕЙТА ПОСАДКИ: ПОЛИТИКА ВЫПУСКА СЕРТИФИКАТОВ СЛУЖБ (приёмка NTF-1,
# раздел «NS», NTF1-J04; замысел З30).
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ЭТО ВОПРОС ПОСАДКИ
#
# notify решает, чья это служба, по идентичности в её сертификате, а kaname по
# ней же отвечает, может ли служба слать письма от своего пространства. Если
# сертификат с идентичностью notify может получить любая учётка кластера, то
# разделение прав служб держится на честном слове. Поэтому notify, поднятый без
# политики выпуска, — не «работает без одной защиты», а работает БЕЗ ТОЙ,
# на которой стоит его модель прав. Включение notify без политики — красный:
#
#     ✗ notify требует политики выпуска сертификатов служб
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ЗНАЧИТ «ПОЛИТИКА ЕСТЬ» — ЧЕТЫРЕ НАБЛЮДЕНИЯ, И НИ ОДНО НЕ ИЗ VALUES
#
#   1. выпускающий внутренний УЦ найден в кластере (ClusterIssuer зонтика);
#   2. обе ветви политики (`workload` — запрос подаёт учётка службы сама,
#      `certificates` — запрос подаёт контроллер cert-manager за объект
#      Certificate) существуют, смотрят на ЭТОТ УЦ и приняты контроллером
#      политики (Ready=True — он их разобрал и исполняет);
#   3. контроллер политики жив (Deployment Available);
#   4. ВСТРОЕННЫЙ одобритель cert-manager НЕ вправе одобрять запросы этого УЦ.
#
# Четвёртое — главное и самое лёгкое для забвения. Политика, которая отказывает,
# пока встроенный одобритель того же cert-manager одобряет всё подряд, ничего не
# решает: запрос одобряет тот, кто успел первым. Право одобрения — это RBAC на
# `signers` (так его проверяет сам вебхук cert-manager), и гейт спрашивает
# ровно его: SubjectAccessReview от имени учётки контроллера cert-manager на оба
# имени, которые вебхук признаёт, — точное `clusterissuers.cert-manager.io/<УЦ>`
# и подстановочное `clusterissuers.cert-manager.io/*`.
#
# ─────────────────────────────────────────────────────────────────────────────
# notify НЕ поднят — требование не предъявляется, но состояние политики
# ПЕЧАТАЕТСЯ: стенд, на котором notify включат следующим, узнаёт об отсутствии
# политики не первым красным.
#
# Предикат вердикта — чистая функция над наблюдениями. Самопроверка кормит её
# синтетическими наблюдениями без кластера (`--self-check`), а доказательство
# инъекцией — `deploy/scripts/cert-issuance-policy-verdict-inject.sh`.
# Поведение политики (чужой SAN не выпускается, свой — выпускается) судит
# не этот гейт, а проба `deploy/tests/cluster/cert-issuance-policy-test.sh`
# (NTF1-J01, J02): гейт посадки отвечает на вопрос «стоит ли и исполняется ли».
# Что гейт видит поднятый notify сквозь настоящий кластер (нагрузки в формах
# соседних чартов; красный без политики, зелёный с ней) — проба
# `deploy/tests/cluster/cert-issuance-gate-notify-presence-test.sh`.
set -uo pipefail

NS="${NS:-kacho}"
# Признак «notify поднят» — ИДЕНТИЧНОСТЬ, а не ярлык. Политика защищает SAN
# `ns/<ns>/sa/<учётка notify>`, и notify поднят ровно тогда, когда в
# пространстве стенда есть рабочая нагрузка (Deployment, StatefulSet, DaemonSet),
# чей под работает под этой учёткой. Учётка — `saName` декларации
# `notify.spiffe` (решение Р12; литерал `kacho-notify` закреплён приёмкой:
# NTF1-J03 `notify.spiffe = {T, N, kacho-notify}`, NTF4-117). Метки чарта
# признаком не служат: соседние чарты метят деплойменты `kacho-<svc>` или не
# метят вовсе, и селектор по метке, которую никто не производит, читал бы
# «notify не поднят» на поднятом notify (находка ревью 2915-J1, F1).
#
# Вторая, страховочная половина — имя: нагрузка с именем или меткой имени
# `notify`, `kacho-notify`, `notify-<что угодно>`, `kacho-notify-<…>` признаётся
# notify при ЛЮБОЙ учётке. Ошибка признания здесь несимметрична: лишнее «поднят»
# даёт громкий красный при отсутствии политики, недосчёт — молчаливый зелёный.
#
# Согласие с деревом держит проба рендера (deploy/tests/helm/
# cert-issuance-policy-render-test.sh, правило 5): декларация `notify.spiffe`
# обязана называть ту же учётку, а каждая нагрузка из чарта notify в каждой
# цепочке — признаваться этим же классификатором (`--classify-notify`).
NOTIFY_SA="kacho-notify"
NOTIFY_NAME_RE='^(kacho-)?notify(-[a-z0-9]([-a-z0-9]*[a-z0-9])?)?$'
POLICY_SELECTOR="kacho.cloud/component=cert-issuance-policy"
ISSUER_SELECTOR="kacho.cloud/component=cert-manager-internal-ca"
APPROVER_SELECTOR="app.kubernetes.io/name=cert-manager-approver-policy"
CERT_MANAGER_SELECTOR="app.kubernetes.io/name=cert-manager,app.kubernetes.io/component=controller"
HEADLINE="notify требует политики выпуска сертификатов служб"

# ═════════════════════════════════════════════════════════════════════════════
# ПРЕДИКАТ ВЕРДИКТА — ЧИСТАЯ ФУНКЦИЯ НАД НАБЛЮДЕНИЯМИ.
#
# verdict <notify> <issuer> <workload> <certificates> <approver> <builtin>
#   notify       present | absent | unread
#   issuer       <имя УЦ> | absent | ambiguous | unread
#   workload     ready | notready | absent | unread        (ветвь `workload`)
#   certificates ready | notready | absent | unread        (ветвь `certificates`)
#   approver     available | unavailable | absent | unread
#   builtin      no | yes | absent | unread                (может ли встроенный
#                                                           одобритель одобрить)
# Печатает строки ✓/✗; код 0 — зелено, 1 — красно.
#
# У классификатора нет корзины «прочее»: неузнанное значение наблюдения — это
# красный с его текстом, а не «наверное, нормально».
# ═════════════════════════════════════════════════════════════════════════════
verdict() {
  local notify="$1" issuer="$2" workload="$3" certificates="$4" approver="$5" builtin="$6"
  local -a gaps=()

  case "$issuer" in
    absent)    gaps+=("выпускающего внутреннего УЦ нет (ClusterIssuer с меткой $ISSUER_SELECTOR)") ;;
    ambiguous) gaps+=("внутренних УЦ больше одного — какой из них под политикой, не установить") ;;
    unread)    gaps+=("выпускающий внутренний УЦ НЕ ПРОЧИТАН — «не прочитано» не значит «нет»") ;;
    "")        gaps+=("имя выпускающего УЦ пусто") ;;
  esac
  local arm state
  for arm in workload certificates; do
    if [ "$arm" = workload ]; then state="$workload"; else state="$certificates"; fi
    case "$state" in
      ready)    ;;
      notready) gaps+=("ветвь политики «$arm» есть, но контроллер политики её НЕ принял (Ready≠True)") ;;
      absent)   gaps+=("ветви политики «$arm» для этого УЦ нет") ;;
      unread)   gaps+=("ветвь политики «$arm» НЕ ПРОЧИТАНА") ;;
      *)        gaps+=("ветвь политики «$arm»: неузнанное наблюдение «$state»") ;;
    esac
  done
  case "$approver" in
    available)   ;;
    unavailable) gaps+=("контроллер политики выпуска не Available — решать запросы некому") ;;
    absent)      gaps+=("контроллера политики выпуска в кластере нет") ;;
    unread)      gaps+=("контроллер политики выпуска НЕ ПРОЧИТАН") ;;
    *)           gaps+=("контроллер политики: неузнанное наблюдение «$approver»") ;;
  esac
  case "$builtin" in
    no)     ;;
    yes)    gaps+=("встроенный одобритель cert-manager ВПРАВЕ одобрять запросы этого УЦ — политика декоративна: одобряет тот, кто успел первым") ;;
    absent) gaps+=("контроллер cert-manager не найден — чьё право одобрения проверять, не установить") ;;
    unread) gaps+=("право одобрения встроенного одобрителя НЕ ПРОЧИТАНО") ;;
    *)      gaps+=("встроенный одобритель: неузнанное наблюдение «$builtin»") ;;
  esac

  case "$notify" in
    present)
      if [ "${#gaps[@]}" -eq 0 ]; then
        echo "  ✓ notify поднят, и политика выпуска сертификатов служб действует: УЦ $issuer, ветви workload и certificates приняты, встроенный одобритель этот УЦ не одобряет"
        return 0
      fi
      echo "  ✗ $HEADLINE:"
      local g; for g in "${gaps[@]}"; do echo "      - $g"; done
      return 1 ;;
    absent)
      if [ "${#gaps[@]}" -eq 0 ]; then
        echo "  ✓ notify не поднят; политика выпуска действует (УЦ $issuer) — включение notify ею не блокируется"
      else
        echo "  ✓ notify не поднят — требование политики выпуска не предъявляется. Состояние политики (включение notify здесь покраснеет):"
        local g; for g in "${gaps[@]}"; do echo "      - $g"; done
      fi
      return 0 ;;
    unread)
      echo "  ✗ поднят ли notify, НЕ ПРОЧИТАНО — без этого требование политики выпуска не оценить"
      return 1 ;;
    *)
      echo "  ✗ наличие notify: неузнанное наблюдение «$notify»"
      return 1 ;;
  esac
}

# ═════════════════════════════════════════════════════════════════════════════
# САМОПРОВЕРКА — предикат на синтетических наблюдениях, без кластера.
# ═════════════════════════════════════════════════════════════════════════════
self_check() {
  local bad=0 n=0 out rc
  expect() { # <ждём: 0|1> <фраза или -> <наблюдения…>
    local want="$1" phrase="$2"; shift 2
    n=$((n + 1))
    out="$(verdict "$@")"; rc=$?
    if [ "$rc" != "$want" ]; then
      echo "  ✗ самопроверка: verdict $* → код $rc, ждали $want"; echo "$out" | sed 's/^/      | /'; bad=1; return
    fi
    if [ "$phrase" != - ] && ! grep -qF "$phrase" <<<"$out"; then
      echo "  ✗ самопроверка: verdict $* — нет фразы «$phrase»"; echo "$out" | sed 's/^/      | /'; bad=1; return
    fi
  }
  local ok_set=(kacho-internal-ca ready ready available no)
  expect 0 "политика выпуска сертификатов служб действует" present "${ok_set[@]}"
  expect 1 "$HEADLINE" present absent absent absent absent no
  expect 1 "$HEADLINE" present kacho-internal-ca absent ready available no
  expect 1 "$HEADLINE" present kacho-internal-ca ready absent available no
  expect 1 "не Available" present kacho-internal-ca ready ready unavailable no
  expect 1 "декоративна" present kacho-internal-ca ready ready available yes
  expect 1 "НЕ ПРОЧИТАНО" present kacho-internal-ca ready ready available unread
  expect 1 "НЕ ПРОЧИТАНО" unread "${ok_set[@]}"
  expect 1 "неузнанное" present kacho-internal-ca ready ready available maybe
  expect 1 "НЕ принял" present kacho-internal-ca notready ready available no
  expect 0 "требование политики выпуска не предъявляется" absent absent absent absent absent yes
  expect 0 "не блокируется" absent "${ok_set[@]}"

  # Признак «notify поднят» — на рабочих нагрузках в той форме, в какой их
  # производят чарты дерева: соседи метят деплойменты `kacho-<svc>` или не метят
  # вовсе, поэтому ось без меток и ось с меткой соседа обязательны.
  classify() { # <ждём> <JSON списка рабочих нагрузок>
    local want="$1" got; n=$((n + 1))
    got="$(printf '%s' "$2" | notify_classify | cut -d' ' -f1)"
    [ "$got" = "$want" ] || { echo "  ✗ самопроверка: классификатор notify → «$got», ждали «$want» на $2"; bad=1; }
  }
  local sa_only='{"items":[{"kind":"Deployment","metadata":{"name":"mail-gateway"},"spec":{"template":{"spec":{"serviceAccountName":"kacho-notify"}}}}]}'
  local neighbor='{"items":[{"kind":"Deployment","metadata":{"name":"kacho-notify","labels":{"app.kubernetes.io/name":"kacho-notify"}},"spec":{"template":{"spec":{"serviceAccountName":"kacho-notify"}}}}]}'
  local by_name='{"items":[{"kind":"StatefulSet","metadata":{"name":"notify-sender"},"spec":{"template":{"spec":{}}}}]}'
  local by_label='{"items":[{"kind":"Deployment","metadata":{"name":"x","labels":{"app.kubernetes.io/name":"notify"}},"spec":{"template":{"spec":{"serviceAccountName":"x"}}}}]}'
  local legacy_sa='{"items":[{"kind":"DaemonSet","metadata":{"name":"x"},"spec":{"template":{"spec":{"serviceAccount":"kacho-notify"}}}}]}'
  local other='{"items":[{"kind":"Deployment","metadata":{"name":"kacho-vpc","labels":{"app.kubernetes.io/name":"kacho-vpc"}},"spec":{"template":{"spec":{"serviceAccountName":"kacho-vpc"}}}}]}'
  local lookalike='{"items":[{"kind":"Deployment","metadata":{"name":"kacho-notifyx"},"spec":{"template":{"spec":{"serviceAccountName":"kacho-notifyx"}}}}]}'
  classify present "$sa_only"
  classify present "$neighbor"
  classify present "$by_name"
  classify present "$by_label"
  classify present "$legacy_sa"
  classify absent "$other"
  classify absent "$lookalike"
  classify absent '{"items":[]}'
  classify unread 'не JSON'
  classify unread '{"kind":"List"}'
  echo "  самопроверка: осей $n"
  return "$bad"
}

# ═════════════════════════════════════════════════════════════════════════════
# КЛАСТЕРНАЯ ПОЛОВИНА — наблюдения.
# ═════════════════════════════════════════════════════════════════════════════
# notify_classify: JSON списка рабочих нагрузок (вид List, поле items) на входе
# → "<present|absent|unread> <нагрузок осмотрено> <из них признано notify>".
# Чистая функция: её же зовут самопроверка и проба рендера.
notify_classify() {
  python3 -c '
import json, re, sys
sa_want, name_re = sys.argv[1], re.compile(sys.argv[2])
try:
    doc = json.load(sys.stdin)
    items = doc["items"]
    if not isinstance(items, list):
        raise ValueError("items не список")
except Exception:
    print("unread 0 0"); sys.exit(0)
hit = 0
for w in items:
    meta = w.get("metadata") or {}
    labels = meta.get("labels") or {}
    pod = ((w.get("spec") or {}).get("template") or {}).get("spec") or {}
    sa = pod.get("serviceAccountName") or pod.get("serviceAccount") or ""
    names = [meta.get("name") or ""] + [labels.get(k) or "" for k in
             ("app.kubernetes.io/name", "app.kubernetes.io/instance", "app")]
    if sa == sa_want or any(name_re.match(n) for n in names if n):
        hit += 1
print("%s %d %d" % ("present" if hit else "absent", len(items), hit))' "$NOTIFY_SA" "$NOTIFY_NAME_RE" 2>/dev/null \
    || echo "unread 0 0"
}

# observe_notify → "<present|absent|unread> <осмотрено> <признано>"
observe_notify() {
  local out
  out="$(kubectl -n "$NS" get deployments.apps,statefulsets.apps,daemonsets.apps -o json 2>/dev/null)" \
    || { echo "unread 0 0"; return; }
  printf '%s' "$out" | notify_classify
}

observe_issuer() {
  local out
  out="$(kubectl get clusterissuers -l "$ISSUER_SELECTOR" -o json 2>/dev/null)" || { echo unread; return; }
  printf '%s' "$out" | python3 -c '
import json,sys
names=[i["metadata"]["name"] for i in json.load(sys.stdin)["items"] if "ca" in i.get("spec",{})]
print("absent" if not names else names[0] if len(names)==1 else "ambiguous")' 2>/dev/null || echo unread
}

# observe_policy_arms <УЦ> → "<workload> <certificates> <политик осмотрено> <из них под этим УЦ>"
observe_policy_arms() {
  local issuer="$1" groups out
  groups="$(kubectl api-resources --api-group=policy.cert-manager.io -o name 2>/dev/null)" || { echo "unread unread"; return; }
  grep -qx 'certificaterequestpolicies.policy.cert-manager.io' <<<"$groups" || { echo "absent absent 0 0"; return; }
  out="$(kubectl get certificaterequestpolicies.policy.cert-manager.io -l "$POLICY_SELECTOR" -o json 2>/dev/null)" \
    || { echo "unread unread"; return; }
  printf '%s' "$out" | python3 -c '
import json,sys
issuer=sys.argv[1]; st={}; items=json.load(sys.stdin)["items"]; mine=0
for p in items:
    arm=p["metadata"].get("labels",{}).get("kacho.cloud/issuance-arm","")
    ref=(p.get("spec",{}).get("selector",{}) or {}).get("issuerRef",{}) or {}
    if ref.get("name")!=issuer or ref.get("kind")!="ClusterIssuer": continue
    mine+=1
    ready=any(c.get("type")=="Ready" and c.get("status")=="True" for c in p.get("status",{}).get("conditions",[]))
    if st.get(arm)!="ready": st[arm]="ready" if ready else "notready"
print(st.get("workload","absent"), st.get("certificates","absent"), len(items), mine)' "$issuer" 2>/dev/null || echo "unread unread"
}

observe_approver() {
  local out
  out="$(kubectl get deployments -A -l "$APPROVER_SELECTOR" -o json 2>/dev/null)" || { echo unread; return; }
  printf '%s' "$out" | python3 -c '
import json,sys
items=json.load(sys.stdin)["items"]
if not items: print("absent")
else: print("available" if any(any(c.get("type")=="Available" and c.get("status")=="True" for c in i.get("status",{}).get("conditions",[])) for i in items) else "unavailable")' 2>/dev/null || echo unread
}

# observe_builtin <УЦ> → "<yes|no|absent|unread> <контроллеров> <проверок права>"
observe_builtin() {
  local issuer="$1" out rows ns sa name allowed any=no ctl=0 sar=0
  out="$(kubectl get deployments -A -l "$CERT_MANAGER_SELECTOR" \
    -o jsonpath='{range .items[*]}{.metadata.namespace} {.spec.template.spec.serviceAccountName}{"\n"}{end}' 2>/dev/null)" \
    || { echo "unread 0 0"; return; }
  rows="$(grep -v '^\s*$' <<<"$out")"
  [ -n "$rows" ] || { echo "absent 0 0"; return; }
  while read -r ns sa; do
    [ -n "$sa" ] || { echo "unread $ctl $sar"; return; }
    ctl=$((ctl + 1))
    for name in "clusterissuers.cert-manager.io/$issuer" "clusterissuers.cert-manager.io/*"; do
      allowed="$(kubectl create -o jsonpath='{.status.allowed}' -f - 2>/dev/null <<EOF
apiVersion: authorization.k8s.io/v1
kind: SubjectAccessReview
spec:
  user: system:serviceaccount:$ns:$sa
  groups: [system:serviceaccounts, "system:serviceaccounts:$ns", system:authenticated]
  resourceAttributes: {group: cert-manager.io, resource: signers, verb: approve, name: "$name"}
EOF
)" || { echo "unread $ctl $sar"; return; }
      sar=$((sar + 1))
      case "$allowed" in
        true) any=yes ;;
        false|"") ;;
        *) echo "unread $ctl $sar"; return ;;
      esac
    done
  done <<<"$rows"
  echo "$any $ctl $sar"
}

main() {
  echo "=== E. политика выпуска сертификатов служб (NTF1-J04; ns $NS) ==="
  local notify issuer arms bi workload certificates approver builtin npol=0 nmine=0 nctl=0 nsar=0 nwl=0 nnot=0
  read -r notify nwl nnot <<<"$(observe_notify)"
  issuer="$(observe_issuer)"
  case "$issuer" in
    absent|ambiguous|unread|"") arms="absent absent 0 0"; bi="absent 0 0" ;;
    *) arms="$(observe_policy_arms "$issuer")"; bi="$(observe_builtin "$issuer")" ;;
  esac
  read -r workload certificates npol nmine <<<"$arms"
  read -r builtin nctl nsar <<<"$bi"
  approver="$(observe_approver)"
  echo "    осмотрено: рабочих нагрузок в $NS ${nwl:-0} (из них notify — учётка $NOTIFY_SA или имя notify — ${nnot:-0}), политик выпуска с меткой ${npol:-0} (под этим УЦ ${nmine:-0}), контроллеров cert-manager ${nctl:-0}, проверок права одобрения ${nsar:-0}"
  echo "    наблюдения: notify=$notify УЦ=$issuer workload=$workload certificates=$certificates контроллер=$approver встроенный-одобритель-вправе=$builtin"
  verdict "$notify" "$issuer" "$workload" "$certificates" "$approver" "$builtin"
}

case "${1:-}" in
  --self-check) self_check ;;
  --classify-notify) notify_classify ;;
  "") main ;;
  *) echo "использование: $0 [--self-check | --classify-notify <JSON списка нагрузок]" >&2; exit 2 ;;
esac
