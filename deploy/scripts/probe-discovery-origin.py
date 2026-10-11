#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""probe-discovery-origin.py — документ обнаружения на внешнем входе консоли
называет происхождение КОНСОЛИ, а не имя стенда разработки (kacho#3113).

    probe-discovery-origin.py [--self-test]

Окружение — файл проб стенда (`stand-ns.sh forward`) либо те же имена рабочего
стенда:
    KACHO_CONSOLE_URL      происхождение консоли (https://<имя>[:порт])
    KACHO_CONSOLE_HOST_IP  адрес, на который разрешать имя (проброс); пусто — DNS
    KACHO_CONSOLE_CA       якорь доверия листа консоли; пусто — системные корни

Что утверждается. GET <консоль>/.well-known/oauth-authorization-server → 200 и
`application/json`; знаменатель — ВСЕ строковые значения документа, являющиеся
адресами (http/https), а не выписанный перечень ключей: адрес, появившийся в
документе позже, судится без правки пробы. Каждый из N обязан стоять на
происхождении консоли (схема, имя, порт), и ни один не несёт имени стенда
разработки (`kacho.local`). Ноль адресов — находка, а не «всё совпало».

Адреса стенда не печатаются: перепись — числами и ключами документа.

Коды: 0 — N из N на происхождении консоли; 1 — находка; 2 — проба не исполнилась
(нет окружения, вход не ответил, ответ не JSON) — это «не выполнилось», а не
красное.
"""
import json
import os
import ssl
import sys
import urllib.parse
import urllib.request

DISCOVERY = "/.well-known/oauth-authorization-server"
DEV_NAME = "kacho.local"


def origin_of(url):
    u = urllib.parse.urlsplit(url)
    port = u.port or {"https": 443, "http": 80}.get(u.scheme)
    return (u.scheme, (u.hostname or "").lower(), port)


def addresses(doc, path=""):
    """Все строковые значения-адреса документа: (ключ, значение)."""
    if isinstance(doc, dict):
        for k, v in doc.items():
            yield from addresses(v, f"{path}.{k}" if path else k)
    elif isinstance(doc, list):
        for i, v in enumerate(doc):
            yield from addresses(v, f"{path}[{i}]")
    elif isinstance(doc, str) and doc.lower().startswith(("http://", "https://")):
        yield path, doc


def judge(console, doc):
    """(найдено адресов, на происхождении консоли, находки) — тело пробы."""
    want = origin_of(console)
    found = list(addresses(doc))
    findings, agree = [], 0
    for key, val in found:
        if DEV_NAME in val.lower():
            findings.append(f"{key}: несёт имя стенда разработки")
        elif origin_of(val) != want:
            findings.append(f"{key}: стоит не на происхождении консоли")
        else:
            agree += 1
    if not found:
        findings.append("в документе НОЛЬ адресов — судить нечего, и это не «все совпали»")
    return len(found), agree, findings


def self_test():
    console = "https://console.probe.test"
    good = {"issuer": console, "authorization_endpoint": console + "/oauth2/authorize",
            "token_endpoint": console + "/oauth2/token", "grant_types_supported": ["client_credentials"]}
    cases = [
        ("законный близнец: три адреса на консоли", good, 0),
        ("издатель стенда разработки", dict(good, issuer="https://kaname.kacho.local"), 1),
        ("точка на чужом имени", dict(good, token_endpoint="https://other.probe.test/oauth2/token"), 1),
        ("точка на другом порту", dict(good, authorization_endpoint=console + ":8443/oauth2/authorize"), 1),
        ("адресов ноль", {"grant_types_supported": ["client_credentials"]}, 1),
        ("вложенный адрес на имени разработки", dict(good, extra={"jwks": ["https://x.kacho.local/k"]}), 1),
    ]
    bad = 0
    for name, doc, want in cases:
        n, ok, findings = judge(console, doc)
        got = 1 if findings else 0
        mark = "ok  " if got == want else "FAIL"
        bad += got != want
        print(f"  {mark} {name}: адресов {n}, совпало {ok}, находок {len(findings)}")
    if judge(console, good)[0] != 3:
        print("  FAIL знаменатель законного близнеца не 3")
        bad += 1
    print(f"самопроверка probe-discovery-origin: провалено {bad}")
    return 1 if bad else 0


def main():
    if sys.argv[1:] == ["--self-test"]:
        return self_test()
    console = os.environ.get("KACHO_CONSOLE_URL", "").rstrip("/")
    if not console:
        print("ABORT: KACHO_CONSOLE_URL не задан — спрашивать некого", file=sys.stderr)
        return 2
    u = urllib.parse.urlsplit(console)
    host_ip = os.environ.get("KACHO_CONSOLE_HOST_IP", "")
    ctx = ssl.create_default_context(cafile=os.environ.get("KACHO_CONSOLE_CA") or None)
    if host_ip:
        # Имя консоли разрешается на проброс; SNI и сверка листа — по имени.
        import socket
        real = socket.getaddrinfo

        def resolve(host, *a, **kw):
            return real(host_ip if host == u.hostname else host, *a, **kw)
        socket.getaddrinfo = resolve
    req = urllib.request.Request(console + DISCOVERY, headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=20) as r:
            status, ctype, body = r.status, r.headers.get("Content-Type", ""), r.read()
    except Exception as e:  # noqa: BLE001 — любой отказ входа: проба не исполнилась
        print(f"ABORT: вход консоли не ответил на {DISCOVERY}: {type(e).__name__}", file=sys.stderr)
        return 2
    if status != 200 or not ctype.startswith("application/json"):
        print(f"находка: {DISCOVERY} ответил {status} {ctype!r}, а не 200 application/json")
        return 1
    try:
        doc = json.loads(body)
    except ValueError:
        print(f"ABORT: тело {DISCOVERY} не JSON", file=sys.stderr)
        return 2
    n, ok, findings = judge(console, doc)
    keys = ", ".join(k for k, _ in addresses(doc))
    print(f"перепись: {DISCOVERY} 200 application/json · адресов в документе {n} ({keys}) · "
          f"на происхождении консоли {ok} из {n} · несут имя стенда разработки "
          f"{sum(1 for f in findings if 'разработки' in f)}")
    for f in findings:
        print(f"находка: {f}")
    return 1 if findings else 0


if __name__ == "__main__":
    sys.exit(main())
