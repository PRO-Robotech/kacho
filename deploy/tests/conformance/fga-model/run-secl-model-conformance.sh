#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# SEC-L FGA-model conformance proof (acceptance §5.4 — BLOCKING).
#
# Loads the canonical kacho-proto FGA model into a real OpenFGA store and
# asserts, at the MODEL-RESOLUTION level (a stub cannot catch a wildcard leak —
# the leak lives in FGA resolution, not in Go), that the SEC-L
# `... or system_viewer from cluster` cascade:
#
#   NEG (leak-guard): user:rando does NOT get viewer on account:aX / project:pX
#                     (the wildcard user:* in cluster.viewer must NOT reach
#                      account/project subtypes — INV-1 / INV-6).
#   POS (operator):   service_account:<op> (system_viewer@cluster) DOES see
#                     account:aX / project:pX — INV-2.
#   POS (owner):      user:u1 (owner@account:aX) still resolves viewer — INV-1
#                     owner→viewer cascade intact.
#
# Exits non-zero on any invariant violation. Requires docker + python3 + curl.
#
# Usage:
#   ./run-secl-model-conformance.sh [--port 18080]
#     [--model ../../../../kacho-proto/proto/kacho/cloud/iam/v1/fga_model.fga]
#     [--cli-image openfga/cli:v0.7.13] [--openfga-image openfga/openfga:latest]

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PORT=18080
MODEL="${HERE}/../../../../kacho-proto/proto/kacho/cloud/iam/v1/fga_model.fga"
CLI_IMAGE="openfga/cli:v0.7.13"
OPENFGA_IMAGE="openfga/openfga:latest"
CONTAINER="secl-fga-conformance"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --port) PORT="$2"; shift 2 ;;
    --model) MODEL="$2"; shift 2 ;;
    --cli-image) CLI_IMAGE="$2"; shift 2 ;;
    --openfga-image) OPENFGA_IMAGE="$2"; shift 2 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

[[ -f "$MODEL" ]] || { echo "canonical fga_model.fga not found: $MODEL" >&2; exit 2; }

BASE="http://localhost:${PORT}"
FAILS=0
expect() { # label actual expected
  if [[ "$2" == "$3" ]]; then echo "PASS  $1 (= $2)"; else echo "FAIL  $1 (got '$2', want '$3')"; FAILS=$((FAILS+1)); fi
}

cleanup() { docker rm -f "$CONTAINER" >/dev/null 2>&1 || true; }
trap cleanup EXIT

cleanup
docker run -d --name "$CONTAINER" -p "${PORT}:8080" "$OPENFGA_IMAGE" run >/dev/null
for _ in $(seq 1 30); do curl -sf "$BASE/healthz" >/dev/null 2>&1 && break; sleep 1; done
curl -sf "$BASE/healthz" >/dev/null || { echo "openfga did not become ready" >&2; exit 1; }

MODEL_JSON="$(docker run --rm -i "$CLI_IMAGE" model transform \
  --input-format fga --output-format json "$(cat "$MODEL")")"

STORE_ID="$(curl -s -X POST "$BASE/stores" -H 'content-type: application/json' \
  -d '{"name":"secl-conformance"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')"
MODEL_ID="$(curl -s -X POST "$BASE/stores/$STORE_ID/authorization-models" \
  -H 'content-type: application/json' -d "$MODEL_JSON" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["authorization_model_id"])')"

wt() { # user relation object
  curl -s -X POST "$BASE/stores/$STORE_ID/write" -H 'content-type: application/json' \
    -d "{\"authorization_model_id\":\"$MODEL_ID\",\"writes\":{\"tuple_keys\":[{\"user\":\"$1\",\"relation\":\"$2\",\"object\":\"$3\"}]}}" >/dev/null
}
chk() { # user relation object → True/False
  curl -s -X POST "$BASE/stores/$STORE_ID/check" -H 'content-type: application/json' \
    -d "{\"authorization_model_id\":\"$MODEL_ID\",\"tuple_key\":{\"user\":\"$1\",\"relation\":\"$2\",\"object\":\"$3\"}}" \
    | python3 -c 'import sys,json;print(json.load(sys.stdin).get("allowed",False))'
}
lso() { # user relation type → comma-joined objects
  curl -s -X POST "$BASE/stores/$STORE_ID/list-objects" -H 'content-type: application/json' \
    -d "{\"authorization_model_id\":\"$MODEL_ID\",\"user\":\"$1\",\"relation\":\"$2\",\"type\":\"$3\"}" \
    | python3 -c 'import sys,json;print(",".join(json.load(sys.stdin).get("objects",[])))'
}

# Seed: account aX + project pX both carry #cluster; operator system_viewer;
# owner u1; NO grant for user:rando.
#
# CRITICAL leak-realism: also seed the wildcard `user:*#viewer@cluster` tuple.
# This is realistic production state — cluster.viewer carries `user:*` for
# tenant-facing reference-data (Region/Zone), so this tuple legitimately exists.
# WITH it present, the NAIVE `viewer from cluster` cascade would resolve
# viewer@account:aX for EVERY authenticated user (mass over-exposure). The
# SEC-L `system_viewer from cluster` cascade must NOT — that is the leak-guard
# this proof exercises (INV-1 / INV-6). Without this tuple the leak is dormant
# and the test would not discriminate the defective design.
wt "user:*"                     "viewer" "cluster:cluster_kacho_root"
wt "cluster:cluster_kacho_root" "cluster" "account:aX"
wt "service_account:svaOP"      "system_viewer" "cluster:cluster_kacho_root"
wt "user:u1"                    "owner"  "account:aX"
wt "account:aX"                 "account" "project:pX"
wt "cluster:cluster_kacho_root" "cluster" "project:pX"

echo "=== ACCOUNT ==="
expect "NEG leak-guard Check(user:rando,viewer,account:aX)" "$(chk user:rando viewer account:aX)" "False"
expect "NEG leak-guard ListObjects(user:rando,viewer,account)" "$(lso user:rando viewer account)" ""
expect "POS operator Check(service_account:svaOP,viewer,account:aX)" "$(chk service_account:svaOP viewer account:aX)" "True"
expect "POS operator ListObjects(service_account:svaOP,viewer,account)" "$(lso service_account:svaOP viewer account)" "account:aX"
expect "POS owner Check(user:u1,viewer,account:aX)" "$(chk user:u1 viewer account:aX)" "True"
expect "POS owner ListObjects(user:u1,viewer,account)" "$(lso user:u1 viewer account)" "account:aX"

echo "=== PROJECT ==="
expect "NEG leak-guard Check(user:rando,viewer,project:pX)" "$(chk user:rando viewer project:pX)" "False"
expect "NEG leak-guard ListObjects(user:rando,viewer,project)" "$(lso user:rando viewer project)" ""
expect "POS operator Check(service_account:svaOP,viewer,project:pX)" "$(chk service_account:svaOP viewer project:pX)" "True"
expect "POS operator ListObjects(service_account:svaOP,viewer,project)" "$(lso service_account:svaOP viewer project)" "project:pX"
expect "POS owner Check(user:u1,viewer,project:pX)" "$(chk user:u1 viewer project:pX)" "True"
expect "POS owner ListObjects(user:u1,viewer,project)" "$(lso user:u1 viewer project)" "project:pX"

if [[ "$FAILS" -ne 0 ]]; then
  echo "SEC-L model conformance: ${FAILS} FAILED — over-exposure / cascade defect" >&2
  exit 1
fi
echo "SEC-L model conformance: all invariants hold (no user:* leak; operator sees all; owner intact)"
