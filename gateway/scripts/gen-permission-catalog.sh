#!/usr/bin/env bash

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

#
# gen-permission-catalog.sh — регенерация permission_catalog.json из proto всех
# доменов Kachō.
#
# api-gateway импортирует proto-stubs всех доменов и потому является
# единственным местом, откуда виден полный набор RPC платформы. Каталог
# permissions собирается ЗДЕСЬ. Монорепо: все доменные .proto (iam / vpc /
# compute / geo / loadbalancer / registry / storage) и общая инфраструктура
# (operation / validation / authz_options) живут в едином внутрирепозиторном
# дереве proto/ в корне репозитория.
#
# Что делает скрипт:
#   1. собирает единое buf-дерево во временном каталоге: всё доменное proto/ +
#      anchor-файл gateway/proto/kacho/iam/authz/catalog/v1/permissions_catalog_root.proto;
#   2. собирает плагин ./cmd/protoc-gen-kacho-permissions;
#   3. запускает `buf generate` со `strategy: all` — плагин получает ВЕСЬ образ
#      одним вызовом и эмитит permission_catalog.json (+ warnings-файл, если
#      какой-то RPC не аннотирован).
#
# Требует полный checkout монорепо (внутрирепозиторное proto/) + buf — это
# dev/maintenance-инструмент, а не часть рантайма (рантайм использует уже
# вшитый internal/middleware/embed/permission_catalog.json).
#
# Использование:
#   scripts/gen-permission-catalog.sh [OUTPUT_JSON]
# По умолчанию OUTPUT_JSON = build/permission_catalog.json (каталог не
# перезаписывает вшитый embed — это делает `make permission-catalog-apply`).

set -euo pipefail

# REPO_ROOT = gateway/ (dir этого скрипта/..); MONOREPO_ROOT = корень монорепо.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MONOREPO_ROOT="$(cd "${REPO_ROOT}/.." && pwd)"
OUT="${1:-${REPO_ROOT}/build/permission_catalog.json}"

# Монорепо: единое внутрирепозиторное proto-дерево в корне (домены больше не
# разнесены по sibling-репозиториям). Все домены читаются из одного PROTO_ROOT.
PROTO_ROOT="${MONOREPO_ROOT}/proto"

if [[ ! -d "${PROTO_ROOT}" ]]; then
  echo "ERR: proto-дерево не найдено: ${PROTO_ROOT}" >&2
  echo "Ожидается внутрирепозиторное дерево proto/ в корне монорепо." >&2
  exit 1
fi
if [[ ! -f "${REPO_ROOT}/proto/kacho/iam/authz/catalog/v1/permissions_catalog_root.proto" ]]; then
  echo "ERR: anchor-файл не найден: ${REPO_ROOT}/proto/kacho/iam/authz/catalog/v1/permissions_catalog_root.proto" >&2
  exit 1
fi
command -v buf >/dev/null || { echo "ERR: buf не установлен" >&2; exit 1; }

STAGE="$(mktemp -d)/catalog-proto"
BIN="$(mktemp -d)"
trap 'rm -rf "${STAGE%/*}" "${BIN}"' EXIT
mkdir -p "${STAGE}/kacho/cloud" "${STAGE}/kacho/iam/authz"

# --- общая инфраструктура (БЕЗ apigateway — у gateway свой служебный сервис,
#     не входящий в публичный/доменный каталог) ---
cp -R "${PROTO_ROOT}/google"                       "${STAGE}/google"
cp -R "${PROTO_ROOT}/kacho/cloud/operation"        "${STAGE}/kacho/cloud/operation"
cp    "${PROTO_ROOT}/kacho/cloud/validation.proto" "${STAGE}/kacho/cloud/validation.proto"
cp -R "${PROTO_ROOT}/kacho/cloud/api"              "${STAGE}/kacho/cloud/api"
cp -R "${PROTO_ROOT}/kacho/iam/authz/v1"           "${STAGE}/kacho/iam/authz/v1"

# --- доменные деревья (монорепо: единый proto/) ---
cp -R "${PROTO_ROOT}/kacho/cloud/iam"             "${STAGE}/kacho/cloud/iam"
cp -R "${PROTO_ROOT}/kacho/cloud/vpc"             "${STAGE}/kacho/cloud/vpc"
cp -R "${PROTO_ROOT}/kacho/cloud/reference"       "${STAGE}/kacho/cloud/reference"
cp -R "${PROTO_ROOT}/kacho/cloud/compute"         "${STAGE}/kacho/cloud/compute"
cp -R "${PROTO_ROOT}/kacho/cloud/access"          "${STAGE}/kacho/cloud/access"
cp -R "${PROTO_ROOT}/kacho/cloud/maintenance"     "${STAGE}/kacho/cloud/maintenance"
cp -R "${PROTO_ROOT}/kacho/cloud/geo"             "${STAGE}/kacho/cloud/geo"
cp -R "${PROTO_ROOT}/kacho/cloud/loadbalancer"    "${STAGE}/kacho/cloud/loadbalancer"
cp -R "${PROTO_ROOT}/kacho/cloud/registry"        "${STAGE}/kacho/cloud/registry"
cp -R "${PROTO_ROOT}/kacho/cloud/storage"         "${STAGE}/kacho/cloud/storage"

# --- anchor-файл плагина (primary file) ---
mkdir -p "${STAGE}/kacho/iam/authz/catalog/v1"
cp "${REPO_ROOT}/proto/kacho/iam/authz/catalog/v1/permissions_catalog_root.proto" \
   "${STAGE}/kacho/iam/authz/catalog/v1/permissions_catalog_root.proto"

# --- сборка плагина ---
go -C "${REPO_ROOT}" build -o "${BIN}/protoc-gen-kacho-permissions" ./cmd/protoc-gen-kacho-permissions

# --- buf-конфиг во временном дереве ---
cat > "${STAGE}/buf.yaml" <<'YAML'
version: v2
modules:
  - path: .
YAML
cat > "${STAGE}/buf.gen.yaml" <<'YAML'
version: v2
plugins:
  # strategy: all — плагину подается ВЕСЬ образ одним вызовом (иначе buf по
  # умолчанию дробит генерацию по директориям и primary-файл получает пустое
  # замыкание → пустой каталог).
  - local: protoc-gen-kacho-permissions
    out: out
    strategy: all
YAML

mkdir -p "${STAGE}/out"
( cd "${STAGE}" && PATH="${BIN}:${PATH}" buf generate )

mkdir -p "$(dirname "${OUT}")"
cp "${STAGE}/out/permission_catalog.json" "${OUT}"
if [[ -f "${STAGE}/out/permission_catalog_warnings.txt" ]]; then
  cp "${STAGE}/out/permission_catalog_warnings.txt" "$(dirname "${OUT}")/permission_catalog_warnings.txt"
  echo "WARN: часть RPC без аннотаций — см. $(dirname "${OUT}")/permission_catalog_warnings.txt" >&2
fi

n="$(grep -c '"fqn"' "${OUT}" || true)"
echo "OK: ${OUT} (${n} entries)"
