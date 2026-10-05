#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# seed-stand-dkim.sh — посев объекта ключа DKIM стенда и зоны DNS стенда
# (NTF-1 Р19 «Профиль стенда», замысел §12а «Посев»; полоса D10; CX1-139).
#
#   seed-stand-dkim.sh <цепочка> [доп. аргументы helm template…]
#
# ЗАЧЕМ. Страж DNS установки notify на старте читает DKIM-запись под селектором
# объекта ключа, SPF и DMARC домена отправителя и без них в готовность не
# выходит. На стенде ключ — посев, а записи отвечает сервер зоны стенда
# (umbrella/templates/stand-dns.yaml). Рецепт подъёма зовёт этот скрипт ДО
# `helm upgrade`: под сервера зоны монтирует объект зоны, под notify — объект
# ключа, и без них оба остаются до старта с именем объекта.
#
# ЧТО ВЫВОДИТСЯ И ОТКУДА — ИЗ РЕНДЕРА ЦЕПОЧКИ, А НЕ ВТОРЫМ НАПИСАНИЕМ:
#   · домен зоны — аннотация `kacho.cloud/stand-dns-zone` пода сервера зоны;
#   · имя объекта зоны — том `zone` пода сервера зоны;
#   · имя объекта ключа и имена его ключей — том `dkim` пода notify и пути
#     KACHO_NOTIFY_DKIM_KEY_FILE / KACHO_NOTIFY_DKIM_SELECTOR_FILE его карты.
# Зона в цепочке выключена (рендер без сервера зоны) — сеять нечего, код 0.
#
# ИСТОЧНИК ОДИН — ОБЪЕКТ КЛЮЧА:
#   · пару RSA 2048 и селектор скрипт выпускает, ТОЛЬКО если объекта нет;
#     существующий объект не трогается (смена ключа — дело оператора и
#     перепроверки notify, а не повторного подъёма);
#   · объект зоны каждый прогон заново ВЫВОДИТСЯ из открытого ключа и
#     селектора существующего объекта и перезаписывается (серийный номер —
#     от содержимого: второй прогон объект не меняет);
#   · после записи — сверка согласия: (N, E) открытого ключа объекта = (N, E)
#     ключа в TXT зоны, селектор зоны = селектор объекта. Расхождение — отказ с
#     именами объектов, без содержимого ключа.
# Закрытого ключа сервер зоны не монтирует; в дерево пара не коммитится.
#
# Коды: 0 — посеяно и согласно (либо зона выключена), 1 — отказ посева или
# сверки, 2 — условие не создано (нет инструмента, рендер не прошёл, кластер не
# ответил).
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"
NS="${KACHO_NAMESPACE:-${STACK_NAMESPACE:-kacho}}"
RELEASE="${STACK_RELEASE:-kacho-umbrella}"

die()  { printf 'ABORT: seed-stand-dkim — %s\n' "$1" >&2; exit "${2:-1}"; }
log()  { printf '=== seed-stand-dkim: %s\n' "$1"; }

STACK="${1:-}"
[ -n "$STACK" ] || die "цепочка не названа: умолчания нет — скрипт пишет в кластер активного контекста" 2
shift

for t in helm kubectl python3 openssl base64 sha256sum; do
  command -v "$t" >/dev/null 2>&1 || die "нет '$t' — сеять нечем (условие прогона, а не находка)" 2
done
python3 -c 'import yaml' 2>/dev/null || die "нет PyYAML — рендер разобрать нечем" 2

# shellcheck source=../tests/helm/stacks.sh
. "$DEPLOY_ROOT/tests/helm/stacks.sh"
ARGS="$(stacks_args "$STACK" "$UMBRELLA")" || die "цепочка '$STACK' не прочиталась" 2
[ -n "$ARGS" ] || die "цепочка '$STACK' прочиталась ПУСТОЙ" 2

