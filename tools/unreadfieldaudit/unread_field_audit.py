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

ЧТО СЧИТАЕТСЯ ЧИТАТЕЛЕМ — ТРИ ОСНОВЫ, И ОНИ РАЗНОЙ СИЛЫ.

  1. ЧТЕНИЕ У ЭТОГО СООБЩЕНИЯ (сильная, основная). Вызов геттера `x.GetFoo()` или
     обращение к полю `x.Foo`, где ТИП `x` — именно это сообщение. Тип резолвится
     `go/types`; индекс строит `tools/unreadfieldaudit/cmd/proto-field-readers`,
     этот предикат его запускает сам. Совпадение имён здесь не закрывает ничего
     by construction. Сюда же входят ДВЕ формы чтения члена `oneof`, у которых
     получателем выступает тип-обёртка `<Родитель>_<Поле>`: обращение к её
     единственному полю (`v.Tcp`) и РАЗЛИЧЕНИЕ ветки по типу
     (`switch x.(type)` / `x.(*T)`) — для члена с пустой полезной нагрузкой
     (`message X {}`) второе есть единственно возможное чтение. Конструирование
     ветки (`&pb.M_Foo{…}` на пути ответа) чтением НЕ считается.
  2. КАТАЛОГ ПРАВ НАЗЫВАЕТ ПОЛЕ У ЭТОГО МЕТОДА (сильная). Край читает поле
     запроса РЕФЛЕКСИЕЙ ПО ИМЕНИ, взяв имя из `scope_extractor.from_request_field`
     либо `object_type_from_request_field`
     (`gateway/internal/middleware/resource_extractor.go`, `ExtractFromProto` →
     `extractByProtoReflect`): им выбирается объект, на котором стоит проверка
     доступа. Основа СИЛЬНАЯ, и причина ровно одна — у записи каталога ЕСТЬ
     получатель: она ключуется FQN метода, а тип запроса метода объявлен в том же
     .proto. Совпадение имени поля с полем соседнего сообщения закрыть ничего не
     может by construction. Составной путь (`address.project_id`) резолвится по
     звеньям — так же, как его резолвит сам край.
  3. ИМЯ ПОЛЯ СТРОКОЙ (слабая). `"<snake>"` в прод-коде: known-set маски
     обновления, whitelist фильтра, перечень неизменяемых полей — сервис читает
     поле РЕФЛЕКСИЕЙ ПО ИМЕНИ, и это настоящий читатель. Но у строки нет
     получателя, поэтому приписать её конкретному сообщению нечем: если такое имя
     объявлено не в одном сообщении домена, читатель мог принадлежать другому.
     Поля, закрытые ТОЛЬКО этой формой, печатаются поимённо, а не прячутся в
     «закрыто». Сюда же попадает явный отказ по имени (`AddFieldViolation`):
     он такой же именной, значит и слабость у него та же — отдельной корзины у
     него нет намеренно, она была бы пуста всегда by construction.

