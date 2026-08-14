# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# stage-proto-tree.sh — сборка единого buf-дерева для генераторов края.
#
# ПРЕДМЕТ. Два генератора (`gen-permission-catalog.sh`, `gen-rest-route-table.sh`)
# делают одно и то же первым шагом: раскладывают доменные `.proto` во временный
# каталог, чтобы скормить их buf одним модулем. Раскладка у них была ОДИНАКОВОЙ и
# лежала ДВУМЯ рукописными копиями — пятнадцать строк `cp -R` в каждой.
#
# ЧЕМ ЭТО ПЛОХО, ИЗМЕРЕННО. Копии стареют не вместе. Пакет, заведённый в `proto/`
# и не дописанный сюда, в стадию не попадает — и генератор падает не своим
# сообщением, а компилятором: «imported file does not exist», координатой чужого
# файла. Наблюдалось 2026-08-15 при заведении общего пакета `kacho/cloud/quota`:
# упали ОБА генератора, по очереди, одинаково — то есть чинить пришлось бы дважды
# и второй раз уже после того, как первый «починен».
#
# РЕШЕНИЕ. Перечень ВЫВОДИТСЯ из дерева: копируется всё под `kacho/cloud`, кроме
# явно исключённого. Список исключений — один, здесь, и он проверяется на наличие
# предмета: запись, которой больше нечего исключать, — находка, а не безобидный
# остаток (иначе она унаследует следующую слепую зону).
#
# Использование:
#   source "$(dirname "$0")/lib/stage-proto-tree.sh"
#   stage_proto_tree "${PROTO_ROOT}" "${STAGE}" "<имя-генератора-для-переписи>"

# Каталоги под `kacho/cloud`, которые в стадию НЕ идут.
#
# `apigateway` — служебный сервис самого края: он не входит ни в публичный, ни в
# доменный каталог прав и не выставляет tenant-facing REST-маршрутов, поэтому его
# присутствие в стадии добавило бы в оба выхода записи, которых там быть не должно.
KACHO_PROTO_STAGE_EXCLUDE=(apigateway)

stage_proto_tree() {
  local proto_root=$1
  local stage=$2
  local caller=${3:-stage-proto-tree}

  mkdir -p "${stage}/kacho/cloud" "${stage}/kacho/iam/authz"

  # --- общая инфраструктура ---
  cp -R "${proto_root}/google"                       "${stage}/google"
  cp    "${proto_root}/kacho/cloud/validation.proto" "${stage}/kacho/cloud/validation.proto"
  cp -R "${proto_root}/kacho/iam/authz/v1"           "${stage}/kacho/iam/authz/v1"

  # --- исключения обязаны иметь предмет ---
  local excluded
  for excluded in "${KACHO_PROTO_STAGE_EXCLUDE[@]}"; do
    if [[ ! -d "${proto_root}/kacho/cloud/${excluded}" ]]; then
      echo "ERR: ${caller}: исключение '${excluded}' не имеет предмета —" >&2
      echo "     каталога ${proto_root}/kacho/cloud/${excluded} нет." >&2
      echo "     Запись, которой больше нечего исключать, — находка: снимите её" >&2
      echo "     из KACHO_PROTO_STAGE_EXCLUDE, иначе она молча унаследует" >&2
      echo "     следующую слепую зону." >&2
      return 1
    fi
  done

  # --- всё остальное под kacho/cloud, выведенное из дерева ---
  local tree name skip copied=0
  for tree in "${proto_root}"/kacho/cloud/*/; do
    name="$(basename "${tree}")"
    skip=0
    for excluded in "${KACHO_PROTO_STAGE_EXCLUDE[@]}"; do
      [[ "${name}" == "${excluded}" ]] && skip=1
    done
    [[ "${skip}" -eq 1 ]] && continue
    cp -R "${tree%/}" "${stage}/kacho/cloud/${name}"
    copied=$((copied + 1))
  done

  # Перепись: «ноль находок» обязано быть отличимо от «ноль прочитанного».
  echo "${caller}: деревьев под kacho/cloud скопировано ${copied}," \
       "исключено: ${KACHO_PROTO_STAGE_EXCLUDE[*]}"

  if [[ "${copied}" -eq 0 ]]; then
    echo "ERR: ${caller}: под ${proto_root}/kacho/cloud не скопировано ни одного" >&2
    echo "     дерева — выход собрался бы из пустоты и вышел бы успехом." >&2
    return 1
  fi
}
