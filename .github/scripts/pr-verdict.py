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

ПО КАЖДОМУ ИМЕНИ СУДИТСЯ ПОСЛЕДНИЙ ПРОГОН, НЕСУЩИЙ ИСХОД (#2865)
-------------------------------------------------------------
Check-runs одной sha приходят из разных наборов: событие `edited` запускает
процесс заново, `cancel-in-progress` снимает прежний прогон, и отменённый
остаётся на sha навсегда. Судить все прогоны значит красить вердикт отменой,
у которой есть замена, — и перезапуск свода давал бы то же красное.

Поэтому одноимённые прогоны одного приложения сводятся к одному:

* «позднее» — больший `id`: у замены он больше (на 9672c86fac5 отменённый
  108278452901, замена 108278551222). `completed_at` у идущей замены пуст, а
  `started_at` — время старта с точностью до секунды и порядка `id` не
  повторяет (на 00994f05007 прогон с меньшим `id` стартовал позже);
* судится последний прогон, НЕСУЩИЙ ИСХОД: идущий либо завершённый не
  нейтрально. Поздний пропуск ничего не осмотрел и прежний отказ не стирает;
  если исхода не несёт ни один — судится последний;
* прогоны разных приложений с одним именем — разные проверки, а не замена;
* одноимённые прогоны, у которых нет целого `id`, судятся все: без порядка
  замену не доказать, и отмена по-прежнему красит.

Отставленные прогоны не исчезают молча: сводка называет каждый и его `id`.

Вход — JSON от `repos/{repo}/commits/{sha}/check-runs` (или список его элементов)
на stdin. Имя собственной джобы исключается: она завершается последней by
construction, и без исключения вердикт ждал бы сам себя вечно.
"""

from __future__ import annotations

import argparse
import json
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
    # несущего исход: «имя — исход, check-run id».
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
    """Проверка — это имя в пределах приложения, которое о ней сообщает."""
    app = r.get("app")
    owner = (app.get("slug") or app.get("id")) if isinstance(app, dict) else None
    return (str(owner), str(r.get("name")))


def _has_id(r: dict) -> bool:
    rid = r.get("id")
    return isinstance(rid, int) and not isinstance(rid, bool)


def _carries_outcome(r: dict) -> bool:
    """Идёт (исход будет) либо завершился не нейтрально (исход есть)."""
    status = str(r.get("status"))
    if status in PENDING:
        return True
    return status == "completed" and str(r.get("conclusion")) in BLOCKING | {"success"}


def _describe(r: dict) -> str:
    status = str(r.get("status"))
    outcome = str(r.get("conclusion")) if status == "completed" else status
    return f"{r.get('name')} — {outcome}, check-run {r.get('id')}"


def _latest_per_check(runs: list[dict]) -> tuple[list[dict], list[dict]]:
    """Судимые прогоны и отставленные: по одному на проверку (см. шапку)."""
    groups: dict[tuple[str, str], list[dict]] = {}
    for r in runs:
        groups.setdefault(_check_key(r), []).append(r)

    judged: list[dict] = []
    superseded: list[dict] = []
    for group in groups.values():
        if len(group) == 1 or not all(_has_id(r) for r in group):
            judged.extend(group)
            continue
        ordered = sorted(group, key=lambda r: r["id"])
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
            f"(у той же проверки судится последний прогон, несущий исход); "
            f"из судимых зелёных {v.green}, "
            f"не-зелёных {v.blocking}, нейтральных или пропущенных {v.neutralish}, "
            f"идущих {v.pending}")
    if v.offenders:
        head += "\n" + "\n".join(f"   • {n}" for n in v.offenders)
    if v.superseded:
        head += "\nотставлены:\n" + "\n".join(f"   ◦ {d}" for d in v.superseded)
    return head


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
