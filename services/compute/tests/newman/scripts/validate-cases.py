#!/usr/bin/env python3

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""
tests/newman/scripts/validate-cases.py — сверщик переписи кейсов compute.

ЗАЧЕМ ОН ЗАВЁЛСЯ ИМЕННО ЗДЕСЬ. У vpc и registry сверщик был, у compute — нет, и
слепая зона оказалась ровно там, где его не было: в `docs/CASES-INDEX.md` выжил
раздел «Instance (77 кейсов) — `cases/instance.py`» для файла, удалённого из дерева,
а перепись в шапке того же файла разошлась с деревом (111/43 против 117/49). Оба
дефекта — утверждения о покрытии, которых никто не проверял. Гейт есть у двоих из
трёх — значит третий и есть место находки.

Запускать до newman-шага (чистый Python, без сети):

    python3 tests/newman/scripts/validate-cases.py

Проверяет ТРИ вещи, и каждая заведена против того, что здесь реально сгнило:

  1. НЕТ ДУБЛЕЙ case-id по всем модулям, которые собирает gen.py.

  2. ПЕРЕПИСЬ В ШАПКЕ CASES-INDEX СОВПАДАЕТ С ДЕРЕВОМ — и всего, и по каждому
     модулю. Число, переписанное рукой, дрейфует само; вопрос лишь в том, заметит
     это гейт или следующий читатель, который на него положится.

  3. НИ ОДИН ЗАГОЛОВОК CASES-INDEX НЕ НАЗЫВАЕТ ФАЙЛ КЕЙСОВ, КОТОРОГО НЕТ. Раздел про
     удалённый файл читается как описание покрытия и переживает своё содержимое —
     именно так здесь и вышло: `## Instance (77 кейсов) — cases/instance.py` жил уже
     после удаления файла.

     Проверяются ЗАГОЛОВКИ, а не весь текст, и это не поблажка. Заголовок — то, что
     читатель принимает за «это есть»; упоминание удалённого файла в ПРОЗЕ, прямо
     говорящей о его удалении (`## Zone / Region — removed (Stage S7)` ниже, и раздел
     про снятый перечень инстанса), — законное историческое свидетельство, и гейт,
     запрещающий его, заставил бы стирать объяснение вместе с ложью. Проверка на весь
     текст краснела на всех трёх таких местах — то есть ловила форму, а не существо.

ЧЕГО ЗДЕСЬ НЕТ И ПОЧЕМУ — это отступление от соседей, и оно объявлено, а не умолчано.
У vpc и registry есть четвёртая проверка: каждый case-id обязан быть каталогизирован в
CASES-INDEX (буквально либо суффиксным шаблоном), иначе — отказ. Для compute она
потребовала бы вернуть в документ ПОКАЗАТЕЛЬНЫЙ ПЕРЕЧЕНЬ всех 117 кейсов — то есть
вторую копию содержимого `cases/*.py`, чей дрейф и был здешним дефектом (перечень
удалённого файла жил в документе, противореча переписи в его же шапке). Требовать
копию, которую мы только что убрали как источник лжи, значит завести гейт против
самого решения. Поэтому источник истины по составу кейсов — `cases/*.py`, а документ
отвечает за ЧИСЛА, и за них отвечает проверка 2.

Все три проверки доказаны инъекцией в обе стороны: сдвиг переписи на единицу, лишний
модуль в переписи и ссылка на несуществующий файл дают красное с координатой;
законное добавление кейса вместе с обновлённой переписью — молчание.
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SCRIPTS_DIR = ROOT / "scripts"
CASES_DIR = ROOT / "cases"
INDEX_FILE = ROOT / "docs" / "CASES-INDEX.md"

_ID_RE = re.compile(r"""id\s*=\s*f?["']([A-Z0-9][A-Z0-9_{}:.-]+)["']""")
# Перепись: «Всего (по `gen.py`): **117 кейсов** — instance-redesign 49, authz-deny 42, …»
_CENSUS_TOTAL_RE = re.compile(r"\*\*(\d+)\s+кейс[а-я]*\*\*")
_CENSUS_ITEM_RE = re.compile(r"([a-z][a-z0-9-]*)\s+(\d+)")
_CASES_FILE_REF_RE = re.compile(r"`?cases/([A-Za-z0-9_-]+)\.py`?")
_HEADING_RE = re.compile(r"^#{1,6}\s")

sys.path.insert(0, str(SCRIPTS_DIR))



