#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# openfga-datastore-durable-test.sh — хранилище ПРАВ обязано пережить рестарт пода.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ЛОВИТ
#
# Движок прав (OpenFGA) хранит кортежи доступа — то, чем каждый арендатор владеет
# и что кому выдано. Подчарт по умолчанию поднимает его на движке `memory`: рабочий,
# отвечающий, проходящий любой e2e — и теряющий ВСЁ содержимое при первом же
# рестарте пода. Наблюдаемого симптома у этого состояния нет ровно до момента
# потери: до рестарта memory и postgres отвечают одинаково.
#
# Профиль при этом может поднимать выделенный Postgres ДЛЯ ЭТОГО ЖЕ движка и не
# указывать на него — инстанс крутится, место занимает, и не участвует ни в чём.
# Именно так и было: два профиля объявляли `pg-openfga.enabled: true` и ни один
# из них не объявлял `openfga.datastore`, а умолчание подчарта — memory.
#
# Это тот же класс, что «проверка с формой, но без содержания», только про
# состояние: ХРАНИЛИЩЕ С ФОРМОЙ, НО БЕЗ ДОЛГОВЕЧНОСТИ. Инстанс есть, ссылки нет.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — на КАЖДОМ разворачиваемом профиле
#
#   (1) движок прав, который вообще рендерится, НИКОГДА не на `memory`;
#   (2) ПАРА, ради которой написан гейт: инстанс БД для прав отрендерился
#       ⇒ движок = postgres (иначе поднятый Postgres не участвует ни в чём);
#   (3) движок = postgres ⇒ адрес хранилища ПРОВЯЗАН (OPENFGA_DATASTORE_URI
#       присутствует) — движок, называющий хранилище, до которого не дотянуться,
#       это отказ старта, а не долговечность.
#
# (1) существует отдельно от (2) НАМЕРЕННО. Без него гейт снимается выключением
# выделенного Postgres: пара становится вакуумной, гейт молчит, а потеря прав
# остаётся на месте. Послабление, воссоздающее дефект, от которого спасает.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧЕГО НЕ УТВЕРЖДАЕТ
#
#   • что сам том Postgres переживает рестарт СВОЕГО пода. Это отдельный предмет
#     и отдельное решение: на кластере fe3455 нет динамического провижининга,
#     поэтому ВСЕ десять инстансов Postgres и хранилище образов идут на emptyDir —
#     объявлено в values.fe3455.yaml («NO StorageClass … Data is ephemeral»).
#     Здесь это не судится: гейт про то, чтобы движок ССЫЛАЛСЯ на хранилище, а
#     не про класс тома под ним;
#   • что содержимое DSN верное (рантайм);
#   • что шифрование транспорта к этой БД включено — это утверждают соседние
#     проверки посадки.
#
# Офлайновый харнесс над `helm template`, кластер не нужен. Зеркалит tests/helm/*.
# Самопроверка: --self-test (гейт обязан краснеть на внесённом дефекте и молчать
# на законной конструкции той же формы).
set -uo pipefail

SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"

# ── Профили, которые РЕАЛЬНО разворачиваются ────────────────────────────────
# Тот же состав и тот же порядок наложения, что у соседних проверок посадки
# (admin-hop-pod-shape-test.sh, outbox-autovacuum-naptime-test.sh). Формат:
# <имя>|<файл> <файл> …  — файлы накладываются слева направо, как в рецепте.
PROFILES=(
  "dev-prod|values.dev.yaml values.dev-prod.yaml"
  "prod|values.prod.yaml"
  "fe3455|values.prod.yaml values.fe3455.yaml values.fe3455-prod.yaml"
  "prorobotech|values.dev.yaml values.prorobotech.yaml"
)

VIOLATIONS=0
# Объём осмотренного — печатается ВСЕГДА. «Ноль находок» обязано быть отличимо
# от «ноль прочитанного»: гейт, отрендеривший ноль профилей, зелёный ровно так же.
PROFILES_SEEN=0
DOCS_SEEN=0
ENGINE_DECLS=0   # контейнеров, несущих OPENFGA_DATASTORE_ENGINE
PG_STS_SEEN=0    # отрендеренных StatefulSet'ов инстанса БД для прав

violation() { echo "FAIL: $1"; VIOLATIONS=$((VIOLATIONS + 1)); }
fatal() { echo "FATAL: $1"; exit 2; }

# ── Предпосылка гейта, проверяемая гейтом ───────────────────────────────────
# Все утверждения ниже написаны на фильтрах mikefarah yq v4. Одноимённая обёртка
# над jq (python-yq) на них молча отдаёт ПУСТО — и тогда «ноль находок» означает
# «ничего не прочитано», а гейт выходит зелёным, ничего не сверив.
command -v yq >/dev/null 2>&1 || fatal "yq не найден — нужен mikefarah yq v4"
yq --version 2>&1 | grep -q "mikefarah" \
  || fatal "в PATH не тот yq: $(yq --version 2>&1 | head -1) — нужен mikefarah yq v4, не обёртка над jq"
