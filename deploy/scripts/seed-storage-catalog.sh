#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Посев базового каталога блочного хранения в ПОДНЯТЫЙ стенд: кластер данных,
# классы и действующие ревизии привязки «класс × зона».
#
# ЗАЧЕМ ЭТО ШАГ ПОДЪЁМА. Каталог классов стартует пустым (решение владельца:
# класс не предлагается раньше, чем объявлено, чем он обслуживается). Пока он
# пуст, КАЖДОЕ создание тома отвечает «класс не найден», а образы и снимки не
# заводятся вовсе. Свежий стенд непригоден, и ни один «под Ready» этого не
# показывает — ровно тот класс беды, из-за которого заведён посев geo.
#
# ЗОНЫ СПРАШИВАЮТСЯ У ВЛАДЕЛЬЦА, а не выписываются: имена зон произвольны, и
# выписанная копия разъехалась бы с каталогом размещения молча.
#
# ОТКАЗ ГРОМКИЙ. Нет базы, не применился SQL, ноль открытых зон, ноль
# действующих привязок — выход НЕнулевой. Тихий пропуск означал бы стенд,
# отчитавшийся поднятым и не способный создать ни одного тома.
set -euo pipefail

NS="${KACHO_NS:-kacho}"
ST_DB_USER="${STORAGE_DB_USER:-storage}"
ST_DB_NAME="${STORAGE_DB_NAME:-kacho_storage}"
GEO_DB_USER="${GEO_DB_USER:-geo}"
GEO_DB_NAME="${GEO_DB_NAME:-kacho_geo}"
PG_CONTAINER="${STORAGE_PG_CONTAINER:-postgresql}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SQL_FILE="$SCRIPT_DIR/storage-catalog.sql"

if [ ! -f "$SQL_FILE" ]; then
  echo "FATAL: нет $SQL_FILE — сеять нечем." >&2
  exit 1
fi

# StatefulSet резолвится ПОИСКОМ, а не выписанным именем: сабчарт префиксует
# ресурс именем релиза, и выписанная копия расходится с чартом молча.
#
# ОТКАЗ ПЕРЕЧИСЛЕНИЯ ОТЛИЧАЕТСЯ ОТ ПУСТОГО ОТВЕТА, и это не формальность.
# `kubectl … 2>/dev/null || true` превращает «не достучались до кластера» в «в
# кластере ничего нет»: скрипт пошёл бы дальше и объявил стенд неразвёрнутым,
# тогда как на деле неизвестно НИЧЕГО. Здесь код возврата перечисления
# сохраняется отдельно, и его отказ говорит своим текстом.
STS_LIST=""
list_statefulsets() {
  local out rc
  out="$(kubectl -n "$NS" get statefulset -o name 2>&1)"; rc=$?
  if [ $rc -ne 0 ]; then
    echo "FATAL: перечисление StatefulSet в ns/$NS ОТКАЗАЛО (код $rc)." >&2
    echo "       Это не «в кластере пусто», а «мы не знаем, что там»: контекст не тот," >&2
    echo "       прав нет либо кластер недоступен. Сеять вслепую нельзя." >&2
    echo "       Ответ инструмента: $(printf '%s' "$out" | head -2)" >&2
    exit 1
  fi
  STS_LIST="$out"
}

find_sts() {
  printf '%s\n' "$STS_LIST" | grep -E "/([a-z0-9-]+-)?pg-$1\$" | head -1 || true
}

list_statefulsets
# СЕРВИСА НЕТ В ЭТОМ СТЕНДЕ — не то же самое, что «его база не найдена».
#
# Стенд собирается ШАРДАМИ: vpc/nlb/edge поднимаются без storage, и сеять там
# нечего by construction. Безусловный посев падал на них отказом «базу посеять
# некуда», хотя предмета у шага просто нет — это ровно та ошибка, которую сам
# скрипт запрещает ниже: отказ перечисления неотличим от пустого ответа.
#
# Предикат — НАЛИЧИЕ САМОГО СЕРВИСА, а не его базы: базу могло не подняться по
# настоящей причине, и это обязано остаться отказом.
if ! kubectl -n "$NS" get deploy kacho-storage >/dev/null 2>&1; then
  echo "[seed-storage] сервиса kacho-storage нет в этом стенде — сеять нечего."
  echo "               Это ШАРД без storage (vpc/nlb/edge), а не поломка: предмета"
  echo "               у шага нет. Появится сервис — посев исполнится и упадёт, если"
  echo "               базы не окажется."
  exit 0
