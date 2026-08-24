#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Полоса обмена в МАШИННОМ чеканщике обязана иметь вызывающего.

─────────────────────────────────────────────────────────────────────────────
ПРЕДМЕТ

`tests/authz-fixtures/mint_rs256.py` — библиотека посева: её единственные
потребители лежат в этом же дереве (посевы рядом импортируют её как `m`) плюс
её собственная командная строка. Значит достижимость здесь ВЫЧИСЛИМА, а не
предположительна: полоса обмена, до которой не дотягивается ни один корень,
вызвана быть не может ничем.

Мёртвой её делает не отсутствие вызывающих само по себе, а то, что предмета у
неё больше нет. Выдача ключа служебной учётки (#1120) и выпуск персонального
токена (#1121) перестали заводить зеркала клиента у прежнего издателя, поэтому
обмен там отвечает отказом опознания ПРИ ЛЮБОМ ВХОДЕ — режим, который не
работает ни при каком вводе.

Вредна при этом не сама функция, а ПРОЗА рядом с ней: шапка называла снятую
полосу доказанной сквозной. Следующий читатель принимает её за живую и чинит
обмен, которого не бывает.

─────────────────────────────────────────────────────────────────────────────
ЧТО ГЕЙТ УТВЕРЖДАЕТ, А ЧТО — НЕТ

Утверждает РОВНО ОДНО: у каждой полосы обмена в машинном чеканщике есть
вызывающий.

НЕ утверждает, что всякая функция модуля достижима. Граница проведена
намеренно и по предмету: недостижимая функция, которая обменом НЕ является
(тонкая обёртка, которую посев обошёл, позвав вложенное напрямую), — предмет
другой задачи, и молчание о ней здесь не означает, что она жива. Гейт, судящий
всякую функцию, потребовал бы ведомости исключений, а ведомость гниёт.

НЕ судит посев церемонии: его обмен у прежнего издателя — ДРУГОЙ вид выдачи
(интерактивный вход, `authorization_code` + PKCE), он живёт в своём файле, и
именно он остаётся предметом требования держать прежнего издателя принятым
(`assert-legacy-issuer-acceptance-has-a-subject.py`, задача #1123). Снятие
машинной полосы этого контура не касается.

─────────────────────────────────────────────────────────────────────────────
ПРИЗНАК — РАЗБОР, А НЕ ПОИСК ПО ТЕКСТУ

Полосой обмена считается функция, чья ИСПОЛНЯЕМАЯ часть строит форму запроса
выдачи (литерал ключа `grant_type`), либо функция, которая такую зовёт. Имя
полосы, названное в докстроке или комментарии, полосой не является: гейт,
считающий его, краснел бы на собственном объяснении.

Корни достижимости собираются ПО ПСЕВДОНИМУ модуля (`import mint_rs256 as m`
⇒ `m.<имя>`), а не по всякому совпавшему слову в соседнем файле: совпадение
имени сделало бы гейт слепым ровно там, где он нужен.

Использование:
    python3 deploy/scripts/assert-machine-minter-has-no-dead-exchange-lane.py --root .
    python3 deploy/scripts/assert-machine-minter-has-no-dead-exchange-lane.py --self-test

Исходы: 0 — полос без вызывающего нет; 1 — находка; 2 — предпосылка сломана.
"""
from __future__ import annotations

import argparse
import ast
import os
import sys
import tempfile

# Машинный чеканщик — предмет гейта.
MINTER = "tests/authz-fixtures/mint_rs256.py"

# Ключ формы выдачи. Его строит КАЖДАЯ полоса обмена и никто больше.
GRANT_FORM_KEY = "grant_type"

# Точка входа командной строки — второй законный корень достижимости.
CLI_ROOT = "main"


class PremiseBroken(Exception):
    """Предпосылка гейта не выполняется: осматривать нечего."""


def _docstring_ids(tree: ast.AST) -> set[int]:
    """Узлы-докстроки: их литералы из рассмотрения исключены."""
    out: set[int] = set()
    for node in ast.walk(tree):
        if not isinstance(node, (ast.Module, ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
            continue
        body = getattr(node, "body", None)
        if not body:
            continue
        first = body[0]
        if isinstance(first, ast.Expr) and isinstance(first.value, ast.Constant) \
                and isinstance(first.value.value, str):
            out.add(id(first.value))
    return out


def _parse(root: str, rel: str) -> ast.Module:
    path = os.path.join(root, rel)
    if not os.path.exists(path):
        raise PremiseBroken(f"нет файла {rel} — читать нечего")
    with open(path, encoding="utf-8") as fh:
        src = fh.read()
    try:
        return ast.parse(src, filename=rel)
    except SyntaxError as exc:
        raise PremiseBroken(f"{rel} не разбирается: {exc}") from exc


def _names_used(node: ast.AST) -> set[str]:
    """Имена, которые тело функции упоминает: вызовы и ссылки на функцию."""
    out: set[str] = set()
    for child in ast.walk(node):
        if isinstance(child, ast.Name):
            out.add(child.id)
        elif isinstance(child, ast.Attribute):
            out.add(child.attr)
    return out


def _module_functions(tree: ast.Module) -> dict[str, ast.AST]:
    return {n.name: n for n in tree.body
            if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef))}


def _builds_grant_form(node: ast.AST, docstrings: set[int]) -> bool:
    """Строит ли исполняемая часть форму выдачи."""
    for child in ast.walk(node):
        if isinstance(child, ast.Constant) and isinstance(child.value, str) \
                and id(child) not in docstrings and child.value == GRANT_FORM_KEY:
            return True
    return False


def exchange_lanes(funcs: dict[str, ast.AST], docstrings: set[int]) -> set[str]:
    """Полосы обмена: строящие форму выдачи + зовущие такую, транзитивно."""
    lanes = {name for name, node in funcs.items() if _builds_grant_form(node, docstrings)}
    changed = True
    while changed:
        changed = False
        for name, node in funcs.items():
            if name in lanes:
                continue
            if _names_used(node) & lanes:
                lanes.add(name)
                changed = True
    return lanes


def harness_roots(root: str, funcs: dict[str, ast.AST]) -> tuple[set[str], int]:
    """Имена чеканщика, до которых дотягивается оснастка, + число соседей."""
    roots: set[str] = {CLI_ROOT} if CLI_ROOT in funcs else set()
    minter_module = os.path.splitext(os.path.basename(MINTER))[0]
    siblings = 0
    directory = os.path.join(root, os.path.dirname(MINTER))
    if not os.path.isdir(directory):
        raise PremiseBroken(f"нет каталога оснастки {os.path.dirname(MINTER)} — "
                            "корни достижимости собрать не из чего")
    for entry in sorted(os.listdir(directory)):
        if not entry.endswith(".py") or entry == os.path.basename(MINTER):
            continue
        try:
            tree = _parse(root, os.path.join(os.path.dirname(MINTER), entry))
        except PremiseBroken:
            continue
        aliases: set[str] = set()
        imports_minter = False
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                for alias in node.names:
                    if alias.name == minter_module:
                        aliases.add(alias.asname or alias.name)
                        imports_minter = True
            elif isinstance(node, ast.ImportFrom) and node.module == minter_module:
                imports_minter = True
                for alias in node.names:
                    roots.add(alias.name)
        # Сосед считается импортёром при ОБЕИХ формах ввоза. Иначе дерево, где
        # чеканщик ввозят только поимённо, объявлялось бы «без импортёров», и
        # предпосылка ломалась бы там, где корни как раз собраны.
        if not imports_minter:
            continue
        siblings += 1
        for node in ast.walk(tree):
            if isinstance(node, ast.Attribute) and isinstance(node.value, ast.Name) \
                    and node.value.id in aliases:
                roots.add(node.attr)
    return roots, siblings


def reachable(funcs: dict[str, ast.AST], roots: set[str]) -> set[str]:
    seen: set[str] = set()
    stack = [r for r in roots if r in funcs]
    while stack:
        name = stack.pop()
        if name in seen:
            continue
        seen.add(name)
        for used in _names_used(funcs[name]):
            if used in funcs and used not in seen:
                stack.append(used)
    return seen


def evaluate(root: str) -> tuple[int, list[str], dict[str, int]]:
    tree = _parse(root, MINTER)
    docstrings = _docstring_ids(tree)
    funcs = _module_functions(tree)
    if not funcs:
        raise PremiseBroken(f"{MINTER} не объявляет ни одной функции — "
                            "«полос без вызывающего нет» неотличимо от «не прочитали»")
    lanes = exchange_lanes(funcs, docstrings)
    if not lanes:
        raise PremiseBroken(
            f"в {MINTER} не нашлось НИ ОДНОЙ полосы обмена (признак — литерал "
            f"{GRANT_FORM_KEY!r} в исполняемой части). Либо форма выдачи переехала, "
            "либо предикат ослеп; тихое зелёное здесь означало бы «не искали»")
    roots, siblings = harness_roots(root, funcs)
    if siblings == 0:
        raise PremiseBroken(
            f"ни один сосед по {os.path.dirname(MINTER)} не импортирует чеканщик — "
            "корни достижимости пусты, и всякая полоса выглядела бы мёртвой")
    alive = reachable(funcs, roots)
    dead = sorted(lanes - alive)
    census = {"funcs": len(funcs), "lanes": len(lanes), "roots": len(roots),
              "siblings": siblings, "alive": len(alive)}
    return (1 if dead else 0), dead, census


# ── самопроверка ────────────────────────────────────────────────────────────
_LANE_LEGACY = '''
def lane_at_provider(url, assertion):
    form = {"grant_type": "client_credentials", "client_assertion": assertion}
    return _post(url, form)


def sa_lane_at_provider(base, subject):
    return lane_at_provider(base, subject)
'''

_LANE_PLATFORM = '''
def exchange_at_platform(url, assertion):
    form = {"grant_type": "client_credentials", "client_assertion": assertion}
    return _post(url, form)


def sa_platform_token(base, subject):
    return exchange_at_platform(base, subject)
'''

_PROSE_ONLY = '''
def mentions_a_lane_in_prose():
    """Прежняя полоса звалась exchange и строила форму с grant_type.

    Ключ grant_type назван здесь ТЕКСТОМ и формы не строит.
    """
    # grant_type в комментарии — тоже не полоса
    return None
'''

_NON_LANE_ORPHAN = '''
def ensure_some_cert():
    """Тонкая обёртка, которую посев обошёл, позвав вложенное напрямую."""
    return _helper()
'''

_CLI = '''
def main():
    return sa_platform_token("base", "subject")
'''


def _plant(root: str, *, minter_body: str, sibling: str | None,
           drop_minter: bool = False, broken_minter: bool = False) -> None:
    directory = os.path.join(root, os.path.dirname(MINTER))
    os.makedirs(directory, exist_ok=True)
    if not drop_minter:
        src = "def _post(url, form):\n    return url\n\n\ndef _helper():\n    return 1\n" + minter_body
        if broken_minter:
            src = "def broken(:\n"
        with open(os.path.join(root, MINTER), "w", encoding="utf-8") as fh:
            fh.write(src)
    if sibling is not None:
        with open(os.path.join(directory, "prodseed_probe.py"), "w", encoding="utf-8") as fh:
            fh.write(sibling)


_SIBLING_PLATFORM = "import mint_rs256 as m\n\nm.sa_platform_token('b', 's')\n"
_SIBLING_BOTH = "import mint_rs256 as m\n\nm.sa_platform_token('b', 's')\nm.sa_lane_at_provider('b', 's')\n"
_SIBLING_FROM = ("from mint_rs256 import sa_lane_at_provider, sa_platform_token\n\n"
                 "sa_lane_at_provider('b', 's')\n")
_SIBLING_LOOKALIKE = ("import mint_rs256 as m\n\nother = object()\n"
                      "m.sa_platform_token('b', 's')\nother.sa_lane_at_provider('b', 's')\n")


def self_test() -> int:
    cases: list[tuple[str, dict, int]] = [
        # ── ось «полоса без вызывающего» — то, ради чего гейт заведён ────────
        ("полоса прежнего издателя без вызывающего ⇒ НАХОДКА",
         {"minter_body": _LANE_LEGACY + _LANE_PLATFORM + _CLI,
          "sibling": _SIBLING_PLATFORM}, 1),
        # ── ось «законный близнец»: та же форма, но вызывающий есть ─────────
        ("та же полоса, но её зовёт посев ⇒ ЗЕЛЕНО (судим достижимость, не имя)",
         {"minter_body": _LANE_LEGACY + _LANE_PLATFORM + _CLI,
          "sibling": _SIBLING_BOTH}, 0),
        ("её зовут импортом по имени (from … import) ⇒ ЗЕЛЕНО",
         {"minter_body": _LANE_LEGACY + _LANE_PLATFORM + _CLI,
          "sibling": _SIBLING_FROM}, 0),
        ("полоса достижима ТОЛЬКО из командной строки ⇒ ЗЕЛЕНО (второй корень)",
         {"minter_body": _LANE_PLATFORM + _CLI, "sibling": _SIBLING_PLATFORM}, 0),
        # ── ось «граница псевдонима»: чужой объект корнем не делает ─────────
        ("одноимённый вызов у ЧУЖОГО объекта ⇒ НАХОДКА (корень — псевдоним модуля)",
         {"minter_body": _LANE_LEGACY + _LANE_PLATFORM + _CLI,
          "sibling": _SIBLING_LOOKALIKE}, 1),
        # ── ось «признак читает разбор, а не текст» ─────────────────────────
        ("полоса названа только прозой ⇒ ЗЕЛЕНО (докстрока полосой не является)",
         {"minter_body": _LANE_PLATFORM + _PROSE_ONLY + _CLI,
          "sibling": _SIBLING_PLATFORM}, 0),
        # ── ось «граница предмета»: не всякая функция, а полоса обмена ──────
        ("недостижимая функция, НЕ являющаяся обменом ⇒ ЗЕЛЕНО (предмет другой задачи)",
         {"minter_body": _LANE_PLATFORM + _NON_LANE_ORPHAN + _CLI,
          "sibling": _SIBLING_PLATFORM}, 0),
        # ── ось «предпосылка» ───────────────────────────────────────────────
        ("полос обмена ноль ⇒ ПРЕДПОСЫЛКА СЛОМАНА (не тихое зелёное)",
         {"minter_body": _NON_LANE_ORPHAN + _CLI, "sibling": _SIBLING_PLATFORM}, 2),
        ("ни один сосед не импортирует чеканщик ⇒ ПРЕДПОСЫЛКА СЛОМАНА",
         {"minter_body": _LANE_PLATFORM + _CLI, "sibling": None}, 2),
        ("чеканщика нет по объявленному пути ⇒ ПРЕДПОСЫЛКА СЛОМАНА",
         {"minter_body": _LANE_PLATFORM, "sibling": _SIBLING_PLATFORM,
          "drop_minter": True}, 2),
        ("чеканщик не разбирается ⇒ ПРЕДПОСЫЛКА СЛОМАНА",
         {"minter_body": _LANE_PLATFORM, "sibling": _SIBLING_PLATFORM,
          "broken_minter": True}, 2),
    ]
    failures = 0
    for title, kwargs, want in cases:
        with tempfile.TemporaryDirectory() as root:
            _plant(root, **kwargs)
            try:
                code, _, _ = evaluate(root)
            except PremiseBroken:
                code = 2
        mark = "OK " if code == want else "ОТКАЗ"
        if code != want:
            failures += 1
        print(f"  [{mark}] {title} — ожидали {want}, получили {code}")
    print(f"самопроверка: утверждений {len(cases)}, отказов {failures}")
    return 1 if failures else 0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--root", default=".", help="корень репозитория")
    ap.add_argument("--self-test", action="store_true",
                    help="доказать, что гейт умеет краснеть и молчать")
    args = ap.parse_args()
    if args.self_test:
        return self_test()
    try:
        code, dead, census = evaluate(args.root)
    except PremiseBroken as exc:
        print(f"ПРЕДПОСЫЛКА СЛОМАНА: {exc}", file=sys.stderr)
        return 2
    print(f"перепись: функций {census['funcs']}, полос обмена {census['lanes']}, "
          f"соседей-импортёров {census['siblings']}, корней {census['roots']}, "
          f"достижимо функций {census['alive']}")
    if code:
        for name in dead:
            print(f"НАХОДКА: полоса обмена {name!r} в {MINTER} недостижима ни из "
                  "командной строки, ни из посевов. Предмета у неё нет: выдача "
                  "больше не заводит зеркала клиента у прежнего издателя (#1120, "
                  "#1121), поэтому обмен отвечает отказом опознания при любом входе. "
                  "Снимите полосу ВМЕСТЕ с прозой, которая называет её живой — "
                  "иначе следующий читатель будет чинить обмен, которого не бывает",
                  file=sys.stderr)
    return code


if __name__ == "__main__":
    sys.exit(main())
