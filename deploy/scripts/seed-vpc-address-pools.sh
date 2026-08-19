#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Посев базового каталога внешних адресов kacho-vpc в ПОДНЯТЫЙ стенд:
# зоне-независимый (аникаст) пул EXTERNAL_PUBLIC «по умолчанию» — источник
# публичного адреса КАЖДОГО внешнего балансировщика.
#
# Единственный источник SQL — vpc-address-pool-baseline.sql рядом; здесь только
# доставка и утверждение исхода.
#
# ЗАЧЕМ ЭТО ШАГ ПОДЪЁМА. Внешнее размещение бывает только региональным, значит
# внешний балансировщик зоне-независим by construction и берёт адрес из полосы
# `zone_id IS NULL`. Пока эта полоса пуста, каждое создание внешнего
# балансировщика отвечает «could not allocate load balancer address», сага
# сносит handle компенсацией, и `GET` по названному идентификатору отдаёт
# настоящий 404. Свежий стенд без этого шага непригоден для целого
# продуктового действия, и ни один «под Ready» этого не показывает — тот же
# класс беды, из-за которого заведены посевы geo и каталога хранения.
#
# ДВА РЕЖИМА, И ОДИН ПРЕДИКАТ НА ОБА.
#   (без аргументов) — посеять и утвердить;
#   --check          — только утвердить, НИЧЕГО не записывая.
# Режим проверки нужен волне консоли: она обязана отличать «условие не создано»
# от «пробы красные» ОТДЕЛЬНЫМ шагом (e2e-flow.md §6), а второй предикат в
# другом файле разошёлся бы с этим молча.
#
# ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — не существование пула, а СПОСОБНОСТЬ ПОЛОСЫ ВЫДЕЛИТЬ
# АДРЕС: пул «по умолчанию» без единого свободного адреса отвечает тем же самым
# отказом, только теперь при видимом пуле. Поэтому предикат — пул полосы, у
# которого непуст список свободных адресов.
#
# ОТКАЗ ГРОМКИЙ. Нет StatefulSet pg-vpc, не применился SQL, полоса пуста — выход
# НЕнулевой. Тихий пропуск здесь означал бы стенд, отчитавшийся поднятым и не
# способный создать ни одного внешнего балансировщика.
set -euo pipefail

MODE="seed"
case "${1:-}" in
  "")        MODE="seed" ;;
  --check)   MODE="check" ;;
  *)         echo "usage: $(basename "$0") [--check]" >&2; exit 2 ;;
esac

NS="${KACHO_NS:-kacho}"
DB_USER="${VPC_DB_USER:-vpc}"
DB_NAME="${VPC_DB_NAME:-kacho_vpc}"
PG_CONTAINER="${VPC_PG_CONTAINER:-postgresql}"
POSTURE_SKIP="${POSTURE_SKIP:-}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SQL_FILE="$SCRIPT_DIR/vpc-address-pool-baseline.sql"

if [ "$MODE" = "seed" ] && [ ! -f "$SQL_FILE" ]; then
  echo "FATAL: нет $SQL_FILE — сеять нечем." >&2
  exit 1
fi

# Отсутствие компонента ОБЪЯВЛЯЕТСЯ, а не угадывается опросом: опрос не отличает
# «компонента нет» от «компонент ещё не появился», и на медленном стенде посев
# решил бы, что сеять нечего, и вышел бы успехом. Та же форма, что у посева
# каталога хранения.
for skipped in $POSTURE_SKIP; do
  if [ "$skipped" = "vpc" ]; then
    echo "[seed-vpc-pools] vpc объявлен ОТСУТСТВУЮЩИМ на этом стенде (POSTURE_SKIP)."
    echo "                 Сеять нечего — предмета у шага нет."
    exit 0
  fi
done

# ОТКАЗ ПЕРЕЧИСЛЕНИЯ ОТЛИЧАЕТСЯ ОТ ПУСТОГО ОТВЕТА: `2>/dev/null || true`
# превратил бы «не достучались до кластера» в «в кластере ничего нет».
STS_LIST=""
if ! STS_LIST="$(kubectl -n "$NS" get statefulset -o name 2>&1)"; then
  echo "FATAL: перечисление StatefulSet в ns/$NS ОТКАЗАЛО." >&2
  echo "       Это не «в кластере пусто», а «мы не знаем, что там»: контекст не тот," >&2
  echo "       прав нет либо кластер недоступен." >&2
  echo "       Ответ инструмента: $(printf '%s' "$STS_LIST" | head -2)" >&2
  exit 1
fi

