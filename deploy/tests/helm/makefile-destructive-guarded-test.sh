#!/usr/bin/env bash
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
#
# КАЖДАЯ цель, которая МЕНЯЕТ кластер, обязана нести гейт цели.
#
# ─────────────────────────────────────────────────────────────────────────────
# ПОЧЕМУ ЭТО ГЕЙТ, А НЕ ОДНА ПРАВКА
#
# Единственная безвозвратно разрушающая цель файла (снос схемы IAM со всеми
# аккаунтами, проектами, пользователями и привязками) была ЕДИНСТВЕННОЙ, кто не
# зависел от гейта контекста, — при том что от него зависели все ЧИТАЮЩИЕ гейты
# рядом. Найденное глазами чинится глазами; найденное свойством надо мерить по
# всему файлу, иначе класс переживёт свою находку. Померили: незащищённых
# целей, меняющих кластер, было НЕ одна.
#
# Имена namespace, пода и базы на управляемом кластере те же, что на стенде,
# поэтому промах контекстом стоит здесь не диагностики, а данных.
#
# ЧТО СЧИТАЕТСЯ «МЕНЯЕТ КЛАСТЕР». Команда, чья цель определяется АКТИВНЫМ
# контекстом kubectl/helm и которая пишет: apply/create/delete/patch/replace/
# scale/annotate/label/taint/cordon/drain, `rollout restart`, `exec` (внутри —
# что угодно, включая psql), и helm upgrade/install/uninstall/rollback.
#
# ЧТО НЕ СЧИТАЕТСЯ, И ПОЧЕМУ ИМЕННО:
#   • `kind …` — адресуется кластеру ПО ИМЕНИ (`--name`), чужой кластер
#     недостижим по построению: kind управляет только своими;
#   • `helm template|lint|dep` — локальный рендер, кластера не касается;
#   • чтение (`get`/`logs`/`describe`/`wait`/`rollout status`/`config`/
#     `port-forward`) — не запись. Диагностика на чужом контексте нежелательна,
#     но это другой разговор и другой размер ущерба.
#
# ГЕЙТ ЧИТАЕТ ИСПОЛНЯЕМУЮ ЧАСТЬ, А НЕ ТЕКСТ. Комментарии и строковые аргументы
# echo/printf вырезаются до классификации. Иначе подсказка оператору вида
# «дальше сделай kubectl rollout restart …», напечатанная САМОЙ целью, читалась
# бы как её действие — и цель, ничего не меняющая, требовала бы гейта, а автор
# следующей правки научился бы обходить проверку переносом команды в строку.
# Ровно такая строка в этом файле есть.
#
# Офлайновая проверка (кластер не нужен), как и остальные tests/helm/*.
set -uo pipefail

SCRIPT="$(basename "$0")"
DEPLOY_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
MAKEFILE="${MAKEFILE_UNDER_TEST:-$DEPLOY_ROOT/Makefile}"

command -v python3 >/dev/null 2>&1 || { echo "FATAL: нужен python3"; exit 2; }

