#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Гейт: набор обязательных контекстов защиты ствола сходится с именами джоб.

ПРЕДМЕТ
-------
С 2026-08-17 (kacho#514) `main` требует 45 зелёных контекстов. Контекст — это
строка `name:` джобы ПОСЛЕ подстановки матрицы. Тем самым имя джобы стало
контрактом с защитой ветки, и у этого контракта не было ни одной проверки:

  * ПЕРЕИМЕНОВАЛ джобу — объявленный контекст перестаёт производиться, и сервер
    ждёт его вечно. Запирается КАЖДЫЙ pull request в ствол, включая тот, что
    несёт само переименование. Выход — правка защиты, то есть ручное действие
    вне дерева;
  * ЗАВЁЛ джобу — в обязательный набор она не попадает, и её краснота слияние не
    останавливает. Дыра тихая: набор выглядит полным;
  * СНЯЛ джобу вместе с предметом — запись в наборе переживает её и запирает
    ствол по той же механике, что переименование.

ПОЧЕМУ СВЕРКА ИДЁТ С ОТВЕТОМ API О ПРОГОНЕ, А НЕ С YAML
-------------------------------------------------------
Имя длиннее 100 байт GitHub ОБРЕЗАЕТ в имени проверки и дописывает многоточие.
В продукте такая джоба одна (`pg-outside-selection`), в воркспейсе две. Их имя в
YAML и имя в прогоне НЕ СОВПАДАЮТ, поэтому набор, собранный из YAML, не сошёлся
бы с производимым ни при каком прогоне — и гейт сам стал бы источником запирания.
Поэтому обе стороны берутся из API: набор — из защиты ветки, произведённое — из
проверок УСТОЯВШЕГОСЯ прогона ствола (см. read_produced).

ИСКЛЮЧЕНИЯ ВЕДУТСЯ ДВУМЯ СПОСОБАМИ, И ПЕРВЫЙ ПРЕДПОЧТИТЕЛЕН
-----------------------------------------------------------
  1. ВЫВОДИМЫЕ — имена шардов сквозного прогона собираются из
     `deploy/e2e-shards.json` по тому же шаблону, что их job. Список,
     ВЫВЕДЕННЫЙ из дерева, не может разойтись с деревом молча; рукописный —
     может, и в этом репозитории уже расходился (три копии перечня репозиториев).
  2. ОБЪЯВЛЕННЫЕ — то, что вывести неоткуда. У каждой записи причина и задача.

Оба перечня САМОИСТЕКАЮЩИЕ: исключение, которому больше нечего исключать, —
находка, а не «стало чище». Иначе запись переживает свой предмет и следующий
читатель примет её за действующее решение.

ЭТОТ ГЕЙТ — ДЕРЖАТЕЛЬ ОБЪЯВЛЕНИЯ `.github/required-contexts.txt`
----------------------------------------------------------------
Производитель версии (`scripts/release/assert-trunk-green.sh`) читает перечень
обязательных контекстов НЕ из ручки защиты, а из объявления в дереве. Причина
измерена, а не предположена: встроенному токену конвейера ручку защиты не
выдают (`Resource not accessible by integration`, HTTP 403), а запросить право
нельзя — `administration` не входит в области `permissions:` для `GITHUB_TOKEN`,
и площадка отвергает такое объявление ЦЕЛИКОМ: `HTTP 422: failed to parse
workflow: Unexpected value 'administration'` (проверено запуском 2026-09-08,
kacho#2306). То есть предпосылка выпуска не спрашивалась бы НИКОГДА.

Объявление без держателя есть то же обещание, которое корпус ловит в продукте.
Держатель — ЭТОТ гейт, и он сверяет объявление ПО ДВУМ ОСЯМ:

  * объявление ↔ ЗАЩИТА ВЕТКИ — требует прав администратора; недоступна —
    исход 2 по этой оси, НИКОГДА не «сошлось»;
  * объявление ↔ ПРОИЗВЕДЁННОЕ прогоном ствола — довольно `checks: read`, то
    есть меряется КАЖДЫМ прогоном гейта, включая штатный токен по расписанию.

Опасное направление дрейфа — защита требует контекста, которого в объявлении
нет — закрыто ВТОРОЙ осью by construction: обязательный контекст производится
джобой, а джоба, не внесённая в объявление, краснеет тихой дырой без всяких
прав администратора.

ЧАСТИЧНОЕ ИЗМЕРЕНИЕ ПЕЧАТАЕТСЯ ЧАСТИЧНЫМ
----------------------------------------
Прежняя редакция возвращала 2 и не мерила НИЧЕГО, стоило ручке защиты отказать.
Теперь недоступна ровно та ось, для которой не хватило прав; прочие меряются, и
их находки объявляются ПЕРВЫМИ. Иначе отсутствие прав маскировало бы дрейф,
который виден и без них.

ТРИ ИСХОДА, А НЕ ДВА
--------------------
  0 — сошлось, и сошлось ПО ВСЕМ осям;
  1 — расхождение (запирание, тихая дыра, истёкшее исключение либо дрейф
      объявления с защитой). Объявляется первым: находка сильнее несделанного
      измерения;
  2 — ИЗМЕРЕНИЕ НЕ СДЕЛАНО хотя бы по одной оси: ручка защиты недоступна (у
      токена нет прав), сеть не ответила, прогон ствола не найден. Это НЕ
      «находок нет»: сетевое измерение обязано быть отличимо от несделанного,
      иначе отсутствие прав читается как порядок.

Запуск:
  python3 .github/scripts/assert-required-contexts-match-jobs.py --self-test
  python3 .github/scripts/assert-required-contexts-match-jobs.py
  python3 .github/scripts/assert-required-contexts-match-jobs.py --repo OWNER/REPO
"""
from __future__ import annotations

import argparse
import io
import json
import contextlib
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

DEFAULT_REPO = "PRO-Robotech/kacho"
DEFAULT_BRANCH = "main"

# Объявление обязательных контекстов. Форма — одна строка на контекст; строка,
# начинающаяся с `#`, и строка из одних пробелов контекстом не являются.
#
# РАЗБОР ЭТОЙ ФОРМЫ СУЩЕСТВУЕТ ДВАЖДЫ — здесь и в
# `scripts/release/assert-trunk-green.sh` (там на bash, без внешних
# инструментов). Два места об одном предмете расходятся молча, поэтому форму
# пинят ОБЕ самопроверки одной и той же фикстурой: примечание, пустая строка,
# строка из пробелов, имя с внутренними пробелами и CRLF.
DECLARATION_PATH = ".github/required-contexts.txt"

# ── ОБЪЯВЛЕННЫЕ исключения ──────────────────────────────────────────────────
#
# Только то, что нельзя вывести из дерева. Каждая запись обязана нести причину и
# задачу; запись, которой нечего исключать, роняет гейт.
DECLARED_EXCLUSIONS: list[dict[str, str]] = [
    {
        "context": "образы (8 шт, один job)",
        "reason": "docker-build идёт по завершении конвейера, а не внутри него: "
                  "сделать его обязательным значило бы ждать сборку восьми образов "
                  "на каждом слиянии",
        "issue": "kacho#582",
    },
    {
        "context": "производитель версии",
        "reason": "release.yml — ручной запуск, и только он: автоматического "
                  "триггера у процесса нет ни одного. Сделать его обязательным "
                  "значило бы запереть каждый pull request контекстом, который "
                  "не производится, пока версию не выпускают. Прогон при этом "
                  "оставляет проверку на вершине ствола — оттого он и виден "
                  "среди произведённых",
        "issue": "kacho#2306",
    },
    {
        "context": "защита ствола сходится с именами джоб",
        "reason": "ЭТОТ гейт: расписание, а не гейт слияния. Сделать его "
                  "обязательным значило бы править защиту ветки ради проверки, "
                  "которая эту правку и стережёт — и запереть ствол на неделю до "
                  "первого прогона по расписанию",
        "issue": "kacho#582",
    },
]


def repo_root() -> Path:
    return Path(__file__).resolve().parents[2]


# ── ЧТЕНИЕ (вынесено, чтобы самопроверка подавала свои данные) ───────────────

class Unavailable(Exception):
    """Измерение не сделано. Отличается от «расхождений нет»."""


def _gh(args: list[str]) -> str:
    try:
        p = subprocess.run(["gh", *args], capture_output=True, text=True, timeout=90)
    except FileNotFoundError as e:
        raise Unavailable(f"нет `gh` в PATH: {e}") from e
    except subprocess.SubprocessError as e:
        raise Unavailable(f"`gh {' '.join(args[:2])}` не ответил: {e}") from e
    if p.returncode != 0:
        raise Unavailable(
            f"`gh {' '.join(args[:2])}` вышел кодом {p.returncode}: "
            f"{p.stderr.strip()[:400]}")
    return p.stdout


def read_required(repo: str, branch: str) -> list[str]:
    """Перечень обязательных контекстов ЗАЩИТЫ ветки — требует прав администратора.

    Читаются ОБЕ формы сразу: исторический `contexts` и нынешний
    `checks[].context`. Провайдер отдаёт то одну, то другую в зависимости от
    того, чем защиту заводили; прочитать одну — значит получить пустое множество
    и объявить «набор пуст» на живой защите. Соседний читатель того же предмета
    (`scripts/release/assert-trunk-green.sh` до перевода на объявление) читал обе
    — здесь было одно место об одном предмете, расходившееся с ним молча.
    """
    out = _gh(["api", f"repos/{repo}/branches/{branch}/protection",
               "-q", "[(.required_status_checks.contexts // [])[], "
                     "(.required_status_checks.checks // [])[].context] | unique | .[]"])
    return sorted({x.strip() for x in out.splitlines() if x.strip()})


def parse_declaration(body: str) -> list[str]:
    """Разбор формы объявления. Держится ТЕМИ ЖЕ фикстурами, что и разбор на bash.

    Имя берётся ДОСЛОВНО: внутренние пробелы значимы, а обрезанные площадкой
    имена оканчиваются многоточием. Обрезка краёв или разбиение по пробелу дали
    бы имена, не совпадающие ни с одним произведённым.
    """
    out: list[str] = []
    for raw in body.split("\n"):
        line = raw.rstrip("\r")
        if line.startswith("#"):
            continue
        if not line.strip():
            continue
        out.append(line)
    return sorted(set(out))


def read_declared(root: Path) -> tuple[list[str] | None, str]:
    """Объявление из ДЕРЕВА — без сети и без прав.

    `None` означает «файла нет либо он не читается». Это НАХОДКА, а не
    несделанное измерение: дерево у гейта под рукой, спросить его ничто не
    мешало, и отсутствие объявления запирает выпуск наглухо.
    """
    p = root / DECLARATION_PATH
    try:
        body = p.read_text(encoding="utf-8")
    except OSError:
        return None, DECLARATION_PATH
    return parse_declaration(body), DECLARATION_PATH


# Сколько коммитов ствола просмотреть в поисках УСТОЯВШЕГОСЯ.
SETTLED_LOOKBACK = 12


def read_produced(repo: str, branch: str) -> tuple[list[str], str]:
    """Произведённые контексты — с УСТОЯВШЕЙСЯ вершины, а не с новейшей.

    «НЕ ПРОИЗВЕДЕНО» И «НЕ ПРОИЗВОДИТСЯ» — РАЗНЫЕ ВЕЩИ, и на их смешении гейт уже
    ошибся. Свежий коммит ствола, чей прогон ещё стоит в очереди, отдаёт неполный
    набор проверок: у него нет ни шардов сквозных проб, ни их сводного вердикта.
    Судя по такому коммиту, гейт объявляет «запирание» на контексте, который
    прекрасно производится, и «исключению нечего исключать» на живых шардах —
    шесть находок, все ложные. Замер: на `b7e17f86` произведено 45 контекстов из
    51, и недостающие шесть — ровно те, чей workflow не начинался.

    Поэтому берётся первый коммит, у которого проверки github-actions есть И НИ
    ОДНА не в очереди и не в работе. Судимая вершина НАЗЫВАЕТСЯ в переписи:
    вердикт о другом коммите обязан быть отличим от вердикта о вершине.
    """
    out = _gh(["api", f"repos/{repo}/commits?sha={branch}&per_page={SETTLED_LOOKBACK}",
               "-q", ".[].sha"])
    shas = [x.strip() for x in out.splitlines() if x.strip()]
    if not shas:
        raise Unavailable("не удалось разрешить вершину ствола")

    unsettled: list[str] = []
    for sha in shas:
        raw = _gh(["api", f"repos/{repo}/commits/{sha}/check-runs?per_page=100",
                   "--paginate", "-q",
                   '.check_runs[] | select(.app.slug=="github-actions") | '
                   '"\\(.status)\\t\\(.name)"'])
        rows = [x.split("\t", 1) for x in raw.splitlines() if "\t" in x]
        if not rows:
            unsettled.append(f"{sha[:8]}: проверок нет")
            continue
        pending = [n for st, n in rows if st != "completed"]
        if pending:
            unsettled.append(f"{sha[:8]}: не завершено {len(pending)} из {len(rows)}")
            continue
        return sorted({n for _st, n in rows}), sha

    raise Unavailable(
        "среди последних " + str(len(shas)) + " коммитов ствола нет ни одного с "
        "ЗАВЕРШЁННЫМ прогоном, поэтому произведённое неизвестно: " +
        "; ".join(unsettled[:4]) + ". Это НЕ «расхождений нет»")


# ── ВЫВОДИМЫЕ исключения ────────────────────────────────────────────────────

def literal_job_names(root: Path) -> set[str]:
    """Имена джоб, записанные в workflow'ах ЛИТЕРАЛОМ (без подстановки матрицы).

    Нужны для самоистечения ОБЪЯВЛЕННЫХ исключений. Предмет такой записи — джоба
    В ДЕРЕВЕ, а не её прошлый прогон: джоба по расписанию либо только что
    заведённая ещё ничего не произвела, и требовать от неё прогона значило бы
    запретить заводить исключение раньше первого понедельника. А вот запись, под
    которой нет НИ прогона, НИ джобы, — пережила свой предмет, и это находка.
    """
    names: set[str] = set()
    wf = root / ".github" / "workflows"
    if not wf.is_dir():
        return names
    for p in sorted(wf.iterdir()):
        if p.suffix not in (".yml", ".yaml"):
            continue
        try:
            body = p.read_text(encoding="utf-8")
        except OSError:
            continue
        for raw in body.split("\n"):
            s = raw.strip()
            if not s.startswith("name:"):
                continue
            v = s[len("name:"):].strip().strip('"\'')
            # Имя с подстановкой матрицы литералом не является — его сверяют
            # выводимые исключения, а не эти.
            if v and "${{" not in v:
                names.add(v)
    return names


def derived_exclusions(root: Path) -> tuple[list[str], str]:
    """Имена шардов сквозного прогона — по тому же шаблону, что их job.

    Шаблон в `.github/workflows/e2e-newman.yml`:
        name: e2e ${{ matrix.shard.id }} (${{ matrix.shard.suites }})
    Массив подставляется через пробел — отсюда форма ниже.
    """
    p = root / "deploy" / "e2e-shards.json"
    try:
        data = json.loads(p.read_text(encoding="utf-8"))
    except (OSError, ValueError) as e:
        raise Unavailable(f"не читается {p}: {e}") from e
    shards = data.get("shards") or []
    if not shards:
        raise Unavailable(f"{p}: ноль шардов — выводить исключения не из чего")
    out = []
    for s in shards:
        sid = s.get("id")
        suites = s.get("suites") or []
        if not sid:
            raise Unavailable(f"{p}: шард без `id` — шаблон имени не построить")
        out.append(f"e2e {sid} ({' '.join(suites)})")
    return sorted(out), str(p.relative_to(root))


# ── СВЕРКА ──────────────────────────────────────────────────────────────────

def adjudicate(declared: list[str] | None, produced: list[str] | None,
               derived: list[str], derived_src: str,
               declared_exclusions: list[dict[str, str]],
               job_names: set[str] | None = None,
               judged_sha: str = "",
               protection: list[str] | None = None,
               declaration_src: str = DECLARATION_PATH) -> tuple[int, str]:
    """Сверка ОБЪЯВЛЕНИЯ с произведённым и с защитой ветки.

    `declared is None` — объявления нет в дереве (находка).
    `produced is None` / `protection is None` — ось не измерена; она НЕ молчит,
    а называется в переписи, потому что «не спрошено» не вычитается в успех.
    """
    log = io.StringIO()
    w = log.write

    decl = set(declared or [])
    prod = set(produced) if produced is not None else set()
    excl_derived = set(derived)
    excl_declared = {d["context"] for d in declared_exclusions}
    excluded = excl_derived | excl_declared

    w("===== защита ствола против имён джоб: перепись =====\n")
    if judged_sha:
        w(f"произведённое взято с УСТОЯВШЕЙСЯ вершины {judged_sha[:8]} (прогон завершён)\n")
    w(f"объявлено контекстов ({declaration_src}): "
      f"{'НЕ ПРОЧИТАНО' if declared is None else len(decl)}\n")
    w("произведено проверок github-actions: "
      f"{'НЕ ИЗМЕРЕНО' if produced is None else len(prod)}\n")
    w("обязательных в защите ветки: "
      f"{'НЕ ИЗМЕРЕНО (нет прав)' if protection is None else len(protection)}\n")
    w(f"исключений выводимых (из {derived_src}): {len(excl_derived)}\n")
    w(f"исключений объявленных: {len(excl_declared)}\n")

    findings: list[str] = []

    # (0) ОБЪЯВЛЕНИЕ ЕСТЬ И НЕПУСТО. Без него производитель версии не спросит
    #     предпосылку вовсе, то есть выпуск заперт наглухо.
    if declared is None:
        findings.append(
            f"ОБЪЯВЛЕНИЯ НЕТ: {declaration_src} не читается. Производитель версии "
            f"берёт перечень обязательных контекстов оттуда и без него выходит "
            f"кодом 3 («не спрошено») — выпуск заперт. Верни файл")
    elif not decl:
        findings.append(
            f"ОБЪЯВЛЕНИЕ ПУСТО: в {declaration_src} ноль контекстов. Условие «все "
            f"обязательные зелены» выполняется тождественно — тег не обещал бы "
            f"НИЧЕГО")

    if produced is not None:
        # (1) ЗАПИРАНИЕ: объявление требует контекст, которого никто не производит.
        for c in sorted(decl - prod):
            findings.append(
                f"ЗАПИРАНИЕ: обязательный контекст «{c}» не производится ни одной "
                f"джобой. Сервер будет ждать его вечно — запирается каждый pull "
                f"request в ствол. Либо верни джобе это имя, либо сними запись из "
                f"защиты ветки и из {declaration_src}")

        # (2) ТИХАЯ ДЫРА: джоба производится, но её краснота слияние не держит.
        for c in sorted(prod - decl - excluded):
            findings.append(
                f"ТИХАЯ ДЫРА: проверка «{c}» производится, но в обязательный набор не "
                f"входит — её краснота слияние не остановит. Либо внеси её в защиту "
                f"ветки и в {declaration_src}, либо объяви исключением с причиной")

        # (3) САМОИСТЕЧЕНИЕ: исключению больше нечего исключать.
        for c in sorted(excl_derived):
            if c not in prod:
                findings.append(
                    f"ИСКЛЮЧЕНИЮ НЕЧЕГО ИСКЛЮЧАТЬ: выводимое имя «{c}» не встречается "
                    f"среди произведённых. Либо шард переименован и шаблон имени в "
                    f"{derived_src} разошёлся с job, либо шард снят — тогда снимается "
                    f"и его строка")
        jobs = job_names or set()
        for d in declared_exclusions:
            c = d["context"]
            if c not in prod and c not in jobs:
                findings.append(
                    f"ИСКЛЮЧЕНИЮ НЕЧЕГО ИСКЛЮЧАТЬ: объявленное «{c}» ({d['issue']}) не "
                    f"производится ни одним прогоном И не объявлено ни одной джобой в "
                    f".github/workflows — запись пережила свой предмет и подлежит снятию")
            elif c in decl:
                findings.append(
                    f"ИСКЛЮЧЕНИЕ ИЗЛИШНЕ: «{c}» ({d['issue']}) стоит и в обязательном "
                    f"наборе — исключать больше нечего, запись подлежит снятию")

    # (4) ДРЕЙФ ОБЪЯВЛЕНИЯ С АВТОРИТЕТОМ. Ось требует прав администратора;
    #     недоступна — молчит НЕ как «сошлось», а как «не измерено» (см. перепись
    #     выше и вердикт вызывающего).
    if protection is not None and declared is not None:
        prot = set(protection)
        for c in sorted(prot - decl):
            findings.append(
                f"ДРЕЙФ: защита ветки требует «{c}», а {declaration_src} о нём "
                f"молчит. Производитель версии не спросит этот контекст — тег "
                f"пообещает зелень, которой никто не проверял. Внеси строку")
        for c in sorted(decl - prot):
            findings.append(
                f"ДРЕЙФ: {declaration_src} требует «{c}», а защита ветки — нет. "
                f"Выпуск отвергается по контексту, который слияние не держит. Либо "
                f"внеси его в защиту ветки, либо сними строку из объявления")

    w(f"\n===== ИТОГ: объявлено {len(decl)}; произведено "
      f"{'—' if produced is None else len(prod)}; в защите "
      f"{'—' if protection is None else len(protection)}; "
      f"исключено {len(excluded)}; находок {len(findings)} =====\n")

    if findings:
        w(f"ПРОВАЛ: {len(findings)} находк(и)\n")
        for x in findings:
            w(f"  - {x}\n")
        return 1, log.getvalue()

    w(f"PASS: объявление из {len(decl)} контекстов сходится с измеренным "
      f"({len(excluded)} исключено с причиной)\n")
    return 0, log.getvalue()


def execute(repo: str, branch: str, root: Path) -> int:
    # ЧАСТИЧНОЕ ИЗМЕРЕНИЕ ОСТАЁТСЯ ИЗМЕРЕНИЕМ. Прежняя редакция роняла всё в
    # исход 2, стоило одной ручке отказать, — и гейт не мерил НИЧЕГО там, где
    # прав не хватало только на одну ось. Оси собираются по отдельности, а
    # несделанные называются поимённо.
    unmeasured: list[str] = []

    declared, decl_src = read_declared(root)

    try:
        produced, judged_sha = read_produced(repo, branch)
    except Unavailable as e:
        produced, judged_sha = None, ""
        unmeasured.append(f"произведённое прогоном ствола: {e}")

    try:
        protection = read_required(repo, branch)
    except Unavailable as e:
        protection = None
        unmeasured.append(f"защита ветки {branch}: {e}")
    else:
        if not protection:
            protection = None
            unmeasured.append(
                f"защита ветки {branch}: обязательный набор ПУСТ. Это не "
                f"«сошлось» — это отсутствие защиты либо отказ ручки, отдавшей "
                f"пустой ответ")

    try:
        derived, src = derived_exclusions(root)
    except Unavailable as e:
        print("ИЗМЕРЕНИЕ НЕ СДЕЛАНО (это НЕ «расхождений нет»):", file=sys.stderr)
        print(f"  выводимые исключения: {e}", file=sys.stderr)
        return 2

    rc, out = adjudicate(declared, produced, derived, src, DECLARED_EXCLUSIONS,
                         literal_job_names(root), judged_sha, protection, decl_src)
    (sys.stdout if rc == 0 else sys.stderr).write(out)

    if unmeasured:
        print("\nНЕ ИЗМЕРЕНО (это НЕ «расхождений нет»):", file=sys.stderr)
        for u in unmeasured:
            print(f"  - {u}", file=sys.stderr)
        print("  Чтение защиты ветки требует прав администратора репозитория; "
              "штатному токену конвейера их не выдают, и запросить их через "
              "`permissions:` нельзя. Гони гейт токеном, у которого они есть, "
              "либо вручную — но не считай его молчание подтверждением.",
              file=sys.stderr)

    # НАХОДКА ОБЪЯВЛЯЕТСЯ ПЕРВОЙ: несделанное измерение не маскирует дрейфа,
    # который виден и без него.
    if rc == 1:
        return 1
    return 2 if unmeasured else 0


# ── ДОКАЗАТЕЛЬСТВО ИНЪЕКЦИЕЙ, В ОБЕ СТОРОНЫ ─────────────────────────────────

_SHARDS = {"shards": [{"id": "vpc", "suites": ["vpc"]},
                      {"id": "edge", "suites": ["geo", "registry"]}]}

_DERIVED = ["e2e edge (geo registry)", "e2e vpc (vpc)"]
_SRC = "deploy/e2e-shards.json"
_DECL = [{"context": "образы", "reason": "идёт после конвейера", "issue": "kacho#582"}]

# Здоровое состояние: набор == произведённое минус исключения.
_REQ_OK = ["build · vet", "golangci-lint"]
_PROD_OK = ["build · vet", "golangci-lint", "образы", *_DERIVED]


def self_test() -> int:
    failures: list[str] = []

    def check(label: str, cond: bool, detail: str = "") -> None:
        if cond:
            print(f"  ок     {label}")
        else:
            print(f"  ПРОВАЛ {label}  {detail}")
            failures.append(label)

    def run(req, prod, derived=_DERIVED, decl=_DECL, jobs=None):
        return adjudicate(sorted(req), sorted(prod), sorted(derived), _SRC, decl,
                          jobs if jobs is not None else set())

    print("(a) джоба переименована — гейт краснеет и называет координату")
    # `golangci-lint` переименован в `lint`: набор ждёт старое имя, никто его не даёт.
    rc, out = run(_REQ_OK, ["build · vet", "lint", "образы", *_DERIVED])
    check("краснеет на запирании", rc == 1, out)
    check("называет запертый контекст", "«golangci-lint»" in out, out)
    check("объясняет следствие", "запирается каждый pull" in out, out)
    check("видит и вторую сторону — новое имя вне набора", "«lint»" in out, out)

    print("(b) законная правка той же внешности — гейт молчит")
    # Джоба переименована И набор обновлён вместе с ней: расхождения нет.
    rc, out = run(["build · vet", "lint"],
                  ["build · vet", "lint", "образы", *_DERIVED])
    check("молчит на согласованном переименовании", rc == 0, out)
    check("перепись печатает объём", "объявлено контекстов" in out and
          "): 2\n" in out, out)

    print("(c) здоровое дерево — молчит")
    rc, out = run(_REQ_OK, _PROD_OK)
    check("молчит на сошедшемся наборе", rc == 0, out)
    check("считает исключения", "исключено 3" in out, out)

    print("(d) заведена джоба, не попавшая в набор — тихая дыра")
    rc, out = run(_REQ_OK, [*_PROD_OK, "новая проверка"])
    check("краснеет на непокрытой джобе", rc == 1, out)
    check("называет её", "«новая проверка»" in out, out)
    check("объясняет, что краснота не держит", "не остановит" in out, out)

    print("(e) шард переименован — ВЫВОДИМОЕ исключение истекает само")
    rc, out = run(_REQ_OK, ["build · vet", "golangci-lint", "образы",
                            "e2e edge (geo registry)", "e2e vpc-new (vpc)"])
    check("краснеет на истёкшем выводимом исключении", rc == 1, out)
    check("называет истёкшую запись", "«e2e vpc (vpc)»" in out, out)
    check("говорит о самоистечении", "НЕЧЕГО ИСКЛЮЧАТЬ" in out, out)

    print("(f) объявленное исключение пережило предмет — тоже находка")
    rc, out = run(_REQ_OK, ["build · vet", "golangci-lint", *_DERIVED])
    check("краснеет на объявленном без предмета", rc == 1, out)
    check("называет задачу записи", "kacho#582" in out, out)

    print("(f2) джоба по расписанию ещё не производила прогона — НЕ находка")
    # Предмет объявленного исключения — джоба В ДЕРЕВЕ. Требовать от неё прогона
    # значило бы запретить заводить исключение раньше первого понедельника.
    rc, out = run(_REQ_OK, ["build · vet", "golangci-lint", *_DERIVED],
                  jobs={"образы"})
    check("молчит, когда джоба объявлена, но ещё не прогонялась", rc == 0, out)
    # И зеркало: снятая джоба (нет ни прогона, ни объявления) — по-прежнему находка.
    rc, out = run(_REQ_OK, ["build · vet", "golangci-lint", *_DERIVED], jobs=set())
    check("краснеет, когда нет НИ прогона, НИ джобы", rc == 1, out)
    check("называет обе стороны предиката",
          "не объявлено ни одной джобой" in out, out)

    print("(g) исключение стало обязательным — запись излишня")
    rc, out = run([*_REQ_OK, "образы"], _PROD_OK)
    check("краснеет на излишнем исключении", rc == 1, out)
    check("говорит, что исключать больше нечего", "ИЗЛИШНЕ" in out, out)

    print("(g2) провенанс: судимая вершина названа в переписи")
    # Вердикт о другом коммите обязан быть отличим от вердикта о вершине.
    rc, out = adjudicate(sorted(_REQ_OK), sorted(_PROD_OK), sorted(_DERIVED), _SRC,
                         _DECL, set(), "b7e17f86ffff")
    check("называет коммит, по которому судил", "b7e17f86" in out, out)
    check("говорит, что вершина устоявшаяся", "УСТОЯВШЕЙСЯ" in out, out)

    print("(g3) неполный прогон НЕ выдаётся за отсутствие производителя")
    # Тот самый ложный вердикт: на коммите с прогоном в очереди недостают ровно
    # шардовые контексты и их сводный вердикт. Судить по такому нельзя —
    # read_produced обязан пройти мимо него и взять устоявшийся ниже.
    calls = {"n": 0}
    def fake_gh(args):
        q = " ".join(args)
        if "/commits?sha=" in q:
            return "aaaaaaaa\nbbbbbbbb\n"
        if "/commits/aaaaaaaa/check-runs" in q:
            calls["n"] += 1
            return "queued\tсводный вердикт (все шарды)\ncompleted\tbuild · vet\n"
        if "/commits/bbbbbbbb/check-runs" in q:
            return ("completed\tbuild · vet\ncompleted\tсводный вердикт (все шарды)\n"
                    "completed\te2e vpc (vpc)\n")
        raise AssertionError(q)
    mod = sys.modules[__name__]
    orig = mod._gh
    try:
        mod._gh = fake_gh
        names, sha = read_produced("o/r", "main")
    finally:
        mod._gh = orig
    check("пропускает коммит с незавершённым прогоном", sha == "bbbbbbbb", sha)
    check("берёт полный набор с устоявшегося",
          "сводный вердикт (все шарды)" in names and "e2e vpc (vpc)" in names, names)
    check("незавершённый коммит всё же был осмотрен", calls["n"] == 1, calls)

    print("(g4) ни одного устоявшегося коммита — ИЗМЕРЕНИЕ НЕ СДЕЛАНО")
    def all_pending(args):
        q = " ".join(args)
        if "/commits?sha=" in q:
            return "aaaaaaaa\n"
        return "queued\tbuild · vet\n"
    try:
        mod._gh = all_pending
        try:
            read_produced("o/r", "main")
            check("отвергает дерево без устоявшегося прогона", False, "не подняло")
        except Unavailable as e:
            check("отвергает дерево без устоявшегося прогона", True)
            check("говорит, что это не «расхождений нет»",
                  "НЕ «расхождений нет»" in str(e), str(e))
    finally:
        mod._gh = orig

    print("(h) пустой набор — ИЗМЕРЕНИЕ НЕ СДЕЛАНО, а не «сошлось»")
    buf = io.StringIO()
    with contextlib.redirect_stdout(buf), contextlib.redirect_stderr(buf):
        rc = execute("owner/repo", "main", Path(tempfile.gettempdir()))
    check("недоступная ручка даёт третий исход", rc == 2, buf.getvalue())
    check("говорит, что это не «находок нет»",
          "НЕ «расхождений нет»" in buf.getvalue(), buf.getvalue())

    print("(i) выводимые исключения строятся из дерева, а не выписаны")
    root = Path(tempfile.mkdtemp(prefix="ctx-selftest-"))
    try:
        (root / "deploy").mkdir(parents=True)
        (root / "deploy" / "e2e-shards.json").write_text(json.dumps(_SHARDS))
        got, src = derived_exclusions(root)
        check("имя шарда собрано по шаблону job", got == sorted(_DERIVED), got)
        check("источник назван", src.endswith("e2e-shards.json"), src)
        (root / "deploy" / "e2e-shards.json").write_text(json.dumps({"shards": []}))
        try:
            derived_exclusions(root)
            check("ноль шардов отвергнут", False, "исключения не подняты")
        except Unavailable:
            check("ноль шардов отвергнут", True)
    finally:
        shutil.rmtree(root, ignore_errors=True)

    print("(j) объявление разошлось с ЗАЩИТОЙ ветки — дрейф в обе стороны")
    # Опасное направление: защита требует контекст, о котором объявление молчит.
    # Производитель версии его не спросит — тег пообещает непроверенную зелень.
    rc, out = adjudicate(sorted(_REQ_OK), sorted(_PROD_OK), sorted(_DERIVED), _SRC,
                         _DECL, set(), "", [*_REQ_OK, "новый обязательный"])
    check("краснеет, когда защита требует незаявленного", rc == 1, out)
    check("называет незаявленный контекст", "«новый обязательный»" in out, out)
    check("объясняет цену", "непроверенн" in out or "не спросит" in out, out)

    # Обратное направление: объявление шире защиты — выпуск отвергается по
    # контексту, который слияние не держит. Тоже находка, но дешевле.
    rc, out = adjudicate(sorted([*_REQ_OK, "лишний"]), sorted([*_PROD_OK, "лишний"]),
                         sorted(_DERIVED), _SRC, _DECL, set(), "", sorted(_REQ_OK))
    check("краснеет, когда объявление шире защиты", rc == 1, out)
    check("называет лишний контекст", "«лишний»" in out, out)

    print("(j2) законный близнец: объявление == защита — молчит")
    rc, out = adjudicate(sorted(_REQ_OK), sorted(_PROD_OK), sorted(_DERIVED), _SRC,
                         _DECL, set(), "", sorted(_REQ_OK))
    check("молчит на сошедшемся объявлении", rc == 0, out)
    check("перепись называет объём защиты", "в защите ветки: 2" in out, out)

    print("(j3) защита НЕ измерена — ось молчит, но перепись говорит об этом")
    rc, out = adjudicate(sorted(_REQ_OK), sorted(_PROD_OK), sorted(_DERIVED), _SRC,
                         _DECL, set(), "", None)
    check("не выдаёт неизмеренную ось за сошедшуюся", "НЕ ИЗМЕРЕНО" in out, out)
    check("прочие оси при этом измерены", rc == 0, out)

    print("(k) объявления нет в дереве — НАХОДКА, а не «не измерено»")
    # Дерево у гейта под рукой: спросить его ничто не мешало, а без объявления
    # производитель версии выходит кодом 3 и выпуск заперт наглухо.
    rc, out = adjudicate(None, sorted(_PROD_OK), sorted(_DERIVED), _SRC, _DECL,
                         set(), "", sorted(_REQ_OK))
    check("краснеет на отсутствующем объявлении", rc == 1, out)
    check("называет файл", DECLARATION_PATH in out, out)
    check("говорит, чем это грозит выпуску", "заперт" in out, out)

    print("(k2) объявление есть, контекстов ноль — тоже находка")
    rc, out = adjudicate([], sorted(_PROD_OK), sorted(_DERIVED), _SRC, _DECL,
                         set(), "", [])
    check("краснеет на пустом объявлении", rc == 1, out)
    check("говорит про тождественно-истинный вердикт", "тождественно" in out, out)

    print("(l) разбор формы объявления — те же фикстуры, что у bash-читателя")
    body = ("# примечание в первой позиции\n"
            "\n"
            "alpha\n"
            "   \n"
            "build · vet · gofmt · test -race\r\n"
            "# ещё примечание\n"
            "Postgres-пробы (пропуск =...\n")
    got = parse_declaration(body)
    check("примечания и пустые строки не контексты", len(got) == 3, got)
    check("имя с внутренними пробелами читается дословно",
          "build · vet · gofmt · test -race" in got, got)
    check("возврат каретки снят", not any("\r" in x for x in got), got)
    check("обрезанное площадкой имя сохраняет многоточие",
          any(x.endswith("=...") for x in got), got)

    print("(l2) частичное измерение: находка объявляется ПЕРВОЙ, а не тонет в исходе 2")
    root = Path(tempfile.mkdtemp(prefix="ctx-partial-"))
    try:
        (root / "deploy").mkdir(parents=True)
        (root / "deploy" / "e2e-shards.json").write_text(json.dumps(_SHARDS))
        (root / ".github").mkdir(parents=True)
        decl_file = root / DECLARATION_PATH
        decl_file.write_text("# объявление\nbuild · vet\ngolangci-lint\n",
                             encoding="utf-8")

        def gh_no_protection(args):
            q = " ".join(args)
            if "/protection" in q:
                raise Unavailable("gh: Resource not accessible by integration (HTTP 403)")
            if "/commits?sha=" in q:
                return "cccccccc\n"
            if "/commits/cccccccc/check-runs" in q:
                # Мир фикстуры ЗДОРОВ по всем осям, кроме измеряемой: у обоих
                # ОБЪЯВЛЕННЫХ исключений есть предмет, у выводимых тоже. Иначе
                # ось мерила бы чужие находки, а не своё различение исходов.
                rows = ["completed\tbuild · vet", "completed\tgolangci-lint",
                        *[f"completed\t{d['context']}" for d in DECLARED_EXCLUSIONS],
                        *[f"completed\t{n}" for n in _DERIVED]]
                return "\n".join(rows) + "\n"
            raise AssertionError(q)

        mod2 = sys.modules[__name__]
        keep = mod2._gh
        try:
            mod2._gh = gh_no_protection
            buf = io.StringIO()
            with contextlib.redirect_stdout(buf), contextlib.redirect_stderr(buf):
                rc = execute("owner/repo", "main", root)
            check("без прав на защиту прочие оси всё же измерены",
                  "объявлено "
                  "контекстов" in buf.getvalue(),
                  buf.getvalue())
            check("частичное измерение даёт исход 2, а не 0", rc == 2, buf.getvalue())
            check("неизмеренная ось названа поимённо",
                  "HTTP 403" in buf.getvalue(), buf.getvalue())

            # Тот же мир, но с ТИХОЙ ДЫРОЙ: находка обязана перебить исход 2.
            decl_file.write_text("# объявление\nbuild · vet\n", encoding="utf-8")
            buf = io.StringIO()
            with contextlib.redirect_stdout(buf), contextlib.redirect_stderr(buf):
                rc = execute("owner/repo", "main", root)
            check("находка перебивает несделанное измерение", rc == 1, buf.getvalue())
            check("названа именно тихая дыра",
                  "ТИХАЯ ДЫРА" in buf.getvalue(),
                  buf.getvalue())
        finally:
            mod2._gh = keep
    finally:
        shutil.rmtree(root, ignore_errors=True)

    print()
    if failures:
        print(f"САМОПРОВЕРКА ПРОВАЛЕНА: {len(failures)} — {', '.join(failures)}",
              file=sys.stderr)
        return 1
    print("ДОКАЗАНО: гейт краснеет на переименовании, на непокрытой джобе, на "
          "истёкшем исключении (обоих видов) и на дрейфе объявления с защитой "
          "(в обе стороны), молчит на согласованной правке, и отличает "
          "несделанное измерение от сошедшегося набора — в том числе когда "
          "измерена только часть осей.")
    return 0


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--repo", default=DEFAULT_REPO)
    ap.add_argument("--branch", default=DEFAULT_BRANCH)
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
