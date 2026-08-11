#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ЖИВОЙ ГЕЙТ АДМИНИСТРАТИВНОГО ПЕРЕХОДА: шифрование подтверждает сторона, не
# участвующая в собственной оценке.
#
# ─────────────────────────────────────────────────────────────────────────────
# ГЛАВНОЕ ПРАВИЛО: ОТРИЦАТЕЛЬНОЕ УТВЕРЖДЕНИЕ ДЕЙСТВИТЕЛЬНО ТОЛЬКО В ПАРЕ С
# ОДНОВРЕМЕННЫМ ПОЛОЖИТЕЛЬНЫМ НА ТОМ ЖЕ АДРЕСЕ.
#
# Мёртвый сосед даёт ТОТ ЖЕ отказ соединения, что и работающий TLS-листенер на
# плейнтекстовый запрос. Поэтому одиночное «плейнтекст отвергнут» зеленеет
# СИЛЬНЕЕ ВСЕГО ровно тогда, когда сломано всё: всеобщий отказ читается как
# «всё запрещено правильно» — немой сбой, строго хуже аварии, потому что о нём
# никто не узнает.
#
# То же и для двух других отрицаний:
#   • «снаружи пода не дотянуться» зачитывается ТОЛЬКО вместе с достижимостью
#     из сетевого пространства САМОГО пода (иначе увод сломал переход, а не
#     защитил его — и выглядит это одинаково);
#   • «сертификата нет ⇒ отказ» зачитывается ТОЛЬКО вместе с наблюдением, что
#     сосед при этом НЕ Ready (иначе «сертификата нет» неотличимо от «стенд не
#     поднят»).
#
# У КЛАССИФИКАТОРА ИСХОДОВ НЕТ КОРЗИНЫ «ВСЁ ОСТАЛЬНОЕ». Такая корзина всегда
# становится корзиной «сломано, но выглядит правильно». Неузнанный исход — это
# `unclassified`, и он КРАСНЫЙ, с печатью сырого наблюдения: гейт, который не
# понял, что видит, не вправе это засчитывать.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ПРОБНИК, А НЕ ПОТРЕБИТЕЛЬ
#
# Потребитель (iam, шлюз) — сторона ПОД ОЦЕНКОЙ. Его успешный вызов доказывает
# работоспособность, но не независимость свидетельства. Пробник не несёт ни одной
# настройки продукта: он получает только якорь доверия и адрес.
#
# Серверная половина — журнал соседа: о шифровании сообщает ДАЛЬНИЙ КОНЕЦ
# соединения, и сообщение производит транспортный стек, а не значение в
# настройках. Это тот же приём, что `pg_stat_ssl` в секции B соседнего гейта.
# Где аналогия слабее: у листенера, слушающего ТОЛЬКО TLS, поле протокола непусто
# по построению, поэтому утверждение было бы вакуумным — закрыто тем, что
# предикат вердикта вынесен и в самопроверке кормится журналами, где поле пусто,
# отсутствует или запись не та.
#
# ЖИВАЯ ИНЪЕКЦИЯ — ОТДЕЛЬНЫЙ ШАГ (deploy/scripts/inject-admin-hop-defects.sh):
# она мутирует стенд и требует монопольного доступа, поэтому в состав гейта
# посадки не входит.
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NS="${NS:-kacho}"
PROBE_IMAGE="${PROBE_IMAGE:-docker.io/alpine/k8s:1.36.2}"

FAILED=0
ASSERTIONS=0
fail() { echo "  ✗ $*"; FAILED=1; }
ok()   { echo "  ✓ $*"; }
note() { echo "    $*"; }
assertion() { ASSERTIONS=$((ASSERTIONS + 1)); }

# ═════════════════════════════════════════════════════════════════════════════
# ПРЕДИКАТЫ ВЕРДИКТА — ЧИСТЫЕ ФУНКЦИИ НАД ТЕКСТОМ.
#
# Вынесены из кластерной половины затем, чтобы самопроверка могла скормить им
# синтетические наблюдения БЕЗ кластера и потребовать покраснеть на каждом
# дефектном и промолчать на законном той же формы.
# ═════════════════════════════════════════════════════════════════════════════

