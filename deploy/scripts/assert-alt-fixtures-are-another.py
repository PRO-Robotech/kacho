#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Фикстура, названная «другой», обязана быть ДРУГОЙ.

ЧТО ИЗМЕРЕНО (боевой прогон 2026-07-30)
---------------------------------------
Кейс `LST-UPD-STATE-DEFAULT-TG-REGION-MISMATCH` переводит группу целей из
ДРУГОГО региона и ждёт отказа по когерентности размещения. Он получил 200 —
и это выглядело как принятый межрегиональный перевод, то есть как нарушение
целостности в продукте.

Продукт был ни при чём. `existingRegionAltId` держал ЗНАЧЕНИЕ ПЕРВИЧНОГО
региона, поэтому «альтернативная» группа целей создавалась в том же регионе, а
перевод внутри одного региона законно принимается. Проверка отказа при этом
существовала, была провязана и исполнялась на каждом прогоне — и не могла
сработать никогда, потому что у её входа не было производителя.

Класс, а не случай: такое совпадение нашлось в 9 объявлениях окружения, и ни
один посев второго региона не создавал. Отрицательный кейс, чьё условие никем
не создаётся, тихо проверяет положительный путь.

ЧТО ПРОВЕРЯЕТСЯ
---------------
1. В каждом файле окружения newman пара `<X>AltId` / `<X>Id` (и `<X>Alt` / `<X>`)
   обязана нести РАЗНЫЕ непустые значения. Пустое значение — заготовка, не
   утверждение, и пропускается.
2. Значение, объявленное «другим», обязано иметь ПРОИЗВОДИТЕЛЯ: посев
   `tests/authz-fixtures/prodseed_matrix.py` обязан создавать ровно тот регион,
   который он же выдаёт как `existingRegionAltId`.
3. Ни один литерал словаря во всём дереве не объявляет один строковый ключ
   дважды: побеждает последнее объявление, всё предыдущее теряется молча.
4. ПОБЕЖДАЮЩЕЕ значение `existingRegionAltId` в посеве отличается от
   `existingRegionId` — проверка по тому, что посев реально выдаёт, а не по
   тому, что где-то в нём написано правильное.

ПОЧЕМУ ДОБАВЛЕНЫ 3 И 4 (замер боевой посадки 2026-07-31). Пункты 1-2 держались,
а кейс снова получал 200. Посев объявлял `existingRegionAltId` ДВАЖДЫ подряд —
сперва вторым регионом, следом первичным; уезжал первичный. Внесено СЛИЯНИЕМ,
разрешившим конфликт сохранением обеих сторон: конфликта нет, обе строки
валидны, дифф их не различает, а пункт 2 находил производителя у КАЖДОГО из двух
объявленных значений и молчал. Файлы окружения при этом были безупречны — их
посев всё равно перезаписывает своими значениями на каждом прогоне, поэтому
проверять надо ИСХОД, а не объявление.

ПРЕДПОСЫЛКА ПРОВЕРЯЕТСЯ, И ОБЕ. Запрет 1-2 держится на том, что пары в дереве
есть; запрет 3-4 — на том, что словари читаются. Ноль найденных пар и ноль
прочитанных ключей — не «чисто», а потерянный предмет либо сломанный обход; гейт
падает и говорит об этом. Перепись осмотренного печатается по той же причине.

Запуск:
    python3 deploy/scripts/assert-alt-fixtures-are-another.py
    python3 deploy/scripts/assert-alt-fixtures-are-another.py --self-test
