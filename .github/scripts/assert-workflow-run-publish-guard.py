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

ЕДИНИЦА СЧЁТА — задание процесса с триггером `workflow_run`, которому доступен
контекст `secrets`: в шаге, на уровне задания (`env`, `container`, `services`,
`with`, ключ `secrets:` вызова переиспользуемого процесса) либо на уровне процесса
(`env`). Ссылка на контекст распознаётся РАЗБОРОМ выражения `${{ … }}` (и неявного
выражения `if:`): лексемы вне строковых литералов, корень `secrets` не после точки
и не имя функции — то есть `secrets.X`, `secrets['X']`, `toJSON(secrets)`, любой
регистр. Текст `secrets` вне выражения и свойство `steps.secrets` — не ссылка.
Перечень процессов выводится из `git ls-files .github/workflows`.

ПУБЛИКАЦИЯ В СЦЕНАРИИ = задание идёт (его `if:` истинно) И исполнение доходит до
шага, читающего секреты. Сцены двух знаков, и обе обязательны:

  * ЧУЖИЕ (публикации нет): исходный прогон поднят не `push` (запрос, запрос с
    правами базы, ручной запуск), голова не из этого репозитория, ветка вне
    правила — на этой редакции правило равно фильтру `workflow_run.branches`:
    ствол `main` и ничего больше; собственное событие процесса вне перечня и
    собственный `push` в ссылку вне правила (см. «Собственные события»);
  * ЗАКОННЫЕ (публикация есть): `push` этого репозитория в `main`; собственный
    `push` в ствол и метки версии; ручной запуск. Без них гейт молчал
    бы на процессе, который не публикует НИЧЕГО, — запрет без положительного
    контроля.

СЛОЁВ ТРИ, И КАЖДЫЙ СУДИТСЯ ПОРОЗНЬ на чужих сценах, иначе один слой прятал бы
снятие другого:
  1. условие задания ложно (кроме сцен «ветка вне правила»: имя с образцом
     выражением не сопоставить, это работа слоя 2);
  2. шаг решения (`id: publish_right`) при обойдённом условии задания ОСТАНАВЛИВАЕТ
     исполнение: отказывает, и отказ не погашен `continue-on-error`. Судится при
     СНЯТОЙ охране шагов с секретами — их условие вычисляется при выводах шагов,
     равных всему, — охрана слоя 3 не прячет снятие слоя 2. Функции состояния в
     этом условии смысл сохраняют: шаг по `failure()`, `always()`, `!success()`
     идёт и ПОСЛЕ отказа, то есть отказ его не останавливает. Шаг решения
     исполняется той оболочкой, которой его исполнит площадка (`shell` шага, иначе
     `defaults.run.shell` задания, иначе процесса, иначе `bash -e {0}`); оболочка,
     которую гейт не исполняет (не bash и не sh), — находка слоя 2 и худший случай
     «выход с кодом 0 без ответа»;
  3. шаг с секретами охраняет своё условие: при ответе решения «нет» БЕЗ отказа,
     а также при ОТСУТСТВИИ ответа без отказа, он не исполняется.

МЕСТО ПРОВЕРКИ — судится структурно, отдельно от сцен: шаг решения есть и стоит
ПЕРВЫМ — до него нет ни одного шага (выкачка и чужое действие меняют машину, шаг
`run:` — окружение решения через `GITHUB_ENV`, а гейт таких шагов не исполняет);
сам он секретов не читает; задание и процесс не дают секретов всем шагам сразу.
Окружение шага решения в исполнении гейта — `env` процесса, задания и шага.

СОБСТВЕННЫЕ СОБЫТИЯ ПРОЦЕССА — ПЕРЕЧНЕМ. Законные: `push` в ссылку правила этого
файла (ствол `main`, метка версии `v<число>.<число>.<число>`) и ручной
запуск; чужие: запрос, запрос с правами базы, очередь слияния, расписание и прочие
события, а также собственный `push` в ссылку вне правила. Фильтр `push` в `on:` —
не держатель: решение судит ссылку само, поэтому его расширение права не даёт.

ТРИГГЕРЫ — судятся структурно: `on:` процесса-потребителя с секретами не выходит
за перечень `push`, `workflow_dispatch`, `workflow_run`. Событие вне перечня —
находка, даже если решение ему откажет: его исход гейт как законный не судит.

ПАРАЛЛЕЛЬНОСТЬ — судится исходом: группа параллелизма (процесса и задания)
вычисляется в каждой сцене, и чужая сцена не вправе получить группу законной.
Прогон в общей группе вытесняет законный — публикация ствола отменяется прогоном,
которому задание откажет.

ЯЗЫК УСЛОВИЙ. Вычисляется подмножество выражений площадки: `|| && ! == !=`,
скобки, строки, `true/false/null`, числа, контексты `github`, `steps`, `env`,
`secrets` (значение — непустая строка-заглушка) и функции состояния
`success() failure() always() cancelled()` (имя без учёта регистра; вызов
распознаётся разбором, а не текстом). `continue-on-error` шага исполняется:
отказ такого шага задание не останавливает. Сравнение строк —
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
import shlex
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
SECRET_VALUE = "<секрет>"

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
            v = self.not_()
            # отрицание худшего случая — снова худший случай, а не «ложь»: иначе
            # `!` над неизвестным выводом прятал бы исполнение шага
            return ANY if v is ANY else not truthy(v)
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
                fn = val.casefold()  # имена функций площадка сверяет без учёта регистра
                if fn not in STATUS_FUNCS:
                    raise Unmeasured(f"функция {val}() не входит в вычисляемое подмножество")
                f = self.ctx.failed
                return {"success": not f, "failure": f, "always": True, "cancelled": False}[fn]
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
        elif root == "secrets":
            # Секреты базового репозитория есть: значение — непустая заглушка.
            return SECRET_VALUE if len(parts) > 1 else {"*": SECRET_VALUE}
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
    """Есть ли в условии ВЫЗОВ функции состояния — разбором, а не образцом: текст
    'failure()' в строковом литерале вызовом не является. Без вызова площадка
    подразумевает `success() &&`."""
    if isinstance(expr, bool):
        return False
    toks = _tokens(_strip_braces(str(expr)))
    return any(k == "id" and v.casefold() in STATUS_FUNCS
               and i + 1 < len(toks) and toks[i + 1][1] == "("
               for i, (k, v) in enumerate(toks))


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