# classify_connection <вывод попытки соединения> → ровно один именованный исход.
#
# Порядок проверок — от частного к общему: подпись «плейнтекст отправлен на
# TLS-порт» ДОЛЖНА проверяться раньше общего «пришёл ответ приложения», иначе
# отказ терминатора (он отвечает 400 и НАЗЫВАЕТ причину) был бы засчитан как
# ответ административного API.
classify_connection() {
  local out="$1"
  # (1) мы говорили плейнтекстом в TLS-листенер — он назвал это прямо.
  if grep -qiE 'plain HTTP request was sent to HTTPS port' <<<"$out"; then
    echo tls-rejected-plaintext; return
  fi
  # (2) удалённый конец ответил плейнтекстом на рукопожатие TLS.
  if grep -qiE 'wrong version number|record layer failure|unknown protocol' <<<"$out"; then
    echo plaintext-answer; return
  fi
  # (3) рукопожатие не прошло проверку цепочки — это НЕ «нет TLS», это
  #     «TLS есть, доверия нет». Разные исходы, разные выводы.
  if grep -qiE 'SSL certificate problem|unable to get local issuer|self.signed certificate|certificate verify failed' <<<"$out"; then
    echo tls-verify-failed; return
  fi
  # (4) имя не разрешилось / маршрута нет / истёк срок ожидания.
  if grep -qiE 'Could not resolve host|Name or service not known|nodename nor servname' <<<"$out"; then
    echo unreachable-name; return
  fi
  if grep -qiE 'Connection timed out|Operation timed out|Timeout was reached' <<<"$out"; then
    echo unreachable-timeout; return
  fi
  # (5) слушателя нет.
  if grep -qiE 'Connection refused|Failed to connect' <<<"$out"; then
    echo refused; return
  fi
  # (6) рукопожатие состоялось.
  if grep -qiE 'SSL connection using TLS' <<<"$out"; then
    echo tls-ok; return
  fi
  # (7) пришёл ответ приложения по открытому тексту.
  if grep -qE 'http_code=[1-5][0-9][0-9]' <<<"$out"; then
    echo app-answer; return
  fi
  # (8) НЕ УЗНАНО. Корзины «всё остальное» здесь нет: неузнанное — красное.
  echo unclassified
}

# tls_protocol_of <вывод> — согласованный протокол, чтобы НАЗВАТЬ его в выводе
# гейта, а не отчитываться словом «ok» без содержания.
tls_protocol_of() {
  grep -oiE 'SSL connection using TLS[^ ,]*' <<<"$1" | head -1 | sed 's/SSL connection using //I'
}

# verify_log_line <строка журнала соседа> <маркер> → именованный исход.
#
# Журнал объявлен с полями протокола и шифра (их заполняет TLS-стек), статуса и
# времени апстрима и маркера запроса. Отсутствие записи — красное отдельно (см.
# кластерную половину): иначе терминатор уклонялся бы от гейта, просто не логируя.
#
# ПОЛЯ АПСТРИМА ПРОВЕРЯЮТСЯ, А НЕ ТОЛЬКО ОБЪЯВЛЯЮТСЯ. Они были выписаны в формат
# журнала и названы в его комментарии ИМЕННО ДЛЯ ЭТОГО — и не читались. Разница
# наблюдаема ровно в том случае, ради которого весь этот переход и построен:
# ответ, произведённый САМИМ СОСЕДОМ (статическая заглушка на пути здоровья),
# апстрима не касается, и nginx пишет в оба поля прочерк. Шифрование при этом
# настоящее, маркер на месте, запись выглядит образцовой — а «под готов»
# перестаёт означать «провайдер отвечает». Прочерк — красное.
verify_log_line() {
  local line="$1" marker="$2"
  if [ -z "$line" ]; then echo empty-log; return; fi
  if ! grep -qF "$marker" <<<"$line"; then echo marker-missing; return; fi
  if ! grep -qE 'ssl_protocol=' <<<"$line"; then echo no-tls-fields; return; fi
  local proto ustatus utime
  proto="$(sed -n 's/.*ssl_protocol=\([^ ]*\).*/\1/p' <<<"$line")"
  # nginx подставляет «-» в переменную, которой у соединения нет. Пустое и «-» —
  # ОДИН И ТОТ ЖЕ смысл: соединение шло не по TLS.
  if [ -z "$proto" ] || [ "$proto" = "-" ]; then echo empty-protocol; return; fi
  # Требуется НАСТОЯЩИЙ код ответа апстрима, а не любой непустой текст: 5xx от
  # провайдера — законный исход (апстрим СПРОШЕН), прочерк — нет.
  ustatus="$(sed -n 's/.*[ ^]upstream_status=\([^ ]*\).*/\1/p' <<<"$line")"
  if ! grep -qE '^[1-5][0-9][0-9]$' <<<"$ustatus"; then echo no-upstream-status; return; fi
  utime="$(sed -n 's/.*[ ^]upstream_time=\([^ ]*\).*/\1/p' <<<"$line")"
  if ! grep -qE '^[0-9]+(\.[0-9]+)?$' <<<"$utime"; then echo no-upstream-time; return; fi
  echo ok
}

# verify_conf_binding <отпечаток из спецификации ЖИВОГО пода> <sha256 текущего
# объекта настроек> → именованный исход.
#
# ЧИТАЕТСЯ ЖИВОЙ ПРОЦЕСС, А НЕ РЕНДЕР. Настройки соседа приезжают ТОМОМ и
# читаются nginx'ом ОДИН РАЗ при старте. Правка объекта настроек сама по себе
# под не перекатывает, поэтому «в кластере лежит новая конфигурация» и «сосед
# работает по новой» — РАЗНЫЕ утверждения, и первое ничего не говорит о втором.
#
# Различить их можно ровно одним способом: отпечаток в спецификации ЗАПУЩЕННОГО
# пода зафиксирован в момент его создания. Если он разошёлся с sha256 текущего
# объекта настроек — под создан из ДРУГОЙ конфигурации, переката не было, и
# живой терминатор работает по старой.
#
# Сверять с ФАЙЛОМ в контейнере бессмысленно: смонтированный том kubelet
# обновляет, и файл покажет НОВОЕ содержимое, пока процесс держит старое. По той
# же причине `nginx -T` тоже не свидетель — он перечитывает файлы с диска.
verify_conf_binding() {
  local spec_sha="$1" cm_sha="$2"
  if [ -z "$cm_sha" ]; then echo missing-configmap; return; fi
  if [ -z "$spec_sha" ]; then echo missing-fingerprint; return; fi
  if [ "$spec_sha" != "$cm_sha" ]; then echo stale-process; return; fi
  echo ok
}

