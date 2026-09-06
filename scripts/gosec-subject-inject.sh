#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# gosec-subject-inject.sh — доказательство, что гейт «у подавления gosec обязан
# быть предмет» СПОСОБЕН упасть, и падает ровно на том, ради чего заведён.
#
# # Зачем проба, если гейт написан верно
#
# Ровно потому, что написан верно. На чистом дереве он печатает ноль находок — и
# в этом состоянии он неотличим от гейта, который ослеп: обход пуст, распознаватель
# не знает ни одной формы, перепись подавлений не читается. Все три случая дают
# один и тот же зелёный. Различает их только настоящий вход.
#
# # Что доказывается, и почему именно ПАРАМИ
#
# Отрицание («директива без предмета — находка») зеленело бы на распознавателе,
# который не видит НИЧЕГО. Поэтому у каждой красной инъекции есть законный
# близнец, отличающийся РОВНО ОДНИМ фактом, на котором гейт обязан молчать:
#
#   A/B  ведомость: число разошлось · запись без предмета   ↔  ведомость как есть
#   C/D  директива с правилом, которое здесь НЕ срабатывает ↔  ТО ЖЕ МЕСТО, ТОТ ЖЕ
#        (находка)                                             код, ТА ЖЕ форма —
#                                                              и правило, которое
#                                                              здесь срабатывает
#                                                              (молчание)
#   E    журнал скана без перечня прочитанного              →  код 2, не 0 и не 1
#   F    модуль пропал из перечня скана                     →  код 2, не 0 и не 1
#   G/H  вызывающие (локальный прогонщик и объявление         →  три исхода читаются
#        конвейера) на кодах 0 · 1 · 2 · неожиданном             РАЗДЕЛЬНО
#
# Пара C/D — несущая. Она меняет один факт: идентификатор правила в директиве.
# Код, файл, строка, форма комментария и причина у обоих одинаковы, поэтому
# красное у C не может прийти «от соседа».
#
# G и H проверяют ДРУГОЙ предмет: не гейт, а того, кто его зовёт. Коды возврата
# у гейта свои и правильные, но прочитать их может только вызывающий, и здесь
# это уже стоило одной правки — `go run` схлопывал любой ненулевой код в
# единицу, и третий исход доезжал неотличимым от находки. Оба блока берутся ИЗ
# своих файлов, а не переписываются: копия разошлась бы с оригиналом молча и
# доказывала бы свойство копии.
#
# # Третий исход проверяется отдельно (E, F)
#
# «Гейт не смог вынести вердикт» — не находка и не чистота. Схлопни его в любую
# из двух сторон, и непрочитанное дерево отчиталось бы наравне с прочитанным.
#
# # Чего проба НЕ делает
#
# Она не судит, обоснованы ли записи ведомости по существу: это вопрос человека,
# и гейт его не решает. И она не заменяет модульных проб распознавателя
# (tools/gosecsubject) — те гоняются без сканера и без дерева.
#
# Скрипт ПРАВИТ рабочую копию и возвращает её обратно. Он отказывается стартовать
# на грязном дереве и сверяет восстановление в конце: «вернул» — это факт, который
# показывают, а не заявление.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LEDGER="$ROOT/tools/gosecsubject/known-inert.tsv"
# Синтетика дописывается в УЖЕ ОТСЛЕЖИВАЕМЫЙ файл, а не заводится новым. Причина
# несущая: гейт берёт состав дерева из ИНДЕКСА git, поэтому новый файл он не
# прочитал бы вовсе — а сканер прочитал бы, и проба доказывала бы расхождение
# счётчиков вместо того, ради чего заведена. Плюс индекс чужой рабочей копии
# трогать нельзя ни при каких условиях.
FIXTURE="$ROOT/tools/gosecsubject/gosecsubject.go"
# Гейт зовётся СОБРАННЫМ БИНАРЁМ, а не через `go run`: тот схлопывает любой
# ненулевой код в единицу, и утверждения про третий исход ниже (E, F) проверяли
# бы поведение `go run`, а не гейта.
GATEPKG="./tools/gosecsubject/cmd/verify-gosec-suppression-subject"
WORK="$(mktemp -d)"
PASS=0
FAIL=0

cleanup() {
    [ -f "$WORK/fixture.orig" ] && cp "$WORK/fixture.orig" "$FIXTURE"
    [ -f "$WORK/ledger.orig" ] && cp "$WORK/ledger.orig" "$LEDGER"
    rm -rf "$WORK"
}
trap cleanup EXIT

