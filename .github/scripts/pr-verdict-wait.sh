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

# Ручки существуют РАДИ ПРОВЕРЯЕМОСТИ: проба подставляет свои получатель и
# решатель и сводит интервал к нулю. Умолчания — боевые.
: "${VERDICT_ATTEMPTS:=170}"
: "${VERDICT_INTERVAL:=20}"
: "${VERDICT_FETCH_CMD:=}"
: "${VERDICT_DECIDE_CMD:=}"
: "${VERDICT_ANNOTATIONS_CMD:=}"

# ── КАНАЛ ТРЕТЬЕЙ КАТЕГОРИИ (#958) ──────────────────────────────────────────
#
# Работа, чьё условие не создано, свою категорию УЗНАЁТ и НАЗЫВАЕТ — и сообщает
# её аннотацией с этим заголовком. Дальше она умирала на границе: до сводного
# вердикта доезжает только заключение check-run, и «не выполнилось» приходило
# неотличимым от настоящего красного. Читающий не мог отличить «продукт сломан»
# от «стенд не поднялся», а лечатся они противоположным.
#
# Здесь эта метка ЧИТАЕТСЯ. Цвет от неё не меняется — проверка остаётся
# блокирующей (решение владельца 2026-09-12), — но вердикт печатает её ОТДЕЛЬНОЙ
# СТРОКОЙ и отдельным перечнем.
#
# Метка одна на всё дерево; перепись «кто различает и кто умеет сообщить» её же
# и считает (.github/scripts/third-category-census.py), и она же падает, если
# метка перестанет здесь искаться: число, пережившее своего читателя, означало
# бы то, чего нет.
UNMET_ANNOTATION_TITLE="НЕ ВЫПОЛНИЛОСЬ"

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

fetch_checks() {
  if [ -n "$VERDICT_FETCH_CMD" ]; then
    "$VERDICT_FETCH_CMD"
  else
    gh api "repos/$REPO/commits/$SHA/check-runs?per_page=100"
  fi
}

# Аннотации ОДНОЙ проверки. Спрашиваются только у НЕ-ЗЕЛЁНЫХ завершившихся: у
# зелёной третьей категории быть не может by construction, а лишний запрос на
# каждую проверку — это десятки обращений на каждый заход цикла.
fetch_annotations() { # <id проверки>
  if [ -n "$VERDICT_ANNOTATIONS_CMD" ]; then
    "$VERDICT_ANNOTATIONS_CMD" "$1"
  else
    gh api "repos/$REPO/check-runs/$1/annotations?per_page=100"
  fi
}

# collect_unmet — имена проверок, чья работа САМА объявила третью категорию.
#
# Отказ опроса НЕ считается «не выполнилось»: тогда имя просто не попадёт в
# перечень, и проверка останется в числе красных ровно как прежде. Неизвестное
# обязано быть красным, а не прощённым.
collect_unmet() { # <файл набора> <куда писать имена>
  : > "$2"
  python3 -c '
import json, sys
try:
    p = json.load(open(sys.argv[1], encoding="utf-8"))
except Exception:
    sys.exit(0)
runs = p.get("check_runs", p) if isinstance(p, dict) else p
for r in runs if isinstance(runs, list) else []:
    if not isinstance(r, dict):
        continue
    if str(r.get("status")) != "completed" or str(r.get("conclusion")) == "success":
        continue
    rid, name = r.get("id"), r.get("name")
    if rid is None or not name:
        continue
    print(f"{rid}\t{name}")
' "$1" | while IFS=$'\t' read -r id name; do
    [ -n "$id" ] || continue
    ann="$(fetch_annotations "$id" 2>/dev/null)" || continue
    case "$ann" in
      *"$UNMET_ANNOTATION_TITLE"*) printf '%s\n' "$name" >> "$2" ;;
    esac
  done
}

decide_verdict() {
  if [ -n "$VERDICT_DECIDE_CMD" ]; then
    "$VERDICT_DECIDE_CMD"
  else
    collect_unmet runs.json unmet.txt
    python3 "$here/pr-verdict.py" --self-name "$SELF" --unmet-names-file unmet.txt
  fi
}

# Страница — не весь набор. `per_page=100` отдаёт ПЕРВУЮ сотню, и если проверок
# больше, вердикт выносится по подмножеству: недостающие невидимы, и «все зелены»
# может быть произнесено над набором, который никто целиком не читал. Это ровно
# тот класс, который этот процесс и стережёт, поэтому усечение — не «зелено», а
# громкий отказ. Обязательных контекстов у ствола уже 44, всего проверок 51.
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
