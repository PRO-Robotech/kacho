#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

# go-mod-tidy-check-inject.sh — доказательство, что сверка `go mod tidy` СПОСОБНА
# упасть, назвать координату и при этом смолчать на законном близнеце.
#
# ЗАЧЕМ ЭТО ОТДЕЛЬНО ОТ САМОГО ГЕЙТА. На дереве, где дрейфа нет, зелёный гейт и
# МЁРТВЫЙ гейт выглядят побайтово одинаково. Проверить это чтением нельзя: обход,
# перепись и диагностика у мёртвого целы — не срабатывает только то, ради чего он
# заведён. Поэтому способность падать доказывается ВНЕСЁННЫМ дефектом, а не
# прочтением кода.
#
# ПОЧЕМУ СИНТЕТИКА, А НЕ НАСТОЯЩЕЕ ДЕРЕВО. Внести дрейф в рабочую копию значит
# править общее состояние ради пробы. Синтетический репозиторий даёт то же
# утверждение и не трогает ничего чужого.
#
# ПОЧЕМУ ОФЛАЙН. Все зависимости синтетики — локальные, через `replace`, и прогон
# идёт с `GOPROXY=off`. Проба, которой нужна сеть, в конвейере оказалась бы
# третьей категорией и перестала бы что-либо доказывать именно тогда, когда сеть
# недоступна.
#
# ДЕЛЬТА ОДНО-ФАКТНАЯ. Каждый отрицательный случай отличается от своего
# положительного близнеца РОВНО ОДНИМ фактом — иначе покраснеть мог бы сосед, и
# доказательство превратилось бы в совпадение.

set -uo pipefail

# Путь к испытуемому переопределяем: без этого нельзя навести набор на ЗАВЕДОМО
# МЁРТВЫЙ гейт и показать, что инъекция его распознаёт. Проба собственной
# предпосылки — в конце файла.
GATE="${TIDY_GATE:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/go-mod-tidy-check.sh}"
[ -x "$GATE" ] || { echo "инъекция: гейта нет по пути $GATE — доказывать нечего"; exit 2; }

command -v go > /dev/null 2>&1 || { echo "НЕ ВЫПОЛНЕНО: go нет в PATH"; exit 2; }

# Лаборатория — ВНЕ всякого репозитория: инструмент, заводящий своё дерево, не
# должен находить чужой индекс обходом вверх.
LAB=$(mktemp -d "${TMPDIR:-/tmp}/tidy-inject-XXXXXX")
trap 'rm -rf "$LAB"' EXIT

pass=0; fail=0
ok()   { pass=$((pass+1)); printf '  ok   %s\n' "$1"; }
bad()  { fail=$((fail+1)); printf '  ОТКАЗ %s\n' "$1"; }
chk()  { # chk <имя> <ожидаемый код> <фактический код>
    if [ "$2" = "$3" ]; then ok "$1 (код $3)"; else bad "$1: ожидался код $2, получен $3"; fi
}

# ── синтетическое дерево: ДВА модуля, как в продукте ────────────────────────
# Второй модуль ВЛОЖЕН — именно эта ось однажды уже стоила слепой зоны на
# половину дерева: `./...` во вложенный модуль не спускается by construction.
build_tree() {
    local r="$1"
    rm -rf "$r"; mkdir -p "$r/dep" "$r/services/inner"
    cat > "$r/dep/go.mod" <<'EOF'
module example.com/dep

go 1.21
EOF
    printf 'package dep\n\nconst X = 1\n' > "$r/dep/dep.go"

    cat > "$r/go.mod" <<'EOF'
module example.com/synth

go 1.21

require example.com/dep v0.0.0

replace example.com/dep => ./dep
EOF
    printf 'package main\n\nimport "example.com/dep"\n\nfunc main() { _ = dep.X }\n' > "$r/main.go"

    cat > "$r/services/inner/go.mod" <<'EOF'
module example.com/inner

go 1.21

require example.com/dep v0.0.0

replace example.com/dep => ../../dep
EOF
    printf 'package inner\n\nimport "example.com/dep"\n\nconst Y = dep.X\n' > "$r/services/inner/inner.go"

    git -C "$r" init -q
    git -C "$r" add -A
    git -C "$r" -c user.email=i@n -c user.name=inject commit -qm synth
}

run_gate() { # run_gate <корень> → печатает вывод, возвращает код
    ( cd "$1" && GOFLAGS=-mod=mod GOPROXY=off "$GATE" ) 2>&1
}

