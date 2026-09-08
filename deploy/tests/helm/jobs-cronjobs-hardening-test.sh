#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# INFRA sec-hardening r3 manifest-assertion guard (offline; no kind cluster).
#
# Closes the 3rd-audit residual: EVERY umbrella-owned Job/CronJob must carry the
# same restricted PSS floor the long-running Deployments enforce, and the public
# api-gateway ingress must route the EXTERNAL edge through the external-marked
# TLS listener (port `tls`, backend-protocol GRPCS) so listenerorigin.ExternalListener
# tags the traffic and the REST dispatcher 404s Internal* paths on the public edge.
#
#   1. (снято — задания начальной настройки движка прав удалены вместе с ним, S6 #747)
#   2. (снято — jwks-rotator CronJob удалён как вестигиальный)
#   3. kacho-geo data-migration Job   — pod + container restricted floor.
#   4. api-gateway external ingress   — backend port `tls` + backend-protocol GRPCS
#                                       (NOT the internal-origin `cmux`/GRPC path).
#
# Mirrors tests/helm/sec-hardening-test.sh: renders via `helm template` and
# asserts with yq. Contracts unchanged (helm/CI/docs only).
set -euo pipefail

SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
DEV="$UMBRELLA/values.dev.yaml"

# ТРИ ИСХОДА (0 зелено · 1 находка о дереве · 2 условие не создано) — общей
# реализацией на весь каталог. До #1195 отказ helm по причине, НЕ относящейся к
# предмету проверки (зависимости умбреллы не собраны), убивал прогон на первом
# же `GEODM=$(render …)` под `set -e`, НЕ СКАЗАВ НИЧЕГО: код 1, ноль байт.
# shellcheck source=deploy/tests/helm/outcome.sh
. "$(dirname "$0")/outcome.sh"
EXPECTED_ASSERTIONS=3

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

# assert_pod_sc <rendered-doc> <podspec-jsonpath> <where>
# Verifies the restricted pod-level securityContext floor.
assert_pod_sc() {
  local doc="$1" podpath="$2" where="$3" sc
  sc=$(echo "$doc" | yq eval-all "select(.kind == \"Job\" or .kind == \"CronJob\") | ${podpath}.securityContext" - 2>/dev/null)
  [ -n "$sc" ] && [ "$sc" != "null" ] || fail "$where: pod has no securityContext"
  [ "$(echo "$sc" | yq '.runAsNonRoot')" = "true" ] || fail "$where: pod runAsNonRoot != true"
  [ "$(echo "$sc" | yq '.seccompProfile.type')" = "RuntimeDefault" ] || fail "$where: pod seccompProfile.type != RuntimeDefault"
  ok
}

# assert_ctr_sc <rendered-doc> <podspec-jsonpath> <container-name> <where>
# Verifies the restricted container-level securityContext floor on a named container.
assert_ctr_sc() {
  local doc="$1" podpath="$2" cname="$3" where="$4" sc
  sc=$(echo "$doc" | yq eval-all \
    "select(.kind == \"Job\" or .kind == \"CronJob\") | ${podpath}.containers[] | select(.name == \"$cname\") | .securityContext" - 2>/dev/null)
  [ -n "$sc" ] && [ "$sc" != "null" ] || fail "$where: container '$cname' has no securityContext"
  [ "$(echo "$sc" | yq '.runAsNonRoot')" = "true" ] || fail "$where/$cname: runAsNonRoot != true"
  [ "$(echo "$sc" | yq '.readOnlyRootFilesystem')" = "true" ] || fail "$where/$cname: readOnlyRootFilesystem != true"
  [ "$(echo "$sc" | yq '.allowPrivilegeEscalation')" = "false" ] || fail "$where/$cname: allowPrivilegeEscalation != false"
  [ "$(echo "$sc" | yq '.capabilities.drop[0]')" = "ALL" ] || fail "$where/$cname: capabilities.drop != [ALL]"
  [ "$(echo "$sc" | yq '.seccompProfile.type')" = "RuntimeDefault" ] || fail "$where/$cname: seccompProfile.type != RuntimeDefault"
  ok
}

POD=".spec.template.spec"

# ── 1. (снято) задания начальной настройки движка прав ───────────────────────
# Подчарт движка удалён вместе с самим движком (S6 эпика #747): решение о доступе
# вычисляет реляционная форма в базе iam. Проверять PSS-floor больше не на чем —
# секция снята вместе с шаблонами, а не ослаблена.

# ── 2. (снято) kaname jwks-rotator CronJob ────────────────────────────────
# CronJob удалён как вестигиальный: iam не владеет ключом подписи (издатель и
# подписант — Hydra; iam лишь проксирует её публичный JWKS), поэтому ротировать
# нечего. Проверять PSS-floor больше не на чем — секция снята вместе с шаблоном.

# ── 3. kacho-geo data-migration Job ──────────────────────────────────────────
render charts/kacho-geo/templates/geo-data-migration-job.yaml \
  --set kacho-geo.dataMigration.enabled=true
GEODM="$HELM_OUT"
assert_pod_sc "$GEODM" "$POD" "geo-data-migration-job"
assert_ctr_sc "$GEODM" "$POD" "copy" "geo-data-migration-job"

# ── 4. api-gateway external ingress → external-marked TLS listener ────────────
# Render the FULL umbrella and select the effective api-gateway Ingress, so the
# assertion is agnostic to which template produces it (sub-chart vs umbrella).
helm_try kacho-umbrella "$UMBRELLA" -f "$DEV"
render_or_fatal "values.dev.yaml → умбрелла целиком"
ALL="$HELM_OUT"
ING=$(echo "$ALL" | yq eval-all \
  'select(.kind == "Ingress" and .metadata.name == "api-gateway")' - 2>/dev/null)
[ -n "$ING" ] || fail "api-gateway ingress: no Ingress named 'api-gateway' rendered"
PORT=$(echo "$ING" | yq '.spec.rules[0].http.paths[0].backend.service.port.name')
[ "$PORT" = "tls" ] || fail "api-gateway ingress: backend port is '$PORT', expected 'tls' (external-marked listener; 'cmux' serves Internal* REST externally)"
PROTO=$(echo "$ING" | yq '.metadata.annotations."nginx.ingress.kubernetes.io/backend-protocol"')
[ "$PROTO" = "GRPCS" ] || fail "api-gateway ingress: backend-protocol is '$PROTO', expected 'GRPCS' (TLS re-encrypt to the pod TLS listener)"
# Exactly one Ingress named api-gateway (no double-ingress from sub-chart + umbrella).
COUNT=$(echo "$ALL" | yq eval-all 'select(.kind == "Ingress" and .metadata.name == "api-gateway") | .metadata.name' - 2>/dev/null | grep -c "api-gateway" || true)
[ "$COUNT" = "1" ] || fail "api-gateway ingress: expected exactly 1, found $COUNT (sub-chart ingress must be disabled)"
ok

outcome_verdict "профилей прочитано: 1 (dev)"
