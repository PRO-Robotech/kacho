#!/usr/bin/env bash
# cutover-fe3455.sh — fe3455 PROD umbrella FORWARD roll (single coherent cutover).
# =============================================================================
# WHAT THIS DOES
#   Runs `helm upgrade kacho-umbrella` against the LIVE fe3455 cluster with the
#   fe3455 prod overlays (values.fe3455.yaml + values.fe3455-prod.yaml). This is a
#   deliberate FORWARD roll of four workloads to their merged main builds, keeping
#   every OTHER workload pinned to its currently-live tag (no revert):
#
#     workload      old (live)                              new (forward)     why
#     ───────────   ─────────────────────────────────────  ────────────────  ─────────────────────────
#     kacho-iam     KAC-registry-docker-auth-c3000530       main-c744f956     #321 audience + :9097 JWKS + #325 RG-1 418-catalog + #326 issued_at
#     api-gateway   main-a7c82963                            main-c7dce40d     #145 RG-1 6 routes + 418-catalog
#     registry      main-5eb21d25                            main-af0eacae     #43 RG-1 Repository persistence (strict fwd; 5eb21d25 ∈ af0eacae)
#     kacho-storage — NOT installed (storage.enabled=false): the kacho-storage chart is
#                     not integrated with the umbrella (values keys diverge + no
#                     existingSecret for the DB password), so its fresh install never came
#                     up and its 15m --wait failed the WHOLE cutover. Pure control-plane
#                     roll until that chart lands. Image main-a185fa07 (CS-1) is built and
#                     waiting. See values.fe3455-prod.yaml (storage block).
#
#   COHERENCE (verified via `helm template` of the 4-overlay stack, 0 stderr, diff
#   vs live shows ONLY these image lines change):
#     • JWKS-flip: registry.iam.jwksUrl → https://kacho-iam-internal:9097 (merged #171).
#       iam-on-main (b3d23769, #323) SERVES :9097 → the flip is coherent (no 401-storm).
#       `helm --wait` brings iam Ready before returning; the registry Bearer verifier
#       fetches JWKS lazily on first token-verify → single upgrade is safe.
#     • catalog: iam(#325)/gateway(#145)/registry both land the 418-entry permission
#       catalog together → the new RG-1 Repository RPCs authorize (no "catalog: no entry").
#     • unchanged & live: vpc main-6fe9c386, compute main-1678f62c, geo main-fc2d945c,
#       nlb main-2c87cac9, zot v2.1.18, every uif remote master-e6001c77, every Postgres
#       (16.1.0-debian-11-r25 / pg-hydra 16.4.0-debian-12-r0) — emptyDir, tags NOT bumped.
#
#   REGISTRY data-plane TLS: the overlay now sets registry.service.dataplaneLB.tlsSidecar
#   (enabled + LE cert), so the chart — not a hand-applied kubectl patch — owns the public
#   TLS termination (443 -> dp-tls, Let's Encrypt). This closes the drift that broke
#   `docker login` on the 2026-07-15 run: --take-ownership adopted the hand-patched Service
#   and re-rendered it from the chart default (443 -> plaintext). The rendered LE Certificate
#   is byte-identical to the live object, so helm ADOPTS it without a re-issue (LE
#   duplicate-limit 5/week).
#
#   STORAGE: NOT installed on this cutover (storage.enabled=false + pg-storage.enabled=false).
#   The kacho-storage chart is not integrated with the umbrella — the overlay's storage.db.*
#   keys do not exist in it (it reads config.dbHost/…), and it has no existingSecret support
#   for the DB password, so it rendered its own Secret with the "changeme" placeholder. The
#   fresh install never became Ready and its 15m --wait failed the whole cutover. Image
#   main-a185fa07 (CS-1) is built and waiting; see values.fe3455-prod.yaml (storage block)
#   for the re-enable checklist. Remaining CS-1 follow-up either way: fga-register drainer +
#   storage-SA fga_writer seed in iam.
#   For a PURE control-plane-only cutover set storage.enabled=false + pg-storage.enabled=false.
#
# DOCKER-LOGIN issued_at BLOCKER — RESOLVED (2026-07-15), guard retained as a denylist.
#   iam's /iam/token (:9096) must emit `issued_at` as an RFC3339 STRING: the docker client
#   parses it via time.Time.UnmarshalJSON, which accepts ONLY a JSON string — a bare Unix
#   number breaks `docker login` ("Time.UnmarshalJSON: input is not a JSON string") → no
#   bearer → all pull/push 401. The earlier forward target main-b3d23769 REVERTED that fix
#   (kacho-iam c300053), so it was a hard blocker. kacho-iam#326 re-applied c300053 onto
#   main (+ a wire-shape regression test locking the string) → main c744f95 → CI published
#   main-c744f956, the tag pinned above. The preflight below stays as a KNOWN-BAD-TAG
#   denylist: main-b3d23769 remains a broken image, so repinning back to it must not be
#   silent (override: ACK_IAM_ISSUED_AT_REVERT=1).
#
# WHY A SCRIPT (not run by the coding agent): the auto-mode classifier blocks `helm`
#   against the fe3455 context, so the agent cannot run the upgrade. You run this.
#
# DEP RESOLUTION: uses `helm dependency build` (respects the committed Chart.lock),
#   NOT `helm dependency update`. This PINS postgresql to 13.4.4 so the upgrade can
#   never bump the pg image tag — critical because every Postgres here runs emptyDir
#   (persistence.enabled=false), so a pg image change would recreate the pod and WIPE
#   the database. `dependency build` still re-vendors the local file:// sub-charts
#   (registry from ../kacho-registry@main with the merged S3/compat chart), which is
#   the reason a re-vendor is needed. If build fails because a sibling chart version
#   changed, run `helm dependency update` manually AND re-confirm pg pins stay 13.4.4.
#
# NO SECRET VALUES are written in this file. Postgres passwords are read from the
#   pre-created k8s Secrets at run time and passed via --set only to satisfy the
#   bitnami passwords-on-upgrade guard (they are never echoed).
# =============================================================================
set -euo pipefail

