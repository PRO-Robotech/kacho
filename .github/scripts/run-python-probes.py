#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Прогонщик регрессионных проб на python: пробы сюит обязаны ИСПОЛНЯТЬСЯ.

ПРЕДМЕТ
-------
Регрессионные пробы вокруг оснастки наборов newman. Сегодня они живут на ОДНОМ
уровне, и образец состава поэтому один (см. `DEFAULT_PATTERNS` ниже):

  * `tests/newman/scripts/*_test.py` — пробы ВЕРДИКТНОГО СЛОЯ, общего на дерево:
    гейта суиты (`assert-suites-green.sh`), гейта ИСПОЛНЕННОСТИ прогона
    (`exec-coverage.py`) и гейта покрытия (`coverage.py`). Слой судит все восемь
    наборов и потому не принадлежит ни одному.

Здесь стоял ВТОРОЙ образец — `tests/authz-fixtures/*_test.py`, пробы ПОСЕВА,
поднимающего фикстуры боевой посадки. Он снят вместе со своим предметом
(2026-09-22): под ним лежали ровно две пробы, и обе проверяли проброс к прежнему
издателю личностей, а издатель снимается из продукта. Слой посева как ДОМ никуда
не делся — делись пробы, которые в нём лежали. Образец, не находящий в дереве НИ
ОДНОЙ пробы, есть расширение вхолостую: держать его «про запас» значит выдавать
пустоту за покрытие.

Их не запускал НИКТО: ни workflow, ни
Makefile, ни другой гейт. Слово `pytest` встречалось во всём дереве один раз — в
`.dockerignore`, где закрыт кэш его прогонов, то есть кто-то гонял их руками и
след остался только от кэша.

Цена не абстрактна. Среди этих проб — доказательство гейта, который в своё время
нашёл, что прогон послал не все запросы, а сюита при этом отчиталась зелёной.
То есть проверка, ловящая ложное зелёное, сама была ложно-зелёной: её вердикт не
доходил ни до чьего кода выхода.

ПОЧЕМУ PYTEST, А НЕ СОБСТВЕННЫЙ РАННЕР
--------------------------------------
28 проб из 48 принимают фикстуру `tmp_path` (свой временный каталог на пробу):
`exec_coverage_test.py` — 23, `coverage_test.py` — 5. Собственный раннер обязан
был бы либо воспроизвести внедрение фикстур и разбор утверждений, либо потребовать
переписать 28 рабочих проб под себя. Переписывать зелёные пробы под раннер,
который сам ещё надо доказать, — больший риск, чем взять исполнитель, чьё
поведение известно (и это прямо запрещает LEAN: не изобретать сложное там, где
хватает готового).

Форма `--self-test` (как у `deploy/scripts/assert-*.py`) уместна ГЕЙТУ: один
предмет, один вердикт, доказательство инъекцией. Здесь предмет другой — НАБОР из
48 независимых проб с интроспекцией утверждений. Оба вида в дереве уже есть:
`phantom_gate_test.py` написан гейтом (свой `main`, ноль функций `test_`), эти
четыре — набором, и написаны они именно под pytest (голый `assert`, `tmp_path`).

ПОЧЕМУ СВЕРХ PYTEST НУЖЕН ЭТОТ ФАЙЛ
-----------------------------------
Одного `pytest <каталог>` недостаточно, и это не вкусовщина:

  * pytest выходит кодом 5 только когда не собрано НИ ОДНОЙ пробы. Он ничего не
    скажет, если из пяти файлов молча выпали четыре — а это ровно тот класс,
    ради которого файл и написан;
  * состав обязан находиться ОБХОДОМ отслеживаемого дерева (`git ls-files`), а не
    перечисляться в шаге: тогда проба нового сервиса попадает под гейт по
    построению, а не после того, как кто-то вспомнит. Единица счёта — элемент,
    versioned в git, то есть то же множество, что увидит CI на свежем checkout'е;
  * `phantom_gate_test.py` устроен ГЕЙТОМ: pytest соберёт из него ноль проб и
    промолчит. Файл, попавший в состав и не давший ни одной пробы, — та же немота
    классом ниже, поэтому вид файла определяется РАЗБОРОМ, а исполняется каждый
    по своей форме;
  * «ноль исполненных» обязано быть ОТКАЗОМ, а не успехом, и объём осмотренного
    обязан печататься: иначе «ноль находок» неотличимо от «ноль прочитанного».

