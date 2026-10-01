#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Гейт: процесс по `workflow_run` публикует только посаженное (kacho#2942).

ПРЕДМЕТ
-------
Процесс, поднятый событием `workflow_run`, исполняется в контексте базового
репозитория — с его секретами — независимо от того, каким событием поднят
исходный прогон. Фильтр `workflow_run.branches` сверяет одно имя ветки и не
отвечает ни на вопрос о событии, ни на вопрос о происхождении. Поэтому право на
публикацию судится отдельно, и судится оно ИСХОДОМ, а не текстом: гейт берёт
РАЗОБРАННЫЙ процесс из дерева, подаёт ему синтетические события и исполняет то,
что исполнила бы площадка, — условие задания, шаг решения, условия шагов.

ЕДИНИЦА СЧЁТА — задание процесса с триггером `workflow_run`, в шагах которого
есть обращение к `secrets.*` (разобранный YAML: значения ключей шага, не
комментарии). Перечень процессов выводится из `git ls-files .github/workflows`.

ПУБЛИКАЦИЯ В СЦЕНАРИИ = задание идёт (его `if:` истинно) И исполнение доходит до
шага, читающего секреты. Сцены двух знаков, и обе обязательны:

  * ЧУЖИЕ (публикации нет): исходный прогон поднят не `push` (запрос, запрос с
    правами базы, ручной запуск), голова не из этого репозитория, ветка вне
    правила — на этой редакции правило равно фильтру `workflow_run.branches`:
    ствол `main` и ничего больше;
  * ЗАКОННЫЕ (публикация есть): `push` этого репозитория в `main`; а
    также собственный `push` процесса в ствол. Без них гейт молчал бы на
    процессе, который не публикует НИЧЕГО, — запрет без положительного контроля.

СЛОЁВ ТРИ, И КАЖДЫЙ СУДИТСЯ ПОРОЗНЬ на чужих сценах, иначе один слой прятал бы
снятие другого:
  1. условие задания ложно (кроме сцен «ветка вне правила»: имя с образцом
     выражением не сопоставить, это работа слоя 2);
  2. шаг решения (`id: publish_right`) при обойдённом условии задания отказывает
     либо отвечает «нет» — и шаг с секретами не исполняется;
  3. шаг с секретами охраняет своё условие: при ответе решения «нет» БЕЗ отказа
     он не исполняется.

ЯЗЫК УСЛОВИЙ. Вычисляется подмножество выражений площадки: `|| && ! == !=`,
скобки, строки, `true/false/null`, числа, контексты `github`, `steps`, `env` и
функции состояния `success() failure() always() cancelled()`. Сравнение строк —
без учёта регистра, разнотипное — через число, как у площадки. Всё прочее —
ОТКАЗ «не измерено» (код 2), а не догадка: непонятое условие не судится ни в
какую сторону. Вывод шага, который не исполнялся, — null; вывод исполненного шага,
которого гейт не исполняет, — «любой» (худший случай: равен всему).

ГРАНИЦА, названная честно. Гейт не судит фильтр `workflow_run.branches` и не
исполняет шаги, кроме шага решения: он отвечает, ДОХОДИТ ли исполнение до
секретов, а не что с ними делается дальше.

Коды: 0 — находок ноль и перепись названа; 1 — находки; 2 — не измерено либо
судить нечего (процессов `workflow_run` с секретами ноль — гейт снимается вместе
с предметом).

Запуск:
  python3 .github/scripts/assert-workflow-run-publish-guard.py --self-test
  python3 .github/scripts/assert-workflow-run-publish-guard.py
