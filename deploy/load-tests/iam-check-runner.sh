#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# iam-check-runner.sh — генератор ПОДАЧИ теневой сверки и генератора нагрузки k6.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАЧЕМ ЭТОТ ФАЙЛ ПОЯВИЛСЯ
#
# `services/iam/tests/k6/Makefile` звал `iam-check-runner.sh up|down` — скрипт,
# которого не было НИ В ДЕРЕВЕ, НИ В ИСТОРИИ. Генератор на стенде был поднят
# руками, и подача (`/fixtures/allow_tuples.json`) собрана руками же. Следствие
# оказалось не косметическим: подача несла 273 тройки шести типов и НИ ОДНОГО
# объекта пяти собственных типов iam, поэтому проба расхождения показывала
# «0.00 %» одновременно с переписью, называвшей 15 085 потерянных объектов ровно
# этих типов. Ноль пробы был «нулём прочитанного», и повторить подачу деревом,
# чтобы это увидеть, было нечем.
#
# ─────────────────────────────────────────────────────────────────────────────
# КАК СТРОИТСЯ ПОДАЧА — И ПОЧЕМУ ЯКОРЬ БЕРЁТСЯ ИЗ ДВУХ РАЗНЫХ МЕСТ
#
# Тройка подачи — это вопрос «может ли субъект S прочитать объект O», на который
# МОДЕЛЬ ПРАВ отвечает «да» по каскаду `super_admin`. Субъектом берётся
# администратор (или владелец) аккаунта, в котором объект живёт.
#
#   собственные пять типов iam — якорь из СХЕМЫ (`users.account_id`,
#     `groups.account_id`, `service_accounts.account_id`, `roles.account_id` либо
#     `roles.project_id`, `access_bindings.resource_type/resource_id`). Схема —
#     источник, который iam ВЕДЁТ; строить их из журнала значило бы строить
#     подачу из того самого места, с которым её потом сверяют;
#
#   зеркальные типы соседей (`vpc_*`, `nlb_*`) — якорь из ЖУРНАЛА
#     (`relation_fact`, указатель `project`). Своей схемы у iam для них нет, и
#     другого источника не существует.
#
# ЖИВОСТЬ ОБЪЕКТА — ЧАСТЬ ПОСТРОЕНИЯ, А НЕ ПОБОЧНЫЙ ЭФФЕКТ. Собственные типы
# берутся из ТАБЛИЦ, а не из журнала, поэтому мёртвые объекты в подачу не
# попадают by construction. Это не педантизм: на 2026-08-20 журнал несёт 14 454
# указателя на группы при ОДНОЙ живой группе — подача, построенная из журнала,
# на 97 % состояла бы из объектов, которых нет, и мерила бы предмет задачи #783
# (рёбра, не чистящиеся при снятии регистрации), объявленный вне границ R7-4.
#
# КЛАСС «ПРОЕКТНАЯ РОЛЬ» РАЗМЕЧАЕТСЯ ЗДЕСЬ, ЗАРАНЕЕ. Роль с областью «проект»
# несёт пустой `account_id` (ограничение `roles_definition_tier_xor`: ровно один
# непустой якорь из трёх), а отношения `project` у типа `iam_role` в модели прав
# нет вовсе. Расхождение по такой роли ОЖИДАЕМО и останется ожидаемым. Тройка
# получает поле `"class": "project_role"`, и прибор считает по ней ОТДЕЛЬНЫЙ
# знаменатель: растворённый в общей доле, этот класс либо объявил бы достройку
# неудачной, либо научил бы игнорировать долю.
#
# ─────────────────────────────────────────────────────────────────────────────
# ИСХОДОВ ТРИ, И ТРЕТИЙ НЕ ВЫЧИТАЕТСЯ ИЗ ВЕРДИКТА:
#   0 — сделано;
#   1 — сделать не удалось (запрос отказал, под не поднялся, вывод пуст);
#   3 — УСЛОВИЕ НЕ СОЗДАНО (базы нет, схемы нет, в данных нет предмета).
#
# Использование:
#   iam-check-runner.sh fixture [путь]   — построить подачу в ЛОКАЛЬНЫЙ файл
#                                          (только чтение базы, стенд не меняется)
#   iam-check-runner.sh up               — построить подачу и поднять генератор
#   iam-check-runner.sh down             — снять генератор и его настройки
#
# Ручки: NS PG_POD DB DBUSER RUNNER SCRIPT_CM FIXTURE_CM CERT_SECRET K6_IMAGE
#        MAX_PER_TYPE (по умолчанию 60) FIXTURE_OUT
set -euo pipefail

