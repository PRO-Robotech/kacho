#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

#
# render-guard.sh — helm-render assertions for the kacho-vpc deploy chart.
#
# Guards the vpc→geo edge wiring: the edge MUST dial the geo k8s Service name
# `kacho-geo` (whose server-cert SAN covers kacho-geo.* / kacho-geo-internal.*),
# NOT the bare `geo.kacho...` host — that host neither resolves nor passes TLS
# serverName verification.
#
# Asserts, against `helm template` with mtls.edges.geo=true:
#   1. the rendered ConfigMap dials extapi.geo.endpoint = kacho-geo.kacho.svc:9090
#   2. the rendered Deployment sets KACHO_VPC_GEO_MTLS_SERVERNAME = kacho-geo.kacho.svc
#   3. the old `geo.kacho.svc` host appears NOWHERE in the render.
#
# Usage: deploy/render-guard.sh   (run from the chart's parent, i.e. repo root or deploy/)
# Exit 0 = all assertions pass; non-zero = a guard failed.
set -euo pipefail

HELM_BIN="${HELM_BIN:-helm}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHART_DIR="$SCRIPT_DIR"

GOOD_HOST="kacho-geo.kacho.svc"
BAD_HOST="geo.kacho.svc"

fail() { echo "render-guard: FAIL: $*" >&2; exit 1; }

RENDER="$("$HELM_BIN" template vpc "$CHART_DIR" \
  --set mtls.enable=true \
  --set mtls.edges.geo=true)"

# Сравнение — встроенное, БЕЗ трубы. `echo … | grep -qE` под `set -o pipefail`
# возвращает ОТКАЗ НА СОВПАДЕНИИ, когда вывод не помещается в буфер трубы
# целиком: grep выходит по первому попаданию, писатель получает SIGPIPE, и
# `pipefail` поднимает ЕГО статус до статуса всего конвейера. Замер на Linux
# (буфер трубы 64 KiB), совпадение в начале вывода: 8 КБ — 0 ложных отказов из
# 200, 70 КБ — 177, 300 КБ — 200. Отсюда «локально зелено, на ранере красно»
# при одном и том же дереве. Задача #658.
#
# Правая часть `=~` идёт БЕЗ кавычек — иначе bash сравнивал бы буквально.
# Многострочность: у `grep` якорь `^` привязан к началу КАЖДОЙ строки, у `=~` —
# к началу всей строки-субъекта. Здесь это не расхождение: класс
# `[^-a-zA-Z0-9]` содержит перевод строки, поэтому начало любой строки, кроме
# первой, покрыто второй ветвью альтернативы.

# 1. ConfigMap geo endpoint dials the corrected Service host on the public :9090 listener.
RE_ENDPOINT="endpoint:[[:space:]]*\"${GOOD_HOST}:9090\""
[[ "$RENDER" =~ $RE_ENDPOINT ]] \
  || fail "ConfigMap geo endpoint is not ${GOOD_HOST}:9090"

# 2. Deployment mTLS serverName for the geo edge is the corrected Service host.
RE_SERVERNAME="value:[[:space:]]*\"${GOOD_HOST}\""
[[ "$RENDER" =~ $RE_SERVERNAME ]] \
  || fail "Deployment KACHO_VPC_GEO_MTLS_SERVERNAME is not ${GOOD_HOST}"

# 3. The bare `geo.kacho...` host must not appear anywhere (configmap endpoint
#    OR mtls serverName). Anchor the leading boundary to a non-host char
#    ([^-a-zA-Z0-9]) so the correct host `kacho-geo.kacho...` (where `geo`
#    follows `-`) is NOT a false positive — only a standalone `geo.kacho...`
#    token is rejected.
RE_BAD_HOST="(^|[^-a-zA-Z0-9])${BAD_HOST//./\\.}"
if [[ "$RENDER" =~ $RE_BAD_HOST ]]; then
  # Печать координат — `grep` БЕЗ `-q`: он дочитывает вход до конца, писатель
  # SIGPIPE не получает, и статус здесь всё равно никем не читается.
  echo "$RENDER" | grep -nE "$RE_BAD_HOST" >&2
  fail "old wrong geo host '${BAD_HOST}' still present in rendered manifests"
fi

echo "render-guard: OK — vpc→geo dials ${GOOD_HOST} (Service + cert SAN)"
