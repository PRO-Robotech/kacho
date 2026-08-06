#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""У ОБЪЯВЛЕННОЙ ЗАКОННОЙ ПОЛОСЫ ОТКАЗА обязан быть читатель, способный её прочитать.

ПРЕДМЕТ. Мутации Kachō отвечают Operation (ban #9), поэтому у отказа две законные
полосы: синхронный валидатор отвергает ДО чеканки Operation (`400`), либо отвергает
воркер (`200` и Operation, завершившаяся с ошибкой). Шаг, объявивший обе полосы
законными (`oneOf([200, 400])`), обязан быть прочитан на ОБЕИХ. На синхронной полосе
Operation НЕ СУЩЕСТВУЕТ, и захват её идентификатора не срабатывает: `save_from_response`
СБРАСЫВАЕТ имя в пустое (иначе опрос уехал бы на чужую операцию — это отдельный класс,
его держит `assert-delete-operation-outcome.py`). Значит следующий шаг, опрашивающий
`GET /operations/{{<имя>}}`, получает ПУСТОЙ адресный сегмент — и обязан это заметить.

ЧТО ПРОИСХОДИТ, ЕСЛИ НЕ ЗАМЕЧАЕТ (наблюдалось живьём, прогон 31061894576, коллекция
`vpc/security-group`). Запрос уходит на `/operations/` без идентификатора; край
отвечает `403` («нет пути авторизации к ресурсу») — потому что резолвить нечего.
Автоматическая обёртка ожидания видимости считает `403` признаком неготовности прав и
повторяет ТОТ ЖЕ запрос до исчерпания бюджета: 27 запросов подряд, ни один из которых
не может пройти НИКОГДА. Кейс падает сообщением «expected undefined to deeply equal
true» — то есть НЕ НАЗЫВАЕТ ни свою причину, ни даже свой предмет, и читается как
дефект авторизации, которым не является. Это ровно тот мягкий проход, что не отличает
настройку от сбоя (`security.md` §8): повтор осмыслен на «ещё не материализовалось» и
бессмыслен на «идентификатора нет вовсе».

ПОЧЕМУ ЭТО ОТДЕЛЬНАЯ МЕРКА, А НЕ ЧАСТЬ `tools/mixedoutcomeaudit`. Та судит по СЛОВАРЮ:
читает обещание метки кейса и сравнивает с принятыми исходами. Здесь словарь не нужен
вовсе — вопрос СТРУКТУРНЫЙ и решается порядком шагов: объявлена ли полоса, на которой
имя пусто, и читает ли её кто-нибудь, не спросив. Две мерки ловят один класс с разных
сторон, и ни одна не покрывает другую: словарная промолчит, если заголовок кейса не
называет направления; структурная промолчит, если полоса отказа не объявлена.

ЕДИНИЦА СЧЁТА — ШАГ СГЕНЕРИРОВАННОЙ КОЛЛЕКЦИИ (`git ls-files
'*.postman_collection.json'`), а не строка исходника кейса: пары «мутация → опрос»
складываются помощниками генератора, и в исходнике порядок шагов не всегда виден
статически. Коллекции отслеживаются git и регенерируются побайтово, поэтому вердикт
воспроизводим.

ЧИТАЕТСЯ ИСПОЛНЯЕМАЯ ЧАСТЬ: JS-комментарии снимаются до поиска. И защита, и её
объяснение написаны одними словами — поиск по сырому тексту принял бы объяснение за
защиту (`testing.md` §«Гейт на класс», п. 4).

ЧТО СЧИТАЕТСЯ ЧИТАТЕЛЕМ. Достаточно ЛЮБОГО из двух, потому что оба делают исход
пустого имени определённым:
  • ранний выход по пустому имени ДО первого утверждения
    (`if (!pm.environment.get('<имя>')) { … return; }` — форма
    `poll_operation_until_done`);
  • пропуск запроса в пред-скрипте (`pm.execution.skipRequest()` под тем же условием —
    форма рукописных поллеров iam).
Шаг, который вообще ничего не утверждает, читателем быть не обязан: он и не может
соврать о результате.

Запуск:
    python3 deploy/scripts/assert-refusal-lane-has-a-reader.py
    python3 deploy/scripts/assert-refusal-lane-has-a-reader.py --self-test
Код возврата: 0 — у каждой объявленной полосы отказа есть читатель; 1 — находка;
2 — обход пуст (предмет потерян либо обход сломан).
"""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys

RE_VAR_FULL = re.compile(r"\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}")
RE_CODE_EQL = re.compile(r"pm\.response\.code[^;]{0,160}?\.to\.eql\((\d{3})\)")
RE_CODE_ONEOF = re.compile(r"pm\.response\.code[^;]{0,160}?\.to\.be\.oneOf\(\[([0-9,\s]+)\]\)")
# Сброс имени в ПУСТОЕ — признак того, что автор объявил ветку «Operation не
# отчеканена» законной. Снятие имени (`unset`) сюда НЕ входит намеренно: снятое имя
# ловит страж неразрешённых подстановок, и запрос до края не доходит вовсе.
RE_RESET_EMPTY = re.compile(
    r"pm\.(?:environment|collectionVariables|globals)\.set\(\s*['\"]([A-Za-z_]\w*)['\"]\s*,\s*''\s*\)")
RE_ANY_SET = re.compile(
    r"pm\.(?:environment|collectionVariables|globals)\.set\(\s*['\"]([A-Za-z_]\w*)['\"]")


def strip_js_comments(s: str) -> str:
    """Снять JS-комментарии, сохранив позиции (заменой на пробелы)."""
    out = list(s)
    i, n, in_str = 0, len(s), None
    while i < n:
        c = s[i]
        if in_str:
            if c == "\\":
                i += 2
                continue
            if c == in_str:
                in_str = None
            i += 1
            continue
        if c in "'\"`":
            in_str = c
            i += 1
            continue
        if c == "/" and i + 1 < n and s[i + 1] == "/":
            j = s.find("\n", i)
            j = n if j < 0 else j
            for k in range(i, j):
                out[k] = " "
            i = j
            continue
        if c == "/" and i + 1 < n and s[i + 1] == "*":
            j = s.find("*/", i)
            j = n if j < 0 else j + 2
            for k in range(i, j):
                if out[k] != "\n":
                    out[k] = " "
            i = j
            continue
        i += 1
    return "".join(out)


def accepted_codes(code: str) -> set:
    """HTTP-исходы, ОБЪЯВЛЕННЫЕ шагом приемлемыми."""
    acc = {int(m.group(1)) for m in RE_CODE_EQL.finditer(code)}
    for m in RE_CODE_ONEOF.finditer(code):
        acc |= {int(p) for p in m.group(1).replace(" ", "").split(",") if p}
    return {c for c in acc if 100 <= c <= 599}


def script_of(item: dict, listen: str) -> str:
    for ev in item.get("event", []) or []:
        if ev.get("listen") == listen:
            return "\n".join((ev.get("script") or {}).get("exec", []) or [])
    return ""


def guards_empty(item: dict, var: str) -> bool:
    """Читает ли шаг пустое имя ДО того, как что-либо утверждать."""
    pre = strip_js_comments(script_of(item, "prerequest"))
    test = strip_js_comments(script_of(item, "test"))
    cond = (r"!\s*pm\.(?:environment|collectionVariables|globals)\.get\(\s*"
            r"['\"]" + re.escape(var) + r"['\"]\s*\)")
    # Пред-скрипт: пропуск запроса под тем же условием.
    if re.search(cond, pre) and "skipRequest" in pre:
        return True
    if "pm.test(" not in test:
        # Шаг ничего не утверждает — соврать о результате он не может.
        return True
    head = test[:test.index("pm.test(")]
    return bool(re.search(cond, head) and "return" in head)


def polled_var(item: dict) -> str:
    """Имя, которым адресуется опрос операции; пусто — шаг не опрос."""
    url = (item.get("request") or {}).get("url") or {}
    path = url.get("path") or []
    if len(path) >= 2 and path[0] == "operations":
        m = RE_VAR_FULL.fullmatch(path[1] or "")
        if m:
            return m.group(1)
    return ""


def walk(items, trail=""):
    for it in items or []:
        if "item" in it:
            yield from walk(it["item"], (trail + "/" if trail else "") + (it.get("name") or ""))
        else:
            yield trail, it


def collection_files(root: str) -> list:
    try:
        out = subprocess.run(["git", "ls-files", "*.postman_collection.json"],
                             cwd=root, capture_output=True, text=True, check=True).stdout
    except (OSError, subprocess.CalledProcessError):
        return []
    return sorted(os.path.join(root, p) for p in out.split())


def scan_collections(docs):
    """docs — [(имя файла, разобранная коллекция)]. Вернуть (находки, перепись)."""
    findings = []
    n_col = n_step = n_poll = n_lane = 0
    for name, doc in docs:
        n_col += 1
        # Ключ — (кейс, имя): область жизни объявленной полосы — ОДИН кейс.
        # Между кейсами имя переиспользуется, и переносить полосу через границу
        # значило бы приписывать кейсу чужое объявление.
        lane = {}
        for case, it in walk(doc.get("item", [])):
            n_step += 1
            test = strip_js_comments(script_of(it, "test"))
            step = it.get("name") or ""

            var = polled_var(it)
            if var:
                n_poll += 1
                origin = lane.get((case, var))
                if origin and not guards_empty(it, var):
                    findings.append({
                        "file": name, "case": case, "poll": step,
                        "var": var, "origin": origin[0], "codes": origin[1],
                    })

            # Учёт объявленных полос ПОСЛЕ проверки: шаг не читает сам себя.
            reset = {m.group(1) for m in RE_RESET_EMPTY.finditer(test)}
            acc = accepted_codes(test)
            refusal_lane = bool(acc) and any(c >= 400 for c in acc)
            for v in RE_ANY_SET.findall(test):
                if v in reset and refusal_lane:
                    lane[(case, v)] = (step, sorted(acc))
                    n_lane += 1
                else:
                    lane.pop((case, v), None)
    return findings, (n_col, n_step, n_poll, n_lane)


# ---------------------------------------------------------------------------
# Самопроверка: инъекция настоящей формы дерева + законный близнец
# ---------------------------------------------------------------------------
def _step(name, path, test_lines, pre_lines=None):
    ev = [{"listen": "test", "script": {"exec": test_lines}}]
    if pre_lines:
        ev.insert(0, {"listen": "prerequest", "script": {"exec": pre_lines}})
    return {"name": name,
            "request": {"method": "GET", "url": {"path": path.strip("/").split("/")}},
            "event": ev}


def _coll(case, steps):
    return {"item": [{"name": case, "item": steps}]}


REFUSE = ["pm.environment.set('opId', '');",
          "pm.test('refused', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
          "try { const j = pm.response.json(); if (j.id) pm.environment.set('opId', String(j.id)); } catch (e) {}"]
GUARD = ["if (!pm.environment.get('opId')) { pm.environment.unset('_pollCount'); return; }",
         "pm.test('poll status 200', () => pm.expect(pm.response.code).to.eql(200));"]
NOGUARD = ["const j = pm.response.json();",
           "pm.test('completed', () => pm.expect(j.done).to.eql(true));"]


def self_test() -> int:
    rc = 0

    def check(nm, ok, detail=""):
        nonlocal rc
        print(f"  {'ОК ' if ok else 'ПРОВАЛ'} {nm}{'' if ok else ' — ' + detail}")
        if not ok:
            rc = 1

    print("=== полоса отказа без читателя: инъекции ===")

    # (1) КРАСНОЕ — дословная форма дерева: объявлены обе полосы, следующий шаг
    #     опрашивает операцию, не спросив, есть ли она.
    f, _ = scan_collections([("x", _coll("C-DEL-NEG", [
        _step("del-default-sg", "/vpc/v1/securityGroups/{{sgId}}", REFUSE),
        _step("check-result", "/operations/{{opId}}", NOGUARD)]))])
    check("опрос без проверки пустого имени после объявленной полосы отказа — находка",
          len(f) == 1 and f[0]["poll"] == "check-result", f"находок {len(f)}")

    # (2) МОЛЧАНИЕ на законном близнеце ТОЙ ЖЕ формы: та же пара, но опрос читает
    #     пустое имя первым стейтментом (форма `poll_operation_until_done`).
    f, _ = scan_collections([("x", _coll("C-DEL-NEG", [
        _step("del-default-sg", "/vpc/v1/securityGroups/{{sgId}}", REFUSE),
        _step("poll-op-1", "/operations/{{opId}}", GUARD)]))])
    check("тот же порядок, но с ранним выходом по пустому имени — не находка",
          not f, f"находок {len(f)}")

    # (3) МОЛЧАНИЕ: пропуск запроса в пред-скрипте (форма рукописных поллеров iam).
    f, _ = scan_collections([("x", _coll("C-DEL-NEG", [
        _step("delete-system", "/iam/v1/roles/{{roleId}}", REFUSE),
        _step("poll-system-delete", "/operations/{{opId}}", NOGUARD,
              pre_lines=["if (!pm.environment.get('opId')) { pm.execution.skipRequest(); }"])]))])
    check("пропуск запроса в пред-скрипте — тоже читатель",
          not f, f"находок {len(f)}")

    # (4) МОЛЧАНИЕ: полоса отказа НЕ объявлена (шаг требует ровно 200), поэтому имя
    #     пустым быть не может, и требовать проверки не за что.
    strict = ["pm.environment.set('opId', '');",
              "pm.test('created', () => pm.expect(pm.response.code).to.eql(200));",
              "try { const j = pm.response.json(); if (j.id) pm.environment.set('opId', String(j.id)); } catch (e) {}"]
    f, _ = scan_collections([("x", _coll("C-CR-OK", [
        _step("cr-net", "/vpc/v1/networks", strict),
        _step("check-result", "/operations/{{opId}}", NOGUARD)]))])
    check("без объявленной полосы отказа опрос не обязан ничего проверять",
          not f, f"находок {len(f)}")

    # (5) МОЛЧАНИЕ: полоса объявлена в ДРУГОМ кейсе. Переносить её через границу
    #     значило бы приписать кейсу чужое объявление.
    f, _ = scan_collections([("x", {"item": [
        {"name": "C-ONE", "item": [_step("del-x", "/a/{{id}}", REFUSE)]},
        {"name": "C-TWO", "item": [_step("check-result", "/operations/{{opId}}", NOGUARD)]}]})])
    check("объявление полосы не переносится между кейсами",
          not f, f"находок {len(f)}")

    # (6) МОЛЧАНИЕ: шаг опроса вообще ничего не утверждает — соврать о результате
    #     он не может, поэтому читателем быть не обязан.
    f, _ = scan_collections([("x", _coll("C-DEL-NEG", [
        _step("del-x", "/a/{{id}}", REFUSE),
        _step("touch-op", "/operations/{{opId}}", ["pm.environment.set('seen', '1');"])]))])
    check("опрос без единого утверждения — не находка", not f, f"находок {len(f)}")

    # (7) ПРЕДПОСЫЛКА ОБХОДА: защита, написанная в КОММЕНТАРИИ, защитой не является.
    commented = ["// if (!pm.environment.get('opId')) { return; }",
                 "const j = pm.response.json();",
                 "pm.test('completed', () => pm.expect(j.done).to.eql(true));"]
    f, _ = scan_collections([("x", _coll("C-DEL-NEG", [
        _step("del-x", "/a/{{id}}", REFUSE),
        _step("check-result", "/operations/{{opId}}", commented)]))])
    check("проверка, оставшаяся комментарием, за защиту не засчитывается",
          len(f) == 1, f"находок {len(f)}")

    print("\n" + ("PASS: полоса отказа без читателя" if rc == 0 else
                  "FAIL: полоса отказа без читателя"))
    return rc


def main() -> int:
    if "--self-test" in sys.argv[1:]:
        return self_test()

    root = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
    docs = []
    for p in collection_files(root):
        try:
            with open(p, encoding="utf-8") as fh:
                docs.append((os.path.relpath(p, root), json.load(fh)))
        except (OSError, ValueError):
            continue
    findings, (n_col, n_step, n_poll, n_lane) = scan_collections(docs)

    print(f"=== полоса отказа и её читатель: осмотрено коллекций {n_col}, шагов {n_step}, "
          f"опросов операции {n_poll}, объявленных полос отказа {n_lane} ===")
    if n_col == 0 or n_lane == 0:
        print("FAIL: обход не нашёл ни коллекций, ни объявленных полос отказа — предмет")
        print("      запрета потерян либо обход сломан. Пустая находка на пустом обходе")
        print("      доказательством не является.")
        return 2

    if findings:
        print(f"FAIL: {len(findings)} опрос(ов) операции читают идентификатор, который на")
        print("      ОБЪЯВЛЕННОЙ ЗАКОННОЙ полосе отказа пуст. На этой полосе Operation не")
        print("      чеканится: запрос уходит на `/operations/` без сегмента, край отвечает")
        print("      403, обёртка ожидания видимости повторяет его до исчерпания бюджета, а")
        print("      кейс падает сообщением, которое не называет ни свою причину, ни предмет.")
        for f in sorted(findings, key=lambda x: (x["file"], x["case"], x["poll"])):
            print(f'    {f["file"]}')
            print(f'      кейс {f["case"]}')
            print(f'      полосу объявил шаг «{f["origin"]}» (исходы {f["codes"]}), '
                  f'читает без проверки «{f["poll"]}» по имени {f["var"]}')
        print()
        print("Исходы (любой): убрать опрос, если синхронная полоса — единственная (Operation")
        print("не существует, полоса прибивается кодом и текстом на самом шаге мутации);")
        print("поставить парный `poll_operation_until_done(must_fail=True)`, который сам")
        print("читает пустое имя; либо, для рукописного поллера, добавить ранний выход по")
        print("пустому имени ДО первого утверждения. Чинить в ГЕНЕРАТОРЕ")
        print("(*/tests/newman/scripts/gen.py и cases/*.py) — правка сгенерированной")
        print("коллекции будет затёрта следующим прогоном.")
        return 1

    print(f"PASS: все {n_lane} объявленных полос отказа прочитаны — ни один опрос операции "
          f"не уезжает на пустой идентификатор ({n_poll} опросов осмотрено)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
