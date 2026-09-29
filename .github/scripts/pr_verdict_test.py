#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Доказательство, что сводный вердикт умеет и краснеть, и зеленеть, и молчать.

Гейт, который нельзя провалить, не является гейтом. Здесь каждому исходу отвечает
проба, а каждому отрицанию — парный положительный контроль: иначе «красное»
зеленело бы на всём сломанном одинаково.
"""

from __future__ import annotations

import copy
import functools
import importlib.util
import json
import os
import subprocess
import sys
from pathlib import Path

import yaml

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
        run_id: object = None, app: str | None = "github-actions", suite: object = None) -> dict:
    """Прогон в форме ответа check-runs. Приложение по умолчанию — то, от
    которого приходят джобы процессов: только его прогон доказывает, что
    объявленная джоба появилась."""
    r: dict = {"name": name, "status": status, "conclusion": conclusion}
    if run_id is not None:
        r["id"] = run_id
    if app is not None:
        r["app"] = {"slug": app}
    if suite is not None:
        r["check_suite"] = {"id": suite}
    return r


def declared(*names: str):
    """Объявление из литеральных имён — в той форме, в какой его вернул бы разбор
    дерева процессов."""
    return pr_verdict.Declared(
        checks=tuple(pr_verdict.JobName("проба", n, None) for n in names),
        workflows=1, on_request=1, breaches=())


def decide_all_declared(payload: object, self_name: str = ""):
    """Вердикт, у которого объявлено ровно то, что пришло прогонами джоб.

    Предмет проб двух первых разделов — исходы прогонов и выбор судимого, а не
    присутствие: объявленное здесь совпадает с пришедшим, и ось присутствия
    молчит. Её судят пробы раздела «Объявленное», где объявление выводится из
    дерева процессов."""
    runs = payload.get("check_runs", []) if isinstance(payload, dict) else payload
    names = sorted({
        str(r.get("name")) for r in runs
        if isinstance(r, dict) and r.get("name") != self_name
        and isinstance(r.get("app"), dict) and r["app"].get("slug") == pr_verdict.ACTIONS_APP
    }) if isinstance(runs, list) else []
    return decide(payload, declared(*names), self_name)


# Имя из прогона 36198101030 (#2865): отменён `cancel-in-progress` событием
# `edited`, заменён прогоном того же имени с большим id.
TITLE = "заголовок и тело запроса без атрибуции"


def test_all_green_yields_green() -> None:
    """Положительный контроль. Без него «красное» ниже зеленело бы на чём угодно."""
    v = decide_all_declared([run("ci", "completed", "success"), run("ui", "completed", "success")])
    assert v.state == GREEN, v
    assert v.exit_code == 0
    assert (v.total, v.green) == (2, 2)


def test_one_red_blocks_and_names_the_culprit() -> None:
    """Одна красная блокирует — и НАЗЫВАЕТ виновника."""
    v = decide_all_declared([run("ci", "completed", "success"), run("ui", "completed", "failure")])
    assert v.state == RED, v
    assert v.offenders == ("ui",), "вердикт обязан называть, что именно красно"


def test_cancelled_and_timed_out_also_block() -> None:
    """Отменённый прогон не даёт вердикта — зачесть его в зелёное значит принять
    отсутствие ответа за ответ."""
    for bad in ("cancelled", "timed_out", "action_required", "stale"):
        v = decide_all_declared([run("ci", "completed", "success"), run("e2e", "completed", bad)])
        assert v.state == RED, (bad, v)


def test_while_anything_runs_there_is_no_verdict() -> None:
    """Третья категория: не зелено и не красно. Вызывающий обязан подождать."""
    for waiting in ("queued", "in_progress", "waiting", "pending", "requested"):
        v = decide_all_declared([run("ci", "completed", "success"), run("ui", waiting)])
        assert v.state == NOT_READY, (waiting, v)
        assert v.pending == 1


def test_a_failure_decides_without_waiting_for_the_rest() -> None:
    """Держать ранеры ради заведомо красного вердикта — расход без предмета."""
    v = decide_all_declared([run("ci", "completed", "failure"), run("ui", "in_progress")])
    assert v.state == RED, v


def test_zero_checks_is_not_green() -> None:
    """Предмет #614 в чистом виде: «никто не смотрел» обязано быть отличимо от
    «замечаний нет». Объявленная проверка есть, прогонов нет ни одного."""
    v = decide([], declared("ci"))
    assert v.state == NOT_READY, v
    assert "никто не смотрел" in v.reason
    assert v.missing == ("ci",), v


def test_an_empty_declaration_is_red_not_green() -> None:
    """Отказ закрыт по умолчанию: объявление без единой проверки — это «не
    прочитано», а не «проверять нечего». Зелёные прогоны его не спасают."""
    v = decide([run("ci", "completed", "success")], declared())
    assert v.state == RED, v
    assert "объявлен" in v.reason, v


def test_a_declaration_with_breaches_is_red_not_green() -> None:
    """Объявление, которое разбор дерева не вычислил, вердикта не даёт: тот же
    зелёный набор с той же проверкой — красно. Близнец — объявление без
    нарушений, ровно тот же набор зелен."""
    runs = [run("ci", "completed", "success")]
    broken = pr_verdict.Declared(checks=declared("ci").checks, workflows=1, on_request=1,
                                 breaches=("ci.yaml: фильтр по путям не вычислен",))
    v = decide(runs, broken)
    assert v.state == RED, v
    assert "ci.yaml" in v.reason, v
    assert decide(runs, declared("ci")).state == GREEN


def test_neutral_only_is_not_green() -> None:
    """Пропущенное и нейтральное не зачитываются в успех: иначе класс «форма без
    содержания» возвращается уровнем ниже."""
    v = decide_all_declared([run("ci", "completed", "skipped"), run("ui", "completed", "neutral")])
    assert v.state == RED, v
    assert v.neutralish == 2 and v.green == 0


def test_neutral_alongside_green_does_not_interfere() -> None:
    """Парный контроль к предыдущей: пропуск сам по себе не отравляет вердикт,
    если хоть одна проверка действительно смотрела."""
    v = decide_all_declared([run("ci", "completed", "success"), run("docs", "completed", "skipped")])
    assert v.state == GREEN, v
    assert (v.green, v.neutralish) == (1, 1)


def test_self_is_excluded_from_the_count() -> None:
    """Без этого вердикт ждал бы собственного завершения вечно."""
    self_name = "сводный вердикт"
    v = decide_all_declared([run(self_name, "in_progress"), run("ci", "completed", "success")], self_name)
    assert v.state == GREEN, v
    assert v.total == 1


def test_both_response_shapes_are_accepted() -> None:
    """API отдаёт объект с ключом check_runs; в пробах удобнее список. Разбор не
    должен зависеть от того, чем его накормили."""
    as_list = decide_all_declared([run("ci", "completed", "success")])
    as_obj = decide_all_declared({"check_runs": [run("ci", "completed", "success")]})
    assert as_list.state == as_obj.state == GREEN


def test_garbage_input_is_not_read_as_green() -> None:
    """Мусор на входе не читается как зелёное."""
    for junk in ("строка", 42, None):
        try:
            decide_all_declared(junk)
        except ValueError:
            continue
        raise AssertionError(f"мусор {junk!r} не отвергнут — вердикт был бы о неизвестно чём")


# ── Повторный прогон той же проверки на той же sha (#2865) ──────────────────
#
# Check-runs одной sha приходят из РАЗНЫХ наборов: событие `edited` запускает
# процесс заново, `cancel-in-progress` снимает прежний прогон. Отменённый
# остаётся на sha навсегда, и перезапуск свода давал бы то же красное.
#
# Номера наборов и прогонов ниже — с живой sha 9672c86fac5 (PR #2861), где
# отменённый прогон и его замена лежат в разных наборах.
OLD_SUITE, NEW_SUITE = 98030781046, 98030864187


def test_cancelled_with_a_later_success_of_the_same_name_is_green() -> None:
    """Предикат снятия #2865: отменённый прогон, заменённый успехом того же
    имени в более позднем наборе, вердикта не красит."""
    v = decide_all_declared([
        run("ci", "completed", "success", run_id=10, suite=1),
        run(TITLE, "completed", "cancelled", run_id=108278452901, suite=OLD_SUITE),
        run(TITLE, "completed", "success", run_id=108278551222, suite=NEW_SUITE),
    ])
    assert v.state == GREEN, v
    assert (v.total, v.green, v.blocking) == (3, 2, 0), v
    assert len(v.superseded) == 1 and TITLE in v.superseded[0], v


def test_cancelled_without_a_replacement_is_red_and_named() -> None:
    """Близнец предыдущей: та же отмена без замены — красно, нарушитель назван.
    Меняется ровно один факт — нет более позднего прогона того же имени."""
    v = decide_all_declared([
        run("ci", "completed", "success", run_id=10, suite=1),
        run(TITLE, "completed", "cancelled", run_id=108278452901, suite=OLD_SUITE),
    ])
    assert v.state == RED, v
    assert v.offenders == (TITLE,), v
    assert v.superseded == (), v


def test_the_later_run_is_ordered_by_suite_then_id_not_by_position() -> None:
    """«Позднее» — по паре (набор, id), а не по порядку во входе: API порядка
    не обещает. Тот же набор в обратном порядке судится одинаково, а поздняя
    отмена после раннего успеха — красна."""
    replaced = [
        run(TITLE, "completed", "success", run_id=200, suite=NEW_SUITE),
        run(TITLE, "completed", "cancelled", run_id=100, suite=OLD_SUITE),
    ]
    assert decide_all_declared(replaced).state == GREEN, decide_all_declared(replaced)
    cancelled_last = [
        run(TITLE, "completed", "success", run_id=100, suite=OLD_SUITE),
        run(TITLE, "completed", "cancelled", run_id=200, suite=NEW_SUITE),
    ]
    v = decide_all_declared(cancelled_last)
    assert v.state == RED, v
    assert v.offenders == (TITLE,), v


def test_a_rerun_of_an_older_suite_does_not_supersede_a_fresher_failure() -> None:
    """Повтор попытки СТАРОГО набора получает больший id, но судит старый
    контекст: `review-text` читает текст из полезной нагрузки события, и повтор
    набора первого события судит ПРЕЖНИЙ текст. Свежий отказ он не отставляет.

    Замер: ui.yml, прогон 35183091753 — у попытки 2 тот же набор 95281882850,
    что у попытки 1, а id прогонов больше."""
    v = decide_all_declared([
        run(TITLE, "completed", "success", run_id=100, suite=OLD_SUITE),
        run(TITLE, "completed", "failure", run_id=200, suite=NEW_SUITE),
        run(TITLE, "completed", "success", run_id=300, suite=OLD_SUITE),
    ])
    assert v.state == RED, v
    assert v.offenders == (TITLE,), v
    assert len(v.superseded) == 2, v


def test_a_rerun_within_the_freshest_suite_supersedes_its_failure() -> None:
    """Законный близнец предыдущей: меняется ровно один факт — повтор идёт в
    СВЕЖЕМ наборе. Повтор того же контекста заменяет его отказ."""
    v = decide_all_declared([
        run(TITLE, "completed", "success", run_id=100, suite=OLD_SUITE),
        run(TITLE, "completed", "failure", run_id=200, suite=NEW_SUITE),
        run(TITLE, "completed", "success", run_id=300, suite=NEW_SUITE),
    ])
    assert v.state == GREEN, v
    assert len(v.superseded) == 2, v


def test_a_failure_replaced_by_a_later_success_is_green() -> None:
    """Исправленный заголовок: прогон на `edited` заменяет прежний отказ."""
    v = decide_all_declared([
        run(TITLE, "completed", "failure", run_id=100, suite=OLD_SUITE),
        run(TITLE, "completed", "success", run_id=200, suite=NEW_SUITE),
    ])
    assert v.state == GREEN, v


def test_a_cancelled_run_whose_replacement_still_runs_is_not_ready() -> None:
    """Замена ещё идёт — вердикта нет, ждать. Красное здесь обрывало бы ожидание
    на первом заходе, хотя замена ещё скажет своё."""
    v = decide_all_declared([
        run("ci", "completed", "success", run_id=10, suite=1),
        run(TITLE, "completed", "cancelled", run_id=100, suite=OLD_SUITE),
        run(TITLE, "in_progress", run_id=200, suite=NEW_SUITE),
    ])
    assert v.state == NOT_READY, v
    assert v.offenders == (TITLE,), v


def test_a_later_skip_does_not_erase_an_earlier_failure() -> None:
    """Пропуск ничего не осмотрел — он не замена. Иначе отказ превращался бы в
    «нейтральное», и соседняя зелёная проверка давала бы «зелено»."""
    v = decide_all_declared([
        run("ci", "completed", "success", run_id=10, suite=1),
        run(TITLE, "completed", "failure", run_id=100, suite=OLD_SUITE),
        run(TITLE, "completed", "skipped", run_id=200, suite=NEW_SUITE),
    ])
    assert v.state == RED, v
    assert v.offenders == (TITLE,), v


def test_a_later_skip_after_a_success_keeps_the_success() -> None:
    """Парный контроль к предыдущей: поздний пропуск при состоявшемся успехе
    вердикта не портит — судится прогон, несущий исход."""
    v = decide_all_declared([
        run(TITLE, "completed", "success", run_id=100, suite=OLD_SUITE),
        run(TITLE, "completed", "skipped", run_id=200, suite=NEW_SUITE),
    ])
    assert v.state == GREEN, v
    assert (v.green, v.neutralish) == (1, 0), v


def test_same_name_from_another_app_is_not_a_replacement() -> None:
    """Имя проверки не принадлежит одному поставщику: одноимённый прогон
    ДРУГОГО приложения — отдельная проверка, а не замена."""
    v = decide_all_declared([
        run("CodeQL", "completed", "failure", run_id=100, app="github-advanced-security",
            suite=OLD_SUITE),
        run("CodeQL", "completed", "success", run_id=200, app="github-actions",
            suite=NEW_SUITE),
    ])
    assert v.state == RED, v
    assert v.offenders == ("CodeQL",), v


def test_without_ids_no_replacement_is_assumed() -> None:
    """Порядка нет — замены не доказать: одноимённые прогоны без id судятся все,
    и отмена по-прежнему красит."""
    v = decide_all_declared([
        run(TITLE, "completed", "cancelled", suite=OLD_SUITE),
        run(TITLE, "completed", "success", suite=NEW_SUITE),
    ])
    assert v.state == RED, v
    assert v.superseded == (), v


def test_without_a_suite_no_replacement_is_assumed() -> None:
    """Номер набора — половина порядка. Без него повтор старого контекста не
    отличить от замены, поэтому прогоны без набора судятся все — как без id.
    Близнец — первая проба раздела: те же прогоны С наборами зелены."""
    v = decide_all_declared([
        run(TITLE, "completed", "cancelled", run_id=108278452901),
        run(TITLE, "completed", "success", run_id=108278551222),
    ])
    assert v.state == RED, v
    assert v.superseded == (), v


def test_a_boolean_is_not_an_order() -> None:
    """`True` в JSON — не число, хотя в python `bool` — подкласс `int`. Прогон с
    таким id или номером набора упорядочить нельзя, и замена не предполагается.
    Близнец в каждой паре меняет ровно один факт — `True` на целое 1."""
    for bad, good in (
        (dict(run_id=True, suite=OLD_SUITE), dict(run_id=1, suite=OLD_SUITE)),
        (dict(run_id=100, suite=True), dict(run_id=100, suite=1)),
    ):
        with_bool = decide_all_declared([
            run(TITLE, "completed", "cancelled", **bad),
            run(TITLE, "completed", "success", run_id=200, suite=NEW_SUITE),
        ])
        assert with_bool.state == RED, (bad, with_bool)
        assert with_bool.superseded == (), (bad, with_bool)
        with_int = decide_all_declared([
            run(TITLE, "completed", "cancelled", **good),
            run(TITLE, "completed", "success", run_id=200, suite=NEW_SUITE),
        ])
        assert with_int.state == GREEN, (good, with_int)


def test_the_report_names_what_was_set_aside() -> None:
    """Отставленный прогон не исчезает молча: сводка называет его, его id и
    его набор."""
    text = render(decide_all_declared([
        run(TITLE, "completed", "cancelled", run_id=108278452901, suite=OLD_SUITE),
        run(TITLE, "completed", "success", run_id=108278551222, suite=NEW_SUITE),
    ]))
    assert "отставлено 1" in text, text
    assert "108278452901" in text and "cancelled" in text, text
    assert str(OLD_SUITE) in text, text


# ── Предпосылка ключа: имя проверки однозначно в процессах запроса ────────────
#
# В ответе check-runs нет ни процесса, ни события, из которых прогон пришёл.
# Ключ (приложение, имя) поэтому держится предпосылкой: одно имя производит
# ровно одна джоба процессов, запускаемых запросом на слияние. Иначе отказ одной
# джобы отставлялся бы успехом другой — одноимённой. Пробы ниже инъецируют
# нарушение в НАСТОЯЩЕЕ дерево процессов и держат рядом законного близнеца.

def premise(workflows: dict[str, object]):
    return pr_verdict.premise(workflows)


WORKFLOWS = Path(__file__).resolve().parents[1] / "workflows"
REVIEW, UI, CI = (f".github/workflows/{n}" for n in ("review-text.yml", "ui.yml", "ci.yaml"))
FUZZ = ".github/workflows/continuous-fuzz.yml"


@functools.cache
def _parsed_tree() -> dict[str, object]:
    files = sorted({*WORKFLOWS.glob("*.yml"), *WORKFLOWS.glob("*.yaml")})
    return {f".github/workflows/{f.name}": yaml.safe_load(f.read_text(encoding="utf-8"))
            for f in files}


def tree_workflows() -> dict[str, object]:
    """Разобранное дерево процессов; каждой пробе — своя копия для инъекции."""
    return copy.deepcopy(_parsed_tree())


def _pr_jobs_declared(workflows: dict[str, object]) -> int:
    """Знаменатель обхода, посчитанный независимо от гейта."""
    n = 0
    for doc in workflows.values():
        on = doc.get("on", doc.get(True))
        triggers = [on] if isinstance(on, str) else list(on or [])
        if {"pull_request", "pull_request_target"} & set(triggers):
            n += len(doc["jobs"])
    return n


def test_the_tree_holds_the_check_name_premise() -> None:
    """Предикат предпосылки: в процессах запроса имя каждой джобы однозначно, и
    осмотрены ВСЕ их джобы, а не часть."""
    wf = tree_workflows()
    p = premise(wf)
    print(f"предпосылка ключа: осмотрено джоб {p.examined}, имён {p.names}, "
          f"нарушений {len(p.breaches)}")
    assert p.breaches == (), "\n".join(p.breaches)
    assert p.examined == _pr_jobs_declared(wf) > 0, p
    assert p.names >= p.examined, p


def test_a_same_named_job_in_another_pr_workflow_breaks_the_premise() -> None:
    """Инъекция: джоба `review-text.yml` скопирована в `ui.yml`. Её отказ в одном
    процессе отставлялся бы успехом в другом. Близнец — та же копия в процессе,
    который запрос не запускает: его прогонов на голове запроса нет."""
    job = tree_workflows()[REVIEW]["jobs"]["attribution"]
    wf = tree_workflows()
    wf[UI]["jobs"]["attribution-copy"] = copy.deepcopy(job)
    breaches = premise(wf).breaches
    assert any(TITLE in b and "ui.yml" in b and "review-text.yml" in b for b in breaches), breaches
    twin = tree_workflows()
    twin[FUZZ]["jobs"]["attribution-copy"] = copy.deepcopy(job)
    assert premise(twin).breaches == (), premise(twin).breaches


def test_a_same_named_job_in_the_same_workflow_breaks_the_premise() -> None:
    """Инъекция внутри одного процесса: вторая джоба под именем `golangci-lint`.
    Близнец — то же тело под другим именем."""
    wf = tree_workflows()
    wf[CI]["jobs"]["lint-twin"] = copy.deepcopy(wf[CI]["jobs"]["lint"])
    breaches = premise(wf).breaches
    assert any("golangci-lint" in b and "lint-twin" in b for b in breaches), breaches
    twin = tree_workflows()
    twin[CI]["jobs"]["lint-twin"] = copy.deepcopy(twin[CI]["jobs"]["lint"])
    twin[CI]["jobs"]["lint-twin"]["name"] = "golangci-lint второй проход"
    assert premise(twin).breaches == (), premise(twin).breaches


def test_a_matrix_name_without_an_expression_breaks_the_premise() -> None:
    """Имя матричной джобы без выражения даёт ногам одно имя — отказ одной ноги
    отставлялся бы успехом другой. Близнец — настоящее имя из дерева."""
    wf = tree_workflows()
    wf[UI]["jobs"]["build"]["name"] = "build"
    breaches = premise(wf).breaches
    assert any("ui.yml" in b and "jobs.build" in b and "project" in b for b in breaches), breaches
    wf = tree_workflows()
    wf[CI]["jobs"]["unit-shard"]["name"] = "юниты"
    breaches = premise(wf).breaches
    assert any("jobs.unit-shard" in b for b in breaches), breaches
    assert premise(tree_workflows()).breaches == ()


def test_a_matrix_dimension_missing_from_the_name_breaks_the_premise() -> None:
    """Измерение матрицы, которого имя не называет, склеивает ноги по нему.
    Близнец меняет один факт — имя называет и это измерение."""
    wf = tree_workflows()
    wf[UI]["jobs"]["test"]["strategy"]["matrix"]["node"] = [20, 22]
    breaches = premise(wf).breaches
    assert any("jobs.test" in b and "node" in b for b in breaches), breaches
    twin = tree_workflows()
    twin[UI]["jobs"]["test"]["strategy"]["matrix"]["node"] = [20, 22]
    twin[UI]["jobs"]["test"]["name"] = "unit (${{ matrix.pkg }}, ${{ matrix.node }})"
    assert premise(twin).breaches == (), premise(twin).breaches


def test_a_reference_to_an_undeclared_matrix_key_is_judged_not_crashed() -> None:
    """Ссылка на ключ, которого у матрицы нет, раскрывается провайдером в пустую
    строку. Обход судит такое имя как образец, а не падает на нём: иначе
    предикат дерева краснел бы трассой, а не нарушением. Образец `unit (…, …)`
    ни с одним именем дерева не совпадает, поэтому исход — ноль нарушений."""
    wf = tree_workflows()
    wf[UI]["jobs"]["test"]["name"] = "unit (${{ matrix.pkg }}, ${{ matrix.typo }})"
    assert premise(wf).breaches == (), premise(wf).breaches


def test_a_literal_name_equal_to_a_matrix_leg_breaks_the_premise() -> None:
    """Литеральное имя, совпавшее с ногой раскрытой матрицы другого процесса:
    `unit (host)` — нога `ui.yml` `unit (${{ matrix.pkg }})`. Близнец — имя,
    которого ни одна нога не даёт."""
    wf = tree_workflows()
    wf[CI]["jobs"]["extra"] = {"name": "unit (host)", "runs-on": "ubuntu-latest", "steps": []}
    breaches = premise(wf).breaches
    assert any("unit (host)" in b and "ui.yml" in b and "jobs.extra" in b for b in breaches), breaches
    twin = tree_workflows()
    twin[CI]["jobs"]["extra"] = {"name": "unit host", "runs-on": "ubuntu-latest", "steps": []}
    assert premise(twin).breaches == (), premise(twin).breaches


def test_a_literal_name_within_a_dynamic_matrix_breaks_the_premise() -> None:
    """Матрица из выражения не раскрывается: её имя — образец. Литерал, который
    ему отвечает, — возможное совпадение. Литерал стоит и после образца
    (`ui.yml` против `ci.yaml`), и перед ним (`ci.yaml` против `e2e-newman.yml`):
    порядок обхода не решает. Близнец — литерал, который образцу не отвечает."""
    cases = (
        (UI, "юниты 07", "юнит 07", "unit-shard"),
        (CI, "e2e 01 (vpc)", "e2e 01 [vpc]", "e2e-newman.yml: jobs.shard"),
    )
    for where, hit, miss, owner in cases:
        wf = tree_workflows()
        wf[where]["jobs"]["extra"] = {"name": hit, "runs-on": "ubuntu-latest", "steps": []}
        breaches = premise(wf).breaches
        assert any(hit in b and owner in b for b in breaches), (hit, breaches)
        twin = tree_workflows()
        twin[where]["jobs"]["extra"] = {"name": miss, "runs-on": "ubuntu-latest", "steps": []}
        assert premise(twin).breaches == (), (miss, premise(twin).breaches)


def test_a_matrix_include_key_missing_from_the_name_breaks_the_premise() -> None:
    """Ключ из `include` — тоже измерение: две ноги, различимые только им,
    получили бы одно имя `unit (host)`. Близнец называет и этот ключ."""
    legs = {"include": [{"pkg": "host", "node": 20}, {"pkg": "host", "node": 22}]}
    wf = tree_workflows()
    wf[UI]["jobs"]["test"]["strategy"]["matrix"] = copy.deepcopy(legs)
    breaches = premise(wf).breaches
    assert any("jobs.test" in b and "node" in b for b in breaches), breaches
    twin = tree_workflows()
    twin[UI]["jobs"]["test"]["strategy"]["matrix"] = copy.deepcopy(legs)
    twin[UI]["jobs"]["test"]["name"] = "unit (${{ matrix.pkg }}, ${{ matrix.node }})"
    assert premise(twin).breaches == (), premise(twin).breaches


def test_two_dynamic_matrix_names_with_compatible_ends_break_the_premise() -> None:
    """Два образца: совпасть могут, только если совместимы и начало, и конец.
    `юниты ${{ matrix.shard.id }}` во втором процессе совпадёт с джобой
    `unit-shard`. Близнецы меняют по одному концу: другое начало, другой конец."""
    def dynamic(name: str) -> dict:
        return {"name": name, "runs-on": "ubuntu-latest", "steps": [],
                "strategy": {"matrix": "${{ fromJSON(needs.plan.outputs.matrix) }}"}}
    wf = tree_workflows()
    wf[UI]["jobs"]["extra"] = dynamic("юниты ${{ matrix.shard.id }}")
    breaches = premise(wf).breaches
    assert any("jobs.extra" in b and "unit-shard" in b for b in breaches), breaches
    for other in ("юнит-шард ${{ matrix.shard.id }}",
                  "e2e ${{ matrix.shard.id }} [${{ matrix.shard.suites }}]"):
        twin = tree_workflows()
        twin[UI]["jobs"]["extra"] = dynamic(other)
        assert premise(twin).breaches == (), (other, premise(twin).breaches)


def test_a_reusable_workflow_call_is_not_read_as_unique() -> None:
    """Вызов переиспользуемого процесса даёт имена `вызывающий / вызванный`,
    которых этот обход не вычисляет. Невычисленное не считается однозначным."""
    wf = tree_workflows()
    wf[UI]["jobs"]["called"] = {"uses": "./.github/workflows/ui.yml"}
    breaches = premise(wf).breaches
    assert any("jobs.called" in b for b in breaches), breaches


def test_an_empty_traversal_is_a_breach_not_a_pass() -> None:
    """Ноль осмотренных джоб — не «нарушений нет», а «никто не смотрел». Близнец —
    одно дерево без процессов запроса, другое пустое: оба красны."""
    assert premise({}).breaches, premise({})
    only_schedule = {FUZZ: tree_workflows()[FUZZ]}
    assert premise(only_schedule).breaches, premise(only_schedule)


# ── Объявленное: джобы, которые запрос обязан завести на голове (#2910) ───────
#
# Прежний `decide` судил только check-runs, уже пришедшие на sha. На PR #2908
# (голова dfbbdbc474c, запрос `2907` в `2797`) к 14:34:52 UTC пришли два прогона
# — сам свод и `review-text`, — и свод вынес «зелено», осмотрев одну проверку:
# задания остальных процессов появились после 14:35:30, а в 14:45 на той же sha
# check-runs было 65, из них 9 шли и 1 упал. Объявленное выводится из дерева
# процессов тем же разбором имён, что стережёт предпосылку ключа выше.

LINE_BASE = "2797"   # база запроса PR #2908
VERDICT = "сводный вердикт (все проверки завершились и зелены)"
# Имя check-run на dfbbdbc474c (2026-09-29): джоба `pg-outside-selection`, 109 байт
# в объявлении, площадка показывает 100.
PG_CUT = "Postgres-пробы вне отбора интеграционной джобы (пропуск =..."

# check-runs dfbbdbc474c на 2026-09-29T14:34:52Z: ответ
# `repos/PRO-Robotech/kacho/commits/dfbbdbc474c1e9044937b645483ab08a42da1c7d/check-runs`
# с отбором `started_at` не позже этого мига; незавершённый к нему — `in_progress`.
OBSERVED_2908 = (
    run(VERDICT, "in_progress", run_id=109458271889, suite=99067826638),
    run(TITLE, "completed", "success", run_id=109458155063, suite=99067826660),
)


def tree_declared(base: str = LINE_BASE, workflows: dict[str, object] | None = None,
                  self_name: str = VERDICT):
    return pr_verdict.declare(tree_workflows() if workflows is None else workflows,
                              base, self_name)


def stand_in(check) -> str:
    """Имя прогона, которым площадка отвечает на объявленную проверку: литерал —
    как есть, у образца выражения заменены словом."""
    return check.text if check.parts is None else "x".join(check.parts)


def all_green(decl) -> list[dict]:
    """По одному успешному прогону на каждую объявленную проверку."""
    return [run(stand_in(c), "completed", "success", run_id=i + 1, suite=1)
            for i, c in enumerate(decl.checks)]


def _request_filter(doc: dict) -> dict:
    on = doc["on"] if "on" in doc else doc[True]
    return on["pull_request"]


def _request_workflows_independently(workflows: dict[str, object]) -> int:
    """Процессов на запросе, посчитанных мимо `declare`: `on` называет
    `pull_request`. Фильтр ветки в дереве у всех один — {main, [0-9]+}, его
    держит `assert-review-trigger-scope.py`, ось 1."""
    n = 0
    for doc in workflows.values():
        on = doc.get("on", doc.get(True))
        triggers = [on] if isinstance(on, str) else list(on or [])
        n += "pull_request" in triggers
    return n


def _checks_declared_independently(workflows: dict[str, object]) -> int:
    """Знаменатель объявленного, посчитанный мимо `declare`: нога литеральной
    матрицы — отдельная проверка, матрица из выражения — одна, свод исключён."""
    n = 0
    for doc in workflows.values():
        on = doc.get("on", doc.get(True))
        triggers = [on] if isinstance(on, str) else list(on or [])
        if "pull_request" not in triggers:
            continue
        for job in doc["jobs"].values():
            if job.get("name") == VERDICT:
                continue
            matrix = (job.get("strategy") or {}).get("matrix")
            legs = 1
            if isinstance(matrix, dict):
                for values in matrix.values():
                    legs *= len(values)
            n += legs
    return n


def test_the_tree_declares_every_job_the_request_starts() -> None:
    """Предикат объявления: каждая джоба процессов запроса объявлена, нога
    литеральной матрицы — отдельной проверкой, длинное имя — в показе площадки,
    свод себя не объявляет. Перепись печатается."""
    wf = tree_workflows()
    d = tree_declared(workflows=wf)
    literals = [c.text for c in d.checks if c.parts is None]
    patterns = [c.text for c in d.checks if c.parts is not None]
    print(f"объявление: процессов {d.workflows}, идут на запросе в «{LINE_BASE}» "
          f"{d.on_request}, проверок {len(d.checks)} (литералов {len(literals)}, "
          f"образцов {len(patterns)})")
    assert d.breaches == (), "\n".join(d.breaches)
    assert len(d.checks) == _checks_declared_independently(wf) > 0, d
    assert d.on_request == _request_workflows_independently(wf) > 1, d
    assert VERDICT not in literals, "свод объявил сам себя — ждал бы собственного завершения"
    assert PG_CUT in literals, "длинное имя объявлено не в показе площадки — его прогон " \
                               "не нашёлся бы никогда"
    assert "integration (vpc)" in literals and "unit (host)" in literals, literals
    assert "юниты ${{ matrix.shard.id }}" in patterns, patterns


def test_the_observed_request_with_one_check_of_many_is_not_green() -> None:
    """Инъекция наблюдённым входом: набор PR #2908 на миг вердикта. Прежний
    `decide` вынес на нём «зелено, осмотрено проверок 1»; объявленных джоб не
    появилось почти ни одной, и это «не готово» с поимённым списком."""
    d = tree_declared()
    v = decide(copy.deepcopy(list(OBSERVED_2908)), d, VERDICT)
    assert v.state == NOT_READY, render(v)
    assert (v.total, v.present) == (1, 1), v
    assert TITLE not in v.missing and "golangci-lint" in v.missing, v.missing
    assert len(v.missing) == len(d.checks) - 1, v
    text = render(v)
    assert f"объявлено проверок {len(d.checks)}" in text, text
    assert "осмотрено проверок 1" in text and "golangci-lint" in text, text


def test_every_declared_check_present_and_green_is_green() -> None:
    """Законный близнец: каждая объявленная проверка пришла и зелена — «зелено»,
    и сводка называет, сколько объявлено и сколько осмотрено, и каждую по имени."""
    d = tree_declared()
    v = decide(all_green(d), d, VERDICT)
    n = len(d.checks)
    assert v.state == GREEN, render(v)
    assert (v.declared, v.present, v.missing, v.total) == (n, n, (), n), v
    text = render(v)
    assert f"объявлено проверок {n}" in text and f"появилось {n}" in text, text
    assert f"осмотрено проверок {n}" in text and f"судимые ({n})" in text, text
    assert PG_CUT in text and "golangci-lint — success" in text, text


def test_one_declared_check_still_running_is_not_ready() -> None:
    """Инъекция: одна объявленная проверка идёт — не зелено, она названа."""
    d = tree_declared()
    runs = all_green(d)
    runs[0]["status"], runs[0]["conclusion"] = "in_progress", None
    v = decide(runs, d, VERDICT)
    assert v.state == NOT_READY, render(v)
    assert v.offenders == (runs[0]["name"],) and v.missing == (), v


def test_one_declared_check_failed_is_red() -> None:
    """Инъекция: одна объявленная проверка упала — «красно», нарушитель назван."""
    d = tree_declared()
    runs = all_green(d)
    victim = next(r for r in runs if r["name"] == "golangci-lint")
    victim["conclusion"] = "failure"
    v = decide(runs, d, VERDICT)
    assert v.state == RED, render(v)
    assert v.offenders == ("golangci-lint",), v


def test_one_declared_check_missing_is_not_ready() -> None:
    """Инъекция: одной объявленной проверки нет, остальные зелены. Прежний
    `decide` выносил на этом входе «зелено»: он судил только пришедшее. Меняется
    один факт против близнеца — нет прогона `golangci-lint`."""
    d = tree_declared()
    runs = [r for r in all_green(d) if r["name"] != "golangci-lint"]
    v = decide(runs, d, VERDICT)
    assert v.state == NOT_READY, render(v)
    assert v.missing == ("golangci-lint",) and v.offenders == (), v
    assert "не появились" in render(v) and "golangci-lint" in render(v)


def test_a_dynamic_matrix_without_a_single_leg_is_missing() -> None:
    """Матрица из выражения объявлена образцом: её доказывает хотя бы одна нога.
    Ни одной — образец не появился. Близнец — одна нога под своим именем."""
    d = tree_declared()
    pattern = "юниты ${{ matrix.shard.id }}"
    runs = [r for r in all_green(d) if r["name"] != "юниты x"]
    v = decide(runs, d, VERDICT)
    assert v.state == NOT_READY and v.missing == (pattern,), render(v)
    runs.append(run("юниты compute", "completed", "success", run_id=900, suite=1))
    assert decide(runs, d, VERDICT).state == GREEN


def test_legs_beyond_the_first_are_still_judged() -> None:
    """Нога сверх первой — не лишняя: её отказ красит вердикт, хотя образец уже
    доказан зелёной ногой."""
    d = tree_declared()
    runs = all_green(d) + [run("юниты compute", "completed", "failure", run_id=901, suite=1)]
    v = decide(runs, d, VERDICT)
    assert v.state == RED and v.offenders == ("юниты compute",), render(v)


def test_a_declared_check_is_proven_only_by_the_actions_app() -> None:
    """Джобу процесса заводит приложение процессов. Одноимённый прогон другого
    приложения её появления не доказывает. Близнец — тот же прогон от
    приложения процессов (проба «законный близнец» выше)."""
    d = tree_declared()
    runs = all_green(d)
    victim = next(r for r in runs if r["name"] == "gosec (Go static analysis)")
    victim["app"] = {"slug": "github-advanced-security"}
    v = decide(runs, d, VERDICT)
    assert v.state == NOT_READY and v.missing == ("gosec (Go static analysis)",), render(v)


SHARD = "e2e ${{ matrix.shard.id }} (${{ matrix.shard.suites }})"


def _one_request_workflow(jobs: dict, request: object = None) -> dict[str, object]:
    """Свод в своём процессе и проверяемый процесс рядом: фильтр второго не
    задевает первого."""
    return {
        "verdict.yml": {"on": "pull_request", "jobs": {"v": {"name": VERDICT}}},
        "probe.yml": {"on": {"pull_request": request}, "jobs": jobs},
    }


def test_a_leg_cut_by_the_provider_proves_its_pattern() -> None:
    """Имя ноги длиннее 100 байт площадка обрезает — обрезанное имя образцу не
    отвечает целиком, но отвечает его началом. Близнец — обрезанное имя с чужим
    началом: образец не доказан."""
    wf = _one_request_workflow({"shard": {"name": SHARD, "strategy": {
        "matrix": "${{ fromJSON(needs.plan.outputs.matrix) }}"}}})
    d = pr_verdict.declare(wf, LINE_BASE, VERDICT)
    assert d.breaches == () and [c.text for c in d.checks] == [SHARD], d
    long_leg = "e2e edge (" + "geo registry api-gateway " * 6 + ")"
    cut = pr_verdict.provider_cut(long_leg)
    assert cut != long_leg and cut.endswith("..."), cut
    assert decide([run(cut, "completed", "success")], d, VERDICT).state == GREEN
    foreign = pr_verdict.provider_cut("e3e edge (" + "geo registry api-gateway " * 6 + ")")
    v = decide([run(foreign, "completed", "success")], d, VERDICT)
    assert v.state == NOT_READY and v.missing == (SHARD,), render(v)


def test_a_short_name_ending_with_dots_is_not_read_as_cut() -> None:
    """Многоточие на конце короткого имени — часть имени, а не обрезка: такое имя
    доказывает образец только целиком."""
    wf = _one_request_workflow({"shard": {"name": SHARD, "strategy": {
        "matrix": "${{ fromJSON(needs.plan.outputs.matrix) }}"}}})
    d = pr_verdict.declare(wf, LINE_BASE, VERDICT)
    v = decide([run("e2e edge...", "completed", "success")], d, VERDICT)
    assert v.state == NOT_READY and v.missing == (SHARD,), render(v)


# ── Какие процессы запрос запускает: фильтр события (#2910) ──────────────────

def _names(d) -> set[str]:
    return {c.text for c in d.checks}


def test_a_workflow_whose_branches_skip_the_base_is_not_declared() -> None:
    """Процесс, чей фильтр ветки не берёт базу запроса, на запросе не идёт — его
    джоб не ждут. Близнец — та же правка на запросе в ствол: джобы объявлены."""
    wf = tree_workflows()
    _request_filter(wf[UI])["branches"] = ["main"]
    on_line, on_trunk = tree_declared(workflows=wf), tree_declared("main", workflows=copy.deepcopy(wf))
    assert on_line.breaches == () and on_trunk.breaches == (), (on_line, on_trunk)
    assert "unit (host)" not in _names(on_line) and "unit (host)" in _names(on_trunk)
    assert on_line.on_request == on_trunk.on_request - 1


def test_branches_ignore_and_negation_follow_the_provider() -> None:
    """`branches-ignore` снимает процесс с базы, которую называет; `!` в
    `branches` отменяет более ранний образец. Близнец в каждой паре — соседняя
    база, которую запись не называет."""
    for body in ({"branches-ignore": [LINE_BASE]}, {"branches": ["[0-9]+", "!" + LINE_BASE]}):
        wf = _one_request_workflow({"j": {"name": "джоба"}}, body)
        assert _names(pr_verdict.declare(wf, LINE_BASE, VERDICT)) == set(), body
        assert _names(pr_verdict.declare(wf, "2796", VERDICT)) == {"джоба"}, body


def test_a_branch_glob_star_does_not_cross_a_slash() -> None:
    """`*` не переходит `/`, `**` переходит, `[0-9]+` требует цифр целиком."""
    table = (
        (["lane/*"], "lane/x", True), (["lane/*"], "lane/x/y", False),
        (["lane/**"], "lane/x/y", True), (["[0-9]+"], "2797", True),
        (["[0-9]+"], "2797-wip", False), (["main"], "main", True), (["main"], "mainline", False),
    )
    for patterns, base, want in table:
        wf = _one_request_workflow({"j": {"name": "джоба"}}, {"branches": patterns})
        got = "джоба" in _names(pr_verdict.declare(wf, base, VERDICT))
        assert got == want, (patterns, base, got)


def test_a_path_filter_is_not_evaluated_and_refuses() -> None:
    """Фильтр по путям зависит от изменения запроса, а его разбор не читает:
    вычислять не из чего — нарушение, а не молчаливое «идёт» или «не идёт».
    Близнец — дерево как есть (проба объявления выше)."""
    for key in ("paths", "paths-ignore"):
        wf = tree_workflows()
        _request_filter(wf[CI])[key] = ["services/**"]
        breaches = tree_declared(workflows=wf).breaches
        assert any("ci.yaml" in b and key in b for b in breaches), (key, breaches)


def test_types_without_a_default_kind_refuse() -> None:
    """Свод идёт на видах по умолчанию. Процесс, чьи `types` не берут хоть один из
    них, на новой голове может не пойти — вычислить нельзя. Близнец — `types`
    шире умолчания: объявление то же."""
    wf = tree_workflows()
    _request_filter(wf[REVIEW])["types"] = ["opened", "reopened", "edited"]
    breaches = tree_declared(workflows=wf).breaches
    assert any("review-text.yml" in b and "synchronize" in b for b in breaches), breaches
    twin = tree_workflows()
    _request_filter(twin[REVIEW])["types"].append("labeled")
    assert tree_declared(workflows=twin).breaches == ()
    assert _names(tree_declared(workflows=twin)) == _names(tree_declared())


def test_an_unknown_request_filter_key_refuses() -> None:
    """Ключ фильтра, которого разбор не знает, мог бы сузить запуск: нарушение."""
    wf = tree_workflows()
    _request_filter(wf[CI])["tags"] = ["v*"]
    assert any("ci.yaml" in b and "tags" in b for b in tree_declared(workflows=wf).breaches)


def test_pull_request_target_refuses() -> None:
    """Процесс на `pull_request_target` исполняется в контексте базы: где лягут
    его прогоны, этот разбор не выводит — нарушение."""
    wf = tree_workflows()
    on = wf[UI]["on"] if "on" in wf[UI] else wf[UI][True]
    on["pull_request_target"] = None
    assert any("ui.yml" in b and "pull_request_target" in b
               for b in tree_declared(workflows=wf).breaches)


def test_every_trigger_spelling_is_read() -> None:
    """`on: pull_request`, `on: [pull_request]` и `pull_request:` без тела — любая
    база и виды по умолчанию: процесс объявлен."""
    for on in ("pull_request", ["push", "pull_request"], {"pull_request": None}):
        wf = {"verdict.yml": {"on": "pull_request", "jobs": {"v": {"name": VERDICT}}},
              "probe.yml": {"on": on, "jobs": {"j": {"name": "джоба"}}}}
        assert _names(pr_verdict.declare(wf, "release/any", VERDICT)) == {"джоба"}, on


def test_the_own_job_must_be_found_once() -> None:
    """Свод исключает себя по имени. Имя, которого нет среди джоб запроса, не
    исключает ничего — и свод ждал бы сам себя; пустое имя — то же."""
    for wrong in ("не та джоба", ""):
        breaches = tree_declared(self_name=wrong).breaches
        assert any("собственн" in b for b in breaches), (wrong, breaches)


def test_an_empty_or_unparsed_tree_refuses() -> None:
    """Ноль процессов, процесс не объектом, джобы не объектом, пустая база — не
    «проверять нечего», а «не вычислено»."""
    assert pr_verdict.declare({}, LINE_BASE, VERDICT).breaches
    assert any("probe.yml" in b for b in pr_verdict.declare(
        {**_one_request_workflow({}), "probe.yml": "строка"}, LINE_BASE, VERDICT).breaches)
    assert any("probe.yml" in b for b in pr_verdict.declare(
        {"verdict.yml": {"on": "pull_request", "jobs": {"v": {"name": VERDICT}}},
         "probe.yml": {"on": "pull_request", "jobs": ["j"]}}, LINE_BASE, VERDICT).breaches)
    assert any("баз" in b for b in tree_declared(base="").breaches)


def test_a_reusable_workflow_call_refuses_the_declaration() -> None:
    """Имена джоб вызванного процесса не вычисляются — объявить их нельзя."""
    wf = tree_workflows()
    wf[UI]["jobs"]["called"] = {"uses": "./.github/workflows/ui.yml"}
    assert any("jobs.called" in b for b in tree_declared(workflows=wf).breaches)


# ── Скрипт целиком: разбор дерева, ожидание, коды возврата (#2910) ────────────
#
# Пробы выше зовут функции. Эти зовут НАСТОЯЩИЙ скрипт и НАСТОЯЩЕЕ ожидание с
# каталогом процессов дерева — так, как их исполняет задание свода, — иначе
# провязка аргументов осталась бы непроверенной.

SCRIPT = Path(__file__).with_name("pr-verdict.py")
WAIT = Path(__file__).with_name("pr-verdict-wait.sh")


def cli(payload: object, *args: str) -> tuple[int, str]:
    p = subprocess.run([sys.executable, str(SCRIPT), *args], input=json.dumps(payload),
                       capture_output=True, text=True, timeout=120, check=False)
    return p.returncode, p.stdout + p.stderr


def test_the_script_derives_the_declaration_from_the_tree() -> None:
    """Код 0 — все объявленные зелены; 3 — наблюдённый набор PR #2908; 1 — одна
    упала. Каждый исход печатает перепись объявления."""
    d = tree_declared()
    args = ("--self-name", VERDICT, "--workflows", str(WORKFLOWS), "--base", LINE_BASE)
    code, out = cli(all_green(d), *args)
    assert code == 0 and f"объявлено проверок {len(d.checks)}" in out, (code, out)
    on_request = _request_workflows_independently(tree_workflows())
    assert f"идут на запросе в «{LINE_BASE}» {on_request}" in out, out
    code, out = cli(list(OBSERVED_2908), *args)
    assert code == 3 and "осмотрено проверок 1" in out, (code, out)
    red = all_green(d)
    red[0]["conclusion"] = "failure"
    assert cli(red, *args)[0] == 1


def test_the_script_refuses_without_a_declaration(tmp_path: Path) -> None:
    """Без базы, без каталога процессов, с пустым каталогом — код 2, «вердикта
    нет», и зелёный набор его не спасает."""
    green = all_green(tree_declared())
    for args in (
        ("--self-name", VERDICT, "--workflows", str(WORKFLOWS), "--base", ""),
        ("--self-name", VERDICT, "--workflows", str(tmp_path), "--base", LINE_BASE),
        ("--self-name", VERDICT, "--base", LINE_BASE),
    ):
        code, out = cli(green, *args)
        assert code == 2, (args, code, out)


def test_a_payload_of_the_wrong_shape_is_not_decided() -> None:
    """JSON не той формы — «вердикт не вынесен» (2), а не трасса с кодом 1:
    ожидание прочло бы 1 как «красно» и выдало бы сбой разбора за вердикт."""
    args = ("--self-name", VERDICT, "--workflows", str(WORKFLOWS), "--base", LINE_BASE)
    for junk in (42, "строка", None):
        code, out = cli(junk, *args)
        assert code == 2 and "НЕ «зелено»" in out and "Traceback" not in out, (junk, code, out)


def _wait(tmp_path: Path, payload: object, base: str | None) -> tuple[int, str]:
    fetch = tmp_path / "fetch.sh"
    (tmp_path / "payload.json").write_text(json.dumps(payload), encoding="utf-8")
    fetch.write_text(f"#!/usr/bin/env bash\ncat '{tmp_path / 'payload.json'}'\n", encoding="utf-8")
    fetch.chmod(0o700)
    env = {k: v for k, v in os.environ.items() if k != "BASE"}
    env.update(REPO="owner/repo", SHA="deadbeef", SELF=VERDICT, VERDICT_ATTEMPTS="2",
               VERDICT_INTERVAL="0", VERDICT_FETCH_CMD=str(fetch))
    if base is not None:
        env["BASE"] = base
    p = subprocess.run(["bash", "-e", str(WAIT)], cwd=tmp_path, env=env,
                       capture_output=True, text=True, timeout=300, check=False)
    return p.returncode, p.stdout + p.stderr


def test_the_wait_passes_the_tree_and_the_base_to_the_decider(tmp_path: Path) -> None:
    """Ожидание зовёт решатель с каталогом процессов дерева и базой запроса: все
    объявленные зелены — 0; наблюдённый набор — заходы кончились, вердикта нет;
    база не передана — «вердикт не вынесен»."""
    d = tree_declared()
    assert _wait(tmp_path, {"total_count": len(d.checks), "check_runs": all_green(d)},
                 LINE_BASE)[0] == 0
    code, out = _wait(tmp_path, {"total_count": 2, "check_runs": list(OBSERVED_2908)}, LINE_BASE)
    assert code == 1 and "не завершились" in out and "golangci-lint" in out, (code, out)
    code, out = _wait(tmp_path, {"total_count": len(d.checks), "check_runs": all_green(d)}, None)
    assert code == 1 and "вердикт не вынесен (код 2)" in out, (code, out)
