#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Страж атрибуции для ОТПРАВКИ (kacho-workspace#861). Предикат —
# scripts/hooks/attribution-rule.sh; зовёт его scripts/hooks/pre-push ПЕРВЫМ —
# до обхода KACHO_SKIP_PREPUSH и до пропуска черновиков: обход снимает проверки
# дерева, а не правило, и черновик на удалённом так же публичен. Проба —
# scripts/hooks/attribution-inject.sh.
#
# Хук коммита мог не исполниться (--no-verify, cherry-pick, rebase, am,
# commit-tree, клон без провязки): здесь судятся уже ЗАПИСАННЫЕ коммиты, чем бы
# они ни были сделаны.
#
# Вход — строки git `<local_ref> <local_sha> <remote_ref> <remote_sha>` на stdin.
#   · снятие ссылки (нулевой local_sha) не судится: коммитов оно не везёт;
#   · судятся НОВЫЕ коммиты — недостижимые ни с одной удалённой ссылки и с
#     прежней вершины этой. Опубликованная история не судится: переписывать её
#     предмет не требует, предмет — чтобы новых таких коммитов не было;
#   · пустой вход (хук позван руками) — судятся неопубликованные коммиты HEAD, и
#     это печатается.
#
# ИСХОДЫ: 0 — нарушений нет либо судить нечего (одни снятия), сказано какое;
# 1 — нарушения, у каждого sha и строка; 2 — судить не смог (нет предиката,
# объект не разрешается, диапазон не читается, HEAD не родился).
set -uo pipefail

src="${BASH_SOURCE[0]}"
case "$src" in */*) here="${src%/*}" ;; *) here=. ;; esac
here="$(cd "$here" && pwd)"
if [ ! -f "$here/attribution-rule.sh" ]; then
    echo "prepush-attribution: нет $here/attribution-rule.sh — предиката нет, судить нечем" >&2
    exit 2
fi
# shellcheck source=scripts/hooks/attribution-rule.sh
. "$here/attribution-rule.sh"

zero="0000000000000000000000000000000000000000"
refs=0
saw=0
cannot=()
while read -r _lref lsha rref rsha || [ -n "${lsha:-}" ]; do
    [ -n "${lsha:-}" ] || continue
    saw=1
    case "$lsha" in *[!0]*) ;; *) continue ;; esac
    refs=$((refs + 1))
    if ! tip="$(git rev-parse --verify --quiet "$lsha^{commit}")"; then
        # Метка не на коммите (на дереве или блобе) коммитов не везёт.
        git cat-file -e "$lsha" 2> /dev/null || cannot+=("ссылка «$rref»: объект $lsha не разрешается")
        continue
    fi
    neg=(--not --remotes)
    if [ -n "${rsha:-}" ] && [ "$rsha" != "$zero" ] && git cat-file -e "$rsha^{commit}" 2> /dev/null; then
        neg+=("$rsha")
    fi
    attribution_judge_range "$tip" "${neg[@]}" || cannot+=("ссылка «$rref»: диапазон не читается git")
done

if [ "$saw" = 0 ]; then
    if tip="$(git rev-parse --verify --quiet 'HEAD^{commit}')"; then
        echo "   вход отправки пуст — судятся неопубликованные коммиты HEAD"
        refs=1
        attribution_judge_range "$tip" --not --remotes || cannot+=("HEAD: диапазон не читается git")
    else
        cannot+=("вход отправки пуст, а HEAD не родился")
    fi
fi

echo "== атрибуция (scripts/hooks/attribution-rule.sh): ссылок $refs, новых коммитов $ATTRIBUTION_SEEN"
if [ "${#ATTRIBUTION_FINDINGS[@]}" -gt 0 ]; then
    echo "   нарушений: ${#ATTRIBUTION_FINDINGS[@]}"
    printf '     %s\n' "${ATTRIBUTION_FINDINGS[@]}"
    echo "   как правильно: сообщение без этих строк (git commit --amend -m … у вершины; глубже — перезапись сообщений до отправки)"
    exit 1
fi
if [ "${#cannot[@]}" -gt 0 ]; then
    echo "   судить НЕ смог:"
    printf '     %s\n' "${cannot[@]}"
    exit 2
fi
if [ "$refs" -eq 0 ]; then
    echo "   отправка несёт одни снятия ссылок — коммитов в ней нет, судить нечего"
    exit 0
fi
echo "   нарушений нет"
exit 0
