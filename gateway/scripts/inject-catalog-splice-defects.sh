#!/usr/bin/env bash

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

#
# inject-catalog-splice-defects.sh — доказательство, что гейт склейки
# (`check-catalog-splice.sh`, kacho#1110) СПОСОБЕН упасть на НАСТОЯЩЕЙ
# пропаже доли, а не только на своих внутренних самотестах (S6/S7 внутри
# самого гейта проверяют то же свойство симуляцией — здесь пропажа вносится
# в РЕАЛЬНОЕ дерево контрактов, ту же самую копию, на которой гейт читает
# вшитые артефакты).
#
# Доказывается не чтением, а возвратом дефекта: дерево контрактов лишается
# домена целиком (каталог временно переименовывается), гейт обязан
# ПОКРАСНЕТЬ и НАЗВАТЬ ПРИЧИНУ; законный близнец — дерево, тронутое, но не
# по предмету гейта (пустой домен без единого RPC), — обязан оставить гейт
# ЗЕЛЁНЫМ.
#
# Исходов ТРИ, и третий не вычитается из вердикта: 0 — доказано · 1 — гейт
# не упал на возвращённом дефекте либо упал на законной правке · 2 — прогон
# не выполнен (нет buf/go/python3, дерево грязное, правка не применилась).

set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GW="${ROOT}/gateway"
GATE="${GW}/scripts/check-catalog-splice.sh"
PROTO_ROOT="${ROOT}/proto"

[[ -f "${GATE}" ]] || { echo "ОТКАЗ: нет ${GATE} — предмета инъекции не существует" >&2; exit 2; }
command -v buf     >/dev/null || { echo "ОТКАЗ: buf не установлен — прогон не выполнен" >&2; exit 2; }
command -v go      >/dev/null || { echo "ОТКАЗ: go не установлен — прогон не выполнен"  >&2; exit 2; }
command -v python3 >/dev/null || { echo "ОТКАЗ: python3 не установлен — прогон не выполнен" >&2; exit 2; }

if [[ -n "$(git -C "${ROOT}" status --porcelain -- proto 2>/dev/null)" ]]; then
  echo "ОТКАЗ: дерево контрактов уже правлено — прогон не выполнен, восстановление" \
       "вернуло бы его к состоянию НА МОМЕНТ ЗАПУСКА, а не к HEAD" >&2
  exit 2
fi

fails=0
axes=0
LOG="$(mktemp)"
trap 'rm -f "${LOG}"' EXIT

# gate_axis <имя> <red|green> [<обязательная подстрока находки>]
gate_axis() {
  local name="$1" want="$2" needle="${3:-}" rc
  axes=$((axes + 1))
  ( cd "${GW}" && ./scripts/check-catalog-splice.sh ) >"${LOG}" 2>&1
  rc=$?
  if [[ "${rc}" -eq 2 ]]; then
    echo "  ✗ ${name}: гейт вышел БЕЗ ПРЕДМЕТА (2) — это не вердикт" >&2
    tail -5 "${LOG}" >&2
    fails=$((fails + 1))
    return
  fi
  if [[ "${want}" == red && "${rc}" -eq 0 ]]; then
    echo "  ✗ ${name}: дефект возвращён, а гейт ЗЕЛЁНЫЙ — он не способен упасть" >&2
    fails=$((fails + 1))
    return
  fi
  if [[ "${want}" == green && "${rc}" -ne 0 ]]; then
    echo "  ✗ ${name}: законная правка, а гейт КРАСНЫЙ — он ловит форму, а не существо" >&2
    grep 'НАХОДКА' "${LOG}" | head -5 >&2
    fails=$((fails + 1))
    return
  fi
  if [[ "${want}" == red && -n "${needle}" ]] && ! grep -qF "${needle}" "${LOG}"; then
    echo "  ✗ ${name}: гейт покраснел, но НЕ НАЗВАЛ координату «${needle}»" >&2
    grep 'НАХОДКА' "${LOG}" | head -5 >&2
    fails=$((fails + 1))
    return
  fi
  echo "  ✓ ${name}"
}

echo "== контроль: нетронутое дерево =="
gate_axis "нетронутое дерево — зелено" green

