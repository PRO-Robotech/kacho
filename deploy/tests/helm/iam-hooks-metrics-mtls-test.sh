#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# kaname Hydra/Kratos hooks listener (:9092) + Prometheus /metrics listener
# (:9095) — per-edge server-side TLS, reusing the SEC-F internal-CA server cert.
#
# Sub-phase 5.5 (kacho-iam#137, supersedes the OFF-gate #122/#136): the hooks edge
# carries FOUR HMAC-authenticated hook endpoints (Hydra token/refresh + Kratos
# provision/recovery); every caller is an HTTP client that presents NO transport
# client-cert (Ory webhooks cannot). The old RequireAndVerifyClientCert default
# would reject every webhook at the TLS handshake. 5.5 introduces a per-edge
# clientAuthMode: the hooks/metrics edges run `server-tls-only` (encryption +
# server-auth; HMAC is the caller-auth — a single mode on :9092 correctly covers
# all four hook endpoints). The prod gate is now flipped ON.
#
# ЭТОТ ГЕЙТ СУДИТ ДВЕ ПОЛОСЫ ИЗ ЧЕТЫРЁХ, И ЭТО НАЗВАНО, ЧТОБЫ «ЗЕЛЕНО» НЕ ЧИТАЛОСЬ
# ШИРЕ, ЧЕМ ЕСТЬ: он читает конфигурацию внешнего сервера OAuth и утверждает
# транспорт полос token/refresh. Полосы службы личности (provision/recovery) он не
# видит вовсе, а с уходом внешнего сервера (эпик #896) у него не останется предмета.
# Транспорт КАЖДОЙ полосы — по перечню, выведенному из конфигурации, — держит
# deploy/identity_callback_transport_test.go; он не знает ни имени того сервера, ни
# его ключей и потому переживёт его уход.
#
# DETERMINISM NOTE: `helm template` on this large multi-subchart umbrella renders
# the kaname subchart Deployment NON-deterministically (values coalescing — the
# httpListeners-gated env block is present/absent across repeated renders; this
# PRE-DATES #137 and is why the prior guard asserted prod-OFF via env-ABSENCE and
# capability via the TEMPLATE SOURCE). So the prod-ON DECISION is asserted from
# values.prod.yaml directly (yq — deterministic). CAPABILITY is asserted from the
# STANDALONE sub-chart render (deterministic, unlike the umbrella): the gate itself
# by an on/off pair, the per-edge env names + their derivation from the template
# source. The Hydra subchart renders ARE deterministic, so its https-URL + CA-mount
# are asserted by render.
#
# This guard asserts:
#   - PROD values DECISION → kaname.mtls.httpListeners=true with both
#     {hooks,metrics}ClientAuthMode=server-tls-only (gate ON, server-tls-only);
#   - PROD Hydra render → token/refresh webhook URLs https://…:9092 (encrypted),
#     HMAC X-Kacho-Hook-Token kept, Hydra pod mounts the SEC-F internal-CA bundle
#     (kaname-server-tls) + SSL_CERT_FILE so it trusts the kaname server cert;
#   - DEV → httpListeners off (unset/false) + Hydra hook URLs stay plaintext
#     http://…:9092 (newman stand byte-identical);
#   - CAPABILITY INTACT → the kaname deployment TEMPLATE emits every per-edge env
#     incl. CLIENTAUTHMODE, reusing the mounted SEC-F server cert (no new PKI).
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
TPL="$UMBRELLA/charts/kaname/templates/deployment.yaml"

# ТРИ ИСХОДА (0 зелено · 1 находка о дереве · 2 условие не создано) — общей
# реализацией на весь каталог. До #1195 отказ helm по причине, НЕ относящейся к
# предмету проверки (зависимости умбреллы не собраны), убивал прогон на первом
# же `HYDRA_CM_PROD="$(render_only …)"` под `set -e`, НЕ СКАЗАВ НИЧЕГО: код 1,
# ноль байт. Утверждение секции 2 `[ -n "$HYDRA_CM_PROD" ] || fail …` до этого
# места просто не доезжало.
# shellcheck source=deploy/tests/helm/outcome.sh
. "$(dirname "$0")/outcome.sh"
EXPECTED_ASSERTIONS=5

require_helm
require_mikefarah_yq

[ -f "$PROD" ] || fatal "values.prod.yaml нет на диске ($PROD)"
[ -f "$DEV" ]  || fatal "values.dev.yaml нет на диске ($DEV)"
[ -f "$TPL" ]  || fatal "шаблона kaname нет на диске ($TPL)"