run_gate() { MAKEFILE="$1" python3 - "$1" <<'PY'
import os, re, sys

path = sys.argv[1]
try:
    src = open(path, encoding="utf-8").read()
except OSError as e:
    print(f"FATAL: не прочитан {path}: {e}")
    sys.exit(2)

GUARDS = ("guard-kind-context", "guard-destructive")

# ── Разбор целей ─────────────────────────────────────────────────────────────
# Цель: строка вида `name: prereqs` в начале строки (не с табуляции).
# Рецепт: последующие строки, начинающиеся с TAB, плюс строки условных блоков
# make (ifdef/ifndef/else/endif) между ними — они часть тела цели.
TARGET_RE = re.compile(r"^([A-Za-z0-9_][A-Za-z0-9_.\-]*)\s*:(?!=)\s*(.*)$")

targets = {}   # name -> {"prereqs": [...], "recipe": [...]}
order = []
cur = None
for raw in src.splitlines():
    if raw.startswith("\t"):
        if cur:
            targets[cur]["recipe"].append(raw)
        continue
    if re.match(r"^\s*(ifdef|ifndef|ifeq|ifneq|else|endif)\b", raw):
        if cur:
            targets[cur]["recipe"].append(raw)
        continue
    m = TARGET_RE.match(raw)
    if m and not raw.lstrip().startswith("#"):
        name, prereqs = m.group(1), m.group(2)
        if name.startswith("."):          # .PHONY и подобные — не цели работы
            cur = None
            continue
        if name not in targets:
            targets[name] = {"prereqs": [], "recipe": []}
            order.append(name)
        targets[name]["prereqs"] += prereqs.split()
        cur = name
        continue
    if raw.strip() == "" or raw.lstrip().startswith("#"):
        continue
    cur = None                            # присваивание переменной и т.п.

if not targets:
    print("FAIL: в Makefile не разобрано НИ ОДНОЙ цели — проверка НЕ ВЫПОЛНЕНА,")
    print("      а это не то же самое, что «нарушений нет».")
    sys.exit(1)

# ── ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ ────────────────────────────────────────
# Запрет опирается на то, что гейты называются именно так. Переименуют — и
# «нарушений не найдено» станет ложью: ни одна цель не будет считаться
# защищённой... либо, наоборот, гейт перестанет их узнавать. Пусть скажет сам.
missing_guards = [g for g in GUARDS if g not in targets]
if missing_guards:
    print(f"FAIL: в Makefile нет целей-гейтов: {', '.join(missing_guards)}.")
    print("      Предпосылка этой проверки перестала выполняться — она больше")
    print("      не может отличить защищённую цель от незащищённой.")
    sys.exit(1)


STRING_LITERAL = re.compile(r"'[^']*'|\"[^\"]*\"")


def executable_part(lines):
    """Рецепт без комментариев и без содержимого строковых литералов.

    Комментарий и подсказка оператору — это ТЕКСТ, а не действие цели.
    Классификация по сырому тексту красила бы цель за строку, которую она
    печатает, и научила бы прятать реальную команду в кавычки.

    Вырезается ВЕСЬ строковый литерал, а не «аргументы echo до разделителя»:
    первая же версия резала по `[^;&|]*` и спотыкалась о точку с запятой ВНУТРИ
    печатаемой строки — хвост подсказки «…goose state dropped. Next: kubectl
    rollout restart …» доезжал до классификатора как команда. Такая строка в
    этом Makefile есть, и она давала ложную причину; цель, которая ТОЛЬКО
    печатает подобное, получила бы ложное нарушение целиком.
    """
    out = []
    for ln in lines:
        ln = STRING_LITERAL.sub(" ", ln)                     # содержимое кавычек
        ln = re.sub(r"(?<!\$)#.*$", "", ln)                  # комментарий (не $#)
        out.append(ln)
    return "\n".join(out)


# ── Классификация «меняет кластер» ──────────────────────────────────────────
KUBECTL_WRITE = re.compile(
    r"\bkubectl\b(?![^\n;&|]*\b(get|logs|describe|explain|version|api-resources|"
    r"config|port-forward|top|auth\s+can-i)\b)"
    r"[^\n;&|]*\b(apply|create|delete|replace|patch|edit|scale|annotate|label|"
    r"taint|cordon|drain|exec|set|expose|autoscale)\b"
)
KUBECTL_ROLLOUT_WRITE = re.compile(r"\bkubectl\b[^\n;&|]*\brollout\s+(restart|undo|pause|resume)\b")
HELM_WRITE = re.compile(r"\bhelm\s+(upgrade|install|uninstall|delete|rollback)\b")


def mutating_reasons(body):
    hits = []
    for pat, what in ((KUBECTL_WRITE, "kubectl-запись"),
                      (KUBECTL_ROLLOUT_WRITE, "kubectl rollout restart/undo"),
                      (HELM_WRITE, "helm upgrade/install/uninstall")):
        m = pat.search(body)
        if m:
            hits.append(f"{what}: «{m.group(0).strip()[:60]}»")
    return hits


# ── Защищена ли цель ────────────────────────────────────────────────────────
def guarded(name, seen=None):
    """Гейт в зависимостях (в т.ч. транзитивно) ИЛИ вызов из рецепта.

    Вызов из рецепта — законная форма: guard-destructive принимает параметры
    (что рушим и каким словом подтверждать), а зависимости параметров не
    принимают.
    """
    seen = seen or set()
    if name in seen or name not in targets:
        return False
    seen.add(name)
    t = targets[name]
    if any(p in GUARDS for p in t["prereqs"]):
        return True
    body = executable_part(t["recipe"])
    if re.search(r"\$\(MAKE\)[^\n;&|]*\b(" + "|".join(GUARDS) + r")\b", body):
        return True
    return any(guarded(p, seen) for p in t["prereqs"])


violations, mutating = [], []
for name in order:
    if name in GUARDS:
        continue
    body = executable_part(targets[name]["recipe"])
    reasons = mutating_reasons(body)
    if not reasons:
        continue
    mutating.append(name)
    if not guarded(name):
        violations.append((name, reasons))

# ── ПОРЯДОК: страж стоит МЕЖДУ созданием кластера и действиями на нём ────────
#
# Наличие стража и его МЕСТО — разные свойства, и второе ловится только так.
# make исполняет зависимости ДО рецепта (а при -j вообще без гарантии порядка),
# поэтому страж, записанный зависимостью РЯДОМ с зависимостью, которая кластер
# создаёт, исполняется РАНЬШЕ создания. Стражу нечего проверять: контекста ещё
# нет, kind ещё не знает кластера — он отказывает, и цель не доходит до создания
# вовсе. Так починка стража ломает то, что он охраняет.
#
# Законная форма ровно одна: страж вызывается ИЗ РЕЦЕПТА — рецепт идёт после всех
# зависимостей, то есть после создания. `dev-up` так и делает.
CREATES_CLUSTER = re.compile(r"\bkind\s+create\s+cluster\b|create-cluster\.sh\b")


def creates_cluster(name, seen=None):
    """Цель создаёт kind-кластер сама либо через свои зависимости."""
    seen = seen or set()
    if name in seen or name not in targets:
        return False
    seen.add(name)
    if CREATES_CLUSTER.search(executable_part(targets[name]["recipe"])):
        return True
    return any(creates_cluster(p, seen) for p in targets[name]["prereqs"])


def guard_via_prereqs(name, seen=None):
    """Гейт исполнится в рамках ЗАВИСИМОСТЕЙ цели, то есть ДО её рецепта.

    Вызов гейта из рецепта здесь НЕ считается — он и есть законная форма.
    """
    seen = seen or set()
    if name in seen:
        return False
    seen.add(name)
    if name in GUARDS:
        return True
    if name not in targets:
        return False
    return any(guard_via_prereqs(p, seen) for p in targets[name]["prereqs"])


creating = [n for n in order if n not in GUARDS and creates_cluster(n)]

misordered = []
for name in order:
    if name in GUARDS:
        continue
    prereqs = targets[name]["prereqs"]
    guard_at = next((i for i, p in enumerate(prereqs) if guard_via_prereqs(p)), None)
    create_at = next((i for i, p in enumerate(prereqs)
                      if not guard_via_prereqs(p) and creates_cluster(p)), None)
    if guard_at is not None and create_at is not None and guard_at < create_at:
        misordered.append((name, prereqs[guard_at], prereqs[create_at]))

# «Ноль находок» обязано быть отличимо от «ноль осмотренного».
print(f"осмотрено целей: {len(targets)}; меняют кластер: {len(mutating)}")
print(f"  {' '.join(sorted(mutating)) or '—'}")
print(f"создают кластер: {len(creating)}")
print(f"  {' '.join(sorted(creating)) or '—'}")

if not mutating:
    print()
    print("FAIL: ни одна цель не классифицирована как меняющая кластер.")
    print("      В файле, который поднимает и обслуживает стенд, это означает")
    print("      сломанную классификацию, а не отсутствие таких целей.")
    sys.exit(1)

# Предпосылка проверки порядка: если кластер не создаёт НИКТО, предикат остался
# без предмета и его «ноль находок» ничего не значит.
if not creating:
    print()
    print("FAIL: ни одна цель не создаёт kind-кластер.")
    print("      Проверка ПОРЯДКА стража осталась без предмета: сравнивать место")
    print("      стража не с чем, и её зелёный ничего не утверждает.")
    sys.exit(1)

if violations:
    print()
    for name, reasons in violations:
        print(f"FAIL: цель '{name}' меняет кластер, но не проходит гейт цели.")
        for r in reasons:
            print(f"        {r}")
    print()
    print("Починка: добавить в зависимости " + " или ".join(GUARDS) + ",")
    print("либо вызвать guard-destructive из рецепта (он принимает OP/TOKEN/WHAT).")
    print("Цель, предназначенная только стенду — guard-kind-context; цель, которая")
    print("по замыслу исполняется и на настоящем кластере — guard-destructive.")
    sys.exit(1)

if misordered:
    print()
    for name, g, c in misordered:
        print(f"FAIL: у цели '{name}' страж '{g}' стоит в зависимостях РАНЬШЕ, чем '{c}',")
        print(f"        которая кластер создаёт. make исполняет зависимости до рецепта,")
        print(f"        поэтому страж отработает на ещё НЕ созданном кластере, откажет —")
        print(f"        и до создания дело не дойдёт: цель не поднимется вовсе.")
    print()
    print("Починка: убрать страж из зависимостей и вызвать его ИЗ РЕЦЕПТА первой")
    print("строкой — рецепт идёт после всех зависимостей, то есть после создания:")
    print("    $(MAKE) --no-print-directory guard-kind-context")
    sys.exit(1)

print(f"\nPASS: {os.path.basename(path)} — все {len(mutating)} целей под гейтом,")
print(f"      порядок стража относительно создания кластера соблюдён "
      f"({len(creating)} создающих целей)")
PY
}

