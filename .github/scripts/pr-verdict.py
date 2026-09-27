#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Сводный вердикт запроса на слияние: решает ли набор проверок пускать в ствол.

ПРЕДМЕТ (#614)
--------------
Обязательная проверка блокирует слияние, только когда она СУЩЕСТВУЕТ. Между
открытием запроса и появлением её check-run есть окно, в котором контекста нет ни
как pending, ни как failure, — и слияние проходит. Защита при этом выглядит
исполненной: список контекстов настроен, обход администратора выключен.

Цена измерена: в ствол уехала ревизия, чья обязательная проверка впоследствии
покраснела, и красное осталось незамеченным трое суток. Хуже прямого следствия
косвенное: из «влито» перестаёт следовать «было зелёным».

ПОЧЕМУ РЕШЕНИЕ ЗДЕСЬ, А НЕ В YAML
---------------------------------
Логика вердикта обязана быть проверяемой: ей подают синтетический набор проверок
и требуют предсказанного исхода. Пока она жила шагом workflow, доказать её можно
было только прогоном в конвейере — то есть тем самым способом, надёжность
которого она и призвана обеспечить.

ИСХОДОВ ТРИ, И ТРЕТИЙ НЕ ЗАЧИТЫВАЕТСЯ В ЗЕЛЁНОЕ
-----------------------------------------------
Исход выносится по СУДИМЫМ прогонам — по одному на проверку (раздел ниже).

* зелёный   — все завершились успехом, и успехов ХОТЯ БЫ ОДИН;
* красный   — есть отказ, отмена, снятие по времени или требование действия;
* не готово — что-то ещё идёт (ждать), либо не появилось НИ ОДНОЙ проверки.

«Ноль проверок» — это НЕ «замечаний нет», это «никто не смотрел», и отличать одно
от другого обязательно: ровно на их неразличении построен дефект, ради которого
написан этот скрипт.

Нейтральные и пропущенные считаются отдельно и НЕ зачитываются в успех: молча
зачесть их в зелёное значило бы повторить тот же класс на уровень ниже.

ПО КАЖДОЙ ПРОВЕРКЕ СУДИТСЯ ПОСЛЕДНИЙ ПРОГОН, НЕСУЩИЙ ИСХОД (#2865)
-----------------------------------------------------------------
Check-runs одной sha приходят из разных наборов: событие `edited` запускает
процесс заново, `cancel-in-progress` снимает прежний прогон, и отменённый
остаётся на sha навсегда. Судить все прогоны значит красить вердикт отменой,
у которой есть замена, — и перезапуск свода давал бы то же красное.

Поэтому прогоны одной проверки сводятся к одному:

* «позднее» — пара (`check_suite.id`, `id`), а не один `id`. Набор заводится
  на событие, и повтор попытки его СОХРАНЯЕТ: ui.yml, прогон 35183091753 —
  попытки 1 и 2 в одном наборе 95281882850, а id прогона у попытки 2 больше
  (105079283520 против 105082053758). Один `id` поставил бы повтор старого
  набора позже свежего события, а повтор судит СТАРЫЙ контекст:
  `review-text` читает текст из полезной нагрузки события. Номер набора
  растёт с событием: на 9672c86fac5 отменённый 108278452901 в наборе
  98030781046, замена 108278551222 в наборе 98030864187. `completed_at` у
  идущей замены пуст, а `started_at` — время с точностью до секунды и порядка
  не повторяет (на 00994f05007 прогон с меньшим `id` стартовал позже);
* судится последний прогон, НЕСУЩИЙ ИСХОД: идущий либо завершённый не
  нейтрально. Поздний пропуск ничего не осмотрел и прежний отказ не стирает;
  если исхода не несёт ни один — судится последний;
* прогоны разных приложений с одним именем — разные проверки, а не замена;
* прогоны одной проверки, у которых нет целого `id` или целого номера набора,
  судятся все: без порядка замену не доказать, и отмена по-прежнему красит.

Отставленные прогоны не исчезают молча: сводка называет каждый, его `id` и набор.

ПРЕДПОСЫЛКА КЛЮЧА: ИМЯ ПРОВЕРКИ ОДНОЗНАЧНО
-----------------------------------------
Проверка — это имя в пределах приложения (`app`, `name`). В ответе check-runs
нет ни процесса, ни события, ни запроса, из которых пришёл прогон, поэтому
одноимённая джоба ДРУГОГО процесса была бы принята за замену: её успех
отставил бы чужой отказ. Ключ держится предпосылкой, и стережёт её
`premise()` над разобранными процессами, которые запускает запрос на слияние:

* имя каждой джобы с раскрытой литеральной матрицей встречается один раз —
  внутри процесса и между процессами; имя из выражения — образец, и литерал,
  которому он отвечает, считается совпадением;
* имя матричной джобы называет каждое измерение матрицы, а имя матрицы из
  выражения — хотя бы одно; различимость ног такой матрицы держит её план;
* вызов переиспользуемого процесса не вычисляется и однозначным не считается;
* ноль осмотренных джоб — нарушение, а не «нарушений нет».

Предикат — `test_the_tree_holds_the_check_name_premise` в `pr_verdict_test.py`;
на 3407caf6e95 осмотрено джоб 29, имён 50, нарушений 0.

Граница предпосылки — чего этот обход НЕ стережёт (измерено 2026-09-27):

* другое событие того же процесса на той же sha. Из восьми процессов запроса
  `push` объявляют пять, и у всех он сужен до `main`; `workflow_dispatch`
  объявляют шесть, и ручной запуск на ветке запроса заводит более поздний
  набор тех же имён — он судится как замена;
* другой запрос с той же головой. У `review-text` наборы отдельны на каждый
  запрос, и набор второго запроса судится как замена набора первого.

Вход — JSON от `repos/{repo}/commits/{sha}/check-runs` (или список его элементов)
на stdin. Имя собственной джобы исключается: она завершается последней by
construction, и без исключения вердикт ждал бы сам себя вечно.
"""

from __future__ import annotations

import argparse
import itertools
import json
import re
import sys
from dataclasses import dataclass

# Завершённые исходы, при которых в ствол пускать нельзя.
BLOCKING = {"failure", "cancelled", "timed_out", "action_required", "stale"}
# Завершённые исходы, которые не являются ни успехом, ни отказом.
NEUTRALISH = {"neutral", "skipped"}
# Состояния «ещё не завершилось».
PENDING = {"queued", "in_progress", "waiting", "pending", "requested"}

GREEN, RED, NOT_READY = "зелено", "красно", "не готово"


@dataclass(frozen=True)
class Verdict:
    state: str
    # Осмотрено прогонов, включая отставленные; остальные счётчики — по судимым.
    total: int
    green: int
    blocking: int
    neutralish: int
    pending: int
    reason: str
    offenders: tuple[str, ...] = ()
    # Прогоны, отставленные ради другого прогона той же проверки — последнего,
    # несущего исход: «имя — исход, check-run id, набор».
    superseded: tuple[str, ...] = ()

    @property
    def exit_code(self) -> int:
        return 0 if self.state == GREEN else 1


def _runs(payload: object) -> list[dict]:
    if isinstance(payload, dict):
        payload = payload.get("check_runs", [])
    if not isinstance(payload, list):
        raise ValueError("вход не похож на ответ check-runs: ожидался объект или список")
    return [r for r in payload if isinstance(r, dict)]


def _check_key(r: dict) -> tuple[str, str]:
    """Проверка — это имя в пределах приложения, которое о ней сообщает.

    Однозначность имени — предпосылка, а не свойство входа (см. шапку, `premise`)."""
    app = r.get("app")
    owner = (app.get("slug") or app.get("id")) if isinstance(app, dict) else None
    return (str(owner), str(r.get("name")))


def _is_int(v: object) -> bool:
    # `bool` в python — подкласс `int`, но `true` в JSON — не число и порядка не несёт.
    return isinstance(v, int) and not isinstance(v, bool)


def _order(r: dict) -> tuple[int, int] | None:
    """(набор, id): сначала порядок событий, внутри набора — порядок попыток.
    Нет целого номера набора или целого id — порядка нет."""
    suite = r.get("check_suite")
    sid = suite.get("id") if isinstance(suite, dict) else None
    rid = r.get("id")
    return (sid, rid) if _is_int(sid) and _is_int(rid) else None


def _carries_outcome(r: dict) -> bool:
    """Идёт (исход будет) либо завершился не нейтрально (исход есть)."""
    status = str(r.get("status"))
    if status in PENDING:
        return True
    return status == "completed" and str(r.get("conclusion")) in BLOCKING | {"success"}


def _describe(r: dict) -> str:
    status = str(r.get("status"))
    outcome = str(r.get("conclusion")) if status == "completed" else status
    suite = r.get("check_suite")
    sid = suite.get("id") if isinstance(suite, dict) else None
    return f"{r.get('name')} — {outcome}, check-run {r.get('id')}, набор {sid}"


def _latest_per_check(runs: list[dict]) -> tuple[list[dict], list[dict]]:
    """Судимые прогоны и отставленные: по одному на проверку (см. шапку)."""
    groups: dict[tuple[str, str], list[dict]] = {}
    for r in runs:
        groups.setdefault(_check_key(r), []).append(r)

    judged: list[dict] = []
    superseded: list[dict] = []
    for group in groups.values():
        orders = [_order(r) for r in group]
        if len(group) == 1 or any(o is None for o in orders):
            judged.extend(group)
            continue
        ordered = [r for _, r in sorted(zip(orders, group), key=lambda pair: pair[0])]
        bearing = [r for r in ordered if _carries_outcome(r)]
        pick = (bearing or ordered)[-1]
        judged.append(pick)
        superseded.extend(r for r in ordered if r is not pick)
    return judged, superseded


def decide(payload: object, self_name: str = "") -> Verdict:
    """Вердикт по набору проверок. Чистая функция: ни сети, ни времени, ни файлов."""
    examined = [r for r in _runs(payload) if r.get("name") != self_name]
    runs, set_aside = _latest_per_check(examined)

    pending = [r for r in runs if str(r.get("status")) in PENDING]
    done = [r for r in runs if str(r.get("status")) == "completed"]

    blocking = [r for r in done if str(r.get("conclusion")) in BLOCKING]
    neutralish = [r for r in done if str(r.get("conclusion")) in NEUTRALISH]
    green = [r for r in done if str(r.get("conclusion")) == "success"]

    counts = dict(
        total=len(examined), green=len(green), blocking=len(blocking),
        neutralish=len(neutralish), pending=len(pending),
        superseded=tuple(sorted(_describe(r) for r in set_aside)),
    )

    # Отказ решает СРАЗУ, не дожидаясь остальных: держать ранеры ради заведомо
    # красного вердикта — расход без предмета.
    if blocking:
        return Verdict(RED, **counts, reason="есть не-зелёные проверки",
                       offenders=tuple(sorted(str(r.get("name")) for r in blocking)))
    if pending:
        return Verdict(NOT_READY, **counts, reason="часть проверок ещё идёт",
                       offenders=tuple(sorted(str(r.get("name")) for r in pending)))
    if not runs:
        return Verdict(NOT_READY, **counts,
                       reason="не появилось НИ ОДНОЙ проверки — это не «замечаний нет», "
                              "а «никто не смотрел»")
    if not green:
        return Verdict(RED, **counts,
                       reason="ни одной зелёной проверки: все нейтральны или пропущены, "
                              "то есть предмета проверки не было")
    return Verdict(GREEN, **counts, reason="все проверки завершились успехом")


def render(v: Verdict) -> str:
    head = (f"вердикт: {v.state} — {v.reason}\n"
            f"осмотрено проверок {v.total}, отставлено {len(v.superseded)} "
            f"(у той же проверки судится последний по набору и id прогон, несущий исход); "
            f"из судимых зелёных {v.green}, "
            f"не-зелёных {v.blocking}, нейтральных или пропущенных {v.neutralish}, "
            f"идущих {v.pending}")
    if v.offenders:
        head += "\n" + "\n".join(f"   • {n}" for n in v.offenders)
    if v.superseded:
        head += "\nотставлены:\n" + "\n".join(f"   ◦ {d}" for d in v.superseded)
    return head


# ── Предпосылка ключа (см. шапку) ────────────────────────────────────────────

PR_TRIGGERS = {"pull_request", "pull_request_target"}
_EXPR = re.compile(r"\$\{\{.*?\}\}", re.S)
_MATRIX_REF = re.compile(r"\bmatrix\.([A-Za-z_][A-Za-z0-9_-]*)")
_BARE_MATRIX_REF = re.compile(r"^\s*matrix\.([A-Za-z_][A-Za-z0-9_-]*)\s*$")


@dataclass(frozen=True)
class Premise:
    examined: int                 # джоб процессов запроса
    names: int                    # имён: литеральная матрица раскрыта, образец — одно имя
    breaches: tuple[str, ...]


@dataclass(frozen=True)
class _Name:
    where: str                    # «<процесс>: jobs.<id>»
    text: str                     # имя либо шаблон образца
    parts: tuple[str, ...] | None  # у образца — литеральные куски между выражениями


def _triggers(doc: dict) -> set[str]:
    # YAML 1.1 читает голый ключ `on` как `True`.
    on = doc.get("on", doc.get(True))
    if isinstance(on, str):
        return {on}
    if isinstance(on, (list, dict)):
        return {str(t) for t in on}
    return set()


def _scalar(v: object) -> str:
    return ("true" if v else "false") if isinstance(v, bool) else str(v)


def _job_names(where: str, job_id: str, job: object) -> tuple[list[_Name], list[str]]:
    if not isinstance(job, dict):
        return [], [f"{where}: джоба не разобрана как объект — её имя не вычислено"]
    if "uses" in job:
        return [], [f"{where}: вызов переиспользуемого процесса — имена его джоб "
                    f"(«вызывающий / вызванный») этот обход не вычисляет"]
    template = str(job.get("name", job_id))
    exprs = [m.group(0)[3:-2] for m in _EXPR.finditer(template)]
    parts = tuple(_EXPR.split(template))
    pattern = [_Name(where, template, parts)] if exprs else [_Name(where, template, None)]
    strategy = job.get("strategy")
    matrix = strategy.get("matrix") if isinstance(strategy, dict) else None
    if matrix is None:
        return pattern, []

    refs = {d for e in exprs for d in _MATRIX_REF.findall(e)}
    if not isinstance(matrix, dict):
        if refs:
            return pattern, []
        return pattern, [f"{where}: матрица из выражения, а имя «{template}» не называет "
                         f"ни одного её измерения — ноги получат одно имя"]

    dims = [str(k) for k in matrix if k not in ("include", "exclude")]
    for extra in matrix.get("include") or []:
        if isinstance(extra, dict):
            dims += [str(k) for k in extra if str(k) not in dims]
    if not dims:
        return pattern, [f"{where}: у матрицы нет ни одного измерения — ноги не вычислены"]
    missing = [d for d in dims if d not in refs]
    if missing:
        return pattern, [f"{where}: имя «{template}» не называет измерение матрицы "
                         f"{', '.join(missing)} — ноги по нему получат одно имя"]

    literal = (
        "include" not in matrix and "exclude" not in matrix
        and all(isinstance(matrix[d], list) and matrix[d]
                and all(not isinstance(v, (dict, list)) for v in matrix[d]) for d in dims)
        and all(_BARE_MATRIX_REF.match(e) for e in exprs)
    )
    if not literal:
        return pattern, []
    legs = []
    for values in itertools.product(*(matrix[d] for d in dims)):
        leg = dict(zip(dims, values))
        text = _EXPR.sub(lambda m: _scalar(leg[_BARE_MATRIX_REF.match(m.group(0)[3:-2]).group(1)]),
                         template)
        legs.append(_Name(where, text, None))
    return legs, []


def _fits(text: str, parts: tuple[str, ...]) -> bool:
    return re.fullmatch(".*".join(re.escape(p) for p in parts), text, re.S) is not None


def _may_equal(a: _Name, b: _Name) -> bool:
    """Литерал с литералом — равенство; литерал с образцом — соответствие;
    образцы — совместимые начало и конец. Последнее грубее истины, но не
    пропускает ни одного совпадения: у разных строк разные начало или конец."""
    if b.parts is None:
        a, b = b, a
    if a.parts is None:
        return a.text == b.text if b.parts is None else _fits(a.text, b.parts)
    head_a, head_b, tail_a, tail_b = a.parts[0], b.parts[0], a.parts[-1], b.parts[-1]
    return ((head_a.startswith(head_b) or head_b.startswith(head_a))
            and (tail_a.endswith(tail_b) or tail_b.endswith(tail_a)))


def premise(workflows: dict[str, object]) -> Premise:
    """Однозначно ли имя проверки в процессах запроса. Чистая функция над
    разобранными процессами: `{путь: документ}`."""
    names: list[_Name] = []
    breaches: list[str] = []
    examined = 0
    for path, doc in sorted(workflows.items()):
        if not isinstance(doc, dict):
            breaches.append(f"{path}: процесс не разобран как объект")
            continue
        if not PR_TRIGGERS & _triggers(doc):
            continue
        jobs = doc.get("jobs")
        if not isinstance(jobs, dict):
            breaches.append(f"{path}: у процесса запроса нет разобранных джоб")
            continue
        for job_id, job in jobs.items():
            examined += 1
            got, bad = _job_names(f"{path}: jobs.{job_id}", str(job_id), job)
            names += got
            breaches += bad
    if not examined:
        breaches.append("осмотрено ноль джоб процессов запроса — это не «нарушений нет», "
                        "а «никто не смотрел»")
    for a, b in itertools.combinations(names, 2):
        if _may_equal(a, b):
            breaches.append(f"имена «{a.text}» ({a.where}) и «{b.text}» ({b.where}) могут "
                            f"совпасть — успех одной джобы отставил бы отказ другой")
    return Premise(examined=examined, names=len(names), breaches=tuple(breaches))


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--self-name", default="", help="имя собственной джобы (исключается из счёта)")
    args = ap.parse_args()

    try:
        payload = json.load(sys.stdin)
    except json.JSONDecodeError as exc:
        print(f"ОТКАЗ: вход не разобран как JSON ({exc}) — ни одна проверка не рассмотрена, "
              f"и это НЕ «зелено»", file=sys.stderr)
        return 2

    v = decide(payload, args.self_name)
    print(render(v))
    # «Не готово» — это не вердикт: вызывающий обязан подождать и спросить снова.
    if v.state == NOT_READY:
        return 3
    return v.exit_code


if __name__ == "__main__":
    sys.exit(main())
