#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""assert-stand-precondition-wiring.py — «условие не создано» доезжает до вердикта.

ПРЕДМЕТ (kacho#655, kacho#2250). Подъём стенда тянет зависимости чарта с
ПОСТОРОННИХ хостов. Их недоступность роняла работу целиком: коллекции не
выполнялись вовсе, гейты краснели «нет отчётов», и в сводке PR это читалось как
дефект продукта. `e2e-flow.md` §6 требует обратного: «условие не создано — не
вердикт: отдельный шаг, отдельное сообщение».

ПОЧЕМУ ГЕЙТ ПЕРЕПИСАН (#2250). Прежняя редакция судила ОДНУ работу — шард
сквозного прогона, — и это была не граница, а СЛЕПАЯ ЗОНА: её предпосылка «стенд
поднимает одна работа» была верна на популяции из одной и потому невидима. Замер
на f9a04a5ef: шагов конвейера, поднимающих стенд, ЧЕТЫРЕ; отметку читал ОДИН.
Три остальных отдавали шагу двойку make и краснели — и ровно так упала боевая
посадка на прогоне 34147256284. Узкая популяция предпосылку не подтверждает,
она её СКРЫВАЕТ.

Поэтому набор работ теперь ВЫВОДИТСЯ ИЗ ДЕРЕВА: работой подъёма считается всякая,
где шаг зовёт подъём стенда в позиции команды. Рукописный перечень пришлось бы
дописывать руками ровно тогда же, когда о новой работе забывают.

ЧТО ПРОВЕРЯЕТСЯ — СВЯЗНОСТЬ, А НЕ НАМЕРЕНИЕ. Звеньев шесть, и любое разорванное
возвращает исходный дефект МОЛЧА:

  1. владелец материализации кладёт отметку (доказано его же --self-test:
     реальный helm, недостижимый хост → код 3 и отметка; наш сломанный сабчарт →
     код 1 и НИКАКОЙ отметки);
  2. КАЖДЫЙ шаг подъёма делегирует классификацию ЕДИНСТВЕННОМУ владельцу
     (.github/scripts/stand-up.sh). Развилка, размноженная по работам, разошлась
     бы молча — она уже разошлась: у трёх работ из четырёх её не было вовсе;
  3. владелец различает три исхода — доказано ИСПОЛНЕНИЕМ (его `--self-test`
     запускает настоящий скрипт под оболочкой конвейера и подменяет ровно два
     факта: код команды подъёма и наличие отметки);
  4. каждый шаг, выносящий вердикт О ПРОДУКТЕ, при несозданном условии
     ПРОПУСКАЕТСЯ — иначе он исполнится на мёртвом кластере и покраснеет, и
     чужая недоступность снова прочитается находкой;
  5. ни один шаг ПОСЛЕ подъёма не остаётся без условия вовсе. До #2250 такие
     шаги были безопасны by construction: шаг подъёма выходил ненулевым и работа
     обрывалась сама. Теперь третья категория работу не красит — значит шаг без
     условия дойдёт до МЁРТВОГО кластера. Это то самое, что правка могла бы
     завести незамеченным, поэтому проверяется отдельно;
  6. вызов владельца из тела шага ИСПОЛНИМ, а не только написан. Звено 2 читает
     объявление и отвечает «зовёт ли»; этого мало — вызов бывает написан и
     неисполним (не тот каталог, потерянное `--`), и тогда владелец отказывает
     кодом 2, а выглядит это как срыв подъёма. Поэтому настоящие тела шагов
     ИСПОЛНЯЮТСЯ, по ТРИ прогона на каждое: контроль (подъём отработал) ·
     третья категория (отметка есть — шаг не краснеет) · законный близнец
     (тот же срыв без отметки — краснеет как прежде). Двух прогонов мало:
     без контроля неизвестно, способен ли шаг выйти нулём вообще, а без
     близнеца правка была бы маской.

Звено 4 — то, что ломается само собой: девятая суита заводится копированием
восьмой, и условие в копию не переносят. Поэтому набор шагов-вердиктов
ВЫВОДИТСЯ ИЗ ТОГО, ЧТО ШАГ ЗАПУСКАЕТ, а не сверяется со списком имён.

ЧЕГО ГЕЙТ НЕ ЗАКРЫВАЕТ, сказано прямо. Он не судит, ТЕРПИТ ли отсутствие кластера
шаг, объявивший `always()`: автор такого шага уже назвал условие словом, и
требовать второго объявления значило бы заводить перечень, который стареет. Шаг,
выносящий вердикт о продукте под `always()`, ловится звеном 4 — по тому, что он
запускает.

Коды возврата: 0 — связно; 1 — разрыв; 2 — судить не по чему (не нашлось ни одной
работы подъёма, шага подъёма или ни одного шага-вердикта: «ноль находок» обязано
быть отличимо от «ноль прочитанного»).
"""
from __future__ import annotations

import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile

import yaml

REPO = pathlib.Path(__file__).resolve().parents[2]
WORKFLOWS = REPO / ".github" / "workflows"
OWNER = ".github/scripts/stand-up.sh"
OWNER_PATH = REPO / ".github" / "scripts" / "stand-up.sh"
FLAG = "STAND_PRECONDITION_UNMET"
MARK_NAME = ".kacho-deps-precondition-unmet"

# Подъём стенда в ПОЗИЦИИ КОМАНДЫ. Слова `make dev-up` стоят в дереве десятки раз
# в прозе и в подсказках оператору; находкой считается только вызов.
STAND_CALL = re.compile(
    r"(^|[;&|(]|\$\()[ \t]*(make[ \t]+dev-(prod-)?up\b"
    r"|bash[ \t]+\S*helm-umbrella-deps\.sh"
    r"|\S*stand-up\.sh)",
    re.M,
)

# Шаг выносит вердикт О ПРОДУКТЕ, если запускает одно из этого. Признак — то, что
# шаг ИСПОЛНЯЕТ, а не как он назван: имя переписывают свободно, а запуск нет.
# Запись, которой в дереве не соответствует НИ ОДИН шаг, — находка: послабление и
# перечень обязаны истекать сами.
VERDICT_RUNNERS = (
    "assert-suites-green.sh",              # гейт суиты: «нет отчётов» → красное
    "assert-ban6-external-isolation.py",   # спрашивает листенер, которого нет
    "newman-live.py report",               # печатает «0 отчётов» как потерю
    "newman-shard-run.sh",                 # прогон суит шарда: без кластера падает на первом запросе
    "npx playwright test",                 # то же для консоли
    "assert-console-probes-verdict.py",    # гейт по отчёту проб консоли
    "browser-reach.mjs",                   # браузер идёт на адрес стенда
    "make seed-vpc-pools-check",           # спрашивает кластер о полосе адресов
)
# Шаг, который обязан исполниться ВОПРЕКИ несозданному условию: он и есть тот,
# кто доносит причину до свода. Законный близнец звена 4.
CARRIER_RUNNERS = (
    "shard-verdict.py",            # опись шарда для сводного вердикта
    "console-run-category.py",     # разметчик исхода работы консоли
)
# Работы подъёма, которым делегирование владельцу ещё не провязано. Запись без
# причины и без предмета не заводится, и запись, которой нечего исключать, —
# находка: послабление обязано истекать само.
#
# ВЕДОМОСТЬ ПУСТА, И ЭТО ЦЕЛЬ, А НЕ ПОЛОМКА (kacho#2257). Последняя запись —
# работа `ci.yaml`/`helm` — снята вместе со своим предметом: её шаг материализации
# теперь зовёт владельца, и гейт потребовал бы снятия записи сам («послаблению
# нечего исключать»). Пустая ведомость проходит: перепись ниже печатает
# «послаблений 0», поэтому «ведомость пуста» отличимо от «ведомость не читали»,
# и отказ на достигнутой цели не подталкивает держать запись ради зелёного.
DELEGATION_EXEMPT: dict[tuple[str, str], str] = {}


def workflows() -> list[pathlib.Path]:
    return sorted(p for p in WORKFLOWS.glob("*.y*ml") if p.is_file())


def stand_jobs() -> list[tuple[str, str, list[dict], dict]]:
    """(имя файла, имя работы, шаги, шаг подъёма) — ВЫВЕДЕНО из дерева, не выписано."""
    out: list[tuple[str, str, list[dict], dict]] = []
    for wf in workflows():
        try:
            doc = yaml.safe_load(wf.read_text(encoding="utf-8")) or {}
        except yaml.YAMLError as exc:
            raise SystemExit(f"FATAL: {wf.name} не разобран как YAML: {exc}")
        for jn, job in (doc.get("jobs") or {}).items():
            steps = list((job or {}).get("steps") or [])
            stand = next((s for s in steps if STAND_CALL.search(str(s.get("run") or ""))), None)
            if stand is not None:
                out.append((wf.name, str(jn), steps, stand))
    return out


def guarded(step: dict) -> bool:
    return f"env.{FLAG}" in str(step.get("if") or "")


def calls_owner(step: dict) -> bool:
    return re.search(r"(^|[;&|(]|\$\()[ \t]*\S*stand-up\.sh\b", str(step.get("run") or ""), re.M) is not None


def check() -> tuple[list[str], dict[str, int]]:
    findings: list[str] = []
    seen = {"jobs": 0, "steps": 0, "verdict": 0, "guarded": 0, "carrier": 0, "noif": 0}
    runners_hit = {r: 0 for r in VERDICT_RUNNERS}
    exempt_hit = {k: 0 for k in DELEGATION_EXEMPT}

    for wf, jn, steps, stand in stand_jobs():
        seen["jobs"] += 1
        seen["steps"] += len(steps)
        where = f"{wf} / работа '{jn}'"

        # ── звено 2: делегирование единственному владельцу ────────────────────
        if not calls_owner(stand):
            if (wf, jn) in DELEGATION_EXEMPT:
                exempt_hit[(wf, jn)] += 1
            else:
                findings.append(
                    f"{where}: шаг подъёма «{stand.get('name')}» не зовёт владельца "
                    f"`{OWNER}` — код 3 через `make` не проходит, и чужая недоступность "
                    f"придёт как обычное красное")
        elif (wf, jn) in DELEGATION_EXEMPT:
            findings.append(
                f"{where}: послаблению нечего исключать — шаг подъёма УЖЕ зовёт владельца; "
                f"снимите запись из DELEGATION_EXEMPT, иначе она переживёт свой предмет")
            exempt_hit[(wf, jn)] += 1

        # ЗВЕНО 5 действует ТОЛЬКО там, где подъём делегирован владельцу. У работы
        # с послаблением шаг подъёма по-прежнему выходит ненулевым, и шаг без
        # условия за ним по-прежнему безопасен by construction — работа
        # обрывается сама. Требовать от неё условий значило бы требовать защиты
        # от исхода, которого у неё не бывает.
        exit_zero_on_unmet = calls_owner(stand)

        after = steps[steps.index(stand) + 1:]
        for s in after:
            run, name = str(s.get("run") or ""), str(s.get("name") or s.get("uses") or "?")
            cond = str(s.get("if") or "").strip()

            hits = [r for r in VERDICT_RUNNERS if r in run]
            if hits:
                for r in hits:
                    runners_hit[r] += 1
                seen["verdict"] += 1
                if guarded(s):
                    seen["guarded"] += 1
                else:
                    findings.append(
                        f"{where}: шаг «{name}» выносит вердикт о продукте, но НЕ "
                        f"пропускается при несозданном условии: он исполнится на мёртвом "
                        f"кластере и покраснеет, и недоступность постороннего хоста снова "
                        f"прочитается как дефект продукта")

            # ── звено 5: шаг без условия вовсе ───────────────────────────────
            if not cond and exit_zero_on_unmet:
                seen["noif"] += 1
                findings.append(
                    f"{where}: шаг «{name}» идёт ПОСЛЕ подъёма и не несёт условия вовсе. "
                    f"Пока подъём краснел, такой шаг был безопасен by construction — работа "
                    f"обрывалась сама. Теперь третья категория работу не красит, и шаг "
                    f"дойдёт до мёртвого кластера")

            if any(c in run for c in CARRIER_RUNNERS):
                seen["carrier"] += 1
                if guarded(s):
                    findings.append(
                        f"{where}: шаг «{name}» доносит причину до свода и пропускаться НЕ "
                        f"ДОЛЖЕН: с условием он промолчит, и третья категория до сводки не "
                        f"доедет")

    for r, n in runners_hit.items():
        if n == 0:
            findings.append(
                f"перечень шагов-вердиктов: записи «{r}» в дереве не соответствует ни один "
                f"шаг — она переживает свой предмет и превращается в слепую зону")
    for k, n in exempt_hit.items():
        if n == 0:
            findings.append(
                f"послабление делегирования {k} не нашло своей работы — снимите запись")
    return findings, seen


# ─────────────────────────────────────────────────────────────────────────────
# ЗВЕНО 3 ПРОВЕРЯЕТСЯ ИСПОЛНЕНИЕМ, И ПРОВЕРЯЕТ ЕГО САМ ВЛАДЕЛЕЦ. Поведение
# оболочки читается только запуском, поэтому доказательство живёт там же, где
# логика; здесь оно ЗОВЁТСЯ, а не переписывается — вторая копия разошлась бы с
# первой ровно там, где расхождение не видно.
def owner_self_test() -> tuple[int, str]:
    p = subprocess.run(["bash", str(OWNER_PATH), "--self-test"],
                       capture_output=True, text=True, cwd=str(REPO))
    return p.returncode, p.stdout + p.stderr


# ─────────────────────────────────────────────────────────────────────────────
# ЗВЕНО 6: ВЫЗОВ ВЛАДЕЛЬЦА ИЗ ШАГА ОБЯЗАН БЫТЬ ИСПОЛНИМ, А НЕ ТОЛЬКО ПРИСУТСТВОВАТЬ.
#
# Звено 2 читает объявление и отвечает «зовёт ли». Этого мало: вызов бывает
# написан и неисполним — не тот каталог в `--dir`, потерянное `--`, ярлык,
# съеденный подстановкой. Такой шаг покраснел бы КОДОМ 2 самого владельца
# («предпосылки не выполнены»), и это выглядело бы как срыв подъёма — то есть
# ровно как класс, который гейт и чинит.
#
# Поэтому здесь исполняется НАСТОЯЩЕЕ тело шага под оболочкой конвейера, а
# подменяются ровно два факта: команда подъёма (`make`, `kubectl`, `kind`
# отвечают нулём либо заданным кодом) и наличие отметки. Один факт на случай —
# иначе неизвестно, что дало исход.
def run_step_body(step: dict, marker: bool, make_rc: int) -> tuple[int, str, str]:
    body = re.sub(r"\$\{\{[^}]*\}\}", "SUBST", str(step.get("run") or ""))
    with tempfile.TemporaryDirectory() as td:
        t = pathlib.Path(td)
        (t / "bin").mkdir()
        # Подставляется только ЗАПУСК подъёма; сам владелец — настоящий, он и
        # есть предмет проверки. Каталог `deploy` в подставном дереве обязан
        # существовать: `--dir deploy` без него дал бы код 2, неотличимый от
        # неисполнимого вызова.
        for name in ("make", "kubectl", "kind", "docker", "helm"):
            (t / "bin" / name).write_text(f"#!/bin/sh\nexit {make_rc if name == 'make' else 0}\n",
                                          encoding="utf-8")
            (t / "bin" / name).chmod(0o755)
        (t / "deploy" / "helm" / "umbrella").mkdir(parents=True)
        (t / ".github" / "scripts").mkdir(parents=True)
        shutil.copy2(OWNER_PATH, t / ".github" / "scripts" / "stand-up.sh")
        if marker:
            (t / "deploy" / "helm" / "umbrella" / MARK_NAME).write_text(
                "источник чартов не ответил: https://charts.example.invalid/x\n", encoding="utf-8")
        genv = t / "github-env"; genv.write_text("", encoding="utf-8")
        gsum = t / "github-summary"; gsum.write_text("", encoding="utf-8")
        env = dict(os.environ,
                   PATH=f"{t / 'bin'}{os.pathsep}{os.environ['PATH']}",
                   GITHUB_WORKSPACE=str(t), GITHUB_ENV=str(genv),
                   GITHUB_STEP_SUMMARY=str(gsum), GITHUB_OUTPUT=str(t / "github-output"),
                   CLUSTER_NAME="probe", SERVICES="iam")
        # Та же оболочка, что у конвейера: `defaults.run.shell: bash` — это
        # `bash --noprofile --norc -eo pipefail`. Под ней и обязана исполняться
        # развилка: код возврата, взятый условием продолжения, оборвал бы тело
        # здесь, и ни один случай ниже не прошёл бы.
        p = subprocess.run(["bash", "--noprofile", "--norc", "-eo", "pipefail", "-c", body],
                           cwd=t, env=env, capture_output=True, text=True)
        return p.returncode, p.stdout + p.stderr, genv.read_text(encoding="utf-8") + gsum.read_text(encoding="utf-8")


def check_bodies() -> tuple[list[str], int]:
    """Звено 6: настоящее тело каждого делегирующего шага, исполнено. ТРИ прогона.

    Два было бы недостаточно: без контроля «всё цело» неизвестно, способен ли шаг
    вообще выйти нулём, а без законного близнеца правка превратила бы ЛЮБОЙ срыв
    подъёма в «условие не создано» — то есть завела бы маску.
    """
    findings: list[str] = []
    proven = 0
    for wf, jn, _steps, stand in stand_jobs():
        if not calls_owner(stand):
            continue
        where = f"{wf} / работа '{jn}'"
        proven += 1

        # (1) КОНТРОЛЬ: подъём отработал → шаг зелёный, флага нет.
        rc, out, side = run_step_body(stand, marker=False, make_rc=0)
        if rc != 0 or FLAG in side:
            findings.append(f"{where}: контроль — подъём отработал, а шаг вышел кодом {rc} "
                            f"(флаг {'выставлен' if FLAG in side else 'нет'}). Вызов владельца "
                            f"из шага НЕИСПОЛНИМ, и его отказ читался бы как срыв подъёма")

        # (2) ТРЕТЬЯ КАТЕГОРИЯ: отметка есть → шаг НЕ краснеет, категория названа,
        #     флаг выставлен.
        rc, out, side = run_step_body(stand, marker=True, make_rc=2)
        if rc != 0:
            findings.append(f"{where}: несозданное условие вышло кодом {rc} — работа краснеет, "
                            f"и чужая недоступность снова читается как дефект продукта")
        if FLAG not in side:
            findings.append(f"{where}: несозданное условие не объявило {FLAG} — шаги ниже о нём "
                            f"не узнают и исполнятся на мёртвом кластере")
        if "УСЛОВИЕ НЕ СОЗДАНО" not in out and "НЕ ВЫПОЛНИЛОСЬ" not in side:
            findings.append(f"{where}: категория не названа отдельной строкой — исход прочтётся "
                            f"как обычная краснота")

        # (3) ЗАКОННЫЙ БЛИЗНЕЦ: тот же срыв БЕЗ отметки → красное как прежде.
        rc, out, side = run_step_body(stand, marker=False, make_rc=2)
        if rc == 0 or FLAG in side:
            findings.append(f"{where}: отказ БЕЗ доказательства чужой причины не покраснел "
                            f"(код {rc}) — это маска: любой срыв подъёма стал бы «условием»")
    return findings, proven


def _self_test() -> int:
    ok = True

    def say(good: bool, label: str, detail: str = "") -> None:
        nonlocal ok
        ok = ok and good
        print(f"  {'ОК ' if good else 'ПРОВАЛ'} {label}" + (f" — {detail}" if detail and not good else ""))

    print("=== assert-stand-precondition-wiring.py --self-test ===")

    print("  --- звено 3: владелец различает три исхода (исполнение)")
    rc, out = owner_self_test()
    say(rc == 0, "самопроверка владельца проходит", out[-600:])
    for needle in ("категория green", "категория unmet", "категория red"):
        say(needle in out, f"владелец назвал исход «{needle.split()[-1]}»")

    print("  --- звено 6: настоящие тела шагов, исполнены (три прогона на каждое)")
    bf, proven = check_bodies()
    say(not bf, f"тел шагов делегирующих работ исполнено {proven}, по три прогона каждое",
        "; ".join(bf))
    say(proven >= 3, f"делегирующих работ {proven} — исполнено не одно тело, а все")

    # ИНЪЕКЦИЯ: вызов владельца НАПИСАН, но неисполним (потерян `--`). Звено 2
    # такое пропускает — оно читает объявление; ловит его только исполнение.
    real_bodies = stand_jobs
    def hurt_call():
        out = []
        for wf, jn, steps, stand in real_bodies():
            if not calls_owner(stand):
                out.append((wf, jn, steps, stand)); continue
            s = dict(stand); s["run"] = str(s.get("run") or "").replace(" -- ", " ")
            out.append((wf, jn, [s if x is stand else x for x in steps], s))
        return out
    globals()["stand_jobs"] = hurt_call
    bf2, _ = check_bodies()
    globals()["stand_jobs"] = real_bodies
    say(bool(bf2), "вызов владельца без `--` → находка (объявление цело, исполнение нет)")

    print("  --- звенья 2/4/5: инъекция в дерево (обе стороны)")
    jobs = stand_jobs()
    say(len(jobs) >= 3,
        f"работ подъёма найдено {len(jobs)} — предикат выводит их из дерева",
        f"нашлось {len(jobs)}, ждали не меньше трёх")

    findings, seen = check()
    say(not findings,
        f"дерево как есть — связно (работ {seen['jobs']}, шагов {seen['steps']}, "
        f"вердиктов {seen['verdict']} все с условием, описей {seen['carrier']})",
        "; ".join(findings))

    # ── (а) шаг подъёма перестал звать владельца → находка с именем работы ───
    real = stand_jobs
    def hurt_owner():
        out = []
        for wf, jn, steps, stand in real():
            if (wf, jn) in DELEGATION_EXEMPT:
                out.append((wf, jn, steps, stand)); continue
            s = dict(stand); s["run"] = "make dev-up\n"
            out.append((wf, jn, [s if x is stand else x for x in steps], s))
        return out
    globals()["stand_jobs"] = hurt_owner
    f, _ = check()
    say(sum(1 for x in f if "не зовёт владельца" in x) >= 3,
        "шаг подъёма без владельца → находка по КАЖДОЙ работе")
    globals()["stand_jobs"] = real

    # ── (б) снято условие у шага-вердикта → находка С ЕГО ИМЕНЕМ ─────────────
    victim = ""
    def hurt_guard():
        nonlocal victim
        out = []
        for wf, jn, steps, stand in real():
            new = []
            for s in steps:
                if not victim and any(r in str(s.get("run") or "") for r in VERDICT_RUNNERS) \
                        and s is not stand and guarded(s):
                    victim = str(s.get("name")); s = dict(s); s["if"] = "always()"
                new.append(s)
            out.append((wf, jn, new, stand))
        return out
    globals()["stand_jobs"] = hurt_guard
    f, _ = check()
    globals()["stand_jobs"] = real
    say(bool(victim) and any(victim in x and "вердикт о продукте" in x for x in f),
        "снятое условие у шага-вердикта → находка с его именем", f"жертва {victim!r}")

    # ── (в) законный близнец: шаг описи БЕЗ условия — молчим ─────────────────
    say(not any("доносит причину до свода" in x for x in check()[0]),
        "шаг описи условия не несёт — причина доезжает до свода")

    # ── (г) условие НА шаге описи → находка ──────────────────────────────────
    def hurt_carrier():
        out = []
        for wf, jn, steps, stand in real():
            new = []
            for s in steps:
                if any(c in str(s.get("run") or "") for c in CARRIER_RUNNERS):
                    s = dict(s); s["if"] = f"always() && env.{FLAG} != '1'"
                new.append(s)
            out.append((wf, jn, new, stand))
        return out
    globals()["stand_jobs"] = hurt_carrier
    f, _ = check()
    globals()["stand_jobs"] = real
    say(any("доносит причину до свода" in x for x in f), "условие на шаге описи → находка")

    # ── (д) звено 5: шаг после подъёма без условия → находка ─────────────────
    def hurt_noif():
        out = []
        for wf, jn, steps, stand in real():
            i = steps.index(stand)
            s = dict(steps[i + 1]); s.pop("if", None)
            out.append((wf, jn, steps[:i + 1] + [s] + steps[i + 2:], stand))
        return out
    globals()["stand_jobs"] = hurt_noif
    f, _ = check()
    globals()["stand_jobs"] = real
    say(any("не несёт условия вовсе" in x for x in f),
        "шаг после подъёма без условия → находка")

    # ── (е) самоистечение перечня: несуществующий прогонщик → находка ────────
    VERDICT_RUNNERS_BAK = globals()["VERDICT_RUNNERS"]
    globals()["VERDICT_RUNNERS"] = VERDICT_RUNNERS_BAK + ("no-such-runner-xyz.sh",)
    f, _ = check()
    globals()["VERDICT_RUNNERS"] = VERDICT_RUNNERS_BAK
    say(any("переживает свой предмет" in x for x in f),
        "запись перечня без предмета в дереве → находка (перечень истекает сам)")

    # ── (ж) ВЕДОМОСТЬ ПОСЛАБЛЕНИЙ ПУСТА — И ПОТОМУ ПРОВЕРЯЕТСЯ СИНТЕТИКОЙ ─────
    # Записей в DELEGATION_EXEMPT сегодня НОЛЬ (kacho#2257), то есть обе ветви,
    # придающие записи смысл, на живом дереве не исполняются. Негативное
    # утверждение, лишившееся входа, ЗАМОЛКАЕТ — оно не краснеет и не зеленеет,
    # и отличить это от исправной работы нельзя ничем, кроме подачи входа.
    # Поэтому вход подаётся здесь, а не ждёт следующей записи.
    EXEMPT_BAK = globals()["DELEGATION_EXEMPT"]
    live = next((wf, jn) for wf, jn, _s, st in stand_jobs() if calls_owner(st))
    globals()["DELEGATION_EXEMPT"] = {live: "синтетика самопроверки"}
    f, _ = check()
    say(any("нечего исключать" in x for x in f),
        "послабление на работу, которая УЖЕ делегирует → находка «снимите запись»")
    globals()["DELEGATION_EXEMPT"] = {("no-such-workflow.yml", "no-such-job"): "синтетика"}
    f, _ = check()
    say(any("не нашло своей работы" in x for x in f),
        "послабление без своей работы в дереве → находка (ведомость истекает сама)")
    globals()["DELEGATION_EXEMPT"] = EXEMPT_BAK
    say(not any("нечего исключать" in x or "не нашло своей работы" in x for x in check()[0]),
        "пустая ведомость послаблений — молчим: цель достигнута, а не сломана")

    print("самопроверка:", "PASS" if ok else "FAIL")
    return 0 if ok else 1


def main() -> int:
    if not WORKFLOWS.is_dir():
        print(f"FATAL: нет {WORKFLOWS} — судить не по чему", file=sys.stderr)
        return 2
    if not OWNER_PATH.is_file():
        print(f"FATAL: нет владельца {OWNER} — звено 2 проверять не на чем", file=sys.stderr)
        return 2
    if shutil.which("bash") is None:
        print("FATAL: нужен bash — поведение владельца проверяется исполнением", file=sys.stderr)
        return 2

    jobs = stand_jobs()
    if not jobs:
        print("FATAL: работ подъёма стенда в дереве не нашлось ни одной — «связно» здесь "
              "значило бы «ничего не прочитано»", file=sys.stderr)
        return 2

    rc, out = owner_self_test()
    if rc != 0:
        print("FAIL: владелец подъёма не доказал, что различает три исхода:")
        print(out)
        return 1

    findings, seen = check()
    body_findings, proven = check_bodies()
    findings += body_findings
    print(f"осмотрено: работ подъёма {seen['jobs']} "
          f"({', '.join(f'{w}/{j}' for w, j, _, _ in jobs)}), шагов в них {seen['steps']}, "
          f"из них выносящих вердикт о продукте {seen['verdict']} (с условием {seen['guarded']}), "
          f"несущих причину своду {seen['carrier']}, без условия после подъёма {seen['noif']}, "
          f"послаблений делегирования {len(DELEGATION_EXEMPT)}; "
          f"тел шагов исполнено {proven} × 3 прогона (контроль · третья категория · находка)")
    if seen["verdict"] == 0 or seen["carrier"] == 0:
        for f in findings:
            print(f"  FATAL: {f}")
        return 2
    if findings:
        print("FAIL: «условие не создано» до вердикта НЕ доезжает:")
        for f in findings:
            print(f"  - {f}")
        return 1
    print("OK: подъём классифицирует исход у единственного владельца, шаги-вердикты при "
          "несозданном условии пропускаются, причина доносится до свода")
    return 0


if __name__ == "__main__":
    sys.exit(_self_test() if "--self-test" in sys.argv[1:] else main())
