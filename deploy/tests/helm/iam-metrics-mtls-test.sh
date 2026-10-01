#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# kaname Prometheus /metrics listener (:9095) — per-edge server-side TLS, reusing
# the SEC-F internal-CA server cert (kacho-iam#137).
#
# ПРЕЖДЕ ГЕЙТ СУДИЛ ДВА СЛУШАТЕЛЯ — обратных вызовов прежнего поставщика личности
# (:9092) и скрейпа. Слушателя обратных вызовов у пиненной службы нет (kaname#363),
# его ручки сняты с подчарта (kacho#2818), и половины о нём — ручка режима, адреса
# и якорь доверия пода издателя — сняты вместе с предметом. Осталось ребро скрейпа.
#
# DETERMINISM NOTE: `helm template` on this large multi-subchart umbrella renders
# the kaname subchart Deployment NON-deterministically (values coalescing). So the
# prod-ON DECISION is asserted from values.prod.yaml directly (yq — deterministic),
# and CAPABILITY from the STANDALONE sub-chart render (deterministic): the gate by an
# on/off pair, the per-edge env names + their derivation from the template source.
#
# This guard asserts:
#   - PROD values DECISION → kaname.mtls.httpListeners=true with
#     metricsClientAuthMode=server-tls-only (gate ON, server-tls-only);
#   - DEV → httpListeners off (unset/false);
#   - CAPABILITY INTACT → the kaname deployment TEMPLATE emits every metrics-edge
#     env incl. CLIENTAUTHMODE, reusing the mounted SEC-F server cert (no new PKI).
#
# Offline manifest-assertion harness (no kind cluster). Mirrors tests/helm/*.
set -euo pipefail
SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
PROD="$UMBRELLA/values.prod.yaml"
DEV="$UMBRELLA/values.dev.yaml"
TPL="$UMBRELLA/charts/kaname/templates/deployment.yaml"

# ТРИ ИСХОДА (0 зелено · 1 находка о дереве · 2 условие не создано) — общей
# реализацией на весь каталог. До #1195 отказ helm по причине, НЕ относящейся к
# предмету проверки (зависимости умбреллы не собраны), убивал прогон на первом
# же присваивании `…="$(render_only …)"` под `set -e`, НЕ СКАЗАВ НИЧЕГО: код 1,
# ноль байт. Утверждение секции 2 о непустом рендере (`[ -n … ] || fail …`) до
# этого места просто не доезжало.
# shellcheck source=deploy/tests/helm/outcome.sh
. "$(dirname "$0")/outcome.sh"
EXPECTED_ASSERTIONS=3

require_helm
require_mikefarah_yq

[ -f "$PROD" ] || fatal "values.prod.yaml нет на диске ($PROD)"
[ -f "$DEV" ]  || fatal "values.dev.yaml нет на диске ($DEV)"
[ -f "$TPL" ]  || fatal "шаблона kaname нет на диске ($TPL)"

# Full per-edge env set, INCLUDING the new CLIENTAUTHMODE (M2): the prior array
# knew only ENABLE/CERTFILE/KEYFILE/CLIENTCAFILES — adding CLIENTAUTHMODE makes the
# capability-intact section RED against a template that does not yet emit it.
METRICS_ENV=(
  KANAME_METRICS_SERVER_MTLS_ENABLE
  KANAME_METRICS_SERVER_MTLS_CLIENTAUTHMODE
  KANAME_METRICS_SERVER_MTLS_CERTFILE
  KANAME_METRICS_SERVER_MTLS_KEYFILE
  KANAME_METRICS_SERVER_MTLS_CLIENTCAFILES
)

# ── 1. PROD values DECISION — gate ON in server-tls-only mode (deterministic yq) ─
prod_http="$(yq '.["kaname"].mtls.httpListeners' "$PROD")"
[ "$prod_http" = "true" ] \
  || fail "prod: kaname.mtls.httpListeners=$prod_http (want true — metrics transport hardening ON, kacho-iam#137)"
