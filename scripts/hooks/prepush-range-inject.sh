#!/usr/bin/env bash
# shellcheck disable=SC2016  # выражения в кавычках — ТЕЛО дефектной копии, раскрывать их нельзя
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Инъекция для базы диапазона ПЕРВОЙ отправки — на ИСХОДЕ ХУКА, а не на
# промежуточном значении.
#
# ПОЧЕМУ ИСХОД, А НЕ НАБОР ГРУПП. Наблюдаемое #805 — «pre-push ОТКАЗ:
# изменение трогает консоль», то есть отказ отправки у ветки, не тронувшей
# консоль ни одним файлом. Проба, утверждающая только набор групп, закрепила бы
# ОТВЕТ производителя, а решение принимает хук: диапазон строится в нём, и
# производитель групп честно считает по тому, что ему выдали. Поэтому здесь
# гоняется НАСТОЯЩИЙ pre-push с настоящими производителями и заглушкой
# прогонщика, а утверждается код возврата и то, что хук напечатал.
#
# ЗАГЛУШКА ПРОГОНЩИКА НЕ СНИСХОДИТЕЛЬНЕЕ НАСТОЯЩЕГО. Она воспроизводит ровно то
# состояние свежей рабочей копии, в котором дефект и наблюдался: зависимостей
# консоли нет, поэтому КАЖДАЯ проверка группы ui-types попадает в «НЕ
# выполнено» с тем же текстом, что печатает scripts/ci-local.sh. Числа сводки —
# в той же форме, которую хук разбирает.
#
# ОБЕ СТОРОНЫ ОБЯЗАТЕЛЬНЫ. Ветка от накопительной БЕЗ правок консоли обязана
# пройти; ветка от накопительной С правкой консоли — обязана быть остановлена.
# Односторонняя проба зеленела бы на хуке, который не требует проверок консоли
# НИКОГДА, — а это тот же дефект с другой стороны и дороже: правка консоли
# уезжала бы в конвейер без единого вердикта.
#
# ИМЕНА ПЕРЕМЕННЫХ ПРОБЫ НЕ СТАЛКИВАЮТСЯ СО СПЕЦИАЛЬНЫМИ. `GROUPS` в bash —
# массив групп процесса: присваивание молча игнорируется, а `"$GROUPS"` даёт
# первый идентификатор группы. Проба, назвавшая так путь к производителю,
# искала бы файл с именем «1000» и падала бы на СВОЁМ дефекте, обвиняя дерево.
#
# ИЗОЛЯЦИЯ ОБЯЗАТЕЛЬНА. Окружение git обрывается: унаследованный GIT_DIR сильнее
# рабочего каталога, и тогда `git add` фикстуры пишет в индекс той копии, из
# которой проба запущена. Падают потом чужие гейты, читающие состав дерева, а
# виновник остаётся невидимым.
set -uo pipefail

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX
unset KACHO_PUSH_RANGE KACHO_TRUNK_REF KACHO_PREPUSH_GROUP KACHO_SKIP_PREPUSH

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK="$HERE/pre-push"
GROUPS_SH="$HERE/prepush-groups.sh"
RANGE="$HERE/prepush-range.sh"
[ -f "$HOOK" ]   || { echo "хука нет: $HOOK" >&2; exit 2; }
[ -f "$GROUPS_SH" ] || { echo "производителя групп нет: $GROUPS_SH" >&2; exit 2; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
zero="0000000000000000000000000000000000000000"

# `gh` подменяется намеренно: хук спрашивает у него идущие прогоны, а проба
# обязана быть детерминированной и не зависеть от сети и от того, кто
# аутентифицирован на этой машине.
mkdir -p "$tmp/bin"
printf '#!/bin/sh\nexit 1\n' > "$tmp/bin/gh"; chmod +x "$tmp/bin/gh"

repo="$tmp/repo"; mkdir -p "$repo"
g() { git -C "$repo" -c user.email=p@i -c user.name=p -c commit.gpgsign=false "$@"; }

mkdir -p "$repo/services" "$repo/ui-future/vpc" "$repo/scripts/hooks"
g init -q -b main
echo core > "$repo/services/a.go"; echo ui > "$repo/ui-future/vpc/a.ts"
# Оснастка самой пробы (хук, производители, заглушка прогонщика) кладётся в
# `scripts/` НЕ коммитом, и она объявляется игнорируемой — тем же словом, каким
# дерево объявляет порождённые артефакты.
#
# Без этого фикстура опиралась бы на послабление, которого у продукта нет: хук
# считает расхождением всякое неотслеживаемое, НЕ попадающее под игнор, и гонял
# бы прогон во временной выкладке, где этой оснастки нет. Фикстура, живущая
# вольготнее продукта, судила бы не то.
printf 'scripts/\n' > "$repo/.gitignore"
g add -A; g commit -qm "основание"
m0="$(g rev-parse HEAD)"

# Ствол ушёл вперёд, тронув консоль. Именно этот коммит и подмешивался в
# диапазон первой отправки у ветки, отведённой от накопительной линии.
echo 'export const trunkUi = 1' >> "$repo/ui-future/vpc/a.ts"
g add -A; g commit -qm "ствол правит консоль"
m1="$(g rev-parse HEAD)"
g update-ref refs/remotes/origin/main "$m1"

# Накопительная линия ответвилась ДО того коммита ствола и несёт свою чужую
# правку консоли: ветка, отведённая от неё, не касается консоли ни одним файлом,
# а трёхточечный диапазон от ствола вбирает обе.
g checkout -q -b line "$m0"
echo 'export const foreignUi = 1' >> "$repo/ui-future/vpc/a.ts"
g add -A; g commit -qm "чужая линия правит консоль"
echo 'package b' > "$repo/services/b.go"
g add -A; g commit -qm "линия правит сервер"
r2="$(g rev-parse HEAD)"
g update-ref refs/remotes/origin/release/line "$r2"

g checkout -q -b off-line-server "$r2"
echo 'package c' > "$repo/services/c.go"; g add -A; g commit -qm "моя правка сервера"
g checkout -q -b off-line-ui "$r2"
echo 'export const mine = 1' >> "$repo/ui-future/vpc/a.ts"; g add -A; g commit -qm "моя правка консоли"
# Ветка от СТВОЛА: её база обязана быть стволом, а не накопительной линией.
# Без этого утверждения прошёл бы производитель, всегда выбирающий релизную
# ссылку, — и правка консоли, пришедшая стволом, снова попала бы в диапазон.
g checkout -q -b off-main-server "$m1"
echo 'package d' > "$repo/services/d.go"; g add -A; g commit -qm "моя правка сервера от ствола"

# Заглушка прогонщика: числа и тексты — той же формы, что у scripts/ci-local.sh.
#
# КОД ВОЗВРАТА ЗАДАЁТСЯ СНАРУЖИ, и это не удобство. У хука ТРИ полосы вердикта —
# зелёная, красная и «прогон не состоялся», — и разводит их именно код. Заглушка,
# всегда выходящая нулём, третьей полосы не показывает ВООБЩЕ: ветку кода 3 можно
# было снять, и все три инъекции остались бы зелёными (проверено — так и было).
cat > "$repo/scripts/ci-local.sh" <<'STUB'
#!/usr/bin/env bash
set -uo pipefail
stub_rc="${KACHO_STUB_RC:-0}"
ran=0; skipped=0
for grp in "$@"; do
    case "$grp" in
        ui-types)
            for m in vpc iam system; do
                printf '\n== ui-types %s\n   ПРОПУСК: зависимости не установлены (npm ci --prefix) — НЕ выполнено\n' "$m"
                skipped=$((skipped + 1))
            done
            ;;
        *) printf '\n== %s\n   (заглушка: исполнено)\n' "$grp"; ran=$((ran + 1)) ;;
    esac
