#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""shard-verdict.py — что ЭТОТ шард реально исполнил, числом, в машинном виде.

ПРЕДМЕТ. После разнесения по раннерам вердикт больше не выносится в одном месте:
каждый шард судит свои суиты сам (assert-suites-green.sh), а свести это в один
ответ можно только по следам. Файл, который пишет этот скрипт, и есть след:
сколько коллекций шард ОБЯЗАН был отчитать (по дереву), сколько отчитал, и что
именно в этих отчётах.

ЭТО ОПИСЬ, А НЕ ВЕРДИКТ. Скрипт ничего не решает и всегда выходит нулём, если
сумел записать файл: решение принимают шаговые гейты шарда и сводный агрегатор.
Второй вердикт рядом с первым — способ получить два разных ответа на один вопрос.

ЧЕТЫРЕ ИСХОДА ЗАПРОСА считаются раздельно — теми же полями, что читает
assert-suites-green.sh, чтобы шард и свод не разошлись в арифметике:
  assertions.failed          — проверка отработала и сказала «нет»;
  requests.failed            — ответа не пришло вовсе (UNANSWERED);
  testScripts/prerequestScripts.failed — скрипт упал ДО проверок;
  assertions.total == 0      — отчёт есть, а не проверено ничего.
Ни одно из трёх последних никогда не сворачивается в первое и не вычитается.

ОТСУТСТВИЕ ОТЧЁТА — ОТДЕЛЬНОЕ СОСТОЯНИЕ, а не ноль падений: коллекция дерева, по
которой нет файла отчёта, попадает в `missing`, и агрегатор трактует это как «не
выполнилось».

ПОЧЕМУ отчётов нет — тоже часть описи (kacho#655). «Ни одна проба не судила»
получается двумя разными способами: стенд поднялся и прогон упал (находка о
дереве) — и стенд не поднялся, потому что посторонний хост не отдал чарты
(«условие не создано», `e2e-flow.md` §6). Следов у обоих одинаково ноль, поэтому
причина приходит ОТМЕТКОЙ от того, кто её наблюдал, — единственного владельца
материализации зависимостей. Своими силами свод этого не установит, и молчание
здесь означало бы, что чужая недоступность и дальше читается как красный шард.
"""
from __future__ import annotations

import argparse
import json
import pathlib
import subprocess
import sys

REPO = pathlib.Path(__file__).resolve().parents[2]
MANIFEST = REPO / "deploy" / "e2e-shards.json"
SUFFIX = ".postman_collection.json"

# Отметку кладёт единственный владелец материализации зависимостей умбреллы
# (deploy/scripts/helm-umbrella-deps.sh) и снимает её в начале каждого прогона,
# поэтому она описывает ЭТОТ прогон, а не позапрошлый.
PRECOND_MARK = REPO / "deploy" / "helm" / "umbrella" / ".kacho-deps-precondition-unmet"


def precondition() -> dict | None:
    """«Условие не создано» — если владелец материализации оставил отметку.

    Пустой файл — тоже отметка: причина могла не извлечься, но САМ ФАКТ чужой
    недоступности наблюдал тот, кто её положил. Считать отметку без текста
    отсутствующей значило бы терять третью категорию из-за пустой строки.
    """
    if not PRECOND_MARK.is_file():
        return None
    try:
        detail = PRECOND_MARK.read_text(encoding="utf-8").strip()
    except Exception:
        detail = ""
    return {"unmet": True, "kind": "external-chart-source",
            "detail": detail or "причина не извлеклась (отметка пуста)"}


def suite_dir(svc: str) -> pathlib.Path:
    return REPO / ("gateway/tests/newman" if svc == "api-gateway"
                   else f"services/{svc}/tests/newman")


def tracked_stems(svc: str) -> list[str]:
    """Коллекции суиты по ДЕРЕВУ (git), а не по диску: gen.py кладёт файлы рядом."""
    d = suite_dir(svc).relative_to(REPO)
    r = subprocess.run(["git", "-C", str(REPO), "ls-files", "--", f"{d}/collections/*{SUFFIX}"],
                       capture_output=True, text=True)
    if r.returncode != 0:
        raise SystemExit(f"FATAL: git ls-files не отработал: {r.stderr.strip()}")
    return sorted(pathlib.PurePath(l).name[: -len(SUFFIX)] for l in r.stdout.split() if l.strip())


def read_report(path: pathlib.Path) -> dict:
    stats = json.loads(path.read_text(encoding="utf-8")).get("run", {}).get("stats", {})

    def n(section: str, field: str) -> int:
        return int((stats.get(section) or {}).get(field) or 0)

    return {
        "requests": n("requests", "total"),
        "unanswered": n("requests", "failed"),
        "assertions": n("assertions", "total"),
        "failed": n("assertions", "failed"),
        "script_failed": n("testScripts", "failed") + n("prerequestScripts", "failed"),
    }


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--shard", required=True)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
    shard = next((s for s in manifest["shards"] if s["id"] == args.shard), None)
    if shard is None:
        raise SystemExit(f"FATAL: шарда '{args.shard}' нет в {MANIFEST}")

    v = {
        "shard": args.shard,
        "suites": shard["suites"],
        "expected": 0, "reported": 0, "missing": [], "empty": [],
        "requests": 0, "unanswered": 0, "assertions": 0,
        "failed": 0, "script_failed": 0,
        "per_collection": {},
    }

    pre = precondition()
    if pre:
        v["precondition"] = pre

    for svc in shard["suites"]:
        for stem in tracked_stems(svc):
            key = f"{svc}/{stem}"
            v["expected"] += 1
            rp = suite_dir(svc) / "out" / f"{stem}.json"
            if not rp.is_file():
                v["missing"].append(key)
                continue
            try:
                st = read_report(rp)
            except Exception as exc:  # нечитаемый отчёт — это НЕ «ноль падений»
                v["missing"].append(f"{key} (отчёт нечитаем: {exc.__class__.__name__})")
                continue
            v["reported"] += 1
            v["per_collection"][key] = st
            for f in ("requests", "unanswered", "assertions", "failed", "script_failed"):
                v[f] += st[f]
            if st["assertions"] == 0:
                v["empty"].append(key)

    out = pathlib.Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(v, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")

    print(f"[shard {args.shard}] суиты: {' '.join(shard['suites'])}")
    if pre:
        print(f"[shard {args.shard}] УСЛОВИЕ НЕ СОЗДАНО: {pre['detail']} — "
              f"пробы не выполнялись вовсе; числа ниже это не вердикт о них")
    print(f"[shard {args.shard}] коллекций отчиталось {v['reported']} из {v['expected']}; "
          f"запросов {v['requests']}, утверждений {v['assertions']}, "
          f"УПАВШИХ {v['failed']}, БЕЗ ОТВЕТА {v['unanswered']}, "
          f"СКРИПТ УПАЛ {v['script_failed']}, ПУСТЫХ ОТЧЁТОВ {len(v['empty'])}")
    if v["missing"]:
        print(f"[shard {args.shard}] БЕЗ ОТЧЁТА ({len(v['missing'])}): {', '.join(v['missing'])}")
    print(f"[shard {args.shard}] опись записана: {out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
