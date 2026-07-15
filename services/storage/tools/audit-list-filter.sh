#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# audit-list-filter.sh — CI gate для public List<Resource> listauthz-posture (INV-10).
#
# kacho-storage использует **project-scoped Check** (AddressPool-style), а НЕ per-object
# `ListAllowedIDs` (как vpc/compute): публичный `Volume.List`/`Snapshot.List` требует
# `projectId`, gateway гейтит его scope_extractor'ом `{project, project_id}` (viewer),
# а repo-запрос сужает строки по `project_id`. Комбинация → caller, авторизованный на
# `prj-1`, **никогда** не видит ресурсы `prj-2` by construction (кросс-проектной утечки
# нет). Решение зафиксировано в acceptance CS-1 §GAP-C и docs/architecture/overview.md.
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
#      трактуется fail-closed (нельзя доказать backstop → падаем).
#
# Whitelist (cluster-catalog, project-scope неприменим by design):
#   - disk_type — публичный каталог `{cluster,*}` viewer, cluster-wide (не project-scoped).
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
  ' "$1"
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
  fi
done

if [[ $FAIL -ne 0 ]]; then
  echo
  echo "Every public project-scoped List<Resource> must (1) narrow rows by project_id"
  echo "in the repo.List body AND (2) reject empty projectId in the use-case List"
  echo "(listauthz posture: gateway {project,project_id} Check + repo project scope +"
  echo "in-service required-projectId backstop, INV-10)."
  echo "Whitelist a cluster-catalog resource with --allow=<resource> if the"
  echo "cluster-wide surface is intentional."
  exit 1
fi

echo "audit-list-filter: OK"
