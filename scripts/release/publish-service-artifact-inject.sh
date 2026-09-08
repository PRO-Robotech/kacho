#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# publish-service-artifact-inject.sh — доказательство падучести производителя.
#
# ЗАЧЕМ. Зелёный прогон производителя не значит ничего, пока не показано, что он
# СПОСОБЕН покраснеть, назвать координату и смолчать на законном близнеце. Гейт,
# потерявший способность падать, на исправном дереве выглядит точно так же, как
# работающий.
#
# ВХОД СИНТЕТИЧЕСКИЙ И ЖИВЁТ ВНЕ ЛЮБОГО РЕПОЗИТОРИЯ. Каждое утверждение поднимает
# своё дерево, вносит РОВНО ОДИН факт против контроля и сравнивает исход. Одно-
# фактность здесь не аккуратность: два внесённых факта сразу делают неизвестным,
# который из них дал красное.
#
# ЧТО ДОКАЗЫВАЕТСЯ ОТДЕЛЬНО ОТ КРАСНОГО:
#   · третья категория не выдаётся за находку и наоборот;
#   · находка объявляется ПЕРВОЙ — непрошенная предпосылка её не маскирует;
#   · красный прогон НИЧЕГО НЕ ОТПРАВЛЯЕТ даже с ключом --publish.
set -uo pipefail
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUT="$HERE/publish-service-artifact.sh"
[ -x "$SUT" ] || { echo "испытуемого нет: $SUT" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL %s\n     %s\n' "$1" "$2"; }

# say <утверждение> <ожидаемый код> <обязательная подстрока|-> <вывод> <код>
say() {
    local name="$1" want="$2" needle="$3" out="$4" rc="$5"
    if [ "$rc" != "$want" ]; then
        bad "$name" "код $rc, ожидался $want"; return
    fi
    # Сравнение средствами оболочки, а не `grep -q`: тот выходит до конца входа,
    # и под `pipefail` SIGPIPE писателя объявил бы найденное ненайденным (#658).
    if [ "$needle" != "-" ] && [[ "$out" != *"$needle"* ]]; then
        bad "$name" "в выводе нет '$needle'"; return
    fi
    ok "$name"
}

# ── Синтетическое дерево ────────────────────────────────────────────────────
# `holder` задаёт судьбу делегированного гейта: pass · fail · absent.
make_tree() {  # make_tree <каталог> <путь-модуля> <holder> [<лишний-файл-с-телом-ключа>]
    local d="$1" modpath="$2" holder="$3" secret="${4:-}"
    mkdir -p "$d/services/probe/internal/supplyhygiene"
    cat > "$d/services/probe/go.mod" <<EOF
module $modpath

go 1.26.0
EOF
    cat > "$d/services/probe/main.go" <<'EOF'
package main

func main() {}
EOF
    case "$holder" in
        pass) cat > "$d/services/probe/internal/supplyhygiene/h_test.go" <<'EOF'
package supplyhygiene

import "testing"

func TestServiceCarriesItsOwnSelfSufficientModule(t *testing.T) {}
EOF
              ;;
        fail) cat > "$d/services/probe/internal/supplyhygiene/h_test.go" <<'EOF'
package supplyhygiene

import "testing"

func TestServiceCarriesItsOwnSelfSufficientModule(t *testing.T) {
	t.Fatal("внесённый дефект: держатель отвергает дерево")
}
EOF
              ;;
        absent) rm -rf "$d/services/probe/internal/supplyhygiene" ;;
    esac
    if [ -n "$secret" ]; then
        # Литеральный материал: заголовок И длинное тело base64 — ровно то, что
        # отличает выложенный ключ от вызова, который ключ порождает.
        { echo "-----BEGIN RSA PRIVATE KEY-----"
          for _ in 1 2 3 4; do
              echo "MIIEowIBAAKCAQEAwJ8n2vQKm3xTzq7bYd1FhLpRs0GvNcXeUiAoBtWkZlMdSfHjPr"
          done
          echo "-----END RSA PRIVATE KEY-----"
        } > "$d/services/probe/$secret"
    fi
    # Личность задаётся В КОНФИГУРАЦИИ, а не ключами вызова: настоящий источник
    # её несёт, и производитель переносит её в клон артефакта. Фикстура без
    # личности изображала бы дерево, которого не бывает.
    ( cd "$d" && git init --quiet -b probe \
      && git config user.name probe && git config user.email probe@invalid \
      && git add -A && git commit --quiet -m t ) >/dev/null 2>&1
}

