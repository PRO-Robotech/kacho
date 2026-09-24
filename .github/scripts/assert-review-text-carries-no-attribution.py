#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Гейт: заголовок и тело запроса на слияние не несут атрибуции.

ПРЕДМЕТ (kacho#2807)
--------------------
Правило корпуса `gi-no-attribution-trailers`: трейлер `Co-Authored-By` и прочая
атрибуция в текст изменения не идут. Держателя у правила не было: шагов,
судящих заголовок или тело запроса, в дереве ноль (`grep -rn
'pull_request.body\\|pull_request.title' .github/` пуст, 2026-09-22).

Текст запроса — не только страница запроса. Коммит слияния площадка собирает из
него: `gh api repos/PRO-Robotech/kacho` отвечает `merge_commit_title: PR_TITLE`,
`merge_commit_message: PR_BODY` (2026-09-24). Строка `Co-Authored-By: …` в
последнем абзаце тела становится трейлером коммита слияния, и снять её оттуда
можно только переписью истории.

ФОРМЫ АТРИБУЦИИ — ЗАКРЫТЫЙ ПЕРЕЧЕНЬ ATTRIBUTION_FORMS
-----------------------------------------------------
Судится СТРОКА: заголовок и каждая строка тела. С начала строки снимается
разметка — всё, что не буква, не цифра и не обратная кавычка, и подчёркивание
(`> `, `- `, `**`, `_`, значок перед текстом, пробелы), — и остаток
сравнивается с формами без регистра: имя трейлера git регистра не различает.
  * трейлер `Co-Authored-By:` — с любым именем после двоеточия;
  * трейлер `Claude-Session:`;
  * строка `Generated with Claude Code`, в том числе ссылкой `[Claude Code](…)`;
  * ссылка на сессию `https://claude.ai/code/session_…`.

ЗАКОННЫЙ БЛИЗНЕЦ — упоминание формы, а не сама форма: имя внутри прозы
(«трейлер Co-Authored-By не ставится») и имя в обратных кавычках в начале
строки. Такие строки гейт пропускает: объяснение запрета обязано оставаться
выразимым в тексте запроса.

ЧЕГО ЭТОТ ГЕЙТ НЕ СУДИТ — СКАЗАНО ПРЯМО
---------------------------------------
  * сообщения коммитов запроса: гейт читает полезную нагрузку события, а не
    историю ветки;
  * форму не в начале строки (после прозы), форму, разбитую невидимыми знаками
    или записанную буквами другого письма;
  * комментарии к запросу и тексты ревью;
  * блок кода — строка внутри ``` судится так же, как вне его: в коммит слияния
    тело идёт без разметки, и трейлер внутри блока остаётся трейлером.

ТРИ ИСХОДА
----------
  0 — полезная нагрузка прочитана, находок ноль, и перепись названа;
  1 — находки: каждая называет место (заголовок или номер строки тела) и форму;
  2 — ИЗМЕРЕНИЕ НЕ СДЕЛАНО: путь полезной нагрузки не задан, файл не прочитан
      или не JSON, в нём нет объекта `pull_request`, заголовок не строка или
      пуст, тело не строка и не `null`. Это НЕ «находок нет».

У каждого отказа есть ИМЯ предпосылки — первый аргумент `Unmeasured`, — и
самопроверка сверяет имя, а не факт отказа; перечень имён она выводит разбором
этого исходника, и предпосылка без подпробы — провал самопроверки.

Запуск:
  python3 .github/scripts/assert-review-text-carries-no-attribution.py --self-test
  python3 .github/scripts/assert-review-text-carries-no-attribution.py
  python3 .github/scripts/assert-review-text-carries-no-attribution.py --event <полезная нагрузка>.json
Без `--event` путь берётся из GITHUB_EVENT_PATH, который ставит ранер.
"""
from __future__ import annotations

import argparse
import ast
import json
import os
import re
import sys
import tempfile
import traceback
from dataclasses import dataclass
from pathlib import Path

# ATTRIBUTION_FORMS — имя формы → образец начала строки после снятой разметки
# (шапка, «ФОРМЫ АТРИБУЦИИ»). Перечень закрытый: форма вне его не судится.
ATTRIBUTION_FORMS: dict[str, re.Pattern[str]] = {
    "трейлер `Co-Authored-By`": re.compile(r"co-authored-by[ \t]*:", re.I),
    "трейлер `Claude-Session`": re.compile(r"claude-session[ \t]*:", re.I),
    "строка «Generated with Claude Code»": re.compile(r"generated[ \t]+with[ \t]+\[?[ \t]*claude[ \t]+code\b", re.I),
    "ссылка на сессию `claude.ai/code/session_`": re.compile(r"https?://claude\.ai/code/session_", re.I),
}

# MARKUP_PREFIX — разметка в начале строки: всё, что не буква, не цифра и не
# обратная кавычка, и подчёркивание.
MARKUP_PREFIX = re.compile(r"(?:[^\w`]|_)*")


