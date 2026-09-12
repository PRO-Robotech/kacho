#!/usr/bin/env bash

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

#
# check-catalog-splice.sh — гейт склейки: каталог прав и таблица маршрутов,
# СОБРАННЫЕ ИЗ ДВУХ ЧАСТЕЙ (платформа `kacho` + служба `kaname`), дают
# ПОБАЙТОВО то же множество записей, что вшитые артефакты, собранные по
# ВСЕМУ дереву контрактов (kacho#1110, «ПРЕДИКАТ — ДЕЙСТВУЮЩИЙ», п.1–3).
#
# ПРЕДМЕТ. `check-domain-generation.sh` (гейт разреза, kacho#1110 первая
# половина) доказывает, что СЛУЖБА способна породить свою долю каталога в
# одиночку, вне дерева монорепо. Он не доказывает и не обязан доказывать
# другое: что эта доля, СЛОЖЕННАЯ с долей платформы, восстанавливает вшитый
# каталог БЕЗ ПРОПУСКА и БЕЗ ЛИШНЕГО. Молчаливый пропуск здесь не гипотеза —
# край гейтит запросы к службе ЕЁ ЖЕ каталогом (`polyrepo.md`, «край несёт
# записи службы намеренно»), и пропавшая при склейке запись означает
# `catalog: no entry for method` → AUTHZ_DENIED на КАЖДЫЙ вызов конкретного
# RPC, независимо от выданных прав.
#
# Сегодня `corelib` не объявлен отдельным Go-модулем (`git ls-files
# '*go.mod' | wc -l` → 2), поэтому платформа и служба физически лежат в
# ОДНОМ дереве контрактов и склейки двух РЕПОЗИТОРИЕВ в природе не
# существует. Гейт проверяет утверждение, которое переживёт этот блокер:
# домен-отбор (уже несомый генераторами края, `KACHO_GEN_DOMAINS`) даёт РОВНО
# ту доменную границу, по которой пройдёт настоящий разрез, — и объединение
# двух отборов обязано быть математически точным ПРЯМО СЕЙЧАС, до того как
# разрез физически случится.
#
# ЧТО ГЕЙТ УТВЕРЖДАЕТ (каждое — отдельным номером, чтобы находка называла ось):
#   S1  отбор СЛУЖБЫ выведен обходом дерева (свои домены kaname) + тем, что
#       РЕАЛЬНО добирает замыкание импортов (генератор называет добор сам —
#       не выписан здесь второй копией);
#   S2  отбор ПЛАТФОРМЫ выведен обходом дерева — ВСЕ домены под корнем kacho,
#       без исключений: платформа не сужается отбором, она и есть остаток;
#   S3  каталог: множество записей (служба) ОБЪЕДИНЁННОЕ с множеством записей
#       (платформа) побайтово РАВНО множеству записей вшитого каталога —
#       ничего не пропало, ничего лишнего не приехало;
#   S4  таблица маршрутов — то же по своей оси;
#   S5  overlap НЕ учитывается арифметикой (не суммой, а множеством): домен,
#       общий обоим отборам (сегодня — `operation`, живёт под kacho, но нужен
#       службе для компиляции и потому объявлен и там), не даёт ложного
#       расхождения «сумма больше вшитого». Величина overlap печатается, а не
#       требуется положительной — исчезновение общего домена гейт не роняет;
#   S6  ГЕЙТ СПОСОБЕН УПАСТЬ: снятие вклада СЛУЖБЫ (объединение — платформа
#       БЕЗ службы) не восстанавливает вшитый каталог — расхождение обязано
#       найтись, и оно называет РОВНО пропавшую долю;
#   S7  ГЕЙТ СПОСОБЕН УПАСТЬ, «законный близнец» S6 — снятие вклада ОДНОГО
#       домена ПЛАТФОРМЫ (не общего со службой) даёт ту же форму расхождения.
#       Обе оси доказывают, что расхождение ловится ГЕНЕРИЧЕСКИ — гейт не
#       заточен под имя «iam», он заметит пропажу ЛЮБОЙ доли.
#
# Коды возврата: 0 — находок нет · 1 — находка · 2 — предмета нет (нет
# buf/go/python3, нет дерева контрактов, нет вшитых артефактов).
#
# Использование:
#   scripts/check-catalog-splice.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MONOREPO_ROOT="$(cd "${REPO_ROOT}/.." && pwd)"
PROTO_ROOT="${MONOREPO_ROOT}/proto"
FULL_CATALOG="${REPO_ROOT}/internal/middleware/embed/permission_catalog.json"
FULL_ROUTES="${REPO_ROOT}/internal/middleware/rest_route_table_gen.go"

