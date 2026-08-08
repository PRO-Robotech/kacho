#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Посев базового каталога размещения kacho-geo (Region/Zone) в ПОДНЯТЫЙ стенд.
# Единственный источник SQL — geo-baseline.sql рядом; здесь только доставка.
#
# ЗАЧЕМ ЭТО ШАГ ПОДЪЁМА, А НЕ РУЧНОЕ ДЕЙСТВИЕ. Каталог geo стартует пустым
# (осознанно: Region/Zone заводит администратор). Пока он пуст, каждое создание
# зонального/регионального ресурса отвечает «зона/регион не найдены» —
# vpc/compute/nlb/storage/registry валидируют placement peer-вызовом к geo на
# пути запроса, fail-closed. Свежий стенд без этого шага непригоден, и ни один
# «под Ready» этого не показывает.
#
# ОТКАЗ ГРОМКИЙ, А НЕ ТИХИЙ. Нет StatefulSet pg-geo, не применился SQL, нет ни
# одной открытой зоны — выход НЕнулевой. Тихий пропуск здесь означал бы стенд,
# который отчитался поднятым и не может создать ни одного ресурса.
set -euo pipefail

NS="${KACHO_NS:-kacho}"
DB_USER="${GEO_DB_USER:-geo}"
DB_NAME="${GEO_DB_NAME:-kacho_geo}"
PG_CONTAINER="${GEO_PG_CONTAINER:-postgresql}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SQL_FILE="$SCRIPT_DIR/geo-baseline.sql"

if [ ! -f "$SQL_FILE" ]; then
  echo "FATAL: нет $SQL_FILE — сеять нечем." >&2
  exit 1
fi

# StatefulSet резолвится ПОИСКОМ, а не выписанным именем: bitnami-сабчарт
# префиксует ресурс именем релиза, и выписанная копия имени разъезжается с
# чартом молча (в этом же Makefile цель `psql SVC=geo` зовёт `statefulset/pg-geo`,
# а цель `psql-nlb` — `statefulset/kacho-umbrella-pg-nlb`: две формы одного имени,
# из которых верна одна).
STS="$(kubectl -n "$NS" get statefulset -o name 2>/dev/null \
        | grep -E '/([a-z0-9-]+-)?pg-geo$' | head -1 || true)"
if [ -z "$STS" ]; then
  echo "FATAL: в ns/$NS не найден StatefulSet pg-geo." >&2
  echo "       Стенд не поднят либо kacho-geo не развёрнут. Базовый каталог geo" >&2
  echo "       посеять некуда, а без него КАЖДОЕ создание зонального ресурса" >&2
  echo "       отвечает «зона не найдена»." >&2
  echo "       Осмотрено StatefulSet'ов: $(kubectl -n "$NS" get statefulset -o name 2>/dev/null | wc -l)" >&2
  exit 1
fi

echo "[seed-geo] цель: $STS (ns/$NS), база $DB_NAME, файл $(basename "$SQL_FILE")"
kubectl -n "$NS" rollout status "$STS" --timeout=180s

psql_in_pod() {
  kubectl -n "$NS" exec -i "$STS" -c "$PG_CONTAINER" -- sh -c \
    "PGPASSWORD=\"\$POSTGRES_PASSWORD\" psql -U '$DB_USER' -d '$DB_NAME' -h 127.0.0.1 -v ON_ERROR_STOP=1 $*"
}

# --single-transaction: два оператора файла (регионы, затем зоны) связаны FK
# RESTRICT — либо каталог посеян целиком, либо не тронут.
psql_in_pod --single-transaction -q -f - < "$SQL_FILE"

count() { psql_in_pod -tAc "\"$1\"" | tr -d '[:space:]'; }

regions="$(count 'SELECT count(*) FROM kacho_geo.regions')"
zones="$(count 'SELECT count(*) FROM kacho_geo.zones')"
open_zones="$(count "SELECT count(*) FROM kacho_geo.zones z JOIN kacho_geo.regions r ON r.id = z.region_id WHERE z.status = 'UP' AND r.status = 'UP'")"

echo "[seed-geo] каталог geo: регионов=$regions, зон=$zones, открытых для размещения=$open_zones"

# Утверждение, а не печать: «открытых зон нет» — ровно то состояние, из-за
# которого стенд непригоден, и оно обязано валить шаг подъёма.
if [ "${open_zones:-0}" -lt 1 ]; then
  echo "FATAL: после посева в каталоге нет ни одной зоны, открытой для размещения" >&2
  echo "       (зона открыта ⟺ zone.status='UP' И region.status='UP')." >&2
  echo "       Стенд непригоден: любое зональное создание ответит «зона не найдена»." >&2
  exit 1
fi

echo "[seed-geo] done (идемпотентно: повторный прогон не пишет ни ресурсных, ни audit-строк)"
