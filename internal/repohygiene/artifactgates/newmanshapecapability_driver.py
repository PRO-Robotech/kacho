#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Перепись ВОЗМОЖНОСТЕЙ помощников newman, различающих ФОРМУ аргумента,
против вызывающих каждой формы.

Читает РАЗОБРАННЫЙ Python (`ast`), а не текст. Причина измерена, а не
предположена: имя `retry_until_present` встречается в дереве и вызовом, и прозой
— в шапках и комментариях четырёх модулей кейсов, — и предикат по подстроке
считал бы объяснение вызовом. Отдельно: корпус ДВУЯЗЫЧЕН (объяснения по-русски,
имена по-английски), и поиск по слову на одном языке недобирает молча; разбор
судит узел, поэтому язык прозы на него не влияет вовсе.

ПРЕДМЕТ. Параметр, по ФОРМЕ которого функция ветвится, объявляет ДВЕ принимаемые
формы. Каждая форма — самостоятельная возможность: её пишут под конкретный
дефект, и если её никто не зовёт, она не проверяется ничем, стареет вместе с
деревом и при первом же использовании работает не так, как ожидал автор.

ДИСКРИМИНАТОР — ЧТО ДЕЛАЕТ ВТОРАЯ ВЕТВЬ, а не то, что стоит в условии.
Проверка типа бывает двух родов, и смешивать их нельзя:

    возможность  обе ветви ПРОИЗВОДЯТ значение
                 `want = [x] if isinstance(x, str) else list(x)`
    страж        одна ветвь ОТВЕРГАЕТ вход
                 `if not isinstance(x, str) or x == "": raise ValueError(...)`

У стража второй принимаемой формы НЕТ — отказ формой не является, и требовать
для него вызывающего значило бы требовать вызова, который обязан упасть.

ФОРМА, КОТОРОЙ РАЗБОР НЕ ЗНАЕТ, — ОТКАЗ, А НЕ МОЛЧАНИЕ. Проверка типа вне
`if`/`if-else` (в присваивании, в `assert`, в выражении объемлющего вызова)
уходит в `unknown_forms`, и гейт на ней падает: невидимость хуже находки.

ИМЕНА ВЫЗЫВАЮЩИХ ВЫВОДЯТСЯ, А НЕ ВЫПИСЫВАЮТСЯ. Помощник доезжает до модуля
кейсов через цепочку: объявление в общем слое -> импорт в генератор набора ->
`functools.partial` с привязкой окна -> запись в таблицу впрыска под ИМЕНЕМ.
Вызов в модуле кейсов идёт по имени из таблицы, а внутри своего модуля — вообще
без селектора. Выписанный перечень имён разошёлся бы с деревом молча, поэтому
цепочка раскрывается до неподвижной точки.

ПРИВЯЗКА ПОЗИЦИОННОГО АРГУМЕНТА СДВИНУЛА БЫ ИНДЕКС — и это тоже отказ, а не
догадка: `functools.partial(f, X)` съедает первый позиционный, и перепись,
не знающая об этом, считала бы форму не того аргумента.

