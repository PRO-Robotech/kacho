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

    python3 tools/mixedoutcomeaudit/mixed_outcome_audit.py [REJECT|SUCCESS|BOTH|NONE|TEARDOWN|UNOWNED]

СЛОВАРЬ — ЧАСТЬ ПРЕДИКАТА, И ЕГО ТОЖЕ НАДО СВЕРЯТЬ С ДЕРЕВОМ. Первая редакция словаря
уборки не знала слов `unbind` / `del pool` / `delete-`, и пять шагов сноса попали в
«обещан успех» — предикат с узким словарём завышает находки ровно так же, как
завышает ноль предикат, который их не видит. Обе ошибки — одна и та же: доверие
числу без сверки мерки.

РАЗРЕШЕНИЕ МЕТКИ — ТОЖЕ ЧАСТЬ ПРЕДИКАТА, и в нём нашлось два дефекта (2026-07-30).
Оба врали в ОБЕ стороны — и завышали, и маскировали, — потому что подставляли находке
чужое обещание:

  1) `id=f"…"` не читался вовсе (регэксп требовал кавычку сразу после `id=`). Девять
     сгенерированных кейсов подсети получали метку соседнего, ничем с ними не
     связанного кейса, стоявшего выше по файлу; направление, соответственно, тоже.
     Теперь `f` допускается, а подстановки `{…}` из метки ВЫРЕЗАЮТСЯ (иначе `{prefix}`
     и `{i:02d}` попадали бы в словарь направления как слова).

  2) `oneOf` в теле параметризованного помощника (`_assert_absent(case_id, …)`,
     `_deny(case_id)`) занимал обещание ближайшего `id=` выше по файлу — то есть
     совершенно посторонннего кейса. Такой помощник раскрывается у КАЖДОГО вызывающего
     со своей меткой, и статически ни одна к нему не привязана. Теперь владение
     решается по границе функции: если объявление кейса лежит ВЫШЕ ближайшего `def`,
     метки нет, и находка идёт в корзину `UNOWNED` — она НЕ молчит, её надо смотреть
     глазами у каждого вызывающего.

Владение и читаемая часть метки — РАЗНЫЕ вещи: `id=f"{prefix}-CR-CONF-ALREADY-EXISTS"`
объявляет кейс (владение есть), но полностью не раскрывается (остаётся хвост). Поэтому
владение решается по ПОЗИЦИИ объявления, а направление — по прочитанному тексту.

ПЕРЕПИСЬ — ОТДЕЛЬНОЕ УТВЕРЖДЕНИЕ: печатается и число осмотренных файлов, и число тех,
в которых нашёлся хоть один смешивающий `oneOf`. «Ноль находок» обязано быть отличимо
от «ноль прочитанного» (пустой обход — выход с кодом 2, а не «чисто»).
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
# ВСЕ места вызова, а не только те, где список записан литералом прямо здесь.
#
# Прежняя редакция читала ТОЛЬКО литеральную форму `oneOf([400, 200])`. Форма,
# в которой список приходит подстановкой — `oneOf({codes})` при
# `codes = "[400, 200, 403, 404]"` — предикатом не виделась ВООБЩЕ, и он выходил
# нулём. На дереве это не гипотеза: одна такая строка
# (`services/vpc/tests/newman/scripts/gen.py`, матрица обязательных полей)
# раскрывается в 16 сгенерированных кейсов, каждый из которых заголовком обещает
# `400 InvalidArgument`, а утверждением принимает 200. Гейт печатал «чисто».
#
# Отсюда два изменения: сайты перечисляются ВСЕ, а список кодов, который не
# удалось прочитать статически, идёт в отдельный счётчик и РОНЯЕТ прогон.
# «Я этого не умею прочитать» не есть «здесь чисто» — это ровно то смешение,
# которое гейт и ловит, только уровнем выше.
RE_ONEOF_ANY = re.compile(r"to\.be\.oneOf\(")
# Присваивание идентификатору строки/выражения, из которого можно вынуть
# литеральные списки: `codes = "[400, 200]" if x else "[400, 404]"`.
RE_ASSIGN = re.compile(r"(?m)^[ \t]*([A-Za-z_]\w*)[ \t]*=[ \t]*(.+)$")
RE_BRACKETED = re.compile(r"\[([^\[\]]*)\]")
RE_IDENT = re.compile(r"\b([A-Za-z_]\w*)\b")
# f-string-подстановка внутри метки: статически не раскрывается, поэтому вырезается,
# а не читается как текст. Иначе `{i:02d}` и `{prefix}` попадали бы в словарь
# направления как обычные слова.
RE_FSUB = re.compile(r"\{[^{}]*\}")
RE_NUM = re.compile(r"\b([1-5]\d\d)\b")
RE_PMTEST = re.compile(r"pm\.test(?:\.skip)?\(\s*(['\"])(.*?)\1", re.S)
# Кортеж/список ЦЕЛЫХ КОДОВ, привязанный к имени. Ловит обе формы одинаково:
# обычное присваивание `sync_codes = (400, 403)` и — что здесь и важно —
# УМОЛЧАНИЕ В СИГНАТУРЕ `def refuse(..., sync_codes=(400,), ...)`. Умолчание есть
# та ветка, которая раскрывается у всякого вызывающего, её не указавшего, поэтому
# читать её обязательно; `RE_ASSIGN` её не видит (он якорится на начало строки и
# спотыкается о хвост аннотации возвращаемого типа).
RE_INT_TUPLE = re.compile(r"\b([A-Za-z_]\w*)\s*=\s*[\(\[]\s*((?:[1-5]\d\d)(?:\s*,\s*[1-5]\d\d)*)\s*,?\s*[\)\]]")


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


