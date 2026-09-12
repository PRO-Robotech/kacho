#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Раздача пакетов с пробами по шардам юнит-прогона — ВЫВОДИТСЯ из дерева.

ПРЕДМЕТ
-------
Юниты гонялись одним шагом. Замер на завершённых прогонах ствола (прогоны
34719767904 · 34717244300 · 34718530951): задание `build · vet · gofmt ·
test -race` 1455…1539 с, из них шаг юнитов 1329…1399 с — 89…91 % времени на ОДИН
шаг. Распил делает временем задания время САМОГО БОЛЬШОГО шарда, а не сумму.

ПОЧЕМУ РАЗДАЧА ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ
-------------------------------------------
Выписанный перечень шардов устареет ровно тогда, когда заведут следующую службу
или каталог, — то есть в единственный момент, когда он и нужен. И устареет ТИХО:
пакет, не названный ни в одном шарде, не исполняется, а «ноль упавших» про него
не говорит ничего. Поэтому здесь нет ни одного имени шарда: есть ПРАВИЛО, по
которому область дерева становится шардом.

ПРАВИЛО (чистая функция пути, `area` ниже):

    services/<служба>/…  →  шард `<служба>`
    <первый сегмент>/…   →  шард `<первый сегмент>`

Следствия, каждое проверено самопробой:

  * заведут службу — у неё появится СВОЙ шард, без правки этого файла и без
    правки объявления процесса;
  * заведут каталог верхнего уровня — он тоже получит свой шард, а не уедет в
    сборный «прочее». Сборного шарда здесь нет НАМЕРЕННО: он и есть место, где
    новая область прячется от глаз, оставаясь формально покрытой;
  * пакет не может выпасть из раздачи by construction — у всякого пути есть
    первый сегмент. Это НЕ повод не считать: перепись ниже сверяет числа, потому
    что дефект бывает и в самом правиле.

