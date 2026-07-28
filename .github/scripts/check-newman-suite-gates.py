#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""check-newman-suite-gates — исполняемая суита обязана иметь ВЕРДИКТ и СЛЕД.

ЧТО ЛОВИТ. Список исполняемых суит задан в одном файле (умолчание ``SERVICES`` в
``deploy/scripts/newman-parallel.sh``), а два его следствия — в другом: шаг-гейт
``assert-suites-green.sh`` и путь в ``upload-artifact`` (оба в
``.github/workflows/e2e-newman.yml``). Добавить сервис в список — одна строка;
забыть два следствия — ничего не происходит: суита исправно гоняется, тратит
время стенда, и при этом её краснота не валит job, а её отчёты не сохраняются.
Ровно это и случилось с ``api-gateway``: суита исполнялась, шага-гейта не было,
маска артефактов её не покрывала.

Симптом класса неотличим от «всё хорошо»: в логе job'а прогон виден, отчёт
печатается, job зелёный. Поэтому связь проверяется отдельно и механически.

ПОЧЕМУ РАЗБОР YAML, А НЕ grep. Путь артефакта пишется двумя равноправными
формами — ``path: <glob>`` и блочный список из нескольких строк. Гейт, знающий
только одну из них, краснеет на законной записи другой формы; такой гейт снимут
при первом же ложном срабатывании. Значения читаются из разобранного документа,
поэтому обе формы (и любая третья) сверяются одинаково.

ЧЕГО НЕ ПРОВЕРЯЕТ (осознанно): содержимое кейсов и порог покрытия — это
``assert-suites-green.sh`` и ``coverage.py``. Здесь только связность трёх списков.

Запуск:      python3 .github/scripts/check-newman-suite-gates.py
Самопроверка: --self-test — вносит дефект в КОПИЮ workflow и требует, чтобы гейт
              на нём покраснел, а на законной записи другой формы — смолчал.
