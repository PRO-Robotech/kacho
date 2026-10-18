#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Пространство имён стенда для посева — ОДНО место, где его читают (задача #2882).

Исторически имя приходило двумя переменными: прогонщики newman и `setup.sh`
передавали `SETUP_NS`, модули посева на python читали `KACHO_NAMESPACE`, а
`prodrun.sh` держал `kacho` литералом. При одной заданной переменной часть шагов
уходила к чужому пространству, а не к названному стенду.

Правило теперь одно:
  * задана одна из двух — она и есть пространство стенда;
  * заданы обе и совпадают — то же значение;
  * заданы обе и РАСХОДЯТСЯ — отказ до первого обращения к кластеру: какую из
    них вызывающий имел в виду, посев не угадывает;
  * не задана ни одна — `kacho`, умолчание подъёма стенда.

Разрешённое значение вызывающий передаёт дальше ОБЕИМИ переменными, чтобы
следующий шаг пришёл к тому же ответу.

Для оболочки то же правило держит `stand-ns.sh`; равенство двух реализаций на
всех сочетаниях судит `stand_namespace_test.py` (ось P). Как программа
(`python3 stand_ns.py`) файл печатает имя на stdout, на расхождении — отказ, код 2.
"""
from __future__ import annotations

import os
import sys

DEFAULT_NAMESPACE = "kacho"
VARIABLES = ("SETUP_NS", "KACHO_NAMESPACE")


class NamespaceConflict(SystemExit):
    """Обе переменные заданы и расходятся; код выхода — 2."""


def resolve(environ=None) -> str:
    env = os.environ if environ is None else environ
    given = {v: env.get(v, "") for v in VARIABLES if env.get(v, "")}
    values = set(given.values())
    if len(values) > 1:
        detail = ", ".join(f"{k}={v}" for k, v in given.items())
        print(f"[stand-ns] FATAL: пространство имён стенда задано двумя переменными по-разному "
              f"({detail}); посев не угадывает, какую имели в виду. Оставьте одну либо "
              f"задайте обе одинаково.", file=sys.stderr)
        raise NamespaceConflict(2)
    return values.pop() if values else DEFAULT_NAMESPACE


if __name__ == "__main__":
    print(resolve())