_REF_TOKEN = re.compile(
    r"\s*(?:(?P<str>'(?:[^']|'')*')"
    r"|(?P<num>0[xX][0-9A-Fa-f]+|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)"
    r"|(?P<id>[A-Za-z_][A-Za-z0-9_\-]*)"
    r"|(?P<op>\|\||&&|==|!=|<=|>=|[<>!()\[\],.*]))"
)


def expr_bodies(text: str) -> list[str]:
    """Тела всех `${{ … }}` в тексте; `}}` внутри строкового литерала тело не
    закрывает. Незакрытое выражение — не измерено, а не «ссылок нет»."""
    out, i = [], 0
    while True:
        j = text.find("${{", i)
        if j < 0:
            return out
        k, quoted = j + 3, False
        while k < len(text):
            if quoted:
                if text[k] == "'":
                    if text.startswith("''", k):
                        k += 2
                        continue
                    quoted = False
            elif text[k] == "'":
                quoted = True
            elif text.startswith("}}", k):
                break
            k += 1
        else:
            raise Unmeasured(f"незакрытое выражение: {text[j:j + 40]!r}")
        out.append(text[j + 3:k])
        i = k + 2


def context_roots(body: str) -> set[str]:
    """Корни контекстов, на которые ссылается выражение: идентификатор не после
    точки и не имя вызываемой функции; регистр не различается, как у площадки."""
    toks, pos, body = [], 0, body.strip()
    while pos < len(body):
        m = _REF_TOKEN.match(body, pos)
        if not m or m.end() == pos:
            if body[pos:].strip() == "":
                break
            raise Unmeasured(f"не разобрано выражение у позиции {pos}: {body[pos:pos + 30]!r}")
        pos = m.end()
        toks.append((m.lastgroup, m.group(m.lastgroup)))
    roots = set()
    for i, (kind, val) in enumerate(toks):
        if kind != "id":
            continue
        after_dot = i > 0 and toks[i - 1][1] == "."
        is_call = i + 1 < len(toks) and toks[i + 1][1] == "("
        if not after_dot and not is_call:
            roots.add(val.casefold())
    return roots


def refs_secrets(node, key=None) -> bool:
    """Есть ли в узле разобранного YAML ссылка на контекст `secrets`."""
    if isinstance(node, str):
        bodies = expr_bodies(node)
        if key == "if" and "${{" not in node:
            bodies = [node]  # `if:` — неявное выражение
        return any("secrets" in context_roots(b) for b in bodies)
    if isinstance(node, dict):
        return any(refs_secrets(v, k) for k, v in node.items())
    if isinstance(node, list):
        return any(refs_secrets(v) for v in node)
    return False


def job_level_exposure(job: dict, doc: dict) -> list[str]:
    """Где секреты доступны заданию ЦЕЛИКОМ, до любого шага: ключ `secrets:` вызова
    переиспользуемого процесса, ключи задания кроме шагов, ключи процесса кроме
    триггеров и заданий."""
    out = []
    if "secrets" in job:
        out.append("задание: ключ secrets")
    out += [f"задание: {k}" for k, v in job.items()
            if k not in ("steps", "secrets") and refs_secrets(v, k)]
    out += [f"процесс: {k}" for k, v in doc.items()
            if k not in ("on", True, "jobs") and refs_secrets(v, k)]
    return out


def _step_label(step: dict) -> str:
    return str(step.get("name") or step.get("id") or step.get("uses") or "<шаг без имени>")


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
            "conclusion": conclusion,
            "head_repository": None if repo is None else {"full_name": repo},
            # репозиторий самого исходного процесса — всегда этот: подмена им
            # происхождения головы обязана краснеть на чужой голове
            "repository": {"full_name": THIS_REPO},
        }},
    }


def _own(event, ref, head_repo=None):
    """Собственное событие процесса: ссылка и (для запроса) репозиторий головы."""
    ev = {} if head_repo is None else {
        "pull_request": {"head": {"repo": {"full_name": head_repo}}}}
    return {"event_name": event, "repository": THIS_REPO, "ref": ref,
            "ref_name": ref.split("/", 2)[-1], "event": ev}


# Собственные события, которые процесс вправе принимать: остальные — находка
# «триггеры» (их исход сцены не судят как законный), а их сцены — чужие.
ALLOWED_TRIGGERS = ("push", "workflow_dispatch", "workflow_run")

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
    Scene("workflow_run ← merge_group",
          _wr("merge_group", "main", THIS_REPO), False),
    Scene("workflow_run ← schedule",
          _wr("schedule", "main", THIS_REPO), False),
    Scene("workflow_run ← workflow_run",
          _wr("workflow_run", "main", THIS_REPO), False),
    Scene("workflow_run ← push, голова без репозитория",
          _wr("push", "main", None), False),
    Scene("workflow_run ← push, голова чужого репозитория",
          _wr("push", "main", FOREIGN_REPO), False),
    Scene("workflow_run ← push, ветка вне правила feature/x",
          _wr("push", "feature/x", THIS_REPO), False, True),
    Scene("workflow_run ← push, ветка вне правила 2942/x",
          _wr("push", "2942/x", THIS_REPO), False, True),
    Scene("workflow_run ← push, ветка вне правила mainx",
          _wr("push", "mainx", THIS_REPO), False, True),
    Scene("workflow_run ← push, ветка вне правила feature/main",
          _wr("push", "feature/main", THIS_REPO), False, True),
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
    # ── собственные события процесса ──
    # Чужие: событие вне перечня разрешённых. Решение обязано отказать им ЯВНО, а
    # не пропустить как «не workflow_run»: запрос из ветки этого репозитория и
    # запрос с правами базы получили бы секреты.
    Scene("собственный pull_request, голова этого репозитория",
          _own("pull_request", "refs/pull/7/merge", THIS_REPO), False),
    Scene("собственный pull_request, голова чужого репозитория",
          _own("pull_request", "refs/pull/7/merge", FOREIGN_REPO), False),
    Scene("собственный pull_request_target",
          _own("pull_request_target", "refs/heads/main", FOREIGN_REPO), False),
    Scene("собственный merge_group",
          _own("merge_group", "refs/heads/gh-readonly-queue/main/pr-7-0"), False),
    Scene("собственный schedule", _own("schedule", "refs/heads/main"), False),
    Scene("собственный repository_dispatch", _own("repository_dispatch", "refs/heads/main"), False),
    Scene("собственный issue_comment", _own("issue_comment", "refs/heads/main"), False),
    # Чужие по ссылке: собственный push вне правила фильтра этого файла (main,
    # метка версии). Фильтр — не держатель: решение судит ссылку само. Форма
    # `KAC-*` снята из фильтра (kacho#2807) и потому — чужая сцена: решение,
    # пускающее её, шире триггера.
    Scene("собственный push, ветка вне правила feature/x",
          _own("push", "refs/heads/feature/x"), False, True),
    Scene("собственный push, ветка вне правила mainx",
          _own("push", "refs/heads/mainx"), False, True),
    Scene("собственный push, ветка вне правила x/refs/heads/main",
          _own("push", "refs/heads/x/refs/heads/main"), False, True),
    Scene("собственный push, ветка вне правила KAC-1",
          _own("push", "refs/heads/KAC-1"), False, True),
    Scene("собственный push, ветка вне правила KAC-1/x",
          _own("push", "refs/heads/KAC-1/x"), False, True),
    Scene("собственный push, метка вне правила v1.2",
          _own("push", "refs/tags/v1.2"), False, True),
    Scene("собственный push, метка вне правила v1.2.3-rc",
          _own("push", "refs/tags/v1.2.3-rc"), False, True),
    Scene("собственный push, метка вне правила v1x2x3",
          _own("push", "refs/tags/v1x2x3"), False, True),
    # Законные.
    Scene("собственный push в main", _own("push", "refs/heads/main"), True),
    Scene("собственный push метки v1.2.3", _own("push", "refs/tags/v1.2.3"), True),
    Scene("собственный workflow_dispatch на ветке feature/x",
          _own("workflow_dispatch", "refs/heads/feature/x"), True),
]


