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

#
# ВХОДЫ ГЕНЕРАТОРА ОБЪЯВЛЕНЫ, А НЕ ВЫВЕДЕНЫ ИЗ РАСКЛАДКИ (заведено вместе с
# разрезом монорепо на отдельные продукты). Домен, вынесенный в свой репозиторий,
# не несёт ни дерева контрактов, ни исходников края: и то и другое приезжает к
# нему ВНЕШНЕЙ зависимостью (versioned Go-модуль). Пока генератор выводил корень
# контрактов из «каталог на уровень выше края», а плагин собирал из «./cmd рядом
# со скриптом», вынесенный домен не мог породить ни каталога прав, ни таблицы
# маршрутов — а без записи каталога средний слой отвечает «catalog: no entry for
# method», то есть отказом в доступе в рантайме.
#
# Ручки (у каждой умолчание воспроизводит прежнее поведение ПОБАЙТОВО):
#   KACHO_PROTO_ROOT      корень дерева контрактов   (умолч. <корень монорепо>/proto)
#   KACHO_CATALOG_ANCHOR  anchor-файл плагина        (умолч. <край>/proto/kacho/iam/authz/catalog/v1/permissions_catalog_root.proto)
#   KACHO_GEN_MODULE_DIR  каталог Go-модуля сборки   (умолч. <край>)
#   KACHO_GEN_PLUGIN_PKG  пакет плагина              (умолч. ./cmd/<плагин>)
#   KACHO_GEN_DOMAINS     отбор доменов через пробел (умолч. пусто = все домены дерева)
#
# ОТБОР СУЖАЕТ ЭМИССИЮ, А НЕ ФОРМУ ДЕРЕВА. Дерево контрактов приезжает целиком,
# урезать его потребитель не вправе; поэтому выход сужает объявленный перечень
# доменов, а недостающие для компиляции деревья добираются ЗАМЫКАНИЕМ импортов
# (см. lib/stage-proto-tree.sh). Домен, чьи RPC попали в выход мимо отбора,
# объявляется НАХОДКОЙ, а не отбрасывается молча: молчаливое отбрасывание дало бы
# каталог без записи для службы, которую сервис в действительности поднимает, —
# то есть ровно тот отказ в доступе, ради предотвращения которого отбор и заведён.

set -euo pipefail

# REPO_ROOT = gateway/ (dir этого скрипта/..); MONOREPO_ROOT = корень монорепо.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MONOREPO_ROOT="$(cd "${REPO_ROOT}/.." && pwd)"
OUT="${1:-${REPO_ROOT}/internal/middleware/rest_route_table_gen.go}"

PROTO_ROOT="${KACHO_PROTO_ROOT:-${MONOREPO_ROOT}/proto}"
ANCHOR="${KACHO_CATALOG_ANCHOR:-${REPO_ROOT}/proto/kacho/iam/authz/catalog/v1/permissions_catalog_root.proto}"
GEN_MODULE_DIR="${KACHO_GEN_MODULE_DIR:-${REPO_ROOT}}"
GEN_PLUGIN_PKG="${KACHO_GEN_PLUGIN_PKG:-./cmd/protoc-gen-kacho-rest-routes}"
GEN_DOMAINS="${KACHO_GEN_DOMAINS:-}"


if [[ ! -d "${PROTO_ROOT}" ]]; then
  echo "ERR: proto-дерево не найдено: ${PROTO_ROOT}" >&2
  echo "Ожидается корень дерева контрактов: внутрирепозиторное proto/ монорепо" >&2
  echo "либо каталог, названный ручкой KACHO_PROTO_ROOT (внешняя зависимость)." >&2
  exit 1
fi
if [[ ! -f "${ANCHOR}" ]]; then
  echo "ERR: anchor-файл не найден: ${ANCHOR}" >&2
  exit 1
fi
if [[ ! -d "${GEN_MODULE_DIR}" ]]; then
  echo "ERR: каталога сборки плагина не существует: ${GEN_MODULE_DIR}" >&2
  exit 1
