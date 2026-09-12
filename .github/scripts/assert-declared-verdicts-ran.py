#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""assert-declared-verdicts-ran.py — зелёное работы означает «объявленное исполнилось».

ПРЕДМЕТ. Отметку `STAND_PRECONDITION_UNMET=1` ставит тот, кто НАБЛЮДАЛ несозданное
условие: владелец подъёма (посторонний источник чартов не ответил) либо провенанс
ревизии (стенд исполняет ДРУГОЕ дерево). Она гасит шаги ниже, и это верно: вердикт
о чужом дереве относится не к тому предмету.

Неверно другое. Погашенный шаг НЕ ПАДАЕТ — он пропускается, а работа без упавших
шагов выходит `success`. Отсутствие вердикта приходило в сводку запроса на слияние
тем же зелёным кружком, что и вердикт «проверено, замечаний нет». Наблюдалось на
прогонах 34694955300 и 34694955309: консоль пропустила `прогон проб` и гейт по
отчёту и вышла `success`; четыре шарда вышли `success` при нуле исполненных
коллекций из 57.

РЕШЕНИЕ ВЛАДЕЛЬЦА (2026-09-12), которое этот скрипт исполняет: «сделай так, чтобы
можно было гарантировать зелёный; если что-то не так — в красный». Значит исход
работы при погашенном вердикте обязан быть КРАСНЫМ. Различение трёх категорий
остаётся в ДИАГНОСТИКЕ: текст ниже называет причину и говорит прямо, что вердикта
о продукте нет ни у одной проверки работы. Красный с честным текстом — то, что
просил владелец; красный без объяснения — нет.

ЧТО ЭТОТ СКРИПТ ДЕЛАЕТ. Сверяет ОБЪЯВЛЕННОЕ с ИСПОЛНЕННЫМ:

  объявленное — шаги работы, чьё условие ЧИТАЕТ отметку. Перечень ВЫВОДИТСЯ из
                объявления процесса, а не выписывается рядом: выписанный
                разошёлся бы молча, и это класс, который эта линия ловила четырежды;
  исполненное — те же шаги в переписи `toJSON(steps)` с заключением
                `success`/`failure`, то есть давшие РЕЗУЛЬТАТ. `skipped` и
                отсутствие в переписи результатом не являются.

Печатает «объявлено вердиктных шагов N, исполнилось M» и падает при M < N.

ПОЧЕМУ «ГАСИМЫЙ ОТМЕТКОЙ» — ЗАКОННАЯ ЕДИНИЦА СЧЁТА ВЕРДИКТНЫХ ШАГОВ. Требование
«каждый шаг, выносящий вердикт о продукте, гасится отметкой» держит отдельный гейт
(`deploy/scripts/assert-stand-precondition-wiring.py`, звено 4). Значит гасимые ⊇
вердиктные, и выведенный перечень вердиктного шага не теряет НИКОГДА. Обратное
включение не утверждается: среди гасимых бывают и статические гейты дерева, и
служебный провенанс. Требовать их исполнения строже, чем нужно, — и это безопасная
сторона: строгость может потребовать лишнего исполнения, но не может выдать
непроверенное за проверенное.

ЧИТАЕТСЯ `conclusion`, А НЕ `outcome`. `outcome` — исход ДО применения
`continue-on-error`, `conclusion` — после. Работу роняет второе, и перепись обязана
описывать то, что произошло с работой.

ЗАКОННЫЙ ПРОПУСК НАЗЫВАЕТСЯ ВЫЗЫВАЮЩИМ, А НЕ ДОГАДЫВАЕТСЯ ЗДЕСЬ. Условие гасимого
шага бывает составным: у консоли два шага несут ещё одну оговорку — «свой подъём»
(`inputs.console_url`), и при внешнем стенде они законно не исполняются. Такие
оговорки перечисляет вызывающий ключом `--legit-skip <подстрока условия>`, и ТОЛЬКО
в том прогоне, где оговорка ложна. Всякая НЕ названная оговорка законным пропуском
не считается: неизвестное обязано быть красным, а не прощённым.

