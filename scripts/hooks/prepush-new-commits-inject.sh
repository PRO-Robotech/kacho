#!/usr/bin/env bash
# shellcheck disable=SC2016  # выражения в кавычках — ТЕЛО дефектной копии, раскрывать их нельзя
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Инъекция для «новых коммитов нет — наборы не нужны» (kacho-workspace#1288) — на
# ИСХОДЕ НАСТОЯЩЕГО pre-push с заглушкой прогонщика: утверждается, дошёл ли хук до
# прогонщика и что напечатал, а не ответ предиката самого по себе.
#
# ОБЕ СТОРОНЫ. Ветка на опубликованной sha прогонщика не зовёт; всякий новый
# коммит — зовёт: обычный, пустой, существующий лишь локально, слияние с деревом,
# отличным от обоих родителей (догон). Слияние с деревом родителя новым не считается. Односторонняя проба зеленела бы
# на хуке, который не гонит наборы НИКОГДА, — это тот же дефект с другой стороны
# и дороже: непроверенное уезжало бы в публичную историю.
#
# Смешанная отправка двух РАЗНЫХ вершин здесь не пробуется: хук отказывает ей и без
# предиката («отправка несёт 2 разных вершин»); перепись по ссылкам держит check-27
# воркспейса, где хук гонит наборы по каждой ссылке.
#
# Дефекты воссоздаются правкой КОПИИ настоящего предиката либо хука; проба
# обязана покраснеть на каждом (≥1 провал) и молчать на настоящем (0).
set -uo pipefail

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
      GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX
unset KACHO_PUSH_RANGE KACHO_TRUNK_REF KACHO_PREPUSH_GROUP KACHO_SKIP_PREPUSH

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK="$HERE/pre-push"
PRED="$HERE/prepush-new-commits.sh"
for f in "$HOOK" "$PRED" "$HERE/prepush-groups.sh" "$HERE/prepush-range.sh" \
         "$HERE/attribution-rule.sh" "$HERE/prepush-attribution.sh" "$HERE/prepush-added-files.sh"; do
    [ -f "$f" ] || { echo "нет $f — хук без него не судим" >&2; exit 2; }
done

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
zero="0000000000000000000000000000000000000000"
MARK="stub-ci-local-исполнен"
SKIP="новых коммитов 0 — наборы не нужны"

mkdir -p "$tmp/bin"
printf '#!/bin/sh\nexit 1\n' > "$tmp/bin/gh"; chmod +x "$tmp/bin/gh"

repo="$tmp/repo"; mkdir -p "$repo/services" "$repo/scripts/hooks"
g() { git -C "$repo" -c user.email=p@i -c user.name=p -c commit.gpgsign=false "$@"; }
g init -q -b main
printf 'scripts/\n' > "$repo/.gitignore"
echo core > "$repo/services/a.go"
g add -A; g commit -qm "основание"
m0="$(g rev-parse HEAD)"
g update-ref refs/remotes/origin/main "$m0"
cat > "$repo/scripts/ci-local.sh" <<STUB
#!/usr/bin/env bash
printf '\n== %s\n' "$MARK"
printf '\n== итог: проверок исполнено 1, отказов 0, НЕ выполнено 0\n'
exit 0
STUB
chmod +x "$repo/scripts/ci-local.sh"

# Опубликованные головы для догона: две ветки с разными файлами.
g checkout -q -b pub-a "$m0"; echo a > "$repo/services/pa.go"; g add -A; g commit -qm "а"
pa="$(g rev-parse HEAD)"; g update-ref refs/remotes/origin/pub-a "$pa"
g checkout -q -b pub-b "$m0"; echo b > "$repo/services/pb.go"; g add -A; g commit -qm "б"
pb="$(g rev-parse HEAD)"; g update-ref refs/remotes/origin/pub-b "$pb"
# Слияние, дерево которого равно дереву родителя: вторая голова — потомок первой.
g checkout -q -b pub-ahead "$m0"; echo ah > "$repo/services/ah.go"; g add -A; g commit -qm "впереди"
g update-ref refs/remotes/origin/pub-ahead "$(g rev-parse HEAD)"
g checkout -q -b b-ff "$m0"; g merge -q --no-ff -m "слияние опубликованного" pub-ahead > /dev/null 2>&1
g checkout -q -b b-catch "$pa"; g merge -q --no-ff -m "догон" pub-b > /dev/null 2>&1
g checkout -q -b b-same "$m0"
g checkout -q -b b-same2 "$m0"
g checkout -q -b b-new "$m0"; echo n > "$repo/services/n.go"; g add -A; g commit -qm "новый"
g checkout -q -b b-empty "$m0"; g commit -q --allow-empty -m "пустой"

install_hooks() { # $1 — предикат (путь либо «-»), $2 — хук
    cp "$2" "$repo/scripts/hooks/pre-push"
    cp "$HERE/prepush-groups.sh" "$HERE/prepush-range.sh" "$HERE/attribution-rule.sh" \
       "$HERE/prepush-attribution.sh" "$HERE/prepush-added-files.sh" "$repo/scripts/hooks/"
    rm -f "$repo/scripts/hooks/prepush-new-commits.sh"
    [ "$1" = "-" ] || cp "$1" "$repo/scripts/hooks/prepush-new-commits.sh"
    chmod +x "$repo/scripts/hooks"/*.sh "$repo/scripts/hooks/pre-push" 2> /dev/null || true
}
ref() { printf 'refs/heads/%s %s refs/heads/%s %s' "$1" "$(g rev-parse "$1")" "$1" "$zero"; }
hook_run() { # вход git (строки) → «rc<код>» и вывод хука
    local out rc br
    # Рабочая копия стоит на первой отправляемой ветке — как при настоящей отправке.
    br="$(printf '%s' "$1" | head -1 | cut -d' ' -f1)"; g checkout -q "${br#refs/heads/}" 2> /dev/null || true
    out="$(cd "$repo" && printf '%s\n' "$1" | env PATH="$tmp/bin:$PATH" bash scripts/hooks/pre-push origin "$repo" 2>&1)"
    rc=$?
    printf 'rc%s\n%s\n' "$rc" "$out"
}

assert_all() { # $1 — метка, $2 — файл для числа провалов
    local tag="$1" out="$2" f=0 r
    ok()   { printf '  ok   [%s] %s\n' "$tag" "$1"; }
    bad()  { printf '  FAIL [%s] %s\n' "$tag" "$1"; f=$((f + 1)); [ "$tag" != real ] || tail -n 12 <<< "$r" | sed 's/^/        | /'; }
    skip_case() { # <описание> <вход>
        r="$(hook_run "$2")"
        if [ "$(head -1 <<< "$r")" = rc0 ] && grep -qF "$SKIP" <<< "$r" && ! grep -qF "$MARK" <<< "$r"; then ok "$1"; else bad "$1: ждали rc0, «$SKIP» и без прогонщика ($(head -1 <<< "$r"))"; fi
    }
    run_case() { # <описание> <вход>
        r="$(hook_run "$2")"
        if [ "$(head -1 <<< "$r")" = rc0 ] && grep -qF "$MARK" <<< "$r" && ! grep -qF "$SKIP" <<< "$r"; then ok "$1"; else bad "$1: ждали rc0 и вызов прогонщика ($(head -1 <<< "$r"))"; fi
    }
    skip_case "ветка на опубликованной sha — наборы не нужны" "$(ref b-same)"
    skip_case "две ветки на опубликованной sha" "$(ref b-same)
$(ref b-same2)"
    skip_case "слияние опубликованных голов с деревом родителя — не новое" "$(ref b-ff)"
    run_case "ветка с одним новым коммитом" "$(ref b-new)"
    run_case "пустой коммит — новый" "$(ref b-empty)"
    run_case "догон: слияние опубликованных голов с деревом, отличным от обоих родителей" "$(ref b-catch)"
    r="$(hook_run "refs/heads/b-gone $zero refs/heads/b-gone $pa")"
    if [ "$(head -1 <<< "$r")" = rc0 ] && ! grep -qF "$SKIP" <<< "$r"; then ok "одни удаления — своя строка, не «новых 0»"; else bad "одни удаления: названы «новых 0» либо код не 0"; fi
    printf '%s' "$f" > "$out"
}

mut() { # <имя> <sed-выражение> <файл-источник> — копия с дефектом; 1, если не воссоздан
    sed "$2" "$3" > "$tmp/$1"
    ! cmp -s "$3" "$tmp/$1"
}

printf '── прогон против настоящего хука и предиката (ждём 0 провалов)\n'
install_hooks "$PRED" "$HOOK"; assert_all real "$tmp/real.n"
real_f="$(cat "$tmp/real.n")"

declare -A mutant_f
run_mutant() { # <метка> <предикат> <хук>
    printf '── прогон против дефекта «%s» (ждём хотя бы один провал)\n' "$1"
    install_hooks "$2" "$3"; assert_all "$1" "$tmp/m.n"; mutant_f["$1"]="$(cat "$tmp/m.n")"
}
broken=0
build() { mut "$@" || { echo "ОТКАЗ: дефект «$1» не воссоздан — форма кода изменилась" >&2; broken=1; return 1; }; }
build p-allzero 's/^\[ "\$total" -eq 0 \]$/exit 0/' "$PRED" && run_mutant "новых нет всегда" "$tmp/p-allzero" "$HOOK"
build p-allnew 's/^\[ "\$total" -eq 0 \]$/exit 1/' "$PRED" && run_mutant "новые есть всегда" "$tmp/p-allnew" "$HOOK"
build p-nosub 's/^    sel=("--remotes=\$remote")$/    sel=(--remotes=нет-такого)/; s/^    sel=(--remotes)$/    sel=(--remotes=нет-такого)/' "$PRED" && run_mutant "опубликованное не вычитается" "$tmp/p-nosub" "$HOOK"
build p-mergenew 's/if \[ "\${#f\[@\]}" -le 2 \]; then/if [ "${#f[@]}" -le 99 ]; then/' "$PRED" && run_mutant "слияние всегда новое" "$tmp/p-mergenew" "$HOOK"
build p-mergeold 's/\[ "\$same" -eq 1 \] || new=\$((new + 1))/:/' "$PRED" && run_mutant "слияние никогда не новое" "$tmp/p-mergeold" "$HOOK"
build h-ignore 's/if \[ "\$nc_rc" -eq 0 \]; then/if true; then/' "$HOOK" && run_mutant "хук не читает код предиката" "$PRED" "$tmp/h-ignore"

printf '\nprepush-new-commits-inject: утверждений на прогон — 7 · дефектов — 6\n'
printf '  провалов у настоящего:            %s (норма 0)\n' "$real_f"
rc=0
[ "$real_f" = 0 ] || rc=1
for k in "${!mutant_f[@]}"; do
    printf '  провалов у дефекта «%s»: %s (норма ≥1)\n' "$k" "${mutant_f[$k]}"
    [ "${mutant_f[$k]}" -ge 1 ] 2> /dev/null || rc=1
done
[ "${#mutant_f[@]}" -eq 6 ] || { echo "ОТКАЗ: исполнено дефектов ${#mutant_f[@]} из 6" >&2; rc=1; }
[ "$broken" = 0 ] || rc=1
exit "$rc"
