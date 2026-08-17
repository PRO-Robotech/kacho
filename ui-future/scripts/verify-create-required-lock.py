#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Injection matrix for resource-registry.create-required.test.ts.

For every (registry copy, create-capable spec, required field) triple, removes the
form field that declares that required field from the registry SOURCE, runs the
lock, and records which rows failed. A lock worth having must fail on exactly the
row whose input was removed — never fewer, never a different one.

Run from ui-future/:  python3 scripts/verify-create-required-lock.py

Exits non-zero unless every injection reddens exactly its own row. Restores each
registry after every run, and refuses to trust an edit that was not surgical.
"""
import glob
import json
import os
import re
import subprocess
import sys

LOCK_BASENAME = "resource-registry.create-required.test.ts"


def has_npm_script(module, script):
    """Does `module` declare that script? Its own package.json is the authority."""
    try:
        with open(os.path.join(module, "package.json")) as fh:
            return script in (json.load(fh).get("scripts") or {})
    except (OSError, ValueError):
        return False


def runner_for_library(module):
    """Which app runs the probes of a library module (one with no own `test`).

    `shared` has no run of its own by construction — его пробы исполняют
    модули-workspace, включающие `../shared/src` в свои `roots`. Кто именно —
    ВЫВОДИТСЯ из их конфигураций проб, а не выписывается: перечень таких модулей
    менялся и разошёлся бы молча.
    """
    candidates = []
    for cfg in sorted(glob.glob("*/jest.config.cjs")):
        app = os.path.dirname(cfg)
        if app == module or not has_npm_script(app, "test"):
            continue
        with open(cfg) as fh:
            src = fh.read()
        # Смотреть надо на то, что модуль ИСПОЛНЯЕТ (`roots`/`testMatch`), а не на
        # всякое упоминание пути. Свободный поиск по файлу давал ложный ответ:
        # `compute` ссылается на `../shared` ради setup-файла и подмены путей, но
        # проб shared НЕ гоняет — а матрица, поверив упоминанию, погнала бы замок
        # shared под чужим модулем.
        runs = "".join(
            m.group(1)
            for m in re.finditer(r"\b(?:roots|testMatch)\s*:\s*\[(.*?)\]", src, re.S)
        )
        if re.search(r"\.\./" + re.escape(module) + r"/src", runs):
            candidates.append(app)
    return candidates[0] if candidates else None


def discover_apps():
    """(app, registry path, lock path relative to app) — ВЫВЕДЕНО ИЗ ДЕРЕВА.

    Прежде здесь стоял литеральный список из пяти записей, и он был полон ровно
    потому, что реестров в дереве пять. Следующий модуль со своим реестром
    оказался бы вне матрицы, а «инъекции прошли» означало бы «прошли те, что были
    перечислены». Перечень поэтому берётся из индекса git — того же множества,
    которое увидит свежий клон и конвейер.
    """
    out, skipped = [], []
    paths = subprocess.run(
        ["git", "ls-files", "*/src/lib/resource-registry.tsx"],
        capture_output=True, text=True, check=True,
    ).stdout.split()
    for reg_path in sorted(paths):
        module = reg_path.split("/")[0]
        lock = os.path.join(module, "src/lib", LOCK_BASENAME)
        if not os.path.exists(lock):
            skipped.append(f"{module}: замка {LOCK_BASENAME} рядом с реестром нет")
            continue
        if has_npm_script(module, "test"):
            out.append((module, reg_path, os.path.join("src/lib", LOCK_BASENAME)))
            continue
        runner = runner_for_library(module)
        if runner is None:
            skipped.append(f"{module}: библиотека, но ни один модуль не включает её в roots")
            continue
        out.append((runner, reg_path, os.path.join("..", module, "src/lib", LOCK_BASENAME)))
    return out, skipped


def scan(src, i, end, on_char):
    """Walk src[i:end] skipping strings and comments; call on_char(char, index).

    Comments must be skipped: this file is full of prose, and an apostrophe in a
    comment («don't», «id'шник») would otherwise open a string and desync every
    bracket count after it.
    """
    in_str, esc = None, False
    while i < end:
        c = src[i]
        if in_str:
            if esc:
                esc = False
            elif c == "\\":
                esc = True
            elif c == in_str:
                in_str = None
        elif c == "/" and i + 1 < end and src[i + 1] == "/":
            i = src.find("\n", i)
            if i == -1 or i >= end:
                return
            continue
        elif c == "/" and i + 1 < end and src[i + 1] == "*":
            j = src.find("*/", i + 2)
            i = (j + 2) if j != -1 else end
            continue
        elif c in "\"'`":
            in_str = c
        else:
            on_char(c, i)
        i += 1


def block(src, start):
    """Slice from `start` (at an opening brace/bracket) to its match."""
    open_ch = src[start]
    close_ch = {"{": "}", "[": "]"}[open_ch]
    state = {"depth": 0, "end": None}

    def step(c, i):
        if state["end"] is not None:
            return
        if c == open_ch:
            state["depth"] += 1
        elif c == close_ch:
            state["depth"] -= 1
            if state["depth"] == 0:
                state["end"] = i + 1

    scan(src, start, len(src), step)
    if state["end"] is None:
        raise ValueError("unbalanced")
    return start, state["end"]


def spec_span(src, spec_id):
    m = re.search(r'\n  "?' + re.escape(spec_id) + r'"?:\s*\{', src)
    if not m:
        raise KeyError(spec_id)
    return block(src, src.index("{", m.start()))


def fields_span(src, s, e):
    m = re.search(r"\n    fields:\s*\[", src[s:e])
    if not m:
        return None
    return block(src, s + src[s:e].index("[", m.start()))


def split_elements(src, s, e):
    """Top-level elements of an array literal src[s:e] -> [(start, end)]."""
    inner_s, inner_e = s + 1, e - 1
    out, state = [], {"depth": 0, "cur": inner_s}

    def step(c, i):
        if c in "{[(":
            state["depth"] += 1
        elif c in "}])":
            state["depth"] -= 1
        elif c == "," and state["depth"] == 0:
            if src[state["cur"]:i].strip():
                out.append((state["cur"], i + 1))
            state["cur"] = i + 1

    scan(src, inner_s, inner_e, step)
    if src[state["cur"]:inner_e].strip():
        out.append((state["cur"], inner_e))
    return out


NAME_STRING = re.compile(r'name:\s*"([^"]+)"')
NAME_TEMPLATE = re.compile(r"name:\s*`([^`]*)`")


def name_head(txt):
    """Голова имени поля (`a.b.c` -> `a`), или None, если статически неизвестна.

    ИМЯ БЫВАЕТ ШАБЛОНОМ, и предикат по `name: "…"` его не видит. Ветви проверки
    живости объявлены как `name: `health_check.${branch}.port`` — четыре поля,
    невидимых прежнему разбору. Следствие было не «недосчитали», а хуже: матрица
    снимала семь ОСТАЛЬНЫХ полей `health_check`, шаблонные оставались, замок
    по-прежнему находил голову и оставался ЗЕЛЁНЫМ. Инъекция выглядела
    выполненной, а доказывала обратное тому, ради чего ставилась.

    У шаблона голова берётся из статической части ДО первой подстановки: у
    `health_check.${branch}.port` она есть и равна `health_check`. Если
    подстановка стоит раньше точки, головы статически нет — и тогда None, а не
    догадка.
    """
    m = NAME_STRING.search(txt)
    if m:
        return m.group(1).split(".")[0] or None
    m = NAME_TEMPLATE.search(txt)
    if m:
        static = m.group(1).split("${")[0]
        return static.split(".")[0] or None if "." in static else None
    return None


def const_names(src):
    """`const FIELD_X: FormField = { name: "y" ... }` -> {FIELD_X: "y"}."""
    res = {}
    for m in re.finditer(r"const\s+(\w+):\s*FormField\s*=\s*\{", src):
        s, e = block(src, src.index("{", m.start()))
        head = name_head(src[s:e])
        if head:
            res[m.group(1)] = head
    return res


def strip_comments(txt):
    txt = re.sub(r"/\*.*?\*/", "", txt, flags=re.S)
    return re.sub(r"//[^\n]*", "", txt)


FIELD_ARRAY_CONST = re.compile(r"(?:export\s+)?const\s+(\w+)\s*:\s*FormField\[\]\s*=\s*\[")
FIELD_ARRAY_FN = re.compile(r"(?:export\s+)?function\s+(\w+)\s*\([^)]*\)\s*:\s*FormField\[\]\s*\{")


def field_arrays(src):
    """name -> (start, end) of every `FormField[]` array literal in this source.

    Обе формы, потому что обе стоят в дереве: именованная константа
    (`GUEST_ACCESS_KEY_FIELDS`) и функция-построитель (`healthCheckFields()`).
    """
    out = {}
    for m in FIELD_ARRAY_CONST.finditer(src):
        # Скобка берётся из КОНЦА совпадения, а не поиском вперёд от его начала:
        # в объявлении `const X: FormField[] = [` первая `[` принадлежит ТИПУ, и
        # поиск находил её — пролёт длиной в два символа, из которого элементов
        # не выходило ни одного, а спека молча оставалась без полей.
        out[m.group(1)] = block(src, m.end() - 1)
    for m in FIELD_ARRAY_FN.finditer(src):
        body = src.index("{", m.end() - 1)
        rm = re.search(r"return\s*\[", src[body:])
        if rm:
            out[m.group(1)] = block(src, body + src[body:].index("[", rm.start()))
    return out


class Pool:
    """Источники, в которых МОГУТ быть объявлены поля формы одного реестра.

    ПОЧЕМУ НЕ ОДИН ФАЙЛ. Матрица правила только исходник реестра, поэтому поля,
    пришедшие из общего модуля, вынуть не могла НИ ОДНО: `fields:
    GUEST_ACCESS_KEY_FIELDS` — не массив, а `...healthCheckFields()` — не объект.
    Замок эти поля видит (он держит настоящий объект), а его способность на них
    ПОКРАСНЕТЬ проверена не была: матрица печатала «no declaring field found» и
    роняла прогон, то есть класс был виден, но не закрыт.

    Набор модулей ВЫВОДИТСЯ из дерева (`*-form.ts` рядом с реестром и в shared),
    а не выписывается.
    """

    def __init__(self, reg_path):
        module = reg_path.split("/")[0]
        paths = [reg_path]
        for pat in (f"{module}/src/lib/*-form.ts", "shared/src/lib/*-form.ts"):
            paths += [p for p in sorted(glob.glob(pat)) if p not in paths]
        self.paths = paths
        self.pristine = {p: open(p).read() for p in paths}
        self.src = dict(self.pristine)

    def restore(self):
        for p, text in self.pristine.items():
            open(p, "w").write(text)
        self.src = dict(self.pristine)

    def lookup(self, name):
        """(path, s, e) of the array declaring `name`; реестр имеет приоритет."""
        for p in self.paths:
            spans = field_arrays(self.src[p])
            if name in spans:
                return (p,) + spans[name]
        return None


def element_head(src, s, e, consts):
    """The required-field head this element declares, or None (spread -> None)."""
    txt = strip_comments(src[s:e]).strip()
    if txt.startswith("..."):
        return None
    if txt.startswith("{"):
        return name_head(txt)
    ident = txt.strip().rstrip(",").strip()
    return consts.get(ident, "").split(".")[0] or None


def walk_elements(pool, path, s, e, seen=None):
    """Every field element reachable from an array -> [(path, start, end, head)].

    Раскрывает `...helper()` и `...CONST` рекурсивно: голова поля объявлена там,
    где стоит его `name`, а не там, где массив подставлен.
    """
    seen = set() if seen is None else seen
    src = pool.src[path]
    consts = const_names(src)
    out = []
    for es, ee in split_elements(src, s, e):
        txt = strip_comments(src[es:ee]).strip()
        if txt.startswith("..."):
            m = re.match(r"(\w+)", txt[3:].strip())
            if not m:
                continue
            tgt = pool.lookup(m.group(1))
            if tgt is None or (tgt[0], m.group(1)) in seen:
                continue
            seen.add((tgt[0], m.group(1)))
            out += walk_elements(pool, tgt[0], tgt[1], tgt[2], seen)
            continue
        head = element_head(src, es, ee, consts)
        if head:
            out.append((path, es, ee, head))
    return out


def spec_elements(pool, reg_path, spec_id):
    """Field elements of one spec, wherever they are declared."""
    src = pool.src[reg_path]
    try:
        s, e = spec_span(src, spec_id)
    except (KeyError, ValueError):
        return []
    fs = fields_span(src, s, e)
    if fs is not None:
        return walk_elements(pool, reg_path, *fs)
    # `fields: IDENT` — массив объявлен именем, в этом файле или в общем модуле.
    m = re.search(r"\n    fields:\s*(\w+)\s*,", src[s:e])
    if not m:
        return []
    tgt = pool.lookup(m.group(1))
    return [] if tgt is None else walk_elements(pool, tgt[0], tgt[1], tgt[2])


def head_map(pool, reg_path):
    """specId -> sorted declared field heads, for every spec of the registry."""
    out = {}
    for spec_id in re.findall(r'\n  "?([\w-]+)"?:\s*\{', pool.src[reg_path]):
        els = spec_elements(pool, reg_path, spec_id)
        if els:
            out[spec_id] = sorted(h for _, _, _, h in els)
    return out


def remove_declarers(pool, reg_path, spec_id, field):
    """Drop every element declaring `field`, in whatever source it lives.

    Возвращает число снятых элементов и множество спецификаций, которых снятие
    ЗАКОННО касается: общий модуль подставлен не одной спекой, и ожидать «ровно
    одну красную строку» там, где входа лишились две, значило бы требовать от
    замка неверного.
    """
    drops = [(p, es, ee) for p, es, ee, h in spec_elements(pool, reg_path, spec_id) if h == field]
    if not drops:
        return 0, set()

    dropped = set(drops)
    affected = {
        sid
        for sid in re.findall(r'\n  "?([\w-]+)"?:\s*\{', pool.src[reg_path])
        if any((p, es, ee) in dropped for p, es, ee, _ in spec_elements(pool, reg_path, sid))
    }

    by_file = {}
    for p, es, ee in drops:
        by_file.setdefault(p, []).append((es, ee))
    for p, spans in by_file.items():
        text = pool.src[p]
        for es, ee in sorted(spans, reverse=True):
            text = text[:es] + text[ee:]
        pool.src[p] = text
        open(p, "w").write(text)
    return len(drops), affected


def failing_rows(app, test_path):
    p = subprocess.run(
        ["node", "--no-warnings", "--experimental-vm-modules", "../node_modules/jest/bin/jest.js",
         "--ci", "--forceExit", "--json", test_path],
        cwd=app, capture_output=True, text=True,
    )
    blob = p.stdout[p.stdout.index("{"):] if "{" in p.stdout else "{}"
    try:
        data = json.loads(blob)
    except json.JSONDecodeError:
        return None, "jest produced no report"
    rows = []
    for suite in data.get("testResults", []):
        for t in suite.get("assertionResults", []):
            if t["status"] == "failed":
                rows.append((t["title"], " ".join(t.get("failureMessages", []))))
    return rows, None


def row_title(reg_src, spec_id):
    """Заголовок строки замка — так его печатает сам замок: `id (apiPath)`."""
    s, e = spec_span(reg_src, spec_id)
    api = re.search(r'apiPath:\s*"([^"]+)"', reg_src[s:e])
    return f"{spec_id} ({api.group(1) if api else '?'})"


def main():
    ok = True
    apps, skipped = discover_apps()
    if not apps:
        print("!! реестров в дереве не найдено — предикат поиска устарел. "
              "«Ноль инъекций» здесь означало бы «ноль прочитанного», а это не вердикт")
        return 1
    for note in skipped:
        print(f"  .. пропущен {note}")

    census = []
    for app, reg_path, test_path in apps:
        module = reg_path.split("/")[0]
        table_src = open(os.path.join(module, "src/lib", LOCK_BASENAME)).read()
        table = dict(re.findall(r'"(/[^"]+)":\s*\[([^\]]*)\]', table_src))
        table = {k: [x.strip().strip('"') for x in v.split(",") if x.strip()] for k, v in table.items()}

        pool = Pool(reg_path)
        spec_ids = re.findall(r'\n  "?([\w-]+)"?:\s*\{', pool.pristine[reg_path])
        seen_specs = creatable = rows_tried = 0

        for spec_id in spec_ids:
            seen_specs += 1
            try:
                s, e = spec_span(pool.pristine[reg_path], spec_id)
            except KeyError:
                continue
            body = pool.pristine[reg_path][s:e]
            if not re.search(r"create:\s*true", body):
                continue
            api = re.search(r'apiPath:\s*"([^"]+)"', body)
            if not api:
                continue
            creatable += 1
            for field in table.get(api.group(1), []):
                rows_tried += 1
                before = head_map(pool, reg_path)
                n, affected = remove_declarers(pool, reg_path, spec_id, field)
                if n == 0:
                    pool.restore()
                    print(f"  !! {app}/{spec_id}: no declaring field found for '{field}'")
                    ok = False
                    continue
                # The edit must remove exactly that input from exactly the specs
                # that declared it. Without this, a mis-sliced edit that mangles a
                # NEIGHBOURING spec reads as the lock catching something, when it
                # caught the harness.
                after = head_map(pool, reg_path)
                touched = {k for k in before if before[k] != after.get(k)}
                bad_after = [
                    k for k in affected
                    if after.get(k) != sorted(h for h in before.get(k, []) if h != field)
                ]
                if touched != affected or bad_after:
                    pool.restore()
                    print(f"  !! {app}/{spec_id}/{field}: edit was not surgical "
                          f"(touched={sorted(touched)}, expected={sorted(affected)})")
                    ok = False
                    continue
                rows, err = failing_rows(app, test_path)
                pool.restore()
                if err:
                    print(f"  !! {app}/{spec_id}/{field}: {err}")
                    ok = False
                    continue
                titles = sorted(t for t, _ in rows)
                want = sorted(row_title(pool.pristine[reg_path], sid) for sid in affected)
                names_it = all(f'"{field}"' in msg for _, msg in rows) and bool(rows)
                good = titles == want and names_it
                shared_note = "" if len(affected) == 1 else f"  (общий модуль: строк {len(affected)})"
                print(f"  {'OK ' if good else 'BAD'} {app:8s} {spec_id:20s} -{field:18s} "
                      f"failed={titles if titles else 'NONE'}"
                      f"{'' if names_it else '  (does not name the field)'}{shared_note}")
                ok = ok and good
        census.append((app, module, seen_specs, creatable, rows_tried))

    print("\nПЕРЕПИСЬ (объём осмотренного — «ноль инъекций» отличимо от «ноль прочитанного»):")
    for app, module, seen_specs, creatable, rows_tried in census:
        note = "" if rows_tried else "  <- обязательных полей в замке не объявлено"
        print(f"  {module:10s} (гоняет {app:8s}) спек {seen_specs:3d} · создаваемых {creatable:2d} · инъекций {rows_tried:2d}{note}")
    print(f"  ИТОГО реестров {len(apps)} · инъекций {sum(c[4] for c in census)}")
    print("\nMATRIX", "PASS" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
