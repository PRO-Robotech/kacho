#!/usr/bin/env bash
set -euo pipefail
# Симулируем отсутствие kind, временно убрав из PATH
TMPBIN=$(mktemp -d)
trap "rm -rf '$TMPBIN'" EXIT

# Копируем всё кроме kind в tmpbin
for tool in docker kubectl helm; do
  which "$tool" >/dev/null 2>&1 && cp "$(which "$tool")" "$TMPBIN/" || true
done

if PATH="$TMPBIN" make dev-up 2>&1 | grep -q "kind not installed"; then
  echo "PASS: F2 — preflight detects missing kind"
else
  echo "FAIL: F2 — preflight did not detect missing kind"
  exit 1
fi
