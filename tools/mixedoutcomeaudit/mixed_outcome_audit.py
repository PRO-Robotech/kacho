#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Зеркальный предикат: утверждение, принимающее взаимоисключающие исходы.

Меряет ОБА направления одной меркой, а не одно из них:

  A (уже сметённое в vpc): метка обещает ОТКАЗ, а `oneOf` принимает и успех.
  B (зеркало, пропущенное): метка обещает УСПЕХ, а `oneOf` принимает и отказ.

Предикат работает по ИСПОЛНЯЕМОЙ части: строки-скрипты кейса, из которых удалены
JS-комментарии (`//` и `/* */`). `oneOf` в комментарии — не утверждение.

Метка — то, что читает человек в отчёте, в порядке близости к утверждению:
  1) имя самого `pm.test('<имя>'`, внутри которого стоит `oneOf`;
  2) `title=` объемлющего Case;
  3) `id=` объемлющего Case.

ЗАЧЕМ ЭТО В ДЕРЕВЕ. Свип 2026-07-29 закрыл направление A и записал «было 20, стало 0».
Число было верным для предиката, которым мерили, и неверным для класса: тот предикат
(а) смотрел только vpc и (б) наследовал асимметрию первого увиденного экземпляра —
искал «обещан отказ, принимается успех» и не искал зеркала. Перемер общей меркой нашёл
в том же направлении ещё 4 экземпляра в vpc и 14 в остальных суитах.

Поэтому мерка лежит рядом с кейсами, а не в чьей-то голове: «стало 0» проверяемо
только вместе с предикатом, которым мерили. Запускать из корня репозитория:

    python3 tools/mixedoutcomeaudit/mixed_outcome_audit.py [REJECT|SUCCESS|BOTH|NONE|TEARDOWN]