class Unmeasured(Exception):
    """Измерение не сделано. Отличается от «находок нет»; `premise` — имя
    отказавшей предпосылки строковой константой (шапка, «ТРИ ИСХОДА»)."""

    def __init__(self, premise: str, text: str) -> None:
        super().__init__(text)
        self.premise = premise


@dataclass(frozen=True)
class Finding:
    place: str
    form: str
    line: str

    def __str__(self) -> str:
        return f"{self.place}: {self.form} — {self.line.strip()!r}"


@dataclass(frozen=True)
class Review:
    number: object
    action: object
    title: str
    body: str
    body_null: bool

    def census(self) -> str:
        lines = len(self.body.splitlines())
        return (f"запрос #{self.number} (событие {self.action}) · строк заголовка {len(self.title.splitlines())} · "
                f"строк тела {lines}{' (тело null)' if self.body_null else ''} · форм атрибуции "
                f"{len(ATTRIBUTION_FORMS)}")


def form_of(line: str) -> str:
    """Имя формы атрибуции, которой начинается строка, или "" (шапка)."""
    head = line[MARKUP_PREFIX.match(line).end():]
    for name, rx in ATTRIBUTION_FORMS.items():
        if rx.match(head):
            return name
    return ""


def judge(title: str, body: str) -> list[Finding]:
    out: list[Finding] = []
    for place, text in (("заголовок", title), ("тело", body)):
        for n, line in enumerate(text.splitlines(), start=1):
            form = form_of(line)
            if form:
                out.append(Finding(place if place == "заголовок" else f"тело, строка {n}", form, line))
    return out


def read_review(path: str | None) -> Review:
    if not path:
        raise Unmeasured("event-path-missing", "путь полезной нагрузки события не задан: нет ни `--event`, ни "
                         "GITHUB_EVENT_PATH — читать нечего")
    try:
        raw = Path(path).read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as exc:
        raise Unmeasured("event-unreadable", f"полезная нагрузка события {path} не прочитана: {exc}") from exc
    try:
        payload = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise Unmeasured("event-not-json", f"полезная нагрузка события {path} не JSON: {exc}") from exc
    pr = payload.get("pull_request") if isinstance(payload, dict) else None
    if not isinstance(pr, dict):
        raise Unmeasured("event-no-review", f"в полезной нагрузке {path} нет объекта `pull_request`: процесс запущен "
                         "не событием запроса, и заголовка с телом у него нет")
    title, body = pr.get("title"), pr.get("body")
    if not isinstance(title, str) or not title.strip():
        raise Unmeasured("title-not-string", f"заголовок запроса не строка или пуст ({type(title).__name__}): "
                         "площадка такого не присылает, и разбор не знает, что судить")
    if body is not None and not isinstance(body, str):
        raise Unmeasured("body-not-string", f"тело запроса не строка и не null ({type(body).__name__}): разбор не "
                         "знает, что судить")
    return Review(pr.get("number"), payload.get("action"), title, body or "", body is None)