def resolve_ident(name, tuples, derived, seen=None):
    """Коды, которые может нести идентификатор. `None` — прочитать не удалось.

    Транзитивно: `codes` собирается из `sync_codes`, а `sync_codes` — умолчание
    сигнатуры. Останавливаться на первом звене значило бы объявить непрочитанным
    то, что прочитывается, и наоборот — прочитать половину и промолчать об
    остатке, а половина списка кодов хуже, чем честное «не прочитал».
    """
    if seen is None:
        seen = set()
    if name in seen:
        return None
    seen.add(name)
    if name in tuples:
        return sorted(set(tuples[name]))
    if name in derived:
        rhs = derived[name]
        out = {int(x) for x in RE_NUM.findall(rhs)}
        for ident in RE_IDENT.findall(rhs):
            if ident == name:
                continue
            sub = resolve_ident(ident, tuples, derived, seen)
            if sub:
                out.update(sub)
        return sorted(out) if out else None
    return None


def resolve_bracket(inner, tuples, derived):
    """Коды скобки `oneOf([...])`, часть которой — подстановка `{…}`.

    Возвращает `(коды, прочитано)`. КАЖДАЯ подстановка обязана дать хотя бы один
    код: подстановка, не давшая ничего, — непрочитанная часть списка, и списывать
    её в «здесь ничего нет» значит повторять ловимый класс уровнем ниже. Именно
    так предикат и слеп: скобка на месте ⇒ «список прочитан», а из `{codes}` не
    извлекалось ни одного кода, и пустота уходила в тишину.
    """
    codes = {int(x) for x in RE_NUM.findall(RE_FSUB.sub(" ", inner))}
    for sub in RE_FSUB.findall(inner):
        got = {int(x) for x in RE_NUM.findall(sub)}
        for ident in RE_IDENT.findall(sub):
            r = resolve_ident(ident, tuples, derived)
            if r:
                got.update(r)
        if not got:
            return [], False
        codes.update(got)
    return sorted(codes), True


