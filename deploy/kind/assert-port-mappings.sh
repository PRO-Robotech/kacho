#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# assert-port-mappings.sh [<имя кластера>] — узел кластера kind публикует на хост
# КАЖДОЕ отображение порта, объявленное deploy/kind/kind-config.yaml.
#
# Зачем (kacho#3025). Отображение портов узла задаётся ТОЛЬКО при создании
# кластера: kind не меняет его у живого. Кластер, созданный до того, как в
# объявление вошёл порт TLS (443 → 28443), поднимается тем же `make dev-up` без
# единой ошибки — и консоль стенда, объявленная по https на 28443, недостижима:
# браузер не находит слушателя, а стенд выглядит поднятым. Это «условие не
# создано», а не «продукт сломан», и сказать это обязан подъём, а не человек,
# разбирающий отказ браузера.
#
# Исходы: 0 — каждое объявленное отображение опубликовано; 1 — часть не
# опубликована (названы порты и как пересоздать); 2 — опросить нечем (нет
# объявления, нет узла, демон не ответил).
set -uo pipefail

CLUSTER="${1:-${CLUSTER_NAME:-kacho}}"
CONFIG="${KIND_CONFIG:-$(cd "$(dirname "$0")" && pwd)/kind-config.yaml}"
NODE="${CLUSTER}-control-plane"

[ -f "$CONFIG" ] || { echo "ABORT: объявление кластера $CONFIG не найдено — сверять не с чем" >&2; exit 2; }

# Пары «порт узла → порт хоста» из объявления. Разбор по форме самого файла:
# `containerPort:` и следующая за ней `hostPort:` одного элемента.
pairs="$(awk '
  /containerPort:/ { c=$NF; next }
  /hostPort:/ && c != "" { print c ":" $NF; c="" }
' "$CONFIG")"
[ -n "$pairs" ] || { echo "ABORT: в $CONFIG не найдено ни одного отображения порта — предмет исчез" >&2; exit 2; }

docker inspect "$NODE" >/dev/null 2>&1 || {
  echo "ABORT: узла $NODE нет либо демон не ответил — отображение не опрошено" >&2; exit 2; }

missing=""; n=0; k=0
for p in $pairs; do
  cport="${p%%:*}"; hport="${p##*:}"; n=$((n + 1))
  published="$(docker port "$NODE" "$cport/tcp" 2>/dev/null || true)"
  if ! grep -Eq ":${hport}\$" <<<"$published"; then
    missing="$missing ${cport}→${hport}"; k=$((k + 1))
  fi
done

if [ -n "$missing" ]; then
  echo "ERROR: узел $NODE не публикует $k из $n объявленных отображений:$missing" >&2
  echo "       Кластер создан до того, как они вошли в $(basename "$CONFIG"); kind не меняет" >&2
  echo "       отображение у живого кластера. Пересоздай стенд:" >&2
  echo "         make dev-down && make dev-up" >&2
  exit 1
fi
echo "=== отображения портов узла $NODE: опубликованы все $n из $(basename "$CONFIG") ==="
