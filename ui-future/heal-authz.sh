#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
set -euo pipefail

NAMESPACE="${KACHO_NAMESPACE:-kacho}"
DEPLOY_DIR="${KACHO_DEPLOY_DIR:-}"
PG_IAM_POD="${KACHO_PG_IAM_POD:-kacho-umbrella-pg-iam-0}"
PG_IAM_USER="${KACHO_PG_IAM_USER:-iam}"
PG_IAM_DB="${KACHO_PG_IAM_DB:-kacho_iam}"
# No hard-coded credential default: the kacho_iam Postgres password must be
# supplied explicitly so this script can never silently authenticate with a
# baked-in dev password against a non-dev cluster.
PG_IAM_PASSWORD="${KACHO_PG_IAM_PASSWORD:?KACHO_PG_IAM_PASSWORD must be set (export the kacho_iam Postgres password before running heal-authz.sh)}"
OPENFGA_URL="${KACHO_OPENFGA_URL:-http://kacho-umbrella-openfga:8080}"
OPENFGA_STORE_SECRET="${KACHO_OPENFGA_STORE_SECRET:-kacho-iam-openfga-store}"
OPENFGA_MODEL_SECRET="${KACHO_OPENFGA_MODEL_SECRET:-openfga-model-id}"
CLUSTER_ID="${KACHO_CLUSTER_ID:-cluster_kacho_root}"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
if [[ -z "$DEPLOY_DIR" ]]; then
  DEPLOY_DIR="$(cd -- "$SCRIPT_DIR/../kacho-deploy" && pwd)"
fi

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "ERROR: required command not found: $1" >&2
    exit 1
  fi
}

psql_iam() {
  kubectl -n "$NAMESPACE" exec "$PG_IAM_POD" -- \
    env PGPASSWORD="$PG_IAM_PASSWORD" \
    psql -U "$PG_IAM_USER" -d "$PG_IAM_DB" -Atc "$1"
}

secret_value() {
  local secret="$1"
  local key="$2"
  kubectl -n "$NAMESPACE" get secret "$secret" \
    -o "jsonpath={.data.${key}}" | base64 -d
}

fga_write() {
  local label="$1"
  local body="$2"
  local url="${OPENFGA_URL}/stores/${STORE_ID}/write"

  if kubectl -n "$NAMESPACE" exec deploy/kacho-iam -- \
    wget -q -O - \
      --header 'content-type: application/json' \
      --post-data "$body" \
      "$url" >/dev/null 2>&1; then
    echo "  ok: ${label}"
  else
    # OpenFGA may reject already-present tuples depending on server version.
    # Continue so the script remains safe to rerun as a dev repair tool.
    echo "  warn: ${label} write failed or already exists"
  fi
}

fga_check() {
  local body="$1"
  local url="${OPENFGA_URL}/stores/${STORE_ID}/check"

  kubectl -n "$NAMESPACE" exec deploy/kacho-iam -- \
    wget -q -O - \
      --header 'content-type: application/json' \
      --post-data "$body" \
      "$url" 2>/dev/null | grep -q '"allowed"[[:space:]]*:[[:space:]]*true'
}

fga_ensure() {
  local label="$1"
  local check_body="$2"
  local write_body="$3"

  if fga_check "$check_body"; then
    echo "  exists: ${label}"
  else
    fga_write "$label" "$write_body"
  fi
}

require_cmd kubectl
require_cmd make

echo "Using namespace: ${NAMESPACE}"
echo "Using deploy dir: ${DEPLOY_DIR}"
echo

echo "Checking cluster access..."
kubectl -n "$NAMESPACE" get pod "$PG_IAM_POD" >/dev/null
kubectl -n "$NAMESPACE" get deploy kacho-iam >/dev/null

echo
echo "Re-running OpenFGA bootstrap..."
(
  cd "$DEPLOY_DIR"
  make fga-bootstrap
)

echo
echo "Waiting for authz consumers to roll out..."
for deploy in kacho-iam api-gateway vpc compute; do
  if kubectl -n "$NAMESPACE" get deploy "$deploy" >/dev/null 2>&1; then
    kubectl -n "$NAMESPACE" rollout status "deploy/${deploy}" --timeout=120s
  fi
