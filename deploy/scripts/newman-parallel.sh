#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# newman-parallel.sh — run the iam/vpc/compute/nlb newman suites CONCURRENTLY
# against the running dev stand (директива #1).
#
# Why parallel: the four service suites are independent (директива #2 gives each
# its own account/project, so their fixtures don't collide) — running them one
# after another serialised ~4× the wall-time and hit the CI job timeout
# ("compute/nlb уходили в no-report"). Fanning them out cuts wall-time to ~max(one
# suite) instead of sum(all), which is what removes the timeout.
#
# Flow (idempotent, deterministic — same as newman-e2e.sh, once for all four):
#   1. port-forward api-gateway public(:18080)+internal(:18081) + iam-internal(:19091)
#   2. seed auth fixtures ONCE via tests/authz-fixtures/setup.sh (per-service
#      isolated accounts/projects) + patch every service env
#   3. seed the nlb external-VIP AddressPool (only nlb needs it)
#   4. regenerate every suite's collections (gen.py)
#   5. run all four suites in parallel (each = its own scripts/run.sh, which itself
#      fans its collections out with --jobs); per-suite logs to out/<svc>-suite.log
#   6. aggregate: print each suite's summary, exit non-zero if ANY suite is red
#
# Usage (after `make dev-up`):
#   ./scripts/newman-parallel.sh                 # all four
#   SERVICES="vpc nlb" ./scripts/newman-parallel.sh
#   DELAY=3 JOBS=3 ./scripts/newman-parallel.sh
set -uo pipefail

SERVICES="${SERVICES:-iam vpc compute nlb}"
NS="${SETUP_NS:-kacho}"
DEV_SECRET="${DEV_SECRET:-kacho-dev-jwt-secret-2026}"
GW_PORT="${GW_PORT:-18080}"
GW_INTERNAL_PORT="${GW_INTERNAL_PORT:-18081}"
IAM_INTERNAL_PORT="${IAM_INTERNAL_PORT:-19091}"
DELAY="${DELAY:-3}"          # per-request delay (ms) inside each collection
JOBS="${JOBS:-2}"            # per-suite collection concurrency (× len(SERVICES) total)
SEED="${SEED:-true}"         # set false to reuse an already-seeded stand

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

for tool in kubectl python3 newman grpcurl jq; do
  command -v "$tool" >/dev/null 2>&1 || { echo "FATAL: '$tool' not found in PATH" >&2; exit 1; }
done

suite_dir() { # <svc>
  if [ "$1" = "api-gateway" ]; then echo "$REPO_ROOT/gateway/tests/newman"; else echo "$REPO_ROOT/services/$1/tests/newman"; fi
}

PF_PIDS=()
TMP_DIRS=()
cleanup() {
  for p in "${PF_PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
  for d in "${TMP_DIRS[@]:-}"; do [ -n "$d" ] && rm -rf "$d"; done
}
trap cleanup EXIT

echo "[parallel] port-forward api-gateway :$GW_PORT/:$GW_INTERNAL_PORT + iam-internal :$IAM_INTERNAL_PORT"
kubectl -n "$NS" port-forward svc/api-gateway "$GW_PORT:8080" >/tmp/e2e-pp-gw.log 2>&1 &            PF_PIDS+=($!)
kubectl -n "$NS" port-forward svc/api-gateway "$GW_INTERNAL_PORT:8081" >/tmp/e2e-pp-gwint.log 2>&1 & PF_PIDS+=($!)
kubectl -n "$NS" port-forward svc/kacho-iam-internal "$IAM_INTERNAL_PORT:9091" >/tmp/e2e-pp-iam.log 2>&1 & PF_PIDS+=($!)
sleep 4

