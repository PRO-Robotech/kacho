#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Пересборка фикстур самопроверки гейта `assert-console-probes-verdict.py`
# НАСТОЯЩИМ playwright (#3289): форма отчёта под `--repeat-each` зависит от
# версии прогонщика и даже от каталога запуска, и придуманная форма её не
# воспроизводит. Вход — каталог `node_modules` набора консоли той версии, что в
# `ui-future/e2e/package-lock.json`. Набор копируется во временный каталог,
# абсолютный путь в отчётах заменяется на `/fixture`.
#
#   regen.sh <путь к ui-future/e2e/node_modules>
#
# Отчёты:
#   split-*  — запуск из каталога конфигурации, как в конвейере: корень проб
#              (`testDir`) не совпадает с каталогом запуска, и отчёт кладёт
#              повтор ОТДЕЛЬНОЙ записью `specs[]` с теми же именем, файлом, строкой;
#   merged-* — запуск из каталога проб: каталоги совпадают, и повторы лежат
#              элементами `tests[]` одной записи.
#   *-flip   — первый повтор пробы «а» упал, второй прошёл (`FX_MODE=flip`).
#   list-narrow — выбор прогонщика (`--list --reporter=json`) тем же фильтром.
set -euo pipefail
modules="$(cd "${1:?каталог node_modules набора консоли}" && pwd)"
here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cp -r "$here/playwright.config.ts" "$here/specs" "$here/preconditions" "$work/"
ln -s "$modules" "$work/node_modules"
pw="$work/node_modules/.bin/playwright"
narrow=(--grep 'проба а')
run() { # <имя> <каталог запуска> <режим> <аргументы…>
  local name="$1" dir="$2" mode="$3"; shift 3
  (cd "$dir" && FX_MODE="$mode" FX_OUT="$work/$name.json" "$pw" test --config "$work/playwright.config.ts" "$@" >/dev/null 2>&1) || true
}
run split-full-pass "$work" pass --repeat-each 2
run split-narrow-pass "$work" pass --repeat-each 2 "${narrow[@]}"
run split-narrow-flip "$work" flip --repeat-each 2 "${narrow[@]}"
run merged-full-pass "$work/specs" pass --repeat-each 2
run merged-narrow-flip "$work/specs" flip --repeat-each 2 "${narrow[@]}"
(cd "$work" && PLAYWRIGHT_JSON_OUTPUT_NAME="$work/list-narrow.json" "$pw" test --config "$work/playwright.config.ts" --list --reporter=json "${narrow[@]}" >/dev/null 2>&1)
for f in "$work"/*.json; do
  python3 - "$f" "$work" "$here/$(basename "$f")" <<'PY'
import json, sys
src, work, dst = sys.argv[1:]
text = open(src, encoding="utf-8").read().replace(work, "/fixture")
json.loads(text)
open(dst, "w", encoding="utf-8").write(text)
PY
done
ls "$here"/*.json
