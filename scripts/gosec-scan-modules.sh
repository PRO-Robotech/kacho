#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# gosec-scan-modules.sh — прогон gosec по КАЖДОМУ модулю Go этого дерева.
#
# ПОЧЕМУ ЭТО ОТДЕЛЬНЫЙ СКРИПТ, А НЕ БЛОК `run:` В WORKFLOW.
#
# Шаг `run:` исполняется под `bash -e`: ненулевой код любой команды обрывает шаг
# МОЛЧА, до любой развилки ниже. Логика, читающая коды возврата как ДАННЫЕ
# (у gosec ненулевой код означает «есть находки», а не «отказ»), внутри такого
# шага не выражается. Плюс скрипт можно позвать локально и из проб — вердикт
# конвейера перестаёт быть невоспроизводимым.
#
# ЧТО ЗДЕСЬ ГЛАВНОЕ: ПЕРЕЧЕНЬ МОДУЛЕЙ ВЫВОДИТСЯ ИЗ ДЕРЕВА.
#
# Прежде скан шёл ОДНОЙ командой `gosec ./...` из корня, и это было верно ровно
# пока модуль в дереве был один. `./...` — множество пакетов ТЕКУЩЕГО модуля:
# во вложенный модуль (свой go.mod) сборщик не спускается by construction. То
# есть с появлением второго модуля половина дерева вышла из-под гейта, а гейт
# продолжал светиться зелёным — «форма без содержания»: он присутствует, зелен
# и не осматривает предмета.
#
# Поэтому список НЕ ВЫПИСЫВАЕТСЯ, а берётся из `git ls-files`: выписанный
# разошёлся бы с деревом молча — ровно тем способом, каким это уже случилось.
# Новый модуль попадает под скан в тот же коммит, которым заводится.
#
# ВЕРДИКТ ЭТОТ СКРИПТ НЕ ВЫНОСИТ. Он всегда выходит нулём: находки обязаны
# доехать до вкладки Security через выгрузку SARIF, а вердикт выносит
# `gosec-verdict.sh` — по метке отказа, переписи и содержимому отчёта. «Ноль
# находок» и «не сканировали» обязаны различаться, и различает их перепись.
#
# ВЫХОДЫ (в $GOSEC_OUT):
#   gosec.sarif          — ОДИН отчёт на всё дерево; пути в нём приведены к корню
#                          репозитория, иначе Security-tab указывал бы на файлы,
#                          которых по названному пути нет;
#   gosec-census.txt     — перепись: по строке на модуль, `<каталог> <файлов> <строк>`;
#   gosec-scan-failed    — МЕТКА отказа с причиной. Пишется, когда анализ не
#                          состоялся. Её читают РАНЬШЕ числа находок: отчёт на
#                          дереве, которое сканер не смог собрать, побайтово
#                          такой же, как на чистом;
#   gosec-<модуль>-summary.txt — сводка каждого модуля, как её напечатал сканер;
#   gosec-<модуль>-suppressions.json + gosec-<модуль>-scan.log — ВТОРОЙ прогон,
#                          с `-track-suppressions`: сканер сам называет каждую
#                          находку, которую подавил, и каждый файл, который
#                          прочитал. Это вход гейта «у подавления обязан быть
#                          предмет» (tools/gosecsubject);
#   gosec-suppressions-manifest.txt — что из этого какому модулю принадлежит.
#                          Перечень пишет ТОТ, КТО СКАНИРОВАЛ, а гейт сверяет его
#                          с индексом git заново: два независимых чтения одного
#                          предмета, иначе модуль, вылетевший из скана, вылетел бы
#                          и из вердикта.
#
# ПОЧЕМУ ВТОРОЙ ПРОГОН, А НЕ ФЛАГ К ПЕРВОМУ. Замерено, а не предположено: с
# `-track-suppressions` подавленные находки попадают в SARIF наравне с обычными и
# несут `level=error`. Добавь этот флаг основному прогону — и `gosec-verdict.sh`
# покраснел бы на ЧИСТОМ дереве по всем полутора сотням подавлений разом. Поэтому
# перепись подавлений собирается отдельно и в отдельный файл; блокирующий отчёт
# не меняется ни одним байтом.
#
# РЕЖИМ `--list-modules` печатает только перечень каталогов модулей и выходит.
# Он существует для гейта: тот сверяет вывод скрипта с деревом и падает, если
# перечень перестал выводиться. Без такого режима «список выводится из дерева»
# осталось бы обещанием, а не проверяемым свойством.

