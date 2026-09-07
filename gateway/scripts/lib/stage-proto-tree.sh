# shellcheck shell=bash
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
# РЕШЕНИЕ. Перечень ВЫВОДИТСЯ из дерева: копируется всё под объявленными корнями
# контракта, кроме явно исключённого. Список исключений — один, здесь, и он
# проверяется на наличие предмета: запись, которой больше нечего исключать, —
# находка, а не безобидный остаток (иначе она унаследует следующую слепую зону).
#
# КОРНЕЙ КОНТРАКТА ДВА, и отбор популяции идёт по ОБЪЯВЛЕННОМУ множеству, а не по
# литералу приставки (KAN-PKG-1). Платформа называет себя `kacho`, служба доступа
# — `kaname`. Литерал оставил бы дерево службы вне обхода, и оба генератора
# честно напечатали бы «скопировано N» по опустевшей популяции: каталог вышел бы
# БЕЗ записей службы, то есть с отказом в доступе на всей её поверхности, а
# прогон остался бы зелёным. Поэтому корень — не приставка в коде, а запись
# перечня, и домен резолвится в свой корень обходом дерева.
#
# ОТБОР ДОМЕНОВ (заведён вместе с разрезом на отдельные продукты). Домен,
# вынесенный в свой репозиторий, получает дерево контрактов ВНЕШНЕЙ зависимостью,
# то есть ЦЕЛИКОМ — урезать его он не вправе. Значит сузить выход обязан отбор, а
# не форма дерева. Отбор — четвёртый аргумент, строка имён через пробел; пусто
# означает «все домены дерева» и воспроизводит прежнее поведение побайтово.
#
# ЗАМЫКАНИЕ ИМПОРТОВ ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ. Домен не компилируется в
# одиночку: `kaname/cloud/iam` импортирует `kacho/cloud/operation` и
# `kacho/cloud/api` — то есть замыкание пересекает границу корней. Рукописный
# перечень «что ещё нужно iam» был бы ровно той
# второй копией, ради снятия которой этот файл и заведён, — поэтому недостающие
# деревья добираются обходом `import`-строк до неподвижной точки.
#
# Использование:
#   source "$(dirname "$0")/lib/stage-proto-tree.sh"
#   stage_proto_tree "${PROTO_ROOT}" "${STAGE}" "<имя-генератора>" ["<домен> <домен> …"]

# KACHO_PROTO_ROOTS — объявленное закрытое множество корней дерева контрактов.
# Пусто означало бы «ни одного корня», то есть пустую стадию, и обход ниже роняет
# прогон на нуле скопированных деревьев.
KACHO_PROTO_ROOTS=(kacho kaname)

# kacho_proto_tree_root — корень, под которым лежит дерево домена. Резолвится
# ОБХОДОМ объявленных корней, а не выводится из имени: имя домена о своём корне
# ничего не сообщает, и вывод по имени был бы второй копией этого перечня.
kacho_proto_tree_root() {
  local proto_root=$1 tree=$2 root
  for root in "${KACHO_PROTO_ROOTS[@]}"; do
    if [[ -d "${proto_root}/${root}/cloud/${tree}" ]]; then
      echo "${root}"
      return 0
    fi
  done
  return 1
}

# Каталоги под корнями контракта, которые в стадию НЕ идут.
#
# `apigateway` — служебный сервис самого края: он не входит ни в публичный, ни в
# доменный каталог прав и не выставляет tenant-facing REST-маршрутов, поэтому его
# присутствие в стадии добавило бы в оба выхода записи, которых там быть не должно.
KACHO_PROTO_STAGE_EXCLUDE=()

# kacho_proto_tree_imports — деревья под объявленными корнями, на которые
# ссылаются `import`-строки файлов одного дерева. Читается синтаксис объявления
# импорта, а не любое вхождение пути: путь встречается и в комментариях.
#
# Образец собирается ИЗ ПЕРЕЧНЯ корней, а не пишется литералом: импорт через
# границу корней законен и существует (контракт службы доступа импортирует
# `kacho/cloud/operation`), и корень, о котором образец не знает, дал бы
# молчание, неотличимое от отсутствия импорта.
kacho_proto_tree_imports() {
  local proto_root=$1 tree=$2 root alt
  root="$(kacho_proto_tree_root "${proto_root}" "${tree}")" || return 0
  alt="$(IFS='|'; echo "${KACHO_PROTO_ROOTS[*]}")"
  grep -rhE "^[[:space:]]*import[[:space:]]+(public[[:space:]]+)?\"(${alt})/cloud/[^\"]+\"[[:space:]]*;" \
    "${proto_root}/${root}/cloud/${tree}" 2>/dev/null \
    | sed -E "s|.*\"(${alt})/cloud/([^/\"]+)/.*|\\2|" | sort -u
}

