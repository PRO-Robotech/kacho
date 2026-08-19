#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# install-browser-deps.sh — системные пакеты браузера: из кэша, а не из сети.
#
# ПРЕДМЕТ (#726). Шаг `install-deps` не уложился в свои пять минут, и джоба
# сквозных проб консоли покраснела ДО всяких проб. В сводке запроса на слияние
# это неотличимо от настоящей красноты: «инфраструктура не успела» читается как
# «продукт сломан», а двадцать пять минут стенда потрачены впустую.
#
# ЗАМЕР по журналу прогона 32197604354 называет виновника точно:
#
#   пакетов в списке playwright             33
#   из них уже стоит в образе ранера        24
#   ставится заново (все — шрифты)           9
#   объём загрузки                      21.1 МБ
#   индексы apt                    11.3 МБ за 1 с (8958 кБ/с)
#   загрузка девяти пакетов     начата, строки «Fetched» НЕТ
#
# Индекс приходит за секунду, а двадцать мегабайт не приходят за четыре с
# половиной минуты. Значит это не «медленное зеркало» в смысле пропускной
# способности, а ЗАВИСШЕЕ соединение — и предел шага его только переименовывает:
# на зависании не хватит и десяти минут.
#
# ЧТО ДЕЛАЕТ ЭТОТ СКРИПТ
#
#   1. Спрашивает СПИСОК пакетов у самого playwright (`--dry-run`, сети не
#      требует). Выписанный здесь список разошёлся бы с пакетом молча при первом
#      же его обновлении — и разошёлся бы там, где расхождение не видно: браузер
#      запустился бы, но рисовал не тем.
#   2. Спрашивает у dpkg, чего из списка НЕ ХВАТАЕТ. Не хватает ничего — выходит,
#      не обратившись в сеть вовсе.
#   3. На попадании в кэш ставит .deb из каталога и в сеть не идёт.
#   4. На промахе идёт в сеть ОГРАНИЧЕННО: ожидание сокета 20 с вместо
#      стандартных 120, три попытки, свой предел на каждую команду.
#   5. ПОСЛЕ установки ПРОВЕРЯЕТ полноту тем же опросом dpkg — и только успех
#      этого опроса выставляет `complete=true`, который единственный открывает
#      сохранение в кэш.
#
# ПОЧЕМУ ПУНКТ 5 НЕСУЩИЙ. Кэш, сохранивший НЕПОЛНУЮ установку, маскирует шаг
# навсегда: следующий прогон получает попадание, пропускает установку и падает
# позже и в другом месте, а ключ стабилен и сам не истечёт. Этот класс здесь уже
# наблюдался — на добыче браузера (`testing.md` §«Кэш, сохранивший неудачу»).
# Поэтому полнота не предполагается, а проверяется.
#
# ОТКАЗ ЭТОГО СКРИПТА — «УСЛОВИЕ НЕ СОЗДАНО», а не красные пробы, и он говорит
# это вслух: категорию читает `console-run-category.py` и печатает её в сводку.
#
# Запуск:
#   install-browser-deps.sh --cache-dir <каталог> [--github-output <файл>]
#   install-browser-deps.sh --self-test

set -euo pipefail

die_unmet() {
  # Отказ подготовки обязан называть себя третьей категорией: иначе читатель
  # сводки уходит разбирать продукт там, где сломалось условие.
  echo "::error title=Условие не создано::$1 Это НЕ вердикт проб: они не выполнялись вовсе, и такой исход не зачитывается ни в зелёное, ни в красное."
  exit 1
}

