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
HERE="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_ROOT="$(cd "$HERE/../.." && pwd)"
MAKEFILE="${MAKEFILE_UNDER_TEST:-$DEPLOY_ROOT/Makefile}"

# Три исхода — ОДНОЙ реализацией на весь каталог: 0 зелено · 1 находка о дереве ·
# 2 условие не создано. Что расходилось здесь:
#   • «нет python3» объявлялось ЧАСТНЫМ `{ echo "FATAL: …"; exit 2; }` — код тот
#     же, но свой текст, вывод в stdout и БЕЗ переписи, поэтому «сколько успели
#     проверить» на этом исходе не печаталось вовсе;
#   • «Makefile не прочитан» решал разбор ВНУТРИ python — то есть уже после того,
#     как оболочка объявила прогон начатым, и своим текстом;
#   • код возврата разбора оболочка не читала СОВСЕМ: `run_gate` стояла последней
#     командой, и 1 (нашли незащищённую цель) от 2 (разбор не состоялся) скрипт не
#     отличал — он их просто пропускал наружу.
#
# Утверждение на уровне оболочки здесь РОВНО ОДНО: разбор прогнан и его вердикт
# прочитан. Собственный счёт целей ведёт python и печатает его сам («осмотрено
# целей / меняют кластер / создают кластер») — эти числа выше вердикта.
# shellcheck source=deploy/tests/helm/outcome.sh
. "$HERE/outcome.sh"
EXPECTED_ASSERTIONS=1

# PyYAML разбору НЕ нужен (Makefile читается `re`, а не YAML), поэтому
# `require_python_yaml` здесь объявил бы «условие не создано» на машине, где
# условие есть. Требуется ровно python3 — и его отсутствие это условие прогона.
require_python3
# Файла может не быть на диске (путь переопределяется MAKEFILE_UNDER_TEST).
# Прежде это ловил разбор внутри python; общая реализация говорит то же самое ДО
# его запуска и тем же словарём.
require_file_present "$MAKEFILE" "Makefile стенда"

