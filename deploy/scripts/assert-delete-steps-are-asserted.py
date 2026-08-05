#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Шаг удаления обязан НЕСТИ УТВЕРЖДЕНИЕ — иначе он не может упасть.

ЧТО ИЗМЕРЕНО (перепись по дереву, `git ls-files '*.postman_collection.json'`)
---------------------------------------------------------------------------
82 коллекции, 2502 кейса, 8233 шага, из них 1359 с методом DELETE. У 457 из них
в скрипте проверки НЕ БЫЛО НИ ОДНОГО утверждения: ни `pm.test`, ни голого
`pm.expect`, ни `pm.response.to.*`. Такой шаг зеленеет при ЛЮБОМ ответе — 200,
403, 404, 500 читаются им одинаково.

Тихим этот класс не остаётся: у асинхронного удаления шаг захватывает `opId`
из тела ответа, а следующий шаг опрашивает операцию по этому имени. Отказ тела
не несёт — захват не срабатывает, `opId` сохраняет ЗНАЧЕНИЕ ПРЕДЫДУЩЕЙ операции
(как правило уже `done`), и опрос подтверждает чужой, давно завершённый успех.
Кейс отчитывается зелёным по операции, которую он не запускал.

Следствия, которые из-за этого не видны: ресурс остаётся жить (фикстура течёт,
ограниченный пул деградирует, списочные контракты плывут), а сам отказ невидим.

ПОЧЕМУ УТВЕРЖДЕНИЕ ЗДЕСЬ ОДНОЗНАЧНО, А НЕ «ЛИБО УСПЕХ, ЛИБО ОТКАЗ»
------------------------------------------------------------------
Перепись действующего лица по этим 457 шагам: 434 идут под предъявителем
коллекции, 23 — под администратором аккаунта своего же кейса, и НИ ОДИН не
идёт под субъектом, которому отказ полагается по замыслу. То есть все они —
удаление СВОЕГО ресурса тем, кому это разрешено, и единственный законный исход
у них один. Отрицательные кейсы удаления (отказ — предмет кейса) утверждение
уже несут и в этот набор не попадают.

Отдельно: у асинхронного удаления отказ ПРЕДМЕТА («тип машины ещё используется»)
приезжает не кодом HTTP, а ошибкой операции — HTTP при этом 200. Поэтому
утверждение о коде ответа и утверждение об исходе операции не конкурируют:
первое проверяет, что запрос вообще принят, второе — что он сделал.

ЧТО ПРОВЕРЯЕТСЯ
---------------
Каждый шаг сгенерированной коллекции с методом DELETE обязан нести в скрипте
проверки хотя бы одно утверждение, СПОСОБНОЕ УПАСТЬ: `pm.test(`, `pm.expect(`
или `pm.response.to.`. Форма не навязывается — навязывается наличие.

ЧИТАЕТСЯ ИСПОЛНЯЕМАЯ ЧАСТЬ, А НЕ ТЕКСТ. Комментарии (`//` и `/* */`) снимаются
до поиска: обёртка повторного обращения приносит в каждый такой шаг несколько
строк объяснений, и поиск по сырому тексту принял бы объяснение защиты за саму
защиту. Ровно этот класс `testing.md` §«Гейт на класс» называет пунктом 4.

ПРЕДПОСЫЛКА ПРОВЕРЯЕТСЯ. Запрет держится на том, что в дереве есть коллекции и
в них есть шаги DELETE. Ноль коллекций или ноль шагов DELETE — не «чисто», а
потерянный предмет либо сломанный обход: гейт падает и говорит об этом. Объём
осмотренного печатается по той же причине — «ноль находок» обязано быть
отличимо от «ноль прочитанного».

ПОЧИНКА — В ГЕНЕРАТОРЕ. Коллекции регенерируются побайтово из
`*/tests/newman/scripts/gen.py`; правка сгенерированного файла будет затёрта
следующим прогоном генератора. Утверждение по умолчанию ставит `step_to_postman`
каждого генератора.

