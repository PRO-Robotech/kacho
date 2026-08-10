#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""aggregate-shard-verdicts.py — один ответ про весь прогон после разнесения по раннерам.

ПРЕДМЕТ. До разнесения весь e2e судился в одном job'е: «зелено» означало «этот job
зелёный». После — восемь суит живут в пяти job'ах на пяти кластерах, и «зелено»
становится утверждением, которое НИКТО не делает, если его не сделать здесь.

ЗЕЛЁНЫМ СЧИТАЕТСЯ ТОЛЬКО ТО, ЧТО СОШЛОСЬ ПО ВСЕМ ЧЕТЫРЁМ ПУНКТАМ:
  1. описи пришли от КАЖДОГО шарда манифеста — отсутствие описи это «не
     выполнилось», а «не выполнилось» никогда не вычитается из вердикта и никогда
     не засчитывается за успех (testing.md);
  2. сумма отчитавшихся коллекций равна числу коллекций в дереве — ни одна не
     потеряна и ни одна не задвоена (это и есть проверка, что шардирование не
     стало тихим способом не гонять кейс);
  3. сумма упавших утверждений, неотвеченных запросов и упавших скриптов — ноль;
  4. пустых отчётов (отчёт есть, утверждений ноль) — ноль: «ничего не проверено»
     обязано быть отличимо от «ничего не упало».

ПОЧЕМУ ПУНКТ 2 ОТДЕЛЬНО ОТ ПУНКТА 1. Шард может отчитаться, а внутри него
коллекция — нет. Тогда его собственный гейт краснеет, но если краснота почему-то
не доехала (job снят по таймауту, артефакт не загрузился), сумма по дереву это
всё равно поймает: 81 из 82 — это находка, а не округление.

НИЧЕГО НЕ ВЫЧИТАЕТСЯ. Здесь нет ни списка известного красного, ни исключений, ни
маски. Если такой механизм когда-нибудь появится, он обязан истекать сам.

Самопроверка (`--self-test`) синтезирует набор описей и вносит по одному дефекту,
требуя красного на каждом, плюс держит рядом законный полный набор ⇒ зелёный.
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
GLOBS = ("services/*/tests/newman/collections/*" + SUFFIX,
         "gateway/tests/newman/collections/*" + SUFFIX)


def tree_collection_count(repo: pathlib.Path = REPO) -> int:
    r = subprocess.run(["git", "-C", str(repo), "ls-files", "--", *GLOBS],
                       capture_output=True, text=True)
    if r.returncode != 0:
        raise SystemExit(f"FATAL: git ls-files не отработал: {r.stderr.strip()}")
    return len([l for l in r.stdout.splitlines() if l.strip()])


def decide(manifest: dict, verdicts: dict[str, dict], tree_total: int) -> tuple[list[str], dict]:
    """verdicts: shard-id → опись. Возвращает (находки, числа). Ничего не вычитает."""
    findings: list[str] = []
    want = [s["id"] for s in manifest["shards"]]

    for sid in want:
        if sid not in verdicts:
            findings.append(
                f"шард '{sid}' НЕ ОТЧИТАЛСЯ — описи нет. Это «не выполнилось»: "
                f"ни зелёное, ни красное, и в вердикт засчитывается как ОТКАЗ")
    for sid in sorted(set(verdicts) - set(want)):
        findings.append(f"пришла опись шарда '{sid}', которого нет в манифесте — "
                        f"свод считает не то дерево")

    tot = {k: 0 for k in ("expected", "reported", "requests", "unanswered",
                          "assertions", "failed", "script_failed")}
    empty: list[str] = []
    missing: list[str] = []
    for sid in want:
        v = verdicts.get(sid)
        if not v:
            continue
        for k in tot:
            tot[k] += int(v.get(k) or 0)
        empty += list(v.get("empty") or [])
        missing += list(v.get("missing") or [])

    if tot["expected"] != tree_total:
        findings.append(f"шарды в сумме ОЖИДАЛИ {tot['expected']} коллекций, "
                        f"а в дереве их {tree_total} — покрытие неполно либо задвоено")
    if tot["reported"] != tree_total:
        findings.append(f"ОТЧИТАЛОСЬ {tot['reported']} коллекций из {tree_total} в дереве"
                        + (f"; без отчёта: {', '.join(sorted(missing))}" if missing else ""))
    if tot["failed"]:
        findings.append(f"упавших утверждений: {tot['failed']}")
    if tot["unanswered"]:
        findings.append(f"запросов без ответа: {tot['unanswered']} "
                        f"(«не дозвонились» — не «прошло»)")
    if tot["script_failed"]:
        findings.append(f"упавших скриптов кейсов: {tot['script_failed']} "
                        f"(проверки этих кейсов не исполнялись вовсе)")
    if empty:
        findings.append(f"пустых отчётов (0 утверждений): {len(empty)} — {', '.join(sorted(empty))}")
    if tot["assertions"] == 0 and tree_total:
        findings.append("НОЛЬ УТВЕРЖДЕНИЙ ЗА ВЕСЬ ПРОГОН — прочитано ничего; "
                        "это отказ, а не «ничего не упало»")

    tot["shards_expected"] = len(want)
    tot["shards_reported"] = len([s for s in want if s in verdicts])
    tot["tree"] = tree_total
    tot["empty"] = len(empty)
    return findings, tot