run_sut() {  # run_sut <каталог> [доп. ключи...]
    local d="$1"; shift
    ( cd "$d" && KACHO_ARTIFACT_HOST=example.test \
        "$SUT" probe owner/probe --confirm owner/probe "$@" 2>&1 )
}

SCRATCH="$(mktemp -d)" || exit 2
trap 'rm -rf "$SCRATCH"' EXIT
command -v go >/dev/null 2>&1 || { echo "нет go — доказать нечем" >&2; exit 3; }

echo "── A. контроль: дерево цело"
make_tree "$SCRATCH/a" "example.test/owner/probe" pass
OUT="$(run_sut "$SCRATCH/a")"; RC=$?
say "A1 целое дерево проходит" 0 "находок 0" "$OUT" "$RC"
say "A2 перепись печатается" 0 "перепись: гейтов 4" "$OUT" "$RC"
say "A3 холостой прогон говорит, что не отправлял" 0 "ничего не отправлено" "$OUT" "$RC"

echo "── B. гейт 1: путь модуля"
make_tree "$SCRATCH/b" "example.test/owner/probe-old" pass
OUT="$(run_sut "$SCRATCH/b")"; RC=$?
say "B1 расхождение пути — находка" 1 "находки: путь модуля" "$OUT" "$RC"
say "B2 находка называет обе координаты" 1 "example.test/owner/probe-old" "$OUT" "$RC"

echo "── B'. законный близнец гейта 1: чужое имя в КОММЕНТАРИИ"
make_tree "$SCRATCH/b2" "example.test/owner/probe" pass
printf '\n// прежде модуль звался example.test/owner/probe-old\n' >> "$SCRATCH/b2/services/probe/go.mod"
( cd "$SCRATCH/b2" && git -c user.name=p -c user.email=p@invalid commit --quiet -am t2 ) >/dev/null 2>&1
OUT="$(run_sut "$SCRATCH/b2")"; RC=$?
say "B3 прежнее имя в комментарии — молчание" 0 "находок 0" "$OUT" "$RC"

echo "── C. гейт 2: делегирование держателю"
make_tree "$SCRATCH/c" "example.test/owner/probe" fail
OUT="$(run_sut "$SCRATCH/c")"; RC=$?
say "C1 отказ держателя — находка" 1 "находки: самодостаточность модуля" "$OUT" "$RC"
say "C2 держателя действительно ПОЗВАЛИ" 1 "внесённый дефект" "$OUT" "$RC"

make_tree "$SCRATCH/c2" "example.test/owner/probe" absent
OUT="$(run_sut "$SCRATCH/c2")"; RC=$?
say "C3 держателя нет — ТРЕТЬЯ категория, не находка" 3 "не спрошено: самодостаточность модуля" "$OUT" "$RC"
say "C4 третья категория не выдаётся за вердикт" 3 "ВЕРДИКТА НЕТ" "$OUT" "$RC"

echo "── D. гейт 3: материал учётных данных"
make_tree "$SCRATCH/d" "example.test/owner/probe" pass "leaked.pem.txt"
OUT="$(run_sut "$SCRATCH/d")"; RC=$?
say "D1 литеральный ключ — находка" 1 "находки: учётные данные" "$OUT" "$RC"
say "D2 находка называет файл" 1 "leaked.pem.txt" "$OUT" "$RC"

echo "── D'. законный близнец: файл ПОРОЖДАЕТ ключ, а не несёт его"
make_tree "$SCRATCH/d2" "example.test/owner/probe" pass
cat > "$SCRATCH/d2/services/probe/gen.go" <<'EOF'
package main

// pem.EncodeToMemory даёт блок "-----BEGIN RSA PRIVATE KEY-----" в прогоне.
const header = "-----BEGIN RSA PRIVATE KEY-----"
EOF
( cd "$SCRATCH/d2" && git -c user.name=p -c user.email=p@invalid add -A \
  && git -c user.name=p -c user.email=p@invalid commit --quiet -m t2 ) >/dev/null 2>&1