# --- источник 1: исходники кейсов (там, где живут правки) --------------------
def scan_case_sources(paths):
    """Вернуть находки, перепись и список НЕПРОЧИТАННЫХ мест вызова.

    Третье значение — не украшение отчёта. Место вызова `oneOf(...)`, чей список
    кодов не раскрылся статически, предикатом не проверен, и молчание о нём
    неотличимо от «здесь чисто».
    """
    findings = []
    unreadable = []
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

        # позиции Case(id=..., title=...) и Step(name=...).
        #
        # `f?` — НЕ косметика. Генераторы объявляют кейс как `id=f"{prefix}-…"`, и
        # регэксп без `f?` их не видел: девять сгенерированных кейсов подсети
        # получали метку СОСЕДНЕГО, ничем с ними не связанного кейса, стоявшего
        # выше по файлу. Мерка, ошибающаяся в метке, ошибается и в направлении —
        # то есть в том единственном, ради чего она существует.
        # Позиция объявления и ЧИТАЕМАЯ часть метки — разные вещи, и путать их
        # нельзя. `id=f"{prefix}-CR-CONF-ALREADY-EXISTS"` объявляет кейс (значит
        # владение есть), но подстановку статически не раскрыть — остаётся хвост.
        # Поэтому владение решается по ПОЗИЦИИ объявления, а направление — по тому
        # тексту, который удалось прочитать (хвост id + title).
        id_positions = [m.start() for m in re.finditer(r"\bid=\s*f?[\"']", body)]
        cases = [(m.start(), RE_FSUB.sub(" ", m.group(1)))
                 for m in re.finditer(r"\bid=\s*f?[\"']([^\"']*)[\"']", body)]
        titles = [(m.start(), RE_FSUB.sub(" ", m.group(1)))
                  for m in re.finditer(r"title=\s*f?[\"'](.*?)[\"']\s*,", body, re.S)]
        steps = [(m.start(), RE_FSUB.sub(" ", m.group(1)))
                 for m in re.finditer(r"\bname=\s*f?[\"']([^\"']*)[\"']", body)]
        # Границы функций верхнего уровня: `oneOf`, лежащий в теле помощника,
        # который САМ принимает идентификатор кейса параметром, не принадлежит
        # никакому кейсу — и не вправе занимать обещание того, чей `id=` случайно
        # оказался выше по файлу. Такой помощник (`_assert_absent`, `_deny`)
        # раскрывается у КАЖДОГО вызывающего со своей меткой, и статически ни одна
        # из них к нему не привязана.
        defs = [m.start() for m in re.finditer(r"(?m)^def\s", body)]

        def nearest_before(pos, arr):
            best = None
            for p, v in arr:
                if p <= pos:
                    best = v
                else:
                    break
            return best or ""

        # Таблица «идентификатор → списки кодов, которые он может нести».
        # Собирается один раз на файл: `codes = "[400, 200, 403]" if f else "[400, 200]"`
        # даёт ДВА списка, и оба обязаны быть проверены — ветка, выбранная не для
        # всех вызывающих, всё равно уезжает в сгенерированную коллекцию.
        assigns = {}
        for a in RE_ASSIGN.finditer(body):
            lists = [[int(x) for x in RE_NUM.findall(g)]
                     for g in RE_BRACKETED.findall(a.group(2))]
            lists = [ls for ls in lists if ls]
            if lists:
                assigns.setdefault(a.group(1), []).extend(lists)

        # Вторая половина той же таблицы — для формы, где список приходит
        # ПОДСТАНОВКОЙ ВНУТРЬ СКОБОК: `oneOf([{codes}])`.
        #   tuples  — имя → целые коды, записанные кортежем/списком, включая
        #             УМОЛЧАНИЕ СИГНАТУРЫ (`sync_codes=(400,)`): это та ветка,
        #             которая раскрывается у всякого вызывающего, её не указавшего;
        #   derived — имя → правая часть присваивания без литерального списка
        #             (`codes = ", ".join(... sync_codes ... 200 ...)`), из которой
        #             коды добираются транзитивно.
        tuples, derived = {}, {}
        for m in RE_INT_TUPLE.finditer(body):
            tuples.setdefault(m.group(1), []).extend(
                int(x) for x in RE_NUM.findall(m.group(2)))
        for a in RE_ASSIGN.finditer(body):
            if a.group(1) not in tuples and a.group(1) not in assigns:
                derived.setdefault(a.group(1), a.group(2))

        literal_at = {m.start(): m for m in RE_ONEOF.finditer(body)}

        for site in RE_ONEOF_ANY.finditer(body):
            pos = site.start()
            # аргумент вызова: до закрывающей скобки того же уровня
            arg = body[site.end():body.find(")", site.end()) + 1
                       if body.find(")", site.end()) >= 0 else len(body)]
            inner = literal_at[pos].group(1) if pos in literal_at else None
            if inner is not None and RE_FSUB.search(inner):
                # СКОБКА ЕСТЬ, НО ЧАСТЬ ЕЁ — ПОДСТАНОВКА. До этой ветки такой сайт
                # уходил в ветку литерала, отдавал ПУСТОЙ список кодов и молча
                # пропадал: ни находкой, ни непрочитанным. Три исхода схлопывались
                # в «чисто» — то самое смешение, которое гейт и ловит, только на
                # уровень выше. Пять живых мест дерева записаны этой формой, и
                # четыре из них — переписи прежней формы `oneOf({codes})`, которую
                # предикат читать умел.
                codes, ok = resolve_bracket(inner, tuples, derived)
                if not ok:
                    if "response.code" not in body[max(0, pos - 200):pos]:
                        continue
                    unreadable.append({
                        "file": path,
                        "line": body[:pos].count("\n") + 1,
                        "arg": " ".join(inner.split())[:120],
                    })
                    continue
                code_lists = [codes]
            elif inner is not None:
                code_lists = [[int(x) for x in RE_NUM.findall(inner)]]
            else:
                code_lists = []
                for ident in RE_IDENT.findall(arg):
                    code_lists.extend(assigns.get(ident, []))
                if not code_lists:
                    # Непрочитанным считается только то, что вообще является
                    # предметом этой мерки — КОД ОТВЕТА HTTP. `oneOf` над
                    # `j.error.code` (коды gRPC) и над `s.status` (строковые
                    # состояния) сравнивает другие величины, и требовать от них
                    # читаемости значило бы краснеть на законном: такой гейт
                    # снимают при первом же ложном срабатывании.
                    if "response.code" not in body[max(0, pos - 200):pos]:
                        continue
                    unreadable.append({
                        "file": path,
                        "line": body[:pos].count("\n") + 1,
                        "arg": " ".join(arg.split())[:120],
                    })
                    continue
            # ближайший pm.test(' перед позицией
            seg = body[:pos]
            tm = None
            for t in RE_PMTEST.finditer(seg):
                tm = t
            pmlabel = tm.group(2) if tm else ""
            cid = nearest_before(pos, cases)
            ctitle = nearest_before(pos, titles)
            sname = nearest_before(pos, steps)
            # Метка принадлежит находке только если её `id=` лежит в ТОЙ ЖЕ
            # функции (или оба на уровне модуля). Иначе метки нет — и это
            # отдельный исход, а не повод взять чужую.
            last_def = max([d for d in defs if d < pos], default=-1)
            cid_pos = max([p for p in id_positions if p <= pos], default=-1)
            owned = cid_pos > last_def
            line = body[:pos].count("\n") + 1
            for codes in code_lists:
                if not codes or not classify(codes):
                    continue
                findings.append({
                    "file": path, "line": line, "codes": sorted(set(codes)),
                    "pmtest": pmlabel,
                    "case_id": cid if owned else "",
                    "case_title": ctitle if owned else "",
                    "step": sname, "owned": owned,
                })
    return findings, scanned, unreadable