done
case "$stub_rc" in
    3)  printf '\n== итог: проверок исполнено %d, отказов 0, НЕ выполнено 1\n' "$ran"
        printf '   ПРОГОН НЕДЕЙСТВИТЕЛЕН: ресурс исчерпан на 1 шаг(ах) — go test -short\n' ;;
    1)  printf '\n== итог: проверок исполнено %d, отказов 1, НЕ выполнено %d\n' "$ran" "$skipped"
        printf '   красное: go test -short\n' ;;
    *)  printf '\n== итог: проверок исполнено %d, отказов 0, НЕ выполнено %d\n' "$ran" "$skipped" ;;
esac
exit "$stub_rc"
STUB
chmod +x "$repo/scripts/ci-local.sh"

# Дефектный производитель базы: она всегда ствол — ровно та форма, что стояла в
# хуке до #805. Подменяется ТОЛЬКО он: хук и производитель групп остаются
# настоящими, иначе проба судила бы собственную копию.
make_broken_range() {
    [ -f "$RANGE" ] || return 1
    sed 's|^\( *\)nearest_published_base "\$local_sha"|\1printf %s "${KACHO_TRUNK_REF:-origin/main}"|' \
        "$RANGE" > "$tmp/range-broken.sh"
    grep -q 'printf %s "${KACHO_TRUNK_REF:-origin/main}"' "$tmp/range-broken.sh"
}

