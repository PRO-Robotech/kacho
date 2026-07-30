#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# run-incremental.sh — обёртка над run-incremental.js: прогон newman-сьюты ПО ОДНОМУ
# кейсу за раз с зачисткой ресурсов (quota-safe, как для YC). См. шапку run-incremental.js.
#
#   ./scripts/run-incremental.sh                # все кейсы, с нуля
#   ./scripts/run-incremental.sh --list         # СУХОЕ перечисление: что будет прогнано (стенд не нужен)
#   ./scripts/run-incremental.sh --resume       # продолжить прерванный прогон
#   ./scripts/run-incremental.sh --service authz-deny  # одна коллекция (имя = файл в collections/)
#   ./scripts/run-incremental.sh --cleanup-only # только зачистить тест-папки
#   CLEANUP_EVERY=10 DELAY_REQUEST=20 ./scripts/run-incremental.sh   # тюнинг
#   ENV=environments/<файл>.postman_environment.json ./scripts/run-incremental.sh   # другой стенд
#
# Набор по умолчанию НЕ перечисляется здесь: он берётся из имён коллекций на диске
# (см. `collectionStems` в run-incremental.js). Прежний пример называл пять коллекций,
# которых в дереве уже нет, — а такое имя теперь ОТКАЗ, а не тихий пропуск.
#
# Требует: api-gateway доступен по baseUrl из env-файла (локально — port-forward на 18080);
#          newman установлен (`npm install -g newman`); node >= 18.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

NODE_PATH_GLOBAL="$(npm root -g 2>/dev/null || true)"
if [[ -n "${NODE_PATH:-}" ]]; then export NODE_PATH="${NODE_PATH}:${NODE_PATH_GLOBAL}"; else export NODE_PATH="${NODE_PATH_GLOBAL}"; fi

exec node "${SCRIPT_DIR}/run-incremental.js" "$@"