@dataclass
class Run:
    job_if: bool
    decision_rc: int | None = None
    decision_publish: str | None = None
    secret_steps: list[str] = field(default_factory=list)  # исполненные, читающие секреты

    @property
    def secret_step(self) -> str | None:
        return self.secret_steps[0] if self.secret_steps else None


# Оболочка шага `run:` у площадки: ключ `shell` шага, иначе `defaults.run.shell`
# задания, иначе процесса, иначе умолчание раннера Linux. Имена `bash` и `sh` —
# записи площадки; прочее — шаблон `команда … {0}`, где `{0}` — файл сценария.
PLATFORM_SHELLS = {
    "bash": "bash --noprofile --norc -eo pipefail {0}",
    "sh": "sh -e {0}",
}
DEFAULT_SHELL = ("bash -e {0}", "умолчание площадки")
EXECUTED_SHELLS = {"bash", "sh"}


def decision_shell(step: dict, job: dict, doc: dict) -> tuple[str, str]:
    """Оболочка, которой площадка исполнит шаг, и уровень, откуда она взята."""
    levels = (("шаг", step),
              ("задание", ((job.get("defaults") or {}).get("run") or {})),
              ("процесс", ((doc.get("defaults") or {}).get("run") or {})))
    for source, holder in levels:
        if isinstance(holder, dict) and holder.get("shell") is not None:
            v = str(holder["shell"]).strip()
            if "${{" in v:
                raise Unmeasured(f"оболочка выражением ({source}): {v!r}")
            return v, source
    return DEFAULT_SHELL


def shell_argv(shell: str, script: str) -> list[str] | None:
    """Команда, которой площадка исполнит сценарий, — либо None, если гейт её не
    исполняет (не bash и не sh): тогда исход шага — худший случай."""
    tmpl = PLATFORM_SHELLS.get(shell, shell)
    if "{0}" not in tmpl:
        return None
    try:
        argv = shlex.split(tmpl)
    except ValueError:
        return None
    if not argv or os.path.basename(argv[0]) not in EXECUTED_SHELLS:
        return None
    return [a.replace("{0}", script) for a in argv]


def _run_decision(step: dict, ctx: Ctx, job: dict, doc: dict) -> tuple[int, dict]:
    # окружение шага у площадки: процесс, затем задание, затем шаг
    env = {"PATH": os.environ.get("PATH", "/usr/bin:/bin")}
    for holder in (doc, job, step):
        for k, v in (holder.get("env") or {}).items():
            env[str(k)] = interpolate(str(v), ctx)
    script = interpolate(str(step.get("run", "")), ctx)
    shell, _ = decision_shell(step, job, doc)
    with tempfile.TemporaryDirectory() as d:
        body = Path(d) / "step.sh"
        body.write_text(script)
        argv = shell_argv(shell, str(body))
        if argv is None:
            # Худший случай для слоя 2: выход с кодом 0 и без ответа.
            return 0, {}
        out = Path(d) / "out"
        out.write_text("")
        env["GITHUB_OUTPUT"] = str(out)
        p = subprocess.run(argv, env=env, capture_output=True, text=True)
        outputs = {}
        for line in out.read_text().splitlines():
            if "=" in line:
                k, v = line.split("=", 1)
                outputs[k] = v
    return p.returncode, outputs


def _continue_on_error(step: dict, ctx: Ctx) -> bool:
    v = step.get("continue-on-error")
    if v is None:
        return False
    if isinstance(v, bool):
        return v
    return truthy(evaluate(v, ctx))


def simulate(job: dict, doc: dict, github: dict, *, bypass_job_if=False,
             forced_decision: tuple[int, dict] | None = None,
             neutral_secret_guards=False) -> Run:
    """Исполнение задания в сцене. `neutral_secret_guards` снимает охрану слоя 3 у
    шагов, читающих секреты: их условие вычисляется при выводах шагов, равных всему
    (худший случай), а функции состояния СОХРАНЯЮТ смысл. Так судится слой 2 —
    останавливает ли отказ решения исполнение: `failure()`, `always()`, `!success()`
    в условии шага с секретами отказ обходят, и снятием условия это не спрятать."""
    ctx = Ctx(github=github, env={**(doc.get("env") or {}), **(job.get("env") or {})})
    cond = job.get("if")
    job_if = True if cond is None else truthy(evaluate(cond, ctx))
    run = Run(job_if=job_if)
    if not job_if and not bypass_job_if:
        return run
    run.secret_steps += [f"<{w}>" for w in job_level_exposure(job, doc)]
    for step in job.get("steps") or []:
        sid = step.get("id")
        reads = refs_secrets(step)
        sc = step.get("if")
        if sc is None:
            runs = not ctx.failed
        else:
            ectx = ctx
            if reads and neutral_secret_guards:
                ectx = Ctx(github=ctx.github, steps=ANY, env=ctx.env, failed=ctx.failed)
            v = truthy(evaluate(sc, ectx))
            runs = v if uses_status_func(sc) else (v and not ctx.failed)
        if not runs:
            if sid:
                ctx.steps[sid] = {"outputs": {}, "outcome": "skipped", "conclusion": "skipped"}
            continue
        if reads:
            run.secret_steps.append(_step_label(step))
        if sid == DECISION_ID:
            rc, outs = forced_decision if forced_decision else _run_decision(step, ctx, job, doc)
            run.decision_rc, run.decision_publish = rc, outs.get("publish")
            tolerated = rc != 0 and _continue_on_error(step, ctx)
            ctx.steps[sid] = {"outputs": outs, "outcome": "success" if rc == 0 else "failure",
                              "conclusion": "success" if rc == 0 or tolerated else "failure"}
            if rc != 0 and not tolerated:
                ctx.failed = True
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


