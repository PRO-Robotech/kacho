#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# audit-list-filter.sh — CI gate для public List<Resource>: per-object видимость
# (RBAC v2, парити с vpc/compute/nlb) + project-scoped listauthz-posture (INV-10).
#
# # Почему гейт пришлось расширить (он был слепым пятном)
#
# Прежняя редакция гейта проверяла ТОЛЬКО project-scope: `projectId` обязателен,
# gateway гейтит его scope_extractor'ом `{project, project_id}` (viewer), repo сужает
# строки по `project_id`. Это закрывает кросс-ПРОЕКТНУЮ утечку и ничего больше. Но
# требование звучит иначе — «публичный List обязан фильтровать результат» — и в
# storage фильтрации ПО ОБЪЕКТАМ не было вовсе: любой член проекта видел КАЖДЫЙ том,
# снимок и образ проекта, независимо от per-object грантов (over-show / BOLA-lite,
# CWE-862), при том что Get/Update/Delete тех же ресурсов per-object грант требовали.
# Гейт с таким именем при этом печатал OK — форма проверки была, содержания не было.
# Поэтому здесь проверяются ОБА измерения; ни одно не заменяет другое.
#
# Гейт роняет PR, если для публичного project-scoped `List`:
#   1. тело `func (r *<R>Repo) List(` в `internal/repo/pg/<r>_repo.go` НЕ сужает
#      результат по `project_id` (регрессия listauthz-posture); проверка скоупится
#      ИМЕННО телом List, а не файлом целиком — `project_id = $`-предикат в
#      Insert/Get не должен давать ложную уверенность (иначе List, обронивший
#      сужение, но сохранивший предикат в другом методе, прошёл бы гейт);
#   2. use-case `func (u *UseCase) List(` в `internal/service/<r>/<r>.go` НЕ требует
#      непустой `projectId` (in-service backstop: пустой projectId → строки ВСЕХ
#      проектов, т.к. repo сужает лишь при ProjectID!=""). Отсутствие файла use-case
#      трактуется fail-closed (нельзя доказать backstop → падаем);
#   3. тело того же use-case `List` НЕ прогоняет прочитанную страницу через
#      per-object фильтр видимости (`authzfilter.FilterVisiblePage` /
#      `FilterVisibleIDs` → kacho-iam AuthorizeService.BatchCheck, `viewer ∪ v_list`).
#
# Форма (3) обязана быть именно «прочитал страницу курсором → батч-Check по её id».
# Обратный приём — `ListAllowedIDs` («перечисли ВСЁ, что subject'у можно») + сужение
# SQL до полученного набора — НЕ принимается: у OpenFGA ListObjects жёсткий
# server-side предел без continuation-token'а, поэтому собственный ресурс тенанта
# молча выпадает за префикс и становится невидимым навсегда. Этот приём снят у всех
# соседей; гейт отвергает его явно, чтобы он не вернулся «как эквивалент».
#
# Whitelist (cluster-catalog, project-scope и per-object гранты неприменимы by design):
#   - disk_type — публичный каталог `{cluster,*}` viewer, cluster-wide reference data.
#
# Override: tools/audit-list-filter.sh --allow="<resource>" расширяет whitelist.

set -euo pipefail
cd "$(dirname "$0")/.."

WHITELIST=("disk_type")
while [[ ${1:-} == --allow=* ]]; do
  WHITELIST+=("${1#--allow=}")
  shift || true
done

is_whitelisted() {
  local r=$1
  for w in "${WHITELIST[@]}"; do [[ "$w" == "$r" ]] && return 0; done
  return 1
}

ROOT=internal/repo/pg
if [[ ! -d "$ROOT" ]]; then
  echo "audit-list-filter: no $ROOT (not a kacho-storage repo)" >&2
  exit 0
fi

# func_body печатает тело функции из файла $1, чья строка-сигнатура содержит
# литеральный маркер $2 (напр. ") List(" — уникален для метода List: ") ListOps("
# / ") ListAttachments(" его НЕ содержат): от строки-сигнатуры до первой строки,
# начинающейся с '}' в 0-й колонке (закрывающая скобка функции у gofmt — единственный
# '}' на нулевом отступе). Скоупит grep телом конкретной функции, а не файлом целиком.
# index() — литеральный поиск, без regex-escaping (dynamic-regex у awk ломается на '(').
func_body() {
  awk -v m="$2" '
    index($0, m) { inb = 1 }
    inb          { print }
    inb && /^}/  { exit }
  ' "$1" | strip_comments
}

