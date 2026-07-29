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
2. **CONTROL-COUNTERPART** — для каждого домена хотя бы один Internal*-метод
   обязан быть ДОКАЗАННО обслужен на своём cluster-internal листенере (:9091,
   mTLS) в том же прогоне. Без этого «метода нет на внешнем» неотличимо от
   «метода нет нигде» — та самая пустота, ради которой контроль и заведён.
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
import json
import os
import re
import shutil
import socket
import subprocess
import sys
import time

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
PROTO_GLOB = os.path.join(REPO_ROOT, "proto", "kacho", "cloud", "*", "v1", "*.proto")
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
# контроля). Домен без эндпоинта не «пропускается молча»: его методы считаются
# отдельно как counterpart-unconfirmed и это печатается числом.
INTERNAL_ENDPOINTS = {
    "iam": ("kacho-iam-internal", 9091),
    "geo": ("kacho-geo-internal", 9091),
    "loadbalancer": ("kacho-nlb-internal", 9091),
    "registry": ("registry-internal", 9091),
    "vpc": ("vpc", 9091),
    "compute": ("compute", 9091),
    "storage": ("kacho-storage", 9091),
}

# Публичные методы для CONTROL-LIVENESS. Берём read-RPC двух разных доменов:
# один домен мог бы лежать сам по себе, и тогда контроль сообщил бы про стенд, а
# не про листенер.
LIVENESS_METHODS = [
    "kacho.cloud.geo.v1.RegionService/List",
    "kacho.cloud.iam.v1.UserService/List",
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


def internal_rpcs(proto_glob: str = PROTO_GLOB) -> list[tuple[str, str, str]]:
    """(package, ServiceName, MethodName) для каждого `service Internal*` в proto."""
    rows: list[tuple[str, str, str]] = []
    for path in sorted(glob.glob(proto_glob)):
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


# ─────────────────────────── живой прогон ────────────────────────────────────


def _free_port() -> int:
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


class PortForward:
    """kubectl port-forward на время блока. Проброс — часть харнесса: если он не
    поднялся, это UNRESOLVED, а не «недостижимо»."""

    def __init__(self, ns: str, svc: str, port: int):
        self.ns, self.svc, self.port = ns, svc, port
        self.local = _free_port()
        self.proc: subprocess.Popen | None = None

    def __enter__(self) -> "PortForward":
        self.proc = subprocess.Popen(
            ["kubectl", "-n", self.ns, "port-forward", f"svc/{self.svc}",
             f"{self.local}:{self.port}"],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        )
        deadline = time.time() + 15
        while time.time() < deadline:
            try:
                with socket.create_connection(("127.0.0.1", self.local), 0.3):
                    return self
            except OSError:
                time.sleep(0.2)
        return self

    def __exit__(self, *_exc) -> None:
        if self.proc:
            self.proc.terminate()
            try:
                self.proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.proc.kill()


def grpc_probe(addr: str, method: str, *, cacert: str, authority: str,
               bearer: str | None = None, cert: str | None = None,
               key: str | None = None, timeout: int = 25) -> str:
    cmd = ["grpcurl", "-cacert", cacert, "-authority", authority, "-max-time", "15"]
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
    args = ap.parse_args(argv)
    ns, wd = args.namespace, args.workdir
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
    have_client = (_kube_secret_file(ns, "api-gateway-client-tls", "tls.crt", crt)
                   and _kube_secret_file(ns, "api-gateway-client-tls", "tls.key", key))

    if not os.path.exists(FIXTURES):
        print(f"HARNESS: нет посева {FIXTURES} — без токена проба не различает "
              f"полосы (без него ВСЁ отвечает Unauthenticated)", file=sys.stderr)
        return 2
    bearer = json.load(open(FIXTURES, encoding="utf-8")).get("jwtBootstrap", "")
    if not bearer:
        print("HARNESS: в посеве нет jwtBootstrap", file=sys.stderr)
        return 2

    rows = internal_rpcs()
    if not rows:
        print("HARNESS: в proto не найдено ни одного service Internal* — предмет пуст",
              file=sys.stderr)
        return 2

    gw_authority = "api-gateway.kacho.svc.cluster.local"
    rc = 0
    with PortForward(ns, "api-gateway", 8443) as ext:
        addr = f"127.0.0.1:{ext.local}"

        # ── CONTROL-LIVENESS ────────────────────────────────────────────────
        print("== CONTROL-LIVENESS: внешний листенер маршрутизирует публичный метод ==")
        alive = 0
        for m in LIVENESS_METHODS:
            v, d = classify(grpc_probe(addr, m, cacert=ca, authority=gw_authority,
                                       bearer=bearer))
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
        domains = sorted({domain_of(p) for p, _, _ in rows})
        confirmed: set[str] = set()
        for dom in domains:
            ep = INTERNAL_ENDPOINTS.get(dom)
            rep = next(((p, s, mm) for p, s, mm in rows if domain_of(p) == dom), None)
            if not ep or not rep or not have_client:
                print(f"  {dom:<14} — встречный контроль НЕ проведён "
                      f"({'нет internal-эндпоинта' if not ep else 'нет client-cert'})")
                continue
            svc, port = ep
            with PortForward(ns, svc, port) as pf:
                method = f"{rep[0]}.{rep[1]}/{rep[2]}"
                v, d = classify(grpc_probe(
                    f"127.0.0.1:{pf.local}", method, cacert=ca,
                    authority=f"{svc}.{ns}.svc.cluster.local", cert=crt, key=key))
            served = v == REACHABLE
            if served:
                confirmed.add(dom)
            print(f"  {dom:<14} {method:<58} {'ОБСЛУЖЕН' if served else 'НЕ подтверждён: ' + d}")
        if not confirmed:
            print("HARNESS: ни один Internal*-метод не подтверждён обслуженным на своём "
                  "listener — 'нет на внешнем' неотличимо от 'нет нигде'", file=sys.stderr)
            return 2

        # ── ПРЕДМЕТ ─────────────────────────────────────────────────────────
        print("\n== ПРЕДМЕТ: каждый Internal*-метод на внешнем листенере ==")
        isolated, violations, unresolved, inconclusive = 0, [], [], []
        for pkg, svc, meth in rows:
            method = f"{pkg}.{svc}/{meth}"
            v, d = classify(grpc_probe(addr, method, cacert=ca,
                                       authority=gw_authority, bearer=bearer))
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
    print(f"из них со встречным контролем домена: {cover} "
          f"(подтверждённые домены: {' '.join(sorted(confirmed)) or '—'})")
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
          "/kacho.cloud.iam.v1.InternalIAMService/Check", ISOLATED)
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

    print("\nсамопроверка перечисления предмета:")
    rows = internal_rpcs()
    ok = len(rows) > 0 and all(s.startswith("Internal") for _, s, _ in rows)
    print(f"  Internal*-методов найдено: {len(rows)}; все сервисы Internal* — "
          f"{'ОК' if ok else 'ПРОВАЛ'}")
    rc |= 0 if ok else 1

    print("\nсамопроверка: " + ("ПРОЙДЕНА" if rc == 0 else "ПРОВАЛЕНА"))
    return rc


if __name__ == "__main__":
    if "--self-test" in sys.argv[1:]:
        sys.exit(self_test())
    sys.exit(main([a for a in sys.argv[1:]]))
