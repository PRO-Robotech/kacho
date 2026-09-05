#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# assert-outbox-autovacuum — доказать, что базы с очередями РЕАЛЬНО ИСПОЛНЯЮТСЯ
# со свежей статистикой, а не просто описаны так в values.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ЗДЕСЬ ПРОВЕРЯЕТСЯ И ПОЧЕМУ ИМЕННО ТАК
#
# Дренаж очереди клеймит строки предикатом `sent_at IS NULL`. Оценку под этот
# предикат планировщик берёт из последнего ANALYZE, а очередь почти всегда пуста
# — значит последний ANALYZE почти всегда снят на ПУСТОМ backlog'е, и во всплеск
# план входит с оценкой `rows=1`. На ней частичные индексы клейма отбрасываются,
# и claim дорожает с ~0.2 мс до ~125 мс (замер, Postgres 16, 32 000 pending).
# Продюсер при этом даёт до ~185 строк/с — очередь растёт всё время, пока
# статистика устаревшая.
#
# ЭТО ВЕРНО ДЛЯ ОЧЕРЕДЕЙ С ДОСТАВКОЙ, И НЕ ДЛЯ ВСЕХ ТАБЛИЦ НИЖЕ.
# `kaname.fga_outbox` доставки больше не несёт: дренаж снят вместе с внешним
# движком прав, величины `sent_at`/`attempt_count`/`last_error` сняты миграцией
# (kacho#917), и клейма у него нет — строку читает триггер проекции в той же
# транзакции, что и вставку. Настройки миграции 0064 на таблице ОСТАЮТСЯ (снять их
# можно только новой миграцией, ban #5), и гейт проверяет ровно применённое:
# журнал append-mostly, замороженная статистика на нём остаётся замороженной
# статистикой. Довод про клейм к нему больше не относится — см. kacho#1049.
#
# Свежесть держат ДВЕ настройки, и обе обязаны быть в силе:
#
#   * ТАБЛИЧНАЯ (миграции iam 0064, vpc 0021, compute 0020, nlb 0027,
#     storage 0010, registry 0010): analyze/vacuum каждые 1000 модификаций,
#     scale_factor 0. Отвязывает срабатывание от РАЗМЕРА таблицы. Очередь
#     append-and-retain (у очереди с доставкой дренаж помечает sent_at, строк не
#     удаляет; у журнала строки просто копятся до фоновой уборки по сроку), поэтому
#     дефолтный порог `50 + 0.1*n_live_tup` со временем перерастает целый
#     всплеск — ровно этот дефект табличная половина и снимает.
#
#   * КЛАСТЕРНАЯ `autovacuum_naptime`: launcher заходит в базу не чаще раза в
#     naptime, по умолчанию раз в минуту. Табличный порог даёт ПРИГОДНОСТЬ к
#     анализу (~5.4 с всплеска на замеренном пике), naptime даёт ОБНАРУЖЕНИЕ —
#     и при дефолте именно он ограничивающий (60 с против 5.4 с). Это и есть
#     остаточный минутный всплеск на первой минуте в замерах «после».
#
# Сузить naptime до нужных таблиц НЕЛЬЗЯ — проверено на Postgres 16:
#     ALTER TABLE q SET (autovacuum_analyze_threshold = 1000);  -> ALTER TABLE
#     ALTER TABLE q SET (autovacuum_naptime = '5s');            -> ERROR: unrecognized parameter
#     ALTER DATABASE d SET autovacuum_naptime = '5s';           -> ERROR: cannot be changed now
#     pg_settings.context = sighup
# Это ручка postmaster'а, поэтому она едет через postgresql.conf чарта, а радиус
# сужается ВЫБОРОМ ИНСТАНСОВ: база-на-сервис, значит строка на pg-iam достаёт
# таблицы iam и ничьи больше.
#
# ПОЧЕМУ ГЕЙТ СПРАШИВАЕТ СУБД, А НЕ ЧИТАЕТ ConfigMap. Настройки, приезжающие
# файлом или окружением, читаются процессом на СТАРТЕ. Правка ConfigMap, не
# перекатившая под, оставляет живой процесс на старом значении — стенд на этом
# уже горел (kacho-storage, 2026-07-25: настройки читались production, процесс
# работал в dev). Поэтому единственный допустимый источник ответа —
# `pg_settings` живого postmaster'а. Дополнительно проверяется `source`: значение
# `default` означает, что до процесса не доехало НИЧЕГО, даже если файл на месте.
#
# Структурную половину — что правка вообще перекатывает под — ловит офлайновый
# tests/helm/outbox-autovacuum-naptime-test.sh (он же проверяет, что строка
# переживает наслоение values во всех разворачиваемых профилях).
#
# ЗАПУСК: нужен доступ к кластеру со стендом.
#     make -C deploy assert-outbox-autovacuum          # текущий kube-контекст, ns=kacho
#     NS=kacho bash deploy/scripts/assert-outbox-autovacuum.sh
#     bash deploy/scripts/assert-outbox-autovacuum.sh --self-test   # без кластера
# Инстанс, не развёрнутый в этом кластере, исключается ЯВНО:
#     OUTBOX_SKIP="pg-registry pg-nlb" bash deploy/scripts/assert-outbox-autovacuum.sh
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