Запуск:
    python3 deploy/scripts/assert-delete-steps-are-asserted.py
    python3 deploy/scripts/assert-delete-steps-are-asserted.py --self-test
Код возврата: 0 — каждый шаг удаления может упасть; 1 — находка.
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile

# Три формы утверждения postman, каждая СПОСОБНА уронить шаг:
#   pm.test('...', () => pm.expect(...))   — именованное утверждение;
#   pm.expect(...)                          — голое, бросает и валит скрипт;
#   pm.response.to.have.status(200)         — форма chai-обёртки postman.
# Перечень намеренно закрытый: «что-нибудь похожее на проверку» принимало бы за
# утверждение любой захват значения, а захват не падает никогда.
ASSERT_FORMS = ("pm.test(", "pm.expect(", "pm.response.to.")


def strip_js_comments(src: str) -> str:
    """Снять `//`-хвосты и `/* */`-блоки, сохранив длину строк по номерам.

    Простой посимвольный разбор с учётом строковых литералов: `//` внутри
    строки (например в URL) комментарием не является, и срезать его значило бы
    рубить код. Регулярным выражением это не делается — оно не различает
    литерал и код, а именно за это различение гейт здесь и отвечает.
    """
    out = []
    i, n = 0, len(src)
    quote = None
    while i < n:
        ch = src[i]
        nxt = src[i + 1] if i + 1 < n else ""
        if quote:
            out.append(ch)
            if ch == "\\" and i + 1 < n:
                out.append(nxt)
                i += 2
                continue
            if ch == quote:
                quote = None
            i += 1
            continue
        if ch in ("'", '"', "`"):
            quote = ch
            out.append(ch)
            i += 1
            continue
        if ch == "/" and nxt == "/":
            while i < n and src[i] != "\n":
                i += 1
            continue
        if ch == "/" and nxt == "*":
            i += 2
            while i < n and not (src[i] == "*" and i + 1 < n and src[i + 1] == "/"):
                if src[i] == "\n":
                    out.append("\n")
                i += 1
            i += 2
            continue
        out.append(ch)
        i += 1
    return "".join(out)


def test_script(item: dict) -> str:
    for ev in item.get("event") or []:
        if ev.get("listen") == "test":
            return "\n".join(ev.get("script", {}).get("exec") or [])
    return ""


def carries_assertion(item: dict) -> bool:
    code = strip_js_comments(test_script(item))
    return any(form in code for form in ASSERT_FORMS)


def collection_files(root: str) -> list[str]:
    """Отслеживаемые коллекции. Авторитет — версионный контроль: то же множество,
    что увидит CI на свежем checkout'е. В синтетическом дереве самопроверки git'а
    нет — там обход файловой системы."""
    git = subprocess.run(["git", "-C", root, "ls-files", "-z", "*.postman_collection.json"],
                         capture_output=True, text=True)
    if git.returncode == 0 and git.stdout.strip("\0"):
        return sorted(n for n in git.stdout.split("\0") if n)
    names = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d != ".git"]
        for fn in filenames:
            if fn.endswith(".postman_collection.json"):
                names.append(os.path.relpath(os.path.join(dirpath, fn), root))
    return sorted(names)


def walk(items, trail):
    """Листья коллекции — элементы, несущие `request`. Папки вложены произвольно,
    поэтому обход рекурсивный, а координата собирается из имён по пути."""
    for it in items or []:
        name = it.get("name", "<без имени>")
        if "request" in it:
            yield it, trail + [name]
        if "item" in it:
            yield from walk(it["item"], trail + [name])