# ---------------------------------------------------------------------------
# Ось 1: снятие вклада СЛУЖБЫ — дерево kaname временно исчезает целиком.
# ---------------------------------------------------------------------------
KANAME_DIR="${PROTO_ROOT}/kaname"
KANAME_BAK="$(mktemp -d)/kaname"
restore_kaname() { [[ -d "${KANAME_BAK}" ]] && { rm -rf "${KANAME_DIR}"; mv "${KANAME_BAK}" "${KANAME_DIR}"; }; }
trap 'restore_kaname' EXIT

echo "== ось 1: дерево kaname снято целиком — доля службы пропадает =="
if [[ ! -d "${KANAME_DIR}" ]]; then
  echo "  ✗ ось 1: ${KANAME_DIR} не существует — предмета инъекции нет" >&2
  fails=$((fails + 1))
else
  mv "${KANAME_DIR}" "${KANAME_BAK}"
  gate_axis "kaname снят — гейт краснеет и называет причину" red \
    "под kaname/cloud не найдено ни одного домена"
  restore_kaname
  trap - EXIT
fi

# ---------------------------------------------------------------------------
# Ось 2: закон. близнец — новый пустой домен платформы, БЕЗ единого RPC.
#        Дерево тронуто (появился каталог), предмета гейта это не касается:
#        каталог без сервисов не эмитирует ни одной записи ни в одну долю.
# ---------------------------------------------------------------------------
TWIN_DIR="${PROTO_ROOT}/kacho/cloud/inject_twin_empty"
cleanup_twin() { rm -rf "${TWIN_DIR}"; }
trap 'cleanup_twin' EXIT

echo "== ось 2 (законный близнец): новый ПУСТОЙ домен платформы — гейт молчит =="
mkdir -p "${TWIN_DIR}/v1"
cat > "${TWIN_DIR}/v1/marker.proto" <<'PROTO'
syntax = "proto3";

package kacho.cloud.inject_twin_empty.v1;

// marker.proto — законный близнец инъекции гейта склейки: домен существует,
// файл разбирается, а RPC в нём нет ни одного. Каталог прав и таблица
// маршрутов эмитируются ТОЛЬКО из RPC-сервисов, поэтому пустой домен не
// меняет ни одну из двух долей — гейт обязан остаться зелёным.
message Marker {
  string note = 1;
}
PROTO
gate_axis "пустой домен платформы — гейт зелёный" green
cleanup_twin
trap - EXIT

# ---------------------------------------------------------------------------
# Ось 3: закон. близнец S7 — снятие ОДНОГО домена ПЛАТФОРМЫ целиком.
# ---------------------------------------------------------------------------
REGISTRY_DIR="${PROTO_ROOT}/kacho/cloud/registry"
REGISTRY_BAK="$(mktemp -d)/registry"
restore_registry() { [[ -d "${REGISTRY_BAK}" ]] && { rm -rf "${REGISTRY_DIR}"; mv "${REGISTRY_BAK}" "${REGISTRY_DIR}"; }; }
trap 'restore_registry' EXIT

echo "== ось 3: домен платформы 'registry' снят целиком — доля платформы теряет его =="
if [[ ! -d "${REGISTRY_DIR}" ]]; then
  echo "  ✗ ось 3: ${REGISTRY_DIR} не существует — предмета инъекции нет" >&2
  fails=$((fails + 1))
else
  mv "${REGISTRY_DIR}" "${REGISTRY_BAK}"
  gate_axis "registry снят — гейт краснеет и называет расхождение" red \
    "объединение долей каталога РАСХОДИТСЯ со вшитым"
  restore_registry
  trap - EXIT
fi

echo "== контроль после восстановления: дерево снова нетронуто =="
gate_axis "дерево восстановлено — зелено" green

if [[ -n "$(git -C "${ROOT}" status --porcelain -- proto 2>/dev/null)" ]]; then
  echo "ОТКАЗ: дерево контрактов НЕ восстановлено побайтово после инъекции" >&2
  git -C "${ROOT}" status --porcelain -- proto >&2
  fails=$((fails + 1))
fi

echo "inject-catalog-splice-defects: осей прогнано ${axes}, провалов ${fails}"
[[ "${fails}" -eq 0 ]] || exit 1
echo "inject-catalog-splice-defects: OK"