Коды возврата:
    0 — объявленное исполнилось целиком;
    1 — НЕ исполнилось: вердикта о продукте у работы нет (красное с честным текстом);
    2 — судить не по чему (работы нет в объявлении, гасимых шагов ноль, перепись
        пуста). Это тоже ненулевой код: «ноль прочитанного» не имеет права
        выглядеть как «замечаний нет».

Запуск:
    python3 .github/scripts/assert-declared-verdicts-ran.py \\
        --workflow .github/workflows/console-e2e.yml --job probes \\
        --steps <файл с toJSON(steps)> --flag "$STAND_PRECONDITION_UNMET" \\
        [--legit-skip inputs.console_url]
    python3 .github/scripts/assert-declared-verdicts-ran.py --self-test
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path

import yaml

FLAG = "STAND_PRECONDITION_UNMET"

# Заключения, означающие РЕЗУЛЬТАТ. Всё прочее (`skipped`, отсутствие в переписи)
# результатом не является: пропуск не есть проход.
PRODUCED = ("success", "failure")

ABSENT = "нет в переписи"


def declared_steps(doc: dict, job: str) -> list[dict]:
    """Шаги работы, чьё условие читает отметку. ВЫВЕДЕНО из объявления."""
    jobs = (doc.get("jobs") or {}) if isinstance(doc, dict) else {}
    spec = jobs.get(job)
    if spec is None:
        return []
    steps = list((spec or {}).get("steps") or [])
    return [s for s in steps if FLAG in str(s.get("if") or "")]


def state_of(steps: dict, sid: str) -> str:
    """Заключение шага в переписи прогона."""
    entry = steps.get(sid)
    if not isinstance(entry, dict):
        return ABSENT
    for key in ("conclusion", "outcome"):
        val = entry.get(key)
        if isinstance(val, str) and val:
            return val
    return ABSENT


def adjudicate(
    doc: dict,
    job: str,
    steps: dict,
    flag_set: bool,
    legit_skip: list[str],
) -> tuple[list[str], int, int, int]:
    """Возвращает (строки вывода, объявлено, исполнилось, код возврата)."""
    log: list[str] = []

    declared = declared_steps(doc, job)
    if not declared:
        return [
            f"ОТКАЗ: в работе «{job}» ни одного шага, гасимого отметкой {FLAG}. "
            "Либо названа не та работа, либо не то объявление, либо отметка из неё "
            "ушла. Перепись обещала бы гарантию, которой не даёт: «ноль объявленного» "
            "нельзя отличить от «ноль прочитанного».",
        ], 0, 0, 2

    if not isinstance(steps, dict) or not steps:
        return [
            "ОТКАЗ: перепись шагов прогона пуста. Перепись судила бы о работе, "
            "которой не читала, и её «исполнилось всё» означало бы «ноль прочитанного».",
        ], len(declared), 0, 2

    ran: list[str] = []
    missing: list[tuple[str, str, str]] = []   # (id, имя, заключение)
    forgiven: list[tuple[str, str]] = []       # (id, оговорка)
    invisible: list[str] = []                  # объявлен и без `id`

    for s in declared:
        sid = str(s.get("id") or "").strip()
        name = str(s.get("name") or s.get("uses") or "без имени")
        cond = str(s.get("if") or "")
        if not sid:
            invisible.append(name)
            continue
        st = state_of(steps, sid)
        if st in PRODUCED:
            ran.append(sid)
            continue
        clause = next((c for c in legit_skip if c in cond), "")
        if clause:
            forgiven.append((sid, clause))
            continue
        missing.append((sid, name, st))

    n, m = len(declared), len(ran)
    log.append(f"=== перепись исходов работы «{job}»")
    log.append(f"объявлено вердиктных шагов {n}, исполнилось {m}")
    log.append(
        f"из объявленных: дали результат {m}, без результата {len(missing)}, "
        f"законно пропущено {len(forgiven)}, невидимо переписи (нет `id`) {len(invisible)}"
    )
    for sid in sorted(ran):
        log.append(f"  · {sid}: {state_of(steps, sid)}")
    for sid, clause in sorted(forgiven):
        log.append(f"  · {sid}: пропущен законно — оговорка условия «{clause}» ложна в этом прогоне")

    if not missing and not invisible:
        log.append("Объявленное исполнилось целиком — зелёное этой работы есть вердикт, "
                   "а не отсутствие вопроса.")
        return log, n, m, 0

    # ── ОТКАЗ. Причина называется до перечня: читатель сводки видит её первой.
    if flag_set:
        log.append("")
        log.append(f"**УСЛОВИЕ НЕ СОЗДАНО.** Отметка `{FLAG}=1` выставлена: её ставит тот, "
                   "кто НАБЛЮДАЛ несозданное условие — владелец подъёма (посторонний источник "
                   "чартов не ответил) либо провенанс ревизии (стенд исполняет ДРУГОЕ дерево). "
                   "Причина названа их выводом ВЫШЕ в журнале этой работы и строкой в сводке "
                   "прогона.")
        log.append("Это НЕ дефект дерева и НЕ находка о продукте: вердикта о продукте нет "
                   "НИ У ОДНОЙ проверки этой работы. Разбирать надо условие, а не продукт; "
                   "прогон повторяется.")
        log.append("Красным это стало по решению владельца 2026-09-12: зелёное обязано быть "
                   "гарантией, а «мы не спрашивали» гарантией не является.")
    else:
        log.append("")
        log.append(f"**ВЕРДИКТ НЕ ВЫНЕСЕН, а отметки `{FLAG}` при этом НЕТ.** Значит шаг не "
                   "исполнился по своему условию либо по отказу шага выше. Вердикта о продукте "
                   "у этой работы нет — и зелёное здесь означало бы «мы не спрашивали».")

    for sid, name, st in sorted(missing):
        log.append(f"  ✗ {sid} («{name}») — {st}: результата не дал")
    for name in sorted(invisible):
        log.append(f"  ✗ «{name}» объявлен гасимым и НЕ ИМЕЕТ `id`: в контекст `steps` такой "
                   "шаг не попадает вовсе, поэтому неизвестно даже, исполнялся ли он. "
                   "Неизвестное обязано быть красным, а не прощённым")
    return log, n, m, 1


