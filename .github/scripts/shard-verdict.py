#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""shard-verdict.py — что ЭТОТ шард реально исполнил, числом, в машинном виде.

ПРЕДМЕТ. После разнесения по раннерам вердикт больше не выносится в одном месте:
каждый шард судит свои суиты сам (assert-suites-green.sh), а свести это в один
ответ можно только по следам. Файл, который пишет этот скрипт, и есть след:
сколько коллекций шард ОБЯЗАН был отчитать (по дереву), сколько отчитал, и что
именно в этих отчётах.

ОПИСЬ — И ИСХОД РАБОТЫ ТАМ, ГДЕ БОЛЬШЕ ЕГО НЕ ВЫНОСИТ НИКТО (решение владельца
2026-09-12: «сделай так, чтобы можно было гарантировать зелёный; если что-то не так
— в красный»). О ПРОБАХ скрипт по-прежнему не судит: их вердикт выносят гейты суит,
и второй ответ рядом с первым дал бы два разных ответа на один вопрос. Поэтому
ветвление ровно такое:

  условие не создано      → код 75, метка «УСЛОВИЕ НЕ СОЗДАНО». Все гейты суит при
                            этом ПРОПУЩЕНЫ, то есть вердикта не выносит НИ ОДИН
                            шаг работы, и прежде работа уходила ЗЕЛЁНОЙ;
  коллекций в дереве ноль → код 1, метка «ОПИСЬ ПУСТА»: «0 из 0» — не вердикт;
  всё прочее              → код 0 и метка «вердикт выносят гейты суит».

Метки взаимно исключают друг друга и печатается ровно одна: два красных — «условие
не создано» и «пробы упали» — обязаны быть различимы ТЕКСТОМ, иначе отладка гадает.
Уникальность метки в дереве держит `--self-test`.