# render_only <values> <show-only-template> — результат в $HELM_OUT.
render_only() {
  helm_try kacho-umbrella "$UMBRELLA" -f "$1" --show-only "$2"
  render_or_fatal "$(basename "$1") → $2"
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
prod_http="$(yq '.["kaname"].mtls.httpListeners' "$PROD")"
[ "$prod_http" = "true" ] \
  || fail "prod: kaname.mtls.httpListeners=$prod_http (want true — hooks/metrics transport hardening ON, kacho-iam#137)"
prod_hooks_mode="$(yq '.["kaname"].mtls.hooksClientAuthMode' "$PROD")"
prod_metrics_mode="$(yq '.["kaname"].mtls.metricsClientAuthMode' "$PROD")"
[ "$prod_hooks_mode" = "server-tls-only" ] \
  || fail "prod: hooksClientAuthMode=$prod_hooks_mode (want server-tls-only — Ory webhooks present no client cert)"
[ "$prod_metrics_mode" = "server-tls-only" ] \
  || fail "prod: metricsClientAuthMode=$prod_metrics_mode (want server-tls-only — no scrape client cert wired)"
# enable=true is the precondition (the SEC-F server cert-trio is mounted only then).
prod_enable="$(yq '.["kaname"].mtls.enable' "$PROD")"
[ "$prod_enable" = "true" ] || fail "prod: kaname.mtls.enable=$prod_enable (httpListeners requires enable=true — no new PKI)"
ok

# ── 2. PROD Hydra — token/refresh webhook URLs https://…:9092 + HMAC kept ────
render_only "$PROD" charts/hydra/templates/configmap.yaml; HYDRA_CM_PROD="$HELM_OUT"
[ -n "$HYDRA_CM_PROD" ] || fail "hydra configmap did not render in prod profile"
any_line_matches "$HYDRA_CM_PROD" 'https://[^"]*:9092/iam/v1/hooks/token' \
  || fail "prod: Hydra token_hook URL must be https://…:9092/iam/v1/hooks/token (got plaintext or missing)"
any_line_matches "$HYDRA_CM_PROD" 'https://[^"]*:9092/iam/v1/hooks/refresh' \
  || fail "prod: Hydra refresh_token_hook URL must be https://…:9092/iam/v1/hooks/refresh"
if any_line_matches "$HYDRA_CM_PROD" 'http://[^"]*:9092/iam/v1/hooks/'; then
  fail "prod: Hydra hook URL still uses plaintext http://…:9092 (must be https for server-tls-only)"
fi
[[ "$HYDRA_CM_PROD" == *'X-Kacho-Hook-Token'* ]] \
  || fail "prod: Hydra webhook must still carry the HMAC header X-Kacho-Hook-Token (caller-auth unchanged)"
ok

# ── 3. PROD Hydra pod mounts the internal-CA bundle (trusts kaname server cert) ─
render_only "$PROD" charts/hydra/templates/deployment.yaml; HYDRA_DEPLOY_PROD="$HELM_OUT"
[ -n "$HYDRA_DEPLOY_PROD" ] || fail "hydra deployment did not render in prod profile"
[[ "$HYDRA_DEPLOY_PROD" == *'kaname-server-tls'* ]] \
  || fail "prod: Hydra pod must mount the SEC-F internal-CA bundle (kaname-server-tls) for webhook CA-trust"
any_line_matches "$HYDRA_DEPLOY_PROD" 'name: SSL_CERT_FILE|name: SSL_CERT_DIR' \
  || fail "prod: Hydra must set SSL_CERT_FILE/SSL_CERT_DIR so its webhook client trusts the internal-CA server cert"
ok

# ── 4. DEV — httpListeners off + Hydra hook URLs stay plaintext ──────────────
dev_http="$(yq '.["kaname"].mtls.httpListeners // false' "$DEV")"
[ "$dev_http" != "true" ] \
  || fail "dev: kaname.mtls.httpListeners=$dev_http (dev hooks/metrics listener must stay PLAINTEXT — regression!)"
render_only "$DEV" charts/hydra/templates/configmap.yaml; HYDRA_CM_DEV="$HELM_OUT"
any_line_matches "$HYDRA_CM_DEV" 'http://[^"]*:9092/iam/v1/hooks/token' \
  || fail "dev: Hydra token_hook URL must stay plaintext http://…:9092 (newman stand unchanged)"
ok

# ── 5. CAPABILITY INTACT — the chart emits the gated block + every env (CLIENTAUTHMODE) ─
#
# ГЕЙТ УТВЕРЖДАЕТСЯ ИСХОДОМ РЕНДЕРА, А НЕ ТЕКСТОМ ШАБЛОНА. Прежняя редакция искала
# в шаблоне буквальную строку `{{- if .Values.mtls.httpListeners }}`. Ручка с тех пор
# разветвилась на рёбра (hooks+metrics / jwks-proxy / docker-token читаются из неё же
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
for name in KACHO_IAM_HOOKS_SERVER_MTLS_ENABLE KACHO_IAM_METRICS_SERVER_MTLS_ENABLE; do
  [[ "$GATE_ON" == *"name: $name"* ]] \
    || fail "capability: mtls.httpListeners=true НЕ включает $name — способность потеряна, боевой профиль отгрузил бы открытый листенер"
  if [[ "$GATE_OFF" == *"name: $name"* ]]; then
    fail "capability: $name эмитируется и БЕЗ mtls.httpListeners — гейта нет, значение ручки исхода не меняет"
  fi
done
for name in "${HOOKS_METRICS_ENV[@]}"; do
  grep -q "name: $name" "$TPL" \
    || fail "capability: env $name missing from template — server-side TLS support / CLIENTAUTHMODE not emitted"
done
# Both CLIENTAUTHMODE env default to server-tls-only in the template.
[[ "$(grep -A1 'KACHO_IAM_HOOKS_SERVER_MTLS_CLIENTAUTHMODE' "$TPL")" == *'hooksClientAuthMode'* ]] \
  || fail "capability: hooks CLIENTAUTHMODE must derive from .Values.mtls.hooksClientAuthMode"
[[ "$(grep -A1 'KACHO_IAM_METRICS_SERVER_MTLS_CLIENTAUTHMODE' "$TPL")" == *'metricsClientAuthMode'* ]] \
  || fail "capability: metrics CLIENTAUTHMODE must derive from .Values.mtls.metricsClientAuthMode"
# The hooks/metrics block must REUSE the mounted server cert-trio (no new PKI).
[[ "$(grep -A1 'KACHO_IAM_HOOKS_SERVER_MTLS_CERTFILE' "$TPL")" == *'tls.crt'* ]] \
  || fail "capability: hooks certfile must reuse the mounted server tls.crt (SEC-F)"
ok

outcome_verdict "профилей прочитано: 2 (dev, prod)"
