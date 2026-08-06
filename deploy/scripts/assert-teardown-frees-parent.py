#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Уборка кейса снимает ПОТОМКА прежде, чем удалять его РОДИТЕЛЯ.

Продукт держит ссылочную целостность на уровне БД (`data-integrity.md` ban #10):
родитель, на которого ещё ссылается строка-потомок, не удаляется — приходит
`FAILED_PRECONDITION` от `ON DELETE RESTRICT`. Это его объявленный контракт, а не
дефект. Значит уборка кейса, заведшая потомка, обязана снять ЕГО, иначе получает
законный отказ на удалении родителя.

ПОЧЕМУ ГЕЙТ ЗАВЕДЁН ТОЛЬКО СЕЙЧАС. Пока шаг удаления не нёс утверждения об исходе
операции, отказ был НЕВИДИМ: операция, завершившаяся ошибкой, тоже `done`, и опрос
на этом успокаивался. Класс проявился ровно тогда, когда `assert-delete-operation-
outcome.py` заставил цепочку называть исход, — и проявился СРАЗУ В ДВУХ доменах:
у балансировщика (уборка удаляла непустую группу целей, `d6f5707a`) и у прав
(уборка удаляла роль, на которую осталась отозванная выдача). Два экземпляра за
двое суток — это класс, а не совпадение, и первый из них чинился без гейта.

ЧТО ИМЕННО ДЕРЖИТ ССЫЛКУ У ПРАВ (частный случай, объясняющий предмет). Отзыв
выдачи — переход состояния, а НЕ удаление: строка остаётся с `status='REVOKED'`
как запись о бывшей выдаче (audit-retention), внешний ключ `ON DELETE RESTRICT`
считает её наравне с действующей. Освобождает ссылку только жёсткое удаление
выдачи. Поэтому «отозвал» и «убрал» — разные действия, и уборке нужно второе.

ЧТО ПРОВЕРЯЕТСЯ
---------------
Единица счёта — тройка (кейс, переменная-потомок, переменная-родитель).

  • ПОТОМОК — имя, в которое шаг-мутация кладёт идентификатор РЕСУРСА из
    метаданных операции (`j.metadata.<что-то>Id`).
  • РОДИТЕЛЬ — имя, подставленное в ТЕЛО того же шага (`{{родитель}}`): именно так
    потомок и объявляет свою ссылку.
  • НАХОДКА — кейс удаляет родителя (`DELETE …/{{родитель}}`) и НЕ удаляет потомка.

ПОЧЕМУ ТОЛЬКО `metadata.<X>Id`, А НЕ ЛЮБОЙ ЗАХВАТ `j.id`. Измерено на этом же
дереве: расширение предиката до `j.id` даёт 26 находок вместо одной, и 25 из них —
идентификаторы ОПЕРАЦИЙ (`…OpId`, `…Op`), а операция ресурсом не является и не
удаляется никогда. Такой гейт краснел бы на законном и был бы снят первым же, кто
на него наткнётся. Сужение — не осторожность, а условие того, чтобы гейт пережил
свою первую неделю.

ЧЕГО ГЕЙТ НЕ ВИДИТ, И ЭТО НАЗВАНО ЧЕСТНО. Потомок, у которого нет собственного
идентификатора (цель внутри группы целей у балансировщика: заводится действием
`:addTargets` на самом родителе), в тройку не складывается by construction —
захватывать нечего. Тот экземпляр класса держит утверждение об исходе на шаге
удаления (`assert-delete-operation-outcome.py`): уборка, забывшая слить группу,
краснеет там же, только на прогоне, а не статически.

ВТОРАЯ СЛЕПАЯ ЗОНА, И У НЕЁ ЕСТЬ СВОЙ ГЕЙТ. Потомок, которого завёл САМ ПРОДУКТ
вместе с родителем (сага `Account.Create` co-commit'ит проект `default`), в тройку
тоже не складывается — но по другой причине: он создаётся ТЕМ ЖЕ шагом, что и
родитель, поэтому подстановки родителя в тело шага нет, а сам кейс потомка не
называет. Его предикат — поле метаданных, и он живёт отдельно, в
`assert-cocreated-child-is-torn-down.py`. Два предиката об одном классе намеренно
разделены: у них разные источники пары «потомок → родитель», и сведение их в один
обход сделало бы каждый менее точным.

ПОЧЕМУ ЧИТАЮТСЯ СГЕНЕРИРОВАННЫЕ КОЛЛЕКЦИИ, А НЕ ИСХОДНИКИ КЕЙСОВ. Исполняется
коллекция; вспомогательные функции уборки у каждой суиты свои, и проверка по
исходникам видела бы только те формы, которые ей знакомы. Чинить при этом надо
ИСХОДНИК: сгенерированное затрётся следующим прогоном генератора.
"""
import json
import os
import re
import sys
import tempfile

# Захват идентификатора РЕСУРСА из метаданных операции. Форму задаёт
# `save_from_response` генератора; если она изменится, счётчик прочитанного
# упадёт в ноль и основной проход обязан упасть (см. проверку предпосылки).
CAPTURE = re.compile(
    r"const v = \(j\.metadata && j\.metadata\.(?P<field>[A-Za-z0-9_]+)\);\s*\n"
    r"\s*if \(v !== undefined[^\n]*\n?[^\n]*"
    r"pm\.environment\.set\(\s*'(?P<var>[A-Za-z0-9_]+)'", re.M)
VAR_IN_BODY = re.compile(r"\{\{([A-Za-z0-9_]+)\}\}")
DELETE_TAIL = re.compile(r"/\{\{([A-Za-z0-9_]+)\}\}\s*$")
MUTATION = {"POST", "PUT", "PATCH"}

# Имена, которые кейс не заводит и не убирает: адреса стенда и идентификатор
# прогона. Идентификатор операции сюда не нужен — он отсекается тем, что
# захватывается не из `metadata`.
AMBIENT = {"opId", "runId", "baseUrl", "internalBaseUrl", "externalBaseUrl"}


def _raw_url(req):
    u = req.get("url")
    return u.get("raw", "") if isinstance(u, dict) else (u or "")


def _test_code(item):
    return "\n".join(
        "\n".join(ev.get("script", {}).get("exec", []) or [])
        for ev in (item.get("event") or []) if ev.get("listen") == "test")


def _leaves(items):
    for it in items:
        if "item" in it:
            yield from _leaves(it["item"])
        else:
            yield it


def scan(root):
    """→ (находки, (коллекций, кейсов, шагов, потомков, удалений))."""
    files = []
    for dirpath, _dirs, names in os.walk(root):
        if os.path.basename(dirpath) != "collections":
            continue
        files += [os.path.join(dirpath, n) for n in sorted(names)
                  if n.endswith(".postman_collection.json")]
    files.sort()

    findings = []
    n_cases = n_steps = n_children = n_deletes = 0
    for path in files:
        try:
            coll = json.load(open(path, encoding="utf-8"))
        except (OSError, ValueError) as exc:
            print(f"НЕ ПРОЧИТАНО {path}: {exc}", file=sys.stderr)
            continue
        suite = path.split("/tests/newman/")[0].split(os.sep)[-1]
        cname = os.path.basename(path).replace(".postman_collection.json", "")
        for case in coll.get("item", []):
            if "item" not in case:
                continue
            n_cases += 1
            steps = list(_leaves(case["item"]))
            n_steps += len(steps)
            children = {}      # потомок -> [шаг, {родители}, поле]
            deleted = set()
            for st in steps:
                req = st.get("request", {}) or {}
                url, method = _raw_url(req), req.get("method", "")
                body = ((req.get("body") or {}).get("raw")) or ""
                if method == "DELETE":
                    m = DELETE_TAIL.search(url)
                    if m:
                        deleted.add(m.group(1))
                        n_deletes += 1
                    continue
                if method not in MUTATION:
                    continue
                parents = {v for v in VAR_IN_BODY.findall(body) if v not in AMBIENT}
                for m in CAPTURE.finditer(_test_code(st)):
                    var = m.group("var")
                    if var in AMBIENT:
                        continue
                    if var not in children:
                        n_children += 1
                    entry = children.setdefault(
                        var, [st.get("name", "?"), set(), m.group("field")])
                    entry[1] |= parents
            for var, (where, parents, field) in sorted(children.items()):
                if var in deleted:
                    continue
                for parent in sorted(parents):
                    if parent in deleted:
                        findings.append({
                            "suite": suite, "collection": cname,
                            "case": case.get("name", "?").split(" —")[0],
                            "child": var, "field": field,
                            "step": where, "parent": parent,
                        })
    return findings, (len(files), n_cases, n_steps, n_children, n_deletes)


# ─── самопроверка: гейт обязан краснеть на настоящем промахе и молчать на
#     законном близнеце той же формы ─────────────────────────────────────────
def _collection(case_name, steps):
    return {"info": {"name": "t", "schema": ""}, "item": [{"name": case_name, "item": steps}]}


def _step(name, method, path, body=None, capture=None):
    it = {"name": name,
          "request": {"method": method, "url": {"raw": "{{baseUrl}}" + path}}}
    if body is not None:
        it["request"]["body"] = {"mode": "raw", "raw": json.dumps(body)}
    if capture:
        field, var = capture
        it["event"] = [{"listen": "test", "script": {"exec": [
            "try {",
            "  const j = pm.response.json();",
            f"  const v = (j.metadata && j.metadata.{field});",
            "  if (v !== undefined && v !== null) "
            f"pm.environment.set('{var}', String(v));",
            "} catch (e) {}",
        ]}}]
    return it


def _write(tmp, name, coll):
    d = os.path.join(tmp, "services", "x", "tests", "newman", "collections")
    os.makedirs(d, exist_ok=True)
    with open(os.path.join(d, name + ".postman_collection.json"), "w",
              encoding="utf-8") as fh:
        json.dump(coll, fh)


def self_test() -> int:
    rc = 0
    print("=== самопроверка: инъекция настоящего промаха + законный близнец ===")

    # (а) НАСТОЯЩИЙ промах: потомок заведён, родитель удаляется, потомок — нет.
    with tempfile.TemporaryDirectory() as tmp:
        _write(tmp, "inj", _collection("CASE-LEAK", [
            _step("mk-parent", "POST", "/iam/v1/roles", {"name": "r"},
                  ("roleId", "pRole")),
            _step("mk-child", "POST", "/iam/v1/accessBindings",
                  {"roleId": "{{pRole}}"}, ("accessBindingId", "cBind")),
            _step("rm-parent", "DELETE", "/iam/v1/roles/{{pRole}}"),
        ]))
        found, (cols, _ca, _st, kids, _d) = scan(tmp)
        named = [f for f in found if f["child"] == "cBind" and f["parent"] == "pRole"]
        if cols == 1 and kids == 2 and named:
            print("  ОК  инъекция: гейт краснеет и НАЗЫВАЕТ координату "
                  f"({named[0]['case']} :: {named[0]['child']} → {named[0]['parent']})")
        else:
            print(f"  ПРОВАЛ инъекция не поймана: коллекций {cols}, потомков {kids}, "
                  f"находок {len(found)}")
            rc = 1

    # (б) ЗАКОННЫЙ БЛИЗНЕЦ той же формы: потомок снимается ПЕРЕД родителем.
    with tempfile.TemporaryDirectory() as tmp:
        _write(tmp, "twin", _collection("CASE-CLEAN", [
            _step("mk-parent", "POST", "/iam/v1/roles", {"name": "r"},
                  ("roleId", "pRole")),
            _step("mk-child", "POST", "/iam/v1/accessBindings",
                  {"roleId": "{{pRole}}"}, ("accessBindingId", "cBind")),
            _step("rm-child", "DELETE", "/iam/v1/accessBindings/{{cBind}}"),
            _step("rm-parent", "DELETE", "/iam/v1/roles/{{pRole}}"),
        ]))
        found, (cols, _ca, _st, kids, dels) = scan(tmp)
        if cols == 1 and kids == 2 and dels == 2 and not found:
            print("  ОК  законный близнец: та же форма, уборка полная — гейт молчит")
        else:
            print(f"  ПРОВАЛ близнец дал {len(found)} находок "
                  f"(потомков {kids}, удалений {dels})")
            rc = 1

    # (в) ОПЕРАЦИЯ — НЕ РЕСУРС: захват `j.id` конверта не делает кейс находкой.
    with tempfile.TemporaryDirectory() as tmp:
        coll = _collection("CASE-OP-ONLY", [
            _step("mk-parent", "POST", "/iam/v1/roles", {"name": "r"},
                  ("roleId", "pRole")),
            _step("grant", "POST", "/iam/v1/accessBindings", {"roleId": "{{pRole}}"}),
            _step("rm-parent", "DELETE", "/iam/v1/roles/{{pRole}}"),
        ])
        coll["item"][0]["item"][1]["event"] = [{"listen": "test", "script": {"exec": [
            "try {", "  const j = pm.response.json();", "  const v = (j.id);",
            "  if (v !== undefined && v !== null) pm.environment.set('gOpId', String(v));",
            "} catch (e) {}"]}}]
        _write(tmp, "op", coll)
        found, _c = scan(tmp)
        if not found:
            print("  ОК  идентификатор операции ресурсом не считается — гейт молчит")
        else:
            print(f"  ПРОВАЛ операция принята за ресурс: {found}")
            rc = 1

    # (г) ПРЕДПОСЫЛКА: пустое дерево обязано дать ноль ПРОЧИТАННОГО, чтобы
    #     «ноль находок» нельзя было спутать с «ноль осмотренного».
    with tempfile.TemporaryDirectory() as tmp:
        _found, (cols, _ca, _st, kids, _d) = scan(tmp)
        if (cols, kids) == (0, 0):
            print("  ОК  пустое дерево даёт ноль прочитанного — основной проход обязан падать")
        else:
            print(f"  ПРОВАЛ пустое дерево дало {cols} коллекций / {kids} потомков")
            rc = 1

    print()
    print("PASS: гейт «уборка снимает потомка прежде родителя»" if rc == 0
          else "FAIL: гейт «уборка снимает потомка прежде родителя»")
    return rc


def main() -> int:
    if "--self-test" in sys.argv[1:]:
        return self_test()

    root = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
    findings, (cols, cases, steps, kids, dels) = scan(root)

    print(f"=== уборка против ссылочной целостности: осмотрено коллекций {cols}, "
          f"кейсов {cases}, шагов {steps}; потомков-ресурсов {kids}, удалений по "
          f"имени {dels} ===")
    if cols == 0 or kids == 0:
        print("FAIL: обход не нашёл ни коллекций, ни потомков-ресурсов — предмет запрета")
        print("      потерян либо форма захвата в генераторе изменилась. Пустая находка")
        print("      на пустом обходе не является доказательством чего-либо.")
        return 1

    if findings:
        print(f"FAIL: {len(findings)} потомк(ов) остаются, пока кейс удаляет их родителя.")
        print("      Продукт откажет законно (ссылка ещё жива), уборка получит отказ, а")
        print("      родитель переживёт прогон: ресурс копится, вердикт краснеет не там.")
        for f in findings:
            print(f"    {f['suite']}/{f['collection']} :: {f['case']}")
            print(f"        потомок {{{{{f['child']}}}}} (metadata.{f['field']}, "
                  f"шаг '{f['step']}') не снят; кейс удаляет родителя "
                  f"{{{{{f['parent']}}}}}")
        print()
        print("Чинить в ИСХОДНИКЕ кейса (*/tests/newman/cases/*.py): уборка обязана снять")
        print("КАЖДОГО потомка, которого кейс завёл, прежде чем удалять родителя. Правка")
        print("сгенерированной коллекции будет затёрта следующим прогоном генератора.")
        return 1

    print(f"PASS: ни один из {kids} заведённых потомков не остаётся при удалении родителя")
    return 0


if __name__ == "__main__":
    sys.exit(main())
