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
#     that has a `mode` knob (api-gateway, kacho-iam, kacho-vpc, kacho-nlb,
#     kacho-storage);
#   - kacho-compute (no `mode` knob — pure internal backend) is fail-closed via
#     per-RPC IAM Check (authzIam non-empty) + list-filter fail-CLOSED + mTLS +
#     DB ssl-mode=require;
#   - kacho-storage is fail-closed on the same axes, and its list-filter must be
#     BOTH enabled AND fail-CLOSED: InternalVolumeService/ListAttachments is
#     ScopeFiltered (no per-RPC Check at all), so the filter is that RPC's only
#     gate and fail-open removes it on any iam error;
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

# ── Verdict discipline ───────────────────────────────────────────────────────
# The verdict is the VIOLATION COUNT, never "the script reached the end".
#
#   violation — a posture assertion failed. Counted, reported, execution CONTINUES
#               so one run lists every violation instead of only the first.
#   fatal     — a precondition failed (profile absent, does not render, wrong yq).
#               Nothing downstream can be judged, so abort — but abort LOUDLY and
#               never as PASS.
#
# SECTIONS counts sections that actually executed and is compared against
# EXPECTED_SECTIONS at the end: a section that gets deleted, commented out or
# skipped must NOT be able to leave a green verdict behind. A gate that prints
# and exits zero is the exact class this file exists to prevent.
SECTIONS=0
VIOLATIONS=0
EXPECTED_SECTIONS=11
violation() { echo "FAIL: $1"; VIOLATIONS=$((VIOLATIONS + 1)); }
fatal() { echo "FATAL: $1"; exit 2; }
ok() { SECTIONS=$((SECTIONS + 1)); }

# ── Preflight: the right yq ──────────────────────────────────────────────────
# Every assertion below is expressed in mikefarah yq v4 syntax (`yq ea`, `select(...)`).
# The unrelated jq-wrapper `yq` (python kislyuk/yq) parses those filters as jq and
# dies mid-pipe with "Cannot index string with string" — a cryptic exit that tells
# the operator nothing about which posture bit was checked. Fail on the TOOL
# explicitly, so a tooling mismatch can never be mistaken for a policy result
# (and, just as important, can never be mistaken for a pass).
command -v yq >/dev/null 2>&1 || fatal "yq not found — install mikefarah yq v4 (https://github.com/mikefarah/yq)"
yq --version 2>&1 | grep -q "mikefarah" \
  || fatal "wrong yq flavour: $(yq --version 2>&1 | head -1) — this guard needs mikefarah yq v4, not the jq wrapper"

[ -f "$PROD" ] || fatal "values.prod.yaml not found at $PROD — production profile missing"
[ -f "$DEV" ]  || fatal "values.dev.yaml not found at $DEV"

# render_only <values-file> <show-only-template> — silence helm's kubeconfig warns.
render_only() {
  helm template kacho-umbrella "$UMBRELLA" -f "$1" --show-only "$2" 2>/dev/null
}
# env_val <ENV_NAME> <render> — value of the named container env entry ("" if absent).
env_val() {
  echo "$2" | yq eval-all \
    "select(.kind==\"Deployment\") | .spec.template.spec.containers[].env[] | select(.name==\"$1\") | .value" -
}
# cm_val <KEY> <render> — value of a ConfigMap data key ("" if absent).
# Services whose knobs reach the container through `envFrom: configMapRef`
# (kacho-storage) keep their posture in the ConfigMap, not in container env, so
# env_val would read empty for every one of them and quietly assert nothing.
cm_val() {
  v="$(echo "$2" | yq eval-all "select(.kind==\"ConfigMap\") | .data.\"$1\"" -)"
  [ "$v" = "null" ] && v=""
  echo "$v"
}

# ── 0. The whole prod profile must render without error ──────────────────────
FULL="$(helm template kacho-umbrella "$UMBRELLA" -f "$PROD" 2>/tmp/prod-guard.err)" \
  || { echo "--- helm error ---"; grep -vE "WARNING: Kubernetes configuration" /tmp/prod-guard.err; \
       fatal "values.prod.yaml does not render via helm template"; }
[ -n "$FULL" ] || fatal "values.prod.yaml render is empty (sibling deps not built? run helm dep update)"; ok