def label_of(f, level):
    if level == "pmtest":
        return f["pmtest"]
    if level == "case":
        return f["case_title"] + " " + f["case_id"]
    return f["pmtest"] + " " + f["case_title"] + " " + f["case_id"]


def main():
    if "--self-test" in sys.argv:
        return self_test()
    roots = sorted(glob.glob("services/*/tests/newman")) + ["gateway/tests/newman"]
    paths = []
    for r in roots:
        paths += sorted(glob.glob(os.path.join(r, "cases", "*.py")))
        paths += sorted(glob.glob(os.path.join(r, "scripts", "gen.py")))
    findings, scanned, unreadable = scan_case_sources(paths)

    if scanned == 0:
        print("НИЧЕГО НЕ ПРОЧИТАНО — предикат ничего не утверждает", file=sys.stderr)
        return 2

    buckets = {"REJECT": [], "SUCCESS": [], "BOTH": [], "NONE": [], "TEARDOWN": [],
               "UNOWNED": []}
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
        if not f["owned"]:
            # Обещания нет: находка внутри параметризованного помощника. Она НЕ
            # молчит — она в своей корзине, и её надо смотреть глазами у каждого
            # вызывающего. Прежде такая находка занимала обещание ближайшего
            # чужого `id=` и классифицировалась неверно в ЛЮБУЮ сторону — как в
            # сторону завышения, так и в сторону маскировки.
            f["dir"] = "UNOWNED"
            buckets["UNOWNED"].append(f)
            continue
        f["dir"] = direction(label_of(f, "case"))
        buckets[f["dir"]].append(f)

    # Перепись — ОТДЕЛЬНОЕ утверждение: «ноль находок» обязано быть отличимо от
    # «ноль прочитанного». Поэтому печатаются и число осмотренных файлов, и число
    # тех, в которых вообще нашёлся исполняемый `oneOf` со смешанными кодами.
    with_findings = len({f["file"] for f in findings})
    print(f"осмотрено файлов: {scanned} (из них с находками: {with_findings})")
    print(f"исполняемых oneOf, СМЕШИВАЮЩИХ успех и отказ: {len(findings)}")
    for k in ("REJECT", "SUCCESS", "BOTH", "NONE", "TEARDOWN", "UNOWNED"):
        print(f"  метка обещает {k:8s}: {len(buckets[k])}")
    which = sys.argv[1] if len(sys.argv) > 1 else "SUCCESS"
    print(f"\n--- {which} ---")
    for f in sorted(buckets.get(which, []), key=lambda x: (x["file"], x["line"])):
        print(f'{f["file"]}:{f["line"]} codes={f["codes"]}')
        print(f'    pm.test: {f["pmtest"][:150]}')
        print(f'    case:    {f["case_id"]} — {f["case_title"][:120]}')

    # ─────────────────────────────────────────────────────────────────────────
    # ВЕРДИКТ. До 2026-07-30 здесь стоял безусловный `return 0`: предикат печатал
    # числа и выходил нулём ДАЖЕ когда находил нарушения. То есть он был отчётом,
    # а не меркой, — при том что шапка выше объясняет его существование именно
    # тем, что «стало 0» проверяемо только вместе с предикатом. Проверяемо оно
    # только если предикат УМЕЕТ СКАЗАТЬ НЕТ.
    #
    # Красными считаются РОВНО два направления — те, что описаны в шапке как
    # класс: метка обещает отказ, а утверждение принимает и успех, и зеркало.
    # Остальные корзины красными не являются и по разным причинам:
    #   • BOTH     — метка честно обещает оба исхода, смешение здесь и есть предмет;
    #   • NONE     — в метке нет направляющего слова, обещания нет вовсе;
    #   • TEARDOWN — идемпотентный снос вправе принять «уже нет»;
    #   • UNOWNED  — находка внутри параметризованного помощника, статически ничья.
    #
    # UNOWNED не роняет прогон намеренно: помощник — законная конструкция, и гейт,
    # краснеющий на законном, будет снят при первом же ложном срабатывании. Но и
    # молчать он не вправе — число печатается ниже отдельной строкой, потому что
    # смотреть такие находки надо глазами у каждого вызывающего.
    violations = len(buckets["REJECT"]) + len(buckets["SUCCESS"])
    # НЕПРОЧИТАННОЕ — тоже вердикт. Место вызова, чей список кодов предикат не
    # раскрыл, не проверен ничем; засчитать его в «чисто» значит повторить
    # ловимый класс на уровень выше.
    print(f"мест вызова oneOf, НЕ прочитанных статически: {len(unreadable)}")
    if unreadable:
        for u in sorted(unreadable, key=lambda x: (x["file"], x["line"]))[:40]:
            print(f'  {u["file"]}:{u["line"]}  аргумент: {u["arg"]}', file=sys.stderr)
        print(f"\nНЕ ПРОЧИТАНО: {len(unreadable)} мест вызова oneOf — их список кодов не "
              f"раскрылся статически, значит они НЕ ПРОВЕРЕНЫ. Исходы: записать список "
              f"литералом рядом с вызовом; присвоить его идентификатору в том же файле "
              f"(предикат раскрывает такие присваивания); либо научить предикат этой "
              f"форме. Промолчать нельзя: «не смог прочитать» неотличимо от «чисто».",
              file=sys.stderr)
        return 1
    if buckets["UNOWNED"]:
        print(f"\nВНИМАНИЕ: {len(buckets['UNOWNED'])} находок внутри параметризованных "
              f"помощников — статически ничьи, смотреть глазами у каждого вызывающего "
              f"(запуск с аргументом UNOWNED покажет их).", file=sys.stderr)
    if violations:
        print(f"\nНАРУШЕНИЙ: {violations} — утверждение принимает исход, "
              f"ПРОТИВОПОЛОЖНЫЙ обещанию своей метки.", file=sys.stderr)
        print("Запусти с аргументом REJECT и SUCCESS, чтобы увидеть оба направления.",
              file=sys.stderr)
        return 1
    print(f"\nчисто: ни одного утверждения, принимающего исход против обещания метки "
          f"(осмотрено файлов: {scanned})")
    return 0