Код возврата: 0 — все «другие» действительно другие и посеяны; 1 — находка.
"""
from __future__ import annotations

import argparse
import ast
import json
import os
import re
import subprocess
import sys
import tempfile

# `existingRegionAltId` → `existingRegionId`; `_suiteRegionAlt` → `_suiteRegion`.
_ALT = re.compile(r"^(?P<stem>.+?)Alt(?P<tail>Id)?$")

# Посев: создание региона админским RPC geo и выдача значения в матрицу фикстур.
# Значением может быть литерал ЛИБО имя модульной постоянной — второе и есть
# нормальная форма, когда посев и выдача обязаны совпадать по построению, поэтому
# имена разрешаются, а неразрешимое имя — находка, а не молчаливый пропуск.
_SEEDS_REGION = re.compile(
    r"""["']/geo/v1/internal/regions["'][^)]*?["']id["']\s*:\s*(?P<val>["'][\w-]+["']|[A-Za-z_]\w*)""",
    re.S)
_EMITS_ALT = re.compile(
    r"""["']existingRegionAltId["']\s*:\s*(?P<val>["'][\w-]+["']|[A-Za-z_]\w*)""")
# NAME = "literal"  /  NAME = os.environ.get("X", "literal")
_CONST = re.compile(r"""^(?P<name>[A-Z_][A-Z0-9_]*)\s*=\s*"""
                    r"""(?:os\.environ\.get\(\s*["'][^"']+["']\s*,\s*)?["'](?P<val>[\w-]+)["']""",
                    re.M)


def _resolve(val: str, consts: dict) -> str | None:
    """Литерал → он сам; имя постоянной → её значение; иначе None (неразрешимо)."""
    val = val.strip()
    if val[:1] in ("'", '"'):
        return val.strip("'\"")
    return consts.get(val)


def env_files(root: str) -> list[str]:
    git = subprocess.run(["git", "-C", root, "ls-files", "-z"], capture_output=True, text=True)
    if git.returncode == 0:
        names = [n for n in git.stdout.split("\0") if n]
    else:
        names = []
        for dirpath, dirnames, filenames in os.walk(root):
            dirnames[:] = [d for d in dirnames if d != ".git"]
            for fn in filenames:
                names.append(os.path.relpath(os.path.join(dirpath, fn), root))
    return sorted(n for n in names
                  if "/environments/" in n.replace(os.sep, "/") and n.endswith(".json"))


def collapsed_pairs(root: str) -> tuple[list[str], int, int]:
    """(находки, пар осмотрено, файлов осмотрено)."""
    findings, pairs, files = [], 0, 0
    for rel in env_files(root):
        try:
            with open(os.path.join(root, rel), encoding="utf-8") as fh:
                doc = json.load(fh)
        except (OSError, ValueError):
            continue
        files += 1
        vals = {v.get("key"): v.get("value", "") for v in (doc.get("values") or [])}
        for key, val in sorted(vals.items()):
            m = _ALT.match(key or "")
            if not m:
                continue
            base = m.group("stem") + (m.group("tail") or "")
            if base not in vals:
                continue
            other = vals[base]
            if not val or not other:
                continue  # заготовка, а не утверждение
            pairs += 1
            if val == other:
                findings.append(f"{rel}: {key}={val!r} — то же самое, что {base}")
    return findings, pairs, files


def alt_region_has_a_producer(root: str) -> list[str]:
    """Посев обязан СОЗДАВАТЬ тот регион, который выдаёт как альтернативный."""
    seeder = os.path.join(root, "tests", "authz-fixtures", "prodseed_matrix.py")
    if not os.path.isfile(seeder):
        return []
    with open(seeder, encoding="utf-8") as fh:
        src = fh.read()
    consts = {m.group("name"): m.group("val") for m in _CONST.finditer(src)}
    rel = "tests/authz-fixtures/prodseed_matrix.py"
    seeded = set()
    for m in _SEEDS_REGION.finditer(src):
        r = _resolve(m.group("val"), consts)
        if r:
            seeded.add(r)
    out = []
    for m in _EMITS_ALT.finditer(src):
        raw = m.group("val")
        e = _resolve(raw, consts)
        if e is None:
            out.append(f"{rel}: значение existingRegionAltId задано как {raw!r} и не "
                       f"разрешается статически — производителя не проверить, а "
                       f"непроверяемое условие ничем не отличается от отсутствующего")
            continue
        if e not in seeded:
            out.append(f"{rel}: выдаёт existingRegionAltId={e!r}, но региона {e!r} не "
                       f"создаёт — у отрицательных кейсов по региону нет производителя "
                       f"условия")
    return out


