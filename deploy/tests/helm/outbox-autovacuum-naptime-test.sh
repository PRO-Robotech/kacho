#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Каждая база, несущая очередь outbox, ОБЯЗАНА объявлять autovacuum_naptime — и
# объявлять его в ТОМ профиле, которым её реально разворачивают.
#
# ЗАЧЕМ. Дренаж очереди клеймит строки предикатом `sent_at IS NULL`, а оценку
# числа строк под этот предикат планировщик берёт из последнего ANALYZE. Очередь
# почти всегда пуста, поэтому последний ANALYZE почти всегда снят на ПУСТОМ
# backlog'е, и в всплеск планировщик входит с оценкой `rows=1`: частичные индексы
# клейма отбрасываются, и claim дорожает с ~0.2 мс до ~125 мс (замер на живом
# стенде, Postgres 16, 32 000 pending). Табличную половину лечения уже несут
# миграции (analyze каждые 1000 модификаций, scale_factor 0) — она ограничивает
# ПРИГОДНОСТЬ к анализу (~5.4 с всплеска на замеренном пике продюсера), но НЕ
# ограничивает ОБНАРУЖЕНИЕ: launcher autovacuum заходит в базу раз в
# `autovacuum_naptime`, по умолчанию раз в минуту. Всплеск после затишья ждёт
# именно launcher'а — это и есть остаточный минутный всплеск в замерах «после».
#
# ПОЧЕМУ ЭТО НЕЛЬЗЯ ПРОВЕРИТЬ «ПОСМОТРЕВ В values.dev.yaml». `extendedConfiguration`
# — СКАЛЯР. Профиль, объявляющий свой extendedConfiguration, заменяет строку
# ЦЕЛИКОМ, а не дописывает в неё. Значит строка, добавленная в базовый
# values.yaml, МОЛЧА исчезает в любом профиле, у которого есть собственный блок
# (в values.dev.yaml таких четыре). Поэтому проверка идёт по РЕНДЕРУ, а не по
# исходникам: рендерим каждую реально разворачиваемую комбинацию `-f` и читаем
# ПОБЕДИВШИЙ ConfigMap. Ровно этот класс — «правка есть в файле, но в стенд не
# попала» — иначе не ловится вообще.
#
# ЧТО ЕЩЁ ЗДЕСЬ ПРОВЕРЯЕТСЯ. Что pod-template привязан к содержимому этого
# ConfigMap (`checksum/extended-configuration`). Без этой привязки правка
# настройки не перекатывает под, и живой процесс остаётся на своём boot-time
# значении — тот самый класс, на котором стенд уже горел (kacho-storage,
# 2026-07-25). Аннотацию даёт сам сабчарт postgresql, но проверяется она здесь,
# потому что молчаливая её потеря при бампе сабчарта не заметна ничем другим.
#
# ЧЕГО ЭТА ПРОВЕРКА НЕ ДОКАЗЫВАЕТ: что живой postmaster ДЕЙСТВИТЕЛЬНО работает с
# этим значением. ConfigMap, не доехавший до процесса, — не настройка. Это
# доказывает scripts/assert-outbox-autovacuum.sh, спрашивая саму СУБД.
#
# Офлайновая manifest-проверка (kind-кластер не нужен), как остальные tests/helm/*.
set -euo pipefail
# Состав стендов — из ЕДИНСТВЕННОЙ таблицы дерева (deploy/stacks.txt).
# Своей копии цепочек здесь нет: копии разъезжались молча.
. "$(dirname "$0")/stacks.sh"

SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"

# Верхняя граница. Значение выбрано так, чтобы launcher перестал быть
# ОГРАНИЧИВАЮЩИМ звеном: 5 с < ~5.4 с, за которые на замеренном пике продюсера
# (~185 строк/с) набегает табличный порог в 1000 модификаций. Тогда свежесть
# статистики определяет ТАБЛИЧНАЯ настройка — она живёт в миграции рядом со своей
# очередью и настраивается по-очередно. Ниже 5 с смысла нет (доминирует порог),
# выше 10-15 с launcher снова впереди порога.
MAX_NAPTIME_S="${MAX_NAPTIME_S:-5}"

# Базы, несущие клеймимую очередь (`sent_at IS NULL` + drainer), поимённо.
# Список ЖЁСТКИЙ: если бы проверка просто обходила «те инстансы, что нашлись»,
# то забытый инстанс делал бы её зелёной молчанием.
#
# alias|очередь(и), ради которых строка стоит
QUEUE_INSTANCES="
pg-vpc|kacho_vpc.fga_register_outbox
pg-compute|public.compute_fga_register_outbox
pg-iam|kacho_iam.fga_outbox,kacho_iam.subject_change_outbox,kacho_iam.resource_reconcile_outbox
pg-nlb|kacho_nlb.fga_register_outbox
pg-storage|kacho_storage.fga_register_outbox
pg-registry|kacho_registry.registry_outbox
"