def emit(log: list[str], job: str, rc: int) -> None:
    """Журнал · сводка прогона · аннотация. Три разных читателя."""
    print("\n".join(log))

    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        try:
            with open(summary, "a", encoding="utf-8") as fh:
                fh.write(f"## Перепись исходов работы «{job}»\n\n")
                for line in log:
                    fh.write(f"{line}\n" if line.startswith(("**", "")) else f"- {line}\n")
                fh.write("\n")
        except OSError as exc:
            print(f"сводка не дописана ({exc}) — вердикт от этого не меняется")

    # Аннотация — единственное, что доезжает до списка проверок запроса на слияние.
    if rc == 1:
        print("::error title=Вердикта НЕТ (условие не создано)::"
              f"работа «{job}»: объявленный вердиктный шаг результата не дал. "
              "Это не «продукт сломан» — причина названа в журнале работы, разбирать надо её. "
              "Зелёным такая работа выйти не вправе: зелёное означает «объявленное исполнилось».")
    elif rc == 2:
        print("::error title=Судить не по чему::"
              f"работа «{job}»: перепись не нашла ни объявленного, ни исполненного. "
              "«Ноль прочитанного» не имеет права выглядеть как «замечаний нет».")


# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА: перепись обязана краснеть на внесённом дефекте и МОЛЧАТЬ на
# законном близнеце той же формы. Каждый случай меняет РОВНО ОДИН факт против
# своей пары — иначе неизвестно, что дало исход.
# ─────────────────────────────────────────────────────────────────────────────

