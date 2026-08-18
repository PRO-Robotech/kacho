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

ТРИ ИСХОДА, А НЕ ДВА
--------------------
  0 — сошлось;
  1 — расхождение (запирание либо тихая дыра);
  2 — ИЗМЕРЕНИЕ НЕ СДЕЛАНО: ручка защиты недоступна (у токена нет прав на
      чтение защиты ветки), сеть не ответила, прогон ствола не найден. Это НЕ
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
    out = _gh(["api", f"repos/{repo}/branches/{branch}/protection",
               "-q", ".required_status_checks.contexts[]"])
    return sorted({x.strip() for x in out.splitlines() if x.strip()})


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

def adjudicate(required: list[str], produced: list[str],
               derived: list[str], derived_src: str,
               declared: list[dict[str, str]],
               job_names: set[str] | None = None,
               judged_sha: str = "") -> tuple[int, str]:
    log = io.StringIO()
    w = log.write

    req, prod = set(required), set(produced)
    excl_derived, excl_declared = set(derived), {d["context"] for d in declared}
    excluded = excl_derived | excl_declared

    w("===== защита ствола против имён джоб: перепись =====\n")
    if judged_sha:
        w(f"произведённое взято с УСТОЯВШЕЙСЯ вершины {judged_sha[:8]} (прогон завершён)\n")
    w(f"контекстов в обязательном наборе: {len(req)}\n")
    w(f"произведено проверок github-actions: {len(prod)}\n")
    w(f"исключений выводимых (из {derived_src}): {len(excl_derived)}\n")
    w(f"исключений объявленных: {len(excl_declared)}\n")

    findings: list[str] = []

    # (1) ЗАПИРАНИЕ: набор требует контекст, которого никто не производит.
    locking = sorted(req - prod)
    for c in locking:
        findings.append(
            f"ЗАПИРАНИЕ: обязательный контекст «{c}» не производится ни одной "
            f"джобой. Сервер будет ждать его вечно — запирается каждый pull "
            f"request в ствол. Либо верни джобе это имя, либо сними запись из "
            f"защиты ветки")

    # (2) ТИХАЯ ДЫРА: джоба производится, но её краснота слияние не держит.
    silent = sorted(prod - req - excluded)
    for c in silent:
        findings.append(
            f"ТИХАЯ ДЫРА: проверка «{c}» производится, но в обязательный набор не "
            f"входит — её краснота слияние не остановит. Либо внеси её в защиту "
            f"ветки, либо объяви исключением с причиной")

    # (3) САМОИСТЕЧЕНИЕ: исключению больше нечего исключать.
    for c in sorted(excl_derived):
        if c not in prod:
            findings.append(
                f"ИСКЛЮЧЕНИЮ НЕЧЕГО ИСКЛЮЧАТЬ: выводимое имя «{c}» не встречается "
                f"среди произведённых. Либо шард переименован и шаблон имени в "
                f"{derived_src} разошёлся с job, либо шард снят — тогда снимается "
                f"и его строка")
    jobs = job_names or set()
    for d in declared:
        c = d["context"]
        if c not in prod and c not in jobs:
            findings.append(
                f"ИСКЛЮЧЕНИЮ НЕЧЕГО ИСКЛЮЧАТЬ: объявленное «{c}» ({d['issue']}) не "
                f"производится ни одним прогоном И не объявлено ни одной джобой в "
                f".github/workflows — запись пережила свой предмет и подлежит снятию")
        elif c in req:
            findings.append(
                f"ИСКЛЮЧЕНИЕ ИЗЛИШНЕ: «{c}» ({d['issue']}) стоит и в обязательном "
                f"наборе — исключать больше нечего, запись подлежит снятию")

    w(f"\n===== ИТОГ: набор {len(req)}; произведено {len(prod)}; "
      f"исключено {len(excluded)}; находок {len(findings)} =====\n")

    if findings:
        w(f"ПРОВАЛ: {len(findings)} находк(и)\n")
        for x in findings:
            w(f"  - {x}\n")
        return 1, log.getvalue()

    w(f"PASS: набор из {len(req)} контекстов сходится с произведёнными "
      f"({len(excluded)} исключено с причиной)\n")
    return 0, log.getvalue()


def execute(repo: str, branch: str, root: Path) -> int:
    try:
        required = read_required(repo, branch)
        produced, judged_sha = read_produced(repo, branch)
        derived, src = derived_exclusions(root)
    except Unavailable as e:
        # ТРЕТИЙ ИСХОД. Не зелёный и не красный: измерение не сделано.
        print("ИЗМЕРЕНИЕ НЕ СДЕЛАНО (это НЕ «расхождений нет»):", file=sys.stderr)
        print(f"  {e}", file=sys.stderr)
        print("  Чтение защиты ветки требует прав администратора репозитория; "
              "штатному токену конвейера их не выдают. Гони гейт токеном, у "
              "которого они есть, либо вручную — но не считай его молчание "
              "подтверждением.", file=sys.stderr)
        return 2

    if not required:
        print("ИЗМЕРЕНИЕ НЕ СДЕЛАНО: обязательный набор ПУСТ. Это не «сошлось» — "
              "это отсутствие защиты либо отказ ручки, отдавшей пустой ответ.",
              file=sys.stderr)
        return 2

    rc, out = adjudicate(required, produced, derived, src, DECLARED_EXCLUSIONS,
                         literal_job_names(root), judged_sha)
    (sys.stdout if rc == 0 else sys.stderr).write(out)
    return rc


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
    check("перепись печатает объём", "контекстов в обязательном наборе: 2" in out, out)

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

    print()
    if failures:
        print(f"САМОПРОВЕРКА ПРОВАЛЕНА: {len(failures)} — {', '.join(failures)}",
              file=sys.stderr)
        return 1
    print("ДОКАЗАНО: гейт краснеет на переименовании, на непокрытой джобе и на "
          "истёкшем исключении (обоих видов), молчит на согласованной правке, "
          "и отличает несделанное измерение от сошедшегося набора.")
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