# ─────────────────────────────────────────────────────────────────────────────
# Самопроверка: гейт обязан краснеть на внесённом дефекте И молчать на законной
# конструкции ТОЙ ЖЕ ФОРМЫ. Без второй половины гейт ловит форму, а не существо,
# и первый же ложный срабат его отключит.
# ─────────────────────────────────────────────────────────────────────────────
if [ "${1:-}" = "--self-test" ]; then
  echo "=== $SCRIPT: self-test (инъекции в копию Makefile) ==="
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  rc=0

  inject() { cp "$MAKEFILE" "$tmp/Makefile"; printf '%s\n' "$2" >>"$tmp/Makefile"; }

  # (A) ИНЪЕКЦИЯ ДЕФЕКТА: цель меняет кластер и не защищена → гейт обязан краснеть
  #     и НАЗВАТЬ координату.
  inject A '
injected-unguarded:
	@kubectl -n kacho delete secret some-secret --ignore-not-found'
  out="$(run_gate "$tmp/Makefile" 2>&1)"; st=$?
  if [ $st -ne 0 ] && printf '%s' "$out" | grep -q "injected-unguarded"; then
    echo "  ОК  (A) незащищённая цель → красное, координата названа"
  else
    echo "  ПРОВАЛ (A) незащищённая цель прошла (exit=$st)"; echo "$out" | sed 's/^/      /'; rc=1
  fi

  # (B) КОНТРОЛЬ: та же форма, но защищена зависимостью → гейт обязан МОЛЧАТЬ.
  inject B '
