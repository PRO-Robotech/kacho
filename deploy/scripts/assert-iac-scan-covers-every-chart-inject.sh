#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Доказательство падучести гейта `assert-iac-scan-covers-every-chart.py` — инъекцией
# настоящим входом, в ОБЕ стороны.
#
# Оси: (1) архив-чарт распознаётся по ОГЛАВЛЕНИЮ (Chart.yaml на любой глубине, tar
# любого сжатия и zip), а не по имени и не по исходу рендера; (2) место архива-чарта —
# `deploy/helm/vendor`, вне его — находка, в том числе для форм, которых сканер не
# рендерит; (3) в каталоге вендоренных каждый архив-чарт даёт цели проходу без
# заглушек, а ERROR в журнале этого прохода — находка с текстом; (4) шаги CI сверены
# с проходами, гейтовый шаг не бывает немым (`continue-on-error`, ложный `if`, `if`,
# снимаемый упавшим предыдущим шагом).
#
# Близнец опыта меняет РОВНО одно: тот же архив в каталоге вендоренных вместо места
# вне его; тот же чарт со значением в values.yaml вместо пустого `required`; тот же
# архив без Chart.yaml. Близнец опытов над шагами CI — контроль A.
#
# Всё исполняется в КОПИИ дерева вне репозитория (`git clone --shared` + наложение
# правленых отслеживаемых файлов): рабочая копия не меняется ни на байт.
#
# ЗНАМЕНАТЕЛЬ — 29 утверждений; итог печатает число исполненных, и расхождение с
# этим числом — тоже повод не верить зелёному.
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
expect "контроль — архив осмотрен проходом вендоренных" "$work/control" 0 \
  "целей  deploy/helm/vendor/$ARCHIVE"
expect "контроль — сжатый поток без оглавления назван «не чарт»" "$work/control" 0 \
  "сжатый поток без оглавления"

# ── B. Настоящий вендоренный архив вне своего каталога ──────────────────────────
make_copy "$work/archive" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
git -C "$work/archive" mv "deploy/helm/vendor/$ARCHIVE" "deploy/helm/$ARCHIVE" || exit 2
expect "архив вне своего каталога — находка «вне vendor»" "$work/archive" 1 \
  "deploy/helm/$ARCHIVE — архив-чарт вне deploy/helm/vendor"
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

