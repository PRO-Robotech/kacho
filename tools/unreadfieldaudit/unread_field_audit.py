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

ЧТО СЧИТАЕТСЯ ЧИТАТЕЛЕМ — ДВЕ ОСНОВЫ, И ОНИ РАЗНОЙ СИЛЫ.

  1. ЧТЕНИЕ У ЭТОГО СООБЩЕНИЯ (сильная, основная). Вызов геттера `x.GetFoo()` или
     обращение к полю `x.Foo`, где ТИП `x` — именно это сообщение. Тип резолвится
     `go/types`; индекс строит `tools/unreadfieldaudit/cmd/proto-field-readers`,
     этот предикат его запускает сам. Совпадение имён здесь не закрывает ничего
     by construction.
  2. ИМЯ ПОЛЯ СТРОКОЙ (слабая). `"<snake>"` в прод-коде: known-set маски
     обновления, whitelist фильтра, `from_request_field` каталога прав — край
     читает поле РЕФЛЕКСИЕЙ ПО ИМЕНИ, и это настоящий читатель. Но у строки нет
     получателя, поэтому приписать её конкретному сообщению нечем: если такое имя
     объявлено не в одном сообщении домена, читатель мог принадлежать другому.
     Поля, закрытые ТОЛЬКО этой формой, печатаются поимённо, а не прячутся в
     «закрыто».