NS="${NS:-kacho}"
PG_POD="${PG_POD:-kacho-umbrella-pg-iam-0}"
DB="${DB:-kaname}"
DBUSER="${DBUSER:-postgres}"
RUNNER="${RUNNER:-k6-iam-runner}"
SCRIPT_CM="${SCRIPT_CM:-k6-iam-check-script}"
FIXTURE_CM="${FIXTURE_CM:-k6-iam-check-fixture}"
CERT_SECRET="${CERT_SECRET:-kacho-compute-client-tls}"
K6_IMAGE="${K6_IMAGE:-grafana/k6:2.1.0}"
MAX_PER_TYPE="${MAX_PER_TYPE:-60}"
FIXTURE_OUT="${FIXTURE_OUT:-${TMPDIR:-/tmp}/kaname-allow-tuples.json}"
TREE_SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/services/iam/tests/k6/internal_check.js"

k() { kubectl -n "$NS" "$@"; }
die_precond() { echo "УСЛОВИЕ НЕ СОЗДАНО: $*" >&2; exit 3; }
die() { echo "ОТКАЗ: $*" >&2; exit 1; }

case "${MAX_PER_TYPE}" in
  ''|*[!0-9]*) die "MAX_PER_TYPE обязан быть целым числом, получено «$MAX_PER_TYPE»";;
esac
[ "$MAX_PER_TYPE" -gt 0 ] || die "MAX_PER_TYPE обязан быть больше нуля"

# ─────────────────────────────────────────────────────────────────────────────
# Определение подачи. Держится ОДНИМ текстом и подставляется в оба запроса ниже:
# две копии разошлись бы молча, и разошлись бы именно между составом и содержимым.
feed_cte() {
cat <<SQL
WITH
acct_admin AS (
  SELECT object_id AS account_id, subject FROM kaname.relation_fact
   WHERE object_type = 'account' AND relation IN ('admin','owner')
     AND split_part(subject, ':', 1) IN ('user','service_account')),
acct_owner AS (
  SELECT object_id AS account_id, subject FROM kaname.relation_fact
   WHERE object_type = 'account' AND relation = 'owner'
     AND split_part(subject, ':', 1) = 'user'),
proj_acct AS (
  SELECT object_id AS project_id, split_part(subject, ':', 2) AS account_id
    FROM kaname.relation_fact
   WHERE object_type = 'project' AND relation = 'account'),
own AS (
  SELECT 'iam_user' AS otype, u.id AS oid, u.account_id AS anchor, NULL::text AS cls
    FROM kaname.users u WHERE u.account_id IS NOT NULL
  UNION ALL SELECT 'iam_group', g.id, g.account_id, NULL::text
    FROM kaname.groups g WHERE g.account_id IS NOT NULL
  UNION ALL SELECT 'iam_service_account', s.id, s.account_id, NULL::text
    FROM kaname.service_accounts s WHERE s.account_id IS NOT NULL
  UNION ALL SELECT 'iam_role', r.id, r.account_id, NULL::text
    FROM kaname.roles r WHERE r.account_id IS NOT NULL
  UNION ALL SELECT 'iam_role', r.id, p.account_id, 'project_role'
    FROM kaname.roles r JOIN kaname.projects p ON p.id = r.project_id
   WHERE r.project_id IS NOT NULL
  UNION ALL SELECT 'iam_access_binding', b.id, b.resource_id, NULL::text
    FROM kaname.access_bindings b WHERE b.resource_type = 'account'
  UNION ALL SELECT 'iam_access_binding', b.id, p.account_id, NULL::text
    FROM kaname.access_bindings b JOIN kaname.projects p ON p.id = b.resource_id
   WHERE b.resource_type = 'project'),
mirror AS (
  SELECT rf.object_type, rf.object_id, pa.account_id, NULL::text
    FROM kaname.relation_fact rf
    JOIN proj_acct pa ON pa.project_id = split_part(rf.subject, ':', 2)
   WHERE rf.relation = 'project'
     AND rf.object_type NOT LIKE 'iam!_%' ESCAPE '!'
     AND rf.object_type NOT IN ('project','account','cluster')),
cand AS (
  SELECT s.otype, s.oid, s.cls, a.subject
    FROM (SELECT * FROM own UNION ALL SELECT * FROM mirror) s
    JOIN acct_admin a ON a.account_id = s.anchor
  UNION ALL SELECT 'account', o.account_id, NULL::text, o.subject FROM acct_owner o
  UNION ALL SELECT 'project', pa.project_id, NULL::text, a.subject
    FROM proj_acct pa JOIN acct_admin a ON a.account_id = pa.account_id),
-- Один субъект на объект и УСТОЙЧИВЫЙ отбор: порядок задан явно, поэтому подача
-- воспроизводима, а не «какая выпала». Недетерминированный вход сделал бы
-- сравнение двух прогонов бессмысленным.
picked AS (
  SELECT DISTINCT ON (otype, oid) otype, oid, cls, subject
    FROM cand ORDER BY otype, oid, subject),
capped AS (
  SELECT q.otype, q.oid, q.cls, q.subject FROM (
    SELECT p.*, row_number() OVER (PARTITION BY otype ORDER BY oid) AS rn FROM picked p
  ) q WHERE q.rn <= ${MAX_PER_TYPE})
SQL
}