# $1 — копия, $2 — путь архива в ней, $3 — вид чарта, $4 — префикс путей внутри
# архива (по умолчанию пусто: `injchart/…`). Виды: closed | open (схема значений
# закрыта / открыта) · required (`{{ required }}` без значения, как у registry и nlb)
# · required_valued (то же, значение в values.yaml) · broken (шаблон не разбирается)
# · nochart (архив без Chart.yaml). Форма по расширению в любом регистре: .zip —
# zip, .tgz/.tar.gz — tar+gzip, иначе — tar.
put_chart_archive() {
  python3 - "$1/$2" "$3" "${4:-}" <<'PY' && git -C "$1" add -- "$2"
import io, json, sys, tarfile, zipfile
dst, kind, prefix = sys.argv[1], sys.argv[2], sys.argv[3]
deploy = ("apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: injchart}\n"
          "spec:\n  selector: {matchLabels: {a: b}}\n  template:\n"
          "    metadata: {labels: {a: b}}\n    spec:\n"
          "      containers: [{name: c, image: \"{{ .Values.image }}\"}]\n")
files = {
    "Chart.yaml": "apiVersion: v2\nname: injchart\nversion: 0.1.0\n",
    "values.yaml": "image: injchart\n",
    "templates/deployment.yaml": deploy,
}
if kind in ("closed", "open"):
    files["values.schema.json"] = json.dumps({
        "type": "object", "properties": {"image": {"type": "string"}},
        "additionalProperties": kind != "closed"})
elif kind in ("required", "required_valued"):
    files["templates/secret.yaml"] = (
        "apiVersion: v1\nkind: Secret\nmetadata: {name: injchart}\n"
        "stringData: {password: {{ required \"password обязателен\" .Values.password | quote }}}\n")
    if kind == "required_valued":
        files["values.yaml"] += "password: inject-only\n"
elif kind == "broken":
    files["templates/deployment.yaml"] = deploy + "{{ .Values.image\n"
elif kind == "nochart":
    files = {"README.txt": "not a chart\n"}
else:
    sys.exit("неизвестный вид: " + kind)
low = dst.lower()
if low.endswith(".zip"):
    with zipfile.ZipFile(dst, "w") as z:
        for rel, body in files.items():
            z.writestr(prefix + "injchart/" + rel, body)
else:
    mode = "w:gz" if low.endswith((".tgz", ".tar.gz")) else "w"
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


# $1 — метка, $2 — имя файла, $3 — вид, $4 — префикс: пара «вне vendor — находка» /
# «тот же архив, схема открыта, в vendor — осмотрен и молчит».
pair_outside_and_vendored() {
  local tag="$1" file="$2" kind="$3" prefix="${4:-}"
  make_copy "$work/$tag-out" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
  put_chart_archive "$work/$tag-out" "deploy/helm/$file" "$kind" "$prefix" || exit 2
  expect "$tag: архив-чарт вне vendor — находка" "$work/$tag-out" 1 \
    "deploy/helm/$file — архив-чарт вне deploy/helm/vendor"
  make_copy "$work/$tag-in" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
  put_chart_archive "$work/$tag-in" "deploy/helm/vendor/$file" open "$prefix" || exit 2
  expect "$tag (близнец): тот же архив в vendor — осмотрен, гейт молчит" "$work/$tag-in" 0 \
    "целей  deploy/helm/vendor/$file"
}

# ── E–I. Формы, которые сканер рендерит: вне vendor — находка, в vendor — осмотр ──
pair_outside_and_vendored M3 closedc-0.1.0.tar.gz closed
pair_outside_and_vendored M4 closedc-0.1.0.tar closed
pair_outside_and_vendored wrap wrapc-0.1.0.tgz closed wrap/
pair_outside_and_vendored dot dotc-0.1.0.tgz closed ./

# ── G. M1 / M5: гейтовый шаг, который заведомо не судит ────────────────────────
make_copy "$work/m1" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
mute_vendored_gate_step "$work/m1" continue-on-error true || exit 2
expect "M1: гейтовый шаг под continue-on-error — находка" "$work/m1" 1 \
  "заведомо не судит: continue-on-error"
make_copy "$work/m5" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
mute_vendored_gate_step "$work/m5" if false || exit 2
expect "M5: гейтовый шаг под if: false — находка" "$work/m5" 1 \
  "заведомо не судит: if: False"


# ── J. 2в: прохода без заглушек нет в PASSES ─────────────────────────────────────
# Гейт берёт его поимённо; без него судить каталог вендоренных нечем — отказ с
# именем прохода, а не исключение. Близнец — контроль A: проход на месте.
make_copy "$work/nobare" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
sed -i '/^    (BARE_NAME, VENDOR_HOME, "trivy-vendored-charts.yaml", ALWAYS_SKIPPED),$/d' \
  "$work/nobare/deploy/scripts/iac_scan_passes.py"
if grep -q '^    (BARE_NAME,' "$work/nobare/deploy/scripts/iac_scan_passes.py"; then
  echo "ОТКАЗ: инъекция 2в не легла — строка прохода изменилась" >&2; exit 2
fi
expect "2в: прохода без заглушек нет — отказ с его именем" "$work/nobare" 2 \
  "прохода «вендоренные» нет в \`iac_scan_passes.PASSES\`"

# $1 — копия, $2 — точное имя шага, $3 — ключ; ключ СНИМАЕТСЯ
drop_step_key() {
  python3 - "$1/.github/workflows/security-scan.yml" "$2" "$3" <<'PY'
import sys, yaml
p, name, key = sys.argv[1], sys.argv[2], sys.argv[3]
d = yaml.safe_load(open(p, encoding="utf-8"))
hit = [st for st in d["jobs"]["trivy"]["steps"] if st.get("name") == name and key in st]
if len(hit) != 1:
    sys.exit("инъекция не нашла шаг «%s» с ключом %s: %d" % (name, key, len(hit)))
del hit[0][key]
yaml.safe_dump(d, open(p, "w", encoding="utf-8"), allow_unicode=True)
PY
}

# ── L. Гейтовый шаг, снимаемый отказом предыдущего (волна #2977) ────────────────
# Без `if` шаг исполняется только при успехе всех предыдущих: упавшая выгрузка SARIF
# сняла так гейт fs. Обе формы — шаг прохода и шаг вне IaC; близнец обеих — контроль A,
# где у шагов стоит `if: '!cancelled()'`.
make_copy "$work/fallcfg" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
drop_step_key "$work/fallcfg" "trivy config, вендоренные чарты (гейт CRITICAL/HIGH)" if || exit 2
expect "гейт прохода без if — снимается упавшим предыдущим, находка" "$work/fallcfg" 1 \
  "исполняется только при успехе всех предыдущих шагов"
make_copy "$work/fallfs" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
drop_step_key "$work/fallfs" "trivy fs (гейт CRITICAL/HIGH)" if || exit 2
expect "гейт fs без if — находка с его именем" "$work/fallfs" 1 \
  "шаг «trivy fs (гейт CRITICAL/HIGH)» (security-scan.yml) объявлен гейтовым"

# ── M. `required` без значения (как у registry и nlb): «не отрендерился» ≠ «не чарт» ──
make_copy "$work/req-out" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
put_chart_archive "$work/req-out" deploy/helm/reqc-0.1.0.tgz required || exit 2
expect "required вне vendor — находка «вне vendor», а не «не чарт»" "$work/req-out" 1 \
  "deploy/helm/reqc-0.1.0.tgz — архив-чарт вне deploy/helm/vendor"
make_copy "$work/req-in" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
put_chart_archive "$work/req-in" deploy/helm/vendor/reqc-0.1.0.tgz required || exit 2
expect "required в vendor — непокрыт" "$work/req-in" 1 \
  "deploy/helm/vendor/reqc-0.1.0.tgz — архив-чарт НЕ ДАЛ ни одной цели"
expect "required в vendor — текст отказа рендера из журнала" "$work/req-in" 1 \
  "Failed to render Chart files"
make_copy "$work/req-valued" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
put_chart_archive "$work/req-valued" deploy/helm/vendor/reqc-0.1.0.tgz required_valued || exit 2
expect "близнец: значение в values.yaml — осмотрен, гейт молчит" "$work/req-valued" 0 \
  "целей  deploy/helm/vendor/reqc-0.1.0.tgz"

# ── N. Рендер падает в vendor (шаблон не разбирается) ────────────────────────────
make_copy "$work/broken" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
put_chart_archive "$work/broken" deploy/helm/vendor/brokenc-0.1.0.tgz broken || exit 2
expect "рендер в vendor падает — находка с текстом журнала" "$work/broken" 1 \
  "в журнале trivy ERROR"

# ── O. Формы, которых сканер не рендерит: .zip и .TGZ с чартом вне vendor ─────────
make_copy "$work/zip" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
put_chart_archive "$work/zip" deploy/helm/zipc-0.1.0.zip open || exit 2
expect ".zip с чартом вне vendor — находка" "$work/zip" 1 \
  "deploy/helm/zipc-0.1.0.zip — архив-чарт вне deploy/helm/vendor"
make_copy "$work/zipnochart" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
put_chart_archive "$work/zipnochart" deploy/helm/docs-0.1.0.zip nochart || exit 2
expect "близнец: .zip без Chart.yaml — «не чарт», гейт молчит" "$work/zipnochart" 0 \
  "deploy/helm/docs-0.1.0.zip — Chart.yaml в оглавлении нет"
make_copy "$work/upper" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
put_chart_archive "$work/upper" deploy/helm/upperc-0.1.0.TGZ open || exit 2
expect ".TGZ с чартом вне vendor — находка" "$work/upper" 1 \
  "deploy/helm/upperc-0.1.0.TGZ — архив-чарт вне deploy/helm/vendor"

# ── P. Непрочитанный архив — находка, а не «не чарт» ─────────────────────────────
# Близнец — контроль A: сжатый поток без оглавления прочитан и назван «не чарт».
make_copy "$work/junk" || { echo "ОТКАЗ: копия дерева не собрана" >&2; exit 2; }
printf 'not an archive at all\n' > "$work/junk/deploy/helm/junk-0.1.0.tgz"
git -C "$work/junk" add -- deploy/helm/junk-0.1.0.tgz || exit 2
expect "непрочитанный архив — находка" "$work/junk" 1 \
  "deploy/helm/junk-0.1.0.tgz — архив не прочитан"

echo "итог: утверждений $((passed+failed)); пройдено $passed; провалено $failed (знаменатель 29)"
[ "$failed" = 0 ] && [ "$((passed+failed))" = 29 ] || exit 1
