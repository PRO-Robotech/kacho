#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# stand-second-factor-wrap-rotate.sh — производитель условия Ф12-35 «в» (kacho#3038):
# смена перечня ключей обёртки секретов второго фактора на стенде с перекатом
# службы доступа и ожиданием готовности края.
#
#   stand-second-factor-wrap-rotate.sh set KEYS --state DIR [--ns NS]
#       перечень KEYS из имён K1 и K2 через запятую (K2,K1 · K2 · K1): K1 — ключ
#       стенда ДО первого вызова с этим DIR, K2 — свежие 32 байта, один на DIR.
#       Секрет получает перечень, служба перекатывается, край готов; последняя
#       строка — «wrap-rotate: keys=<число> pods=<число> list=<KEYS>», число
#       ключей — из самоотчёта ЗАПУЩЕННОГО процесса, а не из заданного перечня.
#   stand-second-factor-wrap-rotate.sh restore --state DIR [--ns NS]
#       вернуть ключ стенда до пробы (K1) тем же перекатом и очистить DIR;
#       восстанавливать нечего — код 0
#   stand-second-factor-wrap-rotate.sh cell SERIES [--ns NS]
#       величина серии счётчика службы — сумма по живым подам; серии нет — 0
#   stand-second-factor-wrap-rotate.sh errors SINCE [--ns NS]
#       записи журнала службы уровня ошибки с момента SINCE (RFC3339), последняя
#       строка — «errors=<число>»
#   stand-second-factor-wrap-rotate.sh limit [--ns NS]
#       предел неверных предъявлений на адрес, который читает процесс
#   stand-second-factor-wrap-rotate.sh write set KEYS KEY | write restore KEY
#       ЗАПИСЬ условия — шаг МАШИНЫ по просьбе pod раннера (раздел «ПОД РАННЕРА»):
#       секрет, перекат, готовность конечных точек; каталог состояния — KEY под
#       KACHO_STAND_COND_STATE, пространство — только KACHO_STAND_NS
#
# Цель: make -C deploy stand-second-factor-wrap-rotate KEYS=… STATE=… [NS=…]
# Читатель: ui-future/e2e/specs/second-factor-wrap-rotation.spec.ts.
#
# ── ЗАЧЕМ ──────────────────────────────────────────────────────────────────────
#
# Ф12-35 «в» утверждает поведение службы при смене перечня МЕЖДУ шагами одной
# пробы: фактор заведён под K1; процесс перезапущен с K2,K1 — код сверяется; с
# K2 без K1 — 503 громко. Перезапуск с другим перечнем — не глагол продукта, и
# ни один прогонщик проб его не производил: позиция была «не выполнилось:
# условие не создано» по построению. Условие создаёт этот скрипт.
#
# ── КАКОЙ КЛАСТЕР И КАКОЕ ПРОСТРАНСТВО ────────────────────────────────────────
#
# Пространство — --ns, иначе KACHO_STAND_NS (его экспортирует `stand-ns.sh run`
# команде прогона), иначе рабочее пространство локального стенда `kacho`.
#
#   стенд проб внешнего кластера — контекст KACHO_CLIENT_CONTEXT (его выбирает
#       и экспортирует `stand-ns.sh run`, kacho#3065); пространство — только
#       тестовое по правилу t<номер>-<коротко>: рабочий стенд площадки этот
#       скрипт не трогает;
#   стенд kind (`make dev-up`, конвейер консоли) — активный контекст, и он
#       обязан быть kind-*: иначе пространство kacho оказалось бы рабочим
#       стендом чужого кластера.
# Ни то ни другое — код 2. Каждый вызов kubectl несёт выбранный контекст явно;
# готовность края спрашивает `wait-edge-ready.sh` с файлом конфигурации, в
# котором выбранный контекст — единственный.
#
# ── ЧТО ПЕРЕКАТЫВАЕТСЯ ────────────────────────────────────────────────────────
#
# Координата секрета и имя службы ВЫВОДЯТСЯ из того, что читает процесс: в
# пространстве ищется ровно один Deployment, у которого переменная
# KANAME_SECOND_FACTOR_ENC_KEY берётся ссылкой на секрет (чарт:
# charts/kaname/templates/deployment.yaml). Имя секрета, ключ, селектор подов —
# оттуда. Выписанное имя разошлось бы с чартом молча.
#
# Переменная из секрета читается процессом на старте, поэтому смена секрета
# без переката не меняет ничего: `rollout restart`, `rollout status`, готовые
# конечные точки каждой службы, выбирающей поды (kacho#3145), затем
# готовность края (`wait-edge-ready.sh` — край отвечает /readyz только когда
# отвечает служба доступа), затем готовность МАРШРУТА посева (kacho#3152):
# тот же адрес, что у посева, — `$KACHO_CONSOLE_URL/iam/v1/auth/csrf?form=login`
# через вход консоли — отвечает 200 с признаком формы. Число ключей — из строки
# самоотчёта старта «second-factor wrapping keys declared» каждого живого пода.
#
# ── АДРЕС МАРШРУТА ────────────────────────────────────────────────────────────
#
# Адрес — тот же, что у посева проб: KACHO_CONSOLE_URL (схема://хост[:порт]);
# имя хоста разрешается отображением KACHO_CONSOLE_HOST_IP, если оно задано;
# доверие TLS — ровно ключу листа KACHO_CONSOLE_CA, если он задан (как у
# браузера проб, stand-tls-trust.ts), иначе обычной проверкой. Все три
# экспортирует `stand-ns.sh run`; конвейер консоли ставит их шагу проб. Адрес
# не назван — код 2 до первой записи: угаданный адрес судил бы другой маршрут.
#
# ── ПОД РАННЕРА: ЗАПИСЬ — ШАГ МАШИНЫ (kacho#3346, kacho#3348) ─────────────────
#
# В pod раннера (`stand-ns.sh run --runner`) учётка `stand-runner` только читает:
# объекты, журналы, проброс, exec (судья роли stand-ns.sh `runner_judge`). Запись
# условия — правка секрета и перекат службы — ей не дана и не даётся. Поэтому
# там `set` и `restore` делятся надвое:
#
#   запись    просьба машине файлом в KACHO_STAND_COND_DIR pod («<имя>.req»:
#             строка «wrap-rotate<TAB>set<TAB>KEYS<TAB>KEY»); машина (цикл
#             `runner_run` stand-ns.sh) исполняет строку ведомости условий
#             `stand-conditions.tsv` — этот скрипт `write …` своим контекстом
#             -client — и кладёт рядом «<имя>.out» и «<имя>.rc»;
#   исход     pod судит сам, своей учёткой: готовность края, маршрут посева
#             изнутри pod, самоотчёт старта каждого живого пода.
#
# Машина не ответила за KACHO_WRAP_MACHINE_WAIT (с, умолчание 480) либо ответила
# не нулём — код 3: условие не создано, проба — «не выполнилось», а не зелёное.
# Pod раннера опознаётся тремя фактами сразу: учётка pod (файл пространства
# учётки) называет NS, KACHO_STAND_COND_DIR есть, а KUBECONFIG и ~/.kube/config —
# нет (kubectl без контекста иначе как учёткой pod говорить не может).
#
# ── МАТЕРИАЛ КЛЮЧЕЙ ───────────────────────────────────────────────────────────
#
# Величины не печатаются и в аргументы kubectl не уходят: K1 и K2 лежат в DIR
# (0700/0600), правка секрета идёт файлом заплатки (--patch-file), который
# удаляется сразу после применения. DIR принадлежит вызывающему; restore его
# очищает. K2 — `openssl rand -hex 32`, та же форма, что у посева
# dev-prod-secrets.sh.
#
# ── ИСХОДЫ ────────────────────────────────────────────────────────────────────
#
#   0 — сделано (условие создано);
#   2 — вызов неверен либо кластер, пространство, секрет не названы или не
#       выводятся: условие не создавалось, стенд не тронут;
#   3 — условие не создано на кластере: перекат не стал готов, конечные точки
#       служб не догнали его за предел, край не ответил, маршрут посева не
#       ответил 200 с признаком формы за предел, самоотчёта старта нет,
#       счётчик не прочитан.
# Красного у скрипта нет: судит проба.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_NAME="KANAME_SECOND_FACTOR_ENC_KEY"
EDGE_DEPLOY="api-gateway"
REPORT_MSG="second-factor wrapping keys declared"
ROLLOUT_TIMEOUT="${KACHO_WRAP_ROLLOUT_TIMEOUT:-300s}"