# ── 1. kacho-iam — production-strict + ssl-mode != disable ───────────────────
IAM_CM="$(render_only "$PROD" charts/kacho-iam/templates/configmap.yaml)"
iam_mode="$(echo "$IAM_CM" | yq '.data."config.yaml"' - | yq '.authn.mode' -)"
iam_ssl="$(echo "$IAM_CM" | yq '.data."config.yaml"' - | yq '.repository.postgres."ssl-mode"' -)"
case "$iam_mode" in production|production-strict) ;; *) violation "kacho-iam authn.mode=$iam_mode (want production*, NOT dev)";; esac
[ "$iam_ssl" != "disable" ] && [ -n "$iam_ssl" ] || violation "kacho-iam ssl-mode=$iam_ssl (must NOT be disable)"; ok

# ── 2. kacho-vpc — production + ssl-mode != disable ──────────────────────────
VPC_CM="$(render_only "$PROD" charts/vpc/templates/configmap.yaml)"
vpc_mode="$(echo "$VPC_CM" | yq '.data."config.yaml"' - | yq '.authn.mode' -)"
vpc_ssl="$(echo "$VPC_CM" | yq '.data."config.yaml"' - | yq '.repository.postgres."ssl-mode"' -)"
case "$vpc_mode" in production|production-strict) ;; *) violation "kacho-vpc authn.mode=$vpc_mode (want production*, NOT dev)";; esac
[ "$vpc_ssl" != "disable" ] && [ -n "$vpc_ssl" ] || violation "kacho-vpc ssl-mode=$vpc_ssl (must NOT be disable)"
# fail-closed authz: list-filter must not fail-open.
vpc_lf_fo="$(echo "$VPC_CM" | yq '.data."config.yaml"' - | yq '.authz."list-filter"."fail-open"' -)"
[ "$vpc_lf_fo" = "false" ] || violation "kacho-vpc authz.list-filter.fail-open=$vpc_lf_fo (must be false — fail-closed)"

# authz.breakglass: the knob is GONE, and its absence is the assertion.
#
# It used to be checked as "must be false". The knob was retired with the move to
# the shared carrier: the state it enabled is no longer representable — the field
# exists neither in the config nor in the descriptor, and the decision edge is now
# required in EVERY posture, not just production. Asserting "false" against a
# retired knob turns null into a violation on a CORRECT tree, i.e. the gate goes
# red on the very state it was written to demand.
#
# Inverted rather than deleted: a knob that once bypassed every Check must not
# come back quietly. `null` is the only accepted value, so re-adding it — with any
# value, including false — fails here first.
vpc_bg="$(echo "$VPC_CM" | yq '.data."config.yaml"' - | yq '.authz.breakglass' -)"
[ "$vpc_bg" = "null" ] || violation "kacho-vpc authz.breakglass=$vpc_bg — the knob is retired; its reappearance (with ANY value) means the Check-bypass path came back"
# Who may FORWARD an end-user identity to vpc. An empty list does not mean
# "nobody": corelib narrows the circle only when the list is non-empty, so empty
# means any peer holding an internal-CA certificate may act as any tenant — and
# vpc's public :9090 carries the whole tenant surface. The service refuses to boot
# on an empty list; this asserts the shipped profile never asks it to. Counted with
# `select(. != "")` because a list of empty strings degenerates to empty in corelib.
vpc_fwd="$(echo "$VPC_CM" | yq '.data."config.yaml"' - | yq '[.authz."trusted-forwarder-sans"[] | select(. != "")] | length' -)"
[ "${vpc_fwd:-0}" -gt 0 ] || violation "kacho-vpc authz.trusted-forwarder-sans is empty (any certificate-verified peer could then act as any tenant)"; ok

