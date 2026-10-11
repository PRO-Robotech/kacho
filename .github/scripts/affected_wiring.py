#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Сверка проводки процессов запроса с картой `.github/ci-paths.json` в обе стороны (kacho#3148).

Каждое задание процесса, идущего на `pull_request`, обязано стоять в группе карты либо в
`always`, нести условие выбора своего детектора и `needs:` на него; запись карты без
задания — находка; процесс без детектора идёт целиком.

ПОЧЕМУ ОТДЕЛЬНЫЙ ФАЙЛ, А НЕ КЛЮЧ `affected.py`. Сверке нужен PyYAML, детектору — только
json. Детектор зовут задания `changes` всех процессов запроса, и всё, что импортирует
скрипт задания, задание обязано поставить (`assert-jobs-provide-python-modules.py` судит
импорты разбором, включая импорт внутри функции). Пока сверка жила в `affected.py`,
шесть заданий `changes` числились зовущими скрипт, которому нужен PyYAML, и не
ставящими его. Сверку зовёт задание `authz-artifacts` в `ci.yaml`, которое PyYAML ставит.

КОДЫ: 0 — проводка сходится; 1 — находки (перечень); 2 — карта не разобрана.

Локально: `python3 .github/scripts/affected_wiring.py .github/workflows`.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

import yaml

sys.path.insert(0, str(Path(__file__).resolve().parent))
from affected import GREEN, MAP, RED, VOID, MapError, PathMap, load_map  # noqa: E402


# ── проводка: каждое задание процессов запроса — в карте или в `always` ──────

def check_wiring(m: PathMap, workflows: Path) -> list[str]:
    """Находки проводки. Пустой перечень — проводка сходится с картой в обе стороны."""
    findings: list[str] = []
    seen: set[str] = set()
    examined = 0
    for wf in sorted({*workflows.glob("*.yml"), *workflows.glob("*.yaml")}):
        doc = yaml.safe_load(wf.read_text(encoding="utf-8"))
        on = doc.get("on", doc.get(True)) if isinstance(doc, dict) else None
        if not (isinstance(on, dict) and "pull_request" in on):
            continue
        jobs = doc.get("jobs") or {}
        detector = m.detectors.get(wf.name)
        if detector is None:
            # Процесс без детектора идёт целиком; в карте его заданий быть не должно.
            for jid in jobs:
                key = f"{wf.name}:{jid}"
                examined += 1
                seen.add(key)
                if key in m.all_jobs():
                    findings.append(f"{key}: в карте, а детектора у процесса нет — выбор не "
                                    f"исполнится")
                elif key not in m.always:
                    findings.append(f"{key}: процесс без детектора не объявлен в `always`")
            continue
        if detector not in jobs:
            findings.append(f"{wf.name}: детектор «{detector}» объявлен картой, а задания нет")
        else:
            # Детектор берёт из выборки только .github: каталог по умолчанию процесса
            # (defaults.run.working-directory) его шаг не переживёт — шаг не стартует,
            # и все задания процесса пропущены (ui.yml, kacho#3149).
            wd = ((doc.get("defaults") or {}).get("run") or {}).get("working-directory")
            steps = (jobs[detector] or {}).get("steps") or []
            runs = [st for st in steps if isinstance(st, dict) and "run" in st]
            if wd and wd != "." and not all(st.get("working-directory") == "." for st in runs):
                findings.append(f"{wf.name}: детектор «{detector}» исполняется в каталоге по "
                                f"умолчанию «{wd}», которого в его выборке нет — нужен "
                                f"working-directory: .")
        for jid, job in jobs.items():
            key = f"{wf.name}:{jid}"
            examined += 1
            seen.add(key)
            if jid == detector:
                continue
            cond = str(job.get("if", "")) if isinstance(job, dict) else ""
            needs = job.get("needs", []) if isinstance(job, dict) else []
            needs = [needs] if isinstance(needs, str) else list(needs or [])
            if key in m.always:
                if f"'{jid}'" in cond:
                    findings.append(f"{key}: в `always`, а условие выбирает его по карте")
                continue
            if key not in m.all_jobs():
                findings.append(f"{key}: задания нет ни в одной группе карты и нет в `always` — "
                                f"на черновике его не выберет никто")
                continue
            want = (f"needs.{detector}.outputs.full == 'true' || contains(fromJSON("
                    f"needs.{detector}.outputs.jobs || '[]'), '{jid}')")
            if want not in cond or detector not in needs:
                findings.append(f"{key}: условие не несёт выбора детектора «{want}» "
                                f"(или нет needs: {detector})")
    for key in sorted((m.all_jobs() | set(m.always)) - seen):
        findings.append(f"{key}: в карте, а задания в процессах запроса нет — запись без предмета")
    if not examined:
        findings.append("осмотрено ноль заданий процессов запроса — это не «находок нет»")
    return findings


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("workflows", type=Path, metavar="WORKFLOWS_DIR")
    ap.add_argument("--map", type=Path, default=MAP)
    args = ap.parse_args(argv)
    try:
        m = load_map(args.map)
    except MapError as exc:
        print(f"ОТКАЗ: {exc}", file=sys.stderr)
        return VOID
    found = check_wiring(m, args.workflows)
    print(f"проводка: групп {len(m.groups)}, заданий в карте {len(m.all_jobs())}, "
          f"всегда идущих {len(m.always)}, детекторов {len(m.detectors)}; находок {len(found)}")
    for f in found:
        print(f"   ✗ {f}")
    return RED if found else GREEN


if __name__ == "__main__":
    sys.exit(main())
