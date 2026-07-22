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

if [[ ! -s "$CACHE" || "$RESEED" == 1 ]]; then
  echo "[prodrun] seeding matrix -> $CACHE" >&2
  python3 "$FIX/prodseed_matrix.py" > "$CACHE"
fi

ENVFILE="$ROOT/services/$SVC/tests/newman/environments/local.postman_environment.json"
[[ -f "$ENVFILE" ]] || { echo "[prodrun] no env: $ENVFILE" >&2; exit 1; }

python3 "$FIX/patch-env.py" "$CACHE" "$ENVFILE" >&2

cd "$ROOT/services/$SVC/tests/newman"
exec ./scripts/run.sh "${EXTRA[@]}"