install_hooks() { # $1 — производитель базы (путь) либо «-»; $2 — хук (умолчание: настоящий)
    cp "${2:-$HOOK}" "$repo/scripts/hooks/pre-push"
    cp "$GROUPS_SH" "$repo/scripts/hooks/prepush-groups.sh"
    rm -f "$repo/scripts/hooks/prepush-range.sh"
    [ "$1" = "-" ] || cp "$1" "$repo/scripts/hooks/prepush-range.sh"
    chmod +x "$repo/scripts/hooks"/*.sh "$repo/scripts/hooks/pre-push" 2> /dev/null || true
}

# Один прогон хука: ветка, вид отправки, окружение. Печатает «rc<код>» первой
# строкой, дальше вывод хука.
hook_run() { # $1 — ветка, $2 — remote_sha (нули = первая отправка), $3.. — окружение
    local br="$1" remote="$2"; shift 2
    g checkout -q "$br"
    local sha out rc
    sha="$(g rev-parse HEAD)"
    out="$(cd "$repo" && printf '%s %s %s %s\n' "refs/heads/$br" "$sha" "refs/heads/$br" "$remote" \
        | env "$@" PATH="$tmp/bin:$PATH" bash scripts/hooks/pre-push origin "$repo" 2>&1)"
    rc=$?
    printf 'rc%s\n%s\n' "$rc" "$out"
}

groups_of() { printf '%s' "$1" | sed -n 's/.*группы прогона: //p' | tail -1; }
rc_of()     { printf '%s' "$1" | head -1 | sed 's/^rc//'; }

assert_all() { # $1 — метка прогона, $2 — файл для числа провалов
    local tag="$1" out="$2" f=0 r rc grp
    ok()   { printf '  ok   [%s] %s\n' "$tag" "$1"; }
    fail() { printf '  FAIL [%s] %s\n' "$tag" "$1"; f=$((f + 1)); }

    r="$(hook_run off-line-server "$zero" KACHO_X=1)"; rc="$(rc_of "$r")"; grp="$(groups_of "$r")"
    if [ "$rc" = "0" ] && [ "$grp" = "proto go" ]; then
        ok "первая отправка ветки от накопительной без правок консоли проходит"
    else
        fail "первая отправка ветки от накопительной: ждали rc=0 и «proto go», получили rc=$rc и «$grp»"
    fi
    case "$r" in
        *origin/release/line*) ok "хук НАЗЫВАЕТ выбранную базу" ;;
        *) fail "хук не назвал базу: сужение, о котором не сказано, неотличимо от дефекта" ;;
    esac

    r="$(hook_run off-line-ui "$zero" KACHO_X=1)"; rc="$(rc_of "$r")"; grp="$(groups_of "$r")"
    if [ "$rc" = "1" ] && [ "$grp" = "proto go ui-types" ]; then
        ok "СВОЯ правка консоли по-прежнему требует вердикта и останавливает отправку"
    else
        fail "своя правка консоли: ждали rc=1 и «proto go ui-types», получили rc=$rc и «$grp»"
    fi

    r="$(hook_run off-main-server "$zero" KACHO_X=1)"; rc="$(rc_of "$r")"; grp="$(groups_of "$r")"
    if [ "$rc" = "0" ] && [ "$grp" = "proto go" ]; then
        ok "ветка от ствола берёт базой ствол, а не релизную линию"
    else
        fail "ветка от ствола: ждали rc=0 и «proto go», получили rc=$rc и «$grp»"
    fi

    r="$(hook_run off-line-server "$r2" KACHO_X=1)"; rc="$(rc_of "$r")"; grp="$(groups_of "$r")"
    if [ "$rc" = "0" ] && [ "$grp" = "proto go" ]; then
        ok "вторая отправка (ссылка на origin есть) не изменилась"
    else
        fail "вторая отправка: ждали rc=0 и «proto go», получили rc=$rc и «$grp»"
    fi

    r="$(hook_run off-line-server "$zero" KACHO_TRUNK_REF=refs/remotes/origin/main)"
    rc="$(rc_of "$r")"; grp="$(groups_of "$r")"
    if [ "$grp" = "proto go ui-types" ]; then
        ok "названный ствол сильнее вывода"
    else
        fail "названный ствол: ждали «proto go ui-types», получили «$grp»"
    fi

    printf '%s' "$f" > "$out"
}

# ─────────────────────────────────────────────────────────────────────────────
# ПОЛОСЫ ВЕРДИКТА ХУКА: красное против НЕСОСТОЯВШЕГОСЯ прогона
# ─────────────────────────────────────────────────────────────────────────────
# Предмет отдельный от базы диапазона, поэтому и утверждения свои, и дефект свой.
# Хук обязан развести две вещи, которые снаружи выглядят одинаково («отправка
# остановлена»): находки есть — чинить их; находок нет ВООБЩЕ, потому что прогон
# оборвался, — освобождать ресурс. Совет «почините найденное» на второй полосе
# посылает читателя искать то, чего не находили.
#
# Утверждения ДВУСТОРОННИЕ по каждой полосе: на коде 3 слова «почините найденное»
# быть НЕ должно, а «НЕДЕЙСТВИТЕЛЕН» — должно; на коде 1 ровно наоборот.
# Односторонняя проба зеленела бы на хуке, который не печатает ничего.
assert_verdict_lanes() { # $1 — метка прогона, $2 — файл для числа провалов
    local tag="$1" out="$2" f=0 r rc
    lok()   { printf '  ok   [%s] %s\n' "$tag" "$1"; }
    lfail() { printf '  FAIL [%s] %s\n' "$tag" "$1"; f=$((f + 1)); }

    r="$(hook_run off-line-server "$zero" KACHO_X=1 KACHO_STUB_RC=3)"; rc="$(rc_of "$r")"
    if [ "$rc" = "1" ]; then
        lok "несостоявшийся прогон останавливает отправку"
    else
        lfail "несостоявшийся прогон: ждали rc=1, получили rc=$rc"
    fi
    # Утверждается СОБСТВЕННАЯ строка хука, а не слово «НЕДЕЙСТВИТЕЛЕН» где угодно
    # в выводе: сводку прогонщика хук печатает целиком, и её текст удовлетворил бы
    # проверку даже при снятой ветке — то есть утверждение о хуке держалось бы на
    # чужих словах. Поймано на первом же прогоне против дефекта.
    case "$r" in
        *"pre-push ОТКАЗ: локальный прогон НЕДЕЙСТВИТЕЛЕН"*)
            lok "…и ХУК называет прогон недействительным своей строкой" ;;
        *) lfail "…но своей строки про недействительный прогон хук не напечатал" ;;
    esac
    case "$r" in
        *"pre-push ОТКАЗ: локальные проверки красные"*)
            lfail "…и всё же объявил прогон красным — находок НЕТ НИ ОДНОЙ" ;;
        *) lok "…и НЕ объявляет прогон красным" ;;
    esac
    case "$r" in
        *"почините найденное"*) lfail "…и всё же советует «почините найденное» — находок НЕТ НИ ОДНОЙ" ;;
        *) lok "…и НЕ советует чинить найденное" ;;
    esac

    r="$(hook_run off-line-server "$zero" KACHO_X=1 KACHO_STUB_RC=1)"; rc="$(rc_of "$r")"
    if [ "$rc" = "1" ]; then
        lok "красный прогон по-прежнему останавливает отправку"
    else
        lfail "красный прогон: ждали rc=1, получили rc=$rc"
    fi
    case "$r" in
        *"почините найденное"*) lok "…и хук по-прежнему советует починить найденное" ;;
        *) lfail "…но совета «почините найденное» нет — красная полоса потеряла свой текст" ;;
    esac
    case "$r" in
        *"pre-push ОТКАЗ: локальный прогон НЕДЕЙСТВИТЕЛЕН"*)
            lfail "…и всё же зовёт красный прогон недействительным" ;;
        *) lok "…и НЕ зовёт красный прогон недействительным" ;;
    esac

    printf '%s' "$f" > "$out"
}

# Дефект полос — ровно та форма, что была до #862: ветки кода 3 нет, и
# несостоявшийся прогон попадает в общую красную ветку вместе с её советом.
make_broken_lanes() {
    sed 's/^if \[ "\$run_rc" -eq 3 \]; then$/if [ "$run_rc" -eq 33 ]; then/' "$HOOK" > "$tmp/hook-no-lane"
    grep -q '"\$run_rc" -eq 33' "$tmp/hook-no-lane"
}

echo "── прогон против настоящего хука (ждём ноль провалов)"
install_hooks "$([ -f "$RANGE" ] && echo "$RANGE" || echo -)"
assert_all настоящий "$tmp/real.n"; real_fails="$(cat "$tmp/real.n")"

broken_fails="н/д"
if make_broken_range; then
    echo
    echo "── прогон против воссозданного дефекта «база всегда ствол» (ждём хотя бы один провал)"
    install_hooks "$tmp/range-broken.sh"
    assert_all дефект "$tmp/broken.n"; broken_fails="$(cat "$tmp/broken.n")"
fi

echo
echo "── полосы вердикта хука (ждём ноль провалов)"
install_hooks "$([ -f "$RANGE" ] && echo "$RANGE" || echo -)"
assert_verdict_lanes настоящий "$tmp/lanes-real.n"; lanes_real="$(cat "$tmp/lanes-real.n")"

lanes_broken="н/д"
if make_broken_lanes; then
    echo
    echo "── прогон против дефекта «ветки кода 3 нет» (ждём хотя бы один провал)"
    install_hooks "$([ -f "$RANGE" ] && echo "$RANGE" || echo -)" "$tmp/hook-no-lane"
    assert_verdict_lanes дефект-полос "$tmp/lanes-broken.n"; lanes_broken="$(cat "$tmp/lanes-broken.n")"
fi

# ПОСТУСЛОВИЕ ОТПРАВКИ (#2799). Код хука — исход ПРОВЕРОК, а не отправки: пакет
# уходит после хука, и обрыв соединения там кодом хука не виден. Поэтому хук
# обязан напечатать, чем отправка проверяется, — ссылку и отправленную sha. Дефект
# своего свойства — снятый вызов печати.
make_broken_postcondition() {
    sed 's/^postcondition_note$/: postcondition_note снят/' "$HOOK" > "$tmp/hook-no-post"
    grep -q '^: postcondition_note снят$' "$tmp/hook-no-post"
}

assert_postcondition() { # $1 — метка прогона, $2 — файл для числа провалов
    local tag="$1" out="$2" f=0 r rc sha
    pok()   { printf '  ok   [%s] %s\n' "$tag" "$1"; }
    pfail() { printf '  FAIL [%s] %s\n' "$tag" "$1"; f=$((f + 1)); }
    sha="$(g rev-parse off-main-server)"
    r="$(hook_run off-main-server "$zero" KACHO_X=1)"; rc="$(rc_of "$r")"
    if [ "$rc" = "0" ]; then pok "зелёная отправка проходит"; else pfail "зелёная отправка остановлена (rc=$rc)"; fi
    case "$r" in
        *"git ls-remote origin refs/heads/off-main-server   # ожидается $sha"*)
            pok "хук печатает постусловие отправки: ссылку и отправленную sha" ;;
        *) pfail "постусловия отправки (ссылка и sha) в выводе хука нет" ;;
    esac
    case "$r" in
        *"исход ПРОВЕРОК, а не отправки"*) pok "хук называет свой код кодом проверок" ;;
        *) pfail "хук не говорит, что его код — исход проверок, а не отправки" ;;
    esac
    printf '%s' "$f" > "$out"
}

echo
echo "── постусловие отправки (ждём ноль провалов)"
install_hooks "$([ -f "$RANGE" ] && echo "$RANGE" || echo -)"
assert_postcondition настоящий "$tmp/post-real.n"; post_real="$(cat "$tmp/post-real.n")"

post_broken="н/д"
if make_broken_postcondition; then
    echo
    echo "── прогон против дефекта «постусловие не печатается» (ждём хотя бы один провал)"
    install_hooks "$([ -f "$RANGE" ] && echo "$RANGE" || echo -)" "$tmp/hook-no-post"
    assert_postcondition дефект-постусловия "$tmp/post-broken.n"; post_broken="$(cat "$tmp/post-broken.n")"
fi

# ─────────────────────────────────────────────────────────────────────────────
# ПРЕДМЕТ ВЕРДИКТА: ТО, ЧТО УЕЗЖАЕТ, А НЕ ТО, НА ЧЁМ СТОИТ КОПИЯ (#2594)
# ─────────────────────────────────────────────────────────────────────────────
# Свойство своё, дефект свой, фикстура своя. Хук отвечает на ОДИН вопрос — «что
# уезжает» — и обязан отвечать на него одним способом: входом от git. Два
# следствия расхождения наблюдались оба, и оба здесь утверждаются.
#
# ПОЧЕМУ ОТДЕЛЬНАЯ ФИКСТУРА. Утверждается не «прогон состоялся», а «прогон
# состоялся НАД ТЕМ ДЕРЕВОМ». Отличить одно от другого можно только заглушкой,
# которая отвечает О СВОЁМ дереве: она читает `MARKER` рядом с собой и печатает
# его. Поэтому и заглушка, и хуки здесь КОММИТЯТСЯ: временная рабочая копия
# отправляемой ревизии содержит ровно то, что в этой ревизии лежит, — а
# неотслеживаемый файл в неё не попадает.
#
# ЗАГЛУШКА КРАСНЕЕТ САМА. Дерево с меткой `broken` она объявляет красным. Без
# этого «наоборот» из предиката снятия — чистая копия, ломаная отправляемая
# ревизия — нечем утвердить: зелёное было бы зелёным при любом выборе дерева.
subj="$tmp/subject"
s() { git -C "$subj" -c user.email=p@i -c user.name=p -c commit.gpgsign=false "$@"; }

subject_fixture() { # $1 — хук, который кладётся в дерево и коммитится
    rm -rf "$subj"
    mkdir -p "$subj/scripts/hooks" "$subj/services"
    s init -q -b main
    cp "$1" "$subj/scripts/hooks/pre-push"
    cp "$GROUPS_SH" "$subj/scripts/hooks/prepush-groups.sh"
    [ -f "$RANGE" ] && cp "$RANGE" "$subj/scripts/hooks/prepush-range.sh"
    chmod +x "$subj/scripts/hooks/pre-push" "$subj/scripts/hooks"/*.sh 2> /dev/null || true

    cat > "$subj/scripts/ci-local.sh" <<'SUBJSTUB'
#!/usr/bin/env bash
# Заглушка прогонщика, отвечающая О СВОЁМ ДЕРЕВЕ: корень она берёт от себя (как
# настоящий scripts/ci-local.sh), печатает метку этого дерева и краснеет, если
# дерево объявлено сломанным.
set -uo pipefail
# Факт вызова пишется ВНЕ дерева, когда проба об этом просит: утверждение
# «прогонщика не звали» не должно держаться на том, что хук напечатал его вывод.
[ -z "${KACHO_STUB_CALLS:-}" ] || printf 'вызван: %s\n' "$*" >> "$KACHO_STUB_CALLS"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
marker="$(cat "$root/MARKER" 2> /dev/null || printf 'МЕТКИ-НЕТ')"
stray=no
[ -e "$root/services/stray.go" ] && stray=yes
printf '\n== заглушка прогонщика судит дерево\n'
printf '   MARKER:%s STRAY:%s\n' "$marker" "$stray"
if [ "$marker" = "broken" ]; then
    printf '\n== итог: проверок исполнено 1, отказов 1, НЕ выполнено 0\n'
    printf '   красное: дерево объявлено сломанным\n'
    exit 1
fi
printf '\n== итог: проверок исполнено 1, отказов 0, НЕ выполнено 0\n'
exit 0
SUBJSTUB
    chmod +x "$subj/scripts/ci-local.sh"

    printf 'base\n' > "$subj/MARKER"; printf 'package a\n' > "$subj/services/a.go"
    # Правило игнора — часть фикстуры: игнорируемый артефакт и неотслеживаемый
    # исходник различаются ровно им, и без него две ветки «расхождения» слиплись бы.
    printf 'ignored-artifact\n' > "$subj/.gitignore"
    s add -A; s commit -qm "основание"
    s update-ref refs/remotes/origin/main "$(s rev-parse HEAD)"

    s checkout -q -b feature/y
    printf 'feature\n' > "$subj/MARKER"; printf 'package y\n' > "$subj/services/y.go"
    s add -A; s commit -qm "моя работа"

    s checkout -q -b feature/bad main
    printf 'broken\n' > "$subj/MARKER"; printf 'package bad\n' > "$subj/services/bad.go"
    s add -A; s commit -qm "работа, которую прогон обязан покраснить"

    s checkout -q -b wip/draft main
    printf 'draft\n' > "$subj/MARKER"; printf 'package d\n' > "$subj/services/d.go"
    s add -A; s commit -qm "черновик"

    s checkout -q main
}

# subj_run — один прогон хука с ЯВНО заданным входом git и явным состоянием копии.
# $1 — где стоит копия: имя ветки либо «detach:<ревизия>»
# $2 — чем испачкать отслеживаемый файл ДО отправки («-» — ничем)
# $3 — строки входа хука (по одной на ссылку)
# $4.. — окружение
subj_run() {
    local at="$1" dirty="$2" input="$3"; shift 3
    case "$at" in
        detach:*) s checkout -q --detach "${at#detach:}" ;;
        *)        s checkout -q "$at" ;;
    esac
    s checkout -q -- MARKER 2> /dev/null || true
    rm -f "$subj/services/stray.go" "$subj/ignored-artifact"
    case "$dirty" in
        -) : ;;
        # Лишний исходник рядом: не отслеживается, под игнор не попадает.
        stray) printf 'package stray\n' > "$subj/services/stray.go" ;;
        # Порождённый артефакт: не отслеживается И попадает под игнор.
        ignored) printf 'кэш\n' > "$subj/ignored-artifact" ;;
        *) printf '%s\n' "$dirty" > "$subj/MARKER" ;;
    esac
    local out rc
    out="$(cd "$subj" && printf '%s\n' "$input" \
        | env "$@" PATH="$tmp/bin:$PATH" bash scripts/hooks/pre-push origin "$subj" 2>&1)"
    rc=$?
    s checkout -q -- MARKER 2> /dev/null || true
    rm -f "$subj/services/stray.go" "$subj/ignored-artifact"
    printf 'rc%s\n%s\n' "$rc" "$out"
}

# ПРОПУСК ЧЕРНОВИКА ВЫВОДИТСЯ ИЗ ВХОДА GIT, А НЕ ИЗ ИМЕНИ ВЕТКИ КОПИИ.
# Обе стороны обязательны: односторонняя проба зеленела бы на хуке, который не
# пропускает НИКОГДА, — а вторая сторона хуже первой: непроверенное уезжает под
# видом проверенного.
assert_draft_by_input() { # $1 — метка, $2 — файл для числа провалов
    local tag="$1" out="$2" f=0 r rc wip_sha feat_sha
    dok()   { printf '  ok   [%s] %s\n' "$tag" "$1"; }
    dfail() { printf '  FAIL [%s] %s\n' "$tag" "$1"; f=$((f + 1)); }

    wip_sha="$(s rev-parse wip/draft)"; feat_sha="$(s rev-parse feature/y)"

    r="$(subj_run "detach:$feat_sha" - "refs/heads/wip/draft $wip_sha refs/heads/wip/draft $zero" KACHO_X=1)"
    rc="$(rc_of "$r")"
    if [ "$rc" = "0" ]; then dok "черновик из копии с отсоединённой головой не отвергнут"
    else dfail "черновик из отсоединённой копии: ждали rc=0, получили rc=$rc"; fi
    case "$r" in
        *"все ссылки отправки объявлены черновиками"*)
            dok "…и хук назвал черновиком САМУ ОТПРАВКУ, а не ветку копии" ;;
        *) dfail "…но своей строки о черновой отправке хук не напечатал — решение всё ещё выводится из имени ветки копии" ;;
    esac
    case "$r" in
        *"группы прогона"*) dfail "…и всё же гонял проверки: пропуск не сработал там, где обязан" ;;
        *) dok "…и проверок не гонял" ;;
    esac

    r="$(subj_run wip/draft - "refs/heads/feature/y $feat_sha refs/heads/feature/y $zero" KACHO_X=1)"
    case "$r" in
        *"проверки пропущены"*) dfail "копия стоит на wip/, а уезжает feature/ — и проверки пропущены: непроверенное уедет под видом проверенного" ;;
        *) dok "имя ветки КОПИИ черновиком отправку не делает" ;;
    esac

    r="$(subj_run wip/draft - "$(printf 'refs/heads/wip/draft %s refs/heads/wip/draft %s\nrefs/heads/feature/y %s refs/heads/feature/y %s' "$wip_sha" "$zero" "$feat_sha" "$zero")" KACHO_X=1)"
    case "$r" in
        *СМЕШАННАЯ*) dok "смешанная отправка названа смешанной" ;;
        *) dfail "смешанная отправка не названа: пропуск, о котором не сказано, неотличим от дефекта" ;;
    esac
    case "$r" in
        *"проверки пропущены"*) dfail "…и всё же пропущена целиком" ;;
        *) dok "…и черновиком не считается" ;;
    esac

    printf '%s' "$f" > "$out"
}

# ВЕРДИКТ — О ТОМ ДЕРЕВЕ, КОТОРОЕ УЕЗЖАЕТ.
# Утверждается не «прогон состоялся», а КАКОЕ дерево он судил: заглушка печатает
# метку своего корня. Обе стороны обязательны, и вторая («и наоборот») дороже:
# без неё зелёное было бы зелёным при любом выборе дерева.
assert_verdict_subject() { # $1 — метка, $2 — файл для числа провалов
    local tag="$1" out="$2" f=0 r rc feat_sha bad_sha main_sha trees
    vok()   { printf '  ok   [%s] %s\n' "$tag" "$1"; }
    vfail() { printf '  FAIL [%s] %s\n' "$tag" "$1"; f=$((f + 1)); }

    feat_sha="$(s rev-parse feature/y)"; bad_sha="$(s rev-parse feature/bad)"
    main_sha="$(s rev-parse main)"

    # Законный близнец, и он первым: пока он красный, всякое отрицание ниже
    # зеленеет по чужой причине.
    r="$(subj_run feature/y - "refs/heads/feature/y $feat_sha refs/heads/feature/y $zero" KACHO_X=1)"
    rc="$(rc_of "$r")"
    case "$r" in
        *"MARKER:feature"*) vok "обычная отправка из своей ветки судит своё дерево" ;;
        *) vfail "обычная отправка: дерево вердикта не feature — «$(printf '%s' "$r" | sed -n 's/.*MARKER:\([^ ]*\).*/\1/p' | tail -1)»" ;;
    esac
    if [ "$rc" = "0" ]; then vok "…и отправка не остановлена"; else vfail "…но отправка остановлена (rc=$rc)"; fi

    # Копия стоит на ЧУЖОЙ ревизии — ровно измеренный случай #2594.
    r="$(subj_run "detach:$main_sha" - "refs/heads/feature/y $feat_sha refs/heads/feature/y $zero" KACHO_X=1)"
    case "$r" in
        *"MARKER:feature"*) vok "копия на чужой ревизии: судится ОТПРАВЛЯЕМОЕ дерево" ;;
        *) vfail "копия на чужой ревизии: вердикт вынесен о рабочей копии, а не об отправляемом" ;;
    esac
    case "$r" in
        *"MARKER:base"*) vfail "…и метка рабочей копии попала в вердикт" ;;
        *) vok "…и метка рабочей копии в вердикт не попала" ;;
    esac

    # Грязное рабочее дерево при ЧИСТОЙ отправляемой ревизии не краснит.
    r="$(subj_run feature/y broken "refs/heads/feature/y $feat_sha refs/heads/feature/y $zero" KACHO_X=1)"
    rc="$(rc_of "$r")"
    if [ "$rc" = "0" ]; then vok "грязная копия при чистой отправляемой ревизии не краснит"
    else vfail "грязная копия при чистой отправляемой ревизии остановила отправку (rc=$rc)"; fi
    case "$r" in
        *"MARKER:feature"*) vok "…и вердикт вынесен о committed-ревизии" ;;
        *) vfail "…и вердикт вынесен о грязном рабочем дереве" ;;
    esac

    # И НАОБОРОТ: копия чистая и здоровая, а уезжает ломаная ревизия.
    r="$(subj_run feature/y - "refs/heads/feature/bad $bad_sha refs/heads/feature/bad $zero" KACHO_X=1)"
    rc="$(rc_of "$r")"
    if [ "$rc" = "1" ]; then vok "ломаная отправляемая ревизия краснит при здоровой копии"
    else vfail "ломаная отправляемая ревизия при здоровой копии: ждали rc=1, получили rc=$rc — непроверенное уедет под видом проверенного"; fi
    case "$r" in
        *"MARKER:broken"*) vok "…и судилось именно отправляемое дерево" ;;
        *) vfail "…но судилось не отправляемое дерево" ;;
    esac

    # Временных рабочих деревьев за собой хук не оставляет: состояние, которое он
    # завёл, он и снимает. Иначе следующая отправка начинается с мусора в .git.
    #
    # Отказ САМОЙ КОМАНДЫ не гасится: `grep -c` на пустом входе даёт ноль, и
    # утверждение «ничего не осталось» прошло бы, не посмотрев никуда. Перепись
    # берётся только у состоявшегося опроса.
    if ! trees="$(s worktree list 2>&1)"; then
        vfail "опрос рабочих копий не состоялся — утверждать об остатках нечем: $trees"
    else
        local n; n="$(printf '%s\n' "$trees" | grep -c .)"
        if [ "$n" -le 1 ]; then vok "временных рабочих деревьев за собой не оставлено (осмотрено записей $n)"
        else vfail "в .git осталось рабочих деревьев: $((n - 1))"; fi
    fi

    # ── точка 4: неотслеживаемое, не попадающее под игнор, в вердикт НЕ входит ──
    # Лишний `.go` рядом компилируется в рабочей копии и отсутствует в отправляемой
    # ревизии: «забыл `git add`» — самый частый случай ложного зелёного.
    r="$(subj_run feature/y stray "refs/heads/feature/y $feat_sha refs/heads/feature/y $zero" KACHO_X=1)"
    case "$r" in
        *"STRAY:no"*) vok "лишний неотслеживаемый исходник в вердикт не вошёл" ;;
        *) vfail "лишний неотслеживаемый исходник вошёл в вердикт — судилась рабочая копия" ;;
    esac

    # …а ИГНОРИРУЕМЫЙ артефакт расхождением не считается: он вход прогонщика, не
    # часть контракта, и объявив его расхождением, мы отняли бы у обычной отправки
    # быстрый путь вместе с её артефактами.
    r="$(subj_run feature/y ignored "refs/heads/feature/y $feat_sha refs/heads/feature/y $zero" KACHO_X=1)"
    case "$r" in
        *"вердикт — о рабочей копии"*) vok "игнорируемый артефакт расхождением не считается" ;;
        *) vfail "игнорируемый артефакт объявлен расхождением — обычная отправка теряет быстрый путь" ;;
    esac

    # ── точка 3: сборщик снимает СВОЁ и не трогает ЧУЖОГО ─────────────────────
    #
    # Обе стороны обязательны, и вторая здесь несущая. «Своё снято» доказывает,
    # что сборщик работает; «чужое живо» — что он работает ТОЛЬКО по своему
    # признаку. Без второй стороны расширение обхода (общий `git worktree prune`,
    # приставка пошире, снятие проверки живости) прошло бы молча — а в этом клоне
    # рядом лежат два десятка чужих рабочих копий, и снявший их не узнал бы об
    # этом от пробы.
    #
    # Идентификатор своего остатка заведомо мёртвый: выше предела ядра, занять его
    # нельзя. Чужая копия лежит РЯДОМ, в том же каталоге, и отличается только
    # именем — то есть проба ловит именно признак, а не расположение.
    local dead="$subj/.git/kacho-prepush-subject/kacho-prepush-4194305"
    local alien="$subj/.git/kacho-prepush-subject/chuzhaya-kopiya"
    mkdir -p "$(dirname "$dead")"
    if s worktree add --detach --quiet "$dead" "$main_sha" > /dev/null 2>&1 &&
        s worktree add --detach --quiet "$alien" "$main_sha" > /dev/null 2>&1; then
        r="$(subj_run feature/y - "refs/heads/feature/y $feat_sha refs/heads/feature/y $zero" KACHO_X=1)"
        if [ -e "$dead" ]; then
            vfail "осиротевшая выкладка прошлой отправки не собрана — мусор копится в .git"
        else
            vok "осиротевшая выкладка прошлой отправки собрана"
        fi
        # ОТРИЦАТЕЛЬНАЯ сторона: чужая рабочая копия пережила сборку.
        if [ -e "$alien" ]; then
            vok "…а чужая рабочая копия рядом ЖИВА — сборщик снимает только своё"
        else
            vfail "…но чужая рабочая копия рядом снесена: сборщик вышел за свой признак"
        fi
        case "$r" in
            *"собрано осиротевших выкладок"*) vok "…и сборка НАЗВАНА числом, а не сделана молча" ;;
            *) vfail "…но о сборке не сказано: уборка, о которой молчат, неотличима от её отсутствия" ;;
        esac
        s worktree remove --force "$dead" > /dev/null 2>&1 || true
        s worktree remove --force "$alien" > /dev/null 2>&1 || true
        rm -rf "$dead" "$alien"
    else
        vfail "фикстуру выкладок (своей и чужой) завести не удалось — утверждать о сборке нечем"
    fi

    printf '%s' "$f" > "$out"
}