"""
from __future__ import annotations

import os
import re
import subprocess
import sys
import tempfile
from dataclasses import dataclass, field
from pathlib import Path

try:
    import yaml
except ImportError:  # pragma: no cover - в конвейере yaml ставится шагом
    print("ОТКАЗ: нет модуля yaml — судить не о чем", file=sys.stderr)
    sys.exit(2)

DECISION_ID = "publish_right"
THIS_REPO = "PRO-Robotech/kacho"
FOREIGN_REPO = "someone-else/kacho"


class Unmeasured(Exception):
    """Условие или процесс, о котором гейт не умеет судить."""


class _Any:
    """Вывод исполненного шага, которого гейт не исполняет: худший случай."""

    def __repr__(self) -> str:  # pragma: no cover
        return "<любой>"


ANY = _Any()

# ───────────────────────── язык условий ─────────────────────────

_TOKEN = re.compile(
    r"\s*(?:(?P<op>\|\||&&|==|!=|!|\(|\)|,)|(?P<str>'(?:[^']|'')*')"
    r"|(?P<num>-?\d+(?:\.\d+)?)|(?P<id>[A-Za-z_][A-Za-z0-9_\-]*(?:\.[A-Za-z_*][A-Za-z0-9_\-]*)*))"
)


def _tokens(expr: str) -> list[tuple[str, str]]:
    out, pos = [], 0
    expr = expr.strip()
    while pos < len(expr):
        m = _TOKEN.match(expr, pos)
        if not m or m.end() == pos:
            if expr[pos:].strip() == "":
                break
            raise Unmeasured(f"не разобрано выражение у позиции {pos}: {expr[pos:pos+30]!r}")
        pos = m.end()
        kind = m.lastgroup
        out.append((kind, m.group(kind)))
    return out


def _num(v):
    if v is None:
        return 0.0
    if isinstance(v, bool):
        return 1.0 if v else 0.0
    if isinstance(v, (int, float)):
        return float(v)
    if isinstance(v, str):
        try:
            return float(v.strip()) if v.strip() else 0.0
        except ValueError:
            return float("nan")
    return float("nan")


def _eq(a, b) -> bool:
    if a is ANY or b is ANY:
        return True
    if isinstance(a, str) and isinstance(b, str):
        return a.casefold() == b.casefold()
    if type(a) is type(b):
        return a == b
    x, y = _num(a), _num(b)
    return x == y  # NaN не равно ничему


def truthy(v) -> bool:
    if v is ANY:
        return True
    if v is None or v is False:
        return False
    if isinstance(v, (int, float)) and not isinstance(v, bool):
        return v != 0
    if isinstance(v, str):
        return v != ""
    return True


@dataclass
class Ctx:
    github: dict
    steps: dict = field(default_factory=dict)
    env: dict = field(default_factory=dict)
    failed: bool = False


STATUS_FUNCS = {"success", "failure", "always", "cancelled"}


class _Parser:
    def __init__(self, toks, ctx: Ctx):
        self.t, self.i, self.ctx = toks, 0, ctx

    def peek(self):
        return self.t[self.i] if self.i < len(self.t) else (None, None)

    def take(self, val=None):
        tok = self.peek()
        if val is not None and tok[1] != val:
            raise Unmeasured(f"ожидалось {val!r}, встречено {tok[1]!r}")
        self.i += 1
        return tok

    def parse(self):
        v = self.or_()
        if self.i != len(self.t):
            raise Unmeasured(f"хвост выражения не разобран: {self.t[self.i:]}")
        return v

    def or_(self):
        v = self.and_()
        while self.peek()[1] == "||":
            self.take()
            r = self.and_()
            v = v if truthy(v) else r
        return v

    def and_(self):
        v = self.not_()
        while self.peek()[1] == "&&":
            self.take()
            r = self.not_()
            v = r if truthy(v) else v
        return v

    def not_(self):
        if self.peek()[1] == "!":
            self.take()
            return not truthy(self.not_())
        return self.cmp()

    def cmp(self):
        v = self.prim()
        op = self.peek()[1]
        if op in ("==", "!="):
            self.take()
            r = self.prim()
            eq = _eq(v, r)
            return eq if op == "==" else (True if (v is ANY or r is ANY) else not eq)
        return v

    def prim(self):
        kind, val = self.take()
        if val == "(":
            v = self.or_()
            self.take(")")
            return v
        if kind == "str":
            return val[1:-1].replace("''", "'")
        if kind == "num":
            return float(val)
        if kind == "id":
            if val in ("true", "false"):
                return val == "true"
            if val == "null":
                return None
            if self.peek()[1] == "(":
                self.take("(")
                self.take(")")
                if val not in STATUS_FUNCS:
                    raise Unmeasured(f"функция {val}() не входит в вычисляемое подмножество")
                f = self.ctx.failed
                return {"success": not f, "failure": f, "always": True, "cancelled": False}[val]
            return self.resolve(val)
        raise Unmeasured(f"неожиданный элемент {val!r}")

    def resolve(self, path: str):
        parts = path.split(".")
        root = parts[0]
        if root == "github":
            cur = self.ctx.github
        elif root == "steps":
            cur = self.ctx.steps
        elif root == "env":
            cur = self.ctx.env
        else:
            raise Unmeasured(f"контекст {root} не входит в вычисляемое подмножество ({path})")
        for p in parts[1:]:
            if cur is ANY:
                return ANY
            if not isinstance(cur, dict):
                return None
            cur = cur.get(p)
        return cur


def _strip_braces(expr: str) -> str:
    e = expr.strip()
    m = re.fullmatch(r"\$\{\{(.*)\}\}", e, re.S)
    return m.group(1) if m else e


def evaluate(expr, ctx: Ctx):
    if isinstance(expr, bool):
        return expr
    return _Parser(_tokens(_strip_braces(str(expr))), ctx).parse()


def uses_status_func(expr) -> bool:
    return bool(re.search(r"\b(success|failure|always|cancelled)\s*\(", str(expr)))


def interpolate(text: str, ctx: Ctx) -> str:
    def sub(m):
        v = evaluate(m.group(1), ctx)
        if v is ANY:
            raise Unmeasured(f"подстановка худшего случая в текст шага: {m.group(0)}")
        if v is None:
            return ""
        if isinstance(v, bool):
            return "true" if v else "false"
        return str(v)

    return re.sub(r"\$\{\{(.*?)\}\}", sub, text, flags=re.S)


# ───────────────────────── процесс ─────────────────────────


def _triggers(doc: dict) -> dict:
    on = doc.get("on", doc.get(True))
    if isinstance(on, str):
        return {on: None}
    if isinstance(on, list):
        return {k: None for k in on}
    return on or {}


_SECRETS = re.compile(r"\$\{\{[^}]*\bsecrets\.", re.S)


def _refs_secrets(node) -> bool:
    if isinstance(node, str):
        return bool(_SECRETS.search(node))
    if isinstance(node, dict):
        return any(_refs_secrets(v) for k, v in node.items() if k != "name")
    if isinstance(node, list):
        return any(_refs_secrets(v) for v in node)
    return False


@dataclass
class Scene:
    name: str
    github: dict
    lawful: bool
    # Правило имени ветки выражением условия не записать (сопоставления с образцом
    # в языке условий нет), поэтому сцену «ветка вне правила» слой 1 не судит —
    # только слои 2 и 3.
    branch_only: bool = False


def _wr(event, branch, repo, conclusion="success"):
    return {
        "event_name": "workflow_run",
        "repository": THIS_REPO,
        "ref": "refs/heads/main",
        "ref_name": "main",
        "event": {"workflow_run": {
            "event": event, "head_branch": branch, "head_sha": "0" * 40,
            "conclusion": conclusion, "head_repository": {"full_name": repo},
        }},
    }


SCENES = [
    Scene("workflow_run ← pull_request, голова чужого репозитория, ветка main",
          _wr("pull_request", "main", FOREIGN_REPO), False),
    Scene("workflow_run ← pull_request, голова этого репозитория, ветка main",
          _wr("pull_request", "main", THIS_REPO), False),
    Scene("workflow_run ← pull_request, ветка-номер с сутью 2942-x",
          _wr("pull_request", "2942-x", THIS_REPO), False),
    Scene("workflow_run ← pull_request, красный исходный прогон",
          _wr("pull_request", "main", FOREIGN_REPO, "failure"), False),
    Scene("workflow_run ← pull_request_target",
          _wr("pull_request_target", "main", THIS_REPO), False),
    Scene("workflow_run ← workflow_dispatch",
          _wr("workflow_dispatch", "main", THIS_REPO), False),
    Scene("workflow_run ← push, голова чужого репозитория",
          _wr("push", "main", FOREIGN_REPO), False),
    Scene("workflow_run ← push, ветка вне правила feature/x",
          _wr("push", "feature/x", THIS_REPO), False, True),
    Scene("workflow_run ← push, ветка вне правила 2942/x",
          _wr("push", "2942/x", THIS_REPO), False, True),
    Scene("workflow_run ← push, ветка вне правила mainx",
          _wr("push", "mainx", THIS_REPO), False, True),
    Scene("workflow_run ← push в main этого репозитория",
          _wr("push", "main", THIS_REPO), True),
    Scene("workflow_run ← push в main, красный исходный прогон",
          _wr("push", "main", THIS_REPO, "failure"), True),
    # Ветки-номера здесь ЧУЖИЕ: правило этой редакции — фильтр
    # `workflow_run.branches: [main]`. Расширение правила — отдельный предмет со
    # своим изменением, и оно обязано перевести эти сцены в законные явно.
    Scene("workflow_run ← push, ветка-номер вне правила 2942",
          _wr("push", "2942", THIS_REPO), False, True),
    Scene("workflow_run ← push, ветка-номер с сутью вне правила 2942-x",
          _wr("push", "2942-x", THIS_REPO), False, True),
    Scene("собственный push в main", {
        "event_name": "push", "repository": THIS_REPO, "ref": "refs/heads/main",
        "ref_name": "main", "event": {}}, True),
]


@dataclass
class Run:
    job_if: bool
    decision_rc: int | None = None
    decision_publish: str | None = None
    secret_step: str | None = None  # имя первого исполненного шага с секретами


def _run_decision(step: dict, ctx: Ctx) -> tuple[int, dict]:
    env = {"PATH": os.environ.get("PATH", "/usr/bin:/bin")}
    for k, v in (step.get("env") or {}).items():
        env[str(k)] = interpolate(str(v), ctx)
    script = interpolate(str(step.get("run", "")), ctx)
    with tempfile.TemporaryDirectory() as d:
        out = Path(d) / "out"
        out.write_text("")
        env["GITHUB_OUTPUT"] = str(out)
        p = subprocess.run(["bash", "--noprofile", "--norc", "-eo", "pipefail", "-c", script],
                           env=env, capture_output=True, text=True)
        outputs = {}
        for line in out.read_text().splitlines():
            if "=" in line:
                k, v = line.split("=", 1)
                outputs[k] = v
    return p.returncode, outputs


def simulate(job: dict, wf_env: dict, github: dict, *, bypass_job_if=False,
             forced_decision: tuple[int, dict] | None = None) -> Run:
    ctx = Ctx(github=github, env={**(wf_env or {}), **(job.get("env") or {})})
    cond = job.get("if")
    job_if = True if cond is None else truthy(evaluate(cond, ctx))
    run = Run(job_if=job_if)
    if not job_if and not bypass_job_if:
        return run
    for step in job.get("steps") or []:
        sc = step.get("if")
        if sc is None:
            runs = not ctx.failed
        else:
            v = truthy(evaluate(sc, ctx))
            runs = v if uses_status_func(sc) else (v and not ctx.failed)
        sid = step.get("id")
        if not runs:
            if sid:
                ctx.steps[sid] = {"outputs": {}, "outcome": "skipped"}
            continue
        if sid == DECISION_ID:
            rc, outs = forced_decision if forced_decision else _run_decision(step, ctx)
            run.decision_rc, run.decision_publish = rc, outs.get("publish")
            ctx.steps[sid] = {"outputs": outs, "outcome": "success" if rc == 0 else "failure"}
            if rc != 0:
                ctx.failed = True
            continue
        if _refs_secrets(step):
            run.secret_step = run.secret_step or step.get("name") or sid or "<шаг без имени>"
            continue
        if sid:
            ctx.steps[sid] = ANY
    return run


@dataclass
class Census:
    workflows: int = 0
    consumers: int = 0
    jobs: int = 0
    jobs_with_secrets: int = 0
    scenes: int = 0


def judge(workflows: dict[str, str]) -> tuple[list[str], Census]:
    """workflows: путь → текст. Возвращает находки и перепись."""
    findings: list[str] = []
    c = Census(workflows=len(workflows))
    for path, text in sorted(workflows.items()):
        try:
            doc = yaml.safe_load(text)
        except yaml.YAMLError as exc:
            raise Unmeasured(f"{path}: не разобран: {exc}") from exc
        if not isinstance(doc, dict) or "workflow_run" not in _triggers(doc):
            continue
        c.consumers += 1
        for jname, job in (doc.get("jobs") or {}).items():
            c.jobs += 1
            if not any(_refs_secrets(s) for s in job.get("steps") or []) and not _refs_secrets(
                    {k: v for k, v in job.items() if k != "steps"}):
                continue
            c.jobs_with_secrets += 1
            where = f"{path}: jobs.{jname}"
            steps = job.get("steps") or []
            ids = [s.get("id") for s in steps]
            if DECISION_ID not in ids:
                findings.append(f"{where}: нет шага решения `id: {DECISION_ID}` — право на "
                                f"публикацию не судится по событию и ветке исходного прогона")
            else:
                di = ids.index(DECISION_ID)
                first_secret = next((i for i, s in enumerate(steps) if _refs_secrets(s)), None)
                if first_secret is not None and first_secret < di:
                    findings.append(f"{where}: шаг с секретами №{first_secret + 1} стоит ДО "
                                    f"шага решения №{di + 1}")
            for sc in SCENES:
                c.scenes += 1
                r = simulate(job, doc.get("env"), sc.github)
                published = r.job_if and r.secret_step is not None
                if sc.lawful:
                    if not published:
                        why = ("условие задания ложно" if not r.job_if else
                               f"шаг решения: код {r.decision_rc}, publish={r.decision_publish!r}"
                               if r.decision_rc not in (None, 0) or r.decision_publish != "true"
                               else "шаг с секретами не исполнен")
                        findings.append(f"{where}: [{sc.name}] законной публикации НЕТ — {why}")
                    continue
                # чужая сцена — три слоя порознь
                if r.job_if and not sc.branch_only:
                    findings.append(f"{where}: [{sc.name}] слой 1: условие задания истинно")
                rb = simulate(job, doc.get("env"), sc.github, bypass_job_if=True)
                if rb.secret_step is not None:
                    findings.append(f"{where}: [{sc.name}] слой 2: при обойдённом условии "
                                    f"задания исполнение доходит до «{rb.secret_step}» "
                                    f"(решение: код {rb.decision_rc}, publish={rb.decision_publish!r})")
                if DECISION_ID in ids:
                    rf = simulate(job, doc.get("env"), sc.github, bypass_job_if=True,
                                  forced_decision=(0, {"publish": "false"}))
                    if rf.secret_step is not None:
                        findings.append(f"{where}: [{sc.name}] слой 3: при ответе решения "
                                        f"«нет» без отказа шаг «{rf.secret_step}» исполняется")
    return findings, c


def tree_workflows(root: Path) -> dict[str, str]:
    out = subprocess.run(["git", "-C", str(root), "ls-files", ".github/workflows"],
                         capture_output=True, text=True, check=True).stdout.split()
    files = [p for p in out if p.endswith((".yml", ".yaml"))]
    return {p: (root / p).read_text() for p in files}


def verdict(workflows: dict[str, str]) -> tuple[int, list[str], Census | None]:
    try:
        f, c = judge(workflows)
    except Unmeasured as exc:
        return 2, [f"НЕ ИЗМЕРЕНО: {exc}"], None
    if c.workflows == 0 or c.jobs_with_secrets == 0:
        return 2, [f"судить нечего: процессов {c.workflows}, с workflow_run {c.consumers}, "
                   f"заданий с секретами {c.jobs_with_secrets}"], c
    return (1 if f else 0), f, c


def _census_line(c: Census) -> str:
    return (f"перепись: процессов {c.workflows} · с workflow_run {c.consumers} · заданий "
            f"{c.jobs} · с секретами {c.jobs_with_secrets} · сцен исполнено {c.scenes}")


# ───────────────────────── самопроверка ─────────────────────────

DOCKER = ".github/workflows/docker-build.yml"


def self_test(root: Path) -> int:
    real = tree_workflows(root)
    if DOCKER not in real:
        print(f"САМОПРОВЕРКА: предпосылка не выполнена — {DOCKER} нет в дереве", file=sys.stderr)
        return 2
    base = real[DOCKER]
    fails = 0
    probes = 0

    def mutate(old: str, new: str) -> str | None:
        if base.count(old) != 1:
            return None
        return base.replace(old, new)

    def expect(name, text, code, must=()):
        nonlocal fails, probes
        probes += 1
        if text is None:
            print(f"✗ {name}: инъекция не легла — образец не найден ровно один раз")
            fails += 1
            return
        got, f, _ = verdict({**real, DOCKER: text} if isinstance(text, str) else text)
        joined = "\n".join(f)
        miss = [m for m in must if m not in joined]
        if got != code or miss:
            print(f"✗ {name}: код {got} (ожидался {code}); недостаёт {miss}\n  {joined[:600]}")
            fails += 1
        else:
            print(f"✓ {name}")

    expect("контроль: дерево как есть", base, 0)
    expect("слой 1: из условия задания снято событие push",
           mutate("(github.event.workflow_run.event == 'push' &&\n",
                  "(\n"), 1,
           ["слой 1", "pull_request, голова этого репозитория"])
    expect("слой 1: из условия задания снято происхождение",
           mutate("github.event.workflow_run.head_repository.full_name == github.repository &&\n", ""),
           1, ["слой 1", "push, голова чужого репозитория"])
    expect("слой 2: решение не судит событие",
           mutate('if [ "${RUN_EVENT:-}" != "push" ]; then', 'if false; then'), 1,
           ["слой 2", "pull_request"])
    expect("слой 2: правило ветки расширено до любой",
           mutate("^main$", "^.*$"), 1,
           ["слой 2", "feature/x"])
    expect("слой 2: правило ветки без якоря конца",
           mutate("^main$", "^main"), 1,
           ["слой 2", "mainx"])
    expect("слой 2: правило ветки расширено до веток-номеров",
           mutate("^main$", "^(main|[0-9]+|[0-9]+-[^/]*)$"), 1,
           ["слой 2", "2942-x"])
    expect("слой 3: шаг с секретами без охраны",
           mutate("        if: steps.publish_right.outputs.publish == 'true'\n", ""), 1,
           ["слой 3"])
    expect("положительный контроль: задание не идёт никогда",
           mutate("github.event_name != 'workflow_run' ||", "false && github.event_name != 'workflow_run' ||"),
           1, ["законной публикации НЕТ", "собственный push в main"])
    expect("положительный контроль: решение отказывает всем",
           mutate('echo "право: push в ветку', 'exit 1; echo "право: push в ветку'), 1,
           ["законной публикации НЕТ", "push в main этого репозитория"])
    expect("нет шага решения",
           mutate("        id: publish_right\n", "        id: publish_right_gone\n"), 1,
           ["нет шага решения"])
    expect("непонятое условие — не измерено, а не зелёное",
           mutate("github.event_name != 'workflow_run' ||", "contains(github.ref, 'x') ||"), 2,
           ["НЕ ИЗМЕРЕНО"])
    # законный близнец: workflow_run без секретов — молчание и учёт в переписи
    twin = ("on:\n  workflow_run:\n    workflows: [x]\n    types: [completed]\n"
            "jobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n")
    expect("законный близнец: workflow_run без секретов", {**real, ".github/workflows/twin.yml": twin}, 0)
    expect("нет предмета: процессов workflow_run с секретами ноль",
           {".github/workflows/twin.yml": twin}, 2, ["судить нечего"])
    print(f"самопроверка: подпроб {probes}, провалов {fails}")
    return 1 if fails else 0


def main(argv: list[str]) -> int:
    root = Path(subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True,
                               text=True, check=True).stdout.strip())
    if "--self-test" in argv:
        return self_test(root)
    code, f, c = verdict(tree_workflows(root))
    if c:
        print(_census_line(c))
    for line in f:
        print(("::error::" if code == 1 else "") + line)
    print({0: "находок 0", 1: f"находок {len(f)}", 2: "ОТКАЗ: не измерено"}[code])
    return code


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