# Инстансы БЕЗ клеймимой очереди — строка им НЕ нужна, и её появление там это не
# «на всякий случай», а лишние пробуждения launcher'а в базе, которой они ничего
# не дают. geo_outbox — курсорный аудит-фид по sequence_no, без sent_at и без
# дренажа; openfga/kratos/hydra — сторонние хранилища без очередей Kacho.
NON_QUEUE_INSTANCES="pg-geo pg-openfga pg-kratos pg-hydra"

# Профили, которыми стенды реально разворачивают, — из ЕДИНСТВЕННОЙ таблицы
# дерева (deploy/stacks.txt). Своей копии здесь нет: копия знала о боевой
# цепочке на слой меньше, и её «ноль находок» по этому слою означало «ноль
# прочитанного». Слой КРЕДОВ боевой площадки в таблицу не входит и входить не
# может (он вне git), но набора pg-* не касается.
PROFILES="$(stacks_table | tr ':,' '| ')"

fail() { echo "FAIL: $1"; FAILED=1; }
FAILED=0

python3 -c 'import yaml' 2>/dev/null || { echo "FAIL: нужен python3 с PyYAML"; exit 1; }
command -v helm >/dev/null || { echo "FAIL: нужен helm"; exit 1; }

# ОБЯЗАТЕЛЬНО перед рендером: сервисные сабчарты вендорятся в charts/*.tgz, и
# `helm template` берёт ИМЕННО их. Сбой материализации — КРАСНОЕ: непроверенный
# рендер это не «всё хорошо».
#
# Зовётся ЕДИНСТВЕННЫЙ владелец (scripts/helm-umbrella-deps.sh), а не helm
# напрямую, и это не оформление. Прежняя строка глотала вывод (`>/dev/null 2>&1`)
# и печатала «FAIL: helm dep update» — вердикт без причины: имя недоступного
# адреса приходилось искать в ЧУЖОМ прогоне конвейера. Владелец печатает вывод
# helm целиком, повторяет попытку ограниченное число раз и не ходит в сеть
# повторно, если зависимости уже на месте.
echo "=== $SCRIPT: зависимости умбреллы ==="
bash "$REPO_ROOT/scripts/helm-umbrella-deps.sh" "$UMBRELLA" \
  || { echo "FAIL: зависимости умбреллы не материализованы (причина — выводом выше)"; exit 1; }

TMPD="$(mktemp -d)"; trap 'rm -rf "$TMPD"' EXIT

# ── Ядро проверки, вынесено отдельным разборщиком, чтобы его можно было
#    прогнать на заведомо испорченном рендере (--self-test ниже).
cat >"$TMPD/check.py" <<'PY'
import sys, yaml

render_path, profile, max_naptime = sys.argv[1], sys.argv[2], int(sys.argv[3])
queue = dict(p.split('|', 1) for p in sys.argv[4].split() if p)
non_queue = set(sys.argv[5].split())

docs = [d for d in yaml.safe_load_all(open(render_path)) if isinstance(d, dict)]
cms  = {d['metadata']['name']: d for d in docs if d.get('kind') == 'ConfigMap'}
stss = {d['metadata']['name']: d for d in docs if d.get('kind') == 'StatefulSet'}

def naptime_seconds(conf):
    """Действующее значение из override.conf: последнее присваивание побеждает,
    как и в самом postgresql.conf. Строки-комментарии игнорируются."""
    val = None
    for raw in conf.splitlines():
        line = raw.split('#', 1)[0].strip()
        if not line or '=' not in line:
            continue
        k, v = (x.strip() for x in line.split('=', 1))
        if k == 'autovacuum_naptime':
            val = v
    if val is None:
        return None
    v = val.strip().strip("'\"").lower()
    for suffix, mult in (('ms', 0.001), ('min', 60), ('s', 1)):
        if v.endswith(suffix):
            return float(v[:-len(suffix)].strip()) * mult
    return float(v)  # голое число в postgresql.conf трактуется в единицах GUC (s)

problems = []
for alias, queues in sorted(queue.items()):
    sts = f'kacho-umbrella-{alias}'
    if sts not in stss:
        # Инстанс в этом профиле не разворачивается — это осознанный выбор
        # оператора, но сказать об этом ВСЛУХ обязательно: молчаливый пропуск и
        # есть механизм ложного зелёного.
        print(f'  -- {alias}: StatefulSet не рендерится в профиле {profile} (пропуск)')
        continue

    cm = cms.get(f'{sts}-extended-configuration')
    if cm is None:
        problems.append(f'{alias}: несёт {queues}, но extendedConfiguration в профиле '
                        f'{profile} не рендерится вообще — autovacuum_naptime останется 60s')
        continue

    got = naptime_seconds(cm.get('data', {}).get('override.conf', ''))
    if got is None:
        problems.append(f'{alias}: несёт {queues}, а в победившем override.conf профиля '
                        f'{profile} нет autovacuum_naptime. Скорее всего профиль объявил '
                        f'СВОЙ extendedConfiguration и заменил строку целиком.')
    elif got > max_naptime:
        problems.append(f'{alias}: autovacuum_naptime={got:g}s > {max_naptime}s в профиле {profile}')
    else:
        print(f'  OK {alias}: autovacuum_naptime={got:g}s  ({queues})')

    ann = (stss[sts].get('spec', {}).get('template', {})
                    .get('metadata', {}).get('annotations') or {})
    if 'checksum/extended-configuration' not in ann:
        problems.append(f'{alias}: pod-template НЕ привязан к содержимому extended-configuration '
                        f'(нет checksum/extended-configuration) — правка настройки не перекатит под, '
                        f'и процесс останется на своём boot-time значении')

