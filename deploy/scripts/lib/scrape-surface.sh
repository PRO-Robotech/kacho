#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# scrape-surface.sh — ОДНО чтение адреса поверхности величин из ОБЪЯВЛЕНИЯ СБОРА
# пода и ОДИН способ к ней обратиться. Файл СОРСИТСЯ.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАЧЕМ ОДИН ФАЙЛ (задачи #2165, #2171)
#
# Адрес поверхности у процесса объявлен ОДИН раз — аннотациями сбора его пода
# (`prometheus.io/port`, `prometheus.io/scheme`, `prometheus.io/path`): это то
# место, которое говорит собирателю, куда идти. Читатели адреса в дереве были
# разные и читали его по-разному:
#   • гейт поверхностей (assert-metrics-surfaces-answer.sh) — порт и схему из
#     объявления, путь выписан;
#   • приборы нагрузки (load-tests/*-rps-step.sh) — НИЧЕГО: адрес
#     `http://127.0.0.1:9095/metrics` выписан целиком, опрос шёл `wget` в образе
#     САМОЙ службы. Слушатель под TLS им недоступен by construction, а отказ
#     частично ТИХИЙ: один счётчик печатал NA, снимок пула не печатал ничего.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ЗДЕСЬ РЕШЕНО
#
#  • СХЕМА. Незаданная аннотация означает `http` — это умолчание САМОГО
#    объявления сбора (агент, аннотации не нашедший, идёт открытым текстом); оно
#    возвращается ОТДЕЛЬНО («умолчание объявления»), чтобы «объявлено http» было
#    отличимо от «не объявлено ничего». Значение вне пары http|https — отказ:
#    по такому адресу не придёт и агент. ПУТЬ: незаданный — `/metrics`, то же
#    умолчание объявления.
#  • АДРЕС — адрес ПОДА из его статуса, а не петля. Изнутри контейнера петля
#    доступна, но сертификат внутреннего центра выписан на имена службы, петли
#    среди них нет; и главное — опрос изнутри чужого контейнера зависит от того,
#    какой инструмент попал в ЕГО образ (в образе края `curl` нет, `wget` есть;
#    умеет ли `wget` образа https — неизвестно). Эту зависимость гейт
#    поверхностей уже снял своим пробником; здесь пробник общий.
#  • ПРОВЕРКА СЕРТИФИКАТА СЕРВЕРА по `https` ОСЛАБЛЕНА (`curl --insecure`), и это
#    ОБЪЯВЛЕННОЕ ослабление: корней доверия внутреннего центра в своём поде-
#    пробнике не смонтировано, а монтировать их значило бы знать имя секрета
#    каждого стенда — второе объявление о посадке. Предмет читателей — «что
#    отвечает поверхность», а не «верна ли цепочка»; посадку транспорта
#    утверждает assert-production-posture.sh по строке самого процесса.
#    Вызывающий обязан ПЕЧАТАТЬ ослабление в своей переписи, когда оно
#    применялось (счётчик SCRAPE_INSECURE_USED).
#
# Использование:
#   . "$(dirname "$0")/lib/scrape-surface.sh"
#   scrape_probe_start "$NS" || …            # код 2 — пробник не поднялся
#   trap scrape_probe_stop EXIT
#   scrape_decl_pod "$NS" "$pod"             # → SCRAPE_IP SCRAPE_PORT SCRAPE_SCHEME_DECL SCRAPE_PATH_DECL
#   scrape_decl_selector "$NS" "$selector"   # то же, первый под по метке; + SCRAPE_POD
#   scrape_resolve || …                      # → SCRAPE_SCHEME SCRAPE_SCHEME_SOURCE SCRAPE_PATH SCRAPE_URL
#   scrape_fetch                             # → SCRAPE_BODY SCRAPE_CODE (код 000 — ответа нет)
#
# Опции оболочки здесь НЕ выставляются: файл подключают скрипты с разными `set`.
#
# Переменные SCRAPE_* — ВЫХОДЫ для вызывающего, в этом файле они только
# назначаются; анализатор оболочки видит их «неиспользуемыми» и неправ.
# shellcheck disable=SC2034

SCRAPE_PROBE_IMAGE="${PROBE_IMAGE:-docker.io/alpine/k8s:1.36.2}"
SCRAPE_PROBE_POD=""
SCRAPE_PROBE_NS=""
SCRAPE_INSECURE_USED=0

# scrape_probe_start <ns> [срок жизни, с] — поднять СВОЙ под с инструментом
# опроса. Код 2 — пробник не поднялся: кластер недоступен либо образ не тянется.
# Это условие прогона, а не вердикт о поверхностях.
#
# Срок жизни — не ожидание, а предел, после которого под уходит сам, если
# вызывающего убили раньше, чем сработала уборка. Гейту хватает умолчания; прибор
# нагрузки передаёт бюджет своего прогона.
scrape_probe_start() {
  local ns="$1" life="${2:-600}" name="kacho-metrics-probe-$$"
  kubectl -n "$ns" run "$name" --image="$SCRAPE_PROBE_IMAGE" --restart=Never \
    --command -- sleep "$life" >/dev/null 2>&1 || true
  if kubectl -n "$ns" wait --for=condition=Ready "pod/$name" --timeout=90s >/dev/null 2>&1; then
    SCRAPE_PROBE_POD="$name"; SCRAPE_PROBE_NS="$ns"
    return 0
  fi
  return 2
}

