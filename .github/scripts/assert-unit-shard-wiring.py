#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Гейт: распил юнитов на шарды провязан так, что потеря пакета НЕВОЗМОЖНА молча.

ПРЕДМЕТ
-------
Юниты гонялись одним заданием: `make test-unit` по всему дереву. Замер на
завершённых прогонах ствола (`gh api .../actions/runs/<id>/jobs`): задание
`build · vet · gofmt · test -race` — 1455…1539 с, из них шаг `test -race -short
(unit)` — 1329…1399 с, то есть 89…91 % времени приходится на ОДИН шаг. Остальные
шаги того же задания: build 58…61 с, vet 43…46 с, checkout 9…11 с, gofmt 2 с.

Распил на параллельные шарды меняет вердикт с «один шаг сказал да» на «сказали
все шарды», и вместе с выигрышем по времени заводит ТРИ класса тихого зелёного,
которых до распила не существовало:

  1. ПАКЕТ НЕ ПОПАЛ НИ В ОДИН ШАРД. Все шарды зелены, пакет не исполнялся вовсе.
     Пропуск не есть проход: «ноль упавших» про него не говорит НИЧЕГО;
  2. ШАРД НЕ ИСПОЛНИЛСЯ. Задание снято по сроку, отменено, не запустилось —
     описи от него нет. Это третья категория исхода, и в проход она не
     засчитывается;
  3. СВОДНЫЙ ВЕРДИКТ ЗЕЛЁН ПРИ НЕИСПОЛНИВШЕМСЯ ШАРДЕ. Работа без упавших шагов
     выходит `success`; без сведения «зелено» становится утверждением, которое
     НИКТО не делает.

ЧТО ЭТОТ ГЕЙТ СУДИТ
-------------------
РАЗОБРАННОЕ объявление процесса (`yaml.safe_load`), а не его текст. Имена шагов
и ключи заданий встречаются в комментариях — в этом файле в том числе, — поэтому
проверка подстрокой краснела бы на собственном объяснении. Судятся отношения:
кто чей `needs`, откуда берётся матрица, что загружается и что скачивается, какие
исходы работ доезжают до сведения.

ЧЕГО ЭТОТ ГЕЙТ НЕ СУДИТ, И ЭТО НАЗВАНО ПРЯМО
--------------------------------------------
Он не проверяет, что раздача пакетов по шардам ПОЛНА на сегодняшнем дереве, — это
предмет `unit-shards.py --census`, и его вердикт считается числами. Здесь
проверяется только то, что такой шаг в конвейере ЕСТЬ и стоит до матрицы: гейт
полноты, который никто не запускает, полноты не держит.

Он не проверяет и истинность сведения — это предмет
`aggregate-unit-shards.py --self-test`. Здесь проверяется провязка: что сведение
получает исходы ОБЕИХ работ параметрами (`needs.<job>.result`), потому что
установить «шард не запускался» своими силами свод не может — следов у «не
запускался» и у «исполнялся и не доехал» одинаково ноль.

Запуск:
  python3 .github/scripts/assert-unit-shard-wiring.py --self-test
  python3 .github/scripts/assert-unit-shard-wiring.py
