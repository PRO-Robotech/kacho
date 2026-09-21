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
* зелёный   — все завершились успехом, и успехов ХОТЯ БЫ ОДИН;
* красный   — есть отказ, отмена, снятие по времени или требование действия;
* не готово — что-то ещё идёт (ждать), либо не появилось НИ ОДНОЙ проверки.

«Ноль проверок» — это НЕ «замечаний нет», это «никто не смотрел», и отличать одно
от другого обязательно: ровно на их неразличении построен дефект, ради которого
написан этот скрипт.

Нейтральные и пропущенные считаются отдельно и НЕ зачитываются в успех: молча
зачесть их в зелёное значило бы повторить тот же класс на уровень ниже.

ТРЕТЬЯ КАТЕГОРИЯ ПЕЧАТАЕТСЯ ОТДЕЛЬНОЙ СТРОКОЙ (#958)
----------------------------------------------------
Работа, чьё условие не создано (стенд не поднялся, посторонний источник не
ответил), СВОЮ категорию узнаёт и называет — и сообщает её аннотацией с
заголовком «НЕ ВЫПОЛНИЛОСЬ». До этого места она доезжала неотличимой от
настоящего красного: у прогона два состояния, и «не выполнилось» отображалось в
отказ БЕЗ СЛЕДА. Читающий вердикт не мог отличить «продукт сломан» от «стенд не
поднялся», а лечатся они противоположным — восемь ночей подряд отказ читался как
дефект.

Имена таких проверок подаёт вызывающий (`--unmet-names-file`), а собирает их
`pr-verdict-wait.sh` из аннотаций не-зелёных проверок.

ЦВЕТ ПРИ ЭТОМ НЕ МЕНЯЕТСЯ, И ЭТО ОБЪЯВЛЕННОЕ РЕШЕНИЕ, А НЕ УМОЛЧАНИЕ. Владелец
2026-09-12, дословно: «сделай так, чтобы можно было гарантировать зелёный; если
что-то не так — в красный». Значит такая проверка остаётся блокирующей: слияние
по ней не разрешается. Меняется то, ЧТО читателю предложат разбирать: число и
перечень печатаются ОТДЕЛЬНО от настоящих красных, а не сливаются с ними.

Список пуст, когда канал недоступен (аннотации не прочитались). Тогда всё
считается красным ровно как прежде: неизвестное обязано быть красным, а не
прощённым.

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
    total: int
    green: int
    blocking: int
    neutralish: int
    pending: int
    reason: str
    offenders: tuple[str, ...] = ()
    # Третья категория: проверки, чья работа САМА объявила «условие не создано».
    # Считается отдельно и НИКОГДА не вычитается из числа блокирующих — она их
    # подмножество, а не соседнее множество.
    unmet: int = 0
    unmet_names: tuple[str, ...] = ()

    @property
    def exit_code(self) -> int:
        return 0 if self.state == GREEN else 1


def _runs(payload: object) -> list[dict]:
    if isinstance(payload, dict):
        payload = payload.get("check_runs", [])
    if not isinstance(payload, list):
        raise ValueError("вход не похож на ответ check-runs: ожидался объект или список")
    return [r for r in payload if isinstance(r, dict)]


def decide(payload: object, self_name: str = "", unmet_names: object = ()) -> Verdict:
    """Вердикт по набору проверок. Чистая функция: ни сети, ни времени, ни файлов."""
    unmet_set = {str(n) for n in (unmet_names or ())}
    runs = [r for r in _runs(payload) if r.get("name") != self_name]

    pending = [r for r in runs if str(r.get("status")) in PENDING]
    done = [r for r in runs if str(r.get("status")) == "completed"]

    blocking = [r for r in done if str(r.get("conclusion")) in BLOCKING]
    neutralish = [r for r in done if str(r.get("conclusion")) in NEUTRALISH]
    green = [r for r in done if str(r.get("conclusion")) == "success"]

    unmet = [r for r in blocking if str(r.get("name")) in unmet_set]

    counts = dict(
        total=len(runs), green=len(green), blocking=len(blocking),
        neutralish=len(neutralish), pending=len(pending),
    )

    # Отказ решает СРАЗУ, не дожидаясь остальных: держать ранеры ради заведомо
    # красного вердикта — расход без предмета.
    if blocking:
        unmet_sorted = tuple(sorted(str(r.get("name")) for r in unmet))
        real = tuple(sorted(str(r.get("name")) for r in blocking
                            if str(r.get("name")) not in unmet_set))
        if unmet_sorted and not real:
            reason = ("не-зелёные проверки есть, и ВСЕ они — «не выполнилось»: "
                      "вердикта о продукте не вынесла ни одна. Слияние не разрешается "
                      "(решение владельца 2026-09-12), но разбирать надо УСЛОВИЕ, "
                      "а не дерево")
        elif unmet_sorted:
            reason = "есть не-зелёные проверки, и часть из них — «не выполнилось»"
        else:
            reason = "есть не-зелёные проверки"
        return Verdict(RED, **counts, reason=reason,
                       offenders=real,
                       unmet=len(unmet_sorted), unmet_names=unmet_sorted)
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
            f"осмотрено проверок {v.total}: зелёных {v.green}, "
            f"не-зелёных {v.blocking}, нейтральных или пропущенных {v.neutralish}, "
            f"идущих {v.pending}")
    # ТРЕТЬЯ КАТЕГОРИЯ — СВОЕЙ СТРОКОЙ, А НЕ В ЧИСЛЕ КРАСНЫХ. Она остаётся
    # блокирующей (решение владельца 2026-09-12), но разбирать её надо иначе:
    # «продукт сломан» и «условие не создано» лечатся противоположным.
    head += (f"\nиз них «не выполнилось» (условие не создано): {v.unmet} — "
             f"вердикта о продукте у них нет")
    if v.offenders:
        head += "\nкрасные (вердикт о продукте вынесен и он отрицательный):"
        head += "\n" + "\n".join(f"   • {n}" for n in v.offenders)
    if v.unmet_names:
        head += "\n«не выполнилось» (разбирать условие, а не дерево):"
        head += "\n" + "\n".join(f"   • {n}" for n in v.unmet_names)
    return head


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--self-name", default="", help="имя собственной джобы (исключается из счёта)")
    ap.add_argument("--unmet-names-file", default="",
                    help="файл с именами проверок, чья работа объявила «не выполнилось» "
                         "(по имени на строку); собирает его pr-verdict-wait.sh из аннотаций")
    args = ap.parse_args()

    unmet: list[str] = []
    if args.unmet_names_file:
        try:
            unmet = [ln.strip() for ln in
                     open(args.unmet_names_file, encoding="utf-8").read().splitlines() if ln.strip()]
        except OSError as exc:
            # Канал недоступен — считаем ВСЁ красным, как прежде. Неизвестное
            # обязано быть красным, а не прощённым.
            print(f"перечень «не выполнилось» не прочитан ({exc}) — третья категория "
                  f"в этом прогоне не различается, и всё не-зелёное считается красным",
                  file=sys.stderr)

    try:
        payload = json.load(sys.stdin)
    except json.JSONDecodeError as exc:
        print(f"ОТКАЗ: вход не разобран как JSON ({exc}) — ни одна проверка не рассмотрена, "
              f"и это НЕ «зелено»", file=sys.stderr)
        return 2

    v = decide(payload, args.self_name, unmet)
    print(render(v))
    # «Не готово» — это не вердикт: вызывающий обязан подождать и спросить снова.
    if v.state == NOT_READY:
        return 3
    return v.exit_code


if __name__ == "__main__":
    sys.exit(main())