done

STORE_ID="$(secret_value "$OPENFGA_STORE_SECRET" store_id)"
MODEL_ID="$(secret_value "$OPENFGA_MODEL_SECRET" current)"

echo
echo "OpenFGA store: ${STORE_ID}"
echo "OpenFGA model: ${MODEL_ID}"

echo
echo "Replaying IAM hierarchy tuples..."
rows="$(psql_iam "
  select
    u.id || '|' || a.id || '|' || p.id
  from kacho_iam.users u
  join kacho_iam.accounts a on a.owner_user_id = u.id
  left join kacho_iam.projects p on p.account_id = a.id
  order by u.created_at, a.created_at, p.created_at
")"

if [[ -z "$rows" ]]; then
  echo "No IAM users with owned accounts found. Nothing to repair."
  exit 0
fi

while IFS='|' read -r user_id account_id project_id; do
  [[ -n "$user_id" && -n "$account_id" ]] || continue

  echo "Repairing user=${user_id} account=${account_id}${project_id:+ project=${project_id}}"

  fga_ensure "account:${account_id}#cluster@cluster:${CLUSTER_ID}" \
    "{\"authorization_model_id\":\"${MODEL_ID}\",\"tuple_key\":{\"user\":\"cluster:${CLUSTER_ID}\",\"relation\":\"cluster\",\"object\":\"account:${account_id}\"}}" \
    "{\"authorization_model_id\":\"${MODEL_ID}\",\"writes\":{\"tuple_keys\":[{\"user\":\"cluster:${CLUSTER_ID}\",\"relation\":\"cluster\",\"object\":\"account:${account_id}\"}]}}"

  fga_ensure "account:${account_id}#owner@user:${user_id}" \
    "{\"authorization_model_id\":\"${MODEL_ID}\",\"tuple_key\":{\"user\":\"user:${user_id}\",\"relation\":\"owner\",\"object\":\"account:${account_id}\"}}" \
    "{\"authorization_model_id\":\"${MODEL_ID}\",\"writes\":{\"tuple_keys\":[{\"user\":\"user:${user_id}\",\"relation\":\"owner\",\"object\":\"account:${account_id}\"}]}}"

  fga_ensure "iam_user:${user_id}#account@account:${account_id}" \
    "{\"authorization_model_id\":\"${MODEL_ID}\",\"tuple_key\":{\"user\":\"account:${account_id}\",\"relation\":\"account\",\"object\":\"iam_user:${user_id}\"}}" \
    "{\"authorization_model_id\":\"${MODEL_ID}\",\"writes\":{\"tuple_keys\":[{\"user\":\"account:${account_id}\",\"relation\":\"account\",\"object\":\"iam_user:${user_id}\"}]}}"

  fga_ensure "iam_user:${user_id}#subject@user:${user_id}" \
    "{\"authorization_model_id\":\"${MODEL_ID}\",\"tuple_key\":{\"user\":\"user:${user_id}\",\"relation\":\"subject\",\"object\":\"iam_user:${user_id}\"}}" \
    "{\"authorization_model_id\":\"${MODEL_ID}\",\"writes\":{\"tuple_keys\":[{\"user\":\"user:${user_id}\",\"relation\":\"subject\",\"object\":\"iam_user:${user_id}\"}]}}"

  if [[ -n "$project_id" ]]; then
    fga_ensure "project:${project_id}#account@account:${account_id}" \
      "{\"authorization_model_id\":\"${MODEL_ID}\",\"tuple_key\":{\"user\":\"account:${account_id}\",\"relation\":\"account\",\"object\":\"project:${project_id}\"}}" \
      "{\"authorization_model_id\":\"${MODEL_ID}\",\"writes\":{\"tuple_keys\":[{\"user\":\"account:${account_id}\",\"relation\":\"account\",\"object\":\"project:${project_id}\"}]}}"
  fi
done <<< "$rows"

echo
echo "Authz repair complete."
