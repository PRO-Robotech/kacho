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

# ── Три исхода — ОБЩЕЙ реализацией каталога, а не своей копией ───────────────
#
# 0 зелено · 1 находка о дереве · 2 условие не создано (плюс текст самого helm).
# Своя копия разошлась с каталогом по трём осям:
#
#   • ОТКАЗ РЕНДЕРА объявлялся находкой о дереве («helm template не отрендерил
#     чарт geo», код 1) — тем же кодом, каким объявляется настоящий дефект;
#   • ТЕКСТ HELM ГЛУШИЛСЯ (`2>/dev/null`) и рендер шёл ИЗ ПОДСТАНОВКИ, поэтому
#     причина отказа не доезжала до читателя вовсе;
#   • ОТСУТСТВИЕ helm ПРОПУСКАЛО проверку (3) и оставляло вердикт ЗЕЛЁНЫМ —
#     то есть «не выполнилось», зачтённое в успех (`e2e-flow.md` §1). Теперь это
#     `require_helm`: у третьей категории есть свой код, и она считается.
#
# Счёт утверждений скрипт ведёт САМ (число зависит от того, сколько профилей
# лежит в дереве), поэтому вердикт печатает `findings_verdict`, а не
# `outcome_verdict`.
# shellcheck source=deploy/tests/helm/outcome.sh
. "$(dirname "$0")/outcome.sh"

# Проверка (3) рендерит чарт. Прежде она была условной («нет helm — пропускаем,
# декларации проверены»), и пропуск не отличался от исполнения: оба давали PASS.
require_helm

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
  local profiles_read=0

  # Файла нет — значит СУДИТЬ НЕ О ЧЕМ (условие не создано, код 2), а не «в дереве
  # дефект»: прежде это возвращало тот же код 1, каким объявляется находка.
  require_file_present "$values" "чарт geo (values.yaml сабчарта)"

  # (0) предпосылка: оба ключа существуют. Здесь код 1 ВЕРЕН и остаётся: ключ,
  # переехавший на другое имя, — это находка о дереве, а не отсутствие условия.
  # `fail` из общей реализации обрывает прогон немедленно, как и прежнее
  # `return 1`: последующие проверки читали бы не тот ключ и прошли бы вакуумно.
  grep -qE '^[[:space:]]+edges:' "$values" || fail "$SCRIPT: предпосылка не выполнена — в чарте geo нет блока mtls.edges"
  grep -qE '^[[:space:]]+iamAuthz:' "$values" || fail "$SCRIPT: предпосылка не выполнена — в чарте geo нет ключа mtls.edges.iamAuthz"
  grep -qE '^[[:space:]]+enable:' "$values" || fail "$SCRIPT: предпосылка не выполнена — в чарте geo нет ключа mtls.enable"
  ok

  # (1) умолчание САМОГО РЕБРА взведено. Умолчание `mtls.enable` здесь не
  # проверяется намеренно: с выключенным mTLS процесс не стартует вовсе
  # (существующая стража требует mTLS на обоих листенерах) — это громкий отказ,
  # а не тихо небезопасная посадка. Опасна другая форма: профиль ДЕПЛОИТ geo и
  # при этом снимает ручку — её ловит проверка (2).
  local def_edge
  def_edge="$(chart_mtls_value "$values" iamAuthz | tr -d '[:space:]')"
  # `violation` НАКАПЛИВАЕТ находку и продолжает — ровно то, что здесь делал
  # `fail=1`. Имя `fail` под это не годится: в общей реализации оно ОБРЫВАЕТ
  # кодом 1, то есть один и тот же глагол означал бы в каталоге противоположное.
  [ "$def_edge" = "true" ] || violation "умолчание mtls.edges.iamAuthz=$def_edge — профиль, забывший ручку, отгрузил бы открытый канал под решением о доступе"
  ok

  # (2) ни один коммитнутый профиль не выключает ребро — ни ручкой, ни блоком.
  local prof
  for prof in "$root"/helm/umbrella/values*.yaml; do
    [ -f "$prof" ] || continue
    profiles_read=$((profiles_read + 1))
    local edge mtls
    edge="$(geo_mtls_value "$prof" iamAuthz)"
    mtls="$(geo_mtls_value "$prof" enable)"
    [ "$edge" != "false" ] || violation "профиль выключает ребро geo→iam (mtls.edges.iamAuthz=false): $prof"
    [ "$mtls" != "false" ] || violation "профиль снимает весь блок mTLS у geo (mtls.enable=false) — ручка ребра не доезжает до пода: $prof"
  done
  [ "$profiles_read" -gt 0 ] || violation "перепись профилей вернула ноль файлов — «ни один не выключает» неотличимо от «ни один не прочитан»"
  ok

  # (3) взведённое ребро реально доезжает до окружения пода — со ЗНАЧЕНИЕМ true.
  #
  # Рендер зовётся НЕ из подстановки: `helm_try`/`render_nonempty_or_fatal` пишут
  # в глобальные переменные, и из подоболочки ни код возврата helm, ни текст его
  # отказа наружу не отдались бы. Отказ рендера и пустой успешный рендер — оба
  # УСЛОВИЕ прогона (код 2 плюс текст helm), а не находка о дереве.
  local val
  helm_try geo-edge "$chart" --set mtls.enable=true --set mtls.edges.iamAuthz=true
  render_nonempty_or_fatal "чарт geo (mtls.enable=true, mtls.edges.iamAuthz=true)"
  val="$(printf '%s\n' "$HELM_OUT" | grep -A1 -- "- name: $KNOB" | sed -n 's/^ *value: *//p' | head -1 | tr -d '"[:space:]')"
  [ "$val" = "true" ] || violation "чарт отдаёт $KNOB со значением '$val' (ожидалось true)"
  ok

  findings_verdict "профилей прочитано: $profiles_read"
}

