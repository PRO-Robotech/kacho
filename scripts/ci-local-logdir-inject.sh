#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Инъекция для `scripts/ci-local.sh` — предмет один: КАТАЛОГ ЖУРНАЛОВ ПРОГОНА (#2852).
#
# ЗАЧЕМ. Отказ шага печатает путь к своему журналу (`| целиком: …`), и разбор
# красного начинается с этого файла. Каталог журналов выводился только из пути
# копии, имя журнала — из номера и слага проверки, запись шла через `>`: повтор
# отправки из той же копии затирал журнал красного прогона, и напечатанный путь
# вёл уже в журнал следующего. Разобрать первое красное было нечем.
#
# ПОЧЕМУ ОТДЕЛЬНАЯ ПРОБА. Соседняя `ci-local-outcome-inject.sh` судит
# классификацию исхода и каждому сценарию задаёт `CI_LOCAL_WORK` — то есть ветку
# «каталог по умолчанию» не исполняет НИ РАЗУ. Здесь ручка не задана, `TMPDIR`
# указывает в песочницу пробы, и в одной копии идут несколько прогонов подряд —
# БЕЗ паузы между ними: идентификатор прогона от часов с секундной точностью
# обязан покраснеть здесь, а не пройти потому, что проба подождала.
#
# СВОЙСТВ ШЕСТЬ, у каждого своя метка и свой воссозданный дефект:
#   A  два прогона одной копии пишут в РАЗНЫЕ каталоги журналов;
#   B  журнал отказа первого прогона после второго лежит по напечатанному пути
#      и содержит строку отказа;
#   K  ручка `CI_LOCAL_WORK` смысла не меняет: при ней оба прогона печатают
#      ровно `ci-local: логи   <ручка>`, без подкаталога прогона;
#   C  две РАЗНЫЕ копии не делят каталога — ни кэша, ни журналов (#677);
#   H  кэш переживает повтор: tofu качается один раз на два прогона, инструменты
#      под пином ставятся в кэш копии, а не в каталог прогона;
#   R  уборка: после CI_LOCAL_RUNS_KEPT+2 прогонов живут ровно CI_LOCAL_RUNS_KEPT
#      последних, включая текущий, а первый снят.
# Свойство A держит не только журналы шагов, но и всех, кто пишет в каталог
# прогона сам (сверка модулей, скан gosec, судья разрывов): поэтому оно судится
# по каталогу, а не по имени одного журнала — дефект «идентификатор только в
# имени журнала» обязан краснеть здесь, хотя B у него зелёный.
#
# ПРОБА ДОКАЗЫВАЕТ СВОЮ СПОСОБНОСТЬ УПАСТЬ САМА: те же утверждения гоняются
# против настоящего прогонщика (ждём ноль провалов) и против семи воссозданных
# дефектов, и у каждого дефекта обязана покраснеть ЕГО метка — «хотя бы один
# провал» пропустил бы дефект, красный от соседа.
#
# Исходы — по контракту доказательств дерева: 0 доказано · 1 провалено ·
# 2 условие не создано (предмета нет, дефект не воссоздан).
set -uo pipefail

# Окружение git обрывается по общей причине (см. prepush-groups-inject.sh):
# унаследованный GIT_DIR сильнее рабочего каталога.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CI_LOCAL_SUBJECT="$HERE/ci-local.sh"
[ -r "$CI_LOCAL_SUBJECT" ] || { echo "предмета нет: $CI_LOCAL_SUBJECT" >&2; exit 2; }
# shellcheck source=scripts/ci-local-copy.sh
. "$HERE/ci-local-copy.sh" || { echo "способа копирования нет: $HERE/ci-local-copy.sh" >&2; exit 2; }

# Глубина уборки читается из ОБЪЯВЛЕНИЯ прогонщика, а не выписывается здесь
# вторым числом: иначе смена глубины красила бы пробу, а не проверялась ею.
# Объявления нет — это находка свойства R, а не отказ пробы: прогонщик без уборки
# и есть тот, у кого каталог растёт на каждой отправке. Прогонов тогда столько,
# сколько было бы при глубине 5.
KEEP="$(sed -n 's/^CI_LOCAL_RUNS_KEPT=\([0-9][0-9]*\)$/\1/p' "$CI_LOCAL_SUBJECT")"
R_RUNS=$(( ${KEEP:-5} + 2 ))

