#!/usr/bin/env bash
#
# KAC-127 Phase 12 — FIDO Alliance WebAuthn Conformance Test runner.
#
# Wraps FIDO Alliance Java conformance test tools against deployed Kratos
# WebAuthn endpoints. Requires:
#   - Java 11+ in PATH
#   - Conformance test JAR (loaned from FIDO Alliance after membership)
#   - Selenium WebDriver (Chrome / Firefox)
#
# RP ID is configurable via --rp-id or KACHO_DOMAIN env.
#
# Usage:
#   ./run-fido-conformance.sh --rp-id=api.kacho.cloud \
#                              --output-dir=./fido-results

set -euo pipefail

RP_ID="${KACHO_DOMAIN:-api.kacho.cloud}"
OUTPUT_DIR="./fido-conformance-results"
SELENIUM_HUB_URL="${SELENIUM_HUB_URL:-http://localhost:4444/wd/hub}"
CONFORMANCE_JAR="${FIDO_CONFORMANCE_JAR:-./tools/fido-conformance.jar}"
BROWSER="${BROWSER:-chrome}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --rp-id=*)
      RP_ID="${1#*=}"
      shift
      ;;
    --output-dir=*)
      OUTPUT_DIR="${1#*=}"
      shift
      ;;
    --selenium=*)
      SELENIUM_HUB_URL="${1#*=}"
      shift
      ;;
    --jar=*)
      CONFORMANCE_JAR="${1#*=}"
      shift
      ;;
    --browser=*)
      BROWSER="${1#*=}"
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

if [[ ! -f "${CONFORMANCE_JAR}" ]]; then
  echo "ERROR: FIDO conformance JAR not found at ${CONFORMANCE_JAR}" >&2
  echo "" >&2
  echo "The JAR is provided by FIDO Alliance after membership registration." >&2
  echo "Apply at https://fidoalliance.org/membership/ and download from members portal." >&2
  echo "" >&2
  echo "For initial self-testing without certification, you can use:" >&2
  echo "  - WebAuthn.io test page: https://webauthn.io/" >&2
  echo "  - duo-labs/webauthn manual testing: https://webauthn.guide/" >&2
  exit 3
fi

ATTESTATION_URL="https://kratos.${RP_ID}/self-service/registration/flows/webauthn"
ASSERTION_URL="https://kratos.${RP_ID}/self-service/login/flows/webauthn"

echo "============================================================"
echo "  Kachō FIDO L3 Conformance — KAC-127 Phase 12"
echo "============================================================"
echo "  RP ID:        ${RP_ID}"
echo "  Attestation:  ${ATTESTATION_URL}"
echo "  Assertion:    ${ASSERTION_URL}"
echo "  Browser:      ${BROWSER}"
echo "  Selenium hub: ${SELENIUM_HUB_URL}"
echo "  Output dir:   ${OUTPUT_DIR}"
echo "============================================================"

mkdir -p "${OUTPUT_DIR}"

if ! curl -sf "https://kratos.${RP_ID}/health/alive" > /dev/null; then
  echo "ERROR: Kratos at https://kratos.${RP_ID} is not reachable" >&2
  exit 4
fi

# Test plan covers:
#   - server-side validation (challenge, signature, counter, origin)
#   - authenticator-side validation (when run against test authenticator)
#   - protocol conformance (CBOR, COSE, attestation formats)
TEST_PLAN_FILE="${OUTPUT_DIR}/test-plan.json"
cat > "${TEST_PLAN_FILE}" <<EOF
{
  "rpId": "${RP_ID}",
  "rpName": "Kachō Cloud",
  "endpoints": {
    "attestationOptions": "${ATTESTATION_URL}/begin",
    "attestationResult": "${ATTESTATION_URL}/finish",
    "assertionOptions": "${ASSERTION_URL}/begin",
    "assertionResult": "${ASSERTION_URL}/finish"
  },
  "browser": "${BROWSER}",
  "seleniumHub": "${SELENIUM_HUB_URL}",
  "tests": [
    "Server-ServerAuthenticatorAttestationResponse-Resp-1",
    "Server-ServerAuthenticatorAttestationResponse-Resp-2",
    "Server-ServerAuthenticatorAttestationResponse-Resp-3",
    "Server-ServerAuthenticatorAttestationResponse-Resp-4",
    "Server-ServerAuthenticatorAttestationResponse-Resp-5",
    "Server-ServerAuthenticatorAssertionResponse-Resp-1",
    "Server-ServerAuthenticatorAssertionResponse-Resp-2",
    "Server-ServerAuthenticatorAssertionResponse-Resp-3",
    "Server-ServerAuthenticatorAssertionResponse-Resp-4",
    "Server-ServerAuthenticatorAssertionResponse-Resp-5",
    "Server-ServerAuthenticatorAssertionResponse-Resp-6",
    "Server-ServerAuthenticatorAssertionResponse-Resp-7"
  ]
}
EOF

echo "Test plan written: ${TEST_PLAN_FILE}"

# Run FIDO conformance JAR with the test plan.
java -jar "${CONFORMANCE_JAR}" \
    --plan="${TEST_PLAN_FILE}" \
    --output-dir="${OUTPUT_DIR}"

# Generate human-readable summary.
SUMMARY="${OUTPUT_DIR}/summary.txt"
{
  echo "FIDO L3 Conformance — Kachō Cloud"
  echo "Date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "RP ID: ${RP_ID}"
  echo ""
  if [[ -f "${OUTPUT_DIR}/results.json" ]]; then
    jq -r '.tests[] | "\(.status): \(.name)"' "${OUTPUT_DIR}/results.json"
  fi
} > "${SUMMARY}"

echo "============================================================"
echo "  Conformance complete. Summary: ${SUMMARY}"
echo "============================================================"