set -uo pipefail

ROOT=$(git rev-parse --show-toplevel 2>/dev/null) || {
    echo "gosec-scan-modules: вне git-дерева — перечень модулей выводить не из чего" >&2
    exit 2
}

# Перечень модулей — из ИНДЕКСА git, а не с диска: тогда объявление, .gitignore и
# поведение не могут разъехаться молча, а порождённые в рабочем каталоге чужие
# go.mod (кеши, распакованные зависимости) в скан не попадают.
list_modules() {
    git -C "$ROOT" ls-files '*go.mod' | while read -r f; do
        d=$(dirname "$f")
        printf '%s\n' "$d"
    done | sort -u
}

if [ "${1:-}" = "--list-modules" ]; then
    list_modules
    exit 0
fi

GOSEC_BIN=${GOSEC_BIN:-"$(go env GOPATH)/bin/gosec"}
GOSEC_OUT=${GOSEC_OUT:-"$PWD"}
mkdir -p "$GOSEC_OUT"
GOSEC_OUT=$(cd "$GOSEC_OUT" && pwd)

CENSUS="$GOSEC_OUT/gosec-census.txt"
MERGED="$GOSEC_OUT/gosec.sarif"
FAILED="$GOSEC_OUT/gosec-scan-failed"
SUPPMANIFEST="$GOSEC_OUT/gosec-suppressions-manifest.txt"

rm -f "$FAILED" "$CENSUS" "$MERGED" "$SUPPMANIFEST"
: > "$CENSUS"
: > "$SUPPMANIFEST"

fail_scan() {
    printf '%s\n' "$1" >> "$FAILED"
    echo "::warning::gosec: $1"
}

if [ ! -x "$GOSEC_BIN" ]; then
    fail_scan "исполняемого gosec нет по пути $GOSEC_BIN — анализ не запускался"
    # Пустой схема-валидный документ нужен ВЫГРУЗКЕ (upload-sarif давится на
    # отсутствующем файле), а не вердикту: вердикт прочитает метку выше раньше,
    # чем число находок в этом документе.
    printf '%s' '{"version":"2.1.0","$schema":"https://json.schemastore.org/sarif-2.1.0.json","runs":[{"tool":{"driver":{"name":"gosec","rules":[]}},"results":[]}]}' > "$MERGED"
    exit 0
fi

mapfile -t MODULES < <(list_modules)
if [ "${#MODULES[@]}" -eq 0 ]; then
    fail_scan "в дереве не нашлось ни одного go.mod — обход пуст, судить не о чем"
    printf '%s' '{"version":"2.1.0","$schema":"https://json.schemastore.org/sarif-2.1.0.json","runs":[{"tool":{"driver":{"name":"gosec","rules":[]}},"results":[]}]}' > "$MERGED"
    exit 0
fi

echo "модулей Go в дереве: ${#MODULES[@]} (${MODULES[*]})"

