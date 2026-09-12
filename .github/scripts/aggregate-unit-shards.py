#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Сводный вердикт юнитов: один ответ про всё дерево после разнесения по шардам.

ПРЕДМЕТ
-------
До распила «зелено» означало «этот шаг зелёный». После — 206 пакетов живут в 13
заданиях, и «зелено» становится утверждением, которое НИКТО не делает, если его не
сделать здесь. Работа без упавших шагов выходит `success` — в том числе когда
шардов исполнилось ноль.

ЗЕЛЁНЫМ СЧИТАЕТСЯ ТОЛЬКО ТО, ЧТО СОШЛОСЬ ПО ВСЕМ ПУНКТАМ
--------------------------------------------------------
  1. ОПИСЬ ПРИШЛА ОТ КАЖДОГО шарда плана. Отсутствие описи — «не выполнилось», а
     «не выполнилось» не вычитается из вердикта и не засчитывается за успех;
  2. ДЕРЕВО ИЗМЕРЕНО ДВАЖДЫ и сошлось: перечень плана против собственного
     `go list`. Два места об одном предмете расходятся молча, поэтому сверяются, а
     не берутся на веру;
  3. РОЗДАНО = ДЕРЕВО: объединение розданного по описям совпадает с деревом как
     МНОЖЕСТВО. Потеря плюс задвоение дают тот же размер, поэтому размера мало;
  4. ИСПОЛНЕНО = РОЗДАНО: у каждого розданного пакета есть терминальное событие.
     Пакет, розданный и не отчитавшийся, — третья категория, а не округление;
  5. ПАДЕНИЙ НОЛЬ и проб исполнено БОЛЬШЕ НУЛЯ: «ничего не проверено» обязано
     быть отличимо от «ничего не упало»;
  6. ИСХОД ОБЕИХ РАБОТ — `success`. Установить «шард не запускался» своими силами
     свод не может: следов у «не запускался» и у «исполнялся и не доехал»
     одинаково ноль, поэтому исходы приходят параметрами из `needs.<job>.result`.

НИЧЕГО НЕ ВЫЧИТАЕТСЯ. Здесь нет ни списка известного красного, ни маски, ни
исключений. Появится такой механизм — он обязан истекать сам.

КАТЕГОРИЯ ИСХОДА НАЗЫВАЕТСЯ ПЕРВОЙ СТРОКОЙ. Их три, четвёртой нет: «упало 4
пробы», «двух шардов не было» и «ни один шард не отчитался» — разные вопросы к
разным людям, и одним красным они неразличимы.

Самопроверка (`--self-test`) синтезирует полный законный набор описей и вносит по
одному дефекту, требуя красного на каждом и зелёного на законном близнеце.

Запуск:
  python3 .github/scripts/aggregate-unit-shards.py --self-test
  python3 .github/scripts/aggregate-unit-shards.py --plan in/unit-shard-plan.json \
      --dir in --plan-result success --shard-result success --summary "$GITHUB_STEP_SUMMARY"
