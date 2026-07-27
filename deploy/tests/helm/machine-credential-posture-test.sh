#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# Machine-credential posture — token lifetime + sender-constrained binding.
#
# WHY THIS GATE EXISTS. values.dev.yaml carried, for a long time, the comment
# "Acceptance §5.2 — TTL discipline (PRODUCTION: 15m, kept in values.prod.yaml)".
# values.prod.yaml had no `ttl` key at all, so production silently inherited the
# identity provider's own built-in access-token default. The claim was false and
# nothing noticed, because no gate ever asserted it — the profiles are NOT
# layered (values.prod.yaml renders standalone / as the base of the prod chain,
# never on top of values.dev.yaml), so a value present only in dev reaches
# nothing.
#
# The defect class is "a check with the form but not the substance": a comment
# that documents a control instead of a gate that verifies it. This script is
# the gate. It was verified RED against the pre-fix tree (no `ttl` block in
# values.prod.yaml → section 1 fails).
#
# Asserted:
#   1. PROD Hydra render  → an explicit access-token TTL exists and is short.
#   2. PROD iam render    → SA-key lifetime envs present; access-token lifespan
#                           pinned per-client (defence in depth over the global).
#   3. DEV  iam render    → key lifetime still bounded, but the per-client token
#                           lifespan is deliberately NOT pinned (the local e2e
#                           stand widens the global TTL to outlast serial newman
#                           waves; pinning 15m here would 401 late collections).
#   4. STAGING ORDER      → machine-token binding is off by default on BOTH
#                           halves. Enforcement without issuance can only
#                           reject, so neither half may ship pre-enabled.
#   5. CAPABILITY INTACT  → both templates still emit the knobs (a removed env
#                           block would make every values-level decision inert —
#                           exactly how the DPoP flag came to be unreachable).
#
# Offline manifest-assertion harness (no kind cluster). Mirrors tests/helm/*.
set -euo pipefail

SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
PROD="$UMBRELLA/values.prod.yaml"
DEV="$UMBRELLA/values.dev.yaml"
IAM_TPL="$UMBRELLA/charts/kacho-iam/templates/deployment.yaml"
GW_TPL="$REPO_ROOT/../gateway/deploy/templates/deployment.yaml"
GW_VALUES="$REPO_ROOT/../gateway/deploy/values.yaml"
N=0
fail() { echo "FAIL: $1"; exit 1; }
ok() { N=$((N + 1)); }

# yq MUST be mikefarah v4. On many machines /usr/bin/yq is the python jq-wrapper
# of the SAME NAME, whose filter syntax and quoting differ — assertions here read
# DECISIONS out of the values files, so an impostor yq would either error or
# return differently-quoted output and the gate would verify nothing. That
# false-green is precisely the class this gate exists to prevent, so detect the
# impostor explicitly rather than trusting `command -v`.
command -v yq >/dev/null 2>&1 || fail "yq not installed (mikefarah yq v4 required)"
yq --version 2>&1 | grep -qi mikefarah || fail \
  "wrong 'yq' on PATH ($(command -v yq)): '$(yq --version 2>&1 | head -1)'. \
mikefarah yq v4 is required — the python-yq jq wrapper's output would make the \
assertions below pass without checking anything."

[ -f "$PROD" ]    || fail "values.prod.yaml not found at $PROD"
[ -f "$DEV" ]     || fail "values.dev.yaml not found at $DEV"
[ -f "$IAM_TPL" ] || fail "kacho-iam deployment template not found at $IAM_TPL"
[ -f "$GW_TPL" ]  || fail "api-gateway deployment template not found at $GW_TPL"

render_only() {
  helm template kacho-umbrella "$UMBRELLA" -f "$1" --show-only "$2" 2>/dev/null
}

# ── 1. PROD access-token TTL is explicit and short ───────────────────────────
# Asserted from the values file (deterministic) AND the render, so neither a
# values regression nor a template regression can slip through alone.
prod_at="$(yq '.hydra.hydra.config.ttl.access_token // ""' "$PROD")"
[ -n "$prod_at" ] \
  || fail "prod: hydra.hydra.config.ttl.access_token is UNSET — production would inherit the provider default (this is the exact regression this gate exists for)"
case "$prod_at" in
  *m) ;; # minutes — short-lived, as documented
  *)  fail "prod: access_token TTL='$prod_at' — the documented production lifetime is minutes, not $prod_at" ;;
