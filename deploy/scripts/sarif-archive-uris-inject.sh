#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Доказательство падучести `sarif-archive-uris.py` — инъекцией в обе стороны, на
# синтетическом checkout'е во временном каталоге (рабочая копия не трогается).
#
# Вход — та форма, которую trivy 0.70.0 действительно пишет для архива-чарта
# (координата `<архив>:<шаблон>` под базой ROOTPATH = каталог scan-ref): её отверг
# Code Scanning на волне #2977. Прогонов шесть: архивная координата переведена ·
# близнец (обычная координата существующего файла) не тронут · архива нет в
# checkout'е · координата со схемой вне архивной формы · база вне checkout'а ·
# отчёт не прочитан.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TOOL="$ROOT/deploy/scripts/sarif-archive-uris.py"
passed=0
failed=0

work="$(mktemp -d)" || { echo "ОТКАЗ: не создан временный каталог" >&2; exit 2; }
trap 'rm -rf -- "$work"' EXIT
work="$(cd "$work" && pwd -P)" || { echo "ОТКАЗ: временный каталог не разрешается" >&2; exit 2; }
mkdir -p "$work/co/deploy/helm/vendor" "$work/co/services/x/deploy/templates" || exit 2
printf 'archive\n' > "$work/co/deploy/helm/vendor/chart-1.0.0.tgz"
printf 'kind: Deployment\n' > "$work/co/services/x/deploy/templates/deployment.yaml"

# $1 — файл, $2 — uri, $3 — база (каталог относительно checkout'а; "-" — без базы)
sarif() {
  python3 - "$@" <<'PY'
import json, pathlib, sys
dst, uri, base = sys.argv[1], sys.argv[2], sys.argv[3]
loc = {"physicalLocation": {"artifactLocation": {"uri": uri},
                            "region": {"startLine": 34, "endLine": 70}},
       "message": {"text": uri}}
run = {"tool": {"driver": {"name": "Trivy"}},
       "results": [{"ruleId": "KSV-0011", "message": {"text": "Artifact: " + uri},
                    "locations": [loc]}]}
if base != "-":
    loc["physicalLocation"]["artifactLocation"]["uriBaseId"] = "ROOTPATH"
    run["originalUriBaseIds"] = {"ROOTPATH": {"uri": pathlib.Path(base).resolve().as_uri() + "/"}}
json.dump({"version": "2.1.0", "runs": [run]}, open(dst, "w"))
PY
}

# $1 — заголовок, $2 — файл, $3 — ожидаемый код, $4 — обязательная подстрока
expect() {
  local title="$1" file="$2" want="$3" needle="$4" out rc
  out="$(cd "$work/co" && python3 "$TOOL" "$file" 2>&1)"; rc=$?
  if [ "$rc" != "$want" ]; then
    echo "ПРОВАЛ  $title: код $rc, ожидался $want"; echo "$out" | tail -5 | sed 's/^/        /'
    failed=$((failed+1)); return
  fi
  if ! grep -qF -- "$needle" <<<"$out$(cat "$file" 2>/dev/null)"; then
    echo "ПРОВАЛ  $title: код верен, но нет «$needle»"; echo "$out" | tail -5 | sed 's/^/        /'
    failed=$((failed+1)); return
  fi
  echo "ok      $title"; passed=$((passed+1))
}

sarif "$work/a.sarif" "chart-1.0.0.tgz:templates/deployment.yaml" "$work/co/deploy/helm/vendor"
expect "архивная координата переведена в путь checkout'а" "$work/a.sarif" 0 \
  '"uri": "deploy/helm/vendor/chart-1.0.0.tgz"'

sarif "$work/b.sarif" "services/x/deploy/templates/deployment.yaml" "$work/co"
expect "близнец: обычная координата существующего файла не тронута" "$work/b.sarif" 0 \
  "переведено из архивной формы 0"

sarif "$work/c.sarif" "gone-1.0.0.tgz:templates/deployment.yaml" "$work/co/deploy/helm/vendor"
expect "архива нет в checkout'е — находка с координатой" "$work/c.sarif" 1 \
  "deploy/helm/vendor/gone-1.0.0.tgz — файла по этой координате в checkout'е нет"

sarif "$work/d.sarif" "chart.zip:templates/deployment.yaml" "$work/co/deploy/helm/vendor"
expect "схема вне архивной формы — находка, а не тихая выгрузка" "$work/d.sarif" 1 \
  "координата несёт схему «chart.zip»"

sarif "$work/e.sarif" "chart-1.0.0.tgz:templates/deployment.yaml" "$work"
expect "база вне checkout'а — находка" "$work/e.sarif" 1 "вне checkout'а"

expect "отчёт не прочитан — код 2" "$work/absent.sarif" 2 "не прочитан как SARIF"

echo "итог: утверждений $((passed+failed)); пройдено $passed; провалено $failed"
[ "$failed" = 0 ] || exit 1