tmp="$(mktemp -d)" || exit 2
trap 'rm -rf "$tmp"' EXIT

# ─────────────────────────────────────────────────────────────────────────────
# Сценарии
# ─────────────────────────────────────────────────────────────────────────────
# Красный шаг — настоящая форма находки `go vet`; зелёный — тот же шаг с кодом 0.
printf '%s\n' "run 'go vet' bash -c \"printf 'vet: internal/repohygiene/foo.go:3:1: expected declaration\\\\n'; exit 1\"" \
    > "$tmp/scen-red.sh"
printf '%s\n' "run 'go vet' bash -c 'exit 0'" > "$tmp/scen-green.sh"

# КЭШИ: настоящие группы proto · go · terraform, но без их проверок — `run`,
# `proof` и сверка модулей здесь пустые, а сеть и сборка подменены заглушками
# (`go`, `curl`, `unzip`, `buf`, `jq`). Исполняется ровно то, что решает, КУДА
# ставятся инструменты и берётся ли tofu заново. Версию tofu заглушка берёт у
# самого прогонщика (`$TOFU_VERSION` виден сценарию, он сорсится в копии).
cat > "$tmp/scen-caches.sh" <<'SCEN'
run() { :; }
proof() { :; }
go_mod_tidy_check() { :; }
export STUB_TOFU_VERSION="$TOFU_VERSION"
export PATH="$STUB_BIN:$PATH"
proto_group
go_group
terraform_group
SCEN

mkdir -p "$tmp/stub-bin"
# go: записывает, КУДА его попросили положить результат, и кладёт пустой файл.
cat > "$tmp/stub-bin/go" <<'STUB'
#!/usr/bin/env bash
out=""; prev=""
for a in "$@"; do [ "$prev" = "-o" ] && out="$a"; prev="$a"; done
printf 'go %s GOBIN=%s OUT=%s\n' "$1" "${GOBIN:-}" "$out" >> "$STUB_LOG"
if [ -n "$out" ]; then mkdir -p "$(dirname "$out")" && : > "$out" && chmod +x "$out"; fi
exit 0
STUB
# curl: считает загрузки; кладёт архив туда, куда попросили.
cat > "$tmp/stub-bin/curl" <<'STUB'
#!/usr/bin/env bash
out=""; prev=""
for a in "$@"; do [ "$prev" = "-o" ] && out="$a"; prev="$a"; done
printf 'curl OUT=%s\n' "$out" >> "$STUB_LOG"
[ -n "$out" ] && : > "$out"
exit 0
STUB
# unzip: «распаковывает» исполнимый tofu той версии, что пинит прогонщик.
cat > "$tmp/stub-bin/unzip" <<'STUB'
#!/usr/bin/env bash
d=""; prev=""
for a in "$@"; do [ "$prev" = "-d" ] && d="$a"; prev="$a"; done
mkdir -p "$d"
printf '#!/usr/bin/env bash\necho "OpenTofu v%s"\n' "$STUB_TOFU_VERSION" > "$d/tofu"
chmod +x "$d/tofu"
exit 0
STUB
printf '#!/usr/bin/env bash\nexit 0\n' > "$tmp/stub-bin/buf"
printf '#!/usr/bin/env bash\nexit 0\n' > "$tmp/stub-bin/jq"
chmod +x "$tmp/stub-bin/"*