build_fixture() { # build_fixture <путь>
  local out="$1" raw comp json
  k get pod "$PG_POD" >/dev/null 2>&1 \
    || die_precond "пода базы $PG_POD в пространстве $NS нет"

  # Один сеанс, ДВА запроса, ЯВНО ТОЛЬКО ДЛЯ ЧТЕНИЯ: состав и содержимое обязаны
  # относиться к одному снимку данных, иначе напечатанный состав описывал бы не
  # ту подачу, которая записана в файл. `READ ONLY` — не декорация: генератор
  # ходит в общую базу, и запрет на запись здесь стоит по построению.
  local sql
  sql="BEGIN TRANSACTION READ ONLY;
$(feed_cte)
SELECT 'СОСТАВ|' || otype || '|' || count(*)::text || '|'
       || count(*) FILTER (WHERE cls IS NOT NULL)::text
  FROM capped GROUP BY otype ORDER BY otype;
$(feed_cte)
SELECT jsonb_pretty(jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
         'subject', subject,
         'relation', 'v_get',
         'object',   otype || ':' || oid,
         'class',    cls)) ORDER BY otype, oid))
  FROM capped;
COMMIT;"

  set +e
  raw=$(printf '%s\n' "$sql" | k exec -i "$PG_POD" -- psql -U "$DBUSER" -d "$DB" -Atq -f - 2>&1)
  local rc=$?
  set -e
  [ "$rc" -eq 0 ] || { printf '%s\n' "$raw" | tail -20 >&2; die "запрос подачи отказал (psql rc=$rc)"; }

  # Разделение по ПРИЗНАКУ строки, а не по номеру: строк состава столько, сколько
  # типов, и это число меняется вместе с данными.
  comp=$(printf '%s\n' "$raw" | sed -n 's/^СОСТАВ|//p')
  json=$(printf '%s\n' "$raw" | sed '/^СОСТАВ|/d' | sed '/^Defaulted container/d')

  [ -n "$comp" ] || die "запрос вернул пустой состав — подачи не построено"
  case "$json" in
    ''|null) die_precond "подача пуста: в базе нет ни одной тройки, которую модель прав разрешает. Мерить нечем, и «ноль расхождений» здесь означал бы «ноль прочитанного»";;
  esac

  local total=0 own=0 pr=0 ty n cn
  echo "── состав построенной подачи ─────────────────────────────"
  while IFS='|' read -r ty n cn; do
    [ -n "$ty" ] || continue
    printf '  %s: %s\n' "$ty" "$n"
    total=$((total + n))
    pr=$((pr + cn))
    # Тип здесь — КОЛОНКА запроса, а не подстрока идентификатора: второго
    # выражения для вывода типа в этом тракте не заводится.
    case "$ty" in
      iam_user|iam_group|iam_role|iam_service_account|iam_access_binding) own=$((own + n));;
    esac
  done <<<"$comp"
  echo "  всего троек $total; из них пяти собственных типов iam — $own"
  echo "  класс «проектная роль»: троек — $pr"

  printf '%s\n' "$json" > "$out"
  echo "  подача записана: $out ($(wc -c <"$out") байт)"

  if [ "$own" -eq 0 ]; then
    die_precond "в построенной подаче ноль объектов пяти собственных типов iam. Это НЕ дефект генератора: в базе стенда нет ни одного живого объекта этих типов, у которого был бы администратор аккаунта. Проба расхождения на такой подаче об этих типах не утверждает ничего и обязана отказаться выносить вердикт"
  fi
}

