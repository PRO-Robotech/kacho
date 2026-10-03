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
# Asserted (the former section 1 — the external issuer's own access-token TTL
# in values.prod.yaml — is gone with the issuer, #1276: its subchart is removed
# from the umbrella, and no profile carries its settings any more):
#   2. PROD iam render    → SA-key lifetime envs present; access-token lifespan
#                           pinned per-client (defence in depth over the global).
#   3. DEV  iam render    → key lifetime still bounded, but the per-client token
#                           lifespan is deliberately NOT pinned (the local e2e
#                           stand widens the global TTL to outlast serial newman
#                           waves; pinning 15m here would 401 late collections).
#   4. NO ENFORCEMENT     → no profile requires machine-token binding at the
#                           gateway. No machine token issued on this platform
#                           carries `cnf` (the token endpoint that exchanges an
#                           SA key takes no proof of possession), so the
#                           requirement can only reject every service-account
#                           token.
#   5. CAPABILITY INTACT  → both templates still emit the knobs they are read by
#                           (a removed env block would make every values-level
#                           decision inert — exactly how the DPoP flag came to
#                           be unreachable).
#   6. RETIRED KNOB LOUD  → `kaname.platform.iam.saKey.bindDpop` is refused: the
#                           identity sub-chart fails the render on its presence,
#                           whatever its value (task #2949), and no profile
#                           declares it. Its one reader in the identity service
#                           is a boot guard that refuses to start with it on
#                           while the token endpoint is enabled; issuance never
#                           read it.
#
# WHY SECTION 6 RENDERS AND DOES NOT ONLY READ THE PROFILES. A profile census
# sees the profiles of this tree; an operator's own profile reaches the chart
# without passing it. The refusal therefore has to be the chart's own, and the
# chart is asked directly. Section 4 prints the census both sections read,
# because «0 findings» must be distinguishable from «0 profiles read».
#
# Offline manifest-assertion harness (no kind cluster). Mirrors tests/helm/*.
set -euo pipefail
SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
# Каталог умбреллы переопределяется ради доказательства инъекцией: копия чарта
# со снятым отказом (machine-credential-posture-inject.sh, ось 7) — проба та же.
UMBRELLA="${MACHINE_POSTURE_UMBRELLA:-$REPO_ROOT/helm/umbrella}"
PROD="$UMBRELLA/values.prod.yaml"
DEV="$UMBRELLA/values.dev.yaml"
IAM_TPL="$UMBRELLA/charts/kaname/templates/deployment.yaml"
GW_TPL="$REPO_ROOT/../gateway/deploy/templates/deployment.yaml"
GW_VALUES="$REPO_ROOT/../gateway/deploy/values.yaml"

# ТРИ ИСХОДА (0 зелено · 1 находка о дереве · 2 условие не создано) — общей
# реализацией на весь каталог. До #1195 отказ helm по причине, НЕ относящейся к
# предмету проверки (зависимости умбреллы не собраны), убивал прогон на первом
# же рендере под `set -e`, НЕ СКАЗАВ НИЧЕГО: код 1,
# ноль байт вывода — при том что этот файл специально написан так, чтобы
# «ноль находок» было отличимо от «ноль прочитанного».
# shellcheck source=deploy/tests/helm/outcome.sh
. "$(dirname "$0")/outcome.sh"
EXPECTED_ASSERTIONS=5

# Перечень профилей ВЫВОДИТСЯ из каталога, а не выписывается: выписанный список
# разошёлся бы с деревом молча, и разошёлся бы в сторону непроверенного профиля.
# Каталог переопределяется ради доказательства инъекцией
# (machine-credential-posture-inject.sh) — сама проба при этом та же.
PROFILE_DIR="${MACHINE_POSTURE_PROFILE_DIR:-$UMBRELLA}"
PROFILES=()
while IFS= read -r _p; do PROFILES+=("$_p"); done < <(ls -1 "$PROFILE_DIR"/values*.yaml 2>/dev/null)
[ "${#PROFILES[@]}" -gt 0 ] \
  || fatal "no values*.yaml under $PROFILE_DIR — a census over zero profiles is not a green verdict (условие не создано, не находка о дереве)"

# enforce_of / declares_retired_of — ОДИН предикат на секцию. Копия условия
# разошлась бы там, где расхождение не видно.
enforce_of()         { yq '.["api-gateway"].authn.requireMachineTokenBinding // false' "$1"; }
declares_retired_of() { yq '(.["kaname"].platform.iam.saKey // {}) | has("bindDpop")' "$1"; }

# yq MUST be mikefarah v4. On many machines /usr/bin/yq is the python jq-wrapper
# of the SAME NAME, whose filter syntax and quoting differ — assertions here read
# DECISIONS out of the values files, so an impostor yq would either error or
# return differently-quoted output and the gate would verify nothing. That
# false-green is precisely the class this gate exists to prevent, so detect the
# impostor explicitly rather than trusting `command -v`.
require_helm
require_mikefarah_yq
[ -f "$PROD" ]    || fatal "values.prod.yaml нет на диске ($PROD)"
[ -f "$DEV" ]     || fatal "values.dev.yaml нет на диске ($DEV)"
[ -f "$IAM_TPL" ] || fatal "шаблона kaname нет на диске ($IAM_TPL)"
[ -f "$GW_TPL" ]  || fatal "шаблона api-gateway нет на диске ($GW_TPL)"

# render_only <values> <show-only-template> — результат в $HELM_OUT.
# Отказ рендера — код 2 плюс ТЕКСТ helm, а не молчаливая смерть под `set -e`.
render_only() {
  helm_try kacho-umbrella "$UMBRELLA" -f "$1" --show-only "$2"
  render_or_fatal "$(basename "$1") → $2"
}

