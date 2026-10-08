#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# relocate.sh — точка входа пост-обработчика (plugin.yaml рядом). Двоичный файл
# собирает deploy/scripts/stand-ns.sh из tools/standns/cmd/stand-ns и называет
# его в KACHO_STAND_NS_BIN; аргументы (`-namespace`, `-ca-namespace`) приходят
# через `--post-renderer-args`. Без двоичного файла — отказ: рендер, который не
# перенесён, писал бы в пространство рабочего стенда.
set -euo pipefail
[ -x "${KACHO_STAND_NS_BIN:-}" ] || {
  echo "kacho-stand-ns: KACHO_STAND_NS_BIN не указывает на собранный stand-ns — перенести рендер нечем" >&2
  exit 2
}
exec "$KACHO_STAND_NS_BIN" relocate "$@"
