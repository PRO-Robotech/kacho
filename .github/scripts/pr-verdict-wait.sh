#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Ожидание набора проверок запроса на слияние и вынесение вердикта.
#
# ПОЧЕМУ ЭТО ФАЙЛ, А НЕ БЛОК `run:` В WORKFLOW
#
# Решение о вердикте уже жило отдельным скриптом (`pr-verdict.py`) — ровно затем,
# чтобы его можно было проверить, не гоняя конвейер. ОЖИДАНИЕ осталось в YAML, и
# дефект сел именно туда: провайдер исполняет `run:` через `bash -e`, а `set -e`
# превращает КОД ВОЗВРАТА В ОТКАЗ. Для этого цикла коды возврата — ДАННЫЕ:
# 3 означает «проверки ещё идут», то есть состояние, а не отказ. Под `-e` шелл
# умирал на первом же таком коде, не дойдя до `rc=$?`, — цикл не делал второго
# захода никогда, и задание падало за шесть секунд при объявленном пределе в 90
# минут (#1073).
#
# Три исхода обязаны различаться, и путать их нельзя:
#
#   всё зелено      → 0, выходим успехом;
#   есть красное    → 1, выходим СРАЗУ: вердикт уже определён, ждать нечего;
#   ещё идут        → НЕ вердикт: спим и спрашиваем снова.
#
# Задание стартует без зависимостей, поэтому в момент первого опроса незавершённые
# проверки есть ВСЕГДА. Третий исход, прочитанный как отказ, красит вердикт на
# каждом запросе слияния — то самое «красное, которое есть всегда», против
# которого этот процесс и заведён.
#
# `set -e` здесь НЕ включается сознательно; каждая команда, чей исход что-то
# значит, проверяется явно.
set -uo pipefail

: "${REPO:?REPO не задан}"
: "${SHA:?SHA не задан — у события нет запроса слияния, и опрос ушёл бы по адресу без коммита}"
: "${SELF:=}"
# База запроса решает, какие процессы запрос запускает, то есть что свод обязан
# дождаться (#2910). Пустая база здесь не отказ: её отвергает решатель кодом 2,
# и отказ приходит вердиктом «не вынесен» с поимённой причиной.
: "${BASE:=}"

# Ручки существуют РАДИ ПРОВЕРЯЕМОСТИ: проба подставляет свои получатель и
# решатель и сводит интервал к нулю. Умолчания — боевые.
: "${VERDICT_ATTEMPTS:=170}"
: "${VERDICT_INTERVAL:=20}"
: "${VERDICT_FETCH_CMD:=}"
: "${VERDICT_DECIDE_CMD:=}"

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Набор читается ЦЕЛИКОМ, постранично: `per_page=100` — потолок провайдера, и
# одна страница — это первая сотня, а не набор. Одной страницей этот скрипт
# читал до #3123, и на голове PR #3122 (135 проверок) страж ниже честно отказывал
# в вердикте на КАЖДОМ опросе: недочитывал не провайдер, а сам скрипт.
#
# Страницы запрашиваются, пока очередная не окажется неполной: неполная —
# последняя. Сколько страниц должно быть, здесь не решается — решает сверка
# `total_count` с прочитанным ниже, и оборванная страница остаётся громким
# отказом, а не зелёным над подмножеством.
VERDICT_PER_PAGE=100
: "${VERDICT_MAX_PAGES:=50}"

fetch_all_pages() {
  rm -f runs.page.*.json
  local page=1 got
  while [ "$page" -le "$VERDICT_MAX_PAGES" ]; do
    gh api "repos/$REPO/commits/$SHA/check-runs?per_page=$VERDICT_PER_PAGE&page=$page" \
      > "runs.page.$page.json" || return 1
    got="$(python3 -c '
import json,sys
p = json.load(open(sys.argv[1]))
print(len(p.get("check_runs") or []) if isinstance(p, dict) else -1)
' "runs.page.$page.json")" || return 1
    if [ "$got" -lt 0 ]; then
      echo "страница $page — не объект ответа check-runs" >&2
      return 1
    fi
    [ "$got" -lt "$VERDICT_PER_PAGE" ] && break
    page=$((page + 1))
  done
  merge_pages
}

