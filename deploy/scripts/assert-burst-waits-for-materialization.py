#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Гейт: залп из скрипта, адресующий СВЕЖИЙ объект, ждёт своё окно видимости.

ПРЕДМЕТ. Ресурс становится durable раньше, чем материализуется его owner-tuple,
поэтому первое обращение создателя к своему ресурсу может кратко получить отказ
на крае. Для обычных шагов это закрыто предикатом автообёртки в генераторах
(`_wrap_own_fresh_reads`): он читает URL шага и ставит ограниченный клиентский
ретрай. Но КОНКУРЕНТНЫЙ кейс шлёт свои запросы не шагом, а из тест-скрипта
(`pm.sendRequest`), и сам шаг при этом ходит на служебный путь. Предикат такой
URL не видит ПО ПОСТРОЕНИЮ — то есть у автообёртки здесь слепая зона, и она
именно слепая, а не пустая.

ЧТО ЭТО СТОИЛО. В прогоне 31027579106 залп двух параллельных правок правил
группы безопасности целиком получил отказ на крае: ни одной операции не
создалось, и кейс, написанный ради гонки, покраснел с `ok=0 conflict=0 ops=[]`.
То есть проба на конкуренцию не проверила НИЧЕГО, и по её падению это было
неотличимо от сломанного OCC.

ПРАВИЛО. Если залп интерполирует в свой URL переменную, РОЖДЁННУЮ РАНЕЕ В ЭТОМ
ЖЕ КЕЙСЕ, то до него в том же кейсе обязан стоять шаг, который (а) адресует ту
же переменную и (б) несёт ограниченный ретрай. Залп по коллекционному пути
(scope — заранее существующий проект) под правило не подпадает: свежего объекта
в его адресе нет.

Гейт читает ПОРОЖДЁННЫЕ коллекции, а не исходники кейсов: там и находится то,
что реально исполнится.