def _all_cases() -> list[tuple[str, str]]:
    """Загрузить модули кейсов ТАК ЖЕ, как gen.py → [(case_id, module), …]."""
    import gen  # noqa: E402

    out: list[tuple[str, str]] = []
    for f in sorted(CASES_DIR.glob("*.py")):
        if f.name.startswith("_"):
            continue
        mod = gen.load_cases_module(f)
        for c in getattr(mod, "CASES", []):
            out.append((c.id, f.stem))
    return out


def _census_from_index(index_text: str) -> tuple[int | None, dict[str, int]]:
    """Прочитать заявленную перепись из шапки CASES-INDEX."""
    total = None
    per: dict[str, int] = {}
    for line in index_text.splitlines():
        m = _CENSUS_TOTAL_RE.search(line)
        if not m:
            continue
        total = int(m.group(1))
        # Модульные числа идут в этой же фразе и могут переноситься на след. строку;
        # читаем оба варианта, начиная с тире.
        tail = line.split("—", 1)[1] if "—" in line else ""
        idx = index_text.splitlines().index(line)
        if idx + 1 < len(index_text.splitlines()):
            tail += " " + index_text.splitlines()[idx + 1]
        for name, num in _CENSUS_ITEM_RE.findall(tail):
            per[name] = int(num)
        break
    return total, per


def main() -> int:
    errors: list[str] = []

    try:
        cases = _all_cases()
    except Exception as exc:  # noqa: BLE001 — предъявить как провал сверки
        sys.stderr.write(f"validate-cases: FAIL — модули кейсов не загрузились: {exc}\n")
        return 1
    if not cases:
        # «Ноль находок» обязано быть отличимо от «ноль прочитанного».
        sys.stderr.write("validate-cases: FAIL — ни одного кейса не прочитано\n")
        return 1

    # ---- (1) дубли case-id ----
    seen: dict[str, str] = {}
    for case_id, mod in cases:
        if case_id in seen:
            errors.append(
                f"дубль case-id {case_id!r}: есть и в {seen[case_id]}, и в {mod} "
                f"(case-id обязан быть уникальным)"
            )
        else:
            seen[case_id] = mod

    index_text = INDEX_FILE.read_text() if INDEX_FILE.exists() else ""
    if not index_text:
        errors.append(f"нет файла {INDEX_FILE}")

    # ---- (2) перепись в шапке совпадает с деревом ----
    actual_per: dict[str, int] = {}
    for _cid, mod in cases:
        actual_per[mod] = actual_per.get(mod, 0) + 1
    actual_total = len(cases)

    claimed_total, claimed_per = _census_from_index(index_text)
    if claimed_total is None:
        errors.append(
            "в шапке docs/CASES-INDEX.md нет переписи вида «**<N> кейсов** — <модуль> <n>, …». "
            "Без неё читателю нечем сверить покрытие, а гейту нечего проверять."
        )
    else:
        if claimed_total != actual_total:
            errors.append(
                f"перепись разошлась с деревом: в шапке заявлено {claimed_total} кейсов, "
                f"gen.py собирает {actual_total}. Сверить: `python3 scripts/gen.py`."
            )
        for mod, n in sorted(actual_per.items()):
            if mod not in claimed_per:
                errors.append(
                    f"модуль {mod!r} ({n} кейсов) в переписи шапки не назван вовсе"
                )
            elif claimed_per[mod] != n:
                errors.append(
                    f"перепись модуля {mod!r}: заявлено {claimed_per[mod]}, в дереве {n}"
                )
        for mod in sorted(claimed_per):
            if mod not in actual_per:
                errors.append(
                    f"перепись называет модуль {mod!r}, которого в cases/ нет — "
                    f"утверждение о покрытии без предмета"
                )

    # ---- (3) CASES-INDEX не ссылается на удалённый файл кейсов ----
    for lineno, line in enumerate(index_text.splitlines(), start=1):
        if not _HEADING_RE.match(line):
            continue
        for name in _CASES_FILE_REF_RE.findall(line):
            if not (CASES_DIR / f"{name}.py").exists():
                errors.append(
                    f"docs/CASES-INDEX.md:{lineno} — заголовок называет `cases/{name}.py`, "
                    f"которого в дереве нет: {line.strip()!r}. Заголовок читается как "
                    f"описание покрытия и переживает своё содержимое."
                )

    if errors:
        sys.stderr.write("validate-cases: FAIL\n")
        for e in errors:
            sys.stderr.write("  - " + e + "\n")
        return 1
    print(
        f"validate-cases: OK — {len(seen)} уникальных case-id, дублей нет; "
        f"перепись шапки совпадает с деревом "
        f"({actual_total} кейсов: " +
        ", ".join(f"{m} {n}" for m, n in sorted(actual_per.items())) + ")"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
