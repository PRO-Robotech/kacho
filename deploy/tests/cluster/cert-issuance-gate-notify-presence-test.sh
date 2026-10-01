#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# cert-issuance-gate-notify-presence-test.sh — СЕКЦИЯ E ГЕЙТА ПОСАДКИ ВИДИТ
# ПОДНЯТЫЙ NOTIFY НА ЖИВОМ КЛАСТЕРЕ (приёмка NTF-1, NTF1-J04; ревью 2915-J1, F1).
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАЧЕМ
#
# J04 краснеет, только если гейт вообще узнал, что notify поднят. Прежняя
# редакция узнавала его по метке `app.kubernetes.io/name=notify`, которой не
# производит ни один чарт, и на notify, заведённом по образцу соседей (метка
# `kacho-<svc>` или никакой), читала «не поднят» и выходила кодом 0 — ровно там,
# где J04 обязан краснеть. Самопроверка гейта судит предикат на синтетических
# наблюдениях; здесь — наблюдение сквозь кластер: настоящий объект, настоящий
# kubectl, настоящий гейт.
#
# ЧТО УТВЕРЖДАЕТСЯ — для каждой формы нагрузки notify, поднятой в собственном
# пространстве пробы:
#   - гейт наблюдает `notify=present`;
#   - исход гейта согласен с состоянием политики в кластере: политика действует
#     → код 0 и «notify поднят, и политика … действует»; не действует → код 1 и
#     «notify требует политики выпуска сертификатов служб».
# Состояние политики проба берёт у того же гейта ДО подъёма нагрузки (на пустом
# пространстве он печатает его, не предъявляя требования), поэтому проба одна и
# для кластера с политикой (близнец), и для кластера без неё (красный J04).
#
# Формы — те, что производят чарты дерева и декларация:
#   neighbor-label   Deployment `kacho-notify`, метка `app.kubernetes.io/name:
#                    kacho-notify`, учётка `kacho-notify` (образец nlb, storage);
#   unlabelled-sa    Deployment без меток с посторонним именем, учётка
#                    `kacho-notify` (образец api-gateway, compute, vpc…:
#                    меток нет, идентичность — только учётка);
#   statefulset-name StatefulSet `notify-sender` под учёткой по умолчанию.
# Близнец: Deployment чужой службы (`kacho-vpc`, учётка `kacho-vpc`) →
# `notify=absent` и код 0.
#
# Нагрузки — с replicas: 0: гейт судит объявление нагрузки, а не живость пода,
# и образ тянуть незачем.
#
# ИСХОДОВ ТРИ: 0 — зелено; 1 — находка (гейт не увидел notify либо исход не
# согласен с политикой); 2 — условие не создано.
#
# ЗАПУСК (из каталога deploy/):
#   EXPECT_CONTEXT=kind-kacho bash tests/cluster/cert-issuance-gate-notify-presence-test.sh
#   EXPECT_CONTEXT=<контекст> EXPECT_SERVER=<адрес API> bash tests/cluster/…  # не kind
#
# Состояние, которое проба заводит, — только её собственное: пространство
# `kacho-notify-presence-<суффикс>` (метка kacho.cloud/probe=notify-presence),
# снимается при любом выходе.
set -uo pipefail

SCRIPT="$(basename "$0")"
HERE="$(cd "$(dirname "$0")" && pwd)"
GATE="$HERE/../../scripts/assert-cert-issuance-policy.sh"
HEADLINE="notify требует политики выпуска сертификатов служб"
# 1 исходное + 4 формы.
EXPECTED_ASSERTIONS=5
N=0
FAILED=0
PROBE_NS=""

good()  { N=$((N + 1)); echo "  ✓ $*"; }
bad()   { N=$((N + 1)); FAILED=1; echo "  ✗ $*"; }
fatal() { echo "FATAL (условие не создано): $*" >&2; echo "  перепись: утверждений исполнено $N из $EXPECTED_ASSERTIONS" >&2; exit 2; }

cleanup() {
  if [ -n "$PROBE_NS" ]; then
    kubectl delete namespace "$PROBE_NS" --wait=false --ignore-not-found >/dev/null 2>&1 \
      || echo "!!! пространство $PROBE_NS НЕ снято — снять руками: kubectl delete namespace $PROBE_NS" >&2
  fi
}
trap cleanup EXIT

for t in kubectl yq python3; do
  command -v "$t" >/dev/null 2>&1 || fatal "нет $t в PATH"
done
[ -f "$GATE" ] || fatal "нет гейта секции E $GATE"
[ -n "${EXPECT_CONTEXT:-}" ] || fatal "EXPECT_CONTEXT не задан: проба пишет в кластер активного контекста, умолчания нет"
ctx="$(kubectl config current-context 2>/dev/null)" || fatal "активный контекст не прочитан"
[ "$ctx" = "$EXPECT_CONTEXT" ] || fatal "активный контекст '$ctx', а назван '$EXPECT_CONTEXT'"
# Имя контекста — ярлык; идентичность — адрес API. Для kind его выдаёт сам
# движок (тот же довод, что в cert-issuance-policy-test.sh); для иного кластера
# (например, голого kube-apiserver без политики — красная сторона J04) адрес
# называет запускающий в EXPECT_SERVER, и он обязан совпасть.
have_server="$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')" \
  || fatal "адрес API активного контекста не прочитан"
