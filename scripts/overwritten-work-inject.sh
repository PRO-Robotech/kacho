#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# overwritten-work-inject.sh — ДОКАЗАТЕЛЬСТВО ПАДУЧЕСТИ предиката полноты.
#
# ЗАЧЕМ. `overwritten-work.py` отвечает на вопрос «какую посаженную работу сняло
# массовое изменение». Проверка, не умеющая упасть, отвечает на него «ничего» при
# любом дереве — и выглядит при этом ровно как исправная. Здесь предикату
# подаётся ВХОД, на котором ответ известен, и проверяется, что он даёт именно его.
#
# ИНЪЕКЦИЯ ДВУСТОРОННЯЯ И ОДНОФАКТНАЯ. Два синтетических мира отличаются РОВНО
# ОДНИМ фактом — пережила ли работа предшественника массовое изменение:
#   * ПЕРЕЗАПИСЬ  — изменение собрано на отставшей базе, работа снята  → НАХОДКА,
#                   и находка обязана НАЗВАТЬ ИМЯ снятого, а не только число;
#   * ЗАКОННЫЙ БЛИЗНЕЦ — то же переименование, тот же переезд координат, работа
#                   предшественника НА МЕСТЕ                          → МОЛЧАНИЕ.
# Односторонняя инъекция здесь бесполезна: предикат, краснеющий на всяком
# массовом переименовании, неотличим от предиката, краснеющего всегда.
#
# ТРЕТЬЕ И ЧЕТВЁРТОЕ УТВЕРЖДЕНИЯ — про исходы, которые легко потерять:
#   * пустой обход обязан быть НАХОДКОЙ, а не нулём находок;
#   * неразрешимая ревизия обязана быть НЕ ВЫПОЛНИЛОСЬ (3), а не находкой (1):
#     «спросить не удалось» за «нет» не выдаётся.
#
# ИЗОЛЯЦИЯ. Синтетическое дерево заводится в СВОЁМ каталоге вне всякого
# репозитория, с явно снятым окружением git. Проба, пишущая в индекс дерева,
# из которого запущена, портит чужую работу молча и делает лживыми проверки,
# читающие состояние дерева.
set -uo pipefail

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX

