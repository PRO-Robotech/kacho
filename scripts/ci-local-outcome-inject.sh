#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Инъекция для `scripts/ci-local.sh` — предмет один: КЛАССИФИКАЦИЯ ИСХОДА ШАГА.
#
# ЗАЧЕМ. Прогонщик различает три исхода, и третий — «не выполнилось» — не
# вычитается из вердикта и не зачитывается в успех. Шаг, оборванный исчерпанием
# ресурса (место на диске, память), вердикта не даёт ВООБЩЕ: находок не получено
# ни одной, и подавать это красным значит посылать читателя искать поломку в
# своей ветке. Наблюдалось 2026-08-20: три имени в строке «красное», ноль находок,
# одна причина на всех три — `no space left on device` в журналах.
#
# ЧТО ДОКАЗЫВАЕТСЯ — ОБЕ СПОСОБНОСТИ, и вторая важнее первой:
#   * оборванный шаг уходит в «НЕ выполнено», прогон называет себя НЕДЕЙСТВИТЕЛЬНЫМ;
#   * шаг, упавший ПО СУЩЕСТВУ, остаётся красным — включая тот, чей текст сам
#     УПОМИНАЕТ исчерпание (в дереве такой есть: классификатор ceph и его проба).
# Без второго признак можно было бы расширить до «любой ненулевой код» — и защита
# от ложного красного стала бы маской, глотающей настоящие находки.
#
# ПОЧЕМУ КОПИЯ НАСТОЯЩЕГО ПРОГОНЩИКА, А НЕ ЕГО ПЕРЕСКАЗ. Утверждения гоняются по
# КОПИИ `ci-local.sh`, у которой подменён только блок выбора группы: сам механизм
# классификации исполняется тот же самый. Проба, воспроизводящая логику своими
# словами, проверяла бы свою копию, а не предмет.
#
# ПРОБА ДОКАЗЫВАЕТ СВОЮ СПОСОБНОСТЬ УПАСТЬ САМА. Один и тот же набор утверждений
# гоняется трижды: против настоящего прогонщика (ждём ноль провалов) и против
# ДВУХ воссозданных дефектов — «признак снят» и «признак расширен до любого
# отказа». Своё свойство — свой дефект: на первом дефекте утверждения про
# законных близнецов проехали бы, потому что он никого не глотает.
set -uo pipefail

