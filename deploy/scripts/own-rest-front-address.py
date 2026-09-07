#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Адрес СОБСТВЕННОГО REST-фронта службы доступа — из объявления посадки.

ПРЕДМЕТ. У службы два своих REST-фронта: публичный и внутренний. Харнесс
сквозных проб обязан знать их адреса, иначе кейсы собственного фронта мерили бы
край платформы вместо предмета — и правильно отказываются это делать.

ПОЧЕМУ АДРЕС ЧИТАЕТСЯ, А НЕ ВЫПИСЫВАЕТСЯ. Номер порта и транспорт слушателя
объявляет ПОСАДКА (чарт службы: `service.public.restPort`,
`service.internal.internalRestPort`, `mtls.httpListeners.publicRest`,
`mtls.httpListeners.internalRest`). Выписанный в прогонщике литерал — второе
место об одном предмете: он разойдётся с чартом МОЛЧА, и разойдётся ровно там,
где расхождение не видно — прогон продолжит идти по прежнему адресу и назовёт
чужой ответ ответом фронта.

ПОРТ БЕРЁТСЯ ПО ИМЕНИ, А НЕ ПО НОМЕРУ. Имя порта (`http-rest`, `http-rest-int`)
— координата, которую объявляет чарт и которая переживает смену номера; номер
объявлением не является. Сверка по номеру означала бы «я знаю, каким он должен
быть», то есть тот же выписанный литерал, только спрятанный в предикате.

СХЕМА — ЧАСТЬ АДРЕСА, И ОНА ЧИТАЕТСЯ ТЕМ ЖЕ ОБРАЩЕНИЕМ, ЧТО И ПОРТ. Транспорт
слушателя — свойство ПРОЦЕССА (переменная `KANAME_REST_SERVER_MTLS_ENABLE` /
`KANAME_INTERNALREST_SERVER_MTLS_ENABLE`), и он МЕНЯЕТСЯ вместе с профилем: на
стенде разработки фронты открыты, на боевой посадке (`values.dev-prod.yaml`:
`publicRest: true`, `internalRest: true`) — под TLS. Обращение открытым текстом
к слушателю под TLS ответа не даёт вовсе, поэтому схема, выписанная здесь
открытым текстом, сделала бы фронт недоступным харнессу BY CONSTRUCTION — ровно
тот дефект, что чинился у гейта опроса счётчиков (#2165).
Из НОМЕРА порта схема не выводится: слушатель меняет транспорт, номера не меняя.

Оба объекта читаются ОДНИМ обращением (`kubectl get svc/... deploy/... -o json`):
между двумя запросами посадка сменится, и тогда порт будет от одной, а схема от
другой — пара, которой не существовало ни в один момент времени.

ИСХОДОВ ТРИ, И ВТОРОЙ НЕ РАВЕН ТРЕТЬЕМУ:

  0 — адрес разрешён, на stdout `<схема>|<порт>|<имя Service>`. Имя Service тоже
      отдаётся отсюда: вызывающему оно нужно для проброса, и выведи он его сам
      («к внутреннему припиши -internal»), раскладка фронтов оказалась бы
      объявленной в двух местах;
  3 — УСЛОВИЕ НЕ СОЗДАНО: посадки нет, службы нет, фронт в ней не объявлен либо
      спросить не удалось. Адрес НЕ выдумывается: вызывающий обязан НЕ
      инъектировать переменную, и тогда кейс сам скажет третьим исходом, что
      условия нет (`services/iam/tests/newman/scripts/gen.py::require_env_url`);
  1 — дефект ВЫЗОВА: неизвестное имя фронта. Это находка о дереве, а не о стенде.

Подставить сюда адрес края платформы — хуже красноты: кейс позеленеет, проверив
чужую поверхность, и будет утверждать о предмете, которого не касался.

Перепись печатается в stderr на каждом прогоне: «ноль находок» обязано быть
отличимо от «ноль прочитанного».

Самопроверка: `--self-test` (подставной `kubectl` на PATH, инъекция по каждой оси
с законным близнецом).
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys

# Раскладка фронта: имя Service, имя порта в нём и переменная процесса, которой
# посадка объявляет ТРАНСПОРТ этого слушателя. Все три — координаты чарта
# (deploy/helm/umbrella/charts/kaname/templates/service-{public,internal}.yaml и
# deployment.yaml), а не догадки о номерах.
FRONTS = {
    "public": {
        "service_suffix": "",
        "port_name": "http-rest",
        "tls_env": "KANAME_REST_SERVER_MTLS_ENABLE",
        "what": "собственный публичный REST-фронт службы",
    },
    "internal": {
        "service_suffix": "-internal",
        "port_name": "http-rest-int",
        "tls_env": "KANAME_INTERNALREST_SERVER_MTLS_ENABLE",
        "what": "собственный внутренний REST-фронт службы",
    },
}

RC_OK = 0
RC_CALLER_DEFECT = 1
RC_PRECONDITION_MISSING = 3


def _kubectl(ns: str, service: str, deployment: str, timeout: int) -> dict | None:
    """Один запрос за ОБОИМИ объектами. None — спросить не удалось."""
    cmd = ["kubectl", "-n", ns, "get", f"svc/{service}", f"deploy/{deployment}", "-o", "json"]
    try:
        out = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired) as exc:
        print(f"[фронт] посадку спросить не удалось: {exc}", file=sys.stderr)
        return None
    if out.returncode != 0:
        first = (out.stderr or "").strip().splitlines()
        print(f"[фронт] посадку спросить не удалось: {first[0] if first else 'kubectl rc=' + str(out.returncode)}",
              file=sys.stderr)
        return None
    try:
        return json.loads(out.stdout)
    except json.JSONDecodeError as exc:
        print(f"[фронт] ответ посадки неразбираем: {exc}", file=sys.stderr)
        return None


