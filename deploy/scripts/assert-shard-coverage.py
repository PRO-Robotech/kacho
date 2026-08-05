#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""assert-shard-coverage.py — шардирование не должно стать способом не гонять кейс.

ПРЕДМЕТ. До разбиения по раннерам один прогон судил ВСЕ коллекции дерева, и
«потерять» коллекцию было негде: она либо есть в дереве, либо её нет. После
разбиения появляется третье состояние — коллекция ЕСТЬ, а ни один шард её не
берёт. Такое дерево выглядит здоровым с любой стороны: файл на месте, гейт суиты
зелёный у всех, кто её гонял (никто), сводный вердикт складывает нули. Это ровно
тот класс, который правила называют «форма проверки без содержания», и он тем
опаснее, что заводится не правкой теста, а правкой РАСПИСАНИЯ.

ЧТО УТВЕРЖДАЕТСЯ (по дереву, а не по памяти):
  1. каждая суита дерева назначена РОВНО одному шарду;
  2. ни один шард не называет суиту, которой в дереве нет;
  3. сумма коллекций по шардам равна числу коллекций в дереве, и равенство
     держится ПОКОЛЛЕКЦИОННО, а не только итогом (иначе потеря в одной суите
     компенсируется добавкой в другой);
  4. состав суит совпадает с умолчанием SERVICES в newman-parallel.sh — новая
     суита не может появиться в прогоне, минуя это разбиение;
  5. каждый компонент, который шард называет, объявлен в gates, и каждый gate
     реально условен в Chart.yaml зонтичного чарта (условие, которого нет,
     рендерится успешно и не делает ничего — измеренный дефект vpc/compute).

ЕДИНИЦА СЧЁТА — ОТСЛЕЖИВАЕМЫЙ git-элемент (`git ls-files`), а не файл на диске:
`gen.py` кладёт коллекции в тот же каталог, и рабочая копия после прогона
содержит артефакты, которых нет в дереве. Считать диск значило бы получать
разные числа до и после прогона.

ОБЪЁМ ОСМОТРЕННОГО ПЕЧАТАЕТСЯ. «Ноль находок» обязано быть отличимо от «ноль
прочитанного»: если предикат перестанет находить коллекции, это будет видно
числом, а не молчанием.

Самопроверка (`--self-test`) вносит четыре дефекта по одному и требует красного
на каждом, плюс держит рядом ЗАКОННОГО близнеца (нетронутое дерево ⇒ зелёный).
Гейт, который не покраснел на внесённом дефекте, — не гейт.
"""
from __future__ import annotations

import json
import pathlib
import re
import subprocess
import sys

HERE = pathlib.Path(__file__).resolve().parent
DEPLOY = HERE.parent
ROOT = DEPLOY.parent
MANIFEST = DEPLOY / "e2e-shards.json"
PARALLEL_SH = DEPLOY / "scripts" / "newman-parallel.sh"
UMBRELLA_CHART = DEPLOY / "helm" / "umbrella" / "Chart.yaml"

COLLECTION_GLOBS = (
    "services/*/tests/newman/collections/*.postman_collection.json",
    "gateway/tests/newman/collections/*.postman_collection.json",
)


def tracked_collections(root: pathlib.Path) -> dict[str, list[str]]:
    """suite -> отсортированный список stem'ов коллекций, ОТСЛЕЖИВАЕМЫХ git."""
    out: dict[str, list[str]] = {}
    r = subprocess.run(["git", "-C", str(root), "ls-files", "--", *COLLECTION_GLOBS],
                       capture_output=True, text=True)
    if r.returncode != 0:
        raise SystemExit(f"FATAL: git ls-files не отработал в {root}: {r.stderr.strip()}")
    for line in r.stdout.splitlines():
        line = line.strip()
        if not line:
            continue
        parts = line.split("/")
        suite = parts[1] if parts[0] == "services" else "api-gateway"
        stem = parts[-1][: -len(".postman_collection.json")]
        out.setdefault(suite, []).append(stem)
    for k in out:
        out[k].sort()
    return out


def parallel_default_services(path: pathlib.Path) -> list[str]:
    """Умолчание SERVICES из newman-parallel.sh — что прогон возьмёт, если не сузить."""
    text = path.read_text(encoding="utf-8")
    m = re.search(r'^SERVICES="\$\{SERVICES:-([^}]*)\}"', text, re.M)
    if not m:
        raise SystemExit(f"FATAL: в {path} не найдено умолчание SERVICES — "
                         "предпосылка гейта отпала, чинить надо гейт, а не молчать")
    return m.group(1).split()


