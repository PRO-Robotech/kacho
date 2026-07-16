#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
set -euo pipefail

NAMESPACE="${KACHO_NAMESPACE:-kacho}"
ADDRESS="${KACHO_PROXY_ADDRESS:-127.0.0.1}"

PIDS=()

cleanup() {
  if ((${#PIDS[@]} > 0)); then
    echo
    echo "Stopping port-forwards..."
    for pid in "${PIDS[@]}"; do
      if kill -0 "$pid" 2>/dev/null; then
        kill "$pid" 2>/dev/null || true
      fi
    done
  fi
}

trap cleanup EXIT INT TERM

start_forward() {
  local name="$1"
  local target="$2"
  local mapping="$3"

  echo "Starting ${name}: ${mapping} -> ${target}"
  kubectl -n "$NAMESPACE" port-forward --address "$ADDRESS" "$target" "$mapping" &
  PIDS+=("$!")
  sleep 0.4
  if ! kill -0 "$!" 2>/dev/null; then
    echo "ERROR: ${name} port-forward failed to start. Check whether local port ${mapping%%:*} is already in use." >&2
    exit 1
  fi
}

echo "Using namespace: ${NAMESPACE}"
echo "Binding address: ${ADDRESS}"
echo

start_forward "api-gateway" "svc/api-gateway" "8080:8080"
start_forward "kratos-public" "svc/kacho-umbrella-kratos-public" "4433:80"
start_forward "kratos-selfservice-ui" "svc/kratos-selfservice-ui" "4300:3000"
start_forward "hydra-public" "svc/kacho-umbrella-hydra-public" "4444:4444"

echo
echo "Port-forwards are running:"
echo "  api-gateway     http://localhost:8080"
echo "  kratos-public   http://localhost:4433"
echo "  kratos-ui       http://localhost:4300"
echo "  hydra-public    http://localhost:4444"
echo
echo "Keep this terminal open. Press Ctrl+C to stop."

wait
