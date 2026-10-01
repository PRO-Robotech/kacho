# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Разбор и трёхзначное вычисление выражений `if:` / `continue-on-error:` GitHub Actions.

ЗАЧЕМ. Гейт покрытия IaC-скана судит, исполнится ли гейтовый шаг. Две редакции подряд
судили по ПОДСТРОКЕ: `!cancelled()` в тексте — «выживает»; `false` целиком — «ложно».
Приёмка нашла обе дыры: `always() && false` подстрока признаёт выжившим, хотя шаг не
исполняется никогда (H5), а `!cancelled() && steps.x.outcome == 'success'` — выжившим,
хотя он снимается вместе с шагом x (F2). Ответ — разбор выражения, а не поиск в тексте.

ФОРМА. Выражение разбирается в дерево (операторы `!`, `&&`, `||`, `==`, `!=`, `<`,
`<=`, `>`, `>=`, скобки; литералы `true`/`false`/`null`/числа/строки в одинарных
кавычках; вызовы функций; ссылки на контекст) и вычисляется в СЦЕНАРИИ — заданных
значениях функций статуса (`success()`, `failure()`, `always()`, `cancelled()`).
Ссылка на контекст (`github.*`, `steps.*`, `matrix.*`, …) и всякая другая функция —
НЕИЗВЕСТНО. Итог трёхзначный: True / False / None (неизвестно), и решение «заведомо»
выносится только на True или False; неизвестное — не доказано.

ПРАВИЛО ПЛАТФОРМЫ. Выражение без функции статуса исполняется как `success() && (…)`.
Здесь оно применяется так же: иначе голый `github.ref == …` читался бы как выживший.

