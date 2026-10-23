#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
# Test-only public checksum holder; production has no fixture environment hook.
set -euo pipefail
if [[ $# != 0 ]]; then
  printf '%s\n' 'test-only checksum holder accepts no product arguments' >&2
  exit 2
fi
cd "$(dirname "$0")/../.."
export GOWORK=off
exec go test ./internal/release -run '^TestReleaseSupplyPublicChecksum$' -count=1 -json -timeout=15m
