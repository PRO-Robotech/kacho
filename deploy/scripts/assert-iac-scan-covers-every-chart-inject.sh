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
# восстанавливать нечего. Прогонов семнадцать: контроль · архив вне своего каталога ·
# срез шага разошёлся · проход без гейтового шага · опыты приёмки 7b9d560610b:
# M3 (`.tar.gz` с закрытой схемой) и близнец M2 (та же форма, схема открыта) · M4
# (`.tar` с закрытой схемой) и близнец M4-twin · M1 (гейтовый шаг под
# `continue-on-error: true`) · M5 (гейтовый шаг под `if: false`) · опыты приёмки
# ba3200fb3a1: вложенный архив `wrap/<чарт>/…` · `./`-архив, каждый с близнецом ·
# 2в (прохода без заглушек нет в PASSES) · `.TGZ` назван «не чартом для сканера».
# Близнец опыта с архивом — тот же архив той же формы в
# том же месте, у которого меняется РОВНО одно: схема значений открыта, и заглушки
# его рендер не роняют. Близнец M1, M5 и 2в — контроль A.
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

# $1 — копия, $2 — путь архива в ней, $3 — closed|open (схема значений),
# $4 — префикс путей внутри архива (по умолчанию пусто: `injchart/…`)
put_chart_archive() {
  python3 - "$1/$2" "$3" "${4:-}" <<'PY' && git -C "$1" add -- "$2"
import io, json, sys, tarfile
dst, schema, prefix = sys.argv[1], sys.argv[2], sys.argv[3]
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
mode = "w:gz" if dst.lower().endswith((".tgz", ".tar.gz")) else "w"
with tarfile.open(dst, mode) as t:
    for rel, body in files.items():
        data = body.encode()
        info = tarfile.TarInfo(prefix + "injchart/" + rel)
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

# ── H. Вложенный архив: `wrap/<чарт>/Chart.yaml` ──────────────────────────────
# Сканер рендерит его как чарт (замер trivy 0.70.0); третья редакция гейта судила по
# глубине Chart.yaml и объявляла его «не чартом». Близнец — тот же архив, схема открыта.
make_copy "$work/wrap" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
put_chart_archive "$work/wrap" deploy/helm/wrapc-0.1.0.tgz closed wrap/ || exit 2
expect "вложенный архив с закрытой схемой — гейт краснеет и называет его" "$work/wrap" 1 \
  "deploy/helm/wrapc-0.1.0.tgz — архив чарта НЕ ДАЛ сканеру ни одной цели"
make_copy "$work/wraptwin" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
put_chart_archive "$work/wraptwin" deploy/helm/wrapo-0.1.0.tgz open wrap/ || exit 2
expect "близнец: вложенный архив с открытой схемой — осмотрен, гейт молчит" "$work/wraptwin" 0 \
  "целей  deploy/helm/wrapo-0.1.0.tgz"

# ── I. `./`-архив: пути внутри начинаются с `./` ──────────────────────────────────
make_copy "$work/dot" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
put_chart_archive "$work/dot" deploy/helm/dotc-0.1.0.tgz closed ./ || exit 2
expect "./-архив с закрытой схемой — гейт краснеет и называет его" "$work/dot" 1 \
  "deploy/helm/dotc-0.1.0.tgz — архив чарта НЕ ДАЛ сканеру ни одной цели"
make_copy "$work/dottwin" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
put_chart_archive "$work/dottwin" deploy/helm/doto-0.1.0.tgz open ./ || exit 2
expect "близнец: ./-архив с открытой схемой — осмотрен, гейт молчит" "$work/dottwin" 0 \
  "целей  deploy/helm/doto-0.1.0.tgz"

# ── J. 2в: прохода без заглушек нет в PASSES ─────────────────────────────────────
# Гейт берёт его поимённо; без него одиночный прогон архива судить нечем — отказ с
# именем прохода, а не исключение. Близнец — контроль A: проход на месте.
make_copy "$work/nobare" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
sed -i '/^    (BARE_NAME, VENDOR_HOME, "trivy-vendored-charts.yaml", ALWAYS_SKIPPED),$/d' \
  "$work/nobare/deploy/scripts/iac_scan_passes.py"
if grep -q '^    (BARE_NAME,' "$work/nobare/deploy/scripts/iac_scan_passes.py"; then
  echo "ОТКАЗ: инъекция 2в не легла — строка прохода изменилась" >&2; exit 2
fi
expect "2в: прохода без заглушек нет — отказ с его именем" "$work/nobare" 2 \
  "прохода «вендоренные» нет в \`iac_scan_passes.PASSES\`"

# ── K. `.TGZ`: сканер такой файл чартом не рендерит (замер trivy 0.70.0) ──────────
# Он не предмет гейта и обязан быть НАЗВАН отдельной строкой, а не пропущен молча.
make_copy "$work/upper" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
put_chart_archive "$work/upper" deploy/helm/upperc-0.1.0.TGZ closed || exit 2
expect ".TGZ — не чарт для сканера, назван отдельно, гейт молчит" "$work/upper" 0 \
  "не чарт для сканера deploy/helm/upperc-0.1.0.TGZ"

echo "итог: утверждений $((passed+failed)); пройдено $passed; провалено $failed"
[ "$failed" = 0 ] || exit 1