def gated_deps(path: pathlib.Path) -> set[str]:
    """Имена зависимостей зонтичного чарта, у которых ЕСТЬ `condition: <имя>.enabled`."""
    gated: set[str] = set()
    for m in re.finditer(r'^\s*condition:\s*([A-Za-z0-9_.-]+)\.enabled\s*$',
                         path.read_text(encoding="utf-8"), re.M):
        gated.add(m.group(1))
    return gated


def check(root: pathlib.Path, manifest_path: pathlib.Path) -> tuple[list[str], dict]:
    findings: list[str] = []
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    shards = manifest["shards"]
    gates = set(manifest["gates"])

    tree = tracked_collections(root)
    tree_suites = set(tree)
    tree_total = sum(len(v) for v in tree.values())

    if tree_total == 0:
        findings.append("ПРОЧИТАНО НОЛЬ КОЛЛЕКЦИЙ — предикат не нашёл предмет; "
                        "это отказ, а не «всё покрыто»")

    # 1-2. каждая суита ровно у одного шарда; чужих суит нет
    seen: dict[str, list[str]] = {}
    for sh in shards:
        for suite in sh["suites"]:
            seen.setdefault(suite, []).append(sh["id"])
    for suite, owners in sorted(seen.items()):
        if suite not in tree_suites:
            findings.append(f"шард(ы) {owners} называют суиту '{suite}', которой нет в дереве")
        if len(owners) > 1:
            findings.append(f"суита '{suite}' задвоена между шардами {owners} — "
                            f"её коллекции посчитаются дважды")
    for suite in sorted(tree_suites - set(seen)):
        findings.append(f"суита '{suite}' ({len(tree[suite])} коллекций) НЕ НАЗНАЧЕНА "
                        f"ни одному шарду — её кейсы не гоняет никто")

    # 3. равенство поколлекционное, а не только итогом
    assigned_total = 0
    for suite, owners in sorted(seen.items()):
        if suite in tree_suites and len(owners) == 1:
            assigned_total += len(tree[suite])
    if assigned_total != tree_total:
        findings.append(f"сумма коллекций по шардам {assigned_total} != {tree_total} в дереве")

    # 4. состав суит == умолчание прогонщика
    default_services = set(parallel_default_services(PARALLEL_SH))
    for suite in sorted(default_services - set(seen)):
        findings.append(f"newman-parallel.sh гонит суиту '{suite}' по умолчанию, "
                        f"а ни один шард её не берёт")
    for suite in sorted(set(seen) - default_services):
        findings.append(f"шард берёт суиту '{suite}', которой нет в умолчании "
                        f"SERVICES прогонщика — она не будет исполнена")

    # 5. компоненты объявлены и реально условны
    chart_gated = gated_deps(UMBRELLA_CHART)
    for g in sorted(gates):
        if g not in chart_gated:
            findings.append(f"компонент '{g}' объявлен переключаемым, но в Chart.yaml "
                            f"зонтичного чарта у него НЕТ `condition: {g}.enabled` — "
                            f"`--set {g}.enabled=false` отрендерится и не сделает ничего")
    for sh in shards:
        for c in sh["components"]:
            if c not in gates:
                findings.append(f"шард '{sh['id']}' включает компонент '{c}', "
                                f"которого нет в gates — выключить его нечем")

    # 6. образы: карта gate→образ обязана называть образы, которые сборка знает
    make_images = set()
    mk = (DEPLOY / "Makefile").read_text(encoding="utf-8")
    m = re.search(r'^SERVICES := (.+)$', mk, re.M)
    if not m:
        findings.append("в deploy/Makefile не найден список SERVICES — "
                        "сверить карту образов не с чем; это отказ, а не «сошлось»")
    else:
        make_images = set(m.group(1).split())
    for img in list(manifest.get("core_images", [])) + list(manifest.get("gate_images", {}).values()):
        if make_images and img not in make_images:
            findings.append(f"манифест называет образ '{img}', которого нет в SERVICES "
                            f"сборки — шард попытается собрать несуществующее")
    for g in sorted(gates):
        if g.startswith("pg-"):
            continue
        if g not in manifest.get("gate_images", {}):
            findings.append(f"компонент '{g}' переключаемый, но образа за ним не закреплено — "
                            f"шард, который его включит, не соберёт его образ")

    # 7. имя, которым шард ОБЪЯВЛЯЕТ отсутствие сервиса, обязано быть тем именем,
    #    которое понимает гейт посадки. Он держит жёсткий список ожидаемых
    #    сервисов намеренно (не развернувшийся сервис обязан быть красным), и
    #    исключение принимает ТОЛЬКО по этому имени. Разъезд имён означал бы, что
    #    шард выключил компонент, а гейт посадки покраснел на его отсутствии — и
    #    покраснел бы верно.
    posture = DEPLOY / "scripts" / "assert-production-posture.sh"
    ptext = posture.read_text(encoding="utf-8")
    pm = re.search(r'^SERVICES="\n((?:.*\n)*?)"\s*$', ptext, re.M)
    if not pm:
        findings.append("в assert-production-posture.sh не найден список SERVICES — "
                        "сверить имена исключений не с чем; это отказ, а не «сошлось»")
    else:
        posture_names = {ln.split("|")[1] for ln in pm.group(1).splitlines()
                         if ln.strip() and "|" in ln}
        for g, img in sorted(manifest.get("gate_images", {}).items()):
            if img not in posture_names:
                findings.append(
                    f"компонент '{g}' объявляет себя как '{img}', но гейт посадки "
                    f"такого сервиса не знает — POSTURE_SKIP='{img}' его не исключит, "
                    f"и стенд шарда покраснет на законном отсутствии")

    stats = {
        "suites_tree": len(tree_suites),
        "suites_assigned": len([s for s in seen if s in tree_suites]),
        "collections_tree": tree_total,
        "collections_assigned": assigned_total,
        "shards": len(shards),
        "gates": len(gates),
        "per_suite": {s: len(tree[s]) for s in sorted(tree)},
    }
    return findings, stats


