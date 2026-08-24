#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Гейт: у каждого адреса, который набирает волна, есть ПРОИЗВОДИТЕЛЬ в прогонщике.

ЧТО ЗДЕСЬ ЗАПРЕЩЕНО И ПОЧЕМУ ЭТО НЕ ВИДНО БЕЗ ГЕЙТА
----------------------------------------------------
Волна включается фактом о дереве, печатает свою строку в журнал, выводит набор
коллекций — и не может исполниться ни разу, потому что адреса, которые набирает её
посев, никто не создаёт. Со стороны это неотличимо от работающего механизма: строка
включения есть, набор выведен, отказ приходит изнутри посева и читается как «стенд не
готов». Ровно это здесь и было: прогонщик открывал пять пробросов, а посев набирал
четыре адреса, которых среди них не было. Работало, пока пробросы держал человек
руками; на чистой машине волна не проходила НИКОГДА.

Это тот же класс, что «гейт, у входа которого нет производителя», только этажом ниже:
не поле запроса без производителя, а транспорт без производителя.

ЧТО ИМЕННО ПРОВЕРЯЕТСЯ
----------------------
Для каждого модуля, ДОСТИЖИМОГО из прогонщика, берётся каждый адрес вида
`os.environ.get("ИМЯ", "…localhost:ПОРТ…")` (для python — разбором, не поиском слова)
и `ИМЯ="${ИМЯ:-…localhost:ПОРТ…}"` (для shell). Такой адрес обязан иметь ОДНО из двух:

  * порт умолчания проброшен прогонщиком, либо
  * прогонщик передаёт `ИМЯ` явно, и порт в переданном значении проброшен.

Второе — предпочтительная форма: тогда открывающий порт и набирающий адрес суть один
факт и разойтись не могут.

ЧЕГО ГЕЙТ НЕ ДЕЛАЕТ: не судит о модулях, до которых из прогонщика не дойти. Их адреса
создаёт кто-то другой (собственная волна, CI, человек), и утверждать о них отсюда
нечего. Зато объём осмотренного печатается ВСЕГДА: «находок нет» обязано быть отличимо
от «ничего не прочитано».

ВТОРОЙ ПРЕДЕЛ, НАЗВАННЫЙ ПОТОМУ ЧТО ОН УЖЕ ПРОПУСТИЛ ДЕФЕКТ: передача читается
ПО ПЕРЕМЕННОЙ, А НЕ ПО ВЫЗОВУ. Прогонщик запускает посев несколькими отдельными
`env … bash <скрипт>`-блоками, и `passed` собирается со ВСЕГО файла: имя, переданное
в одном блоке, засчитывается модулю, который достижим только из другого. Ровно так
2026-08-05 прошёл `IAM_INTERNAL_GRPC` — он передавался блоку матрицы и НЕ передавался
блоку церемонии, поэтому чеканка церемонии набирала умолчание, волна не заводила
условие, и девять коллекций iam остались без отчёта, пока гейт был зелёным.
Закрыть это значит сопоставлять каждому `env`-блоку множество модулей, достижимых
из ЕГО скрипта, и проверять передачу поблочно — переделка обхода, а не правка
предиката. До тех пор предел действует, и знать о нём обязан всякий, кто добавляет
новый блок запуска: адреса перечисляются заново для КАЖДОГО блока, «уже передаётся
выше» здесь не аргумент.

ПРЕДПОСЫЛКИ ГЕЙТА проверяются и отказывают ГРОМКО: нет прогонщика, из него не вычитался
ни один проброс, не достигнут ни один модуль — это ОТКАЗ, а не «чисто».

    python3 deploy/scripts/assert-dialled-transports-have-a-producer.py [--root .]
    python3 deploy/scripts/assert-dialled-transports-have-a-producer.py --self-test