injected-guarded: guard-kind-context
	@kubectl -n kacho delete secret some-secret --ignore-not-found'
  out="$(run_gate "$tmp/Makefile" 2>&1)"; st=$?
  if [ $st -eq 0 ]; then
    echo "  ОК  (B) защищённая цель той же формы → молчит"
  else
    echo "  ПРОВАЛ (B) законная цель покрашена — гейт ловит форму, а не существо"; echo "$out" | sed 's/^/      /'; rc=1
  fi

  # (B2) КОНТРОЛЬ: защита вызовом из рецепта (форма guard-destructive) → молчит.
  inject B2 '
injected-guarded-recipe:
	@$(MAKE) --no-print-directory guard-destructive OP=x TOKEN=Y WHAT=z
	@kubectl -n kacho delete secret some-secret --ignore-not-found'
  out="$(run_gate "$tmp/Makefile" 2>&1)"; st=$?
  if [ $st -eq 0 ]; then
    echo "  ОК  (B2) защита вызовом из рецепта распознана"
  else
    echo "  ПРОВАЛ (B2) вызов гейта из рецепта не засчитан"; echo "$out" | sed 's/^/      /'; rc=1
  fi

  # (C) КОНТРОЛЬ ЧТЕНИЯ: цель лишь ПЕЧАТАЕТ строку с командой записи.
  #     Гейт обязан молчать — иначе он читает текст, а не исполняемую часть,
  #     и его можно обойти кавычками.
  inject C '
injected-prints-only:
	@echo "дальше сделай: kubectl delete pod foo"'
  out="$(run_gate "$tmp/Makefile" 2>&1)"; st=$?
  if [ $st -eq 0 ]; then
    echo "  ОК  (C) печать команды не считается действием"
  else
    echo "  ПРОВАЛ (C) строка в echo классифицирована как запись"; echo "$out" | sed 's/^/      /'; rc=1
  fi

  # (C2) ТОТ ЖЕ КОНТРОЛЬ, НО С ТОЧКОЙ С ЗАПЯТОЙ ВНУТРИ СТРОКИ — форма, на
  #      которой первая версия читателя и сломалась (резала по разделителю, а
  #      не по кавычке). Без этого случая (C) проходил, а дефект жил.
  inject C2 '