prod_metrics_mode="$(yq '.["kaname"].mtls.metricsClientAuthMode' "$PROD")"
[ "$prod_metrics_mode" = "server-tls-only" ] \
  || fail "prod: metricsClientAuthMode=$prod_metrics_mode (want server-tls-only — no scrape client cert wired)"
# enable=true is the precondition (the SEC-F server cert-trio is mounted only then).
prod_enable="$(yq '.["kaname"].mtls.enable' "$PROD")"
[ "$prod_enable" = "true" ] || fail "prod: kaname.mtls.enable=$prod_enable (httpListeners requires enable=true — no new PKI)"
ok

# ── 2. DEV — httpListeners off ───────────────────────────────────────────────
dev_http="$(yq '.["kaname"].mtls.httpListeners // false' "$DEV")"
[ "$dev_http" != "true" ] \
  || fail "dev: kaname.mtls.httpListeners=$dev_http (dev metrics listener must stay PLAINTEXT — regression!)"
ok

# ── 3. CAPABILITY INTACT — the chart emits the gated block + every env (CLIENTAUTHMODE) ─
#
# ГЕЙТ УТВЕРЖДАЕТСЯ ИСХОДОМ РЕНДЕРА, А НЕ ТЕКСТОМ ШАБЛОНА. Прежняя редакция искала
# в шаблоне буквальную строку `{{- if .Values.mtls.httpListeners }}`. Ручка с тех пор
# разветвилась на рёбра (metrics / jwks-proxy / docker-token читаются из неё же
# как из умолчания), буквы разошлись — и проверка утверждала отсутствие СТРОКИ, тогда
# как способность была на месте: утверждение про текст пережило свой предмет.
#
# Теперь спрашивается то, что и требовалось: ручка ВКЛЮЧАЕТ блок и, снятая, ВЫКЛЮЧАЕТ
# его. Пара обязательна: одно положительное не отличило бы гейт от безусловного блока.
# Рендерится ПОД-ЧАРТ отдельно — в отличие от умбреллы (см. DETERMINISM NOTE выше) он
# детерминирован: пять подряд рендеров дают один и тот же состав ручек.
gate_render() {
  helm_try iam "$UMBRELLA/charts/kaname" --set mtls.enable=true "$@" \
    --show-only templates/deployment.yaml
  render_or_fatal "под-чарт kaname standalone${*:+ [$*]}"
}
gate_render --set mtls.httpListeners=true; GATE_ON="$HELM_OUT"
gate_render; GATE_OFF="$HELM_OUT"
for name in KANAME_METRICS_SERVER_MTLS_ENABLE; do
  [[ "$GATE_ON" == *"name: $name"* ]] \
    || fail "capability: mtls.httpListeners=true НЕ включает $name — способность потеряна, боевой профиль отгрузил бы открытый листенер"
  if [[ "$GATE_OFF" == *"name: $name"* ]]; then
    fail "capability: $name эмитируется и БЕЗ mtls.httpListeners — гейта нет, значение ручки исхода не меняет"
  fi
done
for name in "${METRICS_ENV[@]}"; do
  grep -q "name: $name" "$TPL" \
    || fail "capability: env $name missing from template — server-side TLS support / CLIENTAUTHMODE not emitted"
done
# The CLIENTAUTHMODE env defaults to server-tls-only in the template.
[[ "$(grep -A1 'KANAME_METRICS_SERVER_MTLS_CLIENTAUTHMODE' "$TPL")" == *'metricsClientAuthMode'* ]] \
  || fail "capability: metrics CLIENTAUTHMODE must derive from .Values.mtls.metricsClientAuthMode"
# The metrics block must REUSE the mounted server cert-trio (no new PKI).
[[ "$(grep -A1 'KANAME_METRICS_SERVER_MTLS_CERTFILE' "$TPL")" == *'tls.crt'* ]] \
  || fail "capability: metrics certfile must reuse the mounted server tls.crt (SEC-F)"
ok

outcome_verdict "профилей прочитано: 2 (dev, prod)"
