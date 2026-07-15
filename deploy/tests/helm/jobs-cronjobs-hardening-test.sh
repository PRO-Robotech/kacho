#!/usr/bin/env bash
# INFRA sec-hardening r3 manifest-assertion guard (offline; no kind cluster).
#
# Closes the 3rd-audit residual: EVERY umbrella-owned Job/CronJob must carry the
# same restricted PSS floor the long-running Deployments enforce, and the public
# api-gateway ingress must route the EXTERNAL edge through the external-marked
# TLS listener (port `tls`, backend-protocol GRPCS) so listenerorigin.ExternalListener
# tags the traffic and the REST dispatcher 404s Internal* paths on the public edge.
#
#   1. openfga-bootstrap Job          — pod + container restricted floor.
#   2. openfga-postgres-init Job      — pod + container restricted floor.
#   3. kacho-iam jwks-rotator CronJob — pod + container restricted floor.
#   4. kacho-geo data-migration Job   — pod + container restricted floor.
#   5. api-gateway external ingress   — backend port `tls` + backend-protocol GRPCS
#                                       (NOT the internal-origin `cmux`/GRPC path).
#
# Mirrors tests/helm/sec-hardening-test.sh: renders via `helm template` and
# asserts with yq. Contracts unchanged (helm/CI/docs only).
set -euo pipefail

SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
DEV="$UMBRELLA/values.dev.yaml"
N=0
fail() { echo "FAIL: $1"; exit 1; }
ok() { N=$((N + 1)); }

command -v yq >/dev/null 2>&1 || fail "yq not installed (mikefarah yq v4 required)"

# render <show-only-template> [extra helm args...] — silence helm kubeconfig warns.
render() {
  local tmpl="$1"; shift
  helm template kacho-umbrella "$UMBRELLA" -f "$DEV" "$@" \
    --show-only "$tmpl" 2>/dev/null
}

# assert_pod_sc <rendered-doc> <podspec-jsonpath> <where>
# Verifies the restricted pod-level securityContext floor.
assert_pod_sc() {
  local doc="$1" podpath="$2" where="$3" sc
  sc=$(echo "$doc" | yq eval-all "select(.kind == \"Job\" or .kind == \"CronJob\") | ${podpath}.securityContext" - 2>/dev/null)
  [ -n "$sc" ] && [ "$sc" != "null" ] || fail "$where: pod has no securityContext"
  [ "$(echo "$sc" | yq '.runAsNonRoot')" = "true" ] || fail "$where: pod runAsNonRoot != true"
  [ "$(echo "$sc" | yq '.seccompProfile.type')" = "RuntimeDefault" ] || fail "$where: pod seccompProfile.type != RuntimeDefault"
  ok
}

# assert_ctr_sc <rendered-doc> <podspec-jsonpath> <container-name> <where>
# Verifies the restricted container-level securityContext floor on a named container.
assert_ctr_sc() {
  local doc="$1" podpath="$2" cname="$3" where="$4" sc
  sc=$(echo "$doc" | yq eval-all \
    "select(.kind == \"Job\" or .kind == \"CronJob\") | ${podpath}.containers[] | select(.name == \"$cname\") | .securityContext" - 2>/dev/null)
  [ -n "$sc" ] && [ "$sc" != "null" ] || fail "$where: container '$cname' has no securityContext"
  [ "$(echo "$sc" | yq '.runAsNonRoot')" = "true" ] || fail "$where/$cname: runAsNonRoot != true"
  [ "$(echo "$sc" | yq '.readOnlyRootFilesystem')" = "true" ] || fail "$where/$cname: readOnlyRootFilesystem != true"
  [ "$(echo "$sc" | yq '.allowPrivilegeEscalation')" = "false" ] || fail "$where/$cname: allowPrivilegeEscalation != false"
  [ "$(echo "$sc" | yq '.capabilities.drop[0]')" = "ALL" ] || fail "$where/$cname: capabilities.drop != [ALL]"
  [ "$(echo "$sc" | yq '.seccompProfile.type')" = "RuntimeDefault" ] || fail "$where/$cname: seccompProfile.type != RuntimeDefault"
  ok
}

