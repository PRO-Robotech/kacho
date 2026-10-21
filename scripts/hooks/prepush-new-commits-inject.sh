#!/usr/bin/env bash
# shellcheck disable=SC2016  # выражения в кавычках — ТЕЛО дефектной копии, раскрывать их нельзя
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Инъекция для «непроверенных вершин нет — наборы не нужны» (kacho-workspace#1288,
# #3336) — на ИСХОДЕ НАСТОЯЩЕГО pre-push с заглушкой прогонщика: утверждается,
# дошёл ли хук до прогонщика и что напечатал, а не ответ предиката самого по себе.
#
# «Проверено» в песочнице ставит только сам хук: прогоном до заглушки с зелёным
# исходом. Опубликованное (`refs/remotes/*`) проверенным не считается — опыт
# check-verifier ws#1288: черновик, обход, форк, снятая на сервере ссылка, тег и
# перевод ствола публикуют непроверенное.
#
# ОБЕ СТОРОНЫ. Наборы НЕ идут: T1b новое имя на проверенной вершине; две ссылки на
# ней; слияние с деревом проверенного родителя; отправка проверенной адресом.
# Наборы идут: не судившийся коммит; пустой поверх проверенной; предок проверенной
# (T-mid); догон; слияние с деревом НЕпроверенного родителя; красная вершина под
# новым именем; дописывание поверх отправленного в обход (диапазон не покрыл
# обойдённое); D1b черновик, затем обычное имя; D1env-b обход, затем новое имя;
# D2b коммит лишь в форке, отправка адресом, D2a — по имени; D3a снятый на сервере
# черновик с оставшимся отслеживанием, D3b — без него; D4a тег на черновом
# коммите, D4b — на локальном; D5 ствол на черновой коммит.
#
# Смешанная отправка двух РАЗНЫХ вершин здесь не пробуется: хук отказывает ей и без
# предиката («отправка несёт 2 разных вершин»); перепись по ссылкам держит check-27
# воркспейса, где хук гонит наборы по каждой ссылке.
#
# Дефекты воссоздаются правкой КОПИИ настоящего предиката либо хука; проба
# обязана покраснеть на каждом (≥1 провал) и молчать на настоящем (0). Сверка
# копий предиката kacho и воркспейса — check-28 воркспейса.
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
SKIP="наборы не нужны"

mkdir -p "$tmp/bin"
printf '#!/bin/sh\nexit 1\n' > "$tmp/bin/gh"; chmod +x "$tmp/bin/gh"

repo="$tmp/repo"
g() { git -C "$repo" -c user.email=p@i -c user.name=p -c commit.gpgsign=false "$@"; }
n_commit=0
newc() { # <ветка> [<от>] — ветка с одним новым коммитом
    g checkout -q -f -B "$1" "${2:-$m0}"; n_commit=$((n_commit + 1))
    echo "$n_commit" > "$repo/services/c$n_commit.go"; g add -A; g commit -qm "$1"
}
pub() { g update-ref "refs/remotes/${2:-origin}/${3:-$1}" "$(g rev-parse "$1")"; } # опубликовано без хука

seed() { # песочница заново: каждая сторона (настоящий, дефект) судится на свежей записи
    rm -rf "$repo"; mkdir -p "$repo/services" "$repo/scripts/hooks"
    g init -q -b main
    printf 'scripts/\n' > "$repo/.gitignore"
    echo core > "$repo/services/a.go"
    g add -A; g commit -qm "основание"
    m0="$(g rev-parse HEAD)"
    g update-ref refs/remotes/origin/main "$m0"
    cat > "$repo/scripts/ci-local.sh" << STUB
#!/usr/bin/env bash
printf '\n== %s\n' "$MARK"
[ ! -f "\$(dirname "\$0")/../services/red.go" ] || { printf '\n== итог: проверок исполнено 1, отказов 1, НЕ выполнено 0\n'; exit 1; }
printf '\n== итог: проверок исполнено 1, отказов 0, НЕ выполнено 0\n'
exit 0
STUB
    chmod +x "$repo/scripts/ci-local.sh"
    n_commit=0
}

install_hooks() { # $1 — предикат (путь либо «-»), $2 — хук
    cp "$2" "$repo/scripts/hooks/pre-push"
    cp "$HERE/prepush-groups.sh" "$HERE/prepush-range.sh" "$HERE/attribution-rule.sh" \
       "$HERE/prepush-attribution.sh" "$HERE/prepush-added-files.sh" "$repo/scripts/hooks/"
    rm -f "$repo/scripts/hooks/prepush-new-commits.sh"
    [ "$1" = "-" ] || cp "$1" "$repo/scripts/hooks/prepush-new-commits.sh"
    chmod +x "$repo/scripts/hooks"/*.sh "$repo/scripts/hooks/pre-push" 2> /dev/null || true
}
ref() { printf 'refs/heads/%s %s refs/heads/%s %s' "$1" "$(g rev-parse "$1")" "${2:-$1}" "${3:-$zero}"; }
hook_run() { # <вход git> [<первый довод>] [<KACHO_SKIP_PREPUSH>] [<куда встать>] → «rc<код>» и вывод
    local out rc at
    # Рабочая копия стоит на отправляемой вершине — как при настоящей отправке.
    at="${4:-$(printf '%s' "$1" | head -1 | cut -d' ' -f2)}"; g checkout -q -f "$at" 2> /dev/null || true
    out="$(cd "$repo" && printf '%s\n' "$1" | env PATH="$tmp/bin:$PATH" KACHO_SKIP_PREPUSH="${3:-0}" \
        bash scripts/hooks/pre-push "${2:-origin}" "$repo" 2>&1)"
    rc=$?
    printf 'rc%s\n%s\n' "$rc" "$out"
}

assert_all() { # $1 — метка, $2 — файл для числа провалов
    local tag="$1" out="$2" f=0 r
    asserts=0
    ok()   { asserts=$((asserts + 1)); printf '  ok   [%s] %s\n' "$tag" "$1"; }
    bad()  { asserts=$((asserts + 1)); printf '  FAIL [%s] %s\n' "$tag" "$1"; f=$((f + 1)); [ "$tag" != real ] || tail -n 12 <<< "$r" | sed 's/^/        | /'; }
    skip_case() { # <описание> <вход> [<довод>]
        r="$(hook_run "$2" "${3:-origin}")"
        if [ "$(head -1 <<< "$r")" = rc0 ] && grep -qF "$SKIP" <<< "$r" && ! grep -qF "$MARK" <<< "$r"; then ok "$1"; else bad "$1: ждали rc0, «$SKIP» и без прогонщика ($(head -1 <<< "$r"))"; fi
    }
    run_case() { # <описание> <вход> [<довод>] [<код>]
        r="$(hook_run "$2" "${3:-origin}")"
        if [ "$(head -1 <<< "$r")" = "rc${4:-0}" ] && grep -qF "$MARK" <<< "$r" && ! grep -qF "$SKIP" <<< "$r"; then ok "$1"; else bad "$1: ждали rc${4:-0} и вызов прогонщика ($(head -1 <<< "$r"))"; fi
    }
    silent() { # <описание> <вход> [<довод>] [<KACHO_SKIP_PREPUSH>] — посев без наборов
        r="$(hook_run "$2" "${3:-origin}" "${4:-0}")"
        if [ "$(head -1 <<< "$r")" = rc0 ] && ! grep -qF "$MARK" <<< "$r"; then ok "посев: $1"; else bad "посев «$1» прошёл с наборами либо не 0 ($(head -1 <<< "$r"))"; fi
    }

    newc b-new
    run_case "не судившийся коммит — наборы идут" "$(ref b-new)"
    pub b-new
    g branch -f b-t1b b-new
    skip_case "T1b: новое имя на проверенной вершине" "$(ref b-t1b)"
    g branch -f b-t1c b-new
    skip_case "две ссылки на проверенной вершине" "$(ref b-t1b)
$(ref b-t1c)"
    skip_case "проверенная вершина, отправленная адресом" "$(ref b-t1b b-t1d)" "$repo"

    newc b-ahead; run_case "посев слияния: голова впереди" "$(ref b-ahead)"; pub b-ahead
    g checkout -q -f -B b-ff "$m0"; g merge -q --no-ff -m "слияние проверенного" b-ahead > /dev/null 2>&1
    skip_case "слияние с деревом проверенного родителя" "$(ref b-ff)"

    g checkout -q -f -B b-empty b-new; g commit -q --allow-empty -m "пустой"
    run_case "пустой коммит поверх проверенной вершины" "$(ref b-empty)"

    newc b-two; newc b-two2 b-two; g branch -f b-two b-two2
    run_case "посев T-mid: две вершины одной отправкой" "$(ref b-two)"
    g branch -f b-mid b-two2~1
    run_case "T-mid: предок проверенной вершины" "$(ref b-mid)"

    newc b-a; run_case "посев догона: голова а" "$(ref b-a)"; pub b-a
    newc b-b; run_case "посев догона: голова б" "$(ref b-b)"; pub b-b
    g checkout -q -f -B b-catch b-a; g merge -q --no-ff -m "догон" b-b > /dev/null 2>&1
    run_case "догон: дерево слияния отлично от обоих проверенных родителей" "$(ref b-catch)"

    newc b-pub; pub b-pub
    g checkout -q -f -B b-ffu "$m0"; g merge -q --no-ff -m "слияние непроверенного" b-pub > /dev/null 2>&1
    run_case "слияние с деревом НЕпроверенного родителя" "$(ref b-ffu)"

    g checkout -q -f -B b-red "$m0"; echo red > "$repo/services/red.go"; g add -A; g commit -qm "красная"
    run_case "посев: красная вершина" "$(ref b-red)" origin 1
    g branch -f b-red2 b-red
    run_case "красная вершина под новым именем — не записана проверенной" "$(ref b-red2)" origin 1

    newc b-byp
    silent "обход ветки" "$(ref b-byp)" origin 1
    pub b-byp
    g checkout -q -f -B b-byp2 b-byp; echo y > "$repo/services/byp2.go"; g add -A; g commit -qm "поверх обхода"
    run_case "посев: дописывание поверх отправленного в обход" "$(ref b-byp2 b-byp "$(g rev-parse b-byp)")"
    g branch -f b-byp3 b-byp2
    run_case "дописывание поверх обхода не записано: новое имя гонит наборы" "$(ref b-byp3)"

    newc b-d1b
    silent "D1b: черновик" "$(ref b-d1b wip/b-d1b)"
    pub b-d1b origin wip/b-d1b
    run_case "D1b: черновик wip/, затем то же под обычным именем" "$(ref b-d1b)"

    newc b-env
    silent "D1env-b: обход" "$(ref b-env)" origin 1
    pub b-env
    g branch -f b-env2 b-env
    run_case "D1env-b: отправка с KACHO_SKIP_PREPUSH=1, затем новое имя" "$(ref b-env2)"

    newc b-forka; pub b-forka fork
    newc b-forkb; pub b-forkb fork
    run_case "D2b: коммит лишь в форке, отправка адресом" "$(ref b-forka)" "$repo"
    run_case "D2a: коммит лишь в форке, отправка по имени" "$(ref b-forkb)"

    newc b-gone; pub b-gone origin wip/b-gone
    run_case "D3a: черновик снят на сервере, отслеживание осталось" "$(ref b-gone)"
    newc b-prune
    run_case "D3b: черновик снят, отслеживания нет" "$(ref b-prune)"

    newc b-tagd; pub b-tagd origin wip/b-tagd
    run_case "D4a: тег на черновом коммите" "refs/tags/t-d $(g rev-parse b-tagd) refs/tags/t-d $zero"
    newc b-tagl
    run_case "D4b: тег на локальном коммите" "refs/tags/t-l $(g rev-parse b-tagl) refs/tags/t-l $zero"

    newc b-d5; pub b-d5 origin wip/b-d5
    run_case "D5: ствол переводится на черновой коммит" "refs/heads/main $(g rev-parse b-d5) refs/heads/main $m0"

    r="$(hook_run "refs/heads/b-gone $zero refs/heads/b-gone $(g rev-parse b-a)" origin 0 b-a)"
    if [ "$(head -1 <<< "$r")" = rc0 ] && ! grep -qF "$SKIP" <<< "$r"; then ok "одни удаления — своя строка, не «$SKIP»"; else bad "одни удаления: названы «$SKIP» либо код не 0"; fi
    printf '%s' "$f" > "$out"
}

mut() { # <имя> <sed-выражение> <файл-источник> — копия с дефектом; 1, если не воссоздан
    sed "$2" "$3" > "$tmp/$1"
    ! cmp -s "$3" "$tmp/$1"
}

printf '── прогон против настоящего хука и предиката (ждём 0 провалов)\n'
seed; install_hooks "$PRED" "$HOOK"; assert_all real "$tmp/real.n"
real_f="$(cat "$tmp/real.n")"
real_asserts="$asserts"

declare -A mutant_f
run_mutant() { # <метка> <предикат> <хук>
    printf '── прогон против дефекта «%s» (ждём хотя бы один провал)\n' "$1"
    seed; install_hooks "$2" "$3"; assert_all "$1" "$tmp/m.n"; mutant_f["$1"]="$(cat "$tmp/m.n")"
}
broken=0
build() { mut "$@" || { echo "ОТКАЗ: дефект «$1» не воссоздан — форма кода изменилась" >&2; broken=1; return 1; }; }
V='^verified() { \[ -f .*$'
build p-allzero 's/^\[ "\$total" -eq 0 \]$/exit 0/' "$PRED" && run_mutant "непроверенных нет всегда" "$tmp/p-allzero" "$HOOK"
build p-allnew 's/^\[ "\$total" -eq 0 \]$/exit 1/' "$PRED" && run_mutant "непроверенные есть всегда" "$tmp/p-allnew" "$HOOK"
build p-noledger "s#$V#verified() { false; }#" "$PRED" && run_mutant "запись не читается" "$tmp/p-noledger" "$HOOK"
build p-m1 "s#$V#verified() { [ -n \"\$(git for-each-ref --count=1 --contains \"\$1\" refs/remotes)\" ] || { [ -f \"\$ledger\" ] \&\& grep -qxF -- \"\$1 \$ctxkey\" \"\$ledger\"; }; }#" "$PRED" &&
    run_mutant "M1: проверено всё опубликованное" "$tmp/p-m1" "$HOOK"
build p-m2 "s#$V#verified() { [ -n \"\$(git for-each-ref --count=1 --contains \"\$1\" refs/remotes/origin)\" ] || { [ -f \"\$ledger\" ] \&\& grep -qxF -- \"\$1 \$ctxkey\" \"\$ledger\"; }; }#" "$PRED" &&
    run_mutant "M2: проверено опубликованное в origin" "$tmp/p-m2" "$HOOK"
build p-anc "s#$V#verified() { local v k; [ -f \"\$ledger\" ] || return 1; while read -r v k; do [ \"\$k\" = \"\$ctxkey\" ] \&\& git merge-base --is-ancestor \"\$1\" \"\$v\" 2>/dev/null \&\& return 0; done < \"\$ledger\"; return 1; }#" "$PRED" &&
    run_mutant "предок записанной проверен" "$tmp/p-anc" "$HOOK"
build p-mergenew 's/"\${#f\[@\]}" -gt 2 \]/"${#f[@]}" -gt 99 ]/' "$PRED" && run_mutant "слияние с деревом родителя не узнаётся" "$tmp/p-mergenew" "$HOOK"
build p-empty 's/"\${#f\[@\]}" -gt 2 \]/"${#f[@]}" -gt 1 ]/' "$PRED" && run_mutant "пустой коммит поверх проверенной проверен" "$tmp/p-empty" "$HOOK"
build p-mergeunrec 's/if verified "\$p" && \[/if [/' "$PRED" && run_mutant "дерево непроверенного родителя засчитано" "$tmp/p-mergeunrec" "$HOOK"
build p-mergeany 's/if verified "\$p" && \[ "\$(git rev-parse "\$p^{tree}")" = "\$t" \]; then/if true; then/' "$PRED" && run_mutant "любое слияние проверено" "$tmp/p-mergeany" "$HOOK"
build h-ignore 's/if \[ "\$nc_rc" -eq 0 \]; then/if true; then/' "$HOOK" && run_mutant "хук не читает код предиката" "$PRED" "$tmp/h-ignore"
build h-norecord 's/^    record_verified$/    :/' "$HOOK" && run_mutant "хук не пишет запись" "$PRED" "$tmp/h-norecord"
build h-recordred 's/^if \[ "\$run_rc" -ne 0 \]; then$/if [ "$run_rc" -ne 0 ]; then record_verified;/' "$HOOK" && run_mutant "хук записывает красную" "$PRED" "$tmp/h-recordred"
build h-coverall 's/^    \[ -z "\${KACHO_TRUNK_REF:-}" \] || return 1$/    return 0/' "$HOOK" && run_mutant "любой диапазон покрывает вершину" "$PRED" "$tmp/h-coverall"

want=14
printf '\nprepush-new-commits-inject: утверждений на прогон — %s · дефектов — %s\n' "$real_asserts" "$want"
printf '  провалов у настоящего:            %s (норма 0)\n' "$real_f"
rc=0
[ "$real_f" = 0 ] || rc=1
for k in "${!mutant_f[@]}"; do
    printf '  провалов у дефекта «%s»: %s (норма ≥1)\n' "$k" "${mutant_f[$k]}"
    [ "${mutant_f[$k]}" -ge 1 ] 2> /dev/null || rc=1
done
[ "${#mutant_f[@]}" -eq "$want" ] || { echo "ОТКАЗ: исполнено дефектов ${#mutant_f[@]} из $want" >&2; rc=1; }
[ "$broken" = 0 ] || rc=1
exit "$rc"
