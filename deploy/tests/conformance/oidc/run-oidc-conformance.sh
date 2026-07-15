#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# KAC-127 Phase 12 — OpenID Foundation Conformance Suite runner.
#
# Wraps the openid/conformance-suite Docker image; runs against deployed
# Hydra at https://hydra.<domain>; outputs JSON report consumed by
# report_generator.py.
#
# Domain is fully configurable via --domain or KACHO_DOMAIN env. All
# endpoints (issuer, token, jwks, etc) are derived from it.
#
# Usage:
#   ./run-oidc-conformance.sh --domain=api.kacho.cloud \
#                             --client-id=test-client \
#                             --client-secret-from-env=OIDC_CLIENT_SECRET \
#                             --output-dir=./results

set -euo pipefail

DOMAIN="${KACHO_DOMAIN:-api.kacho.cloud}"
CLIENT_ID="${OIDC_TEST_CLIENT_ID:-}"
CLIENT_SECRET_FROM_ENV=""
OUTPUT_DIR="./oidc-conformance-results"
CONFORMANCE_IMAGE="openid/conformance-suite:release-v5.1.4"
PROFILE="oidcc-basic"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --domain=*)
      DOMAIN="${1#*=}"
      shift
      ;;
    --client-id=*)
      CLIENT_ID="${1#*=}"
      shift
      ;;
    --client-secret-from-env=*)
      CLIENT_SECRET_FROM_ENV="${1#*=}"
      shift
      ;;
    --output-dir=*)
      OUTPUT_DIR="${1#*=}"
      shift
      ;;
    --profile=*)
      PROFILE="${1#*=}"
      shift
      ;;
    --image=*)
      CONFORMANCE_IMAGE="${1#*=}"
      shift
      ;;
    -h|--help)
      grep '^#' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "ERROR: unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

if [[ -z "${CLIENT_ID}" ]]; then
  echo "ERROR: --client-id required (or set OIDC_TEST_CLIENT_ID env)" >&2
  exit 2
fi

CLIENT_SECRET=""
if [[ -n "${CLIENT_SECRET_FROM_ENV}" ]]; then
  CLIENT_SECRET="${!CLIENT_SECRET_FROM_ENV:-}"
  if [[ -z "${CLIENT_SECRET}" ]]; then
    echo "ERROR: env var ${CLIENT_SECRET_FROM_ENV} is empty" >&2
    exit 2
  fi
fi

ISSUER="https://hydra.${DOMAIN}"
JWKS_URI="${ISSUER}/.well-known/jwks.json"
DISCOVERY_URL="${ISSUER}/.well-known/openid-configuration"

echo "============================================================"
echo "  Kachō OIDC Conformance — KAC-127 Phase 12"
echo "============================================================"
echo "  Issuer:        ${ISSUER}"
echo "  Discovery:     ${DISCOVERY_URL}"
echo "  JWKS:          ${JWKS_URI}"
echo "  Client ID:     ${CLIENT_ID}"
echo "  Profile:       ${PROFILE}"
echo "  Output dir:    ${OUTPUT_DIR}"
echo "  Image:         ${CONFORMANCE_IMAGE}"
echo "============================================================"

# Verify issuer is reachable + serves discovery JSON
if ! curl -sf "${DISCOVERY_URL}" > /dev/null; then
  echo "ERROR: cannot reach ${DISCOVERY_URL}" >&2
  exit 3
fi

mkdir -p "${OUTPUT_DIR}"

# Generate test configuration JSON
CONFIG_FILE="${OUTPUT_DIR}/config.json"
cat > "${CONFIG_FILE}" <<EOF
{
  "alias": "kacho-${PROFILE}",
  "description": "Kachō Cloud OIDC self-certification — ${PROFILE}",
  "server": {
    "discoveryUrl": "${DISCOVERY_URL}"
  },
  "client": {
    "client_id": "${CLIENT_ID}",
    "client_secret": "${CLIENT_SECRET}"
  },
  "client2": {
    "client_id": "${CLIENT_ID}-secondary",
    "client_secret": "${CLIENT_SECRET}"
  },
  "consent": {
    "automated": true
  },
  "browser": [
    {
      "match": "https://${DOMAIN}/*",
      "tasks": [
        {
          "task": "Login",
          "match": "https://kratos.${DOMAIN}/*",
          "commands": [
            ["text", "id", "identifier", "test-user@${DOMAIN}"],
            ["text", "id", "password", "(skipped — passkey-flow uses WebAuthn)"]
          ]
        }
      ]
    }
  ]
}
EOF

echo "Config written: ${CONFIG_FILE}"

# Run conformance suite via Docker.
echo "Pulling ${CONFORMANCE_IMAGE}…"
docker pull "${CONFORMANCE_IMAGE}"

echo "Starting conformance suite. This may take 15-30 minutes…"
docker run --rm \
  -v "${OUTPUT_DIR}:/work" \
  -e "TEST_CONFIG=/work/config.json" \
  -e "TEST_PROFILE=${PROFILE}" \
  --network=host \
  "${CONFORMANCE_IMAGE}" \
  run-test-plan --plan-name="${PROFILE}" \
                --config-file=/work/config.json \
                --output-dir=/work/

echo "============================================================"
echo "  Conformance run complete. Report: ${OUTPUT_DIR}/report.html"
echo "============================================================"
