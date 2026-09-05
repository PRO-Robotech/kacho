#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# newman-parallel.sh — прогнать набор newman-суит против поднятого стенда.
#
# ИМЯ ОСТАЛОСЬ, ПРЕДМЕТ ИЗМЕНИЛСЯ (2026-08-05). Скрипт назывался «parallel»,
# потому что раскладывал восемь суит по одному стенду одновременно (JOBS=2 внутри
# каждой ⇒ до шестнадцати коллекций сразу). Решением владельца конкуренция снята:
# JOBS=1 и WAVE_JOBS=1, то есть стенд в каждый момент обслуживает ОДНУ коллекцию.
# Имя файла не переименовано намеренно — на него ссылаются workflow, Makefile и
# полдюжины гейтов; переименование дало бы ровно ту порцию ложных координат, за
# которую мы ругаем документацию. Что скрипт делает сегодня, написано здесь.
#
# ПРОПУСКНУЮ СПОСОБНОСТЬ ТЕПЕРЬ ДАЁТ РАЗНЕСЕНИЕ ПО РАННЕРАМ, а не конкуренция
# внутри одного: deploy/e2e-shards.json задаёт, какой шард какие суиты гоняет и
# какие компоненты поднимает. Один шард = один kind-кластер = свои 4 ядра.
# Прежнее обоснование конкуренции («суиты независимы, стенд один») отменено не
# спором, а замером: на 4 ядрах восемь суит исчерпывали ёмкость (см. JOBS ниже).
#
# Flow (idempotent, deterministic — same as newman-e2e.sh, once for all four):
#   1. port-forward api-gateway public(:18080)+internal(:18081) + iam-internal(:19091)
#   2. seed auth fixtures ONCE via tests/authz-fixtures/setup.sh (per-service
#      isolated accounts/projects) + patch every service env
#   3. seed the nlb external-VIP AddressPool (only nlb needs it)
#   4. regenerate every suite's collections (gen.py)
#   5. run all four suites in parallel (each = its own scripts/run.sh, which itself
#      fans its collections out with --jobs); per-suite logs to out/<svc>-suite.log
#   6. aggregate TWICE and print BOTH:
#        PER-SUITE TOTALS — failed assertions / unanswered requests summed per suite
#        GATED            — the SAME gate CI runs (services/iam/tests/newman/scripts/
#                           assert-suites-green.sh), per collection
#      exit code = GATED, so a local run and CI agree on the verdict.
#
#      These two blocks used to be labelled RAW and GATED because the gate DEDUCTED a
#      "known-RED" set before deciding, and the gap between them was the size of the
#      deduction. The deduction was removed 2026-07-30 (see the gate's own note), so
#      the two now agree by construction and the first block is kept for a different
#      reason: it is the per-suite roll-up, which the per-collection gate output does
#      not give you at a glance. If they ever disagree again, something has started
#      subtracting — that is the finding.
#      Nothing is subtracted from the REQUEST count either: a request that got no
#      answer is reported as UNANSWERED and fires the gate. It used to be filtered away
#      as "DNS noise", which is how eight ban-#6 negatives went unexecuted while both
#      verdicts read green.
#
# Usage (after `make dev-up`):
#   ./scripts/newman-parallel.sh                 # все восемь суит, по одной
#   SERVICES="vpc nlb" ./scripts/newman-parallel.sh   # состав шарда
#   DELAY=3 ./scripts/newman-parallel.sh              # темп запросов
# JOBS/WAVE_JOBS существуют, но их умолчание — 1; поднимать их значит вернуть
# снятую конкуренцию, и это должно быть решением с числом (см. ниже).
set -uo pipefail

# storage + registry added: their redesign was NOT under e2e coverage (artifact
# lacked them). Their suite fixtures (isolated account+projects+grant, default-Bearer
# prelude) are now seeded by the shared authz-fixtures.
# geo has its OWN full 42-case suite (Region/Zone public + Internal admin :9091 + authz
# + placement) — owner directive "every module has its own complete tests". geo:dev
# verified locally to boot clean (authMode=dev, mTLS creds mounted) + serve; back in the
# active run.
# api-gateway added 2026-07-26: gateway/tests/newman existed but had never been
# executed — no run.sh, no environment file, and it was absent from this list, while
# suite_dir() below already carried a branch for it. It owns the cluster-RBAC admin
# surface (InternalClusterService on the cluster-internal REST listener), which no
# per-service suite covers; services/iam/tests/newman even cites it as the covering
# location for a case it removed. A suite that exists but never runs is the state
# that let it rot, so it is either wired or deleted — this is the wiring.
SERVICES="${SERVICES:-iam vpc compute nlb storage registry geo api-gateway}"
NS="${SETUP_NS:-kacho}"
GW_PORT="${GW_PORT:-18080}"
GW_INTERNAL_PORT="${GW_INTERNAL_PORT:-18081}"
# The api-gateway's EXTERNAL TLS listener (:8443, advertised as api.kacho.local:443).
# The ban-#6 negatives in the iam suite assert that Internal* RPCs are not reachable
# there. They used to address the advertised HOSTNAME, which does not resolve on a
# developer box (and adding it needs root), so for an unknown length of time those
# eight checks never ran while the gate subtracted their transport errors and printed
# "0 failed requests". Ban #6 is a property of the LISTENER, not of the DNS name used
# to find it — so it is forwarded here like the other two.
GW_TLS_PORT="${GW_TLS_PORT:-18443}"
IAM_INTERNAL_PORT="${IAM_INTERNAL_PORT:-19091}"
HYDRA_PORT="${HYDRA_PUBLIC_PORT:-14444}"   # OAuth2 token endpoint (production-posture seed)
# Адреса ПОЛОСЫ ФАСАДА (#59). Это не api-gateway: кейсы IBT-* обязаны спросить сами
# слушатели, иначе «токен проверяется через фасад» останется утверждением о конфиге,
# а не о поведении. Оба адресата — ЯДРО (iam), то есть есть на каждом стенде.
IAM_JWKS_PORT="${IAM_JWKS_PORT:-19097}"         # iam JWKS-proxy :9097 (server-TLS)
IAM_REGTOKEN_PORT="${IAM_REGTOKEN_PORT:-19096}" # iam docker-token handle :9096 (server-TLS)
# Адрес data plane реестра здесь БОЛЬШЕ НЕ ОБЪЯВЛЯЕТСЯ: его адресат — компонент,
# а не ядро, и порт вместе с портовой ручкой живёт в deploy/e2e-shards.json
# (`optional_transports`), откуда его читает цикл ниже. Объявлять его и здесь
# значило бы завести два места об одном предмете с разными умолчаниями.
# Transports of WAVE 4 (the ceremony). The wave turns itself on from a fact about the
# tree — the ceremony seed exists — and the seed dials the identity provider and the
# token issuer directly, because neither is routed through the gateway. Those dials had
# no producer here: the runner opened five forwards and the seed needed four addresses
# that were not among them. It worked only while a human held the forwards by hand, so
# on a clean machine the wave could not pass ONCE, while everything about it — the
# auto-enable, the log line, the derived collection set — read as working. Same class as
# GW_TLS_PORT above: a probe whose transport nobody creates is a probe that never ran.
# The addresses are passed to the wave explicitly (below) rather than left to the seed's
# defaults, so the port that is opened and the port that is dialled cannot drift apart.
KRATOS_PUBLIC_PORT="${KRATOS_PUBLIC_PORT:-24433}"  # native login flow (password is checked HERE)
KRATOS_ADMIN_PORT="${KRATOS_ADMIN_PORT:-24434}"    # identity create/lookup for the ceremony human
HYDRA_ADMIN_PORT="${HYDRA_ADMIN_PORT:-24445}"      # login-request accept (TLS listener)
DELAY="${DELAY:-3}"          # per-request delay (ms) inside each collection
# ПАРАЛЛЕЛЬНОСТЬ ВНУТРИ ПРОГОНА СНЯТА (решение владельца 2026-08-05).
#
# JOBS — сколько коллекций суиты гонятся одновременно; WAVE_JOBS — сколько СУИТ
# одновременно внутри волны. Оба теперь 1, то есть в каждый момент стенд обслуживает
# ровно одну коллекцию.
#
# Вред от конкуренции был известен ЗАДОЛГО до этого решения и записан прямо здесь же
# (см. `sjobs` ниже): у nlb --jobs>1 исчерпывает общий пул внешних адресов, у iam и
# registry обгоняет дренаж материализации. То есть три суиты из восьми уже стояли на
# принудительной единице, а «параллельно» оставалось у остальных пяти по умолчанию —
# не по замеру, а потому что так завели. Замер, который решил вопрос (04-05.08):
# окно материализации локально (12 ядер) p50 2.0с / p90 5.0с, петель ≥10с ноль;
# в CI (4 ядра) p50 6.0с / p90 11.0с, петель ≥10с — 52; плюс 5 отказов вида
# «authorization backend unavailable» и 288 наблюдений ожидания блокировки
# (среднее 0.72с, максимум 4.6с) — это исчерпание ёмкости, а не дефект логики.
#
# Пропускную способность теперь даёт РАЗНЕСЕНИЕ ПО РАННЕРАМ (deploy/e2e-shards.json):
# каждый шард — свой kind-кластер со своим набором компонентов, свои 4 ядра.
# Восстанавливать конкуренцию внутри шарда значит вернуть ровно то, что разнесение
# и убирает.
JOBS="${JOBS:-1}"            # коллекций одновременно внутри суиты
WAVE_JOBS="${WAVE_JOBS:-1}"  # суит одновременно внутри волны
SEED="${SEED:-true}"         # set false to reuse an already-seeded stand

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

