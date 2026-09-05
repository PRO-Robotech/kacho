#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# authz-edge-census.sh — ПЕРЕПИСЬ «ЦЕПЬ ПРОТИВ ЖИВОЙ РЕГИСТРАЦИИ», обе полярности.
#
# ПРЕДМЕТ. Регистрация ресурса пишет ДВЕ половины одной транзакцией: строку
# `kaname.resource_mirror` и цепь рёбер `kaname.resource_parent_edge`.
# Половины обязаны существовать вместе. База этого не держит и держать не может:
# внешнего ключа между ними НЕТ — стороны названы разными словарями (зеркало —
# словарём каталога `vpc.securityGroup`, ребро — словарём модели прав
# `vpc_security_group`), поэтому каскад тут невыразим by construction. Значит
# сходимость половин — предмет переписи, а не схемы.
#
# ОБЕ ПОЛЯРНОСТИ НАЗЫВАЮТСЯ ОТДЕЛЬНО, и это не оформление — у них РАЗНАЯ починка:
#   • ребро есть, регистрации нет — снятие не убрало цепь. Обход ВНИЗ («что лежит
#     под этой областью») продолжает числить снятый объект под областью выдачи:
#     право пережило свой предмет.
#   • регистрация есть, ребра нет — цепь не записалась (владелец промолчал, либо
#     доставка не применилась). Вопрос о доступе поднимается по цепи, цепи нет, и
#     отказ НЕОТЛИЧИМ от честного.
# Одно число на обе не отвечает ни на что.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ СОПОСТАВЛЕНИЕ ИДЁТ ПО ИДЕНТИФИКАТОРУ, А НЕ ПО ПАРЕ (ТИП, ИДЕНТИФИКАТОР)
#
# Перевод между двумя словарями живёт В GO (`authzmap.ModelTypeName`) и в SQL не
# существует: миграция 0091 ставит на цепь лишь проверку формы (`NOT LIKE '%.%'`),
# а не таблицу соответствий. Выписать соответствие здесь значило бы завести ВТОРОЙ
# словарь, который разойдётся с первым молча — и разойдётся именно там, где
# расхождение не видно: на новом типе, которого в копии ещё нет.
#
# Идентификатор ресурса ГЛОБАЛЬНО-УНИКАЛЕН by construction (crockford-base32 с
# префиксом типа, запрет #15), поэтому сопоставление по нему однозначно и словаря
# не требует. Цена названа: перепись не увидит ребра, записанного с ВЕРНЫМ
# идентификатором и ЧУЖИМ типом. Это другой дефект с другой починкой, и его ловит
# проверка схемы плюс построчная сверка посевщика с производителем.
#
# ─────────────────────────────────────────────────────────────────────────────
# КЛАССИФИКАЦИЯ ТИПА — ПО ДЕРЕВУ ДАННЫХ, А НЕ ПО СПИСКУ ИМЁН
#
# Не всякий тип в цепи производится регистрацией: у объектов самой службы прав
# (проект, аккаунт) строки зеркала нет и не будет — они не проходят через
# посредника. Список таких имён здесь НЕ выписывается: он протух бы при заведении
# первого же нового звена (см. #785), причём молча, превратив законную цепь в
# находку.
#
# Вместо списка — признак: тип считается производным от регистрации, если ХОТЬ У
# ОДНОГО его объекта строка зеркала есть. Тип, у которого таких объектов ноль,
# попадает в отдельную корзину и называется ВСЛУХ — он не «ноль находок», он
# «перепись про него ничего не знает». Иначе целиком снятый тип выглядел бы как
# чистый.
#
# ИСПОЛЬЗОВАНИЕ:
#   deploy/scripts/authz-edge-census.sh              # перепись
#   deploy/scripts/authz-edge-census.sh --show 20    # + первые N расхождений
#
# КОДЫ ВОЗВРАТА различают исходы, потому что «расхождений 0» и «перепись негодна» —
# разные ответы:
#   0 — перепись снята, необъяснённых расхождений нет;
#   1 — перепись снята, есть расхождение;
#   2 — перепись НЕГОДНА (нет доступа к базе, обе таблицы пусты).
set -euo pipefail

NAMESPACE="${KACHO_NAMESPACE:-kacho}"
PG_IAM_POD="${KACHO_PG_IAM_POD:-kacho-umbrella-pg-iam-0}"
SHOW=0
[[ "${1:-}" == "--show" ]] && SHOW="${2:-20}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

psql_iam() {
  kubectl -n "$NAMESPACE" exec -i "$PG_IAM_POD" -c postgresql -- \
    psql -U postgres -d kacho_iam -tA -F '|' 2>/dev/null
}

echo "=== перепись «цепь против живой регистрации» ==="
echo "пространство имён: ${NAMESPACE}, база: ${PG_IAM_POD}"

# ── ОБЪЁМ ОСМОТРЕННОГО ───────────────────────────────────────────────────────
# Печатается ВСЕГДА и первым: «ноль находок» обязано быть отличимо от «ноль
# прочитанного».
if ! VOLUME="$(psql_iam <<'SQL'
SELECT (SELECT count(*) FROM kaname.resource_parent_edge),
       (SELECT count(DISTINCT object_id) FROM kaname.resource_parent_edge),
       (SELECT count(*) FROM kaname.resource_mirror);
SQL
)"; then
  echo "ПЕРЕПИСЬ НЕГОДНА: база iam недоступна (${PG_IAM_POD})" >&2
  exit 2