OUT="$(run_sut "$SCRATCH/d2")"; RC=$?
say "D3 порождающий ключ файл — молчание" 0 "находок 0" "$OUT" "$RC"

echo "── E. анти-маска: находка объявляется ПЕРВОЙ"
make_tree "$SCRATCH/e" "example.test/owner/probe-old" absent
OUT="$(run_sut "$SCRATCH/e")"; RC=$?
say "E1 находка + непрошенное разом → код находки" 1 "находки: путь модуля" "$OUT" "$RC"

echo "── F. позван неверно — необратимого шага нет"
make_tree "$SCRATCH/f" "example.test/owner/probe" pass
OUT="$( cd "$SCRATCH/f" && KACHO_ARTIFACT_HOST=example.test \
        "$SUT" probe owner/probe --confirm owner/WRONG 2>&1 )"; RC=$?
say "F1 подтверждение не совпало" 2 "Ничего не отправлено" "$OUT" "$RC"
OUT="$( cd "$SCRATCH/f" && KACHO_ARTIFACT_HOST=example.test \
        "$SUT" nosuch owner/probe --confirm owner/probe 2>&1 )"; RC=$?
say "F2 службы нет в ревизии" 2 "нет каталога services/nosuch" "$OUT" "$RC"

echo "── G. КРАСНЫЙ прогон с --publish не отправляет ничего"
# Адрес указывает в несуществующее место: дойди исполнение до клона, исход был
# бы третьим (клон не удался). Код 1 доказывает, что до клона не дошло.
make_tree "$SCRATCH/g" "example.test/owner/probe-old" pass
OUT="$( cd "$SCRATCH/g" && KACHO_ARTIFACT_HOST=example.test \
        KACHO_ARTIFACT_URL="$SCRATCH/does-not-exist.git" \
        "$SUT" probe owner/probe --confirm owner/probe --publish 2>&1 )"; RC=$?
say "G1 красное с --publish → код находки, не клона" 1 "ВЫКЛАДКА НЕ ОТКРЫТА" "$OUT" "$RC"
say "G2 клона не было" 1 "-" "$(printf '%s' "$OUT" | grep -c 'клон артефакта' | sed 's/^0$/нет клона/')" "$RC"

echo "── H. полный путь выкладки на ЛОКАЛЬНОМ голом репозитории"
# Необратимый шаг доказывается на своём голом репозитории, а не рассуждением.
# Здесь же ловится класс, который иначе виден только последствием: команда
# конвейера, исполняемая не в том каталоге, правит рабочую копию вызывающего.
make_tree "$SCRATCH/h" "example.test/owner/probe" pass
BARE="$SCRATCH/bare.git"
git init --quiet --bare -b main "$BARE"
SEED="$SCRATCH/seed"; mkdir -p "$SEED"
printf 'прежнее дерево\n' > "$SEED/OLD-ROOT-FILE"
printf 'module example.test/owner/probe\n' > "$SEED/go.mod"
( cd "$SEED" && git init --quiet -b main \
  && git -c user.name=p -c user.email=p@invalid add -A \
  && git -c user.name=p -c user.email=p@invalid commit --quiet -m seed \
  && git push --quiet "$BARE" main ) >/dev/null 2>&1

# Отпечаток рабочей копии ДО вызова: имена корня артефакта нарочно совпадают с
# именами корня вызывающего, иначе класс не воспроизводится.
BEFORE="$( cd "$SCRATCH/h" && git status --porcelain | sort )"
OUT="$( cd "$SCRATCH/h" && KACHO_ARTIFACT_HOST=example.test \
        KACHO_ARTIFACT_URL="$BARE" \
        "$SUT" probe owner/probe --confirm owner/probe --publish 2>&1 )"; RC=$?
AFTER="$( cd "$SCRATCH/h" && git status --porcelain | sort )"
say "H1 выкладка проходит" 0 "Выложено" "$OUT" "$RC"
say "H2 набор равен дереву службы" 0 "набор равен дереву службы" "$OUT" "$RC"

PASS_TOTAL_BEFORE=$PASS
if [ "$BEFORE" = "$AFTER" ]; then
    ok "H3 рабочая копия вызывающего НЕ тронута"
else
    bad "H3 рабочая копия вызывающего НЕ тронута" "появилось: $(printf '%s' "$AFTER" | tr '\n' ' ')"
