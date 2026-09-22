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
#   · красный прогон НИЧЕГО НЕ ОТПРАВЛЯЕТ даже с ключом --publish;
#   · условие, которого проба не создала, — «НЕ ВЫПОЛНИЛОСЬ», а не находка о
#     производителе.
#
# Исходы — по контракту доказательств дерева (`deploy/tests/helm/README.md`
# §«Три исхода»): 0 — доказано; 1 — провалено утверждение; 2 — условие не
# создано (испытуемого нет, go недоступен, условие случая не создано,
# утверждений ноль). Коды производителя 0–3 внутри — его контракт, не этот.
set -uo pipefail
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX

# НАСТРОЙКА GIT ВЫЗЫВАЮЩЕГО В ПРОБУ НЕ ПРОНИКАЕТ (#2800). Фикстуры пробы —
# синтетические репозитории, и всё нужное каждая задаёт себе сама: `make_tree`
# пишет личность в локальную настройку, прочие коммиты несут `-c`. Настройка
# машины сверх этого — не вход, а посторонний факт. Без изоляции случай I не
# создавал своего условия «личность не установлена» там, где личность задана в
# `~/.gitconfig`: производитель законно переносил корневую личность, а проба
# объявляла его правоту дефектом («код 0, ожидался 3») — и хук отправки краснел
# на содержимом ствола у каждого, у кого корневая личность есть.
#
# Закрыты все шесть форм, которыми вызывающий передаёт настройку: корневой файл
# и файл XDG (оба снимает `GIT_CONFIG_GLOBAL`), системный файл, переменные
# `GIT_CONFIG_COUNT/KEY/VALUE` и `GIT_CONFIG_PARAMETERS` — так доезжает `git -c`,
# в том числе до хука, — и каталог шаблонов `GIT_TEMPLATE_DIR` (#2823). Шестая
# форма обходит первые пять: `git init` и `git clone` копируют из шаблона `config`
# (и хуки) в КАЖДЫЙ новый репозиторий пробы, то есть настройка вызывающего
# доезжает ЛОКАЛЬНОЙ, когда корневая и системная уже сняты. Пустое значение —
# «шаблонов нет»: git не копирует ничего. Что изоляция держит каждую форму, а
# снятая изоляция даёт «НЕ ВЫПОЛНИЛОСЬ», а не находку, доказывает
# publish-service-artifact-probe-hermetic-inject.sh. Он снимает три строки ниже,
# сличая их ДОСЛОВНО: правка их формы без правки доказательства даст там
# «дефект не внесён», а не молчаливое зелёное.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1
unset GIT_CONFIG GIT_CONFIG_PARAMETERS GIT_CONFIG_COUNT
export GIT_TEMPLATE_DIR=

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUT="$HERE/publish-service-artifact.sh"
[ -x "$SUT" ] || { echo "испытуемого нет: $SUT" >&2; exit 2; }

PASS=0; FAIL=0; NOTRUN=0
ok()   { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL %s\n     %s\n' "$1" "$2"; }
# notrun <сколько утверждений> <их имена> <почему> — третья категория: условие
# не создано, и утверждать о производителе нечего ни в одну сторону.
notrun() { NOTRUN=$((NOTRUN+$1)); printf '  НЕ ВЫПОЛНИЛОСЬ %s\n     %s\n' "$2" "$3"; }

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
        nobuild) cat > "$d/services/probe/internal/supplyhygiene/h_test.go" <<'EOF'
package supplyhygiene

import (
	"testing"

	"example.test/absent/dependency/pkg/ids"
)

func TestServiceCarriesItsOwnSelfSufficientModule(t *testing.T) { _ = ids.NewID }
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

# Номер задачи РЕПОЗИТОРИЯ АРТЕФАКТА — обязательный вход производителя (#2819):
# им названы ветка, первая строка коммита, заголовок запроса и коммит слияния.
# Номер синтетический и нарочно не похож ни на одну координату фикстуры.
ISSUE_N=42
run_sut() {  # run_sut <каталог> [доп. ключи...]
    local d="$1"; shift
    ( cd "$d" && KACHO_ARTIFACT_HOST=example.test \
        "$SUT" probe owner/probe --confirm owner/probe --issue "$ISSUE_N" "$@" 2>&1 )
}

SCRATCH="$(mktemp -d)" || exit 2
trap 'rm -rf "$SCRATCH"' EXIT
command -v go >/dev/null 2>&1 || { echo "нет go — доказать нечем" >&2; exit 2; }

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