"""

from __future__ import annotations

import fnmatch
import pathlib
import re
import sys

import yaml

REPO = pathlib.Path(__file__).resolve().parents[2]
PARALLEL = REPO / "deploy/scripts/newman-parallel.sh"
WORKFLOW = REPO / ".github/workflows/e2e-newman.yml"

# Зеркалит suite_dir() в newman-parallel.sh: суита шлюза живёт в gateway/, а не в
# services/. Расхождение поймает проверка существования каталога.
def suite_dir(svc: str) -> str:
    return "gateway/tests/newman" if svc == "api-gateway" else f"services/{svc}/tests/newman"


def executed_services(parallel_sh: str) -> list[str]:
    """Умолчание SERVICES — ровно тот список, который гоняет workflow (он его не переопределяет)."""
    m = re.search(r'^SERVICES="\$\{SERVICES:-([^}]*)\}"', parallel_sh, re.M)
    if not m:
        raise SystemExit(f"FAIL: не удалось прочитать умолчание SERVICES из {PARALLEL}")
    return m.group(1).split()


def gated_dirs(job_steps: list[dict]) -> set[str]:
    """working-directory шагов, чей run зовёт assert-suites-green.sh."""
    out = set()
    for st in job_steps:
        run = str(st.get("run") or "")
        wd = st.get("working-directory")
        if wd and "assert-suites-green.sh" in run:
            out.add(str(wd).rstrip("/"))
    return out


def artifact_globs(job_steps: list[dict]) -> list[str]:
    """Пути upload-artifact, независимо от того, одной строкой они записаны или списком."""
    out: list[str] = []
    for st in job_steps:
        if not str(st.get("uses") or "").startswith("actions/upload-artifact"):
            continue
        path = (st.get("with") or {}).get("path")
        if path is None:
            continue
        if isinstance(path, list):
            items = path
        else:
            items = str(path).splitlines()
        out += [p.strip() for p in items if p.strip()]
    return out


def main() -> int:
    services = executed_services(PARALLEL.read_text())
    wf = yaml.safe_load(WORKFLOW.read_text())
    steps = [s for job in wf["jobs"].values() for s in job.get("steps", [])]

    gated = gated_dirs(steps)
    globs = artifact_globs(steps)

    fails: list[str] = []
    for svc in services:
        d = suite_dir(svc)

        if not (REPO / d).is_dir():
            fails.append(f"{svc}: суита исполняется, но каталога {d} нет")
            continue

        if d not in gated:
            fails.append(
                f"{svc}: суита исполняется, но в {WORKFLOW.name} нет шага-гейта "
                f"(assert-suites-green.sh) с working-directory: {d}\n"
                f"      → её краснота не валит job: прогон идёт, вердикт не выносится."
            )

        # Проверяем РАСКРЫТИЕМ glob'а, а не подстрокой: маска обязана реально
        # покрывать отчёт этой суиты.
        probe = f"{d}/out/case.json"
        if not any(fnmatch.fnmatch(probe, g) for g in globs):
            fails.append(
                f"{svc}: отчёты {d}/out/*.json не покрыты ни одним путём upload-artifact "
                f"в {WORKFLOW.name}\n"
                f"      → прогон не оставляет следа: разбирать красноту будет нечем."
            )

    if fails:
        for f in fails:
            print(f"FAIL: {f}", file=sys.stderr)
        print(
            "\nИсполняемая суита без гейта — прогон, результат которого ни на что не влияет;\n"
            "без артефакта — прогон, который нечем разобрать. Либо заведи оба следствия,\n"
            f"либо убери сервис из умолчания SERVICES в {PARALLEL}.",
            file=sys.stderr,
        )
        return 1

    print(
        f"OK: у каждой из {len(services)} исполняемых суит есть шаг-гейт и путь "
        f"в upload-artifact ({' '.join(services)})"
    )
    return 0


def self_test() -> int:
    """Гейт обязан краснеть на внесённом дефекте и молчать на законной записи той же формы.

    Проверяется на КОПИИ workflow в памяти — рабочее дерево не трогаем.
    """
    services = executed_services(PARALLEL.read_text())
    wf = yaml.safe_load(WORKFLOW.read_text())
    steps = [s for job in wf["jobs"].values() for s in job.get("steps", [])]
    rc = 0

    def verdict(svcs: list[str], gated: set[str], globs: list[str]) -> list[str]:
        bad = []
        for svc in svcs:
            d = suite_dir(svc)
            if d not in gated:
                bad.append(f"{svc}:нет-гейта")
            if not any(fnmatch.fnmatch(f"{d}/out/case.json", g) for g in globs):
                bad.append(f"{svc}:нет-артефакта")
        return bad

    gated, globs = gated_dirs(steps), artifact_globs(steps)

    # (0) как есть — обязан молчать
    v = verdict(services, gated, globs)
    print(f"  (0) дерево как есть            → {'МОЛЧИТ' if not v else 'красный: ' + ', '.join(v)}")
    rc |= 0 if not v else 1

    # (A) инъекция: снять шаг-гейт суиты шлюза
    v = verdict(services, gated - {"gateway/tests/newman"}, globs)
    hit = "api-gateway:нет-гейта" in v
    print(f"  (A) снят шаг-гейт api-gateway  → {'КРАСНЫЙ с координатой' if hit else 'ПРОПУСТИЛ'}")
    rc |= 0 if hit else 1

    # (A2) инъекция: снять путь артефакта суиты шлюза
    v = verdict(services, gated, [g for g in globs if not g.startswith("gateway/")])
    hit = "api-gateway:нет-артефакта" in v
    print(f"  (A2) снят путь артефакта       → {'КРАСНЫЙ с координатой' if hit else 'ПРОПУСТИЛ'}")
    rc |= 0 if hit else 1

    # (B) КОНТРОЛЬ: те же пути записаны ОДНОЙ строкой через inline-форму —
    #     законная запись, гейт обязан смолчать (иначе он ловит форму, а не существо).
    inline = yaml.safe_load(
        "steps:\n"
        "  - uses: actions/upload-artifact@v7\n"
        "    with:\n"
        "      path: services/*/tests/newman/out/*.json\n"
        "  - uses: actions/upload-artifact@v7\n"
        "    with:\n"
        "      path: gateway/tests/newman/out/*.json\n"
    )["steps"]
    v = verdict(services, gated, artifact_globs(inline))
    print(f"  (B) те же пути inline-формой   → {'МОЛЧИТ' if not v else 'ЛОЖНОЕ СРАБАТЫВАНИЕ: ' + ', '.join(v)}")
    rc |= 0 if not v else 1

    # (C) КОНТРОЛЬ: сервис ОСОЗНАННО выведен из прогона — вместе с ним законно
    #     исчезают и его шаг-гейт, и его путь артефакта. Гейт обязан смолчать:
    #     он требует следствий только для того, что реально исполняется.
    kept = [s for s in services if s != "api-gateway"]
    v = verdict(
        kept,
        gated - {"gateway/tests/newman"},
        [g for g in globs if not g.startswith("gateway/")],
    )
    print(f"  (C) api-gateway убран из SERVICES → {'МОЛЧИТ' if not v else 'ЛОЖНОЕ СРАБАТЫВАНИЕ: ' + ', '.join(v)}")
    rc |= 0 if not v else 1

    print("самопроверка: " + ("ПРОЙДЕНА" if rc == 0 else "ПРОВАЛЕНА"))
    return rc


if __name__ == "__main__":
    sys.exit(self_test() if "--self-test" in sys.argv[1:] else main())