fi

# Выложенное дерево обязано совпасть с деревом службы ПОБАЙТОВО, а не «выглядеть
# похоже»: сверяются идентификаторы деревьев, а не перечни имён.
WANT_TREE="$( cd "$SCRATCH/h" && git rev-parse "HEAD:services/probe" )"
GOT_TREE="$( git --git-dir="$BARE" rev-parse "main^{tree}" )"
if [ "$WANT_TREE" = "$GOT_TREE" ]; then
    ok "H4 выложенное дерево побайтово равно дереву службы"
else
    bad "H4 выложенное дерево побайтово равно дереву службы" "$WANT_TREE != $GOT_TREE"
fi

# Прежний коммит остаётся предком: ствол НАРАЩИВАЕТСЯ, а не замещается, — иначе
# чужая ветка того же репозитория теряла бы общую историю.
if git --git-dir="$BARE" merge-base --is-ancestor \
     "$( cd "$SEED" && git rev-parse main )" main 2>/dev/null; then
    ok "H5 прежний ствол остался предком — работа не снята"
else
    bad "H5 прежний ствол остался предком — работа не снята" "прежний коммит не предок нового"
fi

# Второй вызов на том же дереве отправлять нечего: производитель идемпотентен.
OUT="$( cd "$SCRATCH/h" && KACHO_ARTIFACT_HOST=example.test \
        KACHO_ARTIFACT_URL="$BARE" \
        "$SUT" probe owner/probe --confirm owner/probe --publish 2>&1 )"; RC=$?
say "H6 повторная выкладка — отправлять нечего" 0 "уже совпадает с монорепо" "$OUT" "$RC"

echo "── I. личность коммитящего не установлена — отказ, а не чужая подпись"
make_tree "$SCRATCH/i" "example.test/owner/probe" pass
( cd "$SCRATCH/i" && git config --unset user.name; git config --unset user.email ) >/dev/null 2>&1
OUT="$( cd "$SCRATCH/i" && KACHO_ARTIFACT_HOST=example.test \
        KACHO_ARTIFACT_URL="$BARE" \
        "$SUT" probe owner/probe --confirm owner/probe --publish 2>&1 )"; RC=$?
say "I1 без личности — не выполнилось" 3 "личность коммитящего не установлена" "$OUT" "$RC"
say "I2 и ничего не отправлено" 3 "ничего не отправлено" "$OUT" "$RC"

echo "── J. транспорт наследуется у источника, а не назначается"
make_tree "$SCRATCH/j" "example.test/owner/probe" pass
( cd "$SCRATCH/j" && git remote add origin "git@example.test:owner/mono.git" ) >/dev/null 2>&1
OUT="$(run_sut "$SCRATCH/j")"; RC=$?
say "J1 источник по ssh → адрес по ssh" 0 "адрес отправки: git@example.test:owner/probe.git" "$OUT" "$RC"
make_tree "$SCRATCH/j2" "example.test/owner/probe" pass
( cd "$SCRATCH/j2" && git remote add origin "https://example.test/owner/mono.git" ) >/dev/null 2>&1
OUT="$(run_sut "$SCRATCH/j2")"; RC=$?
say "J2 источник по https → адрес по https" 0 "адрес отправки: https://example.test/owner/probe.git" "$OUT" "$RC"

echo "── K. отказ отправки — ТРЕТЬЯ категория, а не находка о дереве"
make_tree "$SCRATCH/k" "example.test/owner/probe" pass
OUT="$( cd "$SCRATCH/k" && KACHO_ARTIFACT_HOST=example.test \
        KACHO_ARTIFACT_URL="$SCRATCH/bare.git" \
        "$SUT" probe owner/probe --confirm owner/probe --branch nosuchbranch --publish 2>&1 )"; RC=$?
say "K1 ствола нет у артефакта — не выполнилось" 3 "клон не удался" "$OUT" "$RC"

echo
printf 'перепись доказательства: утверждений %d, прошло %d, провалено %d\n' \
    "$((PASS+FAIL))" "$PASS" "$FAIL"
[ "$((PASS+FAIL))" -gt 0 ] || { echo "утверждений ноль — доказательство беспредметно" >&2; exit 3; }
[ "$FAIL" = "0" ] || exit 1
exit 0
