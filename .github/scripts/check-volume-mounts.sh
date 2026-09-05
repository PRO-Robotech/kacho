#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# check-volume-mounts.sh — точка входа гейта «каждый volumeMount имеет свой volume».
#
# ЭТО ТОНКАЯ ОБЁРТКА. Что проверяется, почему матрицей, а не дефолтом, и почему
# том бывает объявлен не только в `volumes:` — документировано в
# .github/scripts/check-volume-mounts.py. Здесь только вызов.
#
# ПОЧЕМУ ТАБЛИЦА ЧАРТОВ И МАТРИЦА ПЕРЕЕХАЛИ В PYTHON. Прежде они были здесь, и
# это был РУЧНОЙ список из четырёх чартов при девяти сервисных: nlb, registry,
# kaname, kacho-geo и uif не проверялись, и об этом ничто не сообщало. Список,
# который никто не сверяет, отстаёт молча. Теперь таблица сверяется с
# file://-зависимостями умбреллы: сервисный чарт вне гейта — находка, и запись
# без предмета — тоже. Такая сверка в шелле означала бы разбор YAML в шелле, а
# inline-python в шелле в этом самом гейте уже дал два дефекта (см. шапку .py).
#
# Локально:     bash .github/scripts/check-volume-mounts.sh
# Один рендер:  helm template … | python3 .github/scripts/check-volume-mounts.py --stdin
# Самопроверка: python3 .github/scripts/check-volume-mounts.py --self-test
set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.." || exit 1

command -v helm >/dev/null 2>&1 || {
  echo "FATAL: нужен helm — без него не будет прочитан ни один рендер, а «не выполнилось»"
  echo "       не идёт в зачёт «прошло»."
  exit 2
}

exec python3 .github/scripts/check-volume-mounts.py "$@"
