#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Самопроверка предиката «первый доступ к СВОЕМУ свежему ресурсу обёрнут».

ПРЕДМЕТ. Kachō eventually-consistent: owner-tuple свежесозданного ресурса
материализуется вне мутации, поэтому ПЕРВОЕ обращение создателя к своему
ресурсу может кратко получить 403/404 (`testing.md` §e2e-инварианты). Норма
предписывает КЛИЕНТСКИЙ ограниченный ретрай (`retry_until_authorized`) — и
ТОЛЬКО на положительном первом доступе к своему; негативы, чужие аккаунты и
несуществующие id оборачивать запрещено (там ретрай маскировал бы реальный
отказ).

ПОЧЕМУ ГЕЙТ, А НЕ ВНИМАНИЕ. Обёртка ставилась вручную, поэтому пропуск
неотличим от решения. Замер по артефактам прогона CI 31002239590 (8 суит,
82 отчёта, 15648 утверждений): из 68 падений полосы видимости (403/404)
**42** пришлись на шаги БЕЗ обёртки вовсе, причём в одном и том же кейсе
соседние шаги той же формы обёрнуты — то есть это пропуск, а не замысел.
Гейт закрывает КЛАСС: предикат `_wrap_own_fresh_reads` в `gen.py` ставит
обёртку по свойству шага, а эта самопроверка доказывает, что предикат
(а) срабатывает на настоящем пропуске и (б) МОЛЧИТ на законном близнеце.