# mTLS client-cert for grpcurl → iam-internal (dev stand ships mtls.enabled=true).
MTLS_ENV=()
if kubectl -n "$NS" get secret api-gateway-client-tls >/dev/null 2>&1; then
  CERT_DIR="$(mktemp -d)"; TMP_DIRS+=("$CERT_DIR")
  kubectl -n "$NS" get secret api-gateway-client-tls -o jsonpath='{.data.tls\.crt}' | base64 -d > "$CERT_DIR/client.crt"
  kubectl -n "$NS" get secret api-gateway-client-tls -o jsonpath='{.data.tls\.key}' | base64 -d > "$CERT_DIR/client.key"
  chmod 600 "$CERT_DIR"/*
  MTLS_ENV=(IAM_INTERNAL_GRPC_MTLS_CERT="$CERT_DIR/client.crt" IAM_INTERNAL_GRPC_MTLS_KEY="$CERT_DIR/client.key")
fi

if [ "$SEED" = "true" ]; then
  echo "[parallel] seeding auth fixtures (per-service isolated) + patching envs"
  env BASE_URL="http://localhost:$GW_PORT" IAM_INTERNAL_GRPC="localhost:$IAM_INTERNAL_PORT" \
      DEV_SECRET="$DEV_SECRET" PATCH_ENV=true SETUP_NS="$NS" "${MTLS_ENV[@]}" \
      bash "$REPO_ROOT/tests/authz-fixtures/setup.sh"

  if [[ " $SERVICES " == *" nlb "* ]]; then
    echo "[parallel] seeding nlb external-VIP AddressPool (best-effort)"
    SEED_JWT=$(python3 "$REPO_ROOT/tests/authz-fixtures/setup-jwt.py" --secret "$DEV_SECRET" --exp-hours 24 --bulk 2>/dev/null \
      | python3 -c 'import json,sys; print(json.load(sys.stdin).get("jwtBootstrap",""))' 2>/dev/null || true)
    env BASE_URL="http://localhost:$GW_PORT" INTERNAL_BASE_URL="http://localhost:$GW_INTERNAL_PORT" JWT="$SEED_JWT" \
      bash "$REPO_ROOT/deploy/scripts/seed-nlb-fixtures.sh" || true
  fi
fi

echo "[parallel] regenerating collections for: $SERVICES"
for svc in $SERVICES; do ( cd "$(suite_dir "$svc")" && python3 scripts/gen.py >/dev/null ) || { echo "gen $svc FAILED" >&2; exit 1; }; done

# Two-wave scheduler. PHASE2_SERVICES (default: iam) run in a SEPARATE second wave,
# NOT concurrent with the rest. Rationale: iam's OWN authz materialization (AccessBinding
# CRUD, label-revoke delete-stale) is full-path EXCLUSIVE-lock serialized; under the peak
# concurrent load of vpc+compute+nlb registering resources it drains (get-confirms 404,
# post-revoke {allowed:true}, cross-service NOB grant-window contamination). Isolating iam
# to its own wave gives its full-path room to materialize with no competing load. The
# leaf-resource services keep the forward SHARE-lock fast-path in their concurrent wave.
# wall-time = dev-up + max(wave1) + wave2(iam ~serial) instead of max(all).
PHASE2="${PHASE2_SERVICES:-iam}"
_in_phase2() { case " $PHASE2 " in *" $1 "*) return 0 ;; *) return 1 ;; esac; }

RC=0
declare -A SUITE_PID

launch_wave() {  # $@ = services to run concurrently within this wave
  SUITE_PID=()
  local svc d sjobs st
  for svc in "$@"; do
    d="$(suite_dir "$svc")"
    # nlb EXTERNAL suites draw auto-VIPs from ONE shared external AddressPool — --jobs>1
    # transiently exhausts it → phantom (see nlb run.sh header). Force nlb serial.
    sjobs="$JOBS"; [ "$svc" = "nlb" ] && sjobs=1
    mkdir -p "$d/out"   # redirect below opens out/suite.log BEFORE run.sh's own mkdir
    ( cd "$d" && ./scripts/run.sh --service "" --delay "$DELAY" --jobs "$sjobs" \
        --env-var "baseUrl=http://localhost:$GW_PORT" \
        --env-var "internalBaseUrl=http://localhost:$GW_INTERNAL_PORT" \
        >"$d/out/suite.log" 2>&1 ) &
    SUITE_PID[$svc]=$!
  done
  for svc in "$@"; do
    if wait "${SUITE_PID[$svc]}"; then st="GREEN"; else st="RED"; RC=1; fi
    echo "===== [$svc] $st ====="
    tail -n +1 "$(suite_dir "$svc")/out/summary.txt" 2>/dev/null || echo "  (no summary — see out/suite.log)"
  done
}

wave1=(); wave2=()
for svc in $SERVICES; do
  if _in_phase2 "$svc"; then wave2+=("$svc"); else wave1+=("$svc"); fi
done

if [ "${#wave1[@]}" -gt 0 ]; then
  echo "[parallel] WAVE 1 concurrent (delay=${DELAY}ms jobs=${JOBS}/suite; nlb --jobs 1): ${wave1[*]}"
  launch_wave "${wave1[@]}"
fi
if [ "${#wave2[@]}" -gt 0 ]; then
  echo "[parallel] WAVE 2 isolated (no competing load): ${wave2[*]}"
  launch_wave "${wave2[@]}"
fi

echo
if [ "$RC" -eq 0 ]; then echo "[parallel] ALL SUITES GREEN"; else echo "[parallel] one or more suites RED (see per-suite out/summary.txt + out/*.json)"; fi
exit "$RC"