for tool in kubectl python3 newman grpcurl jq; do
  command -v "$tool" >/dev/null 2>&1 || { echo "FATAL: '$tool' not found in PATH" >&2; exit 1; }
done

suite_dir() { # <svc>
  if [ "$1" = "api-gateway" ]; then echo "$REPO_ROOT/gateway/tests/newman"; else echo "$REPO_ROOT/services/$1/tests/newman"; fi
}

PF_PIDS=()
PF_WHAT=()   # «порт|назначение» в том же порядке, что PF_PIDS — для проверки ниже
TMP_DIRS=()
cleanup() {
  for p in "${PF_PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
  for d in "${TMP_DIRS[@]:-}"; do [ -n "$d" ] && rm -rf "$d"; done
}
trap cleanup EXIT

echo "[parallel] port-forward api-gateway :$GW_PORT/:$GW_INTERNAL_PORT/:$GW_TLS_PORT + iam-internal :$IAM_INTERNAL_PORT + hydra :$HYDRA_PORT + ceremony (kratos :$KRATOS_PUBLIC_PORT/:$KRATOS_ADMIN_PORT, hydra-admin :$HYDRA_ADMIN_PORT)"
kubectl -n "$NS" port-forward svc/api-gateway "$GW_PORT:8080" >/tmp/e2e-pp-gw.log 2>&1 &            PF_PIDS+=($!); PF_WHAT+=("$GW_PORT|api-gateway public (:8080)|/tmp/e2e-pp-gw.log")
kubectl -n "$NS" port-forward svc/api-gateway "$GW_INTERNAL_PORT:8081" >/tmp/e2e-pp-gwint.log 2>&1 & PF_PIDS+=($!); PF_WHAT+=("$GW_INTERNAL_PORT|api-gateway internal (:8081)|/tmp/e2e-pp-gwint.log")
kubectl -n "$NS" port-forward svc/api-gateway "$GW_TLS_PORT:8443" >/tmp/e2e-pp-gwtls.log 2>&1 &     PF_PIDS+=($!); PF_WHAT+=("$GW_TLS_PORT|api-gateway external TLS (:8443)|/tmp/e2e-pp-gwtls.log")
kubectl -n "$NS" port-forward svc/kaname-internal "$IAM_INTERNAL_PORT:9091" >/tmp/e2e-pp-iam.log 2>&1 & PF_PIDS+=($!); PF_WHAT+=("$IAM_INTERNAL_PORT|iam internal gRPC (:9091)|/tmp/e2e-pp-iam.log")
# Hydra public — the POST target of the OAuth2 client_credentials exchange that turns an
# iam-issued SA key into the RS256 Bearer a production-posture stand accepts. ClusterIP
# with no ingress route here, so the exchange needs this forward. Harmless in dev (the
# seed never dials it); required in production, and setting it HERE means the seed does
# not have to open one per invocation.
kubectl -n "$NS" port-forward svc/kacho-umbrella-hydra-public "$HYDRA_PORT:4444" >/tmp/e2e-pp-hydra.log 2>&1 & PF_PIDS+=($!); PF_WHAT+=("$HYDRA_PORT|hydra public token endpoint (:4444)|/tmp/e2e-pp-hydra.log")
# Ceremony transports (WAVE 4). Opened unconditionally, next to the other five, and torn
# down by the same trap. Deliberately NOT guarded by "skip the wave if the service is
# absent": a missing transport must surface as the ceremony refusing to seed — which
# leaves no reports, so assert-suites-green.sh reports every collection of the wave as
# (no-report) and the run is RED. "Could not reach it" is a finding, not a pass.
# hydra-public is not re-forwarded: the exchange forward above already serves that
# address, and one service reachable at two ports is two facts that can disagree.
kubectl -n "$NS" port-forward svc/kacho-umbrella-kratos-public "$KRATOS_PUBLIC_PORT:80" >/tmp/e2e-pp-kratos-pub.log 2>&1 &  PF_PIDS+=($!); PF_WHAT+=("$KRATOS_PUBLIC_PORT|kratos public (:80)|/tmp/e2e-pp-kratos-pub.log")
kubectl -n "$NS" port-forward svc/kacho-umbrella-kratos-admin "$KRATOS_ADMIN_PORT:80" >/tmp/e2e-pp-kratos-adm.log 2>&1 &    PF_PIDS+=($!); PF_WHAT+=("$KRATOS_ADMIN_PORT|kratos admin (:80)|/tmp/e2e-pp-kratos-adm.log")
kubectl -n "$NS" port-forward svc/kacho-umbrella-hydra-admin-tls "$HYDRA_ADMIN_PORT:4445" >/tmp/e2e-pp-hydra-adm.log 2>&1 & PF_PIDS+=($!); PF_WHAT+=("$HYDRA_ADMIN_PORT|hydra admin TLS (:4445)|/tmp/e2e-pp-hydra-adm.log")
# Полоса фасада (#59). Каждый проброс попадает в PF_WHAT, поэтому не вставший
# проброс останавливает прогон тем же блоком ниже, а не отдаёт «кейс не смог».
kubectl -n "$NS" port-forward svc/kaname-internal "$IAM_JWKS_PORT:9097" >/tmp/e2e-pp-iam-jwks.log 2>&1 & PF_PIDS+=($!); PF_WHAT+=("$IAM_JWKS_PORT|iam JWKS-proxy (:9097)|/tmp/e2e-pp-iam-jwks.log")
kubectl -n "$NS" port-forward svc/kaname "$IAM_REGTOKEN_PORT:9096" >/tmp/e2e-pp-iam-regtoken.log 2>&1 & PF_PIDS+=($!); PF_WHAT+=("$IAM_REGTOKEN_PORT|iam docker-token handle (:9096)|/tmp/e2e-pp-iam-regtoken.log")

