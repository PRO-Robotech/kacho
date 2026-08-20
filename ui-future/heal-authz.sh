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

# journal_write <user> <relation> <object> — кладёт кортеж СТРОКОЙ ЖУРНАЛА.
#
# ПОЧЕМУ НЕ ПРЯМО В ДВИЖОК, КАК БЫЛО. Состояние движка обязано быть свёрткой
# журнала kacho_iam.fga_outbox (миграция 0098): на этом стоит проекция
# relation_fact, а на ней — форма E. Кортеж, вписанный прямо в движок, в журнал
# не попадает НИКОГДА, поэтому своя БД остаётся беднее чужой — и инструмент,
# который зовут ЧИНИТЬ права, тихо углублял расхождение с каждым прогоном.
#
# Теперь строка ложится в журнал, а дальше её разносит дренаж: тем же путём
# кортеж попадает и в движок, и в проекцию. Цена — применение не мгновенно.
journal_write() {
  local user="$1" relation="$2" object="$3"

  # Значения приходят из наших же таблиц, но в SQL они уходят подстановкой,
  # поэтому словарь сужается явно: одинарная кавычка здесь означала бы
  # выполнение чужого запроса под правами починки.
  local v
  for v in "$user" "$relation" "$object"; do
    if [[ ! "$v" =~ ^[A-Za-z0-9_:*.-]+$ ]]; then
      echo "  ОТКАЗ: значение ${v} вне словаря идентификаторов — строка журнала не пишется" >&2
      return 1
    fi
  done

  psql_iam "INSERT INTO kacho_iam.fga_outbox (event_type, payload, created_at)
            VALUES ('fga.tuple.write',
                    jsonb_build_object('user', '${user}', 'relation', '${relation}',
                                       'object', '${object}'),
                    now());" >/dev/null
}

fga_check() {
  local body="$1"
  local url="${OPENFGA_URL}/stores/${STORE_ID}/check"

  # Ответ забирается подстановкой: `… | grep -q` под `pipefail` даёт ОТКАЗ НА
  # СОВПАДЕНИИ — wget получает SIGPIPE, и «доступ разрешён» читалось бы как
  # «запрещён» (задача #658).
  local resp
  resp="$(kubectl -n "$NAMESPACE" exec deploy/kacho-iam -- \
    wget -q -O - \
      --header 'content-type: application/json' \
      --post-data "$body" \
      "$url" 2>/dev/null || true)"
  [[ "$resp" =~ \"allowed\"[[:space:]]*:[[:space:]]*true ]]
}

# fga_ensure <user> <relation> <object> — кортеж берётся ТРОЙКОЙ, а не двумя
# готовыми телами запроса: прежде каждое место вызова повторяло одну и ту же
# тройку дважды, в двух JSON-ах, и разойтись они могли молча.
fga_ensure() {
  local user="$1" relation="$2" object="$3"
  local label="${object}#${relation}@${user}"

  if fga_check "{\"authorization_model_id\":\"${MODEL_ID}\",\"tuple_key\":{\"user\":\"${user}\",\"relation\":\"${relation}\",\"object\":\"${object}\"}}"; then
    echo "  exists: ${label}"
  elif journal_write "$user" "$relation" "$object"; then
    echo "  queued: ${label} (применит дренаж журнала)"
  else
    echo "  ОТКАЗ: ${label} не поставлен в журнал" >&2
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

  fga_ensure "cluster:${CLUSTER_ID}" "cluster" "account:${account_id}"

  fga_ensure "user:${user_id}" "owner" "account:${account_id}"

  fga_ensure "account:${account_id}" "account" "iam_user:${user_id}"

  fga_ensure "user:${user_id}" "subject" "iam_user:${user_id}"

  if [[ -n "$project_id" ]]; then
    fga_ensure "account:${account_id}" "account" "project:${project_id}"
  fi
done <<< "$rows"

echo
echo "Authz repair complete: недостающие кортежи поставлены в журнал "
echo "kacho_iam.fga_outbox; движок и проекция relation_fact догонят его дренажом."