СЛОВАРЬ — ЧАСТЬ ПРЕДИКАТА, И ЕГО ТОЖЕ НАДО СВЕРЯТЬ С ДЕРЕВОМ. Первая редакция словаря
уборки не знала слов `unbind` / `del pool` / `delete-`, и пять шагов сноса попали в
«обещан успех» — предикат с узким словарём завышает находки ровно так же, как
завышает ноль предикат, который их не видит. Обе ошибки — одна и та же: доверие
числу без сверки мерки.
"""
import re
import sys
import glob
import os
import json

# --- словари направления -----------------------------------------------------
# Обещание ОТКАЗА. Только однозначное: код 4xx/5xx, слово отказа.
REJECT_WORDS = [
    r"\bdenied\b", r"\bdeny\b", r"\brejected\b", r"\breject\b", r"\bforbidden\b",
    r"\bmust not\b", r"\bmust NOT\b", r"\bcannot\b", r"\bnot allowed\b",
    r"\bno leak\b", r"\bnoleak\b", r"\bno-leak\b", r"\bno over-show\b", r"\bover-show\b",
    r"отказ", r"отверга", r"отвергн", r"запрещ", r"не должен", r"не должна", r"не видит",
    r"\b400\b", r"\b401\b", r"\b403\b", r"\b404\b", r"\b409\b", r"\b412\b", r"\b422\b", r"\b5xx\b",
    r"INVALID_ARGUMENT", r"PERMISSION_DENIED", r"NOT_FOUND", r"FAILED_PRECONDITION",
    r"ALREADY_EXISTS", r"UNAUTHENTICATED",
]
# Обещание УСПЕХА.
SUCCESS_WORDS = [
    r"\b200\b", r"\bOK\b", r"\bhappy\b", r"\bsuccess\b", r"\bsucceeds\b", r"\ballowed\b",
    r"\bvisible\b", r"\bsees\b", r"\bdoes see\b", r"\breturns the\b", r"\bround-?trip\b",
    r"успех", r"успешн", r"видит", r"проходит", r"применя",
]

RE_REJECT = re.compile("|".join(REJECT_WORDS))
RE_SUCCESS = re.compile("|".join(SUCCESS_WORDS))

RE_TEARDOWN = re.compile(
    r"\bcleanup\b|\bteardown\b|\bpreclean\b|\bpre-clean\b|\bcleanup-|^rm-|-rm$|\bdrop-|\bуборка\b"
    # Добавлено после сверки итогов: словарь уборки был уже словаря дерева, и пять
    # шагов сноса попали в «обещан успех» только потому, что называются иначе.
    # Предикат, чей словарь не покрывает дерево, завышает находки ровно так же, как
    # завышает ноль тот, чей словарь их не видит.
    r"|\bunbind\b|\bdel pool\b|^del-|\bdelete-|\bunreserve\b",
    re.I,
)
RE_ONEOF = re.compile(r"to\.be\.oneOf\(\s*\[([^\]]*)\]")
RE_NUM = re.compile(r"\b([1-5]\d\d)\b")
RE_PMTEST = re.compile(r"pm\.test(?:\.skip)?\(\s*(['\"])(.*?)\1", re.S)


def strip_js_comments(s: str) -> str:
    """Убрать JS-комментарии, сохранив позиции (заменой на пробелы)."""
    out = list(s)
    i, n = 0, len(s)
    in_str = None
    while i < n:
        c = s[i]
        if in_str:
            if c == "\\":
                i += 2
                continue
            if c == in_str:
                in_str = None
            i += 1
            continue
        if c in "'\"`":
            in_str = c
            i += 1
            continue
        if c == "/" and i + 1 < n and s[i + 1] == "/":
            j = s.find("\n", i)
            j = n if j < 0 else j
            for k in range(i, j):
                out[k] = " "
            i = j
            continue
        if c == "/" and i + 1 < n and s[i + 1] == "*":
            j = s.find("*/", i)
            j = n if j < 0 else j + 2
            for k in range(i, j):
                if out[k] != "\n":
                    out[k] = " "
            i = j
            continue
        i += 1
    return "".join(out)


def direction(label: str):
    r = bool(RE_REJECT.search(label))
    s = bool(RE_SUCCESS.search(label))
    if r and not s:
        return "REJECT"
    if s and not r:
        return "SUCCESS"
    if r and s:
        return "BOTH"
    return "NONE"


def classify(codes):
    has2 = any(200 <= c < 300 for c in codes)
    hasErr = any(c >= 400 for c in codes)
    return has2 and hasErr


# --- источник 1: исходники кейсов (там, где живут правки) --------------------
def scan_case_sources(paths):
    """Вернуть список находок по .py-исходникам кейсов и генератора."""
    findings = []
    scanned = 0
    for path in paths:
        try:
            raw = open(path, encoding="utf-8").read()
        except OSError:
            continue
        scanned += 1
        # Python-комментарии тоже не исполняются: строка, у которой перед `#`
        # нет открытой кавычки, — комментарий. Достаточная аппроксимация:
        # выбрасываем строки, чей первый непробельный символ — `#`.
        lines = raw.split("\n")
        keep = []
        for ln in lines:
            keep.append("" if ln.lstrip().startswith("#") else ln)
        body = strip_js_comments("\n".join(keep))

        # позиции Case(id=..., title=...) и Step(name=...)
        cases = [(m.start(), m.group(1)) for m in re.finditer(r"id=\s*[\"']([^\"']+)[\"']", body)]
        titles = [(m.start(), m.group(1)) for m in re.finditer(r"title=\s*[\"'](.*?)[\"']\s*,", body, re.S)]
        steps = [(m.start(), m.group(1)) for m in re.finditer(r"\bname=\s*[\"']([^\"']*)[\"']", body)]

        def nearest_before(pos, arr):
            best = None
            for p, v in arr:
                if p <= pos:
                    best = v
                else:
                    break
            return best or ""

        for m in RE_ONEOF.finditer(body):
            codes = [int(x) for x in RE_NUM.findall(m.group(1))]
            if not codes or not classify(codes):
                continue
            # ближайший pm.test(' перед позицией
            seg = body[:m.start()]
            tm = None
            for t in RE_PMTEST.finditer(seg):
                tm = t
            pmlabel = tm.group(2) if tm else ""
            cid = nearest_before(m.start(), cases)
            ctitle = nearest_before(m.start(), titles)
            sname = nearest_before(m.start(), steps)
            line = body[:m.start()].count("\n") + 1
            findings.append({
                "file": path, "line": line, "codes": sorted(set(codes)),
                "pmtest": pmlabel, "case_id": cid, "case_title": ctitle,
                "step": sname,
            })
    return findings, scanned


def label_of(f, level):
    if level == "pmtest":
        return f["pmtest"]
    if level == "case":
        return f["case_title"] + " " + f["case_id"]
    return f["pmtest"] + " " + f["case_title"] + " " + f["case_id"]


def main():
    roots = sorted(glob.glob("services/*/tests/newman")) + ["gateway/tests/newman"]
    paths = []
    for r in roots:
        paths += sorted(glob.glob(os.path.join(r, "cases", "*.py")))
        paths += sorted(glob.glob(os.path.join(r, "scripts", "gen.py")))
    findings, scanned = scan_case_sources(paths)

    if scanned == 0:
        print("НИЧЕГО НЕ ПРОЧИТАНО — предикат ничего не утверждает", file=sys.stderr)
        return 2

    buckets = {"REJECT": [], "SUCCESS": [], "BOTH": [], "NONE": [], "TEARDOWN": []}
    for f in findings:
        # Уборка — не предмет кейса. Идемпотентный снос вправе принять «уже нет»
        # (200 либо 404): обещание заголовка к нему не относится, поэтому такие
        # утверждения выносятся в отдельную корзину, а не выбрасываются молча.
        # ВАЖНО: снос — да, посев — НЕТ. Терпимость к сорванному посеву запрещена
        # (кейс поедет по несозданному ресурсу и позеленеет на промахе).
        if RE_TEARDOWN.search(f["step"] or "") or RE_TEARDOWN.search(f["pmtest"] or ""):
            f["dir"] = "TEARDOWN"
            buckets["TEARDOWN"].append(f)
            continue
        # Направление решает МЕТКА КЕЙСА (id + title) — то, что объявляет, ЗАЧЕМ
        # кейс существует. Имя `pm.test` частью обещания НЕ является: это часть
        # самого утверждения, и хеджирующее имя («200 или 403») — ровно тот
        # способ, которым дефект прячется от свипа по именам утверждений.
        f["dir"] = direction(label_of(f, "case"))
        buckets[f["dir"]].append(f)

    print(f"осмотрено файлов: {scanned}")
    print(f"исполняемых oneOf, СМЕШИВАЮЩИХ успех и отказ: {len(findings)}")
    for k in ("REJECT", "SUCCESS", "BOTH", "NONE", "TEARDOWN"):
        print(f"  метка обещает {k:8s}: {len(buckets[k])}")
    which = sys.argv[1] if len(sys.argv) > 1 else "SUCCESS"
    print(f"\n--- {which} ---")
    for f in sorted(buckets.get(which, []), key=lambda x: (x["file"], x["line"])):
        print(f'{f["file"]}:{f["line"]} codes={f["codes"]}')
        print(f'    pm.test: {f["pmtest"][:150]}')
        print(f'    case:    {f["case_id"]} — {f["case_title"][:120]}')
    return 0


if __name__ == "__main__":
    sys.exit(main())
