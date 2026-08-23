#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Инъекция для scripts/hooks/install.sh — доказательство в ОБЕ стороны.
#
# ПРЕДМЕТ (#1051). Провязка хука отправки — единственное, что стоит между
# правкой и стволом: конвейер проверяет ветку один раз, на запросе в ствол, и
# внутри накопительной линии вердикта не будет вовсе. При этом непровязанность
# НЕНАБЛЮДАЕМА по исходу отправки — она проходит молча в обоих случаях. Найдена
# она была только потому, что кто-то специально спросил, было ли красное; а его
# не было ПОТОМУ, ЧТО проверка не исполнялась.
#
# Отсюда требование к самому механизму: он обязан уметь ДВЕ вещи, а не одну —
# заговорить на непровязанном клоне и СМОЛЧАТЬ на провязанном. Механизм, орущий
# всегда, проходит одностороннюю пробу и через неделю перестаёт читаться; тогда
# он не отличается от отсутствующего.
#
# ПОЧЕМУ ЗАКОННЫЕ БЛИЗНЕЦЫ ЗДЕСЬ НЕСУЩИЕ, А НЕ ДЛЯ ПОЛНОТЫ. `check` обязан
# отвергать непровязанный клон и ПРИНИМАТЬ провязанный; `notice` обязан говорить
# на первом и молчать на втором; `install` обязан класть переходник и НИКОГДА не
# затирать чужой файл под тем же именем. Каждое из трёх без своей второй стороны
# зеленеет на сломанном.
#
# ИЗОЛЯЦИЯ ОБЯЗАТЕЛЬНА И ОНА ЗДЕСЬ НЕ ФОРМАЛЬНОСТЬ. Проба заводит репозитории и
# зовёт `git add`; унаследованный GIT_DIR сильнее рабочего каталога, и тогда
# запись ушла бы в индекс той копии, из которой проба запущена. Индекс
# схлопывается, а падают потом ЧУЖИЕ гейты, читающие состав дерева, — виновник
# остаётся невидимым. Наблюдалось в этом репозитории четырежды за одну сессию.
set -uo pipefail

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
SUBJECT="$ROOT/scripts/hooks/install.sh"
[ -r "$SUBJECT" ] || { echo "ОТКАЗ: предмет пробы не найден: $SUBJECT" >&2; exit 2; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fails=0
asserts=0

ok()   { asserts=$((asserts+1)); printf '  ok   %s\n' "$1"; }
bad()  { asserts=$((asserts+1)); fails=$((fails+1)); printf '  FAIL %s\n' "$1"; }
check() { if [ "$1" = "$2" ]; then ok "$3"; else bad "$3 (ждали «$2», получили «$1»)"; fi; }

# mkclone — синтетический клон с ОТСЛЕЖИВАЕМЫМ хуком и разбираемым install.sh.
#
# Копируется сам предмет, а не ссылка на него: проба обязана судить тот файл,
# который лежит в дереве, и делать это, не трогая дерево.
mkclone() {
    local d="$tmp/$1"; shift
    mkdir -p "$d/scripts/hooks"
    cp "$SUBJECT" "$d/scripts/hooks/install.sh"
    for name in "$@"; do
        printf '#!/usr/bin/env bash\necho "ОТСЛЕЖИВАЕМЫЙ ХУК %s ИСПОЛНЕН"\n' "$name" \
            > "$d/scripts/hooks/$name"
        chmod +x "$d/scripts/hooks/$name"
    done
    git -C "$d" init --quiet
    git -C "$d" add -A
    printf '%s' "$d"
}

run_subject() {  # <клон> <режим>; печатает stdout+stderr, код — в $rc
    local d="$1" mode="$2"
    ( cd "$d" && bash scripts/hooks/install.sh "$mode" 2>&1 )
}
rc_of() { local d="$1" mode="$2"; ( cd "$d" && bash scripts/hooks/install.sh "$mode" >/dev/null 2>&1 ); printf '%s' "$?"; }
# ПОРЯДОК ПЕРЕНАПРАВЛЕНИЙ ЗДЕСЬ НАМЕРЕННЫЙ: `2>&1 >/dev/null` уводит stderr на
# нынешний stdout и только ПОТОМ гасит stdout — то есть захватывает СТРОГО
# stderr. Обратный порядок захватил бы оба потока, и утверждение «notice молчит»
# зеленело бы от того, что он пишет в stdout. shellcheck считает эту форму
# опечаткой, потому что ею она чаще всего и бывает; здесь она — предмет.
# shellcheck disable=SC2069
stderr_of() { local d="$1" mode="$2"; ( cd "$d" && bash scripts/hooks/install.sh "$mode" 2>&1 >/dev/null ); }

echo "── (а) ДЕФЕКТ ВОЗВРАЩЁН: клон не провязан"
c="$(mkclone unwired pre-push)"
check "$(rc_of "$c" check)" 1 "check на непровязанном — ОТКАЗ, а не тишина"
out="$(run_subject "$c" check)"
case "$out" in *"не провязаны: pre-push"*) ok "check НАЗЫВАЕТ, что именно не провязано" ;;
                *) bad "check не называет непровязанный хук: $out" ;; esac