fi
EDGES="${VOLUME%%|*}"; REST="${VOLUME#*|}"
EDGE_OBJS="${REST%%|*}"; MIRROR="${REST#*|}"

echo
echo "осмотрено: рёбер ${EDGES} (объектов ${EDGE_OBJS}), строк зеркала ${MIRROR}"

if [[ "$EDGES" -eq 0 && "$MIRROR" -eq 0 ]]; then
  echo "ПЕРЕПИСЬ НЕГОДНА: обе таблицы пусты — судить нечего" >&2
  exit 2
fi

# ── ПЕРЕПИСЬ ПО ТИПАМ, ОБЕ ПОЛЯРНОСТИ ────────────────────────────────────────
psql_iam > "$WORK/by_type.txt" <<'SQL'
WITH e AS (
  SELECT pe.object_type,
         pe.object_id,
         EXISTS (SELECT 1 FROM kaname.resource_mirror m WHERE m.object_id = pe.object_id) AS mirrored
    FROM kaname.resource_parent_edge pe
)
SELECT object_type,
       count(*)                                   AS edges,
       count(*) FILTER (WHERE mirrored)           AS with_mirror,
       count(*) FILTER (WHERE NOT mirrored)       AS without_mirror
  FROM e GROUP BY 1 ORDER BY 4 DESC, 1;
SQL

# Обратная полярность: строка зеркала без единого ребра.
psql_iam > "$WORK/mirror_only.txt" <<'SQL'
SELECT m.object_type, m.object_id
  FROM kaname.resource_mirror m
 WHERE NOT EXISTS (SELECT 1 FROM kaname.resource_parent_edge pe
                    WHERE pe.object_id = m.object_id)
 ORDER BY 1, 2;
SQL

RESIDUAL=0; UNCLASSIFIED=0
: > "$WORK/residual_types.txt"; : > "$WORK/unclassified_types.txt"

echo
echo "  тип (словарь модели)              рёбер   с зерк.  БЕЗ зерк."
while IFS='|' read -r ty edges with without; do
  [[ -z "$ty" ]] && continue
  printf '  %-32s %7s %9s %9s\n' "$ty" "$edges" "$with" "$without"
  if [[ "$with" -gt 0 ]]; then
    RESIDUAL=$(( RESIDUAL + without ))
    [[ "$without" -gt 0 ]] && echo "$ty|$without" >> "$WORK/residual_types.txt"
  else
    # Ни один объект типа не имеет строки зеркала — тип регистрацией не
    # производится ЛИБО снят целиком. Перепись про него не судит и говорит об этом.
    UNCLASSIFIED=$(( UNCLASSIFIED + edges ))
    echo "$ty|$edges" >> "$WORK/unclassified_types.txt"
  fi
done < "$WORK/by_type.txt"

MIRROR_ONLY=$(wc -l < "$WORK/mirror_only.txt" | tr -d ' ')

echo
echo "  ребро есть, регистрации нет : ${RESIDUAL}"
echo "  регистрация есть, ребра нет : ${MIRROR_ONLY}"
echo "  вне суждения переписи       : ${UNCLASSIFIED} (типы, у которых зеркала нет НИ У ОДНОГО объекта)"
echo

if [[ "$UNCLASSIFIED" -gt 0 ]]; then
  echo "--- вне суждения (тип|рёбер) ---"
  cat "$WORK/unclassified_types.txt"
  echo "Это НЕ «чисто»: у типов службы прав строки зеркала нет by construction,"
  echo "а у снятого целиком типа её не осталось. Разбирать по типу, не по числу."
  echo
fi

if [[ "$SHOW" -gt 0 ]]; then
  if [[ "$RESIDUAL" -gt 0 ]]; then
    echo "--- цепь пережила снятие, первые ${SHOW} ---"
    psql_iam <<SQL | head -n "$SHOW"
SELECT pe.object_type || ':' || pe.object_id || ' -> ' || pe.parent_type || ':' || pe.parent_id
  FROM kaname.resource_parent_edge pe
 WHERE NOT EXISTS (SELECT 1 FROM kaname.resource_mirror m WHERE m.object_id = pe.object_id)
   AND EXISTS (SELECT 1 FROM kaname.resource_parent_edge s
                JOIN kaname.resource_mirror m2 ON m2.object_id = s.object_id
               WHERE s.object_type = pe.object_type)
 ORDER BY 1;
SQL
    echo
  fi
  if [[ "$MIRROR_ONLY" -gt 0 ]]; then
    echo "--- регистрация без цепи, первые ${SHOW} ---"
    head -n "$SHOW" "$WORK/mirror_only.txt"
    echo
  fi
fi

RC=0
if [[ "$RESIDUAL" -gt 0 ]]; then
  echo "НАХОДКА: ${RESIDUAL} ребро(рёбер) пережило снятие регистрации. Обход вниз"
  echo "числит эти объекты под областью выдачи, хотя регистрации у них нет."
  RC=1
fi
if [[ "$MIRROR_ONLY" -gt 0 ]]; then
  echo "НАХОДКА: у ${MIRROR_ONLY} живых регистраций нет НИ ОДНОГО ребра. Вопрос о"
  echo "доступе поднимается по цепи, цепи нет, и отказ неотличим от честного."
  RC=1
fi
[[ "$RC" -eq 0 ]] && echo "расхождений нет"
exit "$RC"