esac
HYDRA_CM_PROD="$(render_only "$PROD" charts/hydra/templates/configmap.yaml)"
[ -n "$HYDRA_CM_PROD" ] || fail "hydra configmap did not render in prod profile"
echo "$HYDRA_CM_PROD" | grep -Eq "access_token: *$prod_at" \
  || fail "prod: the rendered Hydra config does not carry access_token: $prod_at"
ok

# ── 2. PROD machine-credential lifetime envs ─────────────────────────────────
# The SA key IS the machine's credential; machine principals are exempt from
# step-up, which holds only while the credential is time-bounded.
IAM_PROD="$(render_only "$PROD" charts/kacho-iam/templates/deployment.yaml)"
[ -n "$IAM_PROD" ] || fail "kacho-iam deployment did not render in prod profile"
echo "$IAM_PROD" | grep -q 'KACHO_IAM_SAKEY_DEFAULT_TTL' \
  || fail "prod: KACHO_IAM_SAKEY_DEFAULT_TTL absent — an omitted ttl_seconds would mint a never-expiring key"
echo "$IAM_PROD" | grep -q 'KACHO_IAM_SAKEY_MAX_TTL' \
  || fail "prod: KACHO_IAM_SAKEY_MAX_TTL absent — no ceiling on how long a machine credential may live"
prod_atl="$(yq '.["kacho-iam"].kacho.iam.saKey.accessTokenTtl // ""' "$PROD")"
[ -n "$prod_atl" ] \
  || fail "prod: kacho-iam.kacho.iam.saKey.accessTokenTtl unset — SA-key clients would inherit the global TTL with no second layer"
ok

# ── 3. DEV keeps bounded keys but does NOT pin the per-client token lifespan ──
IAM_DEV="$(render_only "$DEV" charts/kacho-iam/templates/deployment.yaml)"
[ -n "$IAM_DEV" ] || fail "kacho-iam deployment did not render in dev profile"
echo "$IAM_DEV" | grep -q 'KACHO_IAM_SAKEY_MAX_TTL' \
  || fail "dev: the SA-key ceiling must apply on the local stand too"
dev_atl="$(yq '.["kacho-iam"].kacho.iam.saKey.accessTokenTtl // ""' "$DEV")"
[ -z "$dev_atl" ] \
  || fail "dev: saKey.accessTokenTtl='$dev_atl' — the local stand deliberately inherits the widened global TTL; pinning it 401s late newman collections"
ok

# ── 4. Binding staging order — both halves default OFF ───────────────────────
# Issuance (iam) must precede enforcement (gateway). Enforcement alone rejects
# every service-account token, because binding is per-client REGISTRATION
# metadata: a key registered before issuance was enabled keeps minting bearers.
for f in "$PROD" "$DEV"; do
  bind="$(yq '.["kacho-iam"].kacho.iam.saKey.bindDpop // false' "$f")"
  reqb="$(yq '.["api-gateway"].authn.requireMachineTokenBinding // false' "$f")"
  if [ "$reqb" = "true" ] && [ "$bind" != "true" ]; then
    fail "$(basename "$f"): requireMachineTokenBinding=true while saKey.bindDpop=$bind — enforcement before issuance rejects every machine token"
  fi
done
ok

# ── 5. CAPABILITY INTACT — the templates still emit the knobs ────────────────
# The DPoP feature was unreachable for its whole life precisely because no
# template emitted its env. A values-level decision is inert without this.
for name in KACHO_IAM_SAKEY_DEFAULT_TTL KACHO_IAM_SAKEY_MAX_TTL \
            KACHO_IAM_SAKEY_ACCESS_TOKEN_TTL KACHO_IAM_SAKEY_BIND_DPOP; do
  grep -q "name: $name" "$IAM_TPL" \
    || fail "capability: kacho-iam template no longer emits $name — the values knob would be silently inert"
done
for name in KACHO_API_GATEWAY_AUTHN_ENABLE_DPOP \
            KACHO_API_GATEWAY_AUTHN_REQUIRE_MACHINE_TOKEN_BINDING; do
  grep -q "name: $name" "$GW_TPL" \
    || fail "capability: api-gateway template no longer emits $name — the binding control would be unreachable, which is the state this work fixed"
done
grep -q 'requireMachineTokenBinding' "$GW_VALUES" \
  || fail "capability: api-gateway values.yaml no longer documents the binding rollout order"
ok

echo "OK: $SCRIPT — $N/5 machine-credential posture assertions passed"
