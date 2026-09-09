#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Адрес СОБСТВЕННОГО REST-фронта службы доступа — из объявления посадки.

ПРЕДМЕТ. У службы два своих REST-фронта: публичный и внутренний. Харнесс
сквозных проб обязан знать их адреса, иначе кейсы собственного фронта мерили бы
край платформы вместо предмета — и правильно отказываются это делать.

ПОЧЕМУ АДРЕС ЧИТАЕТСЯ, А НЕ ВЫПИСЫВАЕТСЯ. Номер порта, транспорт слушателя и
режим проверки клиента объявляет ПОСАДКА (чарт службы: `service.public.restPort`,
`service.internal.internalRestPort`, `mtls.publicRest`, `mtls.internalRest`,
`mtls.restClientAuthMode`, `mtls.internalRestClientAuthMode`).
Выписанный в прогонщике литерал — второе место об одном предмете: он разойдётся
с чартом МОЛЧА, и разойдётся ровно там, где расхождение не видно — прогон
продолжит идти по прежнему адресу и назовёт чужой ответ ответом фронта.

Ручки транспорта названы СОСЕДЯМИ общей `mtls.httpListeners`, а не её вложением,
и это не мелочь записи: одноимённый ключ — СКАЛЯР (общее умолчание для всех
HTTP-слушателей), поэтому вложенной формы под ним не бывает BY CONSTRUCTION — ни
один профиль её не объявит. Здесь она и стояла (#2208); в форме координаты она
тут намеренно не воспроизводится — цитата несуществующего ключа читается
проверкой ниже как живое утверждение, и она права.

ЭТИХ КЛЮЧЕЙ СКРИПТ НЕ ЧИТАЕТ, и это сказано вслух, чтобы читатель не искал
здесь разбора значений: перечень выше — ПРОИСХОЖДЕНИЕ того, что скрипт
спрашивает у кластера, а спрашивает он отрендеренные объекты — порт Service по
имени и переменную процесса в Deployment. Держит перечень декларативная проба
deploy/own_rest_front_header_names_declared_keys_test.go: она читает объявления
чарта и профилей и требует, чтобы каждый названный здесь ключ в них резолвился.

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

ТРЕБОВАНИЕ КЛИЕНТСКОГО ЛИСТА — ОТДЕЛЬНОЕ ИЗМЕРЕНИЕ, И ОНО ЧИТАЕТСЯ ЗДЕСЬ ЖЕ.
Транспорт отвечает на «шифруется ли провод»; «требует ли фронт ПРОВЕРЕННОГО
клиентского листа на рукопожатии» — другой вопрос, у него своя ручка и своё
умолчание (`server-tls-only`). Фронт под транспортом бывает и односторонним:
тогда лист подан впустую и не доказывает ничего, а на взаимном ребре без листа
рукопожатие не состоится ВОВСЕ — запрос не уходит, и прогонщик видит «ответа
нет» вместо отказа по существу. Предикат здесь — зеркало продуктового
`MTLSConfig.InternalRESTRequiresClientCert`: транспорт поднят И эффективный
режим — взаимный. Незаданная ручка отвечает своим умолчанием, а не пустотой:
пустоту вызывающий прочёл бы как «ручки нет».

ОСЬ САМООТЧЁТА ЭТОТ ВОПРОС НЕ РЕШАЕТ, и это сказано её собственным автором:
`own_rest_internal_tls` спрашивает про ПРОВОД, «а не про то, требует ли фронт
клиентского сертификата: это отдельное измерение, и в этой оси его нет»
(pkg/observability, services/iam/cmd/kaname). Прогонщик, решавший по ней,
подавал бы лист на одностороннем ребре — то есть отвечал бы на соседний вопрос.

Оба объекта читаются ОДНИМ обращением (`kubectl get svc/... deploy/... -o json`):
между двумя запросами посадка сменится, и тогда порт будет от одной, а схема от
другой — пара, которой не существовало ни в один момент времени. Режим проверки
клиента приходит ТЕМ ЖЕ обращением и по той же причине.

ИСХОДОВ ТРИ, И ВТОРОЙ НЕ РАВЕН ТРЕТЬЕМУ:

  0 — адрес разрешён, на stdout `<схема>|<порт>|<имя Service>|<лист>`, где
      `<лист>` — `required` либо `not-required`. Имя Service тоже отдаётся
      отсюда: вызывающему оно нужно для проброса, и выведи он его сам («к
      внутреннему припиши -internal»), раскладка фронтов оказалась бы
      объявленной в двух местах. Решение о листе отдаётся ГОТОВЫМ по той же
      причине: вычислив его у себя, вызывающий завёл бы вторую копию
      продуктового предиката, и разошлась бы она молча;
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
        "client_auth_env": "KANAME_REST_SERVER_MTLS_CLIENTAUTHMODE",
        "what": "собственный публичный REST-фронт службы",
    },
    "internal": {
        "service_suffix": "-internal",
        "port_name": "http-rest-int",
        "tls_env": "KANAME_INTERNALREST_SERVER_MTLS_ENABLE",
        "client_auth_env": "KANAME_INTERNALREST_SERVER_MTLS_CLIENTAUTHMODE",
        "what": "собственный внутренний REST-фронт службы",
    },
}

