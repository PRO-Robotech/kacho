#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# stand-registry-forwarder-probe.sh — сквозная проба доверенного пересыльщика
# анонимной выдачи токена реестра (kacho#3330; kaname#928, kaname#930).
#
#   stand-registry-forwarder-probe.sh overlay <файл>   записать накладку стенда:
#       анонимный поток службы доступа (темп 1 в секунду на источник) и
#       TLS-сайдкар плоскости данных реестра; передаётся в STAND_VALUES
#   stand-registry-forwarder-probe.sh run              пробу — на поднятом стенде
#       (KACHO_STAND_NS, KACHO_CLIENT_CONTEXT экспортирует `stand-ns.sh run`)
#
# ЧТО УТВЕРЖДАЕТСЯ. Служба доступа берёт адрес клиента из X-Forwarded-For только
# у пира с проверенным клиентским листом объявленного пересыльщика; сайдкар
# реестра — этот пир. Запросы идут через НАСТОЯЩИЙ сайдкар (служба плоскости
# данных реестра, порт dp-tls) от четырёх клиентских подов с разными адресами,
# пачками по N запросов без удостоверения:
#   S — две пачки ОДНОГО пода одновременно: один источник, близнец «общий ключ»;
#   T — по пачке двух РАЗНЫХ подов одновременно: независимые темпы, T > S;
#   P — пачка одного пода, где каждый запрос подделывает X-Forwarded-For своим
#       адресом: сайдкар заголовок перезаписывает, ключ не меняется, P < N.
# Сертификат пересыльщика не дошёл — служба ключует по адресу сайдкара, все
# четыре пода — один источник, и T == S (красное). Сайдкар дописывает заголовок
# вместо перезаписи — P == N (красное).
#
# ИСХОДЫ: 0 — утверждения выполнены; 1 — утверждение нарушено (дефект продукта);
# 125 — условие прогона не создано (сайдкар не поднят, поток не включён,
# клиентские поды не готовы) — это не вердикт о продукте.
#
# Клиентские поды снимаются на выходе (trap), стенд снимает `stand-ns.sh run`.
set -euo pipefail

N="${PROBE_BURST:-6}"
IMAGE="${PROBE_CURL_IMAGE:-curlimages/curl:8.10.1}"
PODS=(fwd-probe-a fwd-probe-b fwd-probe-c fwd-probe-d)

say() { printf 'forwarder-probe: %s\n' "$*"; }
not_created() { say "УСЛОВИЕ НЕ СОЗДАНО: $*"; exit 125; }

overlay() {
  local f="$1"
  cat >"$f" <<'YAML'
# Накладка стенда пробы kacho#3330 — только пространство стенда.
kaname:
  config:
    apiServer:
      registryToken:
        anonymousClientId: registry-anonymous
        anonymousPerSourcePerSec: 1
        anonymousForwarderSan: spiffe://kacho.cloud/ns/kacho/sa/kacho-registry
registry:
  service:
    dataplaneLB:
      enabled: true
      tlsSidecar:
        enabled: true
YAML
  say "накладка записана: $f"
}

k() { kubectl --context "$KACHO_CLIENT_CONTEXT" -n "$KACHO_STAND_NS" "$@"; }

cleanup() { k delete pod "${PODS[@]}" --ignore-not-found --wait=false >/dev/null 2>&1 || true; }

# burst ПОД СМЕЩЕНИЕ_ПОДДЕЛКИ — N запросов без удостоверения; печатает коды
# ответа по строке. СМЕЩЕНИЕ_ПОДДЕЛКИ > 0 — каждый запрос несёт свой
# X-Forwarded-For 198.51.100.<смещение+i>.
burst() {
  local pod="$1" spoof="$2"
  # shellcheck disable=SC2016 # тело исполняет sh пода, доводы — позиционные
  k exec "$pod" -- sh -c '
    n=$1; spoof=$2; ip=$3; aud=$4
    i=1
    while [ "$i" -le "$n" ]; do
      if [ "$spoof" -gt 0 ]; then h="X-Forwarded-For: 198.51.100.$((spoof + i))"; else h="X-Probe: plain"; fi
      curl -s -o /dev/null -w "%{http_code}\n" --max-time 10 \
        --cacert /probe-ca/ca.crt --resolve "registry.in-cloud.io:443:$ip" -H "$h" \
        "https://registry.in-cloud.io/iam/token?service=$aud&scope=repository:probe/forwarder:pull"
      i=$((i + 1))
    done' _ "$N" "$spoof" "$LB_IP" "$AUD"
}

