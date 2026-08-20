#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# authz-journal-census.sh — ПЕРЕПИСЬ «ДВИЖОК ПРОТИВ ЖУРНАЛА», обе полярности.
#
# ПРЕДМЕТ. Миграция 0098 объявляет инвариант: состояние чужого хранилища отношений
# есть свёртка одного журнала kacho_iam.fga_outbox. Инвариант держит гейт дерева
# (tools/authzenginecensus/engineplaces/journaldoor_test.go) — он смотрит на КОД.
# Этот скрипт смотрит на СТЕНД: сходится ли фактически.
#
# ОБЕ ПОЛЯРНОСТИ НАЗЫВАЮТСЯ ОТДЕЛЬНО, и это не оформление:
#   • в движке, нет в журнале — запись мимо журнала. Проекция relation_fact таких
#     прав не увидит НИКОГДА: форма E ответит «нет» там, где движок отвечает «да».
#   • в журнале, нет в движке — журнал не доехал (дренаж отстал, строка отравлена).
#     Это ДРУГОЙ дефект с другой починкой, и одно число на оба не отвечает ни на что.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЛОВУШКА, ИЗ-ЗА КОТОРОЙ СВЁРТКА ЗДЕСЬ ТАКАЯ, А НЕ ПРОЩЕ. Строка журнала несёт
# НАБОР отношений в `relations`, а `relation` — лишь совместимостное эхо первого
# элемента, и у строк СНЯТИЯ его нет вовсе. Наивная свёртка по `relation`:
#   • отбрасывает ВСЕ строки снятия (на замере 2026-08-20 — 159 604 из 359 508);
#   • берёт у строк записи одно отношение из набора вместо всех.
# Оба перекоса действуют в РАЗНЫЕ стороны и не гасят друг друга: такая свёртка
# дала «в движке, нет в журнале = 5358» там, где верный ответ — 2. Число выглядело
# правдоподобно и было ложным, поэтому свёртка ниже РАЗВОРАЧИВАЕТ `relations`.
#
# ИСПОЛЬЗОВАНИЕ:
#   deploy/scripts/authz-journal-census.sh              # перепись
#   deploy/scripts/authz-journal-census.sh --show 20    # + первые N расхождений
#
# КОДЫ ВОЗВРАТА различают исходы, потому что «расхождений 0» и «перепись негодна» —
# разные ответы:
#   0 — перепись снята, расхождений нет либо они объяснены объявленным исключением;
#   1 — перепись снята, есть НЕОБЪЯСНЁННОЕ расхождение;
#   2 — перепись НЕГОДНА (нет доступа к одной из баз, пустая выборка).
set -euo pipefail

NAMESPACE="${KACHO_NAMESPACE:-kacho}"
PG_IAM_POD="${KACHO_PG_IAM_POD:-kacho-umbrella-pg-iam-0}"
PG_FGA_POD="${KACHO_PG_FGA_POD:-kacho-umbrella-pg-openfga-0}"
SHOW=0
[[ "${1:-}" == "--show" ]] && SHOW="${2:-20}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# ОБЪЯВЛЕННОЕ ИСКЛЮЧЕНИЕ. Посев чарта ставит кортежи одиночки-кластера ДО того,
# как журнал вообще может быть применён (хранилище и модель создаёт он же).
# Перечень ДОСЛОВНО зеркалит ведомость гейта дерева; третий кортеж роняет и его,
# и эту перепись — послабление ограничено набором, а не выдано на предъявителя.
declare -a DECLARED_BYPASS=(
  "cluster:cluster_kacho_root|system_viewer|user:bootstrap_marker"
  "cluster:cluster_kacho_root|viewer|user:*"
)

echo "=== перепись «движок против журнала» ==="
echo "пространство имён: ${NAMESPACE}"

# ── сторона ДВИЖКА ───────────────────────────────────────────────────────────
if ! kubectl -n "$NAMESPACE" exec "$PG_FGA_POD" -- bash -c \
  'PGPASSWORD=$POSTGRES_PASSWORD psql -U openfga -d openfga -tAc \
     "select object_type||'"'"':'"'"'||object_id||'"'"'|'"'"'||relation||'"'"'|'"'"'||_user from tuple;"' \
  2>/dev/null | sed '/^$/d' | LC_ALL=C sort -u > "$WORK/engine.txt"; then
  echo "ПЕРЕПИСЬ НЕГОДНА: база движка недоступна (${PG_FGA_POD})" >&2
  exit 2
fi

