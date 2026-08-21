#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# deps-failure-class.sh — ОТЛИЧИТЬ НАШУ ПОЛОМКУ ОТ ЧУЖОЙ НЕДОСТУПНОСТИ.
#
# Живёт отдельным файлом ровно затем, чтобы это различение можно было проверить
# без helm, без сети и без подъёма стенда: проба подсовывает сюда вывод и
# смотрит на вердикт (см. deps-failure-class-inject.sh рядом).

#   classify_deps_failure <файл-с-выводом-helm> → ours | external | transient
classify_deps_failure() {
  local log="$1"
  # Наше: объявление, пин, локальный сабчарт. Проверяется ПЕРВЫМ (см. выше).
  if grep -qE "^Error: directory .* not found|can't get a valid version for|unknown field|cannot be found in the .* directory" "$log"; then
    echo ours; return
  fi
  # Чужое, форма ПЕРВАЯ: удалённый репозиторий не ответил на обновление индекса.
  # Кавычки вокруг адреса — часть формы helm, и они же не дают строке совпасть с
  # нашей прозой об этом же.
  if grep -qE '[Uu]nable to get an update from the "https?://[^"]*" chart repository' "$log"; then
    echo external; return
  fi
  # Чужое, форма ВТОРАЯ: хост не ответил на СКАЧИВАНИИ зависимости, а не на
  # обновлении индекса. Отказ приходит из другой ветки helm и звучит иначе:
  #
  #   Error: could not find : chart hydra not found in https://k8s.ory.sh/helm/charts:
  #   looks like "…" is not a valid chart repository or cannot be reached:
  #   Get "…/index.yaml": read tcp …: read: connection reset by peer
  #
  # Пока форма не была известна, такой отказ падал в `transient`, повторялся до
  # исчерпания бюджета и заканчивался КРАСНЫМ с текстом «доказательства
  # недоступности постороннего хоста НЕТ» — при том что доказательство стояло в
  # том же выводе дословно. То есть «не выполнилось» подавалось как вердикт о
  # дереве. Наблюдалось 2026-08-21 на PR #887.
  #
  # ТРЕБУЮТСЯ ОБА ПРИЗНАКА СРАЗУ, и это не перестраховка. Первый — фраза helm о
  # недостижимости репозитория; второй — СЕТЕВАЯ причина в том же выводе. Одной
  # первой мало: та же фраза выходит и на опечатке в адресе репозитория, а это
  # НАША поломка, которую послабление замаскировало бы навсегда — опечатка не
  # чинится повтором и не перестаёт быть находкой оттого, что адрес выглядит
  # внешним.
  if grep -qE 'is not a valid chart repository or cannot be reached' "$log" \
     && grep -qE 'connection reset by peer|i/o timeout|TLS handshake timeout|EOF|no route to host|network is unreachable|connection refused|502 Bad Gateway|503 Service' "$log"; then
    echo external; return
  fi
  echo transient
}
