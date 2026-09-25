#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Есть ли на стенде поставщик личности — по посадке цепочки, объявленной процессами.

ПРЕДМЕТ. Прогонщики сквозных проб открывают пробросы к службам поставщика
личности (его публичная и административная поверхности, экран входа). На цепочке
`own` поставщика нет вовсе: его подчарты выключены в базе зонта для всех стендов
(#2735). `kubectl port-forward svc/<нет такого>` не встаёт и завершается, после
чего блок живости прогонщика совершенно правильно объявляет прогон
недействительным — и суиты не исполняются ни одной (#2841: 0 коллекций из 58).

ПОЧЕМУ ПОСАДКА, А НЕ НАЛИЧИЕ СЛУЖБЫ. Предикат «служба есть ⇒ пробросить» снимал
бы проброс и там, где поставщик ОБЯЗАН быть, но не поднялся: недостающий
транспорт превратился бы из «прогон недействителен» в тихий пропуск, а кейсы,
которые его набирают, — в вердикт о продукте на предмете, которого харнесс не
создал. Посадка отвечает на другой вопрос — НУЖЕН ли поставщик этой цепочке, — и
на него отвечают сами процессы: строка «boot security posture» службы доступа и
края несёт `identity_provider`. Это то же чтение, которым живой гейт
административного перехода судит отсутствие поставщика
(deploy/scripts/assert-admin-hop-transport.sh, kacho#2816).

РЕШЕНИЕ. Поставщика НЕТ ровно тогда, когда ОБЕ половины объявили `own`. Во всех
остальных случаях он нужен, и пробросы обязательны на прежних условиях:
  * половина назвала `external` — ей поставщик нужен;
  * половина не прочитана (нет строки посадки, лог не отдан, значение не
    распознано) — молчание процесса за объявление отсутствия не идёт.
Так ошибка чтения стоит прогону «проброс не встал», а не тихого пропуска.

ВЫВОД — одна строка `<present|absent>|<основание>`, код 0 при любом решении.
Код 1 — дефект вызова (аргументы). Основание называет обе половины: «не нужно»
обязано быть отличимо от «не прочитано».

    python3 deploy/scripts/identity-provider-landing.py --namespace kacho
    python3 deploy/scripts/identity-provider-landing.py --self-test
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys

PRESENT = "present"
ABSENT = "absent"
LANDING_OWN = "own"
LANDING_EXTERNAL = "external"
POSTURE_MSG = "boot security posture"

# Половины цепочки: объект Deployment и как его назвать в основании. Имена те же,
# что читает гейт административного перехода.
HALVES = (("kaname", "служба"), ("api-gateway", "край"))

# Строка посадки пишется в композиционном корне, то есть в первых килобайтах лога.
# Предел тот же, что у гейта боевой посадки (assert-production-posture.sh).
BOOT_LOG_HEAD_BYTES = int(os.environ.get("BOOT_LOG_HEAD_BYTES", "262144"))

RC_OK = 0
RC_CALLER_DEFECT = 1


def posture_value(log_text: str) -> str | None:
    """`identity_provider` из ПОСЛЕДНЕЙ строки посадки в тексте лога. None — нет."""
    value = None
    for raw in log_text.splitlines():
        raw = raw.strip()
        if not raw.startswith("{"):
            continue
        try:
            doc = json.loads(raw)
        except json.JSONDecodeError:
            continue
        if not isinstance(doc, dict) or doc.get("msg") != POSTURE_MSG:
            continue
        got = doc.get("identity_provider")
        value = got if isinstance(got, str) and got else None
    return value


def decide(halves: list[tuple[str, str | None]]) -> tuple[str, str]:
    """(решение, основание) по посадке половин: [(имя половины, значение|None)]."""
    shown = ", ".join(f"{name} {value if value else 'не прочитано'}" for name, value in halves)
    if halves and all(value == LANDING_OWN for _, value in halves):
        return ABSENT, f"посадка own у обеих половин ({shown}): поставщика на стенде нет по объявлению"
    unread = [name for name, value in halves if value is None]
    if unread:
        return PRESENT, (f"посадка не прочитана у половины «{', '.join(unread)}» ({shown}): "
                         "отсутствие поставщика НЕ установлено, пробросы обязательны")
    strange = [name for name, value in halves if value not in (LANDING_OWN, LANDING_EXTERNAL)]
    if strange:
        return PRESENT, (f"посадка не распознана у половины «{', '.join(strange)}» ({shown}): "
                         "отсутствие поставщика НЕ установлено, пробросы обязательны")
    return PRESENT, f"посадка называет поставщика ({shown}): пробросы обязательны"


def read_half(ns: str, deployment: str, timeout: int) -> str | None:
    cmd = ["kubectl", "-n", ns, "logs", f"deploy/{deployment}", "--all-containers=true",
           f"--limit-bytes={BOOT_LOG_HEAD_BYTES}"]
    try:
        out = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired) as exc:
        print(f"[посадка] лог deploy/{deployment} не получен: {exc}", file=sys.stderr)
        return None
    if out.returncode != 0:
        first = (out.stderr or "").strip().splitlines()
        print(f"[посадка] лог deploy/{deployment} не получен: "
              f"{first[0] if first else 'kubectl rc=' + str(out.returncode)}", file=sys.stderr)
        return None
    return posture_value(out.stdout)


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--namespace", default=os.environ.get("SETUP_NS", "kacho"))
    ap.add_argument("--timeout", type=int, default=20)
    ap.add_argument("--self-test", action="store_true")
    try:
        args = ap.parse_args(argv)
    except SystemExit:
        return RC_CALLER_DEFECT
    if args.self_test:
        return self_test()
    halves = [(name, read_half(args.namespace, dep, args.timeout)) for dep, name in HALVES]
    verdict, why = decide(halves)
    print(f"{verdict}|{why}")
    return RC_OK


# ───────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА. Гоняются НАСТОЯЩИЕ `posture_value` и `decide`; у каждой инъекции
# законный близнец, отличающийся от неё ОДНИМ фактом.
# ───────────────────────────────────────────────────────────────────────────

def _line(value, msg=POSTURE_MSG):
    doc = {"level": "info", "msg": msg, "auth_mode": "production"}
    if value is not None:
        doc["identity_provider"] = value
    return json.dumps(doc)


def self_test() -> int:
    fails = []

    def check(name, ok, detail=""):
        print(f"  {'ok  ' if ok else 'FAIL'} {name}" + ("" if ok else f": {detail}"))
        if not ok:
            fails.append(name)

    print("чтение строки посадки")
    check("строка посадки own → own", posture_value(_line("own")) == "own")
    check("строка посадки external → external", posture_value(_line("external")) == "external")
    check("чужая строка с тем же полем не читается",
          posture_value(_line("external", msg="something else")) is None)
    check("строки посадки нет → не прочитано", posture_value("plain text\n{broken") is None)
    check("поле в строке посадки отсутствует → не прочитано", posture_value(_line(None)) is None)
    two = _line("external") + "\nnoise\n" + _line("own")
    check("из двух строк посадки берётся последняя", posture_value(two) == "own",
          f"получено {posture_value(two)!r}")

    print("решение")
    cases = [
        ("обе половины own → поставщика нет", [("служба", "own"), ("край", "own")], ABSENT),
        ("край не прочитан → пробросы обязательны", [("служба", "own"), ("край", None)], PRESENT),
        ("служба не прочитана → пробросы обязательны", [("служба", None), ("край", "own")], PRESENT),
        ("край external → пробросы обязательны", [("служба", "own"), ("край", "external")], PRESENT),
        ("обе external → пробросы обязательны", [("служба", "external"), ("край", "external")], PRESENT),
        ("значение не распознано → пробросы обязательны", [("служба", "own"), ("край", "n/a")], PRESENT),
        ("половин ноль → отсутствие не установлено", [], PRESENT),
    ]
    for name, halves, want in cases:
        got, why = decide(halves)
        check(name, got == want, f"ждали {want}, получили {got} ({why})")
        for half, value in halves:
            check(f"  основание называет половину «{half}»", half in why, why)

    print(f"самопроверка: {len(fails)} провалено")
    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())
