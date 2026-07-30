#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ФОРМА ПОДА АДМИНИСТРАТИВНОГО ПЕРЕХОДА: вопрос о готовности обязан иметь предмет,
# а числа, которые обязаны совпадать, обязаны совпадать.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ ПЕРВЫЙ: ГОТОВНОСТЬ ЗАКРЫТА В ОБЕ СТОРОНЫ — И ЭТО НИКЕМ НЕ УТВЕРЖДАЛОСЬ
#
# Свойство, на котором держится весь переход: проба идёт ЧЕРЕЗ соседа К ЭНДПОИНТУ
# ЗДОРОВЬЯ ПРОВАЙДЕРА, и у соседа НЕТ статического ответа на этот путь. Тогда
# готовность краснеет и когда мёртв сосед (соединение не установится), и когда
# повис провайдер (апстрим молчит).
#
# В дереве это было сделано верно — и не защищено НИЧЕМ: замена пути на
# статический, порта или схемы проходила зелёной у всех гейтов. Между тем именно
# статический путь здоровья — первый инстинкт следующего читателя: так сделано в
# образце, с которого списан приём, и комментарий рядом предостерегает от этого
# ПРОЗОЙ. Проза не краснеет.
#
# Цена ошибки не «менее строгая проверка», а немой сбой: под рапортует готовность
# при повисшем провайдере, оставаясь конечной точкой ОБОИХ сервисов, а отказ
# интроспекции классифицируется как временный — то есть проверка отзыва токена
# перестаёт исполняться, и об этом никто не узнаёт.
#
# ОТРИЦАНИЕ ЗАСЧИТЫВАЕТСЯ ТОЛЬКО В ПАРЕ С ПОЛОЖИТЕЛЬНЫМ НА ТОМ ЖЕ АДРЕСЕ (то же
# правило, что в живом гейте перехода): «статического ответа нет» само по себе
# зеленеет и на пустой конфигурации, и на переименованном файле. Поэтому рядом
# требуется положительное: путь пробы ПОКРЫТ location'ом, который проксирует на
# петлю к листенеру провайдера.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ ВТОРОЙ: РАССИНХРОН ЧИСЕЛ, КОТОРЫЙ НЕ ПЕРЕКАТЫВАЕТ ПОД
#
# Номер листенера провайдера живёт в ЕГО конфигурации, а порт, на который сосед
# проксирует, — в конфигурации СОСЕДА. Совпадать они обязаны, и до этого гейта не
# совпадали бы молча: правка настроек соседа даёт новый объект настроек и
# ПОБАЙТОВО ТОТ ЖЕ шаблон пода, поэтому переката нет, nginx читает конфигурацию
# один раз при старте — и живой терминатор остаётся со старой. Расхождение
# детонирует при следующем НЕСВЯЗАННОМ рестарте, оторванном от вызвавшей его
# правки.
#
# Здесь сверяются ВСЕ пары чисел, которые обязаны быть одним числом, и каждая
# сторона читается из РЕНДЕРА, а не из константы в тексте гейта: константа
# разошлась бы с рендером так же молча.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ ТРЕТИЙ: ПРИВЯЗКА СОДЕРЖИМОГО К ШАБЛОНУ ПОДА — ДОКАЗУЕМАЯ, А НЕ СЧЁТНАЯ
#
# Классовый гейт привязки (config-rollout-binding-test.sh) считает аннотации и
# по устройству не доказывает, что аннотация хэширует ИМЕННО свой объект настроек.
# Здесь доказывается: цифровой отпечаток конфигурации соседа лежит в спецификации
# САМОГО СОСЕДА, и гейт СВЕРЯЕТ его с sha256 сохранённого содержимого. Совпало —
# значит правка конфигурации меняет шаблон пода ПО ПОСТРОЕНИЮ.
#
# Плюс поведенческая половина: рендер повторяется с изменённым формирующим
# значением, и шаблон пода ОБЯЗАН стать другим.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ ЧЕТВЁРТЫЙ: САМОЛЕЧЕНИЕ И ОКНО НА ОСТАНОВКУ
#
# Проба живости: упавший процесс перезапустит kubelet сам, ЗАВИСШИЙ — нет, и под
# остаётся неготовым бессрочно, унося конечные точки обоих сервисов. В пути
# готовности теперь ДВА способных зависнуть компонента вместо одного.
#
# Предостановочный хук: при удалении пода сигнал приходит контейнерам сразу, а
# снятие конечных точек идёт асинхронно — на хвосте КАЖДОГО переката запросы ещё
# приезжают в умирающий под. Для проверки отзыва токена это ровно тот тихий
# непроход, который никем не считается.
#
# Офлайновая manifest-проверка (kind-кластер не нужен), как и остальные tests/helm/*.
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$DEPLOY_ROOT/helm/umbrella"

FAILURES=0
ASSERTIONS=0
fail() { echo "  ✗ $1"; FAILURES=$((FAILURES + 1)); }
ok()   { echo "  ✓ $1"; }
note() { echo "    $1"; }
assertion() { ASSERTIONS=$((ASSERTIONS + 1)); }

SIDECAR_NAME="${SIDECAR_NAME:-admin-tls}"
PROVIDER_NAME="${PROVIDER_NAME:-hydra}"