# paired — отрицательное утверждение засчитывается ТОЛЬКО вместе с положительным.
#
# <метка> <исход-положительного> <ожидаемый-положительный> <исход-отрицательного> <ожидаемый-отрицательный>
paired() {
  local label="$1" pos="$2" pos_want="$3" neg="$4" neg_want="$5"
  assertion
  if [ "$pos" != "$pos_want" ]; then
    fail "$label: ПОЛОЖИТЕЛЬНАЯ половина не подтвердилась (получено '$pos', ожидалось '$pos_want')"
    note "отрицательная половина ('$neg') НЕ ЗАСЧИТЫВАЕТСЯ: без работающего перехода"
    note "она означала бы «всё лежит», а не «всё запрещено правильно»."
    return 1
  fi
  if [ "$neg" != "$neg_want" ]; then
    fail "$label: отрицательная половина не подтвердилась (получено '$neg', ожидалось '$neg_want')"
    return 1
  fi
  ok "$label: положительное '$pos' И отрицательное '$neg' — на одном адресе"
  return 0
}

# ═════════════════════════════════════════════════════════════════════════════
# САМОПРОВЕРКА — БЕЗ КЛАСТЕРА.
# ═════════════════════════════════════════════════════════════════════════════
if [ "${1:-}" = "--self-test" ]; then
  echo "=== $SCRIPT --self-test: предикаты вердикта против синтетических наблюдений ==="
  rc=0; checked=0

  expect_class() { # <метка> <ожидаемый исход> <наблюдение>
    local label="$1" want="$2" obs="$3" got
    checked=$((checked + 1))
    got="$(classify_connection "$obs")"
    if [ "$got" = "$want" ]; then echo "  ✓ $label → $got"
    else echo "  ✗ $label → получено '$got', ожидалось '$want'"; rc=1; fi
  }

  echo "-- классификатор соединения: каждый исход НАЗВАН, корзины «прочее» нет --"
  expect_class "рукопожатие состоялось" tls-ok \
    '* SSL connection using TLSv1.3 / TLS_AES_256_GCM_SHA384
http_code=200'
  expect_class "удалённый конец ответил плейнтекстом на TLS" plaintext-answer \
    'curl: (35) error:0A00010B:SSL routines::wrong version number'
  expect_class "плейнтекст отправлен в TLS-листенер" tls-rejected-plaintext \
    'The plain HTTP request was sent to HTTPS port
