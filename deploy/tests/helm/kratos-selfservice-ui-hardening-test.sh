#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# INFRA sec-hardening r2 manifest-assertion guard (offline; no kind cluster).
#
# Covers the second-audit INFRA findings on the umbrella-owned auxiliary
# workloads that the first sec-hardening pass (sec-hardening-test.sh) did NOT
# reach — the Ory kratos-selfservice-ui sub-chart.
#
# Здесь же проверялся этаж PSS у задания доверия издателям (federation-in).
# Задание снято вместе со своим предметом: перечень доверенных издателей стал
# нашей таблицей (#1124), и запись у поставщика на решение о доступе не влияет.
# Утверждение о нём снято ВМЕСТЕ с ним — проба, чей предмет исчез, либо
# утверждает несуществующее, либо не может упасть.
#
#   1. kratos-selfservice-ui COOKIE_SECRET / CSRF_COOKIE_SECRET no longer ship a
#      git-committed default. The chart FAILS render when the cookie secret is
#      unset (enabled but neither .existingSecret nor .value given), wires a
#      secretKeyRef under the prod profile, and honours a dev inline .value.
#   2. NODE_TLS_REJECT_UNAUTHORIZED=0 is gated behind insecureSkipTLSVerify — it
#      is ABSENT under the prod profile (cert verification stays ON) and present
#      only on the dev stand.
#   3. kratos-selfservice-ui carries the
#      restricted securityContext floor (runAsNonRoot, runAsUser!=0,
#      readOnlyRootFilesystem, drop ALL caps, allowPrivilegeEscalation=false,
#      seccompProfile RuntimeDefault) on every container.
set -euo pipefail

SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
DEV="$UMBRELLA/values.dev.yaml"
PROD="$UMBRELLA/values.prod.yaml"
N=0
fail() { echo "FAIL: $1"; exit 1; }
ok() { N=$((N + 1)); }

# yq MUST be mikefarah v4. On many machines /usr/bin/yq is the python jq-wrapper
# of the SAME NAME: its filter syntax is incompatible, so it errors to stderr and
# prints NOTHING on stdout. Assertions shaped like `[ -z "$(yq ... 2>/dev/null)" ]`
# then pass VACUOUSLY — the check reports success having verified nothing at all.
# That false-green is the exact class these hardening tests exist to prevent, so
# detect the impostor explicitly instead of trusting `command -v`.
command -v yq >/dev/null 2>&1 || fail "yq not installed (mikefarah yq v4 required)"
# Сравнение — БЕЗ трубы: `… | grep -q` под `set -o pipefail` возвращает ОТКАЗ
# НА СОВПАДЕНИИ (grep выходит по первому попаданию, писатель получает SIGPIPE,
# и `pipefail` поднимает ЕГО статус до статуса конвейера). Задача #658.
YQ_VER="$(yq --version 2>&1 || true)"
[[ "${YQ_VER,,}" == *mikefarah* ]] || fail \
  "wrong 'yq' on PATH ($(command -v yq)): '$(yq --version 2>&1 | head -1)'. \
mikefarah yq v4 is required — the python-yq jq wrapper emits empty output on these \
filters, which would make the assertions below pass without checking anything."

UI_TMPL="charts/kratos-selfservice-ui/templates/deployment.yaml"

render() {
  local values="$1" tmpl="$2"; shift 2
  helm template kacho-umbrella "$UMBRELLA" -f "$values" "$@" \
    --show-only "$tmpl" 2>/dev/null
}

# env_val <doc> <container> <env-name> — prints .value of the named env var.
env_val() {
  echo "$1" | yq eval-all \
    "select(.kind==\"Deployment\") | .spec.template.spec.containers[] | select(.name==\"$2\") | .env[] | select(.name==\"$3\") | .value" - 2>/dev/null
}
# env_secret_ref <doc> <container> <env-name> — prints valueFrom.secretKeyRef.name.
env_secret_ref() {
  echo "$1" | yq eval-all \
    "select(.kind==\"Deployment\") | .spec.template.spec.containers[] | select(.name==\"$2\") | .env[] | select(.name==\"$3\") | .valueFrom.secretKeyRef.name" - 2>/dev/null
}