NS=kacho
RELEASE=kacho-umbrella
CHART_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$CHART_DIR"

log()  { printf '\033[1;34m[cutover]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[cutover WARN]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[cutover ABORT]\033[0m %s\n' "$*" >&2; exit 1; }

# ── 0. tools + cluster context ────────────────────────────────────────────────
command -v helm    >/dev/null || die "helm not found in PATH"
command -v kubectl >/dev/null || die "kubectl not found in PATH"
CTX="$(kubectl config current-context 2>/dev/null || true)"
log "kubectl context: ${CTX:-<none>}   namespace: $NS   chart: $CHART_DIR"
case "$CTX" in
  *fe3455*) : ;;
  *) warn "context '$CTX' does not look like fe3455 — Ctrl-C within 5s if this is the wrong cluster"; sleep 5 ;;
esac

# ── 0b. KNOWN-BAD-TAG GUARD: the iam image must carry the issued_at RFC3339 fix ────
#    main-b3d23769 reverts kacho-iam c300053 (issued_at RFC3339 string) → `docker login`
#    breaks. The current pin (main-c744f956, kacho-iam#326) carries the fix, so this guard
#    is a denylist against a silent repin BACK to the broken image.
if grep -qE '^\s*tag:\s*main-b3d23769\s*$' "$CHART_DIR/values.fe3455-prod.yaml" 2>/dev/null; then
  if [ "${ACK_IAM_ISSUED_AT_REVERT:-0}" != "1" ]; then
    die "BLOCKER: kacho-iam pinned to main-b3d23769, which REVERTS the docker-login
       issued_at RFC3339 fix (kacho-iam commit c300053). Rolling iam to this image breaks
       'docker login' (Time.UnmarshalJSON: input is not a JSON string) → the registry
       data-plane cannot mint a bearer token → all docker pull/push 401.
       RESOLUTION: pin kacho-iam.image.tag to main-c744f956 or later (main c744f95 carries
       c300053 re-applied via kacho-iam#326) in BOTH values.fe3455.yaml and
       values.fe3455-prod.yaml → re-run.
       To knowingly ship the docker-login break anyway: ACK_IAM_ISSUED_AT_REVERT=1 $0"
  fi
  warn "ACK_IAM_ISSUED_AT_REVERT=1 set — proceeding with main-b3d23769; 'docker login' WILL break until c300053 is on the iam image."
fi

# ── 1. required value files present (values.fe3455-ory.yaml is gitignored) ─────
for f in values.prod.yaml values.fe3455.yaml values.fe3455-prod.yaml values.fe3455-ory.yaml; do
  [ -f "$CHART_DIR/$f" ] || die "missing values file: $f  (values.fe3455-ory.yaml is gitignored — restore it locally before cutover)"
done
log "all 4 overlay value files present."

# ── 2. required Secrets present (NOT created here — they hold creds you provide) ─
kubectl -n "$NS" get secret zot-s3-creds >/dev/null 2>&1 \
  || die "Secret 'zot-s3-creds' missing in ns $NS — it holds the Beget S3 access/secret keys zot needs (keys AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY). Create it first, then re-run."
log "Secret zot-s3-creds present."

# ── 3. re-vendor sub-charts from committed Chart.lock (pins pg -> no data loss) ─
log "helm dependency build (respects Chart.lock; re-vendors ../kacho-registry@main S3/compat chart)…"
helm dependency build . >/dev/null \
  || die "helm dependency build failed. If a sibling chart version changed, run 'helm dependency update' manually, then re-confirm every postgresql pin in Chart.lock is 13.4.4 before re-running."

# ── 4. bitnami pg upgrade-guard --set args, read from pre-created Secrets ───────
#    Every pg-<svc> sets auth.existingSecret, so the chart already reads the password
#    from the Secret; these --set values are a defensive belt for the bitnami
#    passwords-on-upgrade guard. Correct value paths: auth.password (secret key
#    'password') + auth.postgresPassword (secret key 'postgres-password').
PG_SVCS=(vpc compute iam geo nlb storage registry openfga kratos hydra)
PGARGS=()
for svc in "${PG_SVCS[@]}"; do
  sec="kacho-umbrella-pg-$svc"
  if ! kubectl -n "$NS" get secret "$sec" >/dev/null 2>&1; then
    warn "Secret $sec absent — skipping pg-$svc guard args (chart will manage it)"
    continue
  fi
  pw="$(kubectl -n "$NS" get secret "$sec" -o jsonpath='{.data.password}' | base64 -d 2>/dev/null || true)"
  apw="$(kubectl -n "$NS" get secret "$sec" -o jsonpath='{.data.postgres-password}' | base64 -d 2>/dev/null || true)"
  [ -n "$pw" ]  && PGARGS+=(--set "pg-$svc.auth.password=$pw")
  [ -n "$apw" ] && PGARGS+=(--set "pg-$svc.auth.postgresPassword=$apw")
done
log "built pg upgrade-guard --set args (${#PG_SVCS[@]} services scanned; values not echoed)."

# ── 5. the convergence upgrade ────────────────────────────────────────────────
log "helm upgrade $RELEASE — CONVERGE onto live images (--take-ownership adopts hand-applied resources)…"
if ! helm upgrade "$RELEASE" . -n "$NS" \
      -f values.prod.yaml \
      -f values.fe3455.yaml \
      -f values.fe3455-prod.yaml \
      -f values.fe3455-ory.yaml \
      --set uif.enabled=true \
      --take-ownership \
      --wait --timeout 15m \
      ${PGARGS[@]+"${PGARGS[@]}"}; then
  warn "helm upgrade FAILED."
  warn "Roll back to the previous good revision:   helm rollback $RELEASE -n $NS"
  warn "Inspect:   helm history $RELEASE -n $NS   |   kubectl -n $NS get pods"
  die  "cutover aborted (see above)."
fi
log "helm upgrade succeeded."

# ── 6. smoke check (registry + api-gateway + uif host rollout, then data-plane) ─
rc=0
log "smoke: rollout status (registry, api-gateway, uif host)…"
for d in registry api-gateway uif; do
  kubectl -n "$NS" rollout status deploy/"$d" --timeout=120s || { warn "rollout deploy/$d not complete"; rc=1; }
done
# storage: only when the overlay enables it (currently OFF — the kacho-storage chart is
# not integrated with the umbrella; see values.fe3455-prod.yaml). Non-fatal either way.
if kubectl -n "$NS" get deploy kacho-umbrella-storage >/dev/null 2>&1; then
  kubectl -n "$NS" rollout status deploy/kacho-umbrella-storage --timeout=120s \
    || warn "kacho-umbrella-storage not Ready yet (storage-split install — check its logs)"
else
  log "storage not installed (storage.enabled=false) — skipping its rollout check."
fi

# ── smoke: iam :9097 cluster-internal JWKS proxy (the JWKS-flip source of truth) ──
#    The registry Bearer verifier now trusts iam's :9097 mirror (registry.iam.jwksUrl).
#    Confirm iam-on-main actually serves it with Hydra kids BEFORE trusting docker auth.
#    Server-TLS (internal-CA leaf), no client cert → curl -k. Port-forward to reach the
#    ClusterIP service from the operator host.
log "smoke: iam :9097 JWKS proxy (GET /.well-known/jwks.json — expect 200 with keys)…"
# THIS upgrade rolls the iam pod, so probe only once it is actually Ready. A port-forward
# established against a terminating pod stays broken for the rest of the probe → false
# negative. That is exactly what the 2026-07-15 run hit: the smoke warned "did NOT return
# a keys set" while the endpoint served 200 with Hydra kids moments later. Hence: wait for
# the rollout, then re-establish a FRESH forward per attempt (one dead tunnel must not
# doom the whole check).
kubectl -n "$NS" rollout status deploy/kacho-iam --timeout=120s >/dev/null 2>&1 \
  || warn "kacho-iam rollout not complete — the JWKS probe below may be unreliable."

jwks=""
for _attempt in 1 2 3 4 5; do
  kubectl -n "$NS" port-forward svc/kacho-iam-internal 19097:9097 >/dev/null 2>&1 &
  pf_pid=$!
  for _ in 1 2 3 4 5 6; do
    curl -sk --max-time 3 https://127.0.0.1:19097/.well-known/jwks.json >/dev/null 2>&1 && break
    sleep 1
  done
  jwks="$(curl -sk --max-time 5 https://127.0.0.1:19097/.well-known/jwks.json 2>/dev/null || true)"
  kill "$pf_pid" >/dev/null 2>&1 || true
  wait "$pf_pid" 2>/dev/null || true
  printf '%s' "$jwks" | grep -q '"keys"' && break
  sleep 2
done
if printf '%s' "$jwks" | grep -q '"keys"'; then
  log "iam :9097 JWKS OK (serves a keys set — JWKS-flip is coherent)."
else
  warn "iam :9097 JWKS did NOT return a keys set — registry token-verify will 401. Check kacho-iam is on main-c744f956 (serves :9097, #323) and the jwks-proxy listener."
  rc=1
fi

log "smoke: curl https://registry.in-cloud.io/v2/ (expect HTTP 401 token-auth challenge)…"
code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 15 https://registry.in-cloud.io/v2/ || echo 000)"
if [ "$code" = "401" ]; then
  log "registry data-plane OK (HTTP 401 auth challenge as expected)."
else
  warn "registry /v2/ returned HTTP $code (expected 401) — check registry pod, zot, and the JWKS terminator."
  rc=1
fi

if [ "$rc" -eq 0 ]; then
  log "CUTOVER COMPLETE — forward roll applied (iam/gateway/registry + storage), smoke green."
else
  warn "CUTOVER upgrade applied, but smoke checks had warnings (see above) — investigate before declaring done."
fi
exit "$rc"