for alias in sorted(non_queue):
    cm = cms.get(f'kacho-umbrella-{alias}-extended-configuration')
    if cm is None:
        continue
    if naptime_seconds(cm.get('data', {}).get('override.conf', '')) is not None:
        problems.append(f'{alias}: клеймимой очереди не несёт, а autovacuum_naptime задан — '
                        f'лишние пробуждения launcher\'а без предмета. Убрать или обосновать.')

for p in problems:
    print(f'  FAIL {p}')
sys.exit(1 if problems else 0)
PY

# ── Самопроверка разборщика на ИНЪЕКЦИИ дефекта: тот же код прогоняется по
#    рендеру, из которого настройка вырезана. Гейт, не проверенный на красном,
#    сам является формой без содержания.
if [ "${1:-}" = "--self-test" ]; then
  echo "=== $SCRIPT: self-test (инъекция дефекта в рендер) ==="
  helm template kacho-umbrella "$UMBRELLA" -n kacho -f "$UMBRELLA/values.dev.yaml" >"$TMPD/dev.yaml"
  python3 "$TMPD/check.py" "$TMPD/dev.yaml" dev "$MAX_NAPTIME_S" "$QUEUE_INSTANCES" "$NON_QUEUE_INSTANCES" >/dev/null \
    || { echo "FAIL: self-test — целый рендер обязан быть ЗЕЛЁНЫМ"; exit 1; }
  echo "  зелёный на целом рендере: OK"
  grep -v 'autovacuum_naptime' "$TMPD/dev.yaml" >"$TMPD/dev-broken.yaml"
  if python3 "$TMPD/check.py" "$TMPD/dev-broken.yaml" dev-injected "$MAX_NAPTIME_S" "$QUEUE_INSTANCES" "$NON_QUEUE_INSTANCES" >/dev/null 2>&1; then
    echo "FAIL: self-test — рендер БЕЗ настройки прошёл проверку; гейт ничего не проверяет"
    exit 1
  fi
  echo "  красный на рендере без настройки: OK"
  echo "$SCRIPT: self-test green"
  exit 0
fi

printf '%s\n' "$PROFILES" | grep -v '^[[:space:]]*$' >"$TMPD/profiles"
JUDGED=0
while IFS= read -r row; do
  name="${row%%|*}"; files="${row#*|}"
  args=""; missing=""
  for f in $files; do
    if [ -f "$UMBRELLA/$f" ]; then args="$args -f $UMBRELLA/$f"; else missing="$missing $f"; fi
  done
  if [ -n "$missing" ]; then fail "$name: нет values-файлов:$missing"; continue; fi

  echo "=== профиль $name ($files) ==="
  # shellcheck disable=SC2086
  if ! helm template kacho-umbrella "$UMBRELLA" -n kacho $args >"$TMPD/$name.yaml" 2>"$TMPD/$name.err"; then
    fail "$name: рендер не удался — $(head -3 "$TMPD/$name.err" | tr '\n' ' ')"
    continue
  fi
  python3 "$TMPD/check.py" "$TMPD/$name.yaml" "$name" "$MAX_NAPTIME_S" \
    "$QUEUE_INSTANCES" "$NON_QUEUE_INSTANCES" || FAILED=1
  JUDGED=$((JUDGED + 1))
done <"$TMPD/profiles"

# FAILED и JUDGED выставляются в теле while, который здесь исполняется в ТЕКУЩЕЙ
# оболочке (перенаправление из файла, не пайп) — значения переживают цикл.
#
# ОБЪЁМ ОСМОТРЕННОГО называется числом, а не подразумевается: профили берутся из
# таблицы стендов, и её опустевшая или переименованная колонка сделала бы эту
# проверку немой — «all green» читалось бы одинаково и при шести отсуженных
# профилях, и при нуле.
QUEUES_DECLARED="$(printf '%s\n' "$QUEUE_INSTANCES" | grep -c .)"
echo "$SCRIPT: осмотрено профилей $JUDGED из $(grep -c . "$TMPD/profiles"), " \
     "баз с клеймимой очередью объявлено $QUEUES_DECLARED, без очереди $(printf '%s\n' $NON_QUEUE_INSTANCES | grep -c .)"
if [ "$JUDGED" -eq 0 ]; then
  echo "$SCRIPT: RED — не отсужено НИ ОДНОГО профиля; таблица стендов пуста или её"
  echo "         колонка переименована. Ноль находок на нуле прочитанного — это провал."
  exit 1
fi
[ "$FAILED" -eq 0 ] || { echo "$SCRIPT: RED"; exit 1; }
echo "$SCRIPT: all green"
