#!/usr/bin/env bash

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

#
# check-domain-generation.sh — гейт разреза: порождение каталога прав и таблицы
# маршрутов ДЛЯ ОБЪЯВЛЕННОГО ПЕРЕЧНЯ ДОМЕНОВ выполнимо вне дерева монорепо и даёт
# ПОБАЙТОВО то же, что даёт порождение по всему дереву.
#
# ПРЕДМЕТ. Вынесенный в свой репозиторий домен (сегодня — iam) не несёт ни дерева
# контрактов, ни исходников края: и то и другое приезжает к нему внешней
# зависимостью. Если генераторы края умеют работать только от раскладки монорепо,
# вынесенный домен не породит ни своего каталога прав, ни своей таблицы
# маршрутов, — а без записи каталога средний слой отвечает «catalog: no entry for
# method», то есть отказом в доступе в рантайме.
#
# ЧТО ГЕЙТ УТВЕРЖДАЕТ (каждое — отдельным номером, чтобы находка называла ось):
#   A1  прогон по умолчанию (весь корень, отбора нет) даёт ПОБАЙТОВО вшитые
#       артефакты — разрез не сменил поведение продукта;
#   A2  урезанное дерево контрактов несёт выбранные домены и НЕ несёт невыбранных;
#   A3  генератор НАЗЫВАЕТ свои входы: корень контрактов, anchor, модуль сборки
#       плагина, пакет плагина, отбор доменов;
#   A4  прогон «внешняя зависимость» (полный корень + отбор, плагин из ОТДЕЛЬНОГО
#       Go-модуля) и прогон «урезанное дерево» дают ПОБАЙТОВО одинаковые выходы;
#   A5  каждая запись доменного выхода — побайтовый блок/строка полного выхода;
#   A6  доменный выход СТРОГО уже полного — иначе отбор ничего не сузил и
#       зелёное вырождено;
#   A7  каждый эмитированный домен ОБЪЯВЛЕН в отборе — замыкание импортов не
#       вправе добирать эмитирующий домен молча;
#   A8  обход непуст: записей > 0, маршрутов > 0;
#   A9  корень контрактов ЧИТАЕТСЯ: корень, в котором выбранного домена нет,
#       обязан быть отвергнут — A4 этого не различает (урезанное дерево есть
#       подмножество полного).
#   A10 распознаватель эмитированных доменов знает ВСЕ корни, чьи имена в выходе
#       есть: корень, чьи имена присутствуют, а домена не дал ни одного, —
#       слепая зона распознавателя, а не отсутствие домена. Независимая половина
#       сверки — чистый разбор первого сегмента имени, БЕЗ перечня корней:
#       обе стороны в одном разборе разошлись бы вместе и молча;
#   A11 стадия ЗАМКНУТА по импортам: каждый `import` файла стадии резолвится
#       внутри неё. A4 этого не заменяет — незамкнутая стадия роняет buf чужим
#       сообщением компилятора, и находка называет симптом вместо причины.
#
# Коды возврата: 0 — находок нет · 1 — находка · 2 — предмета нет (нет buf/go).
#
# Использование:
#   scripts/check-domain-generation.sh [ДОМЕН...]     # умолчание: iam operation quota

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MONOREPO_ROOT="$(cd "${REPO_ROOT}/.." && pwd)"
PROTO_ROOT="${MONOREPO_ROOT}/proto"
ANCHOR="${REPO_ROOT}/proto/kacho/iam/authz/catalog/v1/permissions_catalog_root.proto"

