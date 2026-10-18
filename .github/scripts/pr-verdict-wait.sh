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

# Ручки существуют РАДИ ПРОВЕРЯЕМОСТИ: проба подставляет свои получатель,
# решатель, часы и перечень прогонов головы и сводит интервал к нулю. Умолчания —
# боевые.
#
# ПРЕДЕЛ — СРОКОМ, А НЕ ЧИСЛОМ ЗАХОДОВ (kacho#3299). До него стояло 170 заходов
# при интервале 20 с: заход стоит 20 с сна и ~1,2 с опроса, и предел наступал на
# 60-й минуте — раньше `timeout-minutes: 90` задания и раньше конца набора.
# Замер 2026-10-18 по check-runs четырёх голов (5a586e18, 50cf2727, 636bd543,
# af80b448), от старта свода до последней завершённой проверки: 116,0 · 112,2 ·
# 110,9 · 107,2 мин; последней кончалась «сводный вердикт юнитов (все шарды)»,
# шарды которой стояли в очереди ранеров. Все четыре свода отказали «не
# завершились за отведённое время (170 заходов)» на 60-й минуте.
#
# Срок 180 мин = наибольший замер 116 мин с запасом ~1,55 под рост очереди (на
# замере в очереди стояли пять голов разом). Число заходов зависит от длительности
# опроса, срок — только от часов. Задание обязано жить дольше срока:
# `timeout-minutes` в `required-verdict.yml` — срок плюс 10 мин на подготовку
# (замер: от старта задания до этого шага 4–6 с). Согласованность обоих чисел и
# нижнюю границу срока держит `internal/repohygiene`
# `TestVerdictWaitDeadlineOutlivesTheMeasuredSet`.
: "${VERDICT_DEADLINE:=10800}"
: "${VERDICT_INTERVAL:=20}"
: "${VERDICT_FETCH_CMD:=}"
: "${VERDICT_DECIDE_CMD:=}"
: "${VERDICT_NOW_CMD:=}"
: "${VERDICT_RUNS_CMD:=}"
# Как часто при «ещё идут» спрашивать о вытесненных прогонах, с: вытеснение
# окончательно, а лишний запрос расходует общий лимит токена.
: "${VERDICT_DISPLACED_PERIOD:=120}"

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
# Черновик (kacho#3148): выбор затронутого вычисляется ОДИН раз тем же скриптом и по
# тому же входу, что у детекторов процессов, и свод судит выбранное — красным по
# построению. Выбор не вычислен — вердикта нет, и это не «зелено».
draft_args=()
if [ "${DRAFT:-}" = "true" ] && [ -z "$VERDICT_DECIDE_CMD" ]; then
  if ! python3 "$here/affected.py" --draft-selection > draft-selection.json; then
    echo "::error::черновик: выбор затронутого не вычислен — вердикта НЕТ, и это НЕ «зелено»"
    exit 1
  fi
  echo "черновик: выбор затронутого — $(cat draft-selection.json)"
  draft_args=(--draft-selection draft-selection.json)
fi