def python_files(root: str) -> list[str]:
    """ВСЕ питоновские файлы дерева, а не только посевы фикстур.

    Затенённый ключ — свойство литерала словаря, а не каталога, в котором он
    лежит: ровно так же молча теряются значения в генераторах коллекций и в
    объявлениях окружения. Перепись по дереву (126 файлов, 2530 словарей,
    5805 строковых ключей) стоит доли секунды, поэтому сужать область незачем —
    а «починили там, где нашли» оставило бы класс живым во всех остальных.
    """
    git = subprocess.run(["git", "-C", root, "ls-files", "-z", "*.py"],
                         capture_output=True, text=True)
    if git.returncode == 0 and git.stdout:
        return sorted(n for n in git.stdout.split("\0") if n)
    names = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in (".git", "node_modules", "vendor")]
        for fn in filenames:
            if fn.endswith(".py"):
                names.append(os.path.relpath(os.path.join(dirpath, fn), root))
    return sorted(names)


def seeder_files(root: str) -> list[str]:
    """Посевы фикстур — только они выдают пару «другой/первичный» регион."""
    return [n for n in python_files(root)
            if n.replace(os.sep, "/").startswith("tests/authz-fixtures/")]


def _parse(root: str, rel: str):
    try:
        with open(os.path.join(root, rel), encoding="utf-8") as fh:
            return ast.parse(fh.read(), filename=rel)
    except (OSError, SyntaxError):
        return None


def shadowed_fixture_keys(root: str) -> tuple[list[str], int, int, int]:
    """Ключ, объявленный в ОДНОМ словаре дважды, тихо теряет первое значение.

    Разбор идёт по дереву разбора, а не по тексту: два присваивания одного ключа
    в литерале словаря — синтаксически безупречны, поэтому ни компилятор, ни
    линтер, ни обзор диффа их не отличают от двух разных ключей. Побеждает
    ПОСЛЕДНИЙ; всё, что стояло раньше, — заявление, которого посев не делает.

    Найдено на дереве 2026-07-31: `existingRegionAltId` объявлен дважды подряд —
    сперва вторым регионом, следом первичным. Значение уехало первичное, поэтому
    «другой регион» перестал быть другим, и отрицательная проба когерентности
    размещения снова проверяла положительный путь. Внесено СЛИЯНИЕМ, которое
    разрешило конфликт, оставив обе стороны: конфликта нет, обе строки валидны,
    правка предыдущего коммита отменена молча.

    Возвращает (находки, файлов, словарей, ключей) — перепись печатается, потому
    что «ноль находок» обязано отличаться от «ноль прочитанного».
    """
    findings, files, dicts, keys = [], 0, 0, 0
    for rel in python_files(root):
        tree = _parse(root, rel)
        if tree is None:
            continue
        files += 1
        for node in ast.walk(tree):
            if not isinstance(node, ast.Dict):
                continue
            dicts += 1
            seen: dict[str, int] = {}
            for k in node.keys:
                if not (isinstance(k, ast.Constant) and isinstance(k.value, str)):
                    continue  # `**expr` (k is None) и вычисляемые ключи — не наш предмет
                keys += 1
                if k.value in seen:
                    findings.append(
                        f"{rel}:{k.lineno}: ключ {k.value!r} объявлен в этом словаре "
                        f"повторно (первое объявление — строка {seen[k.value]}); "
                        f"побеждает последнее, предыдущее значение теряется молча")
                seen[k.value] = k.lineno
    return findings, files, dicts, keys


