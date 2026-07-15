#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# newman-e2e.sh — REPRODUCIBLE newman e2e flow against the running dev stand.
#
# Replaces the manual "seed tokens by hand" path with a deterministic,
# committed flow:
#   1. port-forward api-gateway (:18080) + kacho-iam-internal (:19091)
#   2. seed auth fixtures via tests/authz-fixtures/setup.sh (idempotent):
#      mints non-expiring dev JWTs, upserts users, accounts/projects, grants
#      cluster-admin (SQL backdoor), seeds VPC networks, and PATCHES every
#      service's newman environment (existingProjectId, jwt*, …).
#   3. run the requested service's newman collection(s) against :18080.
#   4. tear down the port-forwards on exit.
#
# Usage (after `make dev-up`):
#   make e2e-newman SVC=vpc                      # whole vpc suite
#   make e2e-newman SVC=vpc COLLECTION=internal-network
#   ./scripts/newman-e2e.sh vpc internal-network
#
# Prereqs (fail-fast): kubectl, python3, newman, grpcurl. The flow is
# environment-agnostic — same script seeds + runs in CI and locally.
set -euo pipefail

SVC="${1:-${SVC:-vpc}}"
COLLECTION="${2:-${COLLECTION:-}}"
NS="${SETUP_NS:-kacho}"
DEV_SECRET="${DEV_SECRET:-kacho-dev-jwt-secret-2026}"
GW_PORT="${GW_PORT:-18080}"
IAM_INTERNAL_PORT="${IAM_INTERNAL_PORT:-19091}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(cd "$SCRIPT_DIR/../../.." && pwd)"
NEWMAN_DIR="$WORKSPACE_DIR/project/kacho-$SVC/tests/newman"

for tool in kubectl python3 newman grpcurl; do
  command -v "$tool" >/dev/null 2>&1 || { echo "FATAL: '$tool' not found in PATH" >&2; exit 1; }
done
[ -d "$NEWMAN_DIR" ] || { echo "FATAL: no newman dir for SVC=$SVC ($NEWMAN_DIR)" >&2; exit 1; }

PF_PIDS=()
cleanup() { for p in "${PF_PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done; }
trap cleanup EXIT

echo "[e2e] port-forward api-gateway :$GW_PORT and kacho-iam-internal :$IAM_INTERNAL_PORT"
kubectl -n "$NS" port-forward svc/api-gateway "$GW_PORT:8080" >/tmp/e2e-pf-gw.log 2>&1 &
PF_PIDS+=($!)
kubectl -n "$NS" port-forward svc/kacho-iam-internal "$IAM_INTERNAL_PORT:9091" >/tmp/e2e-pf-iam.log 2>&1 &
PF_PIDS+=($!)
sleep 4

echo "[e2e] seeding auth fixtures (idempotent) + patching newman envs"
BASE_URL="http://localhost:$GW_PORT" \
IAM_INTERNAL_GRPC="localhost:$IAM_INTERNAL_PORT" \
DEV_SECRET="$DEV_SECRET" PATCH_ENV=true SETUP_NS="$NS" \
  bash "$WORKSPACE_DIR/tests/authz-fixtures/setup.sh"

echo "[e2e] regenerating newman collections"
( cd "$NEWMAN_DIR" && python3 scripts/gen.py >/dev/null )

echo "[e2e] running newman (SVC=$SVC COLLECTION=${COLLECTION:-<all>})"
cd "$NEWMAN_DIR"
if [ -n "$COLLECTION" ]; then
  newman run "collections/${COLLECTION}.postman_collection.json" \
    -e environments/local.postman_environment.json \
    --env-var "baseUrl=http://localhost:$GW_PORT" \
    --delay-request 15 --reporters cli
else
  ./scripts/run.sh --service "" --delay 15
fi
