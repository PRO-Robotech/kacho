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
# dev-stand secrets, generated fresh each run — NOT committed, NOT production key
# material. A real cluster provisions them out-of-band / via external-secrets.
set -euo pipefail
NS="${KACHO_NAMESPACE:-kacho}"

# 32-byte hex (64 chars) — iam ResolveJWKSEncryptionKey() requires exactly 32 bytes.
ENC_KEY="$(openssl rand -hex 32)"
# Strong random HMAC token for the Hydra token-hook.
HOOK_TOKEN="$(openssl rand -hex 24)"

kubectl -n "$NS" create secret generic kacho-iam-jwks-enc-key \
  --from-literal=enc_key="$ENC_KEY" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null

kubectl -n "$NS" create secret generic kacho-iam-hook-token \
  --from-literal=token="$HOOK_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null

echo "provisioned kacho-iam-jwks-enc-key (enc_key, 32B hex) + kacho-iam-hook-token (token) in ns/$NS"
