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

decide, render, GREEN, RED, NOT_READY = (
    pr_verdict.decide, pr_verdict.render,
    pr_verdict.GREEN, pr_verdict.RED, pr_verdict.NOT_READY,
)


def run(name: str, status: str, conclusion: str | None = None,
        run_id: int | None = None, app: str | None = None) -> dict:
    r: dict = {"name": name, "status": status, "conclusion": conclusion}
    if run_id is not None:
        r["id"] = run_id
    if app is not None:
        r["app"] = {"slug": app}
    return r


# Имя из прогона 36198101030 (#2865): отменён `cancel-in-progress` событием
# `edited`, заменён прогоном того же имени с большим id.
TITLE = "заголовок и тело запроса без атрибуции"


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


# ── Повторный прогон того же имени на той же sha (#2865) ─────────────────────
#
# Check-runs одной sha приходят из РАЗНЫХ наборов: событие `edited` запускает
# процесс заново, `cancel-in-progress` снимает прежний прогон. Отменённый
# остаётся на sha навсегда, и перезапуск свода давал бы то же красное.


def test_cancelled_with_a_later_success_of_the_same_name_is_green() -> None:
    """Предикат снятия #2865: отменённый прогон, заменённый более поздним
    успехом того же имени, вердикта не красит."""
    v = decide([
        run("ci", "completed", "success", run_id=10),
        run(TITLE, "completed", "cancelled", run_id=108278452901),
        run(TITLE, "completed", "success", run_id=108278551222),
    ])
    assert v.state == GREEN, v
    assert (v.total, v.green, v.blocking) == (3, 2, 0), v
    assert len(v.superseded) == 1 and TITLE in v.superseded[0], v


def test_cancelled_without_a_replacement_is_red_and_named() -> None:
    """Близнец предыдущей: та же отмена без замены — красно, нарушитель назван.
    Меняется ровно один факт — нет более позднего прогона того же имени."""
    v = decide([
        run("ci", "completed", "success", run_id=10),
        run(TITLE, "completed", "cancelled", run_id=108278452901),
    ])
    assert v.state == RED, v
    assert v.offenders == (TITLE,), v
    assert v.superseded == (), v


def test_the_later_run_is_the_larger_id_not_the_later_position() -> None:
    """«Позднее» — по id, а не по порядку во входе: API порядка не обещает.
    Тот же набор в обратном порядке судится одинаково, а поздняя отмена после
    раннего успеха — красна."""
    replaced = [
        run(TITLE, "completed", "success", run_id=200),
        run(TITLE, "completed", "cancelled", run_id=100),
    ]
    assert decide(replaced).state == GREEN, decide(replaced)
    cancelled_last = [
        run(TITLE, "completed", "success", run_id=100),
        run(TITLE, "completed", "cancelled", run_id=200),
    ]
    v = decide(cancelled_last)
    assert v.state == RED, v
    assert v.offenders == (TITLE,), v


def test_a_failure_replaced_by_a_later_success_is_green() -> None:
    """Исправленный заголовок: прогон на `edited` заменяет прежний отказ."""
    v = decide([
        run(TITLE, "completed", "failure", run_id=100),
        run(TITLE, "completed", "success", run_id=200),
    ])
    assert v.state == GREEN, v


def test_a_cancelled_run_whose_replacement_still_runs_is_not_ready() -> None:
    """Замена ещё идёт — вердикта нет, ждать. Красное здесь обрывало бы ожидание
    на первом заходе, хотя замена ещё скажет своё."""
    v = decide([
        run("ci", "completed", "success", run_id=10),
        run(TITLE, "completed", "cancelled", run_id=100),
        run(TITLE, "in_progress", run_id=200),
    ])
    assert v.state == NOT_READY, v
    assert v.offenders == (TITLE,), v


def test_a_later_skip_does_not_erase_an_earlier_failure() -> None:
    """Пропуск ничего не осмотрел — он не замена. Иначе отказ превращался бы в
    «нейтральное», и соседняя зелёная проверка давала бы «зелено»."""
    v = decide([
        run("ci", "completed", "success", run_id=10),
        run(TITLE, "completed", "failure", run_id=100),
        run(TITLE, "completed", "skipped", run_id=200),
    ])
    assert v.state == RED, v
    assert v.offenders == (TITLE,), v


def test_a_later_skip_after_a_success_keeps_the_success() -> None:
    """Парный контроль к предыдущей: поздний пропуск при состоявшемся успехе
    вердикта не портит — судится прогон, несущий исход."""
    v = decide([
        run(TITLE, "completed", "success", run_id=100),
        run(TITLE, "completed", "skipped", run_id=200),
    ])
    assert v.state == GREEN, v
    assert (v.green, v.neutralish) == (1, 0), v


def test_same_name_from_another_app_is_not_a_replacement() -> None:
    """Имя проверки не принадлежит одному поставщику: одноимённый прогон
    ДРУГОГО приложения — отдельная проверка, а не замена."""
    v = decide([
        run("CodeQL", "completed", "failure", run_id=100, app="github-advanced-security"),
        run("CodeQL", "completed", "success", run_id=200, app="github-actions"),
    ])
    assert v.state == RED, v
    assert v.offenders == ("CodeQL",), v


def test_without_ids_no_replacement_is_assumed() -> None:
    """Порядка нет — замены не доказать: одноимённые прогоны без id судятся все,
    и отмена по-прежнему красит."""
    v = decide([
        run(TITLE, "completed", "cancelled"),
        run(TITLE, "completed", "success"),
    ])
    assert v.state == RED, v
    assert v.superseded == (), v


def test_the_report_names_what_was_set_aside() -> None:
    """Отставленный прогон не исчезает молча: сводка называет его и его id."""
    text = render(decide([
        run(TITLE, "completed", "cancelled", run_id=108278452901),
        run(TITLE, "completed", "success", run_id=108278551222),
    ]))
    assert "отставлено 1" in text, text
    assert "108278452901" in text and "cancelled" in text, text