# shellcheck disable=SC2086
RENDER="$(helm template "$RELEASE" "$UMBRELLA" -n "$NS" $ARGS "$@" 2>&1)" || {
  printf '%s\n' "$RENDER" >&2
  die "helm template цепочки '$STACK' отказал (текст выше) — выводить посев не из чего" 2
}

# Одна строка: зона имя_объекта_зоны объект_ключа ключ_закрытого ключ_селектора
# либо «off», когда сервера зоны в рендере нет.
DECL="$(printf '%s\n' "$RENDER" | python3 -c '
import os, sys, yaml
docs = [d for d in yaml.safe_load_all(sys.stdin) if isinstance(d, dict)]
def labels(d): return (d.get("metadata") or {}).get("labels") or {}
srv = [d for d in docs if d.get("kind") == "Deployment" and labels(d).get("app.kubernetes.io/name") == "stand-dns"]
if not srv:
    print("off"); sys.exit(0)
if len(srv) != 1:
    sys.exit("серверов зоны в рендере %d, ожидался один" % len(srv))
tpl = srv[0]["spec"]["template"]
zone = ((tpl.get("metadata") or {}).get("annotations") or {}).get("kacho.cloud/stand-dns-zone")
zcm = [v["configMap"]["name"] for v in tpl["spec"].get("volumes") or [] if v.get("name") == "zone" and v.get("configMap")]
if not zone or len(zcm) != 1:
    sys.exit("у сервера зоны нет аннотации домена зоны или тома zone")
nd = [d for d in docs if d.get("kind") == "Deployment" and labels(d).get("app.kubernetes.io/component") == "notify"]
if len(nd) != 1:
    sys.exit("зона стенда включена, а notify в рендере %d — объект ключа DKIM не назван ничем" % len(nd))
vols = [v["secret"]["secretName"] for v in nd[0]["spec"]["template"]["spec"].get("volumes") or [] if v.get("name") == "dkim" and v.get("secret")]
cm = [d for d in docs if d.get("kind") == "ConfigMap" and "KACHO_NOTIFY_DKIM_KEY_FILE" in (d.get("data") or {})]
if len(vols) != 1 or len(cm) != 1:
    sys.exit("у notify нет тома dkim или карты с путями пары DKIM")
data = cm[0]["data"]
key = os.path.basename(data["KACHO_NOTIFY_DKIM_KEY_FILE"])
sel = os.path.basename(data["KACHO_NOTIFY_DKIM_SELECTOR_FILE"])
svc = [d for d in docs if d.get("kind") == "Service" and labels(d).get("app.kubernetes.io/name") == "stand-dns"]
if len(svc) != 1 or not (svc[0].get("spec") or {}).get("clusterIP"):
    sys.exit("у сервера зоны нет службы с адресом clusterIP")
print(zone, zcm[0], vols[0], key, sel, svc[0]["metadata"]["name"], svc[0]["spec"]["clusterIP"])
')" || die "объявление посева из рендера не выведено (причина выше)" 2

if [ "$DECL" = off ]; then
  log "цепочка $STACK: зона DNS стенда выключена — сеять нечего"
  exit 0
fi
read -r ZONE ZONE_CM DKIM_NAMED KEY_KEY SEL_KEY SVC_NAME SVC_IP <<<"$DECL"
# Имя объекта ключа СТЕНДА — литерал посева, а не выведенная величина: проверка
# дерева (deploy/tests/helm/secret-material-survives-recreation-test.sh) судит,
# что каждое заведение секрета стоит в ветке переиспользования ЭТОГО ЖЕ имени, и
# имя переменной она судить не может. Рендер, назвавший другой объект, — отказ:
# профиль стенда разошёлся с посевом, и под notify ждал бы объекта, которого
# посев не заводит.
DKIM_SECRET=kacho-notify-dkim
[ "$DKIM_NAMED" = "$DKIM_SECRET" ] \
  || die "цепочка $STACK называет объект ключа DKIM «$DKIM_NAMED», а посев стенда заводит $DKIM_SECRET
       (global.kacho.identity.smtp.dkim.secretName профиля стенда)" 1