echo "── C'. отказ ОКРУЖЕНИЯ у держателя — третья категория, а не находка"
# ОДНО-ФАКТНАЯ ДЕЛЬТА против контроля A: то же дерево, тот же держатель
# (`pass`), тот же вызов — отличие РОВНО одно, каталог кэша сборки непригоден.
# Непригодность задаётся ГЕРМЕТИЧНО, файлом в своём каталоге: путь вида
# `/gocache` зависел бы от того, под кем идёт прогон, и под привилегированным
# пользователем дельта не вносилась бы вовсе — утверждение зеленело бы, ничего
# не проверив.
#
# Держатель здесь ИСПРАВЕН и дерево ЦЕЛО: единственная беда — окружение.
# `go test` отдаёт на ней ту же единицу, что на упавшей пробе, и до задачи #2581
# производитель объявлял это НАХОДКОЙ, посылая читателя искать дефект в дереве,
# которое верно.
make_tree "$SCRATCH/c3" "example.test/owner/probe" pass
printf 'это файл, а не каталог\n' > "$SCRATCH/badcache"
OUT="$( cd "$SCRATCH/c3" && GOCACHE="$SCRATCH/badcache" KACHO_ARTIFACT_HOST=example.test \
        "$SUT" probe owner/probe --confirm owner/probe --issue "$ISSUE_N" 2>&1 )"; RC=$?
say "C5 непригодный кэш сборки — НЕ ВЫПОЛНИЛОСЬ, а не находка" 3 "НЕ ВЫПОЛНИЛОСЬ" "$OUT" "$RC"
say "C6 та же полоса названа непрошенной" 3 "не спрошено: самодостаточность модуля" "$OUT" "$RC"
say "C7 третья категория не выдаётся за вердикт" 3 "ВЕРДИКТА НЕТ" "$OUT" "$RC"
# Отрицательная половина: строки находок в выводе быть НЕ должно. Без неё
# утверждения выше истинны и на производителе, объявляющем разом обе категории.
if [[ "$OUT" == *"находки: самодостаточность модуля"* ]]; then
    bad "C8 полоса НЕ объявлена находкой" "в выводе стоит 'находки: самодостаточность модуля'"
else
    ok "C8 полоса НЕ объявлена находкой"
fi
# Диагностика — часть свойства: находка, называющая симптом вместо причины,
# посылает читателя не туда, и её снимают как непонятную.
say "C9 причина названа, а не только категория" 3 "держатель не стартовал" "$OUT" "$RC"

echo "── C''. законный близнец: ОКРУЖЕНИЕ цело, а держатель отвергает дерево"
# Третий прогон пары, и он обязателен: без него молчание СУЩЕСТВУЮЩЕЙ полосы
# находок неотличимо от молчания мёртвой. Дельта против C' ровно одна —
# кэш сборки пригоден, держатель падает.
make_tree "$SCRATCH/c4" "example.test/owner/probe" fail
OUT="$(run_sut "$SCRATCH/c4")"; RC=$?
say "C10 упавший держатель при целом окружении — по-прежнему НАХОДКА" 1 "находки: самодостаточность модуля" "$OUT" "$RC"
say "C11 и это по-прежнему вердикт о дереве" 1 "ВЫКЛАДКА НЕ ОТКРЫТА" "$OUT" "$RC"

echo "── C'''. второй законный близнец: модуль НЕ СОБИРАЕТСЯ"
# Граница нового распознавателя, которую он НЕ вправе перейти. `go` доходит до
# пакета и печатает свой вердикт `FAIL <пакет> [setup failed]` — то есть форму,
# по которой пакет считается достигнутым, — поэтому полоса остаётся НАХОДКОЙ.
#
# Это не мелочь и не край: недостающий `require` есть РОВНО тот дефект, ради
# которого гейт самодостаточности заведён. Переведи распознаватель эту форму в
# третью категорию — гейт перестал бы ловить свой предмет, оставаясь на вид
# рабочим: послабление хуже исходной беды.
#
# До задачи #2581 решение было записано КОММЕНТАРИЕМ и не держалось ничем.
# Утверждение ниже даёт ему держателя: расширение распознавателя, накрывшее эту
# форму, краснеет здесь, а не проходит молча.
make_tree "$SCRATCH/c5" "example.test/owner/probe" nobuild
OUT="$(run_sut "$SCRATCH/c5")"; RC=$?
say "C12 несобравшийся модуль — по-прежнему НАХОДКА" 1 "находки: самодостаточность модуля" "$OUT" "$RC"
say "C13 и причина названа вердиктом go о пакете" 1 "[setup failed]" "$OUT" "$RC"
# Отрицательная половина: третьей категорией эта полоса не объявляется.
if [[ "$OUT" == *"НЕ ВЫПОЛНИЛОСЬ"* ]]; then
    bad "C14 полоса НЕ объявлена третьей категорией" "в выводе стоит 'НЕ ВЫПОЛНИЛОСЬ'"
