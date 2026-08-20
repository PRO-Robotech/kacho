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
cat > "$repo/scripts/ci-local.sh" <<'STUB'
#!/usr/bin/env bash
set -uo pipefail
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
printf '\n== итог: проверок исполнено %d, отказов %d, НЕ выполнено %d\n' "$ran" 0 "$skipped"
exit 0
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

install_hooks() { # $1 — производитель базы (путь) либо «-» (его нет)
    cp "$HOOK" "$repo/scripts/hooks/pre-push"
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
printf 'prepush-range-inject: утверждений на прогон 6\n'
printf '  провалов у настоящего: %s (норма 0)\n' "$real_fails"
printf '  провалов у дефекта базы: %s (норма ≥1 — иначе проба ничего не проверяет)\n' "$broken_fails"

rc=0
[ "$real_fails" = "0" ] || { echo "ОТКАЗ: настоящий хук не проходит собственных утверждений" >&2; rc=1; }
if [ "$broken_fails" = "н/д" ]; then
    echo "ОТКАЗ: производителя базы нет ($RANGE) — дефект воссоздать нечем, проба свою способность упасть не доказала" >&2
    rc=1
elif [ "${broken_fails:-0}" -lt 1 ]; then
    echo "ОТКАЗ: проба ЗЕЛЁНАЯ на возвращённом дефекте — она не проверяет свой предмет" >&2
    rc=1
fi
exit "$rc"