echo "── A. перечень модулей выводится из дерева"
R="$LAB/a"; build_tree "$R"
out=$( cd "$R" && "$GATE" --list-modules )
# Модуля ТРИ: корень, общий `dep` и вложенный. Число намеренно не 2 — гейт не
# вправе зашивать количество модулей продукта.
[ "$out" = ".
dep
services/inner" ] && ok "перечислены все три модуля" || bad "перечень неверен: [$out]"

echo "── B. положительный контроль: дерево в порядке — гейт МОЛЧИТ"
R="$LAB/b"; build_tree "$R"
o=$(run_gate "$R"); rc=$?
chk "чистое дерево" 0 "$rc"
grep -q "сверено 3" <<<"$o" && ok "перепись назвала все три модуля" || bad "переписи на три модуля нет: $(grep ПЕРЕПИСЬ <<<"$o")"

echo "── C. дрейф в КОРНЕВОМ модуле — красное с координатой"
R="$LAB/c"; build_tree "$R"
# ОДИН факт: прямая зависимость помечена косвенной. Ровно класс #2437.
sed -i 's|require example.com/dep v0.0.0|require example.com/dep v0.0.0 // indirect|' "$R/go.mod"
before=$(md5sum < "$R/go.mod")
o=$(run_gate "$R"); rc=$?
chk "дрейф в корне" 1 "$rc"
grep -qE 'ДРЕЙФ.*(^|[^a-z])\.' <<<"$o" && ok "назван корневой модуль" || bad "координата не названа: $(grep ДРЕЙФ <<<"$o")"
[ "$(md5sum < "$R/go.mod")" = "$before" ] && ok "дерево возвращено как было" || bad "гейт оставил за собой правку"

echo "── D. дрейф во ВЛОЖЕННОМ модуле — красное с его именем"
R="$LAB/d"; build_tree "$R"
sed -i 's|require example.com/dep v0.0.0|require example.com/dep v0.0.0 // indirect|' "$R/services/inner/go.mod"
o=$(run_gate "$R"); rc=$?
chk "дрейф во вложенном" 1 "$rc"
grep -q 'services/inner' <<<"$o" && ok "назван вложенный модуль" || bad "вложенный не назван — гейт остановился на корне"

echo "── E. законный близнец: чистый сосед НЕ назван"
# Тот же прогон D: корень чист. Если гейт валит всё подряд, он назовёт и его.
n=$(grep -c '   ДРЕЙФ: ' <<<"$o")
[ "$n" = 1 ] && ok "назван ровно один модуль, чистые не обвинены" || bad "дрейфующими названо модулей: $n"

echo "── F. пустой обход — НЕ ВЫПОЛНЕНО, а не зелёное"
R="$LAB/f"; rm -rf "$R"; mkdir -p "$R"; git -C "$R" init -q
printf 'нет модулей\n' > "$R/README"; git -C "$R" add -A
git -C "$R" -c user.email=i@n -c user.name=inject commit -qm empty
o=$(run_gate "$R"); rc=$?
chk "обход пуст" 2 "$rc"
grep -q 'НЕ ВЫПОЛНЕНО' <<<"$o" && ok "пустой обход назван третьей категорией" || bad "пустой обход не назван"

echo "── G. недостижимый источник модулей — третья категория, не находка"
R="$LAB/g"; build_tree "$R"
# ОДИН факт: требуется модуль, которого нет ни локально, ни в кэше.
printf 'package main\n\nimport _ "example.com/absent"\n' > "$R/absent.go"
o=$(run_gate "$R"); rc=$?
chk "источник недоступен" 2 "$rc"
grep -q 'НЕ ВЫПОЛНЕНО' <<<"$o" && ok "названо не выполненным" || bad "недостижимость подана иначе"

echo "── H. АНТИМАСКА: дрейф РЯДОМ с невыполненным даёт красное, а не 2"
R="$LAB/h"; build_tree "$R"
printf 'package main\n\nimport _ "example.com/absent"\n' > "$R/absent.go"
sed -i 's|require example.com/dep v0.0.0|require example.com/dep v0.0.0 // indirect|' "$R/services/inner/go.mod"
o=$(run_gate "$R"); rc=$?
chk "находка объявляется раньше несостоявшегося" 1 "$rc"

echo "=============================================================="
echo "ПЕРЕПИСЬ ИНЪЕКЦИИ: утверждений $((pass+fail)) | прошло $pass | отказов $fail"
[ "$fail" -eq 0 ] || exit 1
[ "$pass" -ge 12 ] || { echo "утверждений меньше ожидаемого — набор усох"; exit 2; }
echo "гейт доказан: падает по каждой оси, молчит на законном близнеце"
exit 0
