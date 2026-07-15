#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# values.prod.yaml is the documented PRODUCTION profile and MUST be fail-closed.
#
# Background: the chart DEFAULT (values.yaml) + the dev/CI profile
# (values.dev.yaml) deliberately run `mode: dev` (anonymous → full access) +
# `ssl-mode: disable`. A bare `helm install` therefore lands on the insecure dev
# posture (P0 audit finding). values.prod.yaml is the hardened profile production
# rollouts pass explicitly. This guard renders it via `helm template` and asserts
# the security floor for EVERY service:
#   - auth mode is production / production-strict (NEVER dev) on every service
#     that has a `mode` knob (api-gateway, kacho-iam, kacho-vpc, kacho-nlb);
#   - kacho-compute (no `mode` knob — pure internal backend) is fail-closed via
#     per-RPC IAM Check (authzIam non-empty) + list-filter fail-CLOSED + mTLS +
#     DB ssl-mode=require;
#   - Postgres ssl-mode is NEVER `disable` (encrypted transport);
#   - mTLS is ON (cert-manager internal-CA Certificates render) + fail-closed authz.
#
# This guard does NOT assert "no values file has mode:dev" globally —
# values.dev.yaml legitimately carries `mode: dev` and powers the newman CI
# stand. It asserts ONLY that the PROD profile is hardened, and (regression
# guard) that the DEV profile still renders mode:dev unchanged.
#
# Offline manifest-assertion harness (no kind cluster). Mirrors tests/helm/*.
set -euo pipefail

SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
PROD="$UMBRELLA/values.prod.yaml"
DEV="$UMBRELLA/values.dev.yaml"
N=0
fail() { echo "FAIL: $1"; exit 1; }
ok() { N=$((N + 1)); }

[ -f "$PROD" ] || fail "values.prod.yaml not found at $PROD — production profile missing"
[ -f "$DEV" ]  || fail "values.dev.yaml not found at $DEV"

# render_only <values-file> <show-only-template> — silence helm's kubeconfig warns.
render_only() {
  helm template kacho-umbrella "$UMBRELLA" -f "$1" --show-only "$2" 2>/dev/null
}
# env_val <ENV_NAME> <render> — value of the named container env entry ("" if absent).
env_val() {
  echo "$2" | yq eval-all \
    "select(.kind==\"Deployment\") | .spec.template.spec.containers[].env[] | select(.name==\"$1\") | .value" -
}

# ── 0. The whole prod profile must render without error ──────────────────────
FULL="$(helm template kacho-umbrella "$UMBRELLA" -f "$PROD" 2>/tmp/prod-guard.err)" \
  || { echo "--- helm error ---"; grep -vE "WARNING: Kubernetes configuration" /tmp/prod-guard.err; \
       fail "values.prod.yaml does not render via helm template"; }
[ -n "$FULL" ] || fail "values.prod.yaml render is empty (sibling deps not built? run helm dep update)"; ok

# ── 1. kacho-iam — production-strict + ssl-mode != disable ───────────────────
IAM_CM="$(render_only "$PROD" charts/kacho-iam/templates/configmap.yaml)"
iam_mode="$(echo "$IAM_CM" | yq '.data."config.yaml"' - | yq '.authn.mode' -)"
iam_ssl="$(echo "$IAM_CM" | yq '.data."config.yaml"' - | yq '.repository.postgres."ssl-mode"' -)"
case "$iam_mode" in production|production-strict) ;; *) fail "kacho-iam authn.mode=$iam_mode (want production*, NOT dev)";; esac
[ "$iam_ssl" != "disable" ] && [ -n "$iam_ssl" ] || fail "kacho-iam ssl-mode=$iam_ssl (must NOT be disable)"; ok

