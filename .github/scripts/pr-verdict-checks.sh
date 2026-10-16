#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Чтение набора проверок головы целиком — ОДНО место для обоих сводов: опроса
# (`pr-verdict-wait.sh`, переходный период) и события (`pr-verdict-event.sh`,
# kacho#3266). Две копии постраничного чтения разошлись бы молча, а недочтение
# здесь — ложное зелёное в том самом месте, что стережёт ложное зелёное.
#
# Файл подключается (`source`), а не исполняется. Ждёт `REPO` и `SHA`; пишет
# `runs.page.*.json` в рабочий каталог вызывающего.

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
