#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Вердикт по отчёту сквозных проб консоли: исполнено ВСЁ объявленное, упавших нет.

ПРЕДМЕТ. Код возврата прогона отвечает на вопрос «упало ли то, что запускалось».
Он молчит о другом: запускалось ли всё. Проба, выпавшая из набора — переименован
файл, сломался отбор, отвалился каталог с описаниями, — уносит с собой своё
утверждение, а прогон остаётся зелёным. «Ноль упавших» из двух проб и из семи
выглядит одинаково, и различить их можно только сверкой с деревом.

Поэтому здесь два независимых утверждения:

  1. ЧИСЛО. Проб в отчёте столько же, сколько объявлено в дереве. Объявленное
     считается по исходникам проб (`test(` на своей строке), а не выписывается —
     выписанное число разошлось бы с деревом молча, и разошлось бы именно тогда,
     когда пробу добавили и забыли включить.
  2. ИСХОД. Ни одной пробы вне состояния «прошла». Пропуск («skipped») здесь
     ТОЖЕ провал: пропущенная проба — это отсутствующая проверка, и правило
     запрещает её ровно так же, как маску.

Отсутствие отчёта — ПРОВАЛ, а не «нечего проверять»: суита, не оставившая
отчёта, не выполнилась, и эта третья категория из вердикта не вычитается.

Печатается объём осмотренного: сколько исходников прочитано, сколько объявлений
в них найдено, сколько записей разобрано в отчёте. «Ноль находок» обязано быть
отличимо от «ноль прочитанного».

Запуск:
    python3 .github/scripts/assert-console-probes-verdict.py            # вердикт
    python3 .github/scripts/assert-console-probes-verdict.py --self-test # инъекция
