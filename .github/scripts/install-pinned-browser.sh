#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# install-pinned-browser.sh — добыть ПИННУТЫЙ браузер и сказать, каким браузером
# в итоге пойдут пробы.
#
# ПРЕДМЕТ (#1047). Пин версии браузера заведён затем, чтобы вердикт не зависел от
# того, что стоит на ранере. Он НЕ ДЕЙСТВОВАЛ — пять прогонов из пяти пробы шли в
# системный браузер образа, — и это было не видно ни по одному признаку: полоса
# зелёная, а сообщение о причине называло ЧУЖУЮ сторону.
#
# Два сцепленных дефекта, и второй хуже первого:
#
#   1. шаг обрывался внешним пределом на РАСПАКОВКЕ (21:40:54 → 21:41:54, ровно
#      60 с), при том что раздача ответила `200` и отдала 174 МиБ за секунду;
#   2. текст отказа говорил «раздача playwright не ответила и после её собственных
#      пяти попыток». Пока текст обвиняет внешнюю сторону, никто не ищет дефект у
#      себя — и не замечает, что версия браузера в вердикте произвольная.
#
# ─── ДВА ПРЕДЕЛА, И КАЖДЫЙ НАЗВАН СВОЕЙ ВЕЛИЧИНОЙ ───────────────────────────
#
# Один внешний `timeout` на весь процесс не различает фаз by construction: он
# обрывает и молчащую раздачу, и затянувшуюся распаковку, а сказать, что именно
# оборвал, не может. Отсюда обе беды выше — и вторая прямо следовала из первой.
#
# Поэтому фазы стерегутся ОТДЕЛЬНО, по маркеру в журнале установки:
#
#   · DOWNLOAD_BUDGET — до появления признака завершённой загрузки. Раздача
#     исправна отдаёт архив за секунду; сам playwright стережёт молчащий сокет
#     (30 с) и повторяет до пяти раз, печатая «Downloading …» на каждом повторе.
#     Бюджет обязан вмещать эти повторы, иначе мы обрываем чужой механизм
#     восстановления раньше, чем он сработает, — ровно то, что делал прежний
#     внешний предел.
#   · UNPACK_BUDGET — от завершённой загрузки до выхода процесса. Распаковка 174
#     МиБ в тысячи файлов занимает 7.1 с на НЕЗАНЯТОЙ машине (замерено
#     `DEBUG=pw:install`: «download complete» → «fixing permissions»). Шаг стоит
#     ДО подъёма стенда — порядок держит гейт `internal/repohygiene`
#     `TestBrowserIsAcquiredBeforeTheStandTakesTheRunner`, — поэтому машина
#     незанята, и запас здесь на порядок, а не «на всякий случай».
#
# Величины названы переменными, а не вписаны числом в команду: их пересматривают
# замером, и замер должен иметь что менять.
#
# ─── ЧТО ПЕЧАТАЕТСЯ ВСЕГДА ──────────────────────────────────────────────────
#
# ПУТЬ И ВЕРСИЯ БРАУЗЕРА, которым пойдут пробы. Без этой строки «пин действует» и
# «пин не действует» неотличимы: оба состояния дают зелёную полосу. Строка
# печатается на ОБОИХ исходах — и когда браузер добыт, и когда взят из образа.
#
# ─── ОТКАЗ ЭТОГО СКРИПТА — «УСЛОВИЕ НЕ СОЗДАНО» ─────────────────────────────
#
# Пробы при этом не выполнялись вовсе, и такой исход не зачитывается ни в
# зелёное, ни в красное. Категорию читает `console-run-category.py`.
#
# Запуск:
#   install-pinned-browser.sh [--github-output <файл>] [--github-env <файл>]
#   install-pinned-browser.sh --self-test

set -uo pipefail

# ── Величины ─────────────────────────────────────────────────────────────────
#
# ЧИТАЮТСЯ В МОМЕНТ ВЫЗОВА, а не при загрузке скрипта. Разница не косметическая:
# при чтении на загрузке самопроверка не может подменить ни бюджет, ни команду —
# переменные уже вычислены, — и она молча гоняла бы НАСТОЯЩИЙ `playwright
# install` вместо заглушек, объявляя все четыре фазы успешными. Первая редакция
# была именно такой, и поймала её собственная самопроверка: четыре утверждения
# из одиннадцати покраснели на исправном по виду коде.
download_budget() { printf '%s' "${KACHO_BROWSER_DOWNLOAD_BUDGET:-240}"; }
unpack_budget()   { printf '%s' "${KACHO_BROWSER_UNPACK_BUDGET:-180}"; }
# Команда добычи. Подменяется только самопроверкой: настоящий playwright в ней
# недоступен, а предмет проверки — РАЗЛИЧЕНИЕ ФАЗ, а не сам playwright.
install_cmd()     { printf '%s' "${KACHO_BROWSER_INSTALL_CMD:-npx playwright install chromium}"; }
# Признак завершённой загрузки в журнале `DEBUG=pw:install`. Их два, потому что
# playwright печатает разные строки на разных путях, и достаточно любой.
DOWNLOAD_DONE_RE='download complete|100% of '

