#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# manifest-form-outcome-inject.sh — доказательство, что локальный прогонщик
# ЧИТАЕТ все четыре исхода судьи формы манифеста, а не два (задача #1851).
#
# # Что здесь охраняется
#
# У цели `module-manifest-check` исходов четыре: 0 годно · 1 находка · 2 VOID
# («проверять нечего») · 3 «не исполнялась». Схлопывание VOID в успех — главный
# способ незаметно потерять эту проверку: пустое дерево отчитывалось бы зелёным
# так же уверенно, как проверенное, и первый манифест, положенный мимо ожидаемого
# имени, остался бы невидимым навсегда. Схлопывание в красное — обратная ошибка:
# прогон, красный по построению на дереве, где чинить нечего, перестают читать.
#
# # Почему проба нужна, хотя классификатор написан верно
#
# Ровно потому, что он написан верно. Способность различать исходы на дереве
# продукта наблюдается ТОЛЬКО в одном из четырёх случаев (сегодня — VOID), а три
# остальных не встречаются никогда. «Зелёный» там неотличим от «мёртвый».
#
# # Устройство
#
# Функция берётся ИЗ ci-local.sh, а не переписывается здесь: копия разошлась бы с
# оригиналом молча, и проба доказывала бы свойство текста, которого никто не
# исполняет. Цель подменяется заглушкой `make`, возвращающей заданный код.
#
# Проба доказывает и СВОЮ способность упасть: тот же набор утверждений гоняется
# против НАМЕРЕННО сломанной копии, где VOID схлопнут в успех. Набор, зелёный на
# обеих, ничего не измеряет.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT_DIR/scripts/ci-local.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

ASSERTS=0
FAILS=0

# extract — тело функции из прогонщика, дословно.
extract() {
    awk '/^manifest_form_check\(\) \{$/,/^\}$/' "$SRC"
}

body="$(extract)"
if [ -z "$body" ]; then
    echo "ОТКАЗ: manifest_form_check не найдена в $SRC — предмет пробы исчез," >&2
    echo "       и её зелёное означало бы «нечего было читать», а не «всё верно»." >&2
    exit 2
fi
lines="$(printf '%s\n' "$body" | wc -l | tr -d ' ')"

# harness <вариант> <код заглушки> — прогон функции с подменённым `make`.
#
# Заглушка кладётся в свой каталог и выводится в PATH первой: настоящий `make`
# собрал бы Go-двоичный файл, а предмет пробы — не он, а чтение исхода.
harness() { # harness <ключ: real|broken> <код заглушки>
    local variant="$1" code="$2" work
    work="$tmp/run-$variant-$code"
    mkdir -p "$work/bin"
    cat > "$work/bin/make" <<EOF
#!/usr/bin/env bash
echo "заглушка цели: код $code"
exit $code
EOF
    chmod +x "$work/bin/make"

    {
        echo 'set -uo pipefail'
        echo "ROOT='$work'"
        echo "WORK='$work'"
        echo 'ran=0; fails=(); skips=()'
        if [ "$variant" = broken ]; then
            # ДЕФЕКТ: ветвь VOID заменяется целиком на успех — ровно то, что проба
            # обязана поймать. Мутируется ВСЯ ветвь, а не первая её строка: иначе
            # `skips+=` уцелел бы, пропуск по-прежнему считался, и «сломанная»
            # копия отличалась бы от настоящей только текстом.
            printf '%s\n' "$body" | awk '
                /^        2\)/ { print "        2)  echo \"   ok\" ;;"; inb=1; next }
                inb && /^        [0-9*]\)/ { inb=0 }
                inb { next }
                { print }'
        else
            printf '%s\n' "$body"
        fi
        echo 'manifest_form_check'
        echo 'printf "ИТОГ ran=%s fails=%s skips=%s\\n" "$ran" "${#fails[@]}" "${#skips[@]}"'
    } > "$work/probe.sh"

    PATH="$work/bin:$PATH" bash "$work/probe.sh" 2>&1
}

# want <вариант> <код> <ожидания…>  — «!строка» означает «обязана отсутствовать».
want() { # want <ключ> <ярлык> <код> <ожидания…>
    local variant="$1" label="$2" code="$3"; shift 3
    local out need bad=0 why=""
    ASSERTS=$((ASSERTS + 1))
    out="$(harness "$variant" "$code")"
    for need in "$@"; do
        if [ "${need:0:1}" = "!" ]; then
            case "$out" in *"${need:1}"*) bad=1; why="$why; нашлось запрещённое «${need:1}»";; esac
        else
            case "$out" in *"$need"*) ;; *) bad=1; why="$why; не нашлось «$need»";; esac
        fi
    done
    if [ "$bad" = 0 ]; then
        printf '  ok   [%s] код %s\n' "$label" "$code"
    else
        printf '  FAIL [%s] код %s — %s\n' "$label" "$code" "${why#; }"
        FAILS=$((FAILS + 1))
    fi
}

# assert_all — один и тот же набор против обеих копий.
assert_all() { # assert_all <ключ: real|broken> <ярлык для печати>
    local variant="$1" label="$2"
    FAILS=0; ASSERTS=0
    # 0 — годно: ни отказа, ни пропуска.
    want "$variant" "$label" 0 'ok' 'fails=0' 'skips=0'
    # 1 — находка: красное, и НЕ пропуск.
    want "$variant" "$label" 1 'ОТКАЗ' 'fails=1' 'skips=0'
    # 2 — VOID: пропуск, и НИ В КОЕМ СЛУЧАЕ не «ok» и не отказ.
    want "$variant" "$label" 2 'манифестов в дереве нет' 'fails=0' 'skips=1' '!ОТКАЗ'
    # 3 — не исполнялась: тоже пропуск, тоже не отказ и не успех.
    want "$variant" "$label" 3 'НЕ ИСПОЛНЯЛАСЬ' 'fails=0' 'skips=1' '!ОТКАЗ'
}

echo "== manifest_form_check: чтение четырёх исходов"
echo "   источник: $SRC (тело функции — строк $lines)"

echo
echo "-- настоящая копия: набор обязан пройти целиком"
assert_all real настоящий
real_fails="$FAILS"; real_asserts="$ASSERTS"

echo
echo "-- сломанная копия (VOID схлопнут в успех): набор обязан ПОКРАСНЕТЬ"
assert_all broken сломанный
broken_fails="$FAILS"

echo
echo "перепись: утверждений $real_asserts · провалов на настоящей $real_fails · на сломанной $broken_fails"

rc=0
if [ "$real_fails" -ne 0 ]; then
    echo "ОТКАЗ: настоящая копия не проходит собственный набор — классификатор читает не все исходы" >&2
    rc=1
fi
if [ "$broken_fails" -eq 0 ]; then
    # Без этой половины набор мог бы быть зелёным на чём угодно.
    echo "ОТКАЗ: сломанная копия прошла набор — проба НЕ СПОСОБНА поймать схлопывание VOID" >&2
    echo "       в успех, то есть её зелёное на настоящей копии ничего не доказывает." >&2
    rc=1
fi
if [ "$rc" = 0 ]; then
    echo "ГОДНО: все четыре исхода читаются, и проба доказала свою способность упасть"
fi
exit "$rc"