decide_verdict() {
  if [ -n "$VERDICT_DECIDE_CMD" ]; then
    "$VERDICT_DECIDE_CMD"
  else
    python3 "$here/pr-verdict.py" --self-name "$SELF" --workflows "$here/../workflows" --base "$BASE" "${draft_args[@]}"
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

now() {
  if [ -n "$VERDICT_NOW_CMD" ]; then "$VERDICT_NOW_CMD"; else date +%s; fi
}

# ВЫТЕСНЕННЫЙ ПРОГОН (kacho#3299). Процесс с общей группой `concurrency` без
# отмены (`production-posture.yml`: «идёт один подъём, ждёт один, а новый
# вытесняет ждущего») отменяется провайдером, ещё не заведя ни одного задания.
# Его проверки на голове не появятся НИКОГДА: ждать их — значит дожидаться срока и
# отказывать «не завершились», то есть называть причиной время, когда причина —
# вытеснение. Замер 2026-10-18: на двух головах из пяти (5a586e18, 50cf2727)
# прогон posture — `cancelled`, заданий 0, оба свода ждали до предела.
#
# Перезапускать отсюда НЕ берёмся, и это выбор, а не недоделка. Группа общая на
# все ветки и держит одно ожидающее место: перезапуск на этой голове вытеснил бы
# ждущий прогон соседнего запроса, а его свод — наш, и так по кругу. И перезапуск
# требует `actions: write` токену задания, которое исполняет процессы запроса, —
# расширение поверхности ради удобства. Поэтому — отказ с названной причиной и
# адресом прогона: перезапуск делает тот, кто читает вердикт.
#
# Процесс вытеснен, если КАЖДЫЙ его прогон на голове завершён `cancelled` и не
# завёл заданий: перезапуск (новая попытка того же прогона) или ручной запуск на
# той же голове снимают признак сами.
displaced_runs() {
  if [ -n "$VERDICT_RUNS_CMD" ]; then
    "$VERDICT_RUNS_CMD" > head-runs.json || return 1
  else
    gh api "repos/$REPO/actions/runs?head_sha=$SHA&per_page=100" > head-runs.json || return 1
  fi
  python3 - head-runs.json <<'PY'
import json, os, subprocess, sys
try:
    doc = json.load(open(sys.argv[1]))
except ValueError as exc:
    print(f"перечень прогонов головы не разобран: {exc}", file=sys.stderr); sys.exit(2)
runs = doc.get("workflow_runs") if isinstance(doc, dict) else None
if not isinstance(runs, list):
    print("перечень прогонов головы не той формы", file=sys.stderr); sys.exit(2)
total = doc.get("total_count")
if isinstance(total, int) and total > len(runs):
    print(f"прогонов головы {total}, прочитано {len(runs)}", file=sys.stderr); sys.exit(2)
# Число заданий отменённой попытки больше не меняется: кэш по (прогон, попытка)
# бережёт общий лимит запросов токена.
cache_path = "head-runs.jobs-cache.json"
try:
    cache = json.load(open(cache_path))
except (OSError, ValueError):
    cache = {}


def jobs_total(r):
    if isinstance(r.get("jobs_total"), int):
        return r["jobs_total"]
    key = f'{r.get("id")}:{r.get("run_attempt")}'
    if key in cache:
        return cache[key]
    p = subprocess.run(
        ["gh", "api", f'repos/{os.environ["REPO"]}/actions/runs/{r.get("id")}/jobs?per_page=1&filter=latest'],
        capture_output=True, text=True, check=False)
    if p.returncode != 0:
        return None  # не прочитано — не доказано: вытесненным не считается
    try:
        n = json.loads(p.stdout).get("total_count")
    except (ValueError, AttributeError):
        return None
    if isinstance(n, int):
        cache[key] = n
        return n
    return None


def displaced(r):
    return (r.get("status") == "completed" and r.get("conclusion") == "cancelled"
            and jobs_total(r) == 0)


by_path = {}
for r in runs:
    if isinstance(r, dict):
        by_path.setdefault(str(r.get("path") or r.get("name")), []).append(r)
for path, rs in sorted(by_path.items()):
    if all(displaced(r) for r in rs):
        last = max(rs, key=lambda x: x.get("id") or 0)
        print(f'{last.get("name") or path}\t{last.get("id")}\t{last.get("html_url") or ""}')
json.dump(cache, open(cache_path, "w"))
PY
}

started="$(now)"
displaced_at=""
i=0
while :; do
  i=$((i + 1))
  pending=0
  if ! fetch_checks > runs.json 2> api.err; then
    echo "::warning::опрос не удался (заход $i): $(tail -1 api.err)"
  else
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
      3) pending=1 ;;                     # ещё идут — НЕ вердикт, спрашиваем снова
      *) echo "::error::вердикт не вынесен (код $rc) — это НЕ «зелено»"; exit 1 ;;
    esac
  fi

  # Часы спрашиваются ОДИН раз на заход: и предел, и период вытеснения судятся
  # одним отсчётом.
  t="$(now)"
  elapsed=$((t - started))

  if [ "$pending" = 1 ] && { [ -z "$displaced_at" ] || [ $((t - displaced_at)) -ge "$VERDICT_DISPLACED_PERIOD" ]; }; then
    displaced_at="$t"
    if gone="$(displaced_runs 2> runs.err)"; then
      if [ -n "$gone" ]; then
        while IFS=$'\t' read -r name id url; do
          echo "::error::$name вытеснен — нужен перезапуск на голове: прогон $id отменён, не заведя ни одного задания, и его проверки не появятся (${url:-адрес не получен}); перезапустите его, затем этот свод — вердикта НЕТ, и это не «зелено»"
        done <<< "$gone"
        exit 1
      fi
    else
      # Непрочитанный перечень прогонов вердиктом не является: ни «вытеснен», ни
      # «нет» — следующий период спросит снова, а пределом остаётся срок.
      echo "::warning::перечень прогонов головы не прочитан (заход $i): $(tail -1 runs.err)"
    fi
  fi

  if [ $((elapsed + VERDICT_INTERVAL)) -ge "$VERDICT_DEADLINE" ]; then
    echo "::error::проверки не завершились за отведённое время (срок $((VERDICT_DEADLINE / 60)) мин, прошло $((elapsed / 60)) мин, заходов $i) — вердикта НЕТ, и это не «зелено»"
    exit 1
  fi
  sleep "$VERDICT_INTERVAL"
done