# ─────────────────────────────────────────────────────────────────────────────
# Прогон копии и чтение напечатанного
# ─────────────────────────────────────────────────────────────────────────────
# go_default — прогон копии ПО УМОЛЧАНИЮ: ручки нет, TMPDIR — песочница набора.
# go_knob — тот же прогон с ручкой.
go_default() { # go_default <копия> <сценарий> <вывод> <tmpdir> [журнал заглушек]
    env -u CI_LOCAL_WORK CI=1 TMPDIR="$4" CI_LOCAL_SYNTH_FILE="$tmp/scen-$2.sh" \
        STUB_BIN="$tmp/stub-bin" STUB_LOG="${5:-/dev/null}" \
        bash "$1" synth > "$3" 2>&1
}
go_knob() { # go_knob <копия> <сценарий> <вывод> <ручка>
    env CI=1 CI_LOCAL_WORK="$4" CI_LOCAL_SYNTH_FILE="$tmp/scen-$2.sh" \
        bash "$1" synth > "$3" 2>&1
}
logs_of()  { sed -n 's/^ci-local: логи   //p' "$1" | sed -n 1p; }
cache_of() { sed -n 's/^ci-local: кэш    //p' "$1" | sed -n 1p; }
whole_of() { sed -n 's/^   | целиком: //p' "$1" | sed -n 1p; }
under()    { case "$1/" in "$2"/*) return 0 ;; *) return 1 ;; esac; }  # under <путь> <каталог>
# Каталогов прогона в кэше копии. Это состояние ПЕСОЧНИЦЫ, а не дерева: индекса у
# него нет, поэтому считается раскрытием по форме имени, которую даёт прогонщик.
count_runs() { local n=0 r; for r in "$1"/run-*; do [ -d "$r" ] && n=$((n + 1)); done; echo "$n"; }

FAILS=0; ASSERTS=0; FAILED_TAGS=""
check() { # check <метка> <набор> <описание> <условие-команда…>
    local tag="$1" set="$2" what="$3"; shift 3
    ASSERTS=$((ASSERTS + 1))
    if "$@"; then
        printf '  ok   [%s] %s %s\n' "$set" "$tag" "$what"
    else
        printf '  FAIL [%s] %s %s\n' "$set" "$tag" "$what"
        FAILS=$((FAILS + 1))
        case " $FAILED_TAGS " in *" $tag "*) ;; *) FAILED_TAGS="$FAILED_TAGS $tag" ;; esac
    fi
}
nonempty_and_differ() { [ -n "$1" ] && [ -n "$2" ] && [ "$1" != "$2" ]; }
nonempty_and_same()   { [ -n "$1" ] && [ "$1" = "$2" ]; }
file_has()            { [ -n "$1" ] && [ -f "$1" ] && grep -qF -- "$2" "$1"; }
line_is()             { grep -qFx -- "$2" "$1"; }
count_is()            { [ "$1" = "$2" ]; }

assert_all() { # assert_all <набор> <sed-выражение дефекта|пусто>
    local set="$1" defect="$2" d="$tmp/$1" real="$tmp/real-ref/scripts/ci-local.sh"
    FAILS=0; ASSERTS=0; FAILED_TAGS=""
    mkdir -p "$d/tmpdir"
    # Копии — в подоболочке: невоссозданный дефект обрывает свой набор кодом 2,
    # а не всю пробу, и настоящий набор успевает сказать своё.
    local c
    for c in a k c1 c2 h r; do
        if [ -n "$defect" ]; then ( ci_local_copy "$d/$c/scripts/ci-local.sh" "$defect" "$real" )
        else ( ci_local_copy "$d/$c/scripts/ci-local.sh" ); fi || { printf 'unmet' > "$d.state"; return; }
    done
    # Пин сканера группа go читает из объявления конвейера в корне копии: без
    # него она отказывает раньше установки, и путь установки не был бы виден.
    mkdir -p "$d/h/.github/workflows"
    printf '          go install github.com/securego/gosec/v2/cmd/gosec@v2.0.0\n' \
        > "$d/h/.github/workflows/security-scan.yml"

    # A · B — две отправки одной копии подряд: красная, затем зелёная.
    go_default "$d/a/scripts/ci-local.sh" red   "$d/a-1.out" "$d/tmpdir"
    go_default "$d/a/scripts/ci-local.sh" green "$d/a-2.out" "$d/tmpdir"
    local la1 la2 w1
    la1="$(logs_of "$d/a-1.out")"; la2="$(logs_of "$d/a-2.out")"; w1="$(whole_of "$d/a-1.out")"
    check A "$set" "каталоги журналов двух прогонов различны ('$la1' · '$la2')" nonempty_and_differ "$la1" "$la2"
    check B "$set" "журнал отказа первого прогона пережил второй и несёт строку отказа ('$w1')" \
        file_has "$w1" 'expected declaration'
    check B "$set" "напечатанный путь журнала лежит в каталоге СВОЕГО прогона" \
        under "$w1" "$la1"

    # K — ручка: один каталог, названный вызывающим, строка сверяется ЦЕЛИКОМ.
    go_knob "$d/k/scripts/ci-local.sh" red   "$d/k-1.out" "$d/knob-work"
    go_knob "$d/k/scripts/ci-local.sh" green "$d/k-2.out" "$d/knob-work"
    check K "$set" "первый прогон с ручкой печатает ровно её" line_is "$d/k-1.out" "ci-local: логи   $d/knob-work"
    check K "$set" "второй прогон с ручкой печатает ровно её" line_is "$d/k-2.out" "ci-local: логи   $d/knob-work"

    # C — две разные копии (#677): ни кэш, ни журналы не общие.
    go_default "$d/c1/scripts/ci-local.sh" green "$d/c1.out" "$d/tmpdir"
    go_default "$d/c2/scripts/ci-local.sh" green "$d/c2.out" "$d/tmpdir"
    check C "$set" "у двух копий разные каталоги кэша" \
        nonempty_and_differ "$(cache_of "$d/c1.out")" "$(cache_of "$d/c2.out")"
    check C "$set" "журналы копии не лежат в кэше соседней" \
        eval '! under "$(logs_of "$d/c1.out")" "$(cache_of "$d/c2.out")"'

    # H — кэш переживает повтор, и инструменты ставятся в НЕГО.
    : > "$d/h-stub.log"
    go_default "$d/h/scripts/ci-local.sh" caches "$d/h-1.out" "$d/tmpdir" "$d/h-stub.log"
    go_default "$d/h/scripts/ci-local.sh" caches "$d/h-2.out" "$d/tmpdir" "$d/h-stub.log"
    local hc1 hc2 hl1 hl2 downloads gobins badpath=""
    hc1="$(cache_of "$d/h-1.out")"; hc2="$(cache_of "$d/h-2.out")"
    hl1="$(logs_of "$d/h-1.out")";  hl2="$(logs_of "$d/h-2.out")"
    check H "$set" "каталог кэша один на оба прогона копии ('$hc1')" nonempty_and_same "$hc1" "$hc2"
    downloads="$(grep -c '^curl ' "$d/h-stub.log")"
    check H "$set" "tofu скачан один раз на два прогона (скачиваний: $downloads)" count_is "$downloads" 1
    # Пути, по которым прогонщик ставит и собирает инструменты: GOBIN у `go
    # install` и -o у сборки провайдера. Каждый обязан лежать в кэше и НЕ лежать в
    # каталоге прогона. Судьи на прогон (-o в каталог прогона) сюда не входят.
    gobins="$(sed -n 's/^go install GOBIN=\([^ ]*\) .*/\1/p' "$d/h-stub.log")"
    check H "$set" "инструменты под пином ставились (записей go install: $(printf '%s\n' "$gobins" | grep -c .), ждём 4)" \
        count_is "$(printf '%s\n' "$gobins" | grep -c .)" 4
    local p
    for p in $gobins $(sed -n 's/^go build GOBIN=[^ ]* OUT=\(.*terraform-provider-kacho\)$/\1/p' "$d/h-stub.log"); do
        if ! under "$p" "$hc1" || under "$p" "$hl1" || under "$p" "$hl2"; then badpath="$badpath $p"; fi
    done
    check H "$set" "пути инструментов и провайдера — в кэше, вне каталога прогона${badpath:+ (не так:$badpath)}" \
        test -z "$badpath"
    check H "$set" "провайдер для приёмки собирался (записей: $(grep -c 'terraform-provider-kacho' "$d/h-stub.log"), ждём 2)" \
        count_is "$(grep -c 'terraform-provider-kacho' "$d/h-stub.log")" 2

    # R — уборка: KEEP+2 прогонов, живут KEEP последних.
    local i first last n
    for ((i = 1; i <= R_RUNS; i++)); do
        go_default "$d/r/scripts/ci-local.sh" green "$d/r-$i.out" "$d/tmpdir"
    done
    first="$(logs_of "$d/r-1.out")"; last="$(logs_of "$d/r-$R_RUNS.out")"
    n="$(count_runs "$(cache_of "$d/r-1.out")")"
    check R "$set" "глубина уборки объявлена (CI_LOCAL_RUNS_KEPT=${KEEP:-нет}, ждём ≥ 2)" test "${KEEP:-0}" -ge 2
    check R "$set" "после $R_RUNS прогонов живут ровно ${KEEP:-?} каталогов прогона (живут: $n)" count_is "$n" "$KEEP"
    check R "$set" "каталог последнего прогона на месте" test -n "$last" -a -d "$last"
    check R "$set" "каталог первого прогона снят" eval '[ -n "$first" ] && [ ! -e "$first" ]'

    printf '%s' "$FAILS" > "$d.fails"
    printf '%s' "$FAILED_TAGS" > "$d.tags"
    printf '%s' "$ASSERTS" > "$d.asserts"
}

