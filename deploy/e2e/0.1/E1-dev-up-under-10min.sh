#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
set -euo pipefail
START=$(date +%s)
make dev-down >/dev/null 2>&1 || true
make dev-up
END=$(date +%s)
ELAPSED=$((END - START))
echo "dev-up took ${ELAPSED}s"
[ $ELAPSED -lt 600 ] || { echo "FAIL: dev-up took ${ELAPSED}s (>= 600s)"; exit 1; }
echo "PASS: E1"