command -v buf     >/dev/null || { echo "БЕЗ ПРЕДМЕТА: buf не установлен — сверять нечего" >&2; exit 2; }
command -v go      >/dev/null || { echo "БЕЗ ПРЕДМЕТА: go не установлен — сверять нечего"  >&2; exit 2; }
command -v python3 >/dev/null || { echo "БЕЗ ПРЕДМЕТА: python3 не установлен — сверять нечего" >&2; exit 2; }
[[ -d "${PROTO_ROOT}" ]]    || { echo "БЕЗ ПРЕДМЕТА: нет дерева контрактов ${PROTO_ROOT}" >&2; exit 2; }
[[ -f "${FULL_CATALOG}" ]]  || { echo "БЕЗ ПРЕДМЕТА: нет вшитого каталога ${FULL_CATALOG}" >&2; exit 2; }
[[ -f "${FULL_ROUTES}" ]]   || { echo "БЕЗ ПРЕДМЕТА: нет вшитой таблицы ${FULL_ROUTES}" >&2; exit 2; }

# shellcheck source=lib/stage-proto-tree.sh
source "${REPO_ROOT}/scripts/lib/stage-proto-tree.sh"

WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

findings=0
finding() { findings=$((findings + 1)); echo "НАХОДКА [$1]: $2" >&2; }

echo "check-catalog-splice: корень контрактов ${PROTO_ROOT}"
echo "check-catalog-splice: полный каталог ${FULL_CATALOG}"
echo "check-catalog-splice: полная таблица ${FULL_ROUTES}"

