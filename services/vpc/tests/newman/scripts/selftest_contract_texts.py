#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Самопроверка гейта «текст отказа объявлен один раз и цитируется дословно».

Гоняет НАСТОЯЩИЙ судья (`contract_texts.audit`) на синтетических деревьях и
доказывает обе стороны:

  * он КРАСНЕЕТ на внесённом расхождении и НАЗЫВАЕТ файл;
  * он МОЛЧИТ на законном дереве той же формы.

Законный близнец здесь несущий, а не для порядка. Гейт, проверенный только на
дефекте, легко написать по СЛОВУ («в странице есть слово reserved») — и он
зеленел бы на странице, которая про зарезервированные диапазоны рассказывает, а
текст отказа приводит СВОЙ. Поэтому среди инъекций стоит именно такая страница:
слово есть, обещания нет.

Отдельно проверяется предпосылка: переименованная константа обязана давать
находку, а не тишину. Иначе «ноль расхождений» означало бы «ноль прочитанного».

Запуск: python3 scripts/selftest_contract_texts.py   (стенд и newman не нужны)
"""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
_spec = importlib.util.spec_from_file_location("contract_texts_under_test", HERE / "contract_texts.py")
ct_mod = importlib.util.module_from_spec(_spec)
sys.modules["contract_texts_under_test"] = ct_mod
_spec.loader.exec_module(ct_mod)

FAILURES: list[str] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"{'ok  ' if ok else 'FAIL'}  {name}" + (f"  — {detail}" if detail and not ok else ""))
    if not ok:
        FAILURES.append(name)


# Пути СОВПАДАЮТ с настоящими: предикат читает состав `CONTRACT_TEXTS`, и
# синтетика, разложенная иначе, проверяла бы другое дерево, а не это.
DECL = "internal/apps/kacho/api/subnet/reserved_prefixes.go"
DOC = "docs/content/api/subnet.mdx"
CASES = "tests/newman/cases/subnet.py"

CONTRACT = "%s %s overlaps an address range reserved by the platform"

BASE = {
    DECL: (
        "package subnet\n\n"
        f'const reservedOverlapMsg = "{CONTRACT}"\n'
    ),
    DOC: (
        "# Subnet\n\n"
        "Диапазон, пересекающийся со служебным адресным пространством, отвергается:\n"
        "`v4_cidr_blocks[0] 10.0.0.0/24 overlaps an address range reserved by the platform`.\n"
    ),
    CASES: (
        "CASES = []\n"
        "# assert: 'v4_cidr_blocks[0] 169.254.10.0/24 overlaps an address range "
        "reserved by the platform'\n"
    ),
}


def tree(tmp: Path, **overrides: str | None) -> Path:
    """Синтетическое дерево; `None` в значении означает «файла нет»."""
    root = tmp
    files = dict(BASE)
    for key, body in overrides.items():
        rel = {"decl": DECL, "doc": DOC, "cases": CASES}[key]
        if body is None:
            files.pop(rel, None)
        else:
            files[rel] = body
    for rel, body in files.items():
        full = root / rel
        full.parent.mkdir(parents=True, exist_ok=True)
        full.write_text(body)
    return root


def run(root: Path):
    return ct_mod.audit(root)


def main() -> int:
    import tempfile

    with tempfile.TemporaryDirectory() as td:
        base = Path(td)

        # 0. ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. Без него всё, что ниже, зеленело бы и на
        #    судье, который просто всегда находит расхождение.
        findings, census = run(tree(base / "ok"))
        check("законное дерево — ноль находок", not findings, "; ".join(findings))
        check("перепись непуста (дерево действительно прочитано)",
              census.files_read == 3 and census.bytes_read > 0 and census.invariants >= 1,
              f"files_read={census.files_read} bytes={census.bytes_read} inv={census.invariants}")

        # 1. ИНЪЕКЦИЯ: страница молчит про текст отказа.
        findings, census = run(tree(base / "doc-silent", doc="# Subnet\n\nОписание без обещания.\n"))
        check("страница без цитаты — находка", len(findings) == 1)
        check("находка НАЗЫВАЕТ страницу", bool(findings) and DOC in findings[0],
              findings[0] if findings else "нет находки")

        # 2. ИНЪЕКЦИЯ ЗАКОННЫМ БЛИЗНЕЦОМ: страница ГОВОРИТ про зарезервированные
        #    диапазоны и приводит СВОЙ текст. Гейт по слову здесь зеленеет —
        #    гейт по обещанию обязан покраснеть.
        findings, _ = run(tree(base / "doc-word-only", doc=(
            "# Subnet\n\n"
            "Часть адресов зарезервирована платформой (reserved ranges), и подсеть\n"
            "поверх такого диапазона будет отклонена с ошибкой «CIDR is not allowed».\n"
        )))
        check("страница со СВОИМ текстом (слово есть, обещания нет) — находка", len(findings) == 1)

        # 3. ИНЪЕКЦИЯ: кейс не утверждает текст.
        findings, _ = run(tree(base / "case-silent", cases="CASES = []\n"))
        check("кейс без цитаты — находка", len(findings) == 1)
        check("находка НАЗЫВАЕТ файл кейсов", bool(findings) and CASES in findings[0],
              findings[0] if findings else "нет находки")

        # 4. ПРЕДПОСЫЛКА: константу переименовали.
        findings, census = run(tree(base / "renamed", decl=(
            "package subnet\n\n"
            f'const reservedOverlapMessage = "{CONTRACT}"\n'
        )))
        check("переименованное объявление — находка, а не тишина", len(findings) == 1)
        check("находка названа ПРЕДПОСЫЛКОЙ", bool(findings) and "ПРЕДПОСЫЛКА" in findings[0],
              findings[0] if findings else "нет находки")

        # 5. ПРЕДПОСЫЛКА: файла объявления нет вовсе.
        findings, census = run(tree(base / "no-decl", decl=None))
        check("отсутствующее объявление — находка", len(findings) == 1)
        check("при отсутствии объявления перепись НЕ засчитывает его прочитанным",
              census.files_read == 0, f"files_read={census.files_read}")

        # 6. Разбор формата: подстановки выкинуты, флаги не утекают в текст.
        parts = ct_mod.invariant_parts("resource %-10s of %d: %s must be canonical form")
        check("подстановки с флагами/шириной не попадают в неизменяемую часть",
              all("10s" not in p and "%" not in p for p in parts), repr(parts))
        check("длинный неизменяемый кусок сохранён",
              any("must be canonical form" in p for p in parts), repr(parts))

    if FAILURES:
        print(f"\nSELFTEST FAIL: {len(FAILURES)} — {', '.join(FAILURES)}", file=sys.stderr)
        return 1
    print("\nSELFTEST OK — судья краснеет на расхождении, молчит на законном дереве, "
          "и не молчит на своей невыполненной предпосылке.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