def scan(root: str):
    """→ (находки, коллекций, шагов, шагов DELETE)."""
    findings = []
    n_col = n_step = n_del = 0
    for rel in collection_files(root):
        try:
            with open(os.path.join(root, rel), encoding="utf-8") as fh:
                doc = json.load(fh)
        except (OSError, ValueError) as exc:
            findings.append(f"{rel}: коллекция не читается ({exc}) — шаги не осмотрены")
            continue
        n_col += 1
        for item, trail in walk(doc.get("item"), []):
            n_step += 1
            if (item.get("request") or {}).get("method") != "DELETE":
                continue
            n_del += 1
            if not carries_assertion(item):
                findings.append(f"{rel} :: " + " :: ".join(trail))
    return findings, n_col, n_step, n_del


# ─────────────────────────── самопроверка ───────────────────────────────────
def _collection(name: str, steps: list[dict]) -> dict:
    return {"info": {"name": name}, "item": [{"name": "CASE-1", "item": steps}]}


def _step(name: str, method: str, exec_lines: list[str]) -> dict:
    return {
        "name": name,
        "request": {"method": method, "url": {"raw": "{{baseUrl}}/x/v1/things/{{id}}"}},
        "event": [{"listen": "test", "script": {"type": "text/javascript", "exec": exec_lines}}],
    }