# Окружение git обрывается по общей причине (см. prepush-groups-inject.sh):
# унаследованный GIT_DIR сильнее рабочего каталога, и тогда действия пробы
# уезжают в индекс той копии, из которой она запущена.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUBJECT="$HERE/ci-local.sh"
[ -r "$SUBJECT" ] || { echo "предмета нет: $SUBJECT" >&2; exit 2; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# ─────────────────────────────────────────────────────────────────────────────
# Копии прогонщика: настоящая и две дефектные
# ─────────────────────────────────────────────────────────────────────────────
# Блок выбора группы заменяется синтетическим — от `case "$GROUP" in` до `esac`
# в нулевой колонке. Всё остальное, включая классификацию исхода, берётся
# ДОСЛОВНО: подмена обязана быть узкой, иначе проба судит уже не тот код.
make_copy() { # make_copy <куда> [sed-выражение дефекта]
    local dest="$1" defect="${2:-}"
    mkdir -p "$(dirname "$dest")"
    awk '
        /^case "\$GROUP" in$/ { skip = 1 }
        skip && /^esac$/      { skip = 0
                                print "case \"$GROUP\" in"
                                print "    synth) . \"$CI_LOCAL_SYNTH_FILE\" ;;"
                                print "    *) echo \"копия принимает только группу synth\" >&2; exit 2 ;;"
                                print "esac"
                                next }
        !skip { print }
    ' "$SUBJECT" > "$dest"
    grep -q 'CI_LOCAL_SYNTH_FILE' "$dest" || {
        echo "подмена блока выбора группы не сработала — форма выбора группы изменилась" >&2; exit 2; }
    if [ -n "$defect" ]; then
        sed -i "$defect" "$dest"
        # ДЕФЕКТ ОБЯЗАН БЫТЬ ВОССОЗДАН, а не «применён»: `sed`, ничего не нашедший,
        # выходит успехом, и тогда «дефектная» копия побайтово равна настоящей —
        # проба краснела бы на обеих одинаково и ничего бы не доказывала. Первая
        # редакция этой пробы была именно такой.
        cmp -s "$dest" "$REAL" && {
            echo "дефект не воссоздан: копия $dest равна настоящей — форма кода изменилась" >&2
            exit 2; }
    fi
    chmod +x "$dest"
}

REAL="$tmp/real/scripts/ci-local.sh"
make_copy "$REAL"

# Дефект 1 — ПРИЗНАК СНЯТ. Ровно та форма, что стояла до issue #862: исчерпание
# ресурса приходит ненулевым кодом инструмента и классифицируется красным.
BROKEN_BLIND="$tmp/blind/scripts/ci-local.sh"
make_copy "$BROKEN_BLIND" 's/^    if step_aborted_by_exhaustion "$log"; then$/    if false; then/'

# Дефект 2 — ПРИЗНАК РАСШИРЕН до «любой отказ». Тот случай, ради которого в наборе
# стоят законные близнецы: защита от ложного красного, доведённая до маски.
BROKEN_WIDE="$tmp/wide/scripts/ci-local.sh"
make_copy "$BROKEN_WIDE" 's/^    if step_aborted_by_exhaustion "$log"; then$/    if true; then/'

# ─────────────────────────────────────────────────────────────────────────────
# Сценарии: синтетические шаги, чьи ТЕКСТЫ взяты у настоящих инструментов
# ─────────────────────────────────────────────────────────────────────────────
# Тексты не выдуманы. Три первых — дословно из журналов прогона, на котором
# заведена задача; остальные — формы, которые печатают Go-рантайм, ядро и node.
mk() { # mk <имя> <тело>
    printf '%s\n' "$2" > "$tmp/scen-$1.sh"
}

emit() { # emit <имя шага> <код возврата> <строки вывода…>
    local name="$1" rc="$2"; shift 2
    local body=""
    local l
    # Строка вывода экранируется как ЛИТЕРАЛ (`printf %q`): в настоящих сообщениях
    # сборщика Go стоит `$WORK` — это его собственная подстановка-заглушка, и без
    # экранирования её раскрыла бы уже наша оболочка. Тогда проба судила бы текст,
    # которого инструмент не печатает.
    for l in "$@"; do body+="printf '%s\\n' $(printf %q "$l"); "; done
    printf 'run "%s" bash -c %q\n' "$name" "${body}exit $rc"
}

mk space-compile "$(emit 'go build' 1 'compile: writing output: write $WORK/b761/_pkg_.a: no space left on device')"
mk space-link    "$(emit 'go build' 1 'link: mapping output file failed: no space left on device')"
mk space-mkdir   "$(emit 'go test -short' 1 'mkdir /tmp/go-build2947461823/b765/: no space left on device')"
# Форма git — с ПРОПИСНОЙ и с путём перед фразой. Наблюдалась живьём: прогон
# гейтов дерева упал на `git init` в собственной синтетической фикстуре.
mk space-git     "$(emit 'go test -short' 1 \
    '    deferredwork_test.go:181: git [init -q] в синтетическом дереве: exit status 1' \
    '        /tmp/TestSieve1369487545/001/.git/objects/info: No space left on device')"
mk mem-killed    "$(emit 'go test -short' 2 'go: build failed: signal: killed')"
mk mem-goruntime "$(emit 'go test -short' 2 'fatal error: out of memory')"
mk mem-node      "$(emit 'ui vpc: build' 134 'FATAL ERROR: Ineffective mark-compacts near heap limit Allocation failed - JavaScript heap out of memory')"
mk mem-enomem    "$(emit 'golangci-lint' 1 'failed to load: open /tmp/x: cannot allocate memory')"

# КЭШ СБОРКИ ИСЧЕЗ ИЗ-ПОД ИДУЩЕГО ПРОГОНА (#1431). На машине, где параллельно
# работают несколько копий дерева, кэш общий, а решение его подрезать принимает
# любая из копий: `go clean -cache` соседа — штатное событие, а не редкость.
# Инструмент отвечает про ОТКРЫТИЕ файла, которого больше нет; предмета шаг не
# достигает вовсе, поэтому его «отказ» вердиктом не является.
mk cache-gone-open "$(emit 'go build' 1 \
    'go: open /home/dk/.cache/go-build/3f/3fa1c0d7e1b2c3d4e5f60718293a4b5c6d7e8f90-d: no such file or directory')"
mk cache-gone-test "$(emit 'go test -short' 1 \
    'go: open /home/dk/.cache/go-build/trim.txt: no such file or directory')"
# ТУЛЧЕЙН ЖИВЁТ В МОДКЭШЕ (`go.mod` называет toolchain, и Go кладёт его в
# GOMODCACHE), поэтому чистка модкэша соседом уносит саму стандартную библиотеку.
# Форма сообщения — про `std`, и путь в скобках указывает В МОДКЭШ: это и есть
# якорь, разводящий её с настоящей находкой ниже.
mk toolchain-gone "$(emit 'go build' 1 \
    'main.go:5:2: package fmt is not in std (/home/dk/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.13.linux-amd64/src/fmt)')"

# ЗАКОННЫЕ БЛИЗНЕЦЫ — настоящие находки. Каждый обязан остаться КРАСНЫМ.
mk finding-lint  "$(emit 'golangci-lint' 1 'internal/repohygiene/foo.go:12:3: ineffectual assignment to err (ineffassign)')"
# Текст этой находки УПОМИНАЕТ исчерпание — и взят из дерева дословно
# (`services/storage/internal/clients/cephrbd/adapter_test.go`). Разбирает его
# регистр: `no space left on device` строчными печатает системная ошибка, а
# `No space left on device` с прописной — сообщение самого хранилища.
mk finding-ceph  "$(emit 'go test -short' 1 \
    '--- FAIL: TestClassifyOutcome/capacity' \
    '    adapter_test.go:213: rbd: write failed: (28) No space left on device — ждали CapacityExhausted' \
    'FAIL	github.com/PRO-Robotech/kacho/services/storage/internal/clients/cephrbd')"
mk finding-empty "$(emit 'go vet' 1)"
mk finding-prose "$(emit 'helm template' 1 'warning: disk space is low on this machine')"
# Проба, УТВЕРЖДАЮЩАЯ про снятый процесс: фразу она упоминает, но строку ею не
# замыкает. Это и есть предмет якоря `$` у признака про снятие.
mk finding-signal "$(emit 'go test -short' 1 \
    '--- FAIL: TestWorkerStopDrainsBacklog' \
    '    worker_test.go:88: ждали "signal: killed", получили ""')"
# НЕВЕРНЫЙ ИМПОРТ — настоящая находка, и её форма ОТЛИЧАЕТСЯ ОТ ИСЧЕЗНУВШЕГО
# ТУЛЧЕЙНА ТОЛЬКО ПУТЁМ В СКОБКАХ (замер: имя пакета различает их лишь тем, есть
# ли такой пакет в stdlib, а grep этого не знает). Здесь путь указывает в
# установленный GOROOT — значит стандартная библиотека на месте, и виноват код.
mk finding-import "$(emit 'go build' 1 \
    'main.go:2:8: package fmtx is not in std (/usr/lib/go-1.22/src/fmtx)')"
# Проба, чей текст УПОМИНАЕТ каталог кэша сборки, но не в форме «файла нет»:
# литералы `/tmp/go-build/...` живут в дереве (pkg/treecorpus), и признак,
# берущий их, глотал бы настоящее падение этой пробы.
mk finding-gobuild-prose "$(emit 'go test -short' 1 \
    '--- FAIL: TestCachedVerdictReadsTestlog' \
    '    cachedverdict_test.go:33: argv[0] = /tmp/go-build/b001/probe.test, ждали иное')"

# ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ обычного пропуска: «предмета нет» — это по-прежнему
# третий исход, но прогон он НЕДЕЙСТВИТЕЛЬНЫМ не делает и кода не меняет.
mk plain-skip "$(printf 'skip "ui shared: build" "скрипта нет в package.json"\n%s\n' "$(emit 'go build' 0)")"

# ВСЁ ЗЕЛЁНОЕ — идеал не превращён в поломку.
mk all-green "$(emit 'go build' 0)"

# СМЕШАННЫЙ: настоящая находка рядом с оборванным шагом.
mk mixed "$(printf '%s\n%s\n' \
    "$(emit 'golangci-lint' 1 'internal/repohygiene/foo.go:12:3: ineffectual assignment to err (ineffassign)')" \
    "$(emit 'go build' 1 'link: mapping output file failed: no space left on device')")"

# ─────────────────────────────────────────────────────────────────────────────
# Утверждения
# ─────────────────────────────────────────────────────────────────────────────
ASSERTS=0

# want <копия> <метка> <сценарий> <ожидаемый код> <есть-подстрока…|!отсутствует-подстрока…>
#
# Прогон делается ЗДЕСЬ, а не в отдельной функции: код возврата, выставленный
# внутри подстановки команд, до вызывающего не доходит — переменная живёт в
# подоболочке. Первая редакция пробы упала именно на этом.
want() {
    local copy="$1" tag="$2" scen="$3" want_rc="$4"; shift 4
    local out rc bad=0 need why=""
    ASSERTS=$((ASSERTS + 1))
    out="$(CI=1 CI_LOCAL_WORK="$tmp/work-$scen-$RANDOM" CI_LOCAL_SYNTH_FILE="$tmp/scen-$scen.sh" \
           bash "$copy" synth 2>&1)"
    rc=$?
    [ "$rc" = "$want_rc" ] || { bad=1; why="код $rc вместо $want_rc"; }
    for need in "$@"; do
        if [ "${need:0:1}" = "!" ]; then
            case "$out" in *"${need:1}"*) bad=1; why="$why; нашлось запрещённое «${need:1}»";; esac
        else
            case "$out" in *"$need"*) ;; *) bad=1; why="$why; не нашлось «$need»";; esac
        fi
    done
    if [ "$bad" = 0 ]; then
        printf '  ok   [%s] %s\n' "$tag" "$scen"
    else
        printf '  FAIL [%s] %s — %s\n' "$tag" "$scen" "${why#; }"
        FAILS=$((FAILS + 1))
    fi
}