DOMAINS=("$@")
if [[ ${#DOMAINS[@]} -eq 0 ]]; then
  DOMAINS=(iam operation quota)
fi

command -v buf >/dev/null || { echo "БЕЗ ПРЕДМЕТА: buf не установлен — сверять нечего" >&2; exit 2; }
command -v go  >/dev/null || { echo "БЕЗ ПРЕДМЕТА: go не установлен — сверять нечего"  >&2; exit 2; }
[[ -d "${PROTO_ROOT}" ]] || { echo "БЕЗ ПРЕДМЕТА: нет дерева контрактов ${PROTO_ROOT}" >&2; exit 2; }

WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

findings=0
finding() { findings=$((findings + 1)); echo "НАХОДКА [$1]: $2" >&2; }

# --- перепись входов ------------------------------------------------------
echo "check-domain-generation: корень контрактов ${PROTO_ROOT}"
echo "check-domain-generation: отбор доменов: ${DOMAINS[*]}"

# ==========================================================================
# A1. Прогон по умолчанию == вшитые артефакты (побайтово).
# ==========================================================================
"${REPO_ROOT}/scripts/gen-permission-catalog.sh" "${WORK}/full_catalog.json" >"${WORK}/full_catalog.log" 2>&1 \
  || { cat "${WORK}/full_catalog.log" >&2; finding A1 "порождение полного каталога отказало"; }
"${REPO_ROOT}/scripts/gen-rest-route-table.sh"  "${WORK}/full_routes.go"    >"${WORK}/full_routes.log"  2>&1 \
  || { cat "${WORK}/full_routes.log" >&2; finding A1 "порождение полной таблицы маршрутов отказало"; }

cmp -s "${WORK}/full_catalog.json" "${REPO_ROOT}/internal/middleware/embed/permission_catalog.json" \
  || finding A1 "полный каталог разошёлся с вшитым internal/middleware/embed/permission_catalog.json"
cmp -s "${WORK}/full_routes.go" "${REPO_ROOT}/internal/middleware/rest_route_table_gen.go" \
  || finding A1 "полная таблица разошлась с вшитой internal/middleware/rest_route_table_gen.go"

full_entries="$(grep -c '"fqn"' "${WORK}/full_catalog.json" 2>/dev/null || true)"; full_entries="${full_entries:-0}"
full_routes="$(grep -c 'FQN:' "${WORK}/full_routes.go" 2>/dev/null || true)"; full_routes="${full_routes:-0}"
echo "check-domain-generation: полный выход — записей каталога ${full_entries}, маршрутов ${full_routes}"

# ==========================================================================
# A2. Урезанное дерево контрактов: только отбор + замыкание импортов.
#     Строится ТОЙ ЖЕ раскладкой, что кормит генератор (одна реализация), но
#     его свойство утверждается ЯВНО, а не подразумевается.
# ==========================================================================
STRIPPED="${WORK}/stripped-proto"
# shellcheck source=lib/stage-proto-tree.sh
source "${REPO_ROOT}/scripts/lib/stage-proto-tree.sh"
stage_proto_tree "${PROTO_ROOT}" "${STRIPPED}" "check-stripped-tree" "${DOMAINS[*]}" \
  || finding A2 "раскладка урезанного дерева отказала"

# Обход идёт по ОБЪЯВЛЕННЫМ корням контракта (KACHO_PROTO_ROOTS из библиотеки
# раскладки), а не по литералу `kacho/cloud`: корень, о котором обход не знает,
# дал бы «деревьев 0» по опустевшей популяции — то есть находку там, где
# нарушения нет, и молчание там, где дерево службы выпало из стадии.
staged=()
for r in "${KACHO_PROTO_ROOTS[@]}"; do
  [[ -d "${STRIPPED}/${r}/cloud" ]] || continue
  for d in "${STRIPPED}/${r}"/cloud/*/; do
    [[ -d "${d}" ]] || continue
    staged+=("$(basename "${d}")")
  done
done
echo "check-domain-generation: урезанное дерево несёт деревьев контракта: ${#staged[@]} — ${staged[*]:-(нет)}"
[[ ${#staged[@]} -gt 0 ]] || finding A2 "урезанное дерево пусто — обход беспредметен"

# ==========================================================================
# A11. Стадия ЗАМКНУТА по импортам.
#
#     A4 этого НЕ заменяет: незамкнутая стадия роняет buf, и находка тогда
#     называет СИМПТОМ («порождение отказало» + сотня строк компилятора) вместо
#     причины — недостающего дерева. Замыкание — свойство самой стадии, и
#     меряется оно прямо: каждый `import` её файла обязан резолвиться внутри неё.
#
#     Разбор здесь СВОЙ и не зовёт библиотеку раскладки: гейт не вправе выводить
#     ожидание из того, что проверяет. Он читает синтаксис объявления импорта и
#     ничего не знает ни о корнях, ни о доменах.
# ==========================================================================
#     ИСКЛЮЧЕНИЕ ровно одно и у него есть ПРЕДИКАТ: `google/protobuf/*` —
#     well-known types, которые компилятор несёт сам; их не раскладывает никто и
#     раскладывать нечего. Предикат проверяется тут же: если такой файл появится
#     в корне контрактов, исключение потеряет предмет — значит его вендорят, и
#     стадия обязана его нести. Считаются они ОТДЕЛЬНЫМ числом, иначе «не
#     резолвится 0» скрывало бы расширение исключения.
stage_imports_seen=0
stage_unresolved=0
stage_wkt=0
stage_first=""
while IFS=$'\t' read -r imp_file imp_path; do
  [[ -n "${imp_path}" ]] || continue
  stage_imports_seen=$((stage_imports_seen + 1))
  [[ -f "${STRIPPED}/${imp_path}" ]] && continue
  if [[ "${imp_path}" == google/protobuf/* ]]; then
    stage_wkt=$((stage_wkt + 1))
    continue
  fi
  stage_unresolved=$((stage_unresolved + 1))
  [[ -n "${stage_first}" ]] || stage_first="${imp_path} ← ${imp_file#"${STRIPPED}/"}"
done < <(grep -rHoE '^[[:space:]]*import[[:space:]]+(public[[:space:]]+)?"[^"]+"' "${STRIPPED}" 2>/dev/null \
         | awk -F'"' '{ i = index($0, ":"); print substr($0, 1, i - 1) "\t" $(NF - 1) }')
echo "check-domain-generation: импортов стадии прочитано ${stage_imports_seen}," \
     "не резолвится ${stage_unresolved}, встроенных в компилятор ${stage_wkt}"
[[ "${stage_imports_seen}" -gt 0 ]] \
  || finding A11 "в стадии не прочитано ни одного импорта — обход беспредметен"
[[ "${stage_unresolved}" -eq 0 ]] \
  || finding A11 "стадия не замкнута по импортам: не резолвится ${stage_unresolved}, первый — ${stage_first}"
# предмет исключения: корень контрактов НЕ вендорит well-known types
if [[ -d "${PROTO_ROOT}/google/protobuf" ]]; then
  finding A11 "корень контрактов вендорит google/protobuf — исключение потеряло предмет, стадия обязана его нести"
fi

# все домены полного дерева, которых НЕТ в отборе и которых НЕТ в замыкании,
# обязаны отсутствовать в урезанном дереве
staged_has() { local n=$1 s; for s in "${staged[@]}"; do [[ "${s}" == "${n}" ]] && return 0; done; return 1; }
for d in "${DOMAINS[@]}"; do
  staged_has "${d}" || finding A2 "выбранного домена '${d}' нет в урезанном дереве"
done
absent=0
while IFS= read -r d; do
  staged_has "${d}" || absent=$((absent + 1))
done < <(for r in "${KACHO_PROTO_ROOTS[@]}"; do
           for d in "${PROTO_ROOT}/${r}"/cloud/*/; do [[ -d "${d}" ]] && basename "${d}"; done
         done)
echo "check-domain-generation: доменов полного дерева отсутствует в урезанном: ${absent}"
[[ "${absent}" -gt 0 ]] || finding A2 "урезанное дерево несёт ВСЕ домены — оно не урезано"

# ==========================================================================
# Отдельный Go-модуль: у него НЕТ исходников края, kacho он резолвит как
# внешнюю зависимость — ровно та форма, в которой живёт вынесенный домен.
# ==========================================================================
STANDALONE="${WORK}/standalone-module"
mkdir -p "${STANDALONE}"
go_line="$(awk '/^go /{print $2; exit}' "${MONOREPO_ROOT}/go.mod")"
cat > "${STANDALONE}/go.mod" <<EOF
module example.invalid/standalone-domain

go ${go_line}

require github.com/PRO-Robotech/kacho v0.0.0

replace github.com/PRO-Robotech/kacho => ${MONOREPO_ROOT}
EOF
cp "${MONOREPO_ROOT}/go.sum" "${STANDALONE}/go.sum"
# anchor тоже приезжает зависимостью — кладём его ВНЕ дерева края
ANCHOR_COPY="${WORK}/anchor/permissions_catalog_root.proto"
mkdir -p "$(dirname "${ANCHOR_COPY}")"
cp "${ANCHOR}" "${ANCHOR_COPY}"

# run_one_generator <скрипт> <пакет плагина> <корень> <выход> <журнал>
# Журнал у КАЖДОГО генератора СВОЙ: общий журнал делает перепись одного из них
# достаточной за обоих, и снятие переписи у второго проходит незамеченным
# (поймано инъекцией, ось 3).
run_one_generator() {
  KACHO_PROTO_ROOT="$3" \
  KACHO_CATALOG_ANCHOR="${ANCHOR_COPY}" \
  KACHO_GEN_MODULE_DIR="${STANDALONE}" \
  KACHO_GEN_PLUGIN_PKG="$2" \
  KACHO_GEN_DOMAINS="${DOMAINS[*]}" \
  "$1" "$4" >"$5" 2>&1
}

run_domain_generation() { # $1=имя  $2=корень  $3=выход каталога  $4=выход таблицы  $5=префикс журналов
  run_one_generator "${REPO_ROOT}/scripts/gen-permission-catalog.sh" \
    "github.com/PRO-Robotech/kacho/gateway/cmd/protoc-gen-kacho-permissions" \
    "$2" "$3" "$5.catalog.log" || return 1
  run_one_generator "${REPO_ROOT}/scripts/gen-rest-route-table.sh" \
    "github.com/PRO-Robotech/kacho/gateway/cmd/protoc-gen-kacho-rest-routes" \
    "$2" "$4" "$5.routes.log" || return 1
  return 0
}

# прогон «внешняя зависимость»: корень контрактов полный (так его отдаёт кэш
# модулей), сужает ОТБОР
run_domain_generation external "${PROTO_ROOT}" \
  "${WORK}/ext_catalog.json" "${WORK}/ext_routes.go" "${WORK}/ext" \
  || { cat "${WORK}"/ext.*.log >&2; finding A4 "порождение по отбору от полного корня отказало"; }

# прогон «урезанное дерево»: корень контрактов несёт только отбор + замыкание
run_domain_generation stripped "${STRIPPED}" \
  "${WORK}/str_catalog.json" "${WORK}/str_routes.go" "${WORK}/str" \
  || { cat "${WORK}"/str.*.log >&2; finding A4 "порождение в урезанном дереве отказало"; }

# ==========================================================================
# A3. Генератор называет свои входы.
# ==========================================================================
for gen_log in "${WORK}/ext.catalog.log" "${WORK}/ext.routes.log"; do
  [[ -f "${gen_log}" ]] || { finding A3 "журнала генератора нет: ${gen_log}"; continue; }
  for key in "корень контрактов" "anchor" "модуль плагина" "пакет плагина" "отбор доменов"; do
    grep -qF "${key}" "${gen_log}" \
      || finding A3 "перепись генератора $(basename "${gen_log}") не называет «${key}»"
  done
done

# ==========================================================================
# A4. Побайтовое равенство двух прогонов.
# ==========================================================================
if [[ -s "${WORK}/ext_catalog.json" && -s "${WORK}/str_catalog.json" ]]; then
  cmp -s "${WORK}/ext_catalog.json" "${WORK}/str_catalog.json" \
    || finding A4 "каталог домена от полного корня и от урезанного дерева РАЗЛИЧАЮТСЯ побайтово"
else
  finding A4 "каталога домена нет ни у одного из двух прогонов"
fi
if [[ -s "${WORK}/ext_routes.go" && -s "${WORK}/str_routes.go" ]]; then
  cmp -s "${WORK}/ext_routes.go" "${WORK}/str_routes.go" \
    || finding A4 "таблица маршрутов домена от полного корня и от урезанного дерева РАЗЛИЧАЮТСЯ побайтово"
else
  finding A4 "таблицы маршрутов домена нет ни у одного из двух прогонов"
fi

# ==========================================================================
# A5. Каждая запись доменного выхода — побайтовый блок/строка полного выхода.
# ==========================================================================
dom_entries=0
dom_routes=0
if [[ -s "${WORK}/str_catalog.json" && -s "${WORK}/full_catalog.json" ]]; then
  # Верхнеуровневые элементы MarshalIndent(entries,"","  ") ограничены строками
  # `  {` и `  }`/`  },` — вложенные объекты идут с отступом 4, поэтому разбиение
  # по отступу 2 точное.
  awk '/^  \{$/{blk=$0"\n"; inb=1; next}
       inb && /^  \},?$/{gsub(/,$/,"",$0); blk=blk"  }"; print blk "\036"; inb=0; next}
       inb{blk=blk $0"\n"}' "${WORK}/str_catalog.json" > "${WORK}/dom_blocks.txt"
  # то же разбиение полного выхода — сравниваем блок с блоком, а не строку с файлом
  awk '/^  \{$/{blk=$0"\n"; inb=1; next}
       inb && /^  \},?$/{gsub(/,$/,"",$0); blk=blk"  }"; print blk "\036"; inb=0; next}
       inb{blk=blk $0"\n"}' "${WORK}/full_catalog.json" > "${WORK}/full_blocks.txt"
  dom_entries="$(grep -c '"fqn"' "${WORK}/str_catalog.json" || true)"
  missing=0
  while IFS= read -r -d $'\036' blk; do
    grep -qF -- "${blk}" "${WORK}/full_blocks.txt" || { missing=$((missing + 1)); echo "  запись домена отсутствует в полном каталоге: $(printf '%s' "${blk}" | sed -n 's/.*"fqn": "\([^"]*\)".*/\1/p')" >&2; }
  done < "${WORK}/dom_blocks.txt"
  [[ "${missing}" -eq 0 ]] || finding A5 "записей доменного каталога, не совпавших побайтово с полным: ${missing}"
fi
if [[ -s "${WORK}/str_routes.go" && -s "${WORK}/full_routes.go" ]]; then
  dom_routes="$(grep -c 'FQN:' "${WORK}/str_routes.go" || true)"
  missing=0
  while IFS= read -r line; do
    grep -qFx -- "${line}" "${WORK}/full_routes.go" || { missing=$((missing + 1)); echo "  маршрут домена отсутствует в полной таблице: ${line}" >&2; }
  done < <(grep 'FQN:' "${WORK}/str_routes.go")
  [[ "${missing}" -eq 0 ]] || finding A5 "маршрутов домена, не совпавших побайтово с полной таблицей: ${missing}"
fi
echo "check-domain-generation: доменный выход — записей каталога ${dom_entries}, маршрутов ${dom_routes}"

# ==========================================================================
# A6. Доменный выход СТРОГО уже полного.
# ==========================================================================
[[ "${dom_entries}" -lt "${full_entries}" ]] \
  || finding A6 "записей доменного каталога ${dom_entries} при полном ${full_entries} — отбор ничего не сузил"
[[ "${dom_routes}" -lt "${full_routes}" ]] \
  || finding A6 "маршрутов домена ${dom_routes} при полном ${full_routes} — отбор ничего не сузил"

# ==========================================================================
# A7. Каждый эмитированный домен объявлен в отборе.
# ==========================================================================
if [[ -s "${WORK}/str_catalog.json" ]]; then
  undeclared=0
  while IFS= read -r d; do
    declared=0
    for sel in "${DOMAINS[@]}"; do [[ "${d}" == "${sel}" ]] && declared=1; done
    [[ "${declared}" -eq 1 ]] || { undeclared=$((undeclared + 1)); echo "  эмитирован необъявленный домен: ${d}" >&2; }
  done < <(kacho_proto_emitted_domains "${WORK}/str_catalog.json" '"fqn"')
  [[ "${undeclared}" -eq 0 ]] || finding A7 "необъявленных доменов в доменном каталоге: ${undeclared}"
fi

# ==========================================================================
# A10. Распознаватель эмитированных доменов знает ВСЕ корни выхода.
#
#     Историческая слепота: перечень доменов собирался ЛИТЕРАЛОМ приставки
#     `kacho.cloud.` в трёх рукописных копиях. Со вторым корнем контракта
#     (KAN-PKG-1) все три перестали видеть дерево службы — и не покраснели:
#     они молчали, а перепись называла заниженное число. A7 этого не различает
#     by construction: невидимый домен в её перечне не появляется, и «все
#     эмитированные объявлены» выполняется тем вернее, чем шире слепота.
#
#     Независимая половина — чистый разбор ПЕРВОГО сегмента имени, без сверки с
#     перечнем корней. Обе стороны, выведенные из одного разбора, разошлись бы
#     вместе и молча.
# ==========================================================================
for out_spec in "str_catalog.json|\"fqn\"" "str_routes.go|FQN"; do
  out_file="${WORK}/${out_spec%%|*}"; out_key="${out_spec#*|}"
  [[ -s "${out_file}" ]] || { finding A10 "выхода ${out_spec%%|*} нет — распознавателю нечего читать"; continue; }
  observed="$(kacho_proto_output_fqns "${out_file}" "${out_key}" | awk -F. '{ print $1 }' | sort -u)"
  resolved="$(kacho_proto_output_roots "${out_file}" "${out_key}")"
  observed_n="$(printf '%s' "${observed}" | grep -c . || true)"
  resolved_n="$(printf '%s' "${resolved}" | grep -c . || true)"
  echo "check-domain-generation: ${out_spec%%|*} — корней в именах ${observed_n} ($(printf '%s' "${observed}" | tr '\n' ' ')), распознано ${resolved_n} ($(printf '%s' "${resolved}" | tr '\n' ' '))"
  [[ "${observed_n}" -gt 0 ]] || finding A10 "${out_spec%%|*}: имён выхода не прочитано — обход беспредметен"
  while IFS= read -r obs_root; do
    [[ -n "${obs_root}" ]] || continue
    printf '%s\n' "${resolved}" | grep -qxF "${obs_root}" \
      || finding A10 "${out_spec%%|*}: корень '${obs_root}' в именах выхода есть, а домена не дал ни одного — распознаватель на нём слеп"
  done <<< "${observed}"
done

# ==========================================================================
# A9. Корень контрактов действительно ЧИТАЕТСЯ, а не выводится из раскладки.
#
#     A4 этого НЕ доказывает: урезанное дерево — подмножество полного, и когда
#     ручка корня игнорируется, оба прогона читают полное дерево и дают один и
#     тот же выход (поймано инъекцией, ось 2). Различает только корень, в
#     котором выбранного домена НЕТ: генератор обязан отказать. Прошёл —
#     значит читал не тот корень.
# ==========================================================================
ROOTLESS="${WORK}/rootless-proto"
cp -R "${STRIPPED}" "${ROOTLESS}"
# Корень домена РЕЗОЛВИТСЯ, а не пишется литералом: домены живут под разными
# корнями (KAN-PKG-1), и `rm -rf` по литеральному пути снял бы НОЛЬ каталогов
# для домена чужого корня. Тогда генератор честно породил бы выход, а A9
# объявила бы находку — про ручку, которая читается исправно.
rootless_root="$(kacho_proto_tree_root "${ROOTLESS}" "${DOMAINS[0]}")" \
  || finding A9 "домен '${DOMAINS[0]}' не резолвится ни под одним корнем (${KACHO_PROTO_ROOTS[*]})"
rm -rf "${ROOTLESS}/${rootless_root}/cloud/${DOMAINS[0]}"
[[ ! -d "${ROOTLESS}/${rootless_root}/cloud/${DOMAINS[0]}" ]] \
  || finding A9 "домен '${DOMAINS[0]}' не снят из корня без домена — инъекция беспредметна"
# Спрашивается КАЖДЫЙ генератор порознь: ручка бывает не прочитана у одного из
# двух, и общий вердикт «отказало» это скрывает.
rootless_checked=0
for gen_spec in \
  "gen-permission-catalog.sh|github.com/PRO-Robotech/kacho/gateway/cmd/protoc-gen-kacho-permissions|rl_catalog.json" \
  "gen-rest-route-table.sh|github.com/PRO-Robotech/kacho/gateway/cmd/protoc-gen-kacho-rest-routes|rl_routes.go"
do
  gen_name="${gen_spec%%|*}"; gen_rest="${gen_spec#*|}"
  gen_pkg="${gen_rest%%|*}"; gen_out="${gen_rest#*|}"
  rootless_checked=$((rootless_checked + 1))
  if run_one_generator "${REPO_ROOT}/scripts/${gen_name}" "${gen_pkg}" \
       "${ROOTLESS}" "${WORK}/${gen_out}" "${WORK}/rl.${gen_name}.log"; then
    finding A9 "${gen_name}: корень без домена '${DOMAINS[0]}' породил выход — ручка KACHO_PROTO_ROOT не читается"
  elif ! grep -qF "выбранного домена '${DOMAINS[0]}' нет в дереве контрактов" "${WORK}/rl.${gen_name}.log"; then
    finding A9 "${gen_name}: отказ на корне без домена '${DOMAINS[0]}' не назвал причину"
  fi
done
echo "check-domain-generation: генераторов проверено на чтение ручки корня: ${rootless_checked}"

# ==========================================================================
# A8. Обход непуст.
# ==========================================================================
[[ "${dom_entries}" -gt 0 ]] || finding A8 "доменный каталог пуст — зелёное было бы вакуумным"
[[ "${dom_routes}"  -gt 0 ]] || finding A8 "доменная таблица маршрутов пуста — зелёное было бы вакуумным"

echo "check-domain-generation: осмотрено — полных выходов 2, доменных выходов 2, утверждений 11, находок ${findings}"
[[ "${findings}" -eq 0 ]] || exit 1
echo "check-domain-generation: OK"
