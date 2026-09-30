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
        run_id: object = None, app: str | None = None, suite: object = None) -> dict:
    r: dict = {"name": name, "status": status, "conclusion": conclusion}
    if run_id is not None:
        r["id"] = run_id
    if app is not None:
        r["app"] = {"slug": app}
    if suite is not None:
        r["check_suite"] = {"id": suite}
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
    v = decide([
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
    v = decide([
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
    assert decide(replaced).state == GREEN, decide(replaced)
    cancelled_last = [
        run(TITLE, "completed", "success", run_id=100, suite=OLD_SUITE),
        run(TITLE, "completed", "cancelled", run_id=200, suite=NEW_SUITE),
    ]
    v = decide(cancelled_last)
    assert v.state == RED, v
    assert v.offenders == (TITLE,), v


def test_a_rerun_of_an_older_suite_does_not_supersede_a_fresher_failure() -> None:
    """Повтор попытки СТАРОГО набора получает больший id, но судит старый
    контекст: `review-text` читает текст из полезной нагрузки события, и повтор
    набора первого события судит ПРЕЖНИЙ текст. Свежий отказ он не отставляет.

    Замер: ui.yml, прогон 35183091753 — у попытки 2 тот же набор 95281882850,
    что у попытки 1, а id прогонов больше."""
    v = decide([
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
    v = decide([
        run(TITLE, "completed", "success", run_id=100, suite=OLD_SUITE),
        run(TITLE, "completed", "failure", run_id=200, suite=NEW_SUITE),
        run(TITLE, "completed", "success", run_id=300, suite=NEW_SUITE),
    ])
    assert v.state == GREEN, v
    assert len(v.superseded) == 2, v


def test_a_failure_replaced_by_a_later_success_is_green() -> None:
    """Исправленный заголовок: прогон на `edited` заменяет прежний отказ."""
    v = decide([
        run(TITLE, "completed", "failure", run_id=100, suite=OLD_SUITE),
        run(TITLE, "completed", "success", run_id=200, suite=NEW_SUITE),
    ])
    assert v.state == GREEN, v


def test_a_cancelled_run_whose_replacement_still_runs_is_not_ready() -> None:
    """Замена ещё идёт — вердикта нет, ждать. Красное здесь обрывало бы ожидание
    на первом заходе, хотя замена ещё скажет своё."""
    v = decide([
        run("ci", "completed", "success", run_id=10, suite=1),
        run(TITLE, "completed", "cancelled", run_id=100, suite=OLD_SUITE),
        run(TITLE, "in_progress", run_id=200, suite=NEW_SUITE),
    ])
    assert v.state == NOT_READY, v
    assert v.offenders == (TITLE,), v


def test_a_later_skip_does_not_erase_an_earlier_failure() -> None:
    """Пропуск ничего не осмотрел — он не замена. Иначе отказ превращался бы в
    «нейтральное», и соседняя зелёная проверка давала бы «зелено»."""
    v = decide([
        run("ci", "completed", "success", run_id=10, suite=1),
        run(TITLE, "completed", "failure", run_id=100, suite=OLD_SUITE),
        run(TITLE, "completed", "skipped", run_id=200, suite=NEW_SUITE),
    ])
    assert v.state == RED, v
    assert v.offenders == (TITLE,), v


def test_a_later_skip_after_a_success_keeps_the_success() -> None:
    """Парный контроль к предыдущей: поздний пропуск при состоявшемся успехе
    вердикта не портит — судится прогон, несущий исход."""
    v = decide([
        run(TITLE, "completed", "success", run_id=100, suite=OLD_SUITE),
        run(TITLE, "completed", "skipped", run_id=200, suite=NEW_SUITE),
    ])
    assert v.state == GREEN, v
    assert (v.green, v.neutralish) == (1, 0), v


def test_same_name_from_another_app_is_not_a_replacement() -> None:
    """Имя проверки не принадлежит одному поставщику: одноимённый прогон
    ДРУГОГО приложения — отдельная проверка, а не замена."""
    v = decide([
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
    v = decide([
        run(TITLE, "completed", "cancelled", suite=OLD_SUITE),
        run(TITLE, "completed", "success", suite=NEW_SUITE),
    ])
    assert v.state == RED, v
    assert v.superseded == (), v


def test_without_a_suite_no_replacement_is_assumed() -> None:
    """Номер набора — половина порядка. Без него повтор старого контекста не
    отличить от замены, поэтому прогоны без набора судятся все — как без id.
    Близнец — первая проба раздела: те же прогоны С наборами зелены."""
    v = decide([
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
        with_bool = decide([
            run(TITLE, "completed", "cancelled", **bad),
            run(TITLE, "completed", "success", run_id=200, suite=NEW_SUITE),
        ])
        assert with_bool.state == RED, (bad, with_bool)
        assert with_bool.superseded == (), (bad, with_bool)
        with_int = decide([
            run(TITLE, "completed", "cancelled", **good),
            run(TITLE, "completed", "success", run_id=200, suite=NEW_SUITE),
        ])
        assert with_int.state == GREEN, (good, with_int)


def test_the_report_names_what_was_set_aside() -> None:
    """Отставленный прогон не исчезает молча: сводка называет его, его id и
    его набор."""
    text = render(decide([
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


def _dynamic(name: str) -> dict:
    return {"name": name, "runs-on": "ubuntu-latest", "steps": [],
            "strategy": {"matrix": "${{ fromJSON(needs.plan.outputs.matrix) }}"}}


def test_two_dynamic_matrix_names_with_a_compatible_head_break_the_premise() -> None:
    """Два образца. Как есть они совпадают, только если совместимы и начало, и
    конец: `юниты ${{ matrix.shard.id }}` во втором процессе совпадёт с джобой
    `unit-shard`. Но выражение бывает любой длины, и длинное имя GitHub
    обрезает до 97 байт — конец в показе не участвует. Поэтому совместимого
    начала довольно: `e2e … [ … ]` против `e2e … ( … )` из `e2e-newman.yml`.
    Близнец меняет один факт — начало."""
    wf = tree_workflows()
    wf[UI]["jobs"]["extra"] = _dynamic("юниты ${{ matrix.shard.id }}")
    breaches = premise(wf).breaches
    assert any("jobs.extra" in b and "unit-shard" in b for b in breaches), breaches
    wf = tree_workflows()
    wf[UI]["jobs"]["extra"] = _dynamic("e2e ${{ matrix.shard.id }} [${{ matrix.shard.suites }}]")
    breaches = premise(wf).breaches
    assert any("jobs.extra" in b and "e2e-newman.yml: jobs.shard" in b and "97 байт" in b
               for b in breaches), breaches
    twin = tree_workflows()
    twin[UI]["jobs"]["extra"] = _dynamic("юнит-шард ${{ matrix.shard.id }}")
    assert premise(twin).breaches == (), premise(twin).breaches


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


# ── Показ GitHub: имя длиннее 100 байт обрезается до 97 байт и многоточия ─────
#
# `decide` ключуется именем прогона, а не строкой YAML. Имя длиннее 100 байт
# GitHub показывает первыми 97 байтами и `...`: `ci.yaml`, джоба
# `pg-outside-selection` — 109 байт в YAML, 100 в прогоне. Имена, различные в
# YAML, но равные в показе, — одна проверка для `decide`.

PG = "pg-outside-selection"
# Измерено на прогоне, а не взято из кода под пробой: мутант константы там
# должен краснеть здесь.
LIMIT, KEPT = 100, 97


def _pg_name() -> str:
    return tree_workflows()[CI]["jobs"][PG]["name"]


def _with_job(where: str, job_id: str, name: str) -> dict[str, object]:
    wf = tree_workflows()
    wf[where]["jobs"][job_id] = {"name": name, "runs-on": "ubuntu-latest", "steps": []}
    return wf


def _at_byte(name: str, index: int, char: str) -> str:
    """Имя, у которого байт `index` заменён однобайтным `char`."""
    raw = name.encode()
    assert raw[index:index + 1].isascii(), (name, index)
    return (raw[:index] + char.encode() + raw[index + 1:]).decode()


def test_the_tree_holds_a_name_longer_than_the_limit() -> None:
    """Предпосылка проб ниже: в дереве есть имя длиннее 100 байт, и его 97-й
    байт — граница символа. Пропадёт — пробы поменяют предмет молча."""
    raw = _pg_name().encode()
    assert len(raw) > LIMIT, (len(raw), _pg_name())
    assert raw[:KEPT].decode("utf-8", "ignore").encode() == raw[:KEPT], raw[:KEPT]
    assert raw[KEPT - 1:KEPT] == b"=" and raw[KEPT:KEPT + 1] == b" ", raw[KEPT - 3:KEPT + 3]


def test_k1_names_equal_in_the_first_97_bytes_break_the_premise() -> None:
    """K1: джоба другого процесса, чьё имя совпадает с именем
    `pg-outside-selection` в первых 97 байтах и расходится в 98-м. В YAML имена
    различны, в показе — одно: её отказ отставлялся бы успехом той. Близнец
    меняет 97-й байт — последний, который GitHub оставляет."""
    hit = _at_byte(_pg_name(), KEPT, "_")
    breaches = premise(_with_job(UI, "k1", hit)).breaches
    assert any(f"{CI}: jobs.{PG}" in b and f"{UI}: jobs.k1" in b and hit in b
               and _pg_name() in b and "97 байт" in b for b in breaches), breaches
    twin = _at_byte(_pg_name(), KEPT - 1, "_")
    assert premise(_with_job(UI, "k1", twin)).breaches == (), premise(_with_job(UI, "k1", twin))


def test_a_name_of_100_bytes_is_shown_whole() -> None:
    """Граница обрезки — «длиннее 100 байт». Два имени по 100 байт, равные в
    первых 97, показываются целиком и различны. Близнец — те же имена на байт
    длиннее: оба обрезаются, и показ у них один."""
    stem = "x" * 97
    a, b = stem + "abc", stem + "abd"
    wf = _with_job(UI, "a", a)
    wf[CI]["jobs"]["b"] = {"name": b, "runs-on": "ubuntu-latest", "steps": []}
    assert premise(wf).breaches == (), premise(wf).breaches
    wf = _with_job(UI, "a", a + "e")
    wf[CI]["jobs"]["b"] = {"name": b + "e", "runs-on": "ubuntu-latest", "steps": []}
    assert any("jobs.a" in x and "jobs.b" in x for x in premise(wf).breaches), premise(wf)


def test_a_literal_equal_to_the_shown_form_of_a_long_name_breaks_the_premise() -> None:
    """Имя не длиннее 100 байт, которое дословно равно показу длинного: GitHub
    покажет их одинаково. Близнец — то же имя без последней точки."""
    shown = _pg_name().encode()[:KEPT].decode() + "..."
    assert len(shown.encode()) == LIMIT, shown
    breaches = premise(_with_job(UI, "same", shown)).breaches
    assert any(f"jobs.{PG}" in b and "jobs.same" in b for b in breaches), breaches
    twin = shown[:-1]
    assert premise(_with_job(UI, "same", twin)).breaches == (), premise(_with_job(UI, "same", twin))


def test_a_cut_through_a_multibyte_character_backs_off_to_its_boundary() -> None:
    """Разрез посреди многобайтного символа не измерен. Обход отступает к
    границе символа: два длинных имени, равные до неё и различные лишь в
    разрезанном символе, считаются возможным совпадением — грубее истины, но
    не пропускает. Близнец расходится до границы."""
    stem = "y" * 96
    a, b = stem + "ж" + "z" * 8, stem + "ш" + "z" * 8
    assert a.encode()[:97] != b.encode()[:97], (a, b)
    wf = _with_job(UI, "a", a)
    wf[CI]["jobs"]["b"] = {"name": b, "runs-on": "ubuntu-latest", "steps": []}
    assert any("jobs.a" in x and "jobs.b" in x for x in premise(wf).breaches), premise(wf)
    wf = _with_job(UI, "a", "w" + a[1:])
    wf[CI]["jobs"]["b"] = {"name": b, "runs-on": "ubuntu-latest", "steps": []}
    assert premise(wf).breaches == (), premise(wf).breaches


def test_a_dynamic_name_is_compared_in_its_shown_form_too() -> None:
    """Имя из выражения бывает любой длины — значит, и обрезанным. Образец,
    чьё начало совместимо с показом длинного литерала, может с ним совпасть,
    даже если как есть не совпадает (другой конец). Начало длиннее 97 байт
    показывается само по себе. Близнецы расходятся в первых 97 байтах."""
    pg = _pg_name()
    head = pg.split(" (")[0] + " "
    assert pg.startswith(head) and len(head.encode()) < KEPT, head
    cases = (
        (head + "${{ matrix.x }} [другой конец]",
         "Z" + head[1:] + "${{ matrix.x }} [другой конец]"),
        (pg + " (${{ matrix.x }})", _at_byte(pg, KEPT - 1, "_") + " (${{ matrix.x }})"),
    )
    for hit, miss in cases:
        wf = tree_workflows()
        wf[UI]["jobs"]["extra"] = _dynamic(hit)
        breaches = premise(wf).breaches
        assert any(f"jobs.{PG}" in b and "jobs.extra" in b and "97 байт" in b
                   for b in breaches), (hit, breaches)
        twin = tree_workflows()
        twin[UI]["jobs"]["extra"] = _dynamic(miss)
        assert premise(twin).breaches == (), (miss, premise(twin).breaches)


# ── Ветви обхода, которых не держала ни одна проба (#2891, P3 P16 P17 P18) ────

def test_a_pull_request_target_workflow_is_examined() -> None:
    """Процесс, запускаемый только `pull_request_target`, — тоже процесс
    запроса. Копия джобы `review-text.yml` в нём — нарушение. Близнец — та же
    копия, пока процесс запускается только по расписанию."""
    job = tree_workflows()[REVIEW]["jobs"]["attribution"]
    wf = tree_workflows()
    assert True in wf[FUZZ], "YAML 1.1 читает голый ключ `on` как True"
    wf[FUZZ][True] = {"pull_request_target": {"branches": ["main"]}}
    wf[FUZZ]["jobs"]["attribution-copy"] = copy.deepcopy(job)
    p = premise(wf)
    assert any(TITLE in b and "continuous-fuzz.yml" in b and "review-text.yml" in b
               for b in p.breaches), p.breaches
    assert p.examined == _pr_jobs_declared(wf), p
    twin = tree_workflows()
    twin[FUZZ]["jobs"]["attribution-copy"] = copy.deepcopy(job)
    assert premise(twin).breaches == (), premise(twin).breaches


def test_a_workflow_not_parsed_as_an_object_is_a_breach() -> None:
    """Файл процесса, разобранный не объектом (пустой, список, строка), не
    осмотрен — и это нарушение с его путём, а не пропуск. Близнец — тот же
    путь с разобранным процессом по расписанию."""
    extra = ".github/workflows/extra.yml"
    for doc in (None, [], "on: pull_request"):
        wf = tree_workflows()
        wf[extra] = doc
        assert any(extra in b and "не разобран" in b for b in premise(wf).breaches), \
            (doc, premise(wf))
    twin = tree_workflows()
    twin[extra] = copy.deepcopy(twin[FUZZ])
    assert premise(twin).breaches == (), premise(twin).breaches


def test_a_pr_workflow_without_parsed_jobs_is_a_breach() -> None:
    """Процесс запроса, у которого `jobs` не объект, не осмотрен — нарушение.
    Близнец — то же у процесса по расписанию: запрос его не запускает."""
    for jobs in (None, [], "build"):
        wf = tree_workflows()
        wf[UI]["jobs"] = jobs
        assert any("ui.yml" in b and "нет разобранных джоб" in b for b in premise(wf).breaches), \
            (jobs, premise(wf))
    twin = tree_workflows()
    twin[FUZZ]["jobs"] = []
    assert premise(twin).breaches == (), premise(twin).breaches


def test_a_matrix_without_dimensions_is_a_breach() -> None:
    """Матрица без единого измерения ног не вычисляет — нарушение, а не
    «имя однозначно». Близнец — та же джоба с одним измерением, которое имя
    называет."""
    for matrix in ({}, {"include": []}, {"exclude": [{"pkg": "host"}]}):
        wf = tree_workflows()
        wf[UI]["jobs"]["test"]["strategy"]["matrix"] = copy.deepcopy(matrix)
        assert any("jobs.test" in b and "нет ни одного измерения" in b
                   for b in premise(wf).breaches), (matrix, premise(wf))
    twin = tree_workflows()
    twin[UI]["jobs"]["test"]["strategy"]["matrix"] = {"pkg": ["host"]}
    assert premise(twin).breaches == (), premise(twin).breaches