def run_gate(path: str | None, out) -> int:  # noqa: ANN001 - поток вывода
    try:
        review = read_review(path)
    except Unmeasured as exc:
        print(f"ИЗМЕРЕНИЕ НЕ СДЕЛАНО ({exc.premise}): {exc}", file=out)
        return 2
    findings = judge(review.title, review.body)
    print(f"ОБЪЁМ ОСМОТРЕННОГО: {review.census()}", file=out)
    if findings:
        print(f"НАХОДОК {len(findings)} — атрибуция в тексте запроса запрещена правилом gi-no-attribution-trailers, "
              "а тело идёт в сообщение коммита слияния:", file=out)
        for f in findings:
            print(f"  {f}", file=out)
        return 1
    print("находок 0", file=out)
    return 0


# ── САМОПРОВЕРКА ─────────────────────────────────────────────────────────────
#
# Вход — полезная нагрузка события той формы, что присылает площадка, записанная
# во временный файл: гейт читает её тем же путём, что в конвейере. Каждой форме —
# красная подпроба с местом и именем формы; каждой — законный близнец, на котором
# гейт молчит. Исход сверяется кодом `run_gate`, а не только списком находок.


class SelfTestFailure(Exception):
    pass


def _expect(cond: bool, what: str) -> None:
    if not cond:
        raise SelfTestFailure(what)


def premise_sites(source: str) -> dict[str, int]:
    """Имя предпосылки → строка её отказа, разбором исходника; отказы внутри
    самопроверки не в счёт. Имя не константой или повторённое — провал."""
    tree = ast.parse(source)
    probe = {id(n) for f in ast.walk(tree) if isinstance(f, ast.FunctionDef) and f.name == "_self_test"
             for n in ast.walk(f)}
    sites: dict[str, int] = {}
    for node in ast.walk(tree):
        if id(node) in probe or not (isinstance(node, ast.Call) and isinstance(node.func, ast.Name)
                                     and node.func.id == Unmeasured.__name__):
            continue
        arg = node.args[0] if node.args else None
        if not (isinstance(arg, ast.Constant) and isinstance(arg.value, str) and arg.value):
            raise SelfTestFailure(f"отказ в строке {node.lineno}: имя предпосылки не строковая константа")
        if arg.value in sites:
            raise SelfTestFailure(f"предпосылка {arg.value!r} названа дважды (строки {sites[arg.value]} и "
                                  f"{node.lineno})")
        sites[arg.value] = node.lineno
    return sites


CO, SESSION, GENERATED, LINK = ATTRIBUTION_FORMS


def self_test() -> int:
    with tempfile.TemporaryDirectory(prefix="review-text-") as d:
        return _self_test(Path(d))


