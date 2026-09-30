#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# ПРАВИЛО АТРИБУЦИИ — единственный источник предиката (kacho-workspace#861).
# Сам не исполняется: его читают два потребителя, своей копии нет ни у одного:
#
#   scripts/hooks/commit-msg              сообщение в МОМЕНТ коммита;
#   scripts/hooks/prepush-attribution.sh  ЗАПИСАННЫЕ коммиты отправки (pre-push зовёт его первым).
#
# Правило (`git-issues.md` воркспейса, gi-no-attribution-trailers): трейлеров
# атрибуции в сообщении коммита нет. Атрибуция — строка, НАЧАТАЯ ключом
# `Co-Authored-By:` либо `Claude-Session:` (регистр не различается, отступ
# допустим), строка «Generated with [Claude Code]» и ссылка claude.ai/code.
#
# ЗАПРЕЩЁН КЛЮЧ, А НЕ ЗНАЧЕНИЕ: `Co-Authored-By` соавтора-человека — тоже отказ.
# Предикат один на четыре дерева (corelib, kaname, kacho, воркспейс); экземпляр
# у каждого свой (ban20): форма взята у стража corelib, байты не перенесены.
#
# Трейлер — строка, начатая ключом: тот же ключ в СЕРЕДИНЕ строки прозы —
# упоминание (так пишут, снимая шаблон), а не трейлер. Строка «Generated with
# Claude Code» и ссылка судятся в любом месте строки: у них ключа нет.
#
# Первая строка, имя ветки и подпись здесь НЕ судятся — это предмет
# PRO-Robotech/kacho#2793; сводя стражи, он берёт этот предикат, а не заводит
# второй.

# attribution_line <текст> — печатает первую строку атрибуции; 1 — её нет.
attribution_line() {
    local line found=1 restore
    restore="$(shopt -p nocasematch)"
    shopt -s nocasematch
    while IFS= read -r line; do
        if [[ "$line" =~ ^[[:space:]]*co-authored-by: ]] ||
            [[ "$line" =~ ^[[:space:]]*claude-session: ]] ||
            [[ "$line" =~ generated[[:space:]]+with[[:space:]]+\[?claude[[:space:]]+code ]] ||
            [[ "$line" =~ claude\.ai/code ]]; then
            printf '%s' "$line"
            found=0
            break
        fi
    done <<< "$1"
    eval "$restore"
    return "$found"
}

# attribution_message <файл> — сообщение так, как его запишет git: до линии
# ножниц. У `git commit -v` ниже неё лежит дифф (git его вырезает), и строка
# контекста ` Co-Authored-By: …` из тронутого файла трейлером сообщения не стала бы.
attribution_message() {
    local line
    while IFS= read -r line || [ -n "$line" ]; do
        [[ "$line" =~ ^[^[:space:]]+\ -{24}\ \>8\ -{24}$ ]] && break
        printf '%s\n' "$line"
    done < "$1"
}

# attribution_judge_range <аргументы rev-list…> — судит каждый ЗАПИСАННЫЙ коммит
# диапазона. Находки — в ATTRIBUTION_FINDINGS («<sha10> атрибуция в сообщении:
# «<строка>»»), счёт осмотренного — ATTRIBUTION_SEEN. Код 1 — диапазон не
# читается git: судить нечем, и это не «нарушений нет».
ATTRIBUTION_FINDINGS=()
ATTRIBUTION_SEEN=0
attribution_judge_range() {
    local log rec h attr
    log="$(git -c log.showSignature=false log --no-color --encoding=UTF-8 \
        --format='%H%x1f%B%x1e' "$@" 2> /dev/null)" || return 1
    # Запись завершается \x1e, git дописывает перевод строки после каждой.
    while IFS= read -r -d $'\x1e' rec; do
        rec="${rec#$'\n'}"
        [ -n "$rec" ] || continue
        h="${rec%%$'\x1f'*}"
        ATTRIBUTION_SEEN=$((ATTRIBUTION_SEEN + 1))
        if attr="$(attribution_line "${rec#*$'\x1f'}")"; then
            ATTRIBUTION_FINDINGS+=("${h:0:10} атрибуция в сообщении: «$attr»")
        fi
    done <<< "$log"
    return 0
}