# ─── ПРОБРОСЫ К КОМПОНЕНТАМ — ОТКРЫВАЮТСЯ ПО СПРОСУ, А НЕ ВСЕГДА ─────────────
#
# Все пробросы выше ведут к ЯДРУ: api-gateway, iam, провайдер подписи, церемония —
# это есть на каждом стенде. Проброс к КОМПОНЕНТУ (реестр, и далее по списку
# `optional_transports` манифеста) вести себя так не может: шард поднимает не все
# компоненты, а `kubectl port-forward svc/<нет такого>` не встаёт и ЗАВЕРШАЕТСЯ —
# после чего блок ниже совершенно правильно объявляет прогон недействительным.
#
# ИМЕННО ТАК ЧЕТЫРЕ ШАРДА ИЗ ПЯТИ НЕ ЗАПУСТИЛИ НИ ОДНОЙ СУИТЫ (прогон 31344367968,
# по 0 из 16 коллекций у каждого): проброс к data plane реестра открывался
# безусловно, а реестра на тех стендах нет и быть не должно. Ни одна коллекция тех
# суит его при этом не набирала — то есть прогон погиб об адрес, который никому из
# исполняемого не был нужен.
#
# ПОЧЕМУ ЭТО НЕ ПОСЛАБЛЕНИЕ. Спрос ВЫВОДИТСЯ ИЗ ДЕРЕВА (кто из коллекций и
# case-файлов запускаемых суит упоминает переменную), а не выписывается; и если
# спрос есть — проброс ОБЯЗАТЕЛЕН на прежних условиях: не встал ⇒ прогон
# недействителен тем же блоком ниже. Полоса, которую никто не набирает, не имеет
# предмета; полоса, которую набирают, обязана стоять. Парность «набирает ⇒
# компонент поднят» держит assert-shard-coverage.py (п.9), поэтому суита не может
# уехать на стенд без своего адресата молча.
#
# Перепись печатается всегда: «опциональных транспортов не нужно» обязано быть
# отличимо от «ни одного файла не прочитано».
OPT_ENV_ARR=()     # --env-var ... для запуска суиты (массив)
OPT_ENV_ARGS=""    # то же строкой — для волн, принимающих EXTRA_NEWMAN_ARGS
while IFS='|' read -r _ovar _osvc _otport _oportenv _odport _oscheme _owhy; do
  [ -n "${_ovar:-}" ] || continue
  _oport="${!_oportenv:-$_odport}"
  kubectl -n "$NS" port-forward "svc/$_osvc" "$_oport:$_otport" >"/tmp/e2e-pp-opt-$_ovar.log" 2>&1 &
  PF_PIDS+=($!); PF_WHAT+=("$_oport|$_owhy|/tmp/e2e-pp-opt-$_ovar.log")
  OPT_ENV_ARR+=(--env-var "$_ovar=$_oscheme://localhost:$_oport")
  OPT_ENV_ARGS="$OPT_ENV_ARGS --env-var $_ovar=$_oscheme://localhost:$_oport"
  echo "[parallel] транспорт компонента: $_ovar → svc/$_osvc :$_otport на localhost:$_oport ($_owhy)"
done < <(python3 "$SCRIPT_DIR/e2e-optional-transports.py" --suites "$SERVICES" --census)
sleep 4

# ПРОБРОС, КОТОРЫЙ НЕ ВСТАЛ, ОСТАНАВЛИВАЕТ ПРОГОН ЗДЕСЬ.
#
# Пробросы открывались, и НИ ОДИН не проверялся. `kubectl port-forward` на
# занятом порту печатает отказ в свой лог-файл, который никто не читает, и ВЫХОДИТ;
# скрипт спал четыре секунды и шёл дальше. Дальше — два исхода, и худший из них тихий:
#
#   • порт свободен у всех — посев умирает внутри, за сотни строк отсюда, сообщением
#     про чужой предмет («Failed to dial … EOF», «Connection refused»), и читатель
#     идёт чинить продукт вместо порта;
#   • порт занят ЧУЖИМ пробросом — а на общей машине это норма (параллельные волны,
#     забытая сессия), — и тогда весь прогон молча идёт через сокет, которым мы не
#     управляем. Проверка посадки края при этом ПРОХОДИТ: чужой проброс отвечает.
#     Это не «стенд не готов», это вердикт о чужом стенде, выданный за свой.
#
# Оба наблюдались на этой машине 2026-08-05 (см. /tmp/e2e-pp-*.log первого прогона:
# «bind: address already in use» на :19091, а :18080/:18081/:18443 в тот же момент
# держала чужая сессия восемнадцатичасовой давности).
#
# Предикат — ЖИВ ЛИ НАШ ПРОЦЕСС, а не «занят ли порт»: занятость порта как раз и не
# отличает наш проброс от чужого, а `kubectl` при отказе привязки завершается. Значит
# живой процесс — это ровно «producer, которого мы завели, существует».
pf_dead=()
for _i in "${!PF_PIDS[@]}"; do
  kill -0 "${PF_PIDS[$_i]}" 2>/dev/null || pf_dead+=("${PF_WHAT[$_i]}")