_WF = yaml.safe_load("""
jobs:
  probes:
    steps:
      - name: стенд
        id: stand
        run: stand-up.sh
      - name: провенанс стенда
        id: stand_revision
        if: ${{ (github.event_name != 'workflow_dispatch' || inputs.console_url == '') && env.STAND_PRECONDITION_UNMET != '1' }}
        run: stand-revision-verdict.sh
      - name: прогон проб
        id: probes
        if: ${{ env.STAND_PRECONDITION_UNMET != '1' }}
        run: npx playwright test
      - name: гейт по отчёту
        id: report-gate
        if: ${{ always() && env.STAND_PRECONDITION_UNMET != '1' }}
        run: assert-console-probes-verdict.py
      - name: перепись исходов
        id: verdict-census
        if: ${{ always() }}
        run: assert-declared-verdicts-ran.py
""")


def _steps(**kw: str) -> dict:
    base = {"stand": "success", "stand_revision": "success",
            "probes": "success", "report-gate": "success"}
    base.update(kw)
    return {k: {"outcome": v, "conclusion": v} for k, v in base.items()}


def self_test() -> int:
    cases: list[tuple[str, bool, str]] = []

    def case(name: str, want_rc: int, want_n: int, want_m: int, steps: dict,
             flag: bool = False, legit: list[str] | None = None,
             job: str = "probes", doc: dict | None = None,
             want_text: str = "") -> None:
        log, n, m, rc = adjudicate(doc if doc is not None else _WF, job, steps, flag,
                                  legit or [])
        joined = "\n".join(log)
        ok = (rc == want_rc) and (n == want_n) and (m == want_m)
        if want_text and want_text not in joined:
            ok = False
        cases.append((name, ok,
                      f"ждали код {want_rc} N={want_n} M={want_m}"
                      + (f" и текст «{want_text}»" if want_text else "")
                      + f"; получили код {rc} N={n} M={m}"))

    # (1) КОНТРОЛЬ: всё объявленное исполнилось → зелено. Без него всякое «нашёл»
    #     ниже ничего не значит.
    case("контроль: объявленное исполнилось целиком", 0, 3, 3, _steps(),
         want_text="объявлено вердиктных шагов 3, исполнилось 3")

    # (2) ВНЕСЁН ДЕФЕКТ: отметка выставлена, гасимые шаги пропущены → КРАСНОЕ, и
    #     текст называет причину несозданным условием.
    case("отметка выставлена → красное, причина названа", 1, 3, 0,
         _steps(stand_revision="skipped", probes="skipped", **{"report-gate": "skipped"}),
         flag=True, want_text="УСЛОВИЕ НЕ СОЗДАНО")

    # (3) ЗАКОННЫЙ БЛИЗНЕЦ ТОГО ЖЕ КРАСНОГО: пробы ИСПОЛНИЛИСЬ и упали. Перепись
    #     молчит — вердикт вынесен, и второй ответ на тот же вопрос не нужен.
    #     Без этой половины перепись съела бы настоящую красноту продукта.
    case("пробы упали, но исполнились → перепись молчит", 0, 3, 3,
         _steps(probes="failure", **{"report-gate": "failure"}),
         want_text="Объявленное исполнилось целиком")

    # (4) ВНЕСЁН ДЕФЕКТ: шаг пропущен БЕЗ отметки — текст обязан отличаться от (2),
    #     иначе отладка не различит два красных.
    case("пропуск без отметки → красное с ДРУГИМ текстом", 1, 3, 2,
         _steps(probes="skipped"), want_text="отметки `STAND_PRECONDITION_UNMET` при этом НЕТ")

    # (5) ЗАКОННЫЙ ПРОПУСК, НАЗВАННЫЙ ВЫЗЫВАЮЩИМ: внешний стенд — оговорка
    #     `inputs.console_url` ложна, и шаг с ней законно не исполнялся.
    case("внешний стенд: названная оговорка прощает пропуск", 0, 3, 2,
         _steps(stand_revision="skipped"), legit=["inputs.console_url"],
         want_text="пропущен законно")

    # (6) ЗАКОННЫЙ БЛИЗНЕЦ (5): та же оговорка НЕ названа → пропуск красный.
    #     Без этой половины ключ `--legit-skip` стал бы маской.
    case("та же оговорка не названа → пропуск красный", 1, 3, 2,
         _steps(stand_revision="skipped"))

    # (7) ЗАКОННЫЙ БЛИЗНЕЦ (5): оговорка названа, но пропущен ДРУГОЙ шаг, её не
    #     несущий → по-прежнему красное. Прощение не растекается по работе.
    case("названная оговорка не прощает чужой шаг", 1, 3, 2,
         _steps(probes="skipped"), legit=["inputs.console_url"])

    # (8) ВНЕСЁН ДЕФЕКТ: у гасимого шага сняли `id` — он невидим переписи, и
    #     неизвестно даже, исполнялся ли он. Неизвестное красное, а не прощённое.
    no_id = yaml.safe_load(yaml.safe_dump(_WF))
    del no_id["jobs"]["probes"]["steps"][2]["id"]
    case("гасимый шаг без `id` → красное", 1, 3, 2, _steps(), doc=no_id,
         want_text="НЕ ИМЕЕТ `id`")

    # (9) ЗАКОННЫЙ БЛИЗНЕЦ ЧТЕНИЯ ЗАКЛЮЧЕНИЯ: `continue-on-error` даёт
    #     outcome=failure при conclusion=success. Работу роняет второе, значит шаг
    #     результат ДАЛ.
    soft = _steps()
    soft["probes"] = {"outcome": "failure", "conclusion": "success"}
    case("мягкий отказ (conclusion=success) считается исполненным", 0, 3, 3, soft)

    # (10) ОТКАЗ: названа работа, которой в объявлении нет.
    case("работы нет в объявлении → отказ", 2, 0, 0, _steps(), job="нет-такой",
         want_text="ОТКАЗ")

    # (11) ОТКАЗ: перепись прогона пуста — судить не о чем.
    case("пустая перепись прогона → отказ", 2, 3, 0, {}, want_text="ОТКАЗ")

    # (12) ОТКАЗ: гасимых шагов в работе ноль — гарантия обещана и не даётся.
    plain = yaml.safe_load("jobs:\n  lint:\n    steps:\n      - {name: линт, run: make lint}\n")
    case("работа без гашения → отказ", 2, 0, 0, {"lint": {"conclusion": "success"}},
         job="lint", doc=plain, want_text="ни одного шага, гасимого отметкой")

    rc = 0
    for name, ok, detail in cases:
        print(f"  {'ОК ' if ok else 'ПРОВАЛ'} {name} ({detail})")
        if not ok:
            rc = 1
    failed = sum(1 for c in cases if not c[1])
    print(f"=== самопроверка: случаев {len(cases)}, провалов {failed} ===")
    return rc


