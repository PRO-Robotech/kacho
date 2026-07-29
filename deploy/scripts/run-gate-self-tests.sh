#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ГЕЙТ НАД ГЕЙТАМИ: каждая написанная самопроверка обязана ИСПОЛНЯТЬСЯ.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАЧЕМ
#
# Самопроверка (`--self-test`) — это доказательство, что гейт умеет краснеть на
# внесённом дефекте и молчать на законной конструкции той же формы. Ровно то,
# без чего гейт сам является экземпляром «проверки с формой, но без содержания».
#
# Написаны они были — а исполнялись НИКЕМ. Цель манифест-проверок запускала
# скрипты БЕЗ аргументов (то есть только их обычный проход), в workflow'ах их не
# было вовсе. Доказательство существовало как текст и ни разу не проверялось —
# то самое, ради борьбы с чем его и писали.
#
# ЭТОТ СКРИПТ НЕ ВЕДЁТ СПИСОК РУКАМИ. Он НАХОДИТ самопроверки по исполняемому
# признаку и требует, чтобы найденное и объявленное СОВПАДАЛО:
#   • нашлась самопроверка, которой нет в списке → ПРОВАЛ (новую забыли внести,
#     и она бы тихо не исполнялась — тот же дефект классом ниже);
#   • в списке есть запись, для которой самопроверки больше нет → ПРОВАЛ
#     (исключение живёт, пока у него есть предмет; запись без предмета — находка).
#
# ПРИЗНАК — ИСПОЛНЯЕМАЯ ВЕТКА, А НЕ СЛОВО В ТЕКСТЕ. Слово «--self-test»
# встречается в шапках и строках помощи у файлов, где ветки нет; поиск по слову
# нашёл бы их и потребовал запускать несуществующее. Ищем именно разбор
# аргумента.
# ─────────────────────────────────────────────────────────────────────────────
set -uo pipefail

DEPLOY_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$DEPLOY_ROOT" || exit 2

# Объявленный состав. Держится СИНХРОННЫМ с находкой ниже — расхождение в любую
# сторону роняет проверку.
DECLARED="
scripts/assert-ban6-external-isolation.py
scripts/assert-outbox-autovacuum.sh
tests/helm/admin-hop-transport-test.sh
tests/helm/image-rollout-binding-test.sh
tests/helm/makefile-destructive-guarded-test.sh
tests/helm/networkpolicy-egress-test.sh
tests/helm/outbox-autovacuum-naptime-test.sh
tests/helm/prerequisite-secrets-test.sh
tests/helm/trusted-forwarder-profiles-test.sh
"

# Поиск: файл несёт РАЗБОР аргумента --self-test (bash-тест либо разбор argv).
discover() {
  local f
  for f in scripts/assert-*.sh scripts/assert-*.py tests/helm/*.sh; do
    [ -f "$f" ] || continue
    if grep -qE '= *"--self-test" *\]|"--self-test" +in +sys\.argv' "$f"; then
      echo "$f"
    fi
  done | sort
}

# ── ПРЕДУСЛОВИЯ ИСПОЛНЯЮТСЯ ЗДЕСЬ, А НЕ ПРЕДПОЛАГАЮТСЯ У ЗАПУСКАЮЩЕГО ────────
#
# Ровно те же два, что и у цели манифест-проверок, и по той же причине —
# проверено на себе при первом в истории прогоне этих самопроверок:
#
# 1. Зависимости чарта. `charts/*.tgz` в git не лежат; на свежем checkout'е
#    рендер падает у всех, КРОМЕ тех проверок, что зовут `helm dep update` сами.
#    Тогда исход зависит от АЛФАВИТНОГО ПОРЯДКА имён файлов: самопроверка
#    networkpolicy-egress упала «values.prod не рендерится», а после того как
#    шедшая ниже по алфавиту outbox-autovacuum-naptime собрала зависимости —
#    прошла без единой правки. Собираем один раз, заранее, для всех.
#
# 2. Тот ли yq. В PATH бывают две разные программы с этим именем; фильтры
#    написаны под mikefarah v4, а python-обёртка над jq молча отдаёт ПУСТО.
#    Одна проверка ловит это сама и отказывается работать — остальные нет.
#
# Отсутствие инструмента — ОТКАЗ, а не пропуск: «не выполнилось» не идёт в зачёт
# «прошло».
if ! command -v yq >/dev/null 2>&1; then
  echo "FATAL: нужен yq (mikefarah v4) — фильтры проверок написаны под него."
  exit 2
fi
if ! yq --version 2>&1 | grep -qE 'mikefarah|version v?4'; then
  echo "FATAL: в PATH не тот yq: $(command -v yq) → $(yq --version 2>&1)"
  echo "       Нужен mikefarah yq v4. python-yq (обёртка над jq) на этих фильтрах"
  echo "       молча отдаёт ПУСТО — утверждения пройдут, ничего не сверив."
  exit 2
fi
echo "=== helm dependency build (самопроверки рендерят умбреллу; charts/*.tgz не в git) ==="
( cd helm/umbrella && helm dep update >/dev/null 2>&1 ) \
  || { echo "FATAL: helm dep update сорвался — рендер будет неполным, проверки НЕ ВЫПОЛНЕНЫ"; exit 2; }
rm -rf helm/umbrella/tmpcharts-*

FOUND="$(discover)"
WANT="$(printf '%s\n' $DECLARED | sort)"

if [ "$FOUND" != "$WANT" ]; then
  echo "FAIL: состав самопроверок разошёлся с объявленным."
  echo
  extra="$(comm -23 <(printf '%s\n' "$FOUND") <(printf '%s\n' "$WANT"))"
  gone="$(comm -13 <(printf '%s\n' "$FOUND") <(printf '%s\n' "$WANT"))"
  [ -n "$extra" ] && { echo "  самопроверка есть, а в списке её НЕТ (не исполнялась бы):"; printf '    %s\n' $extra; }
  [ -n "$gone" ]  && { echo "  в списке есть, а самопроверки НЕТ (запись без предмета):"; printf '    %s\n' $gone; }
  echo
  echo "Внеси изменение в DECLARED в $(basename "$0") — список существует затем,"
  echo "чтобы забытая самопроверка была видна, а не затем, чтобы его обходить."
  exit 1
fi

count="$(printf '%s\n' "$FOUND" | grep -c . )"
echo "=== самопроверки гейтов: найдено и объявлено $count, состав совпадает ==="

failed=""
ran=0
for f in $FOUND; do
  echo
  echo "=== $f --self-test ==="
  case "$f" in
    *.py) cmd=(python3 "$f" --self-test) ;;
    *)    cmd=(bash "$f" --self-test) ;;
  esac
  if "${cmd[@]}"; then
    ran=$((ran + 1))
  else
    failed="$failed $f"
  fi
done

echo
if [ -n "$failed" ]; then
  echo "!!! самопроверки провалены:$failed"
  echo "    Гейт, чья самопроверка красная, не доказал, что умеет краснеть на дефекте —"
  echo "    и его зелёный обычный проход ничего не значит."
  exit 1
fi

# «Ноль находок» обязано быть отличимо от «ноль прочитанного».
if [ "$ran" -eq 0 ]; then
  echo "FAIL: не исполнено НИ ОДНОЙ самопроверки — это провал, а не чистота."
  exit 1
fi
echo "PASS: самопроверки гейтов — $ran/$count зелёные"
