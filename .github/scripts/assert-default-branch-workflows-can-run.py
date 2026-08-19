#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Гейт: workflow, запускаемый только с ветки по умолчанию, действительно запускается.

ПРЕДМЕТ
-------
События `schedule`, `workflow_dispatch` и `workflow_run` регистрируются ТОЛЬКО
для файла, лежащего на ветке по умолчанию. Workflow, у которого других триггеров
нет, а файла на ней ещё нет, не запускается НИ РАЗУ — и при этом в дереве
выглядит живым: файл есть, job описан, шаги осмысленные. «Ноль отказов за всю
жизнь контроля» у него означает «контроля не было», и отличить это от «всё
чисто» нечем.

Померено на дереве: из 9 файлов этого класса был не зарегистрирован один
(`production-posture.yml`) — API отвечал на него 404.

ТРИ СОСТОЯНИЯ, А НЕ ДВА — И В ЭТОМ БЫЛА ОШИБКА ПРЕЖНЕЙ РЕДАКЦИИ
---------------------------------------------------------------
Прежняя редакция (шаг внутри ci.yaml) знала ровно один вопрос — «зарегистрирован
ли?» — и всякий незарегистрированный объявляла находкой с дихотомией «довести
файл до ветки по умолчанию либо удалить его из дерева». Оба исхода НЕДОСТИЖИМЫ
из того изменения, которое такой workflow вводит: на ветку по умолчанию файл
попадает только слиянием, а слияние блокирует сам гейт. То есть завести новый
workflow этого класса было нельзя ВООБЩЕ — кроме как через рукописный список
ожидания, который приходилось потом снимать вторым заходом.

Различать надо ТРИ состояния, и разделяет их присутствие файла на ветке по
умолчанию — вопрос, которого прежняя редакция не задавала:

  * ФАЙЛА НА ВЕТКЕ ПО УМОЛЧАНИЮ НЕТ → это ВВЕДЕНИЕ. Законно by construction:
    иначе такой workflow не мог бы появиться никогда. Не находка; послабление
    истекает САМО — слиянием, а не чьим-то решением;
  * ФАЙЛ ЕСТЬ, НО НЕ ЗАРЕГИСТРИРОВАН → находка. Файл на ветке по умолчанию, а
    триггеры не поднялись: разбирать надо файл, а не процесс;
  * ЗАРЕГИСТРИРОВАН, НО ПРОГОНОВ НОЛЬ → находка. Регистрация отвечает «может
    запускаться», а не «запускался»: cron, который не совпадает никогда, и
    `workflow_run` по прогону, который не завершается, оба РЕГИСТРИРУЮТСЯ.

ПОСЛАБЛЕНИЕ ПО ВОЗРАСТУ У ПОСЛЕДНЕГО СОСТОЯНИЯ, И ЕГО СЛАБОСТЬ НАЗВАНА
----------------------------------------------------------------------
Свежеслитый workflow ждёт первого срабатывания cron — у недельного расписания до
семи суток. Требовать от него прогона сразу значило бы красить ствол неделю за
работу, сделанную правильно. Поэтому «прогонов ноль» становится находкой только
если файл на ветке по умолчанию не трогали дольше GRACE_DAYS.

СЛАБОСТЬ, КОТОРУЮ НАДО ЗНАТЬ: окно отсчитывается от ПОСЛЕДНЕГО коммита, тронувшего
файл, а не от первого. Косметические правки раз в месяц продлевали бы послабление
бесконечно. Взято именно так, потому что «когда файл впервые появился» требует
пройти всю историю пути, а правка workflow — сама по себе повод посмотреть на него
заново. Если класс проявится — считать от первого появления.

ПОЧЕМУ ЭТО СПРАШИВАЕТСЯ У API, А НЕ ЧИТАЕТСЯ ИЗ ДЕРЕВА
------------------------------------------------------
Факт «файл есть на ветке по умолчанию» в дереве интеграционной ветки не записан.
«Не смог спросить» и «ответ отрицательный» — РАЗНЫЕ исходы, и первый роняет шаг
кодом 2: иначе недоступность API читалась бы как чистота.

Запуск:
  python3 .github/scripts/assert-default-branch-workflows-can-run.py --self-test
  python3 .github/scripts/assert-default-branch-workflows-can-run.py
