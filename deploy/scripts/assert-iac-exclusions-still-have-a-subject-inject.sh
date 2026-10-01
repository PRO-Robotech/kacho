#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Доказательство падучести гейта `assert-iac-exclusions-still-have-a-subject.py` по
# осям прохода отрендеренных профилей зонтика:
#   * записи судятся в ТОЙ форме пути, к которой CI применяет перечень, — относительно
#     каталога рендера (`<стек>/kacho-umbrella/…`), и рендер осмотрен целиком;
#   * исправление значения зонтика, закрывшее находку в профиле, делает запись о ней
#     находкой «предмета больше нет» — запись долга не переживает исправление;
#   * профиль, переставший рендерить путь записи, — находка «снять запись».
# Близнец обоих опытов — контроль: значения и таблица стеков как есть.
# Исполняется в КОПИИ дерева вне репозитория; рабочая копия не меняется.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GATE_REL="deploy/scripts/assert-iac-exclusions-still-have-a-subject.py"
DENOM=3
passed=0
failed=0

command -v trivy >/dev/null 2>&1 || { echo "ОТКАЗ: trivy не найден в PATH — доказывать нечем" >&2; exit 2; }
# Предметы опытов ВЫВОДЯТСЯ из перечня, а не выписаны: запись прохода профилей о
# корне ФС PostgreSQL в стеке prod (её исправление — значение зонтика) и стек первой
# записи прохода профилей (его снятие из таблицы стеков).
read -r PG_ENTRY STACK < <(python3 - "$ROOT/.trivyignore.yaml" <<'PY'
import sys, yaml
doc = yaml.safe_load(open(sys.argv[1], encoding="utf-8")) or {}
paths = [p for e in doc.get("misconfigurations") or [] for p in e.get("paths") or []
         if "/kacho-umbrella/" in p]
pg = [p for p in paths if p.startswith("prod/") and "/charts/pg-vpc/" in p]
print(pg[0] if pg else "-", paths[0].split("/", 1)[0] if paths else "-")
PY
)
[ "${PG_ENTRY:--}" != "-" ] && [ "${STACK:--}" != "-" ] \
  || { echo "ОТКАЗ: в .trivyignore.yaml нет записей прохода профилей — предмета опытов нет" >&2; exit 2; }

work="$(mktemp -d)" || { echo "ОТКАЗ: не создан временный каталог" >&2; exit 2; }
trap 'rm -rf -- "$work"' EXIT
work="$(cd "$work" && pwd -P)" || exit 2
case "$work" in "$(cd "$ROOT" && pwd -P)"/*) echo "ОТКАЗ: временный каталог внутри репозитория" >&2; exit 2 ;; esac

make_copy() {
  local dst="$1" path
  git clone --shared --quiet --no-checkout "$ROOT" "$dst" || return 2
  git -C "$dst" checkout --quiet --detach HEAD || return 2
  # Всё, чем рабочая копия отличается от HEAD (правленое, проиндексированное, новое):
  # доказательство судит дерево, которое сейчас лежит перед автором.
  while IFS= read -r -d '' path; do
    if [ -e "$ROOT/$path" ]; then
      mkdir -p "$dst/$(dirname "$path")" && cp -- "$ROOT/$path" "$dst/$path" || return 2
    else
      rm -f -- "$dst/$path" || return 2
    fi
  done < <(git -C "$ROOT" diff --name-only -z HEAD; git -C "$ROOT" ls-files -o --exclude-standard -z)
  git -C "$dst" add -A || return 2
}

expect() {
  local title="$1" dir="$2" want="$3" needle="$4" out rc
  out="$(cd "$dir" && python3 "$GATE_REL" 2>&1)"; rc=$?
  if [ "$rc" != "$want" ]; then
    echo "ПРОВАЛ  $title: код $rc, ожидался $want"; echo "$out" | tail -6 | sed 's/^/        /'
    failed=$((failed+1)); return
  fi
  if ! grep -qF -- "$needle" <<<"$out"; then
    echo "ПРОВАЛ  $title: код верен, но нет «$needle»"; echo "$out" | tail -6 | sed 's/^/        /'
    failed=$((failed+1)); return
  fi
  echo "ok      $title"; passed=$((passed+1))
}

make_copy "$work/control" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
expect "контроль — записи прохода профилей судятся и имеют предмет" "$work/control" 0 \
  "устаревших записей 0; вне осмотра 0; без пути в дереве 0"

# Исправление значений закрывает находку в отрендеренном профиле — запись о ней
# обязана стать находкой сама (приёмка 4240ac56fd3, п. 2). Близнец — контроль.
make_copy "$work/fixed" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
cat >> "$work/fixed/deploy/helm/umbrella/values.prod.yaml" <<'YAML'
pg-vpc:
  primary:
    containerSecurityContext:
      readOnlyRootFilesystem: true
YAML
git -C "$work/fixed" add -- deploy/helm/umbrella/values.prod.yaml || exit 2
expect "значение зонтика закрыло находку — запись без предмета, находка" "$work/fixed" 1 \
  "«$PG_ENTRY» — предмета больше нет"

# Профиль больше не рендерит путь записи (стек снят из таблицы) — «пути нет».
make_copy "$work/gone" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
sed -i "/^$STACK:/d" "$work/gone/deploy/stacks.txt" && git -C "$work/gone" add -- deploy/stacks.txt || exit 2
expect "стек снят, записи остались — находка «снять запись»" "$work/gone" 1 \
  "— пути нет в дереве ни в одном проходе"

echo "итог: утверждений $((passed+failed)); пройдено $passed; провалено $failed (знаменатель $DENOM)"
[ "$failed" = 0 ] && [ "$((passed+failed))" = "$DENOM" ] || exit 1
