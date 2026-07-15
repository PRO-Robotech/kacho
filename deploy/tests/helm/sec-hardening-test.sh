#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# INFRA sec-hardening manifest-assertion guard (offline; no kind cluster).
#
# Asserts the container/pod hardening re-applied on the current chart structure:
#   1. kacho-iam + kacho-geo workloads carry a hardened pod- AND per-container
#      securityContext (runAsNonRoot, readOnlyRootFilesystem, drop ALL caps,
#      allowPrivilegeEscalation=false, seccompProfile RuntimeDefault) on EVERY
#      container incl. init-containers — not only the OPA sidecar.
#   2. The umbrella ships a Namespace template carrying Pod Security Admission
#      warn+audit=restricted labels for the kacho namespace.
#   3. openfga-bootstrap Role scopes its secrets get/update/patch rule with
#      resourceNames (least-privilege — no namespace-wide secret read).
#   4. Image references support a digest-pin override (repository@sha256:...).
#
# Mirrors tests/helm/*-test.sh: renders via `helm template ... --show-only` and
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

# assert_container_hardened <rendered-doc> <container-jsonpath-name>
# Verifies the securityContext floor on the container selected by name.
assert_sc() {
  local doc="$1" cname="$2" where="$3"
  local sc
  sc=$(echo "$doc" | yq eval-all \
    "select(.kind == \"Deployment\") | (.spec.template.spec.containers[], .spec.template.spec.initContainers[]) | select(.name == \"$cname\") | .securityContext" - 2>/dev/null)
  [ -n "$sc" ] && [ "$sc" != "null" ] || fail "$where: container '$cname' has no securityContext"
  [ "$(echo "$sc" | yq '.runAsNonRoot')" = "true" ] || fail "$where/$cname: runAsNonRoot != true"
  [ "$(echo "$sc" | yq '.readOnlyRootFilesystem')" = "true" ] || fail "$where/$cname: readOnlyRootFilesystem != true"
  [ "$(echo "$sc" | yq '.allowPrivilegeEscalation')" = "false" ] || fail "$where/$cname: allowPrivilegeEscalation != false"
  [ "$(echo "$sc" | yq '.capabilities.drop[0]')" = "ALL" ] || fail "$where/$cname: capabilities.drop != [ALL]"
  [ "$(echo "$sc" | yq '.seccompProfile.type')" = "RuntimeDefault" ] || fail "$where/$cname: seccompProfile.type != RuntimeDefault"
  ok
}

# ── 1. kacho-iam workload hardening ──────────────────────────────────────────
IAM=$(render charts/kacho-iam/templates/deployment.yaml)
POD_SC=$(echo "$IAM" | yq 'select(.kind == "Deployment") | .spec.template.spec.securityContext')
[ "$(echo "$POD_SC" | yq '.runAsNonRoot')" = "true" ] || fail "kacho-iam: pod securityContext.runAsNonRoot != true"
[ "$(echo "$POD_SC" | yq '.seccompProfile.type')" = "RuntimeDefault" ] || fail "kacho-iam: pod seccompProfile != RuntimeDefault"
ok
assert_sc "$IAM" "kacho-iam" "kacho-iam"
assert_sc "$IAM" "migrate" "kacho-iam"
assert_sc "$IAM" "wait-for-openfga" "kacho-iam"

# ── 2. kacho-geo workload hardening ──────────────────────────────────────────
GEO=$(render charts/kacho-geo/templates/deployment.yaml)
GPOD_SC=$(echo "$GEO" | yq 'select(.kind == "Deployment") | .spec.template.spec.securityContext')
[ "$(echo "$GPOD_SC" | yq '.runAsNonRoot')" = "true" ] || fail "kacho-geo: pod securityContext.runAsNonRoot != true"
ok
assert_sc "$GEO" "kacho-geo" "kacho-geo"
assert_sc "$GEO" "migrate" "kacho-geo"