POD=".spec.template.spec"
CRONPOD=".spec.jobTemplate.spec.template.spec"

# ── 1. openfga-bootstrap Job ─────────────────────────────────────────────────
# (values.dev.yaml already enables openfga-bootstrap.openfgaBootstrap.enabled)
BOOT=$(render charts/openfga-bootstrap/templates/openfga-bootstrap-job.yaml \
  --set openfga-bootstrap.openfgaBootstrap.enabled=true)
assert_pod_sc "$BOOT" "$POD" "openfga-bootstrap-job"
assert_ctr_sc "$BOOT" "$POD" "bootstrap" "openfga-bootstrap-job"

# ── 2. openfga-postgres-init Job ─────────────────────────────────────────────
PGINIT=$(render charts/openfga-bootstrap/templates/openfga-postgres-init-job.yaml \
  --set openfga-bootstrap.openfgaBootstrap.enabled=true \
  --set openfga-bootstrap.openfgaBootstrap.initDatabase=true)
assert_pod_sc "$PGINIT" "$POD" "openfga-postgres-init-job"
assert_ctr_sc "$PGINIT" "$POD" "postgres-init" "openfga-postgres-init-job"

# ── 3. kacho-iam jwks-rotator CronJob ────────────────────────────────────────
JWKS=$(render charts/kacho-iam/templates/jwks-rotator-cronjob.yaml)
assert_pod_sc "$JWKS" "$CRONPOD" "jwks-rotator-cronjob"
assert_ctr_sc "$JWKS" "$CRONPOD" "jwks-rotator" "jwks-rotator-cronjob"

# ── 4. kacho-geo data-migration Job ──────────────────────────────────────────
GEODM=$(render charts/kacho-geo/templates/geo-data-migration-job.yaml \
  --set kacho-geo.dataMigration.enabled=true)
assert_pod_sc "$GEODM" "$POD" "geo-data-migration-job"
assert_ctr_sc "$GEODM" "$POD" "copy" "geo-data-migration-job"

# ── 5. api-gateway external ingress → external-marked TLS listener ────────────
# Render the FULL umbrella and select the effective api-gateway Ingress, so the
# assertion is agnostic to which template produces it (sub-chart vs umbrella).
ALL=$(helm template kacho-umbrella "$UMBRELLA" -f "$DEV" \
  --set openfga-bootstrap.openfgaBootstrap.enabled=true 2>/dev/null)
ING=$(echo "$ALL" | yq eval-all \
  'select(.kind == "Ingress" and .metadata.name == "api-gateway")' - 2>/dev/null)
[ -n "$ING" ] || fail "api-gateway ingress: no Ingress named 'api-gateway' rendered"
PORT=$(echo "$ING" | yq '.spec.rules[0].http.paths[0].backend.service.port.name')
[ "$PORT" = "tls" ] || fail "api-gateway ingress: backend port is '$PORT', expected 'tls' (external-marked listener; 'cmux' serves Internal* REST externally)"
PROTO=$(echo "$ING" | yq '.metadata.annotations."nginx.ingress.kubernetes.io/backend-protocol"')
[ "$PROTO" = "GRPCS" ] || fail "api-gateway ingress: backend-protocol is '$PROTO', expected 'GRPCS' (TLS re-encrypt to the pod TLS listener)"
# Exactly one Ingress named api-gateway (no double-ingress from sub-chart + umbrella).
COUNT=$(echo "$ALL" | yq eval-all 'select(.kind == "Ingress" and .metadata.name == "api-gateway") | .metadata.name' - 2>/dev/null | grep -c "api-gateway" || true)
[ "$COUNT" = "1" ] || fail "api-gateway ingress: expected exactly 1, found $COUNT (sub-chart ingress must be disabled)"
ok

echo "$SCRIPT: all green ($N assertions)"
