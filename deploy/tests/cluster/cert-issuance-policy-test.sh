#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# cert-issuance-policy-test.sh — ПОЛИТИКА ВЫПУСКА СЕРТИФИКАТОВ СЛУЖБ НА ПОДНЯТОМ
# КЛАСТЕРЕ (приёмка NTF-1, раздел «NS»: NTF1-J01 и его близнец NTF1-J02).
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО УТВЕРЖДАЕТСЯ
#
# Идентичность службы в сертификате (URI SAN `spiffe://<домен>/ns/<ns>/sa/<sa>`)
# — то, по чему notify и kaname решают, ЧЬЯ это служба. Значит, выпустить её
# обязано быть можно только той учётке, чья она есть: SAN запроса совпадает с
# `spiffe://<домен доверия>/ns/<ns запросившего>/sa/<учётка запросившего>`
# (замысел З30, решение Р12).
#
#   J01  учётка X в пространстве имён A подаёт запрос с SAN notify →
#        запрос НЕ одобрен, сертификата НЕТ, событие отказа наблюдаемо.
#   J02  та же учётка, тот же запрос, ОДИН изменённый факт — SAN `ns/A/sa/X` →
#        сертификат выпущен, и в нём ровно запрошенный SAN.
#
# Пара — не украшение. Одиночное «отказано» зеленеет сильнее всего, когда
# сломано ВСЁ: у выпускающего нет ключа, вебхук лежит, учётке нельзя создать
# запрос. Отказ зачитывается только рядом с выпуском, отличающимся одним фактом.
#
# Тот же вопрос задаётся вторым путём подачи — объектом Certificate, который X
# создаёт в своём пространстве (запрос за него подаёт контроллер cert-manager):
#   J01-c  Certificate с SAN notify в A → отказ;
#   J02-c  Certificate с SAN `ns/A/sa/X` в A → выпуск.
# Без этой пары политика, закрывшая прямой запрос, оставляла бы открытым путь
# через Certificate, и J01 был бы правдой только о половине дверей.
#
# ─────────────────────────────────────────────────────────────────────────────
# ИСХОДОВ ТРИ (deploy/tests/helm/README.md «Три исхода»)
#
#   0 — зелено: все утверждения исполнены, перепись печатается;
#   1 — находка о стенде: политика не действует (выпущено то, что не должно),
#       или своё не выпущено, или отказ не наблюдаем;
#   2 — УСЛОВИЕ НЕ СОЗДАНО: контекст не тот, нет cert-manager, нет выпускающего
#       внутреннего УЦ, не прочитан домен доверия. Это НЕ вердикт о политике.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАПУСК (из каталога deploy/)
#
#   EXPECT_CONTEXT=kind-kacho STACK=dev bash tests/cluster/cert-issuance-policy-test.sh
#
# EXPECT_CONTEXT обязателен: проба создаёт и снимает пространство имён в кластере
# активного контекста, и «наверное, локальный» — не выбор. STACK — цепочка
# `deploy/stacks.txt`, из которой читается домен доверия зонтика (тот же узел,
# что читает помощник `umbrella.trustDomain`). Пространство имён службы, чей SAN
# подделывается в J01, — NS (умолчания нет по той же причине, что у контекста).
#
# Состояние, которое проба заводит, — только её собственное: пространство имён
# `kacho-issuance-probe-<суффикс>` (метка kacho.cloud/probe=cert-issuance-policy),
# и снимается оно при любом выходе. Чужого состояния проба не трогает.
set -uo pipefail

SCRIPT="$(basename "$0")"
HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/../.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"

# Идентичность notify по приёмке (NTF1-J03: `notify.spiffe = {T, N, kacho-notify}`).
# Имя учётки — константа приёмки, а не вывод из чарта notify: чарта ещё может не
# быть, а утверждение J01 о том, что ЧУЖУЮ идентичность не выпустить, от его
# наличия не зависит.
NOTIFY_SA="kacho-notify"

WAIT_SECONDS="${WAIT_SECONDS:-120}"
EXPECTED_ASSERTIONS=6
N=0
FAILED=0
PROBE_NS=""
WORK=""