def _seeder_consts(tree) -> dict:
    """NAME = "lit" / NAME = os.environ.get("X", "lit") на уровне модуля."""
    out = {}
    for node in tree.body:
        if not (isinstance(node, ast.Assign) and len(node.targets) == 1
                and isinstance(node.targets[0], ast.Name)):
            continue
        v = node.value
        if isinstance(v, ast.Constant) and isinstance(v.value, str):
            out[node.targets[0].id] = v.value
        elif (isinstance(v, ast.Call) and len(v.args) == 2
              and isinstance(v.args[1], ast.Constant) and isinstance(v.args[1].value, str)):
            out[node.targets[0].id] = v.args[1].value  # os.environ.get(..., "lit")
    return out


def _emitted(tree, consts: dict, key: str):
    """Значение, которое посев РЕАЛЬНО выдаёт под этим ключом (побеждает последнее)."""
    val = None
    for node in ast.walk(tree):
        if not isinstance(node, ast.Dict):
            continue
        for k, v in zip(node.keys, node.values):
            if not (isinstance(k, ast.Constant) and k.value == key):
                continue
            if isinstance(v, ast.Constant) and isinstance(v.value, str):
                val = v.value
            elif isinstance(v, ast.Name):
                val = consts.get(v.id, val)
    return val


def alt_differs_from_primary_in_seeder(root: str) -> list[str]:
    """Проверка идёт по ПОБЕЖДАЮЩЕМУ значению, а не по наличию правильного.

    Отдельно от `collapsed_pairs`: та читает объявления окружения, а посев
    ПЕРЕЗАПИСЫВАЕТ их своими значениями на каждом прогоне. Схлопнуть пару можно,
    не тронув ни одного файла окружения, — так и вышло.
    """
    out = []
    for rel in seeder_files(root):
        tree = _parse(root, rel)
        if tree is None:
            continue
        consts = _seeder_consts(tree)
        alt = _emitted(tree, consts, "existingRegionAltId")
        primary = _emitted(tree, consts, "existingRegionId")
        if alt is None or primary is None:
            continue
        if alt == primary:
            out.append(f"{rel}: посев выдаёт existingRegionAltId={alt!r} — то же самое, "
                       f"что existingRegionId; «другой регион» другим не будет, и отказ "
                       f"по когерентности размещения не сможет сработать ни разу")
    return out


def run(root: str) -> int:
    findings, pairs, files = collapsed_pairs(root)
    findings += alt_region_has_a_producer(root)
    shadow, sfiles, sdicts, skeys = shadowed_fixture_keys(root)
    findings += shadow
    findings += alt_differs_from_primary_in_seeder(root)
    print("===== «другая» фикстура обязана быть другой =====")
    print(f"осмотрено файлов окружения: {files}; пар <X>Alt/<X> с непустыми значениями: {pairs}")
    print(f"прочитано файлов .py: {sfiles}; словарей: {sdicts}; строковых ключей: {skeys}")
    if pairs == 0:
        print("FAIL: пар не найдено ВОВСЕ — предмета у запрета не осталось. Либо ключи "
              "переименованы, либо обход не видит файлов окружения; «ноль находок» в "
              "обоих случаях ничего не значит.", file=sys.stderr)
        return 1
    if skeys == 0:
        print("FAIL: в посевах не прочитано НИ ОДНОГО строкового ключа — запрет на "
              "затенённое объявление остался без предмета. Либо посевы переехали, либо "
              "разбор их больше не читает.", file=sys.stderr)
        return 1
    if findings:
        print(f"FAIL: {len(findings)} находк(а/и):", file=sys.stderr)
        for f in findings:
            print(f"  {f}", file=sys.stderr)
        print("Отрицательный кейс, чьё условие схлопнуто на положительное, проверяет "
              "положительный путь и зеленеет всегда.", file=sys.stderr)
        return 1
    print("OK: каждая «другая» фикстура отличается от своей пары и имеет производителя.")
    return 0