ВИД ФАЙЛА ЧИТАЕТСЯ РАЗБОРОМ (AST), А НЕ ГРЕПОМ. Строка `def test_...` встречается
в объяснениях и строковых литералах; грепом по тексту вид определялся бы по
прозе. Разбор видит объявления верхнего уровня и ветку `__main__` — то есть код.

ПРОПУСК НЕ ЗАСЧИТЫВАЕТСЯ ЗА ПРОХОД. Пропущенная проба — отказ прогонщика:
маскировка запрещена (`testing.md` §«E2E никогда не пропускаются»), и молчаливый
`skip` здесь — тот самый способ обойти.

Запуск:
  python3 .github/scripts/run-python-probes.py --self-test   # доказательство инъекцией
  python3 .github/scripts/run-python-probes.py               # прогон по дереву
"""
from __future__ import annotations

import argparse
import ast
import fnmatch
import os
import shutil
import subprocess
import sys
import tempfile
import xml.etree.ElementTree as ET
from pathlib import Path

# Образцы состава. Тот же вид, что у сверщиков переписи кейсов
# (`services/*/tests/newman/scripts/validate-cases.py`): сюита названа звёздочкой,
# поэтому новая попадает под гейт сама.
#
# ОБРАЗЕЦ СЕГОДНЯ ОДИН, И ЭТО СОСТОЯНИЕ ДЕРЕВА, А НЕ УПРОЩЕНИЕ ОБХОДА. Он несёт
# пробы вердиктного слоя (`<корень>/tests/newman/scripts/`), который судит все
# восемь наборов и потому не принадлежит ни одному. Перечень остаётся КОРТЕЖЕМ, а
# перепись — ПОИМЁННОЙ по каждому образцу: машинерия на N образцов жива и
# доказана самопроверкой (h) ниже, поэтому возврат второго уровня стоит одной
# строки, а не переделки обхода.
#
# ПОЧЕМУ НЕ ОДИН ОБРАЗЕЦ СО ЗВЁЗДОЧКОЙ ВПЕРЕДИ. `*` в pathspec git пересекает
# `/`, в `fnmatch` — пересекает, а в `filepath.Match` (гейт проводки
# `tools/pythonprobes`) — НЕ пересекает. Один образец `*/tests/newman/…` читался
# бы прогонщиком и его гейтом ПО-РАЗНОМУ, и разошлись бы они молча: гейт
# объявил бы пробу невидимой там, где прогонщик её видит. Перечень явных
# образцов, ни один из которых не пересекает `/`, читается всеми тремя
# одинаково.
#
# ОБРАЗЕЦ, НЕ НАХОДЯЩИЙ НИЧЕГО, — НАХОДКА, а не мелочь: расширение обхода, не
# изменившее переписи, выглядит покрытием и им не является. Гейт проводки
# (`tools/pythonprobes`) требует от каждого образца хотя бы одну пробу дерева.
# Оставшийся образец несут пробы ровно ОДНОГО набора; если этот набор когда-нибудь
# уедет из дерева, образец останется без предмета и гейт потребует решения — снять
# его либо назвать набор, ради которого он держится. Это не поломка, а вопрос,
# который иначе был бы решён молчанием.
#
# СНЯТО ДВА ОБРАЗЦА, И ОБА — ВМЕСТЕ СО СВОИМ ПРЕДМЕТОМ, а не ради зелёного:
#   * `services/*/tests/newman/scripts/*_test.py` — пробы генератора коллекций
#     СВОЕГО набора. Такие пробы нёс только набор службы доступа, а служба
#     вынесена отдельным продуктом (задача #1111);
#   * `tests/authz-fixtures/*_test.py` — пробы ПОСЕВА. Под ним лежали ровно две
#     пробы, и обе судили проброс к прежнему издателю личностей; издатель
#     снимается из продукта, пробы сняты вместе с ним (2026-09-22).
#
# Снятие обратимо и восстанавливается САМО, и вот чем. Каталоги обоих снятых
# уровней ОСТАЛИСЬ в `probeDirGlobs` гейта проводки (`tools/pythonprobes`) —
# перечень там объявлен ОТДЕЛЬНО от этого файла именно затем, чтобы не умирать
# вместе с ним. Появится в таком каталоге хоть одна проба — вторая половина гейта
# («всякий отслеживаемый файл проб покрыт образцом») покраснеет с её ИМЕНЕМ,
# потому что покрыть её станет нечем. Пустой каталог в том перечне — не
# просроченная запись, а сторожевой пост.
DEFAULT_PATTERNS = (
    "tests/newman/scripts/*_test.py",
)

# Виды файла проб.
KIND_PYTEST = "набор pytest"
KIND_SCRIPT = "гейт со своим main"


def repo_root() -> Path:
    return Path(__file__).resolve().parents[2]


def list_tracked(root: Path, patterns: tuple[str, ...]) -> list[str]:
    """Состав — по содержимому репозитория, с откатом на обход ФС.

    Для репозитория авторитет — версионный контроль (то же множество, что у CI на
    свежем checkout'е). В синтетическом дереве самопроверки git недоступен, и тогда
    обход идёт по файловой системе: тот же приём и по той же причине, что в
    `deploy/scripts/run-gate-self-tests.sh`.

    Образцы объединяются, а не выбираются: файл, попавший под два сразу, считается
    один раз — иначе перепись «файлов проб найдено» назвала бы больше, чем есть.
    """
    try:
        out = subprocess.run(
            ["git", "-C", str(root), "ls-files", "-z", "--", *patterns],
            capture_output=True, text=True, timeout=60, check=True).stdout
        names = sorted({n for n in out.split("\0") if n})
        if names:
            return names
    except (subprocess.SubprocessError, OSError):
        pass
    found = set()
    for dirpath, _dirs, files in os.walk(root):
        for f in files:
            rel = os.path.relpath(os.path.join(dirpath, f), root)
            rel = rel.replace(os.sep, "/")
            if any(fnmatch.fnmatch(rel, pat) for pat in patterns):
                found.add(rel)
    return sorted(found)


def classify(path: Path) -> tuple[str | None, list[str]]:
    """Вид файла и имена его проб — РАЗБОРОМ, а не поиском по тексту."""
    try:
        tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    except (SyntaxError, OSError) as e:
        return None, [f"не разбирается: {e}"]

    probes = [
        node.name for node in tree.body
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef))
        and node.name.startswith("test_")
    ]
    if probes:
        return KIND_PYTEST, sorted(probes)

    for node in tree.body:
        if not isinstance(node, ast.If):
            continue
        for sub in ast.walk(node.test):
            if isinstance(sub, ast.Name) and sub.id == "__name__":
                return KIND_SCRIPT, []
    return None, []


def run_pytest(root: Path, files: list[str]) -> tuple[int, int, list[str]]:
    """Прогон набора. Возвращает (исполнено, провалено, замечания).

    Счёт берётся из junit-XML, а не из разбора человекочитаемого хвоста: хвост
    меняется между версиями, и проверка, читающая его глазами, разошлась бы молча.
    """
    if not files:
        return 0, 0, []
    xml_dir = Path(tempfile.mkdtemp(prefix="probes-junit-"))
    xml = xml_dir / "probes.xml"
    try:
        proc = subprocess.run(
            [sys.executable, "-m", "pytest", *files,
             "-q", "-p", "no:cacheprovider", f"--junit-xml={xml}"],
            cwd=str(root), capture_output=True, text=True, timeout=900)
        sys.stdout.write(proc.stdout)
        if proc.stderr.strip():
            sys.stderr.write(proc.stderr)

        if not xml.is_file():
            return 0, 0, [
                "pytest не оставил junit-отчёта — прогон НЕ ВЫПОЛНЕН, "
                f"и это не «ноль находок» (код выхода {proc.returncode})"]

        suite = ET.parse(xml).getroot()
        if suite.tag == "testsuites":
            inner = suite.find("testsuite")
            # `or` здесь читать нельзя: пустой элемент ложен по истинностному
            # значению, и вложенный отчёт без проб молча подменился бы внешним.
            if inner is not None:
                suite = inner
        total = int(suite.get("tests", 0))
        failures = int(suite.get("failures", 0))
        errors = int(suite.get("errors", 0))
        skipped = int(suite.get("skipped", 0))

        notes = []
        if skipped:
            # Маскировка запрещена: пропуск не идёт в зачёт прохода.
            notes.append(
                f"{skipped} проб(а) ПРОПУЩЕНО — пропуск не засчитывается за проход; "
                f"пробе нужна своя посадка, а не skip")
        if proc.returncode == 5:
            notes.append("pytest не собрал НИ ОДНОЙ пробы из переданных файлов")
        elif proc.returncode not in (0, 1):
            notes.append(f"pytest вышел кодом {proc.returncode} — прогон недействителен")
        return total, failures + errors + skipped, notes
    finally:
        shutil.rmtree(xml_dir, ignore_errors=True)


def run_script(root: Path, rel: str) -> tuple[int, list[str]]:
    """Гейт со своим main: считается ОДНОЙ пробой, вердикт — его код выхода."""
    proc = subprocess.run([sys.executable, rel], cwd=str(root),
                          capture_output=True, text=True, timeout=600)
    sys.stdout.write(proc.stdout)
    if proc.stderr.strip():
        sys.stderr.write(proc.stderr)
    if proc.returncode != 0:
        return 1, [f"{rel}: вышел кодом {proc.returncode}"]
    return 1, []


def execute(root: Path, patterns: tuple[str, ...]) -> int:
    files = list_tracked(root, patterns)

    shown = ", ".join(patterns)
    print("===== регрессионные пробы python: перепись состава =====")
    print(f"образцов: {len(patterns)} — {shown}")
    # Перепись ПО КАЖДОМУ образцу отдельно. Одно суммарное число скрывает ровно
    # тот случай, ради которого она поимённая: образец, переставший что-либо
    # находить, суммы не меняет, пока жив ХОТЬ ОДИН сосед, — и его смерть тогда
    # неотличима от исправной работы.
    #
    # Формулировка НЕ привязана к числу образцов, и это не стилистика. Привязанная
    # стареет при КАЖДОМ снятии уровня, молча и в собственном файле: здесь стояла
    # именно такая — она называла образцы числом и опиралась на живого соседа, —
    # и снятие слоя посева (2026-09-22) сделало её ложью ровно тем коммитом,
    # который переписывал шапку, чтобы ложных утверждений не осталось. Дословно
    # мёртвая фраза здесь НЕ приводится: перепись по ней ищет живые вхождения, и
    # цитата в объяснении неотличима от невыправленного утверждения.
    #
    # Сегодня образец ОДИН, и скрывать нечего: при единственном образце сумма и
    # есть его число. Цикл остаётся поимённым потому, что предмет здесь — обход на
    # N образцов, а не сегодняшнее N; свойство держит самопроверка (h), которая
    # задаёт образцы явно и потому переживает снятие любого уровня.
    for pat in patterns:
        n = sum(1 for rel in files if fnmatch.fnmatch(rel, pat))
        print(f"  по образцу {pat}: {n}")
    print(f"файлов проб найдено: {len(files)}")

    # Ноль файлов — ОТКАЗ. Пустой состав отчитался бы «всё чисто», и именно так
    # 48 проб прожили в дереве, не исполнившись ни разу.
    if not files:
        print(f"ОТКАЗ: по образцам {shown} не найдено ни одного файла проб — "
              f"обход сломан либо пробы переехали. Пустой обход не является "
              f"доказательством чистоты.", file=sys.stderr)
        return 1

    pytest_files: list[str] = []
    script_files: list[str] = []
    problems: list[str] = []
    declared = 0

    for rel in files:
        kind, probes = classify(root / rel)
        if kind == KIND_PYTEST:
            pytest_files.append(rel)
            declared += len(probes)
            print(f"  {rel}: {KIND_PYTEST}, проб объявлено {len(probes)}")
        elif kind == KIND_SCRIPT:
            script_files.append(rel)
            print(f"  {rel}: {KIND_SCRIPT}")
        else:
            detail = f" ({probes[0]})" if probes else ""
            print(f"  {rel}: ВИД НЕ ОПОЗНАН{detail}")
            problems.append(
                f"{rel}: ни одной функции `test_*` верхнего уровня, ни ветки "
                f"`__main__` — такой файл собрал бы ноль проб и промолчал; "
                f"это немота, а не чистота")

    if shutil.which(sys.executable) and pytest_files:
        try:
            subprocess.run([sys.executable, "-c", "import pytest"],
                           capture_output=True, check=True, timeout=60)
        except (subprocess.SubprocessError, OSError):
            # Отсутствие инструмента — ОТКАЗ, а не пропуск: «не выполнилось» не
            # идёт в зачёт «прошло».
            print("ОТКАЗ: нет pytest, а 48 проб написаны под него — прогон НЕ "
                  "ВЫПОЛНЕН. Установи его в шаге (`python3 -m pip install pytest`).",
                  file=sys.stderr)
            return 2

    print()
    executed = 0
    failed = 0

    if pytest_files:
        print(f"===== прогон набора pytest ({len(pytest_files)} файл(ов)) =====")
        ran, bad, notes = run_pytest(root, pytest_files)
        executed += ran
        failed += bad
        problems += notes
        print(f"проб исполнено (junit): {ran}; объявлено разбором: {declared}")
        # НЕДОБОР — находка: часть файла молча не собралась (ошибка импорта на
        # уровне модуля читается именно так). ПЕРЕБОР находкой не является и
        # быть не может: `@pytest.mark.parametrize` разворачивает ОДНО
        # объявление в несколько прогонов, и это штатная форма, а не дефект.
        #
        # Прежнее сравнение было на неравенство и потому краснело на законном
        # разворачивании: 109 объявлений против 114 прогонов, упавших проб ноль,
        # а прогон красен. Хуже того, текст описывал ОБРАТНОЕ направление —
        # читатель шёл искать несобравшийся файл, которого не существует, потому
        # что у ошибки импорта исход `исполнено > объявлено` невозможен by
        # construction.
        if ran and declared and ran < declared:
            problems.append(
                f"объявлено {declared} проб, исполнено {ran} — часть файла не "
                f"собралась; недобор состава молчать не должен")

    for rel in script_files:
        print(f"===== {rel} (гейт со своим main) =====")
        ran, notes = run_script(root, rel)
        executed += ran
        failed += len(notes)
        problems += notes

    print()
    print(f"===== ИТОГ: файлов {len(files)} "
          f"(набор {len(pytest_files)}, гейт {len(script_files)}); "
          f"проб исполнено {executed}; провалено {failed} =====")

    if executed == 0:
        print("ОТКАЗ: не исполнено НИ ОДНОЙ пробы — это провал, а не чистота.",
              file=sys.stderr)
        return 1

    # ВЕРДИКТ ЧИТАЕТ ЧИСЛО УПАВШИХ, а не только перечень замечаний.
    #
    # Первая редакция выводила вердикт из `problems` — и печатала PASS, имея
    # `провалено 1`: упавшее утверждение даёт число, но не «замечание», поэтому
    # перечень оставался пуст. Ровно тот класс, который этот прогонщик и обслуживает
    # (прогонщик, печатающий зелёное при красном). Найдено собственной
    # самопроверкой, пункт (a).
    if failed or problems:
        print(f"ПРОВАЛ: пробы на python не зелёные "
              f"(упавших проб: {failed}; замечаний о составе: {len(problems)})",
              file=sys.stderr)
        for p in problems:
            print(f"  - {p}", file=sys.stderr)
        return 1

    print(f"PASS: все {executed} проб(ы) исполнены и зелёные")
    return 0


# ── ДОКАЗАТЕЛЬСТВО ИНЪЕКЦИЕЙ, В ОБЕ СТОРОНЫ ─────────────────────────────────
#
# Прогонщик — тоже проверка, значит обязан быть доказан тем же способом, каким
# требует доказывать других: верни дефект → краснеет и называет координату;
# поставь рядом законную конструкцию той же формы → молчит.

_OK_PROBE = "def test_ok():\n    assert 1 == 1\n"
_BAD_PROBE = "def test_bad():\n    assert 1 == 2, 'внесённый дефект'\n"
_SKIPPED_PROBE = (
    "import pytest\n"
    "@pytest.mark.skip(reason='инъекция: пропуск не должен читаться проходом')\n"
    "def test_skipped():\n    assert False\n"
)
_SCRIPT_OK = "import sys\n\ndef main():\n    return 0\n\nif __name__ == '__main__':\n    sys.exit(main())\n"
_SCRIPT_BAD = "import sys\n\ndef main():\n    return 1\n\nif __name__ == '__main__':\n    sys.exit(main())\n"
# Ни функций `test_*`, ни ветки `__main__`: pytest собрал бы ноль и промолчал.
# Строка `def test_...` СТОИТ здесь — в объяснении, — чтобы предикат доказал, что
# он читает разбор, а не текст: по грепу этот файл был бы «набором».
_MUTE = '"""Пояснение, в котором встречается def test_looks_like_a_probe()."""\nX = 1\n'


def _tree(files: dict[str, str]) -> Path:
    root = Path(tempfile.mkdtemp(prefix="probes-selftest-"))
    for rel, body in files.items():
        p = root / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(body)
    return root


# `_at` СНЯТ вместе со своим предметом (2026-09-11): она писала синтетику под
# `services/x/tests/newman/scripts/`, копируя площадку третьего образца, а тот
# образец сам снят вместе со службой доступа (#1111) — обе площадки, которые
# читали такую синтетику, из `DEFAULT_PATTERNS` исчезли, и самопроверка молча
# перестала находить собственные фикстуры («не найдено ни одного файла проб»),
# хотя сам гейт был исправен. `_at_seed` снята тем же порядком и по той же
# причине (2026-09-22) — вместе со снятым образцом посева. Площадка, привязанная
# к ЖИВОМУ образцу, осталась одна: `_at_root` ниже.
#
# Урок этих двух снятий записан в самопроверке (h): синтетика, привязанная к
# живому образцу, умирает вместе с ним и уносит с собой проверяемое свойство.
# Поэтому свойство «перепись поимённая» проверяется на ЯВНО заданных образцах, а
# не на `DEFAULT_PATTERNS`: его предмет — машинерия обхода на N образцов, и она
# переживает снятие любого конкретного уровня.


def _at_root(name: str, body: str) -> dict[str, str]:
    """Проба вердиктного слоя: она не принадлежит ни одной суите и лежит в корне."""
    return {f"tests/newman/scripts/{name}": body}


def _elsewhere(name: str, body: str) -> dict[str, str]:
    """Место, которого нет НИ В ОДНОМ образце, — контроль против бланкетного обхода."""
    return {f"tools/x/{name}": body}


def self_test() -> int:
    failures = []

    def check(label, cond, detail=""):
        if cond:
            print(f"  ок     {label}")
        else:
            print(f"  ПРОВАЛ {label}  {detail}")
            failures.append(label)

    import io
    import contextlib

    def run(files, patterns=DEFAULT_PATTERNS):
        root = _tree(files)
        buf = io.StringIO()
        try:
            with contextlib.redirect_stdout(buf), contextlib.redirect_stderr(buf):
                rc = execute(root, tuple(patterns))
        finally:
            shutil.rmtree(root, ignore_errors=True)
        return rc, buf.getvalue()

    print("(a) дефект внесён — прогонщик обязан покраснеть и назвать координату")
    rc, out = run(_at_root("alpha_test.py", _OK_PROBE + _BAD_PROBE))
    check("краснеет на упавшей пробе", rc == 1, out)
    check("называет файл", "alpha_test.py" in out, out)
    check("печатает перепись исполненного", "проб исполнено 2" in out, out)

    print("(b) законная конструкция той же формы — прогонщик обязан молчать")
    rc, out = run(_at_root("alpha_test.py", _OK_PROBE))
    check("молчит на зелёной пробе", rc == 0, out)
    check("перепись растёт, а не обнуляется", "проб исполнено 1" in out, out)

    print("(c) гейт со своим main исполняется и его вердикт доезжает")
    rc, out = run(_at_root("gate_test.py", _SCRIPT_BAD))
    check("краснеет на упавшем гейте", rc == 1, out)
    check("называет гейт", "gate_test.py" in out, out)
    rc, out = run(_at_root("gate_test.py", _SCRIPT_OK))
    check("молчит на зелёном гейте", rc == 0, out)

    print("(d) ноль найденного и ноль исполненного — ОТКАЗ, а не успех")
    rc, out = run({"services/x/tests/newman/scripts/helper.py": "X = 1\n"})
    check("пустой состав отвергнут", rc == 1, out)
    check("говорит, что обход ничего не нашёл", "не найдено ни одного файла" in out, out)

    print("(e) файл, который собрал бы ноль проб, — находка, а не тишина")
    rc, out = run(_at_root("mute_test.py", _MUTE))
    check("немой файл отвергнут", rc == 1, out)
    check("вид не опознан по РАЗБОРУ, не по слову в тексте",
          "ВИД НЕ ОПОЗНАН" in out, out)

    print("(f) пропуск не засчитывается за проход")
    rc, out = run(_at_root("skip_test.py", _OK_PROBE + _SKIPPED_PROBE))
    check("пропущенная проба роняет прогон", rc == 1, out)
    check("пропуск назван", "ПРОПУЩЕНО" in out, out)

    # ── (g) ВТОРОЙ ОБРАЗЕЦ ЖИВ, И ОН НЕ БЛАНКЕТНЫЙ ───────────────────────────
    #
    # Образец, добавленный и ничего не находящий, — расширение вхолостую: он
    # выглядит как покрытие и им не является. Ось доказывается ПАРОЙ: проба
    # вердиктного слоя (корень дерева) обязана быть найдена и исполнена, а такая
    # же проба в месте, которого не называет НИ ОДИН образец, — не найдена.
    # Без второй половины «нашёл» означало бы «беру всё подряд».
    print("(g) образец вердиктного слоя жив, и обход не бланкетный")
    rc, out = run(_at_root("verdict_layer_test.py", _OK_PROBE))
    check("проба вердиктного слоя найдена и зелена", rc == 0, out)
    check("названа координатой корня",
          "tests/newman/scripts/verdict_layer_test.py" in out, out)
    check("перепись по образцу вердиктного слоя не нулевая",
          "по образцу tests/newman/scripts/*_test.py: 1" in out, out)
    rc, out = run(_elsewhere("stray_test.py", _OK_PROBE))
    # «ВСЕХ», а не «обоих»: метка, пересчитывающая образцы, лжёт при следующем
    # снятии уровня — и уже солгала при этом. Гейт проводки говорит о том же
    # свойстве теми же словами («лежит вне ВСЕХ образцов»).
    check("проба вне ВСЕХ образцов НЕ засчитывается", rc == 1, out)
    check("пустой обход назван отказом", "не найдено ни одного файла" in out, out)

    # ── (i) СНЯТА ВМЕСТЕ СО СВОИМ ПРЕДМЕТОМ (2026-09-22) ─────────────────────
    #
    # Полоса доказывала, что ЖИВ образец слоя посева. Образец снят вместе с
    # пробами, которые его несли (см. `DEFAULT_PATTERNS` выше), и доказывать
    # стало нечего: полоса, переживающая свой предмет, — не покрытие, а запись,
    # которой нечего исключать. Свойство «образец, ничего не находящий, есть
    # находка» при этом НЕ осиротело: его держит гейт проводки
    # (`tools/pythonprobes`, TestRunnerPatternCoversEveryTrackedProbeFile) по
    # ЖИВОМУ дереву — там оно и должно жить, потому что синтетика о живом дереве
    # ничего не знает. Именно этот гейт и потребовал решения, которым снятие
    # сделано.

    # ── (h) ПЕРЕПИСЬ ПОИМЁННАЯ: ОБРАЗЦЫ НЕ СХЛОПЫВАЮТСЯ В СУММУ ──────────────
    #
    # Предмет здесь — МАШИНЕРИЯ обхода на N образцов, а не второй ЖИВОЙ образец.
    # Живой сегодня один, но цикл поимённой переписи в `execute` никуда не делся,
    # и без этой пары он остался бы в дереве недоказанным: код, переставший быть
    # под проверкой, ломается молча.
    #
    # Образцы поэтому задаются ЯВНО, синтетикой. Дважды уже случилось обратное:
    # полоса, привязанная к живому образцу, умирала вместе с ним и уносила
    # проверяемое свойство (`_at` в 2026-09-11, `_at_seed` сегодня). Явная пара
    # переживает снятие любого конкретного уровня.
    #
    # Пара, как и в (g): два образца дают КАЖДЫЙ своё число, а файл вне ОБОИХ не
    # засчитывается — без второй половины «нашёл» означало бы «беру всё подряд».
    print("(h) перепись поимённая — образцы не схлопываются в сумму")
    _two = ("tests/newman/scripts/*_test.py", "tools/synthetic/*_test.py")
    rc, out = run({**_at_root("verdict_layer_test.py", _OK_PROBE),
                   "tools/synthetic/second_lane_test.py": _SCRIPT_OK},
                  patterns=_two)
    check("оба образца дали по файлу", rc == 0, out)
    check("перепись первой полосы не схлопнута",
          "по образцу tests/newman/scripts/*_test.py: 1" in out, out)
    check("перепись второй полосы не схлопнута",
          "по образцу tools/synthetic/*_test.py: 1" in out, out)
    check("проб исполнено 2", "проб исполнено 2" in out, out)
    rc, out = run(_elsewhere("stray_test.py", _OK_PROBE), patterns=_two)
    check("файл вне ОБОИХ образцов НЕ засчитывается", rc == 1, out)

    print()
    if failures:
        print(f"САМОПРОВЕРКА ПРОВАЛЕНА: {len(failures)} — {', '.join(failures)}",
              file=sys.stderr)
        return 1
    print("ДОКАЗАНО: прогонщик краснеет на дефекте, молчит на законной форме, "
          "отвергает пустой обход, немой файл и пропуск.")
    return 0


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--root", default=None,
                    help="корень обхода (по умолчанию — корень репозитория)")
    ap.add_argument("--pattern", action="append", default=None,
                    help="образец состава файлов проб (можно повторять; "
                         "по умолчанию — объявленный перечень)")
    ap.add_argument("--self-test", action="store_true",
                    help="доказать инъекцией: прогонщик краснеет на дефекте и молчит на законной форме")
    args = ap.parse_args(argv)

    if args.self_test:
        return self_test()
    return execute(Path(args.root).resolve() if args.root else repo_root(),
                   tuple(args.pattern) if args.pattern else DEFAULT_PATTERNS)


if __name__ == "__main__":
    sys.exit(main())
