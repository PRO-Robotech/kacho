#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# bootstrap-mint-operator-identity-test.sh — the cluster-admin token mint (#58)
# has EXACTLY ONE caller, and both halves of that statement are rendered by Helm.
#
# BACKGROUND. `iam.v1.InternalBootstrapTokenService/MintBootstrapToken` issues a
# Bearer for a cluster `system_admin` ServiceAccount, подписанный НАШИМ
# подписантом (задача #1119 — прежде его выдавал внешний поставщик). It has no
# ReBAC gate (it exists to obtain the FIRST token, before any relation exists) and
# no api-gateway REST route (on the plain-HTTP internal listener a route would be a
# credential-free takeover). Its only credential is the CALLER'S CLIENT CERTIFICATE:
# kacho-iam admits the SPIFFE SANs in `authn.bootstrap-mint.allowed-client-sans`
# (authzguard.CallerPolicy arm 3 — enforced in EVERY mode, empty list = deny all).
#
# That splits the operator path across two independently-rendered manifests:
#   (a) umbrella templates/bootstrap-operator-certificate.yaml — issues the
#       client-auth leaf whose URI-SAN identifies the operator;
#   (b) charts/kacho-iam configmap — the allow-list kacho-iam actually enforces.
# Drift between them is SILENT and fails CLOSED: the mint denies every caller (no
# alarm), and in production the boot-guard then refuses to start a stand that looks
# configured. Worse, the usual "fix" for a mint nobody can call is to reopen the
# hole. So this guard asserts (a) and (b) render AND carry the SAME SAN string.
#
# It also pins the two properties that make the identity meaningful:
#   - it is NOT the api-gateway identity (the gateway fronts tenant traffic and is
#     reachable cluster-wide; "is the api-gateway" must never license a
#     cluster-admin mint — that is the whole point of the dedicated leaf);
#   - the chart DEFAULT allow-list is EMPTY (a stand that configures nothing gets a
#     mint with no callers, never a default caller).
#
# Offline manifest-assertion harness (no kind cluster). Mirrors tests/helm/*.
set -euo pipefail

SCRIPT="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
UMBRELLA="$REPO_ROOT/helm/umbrella"
DEV="$UMBRELLA/values.dev.yaml"
DEVPROD="$UMBRELLA/values.dev-prod.yaml"
CERT_TPL="templates/bootstrap-operator-certificate.yaml"
IAM_CM_TPL="charts/kacho-iam/templates/configmap.yaml"
GATEWAY_SAN="spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"
# ТРИ ИСХОДА (0 зелено · 1 находка о дереве · 2 условие не создано) — общей
# реализацией на весь каталог. До #1195 отказ helm по причине, НЕ относящейся к
# предмету проверки (зависимости умбреллы не собраны), убивал прогон на первом
# же `CERT="$(render …)"` под `set -e`, НЕ СКАЗАВ НИЧЕГО: код 1, ноль байт.
# shellcheck source=deploy/tests/helm/outcome.sh
. "$(dirname "$0")/outcome.sh"
EXPECTED_ASSERTIONS=15

# line_in <многострочное значение> <строка> — есть ли СТРОКА ЦЕЛИКОМ в значении.
#
# Замена `grep -qx`: под `set -o pipefail` конвейер `echo … | grep -qx` возвращает
# ОТКАЗ НА СОВПАДЕНИИ — grep выходит по первому попаданию, писатель получает
# SIGPIPE, и статус берётся от него (задача #658). Сравнение здесь БУКВАЛЬНОЕ,
# тогда как `grep -x` трактовал образец как BRE; для SPIFFE-SAN это СТРОЖЕ
# (точка перестаёт значить «любой символ»), то есть ложного зелёного добавить
# не может.
line_in() { [[ $'\n'"$1"$'\n' == *$'\n'"$2"$'\n'* ]]; }

require_helm

[ -f "$DEV" ] || fatal "values.dev.yaml нет на диске ($DEV)"
[ -f "$DEVPROD" ] || fatal "values.dev-prod.yaml нет на диске ($DEVPROD)"

# render <show-only-template> <values-file...> — результат в $HELM_OUT.
# Отказ рендера — код 2 плюс ТЕКСТ helm, а не молчаливая смерть под `set -e`.
render() {
  local tpl="$1"; shift
  local args=()
  local f
  for f in "$@"; do args+=(-f "$f"); done
  helm_try kacho-umbrella "$UMBRELLA" "${args[@]}" --show-only "$tpl"
  render_or_fatal "$* → $tpl"
}

# spiffe_uris <text> — every spiffe:// URI, deduped, one per line (may be empty).
spiffe_uris() { echo "$1" | grep -oE 'spiffe://[A-Za-z0-9./_-]+' | sort -u || true; }

# allowlist_of <iam-configmap-render> — the SANs inside the
# authn.bootstrap-mint.allowed-client-sans block only (never other SANs in the file).
allowlist_of() {
  echo "$1" | sed -n '/allowed-client-sans:/,/^ *[a-z-]*:/p' \
    | grep -oE 'spiffe://[A-Za-z0-9./_-]+' | sort -u || true
}

# ── 1. dev-prod (production posture) — the operator Certificate renders ───────
# This profile ENABLES the mint (bootstrapToken.signingKeySecretName is set), so a
# missing/uncallable caller identity is a boot failure, not a cosmetic gap.
render "$CERT_TPL" "$DEV" "$DEVPROD"; CERT="$HELM_OUT"
[[ "$CERT" == *"kind: Certificate"* ]] \
  || fail "values.dev-prod renders no bootstrap-operator Certificate — the mint would have no caller identity"; ok
[[ "$CERT" == *"client auth"* ]] \
  || fail "bootstrap-operator leaf is not a CLIENT cert (usages must be 'client auth')"; ok
