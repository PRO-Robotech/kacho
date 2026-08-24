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
#   6. READER EXISTS      → no profile turns issuance-side binding on while the
#                           SA-key contour is TRANSLATED to our own token
#                           endpoint. There the provider mirror is not created
#                           at all, so `dpop_bound_access_tokens` has no reader:
#                           the knob would be declared and enforced by nothing
#                           (task #1137). Mirrors the iam boot guard
#                           `Config.validateMachineTokenBinding`.
#
# WHY SECTION 6 EXISTS AND WHY SECTION 4 WAS NOT ENOUGH. Section 4 judges the
# pair «enforcement ⇒ issuance», and while BOTH halves are off it passes without
# examining anything: two disabled sides prove nothing about a control. Section 6
# judges a pair that IS satisfiable today — three profiles in this tree translate
# the contour — so it has real inputs and can actually refuse. Both sections
# print what they read, because «0 findings» must be distinguishable from
# «0 profiles read».
#
# Offline manifest-assertion harness (no kind cluster). Mirrors tests/helm/*.
set -euo pipefail
# any_line_matches <многострочное значение> <ERE> — как `grep -qE`: истинно, если
# ХОТЬ ОДНА строка значения совпадает с выражением. Построчность важна: у `grep`
# точка не переходит через перевод строки, а у `[[ =~ ]]` на всём значении —
# переходит. Труба убрана из-за ложного отказа на совпадении (задача #658).
any_line_matches() {
  local _l
  while IFS= read -r _l; do
    if [[ "$_l" =~ $2 ]]; then return 0; fi
  done <<<"$1"
  return 1
}

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

# Перечень профилей ВЫВОДИТСЯ из каталога, а не выписывается: выписанный список
# разошёлся бы с деревом молча, и разошёлся бы в сторону непроверенного профиля.
# Каталог переопределяется ради доказательства инъекцией
# (machine-credential-posture-inject.sh) — сама проба при этом та же.
PROFILE_DIR="${MACHINE_POSTURE_PROFILE_DIR:-$UMBRELLA}"
PROFILES=()
while IFS= read -r _p; do PROFILES+=("$_p"); done < <(ls -1 "$PROFILE_DIR"/values*.yaml 2>/dev/null)
[ "${#PROFILES[@]}" -gt 0 ] \
  || fail "no values*.yaml under $PROFILE_DIR — a census over zero profiles is not a green verdict"

# bind_of / translated_of — ОДИН предикат на обе секции. Две копии одного
# условия разошлись бы там, где расхождение не видно.
bind_of()       { yq '.["kacho-iam"].kacho.iam.saKey.bindDpop // false' "$1"; }
translated_of() { yq '.["kacho-iam"].config.authn.clientToken.enabled // false' "$1"; }
enforce_of()    { yq '.["api-gateway"].authn.requireMachineTokenBinding // false' "$1"; }

# yq MUST be mikefarah v4. On many machines /usr/bin/yq is the python jq-wrapper
# of the SAME NAME, whose filter syntax and quoting differ — assertions here read
# DECISIONS out of the values files, so an impostor yq would either error or
# return differently-quoted output and the gate would verify nothing. That
# false-green is precisely the class this gate exists to prevent, so detect the
# impostor explicitly rather than trusting `command -v`.
command -v yq >/dev/null 2>&1 || fail "yq not installed (mikefarah yq v4 required)"
# Сравнение — БЕЗ трубы: `… | grep -q` под `set -o pipefail` возвращает ОТКАЗ
# НА СОВПАДЕНИИ (grep выходит по первому попаданию, писатель получает SIGPIPE,
# и `pipefail` поднимает ЕГО статус до статуса конвейера). Задача #658.
YQ_VER="$(yq --version 2>&1 || true)"
[[ "${YQ_VER,,}" == *mikefarah* ]] || fail \
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
any_line_matches "$HYDRA_CM_PROD" "access_token: *$prod_at" \
  || fail "prod: the rendered Hydra config does not carry access_token: $prod_at"
ok

