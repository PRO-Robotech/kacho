#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# INFRA sec-hardening manifest-assertion guard (offline; no kind cluster).
#
# Asserts the container/pod hardening re-applied on the current chart structure:
#   1. kaname + kacho-geo workloads carry a hardened pod- AND per-container
#      securityContext (runAsNonRoot, readOnlyRootFilesystem, drop ALL caps,
#      allowPrivilegeEscalation=false, seccompProfile RuntimeDefault) on EVERY
#      container incl. init-containers — not only the OPA sidecar.
#   2. The umbrella ships a Namespace template carrying Pod Security Admission
#      warn+audit=restricted labels for the kacho namespace.
#   3. Image references support a digest-pin override (repository@sha256:...).
#
# Mirrors tests/helm/*-test.sh: renders via `helm template ... --show-only` and
# asserts with yq. Contracts unchanged (helm/CI/docs only).
set -euo pipefail

SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
DEV="$UMBRELLA/values.dev.yaml"

# ТРИ ИСХОДА (0 зелено · 1 находка о дереве · 2 условие не создано) — общей
# реализацией на весь каталог. До #1195 отказ helm по причине, НЕ относящейся к
# предмету проверки (зависимости умбреллы не собраны), убивал прогон на первом
# же `IAM=$(render …)` под `set -e`, НЕ СКАЗАВ НИЧЕГО: код 1, ноль байт вывода.
# shellcheck source=deploy/tests/helm/outcome.sh
. "$(dirname "$0")/outcome.sh"
EXPECTED_ASSERTIONS=8

require_helm
require_mikefarah_yq

[ -f "$DEV" ] || fatal "values.dev.yaml нет на диске ($DEV)"

# render <show-only-template> [extra helm args...] — результат в $HELM_OUT.
# Отказ рендера — код 2 плюс ТЕКСТ helm, а не молчаливая смерть под `set -e`.
render() {
  local tmpl="$1"; shift
  helm_try kacho-umbrella "$UMBRELLA" -f "$DEV" "$@" --show-only "$tmpl"
  render_or_fatal "values.dev.yaml → $tmpl${*:+ [$*]}"
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

# ── 1. kaname workload hardening ──────────────────────────────────────────
render charts/kaname/templates/deployment.yaml; IAM="$HELM_OUT"
POD_SC=$(echo "$IAM" | yq 'select(.kind == "Deployment") | .spec.template.spec.securityContext')
[ "$(echo "$POD_SC" | yq '.runAsNonRoot')" = "true" ] || fail "kaname: pod securityContext.runAsNonRoot != true"
[ "$(echo "$POD_SC" | yq '.seccompProfile.type')" = "RuntimeDefault" ] || fail "kaname: pod seccompProfile != RuntimeDefault"
ok
assert_sc "$IAM" "kaname" "kaname"
assert_sc "$IAM" "migrate" "kaname"

# ── 2. kacho-geo workload hardening ──────────────────────────────────────────
render charts/kacho-geo/templates/deployment.yaml; GEO="$HELM_OUT"
GPOD_SC=$(echo "$GEO" | yq 'select(.kind == "Deployment") | .spec.template.spec.securityContext')
[ "$(echo "$GPOD_SC" | yq '.runAsNonRoot')" = "true" ] || fail "kacho-geo: pod securityContext.runAsNonRoot != true"
ok
assert_sc "$GEO" "kacho-geo" "kacho-geo"
assert_sc "$GEO" "migrate" "kacho-geo"

# ── 3. Pod Security Admission namespace labels (warn+audit=restricted) ────────
render templates/namespace.yaml --set namespace.create=true; NS="$HELM_OUT"
[ "$(echo "$NS" | yq 'select(.kind == "Namespace") | .metadata.labels."pod-security.kubernetes.io/warn"')" = "restricted" ] \
  || fail "namespace: pod-security warn label != restricted"
[ "$(echo "$NS" | yq 'select(.kind == "Namespace") | .metadata.labels."pod-security.kubernetes.io/audit"')" = "restricted" ] \
  || fail "namespace: pod-security audit label != restricted"
ok

# ── 4. Image digest-pin override (repository@sha256:...) ──────────────────────
DIG="sha256:0000000000000000000000000000000000000000000000000000000000000000"
render charts/kaname/templates/deployment.yaml --set kaname.image.digest="$DIG"; IAM_DIG="$HELM_OUT"
IAM_DIG_IMAGE="$(echo "$IAM_DIG" | yq 'select(.kind == "Deployment") | .spec.template.spec.containers[0].image')"
[[ "$IAM_DIG_IMAGE" == *"@$DIG"* ]] \
  || fail "kaname: image.digest override not honoured (expected repository@$DIG)"
render charts/kacho-geo/templates/deployment.yaml --set kacho-geo.imageDigest="$DIG"; GEO_DIG="$HELM_OUT"
GEO_DIG_IMAGE="$(echo "$GEO_DIG" | yq 'select(.kind == "Deployment") | .spec.template.spec.containers[0].image')"
[[ "$GEO_DIG_IMAGE" == *"@$DIG"* ]] \
  || fail "kacho-geo: imageDigest override not honoured (expected repository@$DIG)"
ok

outcome_verdict "профилей прочитано: 1 (dev)"
