#!/usr/bin/env python3

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""ПЛАТФОРМА НЕ ПЕРЕУТВЕРЖДАЕТ СУЩНОСТИ СЛУЖБЫ ДОСТУПА.

ПРЕДМЕТ. Решение владельца (2026-09-12), дословно: «в качо тестируем связку и НЕ
тестируем ресурсы канаме — только если требуется проверить, что ресурсы созданы,
которые требуются качо». Норма — корпус воркспейса, `e2e-flow.md` §7а «ДОМ пробы
— репозиторий её ПРЕДМЕТА, а не тот, через чей край она ходит».

Пока обе стороны утверждают одно и то же, это ДВА МЕСТА ОБ ОДНОМ ПРЕДМЕТЕ, и
расходятся они молча: правка контракта службы краснеет в наборе платформы, чей
владелец о ней не знает, — и наоборот. Отдельно платится ранерами.

ПРИЗНАК МАШИННЫЙ, ВЕРДИКТ — НЕТ, И ЭТО НЕСУЩЕЕ РАЗЛИЧИЕ
--------------------------------------------------------
Домены выводятся из REST-путей самого модуля. Это даёт КАНДИДАТОВ: модуль, чей
единственный домен `iam`. Кандидат нарушением НЕ является — маршрут `iam` бывает
НОСИТЕЛЕМ свойства, которое производит край. Эталон живёт в этом же дереве:
`gateway/tests/newman/cases/authn_edge.py` гоняет семь кейсов по одному маршруту
`/iam/v1/accounts` именно потому, что тот измеренно свободен от обходов
аутентификации, — предмет там рубеж КРАЯ, а не учётные записи.

Поэтому гейт требует не отсутствия таких модулей, а НАЗВАННОГО ПРОИЗВОДИТЕЛЯ:

  P1  модуль, чей единственный домен `iam`, обязан объявить `ASSERTS_DOMAIN` —
      поведение КАКОГО домена платформы он утверждает;
  P2  `ASSERTS_DOMAIN` не может быть `"iam"`: модуль, утверждающий поведение
      службы, здесь не дома — его дом репозиторий службы;
  P3  `ASSERTS_REASON` непуст — чем именно маршрут `iam` является носителем, а
      не предметом. Это половина разреза, которая ничем не выводится;
  P4  сентинел `ASSERTS_DOMAIN = "fixture"` — объявленное исключение владельца:
      модуль лишь создаёт УСЛОВИЕ, нужное платформе, и поведения службы не
      утверждает. Причина обязательна и здесь.

ЧЕГО ГЕЙТ НЕ ДЕЛАЕТ — сказано прямо: он не судит, ВЕРНА ли адъюдикация.
`ASSERTS_REASON` — проза, её истинность машине недоступна. Он судит наличие,
допустимость значения и согласие с признаком. Тот же предел, что у машинного
чтения вердикта приёмки: оно судит объявление, а не одобрение.

ИСХОДЫ:
    0 — чисто, перепись напечатана;
    1 — находка ЛИБО пустой обход (наборов или модулей ноль — «ноль находок»
        тогда означало бы «ноль прочитанного», а это не вердикт).