def self_test():
    """Инъекция в ОБЕ стороны: предикат обязан краснеть на внесённом дефекте и
    молчать на законной конструкции той же формы.

    Гейт жил без этой ветки, и именно её отсутствие дало ему прожить со слепой
    зоной: он читал только литеральный список кодов, печатал «чисто» и был
    неотличим от работающего. Проверяются ровно те четыре формы, на которых
    ошибка и была возможна.
    """
    import tempfile

    def scan(src):
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "cases.py")
            open(p, "w", encoding="utf-8").write(src)
            return scan_case_sources([p])

    rc = 0

    def check(name, ok, detail=""):
        nonlocal rc
        print(f"  {'ОК ' if ok else 'ПРОВАЛ'} {name}{'' if ok else ' — ' + detail}")
        if not ok:
            rc = 1

    print("=== смешанный исход: инъекции ===")

    # (1) КРАСНОЕ, форма-подстановка. Заголовок обещает ОТКАЗ, список кодов
    #     приходит идентификатором и содержит 200. Ровно живая форма из
    #     services/vpc/tests/newman/scripts/gen.py.
    src = (
        'codes = "[400, 200, 403]"\n'
        'Case(id="X-CR-VAL-REQ-NAME", title="Create без required поля → 400 InvalidArgument",\n'
        '     steps=[Step(name="cr-no-name", test=f"pm.test(\'rejected\', () => '
        'pm.expect(pm.response.code).to.be.oneOf({codes}));")])\n'
    )
    f, _, u = scan(src)
    bad = [x for x in f if direction(label_of(x, "case")) == "REJECT"]
    check("подстановочный список кодов прочитан и признан нарушением",
          len(bad) == 1 and not u, f"находок REJECT {len(bad)}, непрочитанных {len(u)}")

    # (2) МОЛЧАНИЕ на законном той же формы: список кодов — только отказы
    #     (документированная терпимость authz-first), подстановка та же.
    src = (
        'codes = "[400, 403, 404]"\n'
        'Case(id="X-GET-NEG-ABSENT", title="Get несуществующего → отказ",\n'
        '     steps=[Step(name="get-absent", test=f"pm.test(\'rejected\', () => '
        'pm.expect(pm.response.code).to.be.oneOf({codes}));")])\n'
    )
    f, _, u = scan(src)
    check("список только из отказов той же формы — не находка",
          not f and not u, f"находок {len(f)}, непрочитанных {len(u)}")

    # (3) КРАСНОЕ: список кодов вообще не раскрывается статически. Молчать о нём
    #     нельзя — «не смог прочитать» неотличимо от «чисто».
    src = (
        'Case(id="X-CR-VAL", title="Create без поля → 400 InvalidArgument",\n'
        '     steps=[Step(name="cr", test=f"pm.test(\'rejected\', () => '
        'pm.expect(pm.response.code).to.be.oneOf({build_codes(fld)}));")])\n'
    )
    f, _, u = scan(src)
    check("нераскрываемый список попадает в непрочитанное, а не в «чисто»",
          len(u) == 1, f"непрочитанных {len(u)}")

    # (4) МОЛЧАНИЕ: обычный литеральный список — прежнее поведение не сломано.
    src = (
        'Case(id="X-CR-OK", title="Create → 200",\n'
        '     steps=[Step(name="cr", test="pm.test(\'ok\', () => '
        'pm.expect(pm.response.code).to.be.oneOf([200, 201]));")])\n'
    )
    f, _, u = scan(src)
    check("однородный литеральный список — не находка и не непрочитанное",
          not f and not u, f"находок {len(f)}, непрочитанных {len(u)}")

    # ── ФОРМА СО СКОБКАМИ, В КОТОРЫХ СТОИТ ПОДСТАНОВКА ───────────────────────
    #
    # Предыдущая редакция считала «скобка на месте» за «список прочитан»: сайт
    # попадал в ветку литерала, из `{codes}` не извлекалось НИ ОДНОГО кода, пустой
    # список молча пропускался — и в находки, и в непрочитанное. Три исхода
    # схлопывались в «чисто». Ровно этой формой записаны пять живых мест дерева, и
    # четыре из них появились ПЕРЕПИСЬЮ прежней формы `oneOf({codes})`, которую
    # предикат читать умел: переписав кейсы, мы увели их из-под собственной мерки.

    # (5) КРАСНОЕ: скобка с подстановкой, чей список раскрывается по умолчанию
    #     сигнатуры и СМЕШИВАЕТ успех с отказом. Живая форма vpc/nlb.
    src = (
        'def refuse(what, sync_codes=(400,), async_lane=True):\n'
        '    codes = ", ".join(str(c) for c in ((*sync_codes, 200) if async_lane else sync_codes))\n'
        '    return [f"pm.test(\'{what} refused\', () => '
        'pm.expect(pm.response.code).to.be.oneOf([{codes}]));"]\n'
    )
    f, _, u = scan(src)
    got = sorted({c for x in f for c in x["codes"]})
    check("скобка с подстановкой прочитана: смешанный список — находка, а не тишина",
          len(f) == 1 and 200 in got and 400 in got and not u,
          f"находок {len(f)} codes={got}, непрочитанных {len(u)}")

    # (6) МОЛЧАНИЕ на законном близнеце ТОЙ ЖЕ формы: скобка с подстановкой, чей
    #     список — только отказы. Без этой половины гейт ловил бы форму, а не
    #     существо, и был бы снят при первом ложном срабатывании.
    src = (
        'def refuse(what, sync_codes=(400, 403, 404)):\n'
        '    codes = ", ".join(str(c) for c in sync_codes)\n'
        '    return [f"pm.test(\'{what} refused\', () => '
        'pm.expect(pm.response.code).to.be.oneOf([{codes}]));"]\n'
    )
    f, _, u = scan(src)
    check("скобка с подстановкой, раскрывшаяся в одни отказы — не находка",
          not f and not u, f"находок {len(f)}, непрочитанных {len(u)}")

    # (7) КРАСНОЕ: скобка с подстановкой, которая не раскрывается ничем. «Не смог
    #     прочитать» обязано отличаться от «чисто» и в этой форме тоже.
    src = (
        'Case(id="X-CR-VAL", title="Create без поля → 400 InvalidArgument",\n'
        '     steps=[Step(name="cr", test=f"pm.test(\'rejected\', () => '
        'pm.expect(pm.response.code).to.be.oneOf([{build_codes(fld)}]));")])\n'
    )
    f, _, u = scan(src)
    check("нераскрываемая подстановка в скобках → непрочитанное, а не «чисто»",
          len(u) == 1 and not f, f"находок {len(f)}, непрочитанных {len(u)}")

    # (8) МОЛЧАНИЕ: подстановка в скобках над величиной, которая КОДОМ ОТВЕТА не
    #     является (строковые состояния Operation). Требовать от неё читаемости
    #     значило бы краснеть на законном.
    src = (
        'Case(id="X-OP", title="Operation доходит до терминального состояния",\n'
        '     steps=[Step(name="poll", test=f"pm.test(\'terminal\', () => '
        'pm.expect(j.status).to.be.oneOf([{states}]));")])\n'
    )
    f, _, u = scan(src)
    check("подстановка в скобках не над кодом ответа — не непрочитанное",
          not f and not u, f"находок {len(f)}, непрочитанных {len(u)}")

    print("\n" + ("PASS: смешанный исход" if rc == 0 else "FAIL: смешанный исход"))
    return rc


if __name__ == "__main__":
    sys.exit(main())