scrape_probe_stop() {
  [ -n "$SCRAPE_PROBE_POD" ] || return 0
  kubectl -n "$SCRAPE_PROBE_NS" delete pod "$SCRAPE_PROBE_POD" --wait=false >/dev/null 2>&1 || true
  SCRAPE_PROBE_POD=""
}

# _scrape_split <строка «имя|ip|порт|схема|путь»> — поля ОДНОГО обращения.
# Разделитель `|`, а не пробел: незаданная аннотация даёт ПУСТОЕ поле, и при
# разборе по пробелам значение следующего поля заняло бы место пустого
# (незаданный порт при заданной схеме дал бы «порт = https»).
_scrape_split() {
  IFS='|' read -r SCRAPE_POD SCRAPE_IP SCRAPE_PORT SCRAPE_SCHEME_DECL SCRAPE_PATH_DECL <<<"$1"
}

_SCRAPE_JSONPATH='{.metadata.name}|{.status.podIP}|{.metadata.annotations.prometheus\.io/port}|{.metadata.annotations.prometheus\.io/scheme}|{.metadata.annotations.prometheus\.io/path}'

# scrape_decl_pod <ns> <pod> — объявление сбора пода по имени. Все поля ОДНИМ
# обращением: по отдельности ip одного экземпляра встал бы рядом с портом
# другого.
scrape_decl_pod() {
  local line
  line="$(kubectl -n "$1" get pod "$2" -o jsonpath="$_SCRAPE_JSONPATH" 2>/dev/null || true)"
  _scrape_split "$line"
}

# scrape_decl_selector <ns> <селектор> — то же у первого пода по метке.
scrape_decl_selector() {
  local line
  line="$(kubectl -n "$1" get pods -l "$2" \
    -o jsonpath="{.items[0].metadata.name}|{.items[0].status.podIP}|{.items[0].metadata.annotations.prometheus\.io/port}|{.items[0].metadata.annotations.prometheus\.io/scheme}|{.items[0].metadata.annotations.prometheus\.io/path}" \
    2>/dev/null || true)"
  _scrape_split "$line"
}

# scrape_resolve — адрес из прочитанного объявления. Коды:
#   0 — адрес собран;
#   1 — объявление называет схему вне пары http|https (вердикт о поде: по такому
#       адресу не придёт и агент); SCRAPE_REFUSAL называет причину;
#   2 — пода или объявления сбора нет (ip/порт пусты) — опрашивать некого.
scrape_resolve() {
  SCRAPE_REFUSAL=""
  if [ -z "${SCRAPE_IP:-}" ] || [ -z "${SCRAPE_PORT:-}" ]; then
    SCRAPE_REFUSAL="под не найден либо объявления сбора у него нет (ip='${SCRAPE_IP:-}' port='${SCRAPE_PORT:-}')"
    return 2
  fi
  case "${SCRAPE_SCHEME_DECL:-}" in
    "")         SCRAPE_SCHEME=http; SCRAPE_SCHEME_SOURCE="умолчание объявления сбора" ;;
    http|https) SCRAPE_SCHEME="$SCRAPE_SCHEME_DECL"; SCRAPE_SCHEME_SOURCE="объявлена" ;;
    *)
      SCRAPE_REFUSAL="объявление сбора называет схему '$SCRAPE_SCHEME_DECL' — обращения такой схемой не бывает, по этому адресу не придёт и агент"
      return 1 ;;
  esac
  SCRAPE_PATH="${SCRAPE_PATH_DECL:-/metrics}"
  SCRAPE_URL="$SCRAPE_SCHEME://$SCRAPE_IP:$SCRAPE_PORT$SCRAPE_PATH"
  return 0
}

# scrape_fetch — обращение пробником по SCRAPE_URL. Кладёт тело в SCRAPE_BODY и
# код ответа в SCRAPE_CODE; «000» — ответа не было (не та схема, некуда).
# SCRAPE_SILENT=1 — опрос не дал НИ БАЙТА (нечем выполнить обращение): это
# «не выполнилось», а не ответ «000», и вызывающий обязан их различать.
scrape_fetch() {
  local out insecure=()
  SCRAPE_BODY=""; SCRAPE_CODE="000"; SCRAPE_SILENT=1
  [ -n "$SCRAPE_PROBE_POD" ] || return 0
  if [ "$SCRAPE_SCHEME" = https ]; then
    insecure=(--insecure)
    SCRAPE_INSECURE_USED=$((SCRAPE_INSECURE_USED + 1))
  fi
  out="$(kubectl -n "$SCRAPE_PROBE_NS" exec "$SCRAPE_PROBE_POD" -- \
    curl -s --max-time 5 "${insecure[@]}" -w '\n%{http_code}' "$SCRAPE_URL" 2>/dev/null || true)"
  [ -n "$out" ] || return 0
  SCRAPE_SILENT=0
  SCRAPE_CODE="${out##*$'\n'}"
  SCRAPE_BODY="${out%$'\n'*}"
  [ "$SCRAPE_BODY" = "$out" ] && SCRAPE_BODY=""
  return 0
}
