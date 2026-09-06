#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""BAN6-EXT-GRPC — Internal*-методы недостижимы на advertised external листенере.

ЧТО ЭТО УТВЕРЖДЕНИЕ СПОСОБНО ЗАСВИДЕТЕЛЬСТВОВАТЬ (и что — нет)
---------------------------------------------------------------
Свидетельствует: **аутентифицированный вызывающий с самыми широкими правами**
(bootstrap-admin Bearer), придя на внешний TLS-листенер api-gateway
(`KACHO_API_GATEWAY_TLS_LISTEN_ADDR`, здесь :8443) **по gRPC**, получает на
каждый Internal*-метод отказ маршрутизации (`NotFound: unknown method` либо
`Unimplemented`) — то есть метода на этом листенере НЕТ.

НЕ свидетельствует: ничего про ingress-имя `api.kacho.local`, ничего про REST и
ничего про то, что метод где-то работает — последнее проверяется отдельным
встречным контролем (см. ниже) и без него утверждение было бы пустым.

ГРАНИЦА ПРОБЫ: authz ШЛЮЗА ОТВЕЧАЕТ РАНЬШЕ РЕЗОЛВЕРА
-----------------------------------------------------
Решение «маршрута нет» принимает `UnknownServiceHandler`
(`gateway/internal/proxy`), а authz-интерсептор стоит в цепочке ПЕРЕД ним
(`grpc.ChainUnaryInterceptor(...)` в composition root). Значит на методе, чьё
право у пробы отсутствует, ответом будет `PermissionDenied` от каталога шлюза —
и до решения о маршрутизации проба НЕ ДОЙДЁТ. Такой исход классифицируется
отдельным вердиктом `INCONCLUSIVE` и **не засчитывается в изоляцию**: засчитать
его значило бы позеленеть на методе, который не проверялся. Поэтому проба идёт
под самым широким принципалом посева (bootstrap-admin) — чем шире права, тем
больше методов доходит до резолвера.

Отсекаются раньше именно методы с **объектным** `scope_extractor`
(`object_type` + `from_request_field`): цель резолвится из тела запроса, у пробы
такого объекта нет, и гейт закрывается fail-closed. Измерено 2026-07-29: подстановка
синтаксически валидного, но несуществующего id ответ НЕ меняет (hide-existence
deny) — то есть чёрным ящиком эти методы до решения о маршрутизации не довести
вовсе, пока у вызывающего нет РЕАЛЬНОГО доступного объекта нужного типа.

Побочное следствие того же порядка, видимое прямо в выводе: методы, доходящие до
резолвера, получают неотличимое `NotFound: unknown method` (это и есть заявленное
в `proxy/server.go` «NotFound is load-bearing … not an existence-oracle»), а
методы, отсечённые раньше, получают `PermissionDenied` С ИМЕНЕМ ПРАВА — то есть
для них укрытие под «метода не существует» не работает.

СОСТОЯНИЕ ОСТАТКА НА СЕГОДНЯ (замер kind-kacho, 2026-07-29, после перевода проб
на protoset): методов в полосе `INCONCLUSIVE` — **0**, все 79 доходят до решения
о маршрутизации и получают одинаковый отказ. Разбор ниже сохранён потому, что
полоса возвращается сама собой, стоит прогнать пробу под более узким принципалом,
— но описывать её как наблюдаемый сейчас остаток было бы неправдой.

Как закрыть остаток честно (в порядке предпочтения):
  1. отказывать в маршруте для `Internal*Service` ДО authz-интерсептора — тогда
     все 79 доходят до одинакового `NotFound`, и укрытие становится равномерным;
  2. либо посеять по одному доступному объекту на каждый объектно-скоупленный
     Internal-метод, чтобы проба проходила authz и упиралась в резолвер.
Первое дешевле и заодно устраняет расхождение ответов, описанное выше.

ПОЧЕМУ gRPC, А НЕ REST (измерено 2026-07-29 на kind-kacho)
-----------------------------------------------------------
Внешний листенер согласует ALPN **h2** и говорит **только gRPC**. Обычный
HTTP-запрос по нему (`curl https://…/iam/v1/whoami`, в том числе `--http1.1`)
сервер закрывает `close notify` сразу после заголовков, не отдав ни одного
HTTP-ответа; kubectl-проброс при этом валится `broken pipe` и умирает. Поэтому
проба в форме REST на этой поверхности не может быть свидетельством **в
принципе**: она не получает ответа, а «нет ответа» — это сломанный харнесс, а не
доказательство изоляции. Контрольный кейс REST-суиты («внешний листенер отдаёт
REST») падал ровно по этой причине.

Сертификат листенера подписан внутренним CA и несёт SAN
`api-gateway.kacho.svc.cluster.local`, поэтому проба идёт с **полной проверкой
цепочки** (`-cacert` + `-authority`), а не с отключённой верификацией: доверие к
внутреннему CA — часть постановки, а не то, что обходится флагом.

ТРИ КОНТРОЛЯ, БЕЗ КОТОРЫХ ВЕРДИКТ НИЧЕГО НЕ ЗНАЧИТ
---------------------------------------------------
1. **CONTROL-LIVENESS** — публичный метод на ТОМ ЖЕ листенере обязан быть
   отмаршрутизирован и отвечен без gRPC-ошибки. Иначе «unknown method» ниже
   удовлетворяется листенером, который просто лежит, или протухшим токеном.
2. **CONTROL-COUNTERPART** — для КАЖДОГО домена хотя бы один Internal*-метод
   обязан быть ДОКАЗАННО обслужен на своём cluster-internal листенере (:9091,
   mTLS) в том же прогоне. Без этого «метода нет на внешнем» неотличимо от
   «метода нет нигде» — та самая пустота, ради которой контроль и заведён.
   **Непокрытый домен роняет прогон** (rc=2, измерение не выполнено). Прежняя
   версия заявляла это же требование в шапке, а в коде валила прогон только
   когда не подтверждён НИ ОДИН домен; покрытие печаталось числом и ни на что
   не влияло. Живьём подтверждался один домен из восьми — то есть про 53 метода
   из 79 «нет на внешнем» не было подкреплено ничем.

   **Дескрипторы берутся из protoset, а не из reflection.** Причина не в
   удобстве: reflection на cluster-internal листенере ЗАКРЫТ намеренно — сам
   reflection-RPC не значится в карте прав и отвергается fail-closed, как любой
   незаявленный метод. Это правильно (reflection перечисляет админскую
   поверхность), и снимать его ради пробы нельзя. grpcurl без reflection
   получает дескрипторы из `buf build proto` — предмет по-прежнему берётся из
   `proto/`, источника истины.

   **Чем доказывается «обслужен» без reflection.** На cluster-internal
   листенерах не установлен обработчик неизвестного сервиса, поэтому
   незарегистрированный метод отклоняется диспетчером gRPC (`Unimplemented:
   unknown service`) ДО интерсепторов — раньше, чем кто-либо спросит о правах.
   Значит любой ответ, который НЕ является отказом маршрутизации (в том числе
   отказ в правах), доказывает, что метод на листенере зарегистрирован.
   Это утверждение о листенере, а не догадка, поэтому оно ПРОВЕРЯЕТСЯ: на том
   же листенере, тем же соединением, пробуется заведомо чужой Internal-метод, и
   он ОБЯЗАН получить отказ маршрутизации. Не получил — предпосылка на этом
   листенере не выполняется, домен НЕ засчитывается.

   Порядок обратный тому, что на внешнем листенере: там отказ в правах отвечает
   РАНЬШЕ резолвера (стоит обработчик неизвестного сервиса), поэтому там он не
   доказывает ничего и остаётся `INCONCLUSIVE`.