"""
from __future__ import annotations

import argparse
import contextlib
import datetime as dt
import glob
import io
import json
import os
import subprocess
import sys
from pathlib import Path

# Сколько суток свежеслитый workflow вправе не иметь ни одного прогона.
# Больше самого длинного расписания в дереве (недельный cron) с запасом.
GRACE_DAYS = 30


class Unavailable(Exception):
    """Измерение не сделано. Отличается от «находок нет»."""


def repo_root() -> Path:
    return Path(__file__).resolve().parents[2]


# ── ЧТЕНИЕ ДЕРЕВА ───────────────────────────────────────────────────────────

def only_default_branch_workflows(root: Path) -> list[str]:
    """Файлы, у которых НЕТ ни push, ни pull_request — значит запуск только с
    ветки по умолчанию."""
    try:
        import yaml
    except ImportError as e:  # pragma: no cover - в конвейере yaml стоит
        raise Unavailable(f"нет PyYAML, разобрать триггеры нечем: {e}") from e

    out: list[str] = []
    pats = ["**/.github/workflows/*.yml", "**/.github/workflows/*.yaml"]
    seen: set[str] = set()
    for pat in (".github/workflows/*.yml", ".github/workflows/*.yaml"):
        for p in sorted(glob.glob(str(root / pat))):
            rel = os.path.relpath(p, root).replace(os.sep, "/")
            if rel in seen:
                continue
            seen.add(rel)
            try:
                d = yaml.safe_load(open(p, encoding="utf-8")) or {}
            except Exception as e:
                raise Unavailable(f"{rel}: не разбирается как YAML: {e}") from e
            # `on:` в YAML 1.1 читается как булево True — отсюда двойной ключ.
            on = d.get(True) or d.get("on") or {}
            if not isinstance(on, dict):
                on = {str(on): None}
            if "push" not in on and "pull_request" not in on:
                out.append(rel)
    del pats
    return out


# ── ЧТЕНИЕ API ──────────────────────────────────────────────────────────────

def _gh_json(args: list[str]):
    try:
        p = subprocess.run(["gh", *args], capture_output=True, text=True, timeout=90)
    except (FileNotFoundError, subprocess.SubprocessError) as e:
        raise Unavailable(f"`gh {' '.join(args[:2])}`: {e}") from e
    if p.returncode != 0:
        return None, p.stderr.strip()[:300]
    return p.stdout, None


def read_registered(repo: str) -> set[str]:
    out, err = _gh_json(["api", f"repos/{repo}/actions/workflows",
                         "--paginate", "--jq", ".workflows[].path"])
    if out is None:
        raise Unavailable(f"список зарегистрированных workflow недоступен: {err}")
    paths = {x.strip() for x in out.splitlines() if x.strip()}
    if not paths:
        raise Unavailable("API вернул ПУСТОЙ список workflow — ответ бессмысленный")
    return paths


def read_present_on_default(repo: str, branch: str, path: str) -> bool:
    out, _err = _gh_json(["api", f"repos/{repo}/contents/{path}?ref={branch}",
                          "--jq", ".sha"])
    return bool(out and out.strip())


def read_run_count(repo: str, path: str) -> int:
    name = path.rsplit("/", 1)[-1]
    out, err = _gh_json(["api", f"repos/{repo}/actions/workflows/{name}/runs?per_page=1",
                         "--jq", ".total_count"])
    if out is None:
        raise Unavailable(f"число прогонов {name} недоступно: {err}")
    try:
        return int(out.strip())
    except ValueError as e:
        raise Unavailable(f"число прогонов {name} не разбирается: {out!r}") from e


def read_days_since_touch(repo: str, branch: str, path: str) -> float | None:
    out, _err = _gh_json(["api",
                          f"repos/{repo}/commits?sha={branch}&path={path}&per_page=1",
                          "--jq", ".[0].commit.committer.date"])
    if not out or not out.strip():
        return None
    try:
        when = dt.datetime.fromisoformat(out.strip().replace("Z", "+00:00"))
    except ValueError:
        return None
    return (dt.datetime.now(dt.timezone.utc) - when).total_seconds() / 86400.0


# ── СВЕРКА (чистая функция — её и подаёт самопроверка) ──────────────────────

def adjudicate(observations: list[dict]) -> tuple[int, str]:
    """observations: [{path, present, registered, runs, days_since_touch}]"""
    log = io.StringIO()
    w = log.write

    w("===== workflow «только с ветки по умолчанию»: перепись =====\n")
    w(f"файлов этого класса в дереве: {len(observations)}\n")

    findings: list[str] = []
    introduced = healthy = waiting = 0

    for o in observations:
        p = o["path"]

        if not o["present"]:
            # ВВЕДЕНИЕ. Единственный способ, которым такой workflow появляется.
            introduced += 1
            w(f"  {p}: ВВОДИТСЯ — файла на ветке по умолчанию ещё нет, "
              f"регистрация наступит слиянием\n")
            continue

        if not o["registered"]:
            findings.append(
                f"{p}: файл ЕСТЬ на ветке по умолчанию, но workflow НЕ "
                f"зарегистрирован — его триггеры (schedule/workflow_dispatch/"
                f"workflow_run) не поднялись, значит он не запускается и не может. "
                f"«Ноль находок» у него означает «контроля не было». Разбирать надо "
                f"файл (синтаксис, имя, расположение), а не процесс слияния")
            continue

        runs = o["runs"]
        if runs > 0:
            healthy += 1
            w(f"  {p}: зарегистрирован, прогонов {runs}\n")
            continue

        age = o.get("days_since_touch")
        if age is not None and age <= GRACE_DAYS:
            waiting += 1
            w(f"  {p}: зарегистрирован, прогонов 0 — ждёт первого срабатывания "
              f"(на ветке по умолчанию {age:.1f} сут, послабление {GRACE_DAYS})\n")
            continue

        seen = "неизвестно" if age is None else f"{age:.0f} сут"
        findings.append(
            f"{p}: зарегистрирован, но прогонов НОЛЬ, а на ветке по умолчанию его "
            f"не трогали {seen} (послабление {GRACE_DAYS} сут истекло). Регистрация "
            f"отвечает «может запускаться», а не «запускался»: расписание, которое "
            f"не совпадает никогда, и `workflow_run` по прогону, который не "
            f"завершается, оба регистрируются. Контроля не было")

    w(f"\n===== ИТОГ: класса {len(observations)}; вводится {introduced}; "
      f"работает {healthy}; ждёт первого прогона {waiting}; "
      f"находок {len(findings)} =====\n")

    # Ноль осмотренного — ОТКАЗ. Пустой обход отчитался бы «чисто» ровно тогда,
    # когда сломан он сам.
    if not observations:
        w("ОТКАЗ: в дереве нет ни одного workflow этого класса — обход сломан либо "
          "они переехали. Это не «находок нет», это «прочитано ноль».\n")
        return 2, log.getvalue()

    if findings:
        w(f"ПРОВАЛ: {len(findings)} находк(и)\n")
        for x in findings:
            w(f"  - {x}\n")
        return 1, log.getvalue()

    w(f"PASS: все {len(observations)} осмотрены; контроль без прогонов не выдаётся "
      f"за чистоту\n")
    return 0, log.getvalue()


def execute(repo: str, branch: str, root: Path) -> int:
    try:
        paths = only_default_branch_workflows(root)
        if not paths:
            print("ОТКАЗ: workflow этого класса в дереве нет — обход сломан либо они "
                  "переехали. Это не «находок нет», это «прочитано ноль».",
                  file=sys.stderr)
            return 2
        registered = read_registered(repo)
        obs = []
        for p in paths:
            present = read_present_on_default(repo, branch, p)
            reg = p in registered
            obs.append({
                "path": p,
                "present": present,
                "registered": reg,
                "runs": read_run_count(repo, p) if (present and reg) else 0,
                "days_since_touch": read_days_since_touch(repo, branch, p) if present else None,
            })
    except Unavailable as e:
        print("ИЗМЕРЕНИЕ НЕ СДЕЛАНО (это НЕ «находок нет»):", file=sys.stderr)
        print(f"  {e}", file=sys.stderr)
        return 2

    rc, out = adjudicate(obs)
    (sys.stdout if rc == 0 else sys.stderr).write(out)
    return rc


# ── ДОКАЗАТЕЛЬСТВО ИНЪЕКЦИЕЙ, В ОБЕ СТОРОНЫ ─────────────────────────────────

def _o(path, present=True, registered=True, runs=5, age=1.0):
    return {"path": path, "present": present, "registered": registered,
            "runs": runs, "days_since_touch": age}


def self_test() -> int:
    failures: list[str] = []

    def check(label: str, cond: bool, detail: str = "") -> None:
        if cond:
            print(f"  ок     {label}")
        else:
            print(f"  ПРОВАЛ {label}  {detail}")
            failures.append(label)

    healthy = _o(".github/workflows/live.yml", runs=10, age=200)

    print("(a) НОВЫЙ файл, которого нет на ветке по умолчанию — гейт МОЛЧИТ")
    # Ровно тот случай, на котором прежняя редакция краснела, предлагая два
    # недостижимых исхода.
    rc, out = adjudicate([healthy, _o(".github/workflows/new.yml", present=False)])
    check("молчит на вводимом workflow", rc == 0, out)
    check("называет его вводимым", "ВВОДИТСЯ" in out, out)
    check("перепись отличает введение от работающего",
          "вводится 1" in out and "работает 1" in out, out)

    print("(b) файл ЛЕЖИТ в стволе и не зарегистрирован — гейт КРАСНЕЕТ")
    rc, out = adjudicate([healthy, _o(".github/workflows/stuck.yml", registered=False)])
    check("краснеет на настоящей находке", rc == 1, out)
    check("называет координату", "stuck.yml" in out, out)
    check("указывает разбирать файл, а не процесс", "а не процесс слияния" in out, out)

    print("(c) зарегистрирован, прогонов ноль, ждёт первого срабатывания — молчит")
    rc, out = adjudicate([healthy, _o(".github/workflows/fresh.yml", runs=0, age=2.0)])
    check("молчит на свежеслитом", rc == 0, out)
    check("считает его ожидающим", "ждёт первого прогона 1" in out, out)

    print("(d) зарегистрирован, прогонов ноль, послабление истекло — краснеет")
    rc, out = adjudicate([healthy, _o(".github/workflows/dead.yml", runs=0, age=99.0)])
    check("краснеет на контроле без прогонов", rc == 1, out)
    check("называет его", "dead.yml" in out, out)
    check("отличает «может запускаться» от «запускался»",
          "а не «запускался»" in out, out)

    print("(e) граница послабления — ровно GRACE_DAYS ещё молчит, за ним краснеет")
    rc, _ = adjudicate([healthy, _o(".github/workflows/b.yml", runs=0, age=float(GRACE_DAYS))])
    check(f"на {GRACE_DAYS} сут молчит", rc == 0)
    rc, _ = adjudicate([healthy, _o(".github/workflows/b.yml", runs=0, age=GRACE_DAYS + 0.5)])
    check(f"на {GRACE_DAYS}+ сут краснеет", rc == 1)

    print("(f) возраст неизвестен при нуле прогонов — находка, а не молчание")
    rc, out = adjudicate([healthy, _o(".github/workflows/x.yml", runs=0, age=None)])
    check("не выдаёт незнание за чистоту", rc == 1, out)
    check("говорит, что возраст неизвестен", "неизвестно" in out, out)

    print("(g) здоровое дерево — молчит")
    rc, out = adjudicate([healthy, _o(".github/workflows/two.yml", runs=3, age=300)])
    check("молчит", rc == 0, out)

    print("(h) пустой обход — ОТКАЗ, а не успех")
    rc, out = adjudicate([])
    check("отвергает беспредметную перепись", rc == 2, out)
    check("говорит «прочитано ноль»", "прочитано ноль" in out, out)

    print("(i) недоступный API — третий исход, не зелёный")
    buf = io.StringIO()
    with contextlib.redirect_stdout(buf), contextlib.redirect_stderr(buf):
        rc = execute("owner/repo-that-does-not-exist", "main", Path("/nonexistent"))
    check("недоступность даёт код 2", rc == 2, buf.getvalue())

    print("(j) обход дерева читает ТРИГГЕРЫ, а не имена файлов")
    import tempfile
    import shutil
    root = Path(tempfile.mkdtemp(prefix="wfgate-"))
    try:
        wf = root / ".github" / "workflows"
        wf.mkdir(parents=True)
        (wf / "sched.yml").write_text("name: s\non:\n  schedule:\n    - cron: '0 6 * * 1'\njobs: {}\n")
        (wf / "onpr.yml").write_text("name: p\non:\n  pull_request:\njobs: {}\n")
        (wf / "onpush.yml").write_text("name: u\non:\n  push:\n    branches: [main]\njobs: {}\n")
        got = only_default_branch_workflows(root)
        check("берёт только schedule/dispatch-класс",
              got == [".github/workflows/sched.yml"], got)
    finally:
        shutil.rmtree(root, ignore_errors=True)

    print()
    if failures:
        print(f"САМОПРОВЕРКА ПРОВАЛЕНА: {len(failures)} — {', '.join(failures)}",
              file=sys.stderr)
        return 1
    print("ДОКАЗАНО: гейт молчит на вводимом файле и на свежеслитом, краснеет на "
          "лежащем в стволе без регистрации и на зарегистрированном без прогонов, "
          "отвергает пустой обход и недоступный API.")
    return 0


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--repo", default=os.environ.get("GITHUB_REPOSITORY",
                                                     "PRO-Robotech/kacho"))
    ap.add_argument("--branch", default="main")
    ap.add_argument("--root", default=None)
    ap.add_argument("--self-test", action="store_true",
                    help="доказать инъекцией: краснеет на дефекте, молчит на законной форме")
    args = ap.parse_args(argv)
    if args.self_test:
        return self_test()
    return execute(args.repo, args.branch,
                   Path(args.root).resolve() if args.root else repo_root())


if __name__ == "__main__":
    sys.exit(main())