# StatefulSet резолвится ПОИСКОМ, а не выписанным именем: сабчарт префиксует
# ресурс именем релиза, и выписанная копия расходится с чартом молча.
STS="$(printf '%s\n' "$STS_LIST" | grep -E '/([a-z0-9-]+-)?pg-vpc$' | head -1 || true)"
if [ -z "$STS" ]; then
  echo "FATAL: в ns/$NS не найден StatefulSet pg-vpc." >&2
  echo "       Стенд не поднят либо kacho-vpc не развёрнут: каталог внешних адресов" >&2
  echo "       посеять некуда, а без него КАЖДОЕ создание внешнего балансировщика" >&2
  echo "       отвечает «could not allocate load balancer address»." >&2
  echo "       Осмотрено StatefulSet'ов: $(printf '%s\n' "$STS_LIST" | grep -c . || true)" >&2
  exit 1
fi

echo "[seed-vpc-pools] режим: $MODE; цель: $STS (ns/$NS), база $DB_NAME"
kubectl -n "$NS" rollout status "$STS" --timeout=180s

psql_in_pod() {
  kubectl -n "$NS" exec -i "$STS" -c "$PG_CONTAINER" -- sh -c \
    "PGPASSWORD=\"\$POSTGRES_PASSWORD\" psql -U '$DB_USER' -d '$DB_NAME' -h 127.0.0.1 -v ON_ERROR_STOP=1 $*"
}

if [ "$MODE" = "seed" ]; then
  # --single-transaction: строка пула, её блоки, список свободных адресов,
  # курсор IPv6 и audit-строка появляются вместе либо не появляются вовсе.
  psql_in_pod --single-transaction -q -f - < "$SQL_FILE"
fi

count() { psql_in_pod -tAc "\"$1\"" | tr -d '[:space:]'; }

pools="$(count 'SELECT count(*) FROM kacho_vpc.address_pools')"
# Полоса аникаста: пул EXTERNAL_PUBLIC (kind=1) БЕЗ зоны, помеченный «по
# умолчанию». Это ровно тот предикат, которым резолвер выбирает пул
# (GetDefaultForZone("") → WHERE zone_id IS NULL AND kind = $1 AND is_default).
lane="$(count "SELECT count(*) FROM kacho_vpc.address_pools WHERE zone_id IS NULL AND kind = 1 AND is_default")"
# Пригодная полоса — та, из которой ЕСТЬ ЧТО выделить.
usable="$(count "SELECT count(*) FROM kacho_vpc.address_pools p
                  WHERE p.zone_id IS NULL AND p.kind = 1 AND p.is_default
                    AND EXISTS (SELECT 1 FROM kacho_vpc.address_pool_free_ips f WHERE f.pool_id = p.id)")"
free_ips="$(count "SELECT count(*) FROM kacho_vpc.address_pool_free_ips f
                    JOIN kacho_vpc.address_pools p ON p.id = f.pool_id
                   WHERE p.zone_id IS NULL AND p.kind = 1 AND p.is_default")"

echo "[seed-vpc-pools] каталог адресов: пулов всего=$pools, в полосе аникаста «по умолчанию»=$lane, ПРИГОДНЫХ=$usable, свободных адресов в полосе=$free_ips"

if [ "${usable:-0}" -lt 1 ]; then
  # Команда конвейера читается со СТАНДАРТНОГО ВЫВОДА, а не с потока ошибок:
  # отправленная в stderr, она осталась бы обычной строкой лога, и шаг упал бы
  # без заголовка «условие не создано» — то есть неотличимо от красноты проб.
  echo "::error title=Условие не создано::в полосе внешних адресов нет пригодного пула по умолчанию"
  echo "FATAL: у зоне-независимой (аникаст) полосы нет пула «по умолчанию» со свободными адресами." >&2
  echo "       Стенд непригоден для внешнего балансировщика: КАЖДОЕ его создание ответит" >&2
  echo "       «could not allocate load balancer address», сага снесёт handle компенсацией," >&2
  echo "       и GET по названному идентификатору отдаст настоящий 404." >&2
  echo "       Осмотрено пулов: $pools; в полосе: $lane (из них со свободными адресами: $usable)." >&2
  if [ "${lane:-0}" -ge 1 ]; then
    echo "       Пул в полосе ЕСТЬ, но выделять из него нечего — его блоки не нормализованы" >&2
    echo "       либо пересеклись с чужим пулом (address_pool_cidrs EXCLUDE глобален на kind)." >&2
    echo "       Разберите пересечение и повторите: make seed-vpc-pools" >&2
  else
    echo "       Полоса пуста. Посейте её: make seed-vpc-pools" >&2
  fi
  exit 1
fi

if [ "$MODE" = "check" ]; then
  echo "[seed-vpc-pools] условие создано: внешний балансировщик получит адрес (проверка, ничего не записано)"
else
  echo "[seed-vpc-pools] done (идемпотентно: повторный прогон не пишет ни ресурсных, ни audit-строк)"
fi
