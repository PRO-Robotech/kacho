#!/usr/bin/env bash
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

if make dev-up 2>&1 | grep -q "port 80 is already in use"; then
  echo "PASS: F1 — preflight catches busy port 80"
else
  echo "FAIL: F1 — preflight did not detect busy port 80"
  exit 1
fi