http_code=400'
  expect_class "TLS есть, доверия нет" tls-verify-failed \
    'curl: (60) SSL certificate problem: unable to get local issuer certificate'
  expect_class "имя не разрешилось" unreachable-name \
    'curl: (6) Could not resolve host: kacho-umbrella-hydra-admin.kacho.svc'
  expect_class "срок ожидания истёк" unreachable-timeout \
    'curl: (28) Connection timed out after 5001 milliseconds'
  expect_class "слушателя нет" refused \
    'curl: (7) Failed to connect to 10.96.0.10 port 4445: Connection refused'
  expect_class "ответ приложения по открытому тексту" app-answer \
    'http_code=200'
  # НЕУЗНАННОЕ — КРАСНОЕ. Проверка, что корзины «всё остальное» действительно нет.
  expect_class "неузнанный вывод не зачитывается ни во что" unclassified \
    'какой-то невиданный вывод без единой известной подписи'

  echo
  echo "-- РАЗЛИЧЕНИЕ, РАДИ КОТОРОГО ВСЁ ЭТО: отказ соединения ≠ правильный отказ --"
  checked=$((checked + 1))
  a="$(classify_connection 'curl: (7) Failed to connect to 10.96.0.10 port 4445: Connection refused')"
  b="$(classify_connection 'The plain HTTP request was sent to HTTPS port')"
  if [ "$a" != "$b" ]; then
    ok "мёртвый сосед ('$a') и работающий TLS на плейнтекст ('$b') — РАЗНЫЕ исходы"
  else
    echo "  ✗ мёртвый сосед и работающий TLS дали ОДИН исход — гейт зеленел бы над мёртвым переходом"
    rc=1
  fi

  echo
  echo "-- парность: отрицательное без положительного НЕ засчитывается --"
  checked=$((checked + 1))
  if paired "проба(парность)" refused tls-ok tls-rejected-plaintext tls-rejected-plaintext >/dev/null 2>&1; then
    echo "  ✗ пара засчитана при НЕ подтвердившейся положительной половине"; rc=1
  else
    ok "пара отвергнута: положительная половина не подтвердилась"
  fi
  checked=$((checked + 1))
  if paired "проба(парность)" tls-ok tls-ok tls-rejected-plaintext tls-rejected-plaintext >/dev/null 2>&1; then
    ok "законная пара (положительное И отрицательное) засчитана"
  else
    echo "  ✗ законная пара отвергнута — гейт ловит форму, а не существо"; rc=1
  fi

  echo
  echo "-- разбор журнала соседа --"
  expect_log() { # <метка> <ожидаемо> <строка> <маркер>
    local label="$1" want="$2" line="$3" marker="$4" got
    checked=$((checked + 1))
    got="$(verify_log_line "$line" "$marker")"
    if [ "$got" = "$want" ]; then echo "  ✓ $label → $got"
    else echo "  ✗ $label → получено '$got', ожидалось '$want'"; rc=1; fi
  }
  M='b3f1c0de-probe'
  expect_log "законная запись (протокол непуст, маркер совпал)" ok \
    "req-1 ssl_protocol=TLSv1.3 ssl_cipher=TLS_AES_256_GCM_SHA384 upstream_status=200 upstream_time=0.004 marker=$M" "$M"
  expect_log "запись без TLS-полей" no-tls-fields \
    "req-1 upstream_status=200 upstream_time=0.004 marker=$M" "$M"
  expect_log "протокол пуст" empty-protocol \
    "req-1 ssl_protocol= ssl_cipher=- upstream_status=200 upstream_time=0.004 marker=$M" "$M"
  expect_log "протокол '-' (nginx так пишет отсутствующую переменную)" empty-protocol \
    "req-1 ssl_protocol=- ssl_cipher=- upstream_status=200 upstream_time=0.004 marker=$M" "$M"
  expect_log "запись не та (маркера нет)" marker-missing \
    "req-1 ssl_protocol=TLSv1.3 ssl_cipher=X upstream_status=200 upstream_time=0.004 marker=someone-else" "$M"
  expect_log "пустой журнал" empty-log "" "$M"
  # СТАТИЧЕСКАЯ ЗАГЛУШКА НА ПУТИ ЗДОРОВЬЯ — ЗДЕСЬ ОНА И ОБЯЗАНА ПОКРАСНЕТЬ.
  # Ответ, произведённый самим соседом, апстрима не касается: nginx пишет в оба
  # поля прочерк. Шифрование при этом настоящее, маркер на месте, запись
  # выглядит образцовой — и ровно так «под готов» разъезжается с «провайдер
  # отвечает». Без этих двух проверок прочерк проходил как `ok`.
  expect_log "ответ произведён соседом, апстрим не спрошен (статус '-')" no-upstream-status \
    "req-1 ssl_protocol=TLSv1.3 ssl_cipher=TLS_AES_256_GCM_SHA384 upstream_status=- upstream_time=- marker=$M" "$M"
  expect_log "поля апстрима вовсе нет" no-upstream-status \
    "req-1 ssl_protocol=TLSv1.3 ssl_cipher=TLS_AES_256_GCM_SHA384 status=200 marker=$M" "$M"
  expect_log "статус апстрима есть, времени нет" no-upstream-time \
    "req-1 ssl_protocol=TLSv1.3 ssl_cipher=X upstream_status=200 upstream_time=- marker=$M" "$M"
  # ЗАКОННАЯ КОНСТРУКЦИЯ ТОЙ ЖЕ ФОРМЫ: апстрим ответил отказом. Это ОТВЕТ
  # ПРОВАЙДЕРА, а не заглушка соседа, — проверка обязана молчать, иначе она
  # ловит форму (код 200), а не существо (апстрим был спрошен).
  expect_log "апстрим ответил 503 — спрошен, значит засчитывается" ok \
    "req-1 ssl_protocol=TLSv1.3 ssl_cipher=X upstream_status=503 upstream_time=1.204 marker=$M" "$M"

  echo
  echo "-- живой процесс против объекта настроек (перекат состоялся?) --"
  expect_bind() { # <метка> <ожидаемо> <отпечаток спецификации> <sha объекта настроек>
    local label="$1" want="$2" a="$3" b="$4" got
    checked=$((checked + 1))
    got="$(verify_conf_binding "$a" "$b")"
    if [ "$got" = "$want" ]; then echo "  ✓ $label → $got"
    else echo "  ✗ $label → получено '$got', ожидалось '$want'"; rc=1; fi
  }
  H1="$(printf 'conf-A' | sha256sum | cut -d' ' -f1)"
  H2="$(printf 'conf-B' | sha256sum | cut -d' ' -f1)"
  expect_bind "под создан из ТЕКУЩЕЙ конфигурации" ok "$H1" "$H1"
  expect_bind "настройки правили, под не перекатывали" stale-process "$H1" "$H2"
  expect_bind "отпечатка в спецификации нет — привязки нет" missing-fingerprint "" "$H1"
  expect_bind "объекта настроек нет — сверять НЕ С ЧЕМ" missing-configmap "$H1" ""

  echo
  echo "-- протокол НАЗЫВАЕТСЯ, а не подразумевается --"
  checked=$((checked + 1))
  p="$(tls_protocol_of '* SSL connection using TLSv1.3 / TLS_AES_256_GCM_SHA384')"
  if [ "$p" = "TLSv1.3" ]; then ok "согласованный протокол извлечён: $p"
  else echo "  ✗ протокол не извлечён (получено '$p')"; rc=1; fi

  echo
  echo "синтетических наблюдений и законных входов проверено: $checked"
  [ $rc -eq 0 ] && echo "PASS: $SCRIPT --self-test" || echo "FAIL: $SCRIPT --self-test"
  exit $rc
