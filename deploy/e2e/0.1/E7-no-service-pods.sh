#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
set -euo pipefail
for svc in api-gateway resource-manager vpc compute loadbalancer; do
  if kubectl -n kacho get deploy "$svc" >/dev/null 2>&1; then
    echo "FAIL: E7 — service '$svc' should NOT be deployed in 0.1"
    exit 1
  fi
done
echo "PASS: E7 — no service pods deployed"