say()   { echo "$*"; }
good()  { N=$((N + 1)); echo "  ✓ $*"; }
bad()   { N=$((N + 1)); FAILED=1; echo "  ✗ $*"; }
fatal() { echo "FATAL (условие не создано): $*" >&2; echo "  перепись: утверждений исполнено $N из $EXPECTED_ASSERTIONS" >&2; exit 2; }

cleanup() {
  if [ -n "$PROBE_NS" ]; then
    kubectl delete namespace "$PROBE_NS" --wait=false --ignore-not-found >/dev/null 2>&1 \
      || echo "!!! пространство $PROBE_NS НЕ снято — снять руками: kubectl delete namespace $PROBE_NS" >&2
  fi
  [ -n "$WORK" ] && rm -rf "$WORK"
}
trap cleanup EXIT

# ── Предпосылки: «условие не создано» отличимо от находки ────────────────────
for t in kubectl openssl python3 yq; do
  command -v "$t" >/dev/null 2>&1 || fatal "нет $t в PATH"
done
yqv="$(yq --version 2>&1 || true)"
[[ "${yqv,,}" == *mikefarah* ]] || fatal "в PATH не mikefarah yq v4 ('$yqv') — домен доверия прочитался бы пустым молча"

[ -n "${EXPECT_CONTEXT:-}" ] || fatal "EXPECT_CONTEXT не задан: проба пишет в кластер активного контекста, умолчания нет"
[ -n "${STACK:-}" ] || fatal "STACK не задан: домен доверия читается из цепочки deploy/stacks.txt, умолчания нет"
[ -n "${NS:-}" ] || fatal "NS не задан: пространство службы, чей SAN подделывается в J01, умолчания не имеет"

ctx="$(kubectl config current-context 2>/dev/null)" || fatal "активный контекст не прочитан"
[ "$ctx" = "$EXPECT_CONTEXT" ] || fatal "активный контекст '$ctx', а назван '$EXPECT_CONTEXT'"
# Имя контекста — ярлык автора kubeconfig, и одноимённый контекст может вести в
# другой кластер. Идентичность — адрес API: тот, что движок kind выдаёт о своём
# кластере сам, обязан совпасть с адресом активного контекста. Проба — для
# стенда kind, другого способа удостоверить цель у неё нет, поэтому не-kind —
# «условие не создано», а не «наверное, можно».
case "$EXPECT_CONTEXT" in
  kind-?*) ;;
  *) fatal "EXPECT_CONTEXT='$EXPECT_CONTEXT' — не кластер kind; удостоверить его адрес пробе нечем" ;;