# ── 2. kacho-vpc — production + ssl-mode != disable ──────────────────────────
VPC_CM="$(render_only "$PROD" charts/vpc/templates/configmap.yaml)"
vpc_mode="$(echo "$VPC_CM" | yq '.data."config.yaml"' - | yq '.authn.mode' -)"
vpc_ssl="$(echo "$VPC_CM" | yq '.data."config.yaml"' - | yq '.repository.postgres."ssl-mode"' -)"
case "$vpc_mode" in production|production-strict) ;; *) fail "kacho-vpc authn.mode=$vpc_mode (want production*, NOT dev)";; esac
[ "$vpc_ssl" != "disable" ] && [ -n "$vpc_ssl" ] || fail "kacho-vpc ssl-mode=$vpc_ssl (must NOT be disable)"
# fail-closed authz: list-filter must not fail-open, breakglass must be off.
vpc_lf_fo="$(echo "$VPC_CM" | yq '.data."config.yaml"' - | yq '.authz."list-filter"."fail-open"' -)"
vpc_bg="$(echo "$VPC_CM" | yq '.data."config.yaml"' - | yq '.authz.breakglass' -)"
[ "$vpc_lf_fo" = "false" ] || fail "kacho-vpc authz.list-filter.fail-open=$vpc_lf_fo (must be false — fail-closed)"
[ "$vpc_bg" = "false" ] || fail "kacho-vpc authz.breakglass=$vpc_bg (must be false in production)"; ok

# ── 3. kacho-nlb — production + sslmode != disable + breakglass off ──────────
NLB_CM="$(render_only "$PROD" charts/kacho-nlb/templates/configmap.yaml)"
nlb_mode="$(echo "$NLB_CM" | yq '.data."config.yaml"' - | yq '.mode' -)"
nlb_dsn="$(echo "$NLB_CM" | yq '.data."config.yaml"' - | yq '.repository.postgres.url' -)"
case "$nlb_mode" in production|production-strict) ;; *) fail "kacho-nlb mode=$nlb_mode (want production*, NOT dev)";; esac
case "$nlb_dsn" in *sslmode=disable*) fail "kacho-nlb DSN has sslmode=disable: $nlb_dsn";; *sslmode=*) ;; *) fail "kacho-nlb DSN missing sslmode: $nlb_dsn";; esac
nlb_bg="$(echo "$NLB_CM" | yq '.data."config.yaml"' - | yq '.authz.breakglass' -)"
[ "$nlb_bg" = "false" ] || fail "kacho-nlb authz.breakglass=$nlb_bg (must be false in production)"; ok

# ── 4. api-gateway — production-strict AuthN + fail-closed AuthZ ──────────────
AGW="$(render_only "$PROD" charts/api-gateway/templates/deployment.yaml)"
agw_mode="$(env_val KACHO_API_GATEWAY_AUTHN_MODE "$AGW")"
case "$agw_mode" in production|production-strict) ;; *) fail "api-gateway AUTHN_MODE=$agw_mode (want production*, NOT dev)";; esac
agw_authz="$(env_val KACHO_API_GATEWAY_AUTHZ_ENABLED "$AGW")"
agw_fo="$(env_val KACHO_API_GATEWAY_AUTHZ_FAIL_OPEN "$AGW")"
[ "$agw_authz" = "true" ] || fail "api-gateway AUTHZ_ENABLED=$agw_authz (must be true in production)"
[ "$agw_fo" = "false" ] || fail "api-gateway AUTHZ_FAIL_OPEN=$agw_fo (must be false — fail-closed)"
# No dev HS256 secret leaks into the prod gateway.
[ -z "$(env_val KACHO_API_GATEWAY_AUTHN_DEV_SECRET "$AGW")" ] || fail "api-gateway leaks AUTHN_DEV_SECRET in production (HS256 dev path must be OFF)"; ok

