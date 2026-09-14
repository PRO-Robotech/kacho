#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# declared-verdicts-census-inject.sh — НАСТОЯЩЕЕ тело шага переписи исходов,
# исполненное, по ТРИ прогона на каждую тронутую ось.
#
# ДОМ — `deploy/scripts/`, рядом с остальными доказательствами инъекцией, а не
# `.github/scripts/`. Там действует сплошное правило «каждый файл обязан зваться
# ТЕКСТОМ из конвейера» (`internal/repohygiene/artifactgates`
# TestEveryToolScriptGateIsInvoked), а перечень доказательств инъекцией ВЫВОДИТСЯ из
# дерева (`git ls-files` по форме имени) — то есть текстового вызывающего у него нет
# и быть не должно. Попытка положить файл туда дала красноту ровно с этим текстом.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЗАЧЕМ ОТДЕЛЬНО ОТ `--self-test` ВЛАДЕЛЬЦА
#
# Самопроверка владельца (`assert-declared-verdicts-ran.py --self-test`) судит его
# ЛОГИКУ на синтетическом объявлении. Она не говорит ничего о том, ИСПОЛНИМ ли
# вызов из тела шага: вызов бывает написан и неисполним — не тот путь к
# объявлению, не то имя работы, потерянный `--`, массив аргументов, который под
# `set -u` роняет оболочку на пустом значении. Такой шаг покраснел бы кодом
# самого владельца или оболочки, и это выглядело бы как находка о дереве.
#
# Класс уже ловили здесь тем же способом — звено 6
# `deploy/scripts/assert-stand-precondition-wiring.py`: тело шага подъёма
# исполняется по-настоящему, а подменяются ровно два факта.
#
# ─────────────────────────────────────────────────────────────────────────────
# ЧТО ПОДМЕНЯЕТСЯ, А ЧТО ИСПОЛНЯЕТСЯ НАСТОЯЩИМ
#
# Тело шага берётся из РАЗОБРАННОГО объявления процесса и исполняется как есть,
# под оболочкой конвейера (`defaults.run.shell: bash` — это
# `bash --noprofile --norc -eo pipefail`). Подменяются только те факты, что
# приезжают в шаг окружением: перепись исходов шагов прогона, значение отметки и
# адрес внешнего стенда. Один факт на случай — иначе неизвестно, что дало исход.
#
# ТРИ ПРОГОНА НА ОСЬ, И ТРЕТИЙ НЕСУЩИЙ:
#   1. КОНТРОЛЬ          — отметки нет, объявленное исполнилось → код 0;
#   2. ВНЕСЁН ДЕФЕКТ     — отметка есть, объявленное пропущено → код 1, и текст
#                          называет причину несозданным условием;
#   3. ЗАКОННЫЙ БЛИЗНЕЦ  — отметки нет, объявленное ИСПОЛНИЛОСЬ и вердикт
#                          КРАСНЫЙ → перепись молчит (код 0). Без этой половины
#                          правка съедала бы настоящую красноту продукта,
#                          выдавая её за «условие не создано».
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT" || exit 2

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
export RUNNER_TEMP="$WORK"

pass=0; fail=0
ok()  { pass=$((pass + 1)); printf '  ОК  %s\n' "$*"; }
bad() { fail=$((fail + 1)); printf '  ПРОВАЛ %s\n' "$*"; }

# body <файл> <работа> <id шага> — тело шага из РАЗОБРАННОГО объявления.
body() {
  python3 - "$1" "$2" "$3" <<'PY'
import sys, yaml
wf, job, sid = sys.argv[1:4]
doc = yaml.safe_load(open(wf, encoding="utf-8")) or {}
for st in (doc.get("jobs") or {}).get(job, {}).get("steps") or []:
    if str(st.get("id") or "") == sid:
        sys.stdout.write(str(st.get("run") or ""))
        sys.exit(0)
sys.exit(f"шага {sid} нет в {wf}/{job} — подмена молча не состоялась бы")
PY
}

