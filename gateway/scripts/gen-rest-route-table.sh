#!/usr/bin/env bash

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

#
# gen-rest-route-table.sh — регенерация internal/middleware/rest_route_table_gen.go
# из аннотаций `option (google.api.http)` proto всех доменов Kachō.
#
# api-gateway импортирует proto-stubs всех доменов и потому является
# единственным местом, откуда виден полный набор REST-биндингов платформы.
# Таблица path->FQN собирается ЗДЕСЬ. Монорепо: все доменные .proto (iam / vpc /
# compute / geo / loadbalancer / registry / storage) и общая инфраструктура
# (operation / validation) живут в едином внутрирепозиторном дереве proto/ в
# корне репозитория.
#
# Что делает скрипт (то же дерево, что gen-permission-catalog.sh):
#   1. собирает единое buf-дерево во временном каталоге: всё доменное proto/ +
#      anchor-файл permissions_catalog_root.proto;
#   2. собирает плагин ./cmd/protoc-gen-kacho-rest-routes;
#   3. запускает `buf generate` со `strategy: all` — плагин получает ВЕСЬ образ
#      одним вызовом и эмитит rest_route_table_gen.go;
#   4. прогоняет gofmt и кладет результат в internal/middleware/.
#
# Требует полный checkout монорепо (внутрирепозиторное proto/) + buf — это
# dev/maintenance-инструмент, а не часть рантайма (рантайм использует уже вшитую
# таблицу). Идемпотентен: повторный прогон без изменений proto дает нулевой diff.
#
# Использование:
#   scripts/gen-rest-route-table.sh [OUTPUT_GO]
# По умолчанию OUTPUT_GO = internal/middleware/rest_route_table_gen.go.

set -euo pipefail

# REPO_ROOT = gateway/ (dir этого скрипта/..); MONOREPO_ROOT = корень монорепо.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MONOREPO_ROOT="$(cd "${REPO_ROOT}/.." && pwd)"
OUT="${1:-${REPO_ROOT}/internal/middleware/rest_route_table_gen.go}"

# Монорепо: единое внутрирепозиторное proto-дерево в корне (симметрично
# gen-permission-catalog.sh). Все домены читаются из одного PROTO_ROOT.
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

STAGE="$(mktemp -d)/routes-proto"
BIN="$(mktemp -d)"
trap 'rm -rf "${STAGE%/*}" "${BIN}"' EXIT

# Раскладка стадии — ОДНА на оба генератора края (см. lib/stage-proto-tree.sh).
# Здесь стояла её вторая рукописная копия; копии старели порознь, и заведение
# общего пакета роняло оба генератора по очереди одинаковым сообщением.
# shellcheck source=lib/stage-proto-tree.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/stage-proto-tree.sh"
stage_proto_tree "${PROTO_ROOT}" "${STAGE}" "rest-route-table"

# --- anchor-файл плагина (primary file) ---
mkdir -p "${STAGE}/kacho/iam/authz/catalog/v1"
cp "${REPO_ROOT}/proto/kacho/iam/authz/catalog/v1/permissions_catalog_root.proto" \
   "${STAGE}/kacho/iam/authz/catalog/v1/permissions_catalog_root.proto"

# --- сборка плагина ---
go -C "${REPO_ROOT}" build -o "${BIN}/protoc-gen-kacho-rest-routes" ./cmd/protoc-gen-kacho-rest-routes

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
  # замыкание -> пустая таблица).
  - local: protoc-gen-kacho-rest-routes
    out: out
    strategy: all
YAML

mkdir -p "${STAGE}/out"
( cd "${STAGE}" && PATH="${BIN}:${PATH}" buf generate )

# Плагин уже прогоняет go/format; повторный gofmt — дешевая страховка.
gofmt -w "${STAGE}/out/rest_route_table_gen.go"

mkdir -p "$(dirname "${OUT}")"
cp "${STAGE}/out/rest_route_table_gen.go" "${OUT}"

n="$(grep -c 'Method:' "${OUT}" || true)"
echo "OK: ${OUT} (${n} routes)"