[[ "$CERT" == *"kind: ClusterIssuer"* ]] \
  || fail "bootstrap-operator leaf is not issued by the internal-CA ClusterIssuer"; ok

CERT_SAN="$(spiffe_uris "$CERT")"
[ -n "$CERT_SAN" ] || fail "bootstrap-operator Certificate carries no spiffe:// URI-SAN — nothing for the allow-list to match"; ok
[ "$(echo "$CERT_SAN" | wc -l)" = "1" ] \
  || fail "bootstrap-operator Certificate carries multiple SANs ($CERT_SAN) — the mint identity must be unambiguous"; ok

# ── 2. …and it is NOT the api-gateway identity ───────────────────────────────
[ "$CERT_SAN" != "$GATEWAY_SAN" ] \
  || fail "bootstrap-operator reuses the api-gateway SAN ($GATEWAY_SAN) — 'is the api-gateway' must never license a cluster-admin mint"; ok

# ── 3. kacho-iam allow-lists EXACTLY that SAN (dev-prod) ─────────────────────
render "$IAM_CM_TPL" "$DEV" "$DEVPROD"; IAM_CM="$HELM_OUT"
[[ "$IAM_CM" == *"allowed-client-sans"* ]] \
  || fail "kacho-iam config.yaml has no authn.bootstrap-mint.allowed-client-sans — the mint gate is unconfigurable"; ok
ALLOWED="$(allowlist_of "$IAM_CM")"
[ -n "$ALLOWED" ] \
  || fail "values.dev-prod leaves the bootstrap-mint allow-list EMPTY while the mint is enabled — kacho-iam refuses to boot (core rule #16)"; ok
line_in "$ALLOWED" "$CERT_SAN" \
  || fail "allow-list ($ALLOWED) does not contain the issued operator SAN ($CERT_SAN) — the mint would deny its own operator"; ok
if line_in "$ALLOWED" "$GATEWAY_SAN"; then
  fail "the api-gateway SAN is allow-listed for the cluster-admin mint — the gateway must not be a minter"
fi; ok

# ── 4. dev stand — arm 3 is enforced in EVERY mode, so dev needs it too ──────
render "$IAM_CM_TPL" "$DEV"; DEV_CM="$HELM_OUT"
DEV_ALLOWED="$(allowlist_of "$DEV_CM")"
line_in "$DEV_ALLOWED" "$CERT_SAN" \
  || fail "values.dev.yaml does not allow-list the operator SAN — the SAN gate is enforced in dev too, so the dev mint would deny everyone"; ok
render "$CERT_TPL" "$DEV"; DEV_CERT_SAN="$(spiffe_uris "$HELM_OUT")"
[ "$DEV_CERT_SAN" = "$CERT_SAN" ] \
  || fail "dev issues SAN '$DEV_CERT_SAN' but dev-prod issues '$CERT_SAN' — the operator identity must be profile-stable"; ok

# ── 5. chart DEFAULT is deny-everyone ────────────────────────────────────────
# An operator who configures nothing must get a mint with NO callers — never a
# default caller. Override the profile's list back to empty and assert the render
# carries no SAN at all (but still carries the key, so the fail-closed setting
# stays visible in the deployed config rather than silently vanishing).
helm_try kacho-umbrella "$UMBRELLA" -f "$DEV" \
  --set-json 'kacho-iam.config.authn.bootstrapMint.allowedClientSANs=[]' \
  --show-only "$IAM_CM_TPL"
render_or_fatal "values.dev.yaml + пустой allowedClientSANs → $IAM_CM_TPL"
EMPTY_CM="$HELM_OUT"
EMPTY_ALLOWED="$(allowlist_of "$EMPTY_CM")"
[ -z "$EMPTY_ALLOWED" ] \
  || fail "an empty allowedClientSANs still renders callers ($EMPTY_ALLOWED) — the mint must have no default caller"; ok
[[ "$EMPTY_CM" == *"allowed-client-sans"* ]] \
  || fail "empty allow-list drops the key entirely — the fail-closed setting must stay visible in the rendered config"; ok

# ── 6. mTLS off → no operator identity is minted ─────────────────────────────
# The leaf is internal-CA material; without the PKI there is nothing to issue and
# nothing to trust.
# `|| true` здесь БЫЛО и было второй половиной того же дефекта: оно не
# отличало пустой УСПЕШНЫЙ рендер от отказа helm, поэтому на дереве без
# собранных зависимостей отрицание проходило вакуумно. Положительный контроль —
# секция 1 выше: тот же шаблон с включённым mTLS обязан дать Certificate.
# Приёмник писем снимается ВМЕСТЕ с mTLS, и это не обход его стража, а следствие
# того же решения: без внутреннего удостоверяющего сертификат приёмнику выпустить
# нечем, поэтому он роняет рендер намеренно (решение Р5, ban #16 — шифрование не
# снимается и на стенде). Предмет ЭТОЙ проверки — сертификат оператора, почта ей
# не нужна вовсе; оставить её включённой значило бы проверять чужой страж вместо
# своего предмета.
helm_try kacho-umbrella "$UMBRELLA" -f "$DEV" \
  --set mtls.enabled=false --set cert-manager.enabled=false \
  --set mailpit.enabled=false \
  --show-only "$CERT_TPL"
render_or_fatal "values.dev.yaml + mtls.enabled=false → $CERT_TPL"
NOMTLS="$HELM_OUT"
if [[ "$NOMTLS" == *"kind: Certificate"* ]]; then
  fail "bootstrap-operator Certificate renders with mtls.enabled=false — there is no internal CA to sign it"
fi; ok

outcome_verdict "профилей прочитано: 2 (dev, dev-prod)"