fi
# Модуль ищется подъёмом вверх (край лежит внутри модуля монорепо, своего go.mod
# у него нет) — поэтому спрашиваем сам go, а не наличие файла рядом.
gen_gomod="$(go -C "${GEN_MODULE_DIR}" env GOMOD 2>/dev/null || true)"
if [[ -z "${gen_gomod}" || "${gen_gomod}" == "/dev/null" ]]; then
  echo "ERR: из ${GEN_MODULE_DIR} не резолвится ни один Go-модуль —" >&2
  echo "     пакет плагина ${GEN_PLUGIN_PKG} собрать неоткуда." >&2
  exit 1
fi
command -v buf >/dev/null || { echo "ERR: buf не установлен" >&2; exit 1; }

# Перепись входов: вердикт обязан называть, ЧТО он читал, — иначе «порождено»
# неотличимо от «порождено не из того дерева».
echo "корень контрактов: ${PROTO_ROOT}"
echo "anchor: ${ANCHOR}"
echo "модуль плагина: ${GEN_MODULE_DIR} (go.mod: ${gen_gomod})"
echo "пакет плагина: ${GEN_PLUGIN_PKG}"
echo "отбор доменов: ${GEN_DOMAINS:-(все домены дерева)}"

STAGE="$(mktemp -d)/routes-proto"
BIN="$(mktemp -d)"
trap 'rm -rf "${STAGE%/*}" "${BIN}"' EXIT

# Раскладка стадии — ОДНА на оба генератора края (см. lib/stage-proto-tree.sh).
# Здесь стояла её вторая рукописная копия; копии старели порознь, и заведение
# общего пакета роняло оба генератора по очереди одинаковым сообщением.
# shellcheck source=lib/stage-proto-tree.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/stage-proto-tree.sh"
stage_proto_tree "${PROTO_ROOT}" "${STAGE}" "rest-route-table" "${GEN_DOMAINS}"

# --- anchor-файл плагина (primary file) ---
mkdir -p "${STAGE}/kacho/iam/authz/catalog/v1"
cp "${ANCHOR}" "${STAGE}/kacho/iam/authz/catalog/v1/permissions_catalog_root.proto"

# --- сборка плагина ---
go -C "${GEN_MODULE_DIR}" build -o "${BIN}/protoc-gen-kacho-rest-routes" "${GEN_PLUGIN_PKG}"

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


# --- эмитированный домен обязан быть ОБЪЯВЛЕН ---
#
# Замыкание импортов добирает деревья, нужные для компиляции; часть из них несёт
# собственные RPC. Отбросить их молча нельзя — сервис, поднимающий такую службу,
# получил бы каталог без её записи, то есть отказ в доступе в рантайме. Поэтому
# расхождение объявляется НАХОДКОЙ и называет домен поимённо.
if [[ -n "${GEN_DOMAINS// /}" ]]; then
  undeclared=""
  emitted=0
  while IFS= read -r emitted_domain; do
    [[ -n "${emitted_domain}" ]] || continue
    emitted=$((emitted + 1))
    declared=0
    for sel in ${GEN_DOMAINS}; do
      [[ "${emitted_domain}" == "${sel}" ]] && declared=1
    done
    [[ "${declared}" -eq 1 ]] || undeclared="${undeclared} ${emitted_domain}"
  done < <(sed -n 's/.*FQN: "kacho\.cloud\.\([a-z0-9_]*\)\..*/\1/p' "${OUT}" | sort -u)
  echo "эмитировано доменов: ${emitted}"
  if [[ -n "${undeclared}" ]]; then
    echo "ERR: эмитированы домены вне отбора:${undeclared}" >&2
    echo "     Их RPC попали в выход замыканием импортов. Объявите их в" >&2
    echo "     KACHO_GEN_DOMAINS, если сервис их поднимает, либо снимите из" >&2
    echo "     отбора домен, который их тянет." >&2
    exit 1
  fi
fi

n="$(grep -c 'Method:' "${OUT}" || true)"
echo "OK: ${OUT} (${n} routes)"