NEDEYST='ПРОГОН НЕДЕЙСТВИТЕЛЕН'

assert_all() { # assert_all <копия> <метка> <файл для числа провалов>
    local copy="$1" tag="$2" out="$3"
    FAILS=0; ASSERTS=0

    # Оборванные исчерпанием: третий исход, недействительный прогон, ненулевой
    # код — и НИ ОДНОГО имени в строке «красное».
    want "$copy" "$tag" space-compile 3 'НЕ выполнено 1' 'отказов 0' "$NEDEYST" '!красное:'
    want "$copy" "$tag" space-link    3 'НЕ выполнено 1' 'отказов 0' "$NEDEYST" '!красное:'
    want "$copy" "$tag" space-mkdir   3 'НЕ выполнено 1' 'отказов 0' "$NEDEYST" '!красное:'
    want "$copy" "$tag" space-git     3 'НЕ выполнено 1' 'отказов 0' "$NEDEYST" '!красное:'
    want "$copy" "$tag" mem-killed    3 'НЕ выполнено 1' 'отказов 0' "$NEDEYST" '!красное:'
    want "$copy" "$tag" mem-goruntime 3 'НЕ выполнено 1' 'отказов 0' "$NEDEYST" '!красное:'
    want "$copy" "$tag" mem-node      3 'НЕ выполнено 1' 'отказов 0' "$NEDEYST" '!красное:'
    want "$copy" "$tag" mem-enomem    3 'НЕ выполнено 1' 'отказов 0' "$NEDEYST" '!красное:'
    want "$copy" "$tag" cache-gone-open 3 'НЕ выполнено 1' 'отказов 0' "$NEDEYST" '!красное:'
    want "$copy" "$tag" cache-gone-test 3 'НЕ выполнено 1' 'отказов 0' "$NEDEYST" '!красное:'
    want "$copy" "$tag" toolchain-gone  3 'НЕ выполнено 1' 'отказов 0' "$NEDEYST" '!красное:'

    # Текст оператору обязан отличаться от «почините найденное»: находок нет.
    want "$copy" "$tag" space-compile 3 'условие не создано' 'повторите прогон'

    # Законные близнецы: настоящая находка остаётся красной.
    want "$copy" "$tag" finding-lint  1 'отказов 1' 'НЕ выполнено 0' 'красное:' "!$NEDEYST"
    want "$copy" "$tag" finding-ceph  1 'отказов 1' 'НЕ выполнено 0' 'красное:' "!$NEDEYST"
    want "$copy" "$tag" finding-empty 1 'отказов 1' 'НЕ выполнено 0' 'красное:' "!$NEDEYST"
    want "$copy" "$tag" finding-prose 1 'отказов 1' 'НЕ выполнено 0' 'красное:' "!$NEDEYST"
    want "$copy" "$tag" finding-signal 1 'отказов 1' 'НЕ выполнено 0' 'красное:' "!$NEDEYST"
    want "$copy" "$tag" finding-import 1 'отказов 1' 'НЕ выполнено 0' 'красное:' "!$NEDEYST"
    want "$copy" "$tag" finding-gobuild-prose 1 'отказов 1' 'НЕ выполнено 0' 'красное:' "!$NEDEYST"

    # Обычный пропуск кода не меняет — иначе всякий прогон группы ui стал бы
    # красным на исправном дереве.
    want "$copy" "$tag" plain-skip 0 'НЕ выполнено 1' 'отказов 0' "!$NEDEYST"
    want "$copy" "$tag" all-green  0 'отказов 0' 'НЕ выполнено 0' "!$NEDEYST"

    # Смешанный: названы ОБА, вердикт красный (находка остаётся находкой), и
    # сказано, что прочие шаги вердикта не дали.
    want "$copy" "$tag" mixed 1 'отказов 1' 'НЕ выполнено 1' "$NEDEYST" 'красное:'

    printf '%s' "$FAILS" > "$out"
}