# census_steps <работа> <исход гасимых> [id-исключение=исход] — перепись прогона,
# ВЫВЕДЕННАЯ из объявления: гасимые шаги получают заданный исход, остальные —
# success. Выписанный список разошёлся бы с объявлением молча.
census_steps() {
  python3 - "$@" <<'PY'
import json, sys, yaml
wf, job, gated_state = sys.argv[1:4]
override = dict(kv.split("=", 1) for kv in sys.argv[4:])
doc = yaml.safe_load(open(wf, encoding="utf-8")) or {}
out = {}
for st in (doc.get("jobs") or {}).get(job, {}).get("steps") or []:
    sid = str(st.get("id") or "")
    if not sid:
        continue
    state = gated_state if "STAND_PRECONDITION_UNMET" in str(st.get("if") or "") else "success"
    state = override.get(sid, state)
    out[sid] = {"outcome": state, "conclusion": state}
print(json.dumps(out, ensure_ascii=False))
PY
}

# run_body — исполняет тело под оболочкой конвейера; код и вывод кладёт рядом.
run_body() { # <тело> <перепись> <отметка> <адрес внешнего стенда>
  : > "$WORK/out"
  STEPS_JSON="$2" UNMET="$3" GIVEN_URL="$4" VERDICT_UNMET="" \
    bash --noprofile --norc -eo pipefail -c "$1" > "$WORK/out" 2>&1
  printf '%s' "$?" > "$WORK/rc"
}

axis() { # <файл> <работа> <сколько объявлено> <id, который упадёт на третьем прогоне>
  local wf="$1" job="$2" want_n="$3" red_step="$4"
  local b; b="$(body "$wf" "$job" verdict-census)" || { bad "$wf/$job: тело шага не извлечено"; return; }
  printf '\n=== ось: %s / работа «%s» ===\n' "$wf" "$job"

  # (1) КОНТРОЛЬ — без него всякое «нашёл» ниже ничего не значит.
  run_body "$b" "$(census_steps "$wf" "$job" success)" "" ""
  if [ "$(cat "$WORK/rc")" = 0 ] \
     && grep -q "объявлено вердиктных шагов $want_n, исполнилось $want_n" "$WORK/out"; then
    ok "контроль: объявленное исполнилось ($want_n из $want_n) → код 0"
  else
    bad "контроль: код $(cat "$WORK/rc")"; sed 's/^/        /' "$WORK/out" | tail -12
  fi

  # (2) ВНЕСЁН ДЕФЕКТ: отметка выставлена, гасимые шаги пропущены.
  run_body "$b" "$(census_steps "$wf" "$job" skipped)" "1" ""
  if [ "$(cat "$WORK/rc")" = 1 ] \
     && grep -q 'УСЛОВИЕ НЕ СОЗДАНО' "$WORK/out" \
     && grep -q 'вердикта о продукте нет' "$WORK/out" \
     && grep -q "объявлено вердиктных шагов $want_n, исполнилось 0" "$WORK/out"; then
    ok "отметка выставлена → код 1, причина названа несозданным условием"
  else
    bad "отметка выставлена: код $(cat "$WORK/rc") (ждали 1)"; sed 's/^/        /' "$WORK/out" | tail -12
  fi

  # (3) ЗАКОННЫЙ БЛИЗНЕЦ: прогон исполнен, вердикт КРАСНЫЙ. Перепись обязана
  #     молчать — иначе она съест красноту продукта и выдаст её за «не выполнилось».
  run_body "$b" "$(census_steps "$wf" "$job" success "$red_step=failure")" "" ""
  if [ "$(cat "$WORK/rc")" = 0 ] && grep -q 'Объявленное исполнилось целиком' "$WORK/out" \
     && ! grep -q 'УСЛОВИЕ НЕ СОЗДАНО' "$WORK/out"; then
    ok "законный близнец: «$red_step» упал, но исполнился → перепись молчит (код 0)"
  else
    bad "законный близнец: код $(cat "$WORK/rc") (ждали 0)"; sed 's/^/        /' "$WORK/out" | tail -12
  fi
}