# strip_comments срезает `//`-комментарии (до конца строки). Гейт обязан судить по
# КОДУ, а не по прозе: иначе (а) комментарий, объясняющий, почему `ListObjects`
# запрещён, ронял бы сборку, и (б) — что хуже — комментарий, упоминающий
# `FilterVisibleIDs`, «удовлетворял» бы проверку в use-case, который на деле ничего
# не фильтрует. Оба направления залочены фикстурами в audit_list_filter_test.go.
strip_comments() {
  sed -E 's://.*::'
}

FAIL=0
for repofile in "$ROOT"/*_repo.go; do
  [[ -e "$repofile" ]] || continue
  RES=$(basename "$repofile" _repo.go)
  # интересуют только адаптеры с публичным List.
  grep -qE 'func \(r \*[A-Za-z]+Repo\) List\(' "$repofile" || continue
  is_whitelisted "$RES" && continue

  # (1) тело repo.List обязано сужать строки по project_id (listauthz posture).
  #     Скоуп — ТЕЛО List (не файл): предикат в Insert/Get не засчитывается.
  list_body=$(func_body "$repofile" ') List(')
  if ! grep -qE '(\bv\.)?project_id[[:space:]]*=[[:space:]]*\$' <<<"$list_body"; then
    echo "audit-list-filter: $RES — repo.List body does not narrow by project_id"
    echo "  repo: $repofile"
    FAIL=1
    continue
  fi

  # (2) use-case List обязан требовать непустой projectId (in-service backstop).
  #     Пустой projectId → repo вернёт строки ВСЕХ проектов (сужает лишь при !="").
  svc="internal/service/${RES//_/}/${RES//_/}.go"
  if [[ ! -f "$svc" ]]; then
    echo "audit-list-filter: $RES — use-case file $svc absent (cannot prove projectId backstop; fail-closed)"
    FAIL=1
    continue
  fi
  uc_body=$(func_body "$svc" ') List(')
  if ! grep -qE 'ProjectID[[:space:]]*==[[:space:]]*""' <<<"$uc_body"; then
    echo "audit-list-filter: $RES — use-case List does not reject empty projectId"
    echo "  service: $svc"
    FAIL=1
    continue
  fi

  # (3) тело use-case List обязано сузить ПРОЧИТАННУЮ страницу до объектов, видимых
  #     вызывающему (per-object `viewer ∪ v_list` батчем в kacho-iam). Project-scope
  #     из (1)+(2) отвечает лишь «чей это проект», но НЕ «какие объекты этому
  #     caller'у можно» — без (3) любой член проекта видит все строки проекта.
  if ! grep -qE 'FilterVisiblePage|FilterVisibleIDs' <<<"$uc_body"; then
    echo "audit-list-filter: $RES — use-case List does not filter the page per-object (FilterVisiblePage/FilterVisibleIDs)"
    echo "  service: $svc"
    FAIL=1
    continue
  fi

  # (3a) enumerate-then-narrow НЕ считается per-object фильтром: ListObjects усекает
  #      перечисление server-side без continuation-token'а → собственный ресурс
  #      тенанта выпадает за префикс и становится невидимым.
  if grep -qE 'ListAllowedIDs|ListObjects' <<<"$uc_body"; then
    echo "audit-list-filter: $RES — use-case List enumerates allowed ids (ListAllowedIDs/ListObjects) instead of batch-checking the page"
    echo "  service: $svc"
    FAIL=1
  fi
done

if [[ $FAIL -ne 0 ]]; then
  echo
  echo "Every public project-scoped List<Resource> must:"
  echo "  (1) narrow rows by project_id in the repo.List body;"
  echo "  (2) reject an empty projectId in the use-case List (in-service backstop);"
  echo "  (3) filter the page it just read per-object via FilterVisiblePage/"
  echo "      FilterVisibleIDs (kacho-iam BatchCheck, viewer ∪ v_list)."
  echo "(1)+(2) are project scope — they do NOT answer 'may this caller see THESE"
  echo "objects'; without (3) every project member sees every row of the project."
  echo "Enumerating all allowed ids (ListAllowedIDs/ListObjects) is not a substitute."
  echo "Whitelist a cluster-catalog resource with --allow=<resource> if the"
  echo "cluster-wide surface is intentional."
  exit 1
fi

echo "audit-list-filter: OK"