log "цепочка $STACK: зона $ZONE · объект зоны $ZONE_CM · объект ключа $DKIM_SECRET ($KEY_KEY, $SEL_KEY) · служба $SVC_NAME $SVC_IP · ns $NS"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

kubectl get namespace "$NS" >/dev/null 2>&1 \
  || kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f - >/dev/null \
  || die "кластер не отвечает: ни прочитать, ни завести namespace '$NS'" 2

# ── 0. Адрес службы сервера зоны — до `helm upgrade` (§12а (б)) ──────────────
# Служба с этим адресом уже есть и это сервер зоны этого релиза — проверка
# пройдена. Иначе сервер API судит адрес пробной службой `--dry-run=server`: вне
# диапазона служб кластера или занятый — отказ с именем цепочки и адресом.
have_ip="$(kubectl -n "$NS" get service "$SVC_NAME" -o 'jsonpath={.spec.clusterIP}' 2>/dev/null || true)"
if [ "$have_ip" = "$SVC_IP" ]; then
  log "адрес $SVC_IP уже занят сервером зоны $SVC_NAME этого релиза"
elif ! out="$(kubectl -n "$NS" create service clusterip "$SVC_NAME-addr-probe" \
        --clusterip="$SVC_IP" --tcp=53:53 --dry-run=server 2>&1)"; then
  die "цепочка $STACK: адрес службы сервера зоны $SVC_IP (global.kacho.standDNS.serviceIP) отвергнут
       сервером API — вне диапазона служб кластера либо занят: $out" 1
else
  log "адрес $SVC_IP принят сервером API (--dry-run=server)"
fi

# ── 1. Объект ключа: выпуск только при отсутствии ─────────────────────────────
if out="$(kubectl -n "$NS" get secret kacho-notify-dkim -o name 2>&1)"; then
  log "$DKIM_SECRET уже есть — переиспользуется, пара не трогается"
elif [[ "$out" == *"(NotFound)"* ]]; then
  openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$WORK/new.key" 2>/dev/null \
    || die "пара RSA 2048 не выпущена" 1
  # Селектор — метка RFC 6376 §3.1 с датой выпуска: новая пара — новый селектор.
  printf 'stand%s' "$(date -u +%Y%m%d)" > "$WORK/new.sel"
  if ! out="$(kubectl -n "$NS" create secret generic kacho-notify-dkim \
        --from-file="$KEY_KEY=$WORK/new.key" --from-file="$SEL_KEY=$WORK/new.sel" 2>&1)"; then
    case "$out" in
      *"(AlreadyExists)"*) log "$DKIM_SECRET появился между проверкой и заведением — переиспользуется" ;;
      *) die "заведение $DKIM_SECRET отказало: $out" 1 ;;
    esac
  else
    log "$DKIM_SECRET выпущен: RSA 2048, селектор $(cat "$WORK/new.sel")"
  fi
  rm -f "$WORK/new.key"
else
  die "есть ли объект $DKIM_SECRET, НЕ УСТАНОВЛЕНО — сервер ответил не NotFound, а отказом: $out" 2
fi

# ── 2. Открытый ключ и селектор существующего объекта ─────────────────────────
secret_field() { # secret_field <ключ объекта> <файл>
  kubectl -n "$NS" get secret "$DKIM_SECRET" -o "go-template={{index .data \"$1\"}}" | base64 -d > "$2"
}
secret_field "$KEY_KEY" "$WORK/obj.key" || die "ключ $KEY_KEY объекта $DKIM_SECRET не прочитан" 2
secret_field "$SEL_KEY" "$WORK/obj.sel" || die "ключ $SEL_KEY объекта $DKIM_SECRET не прочитан" 2
SELECTOR="$(tr -d '[:space:]' < "$WORK/obj.sel")"
[[ "$SELECTOR" =~ ^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$ ]] \
  || die "селектор объекта $DKIM_SECRET вне формы метки RFC 6376 §3.1" 1