parts=()
for mod in "${MODULES[@]}"; do
    slug=$(printf '%s' "$mod" | tr -c 'A-Za-z0-9' '-')
    summary="$GOSEC_OUT/gosec-$slug-summary.txt"
    sarif="$GOSEC_OUT/gosec-$slug.sarif"
    rm -f "$sarif"

    echo "── gosec: модуль $mod"
    # -exclude-dir=pkg/api — сгенерённые protobuf-стабы: G101 ложно срабатывает
    # на константах FullMethodName с подстрокой "Token" (RPC личности). Флаг
    # даётся КАЖДОМУ модулю, а не только тому, у кого такой каталог есть: он
    # называет СВОЙСТВО каталога, а не адрес, и модуль без него флагом не задет.
    #
    # Флага `-tests` здесь НЕТ — и это решение, а не недосмотр; разбор в
    # `gosec-verdict.sh`, чтобы обоснование стояло рядом с порогом, который оно
    # объясняет.
    #
    # `-stdout -verbose text` не косметика: SARIF НЕ НЕСЁТ ПЕРЕПИСИ осмотренного,
    # а без неё «ноль находок» неотличимо от «ноль прочитанного». Текстовая
    # сводка отвечает на оба вопроса, на которые SARIF молчит: `Golang errors in
    # file:` (дерево не собралось) и `Files : N` (сколько прочитано).
    rc=0
    ( cd "$ROOT/$mod" && "$GOSEC_BIN" -exclude-dir=pkg/api -stdout -verbose text \
        -fmt sarif -out "$sarif" ./... ) > "$summary" 2>&1 || rc=$?
    # Код возврата gosec — ДАННЫЕ, а не вердикт: он ненулевой и при находках
    # ниже порога. Судит содержимое отчёта, а не этот код.
    echo "   gosec завершился с кодом $rc (находки роняют не этот шаг, а вердикт)"

    if [ ! -s "$summary" ]; then
        fail_scan "модуль $mod: сводки нет — перепись осмотренного не снята"
        continue
    fi
    if grep -q 'Golang errors in file' "$summary"; then
        fail_scan "модуль $mod: сканер не смог загрузить часть дерева — это отказ анализа, а не чистота"
        awk '/Golang errors in file/{c=3} c-->0 && n++<20' "$summary"
        continue
    fi

    files=$(sed -nE 's/^[[:space:]]*Files[[:space:]]*:[[:space:]]*([0-9]+).*/\1/p' "$summary" | tail -1)
    lines=$(sed -nE 's/^[[:space:]]*Lines[[:space:]]*:[[:space:]]*([0-9]+).*/\1/p' "$summary" | tail -1)
    if [ -z "$files" ]; then
        fail_scan "модуль $mod: в сводке не разобрана перепись файлов — формат сводки изменился, и предпосылка проверки больше не выполняется"
        continue
    fi
    if [ ! -s "$sarif" ] || ! jq -e . "$sarif" > /dev/null 2>&1; then
        fail_scan "модуль $mod: отчёт SARIF не написан или не разбирается — сканирование не состоялось"
        continue
    fi

    printf '%s %s %s\n' "$mod" "$files" "${lines:-0}" >> "$CENSUS"
    echo "   осмотрено: файлов=$files строк=${lines:-нет}"

    # ── ВТОРОЙ ПРОГОН: перепись подавлений ──────────────────────────────────
    # Он отвечает на вопрос, на который блокирующий отчёт молчит by construction:
    # директива, под которой находки БОЛЬШЕ НЕТ, не оставляет в нём никакого
    # следа — ни находки, ни подавления, — и потому неотличима от директивы,
    # которая честно гасит ложное срабатывание.
    #
    # Флаги первого прогона повторяются ДОСЛОВНО (`-exclude-dir=pkg/api`, тот же
    # `./...` из того же каталога): множество осмотренного у двух прогонов обязано
    # совпадать, иначе «файл вне осмотра» у гейта означало бы «файл чист» у скана,
    # и оба были бы уверены в своей правоте.
    #
    # `-quiet` здесь НЕТ намеренно: перечень прочитанных файлов сканер печатает
    # строками «Checking file:», и это его собственное свидетельство об объёме
    # осмотра. Выводить тот же перечень заново из дерева нельзя — свои исключения,
    # теги сборки и состав пакетов знает только он.
    supp="$GOSEC_OUT/gosec-$slug-suppressions.json"
    scanlog="$GOSEC_OUT/gosec-$slug-scan.log"
    rm -f "$supp" "$scanlog"
    srr=0
    ( cd "$ROOT/$mod" && "$GOSEC_BIN" -exclude-dir=pkg/api -track-suppressions \
        -fmt json -out "$supp" ./... ) > "$scanlog" 2>&1 || srr=$?
    #
    # Отказ этого прогона НЕ обрывает обход модуля и НЕ идёт в `fail_scan`:
    # блокирующий вердикт по находкам от переписи подавлений не зависит и обязан
    # быть вынесен. Молчаливым отказ при этом не остаётся — строки в перечне не
    # появится, и гейт предмета честно ответит «не выполнилось» (код 2), назвав
    # модуль поимённо.
    if [ -s "$supp" ] && jq -e . "$supp" > /dev/null 2>&1; then
        printf '%s\t%s\t%s\n' "$mod" "$supp" "$scanlog" >> "$SUPPMANIFEST"
        supp_hits=$(jq '[.Issues[]|select(.suppressions!=null)]|length' "$supp")
        supp_dirs=$(jq '.Stats.nosec' "$supp")
        echo "   подавлений в переписи: $supp_hits (директив по счёту сканера: $supp_dirs)"
    else
        echo "::warning::gosec: модуль $mod: перепись подавлений не снята (код $srr) — гейт предмета по этому модулю вердикта не вынесет"
    fi

    # Пути отчёта приведены к корню репозитория. Сканер печатает их
    # относительно СВОЕГО модуля, поэтому без этой правки Security-tab указывал
    # бы на файл по адресу, которого в корне нет, — а выглядело бы это как
    # обычная находка.
    if [ "$mod" = "." ]; then
        parts+=("$sarif")
    else
        jq --arg p "$mod/" '
            (.runs[]?.results[]?.locations[]?.physicalLocation.artifactLocation.uri) |= ($p + .)
            | (.runs[]?.results[]?.relatedLocations[]?.physicalLocation.artifactLocation.uri) |= ($p + .)
        ' "$sarif" > "$sarif.rooted" && mv "$sarif.rooted" "$sarif"
        parts+=("$sarif")
    fi
