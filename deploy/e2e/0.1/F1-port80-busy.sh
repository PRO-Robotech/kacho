#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
set -euo pipefail
# ПРИМЕЧАНИЕ: этот тест требует sudo для прослушивания порта 80.
# Запускайте вручную: sudo bash e2e/0.1/F1-port80-busy.sh
# В CI (ubuntu-latest) порт 80 обычно свободен и sudo доступен.

make dev-down >/dev/null 2>&1 || true

# Занимаем порт 80
python3 -m http.server 80 &
SQUATTER_PID=$!
sleep 1
trap "kill $SQUATTER_PID 2>/dev/null || true" EXIT

# Сравнение — БЕЗ трубы: `… | grep -q` под `set -o pipefail` возвращает ОТКАЗ
# НА СОВПАДЕНИИ (grep выходит по первому попаданию, писатель получает SIGPIPE,
# и `pipefail` поднимает ЕГО статус до статуса конвейера). Задача #658.
# Отдельно: `make dev-up` здесь ОБЯЗАН отказать (порт занят), и под `pipefail`
# его ненулевой статус уводил `if` в ветку провала НЕЗАВИСИМО от совпадения —
# проба не могла пройти ни при каком поведении продукта.
DEV_UP_OUT="$(make dev-up 2>&1 || true)"
if [[ "$DEV_UP_OUT" == *"port 80 is already in use"* ]]; then
  echo "PASS: F1 — preflight catches busy port 80"
else
  echo "FAIL: F1 — preflight did not detect busy port 80"
  exit 1
fi
