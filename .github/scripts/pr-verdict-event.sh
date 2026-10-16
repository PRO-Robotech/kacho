#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# Сводный вердикт запроса ПО СОБЫТИЮ (kacho#3266): одно чтение набора проверок
# головы на каждое завершение процесса запроса и commit status с исходом.
#
# ПОЧЕМУ НЕ ОПРОС
#
# Опрос (`pr-verdict-wait.sh`) держал ранер всю жизнь набора: 105 ранер-часов за
# две недели, 8,4–8,9 % ранер-минут, ~40 минут слота на голову, — при общем
# пределе организации в 20 одновременных заданий, где задания стоят в очереди
# больше половины своей жизни. Событие `workflow_run: completed` приходит ровно
# тогда, когда набор МЕНЯЕТСЯ, и ранер занят секунды.
#
# ИСХОДОВ ЧЕТЫРЕ, И ТОЛЬКО ОДИН ЗЕЛЁНЫЙ
#
#   решатель 0 → success;   решатель 1 → failure;
#   решатель 3 → pending — набор ещё идёт, и это НЕ вердикт: следующее
#                завершение поднимет свод снова;
#   прочее     → error — вердикт не вынесен (вход не разобран, объявление не
#                вычислено, процесс головы вне формы).
#
# Чего опрос не знал, а событие обязано знать: ложный success здесь НЕ
# исправляется следующим заходом — следующего события может не быть. Поэтому
# всякое «не знаю» — pending или error, но никогда не success.
#
# УСЛОВИЯ БЕЗОПАСНОСТИ (у задания право statuses: write, а запрос — чужой ввод)
#
#   У1 — этот скрипт, решатель и выбор затронутого исполняются с ветки по
#        умолчанию (выкачка задания); из головы НЕ исполняется ничего: её
#        процессы читаются через API как ДАННЫЕ;
#   У2 — поля события приходят окружением, а не подстановкой в текст шага;
#        голова — ровно 40 знаков [0-9a-f], иначе адреса статуса нет;
#   У4 — файлы процессов головы: имя по basename и образцу, размер с пределом,
#        разбор — `yaml.safe_load` решателя; изменённые пути черновика — из API
#        (`affected.py`);
#   У6 — запрос ищется по голове через API; ноль или больше одного кандидата —
#        pending, а не догадка;
#   голова чужого репозитория — только pending: процессы форка исполнены по
#        процессам ФОРКА, и их зелёное ничего не доказывает.
#
# Поведение проверяется прогоном настоящего скрипта: `internal/repohygiene`
# `TestVerdictEvent*` (подставной `gh` первым в PATH, `bash -e`), сцены форка —
# `assert-workflow-run-publish-guard.py`, единица «право записи».
#
# `set -e` не включается сознательно: коды решателя — ДАННЫЕ (#1073).
set -uo pipefail

: "${REPO:?REPO не задан}"
: "${CONTEXT:?CONTEXT не задан — имя контекста статуса}"
: "${SELF_NAMES:?SELF_NAMES не заданы — собственные джобы свода, по одной в строке}"
: "${HEAD_SHA:=}"
: "${HEAD_REPO:=}"
: "${RUN_EVENT:=}"
: "${RUN_URL:=}"

# Ручки существуют РАДИ ПРОВЕРЯЕМОСТИ; умолчания — боевые.
: "${VERDICT_DECIDE_CMD:=}"
: "${VERDICT_FETCH_CMD:=}"
: "${VERDICT_DRAFT_CMD:=}"
: "${VERDICT_FETCH_RETRIES:=3}"
: "${VERDICT_RETRY_INTERVAL:=10}"
: "${VERDICT_MAX_FILE_BYTES:=524288}"
: "${VERDICT_MAX_FILES:=64}"

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR source=pr-verdict-checks.sh
source "$here/pr-verdict-checks.sh"

if ! [[ "$HEAD_SHA" =~ ^[0-9a-f]{40}$ ]]; then
  echo "::error::голова исходного прогона '${HEAD_SHA:0:64}' — не 40 знаков [0-9a-f]: адреса статуса нет"
  exit 2
fi
SHA="$HEAD_SHA"

# Статус ставится полями формы (`-f`), описание — литералом не длиннее 140 знаков.
post() {
  local state="$1" desc="$2"
  local args=(-X POST "repos/$REPO/statuses/$SHA" -f "state=$state" -f "context=$CONTEXT"
              -f "description=$desc")
  [ -n "$RUN_URL" ] && args+=(-f "target_url=$RUN_URL")
  if ! gh api "${args[@]}" > /dev/null; then
    echo "::error::статус '$state' не поставлен на $SHA"
    return 1
  fi
  echo "статус «$CONTEXT»: $state — $desc"
}

if [ "$RUN_EVENT" != "pull_request" ]; then
  echo "исходный прогон поднят событием '${RUN_EVENT:-<пусто>}', а не запросом — свод не судит"
  exit 0
