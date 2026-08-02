#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# drift.sh — «перенос ветки не откатил ствол».
#
# Предмет. Ветка, отставшая от ствола на сотни файлов, при переносе может
# ВЕРНУТЬ содержимое базы на файлах, которых она не касалась, — и это пройдёт
# без единого конфликта, потому что конфликт возникает только там, где ОБЕ
# стороны правили одно место. Молчание git о конфликте не есть утверждение о
# сохранности ствола.
#
# Предикат. Для каждого файла F, который ствол изменил со времени базы ветки:
# если ветка F НЕ касалась, то результат обязан совпадать со стволом ПОБАЙТОВО.
# Расхождение — откат, и гейт называет координату.
#
# Почему сравнение именно с деревом объектов, а не с рабочей копией: рабочая
# копия несёт незакоммиченное, и гейт, читающий её, отвечал бы про черновик.
#
# Предпосылка проверяется: если множество изменённых стволом файлов пусто, то
# сравнивать нечего, и это НЕ то же самое, что «откатов нет» — такой прогон
# объявляется беспредметным и падает. «Ноль находок» обязано быть отличимо от
# «ноль прочитанного».
#
# Использование:
#   drift.sh <база> <ветка-источник> <ревизия-ствола> <ревизия-результата>

set -euo pipefail

if [ $# -ne 4 ]; then
  echo "usage: drift.sh <base> <source-branch> <trunk-rev> <landed-rev>" >&2
  exit 2
fi

BASE=$1
SRC=$2
TRUNK=$3
LANDED=$4

for rev in "$BASE" "$SRC" "$TRUNK" "$LANDED"; do
  git rev-parse --verify --quiet "$rev^{commit}" >/dev/null || {
    echo "drift: ревизия '$rev' не разрешается в коммит — гейт смотрит не туда" >&2
    exit 2
  }
done

# Файлы, изменённые стволом с базы ветки, и файлы, которых касалась ветка.
mapfile -t TRUNK_TOUCHED < <(git diff --name-only "$BASE" "$TRUNK")
mapfile -t BRANCH_TOUCHED < <(git diff --name-only "$BASE" "$SRC")

if [ "${#TRUNK_TOUCHED[@]}" -eq 0 ]; then
  echo "drift: ствол не изменил с базы $BASE ни одного файла — сравнивать нечего," >&2
  echo "       и это не то же самое, что «откатов нет»; вердикт беспредметен" >&2
  exit 2
fi

declare -A BRANCH_SET=()
for f in "${BRANCH_TOUCHED[@]}"; do BRANCH_SET["$f"]=1; done

findings=0
examined=0
skipped=0

for f in "${TRUNK_TOUCHED[@]}"; do
  if [ -n "${BRANCH_SET[$f]+x}" ]; then
    skipped=$((skipped + 1))
    continue
  fi
  examined=$((examined + 1))
  a=$(git rev-parse --quiet --verify "$TRUNK:$f" 2>/dev/null || echo MISSING)
  b=$(git rev-parse --quiet --verify "$LANDED:$f" 2>/dev/null || echo MISSING)
  if [ "$a" != "$b" ]; then
    findings=$((findings + 1))
    echo "ОТКАТ: $f — ствол несёт $a, результат несёт $b;"
    echo "       ветка этого файла не касалась, значит расхождение внесено переносом"
  fi
done

echo "drift: перепись — ствол изменил ${#TRUNK_TOUCHED[@]} файл(ов) с базы $BASE;"
echo "       ветка отличается на $skipped из них (их судит не этот гейт);"
echo "       сверено побайтово $examined; находок $findings"

if [ "$examined" -eq 0 ]; then
  echo "drift: ни один файл не сверён — ветка касается всего, что менял ствол;" >&2
  echo "       гейт ничего не проверил, и его молчание ничего не значит" >&2
  exit 2
fi

[ "$findings" -eq 0 ] || exit 1
