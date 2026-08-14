#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# install.sh — провязать хуки этого дерева в клон и сказать, провязаны ли они.
# Зовётся целями `make install-hooks` / `make check-hooks`; руками — тоже можно.
#
# ПОЧЕМУ НЕ `core.hooksPath`. Одна строка настройки выглядит проще всего и имеет
# цену, которую видно не сразу: git начинает искать хуки ТОЛЬКО по указанному
# пути, и всё, что уже лежит в `.git/hooks`, перестаёт исполняться — молча, без
# единого сообщения. В этом клоне там лежит страж личности автора коммита, и его
# исчезновение было бы ровно таким же незаметным, как отсутствие хука отправки,
# которое чинится этим скриптом. Лекарство обязано быть проверено на то, что оно
# сохраняет.
#
# Поэтому в `.git/hooks` кладётся ПЕРЕХОДНИК: короткий файл, который находит
# рабочую копию и исполняет отслеживаемый скрипт из неё. Следствия названы, чтобы
# их не пришлось выяснять:
#
#   · посторонние хуки продолжают работать — переходник ставится только под
#     своим именем, чужой файл не затирается НИКОГДА (это отказ, а не перезапись);
#   · переходник берёт скрипт из ТОЙ рабочей копии, из которой идёт отправка,
#     поэтому в worktree исполняется его собственная версия хука;
#   · путей машины в переходнике нет — переезд клона его не ломает (настроенный
#     абсолютный путь, переживший переезд, — наблюдавшийся отдельный класс:
#     настройка указывает в никуда и при этом выглядит настроенной);
#   · цена: новый отслеживаемый хук требует повторного `make install-hooks`.
#     Цена наблюдаемая, а не молчаливая, — её называет `make check-hooks`.
#
# ИМЕНА ХУКОВ ТОЧКИ НЕ СОДЕРЖАТ. Отсюда правило отбора: в `scripts/hooks` хуком
# считается отслеживаемый файл, в имени которого нет точки. Этот скрипт под
# правило не подпадает и хуком не станет.
set -uo pipefail

mode="${1:-install}"

die() { printf '%s\n' "$@" >&2; exit 1; }

root="$(git rev-parse --show-toplevel 2>/dev/null)" ||
    die "install-hooks: это не рабочая копия git — провязывать не во что."
common="$(git rev-parse --git-common-dir 2>/dev/null)" ||
    die "install-hooks: git не назвал общий каталог репозитория."
case "$common" in /*) ;; *) common="$root/$common" ;; esac

src="$root/scripts/hooks"
dst="$common/hooks"

# Отслеживаемые хуки. `git ls-files` — то же множество, что увидит свежий клон:
# файл, лежащий на диске и не добавленный в индекс, провязывать нечестно.
hooks=()
while IFS= read -r rel; do
    base="${rel##*/}"
    case "$base" in *.*) continue ;; esac
    hooks+=("$base")
done < <(git -C "$root" ls-files scripts/hooks)

[ "${#hooks[@]}" -gt 0 ] ||
    die "install-hooks: в scripts/hooks нет НИ ОДНОГО отслеживаемого хука." \
        "Это отказ, а не «нечего делать»: пустой обход здесь означал бы зелёный" \
        "вывод при непровязанном клоне."

marker="kacho-hook-stub v1"

# ── 1. Куда git на самом деле смотрит ────────────────────────────────────────
#
# Выставленный `core.hooksPath` перебивает `.git/hooks` целиком. Молча
# продолжать нельзя: переходники легли бы туда, куда никто не заглядывает.
configured="$(git config --get core.hooksPath 2>/dev/null || true)"
if [ -n "$configured" ]; then
    abs="$configured"
    case "$abs" in /*) ;; *) abs="$root/$abs" ;; esac
    if [ ! -d "$abs" ]; then
        die "ОТКАЗ: core.hooksPath = «$configured» — каталога по этому пути НЕТ." \
            "" \
            "Настройка, указывающая в никуда, выглядит настроенной: git не находит" \
            "ни одного хука и не говорит об этом ни слова. Чаще всего так остаётся" \
            "абсолютный путь, переживший переезд рабочей копии." \
            "" \
            "  git config --unset core.hooksPath   # затем: make install-hooks"
    fi
    real_cfg="$(cd "$abs" && pwd -P)"
    if [ "$real_cfg" != "$(cd "$src" && pwd -P)" ]; then
        die "ОТКАЗ: core.hooksPath = «$configured» ведёт в $real_cfg." \
            "" \
            "git будет искать хуки ТАМ, поэтому провязка в $dst не исполнится ни разу." \
            "Разберитесь с настройкой, прежде чем провязывать:" \
            "" \
            "  git config --unset core.hooksPath   # затем: make install-hooks"
    fi
    # Путь ведёт в наш же каталог: хуки исполняются напрямую. Работает — но
    # платит тем, что названо в шапке, и об этом надо сказать вслух.
    echo "core.hooksPath = «$configured» — хуки исполняются НАПРЯМУЮ из $src."
    blinded=()
    for f in "$dst"/*; do
        [ -e "$f" ] || continue
        b="${f##*/}"
        case "$b" in *.sample) continue ;; esac
        blinded+=("$b")
    done
    if [ "${#blinded[@]}" -gt 0 ]; then
        echo "ВНИМАНИЕ: из-за этой настройки НЕ исполняется ничего в $dst — а там лежит:" >&2
        printf '  %s\n' "${blinded[@]}" >&2
        echo "Если хоть один из них нужен, снимите настройку и провяжите переходниками:" >&2
        echo "  git config --unset core.hooksPath && make install-hooks" >&2
    fi
    echo "отслеживаемых хуков: ${#hooks[@]}; исполняются напрямую"
    exit 0
