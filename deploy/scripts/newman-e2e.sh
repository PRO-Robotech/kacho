#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# newman-e2e.sh — REPRODUCIBLE newman e2e flow against the running dev stand.
#
# Replaces the manual "seed tokens by hand" path with a deterministic,
# committed flow:
#   1. port-forward api-gateway public + internal-rest + kaname-internal — on ports
#      of THIS run (free ones from the kernel unless GW_PORT / GW_INTERNAL_PORT /
#      IAM_INTERNAL_PORT … are set; kacho#3246), each forward checked alive
#   2. seed auth fixtures via tests/authz-fixtures/setup.sh (idempotent):
#      mints non-expiring dev JWTs, upserts users, accounts/projects, grants
#      cluster-admin (SQL backdoor), seeds VPC networks, and PATCHES every
#      service's newman environment (existingProjectId, jwt*, …).
#   3. run the requested service's newman collection(s): {{baseUrl}} → public forward,
#      {{internalBaseUrl}} → internal-rest forward (Internal*-RPC живут ТОЛЬКО там — ban #6).
#   4. tear down the port-forwards on exit.
#
# Usage (after `make dev-up`):
#   make e2e-newman SVC=vpc                      # whole vpc suite
#   make e2e-newman SVC=vpc COLLECTION=internal-network
#   ./scripts/newman-e2e.sh vpc internal-network
#
# Prereqs (fail-fast): kubectl, python3, newman, grpcurl. The flow is
# environment-agnostic — same script seeds + runs in CI and locally.
set -euo pipefail

# ── САМОПРОВЕРКА (kacho#3246): `newman-e2e.sh --self-test` ────────────────────
#
# Прогонщик заводится в мире подменных kubectl/newman/grpcurl, где проброс к
# названному порту Service не встаёт (`bind: address already in use` — так он
# отказывает на общей машине). Утверждается:
#   A. проброс не встал — выход 2 «ПРОГОН НЕДЕЙСТВИТЕЛЕН», причина из журнала
#      этого проброса, до посева прогон не доходит;
#   B. БЛИЗНЕЦ: все пробросы живы — страж молчит, прогон идёт дальше (в подменном
#      мире он останавливается на отсутствии секрета — то есть ПОСЛЕ стража);
#   C. порты, не заданные ручками, у прогона свои: попарно различны, и второй прогон
#      получает другой набор (постоянные порты делили бы два прогона на одной машине).
if [ "${1:-}" = "--self-test" ]; then
  set +e   # исход каждой проверки читается кодом, а не обрывом самопроверки
  _st_dir="$(mktemp -d)"; trap 'rm -rf "$_st_dir"' EXIT
  mkdir -p "$_st_dir/bin"
  cat >"$_st_dir/bin/kubectl" <<'FAKE'
#!/usr/bin/env bash
# Подменный kubectl: port-forward живёт, пока его не снимут, либо отказывает
# привязкой, когда порт Service равен FAKE_PF_FAIL_REMOTE; прочее — отказ.
for a in "$@"; do
  if [ "$a" = port-forward ]; then
    last="${*: -1}"
    if [ -n "${FAKE_PF_FAIL_REMOTE:-}" ] && [ "${last##*:}" = "$FAKE_PF_FAIL_REMOTE" ]; then
      echo "Unable to listen on port ${last%%:*}: bind: address already in use" >&2; exit 1
    fi
    exec sleep 60
  fi