# ── 3. Pod Security Admission namespace labels (warn+audit=restricted) ────────
NS=$(render templates/namespace.yaml --set namespace.create=true)
[ "$(echo "$NS" | yq 'select(.kind == "Namespace") | .metadata.labels."pod-security.kubernetes.io/warn"')" = "restricted" ] \
  || fail "namespace: pod-security warn label != restricted"
[ "$(echo "$NS" | yq 'select(.kind == "Namespace") | .metadata.labels."pod-security.kubernetes.io/audit"')" = "restricted" ] \
  || fail "namespace: pod-security audit label != restricted"
ok

# ── 4. openfga-bootstrap RBAC least-privilege (scoped secrets rule) ───────────
RBAC=$(render charts/openfga-bootstrap/templates/openfga-bootstrap-rbac.yaml --set openfgaBootstrap.enabled=true)
SCOPED=$(echo "$RBAC" | yq 'select(.kind == "Role") | .rules[] | select(.resources[] == "secrets") | select(.resourceNames != null) | .resourceNames | join(",")' 2>/dev/null | head -1)
echo "$SCOPED" | grep -q "kacho-iam-openfga-store" || fail "openfga-bootstrap Role: secrets rule not scoped to kacho-iam-openfga-store via resourceNames"
echo "$SCOPED" | grep -q "openfga-model-id" || fail "openfga-bootstrap Role: secrets rule not scoped to openfga-model-id via resourceNames"
# No unrestricted get on all secrets: every rule granting `get` on secrets must carry resourceNames.
UNSCOPED_GET=$(echo "$RBAC" | yq 'select(.kind == "Role") | .rules[] | select(.resources[] == "secrets") | select(.verbs[] == "get") | select(.resourceNames == null) | .verbs | join(",")' 2>/dev/null)
[ -z "$UNSCOPED_GET" ] || fail "openfga-bootstrap Role: a secrets rule still grants get with no resourceNames"
# The deployments get/patch rule is resourceName-scoped to ONLY the consumer
# Deployments the Job bumps (no namespace-wide deployment patch → no lateral
# image/sidecar swap on a compromised bootstrap SA).
DEP_SCOPED=$(echo "$RBAC" | yq 'select(.kind == "Role") | .rules[] | select(.resources[] == "deployments") | select(.resourceNames != null) | .resourceNames | join(",")' 2>/dev/null | head -1)
for d in kacho-iam api-gateway vpc compute loadbalancer; do
  echo "$DEP_SCOPED" | grep -q "$d" || fail "openfga-bootstrap Role: deployments rule not scoped to $d via resourceNames"
done
# No deployments rule may grant patch/get without resourceNames.
UNSCOPED_DEP=$(echo "$RBAC" | yq 'select(.kind == "Role") | .rules[] | select(.resources[] == "deployments") | select(.resourceNames == null) | .verbs | join(",")' 2>/dev/null)
[ -z "$UNSCOPED_DEP" ] || fail "openfga-bootstrap Role: a deployments rule still grants $UNSCOPED_DEP with no resourceNames"
ok

# ── 5. Image digest-pin override (repository@sha256:...) ──────────────────────
DIG="sha256:0000000000000000000000000000000000000000000000000000000000000000"
IAM_DIG=$(render charts/kacho-iam/templates/deployment.yaml --set kacho-iam.image.digest="$DIG")
echo "$IAM_DIG" | yq 'select(.kind == "Deployment") | .spec.template.spec.containers[0].image' | grep -q "@$DIG" \
  || fail "kacho-iam: image.digest override not honoured (expected repository@$DIG)"
GEO_DIG=$(render charts/kacho-geo/templates/deployment.yaml --set kacho-geo.imageDigest="$DIG")
echo "$GEO_DIG" | yq 'select(.kind == "Deployment") | .spec.template.spec.containers[0].image' | grep -q "@$DIG" \
  || fail "kacho-geo: imageDigest override not honoured (expected repository@$DIG)"
ok

echo "$SCRIPT: all green ($N assertions)"