die_unmet() {
  echo "::error title=Условие не создано::$1 Это НЕ вердикт проб: они не выполнялись вовсе, и такой исход не зачитывается ни в зелёное, ни в красное." >&2
  exit 1
}

# ─────────────────────────────────────────────────────────────────────────────
# ФАЗЫ
# ─────────────────────────────────────────────────────────────────────────────
#
# Возвращает: 0 — установка прошла; 10 — не уложилась ЗАГРУЗКА; 20 — не уложилась
# РАСПАКОВКА; 30 — процесс завершился ошибкой, уложившись в оба предела (тогда
# виноват не предел, и говорить надо про сам отказ).
run_install() {
  local log="$1"
  local dl_budget unp_budget cmd
  dl_budget="$(download_budget)"; unp_budget="$(unpack_budget)"; cmd="$(install_cmd)"
  : > "$log"
  # shellcheck disable=SC2086  # команда задана как строка и обязана разбиться на слова
  ( DEBUG=pw:install $cmd >>"$log" 2>&1 ) &
  local pid=$!

  local waited=0 downloaded=0
  while [ "$waited" -lt "$dl_budget" ]; do
    if ! kill -0 "$pid" 2>/dev/null; then
      # Процесс вышел, не дожидаясь наших пределов: либо всё сделал, либо упал.
      # Готовая установка выходит мгновенно и загрузки не печатает — это НОРМА,
      # а не «загрузка не началась».
      wait "$pid"; local st=$?
      [ "$st" -eq 0 ] && return 0
      return 30
    fi
    if grep -qE "$DOWNLOAD_DONE_RE" "$log" 2>/dev/null; then downloaded=1; break; fi
    sleep 1; waited=$((waited + 1))
  done

  if [ "$downloaded" -eq 0 ]; then
    kill -9 "$pid" 2>/dev/null; wait "$pid" 2>/dev/null
    return 10
  fi

  waited=0
  while [ "$waited" -lt "$unp_budget" ]; do
    if ! kill -0 "$pid" 2>/dev/null; then
      wait "$pid"; local st2=$?
      [ "$st2" -eq 0 ] && return 0
      return 30
    fi
    sleep 1; waited=$((waited + 1))
  done
  kill -9 "$pid" 2>/dev/null; wait "$pid" 2>/dev/null
  return 20
}

# ─────────────────────────────────────────────────────────────────────────────
# КАКИМ БРАУЗЕРОМ ПОЙДУТ ПРОБЫ — печатается ВСЕГДА
# ─────────────────────────────────────────────────────────────────────────────
browser_path_pinned() {
  # Спрашиваем сам playwright, а не угадываем раскладку каталога кэша: она
  # принадлежит ему и меняется без нашего ведома.
  ${KACHO_BROWSER_PATH_CMD:-node -e 'const {chromium}=require("@playwright/test");process.stdout.write(chromium.executablePath())'} 2>/dev/null
}

browser_path_image() {
  local cand
  for cand in "$(command -v google-chrome || true)" \
              "$(command -v chromium-browser || true)" \
              "$(command -v chromium || true)"; do
    if [ -n "$cand" ] && [ -x "$cand" ]; then printf '%s' "$cand"; return 0; fi
  done
  return 1
}

report_browser() {  # <путь> <происхождение>
  local path="$1" origin="$2" ver
  ver="$("$path" --version 2>/dev/null | head -1)"
  [ -n "$ver" ] || ver="версию назвать не удалось"
  echo "=== БРАУЗЕР ПРОБ: $path"
  echo "=== ВЕРСИЯ:       $ver"
  echo "=== ПРОИСХОЖДЕНИЕ: $origin"
}

