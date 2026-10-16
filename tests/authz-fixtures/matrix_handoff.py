# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Передача матрицы посева между шагами — файлом в каталоге СВОЕГО прогона (#3264).

Базовый посев (`prodseed_all.py`, прогонщик `prodrun.sh`) кладёт матрицу в каталог
своего прогона и называет путь расширениям (`prodseed_<svc>_ext.py`) переменной
`PRODSEED_MATRIX_PATH`. Общего файла машины нет: при нём два посева против разных
стендов, идущие параллельно, подменяли матрицу друг другу, и расширение одного
стенда заводило ресурсы токенами и учётками другого.

Путь не назван — отказ, а не откат к общему файлу: молчаливый откат и есть
исходный дефект.
"""
from __future__ import annotations

import json
import os
import pathlib

ENV = "PRODSEED_MATRIX_PATH"


def path() -> pathlib.Path:
    """Путь матрицы своего прогона; не назван — отказ с причиной."""
    value = os.environ.get(ENV, "")
    if not value:
        raise SystemExit(
            f"[prodseed] FATAL: {ENV} не задан — путь матрицы своего прогона называет "
            f"базовый посев (prodseed_all.py, prodrun.sh); общего файла машины нет")
    return pathlib.Path(value)


def read() -> dict:
    """Матрица своего прогона."""
    p = path()
    try:
        return json.loads(p.read_text())
    except FileNotFoundError:
        raise SystemExit(f"[prodseed] FATAL: матрицы нет по пути {ENV}={p} — базовый посев "
                         f"этого прогона её не положил") from None


def write(p: pathlib.Path, fixtures: dict) -> None:
    """Кладёт матрицу в каталог своего прогона."""
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(json.dumps(fixtures))
