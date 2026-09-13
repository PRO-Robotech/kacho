#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Задание, зовущее python-скрипт со сторонним модулем, обязано этот модуль ПОСТАВИТЬ.

ПРЕДМЕТ. Условие прогона либо создаётся заданием, либо предполагается в образе ранера.
Под меткой `ubuntu-latest` работают ДВА пула: образ GitHub-hosted несёт `pyyaml`, `pyjwt`
и `pip`, наши `pro-robotech-runner-*` — нет. Кому достанется задание, решает очередь,
поэтому один и тот же коммит зеленел и краснел по жребию.

ЧТО НАБЛЮДАЛОСЬ (2026-09-13, один день, четыре задания, каждое — отдельный прогон):
  `ОТКАЗ: нет модуля yaml — судить не о чем`            задание чартов
  `/usr/bin/python3: No module named pip`               задание каталога прав
  `ModuleNotFoundError: No module named 'jwt'`          самопроверка церемонии посева
  `ModuleNotFoundError: No module named 'yaml'`         сквозные пробы консоли
Каждый круг стоил ПРОГОНА КОНВЕЙЕРА: отказ виден только там, где модуля нет, а «там» —
половина пула. Поэтому предмет закрывается переписью объявлений, а не починкой по одному.

ЕДИНИЦА СЧЁТА — задание, а не файл и не вхождение. Нужда задания выводится из скриптов,
которые оно ЗОВЁТ, с транзитивным раскрытием ЛОКАЛЬНЫХ импортов: `prodseed_ceremony.py`
сторонних модулей не импортирует вовсе, но тянет соседний `mint_rs256.py`, а тот — `jwt`.
Без раскрытия перепись назвала бы ноль там, где нужда есть.

ЧТО ГЕЙТ ТРЕБУЕТ, и это три отдельных утверждения:
  1. модуль, нужный скриптам задания, задание СТАВИТ (`pip install <пакет>`);
  2. если задание вообще ставит модули, у него есть `actions/setup-python` — системный
     интерпретатор наших ранеров идёт без `pip`, и установка была бы объявлена и
     неисполнима;
  3. установка идёт ПОСЛЕ установки интерпретатора: `setup-python` приносит ЧИСТЫЙ
     интерпретатор и отнимает модули, которые системный нёс, — так уже ломался гейт
     прежнего издателя, работавший до правки.

ИМЯ МОДУЛЯ И ИМЯ ПАКЕТА РАЗЛИЧАЮТСЯ, и это не мелочь: `import yaml` ставится как
`pyyaml`, `import jwt` — как `pyjwt`. Сверять их напрямую значит краснеть на верном
объявлении.

ГРАНИЦА, названная честно. Гейт судит ОБЪЯВЛЕНИЕ, а не прогон: он не запускает python и
не проверяет, что установка удалась. Он также не видит модулей, которые скрипт
импортирует внутри функции по условию, — разбор берёт импорты верхнего уровня, потому что
именно они падают при запуске.

