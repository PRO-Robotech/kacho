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
# без переката не меняет ничего: `rollout restart`, `rollout status`, затем
# готовность края (`wait-edge-ready.sh` — край отвечает /readyz только когда
# отвечает служба доступа). Число ключей — из строки самоотчёта старта
# «second-factor wrapping keys declared» каждого живого пода.
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
#   3 — условие не создано на кластере: перекат не стал готов, край не ответил,
#       самоотчёта старта нет, счётчик не прочитан.
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
  sed -n '7,25p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
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

CTX=""
pick_context() {
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

k() { command kubectl --context "$CTX" -n "$NS" "$@"; }

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
  edge_ready || die "край не готов после переката $DEPLOY (выше)" 3
}

# edge_ready — готовность края тем же судьёй, что у подъёма стенда, с файлом
# конфигурации, в котором выбранный контекст единственный.
edge_ready() {
  local cfg rc
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

cmd_set() {
  local keys="$1" value
  need_state
  pick_context
  load_target
  # Перечень проверяется до первой записи: ошибочное имя не трогает ни секрета, ни состояния.
  case ",$keys," in *,,*) die "перечень «$keys»: пустое имя" ;; esac
  [ -n "$keys" ] || die "перечень пуст: назови K1 и/или K2 через запятую"
  local n; for n in ${keys//,/ }; do case "$n" in K1|K2) ;; *) die "перечень «$keys»: имя «$n» вне словаря K1, K2" ;; esac; done
  prepare_keys
  value="$(mktemp "$STATE/value.XXXXXX")" || die "файл величины не заведён"
  (compose "$keys" >"$value") || { rm -f "$value"; exit 2; }
  log "секрет $SECRET ← перечень $keys; перекат $DEPLOY в $NS (контекст $CTX)"
  apply_value "$value"
  rm -f "$value"
  report "$keys"
}

cmd_restore() {
  need_state
  if [ ! -s "$STATE/K1" ]; then
    log "ключ стенда до пробы не сохранялся — восстанавливать нечего"
    rm -rf "${STATE:?}"/* 2>/dev/null
    return 0
  fi
  pick_context
  load_target
  log "секрет $SECRET ← ключ стенда до пробы; перекат $DEPLOY в $NS (контекст $CTX)"
  apply_value "$STATE/K1"
  report "K1"
  rm -rf "${STATE:?}"/*
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
  self-test) usage ;;
esac