fi

# ═════════════════════════════════════════════════════════════════════════════
# КЛАСТЕРНАЯ ПОЛОВИНА
# ═════════════════════════════════════════════════════════════════════════════
command -v kubectl >/dev/null 2>&1 || { echo "FATAL: нужен kubectl"; exit 2; }

# ЦЕЛЬ ПИНИТСЯ ПО КЛАСТЕРУ, А НЕ ПО ИМЕНИ КОНТЕКСТА. Одноимённый контекст уже
# однажды вёл в другой кластер: имя выбирает автор kubeconfig, совпадение имён
# ничего не доказывает.
ctx="$(kubectl config current-context 2>/dev/null)"
case "$ctx" in
  kind-kacho) ;;
  *) echo "ABORT: активный kube-контекст '$ctx' — не kind-kacho."; exit 2 ;;
esac
want_srv="$(kind get kubeconfig --name kacho 2>/dev/null | sed -n 's/^ *server: *//p' | head -1)"
have_srv="$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null)"
if [ -z "$want_srv" ]; then
  echo "ABORT: kind не знает кластера 'kacho' — сверить адрес apiserver'а НЕ С ЧЕМ,"
  echo "       а «не с чем сверить» означает «не проверили», а не «всё хорошо»."
  exit 2
fi
if [ "$want_srv" != "$have_srv" ]; then
  echo "ABORT: контекст называется kind-kacho, но ведёт НЕ в этот кластер."
  echo "       активный → $have_srv"
  echo "       kind-kacho на самом деле → $want_srv"
  exit 2
fi

echo "=== D. административный переход: свидетельство стороны, не участвующей в оценке ==="

RELEASE="$(kubectl -n "$NS" get pod -l app.kubernetes.io/name=hydra \
  -o jsonpath='{.items[0].metadata.labels.app\.kubernetes\.io/instance}' 2>/dev/null)"
if [ -z "$RELEASE" ]; then
  echo "  ✗ под провайдера не найден в ns/$NS — утверждения перехода НЕ ВЫПОЛНЕНЫ (не чисты)"
  exit 1
fi
HYDRA_POD="$(kubectl -n "$NS" get pod -l app.kubernetes.io/name=hydra \
  -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)"
TERM_SVC="$RELEASE-hydra-admin-tls"
TERM_ADDR="$TERM_SVC.$NS.svc"
TERM_PORT="${TERM_PORT:-4445}"
SECRET="${ADMIN_TLS_SECRET:-$RELEASE-hydra-admin-tls}"

# ПОРТ ЛИСТЕНЕРА ПРОВАЙДЕРА ЧИТАЕТСЯ С ЖИВОГО ПОДА, а не берётся константой:
# константа в тексте гейта разошлась бы с рендером молча.
PROV_PORT="$(kubectl -n "$NS" get pod "$HYDRA_POD" \
  -o jsonpath='{.spec.containers[?(@.name=="hydra")].ports[?(@.name=="http-admin")].containerPort}' 2>/dev/null)"
[ -z "$PROV_PORT" ] && PROV_PORT=4445
POD_IP="$(kubectl -n "$NS" get pod "$HYDRA_POD" -o jsonpath='{.status.podIP}' 2>/dev/null)"

echo "  под провайдера: $HYDRA_POD (release $RELEASE), адрес пода $POD_IP"
echo "  терминатор: $TERM_ADDR:$TERM_PORT · листенер провайдера в поде: :$PROV_PORT"
MARKER="sechat-$(date +%s)-$RANDOM"

# ── SEC-HAT-17: ПРЕДПОСЫЛКА ОБХОДНОГО ПУТИ ──────────────────────────────────
#
# Терминатор существует ТОЛЬКО потому, что развёрнутая версия провайдера не
# читает объявление TLS отдельного листенера. Предмет может измениться ТОЛЬКО
# вместе с версией — поэтому гейт стоит на ЭТОЙ оси.
#
# Версия читается с ЖИВОГО пода, а не из рендера и не из записи: расхождение
# «что объявлено» и «что работает» разрешается в пользу работающего.
PREMISE_DOC="$DEPLOY_ROOT/PROVIDER-LISTENER-PREMISE.md"
assertion
if [ ! -f "$PREMISE_DOC" ]; then
  fail "SEC-HAT-17 записи координаты предпосылки нет ($PREMISE_DOC) — сверять НЕ С ЧЕМ,"
  note "а «не с чем сверить» означает «не проверили», а не «всё хорошо»."