die()  { printf 'ABORT: wrap-rotate — %s\n' "$1" >&2; exit "${2:-2}"; }
log()  { printf '=== wrap-rotate: %s\n' "$*" >&2; }

usage() {
  sed -n '7,29p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 2
}

# ── РАЗБОР ВЫЗОВА ─────────────────────────────────────────────────────────────

[ $# -ge 1 ] || usage
CMD="$1"; shift
ARG=""
case "$CMD" in
  set|cell|errors) [ $# -ge 1 ] || usage; ARG="$1"; shift ;;
  restore|limit) ;;
  self-test) ;;
  write)
    # Аргументы шага машины — из просьбы pod: ни --ns, ни --state (пространство —
    # только KACHO_STAND_NS машины, каталог — только под KACHO_STAND_COND_STATE).
    [ $# -ge 2 ] || usage
    WOP="$1"; shift
    case "$WOP" in
      set) [ $# -eq 2 ] || die "write set: ждались KEYS KEY, получено $#"; ARG="$1"; WKEY="$2"; shift 2 ;;
      restore) [ $# -eq 1 ] || die "write restore: ждался KEY, получено $#"; WKEY="$1"; shift ;;
      *) die "write: операция «$WOP» вне set, restore" ;;
    esac
    [[ "$WKEY" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$ ]] || die "write: ключ состояния «$WKEY» не имя каталога" ;;
  *) usage ;;
esac
NS="" STATE=""
while [ $# -gt 0 ]; do
  case "$1" in
    --ns)    NS="${2:?--ns без значения}"; shift ;;
    --state) STATE="${2:?--state без значения}"; shift ;;
    *) die "неизвестный аргумент «$1»" ;;
  esac
  shift
done
NS="${NS:-${KACHO_STAND_NS:-kacho}}"

# ── КЛАСТЕР ───────────────────────────────────────────────────────────────────

CTX="" IN_POD=0
POD_NS_FILE="${KACHO_WRAP_POD_NS_FILE:-/var/run/secrets/kubernetes.io/serviceaccount/namespace}"
TNS_RE='^t[0-9]+-[a-z0-9]([-a-z0-9]*[a-z0-9])?$'

# in_runner_pod — вызов идёт в pod раннера своего пространства (раздел «ПОД РАННЕРА»).
in_runner_pod() {
  [ -n "${KUBERNETES_SERVICE_HOST:-}" ] && [ -n "${KACHO_STAND_COND_DIR:-}" ] && [ -d "$KACHO_STAND_COND_DIR" ] &&
    [ -z "${KACHO_CLIENT_CONTEXT:-}" ] && [ -z "${KUBECONFIG:-}" ] && [ ! -e "${HOME:-/nonexistent}/.kube/config" ] &&
    [ -r "$POD_NS_FILE" ] && [ "$(cat "$POD_NS_FILE")" = "$NS" ] && [[ "$NS" =~ $TNS_RE ]]
}