# ── сторона ЖУРНАЛА ──────────────────────────────────────────────────────────
# Свёртка: развернуть `relations`, взять ПОСЛЕДНЕЕ событие по ключу кортежа
# (идентификатор строки монотонен и задаёт порядок применения), оставить те, где
# последним было «записать».
if ! kubectl -n "$NAMESPACE" exec "$PG_IAM_POD" -- bash -c \
  "PGPASSWORD=\$POSTGRES_PASSWORD psql -U postgres -d kacho_iam -tAc \"
     with expanded as (
       select o.id, o.event_type,
              o.payload->>'user'   as usr,
              o.payload->>'object' as obj,
              coalesce(r.rel, o.payload->>'relation') as rel
         from kacho_iam.fga_outbox o
         left join lateral jsonb_array_elements_text(
                  case when jsonb_typeof(o.payload->'relations')='array'
                       then o.payload->'relations' end) as r(rel) on true
        where o.payload ? 'user' and o.payload ? 'object'
     ), folded as (
       select distinct on (usr, rel, obj) usr, rel, obj, event_type
         from expanded where rel is not null
        order by usr, rel, obj, id desc
     )
     select obj||'|'||rel||'|'||usr from folded where event_type='fga.tuple.write';\"" \
  2>/dev/null | sed '/^$/d' | LC_ALL=C sort -u > "$WORK/journal.txt"; then
  echo "ПЕРЕПИСЬ НЕГОДНА: база iam недоступна (${PG_IAM_POD})" >&2
  exit 2
fi

ENGINE=$(wc -l < "$WORK/engine.txt")
JOURNAL=$(wc -l < "$WORK/journal.txt")

# ПРЕДПОСЫЛКА. Пустая выборка с ЛЮБОЙ стороны означает, что перепись читала не то,
# а не что расхождений нет: «ноль находок» обязано быть отличимо от «ноль
# прочитанного».
if [[ "$ENGINE" -eq 0 || "$JOURNAL" -eq 0 ]]; then
  echo "ПЕРЕПИСЬ НЕГОДНА: пустая выборка (движок ${ENGINE}, журнал ${JOURNAL}) — судить нечего" >&2
  exit 2
fi

LC_ALL=C comm -23 "$WORK/engine.txt" "$WORK/journal.txt" > "$WORK/engine_only.txt"
LC_ALL=C comm -13 "$WORK/engine.txt" "$WORK/journal.txt" > "$WORK/journal_only.txt"

# вычесть объявленное исключение — с причиной, а не молча
: > "$WORK/declared.txt"
for t in "${DECLARED_BYPASS[@]}"; do echo "$t"; done | LC_ALL=C sort -u > "$WORK/declared.txt"
LC_ALL=C comm -23 "$WORK/engine_only.txt" "$WORK/declared.txt" > "$WORK/engine_only_unexplained.txt"
LC_ALL=C comm -12 "$WORK/engine_only.txt" "$WORK/declared.txt" > "$WORK/engine_only_declared.txt"

E_ONLY=$(wc -l < "$WORK/engine_only.txt")
E_DECL=$(wc -l < "$WORK/engine_only_declared.txt")
E_UNEX=$(wc -l < "$WORK/engine_only_unexplained.txt")
J_ONLY=$(wc -l < "$WORK/journal_only.txt")

echo
echo "осмотрено: кортежей в движке ${ENGINE}, свёртка журнала даёт ${JOURNAL}"
echo
echo "  в движке, нет в журнале : ${E_ONLY}  (объявлено исключением ${E_DECL}, НЕОБЪЯСНЕНО ${E_UNEX})"
echo "  в журнале, нет в движке : ${J_ONLY}"
echo

if [[ "$SHOW" -gt 0 ]]; then
  [[ "$E_UNEX" -gt 0 ]] && { echo "--- необъяснённые (движок без журнала), первые ${SHOW} ---"; head -n "$SHOW" "$WORK/engine_only_unexplained.txt"; echo; }
  [[ "$J_ONLY" -gt 0 ]] && { echo "--- журнал не доехал, первые ${SHOW} ---"; head -n "$SHOW" "$WORK/journal_only.txt"; echo; }
fi

RC=0
if [[ "$E_UNEX" -gt 0 ]]; then
  echo "НАХОДКА: ${E_UNEX} кортеж(ей) есть в движке и НЕТ в журнале, и они не покрыты"
  echo "объявленным исключением. Права по ним у движка есть, у формы E — нет."
  RC=1
fi
if [[ "$J_ONLY" -gt 0 ]]; then
  echo "НАХОДКА: ${J_ONLY} кортеж(ей) есть в журнале и НЕ доехали до движка —"
  echo "это ДРУГОЙ дефект (отставший или заклиненный дренаж), чинится не там же."
  RC=1
fi
[[ "$RC" -eq 0 ]] && echo "состояние движка есть свёртка журнала: расхождений нет"
exit "$RC"