else
  rec_image="$(grep -oE 'oryd/hydra:v[0-9][^`|[:space:]]*' "$PREMISE_DOC" | head -1)"
  rec_chart="$(grep -oE 'hydra-[0-9]+\.[0-9]+\.[0-9]+' "$PREMISE_DOC" | head -1)"
  run_image="$(kubectl -n "$NS" get pod "$HYDRA_POD" \
    -o jsonpath='{.spec.containers[?(@.name=="hydra")].image}' 2>/dev/null | sed 's|^docker\.io/||')"
  run_chart="$(kubectl -n "$NS" get pod "$HYDRA_POD" \
    -o jsonpath='{.metadata.labels.helm\.sh/chart}' 2>/dev/null)"
  if [ "$run_image" = "$rec_image" ] && [ "$run_chart" = "$rec_chart" ]; then
    ok "SEC-HAT-17 предпосылка измерена на той версии, что работает ($run_image, $run_chart)"
  else
    fail "SEC-HAT-17 предпосылка терминатора измерена на «$rec_image / $rec_chart»,"
    note "разворачивается «$run_image / $run_chart»."
    # ПЕРЕМЕР ЗАПУСКАЕТСЯ, А НЕ НАЗЫВАЕТСЯ.
    #
    # Здесь стояла строка «Перемерить: bash $REMEASURE --image …» — совет
    # оператору. Шапка самого перемерщика предупреждает ровно об этом исходе:
    # «записанная прозой процедура деградирует в комментарий, который никто не
    # запускает». Так и вышло — на всём дереве не было НИ ОДНОГО его запуска:
    # исполнялась только его самопроверка, то есть доказательство «умею
    # различать» без единого измерения. Расхождение версий — единственный
    # момент, когда предпосылка под вопросом, поэтому мерить надо здесь и
    # сейчас, а не советовать.
    note "перемеряю предпосылку на работающей версии (совет оператору измерением не является):"
    if bash "$DEPLOY_ROOT/scripts/remeasure-provider-listener-tls.sh" \
         --image "$run_image" 2>&1 | sed 's/^/      /'; then
      note "перемер отработал — сверь его вывод с записью в $PREMISE_DOC и обнови её."
    else
      note "перемер НЕ ВЫПОЛНИЛСЯ (нет кластера/контекста/образа) — предпосылка остаётся"
      note "НЕПРОВЕРЕННОЙ; это не «всё хорошо», а отсутствие измерения."
    fi
    note "Показал перемер, что версия объявление ЧИТАЕТ — терминатор подлежит снятию."
  fi
fi

# Отдельным утверждением, ЧЕСТНО ПОМЕЧЕННЫМ: это проверка СВОЕЙ КОНФИГУРАЦИИ, а
# не предпосылки. Снятая мёртвая ручка могла вернуться незаметно — но её
# отсутствие ничего не говорит о том, читает ли её версия.
assertion
# Подстановкой, а не конвейером в `grep -q`: с `pipefail` совпадение роняет
# статус конвейера через SIGPIPE у писателя слева (см. тот же разбор в
# deploy/tests/helm/admin-hop-address-census-test.sh).
mounts="$(kubectl -n "$NS" get pod "$HYDRA_POD" -o jsonpath='{.spec.containers[?(@.name=="hydra")].volumeMounts[*].mountPath}' 2>/dev/null | tr ' ' '\n')"
if grep -q 'hydra-admin-tls' <<<"$mounts"; then
  fail "своя конфигурация (НЕ предпосылка): секрет TLS снова смонтирован в контейнер провайдера"
  note "ручка мертва — держать её рядом с обходным путём, который существует именно"
  note "потому, что она не работает, значит пригласить следующего снять терминатор."
else
  ok "своя конфигурация (НЕ предпосылка): мёртвая ручка TLS в контейнер провайдера не вернулась"
fi

# ── ОДИН пробник выполняет ВСЕ обращения, чтобы отрицательные и положительные
#    наблюдения были сделаны В ОДИН МОМЕНТ и с одного места. Разнесённые во
#    времени половины пары доказывают меньше, чем кажется.
PROBE=sechat-admin-hop-probe
kubectl -n "$NS" delete pod "$PROBE" --ignore-not-found --wait=true >/dev/null 2>&1

cat <<EOF | kubectl -n "$NS" apply -f - >/dev/null 2>&1
apiVersion: v1
kind: Pod
metadata:
  name: $PROBE
  labels: { app.kubernetes.io/part-of: kacho, kacho.cloud/component: admin-hop-probe }
spec:
  restartPolicy: Never
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    seccompProfile: { type: RuntimeDefault }
  volumes:
    - name: anchor
      secret:
        secretName: $SECRET
        optional: true
  containers:
    - name: probe
      image: $PROBE_IMAGE
      imagePullPolicy: IfNotPresent
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities: { drop: ["ALL"] }
      volumeMounts:
        - { name: anchor, mountPath: /anchor, readOnly: true }
      command: ["sh","-c"]
      args:
        - |
          say() { echo "###\$1"; }
          # (1) ПОЛОЖИТЕЛЬНОЕ: рукопожатие с якорем внутреннего центра.
          say POS_TLS
          curl -sS -v -m 8 --cacert /anchor/ca.crt \\
               -H 'X-Kacho-Probe: $MARKER' \\
               -o /dev/null -w 'http_code=%{http_code}\\n' \\
               https://$TERM_ADDR:$TERM_PORT/health/ready 2>&1
          # (2) ОТРИЦАТЕЛЬНОЕ: та же цепочка против ОДНИХ СИСТЕМНЫХ корней.
          say NEG_SYSROOTS
          curl -sS -v -m 8 -o /dev/null -w 'http_code=%{http_code}\\n' \\
               https://$TERM_ADDR:$TERM_PORT/health/ready 2>&1
          # (3) ОТРИЦАТЕЛЬНОЕ: открытым текстом на тот же адрес и порт.
          say NEG_PLAINTEXT
          curl -sS -v -m 8 -o - -w '\\nhttp_code=%{http_code}\\n' \\
               http://$TERM_ADDR:$TERM_PORT/health/ready 2>&1
          # (4) ОТРИЦАТЕЛЬНОЕ: административный листенер провайдера — по снятому
          #     имени Service и по адресу пода, снаружи пода провайдера.
          say NEG_PROVIDER_SVC
          curl -sS -v -m 8 -o /dev/null -w 'http_code=%{http_code}\\n' \\
               http://$RELEASE-hydra-admin.$NS.svc:4445/health/ready 2>&1
          say NEG_PROVIDER_PODIP
          curl -sS -v -m 8 -o /dev/null -w 'http_code=%{http_code}\\n' \\
               http://$POD_IP:$PROV_PORT/health/ready 2>&1
          say DONE