run_gate() { MAKEFILE="$1" python3 - "$1" <<'PY'
import os, re, sys

path = sys.argv[1]
try:
    src = open(path, encoding="utf-8").read()
except OSError as e:
    print(f"FATAL: не прочитан {path}: {e}")
    sys.exit(2)

# СЛОВАРЬ СТРАЖЕЙ — ТРИ ЧЛЕНА, И ТРЕТИЙ ЗАВЕДЁН ПОД ИЗМЕРЕННЫЙ ПРЕДМЕТ, А НЕ ВПРОК.
#
# `guard-kind-context` пинит цель к локальному стенду и годится тому, что
# предназначено ТОЛЬКО ему. `guard-destructive` показывает человеку живую цель и
# требует ввода — им гейтится то, что по замыслу идёт и на настоящий кластер.
#
# Двух не хватило, и не по вкусу, а BY CONSTRUCTION. `module-manifests-configmap`
# применяет ConfigMap в кластер активного контекста, и на нём:
#   • kind-страж делает цель НЕИСПОЛНИМОЙ на боевых путях выкатки — за это он с
#     неё и снят (kacho#1901), и обратно его не вернуть: свойство держит гейт
#     `TestManifestProducerIsCallableFromEveryBringUpPath`;
#   • `guard-destructive` требует ТЕРМИНАЛА, а `make dev-up` идёт в конвейере без
#     stdin-терминала — он сломал бы каждый прогон e2e-newman, консоли и посадки.
# Снятие первого оставило цель вовсе без гейта — это и нашёл ЭТОТ гейт. Оба
# вердикта верны; вместо выбора между ними словарь вырос на третий член.
#
# `guard-declared-context` — неинтерактивный: цель пишет только в тот кластер,
# который вызывающий НАЗВАЛ (`EXPECT_CONTEXT`), незаданное объявление — отказ.
# Чужой контекст он не отвергает, поэтому исполним на всех трёх путях выкатки.
#
# ЧТО ЭТОТ ГЕЙТ ПРО НЕГО НЕ ЗНАЕТ, СКАЗАНО ПРЯМО: он судит по ИМЕНИ, ровно как и
# по двум прежним, — то есть удостоверяет, что страж ПОЗВАН, и ничего не говорит
# о том, что его тело осталось стражем. Эта граница у словаря была и до третьего
# члена; выхолащивание любого из трёх ловится не здесь, а его собственной пробой.
GUARDS = ("guard-kind-context", "guard-destructive", "guard-declared-context")

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
  if [ $st -ne 0 ] && [[ "$out" == *"injected-unguarded"* ]]; then
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

  # (B3) КОНТРОЛЬ ТРЕТЬЕГО ЧЛЕНА СЛОВАРЯ: защита `guard-declared-context` → молчит.
  #      Заведён вместе с самим членом: словарь, выросший без своей пробы, зеленел
  #      бы одинаково и когда имя читается предикатом, и когда его туда забыли
  #      внести, — то есть рост был бы недоказан. Пара к нему — (D2) ниже: там
  #      ТОТ ЖЕ член исчезает, и гейт обязан объявить предпосылку невыполнимой.
  inject B3 '
injected-guarded-declared: guard-declared-context
	@kubectl -n kacho delete secret some-secret --ignore-not-found'
  out="$(run_gate "$tmp/Makefile" 2>&1)"; st=$?
  if [ $st -eq 0 ]; then
    echo "  ОК  (B3) защита guard-declared-context распознана"
  else
    echo "  ПРОВАЛ (B3) третий член словаря стражей не засчитан"; echo "$out" | sed 's/^/      /'; rc=1
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
  if [ $st -ne 0 ] && [[ "$out" == *"injected-guard-before-create"* ]]; then
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
  if [ $st -ne 0 ] && [[ "$out" == *"без предмета"* ]]; then
    echo "  ОК  (E4) исчезло создание кластера → проверка порядка объявлена невыполнимой"
  else
    echo "  ПРОВАЛ (E4) без создающей цели проверка порядка осталась зелёной (exit=$st)"; rc=1
  fi

  # (D) ПРЕДПОСЫЛКА: гейты переименованы → проверка обязана объявить себя
  #     невыполнимой, а не сообщить «нарушений нет».
  sed 's/^guard-kind-context:/guard-kind-context-renamed:/' "$MAKEFILE" >"$tmp/Makefile"
  out="$(run_gate "$tmp/Makefile" 2>&1)"; st=$?
  if [ $st -ne 0 ] && [[ "$out" == *"Предпосылка"* ]]; then
    echo "  ОК  (D) исчезнувший гейт → проверка объявлена невыполнимой"
  else
    echo "  ПРОВАЛ (D) без цели-гейта проверка осталась зелёной (exit=$st)"; rc=1
  fi

  # (D2) ПРЕДПОСЫЛКА, ВТОРАЯ ПОЛОВИНА ПАРЫ К (B3): исчез ТРЕТИЙ страж. Без этого
  #      случая (B3) доказывал бы только то, что цель с таким именем проходит, —
  #      а проходила бы она и при словаре из двух членов, потому что имя-пререквизит,
  #      которого предикат не знает, просто не делает цель защищённой... и цель без
  #      kubectl-записи он и не обвиняет. Здесь член словаря обязан быть НАЗВАН.
  sed 's/^guard-declared-context:/guard-declared-context-renamed:/' "$MAKEFILE" >"$tmp/Makefile"
  out="$(run_gate "$tmp/Makefile" 2>&1)"; st=$?
  if [ $st -ne 0 ] && [[ "$out" == *"Предпосылка"* ]] && [[ "$out" == *"guard-declared-context"* ]]; then
    echo "  ОК  (D2) исчезнувший третий страж → проверка объявлена невыполнимой и он назван"
  else
    echo "  ПРОВАЛ (D2) третьего стража нет, а проверка не объявила предпосылку (exit=$st)"; echo "$out" | sed 's/^/      /'; rc=1
  fi

  echo
  [ $rc -eq 0 ] && echo "PASS: $SCRIPT --self-test" || echo "FAIL: $SCRIPT --self-test"
  exit $rc
fi

echo "=== цели, меняющие кластер, обязаны нести гейт цели ==="
# Код возврата разбора берётся КАК ДАННЫЕ, а не как условие продолжения: «разбор
# не состоялся» и «разбор нашёл незащищённую цель» — разные исходы, и до сведения
# к общему словарю оболочка их не различала.
run_gate "$MAKEFILE"
gate_rc=$?
case "$gate_rc" in
  0) ok ;;
  2) fatal "разбор Makefile не состоялся (код 2; его текст выше) — это УСЛОВИЕ прогона, а не свойство дерева" ;;
  *) fail "цели, меняющие кластер, не все под гейтом цели (код $gate_rc; перечень выше)" ;;
esac
outcome_verdict "разобран Makefile: $MAKEFILE"