# Эталон, с которым сверяется воссоздание каждого дефекта.
ci_local_copy "$tmp/real-ref/scripts/ci-local.sh"

# ─────────────────────────────────────────────────────────────────────────────
# Дефекты — у каждого своя метка
# ─────────────────────────────────────────────────────────────────────────────
# «метка|описание|sed-выражение»
DEFECTS=(
'A|идентификатор прогона — константа|s|^    WORK="$(mktemp -d "$_run_dir_template")"|    WORK="$CACHE/run-const"; mkdir -p "$WORK"|'
'A|идентификатор только в имени журнала, каталог прежний|s|^    WORK="$(mktemp -d "$_run_dir_template")"|    WORK="$CACHE"; true|;s|^    slug="$(printf|    slug="$$-$(printf|'
'K|ручка получила подкаталог прогона|s|^    WORK="$CI_LOCAL_WORK"$|    WORK="$CI_LOCAL_WORK/run-$$"|'
'C|ключ копии — константа|s|^    _tree_key=.*|    _tree_key=00000000|'
'H|кэш перенесён в каталог прогона|s|^    prune_runs$|    prune_runs; CACHE="$WORK"|'
'R|уборка снята|s|^    prune_runs$|    :|'
'R|уборка снимает новейшие вместо старших|s|^        rm -rf -- "${runs\[i\]}"$|        rm -rf -- "${runs[${#runs[@]} - 1 - i]}"|'
)