# ─────────────────────────────────────────────────────────────────────────────
# Самопроверка: гейт обязан ПОКРАСНЕТЬ на каждом из двух способов снять ребро.
# Без неё он доказывает лишь, что отработал, — а не что отличает взведённое
# ребро от снятого. Инъекция идёт в КОПИЮ дерева, репозиторий не трогается.
# ─────────────────────────────────────────────────────────────────────────────
if [ "${1:-}" = "--self-test" ]; then
  echo "=== self-test($SCRIPT): два способа снять ребро — оба обязаны краснеть ==="
  # run_checks зовётся В ПОДОБОЛОЧКЕ: вердикт общей реализации ВЫХОДИТ (`fail`,
  # `fatal`, `findings_verdict`), а не возвращается, — прямой вызов унёс бы с
  # собой всю самопроверку на первой же инъекции. Подоболочка заодно держит
  # счётчики (`N`, `VIOLATIONS`, `RENDERS`) раздельными: иначе находки первой
  # инъекции пережили бы её и покрасили негативный контроль (г).
  rc=0
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  cp -r "$DEPLOY_ROOT/helm" "$tmp/helm"

  # (а) прямое снятие ручки в профиле.
  first_profile="$(ls "$tmp"/helm/umbrella/values*.yaml | head -1)"
  cp "$first_profile" "$first_profile.orig"
  awk '/^kacho-geo:/{print; print "  mtls:"; print "    edges:"; print "      iamAuthz: false"; next} {print}' \
    "$first_profile.orig" > "$first_profile"
  if ( run_checks "$tmp" ) >/dev/null 2>&1; then
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
  if ( run_checks "$tmp" ) >/dev/null 2>&1; then
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
  if ( run_checks "$tmp" ) >/dev/null 2>&1; then
    echo "  ПРОВАЛ: небезопасное умолчание чарта не покраснело"
    rc=1
  else
    echo "  ОК     небезопасное умолчание чарта → красный"
  fi
  mv "$chart_values.orig" "$chart_values"

  # (г) негативный контроль: нетронутое дерево обязано быть ЗЕЛЁНЫМ — иначе
  #     гейт краснеет на всём подряд и его первое же срабатывание снимут.
  if ( run_checks "$tmp" ) >/dev/null 2>&1; then
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

# Вердикт печатает `findings_verdict` внутри run_checks и он же ВЫХОДИТ нужным
# кодом; прежнее `|| exit 1` схлопывало бы код 2 («условие не создано») в код 1.
run_checks "$DEPLOY_ROOT"