def _self_test(tmp: Path) -> int:
    seq = iter(range(10**6))

    def event(title: object = "#2807 ci: запрос в ветку линии", body: object = "Что и почему.",
              drop_review: bool = False) -> str:
        pr = {"number": 2807, "title": title, "body": body}
        payload = {"action": "edited", "ref": "refs/heads/2807"} if drop_review else {"action": "edited",
                                                                                    "pull_request": pr}
        p = tmp / f"event-{next(seq)}.json"
        p.write_text(json.dumps(payload, ensure_ascii=False), encoding="utf-8")
        return str(p)

    class Out:
        def __init__(self) -> None:
            self.lines: list[str] = []

        def write(self, s: str) -> None:
            self.lines.append(s)

        def text(self) -> str:
            return "".join(self.lines)

    def gate(path: str | None) -> tuple[int, str]:
        out = Out()
        return run_gate(path, out), out.text()

    covered: set[str] = set()
    expected: set[str] = set()

    def refused(path: str | None, premise: str, *says: str) -> None:
        expected.add(premise)
        try:
            read_review(path)
        except Unmeasured as exc:
            _expect(exc.premise == premise, f"ждали отказ {premise!r}, отказала {exc.premise!r}: {exc}")
            for s in says:
                _expect(s in str(exc), f"отказ {premise!r} не называет {s!r}: {exc}")
            code, text = gate(path)
            _expect(code == 2 and f"({premise})" in text, f"исход отказа {premise!r} не 2: {code} {text}")
            covered.add(premise)
            return
        raise SelfTestFailure(f"вход предпосылки {premise!r} принят за измеримый: отказа нет")

    cases: list[tuple[str, object]] = []

    def case(name: str):  # noqa: ANN202
        def deco(fn):  # noqa: ANN001, ANN202
            cases.append((name, fn))
            return fn
        return deco

    # ── красное: каждая форма в теле, в своих написаниях ──
    body_red = [
        ("трейлер с именем инструмента", "Co-Authored-By: Claude <noreply@anthropic.com>", CO),
        ("трейлер с именем человека, строчными", "co-authored-by: A Human <a@example.org>", CO),
        ("пробел перед двоеточием", "Co-Authored-By : X <x@example.org>", CO),
        ("пункт списка", "- Co-Authored-By: X <x@example.org>", CO),
        ("цитата", "> Co-Authored-By: X <x@example.org>", CO),
        ("отступ", "    Co-Authored-By: X <x@example.org>", CO),
        ("полужирный", "**Co-Authored-By:** X <x@example.org>", CO),
        ("курсив подчёркиванием", "_Co-Authored-By: X <x@example.org>_", CO),
        ("трейлер сессии", "Claude-Session: https://claude.ai/code/session_01", SESSION),
        ("трейлер сессии прописными", "CLAUDE-SESSION: x", SESSION),
        ("строка со значком и ссылкой", "🤖 Generated with [Claude Code](https://claude.com/claude-code)", GENERATED),
        ("строка без значка", "Generated with Claude Code", GENERATED),
        ("строка с двойными пробелами, строчными", "generated  with  claude  code", GENERATED),
        ("голая ссылка на сессию", "https://claude.ai/code/session_016JWyL9", LINK),
        ("ссылка на сессию угловыми скобками", "<https://claude.ai/code/session_016JWyL9>", LINK),
    ]
    for label, line, form in body_red:
        @case(f"тело: {label}")
        def _(line=line, form=form):
            path = event(body=f"Что и почему.\r\n\r\n{line}\r\n")
            code, text = gate(path)
            _expect(code == 1 and "тело, строка 3" in text and form in text, f"{line!r}: {code} {text}")

    @case("тело: две формы — две находки, каждая со своей строкой")
    def _():
        found = judge("#2807 ci: x", "Что.\n\nCo-Authored-By: X <x@example.org>\nClaude-Session: y\n")
        _expect([(f.place, f.form) for f in found] == [("тело, строка 3", CO), ("тело, строка 4", SESSION)],
                str(found))

    # ── красное: заголовок ──
    for label, title, form in (("строка атрибуции", "🤖 Generated with Claude Code", GENERATED),
                               ("трейлер", "Co-Authored-By: X <x@example.org>", CO)):
        @case(f"заголовок: {label}")
        def _(title=title, form=form):
            code, text = gate(event(title=title))
            _expect(code == 1 and "заголовок:" in text and form in text, f"{title!r}: {code} {text}")

    # ── законные близнецы ──
    twins = [
        ("имя трейлера внутри прозы", "трейлер Co-Authored-By: не ставится — правило gi-no-attribution-trailers"),
        ("имя в обратных кавычках в начале строки", "`Co-Authored-By:` запрещён правилом"),
        ("строка в обратных кавычках", "`🤖 Generated with Claude Code` — запрещённая строка"),
        ("другой трейлер", "Signed-off-by: A <a@example.org>"),
        ("имя без `-by`", "Co-Authored: X"),
        ("слово без двоеточия", "Co-Authored-By X"),
        ("имя продукта внутри прозы", "Проверено клиентом Claude Code вручную."),
        ("похожее начало", "Generated with care"),
        ("ссылка не на сессию", "https://claude.ai/docs"),
        ("заголовок с номером задачи", "#2807 ci: Co-Authored-By в заголовке"),
    ]
    for label, line in twins:
        @case(f"близнец: {label}")
        def _(line=line):
            code, text = gate(event(body=f"Что и почему.\n\n{line}\n"))
            _expect(code == 0 and "находок 0" in text and "строк тела 3" in text, f"{line!r}: {code} {text}")

    @case("близнец: тело null и пустое — находок ноль, перепись это называет")
    def _():
        for body, says in ((None, "строк тела 0 (тело null)"), ("", "строк тела 0 · ")):
            code, text = gate(event(body=body))
            _expect(code == 0 and says in text and "форм атрибуции 4" in text, f"{body!r}: {code} {text}")

    # ── исход 2: у каждой предпосылки свой вход ──
    @case("исход 2: путь не задан")
    def _():
        refused(None, "event-path-missing", "GITHUB_EVENT_PATH")
        refused("", "event-path-missing", "не задан")

    @case("исход 2: файла нет")
    def _():
        refused(str(tmp / "нет-такого.json"), "event-unreadable", "нет-такого.json")

    @case("исход 2: не JSON")
    def _():
        p = tmp / "broken.json"
        p.write_text("{ обрезано", encoding="utf-8")
        refused(str(p), "event-not-json", "broken.json")

    @case("исход 2: событие не запроса; близнец — то же с запросом")
    def _():
        refused(event(drop_review=True), "event-no-review", "`pull_request`")
        p = tmp / "list.json"
        p.write_text("[]", encoding="utf-8")
        refused(str(p), "event-no-review", "нет объекта")
        _expect(gate(event())[0] == 0, "полезная нагрузка запроса не прочитана")

    @case("исход 2: заголовок не строка или пуст")
    def _():
        for title in (None, 7, "", "   "):
            refused(event(title=title), "title-not-string", "заголовок")

    @case("исход 2: тело не строка и не null")
    def _():
        for body in (["Co-Authored-By: X"], 7, {"a": 1}):
            refused(event(body=body), "body-not-string", "тело")

    @case("перечень форм: четыре имени, и у каждой есть красная подпроба")
    def _():
        _expect(len(ATTRIBUTION_FORMS) == 4, f"форм {len(ATTRIBUTION_FORMS)}: шапка называет четыре")
        seen = {form for _l, _t, form in body_red}
        _expect(seen == set(ATTRIBUTION_FORMS), f"формы без красной подпробы: {set(ATTRIBUTION_FORMS) - seen}")

    # ПОСЛЕДНЕЙ: сверяет то, что вызвали подпробы выше.
    census_line: list[str] = []

    @case("перечень предпосылок выведен из исходника, и у каждой — подпроба со сверенным отказом")
    def _():
        sites = premise_sites(Path(__file__).read_text(encoding="utf-8"))
        census_line.append(f"предпосылок отказа в исходнике {len(sites)} · сверено подпробами "
                           f"{len(covered & set(sites))}")
        _expect(sites, "в исходнике не найдено ни одного отказа — сверять не с чем")
        lost, stale = sorted(set(sites) - covered), sorted(expected - set(sites))
        _expect(not lost, "предпосылки без подпробы: " + ", ".join(f"{k} (строка {sites[k]})" for k in lost))
        _expect(not stale, f"подпробы ждут предпосылок, которых в исходнике нет: {stale}")

    passed, failed = 0, []
    for name, fn in cases:
        try:
            fn()
            passed += 1
        except SelfTestFailure as exc:
            failed.append(f"{name}: {exc}")
        except Exception:  # noqa: BLE001 - поломка пробы — тоже провал, с трассой
            failed.append(f"{name}: {traceback.format_exc()}")
    print(f"самопроверка: подпроб {len(cases)} · прошли {passed} · провалились {len(failed)}"
          + "".join(f" · {x}" for x in census_line))
    for f in failed:
        print(f"  ПРОВАЛ: {f}")
    if not cases:
        return 2
    return 0 if not failed else 1


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n", 1)[0])
    ap.add_argument("--self-test", action="store_true", help="доказательство подачей входа в обе стороны")
    ap.add_argument("--event", help="полезная нагрузка события (по умолчанию — GITHUB_EVENT_PATH)")
    args = ap.parse_args()
    if args.self_test:
        return self_test()
    return run_gate(args.event if args.event is not None else os.environ.get("GITHUB_EVENT_PATH"), sys.stdout)


if __name__ == "__main__":
    sys.exit(main())