Запуск: python3 scripts/selftest_autowrap.py   (стенд и newman не нужны)
"""

from __future__ import annotations

import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import importlib.util  # noqa: E402

_spec = importlib.util.spec_from_file_location("gen_under_test", HERE / "gen.py")
gen = importlib.util.module_from_spec(_spec)
sys.argv = [sys.argv[0]]  # gen.py не должен разбирать наши аргументы
# Регистрация ДО exec_module: @dataclass резолвит типы через sys.modules[__module__],
# и без неё падает на разборе собственных аннотаций.
sys.modules["gen_under_test"] = gen
_spec.loader.exec_module(gen)

Step = gen.Step
Case = gen.Case

FAILURES: list[str] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"{'ok  ' if ok else 'FAIL'}  {name}" + (f"  — {detail}" if detail and not ok else ""))
    if not ok:
        FAILURES.append(name)


def wrapped(step) -> bool:
    return any("_authRetryCount" in ln for ln in step.test_script)


def steps_of(case: Case):
    return gen._wrap_own_fresh_reads(case.steps)


# ---------------------------------------------------------------------------
# 1. ИНЪЕКЦИЯ НАСТОЯЩИМ ВХОДОМ: шаг-положительный первый доступ к своему свежему
#    ресурсу, записанный БЕЗ обёртки, обязан быть обёрнут предикатом.
#    Форма взята с натуры — так выглядели упавшие `del-a1` / `list-used` / `lbs`.
# ---------------------------------------------------------------------------
injected = Case(
    id="SELFTEST-INJECT", title="own fresh read without wrapper", classes=["CRUD"], priority="P0",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/addresses",
             body={"projectId": "{{_suiteProjectId}}"},
             test_script=[*gen.assert_status(200),
                          *gen.save_from_response("j.metadata && j.metadata.addressId", "stAddrId")]),
        Step(name="del-own", method="DELETE", path="/vpc/v1/addresses/{{stAddrId}}",
             test_script=[*gen.assert_status(200)]),
    ],
)
out = steps_of(injected)
check("инъекция: необёрнутый положительный первый доступ к своему — обёрнут", wrapped(out[1]),
      "шаг остался без ограниченного ретрая — класс не закрыт")
check("инъекция: имя шага стало уникальным (self-retry резолвится в СЕБЯ)",
      out[1].name.startswith("del-own-rya"), out[1].name)
check("инъекция: собственные утверждения шага сохранены целиком",
      all(ln in out[1].test_script for ln in gen.assert_status(200)))

# ---------------------------------------------------------------------------
# 2. ЗАКОННЫЙ БЛИЗНЕЦ №1: НЕГАТИВ (ждёт отказ) — обёртка запрещена, предикат молчит.
#    Без этого контроля гейт ловил бы форму, а не существо.
# ---------------------------------------------------------------------------
neg = Case(
    id="SELFTEST-NEG", title="negative on own fresh resource", classes=["NEG"], priority="P0",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/subnets",
             test_script=[*gen.assert_status(200), *gen.save_from_response("j.metadata && j.metadata.subnetId", "stSubId")]),
        Step(name="add-overlapping", method="POST", path="/vpc/v1/subnets/{{stSubId}}:addCidrBlocks",
             test_script=[*gen.assert_status(400)]),
    ],
)
check("близнец-негатив: шаг, ждущий отказ, НЕ обёрнут", not wrapped(steps_of(neg)[1]))

# ---------------------------------------------------------------------------
# 3. ЗАКОННЫЙ БЛИЗНЕЦ №2: шаг с собственной петлёй (поллер операции) — не трогаем,
#    иначе две петли на одном шаге и сломанный резолв имени.
# ---------------------------------------------------------------------------
poll = Case(
    id="SELFTEST-POLL", title="operation poller keeps its own loop", classes=["CRUD"], priority="P0",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/networks",
             test_script=[*gen.assert_status(200), *gen.save_from_response("j.id", "opId")]),
        gen.poll_operation_until_done(),
    ],
)
check("близнец-поллер: шаг со своей петлёй не получает вторую", not wrapped(steps_of(poll)[1]))

# ---------------------------------------------------------------------------
# 4. ЗАКОННЫЙ БЛИЗНЕЦ №3: уже обёрнутый вручную шаг не оборачивается повторно.
# ---------------------------------------------------------------------------
manual = Case(
    id="SELFTEST-MANUAL", title="already wrapped by hand", classes=["CRUD"], priority="P0",
    steps=[
        Step(name="create", method="POST", path="/vpc/v1/gateways",
             test_script=[*gen.assert_status(200), *gen.save_from_response("j.metadata && j.metadata.gatewayId", "stGwId")]),
        gen.retry_until_authorized(Step(name="get", method="GET", path="/vpc/v1/gateways/{{stGwId}}",
                                        test_script=[*gen.assert_status(200)])),
    ],
)
got = steps_of(manual)[1]
check("близнец-ручной: повторной обёртки нет",
      sum(1 for ln in got.test_script if "_authRetryStarted" in ln and "!==" in ln) == 1)
check("близнец-ручной: имя не переименовано дважды", got.name.count("-rya") == 1, got.name)

# ---------------------------------------------------------------------------
# 5. ЗАКОННЫЙ БЛИЗНЕЦ №4: обращение к ЧУЖОМУ (не созданному в этом кейсе) id —
#    предикат его не знает и молчит. Так негативы про absent-id остаются строгими.
# ---------------------------------------------------------------------------
foreign = Case(
    id="SELFTEST-FOREIGN", title="unknown id is not our fresh resource", classes=["NEG"], priority="P0",
    steps=[
        Step(name="get-absent", method="GET", path="/vpc/v1/networks/{{netAbsentId}}",
             test_script=[*gen.assert_status(200)]),
    ],
)
check("близнец-чужой: id, не рождённый в этом кейсе, не оборачивается",
      not wrapped(steps_of(foreign)[0]))

# ---------------------------------------------------------------------------
# 6. ОБЪЁМ ОСМОТРЕННОГО: «ноль находок» обязано быть отличимо от «ноль прочитанного».
# ---------------------------------------------------------------------------
print()
print(f"осмотрено кейсов самопроверки: 5, шагов: "
      f"{sum(len(c.steps) for c in (injected, neg, poll, manual, foreign))}")

if FAILURES:
    print(f"\nSELFTEST FAIL: {len(FAILURES)} — " + "; ".join(FAILURES), file=sys.stderr)
    sys.exit(1)
print("SELFTEST OK")
