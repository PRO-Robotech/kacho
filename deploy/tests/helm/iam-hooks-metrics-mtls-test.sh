#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# kacho-iam Hydra/Kratos hooks listener (:9092) + Prometheus /metrics listener
# (:9095) — per-edge server-side TLS, reusing the SEC-F internal-CA server cert.
#
# Sub-phase 5.5 (kacho-iam#137, supersedes the OFF-gate #122/#136): the hooks edge
# carries THREE HMAC-authenticated hook endpoints (Hydra token/refresh + Kratos
# provision); all three callers are HTTP clients that present NO transport
# client-cert (Ory webhooks cannot). The old RequireAndVerifyClientCert default
# would reject every webhook at the TLS handshake. 5.5 introduces a per-edge
# clientAuthMode: the hooks/metrics edges run `server-tls-only` (encryption +
# server-auth; HMAC is the caller-auth — a single mode on :9092 correctly covers
# all three hook endpoints). The prod gate is now flipped ON.
#
# DETERMINISM NOTE: `helm template` on this large multi-subchart umbrella renders
# the kacho-iam subchart Deployment NON-deterministically (values coalescing — the
# httpListeners-gated env block is present/absent across repeated renders; this
# PRE-DATES #137 and is why the prior guard asserted prod-OFF via env-ABSENCE and
# capability via the TEMPLATE SOURCE). So the prod-ON DECISION is asserted from
# values.prod.yaml directly (yq — deterministic), and CAPABILITY (env names incl.
# the new CLIENTAUTHMODE) from the template source (deterministic). The Hydra
# subchart renders ARE deterministic, so its https-URL + CA-mount are asserted by
# render.
#
# This guard asserts:
#   - PROD values DECISION → kacho-iam.mtls.httpListeners=true with both
#     {hooks,metrics}ClientAuthMode=server-tls-only (gate ON, server-tls-only);
#   - PROD Hydra render → token/refresh webhook URLs https://…:9092 (encrypted),
#     HMAC X-Kacho-Hook-Token kept, Hydra pod mounts the SEC-F internal-CA bundle
#     (kacho-iam-server-tls) + SSL_CERT_FILE so it trusts the kacho-iam server cert;
#   - DEV → httpListeners off (unset/false) + Hydra hook URLs stay plaintext
#     http://…:9092 (newman stand byte-identical);
#   - CAPABILITY INTACT → the kacho-iam deployment TEMPLATE emits every per-edge env
#     incl. CLIENTAUTHMODE, reusing the mounted SEC-F server cert (no new PKI).
#
# Offline manifest-assertion harness (no kind cluster). Mirrors tests/helm/*.
set -euo pipefail

SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
PROD="$UMBRELLA/values.prod.yaml"
DEV="$UMBRELLA/values.dev.yaml"
TPL="$UMBRELLA/charts/kacho-iam/templates/deployment.yaml"
N=0
fail() { echo "FAIL: $1"; exit 1; }
ok() { N=$((N + 1)); }

[ -f "$PROD" ] || fail "values.prod.yaml not found at $PROD"
[ -f "$DEV" ]  || fail "values.dev.yaml not found at $DEV"
[ -f "$TPL" ]  || fail "kacho-iam deployment template not found at $TPL"

render_only() {
  helm template kacho-umbrella "$UMBRELLA" -f "$1" --show-only "$2" 2>/dev/null
}

# Full per-edge env set, INCLUDING the new CLIENTAUTHMODE (M2): the prior array
# knew only ENABLE/CERTFILE/KEYFILE/CLIENTCAFILES — adding CLIENTAUTHMODE makes the
# capability-intact section RED against a template that does not yet emit it.
HOOKS_METRICS_ENV=(
  KACHO_IAM_HOOKS_SERVER_MTLS_ENABLE
  KACHO_IAM_HOOKS_SERVER_MTLS_CLIENTAUTHMODE
  KACHO_IAM_HOOKS_SERVER_MTLS_CERTFILE
  KACHO_IAM_HOOKS_SERVER_MTLS_KEYFILE
  KACHO_IAM_HOOKS_SERVER_MTLS_CLIENTCAFILES
  KACHO_IAM_METRICS_SERVER_MTLS_ENABLE
  KACHO_IAM_METRICS_SERVER_MTLS_CLIENTAUTHMODE
  KACHO_IAM_METRICS_SERVER_MTLS_CERTFILE
  KACHO_IAM_METRICS_SERVER_MTLS_KEYFILE
  KACHO_IAM_METRICS_SERVER_MTLS_CLIENTCAFILES
)

# ── 1. PROD values DECISION — gate ON in server-tls-only mode (deterministic yq) ─
prod_http="$(yq '.["kacho-iam"].mtls.httpListeners' "$PROD")"
[ "$prod_http" = "true" ] \
  || fail "prod: kacho-iam.mtls.httpListeners=$prod_http (want true — hooks/metrics transport hardening ON, kacho-iam#137)"
prod_hooks_mode="$(yq '.["kacho-iam"].mtls.hooksClientAuthMode' "$PROD")"
prod_metrics_mode="$(yq '.["kacho-iam"].mtls.metricsClientAuthMode' "$PROD")"
[ "$prod_hooks_mode" = "server-tls-only" ] \
  || fail "prod: hooksClientAuthMode=$prod_hooks_mode (want server-tls-only — Ory webhooks present no client cert)"
