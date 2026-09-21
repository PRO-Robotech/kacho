#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ДОКАЗАТЕЛЬСТВО, ЧТО СУДЬЯ ПРЕДПИСАНИЯ СПОСОБЕН УПАСТЬ И СПОСОБЕН СМОЛЧАТЬ.
#
# На дереве без дефекта зелёный гейт и мёртвый выглядят ОДИНАКОВО. Поэтому здесь
# в хук вносится ровно тот дефект, ради которого гейт заведён, и проверяется, что
# гейт его НАЗЫВАЕТ; следом гоняется настоящий хук — и гейт обязан смолчать.
# Одного плеча не хватило бы: гейт, падающий всегда, тоже «находит дефект».
#
# ОСЕЙ ЧЕТЫРЕ, ПО ЧИСЛУ НАХОДОК, и каждая возвращается ОТДЕЛЬНО: гейт, ловящий
# заполнитель и слепой к пропавшему перечню, прошёл бы проверку одной осью.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GATE="$ROOT/scripts/hooks/prepush-console-remedy-executable.sh"
HOOK="$ROOT/scripts/hooks/pre-push"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
rc_total=0

# expect <ожидаемый код> <ожидаемая находка|-> <имя оси> <путь к судимому хуку>
expect() {
    local want_rc="$1" want_finding="$2" axis="$3" hook="$4"
    local out rc=0
    out="$(KACHO_REMEDY_GATE_HOOK="$hook" bash "$GATE" 2>&1)" || rc=$?
    if [ "$rc" -ne "$want_rc" ]; then
        printf 'ОСЬ «%s»: код %s, ожидался %s\n%s\n' "$axis" "$rc" "$want_rc" "$out" >&2
        rc_total=1
        return
    fi
    # СРАВНЕНИЕ БЕЗ ТРУБЫ (#658). Здесь стояло `printf … | grep -q "$want_finding"`.
    # `grep -q` выходит на первом совпадении, не дочитав вход; писатель слева
    # получает SIGPIPE, а `pipefail` поднимает это до статуса конвейера — то есть
    # НАЙДЕННОЕ объявляется ненайденным.
    #
    # В файле-судье это хуже, чем где-либо ещё: собственный вердикт инъекции
    # приходил бы из трубы, и «гейт не назвал находку» печаталось бы там, где он
    # её назвал. Судья, чей вердикт зависит от гонки за SIGPIPE, не судит.
    #
    # Правая часть `=~` идёт БЕЗ кавычек: ожидание — выражение (`НАХОДКА \[F1\]`),
    # а в кавычках оно стало бы литералом и ось молчала бы всегда.
    if [ "$want_finding" != "-" ] && ! [[ "$out" =~ $want_finding ]]; then
        printf 'ОСЬ «%s»: код верен, но находка «%s» НЕ НАЗВАНА\n%s\n' "$axis" "$want_finding" "$out" >&2
        rc_total=1
        return
    fi
    printf 'ОСЬ «%s»: ok (код %s)\n' "$axis" "$rc"
}

# ВАРИАНТЫ ХУКА СТРОЯТСЯ РАЗБОРОМ, А НЕ ЗАМЕНОЙ ПО ОБРАЗЦУ. Замена «почти
# подошла» на первом заходе: варианты собрались, но дефекта в них не оказалось —
# то есть инъекция объявила бы гейт способным падать, ничего ему не подложив.
# Здесь вырезается ИМЕННО вызов производителя, по его границам в тексте.
mutate() { # mutate <куда> <чем заменить вызов производителя> [--drop-list]
    python3 - "$HOOK" "$@" <<'PYEOF'
import io, sys

src, dst, replacement = sys.argv[1], sys.argv[2], sys.argv[3]
drop_list = "--drop-list" in sys.argv[4:]
s = io.open(src, encoding="utf-8").read()

marker = "                printf '%s\\n' \"$run_out\" |\n"
call_start = s.index(marker)
tail = '--work-root "$ROOT" >&2\n'
call_end = s.index(tail, call_start) + len(tail)
s = s[:call_start] + replacement + s[call_end:]

if drop_list:
    # Перечень с координатами снимается целиком — остаётся голое число.
    ls = s.index("    printf '%s\\n' \"$run_out\" |\n")
    tail_list = "--list >&2\n"
    le = s.index(tail_list, ls) + len(tail_list)
    s = s[:ls] + s[le:]

io.open(dst, "w", encoding="utf-8").write(s)
PYEOF
}

# (0) КОНТРОЛЬ: настоящий хук — гейт обязан смолчать.
expect 0 - "настоящий хук молчит" "$HOOK"

# (а) ЗАПОЛНИТЕЛЬ ВМЕСТО ЗНАЧЕНИЯ — ровно дефект, стоивший семи веток.
mutate "$work/pre-push-placeholder" \
    "                printf 'Почините условие: npm ci --prefix <пакет>\\n' >&2
"
expect 1 'НАХОДКА \[F1\]' "заполнитель в команде" "$work/pre-push-placeholder"

# (б) ПУТЬ, КОТОРОГО НЕТ: команда без скобок, но и без смысла. Заполнителя здесь
# НЕТ — иначе ось не отличалась бы от предыдущей и у F2 не было бы судьи.
mutate "$work/pre-push-absent" \
    "                printf 'Почините условие: npm ci --prefix /nonexistent/kacho/ui-future\\n' >&2
"
expect 1 'НАХОДКА \[F2\]' "путь, которого нет" "$work/pre-push-absent"

# (в) ЧИСЛО БЕЗ ПЕРЕЧНЯ: команда исполнима и путь существует, но какие проверки
# остались без вердикта — не сказано. Прежний текст был именно таким.
mutate "$work/pre-push-nameless" \
    "                printf 'Почините условие: npm ci --prefix %s/ui-future\\n' \"\$ROOT\" >&2
" --drop-list
expect 1 'НАХОДКА \[F3\]' "число без перечня" "$work/pre-push-nameless"

# (г) СЛЕПОТА К ВРЕМЕННОЙ ВЫКЛАДКЕ: хук зовёт производителя, но говорит ему, что
# рабочая копия и есть судимое дерево. Производитель тогда честно печатает
# команду прогонщика — а она ведёт в выкладку, которая снимается по выходу.
# Так дефект переезжает во вторую ветку сообщения и становится невидимым первой
# оси гейта: там-то копия равна ревизии и всё исполнимо.
python3 - "$HOOK" "$work/pre-push-blind" <<'PYEOF'
import io, sys

src, dst = sys.argv[1], sys.argv[2]
s = io.open(src, encoding="utf-8").read()
old = '--subject-root "$subject_root" --work-root "$ROOT" >&2'
new = '--subject-root "$subject_root" --work-root "$subject_root" >&2'
assert s.count(old) == 1, s.count(old)
io.open(dst, "w", encoding="utf-8").write(s.replace(old, new))
PYEOF
expect 1 'НАХОДКА \[F4\]' "слепота к временной выкладке" "$work/pre-push-blind"

if [ "$rc_total" -ne 0 ]; then
    echo "ИНЪЕКЦИЯ ПРОВАЛЕНА: судья предписания не доказал способность падать" >&2
    exit 1
fi
echo "инъекция: судья предписания падает на каждой из четырёх осей и молчит на исправном"