else
    ok "C14 полоса НЕ объявлена третьей категорией"
fi

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
        "$SUT" probe owner/probe --confirm owner/WRONG --issue "$ISSUE_N" 2>&1 )"; RC=$?
say "F1 подтверждение не совпало" 2 "Ничего не отправлено" "$OUT" "$RC"
OUT="$( cd "$SCRATCH/f" && KACHO_ARTIFACT_HOST=example.test \
        "$SUT" nosuch owner/probe --confirm owner/probe --issue "$ISSUE_N" 2>&1 )"; RC=$?
say "F2 службы нет в ревизии" 2 "нет каталога services/nosuch" "$OUT" "$RC"
# Номер задачи не передан — позван неверно, ДО первой отправки. Адрес указывает в
# несуществующее место: дойди исполнение до клона, исход был бы третьим (клон не
# удался). Код 2 доказывает, что до клона не дошло (#2819).
OUT="$( cd "$SCRATCH/f" && KACHO_ARTIFACT_HOST=example.test \
        KACHO_ARTIFACT_URL="$SCRATCH/does-not-exist.git" \
        "$SUT" probe owner/probe --confirm owner/probe --publish --via-pull-request 2>&1 )"; RC=$?
say "F3 номер задачи не передан — позван неверно, до клона" 2 "номер задачи репозитория артефакта не передан" "$OUT" "$RC"
OUT="$( cd "$SCRATCH/f" && KACHO_ARTIFACT_HOST=example.test \
        KACHO_ARTIFACT_URL="$SCRATCH/does-not-exist.git" \
        "$SUT" probe owner/probe --confirm owner/probe --issue "issue-$ISSUE_N" --publish 2>&1 )"; RC=$?
say "F4 номер задачи не номер — позван неверно" 2 "Ничего не отправлено" "$OUT" "$RC"

echo "── G. КРАСНЫЙ прогон с --publish не отправляет ничего"
# Адрес указывает в несуществующее место: дойди исполнение до клона, исход был
# бы третьим (клон не удался). Код 1 доказывает, что до клона не дошло.
make_tree "$SCRATCH/g" "example.test/owner/probe-old" pass
OUT="$( cd "$SCRATCH/g" && KACHO_ARTIFACT_HOST=example.test \
        KACHO_ARTIFACT_URL="$SCRATCH/does-not-exist.git" \
        "$SUT" probe owner/probe --confirm owner/probe --issue "$ISSUE_N" --publish 2>&1 )"; RC=$?
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
        "$SUT" probe owner/probe --confirm owner/probe --issue "$ISSUE_N" --publish 2>&1 )"; RC=$?
AFTER="$( cd "$SCRATCH/h" && git status --porcelain | sort )"
say "H1 выкладка проходит" 0 "Выложено" "$OUT" "$RC"
say "H2 набор равен дереву службы" 0 "набор равен дереву службы" "$OUT" "$RC"
# Первая строка выложенного коммита — `#<N> ` задачи репозитория артефакта:
# сверяется объект на удалённом, а не вывод производителя (#2819).
H_SUBJ="$(git --git-dir="$BARE" log -1 --format=%s main 2>/dev/null)"
if [[ "$H_SUBJ" == "#$ISSUE_N "* ]]; then
    ok "H2a первая строка коммита — «#$ISSUE_N …»"
else
    bad "H2a первая строка коммита — «#$ISSUE_N …»" "первая строка: '$H_SUBJ'"
fi

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
        "$SUT" probe owner/probe --confirm owner/probe --issue "$ISSUE_N" --publish 2>&1 )"; RC=$?
say "H6 повторная выкладка — отправлять нечего" 0 "уже совпадает с монорепо" "$OUT" "$RC"