stage_proto_tree() {
  local proto_root=$1
  local stage=$2
  local caller=${3:-stage-proto-tree}
  local selection_raw=${4:-}

  local _root
  for _root in "${KACHO_PROTO_ROOTS[@]}"; do
    mkdir -p "${stage}/${_root}/cloud"
  done
  mkdir -p "${stage}/kacho/iam/authz"

  # --- общая инфраструктура ---
  #
  # Здесь стояла ТРЕТЬЯ строка — безусловное копирование `kacho/cloud/validation.proto`,
  # объявлявшего семейство ограничений полей. Файл снят вместе с семейством (kacho#1255):
  # исполнителя на пути запроса у него не было ни одного. Строка была БЕЗУСЛОВНОЙ, а оба
  # зовущих генератора идут под `set -euo pipefail`, поэтому её пропуск ронял их обоих на
  # `cp: cannot stat`, то есть раньше, чем они успевали произвести хоть что-нибудь. Ни
  # `buf breaking`, ни перепись читателей на Go этого пути не видят — он живой и
  # единственный такой.
  cp -R "${proto_root}/google"             "${stage}/google"
  cp -R "${proto_root}/kacho/iam/authz/v1" "${stage}/kacho/iam/authz/v1"

  # --- исключения обязаны иметь предмет ---
  local excluded
  for excluded in "${KACHO_PROTO_STAGE_EXCLUDE[@]}"; do
    if ! kacho_proto_tree_root "${proto_root}" "${excluded}" >/dev/null; then
      echo "ERR: ${caller}: исключение '${excluded}' не имеет предмета —" >&2
      echo "     дерева '${excluded}' нет ни под одним объявленным корнем" >&2
      echo "     (${KACHO_PROTO_ROOTS[*]})." >&2
      echo "     Запись, которой больше нечего исключать, — находка: снимите её" >&2
      echo "     из KACHO_PROTO_STAGE_EXCLUDE, иначе она молча унаследует" >&2
      echo "     следующую слепую зону." >&2
      return 1
    fi
  done

  # --- перечень деревьев дерева, выведенный из дерева ---
  local tree name skip root
  local -a present=()
  for root in "${KACHO_PROTO_ROOTS[@]}"; do
    for tree in "${proto_root}/${root}"/cloud/*/; do
      [[ -d "${tree}" ]] || continue
      name="$(basename "${tree}")"
      skip=0
      for excluded in "${KACHO_PROTO_STAGE_EXCLUDE[@]}"; do
        [[ "${name}" == "${excluded}" ]] && skip=1
      done
      [[ "${skip}" -eq 1 ]] && continue
      present+=("${name}")
    done
  done

  # --- отбор ---
  local -a selected=()
  if [[ -z "${selection_raw// /}" ]]; then
    selected=("${present[@]}")
  else
    read -r -a selected <<< "${selection_raw}"
    for name in "${selected[@]}"; do
      if ! kacho_proto_tree_root "${proto_root}" "${name}" >/dev/null; then
        echo "ERR: ${caller}: выбранного домена '${name}' нет в дереве контрактов" >&2
        echo "     ни под одним объявленным корнем (${KACHO_PROTO_ROOTS[*]}) —" >&2
        echo "     отбор назвал то, чего нет." >&2
        return 1
      fi
    done
  fi

  # --- замыкание импортов до неподвижной точки ---
  local -A in_closure=()
  local -a queue=() closure_order=()
  for name in "${selected[@]}"; do
    [[ -n "${in_closure[${name}]:-}" ]] && continue
    in_closure[${name}]=1
    closure_order+=("${name}")
    queue+=("${name}")
  done
  local -a added_by_closure=()
  local head dep
  while [[ ${#queue[@]} -gt 0 ]]; do
    head="${queue[0]}"
    queue=("${queue[@]:1}")
    while IFS= read -r dep; do
      [[ -n "${dep}" ]] || continue
      [[ -n "${in_closure[${dep}]:-}" ]] && continue
      if ! kacho_proto_tree_root "${proto_root}" "${dep}" >/dev/null; then
        echo "ERR: ${caller}: '${head}' импортирует дерево '${dep}', а такого" >&2
        echo "     дерева в ${proto_root} нет ни под одним объявленным корнем" >&2
        echo "     (${KACHO_PROTO_ROOTS[*]}) — стадия собралась бы неполной и buf" >&2
        echo "     упал бы чужим сообщением компилятора." >&2
        return 1
      fi
      in_closure[${dep}]=1
      closure_order+=("${dep}")
      added_by_closure+=("${dep}")
      queue+=("${dep}")
    done < <(kacho_proto_tree_imports "${proto_root}" "${head}")
  done

  # --- копирование ---
  local copied=0
  local -A copied_per_root=()
  for name in "${closure_order[@]}"; do
    root="$(kacho_proto_tree_root "${proto_root}" "${name}")"
    cp -R "${proto_root}/${root}/cloud/${name}" "${stage}/${root}/cloud/${name}"
    copied=$((copied + 1))
    copied_per_root[${root}]=$(( ${copied_per_root[${root}]:-0} + 1 ))
  done

  # Перепись: «ноль находок» обязано быть отличимо от «ноль прочитанного».
  # Печатается число ПО КАЖДОМУ корню: одно суммарное число скрыло бы ровно тот
  # случай, ради которого перечень корней заведён, — корень, из которого не
  # скопировано ни одного дерева.
  local per_root_census="" _r
  for _r in "${KACHO_PROTO_ROOTS[@]}"; do
    per_root_census+=" ${_r}=${copied_per_root[${_r}]:-0}"
  done
  echo "${caller}: деревьев контракта скопировано ${copied} (по корням:${per_root_census})," \
       "исключено: ${KACHO_PROTO_STAGE_EXCLUDE[*]}"
  echo "${caller}: отбор доменов: ${selected[*]}" \
       "· добрано замыканием импортов: ${added_by_closure[*]:-(нечего)}"

  if [[ "${copied}" -eq 0 ]]; then
    echo "ERR: ${caller}: под корнями (${KACHO_PROTO_ROOTS[*]}) не скопировано ни" >&2
    echo "     одного дерева — выход собрался бы из пустоты и вышел бы успехом." >&2
    return 1
  fi
}