ИМЯ ШАРДА НЕ НЕСЁТ ЧИСЛА, И ЭТО НЕ КОСМЕТИКА (kacho#2587)
---------------------------------------------------------
Имя задания есть имя обязательного контекста защиты ствола. Шард с именем вида
«1 из 8» при смене числа шардов перестаёт производить объявленный контекст, и
запирается КАЖДЫЙ запрос на слияние, включая тот, что состав меняет. Имя шарда —
имя ОБЛАСТИ дерева, поэтому живёт столько же, сколько сама область.

ПЕРЕПИСЬ — ЧИСЛАМИ, А НЕ ПОСТРОЕНИЕМ
------------------------------------
`--census` печатает «в дереве N · роздано M» и сверяет их машинно: множества
обязаны СОВПАСТЬ, а не просто сойтись размером (потеря плюс задвоение дают тот же
размер). Расхождение любой пары — отказ, и он называет пакеты поимённо.

Единица счёта названа рядом с числом: пакет, у которого `go list` видит
`TestGoFiles` либо `XTestGoFiles` при сборке по умолчанию, — то же множество,
которое исполняет `go test ./...`. Пакеты под признаком сборки (`integration`) в
него не входят и здесь не судятся: их гоняет `make test-integration`, у них свой
отбор и свой гейт достижимости.

Запуск:
  python3 .github/scripts/unit-shards.py --self-test
  python3 .github/scripts/unit-shards.py --census --out unit-shard-plan.json
  python3 .github/scripts/unit-shards.py --matrix >> "$GITHUB_OUTPUT"
  python3 .github/scripts/unit-shards.py --packages vpc
  python3 .github/scripts/unit-shards.py --ids
"""
from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

LIST_FORMAT = "{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}"
SERVICES_SEGMENT = "services"
ROOT_SHARD = "root"


class Unmeasured(Exception):
    """Измерение не сделано. Отличается от «пакетов ноль» — и это не педантизм:
    сорвавшийся `go list` отдаёт пустой список, а пустой список, принятый за
    ответ, дал бы зелёный прогон с нулём исполненных проб."""


def module_path(root: Path) -> str:
    try:
        p = subprocess.run(["go", "list", "-m"], cwd=root, capture_output=True,
                           text=True, timeout=300)
    except (OSError, subprocess.SubprocessError) as e:
        raise Unmeasured(f"`go list -m` не отработал: {e}") from e
    if p.returncode != 0:
        raise Unmeasured(f"`go list -m` вышел кодом {p.returncode}: {p.stderr.strip()[:400]}")
    out = p.stdout.strip().splitlines()
    if not out or not out[0].strip():
        raise Unmeasured("`go list -m` не назвал модульного пути — резать пути не по чему")
    return out[0].strip()


def tree_packages(root: Path) -> list[str]:
    """Пакеты с пробами — ТЕ ЖЕ, что исполнит `go test ./...`."""
    try:
        p = subprocess.run(["go", "list", "-f", LIST_FORMAT, "./..."], cwd=root,
                           capture_output=True, text=True, timeout=1800)
    except (OSError, subprocess.SubprocessError) as e:
        raise Unmeasured(f"`go list ./...` не отработал: {e}") from e
    if p.returncode != 0:
        raise Unmeasured(
            f"`go list ./...` вышел кодом {p.returncode} — состав пакетов НЕ ИЗМЕРЕН. "
            f"Это отказ, а не «нечего гонять»: {p.stderr.strip()[:400]}")
    pkgs = sorted({x.strip() for x in p.stdout.splitlines() if x.strip()})
    if not pkgs:
        raise Unmeasured(
            "пакетов с пробами найдено НОЛЬ — обход пуст. Пустая раздача дала бы "
            "зелёный прогон, в котором не исполнилось ни одной пробы")
    return pkgs


def index_areas(root: Path) -> tuple[list[str], int]:
    """Области дерева по ИНДЕКСУ git — тот же `area`, другой вход.

    Зачем второй вход. Имена шардов нужны держателю обязательных контекстов
    (`assert-required-contexts-match-jobs.py`), а он живёт в процессе по
    расписанию, где Go не поднимается вовсе. Спросить `go list` оттуда нельзя, и
    выписать имена рядом — значит завести второе место об одном предмете.

    ЕДИНИЦА СЧЁТА ДРУГАЯ, И ЭТО НАЗВАНО. `go list` видит пакеты, собираемые по
    умолчанию; индекс видит файлы. Пакет, чьи пробы целиком под признаком сборки
    (`integration`), в первый вход не попадает, а во второй попадает. Поэтому
    расхождение двух наборов — НАХОДКА переписи ниже, а не повод выбрать
    правдоподобный: на сегодняшнем дереве оба дают 13 областей.

    Фикстуры под `testdata/` исключены: это входы проб, а не пакеты дерева, и
    каталог с фикстурой завёл бы область, которой не производит ни один шард.
    """
    try:
        r = subprocess.run(["git", "-C", str(root), "ls-files", "*_test.go"],
                           capture_output=True, text=True, timeout=300)
    except (OSError, subprocess.SubprocessError) as e:
        raise Unmeasured(f"`git ls-files` не отработал: {e}") from e
    if r.returncode != 0:
        raise Unmeasured(f"`git ls-files` вышел кодом {r.returncode}: {r.stderr.strip()[:300]}")
    paths = [x.strip() for x in r.stdout.splitlines() if x.strip()]
    paths = [x for x in paths if "/testdata/" not in x and not x.startswith("testdata/")]
    if not paths:
        raise Unmeasured(
            "в индексе ноль файлов `*_test.go` — обход пуст, и это отказ, а не «проб нет»")
    return sorted({area(x, "") for x in paths}), len(paths)


def area(pkg: str, module: str) -> str:
    """Шард пакета. Чистая функция пути — самопроба подаёт свои строки."""
    rel = pkg[len(module):].lstrip("/") if pkg.startswith(module) else pkg
    segs = [s for s in rel.split("/") if s]
    if not segs:
        return ROOT_SHARD
    if segs[0] == SERVICES_SEGMENT and len(segs) > 1:
        return segs[1]
    return segs[0]


TEST_DECL_RE = re.compile(r"^func (?:Test|Fuzz)", re.M)


def order_weights(root: Path) -> dict[str, int]:
    """Вес области ДЛЯ ПОРЯДКА ОТПРАВКИ — число объявлений проб в её файлах.

    ЧТО ЭТОТ ВЕС РЕШАЕТ И ЧЕГО НЕ РЕШАЕТ. Он решает ТОЛЬКО порядок строк матрицы.
    Раздачи он не касается, полноты не касается, вердикта не касается: ошибись он
    вдвое — изменится очередь отправки, и ничего больше. Поэтому приблизительность
    здесь законна, а неточность не может стать потерей пакета.

    ЗАЧЕМ ВООБЩЕ ПОРЯДОК. Площадка создаёт задания матрицы в порядке строк и
    раздаёт ранеры по мере освобождения. Когда ранеров меньше, чем шардов, — а это
    измерено: в прогоне 34723597932 тринадцать шардов стали готовы одновременно и
    самый долгий получил ранер через 1058 с, — порядок определяет критический путь
    прогона целиком. Долгий шард, отправленный последним, добавляет к прогону всё
    время ожидания.

    ПОЧЕМУ ЧИСЛО ПРОБ, А НЕ ЧИСЛО ПАКЕТОВ. Число пакетов как предсказание
    ОПРОВЕРГНУТО замером: `internal` — 6 пакетов из 206 (3 %) и 1073 с из 1488 с
    критического пути (72 %), тогда как `vpc` — 40 пакетов и 87 с. Число
    объявлений проб ставит `internal` первым (2629 против 1499 у `vpc`), то есть
    отвечает на нужный вопрос. Идеальным предсказателем оно не является и здесь
    им быть не обязано — см. первый абзац.
    """
    try:
        r = subprocess.run(["git", "-C", str(root), "ls-files", "*_test.go"],
                           capture_output=True, text=True, timeout=300)
    except (OSError, subprocess.SubprocessError):
        return {}
    if r.returncode != 0:
        return {}
    out: dict[str, int] = {}
    for rel in r.stdout.splitlines():
        rel = rel.strip()
        if not rel or "/testdata/" in rel or rel.startswith("testdata/"):
            continue
        try:
            body = (root / rel).read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        out[area(rel, "")] = out.get(area(rel, ""), 0) + len(TEST_DECL_RE.findall(body))
    return out


def assign(pkgs: list[str], module: str) -> dict[str, list[str]]:
    out: dict[str, list[str]] = {}
    for pkg in pkgs:
        out.setdefault(area(pkg, module), []).append(pkg)
    return {k: sorted(v) for k, v in sorted(out.items())}


def census(tree: list[str], shards: dict[str, list[str]],
           index: list[str] | None = None) -> tuple[list[str], dict]:
    """Находки и числа. Сверяются МНОЖЕСТВА: потеря плюс задвоение дают тот же
    размер, и счёт по размеру объявил бы такую пару сошедшейся."""
    findings: list[str] = []
    tree_set = set(tree)
    handed: list[str] = []
    for pkgs in shards.values():
        handed.extend(pkgs)
    handed_set = set(handed)

    lost = sorted(tree_set - handed_set)
    extra = sorted(handed_set - tree_set)
    dup = sorted({p for p in handed if handed.count(p) > 1})

    if lost:
        findings.append(
            f"ПАКЕТЫ ВЫПАЛИ ИЗ ВСЕХ ШАРДОВ ({len(lost)}): {', '.join(lost[:8])}"
            f"{' …' if len(lost) > 8 else ''}. Пропуск не есть проход: их пробы не "
            f"исполнятся, а «ноль упавших» про них не скажет ничего")
    if extra:
        findings.append(
            f"В ШАРДАХ ЕСТЬ ПАКЕТЫ НЕ ИЗ ДЕРЕВА ({len(extra)}): "
            f"{', '.join(extra[:8])}. Раздача разошлась с обходом")
    if dup:
        findings.append(
            f"ПАКЕТ РОЗДАН ДВАЖДЫ ({len(dup)}): {', '.join(dup[:8])}. Время шардов "
            f"тратится на одно и то же, а вердиктов о пакете становится два")
    if not shards:
        findings.append("ШАРДОВ НОЛЬ — раздавать некуда, и это отказ")

    if index is not None:
        only_go = sorted(set(shards) - set(index))
        only_index = sorted(set(index) - set(shards))
        if only_go or only_index:
            findings.append(
                f"ДВА ВХОДА ДАЮТ РАЗНЫЕ ОБЛАСТИ: только у `go list` "
                f"{only_go or '—'}, только у индекса git {only_index or '—'}. "
                f"Имена шардов держателю обязательных контекстов выводятся из "
                f"ИНДЕКСА (Go в процессе по расписанию не поднимается), поэтому "
                f"расхождение даёт либо тихую дыру в защите ствола, либо "
                f"исключение, которому нечего исключать")

    numbers = {
        "tree": len(tree_set),
        "handed": len(handed),
        "handed_unique": len(handed_set),
        "shards": len(shards),
        "per_shard": {k: len(v) for k, v in shards.items()},
    }
    return findings, numbers


def plan_document(tree: list[str], shards: dict[str, list[str]], module: str) -> dict:
    return {
        "module": module,
        "tree_packages": sorted(tree),
        "shards": [{"id": sid, "packages": pkgs} for sid, pkgs in shards.items()],
    }


def matrix_document(shards: dict[str, list[str]],
                    weights: dict[str, int] | None = None) -> dict:
    """Строка матрицы несёт ТОЛЬКО `id` области — не перечень пакетов.

    Перечень шард выводит сам, тем же `--packages` и тем же правилом. Это ВТОРОЕ
    вычисление одного предмета, и оно оставлено сознательно, потому что его
    расхождение ОБНАРУЖИВАЕТСЯ, а не предполагается отсутствующим: сводный вердикт
    сверяет перечень дерева ИЗ ПЛАНА с объединением того, что шарды ДЕЙСТВИТЕЛЬНО
    гоняли (`assigned` в описях). Разъехались — красное с поимённым списком.

    Обратный порядок — передать перечень матрицей — экономит вычисление и теряет
    эту сверку: шард гонял бы ровно то, что ему сказали, и вопрос «а то же ли это,
    что в дереве» задавать было бы негде.

    ПОРЯДОК СТРОК — САМЫЙ ТЯЖЁЛЫЙ ПЕРВЫМ (см. `order_weights`). При нехватке
    ранеров порядок строк и есть критический путь прогона. Без веса порядок
    остаётся детерминированным — по идентификатору, — потому что недетерминизм
    матрицы сделал бы неповторимым и разбор прогона.
    """
    order = sorted(shards, key=lambda sid: (-(weights or {}).get(sid, 0), sid))
    return {"shard": [{"id": sid} for sid in order]}


# ─── самопроба ──────────────────────────────────────────────────────────────

MOD = "github.com/PRO-Robotech/kacho"
FIXTURE = [
    f"{MOD}/services/vpc/internal/repo",
    f"{MOD}/services/vpc/internal/usecase",
    f"{MOD}/services/compute/internal/domain",
    f"{MOD}/gateway/internal/allowlist",
    f"{MOD}/internal/repohygiene",
    f"{MOD}/tools/foreignclouds",
    f"{MOD}/pkg/api",
]


def _case(name: str, expect_red: bool, findings: list[str], needle: str | None,
          note: str = "") -> bool:
    red = bool(findings)
    ok = (red == expect_red) and (needle is None or any(needle in f for f in findings))
    print(f"  [{'OK ' if ok else 'ОТКАЗ'}] {name}: находок {len(findings)} "
          f"(ждали {'красное' if expect_red else 'зелёное'}){note}")
    if not ok:
        for f in findings:
            print("      · " + f[:200])
    return ok


def self_test() -> int:
    print("самопроба раздачи: инъекция в обе стороны по каждой оси")
    ok = True

    # Ось 1 — правило разносит по областям, и КОНТРОЛЬ обязан молчать.
    shards = assign(FIXTURE, MOD)
    findings, num = census(FIXTURE, shards)
    ok &= _case("контроль: законная раздача → зелено", False, findings, None,
                f", шардов {num['shards']}, роздано {num['handed']}")
    expected = {"vpc": 2, "compute": 1, "gateway": 1, "internal": 1, "tools": 1, "pkg": 1}
    got = num["per_shard"]
    same = got == expected
    print(f"  [{'OK ' if same else 'ОТКАЗ'}] правило разносит по областям: {got}")
    ok &= same

    # Ось 2 — ПАКЕТ ВЫПАЛ. Класс, ради которого распил и опасен.
    lost = {k: (v[1:] if k == "vpc" else v) for k, v in shards.items()}
    findings, _ = census(FIXTURE, lost)
    ok &= _case("пакет выпал из всех шардов → красно и назван поимённо", True,
                findings, "ВЫПАЛИ ИЗ ВСЕХ ШАРДОВ")
    named = any("services/vpc/internal/repo" in f for f in findings)
    print(f"  [{'OK ' if named else 'ОТКАЗ'}] выпавший пакет назван поимённо")
    ok &= named

    # Ось 2б — ЗАКОННЫЙ БЛИЗНЕЦ: тот же пакет, но розданный. Без него красное
    # первой инъекции нельзя отличить от гейта, краснеющего на всём.
    findings, _ = census(FIXTURE, shards)
    ok &= _case("тот же пакет роздан → зелено (близнец)", False, findings, None)

    # Ось 3 — ЗАДВОЕНИЕ. Размер множеств при потере+задвоении совпадает, поэтому
    # счёт по размеру объявил бы такую пару сошедшейся.
    dbl = {k: list(v) for k, v in shards.items()}
    dbl["compute"] = dbl["compute"] + [FIXTURE[0]]   # задвоили один
    dbl["tools"] = []                                # и потеряли другой
    findings, num2 = census(FIXTURE, dbl)
    ok &= _case("потеря и задвоение при СОВПАВШЕМ размере → красно", True, findings,
                "РОЗДАН ДВАЖДЫ", f", роздано {num2['handed']} при дереве {num2['tree']}")
    ok &= _case("та же пара названа и второй находкой (потеря)", True, findings,
                "ВЫПАЛИ ИЗ ВСЕХ ШАРДОВ")

    # Ось 4 — пакет не из дерева.
    alien = {k: list(v) for k, v in shards.items()}
    alien["vpc"] = alien["vpc"] + [f"{MOD}/services/vpc/internal/gone"]
    findings, _ = census(FIXTURE, alien)
    ok &= _case("пакет не из дерева → красно", True, findings, "НЕ ИЗ ДЕРЕВА")

    # Ось 5 — пустая раздача. Ноль шардов обязан быть отказом, а не идеалом.
    findings, _ = census(FIXTURE, {})
    ok &= _case("ноль шардов → красно", True, findings, "ШАРДОВ НОЛЬ")

    # Ось 6 — НОВАЯ область получает СВОЙ шард без правки этого файла.
    grown = FIXTURE + [f"{MOD}/services/iam/internal/repo", f"{MOD}/experiments/foo"]
    gs = assign(grown, MOD)
    new_ok = "iam" in gs and "experiments" in gs
    print(f"  [{'OK ' if new_ok else 'ОТКАЗ'}] новая служба и новый каталог дали свои "
          f"шарды: {sorted(gs)}")
    ok &= new_ok
    findings, _ = census(grown, gs)
    ok &= _case("выросшее дерево → зелено без правки правила", False, findings, None)

    # Ось 7 — ни одно имя шарда не несёт цифры (kacho#2587).
    digits = [s for s in gs if any(c.isdigit() for c in s)]
    print(f"  [{'OK ' if not digits else 'ОТКАЗ'}] имён с цифрой: {len(digits)}")
    ok &= not digits

    # Ось 8 — матрица несёт ровно идентификаторы областей: по строке на шард, без
    # чисел в значениях и без перечней.
    m = matrix_document(gs)
    rows = [r["id"] for r in m["shard"]]
    same = rows == sorted(gs) and all(set(r) == {"id"} for r in m["shard"])
    print(f"  [{'OK ' if same else 'ОТКАЗ'}] в матрице строк {len(rows)} при шардах "
          f"{len(gs)}, поля строки {sorted(set().union(*(set(r) for r in m['shard'])))}")
    ok &= same

    # Ось 9 — ДВА ВХОДА. Имена шардов держателю обязательных контекстов выводятся
    # из индекса git, а раздача — из `go list`. Расхождение обязано быть находкой:
    # иначе оно даёт либо тихую дыру в защите ствола, либо исключение, которому
    # нечего исключать, — и обе формы молчат.
    findings, _ = census(FIXTURE, shards, sorted(shards))
    ok &= _case("оба входа дали одни области → зелено", False, findings, None)
    findings, _ = census(FIXTURE, shards, sorted(shards) + ["iam"])
    ok &= _case("индекс знает область, которой нет у `go list` → красно", True,
                findings, "ДВА ВХОДА ДАЮТ РАЗНЫЕ ОБЛАСТИ")
    findings, _ = census(FIXTURE, shards, sorted(set(shards) - {"pkg"}))
    ok &= _case("у `go list` есть область, которой не знает индекс → красно", True,
                findings, "ДВА ВХОДА ДАЮТ РАЗНЫЕ ОБЛАСТИ")

    # Ось 10 — `area` читает ПУТЬ, а не модульный префикс: вход из индекса подаёт
    # путь от корня репозитория, вход из `go list` — полный путь импорта.
    same = area("services/vpc/internal/repo", "") == area(
        f"{MOD}/services/vpc/internal/repo", MOD) == "vpc"
    print(f"  [{'OK ' if same else 'ОТКАЗ'}] одно правило на оба входа: путь из "
          f"индекса и путь импорта дают одну область")
    ok &= same

    # Ось 11 — ПОРЯДОК ОТПРАВКИ. Вес решает только очередь: множество шардов и
    # раздача от него не зависят ни в одном случае.
    w = {"pkg": 900, "vpc": 10, "gateway": 5}
    rows = [r["id"] for r in matrix_document(shards, w)["shard"]]
    first_ok = rows[0] == "pkg"
    print(f"  [{'OK ' if first_ok else 'ОТКАЗ'}] самый тяжёлый по весу отправляется "
          f"первым: {rows}")
    ok &= first_ok
    same_set = sorted(rows) == sorted(shards)
    print(f"  [{'OK ' if same_set else 'ОТКАЗ'}] вес НЕ меняет множество шардов "
          f"({len(rows)} против {len(shards)})")
    ok &= same_set
    no_w = [r["id"] for r in matrix_document(shards, {})["shard"]]
    det = no_w == sorted(shards)
    print(f"  [{'OK ' if det else 'ОТКАЗ'}] без веса порядок детерминирован по "
          f"идентификатору: {no_w}")
    ok &= det
    # Раздача обязана остаться той же при любом весе — иначе вес стал бы решать
    # полноту, а его приблизительность законна только потому, что не решает.
    findings_w, num_w = census(FIXTURE, shards, sorted(shards))
    same_handed = num_w["handed"] == len(FIXTURE) and not findings_w
    print(f"  [{'OK ' if same_handed else 'ОТКАЗ'}] раздача при любом весе та же: "
          f"роздано {num_w['handed']} при дереве {len(FIXTURE)}")
    ok &= same_handed

    print("самопроба:", "ПРОЙДЕНА" if ok else "ПРОВАЛЕНА")
    return 0 if ok else 1


# ─── точка входа ────────────────────────────────────────────────────────────

def main() -> int:
    ap = argparse.ArgumentParser(description="раздача пакетов с пробами по шардам")
    ap.add_argument("--self-test", action="store_true")
    ap.add_argument("--census", action="store_true",
                    help="перепись полноты: в дереве N · роздано M; расхождение — отказ")
    ap.add_argument("--matrix", action="store_true",
                    help="строка `matrix=<json>` для $GITHUB_OUTPUT")
    ap.add_argument("--ids", action="store_true", help="идентификаторы шардов, по одному в строке")
    ap.add_argument("--from-index", action="store_true",
                    help="выводить области из ИНДЕКСА git, а не из `go list` — для "
                         "читателя, у которого Go нет (процесс по расписанию)")
    ap.add_argument("--packages", metavar="SHARD",
                    help="пакеты названного шарда, по одному в строке")
    ap.add_argument("--out", metavar="PATH", help="куда записать план (JSON)")
    ap.add_argument("--root", default=None)
    args = ap.parse_args()

    if args.self_test:
        return self_test()

    root = Path(args.root) if args.root else Path(__file__).resolve().parents[2]

    # Вход из индекса не требует Go и потому обслуживается ДО всего остального:
    # позвавший его читатель Go не поднимал и не обязан.
    if args.from_index:
        if not args.ids:
            sys.stderr.write(
                "ОТКАЗ: --from-index имеет смысл только с --ids: другие режимы "
                "требуют перечня ПАКЕТОВ, а индекс знает файлы\n")
            return 2
        try:
            ids, files = index_areas(root)
        except Unmeasured as e:
            sys.stderr.write(f"ОТКАЗ: {e}\n")
            return 2
        sys.stderr.write(f"осмотрено файлов `*_test.go` в индексе: {files}\n")
        print("\n".join(ids))
        return 0

    try:
        module = module_path(root)
        tree = tree_packages(root)
        index, _files = index_areas(root)
    except Unmeasured as e:
        sys.stderr.write(f"ОТКАЗ: {e}\n")
        return 2

    shards = assign(tree, module)
    findings, num = census(tree, shards, index)
    weights = order_weights(root)

    if args.packages:
        if args.packages not in shards:
            sys.stderr.write(
                f"ОТКАЗ: шарда `{args.packages}` в раздаче нет. Есть: "
                f"{' '.join(sorted(shards))}\n")
            return 2
        # Раздача судится ДО выдачи: шард, уехавший по непроверенной раздаче,
        # уносит с собой и её дефект.
        if findings:
            for f in findings:
                sys.stderr.write(f"ОТКАЗ: {f}\n")
            return 1
        print("\n".join(shards[args.packages]))
        return 0

    if args.ids:
        if findings:
            for f in findings:
                sys.stderr.write(f"ОТКАЗ: {f}\n")
            return 1
        print("\n".join(shards))
        return 0

    if args.matrix:
        if findings:
            for f in findings:
                sys.stderr.write(f"ОТКАЗ: {f}\n")
            return 1
        print("matrix=" + json.dumps(matrix_document(shards, weights),
                                     ensure_ascii=False))
        return 0

    # Перепись — умолчание: вызов без режима обязан ЧТО-ТО измерить, а не молчать.
    print(f"ПЕРЕПИСЬ ПОЛНОТЫ (единица счёта — пакет, у которого `go list` видит "
          f"TestGoFiles либо XTestGoFiles при сборке по умолчанию)")
    print(f"  пакетов с пробами в дереве: {num['tree']}")
    print(f"  роздано по шардам:          {num['handed']} (различных {num['handed_unique']})")
    print(f"  шардов:                     {num['shards']}")
    print(f"  порядок отправки — самый тяжёлый первым; вес = объявлений проб "
          f"(решает ТОЛЬКО очередь, не раздачу)")
    for row in matrix_document(shards, weights)["shard"]:
        sid = row["id"]
        print(f"    {sid:14} пакетов {num['per_shard'][sid]:4} · "
              f"объявлений проб {weights.get(sid, 0):5}")
    if args.out:
        Path(args.out).write_text(
            json.dumps(plan_document(tree, shards, module), ensure_ascii=False,
                       indent=1) + "\n", encoding="utf-8")
        print(f"  план записан: {args.out}")
    if findings:
        print(f"НАХОДОК {len(findings)}:")
        for f in findings:
            print("  · " + f)
        return 1
    print("роздано = дерево: ни одного пакета вне шардов, ни одного дважды")
    return 0


if __name__ == "__main__":
    sys.exit(main())
