#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# stacks.sh — единственный shell-читатель состава стендов (deploy/stacks.txt).
#
# Зачем отдельный файл, а не строка sed в каждом гейте: строку копировали, и
# копии разъезжались молча. Здесь она одна, и она ОТКАЗЫВАЕТ, когда прочитать
# нечего, — вместо того чтобы вернуть пусто и позволить вызывающему объявить
# «стеков не осталось».
#
# Имя НЕ оканчивается на `-test.sh` намеренно: цель `helm-manifest-test` перебирает
# `tests/helm/*-test.sh`, и библиотека, попавшая в этот перебор, запускалась бы
# как проверка.
#
# Использование:
#   stacks.sh --names                     → имена стеков, по одному на строку
#   stacks.sh --table                     → строки `<имя>:<профиль>,…`
#   stacks.sh --chain <имя> [РАЗДЕЛИТЕЛЬ] → профили стека (по умолчанию через пробел)
#   stacks.sh --args  <имя> [КАТАЛОГ]     → `-f КАТАЛОГ/профиль …` для helm
#
# Либо как библиотека:  . "$(dirname "$0")/stacks.sh"  → функции stacks_names /
# stacks_table / stacks_chain / stacks_args.
#
# Опции оболочки здесь НЕ выставляются: файл подключают гейты с разными `set`
# (часть с `-e`, часть без), и библиотека, переписывающая их режим, меняла бы
# поведение вызывающего вдали от места правки.

_stacks_table_file() {
  # Путь считается от ЭТОГО файла, а не от рабочего каталога вызывающего:
  # гейты запускаются и из deploy/, и из tests/helm/, и из корня.
  local self
  self="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  printf '%s\n' "$self/../../stacks.txt"
}

_stacks_die() { echo "FATAL: stacks.sh — $1" >&2; exit 2; }

# stacks_table — строки `<имя>:<профиль>,…` без комментариев и пустых.
# Пустой результат — ОТКАЗ: «стеков не осталось» и «таблица не прочиталась»
# обязаны быть различимы, иначе вызывающий объявит дерево осмотренным, не
# прочитав ни строки.
stacks_table() {
  local f n out
  f="$(_stacks_table_file)"
  [ -r "$f" ] || _stacks_die "таблица стеков $f не читается — состав стендов взять неоткуда"
  out="$(grep -vE '^[[:space:]]*(#|$)' "$f")"
  n="$(printf '%s\n' "$out" | grep -c . || true)"
  [ "${n:-0}" -ge 1 ] || _stacks_die "в $f нет ни одной строки стека — обходить нечего"
  # Разбор обязан узнавать КАЖДУЮ строку: нераспознанная строка это не «стеков
  # меньше», это «предикат перестал их узнавать».
  local bad
  bad="$(printf '%s\n' "$out" | grep -vE '^[a-z0-9][a-z0-9-]*:values[^,[:space:]]*(,values[^,[:space:]]*)*$' || true)"
  [ -z "$bad" ] || _stacks_die "строка таблицы стеков не разобрана: $bad"
  printf '%s\n' "$out"
}

stacks_names() { stacks_table | cut -d: -f1; }

# stacks_chain <имя> [РАЗДЕЛИТЕЛЬ]
stacks_chain() {
  local want="$1" sep="${2:- }" line
  line="$(stacks_table | grep -E "^${want}:" || true)"
  [ -n "$line" ] || _stacks_die "стека '$want' в таблице нет — имя переехало или стек снят"
  printf '%s\n' "${line#*:}" | tr ',' "$sep"
}

# stacks_args <имя> [КАТАЛОГ]
stacks_args() {
  local want="$1" dir="${2:-}" f out=""
  for f in $(stacks_chain "$want" ' '); do
    if [ -n "$dir" ]; then out="$out -f $dir/$f"; else out="$out -f $f"; fi
  done
  printf '%s\n' "${out# }"
}

# Как исполняемый файл — разбор аргументов; как библиотека (source) — только
# функции выше.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  case "${1:---table}" in
    --names) stacks_names ;;
    --table) stacks_table ;;
    --chain) shift; stacks_chain "$@" ;;
    --args)  shift; stacks_args "$@" ;;
    *) _stacks_die "неизвестный аргумент '$1' (--names|--table|--chain|--args)" ;;
  esac
fi
