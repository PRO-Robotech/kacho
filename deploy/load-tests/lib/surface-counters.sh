#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# surface-counters.sh — съём величин с поверхностей участников прогона нагрузки.
# Файл СОРСИТСЯ приборами load-tests/*-rps-step.sh.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАЧЕМ ОТДЕЛЬНЫЙ ФАЙЛ (задача #2171)
#
# Приборы шли к поверхности величин ВЫПИСАННЫМ адресом — `http://127.0.0.1:9095`
# через `wget` в образе самой службы — и не читали ни порта, ни схемы из
# объявления сбора пода. Слушатель под TLS (боевые профили) им недоступен by
# construction, а отказ был частично ТИХИМ: счётчики проверки доступа печатали
# `NA` без причины, а снимок пула соединений не печатал НИЧЕГО — разложение цены
# просто исчезало из снимка, и прогон продолжался с числом, у которого нет
# половины.
#
# Теперь:
#   • адрес — из объявления сбора ЭТОГО пода, одной реализацией на дерево
#     (deploy/scripts/lib/scrape-surface.sh — её же читает гейт поверхностей);
#   • «величин не прочитано» — ЧИСЛО: каждая строка съёма несёт
#     `surfaces_read=<прочитано>/<всего>`, снимок пула — строку переписи и
#     причину по каждому непрочитанному поду;
#   • способность этого всего покраснеть доказывается без стенда —
#     load-tests/surface-counters-inject.sh (kubectl подменяется заглушкой).
#
# Опции оболочки здесь НЕ выставляются: файл подключают приборы со своими `set`.

# shellcheck source=deploy/scripts/lib/scrape-surface.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/../../scripts/lib" && pwd)/scrape-surface.sh"

# surface_read <ns> <pod> — прочитать поверхность пода. 0 — прочитано (тело в
# SCRAPE_BODY), 1 — нет, причина в SURFACE_WHY.
surface_read() {
  SURFACE_WHY=""
  scrape_decl_pod "$1" "$2"
  if ! scrape_resolve; then SURFACE_WHY="$2: $SCRAPE_REFUSAL"; return 1; fi
  if [ -z "$SCRAPE_PROBE_POD" ]; then SURFACE_WHY="$2: под-пробник не поднят — опрашивать нечем"; return 1; fi
  scrape_fetch
  # SCRAPE_CODE / SCRAPE_BODY назначает scrape_fetch (scripts/lib/scrape-surface.sh).
  # shellcheck disable=SC2153
  if [ "$SCRAPE_CODE" != 200 ] || [ -z "$SCRAPE_BODY" ]; then
    SURFACE_WHY="$2: ответ $SCRAPE_CODE по $SCRAPE_URL (схема — $SCRAPE_SCHEME_SOURCE)"
    return 1
  fi
  return 0
}

# sum_series <тело> <ERE имени ряда> [формат] — сумма значений рядов.
#
# ЧИСЛОВАЯ ЛОКАЛЬ — `C`, И ЭТО НЕ ОФОРМЛЕНИЕ. Экспозиция величин пишет десятичную
# ТОЧКУ; `awk` под локалью с десятичной запятой читает `0.5` как ноль и печатает
# `0,000000` — сумма времени проверок молча становилась нулём. Поймано пробой
# surface-counters-inject.sh на машине с такой локалью; прежняя редакция прибора
# делала то же самое.
sum_series() {
  LC_ALL=C awk -v re="$2" -v fmt="${3:-%.0f}" '$0 ~ re {t += $2} END {printf fmt, t + 0}' <<<"$1"
}

# counters <ns> <перечень подов> <метка=ERE[:формат]>… — суммы по всем подам и
# перепись. Непрочитанный под делает суммы NA (половина слагаемых — не число), но
# перепись `surfaces_read=r/t` и причина печатаются ВСЕГДА: «не измерено»
# отличимо от «не потратило», и видно, СКОЛЬКО не измерено.
counters() {
  local ns="$1" pods="$2" p spec label re fmt read=0 total=0 why=""
  shift 2
  declare -A acc=()
  for p in $pods; do
    total=$((total + 1))
    if surface_read "$ns" "$p"; then
      read=$((read + 1))
      for spec in "$@"; do
        label="${spec%%=*}"; re="${spec#*=}"; fmt="%.0f"
        case "$re" in *:%*) fmt="${re##*:}"; re="${re%:*}" ;; esac
        acc[$label]="$(LC_ALL=C awk -v a="${acc[$label]:-0}" -v b="$(sum_series "$SCRAPE_BODY" "$re" "$fmt")" -v f="$fmt" 'BEGIN{printf f, a + b}')"
      done
    else
      why="$why; $SURFACE_WHY"
    fi
  done
  local out=""
  for spec in "$@"; do
    label="${spec%%=*}"
    if [ "$total" -gt 0 ] && [ "$read" -eq "$total" ]; then out="$out $label=${acc[$label]}"
    else out="$out $label=NA"; fi
  done
  printf '%s surfaces_read=%s/%s' "${out# }" "$read" "$total"
  [ -n "$why" ] && printf ' unread="%s"' "${why#; }"
  printf '\n'
}

# series_lines <ns> <перечень подов> <ERE> — строки рядов с именем пода впереди
# и перепись в конце. Непрочитанный под называется строкой `# <под>: …`.
series_lines() {
  local ns="$1" pods="$2" re="$3" p read=0 total=0
  for p in $pods; do
    total=$((total + 1))
    if surface_read "$ns" "$p"; then
      read=$((read + 1))
      grep -E "$re" <<<"$SCRAPE_BODY" | sed "s|^|$p |" || true
    else
      echo "# величины НЕ ПРОЧИТАНЫ — $SURFACE_WHY"
    fi
  done
  echo "# поверхностей прочитано $read из $total"
}