def load(dirpath: pathlib.Path) -> dict[str, dict]:
    out: dict[str, dict] = {}
    for p in sorted(dirpath.rglob("shard-verdict-*.json")):
        try:
            v = json.loads(p.read_text(encoding="utf-8"))
        except Exception as exc:
            print(f"ВНИМАНИЕ: опись {p} нечитаема ({exc.__class__.__name__}) — "
                  f"считается непришедшей", file=sys.stderr)
            continue
        sid = v.get("shard")
        if sid:
            out[sid] = v
    return out


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--dir", required=True, help="каталог со скачанными описями шардов")
    ap.add_argument("--summary", default=None, help="файл GITHUB_STEP_SUMMARY (необязательно)")
    args = ap.parse_args()

    manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
    verdicts = load(pathlib.Path(args.dir))
    tree_total = tree_collection_count()
    findings, tot = decide(manifest, verdicts, tree_total)

    lines = ["## Сводный вердикт e2e (по шардам)", "",
             f"шардов отчиталось **{tot['shards_reported']} из {tot['shards_expected']}**; "
             f"коллекций **{tot['reported']} из {tot['tree']}** в дереве", "",
             "| шард | суиты | коллекций | запросов | утверждений | УПАЛО | без ответа | скрипт упал |",
             "|---|---|---:|---:|---:|---:|---:|---:|"]
    for s in manifest["shards"]:
        v = verdicts.get(s["id"])
        if not v:
            lines.append(f"| `{s['id']}` | {' '.join(s['suites'])} | **НЕ ОТЧИТАЛСЯ** | — | — | — | — | — |")
            continue
        lines.append(f"| `{s['id']}` | {' '.join(s['suites'])} | {v['reported']}/{v['expected']} | "
                     f"{v['requests']} | {v['assertions']} | **{v['failed']}** | "
                     f"{v['unanswered']} | {v['script_failed']} |")
    lines += ["", f"**ИТОГО**: запросов {tot['requests']}, утверждений {tot['assertions']}, "
                  f"упавших {tot['failed']}, без ответа {tot['unanswered']}, "
                  f"упавших скриптов {tot['script_failed']}, пустых отчётов {tot['empty']}."]
    if findings:
        lines += ["", "### Находки", ""] + [f"- {f}" for f in findings]
    text = "\n".join(lines)
    print(text)
    if args.summary:
        with open(args.summary, "a", encoding="utf-8") as fh:
            fh.write(text + "\n")

    if findings:
        print("\nFAIL: сводный вердикт КРАСНЫЙ — находок "
              f"{len(findings)}. Ничего не вычитается и не объясняется.", file=sys.stderr)
        return 1
    print(f"\nOK: все {tot['shards_expected']} шардов отчитались, "
          f"{tot['reported']} коллекций из {tot['tree']}, упавших 0.")
    return 0