pick_context() {
  if in_runner_pod; then
    CTX="" IN_POD=1
    EDGE_VIA="port-forward"
    return
  fi
  if [ -n "${KACHO_CLIENT_CONTEXT:-}" ]; then
    [[ "$NS" =~ ^t[0-9]+-[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] ||
      die "пространство «$NS» на названном внешнем кластере не тестовое (t<номер>-<коротко>): рабочий стенд площадки скрипт не трогает"
    CTX="$KACHO_CLIENT_CONTEXT"
    EDGE_VIA="port-forward"
    return
  fi
  [ "$NS" = kacho ] || die "пространство стенда проб «$NS» без названного кластера: KACHO_CLIENT_CONTEXT не задан (его экспортирует stand-ns.sh run)"
  local cur got want
  cur="$(command kubectl config current-context 2>/dev/null)" || cur=""
  # Имя контекста — первый отсев ради внятного совета; идентичность — адрес
  # apiserver'а, который kind выдаёт о своём кластере сам: одноимённый контекст,
  # ведущий в другой кластер, этой сверки не проходит.
  [[ "$cur" == kind-* ]] ||
    die "пространство kacho, а активный контекст «${cur:-<нет>}» не kind-*: рабочий стенд чужого кластера скрипт не трогает; для стенда проб назови KACHO_CLIENT_CONTEXT"
  command -v kind >/dev/null 2>&1 || die "контекст $cur назван kind-*, а kind нет — кластер не подтверждён"
  want="$(kind get kubeconfig --name "${cur#kind-}" 2>/dev/null | python3 -c '
import sys, yaml
c = yaml.safe_load(sys.stdin) or {}
print(((c.get("clusters") or [{}])[0].get("cluster") or {}).get("server", ""))
')"
  got="$(command kubectl config view --minify --context "$cur" -o 'jsonpath={.clusters[0].cluster.server}' 2>/dev/null)"
  [ -n "$want" ] && [ "$want" = "$got" ] ||
    die "контекст $cur ведёт на «${got:-<нет>}», а kind о кластере ${cur#kind-} говорит «${want:-<нет>}» — кластер не тот"
  CTX="$cur"
  EDGE_VIA="${EDGE_READY_VIA:-proxy}"
}

k() {
  if [ "$IN_POD" = 1 ]; then command kubectl -n "$NS" "$@"; return; fi
  command kubectl --context "$CTX" -n "$NS" "$@"
}

# ── ЧТО ЧИТАЕТ ПРОЦЕСС ───────────────────────────────────────────────────────

# target — Deployment, чья переменная ENV_NAME берётся из секрета. Печатает
# строку: deploy<TAB>container<TAB>secret<TAB>key<TAB>selector<TAB>configmap<TAB>attempts-env.
target() {
  local json
  json="$(k get deploy -o json 2>&1)" || die "Deployment'ы пространства $NS не прочитаны: $(head -1 <<<"$json")" 3
  python3 -c "$TARGET_PY" "$ENV_NAME" <<<"$json"
}

# TARGET_PY — разбор Deployment'ов пространства (программа отдельно от входа:
# вход идёт на stdin).
read -r -d '' TARGET_PY <<'PY'
import json, sys
env = sys.argv[1]
doc = json.load(sys.stdin)
rows = []
for d in doc.get("items", []):
    spec = d.get("spec", {})
    tpl = spec.get("template", {}).get("spec", {})
    cm = ""
    for v in tpl.get("volumes", []) or []:
        if v.get("name") == "config" and v.get("configMap"):
            cm = v["configMap"].get("name", "")
    for c in tpl.get("containers", []) or []:
        envs = c.get("env", []) or []
        ref = None
        attempts = ""
        for e in envs:
            if e.get("name") == env:
                ref = ((e.get("valueFrom") or {}).get("secretKeyRef")) or None
            if e.get("name") == "KANAME_AUTHN__LOGIN__ADDRESS_ATTEMPTS":
                attempts = str(e.get("value", ""))
        if ref:
            sel = ",".join(f"{k}={v}" for k, v in sorted((spec.get("selector", {}).get("matchLabels") or {}).items()))
            rows.append("\t".join([d["metadata"]["name"], c["name"], ref.get("name", ""), ref.get("key", ""), sel, cm, attempts]))
if len(rows) != 1:
    sys.stderr.write(f"Deployment'ов, читающих {env} из секрета: {len(rows)}, а не 1\n")
    sys.exit(1)
print(rows[0])
PY

TGT=""
load_target() {
  TGT="$(target)" || die "служба, читающая $ENV_NAME из секрета, в пространстве $NS не выведена (выше)"
  IFS=$'\t' read -r DEPLOY CONTAINER SECRET SKEY SELECTOR CONFIGMAP ENV_ATTEMPTS <<<"$TGT"
  [ -n "$SECRET" ] && [ -n "$SKEY" ] && [ -n "$SELECTOR" ] ||
    die "у $DEPLOY не выведены секрет, ключ или селектор подов ($TGT)"
}

# live_pods — живые поды службы (Running, не в завершении): «имя<TAB>порт<TAB>схема».
live_pods() {
  local json
  json="$(k get pods -l "$SELECTOR" -o json 2>&1)" || { log "поды $SELECTOR не прочитаны: $(head -1 <<<"$json")"; return 1; }
  python3 -c '
import json, sys
for p in json.load(sys.stdin).get("items", []):
    md = p.get("metadata", {})
    if md.get("deletionTimestamp") or p.get("status", {}).get("phase") != "Running":
        continue
    a = md.get("annotations") or {}
    print("\t".join([md["name"], a.get("prometheus.io/port", ""), a.get("prometheus.io/scheme", "http") or "http"]))
' <<<"$json"
}

# ── СОСТОЯНИЕ ─────────────────────────────────────────────────────────────────

need_state() {
  [ -n "$STATE" ] || die "каталог состояния не назван (--state DIR): ключ стенда до пробы хранить негде"
  mkdir -p "$STATE" || die "каталог состояния $STATE не заведён"
  chmod 700 "$STATE" || die "каталог состояния $STATE не закрыт (chmod 700)"
}

current_value() {
  local b64 out
  out="$(k get secret "$SECRET" -o "jsonpath={.data.$SKEY}" 2>&1)" ||
    die "секрет $SECRET (ключ $SKEY) в пространстве $NS не прочитан: $(head -1 <<<"$out")"
  b64="$out"
  [ -n "$b64" ] || die "в секрете $SECRET нет ключа $SKEY"
  base64 -d <<<"$b64" 2>/dev/null || die "ключ $SKEY секрета $SECRET не base64"
}

# compose KEYS — величина перечня из имён; файлы K1/K2 в STATE.
compose() {
  local keys="$1" name out="" seen=" "
  [ -n "$keys" ] || die "перечень пуст: назови K1 и/или K2 через запятую"
  IFS=, read -r -a names <<<"$keys"
  [ "${keys: -1}" != "," ] || die "перечень «$keys»: пустое имя"
  for name in "${names[@]}"; do
    case "$name" in
      K1|K2) ;;
      "") die "перечень «$keys»: пустое имя" ;;
      *) die "перечень «$keys»: имя «$name» вне словаря K1, K2" ;;
    esac
    [[ "$seen" != *" $name "* ]] || die "перечень «$keys»: имя $name дважды"
    seen="$seen$name "
    out="${out:+$out,}$(cat "$STATE/$name")"
  done
  printf '%s' "$out"
}