# ─────────────────────────────────────────────────────────────────────────────
# ГЛАВНОЕ
# ─────────────────────────────────────────────────────────────────────────────
acquire() {
  local out="$1" envf="$2"
  local log="${RUNNER_TEMP:-/tmp}/playwright-install.log"

  echo "=== пределы: загрузка $(download_budget)с, распаковка $(unpack_budget)с (величины раздельные)"
  run_install "$log"; local rc=$?
  tail -40 "$log" 2>/dev/null || true

  local reason=""
  case "$rc" in
    0)
      [ -n "$out" ] && echo "complete=true" >> "$out"
      local p; p="$(browser_path_pinned)"
      if [ -n "$p" ] && [ -x "$p" ]; then
        report_browser "$p" "пиннутый playwright — пин ДЕЙСТВУЕТ"
        return 0
      fi
      # Установка прошла, а путь не назван: пробы пойдут неизвестно чем, и
      # молчать об этом значит вернуть ровно ту неразличимость, ради которой
      # печать и заведена.
      reason="установка прошла, но playwright не назвал путь к браузеру"
      ;;
    10) reason="ЗАГРУЗКА не уложилась в $(download_budget)с (свой предел загрузки). Раздача молчала дольше, чем playwright успевает повторить — это ВНЕШНЯЯ сторона." ;;
    20) reason="РАСПАКОВКА не уложилась в $(unpack_budget)с (свой предел распаковки). Загрузка при этом ЗАВЕРШИЛАСЬ — раздача ни при чём, разбирать надо занятость ранера." ;;
    30) reason="добыча завершилась ошибкой, уложившись в ОБА предела — виноват не предел, читайте вывод выше." ;;
    *)  reason="неизвестный исход добычи ($rc)" ;;
  esac

  echo "::warning title=Пиннутый браузер не добыт::${reason}"

  local img
  if img="$(browser_path_image)"; then
    [ -n "$envf" ] && echo "KACHO_CHROMIUM=$img" >> "$envf"
    report_browser "$img" "БРАУЗЕР ОБРАЗА — пин НЕ действует (${reason})"
    echo "::warning title=Браузер взят из образа::Вердикт будет получен, но браузер НЕ тот, к которому пинится набор. Причина: ${reason}"
    return 0
  fi
  die_unmet "Пиннутый браузер не добыт (${reason}), и в образе нет ни одного браузера."
}

# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА: обе фазы обязаны называться СВОИМ именем
# ─────────────────────────────────────────────────────────────────────────────
#
# Настоящий playwright здесь недоступен и не нужен: предмет — РАЗЛИЧЕНИЕ ФАЗ.
# Заглушка воспроизводит три поведения, и заведомо малый предел обязан дать
# сообщение про ТУ фазу, в которой процесс застрял, а не про раздачу всегда.
self_test() {
  local tmp; tmp="$(mktemp -d)"
  local f=0 n=0
  ok()  { n=$((n+1)); printf '  ok   %s\n' "$1"; }
  bad() { n=$((n+1)); f=$((f+1)); printf '  FAIL %s\n' "$1"; }

  # (1) висит ДО загрузки
  printf '#!/bin/sh\necho "Downloading Chromium"\nsleep 30\n' > "$tmp/stall-download"
  # (2) загрузку завершил, висит в РАСПАКОВКЕ
  printf '#!/bin/sh\necho "Downloading Chromium"\necho "100%% of 173.9 MiB"\nsleep 30\n' > "$tmp/stall-unpack"
  # (3) всё сделал
  printf '#!/bin/sh\necho "100%% of 173.9 MiB"\necho "downloaded to /tmp/x"\n' > "$tmp/good"
  # (4) упал сам, уложившись в пределы
  printf '#!/bin/sh\necho "100%% of 173.9 MiB"\necho "boom" >&2\nexit 7\n' > "$tmp/broken"
  chmod +x "$tmp"/stall-download "$tmp"/stall-unpack "$tmp"/good "$tmp"/broken

  local rc
  KACHO_BROWSER_DOWNLOAD_BUDGET=2 KACHO_BROWSER_UNPACK_BUDGET=2 \
    KACHO_BROWSER_INSTALL_CMD="$tmp/stall-download" run_install "$tmp/l1" ; rc=$?
  if [ "$rc" = 10 ]; then ok "зависшая ЗАГРУЗКА опознана как загрузка"
  else bad "зависшая загрузка дала исход $rc (ждали 10)"; fi

  KACHO_BROWSER_DOWNLOAD_BUDGET=2 KACHO_BROWSER_UNPACK_BUDGET=2 \
    KACHO_BROWSER_INSTALL_CMD="$tmp/stall-unpack" run_install "$tmp/l2" ; rc=$?
  if [ "$rc" = 20 ]; then ok "зависшая РАСПАКОВКА опознана как распаковка, а не как раздача"
  else bad "зависшая распаковка дала исход $rc (ждали 20)"; fi

  KACHO_BROWSER_DOWNLOAD_BUDGET=5 KACHO_BROWSER_UNPACK_BUDGET=5 \
    KACHO_BROWSER_INSTALL_CMD="$tmp/good" run_install "$tmp/l3" ; rc=$?
  if [ "$rc" = 0 ]; then ok "законный близнец: успешная добыча — молчит"
  else bad "успешная добыча дала исход $rc (ждали 0)"; fi

  KACHO_BROWSER_DOWNLOAD_BUDGET=5 KACHO_BROWSER_UNPACK_BUDGET=5 \
    KACHO_BROWSER_INSTALL_CMD="$tmp/broken" run_install "$tmp/l4" ; rc=$?
  if [ "$rc" = 30 ]; then ok "собственный отказ добычи НЕ выдаётся за исчерпанный предел"
  else bad "собственный отказ дал исход $rc (ждали 30)"; fi

  # ТЕКСТ. Заведомо малый предел распаковки обязан дать сообщение ПРО РАСПАКОВКУ.
  local outtxt
  outtxt="$(KACHO_BROWSER_DOWNLOAD_BUDGET=2 KACHO_BROWSER_UNPACK_BUDGET=2 \
      KACHO_BROWSER_INSTALL_CMD="$tmp/stall-unpack" \
      KACHO_BROWSER_PATH_CMD="true" \
      acquire "" "" 2>&1)"
  case "$outtxt" in *"РАСПАКОВКА не уложилась"*) ok "текст отказа называет СВОЮ причину (распаковку)" ;;
      *) bad "текст отказа не называет распаковку: $outtxt" ;; esac
  case "$outtxt" in *"Раздача"*|*"раздача"*)
        # Слово допустимо только в форме «раздача ни при чём».
        case "$outtxt" in *"раздача ни при чём"*) ok "раздача упомянута лишь как непричастная" ;;
            *) bad "текст всё ещё обвиняет раздачу: $outtxt" ;; esac ;;
      *) ok "текст раздачу не обвиняет" ;; esac

  outtxt="$(KACHO_BROWSER_DOWNLOAD_BUDGET=2 KACHO_BROWSER_UNPACK_BUDGET=2 \
      KACHO_BROWSER_INSTALL_CMD="$tmp/stall-download" \
      KACHO_BROWSER_PATH_CMD="true" \
      acquire "" "" 2>&1)"
  case "$outtxt" in *"ЗАГРУЗКА не уложилась"*) ok "зависшая загрузка называется загрузкой" ;;
      *) bad "текст не называет загрузку: $outtxt" ;; esac

  # ПЕЧАТЬ БРАУЗЕРА — на ОБОИХ исходах.
  outtxt="$(KACHO_BROWSER_DOWNLOAD_BUDGET=5 KACHO_BROWSER_UNPACK_BUDGET=5 \
      KACHO_BROWSER_INSTALL_CMD="$tmp/good" \
      KACHO_BROWSER_PATH_CMD="printf %s /bin/echo" \
      acquire "" "" 2>&1)"
  case "$outtxt" in *"БРАУЗЕР ПРОБ: /bin/echo"*) ok "на успехе печатается ПУТЬ браузера" ;;
      *) bad "путь браузера не напечатан: $outtxt" ;; esac
  case "$outtxt" in *"ВЕРСИЯ:"*) ok "на успехе печатается ВЕРСИЯ" ;;
      *) bad "версия не напечатана: $outtxt" ;; esac
  case "$outtxt" in *"пин ДЕЙСТВУЕТ"*) ok "происхождение названо: пин действует" ;;
      *) bad "происхождение не названо: $outtxt" ;; esac

  outtxt="$(KACHO_BROWSER_DOWNLOAD_BUDGET=2 KACHO_BROWSER_UNPACK_BUDGET=2 \
      KACHO_BROWSER_INSTALL_CMD="$tmp/stall-unpack" \
      KACHO_BROWSER_PATH_CMD="true" \
      acquire "" "" 2>&1)"
  case "$outtxt" in *"пин НЕ действует"*) ok "на откате СКАЗАНО, что пин не действует" ;;
      *) bad "откат не сказал, что пин не действует: $outtxt" ;; esac

  rm -rf "$tmp"
  printf 'install-pinned-browser: утверждений %s, провалов %s\n' "$n" "$f"
  [ "$n" -ge 11 ] || { echo "ОТКАЗ: утверждений меньше объявленного — проба не исполнилась целиком." >&2; return 1; }
  [ "$f" -eq 0 ] || { echo "ОТКАЗ: различение фаз не держит собственных утверждений." >&2; return 1; }
  return 0
}

main() {
  local out="${GITHUB_OUTPUT:-}" envf="${GITHUB_ENV:-}"
  while [ $# -gt 0 ]; do
    case "$1" in
      --self-test) self_test; return $? ;;
      --github-output) out="$2"; shift 2 ;;
      --github-env) envf="$2"; shift 2 ;;
      *) echo "неизвестный аргумент: $1; допустимо: [--github-output <файл>] [--github-env <файл>] | --self-test" >&2
         return 2 ;;
    esac
  done
  acquire "$out" "$envf"
}

main "$@"