# ── 3. kacho-nlb — production + sslmode != disable + breakglass off ──────────
NLB_CM="$(render_only "$PROD" charts/kacho-nlb/templates/configmap.yaml)"
nlb_mode="$(echo "$NLB_CM" | yq '.data."config.yaml"' - | yq '.mode' -)"
nlb_dsn="$(echo "$NLB_CM" | yq '.data."config.yaml"' - | yq '.repository.postgres.url' -)"
case "$nlb_mode" in production|production-strict) ;; *) violation "kacho-nlb mode=$nlb_mode (want production*, NOT dev)";; esac
case "$nlb_dsn" in *sslmode=disable*) violation "kacho-nlb DSN has sslmode=disable: $nlb_dsn";; *sslmode=*) ;; *) violation "kacho-nlb DSN missing sslmode: $nlb_dsn";; esac
nlb_bg="$(echo "$NLB_CM" | yq '.data."config.yaml"' - | yq '.authz.breakglass' -)"
[ "$nlb_bg" = "false" ] || violation "kacho-nlb authz.breakglass=$nlb_bg (must be false in production)"; ok

# ── 4. api-gateway — production-strict AuthN + fail-closed AuthZ ──────────────
AGW="$(render_only "$PROD" charts/api-gateway/templates/deployment.yaml)"
agw_mode="$(env_val KACHO_API_GATEWAY_AUTHN_MODE "$AGW")"
case "$agw_mode" in production|production-strict) ;; *) violation "api-gateway AUTHN_MODE=$agw_mode (want production*, NOT dev)";; esac
agw_authz="$(env_val KACHO_API_GATEWAY_AUTHZ_ENABLED "$AGW")"
agw_fo="$(env_val KACHO_API_GATEWAY_AUTHZ_FAIL_OPEN "$AGW")"
[ "$agw_authz" = "true" ] || violation "api-gateway AUTHZ_ENABLED=$agw_authz (must be true in production)"
[ "$agw_fo" = "false" ] || violation "api-gateway AUTHZ_FAIL_OPEN=$agw_fo (must be false — fail-closed)"
# No dev HS256 secret leaks into the prod gateway.
[ -z "$(env_val KACHO_API_GATEWAY_AUTHN_DEV_SECRET "$AGW")" ] || violation "api-gateway leaks AUTHN_DEV_SECRET in production (HS256 dev path must be OFF)"; ok

# ── 5. kacho-compute — fail-closed (no mode knob; posture = authz + ssl) ─────
CMP="$(render_only "$PROD" charts/compute/templates/deployment.yaml)"
cmp_ssl="$(env_val KACHO_COMPUTE_DB_SSLMODE "$CMP")"
cmp_authz_addr="$(env_val KACHO_COMPUTE_AUTHZ_IAM_GRPC_ADDR "$CMP")"
cmp_lf_fo="$(env_val KACHO_COMPUTE_LIST_FILTER_FAIL_OPEN "$CMP")"
[ "$cmp_ssl" != "disable" ] && [ -n "$cmp_ssl" ] || violation "kacho-compute KACHO_COMPUTE_DB_SSLMODE=$cmp_ssl (must NOT be disable)"
[ -n "$cmp_authz_addr" ] || violation "kacho-compute KACHO_COMPUTE_AUTHZ_IAM_GRPC_ADDR empty (per-RPC IAM Check disabled = fail-OPEN)"
[ "$cmp_lf_fo" = "false" ] || violation "kacho-compute KACHO_COMPUTE_LIST_FILTER_FAIL_OPEN=$cmp_lf_fo (must be false — fail-closed)"; ok

# ── 6. kacho-storage — fail-closed, and its list-filter is load-bearing ──────
# storage passes its knobs through a ConfigMap consumed with `envFrom`, so the
# posture lives in the ConfigMap, not in container env (env_val would read empty
# and assert nothing). mTLS is plain container env, read separately below.
#
# LIST_FILTER_FAIL_OPEN is the assertion this section exists for.
# InternalVolumeService/ListAttachments is ScopeFiltered: the per-RPC Check is not
# performed for it at all, so the per-object filter is its ONLY gate. fail-open
# hands back the whole unfiltered page on ANY iam error — i.e. the gate disappears
# precisely when iam is in trouble, and volume attachments of arbitrary named
# instances (other projects, other accounts) become readable. The service now
# refuses to boot on that combination (config.Validate); this asserts the shipped
# profile never asks it to.
STO_CM="$(render_only "$PROD" charts/storage/templates/configmap.yaml)"
STO_DEP="$(render_only "$PROD" charts/storage/templates/deployment.yaml)"
if [ -z "$STO_CM" ]; then
  violation "kacho-storage renders nothing (storage.enabled must be true in the production profile — otherwise the canonical install ships no block storage and this posture is unasserted)"