# Дефект пропуска — ровно та форма, что стояла до #2594: решение выводится из
# имени ветки РАБОЧЕЙ КОПИИ, а не из входа git. Подменяется одна строка.
make_broken_draft() {
    sed 's/^    if \[ "\$push_refs_total" -eq 0 \]; then$/    if true; then/' "$HOOK" > "$tmp/hook-draft-by-head"
    grep -q '^    if true; then$' "$tmp/hook-draft-by-head"
}

# Дефект сборщика — снятый вызов: уборка по выходу остаётся, а остатки жёсткого
# обрыва собирать становится некому. Свойство своё, дефект свой.
make_broken_reap() {
    sed 's/^reap_own_subject_trees$/: сборщик снят/' "$HOOK" > "$tmp/hook-no-reap"
    grep -q '^: сборщик снят$' "$tmp/hook-no-reap"
}

# Дефект ОБЛАСТИ сборщика — расширенный обход: он ходит по всему каталогу и не
# разбирает идентификатор. Ровно та правка, которую сделают «заодно», и ровно та,
# от которой отрицательная сторона утверждения и защищает.
make_broken_reap_scope() {
    sed -e 's|^    for dir in "\$subject_tree_parent"/kacho-prepush-\*; do$|    for dir in "$subject_tree_parent"/*; do|' \
        -e "s|^        case \"\$pid\" in '' \| \*\[!0-9\]\*) continue ;; esac$|        :|" \
        "$HOOK" > "$tmp/hook-wide-reap"
    grep -q '^    for dir in "\$subject_tree_parent"/\*; do$' "$tmp/hook-wide-reap" &&
        ! grep -q 'case "\$pid" in' "$tmp/hook-wide-reap"
}