# Режим проверки клиента: величины и умолчание — зеркало продуктового словаря
# (services/iam/internal/apps/kaname/config/mtls.go: clientAuthServerTLSOnly,
# clientAuthMutual, resolveClientAuthMode). Умолчание ОДНОСТОРОННЕЕ и объявлено
# осознанно: незаданная ручка означает «сертификата не требуем», а не «неизвестно».
CLIENT_AUTH_DEFAULT = "server-tls-only"
CLIENT_AUTH_MUTUAL = "mutual"

# Решение о листе — то, что вызывающему нужно, а не сырой режим. Сырой режим
# заставил бы его завести вторую копию предиката, и она разошлась бы молча.
LEAF_REQUIRED = "required"
LEAF_NOT_REQUIRED = "not-required"

RC_OK = 0
RC_CALLER_DEFECT = 1
RC_PRECONDITION_MISSING = 3


def requires_client_leaf(tls_enabled: bool, raw_mode) -> tuple[str, str]:
    """(«required»|«not-required», эффективный режим) — зеркало продуктового
    `MTLSConfig.InternalRESTRequiresClientCert`.

    ДВЕ ПОЛОВИНЫ, И ОБЕ НЕСУЩИЕ. Выключенный транспорт — «нет» by construction:
    сертификата не бывает там, где нет рукопожатия. Поднятый транспорт решает
    РЕЖИМ, и требует лист ТОЛЬКО взаимный: запрашивающий (`optional-mutual`)
    соединение без листа пропускает, то есть не требует его.

    НЕИЗВЕСТНЫЙ РЕЖИМ — «не требует», И ЭТО ЗЕРКАЛО, А НЕ ПОСЛАБЛЕНИЕ. Продукт
    отвечает на нём так же, а построение транспорта на нём ОТКАЗЫВАЕТ: процесс с
    таким режимом не стартует вовсе, и фронта, к которому надо было бы носить
    лист, на этой посадке не существует. Второе прочтение («неизвестно ⇒ подать
    на всякий случай») развело бы харнесс с продуктом там, где различие не видно.
    """
    mode = (raw_mode or "").strip() or CLIENT_AUTH_DEFAULT
    if not tls_enabled:
        return LEAF_NOT_REQUIRED, mode
    return (LEAF_REQUIRED if mode == CLIENT_AUTH_MUTUAL else LEAF_NOT_REQUIRED), mode


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

    # ЛИСТ. Читается ТЕМ ЖЕ разобранным ответом и тем же контейнером, что схема:
    # два обращения дали бы пару, которой не существовало ни в один момент.
    raw_mode = env.get(spec["client_auth_env"])
    leaf, mode = requires_client_leaf(scheme == "https", raw_mode)
    if raw_mode is None:
        mode_how = f"умолчанием объявления ({spec['client_auth_env']} не эмитирована)"
    else:
        mode_how = f"{spec['client_auth_env']}={raw_mode}"
    census.append(f"клиентский лист {leaf} — режим {mode}, {mode_how}")

    service = (svc.get("metadata") or {}).get("name") or ""
    if not service:
        return None, "у Service нет имени — пробрасывать не к чему"
    return f"{scheme}|{port}|{service}|{leaf}", " · ".join(census)


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
         container="kaname", kinds=("Service", "Deployment"), service_name="kaname",
         mode_env=None, mode_value=None):
    items = []
    if "Service" in kinds:
        items.append({"kind": "Service", "metadata": {"name": service_name},
                      "spec": {"ports": [{"name": "grpc", "port": 9090},
                                         {"name": port_name, "port": port}]}})
    if "Deployment" in kinds:
        env = [] if tls_env is None else [{"name": tls_env, "value": tls_value}]
        if mode_env is not None:
            env = env + [{"name": mode_env, "value": mode_value}]
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

    def where(addr):
        """Адрес БЕЗ решения о листе: оси 1-6 судят адрес, оси 7-9 — лист.

        Разделение несущее, а не косметическое: ось, чьё ожидание несёт ОБА
        предмета, краснеет от инъекции в любой из них — и находка перестаёт
        называть виновника. Проверено инъекцией: со слитым ожиданием дефект
        предиката листа ронял ещё и «ручка транспорта поднята → https».
        """
        return None if addr is None else addr.rsplit("|", 1)[0]

    # ─── ИМЕНА РУЧЕК, чтобы фикстуры не выписывали их по памяти ─────────────
    PUB_TLS = FRONTS["public"]["tls_env"]
    PUB_MODE = FRONTS["public"]["client_auth_env"]
    INT_TLS = FRONTS["internal"]["tls_env"]
    INT_MODE = FRONTS["internal"]["client_auth_env"]

    print("ось 1 — порт берётся ПО ИМЕНИ, а не по номеру")
    addr, c = resolve(_doc(port=31098), "public", "kaname")
    check("номер сменился — адрес всё равно разрешён", where(addr) == "http|31098|kaname", f"{addr} / {c}")
    addr, c = resolve(_doc(port_name="http-rest-renamed"), "public", "kaname")
    check("имени нет — УСЛОВИЕ НЕ СОЗДАНО, а не выдуманный номер", addr is None, f"{addr}")
    check("находка называет объявленные имена", "grpc" in c and "http-rest" in c, c)

    print("ось 2 — схема читается из объявления, а не выписывается")
    addr, _ = resolve(_doc(tls_env=PUB_TLS, tls_value="true"), "public", "kaname")
    check("ручка транспорта поднята → https", where(addr) == "https|9098|kaname", str(addr))
    addr, _ = resolve(_doc(tls_env=PUB_TLS, tls_value="false"), "public", "kaname")
    check("та же посадка, ручка опущена → http (различие РОВНО одно)",
          where(addr) == "http|9098|kaname", str(addr))
    addr, c = resolve(_doc(), "public", "kaname")
    check("ручки нет вовсе → http умолчанием объявления", where(addr) == "http|9098|kaname", str(addr))
    check("и умолчание НАЗВАНО, а не подставлено молча", "умолчанием объявления" in c, c)

    print("ось 3 — схема НЕ выводится из номера порта")
    addr, _ = resolve(_doc(port=443, tls_env=None), "public", "kaname")
    check("порт 443 без объявленного транспорта остаётся http", where(addr) == "http|443|kaname", str(addr))

    print("ось 4 — внутренний фронт судится СВОИМИ координатами")
    addr, _ = resolve(_doc(port_name="http-rest-int", port=9099,
                           tls_env=INT_TLS, tls_value="true"), "internal", "kaname")
    check("своё имя порта и своя ручка → https|9099", where(addr) == "https|9099|kaname", str(addr))
    addr, _ = resolve(_doc(port_name="http-rest-int", port=9099,
                           tls_env=PUB_TLS, tls_value="true"), "internal", "kaname")
    check("ручка ПУБЛИЧНОГО фронта внутренний не поднимает", where(addr) == "http|9099|kaname", str(addr))

    print("ось 5 — имя Service отдаёт производитель, а не выводит вызывающий")
    addr, _ = resolve(_doc(port_name="http-rest-int", port=9099, service_name="kaname-internal"),
                      "internal", "kaname")
    check("имя приходит из ответа посадки", where(addr) == "http|9099|kaname-internal", str(addr))

    print("ось 6 — неполный ответ посадки не даёт адреса")
    addr, c = resolve(_doc(kinds=("Service",)), "public", "kaname")
    check("Deployment не пришёл → адреса нет", addr is None and "транспорт" in c, f"{addr} / {c}")
    addr, c = resolve(_doc(kinds=("Deployment",)), "public", "kaname")
    check("Service не пришёл → адреса нет", addr is None, f"{addr} / {c}")
    addr, c = resolve(_doc(container="иной"), "public", "kaname")
    check("контейнер не тот → адреса нет, и находка его называет",
          addr is None and "иной" in c, f"{addr} / {c}")

    # ─────────────────────────────────────────────────────────────────────────
    # ось 7 — КЛИЕНТСКИЙ ЛИСТ: отдельное измерение, а не следствие транспорта.
    #
    # Инъекция и её ЗАКОННЫЙ БЛИЗНЕЦ отличаются РОВНО одним фактом — величиной
    # на ручке режима: посадка, порт, транспорт и контейнер у них одни и те же.
    # ─────────────────────────────────────────────────────────────────────────
    print("ось 7 — лист требуется ТОЛЬКО взаимным режимом")
    tls_up = dict(port_name="http-rest-int", port=9099, tls_env=INT_TLS, tls_value="true")
    addr, c = resolve(_doc(**tls_up, mode_env=INT_MODE, mode_value="mutual"), "internal", "kaname")
    check("взаимный режим → лист ТРЕБУЕТСЯ", addr == "https|9099|kaname|required", f"{addr} / {c}")
    addr, c = resolve(_doc(**tls_up, mode_env=INT_MODE, mode_value="server-tls-only"), "internal", "kaname")
    check("тот же фронт под транспортом, режим односторонний → лист НЕ требуется "
          "(различие РОВНО одно — величина на ручке)",
          addr == "https|9099|kaname|not-required", f"{addr} / {c}")
    addr, _ = resolve(_doc(**tls_up, mode_env=INT_MODE, mode_value="optional-mutual"), "internal", "kaname")
    check("запрашивающий режим листа НЕ требует: соединение без него проходит",
          addr == "https|9099|kaname|not-required", str(addr))
    addr, c = resolve(_doc(**tls_up), "internal", "kaname")
    check("ручки режима нет → одностороннее умолчание, а не «неизвестно»",
          addr == "https|9099|kaname|not-required", str(addr))
    check("и умолчание НАЗВАНО в переписи", "не эмитирована" in c and "server-tls-only" in c, c)
    addr, _ = resolve(_doc(port_name="http-rest-int", port=9099,
                           mode_env=INT_MODE, mode_value="mutual"), "internal", "kaname")
    check("взаимный режим БЕЗ транспорта листа не требует: рукопожатия нет вовсе",
          addr == "http|9099|kaname|not-required", str(addr))
    addr, _ = resolve(_doc(**tls_up, mode_env=INT_MODE, mode_value="невнятица"), "internal", "kaname")
    check("неизвестный режим — «не требует», как отвечает и продукт "
          "(процесс с ним не стартует вовсе)", addr == "https|9099|kaname|not-required", str(addr))

    print("ось 8 — ручка ЧУЖОГО фронта режим этого не меняет")
    addr, _ = resolve(_doc(**tls_up, mode_env=PUB_MODE, mode_value="mutual"), "internal", "kaname")
    check("взаимный режим объявлен ПУБЛИЧНОМУ фронту — внутренний остаётся односторонним",
          addr == "https|9099|kaname|not-required", str(addr))
    addr, _ = resolve(_doc(tls_env=PUB_TLS, tls_value="true",
                           mode_env=PUB_MODE, mode_value="mutual"), "public", "kaname")
    check("тот же режим на СВОЁМ фронте лист требует (положительный контроль оси)",
          addr == "https|9098|kaname|required", str(addr))

    print("ось 9 — предикат листа зеркалит продуктовый, а не пересказывает его")
    check("выключенный транспорт: «нет» независимо от режима",
          requires_client_leaf(False, "mutual") == (LEAF_NOT_REQUIRED, "mutual"),
          str(requires_client_leaf(False, "mutual")))
    check("пустая ручка отвечает УМОЛЧАНИЕМ, а не пустотой",
          requires_client_leaf(True, "") == (LEAF_NOT_REQUIRED, CLIENT_AUTH_DEFAULT),
          str(requires_client_leaf(True, "")))
    check("пробелы вокруг величины её не меняют",
          requires_client_leaf(True, "  mutual  ") == (LEAF_REQUIRED, CLIENT_AUTH_MUTUAL),
          str(requires_client_leaf(True, "  mutual  ")))

    print()
    total = 14 + 9 + 2 + 3
    if fails:
        print(f"ОТКАЗ: провалено утверждений {len(fails)} из {total}", file=sys.stderr)
        for f in fails:
            print("  " + f, file=sys.stderr)
        return 1
    print(f"ЧИСТО: {total} утверждений — порт по имени, схема из объявления, имя Service "
          "от производителя, требование клиентского листа отдельным измерением, "
          "неполная посадка адреса не даёт")
    return 0


if __name__ == "__main__":
    sys.exit(main())