3. **CONTROL-RESOLUTION** — дескриптор каждого пробуемого метода обязан
   резолвиться. Ошибка резолва на стороне grpcurl (опечатка в имени, метод
   удалён из proto) классифицируется как **отказ харнесса**, НИКОГДА как «не
   достижим»: иначе опечатка выглядела бы как доказанная изоляция.

Перечень предмета берётся из `proto/` — источника истины, а не из reflection:
reflection на этом листенере отдаёт лишь то, что шлюз регистрирует локально
(4 сервиса), а всё остальное он проксирует, поэтому список из reflection
занизил бы предмет молча.

Запуск:
  python3 deploy/scripts/assert-ban6-external-isolation.py            # живой прогон
  python3 deploy/scripts/assert-ban6-external-isolation.py --self-test # инъекции, без стенда
"""

from __future__ import annotations

import argparse
import glob
import importlib.util
import json
import os
import pathlib
import re
import shutil
import socket
import subprocess
import sys
import time

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
# Образец адресует каталог домена ЦЕЛИКОМ. Прежняя редакция брала только
# `*/v1/*.proto`, и домен, положивший контракт рядом с формой, выпадал из
# предмета молча: раскладка `v1/` — соглашение, а не инвариант.
PROTO_GLOB = os.path.join(REPO_ROOT, "proto", "kacho", "cloud", "**", "*.proto")
FIXTURES = os.path.join(REPO_ROOT, "tests", "authz-fixtures", "out", "authz-fixtures.json")

# Вердикты классификации одной пробы.
ISOLATED = "ISOLATED"  # сервер ответил «маршрута нет» — предмет доказан
REACHABLE = "REACHABLE"  # сервер ответил по существу — метод отмаршрутизирован
UNRESOLVED = "UNRESOLVED"  # ответа по существу не было — отказ харнесса, не вердикт
# Проба НЕ ДОШЛА до решения о маршрутизации: authz-интерсептор шлюза стоит в
# цепочке ПЕРЕД UnknownServiceHandler (gateway/cmd/api-gateway/main.go:
# ChainUnaryInterceptor(...) + proxy.NewServer(resolver)), поэтому отказ в правах
# отвечает раньше, чем резолвер успевает сказать «маршрута нет». Такой ответ не
# свидетельствует НИ об изоляции, НИ о достижимости — и не имеет права
# засчитываться в первую пользу.
INCONCLUSIVE = "INCONCLUSIVE"

# Домен proto-пакета → cluster-internal gRPC-эндпоинт владельца (для встречного
# контроля): (цель-проброса, порт, authority-для-TLS).
#
# Домен без эндпоинта РОНЯЕТ прогон: «мы не знаем, где его слушают» — это не
# повод засчитать изоляцию его методов.
#
# Здесь стояла запись края (`apigateway`), пробрасываемая на ПОД, а не на
# Service: внутренний gRPC-листенер шлюза не был выставлен ни одним Service.
# Записи нет, потому что нет её предмета — задача #1024 сняла у края
# единственную службу этого листенера (гашение кэша решений по запросу iam), а с
# ней и сам листенер: обслуживать было бы нечего.
#
# Снята, а не оставлена «на будущее», и это держится ПРЕДИКАТОМ, а не памятью:
# самопроверка ниже сверяет карту в ОБЕ стороны, и домен с известным эндпоинтом,
# не признанный провязанным, роняет её. То есть запись, пережившая свой предмет,
# здесь не молчит — она краснеет.
#
# КЛЮЧ — НОСИТЕЛЬ, ТО ЕСТЬ КАТАЛОГ СЕРВИСА, А НЕ ДОМЕН. Листенером владеет
# процесс, а не имя контракта, и различие это не педантское: домен
# `subscription` (поток изменений ресурсов) служат ПЯТЬ носителей сразу, и
# «своего» листенера у него нет ни одного. Ключуй карту доменом — и у такого
# домена не окажется правильного значения ВООБЩЕ: любой выбранный носитель был
# бы произволом, который ломается молча в день, когда именно он перестанет
# монтировать службу. Носители домена ВЫВОДЯТСЯ из дерева
# (`e2e-ban6-domains.py`, `hosts`), поэтому шестой носитель войдёт в перебор сам.
INTERNAL_ENDPOINTS = {
    "iam": ("svc/kaname-internal", 9091, "kaname-internal.kacho.svc.cluster.local"),
    "geo": ("svc/kacho-geo-internal", 9091, "kacho-geo-internal.kacho.svc.cluster.local"),
    "nlb": ("svc/kacho-nlb-internal", 9091, "kacho-nlb-internal.kacho.svc.cluster.local"),
    "registry": ("svc/registry-internal", 9091, "registry-internal.kacho.svc.cluster.local"),
    "vpc": ("svc/vpc", 9091, "vpc.kacho.svc.cluster.local"),
    "compute": ("svc/compute", 9091, "compute.kacho.svc.cluster.local"),
    "storage": ("svc/kacho-storage", 9091, "kacho-storage.kacho.svc.cluster.local"),
}

# Вердикты встречного контроля (отдельные от вердиктов внешней пробы — вопрос
# другой и порядок обработки на этом листенере другой, см. шапку).
SERVED = "ОБСЛУЖЕН"
ABSENT = "НЕ ЗАРЕГИСТРИРОВАН"

# Публичные методы для CONTROL-LIVENESS. Берём read-RPC двух разных доменов:
# один домен мог бы лежать сам по себе, и тогда контроль сообщил бы про стенд, а
# не про листенер.
LIVENESS_METHODS = [
    "kacho.cloud.geo.v1.RegionService/List",
    "kaname.cloud.iam.v1.UserService/List",
]

# Признаки того, что ответа ПО СУЩЕСТВУ не было: grpcurl не смог собрать запрос
# или не дозвонился. Всё это — отказ харнесса.
_CLIENT_SIDE = (
    "does not include a method named",
    "does not expose service",
    "Failed to dial",
    "Failed to resolve",
    "failed to query for service descriptor",
    "Timeout expired",
    "connection refused",
    "no such host",
)


# Сообщения, которыми отвечает ОТКАЗ МАРШРУТИЗАЦИИ (метода на листенере нет).
# Именно тело, а не один код: `NotFound` приходит и от дошедшего владельца
# («Region rgn-zzz not found»), и путать их нельзя — второе означает, что метод
# как раз отмаршрутизирован.
_ROUTING_MISS = ("unknown method", "unknown service", "not implemented",
                 "unimplemented method", "unimplemented service")


def classify(transcript: str) -> tuple[str, str]:
    """Вердикт по одной расшифровке grpcurl.

    Возвращает (вердикт, деталь). Деталь — короткая координата для отчёта.

    Три исхода, и они НЕ схлопываются:
      ISOLATED   — сервер сказал «такого метода тут нет» (предмет доказан);
      REACHABLE  — сервер ответил ПО СУЩЕСТВУ (метод отмаршрутизирован);
      UNRESOLVED — ответа по существу не было (отказ харнесса), в том числе
                   `Unauthenticated`: так отвечают ВСЕ методы этого листенера
                   без токена, включая публичные, поэтому принять это за
                   изоляцию значит объявить изолированным весь листенер.
    """
    text = transcript.strip()
    low = text.lower()

    for marker in _CLIENT_SIDE:
        if marker.lower() in low:
            return UNRESOLVED, marker

    m = re.search(r"^\s*Code:\s*(\w+)", text, re.M)
    if m:
        code = m.group(1)
        msg_m = re.search(r"^\s*Message:\s*(.*)$", text, re.M)
        msg = (msg_m.group(1).strip() if msg_m else "")
        if code == "Unauthenticated":
            return UNRESOLVED, "Unauthenticated — проба без пригодного токена"
        if code == "PermissionDenied":
            return INCONCLUSIVE, f"authz ответил раньше резолвера: {msg[:60]}"
        if any(k in msg.lower() for k in _ROUTING_MISS):
            return ISOLATED, f"{code}: {msg[:60]}"
        if code == "Unimplemented":
            return ISOLATED, f"{code}: {msg[:60]}"
        return REACHABLE, f"{code}: {msg[:60]}"

    if not text:
        return UNRESOLVED, "пустая расшифровка"
    return REACHABLE, "ответ телом (2xx)"


def counterpart_verdict(transcript: str) -> tuple[str, str]:
    """«Обслужен ли метод на СВОЁМ cluster-internal листенере».

    Другой вопрос, чем у внешней пробы, и другой порядок обработки на сервере,
    поэтому вердикт отдельный, а не переиспользование внешнего.

    На этих листенерах не установлен обработчик неизвестного сервиса, поэтому
    незарегистрированный метод отклоняет диспетчер gRPC ДО интерсепторов. Значит:

      ABSENT     — отказ маршрутизации: метода на листенере НЕТ;
      SERVED     — всё остальное по существу, ВКЛЮЧАЯ отказ в правах: до прав
                   доходит только зарегистрированный метод;
      UNRESOLVED — ответа по существу не было (отказ харнесса), не вердикт.

    Утверждение «до прав доходит только зарегистрированный» проверяется на том
    же листенере контрольной пробой заведомо чужого метода — см. serve_check().
    """
    v, d = classify(transcript)
    if v == UNRESOLVED:
        return UNRESOLVED, d
    if v == ISOLATED:
        return ABSENT, d
    return SERVED, d


def internal_rpcs(proto_glob: str = PROTO_GLOB) -> list[tuple[str, str, str]]:
    """(package, ServiceName, MethodName) для каждого `service Internal*` в proto."""
    rows: list[tuple[str, str, str]] = []
    for path in sorted(glob.glob(proto_glob, recursive=True)):
        txt = open(path, encoding="utf-8").read()
        m = re.search(r"^package\s+([\w.]+)\s*;", txt, re.M)
        if not m:
            continue
        pkg = m.group(1)
        for sm in re.finditer(r"^service\s+(Internal\w+)\s*\{", txt, re.M):
            depth, i = 1, sm.end()
            while i < len(txt) and depth > 0:
                if txt[i] == "{":
                    depth += 1
                elif txt[i] == "}":
                    depth -= 1
                i += 1
            body = txt[sm.end(): i]
            for rm in re.finditer(r"^\s*rpc\s+(\w+)\s*\(", body, re.M):
                rows.append((pkg, sm.group(1), rm.group(1)))
    return rows


def domain_of(pkg: str) -> str:
    parts = pkg.split(".")
    return parts[2] if len(parts) > 2 else pkg


def counterpart_candidates(dom: str, hosts_of: dict[str, list[str]],
                           only: set[str]) -> list[str]:
    """Носители домена, на чьём :9091 имеет смысл спрашивать встречный контроль.

    Отбор из двух ступеней, и вторая существует ради ВРЕМЕНИ, а не строгости.

    Ступень первая: у носителя обязан быть объявлен эндпоинт — иначе спрашивать
    некуда.

    Ступень вторая: когда стенд сужен (`--domains`), вперёд ставятся носители,
    чей СОБСТВЕННЫЙ домен на этом стенде развёрнут. Собственный домен носителя —
    тот, у кого этот носитель единственный (`vpc` у vpc, `loadbalancer` у nlb);
    он и служит признаком «процесс здесь поднят». Без этой ступени проба на шарде
    `edge` перебирала бы четырёх отсутствующих носителей по таймауту проброса,
    прежде чем дойти до registry — то есть платила бы минутами за порядок списка.

    Отфильтрованное НЕ выбрасывается: если ни один приоритетный не подошёл,
    возвращаются все носители с эндпоинтом. Сужение здесь — подсказка о порядке,
    а не вторая карта состава стенда: ошибись оно, проба обязана честно спросить
    и честно ответить, а не объявить домен неизмеримым.
    """
    own = {h: d for d, hs in hosts_of.items() if len(hs) == 1 for h in hs}
    known = [h for h in hosts_of.get(dom, []) if h in INTERNAL_ENDPOINTS]
    if not only:
        return known
    deployed = [h for h in known if own.get(h) in only]
    return deployed + [h for h in known if h not in deployed]


def _ban6_module():
    """Тот же предикат популяции, что исполняет `assert-shard-coverage.py`.

    Два места об одном предмете здесь разошлись бы молча — и разошлись бы именно
    там, где расхождение не видно: обе реализации отвечают «да» на очевидном
    входе. Замер 2026-08-23: гейт покрытия видел 9 доменов, эта проба — 8, и
    девятый не измерял НИКТО.
    """
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "e2e-ban6-domains.py")
    spec = importlib.util.spec_from_file_location("e2e_ban6_domains", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def subject_rpcs() -> tuple[list[tuple[str, str, str]], dict]:
    """Предмет пробы: Internal*-методы доменов, у которых ban #6 ИМЕЕТ предмет.

    Домен, чей контракт не регистрирует ни один композиционный корень, недостижим
    на внешнем листенере by construction. Засчитать это в изоляцию значило бы
    получить зелёное из отсутствия — ровно то, ради невозможности чего заведён
    CONTROL-COUNTERPART, который на таком домене роняет прогон, а не подтверждает
    его. Поэтому такой домен из предмета ИСКЛЮЧАЕТСЯ — и это НЕ послабление:
    исключение выведено из дерева и истекает само, как только регистрация
    появится в прод-коде.

    Перепись возвращается рядом с предметом: «ноль находок» обязано быть отличимо
    от «ноль прочитанного», а сужение — быть НАПЕЧАТАННЫМ, а не выведенным из
    тишины.
    """
    census = _ban6_module().census(pathlib.Path(REPO_ROOT))
    rows = [r for r in internal_rpcs() if domain_of(r[0]) in census["served"]]
    return rows, census


# ─────────────────────────── живой прогон ────────────────────────────────────


def _free_port() -> int:
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


class PortForward:
    """kubectl port-forward на время блока. Проброс — часть харнесса: если он не
    поднялся, это UNRESOLVED, а не «недостижимо».

    target — готовая координата вида `svc/<имя>` либо `deploy/<имя>`: часть
    внутренних листенеров не выставлена ни одним Service, и проброс на под —
    единственный способ их опросить. Отсутствие Service не повод не проверять.
    """

    def __init__(self, ns: str, target: str, port: int):
        self.ns, self.target, self.port = ns, target, port
        self.local = _free_port()
        self.proc: subprocess.Popen | None = None
        self.ready = False          # проброс реально принял соединение
        self.error = ""             # почему не принял — называемая причина

    def __enter__(self) -> "PortForward":
        # stderr НЕ выбрасывается: не поднявшийся проброс — это отдельная,
        # называемая причина. Прежде он глушился в /dev/null, `__enter__`
        # возвращал себя в любом случае, и каждая проба получала «Failed to
        # dial: connection refused» — то есть сорванный харнесс выглядел
        # неотличимо от недоступного листенера. Разбор занимал часы, а вывод
        # «листенер не ответил» был просто неверен.
        self.proc = subprocess.Popen(
            ["kubectl", "-n", self.ns, "port-forward", self.target,
             f"{self.local}:{self.port}"],
            stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True,
        )
        deadline = time.time() + 20
        while time.time() < deadline:
            if self.proc.poll() is not None:
                self.error = (self.proc.stderr.read() or "").strip() if self.proc.stderr else ""
                self.error = self.error or f"kubectl port-forward вышел с кодом {self.proc.returncode}"
                return self
            try:
                with socket.create_connection(("127.0.0.1", self.local), 0.3):
                    self.ready = True
                    return self
            except OSError:
                time.sleep(0.2)
        self.error = f"проброс {self.target}:{self.port} не открылся за 20 c"
        return self

    def __exit__(self, *_exc) -> None:
        if self.proc:
            self.proc.terminate()
            try:
                self.proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.proc.kill()


def build_protoset(dest: str) -> str | None:
    """Дескрипторы предмета из `proto/` — тем же деревом, что и перечень методов.

    Нужны потому, что reflection на cluster-internal листенерах закрыт
    НАМЕРЕННО (он перечисляет админскую поверхность), и его отказ приходит
    grpcurl'у как невозможность собрать запрос — то есть как отказ харнесса.
    Открывать reflection ради пробы значило бы ослабить проверяемое, чтобы
    проверка прошла.
    """
    if not shutil.which("buf"):
        return None
    r = subprocess.run(["buf", "build", os.path.join(REPO_ROOT, "proto"), "-o", dest],
                       capture_output=True, text=True, timeout=120)
    return dest if r.returncode == 0 and os.path.exists(dest) else None


def grpc_probe(addr: str, method: str, *, cacert: str, authority: str,
               bearer: str | None = None, cert: str | None = None,
               key: str | None = None, timeout: int = 25,
               protoset: str | None = None, plaintext: bool = False) -> str:
    cmd = ["grpcurl", "-max-time", "15"]
    if protoset:
        cmd += ["-protoset", protoset]
    if plaintext:
        cmd += ["-plaintext"]
    else:
        cmd += ["-cacert", cacert, "-authority", authority]
        if cert and key:
            cmd += ["-cert", cert, "-key", key]
    if bearer:
        cmd += ["-H", f"authorization: Bearer {bearer}"]
    cmd += ["-d", "{}", addr, method]
    try:
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
        return (r.stdout or "") + (r.stderr or "")
    except subprocess.TimeoutExpired:
        return "Timeout expired"
    except FileNotFoundError:
        return "Failed to dial: grpcurl not found in PATH"


# Ответ TLS-стороны, означающий «здесь говорят открытым текстом». Внутренний
# листенер шлюза в dev-посадке поднимается без mTLS, и это не отказ харнесса —
# это другая посадка, о которой отчёт обязан сказать вслух.
_PLAINTEXT_HINT = "first record does not look like a tls handshake"


def _kube_secret_file(ns: str, secret: str, key: str, dest: str) -> bool:
    r = subprocess.run(
        ["kubectl", "-n", ns, "get", "secret", secret, "-o",
         "jsonpath={.data." + key.replace(".", chr(92) + ".") + "}"],
        capture_output=True, text=True, timeout=60,
    )
    if r.returncode != 0 or not r.stdout.strip():
        return False
    import base64
    with open(dest, "wb") as fh:
        fh.write(base64.b64decode(r.stdout.strip()))
    os.chmod(dest, 0o600)
    return True


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--namespace", default=os.environ.get("KACHO_NS", "kacho"))
    ap.add_argument("--workdir", default="/tmp/ban6-gate")
    # --domains — какие домены РАЗВЁРНУТЫ на этом стенде.
    #
    # Зачем понадобилось. Встречный контроль спрашивает внутренний листенер КАЖДОГО
    # домена: «этот Internal-метод у тебя обслужен?». Не дозвонились — домен не
    # засчитывается, и это правильно: «метода нет на внешнем» неотличимо от «метода
    # нет нигде». Но с разнесением e2e по раннерам ни один стенд не несёт все восемь
    # доменов, и гейт стал падать на ЗАКОННОМ отсутствии — то есть требовать
    # измерения там, где предмета нет by design.
    #
    # Сузить набор молча было бы ровно тем классом, который этот гейт и ловит.
    # Поэтому: (а) умолчание — ВСЕ домены (полный стенд ведёт себя как прежде);
    # (б) сужение называется явно и печатается числом вместе с тем, что НЕ измерено;
    # (в) полнота держится снаружи — deploy/scripts/assert-shard-coverage.py требует,
    # чтобы объединение доменов по шардам покрывало все домены дерева. Ни один домен
    # не может выпасть из измерения, оставшись при этом «зелёным».
    ap.add_argument("--domains", default=os.environ.get("BAN6_DOMAINS", ""),
                    help="домены, развёрнутые на этом стенде (через пробел); пусто = все")
    args = ap.parse_args(argv)
    ns, wd = args.namespace, args.workdir
    only = {d for d in args.domains.replace(",", " ").split() if d}
    os.makedirs(wd, exist_ok=True)

    if not shutil.which("grpcurl"):
        print("HARNESS: grpcurl не найден в PATH — проба невозможна", file=sys.stderr)
        return 2

    ca = os.path.join(wd, "ca.crt")
    crt = os.path.join(wd, "client.crt")
    key = os.path.join(wd, "client.key")
    if not _kube_secret_file(ns, "kacho-internal-ca-root", "ca.crt", ca):
        print("HARNESS: не прочитан internal-CA (secret kacho-internal-ca-root)", file=sys.stderr)
        return 2
    # Client-cert — НЕ «если получится». Без него встречный контроль не проводится
    # ни для одного домена, а прогон без встречного контроля не имеет предмета.
    if not (_kube_secret_file(ns, "api-gateway-client-tls", "tls.crt", crt)
            and _kube_secret_file(ns, "api-gateway-client-tls", "tls.key", key)):
        print("HARNESS: не прочитан client-cert (secret api-gateway-client-tls) — "
              "встречный контроль невозможен, а без него вердикт об изоляции пуст",
              file=sys.stderr)
        return 2

    # Дескрипторы предмета. Без них grpcurl спрашивал бы их у сервера
    # (reflection), а на cluster-internal листенерах reflection закрыт намеренно.
    protoset = build_protoset(os.path.join(wd, "kacho.binpb"))
    if not protoset:
        print("HARNESS: не собран protoset (`buf build proto`) — дескрипторы пришлось бы "
              "спрашивать у сервера, а reflection на внутренних листенерах закрыт "
              "намеренно; проба свелась бы к отказу харнесса", file=sys.stderr)
        return 2

    if not os.path.exists(FIXTURES):
        print(f"HARNESS: нет посева {FIXTURES} — без токена проба не различает "
              f"полосы (без него ВСЁ отвечает Unauthenticated)", file=sys.stderr)
        return 2
    bearer = json.load(open(FIXTURES, encoding="utf-8")).get("jwtBootstrap", "")
    if not bearer:
        print("HARNESS: в посеве нет jwtBootstrap", file=sys.stderr)
        return 2

    rows, b6 = subject_rpcs()
    if not rows:
        print("HARNESS: в proto не найдено ни одного service Internal* — предмет пуст",
              file=sys.stderr)
        return 2
    print(f"ПРЕДМЕТ: прочитано .proto {b6['proto_files_read']}, доменов с "
          f"Internal*-контрактом {b6['domains_with_contract']}, из них провязано "
          f"прод-кодом {len(b6['served'])}")
    for dom, svcs in sorted(b6["unserved"].items()):
        print(f"   {dom}: контракт приземлён ({', '.join(svcs)}), но не провязан ни одним "
              f"композиционным корнем — у ban #6 нет предмета; полноту охвата держит "
              f"assert-shard-coverage.py")

    gw_authority = "api-gateway.kacho.svc.cluster.local"
    rc = 0
    with PortForward(ns, "svc/api-gateway", 8443) as ext:
        addr = f"127.0.0.1:{ext.local}"
        # Не поднявшийся проброс — НАЗЫВАЕМАЯ причина, а не «листенер не ответил».
        if not ext.ready:
            print(f"HARNESS: проброс до внешнего листенера не поднялся: {ext.error}",
                  file=sys.stderr)
            return 2

        # ── CONTROL-LIVENESS ────────────────────────────────────────────────
        print("== CONTROL-LIVENESS: внешний листенер маршрутизирует публичный метод ==")
        alive = 0
        for m in LIVENESS_METHODS:
            v, d = classify(grpc_probe(addr, m, cacert=ca, authority=gw_authority,
                                       bearer=bearer, protoset=protoset))
            ok = v == REACHABLE
            alive += 1 if ok else 0
            print(f"  {m:<52} {v:<11} {'OK' if ok else d}")
        if alive == 0:
            print("HARNESS: внешний листенер не ответил НИ НА ОДИН публичный метод — "
                  "любой 'unknown method' ниже удовлетворяется лежащим листенером",
                  file=sys.stderr)
            return 2

        # ── CONTROL-COUNTERPART ─────────────────────────────────────────────
        print("\n== CONTROL-COUNTERPART: тот же метод ОБСЛУЖЕН на своём :9091 ==")
        all_domains = sorted({domain_of(p) for p, _, _ in rows})
        if only:
            unknown = sorted(only - set(all_domains))
            if unknown:
                print(f"HARNESS: --domains называет то, чего нет в дескрипторах: "
                      f"{', '.join(unknown)} — сужение мимо предмета", file=sys.stderr)
                return 2
            domains = [d for d in all_domains if d in only]
            skipped = [d for d in all_domains if d not in only]
        else:
            domains, skipped = all_domains, []
        # Перепись ПЕРЕД вердиктом: «ноль находок» обязано быть отличимо от «ноль
        # осмотренного», а сужение — быть видимым, а не выводимым из тишины.
        print(f"ОХВАТ: доменов в дескрипторах {len(all_domains)}, "
              f"измеряется на этом стенде {len(domains)}"
              + (f"; НЕ развёрнуто здесь ({len(skipped)}): {', '.join(skipped)} — "
                 f"их измеряет другой шард (полноту держит assert-shard-coverage.py)"
                 if skipped else ""))
        confirmed: set[str] = set()
        unconfirmed: list[tuple[str, str]] = []

        def probe_internal(target: str, port: int, authority: str,
                           method: str) -> tuple[str, str, str]:
            """Проба внутреннего листенера: mTLS, при открытом порту — открытым текстом.

            Возвращает (вердикт, деталь, режим). Режим печатается: посадка
            листенера — факт о стенде, который отчёт не вправе замалчивать
            (внутренний листенер шлюза в dev-посадке слушает без mTLS).

            КАЖДАЯ попытка идёт по СВЕЖЕМУ пробросу. Неудачное TLS-рукопожатие
            против открытого порта сервер закрывает, и kubectl-проброс на этом
            умирает — повтор по тому же пробросу получал бы «не дозвонились», то
            есть отказ харнесса вместо ответа. Свой проброс на попытку дороже, но
            даёт ответ вместо артефакта.
            """
            def once(plain: bool) -> str:
                with PortForward(ns, target, port) as pf:
                    if not pf.ready:
                        return f"Failed to dial: проброс не поднялся — {pf.error}"
                    return grpc_probe(
                        f"127.0.0.1:{pf.local}", method, cacert=ca, authority=authority,
                        cert=None if plain else crt, key=None if plain else key,
                        protoset=protoset, plaintext=plain)

            t = once(plain=False)
            if _PLAINTEXT_HINT in t.lower():
                v, d = counterpart_verdict(once(plain=True))
                return v, d, "plaintext"
            v, d = counterpart_verdict(t)
            return v, d, "mTLS"

        hosts_of = b6.get("hosts", {})
        for dom in domains:
            rep = next(((p, s, mm) for p, s, mm in rows if domain_of(p) == dom), None)
            candidates = counterpart_candidates(dom, hosts_of, only)
            if not rep:
                unconfirmed.append((dom, "не из чего собрать пробу"))
                print(f"  {dom:<14} — НЕ ПОДТВЕРЖДЁН: не из чего собрать пробу")
                continue
            if not candidates:
                unconfirmed.append((dom, "не объявлен cluster-internal эндпоинт носителя"))
                print(f"  {dom:<14} — НЕ ПОДТВЕРЖДЁН: ни у одного носителя "
                      f"({', '.join(hosts_of.get(dom, [])) or '—'}) нет эндпоинта "
                      f"в INTERNAL_ENDPOINTS")
                continue
            method = f"{rep[0]}.{rep[1]}/{rep[2]}"
            why: list[str] = []
            for host in candidates:
                target, port, authority = INTERNAL_ENDPOINTS[host]
                # Заведомо ЧУЖОЙ Internal-метод — контроль предпосылки «до прав
                # доходит только зарегистрированный метод». Берётся у домена,
                # которого ЭТОТ НОСИТЕЛЬ не служит, а не просто у другого домена:
                # на листенере vpc метод домена `vpc` зарегистрирован, и такой
                # страж провалил бы предпосылку, ничего о ней не сказав.
                sentinel = next((f"{p}.{s}/{m}" for p, s, m in rows
                                 if host not in hosts_of.get(domain_of(p), [])), None)
                if not sentinel:
                    why.append(f"{host}: не из чего собрать стража предпосылки")
                    continue
                v, d, mode = probe_internal(target, port, authority, method)
                sv, sd, _ = probe_internal(target, port, authority, sentinel)

                # Предпосылка: на ЭТОМ листенере незарегистрированный метод обязан
                # получать отказ маршрутизации. Не получает — из «отказали в правах»
                # нельзя вывести «метод зарегистрирован», и носитель не засчитывается.
                if sv != ABSENT:
                    why.append(f"{host}: предпосылка не выполняется — чужой метод "
                               f"получил '{sv}' ({sd})")
                    continue
                if v == SERVED:
                    confirmed.add(dom)
                    print(f"  {dom:<14} {method:<50} ОБСЛУЖЕН у носителя '{host}' [{mode}] "
                          f"(контроль: чужой метод → {ABSENT})")
                    break
                why.append(f"{host}: {v} — {d}")
            else:
                # Ни один носитель не подтвердил. Перечисляются ВСЕ попытки: «домен
                # не подтверждён» без разбора по носителям неотличимо от «носителя
                # нет на стенде», а это разные беды с разной починкой.
                unconfirmed.append((dom, "; ".join(why)))
                print(f"  {dom:<14} {method:<50} НЕ ПОДТВЕРЖДЁН ни у одного носителя: "
                      f"{'; '.join(why)}")

        # Непокрытый домен РОНЯЕТ прогон. Печатать покрытие числом и идти дальше
        # значит объявлять изолированными методы, про которые не установлено даже
        # того, что они где-то работают.
        if unconfirmed:
            print(f"\nHARNESS: встречный контроль НЕ подтверждён для доменов: "
                  f"{', '.join(d for d, _ in unconfirmed)}", file=sys.stderr)
            for d, why in unconfirmed:
                print(f"  {d}: {why}", file=sys.stderr)
            print("Для этих доменов «метода нет на внешнем листенере» неотличимо от "
                  "«метода нет нигде» — это не изоляция, а отсутствие измерения.",
                  file=sys.stderr)
            return 2

        # ── ПРЕДМЕТ ─────────────────────────────────────────────────────────
        print("\n== ПРЕДМЕТ: каждый Internal*-метод на внешнем листенере ==")
        isolated, violations, unresolved, inconclusive = 0, [], [], []
        # Метод НЕ развёрнутого домена из предмета исключается: он «недостижим на
        # внешнем листенере» просто потому, что его сервиса нет на кластере, и
        # засчитать это в изоляцию значило бы получить зелёное из отсутствия.
        rows = [r for r in rows if not only or domain_of(r[0]) in only]
        for pkg, svc, meth in rows:
            method = f"{pkg}.{svc}/{meth}"
            v, d = classify(grpc_probe(addr, method, cacert=ca,
                                       authority=gw_authority, bearer=bearer,
                                       protoset=protoset))
            if v == ISOLATED:
                isolated += 1
            elif v == REACHABLE:
                violations.append((method, d))
                print(f"  ДОСТИЖИМ    {method}  → {d}")
            elif v == INCONCLUSIVE:
                inconclusive.append((method, d))
                print(f"  НЕ ПРОВЕРЕН {method}  → {d}")
            else:
                unresolved.append((method, d))
                print(f"  НЕ РАЗРЕШЁН {method}  → {d}")

    total = len(rows)
    cover = sum(1 for p, _, _ in rows if domain_of(p) in confirmed)
    print(f"\nпроверено Internal*-методов: {total} (сервисов: "
          f"{len({(p, s) for p, s, _ in rows})}, доменов: {len(domains)})")
    print(f"недостижимо на внешнем листенере: {isolated}")
    # Досюда прогон доходит только когда встречный контроль подтверждён для ВСЕХ
    # доменов — непокрытый роняет его выше. Поэтому число здесь равно total и
    # печатается как подтверждение, а не как «покрытие, которое ни на что не влияет».
    print(f"из них со встречным контролем домена: {cover}/{total} "
          f"(подтверждено доменов: {len(confirmed)}/{len(domains)} — "
          f"{' '.join(sorted(confirmed))})")
    print(f"достижимо (нарушение ban #6): {len(violations)}")
    print(f"НЕ проверено (authz ответил раньше резолвера): {len(inconclusive)}")
    print(f"отказ харнесса (не вердикт): {len(unresolved)}")
    if violations:
        print("FAIL: Internal*-методы отвечают на advertised external листенере: "
              + ", ".join(m for m, _ in violations))
        rc = 1
    if inconclusive:
        print("FAIL: проба не дошла до решения о маршрутизации — отказ в правах "
              "ответил раньше резолвера, поэтому про эти методы НИЧЕГО не доказано "
              "(и засчитать их в изоляцию нельзя): "
              + ", ".join(m for m, _ in inconclusive))
        rc = 3 if rc == 0 else rc
    if unresolved:
        print("FAIL: пробы без ответа по существу — харнесс, а не изоляция: "
              + ", ".join(m for m, _ in unresolved))
        rc = 2 if rc == 0 else rc
    if rc == 0:
        print(f"OK: BAN6-EXT-GRPC — {isolated}/{total} Internal*-методов недостижимы "
              f"на advertised external листенере при живом публичном контроле")
    return rc


# ─────────────────────────── самопроверка ────────────────────────────────────


def self_test() -> int:
    """Гейт обязан краснеть на внесённом дефекте и молчать на законной записи той же формы.

    Инъекции идут по РАСШИФРОВКАМ проб — стенд не нужен: предмет самопроверки не
    «работает ли кластер», а «различает ли классификатор ответ по существу от
    отказа маршрутизации и от отказа харнесса».
    """
    rc = 0

    def check(label: str, transcript: str, expect: str) -> None:
        nonlocal rc
        got, detail = classify(transcript)
        ok = got == expect
        mark = "ОК" if ok else f"ПРОВАЛ (ждали {expect}, получили {got})"
        print(f"  {label:<58} → {got:<11} {mark}  [{detail}]")
        rc |= 0 if ok else 1

    print("самопроверка классификатора:")
    # (0) законная запись: шлюз отказал в маршрутизации — гейт обязан молчать
    check("(0) NotFound + unknown method (штатная изоляция)",
          "ERROR:\n  Code: NotFound\n  Message: unknown method: "
          "/kaname.cloud.iam.v1.InternalIAMService/Check", ISOLATED)
    check("(0b) Unimplemented (та же форма у другого прокси)",
          "ERROR:\n  Code: Unimplemented\n  Message: unknown service", ISOLATED)

    # (A) ИНЪЕКЦИЯ: метод отмаршрутизирован и дошёл до владельца
    check("(A) InvalidArgument от владельца — метод ДОШЁЛ",
          "ERROR:\n  Code: InvalidArgument\n  Message: Illegal argument subject_id: required",
          REACHABLE)
    check("(A3) успешный ответ телом — метод ДОШЁЛ",
          '{\n  "regions": [\n    {\n      "id": "ru-central1"\n    }\n  ]\n}', REACHABLE)

    # (B) КОНТРОЛЬ: отказ харнесса НЕ должен читаться как изоляция
    check("(B) grpcurl не собрал запрос (опечатка в имени метода)",
          'Error invoking method "x/Y": service "x" does not include a method named "Y"',
          UNRESOLVED)
    check("(B2) не дозвонились",
          "Failed to dial target host \"127.0.0.1:1\": connection refused", UNRESOLVED)
    check("(B3) таймаут пробы", "Timeout expired", UNRESOLVED)

    # (C) КОНТРОЛЬ: Unauthenticated — НЕ изоляция. Так отвечают ВСЕ методы без
    #     токена, включая публичные: принять это за «недостижим» значит объявить
    #     весь листенер изолированным при протухшем токене.
    check("(C) Unauthenticated (нет/протух токен) — не вердикт",
          "ERROR:\n  Code: Unauthenticated\n  Message: unauthenticated: credentials required",
          UNRESOLVED)

    # (D) КОНТРОЛЬ: NotFound ПО СУЩЕСТВУ (владелец не нашёл объект) — это ответ
    #     дошедшего метода, а не отказ маршрутизации. Отличается телом сообщения.
    check("(D) NotFound от владельца (объект не найден) — метод ДОШЁЛ",
          "ERROR:\n  Code: NotFound\n  Message: Region rgn-zzz not found", REACHABLE)

    # (E) ГЛАВНАЯ ИНЪЕКЦИЯ КЛАССА: authz шлюза отвечает РАНЬШЕ резолвера, поэтому
    #     отказ в правах не говорит ни об изоляции, ни о достижимости. Засчитать
    #     его в пользу изоляции — ровно «проверка с формой, но без содержания»:
    #     гейт позеленел бы на методе, которого он не проверил.
    check("(E) PermissionDenied — до решения о маршруте НЕ дошли",
          "ERROR:\n  Code: PermissionDenied\n  Message: permission denied: storage.volumes.attach",
          INCONCLUSIVE)

    # (F) КОНТРОЛЬ формы: тот же код, но текст отказа принадлежит ВЛАДЕЛЬЦУ,
    #     а не каталогу шлюза. Классификатор не обязан их различать — оба
    #     одинаково не доходят до резолвера, и оба обязаны остаться INCONCLUSIVE.
    check("(F) PermissionDenied тоном владельца — тоже не вердикт",
          "ERROR:\n  Code: PermissionDenied\n  Message: no path", INCONCLUSIVE)

    print("\nсамопроверка учёта (INCONCLUSIVE не имеет права засчитываться в изоляцию):")
    verdicts = [classify(t)[0] for t in (
        "ERROR:\n  Code: NotFound\n  Message: unknown method: /x/Y",
        "ERROR:\n  Code: PermissionDenied\n  Message: permission denied: storage.volumes.attach",
    )]
    ok = verdicts == [ISOLATED, INCONCLUSIVE]
    print(f"  {verdicts} — {'ОК' if ok else 'ПРОВАЛ'}")
    rc |= 0 if ok else 1

    # ── ВСТРЕЧНЫЙ КОНТРОЛЬ: отдельный вопрос, отдельный вердикт ──────────────
    # На cluster-internal листенере порядок ОБРАТНЫЙ внешнему: диспетчер gRPC
    # отклоняет незарегистрированный метод ДО интерсепторов, поэтому отказ в
    # правах там доказывает регистрацию. Если бы встречный контроль пользовался
    # внешним классификатором, он засчитывал бы обслуженными только те методы,
    # что ответили по существу, — а таких на закрытом периметре меньшинство, и
    # именно поэтому подтверждался один домен из восьми.
    print("\nсамопроверка встречного контроля (:9091 — свой порядок обработки):")

    def check_cp(label: str, transcript: str, expect: str) -> None:
        nonlocal rc
        got, detail = counterpart_verdict(transcript)
        ok = got == expect
        mark = "ОК" if ok else f"ПРОВАЛ (ждали {expect}, получили {got})"
        print(f"  {label:<58} → {got:<18} {mark}  [{detail}]")
        rc |= 0 if ok else 1

    check_cp("(G) отказ в правах на своём листенере — метод ЕСТЬ",
             "ERROR:\n  Code: PermissionDenied\n  Message: permission denied", SERVED)
    check_cp("(G2) ответ владельца по существу — метод ЕСТЬ",
             "ERROR:\n  Code: InvalidArgument\n  Message: subject required", SERVED)
    check_cp("(H) unknown service — метода на листенере НЕТ",
             "ERROR:\n  Code: Unimplemented\n  Message: unknown service "
             "kacho.cloud.vpc.v1.InternalAddressPoolService", ABSENT)
    # Контроль: отказ харнесса не имеет права читаться как «обслужен». Именно в
    # эту полосу уходил закрытый reflection — grpcurl не мог собрать запрос.
    check_cp("(H2) reflection закрыт — дескриптор не получен: отказ ХАРНЕССА",
             'Error invoking method "x/Y": rpc error: code = PermissionDenied desc = '
             'failed to query for service descriptor "x": permission denied (rpc not mapped)',
             UNRESOLVED)
    check_cp("(H3) не дозвонились — отказ харнесса, а не «обслужен»",
             'Failed to dial target host "127.0.0.1:1": connection refused', UNRESOLVED)

    print("\nсамопроверка предпосылки встречного контроля:")
    # Предпосылка «до прав доходит только зарегистрированный метод» ВЕРНА лишь
    # там, где незарегистрированный получает отказ маршрутизации. Контрольная
    # проба чужим методом это и проверяет; вот обе её стороны.
    sentinel_ok, _ = counterpart_verdict(
        "ERROR:\n  Code: Unimplemented\n  Message: unknown service kaname.cloud.iam.v1.InternalIAMService")
    sentinel_bad, _ = counterpart_verdict(
        "ERROR:\n  Code: PermissionDenied\n  Message: permission denied")
    ok = sentinel_ok == ABSENT and sentinel_bad != ABSENT
    print(f"  чужой метод → {sentinel_ok}; если бы он отвечал отказом в правах → "
          f"{sentinel_bad} (предпосылка сорвана, домен не засчитывается) — "
          f"{'ОК' if ok else 'ПРОВАЛ'}")
    rc |= 0 if ok else 1

    print("\nсамопроверка карты внутренних эндпоинтов:")
    # Домен без эндпоинта роняет прогон, поэтому карта обязана покрывать каждый
    # домен ПРЕДМЕТА. Предмет — не «все домены с Internal*-контрактом», а те, чей
    # контракт кто-то регистрирует: у эндпоинта непровязанного домена нет
    # координаты by construction, и требовать её значило бы требовать адреса того,
    # чего нет. Полноту охвата держит assert-shard-coverage.py — он же покраснеет
    # в день, когда домен провяжут и его никто не возьмёт.
    subject, b6 = subject_rpcs()
    doms = {domain_of(p) for p, _, _ in subject}
    hosts_of = b6.get("hosts", {})
    # Спрашивается не «есть ли эндпоинт у домена» — у поперечного домена своего
    # листенера нет вовсе, — а «есть ли эндпоинт хоть у ОДНОГО его носителя».
    gap = sorted(d for d in doms
                 if not [h for h in hosts_of.get(d, []) if h in INTERNAL_ENDPOINTS])
    print(f"  доменов с Internal*-контрактом: {b6['domains_with_contract']}; "
          f"из них провязано прод-кодом: {len(b6['served'])}; носителей в карте: "
          f"{len(INTERNAL_ENDPOINTS)}; без эндпоинта хотя бы у одного носителя: "
          f"{', '.join(gap) if gap else 'нет'} — {'ПРОВАЛ' if gap else 'ОК'}")
    for d in sorted(doms):
        hs = hosts_of.get(d, [])
        if len(hs) > 1:
            print(f"    поперечный домен: {d} служат {len(hs)} носителей "
                  f"({', '.join(hs)}) — своего листенера у него нет, встречный "
                  f"контроль идёт перебором")
    for dom, svcs in sorted(b6["unserved"].items()):
        print(f"    вне предмета: {dom} ({', '.join(svcs)}) — не провязан ни одним "
              f"композиционным корнем")
    rc |= 1 if gap else 0

    # ОБРАТНАЯ СТОРОНА, без которой сужение выше было бы маской: домен, чей
    # cluster-internal эндпоинт МЫ ЗНАЕМ, обязан быть признан провязанным. Не
    # признан — значит сломался предикат провязки (например, регистрацию завели
    # через обёртку), и сужение начало съедать настоящий предмет. Это находка
    # здесь, а не тихо уменьшившийся охват на живом стенде.
    known = set(INTERNAL_ENDPOINTS)
    serving = {h for d in b6["served"] for h in hosts_of.get(d, [])}
    lost = sorted(known - serving)
    print(f"  контроль в обратную сторону: носителей с известным эндпоинтом {len(known)}; "
          f"не служат НИ ОДНОГО провязанного домена: {', '.join(lost) if lost else 'нет'} — "
          f"{'ПРОВАЛ' if lost else 'ОК'}")
    rc |= 1 if lost else 0

    # ПРЕДПОСЫЛКА ПЕРЕЧИСЛЕНИЯ: каждый провязанный домен обязан быть ПРЕДСТАВЛЕН в
    # предмете хотя бы одним методом. Не представлен — значит образец перестал
    # видеть его контракт (ровно тот дефект, из которого выведен общий предикат:
    # образец адресовал только раскладку `v1/`). Без этой стороны сужение
    # предмета проверялось бы лишь там, где эндпоинт уже известен, и возврат
    # узкого образца прошёл бы молча.
    missing = sorted(set(b6["served"]) - doms)
    print(f"  предпосылка перечисления: провязанных доменов {len(b6['served'])}; "
          f"нет ни одного метода в предмете: {', '.join(missing) if missing else 'нет'} — "
          f"{'ПРОВАЛ' if missing else 'ОК'}")
    rc |= 1 if missing else 0

    print("\nсамопроверка перечисления предмета:")
    rows = internal_rpcs()
    ok = len(rows) > 0 and all(s.startswith("Internal") for _, s, _ in rows)
    print(f"  Internal*-методов в дереве: {len(rows)}; из них в предмете: {len(subject)}; "
          f"все сервисы Internal* — {'ОК' if ok else 'ПРОВАЛ'}")
    rc |= 0 if ok else 1

    print("\nсамопроверка: " + ("ПРОЙДЕНА" if rc == 0 else "ПРОВАЛЕНА"))
    return rc


if __name__ == "__main__":
    if "--self-test" in sys.argv[1:]:
        sys.exit(self_test())
    sys.exit(main([a for a in sys.argv[1:]]))
