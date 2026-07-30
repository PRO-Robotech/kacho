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

ПРЕДПОСЫЛКА ПРОВЕРЯЕТСЯ. Запрет держится на том, что пары в дереве есть. Ноль
найденных пар — не «чисто», а потерянный предмет либо сломанный обход; гейт
падает и говорит об этом. Перепись осмотренного печатается по той же причине.

Запуск:
    python3 deploy/scripts/assert-alt-fixtures-are-another.py
    python3 deploy/scripts/assert-alt-fixtures-are-another.py --self-test
Код возврата: 0 — все «другие» действительно другие и посеяны; 1 — находка.
"""
from __future__ import annotations

import argparse
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


def run(root: str) -> int:
    findings, pairs, files = collapsed_pairs(root)
    findings += alt_region_has_a_producer(root)
    print("===== «другая» фикстура обязана быть другой =====")
    print(f"осмотрено файлов окружения: {files}; пар <X>Alt/<X> с непустыми значениями: {pairs}")
    if pairs == 0:
        print("FAIL: пар не найдено ВОВСЕ — предмета у запрета не осталось. Либо ключи "
              "переименованы, либо обход не видит файлов окружения; «ноль находок» в "
              "обоих случаях ничего не значит.", file=sys.stderr)
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