prepare_keys() {
  if [ ! -s "$STATE/K1" ]; then
    (umask 077; current_value >"$STATE/K1") || exit $?
    [ -s "$STATE/K1" ] || die "ключ стенда до пробы пуст"
    log "ключ стенда до пробы сохранён в каталоге состояния (величина не печатается)"
  fi
  if [ ! -s "$STATE/K2" ]; then
    (umask 077; openssl rand -hex 32 | tr -d '\n' >"$STATE/K2") || die "свежий ключ K2 не отчеканен"
  fi
}

# apply VALUE-FILE — секрет получает величину файла; перекат; готовность края.
apply_value() {
  local file="$1" patch out
  patch="$(mktemp "$STATE/patch.XXXXXX")" || die "файл заплатки не заведён"
  python3 -c '
import base64, json, sys
v = open(sys.argv[1], "rb").read()
json.dump({"data": {sys.argv[2]: base64.b64encode(v).decode()}}, open(sys.argv[3], "w"))
' "$file" "$SKEY" "$patch" || { rm -f "$patch"; die "заплатка секрета не составлена"; }
  out="$(k patch secret "$SECRET" --type merge --patch-file "$patch" 2>&1)"
  local rc=$?
  rm -f "$patch"
  [ "$rc" -eq 0 ] || die "секрет $SECRET не изменён: $(head -1 <<<"$out")" 3
  out="$(k rollout restart "deployment/$DEPLOY" 2>&1)" || die "перекат $DEPLOY не начат: $(head -1 <<<"$out")" 3
  if ! out="$(k rollout status "deployment/$DEPLOY" --timeout="$ROLLOUT_TIMEOUT" 2>&1)"; then
    k get pods -l "$SELECTOR" -o wide >&2 2>&1 || true
    die "перекат $DEPLOY не стал готов за $ROLLOUT_TIMEOUT: $(tail -1 <<<"$out")" 3
  fi
  endpoints_ready || die "конечные точки служб $DEPLOY не готовы после переката (выше)" 3
}

# verify_applied — готовность края и маршрута посева после переката: та половина,
# что судит исход (в pod раннера — его учёткой и его маршрутом).
verify_applied() {
  edge_ready || die "край не готов после переката $DEPLOY (выше)" 3
  route_ready || die "маршрут посева $ROUTE_URL не готов после переката $DEPLOY (выше)" 3
}

