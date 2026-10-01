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
# восстанавливать нечего. Прогонов одиннадцать: контроль · архив вне своего каталога ·
# срез шага разошёлся · проход без гейтового шага · опыты приёмки 7b9d560610b:
# M3 (`.tar.gz` с закрытой схемой) и близнец M2 (та же форма, схема открыта) · M4
# (`.tar` с закрытой схемой) и близнец M4-twin · M1 (гейтовый шаг под
# `continue-on-error: true`) · M5 (гейтовый шаг под `if: false`) · перечень форм
# архива отстал от сканера. Близнец опыта с архивом — тот же архив той же формы в
# том же месте, у которого меняется РОВНО одно: схема значений открыта, и заглушки
# его рендер не роняют. Близнец M1, M5 и перечня форм — контроль A.
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

# $1 — копия, $2 — путь архива в ней, $3 — closed|open (схема значений)
put_chart_archive() {
  python3 - "$1/$2" "$3" <<'PY' && git -C "$1" add -- "$2"
import io, json, sys, tarfile
dst, schema = sys.argv[1], sys.argv[2]
files = {
    "Chart.yaml": "apiVersion: v2\nname: injchart\nversion: 0.1.0\n",
    "values.yaml": "image: injchart\n",
    "templates/deployment.yaml": (
        "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: injchart}\n"
        "spec:\n  selector: {matchLabels: {a: b}}\n  template:\n"
        "    metadata: {labels: {a: b}}\n    spec:\n"
        "      containers: [{name: c, image: \"{{ .Values.image }}\"}]\n"),
    "values.schema.json": json.dumps({
        "type": "object", "properties": {"image": {"type": "string"}},
        "additionalProperties": schema != "closed"}),
}
mode = "w:gz" if dst.endswith((".tgz", ".tar.gz")) else "w"
with tarfile.open(dst, mode) as t:
    for rel, body in files.items():
        data = body.encode()
        info = tarfile.TarInfo("injchart/" + rel)
        info.size = len(data)
        t.addfile(info, io.BytesIO(data))
PY
}

# $1 — копия, $2 — ключ шага, $3 — значение (YAML-скаляр)
mute_vendored_gate_step() {
  python3 - "$1/.github/workflows/security-scan.yml" "$2" "$3" <<'PY'
import sys, yaml
p, key, raw = sys.argv[1], sys.argv[2], sys.argv[3]
d = yaml.safe_load(open(p, encoding="utf-8"))
hit = 0
for st in d["jobs"]["trivy"]["steps"]:
    w = st.get("with") or {}
    if w.get("scan-type") == "config" and w.get("scan-ref") == "deploy/helm/vendor" \
            and str(w.get("exit-code")) == "1":
        st[key] = yaml.safe_load(raw)
        hit += 1
if hit != 1:
    sys.exit("инъекция не нашла гейтовый шаг прохода вендоренных: %d" % hit)
yaml.safe_dump(d, open(p, "w", encoding="utf-8"), allow_unicode=True)
PY
}

# ── E. M3 / M2: `.tar.gz` в срезе прохода заглушек ──────────────────────────────
make_copy "$work/m3" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
put_chart_archive "$work/m3" deploy/helm/closedc-0.1.0.tar.gz closed || exit 2
expect "M3: .tar.gz с закрытой схемой — гейт краснеет и называет его" "$work/m3" 1 \
  "deploy/helm/closedc-0.1.0.tar.gz — архив чарта НЕ ДАЛ сканеру ни одной цели"
make_copy "$work/m2" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
put_chart_archive "$work/m2" deploy/helm/openc-0.1.0.tar.gz open || exit 2
expect "M2 (близнец M3): .tar.gz с открытой схемой — осмотрен, гейт молчит" "$work/m2" 0 \
  "целей  deploy/helm/openc-0.1.0.tar.gz"

# ── F. M4 / M4-twin: `.tar` в срезе прохода заглушек ────────────────────────────
make_copy "$work/m4" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
put_chart_archive "$work/m4" deploy/helm/closedc-0.1.0.tar closed || exit 2
expect "M4: .tar с закрытой схемой — гейт краснеет и называет его" "$work/m4" 1 \
  "deploy/helm/closedc-0.1.0.tar — архив чарта НЕ ДАЛ сканеру ни одной цели"
make_copy "$work/m4twin" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
put_chart_archive "$work/m4twin" deploy/helm/openc-0.1.0.tar open || exit 2
expect "M4-twin: .tar с открытой схемой — осмотрен, гейт молчит" "$work/m4twin" 0 \
  "целей  deploy/helm/openc-0.1.0.tar"

# ── G. M1 / M5: гейтовый шаг, который заведомо не судит ────────────────────────
make_copy "$work/m1" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
mute_vendored_gate_step "$work/m1" continue-on-error true || exit 2
expect "M1: гейтовый шаг под continue-on-error — находка" "$work/m1" 1 \
  "заведомо не судит: continue-on-error"
make_copy "$work/m5" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
mute_vendored_gate_step "$work/m5" if false || exit 2
expect "M5: гейтовый шаг под if: false — находка" "$work/m5" 1 \
  "заведомо не судит: if: False"

# ── H. Предпосылка: перечень форм архива перемеряется, а не принят на веру ──────
# Перечень, отставший от поведения сканера, — ровно та слепота, что нашли M3 и M4.
# Инъекция возвращает его к первой редакции (одна `.tgz`); законный близнец —
# контроль A, где перечень совпал с замером.
make_copy "$work/forms" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
sed -i 's/^ARCHIVE_FORMS = (".tgz", ".tar.gz", ".tar")$/ARCHIVE_FORMS = (".tgz",)/' \
  "$work/forms/$GATE_REL"
grep -q '^ARCHIVE_FORMS = (".tgz",)$' "$work/forms/$GATE_REL" \
  || { echo "ОТКАЗ: инъекция перечня форм не легла — строка ARCHIVE_FORMS изменилась" >&2; exit 2; }
expect "перечень форм отстал от сканера — отказ предпосылки" "$work/forms" 2 \
  "перечень форм архива-чарта разошёлся с поведением сканера"

echo "итог: утверждений $((passed+failed)); пройдено $passed; провалено $failed"
[ "$failed" = 0 ] || exit 1
