#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# render-umbrella-profiles.sh <каталог вывода> — отрендерить КАЖДЫЙ профиль зонтика
# так, как он развёртывается: цепочка `-f` из таблицы стеков (`deploy/stacks.txt`,
# читатель `deploy/tests/helm/stacks.sh`), С ХУКАМИ (умолчание `helm template`), по
# файлу на шаблон (`--output-dir`): `<вывод>/<стек>/<чарт>/…`.
#
# ЗАЧЕМ. IaC-скан осматривал подчарты зонтика с их СОБСТВЕННЫМИ умолчаниями: значения
# профиля (контекст безопасности, hostPort) и хуки до сканера не доходили, а записи
# ведомости о долге переживали исправление значений (приёмка 4240ac56fd3). Скан того,
# что развёртывается, закрывает обе слепоты: находка исчезает вместе с исправлением в
# профиле, и запись о ней становится находкой гейта исключений.
#
# Одна реализация на двух читателей: шаг задания trivy в `security-scan.yml` и гейты
# `deploy/scripts/assert-iac-*` (через `iac_scan_passes.py`) зовут ЭТОТ скрипт.
#
# Коды: 0 — все стеки отрендерены; 2 — условие не создано (нет helm, таблицы,
# зависимостей) либо стек не рендерится: вердикта по профилям нет.
set -uo pipefail

OUT="${1:?укажи каталог вывода}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)" || exit 2
UMBRELLA="$ROOT/deploy/helm/umbrella"
# Версия Kubernetes — узел стенда (kind 0.32.0 → `kindest/node:v1.36.1`); без неё helm
# рендерит под умолчанием, и чарты с требованием `kubeVersion` отказывают.
KUBE_VERSION="1.36.1"

command -v helm >/dev/null 2>&1 || { echo "ОТКАЗ: нужен helm — профили рендерить нечем" >&2; exit 2; }
# shellcheck source=deploy/tests/helm/stacks.sh
. "$ROOT/deploy/tests/helm/stacks.sh" || { echo "ОТКАЗ: читатель таблицы стеков не загружен" >&2; exit 2; }
ROWS="$(stacks_table)" || { echo "ОТКАЗ: таблица стеков не прочитана" >&2; exit 2; }
bash "$ROOT/deploy/scripts/helm-umbrella-deps.sh" "$UMBRELLA" >&2 \
  || { echo "ОТКАЗ: зависимости зонтика не материализованы — рендер был бы неполным" >&2; exit 2; }
rm -rf "$UMBRELLA"/tmpcharts-* || true

if ! { rm -rf -- "$OUT" && mkdir -p -- "$OUT"; }; then
  echo "ОТКАЗ: каталог вывода $OUT не создан" >&2; exit 2
fi
n=0
while IFS= read -r row; do
  [ -z "$row" ] && continue
  stack="${row%%:*}"; files="${row#*:}"
  args=()
  IFS=','; for f in $files; do args+=(-f "$UMBRELLA/$f"); done; unset IFS
  errf="$OUT/.err-$stack"
  # Число файлов — из отчёта самого helm («wrote <путь>» на каждый записанный шаблон),
  # а не обходом диска: считается то, что записал рендер, а не то, что лежит рядом.
  if ! wrote="$(helm template kacho-umbrella "$UMBRELLA" -n kacho "${args[@]}" \
        --kube-version "$KUBE_VERSION" --output-dir "$OUT/$stack" 2>"$errf")"; then
    echo "ОТКАЗ: стек $stack ($files) не рендерится: $(head -c 400 "$errf")" >&2
    exit 2
  fi
  rm -f -- "$errf"
  count="$(printf '%s\n' "$wrote" | grep -c '^wrote ' || true)"
  [ "$count" -ge 1 ] || { echo "ОТКАЗ: стек $stack отрендерил ноль файлов" >&2; exit 2; }
  echo "render-umbrella-profiles: стек $stack — записей шаблонов $count" >&2
  n=$((n + 1))
done <<<"$ROWS"
[ "$n" -ge 1 ] || { echo "ОТКАЗ: ни одного стека не отрендерено" >&2; exit 2; }
echo "render-umbrella-profiles: стеков $n в $OUT" >&2