ПОЧЕМУ ЗАВЕДЕНА ОСНОВА №2 (issue #2115). Каталог не читался предикатом ВООБЩЕ:
строковый поиск ходит по `.go`-файлам индекса, а каталог лежит рядом JSON'ом, и
резолв типа его не видит тем более. Цена незнания этой формы — два разных исхода,
и оба тихие: поле, чей ЕДИНСТВЕННЫЙ читатель каталог, стало бы ЛОЖНОЙ НАХОДКОЙ;
поле, чьё имя вдобавок встречается строкой у соседнего сообщения, уезжало в слабую
корзину, то есть измерялось совпадением имён. Второе и наблюдалось:
`BatchAuthorizeCheckRequest.scope_id` закрывался строками `"scope_id"`, которые
объявляли СОСЕДНИЕ сообщения домена (маска правки привязки, неизменяемые поля
предела), — при том что настоящий его читатель край, и промах атрибуции ничем не
проявлялся. Замер: закрыто каталогом 1, слабая корзина 2 → 1.

ЗАПИСЬ КАТАЛОГА, ЧЬЁ ИМЯ ПОЛЯ У ЗАПРОСА НЕ ОБЪЯВЛЕНО, НАЗЫВАЕТСЯ ОТДЕЛЬНО. Имя в
опции proto пишется РУКОЙ; поле переименовали, опцию забыли — край читает пустоту,
`ExtractFromProto` отдаёт подстановочный знак, и проверка доступа уходит на
`<тип>:*` вместо названного объекта. Проглотить такую запись значило бы измерить её
как «поле закрыто», то есть выдать несуществующего читателя за существующего.

ПОЧЕМУ ОСНОВА СМЕНИЛАСЬ (issue kacho#110). Прежде все формы искали ИМЯ, и имя
между сообщениями одного домена не уникально (`filter`, `name`, `labels`,
`page_size`). Замер на `e5bbd92f`: из 826 осмотренных полей 755 закрывались
именем-омонимом — девять десятых вердикта держались на совпадении имён, то есть
«полей без читателя: 0» опиралось на атрибуцию, которая могла указывать на другое
сообщение. Так предикат пропускал `NetworkInterfaceSpec.nic_id` и `.index` (оба
найдены глазами, оба оказались дефектами).

ПОЧЕМУ СОСТАВ ОСМАТРИВАЕМОГО ВЫРОС (тот же issue, вторая половина). Сменить
атрибуцию мало: корзина, куда поле не попадает ВООБЩЕ, порогом не сторожится.
Слепых зон было три, все закрыты, замер на `982b0849` — 826 → 905 полей и 2 → 0
сообщений без сгенерированного кода:

  * поля внутри `oneof` не осматривались вовсе (разборщик вырезал блок наравне с
    любым `{…}`) — 62 таких поля в дереве;
  * вложенное сообщение ключевалось коротким именем и потому не сходилось ни с
    индексом чтений, ни с геттерами (Go-имя — `Parent_Nested`);
  * `message X {}` объявлялось «нет .pb.go» по отсутствию геттеров, которых у
    пустого сообщения нет by construction.

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
  * сообщения ключуются Go-ИМЕНЕМ ТИПА (вложенное — `Parent_Nested`), тем же,
    каким их называют индекс чтений и сгенерированные геттеры. Пока ключом было
    короткое имя, вложенное сообщение не сходилось ни с тем, ни с другим;
  * у домена найдено хоть одно дерево прод-кода И НЕПУСТО ЕГО СОБСТВЕННОЕ.
    Проверяются оба, и второе — потому, что первое о пропаже дерева службы не
    спрашивает: `prod_sources` берёт ещё край и общий `internal/`, поэтому
    остаётся непустым, пока жив край. Имя proto-домена не равно имени каталога
    сервиса — см. DOMAIN_SERVICE_DIR;
  * КАТАЛОГ ПРАВ ПРОЧИТАН И НЕПУСТ. Пустой либо нечитаемый каталог — отказ
    (rc=2), а не «каталог о полях не говорит»: молчаливая пустота вернула бы
    прежнее состояние, и отличить «основа не сработала» от «каталог их не
    называет» было бы нечем;
  * КАЖДЫЙ обойденный Go-модуль дал хоть один пакет, и объём осмотренного
    печатается ПО МОДУЛЯМ ПОРОЗНЬ. Дерево несёт больше одного модуля, а `go list`
    в каталог со своим `go.mod` не спускается by construction: пока перепись знала
    одно суммарное число, обход одного дерева был неотличим от обхода обоих.
    Замер, из которого выведено (issue #2093): после выноса службы в модуль
    `github.com/PRO-Robotech/kaname` её 136 пакетов стали НЕВИДИМЫ — не
    «непрочитанными» и не «находками», а ничем, — и вместе с ними исчезли
    обработчики её типов запроса, отчего все 268 полей домена уехали в полосу
    `RPC-НЕ-РЕАЛИЗОВАН`. Со стороны это выглядело исправным деревом.

ОТСУТСТВИЕ ЧИТАТЕЛЯ — НЕ ВСЕГДА НАХОДКА, И ДВЕ ПРИЧИНЫ ВЫЧИСЛЯЮТСЯ ПО ДЕРЕВУ, а не
предполагаются (иначе перепись выдаётся за вердикт):

  * `RPC-НЕ-РЕАЛИЗОВАН` — у типа запроса нет обработчика, RPC отвечает
    `Unimplemented`. Внутри такого запроса поля не «приняты и выброшены» — они не
    приняты вовсе. Сообщение извиняется только если его НЕ достаёт ни один
    реализованный RPC (см. `excused_sets`).
  * `ПРЕДОК-ОТВЕРГАЕТСЯ` — сервис отвергает поле-родителя явно, с именем поля,
    синхронно (это исход №2 правила). Всё, что достижимо ТОЛЬКО через отвергнутое
    поле, недостижимо по построению.

Обе причины вычисляются по ДЕРЕВУ и от атрибуции по имени не зависят, поэтому
проверяются РАНЬШЕ слабой корзины: иначе поле нереализованного RPC, чьё имя
где-то встречается строкой, объявлялось бы «закрытым слабой атрибуцией» — то есть
слабость мерки выписывалась бы там, где мерка не применялась.

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
нужен — предикат строит индекс сам). Флаг `--self-test` — инъекция в обе стороны
на разборщике и на атрибуции чтения (см. `self_test`).
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
PERMISSION_CATALOG = "gateway/internal/middleware/embed/permission_catalog.json"

# ИМЯ PROTO-ДОМЕНА НЕ РАВНО ИМЕНИ КАТАЛОГА СЕРВИСА, и это не мелочь предпосылки:
# домен `loadbalancer` реализован в `services/nlb`, поэтому вывод пути по имени
# домена давал ПУСТОЕ дерево прод-кода — и предикат объявлял «без читателя» ВСЕ 87
# полей домена, ни одного файла не прочитав. Соответствие задаётся явно, а пустое
# дерево — отказ (см. ниже), а не тихий поток находок.
#
# `quota` — второй экземпляр того же класса, найденный при укреплении предпосылки
# (2026-09-06): публичный `IdentityQuotaService` объявлен в домене `quota`, а
# реализован в `services/iam` (`internal/apps/kaname/api/identityquota`). Каталога
# `services/quota` в дереве нет, поэтому прод-дерево домена выводилось пустым, и
# домен мерился одним лишь читателем края. Сегодня у него ноль полей (запрос пуст
# by design — см. его контракт), поэтому цена нулевая, а мерка была неверна.
DOMAIN_SERVICE_DIR = {"loadbalancer": "nlb", "quota": "iam"}

# Объявление модуля в go.mod — по нему выводится путь пакетов вынесенного сервиса.
RE_GO_MODULE = re.compile(r"^module\s+(\S+)", re.M)


def service_module_path(svc_dir):
    """Путь Go-модуля службы, если она вынесена в СВОЙ модуль; иначе None.

    ЗАЧЕМ. Служба, вынесенная в собственный модуль, живёт под ЕГО путём пакетов, а
    не под `<корневой модуль>/services/<имя>`. Пока префикс выводился из корневого
    модуля, читатели вынесенной службы были ПРОЧИТАНЫ индексом и не приписаны
    никому: перепись выросла вдвое, а вердикт не сдвинулся ни на одно поле — то
    есть прибавка целиком легла в слепую зону.

    Выводится из дерева, а не выписывается: перечень вынесенных служб, записанный
    руками, разошёлся бы с деревом молча — ровно так эта слепота и завелась.
    """
    try:
        with open(os.path.join(svc_dir, "go.mod"), encoding="utf-8") as f:
            body = f.read()
    except OSError:
        return None
    m = RE_GO_MODULE.search(body)
    return m.group(1) if m else None


# Деревья прод-кода, где законно живёт читатель поля запроса: сам сервис, край
# (grpc-gateway + authz-middleware читают поля запроса рефлексией по имени) и
# общий repo-root `internal/`. Здесь — в виде префиксов ПАКЕТОВ: файлы приходят
# из индекса, сгруппированные по пакету, поэтому дерево выбирается по пакету, а
# не по пути.
def prod_pkg_prefixes(domain):
    svc = DOMAIN_SERVICE_DIR.get(domain, domain)
    own = service_module_path(f"services/{svc}")
    return (own or f"{MODULE}/services/{svc}",
            f"{MODULE}/gateway", f"{MODULE}/internal")


def prod_trees(domain):
    svc = DOMAIN_SERVICE_DIR.get(domain, domain)
    return [f"services/{svc}", "gateway", "internal"]


RE_PACKAGE = re.compile(r"^\s*package\s+([\w.]+)\s*;", re.M)
RE_SERVICE = re.compile(r"^service\s+(\w+)\s*\{", re.M)
RE_RPC = re.compile(r"\brpc\s+(\w+)\s*\(\s*([\w.]+)\s*\)")
# message-объявление верхнего или вложенного уровня.
RE_MESSAGE = re.compile(r"^(\s*)message\s+(\w+)\s*\{", re.M)
RE_FIELD = re.compile(
    r"^\s*(?:(repeated|optional)\s+)?([\w.]+)\s+(\w+)\s*=\s*(\d+)\s*[;\[]", re.M)
RE_GETTER = re.compile(r"^func \(x \*(\w+)\) Get(\w+)\(\)", re.M)
# Заголовок `oneof <имя>` непосредственно перед `{` — см. _own_level.
RE_ONEOF_HEAD = re.compile(r"\boneof\s+\w+\s*$")
# Объявление Go-структуры в сгенерированном коде — см. load_getters.
RE_STRUCT = re.compile(r"^type (\w+) struct \{", re.M)
RE_MAP = re.compile(r"^\s*map<[^>]*>\s+(\w+)\s*=", re.M)

SCALARS = {
    "double", "float", "int32", "int64", "uint32", "uint64", "sint32", "sint64",
    "fixed32", "fixed64", "sfixed32", "sfixed64", "bool", "string", "bytes",
}


def strip_comments(s):
    out = re.sub(r"/\*.*?\*/", "", s, flags=re.S)
    return re.sub(r"//[^\n]*", "", out)


def proto_package(path):
    """Объявленный `package` файла — левая часть FQN метода в каталоге прав.

    Читается ИЗ ФАЙЛА, а не выводится из каталога на диске: `kacho.cloud.<домен>.v1`
    верно для сегодняшней раскладки и перестанет быть верным у первого же пакета
    без сегмента версии (`kacho.cloud.subscription` уже такой). Вывод по имени
    каталога дал бы FQN, который не совпадёт с каталожным НИ ОДНОЙ записью, и
    основа закрылась бы молча — ноль закрытых был бы неотличим от «каталог о них
    не говорит».
    """
    m = RE_PACKAGE.search(strip_comments(open(path, encoding="utf-8").read()))
    return m.group(1) if m else ""


def parse_proto(path):
    """Вернуть (services, messages, scopes) одного .proto.

    services: {ServiceName: [(rpc, request_type)]}
    messages: {GoИмя: [(field_name, сырой_тип, repeated)]}
    scopes:   {GoИмя: [GoИмя, родитель, …, ""]} — цепочка областей видимости
              proto, от самой узкой к глобальной; по ней резолвятся типы полей.

    КЛЮЧ — Go-ИМЯ ТИПА, А НЕ КОРОТКОЕ ИМЯ message'а, и это не косметика.
    Вложенный `message Nested` внутри `message Parent` порождает Go-тип
    `Parent_Nested`; индекс чтений (`protofieldreaders`) ключуется именно
    Go-типом, и сгенерированные геттеры объявлены на нём же. Пока ключом было
    короткое имя, вложенное сообщение не сходилось ни с индексом, ни с
    геттерами — оно объявлялось «нет .pb.go» и его поля НЕ ОСМАТРИВАЛИСЬ вовсе
    (замер на 982b0849: 6 таких сообщений домена loadbalancer, достижимых через
    `oneof`). Теперь ключ строится по вложенности и совпадает с Go-именем
    побайтово, поэтому сходиться ему больше не с чем расходиться.
    """
    body = strip_comments(open(path, encoding="utf-8").read())
    services = {}
    for m in RE_SERVICE.finditer(body):
        name = m.group(1)
        block = _balanced(body, m.end() - 1)
        services[name] = [(r.group(1), r.group(2)) for r in RE_RPC.finditer(block)]

    spans = []
    for m in RE_MESSAGE.finditer(body):
        o = m.end() - 1
        spans.append((m.group(2), o, _closing(body, o)))
    spans.sort(key=lambda s: s[1])

    messages, scopes = {}, {}
    stack = []  # [(GoИмя, индекс закрывающей скобки)]
    for name, o, c in spans:
        while stack and o > stack[-1][1]:
            stack.pop()
        qual = f"{stack[-1][0]}_{name}" if stack else name
        scopes[qual] = [q for q, _c in reversed(stack)] + [""]
        scopes[qual].insert(0, qual)
        stack.append((qual, c))
        own = _own_level(body[o + 1:c])
        fields = []
        for f in RE_FIELD.finditer(own):
            label, typ, fname, _num = f.group(1), f.group(2), f.group(3), f.group(4)
            if fname in ("reserved", "option", "returns"):
                continue
            fields.append((fname, typ, label == "repeated"))
        for f in RE_MAP.finditer(own):
            fields.append((f.group(1), "", False))
        messages[qual] = fields
    return services, messages, scopes


def resolve_types(messages, scopes):
    """Заменить сырые имена типов полей на ключи `messages` (Go-имена).

    Правило разрешения — proto: имя ищется от самой узкой области видимости к
    глобальной, а точки в имени соответствуют вложенности. Не наш message
    (скаляр, `google.protobuf.*`, тип чужого домена) остаётся как есть и просто
    не найдётся в `messages` — обход достижимости туда не пойдёт, как и раньше.
    """
    out = {}
    for qual, fields in messages.items():
        chain = scopes.get(qual, [qual, ""])
        resolved = []
        for fname, typ, rep in fields:
            resolved.append((fname, _resolve_type(typ, chain, messages), rep))
        out[qual] = resolved
    return out


def _resolve_type(typ, chain, messages):
    if not typ or typ in SCALARS:
        return typ
    parts = typ.split(".")
    for start in range(len(parts)):
        cand_tail = "_".join(parts[start:])
        for scope in chain:
            cand = f"{scope}_{cand_tail}" if scope else cand_tail
            if cand in messages:
                return cand
    return parts[-1]


def _closing(body, open_brace_idx):
    """Индекс `}`, парной к `{` по указанному индексу."""
    depth, i, n = 0, open_brace_idx, len(body)
    while i < n:
        if body[i] == "{":
            depth += 1
        elif body[i] == "}":
            depth -= 1
            if depth == 0:
                return i
        i += 1
    return n


def _balanced(body, open_brace_idx):
    """Текст от открывающей `{` до её пары."""
    return body[open_brace_idx + 1:_closing(body, open_brace_idx)]


def _own_level(block):
    """Убрать вложенные `{…}`-блоки (их поля разбираются своим RE_MESSAGE-проходом),
    но СОХРАНИТЬ `[...]`-опции полей: поле объявляется `... = 7 [(validate)…];`, и
    вырезание квадратных скобок ничего не ломает, а вот вырезание фигурных —
    обязательно, иначе поля вложенного message'а достанутся объемлющему.

    `oneof <имя> { … }` — ИСКЛЮЧЕНИЕ, И ЭТО НЕ ПОСЛАБЛЕНИЕ, А ФОРМА ДЕРЕВА. Он не
    отдельное сообщение, а группировка полей ОДНОГО сообщения: в сгенерированном
    Go геттер каждого члена стоит на РОДИТЕЛЕ (`func (x *Target) GetNicId()`), и
    клиент присылает член в теле родителя. Прежняя редакция резала его наравне с
    вложенным `message`, поэтому члены `oneof` не попадали В СОСТАВ полей вовсе —
    ни в находки, ни в корзины «не находка», ни в перепись: они исчезали до
    вердикта, а «осмотрено полей: N» читалось как полный обход (issue kacho#110,
    замер на 982b0849 — 62 таких поля в дереве, из них в раскрытой публичной
    поверхности 38).

    Прозрачность НЕ наследуется внутрь непрозрачного: `oneof` внутри вложенного
    `message` остаётся у вложенного (его разберёт свой RE_MESSAGE-проход), иначе
    поля снова достались бы объемлющему — тот самый дефект зеркально.
    """
    out = []
    # Для каждой открытой `{` — своя ли она этому уровню. `all([])` — True, то
    # есть текст вне блоков берётся как прежде.
    stack = []
    for i, ch in enumerate(block):
        if ch == "{":
            head = block[max(0, i - 128):i].rstrip()
            stack.append(bool(RE_ONEOF_HEAD.search(head)) and all(stack))
            continue
        if ch == "}":
            if stack:
                stack.pop()
            continue
        if all(stack):
            out.append(ch)
    return "".join(out)


def load_getters(domain):
    """({GoТип: {GoПоле, …}}, {объявленные GoТипы}) из сгенерированного кода домена.

    ДВА РАЗНЫХ ФАКТА, И ИХ НЕЛЬЗЯ СХЛОПЫВАТЬ. «Геттеров нет» и «типа нет» —
    разные состояния: `message X {}` порождает Go-структуру БЕЗ единого геттера,
    и по одному лишь отсутствию геттеров такое сообщение объявлялось «нет
    .pb.go», то есть попадало в слепую зону, где ему нечего делать: осматривать в
    нём нечего by construction. Три пустых сообщения дерева (`WhoAmIRequest`,
    `ListPermissionCatalogRequest`, `AccessTargetAllInScope`, плюс `PublicVip`,
    `NatGatewaySpec`) держали порог слепой зоны непустым, и порог
    сторожил не то, что называл.
    """
    getters, declared = {}, set()
    for path in sorted(glob.glob(os.path.join(GEN_ROOT, domain, "v1", "*.pb.go"))):
        body = open(path, encoding="utf-8").read()
        for m in RE_GETTER.finditer(body):
            getters.setdefault(m.group(1), set()).add(m.group(2))
        for m in RE_STRUCT.finditer(body):
            declared.add(m.group(1))
    return getters, declared


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


def own_tree_packages(index, domain):
    """Пакеты СОБСТВЕННОГО прод-дерева домена в индексе.

    ОТДЕЛЬНАЯ ПРЕДПОСЫЛКА, А НЕ ПОДМНОЖЕСТВО `prod_sources`, и различие несущее.
    `prod_sources` берёт три дерева — сам сервис, край и общий `internal/`, — и
    непустым оказывается, пока жив хоть край. Значит вопрос «прочитали ли мы
    сервис» она не задаёт ВООБЩЕ: дерево вынесенной службы исчезло из обхода
    целиком, а предпосылка молчала, потому что край на месте.

    Собственное дерево — то место, где читатель поля запроса живёт прежде всего:
    его отсутствие означает, что «читателя нет» здесь читается как «читать было
    негде», и печатать находки нельзя.
    """
    pref = prod_pkg_prefixes(domain)[0]
    return [p["path"] for p in index["packages"]
            if p["path"] == pref or p["path"].startswith(pref + "/")]


def typed_readers(index, domain):
    """{(GoТип, GoПоле)} — прочитано у ЭТОГО сообщения в деревьях домена.

    ЧЛЕН `oneof` ЧИТАЮТ ЧЕРЕЗ ТИП-ОБЁРТКУ, и это ЕГО чтение, а не чужое.
    protoc-gen-go порождает на каждый член `oneof` отдельный тип `M_Foo` с
    единственным полем `Foo`, и канонический разбор в дереве — переключатель по
    типу с последующим `v.Foo` (`services/nlb/.../targetgroup/helpers.go`).
    Получателем такого чтения `go/types` называет обёртку, поэтому без этого
    правила настоящий и единственный читатель члена `oneof` не засчитывался
    сильной основой и поле уезжало в слабую корзину — «имя строкой», где
    приписать читателя нечем.

    Правило точное, а не эвристическое: пара засчитывается родителю ТОЛЬКО когда
    имя типа посимвольно равно `<родитель>_<Поле>` — форма, которую генератор
    производит ровно для члена `oneof`.
    """
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
        wrapper_tail = "_" + field
        if msg.endswith(wrapper_tail) and len(msg) > len(wrapper_tail):
            out.add((msg[:-len(wrapper_tail)], field))
    # ВТОРАЯ ФОРМА ЧТЕНИЯ ЧЛЕНА `oneof` — РАЗЛИЧЕНИЕ ВЕТКИ БЕЗ ОБРАЩЕНИЯ К ПОЛЮ.
    # У члена с пустой полезной нагрузкой (`message X {}`) читать внутри нечего:
    # вся информация — факт выбранной ветки, и прод-код берёт её переключателем по
    # типу либо тип-ассершеном. Индекс различает это от конструирования ветки на
    # пути ответа (см. `protofieldreaders.Index.Discriminated`).
    for key, readers in (index.get("discriminated") or {}).items():
        if not key.startswith(stub):
            continue
        if not any(r == pref or r.startswith(pref + "/")
                   for r in readers for pref in prefixes):
            continue
        typ = key[len(stub):]
        parent, _sep, field = typ.rpartition("_")
        # Go-имя поля подчёркиваний не содержит, поэтому правый сегмент — поле, а
        # всё левое — родитель (в т.ч. вложенный: `A_B_Foo` → родитель `A_B`).
        if parent and field:
            out.add((parent, field))
    return out


def has_handler(request_msg, sources):
    """Есть ли в прод-коде сигнатура обработчика, ПРИНИМАЮЩАЯ этот тип запроса?

    Форма — параметр `*<pkg>.<Request>` сразу за `context.Context`, и ФОРМ ЭТИХ
    ТРИ, а не одна:

      * `ctx context.Context, req *iamv1.GetUserRequest)` — каноническая;
      * `ctx context.Context, _ *iamv1.WhoAmIRequest)` — параметр назван `_`,
        законная форма для запроса без полей;
      * сигнатура, перенесённая на строки: после типа стоит ЗАПЯТАЯ, а не `)`.

    Тип-ассершен в карте прав (`req.(*vpcv1.XRequest)`) сюда НЕ попадает
    намеренно: authz резолвит область по чужому полю и о чтении остальных полей
    ничего не говорит.

    ЗАЧЕМ. RPC, у которого обработчика нет, отвечает `Unimplemented` — его запрос не
    читает НИКТО и не может: поля внутри такого запроса не «приняты и выброшены»,
    они не приняты вовсе. Считать их находками того же класса — врать в обе стороны:
    завышать число и прятать за ним настоящие.

    ПОЧЕМУ ФОРМ СТАЛО ТРИ (issue #2093). Прежняя редакция искала подстроку
    `"context.Context, req *"` и требовала `)` сразу за типом, а шапка утверждала,
    что «именно так объявлен каждый handler/use-case-вход этого дерева». Утверждение
    пережило свой предмет: на 04feea5aa4 четыре типа запроса в пяти доменах
    объявлены двумя другими формами, и каждый объявлялся НЕПРИНЯТЫМ. Сегодня цена
    нулевая — все четыре запроса пусты либо их поля читаются и так, — но полоса
    ложного освобождения была открыта: поле такого запроса без читателя уехало бы
    в `RPC-НЕ-РЕАЛИЗОВАН` при живом обработчике, то есть выглядело бы «не находкой».
    """
    rx = re.compile(r"context\.Context\s*,\s*[A-Za-z_]\w*\s+\*\s*\w+\." +
                    re.escape(request_msg) + r"\s*[,)]")
    for _p, text in sources:
        if rx.search(text):
            return True
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


def load_permission_catalog(path=PERMISSION_CATALOG):
    """Записи каталога прав — ТОТ ЖЕ файл, который читает край.

    ЗАЧЕМ ОН ЗДЕСЬ. Край читает поле запроса РЕФЛЕКСИЕЙ ПО ИМЕНИ, взяв имя из
    `scope_extractor.from_request_field` (`gateway/internal/middleware/resource_extractor.go`,
    `ExtractFromProto` → `extractByProtoReflect`). Это настоящий читатель: им
    выбирается объект, на котором стоит проверка доступа. До этой основы каталог
    не читался ВООБЩЕ — ни одним из двух прежних способов: строковый поиск ходит
    по `.go`-файлам индекса, а каталог лежит рядом JSON'ом.

    ПУСТОЙ ЛИБО НЕЧИТАЕМЫЙ КАТАЛОГ — ОТКАЗ, а не «каталог о полях не говорит».
    Молчаливая пустота вернула бы прежнее состояние (поля уезжают в слабую
    корзину или в находки), и отличить «основа не сработала» от «каталог их не
    называет» было бы нечем — ровно тот класс, ради которого перепись и
    печатается. Каталог порождается из proto (`make permission-catalog`) и
    обязан существовать; его отсутствие означает поломку края, а не факт о полях.
    """
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
    entries = data.get("entries") if isinstance(data, dict) else data
    if not isinstance(entries, list) or not entries:
        raise ValueError(f"каталог прав {path!r} пуст либо не разобран — край "
                         f"читает поля запроса ПО НЕМУ, и без него основа "
                         f"«назван каталогом» не утверждает ничего")
    return entries


def _catalog_field_paths(entry):
    """Пути полей запроса, которые край читает по этой записи каталога.

    Их два: `scope_extractor.from_request_field` (объект проверки) и
    `object_type_from_request_field` (его ТИП — scope-полиморфная форма). Оба
    читаются одной и той же рефлексией, поэтому оба — читатели.

    `"*"` полем не является: это объявленный подстановочный знак, при котором
    край в сообщение не заглядывает вовсе.
    """
    se = entry.get("scope_extractor") or {}
    out = []
    for key in ("from_request_field", "object_type_from_request_field"):
        val = (se.get(key) or "").strip()
        if val and val != "*":
            out.append(val)
    return out


def catalog_readers(entries, package, services, messages):
    """({(GoИмяСообщения, snake-поле)}, [нерезолвящиеся пути]) — по каталогу прав.

    АТРИБУЦИЯ ТОЧНАЯ, И ЭТО ЕДИНСТВЕННАЯ ПРИЧИНА СЧИТАТЬ ОСНОВУ СИЛЬНОЙ. Запись
    каталога ключуется FQN метода (`<пакет>.<Сервис>/<Rpc>`), а тип запроса
    метода объявлен в том же .proto. Значит пара «сообщение × поле» выводится, а
    не угадывается: совпадение имени поля с полем соседнего сообщения закрыть
    ничего не может by construction — ровно как у чтения с резолвленным типом.

    Мерится ТА ЖЕ публичная поверхность, что и предикатом: сервисы, чьё имя
    начинается с `Internal`, пропускаются (ban #6 — они живут на внутреннем
    слушателе и в публичную поверхность не входят).

    Составной путь (`address.project_id`) резолвится ПО ЗВЕНЬЯМ, как его
    резолвит сам край (`extractByProtoReflect` рекурсивен): читаются ОБА поля —
    и ссылка у запроса, и поле у вложенного сообщения.

    НЕРЕЗОЛВЯЩИЙСЯ ПУТЬ ВОЗВРАЩАЕТСЯ ОТДЕЛЬНО, а не проглатывается. Имя в опции
    proto пишется РУКОЙ; поле переименовали, опцию забыли — край читает пустоту,
    и область проверки молча становится подстановочной. Проглотить такую запись
    значило бы измерить её как «поле закрыто», то есть выдать несуществующего
    читателя за существующего.
    """
    want = {}
    for sname, rpcs in (services or {}).items():
        if sname.startswith("Internal"):
            continue
        for rpc, req in rpcs:
            want[f"{package}.{sname}/{rpc}"] = req.split(".")[-1]

    pairs, unresolved = set(), []
    for entry in entries:
        msg = want.get((entry.get("fqn") or "").strip())
        if msg is None:
            continue
        for path in _catalog_field_paths(entry):
            cur = msg
            for seg in path.split("."):
                fields = messages.get(cur)
                if fields is None:
                    # Сообщение этим доменом НЕ РАЗОБРАНО (тип из чужого пакета).
                    # Судить его полем нельзя — но и молчать нельзя: край по этой
                    # записи что-то читает, а предикат об этом не утверждает
                    # ничего. Называется той же строкой и другим текстом, чтобы
                    # причина была видна с первого взгляда.
                    unresolved.append(
                        f"{msg}: {path} (сообщение {cur!r} этим доменом не разобрано)")
                    break
                typ = None
                found = False
                for fname, ftyp, _rep in fields:
                    if fname == seg:
                        typ, found = ftyp, True
                        break
                if not found:
                    unresolved.append(f"{msg}: {path} (нет поля {seg!r} у {cur})")
                    break
                pairs.add((cur, seg))
                cur = typ
    return pairs, unresolved


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


def self_test():
    """Инъекция в ОБЕ стороны на РАЗБОРЩИКЕ .proto — том месте, где состав
    осматриваемых полей и определяется.

    Зачем именно здесь. Вердикт предиката — про поля, которые он РАЗЛОЖИЛ по
    сообщениям; поле, не дошедшее до разложения, не попадает ни в находки, ни в
    корзины «не находка», ни в перепись — оно исчезает молча, и «осмотрено полей:
    N» читается как полный обход. Ровно это и случилось с членами `oneof`: они
    вырезались вместе с любым `{…}`-блоком.

    Проверяются обе стороны, иначе гейт ловил бы форму, а не существо:
      * КРАСНОЕ — член `oneof` обязан быть полем ОБЪЕМЛЮЩЕГО сообщения (так же,
        как его видит сгенерированный Go: геттер стоит на родителе);
      * МОЛЧАНИЕ — поле ВЛОЖЕННОГО `message` объемлющему НЕ достаётся, значение
        `enum` полем не становится, а член `oneof` вложенного сообщения остаётся
        у вложенного. Ради этого разделения вырезание блоков и заводилось.
    """
    import tempfile

    rc = 0
    checks = 0

    def check(name, ok, detail=""):
        nonlocal rc, checks
        checks += 1
        print(f"  {'ОК ' if ok else 'ПРОВАЛ'} {name}{'' if ok else ' — ' + detail}")
        if not ok:
            rc = 1

    def parse(src):
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "fixture.proto")
            open(p, "w", encoding="utf-8").write(src)
            _svcs, msgs, scopes = parse_proto(p)
            return msgs, scopes

    def resolve(msgs, scopes):
        return resolve_types(msgs, scopes)

    print("=== поле публичного запроса: инъекции в разборщик ===")

    # Форма — живая: `Target` домена loadbalancer (4-way identity oneof) рядом с
    # плоскими полями, вложенным сообщением, перечислением и вторым `oneof`
    # внутри вложенного сообщения (так записан `SecurityGroupRule` домена vpc).
    src = """
syntax = "proto3";
package kacho.cloud.fixture.v1;

message InCloudIP {
  string global_only = 1;
}

message Target {
  string target_group_id = 1;

  message InCloudIP {
    string subnet_id = 1;
    oneof scope {
      string zone_id = 2;
    }
  }

  enum Mode {
    MODE_UNSPECIFIED = 0;
    MODE_ACTIVE = 1;
  }
  Mode mode = 2;

  oneof identity {
    string instance_id = 3;
    string nic_id = 4;
    InCloudIP ip_ref = 5;
  }
  map<string, string> labels = 6;
}
"""
    msgs = resolve(*parse(src))
    top = {f for f, _t, _r in msgs.get("Target", [])}
    nested = {f for f, _t, _r in msgs.get("Target_InCloudIP", [])}
    glob_msg = {f for f, _t, _r in msgs.get("InCloudIP", [])}

    # (1) КРАСНОЕ до фикса: члены oneof — поля объемлющего сообщения.
    check("члены oneof разложены в объемлющее сообщение",
          {"instance_id", "nic_id", "ip_ref"} <= top,
          f"поля Target: {sorted(top)}")

    # (2) КРАСНОЕ до фикса: вложенное сообщение ключуется Go-именем `Parent_Nested`
    #     — тем же, каким его называют индекс чтений и сгенерированный геттер.
    check("вложенное сообщение ключуется Go-именем родитель_вложенное",
          "Target_InCloudIP" in msgs and "subnet_id" in nested,
          f"ключи: {sorted(msgs)}")

    # (3) КРАСНОЕ до фикса: тип поля резолвится ОТ САМОЙ УЗКОЙ области — `InCloudIP`
    #     внутри `Target` есть `Target_InCloudIP`, а не одноимённое глобальное.
    #     Без этого обход достижимости уходил бы в чужое сообщение.
    check("тип поля резолвится от узкой области видимости к глобальной",
          ("ip_ref", "Target_InCloudIP", False) in msgs.get("Target", []) and
          glob_msg == {"global_only"},
          f"поля Target: {msgs.get('Target')}")

    # (4) МОЛЧАНИЕ: имя самого oneof полем НЕ становится — в proto его нет.
    check("имя oneof полем не становится",
          "identity" not in top and "scope" not in top and "scope" not in nested,
          f"Target: {sorted(top)}; Target_InCloudIP: {sorted(nested)}")

    # (5) МОЛЧАНИЕ, законный близнец той же формы: поле ВЛОЖЕННОГО сообщения
    #     объемлющему не достаётся. Ради этого блоки и вырезаются.
    check("поле вложенного message объемлющему не достаётся",
          "subnet_id" not in top and "subnet_id" in nested,
          f"Target: {sorted(top)}; Target_InCloudIP: {sorted(nested)}")

    # (6) МОЛЧАНИЕ: член oneof ВЛОЖЕННОГО сообщения остаётся у вложенного.
    check("член oneof вложенного сообщения остаётся у вложенного",
          "zone_id" in nested and "zone_id" not in top,
          f"Target: {sorted(top)}; Target_InCloudIP: {sorted(nested)}")

    # (7) МОЛЧАНИЕ: значение enum полем не становится.
    check("значение enum полем не становится",
          "MODE_UNSPECIFIED" not in top and "MODE_ACTIVE" not in top,
          f"Target: {sorted(top)}")

    # (8) МОЛЧАНИЕ: плоские поля и map читаются как прежде.
    check("плоские поля и map читаются как прежде",
          {"target_group_id", "labels", "mode"} <= top,
          f"Target: {sorted(top)}")

    # ── ЧИТАТЕЛЬ ЧЛЕНА oneof ПРИХОДИТ ЧЕРЕЗ ТИП-ОБЁРТКУ ──────────────────────
    #
    # Канонический разбор в дереве — переключатель по типу и `v.Foo`, поэтому
    # получателем чтения `go/types` называет `M_Foo`, а не `M`. Без правила
    # засчитывания настоящий читатель не виден сильной основой.
    pkg = f"{MODULE}/{GEN_ROOT}/loadbalancer/v1"
    reader = f"{MODULE}/services/nlb/internal/apps/kacho/api/targetgroup"
    idx = {"packages": [], "reads": {
        f"{pkg}|HealthCheck_Tcp|Tcp": [reader],
        f"{pkg}|HealthCheck_TcpOptions|Port": [reader],
    }}
    got = typed_readers(idx, "loadbalancer")
    # (9) КРАСНОЕ до фикса: чтение через обёртку засчитано ЧЛЕНУ родителя.
    check("чтение члена oneof через тип-обёртку засчитано родителю",
          ("HealthCheck", "Tcp") in got, f"пары: {sorted(got)}")
    # (10) МОЛЧАНИЕ: обычное чтение у вложенного сообщения родителю НЕ засчитано
    #      — иначе правило закрывало бы поля чужим читателем, то есть возвращало
    #      бы ровно ту слабость, ради снятия которой основу и меняли.
    check("чтение у вложенного сообщения родителю не засчитано",
          ("HealthCheck_TcpOptions", "Port") in got and
          ("HealthCheck", "Port") not in got, f"пары: {sorted(got)}")

    # ── ПРОД-ДЕРЕВО ДОМЕНА, ВЫНЕСЕННОГО В СВОЙ Go-МОДУЛЬ ────────────────────
    #
    # Вторая форма записи читателя, о которой распознаватель не знал. Сервис,
    # вынесенный в собственный модуль, живёт под ЕГО путём пакетов
    # (`github.com/PRO-Robotech/kaname/...`), а не под `<корневой модуль>/services/<имя>`.
    # Пока префикс выводился из корневого модуля, читатели вынесенного сервиса
    # были прочитаны индексом и не приписаны никому — вердикт не менялся ни на
    # одно поле при выросшей вдвое переписи.
    #
    # Отличие между двумя фикстурами РОВНО ОДНО — наличие `go.mod` у каталога
    # сервиса; всё остальное совпадает побайтово.
    with tempfile.TemporaryDirectory() as root:
        os.makedirs(os.path.join(root, "services", "alpha"))
        os.makedirs(os.path.join(root, "services", "beta"))
        with open(os.path.join(root, "services", "alpha", "go.mod"), "w",
                  encoding="utf-8") as f:
            f.write("module github.com/PRO-Robotech/alpha\n\ngo 1.26.0\n")
        prev = os.getcwd()
        os.chdir(root)
        try:
            own_pref = prod_pkg_prefixes("alpha")[0]
            twin_pref = prod_pkg_prefixes("beta")[0]
        finally:
            os.chdir(prev)

    # (11) КРАСНОЕ до фикса: вынесенный сервис адресуется путём СВОЕГО модуля.
    check("прод-дерево вынесенного сервиса адресуется путём ЕГО модуля",
          own_pref == "github.com/PRO-Robotech/alpha", f"префикс: {own_pref}")
    # (12) МОЛЧАНИЕ, законный близнец: сервис БЕЗ своего go.mod адресуется
    #      прежним путём корневого модуля. Без этой половины «выводи из дерева»
    #      было бы неотличимо от «выводи что угодно».
    check("сервис без своего go.mod адресуется прежним путём корневого модуля",
          twin_pref == f"{MODULE}/services/beta", f"префикс: {twin_pref}")

    # ── ФОРМЫ ОБЪЯВЛЕНИЯ ОБРАБОТЧИКА ───────────────────────────────────────
    #
    # `has_handler` кормит освобождение `RPC-НЕ-РЕАЛИЗОВАН`: тип запроса, чьего
    # обработчика распознаватель не увидел, объявляется непринятым, и все его поля
    # перестают меряться. Значит форма, которой распознаватель не знает, даёт не
    # находку и не закрытие, а ЛОЖНОЕ ОСВОБОЖДЕНИЕ — самый дорогой из трёх
    # исходов, потому что выглядит он как «не находка».
    #
    # Форм в дереве три, а знал распознаватель одну (замер 2026-09-06: четыре типа
    # запроса в пяти доменах объявлены остальными двумя).
    handler_src = [("h.go", """
func (h *Handler) Get(ctx context.Context, req *iamv1.GetUserRequest) (*iamv1.User, error) {
func (h *Handler) WhoAmI(ctx context.Context, _ *iamv1.WhoAmIRequest) (*iamv1.WhoAmIResponse, error) {
func (h *Handler) List(
	ctx context.Context, req *computev1.ListQuotasRequest,
) (*computev1.ListQuotasResponse, error) {
""")]
    # Законный близнец ложной формы: тип-ассершен карты прав. Он называет тот же
    # тип запроса и обработчиком НЕ является — authz резолвит область по ЧУЖОМУ
    # полю и о чтении остальных не говорит ничего.
    assertion_src = [("authz.go", '\tif r, ok := req.(*vpcv1.DeleteSubnetRequest); ok {\n')]

    # (11a) ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: каноническая форма по-прежнему находится.
    #       Без него распознаватель, отвечающий «нет» на всё, прошёл бы отрицания.
    check("каноническая форма объявления обработчика находится",
          has_handler("GetUserRequest", handler_src))
    # (13) КРАСНОЕ до фикса: параметр запроса назван `_` — законная форма для
    #      запроса без полей, и именно её несут WhoAmI и List квот.
    check("обработчик с параметром запроса `_` находится",
          has_handler("WhoAmIRequest", handler_src))
    # (14) КРАСНОЕ до фикса: сигнатура перенесена на строки — после типа стоит
    #      запятая, а не `)`.
    check("обработчик с перенесённой на строки сигнатурой находится",
          has_handler("ListQuotasRequest", handler_src))
    # (15) МОЛЧАНИЕ: тип-ассершен карты прав обработчиком не считается.
    check("тип-ассершен карты прав обработчиком не считается",
          not has_handler("DeleteSubnetRequest", assertion_src))
    # (16) МОЛЧАНИЕ: тип запроса, которого в источниках нет, не находится.
    check("не объявленный в источниках тип запроса не находится",
          not has_handler("CreateNetworkRequest", handler_src))

    # ── КАТАЛОГ ПРАВ — ЧИТАТЕЛЬ, ПРИПИСЫВАЕМЫЙ ТОЧНО ────────────────────────
    #
    # Третья форма записи читателя, о которой распознаватель не знал вовсе. Край
    # читает поле запроса РЕФЛЕКСИЕЙ ПО ИМЕНИ, взяв имя из каталога прав
    # (`scope_extractor.from_request_field`), и это настоящий читатель — им
    # выбирается объект, на котором стоит проверка доступа.
    #
    # ОТЛИЧИЕ ОТ СЛАБОЙ ОСНОВЫ — НЕСУЩЕЕ. У строкового литерала получателя нет,
    # поэтому приписать его сообщению нечем. У записи каталога получатель ЕСТЬ:
    # запись ключуется FQN метода, а тип запроса метода объявлен в том же .proto.
    # Омонимия здесь не закрывает ничего by construction — ровно как у чтения с
    # резолвленным типом.
    #
    # ЦЕНА НЕЗНАНИЯ ЭТОЙ ФОРМЫ: поле, чей ЕДИНСТВЕННЫЙ читатель — каталог, было
    # бы ЛОЖНОЙ НАХОДКОЙ; поле, чьё имя вдобавок встречается строкой у соседнего
    # сообщения, уезжало в слабую корзину, то есть измерялось совпадением имён.
    # Второе и наблюдалось: `BatchAuthorizeCheckRequest.scope_id` (issue #2115).
    cat_src = """
syntax = "proto3";
package kacho.cloud.fixture.v1;

service ScopedService {
  rpc BatchCheck (BatchScopedRequest) returns (BatchScopedResponse);
  rpc Peer (PeerScopedRequest) returns (PeerScopedResponse);
}

service InternalScopedService {
  rpc Sweep (SweepScopedRequest) returns (SweepScopedResponse);
}

message Anchor { string project_id = 1; }
message BatchScopedRequest {
  string scope_id = 1;
  Anchor anchor = 2;
}
message BatchScopedResponse { string ok = 1; }
message PeerScopedRequest { string scope_id = 1; }
message PeerScopedResponse { string ok = 1; }
message SweepScopedRequest { string scope_id = 1; }
message SweepScopedResponse { string ok = 1; }
"""
    with tempfile.TemporaryDirectory() as d:
        cat_path = os.path.join(d, "fixture.proto")
        open(cat_path, "w", encoding="utf-8").write(cat_src)
        cat_svcs, cat_raw, cat_scopes = parse_proto(cat_path)
        cat_pkg = proto_package(cat_path)
    cat_msgs = resolve_types(cat_raw, cat_scopes)

    entries = [
        # Предмет: запись называет ПОЛЕ у ЭТОГО метода.
        {"fqn": "kacho.cloud.fixture.v1.ScopedService/BatchCheck",
         "scope_extractor": {"object_type": "project",
                             "from_request_field": "scope_id"}},
        # Законный близнец №1 — ТО ЖЕ ИМЯ ПОЛЯ у ДРУГОГО метода, чья запись
        # называет подстановочный знак. Приписаться `PeerScopedRequest` оно не
        # вправе: иначе основа снова мерила бы совпадение имён.
        {"fqn": "kacho.cloud.fixture.v1.ScopedService/Peer",
         "scope_extractor": {"from_request_field": "*"}},
        # Законный близнец №2 — та же пара «сервис/метод» в ЧУЖОМ пакете, и она
        # называет ДРУГОЕ поле здешнего запроса. Назови она то же самое, что и
        # предмет, — близнец стал бы неотличим от него, и атрибуция по хвосту
        # «Сервис/Метод» прошла бы проверку молча.
        {"fqn": "kacho.cloud.other.v1.ScopedService/BatchCheck",
         "scope_extractor": {"from_request_field": "anchor"}},
        # Законный близнец №3 — Internal-сервис: он вне публичной поверхности,
        # которую мерит предикат, и закрывать в ней ничего не может.
        {"fqn": "kacho.cloud.fixture.v1.InternalScopedService/Sweep",
         "scope_extractor": {"from_request_field": "scope_id"}},
    ]
    cat_pairs, cat_unresolved = catalog_readers(entries, cat_pkg, cat_svcs, cat_msgs)

    # (17) КРАСНОЕ до фикса: поле, названное каталогом у ЭТОГО метода, закрыто.
    check("поле, названное каталогом у этого метода, приписано его запросу",
          ("BatchScopedRequest", "scope_id") in cat_pairs,
          f"пары: {sorted(cat_pairs)}")
    # (18) МОЛЧАНИЕ, законный близнец: то же имя у другого метода не закрывается.
    check("то же имя поля у другого метода каталогом не закрывается",
          ("PeerScopedRequest", "scope_id") not in cat_pairs,
          f"пары: {sorted(cat_pairs)}")
    # (19) МОЛЧАНИЕ: запись чужого пакета не закрывает ничего — атрибуция идёт по
    #      FQN целиком, а не по хвосту «сервис/метод».
    check("запись каталога из чужого пакета не закрывает ничего",
          ("BatchScopedRequest", "anchor") not in cat_pairs,
          f"пары: {sorted(cat_pairs)}")
    # (19а) МОЛЧАНИЕ: Internal-сервис вне публичной поверхности предиката (ban #6),
    #       и его запись каталога закрыть в ней ничего не может.
    check("запись каталога Internal-сервиса не закрывает ничего",
          ("SweepScopedRequest", "scope_id") not in cat_pairs,
          f"пары: {sorted(cat_pairs)}")
    # (20) КРАСНОЕ до фикса: составной путь резолвится ПО ЗВЕНЬЯМ — читаются оба.
    dotted = [{"fqn": "kacho.cloud.fixture.v1.ScopedService/BatchCheck",
               "scope_extractor": {"from_request_field": "anchor.project_id"}}]
    dot_pairs, _dot_unres = catalog_readers(dotted, cat_pkg, cat_svcs, cat_msgs)
    check("составной путь каталога резолвится по звеньям",
          ("BatchScopedRequest", "anchor") in dot_pairs and
          ("Anchor", "project_id") in dot_pairs,
          f"пары: {sorted(dot_pairs)}")
    # (21) МОЛЧАНИЕ: подстановочный знак полем не является и не закрывает ничего.
    star_pairs, star_unres = catalog_readers(
        [{"fqn": "kacho.cloud.fixture.v1.ScopedService/BatchCheck",
          "scope_extractor": {"from_request_field": "*"}}],
        cat_pkg, cat_svcs, cat_msgs)
    check("подстановочный знак каталога не закрывает ни одного поля",
          not star_pairs and not star_unres,
          f"пары: {sorted(star_pairs)}; не резолвится: {star_unres}")
    # (22) КРАСНОЕ до фикса: имя, которого у запроса НЕТ, названо отдельно, а не
    #      проглочено. Такая запись означает, что край читает пустоту и область
    #      проверки молча становится подстановочной.
    ghost_pairs, ghost_unres = catalog_readers(
        [{"fqn": "kacho.cloud.fixture.v1.ScopedService/BatchCheck",
          "scope_extractor": {"from_request_field": "no_such_field"}}],
        cat_pkg, cat_svcs, cat_msgs)
    check("имя, которого у запроса нет, названо отдельной строкой",
          not ghost_pairs and len(ghost_unres) == 1,
          f"пары: {sorted(ghost_pairs)}; не резолвится: {ghost_unres}")
    # (23) МОЛЧАНИЕ: пустой каталог — ОТКАЗ предпосылки, а не «чисто».
    with tempfile.TemporaryDirectory() as d:
        empty = os.path.join(d, "empty.json")
        open(empty, "w", encoding="utf-8").write("[]")
        try:
            load_permission_catalog(empty)
            empty_refused = False
        except (RuntimeError, OSError, ValueError):
            empty_refused = True
    check("пустой каталог прав — отказ предпосылки, а не чистый обход",
          empty_refused)

    print(f"проверок исполнено: {checks}; разобрано фикстур: 1; "
          f"сообщений в фикстуре: {len(msgs)}; ключей индекса-фикстуры: "
          f"{len(idx['reads'])}")
    if rc:
        print("ПРОВАЛ: состав осматриваемых полей либо атрибуция читателя не "
              "совпадают с тем, как их видит сгенерированный Go.", file=sys.stderr)
    return rc


def main():
    args = sys.argv[1:]
    if "--self-test" in sys.argv:
        return self_test()
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
    # КАТАЛОГ ПРАВ — ВТОРОЙ ВХОД ПРЕДИКАТА, и его отсутствие есть отказ, а не
    # факт о полях: по нему край читает поле запроса рефлексией, и без него
    # третья основа закрывала бы ноль полей МОЛЧА.
    try:
        catalog = load_permission_catalog()
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
    # ОБЪЁМ ОСМОТРЕННОГО ПЕЧАТАЕТСЯ ПОРОЗНЬ ПО МОДУЛЯМ, и пустой обход ЛЮБОГО из
    # них — отказ. Дерево несёт больше одного Go-модуля; пока перепись знала одно
    # суммарное число, обход одного дерева был неотличим от обхода обоих —
    # суммарное росло от чего угодно.
    idx_mods = index.get("modules") or []
    empty_mods = [m for m in idx_mods if not m.get("packages")]
    if empty_mods:
        for m in empty_mods:
            print(f"ПРЕДПОСЫЛКА НЕ ВЫПОЛНЕНА: модуль {m.get('path')!r} "
                  f"({m.get('dir')}) обойдён по {m.get('patterns')} и не дал ни "
                  f"одного пакета — его чтения НЕВИДИМЫ", file=sys.stderr)
        return 2

    protos_read = svc_public = rpc_public = 0
    msgs_expanded = fields_seen = 0
    closed_typed = 0
    closed_catalog = 0
    catalog_unresolved = []
    without_gen = []
    premise_failures = []
    findings = []
    buckets = {"RPC-НЕ-РЕАЛИЗОВАН": [], "ПРЕДОК-ОТВЕРГАЕТСЯ": []}
    by_name_only = []
    unimpl_rpcs = {}

    for domain in domains:
        proto_paths = sorted(glob.glob(os.path.join(PROTO_ROOT, domain, "v1", "*.proto")))
        if not proto_paths:
            continue
        services, raw_messages, scopes = {}, {}, {}
        packages = set()
        for p in proto_paths:
            protos_read += 1
            s, m, sc = parse_proto(p)
            services.update(s)
            raw_messages.update(m)
            scopes.update(sc)
            pkg = proto_package(p)
            if pkg:
                packages.add(pkg)
        # Резолв типов — ПОСЛЕ слияния всех файлов домена: поле одного файла
        # ссылается на сообщение другого, и по одному файлу такая ссылка не
        # резолвится (обход достижимости остановился бы на границе файла).
        messages = resolve_types(raw_messages, scopes)

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

        getters, declared = load_getters(domain)
        sources = prod_sources(index, domain)
        own_pkgs = own_tree_packages(index, domain)
        if not sources or not own_pkgs:
            # ПРЕДПОСЫЛКА НЕ ВЫПОЛНЕНА: дерево прод-кода этого домена не найдено, а
            # значит «читателя нет» здесь означает «читать было негде». Молчать нельзя
            # и находки этого домена печатать нельзя — они были бы утверждением о
            # непрочитанном.
            #
            # СОБСТВЕННОЕ ДЕРЕВО ПРОВЕРЯЕТСЯ ОТДЕЛЬНО ОТ `sources`, и без этого
            # предпосылка не срабатывала ни разу: `sources` берёт ещё край и общий
            # `internal/`, поэтому оставалась непустой при ПОЛНОСТЬЮ исчезнувшем
            # дереве службы. Ровно так 268 полей домена iam и уехали в полосу
            # «RPC-НЕ-РЕАЛИЗОВАН»: их обработчики стали невидимы, а предикат об
            # этом не сказал ничего.
            premise_failures.append((domain, prod_pkg_prefixes(domain)[0],
                                     len(sources), len(own_pkgs)))
            continue
        typed = typed_readers(index, domain)
        # ТРЕТЬЯ ОСНОВА — чтение краем по каталогу прав; атрибуция точная (FQN
        # метода → его тип запроса), поэтому она стоит рядом с сильной, а не
        # рядом со слабой.
        cat_pairs = set()
        for pkg in sorted(packages):
            pp, unres = catalog_readers(catalog, pkg, services, messages)
            cat_pairs |= pp
            catalog_unresolved.extend(unres)
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
                if name not in declared:
                    without_gen.append(f"{domain}:{name}")
                    continue
                # Сообщение сгенерировано, но геттеров у него нет — значит полей
                # нет вовсе (`message X {}`). Осматривать нечего, и это НЕ слепая
                # зона: раскрытым его считать надо, иначе порог непрочитанного
                # держится тем, в чём читать нечего.
                gos = set()
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
                if (name, fname) in cat_pairs:
                    # ТОЖЕ СИЛЬНАЯ ОСНОВА: поле названо каталогом прав у ЭТОГО
                    # метода, и край читает его рефлексией. Получатель у записи
                    # есть — FQN метода, — поэтому омонимия не закрывает ничего
                    # и здесь. Печатается отдельной строкой переписи, а не
                    # сливается с чтением по типу: «резолвлен тип получателя»
                    # про эту форму неверно, а перепись обязана называть то, что
                    # действительно измерено.
                    closed_catalog += 1
                    continue
                # СТРУКТУРНЫЕ ОСВОБОЖДЕНИЯ — ПЕРЕД слабой основой, и порядок здесь
                # содержателен. Оба вычислены по ДЕРЕВУ (есть ли обработчик у типа
                # запроса; достижимо ли сообщение иначе как через отвергнутое поле)
                # и от атрибуции читателя по имени не зависят ВООБЩЕ. Пока они
                # стояли после слабой корзины, поле нереализованного RPC, чьё имя
                # где-то встречается строкой, объявлялось «закрытым слабой
                # атрибуцией» — то есть слабость мерки выписывалась там, где мерка
                # не применялась: читать такой запрос НЕКОМУ и незачем.
                if name in by_unimpl:
                    buckets["RPC-НЕ-РЕАЛИЗОВАН"].append((domain, name, fname, go))
                    continue
                if name in by_refusal:
                    buckets["ПРЕДОК-ОТВЕРГАЕТСЯ"].append((domain, name, fname, go))
                    continue
                if _has_string_reader(fname, sources):
                    # Слабая основа: рефлексивный читатель по имени. Приписать его
                    # сообщению нечем — поле называется поимённо, а не прячется.
                    #
                    # Сюда же попадает и явный отказ по имени (`AddFieldViolation`),
                    # и это НЕ потеря: он такой же именной, значит и слабость у него
                    # та же. Отдельной корзины у него нет намеренно — она была бы
                    # пуста всегда by construction (литерал отказа содержит имя поля,
                    # поэтому строковый читатель находит его первым), а корзина,
                    # куда ничто не может попасть, и есть предмет issue #110.
                    by_name_only.append(
                        (domain, name, fname, go, fname in homonyms))
                    continue
                findings.append((domain, name, fname, go))

    if protos_read == 0:
        print("НИЧЕГО НЕ ПРОЧИТАНО — предикат ничего не утверждает", file=sys.stderr)
        return 2

    print(f"прочитано .proto: {protos_read}; публичных сервисов: {svc_public}; "
          f"публичных RPC: {rpc_public}")
    print(f"протипизировано пакетов прод-кода: {len(idx_pkgs)}; файлов: {idx_files} "
          f"(пакетов без непроверочных файлов: "
          f"{len(index.get('skipped_no_prod_files') or [])})")
    print(f"обойдено Go-модулей: {len(idx_mods)}")
    for m in idx_mods:
        print(f"    модуль {m.get('path')} ({m.get('dir')}, {m.get('patterns')}): "
              f"пакетов {m.get('packages')}, файлов {m.get('files')}")
    print(f"раскрыто сообщений публичного запроса: {msgs_expanded} "
          f"(без сгенерированного кода, НЕ осмотрены: {len(without_gen)})")
    for name in without_gen:
        # Предпосылка предиката: Go-имена берутся из сгенерированного кода. Сообщение
        # без него не осмотрено — и это НАЗЫВАЕТСЯ, иначе «ноль находок» скрывало бы
        # слепую зону.
        print(f"    НЕ ОСМОТРЕНО (нет .pb.go): {name}")
    print(f"осмотрено полей: {fields_seen}")
    for d, pref, nsrc, nown in premise_failures:
        print(f"ПРЕДПОСЫЛКА НЕ ВЫПОЛНЕНА: у домена {d!r} прочитано файлов прод-кода "
              f"{nsrc}, пакетов СОБСТВЕННОГО дерева {nown} (искали под {pref!r}, "
              f"деревья {prod_trees(d)}) — домен НЕ измерен. Либо каталог сервиса "
              f"не сопоставлен домену в DOMAIN_SERVICE_DIR, либо служба вынесена в "
              f"свой Go-модуль, а обход в него не спустился")
    print(f"закрыто ЧТЕНИЕМ У ЭТОГО СООБЩЕНИЯ (тип получателя резолвлен): "
          f"{closed_typed} из {fields_seen}")
    print(f"записей каталога прав прочитано: {len(catalog)}")
    print(f"закрыто КАТАЛОГОМ ПРАВ (поле названо у ЭТОГО метода): "
          f"{closed_catalog} из {fields_seen}")
    # Запись каталога, чьё имя поля у запроса НЕ объявлено, — не «поле закрыто»
    # и не молчание: край читает по ней пустоту, и объект проверки молча
    # становится подстановочным. Сюда же — запись, чьё сообщение этим доменом не
    # разобрано: судить её нечем, и молчать об этом тоже нельзя. Называется
    # поимённо; ноль печатается тоже, иначе пустая строка была бы неотличима от
    # неисполненной проверки.
    print(f"запись каталога без предмета (нет поля либо сообщение не разобрано): "
          f"{len(catalog_unresolved)}")
    for row in catalog_unresolved:
        print(f"    {row}")
    for label in ("RPC-НЕ-РЕАЛИЗОВАН", "ПРЕДОК-ОТВЕРГАЕТСЯ"):
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