def _pick(doc: dict, kind: str) -> dict | None:
    for item in doc.get("items", []) or []:
        if item.get("kind") == kind:
            return item
    return None


def resolve(doc: dict, front: str, container: str) -> tuple[str | None, str]:
    """(«<схема>|<порт>|<имя Service>» либо None, строка переписи).

    Судит РАЗОБРАННЫЙ ответ, а не текст: имя порта встречается и в комментариях
    шаблонов, и в именах контейнерных портов — сверка по подстроке нашла бы их
    и назвала бы адресом то, что адресом не является.
    """
    spec = FRONTS[front]
    svc = _pick(doc, "Service")
    dep = _pick(doc, "Deployment")
    census = []

    if svc is None:
        return None, "Service в ответе посадки нет"
    ports = (svc.get("spec") or {}).get("ports") or []
    census.append(f"портов Service {len(ports)}")
    port = None
    for p in ports:
        if p.get("name") == spec["port_name"]:
            port = p.get("port")
            break
    if port is None:
        names = ",".join(str(p.get("name")) for p in ports) or "нет ни одного"
        return None, (f"порт с именем {spec['port_name']!r} у Service не объявлен "
                      f"(объявлены: {names})")
    census.append(f"порт {spec['port_name']}={port}")

    # ТРАНСПОРТ. Незаданная переменная означает открытый текст — это умолчание
    # САМОГО объявления (чарт эмитит блок только при поднятой ручке слушателя), и
    # оно ПЕЧАТАЕТСЯ, а не подставляется молча.
    if dep is None:
        return None, "Deployment в ответе посадки нет — транспорт слушателя неизвестен"
    containers = (((dep.get("spec") or {}).get("template") or {}).get("spec") or {}).get("containers") or []
    env = {}
    for c in containers:
        if c.get("name") == container:
            env = {e.get("name"): e.get("value") for e in (c.get("env") or [])}
            break
    else:
        names = ",".join(str(c.get("name")) for c in containers) or "нет ни одного"
        return None, (f"контейнера {container!r} в Deployment нет (есть: {names}) — "
                      f"транспорт слушателя неизвестен")
    raw = env.get(spec["tls_env"])
    if raw is None:
        scheme, how = "http", "умолчанием объявления (ручка транспорта не эмитирована)"
    elif str(raw).strip().lower() == "true":
        scheme, how = "https", f"{spec['tls_env']}={raw}"
    else:
        scheme, how = "http", f"{spec['tls_env']}={raw}"
    census.append(f"схема {scheme} — {how}")
    service = (svc.get("metadata") or {}).get("name") or ""
    if not service:
        return None, "у Service нет имени — пробрасывать не к чему"
    return f"{scheme}|{port}|{service}", " · ".join(census)


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("front", nargs="?", choices=sorted(FRONTS), help="какой фронт разрешать")
    ap.add_argument("--namespace", default=os.environ.get("SETUP_NS", "kacho"))
    ap.add_argument("--release", default=os.environ.get("KANAME_RELEASE", "kaname"),
                    help="имя Service/Deployment службы доступа в этой посадке")
    ap.add_argument("--container", default=os.environ.get("KANAME_CONTAINER", "kaname"))
    ap.add_argument("--timeout", type=int, default=20)
    ap.add_argument("--self-test", action="store_true")
    args = ap.parse_args(argv)

    if args.self_test:
        return self_test()
    if args.front is None:
        print("ОТКАЗ: не назван фронт (public|internal)", file=sys.stderr)
        return RC_CALLER_DEFECT

    service = args.release + FRONTS[args.front]["service_suffix"]
    doc = _kubectl(args.namespace, service, args.release, args.timeout)
    if doc is None:
        print(f"[фронт] УСЛОВИЕ НЕ СОЗДАНО: {FRONTS[args.front]['what']} — посадка не отвечает; "
              f"переменная НЕ инъектируется, кейсы скажут об этом сами", file=sys.stderr)
        return RC_PRECONDITION_MISSING

    addr, census = resolve(doc, args.front, args.container)
    print(f"[фронт {args.front}] перепись: {census}", file=sys.stderr)
    if addr is None:
        print(f"[фронт] УСЛОВИЕ НЕ СОЗДАНО: {FRONTS[args.front]['what']} посадкой не объявлен; "
              f"переменная НЕ инъектируется, кейсы скажут об этом сами", file=sys.stderr)
        return RC_PRECONDITION_MISSING
    print(addr)
    return RC_OK