echo "── прогон против настоящего прогонщика (ждём ноль провалов)"
assert_all real ""
[ -f "$tmp/real.state" ] && { echo "ОТКАЗ: копия настоящего прогонщика не собралась — судить нечего" >&2; exit 2; }
real_fails="$(cat "$tmp/real.fails")"; real_asserts="$(cat "$tmp/real.asserts")"

defect_bad=0; defect_unmet=0
k=0
for entry in "${DEFECTS[@]}"; do
    k=$((k + 1))
    tag="${entry%%|*}"; rest="${entry#*|}"; what="${rest%%|*}"; expr="${rest#*|}"
    echo
    echo "── дефект $k «$what» (ждём красную метку $tag)"
    assert_all "defect-$k" "$expr"
    if [ -f "$tmp/defect-$k.state" ]; then
        printf '  →    НЕ ВЫПОЛНЕНО: дефект не воссоздан — форма кода изменилась, свойство %s не доказано\n' "$tag"
        defect_unmet=$((defect_unmet + 1)); continue
    fi
    got_tags="$(cat "$tmp/defect-$k.tags")"
    case " $got_tags " in
        *" $tag "*) printf '  →    метка %s покраснела (красные метки:%s)\n' "$tag" "$got_tags" ;;
        *) printf '  →    ОТКАЗ: метка %s НЕ покраснела (красные метки:%s) — свойство не держится\n' "$tag" "${got_tags:- нет}"
           defect_bad=$((defect_bad + 1)) ;;
    esac
done

echo
# Перепись — отдельное утверждение: «ноль провалов» отличимо от «ноль прогнанного».
printf 'ci-local-logdir-inject: утверждений на набор %s, наборов %d (настоящий + %d дефектов); глубина уборки %s\n' \
    "$real_asserts" "$((k + 1))" "$k" "${KEEP:-не объявлена}"
printf '  провалов у настоящего:               %s (норма 0)\n' "$real_fails"
printf '  дефектов, не покрасивших свою метку: %s (норма 0)\n' "$defect_bad"
printf '  дефектов, не воссозданных:           %s (норма 0)\n' "$defect_unmet"

[ "${real_asserts:-0}" -gt 0 ] || { echo "ОТКАЗ: утверждений ноль — проба беспредметна" >&2; exit 2; }
rc=0
[ "$real_fails" = "0" ] || { echo "ОТКАЗ: настоящий прогонщик не проходит собственных утверждений" >&2; rc=1; }
[ "$defect_bad" = "0" ] || { echo "ОТКАЗ: проба ЗЕЛЕНА на воссозданном дефекте — она не проверяет его свойство" >&2; rc=1; }
[ "$rc" = 0 ] && [ "$defect_unmet" -gt 0 ] && { echo "ВЕРДИКТА НЕТ по $defect_unmet дефектам — это не «доказано»" >&2; rc=2; }
exit "$rc"