done
exit 1
FAKE
  printf '#!/usr/bin/env bash\nexit 0\n' >"$_st_dir/bin/newman"
  cp "$_st_dir/bin/newman" "$_st_dir/bin/grpcurl"
  chmod +x "$_st_dir/bin/"*
  _st_fail=0; _st_n=0
  _st_check() { _st_n=$((_st_n+1)); if [ "$2" = 0 ]; then echo "ок    $1"; else echo "ОТКАЗ $1"; _st_fail=$((_st_fail+1)); fi; }
  _st_run() {  # <FAKE_PF_FAIL_REMOTE> → вывод в $_st_dir/out, код в _st_rc
    env -u GW_PORT -u GW_INTERNAL_PORT -u GW_TLS_PORT -u IAM_INTERNAL_PORT -u IAM_JWKS_PORT \
        -u IAM_REGTOKEN_PORT -u OWN_REST_PORT -u OWN_INTERNAL_REST_PORT -u MAILBOX_PORT \
        PATH="$_st_dir/bin:$PATH" FAKE_PF_FAIL_REMOTE="$1" SETUP_NS=t0-selftest \
        timeout 60 bash "${BASH_SOURCE[0]}" api-gateway none >"$_st_dir/out" 2>&1
    _st_rc=$?
  }
  _st_run 8081
  grep -q "ПРОГОН НЕДЕЙСТВИТЕЛЕН: проброс не встал" "$_st_dir/out" && [ "$_st_rc" = 2 ] \
    && ! grep -q "seeding auth fixtures" "$_st_dir/out"
  _st_check "A: проброс не встал — выход 2 до посева (код $_st_rc)" $?
  grep -q "address already in use" "$_st_dir/out"
  _st_check "A: причина взята из журнала ЭТОГО проброса" $?
  _st_run ""
  ! grep -q "ПРОГОН НЕДЕЙСТВИТЕЛЕН" "$_st_dir/out" && grep -q "FATAL: секрета" "$_st_dir/out"
  _st_check "B: пробросы живы — страж молчит, прогон идёт дальше (код $_st_rc)" $?
  _st_ports="$(sed -n 's/^\[e2e\] порты прогона: //p' "$_st_dir/out")"
  _st_ok=1
  if [ "$(wc -w <<<"$_st_ports")" = 9 ] && [ "$(tr ' ' '\n' <<<"$_st_ports" | sort -u | wc -l)" = 9 ]; then _st_ok=0; fi
  _st_check "C: девять портов прогона попарно различны («$_st_ports»)" "$_st_ok"
  _st_run ""
  _st_ports2="$(sed -n 's/^\[e2e\] порты прогона: //p' "$_st_dir/out")"
  _st_ok=1
  if [ -n "$_st_ports2" ] && [ "$_st_ports2" != "$_st_ports" ]; then _st_ok=0; fi
  _st_check "C: второй прогон получил СВОИ порты, а не те же постоянные («$_st_ports2»)" "$_st_ok"
  [ "$_st_fail" = 0 ] || { echo "--- вывод последнего прогона:"; cat "$_st_dir/out"; }
  echo "перепись: проверок исполнено $_st_n, отказов $_st_fail"
  [ "$_st_n" -gt 0 ] && [ "$_st_fail" = 0 ]; exit $?
fi