else
  sto_mode="$(cm_val KACHO_STORAGE_AUTH_MODE "$STO_CM")"
  sto_ssl="$(cm_val KACHO_STORAGE_DB_SSLMODE "$STO_CM")"
  sto_authz_addr="$(cm_val KACHO_STORAGE_AUTHZ_IAM_GRPC_ADDR "$STO_CM")"
  sto_lf_en="$(cm_val KACHO_STORAGE_LIST_FILTER_ENABLED "$STO_CM")"
  sto_lf_fo="$(cm_val KACHO_STORAGE_LIST_FILTER_FAIL_OPEN "$STO_CM")"
  sto_fwd="$(cm_val KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS "$STO_CM")"
  case "$sto_mode" in production|production-strict) ;; *) violation "kacho-storage KACHO_STORAGE_AUTH_MODE=$sto_mode (want production*, NOT dev)";; esac
  [ "$sto_ssl" != "disable" ] && [ -n "$sto_ssl" ] || violation "kacho-storage KACHO_STORAGE_DB_SSLMODE=$sto_ssl (must NOT be disable)"
  [ -n "$sto_authz_addr" ] || violation "kacho-storage KACHO_STORAGE_AUTHZ_IAM_GRPC_ADDR empty (per-RPC IAM Check disabled = fail-OPEN)"
  [ "$sto_lf_en" = "true" ] || violation "kacho-storage KACHO_STORAGE_LIST_FILTER_ENABLED=$sto_lf_en (must be true — it is the only gate of the ScopeFiltered attachment listing)"
  [ "$sto_lf_fo" = "false" ] || violation "kacho-storage KACHO_STORAGE_LIST_FILTER_FAIL_OPEN=$sto_lf_fo (must be false — fail-open returns the page UNFILTERED on any iam error, and the ScopeFiltered attachment listing has no per-RPC Check behind it)"
  # Empty forwarder list does not mean "nobody": corelib narrows the circle only
  # when the list is non-empty, so empty = any certificate-verified peer may
  # forward someone else's identity.
  [ -n "$sto_fwd" ] || violation "kacho-storage KACHO_STORAGE_AUTHZ_TRUSTED_FORWARDER_SANS empty (any certificate-verified peer could then act as any tenant)"
fi
sto_pub_mtls="$(env_val KACHO_STORAGE_PUBLIC_SERVER_MTLS_ENABLE "$STO_DEP")"
sto_int_mtls="$(env_val KACHO_STORAGE_INTERNAL_SERVER_MTLS_ENABLE "$STO_DEP")"
[ "$sto_pub_mtls" = "true" ] || violation "kacho-storage KACHO_STORAGE_PUBLIC_SERVER_MTLS_ENABLE=$sto_pub_mtls (must be true)"
[ "$sto_int_mtls" = "true" ] || violation "kacho-storage KACHO_STORAGE_INTERNAL_SERVER_MTLS_ENABLE=$sto_int_mtls (must be true — internal :9091 is NOT exempt)"; ok

# ── 7. mTLS — internal-CA ClusterIssuer chain + per-service leaf Certificates ─
ncerts="$(echo "$FULL" | yq ea 'select(.kind=="Certificate") | .metadata.name' - | grep -c . || true)"
nissuers="$(echo "$FULL" | yq ea 'select(.kind=="ClusterIssuer") | .metadata.name' - | grep -c . || true)"
[ "$ncerts" -ge 5 ] || violation "production render has only $ncerts cert-manager Certificates (mTLS not wired?)"
[ "$nissuers" -ge 1 ] || violation "production render has no internal-CA ClusterIssuer (SEC-F mTLS PKI not wired?)"; ok

# ── 8. NO secret material committed in values.prod.yaml ──────────────────────
# Credentials must be secretKeyRef / existingSecret only (workspace rule).
grep -iqE "password:[[:space:]]*[\"']?[A-Za-z0-9]" "$PROD" \
  && violation "values.prod.yaml appears to contain a plaintext password — use existingSecret/secretKeyRef" || true
grep -iqE "devSecret:" "$PROD" \
  && violation "values.prod.yaml sets a dev HS256 devSecret — forbidden in the production profile" || true; ok