cmd_fixture() {
  build_fixture "${1:-$FIXTURE_OUT}"
}

cmd_up() {
  local fx="$FIXTURE_OUT"
  build_fixture "$fx"
  [ -f "$TREE_SCRIPT" ] || die_precond "прибора нет в дереве: $TREE_SCRIPT"
  k get secret "$CERT_SECRET" >/dev/null 2>&1 \
    || die_precond "секрета с клиентским сертификатом $CERT_SECRET нет: генератору нечем предъявить себя внутреннему слушателю (mTLS)"

  echo "── настройки генератора ──────────────────────────────────"
  k create configmap "$SCRIPT_CM"  --from-file=internal_check.js="$TREE_SCRIPT" \
    --dry-run=client -o yaml | k apply -f - >/dev/null
  k create configmap "$FIXTURE_CM" --from-file=allow_tuples.json="$fx" \
    --dry-run=client -o yaml | k apply -f - >/dev/null
  echo "  $SCRIPT_CM ← $TREE_SCRIPT"
  echo "  $FIXTURE_CM ← $fx"

  echo "── генератор ─────────────────────────────────────────────"
  # Под пересоздаётся: `restartPolicy: Never` и смонтированные ConfigMap'ы
  # читаются при старте, поэтому «применить поверх» дало бы генератор со старой
  # подачей — ровно тот класс, из-за которого правка настроек не доезжает до
  # процесса.
  k delete pod "$RUNNER" --ignore-not-found --wait=true >/dev/null
  k apply -f - >/dev/null <<POD
apiVersion: v1
kind: Pod
metadata:
  name: ${RUNNER}
  namespace: ${NS}
  labels:
    app: ${RUNNER}
spec:
  restartPolicy: Never
  securityContext:
    fsGroup: 12345
  containers:
    - name: k6
      image: ${K6_IMAGE}
      imagePullPolicy: IfNotPresent
      command: ["sleep", "infinity"]
      resources:
        requests: { cpu: "2", memory: 1Gi }
        limits:   { cpu: "4", memory: 4Gi }
      securityContext:
        allowPrivilegeEscalation: false
        capabilities: { drop: ["ALL"] }
        readOnlyRootFilesystem: true
        runAsNonRoot: true
        runAsUser: 12345
        seccompProfile: { type: RuntimeDefault }
      volumeMounts:
        - { name: scripts,  mountPath: /scripts }
        - { name: fixtures, mountPath: /fixtures }
        - { name: certs,    mountPath: /certs }
        - { name: out,      mountPath: /out }
  volumes:
    - name: scripts
      configMap: { name: ${SCRIPT_CM} }
    - name: fixtures
      configMap: { name: ${FIXTURE_CM} }
    - name: certs
      secret: { secretName: ${CERT_SECRET}, defaultMode: 288 }
    - name: out
      emptyDir: {}
POD
  k wait --for=condition=Ready "pod/$RUNNER" --timeout=180s >/dev/null \
    || die "генератор $RUNNER не поднялся за 180 с; посмотрите: kubectl -n $NS describe pod $RUNNER"
  echo "  $RUNNER готов"

  # Под готов — это НЕ доказательство того, что подача доехала до тома: ConfigMap
  # монтируется отдельно от старта контейнера. Спрашивается ФАЙЛ В ПОДЕ.
  local seen
  seen=$(k exec "$RUNNER" -- sh -c 'wc -c < /fixtures/allow_tuples.json' 2>/dev/null || true)
  case "${seen:-0}" in
    ''|0|*[!0-9]*) die "подача в поде пуста или нечитаема — генератор поднят, а мерить ему нечем";;
  esac
  echo "  подача в поде: $seen байт"
}

cmd_down() {
  k delete pod "$RUNNER" --ignore-not-found --wait=false >/dev/null
  k delete configmap "$SCRIPT_CM" "$FIXTURE_CM" --ignore-not-found >/dev/null
  echo "снято: под $RUNNER, настройки $SCRIPT_CM и $FIXTURE_CM"
}

case "${1:-}" in
  fixture) shift; cmd_fixture "$@";;
  up)      cmd_up;;
  down)    cmd_down;;
  *)
    echo "использование: $(basename "$0") <fixture [путь]|up|down>" >&2
    exit 2;;
esac
