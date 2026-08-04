#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# drain_fga_outbox.sh — deterministic post-reseed grant-materialization gate.
#
# Root cause it fixes: seeding the RS256 AccessBinding matrix enqueues a BURST of
# owner/verb FGA tuples into kacho_iam.fga_outbox. A suite launched at matrix-age-0
# (before the iam reconciler drains that burst) hits the materialization window and
# the caller's freshly-granted principals get a 403 cascade on their own resources —
# the "reseed-warmup" race that reddened suite-1 of a serial run despite the resource
# authz being correct.
#
# A fixed `sleep 60` under-waits a large burst and over-waits a small one. This gate
# instead POLLS the healthy (non-poison) fga_outbox depth until the reconciler has
# caught up (== 0), bounded by BUDGET. Poison rows (sent_at IS NULL AND last_error
# non-empty) are permanent no-retry dead-letters — they never drain, so they are
# EXCLUDED from the wait (else the gate would always burn its full budget).
#
# When the iam Postgres is not directly reachable (CI without kubectl/psql exec into
# the pod), it degrades to a bounded settle sleep so it never hard-blocks.
#
# Usage: drain_fga_outbox.sh [budget_seconds]   (default 180)
set -uo pipefail
# KUBECONFIG НЕ ПОДМЕНЯЕТСЯ ВЫДУМАННЫМ ПУТЁМ.
#
# Здесь стояло `export KUBECONFIG=${KUBECONFIG:-/tmp/kacho.kubeconfig}`. Этого файла
# не создаёт НИКТО — ни CI, ни один скрипт дерева (проверено поиском по всему репо:
# путь встречается только в двух таких же строках и в одной записке). На машине, где
# оператор работает через `~/.kube/config` — то есть в обычном случае, и именно так
# работали все остальные шаги этого же прогона, — подстановка ЛОМАЛА рабочую
# настройку: kubectl переставал видеть кластер и уходил на legacy-умолчание
# `localhost:8080`.
#
# Следствие было тихим и постоянным: проба ниже возвращала пустую строку, гейт
# объявлял «iam DB not reachable» и уходил в слепой сон. То есть детерминированного
# дренажа, ради которого этот файл написан, не происходило НИ РАЗУ, а сообщение
# указывало на БД — на предмет, который в этот момент отвечал нормально. Отличать
# надо было не «ответила / не ответила», а «мы не туда стучимся» от «оно лежит»
# (security.md §8: постоянная неправильная настройка не должна становиться штатным
# режимом под видом мягкой деградации).
#
# Теперь путь берётся только если он ЗАДАН явно либо СУЩЕСТВУЕТ; иначе kubectl
# разрешает конфигурацию сам, как и остальные скрипты прогона.
if [ -n "${KUBECONFIG:-}" ]; then
  export KUBECONFIG
elif [ -f /tmp/kacho.kubeconfig ]; then
  export KUBECONFIG=/tmp/kacho.kubeconfig
fi

BUDGET=${1:-180}
NS=${KACHO_NS:-kacho}
POD=${KACHO_IAM_PG_POD:-kacho-umbrella-pg-iam-0}
FALLBACK=${DRAIN_FALLBACK:-60}
# healthy pending = enqueued-but-unsent AND not a permanent poison dead-letter.
Q="SELECT count(*) FROM kacho_iam.fga_outbox WHERE sent_at IS NULL AND coalesce(last_error,'')='';"

ERRF=$(mktemp); trap 'rm -f "$ERRF"' EXIT

_hp() {
  kubectl -n "$NS" exec "$POD" -c postgresql -- \
    sh -c "PGPASSWORD=\"\$POSTGRES_PASSWORD\" psql -U iam -d kacho_iam -h 127.0.0.1 -tAc \"$Q\"" \
    2>"$ERRF" | tr -d '[:space:]'
}

probe=$(_hp || true)
if ! [[ "$probe" =~ ^[0-9]+$ ]]; then
  # ПРИЧИНА ПЕЧАТАЕТСЯ. Прежде stderr уходил в /dev/null, поэтому единственное, что
  # видел читатель, — вывод «БД недоступна», и он был неверен в самом частом случае.
  # Отказ, у которого не видно причины, неотличим от отказа, которого не чинят.
  echo "[drain-gate] дренаж НЕ ВЫПОЛНЕН: проба глубины fga_outbox не дала числа" >&2
  echo "[drain-gate]   ns=$NS pod=$POD kubeconfig=${KUBECONFIG:-<по умолчанию kubectl>}" >&2
  echo "[drain-gate]   ответ kubectl/psql: $(grep -a -m1 . "$ERRF" 2>/dev/null || echo '<пусто>')" >&2
  echo "[drain-gate] откат на ограниченный сон ${FALLBACK}s — волна iam пойдёт БЕЗ" >&2
  echo "[drain-gate]   детерминированного оседания очереди; если суита iam покраснеет" >&2
  echo "[drain-gate]   на материализации грантов, причина в первую очередь здесь." >&2
  sleep "$FALLBACK"
  exit 0
fi

i=0
hp="$probe"
while (( i < BUDGET )); do
  [[ "$hp" =~ ^[0-9]+$ ]] || hp=999
  echo "[drain-gate] healthy_pending=$hp (t=${i}s/${BUDGET}s)" >&2
  (( hp == 0 )) && { echo "[drain-gate] CLEAR — reconciler caught up" >&2; exit 0; }
  sleep 3
  i=$(( i + 3 ))
  hp=$(_hp || echo 999)
done
echo "[drain-gate] budget ${BUDGET}s spent (healthy_pending=$hp) — proceeding anyway" >&2
exit 0
