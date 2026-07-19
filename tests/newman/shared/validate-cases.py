#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""
tests/newman/shared/validate-cases.py — ЕДИНЫЙ canonical MANDATORY case-validation
gate (H0). Раньше каждый сервис держал СВОЮ копию (vpc/nlb/registry/storage) либо
не имел вовсе (iam/compute полагались на `gen.py --validate` / чужой coverage.py).

Pure-Python, без сети. Гоняется в CI **до** тяжёлого newman-шага. Hard-fail (exit 1):

  1. Дубль case-id среди ВСЕХ кейсов, что генерирует `gen.py` (внутри файла, между
     файлами, в helper-блоках) — case-id обязан быть уникален.
  2. Каждый case-id зафиксирован в каталоге паттернов `docs/CASES-INDEX.md`:
       (a) суффикс-паттерн `*-<SUFFIX>` ИЛИ литеральный case-id в тексте индекса, ЛИБО
       (b) помечен `# index: <ref>` рядом с `id="..."` (1-2 строки выше) — инстанс
           известного паттерна.
     Исключение: `internal-*.py` (admin/IPAM RPC) — каталогизированы заметкой,
     индекс-покрытие не требуется (но dup-id-проверка работает и для них).

Использование (сервис-агностично — service newman-root параметризуем):
    # из scripts/ сервиса (как раньше):
    python3 ../../../../tests/newman/shared/validate-cases.py
    # или явно указать newman-root:
    python3 tests/newman/shared/validate-cases.py services/<svc>/tests/newman
    # или через env:
    NEWMAN_ROOT=services/<svc>/tests/newman python3 tests/newman/shared/validate-cases.py

`gen.load_cases_module` берётся из `<root>/scripts/gen.py` (каждый сервис оставляет свои
resource-специфичные define/emit-блоки в своём gen.py; helper-namespace — из shared/harness.py).

Тонкий per-service shim (H3) может делать:
    import runpy, os; os.environ["NEWMAN_ROOT"] = str(ROOT)
    runpy.run_path(str(SHARED / "validate-cases.py"), run_name="__main__")
"""
from __future__ import annotations

import os
import re
import sys
from pathlib import Path


def _resolve_root(argv: list[str]) -> Path:
    """service newman-root: argv[1] → $NEWMAN_ROOT → cwd-walkup (…/tests/newman)."""
    if len(argv) > 1 and not argv[1].startswith("-"):
        return Path(argv[1]).resolve()
    env = os.environ.get("NEWMAN_ROOT")
    if env:
        return Path(env).resolve()
    # walk up from cwd to a dir that looks like tests/newman (has cases/ + scripts/)
    here = Path.cwd().resolve()
    for cand in [here, *here.parents]:
        if (cand / "cases").is_dir() and (cand / "scripts" / "gen.py").is_file():
            return cand
    # last resort: cwd
    return here


_ID_RE = re.compile(r"""id\s*=\s*["']([A-Z0-9][A-Z0-9_-]+)["']""")
_INDEX_TAG_RE = re.compile(r"#\s*index:\s*(\S+)")
INTERNAL_FILE_RE = re.compile(r"^internal-")


def _suffix(case_id: str) -> str:
    """`NIC-CR-CRUD-OK` -> `CR-CRUD-OK` (drop domain-prefix before first '-')."""
    parts = case_id.split("-")
    return "-".join(parts[1:]) if len(parts) > 1 else case_id


def _text_tags(cases_dir: Path) -> dict[str, set[str]]:
    """{case_id: {filenames where id= has a `# index:` tag on/above the line}}."""
    tagged: dict[str, set[str]] = {}
    for f in sorted(cases_dir.glob("*.py")):
        lines = f.read_text().splitlines()
        for i, line in enumerate(lines):
            m = _ID_RE.search(line)
            if not m:
                continue
            case_id = m.group(1)
            window = "\n".join(lines[max(0, i - 2): i + 1])
            if _INDEX_TAG_RE.search(window):
                tagged.setdefault(case_id, set()).add(f.name)
    return tagged


def _all_cases(scripts_dir: Path, cases_dir: Path) -> list[tuple[str, str]]:
    """Import case-modules exactly as gen.py does → [(case_id, filename), ...] in
    generation order (helper-blocks included)."""
    sys.path.insert(0, str(scripts_dir))
    import gen  # noqa: E402  (lazy — sys.path adjusted above)

    out: list[tuple[str, str]] = []
    globber = sorted(f for f in cases_dir.glob("*.py") if not f.name.startswith("_"))
    for f in globber:
        mod = gen.load_cases_module(f)
        for c in getattr(mod, "CASES", []):
            out.append((c.id, f.name))
    return out


def main(argv: list[str]) -> int:
    root = _resolve_root(argv)
    scripts_dir = root / "scripts"
    cases_dir = root / "cases"
    index_file = root / "docs" / "CASES-INDEX.md"

    if not (scripts_dir / "gen.py").is_file():
        sys.stderr.write(f"validate-cases: FAIL — no gen.py under {scripts_dir}\n")
        return 1

    errors: list[str] = []
    try:
        cases = _all_cases(scripts_dir, cases_dir)
    except Exception as exc:  # noqa: BLE001 — surface as a validation error
        sys.stderr.write(f"validate-cases: FAIL — could not load case-modules: {exc}\n")
        return 1
    if not cases:
        sys.stderr.write("validate-cases: FAIL — no cases\n")
        return 1

    # ---- (1) duplicate case-id ----
    seen: dict[str, str] = {}
    for case_id, fname in cases:
        if case_id in seen:
            errors.append(
                f"duplicate case-id {case_id!r}: appears in {seen[case_id]} and {fname} "
                f"(case-id must be unique across all cases)"
            )
        else:
            seen[case_id] = fname

    # ---- (2) every case-id catalogued in CASES-INDEX.md or tagged `# index:` ----
    index_text = index_file.read_text() if index_file.exists() else ""
    if not index_text:
        errors.append(f"missing {index_file}")
    tagged = _text_tags(cases_dir)
    checked: set[str] = set()
    for case_id, fname in cases:
        if case_id in checked:
            continue
        checked.add(case_id)
        if INTERNAL_FILE_RE.match(fname):
            continue  # admin/IPAM — catalogued by note
        if case_id in tagged:
            continue
        suf = _suffix(case_id)
        if f"*-{suf}" in index_text or case_id in index_text:
            continue
        errors.append(
            f"case {case_id!r} (from {fname}) not catalogued in docs/CASES-INDEX.md.\n"
            f"    → NEW unique pattern: add `*-{suf}` (or `{case_id}`) to docs/CASES-INDEX.md;\n"
            f"    → INSTANCE of an existing pattern: tag the id= line `# index: <pattern-ref>`."
        )

    if errors:
        sys.stderr.write("validate-cases: FAIL\n")
        for e in errors:
            sys.stderr.write("  - " + e + "\n")
        return 1
    print(
        f"validate-cases: OK — {len(seen)} unique case-ids, no duplicates, "
        f"all catalogued (CASES-INDEX / # index:)"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