esac
command -v kind >/dev/null 2>&1 || fatal "нет kind в PATH — адрес кластера '$EXPECT_CONTEXT' сверить не с чем"
want_server="$(kind get kubeconfig --name "${EXPECT_CONTEXT#kind-}" 2>/dev/null | yq -r '.clusters[0].cluster.server')" \
  || fatal "kind не выдал kubeconfig кластера '${EXPECT_CONTEXT#kind-}'"
have_server="$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')" \
  || fatal "адрес API активного контекста не прочитан"
[ -n "$want_server" ] && [ "$want_server" = "$have_server" ] \
  || fatal "активный контекст ведёт в '$have_server', а кластер kind '${EXPECT_CONTEXT#kind-}' отвечает на '$want_server'"

chain="$(bash "$DEPLOY_ROOT/tests/helm/stacks.sh" --chain "$STACK")" || fatal "цепочка '$STACK' не прочитана из deploy/stacks.txt"
files=("$UMBRELLA/values.yaml")
for f in $chain; do files+=("$UMBRELLA/$f"); done
TRUST="$(yq eval-all -r '. as $i ireduce ({}; . * $i) | .mtls.bootstrapOperator.spiffe.trustDomain // ""' "${files[@]}")" \
  || fatal "домен доверия цепочки '$STACK' не прочитан (yq отказал)"
[ -n "$TRUST" ] || fatal "домен доверия в цепочке '$STACK' пуст (.mtls.bootstrapOperator.spiffe.trustDomain)"
ISSUER="$(yq eval-all -r '. as $i ireduce ({}; . * $i) | .mtls.internalCA.clusterIssuerName // ""' "${files[@]}")" \
  || fatal "имя выпускающего внутреннего УЦ не прочитано"
[ -n "$ISSUER" ] || fatal "имя выпускающего внутреннего УЦ в цепочке '$STACK' пусто (.mtls.internalCA.clusterIssuerName)"

kubectl get crd certificaterequests.cert-manager.io >/dev/null 2>&1 \
  || fatal "в кластере нет CRD certificaterequests.cert-manager.io — cert-manager не поднят"
ready="$(kubectl get clusterissuer "$ISSUER" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null)" \
  || fatal "ClusterIssuer/$ISSUER не прочитан — внутренний УЦ не поднят"
[ "$ready" = "True" ] || fatal "ClusterIssuer/$ISSUER не Ready (Ready=$ready) — выпускать нечем, отказ ничего бы не доказал"

WORK="$(mktemp -d)" || fatal "не создан временный каталог"
PROBE_NS="kacho-issuance-probe-$(od -An -N4 -tx1 /dev/urandom | tr -d ' \n')"
X="probe-requester"
NOTIFY_SAN="spiffe://$TRUST/ns/$NS/sa/$NOTIFY_SA"
OWN_SAN="spiffe://$TRUST/ns/$PROBE_NS/sa/$X"

say "=== $SCRIPT: контекст $ctx, цепочка $STACK, домен доверия $TRUST, выпускающий ClusterIssuer/$ISSUER ==="
say "    учётка X = $PROBE_NS/$X; SAN notify = $NOTIFY_SAN; свой SAN = $OWN_SAN"

# ── Фикстура: пространство A и учётка X с правом подавать запросы ─────────────
kubectl create namespace "$PROBE_NS" >/dev/null || { PROBE_NS=""; fatal "пространство пробы не создано"; }
kubectl label namespace "$PROBE_NS" kacho.cloud/probe=cert-issuance-policy >/dev/null || fatal "метка пространства пробы не поставлена"
kubectl -n "$PROBE_NS" create serviceaccount "$X" >/dev/null || fatal "учётка X не создана"
kubectl -n "$PROBE_NS" create role issuance-requester \
  --verb=create,get,list,watch --resource=certificaterequests.cert-manager.io,certificates.cert-manager.io >/dev/null \
  || fatal "роль подачи запросов не создана"
kubectl -n "$PROBE_NS" create rolebinding issuance-requester --role=issuance-requester \
  --serviceaccount="$PROBE_NS:$X" >/dev/null || fatal "привязка роли не создана"

# От имени X — ЕГО токеном, а не олицетворением администратора: имя запросившего
# в запросе ставит вебхук cert-manager по аутентифицированному пользователю.
token="$(kubectl -n "$PROBE_NS" create token "$X" --duration=15m)" || fatal "токен учётки X не выпущен"
server="$(kubectl config view --raw --minify -o jsonpath='{.clusters[0].cluster.server}')"
cadata="$(kubectl config view --raw --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')"
[ -n "$server" ] && [ -n "$cadata" ] || fatal "адрес API или его якорь не прочитаны из kubeconfig"
cat >"$WORK/kubeconfig-x" <<EOF
apiVersion: v1
kind: Config
clusters: [{name: c, cluster: {server: "$server", certificate-authority-data: "$cadata"}}]
users: [{name: x, user: {token: "$token"}}]
contexts: [{name: x, context: {cluster: c, user: x, namespace: "$PROBE_NS"}}]
current-context: x
EOF
as_x() { KUBECONFIG="$WORK/kubeconfig-x" kubectl "$@"; }
who="$(as_x auth whoami -o jsonpath='{.status.userInfo.username}' 2>/dev/null)" || fatal "API не принял токен учётки X"
[ "$who" = "system:serviceaccount:$PROBE_NS:$X" ] || fatal "токен X аутентифицирован как '$who'"

# csr <имя> <SAN> → base64 PEM запроса подписи с единственным URI SAN.
csr() {
  openssl ecparam -name prime256v1 -genkey -noout -out "$WORK/$1.key" 2>/dev/null \
    && openssl req -new -key "$WORK/$1.key" -subj "/" -addext "subjectAltName=URI:$2" \
         -out "$WORK/$1.csr" 2>/dev/null \
    && base64 -w0 "$WORK/$1.csr"
}

# decision <kind> <имя> → approved|denied|issued|pending|unread
# Исход запроса читается по условиям, которые ставит САМ cert-manager, а не по
# нашему ожиданию: `Denied=True` — отказ, `Ready=True` с непустым сертификатом —
# выпуск. Для Certificate — по запросу, который контроллер подал за него.
decision() {
  local kind="$1" name="$2" deadline=$((SECONDS + WAIT_SECONDS)) cr js
  while [ "$SECONDS" -lt "$deadline" ]; do
    if [ "$kind" = certificate ]; then
      cr="$(kubectl -n "$PROBE_NS" get certificaterequests -o json 2>/dev/null \
        | python3 -c 'import json,sys; n=sys.argv[1]; print(" ".join(i["metadata"]["name"] for i in json.load(sys.stdin)["items"] if any(o.get("kind")=="Certificate" and o.get("name")==n for o in i["metadata"].get("ownerReferences",[]))))' "$name")" \
        || { echo unread; return; }
      [ -n "$cr" ] || { sleep 2; continue; }
      cr="${cr%% *}"
    else
      cr="$name"
    fi
    js="$(kubectl -n "$PROBE_NS" get certificaterequest "$cr" -o json 2>/dev/null)" || { echo unread; return; }
    out="$(printf '%s' "$js" | python3 -c '
import json,sys
d=json.load(sys.stdin); c={x["type"]:x for x in d.get("status",{}).get("conditions",[])}
if c.get("Denied",{}).get("status")=="True": print("denied")
elif c.get("Ready",{}).get("status")=="True" and d.get("status",{}).get("certificate"): print("issued")
elif c.get("Ready",{}).get("reason") in ("Failed","Denied","InvalidRequest"): print("failed:"+c["Ready"].get("message",""))
else: print("pending")')"
    case "$out" in
      pending) sleep 2 ;;
      *) echo "$out|$cr"; return ;;
    esac
  done
  echo "pending|${cr:-}"
}