Запуск:  python3 deploy/scripts/assert-burst-waits-for-materialization.py
Само:    python3 deploy/scripts/assert-burst-waits-for-materialization.py --self-test
"""

from __future__ import annotations

import json
import re
import subprocess
import sys
from pathlib import Path
from typing import Iterator

ROOT = Path(__file__).resolve().parents[2]

# URL залпа собирается шаблонной строкой JS, поэтому переменная приезжает как
# `${pm.environment.get('sgId')}`. Публикация — `pm.environment.set('sgId', …)`.
# Адрес залпа — ТОЛЬКО значение ключа `url:` до ближайшей запятой. Брать шире
# нельзя: в том же вызове рядом лежит тело запроса, и свежий id в ТЕЛЕ
# (`{networkId: …}` при POST на коллекцию) адресом не является — окна
# материализации там нет, потому что scope запроса даёт заранее существующий
# проект. Ровно на этом первая редакция предиката и попалась своему близнецу.
_URL_EXPR = re.compile(r"url:\s*([^,\n]*)")
_GET_VAR = re.compile(r"pm\.environment\.get\(\s*['\"]([A-Za-z_][A-Za-z0-9_]*)['\"]")

# Полоса операций под правило НЕ подпадает, и это не послабление, а другой
# способ авторизации: право читать операцию край берёт из САМОЙ СТРОКИ операции
# (`principal_type`/`principal_id`, `checkOperationOwnership` в
# `gateway/internal/opsproxy/proxy.go`), а не из отношения, которое надо
# материализовать. Материализуемого окна там нет по построению, поэтому ждать
# нечего. Предпосылка проверяется гейтом (см. `_premise_holds`): если полоса
# операций когда-нибудь поедет на пообъектную материализацию, исключение станет
# ложным — и гейт обязан это заметить сам, а не пережить свой предмет.
_OPERATIONS_LANE = "/operations/"
_PREMISE_FILE = ROOT / "gateway" / "internal" / "opsproxy" / "proxy.go"
_PREMISE_MARK = "checkOperationOwnership"


def _premise_holds() -> tuple[bool, str]:
    if not _PREMISE_FILE.exists():
        return False, f"нет файла {_PREMISE_FILE.relative_to(ROOT)}"
    src = _PREMISE_FILE.read_text()
    if _PREMISE_MARK not in src or "GetPrincipalId()" not in src:
        return False, (f"{_PREMISE_FILE.relative_to(ROOT)} больше не решает доступ к операции "
                       f"по principal'у из строки — исключение полосы операций потеряло основание")
    return True, ""
_SET_VAR = re.compile(r"pm\.(?:environment|collectionVariables|globals)\.set\(\s*['\"]([A-Za-z_][A-Za-z0-9_]*)['\"]")
_PATH_VAR = re.compile(r"\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}")
_RETRY_MARK = "_authRetryCount"


def _test_src(item: dict) -> str:
    out = []
    for ev in item.get("event", []) or []:
        if ev.get("listen") == "test":
            out.extend(ev.get("script", {}).get("exec", []) or [])
    return "\n".join(out)


def _item_url(item: dict) -> str:
    raw = ((item.get("request") or {}).get("url") or {})
    if isinstance(raw, str):
        return raw
    return raw.get("raw", "")


def _cases(collection: dict) -> Iterator[tuple[str, list[dict]]]:
    """Кейс = папка первого уровня, несущая шаги (без вложенных папок)."""
    def walk(items, name):
        leaves = [it for it in items if "item" not in it]
        if leaves:
            yield name, leaves
        for it in items:
            if "item" in it:
                yield from walk(it["item"], it.get("name", name))
    yield from walk(collection.get("item", []), collection.get("info", {}).get("name", "?"))


def audit(collection: dict, coll_name: str) -> tuple[list[str], int, int]:
    findings: list[str] = []
    steps_seen = 0
    bursts_seen = 0
    for case_name, steps in _cases(collection):
        published: set[str] = set()
        # Переменная → был ли до текущего места шаг, который её адресует И ждёт.
        awaited: set[str] = set()
        for st in steps:
            steps_seen += 1
            src = _test_src(st)
            burst_vars: set[str] = set()
            if "pm.sendRequest" in src:
                for expr in _URL_EXPR.findall(src):
                    if _OPERATIONS_LANE in expr:
                        continue
                    burst_vars |= set(_GET_VAR.findall(expr))
            burst_vars &= published
            if burst_vars:
                bursts_seen += 1
                for v in sorted(burst_vars - awaited):
                    findings.append(
                        f"{coll_name} :: {case_name} :: {st.get('name')} — залп адресует "
                        f"свежий '{v}', но до него нет шага, который ждёт окно видимости "
                        f"этого объекта (ограниченный ретрай)"
                    )
            # Шаг, адресующий переменную своим URL и несущий ретрай, закрывает окно.
            if _RETRY_MARK in src:
                awaited |= set(_PATH_VAR.findall(_item_url(st)))
            published |= set(_SET_VAR.findall(src))
    return findings, steps_seen, bursts_seen


def tracked_collections() -> list[Path]:
    """Состав берётся из индекса git, а не с диска: иначе чужой мусор и
    .gitignore могут молча изменить объём осмотренного."""
    out = subprocess.run(
        ["git", "ls-files", "*/tests/newman/collections/*.json",
         "*/*/tests/newman/collections/*.json"],
        cwd=ROOT, capture_output=True, text=True, check=True).stdout.split()
    return [ROOT / p for p in out]


def main() -> int:
    files = tracked_collections()
    if not files:
        print("ГЕЙТ ПРОВАЛЕН: не найдено ни одной порождённой коллекции — "
              "предмета для проверки нет, «ноль находок» здесь ничего не значит",
              file=sys.stderr)
        return 1
    ok, why = _premise_holds()
    if not ok:
        print("ГЕЙТ ПРОВАЛЕН: предпосылка исключения больше не верна — " + why, file=sys.stderr)
        return 1
    findings: list[str] = []
    steps = bursts = 0
    for f in files:
        d = json.loads(f.read_text())
        fnd, s, b = audit(d, f.name)
        findings += fnd
        steps += s
        bursts += b
    print(f"осмотрено: коллекций {len(files)}, шагов {steps}, "
          f"залпов по свежему объекту {bursts}")
    if bursts == 0:
        print("ГЕЙТ ПРОВАЛЕН: залпов, адресующих свежий объект, не найдено ВОВСЕ — "
              "предикат перестал видеть свой предмет, и «ноль находок» стало "
              "неотличимо от «ноль прочитанного»", file=sys.stderr)
        return 1
    if findings:
        print(f"\nНАХОДОК: {len(findings)}", file=sys.stderr)
        for x in findings:
            print("  " + x, file=sys.stderr)
        return 1
    print("гейт зелёный: каждый залп по свежему объекту ждёт своё окно")
    return 0


# --------------------------------------------------------------------------
# Самопроверка: гейт, не доказавший, что умеет краснеть, доказательством не
# является. Инъекция настоящим входом + законные близнецы в обе стороны.
# --------------------------------------------------------------------------
def _coll(case_name: str, steps: list[dict]) -> dict:
    return {"info": {"name": "selftest"}, "item": [{"name": case_name, "item": steps}]}


def _step(name: str, url: str, script: list[str]) -> dict:
    return {"name": name,
            "request": {"url": {"raw": url}},
            "event": [{"listen": "test", "script": {"exec": script}}]}


def self_test() -> int:
    ok = True

    create = _step("create", "{{baseUrl}}/vpc/v1/securityGroups",
                   ["pm.environment.set('sgId', 'sgr1');"])
    burst = _step("burst", "{{baseUrl}}/healthz", [
        "pm.sendRequest({url: base + `/vpc/v1/securityGroups/${pm.environment.get('sgId')}/rules`,"
        " method: 'PATCH'}, cb);"])
    awaitstep = _step("await", "{{baseUrl}}/vpc/v1/securityGroups/{{sgId}}",
                      ["const _arc = pm.environment.get('_authRetryCount');",
                       "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));"])

    # 1. ИНЪЕКЦИЯ: залп по свежему объекту без ожидания — находка.
    f, _, b = audit(_coll("INJECT", [create, burst]), "selftest")
    if not (len(f) == 1 and b == 1):
        ok = False
        print(f"FAIL инъекция: ожидалась 1 находка на 1 залпе, получено {len(f)} на {b}",
              file=sys.stderr)
    else:
        print("ok   инъекция: залп по свежему объекту без ожидания — находка")

    # 2. ЗАКОННЫЙ БЛИЗНЕЦ: тот же залп, но окно пережидается — гейт молчит.
    f, _, b = audit(_coll("LEGAL", [create, awaitstep, burst]), "selftest")
    if f or b != 1:
        ok = False
        print(f"FAIL близнец-законный: гейт не смолчал ({f})", file=sys.stderr)
    else:
        print("ok   близнец-законный: залп после ожидания окна — молчит")

    # 3. ЗАКОННЫЙ БЛИЗНЕЦ: залп по КОЛЛЕКЦИОННОМУ пути (свежего объекта в адресе
    #    нет — scope это заранее существующий проект) — не предмет правила.
    coll_burst = _step("burst-collection", "{{baseUrl}}/healthz", [
        "pm.sendRequest({url: base + '/vpc/v1/subnets', method: 'POST',"
        " body: {networkId: pm.environment.get('netId')}}, cb);"])
    f, _, b = audit(_coll("COLLECTION", [
        _step("create-net", "{{baseUrl}}/vpc/v1/networks", ["pm.environment.set('netId', 'net1');"]),
        coll_burst]), "selftest")
    if f:
        ok = False
        print(f"FAIL близнец-коллекция: залп по коллекционному пути помечен ({f})", file=sys.stderr)
    else:
        print("ok   близнец-коллекция: свежий id в ТЕЛЕ, а не в адресе — не предмет")

    # 4. ЗАКОННЫЙ БЛИЗНЕЦ: переменная пришла извне кейса (посев) — окна нет.
    f, _, _ = audit(_coll("SEEDED", [burst]), "selftest")
    if f:
        ok = False
        print(f"FAIL близнец-посев: переменная не рождена в кейсе, а помечена ({f})", file=sys.stderr)
    else:
        print("ok   близнец-посев: переменная не из этого кейса — не предмет")

    # 5. ЗАКОННЫЙ БЛИЗНЕЦ: ожидание есть, но БЕЗ ретрая — это не ожидание.
    bare = _step("plain-get", "{{baseUrl}}/vpc/v1/securityGroups/{{sgId}}",
                 ["pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));"])
    f, _, _ = audit(_coll("BARE", [create, bare, burst]), "selftest")
    if len(f) != 1:
        ok = False
        print(f"FAIL близнец-без-ретрая: чтение без ретрая засчитано за ожидание ({f})",
              file=sys.stderr)
    else:
        print("ok   близнец-без-ретрая: чтение без ограниченного ретрая окном не считается")

    print("\nSELFTEST " + ("OK" if ok else "FAIL"))
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(self_test() if "--self-test" in sys.argv else main())