def self_test() -> int:
    print("=== шаг удаления обязан нести утверждение: инъекции в синтетическое дерево ===")
    rc = 0
    with tempfile.TemporaryDirectory() as tmp:
        os.makedirs(os.path.join(tmp, "svc", "collections"), exist_ok=True)

        # ─ ДЕФЕКТ: шаг удаления, скрипт которого только ЗАХВАТЫВАЕТ значение.
        # Ровно та форма, что стояла в дереве: захват не падает никогда.
        defect = _step("cleanup-thing", "DELETE", [
            "try {",
            "  const j = pm.response.json();",
            "  if (j.id) pm.environment.set('opId', String(j.id));",
            "} catch (e) {}",
        ])
        # ─ ЗАКОННЫЙ БЛИЗНЕЦ ТОЙ ЖЕ ФОРМЫ: удаление, чьё утверждение записано
        # ДРУГИМ способом (chai-обёртка), а не `pm.test`. Гейт обязан молчать —
        # он требует наличия утверждения, а не полюбившейся ему записи.
        twin = _step("del-thing-chai", "DELETE", [
            "pm.response.to.have.status(200);",
            "pm.environment.set('opId', pm.response.json().id);",
        ])
        # ─ ВТОРОЙ ЗАКОННЫЙ БЛИЗНЕЦ: голый `pm.expect` без обёртки `pm.test`.
        twin2 = _step("del-thing-bare", "DELETE", [
            "pm.expect(pm.response.code, pm.response.text()).to.eql(200);",
        ])
        # ─ ДЕФЕКТ, ЗАМАСКИРОВАННЫЙ ОБЪЯСНЕНИЕМ: единственный `pm.test(` в шаге
        # стоит в КОММЕНТАРИИ, объясняющем защиту. Поиск по сырому тексту счёл бы
        # объяснение защитой и промолчал.
        commented = _step("cleanup-commented", "DELETE", [
            "// здесь должно стоять pm.test('status 200', () => pm.expect(...));",
            "/* и ещё pm.expect(pm.response.code).to.eql(200); — тоже объяснение */",
            "pm.environment.set('opId', '1');",
        ])
        # ─ НЕ ПРЕДМЕТ ГЕЙТА: шаг другого метода без утверждений. Обязан молчать —
        # иначе гейт ловит «шаг без утверждения» вообще, а это другой предмет и
        # другой разговор.
        other = _step("seed-thing", "POST", ["pm.environment.set('id', '1');"])
        # ─ ГРАНИЦА РАЗБОРА: `//` внутри строкового литерала не комментарий.
        # Срезав его, читатель отрубил бы настоящее утверждение следом.
        in_string = _step("del-thing-url", "DELETE", [
            "const base = 'https://api.kacho.local//v1';",
            "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
        ])

        path = os.path.join(tmp, "svc", "collections", "probe.postman_collection.json")
        with open(path, "w", encoding="utf-8") as fh:
            json.dump(_collection("probe", [defect, twin, twin2, commented, other, in_string]), fh)

        findings, n_col, n_step, n_del = scan(tmp)
        got = {f.split(" :: ")[-1] for f in findings}

        for want, why in (("cleanup-thing", "захват значения принят за утверждение"),
                          ("cleanup-commented", "объяснение защиты принято за защиту")):
            if want in got:
                print(f"  ОК  дефект найден: {want}")
            else:
                print(f"  ПРОВАЛ дефект НЕ найден: {want} — {why}")
                rc = 1

        for quiet, why in (("del-thing-chai", "утверждение записано chai-обёрткой"),
                           ("del-thing-bare", "утверждение записано голым pm.expect"),
                           ("del-thing-url", "`//` внутри строкового литерала срезан как комментарий"),
                           ("seed-thing", "шаг другого метода — не предмет гейта")):
            if quiet in got:
                print(f"  ПРОВАЛ ложное срабатывание: {quiet} — {why}")
                rc = 1
            else:
                print(f"  ОК  законная форма пропущена: {quiet}")

        # КООРДИНАТА. Гейт, который говорит «есть находки», но не говорит где,
        # не чинится — его снимают. Проверяем, что путь ведёт к файлу, кейсу и шагу.
        coord = next((f for f in findings if f.endswith("cleanup-thing")), "")
        if coord.count(" :: ") >= 2 and coord.startswith("svc/collections/"):
            print(f"  ОК  находка названа координатой: {coord}")
        else:
            print(f"  ПРОВАЛ находка без координаты: {coord!r}")
            rc = 1

        if (n_col, n_del) == (1, 5):
            print(f"  ОК  объём осмотренного считается: коллекций {n_col}, шагов {n_step}, DELETE {n_del}")
        else:
            print(f"  ПРОВАЛ объём осмотренного неверен: коллекций {n_col}, шагов {n_step}, DELETE {n_del}"
                  f" (ожидалось 1 и 5)")
            rc = 1

        # ПРЕДПОСЫЛКА: пустое дерево обязано быть ОТКАЗОМ, а не «чисто».
        with tempfile.TemporaryDirectory() as empty:
            _, e_col, _, e_del = scan(empty)
            if (e_col, e_del) == (0, 0):
                print("  ОК  пустое дерево даёт ноль ПРОЧИТАННОГО — основной проход обязан на этом падать")
            else:
                print(f"  ПРОВАЛ пустое дерево дало {e_col} коллекций / {e_del} DELETE")
                rc = 1

    print()
    print("PASS: гейт «шаг удаления обязан нести утверждение»" if rc == 0
          else "FAIL: гейт «шаг удаления обязан нести утверждение»")
    return rc


def main() -> int:
    if "--self-test" in sys.argv[1:]:
        return self_test()

    root = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
    findings, n_col, n_step, n_del = scan(root)

    print(f"=== шаги удаления: осмотрено коллекций {n_col}, шагов {n_step}, "
          f"из них DELETE {n_del} ===")
    if n_col == 0 or n_del == 0:
        print("FAIL: обход не нашёл ни коллекций, ни шагов удаления — предмет запрета")
        print("      потерян либо обход сломан. Пустая находка на пустом обходе не")
        print("      является доказательством чего-либо.")
        return 1

    if findings:
        print(f"FAIL: {len(findings)} шаг(ов) удаления не несут НИ ОДНОГО утверждения —")
        print("      такой шаг зеленеет при любом ответе, включая отказ.")
        for f in findings:
            print(f"    {f}")
        print()
        print("Чинить в ГЕНЕРАТОРЕ (*/tests/newman/scripts/gen.py, step_to_postman) —")
        print("правка сгенерированной коллекции будет затёрта следующим прогоном.")
        return 1

    print(f"PASS: все {n_del} шагов удаления несут утверждение и способны упасть")
    return 0


if __name__ == "__main__":
    sys.exit(main())