SUT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/overwritten-work.py"
[ -x "$SUT" ] || { echo "НЕ ВЫПОЛНИЛОСЬ: не найден испытуемый $SUT" >&2; exit 3; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

pass=0; fail=0
assert() { # <заголовок> <ожидание> <факт>
    if [ "$2" = "$3" ]; then
        pass=$((pass+1)); echo "  ✓ $1"
    else
        fail=$((fail+1)); echo "  ✗ $1: ожидалось [$2], получено [$3]"
    fi
}

# ── Постройка синтетического мира ───────────────────────────────────────────
# base → предшественник P (заводит ИМЕНОВАННУЮ работу) → массовое изменение.
build() { # <каталог> <режим: overwrite|lawful>
    local d="$tmp/$1" mode="$2"
    mkdir -p "$d"
    git -C "$d" init -q
    git -C "$d" config user.email probe@example.invalid
    git -C "$d" config user.name probe

    mkdir -p "$d/pkg" "$d/scripts"
    cat > "$d/pkg/root.go" <<'GO'
package kachoroot

func Existing() string { return "kacho.cloud.iam.v1" }
GO
    cat > "$d/scripts/lib.sh" <<'SH'
#!/usr/bin/env bash
existing_helper() { echo kacho; }
SH
    git -C "$d" add -A && git -C "$d" commit -q -m base

    # ── Предшественник: сажает работу и её пробу ────────────────────────────
    cat >> "$d/pkg/root.go" <<'GO'

func PeerCheckRequired() bool { return true }
GO
    cat > "$d/pkg/root_test.go" <<'GO'
package kachoroot

import "testing"

func TestPeerCheckIsRequiredWhereTheNeighbourDecides(t *testing.T) {
	if !PeerCheckRequired() {
		t.Fatal("KACHO_PEER_MODE")
	}
}
GO
    cat >> "$d/scripts/lib.sh" <<'SH'
product_platform_prefix() { echo "${KACHO_TREE_ROOT:-.}"; }
SH
    git -C "$d" add -A && git -C "$d" commit -q -m "предшественник: работа с именами"

    # ── Массовое изменение: переименование kacho→kaname ─────────────────────
    # РАЗЛИЧИЕ МИРОВ РОВНО ЗДЕСЬ И БОЛЬШЕ НИГДЕ.
    if [ "$mode" = overwrite ]; then
        # собрано на ОТСТАВШЕЙ базе: переименование есть, работы предшественника нет
        cat > "$d/pkg/root.go" <<'GO'
package kanameroot

func Existing() string { return "kaname.cloud.iam.v1" }
GO
        rm -f "$d/pkg/root_test.go"
        cat > "$d/scripts/lib.sh" <<'SH'
#!/usr/bin/env bash
existing_helper() { echo kaname; }
SH
    else
        # законный близнец: то же переименование, работа предшественника НА МЕСТЕ
        cat > "$d/pkg/root.go" <<'GO'
package kanameroot

func Existing() string { return "kaname.cloud.iam.v1" }

func PeerCheckRequired() bool { return true }
GO
        cat > "$d/pkg/root_test.go" <<'GO'
package kanameroot

import "testing"

func TestPeerCheckIsRequiredWhereTheNeighbourDecides(t *testing.T) {
	if !PeerCheckRequired() {
		t.Fatal("KANAME_PEER_MODE")
	}
}
GO
        cat > "$d/scripts/lib.sh" <<'SH'
#!/usr/bin/env bash
existing_helper() { echo kaname; }
product_platform_prefix() { echo "${KANAME_TREE_ROOT:-.}"; }
SH
    fi
    git -C "$d" add -A && git -C "$d" commit -q -m "массовое изменение: контракт переезжает"
}

run_sut() { # <каталог> → печатает вывод, возвращает код
    (cd "$tmp/$1" && python3 "$SUT" HEAD --rename kacho=kaname --since HEAD~2) 2>&1
}

echo "── инъекция A: перезапись посаженной работы обязана быть НАХОДКОЙ ─────"
build overwrite overwrite
out_a="$(run_sut overwrite)"; rc_a=$?
assert "перезапись даёт находку (код 1)" "1" "$rc_a"
assert "находка называет снятое объявление PeerCheckRequired" "yes" \
       "$(grep -q 'PeerCheckRequired' <<<"$out_a" && echo yes || echo no)"
assert "находка называет снятую пробу TestPeerCheckIsRequired…" "yes" \
       "$(grep -q 'TestPeerCheckIsRequiredWhereTheNeighbourDecides' <<<"$out_a" && echo yes || echo no)"
assert "находка называет снятую функцию оболочки product_platform_prefix" "yes" \
       "$(grep -q 'product_platform_prefix' <<<"$out_a" && echo yes || echo no)"
assert "находка называет ПРЕДШЕСТВЕННИКА по сообщению" "yes" \
       "$(grep -q 'предшественник: работа с именами' <<<"$out_a" && echo yes || echo no)"
assert "перепись объёма напечатана" "yes" \
       "$(grep -q 'перепись: файлов у родителя' <<<"$out_a" && echo yes || echo no)"

echo "── инъекция B: законный близнец обязан МОЛЧАТЬ ───────────────────────"
build lawful lawful
out_b="$(run_sut lawful)"; rc_b=$?
assert "переезд координат при живой работе — молчание (код 0)" "0" "$rc_b"
assert "молчание не содержит слова НАХОДКА" "no" \
       "$(grep -q 'НАХОДКА' <<<"$out_b" && echo yes || echo no)"
assert "перепись объёма напечатана и на молчании" "yes" \
       "$(grep -q 'перепись: файлов у родителя' <<<"$out_b" && echo yes || echo no)"

echo "── контроль C: пустой обход — находка, а не ноль находок ─────────────"
d="$tmp/empty"; mkdir -p "$d"; git -C "$d" init -q
git -C "$d" config user.email probe@example.invalid; git -C "$d" config user.name probe
echo hi > "$d/README"; git -C "$d" add -A; git -C "$d" commit -q -m base
echo hi2 > "$d/README"; git -C "$d" add -A; git -C "$d" commit -q -m second
out_c="$( (cd "$d" && python3 "$SUT" HEAD) 2>&1 )"; rc_c=$?
assert "дерево без разбираемых имён даёт находку (код 1)" "1" "$rc_c"
assert "находка называет пустой обход, а не ноль находок" "yes" \
       "$(grep -q 'обход пуст' <<<"$out_c" && echo yes || echo no)"

echo "── контроль D: спросить не удалось — это 3, а не 1 ───────────────────"
out_d="$( (cd "$tmp/lawful" && python3 "$SUT" 0000000000000000000000000000000000000000) 2>&1 )"; rc_d=$?
assert "неразрешимая ревизия — НЕ ВЫПОЛНИЛОСЬ (код 3)" "3" "$rc_d"
out_e="$( (cd "$tmp/lawful" && python3 "$SUT" HEAD --rename ерунда) 2>&1 )"; rc_e=$?
assert "неверный аргумент — позван неверно (код 2)" "2" "$rc_e"

echo
echo "перепись доказательства: утверждений $((pass+fail)), прошло $pass, провалено $fail"
if [ "$fail" != 0 ]; then
    echo "НАХОДКА: предикат полноты не доказан — см. провалившиеся утверждения" >&2
    exit 1
fi
if [ "$pass" = 0 ]; then
    echo "НАХОДКА: не выполнено ни одного утверждения — доказательство беспредметно" >&2
    exit 1
fi
echo "✓ предикат способен упасть на перезаписи и смолчать на законной правке"
exit 0