fi

# Голова чужого репозитория — только pending, и ставится он ОДНИМ местом: по полю
# события и по запросу вживую.
FORK_PENDING="запрос из форка: свод по событию вердикта не выносит"
fork_pending() {
  post pending "$FORK_PENDING"
}

if [ -z "$HEAD_REPO" ] || [ "$HEAD_REPO" != "$REPO" ]; then
  echo "голова из '${HEAD_REPO:-<удалённый репозиторий>}', а не из $REPO"
  fork_pending
  exit $?
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$work" || exit 2

# ── запрос по голове (У6) и его состояние вживую (У5) ──
if ! gh api "repos/$REPO/commits/$SHA/pulls?per_page=100" > pulls.json 2> api.err; then
  echo "::warning::запросы головы не прочитаны: $(tail -1 api.err)"
  post error "запросы головы не прочитаны"
  exit $?
fi
pr="$(python3 - pulls.json "$SHA" "$REPO" <<'PY'
import json, sys
path, sha, repo = sys.argv[1:4]
try:
    items = json.load(open(path))
except ValueError:
    items = None
if not isinstance(items, list):
    print("форма")
    sys.exit(0)
hit = [p for p in items if isinstance(p, dict) and p.get("state") == "open"
       and (p.get("head") or {}).get("sha") == sha
       and ((p.get("head") or {}).get("repo") or {}).get("full_name") == repo
       and ((p.get("base") or {}).get("repo") or {}).get("full_name") == repo
       and isinstance(p.get("number"), int)]
print(hit[0]["number"] if len(hit) == 1 else f"кандидатов {len(hit)}")
PY
)"
if ! [[ "$pr" =~ ^[0-9]+$ ]]; then
  echo "запрос по голове не однозначен: $pr"
  post pending "запрос по голове не однозначен"
  exit $?
fi

if ! gh api "repos/$REPO/pulls/$pr" > pr.json 2> api.err; then
  echo "::warning::запрос #$pr не прочитан: $(tail -1 api.err)"
  post error "запрос не прочитан"
  exit $?
fi
live="$(python3 - pr.json "$SHA" "$REPO" <<'PY'
import json, re, sys
path, sha, repo = sys.argv[1:4]
p = json.load(open(path))
head, base = p.get("head") or {}, p.get("base") or {}
ref, merge, draft = base.get("ref"), p.get("merge_commit_sha"), p.get("draft")
if p.get("state") != "open" or head.get("sha") != sha:
    print("stale")
elif (head.get("repo") or {}).get("full_name") != repo or (base.get("repo") or {}).get("full_name") != repo:
    print("fork")
elif not isinstance(ref, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._/-]{0,199}", ref) \
        or not isinstance(draft, bool):
    print("form")
else:
    # Ревизии слияния нет у запроса с конфликтом: процессы тогда судятся по голове.
    rev = merge if isinstance(merge, str) and re.fullmatch(r"[0-9a-f]{40}", merge) else ""
    print(f"ok\t{'true' if draft else 'false'}\t{ref}\t{rev}")
PY
)"
IFS=$'\t' read -r kind draft base merge <<< "$live"
case "$kind" in
  ok) ;;
  stale) echo "голова запроса #$pr сменилась либо запрос закрыт — судить эту голову некому"; exit 0 ;;
  fork) fork_pending; exit $? ;;
  *) post error "запрос не разобран"; exit $? ;;
esac
if [ -z "$merge" ]; then
  echo "ревизии слияния у запроса #$pr нет (конфликт) — процессы читаются по голове"
  merge="$SHA"
fi
echo "запрос #$pr: база «$base», черновик $draft, ревизия процессов $merge"

# ── процессы ревизии слияния — данными (У1, У4) ──
#
# Каталог процессов и карта выбора читаются ОДНИМ запросом GraphQL (только
# чтение): по файлу на вызов это было бы ~16 вызовов REST на каждое событие, а у
# встроенного токена свой предел запросов на репозиторий.
tree="$work/head"
mkdir -p "$tree/.github/workflows"
# shellcheck disable=SC2016  # переменные запроса GraphQL, не шелла
query='query($owner: String!, $name: String!, $wf: String!, $map: String!) {
  repository(owner: $owner, name: $name) {
    wf: object(expression: $wf) { ... on Tree { entries { name type
      object { ... on Blob { byteSize isBinary isTruncated text } } } } }
    map: object(expression: $map) { ... on Blob { byteSize isBinary isTruncated text } }
  }
}'
if ! gh api graphql -f "query=$query" -f "owner=${REPO%%/*}" -f "name=${REPO#*/}" \
     -f "wf=$merge:.github/workflows" -f "map=$merge:.github/ci-paths.json" \
     > tree.json 2> api.err; then
  echo "::warning::процессы головы не прочитаны: $(tail -1 api.err)"
  post error "процессы головы не прочитаны"
  exit $?
