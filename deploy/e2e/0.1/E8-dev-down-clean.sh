#!/usr/bin/env bash
set -euo pipefail
make dev-down
sleep 2
! kind get clusters | grep -q '^kacho$' || { echo "FAIL: cluster still exists"; exit 1; }
! ss -tln | grep -q ':80 ' || { echo "FAIL: port 80 still bound"; exit 1; }
echo "PASS: E8"