def self_test() -> int:
    ok = True
    with tempfile.TemporaryDirectory() as td:
        d = os.path.join(td, "services", "x", "tests", "newman", "environments")
        os.makedirs(d)

        def env(name, values):
            with open(os.path.join(d, name), "w", encoding="utf-8") as fh:
                json.dump({"values": [{"key": k, "value": v} for k, v in values.items()]}, fh)

        # ДЕФЕКТ: «другой» регион совпал с первичным.
        env("collapsed.json", {"existingRegionId": "r1", "existingRegionAltId": "r1"})
        # ЗАКОННО: та же форма, значения разные.
        env("distinct.json", {"existingRegionId": "r1", "existingRegionAltId": "r2"})
        # ЗАКОННО: заготовка (пусто) — это не утверждение, и находкой быть не должна,
        # иначе гейт краснеет на каждом шаблоне и будет снят.
        env("placeholder.json", {"_suiteRegionId": "", "_suiteRegionAltId": ""})
        # БЕЗУПРЕЧНЫЙ посев кладётся рядом НАМЕРЕННО: без него `run` вернул бы отказ
        # по СВОЕЙ предпосылке (посевов нет — читать нечего), и проверка ниже
        # зеленела бы, не доказав ничего про внесённый дефект. Отказ обязан быть
        # отнесён к инъекции, а не к устройству синтетического дерева.
        sd0 = os.path.join(td, "tests", "authz-fixtures")
        os.makedirs(sd0)
        with open(os.path.join(sd0, "prodseed_matrix.py"), "w", encoding="utf-8") as fh:
            fh.write('_curl("POST", "/geo/v1/internal/regions", boot, {"id": "r2"})\n'
                     'fixtures = {"existingRegionId": "r1", "existingRegionAltId": "r2"}\n')

        findings, pairs, files = collapsed_pairs(td)
        if len(findings) != 1 or "collapsed.json" not in findings[0]:
            print(f"SELF-TEST FAIL: находки {findings}", file=sys.stderr)
            ok = False
        if pairs != 2:
            print(f"SELF-TEST FAIL: пар осмотрено {pairs}, ожидалось 2", file=sys.stderr)
            ok = False
        if files != 3:
            print(f"SELF-TEST FAIL: файлов осмотрено {files}, ожидалось 3", file=sys.stderr)
            ok = False
        if run(td) != 1:
            print("SELF-TEST FAIL: внесённый дефект не покраснел", file=sys.stderr)
            ok = False

    # Производитель: выдаёт один регион, создаёт другой → находка; совпало → тихо.
    with tempfile.TemporaryDirectory() as td2:
        sd = os.path.join(td2, "tests", "authz-fixtures")
        os.makedirs(sd)
        def seeder(text):
            with open(os.path.join(sd, "prodseed_matrix.py"), "w", encoding="utf-8") as fh:
                fh.write(text)

        bad = ('_curl("POST", "/geo/v1/internal/regions", boot, {"id": "r1"})\n'
               'fixtures = {"existingRegionAltId": "r2"}\n')
        seeder(bad)
        if len(alt_region_has_a_producer(td2)) != 1:
            print("SELF-TEST FAIL: выдача без посева не найдена", file=sys.stderr)
            ok = False
        seeder(bad.replace('"id": "r1"', '"id": "r2"'))
        if alt_region_has_a_producer(td2):
            print("SELF-TEST FAIL: посеянный регион объявлен находкой", file=sys.stderr)
            ok = False
        # Через постоянную — нормальная форма в дереве; обязана РАЗРЕШАТЬСЯ, иначе
        # проверка производителя стала бы молчаливой ровно там, где она нужна.
        seeder('ALT_REGION = os.environ.get("SEED_ALT_REGION", "r2")\n'
               '_curl("POST", "/geo/v1/internal/regions", boot, {"id": ALT_REGION})\n'
               'fixtures = {"existingRegionAltId": ALT_REGION}\n')
        if alt_region_has_a_producer(td2):
            print("SELF-TEST FAIL: посев через постоянную не распознан", file=sys.stderr)
            ok = False
        # Постоянная, которой нет: значение неразрешимо → находка, не тишина.
        seeder('_curl("POST", "/geo/v1/internal/regions", boot, {"id": "r1"})\n'
               'fixtures = {"existingRegionAltId": SOMEWHERE_ELSE}\n')
        if len(alt_region_has_a_producer(td2)) != 1:
            print("SELF-TEST FAIL: неразрешимое значение прошло молча", file=sys.stderr)
            ok = False

    # Затенённый ключ: тот же словарь объявляет одно имя дважды. ДЕФЕКТ, который
    # внесло слияние 2026-07-31 и который прошёл мимо всех прежних проверок —
    # обе строки синтаксически безупречны, конфликта нет, диффом не отличить.
    with tempfile.TemporaryDirectory() as td4:
        sd = os.path.join(td4, "tests", "authz-fixtures")
        os.makedirs(sd)

        def seeder4(text, name="prodseed_matrix.py"):
            with open(os.path.join(sd, name), "w", encoding="utf-8") as fh:
                fh.write(text)

        # ДЕФЕКТ: ключ объявлен дважды в ОДНОМ словаре — побеждает последний.
        seeder4('ALT = "r2"\n'
                'fixtures = {"existingRegionId": "r1", "existingRegionAltId": ALT,\n'
                '            "existingRegionAltId": "r1"}\n')
        f4, files4, dicts4, keys4 = shadowed_fixture_keys(td4)
        if len(f4) != 1 or ":3:" not in f4[0] or "existingRegionAltId" not in f4[0]:
            print(f"SELF-TEST FAIL: затенённый ключ не назван координатой: {f4}", file=sys.stderr)
            ok = False
        if files4 != 1 or keys4 != 3:
            print(f"SELF-TEST FAIL: перепись посева {files4}/{dicts4}/{keys4}, ожидалось 1/1/3",
                  file=sys.stderr)
            ok = False
        # И побеждающее значение при этом схлопывает пару — второй запрет ловит
        # ТО ЖЕ САМОЕ с другой стороны, по значению, а не по форме объявления.
        if len(alt_differs_from_primary_in_seeder(td4)) != 1:
            print("SELF-TEST FAIL: побеждающее значение, равное первичному, прошло",
                  file=sys.stderr)
            ok = False

        # ЗАКОННО: один ключ в РАЗНЫХ словарях — это не затенение, и находкой быть
        # не должно, иначе гейт краснеет на любом посеве с двумя профилями.
        seeder4('a = {"k": 1}\nb = {"k": 2}\n'
                'fixtures = {"existingRegionId": "r1", "existingRegionAltId": "r2"}\n')
        f4b, _, _, _ = shadowed_fixture_keys(td4)
        if f4b:
            print(f"SELF-TEST FAIL: разные словари объявлены затенением: {f4b}", file=sys.stderr)
            ok = False
        if alt_differs_from_primary_in_seeder(td4):
            print("SELF-TEST FAIL: разные значения объявлены схлопнутой парой", file=sys.stderr)
            ok = False

        # ЗАКОННО: вычисляемые/распакованные ключи — не наш предмет, молчим.
        seeder4('k = "x"\nfixtures = {k: 1, **{"y": 2},\n'
                '             "existingRegionId": "r1", "existingRegionAltId": "r2"}\n')
        f4c, _, _, _ = shadowed_fixture_keys(td4)
        if f4c:
            print(f"SELF-TEST FAIL: вычисляемый ключ объявлен затенением: {f4c}", file=sys.stderr)
            ok = False

    # Пустое дерево: предпосылки нет — падаем, а не отчитываемся «чисто».
    with tempfile.TemporaryDirectory() as td3:
        if run(td3) != 1:
            print("SELF-TEST FAIL: дерево без пар объявлено чистым", file=sys.stderr)
            ok = False

    print("SELF-TEST OK" if ok else "SELF-TEST FAILED")
    return 0 if ok else 1


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--self-test", action="store_true",
                    help="доказать инъекцией, что схлопнутая пара краснеет, а разная — нет")
    ap.add_argument("--root", default=None)
    args = ap.parse_args()
    if args.self_test:
        return self_test()
    root = args.root or os.path.abspath(
        os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
    return run(root)


if __name__ == "__main__":
    sys.exit(main())