# Дефект предмета — та же форма: прогон идёт в рабочей копии, чем бы она ни была.
make_broken_subject() {
    sed 's|^subject_root="$(subject_tree_for_push)"$|subject_root="$ROOT"|' "$HOOK" > "$tmp/hook-subject-is-cwd"
    grep -q '^subject_root="\$ROOT"$' "$tmp/hook-subject-is-cwd"
}

echo
echo "── предмет вердикта отправки: черновик по входу (ждём ноль провалов)"
subject_fixture "$HOOK"
assert_draft_by_input настоящий "$tmp/draft-real.n"; draft_real="$(cat "$tmp/draft-real.n")"

echo
echo "── предмет вердикта отправки: судится отправляемая ревизия (ждём ноль провалов)"
assert_verdict_subject настоящий "$tmp/subj-real.n"; subj_real="$(cat "$tmp/subj-real.n")"

draft_broken="н/д"
if make_broken_draft; then
    echo
    echo "── прогон против дефекта «черновик по имени ветки копии» (ждём хотя бы один провал)"
    subject_fixture "$tmp/hook-draft-by-head"
    assert_draft_by_input дефект-черновика "$tmp/draft-broken.n"; draft_broken="$(cat "$tmp/draft-broken.n")"
fi

reap_broken="н/д"
if make_broken_reap; then
    echo
    echo "── прогон против дефекта «сборщика остатков нет» (ждём хотя бы один провал)"
    subject_fixture "$tmp/hook-no-reap"
    assert_verdict_subject дефект-сборщика "$tmp/reap-broken.n"; reap_broken="$(cat "$tmp/reap-broken.n")"