openssl pkey -in "$WORK/obj.key" -pubout -outform DER -out "$WORK/obj.pub.der" 2>/dev/null \
  || die "закрытый ключ объекта $DKIM_SECRET не разбирается" 1
rm -f "$WORK/obj.key"
P_VALUE="$(base64 -w0 < "$WORK/obj.pub.der")"

# ── 3. Объект зоны — выводится заново каждый прогон ───────────────────────────
# TXT длиннее 255 байт режется на строки одной записи; резолвер склеивает их.
txt_chunks() { local s="$1" out=""; while [ -n "$s" ]; do out="$out \"${s:0:200}\""; s="${s:200}"; done; printf '%s' "${out# }"; }
BODY="\$ORIGIN ${ZONE}.
\$TTL 60
@ IN NS ns.${ZONE}.
ns IN A 127.0.0.1
@ IN TXT \"v=spf1 -all\"
_dmarc IN TXT \"v=DMARC1; p=reject\"
${SELECTOR}._domainkey IN TXT $(txt_chunks "v=DKIM1; k=rsa; p=${P_VALUE}")
"
# Серийный номер — от содержимого: второй прогон даёт тот же объект.
SERIAL=$(( 16#$(printf '%s' "$BODY" | sha256sum | cut -c1-7) ))
{
  printf '%s\n' "\$ORIGIN ${ZONE}."
  printf '@ 60 IN SOA ns.%s. hostmaster.%s. %s 60 60 600 60\n' "$ZONE" "$ZONE" "$SERIAL"
  printf '%s' "$BODY"
} > "$WORK/db.zone"
kubectl -n "$NS" create configmap "$ZONE_CM" --from-file=db.zone="$WORK/db.zone" \
  --dry-run=client -o yaml \
  | kubectl label --local -f - -o yaml kacho.cloud/component=stand-dns app.kubernetes.io/name=stand-dns \
  | kubectl -n "$NS" apply -f - >/dev/null \
  || die "объект зоны $ZONE_CM не записан" 1
log "$ZONE_CM записан: SPF, DMARC, DKIM под селектором $SELECTOR (серийный $SERIAL)"

# ── 4. Сверка согласия: (N, E) и селектор объекта зоны против объекта ключа ──
kubectl -n "$NS" get configmap "$ZONE_CM" -o 'go-template={{index .data "db.zone"}}' > "$WORK/back.zone" \
  || die "объект зоны $ZONE_CM не прочитан обратно" 2
ZONE_SEL="$(sed -n 's/^\([^ ]*\)\._domainkey IN TXT .*/\1/p' "$WORK/back.zone")"
ZONE_P="$(sed -n 's/^[^ ]*\._domainkey IN TXT //p' "$WORK/back.zone" | tr -d '" ' | sed -n 's/.*;p=//p')"
[ "$ZONE_SEL" = "$SELECTOR" ] \
  || die "селектор объекта зоны $ZONE_CM не равен селектору объекта ключа $DKIM_SECRET" 1
printf '%s' "$ZONE_P" | base64 -d > "$WORK/zone.pub.der" 2>/dev/null \
  || die "ключ в TXT объекта зоны $ZONE_CM не base64" 1
ne() { openssl rsa -pubin -inform DER -in "$1" -noout -modulus -text 2>/dev/null | grep -E '^(Modulus=|Exponent:)'; }
[ -n "$(ne "$WORK/obj.pub.der")" ] && [ "$(ne "$WORK/obj.pub.der")" = "$(ne "$WORK/zone.pub.der")" ] \
  || die "(N, E) ключа в TXT объекта зоны $ZONE_CM не равны открытому ключу объекта $DKIM_SECRET" 1
log "согласие: (N, E) и селектор объекта зоны равны объекту ключа"