echo "── I. личность коммитящего не установлена — отказ, а не чужая подпись"
make_tree "$SCRATCH/i" "example.test/owner/probe" pass
( cd "$SCRATCH/i" && git config --unset user.name; git config --unset user.email ) >/dev/null 2>&1
# ПРЕДПОСЫЛКА СЛУЧАЯ СПРАШИВАЕТСЯ У GIT, А НЕ ВЫВОДИТСЯ ИЗ ТОГО, ЧТО СНЯТО.
# Снятая локальная личность не означает, что личности нет: git собирает её со
# всех уровней настройки, и производитель читает ровно то, что соберёт git.
# Условие не создано — утверждать о производителе нечего: отказ перенести
# законно найденную личность был бы дефектом, а не исполнением контракта. Это
# «НЕ ВЫПОЛНИЛОСЬ» с названным источником, а не находка.
SEEN="$( cd "$SCRATCH/i" && { git config --show-origin --get-all user.name
                              git config --show-origin --get-all user.email; } 2>/dev/null )"
if [ -n "$SEEN" ]; then
    notrun 2 "I1, I2" "условие «личность не установлена» не создано — git видит: $(printf '%s' "$SEEN" | tr '\t\n' ' ;')"
else
    OUT="$( cd "$SCRATCH/i" && KACHO_ARTIFACT_HOST=example.test \
            KACHO_ARTIFACT_URL="$BARE" \
            "$SUT" probe owner/probe --confirm owner/probe --issue "$ISSUE_N" --publish 2>&1 )"; RC=$?
    say "I1 без личности — не выполнилось" 3 "личность коммитящего не установлена" "$OUT" "$RC"
    say "I2 и ничего не отправлено" 3 "ничего не отправлено" "$OUT" "$RC"
fi

echo "── I'. личность ТОЛЬКО в корневой настройке — ею и подписан коммит артефакта"
# Положительная половина случая I (#2822). Решение владельца 2026-09-22: подпись
# — только личностью из корневой настройки. Случай I держит отказ, когда её нет,
# но не то, ОТКУДА она взята: фикстуры несут локальную личность `probe`, и
# производитель, задающий себе личность сам (`git config --local user.*` во
# временном клоне), проходил все прочие утверждения — подделка совпадала с
# фикстурой. Здесь личность лежит только в корневой настройке ИЗОЛИРОВАННОГО дома
# пробы (свой файл, не вызывающего), локальной у источника нет, и автор и
# коммиттер коммита артефакта обязаны быть ею.
#
# Переменные личности коммита (GIT_AUTHOR_*, GIT_COMMITTER_*, EMAIL) сняты в самом
# случае: они сильнее любой настройки, и значение вызывающего в них подписало бы
# коммит мимо производителя — случай судил бы окружение, а не производителя.
RHOME="$SCRATCH/root-home"; mkdir -p "$RHOME"
printf '[user]\n\tname = root-only\n\temail = root-only@invalid\n' > "$RHOME/.gitconfig"
root_only() {  # root_only <команда...> — корневая настройка случая, без переменных личности
    ( unset GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL EMAIL
      export GIT_CONFIG_GLOBAL="$RHOME/.gitconfig"; "$@" )
}
make_tree "$SCRATCH/i3" "example.test/owner/probe" pass
( cd "$SCRATCH/i3" && git config --unset user.name; git config --unset user.email ) >/dev/null 2>&1
RBARE="$SCRATCH/bare-root.git"
git init --quiet --bare -b main "$RBARE"
RSEED="$SCRATCH/seed-root"; mkdir -p "$RSEED"
printf 'прежнее дерево\n' > "$RSEED/OLD-ROOT-FILE"
( cd "$RSEED" && git init --quiet -b main \
  && git -c user.name=p -c user.email=p@invalid add -A \
  && git -c user.name=p -c user.email=p@invalid commit --quiet -m seed \
  && git push --quiet "$RBARE" main ) >/dev/null 2>&1
# ПРЕДПОСЫЛКА СПРАШИВАЕТСЯ У GIT В ТОМ ЖЕ ОКРУЖЕНИИ, в котором позван
# производитель: личность видна ровно одна и ровно из корневого файла случая.
# Иначе (утечка настройки вызывающего, лишний уровень) условие не создано, и это
# «НЕ ВЫПОЛНИЛОСЬ» с названным источником, а не находка о производителе.
SEEN="$( cd "$SCRATCH/i3" && root_only sh -c \
    'git config --show-origin --get-all user.name; git config --show-origin --get-all user.email' 2>/dev/null )"
WANT="file:$RHOME/.gitconfig	root-only
file:$RHOME/.gitconfig	root-only@invalid"
# И какой подписью git подписал бы коммит в этом окружении: переменные личности
# сильнее настройки, и настройка без них ещё не всё окружение.
IDENT="$( cd "$SCRATCH/i3" && root_only sh -c 'git var GIT_AUTHOR_IDENT; git var GIT_COMMITTER_IDENT' 2>/dev/null \
          | sed -E 's/ [0-9]+ [+-][0-9]{4}$//' )"
