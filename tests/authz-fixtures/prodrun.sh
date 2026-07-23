#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# prodrun.sh — production-mode newman driver (#59).
#   Seeds the RS256 SA-principal matrix ONCE (cached in /tmp/matrix.json), patches
#   the target service's newman env with it, then runs the service suite.
#
# Usage:
#   prodrun.sh <service> [--reseed] [--service <collection>] [run.sh args...]
#   prodrun.sh geo
#   prodrun.sh vpc --service authz-deny
set -euo pipefail
export KUBECONFIG=${KUBECONFIG:-/tmp/kacho.kubeconfig}

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FIX="$ROOT/tests/authz-fixtures"
CACHE=/tmp/matrix.json

SVC="${1:?usage: prodrun.sh <service> [args]}"; shift || true

RESEED=0
EXTRA=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --reseed) RESEED=1; shift ;;
    *) EXTRA+=("$1"); shift ;;
  esac
done

# Reseed if missing, forced, or STALE: Hydra issues SA access tokens with a 900s
# (15min) lifespan, so a matrix older than ~10min mints tokens that expire mid-run
# (gateway: "token is expired" → 401 cascade). Reseed aggressively; each suite must
# then finish inside the remaining token window (keep --delay low).
STALE=0
if [[ -s "$CACHE" ]]; then
  age=$(( $(date +%s) - $(stat -c %Y "$CACHE") ))
  [[ "$age" -gt 600 ]] && STALE=1
fi
DID_RESEED=0
if [[ ! -s "$CACHE" || "$RESEED" == 1 || "$STALE" == 1 ]]; then
  echo "[prodrun] seeding matrix -> $CACHE (stale=$STALE)" >&2
  # Bounded-retry: prodseed's owner db_lookup can transiently race the account/project
  # provisioning EC on a FRESH stand (owner-a account/project not yet queryable after the
  # first OIDC login) → "db_lookup(...) empty after retries". A single re-attempt clears
  # it; without this a flaked reseed leaves an empty matrix → the whole suite washes.
  reseed_ok=0
  for attempt in 1 2 3; do
    tmp="$(mktemp)"
    if python3 "$FIX/prodseed_matrix.py" > "$tmp" 2>/tmp/prodseed-matrix.err && [[ -s "$tmp" ]]; then
      mv "$tmp" "$CACHE"; reseed_ok=1; break
    fi
    echo "[prodrun] reseed attempt $attempt failed (provisioning EC?) — retrying in 8s" >&2
    rm -f "$tmp"; sleep 8
  done
  [[ "$reseed_ok" == 1 ]] || { echo "[prodrun] FATAL: matrix reseed failed after 3 attempts" >&2; tail -3 /tmp/prodseed-matrix.err >&2; exit 1; }
  rm -f /tmp/matrix-*-ext.json   # subject ids change on reseed → invalidate ext caches
  DID_RESEED=1
fi

ENVFILE="$ROOT/services/$SVC/tests/newman/environments/local.postman_environment.json"
[[ -f "$ENVFILE" ]] || { echo "[prodrun] no env: $ENVFILE" >&2; exit 1; }

python3 "$FIX/patch-env.py" "$CACHE" "$ENVFILE" >&2

# per-service extension seeder (resource deps + object-scope FGA grants the base
# matrix cannot express). Emits extra fixtures on stdout; merged into the env.
EXT="$FIX/prodseed_${SVC}_ext.py"
EXTCACHE="/tmp/matrix-${SVC}-ext.json"
if [[ -f "$EXT" ]]; then
  if [[ ! -s "$EXTCACHE" || "$RESEED" == 1 ]]; then
    echo "[prodrun] seeding $SVC extension -> $EXTCACHE" >&2
    python3 "$EXT" > "$EXTCACHE"
  fi
  python3 "$FIX/patch-env.py" "$EXTCACHE" "$ENVFILE" >&2
fi

# Grant-materialization drain-gate: freshly-created AccessBindings materialize the
# subject's owner/verb FGA tuples eventually-consistent. Running collections at
# matrix-age-0 hits that window (403 cascade in suites with thin retry coverage —
# the reseed-warmup race). Deterministically wait (poll healthy fga_outbox depth →
# 0) once after a reseed so grants are visible before the first suite; adapts to the
# burst size instead of a fixed under/over-shooting sleep, and degrades to a bounded
# settle when the iam DB is not directly reachable. The run then fits the 15min token
# window (reseed ~3min + drain + run).
if [[ "$DID_RESEED" == 1 ]]; then
  echo "[prodrun] grant-materialization drain-gate…" >&2
  bash "$FIX/drain_fga_outbox.sh" "${DRAIN_BUDGET:-180}" || true
fi

cd "$ROOT/services/$SVC/tests/newman"
exec ./scripts/run.sh "${EXTRA[@]}"
