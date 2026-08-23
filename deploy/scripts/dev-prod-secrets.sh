#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# dev-prod-secrets.sh — provision the AuthN secrets that kacho-iam production-strict
# Config.Validate REQUIRES (fail-closed, defense-in-depth).
#
#   - kacho-iam-hook-token   key=token    — общий секрет обратных вызовов провайдера
#   - kacho-iam-jwks-enc-key key=enc_key  — 32-byte-hex JWKS private-key encryption key
#
# ЗАПУСКАЕТСЯ ДО ПЕРВОГО ПРОГОНА helm, А НЕ МЕЖДУ ПРОГОНАМИ (задача #948). Общий
# секрет обратных вызовов — ПРЕДУСЛОВИЕ: провайдер берёт его величину обязательной
# ссылкой уже в базовом профиле, потому что необязательная ссылка на отсутствующий
# секрет даёт ПУСТУЮ величину, полосу отвергают на первом же обращении, а стенд при
# этом выглядит поднятым. Здесь стояло «Run BEFORE the production helm upgrade» —
# верно для прежнего порядка, когда ссылку нёс только слой боевой посадки.
#
# Idempotent (apply). NB: these are LOCAL kind
# dev-stand secrets, generated fresh each run — NOT committed, NOT production key
# material. A real cluster provisions them out-of-band / via external-secrets.
set -euo pipefail
NS="${KACHO_NAMESPACE:-kacho}"

# 32-byte hex (64 chars) — iam ResolveJWKSEncryptionKey() requires exactly 32 bytes.
ENC_KEY="$(openssl rand -hex 32)"
kubectl -n "$NS" create secret generic kacho-iam-jwks-enc-key \
  --from-literal=enc_key="$ENC_KEY" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null

# ─── ОБЩИЙ СЕКРЕТ ОБРАТНОГО ВЫЗОВА: СОЗДАЁТСЯ ОДИН РАЗ, НЕ РОТИРУЕТСЯ ────────
#
# Величину читают ДВА пода — отправитель (провайдер) и проверяющая сторона
# (служба прав), — и каждый читает её ОДИН РАЗ, при старте контейнера.
# Перевыпуск на каждом прогоне поэтому не «обновляет секрет», а заводит окно, в
# котором стороны держат РАЗНЫЕ величины: повторный `make dev-up` пересобирает
# образы служб, поэтому под службы прав перекатывается и берёт новую величину, а
# под провайдера остаётся прежним и продолжает подписывать старой. Исход — тот
# самый `401` на обратном вызове, ради которого заведена задача #948, только
# приходящий не с первой выкатки, а со второй.
#
# Поэтому: сгенерировать ОДИН раз, дальше переиспользовать — той же формой, что
# у подписного ключа ниже. Ротация общего секрета — осознанное действие: снять
# секрет и перекатить ОБА пода, а не побочный эффект подъёма стенда.
if kubectl -n "$NS" get secret kacho-iam-hook-token >/dev/null 2>&1; then
  echo "kacho-iam-hook-token already present — reusing (обе стороны держат одну величину)"
else
  kubectl -n "$NS" create secret generic kacho-iam-hook-token \
    --from-literal=token="$(openssl rand -hex 24)" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "provisioned kacho-iam-hook-token (token, 24B hex)"
fi

# Bootstrap-admin SA ES256 (P-256, PKCS#8) signing key — the private key that
# InternalBootstrapTokenService (#58) uses to sign the private_key_jwt
# client_assertion it exchanges at Hydra (aud=https://{API_DOMAIN}) for the first
# non-interactive RS256 admin Bearer. The mint use-case derives the PUBLIC JWK
# from this key and self-registers the Hydra OAuth client on first mint — so the
# key MUST be STABLE across re-runs (regenerating it would orphan the already-
# registered Hydra client's JWK → assertion signature no longer verifies). Hence:
# generate ONCE; reuse the existing secret on re-run (idempotent, NOT rotate).
if kubectl -n "$NS" get secret kacho-iam-bootstrap-sa-key >/dev/null 2>&1; then
  echo "kacho-iam-bootstrap-sa-key already present — reusing (stable signing key)"
else
  BOOTSTRAP_KEY="$(openssl ecparam -name prime256v1 -genkey -noout | openssl pkcs8 -topk8 -nocrypt)"
  kubectl -n "$NS" create secret generic kacho-iam-bootstrap-sa-key \
    --from-literal=private_key_pem="$BOOTSTRAP_KEY" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "provisioned kacho-iam-bootstrap-sa-key (private_key_pem, ES256 P-256)"
fi

echo "prerequisite secrets ready in ns/$NS"
