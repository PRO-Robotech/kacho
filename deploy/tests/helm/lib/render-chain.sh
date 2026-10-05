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

# ─── ОДИНОЧНЫЙ РЕНДЕР ПОДЧАРТА kaname (NTF-1, полоса D2; замысел З28, CX1-113, N35-2)
#
# Шаблон `ConfigMap` подчарта kaname берёт флаг почты службы помощником
# `kacho.notifications.enabledFor`, а тело помощника одно на релиз — в чарте
# notify (`deploy/helm/notify/templates/_flag.tpl`, `_sources.tpl`). В рендере
# зонтика его видно; одиночный рендер `charts/kaname` его не видит и отказывает
# «no template». Поэтому одиночный рендер идёт только через эту обёртку:
#   1. копия каталога подчарта во временном каталоге;
#   2. в её `templates/` — побайтовая копия файлов помощников чарта notify (в
#      отслеживаемом дереве тело по-прежнему одно);
#   3. первым слоем `-f` — узлы `global.kacho.notifications` и
#      `global.kacho.spiffe`, выписанные `yq` из values.yaml зонтика в момент
#      рендера (второго объявления значений нет; слои вызывающего — поверх).
#
# Использование:
#   render_kaname_alone <релиз> <каталог подчарта> [аргументы helm template …]
# Печать — вывод `helm template` (stdout и stderr), код — код helm. Нехватка
# инструмента, каталога или файла помощника — код 2 с причиной в stderr.
# Тело — подоболочка: временный каталог снимается её ловушкой, ловушки
# вызывающего не трогаются. Двойник на Go — `renderKanameAlone` (deploy/).
render_kaname_alone() (
  release="${1:-}" chart="${2:-}"
  [ -n "$release" ] && [ -n "$chart" ] || {
    echo "FATAL: render_kaname_alone — нужны релиз и каталог подчарта: render_kaname_alone <релиз> <каталог> [аргументы helm]" >&2
    exit 2
  }
  shift 2
  [ -f "$chart/Chart.yaml" ] || { echo "FATAL: render_kaname_alone — \`$chart\` не каталог чарта (Chart.yaml нет)" >&2; exit 2; }
  command -v helm >/dev/null 2>&1 || { echo "FATAL: render_kaname_alone — helm не в PATH" >&2; exit 2; }
  command -v yq >/dev/null 2>&1 || { echo "FATAL: render_kaname_alone — yq не в PATH (узлы global выписываются им)" >&2; exit 2; }
  deploy="$(cd "$_render_chain_self/../../.." && pwd)" || exit 2
  notify_tpl="$deploy/helm/notify/templates"
  umbrella_values="$deploy/helm/umbrella/values.yaml"
  for f in "$notify_tpl/_flag.tpl" "$notify_tpl/_sources.tpl" "$umbrella_values"; do
    [ -f "$f" ] || { echo "FATAL: render_kaname_alone — нет $f" >&2; exit 2; }
  done
  work="$(mktemp -d)" || exit 2
  trap 'rm -rf "$work"' EXIT
  cp -R "$chart" "$work/kaname" || exit 2
  cp "$notify_tpl/_flag.tpl" "$notify_tpl/_sources.tpl" "$work/kaname/templates/" || exit 2
  yq '{"global": {"kacho": {"notifications": .global.kacho.notifications, "spiffe": .global.kacho.spiffe}}} | del(.. | select(. == null))' \
    "$umbrella_values" > "$work/globals.yaml" || {
    echo "FATAL: render_kaname_alone — узлы global не выписаны из $umbrella_values" >&2
    exit 2
  }
  helm template "$release" "$work/kaname" -f "$work/globals.yaml" "$@"
)

# render_kaname_alone_try <релиз> <каталог подчарта> [аргументы helm …] — форма
# `helm_try` библиотеки исходов (outcome.sh) для одиночного рендера kaname:
# HELM_OUT — stdout, HELM_RC — код, HELM_ERR — stderr без предупреждения о
# kubeconfig, RENDERS — +1. Отказ читается `render_or_fatal`, как у `helm_try`.
render_kaname_alone_try() {
  local errf; errf="$(mktemp)"
  HELM_OUT="$(render_kaname_alone "$@" 2>"$errf")" && HELM_RC=0 || HELM_RC=$?
  RENDERS=$((${RENDERS:-0} + 1))
  HELM_ERR="$(grep -vE 'WARNING: Kubernetes configuration' "$errf" || true)"
  rm -f "$errf"
}
