#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# stand-ns-isolation-probe.sh NS — сетевая изоляция стенда проб (kacho#3102)
# утверждается СПОСОБНОСТЬЮ, а не наличием политики.
#
#   bash deploy/scripts/stand-ns-isolation-probe.sh NS
#
# Зовётся командой прогона на поднятом стенде:
#   make -C deploy stand-ns-run NS=t<задача>-<коротко> CMD='bash deploy/scripts/stand-ns-isolation-probe.sh "$KACHO_STAND_NS"'
#
# Политики stand-ingress и stand-egress (scripts/stand-ns.sh, ns_objects) судятся
# соединением TCP из двух подов-проб: одного В стенде и одного в соседнем
# тестовом пространстве NS-peer (помеченном так же, снимается этой же пробой на
# любом исходе). У каждого отказа есть законный близнец, проходящий тем же
# путём, — иначе отказ мог бы быть свойством пробы (нет образа, нет резолвера):
#
#   из стенда   → край своего стенда, порт сбора величин        ПРОХОДИТ
#   из стенда   → край `kacho`, тот же порт                     ОТКАЗ   (близнец: из соседа — проходит)
#   из стенда   → apiserver (kubernetes.default:443)            ПРОХОДИТ (служба прав стенда к нему ходит)
#   из соседа   → публичный слушатель vpc стенда                ОТКАЗ   (близнец: из соседа к vpc `kacho` — проходит)
#   из соседа   → край стенда, порт сбора величин               ПРОХОДИТ — остаток: политика чарта
#                                                               открывает этот порт всем пространствам
#
# В пространство `kacho` проба НЕ пишет: к нему только соединение TCP без
# запроса (nc -z), ни одного объекта там не создаётся.
#
# Код: 0 — всё как утверждено; 1 — расхождение (строки «РАСХОЖДЕНИЕ»);
#      2 — условие пробы не создано (под-проба не поднялся, адреса не прочитаны).
set -uo pipefail

NS="${1:-}"
case "$NS" in t[0-9]*-*) ;; *) echo "ABORT: имя стенда «$NS» не по правилу t<задача>-<коротко>" >&2; exit 2 ;; esac
PEER="${NS}-peer"
[ "${#PEER}" -le 63 ] || { echo "ABORT: имя соседа $PEER длиннее 63" >&2; exit 2; }
TASK="${NS#t}"; TASK="${TASK%%-*}"
IMAGE="${STAND_PROBE_IMAGE:-docker.io/library/busybox:1.37.0}"
METRICS_PORT="${STAND_EDGE_METRICS_PORT:-9095}"

# shellcheck disable=SC2329 # зовётся ловушкой EXIT
cleanup() {
  kubectl -n "$NS" delete pod stand-isolation-probe --ignore-not-found --wait=false >/dev/null 2>&1
  kubectl delete namespace "$PEER" --ignore-not-found --wait=true --timeout=180s >/dev/null 2>&1 ||
    echo "stand-isolation: сосед $PEER не снят — make -C deploy stand-ns-down NS=$PEER" >&2
}
trap cleanup EXIT

probe_pod() {
  cat <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: stand-isolation-probe
  namespace: $1
  labels: {kacho.io/stand: test, app: stand-isolation-probe}
spec:
  restartPolicy: Never
  automountServiceAccountToken: false
  securityContext:
    runAsNonRoot: true
    runAsUser: 65534
    seccompProfile: {type: RuntimeDefault}
  containers:
    - name: probe
      image: $IMAGE
      command: ["sleep", "900"]
      resources:
        requests: {cpu: 10m, memory: 16Mi}
        limits: {memory: 32Mi}
      securityContext:
        allowPrivilegeEscalation: false
        capabilities: {drop: [ALL]}
EOF
}

expires="$(date -u -d '+1 hours' +%Y-%m-%dT%H:%M:%SZ)"
kubectl apply -f - >/dev/null <<EOF || { echo "ABORT: сосед $PEER не заведён" >&2; exit 2; }
apiVersion: v1
kind: Namespace
metadata:
  name: $PEER
  labels: {kacho.io/stand: test, kacho.io/task: "$TASK"}
  annotations: {kacho.io/expires: "$expires"}
EOF
for ns in "$NS" "$PEER"; do
  probe_pod "$ns" | kubectl apply -f - >/dev/null || { echo "ABORT: под-проба в $ns не заведён" >&2; exit 2; }
done
for ns in "$NS" "$PEER"; do
  kubectl -n "$ns" wait pod/stand-isolation-probe --for=condition=Ready --timeout=180s >/dev/null ||
    { echo "ABORT: под-проба в $ns не готов — условие пробы не создано" >&2; kubectl -n "$ns" describe pod stand-isolation-probe | tail -n 15 >&2; exit 2; }
done

edge_ip() {
  kubectl -n "$1" get pod -l app=api-gateway -o jsonpath='{.items[0].status.podIP}' 2>/dev/null
}
own_edge="$(edge_ip "$NS")"; kacho_edge="$(edge_ip kacho)"
[ -n "$own_edge" ] && [ -n "$kacho_edge" ] || { echo "ABORT: адрес пода края не прочитан (стенд: «$own_edge», kacho: «$kacho_edge»)" >&2; exit 2; }

fail=0 n=0
# check FROM_NS WANT(pass|deny) HOST PORT — что утверждается
check() {
  local from="$1" want="$2" host="$3" port="$4" what="$5" got
  n=$((n + 1))
  if kubectl -n "$from" exec stand-isolation-probe -- nc -z -w 4 "$host" "$port" >/dev/null 2>&1; then got=pass; else got=deny; fi
  if [ "$got" = "$want" ]; then
    printf 'ok    %-5s %s\n' "$got" "$what"
  else
    printf 'РАСХОЖДЕНИЕ  ждали %s, получили %s: %s\n' "$want" "$got" "$what"
    fail=1
  fi
}

check "$NS"   pass "$own_edge"                 "$METRICS_PORT" "из стенда к своему краю (порт сбора величин)"
check "$PEER" pass "$kacho_edge"               "$METRICS_PORT" "из соседа к краю kacho — близнец отказа ниже"
check "$NS"   deny "$kacho_edge"               "$METRICS_PORT" "из стенда к краю kacho"
check "$NS"   deny "vpc.kacho.svc"             9090            "из стенда к публичному слушателю vpc kacho"
check "$NS"   pass "kubernetes.default.svc"    443             "из стенда к apiserver"
check "$PEER" pass "vpc.kacho.svc"             9090            "из соседа к vpc kacho — близнец отказа ниже"
check "$PEER" deny "vpc.$NS.svc"               9090            "из соседа к публичному слушателю vpc стенда"
check "$PEER" pass "$own_edge"                 "$METRICS_PORT" "из соседа к краю стенда (порт сбора величин) — остаток, открыт политикой чарта"

echo "stand-isolation: утверждений $n, расхождений $([ "$fail" = 0 ] && echo 0 || echo 'есть')"
exit "$fail"