# Склейка страниц в один ответ той же формы. Набор, менявшийся МЕЖДУ страницами
# (`total_count` разошёлся либо проверка пришла дважды из-за сдвига страниц), —
# не вердикт и не недочтение, а неудачный опрос: код 1, и цикл спросит снова.
merge_pages() {
  python3 - runs.page.*.json <<'PY'
import json, re, sys
paths = sorted(sys.argv[1:], key=lambda p: int(re.search(r"\.(\d+)\.json$", p).group(1)))
totals, runs = set(), []
for path in paths:
    page = json.load(open(path))
    totals.add(page.get("total_count"))
    runs.extend(page.get("check_runs") or [])
ids = [r.get("id") for r in runs if isinstance(r, dict) and r.get("id") is not None]
if len(totals) != 1:
    print(f"набор менялся между страницами: total_count {sorted(map(str, totals))}", file=sys.stderr)
    sys.exit(1)
if len(ids) != len(set(ids)):
    print(f"набор менялся между страницами: повторов id {len(ids) - len(set(ids))}", file=sys.stderr)
    sys.exit(1)
json.dump({"total_count": totals.pop(), "pages": len(paths), "check_runs": runs}, sys.stdout)
PY
}

fetch_checks() {
  if [ -n "$VERDICT_FETCH_CMD" ]; then
    "$VERDICT_FETCH_CMD"
  else
    fetch_all_pages
  fi
}

# Ожидаемое решатель выводит из процессов ТОЙ ЖЕ ревизии, что исполнил запрос:
# задание свода кладёт их рядом со скриптами. Пришедшие check-runs перечнем
# ожидаемого не служат — до #2910 вердикт «зелено» выносился над одной
# проверкой, потому что остальные ещё не успели появиться.
decide_verdict() {
  if [ -n "$VERDICT_DECIDE_CMD" ]; then
    "$VERDICT_DECIDE_CMD"
  else
    python3 "$here/pr-verdict.py" --self-name "$SELF" --workflows "$here/../workflows" --base "$BASE"
  fi
}

# Прочитанное — не обязательно весь набор. Если склеенных проверок меньше, чем
# объявляет `total_count` (страница оборвана, страниц больше предела), вердикт
# вынесся бы по подмножеству: недостающие невидимы, и «все зелены» было бы
# произнесено над набором, который никто целиком не читал. Это ровно тот класс,
# который этот процесс и стережёт, поэтому недочтение — не «зелено», а громкий
# отказ. Страж стоит и после постраничного чтения (#3123): он судит не форму
# запроса, а прочитанное.
payload_is_whole() {
  python3 -c '
import json,sys
try:
    p = json.load(sys.stdin)
except Exception as exc:
    print(f"вход не разобран как JSON: {exc}", file=sys.stderr); sys.exit(2)
if isinstance(p, dict):
    total, got = p.get("total_count"), len(p.get("check_runs") or [])
    if isinstance(total, int) and total > got:
        print(f"прочитана только страница: проверок {total}, получено {got}", file=sys.stderr)
        sys.exit(1)
sys.exit(0)
' < runs.json
}

for i in $(seq 1 "$VERDICT_ATTEMPTS"); do
  if ! fetch_checks > runs.json 2> api.err; then
    echo "::warning::опрос не удался (заход $i): $(tail -1 api.err)"
    sleep "$VERDICT_INTERVAL"
    continue
  fi

  if ! whole_err="$(payload_is_whole 2>&1)"; then
    echo "::error::набор проверок прочитан НЕ ЦЕЛИКОМ ($whole_err) — вердикта НЕТ, и это не «зелено»"
    exit 1
  fi

  # Код возврата — ДАННЫЕ. `&& rc=0 || rc=$?` ставит вызов в условный контекст,
  # поэтому даже под включённым `-e` у вызывающего он не обрывает работу.
  decide_verdict < runs.json && rc=0 || rc=$?

  case "$rc" in
    0) exit 0 ;;                        # зелено
    1) exit 1 ;;                        # красно — решает сразу, ждать нечего
    3) sleep "$VERDICT_INTERVAL" ;;     # ещё идут — НЕ вердикт, спрашиваем снова
    *) echo "::error::вердикт не вынесен (код $rc) — это НЕ «зелено»"; exit 1 ;;
  esac
done

echo "::error::проверки не завершились за отведённое время ($VERDICT_ATTEMPTS заходов) — вердикта НЕТ, и это не «зелено»"
exit 1