injected-prints-only-semicolon:
	@echo "готово; дальше: kubectl rollout restart deploy/foo"'
  out="$(run_gate "$tmp/Makefile" 2>&1)"; st=$?
  if [ $st -eq 0 ]; then
    echo "  ОК  (C2) точка с запятой внутри печатаемой строки не делает её командой"
  else
    echo "  ПРОВАЛ (C2) читатель режет строку по разделителю, а не по кавычке"; echo "$out" | sed 's/^/      /'; rc=1
  fi

  # (E) ИНЪЕКЦИЯ ДЕФЕКТА ПОРЯДКА: страж записан зависимостью РЯДОМ с зависимостью,
  #     которая кластер создаёт → гейт обязан краснеть и назвать координату.
  #     Это ровно та регрессия, из-за которой цель боевой посадки не поднималась.
  inject E '
injected-creator:
	@./kind/create-cluster.sh

injected-guard-before-create: guard-kind-context injected-creator
	@kubectl -n kacho apply -f manifest.yaml'
  out="$(run_gate "$tmp/Makefile" 2>&1)"; st=$?
  if [ $st -ne 0 ] && printf '%s' "$out" | grep -q "injected-guard-before-create"; then
    echo "  ОК  (E) страж раньше создания кластера → красное, координата названа"
  else
    echo "  ПРОВАЛ (E) страж перед созданием кластера прошёл (exit=$st)"; echo "$out" | sed 's/^/      /'; rc=1
  fi

  # (E2) КОНТРОЛЬ ТОЙ ЖЕ ФОРМЫ: страж вызывается ИЗ РЕЦЕПТА после создающей
  #      зависимости — законная форма, гейт обязан МОЛЧАТЬ. Без этой половины
  #      проверка ловила бы форму «страж + создание рядом», а не порядок.
  inject E2 '
injected-creator2:
	@./kind/create-cluster.sh

injected-guard-after-create: injected-creator2
	@$(MAKE) --no-print-directory guard-kind-context
	@kubectl -n kacho apply -f manifest.yaml'
  out="$(run_gate "$tmp/Makefile" 2>&1)"; st=$?
  if [ $st -eq 0 ]; then
    echo "  ОК  (E2) страж из рецепта после создания → молчит"
  else
    echo "  ПРОВАЛ (E2) законный порядок покрашен"; echo "$out" | sed 's/^/      /'; rc=1
  fi

  # (E3) КОНТРОЛЬ: страж зависимостью, но НИ ОДНА зависимость кластер не создаёт
  #      (форма всех читающих гейтов файла) → молчит. Иначе гейт запретил бы
  #      законную и самую частую конструкцию.
  inject E3 '
injected-guard-no-create: guard-kind-context
	@kubectl -n kacho apply -f manifest.yaml'
  out="$(run_gate "$tmp/Makefile" 2>&1)"; st=$?
  if [ $st -eq 0 ]; then
    echo "  ОК  (E3) страж зависимостью без создающей зависимости → молчит"
  else
    echo "  ПРОВАЛ (E3) обычная защищённая цель покрашена"; echo "$out" | sed 's/^/      /'; rc=1
  fi

  # (E4) ПРЕДПОСЫЛКА ПОРЯДКА: создание кластера из файла исчезло → проверка
  #      порядка осталась без предмета и обязана объявить себя невыполнимой.
  sed 's|\./kind/create-cluster\.sh|./kind/bring-up.sh|' "$MAKEFILE" >"$tmp/Makefile"
  out="$(run_gate "$tmp/Makefile" 2>&1)"; st=$?
  if [ $st -ne 0 ] && printf '%s' "$out" | grep -q "без предмета"; then
    echo "  ОК  (E4) исчезло создание кластера → проверка порядка объявлена невыполнимой"
  else
    echo "  ПРОВАЛ (E4) без создающей цели проверка порядка осталась зелёной (exit=$st)"; rc=1
  fi

  # (D) ПРЕДПОСЫЛКА: гейты переименованы → проверка обязана объявить себя
  #     невыполнимой, а не сообщить «нарушений нет».
  sed 's/^guard-kind-context:/guard-kind-context-renamed:/' "$MAKEFILE" >"$tmp/Makefile"
  out="$(run_gate "$tmp/Makefile" 2>&1)"; st=$?
  if [ $st -ne 0 ] && printf '%s' "$out" | grep -q "Предпосылка"; then
    echo "  ОК  (D) исчезнувший гейт → проверка объявлена невыполнимой"
  else
    echo "  ПРОВАЛ (D) без цели-гейта проверка осталась зелёной (exit=$st)"; rc=1
  fi

  echo
  [ $rc -eq 0 ] && echo "PASS: $SCRIPT --self-test" || echo "FAIL: $SCRIPT --self-test"
  exit $rc
fi

echo "=== цели, меняющие кластер, обязаны нести гейт цели ==="
run_gate "$MAKEFILE"
