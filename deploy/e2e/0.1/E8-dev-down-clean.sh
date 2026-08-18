#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
set -euo pipefail

# line_in <многострочное значение> <строка> — есть ли СТРОКА ЦЕЛИКОМ в значении.
# Замена `grep -qx`/`grep -qxF`: под `pipefail` труба даёт ложный отказ НА
# СОВПАДЕНИИ, потому что писатель получает SIGPIPE (задача #658). Сравнение
# буквальное — там, где раньше стоял `-x` без `-F`, это СТРОЖЕ, то есть ложного
# зелёного добавить не может.
line_in() { [[ $'\n'"$1"$'\n' == *$'\n'"$2"$'\n'* ]]; }
make dev-down
sleep 2
# Сравнение — БЕЗ трубы: `… | grep -q` под `set -o pipefail` возвращает ОТКАЗ
# НА СОВПАДЕНИИ (grep выходит по первому попаданию, писатель получает SIGPIPE,
# и `pipefail` поднимает ЕГО статус до статуса конвейера). Задача #658.
CLUSTERS="$(kind get clusters 2>/dev/null || true)"
if line_in "$CLUSTERS" 'kacho'; then echo "FAIL: cluster still exists"; exit 1; fi
LISTENERS="$(ss -tln 2>/dev/null || true)"
if [[ "$LISTENERS" == *':80 '* ]]; then echo "FAIL: port 80 still bound"; exit 1; fi
echo "PASS: E8"
