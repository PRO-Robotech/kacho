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
export KUBECONFIG=${KUBECONFIG:-/tmp/kacho.kubeconfig}

BUDGET=${1:-180}
NS=${KACHO_NS:-kacho}
POD=${KACHO_IAM_PG_POD:-kacho-umbrella-pg-iam-0}
FALLBACK=${DRAIN_FALLBACK:-60}
# healthy pending = enqueued-but-unsent AND not a permanent poison dead-letter.
Q="SELECT count(*) FROM kacho_iam.fga_outbox WHERE sent_at IS NULL AND coalesce(last_error,'')='';"

_hp() {
  kubectl -n "$NS" exec "$POD" -c postgresql -- \
    sh -c "PGPASSWORD=\"\$POSTGRES_PASSWORD\" psql -U iam -d kacho_iam -h 127.0.0.1 -tAc \"$Q\"" \
    2>/dev/null | tr -d '[:space:]'
}

probe=$(_hp || true)
if ! [[ "$probe" =~ ^[0-9]+$ ]]; then
  echo "[drain-gate] iam DB not reachable (kubectl/psql exec) — bounded ${FALLBACK}s settle fallback" >&2
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