echo "── прогон против настоящего прогонщика (ждём ноль провалов)"
assert_all "$REAL" настоящий "$tmp/real.n"; real_fails="$(cat "$tmp/real.n")"
echo
echo "── прогон против дефекта «признак снят» (ждём хотя бы один провал)"
assert_all "$BROKEN_BLIND" снят "$tmp/blind.n"; blind_fails="$(cat "$tmp/blind.n")"
echo
echo "── прогон против дефекта «признак расширен до любого отказа» (ждём хотя бы один провал)"
assert_all "$BROKEN_WIDE" расширен "$tmp/wide.n"; wide_fails="$(cat "$tmp/wide.n")"

# ─────────────────────────────────────────────────────────────────────────────
# Проверка ПРЕДПОСЫЛКИ: у каждого объявленного признака есть производитель
# ─────────────────────────────────────────────────────────────────────────────
# Признак опирается на ТЕКСТ, который печатает инструмент. Текст меняется, и
# тогда запись перестаёт что-либо отбирать — послабление без предмета. Здесь оба
# направления: признак без производителя истёк сам, а сообщение, которое ни один
# признак не берёт, означает дыру в распознавании.
echo
echo "── предпосылка: у каждого признака есть производитель, у каждого сообщения — признак"
corpus="$tmp/corpus.txt"
{
    printf '%s\n' 'compile: writing output: write $WORK/b761/_pkg_.a: no space left on device'
    printf '%s\n' 'link: mapping output file failed: no space left on device'
    printf '%s\n' 'mkdir /tmp/go-build2947461823/b765/: no space left on device'
    printf '%s\n' '        /tmp/TestSieve1369487545/001/.git/objects/info: No space left on device'
    printf '%s\n' 'go: build failed: signal: killed'
    printf '%s\n' 'fatal error: out of memory'
    printf '%s\n' 'FATAL ERROR: Ineffective mark-compacts near heap limit Allocation failed - JavaScript heap out of memory'
    printf '%s\n' 'failed to load: open /tmp/x: cannot allocate memory'
    printf '%s\n' 'go: open /home/dk/.cache/go-build/3f/3fa1c0d7e1b2c3d4e5f60718293a4b5c6d7e8f90-d: no such file or directory'
    printf '%s\n' 'main.go:5:2: package fmt is not in std (/home/dk/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.13.linux-amd64/src/fmt)'
} > "$corpus"