"""

from __future__ import annotations

import json
import re
import sys
import tempfile
from pathlib import Path

# Объявление пробы: вызов `test(` в начале логической строки. Форма `test.describe(`
# и `test.skip(` под неё намеренно не подпадают: первая — группа, а не проба;
# вторая обязана ловиться отдельным запретом на пропуски, а не считаться пробой.
DECL = re.compile(r"^[ \t]*test\(", re.MULTILINE)

SPECS_GLOB = "*.spec.ts"


def declared_probes(specs_dir: Path) -> tuple[int, int]:
    """Сколько проб объявлено в дереве и сколько файлов для этого прочитано."""
    files = sorted(specs_dir.glob(SPECS_GLOB))
    return sum(len(DECL.findall(f.read_text(encoding="utf-8"))) for f in files), len(files)


def walk_specs(node: dict) -> list[dict]:
    """Плоский список записей проб из отчёта playwright (вложенность произвольна)."""
    out: list[dict] = []
    for spec in node.get("specs", []) or []:
        out.append(spec)
    for child in node.get("suites", []) or []:
        out.extend(walk_specs(child))
    return out


def outcomes(report: dict) -> list[tuple[str, str]]:
    """Пары «имя пробы → исход». Исход берётся из последнего результата попытки."""
    res: list[tuple[str, str]] = []
    for spec in walk_specs(report):
        title = spec.get("title") or "(без имени)"
        status = "не исполнена"
        for t in spec.get("tests", []) or []:
            runs = t.get("results", []) or []
            if runs:
                status = runs[-1].get("status") or "не исполнена"
            elif t.get("status"):
                status = t["status"]
        res.append((title, status))
    return res


def verdict(report_path: Path, specs_dir: Path) -> tuple[int, list[str]]:
    """Возвращает (код возврата, строки вывода)."""
    log: list[str] = []
    declared, files = declared_probes(specs_dir)
    log.append(f"=== осмотрено: исходников проб {files}, объявлений в них {declared} ===")
    if files == 0 or declared == 0:
        log.append(
            "ПРОВАЛ: в дереве не найдено ни одного объявления пробы. Гейт судил бы о "
            "наборе, которого не читал, и его «ноль упавших» означало бы «ноль "
            f"прочитанного». Каталог: {specs_dir}"
        )
        return 1, log

    if not report_path.exists():
        log.append(
            f"ПРОВАЛ: отчёта нет ({report_path}). Суита без отчёта НЕ ВЫПОЛНИЛАСЬ — это "
            "третья категория исхода, и она не вычитается из вердикта и не зачитывается "
            "в зелёное."
        )
        return 1, log

    try:
        report = json.loads(report_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        log.append(f"ПРОВАЛ: отчёт не разбирается ({exc}) — исход прогона неизвестен.")
        return 1, log

    got = outcomes(report)
    log.append(f"=== разобрано записей отчёта: {len(got)} ===")
    rc = 0

    if len(got) != declared:
        log.append(
            f"ПРОВАЛ: объявлено проб {declared}, в отчёте {len(got)}. Расхождение значит, "
            "что часть проб не исполнялась, а прогон об этом молчал: «ноль упавших» из "
            "двух и из семи выглядит одинаково."
        )
        rc = 1

    bad = [(n, s) for n, s in got if s != "passed"]
    for name, status in got:
        log.append(f"  {'✓' if status == 'passed' else '✗'} {status:<12} {name}")
    if bad:
        log.append(f"ПРОВАЛ: проб не в состоянии «прошла»: {len(bad)}.")
        log.append(
            "  Пропуск здесь — тоже провал: пропущенная проба это отсутствующая проверка."
        )
        rc = 1

    if rc == 0:
        log.append(f"✓ исполнены все {declared} объявленных проб, упавших и пропущенных нет")
    return rc, log


# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА: гейт обязан покраснеть на настоящем дефекте и смолчать на
# законном близнеце. Не доказавший обоих — не доказательство.
# ─────────────────────────────────────────────────────────────────────────────

_SPEC = 'test("одна", async () => {});\ntest("две", async () => {});\n'
_SPEC_WITH_GROUP = (
    'test.describe("группа", () => {\n  test("три", async () => {});\n});\n'
)


def _report(*statuses: str) -> str:
    specs = [
        {"title": f"проба-{i}", "tests": [{"results": [{"status": s}]}]}
        for i, s in enumerate(statuses)
    ]
    return json.dumps({"suites": [{"title": "s", "specs": specs}]})


def self_test() -> int:
    rc = 0
    cases: list[tuple[str, bool, object, object]] = []

    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        specs = root / "specs"
        specs.mkdir()
        (specs / "a.spec.ts").write_text(_SPEC, encoding="utf-8")
        (specs / "b.spec.ts").write_text(_SPEC_WITH_GROUP, encoding="utf-8")
        # Объявлено 3: две на верхнем уровне и одна внутри группы. Сама группа
        # (`test.describe(`) пробой не считается — иначе законный близнец
        # завышал бы объявленное и гейт краснел бы на исправном прогоне.
        declared, _ = declared_probes(specs)
        cases.append(("объявленное считается по дереву", declared == 3, 3, declared))

        rep = root / "results.json"

        # МОЛЧИТ: исполнено всё объявленное, все прошли.
        rep.write_text(_report("passed", "passed", "passed"), encoding="utf-8")
        got, _ = verdict(rep, specs)
        cases.append(("законный прогон принят", got == 0, 0, got))

        # КРАСНЕЕТ: одна проба упала.
        rep.write_text(_report("passed", "failed", "passed"), encoding="utf-8")
        got, _ = verdict(rep, specs)
        cases.append(("упавшая проба ловится", got == 1, 1, got))

        # КРАСНЕЕТ: проба пропущена — отсутствующая проверка, а не «не считается».
        rep.write_text(_report("passed", "passed", "skipped"), encoding="utf-8")
        got, _ = verdict(rep, specs)
        cases.append(("пропуск ловится", got == 1, 1, got))

        # КРАСНЕЕТ: проба молча выпала из набора. Ровно тот случай, ради которого
        # гейт и существует: код возврата прогона здесь был бы нулевым.
        rep.write_text(_report("passed", "passed"), encoding="utf-8")
        got, _ = verdict(rep, specs)
        cases.append(("недосчёт исполненного ловится", got == 1, 1, got))

        # КРАСНЕЕТ: отчёта нет вовсе — «не выполнилось» не зачитывается в зелёное.
        rep.unlink()
        got, _ = verdict(rep, specs)
        cases.append(("отсутствие отчёта ловится", got == 1, 1, got))

        # КРАСНЕЕТ: дерево проб пусто — «ноль упавших» из ничего.
        empty = root / "empty"
        empty.mkdir()
        rep.write_text(_report(), encoding="utf-8")
        got, _ = verdict(rep, empty)
        cases.append(("пустое дерево проб ловится", got == 1, 1, got))

    for name, ok, want, have in cases:
        print(f"  {'ОК ' if ok else 'ПРОВАЛ'} {name} (ждали {want}, получили {have})")
        if not ok:
            rc = 1
    print(f"=== самопроверка: случаев {len(cases)}, провалов {sum(1 for c in cases if not c[1])} ===")
    return rc


def main() -> int:
    if "--self-test" in sys.argv[1:]:
        return self_test()
    unknown = [a for a in sys.argv[1:] if a != "--self-test"]
    if unknown:
        # Неизвестный ввод — явный отказ. Опечатка в шаге конвейера иначе дала бы
        # зелёное, ничего не проверив.
        print(f"неизвестный аргумент: {unknown}; допустимо: без аргументов | --self-test",
              file=sys.stderr)
        return 2

    root = Path(__file__).resolve().parents[2]
    e2e = root / "ui-future" / "e2e"
    rc, log = verdict(e2e / "results.json", e2e / "specs")
    print("\n".join(log))
    return rc


if __name__ == "__main__":
    sys.exit(main())