def concurrency_groups(job: dict, doc: dict, github: dict) -> list[tuple[str, str]]:
    """Группы параллелизма, в которые прогон сцены встаёт: процесса и задания.
    Прогон в общей группе вытесняет ждущий (а при `cancel-in-progress` — и
    идущий) прогон той же группы, поэтому судится сама общность группы."""
    out = []
    for level, holder in (("процесса", doc), ("задания", job)):
        c = holder.get("concurrency")
        if c is None:
            continue
        g = c.get("group") if isinstance(c, dict) else c
        if g is None:
            raise Unmeasured(f"группа параллелизма {level} без ключа group")
        out.append((level, interpolate(str(g), Ctx(github=github)).casefold()))
    return out


LAYER3_ANSWERS = [("«нет»", (0, {"publish": "false"})), ("без ответа", (0, {}))]


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
        consumer_with_secrets = False
        for jname, job in (doc.get("jobs") or {}).items():
            c.jobs += 1
            steps = job.get("steps") or []
            exposure = job_level_exposure(job, doc)
            if not exposure and not any(refs_secrets(s) for s in steps):
                continue
            c.jobs_with_secrets += 1
            consumer_with_secrets = True
            where = f"{path}: jobs.{jname}"
            # ── место проверки ──
            for w in exposure:
                findings.append(f"{where}: место проверки: секреты доступны всем шагам "
                                f"сразу ({w}) — до шага решения")
            ids = [s.get("id") for s in steps]
            if DECISION_ID not in ids:
                findings.append(f"{where}: нет шага решения `id: {DECISION_ID}` — право на "
                                f"публикацию не судится по событию и ветке исходного прогона")
            else:
                di = ids.index(DECISION_ID)
                shell, source = decision_shell(steps[di], job, doc)
                if shell_argv(shell, "x") is None:
                    findings.append(f"{where}: слой 2: шаг решения №{di + 1} исполняется "
                                    f"оболочкой '{shell}' ({source}) — гейт исполняет только "
                                    f"bash и sh, отказ решения не доказан; судится худший "
                                    f"случай: выход с кодом 0 без ответа")
                if refs_secrets(steps[di]):
                    findings.append(f"{where}: место проверки: шаг решения №{di + 1} сам "
                                    f"читает секреты")
                for i, st in enumerate(steps[:di]):
                    if refs_secrets(st):
                        findings.append(f"{where}: место проверки: шаг с секретами №{i + 1} "
                                        f"«{_step_label(st)}» стоит ДО шага решения №{di + 1}")
                    # Любой шаг до решения меняет его окружение (GITHUB_ENV,
                    # GITHUB_PATH, файлы машины), а гейт такие шаги не исполняет:
                    # решение обязано стоять первым.
                    kind = "uses" if "uses" in st else "run"
                    findings.append(f"{where}: место проверки: шаг №{i + 1} "
                                    f"«{_step_label(st)}» ({kind}) исполняется ДО шага "
                                    f"решения №{di + 1}")
            # ── сцены ──
            # группы законных сцен: чужой прогон в той же группе вытесняет публикацию
            lawful_groups: dict[tuple[str, str], str] = {}
            for sc in SCENES:
                if sc.lawful:
                    for key in concurrency_groups(job, doc, sc.github):
                        lawful_groups.setdefault(key, sc.name)
            for sc in SCENES:
                c.scenes += 1
                r = simulate(job, doc, sc.github)
                published = r.job_if and r.secret_step is not None
                if not sc.lawful:
                    for level, group in concurrency_groups(job, doc, sc.github):
                        if level == "задания" and not r.job_if:
                            continue  # задание не идёт — в группу задания не встаёт
                        other = lawful_groups.get((level, group))
                        if other is not None:
                            findings.append(
                                f"{where}: [{sc.name}] параллельность: группа {level} "
                                f"«{group}» та же, что у законной сцены [{other}] — "
                                f"прогон, которому публикация не положена, вытесняет "
                                f"законный")
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
                rb = simulate(job, doc, sc.github, bypass_job_if=True, neutral_secret_guards=True)
                if rb.secret_step is not None:
                    findings.append(f"{where}: [{sc.name}] слой 2: при обойдённом условии "
                                    f"задания решение не остановило исполнение до "
                                    f"«{rb.secret_step}» (решение: код {rb.decision_rc}, "
                                    f"publish={rb.decision_publish!r})")
                if DECISION_ID in ids:
                    for label, answer in LAYER3_ANSWERS:
                        rf = simulate(job, doc, sc.github, bypass_job_if=True,
                                      forced_decision=answer)
                        if rf.secret_step is not None:
                            findings.append(f"{where}: [{sc.name}] слой 3: при ответе решения "
                                            f"{label} без отказа шаг «{rf.secret_step}» "
                                            f"исполняется")
        # ── триггеры: собственные события процесса ──
        if consumer_with_secrets:
            for t in _triggers(doc):
                if t not in ALLOWED_TRIGGERS:
                    findings.append(f"{path}: триггеры: процесс поднимается событием "
                                    f"'{t}' — вне перечня {', '.join(ALLOWED_TRIGGERS)}")
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


LAYERS = ("слой 1", "слой 2", "слой 3", "место проверки", "триггеры", "параллельность")


def _parser_probes() -> list[tuple[str, str, bool]]:
    """Распознавание ссылки на `secrets` — разбором, а не образцом: формы ссылки
    (обязаны находиться) и законные близнецы той же формы (обязаны молчать)."""
    return [
        ("точка", "${{ secrets.A }}", True),
        ("индекс", "${{ secrets['A'] }}", True),
        ("индекс с пробелами", "${{ secrets [ 'A' ] }}", True),
        ("весь контекст функцией", "${{ toJSON(secrets) }}", True),
        ("аргумент format", "${{ format('{0}', secrets.A) }}", True),
        ("регистр", "${{ SECRETS.A }}", True),
        ("в скобках", "${{ (secrets).A }}", True),
        ("`}}` в литерале не закрывает выражение", "${{ format('}}', secrets.A) }}", True),
        ("второе выражение строки", "a ${{ github.sha }} b ${{ secrets.A }}", True),
        ("близнец: текст вне выражения", "echo secrets.A secrets['A'] toJSON(secrets)", False),
        ("близнец: строковый литерал", "${{ format('{0}', 'secrets.A') }}", False),
        ("близнец: свойство steps.secrets", "${{ toJSON(steps.secrets) }}", False),
        ("близнец: свойство github.secrets", "${{ github.secrets }}", False),
        ("близнец: индекс по строке 'secrets'", "${{ env['secrets'] }}", False),
    ]


