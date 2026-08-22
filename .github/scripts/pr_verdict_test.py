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


def test_all_green_yields_green() -> None:
    """Положительный контроль. Без него «красное» ниже зеленело бы на чём угодно."""
    v = decide([run("ci", "completed", "success"), run("ui", "completed", "success")])
    assert v.state == GREEN, v
    assert v.exit_code == 0
    assert (v.total, v.green) == (2, 2)


def test_one_red_blocks_and_names_the_culprit() -> None:
    """Одна красная блокирует — и НАЗЫВАЕТ виновника."""
    v = decide([run("ci", "completed", "success"), run("ui", "completed", "failure")])
    assert v.state == RED, v
    assert v.offenders == ("ui",), "вердикт обязан называть, что именно красно"


def test_cancelled_and_timed_out_also_block() -> None:
    """Отменённый прогон не даёт вердикта — зачесть его в зелёное значит принять
    отсутствие ответа за ответ."""
    for bad in ("cancelled", "timed_out", "action_required", "stale"):
        v = decide([run("ci", "completed", "success"), run("e2e", "completed", bad)])
        assert v.state == RED, (bad, v)


def test_while_anything_runs_there_is_no_verdict() -> None:
    """Третья категория: не зелено и не красно. Вызывающий обязан подождать."""
    for waiting in ("queued", "in_progress", "waiting", "pending", "requested"):
        v = decide([run("ci", "completed", "success"), run("ui", waiting)])
        assert v.state == NOT_READY, (waiting, v)
        assert v.pending == 1


def test_a_failure_decides_without_waiting_for_the_rest() -> None:
    """Держать ранеры ради заведомо красного вердикта — расход без предмета."""
    v = decide([run("ci", "completed", "failure"), run("ui", "in_progress")])
    assert v.state == RED, v


def test_zero_checks_is_not_green() -> None:
    """Предмет #614 в чистом виде: «никто не смотрел» обязано быть отличимо от
    «замечаний нет»."""
    v = decide([])
    assert v.state == NOT_READY, v
    assert "никто не смотрел" in v.reason


def test_neutral_only_is_not_green() -> None:
    """Пропущенное и нейтральное не зачитываются в успех: иначе класс «форма без
    содержания» возвращается уровнем ниже."""
    v = decide([run("ci", "completed", "skipped"), run("ui", "completed", "neutral")])
    assert v.state == RED, v
    assert v.neutralish == 2 and v.green == 0


def test_neutral_alongside_green_does_not_interfere() -> None:
    """Парный контроль к предыдущей: пропуск сам по себе не отравляет вердикт,
    если хоть одна проверка действительно смотрела."""
    v = decide([run("ci", "completed", "success"), run("docs", "completed", "skipped")])
    assert v.state == GREEN, v
    assert (v.green, v.neutralish) == (1, 1)


def test_self_is_excluded_from_the_count() -> None:
    """Без этого вердикт ждал бы собственного завершения вечно."""
    self_name = "сводный вердикт"
    v = decide([run(self_name, "in_progress"), run("ci", "completed", "success")], self_name)
    assert v.state == GREEN, v
    assert v.total == 1


def test_both_response_shapes_are_accepted() -> None:
    """API отдаёт объект с ключом check_runs; в пробах удобнее список. Разбор не
    должен зависеть от того, чем его накормили."""
    as_list = decide([run("ci", "completed", "success")])
    as_obj = decide({"check_runs": [run("ci", "completed", "success")]})
    assert as_list.state == as_obj.state == GREEN


def test_garbage_input_is_not_read_as_green() -> None:
    """Мусор на входе не читается как зелёное."""
    for junk in ("строка", 42, None):
        try:
            decide(junk)
        except ValueError:
            continue
        raise AssertionError(f"мусор {junk!r} не отвергнут — вердикт был бы о неизвестно чём")