sig_fails=0
sigs_read=0
if ! CI=1 bash "$REAL" --exhaustion-signatures > "$tmp/sigs.txt" 2>"$tmp/sigs.err"; then
    echo "  FAIL признаки не перечисляются: прогонщик не знает --exhaustion-signatures"
    sed 's/^/       | /' "$tmp/sigs.err"
    sig_fails=$((sig_fails + 1))
else
    while IFS= read -r sig; do
        [ -n "$sig" ] || continue
        sigs_read=$((sigs_read + 1))
        if grep -qE -- "$sig" "$corpus"; then
            printf '  ok   признак «%s» отбирает хотя бы одно настоящее сообщение\n' "$sig"
        else
            printf '  FAIL признак «%s» не отбирает НИ ОДНОГО сообщения — истёк сам\n' "$sig"
            sig_fails=$((sig_fails + 1))
        fi
    done < "$tmp/sigs.txt"
    while IFS= read -r msg; do
        matched=0
        while IFS= read -r sig; do
            [ -n "$sig" ] || continue
            # Сравнение БЕЗ трубы (#658): `grep -q` выходит до конца входа, писатель
            # слева получает SIGPIPE, и под `pipefail` это поднимается до статуса
            # конвейера — найденное объявляется ненайденным. Здесь это было бы особо
            # тихо: ложное «не нашлось» означало бы «у сообщения нет признака», то
            # есть выдуманную дыру в распознавании. Правая часть `=~` БЕЗ кавычек —
            # иначе выражение сравнивалось бы как литерал.
            [[ "$msg" =~ $sig ]] && { matched=1; break; }
        done < "$tmp/sigs.txt"
        [ "$matched" = 1 ] || {
            printf '  FAIL сообщение не берёт ни один признак: %s\n' "$msg"
            sig_fails=$((sig_fails + 1)); }
    done < "$corpus"
    [ "$sigs_read" -gt 0 ] || {
        echo "  FAIL признаков прочитано НОЛЬ — перепись беспредметна"; sig_fails=$((sig_fails + 1)); }

    # КОНТРОЛЬ В ДРУГУЮ СТОРОНУ у самой сверки. Обе петли выше печатают «ok», пока
    # что-то совпадает; сверка, у которой совпадает ВСЁ, не отличима от сверки,
    # которая ничего не проверяет. Строка настоящей находки обязана НЕ совпасть ни
    # с одним признаком — иначе «у каждого сообщения есть признак» ничего не значит.
    control='internal/repohygiene/foo.go:12:3: ineffectual assignment to err (ineffassign)'
    ctl_matched=0
    while IFS= read -r sig; do
        [ -n "$sig" ] || continue
        [[ "$control" =~ $sig ]] && { ctl_matched=1; break; }
    done < "$tmp/sigs.txt"
    if [ "$ctl_matched" = 0 ]; then
        echo "  ok   строка настоящей находки НЕ берётся ни одним признаком (контроль)"
    else
        echo "  FAIL признак «$sig» берёт настоящую находку — распознавание стало маской"
        sig_fails=$((sig_fails + 1))
    fi