# ---------------------------------------------------------------------------
# Домены корня — обходом, не литералом.
# ---------------------------------------------------------------------------
domains_under() {
  local root="$1" d
  for d in "${PROTO_ROOT}/${root}"/cloud/*/; do
    [[ -d "${d}" ]] || continue
    basename "${d}"
  done
}

mapfile -t KANAME_OWN < <(domains_under kaname)
mapfile -t KACHO_ALL  < <(domains_under kacho)
echo "check-catalog-splice: доменов kaname (свои службы) ${#KANAME_OWN[@]} — ${KANAME_OWN[*]:-(нет)}"
echo "check-catalog-splice: доменов kacho (вся платформа) ${#KACHO_ALL[@]} — ${KACHO_ALL[*]:-(нет)}"
[[ ${#KANAME_OWN[@]} -gt 0 ]] || finding S1 "под kaname/cloud не найдено ни одного домена — обход беспредметен"
[[ ${#KACHO_ALL[@]}  -gt 0 ]] || finding S2 "под kacho/cloud не найдено ни одного домена — обход беспредметен"

# ---------------------------------------------------------------------------
# S1. Отбор службы = свои домены + РЕАЛЬНЫЙ добор замыкания импортов.
#
# Пробный прогон называет добор САМ (строка «добрано замыканием импортов:
# …» в собственном журнале генератора) — перечень не выписывается второй
# копией, которая устареет первой же новой зависимостью службы.
# ---------------------------------------------------------------------------
probe_log="${WORK}/service-probe.log"
if KACHO_GEN_DOMAINS="${KANAME_OWN[*]}" "${REPO_ROOT}/scripts/gen-permission-catalog.sh" \
     "${WORK}/service-probe.json" >"${probe_log}" 2>&1; then
  probe_added=""
else
  probe_added="$(sed -n 's/.*добрано замыканием импортов: \(.*\)$/\1/p' "${probe_log}" | tail -1)"
  if [[ -z "${probe_added}" ]]; then
    cat "${probe_log}" >&2
    finding S1 "пробный прогон отбора службы отказал не по причине добора замыкания — см. журнал выше"
  fi
fi
[[ "${probe_added}" != "(нечего)" ]] || probe_added=""
read -r -a CLOSURE_ADDED <<< "${probe_added}"
declare -a SERVICE_DOMAINS=("${KANAME_OWN[@]}")
for d in "${CLOSURE_ADDED[@]}"; do
  [[ -n "${d}" ]] || continue
  SERVICE_DOMAINS+=("${d}")
done
echo "check-catalog-splice: отбор службы (свои + добор замыкания) — ${SERVICE_DOMAINS[*]}"

# ---------------------------------------------------------------------------
# S2. Отбор платформы = ВСЕ домены kacho, без исключений.
# ---------------------------------------------------------------------------
declare -a PLATFORM_DOMAINS=("${KACHO_ALL[@]}")
echo "check-catalog-splice: отбор платформы (все домены kacho) — ${PLATFORM_DOMAINS[*]}"

# ---------------------------------------------------------------------------
# Порождение обеих долей — каталог + таблица маршрутов.
# ---------------------------------------------------------------------------
gen_pair() { # $1=имя $2=домены(строка) $3=выход-каталога $4=выход-таблицы
  local label="$1" domains="$2" out_cat="$3" out_rt="$4"
  local log="${WORK}/${label}"
  if ! KACHO_GEN_DOMAINS="${domains}" "${REPO_ROOT}/scripts/gen-permission-catalog.sh" \
        "${out_cat}" >"${log}.catalog.log" 2>&1; then
    cat "${log}.catalog.log" >&2
    return 1
  fi
  if ! KACHO_GEN_DOMAINS="${domains}" "${REPO_ROOT}/scripts/gen-rest-route-table.sh" \
        "${out_rt}" >"${log}.routes.log" 2>&1; then
    cat "${log}.routes.log" >&2
    return 1
  fi
  return 0
}

gen_pair service  "${SERVICE_DOMAINS[*]}"  "${WORK}/service_catalog.json"  "${WORK}/service_routes.go" \
  || finding S1 "порождение доли службы отказало (домены: ${SERVICE_DOMAINS[*]})"
gen_pair platform "${PLATFORM_DOMAINS[*]}" "${WORK}/platform_catalog.json" "${WORK}/platform_routes.go" \
  || finding S2 "порождение доли платформы отказало (домены: ${PLATFORM_DOMAINS[*]})"

# ---------------------------------------------------------------------------
# Сравнение множеств — ОДНА функция на каталог и таблицу, СВОЯ форма записи
# у каждого: JSON-блок каталога канонизируется python'ом (порядок ключей
# сортировкой), строка таблицы маршрутов сравнивается как есть — обе формы
# уже читает `kacho_proto_output_fqns` (lib/stage-proto-tree.sh), здесь она
# НЕ переиспользуется: там нужны ИМЕНА, здесь — целые ЗАПИСИ.
# ---------------------------------------------------------------------------
splice_diff_json() { # $1=svc $2=plat $3=full -> печатает: missing extra overlap svc_n plat_n full_n
  python3 - "$1" "$2" "$3" <<'PY'
import json, sys
def load(p):
    return set(json.dumps(e, sort_keys=True) for e in json.load(open(p)))
svc, plat, full = load(sys.argv[1]), load(sys.argv[2]), load(sys.argv[3])
union = svc | plat
missing = full - union
extra = union - full
overlap = svc & plat
print(len(missing), len(extra), len(overlap), len(svc), len(plat), len(full))
for e in sorted(missing)[:5]:
    d = json.loads(e)
    print("MISSING:" + d.get("fqn", "?"), file=sys.stderr)
for e in sorted(extra)[:5]:
    d = json.loads(e)
    print("EXTRA:" + d.get("fqn", "?"), file=sys.stderr)
PY
}

splice_diff_lines() { # $1=svc $2=plat $3=full -> та же форма вывода, по строкам FQN:
  python3 - "$1" "$2" "$3" <<'PY'
import re, sys
def load(p):
    out = set()
    for line in open(p):
        line = line.strip()
        if 'FQN:' in line:
            out.add(line.rstrip(','))
    return out
svc, plat, full = load(sys.argv[1]), load(sys.argv[2]), load(sys.argv[3])
union = svc | plat
missing = full - union
extra = union - full
overlap = svc & plat
print(len(missing), len(extra), len(overlap), len(svc), len(plat), len(full))
for e in sorted(missing)[:5]:
    print("MISSING:" + e, file=sys.stderr)
for e in sorted(extra)[:5]:
    print("EXTRA:" + e, file=sys.stderr)
PY
}

# ==========================================================================
# S3. Каталог: объединение долей == вшитый каталог.
# ==========================================================================
if [[ -s "${WORK}/service_catalog.json" && -s "${WORK}/platform_catalog.json" ]]; then
  read -r cat_missing cat_extra cat_overlap cat_svc cat_plat cat_full \
    < <(splice_diff_json "${WORK}/service_catalog.json" "${WORK}/platform_catalog.json" "${FULL_CATALOG}" 2>"${WORK}/cat_diff.err")
  echo "check-catalog-splice: каталог — служба ${cat_svc}, платформа ${cat_plat}, вшитый ${cat_full}," \
       "overlap ${cat_overlap}, пропало из объединения ${cat_missing}, лишнее в объединении ${cat_extra}"
  if [[ "${cat_missing}" -ne 0 || "${cat_extra}" -ne 0 ]]; then
    cat "${WORK}/cat_diff.err" >&2
    finding S3 "объединение долей каталога РАСХОДИТСЯ со вшитым: пропало ${cat_missing}, лишнее ${cat_extra}"
  fi
else
  finding S3 "каталога хотя бы одной доли нет — сравнение беспредметно"
  cat_svc=0; cat_plat=0; cat_full=0; cat_overlap=0
fi

# ==========================================================================
# S4. Таблица маршрутов: объединение долей == вшитая таблица.
# ==========================================================================
if [[ -s "${WORK}/service_routes.go" && -s "${WORK}/platform_routes.go" ]]; then
  read -r rt_missing rt_extra rt_overlap rt_svc rt_plat rt_full \
    < <(splice_diff_lines "${WORK}/service_routes.go" "${WORK}/platform_routes.go" "${FULL_ROUTES}" 2>"${WORK}/rt_diff.err")
  echo "check-catalog-splice: маршруты — служба ${rt_svc}, платформа ${rt_plat}, вшитая ${rt_full}," \
       "overlap ${rt_overlap}, пропало из объединения ${rt_missing}, лишнее в объединении ${rt_extra}"
  if [[ "${rt_missing}" -ne 0 || "${rt_extra}" -ne 0 ]]; then
    cat "${WORK}/rt_diff.err" >&2
    finding S4 "объединение долей таблицы маршрутов РАСХОДИТСЯ со вшитой: пропало ${rt_missing}, лишнее ${rt_extra}"
  fi
else
  finding S4 "таблицы маршрутов хотя бы одной доли нет — сравнение беспредметно"
  rt_svc=0; rt_plat=0; rt_full=0; rt_overlap=0
fi

# ==========================================================================
# S5. Overlap считается МНОЖЕСТВОМ, не суммой — величина печатается как факт
#     дерева, а не требуется положительной (домен, дающий overlap сегодня,
#     завтра может перестать быть общим, и это не дефект склейки).
# ==========================================================================
cat_sum=$((cat_svc + cat_plat))
rt_sum=$((rt_svc + rt_plat))
echo "check-catalog-splice: контроль арифметики — каталог: сумма долей ${cat_sum} против объединения" \
     "$((cat_svc + cat_plat - cat_overlap)); таблица: сумма ${rt_sum} против объединения $((rt_svc + rt_plat - rt_overlap))"
if [[ "${cat_overlap}" -gt 0 && "${cat_sum}" -eq "${cat_full}" ]]; then
  finding S5 "у каталога overlap ${cat_overlap} > 0, а сумма долей ${cat_sum} совпала с полным ${cat_full} — " \
    "проверка вырождена: она не отличила бы объединение от наивной суммы"
fi
if [[ "${rt_overlap}" -gt 0 && "${rt_sum}" -eq "${rt_full}" ]]; then
  finding S5 "у таблицы overlap ${rt_overlap} > 0, а сумма долей ${rt_sum} совпала с полным ${rt_full} — " \
    "проверка вырождена: она не отличила бы объединение от наивной суммы"
fi

# ==========================================================================
# S6. Гейт способен упасть: снятие вклада СЛУЖБЫ.
#
#     Объединение без доли службы = сама доля платформы. Если оно, вопреки
#     ожиданию, совпало со вшитым — служба ничего не добавляет, и весь этот
#     самотест беспредметен (печатается как S6, а не молчит).
# ==========================================================================
if [[ -s "${WORK}/platform_catalog.json" ]]; then
  read -r m e _ _ _ _ < <(splice_diff_json "${WORK}/platform_catalog.json" "${WORK}/platform_catalog.json" "${FULL_CATALOG}" 2>/dev/null)
  # объединение "платформа с платформой" эквивалентно "только платформа"
  if [[ "${m}" -eq 0 && "${e}" -eq 0 ]]; then
    finding S6 "снятие доли службы из каталога НЕ дало расхождения — самотест беспредметен: " \
      "доля службы (${cat_svc} записей) ничего не добавляет к платформе"
  else
    echo "check-catalog-splice: S6 (каталог) — без доли службы пропадает ${m} записей, находка ловится"
  fi
else
  finding S6 "доли платформы нет — самотест S6 (каталог) беспредметен"
fi
if [[ -s "${WORK}/platform_routes.go" ]]; then
  read -r m e _ _ _ _ < <(splice_diff_lines "${WORK}/platform_routes.go" "${WORK}/platform_routes.go" "${FULL_ROUTES}" 2>/dev/null)
  if [[ "${m}" -eq 0 && "${e}" -eq 0 ]]; then
    finding S6 "снятие доли службы из таблицы маршрутов НЕ дало расхождения — самотест беспредметен"
  else
    echo "check-catalog-splice: S6 (маршруты) — без доли службы пропадает ${m} маршрутов, находка ловится"
  fi
else
  finding S6 "доли платформы нет — самотест S6 (маршруты) беспредметен"
fi

# ==========================================================================
# S7. Законный близнец S6: снятие вклада ОДНОГО домена ПЛАТФОРМЫ — того,
#     что НЕ входит в overlap со службой (иначе служба сама его прикроет и
#     расхождения не будет — то и был бы вырожденный, беспредметный тест).
#     Домен берётся АЛФАВИТНО ПЕРВЫМ из (платформа минус служба), не
#     литералом: перечень доменов растёт, а строка кода — нет.
# ==========================================================================
declare -a PLATFORM_ONLY=()
for d in "${PLATFORM_DOMAINS[@]}"; do
  own=0
  for s in "${SERVICE_DOMAINS[@]}"; do [[ "${d}" == "${s}" ]] && own=1; done
  [[ "${own}" -eq 0 ]] && PLATFORM_ONLY+=("${d}")
done
if [[ ${#PLATFORM_ONLY[@]} -eq 0 ]]; then
  finding S7 "у платформы не нашлось ни одного домена вне overlap со службой — законный близнец беспредметен"
else
  mapfile -t PLATFORM_ONLY_SORTED < <(printf '%s\n' "${PLATFORM_ONLY[@]}" | sort)
  drop_domain="${PLATFORM_ONLY_SORTED[0]}"
  declare -a PLATFORM_MINUS=()
  for d in "${PLATFORM_DOMAINS[@]}"; do
    [[ "${d}" == "${drop_domain}" ]] && continue
    PLATFORM_MINUS+=("${d}")
  done
  echo "check-catalog-splice: S7 — законный близнец снимает домен '${drop_domain}' из отбора платформы" \
       "(${PLATFORM_MINUS[*]})"
  if gen_pair platform_minus "${PLATFORM_MINUS[*]}" "${WORK}/platform_minus_catalog.json" "${WORK}/platform_minus_routes.go"; then
    read -r m7c e7c _ _ _ _ \
      < <(splice_diff_json "${WORK}/service_catalog.json" "${WORK}/platform_minus_catalog.json" "${FULL_CATALOG}" 2>"${WORK}/s7c.err")
    if [[ "${m7c}" -eq 0 && "${e7c}" -eq 0 ]]; then
      finding S7 "снятие домена '${drop_domain}' из каталога платформы НЕ дало расхождения — " \
        "самотест беспредметен либо домен не даёт записей"
    else
      echo "check-catalog-splice: S7 (каталог) — без домена '${drop_domain}' пропадает ${m7c} записей, находка ловится"
    fi
    read -r m7r e7r _ _ _ _ \
      < <(splice_diff_lines "${WORK}/service_routes.go" "${WORK}/platform_minus_routes.go" "${FULL_ROUTES}" 2>"${WORK}/s7r.err")
    if [[ "${m7r}" -eq 0 && "${e7r}" -eq 0 ]]; then
      finding S7 "снятие домена '${drop_domain}' из таблицы платформы НЕ дало расхождения — самотест беспредметен"
    else
      echo "check-catalog-splice: S7 (маршруты) — без домена '${drop_domain}' пропадает ${m7r} маршрутов, находка ловится"
    fi
  else
    finding S7 "порождение доли платформы без домена '${drop_domain}' отказало — самотест не выполнен"
  fi
fi

assertions="$(grep -oE 'finding S[0-9]+' "${BASH_SOURCE[0]}" | sort -u | wc -l | tr -d ' ')"
echo "check-catalog-splice: осмотрено — доменов kaname ${#KANAME_OWN[@]}, доменов kacho ${#KACHO_ALL[@]}," \
     "утверждений ${assertions}, находок ${findings}"
[[ "${findings}" -eq 0 ]] || exit 1
echo "check-catalog-splice: OK"