NS="${NS:-kacho}"

# Верхняя граница naptime. 5 с выбраны так, чтобы launcher перестал быть
# ОГРАНИЧИВАЮЩИМ звеном: это меньше ~5.4 с, за которые на замеренном пике
# продюсера набегает табличный порог в 1000 модификаций. Ниже смысла нет (тогда
# доминирует порог), выше 10-15 с launcher снова впереди порога.
MAX_NAPTIME_S="${MAX_NAPTIME_S:-5}"

# alias | database | таблицы с ТАБЛИЧНОЙ настройкой из миграции | очереди БЕЗ неё
#
# Последняя колонка — известный и НАЗВАННЫЙ пробел, а не молчание: обе таблицы iam
# так же append-and-retain, но своей миграции с per-table порогом у них нет.
# Клеймится из этих двух только `resource_reconcile_outbox`; `subject_change_outbox`
# — журнал, который край читает курсором, и признака доставки у него нет вовсе. Это чинится в kaname (миграция вида 0064), а
# не здесь; гейт их печатает, чтобы пробел был виден, и НЕ валит на них — иначе
# гейт этой правки был бы красным с момента посадки по чужой причине.
INSTANCES="
pg-vpc|kacho_vpc|kacho_vpc.fga_register_outbox|
pg-compute|kacho_compute|public.compute_fga_register_outbox|
pg-iam|kacho_iam|kaname.fga_outbox|kaname.subject_change_outbox,kaname.resource_reconcile_outbox
pg-nlb|kacho_nlb|kacho_nlb.fga_register_outbox|
pg-storage|kacho_storage|kacho_storage.fga_register_outbox|
pg-registry|kacho_registry|kacho_registry.registry_outbox|
"

OUTBOX_SKIP="${OUTBOX_SKIP:-}"
FAILED=0
ok()   { echo "  OK   $*"; }
warn() { echo "  ..   $*"; }
fail() { echo "  FAIL $*"; FAILED=1; }

# ── Вердикт по naptime. Вынесен отдельной функцией, чтобы его можно было
#    прогнать на ЗАВЕДОМО плохом ответе без кластера (--self-test ниже).
#    Вход — строка `setting|unit|source`, ровно как её отдаёт psql -tAF'|'.
naptime_verdict() {
  local alias="$1" line="$2" setting unit source secs
  if [ -z "$line" ]; then
    fail "$alias: СУБД не ответила на запрос autovacuum_naptime — посадка НЕ ПОДТВЕРЖДЕНА"
    return
  fi
  IFS='|' read -r setting unit source <<<"$line"
  case "$unit" in
    s|"") secs="$setting" ;;
    ms)   secs=$(( setting / 1000 )) ;;
    min)  secs=$(( setting * 60 )) ;;
    *)    fail "$alias: неизвестная единица autovacuum_naptime: '$unit'"; return ;;
  esac
  if [ "$source" = "default" ]; then
    fail "$alias: autovacuum_naptime=${secs}s из source=default — до процесса не доехало НИЧЕГО."
    echo "       ConfigMap мог быть уже поправлен: под, который не перекатился, живёт на своём"
    echo "       boot-time значении, и 'Ready' этого не опровергает."
    return
  fi
  if [ "$secs" -gt "$MAX_NAPTIME_S" ]; then
    fail "$alias: autovacuum_naptime=${secs}s > ${MAX_NAPTIME_S}s (source=$source)"
    return
  fi
  ok "$alias: autovacuum_naptime=${secs}s (source=$source)"
}

# ── Самопроверка на ИНЪЕКЦИИ. Обе строки — РЕАЛЬНЫЙ вывод psql с Postgres 16,
#    снятый до и после подкладывания conf.d/override.conf с `autovacuum_naptime =
#    5s` и `pg_reload_conf()` (перезапуск не требовался — ручка sighup-уровня).
#    Гейт, не проверенный на красном, сам является формой без содержания.
if [ "${1:-}" = "--self-test" ]; then
  echo "=== assert-outbox-autovacuum: self-test (кластер не нужен) ==="
  FAILED=0; naptime_verdict "probe-default" "60|s|default"
  [ "$FAILED" -eq 1 ] || { echo "FAIL: дефолтные 60s обязаны быть КРАСНЫМИ"; exit 1; }
  FAILED=0; naptime_verdict "probe-configured" "5|s|configuration file"
  [ "$FAILED" -eq 0 ] || { echo "FAIL: настроенные 5s обязаны быть ЗЕЛЁНЫМИ"; exit 1; }
  FAILED=0; naptime_verdict "probe-silent" ""
  [ "$FAILED" -eq 1 ] || { echo "FAIL: молчание СУБД обязано быть КРАСНЫМ (fail-closed)"; exit 1; }
  FAILED=0; naptime_verdict "probe-too-slow" "30|s|configuration file"
  [ "$FAILED" -eq 1 ] || { echo "FAIL: 30s обязаны быть КРАСНЫМИ"; exit 1; }
  echo "assert-outbox-autovacuum: self-test green"
  exit 0