# machine_step OP [ARG] — запись условия просьбой машине (раздел «ПОД РАННЕРА»).
# Печатает вывод машины; её код не 0 либо ответа нет за предел — код 3 (условие не создано).
MACHINE_WAIT="${KACHO_WRAP_MACHINE_WAIT:-480}"
machine_step() {
  local dir="$KACHO_STAND_COND_DIR" id req i rc out
  [[ "$MACHINE_WAIT" =~ ^[0-9]+$ ]] || die "KACHO_WRAP_MACHINE_WAIT=«$MACHINE_WAIT» — не число секунд"
  id="wrap-rotate-$(date -u +%Y%m%dT%H%M%SZ)-$$-$RANDOM"
  req="$(printf 'wrap-rotate\twrite\t%s' "$1")"
  [ $# -lt 2 ] || req="$req$(printf '\t%s' "$2")"
  req="$req$(printf '\t%s' "$(basename "$STATE")")"
  printf '%s\n' "$req" >"$dir/$id.req.tmp" && mv "$dir/$id.req.tmp" "$dir/$id.req" ||
    die "просьба машине не записана в $dir" 3
  log "запись условия — шаг машины: просьба $id ($*), ждём ответа до $MACHINE_WAIT с"
  for ((i = 0; i < MACHINE_WAIT; i++)); do
    [ -e "$dir/$id.rc" ] && break
    sleep 1
  done
  if [ ! -e "$dir/$id.rc" ]; then
    rm -f "$dir/$id.req"
    die "условие не создано: шаг машины «wrap-rotate $*» не исполнен за $MACHINE_WAIT с — просьбу никто не взял (машина не служит ведомости условий stand-conditions.tsv)" 3
  fi
  rc="$(cat "$dir/$id.rc")"; out="$(cat "$dir/$id.out" 2>/dev/null)"
  printf '%s\n' "$out" >&2
  [ "$rc" = 0 ] || die "условие не создано: шаг машины «wrap-rotate $*» завершился кодом $rc (вывод выше)" 3
  printf '%s\n' "$out"
}

# ── МАРШРУТ ПОСЕВА ────────────────────────────────────────────────────────────
#
# route_ready — kacho#3152 (job 114526162992). Готовые конечные точки службы
# доступа и `/readyz` края, спрошенный у пода напрямую, не говорят, что ВХОД
# КОНСОЛИ ведёт к краю: во время переката службы доступа под края теряет
# готовность, его адрес уходит из конечных точек службы края, и контроллер
# входа догоняет их возврат позже, чем под снова отвечает `/readyz`. Первый
# запрос посева в этот зазор получал «503 Service Temporarily Unavailable»
# контроллера. Готовность — когда ТОТ ЖЕ запрос, с которого начинает посев
# (признак формы входа), отвечает 200 с непустым csrfToken STREAK раз подряд;
# 200 без признака (страница раздачи) готовностью не является. Предел —
# ATTEMPTS запросов через INTERVAL секунд; исчерпан — отказ с последним ответом.
ROUTE_PATH="/iam/v1/auth/csrf?form=login"
ROUTE_ATTEMPTS="${KACHO_WRAP_ROUTE_ATTEMPTS:-90}"
ROUTE_INTERVAL="${KACHO_WRAP_ROUTE_INTERVAL:-2}"
ROUTE_STREAK="${KACHO_WRAP_ROUTE_STREAK:-3}"
ROUTE_URL=""
ROUTE_ARGS=()

# route_address — адрес и способ запроса маршрута из окружения посева; вызывается
# до первой записи, ошибка — код 2.
route_address() {
  local base="${KACHO_CONSOLE_URL:-}" scheme host port spki
  [ -n "$base" ] ||
    die "адрес маршрута посева не назван: KACHO_CONSOLE_URL пуст (его экспортирует stand-ns.sh run; конвейер ставит шагу проб) — готовность входа консоли спрашивать не у кого"
  [[ "$base" =~ ^(https?)://([A-Za-z0-9.-]+)(:([0-9]+))?/?$ ]] ||
    die "KACHO_CONSOLE_URL «$base» — не адрес вида схема://хост[:порт]"
  scheme="${BASH_REMATCH[1]}" host="${BASH_REMATCH[2]}" port="${BASH_REMATCH[4]}"
  [ -n "$port" ] || { [ "$scheme" = https ] && port=443 || port=80; }
  command -v curl >/dev/null 2>&1 || die "нет curl — маршрут посева спрашивать нечем"
  ROUTE_URL="${base%/}$ROUTE_PATH"
  ROUTE_ARGS=(--silent --show-error --max-time 10 -H "Accept: application/json")
  if [ -n "${KACHO_CONSOLE_HOST_IP:-}" ]; then
    ROUTE_ARGS+=(--resolve "$host:$port:$KACHO_CONSOLE_HOST_IP")
  fi
  if [ -n "${KACHO_CONSOLE_CA:-}" ]; then
    [ -r "$KACHO_CONSOLE_CA" ] || die "KACHO_CONSOLE_CA=$KACHO_CONSOLE_CA: файл листа стенда не читается"
    spki="$(openssl x509 -in "$KACHO_CONSOLE_CA" -pubkey -noout 2>/dev/null |
      openssl pkey -pubin -outform der 2>/dev/null | openssl dgst -sha256 -binary | base64)"
    [ -n "$spki" ] && [ "$spki" != "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=" ] ||
      die "KACHO_CONSOLE_CA=$KACHO_CONSOLE_CA: не сертификат PEM — ключ листа не выведен"
    # Доверие ровно ключу листа: цепочка не проверяется, ключ сверяется всегда.
    ROUTE_ARGS+=(--insecure --pinnedpubkey "sha256//$spki")
  fi
}

route_ready() {
  local i streak=0 body err code why="" verdict
  body="$(mktemp)" err="$(mktemp)" || return 1
  log "маршрут посева: ждём 200 с признаком формы от $ROUTE_URL ($ROUTE_STREAK запроса подряд, попыток до $ROUTE_ATTEMPTS)"
  for ((i = 1; i <= ROUTE_ATTEMPTS; i++)); do
    : >"$body"
    code="$(curl "${ROUTE_ARGS[@]}" -o "$body" -w '%{http_code}' "$ROUTE_URL" 2>"$err")" || true
    verdict="$(python3 -c '
import json, sys
code, path = sys.argv[1], sys.argv[2]
raw = open(path, "rb").read().decode("utf-8", "replace")
if code != "200":
    print("NO ответ " + (code or "000") + ": " + " ".join(raw.split())[:200]); sys.exit()
try:
    tok = json.loads(raw).get("csrfToken")
except (ValueError, AttributeError):
    tok = None
if isinstance(tok, str) and tok:
    print("OK"); sys.exit()
print("NO ответ 200 без csrfToken: " + " ".join(raw.split())[:200])
' "$code" "$body" 2>&1)"
    if [ "$verdict" = OK ]; then
      streak=$((streak + 1))
      if [ "$streak" -ge "$ROUTE_STREAK" ]; then
        log "маршрут посева готов: 200 с признаком формы подряд $streak; попытка $i из $ROUTE_ATTEMPTS"
        rm -f "$body" "$err"
        return 0
      fi
    else
      streak=0
      why="${verdict#NO }"
      [ -s "$err" ] && why="$why; $(head -1 "$err")"
    fi
    [ "$i" -lt "$ROUTE_ATTEMPTS" ] && sleep "$ROUTE_INTERVAL"
  done
  rm -f "$body" "$err"
  log "маршрут посева не готов за $ROUTE_ATTEMPTS запросов: ${why:-серия 200 не набрана}"
  return 1
}

# endpoints_ready — kacho#3145. `rollout status` говорит «новые реплики
# доступны», но не «адрес службы ведёт на них»: конечные точки (EndpointSlice)
# догоняют перекат позже, и проба, стартовавшая в этот зазор, получала 503
# входа и `connection refused` адреса службы доступа. Готовность — когда у
# КАЖДОЙ службы (Service), чей селектор выбирает живые поды Deployment'а,
# готовые адреса — ровно живые готовые поды, и ни один адрес (в любом
# состоянии) не ведёт на под вне этого множества; держится STREAK опросов
# подряд. Служб ноль — не готовность: спрашивать не о чем. Предел — ATTEMPTS
# опросов через INTERVAL секунд; исчерпан — отказ с последней причиной.
ENDPOINTS_ATTEMPTS="${KACHO_WRAP_ENDPOINTS_ATTEMPTS:-90}"
ENDPOINTS_INTERVAL="${KACHO_WRAP_ENDPOINTS_INTERVAL:-2}"
ENDPOINTS_STREAK="${KACHO_WRAP_ENDPOINTS_STREAK:-3}"

endpoints_ready() {
  local i streak=0 pods svcs slices verdict why=""
  log "конечные точки служб $DEPLOY: ждём готовые адреса ровно живых подов ($ENDPOINTS_STREAK опроса подряд, попыток до $ENDPOINTS_ATTEMPTS)"
  for ((i = 1; i <= ENDPOINTS_ATTEMPTS; i++)); do
    if pods="$(k get pods -l "$SELECTOR" -o json 2>&1)" &&
      svcs="$(k get services -o json 2>&1)" &&
      slices="$(k get endpointslices -o json 2>&1)"; then
      verdict="$(python3 -c "$ENDPOINTS_PY" "$pods" "$svcs" "$slices" 2>&1)"
    else
      verdict="NO поды, службы или конечные точки не прочитаны: $(head -1 <<<"${slices:-${svcs:-$pods}}")"
    fi
    if [ "${verdict%% *}" = OK ]; then
      streak=$((streak + 1))
      if [ "$streak" -ge "$ENDPOINTS_STREAK" ]; then
        log "конечные точки готовы: ${verdict#OK }; подряд $streak; попытка $i из $ENDPOINTS_ATTEMPTS"
        return 0
      fi
    else
      streak=0
      why="${verdict#NO }"
    fi
    [ "$i" -lt "$ENDPOINTS_ATTEMPTS" ] && sleep "$ENDPOINTS_INTERVAL"
  done
  log "конечные точки не готовы за $ENDPOINTS_ATTEMPTS опросов: ${why:-$verdict}"
  return 1
}

# ENDPOINTS_PY — суд одного опроса: argv — поды селектора, службы, конечные
# точки пространства (JSON). Печатает «OK <перепись>» либо «NO <причина>».
read -r -d '' ENDPOINTS_PY <<'PYEP'
import json, sys
try:
    pods, svcs, slices = (json.loads(a) for a in sys.argv[1:4])
except ValueError as e:
    print(f"NO ответ кластера не разобран: {e}"); sys.exit()
def ready(p):
    return any(c.get("type") == "Ready" and c.get("status") == "True" for c in p.get("status", {}).get("conditions") or [])
live = {p["metadata"]["name"]: p["metadata"].get("labels") or {} for p in pods.get("items", [])
        if not p["metadata"].get("deletionTimestamp") and p.get("status", {}).get("phase") == "Running" and ready(p)}
if not live:
    print("NO живых готовых подов нет"); sys.exit()
names = []
for s in svcs.get("items", []):
    sel = (s.get("spec") or {}).get("selector") or {}
    if sel and all(all(l.get(k) == v for k, v in sel.items()) for l in live.values()):
        names.append(s["metadata"]["name"])
if not names:
    print("NO служб, чей селектор выбирает живые поды, ноль — спрашивать не о чем"); sys.exit()
bad = []
for n in sorted(names):
    rdy, other = set(), set()
    for sl in slices.get("items", []):
        if (sl.get("metadata", {}).get("labels") or {}).get("kubernetes.io/service-name") != n:
            continue
        for ep in sl.get("endpoints") or []:
            pod = (ep.get("targetRef") or {}).get("name", "")
            if (ep.get("conditions") or {}).get("ready") is True:
                rdy.add(pod)
            if pod not in live:
                other.add(pod or "<без пода>")
    miss = sorted(set(live) - rdy)
    if miss or other:
        bad.append(f"{n}: не готовы адреса {','.join(miss) or '-'}; адреса чужих подов {','.join(sorted(other)) or '-'}")
if bad:
    print("NO " + "; ".join(bad)); sys.exit()
print(f"OK служб {len(names)} ({' '.join(sorted(names))}), готовых подов {len(live)} ({' '.join(sorted(live))})")
PYEP

# edge_ready — готовность края тем же судьёй, что у подъёма стенда, с файлом
# конфигурации, в котором выбранный контекст единственный.
edge_ready() {
  local cfg rc
  if [ "$IN_POD" = 1 ]; then
    # Учётка pod: kubectl без файла конфигурации говорит лишь ею (in_runner_pod).
    EDGE_READY_VIA="$EDGE_VIA" bash "$HERE/wait-edge-ready.sh" "$NS" "$EDGE_DEPLOY" \
      "${EDGE_READY_ATTEMPTS:-60}" "${EDGE_READY_INTERVAL:-2}" "${EDGE_READY_STREAK:-3}" >&2
    return
  fi
  cfg="$(mktemp)" || return 1
  command kubectl config view --minify --flatten --context "$CTX" >"$cfg" 2>/dev/null || { rm -f "$cfg"; log "контекст $CTX не выписан"; return 1; }
  KUBECONFIG="$cfg" EDGE_READY_VIA="$EDGE_VIA" bash "$HERE/wait-edge-ready.sh" "$NS" "$EDGE_DEPLOY" \
    "${EDGE_READY_ATTEMPTS:-60}" "${EDGE_READY_INTERVAL:-2}" "${EDGE_READY_STREAK:-3}" >&2
  rc=$?
  rm -f "$cfg"
  return "$rc"
}

# report LIST — самоотчёт старта каждого живого пода; все обязаны совпасть.
report() {
  local list="$1" pods name n keys="" count=0
  pods="$(live_pods)" || die "поды службы не прочитаны" 3
  [ -n "$pods" ] || die "живых подов службы ($SELECTOR) нет" 3
  while IFS=$'\t' read -r name _ _; do
    n="$(k logs "$name" -c "$CONTAINER" 2>/dev/null | python3 -c '
import json, sys
msg = sys.argv[1]
last = ""
for line in sys.stdin:
    try:
        rec = json.loads(line)
    except ValueError:
        continue
    if rec.get("msg") == msg and isinstance(rec.get("keys"), int):
        last = str(rec["keys"])
print(last)
' "$REPORT_MSG")"
    [ -n "$n" ] || die "под $name не напечатал самоотчёт старта «$REPORT_MSG»" 3
    [ -z "$keys" ] || [ "$keys" = "$n" ] || die "поды службы назвали разное число ключей ($keys и $n у $name)" 3
    keys="$n"; count=$((count + 1))
  done <<<"$pods"
  printf 'wrap-rotate: keys=%s pods=%s list=%s\n' "$keys" "$count" "$list"
}

# check_keys KEYS — перечень проверяется до первой записи: ошибочное имя не трогает
# ни секрета, ни состояния.
check_keys() {
  local keys="$1" n
  case ",$keys," in *,,*) die "перечень «$keys»: пустое имя" ;; esac
  [ -n "$keys" ] || die "перечень пуст: назови K1 и/или K2 через запятую"
  for n in ${keys//,/ }; do case "$n" in K1|K2) ;; *) die "перечень «$keys»: имя «$n» вне словаря K1, K2" ;; esac; done
}

cmd_set() {
  local keys="$1" value
  need_state
  pick_context
  load_target
  route_address
  check_keys "$keys"
  if [ "$IN_POD" = 1 ]; then
    machine_step set "$keys" >/dev/null
    verify_applied
    report "$keys"
    return
  fi
  prepare_keys
  value="$(mktemp "$STATE/value.XXXXXX")" || die "файл величины не заведён"
  (compose "$keys" >"$value") || { rm -f "$value"; exit 2; }
  log "секрет $SECRET ← перечень $keys; перекат $DEPLOY в $NS (контекст $CTX)"
  apply_value "$value"
  rm -f "$value"
  verify_applied
  report "$keys"
}

cmd_restore() {
  need_state
  if in_runner_pod; then
    local out
    pick_context
    load_target
    route_address
    out="$(machine_step restore)" || exit $?
    if [[ "$out" == *"wrap-rotate-write: nothing"* ]]; then
      log "ключ стенда до пробы не сохранялся — восстанавливать нечего"
      return 0
    fi
    verify_applied
    report "K1"
    return
  fi
  if [ ! -s "$STATE/K1" ]; then
    log "ключ стенда до пробы не сохранялся — восстанавливать нечего"
    rm -rf "${STATE:?}"/* 2>/dev/null
    return 0
  fi
  pick_context
  load_target
  route_address
  log "секрет $SECRET ← ключ стенда до пробы; перекат $DEPLOY в $NS (контекст $CTX)"
  apply_value "$STATE/K1"
  verify_applied
  report "K1"
  rm -rf "${STATE:?}"/*
}

# cmd_write — шаг МАШИНЫ (раздел «ПОД РАННЕРА»): только запись и перекат, своим
# контекстом -client; исход судит pod. Пространство — KACHO_STAND_NS машины, каталог
# состояния — KACHO_STAND_COND_STATE/KEY; в pod раннера и без контекста — отказ.
cmd_write() {
  [ -n "${KACHO_CLIENT_CONTEXT:-}" ] || die "write — шаг машины стенда проб: KACHO_CLIENT_CONTEXT не задан (его экспортирует stand-ns.sh run)"
  ! in_runner_pod || die "write — шаг машины: в pod раннера записи нет (учётка pod только читает)"
  [ -n "${KACHO_STAND_COND_STATE:-}" ] || die "write: KACHO_STAND_COND_STATE не задан — ключ стенда до пробы хранить негде"
  [ -n "${KACHO_STAND_NS:-}" ] && [ "$NS" = "$KACHO_STAND_NS" ] || die "write: пространство — только KACHO_STAND_NS машины"
  STATE="$KACHO_STAND_COND_STATE/$WKEY"
  need_state
  pick_context
  load_target
  case "$WOP" in
    set)
      local value
      check_keys "$ARG"
      prepare_keys
      value="$(mktemp "$STATE/value.XXXXXX")" || die "файл величины не заведён"
      (compose "$ARG" >"$value") || { rm -f "$value"; exit 2; }
      log "шаг машины: секрет $SECRET ← перечень $ARG; перекат $DEPLOY в $NS"
      apply_value "$value"
      rm -f "$value"
      echo "wrap-rotate-write: done list=$ARG" ;;
    restore)
      if [ ! -s "$STATE/K1" ]; then
        rm -rf "${STATE:?}"
        echo "wrap-rotate-write: nothing"
        return 0
      fi
      log "шаг машины: секрет $SECRET ← ключ стенда до пробы; перекат $DEPLOY в $NS"
      apply_value "$STATE/K1"
      rm -rf "${STATE:?}"
      echo "wrap-rotate-write: done list=K1" ;;
  esac
}

cmd_cell() {
  local series="$1" pods name port scheme lp pf logf body sum=0 v
  pick_context
  load_target
  pods="$(live_pods)" || die "поды службы не прочитаны" 3
  [ -n "$pods" ] || die "живых подов службы ($SELECTOR) нет" 3
  while IFS=$'\t' read -r name port scheme; do
    [ -n "$port" ] || die "у пода $name нет объявления сбора (prometheus.io/port)" 3
    logf="$(mktemp)"
    k port-forward "pod/$name" ":$port" >"$logf" 2>&1 &
    pf=$!
    lp=""
    for _ in $(seq 1 40); do
      lp="$(sed -nE 's/^Forwarding from 127\.0\.0\.1:([0-9]+) .*/\1/p' "$logf" | head -1)"
      [ -n "$lp" ] && break
      sleep 0.25
    done
    if [ -n "$lp" ]; then
      if [ "$scheme" = https ]; then
        body="$(curl -s --max-time 10 --insecure -w '\n%{http_code}' "https://127.0.0.1:$lp/metrics" || true)"
      else
        body="$(curl -s --max-time 10 -w '\n%{http_code}' "http://127.0.0.1:$lp/metrics" || true)"
      fi
    else
      body=""
    fi
    kill "$pf" 2>/dev/null; wait "$pf" 2>/dev/null
    if [ -z "$lp" ] || [ "$(tail -n1 <<<"$body")" != 200 ]; then
      log "журнал проброса: $(head -2 "$logf" | tr '\n' ' ')"
      rm -f "$logf"
      die "поверхность счётчиков пода $name не прочитана (${scheme}, порт $port)" 3
    fi
    rm -f "$logf"
    v="$(python3 -c '
import sys
want = sys.argv[1]
total = 0.0
for line in sys.stdin:
    line = line.strip()
    if not line or line.startswith("#"):
        continue
    name, _, val = line.rpartition(" ")
    if name == want:
        total += float(val)
print(int(total) if total == int(total) else total)
' "$series" <<<"$(sed '$d' <<<"$body")")" || die "ответ счётчиков пода $name не разобран" 3
    sum="$(python3 -c 'import sys; a=float(sys.argv[1])+float(sys.argv[2]); print(int(a) if a==int(a) else a)' "$sum" "$v")"
  done <<<"$pods"
  echo "$sum"
}

cmd_errors() {
  local since="$1" out
  date -u -d "$since" +%s >/dev/null 2>&1 && [[ "$since" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T ]] ||
    die "момент «$since» не RFC3339"
  pick_context
  load_target
  out="$(k logs -l "$SELECTOR" -c "$CONTAINER" --since-time="$since" --prefix --tail=-1 --max-log-requests=20 2>&1)" ||
    die "журнал службы не прочитан: $(head -1 <<<"$out")" 3
  python3 -c '
import json, sys
n = 0
for line in sys.stdin:
    raw = line.rstrip("\n")
    body = raw.split("] ", 1)[1] if raw.startswith("[") and "] " in raw else raw
    try:
        rec = json.loads(body)
    except ValueError:
        continue
    if str(rec.get("level", "")).upper() == "ERROR":
        print(raw)
        n += 1
print(f"errors={n}")
' <<<"$out"
}

cmd_limit() {
  local cfg v
  pick_context
  load_target
  if [ -n "$ENV_ATTEMPTS" ]; then
    echo "$ENV_ATTEMPTS"
    return
  fi
  [ -n "$CONFIGMAP" ] || die "у $DEPLOY нет тома настроек config — предел читать негде" 3
  cfg="$(k get configmap "$CONFIGMAP" -o 'jsonpath={.data.config\.yaml}' 2>&1)" ||
    die "настройки $CONFIGMAP не прочитаны: $(head -1 <<<"$cfg")" 3
  v="$(python3 -c '
import sys, yaml
c = yaml.safe_load(sys.stdin) or {}
v = ((c.get("authn") or {}).get("login") or {}).get("address-attempts")
print("" if v is None else int(v))
' <<<"$cfg")" || die "настройки $CONFIGMAP не разобраны" 3
  [ -n "$v" ] || die "в настройках $CONFIGMAP нет authn.login.address-attempts" 3
  echo "$v"
}

case "$CMD" in
  set)     cmd_set "$ARG" ;;
  restore) cmd_restore ;;
  cell)    cmd_cell "$ARG" ;;
  errors)  cmd_errors "$ARG" ;;
  limit)   cmd_limit ;;
  write)   cmd_write ;;
  self-test) usage ;;
esac