# ───────────────────────────────────────────────────────────────────────────
# САМОПРОВЕРКА. Гоняется НАСТОЯЩАЯ судящая функция `resolve`, а не её пересказ:
# оси проверяются по одной, у каждой инъекции — законный близнец, отличающийся
# РОВНО ОДНИМ фактом.
# ───────────────────────────────────────────────────────────────────────────

def _doc(port_name="http-rest", port=9098, tls_env=None, tls_value=None,
         container="kaname", kinds=("Service", "Deployment"), service_name="kaname"):
    items = []
    if "Service" in kinds:
        items.append({"kind": "Service", "metadata": {"name": service_name},
                      "spec": {"ports": [{"name": "grpc", "port": 9090},
                                         {"name": port_name, "port": port}]}})
    if "Deployment" in kinds:
        env = [] if tls_env is None else [{"name": tls_env, "value": tls_value}]
        items.append({"kind": "Deployment", "metadata": {"name": "kaname"},
                      "spec": {"template": {"spec": {"containers": [
                          {"name": container, "env": env}]}}}})
    return {"items": items}


def self_test() -> int:
    fails = []

    def check(name, ok, detail=""):
        print(f"  {'ok  ' if ok else 'FAIL'} {name}" + ("" if ok else f": {detail}"))
        if not ok:
            fails.append(f"{name}: {detail}")

    print("ось 1 — порт берётся ПО ИМЕНИ, а не по номеру")
    addr, c = resolve(_doc(port=31098), "public", "kaname")
    check("номер сменился — адрес всё равно разрешён", addr == "http|31098|kaname", f"{addr} / {c}")
    addr, c = resolve(_doc(port_name="http-rest-renamed"), "public", "kaname")
    check("имени нет — УСЛОВИЕ НЕ СОЗДАНО, а не выдуманный номер", addr is None, f"{addr}")
    check("находка называет объявленные имена", "grpc" in c and "http-rest" in c, c)

    print("ось 2 — схема читается из объявления, а не выписывается")
    addr, _ = resolve(_doc(tls_env="KANAME_REST_SERVER_MTLS_ENABLE", tls_value="true"),
                      "public", "kaname")
    check("ручка транспорта поднята → https", addr == "https|9098|kaname", str(addr))
    addr, _ = resolve(_doc(tls_env="KANAME_REST_SERVER_MTLS_ENABLE", tls_value="false"),
                      "public", "kaname")
    check("та же посадка, ручка опущена → http (различие РОВНО одно)", addr == "http|9098|kaname", str(addr))
    addr, c = resolve(_doc(), "public", "kaname")
    check("ручки нет вовсе → http умолчанием объявления", addr == "http|9098|kaname", str(addr))
    check("и умолчание НАЗВАНО, а не подставлено молча", "умолчанием объявления" in c, c)

    print("ось 3 — схема НЕ выводится из номера порта")
    addr, _ = resolve(_doc(port=443, tls_env=None), "public", "kaname")
    check("порт 443 без объявленного транспорта остаётся http", addr == "http|443|kaname", str(addr))

    print("ось 4 — внутренний фронт судится СВОИМИ координатами")
    addr, _ = resolve(_doc(port_name="http-rest-int", port=9099,
                           tls_env="KANAME_INTERNALREST_SERVER_MTLS_ENABLE", tls_value="true"),
                      "internal", "kaname")
    check("своё имя порта и своя ручка → https|9099", addr == "https|9099|kaname", str(addr))
    addr, _ = resolve(_doc(port_name="http-rest-int", port=9099,
                           tls_env="KANAME_REST_SERVER_MTLS_ENABLE", tls_value="true"),
                      "internal", "kaname")
    check("ручка ПУБЛИЧНОГО фронта внутренний не поднимает", addr == "http|9099|kaname", str(addr))

    print("ось 5 — имя Service отдаёт производитель, а не выводит вызывающий")
    addr, _ = resolve(_doc(port_name="http-rest-int", port=9099, service_name="kaname-internal"),
                      "internal", "kaname")
    check("имя приходит из ответа посадки", addr == "http|9099|kaname-internal", str(addr))

    print("ось 6 — неполный ответ посадки не даёт адреса")
    addr, c = resolve(_doc(kinds=("Service",)), "public", "kaname")
    check("Deployment не пришёл → адреса нет", addr is None and "транспорт" in c, f"{addr} / {c}")
    addr, c = resolve(_doc(kinds=("Deployment",)), "public", "kaname")
    check("Service не пришёл → адреса нет", addr is None, f"{addr} / {c}")
    addr, c = resolve(_doc(container="иной"), "public", "kaname")
    check("контейнер не тот → адреса нет, и находка его называет",
          addr is None and "иной" in c, f"{addr} / {c}")

    print()
    if fails:
        print(f"ОТКАЗ: провалено утверждений {len(fails)} из 14", file=sys.stderr)
        for f in fails:
            print("  " + f, file=sys.stderr)
        return 1
    print("ЧИСТО: 14 утверждений — порт по имени, схема из объявления, имя Service "
          "от производителя, неполная посадка адреса не даёт")
    return 0


if __name__ == "__main__":
    sys.exit(main())
