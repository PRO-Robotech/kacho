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
# Ребро выключается ДВУМЯ способами, и оба обязаны быть находкой:
#   (а) `mtls.edges.iamAuthz: false` — прямое снятие ручки;
#   (б) `mtls.enable: false` — весь блок mTLS в шаблоне под этим ключом, поэтому
#       ручка не доезжает до пода вовсе. Первая редакция этой проверки видела
#       только (а) и пропускала (б).
#
# Проверка предпосылки: если ключей `edges.iamAuthz` / `enable` в чарте больше
# нет (переезд на другое имя), тест обязан упасть, а не молча «ничего не найти»;
# то же — если перепись профилей вернула ноль файлов.
set -uo pipefail

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CHART="$DEPLOY_ROOT/helm/umbrella/charts/kacho-geo"
VALUES="$CHART/values.yaml"
KNOB="KACHO_GEO_IAM_AUTHZ_MTLS_ENABLE"

# geo_block_value <файл> <ключ> — значение ключа внутри блока kacho-geo профиля
# (пусто, если ключа там нет). Читаем текстом, а не через yq: в разных средах под
# этим именем стоят два несовместимых инструмента, и проверка не должна зависеть
# от того, какой оказался в PATH.
# geo_mtls_value <файл-профиля> <enable|iamAuthz> — значение ключа именно в
# блоке kacho-geo.mtls (а не первого попавшегося `enable:` в файле: их там
# десятки, и ловить любой значило бы читать не тот ключ).
geo_mtls_value() {
  awk -v leaf="$2" '
    function indent(l,   i) { i = match(l, /[^ ]/); return i == 0 ? 0 : i - 1 }
    /^kacho-geo:/ { g = 1; next }
    g && /^[A-Za-z]/ { g = 0 }
    !g { next }
    { ind = indent($0) }
    ind == 2 && $1 == "mtls:" { m = 1; next }
    m && ind <= 2 { m = 0 }
    !m { next }
    ind == 4 && $1 == "edges:" { e = 1; next }
    e && ind <= 4 { e = 0 }
    leaf == "enable" && !e && ind == 4 && $1 == "enable:" { sub(/^[^:]*:[ \t]*/, ""); gsub(/^"|"$/, ""); print; exit }
    leaf == "iamAuthz" && e && ind == 6 && $1 == "iamAuthz:" { sub(/^[^:]*:[ \t]*/, ""); gsub(/^"|"$/, ""); print; exit }
  ' "$1"
}

# chart_mtls_value <values.yaml чарта> <enable|iamAuthz> — то же для чарта, где
# блок mtls лежит на верхнем уровне.
chart_mtls_value() {
  awk -v leaf="$2" '
    function indent(l,   i) { i = match(l, /[^ ]/); return i == 0 ? 0 : i - 1 }
    /^mtls:/ { m = 1; next }
    m && /^[A-Za-z]/ { m = 0 }
    !m { next }
    { ind = indent($0) }
    ind == 2 && $1 == "edges:" { e = 1; next }
    e && ind <= 2 { e = 0 }
    leaf == "enable" && !e && ind == 2 && $1 == "enable:" { sub(/^[^:]*:[ \t]*/, ""); gsub(/^"|"$/, ""); print; exit }
    leaf == "iamAuthz" && e && ind == 4 && $1 == "iamAuthz:" { sub(/^[^:]*:[ \t]*/, ""); gsub(/^"|"$/, ""); print; exit }
  ' "$1"
}