echo "=== declared-verdicts-census-inject.sh: настоящие тела шагов, по три прогона ==="
axis .github/workflows/console-e2e.yml probes 6 probes
axis .github/workflows/ci.yaml         helm   5 manifest-tests

# ─── ЧЕТВЁРТЫЙ ПРОГОН ОДНОЙ ОСИ: ЗАКОННЫЙ ПРОПУСК, НАЗВАННЫЙ ВЫЗЫВАЮЩИМ ──────
# Внешний стенд: два гасимых шага консоли несут ещё одну оговорку — «свой
# подъём», — и при заданном адресе законно не исполняются. Проверяются ОБЕ
# половины: с адресом перепись прощает, без адреса — нет. Без второй половины
# ключ `--legit-skip` стал бы маской.
printf '\n=== ось: законный пропуск при внешнем стенде ===\n'
CB="$(body .github/workflows/console-e2e.yml probes verdict-census)"
EXT="$(census_steps .github/workflows/console-e2e.yml probes success \
        stand_revision=skipped pools=skipped stand=skipped)"
run_body "$CB" "$EXT" "" "http://console.example"
if [ "$(cat "$WORK/rc")" = 0 ] && grep -q 'пропущен законно' "$WORK/out"; then
  ok "адрес задан → оговорка названа, пропуск прощён (код 0)"
else
  bad "внешний стенд: код $(cat "$WORK/rc") (ждали 0)"; sed 's/^/        /' "$WORK/out" | tail -12
fi
run_body "$CB" "$EXT" "" ""
if [ "$(cat "$WORK/rc")" = 1 ] && grep -q 'результата не дал' "$WORK/out"; then
  ok "адреса нет → та же перепись красная: прощение не растекается"
else
  bad "без адреса: код $(cat "$WORK/rc") (ждали 1)"; sed 's/^/        /' "$WORK/out" | tail -12
fi

# ─── ОСЬ РАЗМЕТЧИКА ИСХОДА: ТРИ КАТЕГОРИИ РАЗЛИЧИМЫ ТЕКСТОМ, А НЕ ТОЛЬКО ЦВЕТОМ
# Два красных обязаны читаться по-разному: «условие не создано» и «пробы упали».
# Иначе отладка идёт в продукт там, где сломалась подготовка.
printf '\n=== ось: разметчик исхода (настоящее тело шага «category») ===\n'
CAT="$(body .github/workflows/console-e2e.yml probes category)"
run_body "$CAT" "$(census_steps .github/workflows/console-e2e.yml probes success)" "" ""
if [ "$(cat "$WORK/rc")" = 0 ] && grep -q 'ЗЕЛЕНО' "$WORK/out"; then
  ok "контроль: всё исполнилось → ЗЕЛЕНО, код 0"
else
  bad "разметчик, контроль: код $(cat "$WORK/rc")"; sed 's/^/        /' "$WORK/out" | tail -12
fi
run_body "$CAT" "$(census_steps .github/workflows/console-e2e.yml probes skipped)" "1" ""
if [ "$(cat "$WORK/rc")" = 1 ] && grep -q 'НЕ ВЫПОЛНИЛОСЬ' "$WORK/out"; then
  ok "отметка выставлена → НЕ ВЫПОЛНИЛОСЬ, код 1 (прежде был 0 при зелёной работе)"
else
  bad "разметчик, отметка: код $(cat "$WORK/rc") (ждали 1)"; sed 's/^/        /' "$WORK/out" | tail -12
fi
run_body "$CAT" "$(census_steps .github/workflows/console-e2e.yml probes success probes=failure)" "" ""
if [ "$(cat "$WORK/rc")" = 0 ] && grep -q 'КРАСНОЕ' "$WORK/out" \
   && ! grep -q 'НЕ ВЫПОЛНИЛОСЬ' "$WORK/out"; then
  ok "законный близнец: пробы упали → КРАСНОЕ с ДРУГИМ текстом, код 0 (краснит шаг проб)"
else
  bad "разметчик, близнец: код $(cat "$WORK/rc") (ждали 0)"; sed 's/^/        /' "$WORK/out" | tail -12
fi

printf '\nитог: прошло %d, провалено %d\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