# denial_event <запрос> → текст события отказа на этом объекте или пусто.
denial_event() {
  kubectl -n "$PROBE_NS" get events --field-selector "involvedObject.kind=CertificateRequest,involvedObject.name=$1,reason=Denied" \
    -o jsonpath='{range .items[*]}{.reason}: {.message}{"\n"}{end}' 2>/dev/null
}

# san_of <запрос> → URI SAN выпущенного сертификата.
san_of() {
  kubectl -n "$PROBE_NS" get certificaterequest "$1" -o jsonpath='{.status.certificate}' | base64 -d \
    | openssl x509 -noout -ext subjectAltName 2>/dev/null | sed -n 's/^ *URI://p' | tr -d ' '
}

submit_request() { # <имя> <SAN>
  local req
  req="$(csr "$1" "$2")" || fatal "запрос подписи '$1' не собран (openssl)"
  as_x apply -f - >/dev/null <<EOF || return 1
apiVersion: cert-manager.io/v1
kind: CertificateRequest
metadata: {name: $1, namespace: $PROBE_NS}
spec:
  request: $req
  duration: 1h
  usages: [client auth]
  issuerRef: {name: $ISSUER, kind: ClusterIssuer, group: cert-manager.io}
EOF
}

submit_certificate() { # <имя> <SAN>
  as_x apply -f - >/dev/null <<EOF || return 1
apiVersion: cert-manager.io/v1
kind: Certificate
metadata: {name: $1, namespace: $PROBE_NS}
spec:
  secretName: $1
  duration: 1h
  renewBefore: 30m
  privateKey: {algorithm: ECDSA, size: 256}
  uris: ["$2"]
  usages: [client auth]
  issuerRef: {name: $ISSUER, kind: ClusterIssuer, group: cert-manager.io}
EOF
}