# ── 9. REGRESSION GUARD — what the DEV base layer still renders ──────────────
# Purpose unchanged: prove that hardening the production profile did not silently
# reshape the local stand. What is EXPECTED of the edge changed, and the assertion
# changed with it rather than being loosened.
#
# The edge line used to require the relaxed mode here, which made this a test that
# PINNED the thing it was protecting: a fix removing the relaxed posture from the base
# layer would have shown up as a red gate, and the obvious reading of that red is
# "somebody broke the dev stand". It is written out so the next reader does not restore
# the old expectation. The two backend lines are unchanged and still require the
# relaxed mode: the production overlay is what lifts them, and that is a separate
# posture question from how a bearer is verified.
DEV_IAM="$(render_only "$DEV" charts/kacho-iam/templates/configmap.yaml | yq '.data."config.yaml"' - | yq '.authn.mode' -)"
DEV_VPC="$(render_only "$DEV" charts/vpc/templates/configmap.yaml | yq '.data."config.yaml"' - | yq '.authn.mode' -)"
DEV_AGW="$(env_val KACHO_API_GATEWAY_AUTHN_MODE "$(render_only "$DEV" charts/api-gateway/templates/deployment.yaml)")"
DEV_AGW_SECRET="$(env_val KACHO_API_GATEWAY_AUTHN_DEV_SECRET "$(render_only "$DEV" charts/api-gateway/templates/deployment.yaml)")"
[ "$DEV_IAM" = "dev" ] || violation "values.dev.yaml kacho-iam authn.mode=$DEV_IAM (expected dev — dev stand changed!)"
[ "$DEV_VPC" = "dev" ] || violation "values.dev.yaml kacho-vpc authn.mode=$DEV_VPC (expected dev — dev stand changed!)"
case "$DEV_AGW" in production|production-strict) ;; *) violation "values.dev.yaml api-gateway AUTHN_MODE=$DEV_AGW (expected production* — a stand that is up verifies bearers by signature, whatever it is called)";; esac
[ -z "$DEV_AGW_SECRET" ] || violation "values.dev.yaml renders a shared signing key into the api-gateway (KACHO_API_GATEWAY_AUTHN_DEV_SECRET) — every holder of the profile would be an issuer"; ok

# ── 10. Per-datastore Postgres NetworkPolicy — ENABLED in production ───────────
# The credential-bearing pg-<svc>:5432 listeners must be ingress-restricted to
# their declared consumers in prod. templates/networkpolicy-datastore.yaml is
# default-off (dev/kind does not enforce NetworkPolicy); the production profile
# MUST flip networkPolicy.datastore.enabled=true, else every pg-* is reachable
# namespace-wide (lateral movement to DB credentials — CIS Kubernetes 5.3.2).
DS_POLICIES="$(echo "$FULL" | yq ea 'select(.kind=="NetworkPolicy" and .metadata.labels."kacho.cloud/component"=="datastore-netpol") | .metadata.name' - | grep -c . || true)"
[ "$DS_POLICIES" -ge 6 ] || violation "production render has only $DS_POLICIES datastore NetworkPolicies (networkPolicy.datastore.enabled not set in values.prod.yaml — every pg-*:5432 stays reachable namespace-wide)"; ok

# ── Verdict — by the counters, never by "we got here" ────────────────────────
# Two independent ways to be red:
#   VIOLATIONS > 0                  — the profile asserts something insecure;
#   SECTIONS  != EXPECTED_SECTIONS  — a section did not run, so its posture bits
#                                     were never examined and silence proves nothing.
# The second is the guard on the guard: without it, deleting a whole section is
# indistinguishable from passing it.
if [ "$SECTIONS" -ne "$EXPECTED_SECTIONS" ]; then
  echo "FAIL: $SCRIPT executed $SECTIONS of $EXPECTED_SECTIONS sections — a section was skipped or removed; its assertions did not run"
  VIOLATIONS=$((VIOLATIONS + 1))
fi
if [ "$VIOLATIONS" -gt 0 ]; then
  echo "FAIL: $SCRIPT — $VIOLATIONS violation(s) in $SECTIONS section(s)"
  exit 1
fi
echo "PASS: $SCRIPT ($SECTIONS sections, 0 violations)"
