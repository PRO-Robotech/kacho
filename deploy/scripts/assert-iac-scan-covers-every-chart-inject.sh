#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Доказательство падучести гейта `assert-iac-scan-covers-every-chart.py` — инъекцией
# настоящим входом, в ОБЕ стороны, по тем двум осям, которые гейт получил вместе с
# проходами скана (`iac_scan_passes.py`):
#
#   * архивная форма чарта — отслеживаемый `.tgz` вне каталога другого чарта обязан
#     дать цель. Инъекция кладёт НАСТОЯЩИЙ вендоренный архив (схема значений закрыта)
#     в срез прохода заглушек — ровно то, что случилось с ним в дереве, — и гейт обязан
#     назвать его поимённо. Законный близнец — тот же архив в своём каталоге (контроль);
#   * сверка шагов CI с проходами — шаг, чей срез разошёлся с проходом, и проход без
#     гейтового шага — находки. Близнец — тот же файл задания без правки (контроль).
#
# Всё исполняется в КОПИИ дерева вне репозитория (`git clone --shared` + наложение
# правленых отслеживаемых файлов): рабочая копия не меняется ни на байт, и
# восстанавливать нечего. Прогонов четыре: контроль · архив вне своего каталога ·
# срез шага разошёлся · проход без гейтового шага.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GATE_REL="deploy/scripts/assert-iac-scan-covers-every-chart.py"
ARCHIVE="cert-manager-approver-policy-v0.28.0.tgz"
passed=0
failed=0

if ! command -v trivy >/dev/null 2>&1; then
  echo "ОТКАЗ: trivy не найден в PATH — доказывать нечем" >&2
  exit 2
fi
if ! git -C "$ROOT" ls-files --error-unmatch "deploy/helm/vendor/$ARCHIVE" >/dev/null 2>&1; then
  echo "ОТКАЗ: предмет инъекции deploy/helm/vendor/$ARCHIVE не отслеживается — доказательство" >&2
  echo "       обязано взять другой вендоренный архив, а не пройти на пустом месте" >&2
  exit 2
fi

work="$(mktemp -d)" || { echo "ОТКАЗ: не создан временный каталог" >&2; exit 2; }
trap 'rm -rf -- "$work"' EXIT
# Сравниваются разрешённые пути: `TMPDIR` вправе нести `..` и символьные ссылки.
work="$(cd "$work" && pwd -P)" || { echo "ОТКАЗ: временный каталог не разрешается" >&2; exit 2; }
case "$work" in "$(cd "$ROOT" && pwd -P)"/*) echo "ОТКАЗ: временный каталог внутри репозитория: $work" >&2; exit 2 ;; esac

# $1 — каталог копии
make_copy() {
  local dst="$1" path
  git clone --shared --quiet --no-checkout "$ROOT" "$dst" || return 2
  git -C "$dst" checkout --quiet --detach HEAD || return 2
  # Правленые и новые отслеживаемые-к-коммиту файлы рабочей копии: доказательство
  # судит дерево, которое сейчас лежит перед автором, а не последний коммит.
  while IFS= read -r -d '' path; do
    mkdir -p "$dst/$(dirname "$path")" && cp -- "$ROOT/$path" "$dst/$path" || return 2
  done < <(git -C "$ROOT" ls-files -m -o --exclude-standard -z -- deploy/scripts .github/workflows trivy.yaml trivy-vendored-charts.yaml)
  git -C "$dst" add -A -- deploy/scripts .github/workflows trivy.yaml trivy-vendored-charts.yaml || return 2
}

# $1 — заголовок, $2 — каталог копии, $3 — ожидаемый код, $4 — обязательная подстрока
expect() {
  local title="$1" dir="$2" want="$3" needle="$4" out rc
  out="$(cd "$dir" && python3 "$GATE_REL" 2>&1)"; rc=$?
  if [ "$rc" != "$want" ]; then
    echo "ПРОВАЛ  $title: код $rc, ожидался $want"
    echo "$out" | tail -8 | sed 's/^/        /'
    failed=$((failed+1)); return
  fi
  if ! grep -qF -- "$needle" <<<"$out"; then
    echo "ПРОВАЛ  $title: код верен, но в выводе нет «$needle»"
    echo "$out" | tail -8 | sed 's/^/        /'
    failed=$((failed+1)); return
  fi
  echo "ok      $title"; passed=$((passed+1))
}

# ── A. Контроль: архив в своём каталоге, шаги CI совпадают с проходами ──────────
make_copy "$work/control" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
expect "контроль — гейт молчит" "$work/control" 0 "непокрытых 0"
expect "контроль — архив осмотрен проходом вендоренных" "$work/control" 0 "deploy/helm/vendor/$ARCHIVE"

# ── B. Архив с закрытой схемой в срезе прохода заглушек ─────────────────────────
make_copy "$work/archive" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
git -C "$work/archive" mv "deploy/helm/vendor/$ARCHIVE" "deploy/helm/$ARCHIVE" || exit 2
expect "архив вне своего каталога — гейт краснеет и называет его" "$work/archive" 1 \
  "deploy/helm/$ARCHIVE — архив чарта НЕ ДАЛ сканеру ни одной цели"
expect "архив вне своего каталога — проход вендоренных без предмета" "$work/archive" 1 \
  "deploy/helm/vendor — в каталоге нет ни одного отслеживаемого архива"

# ── C. Срез шага CI разошёлся с проходом ────────────────────────────────────────
make_copy "$work/skip" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
sed -i "/^ *skip-dirs: 'deploy\/helm\/vendor'$/d" "$work/skip/.github/workflows/security-scan.yml"
expect "шаг без skip-dirs прохода — находка" "$work/skip" 1 \
  "не совпадает ни с одним проходом"

# ── D. Проход без гейтового шага ────────────────────────────────────────────────
make_copy "$work/ungated" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
python3 - "$work/ungated/.github/workflows/security-scan.yml" <<'PY' || exit 2
import sys, yaml
p = sys.argv[1]
d = yaml.safe_load(open(p, encoding="utf-8"))
hit = 0
for st in d["jobs"]["trivy"]["steps"]:
    w = st.get("with") or {}
    if w.get("scan-type") == "config" and w.get("scan-ref") == "deploy/helm/vendor" \
            and str(w.get("exit-code")) == "1":
        w["exit-code"] = "0"
        hit += 1
if hit != 1:
    sys.exit("инъекция не нашла гейтовый шаг прохода вендоренных: %d" % hit)
yaml.safe_dump(d, open(p, "w", encoding="utf-8"), allow_unicode=True)
PY
expect "проход без гейтового шага — находка" "$work/ungated" 1 \
  "проход «вендоренные» не судится в CI"

echo "итог: утверждений $((passed+failed)); пройдено $passed; провалено $failed"
[ "$failed" = 0 ] || exit 1