# ─────────────────────────────────────────────────────────────────────────────
# УСТАНОВКА
# ─────────────────────────────────────────────────────────────────────────────
install_deps() {
  local dir="$1" out="$2"
  local line pkgs total miss n

  line=$(npx playwright install-deps --dry-run chromium)
  # Разбор печатает готовую команду одной строкой; список — всё после ключа
  # `--no-install-recommends` до закрывающей кавычки.
  pkgs=$(printf '%s' "$line" | sed -n 's/.*--no-install-recommends //p' | tr -d '"')
  if [ -z "$pkgs" ]; then
    die_unmet "playwright не назвал список системных пакетов. Его вывод: ${line}."
  fi
  total=$(printf '%s\n' $pkgs | wc -l)
  echo "=== в списке playwright пакетов: ${total}"

  # Установлен ли пакет. Сравнение БЕЗ внешнего процесса и без трубы: `grep -q`
  # выходит до конца входа, писатель слева получает SIGPIPE, и под `pipefail`
  # статус конвейера становится ненулевым — то есть УСТАНОВЛЕННЫЙ пакет читался
  # бы как отсутствующий. Здесь это дало бы вечную «недостачу»: каждый прогон
  # ходил бы в сеть, а после установки объявлял её неполной и отказывал.
  # Класс держит гейт `internal/repohygiene` `TestPipefailVerdictNeverComesFromAPipe`
  # (задача #658) — он же эту трубу здесь и нашёл.
  is_installed() {
    local st
    st=$(dpkg-query -W -f='${Status}' "$1" 2>/dev/null) || return 1
    [[ "$st" == *"ok installed"* ]]
  }

  # Чего из списка нет. Локальный опрос, сети не требует.
  missing() {
    local p
    for p in $pkgs; do
      if ! is_installed "$p"; then
        printf '%s ' "$p"
      fi
    done
    return 0
  }

  # Ставим из каталога ТОЛЬКО отсутствующее. Кэш мог быть собран на прежнем
  # образе ранера, и безусловный `dpkg -i` ПОНИЗИЛ БЫ версию того, что образ уже
  # несёт. Зависимости, которые apt положил в тот же каталог, при этом не
  # теряются — они тоже отсутствуют и попадут в набор.
  install_from_dir() {
    local todo="" deb name
    for deb in "$dir"/*.deb; do
      [ -e "$deb" ] || continue
      name=$(dpkg-deb -f "$deb" Package 2>/dev/null || true)
      [ -n "$name" ] || continue
      if is_installed "$name"; then
        continue
      fi
      todo="$todo $deb"
    done
    if [ -z "$todo" ]; then
      echo "  (ставить из каталога нечего — всё уже стоит)"
      return 0
    fi
    echo "  ставлю из каталога пакетов: $(printf '%s\n' $todo | wc -l)"
    sudo dpkg -i $todo
  }

  miss=$(missing)
  if [ -z "$miss" ]; then
    echo "=== недостающих нет — установка не нужна, в сеть не обращались"
    echo "complete=false" >> "$out"
    return 0
  fi
  echo "=== недостаёт пакетов: $(printf '%s\n' $miss | wc -l) —$(printf ' %s' $miss)"

  if ls "$dir"/*.deb >/dev/null 2>&1; then
    echo "=== кэш восстановлен: файлов $(ls "$dir"/*.deb | wc -l); ставлю ЛОКАЛЬНО, сеть не нужна"
    if install_from_dir; then
      miss=$(missing)
      if [ -z "$miss" ]; then
        echo "=== все пакеты на месте; в сеть не обращались"
        echo "complete=true" >> "$out"
        return 0
      fi
    fi
    echo "::warning title=Кэш системных пакетов неполон::после установки из кэша недостаёт:$(printf ' %s' $miss). Иду в сеть и пересоберу кэш — состав предустановленного в образе ранера изменился."
  else
    echo "=== кэша нет — иду в сеть"
  fi

  # ─── ПРОМАХ: СЕТЬ, НО ОГРАНИЧЕННАЯ ────────────────────────────────────────
  # У каждой команды СВОЙ предел, а не только предел шага: предел шага говорит
  # «что-то встало» и не говорит что. Тот же класс уже чинили на добыче браузера.
  echo "=== apt: индексы"
  if ! sudo timeout 180 apt-get update; then
    die_unmet "apt не обновил индексы за 180 с."
  fi

  mkdir -p "$dir"
  sudo mkdir -p "$dir/partial"
  echo "=== apt: загрузка недостающего в ${dir}"
  # `--download-only` кладёт .deb в наш каталог и НИЧЕГО не ставит: установка
  # идёт ниже тем же кодом, что и на попадании в кэш. Иначе у двух путей было бы
  # два разных исхода при одном и том же входе.
  if ! sudo timeout 420 apt-get install -y --no-install-recommends --download-only \
         -o "Dir::Cache::archives=${dir}/" \
         -o Acquire::Retries=3 \
         -o Acquire::http::Timeout=20 \
         -o Acquire::https::Timeout=20 \
         $pkgs; then
    die_unmet "apt не загрузил системные пакеты браузера за 420 с (ожидание сокета ограничено 20 с, попыток 3)."
  fi
  sudo rm -rf "$dir/partial" "$dir/lock"
  sudo chown -R "$(id -u):$(id -g)" "$dir"

  if ! install_from_dir; then
    die_unmet "dpkg не поставил загруженные пакеты."
  fi

  # ПОЛНОТА ПРОВЕРЯЕТСЯ, А НЕ ПРЕДПОЛАГАЕТСЯ — см. шапку, пункт 5.
  miss=$(missing)
  if [ -n "$miss" ]; then
    die_unmet "после установки всё ещё недостаёт:$(printf ' %s' $miss). В кэш НЕ сохраняю."
  fi
  n=$(ls "$dir"/*.deb 2>/dev/null | wc -l)
  echo "=== установка полна; в кэш уедет файлов: ${n}"
  if [ "$n" -gt 0 ]; then
    echo "complete=true" >> "$out"
  else
    echo "complete=false" >> "$out"
  fi
}

# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА: скрипт обязан пройти на исправном входе И упасть на каждом
# настоящем отказе, ни разу не объявив неполную установку полной.
# Все внешние команды подменяются заглушками — сети и root'а самопроверка не
# требует и требовать не вправе.
# ─────────────────────────────────────────────────────────────────────────────
self_test() {
  local me; me="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
  local root; root=$(mktemp -d)

  local bin="$root/bin"; mkdir -p "$bin"

  cat > "$bin/npx" <<'STUB'
#!/usr/bin/env bash
# Заглушка playwright: печатает ту же форму, что и настоящий `--dry-run`.
if [ "${STUB_PKGS:-}" = "НЕТ" ]; then echo "Cannot install dependencies"; exit 0; fi
echo "sudo -- sh -c \"apt-get update&& apt-get install -y --no-install-recommends ${STUB_PKGS}\""
STUB

  cat > "$bin/dpkg-query" <<'STUB'
#!/usr/bin/env bash
p="${!#}"
grep -qx "$p" "$STUB_STATE/installed" 2>/dev/null || exit 1
echo "install ok installed"
STUB

  cat > "$bin/dpkg-deb" <<'STUB'
#!/usr/bin/env bash
basename "${!#}" | sed 's/_.*//'
STUB

  cat > "$bin/dpkg" <<'STUB'
#!/usr/bin/env bash
echo "dpkg" >> "$STUB_STATE/calls"
[ "${STUB_DPKG_FAILS:-0}" = "1" ] && exit 1
for a in "$@"; do
  case "$a" in
    *.deb) basename "$a" | sed 's/_.*//' >> "$STUB_STATE/installed" ;;
  esac
done
exit 0
STUB

  cat > "$bin/apt-get" <<'STUB'
#!/usr/bin/env bash
echo "apt-get $1" >> "$STUB_STATE/calls"
[ "$1" = "update" ] && exit "${STUB_APT_UPDATE_RC:-0}"
[ "${STUB_APT_RC:-0}" != "0" ] && exit "${STUB_APT_RC}"
dir=""
for a in "$@"; do case "$a" in Dir::Cache::archives=*) dir="${a#Dir::Cache::archives=}";; esac; done
for p in ${STUB_APT_PROVIDES:-}; do : > "${dir}${p}_1.0_all.deb"; done
exit 0
STUB

  # `sudo` и `timeout` только снимаются: их поведение самопроверке не предмет.
  printf '#!/usr/bin/env bash\nexec "$@"\n' > "$bin/sudo"
  printf '#!/usr/bin/env bash\nshift\nexec "$@"\n' > "$bin/timeout"
  chmod +x "$bin"/*

  local passed=0 failed=0
  # Один случай: имя · код · `complete` · маркер в выводе · ходил ли в сеть.
  #
  # Последнее — не украшение: «на попадании в кэш шаг не обращается в сеть» и
  # есть предикат снятия задачи. Утверждать его надо ФАКТОМ вызова apt, а не
  # напечатанной строкой: строка переживёт любую перестановку кода.
  run_case() {
    local name="$1" want_rc="$2" want_complete="$3" want_mark="$4" want_apt="$5"; shift 5
    local case_dir; case_dir=$(mktemp -d -p "$root")
    local cache="$case_dir/cache"; mkdir -p "$cache"
    local state="$case_dir/state"; mkdir -p "$state"
    : > "$state/installed"; : > "$state/calls"
    local out="$case_dir/out" log="$case_dir/log"

    # Обстановка случая: STUB_* и предзаполнение кэша/установленного.
    STUB_STATE="$state" eval "$*"

    local rc=0
    ( export PATH="$bin:$PATH" STUB_STATE="$state" \
        STUB_PKGS="${STUB_PKGS:-}" STUB_APT_PROVIDES="${STUB_APT_PROVIDES:-}" \
        STUB_APT_RC="${STUB_APT_RC:-0}" STUB_APT_UPDATE_RC="${STUB_APT_UPDATE_RC:-0}" \
        STUB_DPKG_FAILS="${STUB_DPKG_FAILS:-0}"
      bash "$me" --cache-dir "$cache" --github-output "$out" ) > "$log" 2>&1 || rc=$?

    local complete="нет"
    grep -q '^complete=true$' "$out" 2>/dev/null && complete="true"
    grep -q '^complete=false$' "$out" 2>/dev/null && complete="false"

    local apt="нет"
    grep -q '^apt-get' "$state/calls" 2>/dev/null && apt="да"

    local ok=1
    [ "$rc" = "$want_rc" ] || ok=0
    [ "$complete" = "$want_complete" ] || ok=0
    [ "$apt" = "$want_apt" ] || ok=0
    if [ -n "$want_mark" ]; then grep -q "$want_mark" "$log" || ok=0; fi

    if [ "$ok" = 1 ]; then
      passed=$((passed + 1)); echo "  ОК  $name"
    else
      failed=$((failed + 1))
      echo "  ПРОВАЛ $name: код $rc (ждали $want_rc), complete=$complete (ждали $want_complete), apt=$apt (ждали $want_apt), маркер «$want_mark»"
      sed 's/^/      | /' "$log"
    fi
    unset STUB_PKGS STUB_APT_PROVIDES STUB_APT_RC STUB_APT_UPDATE_RC STUB_DPKG_FAILS
  }

  echo "=== самопроверка install-browser-deps.sh ==="

  # ЗАКОННЫЙ БЛИЗНЕЦ. Без него все отрицания зеленели бы на сломанном скрипте.
  run_case "всё уже стоит → сети не касаемся" 0 false "в сеть не обращались" нет \
    'STUB_PKGS="libfoo libbar"; printf "libfoo\nlibbar\n" > "$STUB_STATE/installed"'

  # Кэш закрывает предмет задачи: недостающее ставится БЕЗ сети.
  run_case "кэш полон → ставим локально, apt НЕ звался" 0 true "сеть не нужна" нет \
    'STUB_PKGS="libfoo fonts-x"; echo libfoo > "$STUB_STATE/installed"; : > "$cache/fonts-x_1.0_all.deb"'

  # Промах: идём в сеть, ставим, полнота подтверждена — кэш можно наполнять.
  run_case "кэша нет → сеть, установка полна, complete=true" 0 true "установка полна" да \
    'STUB_PKGS="libfoo fonts-x"; echo libfoo > "$STUB_STATE/installed"; STUB_APT_PROVIDES="fonts-x"'

  # Кэш собран на прежнем образе и уже не покрывает список: не молчим, а идём
  # в сеть и пересобираем. Именно здесь прежний класс возвращался бы тихо.
  run_case "кэш неполон → предупреждение и поход в сеть" 0 true "Кэш системных пакетов неполон" да \
    'STUB_PKGS="libfoo fonts-x fonts-y"; echo libfoo > "$STUB_STATE/installed";
     : > "$cache/fonts-x_1.0_all.deb"; STUB_APT_PROVIDES="fonts-y"'

  # ГЛАВНОЕ ОТРИЦАНИЕ: установка неполна ⇒ отказ И `complete` НЕ выставлен.
  # Без этого случая кэш сохранил бы неудачу и замаскировал шаг навсегда.
  run_case "установка неполна → отказ, complete НЕ true" 1 нет "после установки всё ещё недостаёт" да \
    'STUB_PKGS="libfoo fonts-x"; echo libfoo > "$STUB_STATE/installed"; STUB_APT_PROVIDES=""'

  run_case "apt не загрузил → «условие не создано»" 1 нет "Условие не создано" да \
    'STUB_PKGS="libfoo fonts-x"; echo libfoo > "$STUB_STATE/installed"; STUB_APT_RC=1'

  run_case "apt не обновил индексы → «условие не создано»" 1 нет "индексы за 180" да \
    'STUB_PKGS="libfoo fonts-x"; echo libfoo > "$STUB_STATE/installed"; STUB_APT_UPDATE_RC=1'

  run_case "dpkg не поставил → «условие не создано»" 1 нет "Условие не создано" да \
    'STUB_PKGS="libfoo fonts-x"; echo libfoo > "$STUB_STATE/installed";
     : > "$cache/fonts-x_1.0_all.deb"; STUB_DPKG_FAILS=1; STUB_APT_PROVIDES=""'

  run_case "playwright не назвал список → «условие не создано»" 1 нет "не назвал список" нет \
    'STUB_PKGS="НЕТ"'

  rm -rf "$root"
  echo "=== самопроверка: случаев $((passed + failed)), провалов ${failed} ==="
  [ "$failed" = 0 ]
}

main() {
  local cache_dir="" out="${GITHUB_OUTPUT:-/dev/null}"
  while [ $# -gt 0 ]; do
    case "$1" in
      --self-test) self_test; return $? ;;
      --cache-dir) cache_dir="$2"; shift 2 ;;
      --github-output) out="$2"; shift 2 ;;
      *)
        # Неизвестный ввод — явный отказ: опечатка в шаге конвейера иначе дала бы
        # зелёное, ничего не сделав.
        echo "неизвестный аргумент: $1; допустимо: --cache-dir <кат> [--github-output <файл>] | --self-test" >&2
        return 2 ;;
    esac
  done
  if [ -z "$cache_dir" ]; then
    echo "не назван --cache-dir" >&2
    return 2
  fi
  mkdir -p "$cache_dir"
  install_deps "$cache_dir" "$out"
}

main "$@"