command -v helm >/dev/null 2>&1 || fatal "helm не найден"

# render <файлы…> [--set …] — рендер профиля; предупреждения helm про kubeconfig
# в вывод не попадают, ошибки рендера — фатальны (нечего судить).
render() {
  local files="$1"; shift
  local args=()
  local f
  for f in $files; do args+=(-f "$UMBRELLA/$f"); done
  helm template kacho-umbrella "$UMBRELLA" "${args[@]}" "$@" 2>/dev/null
}

# ── Извлечение — из ОТРЕНДЕРЕННОГО манифеста, не из values ──────────────────
# Судится исход (что поедет в кластер), а не объявление: значение движка приходит
# из умолчания подчарта, поэтому в values профиля его может не быть вовсе —
# именно так дефект и выглядел.
engines() {   # значения OPENFGA_DATASTORE_ENGINE, по одному на строку
  echo "$1" | yq ea 'select(.kind=="Deployment" or .kind=="StatefulSet")
    | .spec.template.spec.containers[]?.env[]? | select(.name=="OPENFGA_DATASTORE_ENGINE") | .value' - 2>/dev/null \
    | grep -v '^null$' || true
}
uri_wired() { # непусто, если адрес хранилища провязан (значением или ссылкой на секрет)
  echo "$1" | yq ea 'select(.kind=="Deployment" or .kind=="StatefulSet")
    | .spec.template.spec.containers[]?.env[]? | select(.name=="OPENFGA_DATASTORE_URI") | .name' - 2>/dev/null \
    | grep -v '^null$' || true
}
pg_sts() {    # имена StatefulSet'ов инстанса БД для прав
  echo "$1" | yq ea 'select(.kind=="StatefulSet") | .metadata.name' - 2>/dev/null \
    | grep -- '-pg-openfga$' || true
}
docs() { echo "$1" | grep -c '^---' || true; }

# judge <имя-профиля> <рендер> — три утверждения выше. Ничего не печатает при
# отсутствии находок: печатает вердикт вызывающий, по счётчикам.
judge() {
  local name="$1" r="$2"
  local eng uri sts n_eng
  eng="$(engines "$r")"
  uri="$(uri_wired "$r")"
  sts="$(pg_sts "$r")"
  n_eng="$(printf '%s' "$eng" | grep -c . || true)"

  DOCS_SEEN=$((DOCS_SEEN + $(docs "$r")))
  ENGINE_DECLS=$((ENGINE_DECLS + n_eng))
  PG_STS_SEEN=$((PG_STS_SEEN + $(printf '%s' "$sts" | grep -c . || true)))

  # (1) движок прав, который рендерится, никогда не на memory.
  local e
  for e in $eng; do
    [ "$e" = "memory" ] && violation "профиль $name: движок прав на OPENFGA_DATASTORE_ENGINE=memory — кортежи доступа теряются при рестарте пода (весь выданный арендаторам доступ)"
  done

  # (2) ПАРА: инстанс БД для прав отрендерился ⇒ движок = postgres.
  if [ -n "$sts" ] && [ "$n_eng" -gt 0 ]; then
    for e in $eng; do
      [ "$e" = "postgres" ] || violation "профиль $name: отрендерен инстанс БД для прав ($(printf '%s' "$sts" | tr '\n' ' ')), а движок = ${e:-<пусто>} — поднятый для него Postgres не участвует ни в чём"
    done
  fi

  # (3) движок = postgres ⇒ адрес хранилища провязан.
  if printf '%s\n' $eng | grep -qx postgres && [ -z "$uri" ]; then
    violation "профиль $name: движок = postgres, но OPENFGA_DATASTORE_URI не провязан — движок называет хранилище, до которого не дотянуться (отказ старта, а не долговечность)"
  fi
}

main() {
  local p name files r
  for p in "${PROFILES[@]}"; do
    name="${p%%|*}"; files="${p#*|}"
    r="$(render "$files")"
    [ -n "$r" ] || fatal "профиль $name ($files) не рендерится или пуст — судить нечего (собраны ли зависимости? helm dep update)"
    PROFILES_SEEN=$((PROFILES_SEEN + 1))
    judge "$name" "$r"
  done

  # ── Предпосылка: предмет вообще существует в дереве ────────────────────────
  # Если ни один профиль не несёт ни движка прав, ни его инстанса БД — гейт не
  # «нашёл ноль нарушений», а не нашёл ПРЕДМЕТА: имя переменной сменилось,
  # подчарт заменён, состав профилей разъехался. Молчать в этом случае значит
  # выдавать слепоту за чистоту.
  [ "$ENGINE_DECLS" -gt 0 ] || fatal "ни один из $PROFILES_SEEN профилей не рендерит OPENFGA_DATASTORE_ENGINE — предмета не найдено (сменилось имя переменной или подчарт?), а не «нарушений нет»"
  [ "$PG_STS_SEEN" -gt 0 ] || fatal "ни один из $PROFILES_SEEN профилей не рендерит StatefulSet *-pg-openfga — предмета пары не найдено, а не «нарушений нет»"

  echo "осмотрено: профилей $PROFILES_SEEN, документов $DOCS_SEEN, объявлений движка прав $ENGINE_DECLS, инстансов БД для прав $PG_STS_SEEN"
  if [ "$VIOLATIONS" -gt 0 ]; then
    echo "FAIL: $SCRIPT — нарушений $VIOLATIONS"
    exit 1
  fi
  echo "PASS: $SCRIPT (профилей $PROFILES_SEEN, нарушений 0)"
}

# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА — инъекция в ОБЕ стороны
#
# (A) вносим ровно исходный дефект (движок обратно на memory) — гейт обязан
#     покраснеть И НАЗВАТЬ ПРОФИЛЬ. Без координаты красный бесполезен;
# (B) законная конструкция той же формы — гейт обязан СМОЛЧАТЬ. Двух видов,
#     потому что «молчит» бывает по двум разным законным причинам:
#       B1 — движка прав нет на стенде вовсе (сокращённая посадка);
#       B2 — движок на postgres, но хранилище ВНЕШНЕЕ (управляемая БД), поэтому
#            своего инстанса не рендерится. Пара вакуумна законно.
#     Без (B) гейт ловил бы форму («нет своего Postgres» / «есть слово memory»),
#     а не существо, и первый же законный профиль его бы отключил.
self_test() {
  local rc=0 r out
  echo "=== $SCRIPT --self-test ==="

  # (0) дерево как есть
  if out="$(main 2>&1)"; then
    echo "  (0) дерево как есть                                   → МОЛЧИТ ($(printf '%s' "$out" | tail -2 | head -1))"
  else
    echo "  (0) дерево как есть                                   → КРАСНЫЙ:"
    printf '%s\n' "$out" | sed 's/^/        /'
    rc=1
  fi

  # (A) ИНЪЕКЦИЯ: движок обратно на memory в профиле prod
  VIOLATIONS=0
  r="$(render "values.prod.yaml" --set openfga.datastore.engine=memory)"
  out="$(judge prod "$r" 2>&1)"
  if printf '%s' "$out" | grep -q 'memory' && printf '%s' "$out" | grep -q 'профиль prod'; then
    echo "  (A) движок возвращён на memory (prod)                 → КРАСНЫЙ с координатой: $(printf '%s' "$out" | head -1)"
  else
    echo "  (A) движок возвращён на memory (prod)                 → ПРОПУСТИЛ (вывод: ${out:-<пусто>})"; rc=1
  fi

  # (B1) КОНТРОЛЬ: движка прав на стенде нет вовсе
  VIOLATIONS=0
  r="$(render "values.prod.yaml" --set openfga.enabled=false --set pg-openfga.enabled=false)"
  out="$(judge b1 "$r" 2>&1)"
  if [ -z "$out" ]; then
    echo "  (B1) движка прав нет на стенде вовсе                  → МОЛЧИТ"
  else
    echo "  (B1) движка прав нет на стенде вовсе                  → ЛОЖНЫЙ КРАСНЫЙ: $out"; rc=1
  fi

  # (B2) КОНТРОЛЬ: postgres, но хранилище внешнее (своего инстанса нет)
  VIOLATIONS=0
  r="$(render "values.prod.yaml" --set pg-openfga.enabled=false \
        --set openfga.datastore.engine=postgres --set openfga.datastore.uriSecret=external-fga-dsn)"
  out="$(judge b2 "$r" 2>&1)"
  if [ -z "$out" ]; then
    echo "  (B2) postgres на ВНЕШНЕЙ БД (своего инстанса нет)     → МОЛЧИТ"
  else
    echo "  (B2) postgres на ВНЕШНЕЙ БД (своего инстанса нет)     → ЛОЖНЫЙ КРАСНЫЙ: $out"; rc=1
  fi

  # (C) ИНЪЕКЦИЯ в третье утверждение: postgres без провязанного адреса
  VIOLATIONS=0
  r="$(render "values.prod.yaml" --set openfga.datastore.uri=null --set openfga.datastore.uriSecret=null \
        --set openfga.datastore.existingSecret=null)"
  out="$(judge c "$r" 2>&1)"
  if printf '%s' "$out" | grep -q 'OPENFGA_DATASTORE_URI'; then
    echo "  (C) postgres без провязанного адреса хранилища        → КРАСНЫЙ: $(printf '%s' "$out" | head -1)"
  else
    echo "  (C) postgres без провязанного адреса хранилища        → ПРОПУСТИЛ (вывод: ${out:-<пусто>})"; rc=1
  fi

  [ "$rc" -eq 0 ] && echo "SELF-TEST PASS: $SCRIPT" || echo "SELF-TEST FAIL: $SCRIPT"
  exit "$rc"
}

[ "${1:-}" = "--self-test" ] && self_test
main