render_only "$PROD" charts/kaname/templates/deployment.yaml; IAM_PROD="$HELM_OUT"
[ -n "$IAM_PROD" ] || fail "kaname deployment did not render in prod profile"
[[ "$IAM_PROD" == *'KANAME_SAKEY_DEFAULT_TTL'* ]] \
  || fail "prod: KANAME_SAKEY_DEFAULT_TTL absent — an omitted ttl_seconds would mint a never-expiring key"
[[ "$IAM_PROD" == *'KANAME_SAKEY_MAX_TTL'* ]] \
  || fail "prod: KANAME_SAKEY_MAX_TTL absent — no ceiling on how long a machine credential may live"
prod_atl="$(yq '.["kaname"].platform.iam.saKey.accessTokenTtl // ""' "$PROD")"
[ -n "$prod_atl" ] \
  || fail "prod: kaname.platform.iam.saKey.accessTokenTtl unset — SA-key clients would inherit the global TTL with no second layer"
ok

# ── 3. DEV keeps bounded keys but does NOT pin the per-client token lifespan ──
render_only "$DEV" charts/kaname/templates/deployment.yaml; IAM_DEV="$HELM_OUT"
[ -n "$IAM_DEV" ] || fail "kaname deployment did not render in dev profile"
[[ "$IAM_DEV" == *'KANAME_SAKEY_MAX_TTL'* ]] \
  || fail "dev: the SA-key ceiling must apply on the local stand too"
dev_atl="$(yq '.["kaname"].platform.iam.saKey.accessTokenTtl // ""' "$DEV")"
[ -z "$dev_atl" ] \
  || fail "dev: saKey.accessTokenTtl='$dev_atl' — the local stand deliberately inherits the widened global TTL; pinning it 401s late newman collections"
ok

# ── 4. No profile requires machine-token binding ─────────────────────────────
# The gateway rejects, 401, a service-account token without `cnf` once
# requireMachineTokenBinding is on. No issuance on this platform binds a
# machine token: the token endpoint where an SA key is exchanged takes no proof
# of possession. Enforcement therefore rejects every service-account token, and
# there is no issuance-side switch to turn on first.
#
# Read over EVERY profile, not just prod/dev: a deployable production profile
# sits among the ones a prod/dev pair would skip.
n_enforce=0
n_retired=0
for f in "${PROFILES[@]}"; do
  [ "$(declares_retired_of "$f")" = "true" ] && n_retired=$((n_retired + 1))
  if [ "$(enforce_of "$f")" = "true" ]; then
    n_enforce=$((n_enforce + 1))
    fail "$(basename "$f"): requireMachineTokenBinding=true — no machine token issued on this platform carries cnf, so the gateway would reject every service-account token"
  fi
done
echo "  census: profiles read ${#PROFILES[@]} · binding required $n_enforce · declaring retired saKey.bindDpop $n_retired"
ok

# ── 5. CAPABILITY INTACT — the templates still emit the knobs ────────────────
# The DPoP feature was unreachable for its whole life precisely because no
# template emitted its env. A values-level decision is inert without this.
for name in KANAME_SAKEY_DEFAULT_TTL KANAME_SAKEY_MAX_TTL \
            KANAME_SAKEY_ACCESS_TOKEN_TTL; do
  grep -q "name: $name" "$IAM_TPL" \
    || fail "capability: kaname template no longer emits $name — the values knob would be silently inert"
done
for name in KACHO_API_GATEWAY_AUTHN_ENABLE_DPOP \
            KACHO_API_GATEWAY_AUTHN_REQUIRE_MACHINE_TOKEN_BINDING; do
  grep -q "name: $name" "$GW_TPL" \
    || fail "capability: api-gateway template no longer emits $name — the binding control would be unreachable, which is the state this work fixed"
done
grep -q 'requireMachineTokenBinding' "$GW_VALUES" \
  || fail "capability: api-gateway values.yaml no longer documents the binding knob"
ok

# ── 6. The retired issuance-side knob is refused, not ignored (#2949) ────────
# `kaname.platform.iam.saKey.bindDpop` changed no issuance on any contour: the
# token endpoint that exchanges an SA key takes no proof of possession, and
# without that endpoint no SA key is issued at all. Its one reader in the
# identity service is a boot guard that refuses to start with it on while the
# endpoint is enabled. The sub-chart therefore no longer emits it and refuses a
# profile that declares it (`kaname.refuseRetiredKnobs`) — judged on PRESENCE:
# `false` is a declaration too.
#
# Positive control: the prod render of section 2 succeeded with the same chain
# minus the key, so a refusal here is the key's, not the chain's.
for f in "${PROFILES[@]}"; do
  if [ "$(declares_retired_of "$f")" = "true" ]; then
    fail "$(basename "$f"): declares kaname.platform.iam.saKey.bindDpop — the knob is retired; the identity sub-chart refuses this profile at render"
  fi
done
RETIRED_OVERLAY="$(mktemp)"
trap 'rm -f "$RETIRED_OVERLAY"' EXIT
printf 'kaname:\n  platform:\n    iam:\n      saKey:\n        bindDpop: false\n' > "$RETIRED_OVERLAY"
helm_try kacho-umbrella "$UMBRELLA" -f "$PROD" -f "$RETIRED_OVERLAY" --show-only charts/kaname/templates/deployment.yaml
render_must_fail_because "platform.iam.saKey.bindDpop" \
  "values.prod.yaml + saKey.bindDpop → charts/kaname/templates/deployment.yaml" \
  "prod + saKey.bindDpop rendered — the identity sub-chart accepts a retired knob it no longer reads"
ok

outcome_verdict "профилей прочитано: ${#PROFILES[@]}"