# ── ПОРТЫ ПРОБРОСА — СВОИ У ПРОГОНА (kacho#3246) ──────────────────────────────
#
# Порты были постоянными (18080, 18081, 19091 …), а на машине одновременно идут до
# трёх прогонов слота newman, каждый против СВОЕГО пространства стенда. Второй
# прогон не мог привязать занятый порт, его проброс умирал, а запросы уходили в
# проброс ПЕРВОГО — к чужому стенду: наблюдалось «tls: unknown certificate
# authority» на чеканке (лист этого стенда предъявлен чужой службе) и «no op id: {}»
# посева F8-S7-80 (пустой ответ вместо ответа края). Порт, не заданный ручкой,
# теперь берётся у ядра свободным, все девять — одним вызовом, чтобы они не совпали
# между собой. Ручки прежние и по-прежнему перекрывают.
read -r _fp1 _fp2 _fp3 _fp4 _fp5 _fp6 _fp7 _fp8 _fp9 < <(python3 -c '
import socket
ss = [socket.socket() for _ in range(9)]
for s in ss:
    s.bind(("127.0.0.1", 0))
print(" ".join(str(s.getsockname()[1]) for s in ss))
')

SVC="${1:-${SVC:-vpc}}"
COLLECTION="${2:-${COLLECTION:-}}"
NS="${SETUP_NS:-kacho}"
GW_PORT="${GW_PORT:-$_fp1}"
GW_INTERNAL_PORT="${GW_INTERNAL_PORT:-$_fp2}"   # api-gateway internal-rest :8081 (Internal*-RPC)
# api-gateway EXTERNAL TLS listener :8443 (advertised as api.kacho.local:443). The ban-#6
# negatives address it here rather than by its advertised hostname: that name does not
# resolve on a developer box, adding it needs root, kind publishes only node:80, and the
# Ingress in front of it speaks GRPCS so every REST path through it answers 502 — every path
# but the three exact ceremony coordinates, which it forwards over HTTPS (kacho#2860). Ban #6
# is about which routes the LISTENER serves, not about the name used to find it.
GW_TLS_PORT="${GW_TLS_PORT:-$_fp3}"
IAM_INTERNAL_PORT="${IAM_INTERNAL_PORT:-$_fp4}"
# Адреса ПОЛОСЫ ФАСАДА (#59, iam-token-facade-conformance). Кейсы IBT-* спрашивают
# сами слушатели — иначе «проверка подписи идёт через фасад» останется утверждением
# о конфигурации, а не о поведении.
IAM_JWKS_PORT="${IAM_JWKS_PORT:-$_fp5}"         # iam JWKS-proxy :9097 (server-TLS)
IAM_REGTOKEN_PORT="${IAM_REGTOKEN_PORT:-$_fp6}" # iam docker-token handle :9096 (server-TLS)
# СОБСТВЕННЫЕ REST-ФРОНТЫ СЛУЖБЫ. Здесь объявляется ТОЛЬКО локальный порт проброса —
# то, что принадлежит этой машине. Порт слушателя, его транспорт и имя Service
# объявляет ПОСАДКА, и читаются они у неё (own-rest-front-address.py): выписанные
# здесь литералом, они разошлись бы с чартом молча, а прогон пошёл бы по прежнему
# адресу и назвал бы чужой ответ ответом фронта.
OWN_REST_PORT="${OWN_REST_PORT:-$_fp7}"                 # собственный публичный REST-фронт
OWN_INTERNAL_REST_PORT="${OWN_INTERNAL_REST_PORT:-$_fp8}" # собственный внутренний REST-фронт
MAILBOX_PORT="${MAILBOX_PORT:-$_fp9}"                   # приёмник писем стенда (поверхность чтения посева)
# Порт data plane реестра здесь не объявляется: адресат — компонент, и порт вместе
# с портовой ручкой живёт в deploy/e2e-shards.json (`optional_transports`).

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Монорепа: deploy/scripts → корень репо на два уровня выше. Раскладка — services/<svc>,
# кроме api-gateway (gateway/). Раньше было
# "$WORKSPACE_DIR/project/kacho-$SVC/tests/newman" — polyrepo-путь к sibling-репо.
# authz-фикстуры тоже переехали: были в kacho-workspace/tests/, теперь tests/ монорепы.
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
if [ "$SVC" = "api-gateway" ]; then
  NEWMAN_DIR="$REPO_ROOT/gateway/tests/newman"
else
  NEWMAN_DIR="$REPO_ROOT/services/$SVC/tests/newman"
fi

for tool in kubectl python3 newman grpcurl; do
  command -v "$tool" >/dev/null 2>&1 || { echo "FATAL: '$tool' not found in PATH" >&2; exit 1; }
done
[ -d "$NEWMAN_DIR" ] || { echo "FATAL: no newman dir for SVC=$SVC ($NEWMAN_DIR)" >&2; exit 1; }

PF_PIDS=()
PF_WHAT=()   # «порт|что|журнал» на каждый проброс — для стража живости ниже
TMP_DIRS=()
# Чистим и порт-форварды, и временные каталоги: в них лежит ПРИВАТНЫЙ КЛЮЧ
# client-cert'а — оставлять его в /tmp после прогона нельзя.
cleanup() {
  for p in "${PF_PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
  for d in "${TMP_DIRS[@]:-}"; do [ -n "$d" ] && rm -rf "$d"; done
}
trap cleanup EXIT
# Журналы пробросов — в СВОЁМ каталоге прогона, а не под общими именами /tmp/e2e-pf-*:
# параллельный прогон перезаписывал их, и причина отказа читалась из чужого журнала.
PF_LOG_DIR="$(mktemp -d)"; TMP_DIRS+=("$PF_LOG_DIR")

# pf_start <локальный порт> <что> <имя журнала> <svc/…> <порт Service>
pf_start() {
  kubectl -n "$NS" port-forward "$4" "$1:$5" >"$PF_LOG_DIR/$3.log" 2>&1 &
  PF_PIDS+=($!); PF_WHAT+=("$1|$2|$PF_LOG_DIR/$3.log")
}

echo "[e2e] порты прогона: $GW_PORT $GW_INTERNAL_PORT $GW_TLS_PORT $IAM_INTERNAL_PORT $IAM_JWKS_PORT $IAM_REGTOKEN_PORT $OWN_REST_PORT $OWN_INTERNAL_REST_PORT $MAILBOX_PORT"
echo "[e2e] port-forward api-gateway :$GW_PORT (public) / :$GW_INTERNAL_PORT (internal-rest) / :$GW_TLS_PORT (external TLS) + kaname-internal :$IAM_INTERNAL_PORT"
pf_start "$GW_PORT" "публичный край api-gateway:8080" gw svc/api-gateway 8080
# internal-rest (:8081) — ОТДЕЛЬНЫЙ листенер для Internal*-RPC. На публичном :8080 их
# нет и быть не должно (ban #6: Internal.* не публикуется на external endpoint), поэтому
# коллекции internal-* обязаны ходить сюда через {{internalBaseUrl}}, иначе получают
# закономерный 404. iam-набор так и делает; vpc-набор — ещё нет (см. README/issue).
pf_start "$GW_INTERNAL_PORT" "внутренний слушатель края api-gateway:8081" gw-internal svc/api-gateway 8081
# external TLS (:8443) — то, что рекламируется наружу. Сюда ходят ban-#6 негативы
# ({{externalBaseUrl}}): Internal*-пути обязаны быть недостижимы на нём.
pf_start "$GW_TLS_PORT" "внешний TLS края api-gateway:8443" gw-tls svc/api-gateway 8443
pf_start "$IAM_INTERNAL_PORT" "внутренний gRPC службы доступа kaname-internal:9091" iam svc/kaname-internal 9091
# Полоса фасада (#59): JWKS-прокси iam, ручка docker-токена iam и data-plane реестра.
pf_start "$IAM_JWKS_PORT" "JWKS службы доступа kaname-internal:9097" iam-jwks svc/kaname-internal 9097
pf_start "$IAM_REGTOKEN_PORT" "ручка токена службы доступа kaname:9096" iam-regtoken svc/kaname 9096
# Приёмник писем стенда — поверхность чтения посева людей (kacho#2901, F6b-53):
# человек наборов подтверждается кодом из письма регистрации, и посев читает его
# здесь. Тот же проброс и тот же довод, что у прогонщика шардов (newman-parallel.sh).
MAILBOX_SVC="${MAILBOX_SVC:-kacho-umbrella-mailpit}"
pf_start "$MAILBOX_PORT" "приёмник писем $MAILBOX_SVC:8025" mailbox "svc/$MAILBOX_SVC" 8025

# ─── СОБСТВЕННЫЕ REST-ФРОНТЫ: АДРЕС ЧИТАЕТСЯ У ПОСАДКИ ──────────────────────
#
# Кейсы `kaname-own-rest-front` спрашивают СОБСТВЕННУЮ поверхность службы, а не
# край платформы, и адресуются к ней своими переменными. Подставить сюда адрес
# края — хуже красноты: кейс позеленеет, проверив чужую поверхность.
#
# АДРЕСА НЕ ВЫДУМЫВАЮТСЯ. Посадка фронта не объявила — переменная НЕ
# инъектируется, и кейс сам скажет об этом ТРЕТЬИМ ИСХОДОМ
# (gen.py::PRECONDITION_MARK → assert-suites-green.sh, код 3). Инъекция
# «какого-нибудь» адреса превратила бы «условия нет» в вердикт о продукте.
OWN_FRONT_ENV_ARGS=()
OWN_FRONT_TLS_ARGS=()   # --ssl-client-* для фронта, чьё ребро ВЗАИМНОЕ

# own_front_client_leaf — тот же помощник, что у прогонщика шардов, и по той же
# причине: лист берётся у ПОСАДКИ, а не выписывается. 0 — лист есть, 1 — секрета нет.
own_front_client_leaf() {
  if [ "${#OWN_FRONT_TLS_ARGS[@]}" -gt 0 ]; then return 0; fi
  kubectl -n "$NS" get secret api-gateway-client-tls >/dev/null 2>&1 || return 1
  local dir; dir="$(mktemp -d)"; TMP_DIRS+=("$dir")
  kubectl -n "$NS" get secret api-gateway-client-tls -o jsonpath='{.data.tls\.crt}' \
    | base64 -d > "$dir/client.crt"
  kubectl -n "$NS" get secret api-gateway-client-tls -o jsonpath='{.data.tls\.key}' \
    | base64 -d > "$dir/client.key"
  chmod 600 "$dir"/*
  OWN_FRONT_TLS_ARGS=(--ssl-client-cert "$dir/client.crt" --ssl-client-key "$dir/client.key")
  echo "[e2e] клиентский лист взят из secret/api-gateway-client-tls"
  return 0
}

own_front_forward() {  # <public|internal> <локальный порт> <имя переменной проб>
  local front="$1" local_port="$2" var="$3" addr scheme port svc leaf
  if ! addr="$(python3 "$SCRIPT_DIR/own-rest-front-address.py" "$front" --namespace "$NS")"; then
    echo "[e2e] $var НЕ инъектируется: посадка адреса не дала — кейсы скажут «условие не создано» сами"
    return 0
  fi
  # Поля — ПО НОМЕРУ: хвост строки (`${addr##*|}`) верен лишь для трёхполевой
  # формы, и добавленное производителем поле молча стало бы именем Service.
  scheme="$(echo "$addr" | cut -d'|' -f1)"
  port="$(echo "$addr" | cut -d'|' -f2)"
  svc="$(echo "$addr" | cut -d'|' -f3)"
  leaf="$(echo "$addr" | cut -d'|' -f4)"

  # ТРЕБОВАНИЕ КЛИЕНТСКОГО ЛИСТА — решение ПРОИЗВОДИТЕЛЯ, зеркалящего продуктовый
  # предикат; здесь оно не вычисляется (вторая копия разошлась бы молча). До этой
  # правки серийный прогонщик листа не подавал ВОВСЕ, и на взаимном ребре каждый
  # запрос его собственных фронтовых кейсов умирал бы на рукопожатии — то есть
  # «ответа нет» вместо вердикта о предмете.
  case "$leaf" in
    required)
      if ! own_front_client_leaf; then
        echo "[e2e] $var НЕ инъектируется: ребро ВЗАИМНОЕ, а secret/api-gateway-client-tls" \
             "в посадке нет — предъявить нечем; это «условие не создано», а не «лист не нужен»"
        return 0
      fi ;;
    not-required)
      echo "[e2e] $var: ребро ОДНОСТОРОННЕЕ — клиентский лист НЕ подаём: он ничего бы не доказал" ;;
    *)
      echo "[e2e] $var НЕ инъектируется: производитель адреса вернул нераспознанное решение" \
           "о клиентском листе (${leaf:-пусто}) в «$addr» — дефект связки, а не посадки"
      return 0 ;;
  esac

  pf_start "$local_port" "собственный фронт $var $svc:$port" "own-$var" "svc/$svc" "$port"
  OWN_FRONT_ENV_ARGS+=(--env-var "$var=$scheme://127.0.0.1:$local_port")
  echo "[e2e] собственный фронт: $var → svc/$svc :$port ($scheme, лист $leaf) на 127.0.0.1:$local_port"
}
own_front_forward public  "$OWN_REST_PORT"          ownRestBaseUrl
own_front_forward internal "$OWN_INTERNAL_REST_PORT" ownInternalRestBaseUrl

# ПРОБРОС К КОМПОНЕНТУ — ПО СПРОСУ, ТЕМ ЖЕ ПРЕДИКАТОМ, ЧТО У newman-parallel.sh.
#
# Все пробросы выше ведут к ЯДРУ и есть на любом стенде. Адресат такого проброса —
# переключаемый компонент, которого на стенде может не быть; `kubectl port-forward
# svc/<нет такого>` не встаёт и ЗАВЕРШАЕТСЯ. У этого скрипта проверки живости
# пробросов нет вовсе, поэтому здесь это давало не «прогон недействителен», а
# мёртвый порт и отказ на сотню строк ниже — про чужой предмет.
#
# Спрос выводится ИЗ ДЕРЕВА для ТОЙ суиты, которую этот запуск и гоняет, и тем же
# модулем, что у прогонщика шардов: два предиката об одном разошлись бы молча.
OPT_ENV_ARGS=()
while IFS='|' read -r _ovar _osvc _otport _oportenv _odport _oscheme _owhy; do
  [ -n "${_ovar:-}" ] || continue
  _oport="${!_oportenv:-$_odport}"
  pf_start "$_oport" "транспорт компонента $_ovar $_osvc:$_otport" "opt-$_ovar" "svc/$_osvc" "$_otport"
  OPT_ENV_ARGS+=(--env-var "$_ovar=$_oscheme://localhost:$_oport")
  echo "[e2e] транспорт компонента: $_ovar → svc/$_osvc :$_otport на localhost:$_oport ($_owhy)"
done < <(python3 "$(dirname "${BASH_SOURCE[0]}")/e2e-optional-transports.py" --suites "$SVC" --census)
sleep 4

# ПРОБРОС, КОТОРЫЙ НЕ ВСТАЛ, ОСТАНАВЛИВАЕТ ПРОГОН ЗДЕСЬ — тот же страж и тот же
# предикат, что у newman-parallel.sh: жив ли НАШ процесс (занятость порта не
# отличает наш проброс от чужого, а kubectl при отказе привязки завершается).
pf_dead=()
for _i in "${!PF_PIDS[@]}"; do
  kill -0 "${PF_PIDS[$_i]}" 2>/dev/null || pf_dead+=("${PF_WHAT[$_i]}")
done
if [ "${#pf_dead[@]}" -gt 0 ]; then
  echo
  echo "===== ПРОГОН НЕДЕЙСТВИТЕЛЕН: проброс не встал (${#pf_dead[@]} из ${#PF_PIDS[@]}) ====="
  for _d in "${pf_dead[@]}"; do
    _port="${_d%%|*}"; _rest="${_d#*|}"; _why="${_rest%%|*}"; _log="${_rest#*|}"
    _first="$(tr -d '\r' <"$_log" 2>/dev/null | grep -a -m1 . || true)"
    echo "  :$_port — $_why"
    echo "      ${_first:-журнал пуст: $_log}"
  done
  echo
  echo "Посев и суиты НЕ запускались. Это НЕ красный прогон и НЕ зелёный — результата нет."
  exit 2
fi

# mTLS для grpcurl → kaname-internal:9091.
#
# dev-стенд идёт с mtls.enabled=true (фаза 2 dev-up), поэтому internal-листенер iam
# требует client-cert: plaintext-grpcurl просто ВИСНЕТ на хендшейке (не падает внятно —
# «context deadline exceeded»), и setup.sh замирает на шаге «upserting test users».
# setup.sh это поддерживает через IAM_INTERNAL_GRPC_MTLS_CERT/_KEY (дефолт — plaintext,
# рассчитан на mTLS-off CI), но кто-то должен ему серт ДАТЬ. Достаём из секрета,
# который выпустил cert-manager: iam принимает любой client-cert, подписанный
# internal-CA (KANAME_INTERNAL_SERVER_MTLS_CLIENTCAFILES = ca.crt того же CA).
MTLS_ENV=()
if kubectl -n "$NS" get secret api-gateway-client-tls >/dev/null 2>&1; then
  CERT_DIR="$(mktemp -d)"; TMP_DIRS+=("$CERT_DIR")
  kubectl -n "$NS" get secret api-gateway-client-tls -o jsonpath='{.data.tls\.crt}' | base64 -d > "$CERT_DIR/client.crt"
  kubectl -n "$NS" get secret api-gateway-client-tls -o jsonpath='{.data.tls\.key}' | base64 -d > "$CERT_DIR/client.key"
  chmod 600 "$CERT_DIR"/*
  # setup.sh ходит grpcurl'ом с -insecure (server-cert не пинится), поэтому нужны
  # только client cert/key — CA не передаём.
  MTLS_ENV=(IAM_INTERNAL_GRPC_MTLS_CERT="$CERT_DIR/client.crt"
            IAM_INTERNAL_GRPC_MTLS_KEY="$CERT_DIR/client.key")
  echo "[e2e] iam-internal: mTLS client-cert взят из secret/api-gateway-client-tls"
else
  echo "[e2e] iam-internal: mTLS-секрета нет — grpcurl пойдёт plaintext (mTLS-off стенд)"
fi
# Bootstrap-operator leaf — a DIFFERENT identity, the only SPIFFE SAN kaname
# allow-lists for MintBootstrapToken (the production seed's entry point). Deliberately
# not the api-gateway one: the gateway fronts tenant traffic, so "is the api-gateway"
# must never be a licence to mint a cluster admin.
if kubectl -n "$NS" get secret kacho-bootstrap-operator-client-tls >/dev/null 2>&1; then
  OP_DIR="$(mktemp -d)"; TMP_DIRS+=("$OP_DIR")
  kubectl -n "$NS" get secret kacho-bootstrap-operator-client-tls -o jsonpath='{.data.tls\.crt}' | base64 -d > "$OP_DIR/client.crt"
  kubectl -n "$NS" get secret kacho-bootstrap-operator-client-tls -o jsonpath='{.data.tls\.key}' | base64 -d > "$OP_DIR/client.key"
  chmod 600 "$OP_DIR"/*
  MTLS_ENV+=(BOOTSTRAP_MINT_MTLS_CERT="$OP_DIR/client.crt"
             BOOTSTRAP_MINT_MTLS_KEY="$OP_DIR/client.key")
fi

# ─── ВНУТРЕННИЙ REST-СЛУШАТЕЛЬ КРАЯ: mTLS И ТОЛЬКО ОН (kacho#3131) ──────────
#
# {{internalBaseUrl}} — https: слушатель требует лист операторской личности
# (kacho-internal-rest-operator-client-tls) и проверяется по УЦ установки, без
# ослабления. Лист — списком по адресу внутреннего слушателя, а не глобальным
# флагом. Посев (setup.sh) получает тот же материал через INTERNAL_REST_*.
# Секрета нет — прогон отказывает здесь: открытым текстом слушатель не отвечает.
# shellcheck source=deploy/scripts/lib/internal-rest-client.sh
. "$SCRIPT_DIR/lib/internal-rest-client.sh"
IR_DIR="$(mktemp -d)"; TMP_DIRS+=("$IR_DIR")
internal_rest_client_leaf "$NS" "$IR_DIR" \
  || { echo "[e2e] FATAL: секрета $INTERNAL_REST_OPERATOR_SECRET нет — внутренний слушатель края недостижим" >&2; exit 1; }
internal_rest_newman_args "$GW_INTERNAL_PORT" "$IR_DIR"

echo "[e2e] seeding auth fixtures (idempotent) + patching newman envs"
env BASE_URL="http://localhost:$GW_PORT" \
INTERNAL_BASE_URL="https://localhost:$GW_INTERNAL_PORT" \
IAM_INTERNAL_GRPC="localhost:$IAM_INTERNAL_PORT" \
PLATFORM_TOKEN_URL="https://127.0.0.1:$IAM_REGTOKEN_PORT/iam/v1/token" \
MAILBOX_URL="http://localhost:$MAILBOX_PORT" \
PATCH_ENV=true SETUP_NS="$NS" \
"${MTLS_ENV[@]}" \
  bash "$REPO_ROOT/tests/authz-fixtures/setup.sh"

# nlb's external-VIP AddressPool is seeded by setup.sh, and ONLY there.
#
# A second pass used to stand here, guarded by "unless the seed ran in production
# posture". That guard could never be false: the seed's classifier leaves `production` as
# the only value standing, and a seed that does not reach that point aborts this script
# (`set -e`) before the read. So the block was unreachable from the day the classifier
# was narrowed — and its fallback named `dev`, the one posture the classifier REFUSES.
#
# Nothing is lost by removing it: setup.sh delegates to prodseed_all.py, which drives
# deploy/scripts/seed-nlb-fixtures.sh itself and is the sole author of that
# cluster-wide default-pool slot. A second author is what the guard was trying to
# prevent — unconditionally now, by not existing.
#
# deploy/scripts/assert-posture-branches-can-be-taken.py keeps this shape from coming
# back: a branch on the seed posture must be able to go both ways.

echo "[e2e] regenerating newman collections"
( cd "$NEWMAN_DIR" && python3 scripts/gen.py >/dev/null )

echo "[e2e] running newman (SVC=$SVC COLLECTION=${COLLECTION:-<all>})"
cd "$NEWMAN_DIR"
if [ -n "$COLLECTION" ]; then
  newman run "collections/${COLLECTION}.postman_collection.json" \
    -e environments/local.postman_environment.json \
    --env-var "baseUrl=http://localhost:$GW_PORT" \
    --env-var "internalBaseUrl=https://localhost:$GW_INTERNAL_PORT" \
    --env-var "externalBaseUrl=https://127.0.0.1:$GW_TLS_PORT" \
    --env-var "iamJwksBaseUrl=https://127.0.0.1:$IAM_JWKS_PORT" \
    --env-var "iamRegistryTokenBaseUrl=https://127.0.0.1:$IAM_REGTOKEN_PORT" \
    "${OWN_FRONT_ENV_ARGS[@]}" \
    ${OWN_FRONT_TLS_ARGS[@]+"${OWN_FRONT_TLS_ARGS[@]}"} \
    "${INTERNAL_REST_NEWMAN_ARGS[@]}" \
    "${OPT_ENV_ARGS[@]}" \
    --delay-request 15 --reporters cli
else
  # run.sh НЕ читает BASE_URL/INTERNAL_BASE_URL из окружения — значения он берёт только
  # из env-файла, а всё неизвестное в argv пробрасывает в newman как есть (массив EXTRA).
  # Поэтому передаём --env-var через argv, а не через env: иначе {{internalBaseUrl}}
  # остаётся пустым и Internal*-шаги молча уходят на публичный порт → 404
  # (internal-pool: 78/0 в одиночном прогоне, но 62/56 в полном — ровно этот разрыв).
  set +e
  ./scripts/run.sh --service "" --delay 15 \
    --env-var "baseUrl=http://localhost:$GW_PORT" \
    --env-var "internalBaseUrl=https://localhost:$GW_INTERNAL_PORT" \
    --env-var "externalBaseUrl=https://127.0.0.1:$GW_TLS_PORT" \
    --env-var "iamJwksBaseUrl=https://127.0.0.1:$IAM_JWKS_PORT" \
    --env-var "iamRegistryTokenBaseUrl=https://127.0.0.1:$IAM_REGTOKEN_PORT" \
    "${OWN_FRONT_ENV_ARGS[@]}" \
    ${OWN_FRONT_TLS_ARGS[@]+"${OWN_FRONT_TLS_ARGS[@]}"} \
    "${INTERNAL_REST_NEWMAN_ARGS[@]}" \
    "${OPT_ENV_ARGS[@]}"
  RAW_RC=$?
  set -e

  # ── same two-verdict discipline as newman-parallel.sh ──────────────────────
  # RAW = run.sh's own verdict (every failed assertion / non-zero newman rc).
  # GATED = the verdict CI grades with (services/iam/tests/newman/scripts/
  # assert-suites-green.sh), per collection.
  #
  # These two used to differ BY CONSTRUCTION, because the gate deducted a set of
  # cases before deciding and the gap between the blocks was the size of the
  # deduction. That deduction was removed 2026-07-30 (see the gate's own note on
  # why narrowing it could never work), so nothing is subtracted from the verdict
  # any more and nothing is filtered out of the request count either: a request
  # that got no answer is reported as UNANSWERED and fires the gate.
  #
  # RAW therefore stays printed for a different reason than before: it is the
  # roll-up you can read at a glance, and if the two blocks ever disagree, THAT is
  # the finding — something has started subtracting again.
  GATE="${GATE:-true}"
  GATE_SCRIPT="$REPO_ROOT/tests/newman/scripts/assert-suites-green.sh"
  echo
  echo "[e2e] RAW verdict (run.sh): rc=$RAW_RC — see out/summary.txt"
  # Три исхода, а не два — см. разбор в newman-parallel.sh рядом с тем же
  # условием. Коротко: «выключено ручкой» есть решение оператора, «файла нет» —
  # НЕ ВЫПОЛНИЛОСЬ. Прежняя редакция сливала их в одну ветвь, а её сообщение не
  # называло даже пути, поэтому две причины были неразличимы для читателя
  # журнала вовсе.
  if [ "$GATE" != "true" ]; then
    echo "[e2e] CI gate switched OFF by the knob (GATE=$GATE; script=$GATE_SCRIPT) — grading on RAW"
    exit "$RAW_RC"
  fi
  if [ ! -f "$GATE_SCRIPT" ]; then
    echo "[e2e] DID NOT RUN: the verdict gate is not at $GATE_SCRIPT." >&2
    echo "      This is NOT a skip and NOT a verdict: RAW is a bare count, without the" >&2
    echo "      three-outcome split, the executed-vs-declared census or the refusal on a mute report." >&2
    echo "      Fix the path or switch the gate off deliberately with GATE=false." >&2
    # Тот же код, что у newman-parallel.sh: «не выполнилось» обязано быть ОДНИМ
    # кодом на оба прогонщика, иначе читателю придётся помнить, какой из них он
    # запустил, чтобы истолковать число.
    exit 2
  fi
  echo "[e2e] GATED verdict (the gate CI runs):"
  set +e
  bash "$GATE_SCRIPT"
  GATE_RC=$?
  set -e
  [ "$GATE_RC" -eq 0 ] && [ "$RAW_RC" -ne 0 ] && \
    echo "[e2e] INCONSISTENT: the gate is green while run.sh is red, and nothing is deducted from the verdict any more — read the raw failures, do not trust this GREEN."
  exit "$GATE_RC"
fi
