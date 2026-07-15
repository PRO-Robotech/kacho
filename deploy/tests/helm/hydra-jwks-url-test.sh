#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# api-gateway must resolve a REACHABLE cluster-internal Hydra JWKS URL.
#
# Bug: the api-gateway auth path validates Hydra-issued RS256 login tokens by
# fetching Hydra's JWKS. cfg.ResolvedHydraJWKSURL() reads KACHO_HYDRA_JWKS_URL
# (explicit) and otherwise derives `{HydraIssuer}/.well-known/jwks.json` — whose
# default issuer (`https://hydra.api.kacho.cloud`) is NOT reachable in-cluster
# (Hydra self.issuer in dev is `http://localhost:28080/...`). With no env set the
# gateway pod fetches an unreachable URL → JWKS load fails → Hydra tokens are
# never validated → WhoAmI/Account/Project return code 16 AUTHN_REQUIRED.
#
# The dev stand's in-cluster Hydra PUBLIC Service is `kacho-umbrella-hydra-public`
# (release `kacho-umbrella`), port 4444, JWKS path `/.well-known/jwks.json`
# (verified: `helm template ... charts/hydra/templates/service-public.yaml`).
#
# This renders BOTH:
#   (1) the sibling api-gateway chart standalone (source the umbrella vendors via
#       file://../../../kacho-api-gateway/deploy — same pattern as
#       service-mtls-wiring-test.sh) with values that set hydra.jwksUrl, and
#   (2) the umbrella with values.dev.yaml (the actual dev stand) restricted to
#       the api-gateway Deployment.
# It asserts the rendered KACHO_HYDRA_JWKS_URL is the cluster-internal hydra-public
# endpoint — never localhost, never the public `hydra.<domain>` issuer.
#
# Offline manifest-assertion harness (no kind cluster). Mirrors tests/helm/*.
set -euo pipefail

SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
PROJ="$(cd "$REPO_ROOT/.." && pwd)"   # project/  (siblings live next to kacho-deploy)
UMBRELLA="$REPO_ROOT/helm/umbrella"
AGW="$PROJ/kacho-api-gateway/deploy"
WANT="http://kacho-umbrella-hydra-public.kacho.svc.cluster.local:4444/.well-known/jwks.json"
N=0
fail() { echo "FAIL: $1"; exit 1; }
ok() { N=$((N + 1)); }

# env_val <ENV_NAME> <render> — value of the named container env entry ("" if absent).
env_val() {
  echo "$2" | yq eval-all \
    "select(.kind==\"Deployment\") | .spec.template.spec.containers[].env[] | select(.name==\"$1\") | .value" -
}

[ -d "$AGW" ] || fail "kacho-api-gateway chart not found at $AGW"

# ── (1) sibling chart standalone — hydra.jwksUrl drives the env ───────────────
ON="$(helm template ag "$AGW" --set hydra.jwksUrl="$WANT" 2>/dev/null)"
jw="$(env_val KACHO_HYDRA_JWKS_URL "$ON")"
[ -n "$jw" ] || fail "sibling chart did not render KACHO_HYDRA_JWKS_URL env when hydra.jwksUrl set"
[ "$jw" = "$WANT" ] || fail "sibling KACHO_HYDRA_JWKS_URL=$jw (want $WANT)"; ok
case "$jw" in
  *localhost*) fail "sibling JWKS URL points at localhost ($jw) — unreachable in-cluster" ;;
  https://hydra.*) fail "sibling JWKS URL points at the PUBLIC issuer ($jw) — unreachable in-cluster" ;;
esac; ok

# Default (no hydra.jwksUrl) must NOT leak the env — Go config default applies,
# zero regression for overlays that don't opt in.
OFF="$(helm template ag "$AGW" 2>/dev/null)"
[ -z "$(env_val KACHO_HYDRA_JWKS_URL "$OFF")" ] || fail "sibling leaks KACHO_HYDRA_JWKS_URL when hydra.jwksUrl unset"; ok

# ── (2) umbrella + values.dev.yaml — the actual dev stand ─────────────────────
# `helm template` resolves the file:// api-gateway dep from the vendored .tgz; if
# the dep is stale this still renders the committed chart. Restrict to the
# api-gateway Deployment via --show-only.
DEV="$(helm template kacho-umbrella "$UMBRELLA" -f "$UMBRELLA/values.dev.yaml" \
        --show-only charts/api-gateway/templates/deployment.yaml 2>/dev/null)"
[ -n "$DEV" ] || fail "umbrella render of api-gateway deployment is empty (dep not built? run helm dep update)"
djw="$(env_val KACHO_HYDRA_JWKS_URL "$DEV")"
[ -n "$djw" ] || fail "dev stand api-gateway pod has NO KACHO_HYDRA_JWKS_URL env — gateway will fetch unreachable default"
[ "$djw" = "$WANT" ] || fail "dev KACHO_HYDRA_JWKS_URL=$djw (want cluster-internal $WANT)"; ok
case "$djw" in
  *localhost*) fail "dev JWKS URL points at localhost ($djw) — gateway pod cannot reach it" ;;
  https://hydra.*) fail "dev JWKS URL points at PUBLIC issuer ($djw) — not reachable from the gateway pod" ;;
esac; ok

# SEC-J: the verifier does an EXACT-match `iss` check, so the dev gateway issuer
# MUST equal Hydra's dev self.issuer (values.dev.yaml hydra.config.urls.self.issuer
# = http://localhost:28080/.ory/hydra/public/). Without it, KACHO_HYDRA_ISSUER
# derives the unreachable external default → every real login token fails the iss
# check → AUTHN_REQUIRED persists even with a reachable JWKS URL.
DEV_ISSUER="http://localhost:28080/.ory/hydra/public/"
dis="$(env_val KACHO_HYDRA_ISSUER "$DEV")"
[ "$dis" = "$DEV_ISSUER" ] || fail "dev KACHO_HYDRA_ISSUER=$dis (want $DEV_ISSUER matching Hydra dev self.issuer)"; ok

# ── (3) umbrella + values.prod.yaml — production-strict makes the verifier
#        mandatory, so the JWKS URL must STILL be the in-cluster Service (not the
#        public ingress hairpin). The expected `iss` is the public issuer.
PROD="$(helm template kacho-umbrella "$UMBRELLA" -f "$UMBRELLA/values.prod.yaml" \
         --show-only charts/api-gateway/templates/deployment.yaml 2>/dev/null)"
[ -n "$PROD" ] || fail "umbrella render of api-gateway deployment (prod) is empty"
pjw="$(env_val KACHO_HYDRA_JWKS_URL "$PROD")"
[ "$pjw" = "$WANT" ] || fail "prod KACHO_HYDRA_JWKS_URL=$pjw (want cluster-internal $WANT)"
case "$pjw" in
  https://hydra.*) fail "prod JWKS URL is the public ingress ($pjw) — must hit the in-cluster Service" ;;
esac
pis="$(env_val KACHO_HYDRA_ISSUER "$PROD")"
[ "$pis" = "https://hydra.api.kacho.cloud" ] || fail "prod KACHO_HYDRA_ISSUER=$pis (want public issuer https://hydra.api.kacho.cloud)"; ok

echo "PASS: $SCRIPT ($N assertions)"
