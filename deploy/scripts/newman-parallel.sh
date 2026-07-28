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
#   6. aggregate TWICE and print BOTH:
#        RAW   — every failed assertion / request exactly as newman reported it
#        GATED — the SAME gate CI runs (services/iam/tests/newman/scripts/
#                assert-suites-green.sh: known-RED whitelist)
#      exit code = GATED, so a local run and CI agree on the verdict. The RAW block is
#      NOT optional output: it is what keeps the whitelist honest — you can always see
#      how much was failing BEFORE the filter, and a whitelist that starts absorbing new
#      failures shows up as a growing raw-vs-gated gap instead of as silence.
#      The gate no longer subtracts anything from the REQUEST count: a request that got
#      no answer is reported as UNANSWERED and fires the gate. It used to be filtered
#      away as "DNS noise", which is how eight ban-#6 negatives went unexecuted while
#      both verdicts read green.
#
# Usage (after `make dev-up`):
#   ./scripts/newman-parallel.sh                 # all four
#   SERVICES="vpc nlb" ./scripts/newman-parallel.sh
#   DELAY=3 JOBS=3 ./scripts/newman-parallel.sh
set -uo pipefail

# storage + registry added: their redesign was NOT under e2e coverage (artifact
# lacked them). Their suite fixtures (isolated account+projects+grant, default-Bearer
# prelude) are now seeded by the shared authz-fixtures.
# geo has its OWN full 42-case suite (Region/Zone public + Internal admin :9091 + authz
# + placement) — owner directive "every module has its own complete tests". geo:dev
# verified locally to boot clean (authMode=dev, mTLS creds mounted) + serve; back in the
# active run.
# api-gateway added 2026-07-26: gateway/tests/newman existed but had never been
# executed — no run.sh, no environment file, and it was absent from this list, while
# suite_dir() below already carried a branch for it. It owns the cluster-RBAC admin
# surface (InternalClusterService on the cluster-internal REST listener), which no
# per-service suite covers; services/iam/tests/newman even cites it as the covering
# location for a case it removed. A suite that exists but never runs is the state
# that let it rot, so it is either wired or deleted — this is the wiring.
SERVICES="${SERVICES:-iam vpc compute nlb storage registry geo api-gateway}"
NS="${SETUP_NS:-kacho}"
DEV_SECRET="${DEV_SECRET:-kacho-dev-jwt-secret-2026}"
GW_PORT="${GW_PORT:-18080}"
GW_INTERNAL_PORT="${GW_INTERNAL_PORT:-18081}"
# The api-gateway's EXTERNAL TLS listener (:8443, advertised as api.kacho.local:443).
# The ban-#6 negatives in the iam suite assert that Internal* RPCs are not reachable
# there. They used to address the advertised HOSTNAME, which does not resolve on a
# developer box (and adding it needs root), so for an unknown length of time those
# eight checks never ran while the gate subtracted their transport errors and printed
# "0 failed requests". Ban #6 is a property of the LISTENER, not of the DNS name used
# to find it — so it is forwarded here like the other two.
GW_TLS_PORT="${GW_TLS_PORT:-18443}"
IAM_INTERNAL_PORT="${IAM_INTERNAL_PORT:-19091}"
HYDRA_PORT="${HYDRA_PUBLIC_PORT:-14444}"   # OAuth2 token endpoint (production-posture seed)
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

echo "[parallel] port-forward api-gateway :$GW_PORT/:$GW_INTERNAL_PORT/:$GW_TLS_PORT + iam-internal :$IAM_INTERNAL_PORT + hydra :$HYDRA_PORT"
kubectl -n "$NS" port-forward svc/api-gateway "$GW_PORT:8080" >/tmp/e2e-pp-gw.log 2>&1 &            PF_PIDS+=($!)
kubectl -n "$NS" port-forward svc/api-gateway "$GW_INTERNAL_PORT:8081" >/tmp/e2e-pp-gwint.log 2>&1 & PF_PIDS+=($!)
kubectl -n "$NS" port-forward svc/api-gateway "$GW_TLS_PORT:8443" >/tmp/e2e-pp-gwtls.log 2>&1 &     PF_PIDS+=($!)
kubectl -n "$NS" port-forward svc/kacho-iam-internal "$IAM_INTERNAL_PORT:9091" >/tmp/e2e-pp-iam.log 2>&1 & PF_PIDS+=($!)
# Hydra public — the POST target of the OAuth2 client_credentials exchange that turns an
# iam-issued SA key into the RS256 Bearer a production-posture stand accepts. ClusterIP
# with no ingress route here, so the exchange needs this forward. Harmless in dev (the
# seed never dials it); required in production, and setting it HERE means the seed does
# not have to open one per invocation.
kubectl -n "$NS" port-forward svc/kacho-umbrella-hydra-public "$HYDRA_PORT:4444" >/tmp/e2e-pp-hydra.log 2>&1 & PF_PIDS+=($!)
sleep 4

