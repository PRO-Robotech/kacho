#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
set -euo pipefail
# Симулируем отсутствие kind, временно убрав из PATH
TMPBIN=$(mktemp -d)
trap "rm -rf '$TMPBIN'" EXIT

# Копируем всё кроме kind в tmpbin
for tool in docker kubectl helm; do
  which "$tool" >/dev/null 2>&1 && cp "$(which "$tool")" "$TMPBIN/" || true
done

# Сравнение — БЕЗ трубы: `… | grep -q` под `set -o pipefail` возвращает ОТКАЗ
# НА СОВПАДЕНИИ (grep выходит по первому попаданию, писатель получает SIGPIPE,
# и `pipefail` поднимает ЕГО статус до статуса конвейера). Задача #658.
# `make dev-up` здесь ОБЯЗАН отказать (kind убран из PATH): под `pipefail` его
# статус уводил `if` в ветку провала независимо от совпадения.
DEV_UP_OUT="$(PATH="$TMPBIN" make dev-up 2>&1 || true)"
if [[ "$DEV_UP_OUT" == *"kind not installed"* ]]; then
  echo "PASS: F2 — preflight detects missing kind"
else
  echo "FAIL: F2 — preflight did not detect missing kind"
  exit 1
fi
