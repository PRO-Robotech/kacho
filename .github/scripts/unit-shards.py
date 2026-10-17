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

РАСПИЛ ПАКЕТА ПО ПРОБАМ (kacho#3277)
------------------------------------
Область — не предел распила. Прогон 38987199455: стену юнитов (1128–1228 с) держал
ОДИН шард `internal`, а в нём два пакета — `internal/repohygiene` 1189 с,
`internal/repohygiene/artifactgates` 538 с — при прочих шардах не дольше 276 с.
Ниже пакета раздача по областям не опускается, поэтому пакеты из `SPLIT_BY_TEST`
режутся на части ПО ПРОБАМ верхнего уровня, каждая часть — свой шард.

`-race` с них НЕ снят, и это решено замером, а не чтением (#2628): прогон
`go test -json -trace` обоих пакетов — из 2638 проб `repohygiene` под
`t.Parallel()` 2636, из 222 проб `artifactgates` — 221; горутин, порождённых кодом
`repohygiene`, 28 (в их числе прод-обход `kanameResidueWorldOfTree`), `os/exec` —
7821, и они пишут в буферы проб. Пробы идут одновременно над общим состоянием пакета
(мемоизация корпуса дерева) — это и есть предмет `-race`.

Часть — чистая функция ИМЕНИ пробы (`part_index`: sha256 имени по модулю N): проба,
заведённая завтра, не перетасовывает остальные, раздача повторима на любой машине.
Перечень проб даёт сам тестовый бинарь (`go test -list`), а не образец по тексту:
`func TestX` стоит и в строковых литералах проб инъекции. Опись части называет
пробы с терминальным событием, свод сверяет «каждая проба ровно в одной части» как
МНОЖЕСТВА.

ВЫБОР ЗАТРОНУТОГО ПО ПАКЕТАМ (kacho#3277)
----------------------------------------
На запросе в ветку эпика или волны (база не из `full_bases` карты
`.github/ci-paths.json`) план отбирает пакеты, которые изменение способно
покрасить: пакет правленого `.go` и всякий пакет, в тестовый бинарь которого он
входит (`go list -test`, `Deps` у `<пакет>.test`); правка `_test.go` — только свой
пакет; не-Go файл внутри каталога пакета (встроенное, testdata) — ближайший
объемлющий пакет и его обратные зависимые. Всё прочее — полный набор: `go.mod`,
`go.sum`, путь вне каталогов пакетов (`.github/`, `docs/`, `scripts/`), `.go` вне
пакетов модуля, неполный перечень путей запроса, пустой выбор.

ЦЕНА НАЗВАНА: гейты дерева (`internal/repohygiene`) читают файлы мимо графа
импорта, и на целевом прогоне правка `services/vpc` их не гоняет. Весь их охват —
полный прогон: запрос эпика в `main`, push, ручной запуск.

Запуск:
  python3 .github/scripts/unit-shards.py --self-test
  python3 .github/scripts/unit-shards.py --census --out unit-shard-plan.json
  python3 .github/scripts/unit-shards.py --matrix --plan unit-shard-plan.json >> "$GITHUB_OUTPUT"
  python3 .github/scripts/unit-shards.py --shard-spec internal-repohygiene-a --plan unit-shard-plan.json
  python3 .github/scripts/unit-shards.py --select-files - < пути   (выбор по путям, без события)
  python3 .github/scripts/unit-shards.py --packages vpc
  python3 .github/scripts/unit-shards.py --ids
"""
from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import os
import re
import shlex
import subprocess
import sys
from pathlib import Path

LIST_FORMAT = "{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}"
SERVICES_SEGMENT = "services"
ROOT_SHARD = "root"

# Пакеты, распиленные ПО ПРОБАМ: путь от корня модуля → число частей. Число частей —
# решение о стене, а не о полноте: ошибись оно — изменится длина шарда, и только.
# Запись без пакета с пробами — находка переписи (запись пережила предмет).
# Основание чисел: прогон 38987199455 — 1189 с и 538 с при цели ≈ 276 с на шард;
# неравномерность раздачи по имени (замер Elapsed проб на этом дереве) — до 1,4
# от среднего у `repohygiene`; у `artifactgates` одна проба держит ≈ 40 % пакета,
# и делить его мельче трёх частей не даёт ничего.
SPLIT_BY_TEST: dict[str, int] = {
    "internal/repohygiene": 8,
    "internal/repohygiene/artifactgates": 3,
}
PART_LETTERS = "abcdefghijklmnopqrstuvwxyz"
# Флаги перечня — те же, что у прогона (`-race`): кэш сборки общий, и перечень не
# платит второй сборки. `-count=1` обязателен: TestMain гейтов дерева отказывает
# кэшируемому прогону (corelib treecorpus.CachedVerdictRefusal), и `-list` тоже.
LIST_FLAGS = ["-race", "-count=1"]
TEST_NAME_RE = re.compile(r"(?:Test|Fuzz|Example)\w*")
# Разделитель `Deps` — запятая: в записях вариантов пакета стоит пробел
# («p [p.test]»), и разбор по пробелу резал бы их пополам.
GO_LIST_TEST_FORMAT = "{{.ImportPath}}\t{{.Dir}}\t{{join .Deps \",\"}}"
FULL_FILE_NAMES = {"go.mod", "go.sum", "go.work", "go.work.sum"}


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
    ids: set[str] = set()
    for x in paths:
        ids.update(shard_ids_for(x.rsplit("/", 1)[0] if "/" in x else "", ""))
    return sorted(ids), len(paths)


def area(pkg: str, module: str) -> str:
    """Шард пакета. Чистая функция пути — самопроба подаёт свои строки."""
    rel = pkg[len(module):].lstrip("/") if pkg.startswith(module) else pkg
    segs = [s for s in rel.split("/") if s]
    if not segs:
        return ROOT_SHARD
    if segs[0] == SERVICES_SEGMENT and len(segs) > 1:
        return segs[1]
    return segs[0]


def rel_of(pkg: str, module: str) -> str:
    return pkg[len(module):].lstrip("/") if module and pkg.startswith(module) else pkg


def part_ids(rel: str, n: int) -> list[str]:
    """Имена частей распиленного пакета. Без цифр (kacho#2587): часть — буква."""
    base = rel.replace("/", "-")
    return [f"{base}-{PART_LETTERS[i]}" for i in range(n)]


def part_index(test: str, n: int) -> int:
    """Часть пробы — чистая функция ИМЕНИ: от соседей и порядка не зависит."""
    return int(hashlib.sha256(test.encode("utf-8")).hexdigest()[:12], 16) % n


def shard_ids_for(pkg: str, module: str) -> list[str]:
    """Шарды пакета: области — один, распиленному — все его части."""
    rel = rel_of(pkg, module)
    if rel in SPLIT_BY_TEST:
        return part_ids(rel, SPLIT_BY_TEST[rel])
    return [area(pkg, module)]


TEST_DECL_RE = re.compile(r"^func ((?:Test|Fuzz)\w*)", re.M)


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
        rdir = rel.rsplit("/", 1)[0] if "/" in rel else ""
        ids = shard_ids_for(rdir, "")
        for name in TEST_DECL_RE.findall(body):
            sid = ids[part_index(name, len(ids))] if len(ids) > 1 else ids[0]
            out[sid] = out.get(sid, 0) + 1
    return out


def assign(pkgs: list[str], module: str) -> dict[str, list[str]]:
    """Шард → пакеты. Распиленный пакет стоит в КАЖДОЙ своей части: часть гоняет
    его пробы своей доли (`split_tests`), а не пакет целиком."""
    out: dict[str, list[str]] = {}
    for pkg in pkgs:
        for sid in shard_ids_for(pkg, module):
            out.setdefault(sid, []).append(pkg)
    return {k: sorted(v) for k, v in sorted(out.items())}


def split_parts(shards: dict[str, list[str]], module: str) -> dict[str, str]:
    """Часть → распиленный пакет, по раздаче."""
    out = {}
    for pkg in {p for v in shards.values() for p in v}:
        rel = rel_of(pkg, module)
        if rel in SPLIT_BY_TEST:
            for sid in part_ids(rel, SPLIT_BY_TEST[rel]):
                out[sid] = pkg
    return out


def split_tests(tests: list[str], n: int) -> list[list[str]]:
    parts: list[list[str]] = [[] for _ in range(n)]
    for t in sorted(set(tests)):
        parts[part_index(t, n)].append(t)
    return parts


def census(tree: list[str], shards: dict[str, list[str]],
           index: list[str] | None = None, module: str = "") -> tuple[list[str], dict]:
    """Находки и числа. Сверяются МНОЖЕСТВА: потеря плюс задвоение дают тот же
    размер, и счёт по размеру объявил бы такую пару сошедшейся.

    Распиленный пакет законно стоит в каждой СВОЕЙ части и больше нигде: задвоение
    судится по шардам, а не по счёту вхождений."""
    findings: list[str] = []
    tree_set = set(tree)
    handed: list[str] = []
    for pkgs in shards.values():
        handed.extend(pkgs)
    handed_set = set(handed)

    lost = sorted(tree_set - handed_set)
    extra = sorted(handed_set - tree_set)
    owners: dict[str, list[str]] = {}
    for sid, pkgs in shards.items():
        for p in pkgs:
            owners.setdefault(p, []).append(sid)
    dup = sorted(p for p, sids in owners.items()
                 if sorted(sids) != sorted(shard_ids_for(p, module)))
    for sid, pkgs in shards.items():
        rel_split = [p for p in pkgs if rel_of(p, module) in SPLIT_BY_TEST]
        if rel_split and len(pkgs) != 1:
            findings.append(
                f"ЧАСТЬ `{sid}` НЕСЁТ ЧУЖОЙ ПАКЕТ: отбор `-run` части действует на "
                f"все пакеты вызова, и пробы соседа исполнились бы урезанными "
                f"({', '.join(pkgs[:4])})")
    stale = sorted(rel for rel in SPLIT_BY_TEST
                   if not any(rel_of(p, module) == rel for p in tree_set))
    if stale and module:
        findings.append(
            f"РАСПИЛ БЕЗ ПРЕДМЕТА ({len(stale)}): {', '.join(stale)} — в дереве нет "
            f"такого пакета с пробами. Запись пережила свой пакет; сними её")
    bad_n = sorted(f"{rel}={n}" for rel, n in SPLIT_BY_TEST.items()
                   if not 2 <= n <= len(PART_LETTERS))
    if bad_n:
        findings.append(f"ЧИСЛО ЧАСТЕЙ ВНЕ [2, {len(PART_LETTERS)}]: {', '.join(bad_n)}")

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
            f"ПАКЕТ РОЗДАН ДВАЖДЫ либо не своим шардам ({len(dup)}): "
            f"{', '.join(dup[:8])}. Время шардов тратится на одно и то же, а "
            f"вердиктов о пакете становится два; распиленный стоит ровно в своих частях")
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


# ─── перечень проб распиленного пакета ──────────────────────────────────────

def parse_test_list(out: str, pkg: str) -> list[str]:
    """Разбор вывода `go test -list '.*'`. СТРОГИЙ: строка, не являющаяся ни именем
    пробы, ни итогом `ok`, — отказ, а не пропуск. Тихо отброшенная строка была бы
    пробой, которая не исполнится ни в одной части, — и свод её не хватился бы:
    он сверяет исполненное с ЭТИМ перечнем."""
    names: list[str] = []
    ok_seen = False
    for ln in out.splitlines():
        t = ln.strip()
        if not t:
            continue
        if re.fullmatch(r"Benchmark\w*", t):
            continue  # замеры отбирает `-bench`, юнит-прогон их не исполняет
        if TEST_NAME_RE.fullmatch(t):
            names.append(t)
            continue
        if re.match(r"ok\s", t):
            ok_seen = True
            continue
        raise Unmeasured(f"`go test -list` по {pkg}: строка — не имя пробы и не итог: "
                         f"«{t[:160]}». Перечень не разобран, раздавать по нему нельзя")
    if not ok_seen:
        raise Unmeasured(f"`go test -list` по {pkg}: нет итоговой строки `ok` — перечень "
                         f"оборван")
    if not names:
        raise Unmeasured(f"`go test -list` по {pkg}: проб НОЛЬ — делить нечего, и это "
                         f"отказ, а не «пусто»")
    return sorted(set(names))


def list_tests(root: Path, pkg: str) -> list[str]:
    try:
        p = subprocess.run(["go", "test", *LIST_FLAGS, "-list", ".*", pkg], cwd=root,
                           capture_output=True, text=True, timeout=1800)
    except (OSError, subprocess.SubprocessError) as e:
        raise Unmeasured(f"`go test -list` по {pkg} не отработал: {e}") from e
    if p.returncode != 0:
        raise Unmeasured(f"`go test -list` по {pkg} вышел кодом {p.returncode}: "
                         f"{(p.stderr or p.stdout).strip()[:400]}")
    return parse_test_list(p.stdout, pkg)


# ─── выбор затронутого по пакетам ───────────────────────────────────────────

def go_graph(root: Path) -> tuple[dict[str, str], dict[str, set[str]]]:
    """(каталог от корня → путь импорта — для КАЖДОГО пакета модуля;
        пакет с пробами → пакеты, входящие в его тестовый бинарь).

    Второе — `Deps` записи `<пакет>.test` из `go list -test`: в неё входят и
    импорты проб (`TestImports`, `XTestImports`) со всем их замыканием. Поле
    `Deps` самого пакета этого не знает — проба, импортирующая чужой пакет, его
    правку не видела бы."""
    try:
        p = subprocess.run(["go", "list", "-test", "-f", GO_LIST_TEST_FORMAT, "./..."],
                           cwd=root, capture_output=True, text=True, timeout=1800)
    except (OSError, subprocess.SubprocessError) as e:
        raise Unmeasured(f"`go list -test` не отработал: {e}") from e
    if p.returncode != 0:
        raise Unmeasured(f"`go list -test` вышел кодом {p.returncode}: "
                         f"{p.stderr.strip()[:400]}")
    return parse_go_graph(p.stdout, str(root))


def parse_go_graph(out: str, root: str) -> tuple[dict[str, str], dict[str, set[str]]]:
    """Разбор вывода `go list -test -f GO_LIST_TEST_FORMAT` — чистая функция."""
    dirs: dict[str, str] = {}
    deps: dict[str, set[str]] = {}
    for ln in out.splitlines():
        cols = ln.split("\t")
        if len(cols) != 3:
            continue
        imp, d, dep = cols
        if imp.endswith(".test") and " " not in imp:
            deps[imp[:-len(".test")]] = {x.split(" [", 1)[0] for x in dep.split(",") if x}
        elif " [" not in imp:
            rel = os.path.relpath(d, root)
            dirs["" if rel == "." else rel.replace(os.sep, "/")] = imp
    if not dirs:
        raise Unmeasured("`go list -test` назвал ноль пакетов — граф не получен")
    return dirs, deps


def select_packages(files: list[str], dirs: dict[str, str], deps: dict[str, set[str]],
                    tree: list[str]) -> tuple[list[str] | None, str]:
    """Пути изменения → пакеты с пробами, которые оно способно покрасить.
    `None` — полный набор, и основание называется. Чистая функция."""
    files = sorted({f for f in files if f})
    if not files:
        return None, "путей изменения ноль — выбирать не из чего, судится всё"
    touched: set[str] = set()
    own_tests: set[str] = set()
    for f in files:
        name = f.rsplit("/", 1)[-1]
        d = f.rsplit("/", 1)[0] if "/" in f else ""
        if name in FULL_FILE_NAMES:
            return None, f"тронут {f}: граф зависимостей сменился — судится всё"
        if f.endswith(".go") and "/testdata/" not in "/" + f:
            if d not in dirs:
                return None, (f"{f}: `.go` вне пакетов модуля (снятый пакет, другой "
                              f"модуль, исключённый сборкой) — судится всё")
            (own_tests if name.endswith("_test.go") else touched).add(dirs[d])
            continue
        anc = d
        while anc and anc not in dirs:
            anc = anc.rsplit("/", 1)[0] if "/" in anc else ""
        if not anc:
            return None, (f"{f}: путь вне каталогов пакетов — его читают гейты дерева "
                          f"мимо графа импорта, судится всё")
        touched.add(dirs[anc])
    sel = sorted(p for p in set(tree)
                 if p in own_tests or p in touched or touched & deps.get(p, set()))
    if not sel:
        return None, "целевой выбор пуст — судится всё"
    return sel, (f"целевой выбор: путей {len(files)}, пакетов с пробами {len(sel)} "
                 f"из {len(set(tree))}")


def _affected():
    spec = importlib.util.spec_from_file_location(
        "affected", Path(__file__).resolve().with_name("affected.py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)  # type: ignore[union-attr]
    return mod


def event_files() -> tuple[list[str] | None, str]:
    """Пути изменения для целевого выбора — или `None` (полный набор) с основанием.

    Целевой — запрос (`pull_request`, черновик или нет) в базу НЕ из `full_bases`
    карты `.github/ci-paths.json`, то есть в ветку эпика или волны. Всё, что не
    удалось узнать, ведёт к ПОЛНОМУ набору, а не к пустому: «не смог спросить» не
    значит «трогать нечего»."""
    try:
        aff = _affected()
        m = aff.load_map()
    except Exception as e:  # noqa: BLE001 — любой отказ карты ведёт к полному
        return None, f"карта выбора не прочитана ({e}) — судится всё"
    event = os.environ.get("EVENT", "")
    base = os.environ.get("BASE", "")
    if event != "pull_request":
        return None, f"событие {event or '—'}: полный набор"
    if base in m.full_bases:
        return None, f"запрос в {base}: полный набор"
    try:
        files = aff.pr_files(os.environ["REPO"], os.environ["PR"])
    except (KeyError, OSError, subprocess.CalledProcessError) as e:
        return None, f"пути запроса не получены ({e}) — судится всё"
    if files is None:
        return None, "путей запроса не меньше предела площадки — перечень неполон, судится всё"
    return files, f"запрос в {base}: выбор по пакетам"


def plan_document(tree: list[str], shards: dict[str, list[str]], module: str,
                  selected: list[str] | None = None, reason: str = "полный набор",
                  listing: dict[str, list[str]] | None = None) -> dict:
    """План: дерево, ВЫБРАННОЕ, распил и шарды, которым есть что гонять.

    Часть распиленного пакета несёт свои пробы (`tests`); часть без проб в план не
    идёт — гонять ей нечего, а свод сверяет пробы пакета по всем частям сразу."""
    sel = sorted(set(tree) if selected is None else set(selected))
    sel_set = set(sel)
    listing = listing or {}
    parts = split_parts(shards, module)
    rows = []
    for sid, pkgs in shards.items():
        keep = [p for p in pkgs if p in sel_set]
        if not keep:
            continue
        row: dict = {"id": sid, "packages": keep}
        pkg = parts.get(sid)
        if pkg is not None:
            ids = part_ids(rel_of(pkg, module), SPLIT_BY_TEST[rel_of(pkg, module)])
            row["tests"] = split_tests(listing.get(pkg, []), len(ids))[ids.index(sid)]
            if not row["tests"]:
                continue
        rows.append(row)
    return {
        "module": module,
        "tree_packages": sorted(tree),
        "selection": {"mode": "full" if selected is None else "targeted",
                      "reason": reason, "packages": sel},
        "split": {pkg: {"parts": part_ids(rel_of(pkg, module),
                                          SPLIT_BY_TEST[rel_of(pkg, module)]),
                        "tests": listing.get(pkg, [])}
                  for pkg in sorted(set(parts.values())) if pkg in sel_set},
        "shards": rows,
    }


def run_regex(tests: list[str]) -> str:
    return "^(" + "|".join(re.escape(t) for t in sorted(tests)) + ")$"


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


def _self_test_areas() -> bool:
    """Оси раздачи по ОБЛАСТЯМ — без распила (его оси ниже, своей функцией)."""
    ok = True

    # Ось 1 — правило разносит по областям, и КОНТРОЛЬ обязан молчать.
    shards = assign(FIXTURE, MOD)
    findings, num = census(FIXTURE, shards, module=MOD)
    ok &= _case("контроль: законная раздача → зелено", False, findings, None,
                f", шардов {num['shards']}, роздано {num['handed']}")
    expected = {"vpc": 2, "compute": 1, "gateway": 1, "internal": 1, "tools": 1, "pkg": 1}
    got = num["per_shard"]
    same = got == expected
    print(f"  [{'OK ' if same else 'ОТКАЗ'}] правило разносит по областям: {got}")
    ok &= same

    # Ось 2 — ПАКЕТ ВЫПАЛ. Класс, ради которого распил и опасен.
    lost = {k: (v[1:] if k == "vpc" else v) for k, v in shards.items()}
    findings, _ = census(FIXTURE, lost, module=MOD)
    ok &= _case("пакет выпал из всех шардов → красно и назван поимённо", True,
                findings, "ВЫПАЛИ ИЗ ВСЕХ ШАРДОВ")
    named = any("services/vpc/internal/repo" in f for f in findings)
    print(f"  [{'OK ' if named else 'ОТКАЗ'}] выпавший пакет назван поимённо")
    ok &= named

    # Ось 2б — ЗАКОННЫЙ БЛИЗНЕЦ: тот же пакет, но розданный. Без него красное
    # первой инъекции нельзя отличить от гейта, краснеющего на всём.
    findings, _ = census(FIXTURE, shards, module=MOD)
    ok &= _case("тот же пакет роздан → зелено (близнец)", False, findings, None)

    # Ось 3 — ЗАДВОЕНИЕ. Размер множеств при потере+задвоении совпадает, поэтому
    # счёт по размеру объявил бы такую пару сошедшейся.
    dbl = {k: list(v) for k, v in shards.items()}
    dbl["compute"] = dbl["compute"] + [FIXTURE[0]]   # задвоили один
    dbl["tools"] = []                                # и потеряли другой
    findings, num2 = census(FIXTURE, dbl, module=MOD)
    ok &= _case("потеря и задвоение при СОВПАВШЕМ размере → красно", True, findings,
                "РОЗДАН ДВАЖДЫ", f", роздано {num2['handed']} при дереве {num2['tree']}")
    ok &= _case("та же пара названа и второй находкой (потеря)", True, findings,
                "ВЫПАЛИ ИЗ ВСЕХ ШАРДОВ")

    # Ось 4 — пакет не из дерева.
    alien = {k: list(v) for k, v in shards.items()}
    alien["vpc"] = alien["vpc"] + [f"{MOD}/services/vpc/internal/gone"]
    findings, _ = census(FIXTURE, alien, module=MOD)
    ok &= _case("пакет не из дерева → красно", True, findings, "НЕ ИЗ ДЕРЕВА")

    # Ось 5 — пустая раздача. Ноль шардов обязан быть отказом, а не идеалом.
    findings, _ = census(FIXTURE, {}, module=MOD)
    ok &= _case("ноль шардов → красно", True, findings, "ШАРДОВ НОЛЬ")

    # Ось 6 — НОВАЯ область получает СВОЙ шард без правки этого файла.
    grown = FIXTURE + [f"{MOD}/services/iam/internal/repo", f"{MOD}/experiments/foo"]
    gs = assign(grown, MOD)
    new_ok = "iam" in gs and "experiments" in gs
    print(f"  [{'OK ' if new_ok else 'ОТКАЗ'}] новая служба и новый каталог дали свои "
          f"шарды: {sorted(gs)}")
    ok &= new_ok
    findings, _ = census(grown, gs, module=MOD)
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
    findings, _ = census(FIXTURE, shards, sorted(shards), module=MOD)
    ok &= _case("оба входа дали одни области → зелено", False, findings, None)
    findings, _ = census(FIXTURE, shards, sorted(shards) + ["iam"], module=MOD)
    ok &= _case("индекс знает область, которой нет у `go list` → красно", True,
                findings, "ДВА ВХОДА ДАЮТ РАЗНЫЕ ОБЛАСТИ")
    findings, _ = census(FIXTURE, shards, sorted(set(shards) - {"pkg"}), module=MOD)
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
    findings_w, num_w = census(FIXTURE, shards, sorted(shards), module=MOD)
    same_handed = num_w["handed"] == len(FIXTURE) and not findings_w
    print(f"  [{'OK ' if same_handed else 'ОТКАЗ'}] раздача при любом весе та же: "
          f"роздано {num_w['handed']} при дереве {len(FIXTURE)}")
    ok &= same_handed

    return ok


# ─── точка входа ────────────────────────────────────────────────────────────

def _check(name: str, cond: bool, note: str = "") -> bool:
    print(f"  [{'OK ' if cond else 'ОТКАЗ'}] {name}{note}")
    return cond


def _self_test_split() -> bool:
    """Оси распила по пробам. Распил подменяется на время осей — оси областей выше
    идут БЕЗ него, и их ожидания от него не зависят."""
    global SPLIT_BY_TEST
    ok = True
    rh = f"{MOD}/internal/repohygiene"
    tree = FIXTURE
    saved = SPLIT_BY_TEST
    try:
        SPLIT_BY_TEST = {"internal/repohygiene": 3}
        shards = assign(tree, MOD)
        parts = sorted(s for s in shards if s.startswith("internal-repohygiene-"))
        findings, _ = census(tree, shards, module=MOD)
        ok &= _case("контроль распила: пакет ровно в своих частях → зелено", False,
                    findings, None, f", частей {len(parts)}")
        ok &= _check("распиленный пакет не остался в шарде области",
                     rh not in shards.get("internal", []))
        ok &= _check("имена частей без цифр (kacho#2587)",
                     not any(DIGIT_IN_NAME.search(p) for p in parts), f": {parts}")

        lost = {k: list(v) for k, v in shards.items()}
        lost["internal-repohygiene-b"] = []
        findings, _ = census(tree, lost, module=MOD)
        ok &= _case("часть потеряла распиленный пакет → красно", True, findings,
                    "не своим шардам")

        alien = {k: list(v) for k, v in shards.items()}
        alien["internal-repohygiene-a"] = alien["internal-repohygiene-a"] + [FIXTURE[0]]
        findings, _ = census(tree, alien, module=MOD)
        ok &= _case("часть несёт чужой пакет → красно", True, findings, "ЧУЖОЙ ПАКЕТ")

        SPLIT_BY_TEST = {"internal/repohygiene": 3, "internal/gone": 2}
        findings, _ = census(tree, assign(tree, MOD), module=MOD)
        ok &= _case("запись распила без пакета → красно", True, findings,
                    "РАСПИЛ БЕЗ ПРЕДМЕТА")
        SPLIT_BY_TEST = {"internal/repohygiene": 3}

        # Раздача проб: каждая — ровно в одной части, повторимо, устойчиво к новой.
        tests = [f"TestGate{i:03d}" for i in range(60)] + ["FuzzParse", "ExampleOut"]
        p1 = split_tests(tests, 3)
        flat = [t for part in p1 for t in part]
        ok &= _check("каждая проба ровно в одной части",
                     sorted(flat) == sorted(tests) and len(flat) == len(set(flat)),
                     f": роздано {len(flat)} при {len(tests)}")
        ok &= _check("раздача повторима", split_tests(list(reversed(tests)), 3) == p1)
        p2 = split_tests(tests + ["TestNewcomer"], 3)
        moved = sum(1 for i in range(3) for t in p1[i] if t not in p2[i])
        ok &= _check("новая проба не перетасовывает прежние", moved == 0,
                     f": переехало {moved}")
        ok &= _check("ни одна часть не пуста на 62 пробах", all(p1),
                     f": {[len(x) for x in p1]}")

        # План: части несут свои пробы; часть без проб в план не идёт.
        plan = plan_document(tree, shards, MOD, None, "полный",
                             {rh: tests})
        got = [t for r in plan["shards"] if r["id"].startswith("internal-repohygiene-")
               for t in r.get("tests", [])]
        ok &= _check("план: пробы частей в сумме — перечень, без задвоения",
                     sorted(got) == sorted(tests) and len(got) == len(set(got)))
        plan1 = plan_document(tree, shards, MOD, None, "полный", {rh: ["TestOnly"]})
        kept = [r["id"] for r in plan1["shards"] if r["id"].startswith("internal-repohygiene-")]
        ok &= _check("часть без проб в план не идёт", len(kept) == 1, f": {kept}")

        # Отбор `-run`: ровно перечень, без префиксных совпадений.
        rx = re.compile(run_regex(["TestA", "TestB"]))
        ok &= _check("отбор `-run` ловит перечень и не ловит продолжение имени",
                     bool(rx.match("TestA")) and bool(rx.match("TestB"))
                     and not rx.match("TestAB") and not rx.match("XTestA"))
    finally:
        SPLIT_BY_TEST = saved

    # Разбор перечня: строгий в обе стороны.
    legal = "TestA\nTestB\nFuzzC\nExampleD\nBenchmarkE\nok  \tgithub.com/x/y\t0.01s\n"
    try:
        names = parse_test_list(legal, "x")
        ok &= _check("законный перечень разобран, замер отброшен",
                     names == ["ExampleD", "FuzzC", "TestA", "TestB"], f": {names}")
    except Unmeasured as e:
        ok &= _check(f"законный перечень разобран ({e})", False)
    for name, text in (("посторонняя строка → отказ", "TestA\nпривет из init\nok  \tx\t0s\n"),
                       ("нет итога ok → отказ", "TestA\n"),
                       ("ноль проб → отказ", "ok  \tx\t0s\n")):
        try:
            parse_test_list(text, "x")
            ok &= _check(name, False)
        except Unmeasured:
            ok &= _check(name, True)
    return ok


def _self_test_select() -> bool:
    """Оси выбора по пакетам на синтетическом графе: дом пробы — в обе стороны."""
    ok = True
    # Разбор настоящей формы `go list -test`: варианты пакета несут пробел
    # («p [p.test]»), и разделитель обязан его пережить. Найдено живым прогоном:
    # с разделителем-пробелом правка домена vpc выбирала 1 пакет вместо 31.
    t = f"{MOD}/svc/a"
    out = (f"{t}\t/r/svc/a\t{MOD}/pkg/x\n"
           f"{t}.test\t/r/svc/a\t{MOD}/pkg/x,{t} [{t}.test],{MOD}/pkg/y [{t}.test]\n"
           f"{t}_test [{t}.test]\t/r/svc/a\t{MOD}/pkg/x\n")
    gd, gp = parse_go_graph(out, "/r")
    ok &= _check("разбор графа: варианты «p [p.test]» сведены к пути пакета",
                 gd == {"svc/a": t} and gp.get(t) == {f"{MOD}/pkg/x", t, f"{MOD}/pkg/y"},
                 f": {sorted(gp.get(t, []))}")
    dom = f"{MOD}/services/vpc/internal/domain"
    repo = f"{MOD}/services/vpc/internal/repo"
    mig = f"{MOD}/services/vpc/internal/migrations"
    cmd = f"{MOD}/services/vpc/cmd/vpc"
    rh = f"{MOD}/internal/repohygiene"
    gw = f"{MOD}/gateway/internal/allowlist"
    ids = f"{MOD}/pkg/ids"
    dirs = {"services/vpc/internal/domain": dom, "services/vpc/internal/repo": repo,
            "services/vpc/internal/migrations": mig, "services/vpc/cmd/vpc": cmd,
            "internal/repohygiene": rh, "gateway/internal/allowlist": gw, "pkg/ids": ids}
    deps = {dom: {dom, ids}, repo: {repo, dom, mig, ids}, mig: {mig},
            rh: {rh, ids}, gw: {gw, dom, ids}, ids: {ids}}
    tree = sorted(deps)

    def sel(files, d=deps):
        got, why = select_packages(files, dirs, d, tree)
        return got, why

    got, why = sel(["services/vpc/internal/domain/subnet.go"])
    ok &= _check("правка vpc выбирает vpc и обратных зависимых (край)",
                 got is not None and {dom, repo, gw} <= set(got), f": {why}")
    ok &= _check("правка vpc НЕ выбирает гейты дерева internal",
                 got is not None and rh not in got)
    # Законный близнец «лишнего выбора»: гейт, который ИМПОРТИРУЕТ домен vpc, обязан
    # быть выбран — иначе молчание выше было бы слепотой, а не точностью.
    d2 = dict(deps)
    d2[rh] = {rh, dom, ids}
    got2, _ = sel(["services/vpc/internal/domain/subnet.go"], d2)
    ok &= _check("гейт, импортирующий правленый пакет, выбран (близнец)",
                 got2 is not None and rh in got2)
    ok &= _check("пакет, не зависящий от правки, не выбран (pkg/ids)",
                 got is not None and ids not in got)

    got, _ = sel(["services/vpc/internal/domain/subnet_test.go"])
    ok &= _check("правка _test.go — только свой пакет", got == [dom], f": {got}")

    got, _ = sel(["services/vpc/internal/migrations/0042_x.sql"])
    ok &= _check("не-Go файл в каталоге пакета — пакет и его обратные зависимые",
                 got is not None and {mig, repo} <= set(got) and rh not in got, f": {got}")
    got, _ = sel(["services/vpc/internal/domain/testdata/x.go"])
    ok &= _check(".go под testdata — объемлющий пакет", got is not None and dom in got)

    for name, files in (("путь вне пакетов (.github) → полный", [".github/workflows/ci.yaml"]),
                        ("go.mod → полный", ["go.mod"]),
                        (".go вне пакетов модуля → полный", ["services/vpc/gone/x.go"]),
                        ("выбор пуст (пакет без проб и обратных) → полный",
                         ["services/vpc/cmd/vpc/main.go"]),
                        ("путей ноль → полный", [])):
        got, why = sel(files)
        ok &= _check(name, got is None, f": {why}")
    got, _ = sel(["services/vpc/internal/domain/subnet.go", "docs/x.md"])
    ok &= _check("один путь вне пакетов делает полным весь запрос", got is None)
    return ok


def self_test() -> int:
    print("самопроба раздачи: инъекция в обе стороны по каждой оси")
    global SPLIT_BY_TEST
    saved = SPLIT_BY_TEST
    try:
        SPLIT_BY_TEST = {}
        ok = _self_test_areas()
    finally:
        SPLIT_BY_TEST = saved
    print("самопроба распила по пробам:")
    ok &= _self_test_split()
    print("самопроба выбора по пакетам:")
    ok &= _self_test_select()
    print("самопроба:", "ПРОЙДЕНА" if ok else "ПРОВАЛЕНА")
    return 0 if ok else 1


DIGIT_IN_NAME = re.compile(r"\d")


def _refuse(findings: list[str]) -> int:
    for f in findings:
        sys.stderr.write(f"ОТКАЗ: {f}\n")
    return 1


def _read_plan(path: str) -> dict:
    try:
        plan = json.loads(Path(path).read_text(encoding="utf-8"))
    except (OSError, ValueError) as e:
        raise Unmeasured(f"план {path} не прочитан: {e}") from e
    if not isinstance(plan, dict) or not isinstance(plan.get("shards"), list):
        raise Unmeasured(f"план {path}: нет перечня шардов")
    return plan


def shard_spec(sid: str, root: Path, module: str, shards: dict[str, list[str]],
               plan: dict | None) -> dict:
    """Доля шарда: пакеты (выведены ЗАНОВО, тем же правилом) и, у части, — пробы.

    С планом пакеты сужаются ВЫБОРОМ плана, а пробы части берутся из плана: их
    перечень — вывод тестового бинаря, и вторая его сборка ради того же ответа
    стоила бы части лишней линковки. Свод сверяет исполненное с перечнем плана."""
    if sid not in shards:
        raise Unmeasured(f"шарда `{sid}` в раздаче нет. Есть: {' '.join(sorted(shards))}")
    pkgs = list(shards[sid])
    pkg = split_parts(shards, module).get(sid)
    tests: list[str] = []
    if plan is not None:
        row = next((r for r in plan["shards"] if r.get("id") == sid), None)
        if row is None:
            raise Unmeasured(f"шарда `{sid}` нет в плане — план его не выбирал")
        chosen = set((plan.get("selection") or {}).get("packages")
                     or plan.get("tree_packages") or [])
        pkgs = [p for p in pkgs if p in chosen]
        if pkg is not None:
            tests = list(row.get("tests") or [])
    elif pkg is not None:
        ids = part_ids(rel_of(pkg, module), SPLIT_BY_TEST[rel_of(pkg, module)])
        tests = split_tests(list_tests(root, pkg), len(ids))[ids.index(sid)]
    if pkg is not None and not tests:
        raise Unmeasured(f"часть `{sid}`: проб НОЛЬ — гонять нечего, и это отказ")
    return {"packages": pkgs, "split_pkg": pkg or "", "tests": tests}


def main() -> int:
    ap = argparse.ArgumentParser(description="раздача пакетов с пробами по шардам")
    ap.add_argument("--self-test", action="store_true")
    ap.add_argument("--census", action="store_true",
                    help="перепись полноты: в дереве N · роздано M; расхождение — отказ; "
                         "с --out — план с выбором и распилом")
    ap.add_argument("--matrix", action="store_true",
                    help="строка `matrix=<json>` для $GITHUB_OUTPUT (с --plan — из плана)")
    ap.add_argument("--ids", action="store_true", help="идентификаторы шардов, по одному в строке")
    ap.add_argument("--from-index", action="store_true",
                    help="выводить области из ИНДЕКСА git, а не из `go list` — для "
                         "читателя, у которого Go нет (процесс по расписанию)")
    ap.add_argument("--packages", metavar="SHARD",
                    help="пакеты названного шарда, по одному в строке")
    ap.add_argument("--shard-spec", metavar="SHARD",
                    help="доля шарда присваиваниями оболочки: pkgs, run, split_pkg, split_tests")
    ap.add_argument("--plan", metavar="PATH", help="план от --census --out")
    ap.add_argument("--select-files", metavar="PATH",
                    help="целевой выбор по путям из файла (- — stdin), без события")
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

    # Матрица из плана не спрашивает дерево второй раз: план уже судим переписью,
    # и шард сам выведет свою долю заново (`--shard-spec`).
    if args.matrix and args.plan:
        try:
            plan = _read_plan(args.plan)
        except Unmeasured as e:
            sys.stderr.write(f"ОТКАЗ: {e}\n")
            return 2
        rows = {r["id"]: r.get("packages") or [] for r in plan["shards"] if r.get("id")}
        if not rows:
            sys.stderr.write("ОТКАЗ: в плане ноль шардов — матрица была бы пустой\n")
            return 1
        print("matrix=" + json.dumps(matrix_document(rows, order_weights(root)),
                                     ensure_ascii=False))
        return 0

    try:
        module = module_path(root)
        tree = tree_packages(root)
        index, _files = index_areas(root)
    except Unmeasured as e:
        sys.stderr.write(f"ОТКАЗ: {e}\n")
        return 2

    shards = assign(tree, module)
    findings, num = census(tree, shards, index, module)
    weights = order_weights(root)

    if args.packages or args.shard_spec:
        sid = args.packages or args.shard_spec
        # Раздача судится ДО выдачи: шард, уехавший по непроверенной раздаче,
        # уносит с собой и её дефект.
        if findings:
            return _refuse(findings)
        try:
            spec = shard_spec(sid, root, module, shards,
                              _read_plan(args.plan) if args.plan else None)
        except Unmeasured as e:
            sys.stderr.write(f"ОТКАЗ: {e}\n")
            return 2
        if args.packages:
            print("\n".join(spec["packages"]))
            return 0
        print(f"pkgs={shlex.quote(' '.join(spec['packages']))}")
        print(f"run={shlex.quote(run_regex(spec['tests']) if spec['tests'] else '')}")
        print(f"split_pkg={shlex.quote(spec['split_pkg'])}")
        print(f"split_tests={shlex.quote(' '.join(spec['tests']))}")
        return 0

    if args.ids:
        if findings:
            return _refuse(findings)
        print("\n".join(shards))
        return 0

    if args.matrix:
        if findings:
            return _refuse(findings)
        print("matrix=" + json.dumps(matrix_document(shards, weights),
                                     ensure_ascii=False))
        return 0

    # ВЫБОР: по путям из файла (локально, проба выбора) либо по событию запроса.
    if args.select_files:
        try:
            src = sys.stdin if args.select_files == "-" else open(
                args.select_files, encoding="utf-8")
            changed: list[str] | None = [ln.strip() for ln in src if ln.strip()]
        except OSError as e:
            sys.stderr.write(f"ОТКАЗ: пути не прочитаны: {e}\n")
            return 2
        why = "выбор по путям из файла"
    else:
        changed, why = event_files()
    selected: list[str] | None = None
    if changed is not None:
        try:
            dirs, deps = go_graph(root)
        except Unmeasured as e:
            sys.stderr.write(f"ОТКАЗ: {e}\n")
            return 2
        selected, why = select_packages(changed, dirs, deps, tree)

    # Перечень проб — лишь у распиленных пакетов, которые выбраны: гонять
    # невыбранный пакет никто не будет, и его линковка была бы чистой тратой.
    chosen = set(tree) if selected is None else set(selected)
    listing: dict[str, list[str]] = {}
    try:
        for pkg in sorted(set(split_parts(shards, module).values()) & chosen):
            listing[pkg] = list_tests(root, pkg)
    except Unmeasured as e:
        sys.stderr.write(f"ОТКАЗ: {e}\n")
        return 2
    plan = plan_document(tree, shards, module, selected, why, listing)

    # Перепись — умолчание: вызов без режима обязан ЧТО-ТО измерить, а не молчать.
    print(f"ПЕРЕПИСЬ ПОЛНОТЫ (единица счёта — пакет, у которого `go list` видит "
          f"TestGoFiles либо XTestGoFiles при сборке по умолчанию)")
    print(f"  пакетов с пробами в дереве: {num['tree']}")
    print(f"  роздано по шардам:          {num['handed']} (различных {num['handed_unique']})")
    print(f"  шардов:                     {num['shards']}")
    print(f"ВЫБОР: {'ПОЛНЫЙ' if selected is None else 'ЦЕЛЕВОЙ'} — {why}")
    print(f"  пакетов выбрано {len(plan['selection']['packages'])} из {num['tree']}; "
          f"шардов к прогону {len(plan['shards'])} из {num['shards']}")
    for pkg, info in plan["split"].items():
        sizes = {r["id"]: len(r.get("tests") or []) for r in plan["shards"]
                 if r["id"] in info["parts"]}
        print(f"  распил {rel_of(pkg, module)}: проб {len(info['tests'])} "
              f"(`go test {' '.join(LIST_FLAGS)} -list`) по частям {sizes}")
    print(f"  порядок отправки — самый тяжёлый первым; вес = объявлений проб "
          f"(решает ТОЛЬКО очередь, не раздачу)")
    for row in matrix_document({r["id"]: r["packages"] for r in plan["shards"]},
                               weights)["shard"]:
        sid = row["id"]
        print(f"    {sid:34} пакетов {len(shards[sid]):4} · "
              f"объявлений проб {weights.get(sid, 0):5}")
    if args.out:
        Path(args.out).write_text(json.dumps(plan, ensure_ascii=False, indent=1) + "\n",
                                  encoding="utf-8")
        print(f"  план записан: {args.out}")
    if findings:
        print(f"НАХОДОК {len(findings)}:")
        for f in findings:
            print("  · " + f)
        return 1
    print("роздано = дерево: ни одного пакета вне шардов, ни одного дважды; "
          "распиленный — ровно в своих частях")
    return 0


if __name__ == "__main__":
    sys.exit(main())