fi

scope_broken="н/д"
if make_broken_reap_scope; then
    echo
    echo "── прогон против дефекта «сборщик ходит шире своего признака» (ждём хотя бы один провал)"
    subject_fixture "$tmp/hook-wide-reap"
    assert_verdict_subject дефект-области "$tmp/scope-broken.n"; scope_broken="$(cat "$tmp/scope-broken.n")"
fi

subj_broken="н/д"
if make_broken_subject; then
    echo
    echo "── прогон против дефекта «судится рабочая копия» (ждём хотя бы один провал)"
    subject_fixture "$tmp/hook-subject-is-cwd"
    assert_verdict_subject дефект-предмета "$tmp/subj-broken.n"; subj_broken="$(cat "$tmp/subj-broken.n")"
fi

# ─────────────────────────────────────────────────────────────────────────────
# ОТПРАВКА ОДНИХ УДАЛЕНИЙ: ПРОГОНА НЕТ, И ОБ ЭТОМ СКАЗАНО (#2825)
# ─────────────────────────────────────────────────────────────────────────────
# Удаление ссылки не несёт ревизии, судить в нём нечего, — а прогон шёл всё
# равно, над рабочей копией, из которой отправляют. Утверждается ИСХОД: код хука
# и ФАКТ вызова прогонщика, который заглушка записывает вне дерева.
#
# Копия стоит на ревизии, которую заглушка объявляет красной. Поэтому прогон,
# позванный на удалении, виден дважды — записью вызова и кодом 1, — и зелёным
# по совпадению не выходит.
#
# ДЕФЕКТОВ ДВА, И ОБА ОБЯЗАНЫ КРАСНЕТЬ. Узкий: удаление гонит прогон (форма до
# #2825). Широкий: пропуск срабатывает на ЛЮБОЙ строке удаления, и смешанная
# отправка уезжает без вердикта о своей вершине. Второй дороже первого:
# непроверенное уходит под видом проверенного. Его ловит законный близнец —
# та же строка удаления рядом с ненулевой вершиной.
assert_delete_only() { # $1 — метка, $2 — файл для числа провалов
    local tag="$1" out="$2" f=0 r rc n feat_sha bad_sha calls="$tmp/runner-calls"
    xok()   { printf '  ok   [%s] %s\n' "$tag" "$1"; }
    xfail() { printf '  FAIL [%s] %s\n' "$tag" "$1"; f=$((f + 1)); }
    # Записи нет вовсе — утверждать о вызове нечем, и это провал, а не ноль.
    calls_of() { [ -f "$calls" ] || { printf 'записи-нет'; return; }; grep -c . "$calls" || true; }

    feat_sha="$(s rev-parse feature/y)"; bad_sha="$(s rev-parse feature/bad)"

    : > "$calls"
    r="$(subj_run feature/bad - "(delete) $zero refs/heads/gone $feat_sha" KACHO_X=1 KACHO_STUB_CALLS="$calls")"
    rc="$(rc_of "$r")"; n="$(calls_of)"
    if [ "$rc" = "0" ]; then xok "удаление ссылки из копии с красным прогоном не отвергнуто"
    else xfail "удаление ссылки: ждали rc=0, получили rc=$rc — снятие ссылки зависит от зелёности рабочей копии"; fi
    if [ "$n" = "0" ]; then xok "…и прогонщик не вызван (записей вызова 0)"
    else xfail "…но прогонщик вызван (записей вызова $n) — удаление судится проверками содержимого"; fi
    case "$r" in
        *"pre-push: отправка несёт только удаление ссылок"*) xok "…и хук своей строкой говорит, что уезжает только удаление" ;;
        *) xfail "…но своей строки об отправке одних удалений хук не напечатал" ;;
    esac
    case "$r" in
        *"проверки содержимого НЕ выполнялись"*) xok "…и что проверок не было: «не выполнялось» отличимо от «зелено»" ;;
        *) xfail "…но не сказал, что проверок не было: пропуск, о котором молчат, читается зелёным" ;;
    esac
    case "$r" in
        *"локальные проверки зелёные"*) xfail "…и всё же объявил проверки зелёными — их не было" ;;
        *) xok "…и НЕ объявляет проверки зелёными" ;;
    esac
    case "$r" in
        *"git ls-remote origin refs/heads/gone   # ожидается пустой вывод"*)
            xok "…и печатает постусловие удаления: ссылки на сервере быть не должно" ;;
        *) xfail "…но постусловия удаления (ссылка и пустой ответ сервера) в выводе нет" ;;
    esac

    : > "$calls"
    r="$(subj_run feature/bad - "$(printf '(delete) %s refs/heads/gone %s\n(delete) %s refs/heads/gone2 %s' \
        "$zero" "$feat_sha" "$zero" "$bad_sha")" KACHO_X=1 KACHO_STUB_CALLS="$calls")"
    rc="$(rc_of "$r")"; n="$(calls_of)"
    if [ "$rc" = "0" ] && [ "$n" = "0" ]; then xok "два удаления разом: rc=0, прогонщик не вызван"
    else xfail "два удаления разом: ждали rc=0 и 0 вызовов, получили rc=$rc и $n"; fi

    # ЗАКОННЫЙ БЛИЗНЕЦ: та же строка удаления и ненулевая вершина. Вершина
    # ломаная, копия здоровая — прогон обязан состояться над вершиной и отвергнуть.
    : > "$calls"
    r="$(subj_run feature/y - "$(printf '(delete) %s refs/heads/gone %s\nrefs/heads/feature/bad %s refs/heads/feature/bad %s' \
        "$zero" "$feat_sha" "$bad_sha" "$zero")" KACHO_X=1 KACHO_STUB_CALLS="$calls")"
    rc="$(rc_of "$r")"; n="$(calls_of)"
    if [ "$n" = "1" ]; then xok "смешанная отправка (удаление и вершина): прогонщик вызван"
    else xfail "смешанная отправка: ждали 1 вызов прогонщика, получили $n — вершина уезжает без вердикта"; fi
    if [ "$rc" = "1" ]; then xok "…и красная вершина отправку останавливает"
    else xfail "…но красная вершина отправку не остановила (rc=$rc)"; fi
    case "$r" in
        *"MARKER:broken"*) xok "…и судилась именно отправляемая вершина" ;;
        *) xfail "…но судилась не отправляемая вершина" ;;
    esac

    # Тот же близнец со здоровой вершиной проходит, и постусловие называет ОБЕ
    # ссылки: удалённую — пустым ответом, отправленную — её sha.
    : > "$calls"
    r="$(subj_run feature/y - "$(printf '(delete) %s refs/heads/gone %s\nrefs/heads/feature/y %s refs/heads/feature/y %s' \
        "$zero" "$bad_sha" "$feat_sha" "$zero")" KACHO_X=1 KACHO_STUB_CALLS="$calls")"
    rc="$(rc_of "$r")"; n="$(calls_of)"
    if [ "$rc" = "0" ] && [ "$n" = "1" ]; then xok "смешанная отправка со здоровой вершиной проходит после прогона"
    else xfail "смешанная отправка со здоровой вершиной: ждали rc=0 и 1 вызов, получили rc=$rc и $n"; fi
    case "$r" in
        *"refs/heads/gone   # ожидается пустой вывод"*"refs/heads/feature/y   # ожидается $feat_sha"*)
            xok "…и постусловие называет обе ссылки: удалённую и отправленную" ;;
        *) xfail "…но постусловие смешанной отправки не называет обе ссылки" ;;
    esac

    printf '%s' "$f" > "$out"
}