"""
from __future__ import annotations

import argparse
import ast
import os
import re
import sys
import tempfile

RUNNER = "deploy/scripts/newman-parallel.sh"

# ─────────────────────────────────────────────────────────────────────────────
# Адрес, который НЕ набирают: он ИДЕНТИЧНОСТЬ, а не транспорт
# ─────────────────────────────────────────────────────────────────────────────
# Строка вида `https://localhost:ПОРТ/...` не обязана быть достижимой: провайдер
# ОБЪЯВЛЯЕТ себя таким адресом, и значение сравнивается либо кладётся в утверждение
# токена. Пробросить такой порт было бы не «починкой», а порчей: совпасть значение
# обязано с тем, что провайдер о себе говорит, а не с тем, куда мы дозвонились.
#
# Запись обязана нести ПРИЧИНУ и обязана иметь ПРЕДМЕТ: имя, которого ни один
# достижимый модуль больше не читает, — находка, а не безобидный остаток. Иначе
# следующий читатель унаследует её как действующее послабление и спрячет за ней
# настоящий транспорт без производителя.
NOT_A_TRANSPORT: dict[str, str] = {
    "HYDRA_ISSUER_PREFIX": (
        "объявленная издателем приставка. Значение только ищется и ЗАМЕНЯЕТСЯ на "
        "адрес, по которому издатель реально отвечает; сокет по нему не открывается."
    ),
}

# `VAR="${OTHER:-значение}"` — умолчание shell-переменной прогонщика.
_SH_DEFAULT = re.compile(r'^\s*([A-Za-z_][A-Za-z0-9_]*)="\$\{([A-Za-z_][A-Za-z0-9_]*):-([^}"]*)\}"')
# `port-forward svc/имя "$VAR:цель"` либо `"18080:8080"` — host-порт, который создаётся.
_PF = re.compile(r'port-forward\s+\S+\s+"?(\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?|[0-9]+):[0-9A-Za-z_-]+"?')
# `ИМЯ="scheme://localhost:$VAR"` / `ИМЯ="localhost:$VAR"` — явная передача адреса.
_PASS = re.compile(
    r'\b([A-Z][A-Z0-9_]*)="(?:[a-z]+://)?(?:localhost|127\.0\.0\.1):(\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?|[0-9]+)')
# Ссылка на другой скрипт/модуль в теле скрипта.
_REF = re.compile(r"[\w./$-]*[\w-]+\.(?:sh|py)\b")
# Адрес-умолчание в shell: `ИМЯ="${ИМЯ:-http://localhost:24433}"`.
_SH_DIAL = re.compile(
    r'([A-Z][A-Z0-9_]*)="\$\{[A-Za-z_][A-Za-z0-9_]*:-(?:[a-z]+://)?(?:localhost|127\.0\.0\.1):([0-9]+)')
_HOSTPORT = re.compile(r'^(?:[a-z]+://)?(?:localhost|127\.0\.0\.1):([0-9]+)')


class PremiseError(RuntimeError):
    """Предпосылка гейта не выполнена — утверждать о дереве не на чем."""


def _read(path: str) -> str:
    with open(path, encoding="utf-8") as fh:
        return fh.read()


def parse_runner(root: str) -> dict:
    """Умолчания переменных, созданные порты и явно переданные адреса прогонщика."""
    path = os.path.join(root, RUNNER)
    if not os.path.exists(path):
        raise PremiseError(f"прогонщика нет по пути {RUNNER} — читать нечего")
    text = _read(path)

    shvars: dict[str, str] = {}
    for line in text.splitlines():
        m = _SH_DEFAULT.match(line)
        if m:
            shvars[m.group(1)] = m.group(3)

    def _resolve(tok: str) -> str | None:
        if tok.isdigit():
            return tok
        name = tok.lstrip("$").strip("{}")
        val = shvars.get(name)
        return val if val and val.isdigit() else None

    # Порт → ИМЯ РУЧКИ, которой этот проброс двигается (None = записан литералом).
    # Различие несущее, а не справочное: проброс, host-порт которого взят из ручки,
    # переезжает вместе с ней, а адрес-умолчание — НЕТ. Их совпадение держится на
    # том, что умолчания совпали, и разъезжается в первом же прогоне, где ручку
    # сдвинули. Литерал так разъехаться не может — он неподвижен by construction.
    forwarded: dict[str, str | None] = {}
    for m in _PF.finditer(text):
        port = _resolve(m.group(1))
        if not port:
            continue
        knob = m.group(2)  # None, когда host-порт записан числом
        # Литерал выигрывает: если тот же порт где-то прописан числом, он неподвижен.
        if port not in forwarded or knob is None:
            forwarded[port] = knob

    passed: dict[str, str] = {}
    for m in _PASS.finditer(text):
        port = _resolve(m.group(2))
        if port:
            passed[m.group(1)] = port

    if not forwarded:
        raise PremiseError(
            f"из {RUNNER} не вычитался НИ ОДИН проброс — форма строки изменилась, "
            "и молчание этого гейта означало бы не «чисто», а «не прочитано»")
    return {"text": text, "forwarded": forwarded, "passed": passed, "path": path}


def reachable(root: str, runner_path: str) -> tuple[list[str], list[str]]:
    """Скрипты и модули, достижимые из прогонщика; плюс неразрешённые ссылки."""
    seen: set[str] = set()
    unresolved: set[str] = set()
    queue = [runner_path]
    while queue:
        cur = queue.pop()
        cur = os.path.normpath(cur)
        if cur in seen or not os.path.isfile(cur):
            continue
        seen.add(cur)
        try:
            body = _read(cur)
        except OSError:
            continue
        here = os.path.dirname(cur)
        for raw in set(_REF.findall(body)):
            for var in ("$REPO_ROOT/", "$SCRIPT_DIR/", "$ROOT/", "$SUITE_DIR/", "${REPO_ROOT}/", "./"):
                if raw.startswith(var):
                    raw = raw[len(var):]
            if "$" in raw:
                continue
            cands = [os.path.join(root, raw), os.path.join(here, raw),
                     os.path.join(here, "..", raw)]
            hit = next((c for c in cands if os.path.isfile(c)), None)
            if hit:
                queue.append(hit)
            elif raw.endswith((".py", ".sh")):
                unresolved.add(raw)
        # python-модуль тянет соседей по имени импорта
        if cur.endswith(".py"):
            try:
                tree = ast.parse(body)
            except SyntaxError:
                continue
            for n in ast.walk(tree):
                mods = []
                if isinstance(n, ast.Import):
                    mods = [a.name for a in n.names]
                elif isinstance(n, ast.ImportFrom) and n.module and n.level:
                    mods = [n.module]
                for mod in mods:
                    cand = os.path.join(here, mod.split(".")[0] + ".py")
                    if os.path.isfile(cand):
                        queue.append(cand)
    return sorted(seen), sorted(unresolved)


def dialled(paths: list[str], root: str) -> list[tuple[str, str, str]]:
    """(модуль, ИМЯ, порт) — адреса-умолчания, которые набирают достижимые модули."""
    out: list[tuple[str, str, str]] = []
    for p in paths:
        rel = os.path.relpath(p, root)
        try:
            body = _read(p)
        except OSError:
            continue
        if p.endswith(".py"):
            # Разбором, а не поиском слова: имя переменной встречается в прозе,
            # в докстроках и в примерах запуска, и текстовый поиск считал бы их.
            try:
                tree = ast.parse(body)
            except SyntaxError:
                continue
            for n in ast.walk(tree):
                if not (isinstance(n, ast.Call) and isinstance(n.func, ast.Attribute)
                        and n.func.attr == "get" and len(n.args) == 2):
                    continue
                f = n.func.value
                if not (isinstance(f, ast.Attribute) and f.attr == "environ"):
                    continue
                name, default = n.args
                if not (isinstance(name, ast.Constant) and isinstance(name.value, str)):
                    continue
                if not (isinstance(default, ast.Constant) and isinstance(default.value, str)):
                    continue
                m = _HOSTPORT.match(default.value)
                if m:
                    out.append((rel, name.value, m.group(1)))
        else:
            for m in _SH_DIAL.finditer(body):
                out.append((rel, m.group(1), m.group(2)))
    return out


def check(root: str) -> tuple[int, list[str], dict]:
    runner = parse_runner(root)
    paths, unresolved = reachable(root, runner["path"])
    if len(paths) < 2:
        raise PremiseError(
            "из прогонщика не достигнут НИ ОДИН модуль — обход сломан, "
            "и «находок нет» здесь означало бы «ничего не прочитано»")
    addrs = dialled(paths, root)
    findings: list[str] = []
    named = {name for _, name, _ in addrs}
    for exc, why in sorted(NOT_A_TRANSPORT.items()):
        if exc not in named:
            findings.append(
                f"послаблению «{exc}» больше нечего исключать — ни один достижимый "
                f"модуль его не читает. Причина записи: {why} Снять запись.")
    for rel, name, port in sorted(set(addrs)):
        if name in NOT_A_TRANSPORT:
            continue
        if name in runner["passed"]:
            if runner["passed"][name] in runner["forwarded"]:
                continue
            findings.append(
                f"{rel}: адрес {name} передан прогонщиком на порт {runner['passed'][name]}, "
                f"но этот порт не пробрасывается")
            continue
        if port in runner["forwarded"]:
            knob = runner["forwarded"][port]
            if knob is None:
                continue  # порт неподвижен: совпасть тут не с чем, разъехаться нечему
            findings.append(
                f"{rel}: адрес {name} (умолчание :{port}) держится на СОВПАДЕНИИ умолчаний — "
                f"прогонщик двигает этот проброс ручкой {knob}, а {name} не передаёт. "
                f"Сдвинь {knob} — проброс уедет, адрес останется на :{port}, и набор уйдёт "
                f"в чужой сокет либо в пустоту. Передавать {name} явно")
            continue
        findings.append(
            f"{rel}: адрес {name} (умолчание :{port}) — прогонщик его НЕ передаёт "
            f"и порт :{port} не пробрасывает; набирать некуда")
    scope = {"scripts": len(paths), "addresses": len(set(addrs)),
             "forwarded": len(runner["forwarded"]), "passed": len(runner["passed"]),
             "unresolved": len(unresolved), "exempt": len(NOT_A_TRANSPORT)}
    return len(findings), findings, scope


# ─────────────────────────────────────────────────────────────────────────────
# Самопроверка инъекцией — в ОБЕ стороны, с законным близнецом
# ─────────────────────────────────────────────────────────────────────────────
_FAKE_RUNNER = '''#!/usr/bin/env bash
GW_PORT="${GW_PORT:-18080}"
EXTRA_PORT="${EXTRA_PORT:-24433}"
kubectl -n "$NS" port-forward svc/api-gateway "$GW_PORT:8080" &
kubectl -n "$NS" port-forward svc/lit "28081:80" &
GOOD_URL="http://localhost:$GW_PORT"
%s
bash tests/fake/seed.sh
'''
_FAKE_SEED_SH = 'python3 tests/fake/mod.py\npython3 tests/fake/ident.py\n'
# Три законные формы разом, чтобы «молчит» и «краснеет» мерились на одном дереве:
#   GOOD_URL — передан прогонщиком явно (форма «б», предпочтительная);
#   LIT_URL  — форма «а» над ЛИТЕРАЛЬНЫМ пробросом: порт неподвижен, разъехаться нечему;
#   EXTRA_URL — форма «а» над пробросом, который двигает ручка EXTRA_PORT: это и есть
#               совпадение умолчаний, и оно обязано быть находкой, когда проброс есть.
_FAKE_MOD = ('import os\n'
             'A = os.environ.get("GOOD_URL", "http://localhost:18080")\n'
             'L = os.environ.get("LIT_URL", "http://localhost:28081")\n'
             'B = os.environ.get("EXTRA_URL", "http://localhost:24433")\n')
# Второй модуль даёт ПРЕДМЕТ объявленным идентичностям: без него каждая проверка
# ниже краснела бы по ветке самоистечения, и три случая из четырёх измеряли бы не то,
# ради чего написаны.
_FAKE_IDENT = ('import os\n' + "".join(
    f'{n} = os.environ.get("{n}", "https://localhost:28080/x")\n' for n in sorted(NOT_A_TRANSPORT)))


def _self_test() -> int:
    ok = True

    def build(tmp: str, with_forward: bool, with_ident: bool = True) -> None:
        os.makedirs(os.path.join(tmp, "deploy", "scripts"), exist_ok=True)
        os.makedirs(os.path.join(tmp, "tests", "fake"), exist_ok=True)
        extra = ('kubectl -n "$NS" port-forward svc/x "$EXTRA_PORT:80" &'
                 if with_forward else "")
        with open(os.path.join(tmp, RUNNER), "w", encoding="utf-8") as fh:
            fh.write(_FAKE_RUNNER % extra)
        with open(os.path.join(tmp, "tests/fake/seed.sh"), "w", encoding="utf-8") as fh:
            fh.write(_FAKE_SEED_SH)
        with open(os.path.join(tmp, "tests/fake/mod.py"), "w", encoding="utf-8") as fh:
            fh.write(_FAKE_MOD)
        if with_ident:
            with open(os.path.join(tmp, "tests/fake/ident.py"), "w", encoding="utf-8") as fh:
                fh.write(_FAKE_IDENT)

    # (а) настоящий дефект — проброса нет: гейт обязан покраснеть И НАЗВАТЬ адрес.
    with tempfile.TemporaryDirectory() as tmp:
        build(tmp, with_forward=False)
        n, f, _ = check(tmp)
        hit = n == 1 and "EXTRA_URL" in f[0] and "24433" in f[0]
        print(f"  {'ОК ' if hit else 'ПРОВАЛ'} инъекция: адрес без производителя — красный и назван")
        ok &= hit

    # (б) законные близнецы — та же форма, но производитель НЕ МОЖЕТ разъехаться:
    #     передан явно (GOOD_URL) либо проброшен литералом (LIT_URL). Гейт обязан
    #     молчать ПРО НИХ — именно про них, а не «вообще»: судить по общему счёту
    #     находок значило бы принять за успех молчание по чужой причине.
    with tempfile.TemporaryDirectory() as tmp:
        build(tmp, with_forward=True)
        n, f, _ = check(tmp)
        hit = not any("GOOD_URL" in x or "LIT_URL" in x for x in f)
        print(f"  {'ОК ' if hit else 'ПРОВАЛ'} законные близнецы (передан явно / литеральный "
              f"проброс) — молчит" + ("" if hit else f" ({f})"))
        ok &= hit

    # (е) СОВПАДЕНИЕ УМОЛЧАНИЙ — проброс есть, но его двигает ручка, а адрес не передан.
    #     Гейт обязан покраснеть и назвать ОБЕ координаты: адрес и ручку, которой он
    #     не следует. Без этой пары находка нечинима — читатель видит «порт проброшен»
    #     и закрывает её как ложную.
    with tempfile.TemporaryDirectory() as tmp:
        build(tmp, with_forward=True)
        n, f, _ = check(tmp)
        hit = any("EXTRA_URL" in x and "EXTRA_PORT" in x for x in f)
        print(f"  {'ОК ' if hit else 'ПРОВАЛ'} совпадение умолчаний: проброс двигается ручкой, "
              f"адрес — нет — красный, названы адрес и ручка")
        ok &= hit

    # (в) предпосылка: прогонщик без единого проброса — ГРОМКИЙ отказ, не тишина.
    with tempfile.TemporaryDirectory() as tmp:
        build(tmp, with_forward=False)
        with open(os.path.join(tmp, RUNNER), "w", encoding="utf-8") as fh:
            fh.write("#!/usr/bin/env bash\necho no forwards here\n")
        try:
            check(tmp)
            hit = False
        except PremiseError:
            hit = True
        print(f"  {'ОК ' if hit else 'ПРОВАЛ'} предпосылка (ни одного проброса) — громкий отказ")
        ok &= hit

    # (в2) объявленная идентичность НЕ маскирует настоящий транспорт: она молчит
    # только про своё имя. Тот же порт под ДРУГИМ именем обязан остаться находкой.
    with tempfile.TemporaryDirectory() as tmp:
        build(tmp, with_forward=False)
        with open(os.path.join(tmp, "tests/fake/ident.py"), "a", encoding="utf-8") as fh:
            fh.write('C = os.environ.get("REAL_URL", "https://localhost:28080/x")\n')
        n, f, _ = check(tmp)
        hit = any("REAL_URL" in x for x in f)
        print(f"  {'ОК ' if hit else 'ПРОВАЛ'} послабление именное: тот же порт под другим именем — находка")
        ok &= hit

    # (г) послабление, которому нечего исключать, — САМО находка (самоистечение).
    with tempfile.TemporaryDirectory() as tmp:
        build(tmp, with_forward=True, with_ident=False)
        n, f, _ = check(tmp)
        # Считаем находки ПРО ПОСЛАБЛЕНИЯ, а не все подряд: на этом же дереве законно
        # краснеет совпадение умолчаний (случай «е»), и общий счёт мерил бы уже не
        # самоистечение.
        about_exempt = [x for x in f if any(name in x for name in NOT_A_TRANSPORT)]
        hit = len(about_exempt) == len(NOT_A_TRANSPORT) and all(
            any(name in x for x in about_exempt) for name in NOT_A_TRANSPORT)
        print(f"  {'ОК ' if hit else 'ПРОВАЛ'} самоистечение: послабление без предмета — находка")
        ok &= hit

    # (д) предпосылка: прогонщика нет вовсе — ГРОМКИЙ отказ.
    with tempfile.TemporaryDirectory() as tmp:
        try:
            check(tmp)
            hit = False
        except PremiseError:
            hit = True
        print(f"  {'ОК ' if hit else 'ПРОВАЛ'} предпосылка (прогонщика нет) — громкий отказ")
        ok &= hit

    print("PASS: самопроверка гейта" if ok else "FAIL: самопроверка гейта")
    return 0 if ok else 1


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=".")
    ap.add_argument("--self-test", action="store_true")
    args = ap.parse_args()
    if args.self_test:
        return _self_test()
    try:
        n, findings, scope = check(args.root)
    except PremiseError as exc:
        print(f"ОТКАЗ: {exc}", file=sys.stderr)
        return 2
    print(f"осмотрено: {scope['scripts']} скрипт(ов)/модул(ей), достижимых из {RUNNER}; "
          f"адресов-умолчаний {scope['addresses']}; пробросов {scope['forwarded']}; "
          f"передано явно {scope['passed']}; объявлено идентичностью {scope['exempt']}; "
          f"ссылок не разрешено {scope['unresolved']}")
    if n:
        print(f"НАХОДКИ ({n}) — волна набирает адрес, которого никто не создаёт:", file=sys.stderr)
        for line in findings:
            print(f"  {line}", file=sys.stderr)
        return 1
    print("OK: у каждого набираемого адреса есть производитель в прогонщике.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