[ "$prod_metrics_mode" = "server-tls-only" ] \
  || fail "prod: metricsClientAuthMode=$prod_metrics_mode (want server-tls-only — no scrape client cert wired)"
# enable=true is the precondition (the SEC-F server cert-trio is mounted only then).
prod_enable="$(yq '.["kacho-iam"].mtls.enable' "$PROD")"
[ "$prod_enable" = "true" ] || fail "prod: kacho-iam.mtls.enable=$prod_enable (httpListeners requires enable=true — no new PKI)"
ok

# ── 2. PROD Hydra — token/refresh webhook URLs https://…:9092 + HMAC kept ────
HYDRA_CM_PROD="$(render_only "$PROD" charts/hydra/templates/configmap.yaml)"
[ -n "$HYDRA_CM_PROD" ] || fail "hydra configmap did not render in prod profile"
echo "$HYDRA_CM_PROD" | grep -Eq 'https://[^"]*:9092/iam/v1/hooks/token' \
  || fail "prod: Hydra token_hook URL must be https://…:9092/iam/v1/hooks/token (got plaintext or missing)"
echo "$HYDRA_CM_PROD" | grep -Eq 'https://[^"]*:9092/iam/v1/hooks/refresh' \
  || fail "prod: Hydra refresh_token_hook URL must be https://…:9092/iam/v1/hooks/refresh"
if echo "$HYDRA_CM_PROD" | grep -Eq 'http://[^"]*:9092/iam/v1/hooks/'; then
  fail "prod: Hydra hook URL still uses plaintext http://…:9092 (must be https for server-tls-only)"
fi
echo "$HYDRA_CM_PROD" | grep -q 'X-Kacho-Hook-Token' \
  || fail "prod: Hydra webhook must still carry the HMAC header X-Kacho-Hook-Token (caller-auth unchanged)"
ok

# ── 3. PROD Hydra pod mounts the internal-CA bundle (trusts kacho-iam server cert) ─
HYDRA_DEPLOY_PROD="$(render_only "$PROD" charts/hydra/templates/deployment.yaml)"
[ -n "$HYDRA_DEPLOY_PROD" ] || fail "hydra deployment did not render in prod profile"
echo "$HYDRA_DEPLOY_PROD" | grep -q 'kacho-iam-server-tls' \
  || fail "prod: Hydra pod must mount the SEC-F internal-CA bundle (kacho-iam-server-tls) for webhook CA-trust"
echo "$HYDRA_DEPLOY_PROD" | grep -qE 'name: SSL_CERT_FILE|name: SSL_CERT_DIR' \
  || fail "prod: Hydra must set SSL_CERT_FILE/SSL_CERT_DIR so its webhook client trusts the internal-CA server cert"
ok

# ── 4. DEV — httpListeners off + Hydra hook URLs stay plaintext ──────────────
dev_http="$(yq '.["kacho-iam"].mtls.httpListeners // false' "$DEV")"
[ "$dev_http" != "true" ] \
  || fail "dev: kacho-iam.mtls.httpListeners=$dev_http (dev hooks/metrics listener must stay PLAINTEXT — regression!)"
HYDRA_CM_DEV="$(render_only "$DEV" charts/hydra/templates/configmap.yaml)"
echo "$HYDRA_CM_DEV" | grep -Eq 'http://[^"]*:9092/iam/v1/hooks/token' \
  || fail "dev: Hydra token_hook URL must stay plaintext http://…:9092 (newman stand unchanged)"
ok

# ── 5. CAPABILITY INTACT — the chart emits the gated block + every env (CLIENTAUTHMODE) ─
grep -q '{{- if .Values.mtls.httpListeners }}' "$TPL" \
  || fail "capability: the '{{- if .Values.mtls.httpListeners }}' gate block was removed from the template"
for name in "${HOOKS_METRICS_ENV[@]}"; do
  grep -q "name: $name" "$TPL" \
    || fail "capability: env $name missing from template — server-side TLS support / CLIENTAUTHMODE not emitted"
done
# Both CLIENTAUTHMODE env default to server-tls-only in the template.
grep -A1 'KACHO_IAM_HOOKS_SERVER_MTLS_CLIENTAUTHMODE' "$TPL" | grep -q 'hooksClientAuthMode' \
  || fail "capability: hooks CLIENTAUTHMODE must derive from .Values.mtls.hooksClientAuthMode"
grep -A1 'KACHO_IAM_METRICS_SERVER_MTLS_CLIENTAUTHMODE' "$TPL" | grep -q 'metricsClientAuthMode' \
  || fail "capability: metrics CLIENTAUTHMODE must derive from .Values.mtls.metricsClientAuthMode"
# The hooks/metrics block must REUSE the mounted server cert-trio (no new PKI).
grep -A1 'KACHO_IAM_HOOKS_SERVER_MTLS_CERTFILE' "$TPL" | grep -q 'tls.crt' \
  || fail "capability: hooks certfile must reuse the mounted server tls.crt (SEC-F)"
ok

echo "PASS: $SCRIPT ($N assertions)"