# ═════════════════════════════════════════════════════════════════════════════
# ПРЕДИКАТ — ЧИСТАЯ ФУНКЦИЯ НАД ТЕКСТОМ РЕНДЕРА.
#
# Печатает строки `KEY=value`, чтобы самопроверка могла скормить ему
# синтетический рендер БЕЗ helm и сверить исход.
# ═════════════════════════════════════════════════════════════════════════════
analyze_render() {
  python3 - "$1" "$SIDECAR_NAME" "$PROVIDER_NAME" <<'PY'
import sys, yaml, hashlib, re

path, sidecar_name, provider_name = sys.argv[1], sys.argv[2], sys.argv[3]
with open(path) as fh:
    docs = [d for d in yaml.safe_load_all(fh) if isinstance(d, dict)]

out = []
def emit(k, v):
    out.append("%s=%s" % (k, v))

# ── под провайдера ───────────────────────────────────────────────────────────
workload = None
for d in docs:
    if d.get("kind") not in ("Deployment", "StatefulSet"):
        continue
    tmpl = ((d.get("spec") or {}).get("template") or {})
    labels = (tmpl.get("metadata") or {}).get("labels") or {}
    if labels.get("app.kubernetes.io/name") == provider_name:
        workload = d
        break

if workload is None:
    emit("PROVIDER_POD", "")
    print("\n".join(out))
    sys.exit(0)

emit("PROVIDER_POD", (workload.get("metadata") or {}).get("name", "?"))
podspec = ((workload.get("spec") or {}).get("template") or {}).get("spec") or {}
containers = {c.get("name"): c for c in (podspec.get("containers") or [])}
emit("CONTAINERS", ",".join(sorted(containers)))

def port_named(c, name):
    for p in c.get("ports") or []:
        if p.get("name") == name:
            return str(p.get("containerPort", ""))
    return ""

def probe_shape(c, key):
    p = c.get(key)
    if not p:
        return "none"
    if "httpGet" in p:
        g = p["httpGet"]
        return "httpGet:%s:%s:%s" % (
            (g.get("scheme") or "HTTP"), g.get("port", ""), g.get("path", ""))
    if "tcpSocket" in p:
        return "tcpSocket:%s" % (p["tcpSocket"].get("port", ""),)
    if "exec" in p:
        return "exec"
    if "grpc" in p:
        return "grpc:%s" % (p["grpc"].get("port", ""),)
    return "unknown"

def prestop(c):
    hook = ((c.get("lifecycle") or {}).get("preStop") or {})
    if not hook:
        return "none"
    return ",".join(sorted(hook))

sc = containers.get(sidecar_name)
if sc is None:
    emit("SIDECAR_PORT", "")
    emit("SIDECAR_LIVENESS", "absent-container")
    emit("SIDECAR_PRESTOP", "absent-container")
    emit("SIDECAR_CONF_DIGEST", "")
else:
    ports = [str(p.get("containerPort", "")) for p in (sc.get("ports") or [])]
    emit("SIDECAR_PORT", ports[0] if ports else "")
    emit("SIDECAR_LIVENESS", probe_shape(sc, "livenessProbe"))
    emit("SIDECAR_PRESTOP", prestop(sc))
    digest = ""
    for e in sc.get("env") or []:
        if str(e.get("name", "")).endswith("CONF_SHA256"):
            digest = str(e.get("value", ""))
    emit("SIDECAR_CONF_DIGEST", digest)
    lim = ((sc.get("resources") or {}).get("limits") or {})
    emit("SIDECAR_CPU_LIMIT", str(lim.get("cpu", "")))

pc = containers.get(provider_name)
if pc is None:
    emit("PROVIDER_ADMIN_PORT", "")
    emit("PROVIDER_PUBLIC_PORT", "")
    emit("PROVIDER_LIVENESS", "absent-container")
    emit("PROVIDER_PRESTOP", "absent-container")
    emit("READY_PROBE", "none")
    emit("START_PROBE", "none")
else:
    emit("PROVIDER_ADMIN_PORT", port_named(pc, "http-admin"))
    emit("PROVIDER_PUBLIC_PORT", port_named(pc, "http-public"))
    emit("PROVIDER_LIVENESS", probe_shape(pc, "livenessProbe"))
    emit("PROVIDER_PRESTOP", prestop(pc))
    emit("READY_PROBE", probe_shape(pc, "readinessProbe"))
    emit("START_PROBE", probe_shape(pc, "startupProbe"))

# ── конфигурация соседа ──────────────────────────────────────────────────────
CONF_KEY = "admin-tls.conf"
conf = None
conf_name = ""
for d in docs:
    if d.get("kind") != "ConfigMap":
        continue
    data = d.get("data") or {}
    if CONF_KEY in data:
        conf = data[CONF_KEY]
        conf_name = (d.get("metadata") or {}).get("name", "?")
if conf is None:
    emit("CONF_PRESENT", "no")
else:
    emit("CONF_PRESENT", "yes")
    emit("CONF_NAME", conf_name)
    emit("CONF_SHA", hashlib.sha256(conf.encode()).hexdigest())
    # Комментарии nginx выбрасываются ДО разбора: запрет обязан читать
    # исполняемую часть, а не текст, объясняющий этот же запрет.
    body = re.sub(r"(?m)#.*$", "", conf)
    m = re.search(r"\blisten\s+(\d+)([^;]*);", body)
    emit("CONF_LISTEN", m.group(1) if m else "")
    emit("CONF_LISTEN_SSL", "yes" if (m and "ssl" in m.group(2)) else "no")
    up = re.search(r"\bproxy_pass\s+http://([^;\s]+)\s*;", body)
    emit("CONF_UPSTREAM", up.group(1) if up else "")

    # location-блоки: вырезаются по балансу скобок, чтобы классифицировать тело.
    locs = []
    for m in re.finditer(r"\blocation\s+([^{]+?)\s*\{", body):
        start = m.end() - 1
        depth, i = 0, start
        while i < len(body):
            if body[i] == "{":
                depth += 1
            elif body[i] == "}":
                depth -= 1
                if depth == 0:
                    break
            i += 1
        block = body[start + 1:i]
        static = bool(re.search(r"\b(return|root|alias|index|stub_status|empty_gif)\b", block))
        proxied = bool(re.search(r"\bproxy_pass\b", block))
        kind = "static" if static else ("proxy" if proxied else "empty")
        locs.append((m.group(1).strip(), kind))
    emit("CONF_LOCATIONS", str(len(locs)))
    for match, kind in locs:
        emit("CONF_LOCATION", "%s|%s" % (match, kind))

# ── Service терминатора ──────────────────────────────────────────────────────
svc = None
for d in docs:
    if d.get("kind") != "Service":
        continue
    labels = (d.get("metadata") or {}).get("labels") or {}
    if labels.get("app.kubernetes.io/component") == "hydra-admin-terminator":
        svc = d
if svc is None:
    emit("SVC_PRESENT", "no")
else:
    emit("SVC_PRESENT", "yes")
    for p in (svc.get("spec") or {}).get("ports") or []:
        emit("SVC_PORT", "%s:%s" % (p.get("port", ""), p.get("targetPort", "")))

# ── собственная конфигурация провайдера (его же ConfigMap) ───────────────────
for d in docs:
    if d.get("kind") != "ConfigMap":
        continue
    data = d.get("data") or {}
    if "hydra.yaml" not in data:
        continue
    try:
        cfg = yaml.safe_load(data["hydra.yaml"]) or {}
    except Exception:
        continue
    admin = ((cfg.get("serve") or {}).get("admin") or {})
    public = ((cfg.get("serve") or {}).get("public") or {})
    emit("CFG_ADMIN_HOST", str(admin.get("host", "")))
    emit("CFG_ADMIN_PORT", str(admin.get("port", "")))
    emit("CFG_PUBLIC_PORT", str(public.get("port", "")))

print("\n".join(out))
PY
}

