#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Прогонщик регрессионных проб на python: пробы сюит обязаны ИСПОЛНЯТЬСЯ.

ПРЕДМЕТ
-------
`services/*/tests/newman/scripts/*_test.py` — регрессионные пробы вокруг оснастки
сюит: генератора коллекций (`gen.py`), гейта покрытия (`coverage.py`) и гейта
ИСПОЛНЕННОСТИ прогона (`exec-coverage.py`). Их не запускал НИКТО: ни workflow, ни
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

# Образец состава. Тот же вид, что у сверщиков переписи кейсов
# (`services/*/tests/newman/scripts/validate-cases.py`): сюита названа звёздочкой,
# поэтому новая попадает под гейт сама.
DEFAULT_PATTERN = "services/*/tests/newman/scripts/*_test.py"

# Виды файла проб.
KIND_PYTEST = "набор pytest"
KIND_SCRIPT = "гейт со своим main"


def repo_root() -> Path:
    return Path(__file__).resolve().parents[2]


def list_tracked(root: Path, pattern: str) -> list[str]:
    """Состав — по содержимому репозитория, с откатом на обход ФС.

    Для репозитория авторитет — версионный контроль (то же множество, что у CI на
    свежем checkout'е). В синтетическом дереве самопроверки git недоступен, и тогда
    обход идёт по файловой системе: тот же приём и по той же причине, что в
    `deploy/scripts/run-gate-self-tests.sh`.
    """
    try:
        out = subprocess.run(
            ["git", "-C", str(root), "ls-files", "-z", "--", pattern],
            capture_output=True, text=True, timeout=60, check=True).stdout
        names = [n for n in out.split("\0") if n]
        if names:
            return sorted(names)
    except (subprocess.SubprocessError, OSError):
        pass
    found = []
    for dirpath, _dirs, files in os.walk(root):
        for f in files:
            rel = os.path.relpath(os.path.join(dirpath, f), root)
            rel = rel.replace(os.sep, "/")
            if fnmatch.fnmatch(rel, pattern):
                found.append(rel)
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


def execute(root: Path, pattern: str) -> int:
    files = list_tracked(root, pattern)

    print("===== регрессионные пробы python: перепись состава =====")
    print(f"образец: {pattern}")
    print(f"файлов проб найдено: {len(files)}")

    # Ноль файлов — ОТКАЗ. Пустой состав отчитался бы «всё чисто», и именно так
    # 48 проб прожили в дереве, не исполнившись ни разу.
    if not files:
        print(f"ОТКАЗ: по образцу {pattern} не найдено ни одного файла проб — "
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


def _at(name: str, body: str) -> dict[str, str]:
    return {f"services/x/tests/newman/scripts/{name}": body}


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

    def run(files):
        root = _tree(files)
        buf = io.StringIO()
        try:
            with contextlib.redirect_stdout(buf), contextlib.redirect_stderr(buf):
                rc = execute(root, DEFAULT_PATTERN)
        finally:
            shutil.rmtree(root, ignore_errors=True)
        return rc, buf.getvalue()

    print("(a) дефект внесён — прогонщик обязан покраснеть и назвать координату")
    rc, out = run(_at("alpha_test.py", _OK_PROBE + _BAD_PROBE))
    check("краснеет на упавшей пробе", rc == 1, out)
    check("называет файл", "alpha_test.py" in out, out)
    check("печатает перепись исполненного", "проб исполнено 2" in out, out)

    print("(b) законная конструкция той же формы — прогонщик обязан молчать")
    rc, out = run(_at("alpha_test.py", _OK_PROBE))
    check("молчит на зелёной пробе", rc == 0, out)
    check("перепись растёт, а не обнуляется", "проб исполнено 1" in out, out)

    print("(c) гейт со своим main исполняется и его вердикт доезжает")
    rc, out = run(_at("gate_test.py", _SCRIPT_BAD))
    check("краснеет на упавшем гейте", rc == 1, out)
    check("называет гейт", "gate_test.py" in out, out)
    rc, out = run(_at("gate_test.py", _SCRIPT_OK))
    check("молчит на зелёном гейте", rc == 0, out)

    print("(d) ноль найденного и ноль исполненного — ОТКАЗ, а не успех")
    rc, out = run({"services/x/tests/newman/scripts/helper.py": "X = 1\n"})
    check("пустой состав отвергнут", rc == 1, out)
    check("говорит, что обход ничего не нашёл", "не найдено ни одного файла" in out, out)

    print("(e) файл, который собрал бы ноль проб, — находка, а не тишина")
    rc, out = run(_at("mute_test.py", _MUTE))
    check("немой файл отвергнут", rc == 1, out)
    check("вид не опознан по РАЗБОРУ, не по слову в тексте",
          "ВИД НЕ ОПОЗНАН" in out, out)

    print("(f) пропуск не засчитывается за проход")
    rc, out = run(_at("skip_test.py", _OK_PROBE + _SKIPPED_PROBE))
    check("пропущенная проба роняет прогон", rc == 1, out)
    check("пропуск назван", "ПРОПУЩЕНО" in out, out)

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
    ap.add_argument("--pattern", default=DEFAULT_PATTERN,
                    help="образец состава файлов проб")
    ap.add_argument("--self-test", action="store_true",
                    help="доказать инъекцией: прогонщик краснеет на дефекте и молчит на законной форме")
    args = ap.parse_args(argv)

    if args.self_test:
        return self_test()
    return execute(Path(args.root).resolve() if args.root else repo_root(),
                   args.pattern)


if __name__ == "__main__":
    sys.exit(main())