say() { printf '\n── %s\n' "$1"; }
ok()  { PASS=$((PASS + 1)); printf '   ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '   БЕДА %s\n' "$1"; }

# Грязное дерево — отказ, а не «разберёмся»: инъекция правит файлы, и отличить
# своё изменение от чужого потом будет нечем.
if [ -n "$(git -C "$ROOT" status --porcelain -- tools/gosecsubject scripts 2>/dev/null)" ]; then
    echo "ОТКАЗ: в tools/gosecsubject или scripts есть незакоммиченные правки." >&2
    echo "       Инъекция правит эти файлы и восстанавливает их; на грязном дереве" >&2
    echo "       восстановление нечем доказать." >&2
    exit 2
fi
cp "$LEDGER" "$WORK/ledger.orig"
cp "$FIXTURE" "$WORK/fixture.orig"

# ── ПИННУТЫЙ СКАНЕР ─────────────────────────────────────────────────────────
# Версия берётся оттуда же, откуда её берёт конвейер. «Тот же gosec» — это тот
# же ПИН: с другой сборкой множество находок другое, и вердикт этой пробы
# относился бы к чужому инструменту.
PIN=$(grep -oE 'gosec/v2/cmd/gosec@v[0-9.]+' "$ROOT/.github/workflows/security-scan.yml" | head -1)
if [ -z "$PIN" ]; then
    echo "ОТКАЗ: пин gosec не найден в security-scan.yml — сканировать нечем" >&2
    exit 2
fi
GOSEC_BIN=${GOSEC_BIN:-}
if [ -z "$GOSEC_BIN" ]; then
    echo "ставлю gosec ${PIN##*@} (как в конвейере)…"
    if ! GOBIN="$WORK/bin" go install "github.com/securego/$PIN" > "$WORK/install.txt" 2>&1; then
        echo "ОТКАЗ: пиннутый gosec не поставился — проба НЕ ВЫПОЛНЕНА" >&2
        tail -5 "$WORK/install.txt" >&2
        exit 2
    fi
    GOSEC_BIN="$WORK/bin/gosec"
fi

if ! ( cd "$ROOT" && go build -o "$WORK/gate-bin" "$GATEPKG" ) > "$WORK/build.txt" 2>&1; then
    echo "ОТКАЗ: гейт не собрался — проба НЕ ВЫПОЛНЕНА" >&2
    sed 's/^/  | /' "$WORK/build.txt" >&2
    exit 2
fi

scan() {
    GOSEC_BIN="$GOSEC_BIN" GOSEC_OUT="$WORK" "$ROOT/scripts/gosec-scan-modules.sh" \
        > "$WORK/scan.txt" 2>&1
}
gate() {
    "$WORK/gate-bin" "$ROOT" "$WORK" > "$WORK/gate.txt" 2>&1
    echo $?
}

# ── КОНТРОЛЬ: всё цело ──────────────────────────────────────────────────────
say "контроль: дерево как есть — гейт обязан молчать"
scan
rc=$(gate)
if [ "$rc" -eq 0 ]; then ok "код 0 на нетронутом дереве"; else
    bad "код $rc на нетронутом дереве — дальше вся проба недействительна"
    sed 's/^/     | /' "$WORK/gate.txt"
    printf '\nитог: годно %d, беда %d\n' "$PASS" "$FAIL"
    exit 1
fi
grep -q 'директив прочитано' "$WORK/gate.txt" && ok "перепись напечатана" \
    || bad "переписи нет — «ноль находок» неотличимо от «ноль прочитанного»"
cp "$WORK/gate.txt" "$WORK/control.txt"

# ── A: число в ведомости разошлось ──────────────────────────────────────────
say "A: число в ведомости завышено на единицу"
awk -F'\t' 'BEGIN{OFS="\t"} /^services\/compute\/internal\/protoconv/ {$3=$3+1} {print}' \
    "$WORK/ledger.orig" > "$LEDGER"
rc=$(gate)
[ "$rc" -eq 1 ] && ok "код 1" || bad "код $rc, ожидалась находка"
grep -q 'число разошлось' "$WORK/gate.txt" && ok "отказ называет предмет: число ТОЧНОЕ, а не потолок" \
    || bad "отказ не называет предмет: $(head -c 200 "$WORK/gate.txt")"
cp "$WORK/ledger.orig" "$LEDGER"

# ── B: запись, которой нечего исключать ─────────────────────────────────────
say "B: в ведомость дописана запись без предмета"
{ cat "$WORK/ledger.orig"
  printf 'services/geo/cmd/kacho-geo/main.go\tG304\t1\tсинтетика инъекции\n'; } > "$LEDGER"
rc=$(gate)
[ "$rc" -eq 1 ] && ok "код 1" || bad "код $rc, ожидалась находка"
grep -q 'исключать больше нечего' "$WORK/gate.txt" && ok "отказ называет предмет" \
    || bad "отказ не называет предмет: $(head -c 200 "$WORK/gate.txt")"
cp "$WORK/ledger.orig" "$LEDGER"

# ── B': запись без причины ──────────────────────────────────────────────────
# Причина — то единственное, что отличает принятое решение от проглоченного
# отказа: машинно они неразличимы. Запись без неё судить нечем, поэтому исход
# третий (код 2), а не находка.
say "B': запись в ведомости без причины"
{ cat "$WORK/ledger.orig"
  printf 'services/geo/cmd/kacho-geo/main.go\tG304\t1\t\n'; } > "$LEDGER"
rc=$(gate)
[ "$rc" -eq 2 ] && ok "код 2 — вердикт не выносится" || bad "код $rc, ожидался 2"
grep -q 'без причины' "$WORK/gate.txt" && ok "отказ называет предмет" \
    || bad "отказ не называет предмет: $(head -c 200 "$WORK/gate.txt")"
cp "$WORK/ledger.orig" "$LEDGER"

# ── C и D: ОДИН И ТОТ ЖЕ КОД, РАЗНОЕ ПРАВИЛО В ДИРЕКТИВЕ ────────────────────
# Функция ниже читает файл по пути из переменной — на этом gosec срабатывает
# правилом G304. Директива, называющая G304, находку гасит: предмет есть.
# Директива, называющая G115, не гасит ничего: правило по этой координате не
# срабатывает. Отличие между C и D — ровно четыре знака в тексте комментария,
# всё остальное (файл, строка, код, форма комментария, причина) совпадает.
FIXLINE=0
fixture() {
    cp "$WORK/fixture.orig" "$FIXTURE"
    cat >> "$FIXTURE" <<GOFIX

// injectionFixtureRead — синтетика пробы scripts/gosec-subject-inject.sh.
// Дописывается ею же и снимается в конце; в дереве этой функции нет.
func injectionFixtureRead(path string) ([]byte, error) {
	return os.ReadFile(path) // #nosec $1 -- синтетика инъекции
}
GOFIX
    FIXLINE=$(grep -n '#nosec' "$FIXTURE" | tail -1 | cut -d: -f1)
}

say "C: директива называет правило, которое по этой координате НЕ срабатывает"
fixture G115
scan
rc=$(gate)
[ "$rc" -eq 1 ] && ok "код 1" || bad "код $rc, ожидалась находка"
if grep -q "gosecsubject.go:$FIXLINE: #nosec G115" "$WORK/gate.txt"; then
    ok "находка названа КООРДИНАТОЙ (файл и строка $FIXLINE)"
else
    bad "находки по координате gosecsubject.go:$FIXLINE нет"
    grep 'gosecsubject.go' "$WORK/gate.txt" | sed 's/^/     | /'
fi
# Инъекция обязана ронять ТОЛЬКО проверяемое: прочие находки не должны появиться.
before=$(grep -c 'подавляет НИЧЕГО' "$WORK/control.txt")
after=$(grep -c 'подавляет НИЧЕГО' "$WORK/gate.txt")
[ "$after" -eq $((before + 1)) ] && ok "прибавилась РОВНО одна находка ($before → $after)" \
    || bad "находок было $before, стало $after — инъекция задела не только свой предмет"

say "D: ТОТ ЖЕ код и та же форма, правило — то, которое здесь срабатывает"
fixture G304
scan
rc=$(gate)
[ "$rc" -eq 0 ] && ok "код 0 — законный близнец молчит" || bad "код $rc на живом предмете"
grep -q "gosecsubject.go:$FIXLINE" "$WORK/gate.txt" \
    && bad "гейт назвал находкой директиву с ЖИВЫМ предметом" \
    || ok "директива с живым предметом в находки не попала"
cp "$WORK/fixture.orig" "$FIXTURE"

# ── E: журнал скана молчит о прочитанном ────────────────────────────────────
say "E: из журнала скана убран перечень прочитанных файлов"
scan
grep -v 'Checking file:' "$WORK/gosec---scan.log" > "$WORK/trimmed.log"
cp "$WORK/gosec---scan.log" "$WORK/scanlog.orig"
cp "$WORK/trimmed.log" "$WORK/gosec---scan.log"
rc=$(gate)
[ "$rc" -eq 2 ] && ok "код 2 — «не выполнилось», а не чистота и не находка" || bad "код $rc, ожидался 2"
grep -q 'Checking file' "$WORK/gate.txt" && ok "отказ называет предмет" \
    || bad "отказ не называет предмет: $(head -c 200 "$WORK/gate.txt")"
cp "$WORK/scanlog.orig" "$WORK/gosec---scan.log"

# ── F: модуль пропал из перечня скана ───────────────────────────────────────
say "F: модуль службы пропал из перечня скана"
grep -v '^services/iam' "$WORK/gosec-suppressions-manifest.txt" > "$WORK/m.tmp"
mv "$WORK/m.tmp" "$WORK/gosec-suppressions-manifest.txt"
rc=$(gate)
[ "$rc" -eq 2 ] && ok "код 2 — вердикт по неполному дереву не выносится" || bad "код $rc, ожидался 2"
grep -q 'services/iam' "$WORK/gate.txt" && ok "отказ называет модуль поимённо" \
    || bad "отказ не называет модуль: $(head -c 200 "$WORK/gate.txt")"

# ── G и H: ВЫЗЫВАЮЩИЕ ЧИТАЮТ ТРИ ИСХОДА, А НЕ ДВА ───────────────────────────
# Подставной `go build` кладёт на место гейта заглушку, выходящую заданным кодом.
# Проверяется, что вызывающий различает 0 · 1 · 2 и не засчитывает неожиданный
# код в успех.
probe_caller() {
    local name="$1" block="$2" want="$3" expect="$4"
    local box; box=$(mktemp -d)
    mkdir -p "$box/binpath" "$box/root"
    cat > "$box/binpath/go" <<GOSTUB
#!/usr/bin/env bash
if [ "\$1" = build ]; then
    out=""; prev=""
    for a in "\$@"; do [ "\$prev" = "-o" ] && out="\$a"; prev="\$a"; done
    printf '#!/usr/bin/env bash\nexit %s\n' "$want" > "\$out"; chmod +x "\$out"; exit 0
fi
exit 0
GOSTUB
    chmod +x "$box/binpath/go"
    local out rc=0
    out=$(cd "$box/root" && PATH="$box/binpath:$PATH" bash -c "
        set -uo pipefail
        ROOT='$box/root'; WORK='$box'; fails=()
        probe() {
$block
        }
        probe
        printf 'ОТКАЗОВ=%d [%s]\n' \"\${#fails[@]}\" \"\${fails[*]-}\"
    " 2>&1) || rc=$?
    if printf '%s' "$out" | grep -q "$expect"; then
        ok "$name: код $want → $expect"
    else
        bad "$name: код $want не дал «$expect»: $(printf '%s' "$out" | tr '\n' '|' | head -c 200)"
    fi
    rm -rf "$box"
}

say "G: локальный прогонщик различает исходы гейта"
CALLER_BLOCK=$(awk '/# У ПОДАВЛЕНИЯ ОБЯЗАН БЫТЬ ПРЕДМЕТ/,/^                esac$/' \
    "$ROOT/scripts/ci-local.sh" | sed 's/^                //')
if [ -z "$CALLER_BLOCK" ]; then
    bad "G: блок вызова не найден в ci-local.sh — предмет пробы исчез, а сама она смолчала бы"
else
    probe_caller "ci-local" "$CALLER_BLOCK" 0 'ОТКАЗОВ=0'
    probe_caller "ci-local" "$CALLER_BLOCK" 1 'подавление без предмета'
    probe_caller "ci-local" "$CALLER_BLOCK" 2 'НЕ ВЫНЕС вердикта (код 2)'
    probe_caller "ci-local" "$CALLER_BLOCK" 7 'НЕ ВЫНЕС вердикта (код 7)'
fi

say "H: объявление конвейера различает исходы гейта"
WF_BLOCK=$(python3 - "$ROOT/.github/workflows/security-scan.yml" <<'PY'
import sys, yaml
d = yaml.safe_load(open(sys.argv[1], encoding='utf-8'))
for st in d['jobs']['gosec']['steps']:
    if 'gosecsubject' in str(st.get('run', '')):
        sys.stdout.write(st['run'])
        break
PY
)
if [ -z "$WF_BLOCK" ]; then
    bad "H: шаг вызова гейта не найден в security-scan.yml"
else
    probe_caller "конвейер" "$WF_BLOCK" 0 'есть предмет'
    probe_caller "конвейер" "$WF_BLOCK" 1 'без предмета'
    probe_caller "конвейер" "$WF_BLOCK" 2 'НЕ ВЫНЕС вердикта (код 2)'
    probe_caller "конвейер" "$WF_BLOCK" 7 'НЕ ВЫНЕС вердикта (код 7)'
fi

# ── ВОССТАНОВЛЕНИЕ — ФАКТ, А НЕ ЗАЯВЛЕНИЕ ───────────────────────────────────
say "восстановление рабочей копии"
cp "$WORK/fixture.orig" "$FIXTURE"
cp "$WORK/ledger.orig" "$LEDGER"
dirty=$(git -C "$ROOT" status --porcelain -- tools/gosecsubject scripts)
[ -z "$dirty" ] && ok "дерево вернулось к исходному (git status пуст)" \
    || bad "дерево НЕ восстановлено:
$dirty"

printf '\nитог: утверждений годно %d, беда %d\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