WANT_IDENT="root-only <root-only@invalid>
root-only <root-only@invalid>"
if [ "$SEEN" != "$WANT" ] || [ "$IDENT" != "$WANT_IDENT" ]; then
    notrun 3 "I3, I4, I5" "условие «личность только в корневой настройке» не создано — git видит: $(printf '%s' "$SEEN" | tr '\t\n' ' ;'); подписал бы: $(printf '%s' "$IDENT" | tr '\n' ';')"
else
    OUT="$(KACHO_ARTIFACT_URL="$RBARE" root_only run_sut "$SCRATCH/i3" --publish)"; RC=$?
    say "I3 личность только корневая — выложено" 0 "Выложено" "$OUT" "$RC"
    # Сверяется КОММИТ на удалённом, а не вывод производителя: подпись — свойство
    # выложенного объекта, и пересказ её в выводе ничего о нём не утверждает.
    GOT_AUTHOR="$(git --git-dir="$RBARE" log -1 --format='%an <%ae>' main 2>/dev/null)"
    GOT_COMMITTER="$(git --git-dir="$RBARE" log -1 --format='%cn <%ce>' main 2>/dev/null)"
    if [ "$GOT_AUTHOR" = "root-only <root-only@invalid>" ]; then
        ok "I4 автор коммита артефакта — личность корневой настройки"
    else
        bad "I4 автор коммита артефакта — личность корневой настройки" "автор: '$GOT_AUTHOR'"
    fi
    if [ "$GOT_COMMITTER" = "root-only <root-only@invalid>" ]; then
        ok "I5 коммиттер коммита артефакта — личность корневой настройки"
    else
        bad "I5 коммиттер коммита артефакта — личность корневой настройки" "коммиттер: '$GOT_COMMITTER'"
    fi
fi

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
        "$SUT" probe owner/probe --confirm owner/probe --issue "$ISSUE_N" --branch nosuchbranch --publish 2>&1 )"; RC=$?
say "K1 ствола нет у артефакта — не выполнилось" 3 "клон не удался" "$OUT" "$RC"

echo "── M. условие транспорта: объявление конвейера и полномочие OAuth"
# ОДНО-ФАКТНАЯ ПАРА. Дерево одно и то же, различается ТОЛЬКО транспорт: по https
# отправка идёт от приложения OAuth и упирается в полномочие workflow, по ssh —
# от личности владельца ключа и не упирается ни во что. Утверждение отрицательное
# («по ssh молчит»), поэтому рядом стоит положительное («по https говорит»),
# иначе молчание зеленело бы на скрипте, который не умеет говорить вовсе.
make_tree "$SCRATCH/m" "example.test/owner/probe" pass
mkdir -p "$SCRATCH/m/services/probe/.github/workflows"
printf 'name: ci\non: [push]\n' > "$SCRATCH/m/services/probe/.github/workflows/ci.yml"
( cd "$SCRATCH/m" && git -c user.name=p -c user.email=p@invalid add -A \
  && git -c user.name=p -c user.email=p@invalid commit --quiet -m wf ) >/dev/null 2>&1

( cd "$SCRATCH/m" && git remote add origin "https://example.test/owner/mono.git" ) >/dev/null 2>&1
OUT="$(run_sut "$SCRATCH/m")"; RC=$?
say "M1 https + объявление конвейера — условие названо" 0 "полномочия workflow" "$OUT" "$RC"
say "M2 условие названо числом, а не намёком" 0 "объявлений конвейера 1" "$OUT" "$RC"
say "M3 и это НЕ находка: дерево верно" 0 "находок 0" "$OUT" "$RC"

# Законный близнец: то же дерево, транспорт ssh — молчание.
( cd "$SCRATCH/m" && git remote set-url origin "git@example.test:owner/mono.git" ) >/dev/null 2>&1
OUT="$(run_sut "$SCRATCH/m")"; RC=$?
if [[ "$OUT" != *"полномочия workflow"* ]]; then
    ok "M4 ssh + то же дерево — молчание"
else
    bad "M4 ssh + то же дерево — молчание" "условие названо там, где его нет"
fi

# Второй законный близнец: https, но объявлений конвейера в дереве нет.
make_tree "$SCRATCH/m2" "example.test/owner/probe" pass
( cd "$SCRATCH/m2" && git remote add origin "https://example.test/owner/mono.git" ) >/dev/null 2>&1
OUT="$(run_sut "$SCRATCH/m2")"; RC=$?
if [[ "$OUT" != *"полномочия workflow"* ]]; then
    ok "M5 https без объявлений конвейера — молчание"