fi

ST_STS="$(find_sts storage)"
GEO_STS="$(find_sts geo)"

if [ -z "$ST_STS" ]; then
  echo "FATAL: в ns/$NS не найден StatefulSet pg-storage." >&2
  echo "       Стенд не поднят либо kacho-storage не развёрнут: каталог хранения" >&2
  echo "       посеять некуда, а без него КАЖДОЕ создание тома отвечает «класс не найден»." >&2
  echo "       Осмотрено StatefulSet'ов: $(printf '%s\n' "$STS_LIST" | grep -c . || true)" >&2
  exit 1
fi
if [ -z "$GEO_STS" ]; then
  echo "FATAL: в ns/$NS не найден StatefulSet pg-geo — не у кого спросить зоны." >&2
  echo "       Привязка называет зону, и выписать её списком нельзя (имена зон произвольны)." >&2
  exit 1
fi

echo "[seed-storage] цель: $ST_STS (ns/$NS), база $ST_DB_NAME; зоны — из $GEO_STS/$GEO_DB_NAME"
kubectl -n "$NS" rollout status "$ST_STS" --timeout=180s
kubectl -n "$NS" rollout status "$GEO_STS" --timeout=180s

psql_storage() {
  kubectl -n "$NS" exec -i "$ST_STS" -c "$PG_CONTAINER" -- sh -c \
    "PGPASSWORD=\"\$POSTGRES_PASSWORD\" psql -U '$ST_DB_USER' -d '$ST_DB_NAME' -h 127.0.0.1 -v ON_ERROR_STOP=1 $*"
}
psql_geo() {
  kubectl -n "$NS" exec -i "$GEO_STS" -c "$PG_CONTAINER" -- sh -c \
    "PGPASSWORD=\"\$POSTGRES_PASSWORD\" psql -U '$GEO_DB_USER' -d '$GEO_DB_NAME' -h 127.0.0.1 -v ON_ERROR_STOP=1 $*"
}

# ── 1. Кластер данных и классы ──────────────────────────────────────────────
# --single-transaction: либо каталог посеян целиком, либо не тронут.
psql_storage --single-transaction -q -f - < "$SQL_FILE"

# ── 2. Открытые зоны — у владельца ──────────────────────────────────────────
#
# «Открыта» ⟺ зона UP И её регион UP — тот же предикат, которым geo-посев
# утверждает пригодность стенда. Класс, привязанный к закрытой зоне, обещал бы
# размещение там, куда размещать нельзя.
ZONES="$(psql_geo -tAc "\"SELECT z.id FROM kacho_geo.zones z JOIN kacho_geo.regions r ON r.id = z.region_id WHERE z.status = 'UP' AND r.status = 'UP' ORDER BY z.id\"" | tr -d '\r' | sed '/^[[:space:]]*$/d')"
ZONE_COUNT="$(printf '%s\n' "$ZONES" | sed '/^$/d' | wc -l | tr -d '[:space:]')"

if [ "${ZONE_COUNT:-0}" -lt 1 ]; then
  echo "FATAL: у владельца каталога размещения нет ни одной открытой зоны." >&2
  echo "       Привязывать классы не к чему. Сначала — seed-geo." >&2
  exit 1
fi
echo "[seed-storage] открытых зон у geo: $ZONE_COUNT"

