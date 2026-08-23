#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""console-run-category.py — какой из ТРЁХ исходов у джобы сквозных проб, названный словом.

ПРЕДМЕТ. Сводка запроса на слияние показывает одну строку с именем проверки и её
цвет. Цвет один и тот же у «продукт сломан» и у «условие не создано», а это
разные исходы: второй из вердикта не вычитается и в зелёное не зачитывается
(`e2e-flow.md` §1, §6). Читатель сводки различить их не может — он видит красноту,
а не журнал, и уходит разбирать продукт там, где предметом была скорость зеркала
пакетов или недоступность стенда.

Наблюдалось (#726): шаг установки системных пакетов браузера не уложился в свой
предел, пробы не начинались вовсе — и всё это пришло в сводку неотличимым от
падения проб. Двадцать пять минут стенда были потрачены, а вердикта по консоли
не получила НИ ОДНА проба.

ЧТО ЭТОТ СКРИПТ ДЕЛАЕТ. Читает исходы ВСЕХ шагов джобы (`toJSON(steps)`) и
называет категорию: «не выполнилось», «красное» или «зелено». Печатает её
заголовком в сводку прогона и аннотацией — то есть ровно туда, куда смотрят из
запроса на слияние.

ЧЕГО ОН НЕ ДЕЛАЕТ. Он НЕ выносит вердикта: вердикт уже вынесен прогоном проб и
гейтом по отчёту, а второй вердикт рядом с первым даёт два разных ответа на один
вопрос. Поэтому он выходит нулём при любой категории — это ОПИСЬ.

ОТКАЗЫВАЕТ ОН ТОЛЬКО ТОГДА, КОГДА СУДИТЬ НЕ О ЧЕМ (код 2): перепись шагов пуста,
названного шага проб в ней нет, условных шагов не осталось. «Ноль находок»
обязано быть отличимо от «ноль прочитанного», иначе разметчик, потерявший вход,
молча объявлял бы всё зелёным.

ЧИТАЕТСЯ `conclusion`, А НЕ `outcome`. `outcome` — исход ДО применения
`continue-on-error`, `conclusion` — после. Джобу роняет второе, и категория
обязана описывать то, что произошло с джобой, а не то, что произошло бы без
послаблений.

ВИДИМОСТЬ ШАГА — ЕГО `id`. Шаг без `id` в контекст `steps` не попадает вовсе,
поэтому этот скрипт его не видит и отнести к «условие не создано» не может.
Полноту держит гейт `internal/repohygiene`
`TestEveryStepOfTheProbeJobIsVisibleToTheCategoriser`.

Запуск:
    python3 .github/scripts/console-run-category.py \\
        --steps <файл с toJSON(steps)> --probe-step probes \\
        --verdict-step report-gate --bookkeeping pods,artifact,category,teardown
    python3 .github/scripts/console-run-category.py --self-test
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path

UNMET = "НЕ ВЫПОЛНИЛОСЬ — условие не создано"
RED = "КРАСНОЕ — пробы консоли упали"
GREEN = "ЗЕЛЕНО — пробы консоли прошли"

# Шаг, которого в переписи нет, считается неисполнявшимся. Это не то же, что
# «прошёл»: пропуск и отсутствие обязаны читаться отдельно от успеха.
ABSENT = "не исполнялся"


def conclusion_of(entry: object) -> str:
    """Исход шага. `conclusion` — после `continue-on-error`, им и роняется джоба."""
    if not isinstance(entry, dict):
        return ABSENT
    for key in ("conclusion", "outcome"):
        val = entry.get(key)
        if isinstance(val, str) and val:
            return val
    return ABSENT


def categorize(
    steps: dict,
    probe: str,
    verdict_ids: list[str],
    bookkeeping_ids: list[str],
    verdict_unmet: bool = False,
) -> tuple[str, list[str], int]:
    """Возвращает (категория, строки вывода, код возврата).

    Код 2 — отказ судить: вход не позволяет отличить «ничего не сломалось» от
    «ничего не прочитано».
    """
    log: list[str] = []

    if not isinstance(steps, dict) or not steps:
        return "", [
            "ОТКАЗ: перепись шагов пуста. Разметчик судил бы о джобе, которой не "
            "читал, и его «зелено» означало бы «ноль прочитанного»."
        ], 2

    if probe not in steps:
        return "", [
            f"ОТКАЗ: шага проб «{probe}» в переписи нет. Либо у него сняли `id` "
            "(тогда он невидим контексту `steps`), либо его переименовали, а "
            "разметчику об этом не сказали. Судить не о чем."
        ], 2

    excluded = {probe, *verdict_ids, *bookkeeping_ids}
    # Служебные шаги, идущие ПОСЛЕ разметчика (и он сам), в переписи отсутствуют
    # by construction — она снимается в момент его запуска. Это норма и молчания
    # не нарушает. А вот вердиктный шаг, которого в переписи нет, — предмет:
    # либо у него сняли `id`, либо его переименовали, и тогда он молча
    # переехал бы из вердиктных в условные, поменяв категорию исхода.
    missing_verdict = [i for i in verdict_ids if i not in steps]
    conditions = {sid: e for sid, e in steps.items() if sid not in excluded}

    if not conditions:
        return "", [
            "ОТКАЗ: условных шагов не осталось — всё названо вердиктным или "
            "служебным. Тогда категория «условие не создано» не может наступить "
            "никогда, и разметчик обещает различение, которого не делает."
        ], 2

    log.append(
        f"=== осмотрено шагов: {len(steps)}; из них условных {len(conditions)}, "
        f"вердиктных {len([i for i in verdict_ids if i in steps])}, "
        f"служебных {len([i for i in bookkeeping_ids if i in steps])} ==="
    )
    if missing_verdict:
        log.append(
            "ВНИМАНИЕ: вердиктных шагов нет в переписи: "
            f"{', '.join(sorted(missing_verdict))}. Категория выносится без них — "
            "проверь, не сняли ли у них `id` и не переименовали ли."
        )

    broken = sorted(sid for sid, e in conditions.items() if conclusion_of(e) == "failure")
    probe_state = conclusion_of(steps[probe])
    verdict_broken = sorted(
        vid for vid in verdict_ids if vid in steps and conclusion_of(steps[vid]) == "failure"
    )

    if broken:
        log.append(f"условие не создано; сломались шаги: {', '.join(broken)}")
        log.append(
            "Пробы вердикта не выносили: часть из них не запускалась вовсе. Это "
            "ТРЕТЬЯ категория исхода — она не вычитается из вердикта и не "
            "зачитывается в зелёное. Разбирать надо шаг из списка выше, а не консоль."
        )
        return UNMET, log, 0

    if probe_state == "failure" or verdict_broken:
        if verdict_broken:
            log.append(f"вердиктные шаги с отказом: {', '.join(verdict_broken)}")

        # СВИДЕТЕЛЬСТВО ВЕРДИКТНОГО ШАГА ПЕРЕВЕШИВАЕТ ИСХОДЫ ШАГОВ (#1041).
        #
        # Условные шаги отработали — значит стенд был достижим В МОМЕНТ, КОГДА ИХ
        # СПРАШИВАЛИ. Это не то же, что «был достижим во время прогона»: имя может
        # перестать разрешаться после проверки и до проб, и тогда все пробы падают
        # на навигации, не дойдя до продукта.
        #
        # Разметчик такого различить не может — он видит только исходы шагов. А
        # гейт по отчёту может: он читает ТЕКСТ каждого отказа и отличает падение
        # на навигации от падения по существу. Его вывод сюда и приезжает.
        #
        # Наблюдалось (прогон 32599157014): 66 проб упали за 500–800 мс с одним и
        # тем же `ERR_NAME_NOT_RESOLVED`, вердикт пришёл красным, и разбор ушёл в
        # консоль — при том что ветка проб не касалась НИ СТРОКОЙ.
        if verdict_unmet:
            log.append(
                "гейт по отчёту сообщил ТРЕТЬЮ категорию: до продукта не дошла ни "
                "одна проба. Условные шаги при этом отработали — значит стенд был "
                "достижим, когда их спрашивали, и перестал быть достижим позже. "
                "Разбирать надо достижимость стенда для браузера, а не консоль."
            )
            return UNMET, log, 0

        log.append(f"прогон проб: {probe_state}; все условные шаги отработали")
        log.append("Условие было создано — значит красное относится к продукту.")
        return RED, log, 0

    if probe_state != "success":
        log.append(
            f"прогон проб: {probe_state} — то есть пробы не выполнялись, при этом "
            "ни один условный шаг отказа не назвал."
        )
        return UNMET, log, 0

    log.append("прогон проб: success; условные и вердиктные шаги отработали")
    return GREEN, log, 0


def emit(category: str, log: list[str]) -> None:
    """Категория идёт в журнал, в сводку прогона и аннотацией."""
    print("\n".join(log))
    if not category:
        return
    print(f"=== КАТЕГОРИЯ ИСХОДА: {category} ===")

    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a", encoding="utf-8") as fh:
            fh.write(f"## Категория исхода: {category}\n\n")
            for line in log:
                fh.write(f"- {line}\n")

    if category == UNMET:
        # Именно аннотация доезжает до сводки запроса на слияние. Без неё
        # различение осталось бы в журнале, куда читатель сводки не заходит.
        print(
            "::error title=НЕ ВЫПОЛНИЛОСЬ (условие не создано)::"
            "Пробы консоли не выносили вердикта: сломался шаг подготовки. "
            "Это не «продукт сломан» — смотри журнал джобы, шаг назван там."
        )
    elif category == RED:
        print("::error title=Пробы консоли красные::Условие было создано, вердикт относится к продукту.")


# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА: разметчик обязан назвать разное на разном и отказать на пустом.
# Не доказавший обоих направлений — не доказательство.
# ─────────────────────────────────────────────────────────────────────────────

_VERDICT = ["report-gate"]
_BOOK = ["pods", "artifact", "category", "teardown"]


def _steps(**kw: str) -> dict:
    """Перепись шагов: имя → исход. Умолчание — success."""
    base = {
        "checkout": "success",
        "install-deps": "success",
        "stand": "success",
        "console": "success",
        "probes": "success",
        "report-gate": "success",
        "pods": "success",
    }
    base.update(kw)
    return {k: {"outcome": v, "conclusion": v} for k, v in base.items()}


def self_test() -> int:
    cases: list[tuple[str, bool, object, object]] = []

    def check(name: str, steps: dict, want_cat: str, want_rc: int, probe: str = "probes",
              unmet: bool = False) -> None:
        cat, _, rc = categorize(steps, probe, _VERDICT, _BOOK, verdict_unmet=unmet)
        ok = (cat == want_cat) and (rc == want_rc)
        cases.append((name, ok, (want_cat or "отказ", want_rc), (cat or "отказ", rc)))

    # ЗЕЛЕНО — законный близнец. Без него «не выполнилось» зеленело бы на всём.
    check("всё отработало → зелено", _steps(), GREEN, 0)

    # НЕ ВЫПОЛНИЛОСЬ — предмет #726: сломалась подготовка, пробы не запускались.
    check(
        "отказ установки пакетов → не выполнилось, а НЕ красное",
        _steps(**{"install-deps": "failure", "probes": "skipped", "report-gate": "skipped"}),
        UNMET, 0,
    )

    # НЕ ВЫПОЛНИЛОСЬ — стенд не поднялся. Тот же класс с другой стороны.
    check(
        "стенд не поднялся → не выполнилось",
        _steps(**{"stand": "failure", "probes": "skipped", "report-gate": "skipped"}),
        UNMET, 0,
    )

    # КРАСНОЕ — условие создано, упали сами пробы.
    check("упали пробы → красное", _steps(probes="failure"), RED, 0)

    # КРАСНОЕ — пробы прошли, но гейт по отчёту нашёл недосчёт исполненного.
    check("гейт отчёта нашёл недосчёт → красное", _steps(**{"report-gate": "failure"}), RED, 0)

    # ─── СВИДЕТЕЛЬСТВО ВЕРДИКТНОГО ШАГА (#1041), ОБЕ СТОРОНЫ ───────────────
    #
    # (а) пробы упали, условные шаги чисты, а гейт по отчёту говорит «до продукта
    #     не дошла ни одна» → НЕ ВЫПОЛНИЛОСЬ, а не красное.
    check("пробы упали + гейт назвал третью категорию → не выполнилось",
          _steps(probes="failure", **{"report-gate": "failure"}), UNMET, 0, unmet=True)

    # (б) ЗАКОННЫЙ БЛИЗНЕЦ: те же исходы шагов БЕЗ свидетельства → красное.
    #     Без этой пары послабление стало бы маской: любое падение проб уводило бы
    #     прогон в «условие не создано».
    check("те же исходы без свидетельства → красное",
          _steps(probes="failure", **{"report-gate": "failure"}), RED, 0, unmet=False)

    # (в) ЗАКОННЫЙ БЛИЗНЕЦ: свидетельство не красит ЗЕЛЁНЫЙ прогон. Флаг обязан
    #     влиять только там, где уже есть отказ, — иначе он меняет исход сам.
    check("свидетельство при зелёном прогоне ничего не меняет", _steps(), GREEN, 0, unmet=True)

    # (г) сломанный УСЛОВНЫЙ шаг остаётся третьей категорией и без свидетельства:
    #     эта ветка решает раньше и от флага не зависит.
    check("сломанное условие → не выполнилось независимо от свидетельства",
          _steps(**{"stand": "failure", "probes": "skipped", "report-gate": "skipped"}),
          UNMET, 0, unmet=False)

    # НЕ ВЫПОЛНИЛОСЬ — пробы не запускались, а условие отказа не назвало.
    # Отдельный случай: «пропущено» не есть «прошло».
    check("пробы пропущены без отказа условий → не выполнилось", _steps(probes="skipped"), UNMET, 0)

    # ЗАКОННЫЙ БЛИЗНЕЦ ДРУГОЙ КОНСТРУКЦИИ: шаг с `continue-on-error` — исход
    # отказ, заключение успех. Джобу он не роняет, категорию менять не вправе.
    # Проверяет, что читается `conclusion`, а не `outcome`.
    soft = _steps()
    soft["pods"] = {"outcome": "failure", "conclusion": "success"}
    soft["disk"] = {"outcome": "failure", "conclusion": "success"}
    check("мягкий отказ (conclusion=success) не меняет категорию", soft, GREEN, 0)

    # НЕ ОТКАЗ, НО ГОВОРИТ: вердиктного шага в переписи нет (сняли `id`,
    # переименовали). Категория выносится по оставшемуся — молчать здесь значило
    # бы дать шагу молча переехать из вердиктных в условные и сменить исход.
    no_gate = _steps()
    del no_gate["report-gate"]
    check("вердиктного шага нет в переписи → категория всё равно называется",
          no_gate, GREEN, 0)

    # ОТКАЗ: перепись пуста — судить не о чем.
    check("пустая перепись → отказ", {}, "", 2)

    # ОТКАЗ: названного шага проб в переписи нет (сняли `id`, переименовали).
    check("шага проб нет в переписи → отказ", _steps(), "", 2, probe="нет-такого")

    # ОТКАЗ: условных шагов не осталось — различение обещано и не делается.
    only = {k: {"conclusion": "success"} for k in ("probes", "report-gate", "pods")}
    check("условных шагов не осталось → отказ", only, "", 2)

    rc = 0
    for name, ok, want, have in cases:
        print(f"  {'ОК ' if ok else 'ПРОВАЛ'} {name} (ждали {want}, получили {have})")
        if not ok:
            rc = 1
    failed = sum(1 for c in cases if not c[1])
    print(f"=== самопроверка: случаев {len(cases)}, провалов {failed} ===")
    return rc


def main() -> int:
    if "--self-test" in sys.argv[1:]:
        return self_test()

    ap = argparse.ArgumentParser()
    ap.add_argument("--steps", required=True, help="файл с toJSON(steps)")
    ap.add_argument("--probe-step", required=True, help="id шага, выносящего вердикт прогоном проб")
    ap.add_argument("--verdict-step", default="", help="id-ы вердиктных шагов через запятую")
    ap.add_argument("--bookkeeping", default="", help="id-ы служебных шагов через запятую")
    # Свидетельство вердиктного шага: он один читает ТЕКСТЫ отказов и потому один
    # может отличить «продукт ответил не то» от «до продукта не дошли». Значение
    # приходит выходом шага; пустое и любое, кроме `true`, читается как «нет
    # свидетельства» — умолчание обязано быть тем, что НЕ выдаёт послаблений.
    ap.add_argument("--verdict-unmet", default="",
                    help="`true`, если гейт по отчёту сообщил третью категорию")
    args = ap.parse_args()

    def ids(raw: str) -> list[str]:
        return [x.strip() for x in raw.split(",") if x.strip()]

    try:
        steps = json.loads(Path(args.steps).read_text(encoding="utf-8") or "{}")
    except (OSError, json.JSONDecodeError) as exc:
        print(f"ОТКАЗ: перепись шагов не прочитана ({exc.__class__.__name__}: {exc}).",
              file=sys.stderr)
        return 2

    category, log, rc = categorize(
        steps, args.probe_step, ids(args.verdict_step), ids(args.bookkeeping),
        verdict_unmet=args.verdict_unmet.strip().lower() == "true",
    )
    if rc != 0:
        print("\n".join(log), file=sys.stderr)
        return rc
    emit(category, log)
    return 0


if __name__ == "__main__":
    sys.exit(main())