else
    bad "M5 https без объявлений конвейера — молчание" "условие названо без предмета"
fi

echo "── L. полоса запроса на слияние: ствол НЕ трогается напрямую"
# ЗАЧЕМ ОТДЕЛЬНАЯ СЕКЦИЯ. Полоса заведена ради защищённого ствола, и её главное
# свойство — отрицательное: ствол артефакта не двигается, пока проверки не дали
# вердикт. Отрицание доказывается ТОЛЬКО в паре с положительным контролем,
# поэтому рядом с каждым «ствол не тронут» стоит «ветка появилась».

# Подставной форж. Он НЕ прогоняет ничего и не притворяется, что прогнал: его
# предмет — три ответа, которые производитель обязан различать. Каждый ответ
# подаётся ОТДЕЛЬНЫМ утверждением с одно-фактной дельтой, а вызовы пишутся в
# журнал — провязка доказывается тем, ЧТО ЕГО ПОЗВАЛИ, а не тем, что он ответил
# «хорошо».
STUB="$SCRATCH/forge"
cat > "$STUB" <<'FORGE'
#!/usr/bin/env bash
MODE="$(cat "$STUB_STATE/mode" 2>/dev/null || echo green)"
printf '%s\n' "$*" >> "$STUB_STATE/log"
case "$1 $2" in
  "pr list")   [ -f "$STUB_STATE/created" ] && echo 77; exit 0 ;;
  "pr create") : > "$STUB_STATE/created"; echo "https://forge.invalid/pr/77"; exit 0 ;;
  "pr merge")  echo "влито"; exit 0 ;;
esac
if [ "$1" = "api" ]; then
  case "$2" in
    *"/protection") printf 'гейт-а\nгейт-б\n'; exit 0 ;;
    *"/check-runs"*)
      case "$MODE" in
        green)   printf 'гейт-а\tcompleted\tsuccess\nгейт-б\tcompleted\tsuccess\n' ;;
        red)     printf 'гейт-а\tcompleted\tsuccess\nгейт-б\tcompleted\tfailure\n' ;;
        running) printf 'гейт-а\tcompleted\tsuccess\nгейт-б\tin_progress\t\n' ;;
        absent)  printf 'гейт-а\tcompleted\tsuccess\n' ;;
      esac
      exit 0 ;;
  esac
fi
exit 0
FORGE
chmod +x "$STUB"

stub_reset() { rm -rf "$SCRATCH/stub"; mkdir -p "$SCRATCH/stub"; printf '%s' "$1" > "$SCRATCH/stub/mode"; : > "$SCRATCH/stub/log"; }
bare_trunk() { git --git-dir="$1" rev-parse main 2>/dev/null; }

# Свой голый репозиторий: секция H оставила свой уже приведённым к дереву службы,
# и вторая выкладка туда сказала бы «отправлять нечего» ещё до полосы.
LBARE="$SCRATCH/bare-pr.git"
git init --quiet --bare -b main "$LBARE"
LSEED="$SCRATCH/seed-pr"; mkdir -p "$LSEED"
printf 'прежнее дерево\n' > "$LSEED/OLD-ROOT-FILE"
( cd "$LSEED" && git init --quiet -b main \
  && git -c user.name=p -c user.email=p@invalid add -A \
  && git -c user.name=p -c user.email=p@invalid commit --quiet -m seed \
  && git push --quiet "$LBARE" main ) >/dev/null 2>&1
TRUNK0="$(bare_trunk "$LBARE")"

make_tree "$SCRATCH/l" "example.test/owner/probe" pass
WB="$ISSUE_N"

run_pr() {  # run_pr <mode> <forge> [доп. ключи]
    local mode="$1" forge="$2"; shift 2
    stub_reset "$mode"
    ( cd "$SCRATCH/l" && KACHO_ARTIFACT_HOST=example.test \
        KACHO_ARTIFACT_URL="$LBARE" KACHO_FORGE_CMD="$forge" \
        STUB_STATE="$SCRATCH/stub" \
        "$SUT" probe owner/probe --confirm owner/probe --issue "$ISSUE_N" --publish --via-pull-request "$@" 2>&1 )
}

# L1 — холостой прогон называет полосу, которой позван.
OUT="$( cd "$SCRATCH/l" && KACHO_ARTIFACT_HOST=example.test \
        "$SUT" probe owner/probe --confirm owner/probe --issue "$ISSUE_N" --via-pull-request 2>&1 )"; RC=$?