# ── 3. Действующие ревизии привязки ─────────────────────────────────────────
#
# Ревизия НЕИЗМЕНЯЕМА: смена условий заводит следующую, прежняя становится
# SUPERSEDED. Поэтому здесь ставится только ПЕРВАЯ (revision=1) и только там,
# где действующей ещё нет — повторный прогон не переписывает историю и не
# меняет правила уже созданным томам задним числом.
#
# Идентификатор выводится из пары «класс × зона» детерминированно, поэтому
# повтор ловится первичным ключом, а не гонкой чтения.
for zone in $ZONES; do
  for dt in block-capacity block-balanced block-fast; do
    psql_storage -q -c "\"
      INSERT INTO kacho_storage.disk_type_bindings
        (id, disk_type_id, zone_id, backend_id, revision, pool, namespace_template,
         cap_snapshots, cap_clone_from_snapshot, cap_clone_from_image,
         cap_clone_keeps_parent, cap_online_grow, cap_multi_attach,
         cap_encryption_at_rest, trash_ttl_seconds, qos, status)
      VALUES
        ('dtb-' || substr(md5('$dt:$zone'), 1, 17), '$dt', '$zone',
         'sb-devstand00000001', 1, 'kacho-$dt', 'proj-{project_id}',
         true, true, true, true, true, false, false, 0, '{}'::jsonb, 'ACTIVE')
      ON CONFLICT DO NOTHING\""
  done
done

count_st() { psql_storage -tAc "\"$1\"" | tr -d '[:space:]'; }

backends="$(count_st "SELECT count(*) FROM kacho_storage.storage_backends WHERE status = 'ACTIVE'")"
types="$(count_st "SELECT count(*) FROM kacho_storage.disk_types WHERE lifecycle = 'ACTIVE'")"
bindings="$(count_st "SELECT count(*) FROM kacho_storage.disk_type_bindings WHERE status = 'ACTIVE'")"
# Пригодный класс — тот, у кого есть действующая ревизия хотя бы в одной зоне.
# Число классов само по себе ничего не значит: класс без привязки не обслуживается.
usable="$(count_st "SELECT count(DISTINCT d.id) FROM kacho_storage.disk_types d JOIN kacho_storage.disk_type_bindings b ON b.disk_type_id = d.id AND b.status = 'ACTIVE' WHERE d.lifecycle = 'ACTIVE'")"

echo "[seed-storage] каталог: кластеров=$backends, классов=$types, действующих привязок=$bindings, ПРИГОДНЫХ классов=$usable"

if [ "${usable:-0}" -lt 1 ]; then
  echo "FATAL: после посева нет ни одного класса с действующей ревизией привязки." >&2
  echo "       Стенд непригоден: создание тома ответит отказом на каждом классе." >&2
  exit 1
fi

# ── 4. Что суита получит на вход ────────────────────────────────────────────
#
# Класс НАЗЫВАЕТСЯ здесь, а не выписывается в шаблоне среды. Прежде среда пинила
# слаг посева миграции; посев снят, и литерал стал ссылкой в пустоту — суита
# ходила бы за классом, которого нет, и падала бы на КАЖДОМ создании тома, а
# причина читалась бы как дефект продукта.
#
# Берётся не «первый попавшийся», а тот, что ПРИГОДЕН: действующее обращение и
# действующая ревизия привязки. Иначе на вход суите уехал бы класс, который
# читается, но ничем не обслуживается.
USABLE_ID="$(psql_storage -tAc "\"SELECT d.id FROM kacho_storage.disk_types d JOIN kacho_storage.disk_type_bindings b ON b.disk_type_id = d.id AND b.status = 'ACTIVE' WHERE d.lifecycle = 'ACTIVE' ORDER BY d.id LIMIT 1\"" | tr -d '[:space:]')"

if [ -z "$USABLE_ID" ]; then
  echo "FATAL: не выбран пригодный класс, хотя счётчик выше насчитал $usable." >&2
  echo "       Расхождение двух предикатов об одном предмете — чинить здесь, не в суите." >&2
  exit 1
fi

REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OUT_FILE="${OUT_FILE:-$REPO_ROOT/.seeded-ids.env}"

# Дописываем СВОЮ строку, не перетирая чужие: этот файл наполняют несколько
# посевов, и перезапись сделала бы последний прогон единственным.
if [ -f "$OUT_FILE" ]; then
  grep -v '^existingDiskTypeId=' "$OUT_FILE" > "$OUT_FILE.tmp" 2>/dev/null || true
  mv "$OUT_FILE.tmp" "$OUT_FILE"
fi
echo "existingDiskTypeId=$USABLE_ID" >> "$OUT_FILE"
echo "[seed-storage] в $OUT_FILE записано existingDiskTypeId=$USABLE_ID"

echo "[seed-storage] done (идемпотентно: повторный прогон не пишет новых строк и не мятит ревизии)"
