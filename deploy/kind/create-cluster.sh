#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
kind create cluster --config "$SCRIPT_DIR/kind-config.yaml" --name kacho --wait 60s
