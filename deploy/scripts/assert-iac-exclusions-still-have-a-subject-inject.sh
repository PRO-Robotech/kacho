#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Доказательство падучести гейта `assert-iac-exclusions-still-have-a-subject.py` по
# двум осям, полученным с третьим проходом (#2980):
#   * записи судятся в ТОЙ форме пути, к которой CI применяет перечень — относительно
#     scan-ref прохода (`cert-manager-v1.16.5.tgz:templates/rbac.yaml`), а не в
#     приведённой к корню; иначе все записи третьего прохода были бы «вне осмотра» и
#     не судились бы никогда;
#   * запись, чей путь снят из дерева (подчарт, уходящий с релизом отказа от
#     стороннего издателя личности), — находка «снять запись»: так держится предикат
#     «снять при снятии подчарта». Предмет опыта ВЫВОДИТСЯ из перечня — первая
#     запись, чей путь лежит в архиве подчарта зонтика, — а не выписан именем.
# Близнец снятия — контроль: архив на месте, запись судится и имеет предмет.
# Исполняется в КОПИИ дерева вне репозитория; рабочая копия не меняется.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GATE_REL="deploy/scripts/assert-iac-exclusions-still-have-a-subject.py"
DENOM=3
passed=0
failed=0

command -v trivy >/dev/null 2>&1 || { echo "ОТКАЗ: trivy не найден в PATH — доказывать нечем" >&2; exit 2; }
# Предмет: первая запись, чей путь лежит в отслеживаемом архиве подчарта зонтика.
ENTRY="$(python3 - "$ROOT/.trivyignore.yaml" <<'PY'
import sys, yaml
doc = yaml.safe_load(open(sys.argv[1], encoding="utf-8")) or {}
hits = [p for e in doc.get("misconfigurations") or [] for p in e.get("paths") or []
        if ":" in p and p.split(":", 1)[0].endswith(".tgz")]
print(hits[0] if hits else "")
PY
)"
[ -n "$ENTRY" ] || { echo "ОТКАЗ: в .trivyignore.yaml нет записи в архиве подчарта — предмета опыта нет" >&2; exit 2; }
ARCHIVE="deploy/helm/umbrella/charts/${ENTRY%%:*}"
git -C "$ROOT" ls-files --error-unmatch "$ARCHIVE" >/dev/null 2>&1 \
  || { echo "ОТКАЗ: архив записи $ARCHIVE не отслеживается — опыт снимать нечего" >&2; exit 2; }

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
expect "контроль — записи третьего прохода судятся и имеют предмет" "$work/control" 0 \
  "устаревших записей 0; вне осмотра 0; без пути в дереве 0"
expect "контроль — записей прочитано 9" "$work/control" 0 "записей исключений 9"

make_copy "$work/gone" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
git -C "$work/gone" rm -q -- "$ARCHIVE" || exit 2
expect "подчарт снят, запись осталась — находка «снять запись»" "$work/gone" 1 \
  "«$ENTRY» — пути нет в дереве ни в одном проходе"

echo "итог: утверждений $((passed+failed)); пройдено $passed; провалено $failed (знаменатель $DENOM)"
[ "$failed" = 0 ] && [ "$((passed+failed))" = "$DENOM" ] || exit 1