# run_checks — сам вердикт. Вынесен в функцию, чтобы самопроверка могла прогнать
# его на дереве с инъекцией и потребовать КРАСНОГО.
run_checks() {
  local root chart values
  root="$1"
  chart="$root/helm/umbrella/charts/kacho-geo"
  values="$chart/values.yaml"
  local n=0 profiles_read=0
  local fail=0

  [ -f "$values" ] || { echo "FAIL($SCRIPT): чарт geo не найден: $values"; return 1; }

  # (0) предпосылка: оба ключа существуют.
  grep -qE '^[[:space:]]+edges:' "$values" || { echo "FAIL($SCRIPT): предпосылка не выполнена — в чарте geo нет блока mtls.edges"; return 1; }
  grep -qE '^[[:space:]]+iamAuthz:' "$values" || { echo "FAIL($SCRIPT): предпосылка не выполнена — в чарте geo нет ключа mtls.edges.iamAuthz"; return 1; }
  grep -qE '^[[:space:]]+enable:' "$values" || { echo "FAIL($SCRIPT): предпосылка не выполнена — в чарте geo нет ключа mtls.enable"; return 1; }
  n=$((n + 1))

  # (1) умолчание САМОГО РЕБРА взведено. Умолчание `mtls.enable` здесь не
  # проверяется намеренно: с выключенным mTLS процесс не стартует вовсе
  # (существующая стража требует mTLS на обоих листенерах) — это громкий отказ,
  # а не тихо небезопасная посадка. Опасна другая форма: профиль ДЕПЛОИТ geo и
  # при этом снимает ручку — её ловит проверка (2).
  local def_edge
  def_edge="$(chart_mtls_value "$values" iamAuthz | tr -d '[:space:]')"
  [ "$def_edge" = "true" ] || { echo "FAIL($SCRIPT): умолчание mtls.edges.iamAuthz=$def_edge — профиль, забывший ручку, отгрузил бы открытый канал под решением о доступе"; fail=1; }
  n=$((n + 1))

  # (2) ни один коммитнутый профиль не выключает ребро — ни ручкой, ни блоком.
  local prof
  for prof in "$root"/helm/umbrella/values*.yaml; do
    [ -f "$prof" ] || continue
    profiles_read=$((profiles_read + 1))
    local edge mtls
    edge="$(geo_mtls_value "$prof" iamAuthz)"
    mtls="$(geo_mtls_value "$prof" enable)"
    [ "$edge" != "false" ] || { echo "FAIL($SCRIPT): профиль выключает ребро geo→iam (mtls.edges.iamAuthz=false): $prof"; fail=1; }
    [ "$mtls" != "false" ] || { echo "FAIL($SCRIPT): профиль снимает весь блок mTLS у geo (mtls.enable=false) — ручка ребра не доезжает до пода: $prof"; fail=1; }
  done
  [ "$profiles_read" -gt 0 ] || { echo "FAIL($SCRIPT): перепись профилей вернула ноль файлов — «ни один не выключает» неотличимо от «ни один не прочитан»"; fail=1; }
  n=$((n + 1))

  # (3) взведённое ребро реально доезжает до окружения пода — со ЗНАЧЕНИЕМ true.
  if command -v helm >/dev/null 2>&1; then
    local render val
    render="$(helm template geo-edge "$chart" --set mtls.enable=true --set mtls.edges.iamAuthz=true 2>/dev/null)"
    if [ -z "$render" ]; then
      echo "FAIL($SCRIPT): helm template не отрендерил чарт geo"
      fail=1
    else
      val="$(printf '%s\n' "$render" | grep -A1 -- "- name: $KNOB" | sed -n 's/^ *value: *//p' | head -1 | tr -d '"[:space:]')"
      [ "$val" = "true" ] || { echo "FAIL($SCRIPT): чарт отдаёт $KNOB со значением '$val' (ожидалось true)"; fail=1; }
    fi
    n=$((n + 1))
  else
    echo "note($SCRIPT): helm не найден — проверка (3) пропущена, декларации (0)-(2) проверены"
  fi

  [ "$fail" -eq 0 ] || return 1
  echo "PASS($SCRIPT): проверок выполнено: $n; профилей прочитано: $profiles_read"
  return 0
}

# ─────────────────────────────────────────────────────────────────────────────
# Самопроверка: гейт обязан ПОКРАСНЕТЬ на каждом из двух способов снять ребро.
# Без неё он доказывает лишь, что отработал, — а не что отличает взведённое
# ребро от снятого. Инъекция идёт в КОПИЮ дерева, репозиторий не трогается.
# ─────────────────────────────────────────────────────────────────────────────
if [ "${1:-}" = "--self-test" ]; then
  echo "=== self-test($SCRIPT): два способа снять ребро — оба обязаны краснеть ==="
  rc=0
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  cp -r "$DEPLOY_ROOT/helm" "$tmp/helm"

  # (а) прямое снятие ручки в профиле.
  first_profile="$(ls "$tmp"/helm/umbrella/values*.yaml | head -1)"
  cp "$first_profile" "$first_profile.orig"
  awk '/^kacho-geo:/{print; print "  mtls:"; print "    edges:"; print "      iamAuthz: false"; next} {print}' \
    "$first_profile.orig" > "$first_profile"
  if run_checks "$tmp" >/dev/null 2>&1; then
    echo "  ПРОВАЛ: снятая ручка iamAuthz в профиле не покраснела"
    rc=1
  else
    echo "  ОК     снятая ручка iamAuthz в профиле → красный"
  fi
  mv "$first_profile.orig" "$first_profile"

  # (б) снятие всего блока mTLS в профиле — ручка не доезжает до пода.
  cp "$first_profile" "$first_profile.orig"
  awk '/^kacho-geo:/{print; print "  mtls:"; print "    enable: false"; next} {print}' \
    "$first_profile.orig" > "$first_profile"
  if run_checks "$tmp" >/dev/null 2>&1; then
    echo "  ПРОВАЛ: снятый блок mtls.enable в профиле не покраснел"
    rc=1
  else
    echo "  ОК     снятый блок mtls.enable в профиле → красный"
  fi
  mv "$first_profile.orig" "$first_profile"

  # (в) небезопасное умолчание чарта.
  chart_values="$tmp/helm/umbrella/charts/kacho-geo/values.yaml"
  cp "$chart_values" "$chart_values.orig"
  sed -i 's/^\(    iamAuthz:\) true/\1 false/' "$chart_values"
  if run_checks "$tmp" >/dev/null 2>&1; then
    echo "  ПРОВАЛ: небезопасное умолчание чарта не покраснело"
    rc=1
  else
    echo "  ОК     небезопасное умолчание чарта → красный"
  fi
  mv "$chart_values.orig" "$chart_values"

  # (г) негативный контроль: нетронутое дерево обязано быть ЗЕЛЁНЫМ — иначе
  #     гейт краснеет на всём подряд и его первое же срабатывание снимут.
  if run_checks "$tmp" >/dev/null 2>&1; then
    echo "  ОК     нетронутое дерево → зелёный"
  else
    echo "  ПРОВАЛ: нетронутое дерево краснеет — гейт ловит форму, а не существо"
    rc=1
  fi

  if [ "$rc" -eq 0 ]; then
    echo "PASS: $SCRIPT --self-test"
  else
    echo "FAIL: $SCRIPT --self-test"
  fi
  exit "$rc"
fi

run_checks "$DEPLOY_ROOT" || exit 1
