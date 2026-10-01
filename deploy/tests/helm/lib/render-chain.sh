# shellcheck shell=bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# render-chain.sh — тестовая шелл-обёртка рендера цепочек: строка общего
# читателя таблицы стендов (`stacks_args`) и, ТОЛЬКО для цепочки `prod`, слой
# оператора из каталога образцов `deploy/testdata/mail-node/` последним `-f`
# (NTF-1, полоса D9; замысел З28, Д48, CX1-86 (б), CX1-89, CX1-92, N9, N12, N16).
#
# Для ГЕЙТОВ рендера, не для установки. Рецепты установки (`deploy/Makefile`
# `STACK_ARGS`, `cutover-fe3455.sh`, `stack-secrets.sh`) читают таблицу общим
# читателем `stacks.sh`: слой в нём попал бы в настоящую установку `prod`.
#
# ТОЛЬКО БИБЛИОТЕКА (N9). Точки входа нет: подключается `source`, режим в
# индексе — 100644, исполнение по пути отказывает кодом 2. Гейт кодов возврата
# читателей перечня (`internal/repohygiene/rosterreaderexitcode_test.go`) знает
# читателей по ИМЕНИ функции; вызов файла по пути прошёл бы мимо него.
#
# Использование:
#   . "$(dirname "$0")/lib/render-chain.sh"
#   args="$(render_chain_args <цепочка> <каталог> [<образец>])" || <отказ>
#   samples="$(render_chain_samples)" || <отказ>
#
# Контракт (один с обёрткой Go `deployStacksForRender`):
#   - <каталог> обязателен и означает то же, что у `stacks_args`: каждый профиль
#     выводится как `-f <каталог>/<профиль>`; пустой либо не каталог — отказ;
#   - образец принимается на ЛЮБОЙ цепочке, дописывается ТОЛЬКО к `prod`;
#     у `prod` он обязателен;
#   - образец — имя ОБЫЧНОГО файла каталога образцов точным совпадением; иное
#     (`.`, `../…`, подкаталог, несуществующее) — отказ с перечнем каталога;
#   - элемент образца — АБСОЛЮТНЫЙ путь, выведенный из положения этого файла:
#     верен из любого каталога вызывающего при любом значении <каталог>.
# Отказ — ненулевой код, причина в stderr и ПУСТОЙ вывод. Отказ таблицы — это
# отказ `stacks_args`, и он доезжает: строка берётся присваиванием с кодом.
#
# Опции оболочки здесь НЕ выставляются (тот же довод, что у `stacks.sh`).

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  echo "FATAL: render-chain.sh — библиотека, подключайте source: . \"\$(dirname \"\$0\")/lib/render-chain.sh\"" >&2
  exit 2
fi

_render_chain_self="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)" || {
  echo "FATAL: render-chain.sh — каталог библиотеки не разрешается" >&2
  return 2
}
# shellcheck source=deploy/tests/helm/stacks.sh
. "$_render_chain_self/../stacks.sh"

_render_chain_refuse() { echo "FATAL: render_chain_args — $1" >&2; return 2; }

# _render_chain_samples_dir — абсолютный путь каталога образцов.
_render_chain_samples_dir() {
  (cd "$_render_chain_self/../../../testdata/mail-node" 2>/dev/null && pwd) || {
    echo "FATAL: render-chain.sh — каталог образцов $_render_chain_self/../../../testdata/mail-node не разрешается" >&2
    return 2
  }
}

# render_chain_samples — имена ОБЫЧНЫХ файлов каталога образцов, по одному на
# строку. Пустой каталог — отказ, а не «образцов нет».
render_chain_samples() {
  local dir f out=""
  dir="$(_render_chain_samples_dir)" || return $?
  for f in "$dir"/* "$dir"/.[!.]*; do
    [ -f "$f" ] && [ ! -L "$f" ] || continue
    out="$out${f##*/}"$'\n'
  done
  [ -n "$out" ] || {
    echo "FATAL: render_chain_samples — в каталоге образцов $dir нет ни одного файла — обходить нечего" >&2
    return 2
  }
  printf '%s' "$out"
}

# render_chain_args <цепочка> <каталог> [<образец>]
render_chain_args() {
  local chain="${1:-}" dir="${2:-}" sample="${3:-}" base samples sdir listing
  [ -n "$chain" ] || { _render_chain_refuse "цепочка не названа"; return 2; }
  [ -n "$dir" ] || { _render_chain_refuse "каталог обязателен: render_chain_args <цепочка> <каталог> [<образец>]"; return 2; }
  [ -d "$dir" ] || { _render_chain_refuse "\`$dir\` — не каталог; каталог обязателен: render_chain_args <цепочка> <каталог> [<образец>]"; return 2; }
  base="$(stacks_args "$chain" "$dir")" || return $?
  if [ "$chain" = prod ] && [ -z "$sample" ]; then
    _render_chain_refuse "образец обязателен для prod: слой оператора — узел почты — в профиле не лежит (Д48)"
    return 2
  fi
  if [ -n "$sample" ]; then
    samples="$(render_chain_samples)" || return $?
    if ! grep -qxF -- "$sample" <<<"$samples"; then
      listing="$(printf '%s' "$samples" | tr '\n' ',' | sed 's/,$//; s/,/, /g')"
      _render_chain_refuse "образец \`$sample\` не файл каталога образцов; в каталоге: $listing"
      return 2
    fi
  fi
  if [ "$chain" = prod ]; then
    sdir="$(_render_chain_samples_dir)" || return $?
    printf '%s -f %s\n' "$base" "$sdir/$sample"
  else
    printf '%s\n' "$base"
  fi
}