ПОЧЕМУ ОСНОВА СМЕНИЛАСЬ (issue kacho#110). Прежде все формы искали ИМЯ, и имя
между сообщениями одного домена не уникально (`filter`, `name`, `labels`,
`page_size`). Замер на `e5bbd92f`: из 826 осмотренных полей 755 закрывались
именем-омонимом — девять десятых вердикта держались на совпадении имён, то есть
«полей без читателя: 0» опиралось на атрибуцию, которая могла указывать на другое
сообщение. Так предикат пропускал `NetworkInterfaceSpec.nic_id` и `.index` (оба
найдены глазами, оба оказались дефектами). После смены основы слабой атрибуцией
закрыто 8 полей — и они названы поимённо.

ПРЕДПОСЫЛКИ, БЕЗ КОТОРЫХ ПРЕДИКАТ НЕ УТВЕРЖДАЕТ НИЧЕГО (проверяются, а не
предполагаются; при невыполнении — выход с кодом 2, не «чисто»):

  * дерево СОБИРАЕТСЯ: типы берутся из export-данных `go list -deps -export`.
    Пакет, который не протипизировался, — это пакет, чьи чтения НЕВИДИМЫ, а его
    поля стали бы ложными находками. Такой пакет валит индекс целиком;
  * файлы прод-кода перечисляет ТОТ ЖЕ индекс, что резолвит типы, — поэтому
    «где искали строку» и «где резолвили тип» разойтись не могут. Следствие,
    которое надо помнить: файл, отсечённый build-тегом, невидим ОБЕИМ формам;
  * Go-имя поля берётся из `pkg/api/**/*.pb.go`, а не выводится самодельной
    камелизацией; сообщение без сгенерированного кода НЕ осмотрено и называется
    отдельной строкой переписи;
  * ПОЛЯ ВНУТРИ `oneof` НЕ ОСМАТРИВАЮТСЯ ВОВСЕ — слепая зона, унаследованная от
    разборщика: `_own_level` вырезает любой `{…}`-блок, поэтому члены `oneof`
    не попадают в поля объемлющего сообщения. Замер на `e5bbd92f` (предикат:
    для каждого `oneof … {` в `proto/kacho/cloud/*/v1/*.proto` посчитаны
    объявления полей внутри блока, владелец — самое узкое объемлющее
    `message`): таких полей 62, из них 37 — в сообщениях, которые предикат
    раскрывает как публичный запрос (`CreateInstanceRequest.spec`,
    `Target.identity`, `VipSource.source`, `HealthCheck.options` и др.). Это
    ЗНАЧИТ, что «осмотрено полей: N» — про поля вне `oneof`, и закрытие этой
    зоны — отдельная работа, а не побочный эффект правки основы (issue #110
    менял АТРИБУЦИЮ читателя, а не состав осматриваемых полей);
  * у домена найдено хоть одно дерево прод-кода (имя proto-домена не равно имени
    каталога сервиса — см. DOMAIN_SERVICE_DIR).

ОТСУТСТВИЕ ЧИТАТЕЛЯ — НЕ ВСЕГДА НАХОДКА, И ДВЕ ПРИЧИНЫ ВЫЧИСЛЯЮТСЯ ПО ДЕРЕВУ, а не
предполагаются (иначе перепись выдаётся за вердикт):

  * `RPC-НЕ-РЕАЛИЗОВАН` — у типа запроса нет обработчика, RPC отвечает
    `Unimplemented`. Внутри такого запроса поля не «приняты и выброшены» — они не
    приняты вовсе. Сообщение извиняется только если его НЕ достаёт ни один
    реализованный RPC (см. `excused_sets`).
  * `ПРЕДОК-ОТВЕРГАЕТСЯ` / `ОТВЕРГАЕТСЯ-ПО-ИМЕНИ` — сервис отвергает поле явно, с
    именем поля, синхронно (это исход №2 правила). Всё, что достижимо ТОЛЬКО через
    отвергнутое поле, недостижимо по построению.

Остаётся «полей БЕЗ читателя» — вот это вердикт, и он требует одного из трёх
исходов правила по каждому полю.

ПЕРЕПИСЬ — ОТДЕЛЬНОЕ УТВЕРЖДЕНИЕ. Печатаются: сколько proto-файлов прочитано,
сколько публичных сервисов и RPC найдено, сколько пакетов и файлов прод-кода
протипизировано, сколько сообщений раскрыто, сколько полей осмотрено и сколько
сообщений осталось без сгенерированного кода. Пустой обход — выход с кодом 2, а
не «чисто».

Запуск из корня репозитория:

    python3 tools/unreadfieldaudit/unread_field_audit.py [<домен> …]

Без аргументов — все домены `proto/kacho/cloud/*/v1`. Флаг
`--reader-index=<файл>` берёт готовый индекс вместо построения (отладка; в CI не
нужен — предикат строит индекс сам).
"""
import glob
import json
import os
import re
import subprocess
import sys

PROTO_ROOT = "proto/kacho/cloud"
GEN_ROOT = "pkg/api/kacho/cloud"
MODULE = "github.com/PRO-Robotech/kacho"
INDEXER = "./tools/unreadfieldaudit/cmd/proto-field-readers"

# ИМЯ PROTO-ДОМЕНА НЕ РАВНО ИМЕНИ КАТАЛОГА СЕРВИСА, и это не мелочь предпосылки:
# домен `loadbalancer` реализован в `services/nlb`, поэтому вывод пути по имени
# домена давал ПУСТОЕ дерево прод-кода — и предикат объявлял «без читателя» ВСЕ 87
# полей домена, ни одного файла не прочитав. Соответствие задаётся явно, а пустое
# дерево — отказ (см. ниже), а не тихий поток находок.
DOMAIN_SERVICE_DIR = {"loadbalancer": "nlb"}


# Деревья прод-кода, где законно живёт читатель поля запроса: сам сервис, край
# (grpc-gateway + authz-middleware читают поля запроса рефлексией по имени) и
# общий repo-root `internal/`. Здесь — в виде префиксов ПАКЕТОВ: файлы приходят
# из индекса, сгруппированные по пакету, поэтому дерево выбирается по пакету, а
# не по пути.
def prod_pkg_prefixes(domain):
    svc = DOMAIN_SERVICE_DIR.get(domain, domain)
    return (f"{MODULE}/services/{svc}", f"{MODULE}/gateway", f"{MODULE}/internal")


def prod_trees(domain):
    svc = DOMAIN_SERVICE_DIR.get(domain, domain)
    return [f"services/{svc}", "gateway", "internal"]


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


def build_reader_index(path=None):
    """Индекс чтений, атрибутированный ТИПОМ ПОЛУЧАТЕЛЯ.

    Строится Go-командой `proto-field-readers` (она же перечисляет файлы прод-кода,
    которые протипизировала). Отказ команды — ОТКАЗ ПРЕДИКАТА, а не повод
    вернуться к поиску по имени: тихий откат к слабой основе выглядел бы как
    зелёный обход и был бы ровно тем «мягким проходом при отказе», который правила
    запрещают.
    """
    if path:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    proc = subprocess.run(["go", "run", INDEXER], capture_output=True, text=True)
    if proc.returncode != 0:
        raise RuntimeError(
            f"индекс чтений не построен (go run {INDEXER} → {proc.returncode}). "
            f"stderr:\n{proc.stderr.strip()}")
    return json.loads(proc.stdout)


_FILE_CACHE = {}


def prod_sources(index, domain):
    """Список (path, text) файлов прод-кода домена — ИЗ ИНДЕКСА, не своим обходом.

    Один обход на оба вопроса («где резолвился тип» и «где искалась строка») —
    иначе они разойдутся молча, и разойдутся там, где расхождение не видно.
    """
    prefixes = prod_pkg_prefixes(domain)
    files = []
    for pkg in index["packages"]:
        p = pkg["path"]
        if not any(p == pref or p.startswith(pref + "/") for pref in prefixes):
            continue
        for rel in pkg["files"]:
            if rel not in _FILE_CACHE:
                try:
                    _FILE_CACHE[rel] = open(rel, encoding="utf-8").read()
                except OSError:
                    continue
            files.append((rel, _FILE_CACHE[rel]))
    return files


def typed_readers(index, domain):
    """{(Сообщение, ПолеGo)} — прочитано у ЭТОГО сообщения в деревьях домена."""
    stub = f"{MODULE}/{GEN_ROOT}/{domain}/v1|"
    prefixes = prod_pkg_prefixes(domain)
    out = set()
    for key, readers in index["reads"].items():
        if not key.startswith(stub):
            continue
        if not any(r == pref or r.startswith(pref + "/")
                   for r in readers for pref in prefixes):
            continue
        msg, _sep, field = key[len(stub):].partition("|")
        out.add((msg, field))
    return out


def has_handler(request_msg, sources):
    """Есть ли в прод-коде сигнатура обработчика, ПРИНИМАЮЩАЯ этот тип запроса?

    Форма — `context.Context, req *<pkg>v1.<Request>)`: именно так объявлен каждый
    handler/use-case-вход этого дерева. Тип-ассершен в карте прав
    (`req.(*vpcv1.XRequest)`) сюда НЕ попадает намеренно: authz резолвит область по
    чужому полю и о чтении остальных полей ничего не говорит.

    ЗАЧЕМ. RPC, у которого обработчика нет, отвечает `Unimplemented` — его запрос не
    читает НИКТО и не может: поля внутри такого запроса не «приняты и выброшены»,
    они не приняты вовсе. Считать их находками того же класса — врать в обе стороны:
    завышать число и прятать за ним настоящие.
    """
    needle = "context.Context, req *"
    tail = f".{request_msg})"
    for _p, text in sources:
        i = 0
        while True:
            i = text.find(needle, i)
            if i < 0:
                break
            seg = text[i:i + len(needle) + 80]
            if tail in seg:
                return True
            i += len(needle)
    return False


# Путь поля, а не только имя: дерево называет вложенное поле точкой
# (`network_interface_specs.primary_v4_address_spec`), и предикат, знавший лишь
# односегментное имя, такой отказ НЕ видел — четыре отвергнутых поля остались бы
# «находками». Берётся ПОСЛЕДНИЙ сегмент: он и есть имя поля в его сообщении.
RE_REFUSED = re.compile(r'(?:AddFieldViolation|add)\(\s*"([a-z0-9_.]+)"')


def refused_fields(sources):
    """Имена полей, которые сервис ОТВЕРГАЕТ ЯВНО (второй законный исход).

    Форма — обращение к билдеру нарушений с именем поля строкой
    (`AddFieldViolation("serial_port_settings", …)` и его локальная обёртка
    `add("filesystem_specs", …)`). compute уже пользуется ровно этим приёмом.

    ЗАЧЕМ. Отвергнутое по имени поле НЕ обязано иметь читателя: вызывающий получает
    синхронный отказ с именем поля, то есть ровно то, что правило требует как исход
    №2. И всё, что достижимо ТОЛЬКО через такое поле, недостижимо по построению —
    писать читателя для внутренностей отвергнутого сообщения незачем.

    Предпосылка (её надо пересверять): распознаётся форма, а не смысл. Если сервис
    начнёт отвергать поля иначе, этот список опустеет, и находки вернутся — что
    правильнее молчаливого пропуска.
    """
    out = set()
    for _p, text in sources:
        for m in RE_REFUSED.finditer(text):
            out.add(m.group(1).split(".")[-1])
    return out


def excused_sets(messages, roots_impl, roots_unimpl, refused):
    """Кого нельзя мерить читателем: (по нереализованному RPC, по отвергнутому предку).

    Возвращает два множества имён сообщений.
    """
    def reach_from(rootset):
        seen, queue = set(), list(rootset)
        while queue:
            name = queue.pop()
            if name in seen or name not in messages:
                continue
            seen.add(name)
            for _fn, typ, _rep in messages[name]:
                if typ and typ not in SCALARS and typ in messages:
                    queue.append(typ)
        return seen

    from_impl = reach_from(roots_impl)
    from_unimpl = reach_from(roots_unimpl)
    # Сообщение извиняется нереализованностью только если его НЕ достаёт ни один
    # реализованный RPC: иначе оно живое, и находка в нём настоящая.
    by_unimpl = {m for m in from_unimpl if m not in from_impl}

    # Входящие рёбра: parent -> (field, child)
    incoming = {}
    for parent, fields in messages.items():
        for fname, typ, _rep in fields:
            if typ and typ in messages:
                incoming.setdefault(typ, []).append((parent, fname))

    all_roots = set(roots_impl) | set(roots_unimpl)
    unimpl_roots = set(roots_unimpl)

    # РЕБРО ЗАКРЫТО, если по нему нельзя доехать с приёмом: поле отвергнуто по имени,
    # либо родитель уже недостижим (отвергнут или живёт только под нереализованным
    # RPC), либо родитель САМ — корень нереализованного RPC.
    #
    # Последний случай пришлось добавить отдельно, и без него мерка врала: спека NAT
    # достижима двумя дверями — из создания машины (через отвергнутую спеку адреса) и
    # из нереализованного RPC добавления NAT. Первая закрыта отказом, вторая — тем, что
    # обработчика нет; но проверка «все входящие рёбра закрыты» вторую дверь закрытой
    # не считала, и всё DNS-поддерево под ней оставалось «находками», хотя доехать до
    # него нечем ни одним путём.
    def edge_closed(parent, fname, by_refusal):
        return fname in refused or parent in by_refusal or parent in by_unimpl \
            or parent in unimpl_roots

    by_refusal = set()
    changed = True
    while changed:
        changed = False
        for msg in messages:
            if msg in by_refusal or msg in all_roots:
                continue
            edges = incoming.get(msg, [])
            if not edges:
                continue
            if all(edge_closed(parent, fname, by_refusal) for parent, fname in edges):
                by_refusal.add(msg)
                changed = True
    return by_unimpl, by_refusal


def homonym_names(messages):
    """Имена полей, объявленные более чем в одном сообщении домена.

    ОСТАЛОСЬ ОБЪЯВЛЕНИЕМ СЛАБОСТИ, НО ТЕПЕРЬ УЗКИМ. Сильная основа (чтение у этого
    сообщения) омонимией не задевается вовсе. Пометка нужна ровно для второй,
    слабой формы — имени поля строкой: у строки нет получателя, приписать её
    конкретному сообщению нечем, и если имя объявлено не в одном сообщении домена,
    читатель мог принадлежать другому.
    """
    seen, dup = {}, set()
    for msg, fields in messages.items():
        for fname, _typ, _rep in fields:
            if fname in seen and seen[fname] != msg:
                dup.add(fname)
            seen[fname] = msg
    return dup


def main():
    args = sys.argv[1:]
    index_path = None
    for a in args:
        if a.startswith("--reader-index="):
            index_path = a.split("=", 1)[1]
    domains = [a for a in args if not a.startswith("-")] or [
        os.path.basename(os.path.dirname(d))
        for d in sorted(glob.glob(os.path.join(PROTO_ROOT, "*", "v1")))]

    try:
        index = build_reader_index(index_path)
    except (RuntimeError, OSError, ValueError) as e:
        print(f"ПРЕДПОСЫЛКА НЕ ВЫПОЛНЕНА: {e}", file=sys.stderr)
        return 2
    if index.get("errors"):
        for e in index["errors"]:
            print(f"ПРЕДПОСЫЛКА НЕ ВЫПОЛНЕНА (пакет не протипизирован, его чтения "
                  f"невидимы): {e}", file=sys.stderr)
        return 2
    idx_pkgs = index.get("packages") or []
    idx_files = sum(len(p["files"]) for p in idx_pkgs)
    if not idx_pkgs or idx_files == 0:
        print("ПРЕДПОСЫЛКА НЕ ВЫПОЛНЕНА: индекс чтений пуст — прод-код не прочитан",
              file=sys.stderr)
        return 2

    protos_read = svc_public = rpc_public = 0
    msgs_expanded = fields_seen = 0
    closed_typed = 0
    without_gen = []
    premise_failures = []
    findings = []
    buckets = {"RPC-НЕ-РЕАЛИЗОВАН": [], "ПРЕДОК-ОТВЕРГАЕТСЯ": [],
               "ОТВЕРГАЕТСЯ-ПО-ИМЕНИ": []}
    by_name_only = []
    unimpl_rpcs = {}

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
        sources = prod_sources(index, domain)
        if not sources:
            # ПРЕДПОСЫЛКА НЕ ВЫПОЛНЕНА: дерево прод-кода этого домена не найдено, а
            # значит «читателя нет» здесь означает «читать было негде». Молчать нельзя
            # и находки этого домена печатать нельзя — они были бы утверждением о
            # непрочитанном.
            premise_failures.append(domain)
            continue
        typed = typed_readers(index, domain)
        # Две причины отсутствия читателя, которые находкой НЕ являются, —
        # вычисляются по дереву, а не предполагаются (см. godoc обеих функций).
        roots_impl = [r for r in set(roots) if has_handler(r, sources)]
        roots_unimpl = [r for r in set(roots) if r not in roots_impl]
        refused = refused_fields(sources)
        by_unimpl, by_refusal = excused_sets(messages, roots_impl, roots_unimpl, refused)
        homonyms = homonym_names(messages)
        unimpl_rpcs[domain] = sorted(roots_unimpl)

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
                if (name, go) in typed:
                    # Сильная основа: чтение у ЭТОГО сообщения, тип получателя
                    # резолвлен. Омонимия ничего здесь не закрывает.
                    closed_typed += 1
                    continue
                if _has_string_reader(fname, sources):
                    # Слабая основа: рефлексивный читатель по имени. Приписать его
                    # сообщению нечем — поле называется поимённо, а не прячется.
                    by_name_only.append(
                        (domain, name, fname, go, fname in homonyms))
                    continue
                if name in by_unimpl:
                    buckets["RPC-НЕ-РЕАЛИЗОВАН"].append((domain, name, fname, go))
                elif name in by_refusal:
                    buckets["ПРЕДОК-ОТВЕРГАЕТСЯ"].append((domain, name, fname, go))
                elif fname in refused:
                    buckets["ОТВЕРГАЕТСЯ-ПО-ИМЕНИ"].append((domain, name, fname, go))
                else:
                    findings.append((domain, name, fname, go))

    if protos_read == 0:
        print("НИЧЕГО НЕ ПРОЧИТАНО — предикат ничего не утверждает", file=sys.stderr)
        return 2

    print(f"прочитано .proto: {protos_read}; публичных сервисов: {svc_public}; "
          f"публичных RPC: {rpc_public}")
    print(f"протипизировано пакетов прод-кода: {len(idx_pkgs)}; файлов: {idx_files} "
          f"(пакетов без непроверочных файлов: "
          f"{len(index.get('skipped_no_prod_files') or [])})")
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
    print(f"закрыто ЧТЕНИЕМ У ЭТОГО СООБЩЕНИЯ (тип получателя резолвлен): "
          f"{closed_typed} из {fields_seen}")
    for label in ("RPC-НЕ-РЕАЛИЗОВАН", "ПРЕДОК-ОТВЕРГАЕТСЯ", "ОТВЕРГАЕТСЯ-ПО-ИМЕНИ"):
        rows = buckets[label]
        print(f"НЕ находка ({label}): {len(rows)}")
        for domain, msg, fname, go in rows:
            print(f"    {domain}: {msg}.{fname}  (Go: {go})")
    for domain, rpcs in sorted(unimpl_rpcs.items()):
        if rpcs:
            print(f"    у домена {domain} без обработчика: {', '.join(rpcs)}")
    # СПИСОК, А НЕ ТОЛЬКО ЧИСЛО — И ЭТО СТАЛО ВОЗМОЖНЫМ ПОСЛЕ СМЕНЫ ОСНОВЫ. Прежняя
    # слабая корзина держала 755 полей из 826: перечисление, помечающее девять
    # десятых, не помечает ничего, поэтому печаталось одно число. Теперь слабой
    # остаётся только форма «имя поля строкой», и её можно назвать поимённо —
    # объявление слабости, которое можно взять и проверить глазами.
    weak = sum(1 for *_x, homo in by_name_only if homo)
    print(f"закрыто ТОЛЬКО ИМЕНЕМ-СТРОКОЙ (рефлексивный читатель): "
          f"{len(by_name_only)} из {fields_seen}")
    # СТРОКА-ПРЕЕМНИЦА прежней «закрыто ИМЕНЕМ-ОМОНИМОМ: 755 из 826». Она и есть
    # объявление слабости мерки числом: закрыто там, где приписать читателя
    # сообщению НЕЧЕМ (у строкового литерала нет получателя) И имя объявлено не в
    # одном сообщении домена. Порог на неё в CI сужается, не растёт.
    print(f"закрыто СЛАБОЙ АТРИБУЦИЕЙ (имя-строка + имя объявлено не в одном "
          f"сообщении домена): {weak} из {fields_seen}")
    for domain, msg, fname, go, homo in by_name_only:
        mark = "имя-омоним" if homo else "имя уникально в домене"
        print(f"    {domain}: {msg}.{fname}  (Go: {go}; {mark})")
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


def _has_string_reader(snake, sources):
    """Рефлексивный читатель: имя поля строковым литералом в прод-коде."""
    quoted = f'"{snake}"'
    for _p, text in sources:
        if quoted in text:
            return True
    return False


if __name__ == "__main__":
    sys.exit(main())