fi

command -v kubectl >/dev/null || { echo "FAIL: нужен kubectl"; exit 1; }

echo "=== autovacuum-посадка баз с очередями (ns=$NS, порог ${MAX_NAPTIME_S}s) ==="

while IFS='|' read -r alias db tuned untuned; do
  [ -z "${alias:-}" ] && continue
  case " $OUTBOX_SKIP " in *" $alias "*) echo "  --   $alias: исключён через OUTBOX_SKIP"; continue ;; esac

  pod="kacho-umbrella-${alias}-0"
  if ! kubectl -n "$NS" get pod "$pod" >/dev/null 2>&1; then
    fail "$alias: пода $pod нет — посадка его очереди ($tuned) НЕ ПОДТВЕРЖДЕНА."
    echo "       Если этот инстанс в кластере не разворачивается, исключи его ЯВНО:"
    echo "       OUTBOX_SKIP=\"$alias\" — отсутствие в кластере не должно зеленить гейт."
    continue
  fi

  # A. Ручка postmaster'а — как её сообщает САМ живой процесс.
  line="$(kubectl -n "$NS" exec "$pod" -c postgresql -- bash -c \
    'PGPASSWORD=$POSTGRES_POSTGRES_PASSWORD psql -U postgres -tAF"|" -c \
     "SELECT setting, unit, source FROM pg_settings WHERE name='"'"'autovacuum_naptime'"'"';"' \
    2>/dev/null | tr -d '\r' | grep -v '^$' | head -1)"
  naptime_verdict "$alias" "$line"

  # B. Табличная половина — reloptions живой таблицы, а не текст миграции.
  #    Миграцию могли откатить, базу — восстановить из дампа постарше.
  for t in ${tuned//,/ }; do
    [ -z "$t" ] && continue
    row="$(kubectl -n "$NS" exec "$pod" -c postgresql -- bash -c \
      "PGPASSWORD=\$POSTGRES_POSTGRES_PASSWORD psql -U postgres -d $db -tAF'|' -c \
       \"SELECT coalesce((SELECT option_value FROM pg_options_to_table(c.reloptions) WHERE option_name='autovacuum_analyze_threshold'),'-'),
                coalesce((SELECT option_value FROM pg_options_to_table(c.reloptions) WHERE option_name='autovacuum_analyze_scale_factor'),'-'),
                coalesce(to_char(s.last_autoanalyze,'YYYY-MM-DD HH24:MI:SS'),'never')
           FROM pg_class c
           JOIN pg_namespace n ON n.oid = c.relnamespace
           LEFT JOIN pg_stat_all_tables s ON s.relid = c.oid
          WHERE n.nspname || '.' || c.relname = '$t';\"" \
      2>/dev/null | tr -d '\r' | grep -v '^$' | head -1)"
    if [ -z "$row" ]; then
      fail "$alias: таблицы $t в базе $db нет — миграция с табличной настройкой не применена"
      continue
    fi
    IFS='|' read -r thr scale last <<<"$row"
    if [ "$thr" = "-" ] || [ "$scale" = "-" ]; then
      fail "$alias: у $t нет табличных autovacuum_analyze_* (threshold=$thr scale=$scale) —"
      echo "       срабатывание снова привязано к РАЗМЕРУ таблицы, а очередь растёт и не чистится"
    else
      ok "$alias: $t analyze_threshold=$thr scale_factor=$scale, последний autoanalyze: $last"
    fi
  done

  for t in ${untuned//,/ }; do
    [ -z "$t" ] && continue
    warn "$alias: $t клеймится тем же дренажем, но своей табличной настройки НЕ несёт"
    echo "       (известный пробел; чинится миграцией в kaname, здесь не валит)"
  done
done <<EOF
$(printf '%s\n' "$INSTANCES" | grep -v '^[[:space:]]*$')
EOF

if [ "$FAILED" -ne 0 ]; then
  echo "assert-outbox-autovacuum: RED"
  exit 1
fi
echo "assert-outbox-autovacuum: all green"
