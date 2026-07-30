#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Каждый читатель массива исполнений newman обязан сверяться со СВОДКОЙ.

ЧТО ИЗМЕРЕНО (боевой прогон 2026-07-30, отчёты в services/*/tests/newman/out)
---------------------------------------------------------------------------
`run.executions` — НЕ полная запись того, что исполнилось, и расходится со
сводкой в ОБЕ стороны:

  * НЕДОСЧЁТ. Шаг, снявший собственный запрос из предзапросного скрипта, не
    получает записи в массиве вовсе — а его утверждения при этом считаются в
    `run.stats` и перечислены в `run.failures`. В `iam-service-account.json`
    обход массива дал 143 утверждения против 153 в сводке; в `iam-user.json` —
    188 против 204, и 16 упавших утверждений принадлежали шагам, которых в
    массиве нет ни одной записью. Такая форма нашлась в 4 коллекциях iam.
  * ПЕРЕСЧЁТ. Шаг, повторённый самопетлёй, получает НЕСКОЛЬКО записей, и все они
    несут ОДИН И ТОТ ЖЕ накопленный набор утверждений. В `vpc/operation.json`
    обход даёт 47 против 37 в сводке.

Следствие для оснастки: любой инструмент, берущий число из массива, тихо
занижает или завышает — и занижение выглядит как улучшение, поэтому само себя
не выдаёт. Вердикт берётся из `run.stats`, перечень упавшего — из
`run.failures`; массив пригоден лишь для позиционных свидетельств, и тогда его
обязаны ОБЪЕДИНЯТЬ с перечнем упавшего.

ЧТО ПРОВЕРЯЕТСЯ
---------------
Перепись по содержимому репозитория (`git ls-files`): каждый файл, читающий
`run.executions`, обязан в исполняемой части читать ТАКЖЕ `failures` или
`stats`. Файл, который берёт только массив, — находка с координатой.

ПРЕДПОСЫЛКА ПРОВЕРЯЕТСЯ. Запрет держится на факте «в дереве есть читатели
массива». Если читателей не найдено вовсе — это не «ноль находок», а сломанный
поиск либо исчезнувший предмет; гейт падает и говорит об этом. Перепись
осмотренного печатается отдельно по той же причине.

ЧИТАЕТСЯ ИСПОЛНЯЕМАЯ ЧАСТЬ, А НЕ ТЕКСТ. Слова `executions` и `stats` стоят в
шапках и объяснениях (в этой — тоже). Строки-комментарии отбрасываются до
сличения, а признак — это ВЫРАЖЕНИЕ доступа (`["executions"]`, `.get("…")`,
`json:"executions"`, `.run.executions`), а не слово.

Запуск:
    python3 deploy/scripts/assert-report-readers-use-the-summary.py
    python3 deploy/scripts/assert-report-readers-use-the-summary.py --self-test
Код возврата: 0 — все читатели сверяются со сводкой; 1 — находка либо предмета
не осталось.
"""
from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys
import tempfile

EXTS = (".py", ".go", ".sh", ".bash", ".js", ".mjs", ".ts", ".jq")

# Доступ к массиву исполнений — именно ВЫРАЖЕНИЕ, в любой из принятых в дереве
# форм: индекс по ключу, чтение из словаря, тег структуры Go, путь jq/js.
READS_EXECUTIONS = re.compile(
    r"""\[\s*['"]executions['"]\s*\]"""          # d["executions"]
    r"""|\.get\(\s*['"]executions['"]"""          # run.get("executions", [])
    r"""|json:\s*"executions\""""                 # `json:"executions"`
    r"""|\.run\.executions"""                     # jq / js
    r"""|\.executions\s*[\[\.]"""                 # doc.Run.Executions[...] / .executions.map
    r"""|\bExecutions\s*\[\]"""                   # Go: Executions []struct{…}
)

# Сверка со сводкой либо с перечнем упавшего — те же формы.
READS_SUMMARY = re.compile(
    r"""\[\s*['"](?:failures|stats)['"]\s*\]"""
    r"""|\.get\(\s*['"](?:failures|stats)['"]"""
    r"""|json:\s*"(?:failures|stats)\""""
    r"""|\.run\.(?:failures|stats)"""
    r"""|\.(?:failures|stats)\s*[\[\.]"""
    r"""|\b(?:Failures|Stats)\s*\[\]"""
    r"""|\b(?:Failures|Stats)\s+(?:\[\]|struct)"""
)

_LINE_COMMENT = re.compile(r"^\s*(#|//|\*|/\*)")


def executable_lines(path: str) -> str:
    """Содержимое файла без строк-комментариев.

    Строковые литералы НЕ отбрасываются: ключ отчёта и есть строковый литерал,
    и отбросив их, гейт перестал бы видеть собственный предмет.
    """
    out = []
    try:
        with open(path, encoding="utf-8", errors="replace") as fh:
            for line in fh:
                if _LINE_COMMENT.match(line):
                    continue
                # Хвостовой комментарий: отрезается только когда он отделён
                # пробелом, чтобы не покалечить URL и не тронуть `#` внутри строк.
                line = re.sub(r"\s#\s.*$", "", line)
                out.append(line)
    except OSError:
        return ""
    return "".join(out)


def tree_files(root: str) -> list[str]:
    """Состав дерева. В репозитории авторитет — версионный контроль (то же
    множество, что увидит свежий checkout); в синтетическом дереве — обход."""
    git = subprocess.run(["git", "-C", root, "ls-files", "-z"],
                         capture_output=True, text=True)
    if git.returncode == 0:
        names = [n for n in git.stdout.split("\0") if n]
    else:
        names = []
        for dirpath, dirnames, filenames in os.walk(root):
            dirnames[:] = [d for d in dirnames if d not in (".git", "node_modules", "vendor")]
            for fn in filenames:
                names.append(os.path.relpath(os.path.join(dirpath, fn), root))
    return sorted(n for n in names if n.endswith(EXTS))


def scan(root: str) -> tuple[list[str], list[str], int]:
    """(читатели массива, читатели БЕЗ сверки со сводкой, файлов осмотрено)."""
    readers, offenders = [], []
    files = tree_files(root)
    for rel in files:
        src = executable_lines(os.path.join(root, rel))
        if not src or not READS_EXECUTIONS.search(src):
            continue
        readers.append(rel)
        if not READS_SUMMARY.search(src):
            offenders.append(rel)
    return readers, offenders, len(files)


def run(root: str) -> int:
    readers, offenders, examined = scan(root)
    print("===== читатели массива исполнений newman =====")
    print(f"осмотрено файлов: {examined}; читателей массива: {len(readers)}")
    for r in readers:
        mark = "БЕЗ СВОДКИ" if r in offenders else "сверяется"
        print(f"  {mark:10s}  {r}")

    if not readers:
        # Проверка СВОЕЙ предпосылки: запрет обоснован тем, что читатели есть.
        # Их отсутствие — не «чисто», а потеря предмета либо сломанный поиск.
        print("FAIL: читателей массива не найдено ВОВСЕ — у запрета не осталось "
              "предмета. Либо признак перестал узнавать формы доступа, либо обход "
              "не видит дерева; и то и другое делает «ноль находок» бессмысленным.",
              file=sys.stderr)
        return 1
    if offenders:
        print(f"FAIL: {len(offenders)} читател(ь/я/ей) массива не сверяются со сводкой:",
              file=sys.stderr)
        for o in offenders:
            print(f"  {o}", file=sys.stderr)
        print("Вердикт берётся из run.stats, перечень упавшего — из run.failures. "
              "Массив неполон в одну сторону (шаг, снявший свой запрос, записи не "
              "получает) и избыточен в другую (повтор самопетлёй множит одни и те же "
              "утверждения).", file=sys.stderr)
        return 1
    print("OK: каждый читатель массива сверяется со сводкой либо с перечнем упавшего.")
    return 0


# ── самопроверка: инъекция в ОБЕ стороны на синтетическом дереве ─────────────
def self_test() -> int:
    ok = True
    with tempfile.TemporaryDirectory() as td:
        os.makedirs(os.path.join(td, "tools"))

        def w(rel: str, text: str):
            with open(os.path.join(td, rel), "w", encoding="utf-8") as fh:
                fh.write(text)

        # (а) ДЕФЕКТ: берёт число из массива и ничего не сверяет.
        w("tools/array_only.py",
          'import json\n'
          'run = json.load(open("r.json"))["run"]\n'
          'failed = sum(1 for e in run["executions"] for a in e.get("assertions", []) if a.get("error"))\n'
          'print(failed)\n')
        # (б) ЗАКОННО: та же форма доступа, но вердикт берётся из сводки.
        w("tools/reconciled.py",
          'import json\n'
          'run = json.load(open("r.json"))["run"]\n'
          'seen = {e["cursor"]["position"] for e in run["executions"]}\n'
          'failed = run["stats"]["assertions"]["failed"]\n'
          'named = {f["source"]["name"] for f in run["failures"]}\n'
          'print(seen, failed, named)\n')
        # (в) ПРОЗА: слово есть, доступа нет. Обязан не попасть в читатели —
        #     иначе первый же объясняющий комментарий станет находкой.
        w("tools/prose_only.py",
          '# run["executions"] недосчитывает: шаг, снявший свой запрос, записи не получает.\n'
          '# Поэтому вердикт берут из run["stats"]. Ниже — ни того, ни другого.\n'
          'print("nothing")\n')
        # (г) Go-форма тега структуры — она и есть у второго читателя в дереве.
        w("tools/go_form.go",
          'package x\n'
          'type doc struct {\n'
          '\tRun struct {\n'
          '\t\tExecutions []struct{ Position int } `json:"executions"`\n'
          '\t\tFailures   []struct{ Name string } `json:"failures"`\n'
          '\t} `json:"run"`\n'
          '}\n')

        readers, offenders, examined = scan(td)
        want_readers = {"tools/array_only.py", "tools/reconciled.py", "tools/go_form.go"}
        if set(readers) != want_readers:
            print(f"SELF-TEST FAIL: читатели {sorted(readers)} != {sorted(want_readers)}",
                  file=sys.stderr)
            ok = False
        if offenders != ["tools/array_only.py"]:
            print(f"SELF-TEST FAIL: находки {offenders} != ['tools/array_only.py']",
                  file=sys.stderr)
            ok = False
        if examined != 4:
            print(f"SELF-TEST FAIL: осмотрено {examined} файлов, ожидалось 4", file=sys.stderr)
            ok = False
        if run(td) != 1:
            print("SELF-TEST FAIL: внесённый дефект не покраснел", file=sys.stderr)
            ok = False

    # Пустое дерево: предпосылки нет — обязан упасть, а не отчитаться «чисто».
    with tempfile.TemporaryDirectory() as td2:
        with open(os.path.join(td2, "empty.py"), "w", encoding="utf-8") as fh:
            fh.write("print(1)\n")
        if run(td2) != 1:
            print("SELF-TEST FAIL: дерево без читателей объявлено чистым", file=sys.stderr)
            ok = False

    print("SELF-TEST OK" if ok else "SELF-TEST FAILED")
    return 0 if ok else 1


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--self-test", action="store_true",
                    help="доказать инъекцией, что признак читает доступ, а не слово")
    ap.add_argument("--root", default=None, help="корень обхода (по умолчанию — корень репозитория)")
    args = ap.parse_args()
    if args.self_test:
        return self_test()
    root = args.root or os.path.abspath(
        os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
    return run(root)


if __name__ == "__main__":
    sys.exit(main())