done

if [ "${#parts[@]}" -eq 0 ]; then
    fail_scan "ни один модуль не дал разбираемого отчёта"
    printf '%s' '{"version":"2.1.0","$schema":"https://json.schemastore.org/sarif-2.1.0.json","runs":[{"tool":{"driver":{"name":"gosec","rules":[]}},"results":[]}]}' > "$MERGED"
    exit 0
fi

# Слияние — в ОДИН run, а не N штук рядом: у выгрузки есть предел на число
# run'ов в файле, и с ростом числа модулей отчёт молча перестал бы приниматься.
# Результаты gosec ссылаются на правило по `ruleId` и не несут `ruleIndex`,
# поэтому объединение правил по идентификатору ничего не разъезжает.
#
# Дедуп тегов: gosec пишет ДУБЛИ в properties.tags, а валидатор SARIF отвергает
# такой документ («contains duplicate item») → выгрузка падает.
if ! jq -s '
    {
      "version": "2.1.0",
      "$schema": "https://json.schemastore.org/sarif-2.1.0.json",
      "runs": [{
        "tool": {"driver": (
            (.[0].runs[0].tool.driver // {"name": "gosec"})
            + {"rules": ([.[] | .runs[]?.tool.driver.rules[]?] | unique_by(.id))}
        )},
        "results": [.[] | .runs[]?.results[]?]
      }]
    }
    | (.runs[0].tool.driver.rules[]?.properties.tags) |= (if . then unique else . end)
    ' "${parts[@]}" > "$MERGED.tmp" 2> "$GOSEC_OUT/gosec-merge-err.txt"; then
    fail_scan "отчёты модулей не свелись в один документ — сведение не состоялось"
    sed 's/^/   | /' "$GOSEC_OUT/gosec-merge-err.txt"
    printf '%s' '{"version":"2.1.0","$schema":"https://json.schemastore.org/sarif-2.1.0.json","runs":[{"tool":{"driver":{"name":"gosec","rules":[]}},"results":[]}]}' > "$MERGED"
    exit 0
fi
mv "$MERGED.tmp" "$MERGED"

echo "сведено: модулей $(wc -l < "$CENSUS"), результатов $(jq '[.runs[].results[]]|length' "$MERGED")"
exit 0
