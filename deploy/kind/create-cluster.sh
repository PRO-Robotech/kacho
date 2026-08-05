#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Имя кластера — параметр, а не константа. Разнесение e2e по раннерам поднимает
# несколько кластеров, и совпадение имён означало бы, что шарды сносят друг друга
# (`kind delete cluster --name kacho` в чужой job'е). Умолчание прежнее, поэтому
# локальный `make dev-up` ведёт себя как раньше.
kind create cluster --config "$SCRIPT_DIR/kind-config.yaml" --name "${CLUSTER_NAME:-kacho}" --wait 60s
