#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Матрица шардов e2e-newman и доводы helm шарда — из deploy/e2e-shards.json.

Единственный вывод производных строк манифеста (выключение компонентов для
helm, перечень образов, сборка консоли, явное отсутствие для гейта посадки,
домены ban #6). Его читают ДВА потребителя, и копии у них нет:

  * конвейер (`.github/workflows/e2e-newman.yml`, шаг «матрица»):
        e2e-shard-matrix.py            → строка `matrix=<json>` для GITHUB_OUTPUT;
  * проба рендера стенда шарда (`deploy/tests/helm/e2e-shard-stand-renders-test.sh`):
        e2e-shard-matrix.py --ids      → идентификаторы шардов, по строке;
        e2e-shard-matrix.py --helm ID  → доводы helm шарда `ID` одной строкой.

Пока вывод жил телом шага конвейера, проба могла судить лишь свою копию
логики, а копия и оригинал разъехались бы молча. Теперь проба рендерит ровно
ту строку, которую получит `make dev-up DEV_UP_VALUES=…` в задании шарда.

Значения, сопровождающие выключение (`gate_off_values` манифеста): компонент,
не названный шардом, выключается `--set <gate>.enabled=false`, а его спутники
из карты — `--set-string <ключ>=<значение>` той же строкой. Строка уходит в
make без кавычек, поэтому пробельный знак в ключе или значении — отказ
(код 1), а не тихий разрыв на два довода. Ключ карты вне `gates` — тоже отказ:
выключать нечего, и запись пережила свой предмет.
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

MANIFEST = Path(__file__).resolve().parent.parent / "e2e-shards.json"
SPACE = re.compile(r"\s")


def die(msg: str) -> None:
    print(f"e2e-shard-matrix: {msg}", file=sys.stderr)
    sys.exit(1)


def off_values(m: dict) -> dict[str, dict[str, str]]:
    carried = m.get("gate_off_values", {})
    gates = set(m["gates"])
    for gate, pairs in carried.items():
        if gate not in gates:
            die(f"gate_off_values.{gate}: компонента нет в gates — выключать нечего, запись пережила свой предмет")
        if not isinstance(pairs, dict) or not pairs:
            die(f"gate_off_values.{gate}: ожидается непустая карта «ключ → значение»")
        for key, val in pairs.items():
            if not isinstance(val, str):
                die(f"gate_off_values.{gate}.{key}: значение обязано быть строкой (уходит в --set-string)")
            if not key or SPACE.search(key) or SPACE.search(val):
                die(f"gate_off_values.{gate}.{key}: пробельный знак разорвал бы строку DEV_UP_VALUES на два довода")
    return carried


def helm_args(m: dict, shard: dict) -> str:
    carried = off_values(m)
    # Компонент, не названный шардом, выключается ЯВНО. Умолчание чарта —
    # «включён», поэтому «не упомянул» означало бы «поднял».
    off = [g for g in m["gates"] if g not in shard["components"]]
    words = []
    for g in off:
        words.append(f"--set {g}.enabled=false")
        words.extend(f"--set-string {k}={v}" for k, v in carried.get(g, {}).items())
    return " ".join(words)


def row(m: dict, s: dict) -> dict:
    gi = m["gate_images"]
    off = [g for g in m["gates"] if g not in s["components"]]
    images = list(m["core_images"]) + [gi[c] for c in s["components"] if c in gi]
    return {
        "id": s["id"],
        "suites": " ".join(s["suites"]),
        "helm": helm_args(m, s),
        "images": " ".join(sorted(set(images))),
        # Сборка консоли выводится ИЗ ТОЙ ЖЕ строки, что и её выключение (`off`),
        # а не объявляется вторым местом: иначе стенд без консоли и девять всё
        # равно собранных образов разъехались бы молча — и именно в тихую сторону,
        # где расход есть, а предмета у него нет.
        "build_ui": "0" if "uif" in off else "1",
        # Гейт посадки держит ЖЁСТКИЙ список ожидаемых сервисов, чтобы не
        # развернувшийся сервис не делал его зелёным молчанием. Поэтому
        # отсутствие называется явно — из той же карты gate_images.
        "skip": " ".join(sorted({gi[g] for g in off if g in gi})),
        # Домены, чью изоляцию Internal*-методов этот шард способен проверить: у
        # пробы ban #6 есть встречный контроль на внутреннем листенере домена, и
        # не развёрнутый домен ответить не может. Полноту объединения по шардам
        # утверждает assert-shard-coverage.py. Значение карты — СПИСОК: один
        # компонент способен дать больше одного домена.
        "ban6": " ".join(sorted(set(m["core_ban6_domains"]).union(*(
            m["gate_ban6_domains"].get(c, []) for c in s["components"]
        ), set()))),
    }


def main(argv: list[str]) -> int:
    m = json.loads(MANIFEST.read_text(encoding="utf-8"))
    shards = m["shards"]
    if not shards:
        die("шардов в манифесте ноль — матрица из пустого перечня не вердикт")
    if argv == ["--ids"]:
        print("\n".join(s["id"] for s in shards))
        return 0
    if len(argv) == 2 and argv[0] == "--helm":
        for s in shards:
            if s["id"] == argv[1]:
                print(helm_args(m, s))
                return 0
        die(f"шарда '{argv[1]}' в манифесте нет")
    if argv:
        die("доводы: без доводов | --ids | --helm <шард>")
    rows = [row(m, s) for s in shards]
    print("matrix=" + json.dumps({"shard": rows}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