say "L1 холостой прогон называет полосу" 0 "ЧЕРЕЗ ЗАПРОС НА СЛИЯНИЕ" "$OUT" "$RC"
say "L2 подсказка несёт тот же ключ" 0 "--publish --via-pull-request" "$OUT" "$RC"

# L3 — форжа нет: третья категория, ветка отправлена, ствол не тронут.
OUT="$(run_pr green "$SCRATCH/no-such-forge-xyz")"; RC=$?
say "L3 форжа нет — не выполнилось, а не находка" 3 "команда форжа" "$OUT" "$RC"
if [ -n "$(git --git-dir="$LBARE" rev-parse --verify --quiet "refs/heads/$WB")" ]; then
    ok "L4 ветка отправлена (положительный контроль отрицания ниже)"
else
    bad "L4 ветка отправлена (положительный контроль отрицания ниже)" "ветки $WB нет"
fi
if [ "$(bare_trunk "$LBARE")" = "$TRUNK0" ]; then
    ok "L5 ствол НЕ тронут"
else
    bad "L5 ствол НЕ тронут" "ствол сдвинулся: $TRUNK0 → $(bare_trunk "$LBARE")"
fi
# Ветка — номер задачи, и ни одной ветки иного имени производитель не заводит:
# спрашивается перечень веток удалённого, а не значение, которое проба сама
# назвала (#2819). Ствол — единственное законное имя-не-номер.
L_HEADS="$(git --git-dir="$LBARE" for-each-ref --format='%(refname:short)' refs/heads/ 2>/dev/null)"
L_ODD=""
while IFS= read -r h; do
    [ -n "$h" ] || continue
    [ "$h" = main ] && continue
    [[ "$h" =~ ^[0-9]+$ ]] || L_ODD="$L_ODD $h"
done <<< "$L_HEADS"
if [ -z "$L_ODD" ] && [[ $'\n'"$L_HEADS"$'\n' == *$'\n'"$ISSUE_N"$'\n'* ]]; then
    ok "L5a ветка — номер задачи ($ISSUE_N), веток иного имени нет"
else
    bad "L5a ветка — номер задачи ($ISSUE_N), веток иного имени нет" "ветки: $(printf '%s' "$L_HEADS" | tr '\n' ' ')"
fi
L_SUBJ="$(git --git-dir="$LBARE" log -1 --format=%s "refs/heads/$WB" 2>/dev/null)"
if [[ "$L_SUBJ" == "#$ISSUE_N "* ]]; then
    ok "L5b первая строка коммита ветки — «#$ISSUE_N …»"
else
    bad "L5b первая строка коммита ветки — «#$ISSUE_N …»" "первая строка: '$L_SUBJ'"
fi

# L6 — красная обязательная проверка: НАХОДКА о дереве, а не третья категория.
OUT="$(run_pr red "$STUB")"; RC=$?
say "L6 красная обязательная — находка" 1 "ВЛИВАНИЕ НЕ ОТКРЫТО" "$OUT" "$RC"
if grep -q '^pr create' "$SCRATCH/stub/log"; then
    ok "L6a запрос на слияние заведён"
else
    bad "L6a запрос на слияние заведён" "журнал: $(tr '\n' '|' < "$SCRATCH/stub/log")"
fi
# Заголовок запроса начинается с `#<N> ` — журнал форжа несёт доводы вызова как
# есть, и заголовок в нём стоит сразу за ключом (#2819).
if grep -q -- "^pr create .*--title #$ISSUE_N " "$SCRATCH/stub/log"; then
    ok "L6b заголовок запроса — «#$ISSUE_N …»"
else
    bad "L6b заголовок запроса — «#$ISSUE_N …»" "журнал: $(tr '\n' '|' < "$SCRATCH/stub/log")"
fi
say "L7 находка называет проверку" 1 "гейт-б" "$OUT" "$RC"
if ! grep -q '^pr merge' "$SCRATCH/stub/log"; then
    ok "L8 вливания на красном НЕ было"
else
    bad "L8 вливания на красном НЕ было" "в журнале форжа есть pr merge"
fi

# L7' — контекст, которого нет среди произведённых: НЕ красное и не зелёное.
OUT="$(run_pr absent "$STUB" --checks-budget 0)"; RC=$?
say "L9 не появившийся контекст — третья категория" 3 "не появилось ни разу" "$OUT" "$RC"
say "L10 и он назван поимённо" 3 "гейт-б" "$OUT" "$RC"