# ── 2. PROD machine-credential lifetime envs ─────────────────────────────────
# The SA key IS the machine's credential; machine principals are exempt from
# step-up, which holds only while the credential is time-bounded.
IAM_PROD="$(render_only "$PROD" charts/kacho-iam/templates/deployment.yaml)"
[ -n "$IAM_PROD" ] || fail "kacho-iam deployment did not render in prod profile"
[[ "$IAM_PROD" == *'KACHO_IAM_SAKEY_DEFAULT_TTL'* ]] \
  || fail "prod: KACHO_IAM_SAKEY_DEFAULT_TTL absent — an omitted ttl_seconds would mint a never-expiring key"
[[ "$IAM_PROD" == *'KACHO_IAM_SAKEY_MAX_TTL'* ]] \
  || fail "prod: KACHO_IAM_SAKEY_MAX_TTL absent — no ceiling on how long a machine credential may live"
prod_atl="$(yq '.["kacho-iam"].kacho.iam.saKey.accessTokenTtl // ""' "$PROD")"
[ -n "$prod_atl" ] \
  || fail "prod: kacho-iam.kacho.iam.saKey.accessTokenTtl unset — SA-key clients would inherit the global TTL with no second layer"
ok

# ── 3. DEV keeps bounded keys but does NOT pin the per-client token lifespan ──
IAM_DEV="$(render_only "$DEV" charts/kacho-iam/templates/deployment.yaml)"
[ -n "$IAM_DEV" ] || fail "kacho-iam deployment did not render in dev profile"
[[ "$IAM_DEV" == *'KACHO_IAM_SAKEY_MAX_TTL'* ]] \
  || fail "dev: the SA-key ceiling must apply on the local stand too"
dev_atl="$(yq '.["kacho-iam"].kacho.iam.saKey.accessTokenTtl // ""' "$DEV")"
[ -z "$dev_atl" ] \
  || fail "dev: saKey.accessTokenTtl='$dev_atl' — the local stand deliberately inherits the widened global TTL; pinning it 401s late newman collections"
ok

# ── 4. Binding staging order — issuance precedes enforcement ─────────────────
# Issuance (iam) must precede enforcement (gateway). Enforcement alone rejects
# every service-account token, because binding is per-client REGISTRATION
# metadata: a key registered before issuance was enabled keeps minting bearers.
#
# Read over EVERY profile, not just prod/dev: the two profiles this section used
# to read are 2 of the 10 in the tree, and the eight it skipped included a
# deployable production profile.
n_bind=0
n_translated=0
for f in "${PROFILES[@]}"; do
  bind="$(bind_of "$f")"
  reqb="$(enforce_of "$f")"
  [ "$bind" = "true" ] && n_bind=$((n_bind + 1))
  [ "$(translated_of "$f")" = "true" ] && n_translated=$((n_translated + 1))
  if [ "$reqb" = "true" ] && [ "$bind" != "true" ]; then
    fail "$(basename "$f"): requireMachineTokenBinding=true while saKey.bindDpop=$bind — enforcement before issuance rejects every machine token"
  fi
done
echo "  census: profiles read ${#PROFILES[@]} · issuance-side binding on $n_bind · contour translated $n_translated"
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

# ── 6. The issuance-side knob must have a READER (#1137) ─────────────────────
# `saKey.bindDpop` is registration metadata for the client MIRROR at the previous
# issuer. A translated contour (`authn.clientToken.enabled`) creates no mirror at
# all, so on it the knob is read by nothing: the operator declares the control
# and gets a stand where machine tokens are NOT bound, believing the opposite.
#
# The same contradiction refuses the iam start (`Config.validateMachineTokenBinding`).
# Asserted here as well because a values-level decision is made before any pod
# starts, and «the chart renders» is where an operator looks first.
for f in "${PROFILES[@]}"; do
  if [ "$(bind_of "$f")" = "true" ] && [ "$(translated_of "$f")" = "true" ]; then
    fail "$(basename "$f"): saKey.bindDpop=true while authn.clientToken.enabled=true — a translated contour registers no mirror, so the sender-constrained requirement has no reader (task #1137); iam refuses to start in this state"
  fi
done
ok

echo "OK: $SCRIPT — $N/6 machine-credential posture assertions passed"