# expect_denied <метка> <kind> <имя>
expect_denied() {
  local res cr ev
  res="$(decision "$2" "$3")"; cr="${res#*|}"; res="${res%%|*}"
  case "$res" in
    denied)
      good "$1: запрос $cr НЕ одобрен (Denied=True), сертификата нет"
      ev="$(denial_event "$cr")"
      if [ -n "$ev" ]; then good "$1: событие отказа наблюдаемо — ${ev%%$'\n'*}"
      else bad "$1: отказ есть, а события отказа (reason=Denied) у $cr нет — отказ не наблюдаем"; fi ;;
    issued)
      bad "$1: ВЫПУЩЕН сертификат с SAN «$(san_of "$cr")» учётке $PROBE_NS/$X — политика выпуска не действует"
      bad "$1: события отказа нет — отказывать было некому" ;;
    pending)
      bad "$1: за ${WAIT_SECONDS} с запрос ${cr:-<не подан>} ни одобрен, ни отклонён — отказ не наблюдаем (приёмка требует отказа событием)"
      bad "$1: события отказа нет" ;;
    unread) fatal "$1: состояние запроса не прочитано (kubectl отказал)" ;;
    *)
      bad "$1: неузнанный исход «$res» у ${cr:-<нет запроса>} — не засчитывается"
      bad "$1: события отказа не установлено" ;;
  esac
}

# expect_issued <метка> <kind> <имя> <SAN>
expect_issued() {
  local res cr got
  res="$(decision "$2" "$3")"; cr="${res#*|}"; res="${res%%|*}"
  case "$res" in
    issued)
      got="$(san_of "$cr")"
      if [ "$got" = "$4" ]; then good "$1: сертификат выпущен ($cr), SAN ровно «$got»"
      else bad "$1: выпущен $cr, но SAN «$got» ≠ запрошенному «$4»"; fi ;;
    denied) bad "$1: свой SAN ОТКЛОНЁН ($cr): $(denial_event "$cr" | head -1) — отказ ниже ничего не доказывает" ;;
    pending) bad "$1: за ${WAIT_SECONDS} с свой SAN не выпущен (${cr:-<запрос не подан>}) — выпускающий не работает, отказ ниже ничего не доказывает" ;;
    unread) fatal "$1: состояние запроса не прочитано (kubectl отказал)" ;;
    *) bad "$1: неузнанный исход «$res» у ${cr:-<нет запроса>}" ;;
  esac
}

say "=== путь 1: учётка X подаёт CertificateRequest сама ==="
submit_request j02-own "$OWN_SAN" || fatal "X не смог подать запрос со своим SAN — у фикстуры нет права, а не у политики"
submit_request j01-notify "$NOTIFY_SAN" || fatal "X не смог подать запрос с SAN notify — отказ на входе API, а не решение политики"
expect_issued "J02 (свой SAN)" request j02-own "$OWN_SAN"
expect_denied "J01 (SAN notify)" request j01-notify

say "=== путь 2: учётка X создаёт Certificate в своём пространстве ==="
submit_certificate j02c-own "$OWN_SAN" || fatal "X не смог создать Certificate со своим SAN"
submit_certificate j01c-notify "$NOTIFY_SAN" || fatal "X не смог создать Certificate с SAN notify"
expect_issued "J02-c (свой SAN)" certificate j02c-own "$OWN_SAN"
expect_denied "J01-c (SAN notify)" certificate j01c-notify

echo
if [ "$N" -ne "$EXPECTED_ASSERTIONS" ]; then
  echo "FAIL: $SCRIPT — утверждений исполнено $N из $EXPECTED_ASSERTIONS: секция пропущена" >&2
  exit 1
fi
if [ "$FAILED" -ne 0 ]; then
  echo "FAIL: $SCRIPT — политика выпуска сертификатов служб НЕ действует (см. ✗ выше); утверждений $N из $EXPECTED_ASSERTIONS" >&2
  exit 1
fi
echo "PASS: $SCRIPT ($N assertions) — J01, J02 и их пара через Certificate на $ctx"