fi

# ── 2. Состояние переходников ───────────────────────────────────────────────
mkdir -p "$dst" || die "install-hooks: не создать $dst"

stub_for() {
    cat <<STUB
#!/usr/bin/env bash
# СГЕНЕРИРОВАН \`make install-hooks\` — правится НЕ здесь, а в scripts/hooks/$1.
# $marker
set -uo pipefail
top="\$(git rev-parse --show-toplevel 2>/dev/null)" || top=""
[ -n "\$top" ] || top="\$PWD"
real="\$top/scripts/hooks/$1"
if [ ! -x "\$real" ]; then
    echo "$1: в этой рабочей копии нет \$real — проверок НЕ БЫЛО" >&2
    exit 0
fi
exec "\$real" "\$@"
STUB
}

wired=0
missing=()
foreign=()
for name in "${hooks[@]}"; do
    target="$dst/$name"
    if [ ! -e "$target" ]; then
        missing+=("$name")
        continue
    fi
    if grep -qF "$marker" "$target" 2>/dev/null; then
        if [ -x "$target" ]; then
            wired=$((wired + 1))
        else
            missing+=("$name")
        fi
        continue
    fi
    foreign+=("$name")
done

# Посторонние хуки, которых мы не предоставляем, — их провязка не касается, и
# сказать это надо прямо: именно ради их сохранности выбран переходник.
kept=()
for f in "$dst"/*; do
    [ -e "$f" ] || continue
    b="${f##*/}"
    case "$b" in *.sample) continue ;; esac
    ours=0
    for name in "${hooks[@]}"; do [ "$b" = "$name" ] && ours=1; done
    [ "$ours" = 1 ] || kept+=("$b")
done

report() {
    echo "хуки: провязано $wired из ${#hooks[@]} ($dst)"
    [ "${#kept[@]}" -eq 0 ] ||
        printf 'посторонних хуков оставлено нетронутыми: %s\n' "${kept[*]}"
}

case "$mode" in
check)
    report
    if [ "${#foreign[@]}" -gt 0 ]; then
        echo "ОТКАЗ: под именем хука лежит ЧУЖОЙ файл: ${foreign[*]}" >&2
        echo "Он не перезаписывается — уберите его сами либо слейте с scripts/hooks/<имя>." >&2
        exit 1
    fi
    if [ "${#missing[@]}" -gt 0 ]; then
        echo "ОТКАЗ: не провязаны: ${missing[*]}" >&2
        echo "Отправка ветки НЕ проверяется локально: конвейер станет первым читателем." >&2
        echo "  make install-hooks" >&2
        exit 1
    fi
    exit 0
    ;;
notice)
    # Тихий режим для чужих целей: он ничего не роняет и говорит ровно тогда,
    # когда сказать есть о чём. Молчание означает «провязано» — положительное
    # утверждение печатает `make check-hooks`, а не каждый прогон тестов.
    if [ "${#missing[@]}" -gt 0 ] || [ "${#foreign[@]}" -gt 0 ]; then
        echo "ВНИМАНИЕ: хуки git не провязаны (${#missing[@]} не провязано, ${#foreign[@]} занято чужим)." >&2
        echo "  Отправка ветки НЕ будет проверена локально — «make install-hooks» это чинит." >&2
    fi
    exit 0
    ;;
install) ;;
*) die "install-hooks: неизвестный режим «$mode» (install | check | notice)" ;;
esac

# ── 3. Провязка ─────────────────────────────────────────────────────────────
if [ "${#foreign[@]}" -gt 0 ]; then
    die "ОТКАЗ: под именем хука уже лежит ЧУЖОЙ файл: ${foreign[*]}" \
        "" \
        "Он НЕ перезаписывается: чужой хук мог быть заведён осознанно, и его тихая" \
        "пропажа — тот самый класс, ради которого здесь не выставляется core.hooksPath." \
        "Уберите файл сами либо слейте его с scripts/hooks/<имя>, затем повторите."
fi

installed=()
for name in "${hooks[@]}"; do
    target="$dst/$name"
    stub_for "$name" > "$target" || die "install-hooks: не записать $target"
    chmod +x "$target" || die "install-hooks: не сделать исполняемым $target"
    installed+=("$name")
done

wired=${#installed[@]}
report
printf 'провязаны переходниками: %s\n' "${installed[*]}"
echo "проверить в любой момент: make check-hooks"