# L11 — идущая проверка: тоже не вердикт.
OUT="$(run_pr running "$STUB" --checks-budget 0)"; RC=$?
say "L11 идущая проверка — не вердикт" 3 "вердикт не вынесен" "$OUT" "$RC"

# L12 — зелено: вливание ПОЗВАНО (провязка доказывается вызовом).
OUT="$(run_pr green "$STUB")"; RC=$?
say "L12 зелено — выложено" 0 "Выложено" "$OUT" "$RC"
if grep -q '^pr merge 77' "$SCRATCH/stub/log"; then
    ok "L13 вливание позвано с номером запроса"
else
    bad "L13 вливание позвано с номером запроса" "журнал: $(tr '\n' '|' < "$SCRATCH/stub/log")"
fi
if grep -q '^pr merge 77 .*--body-file' "$SCRATCH/stub/log"; then
    ok "L13a тело коммита слияния задано ЯВНО, а не умолчанием форжа"
else
    bad "L13a тело коммита слияния задано ЯВНО, а не умолчанием форжа" "журнал: $(tr '\n' '|' < "$SCRATCH/stub/log")"
fi
# Вливание — коммитом слияния, без схлопывания и rebase, и его первая строка —
# `#<N> merge #<N>: …`, а не умолчание форжа «Merge pull request #…» (#2819).
if grep -q -- '^pr merge 77 .*--merge' "$SCRATCH/stub/log" &&
    ! grep -q -E -- '^pr merge 77 .*--(squash|rebase)' "$SCRATCH/stub/log"; then
    ok "L13b вливание — коммитом слияния (--merge), без схлопывания и rebase"
else
    bad "L13b вливание — коммитом слияния (--merge), без схлопывания и rebase" "журнал: $(tr '\n' '|' < "$SCRATCH/stub/log")"
fi
if grep -q -- "^pr merge 77 .*--subject #$ISSUE_N merge #$ISSUE_N: " "$SCRATCH/stub/log"; then
    ok "L13c первая строка коммита слияния — «#$ISSUE_N merge #$ISSUE_N: …»"
else
    bad "L13c первая строка коммита слияния — «#$ISSUE_N merge #$ISSUE_N: …»" "журнал: $(tr '\n' '|' < "$SCRATCH/stub/log")"
fi
if [ "$(bare_trunk "$LBARE")" = "$TRUNK0" ]; then
    ok "L14 ствол двигает ФОРЖ, а не производитель"
else
    bad "L14 ствол двигает ФОРЖ, а не производитель" "производитель сам сдвинул ствол"
fi

# L15 — ветка занята ЧУЖИМ деревом: отказ, и она НЕ перезаписана.
OCCUP="$SCRATCH/occupied"; mkdir -p "$OCCUP"
printf 'чужая работа\n' > "$OCCUP/ЧУЖОЕ"
( cd "$OCCUP" && git init --quiet -b x \
  && git -c user.name=p -c user.email=p@invalid add -A \
  && git -c user.name=p -c user.email=p@invalid commit --quiet -m чужое \
  && git push --quiet "$LBARE" "x:refs/heads/$WB" --force ) >/dev/null 2>&1
OCC_HEAD="$(git --git-dir="$LBARE" rev-parse "refs/heads/$WB")"
OUT="$(run_pr green "$STUB")"; RC=$?
say "L15 занятая чужим ветка — не выполнилось" 3 "занята ЧУЖИМ деревом" "$OUT" "$RC"
if [ "$(git --git-dir="$LBARE" rev-parse "refs/heads/$WB")" = "$OCC_HEAD" ]; then
    ok "L16 чужая ветка НЕ перезаписана"
else
    bad "L16 чужая ветка НЕ перезаписана" "ветка сдвинулась"
fi

echo
printf 'перепись доказательства: утверждений %d, прошло %d, провалено %d, не выполнено %d\n' \
    "$((PASS+FAIL))" "$PASS" "$FAIL" "$NOTRUN"
[ "$((PASS+FAIL))" -gt 0 ] || { echo "утверждений ноль — доказательство беспредметно" >&2; exit 2; }
# Находка объявляется ПЕРВОЙ: не созданное условие соседнего случая её не маскирует.
[ "$FAIL" = "0" ] || exit 1
[ "$NOTRUN" = "0" ] || {
    echo "ВЕРДИКТА НЕТ по $NOTRUN утверждениям: условие не создано — это не «доказано» и не находка о производителе" >&2
    exit 2; }
exit 0