EOF

phase=""
for _ in $(seq 1 60); do
  phase="$(kubectl -n "$NS" get pod "$PROBE" -o jsonpath='{.status.phase}' 2>/dev/null)"
  case "$phase" in Succeeded|Failed) break ;; esac
  sleep 2
done
OUT="$(kubectl -n "$NS" logs "$PROBE" 2>&1)"
kubectl -n "$NS" delete pod "$PROBE" --ignore-not-found --wait=false >/dev/null 2>&1

if [ -z "$OUT" ] || ! grep -q '###DONE' <<<"$OUT"; then
  echo "  ✗ пробник не отработал (phase=$phase) — утверждения НЕ ВЫПОЛНЕНЫ, а не чисты"
  printf '%s\n' "$OUT" | tail -15 | sed 's/^/      /'
  exit 1
fi

section() { awk -v s="###$1" -v e="###$2" '$0==s{f=1;next} $0==e{f=0} f' <<<"$OUT"; }

C_POS="$(classify_connection "$(section POS_TLS NEG_SYSROOTS)")"
C_SYS="$(classify_connection "$(section NEG_SYSROOTS NEG_PLAINTEXT)")"
C_PLAIN="$(classify_connection "$(section NEG_PLAINTEXT NEG_PROVIDER_SVC)")"
C_SVC="$(classify_connection "$(section NEG_PROVIDER_SVC NEG_PROVIDER_PODIP)")"
C_POD="$(classify_connection "$(section NEG_PROVIDER_PODIP DONE)")"
PROTO="$(tls_protocol_of "$(section POS_TLS NEG_SYSROOTS)")"

echo
echo "  наблюдения пробника (одно исполнение, один момент):"
echo "    рукопожатие с якорем внутреннего центра : $C_POS${PROTO:+ ($PROTO)}"
echo "    то же против одних системных корней     : $C_SYS"
echo "    открытым текстом на адрес терминатора   : $C_PLAIN"
echo "    листенер провайдера по имени Service    : $C_SVC"
echo "    листенер провайдера по адресу пода      : $C_POD"
echo

# ── SEC-HAT-05: клиентская половина. Пара «доверяем ⇒ ок» И «не доверяем ⇒ нет».
paired "SEC-HAT-05 цепочка проверяется против внутреннего центра" \
  "$C_POS" tls-ok "$C_SYS" tls-verify-failed
assertion
if [ -n "$PROTO" ]; then ok "SEC-HAT-05 согласованный протокол назван: $PROTO"
else fail "SEC-HAT-05 протокол не назван — «ok» без содержания не принимается"; fi

# ── SEC-HAT-06: плейнтекст в адрес терминатора отвергнут — В ПАРЕ с (1).
paired "SEC-HAT-06 открытым текстом в терминатор — отказ" \
  "$C_POS" tls-ok "$C_PLAIN" tls-rejected-plaintext

# ── SEC-HAT-03: снаружи пода до листенера провайдера не дотянуться — В ПАРЕ с
#    достижимостью ИЗ сетевого пространства пода провайдера.
IN_NETNS="$(kubectl -n "$NS" exec "$HYDRA_POD" -c admin-tls -- \
  sh -c "wget -q -O - -T 5 http://127.0.0.1:$PROV_PORT/health/ready 2>&1 || echo WGET_FAILED" 2>&1)"
assertion
if grep -qE '"status"|ok|alive|ready' <<<"$IN_NETNS" && ! grep -q WGET_FAILED <<<"$IN_NETNS"; then
  ok "SEC-HAT-03 положительная половина: из сетевого пространства пода листенер ОТВЕЧАЕТ"
  for pair in "по имени Service:$C_SVC" "по адресу пода:$C_POD"; do
    label="${pair%%:*}"; got="${pair##*:}"
    assertion
    case "$got" in
      unreachable-name|refused|unreachable-timeout)
        ok "SEC-HAT-03 снаружи пода ($label): $got — и это засчитано, потому что изнутри отвечает" ;;
      *)
        fail "SEC-HAT-03 снаружи пода ($label): $got — открытый участок ДОСТИЖИМ" ;;
    esac
  done