fi
if ! python3 - tree.json "$tree/.github" "$VERDICT_MAX_FILE_BYTES" "$VERDICT_MAX_FILES" <<'PY'
import json, os, re, sys
from pathlib import Path
path, dest, limit, cap = sys.argv[1], Path(sys.argv[2]), int(sys.argv[3]), int(sys.argv[4])
doc = json.load(open(path))
repo = ((doc.get("data") or {}).get("repository") or {}) if isinstance(doc, dict) else {}
if not isinstance(doc, dict) or doc.get("errors"):
    sys.exit(f"ответ с ошибкой: {str(doc.get('errors') if isinstance(doc, dict) else doc)[:200]}")


def text_of(blob, what):
    if not isinstance(blob, dict) or blob.get("isBinary") or blob.get("isTruncated") \
            or not isinstance(blob.get("text"), str):
        sys.exit(f"{what}: не текст либо усечён")
    size = blob.get("byteSize")
    if not isinstance(size, int) or size > limit or len(blob["text"].encode()) > limit:
        sys.exit(f"{what}: размер {size!r} сверх предела {limit}")
    return blob["text"]


entries = ((repo.get("wf") or {}).get("entries"))
if not isinstance(entries, list):
    sys.exit("каталог процессов не прочитан")
form = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,98}\.ya?ml")
files = {}
for e in entries:
    name = e.get("name") if isinstance(e, dict) else None
    if not isinstance(name, str):
        sys.exit("элемент каталога без имени")
    if not name.endswith((".yml", ".yaml")) or e.get("type") != "blob":
        continue
    if os.path.basename(name) != name or not form.fullmatch(name):
        sys.exit(f"имя процесса вне формы: {name!r}")
    files[name] = text_of(e.get("object"), name)
if not files or len(files) > cap:
    sys.exit(f"процессов {len(files)} — вне 1..{cap}")
for name, text in files.items():
    (dest / "workflows" / name).write_text(text, encoding="utf-8")
# Карта выбора той же ревизии: решатель читает её рядом с каталогом процессов.
# Её нет на ревизии, старше карты, — тогда нет и пропусков (как у решателя);
# карта есть, но не читается — отказ.
if repo.get("map") is not None:
    (dest / "ci-paths.json").write_text(text_of(repo["map"], "ci-paths.json"), encoding="utf-8")
print(f"процессов головы {len(files)}: {', '.join(sorted(files))}")
PY
then
  post error "процессы головы вне формы"
  exit $?
fi

# ── черновик: выбор затронутого с базы, пути — из API (kacho#3148) ──
draft_args=()
if [ "$draft" = "true" ]; then
  if [ -n "$VERDICT_DRAFT_CMD" ]; then
    draft_cmd=("$VERDICT_DRAFT_CMD")
  else
    draft_cmd=(python3 "$here/affected.py" --draft-selection)
  fi
  if ! BASE="$base" PR="$pr" "${draft_cmd[@]}" > draft-selection.json; then
    post error "черновик: выбор затронутого не вычислен"
    exit $?
  fi
  draft_args=(--draft-selection "$work/draft-selection.json")
fi

# ── набор проверок головы: одно чтение, целиком ──
fetch_checks() {
  if [ -n "$VERDICT_FETCH_CMD" ]; then "$VERDICT_FETCH_CMD"; else fetch_all_pages; fi
}
read_ok=""
for i in $(seq 1 "$VERDICT_FETCH_RETRIES"); do
  if fetch_checks > runs.json 2> api.err; then read_ok=1; break; fi
  echo "::warning::набор не прочитан (заход $i): $(tail -1 api.err)"
  sleep "$VERDICT_RETRY_INTERVAL"
done
if [ -z "$read_ok" ]; then
  post error "набор проверок не прочитан"
  exit $?
fi
if ! whole_err="$(payload_is_whole 2>&1)"; then
  echo "::error::набор проверок прочитан НЕ ЦЕЛИКОМ ($whole_err)"
  post error "набор проверок прочитан не целиком"
  exit $?
fi

self_args=()
while IFS= read -r n; do
  [ -n "$n" ] && self_args+=(--self-name "$n")
done <<< "$SELF_NAMES"
decide_args=("${self_args[@]}" --workflows "$tree/.github/workflows" --base "$base" "${draft_args[@]}")
if [ -n "$VERDICT_DECIDE_CMD" ]; then
  "$VERDICT_DECIDE_CMD" "${decide_args[@]}" < runs.json && rc=0 || rc=$?
else
  python3 "$here/pr-verdict.py" "${decide_args[@]}" < runs.json && rc=0 || rc=$?
fi

case "$rc" in
  0) state=success; desc="все проверки завершились и зелены" ;;
  1) state=failure; desc="есть красное — подробности в прогоне свода" ;;
  3) state=pending; desc="проверки ещё идут" ;;
  *) state=error; desc="вердикт не вынесен" ;;
esac
post "$state" "$desc"