# assert_sc <doc> <container-name> <where> — restricted floor on a container.
assert_sc() {
  local doc="$1" cname="$2" where="$3" sc
  sc=$(echo "$doc" | yq eval-all \
    "select(.kind==\"Deployment\" or .kind==\"Job\") | (.spec.template.spec.containers[], .spec.template.spec.initContainers[]) | select(.name==\"$cname\") | .securityContext" - 2>/dev/null)
  [ -n "$sc" ] && [ "$sc" != "null" ] || fail "$where: container '$cname' has no securityContext"
  [ "$(echo "$sc" | yq '.runAsNonRoot')" = "true" ] || fail "$where/$cname: runAsNonRoot != true"
  [ "$(echo "$sc" | yq '.runAsUser')" != "0" ] || fail "$where/$cname: runAsUser == 0"
  [ "$(echo "$sc" | yq '.runAsUser')" != "null" ] || fail "$where/$cname: runAsUser unset"
  [ "$(echo "$sc" | yq '.readOnlyRootFilesystem')" = "true" ] || fail "$where/$cname: readOnlyRootFilesystem != true"
  [ "$(echo "$sc" | yq '.allowPrivilegeEscalation')" = "false" ] || fail "$where/$cname: allowPrivilegeEscalation != false"
  [ "$(echo "$sc" | yq '.capabilities.drop[0]')" = "ALL" ] || fail "$where/$cname: capabilities.drop != [ALL]"
  [ "$(echo "$sc" | yq '.seccompProfile.type')" = "RuntimeDefault" ] || fail "$where/$cname: seccompProfile.type != RuntimeDefault"
  ok
}

# ── 1a. cookieSecret unset (enabled, no source) → render MUST fail ────────────
# Layer an explicitly-empty cookieSecret over the otherwise-complete dev profile
# so only the missing cookie secret can trip the render (everything else renders).
EMPTY_OVERLAY="$(mktemp)"
trap 'rm -f "$EMPTY_OVERLAY"' EXIT
cat >"$EMPTY_OVERLAY" <<'EOF'
kratos-selfservice-ui:
  kratosSelfServiceUI:
    enabled: true
    cookieSecret:
      existingSecret: ""
      existingSecretKey: cookieSecret
      value: ""
EOF
if helm template kacho-umbrella "$UMBRELLA" -f "$DEV" -f "$EMPTY_OVERLAY" \
     --show-only "$UI_TMPL" >/dev/null 2>&1; then
  fail "kratos-ui: render succeeded with cookieSecret unset — expected fail-closed"
fi
ok

# ── 1b. prod profile wires COOKIE_SECRET + CSRF_COOKIE_SECRET via secretKeyRef ─
UI_PROD=$(render "$PROD" "$UI_TMPL")
[ -n "$UI_PROD" ] || fail "kratos-ui: prod profile rendered empty (sub-chart disabled?)"
CS_REF=$(env_secret_ref "$UI_PROD" ui COOKIE_SECRET)
CSRF_REF=$(env_secret_ref "$UI_PROD" ui CSRF_COOKIE_SECRET)
[ -n "$CS_REF" ] && [ "$CS_REF" != "null" ] || fail "kratos-ui/prod: COOKIE_SECRET not via secretKeyRef"
[ -n "$CSRF_REF" ] && [ "$CSRF_REF" != "null" ] || fail "kratos-ui/prod: CSRF_COOKIE_SECRET not via secretKeyRef"
# No committed literal cookie secret must survive in the prod render.
[[ "$UI_PROD" == *"please-change-this-32-bytes"* ]] \
  && fail "kratos-ui/prod: committed default cookie secret still present"
ok

# ── 1c. dev inline .value renders as a literal COOKIE_SECRET/CSRF value ───────
UI_DEV=$(render "$DEV" "$UI_TMPL")
[ -n "$(env_val "$UI_DEV" ui COOKIE_SECRET)" ] || fail "kratos-ui/dev: COOKIE_SECRET inline value missing"
[ -n "$(env_val "$UI_DEV" ui CSRF_COOKIE_SECRET)" ] || fail "kratos-ui/dev: CSRF_COOKIE_SECRET inline value missing"
ok

# ── 2. NODE_TLS_REJECT_UNAUTHORIZED gate ─────────────────────────────────────
# prod: verification stays ON → the disable-env must be ABSENT.
[ -z "$(env_val "$UI_PROD" ui NODE_TLS_REJECT_UNAUTHORIZED)" ] \
  || fail "kratos-ui/prod: NODE_TLS_REJECT_UNAUTHORIZED present (cert verification disabled in prod)"
# dev: opt-in flag set → the disable-env is present with value 0.
[ "$(env_val "$UI_DEV" ui NODE_TLS_REJECT_UNAUTHORIZED)" = "0" ] \
  || fail "kratos-ui/dev: NODE_TLS_REJECT_UNAUTHORIZED != 0 (dev opt-in expected)"
ok

# ── 3. restricted securityContext floor on the three workloads ───────────────
assert_sc "$UI_DEV" ui "kratos-ui"
# pod-level floor on kratos-ui
UI_POD_SC=$(echo "$UI_DEV" | yq 'select(.kind=="Deployment") | .spec.template.spec.securityContext')
[ "$(echo "$UI_POD_SC" | yq '.runAsNonRoot')" = "true" ] || fail "kratos-ui: pod runAsNonRoot != true"
[ "$(echo "$UI_POD_SC" | yq '.seccompProfile.type')" = "RuntimeDefault" ] || fail "kratos-ui: pod seccomp != RuntimeDefault"
ok



echo "$SCRIPT: all green ($N assertions)"