Вход: `--decl <генератор>...` `--calls <каталог>...`
Выход: JSON.
"""
import ast
import json
import sys
from pathlib import Path

SEQ_NODES = (ast.List, ast.Tuple, ast.Set)


def _param_names(fn):
    a = fn.args
    return {p.arg for p in (*a.posonlyargs, *a.args, *a.kwonlyargs)}


def _isinstance_targets(test, params):
    """Имена параметров, о ФОРМЕ которых спрашивает это условие."""
    out = set()
    for n in ast.walk(test):
        if not isinstance(n, ast.Call):
            continue
        if getattr(n.func, "id", None) != "isinstance" or not n.args:
            continue
        t = n.args[0]
        if isinstance(t, ast.Name) and t.id in params:
            out.add(t.id)
    return out


def _only_raises(body):
    return bool(body) and all(isinstance(s, ast.Raise) for s in body)


def _classify(fn, params):
    """[(param, kind, lineno)] — kind: capability | guard | unknown-form."""
    found = []
    seen = set()
    for node in ast.walk(fn):
        if isinstance(node, ast.IfExp):
            for p in _isinstance_targets(node.test, params):
                found.append((p, "capability", node.lineno))
                seen |= _ids(node.test)
        elif isinstance(node, ast.If):
            tg = _isinstance_targets(node.test, params)
            if not tg:
                continue
            kind = "guard" if (_only_raises(node.body) or _only_raises(node.orelse)) else "capability"
            for p in tg:
                found.append((p, kind, node.lineno))
            seen |= _ids(node.test)
    # Проверка типа, не попавшая ни в одну известную форму: не находка и не
    # молчание, а невидимость — поэтому называется отдельно и роняет гейт.
    for node in ast.walk(fn):
        if not isinstance(node, ast.Call):
            continue
        if getattr(node.func, "id", None) != "isinstance" or not node.args:
            continue
        t = node.args[0]
        if isinstance(t, ast.Name) and t.id in params and id(node) not in seen:
            found.append((t.id, "unknown-form", node.lineno))
    return found


def _ids(test):
    return {id(n) for n in ast.walk(test)
            if isinstance(n, ast.Call) and getattr(n.func, "id", None) == "isinstance"}


def _positional_index(fn, param):
    a = fn.args
    names = [p.arg for p in (*a.posonlyargs, *a.args)]
    return names.index(param) if param in names else -1


def _alias_names(trees, roots):
    """Неподвижная точка: имена, за которыми стоит объявленный помощник.

    Формы, встречающиеся в дереве: прямое присваивание, `functools.partial`
    и запись в словарь строковым ключом (таблица впрыска).
    Возвращает (имена, сдвиги позиционного аргумента по именам).
    """
    names = set(roots)
    shifts = {}
    for _ in range(8):  # цепочка коротка; предел — против самоссылки
        before = len(names)
        for tree in trees:
            for node in ast.walk(tree):
                if isinstance(node, ast.Assign):
                    for tgt in node.targets:
                        nm = getattr(tgt, "id", None)
                        if nm:
                            src, shift = _source_of(node.value, names)
                            if src:
                                names.add(nm)
                                shifts[nm] = shifts.get(src, 0) + shift
                if isinstance(node, ast.Dict):
                    for k, v in zip(node.keys, node.values):
                        if not isinstance(k, ast.Constant) or not isinstance(k.value, str):
                            continue
                        src, shift = _source_of(v, names)
                        if src:
                            names.add(k.value)
                            shifts[k.value] = shifts.get(src, 0) + shift
                if isinstance(node, ast.ImportFrom):
                    for al in node.names:
                        if al.name in names and al.asname:
                            names.add(al.asname)
                            shifts[al.asname] = shifts.get(al.name, 0)
        if len(names) == before:
            break
    return names, shifts


def _source_of(value, names):
    """(имя-источник, сдвиг позиционного аргумента) либо (None, 0)."""
    if isinstance(value, ast.Name) and value.id in names:
        return value.id, 0
    if isinstance(value, ast.Call):
        f = value.func
        fname = getattr(f, "attr", None) or getattr(f, "id", None)
        if fname == "partial" and value.args:
            inner = value.args[0]
            if isinstance(inner, ast.Name) and inner.id in names:
                return inner.id, len(value.args) - 1
    return None, 0


def _shape(arg):
    if isinstance(arg, ast.Constant) and isinstance(arg.value, str):
        return "str"
    if isinstance(arg, SEQ_NODES):
        return "seq"
    return "unknown"


def main(argv) -> int:
    rest = argv[1:]
    decl = rest[rest.index("--decl") + 1:rest.index("--calls")]
    call_dirs = rest[rest.index("--calls") + 1:]

    trees = {}
    for d in decl:
        trees[d] = ast.parse(Path(d).read_text(encoding="utf-8"))

    subjects = []
    for path, tree in trees.items():
        for fn in tree.body:
            if not isinstance(fn, (ast.FunctionDef, ast.AsyncFunctionDef)):
                continue
            params = _param_names(fn)
            for param, kind, lineno in _classify(fn, params):
                subjects.append({
                    "file": path, "func": fn.name, "param": param,
                    "kind": kind, "line": lineno,
                    "index": _positional_index(fn, param),
                })

    # Дерево на КАЖДЫЙ путь ровно одно. Каталоги вызывающих включают
    # `.../scripts/`, где лежит сам генератор, — без сведения по пути его вызовы
    # считались бы дважды, и перепись врала бы в сторону «вызывающие есть».
    by_path = dict(trees)
    files_scanned = 0
    for raw in call_dirs:
        for f in sorted(Path(raw).glob("*.py")):
            key = str(f.resolve())
            if key in {str(Path(d).resolve()) for d in trees}:
                continue
            if key in by_path:
                continue
            files_scanned += 1
            by_path[key] = ast.parse(f.read_text(encoding="utf-8"))
    all_trees = list(by_path.values())

    out = []
    for s in subjects:
        names, shifts = _alias_names(all_trees, {s["func"]})
        shapes = {"str": 0, "seq": 0, "unknown": 0}
        shifted = sorted(n for n in names if shifts.get(n, 0) > 0)
        for tree in all_trees:
            for node in ast.walk(tree):
                if not isinstance(node, ast.Call):
                    continue
                nm = getattr(node.func, "id", None) or getattr(node.func, "attr", None)
                if nm not in names:
                    continue
                idx = s["index"]
                arg = None
                if 0 <= idx < len(node.args):
                    arg = node.args[idx]
                else:
                    for kw in node.keywords:
                        if kw.arg == s["param"]:
                            arg = kw.value
                if arg is None:
                    continue
                shapes[_shape(arg)] += 1
        out.append({**s, "aliases": sorted(names), "shifted": shifted, "shapes": shapes})

    sys.stdout.write(json.dumps({
        "generators": len(trees), "files": files_scanned, "subjects": out,
    }))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
