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
import json
import re
import shutil
import subprocess
import sys

APPS = [
    # (app dir that owns the jest project, registry source path, lock test path)
    ("vpc", "shared/src/lib/resource-registry.tsx", "../shared/src/lib/resource-registry.create-required.test.ts"),
    ("nlb", "nlb/src/lib/resource-registry.tsx", "src/lib/resource-registry.create-required.test.ts"),
    ("storage", "storage/src/lib/resource-registry.tsx", "src/lib/resource-registry.create-required.test.ts"),
    ("compute", "compute/src/lib/resource-registry.tsx", "src/lib/resource-registry.create-required.test.ts"),
    ("registry", "registry/src/lib/resource-registry.tsx", "src/lib/resource-registry.create-required.test.ts"),
]


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


def const_names(src):
    """`const FIELD_X: FormField = { name: "y" ... }` -> {FIELD_X: "y"}."""
    res = {}
    for m in re.finditer(r"const\s+(\w+):\s*FormField\s*=\s*\{", src):
        s, e = block(src, src.index("{", m.start()))
        nm = re.search(r'name:\s*"([^"]+)"', src[s:e])
        if nm:
            res[m.group(1)] = nm.group(1)
    return res


def strip_comments(txt):
    txt = re.sub(r"/\*.*?\*/", "", txt, flags=re.S)
    return re.sub(r"//[^\n]*", "", txt)


def element_head(src, s, e, consts):
    """The required-field head this element declares, or None."""
    txt = strip_comments(src[s:e]).strip()
    if txt.startswith("..."):
        return None  # spread helper: not statically resolvable
    if txt.startswith("{"):
        nm = re.search(r'name:\s*"([^"]+)"', txt)
        return nm.group(1).split(".")[0] if nm else None
    ident = txt.strip().rstrip(",").strip()
    return consts.get(ident, "").split(".")[0] or None


def head_map(src):
    """specId -> sorted list of declared field heads, for every create-capable spec."""
    consts = const_names(src)
    out = {}
    for spec_id in re.findall(r'\n  "?([\w-]+)"?:\s*\{', src):
        try:
            s, e = spec_span(src, spec_id)
        except (KeyError, ValueError):
            continue
        fs = fields_span(src, s, e)
        if fs is None:
            continue
        out[spec_id] = sorted(
            h for h in (element_head(src, es, ee, consts) for es, ee in split_elements(src, *fs)) if h
        )
    return out


def remove_declarers(src, spec_id, field):
    """Drop every field element of `spec_id` whose head name == field."""
    s, e = spec_span(src, spec_id)
    fs = fields_span(src, s, e)
    if fs is None:
        return src, 0
    consts = const_names(src)
    drops = [
        (es, ee)
        for es, ee in split_elements(src, *fs)
        if element_head(src, es, ee, consts) == field
    ]
    for es, ee in reversed(drops):
        src = src[:es] + src[ee:]
    return src, len(drops)


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


def main():
    ok = True
    for app, reg_path, test_path in APPS:
        table_src = open(
            ("shared" if app == "vpc" else app) + "/src/lib/resource-registry.create-required.test.ts"
        ).read()
        table = dict(re.findall(r'"(/[^"]+)":\s*\[([^\]]*)\]', table_src))
        table = {k: [x.strip().strip('"') for x in v.split(",") if x.strip()] for k, v in table.items()}

        reg = open(reg_path).read()
        spec_ids = re.findall(r'\n  "?([\w-]+)"?:\s*\{', reg)
        pristine = reg

        for spec_id in spec_ids:
            try:
                s, e = spec_span(reg, spec_id)
            except KeyError:
                continue
            body = reg[s:e]
            if not re.search(r"create:\s*true", body):
                continue
            api = re.search(r'apiPath:\s*"([^"]+)"', body)
            if not api:
                continue
            for field in table.get(api.group(1), []):
                mutated, n = remove_declarers(pristine, spec_id, field)
                if n == 0:
                    print(f"  !! {app}/{spec_id}: no declaring field found for '{field}'")
                    ok = False
                    continue
                # The edit must remove exactly that input from exactly that spec.
                # Without this, a mis-sliced edit that mangles a NEIGHBOURING spec
                # reads as the lock catching something, when it caught the harness.
                before, after = head_map(pristine), head_map(mutated)
                touched = {k for k in before if before[k] != after.get(k)}
                expect_after = sorted(h for h in before[spec_id] if h != field)
                if touched != {spec_id} or after.get(spec_id) != expect_after:
                    print(f"  !! {app}/{spec_id}/{field}: edit was not surgical "
                          f"(touched={sorted(touched)})")
                    ok = False
                    continue
                open(reg_path, "w").write(mutated)
                rows, err = failing_rows(app, test_path)
                open(reg_path, "w").write(pristine)
                if err:
                    print(f"  !! {app}/{spec_id}/{field}: {err}")
                    ok = False
                    continue
                titles = [t for t, _ in rows]
                expect_title = f"{spec_id} ({api.group(1)})"
                names_it = f'"{field}"' in (rows[0][1] if rows else "")
                good = titles == [expect_title] and names_it
                print(f"  {'OK ' if good else 'BAD'} {app:8s} {spec_id:20s} -{field:18s} "
                      f"failed={titles if titles else 'NONE'}{'' if names_it else '  (does not name the field)'}")
                ok = ok and good
    print("\nMATRIX", "PASS" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