else
  fail "SEC-HAT-03 положительная половина НЕ подтвердилась: из сетевого пространства пода"
  note "листенер не ответил ($(head -c 120 <<<"$IN_NETNS"))."
  note "Отрицательные наблюдения ('$C_SVC' / '$C_POD') НЕ ЗАСЧИТЫВАЮТСЯ: без них это"
  note "означало бы, что увод СЛОМАЛ переход, а не защитил его — а выглядит одинаково."
fi

# ── SEC-HAT-07: серверная половина — журнал соседа.
LOG="$(kubectl -n "$NS" logs "$HYDRA_POD" -c admin-tls --tail=500 2>/dev/null)"
LOG_LINES="$(printf '%s\n' "$LOG" | grep -c . )"
LINE="$(grep -F "$MARKER" <<<"$LOG" | tail -1)"
V="$(verify_log_line "$LINE" "$MARKER")"
assertion
echo "  журнал соседа: прочитано строк $LOG_LINES, запись пробы: $V"
case "$V" in
  ok)
    ok "SEC-HAT-07 дальний конец соединения подтвердил шифрование: $(sed -n 's/.*\(ssl_protocol=[^ ]*\).*/\1/p' <<<"$LINE") $(sed -n 's/.*\(ssl_cipher=[^ ]*\).*/\1/p' <<<"$LINE")"
    note "статус и время апстрима: $(sed -n 's/.*\(upstream_status=[^ ]*\).*/\1/p' <<<"$LINE") $(sed -n 's/.*\(upstream_time=[^ ]*\).*/\1/p' <<<"$LINE")" ;;
  empty-log)
    fail "SEC-HAT-07 записи пробы в журнале соседа НЕТ (прочитано строк: $LOG_LINES)"
    note "отсутствие записи — красное: иначе терминатор уклонялся бы от гейта, просто не логируя." ;;
  no-upstream-status|no-upstream-time)
    fail "SEC-HAT-07 сосед ответил САМ: апстрим по пути пробы не спрошен ($V)"
    note "строка: $(head -c 200 <<<"$LINE")"
    note "у соседа появился ответ, не зависящий от провайдера, — а пробы пода направлены"
    note "СЮДА. Значит «под готов» перестало означать «провайдер отвечает»: повисший"
    note "провайдер не уронил бы готовность, и переход, оставшись конечной точкой обоих"
    note "сервисов, рапортовал бы исправность. Убрать статический ответ: всё, включая"
    note "/health/*, обязано уходить на апстрим (см. конфигурацию соседа)." ;;
  *)
    fail "SEC-HAT-07 запись пробы негодна: $V"
    note "строка: $(head -c 200 <<<"$LINE")" ;;
esac

# ── SEC-HAT-23: живой процесс работает по ТЕКУЩЕЙ конфигурации, а не по той,
#    с которой когда-то стартовал. Читается ЗАПУЩЕННЫЙ под, а не рендер.
assertion
SPEC_SHA="$(kubectl -n "$NS" get pod "$HYDRA_POD" \
  -o jsonpath='{.spec.containers[?(@.name=="admin-tls")].env[?(@.name=="KACHO_ADMIN_TLS_CONF_SHA256")].value}' 2>/dev/null)"
CM_CONF="$(kubectl -n "$NS" get configmap "$RELEASE-hydra-admin-tls-nginx" \
  -o jsonpath='{.data.admin-tls\.conf}' 2>/dev/null)"
CM_SHA=""
[ -n "$CM_CONF" ] && CM_SHA="$(printf '%s' "$CM_CONF" | sha256sum | cut -d' ' -f1)"
B="$(verify_conf_binding "$SPEC_SHA" "$CM_SHA")"
case "$B" in
  ok)
    ok "SEC-HAT-23 живой сосед создан из ТЕКУЩЕЙ конфигурации (${SPEC_SHA:0:12}…)" ;;
  stale-process)
    fail "SEC-HAT-23 конфигурация в кластере ИЗМЕНИЛАСЬ, а под НЕ ПЕРЕКАТЫВАЛСЯ"
    note "спецификация пода: ${SPEC_SHA:0:12}…, текущий объект настроек: ${CM_SHA:0:12}…"
    note "nginx читает конфигурацию один раз при старте — значит живой терминатор"
    note "работает по СТАРОЙ, а рендер и объект настроек показывают новую." ;;
  missing-fingerprint)
    fail "SEC-HAT-23 в спецификации соседа нет отпечатка его конфигурации"
    note "без него правка настроек не меняет шаблон пода и перекат не происходит." ;;
  missing-configmap)
    fail "SEC-HAT-23 объекта настроек соседа нет — сверять НЕ С ЧЕМ,"
    note "а «не с чем сверить» означает «не проверили», а не «всё хорошо»." ;;
esac

echo
echo "  утверждений выполнено: $ASSERTIONS"
if [ "$ASSERTIONS" -eq 0 ]; then
  echo "FAIL: не выполнено НИ ОДНОГО утверждения — это провал, а не чистота."
  exit 1
fi
[ "$FAILED" -ne 0 ] && { echo "FAIL: $SCRIPT"; exit 1; }
echo "PASS: $SCRIPT"
