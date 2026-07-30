#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Предикат: поле ПУБЛИЧНОГО запроса, у которого нет читателя в прод-коде.

Правило, которое он меряет (`.claude/rules/api-conventions.md`, «Принято-и-
проигнорировано — ЗАПРЕЩЕНО»): поле публичного запроса обязано иметь читателя в
прод-коде своего сервиса. Молча принять и выбросить — не исход: вызывающий
получает успех и уверен, что его параметр применён.

ЧТО СЧИТАЕТСЯ ПУБЛИЧНЫМ ЗАПРОСОМ. Тип запроса RPC сервиса, чьё имя НЕ начинается
с `Internal` (ban #6: `Internal.*` живёт только на cluster-internal листенере), и
транзитивно — каждое message-поле такого типа: клиент вправе прислать вложенное
сообщение целиком, поэтому «принято и выброшено» одинаково относится и к
вложенному полю.

ЧТО СЧИТАЕТСЯ ЧИТАТЕЛЕМ — ТРИ ФОРМЫ, И ВСЕ ТРИ ОБЯЗАТЕЛЬНЫ. Предикат, знающий
только геттер, ЛЖЁТ: `CreateInstanceRequest.placement_group_id` читается как
`req.PlacementGroupId` (обращение к полю структуры, `internal/handler/
instance_handler.go`) и как строка `"placement_group_id"` (known-set маски,
`api/instance/instance.go`) — геттера у него нет ни одного. Поэтому читателем
считается любая из форм:

  1. `Get<Go>(`      — сгенерированный геттер;
  2. `.<Go>`         — обращение к полю структуры;
  3. `"<snake>"`     — имя поля строкой (known-set update_mask, whitelist
                       фильтра, `from_request_field` каталога прав — край читает
                       поле рефлексией ПО ИМЕНИ, и это настоящий читатель).

Смещение предиката — в сторону НЕДОобнаружения: любая из трёх форм закрывает
поле. Значит находка — высокой достоверности, а «ноль находок» не означает
«полей без читателя нет», означает «этим предикатом не найдено».

ИМЕНА ЧИТАЮТСЯ ИЗ СГЕНЕРИРОВАННОГО КОДА, А НЕ УГАДЫВАЮТСЯ. Go-имя поля берётся
из `pkg/api/**/*.pb.go` (`func (x *Msg) Get<Name>()`), а не выводится
самодельной камелизацией: правила protoc-gen-go для цифр и подчёркиваний
неочевидны, и предикат, ошибающийся в имени, ошибается в единственном, ради чего
существует. Следствие-предпосылка: **предикат читает только те сообщения, для
которых в дереве есть сгенерированный код**; сообщение без `.pb.go` он не видит и
сообщает об этом отдельной строкой переписи.

ПЕРЕПИСЬ — ОТДЕЛЬНОЕ УТВЕРЖДЕНИЕ. Печатаются: сколько proto-файлов прочитано,
сколько публичных сервисов и RPC найдено, сколько сообщений раскрыто, сколько
полей осмотрено и сколько сообщений осталось без сгенерированного кода. Пустой
обход — выход с кодом 2, а не «чисто».

Запуск из корня репозитория:

    python3 tools/unreadfieldaudit/unread_field_audit.py [<домен> …]

Без аргументов — все домены `proto/kacho/cloud/*/v1`.
"""
import glob
import os
import re
import sys

PROTO_ROOT = "proto/kacho/cloud"
GEN_ROOT = "pkg/api/kacho/cloud"

# ИМЯ PROTO-ДОМЕНА НЕ РАВНО ИМЕНИ КАТАЛОГА СЕРВИСА, и это не мелочь предпосылки:
# домен `loadbalancer` реализован в `services/nlb`, поэтому вывод пути по имени
# домена давал ПУСТОЕ дерево прод-кода — и предикат объявлял «без читателя» ВСЕ 87
# полей домена, ни одного файла не прочитав. Соответствие задаётся явно, а пустое
# дерево — отказ (см. ниже), а не тихий поток находок.
DOMAIN_SERVICE_DIR = {"loadbalancer": "nlb"}


# Деревья прод-кода, где законно живёт читатель поля запроса: сам сервис, край
# (grpc-gateway + authz-middleware читают поля запроса рефлексией по имени) и
# общий repo-root `internal/`.
def prod_trees(domain):
    return [f"services/{DOMAIN_SERVICE_DIR.get(domain, domain)}", "gateway", "internal"]


RE_SERVICE = re.compile(r"^service\s+(\w+)\s*\{", re.M)
RE_RPC = re.compile(r"\brpc\s+(\w+)\s*\(\s*([\w.]+)\s*\)")
# message-объявление верхнего или вложенного уровня.
RE_MESSAGE = re.compile(r"^(\s*)message\s+(\w+)\s*\{", re.M)
RE_FIELD = re.compile(
    r"^\s*(?:(repeated|optional)\s+)?([\w.]+)\s+(\w+)\s*=\s*(\d+)\s*[;\[]", re.M)
RE_GETTER = re.compile(r"^func \(x \*(\w+)\) Get(\w+)\(\)", re.M)
RE_MAP = re.compile(r"^\s*map<[^>]*>\s+(\w+)\s*=", re.M)

SCALARS = {
    "double", "float", "int32", "int64", "uint32", "uint64", "sint32", "sint64",
    "fixed32", "fixed64", "sfixed32", "sfixed64", "bool", "string", "bytes",
}


def strip_comments(s):
    out = re.sub(r"/\*.*?\*/", "", s, flags=re.S)
    return re.sub(r"//[^\n]*", "", out)


def parse_proto(path):
    """Вернуть (services, messages) одного .proto.

    services: {ServiceName: [(rpc, request_type)]}
    messages: {ShortName: [(field_name, type_name, repeated)]}  (вложенные — по
              короткому имени: этого достаточно, потому что Go-имена всё равно
              берутся из сгенерированного кода по короткому имени message'а).
    """
    body = strip_comments(open(path, encoding="utf-8").read())
    services = {}
    for m in RE_SERVICE.finditer(body):
        name = m.group(1)
        block = _balanced(body, m.end() - 1)
        services[name] = [(r.group(1), r.group(2)) for r in RE_RPC.finditer(block)]
    messages = {}
    for m in RE_MESSAGE.finditer(body):
        name = m.group(2)
        block = _balanced(body, m.end() - 1)
        own = _own_level(block)
        fields = []
        for f in RE_FIELD.finditer(own):
            label, typ, fname, _num = f.group(1), f.group(2), f.group(3), f.group(4)
            if fname in ("reserved", "option", "returns"):
                continue
            fields.append((fname, typ.split(".")[-1], label == "repeated"))
        for f in RE_MAP.finditer(own):
            fields.append((f.group(1), "", False))
        messages[name] = fields
    return services, messages


def _balanced(body, open_brace_idx):
    """Текст от открывающей `{` до её пары."""
    depth, i, n = 0, open_brace_idx, len(body)
    while i < n:
        if body[i] == "{":
            depth += 1
        elif body[i] == "}":
            depth -= 1
            if depth == 0:
                return body[open_brace_idx + 1:i]
        i += 1
    return body[open_brace_idx + 1:]


def _own_level(block):
    """Убрать вложенные `{…}`-блоки (их поля разбираются своим RE_MESSAGE-проходом),
    но СОХРАНИТЬ `[...]`-опции полей: поле объявляется `... = 7 [(validate)…];`, и
    вырезание квадратных скобок ничего не ломает, а вот вырезание фигурных —
    обязательно, иначе поля вложенного message'а достанутся объемлющему."""
    out, depth = [], 0
    for ch in block:
        if ch == "{":
            depth += 1
            continue
        if ch == "}":
            depth = max(0, depth - 1)
            continue
        if depth == 0:
            out.append(ch)
    return "".join(out)


def load_getters(domain):
    """{MessageName: {GoFieldName, …}} из сгенерированного кода домена."""
    out = {}
    for path in sorted(glob.glob(os.path.join(GEN_ROOT, domain, "v1", "*.pb.go"))):
        body = open(path, encoding="utf-8").read()
        for m in RE_GETTER.finditer(body):
            out.setdefault(m.group(1), set()).add(m.group(2))
    return out


def prod_sources(domain):
    """Список (path, text) непроверочных прод-файлов, где законен читатель."""
    files = []
    for root in prod_trees(domain):
        for dirpath, dirnames, filenames in os.walk(root):
            dirnames[:] = [d for d in dirnames
                           if d not in (".git", "testdata", "newman", "tests")]
            for fn in filenames:
                if not fn.endswith(".go") or fn.endswith("_test.go"):
                    continue
                p = os.path.join(dirpath, fn)
                if p.startswith("pkg/api/"):
                    continue
                try:
                    files.append((p, open(p, encoding="utf-8").read()))
                except OSError:
                    continue
    return files


def main():
    domains = sys.argv[1:] or [os.path.basename(os.path.dirname(d))
                               for d in sorted(glob.glob(os.path.join(PROTO_ROOT, "*", "v1")))]
    protos_read = svc_public = rpc_public = 0
    msgs_expanded = fields_seen = 0
    without_gen = []
    premise_failures = []
    findings = []

    for domain in domains:
        proto_paths = sorted(glob.glob(os.path.join(PROTO_ROOT, domain, "v1", "*.proto")))
        if not proto_paths:
            continue
        services, messages = {}, {}
        for p in proto_paths:
            protos_read += 1
            s, m = parse_proto(p)
            services.update(s)
            messages.update(m)

        # Публичная поверхность: типы запросов НЕ-Internal сервисов, транзитивно
        # по message-полям.
        roots = []
        for sname, rpcs in services.items():
            if sname.startswith("Internal"):
                continue
            svc_public += 1
            for _rpc, req in rpcs:
                rpc_public += 1
                roots.append(req.split(".")[-1])
        reach, queue = set(), list(roots)
        while queue:
            name = queue.pop()
            if name in reach or name not in messages:
                continue
            reach.add(name)
            for _fn, typ, _rep in messages[name]:
                if typ and typ not in SCALARS and typ in messages:
                    queue.append(typ)
        if not reach:
            continue

        getters = load_getters(domain)
        sources = prod_sources(domain)
        if not sources:
            # ПРЕДПОСЫЛКА НЕ ВЫПОЛНЕНА: дерево прод-кода этого домена не найдено, а
            # значит «читателя нет» здесь означает «читать было негде». Молчать нельзя
            # и находки этого домена печатать нельзя — они были бы утверждением о
            # непрочитанном.
            premise_failures.append(domain)
            continue
        for name in sorted(reach):
            gos = getters.get(name)
            if gos is None:
                without_gen.append(f"{domain}:{name}")
                continue
            msgs_expanded += 1
            for fname, _typ, _rep in messages[name]:
                go = _match_go(fname, gos)
                if go is None:
                    # Поле есть в proto, геттера в сгенерированном коде нет —
                    # предпосылка предиката не выполняется, молчать нельзя.
                    findings.append((domain, name, fname, "НЕТ ГЕТТЕРА В GEN"))
                    continue
                fields_seen += 1
                if not _has_reader(go, fname, sources):
                    findings.append((domain, name, fname, go))

    if protos_read == 0:
        print("НИЧЕГО НЕ ПРОЧИТАНО — предикат ничего не утверждает", file=sys.stderr)
        return 2

    print(f"прочитано .proto: {protos_read}; публичных сервисов: {svc_public}; "
          f"публичных RPC: {rpc_public}")
    print(f"раскрыто сообщений публичного запроса: {msgs_expanded} "
          f"(без сгенерированного кода, НЕ осмотрены: {len(without_gen)})")
    for name in without_gen:
        # Предпосылка предиката: Go-имена берутся из сгенерированного кода. Сообщение
        # без него не осмотрено — и это НАЗЫВАЕТСЯ, иначе «ноль находок» скрывало бы
        # слепую зону.
        print(f"    НЕ ОСМОТРЕНО (нет .pb.go): {name}")
    print(f"осмотрено полей: {fields_seen}")
    for d in premise_failures:
        print(f"ПРЕДПОСЫЛКА НЕ ВЫПОЛНЕНА: у домена {d!r} не найдено ни одного файла "
              f"прод-кода в {prod_trees(d)} — домен НЕ измерен (сопоставь его каталог "
              f"сервиса в DOMAIN_SERVICE_DIR)")
    print(f"полей БЕЗ читателя в прод-коде: {len(findings)}")
    for domain, msg, fname, go in findings:
        print(f"  {domain}: {msg}.{fname}  (Go: {go})")
    if premise_failures:
        return 2
    return 1 if findings else 0


def _match_go(snake, gos):
    """Сопоставить proto-имя поля с Go-именем из сгенерированного кода."""
    want = snake.replace("_", "").lower()
    for g in gos:
        if g.replace("_", "").lower() == want:
            return g
    return None


def _has_reader(go, snake, sources):
    getter = f"Get{go}("
    field = f".{go}"
    quoted = f'"{snake}"'
    for _p, text in sources:
        if getter in text or field in text or quoted in text:
            return True
    return False


if __name__ == "__main__":
    sys.exit(main())
