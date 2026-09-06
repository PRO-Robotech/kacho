#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Доказательство падучести гейта `assert-scan-stubs-hide-nothing.py` — инъекцией
# настоящим входом, в ОБЕ стороны.
#
# Гейт объявляет: заглушка ТОЛЬКО ДЛЯ СКАНА обязана открывать находки и не гасить их.
# Утверждение проверяемо ровно тогда, когда гейт КРАСНЕЕТ на гасящей заглушке и
# МОЛЧИТ на законной. Ни того, ни другого из чтения кода не видно.
#
# Инъекция ломает РОВНО проверяемое (testing.md §«Гейт на класс», п. 2в): гасящая
# заглушка оставляет все цели на месте, поэтому соседний гейт покрытия обязан
# остаться зелёным — это и проверяется отдельным утверждением. Без него молчание
# соседа было бы неотличимо от молчания мёртвой проверки.
#
# Прогонов ПЯТЬ, а не два: контроль · гасящая заглушка · сосед на той же инъекции ·
# заглушка без предмета · пустой список (идеал не превращён в поломку).
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CONFIG="$ROOT/trivy.yaml"
GATE="$ROOT/deploy/scripts/assert-scan-stubs-hide-nothing.py"
COVERAGE="$ROOT/deploy/scripts/assert-iac-scan-covers-every-chart.py"
BACKUP="$(mktemp)"
passed=0
failed=0

cleanup() {
  cp "$BACKUP" "$CONFIG"
  rm -f "$BACKUP"
}
trap cleanup EXIT

if ! command -v trivy >/dev/null 2>&1; then
  echo "ОТКАЗ: trivy не найден в PATH — доказывать нечем" >&2
  exit 2
fi
cp "$CONFIG" "$BACKUP"

# $1 — заголовок, $2 — ожидаемый код, $3 — обязательная подстрока вывода ("" — любая)
expect() {
  local title="$1" want="$2" needle="$3" out rc
  out="$(python3 "$GATE" 2>&1)"; rc=$?
  if [ "$rc" != "$want" ]; then
    echo "ПРОВАЛ  $title: код $rc, ожидался $want"
    echo "$out" | tail -6 | sed 's/^/        /'
    failed=$((failed+1)); return
  fi
  if [ -n "$needle" ] && ! grep -qF "$needle" <<<"$out"; then
    echo "ПРОВАЛ  $title: код верен, но в выводе нет «$needle»"
    echo "$out" | tail -6 | sed 's/^/        /'
    failed=$((failed+1)); return
  fi
  echo "ok      $title"; passed=$((passed+1))
}

# ── A. Контроль: дерево как есть. Законная заглушка обязана МОЛЧАТЬ ────────────
expect "законные заглушки — гейт молчит" 0 "погашено 0"

# ── B. Инъекция: заглушка, гасящая находку и НЕ теряющая ни одной цели ────────
# `image.repository` подменяет координату образа у чартов, объявивших `image`
# таблицей, — и KSV-0125 («образы только из доверенных реестров») перестаёт
# срабатывать. Целей при этом ровно столько же.
printf '      - image.repository=gcr.io/trivy-scan-only\n' >> "$CONFIG"
expect "гасящая заглушка — гейт краснеет" 1 "KSV-0125"
expect "гасящая заглушка — назван чарт" 1 "services/nlb/deploy/templates/deployment.yaml"

# ── C. Та же инъекция НЕ роняет соседа: класс различим ────────────────────────
cov_out="$(python3 "$COVERAGE" 2>&1)"; cov_rc=$?
if [ "$cov_rc" = 0 ]; then
  echo "ok      гейт покрытия на той же инъекции молчит — класс различим"
  passed=$((passed+1))
else
  echo "ПРОВАЛ  гейт покрытия покраснел (код $cov_rc): инъекция ломает не только"
  echo "        проверяемое, и красное могло прийти от соседа"
  echo "$cov_out" | tail -4 | sed 's/^/        /'
  failed=$((failed+1))
fi
cp "$BACKUP" "$CONFIG"

# ── D. Заглушка без предмета: объявлена, но снятие её не меняет ничего ───────
# Это находка, а не третий исход: заглушка, ничего не открывающая, подменяет
# величину даром. Так самоистекает послабление — ровно как запись ведомости
# исключений, которой больше нечего исключать.
python3 - "$CONFIG" <<'PY'
import sys, yaml
p = sys.argv[1]
d = yaml.safe_load(open(p, encoding="utf-8"))
d["misconfiguration"]["helm"]["set"] = ["trivyStubsProbe=true"]
yaml.safe_dump(d, open(p, "w", encoding="utf-8"), allow_unicode=True)
PY
expect "заглушка без предмета — находка" 1 "не меняет НИЧЕГО"
cp "$BACKUP" "$CONFIG"

# ── E. Идеал не превращён в поломку: заглушек нет вовсе ───────────────────────
python3 - "$CONFIG" <<'PY'
import sys, yaml
p = sys.argv[1]
d = yaml.safe_load(open(p, encoding="utf-8"))
d["misconfiguration"]["helm"]["set"] = []
yaml.safe_dump(d, open(p, "w", encoding="utf-8"), allow_unicode=True)
PY
expect "заглушек нет — гейт проходит, а не падает" 0 "судить нечего"
cp "$BACKUP" "$CONFIG"

# ── F. Откат ДОКАЗАН, а не заявлен ───────────────────────────────────────────
# Сверяется с копией, снятой ПЕРЕД инъекцией, а не с индексом git: инъекция обязана
# вернуть ровно то, что застала, — и когда сам файл правится этим же изменением
# (то есть отличается от индекса законно), предикат по индексу отвечал бы о другом.
if cmp -s "$BACKUP" "$CONFIG"; then
  echo "ok      trivy.yaml восстановлен побайтово"; passed=$((passed+1))
else
  echo "ПРОВАЛ  trivy.yaml НЕ восстановлен после инъекции"; failed=$((failed+1))
fi

echo "итог: утверждений $((passed+failed)); пройдено $passed; провалено $failed"
[ "$failed" = 0 ] || exit 1