done
if [ "${#pf_dead[@]}" -gt 0 ]; then
  echo
  echo "===== ПРОГОН НЕДЕЙСТВИТЕЛЕН: проброс не встал (${#pf_dead[@]} из ${#PF_PIDS[@]}) ====="
  # Причина берётся из ЖУРНАЛА ЭТОГО проброса, а не поиском по всем сразу: общий
  # поиск приписывал одному порту сообщение другого, то есть сам врал ровно тем
  # способом, против которого этот блок и написан.
  for _d in "${pf_dead[@]}"; do
    _port="${_d%%|*}"; _rest="${_d#*|}"; _why="${_rest%%|*}"; _log="${_rest#*|}"
    echo "  :$_port — $_why"
    # -a: журнал предыдущего прогона мог остаться с двоичным мусором, и без него
    # grep отвечает «binary file matches» ВМЕСТО причины — диагностика, потерянная
    # ровно там, где она нужна.
    # Значение берётся отдельно, а статус конвейера ОТБРАСЫВАЕТСЯ: `grep -m1`
    # выходит по первой строке, `tr` получает SIGPIPE, и под `pipefail` непустой
    # журнал объявлялся пустым (задача #658). Решает теперь ПУСТОТА значения.
    _first="$(tr -d '\r' <"$_log" 2>/dev/null | grep -a -m1 . || true)"
    echo "      ${_first:-журнал пуст: $_log}"
  done
  echo
  echo "Суиты НЕ запускались. Это НЕ красный прогон и НЕ зелёный — результата нет."
  echo "Чаще всего порт занят другим прогоном или забытой сессией (\`ss -ltnp\`)."
  echo "Порты переносятся ручками: GW_PORT / GW_INTERNAL_PORT / GW_TLS_PORT /"
  echo "IAM_INTERNAL_PORT / HYDRA_PUBLIC_PORT / KRATOS_PUBLIC_PORT / KRATOS_ADMIN_PORT /"
  echo "IAM_JWKS_PORT / IAM_REGTOKEN_PORT / REGISTRY_DATAPLANE_PORT /"
  echo "HYDRA_ADMIN_PORT — набираемые адреса следуют за ними (см. передачу в посев ниже)."
  exit 2
fi