"""
from __future__ import annotations

import argparse
import copy
import re
import sys
from pathlib import Path

import yaml

CI = ".github/workflows/ci.yaml"
WORKFLOW_DIR = ".github/workflows"

PLAN_SCRIPT = ".github/scripts/unit-shards.py"
AGGREGATE_SCRIPT = ".github/scripts/aggregate-unit-shards.py"
VERDICT_SCRIPT = ".github/scripts/go-test-verdict.py"
WIRING_SCRIPT = ".github/scripts/assert-unit-shard-wiring.py"

# Самопробы, которые ОБЯЗАНЫ идти в задании плана до выдачи матрицы. Доказательство
# инъекцией, лежащее в дереве и не исполняемое конвейером, — текст, а не проверка.
REQUIRED_SELF_TESTS = (PLAN_SCRIPT, AGGREGATE_SCRIPT, WIRING_SCRIPT)

MATRIX_RE = re.compile(r"\$\{\{\s*fromJSON\(\s*needs\.([A-Za-z0-9_-]+)\.outputs\.matrix\s*\)\s*\}\}")
DIGIT_RE = re.compile(r"\d")


def steps_of(job: dict) -> list[dict]:
    return [s for s in (job.get("steps") or []) if isinstance(s, dict)]


def run_text(job: dict) -> str:
    """Весь исполняемый текст задания одной строкой. Только `run:` — `name:` и
    комментарии исполняемой частью не являются, и судить по ним значило бы судить
    по прозе."""
    return "\n".join(str(s.get("run") or "") for s in steps_of(job))


def uses_of(job: dict) -> list[tuple[str, dict]]:
    out = []
    for s in steps_of(job):
        u = s.get("uses")
        if u:
            out.append((str(u), s.get("with") or {}))
    return out


def find_plan(jobs: dict) -> str | None:
    """Задание плана — то, что ОТДАЁТ матрицу и производит её разбором дерева."""
    for jid, job in jobs.items():
        if not isinstance(job, dict):
            continue
        if "matrix" not in (job.get("outputs") or {}):
            continue
        if f"{PLAN_SCRIPT} --matrix" in run_text(job):
            return jid
    return None


def find_shard(jobs: dict) -> tuple[str | None, str | None]:
    """Задание шарда — то, чья матрица ВЫВЕДЕНА из выхода плана.

    Возвращает (id задания, id задания-плана из выражения матрицы). Второе нужно
    отдельно: матрица, взятая из ЧУЖОГО плана, провязкой не является."""
    for jid, job in jobs.items():
        if not isinstance(job, dict):
            continue
        m = ((job.get("strategy") or {}).get("matrix"))
        if not isinstance(m, str):
            continue
        hit = MATRIX_RE.search(m)
        if hit:
            return jid, hit.group(1)
    return None, None


def find_summary(jobs: dict) -> str | None:
    for jid, job in jobs.items():
        if not isinstance(job, dict):
            continue
        if AGGREGATE_SCRIPT in run_text(job) and "--self-test" not in run_text(job).replace(
                f"{AGGREGATE_SCRIPT} --self-test", ""):
            # Сводит тот, кто зовёт агрегатор НЕ самопробой. Самопроба идёт в
            # плане, и спутать её со сведением нельзя: у неё нет ни описей, ни
            # исходов работ.
            return jid
        if AGGREGATE_SCRIPT in run_text(job) and "--plan-result" in run_text(job):
            return jid
    return None


def needs_of(job: dict) -> list[str]:
    n = job.get("needs")
    if n is None:
        return []
    if isinstance(n, str):
        return [n]
    return [str(x) for x in n]


def artifact_names(job: dict, action: str) -> list[str]:
    out = []
    for u, w in uses_of(job):
        if action in u:
            for key in ("name", "pattern"):
                if w.get(key):
                    out.append(str(w[key]))
    return out


def adjudicate(ci: dict, workflows: dict[str, dict]) -> tuple[list[str], dict[str, int]]:
    """Находки и объём осмотренного. Чистая функция — самопроба подаёт свои данные."""
    findings: list[str] = []
    jobs = ci.get("jobs") or {}
    seen = {"jobs": len(jobs),
            "steps": sum(len(steps_of(j)) for j in jobs.values() if isinstance(j, dict)),
            "workflows": len(workflows)}

    if not jobs:
        return ["ОТКАЗ: в объявлении ноль заданий — обход пуст, и «находок нет» "
                "здесь означало бы «прочитано ноль»"], seen

    plan = find_plan(jobs)
    shard, shard_plan_ref = find_shard(jobs)
    summary = find_summary(jobs)

    if plan is None:
        findings.append(
            f"ПЛАНА НЕТ: ни одно задание не отдаёт `outputs.matrix`, производя её "
            f"вызовом `{PLAN_SCRIPT} --matrix`. Значит перечень шардов либо выписан "
            f"в объявлении — и устареет при заведении следующей службы, — либо "
            f"распила нет вовсе")
    if shard is None:
        findings.append(
            "ШАРДОВ НЕТ: ни одно задание не берёт матрицу из "
            "`${{ fromJSON(needs.<план>.outputs.matrix) }}`. Матрица, записанная "
            "списком, есть выписанный перечень: пакет новой службы не попадёт ни в "
            "один шард и его отсутствие будет неотличимо от прохода")
    if summary is None:
        findings.append(
            f"СВЕДЕНИЯ НЕТ: ни одно задание не зовёт `{AGGREGATE_SCRIPT}` с исходами "
            f"работ. Без сведения «зелено» — утверждение, которого никто не делает: "
            f"работа без упавших шагов выходит success, в том числе когда шардов "
            f"исполнилось ноль")

    # ── провязка плана ──────────────────────────────────────────────────────
    if plan is not None:
        ptext = run_text(jobs[plan])
        if f"{PLAN_SCRIPT} --census" not in ptext:
            findings.append(
                f"ПОЛНОТА НЕ СЧИТАЕТСЯ: задание `{plan}` выдаёт матрицу, но не зовёт "
                f"`{PLAN_SCRIPT} --census`. Перепись «в дереве N, роздано M» — "
                f"единственное, чем потеря пакета отличается от прохода")
        for script in REQUIRED_SELF_TESTS:
            if f"{script} --self-test" not in ptext:
                findings.append(
                    f"САМОПРОБА НЕ ИСПОЛНЯЕТСЯ: `{script} --self-test` не вызвана в "
                    f"задании `{plan}`. Доказательство инъекцией, лежащее в дереве и "
                    f"никем не запускаемое, — текст, а не проверка")
        # Порядок значим: гейт полноты, стоящий ПОСЛЕ выдачи матрицы, судит уже
        # разосланное, и шарды успевают уехать по непроверенной раздаче.
        order = [i for i, s in enumerate(steps_of(jobs[plan]))
                 if f"{PLAN_SCRIPT} --census" in str(s.get("run") or "")]
        emit = [i for i, s in enumerate(steps_of(jobs[plan]))
                if f"{PLAN_SCRIPT} --matrix" in str(s.get("run") or "")]
        if order and emit and min(order) > min(emit):
            findings.append(
                f"ПОРЯДОК ОБРАТНЫЙ: в задании `{plan}` перепись полноты стоит ПОСЛЕ "
                f"выдачи матрицы. Шарды уедут по раздаче, которую ещё не судили")

    # ── провязка шардов ─────────────────────────────────────────────────────
    if shard is not None:
        sjob = jobs[shard]
        if plan is not None and shard_plan_ref != plan:
            findings.append(
                f"МАТРИЦА ИЗ ЧУЖОГО ПЛАНА: `{shard}` читает "
                f"`needs.{shard_plan_ref}.outputs.matrix`, а план — `{plan}`")
        if plan is not None and plan not in needs_of(sjob):
            findings.append(
                f"ШАРД НЕ ЖДЁТ ПЛАНА: у `{shard}` нет `needs: {plan}`. Матрица "
                f"вычисляется из выхода задания, которое могло не отработать")
        if (sjob.get("strategy") or {}).get("fail-fast") is not False:
            findings.append(
                f"FAIL-FAST НЕ СНЯТ у `{shard}`: первый упавший шард отменяет "
                f"остальные, и они приходят третьей категорией — вердикта о своих "
                f"пакетах не даёт НИ ОДИН, а причина у этого одна и внешняя")
        stext = run_text(sjob)
        if "matrix.shard.id" not in stext:
            findings.append(
                f"ШАРД НЕ ПОЛУЧАЕТ СВОЕЙ ДОЛИ: в `run:` задания `{shard}` нет "
                f"`matrix.shard.id`. Задание, гоняющее всё дерево в каждой полосе, "
                f"стоит дороже нераспиленного и вердикта о раздаче не даёт")
        if not artifact_names(sjob, "upload-artifact"):
            findings.append(
                f"ОПИСЬ НЕ ЗАГРУЖАЕТСЯ: `{shard}` не отдаёт артефакта. Свод считает "
                f"по описям; шард без описи неотличим от неисполнившегося")
        # Опись обязана уезжать и с КРАСНОГО шарда: иначе падение продукта
        # приедет в свод как «шард не отчитался», то есть причина подменится.
        for s in steps_of(sjob):
            u = str(s.get("uses") or "")
            if "upload-artifact" in u:
                cond = str(s.get("if") or "")
                if "cancelled()" not in cond and "always()" not in cond:
                    findings.append(
                        f"ОПИСЬ ТЕРЯЕТСЯ НА КРАСНОМ: шаг загрузки в `{shard}` без "
                        f"`if: !cancelled()`. Условие без функции состояния "
                        f"вычисляется как `success()`, поэтому упавший шард описи "
                        f"не отдаёт, и падение продукта приезжает в свод как "
                        f"«шард не отчитался»")

    # ── провязка сведения ───────────────────────────────────────────────────
    if summary is not None:
        sm = jobs[summary]
        for dep in (plan, shard):
            if dep is not None and dep not in needs_of(sm):
                findings.append(
                    f"СВОД НЕ ЖДЁТ `{dep}`: без `needs` он выносит вердикт до того, "
                    f"как работа отчиталась")
        cond = str(sm.get("if") or "")
        if "cancelled()" not in cond:
            findings.append(
                f"СВОД ГАСНЕТ НА КРАСНОМ: у `{summary}` нет `if: !cancelled()`. "
                f"Задание с непройденным `needs` пропускается, а пропущенное "
                f"обязательное задание не краснеет — оно молчит")
        mtext = run_text(sm)
        for jid, flag in ((plan, "--plan-result"), (shard, "--shard-result")):
            if jid is None:
                continue
            if f"needs.{jid}.result" not in mtext or flag not in mtext:
                findings.append(
                    f"ИСХОД РАБОТЫ НЕ ДОЕЗЖАЕТ: свод не получает "
                    f"`{flag} ${{{{ needs.{jid}.result }}}}`. Установить «шард не "
                    f"запускался» своими силами он не может: следов у «не "
                    f"запускался» и у «исполнялся и не доехал» одинаково ноль")
        pattern = artifact_names(sm, "download-artifact")
        produced = []
        if shard is not None:
            produced = artifact_names(jobs[shard], "upload-artifact")
        if pattern and produced:
            def matches(pat: str, name: str) -> bool:
                # Имя артефакта несёт подстановку матрицы; сверяется ПРЕФИКС до
                # первой подстановки — он и есть то, что образец обязан поймать.
                head = name.split("${{")[0]
                return pat.rstrip("*").startswith(pat.rstrip("*")) and head.startswith(
                    pat.rstrip("*"))
            if not any(matches(p, n) for p in pattern for n in produced):
                findings.append(
                    f"ОБРАЗЕЦ НЕ ЛОВИТ ОПИСЬ: свод скачивает {pattern}, шард "
                    f"загружает {produced}. Скачано будет ноль описей, и свод "
                    f"объявит непроведённым то, что проведено")

    # ── имя не несёт числа (kacho#2587) ─────────────────────────────────────
    for jid in (plan, shard, summary):
        if jid is None:
            continue
        nm = str(jobs[jid].get("name") or "")
        if DIGIT_RE.search(nm):
            findings.append(
                f"ЧИСЛО В ИМЕНИ `{jid}`: «{nm}». Имя задания есть имя обязательного "
                f"контекста защиты ствола; при смене состава шардов объявленный "
                f"контекст перестанет производиться и запрёт КАЖДЫЙ запрос на "
                f"слияние, включая тот, что состав меняет (kacho#2587)")

    # ── латиница в ключах и в `id` шагов (ban #17) ───────────────────────────
    for jid in (plan, shard, summary):
        if jid is None:
            continue
        if not jid.isascii():
            findings.append(
                f"КЛЮЧ ЗАДАНИЯ НЕ ЛАТИНИЦЕЙ: `{jid}`. Кириллический ключ делает "
                f"объявление неразбираемым: процесс не запускается вовсе, заданий "
                f"ноль, а в ответе API вместо имени приходит путь к файлу")
        for s in steps_of(jobs[jid]):
            sid = s.get("id")
            if sid is not None and not str(sid).isascii():
                findings.append(
                    f"`id` ШАГА НЕ ЛАТИНИЦЕЙ в `{jid}`: `{sid}` — то же следствие")

    # ── нераспиленного прогона не осталось ни в одном процессе ──────────────
    for wname, doc in sorted(workflows.items()):
        for jid, job in (doc.get("jobs") or {}).items():
            if not isinstance(job, dict):
                continue
            if wname == CI and jid == shard:
                continue
            for s in steps_of(job):
                text = str(s.get("run") or "")
                for line in text.split("\n"):
                    if "make test-unit" not in line:
                        continue
                    if "SHARD" in line:
                        continue
                    findings.append(
                        f"НЕРАСПИЛЕННЫЙ ПРОГОН ОСТАЛСЯ: `{wname}` задание `{jid}` "
                        f"зовёт `make test-unit` без `SHARD`. Это те же 1329…1399 с "
                        f"одним шагом — распил оплачен и не получен")

    return findings, seen


# ─── чтение дерева ──────────────────────────────────────────────────────────

def read_workflows(root: Path) -> dict[str, dict]:
    out: dict[str, dict] = {}
    d = root / WORKFLOW_DIR
    for p in sorted(d.iterdir()) if d.is_dir() else []:
        if p.suffix not in (".yml", ".yaml"):
            continue
        try:
            doc = yaml.safe_load(p.read_text(encoding="utf-8"))
        except (OSError, yaml.YAMLError) as e:
            raise SystemExit(
                f"ОТКАЗ: `{p}` не разбирается ({e}). Неразбираемое объявление не "
                f"запускается вовсе — заданий ноль, и это не «находок нет»")
        if isinstance(doc, dict):
            out[str(p.relative_to(root))] = doc
    return out


# ─── самопроба ──────────────────────────────────────────────────────────────

def _legal() -> tuple[dict, dict[str, dict]]:
    """Законный близнец: провязка, которую этот гейт обязан пропускать молча."""
    ci = {
        "jobs": {
            "unit-plan": {
                "name": "план шардов юнитов",
                "outputs": {"matrix": "${{ steps.matrix.outputs.matrix }}"},
                "steps": [
                    {"run": f"python3 {WIRING_SCRIPT} --self-test\n"
                            f"python3 {WIRING_SCRIPT}\n"},
                    {"run": f"python3 {PLAN_SCRIPT} --self-test\n"},
                    {"run": f"python3 {AGGREGATE_SCRIPT} --self-test\n"},
                    {"run": f"python3 {PLAN_SCRIPT} --census --out plan.json\n"},
                    {"id": "matrix",
                     "run": f'python3 {PLAN_SCRIPT} --matrix >> "$GITHUB_OUTPUT"\n'},
                    {"uses": "actions/upload-artifact@dead", "if": "${{ !cancelled() }}",
                     "with": {"name": "unit-shard-plan", "path": "plan.json"}},
                ],
            },
            "unit-shard": {
                "name": "юниты ${{ matrix.shard.id }}",
                "needs": "unit-plan",
                "strategy": {"fail-fast": False,
                             "matrix": "${{ fromJSON(needs.unit-plan.outputs.matrix) }}"},
                "steps": [
                    {"run": f"python3 {VERDICT_SCRIPT} --self-test\n"},
                    {"run": 'make test-unit SHARD="${{ matrix.shard.id }}"\n'},
                    {"uses": "actions/upload-artifact@dead", "if": "${{ !cancelled() }}",
                     "with": {"name": "unit-census-${{ matrix.shard.id }}",
                              "path": "unit-censuses/"}},
                ],
            },
            "unit-verdict": {
                "name": "сводный вердикт юнитов (все шарды)",
                "needs": ["unit-plan", "unit-shard"],
                "if": "${{ !cancelled() }}",
                "steps": [
                    {"uses": "actions/download-artifact@dead",
                     "with": {"pattern": "unit-", "path": "in"}},
                    {"run": f"python3 {AGGREGATE_SCRIPT} --dir in "
                            "--plan-result '${{ needs.unit-plan.result }}' "
                            "--shard-result '${{ needs.unit-shard.result }}'\n"},
                ],
            },
        }
    }
    return ci, {CI: ci}


def _case(name: str, ci: dict, wfs: dict[str, dict], expect_red: bool,
          needle: str | None) -> bool:
    findings, seen = adjudicate(ci, wfs)
    red = bool(findings)
    ok = (red == expect_red) and (needle is None or any(needle in f for f in findings))
    print(f"  [{'OK ' if ok else 'ОТКАЗ'}] {name}: находок {len(findings)} "
          f"(ждали {'красное' if expect_red else 'зелёное'}), осмотрено "
          f"заданий {seen['jobs']}")
    if not ok:
        for f in findings:
            print("      · " + f.replace("\n", " ")[:200])
    return ok


def self_test() -> int:
    print("самопроба провязки распила: инъекция в обе стороны по каждой оси")
    ok = True

    ci, wfs = _legal()
    ok &= _case("контроль: законная провязка → зелено", ci, wfs, False, None)

    def broken(mutate) -> tuple[dict, dict[str, dict]]:
        c, _ = _legal()
        mutate(c)
        return c, {CI: c}

    # 1. Матрица выписана списком вместо вывода из плана.
    c, w = broken(lambda c: c["jobs"]["unit-shard"]["strategy"].__setitem__(
        "matrix", {"shard": [{"id": "vpc"}, {"id": "compute"}]}))
    ok &= _case("матрица списком → красно", c, w, True, "ШАРДОВ НЕТ")

    # 2. Перепись полноты не вызвана: потеря пакета станет неотличима от прохода.
    def drop_census(c):
        c["jobs"]["unit-plan"]["steps"] = [
            s for s in c["jobs"]["unit-plan"]["steps"]
            if "--census" not in str(s.get("run") or "")]
    c, w = broken(drop_census)
    ok &= _case("нет переписи полноты → красно", c, w, True, "ПОЛНОТА НЕ СЧИТАЕТСЯ")

    # 3. Перепись ПОСЛЕ выдачи матрицы — судит уже разосланное.
    def reorder(c):
        st = c["jobs"]["unit-plan"]["steps"]
        cen = [s for s in st if "--census" in str(s.get("run") or "")]
        rest = [s for s in st if s not in cen]
        c["jobs"]["unit-plan"]["steps"] = rest + cen
    c, w = broken(reorder)
    ok &= _case("перепись после матрицы → красно", c, w, True, "ПОРЯДОК ОБРАТНЫЙ")

    # 4. Свод не получает исхода работы шардов.
    c, w = broken(lambda c: c["jobs"]["unit-verdict"]["steps"].__setitem__(
        1, {"run": f"python3 {AGGREGATE_SCRIPT} --dir in "
                   "--plan-result '${{ needs.unit-plan.result }}'\n"}))
    ok &= _case("нет --shard-result → красно", c, w, True, "ИСХОД РАБОТЫ НЕ ДОЕЗЖАЕТ")

    # 5. Свод гаснет вместе с красным шардом.
    c, w = broken(lambda c: c["jobs"]["unit-verdict"].pop("if"))
    ok &= _case("свод без !cancelled() → красно", c, w, True, "СВОД ГАСНЕТ")

    # 6. fail-fast отменяет соседние шарды: они приходят третьей категорией.
    c, w = broken(lambda c: c["jobs"]["unit-shard"]["strategy"].pop("fail-fast"))
    ok &= _case("fail-fast не снят → красно", c, w, True, "FAIL-FAST НЕ СНЯТ")

    # 7. Опись не уезжает с красного шарда — причина подменится.
    c, w = broken(lambda c: c["jobs"]["unit-shard"]["steps"][2].pop("if"))
    ok &= _case("загрузка описи без !cancelled() → красно", c, w, True,
                "ОПИСЬ ТЕРЯЕТСЯ НА КРАСНОМ")

    # 8. Шард не получает своей доли — гоняет всё дерево в каждой полосе.
    c, w = broken(lambda c: c["jobs"]["unit-shard"]["steps"].__setitem__(
        1, {"run": "make test-unit\n"}))
    ok &= _case("шард без matrix.shard.id → красно", c, w, True,
                "ШАРД НЕ ПОЛУЧАЕТ СВОЕЙ ДОЛИ")

    # 9. Нераспиленный прогон остался в другом задании.
    def leftover(c):
        c["jobs"]["build-test"] = {"name": "build", "steps": [{"run": "make test-unit\n"}]}
    c, w = broken(leftover)
    ok &= _case("нераспиленный прогон в соседнем задании → красно", c, w, True,
                "НЕРАСПИЛЕННЫЙ ПРОГОН")

    # 10. Число в имени шарда запирает ствол при смене состава.
    c, w = broken(lambda c: c["jobs"]["unit-shard"].__setitem__(
        "name", "юниты 1 из 8"))
    ok &= _case("число в имени шарда → красно", c, w, True, "ЧИСЛО В ИМЕНИ")

    # 11. Кириллица в ключе задания: объявление неразбираемо, заданий ноль.
    def cyrillic(c):
        c["jobs"]["юниты-шард"] = c["jobs"].pop("unit-shard")
        c["jobs"]["unit-verdict"]["needs"] = ["unit-plan", "юниты-шард"]
    c, w = broken(cyrillic)
    ok &= _case("кириллический ключ задания → красно", c, w, True,
                "КЛЮЧ ЗАДАНИЯ НЕ ЛАТИНИЦЕЙ")

    # 12. Самопроба агрегатора не исполняется — доказательство остаётся текстом.
    def drop_selftest(c):
        c["jobs"]["unit-plan"]["steps"] = [
            s for s in c["jobs"]["unit-plan"]["steps"]
            if f"{AGGREGATE_SCRIPT} --self-test" not in str(s.get("run") or "")]
    c, w = broken(drop_selftest)
    ok &= _case("самопроба агрегатора не вызвана → красно", c, w, True,
                "САМОПРОБА НЕ ИСПОЛНЯЕТСЯ")

    # 13. Пустое объявление — обход пуст, и это ОТКАЗ, а не «находок нет».
    ok &= _case("ноль заданий → красно", {"jobs": {}}, {CI: {"jobs": {}}}, True,
                "обход пуст")

    # Законный близнец каждой инъекции — контроль выше. Проверяем отдельно, что
    # ИМЕНИ с цифрой не бывает там, где цифра стоит в имени ШАГА: предмет запрета
    # — имя задания, и расширять его на шаги гейт не вправе.
    c, w = broken(lambda c: c["jobs"]["unit-shard"]["steps"][1].__setitem__(
        "name", "юниты: 206 пакетов дерева"))
    ok &= _case("цифра в имени ШАГА → зелено (предмет запрета — имя задания)",
                c, w, False, None)

    print("самопроба:", "ПРОЙДЕНА" if ok else "ПРОВАЛЕНА")
    return 0 if ok else 1


def main() -> int:
    ap = argparse.ArgumentParser(description="провязка распила юнитов на шарды")
    ap.add_argument("--self-test", action="store_true")
    ap.add_argument("--root", default=None)
    args = ap.parse_args()

    if args.self_test:
        return self_test()

    root = Path(args.root) if args.root else Path(__file__).resolve().parents[2]
    wfs = read_workflows(root)
    ci = wfs.get(CI)
    if ci is None:
        print(f"ОТКАЗ: `{CI}` не найден — судить нечего")
        return 2

    findings, seen = adjudicate(ci, wfs)
    print(f"осмотрено: заданий {seen['jobs']} · шагов {seen['steps']} · "
          f"процессов {seen['workflows']}")
    if not findings:
        print("провязка распила сходится: план выводит раздачу, шарды берут матрицу "
              "из него, свод судит по описям и получает исходы обеих работ")
        return 0
    print(f"НАХОДОК {len(findings)}:")
    for f in findings:
        print("  · " + f)
    return 1


if __name__ == "__main__":
    sys.exit(main())
