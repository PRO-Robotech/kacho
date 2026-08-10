#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""e2e-optional-transports.py — кто из суит НАБИРАЕТ транспорт к компоненту.

ПРЕДМЕТ. Часть адресов прогона ведёт к ЯДРУ (api-gateway, iam, провайдер подписи,
церемония) — оно есть на каждом стенде. Часть ведёт к КОМПОНЕНТУ, который шард
поднимает или не поднимает (deploy/e2e-shards.json). Проброс ко второму роду
адресов нельзя открывать безусловно: `kubectl port-forward svc/<нет такого>`
не встаёт и ЗАВЕРШАЕТСЯ, а не вставший проброс — по построению прогонщика —
объявляет прогон недействительным. Именно так четыре шарда из пяти не запустили
НИ ОДНОЙ суиты (прогон 31344367968: 0 из 16 коллекций у каждого).

ЧТО ЭТОТ СКРИПТ ОТВЕЧАЕТ. Для набора суит — какие ОПЦИОНАЛЬНЫЕ транспорты они
набирают. Ответ ВЫВОДИТСЯ ИЗ ДЕРЕВА: транспорт считается набираемым суитой, если
хоть одна её отслеживаемая коллекция или её исходный case-файл упоминает
`{{<имя переменной>}}`. Выписанный перечень «суита X набирает транспорт Y» здесь
не годится — он разъехался бы с коллекциями молча, и разъехался бы именно тогда,
когда кто-то добавит кейс.

ЕДИНИЦА СЧЁТА — ОТСЛЕЖИВАЕМЫЙ git-элемент (`git ls-files`), а не файл на диске:
gen.py кладёт коллекции в тот же каталог, поэтому рабочая копия после прогона
несёт артефакты, которых нет в дереве. Считать диск значило бы получать разные
ответы до и после прогона.

ОБЪЁМ ОСМОТРЕННОГО ПЕЧАТАЕТСЯ (`--census`). «Ни один транспорт не нужен» обязано
быть отличимо от «ни одного файла не прочитано»: если предикат перестанет
находить коллекции, это будет видно числом, а не молчанием.

ПОЧЕМУ ИСХОДНИКИ КЕЙСОВ ЧИТАЮТСЯ НАРЯДУ С КОЛЛЕКЦИЯМИ. Коллекция — производный
артефакт; между правкой кейса и регенерацией существует окно, в котором дерево
уже несёт нового потребителя, а собранная коллекция — ещё нет. Ответ «транспорт
не нужен», данный в этом окне, закрыл бы полосу ровно у того кейса, который её
только что завёл. Объединение двух источников закрывает окно с той стороны, где
ошибка дороже: лишний проброс — это лишний проброс, отсутствующий — это кейс,
который не исполнится.

ВЫХОД:
  --format lines (умолчание) — по строке на требуемый транспорт, поля через `|`:
      <переменная>|<сервис>|<порт назначения>|<env порта>|<порт по умолчанию>|<схема>|<пояснение>
    Формат читается bash-циклом прогонщика; пустой выход — законный ответ
    «опциональных транспортов не нужно».
  --format json — то же машинно, плюс перепись.
"""
from __future__ import annotations

import argparse
import json
import pathlib
import re
import subprocess
import sys

HERE = pathlib.Path(__file__).resolve().parent
DEPLOY = HERE.parent
ROOT = DEPLOY.parent
MANIFEST = DEPLOY / "e2e-shards.json"


def suite_dir(suite: str) -> str:
    """Каталог набора суиты — та же развилка, что у прогонщика (`suite_dir`)."""
    return "gateway/tests/newman" if suite == "api-gateway" else f"services/{suite}/tests/newman"


def tracked_files(prefix: str) -> list[str]:
    """Отслеживаемые файлы под префиксом. Не диск: см. «единица счёта» выше."""
    out = subprocess.run(
        ["git", "ls-files", "-z", "--", prefix],
        cwd=ROOT, capture_output=True, text=True, check=True,
    ).stdout
    return [p for p in out.split("\0") if p]


def suite_sources(suite: str) -> list[pathlib.Path]:
    """Файлы суиты, в которых потребитель транспорта наблюдаем.

    Коллекции — что ИСПОЛНИТСЯ; case-файлы — что УЖЕ ОБЪЯВЛЕНО и исполнится
    после ближайшей регенерации (окно между правкой и gen.py, см. шапку).
    """
    d = suite_dir(suite)
    keep = []
    for rel in tracked_files(d):
        p = pathlib.Path(rel)
        if p.suffix == ".json" and p.parent.name == "collections":
            keep.append(ROOT / p)
        elif p.suffix == ".py" and p.parent.name == "cases":
            keep.append(ROOT / p)
    return keep


def transports_dialled_by(suite: str, variables: list[str]) -> tuple[set[str], int]:
    """Какие из `variables` набирает суита + сколько её файлов прочитано."""
    pats = {v: re.compile(r"\{\{" + re.escape(v) + r"\}\}") for v in variables}
    found: set[str] = set()
    files = suite_sources(suite)
    for f in files:
        try:
            text = f.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        for var, pat in pats.items():
            if var not in found and pat.search(text):
                found.add(var)
    return found, len(files)


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--suites", required=True,
                    help="суиты через пробел — ровно то, что прогонщик держит в SERVICES")
    ap.add_argument("--format", choices=("lines", "json"), default="lines")
    ap.add_argument("--census", action="store_true",
                    help="печатать объём осмотренного в stderr (прогонщик это делает всегда)")
    args = ap.parse_args(argv)

    manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
    declared = manifest.get("optional_transports", {})

    suites = [s for s in args.suites.split() if s]
    variables = sorted(declared)

    required: dict[str, set[str]] = {}
    scanned = 0
    for suite in suites:
        hit, n = transports_dialled_by(suite, variables)
        scanned += n
        for var in hit:
            required.setdefault(var, set()).add(suite)

    if args.census:
        print(f"[transports] суит={len(suites)} прочитано файлов={scanned} "
              f"объявлено транспортов={len(variables)} требуется={len(required)}",
              file=sys.stderr)

    if args.format == "json":
        print(json.dumps({
            "suites": suites,
            "files_scanned": scanned,
            "declared": variables,
            "required": {v: sorted(s) for v, s in sorted(required.items())},
        }, ensure_ascii=False, indent=2))
        return 0

    for var in sorted(required):
        t = declared[var]
        print("|".join(str(x) for x in (
            var, t["service"], t["target_port"], t["port_env"],
            t["default_port"], t.get("scheme", "http"), t["why"],
        )))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