# Client certificates for the cluster-internal listeners. iam :9091 demands a verified
# client cert in EVERY posture (security.md — internal is not exempt), and the two
# identities are NOT interchangeable:
#   api-gateway-client-tls  → the ordinary internal RPCs (UpsertFromIdentity, LookupSubject)
#   kacho-bootstrap-operator-client-tls → the ONLY SAN iam allow-lists for
#     MintBootstrapToken, the production seed's entry point. Pointing the mint at the
#     gateway identity is precisely the "fix" the #58 hardening exists to prevent, so the
#     two are exported separately and never aliased.
MTLS_ENV=()
if kubectl -n "$NS" get secret api-gateway-client-tls >/dev/null 2>&1; then
  CERT_DIR="$(mktemp -d)"; TMP_DIRS+=("$CERT_DIR")
  kubectl -n "$NS" get secret api-gateway-client-tls -o jsonpath='{.data.tls\.crt}' | base64 -d > "$CERT_DIR/client.crt"
  kubectl -n "$NS" get secret api-gateway-client-tls -o jsonpath='{.data.tls\.key}' | base64 -d > "$CERT_DIR/client.key"
  chmod 600 "$CERT_DIR"/*
  MTLS_ENV=(IAM_INTERNAL_GRPC_MTLS_CERT="$CERT_DIR/client.crt" IAM_INTERNAL_GRPC_MTLS_KEY="$CERT_DIR/client.key")
fi
if kubectl -n "$NS" get secret kacho-bootstrap-operator-client-tls >/dev/null 2>&1; then
  OP_DIR="$(mktemp -d)"; TMP_DIRS+=("$OP_DIR")
  kubectl -n "$NS" get secret kacho-bootstrap-operator-client-tls -o jsonpath='{.data.tls\.crt}' | base64 -d > "$OP_DIR/client.crt"
  kubectl -n "$NS" get secret kacho-bootstrap-operator-client-tls -o jsonpath='{.data.tls\.key}' | base64 -d > "$OP_DIR/client.key"
  chmod 600 "$OP_DIR"/*
  MTLS_ENV+=(BOOTSTRAP_MINT_MTLS_CERT="$OP_DIR/client.crt" BOOTSTRAP_MINT_MTLS_KEY="$OP_DIR/client.key")
fi

if [ "$SEED" = "true" ]; then
  echo "[parallel] seeding auth fixtures (per-service isolated) + patching envs"
  # INTERNAL_BASE_URL (:8081 internal mux, already port-forwarded above) lets the seed
  # reach the Internal admin catalog via the :18081 mux. Производитель посева geo назван
  # точно: setup.sh — точка входа, а регион и зоны заводит его делегат
  # tests/authz-fixtures/prodseed_matrix.py, функция _seed_geo_catalog. Без этой
  # переменной каталог greenfield-стенда остаётся пуст → каждое создание зоны/региона
  # падает. На стенде, поднятом `make dev-up`, тот же базовый каталог уже посеян целью
  # `make seed-geo`, и делегат становится подтверждённым no-op — но прогон не вправе
  # ЗАВИСЕТЬ от того, чем поднимали стенд, поэтому посев остаётся.
  # ЗДЕСЬ ПЕРЕДАВАЛСЯ HYDRA_TOKEN_URL — адрес обмена у прежнего издателя. Читателя у
  # него не осталось (задача #1120): ключ служебной учётки зеркала у поставщика не
  # заводит и обменивается только у нашего издателя (PLATFORM_TOKEN_URL ниже).
  # Переменная, которую никто не читает, читается следующим как действующая полоса,
  # поэтому снята вместе с ней. HYDRA_PUBLIC_PORT остаётся: проброс нужен другим
  # потребителям (проверка достижимости в prodseed_all.py, providerPublicBaseUrl суит).
  # ПОСЕВ ШИРЕ, ЧЕМ НАБОР СУИТ, И ЭТО НЕ ОПЛОШНОСТЬ.
  #
  # Что посеять — вопрос про СТЕНД, а не про то, чьи кейсы мы сегодня гоняем.
  # Показательный случай: коллекция `label-revoke-nlb` живёт в суите iam, а
  # создаёт балансировщик с автоматическим внешним адресом — то есть требует
  # пула, который сеет посев nlb. При разнесении по раннерам шард iam поднимает
  # nlb (иначе эта коллекция не выполнима), но суиту nlb не гоняет; посев,
  # выведенный из списка суит, пул бы не завёл, и коллекция покраснела бы
  # утверждением про продукт вместо «фикстуры нет».
  #
  # Поэтому список для посева = список суит ПЛЮС домены, чьи сервисы реально
  # подняты на этом стенде. Предикат — факт о кластере (`kubectl get deploy`), а
  # не объявление вызывающего: объявление и стенд разъезжаются молча.
  SEED_SERVICES="$SERVICES"
  for _pair in "nlb:kacho-nlb" "storage:kacho-storage" "compute:compute" "vpc:vpc" "registry:registry"; do
    _svc="${_pair%%:*}"; _dep="${_pair##*:}"
    case " $SEED_SERVICES " in *" $_svc "*) continue ;; esac
    if kubectl -n "$NS" get deploy "$_dep" >/dev/null 2>&1; then
      SEED_SERVICES="$SEED_SERVICES $_svc"
    fi
  done
  if [ "$SEED_SERVICES" != "$SERVICES" ]; then
    echo "[parallel] посев расширен по факту о стенде: суиты «$SERVICES» → посев «$SEED_SERVICES»"
  fi

  env BASE_URL="http://localhost:$GW_PORT" INTERNAL_BASE_URL="http://localhost:$GW_INTERNAL_PORT" \
      IAM_INTERNAL_GRPC="localhost:$IAM_INTERNAL_PORT" HYDRA_PUBLIC_PORT="$HYDRA_PORT" \
      PLATFORM_TOKEN_URL="https://127.0.0.1:$IAM_REGTOKEN_PORT/iam/v1/token" \
      SERVICES="$SEED_SERVICES" \
      PATCH_ENV=true SETUP_NS="$NS" "${MTLS_ENV[@]}" \
      bash "$REPO_ROOT/tests/authz-fixtures/setup.sh"
  SEED_RC=$?

  # A FAILED SEED ABORTS THE RUN. It is not a red suite — it is not a result at all.
  #
  # This script runs `set -uo pipefail` WITHOUT `-e`, and the seed's exit code used to
  # be dropped on the floor. So a seed that died on its first step went straight into
  # WAVE 1, every suite ran against a stand with placeholder credentials, and the
  # verdict reported the wholesale 401/403 as FAILED ASSERTIONS — i.e. an unexecuted
  # run wearing the clothes of a product regression. Whoever read that verdict would
  # triage the product for a defect that lives in the harness.
  #
  # Observed 2026-07-30 on the first production-posture run: the seed could not mint
  # its first token (a request field had been tombstoned in proto and this caller was
  # never updated), printed a Python traceback, and the runner announced
  # "WAVE 1 concurrent" on the very next line.
  #
  # Aborting is the only defensible outcome: "нет посева" and "продукт сломан" are
  # different findings, and only the second is evidence (testing.md — «не выполнилось»
  # никогда не вычитается из вердикта и не засчитывается в «прошло»).
  if [ "$SEED_RC" -ne 0 ]; then
    echo
    echo "===== ПРОГОН НЕДЕЙСТВИТЕЛЕН: посев фикстур упал (rc=$SEED_RC) ====="
    # rc=3 у посева — отдельный исход: посадку края НЕ УДАЛОСЬ УСТАНОВИТЬ. Он назван
    # здесь, а не свёрнут в общий отказ, потому что чинить в этих двух случаях надо
    # разное: «край расслаблен» — перекатить стенд, «свидетельства нет» — вернуть
    # достижимость края (порт-форвард/адрес). Прежний классификатор эти два случая
    # как раз и путал, и оператор шёл не туда.
    if [ "$SEED_RC" -eq 3 ]; then
      echo "Посадка края НЕ УСТАНОВЛЕНА — про стенд не утверждается ничего; это не"
      echo "«стенд расслаблен», а «свидетельства не получено» (проверь достижимость"
      echo "края по BASE_URL — порт-форвард живёт ровно столько, сколько этот скрипт)."
    fi
    echo "Суиты НЕ запускались. Это НЕ красный прогон и НЕ зелёный — результата нет."
    echo "Причина — выше, в выводе tests/authz-fixtures/setup.sh (в боевой посадке он"
    echo "делегирует prodseed_all.py). Починить посев и ПОВТОРИТЬ; разбирать суиты"
    echo "против непосеянного стенда нечего."
    exit 2
  fi

  # nlb's external-VIP AddressPool is seeded by setup.sh, and ONLY there.
  #
  # A second pass used to stand here, guarded by "unless the seed ran in production
  # posture". That guard could never be false: the seed's classifier leaves `production`
  # as the only value standing, and a seed that does not reach that point exits this script
  # above (rc≠0 ⇒ "ПРОГОН НЕДЕЙСТВИТЕЛЕН") before the read. So the block was unreachable
  # from the day the classifier was narrowed — and its fallback named `dev`, the one
  # posture the classifier REFUSES.
  #
  # Nothing is lost by removing it: setup.sh delegates to prodseed_all.py, which drives
  # deploy/scripts/seed-nlb-fixtures.sh itself and is the sole author of that
  # cluster-wide default-pool slot, pinned to the nlb-dedicated zone. A second author is
  # what the guard was trying to prevent — unconditionally now, by not existing.
  #
  # deploy/scripts/assert-posture-branches-can-be-taken.py keeps this shape from coming
  # back: a branch on the seed posture must be able to go both ways.
  :
fi

echo "[parallel] regenerating collections for: $SERVICES"
for svc in $SERVICES; do ( cd "$(suite_dir "$svc")" && python3 scripts/gen.py >/dev/null ) || { echo "gen $svc FAILED" >&2; exit 1; }; done

# Two-wave scheduler. PHASE2_SERVICES (default: iam) run in a SEPARATE second wave,
# NOT concurrent with the rest. Rationale: iam's OWN authz materialization (AccessBinding
# CRUD, label-revoke delete-stale) is full-path EXCLUSIVE-lock serialized; under the peak
# concurrent load of vpc+compute+nlb registering resources it drains (get-confirms 404,
# post-revoke {allowed:true}, cross-service NOB grant-window contamination). Isolating iam
# to its own wave gives its full-path room to materialize with no competing load. The
# leaf-resource services keep the forward SHARE-lock fast-path in their concurrent wave.
# wall-time = dev-up + max(wave1) + wave2(iam ~serial) instead of max(all).
PHASE2="${PHASE2_SERVICES:-iam}"
_in_phase2() { case " $PHASE2 " in *" $1 "*) return 0 ;; *) return 1 ;; esac; }

RC=0
declare -A SUITE_PID
WAVE_RUNNING=()   # pid'ы ТОЛЬКО запущенных суит (пробросы портов сюда не попадают)

# _wave_wait_below <n> — ждать, пока живых суит не станет меньше n.
# Опрос `kill -0` вместо `wait -n`: `wait -n` без списка pid'ов ждёт ЛЮБОГО потомка,
# включая вечные пробросы портов, а со списком он доступен не во всех версиях bash,
# на которых этот скрипт гоняют. Опрос дешевле любой из этих зависимостей.
_wave_wait_below() {
  local want="$1" p alive
  while :; do
    alive=()
    for p in "${WAVE_RUNNING[@]:-}"; do
      [ -n "$p" ] && kill -0 "$p" 2>/dev/null && alive+=("$p")
    done
    WAVE_RUNNING=("${alive[@]:-}")
    [ "${#alive[@]}" -lt "$want" ] && return 0
    sleep 1
  done
}

launch_wave() {  # $@ = суиты волны; одновременно исполняется не более WAVE_JOBS
  SUITE_PID=()
  WAVE_RUNNING=()
  local svc d sjobs st
  for svc in "$@"; do
    # Ограничитель суит. При WAVE_JOBS=1 (умолчание) это делает волну строго
    # последовательной, и «волна» из имени становится просто порядком, а не
    # одновременностью. Ограничитель оставлен ручкой, а не выпилен: разнесение по
    # раннерам обычно даёт шарду одну-две суиты, и восстановить конкуренцию должно
    # быть решением с числом, а не правкой кода.
    #
    # СЧИТАЮТСЯ ТОЛЬКО СУИТЫ, И ЭТО НЕ ПРИДИРКА. У этого скрипта в фоне живут
    # пробросы портов, которые не завершаются никогда — они живут ровно столько,
    # сколько прогон. Поэтому ни `jobs -rp | wc -l` (считает и их), ни голый
    # `wait`/`wait -n` (ждёт и их) здесь не годятся: первая же итерация ограничителя
    # встала бы навсегда, и прогон умер бы по таймауту шага, не отчитавшись ни одной
    # коллекцией — то есть выглядел бы как «все суиты без отчёта», а не как зависший
    # планировщик. Ждём поимённо своих потомков и никого больше.
    _wave_wait_below "$WAVE_JOBS"
    d="$(suite_dir "$svc")"
    # nlb EXTERNAL suites draw auto-VIPs from ONE shared external AddressPool — --jobs>1
    # transiently exhausts it → phantom (see nlb run.sh header). Force nlb serial.
    # nlb: shared external-VIP pool (--jobs>1 exhausts it). iam+registry: materialization-
    # heavy (every AccessBinding / registry+repo Create → owner-tuple via fga_register_outbox
    # → drainer). Under --jobs>1 the concurrent create rate outruns the
    # drainer → owner-tuple materialization backlog → create→Get/Delete 403/404 read-your-
    # writes past even a 48s client retry (throughput inversion, EC-lag not correctness — the
    # emission is verified). Serialising these suites keeps the drainer caught up so the
    # bounded client-retries cover the window. Correctness is validated at this concurrency;
    # prod-scale materialization throughput is the separate tracked scalability epic.
    sjobs="$JOBS"; case "$svc" in nlb|iam|registry) sjobs=1 ;; esac
    mkdir -p "$d/out"   # redirect below opens out/suite.log BEFORE run.sh's own mkdir
    ( cd "$d" && ./scripts/run.sh --service "" --delay "$DELAY" --jobs "$sjobs" \
        --env-var "baseUrl=http://localhost:$GW_PORT" \
        --env-var "internalBaseUrl=http://localhost:$GW_INTERNAL_PORT" \
        --env-var "externalBaseUrl=https://127.0.0.1:$GW_TLS_PORT" \
        --env-var "iamJwksBaseUrl=https://127.0.0.1:$IAM_JWKS_PORT" \
        --env-var "providerPublicBaseUrl=http://localhost:$HYDRA_PORT" \
        --env-var "iamRegistryTokenBaseUrl=https://127.0.0.1:$IAM_REGTOKEN_PORT" \
        "${OPT_ENV_ARR[@]}" \
        >"$d/out/suite.log" 2>&1; echo "$?" > "$d/out/suite.rc" ) &
    SUITE_PID[$svc]=$!
    WAVE_RUNNING+=("$!")
  done
  # Дождаться ТОЛЬКО суит. Голый `wait` ждал бы ещё и вечные пробросы
  # портов — то есть не вернулся бы никогда (см. пояснение у ограничителя выше).
  _wave_wait_below 1
  for svc in "$@"; do
    # Код возврата читается из ФАЙЛА, а не из `wait <pid>`: ограничитель выше уже
    # снял часть потомков опросом, и `wait` по такому pid ответил бы «не мой
    # потомок» — отказом, которого не было. Отсутствие файла — тоже исход, и он
    # считается отказом, а не успехом.
    st="RED"
    if [ "$(cat "$(suite_dir "$svc")/out/suite.rc" 2>/dev/null || echo 1)" = "0" ]; then st="GREEN"; else RC=1; fi
    echo "===== [$svc] $st ====="
    tail -n +1 "$(suite_dir "$svc")/out/summary.txt" 2>/dev/null || echo "  (no summary — see out/suite.log)"
  done
}

wave1=(); wave2=()
for svc in $SERVICES; do
  if _in_phase2 "$svc"; then wave2+=("$svc"); else wave1+=("$svc"); fi
done

if [ "${#wave1[@]}" -gt 0 ]; then
  echo "[parallel] WAVE 1 concurrent (delay=${DELAY}ms jobs=${JOBS}/suite; nlb --jobs 1): ${wave1[*]}"
  launch_wave "${wave1[@]}"
fi
# ЗДЕСЬ СТОЯЛ МЕЖВОЛНОВОЙ ГЕЙТ ОСЕДАНИЯ — он снят вместе со своим предметом (kacho#1049).
#
# Гейт ждал, пока дренаж применит к внешнему движку прав строки `kacho_iam.fga_outbox`,
# накопленные первой волной. Ни дренажа, ни движка больше нет (стадия S6 эпика #747):
# единственный потребитель журнала — триггер `relation_fact_from_journal`, и он складывает
# прямой факт В ТОЙ ЖЕ транзакции, что и вставку строки. Ждать нечего by construction:
# право действует С КОММИТА, а не «когда доедет».
#
# Скрипт при этом не просто устарел — он ОТКАЗЫВАЛ: колонки `sent_at`/`last_error`, по
# которым он считал глубину, сняты миграцией (kacho#917), а вызов стоял под `|| true`.
# То есть он молча не делал ничего, и это было неотличимо от исправной работы.
# Класс держит гейт `internal/repohygiene` TestJournalWithoutDeliveryMarkerIsNotQueriedByIt.
if [ "${#wave2[@]}" -gt 0 ]; then
  echo "[parallel] WAVE 2 isolated (no competing load): ${wave2[*]}"
  launch_wave "${wave2[@]}"
fi

# ─── WAVE 3: суита, которой нужен ВЫКЛЮЧЕННЫЙ store прав ─────────────────────
# Кейс «шлюз отказывает, когда хранилище прав недоступно» нельзя гонять рядом с
# остальными: условие, которое он проверяет, ломает их все. Раньше из этого
# следовало исключение в гейте — то есть инвариант «нет ответа о правах ⇒ отказ,
# никогда не 200» не проверялся ни разу, а выглядело это как зелёная суита.
# Теперь у него СВОЯ волна, которая условие СОЗДАЁТ: сворачивает store в ноль,
# гоняет одну коллекцию и поднимает обратно, дожидаясь готовности.
#
# Последней — потому что это единственный момент, когда стенд можно ломать: все
# остальные суиты уже отчитались. Исключений ей не полагается: не отработала ⇒
# отчёта нет ⇒ гейт ниже докладывает `authz-failclosed(no-report)` и краснеет.
FAILCLOSED_WAVE="${FAILCLOSED_WAVE:-true}"
FAILCLOSED_SH="$REPO_ROOT/services/iam/tests/newman/scripts/run-failclosed.sh"
if [ "$FAILCLOSED_WAVE" = "true" ] && [[ " $SERVICES " == *" iam "* ]] && [ -f "$FAILCLOSED_SH" ]; then
  echo "[parallel] WAVE 3 fail-closed (store прав сворачивается в ноль и поднимается обратно)"
  # Код возврата НЕ теряется: он уходит в RC (RAW-вердикт), а сам прогон
  # оставляет out/authz-failclosed.json, по которому вердикт вынесет гейт.
  if ( cd "$REPO_ROOT/services/iam/tests/newman" \
        && env SETUP_NS="$NS" DELAY="$DELAY" \
           EXTRA_NEWMAN_ARGS="--env-var baseUrl=http://localhost:$GW_PORT --env-var internalBaseUrl=http://localhost:$GW_INTERNAL_PORT --env-var externalBaseUrl=https://127.0.0.1:$GW_TLS_PORT --env-var iamJwksBaseUrl=https://127.0.0.1:$IAM_JWKS_PORT --env-var providerPublicBaseUrl=http://localhost:$HYDRA_PORT --env-var iamRegistryTokenBaseUrl=https://127.0.0.1:$IAM_REGTOKEN_PORT$OPT_ENV_ARGS" \
           bash "$FAILCLOSED_SH" ); then
    echo "===== [failclosed] GREEN ====="
  else
    echo "===== [failclosed] RED ====="
    RC=1
  fi
fi

# ─── WAVE 4: волна, у которой вызывающий — ЧЕЛОВЕК ───────────────────────────
# Часть набора описывает поведение, у которого вызывающий человек: аккаунт
# принадлежит пользователю by construction, а уровень аутентификации поднимается
# только церемонией входа. Машинный посев такого предъявителя не производит, и
# отходных путей ровно два — волна, создающая условие, либо ОТКРЫТЫЙ ДОЛГ С
# ЧИСЛОМ. Маска, список известных красных и «пока пропустим» в этот перечень не
# входят (testing.md).
#
# Набор волны НЕ выписан: он выводится из дерева единственным объявлением
# (tests/authz-fixtures/ceremony_credentials.py) на каждом запуске.
#
# ВКЛЮЧАЕТСЯ ВНЕШНИМ ФАКТОМ, а не ручкой: волна идёт ровно тогда, когда посев
# церемонии существует в дереве (артефакт стадии S2). Обе ветки достижимы и обе
# проверены: без посева печатается долг с числами, с посевом волна обязана
# отработать. Ручное `CEREMONY_WAVE=true` при отсутствии посева намеренно НЕ даёт
# зелёного — прогонщик волны в этом случае падает и отчётов не оставляет, поэтому
# гейт докладывает по её коллекциям `(no-report)`.
CEREMONY_WAVE="${CEREMONY_WAVE:-auto}"
CEREMONY_SH="$REPO_ROOT/services/iam/tests/newman/scripts/run-ceremony.sh"
CEREMONY_DECL="$REPO_ROOT/tests/authz-fixtures/ceremony_credentials.py"
if [[ " $SERVICES " == *" iam "* ]] && [ -f "$CEREMONY_SH" ] && [ -f "$CEREMONY_DECL" ]; then
  if [ "$CEREMONY_WAVE" = "auto" ]; then
    if python3 "$CEREMONY_DECL" --root "$REPO_ROOT" --seed-exists 2>/dev/null; then
      CEREMONY_WAVE=true
    else
      CEREMONY_WAVE=false
    fi
  fi
  if [ "$CEREMONY_WAVE" = "true" ]; then
    echo
    echo "[parallel] WAVE 4 церемония (условие создаётся посевом церемонии, затем её коллекции)"
    # The seed's four addresses come from the ports opened above, not from its own
    # defaults: the opener and the dialler must be one fact. BASE_URL/INTERNAL_BASE_URL
    # are passed for the same reason.
    #
    # PUBLIC_BASE is the SAME address as BASE_URL under a second name: the expired-bearer
    # stage this wave delegates to (tests/authz-fixtures/prodseed_expired_bearer.py) reads
    # that name and no other. Passing only BASE_URL left it on its own default, so moving
    # the gateway forward sent that stage to :18080 — whatever happens to be listening
    # there, which on a shared machine is somebody else's forward rather than nothing.
    # That failure mode is worse than a refusal: it answers. The duplicate name is carried
    # here rather than renamed at the reader, because the reader is also driven standalone
    # (services/iam/tests/newman/scripts/run-expired-bearer.sh) and renaming its knob from
    # here would move the drift instead of removing it.
    #
    # IAM_INTERNAL_GRPC — ПЯТЫЙ адрес церемонии, и его отсутствие здесь стоило волне
    # всех её коллекций. Стадия «2-администратор» чеканит предъявителя на внутреннем
    # порту iam, а этот блок передавал только четыре адреса, поэтому набиралось
    # умолчание `localhost:19091`. На стенде с перенесённым портом это отказ, и волна
    # не заводила условие → девять коллекций iam остались без отчёта.
    # Особенно наглядно то, что печатала при этом сама церемония: «1-преполёт: 4 из 4
    # адресов отвечают» — преполёт проверяет ровно те адреса, которые ему ДАЛИ, и
    # молчит про тот, которого не дали. Проверка достижимости не может обнаружить
    # транспорт, о котором её не спросили.
    # MTLS_ENV идёт следом по той же причине: внутренний порт требует клиентский
    # сертификат в любой посадке, и чеканка — единственный SAN, который iam для неё
    # допускает (см. сбор сертификатов выше).
    if ( cd "$REPO_ROOT/services/iam/tests/newman" \
          && env SETUP_NS="$NS" DELAY="$DELAY" \
             BASE_URL="http://localhost:$GW_PORT" \
             INTERNAL_BASE_URL="http://localhost:$GW_INTERNAL_PORT" \
             PUBLIC_BASE="http://localhost:$GW_PORT" \
             KRATOS_PUBLIC_URL="http://localhost:$KRATOS_PUBLIC_PORT" \
             KRATOS_ADMIN_URL="http://localhost:$KRATOS_ADMIN_PORT" \
             HYDRA_PUBLIC_URL="http://localhost:$HYDRA_PORT" \
             HYDRA_ADMIN_URL="https://localhost:$HYDRA_ADMIN_PORT" \
             IAM_INTERNAL_GRPC="localhost:$IAM_INTERNAL_PORT" "${MTLS_ENV[@]}" \
             EXTRA_NEWMAN_ARGS="--env-var baseUrl=http://localhost:$GW_PORT --env-var internalBaseUrl=http://localhost:$GW_INTERNAL_PORT --env-var externalBaseUrl=https://127.0.0.1:$GW_TLS_PORT --env-var iamJwksBaseUrl=https://127.0.0.1:$IAM_JWKS_PORT --env-var providerPublicBaseUrl=http://localhost:$HYDRA_PORT --env-var iamRegistryTokenBaseUrl=https://127.0.0.1:$IAM_REGTOKEN_PORT$OPT_ENV_ARGS" \
             bash "$CEREMONY_SH" ); then
      echo "===== [ceremony] GREEN ====="
    else
      echo "===== [ceremony] RED ====="
      RC=1
    fi
  else
    echo
    echo "[parallel] WAVE 4 церемония НЕ ИДЁТ — условие создать нечем:"
    python3 "$CEREMONY_DECL" --root "$REPO_ROOT" --debt
    echo "[parallel] Это НЕ зачёт и не вычет: перечисленные шаги сейчас исполняются машинным"
    echo "           принципалом, то есть отвечают не на тот вопрос, который кейс задаёт."
  fi
fi

echo
# ─── Verdict: RAW (what newman reported) + GATED (what CI grades) ────────────
# Local runners used to grade on RAW only, while CI graded through
# services/iam/tests/newman/scripts/assert-suites-green.sh — so the two disagreed by
# construction. Both now grade with the same script.
#
# The example that used to stand here is worth keeping as a warning rather than as
# documentation, because it was the defect: `iam-internal-only-check` reported 0 failed
# ASSERTIONS and a non-zero exit, because 8 requests could not resolve api.kacho.local.
# The disagreement was reconciled the wrong way round — CI subtracted those transport
# errors, on the stated reasoning that an unreachable advertised host IS the internal-only
# invariant. It is not. Those eight checks were the ban-#6 negatives, and they did not run.
# "Could not reach it" and "it refused me" are different findings; only the second is
# evidence. The endpoint is now forwarded (GW_TLS_PORT) so the probes execute, and the
# gate reports an unanswered request as UNANSWERED instead of subtracting it.
GATE="${GATE:-true}"
GATE_SCRIPT="$REPO_ROOT/services/iam/tests/newman/scripts/assert-suites-green.sh"

echo "===== PER-SUITE TOTALS (nothing is subtracted; the gate below agrees by construction) ====="
printf "%-12s %10s %10s %10s\n" "SUITE" "ASSERT-F" "REQ-F" "REPORTS"
raw_total_a=0; raw_total_r=0; zero_report_suites=""
for svc in $SERVICES; do
  d="$(suite_dir "$svc")"
  a=0; r=0; n=0
  for f in "$d"/out/*.json; do
    [ -e "$f" ] || continue
    n=$((n + 1))
    a=$((a + $(jq -r '.run.stats.assertions.failed // 0' "$f" 2>/dev/null || echo 0)))
    r=$((r + $(jq -r '.run.stats.requests.failed // 0' "$f" 2>/dev/null || echo 0)))
  done
  printf "%-12s %10s %10s %10s\n" "$svc" "$a" "$r" "$n"
  raw_total_a=$((raw_total_a + a)); raw_total_r=$((raw_total_r + r))
  # A suite that produced NO report contributes a row of zeros — and a row of zeros
  # is exactly what a clean suite looks like. The REPORTS column already carried the
  # count, but a number sitting in a column is not a signal; it has to be said. This
  # is the same distinction the coverage gate now makes one level down: "nothing
  # examined" must not read as "nothing found".
  if [ "$n" -eq 0 ]; then
    zero_report_suites="$zero_report_suites $svc"
  fi
done
printf "%-12s %10s %10s\n" "TOTAL" "$raw_total_a" "$raw_total_r"
if [ -n "$zero_report_suites" ]; then
  echo "[parallel] DID NOT RUN:${zero_report_suites} — zero reports on disk. The rows above say" >&2
  echo "           nothing about these suites; they are absent from the roll-up, not passing it." >&2
fi
if [ "$RC" -eq 0 ]; then echo "[parallel] runner verdict: ALL SUITES GREEN"; else echo "[parallel] runner verdict: one or more suites RED (see per-suite out/summary.txt + out/*.json)"; fi

if [ "$GATE" != "true" ] || [ ! -f "$GATE_SCRIPT" ]; then
  echo "[parallel] CI gate skipped (GATE=$GATE, script=$GATE_SCRIPT) — grading on the per-suite roll-up"
  exit "$RC"
fi

echo
echo "===== GATED (the exact gate CI runs: assert-suites-green.sh) ====="
GATE_RC=0
for svc in $SERVICES; do
  d="$(suite_dir "$svc")"
  echo "--- $svc"
  # cwd = the suite's newman dir: the gate walks collections/*.json vs out/<name>.json there.
  if ( cd "$d" && bash "$GATE_SCRIPT" ); then :; else GATE_RC=1; fi
done

echo
if [ "$GATE_RC" -eq 0 ]; then
  echo "[parallel] GATED verdict: GREEN (CI would pass)"
  # The gate deducts nothing, so GREEN here cannot coexist with failures above. If it
  # somehow does, the two are counting different things and THAT is the finding.
  [ "$RC" -eq 0 ] || echo "[parallel] INCONSISTENT: the per-suite roll-up above shows failures while the gate passed — the two disagree, and the gate subtracts nothing. Investigate the counters, do not trust this GREEN."
else
  echo "[parallel] GATED verdict: RED (CI would fail) — see the per-collection lines above"
fi
exit "$GATE_RC"