val() { sed -n "s/^$2=//p" <<<"$1" | head -1; }

# ═════════════════════════════════════════════════════════════════════════════
# УТВЕРЖДЕНИЯ НАД ОДНИМ РЕНДЕРОМ — общая функция для основного прохода и
# самопроверки, чтобы инъекция проверяла ТУ ЖЕ логику, что и боевой проход.
# ═════════════════════════════════════════════════════════════════════════════
assert_shape() { # <метка> <вывод предиката>
  local tag="$1" info="$2"
  local sidecar_port conf_listen conf_upstream conf_up_host conf_up_port
  local ready start prov_admin prov_public locs svc_port digest conf_sha

  sidecar_port="$(val "$info" SIDECAR_PORT)"
  conf_listen="$(val "$info" CONF_LISTEN)"
  conf_upstream="$(val "$info" CONF_UPSTREAM)"
  conf_up_host="${conf_upstream%%:*}"
  conf_up_port="${conf_upstream##*:}"
  ready="$(val "$info" READY_PROBE)"
  start="$(val "$info" START_PROBE)"
  prov_admin="$(val "$info" PROVIDER_ADMIN_PORT)"
  prov_public="$(val "$info" PROVIDER_PUBLIC_PORT)"
  locs="$(val "$info" CONF_LOCATIONS)"
  svc_port="$(val "$info" SVC_PORT)"
  digest="$(val "$info" SIDECAR_CONF_DIGEST)"
  conf_sha="$(val "$info" CONF_SHA)"

  # ── 1. ПОЛОЖИТЕЛЬНОЕ: путь пробы покрыт проксирующим location'ом ──────────
  #    Без этой половины «статического ответа нет» зеленело бы и на пустой
  #    конфигурации, и на переименованном ключе.
  assertion
  local ready_path proxy_all static_locs
  ready_path="${ready##*:}"
  proxy_all="$(grep -c '^CONF_LOCATION=/|proxy$' <<<"$info")"
  static_locs="$(grep -c '^CONF_LOCATION=.*|static$' <<<"$info")"
  if [ "$locs" = "1" ] && [ "$proxy_all" = "1" ]; then
    ok "[$tag] ПОЛОЖИТЕЛЬНОЕ: единственный location «/» проксирует — путь пробы «$ready_path» покрыт им"
  else
    fail "[$tag] путь пробы «$ready_path» НЕ покрыт единственным проксирующим location'ом"
    note "location'ов: ${locs:-0}, из них «/»-проксирующих: $proxy_all"
    note "проба пода направлена СЮДА; если её путь обслуживает не проксирующий блок,"
    note "готовность перестаёт зависеть от провайдера."
    grep '^CONF_LOCATION=' <<<"$info" | sed 's/^/      /'
  fi

  # ── 2. ОТРИЦАТЕЛЬНОЕ: статического ответа нет ни в одном location ─────────
  assertion
  if [ "$static_locs" = "0" ]; then
    ok "[$tag] ОТРИЦАТЕЛЬНОЕ: статического ответа у соседа нет ни на одном пути"
  else
    fail "[$tag] у соседа появился ответ, НЕ ЗАВИСЯЩИЙ от апстрима ($static_locs шт.)"
    note "это первый инстинкт: в образце, с которого списан приём, путь здоровья"
    note "возвращал константу. Здесь это означает «под готов» при повисшем провайдере."
    grep '^CONF_LOCATION=.*|static$' <<<"$info" | sed 's/^/      /'
  fi

  # ── 3. Проба идёт К СОСЕДУ и ПО TLS, а путь — здоровья провайдера ─────────
  for pair in "готовности:$ready" "запуска:$start"; do
    local what got
    what="${pair%%:*}"; got="${pair#*:}"
    assertion
    case "$got" in
      httpGet:HTTPS:"$sidecar_port":/health/*)
        ok "[$tag] проба $what: HTTPS на порт соседа $sidecar_port, путь ${got##*:}" ;;
      none)
        fail "[$tag] пробы $what НЕТ — умолчание чарта бьёт по адресу пода в"
        note "административный порт, который слушает только петлю: под не станет готовым." ;;
      *)
        fail "[$tag] проба $what имеет вид «$got»"
        note "ожидалось httpGet:HTTPS:$sidecar_port:/health/… — через соседа, по TLS,"
        note "на путь здоровья ПРОВАЙДЕРА (не на путь, который сосед обслуживает сам)." ;;
    esac
  done

  # ── 4. Числа, которые обязаны быть ОДНИМ числом ───────────────────────────
  assertion
  if [ -n "$sidecar_port" ] && [ "$sidecar_port" = "$conf_listen" ]; then
    ok "[$tag] порт соседа в поде и в его конфигурации — одно число ($sidecar_port)"
  else
    fail "[$tag] порт соседа расходится: в поде «$sidecar_port», в конфигурации «$conf_listen»"
  fi

  assertion
  if [ -n "$conf_up_port" ] && [ "$conf_up_port" = "$prov_admin" ]; then
    ok "[$tag] апстрим соседа и административный листенер провайдера — одно число ($conf_up_port)"
  else
    fail "[$tag] РАССИНХРОН ПОРТОВ: сосед проксирует на «$conf_up_port», листенер провайдера «$prov_admin»"
    note "правка одной стороны не перекатывает под: конфигурация читается один раз при"
    note "старте, поэтому дерево и работающая система разъедутся МОЛЧА — до следующего"
    note "несвязанного рестарта, который унесёт конечные точки обоих сервисов."
  fi

  assertion
  if [ "$conf_up_host" = "127.0.0.1" ]; then
    ok "[$tag] апстрим соседа — петля этого же пода ($conf_up_host)"
  else
    fail "[$tag] апстрим соседа не петля, а «$conf_up_host» — открытый участок покидает под"
  fi

  assertion
  if [ "$(val "$info" CFG_ADMIN_HOST)" = "127.0.0.1" ]; then
    ok "[$tag] листенер провайдера слушает только петлю"
  else
    fail "[$tag] листенер провайдера слушает «$(val "$info" CFG_ADMIN_HOST)», а не петлю"
  fi

  assertion
  if [ "$svc_port" = "$sidecar_port:$sidecar_port" ]; then
    ok "[$tag] Service терминатора целится в порт соседа ($svc_port)"
  else
    fail "[$tag] Service терминатора: «$svc_port», а сосед слушает «$sidecar_port»"
  fi

  # ── 5. Привязка содержимого к шаблону пода — ДОКАЗАННАЯ, не счётная ───────
  assertion
  if [ -z "$digest" ]; then
    fail "[$tag] в спецификации соседа НЕТ отпечатка его конфигурации"
    note "аннотацию шаблона пода через значения чарта провайдера добавить нельзя —"
    note "через шаблонизацию проходит только список дополнительных контейнеров."
    note "Поэтому отпечаток живёт в спецификации САМОГО соседа; без него правка"
    note "настроек даёт новый объект настроек и побайтово тот же шаблон пода."
  elif [ "$digest" = "$conf_sha" ]; then
    ok "[$tag] отпечаток в спецификации соседа = sha256 его конфигурации (${digest:0:12}…)"
  else
    fail "[$tag] отпечаток соседа «${digest:0:12}…» НЕ равен sha256 конфигурации «${conf_sha:0:12}…»"
    note "отпечаток, привязанный не к тому содержимому, — та же дыра, что и его отсутствие,"
    note "только незаметнее."
  fi

  # ── 6. Самолечение и окно на остановку — на КАЖДОМ контейнере пода ────────
  for pair in "сосед:$(val "$info" SIDECAR_LIVENESS)" "провайдер:$(val "$info" PROVIDER_LIVENESS)"; do
    local who got
    who="${pair%%:*}"; got="${pair#*:}"
    assertion
    if [ "$got" = "none" ]; then
      fail "[$tag] у контейнера «$who» НЕТ пробы живости"
      note "упавший процесс kubelet перезапустит сам, ЗАВИСШИЙ — нет: под останется"
      note "неготовым бессрочно, унося конечные точки обоих сервисов."
    elif [ "$got" = "absent-container" ]; then
      fail "[$tag] контейнера «$who» в поде нет вовсе"
    else
      ok "[$tag] проба живости «$who»: $got"
    fi
  done

  for pair in "сосед:$(val "$info" SIDECAR_PRESTOP)" "провайдер:$(val "$info" PROVIDER_PRESTOP)"; do
    local who got
    who="${pair%%:*}"; got="${pair#*:}"
    assertion
    if [ "$got" = "none" ] || [ "$got" = "absent-container" ]; then
      fail "[$tag] у контейнера «$who» НЕТ предостановочного хука"
      note "сигнал приходит контейнерам сразу, а снятие конечных точек идёт асинхронно —"
      note "на хвосте КАЖДОГО переката запросы ещё приезжают в умирающий под."
    else
      ok "[$tag] предостановочный хук «$who»: $got"
    fi
  done

  # ── 7. Проба живости провайдера не зависит от соседа ──────────────────────
  assertion
  local plive
  plive="$(val "$info" PROVIDER_LIVENESS)"
  case "$plive" in
    *":$sidecar_port:"*|*":$sidecar_port")
      fail "[$tag] проба живости провайдера идёт через порт СОСЕДА ($sidecar_port)"
      note "тогда сломанный сосед перезапускал бы провайдера — сцепление в ту сторону,"
      note "в которую его не хотели." ;;
    none|absent-container)
      note "[$tag] (проба живости провайдера отсутствует — учтено выше)"
      ok "[$tag] независимость пробы живости провайдера: предмета нет" ;;
    *":$prov_public:"*)
      ok "[$tag] проба живости провайдера идёт по его СОБСТВЕННОМУ листенеру ($prov_public)" ;;
    *)
      fail "[$tag] проба живости провайдера «$plive» — ни порт соседа, ни его публичный листенер ($prov_public)" ;;
  esac
}

# ═════════════════════════════════════════════════════════════════════════════
# САМОПРОВЕРКА — БЕЗ helm И БЕЗ КЛАСТЕРА.
#
# Инъекция в ОБЕ стороны: дефектная конструкция обязана краснеть И НАЗЫВАТЬ
# координату, законная конструкция ТОЙ ЖЕ ФОРМЫ — молчать.
# ═════════════════════════════════════════════════════════════════════════════
if [ "${1:-}" = "--self-test" ]; then
  echo "=== $SCRIPT --self-test: форма пода перехода, инъекции в обе стороны ==="
  rc=0; checked=0
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

  mk_render() { # <имя случая> [ключ=значение…]
    python3 - "$@" <<'PY'
import sys, hashlib, yaml

name = sys.argv[1]
opt = dict(kv.split("=", 1) for kv in sys.argv[2:])
g = lambda k, d: opt.get(k, d)

term_port   = g("term_port", "4445")
listen_port = g("listen_port", term_port)
prov_port   = g("prov_port", "4455")
up_port     = g("up_port", prov_port)
up_host     = g("up_host", "127.0.0.1")
ready_port  = g("ready_port", term_port)
ready_scheme= g("ready_scheme", "HTTPS")
ready_path  = g("ready_path", "/health/ready")
public_port = g("public_port", "4444")
extra_loc   = g("extra_loc", "")
digest_mode = g("digest", "true")
live_side   = g("live_sidecar", "tcp")
live_prov   = g("live_provider", "public")
prestop     = g("prestop", "both")
svc_port    = g("svc_port", term_port)

conf = """# СТАТИЧЕСКОГО ПУТИ ЗДОРОВЬЯ ЗДЕСЬ НЕТ: комментарий, объясняющий запрет,
# содержит слова return и location — гейт обязан читать исполняемую часть.
server {
  listen %s ssl;
  location / {
    proxy_pass http://%s:%s;
  }
%s}
""" % (listen_port, up_host, up_port, extra_loc)

sha = hashlib.sha256(conf.encode()).hexdigest()
if digest_mode == "false":
    env = []
elif digest_mode == "wrong":
    env = [{"name": "KACHO_ADMIN_TLS_CONF_SHA256", "value": "0" * 64}]
else:
    env = [{"name": "KACHO_ADMIN_TLS_CONF_SHA256", "value": sha}]

sidecar = {
    "name": "admin-tls",
    "image": "nginx",
    "ports": [{"name": "https-admin", "containerPort": int(term_port)}],
    "env": env,
    "resources": {"limits": {"memory": "64Mi"}},
}
if live_side == "tcp":
    sidecar["livenessProbe"] = {"tcpSocket": {"port": int(term_port)}}
if prestop in ("both", "sidecar"):
    sidecar["lifecycle"] = {"preStop": {"exec": {"command": ["/bin/sleep", "5"]}}}

provider = {
    "name": "hydra",
    "image": "hydra",
    "ports": [
        {"name": "http-public", "containerPort": int(public_port)},
        {"name": "http-admin", "containerPort": int(prov_port)},
    ],
    "readinessProbe": {"httpGet": {
        "path": ready_path, "port": int(ready_port), "scheme": ready_scheme}},
    "startupProbe": {"httpGet": {
        "path": "/health/ready", "port": int(term_port), "scheme": "HTTPS"}},
}
if live_prov == "public":
    provider["livenessProbe"] = {"httpGet": {
        "path": "/health/alive", "port": int(public_port), "scheme": "HTTP"}}
elif live_prov == "through-sidecar":
    provider["livenessProbe"] = {"httpGet": {
        "path": "/health/alive", "port": int(term_port), "scheme": "HTTPS"}}
if prestop in ("both", "provider"):
    provider["lifecycle"] = {"preStop": {"exec": {"command": ["/bin/sleep", "5"]}}}

docs = [
    {"apiVersion": "apps/v1", "kind": "Deployment",
     "metadata": {"name": "rel-hydra"},
     "spec": {"template": {
         "metadata": {"labels": {"app.kubernetes.io/name": "hydra"}},
         "spec": {"containers": [provider, sidecar]}}}},
    {"apiVersion": "v1", "kind": "ConfigMap",
     "metadata": {"name": "rel-hydra-admin-tls-nginx"},
     "data": {"admin-tls.conf": conf}},
    {"apiVersion": "v1", "kind": "ConfigMap",
     "metadata": {"name": "rel-hydra"},
     "data": {"hydra.yaml": yaml.safe_dump(
         {"serve": {"admin": {"host": "127.0.0.1", "port": int(prov_port)},
                    "public": {"port": int(public_port)}}})}},
    {"apiVersion": "v1", "kind": "Service",
     "metadata": {"name": "rel-hydra-admin-tls",
                  "labels": {"app.kubernetes.io/component": "hydra-admin-terminator"}},
     "spec": {"ports": [{"port": int(svc_port), "targetPort": int(svc_port)}]}},
]
with open(name, "w") as fh:
    yaml.safe_dump_all(docs, fh)
PY
  }

  run_case() { # <метка> <ожидание: silent|red> <подстрока-координата> <опции…>
    local label="$1" want="$2" needle="$3"; shift 3
    checked=$((checked + 1))
    local f="$tmp/case.yaml" info out got
    rm -f "$f"; mk_render "$f" "$@"
    info="$(analyze_render "$f")"
    # Подстановка исполняет assert_shape в ПОДОБОЛОЧКЕ, поэтому счётчики
    # родителя она не меняет — находки считаются по её собственному выводу.
    # (Первая редакция считала разницу счётчика и потому не различала НИ ОДНОЙ
    # инъекции: все пятнадцать «не покраснели». Гейт, доказывающий сам себя,
    # обязан быть проверен так же, как всё остальное.)
    out="$(assert_shape "self" "$info" 2>&1)"
    got="$(grep -c '✗' <<<"$out")"
    if [ "$want" = silent ]; then
      if [ "$got" -eq 0 ]; then echo "  ✓ $label — молчит (законная конструкция)"
      else echo "  ✗ $label — покраснел на ЗАКОННОЙ конструкции ($got):"; sed 's/^/      /' <<<"$out" | grep '✗' ; rc=1; fi
    else
      if [ "$got" -eq 0 ]; then
        echo "  ✗ $label — НЕ покраснел на внесённом дефекте"; rc=1
      elif ! grep -qF "$needle" <<<"$out"; then
        echo "  ✗ $label — покраснел ($got), но координату «$needle» не назвал:"; sed 's/^/      /' <<<"$out" | grep '✗'; rc=1
      else
        echo "  ✓ $label — краснеет и называет координату"
      fi
    fi
  }

  echo "-- законная конструкция обязана МОЛЧАТЬ --"
  run_case "эталонная форма" silent ""

  echo
  echo "-- инъекции: каждая обязана краснеть И назвать координату --"
  run_case "статический ответ на пути здоровья" red "НЕ ЗАВИСЯЩИЙ от апстрима" \
    extra_loc='  location = /health/ready {
    return 200 "ok";
  }
'
  run_case "проба ушла на путь, который сосед обслуживает сам" red "путь пробы" \
    ready_path=/nginx-health extra_loc='  location = /nginx-health {
    return 200 "ok";
  }
'
  run_case "проба по открытому тексту" red "проба готовности" ready_scheme=HTTP
  run_case "проба мимо соседа — прямо в порт провайдера" red "проба готовности" ready_port=4455
  run_case "порт соседа в поде и в конфигурации разошёлся" red "порт соседа расходится" listen_port=4446
  run_case "апстрим соседа и листенер провайдера разошлись" red "РАССИНХРОН ПОРТОВ" up_port=4456
  run_case "апстрим соседа покинул под" red "не петля" up_host=hydra-admin
  run_case "Service целится не в порт соседа" red "Service терминатора" svc_port=4446
  run_case "отпечатка конфигурации в спецификации соседа нет" red "НЕТ отпечатка" digest=false
  run_case "отпечаток привязан не к тому содержимому" red "НЕ равен sha256" digest=wrong
  run_case "у соседа нет пробы живости" red "«сосед» НЕТ пробы живости" live_sidecar=none
  run_case "у провайдера нет пробы живости" red "«провайдер» НЕТ пробы живости" live_provider=none
  run_case "проба живости провайдера идёт через соседа" red "через порт СОСЕДА" live_provider=through-sidecar
  run_case "предостановочного хука нет у соседа" red "«сосед» НЕТ предостановочного" prestop=provider
  run_case "предостановочного хука нет у провайдера" red "«провайдер» НЕТ предостановочного" prestop=sidecar

  echo
  echo "-- законные конструкции ТОЙ ЖЕ ФОРМЫ обязаны молчать --"
  # Слова `return` и `location` присутствуют в комментарии конфигурации ВСЕГДА
  # (см. mk_render) — если бы предикат читал текст, а не исполняемую часть,
  # эталонный случай выше уже покраснел бы. Это утверждение проверяется отдельно.
  checked=$((checked + 1))
  mk_render "$tmp/legit.yaml"
  if grep -q 'return' "$tmp/legit.yaml" && [ "$(val "$(analyze_render "$tmp/legit.yaml")" CONF_LOCATIONS)" = "1" ]; then
    ok "слово «return» в комментарии конфигурации предикат НЕ считает статическим ответом"
  else
    echo "  ✗ предикат читает текст, а не исполняемую часть"; rc=1
  fi
  # Иные порты — та же форма, другое число: коллизия чисел не должна маскировать.
  run_case "все числа сдвинуты согласованно" silent "" \
    term_port=8445 prov_port=8455 public_port=8444
  # Проба готовности на /health/alive — тоже путь здоровья ПРОВАЙДЕРА.
  run_case "готовность читает /health/alive" silent "" ready_path=/health/alive

  echo
  echo "-- отсутствие пода провайдера обязано быть отличимо от «всё хорошо» --"
  checked=$((checked + 1))
  printf 'apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n' >"$tmp/empty.yaml"
  if [ "$(val "$(analyze_render "$tmp/empty.yaml")" PROVIDER_POD)" = "" ]; then
    ok "под провайдера не найден — предикат говорит это прямо, а не молчит"
  else
    echo "  ✗ предикат выдумал под провайдера"; rc=1
  fi

  echo
  echo "инъекций и законных входов проверено: $checked"
  [ $rc -eq 0 ] && echo "PASS: $SCRIPT --self-test" || echo "FAIL: $SCRIPT --self-test"
  exit $rc
fi

# ═════════════════════════════════════════════════════════════════════════════
# ОСНОВНОЙ ПРОХОД
# ═════════════════════════════════════════════════════════════════════════════
command -v helm >/dev/null 2>&1 || { echo "FATAL: нужен helm"; exit 2; }
python3 -c 'import yaml' 2>/dev/null || { echo "FATAL: нужен python3 с PyYAML"; exit 2; }

# ПРОФИЛИ ПЕРЕЧИСЛЕНЫ ТЕМ ЖЕ СПИСКОМ, что и у соседнего гейта портов: расхождение
# составов было бы слепой зоной, о которой никто бы не узнал.
STACKS='dev:values.dev.yaml
dev-prod:values.dev.yaml,values.dev-prod.yaml
prod:values.prod.yaml
fe3455:values.prod.yaml,values.fe3455.yaml,values.fe3455-prod.yaml
prorobotech:values.dev.yaml,values.prorobotech.yaml'

echo "=== $SCRIPT: форма пода административного перехода ==="
work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
stacks_total=0; stacks_with_terminator=0

while IFS= read -r line; do
  [ -z "$line" ] && continue
  stack="${line%%:*}"; files="${line#*:}"
  stacks_total=$((stacks_total + 1))
  args=""
  IFS=','; for f in $files; do args="$args -f $UMBRELLA/$f"; done; unset IFS

  render="$work/$stack.yaml"
  # shellcheck disable=SC2086
  if ! (cd "$UMBRELLA" && helm template kacho-umbrella . $args --namespace kacho) >"$render" 2>"$work/$stack.err"; then
    assertion; fail "[$stack] рендер не собрался — утверждения НЕ ВЫПОЛНЕНЫ, а не чисты"
    sed 's/^/        /' "$work/$stack.err" | tail -4
    continue
  fi

  info="$(analyze_render "$render")"
  if [ "$(val "$info" CONF_PRESENT)" != "yes" ]; then
    echo "  -- [$stack] терминатор в профиле не включён — доказывать нечего"
    continue
  fi
  stacks_with_terminator=$((stacks_with_terminator + 1))
  echo
  echo "  ── [$stack] ──"
  assert_shape "$stack" "$info"
done <<<"$STACKS"

# ── ПОВЕДЕНЧЕСКАЯ ПОЛОВИНА: правка формирующего значения ОБЯЗАНА менять шаблон
#    пода. Структурная привязка выше доказывает совпадение отпечатка с
#    содержимым; здесь доказывается сам ПЕРЕКАТ — тем же способом, каким дефект
#    проявился бы на стенде.
#
#    КОНТРОЛЬНЫЙ СЛУЧАЙ ОБЯЗАТЕЛЕН, И ЭТО НЕ ПЕДАНТИЗМ. Первая редакция сравнивала
#    два рендера напрямую и отчиталась ЗЕЛЁНЫМ ещё ДО того, как привязка была
#    сделана: чарт провайдера чеканит свои секреты случайно при каждом рендере,
#    поэтому ЛЮБЫЕ два рендера различаются аннотацией их контрольной суммы.
#    Утверждение «шаблон изменился» было истинно всегда и не значило ничего.
#    Поэтому: третий рендер С ТЕМИ ЖЕ значениями калибрует шум (ключи, которые
#    различаются сами по себе, называются и исключаются), и только после этого
#    сравнение имеет смысл. Контроль обязан дать РАВНО, проба — РАЗЛИЧНО.
echo
echo "  ── перекат при правке настроек соседа (поведенческая половина) ──"
assertion
base="$work/roll-base.yaml"; ctrl="$work/roll-ctrl.yaml"; bumped="$work/roll-bumped.yaml"
render_prod() { # <файл> [доп. аргументы]
  local f="$1"; shift
  (cd "$UMBRELLA" && helm template kacho-umbrella . -f "$UMBRELLA/values.prod.yaml" \
     --namespace kacho "$@") >"$f" 2>/dev/null
}
if ! render_prod "$base" || ! render_prod "$ctrl" \
   || ! render_prod "$bumped" --set global.kacho.hydraAdminTls.upstreamTimeout=31s; then
  fail "рендер боевого профиля не собрался — перекат НЕ ПРОВЕРЕН, а не подтверждён"
else
  roll="$(python3 - "$base" "$ctrl" "$bumped" <<'PY'
import sys, yaml, json

def pod_template(path):
    for d in yaml.safe_load_all(open(path)):
        if not isinstance(d, dict) or d.get("kind") != "Deployment":
            continue
        t = ((d.get("spec") or {}).get("template") or {})
        if (t.get("metadata") or {}).get("labels", {}).get("app.kubernetes.io/name") == "hydra":
            return t
    return None

base, ctrl, bumped = (pod_template(p) for p in sys.argv[1:4])
if base is None or ctrl is None or bumped is None:
    print("RESULT=no-pod-template")
    sys.exit(0)

def ann(t):
    return ((t.get("metadata") or {}).get("annotations") or {})

# Шум калибруется контрольным рендером, а не угадывается списком имён.
noisy = sorted(k for k in set(ann(base)) | set(ann(ctrl)) if ann(base).get(k) != ann(ctrl).get(k))
for t in (base, ctrl, bumped):
    for k in noisy:
        ann(t).pop(k, None)
print("NOISY=%s" % (",".join(noisy) or "-"))

j = lambda t: json.dumps(t, sort_keys=True)
print("CONTROL=%s" % ("equal" if j(base) == j(ctrl) else "differs"))
print("BUMPED=%s" % ("differs" if j(base) != j(bumped) else "equal"))
PY
)"
  noisy="$(val "$roll" NOISY)"
  control="$(val "$roll" CONTROL)"
  bumped_r="$(val "$roll" BUMPED)"
  note "нестабильные сами по себе аннотации (исключены по контрольному рендеру): ${noisy:--}"
  if [ "$(val "$roll" RESULT)" = "no-pod-template" ]; then
    fail "шаблон пода провайдера не найден в одном из рендеров — сравнивать НЕЧЕГО"
  elif [ "$control" != "equal" ]; then
    fail "КОНТРОЛЬ не сошёлся: два рендера с ОДНИМИ значениями дали разные шаблоны пода"
    note "значит сравнение ничего не различает, и «шаблон изменился» истинно всегда."
  elif [ "$bumped_r" = "differs" ]; then
    ok "контроль равен, а правка формирующего значения меняет шаблон пода → под перекатится"
  else
    fail "правка настроек соседа НЕ изменила шаблон пода — переката не будет"
    note "nginx читает конфигурацию ОДИН РАЗ при старте: живой терминатор останется"
    note "со старой, а любой гейт, читающий рендер, отчитается новым значением."
  fi
fi

echo
echo "── объём осмотренного ──"
echo "  стеков отрендерено: $stacks_total; из них с терминатором: $stacks_with_terminator"
if [ "$stacks_with_terminator" -eq 0 ]; then
  echo
  echo "FAIL: терминатор не включён НИ В ОДНОМ профиле — проверять было нечего."
  echo "      Пустой результат НЕ означает «всё хорошо»."
  exit 1
fi

echo
echo "=== вердикт: утверждений $ASSERTIONS, находок $FAILURES ==="
if [ "$ASSERTIONS" -eq 0 ]; then
  echo "FAIL: не выполнено НИ ОДНОГО утверждения — это провал, а не чистота."
  exit 1
fi
[ "$FAILURES" -ne 0 ] && { echo "FAIL: $SCRIPT"; exit 1; }
echo "PASS: $SCRIPT"