def report(root: pathlib.Path, manifest_path: pathlib.Path) -> int:
    findings, st = check(root, manifest_path)
    print("=== покрытие шардов (единица счёта — отслеживаемая git коллекция) ===")
    print(f"осмотрено: суит {st['suites_tree']}, коллекций {st['collections_tree']}, "
          f"шардов {st['shards']}, переключаемых компонентов {st['gates']}")
    print(f"назначено: суит {st['suites_assigned']}, коллекций {st['collections_assigned']}")
    for s, n in st["per_suite"].items():
        owner = next((sh["id"] for sh in json.loads(manifest_path.read_text())["shards"]
                      if s in sh["suites"]), "—")
        print(f"   {s:12s} {n:3d} коллекций → шард {owner}")
    if findings:
        print()
        for f in findings:
            print(f"НАХОДКА: {f}")
        print(f"\nFAIL: находок {len(findings)}")
        return 1
    print("\nOK: каждая коллекция дерева назначена ровно одному шарду; "
          f"сумма по шардам = {st['collections_assigned']} = коллекций в дереве")
    return 0


def _self_test() -> int:
    """Инъекция: четыре дефекта по одному ⇒ красный на каждом; законный близнец ⇒ зелёный."""
    import copy
    import tempfile

    base = json.loads(MANIFEST.read_text(encoding="utf-8"))
    ok = True

    def run(m: dict, label: str, want_red: bool) -> None:
        nonlocal ok
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as fh:
            json.dump(m, fh)
            p = pathlib.Path(fh.name)
        try:
            findings, _ = check(ROOT, p)
        finally:
            p.unlink(missing_ok=True)
        red = bool(findings)
        good = red == want_red
        ok = ok and good
        state = "КРАСНЫЙ" if red else "зелёный"
        want = "красного" if want_red else "зелёного"
        print(f"  [{'ok ' if good else 'FAIL'}] {label}: {state} (ждали {want})"
              + (f" — {findings[0]}" if red else ""))

    print("=== самопроверка гейта (инъекция в обе стороны) ===")
    run(base, "законный близнец: дерево как есть", want_red=False)

    # (а) шард потерял суиту целиком
    m = copy.deepcopy(base)
    m["shards"] = [s for s in m["shards"] if s["id"] != "edge"]
    run(m, "(а) суиты edge-шарда никем не взяты", want_red=True)

    # (б) суита задвоена между двумя шардами
    m = copy.deepcopy(base)
    m["shards"][0]["suites"] = m["shards"][0]["suites"] + ["geo"]
    run(m, "(б) суита geo задвоена", want_red=True)

    # (в) шард называет несуществующую суиту
    m = copy.deepcopy(base)
    m["shards"][0]["suites"] = m["shards"][0]["suites"] + ["dns"]
    run(m, "(в) шард берёт суиту, которой нет в дереве", want_red=True)

    # (г) компонент объявлен включённым, но выключить его нечем
    m = copy.deepcopy(base)
    m["shards"][0]["components"] = m["shards"][0]["components"] + ["kacho-iam"]
    run(m, "(г) компонент вне gates", want_red=True)

    # (д) предпосылка гейта: gates обязаны быть условны в Chart.yaml
    m = copy.deepcopy(base)
    m["gates"] = m["gates"] + ["kratos-selfservice-ui"]
    run(m, "(д) gate без condition в Chart.yaml", want_red=True)

    print("самопроверка:", "OK" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    if "--self-test" in sys.argv:
        sys.exit(_self_test())
    sys.exit(report(ROOT, MANIFEST))