def self_test(root: Path) -> int:
    real = tree_workflows(root)
    if DOCKER not in real:
        print(f"САМОПРОВЕРКА: предпосылка не выполнена — {DOCKER} нет в дереве", file=sys.stderr)
        return 2
    base = real[DOCKER]
    fails = 0
    probes = 0

    def mutate(old: str, new: str, text: str | None = None) -> str | None:
        src = base if text is None else text
        if src.count(old) != 1:
            return None
        return src.replace(old, new)

    def expect(name, text, code, must=(), only=None):
        """`only` — слой, ЕДИНСТВЕННО вправе дать находки: порча роняет ровно свой
        предмет, а не соседний."""
        nonlocal fails, probes
        probes += 1
        if text is None:
            print(f"✗ {name}: инъекция не легла — образец не найден ровно один раз")
            fails += 1
            return
        got, f, _ = verdict({**real, DOCKER: text} if isinstance(text, str) else text)
        joined = "\n".join(f)
        miss = [m for m in must if m not in joined]
        alien = [x for x in LAYERS if only and x != only and x in joined]
        if got != code or miss or alien:
            print(f"✗ {name}: код {got} (ожидался {code}); недостаёт {miss}; чужие слои "
                  f"{alien}\n  {joined[:900]}")
            fails += 1
        else:
            print(f"✓ {name}")

    # ── распознавание ссылки на секреты ──
    for name, text, want in _parser_probes():
        probes += 1
        got = refs_secrets({"run": text})
        if got != want:
            print(f"✗ разбор ссылки — {name}: {text!r} → {got} (ожидалось {want})")
            fails += 1
        else:
            print(f"✓ разбор ссылки — {name}")
    probes += 1
    try:
        refs_secrets({"run": "${{ secrets.A "})
        print("✗ разбор ссылки — незакрытое выражение принято молча")
        fails += 1
    except Unmeasured:
        print("✓ разбор ссылки — незакрытое выражение: не измерено")

    expect("контроль: дерево как есть", base, 0)

    # ── слой 1: условие задания (событие, происхождение) ──
    expect("слой 1: из условия задания снято событие push",
           mutate("(github.event.workflow_run.event == 'push' &&\n",
                  "(\n"), 1,
           ["слой 1", "pull_request, голова этого репозитория"], "слой 1")
    expect("слой 1: событие — запрет одного вместо разрешения push",
           mutate("github.event.workflow_run.event == 'push' &&",
                  "github.event.workflow_run.event != 'pull_request' &&"), 1,
           ["слой 1", "pull_request_target", "merge_group"], "слой 1")
    expect("слой 1: из условия задания снято происхождение",
           mutate("github.event.workflow_run.head_repository.full_name == github.repository &&\n", ""),
           1, ["слой 1", "push, голова чужого репозитория"], "слой 1")
    expect("слой 1: происхождение по репозиторию процесса, а не головы",
           mutate("github.event.workflow_run.head_repository.full_name == github.repository",
                  "github.event.workflow_run.repository.full_name == github.repository"),
           1, ["слой 1", "push, голова чужого репозитория"], "слой 1")

    # ── слой 2: шаг решения (событие, происхождение, ветка, остановка) ──
    expect("слой 2: решение не судит событие",
           mutate('if [ "${RUN_EVENT:-}" != "push" ]; then', 'if false; then'), 1,
           ["слой 2", "pull_request"], "слой 2")
    expect("слой 2: событие — запрет одного вместо разрешения push",
           mutate('if [ "${RUN_EVENT:-}" != "push" ]; then',
                  'if [ "${RUN_EVENT:-}" = "pull_request" ]; then'), 1,
           ["слой 2", "pull_request_target", "workflow_dispatch"], "слой 2")
    expect("слой 2: решение не судит происхождение",
           mutate('[ "$RUN_REPO" != "$THIS_REPO" ]', 'false'), 1,
           ["слой 2", "push, голова чужого репозитория"], "слой 2")
    expect("слой 2: правило ветки расширено до любой",
           mutate("^main$", "^.*$"), 1,
           ["слой 2", "feature/x"], "слой 2")
    expect("слой 2: правило ветки без якоря конца",
           mutate("^main$", "^main"), 1,
           ["слой 2", "mainx"], "слой 2")
    expect("слой 2: правило ветки без якоря начала",
           mutate("^main$", "main$"), 1,
           ["слой 2", "feature/main"], "слой 2")
    expect("слой 2: правило ветки расширено до веток-номеров",
           mutate("^main$", "^(main|[0-9]+|[0-9]+-[^/]*)$"), 1,
           ["слой 2", "2942-x"], "слой 2")
    expect("слой 2: ветка взята из контекста процесса, а не головы",
           mutate("RUN_BRANCH: ${{ github.event.workflow_run.head_branch }}\n          RUN_REPO",
                  "RUN_BRANCH: ${{ github.ref_name }}\n          RUN_REPO"), 1,
           ["слой 2", "feature/x"], "слой 2")
    expect("слой 2: отказ решения погашен continue-on-error",
           mutate("        id: publish_right\n",
                  "        id: publish_right\n        continue-on-error: true\n"), 1,
           ["слой 2", "pull_request"], "слой 2")
    expect("слой 2: отказ погашен continue-on-error выражением",
           mutate("        id: publish_right\n",
                  "        id: publish_right\n        continue-on-error: ${{ github.event_name == 'workflow_run' }}\n"),
           1, ["слой 2"], "слой 2")
    expect("законный близнец: continue-on-error: false у решения",
           mutate("        id: publish_right\n",
                  "        id: publish_right\n        continue-on-error: false\n"), 0)

    # слой 2: шаг с секретами идёт ИМЕННО при отказе решения — функция состояния в
    # его условии. Охрана слоя 3 тут не мешает судить слой 2: снимается она, а не
    # функции состояния, которые и решают, идёт ли шаг после отказа.
    guard = "        if: steps.publish_right.outputs.publish == 'true'\n"
    login = "        if: steps.gate.outputs.push == 'true'\n"
    expect("слой 2: вход в реестр по failure()",
           mutate(login, "        if: failure()\n"), 1,
           ["слой 2", "login to Docker Hub"], "слой 2")
    expect("слой 2: вход в реестр по failure() ИЛИ законному ответу",
           mutate(login, "        if: failure() || steps.gate.outputs.push == 'true'\n"), 1,
           ["слой 2", "login to Docker Hub"], "слой 2")
    expect("слой 2: шаг с секретами по failure() ИЛИ законному ответу",
           mutate(guard, "        if: failure() || steps.publish_right.outputs.publish == 'true'\n"),
           1, ["слой 2", "gate on DockerHub secrets"], "слой 2")
    expect("слой 2: шаг с секретами по отрицанию success()",
           mutate(guard, "        if: ${{ !success() }}\n"), 1,
           ["слой 2", "gate on DockerHub secrets"])
    expect("слой 2: функция состояния в другом регистре",
           mutate(login, "        if: Failure()\n"), 1,
           ["слой 2", "login to Docker Hub"], "слой 2")
    expect("слой 2: always() И законный ответ — отказ не останавливает, держит один слой 3",
           mutate(guard, "        if: always() && steps.publish_right.outputs.publish == 'true'\n"),
           1, ["слой 2", "gate on DockerHub secrets"], "слой 2")
    expect("законный близнец: success() И законный ответ",
           mutate(guard, "        if: success() && steps.publish_right.outputs.publish == 'true'\n"),
           0)
    expect("законный близнец: 'failure()' строковым литералом",
           mutate(guard, "        if: steps.publish_right.outputs.publish == 'true' && "
                         "'failure()' != ''\n"), 0)

    # слой 2: оболочка решения — та, что исполнит площадка. Переопределение на любом
    # из трёх уровней (шаг, задание, процесс) оболочкой, которая выходит с кодом 0 и
    # ничего не пишет, снимает отказ решения.
    expect("слой 2: оболочка шага решения переопределена",
           mutate("        id: publish_right\n",
                  "        id: publish_right\n        shell: true {0}\n"), 1,
           ["слой 2", "оболочкой 'true {0}'"], "слой 2")
    expect("слой 2: оболочка задания переопределена",
           mutate("    runs-on: ubuntu-latest\n",
                  "    runs-on: ubuntu-latest\n    defaults:\n      run:\n        shell: true {0}\n"),
           1, ["слой 2", "оболочкой 'true {0}'", "задание"], "слой 2")
    expect("слой 2: оболочка процесса переопределена",
           mutate("    shell: bash\n", "    shell: true {0}\n"), 1,
           ["слой 2", "оболочкой 'true {0}'", "процесс"], "слой 2")
    expect("слой 2: оболочка шага — bash без -e и без отказа в сценарии",
           mutate("        id: publish_right\n",
                  "        id: publish_right\n        shell: bash -c 'exit 0' {0}\n"), 1,
           ["слой 2"], "слой 2")
    expect("законный близнец: shell: bash у шага решения",
           mutate("        id: publish_right\n",
                  "        id: publish_right\n        shell: bash\n"), 0)
    expect("законный близнец: shell: bash у задания",
           mutate("    runs-on: ubuntu-latest\n",
                  "    runs-on: ubuntu-latest\n    defaults:\n      run:\n        shell: bash\n"), 0)
    expect("законный близнец: оболочка шага решения записью площадки",
           mutate("        id: publish_right\n",
                  "        id: publish_right\n"
                  "        shell: bash --noprofile --norc -eo pipefail {0}\n"), 0)
    expect("законный близнец: умолчание площадки (оболочка процесса снята)",
           mutate("defaults:\n  run:\n    shell: bash\n", ""), 0)

    # ── слой 3: условие шага с секретами ──
    guard = "        if: steps.publish_right.outputs.publish == 'true'\n"
    expect("слой 3: шаг с секретами без охраны",
           mutate(guard, ""), 1, ["слой 3"], "слой 3")
    expect("слой 3: охрана «не нет» вместо «да» — пропускает отсутствие ответа",
           mutate(guard, "        if: steps.publish_right.outputs.publish != 'false'\n"), 1,
           ["слой 3", "без ответа"], "слой 3")
    expect("слой 3: охрана по исходу шага, а не по ответу",
           mutate(guard, "        if: steps.publish_right.outcome == 'success'\n"), 1,
           ["слой 3", "«нет»"], "слой 3")
    expect("слой 3: охрана выражением-строкой (непустая строка истинна)",
           mutate(guard, "        if: ${{ steps.publish_right.outputs.publish }}\n"), 1,
           ["слой 3", "«нет»"], "слой 3")
    expect("слой 3: охрана по чужому шагу",
           mutate(guard, "        if: steps.subject.outputs.publish == 'true'\n"), 1,
           ["слой 3"], "слой 3")
    expect("слой 3: секрет индексом в шаге выкачки",
           mutate("          ref: ${{ steps.subject.outputs.ref }}\n",
                  "          ref: ${{ steps.subject.outputs.ref }}\n"
                  "          token: ${{ secrets['DOCKERHUB_TOKEN'] }}\n"), 1,
           ["слой 3", "actions/checkout"], "слой 3")
    expect("законный близнец: литерал 'secrets…' в шаге выкачки",
           mutate("          ref: ${{ steps.subject.outputs.ref }}\n",
                  "          ref: ${{ steps.subject.outputs.ref }}\n"
                  "          token: ${{ format('{0}', 'secrets.DOCKERHUB_TOKEN') }}\n"), 0)
    sha_env = "          RUN_SHA: ${{ github.event.workflow_run.head_sha }}\n"
    expect("слой 3: весь контекст секретов функцией после решения",
           mutate(sha_env, sha_env + "          ALL: ${{ toJSON(secrets) }}\n"), 1,
           ["слой 3", "состояние под проверкой"], "слой 3")
    expect("законный близнец: toJSON(steps.secrets) после решения",
           mutate(sha_env, sha_env + "          ALL: ${{ toJSON(steps.secrets) }}\n"), 0)

    # ── место проверки ──
    expect("место проверки: секрет в env задания",
           mutate("    runs-on: ubuntu-latest\n",
                  "    runs-on: ubuntu-latest\n    env:\n      DH: ${{ secrets.DOCKERHUB_TOKEN }}\n"),
           1, ["место проверки", "задание: env", "слой 2", "слой 3"])
    expect("законный близнец: env задания без секретов",
           mutate("    runs-on: ubuntu-latest\n",
                  "    runs-on: ubuntu-latest\n    env:\n      DH: ${{ github.repository }}\n"), 0)
    expect("место проверки: секрет в env процесса",
           mutate("  FALLBACK_NS: local\n",
                  "  FALLBACK_NS: local\n  DH: ${{ secrets.DOCKERHUB_TOKEN }}\n"),
           1, ["место проверки", "процесс: env", "слой 2", "слой 3"])
    expect("место проверки: шаг решения сам читает секреты",
           mutate("          THIS_REPO: ${{ github.repository }}\n",
                  "          THIS_REPO: ${{ github.repository }}\n"
                  "          DH: ${{ secrets.DOCKERHUB_TOKEN }}\n"),
           1, ["место проверки", "шаг решения №1 сам читает секреты"])
    head = "      - name: право на публикацию\n"
    tail = "      # ── СОСТОЯНИЕ ПОД ПРОВЕРКОЙ"
    co_end = "          ref: ${{ steps.subject.outputs.ref }}\n"
    moved = None
    if base.count(head) == 1 and base.count(tail) == 1 and base.count(co_end) == 1:
        i, j = base.index(head), base.index(tail)
        block, rest = base[i:j], base[:i] + base[j:]
        k = rest.index(co_end) + len(co_end)
        moved = rest[:k] + "\n" + block + rest[k:]
    expect("место проверки: решение после выкачки", moved, 1,
           ["место проверки", "actions/checkout", "(uses)"], "место проверки")
    expect("место проверки: шаг run без секретов до решения (окружение решения)",
           mutate(head, "      - name: отметка\n        run: echo BASH_ENV=/tmp/x >> \"$GITHUB_ENV\"\n"
                  + head), 1,
           ["место проверки", "«отметка» (run)"], "место проверки")
    expect("место проверки: секреты задания ключом вызова",
           {**real, ".github/workflows/reuse.yml":
            "on:\n  workflow_run:\n    workflows: [x]\n    types: [completed]\n"
            "jobs:\n  j:\n    uses: ./.github/workflows/x.yml\n    secrets: inherit\n"},
           1, ["reuse.yml", "задание: ключ secrets", "нет шага решения"])

    # ── собственные события процесса: перечень, ссылка собственного push ──
    own_if = "      github.event_name == 'push' ||\n      github.event_name == 'workflow_dispatch' ||\n"
    expect("слой 1: собственные события — «всё, кроме workflow_run» вместо перечня",
           mutate(own_if, "      github.event_name != 'workflow_run' ||\n"), 1,
           ["слой 1", "собственный pull_request, голова этого репозитория",
            "собственный pull_request_target"], "слой 1")
    expect("слой 1: собственные события — запрет одного вместо перечня",
           mutate("      github.event_name == 'push' ||\n",
                  "      github.event_name != 'pull_request' ||\n"), 1,
           ["слой 1", "собственный pull_request_target", "собственный schedule"], "слой 1")
    expect("положительный контроль: ручной запуск снят из условия задания",
           mutate("      github.event_name == 'workflow_dispatch' ||\n", ""), 1,
           ["законной публикации НЕТ", "собственный workflow_dispatch", "условие задания ложно"])
    any_event = "              why=\"событие '${EVENT_NAME:-<пусто>}' вне перечня"
    expect("слой 2: решение — «да» любому собственному событию вне перечня",
           mutate(any_event, "              echo \"publish=true\" >> \"$GITHUB_OUTPUT\"; exit 0\n"
                  + any_event), 1,
           ["слой 2", "собственный pull_request, голова этого репозитория",
            "собственный pull_request_target", "собственный issue_comment"], "слой 2")
    expect("слой 2: решение — запрос в перечне разрешённых",
           mutate("            workflow_dispatch)\n",
                  "            workflow_dispatch|pull_request|pull_request_target)\n"), 1,
           ["слой 2", "собственный pull_request, голова чужого репозитория",
            "собственный pull_request_target"], "слой 2")
    expect("положительный контроль: ручной запуск снят из перечня решения",
           mutate("            workflow_dispatch)\n", "            workflow_dispatch_gone)\n"), 1,
           ["законной публикации НЕТ", "собственный workflow_dispatch"])
    expect("слой 2: правило собственного push расширено до любой ветки",
           mutate("^refs/heads/main$", "^refs/heads/.*$"), 1,
           ["слой 2", "собственный push, ветка вне правила feature/x"], "слой 2")
    expect("слой 2: правило собственного push без якоря конца",
           mutate("^refs/heads/main$", "^refs/heads/main"), 1,
           ["слой 2", "ветка вне правила mainx"], "слой 2")
    expect("слой 2: правило собственного push без якоря начала",
           mutate("^refs/heads/main$", "refs/heads/main$"), 1,
           ["слой 2", "ветка вне правила x/refs/heads/main"], "слой 2")
    expect("слой 2: правило собственного push снова пускает снятую форму KAC-*",
           mutate("^refs/heads/main$ ]] ||\n",
                  "^refs/heads/main$ ]] ||\n"
                  "                 [[ \"${OWN_REF:-}\" =~ ^refs/heads/KAC-[^/]*$ ]] ||\n"), 1,
           ["слой 2", "ветка вне правила KAC-1"], "слой 2")
    expect("слой 2: в правило собственного push вошла ветка с вложенным путём",
           mutate("^refs/heads/main$ ]] ||\n",
                  "^refs/heads/main$ ]] ||\n"
                  "                 [[ \"${OWN_REF:-}\" =~ ^refs/heads/KAC-[0-9]+/.*$ ]] ||\n"), 1,
           ["слой 2", "ветка вне правила KAC-1/x"], "слой 2")
    expect("слой 2: правило метки без якоря конца",
           mutate("[0-9]+\\.[0-9]+\\.[0-9]+$ ]]", "[0-9]+\\.[0-9]+\\.[0-9]+ ]]"), 1,
           ["слой 2", "метка вне правила v1.2.3-rc"], "слой 2")
    expect("слой 2: правило метки — точка не экранирована",
           mutate("v[0-9]+\\.[0-9]+\\.[0-9]+$", "v[0-9]+.[0-9]+.[0-9]+$"), 1,
           ["слой 2", "метка вне правила v1x2x3"], "слой 2")
    expect("слой 2: правило метки — третья часть версии необязательна",
           mutate("v[0-9]+\\.[0-9]+\\.[0-9]+$", "v[0-9]+\\.[0-9]+(\\.[0-9]+)?$"), 1,
           ["слой 2", "метка вне правила v1.2"], "слой 2")
    expect("слой 2: ссылка собственного push не из контекста прогона",
           mutate("          OWN_REF: ${{ github.ref }}\n", "          OWN_REF: refs/heads/main\n"), 1,
           ["слой 2", "собственный push, ветка вне правила feature/x"], "слой 2")
    expect("положительный контроль: метки версий сняты из правила собственного push",
           mutate("[[ \"${OWN_REF:-}\" =~ ^refs/tags/v", "[[ \"${OWN_REF:-}\" =~ ^refs/tags/GONE"), 1,
           ["законной публикации НЕТ", "собственный push метки v1.2.3"])
    expect("законный близнец: фильтр push расширен до любой ветки — решение судит ссылку само",
           mutate('    branches:\n      - main\n    tags:\n',
                  '    branches:\n      - main\n      - "**"\n    tags:\n'), 0)

    # окружение решения — процесс, задание, шаг, как у площадки
    this_repo = "          THIS_REPO: ${{ github.repository }}\n"
    expect("законный близнец: репозиторий сравнения из env задания",
           mutate("    runs-on: ubuntu-latest\n",
                  "    runs-on: ubuntu-latest\n    env:\n      THIS_REPO: ${{ github.repository }}\n",
                  mutate(this_repo, "")), 0)
    expect("слой 2: репозиторий сравнения из env процесса — чужой",
           mutate("  FALLBACK_NS: local\n", f"  FALLBACK_NS: local\n  THIS_REPO: {FOREIGN_REPO}\n",
                  mutate(this_repo, "")), 1,
           ["слой 2", "push, голова чужого репозитория"])

    # ── триггеры: событие вне перечня в `on:` ──
    expect("триггеры: процесс поднимается запросом",
           mutate("  workflow_dispatch:\n", "  workflow_dispatch:\n  pull_request:\n"), 1,
           ["триггеры", "'pull_request'"], "триггеры")
    expect("триггеры: процесс поднимается запросом с правами базы",
           mutate("  workflow_dispatch:\n", "  workflow_dispatch:\n  pull_request_target:\n"
                  "    types: [opened]\n"), 1,
           ["триггеры", "'pull_request_target'"], "триггеры")
    expect("законный близнец: разрешённый триггер с иным фильтром",
           mutate("  workflow_dispatch:\n", "  workflow_dispatch:\n    inputs: {}\n"), 0)

    # ── параллельность: чужой прогон не делит группу с законным ──
    grp = "  group: docker-build-${{ github.event_name }}-${{ github.event.workflow_run.event }}-${{ github.event.workflow_run.head_repository.full_name }}-${{ github.event.workflow_run.head_branch || github.ref }}\n"
    expect("параллельность: прежний ключ — одна ветка",
           mutate(grp, "  group: docker-build-${{ github.event.workflow_run.head_branch || github.ref }}\n"),
           1, ["параллельность", "pull_request, голова чужого репозитория, ветка main"],
           "параллельность")
    expect("параллельность: из ключа снято событие исходного прогона",
           mutate("-${{ github.event.workflow_run.event }}-", "-"), 1,
           ["параллельность", "pull_request, голова этого репозитория, ветка main"],
           "параллельность")
    expect("параллельность: из ключа снят репозиторий головы",
           mutate("-${{ github.event.workflow_run.head_repository.full_name }}-", "-"), 1,
           ["параллельность", "push, голова чужого репозитория"], "параллельность")
    expect("параллельность: репозиторий головы с подстановкой репозитория процесса",
           mutate("-${{ github.event.workflow_run.head_repository.full_name }}-",
                  "-${{ github.event.workflow_run.head_repository.full_name || github.repository }}-"), 1,
           ["параллельность", "голова без репозитория"], "параллельность")
    expect("параллельность: из ключа снято событие этого прогона",
           mutate("docker-build-${{ github.event_name }}-", "docker-build-"), 1,
           ["параллельность", "собственный pull_request_target"], "параллельность")
    expect("параллельность: группа задания — постоянная",
           mutate("    runs-on: ubuntu-latest\n",
                  "    runs-on: ubuntu-latest\n    concurrency: publish\n"), 1,
           ["параллельность", "группа задания"], "параллельность")
    expect("законный близнец: ключ с добавленным именем процесса",
           mutate(grp, grp[:-1] + "-${{ github.workflow }}\n"), 0)
    expect("законный близнец: группы параллелизма нет",
           mutate("concurrency:\n" + grp + "  cancel-in-progress: true\n", ""), 0)

    # ── положительный контроль и предмет ──
    expect("положительный контроль: задание не идёт никогда",
           mutate("    if: >-\n      github.event_name == 'push' ||",
                  "    if: >-\n      false && (github.event_name == 'push' ||",
                  mutate("github.event.workflow_run.conclusion != 'skipped')\n",
                         "github.event.workflow_run.conclusion != 'skipped'))\n")),
           1, ["законной публикации НЕТ", "собственный push в main"])
    expect("положительный контроль: решение отказывает всем",
           mutate('echo "право: push в ветку', 'exit 1; echo "право: push в ветку'), 1,
           ["законной публикации НЕТ", "push в main этого репозитория"])
    expect("нет шага решения",
           mutate("        id: publish_right\n", "        id: publish_right_gone\n"), 1,
           ["нет шага решения"])
    expect("непонятое условие — не измерено, а не зелёное",
           mutate("github.event_name == 'workflow_dispatch' ||", "contains(github.ref, 'x') ||"), 2,
           ["НЕ ИЗМЕРЕНО"])
    # законный близнец: workflow_run без секретов — молчание и учёт в переписи
    twin = ("on:\n  workflow_run:\n    workflows: [x]\n    types: [completed]\n"
            "jobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n")
    expect("законный близнец: workflow_run без секретов", {**real, ".github/workflows/twin.yml": twin}, 0)
    expect("нет предмета: процессов workflow_run с секретами ноль",
           {".github/workflows/twin.yml": twin}, 2, ["судить нечего"])
    print(f"самопроверка: подпроб {probes}, провалов {fails}")
    return 1 if fails else 0


def main() -> int:
    root = Path(subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True,
                               text=True, check=True).stdout.strip())
    if "--self-test" in sys.argv:
        return self_test(root)
    code, f, c = verdict(tree_workflows(root))
    if c:
        print(_census_line(c))
    for line in f:
        print(("::error::" if code == 1 else "") + line)
    print({0: "находок 0", 1: f"находок {len(f)}", 2: "ОТКАЗ: не измерено"}[code])
    return code


if __name__ == "__main__":
    sys.exit(main())
