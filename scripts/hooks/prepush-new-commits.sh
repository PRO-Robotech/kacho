#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# prepush-new-commits.sh — уезжает ли в отправке вершина, которую хук этого клона
# ещё не судил зелёным (kacho-workspace#1288, kacho#3336).
# Пара: воркспейс и PRO-Robotech/kacho, путь scripts/hooks/prepush-new-commits.sh в обоих —
# побайтово; сверяет scripts/tooling-gate/check-28 воркспейса.
#
# Класс: ветка заводится на sha, которую хук уже проверил (сборка волны на голове
# полосы), и наборы второй раз судят то же дерево. Нет непроверенных вершин —
# наборы ничего не покупают; есть хоть одна — идут как прежде.
#
# ПРОВЕРЕННОЕ — ЗАПИСЬ ХУКА, А НЕ ОПУБЛИКОВАННОЕ. Прежняя редакция вычитала ветки
# удалённого, и посылка «опубликовано — значит, проверено» оказалась ложной
# (опыт check-verifier, ws#1288): черновик `wip/`, `tmp/` уходит без наборов, обход
# `KACHO_SKIP_PREPUSH` — тоже, коммит форка судился не нами, ссылка, снятая на
# сервере, живёт в `refs/remotes` до `fetch --prune`, тег и перевод ствола несут
# любую sha. При отправке адресом git передаёт адрес вместо имени, и прежний откат
# на «все удалённые» вычитал ещё и форк — слабее, а не строже. Поэтому вычитается
# лишь то, что записал сам хук: после зелёного прогона он зовёт `--record <sha>…`.
# Имя удалённого и адрес здесь не читаются вовсе: проверенность — свойство
# вершины, а не того, куда она уезжает.
#
# Запись — `<общий каталог git>/kacho-prepush-verified`, строка «<sha> <условие>»;
# её видят все связанные копии клона. Условие — то, ЧЕМ ЕЩЁ, кроме дерева, решался
# вердикт (`KACHO_PREPUSH_CONTEXT`: хук воркспейса кладёт туда ревизии стволов
# продуктов, которые сверяют его наборы); в записи — его git-хеш, пустое — «-».
# Вершина с тем же sha при другом условии — непроверенная: ствол продукта ушёл
# вперёд, и вчерашнее зелёное о сегодняшнем не говорит. Переросла 4000 строк — усекается до последних 2000: потеря
# записи гонит наборы, а не пропускает их. Писать в неё может любой с правом на клон — тот же, кто вправе
# отправить с `KACHO_SKIP_PREPUSH`; защитой от умысла запись не служит.
#
# Вершина проверена, если её sha с текущим условием есть в записи. Слияние — ещё и когда его дерево
# равно дереву родителя, sha которого в записи: судилось то же дерево. НЕ
# проверены: предок записанной вершины (судилось дерево вершины, а не
# промежуточных коммитов), пустой коммит поверх записанной (другая sha), sha,
# которой нет среди ревизий.
#
# Удаление ссылки (нулевой `<local_sha>`) вершины не несёт и пропускается; отправка
# одних удалений — код 2.
#
# Вход — строки git для хука отправки на стандартном вводе (`<local_ref>
# <local_sha> <remote_ref> <remote_sha>`). Коды: 0 — ссылки судились, непроверенных
# вершин 0; 1 — непроверенная есть; 2 — судить нечего. `--record <sha>…` — 0
# записано, 1 — не коммит либо запись не удалась; `--recorded <sha>` — 0 есть в
# записи, 1 — нет. Печать — строка на ссылку и итог.
set -uo pipefail

zero_sha="0000000000000000000000000000000000000000"
keep=2000
common="$(git rev-parse --git-common-dir 2> /dev/null)" || {
    echo "prepush-new-commits: общий каталог git не найден — судить нечем" >&2
    exit 1
}
case "$common" in /*) ;; *) common="$PWD/$common" ;; esac
ledger="$common/kacho-prepush-verified"
ctxkey="-"
[ -z "${KACHO_PREPUSH_CONTEXT:-}" ] || ctxkey="$(printf '%s' "$KACHO_PREPUSH_CONTEXT" | git hash-object --stdin)"

verified() { [ -f "$ledger" ] && grep -qxF -- "$1 $ctxkey" "$ledger"; }

if [ "${1:-}" = "--record" ]; then
    shift
    [ "$#" -gt 0 ] || { echo "prepush-new-commits: --record без вершин" >&2; exit 1; }
    for s in "$@"; do
        c="$(git rev-parse --verify -q "$s^{commit}")" || {
            echo "prepush-new-commits: «$s» не коммит — не записано" >&2
            exit 1
        }
        printf '%s %s\n' "$c" "$ctxkey" >> "$ledger" || exit 1
    done
    if [ "$(wc -l < "$ledger")" -gt $((keep * 2)) ]; then
        tail -n "$keep" "$ledger" > "$ledger.$$" && mv -f "$ledger.$$" "$ledger"
    fi
    echo "prepush-new-commits: записано проверенных вершин $#"
    exit 0
fi
if [ "${1:-}" = "--recorded" ]; then
    c="$(git rev-parse --verify -q "${2:-}^{commit}")" || exit 1
    verified "$c"
    exit
fi

judged=0
total=0
while read -r _lref lsha rref _rsha; do
    [ -n "${lsha:-}" ] || continue
    [ "$lsha" != "$zero_sha" ] || continue
    judged=$((judged + 1))
    how=""
    if c="$(git rev-parse --verify -q "$lsha^{commit}")"; then
        if verified "$c"; then
            how="в записи"
        else
            read -r -a f <<< "$(git rev-list --parents -n 1 "$c")"
            if [ "${#f[@]}" -gt 2 ]; then
                t="$(git rev-parse "$c^{tree}")"
                for p in "${f[@]:1}"; do
                    if verified "$p" && [ "$(git rev-parse "$p^{tree}")" = "$t" ]; then
                        how="слияние с деревом записанного родителя ${p:0:12}"
                        break
                    fi
                done
            fi
        fi
    else
        echo "prepush-new-commits: «${rref:-?}» ($lsha) не разобрана — считается непроверенной" >&2
    fi
    if [ -n "$how" ]; then
        n=0
    else
        n=1
        how="хук её не судил"
    fi
    echo "${rref:-?} ${lsha:0:12} непроверенных вершин $n ($how)"
    total=$((total + n))
done
[ "$judged" -gt 0 ] || { echo "prepush-new-commits: судить нечего — вершин в отправке нет"; exit 2; }
echo "итого непроверенных вершин $total по ссылкам $judged"
[ "$total" -eq 0 ]