# Узкий дефект — форма до #2825: ветви одних удалений нет, и прогон идёт.
make_broken_delete_runs() {
    sed 's/^if push_is_deletion_only; then$/if false; then/' "$HOOK" > "$tmp/hook-delete-runs"
    grep -q '^if false; then$' "$tmp/hook-delete-runs"
}

# Широкий дефект — пропуск на ЛЮБОЙ строке удаления, а не на отправке, где
# удаления ВСЕ. Ровно та правка, которую сделают «проще».
make_broken_delete_wide() {
    sed 's/\[ "\$push_refs_deleted" -eq "\$push_refs_total" \]/[ "$push_refs_deleted" -gt 0 ]/' \
        "$HOOK" > "$tmp/hook-delete-wide"
    grep -q '\[ "\$push_refs_deleted" -gt 0 \]' "$tmp/hook-delete-wide"
}

echo
echo "── отправка одних удалений (ждём ноль провалов)"
subject_fixture "$HOOK"
assert_delete_only настоящий "$tmp/del-real.n"; del_real="$(cat "$tmp/del-real.n")"

del_runs_broken="н/д"
if make_broken_delete_runs; then
    echo
    echo "── прогон против дефекта «удаление гонит прогон» (ждём хотя бы один провал)"
    subject_fixture "$tmp/hook-delete-runs"
    assert_delete_only дефект-удаления "$tmp/del-runs-broken.n"; del_runs_broken="$(cat "$tmp/del-runs-broken.n")"