ПУСТОЙ ОБХОД — НАХОДКА: «ноль заданий без модуля» обязано быть отличимо от «ни одного
объявления не прочитано».
"""

from __future__ import annotations

import ast
import glob
import io
import os
import re
import subprocess
import sys

import yaml

# Имя модуля → имя пакета для установки. Список закрытый: расширяется вместе с деревом.
# Имя модуля → имя пакета. У `jwt` это ПАКЕТ С ДОПОЛНЕНИЕМ, и дополнение несущее:
# `pip install pyjwt` ставит библиотеку без крипто-бэкенда, и подпись ES256/RS256 падает
# `NotImplementedError: Algorithm 'ES256' could not be found. Do you have cryptography
# installed?`. Наблюдалось 2026-09-13 (задание 103675929789): стенд поднялся, посев упал,
# «ПРОГОН НЕДЕЙСТВИТЕЛЕН», 0 из 9 суит отчитались. Сам `tests/authz-fixtures/mint_rs256.py`
# требование объявляет в шапке: «Requires PyJWT + cryptography (ES256 signing)».
PACKAGE_OF = {"yaml": "pyyaml", "jwt": "pyjwt[crypto]"}
STD = set(sys.stdlib_module_names) | {"__future__"}
PIP_NOISE = {"pip", "install", "python3", "-m", "--quiet", "--disable-pip-version-check",
             "--no-input", "--upgrade", "set", "-euo", "pipefail"}

# Признак того, что задание ПОДНИМАЕТ СТЕНД. Такому заданию нужен ПОЛНЫЙ набор сторонних
# модулей дерева, а не только тот, что виден в его `run:`.
#
# ПОЧЕМУ ТАК, а не точным графом вызовов: посев стенда доходит до python через цепочку
# `stand-up.sh` → `make dev-up` → `tests/authz-fixtures/setup.sh` → `prodseed_all.py` →
# `mint_rs256.py` → `jwt`. Раскрыть её объявлением нельзя — в середине Makefile, и
# пройти его разбором значит написать второй make. Наблюдалось 2026-09-13 (задание
# 103673961666): задание ставило `pyyaml`, посев упал `No module named 'jwt'`, и четыре
# шарда e2e дали «TOTAL: 0/18 collection(s) reported» — стенд поднялся, а вердикта о
# продукте не было.
#
# Поэтому для поднимающих стенд требование грубее и ВЕРНЕЕ: весь набор дерева. Цена —
# лишний пакет там, где он может не понадобиться; цена ошибки в другую сторону — прогон
# стенда без вердикта.
STAND_MARKERS = ("stand-up.sh", "dev-up")


def tracked_files() -> set[str]:
    p = subprocess.run(["git", "ls-files"], capture_output=True, text=True)
    return set(p.stdout.split())


def third_party(path: str, tracked: set[str], seen: set[str] | None = None,
                basenames: dict[str, str] | None = None) -> set[str]:
    """Сторонние модули скрипта. Локальные импорты раскрываются транзитивно.

    РАЗБОР, А НЕ ОБРАЗЕЦ. Первая редакция искала импорты регулярным выражением и
    приносила `the` — слово из прозы внутри докстринга. Подстрочный счёт не отличает
    импорт от текста о нём, и это тот же класс, который корпус ловит у гейтов.

    ЛОКАЛЬНОСТЬ — ПО ВСЕМУ ДЕРЕВУ, а не по соседнему каталогу: `gen_shared` лежит в
    другом каталоге и подключается через `sys.path`, поэтому проверка «файл рядом»
    называла его сторонним.
    """
    if seen is None:
        seen = set()
    if basenames is None:
        basenames = {os.path.basename(f)[:-3]: f for f in tracked if f.endswith(".py")}
    if path in seen or path not in tracked:
        return set()
    seen.add(path)
    try:
        tree = ast.parse(io.open(path, encoding="utf-8").read())
    except (OSError, SyntaxError):
        return set()
    out: set[str] = set()
    here = os.path.dirname(path)
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            names = [a.name.split(".")[0] for a in node.names]
        elif isinstance(node, ast.ImportFrom):
            names = [(node.module or "").split(".")[0]] if node.level == 0 else []
        else:
            continue
        for mod in names:
            if not mod or mod in STD:
                continue
            local = os.path.join(here, mod + ".py")
            if local in tracked:
                out |= third_party(local, tracked, seen, basenames)
            elif mod in basenames:
                out |= third_party(basenames[mod], tracked, seen, basenames)
            else:
                out.add(mod)
    return out


def judge(jobs: list[dict], all_tree: set[str] | None = None) -> list[str]:
    """jobs: [{workflow, job, needs, pip, setup_python_at, pip_at, raises_stand}]"""
    findings = []
    full = {PACKAGE_OF.get(m, m) for m in (all_tree or set())}
    for j in jobs:
        need = {PACKAGE_OF.get(m, m) for m in j["needs"]}
        if j.get("raises_stand") and full:
            need |= full
        have = set(j["pip"])
        missing = sorted(need - have)
        if missing:
            findings.append(
                f'{j["workflow"]} / {j["job"]}: зовёт python-скрипты, которым нужны '
                f'{", ".join(missing)} — задание их НЕ ставит. На ранере без этих модулей '
                f'шаг не исполнится, и красное будет о ранере, а не о дереве'
            )
        if have and j["setup_python_at"] is None:
            findings.append(
                f'{j["workflow"]} / {j["job"]}: ставит модули ({", ".join(sorted(have))}), '
                f'но не ставит интерпретатор — `actions/setup-python` отсутствует. '
                f'Системный python наших ранеров идёт без pip: установка объявлена и неисполнима'
            )
        if have and j["setup_python_at"] is not None:
            early = [i for i in j["pip_at"] if i < j["setup_python_at"]]
            if early:
                findings.append(
                    f'{j["workflow"]} / {j["job"]}: установка модулей на шаге(ах) '
                    f'{early} идёт ДО `setup-python` (шаг {j["setup_python_at"]}) — '
                    f'модули лягут в другой интерпретатор'
                )
    return findings


def survey(root: str = ".") -> list[dict]:
    tracked = tracked_files()
    out = []
    for wf in sorted(glob.glob(os.path.join(root, ".github/workflows/*.y*ml"))):
        try:
            doc = yaml.safe_load(io.open(wf, encoding="utf-8").read())
        except (OSError, yaml.YAMLError):
            continue
        if not isinstance(doc, dict):
            continue
        for jname, j in (doc.get("jobs") or {}).items():
            steps = j.get("steps") or []
            needs: set[str] = set()
            pip: set[str] = set()
            pip_at: list[int] = []
            setup_at = None
            for i, s in enumerate(steps):
                if "setup-python" in str(s.get("uses") or "") and setup_at is None:
                    setup_at = i
                run = str(s.get("run") or "")
                if "pip install" in run:
                    pip_at.append(i)
                    for m in re.finditer(r"pip install([^\n]*)", run):
                        pip |= {w for w in m.group(1).split() if w not in PIP_NOISE
                                and not w.startswith("-")}
                for m in re.finditer(r"([\w./-]+\.py)", run):
                    needs |= third_party(m.group(1), tracked)
            raises = any(mark in str(s.get("run") or "") for s in steps
                         for mark in STAND_MARKERS)
            if needs or pip or raises:
                out.append({"workflow": os.path.basename(wf), "job": jname,
                            "needs": sorted(needs), "pip": sorted(pip),
                            "pip_at": pip_at, "setup_python_at": setup_at,
                            "raises_stand": raises})
    return out


def tree_third_party(tracked: set[str]) -> set[str]:
    """Сторонние модули ВСЕГО дерева — нужда задания, поднимающего стенд."""
    out: set[str] = set()
    for f in tracked:
        if f.endswith(".py"):
            out |= third_party(f, tracked)
    return out


def self_test() -> int:
    cases = [
        ("контроль: нужное ставится после интерпретатора → молчит",
         [{"workflow": "w", "job": "j", "needs": ["yaml"], "pip": ["pyyaml"],
           "pip_at": [2], "setup_python_at": 1}], 0, None),
        ("дефект: модуль нужен и не ставится → находка",
         [{"workflow": "w", "job": "j", "needs": ["yaml"], "pip": [],
           "pip_at": [], "setup_python_at": None}], 1, "НЕ ставит"),
        ("дефект: ставит модули без интерпретатора → находка про pip",
         [{"workflow": "w", "job": "j", "needs": ["yaml"], "pip": ["pyyaml"],
           "pip_at": [2], "setup_python_at": None}], 1, "без pip"),
        ("дефект: установка ДО интерпретатора → находка про другой интерпретатор",
         [{"workflow": "w", "job": "j", "needs": ["yaml"], "pip": ["pyyaml"],
           "pip_at": [1], "setup_python_at": 3}], 1, "ДО `setup-python`"),
        ("законный близнец: имя модуля против имени пакета (jwt→pyjwt[crypto]) → молчит",
         [{"workflow": "w", "job": "j", "needs": ["jwt"], "pip": ["pyjwt[crypto]"],
           "pip_at": [2], "setup_python_at": 1}], 0, None),
        ("дефект: поставлен модуль не тот, что нужен → находка",
         [{"workflow": "w", "job": "j", "needs": ["jwt"], "pip": ["pyyaml"],
           "pip_at": [2], "setup_python_at": 1}], 1, "pyjwt"),
        ("законный близнец: задание вообще без python → молчит",
         [{"workflow": "w", "job": "j", "needs": [], "pip": [],
           "pip_at": [], "setup_python_at": None}], 0, None),
        ("дефект: pyjwt БЕЗ крипто-дополнения → находка (ES256 не заработает)",
         [{"workflow": "w", "job": "j", "needs": ["jwt"], "pip": ["pyjwt"],
           "pip_at": [2], "setup_python_at": 1}], 1, "pyjwt[crypto]"),
        ("подъём стенда требует ПОЛНОГО набора дерева → находка при частичном",
         [{"workflow": "w", "job": "stand", "needs": ["yaml"], "pip": ["pyyaml"],
           "pip_at": [2], "setup_python_at": 1, "raises_stand": True}], 1, "pyjwt"),
        ("законный близнец: подъём с полным набором → молчит",
         [{"workflow": "w", "job": "stand", "needs": ["yaml"],
           "pip": ["pyyaml", "pyjwt[crypto]"],
           "pip_at": [2], "setup_python_at": 1, "raises_stand": True}], 0, None),
        ("дефект в одном задании из двух не прикрывается вторым",
         [{"workflow": "w", "job": "ok", "needs": ["yaml"], "pip": ["pyyaml"],
           "pip_at": [2], "setup_python_at": 1},
          {"workflow": "w", "job": "bad", "needs": ["yaml"], "pip": [],
           "pip_at": [], "setup_python_at": None}], 1, "bad:"),
    ]
    ok = True
    for name, jobs, want, needle in cases:
        f = judge(jobs, {"yaml", "jwt"} if any(j.get("raises_stand") for j in jobs) else None)
        good = len(f) == want and (needle is None or any(needle in x for x in f))
        ok &= good
        print(f"  [{'OK ' if good else 'ОТКАЗ'}] {name}: находок {len(f)} (ждали {want})")

    # Транзитивность локального импорта — та самая, без которой перепись врёт нулём.
    tracked = tracked_files()
    probe = "tests/authz-fixtures/prodseed_ceremony.py"
    if probe in tracked:
        got = third_party(probe, tracked)
        good = "jwt" in got
        print(f"  [{'OK ' if good else 'ОТКАЗ'}] локальный импорт раскрыт транзитивно: "
              f"{probe} → {sorted(got)} (ждали среди них jwt)")
        ok &= good
    else:
        print(f"  [ПРОПУСК] {probe} нет в индексе — транзитивность не проверена. "
              f"Это «не выполнилось», а не «сошлось»")
        ok = False
    print("самопроба:", "ПРОЙДЕНА" if ok else "ПРОВАЛЕНА")
    return 0 if ok else 1


def main() -> int:
    if "--self-test" in sys.argv:
        return self_test()
    jobs = survey()
    tree = tree_third_party(tracked_files())
    stands = [j for j in jobs if j.get("raises_stand")]
    with_python = [j for j in jobs if j["needs"]]
    print(f"осмотрено заданий с python-скриптами: {len(jobs)}; "
          f"из них требуют сторонних модулей: {len(with_python)}; "
          f"поднимают стенд: {len(stands)}; сторонних модулей в дереве: "
          f"{', '.join(sorted(PACKAGE_OF.get(m, m) for m in tree)) or '—'}")
    if not jobs:
        sys.stderr.write(
            "ОТКАЗ: не прочитано НИ ОДНОГО объявления задания с python-скриптами. "
            "Это «не выполнилось», а не «нарушений нет»: обход пуст, судить нечего\n")
        return 2
    findings = judge(jobs, tree)
    if not findings:
        print("условия прогона создаются заданиями: каждое, чьи скрипты требуют сторонний "
              "модуль, ставит его сам и после установки интерпретатора")
        return 0
    sys.stderr.write(f"НАХОДОК {len(findings)}:\n")
    for f in findings:
        sys.stderr.write(f"  · {f}\n")
    return 1


if __name__ == "__main__":
    sys.exit(main())