Здесь стояло «всегда выходит нулём, если сумел записать файл». Утверждение было
верно ровно до тех пор, пока работа краснела сама: прогон 34694955309 дал ЧЕТЫРЕ
шарда `success` при исполненных нуле коллекций из 57 — отметку поставил вердикт
провенанса, десять вердиктных шагов погасли, и сказал об этом ТОЛЬКО свод.

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
получается ТРЕМЯ разными способами, и следов у всех трёх одинаково ноль: прогон
шёл и упал (находка о дереве); стенд не поднялся, потому что посторонний хост не
отдал чарты; стенд поднялся, но исполняет ДРУГОЕ дерево. Второе и третье —
«условие не создано» (`e2e-flow.md` §6), и они приходят ОТМЕТКОЙ от того, кто их
наблюдал: наблюдателей ДВА, и опись называет обоих (см. FLAG_PRODUCERS ниже).
Прежде здесь стоял «единственный владелец материализации зависимостей» — второй
наблюдатель существовал уже тогда, и его форма молчала ровно потому, что этот
текст о ней не знал. Своими силами свод причины не установит, и молчание здесь
означало бы, что чужая причина и дальше читается как красный шард.
"""
from __future__ import annotations

import argparse
import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile

REPO = pathlib.Path(__file__).resolve().parents[2]
MANIFEST = REPO / "deploy" / "e2e-shards.json"
SUFFIX = ".postman_collection.json"

# ФОРМ НЕСОЗДАННОГО УСЛОВИЯ ДВЕ, И ВТОРАЯ БЫЛА НЕВИДИМА.
#
#   форма A — отметка-ФАЙЛ. Её кладёт единственный владелец материализации
#             зависимостей умбреллы (deploy/scripts/helm-umbrella-deps.sh) и
#             снимает в начале каждого прогона, поэтому она описывает ЭТОТ
#             прогон, а не позапрошлый. Причина лежит в файле;
#   форма B — отметка в ОКРУЖЕНИИ и больше нигде. Её ставит вердикт провенанса
#             (.github/scripts/stand-revision-verdict.sh), когда стенд поднялся,
#             но исполняет ДРУГОЕ дерево. Файла он не оставляет — и распознаватель,
#             искавший только файл, на этой форме МОЛЧАЛ: в описи не было ни
#             `precondition`, ни причины, и свод честно печатал «НЕ ВЫПОЛНИЛОСЬ …
#             0 коллекций из 57», ни разу не сказав «условие не создано».
#
# Перечень производителей ВЫВОДИТСЯ ИЗ ДЕРЕВА самопроверкой и сверяется с
# FLAG_PRODUCERS в обе стороны: третий производитель обязан дать находку, а не
# молчание, а запись без производителя — не переживать свой предмет.
MARK_NAME = ".kacho-deps-precondition-unmet"
PRECOND_MARK = REPO / "deploy" / "helm" / "umbrella" / MARK_NAME
FLAG = "STAND_PRECONDITION_UNMET"

# Код 75 — машинный признак ТРЕТЬЕЙ КАТЕГОРИИ внутри прогона, тот же, что у
# классификатора посадки (deploy/scripts/classify-integration-outcome.sh, читает
# его ci.yaml). Работа при нём краснеет: исход РАБОТЫ меняется, различение
# остаётся в тексте и в коде.
EXIT_UNMET = 75
EXIT_EMPTY = 1

# Метки исхода. ОДНА печатается всегда, и они взаимно исключают друг друга:
# отладка не обязана гадать, какое из двух красных перед ней.
VERDICT_UNMET = "ВЕРДИКТ РАБОТЫ ШАРДА: КРАСНОЕ — УСЛОВИЕ НЕ СОЗДАНО"
VERDICT_EMPTY = "ВЕРДИКТ РАБОТЫ ШАРДА: КРАСНОЕ — ОПИСЬ ПУСТА"
VERDICT_GATES = "ВЕРДИКТ РАБОТЫ ШАРДА: вердикт выносят гейты суит"



def precondition() -> dict | None:
    """«Условие не создано» — в ЛЮБОЙ из двух форм, с названной формой и причиной.

    Отметка-ФАЙЛ идёт первой не по старшинству, а по содержательности: в ней
    лежит причина, названная тем, кто её наблюдал. Отметка в окружении причины
    не несёт — её производитель печатает обе ревизии в журнал и в сводку, и
    подставлять здесь свою догадку значило бы выдать её за наблюдение.

    Пустой файл — тоже отметка: причина могла не извлечься, но САМ ФАКТ чужой
    недоступности наблюдал тот, кто её положил. Считать отметку без текста
    отсутствующей значило бы терять третью категорию из-за пустой строки.
    """
    if PRECOND_MARK.is_file():
        try:
            detail = PRECOND_MARK.read_text(encoding="utf-8").strip()
        except OSError as exc:
            # Нечитаемая отметка — НЕ «отметки нет»: факт наблюдён, потеряна
            # только его формулировка, и молчать об этом нельзя.
            detail = f"отметка есть, но не прочиталась ({exc.__class__.__name__})"
        return {"unmet": True, "kind": "external-chart-source",
                "producer": ".github/scripts/stand-up.sh",
                "detail": detail or "причина не извлеклась (отметка пуста)"}
    if os.environ.get(FLAG) == "1":
        return {"unmet": True, "kind": "stand-revision-divergence",
                "producer": ".github/scripts/stand-revision-verdict.sh",
                "detail": "стенд поднялся, но исполняет ДРУГОЕ дерево: ревизия контейнеров "
                          "не сошлась с ожидаемой. Обе величины назвал шаг «провенанс "
                          "стенда» выше — в журнале работы и в сводке прогона"}
    return None


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

    # ─── ИСХОД РАБОТЫ. Ровно одна метка, и порядок ветвей несущий ───────────
    #
    # ПУСТОЙ ОБХОД РАНЬШЕ ВСЕГО. «Ожидали 0, отчиталось 0» сходится с любым
    # утверждением и потому не утверждает ничего; поставив эту ветвь после
    # несозданного условия, мы получили бы объяснение вместо отказа — более
    # утешительную историю на месте слепого прибора.
    if v["expected"] == 0:
        print(f"{VERDICT_EMPTY} — коллекций суит {' '.join(shard['suites'])} в дереве "
              f"не нашлось ни одной. Судить не по чему: «0 из 0» сходится с любым "
              f"утверждением, поэтому это ОТКАЗ, а не зелёное. Чинится там, где "
              f"порождаются коллекции, а не здесь.")
        return EXIT_EMPTY

    # УСЛОВИЕ НЕ СОЗДАНО ⇒ КРАСНОЕ (решение владельца 2026-09-12). Гейты суит
    # при отметке ПРОПУЩЕНЫ все до одного — значит вердикта о пробах не выносит
    # ни один шаг работы, и «ничего не упало» здесь означало «ничего не
    # спрашивали». Прежде работа при этом уходила ЗЕЛЁНОЙ.
    if pre:
        print(f"{VERDICT_UNMET} · форма «{pre['kind']}» · наблюдатель "
              f"{pre['producer']}")
        print(f"    причина: {pre['detail']}")
        print(f"    вердикта о пробах нет НИ У ОДНОГО шага этой работы: гейты суит "
              f"пропущены отметкой {FLAG}, коллекций исполнено "
              f"{v['reported']} из {v['expected']}. Дефекта продукта здесь НЕ "
              f"показано — это ТРЕТЬЯ КАТЕГОРИЯ, и работа краснеет именно поэтому: "
              f"зелёное означало бы «проверено и чисто».")
        print(f"    что делать: снять названную причину и ПОВТОРИТЬ прогон. Разбирать "
              f"как упавшую пробу нечего — ни одна не исполнялась.")
        return EXIT_UNMET

    # ПРОХОД ТРЕБУЕТ ПРОЧИТАННОГО, а не отсутствия падений: `expected > 0`
    # проверено ветвью выше, и снять её значит вернуть зелёное на пустом обходе.
    print(f"{VERDICT_GATES}: условие создано, коллекций прочитано "
          f"{v['reported']} из {v['expected']}. Опись вердикта о пробах не выносит — "
          f"он у гейтов суит выше, и второй ответ здесь дал бы два разных ответа "
          f"на один вопрос.")
    return 0


# ─────────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА: РАСПОЗНАВАТЕЛЬ ЗНАЕТ ВСЕ ФОРМЫ, А КРАСНОЕ ДОЕЗЖАЕТ ДО РАБОТЫ
#
# Предмет самопроверки — не «написано ли», а ТРИ вещи, каждая из которых
# ломается молча:
#
#   1. форм несозданного условия в дереве ДВЕ, и вторая была невидима. Отметку
#      `STAND_PRECONDITION_UNMET=1` кладут ДВА производителя: владелец подъёма
#      (при отметке-файле постороннего источника чартов) и вердикт провенанса
#      (когда стенд исполняет ДРУГОЕ дерево). Второй файла не оставляет — и
#      распознаватель, ищущий только файл, на нём МОЛЧАЛ. Наблюдалось на прогоне
#      34694955309: свод сказал «НЕ ВЫПОЛНИЛОСЬ … 0 коллекций из 57» и НЕ сказал
#      «условие не создано», потому что ни одна опись этого не несла;
#   2. перечень производителей ВЫВОДИТСЯ ИЗ ДЕРЕВА, а не выписан. Третий
#      производитель, о котором распознаватель не рассуждал, обязан давать
#      находку, а не молчание;
#   3. краснота скрипта становится краснотой РАБОТЫ только если шаг её не глотает.
#      `continue-on-error`, `|| true`, снятое условие — и ложное зелёное
#      возвращается, ничем этого не показав.
#
# Прогонов на каждую ось три, а не два: контроль · инъекция нового · инъекция
# существующего. Без третьего молчание прежней формы неотличимо от её смерти.
WORKFLOW = REPO / ".github" / "workflows" / "e2e-newman.yml"

# Производители отметки, О КОТОРЫХ РАСПОЗНАВАТЕЛЬ РАССУЖДАЛ, и форма каждого.
# Набор сверяется с деревом В ОБЕ СТОРОНЫ: запись без производителя переживает
# свой предмет, производитель без записи — слепая зона.
FLAG_PRODUCERS = {
    ".github/scripts/stand-up.sh": "external-chart-source",
    ".github/scripts/stand-revision-verdict.sh": "stand-revision-divergence",
}


def flag_producers_in_tree() -> dict[str, bool]:
    """путь → оставляет ли он ещё и отметку-ФАЙЛ. ВЫВЕДЕНО из дерева.

    Признак производителя — строка, кладущая отметку в `$GITHUB_ENV`, а не слово
    в прозе: имя переменной стоит в дереве десятки раз в условиях шагов, в
    объяснениях и в самопроверках, и поиск по слову нашёл бы их все.
    """
    r = subprocess.run(["git", "-C", str(REPO), "grep", "-n", "--fixed-strings",
                        f"{FLAG}=1"], capture_output=True, text=True)
    # код 1 у git grep — «совпадений нет»; это не отказ инструмента, но и не
    # находка: пустой обход обрабатывается вызывающим.
    if r.returncode not in (0, 1):
        raise SystemExit(f"FATAL: git grep не отработал: {r.stderr.strip()}")
    out: dict[str, bool] = {}
    for line in r.stdout.splitlines():
        parts = line.split(":", 2)
        if len(parts) < 3 or "GITHUB_ENV" not in parts[2]:
            continue
        out.setdefault(parts[0], False)
    for path in list(out):
        # Отказ чтения НЕ глушится: `except` здесь превратил бы неизвестность в
        # «отметки-файла не кладёт», то есть в тихий ответ о форме. Проверено
        # собой: первая редакция этой строки маскировала NameError и отвечала
        # «нет» — ось формы покраснела, и только двусторонняя сверка это назвала.
        out[path] = MARK_NAME in (REPO / path).read_text(encoding="utf-8")
    return out


def verdict_steps() -> list[dict]:
    """Шаги объявления работы `shard`, запускающие ЭТУ опись. Выведены из дерева."""
    import yaml  # локально: обычный проход описи разбора YAML не требует
    doc = yaml.safe_load(WORKFLOW.read_text(encoding="utf-8")) or {}
    steps = list(((doc.get("jobs") or {}).get("shard") or {}).get("steps") or [])
    return [s for s in steps if "shard-verdict.py" in str(s.get("run") or "")]


def step_swallows_red(step: dict) -> list[str]:
    """Чем шаг мог бы проглотить ненулевой код. Пусто — не проглотит."""
    why: list[str] = []
    if str(step.get("continue-on-error") or "").lower() in ("true", "yes", "on"):
        why.append("continue-on-error")
    run = str(step.get("run") or "")
    for token in ("|| true", "|| exit 0", "; exit 0", "set +e", "|| :"):
        if token in run:
            why.append(f"«{token}» в теле")
    if not str(step.get("if") or "").strip():
        why.append("условия нет вовсе — шаг не дойдёт до упавшей работы")
    return why


def _probe_tree(tmp: pathlib.Path, *, collections: int = 2) -> pathlib.Path:
    """Подставное дерево: настоящий скрипт, синтетический манифест и суита.

    Настоящее дерево трогать нельзя — опись читает отчёты по путям суит, и
    проба, кладущая их туда, оставляла бы следы в рабочей копии. Скрипт при этом
    КОПИРУЕТСЯ, а не переписывается: судится то, что поедет в конвейер.
    """
    (tmp / ".github" / "scripts").mkdir(parents=True)
    (tmp / "deploy" / "helm" / "umbrella").mkdir(parents=True)
    suite = tmp / "services" / "geo" / "tests" / "newman"
    (suite / "collections").mkdir(parents=True)
    (suite / "out").mkdir(parents=True)
    shutil.copy2(pathlib.Path(__file__).resolve(), tmp / ".github" / "scripts" / "shard-verdict.py")
    (tmp / "deploy" / "e2e-shards.json").write_text(
        json.dumps({"shards": [{"id": "probe", "suites": ["geo"]}]}), encoding="utf-8")
    for i in range(collections):
        (suite / "collections" / f"c{i}{SUFFIX}").write_text("{}", encoding="utf-8")
    subprocess.run(["git", "init", "-q"], cwd=tmp, check=True, capture_output=True)
    # Пути перечислены ЯВНО: `-A` в общей рабочей копии запрещён, и привычка
    # сохраняется здесь, чтобы не переехала обратно копированием.
    subprocess.run(["git", "add", "services", "deploy", ".github"],
                   cwd=tmp, check=True, capture_output=True)
    return suite


def _report(failed: int = 0, assertions: int = 4, requests: int = 4,
            unanswered: int = 0, scripts: int = 0) -> str:
    return json.dumps({"run": {"stats": {
        "requests": {"total": requests, "failed": unanswered},
        "assertions": {"total": assertions, "failed": failed},
        "testScripts": {"total": requests, "failed": scripts},
        "prerequestScripts": {"total": requests, "failed": 0},
    }}})


def _run_probe(tmp: pathlib.Path, env_flag: bool) -> tuple[int, str, dict]:
    env = dict(os.environ)
    env.pop(FLAG, None)
    if env_flag:
        env[FLAG] = "1"
    out = tmp / "shard-verdicts" / "shard-verdict-probe.json"
    p = subprocess.run([sys.executable, str(tmp / ".github" / "scripts" / "shard-verdict.py"),
                        "--shard", "probe", "--out", str(out)],
                       cwd=tmp, env=env, capture_output=True, text=True)
    written: dict = {}
    if out.is_file():
        written = json.loads(out.read_text(encoding="utf-8"))
    return p.returncode, p.stdout + p.stderr, written


def _self_test() -> int:
    ok = True
    seen = {"производителей": 0, "шагов-вызовов": 0, "прогонов описи": 0}

    def say(good: bool, label: str, detail: str = "") -> None:
        nonlocal ok
        ok = ok and good
        print(f"  {'ОК    ' if good else 'ПРОВАЛ'} {label}"
              + (f" — {detail}" if detail and not good else ""))

    print("=== shard-verdict.py --self-test ===")

    # ── ось 1: формы несозданного условия выведены из дерева ─────────────────
    print("  --- формы отметки: перечень выведен из дерева, сверен в обе стороны")
    tree = flag_producers_in_tree()
    seen["производителей"] = len(tree)
    say(bool(tree), f"производителей отметки {FLAG} в дереве: {len(tree)}",
        "обход пуст — «все формы известны» значило бы «ни одна не прочитана»")
    for path, with_mark in sorted(tree.items()):
        want = FLAG_PRODUCERS.get(path)
        say(want is not None,
            f"о производителе '{path}' распознаватель рассуждал",
            "производителя нет в FLAG_PRODUCERS — его форма даст МОЛЧАНИЕ")
        if want is not None:
            expect = "external-chart-source" if with_mark else "stand-revision-divergence"
            say(want == expect,
                f"'{path}' → форма «{want}» (отметка-файл: {'да' if with_mark else 'нет'})",
                f"дерево говорит «{expect}»")
    for path in FLAG_PRODUCERS:
        say(path in tree, f"запись '{path}' имеет производителя в дереве",
            "запись переживает свой предмет — снимите её")

    # ── ось 2: контроль ──────────────────────────────────────────────────────
    print("  --- контроль: отметки нет, отчёты на местах, пробы зелёные")
    with tempfile.TemporaryDirectory() as td:
        tmp = pathlib.Path(td)
        suite = _probe_tree(tmp)
        (suite / "out" / "c0.json").write_text(_report(), encoding="utf-8")
        (suite / "out" / "c1.json").write_text(_report(), encoding="utf-8")
        rc, out, v = _run_probe(tmp, env_flag=False)
        seen["прогонов описи"] += 1
        say(rc == 0, f"опись вышла нулём (код {rc})", out[-500:])
        say(int(v.get("expected") or 0) > 0 and v.get("reported") == v.get("expected"),
            f"прочитано коллекций {v.get('reported')} из {v.get('expected')} — обход не пуст")
        say("precondition" not in v, "отметки в описи нет — условие создано")
        say(VERDICT_GATES in out and VERDICT_UNMET not in out,
            "напечатана метка «вердикт выносят гейты суит», и только она")

    # ── ось 3: инъекция НОВОГО — форма без файла (расхождение ревизий) ───────
    print("  --- инъекция нового: отметка в окружении, файла нет ⇒ КРАСНОЕ")
    with tempfile.TemporaryDirectory() as td:
        tmp = pathlib.Path(td)
        suite = _probe_tree(tmp)
        rc, out, v = _run_probe(tmp, env_flag=True)
        seen["прогонов описи"] += 1
        say(rc != 0, f"опись покраснела (код {rc})", "исход работы остался бы зелёным")
        say(rc == 75, f"код {rc} — машинный признак третьей категории",
            "ждали 75: тот же признак, что у классификатора посадки")
        say(VERDICT_UNMET in out, "текст назвал несозданное условие", out[-500:])
        say("вердикта о пробах нет" in out,
            "текст сказал, что вердикта о пробах НЕТ", out[-500:])
        say((v.get("precondition") or {}).get("kind") == "stand-revision-divergence",
            "опись несёт форму «stand-revision-divergence» — свод её прочтёт",
            f"в описи: {v.get('precondition')}")

    # ── ось 4: инъекция СУЩЕСТВУЮЩЕГО — отметка-файл постороннего источника ──
    print("  --- инъекция существующего: отметка-ФАЙЛ ⇒ КРАСНОЕ, причина из файла")
    for with_env in (True, False):
        with tempfile.TemporaryDirectory() as td:
            tmp = pathlib.Path(td)
            suite = _probe_tree(tmp)
            (tmp / "deploy" / "helm" / "umbrella" / MARK_NAME).write_text(
                "источник чартов не ответил: https://charts.example.invalid/x\n", encoding="utf-8")
            rc, out, v = _run_probe(tmp, env_flag=with_env)
            seen["прогонов описи"] += 1
            tail = f"(отметка в окружении: {'есть' if with_env else 'нет'})"
            say(rc == 75, f"код 75 {tail}", f"получено {rc}")
            say((v.get("precondition") or {}).get("kind") == "external-chart-source",
                f"форма «external-chart-source» {tail}", f"в описи: {v.get('precondition')}")
            say("charts.example.invalid" in out,
                f"причина взята ИЗ ФАЙЛА, а не подставлена {tail}", out[-500:])

    # ── ось 5: законный близнец — пробы УПАЛИ, условие создано ───────────────
    print("  --- законный близнец: пробы упали ⇒ опись НЕ присваивает себе вердикт")
    with tempfile.TemporaryDirectory() as td:
        tmp = pathlib.Path(td)
        suite = _probe_tree(tmp)
        (suite / "out" / "c0.json").write_text(_report(failed=3), encoding="utf-8")
        (suite / "out" / "c1.json").write_text(_report(unanswered=2), encoding="utf-8")
        rc, out, v = _run_probe(tmp, env_flag=False)
        seen["прогонов описи"] += 1
        say(rc == 0, f"опись вышла нулём (код {rc}) — краснеет гейт суиты, а не она",
            "второй вердикт об одном предмете")
        say(VERDICT_UNMET not in out,
            "метки «условие не создано» в тексте НЕТ — два красных различимы", out[-500:])
        say(v.get("failed") == 3 and v.get("unanswered") == 2,
            f"числа доехали до свода: упавших {v.get('failed')}, без ответа {v.get('unanswered')}")

    # ── ось 6: пустой обход роняет ───────────────────────────────────────────
    print("  --- пустой обход: коллекций в дереве ноль ⇒ КРАСНОЕ, а не «0 из 0»")
    with tempfile.TemporaryDirectory() as td:
        tmp = pathlib.Path(td)
        _probe_tree(tmp, collections=0)
        rc, out, v = _run_probe(tmp, env_flag=False)
        seen["прогонов описи"] += 1
        say(rc != 0, f"опись покраснела (код {rc})",
            "«ожидали 0, отчиталось 0» уехало бы зелёным")
        say(VERDICT_EMPTY in out, "метка «опись пуста» — отдельная от двух остальных",
            out[-500:])

    # ── ось 7: метка уникальна в дереве ──────────────────────────────────────
    # ПЕЧАТАЮЩИХ ровно один, и это несущее число. «Два красных различимы по
    # тексту» держится не обещанием, а тем, что метку печатает единственный
    # скрипт: второй печатающий вернул бы догадку. Замерено ИСПОЛНЕНИЕМ, почему
    # одной общей фразы было недостаточно: гейт суиты на упавших пробах печатает
    # `TOTAL(категории): … УСЛОВИЕ НЕ СОЗДАНО 0` — то есть отбор по короткой фразе
    # выбрал бы ОБА красных. Отбор по метке выбирает ровно одно.
    #
    # Документация из счёта исключена намеренно: предмет — печать в журнал
    # прогона, а страница, цитирующая метку, вторым печатающим не становится.
    print("  --- различимость: метку печатает РОВНО один исполняемый файл дерева")
    for mark in (VERDICT_UNMET, VERDICT_EMPTY):
        r = subprocess.run(["git", "-C", str(REPO), "grep", "-l", "--fixed-strings", mark],
                           capture_output=True, text=True)
        found = [l for l in r.stdout.splitlines() if l.strip()]
        say(bool(found), f"метка «{mark[:44]}…» в дереве вообще есть",
            "не нашлось ни одного вхождения — предикат слеп")
        printers = [f for f in found if not f.endswith(".md")]
        say(len(printers) == 1, f"печатающих её файлов {len(printers)} "
            f"(всего вхождений в файлах: {len(found)})",
            f"печатают: {printers or 'никто'}")

    # ── ось 8: краснота доезжает до работы ───────────────────────────────────
    print("  --- распространение: шаг работы `shard` ненулевой код НЕ глотает")
    steps = verdict_steps()
    seen["шагов-вызовов"] = len(steps)
    say(bool(steps), f"шагов, зовущих опись в работе `shard`: {len(steps)}",
        "ни одного — судить распространение не по чему")
    for s in steps:
        why = step_swallows_red(s)
        say(not why, f"шаг «{s.get('name')}» проглотить красное не может",
            "; ".join(why))
    hurt = [dict(s, **{"continue-on-error": True}) for s in steps]
    say(bool(steps) and all(step_swallows_red(h) for h in hurt),
        "инъекция: `continue-on-error: true` → находка")
    hurt2 = [dict(s, run=str(s.get("run") or "") + " || true") for s in steps]
    say(bool(steps) and all(step_swallows_red(h) for h in hurt2),
        "инъекция: «|| true» в теле → находка")

    print("осмотрено: " + ", ".join(f"{k} {n}" for k, n in seen.items()))
    print("самопроверка:", "PASS" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(_self_test() if "--self-test" in sys.argv[1:] else main())