case "$EXPECT_CONTEXT" in
  kind-?*)
    command -v kind >/dev/null 2>&1 || fatal "нет kind в PATH"
    want_server="$(kind get kubeconfig --name "${EXPECT_CONTEXT#kind-}" 2>/dev/null | yq -r '.clusters[0].cluster.server')" \
      || fatal "kind не выдал kubeconfig кластера '${EXPECT_CONTEXT#kind-}'" ;;
  *)
    want_server="${EXPECT_SERVER:-}"
    [ -n "$want_server" ] || fatal "EXPECT_CONTEXT='$EXPECT_CONTEXT' — не кластер kind, а EXPECT_SERVER не задан: удостоверить цель нечем" ;;
esac
[ -n "$want_server" ] && [ "$want_server" = "$have_server" ] \
  || fatal "активный контекст ведёт в '$have_server', а ожидался '$want_server'"

PROBE_NS="kacho-notify-presence-$(od -An -N4 -tx1 /dev/urandom | tr -d ' \n')"
kubectl create namespace "$PROBE_NS" >/dev/null || { PROBE_NS=""; fatal "пространство пробы не создано"; }
kubectl label namespace "$PROBE_NS" kacho.cloud/probe=notify-presence >/dev/null || fatal "метка пространства пробы не поставлена"
echo "=== $SCRIPT: контекст $ctx, пространство пробы $PROBE_NS ==="

gate() { NS="$PROBE_NS" bash "$GATE" 2>&1; }

# ── Исходное: пустое пространство → notify=absent, состояние политики ─────────
out="$(gate)"; rc=$?
echo "$out" | sed 's/^/      | /'
grep -qF "notify=absent" <<<"$out" || fatal "на пустом пространстве гейт не наблюдает notify=absent — исходное не установлено"
[ "$rc" -eq 0 ] || fatal "на пустом пространстве гейт дал код $rc — исходное не установлено"
if grep -qF "политика выпуска действует" <<<"$out"; then
  POLICY=acting
elif grep -qF "Состояние политики" <<<"$out"; then
  POLICY=missing
else
  fatal "состояние политики из вывода гейта не установлено"
fi
good "исходное: notify=absent, код 0; политика в кластере: $POLICY"

workload() { # <вид> <имя> <учётка или -> <метка имени или ->
  local sa_line="" label_line=""
  [ "$3" != - ] && sa_line="      serviceAccountName: $3"
  [ "$4" != - ] && label_line="  labels: {app.kubernetes.io/name: $4}"
  local svc_line=""
  [ "$1" = StatefulSet ] && svc_line="  serviceName: $2"
  cat <<EOF
apiVersion: apps/v1
kind: $1
metadata:
  name: $2
$label_line
spec:
  replicas: 0
$svc_line
  selector: {matchLabels: {kacho.cloud/probe-workload: $2}}
  template:
    metadata: {labels: {kacho.cloud/probe-workload: $2}}
    spec:
$sa_line
      containers: [{name: c, image: registry.k8s.io/pause:3.10}]
EOF
}

# check <форма> <ждём notify: present|absent> <вид> <имя> <учётка|-> <метка|->
check() {
  local form="$1" want="$2" kind="$3" name="$4"
  workload "$kind" "$name" "$5" "$6" | kubectl -n "$PROBE_NS" apply -f - >/dev/null \
    || fatal "нагрузка формы $form не создана"
  out="$(gate)"; rc=$?
  echo "$out" | sed 's/^/      | /'
  kubectl -n "$PROBE_NS" delete "$kind" "$name" --wait=true >/dev/null 2>&1 \
    || fatal "нагрузка формы $form не снята — следующая форма судилась бы не одна"
  if ! grep -qF "notify=$want" <<<"$out"; then
    bad "$form: гейт не наблюдает notify=$want"; return
  fi
  if [ "$want" = absent ]; then
    [ "$rc" -eq 0 ] && good "$form: notify=absent, код 0" || bad "$form: notify=absent, а код $rc"
    return
  fi
  case "$POLICY" in
    acting)  if [ "$rc" -eq 0 ] && grep -qF "notify поднят, и политика" <<<"$out"; then
               good "$form: notify=present, политика действует → код 0"
             else bad "$form: notify=present при действующей политике, а код $rc"; fi ;;
    missing) if [ "$rc" -eq 1 ] && grep -qF "$HEADLINE" <<<"$out"; then
               good "$form: notify=present без политики → код 1 и «$HEADLINE»"
             else bad "$form: notify=present без политики, а код $rc без «$HEADLINE» — J04 прошёл бы молча"; fi ;;
  esac
}

check neighbor-label   present Deployment  kacho-notify  kacho-notify kacho-notify
check unlabelled-sa    present Deployment  mail-gateway  kacho-notify -
check statefulset-name present StatefulSet notify-sender -            -
check foreign-twin     absent  Deployment  kacho-vpc     kacho-vpc    kacho-vpc

# ── Перепись ─────────────────────────────────────────────────────────────────
echo "  перепись: утверждений исполнено $N из $EXPECTED_ASSERTIONS; политика в кластере: $POLICY"
[ "$N" -eq "$EXPECTED_ASSERTIONS" ] || { echo "FAIL: исполнено $N утверждений, объявлено $EXPECTED_ASSERTIONS"; exit 1; }
if [ "$FAILED" -ne 0 ]; then
  echo "FAIL: $SCRIPT"
  exit 1
fi
echo "PASS: $SCRIPT ($N утверждений, политика $POLICY)"