def _self_test() -> int:
    manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
    ids = [s["id"] for s in manifest["shards"]]
    # Число берётся ИЗ ДЕРЕВА, тем же способом, что и раскладка `full()` ниже.
    #
    # Здесь стояла константа, а `full()` рядом считала коллекции по `git ls-files`.
    # Две половины одной фикстуры из разных источников: пока числа совпадали,
    # самопроверка проходила, а первая же добавленная коллекция сделала «законный
    # близнец» красным — то есть гейт ронял конвейер от НОРМАЛЬНОЙ работы, и чинить
    # тянуло бы не его, а свод.
    #
    # Синтетическим здесь остаётся то, ради чего самопроверка и существует: описи
    # шардов и внесённые в них дефекты. Размер дерева синтезировать незачем — он и
    # есть тот вход, по которому свод судит.
    tree = tree_collection_count()

    def full() -> dict[str, dict]:
        per = {sid: {"shard": sid, "expected": 0, "reported": 0, "missing": [], "empty": [],
                     "requests": 0, "unanswered": 0, "assertions": 0,
                     "failed": 0, "script_failed": 0} for sid in ids}
        # раскладываем 82 коллекции по шардам ровно так, как их делит манифест
        import subprocess as sp
        for s in manifest["shards"]:
            n = 0
            for svc in s["suites"]:
                d = "gateway/tests/newman" if svc == "api-gateway" else f"services/{svc}/tests/newman"
                r = sp.run(["git", "-C", str(REPO), "ls-files", "--",
                            f"{d}/collections/*{SUFFIX}"], capture_output=True, text=True)
                n += len([l for l in r.stdout.splitlines() if l.strip()])
            per[s["id"]]["expected"] = n
            per[s["id"]]["reported"] = n
            per[s["id"]]["assertions"] = 10 * n
        return per

    ok = True

    def run(v: dict, label: str, want_red: bool, want_word: str | None = None) -> None:
        nonlocal ok
        findings, _ = decide(manifest, v, tree)
        red = bool(findings)
        good = (red == want_red) and (not want_word or any(want_word in f for f in findings))
        ok = ok and good
        print(f"  [{'ok ' if good else 'FAIL'}] {label}: "
              f"{'КРАСНЫЙ' if red else 'зелёный'} (ждали {'красного' if want_red else 'зелёного'})"
              + (f" — {findings[0]}" if red else ""))

    print("=== самопроверка свода (инъекция в обе стороны) ===")
    run(full(), "законный близнец: все шарды отчитались, 82 из 82", want_red=False)

    v = full(); v.pop(ids[-1])
    run(v, f"(а) шард '{ids[-1]}' не отчитался", want_red=True, want_word="НЕ ОТЧИТАЛСЯ")

    v = full(); v[ids[0]]["reported"] -= 1; v[ids[0]]["missing"] = ["vpc/subnet"]
    run(v, "(б) одна коллекция шарда без отчёта", want_red=True, want_word="ОТЧИТАЛОСЬ")

    v = full(); v[ids[0]]["expected"] -= 1; v[ids[0]]["reported"] -= 1
    run(v, "(в) коллекцию убрали из назначения шарда", want_red=True, want_word="ОЖИДАЛИ")

    v = full(); v[ids[1]]["failed"] = 3
    run(v, "(г) три упавших утверждения", want_red=True, want_word="упавших утверждений")

    v = full(); v[ids[1]]["unanswered"] = 2
    run(v, "(д) два запроса без ответа", want_red=True, want_word="без ответа")

    v = full(); v[ids[1]]["script_failed"] = 1
    run(v, "(е) упавший скрипт кейса", want_red=True, want_word="упавших скриптов")

    v = full(); v[ids[1]]["empty"] = ["iam/iam-role"]
    run(v, "(ж) отчёт без единого утверждения", want_red=True, want_word="пустых отчётов")

    v = full(); v["dns"] = {"shard": "dns", "expected": 0, "reported": 0}
    run(v, "(з) опись шарда вне манифеста", want_red=True, want_word="нет в манифесте")

    print("самопроверка:", "OK" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(_self_test() if "--self-test" in sys.argv[1:] else main())