case "$out" in *"провязано 0 из 1"*) ok "check печатает объём осмотренного (0 из 1)" ;;
                *) bad "check не печатает переписи: $out" ;; esac
err="$(stderr_of "$c" notice)"
case "$err" in *"не провязаны"*) ok "notice ЗАГОВОРИЛ на непровязанном клоне" ;;
                *) bad "notice промолчал на непровязанном клоне (stderr: «$err»)" ;; esac
check "$(rc_of "$c" notice)" 0 "notice ничего не роняет — предмет прогонщика код, а не настройка"

echo
echo "── (б) ЗАКОННЫЙ БЛИЗНЕЦ: тот же клон, но провязанный"
check "$(rc_of "$c" install)" 0 "install проходит"
check "$(rc_of "$c" check)" 0 "check на провязанном — ПРИНИМАЕТ"
err="$(stderr_of "$c" notice)"
if [ -z "$err" ]; then ok "notice МОЛЧИТ на провязанном (иначе его перестанут читать)"
else bad "notice говорит на провязанном клоне: «$err»"; fi

echo
echo "── переходник не просто лежит, а ИСПОЛНЯЕТСЯ и доходит до дерева"
if [ -x "$c/.git/hooks/pre-push" ]; then ok "переходник исполняемый"
else bad "переходник не исполняемый — git его не позовёт"; fi
got="$( cd "$c" && ./.git/hooks/pre-push 2>&1 )"
case "$got" in *"ОТСЛЕЖИВАЕМЫЙ ХУК pre-push ИСПОЛНЕН"*)
        ok "переходник дошёл до отслеживаемого скрипта рабочей копии" ;;
    *) bad "переходник до отслеживаемого скрипта НЕ дошёл: «$got»" ;; esac

echo
echo "── чужое состояние: посторонний хук НЕ затирается"
c2="$(mkclone foreign pre-push)"
mkdir -p "$c2/.git/hooks"
printf '#!/bin/sh\necho ЧУЖОЙ\n' > "$c2/.git/hooks/pre-push"; chmod +x "$c2/.git/hooks/pre-push"
printf '#!/bin/sh\necho ПОСТОРОННИЙ\n' > "$c2/.git/hooks/post-commit"; chmod +x "$c2/.git/hooks/post-commit"
check "$(rc_of "$c2" install)" 1 "install ОТКАЗЫВАЕТ, встретив чужой файл под именем хука"
if grep -q ЧУЖОЙ "$c2/.git/hooks/pre-push"; then ok "чужой файл остался нетронутым"
else bad "чужой файл ЗАТЁРТ — это уничтожение чужого состояния"; fi
if grep -q ПОСТОРОННИЙ "$c2/.git/hooks/post-commit"; then ok "посторонний хук под другим именем не тронут"
else bad "посторонний хук пропал"; fi

echo
echo "── пустой обход — ОТКАЗ, а не «нечего делать»"
c3="$(mkclone nohooks)"   # ни одного отслеживаемого хука
check "$(rc_of "$c3" install)" 1 "пустой набор хуков роняет install"
case "$(run_subject "$c3" install)" in *"НИ ОДНОГО отслеживаемого хука"*)
        ok "отказ называет причину (пустой обход, а не ноль находок)" ;;
    *) bad "отказ не называет пустой обход" ;; esac

echo
echo "── настройка, указывающая в никуда, — ОТКАЗ (она выглядит настроенной)"
c4="$(mkclone deadpath pre-push)"
git -C "$c4" config core.hooksPath /нет/такого/каталога
check "$(rc_of "$c4" install)" 1 "core.hooksPath в никуда роняет install"
case "$(run_subject "$c4" install)" in *"каталога по этому пути НЕТ"*)
        ok "отказ называет саму настройку" ;;
    *) bad "отказ не называет core.hooksPath" ;; esac

echo
echo "── неизвестный режим — явный отказ, а не молчаливый успех"
check "$(rc_of "$c" ерунда)" 1 "неизвестный режим отвергнут"

echo
printf 'install-inject: утверждений %s, провалов %s\n' "$asserts" "$fails"
if [ "$asserts" -lt 15 ]; then
    echo "ОТКАЗ: утверждений меньше, чем проба объявляет, — она сама не исполнилась целиком." >&2
    exit 1
fi
if [ "$fails" -ne 0 ]; then
    echo "ОТКАЗ: провязка хуков не держит собственных утверждений." >&2
    exit 1
fi
exit 0
