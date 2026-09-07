#!/usr/bin/env python3

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: AGPL-3.0-or-later

"""Гейт: у метки третьего исхода ОДИН производитель, и вердикт читает её у него.

ПРЕДМЕТ. «Условие не создано» и «продукт неверен» — разные вердикты, и различает
их метка в имени утверждения (`gen.py::PRECONDITION_MARK`). Метка отделяет
целый класс исходов от находок, поэтому у неё ровно два способа сломаться, и оба
тихие:

  1. КТО-ТО ЕЩЁ ЕЁ СТАВИТ. Рукописный кейс, поставивший метку себе, выводит своё
     падение из находок — вручную и без чьего-либо решения. Это ровно то
     вычитание, которое снято из вердикта целиком; вернуть его через имя
     утверждения было бы возвратом того же класса с чёрного хода;
  2. ВЕРДИКТ ВЫПИСЫВАЕТ ЕЁ ВТОРЫМ ЛИТЕРАЛОМ. Тогда это два места об одном
     предмете: они разойдутся МОЛЧА, помеченные утверждения тихо вернутся в
     находки, и дефект, ради которого метка заведена, вернётся вместе с зелёным
     гейтом.

САМ ЭТОТ ГЕЙТ ЛИТЕРАЛА НЕ СОДЕРЖИТ — он читает его у того же производителя.
Иначе гейт единственности сам стал бы вторым местом об одном предмете, а его
собственное объяснение — находкой (проверка по подстроке краснеет на прозе о
том, что она проверяет).

ПЕРЕПИСЬ печатает объём осмотренного: «ноль находок» обязано быть отличимо от
«ноль прочитанного».

Самопроверка способности упасть — `precondition_mark_injection_test.py` (инъекция по
каждой оси с законным близнецом).
"""

import pathlib
import re
import sys

NEWMAN = pathlib.Path(__file__).resolve().parents[1]

# Присвоение константы — то, что ПРОИЗВОДИТ метку. Упоминание имени константы в
# прозе производителем не является, поэтому образец требует присвоения строки.
RE_PRODUCER = re.compile(r'^PRECONDITION_MARK\s*=\s*(["\'])(?P<mark>.+?)\1\s*$', re.M)


def read_mark(gen_path):
    """Метка и число её производителей — из ЕДИНСТВЕННОГО источника."""
    text = gen_path.read_text(encoding="utf-8")
    hits = RE_PRODUCER.findall(text)
    marks = [m for _, m in hits]
    return marks


def audit(newman_root):
    findings, census = [], {}
    gen = newman_root / "scripts" / "gen.py"
    gate = newman_root / "scripts" / "assert-suites-green.sh"
    cases_dir = newman_root / "cases"

    if not gen.is_file():
        return {}, ["производителя метки нет вовсе (scripts/gen.py) — вердикт беспредметен"]

    marks = read_mark(gen)
    census["производителей метки в gen.py"] = len(marks)
    if len(marks) != 1:
        findings.append(
            f"производителей метки {len(marks)}, а должен быть ровно один: "
            f"второе объявление разойдётся с первым молча")
        if not marks:
            return census, findings
    mark = marks[0]

    # 1. Ставит ли метку кто-то ещё. Судятся ФАЙЛЫ КЕЙСОВ: там она означала бы
    #    ручное выведение своего падения из находок.
    seen = 0
    for path in sorted(cases_dir.glob("*.py")) if cases_dir.is_dir() else []:
        seen += 1
        if mark in path.read_text(encoding="utf-8"):
            findings.append(
                f"{path.name} ставит метку третьего исхода сам: падение кейса ушло бы "
                f"из находок без чьего-либо решения — то самое вычитание, которое "
                f"снято из вердикта целиком")
    census["файлов кейсов осмотрено"] = seen

    # 2. Читает ли вердикт метку у производителя, а не выписывает.
    if not gate.is_file():
        findings.append("вердиктного гейта нет (scripts/assert-suites-green.sh) — "
                        "категорию третьего исхода читать нечем")
    else:
        gate_text = gate.read_text(encoding="utf-8")
        census["строк вердиктного гейта"] = gate_text.count("\n") + 1
        if mark in gate_text:
            findings.append(
                "вердиктный гейт выписывает метку своим литералом: два места об одном "
                "предмете разойдутся молча, и помеченное тихо вернётся в находки")
        if "gen.PRECONDITION_MARK" not in gate_text:
            findings.append(
                "вердиктный гейт не берёт метку у производителя (gen.PRECONDITION_MARK): "
                "категория третьего исхода держалась бы совпадением строк")
    return census, findings


def main():
    census, findings = audit(NEWMAN)
    print("перепись: " + " · ".join(f"{k} {v}" for k, v in census.items()))
    if census.get("файлов кейсов осмотрено", 0) == 0:
        print("ОТКАЗ: файлов кейсов не прочитано — вердикт беспредметен", file=sys.stderr)
        return 1
    if findings:
        print(f"НАХОДКИ ({len(findings)}):", file=sys.stderr)
        for f in findings:
            print("  " + f, file=sys.stderr)
        return 1
    print("ЧИСТО: у метки третьего исхода один производитель, кейсы её не ставят, "
          "вердикт читает её у него")
    return 0


if __name__ == "__main__":
    sys.exit(main())