fi

echo
# Перепись — отдельное утверждение: «ноль провалов» обязано быть отличимо от
# «ноль прогнанного». Числа СЧИТАЮТСЯ, а не выписываются: выписанное разошлось бы
# с набором молча при первом же добавленном сценарии.
printf 'ci-local-outcome-inject: утверждений на прогон %d, прогонов 3; сценариев %d\n' \
    "$ASSERTS" "$(find "$tmp" -maxdepth 1 -name 'scen-*.sh' | wc -l)"
printf '  признаков прочитано: %s, сообщений в корпусе: %s\n' \
    "$sigs_read" "$(wc -l < "$corpus")"
printf '  провалов у настоящего:            %s (норма 0)\n' "$real_fails"
printf '  провалов у дефекта «признак снят»: %s (норма ≥1 — иначе проба ничего не проверяет)\n' "$blind_fails"
printf '  провалов у дефекта «расширен»:     %s (норма ≥1 — своё свойство, свой дефект)\n' "$wide_fails"
printf '  провалов предпосылки:              %s (норма 0)\n' "$sig_fails"

rc=0
[ "$real_fails" = "0" ] || { echo "ОТКАЗ: настоящий прогонщик не проходит собственных утверждений" >&2; rc=1; }
[ "${blind_fails:-0}" -ge 1 ] || { echo "ОТКАЗ: проба ЗЕЛЁНАЯ на снятом признаке — она не проверяет свой предмет" >&2; rc=1; }
[ "${wide_fails:-0}" -ge 1 ] || { echo "ОТКАЗ: проба ЗЕЛЁНАЯ на расширенном признаке — законные близнецы ничего не держат" >&2; rc=1; }
[ "$sig_fails" = "0" ] || { echo "ОТКАЗ: объявление признаков разошлось с текстами инструментов" >&2; rc=1; }
exit "$rc"