# ── 5. kacho-compute — fail-closed (no mode knob; posture = authz + ssl) ─────
CMP="$(render_only "$PROD" charts/compute/templates/deployment.yaml)"
cmp_ssl="$(env_val KACHO_COMPUTE_DB_SSLMODE "$CMP")"
cmp_authz_addr="$(env_val KACHO_COMPUTE_AUTHZ_IAM_GRPC_ADDR "$CMP")"
cmp_lf_fo="$(env_val KACHO_COMPUTE_LIST_FILTER_FAIL_OPEN "$CMP")"
[ "$cmp_ssl" != "disable" ] && [ -n "$cmp_ssl" ] || fail "kacho-compute KACHO_COMPUTE_DB_SSLMODE=$cmp_ssl (must NOT be disable)"
[ -n "$cmp_authz_addr" ] || fail "kacho-compute KACHO_COMPUTE_AUTHZ_IAM_GRPC_ADDR empty (per-RPC IAM Check disabled = fail-OPEN)"
[ "$cmp_lf_fo" = "false" ] || fail "kacho-compute KACHO_COMPUTE_LIST_FILTER_FAIL_OPEN=$cmp_lf_fo (must be false — fail-closed)"; ok

# ── 6. mTLS — internal-CA ClusterIssuer chain + per-service leaf Certificates ─
ncerts="$(echo "$FULL" | yq ea 'select(.kind=="Certificate") | .metadata.name' - | grep -c . || true)"
nissuers="$(echo "$FULL" | yq ea 'select(.kind=="ClusterIssuer") | .metadata.name' - | grep -c . || true)"
[ "$ncerts" -ge 5 ] || fail "production render has only $ncerts cert-manager Certificates (mTLS not wired?)"
[ "$nissuers" -ge 1 ] || fail "production render has no internal-CA ClusterIssuer (SEC-F mTLS PKI not wired?)"; ok

# ── 7. NO secret material committed in values.prod.yaml ──────────────────────
# Credentials must be secretKeyRef / existingSecret only (workspace rule).
grep -iqE "password:[[:space:]]*[\"']?[A-Za-z0-9]" "$PROD" \
  && fail "values.prod.yaml appears to contain a plaintext password — use existingSecret/secretKeyRef" || true
grep -iqE "devSecret:" "$PROD" \
  && fail "values.prod.yaml sets a dev HS256 devSecret — forbidden in the production profile" || true; ok

# ── 8. REGRESSION GUARD — the DEV profile still renders mode:dev (untouched) ──
# Proves hardening the prod profile did NOT change the dev/CI stand.
DEV_IAM="$(render_only "$DEV" charts/kacho-iam/templates/configmap.yaml | yq '.data."config.yaml"' - | yq '.authn.mode' -)"
DEV_VPC="$(render_only "$DEV" charts/vpc/templates/configmap.yaml | yq '.data."config.yaml"' - | yq '.authn.mode' -)"
DEV_AGW="$(env_val KACHO_API_GATEWAY_AUTHN_MODE "$(render_only "$DEV" charts/api-gateway/templates/deployment.yaml)")"
[ "$DEV_IAM" = "dev" ] || fail "values.dev.yaml kacho-iam authn.mode=$DEV_IAM (expected dev — dev stand changed!)"
[ "$DEV_VPC" = "dev" ] || fail "values.dev.yaml kacho-vpc authn.mode=$DEV_VPC (expected dev — dev stand changed!)"
[ "$DEV_AGW" = "dev" ] || fail "values.dev.yaml api-gateway AUTHN_MODE=$DEV_AGW (expected dev — dev stand changed!)"; ok

# ── 9. Per-datastore Postgres NetworkPolicy — ENABLED in production ───────────
# The credential-bearing pg-<svc>:5432 listeners must be ingress-restricted to
# their declared consumers in prod. templates/networkpolicy-datastore.yaml is
# default-off (dev/kind does not enforce NetworkPolicy); the production profile
# MUST flip networkPolicy.datastore.enabled=true, else every pg-* is reachable
# namespace-wide (lateral movement to DB credentials — CIS Kubernetes 5.3.2).
DS_POLICIES="$(echo "$FULL" | yq ea 'select(.kind=="NetworkPolicy" and .metadata.labels."kacho.cloud/component"=="datastore-netpol") | .metadata.name' - | grep -c . || true)"
[ "$DS_POLICIES" -ge 6 ] || fail "production render has only $DS_POLICIES datastore NetworkPolicies (networkPolicy.datastore.enabled not set in values.prod.yaml — every pg-*:5432 stays reachable namespace-wide)"; ok

echo "PASS: $SCRIPT ($N assertions)"
