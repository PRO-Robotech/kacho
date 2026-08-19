#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""assert-stand-precondition-wiring.py — «условие не создано» доезжает до вердикта.

ПРЕДМЕТ (kacho#655). Подъём стенда шарда тянет десять зависимостей чарта с
ПОСТОРОННЕГО хоста. Его недоступность роняла шард целиком: девять коллекций не
выполнялись вовсе, гейты суит краснели «нет отчётов», и в сводке PR это читалось
как красный шард — то есть как дефект продукта. `e2e-flow.md` §6 требует
обратного: «условие не создано — не вердикт: отдельный шаг, отдельное сообщение».

ЧТО ЗДЕСЬ ПРОВЕРЯЕТСЯ — СВЯЗНОСТЬ, А НЕ НАМЕРЕНИЕ. Механизм состоит из четырёх
звеньев, и любое разорванное возвращает исходный дефект МОЛЧА:

  1. владелец материализации кладёт отметку (доказано его же --self-test:
     реальный helm, недостижимый хост → код 3 и отметка; наш сломанный сабчарт →
     код 1 и НИКАКОЙ отметки);
  2. шаг стенда отметку ЧИТАЕТ, объявляет `STAND_PRECONDITION_UNMET` и печатает
     заголовок «условие не создано»;
  3. каждый шаг, выносящий вердикт О ПРОБАХ, при этом ПРОПУСКАЕТСЯ — иначе он
     покраснеет «нет отчётов», и чужая недоступность снова прочитается находкой;
  4. шаг описи, наоборот, НЕ пропускается — он единственный, кто доносит причину
     до свода. Пропустить его значило бы потерять третью категорию у сводки.

Звено 3 — то, что ломается само собой: девятая суита заводится копированием
восьмой, и условие в копию не переносят. Поэтому набор шагов-вердиктов
ВЫВОДИТСЯ ИЗ ТОГО, ЧТО ШАГ ЗАПУСКАЕТ, а не сверяется со списком имён: список
имён пришлось бы дописывать руками ровно тогда же, когда забывают условие.

Звено 2 проверяется ИСПОЛНЕНИЕМ настоящего тела шага (см. `--self-test`), а не
чтением: тело — это shell, и его поведение читается только запуском.

Коды возврата: 0 — связно; 1 — разрыв; 2 — судить не по чему (не нашёл работы,
шага стенда или ни одного шага-вердикта: «ноль находок» обязано быть отличимо от
«ноль прочитанного»).
"""
from __future__ import annotations

import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile

import yaml

REPO = pathlib.Path(__file__).resolve().parents[2]
WORKFLOW = REPO / ".github" / "workflows" / "e2e-newman.yml"
JOB = "shard"
FLAG = "STAND_PRECONDITION_UNMET"
MARK_NAME = ".kacho-deps-precondition-unmet"

# Шаг выносит вердикт О ПРОБАХ, если запускает одно из этого. Признак — то, что
# шаг ИСПОЛНЯЕТ, а не как он назван: имя переписывают свободно, а запуск нет.
VERDICT_RUNNERS = (
    "assert-suites-green.sh",              # гейт суиты: «нет отчётов» → красное
    "assert-ban6-external-isolation.py",   # спрашивает листенер, которого нет
    "coverage.py",                         # считает по отчётам, которых нет
    "newman-live.py report",               # печатает «0 отчётов» как потерю
)
# Шаг, который обязан исполниться ВОПРЕКИ несозданному условию: он и есть тот,
# кто доносит причину до свода. Законный близнец звена 3.
CARRIER_RUNNER = "shard-verdict.py"


def load_steps() -> list[dict]:
    doc = yaml.safe_load(WORKFLOW.read_text(encoding="utf-8"))
    return list(((doc.get("jobs") or {}).get(JOB) or {}).get("steps") or [])


def guarded(step: dict) -> bool:
    return f"env.{FLAG}" in str(step.get("if") or "")


def stand_step(steps: list[dict]) -> dict | None:
    """Шаг подъёма стенда — тот, кто зовёт `make dev-up`. Опять по запуску, не по имени."""
    for s in steps:
        if "make dev-up" in str(s.get("run") or ""):
            return s
    return None


def check(steps: list[dict]) -> tuple[list[str], dict[str, int]]:
    findings: list[str] = []
    seen = {"steps": len(steps), "verdict": 0, "guarded": 0, "carrier": 0}

    stand = stand_step(steps)
    if stand is None:
        findings.append("шага подъёма стенда (`make dev-up`) в работе не нашлось — "
                        "механизм проверять не на чем")
        return findings, seen

    body = str(stand.get("run") or "")
    for needle, why in (
        (MARK_NAME, "шаг стенда не читает отметку — чужой отказ неотличим от нашего"),
        (FLAG, f"шаг стенда не объявляет {FLAG} — шаги ниже о нём не узнают"),
        ("GITHUB_ENV", f"{FLAG} не уходит в GITHUB_ENV — условие шагов ниже не сработает"),
        ("УСЛОВИЕ НЕ СОЗДАНО", "шаг стенда не называет категорию заголовком — "
                               "исход прочтётся как обычная краснота"),
    ):
        if needle not in body:
            findings.append(why)

    for s in steps:
        run = str(s.get("run") or "")
        name = str(s.get("name") or "?")
        if any(r in run for r in VERDICT_RUNNERS):
            seen["verdict"] += 1
            if guarded(s):
                seen["guarded"] += 1
            else:
                findings.append(
                    f"шаг «{name}» выносит вердикт о пробах, но НЕ пропускается при "
                    f"несозданном условии: он покраснеет «нет отчётов», и недоступность "
                    f"постороннего хоста снова прочитается как дефект продукта")
        if CARRIER_RUNNER in run:
            seen["carrier"] += 1
            if guarded(s):
                findings.append(
                    f"шаг «{name}» доносит причину до свода и пропускаться НЕ ДОЛЖЕН: "
                    f"с условием он промолчит, и третья категория до сводки не доедет")

    if seen["verdict"] == 0:
        findings.append("шагов-вердиктов не найдено НИ ОДНОГО — предикат смотрит не туда, "
                        "и «связно» здесь значило бы «ничего не прочитано»")
    if seen["carrier"] == 0:
        findings.append("шага описи не найдено — причину до свода нести некому")
    return findings, seen


# ─────────────────────────────────────────────────────────────────────────────
# ЗВЕНО 2 ПРОВЕРЯЕТСЯ ИСПОЛНЕНИЕМ. Тело шага — shell; прочитать в нём можно
# наличие слов, а поведение — только запуском. `make` подменяется заглушкой,
# потому что предмет пробы — РАЗВИЛКА ПОСЛЕ отказа, а не сам подъём стенда.
def run_stand_body(marker: bool, make_rc: int) -> tuple[int, str, str]:
    body = str(stand_step(load_steps()).get("run") or "")
    body = re.sub(r"\$\{\{[^}]*\}\}", "SHARDID", body)   # подстановки конвейера
    with tempfile.TemporaryDirectory() as td:
        t = pathlib.Path(td)
        (t / "bin").mkdir()
        (t / "bin" / "make").write_text(f"#!/bin/sh\nexit {make_rc}\n", encoding="utf-8")
        (t / "bin" / "make").chmod(0o755)
        (t / "helm" / "umbrella").mkdir(parents=True)
        if marker:
            (t / "helm" / "umbrella" / MARK_NAME).write_text(
                "источник чартов не ответил: https://charts.example.invalid/x\n", encoding="utf-8")
        genv = t / "github-env"
        genv.write_text("", encoding="utf-8")
        env = dict(os.environ,
                   PATH=f"{t / 'bin'}{os.pathsep}{os.environ['PATH']}",
                   GITHUB_ENV=str(genv), CLUSTER_NAME="probe")
        # Та же оболочка, что у конвейера: `defaults.run.shell: bash` — это
        # `bash --noprofile --norc -eo pipefail`.
        p = subprocess.run(["bash", "--noprofile", "--norc", "-eo", "pipefail", "-c", body],
                           cwd=t, env=env, capture_output=True, text=True)
        return p.returncode, p.stdout + p.stderr, genv.read_text(encoding="utf-8")


def _self_test() -> int:
    ok = True

    def say(good: bool, label: str, detail: str = "") -> None:
        nonlocal ok
        ok = ok and good
        print(f"  {'ОК ' if good else 'ПРОВАЛ'} {label}" + (f" — {detail}" if detail and not good else ""))

    print("=== assert-stand-precondition-wiring.py --self-test ===")
    print("  --- звено 2: настоящее тело шага, исполнено (make подменён)")

    rc, out, genv = run_stand_body(marker=True, make_rc=2)
    say(rc != 0 and FLAG in genv and "УСЛОВИЕ НЕ СОЗДАНО" in out,
        "отметка ЕСТЬ → объявлено «условие не создано», флаг выставлен, шаг не зелёный",
        f"код {rc}, env={genv.strip()!r}")
    say("kacho#655" in out, "заголовок называет предмет — читателю есть куда пойти")

    # ЗАКОННЫЙ БЛИЗНЕЦ. Без него правка превратила бы ЛЮБОЙ срыв подъёма в
    # «условие не создано», то есть завела бы маску.
    rc, out, genv = run_stand_body(marker=False, make_rc=2)
    say(rc != 0 and FLAG not in genv and "УСЛОВИЕ НЕ СОЗДАНО" not in out,
        "отметки НЕТ → это НАШ отказ: флаг не выставлен, краснота остаётся краснотой",
        f"код {rc}, env={genv.strip()!r}")

    rc, out, genv = run_stand_body(marker=False, make_rc=0)
    say(rc == 0 and FLAG not in genv,
        "стенд поднялся → шаг зелёный, флага нет", f"код {rc}")

    # Здоровый стенд, но отметка от ПРОШЛОГО прогона осталась бы: шаг зелёный,
    # флага нет — до развилки дело не доходит вовсе. (Саму отметку снимает
    # владелец материализации; здесь проверяется, что успех её не читает.)
    rc, out, genv = run_stand_body(marker=True, make_rc=0)
    say(rc == 0 and FLAG not in genv,
        "стенд поднялся при лежащей отметке → флаг НЕ выставляется", f"код {rc}")

    print("  --- звено 3: инъекция в дерево проверки (обе стороны)")
    steps = load_steps()
    findings, seen = check(steps)
    say(not findings, f"дерево как есть — связно (шагов {seen['steps']}, "
        f"вердиктов {seen['verdict']}, из них с условием {seen['guarded']}, описей {seen['carrier']})",
        "; ".join(findings))

    # (а) снимаем условие у одного шага-вердикта → обязана быть находка С ИМЕНЕМ
    hurt = [dict(s) for s in steps]
    for s in hurt:
        if any(r in str(s.get("run") or "") for r in VERDICT_RUNNERS):
            s["if"] = "always()"
            victim = str(s.get("name"))
            break
    f2, _ = check(hurt)
    say(any(victim in x for x in f2), "снятое условие у шага-вердикта → находка с его именем")

    # (б) законный близнец: шаг описи БЕЗ условия — молчим (так и должно быть)
    say(not any(CARRIER_RUNNER in str(s.get("run") or "") and guarded(s) for s in steps),
        "шаг описи условия не несёт — причина доезжает до свода")

    # (в) условие НА шаге описи → находка: третья категория не доедет
    hurt = [dict(s) for s in steps]
    for s in hurt:
        if CARRIER_RUNNER in str(s.get("run") or ""):
            s["if"] = f"always() && env.{FLAG} != '1'"
            break
    f3, _ = check(hurt)
    say(any("до свода" in x for x in f3), "условие на шаге описи → находка")

    # (г) шаг стенда перестал читать отметку → находка
    hurt = [dict(s) for s in steps]
    for s in hurt:
        if "make dev-up" in str(s.get("run") or ""):
            s["run"] = "make dev-up\n"
            break
    f4, _ = check(hurt)
    say(len(f4) >= 3, "шаг стенда без чтения отметки → находки по каждому звену")

    print("самопроверка:", "PASS" if ok else "FAIL")
    return 0 if ok else 1


def main() -> int:
    if not WORKFLOW.is_file():
        print(f"FATAL: нет {WORKFLOW} — судить не по чему", file=sys.stderr)
        return 2
    if shutil.which("bash") is None:
        print("FATAL: нужен bash — тело шага проверяется исполнением", file=sys.stderr)
        return 2

    steps = load_steps()
    if not steps:
        print(f"FATAL: в работе '{JOB}' ноль шагов — «связно» значило бы "
              f"«ничего не прочитано»", file=sys.stderr)
        return 2

    findings, seen = check(steps)
    print(f"осмотрено: шагов работы '{JOB}' {seen['steps']}, из них выносящих вердикт о "
          f"пробах {seen['verdict']} (с условием {seen['guarded']}), несущих причину своду "
          f"{seen['carrier']}")
    if seen["verdict"] == 0 or seen["carrier"] == 0:
        for f in findings:
            print(f"  FATAL: {f}")
        return 2
    if findings:
        print("FAIL: «условие не создано» до вердикта НЕ доезжает:")
        for f in findings:
            print(f"  - {f}")
        return 1
    print("OK: чужая недоступность объявляется отдельной категорией, гейты проб при ней "
          "пропускаются, причина доносится до свода")
    return 0


if __name__ == "__main__":
    sys.exit(_self_test() if "--self-test" in sys.argv[1:] else main())
