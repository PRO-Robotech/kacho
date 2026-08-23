#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# dev-prod-secrets.sh — provision the AuthN secrets that kacho-iam production-strict
# Config.Validate REQUIRES (fail-closed, defense-in-depth). On the insecure-dev stand
# both are `optional: true` secretKeyRefs (absent) so dev iam boots without them; the
# production-mode stand (values.dev-prod.yaml) MUST have them present.
#
#   - kacho-iam-hook-token   key=token    — Hydra token-hook HMAC shared secret
#   - kacho-iam-jwks-enc-key key=enc_key  — 32-byte-hex JWKS private-key encryption key
#
# Idempotent (apply). Run BEFORE the production helm upgrade. NB: these are LOCAL kind
# dev-stand secrets — NOT committed, NOT production key material. A real cluster
# provisions them out-of-band / via external-secrets.
#
# ЧТО ЗДЕСЬ ПОРОЖДАЕТСЯ ОДНАЖДЫ, А ЧТО КАЖДЫЙ РАЗ — это не стиль, а свойство
# величины (задача #1062). Величина, которой УЖЕ ЧТО-ТО ЗАПИСАНО в базе,
# порождается ровно один раз и переиспользуется на каждом следующем прогоне:
# перевыпуск делает записанное нечитаемым НАВСЕГДА, и обнаруживается это не
# отказом, а тем, что клиент перестаёт верить выданным токенам. Дисциплину
# держит гейт deploy/tests/helm/secret-material-survives-recreation-test.sh.
set -euo pipefail
NS="${KACHO_NAMESPACE:-kacho}"

# ── Ключ ОБЁРТКИ приватной половины подписного ключа ────────────────────────
# 32-byte hex (64 chars) — iam ResolveJWKSEncryptionKey() requires exactly 32 bytes.
#
# Им обёрнута колонка kacho_iam.token_signing_keys.private_key_wrapped
# (services/iam/internal/keywrap, AES-256-GCM). Поэтому ключ ОБЯЗАН пережить
# пересоздание стенда: новый ключ не разворачивает ни одной уже записанной
# приватной половины, и вернуть их нечем. Порождаем ОДНАЖДЫ, дальше —
# переиспользуем (идемпотентно, НЕ ротация).
#
# Значение негодной формы здесь не чинится намеренно: iam сверяет длину при
# старте и отказывается подниматься, называя ручку. Молча заменить его на новое
# значило бы, что ошибка настройки становится рабочим режимом.
if kubectl -n "$NS" get secret kacho-iam-jwks-enc-key >/dev/null 2>&1; then
  echo "kacho-iam-jwks-enc-key already present — reusing (wrapping key, must survive re-runs)"
else
  ENC_KEY="$(openssl rand -hex 32)"
  kubectl -n "$NS" create secret generic kacho-iam-jwks-enc-key \
    --from-literal=enc_key="$ENC_KEY" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  echo "provisioned kacho-iam-jwks-enc-key (enc_key, 32B hex) — generated ONCE"
fi

# Strong random HMAC token for the Hydra token-hook.
HOOK_TOKEN="$(openssl rand -hex 24)"

kubectl -n "$NS" create secret generic kacho-iam-hook-token \
  --from-literal=token="$HOOK_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null

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

echo "provisioned kacho-iam-hook-token (token) in ns/$NS"