САМОПРОВЕРКА — `--self-test`: синтетическое дерево, инъекция по каждой оси с
законным близнецом, плюс пустой обход.
"""

from __future__ import annotations

import argparse
import ast
import pathlib
import re
import sys
import tempfile

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parents[2]

SERVICE_DOMAIN = "iam"
FIXTURE_SENTINEL = "fixture"

DOMAIN_RE = re.compile(r"/(iam|geo|vpc|nlb|storage|compute|registry)/v1")


def case_dirs(root: pathlib.Path) -> list[pathlib.Path]:
    """Наборы ВЫВОДЯТСЯ из дерева, а не выписываются.

    Рукописный перечень наборов разошёлся бы с деревом молча и в одну сторону:
    новый набор в него просто не попал бы — ровно тот класс, который этот гейт
    и стережёт на уровне модулей.
    """
    return sorted({p for p in root.glob("*/tests/newman/cases") if p.is_dir()} |
                  {p for p in root.glob("*/*/tests/newman/cases") if p.is_dir()})


def domains_of(text: str) -> list[str]:
    return sorted(set(DOMAIN_RE.findall(text)))


def _module_str_constant(tree: ast.Module, name: str) -> str | None:
    for node in tree.body:
        if not isinstance(node, ast.Assign):
            continue
        for tgt in node.targets:
            if isinstance(tgt, ast.Name) and tgt.id == name:
                if isinstance(node.value, ast.Constant) and isinstance(node.value.value, str):
                    return node.value.value
                return ""
    return None


def audit(root: pathlib.Path) -> tuple[list[str], dict]:
    findings: list[str] = []
    census = {"наборов": 0, "модулей": 0, "только iam": 0,
              "iam с чужим доменом": 0, "iam не трогают": 0, "объявили производителя": 0}

    for d in case_dirs(root):
        census["наборов"] += 1
        for path in sorted(d.glob("*.py")):
            if path.name.startswith("_"):
                continue
            text = path.read_text(encoding="utf-8")
            try:
                tree = ast.parse(text, filename=str(path))
            except SyntaxError as exc:
                findings.append(f"{path.relative_to(root)}: не разбирается как python ({exc})")
                continue
            census["модулей"] += 1
            doms = domains_of(text)
            if doms == [SERVICE_DOMAIN]:
                census["только iam"] += 1
            elif SERVICE_DOMAIN in doms:
                census["iam с чужим доменом"] += 1
                continue
            else:
                census["iam не трогают"] += 1
                continue

            rel = path.relative_to(root)
            asserts = _module_str_constant(tree, "ASSERTS_DOMAIN")
            reason = _module_str_constant(tree, "ASSERTS_REASON")

            if asserts is None:                                            # P1
                findings.append(
                    f"{rel}: единственный домен — `iam`, и не объявлено ASSERTS_DOMAIN. "
                    f"Платформа не переутверждает сущности службы (решение владельца 2026-09-12, "
                    f"e2e-flow.md §7а). Назови домен платформы, чьё поведение утверждает модуль, "
                    f"либо объяви ASSERTS_DOMAIN = \"{FIXTURE_SENTINEL}\", либо перенеси модуль "
                    f"в репозиторий службы.")
                continue
            census["объявили производителя"] += 1
            if not asserts.strip():                                        # P1
                findings.append(f"{rel}: ASSERTS_DOMAIN пуст — производитель не назван")
                continue
            if asserts == SERVICE_DOMAIN:                                  # P2
                findings.append(
                    f"{rel}: ASSERTS_DOMAIN=\"{SERVICE_DOMAIN}\" — модуль утверждает поведение "
                    f"СЛУЖБЫ, и здесь он не дома. Его место — репозиторий службы.")
            if not (reason or "").strip():                                 # P3/P4
                findings.append(
                    f"{rel}: ASSERTS_DOMAIN=\"{asserts}\" без ASSERTS_REASON. Назови, чем маршрут "
                    f"`iam` здесь является НОСИТЕЛЕМ, а не предметом: это та половина разреза, "
                    f"которая ничем не выводится.")

    return findings, census


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--self-test", action="store_true")
    ap.add_argument("--root", default=str(ROOT))
    args = ap.parse_args(argv)
    if args.self_test:
        return self_test()

    root = pathlib.Path(args.root)
    findings, census = audit(root)
    print("перепись: " + " · ".join(f"{k} {v}" for k, v in census.items()))
    if census["наборов"] == 0 or census["модулей"] == 0:
        print(f"ОТКАЗ: обход пуст — в {root} не прочитано ни одного модуля кейсов.", file=sys.stderr)
        print("«Ноль находок» здесь означало бы «ноль прочитанного», а это не вердикт.",
              file=sys.stderr)
        return 1
    if findings:
        print(f"\nНАХОДОК: {len(findings)}", file=sys.stderr)
        for f in findings:
            print(f"  · {f}", file=sys.stderr)
        return 1
    print("ЧИСТО: ни один модуль не переутверждает сущности службы молча")
    return 0


# --------------------------------------------------------------------------

_OK_EDGE = ('ASSERTS_DOMAIN = "gateway"\nASSERTS_REASON = "предмет — рубеж края"\n'
            'S = ["/iam/v1/accounts"]\n')
_OK_CONNECTIVE = 'S = ["/iam/v1/users", "/vpc/v1/networks"]\n'
_OK_FOREIGN = 'S = ["/vpc/v1/networks"]\n'
_OK_FIXTURE = ('ASSERTS_DOMAIN = "fixture"\nASSERTS_REASON = "создаёт условие, нужное vpc"\n'
               'S = ["/iam/v1/accounts"]\n')

_CASES = [
    ("ok_edge.py",       _OK_EDGE,       False, "законный близнец: только iam, производитель назван"),
    ("ok_connective.py", _OK_CONNECTIVE, False, "законный близнец: связка iam+чужой — вне предмета P1"),
    ("ok_foreign.py",    _OK_FOREIGN,    False, "законный близнец: iam не трогает вовсе"),
    ("ok_fixture.py",    _OK_FIXTURE,    False, "законный близнец: объявленная фикстура"),
    ("bare.py",  'S = ["/iam/v1/accounts"]\n', True, "P1: только iam, производитель не назван"),
    ("empty_dom.py", 'ASSERTS_DOMAIN = ""\nS = ["/iam/v1/accounts"]\n', True,
     "P1: пустой ASSERTS_DOMAIN"),
    ("says_iam.py", 'ASSERTS_DOMAIN = "iam"\nASSERTS_REASON = "r"\nS = ["/iam/v1/accounts"]\n',
     True, "P2: утверждает поведение службы — не дома"),
    ("no_reason.py", 'ASSERTS_DOMAIN = "gateway"\nS = ["/iam/v1/accounts"]\n', True,
     "P3: производитель назван, причина — нет"),
    ("blank_reason.py",
     'ASSERTS_DOMAIN = "gateway"\nASSERTS_REASON = "  "\nS = ["/iam/v1/accounts"]\n', True,
     "P3: пробельная причина причиной не является"),
    ("fixture_no_reason.py", f'ASSERTS_DOMAIN = "{FIXTURE_SENTINEL}"\nS = ["/iam/v1/accounts"]\n',
     True, "P4: фикстура тоже обязана назвать причину"),
]


def self_test() -> int:
    failures: list[str] = []
    checks = 0
    with tempfile.TemporaryDirectory() as td:
        root0 = pathlib.Path(td)
        for name, body, must_find, what in _CASES:
            r = root0 / f"c_{name[:-3]}"
            d = r / "gateway" / "tests" / "newman" / "cases"
            d.mkdir(parents=True)
            for cn, cb, *_ in _CASES[:4]:
                (d / cn).write_text(cb, encoding="utf-8")
            (d / name).write_text(body, encoding="utf-8")
            findings, census = audit(r)
            mine = [f for f in findings if pathlib.PurePath(f.split(":")[0]).name == name]
            checks += 1
            if must_find and not mine:
                failures.append(f"{what}: дефект внесён, гейт молчит ({name}); нашёл: {findings}")
            if not must_find and findings:
                failures.append(f"{what}: дерево законно, гейт нашёл {findings}")
            checks += 1
            twins = [f for f in findings if pathlib.PurePath(f.split(":")[0]).name != name]
            if twins:
                failures.append(f"{what}: покраснел законный близнец: {twins}")

        # пустой обход — отказ
        empty = root0 / "empty"
        (empty / "gateway").mkdir(parents=True)
        checks += 1
        if main(["--root", str(empty)]) != 1:
            failures.append("пустой обход обязан быть отказом")

        # набор ВЫВОДИТСЯ: второй набор на другой глубине обязан попасть в обход
        two = root0 / "two"
        for sub in ("gateway/tests/newman/cases", "services/vpc/tests/newman/cases"):
            (two / sub).mkdir(parents=True)
            (two / sub / "ok_edge.py").write_text(_OK_EDGE, encoding="utf-8")
        f2, c2 = audit(two)
        checks += 1
        if c2["наборов"] != 2 or c2["модулей"] != 2 or f2:
            failures.append(f"перечень наборов обязан ВЫВОДИТЬСЯ из дерева: {c2}, {f2}")

        # помощник вне обхода
        h = root0 / "h" / "gateway" / "tests" / "newman" / "cases"
        h.mkdir(parents=True)
        (h / "ok_edge.py").write_text(_OK_EDGE, encoding="utf-8")
        (h / "_helpers.py").write_text('S = ["/iam/v1/accounts"]\n', encoding="utf-8")
        f3, c3 = audit(root0 / "h")
        checks += 1
        if f3 or c3["модулей"] != 1:
            failures.append(f"помощник `_*.py` обязан быть вне обхода: {f3}, {c3}")

    print(f"самопроверка: утверждений {checks}, осей {len(_CASES)} + пустой обход + "
          f"вывод перечня наборов + помощник")
    if failures:
        print("ОТКАЗ самопроверки:", file=sys.stderr)
        for f in failures:
            print(f"  · {f}", file=sys.stderr)
        return 1
    print("самопроверка ПРОЙДЕНА: гейт падает на каждой оси и молчит на законных близнецах")
    return 0


if __name__ == "__main__":
    sys.exit(main())