# Client certificates for the cluster-internal listeners. iam :9091 demands a verified
# client cert in EVERY posture (security.md — internal is not exempt), and the two
# identities are NOT interchangeable:
#   api-gateway-client-tls  → the ordinary internal RPCs (UpsertFromIdentity, LookupSubject)
#   kacho-bootstrap-operator-client-tls → the ONLY SAN iam allow-lists for
#     MintBootstrapToken, the production seed's entry point. Pointing the mint at the
#     gateway identity is precisely the "fix" the #58 hardening exists to prevent, so the
#     two are exported separately and never aliased.
MTLS_ENV=()
if kubectl -n "$NS" get secret api-gateway-client-tls >/dev/null 2>&1; then
  CERT_DIR="$(mktemp -d)"; TMP_DIRS+=("$CERT_DIR")
  kubectl -n "$NS" get secret api-gateway-client-tls -o jsonpath='{.data.tls\.crt}' | base64 -d > "$CERT_DIR/client.crt"
  kubectl -n "$NS" get secret api-gateway-client-tls -o jsonpath='{.data.tls\.key}' | base64 -d > "$CERT_DIR/client.key"
  chmod 600 "$CERT_DIR"/*
  MTLS_ENV=(IAM_INTERNAL_GRPC_MTLS_CERT="$CERT_DIR/client.crt" IAM_INTERNAL_GRPC_MTLS_KEY="$CERT_DIR/client.key")
fi
if kubectl -n "$NS" get secret kacho-bootstrap-operator-client-tls >/dev/null 2>&1; then
  OP_DIR="$(mktemp -d)"; TMP_DIRS+=("$OP_DIR")
  kubectl -n "$NS" get secret kacho-bootstrap-operator-client-tls -o jsonpath='{.data.tls\.crt}' | base64 -d > "$OP_DIR/client.crt"
  kubectl -n "$NS" get secret kacho-bootstrap-operator-client-tls -o jsonpath='{.data.tls\.key}' | base64 -d > "$OP_DIR/client.key"
  chmod 600 "$OP_DIR"/*
  MTLS_ENV+=(BOOTSTRAP_MINT_MTLS_CERT="$OP_DIR/client.crt" BOOTSTRAP_MINT_MTLS_KEY="$OP_DIR/client.key")
fi

if [ "$SEED" = "true" ]; then
  echo "[parallel] seeding auth fixtures (per-service isolated) + patching envs"
  # INTERNAL_BASE_URL (:8081 internal mux, already port-forwarded above) lets setup.sh
  # seed the geo baseline (region/zones) + any Internal admin catalog via the :18081 mux.
  # Without it the greenfield geo catalog stays empty → every zone/region create fails.
  env BASE_URL="http://localhost:$GW_PORT" INTERNAL_BASE_URL="http://localhost:$GW_INTERNAL_PORT" \
      IAM_INTERNAL_GRPC="localhost:$IAM_INTERNAL_PORT" HYDRA_PUBLIC_PORT="$HYDRA_PORT" \
      DEV_SECRET="$DEV_SECRET" PATCH_ENV=true SETUP_NS="$NS" "${MTLS_ENV[@]}" \
      bash "$REPO_ROOT/tests/authz-fixtures/setup.sh"

  # Posture the seed actually ran in (written by setup.sh — ONE detector, no re-derivation).
  SEED_POSTURE_RAN="$(cat "$REPO_ROOT/tests/authz-fixtures/out/seed-posture" 2>/dev/null || echo dev)"

  # In PRODUCTION posture setup.sh delegates to prodseed_all.py, which already drives
  # seed-nlb-fixtures.sh with the RS256 admin Bearer. Re-running it here would make a
  # second author for the same cluster-wide default-pool slot.
  if [[ " $SERVICES " == *" nlb "* ]] && [ "$SEED_POSTURE_RAN" != "production" ]; then
    echo "[parallel] seeding nlb external-VIP AddressPool (best-effort)"
    # Take the admin Bearer the seed ACTUALLY produced instead of re-forging an HS256 one:
    # on a production-posture stand a locally-minted HS256 token is rejected (empty
    # devSecret) and this step silently seeded nothing. authz-fixtures.json carries
    # whichever admin credential the posture called for.
    SEED_JWT=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("jwtBootstrap",""))' \
      "$REPO_ROOT/tests/authz-fixtures/out/authz-fixtures.json" 2>/dev/null || true)
    # NLB_ZONE_ID — the nlb-DEDICATED zone (tests/authz-fixtures/setup.sh block 5d owns the
    # zone table and seeds it). setup.sh already ran the seeder for the isolated nlb project
    # above; this second, project-agnostic pass must target the SAME zone. Unpinned it falls
    # back to zone[0] = ru-central1-a and plants a v6-carrying default EXTERNAL_PUBLIC pool
    # there, deterministically breaking the vpc case ADR-CR-EXT-V6-FAMILY-FALLTHROUGH (which
    # asserts zone a has no v6-capable pool) — a cross-suite fixture collision, not a flake.
    env BASE_URL="http://localhost:$GW_PORT" INTERNAL_BASE_URL="http://localhost:$GW_INTERNAL_PORT" JWT="$SEED_JWT" \
      NLB_ZONE_ID="${NLB_ZONE:-ru-central1-e}" \
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
    # nlb: shared external-VIP pool (--jobs>1 exhausts it). iam+registry: materialization-
    # heavy (every AccessBinding / registry+repo Create → owner-tuple via fga_register_outbox
    # → drainer → OpenFGA Write). Under --jobs>1 the concurrent create rate outruns the
    # drainer → owner-tuple materialization backlog → create→Get/Delete 403/404 read-your-
    # writes past even a 48s client retry (throughput inversion, EC-lag not correctness — the
    # emission is verified). Serialising these suites keeps the drainer caught up so the
    # bounded client-retries cover the window. Correctness is validated at this concurrency;
    # prod-scale materialization throughput is the separate tracked scalability epic.
    sjobs="$JOBS"; case "$svc" in nlb|iam|registry) sjobs=1 ;; esac
    mkdir -p "$d/out"   # redirect below opens out/suite.log BEFORE run.sh's own mkdir
    ( cd "$d" && ./scripts/run.sh --service "" --delay "$DELAY" --jobs "$sjobs" \
        --env-var "baseUrl=http://localhost:$GW_PORT" \
        --env-var "internalBaseUrl=http://localhost:$GW_INTERNAL_PORT" \
        --env-var "externalBaseUrl=https://127.0.0.1:$GW_TLS_PORT" \
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
# Inter-wave drain-gate: WAVE 1 (vpc/compute/nlb/storage/… leaf-registrations) floods iam's
# fga_outbox + reconcile backlog. iam's OWN authz materialization (WAVE 2 — AccessBinding
# CRUD, label-revoke) runs the FULL EXCLUSIVE-lock ReconcileObject/drainer path; starting it
# while WAVE 1's backlog is still draining STARVES it (get-confirms 404, revoke-not-sticking,
# post-revoke {allowed:true} — the exact iam-suite reds under parallel load). Deterministically
# drain the healthy fga_outbox depth → 0 (bounded by DRAIN_BUDGET) BEFORE the isolated iam wave
# so its materialization has quiet room. Best-effort (the script degrades to a bounded settle if
# the iam DB is unreachable); skipped when there is no WAVE 2 or no WAVE 1.
if [ "${#wave2[@]}" -gt 0 ] && [ "${#wave1[@]}" -gt 0 ] && [ -f "$REPO_ROOT/tests/authz-fixtures/drain_fga_outbox.sh" ]; then
  echo "[parallel] inter-wave drain-gate (settle WAVE 1 fga_outbox backlog before iam wave)…"
  KACHO_NS="$NS" bash "$REPO_ROOT/tests/authz-fixtures/drain_fga_outbox.sh" "${DRAIN_BUDGET:-180}" || true
fi
if [ "${#wave2[@]}" -gt 0 ]; then
  echo "[parallel] WAVE 2 isolated (no competing load): ${wave2[*]}"
  launch_wave "${wave2[@]}"
fi

echo
# ─── Verdict: RAW (what newman reported) + GATED (what CI grades) ────────────
# Local runners used to grade on RAW only, while CI graded through
# services/iam/tests/newman/scripts/assert-suites-green.sh — so the two disagreed by
# construction. Both now grade with the same script.
#
# The example that used to stand here is worth keeping as a warning rather than as
# documentation, because it was the defect: `iam-internal-only-check` reported 0 failed
# ASSERTIONS and a non-zero exit, because 8 requests could not resolve api.kacho.local.
# The disagreement was reconciled the wrong way round — CI subtracted those transport
# errors, on the stated reasoning that an unreachable advertised host IS the internal-only
# invariant. It is not. Those eight checks were the ban-#6 negatives, and they did not run.
# "Could not reach it" and "it refused me" are different findings; only the second is
# evidence. The endpoint is now forwarded (GW_TLS_PORT) so the probes execute, and the
# gate reports an unanswered request as UNANSWERED instead of subtracting it.
GATE="${GATE:-true}"
GATE_SCRIPT="$REPO_ROOT/services/iam/tests/newman/scripts/assert-suites-green.sh"

echo "===== RAW (pre-whitelist) ====="
printf "%-12s %10s %10s %10s\n" "SUITE" "ASSERT-F" "REQ-F" "REPORTS"
raw_total_a=0; raw_total_r=0
for svc in $SERVICES; do
  d="$(suite_dir "$svc")"
  a=0; r=0; n=0
  for f in "$d"/out/*.json; do
    [ -e "$f" ] || continue
    n=$((n + 1))
    a=$((a + $(jq -r '.run.stats.assertions.failed // 0' "$f" 2>/dev/null || echo 0)))
    r=$((r + $(jq -r '.run.stats.requests.failed // 0' "$f" 2>/dev/null || echo 0)))
  done
  printf "%-12s %10s %10s %10s\n" "$svc" "$a" "$r" "$n"
  raw_total_a=$((raw_total_a + a)); raw_total_r=$((raw_total_r + r))
done
printf "%-12s %10s %10s\n" "TOTAL" "$raw_total_a" "$raw_total_r"
if [ "$RC" -eq 0 ]; then echo "[parallel] RAW verdict: ALL SUITES GREEN"; else echo "[parallel] RAW verdict: one or more suites RED (see per-suite out/summary.txt + out/*.json)"; fi

if [ "$GATE" != "true" ] || [ ! -f "$GATE_SCRIPT" ]; then
  echo "[parallel] CI gate skipped (GATE=$GATE, script=$GATE_SCRIPT) — grading on RAW"
  exit "$RC"
fi

echo
echo "===== GATED (the exact gate CI runs: assert-suites-green.sh) ====="
GATE_RC=0
for svc in $SERVICES; do
  d="$(suite_dir "$svc")"
  echo "--- $svc"
  # cwd = the suite's newman dir: the gate walks collections/*.json vs out/<name>.json there.
  if ( cd "$d" && bash "$GATE_SCRIPT" ); then :; else GATE_RC=1; fi
done

echo
if [ "$GATE_RC" -eq 0 ]; then
  echo "[parallel] GATED verdict: GREEN (CI would pass)"
  [ "$RC" -eq 0 ] || echo "[parallel] NOTE: raw failures above are covered by the known-RED whitelist — read them, do not extend the list to keep this green."
else
  echo "[parallel] GATED verdict: RED (CI would fail) — see the per-collection lines above"
fi
exit "$GATE_RC"