fi

del_wide_broken="н/д"
if make_broken_delete_wide; then
    echo
    echo "── прогон против дефекта «удаление глотает смешанную отправку» (ждём хотя бы один провал)"
    subject_fixture "$tmp/hook-delete-wide"
    assert_delete_only дефект-смешанной "$tmp/del-wide-broken.n"; del_wide_broken="$(cat "$tmp/del-wide-broken.n")"
fi

echo
printf 'prepush-range-inject: утверждений на прогон — 6 (база диапазона) · 7 (полосы вердикта)\n'
printf '                      · 6 (черновик по входу) · 14 (предмет вердикта) · 3 (постусловие)\n'
printf '                      · 12 (одни удаления)\n'
printf '  провалов у настоящего: %s (норма 0)\n' "$real_fails"
printf '  провалов у дефекта базы: %s (норма ≥1 — иначе проба ничего не проверяет)\n' "$broken_fails"
printf '  провалов у настоящего (полосы): %s (норма 0)\n' "$lanes_real"
printf '  провалов у дефекта полос:       %s (норма ≥1 — своё свойство, свой дефект)\n' "$lanes_broken"
printf '  провалов у настоящего (черновик по входу): %s (норма 0)\n' "$draft_real"
printf '  провалов у дефекта черновика:              %s (норма ≥1)\n' "$draft_broken"
printf '  провалов у настоящего (предмет вердикта):  %s (норма 0)\n' "$subj_real"
printf '  провалов у дефекта предмета:               %s (норма ≥1)\n' "$subj_broken"
printf '  провалов у дефекта сборщика остатков:      %s (норма ≥1)\n' "$reap_broken"
printf '  провалов у дефекта ОБЛАСТИ сборщика:       %s (норма ≥1 — чужая копия рядом)\n' "$scope_broken"
printf '  провалов у настоящего (постусловие):       %s (норма 0)\n' "$post_real"
printf '  провалов у дефекта постусловия:            %s (норма ≥1)\n' "$post_broken"
printf '  провалов у настоящего (одни удаления):     %s (норма 0)\n' "$del_real"
printf '  провалов у дефекта «удаление гонит прогон»: %s (норма ≥1)\n' "$del_runs_broken"
printf '  провалов у дефекта «удаление глотает смешанную»: %s (норма ≥1 — законный близнец)\n' "$del_wide_broken"

rc=0
[ "$real_fails" = "0" ] || { echo "ОТКАЗ: настоящий хук не проходит собственных утверждений" >&2; rc=1; }
if [ "$broken_fails" = "н/д" ]; then
    echo "ОТКАЗ: производителя базы нет ($RANGE) — дефект воссоздать нечем, проба свою способность упасть не доказала" >&2
    rc=1
elif [ "${broken_fails:-0}" -lt 1 ]; then
    echo "ОТКАЗ: проба ЗЕЛЁНАЯ на возвращённом дефекте — она не проверяет свой предмет" >&2
    rc=1
fi
[ "$lanes_real" = "0" ] || { echo "ОТКАЗ: настоящий хук не разводит полосы вердикта" >&2; rc=1; }
if [ "$lanes_broken" = "н/д" ]; then
    echo "ОТКАЗ: дефект полос не воссоздан — форма ветки кода 3 в хуке изменилась" >&2
    rc=1
elif [ "${lanes_broken:-0}" -lt 1 ]; then
    echo "ОТКАЗ: проба ЗЕЛЁНАЯ на снятой ветке кода 3 — полосы вердикта не держатся ничем" >&2
    rc=1
fi
[ "$draft_real" = "0" ] || { echo "ОТКАЗ: настоящий хук решает о черновике не по входу git" >&2; rc=1; }
if [ "$draft_broken" = "н/д" ]; then
    echo "ОТКАЗ: дефект черновика не воссоздан — форма решения о пропуске в хуке изменилась" >&2
    rc=1
elif [ "${draft_broken:-0}" -lt 1 ]; then
    echo "ОТКАЗ: проба ЗЕЛЁНАЯ на решении по имени ветки копии — она не проверяет свой предмет" >&2
    rc=1
fi
[ "$subj_real" = "0" ] || { echo "ОТКАЗ: настоящий хук судит не отправляемую ревизию" >&2; rc=1; }
if [ "$reap_broken" = "н/д" ]; then
    echo "ОТКАЗ: дефект сборщика не воссоздан — форма вызова сборщика в хуке изменилась" >&2
    rc=1
elif [ "${reap_broken:-0}" -lt 1 ]; then
    echo "ОТКАЗ: проба ЗЕЛЁНАЯ на снятом сборщике — остатки жёсткого обрыва не собирает ничто" >&2
    rc=1
fi
if [ "$scope_broken" = "н/д" ]; then
    echo "ОТКАЗ: дефект области сборщика не воссоздан — форма обхода в хуке изменилась" >&2
    rc=1
elif [ "${scope_broken:-0}" -lt 1 ]; then
    echo "ОТКАЗ: проба ЗЕЛЁНАЯ на расширенном обходе — чужие рабочие копии не защищены ничем" >&2
    rc=1
fi
if [ "$subj_broken" = "н/д" ]; then
    echo "ОТКАЗ: дефект предмета не воссоздан — форма выбора судимого дерева в хуке изменилась" >&2
    rc=1
elif [ "${subj_broken:-0}" -lt 1 ]; then
    echo "ОТКАЗ: проба ЗЕЛЁНАЯ на прогоне в рабочей копии — она не проверяет свой предмет" >&2
    rc=1
fi
[ "$post_real" = "0" ] || { echo "ОТКАЗ: настоящий хук не печатает постусловие отправки" >&2; rc=1; }
if [ "$post_broken" = "н/д" ]; then
    echo "ОТКАЗ: дефект постусловия не воссоздан — форма вызова печати в хуке изменилась" >&2
    rc=1
elif [ "${post_broken:-0}" -lt 1 ]; then
    echo "ОТКАЗ: проба ЗЕЛЁНАЯ на снятой печати — постусловие отправки не держится ничем" >&2
    rc=1
fi
[ "$del_real" = "0" ] || { echo "ОТКАЗ: настоящий хук судит отправку одних удалений либо молчит о ней" >&2; rc=1; }
if [ "$del_runs_broken" = "н/д" ]; then
    echo "ОТКАЗ: дефект «удаление гонит прогон» не воссоздан — ветви одних удалений в хуке нет либо её форма изменилась" >&2
    rc=1
elif [ "${del_runs_broken:-0}" -lt 1 ]; then
    echo "ОТКАЗ: проба ЗЕЛЁНАЯ на хуке, гоняющем прогон на удалении — она не проверяет свой предмет" >&2
    rc=1
fi
if [ "$del_wide_broken" = "н/д" ]; then
    echo "ОТКАЗ: дефект «удаление глотает смешанную» не воссоздан — форма условия одних удалений изменилась" >&2
    rc=1
elif [ "${del_wide_broken:-0}" -lt 1 ]; then
    echo "ОТКАЗ: проба ЗЕЛЁНАЯ на пропуске по любой строке удаления — смешанная отправка не защищена ничем" >&2
    rc=1
fi
exit "$rc"
