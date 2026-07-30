#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# geo: транспорт ребра, несущего решение о доступе, взведён во ВСЕХ профилях.
#
# Предмет. Ребро geo→iam несёт per-RPC Check (решение о доступе) и переданную
# личность вызывающего. Клиентские creds на невзведённой ручке вырождаются в
# insecure БЕЗ ошибки, поэтому процесс поднимался бы и отчитывался «authz
# включён», пока Check уходит по открытому каналу. Теперь стража отказывает в
# старте, а умолчание чарта — взведено.
#
# Что этот файл закрывает и не могут закрыть Go-тесты: Go-стража работает с
# конфигом ПРОЦЕССА, а сюда конфиг приезжает из файлов значений. Проверяется
# ДЕКЛАРАЦИЯ (сами файлы), а не отрендеренный шаблон: рендер зависит от того,
# какие ключи профиль переопределил, и проверка рендера пропустила бы профиль,
# который просто не деплоит geo.
#
#   (1) умолчание чарта — взведено (профиль, забывший ручку, получает безопасное);
#   (2) ни один коммитнутый профиль не выключает её обратно;
#   (3) чарт РЕАЛЬНО отдаёт ручку в окружение пода при взведённом ребре.
#
# Проверка предпосылки: если ключа `edges.iamAuthz` в чарте больше нет (переезд
# на другое имя), тест обязан упасть, а не молча «ничего не найти».
set -euo pipefail

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CHART="$DEPLOY_ROOT/helm/umbrella/charts/kacho-geo"
VALUES="$CHART/values.yaml"
KNOB="KACHO_GEO_IAM_AUTHZ_MTLS_ENABLE"

N=0
fail() { echo "FAIL($SCRIPT): $1"; exit 1; }
ok() { N=$((N + 1)); }

[ -d "$CHART" ] || fail "geo chart not found at $CHART"
[ -f "$VALUES" ] || fail "geo chart values not found at $VALUES"

# (0) предпосылка: ключ существует под mtls.edges.
grep -qE '^[[:space:]]+edges:' "$VALUES" || fail "предпосылка не выполнена: в чарте geo нет блока mtls.edges — проверка потеряла предмет"
grep -qE '^[[:space:]]+iamAuthz:' "$VALUES" || fail "предпосылка не выполнена: в чарте geo нет ключа mtls.edges.iamAuthz — проверка потеряла предмет"
ok

# (1) умолчание чарта взведено.
default_val="$(grep -E '^[[:space:]]+iamAuthz:[[:space:]]*(true|false)' "$VALUES" | head -1 | sed 's/.*iamAuthz:[[:space:]]*//' | tr -d '[:space:]')"
[ -n "$default_val" ] || fail "не удалось прочитать умолчание mtls.edges.iamAuthz в $VALUES"
[ "$default_val" = "true" ] || fail "умолчание mtls.edges.iamAuthz=$default_val: профиль, забывший ручку, отгрузил бы открытый канал под решением о доступе"
ok

# (2) ни один коммитнутый профиль не выключает ребро обратно. Ищем в блоке
# kacho-geo каждого профиля.
for prof in "$DEPLOY_ROOT"/helm/umbrella/values*.yaml; do
  [ -f "$prof" ] || continue
  off="$(awk '
    /^kacho-geo:/ { inblk = 1; next }
    inblk && /^[A-Za-z]/ { inblk = 0 }
    inblk && $1 == "iamAuthz:" && $2 == "false" { print FILENAME": "NR; }
  ' "$prof")"
  [ -z "$off" ] || fail "профиль выключает ребро geo→iam: $off"
done
ok

# (3) взведённое ребро реально доезжает до окружения пода.
if command -v helm >/dev/null 2>&1; then
  render="$(helm template geo-edge "$CHART" --set mtls.enable=true --set mtls.edges.iamAuthz=true 2>/dev/null || true)"
  [ -n "$render" ] || fail "helm template не отрендерил чарт geo"
  printf '%s\n' "$render" | grep -q -- "- name: $KNOB" || fail "чарт не отдаёт $KNOB в окружение пода при взведённом ребре"
  ok
else
  echo "note($SCRIPT): helm не найден — проверка (3) пропущена, декларации (0)-(2) проверены"
fi

echo "PASS($SCRIPT): проверок выполнено: $N"