Неразборное выражение — `ParseError`; гейт обязан назвать его находкой, а не угадать.
"""
import re

STATUS_FUNCS = ("success", "failure", "always", "cancelled")

# Сценарии: значения функций статуса.
PREDECESSOR_FAILED = {"success": False, "failure": True, "always": True, "cancelled": False}
ALL_SUCCEEDED = {"success": True, "failure": False, "always": True, "cancelled": False}
CANCELLED = {"success": False, "failure": False, "always": True, "cancelled": True}
SCENARIOS = (ALL_SUCCEEDED, PREDECESSOR_FAILED, CANCELLED)

_TOKEN = re.compile(r"""
    \s*(?:
      (?P<num>-?\d+(?:\.\d+)?)
    | (?P<str>'(?:[^']|'')*')
    | (?P<op>&&|\|\||==|!=|<=|>=|<|>|!|\(|\)|,|\[|\]|\.)
    | (?P<ident>[A-Za-z_][A-Za-z0-9_-]*|\*)
    )""", re.X)


class ParseError(ValueError):
    pass


class Unknown:
    """Значение, которое из текста не вычислить (контекст прогона)."""
    def __repr__(self):
        return "НЕИЗВЕСТНО"


UNKNOWN = Unknown()


def _strip(text):
    s = str(text).strip()
    if s.startswith("${{") and s.endswith("}}"):
        s = s[3:-2].strip()
    return s


def tokenize(text):
    s, pos, out = _strip(text), 0, []
    while pos < len(s):
        if s[pos:].strip() == "":
            break
        m = _TOKEN.match(s, pos)
        if not m or m.end() == pos:
            raise ParseError("не разобран фрагмент «%s»" % s[pos:pos + 20])
        kind = m.lastgroup
        out.append((kind, m.group(kind)))
        pos = m.end()
    return out


class _Parser:
    PREC = {"||": 1, "&&": 2, "==": 3, "!=": 3, "<": 4, "<=": 4, ">": 4, ">=": 4}

    def __init__(self, toks):
        self.t, self.i = toks, 0

    def peek(self):
        return self.t[self.i] if self.i < len(self.t) else (None, None)

    def take(self, value=None):
        tok = self.peek()
        if tok[0] is None:
            raise ParseError("выражение оборвано" + (" — ожидалось «%s»" % value if value else ""))
        if value is not None and tok[1] != value:
            raise ParseError("ожидалось «%s», найдено «%s»" % (value, tok[1]))
        self.i += 1
        return tok

    def expr(self, min_prec=1):
        left = self.unary()
        while True:
            kind, val = self.peek()
            if kind != "op" or val not in self.PREC or self.PREC[val] < min_prec:
                return left
            self.take()
            right = self.expr(self.PREC[val] + 1)
            left = ("bin", val, left, right)

    def unary(self):
        kind, val = self.peek()
        if kind == "op" and val == "!":
            self.take()
            return ("not", self.unary())
        return self.postfix(self.primary())

    def primary(self):
        kind, val = self.take()
        if kind == "num":
            return ("lit", float(val))
        if kind == "str":
            return ("lit", val[1:-1].replace("''", "'"))
        if kind == "op" and val == "(":
            node = self.expr()
            self.take(")")
            return node
        if kind == "ident":
            low = val.lower()
            if low == "true":
                return ("lit", True)
            if low == "false":
                return ("lit", False)
            if low == "null":
                return ("lit", None)
            if self.peek() == ("op", "("):
                self.take("(")
                args = []
                if self.peek() != ("op", ")"):
                    args.append(self.expr())
                    while self.peek() == ("op", ","):
                        self.take(",")
                        args.append(self.expr())
                self.take(")")
                return ("call", low, args)
            return ("ref", val)
        raise ParseError("неожиданный элемент «%s»" % val)

    def postfix(self, node):
        while True:
            kind, val = self.peek()
            if kind == "op" and val == ".":
                self.take(".")
                self.take()
                node = ("ref", "…")
            elif kind == "op" and val == "[":
                self.take("[")
                self.expr()
                self.take("]")
                node = ("ref", "…")
            else:
                return node


def parse(text):
    """→ дерево выражения. Пустое выражение — ParseError."""
    toks = tokenize(text)
    if not toks:
        raise ParseError("пустое выражение")
    p = _Parser(toks)
    node = p.expr()
    if p.i != len(toks):
        raise ParseError("лишний хвост «%s»" % " ".join(v for _, v in toks[p.i:]))
    return node


def _uses_status(node):
    if node[0] == "call" and node[1] in STATUS_FUNCS:
        return True
    return any(_uses_status(c) for c in node[1:] if isinstance(c, tuple)) or \
        (node[0] == "call" and any(_uses_status(a) for a in node[2]))


def _truthy(v):
    if v is UNKNOWN:
        return UNKNOWN
    return bool(v) and v != ""


def _eval(node, scen):
    kind = node[0]
    if kind == "lit":
        return node[1]
    if kind == "ref":
        return UNKNOWN
    if kind == "call":
        if node[1] in STATUS_FUNCS and not node[2]:
            return scen[node[1]]
        return UNKNOWN
    if kind == "not":
        v = _truthy(_eval(node[1], scen))
        return UNKNOWN if v is UNKNOWN else (not v)
    op, a, b = node[1], node[2], node[3]
    if op in ("&&", "||"):
        x = _truthy(_eval(a, scen))
        if op == "&&" and x is False:
            return False
        if op == "||" and x is True:
            return True
        y = _truthy(_eval(b, scen))
        if op == "&&":
            if y is False:
                return False
            return True if (x is True and y is True) else UNKNOWN
        if y is True:
            return True
        return False if (x is False and y is False) else UNKNOWN
    x, y = _eval(a, scen), _eval(b, scen)
    if x is UNKNOWN or y is UNKNOWN:
        return UNKNOWN
    try:
        return {"==": x == y, "!=": x != y, "<": x < y, "<=": x <= y,
                ">": x > y, ">=": x >= y}[op]
    except TypeError:
        return UNKNOWN


def evaluate(text, scen, implicit_success=True):
    """→ True / False / UNKNOWN: исполнится ли шаг с этим `if` в сценарии `scen`.

    `text is None` — ключа нет: платформа исполняет шаг как `success()`.
    """
    if text is None:
        return scen["success"]
    if isinstance(text, bool):
        return text
    if isinstance(text, (int, float)):
        return bool(text)
    node = parse(text)
    if implicit_success and not _uses_status(node):
        node = ("bin", "&&", ("call", "success", []), node)
    return _truthy(_eval(node, scen))


def value_truth(value, scen=ALL_SUCCEEDED):
    """Истинность значения ключа без правила `success() &&` (для `continue-on-error`)."""
    if value is None:
        return False
    if isinstance(value, bool):
        return value
    if isinstance(value, (int, float)):
        return bool(value)
    return evaluate(value, scen, implicit_success=False)


def survives_failure(text):
    """→ (True, None) | (False, причина): исполнится ли шаг И при успехе всех
    предыдущих, И при отказе любого из них (не отменённый прогон)."""
    try:
        ok = evaluate(text, ALL_SUCCEEDED)
        failed = evaluate(text, PREDECESSOR_FAILED)
    except ParseError as err:
        return False, "выражение не разобрано (%s)" % err
    if ok is True and failed is True:
        return True, None
    def say(v):
        return {True: "исполнится", False: "НЕ исполнится"}.get(v, "неизвестно (зависит от контекста прогона)")
    return False, "при успехе предыдущих — %s; при отказе предыдущего — %s" % (say(ok), say(failed))


def never_runs(text):
    """→ причина либо None: шаг с этим `if` не исполняется ни в одном сценарии."""
    if text is None:
        return None
    try:
        vals = [evaluate(text, s) for s in SCENARIOS]
    except ParseError as err:
        return "выражение не разобрано (%s)" % err
    return "ложно во всех сценариях прогона" if all(v is False for v in vals) else None