"""
from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path

LIST_FORMAT = "{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}"

GREEN = "ЗЕЛЁНЫЙ"
RED = "КРАСНЫЙ"
PARTIAL = "КРАСНЫЙ · ЧАСТЬ ДЕРЕВА НЕ СУДИЛАСЬ"
DIDNOTRUN = "НЕ ВЫПОЛНИЛОСЬ"

GLOSS = {
    GREEN: "всё дерево судилось, падений нет",
    RED: "всё дерево судилось, есть падения — читать «Находки»",
    PARTIAL: ("часть пакетов НЕ судилась вовсе: их зелёного нет и красного тоже — "
              "числа ниже относятся к меньшему, чем всё дерево"),
    DIDNOTRUN: ("вердикта о пробах не выносил НИКТО: описей нет либо исполнено ноль "
                "проб. Прогон повторяется, а не читается как красные юниты"),
}


def own_tree(root: Path) -> tuple[list[str] | None, str]:
    """Собственное измерение дерева. `None` — измерение НЕ СДЕЛАНО, и это не «сошлось»."""
    try:
        p = subprocess.run(["go", "list", "-f", LIST_FORMAT, "./..."], cwd=root,
                           capture_output=True, text=True, timeout=1800)
    except (OSError, subprocess.SubprocessError) as e:
        return None, f"`go list` не отработал: {e}"
    if p.returncode != 0:
        return None, f"`go list` вышел кодом {p.returncode}: {p.stderr.strip()[:300]}"
    pkgs = sorted({x.strip() for x in p.stdout.splitlines() if x.strip()})
    if not pkgs:
        return None, "`go list` назвал ноль пакетов с пробами — обход пуст"
    return pkgs, "go list -f '{{if or .TestGoFiles .XTestGoFiles}}…' ./..."


def adjudicate(plan: dict | None, censuses: dict[str, dict],
               own: list[str] | None, own_note: str,
               plan_result: str, shard_result: str) -> tuple[str, list[str], dict]:
    """Категория · находки · числа. Чистая функция — самопроба подаёт свои данные."""
    findings: list[str] = []

    if plan is None:
        return (DIDNOTRUN,
                ["ПЛАНА НЕТ: артефакт плана не скачан либо не читается. Свод не знает "
                 "ни дерева, ни перечня шардов — судить не по чему"],
                {"tree": 0, "handed": 0, "executed_packages": 0, "tests": 0})

    plan_tree = sorted(set(plan.get("tree_packages") or []))
    plan_shards = [s.get("id") for s in (plan.get("shards") or []) if s.get("id")]

    if not plan_tree:
        findings.append("ПЛАН ПУСТ: в нём ноль пакетов дерева — это отказ, а не идеал")
    if not plan_shards:
        findings.append("ПЛАН БЕЗ ШАРДОВ: перечня шардов нет, значит «опись от каждого» "
                        "проверить нечем")

    # 2. Дерево измерено дважды.
    if own is None:
        findings.append(
            f"ДЕРЕВО ИЗМЕРЕНО ОДИН РАЗ: собственное измерение не сделано ({own_note}). "
            f"Это НЕ «сошлось»: перечень плана остаётся непроверенным")
    elif sorted(set(own)) != plan_tree:
        only_plan = sorted(set(plan_tree) - set(own))
        only_own = sorted(set(own) - set(plan_tree))
        findings.append(
            f"ДВА ИЗМЕРЕНИЯ ДЕРЕВА РАСХОДЯТСЯ: только в плане {len(only_plan)} "
            f"({', '.join(only_plan[:5])}), только в своём {len(only_own)} "
            f"({', '.join(only_own[:5])}). План считался на другой ревизии либо другим "
            f"предикатом")

    # 1. Опись от каждого шарда.
    missing_shards = [s for s in plan_shards if s not in censuses]
    if missing_shards:
        findings.append(
            f"ШАРДЫ НЕ ОТЧИТАЛИСЬ ({len(missing_shards)}): {', '.join(missing_shards)}. "
            f"Это третья категория исхода: вердикта о их пакетах не дал НИКТО, и в "
            f"успех она не засчитывается")
    alien = [s for s in censuses if s not in plan_shards]
    if alien:
        findings.append(
            f"ОПИСЬ ОТ ШАРДА ВНЕ ПЛАНА ({len(alien)}): {', '.join(sorted(alien))}. "
            f"Опись и план говорят о разных прогонах")

    # 3-4. Роздано и исполнено.
    handed: list[str] = []
    seen: list[str] = []
    tests = failed = pkg_failed = build_failed = unfinished = skipped = 0
    bad_rc: list[str] = []
    not_reported: list[str] = []
    for sid in sorted(censuses):
        c = censuses[sid]
        handed.extend(c.get("assigned") or [])
        seen.extend(c.get("seen") or [])
        miss = c.get("missing") or []
        if miss:
            not_reported.append(f"{sid}: {len(miss)} ({', '.join(miss[:3])})")
        tests += int(c.get("tests_executed") or 0)
        failed += int(c.get("tests_failed") or 0)
        pkg_failed += int(c.get("packages_failed") or 0)
        build_failed += int(c.get("builds_failed") or 0)
        unfinished += int(c.get("tests_unfinished") or 0)
        skipped += int(c.get("tests_skipped") or 0)
        if int(c.get("rc") or 0) != 0:
            bad_rc.append(f"{sid} (код {c.get('rc')})")

    handed_set = set(handed)
    dup = sorted({p for p in handed if handed.count(p) > 1})
    lost = sorted(set(plan_tree) - handed_set) if plan_tree else []

    if lost:
        findings.append(
            f"ПАКЕТЫ НЕ РОЗДАНЫ НИ ОДНОМУ ОТЧИТАВШЕМУСЯ ШАРДУ ({len(lost)}): "
            f"{', '.join(lost[:6])}. Пропуск не есть проход")
    if dup:
        findings.append(
            f"ПАКЕТ РОЗДАН ДВАЖДЫ ({len(dup)}): {', '.join(dup[:6])}. Вердиктов о нём "
            f"два, а времени потрачено вдвое")
    if not_reported:
        findings.append(
            f"РОЗДАНО И НЕ ОТЧИТАЛОСЬ: {'; '.join(not_reported)}. У пакета нет "
            f"терминального события — вердикта о нём нет ни одного")

    # 5. Падения и беспредметность.
    if failed or pkg_failed or build_failed:
        findings.append(
            f"ПАДЕНИЯ: проб {failed} · пакетов {pkg_failed} · сборок {build_failed}. "
            f"Разбор — в логе шарда, он не сокращается")
    if unfinished:
        findings.append(
            f"НЕ ВЫПОЛНИЛОСЬ: проб начато и не завершено {unfinished}. Вердикта у них "
            f"нет ни у одной")
    if bad_rc:
        findings.append(
            f"ШАРД ВЫШЕЛ НЕНУЛЁМ: {', '.join(bad_rc)}. Вердикт шарда сильнее сводных "
            f"чисел: он мог найти необъявленный пропуск, который в числа не попадает")

    # 6. Исходы работ. Каскад называется ОДИН раз: если план не отработал, «шарды
    # не отчитались» есть СЛЕДСТВИЕ одной находки, а не тринадцать находок.
    for what, res in (("план", plan_result), ("шарды", shard_result)):
        if res and res != "success":
            findings.append(
                f"ИСХОД РАБОТЫ «{what}» — `{res}`, а не `success`. "
                f"{'Пропущенная работа не краснеет, она молчит — потому её исход и приходит параметром' if res == 'skipped' else 'Вердикт работы сильнее сводных чисел'}")

    num = {
        "tree": len(plan_tree),
        "handed": len(handed_set),
        "handed_rows": len(handed),
        "executed_packages": len(set(seen) & handed_set),
        "tests": tests,
        "skipped": skipped,
        "shards_planned": len(plan_shards),
        "shards_reported": len(censuses),
    }

    if not censuses or tests == 0:
        return DIDNOTRUN, findings or [
            "описей ноль либо проб исполнено ноль — перепись беспредметна"], num
    if missing_shards or lost or not_reported:
        return PARTIAL, findings, num
    if findings:
        return RED, findings, num
    return GREEN, findings, num


def read_censuses(d: Path) -> dict[str, dict]:
    out: dict[str, dict] = {}
    if not d.is_dir():
        return out
    for p in sorted(d.rglob("*.json")):
        try:
            doc = json.loads(p.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            continue
        sid = doc.get("shard")
        if isinstance(doc, dict) and sid:
            out[str(sid)] = doc
    return out


def render(category: str, findings: list[str], num: dict,
           censuses: dict[str, dict]) -> str:
    lines = [f"ИСХОД: {category} — {GLOSS[category]}", ""]
    lines.append(f"пакетов с пробами в дереве N {num['tree']} · "
                 f"роздано по шардам M {num['handed']} · "
                 f"исполнено K {num['executed_packages']}")
    lines.append(f"шардов в плане {num['shards_planned']} · отчиталось "
                 f"{num['shards_reported']} · проб исполнено {num['tests']} · "
                 f"пропущено {num['skipped']}")
    if censuses:
        lines.append("")
        lines.append("| шард | роздано | исполнено | проб | до первой пробы, с | код |")
        lines.append("|---|---:|---:|---:|---:|---:|")
        for sid in sorted(censuses):
            c = censuses[sid]
            ttf = c.get("seconds_to_first_test")
            lines.append(
                f"| {sid} | {len(c.get('assigned') or [])} | {len(c.get('seen') or [])} "
                f"| {c.get('tests_executed', 0)} "
                f"| {'—' if ttf is None else f'{float(ttf):.0f}'} | {c.get('rc', '?')} |")
    if findings:
        lines.append("")
        lines.append(f"НАХОДОК {len(findings)}:")
        lines.extend("  · " + f for f in findings)
    return "\n".join(lines) + "\n"


# ─── самопроба ──────────────────────────────────────────────────────────────

MOD = "github.com/PRO-Robotech/kacho"
TREE = [f"{MOD}/services/vpc/internal/repo", f"{MOD}/services/vpc/internal/usecase",
        f"{MOD}/gateway/internal/allowlist", f"{MOD}/internal/repohygiene"]
PLAN = {
    "module": MOD,
    "tree_packages": TREE,
    "shards": [{"id": "vpc", "packages": TREE[:2]},
               {"id": "gateway", "packages": TREE[2:3]},
               {"id": "internal", "packages": TREE[3:]}],
}


def _census(sid: str, pkgs: list[str], **kw) -> dict:
    c = {"shard": sid, "assigned": list(pkgs), "seen": list(pkgs), "missing": [],
         "tests_executed": 7 * len(pkgs), "tests_failed": 0, "tests_skipped": 0,
         "packages_failed": 0, "builds_failed": 0, "tests_unfinished": 0,
         "seconds_to_first_test": 42.0, "rc": 0}
    c.update(kw)
    return c


def _full() -> dict[str, dict]:
    return {s["id"]: _census(s["id"], s["packages"]) for s in PLAN["shards"]}


# Часовой «аргумент не подан». `None` для этой роли не годится: он и есть тот
# вход, который проверяется («плана нет», «измерение не сделано»), и умолчание,
# заданное через `None`, подменяло бы инъекцию законным близнецом — обе такие
# пробы зеленели, доказывая ровно ничего.
_ABSENT = object()


def _case(name: str, censuses: dict[str, dict], expect: str, needle: str | None,
          plan=_ABSENT, own=_ABSENT, plan_result="success",
          shard_result="success") -> bool:
    cat, findings, num = adjudicate(PLAN if plan is _ABSENT else plan, censuses,
                                   TREE if own is _ABSENT else own, "своё измерение",
                                   plan_result, shard_result)
    ok = cat == expect and (needle is None or any(needle in f for f in findings))
    print(f"  [{'OK ' if ok else 'ОТКАЗ'}] {name}: {cat} (ждали {expect}); "
          f"N {num['tree']} M {num['handed']} K {num['executed_packages']}")
    if not ok:
        for f in findings:
            print("      · " + f[:200])
    return ok


def self_test() -> int:
    print("самопроба сводного вердикта: инъекция в обе стороны по каждой оси")
    ok = True

    ok &= _case("контроль: полный законный набор → ЗЕЛЁНЫЙ", _full(), GREEN, None)

    c = _full(); c.pop("gateway")
    ok &= _case("описи одного шарда нет → не зелёный", c, PARTIAL, "ШАРДЫ НЕ ОТЧИТАЛИСЬ")

    c = _full(); c["vpc"] = _census("vpc", TREE[:2], seen=TREE[:1], missing=TREE[1:2])
    ok &= _case("пакет роздан и не отчитался → не зелёный", c, PARTIAL,
                "РОЗДАНО И НЕ ОТЧИТАЛОСЬ")

    c = _full(); c["vpc"] = _census("vpc", TREE[:1], seen=TREE[:1])
    ok &= _case("пакет не роздан ни одному шарду → не зелёный", c, PARTIAL,
                "НЕ РОЗДАНЫ НИ ОДНОМУ")

    c = _full(); c["gateway"] = _census("gateway", TREE[2:3] + TREE[:1],
                                       seen=TREE[2:3] + TREE[:1])
    ok &= _case("пакет роздан дважды → КРАСНЫЙ", c, RED, "РОЗДАН ДВАЖДЫ")

    c = _full(); c["internal"] = _census("internal", TREE[3:], tests_failed=2, rc=1)
    ok &= _case("упавшая проба → КРАСНЫЙ", c, RED, "ПАДЕНИЯ")

    c = _full(); c["internal"] = _census("internal", TREE[3:], rc=1)
    ok &= _case("шард вышел ненулём при нуле падений → КРАСНЫЙ", c, RED,
                "ВЫШЕЛ НЕНУЛЁМ")

    c = _full(); c["internal"] = _census("internal", TREE[3:], tests_unfinished=3)
    ok &= _case("проба начата и не завершена → КРАСНЫЙ", c, RED, "НЕ ВЫПОЛНИЛОСЬ")

    ok &= _case("исход работы шардов `skipped` → не зелёный", _full(), RED,
                "ИСХОД РАБОТЫ «шарды»", shard_result="skipped")
    ok &= _case("исход работы плана `failure` → не зелёный", _full(), RED,
                "ИСХОД РАБОТЫ «план»", plan_result="failure")

    ok &= _case("описей ноль → НЕ ВЫПОЛНИЛОСЬ", {}, DIDNOTRUN, None)

    c = {s["id"]: _census(s["id"], s["packages"], tests_executed=0)
         for s in PLAN["shards"]}
    ok &= _case("проб исполнено ноль при полных описях → НЕ ВЫПОЛНИЛОСЬ", c,
                DIDNOTRUN, None)

    ok &= _case("плана нет → НЕ ВЫПОЛНИЛОСЬ", _full(), DIDNOTRUN, "ПЛАНА НЕТ",
                plan=None)

    ok &= _case("собственное измерение не сделано → КРАСНЫЙ, а не «сошлось»",
                _full(), RED, "ИЗМЕРЕНО ОДИН РАЗ", own=None)

    ok &= _case("два измерения дерева расходятся → КРАСНЫЙ", _full(), RED,
                "РАСХОДЯТСЯ", own=TREE + [f"{MOD}/services/geo/internal/repo"])

    c = _full(); c["alien"] = _census("alien", [])
    ok &= _case("опись от шарда вне плана → КРАСНЫЙ", c, RED, "ВНЕ ПЛАНА")

    # Законный близнец шума: пропуски сами по себе зелёного не отменяют — их
    # судит прогонщик шарда по ведомости, а не свод.
    c = _full(); c["vpc"] = _census("vpc", TREE[:2], tests_skipped=5)
    ok &= _case("пропуски при нулевом коде шарда → ЗЕЛЁНЫЙ (судит прогонщик)", c,
                GREEN, None)

    print("самопроба:", "ПРОЙДЕНА" if ok else "ПРОВАЛЕНА")
    return 0 if ok else 1


def main() -> int:
    ap = argparse.ArgumentParser(description="сводный вердикт юнитов по шардам")
    ap.add_argument("--self-test", action="store_true")
    ap.add_argument("--plan", default="unit-shard-plan.json")
    ap.add_argument("--dir", default="unit-censuses")
    ap.add_argument("--plan-result", default="")
    ap.add_argument("--shard-result", default="")
    ap.add_argument("--summary", default="")
    ap.add_argument("--root", default=None)
    args = ap.parse_args()

    if args.self_test:
        return self_test()

    root = Path(args.root) if args.root else Path(__file__).resolve().parents[2]
    try:
        plan = json.loads(Path(args.plan).read_text(encoding="utf-8"))
    except (OSError, ValueError) as e:
        plan = None
        sys.stdout.write(f"план не прочитан ({args.plan}): {e}\n")

    censuses = read_censuses(Path(args.dir))
    own, note = own_tree(root)
    category, findings, num = adjudicate(plan, censuses, own, note,
                                         args.plan_result, args.shard_result)
    text = render(category, findings, num, censuses)
    sys.stdout.write(text)
    if args.summary:
        try:
            with open(args.summary, "a", encoding="utf-8") as fh:
                fh.write("## Сводный вердикт юнитов\n\n" + text)
        except OSError as e:
            sys.stdout.write(f"(сводку не записали: {e})\n")

    if category == GREEN:
        return 0
    return 2 if category == DIDNOTRUN else 1


if __name__ == "__main__":
    sys.exit(main())