count() { grep -c "^$1$" || true; }

run() {
  : "${KACHO_STAND_NS:?KACHO_STAND_NS не задан — пробу зовёт stand-ns.sh run}"
  : "${KACHO_CLIENT_CONTEXT:?KACHO_CLIENT_CONTEXT не задан}"
  trap cleanup EXIT

  # ── условие прогона ──
  local names
  names="$(k get deploy registry -o jsonpath='{.spec.template.spec.containers[*].name}')"
  [[ " $names " == *" dataplane-tls "* ]] ||
    not_created "у пода реестра нет контейнера dataplane-tls — сайдкар не поднят"
  k rollout status deploy/registry --timeout=180s >/dev/null || not_created "под реестра не готов"
  local cm
  cm="$(k get configmap -o name | grep -E 'kaname.*-config$' | head -1)"
  [ -n "$cm" ] || not_created "карты настроек службы доступа нет"
  local cfg
  cfg="$(k get "$cm" -o jsonpath='{.data.config\.yaml}')"
  [[ "$cfg" == *"anonymous-forwarder-san:"* ]] ||
    not_created "анонимный поток службы доступа не включён (в $cm нет anonymous-forwarder-san)"
  LB_IP="$(k get svc registry-lb -o jsonpath='{.spec.clusterIP}')"
  [ -n "$LB_IP" ] || not_created "службы плоскости данных registry-lb нет"
  AUD="$(k get deploy registry -o jsonpath='{.spec.template.spec.containers[?(@.name=="registry")].env[?(@.name=="KACHO_REGISTRY_SERVICE_AUD")].value}')"
  [ -n "$AUD" ] || not_created "имя службы докерной полосы (KACHO_REGISTRY_SERVICE_AUD) не выведено"
  local secret
  secret="$(k get deploy registry -o jsonpath='{.spec.template.spec.volumes[?(@.name=="registry-dataplane-tls")].secret.secretName}')"
  [ -n "$secret" ] || not_created "секрета листа сайдкара нет"
  say "сайдкар: служба registry-lb $LB_IP:443; адресат $AUD; поток: $cm; пачка $N"

  local p
  for p in "${PODS[@]}"; do
    k apply -f - >/dev/null <<YAML
apiVersion: v1
kind: Pod
metadata:
  name: $p
  labels: {kacho.io/probe: registry-forwarder}
spec:
  restartPolicy: Never
  securityContext:
    runAsNonRoot: true
    runAsUser: 10001
    seccompProfile: {type: RuntimeDefault}
  containers:
    - name: curl
      image: $IMAGE
      command: ["sleep", "3600"]
      resources:
        requests: {cpu: 10m, memory: 16Mi}
        limits: {cpu: 100m, memory: 64Mi}
      securityContext:
        allowPrivilegeEscalation: false
        capabilities: {drop: ["ALL"]}
      volumeMounts:
        - {name: ca, mountPath: /probe-ca, readOnly: true}
  volumes:
    - name: ca
      secret:
        secretName: $secret
        items: [{key: ca.crt, path: ca.crt}]
YAML
  done
  k wait --for=condition=Ready pod "${PODS[@]}" --timeout=180s >/dev/null || not_created "клиентские поды не готовы"
  local ips
  ips="$(k get pod "${PODS[@]}" -o jsonpath='{range .items[*]}{.status.podIP}{" "}{end}')"
  say "адреса клиентов: $ips"
  [ "$(tr ' ' '\n' <<<"$ips" | grep -c . | tr -d ' ')" -eq 4 ] || not_created "у клиентских подов нет четырёх адресов"
  [ "$(tr ' ' '\n' <<<"$ips" | grep . | sort -u | wc -l | tr -d ' ')" -eq 4 ] || not_created "адреса клиентов совпадают"

  local rep
  rep="$(k get deploy kaname -o jsonpath='{.status.readyReplicas}')"
  [ "$rep" = 1 ] || not_created "готовых реплик службы доступа «$rep», нужна одна: темп — на реплику, и две реплики делили бы пачку"

  # S: две пачки одного пода одновременно; T: по пачке двух подов одновременно;
  # P: пачка одного пода с подделанным заголовком. Строка ответа — «под код».
  local fn s t pd
  fn="$(declare -f k burst); N=$N LB_IP=$LB_IP AUD=$AUD KACHO_CLIENT_CONTEXT=$KACHO_CLIENT_CONTEXT KACHO_STAND_NS=$KACHO_STAND_NS burst \"\$1\" \"\$2\" | sed \"s/^/\$1 /\""
  s="$(printf 'fwd-probe-a 0\nfwd-probe-a 0\n' | xargs -P 2 -L 1 bash -c "$fn" _)"
  t="$(printf 'fwd-probe-b 0\nfwd-probe-c 0\n' | xargs -P 2 -L 1 bash -c "$fn" _)"
  pd="$(printf 'fwd-probe-d 10\n' | xargs -L 1 bash -c "$fn" _)"
  local s200 s429 t200 t429 b200 c200 p200 p429
  s200="$(count '.* 200' <<<"$s")"; s429="$(count '.* 429' <<<"$s")"
  t200="$(count '.* 200' <<<"$t")"; t429="$(count '.* 429' <<<"$t")"
  b200="$(count 'fwd-probe-b 200' <<<"$t")"; c200="$(count 'fwd-probe-c 200' <<<"$t")"
  p200="$(count '.* 200' <<<"$pd")"; p429="$(count '.* 429' <<<"$pd")"
  say "S (один под, две пачки по $N): 200=$s200 429=$s429"
  say "T (два пода, по пачке $N):     200=$t200 (b=$b200, c=$c200) 429=$t429"
  say "P (подделка XFF, пачка $N):    200=$p200 429=$p429"

  [ $((s200 + s429)) -eq $((2 * N)) ] && [ $((t200 + t429)) -eq $((2 * N)) ] && [ $((p200 + p429)) -eq "$N" ] ||
    { say "НЕ ВЫПОЛНИЛОСЬ: среди ответов есть коды кроме 200 и 429:"; printf '%s\n' "$s" "$t" "$pd" | sort | uniq -c; exit 125; }
  [ "$s200" -ge 1 ] || not_created "ни одного 200 у анонимного запроса через сайдкар — путь выдачи не поднят"
  # Близнец «один ключ»: две одновременные пачки одного источника упираются в
  # темп — иначе пачка не различает общий ключ и независимые.
  [ "$s429" -gt 0 ] && [ "$s200" -le 2 ] ||
    { say "НЕ ВЫПОЛНИЛОСЬ: один источник не упёрся в темп (200=$s200) — пачки не уложились в окно ведра"; exit 125; }

  local bad=0
  if [ "$b200" -lt 1 ] || [ "$c200" -lt 1 ]; then
    say "КРАСНОЕ: два клиента с разными адресами делят темп (b=$b200, c=$c200 ответов 200) — ключ не адрес клиента"
    bad=1
  fi
  if [ "$p200" -gt 2 ]; then
    say "КРАСНОЕ: подделанный X-Forwarded-For менял ключ темпа ($p200 из $N — 200)"
    bad=1
  fi
  [ "$bad" -eq 0 ] || exit 1
  say "ЗЕЛЁНОЕ: у двух адресов независимые темпы (b=$b200, c=$c200), один источник ограничен ($s200 из $((2 * N))), подделанный заголовок ключа не меняет ($p200 из $N)"
}

case "${1:-}" in
  overlay) overlay "${2:?файл накладки не назван}" ;;
  run) run ;;
  *) echo "использование: $0 overlay <файл> | run" >&2; exit 2 ;;
esac