def main() -> int:
    if "--self-test" in sys.argv[1:]:
        return self_test()

    ap = argparse.ArgumentParser()
    ap.add_argument("--workflow", required=True, help="путь к объявлению процесса")
    ap.add_argument("--job", required=True, help="имя работы в этом объявлении")
    ap.add_argument("--steps", required=True, help="файл с toJSON(steps)")
    ap.add_argument("--flag", default="", help=f"значение {FLAG} на момент переписи")
    ap.add_argument("--legit-skip", action="append", default=[],
                    help="подстрока условия, оговорка которой в ЭТОМ прогоне ложна")
    args = ap.parse_args()

    try:
        doc = yaml.safe_load(Path(args.workflow).read_text(encoding="utf-8")) or {}
    except (OSError, yaml.YAMLError) as exc:
        print(f"ОТКАЗ: объявление {args.workflow} не прочитано "
              f"({exc.__class__.__name__}: {exc}) — выводить перечень не из чего.",
              file=sys.stderr)
        return 2
    try:
        steps = json.loads(Path(args.steps).read_text(encoding="utf-8") or "{}")
    except (OSError, json.JSONDecodeError) as exc:
        print(f"ОТКАЗ: перепись шагов не прочитана ({exc.__class__.__name__}: {exc}).",
              file=sys.stderr)
        return 2

    log, _n, _m, rc = adjudicate(doc, args.job, steps,
                                args.flag.strip() == "1", list(args.legit_skip))
    emit(log, args.job, rc)
    return rc


if __name__ == "__main__":
    sys.exit(main())
