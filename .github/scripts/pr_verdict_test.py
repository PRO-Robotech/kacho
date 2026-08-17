#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Доказательство, что сводный вердикт умеет и краснеть, и зеленеть, и молчать.

Гейт, который нельзя провалить, не является гейтом. Здесь каждому исходу отвечает
проба, а каждому отрицанию — парный положительный контроль: иначе «красное»
зеленело бы на всём сломанном одинаково.
"""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

_spec = importlib.util.spec_from_file_location(
    "pr_verdict", Path(__file__).with_name("pr-verdict.py")
)
assert _spec and _spec.loader
pr_verdict = importlib.util.module_from_spec(_spec)
sys.modules["pr_verdict"] = pr_verdict
_spec.loader.exec_module(pr_verdict)

decide, GREEN, RED, NOT_READY = (
    pr_verdict.decide, pr_verdict.GREEN, pr_verdict.RED, pr_verdict.NOT_READY
)


def run(name: str, status: str, conclusion: str | None = None) -> dict:
    return {"name": name, "status": status, "conclusion": conclusion}


def test_все_зелёные_дают_зелёное() -> None:
    """Положительный контроль. Без него «красное» ниже зеленело бы на чём угодно."""
    v = decide([run("ci", "completed", "success"), run("ui", "completed", "success")])
    assert v.state == GREEN, v
    assert v.exit_code == 0
    assert (v.total, v.green) == (2, 2)


def test_одна_красная_блокирует_и_НАЗЫВАЕТ_виновника() -> None:
    v = decide([run("ci", "completed", "success"), run("ui", "completed", "failure")])
    assert v.state == RED, v
    assert v.offenders == ("ui",), "вердикт обязан называть, что именно красно"


def test_отмена_и_снятие_по_времени_тоже_блокируют() -> None:
    """Отменённый прогон не даёт вердикта — зачесть его в зелёное значит принять
    отсутствие ответа за ответ."""
    for bad in ("cancelled", "timed_out", "action_required", "stale"):
        v = decide([run("ci", "completed", "success"), run("e2e", "completed", bad)])
        assert v.state == RED, (bad, v)


def test_пока_что_то_идёт_вердикта_НЕТ() -> None:
    """Третья категория: не зелено и не красно. Вызывающий обязан подождать."""
    for waiting in ("queued", "in_progress", "waiting", "pending", "requested"):
        v = decide([run("ci", "completed", "success"), run("ui", waiting)])
        assert v.state == NOT_READY, (waiting, v)
        assert v.pending == 1


def test_отказ_решает_НЕ_дожидаясь_остальных() -> None:
    """Держать ранеры ради заведомо красного вердикта — расход без предмета."""
    v = decide([run("ci", "completed", "failure"), run("ui", "in_progress")])
    assert v.state == RED, v


def test_ноль_проверок_это_НЕ_зелено() -> None:
    """Предмет #614 в чистом виде: «никто не смотрел» обязано быть отличимо от
    «замечаний нет»."""
    v = decide([])
    assert v.state == NOT_READY, v
    assert "никто не смотрел" in v.reason


def test_только_нейтральные_это_НЕ_зелено() -> None:
    """Пропущенное и нейтральное не зачитываются в успех: иначе класс «форма без
    содержания» возвращается уровнем ниже."""
    v = decide([run("ci", "completed", "skipped"), run("ui", "completed", "neutral")])
    assert v.state == RED, v
    assert v.neutralish == 2 and v.green == 0


def test_нейтральные_рядом_с_зелёными_не_мешают() -> None:
    """Парный контроль к предыдущей: пропуск сам по себе не отравляет вердикт,
    если хоть одна проверка действительно смотрела."""
    v = decide([run("ci", "completed", "success"), run("docs", "completed", "skipped")])
    assert v.state == GREEN, v
    assert (v.green, v.neutralish) == (1, 1)


def test_себя_из_счёта_исключаем() -> None:
    """Без этого вердикт ждал бы собственного завершения вечно."""
    self_name = "сводный вердикт"
    v = decide([run(self_name, "in_progress"), run("ci", "completed", "success")], self_name)
    assert v.state == GREEN, v
    assert v.total == 1


def test_принимает_обе_формы_ответа() -> None:
    """API отдаёт объект с ключом check_runs; в пробах удобнее список. Разбор не
    должен зависеть от того, чем его накормили."""
    as_list = decide([run("ci", "completed", "success")])
    as_obj = decide({"check_runs": [run("ci", "completed", "success")]})
    assert as_list.state == as_obj.state == GREEN


def test_мусор_на_входе_не_читается_как_зелёное() -> None:
    for junk in ("строка", 42, None):
        try:
            decide(junk)
        except ValueError:
            continue
        raise AssertionError(f"мусор {junk!r} не отвергнут — вердикт был бы о неизвестно чём")
