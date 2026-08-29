#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Перепись впрыскиваемых помощников генератора newman против их вызывающих.

Читает разобранное дерево (`ast`), а не текст: имя помощника встречается в прозе
шапок, в комментариях и в отчётах набора, и предикат по подстроке считал бы
объяснение вызовом.

Вход: путь к генератору набора, каталоги СВОЕГО набора и — после разделителя
`--cross` — каталоги МЕЖНАБОРНЫХ потребителей.
Выход: JSON — по каждому впрыскиваемому имени число ВЫЗОВОВ (`ast.Call`) порознь
в своём наборе и у межнаборных потребителей. Ссылка значением
(`auth_pre=_auth_pre_script`) вызовом не считается: она доставляет имя, но не
потребляет его.

ПОЧЕМУ ДВЕ ПОЛОСЫ, А НЕ ОДНА СУММА. Помощник бывает не нужен ни одному кейсу
СВОЕГО набора и при этом иметь настоящего вызывающего в чужом: проба стойкости
сериализатора живёт в одном наборе и обходит генераторы ВСЕХ. Перепись, знавшая
только свою полосу, назвала такой помощник мёртвым, его сняли — и проба упала.
Форма вызывающего, которой распознаватель не знает, уводит предмет не в находку
и не в молчание, а в невидимость.

ЦЕНА ЭТОЙ ПОЛОСЫ НАЗВАНА: межнаборный вызов идёт через переменную
(`g.assert_op_error_oneof(...)`), поэтому по разбору нельзя сказать, ЧЕЙ генератор
за ней стоит. Полоса засчитывается КАЖДОМУ набору, объявившему это имя, — то есть
ошибается в сторону молчания и ровно для тех имён, что стоят в межнаборной
ведомости. Отдельное число в переписи делает эту слепоту видимой.
"""
import ast
import json
import sys
from pathlib import Path


def _injected_names(tree: ast.Module) -> set:
    """Имена, попадающие в пространство имён модуля кейсов через таблицу впрыска."""
    out = set()
    for node in tree.body:
        if not isinstance(node, ast.Assign):
            continue
        if not any(getattr(t, "id", "") == "_INJECTED" for t in node.targets):
            continue
        for v in ast.walk(node.value):
            if isinstance(v, ast.Name):
                out.add(v.id)
    return out


def main(argv) -> int:
    gen = Path(argv[1])
    tree = ast.parse(gen.read_text())
    declared = {n.name for n in tree.body
                if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef))}
    subject = sorted(declared & _injected_names(tree))

    rest = argv[2:]
    own_dirs = rest[:rest.index("--cross")] if "--cross" in rest else rest
    cross_dirs = rest[rest.index("--cross") + 1:] if "--cross" in rest else []

    calls = {name: 0 for name in subject}
    cross = {name: 0 for name in subject}
    scanned = 0
    cross_scanned = 0

    def _count(dirs, sink):
        seen = 0
        for raw in dirs:
            for f in sorted(Path(raw).glob("*.py")):
                seen += 1
                for node in ast.walk(ast.parse(f.read_text())):
                    if not isinstance(node, ast.Call):
                        continue
                    nm = getattr(node.func, "id", None) or getattr(node.func, "attr", None)
                    if nm in sink:
                        sink[nm] += 1
        return seen

    scanned = _count(own_dirs, calls)
    cross_scanned = _count(cross_dirs, cross)
    sys.stdout.write(json.dumps({
        "scanned": scanned, "cross_scanned": cross_scanned,
        "calls": calls, "cross": cross,
    }))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
