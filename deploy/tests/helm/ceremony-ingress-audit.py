#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""ceremony-ingress-audit.py — куда внешний вход умбреллы ведёт координаты церемонии.

Ловит конкретный случай (kacho#2860): единственный вход края объявлен одним
правилом `path: /` с протоколом бэкенда GRPCS, и три координаты церемонии —
`GET /iam/v1/authorize`, `POST /iam/v1/token`,
`GET /.well-known/oauth-authorization-server` — уходят к краю gRPC-проксированием.
Обычный HTTP-запрос браузера или клиента OAuth по ним до ретрансляции края не
доходит: посредник отвечает ошибкой шлюза, край не видит ни строки.

Судит не наличие аннотации, а ИСХОД МАРШРУТИЗАЦИИ: по разобранным объектам
Ingress одного рендера решает, какое правило выиграет у посредника для
заданного пути на хосте края, — тем же порядком, что у ingress-nginx: точное
совпадение (`pathType: Exact`) раньше приставки, среди приставок — самая
длинная, приставка совпадает по границе элемента пути (`/a` покрывает `/a` и
`/a/b`, но не `/ab`). Правило чужого объекта того же класса на том же хосте
участвует наравне: угнать координату может и вход консоли, объявленный на хост
края.

Утверждения (печатаются находками, по одной на нарушение):

  1. каждая из трёх координат выигрывает правило ТОЧНОГО совпадения, ведущее к
     службе края на порт `tls` (слушатель, помеченный внешним) протоколом HTTPS;
  2. отрицательный близнец `/iam/v1/authorize:check` и соседи каждой координаты
     (подпуть, хвостовая черта, дописанная буква, суффикс `:verb`, усечённая
     буква) остаются на ПРЕЖНЕМ правиле — том, что выигрывает путь `/`;
  3. прочие пути края — gRPC-метод, глаголы формы входа, прочие адреса
     `/.well-known/` — остаются на прежнем правиле;
  4. прежнее правило — gRPC к порту `tls` (его форму держит и
     `jobs-cronjobs-hardening-test.sh`; здесь оно нужно как эталон «прежнего»);
  5. второго внешнего входа нет: все объекты Ingress, ведущие к краю, стоят на
     одном классе входа; путь `/` к краю ведёт ровно один хост (вход края), и
     его объекты края несут TLS одним секретом, а правил сверх прежнего и трёх
     координат на нём нет. Хост другой поверхности (консоли), чей `/`
     выигрывает её собственная служба, вправе вести к краю ПОЛОСЫ (звено фронта,
     kacho#3028) — только на слушатель `tls` протоколом HTTPS и с тем же TLS,
     что у `/` этого хоста; такой хост не больше одного. Хост, на котором `/` не выигрывает никто, — второй
     вход, хотя бы и из одних координат.

Печатает построчно:
    FINDING <текст>        — находка о дереве
    SCOPE <n> <n> <n> <n>  — координат сверено · соседей и близнецов сверено ·
                             прочих путей сверено · полос хостов других
                             поверхностей сверено
    SKIP <причина>         — в рендере нет входа края: судить не о чем (не
                             находка и не успех — вызывающий считает такие стеки)

`--self-test` кормит ту же функцию синтетическими рендерами: законная форма
обязана молчать, каждая форма с ОДНИМ внесённым дефектом — назвать его.
"""

import glob
import os
import sys
import tarfile

import yaml

EDGE_SERVICE = "api-gateway"
EDGE_PORT_NAME = "tls"
PRIOR_PROTOCOL = "GRPCS"
CEREMONY_PROTOCOL = "HTTPS"
BACKEND_PROTOCOL = "nginx.ingress.kubernetes.io/backend-protocol"

# Координаты церемонии. Те же строки объявляет край — CeremonyPath* в
# `gateway/internal/middleware/login_lane_paths.go` (вносит kacho#2721). Сверки
# двух объявлений этот разбор НЕ делает: он судит только маршрутизацию входа, и
# расхождение с краем ему не видно.
CEREMONY_COORDINATES = (
    "/iam/v1/authorize",
    "/iam/v1/token",
    "/.well-known/oauth-authorization-server",
)

# Отрицательный близнец предиката снятия (kacho#2860 п.2): соседний путь
# проверки прав, который край обслуживает по gRPC-шлюзу REST.
NEGATIVE_TWIN = "/iam/v1/authorize:check"

# Прочие пути края, обязанные остаться на прежнем правиле.
OTHER_PATHS = (
    "/",
    "/kacho.cloud.vpc.v1.NetworkService/ListNetworks",
    "/iam/v1/auth/login",
    "/iam/v1/authorize:batchCheck",
    "/.well-known/openid-configuration",
    "/.well-known/jwks.json",
    "/iam/v1/tokens",
)


def neighbours(coord):
    """Пути, которые приставочное или неточное правило ошибочно захватило бы."""
    return (
        coord + "/",
        coord + "/x",
        coord + "x",
        coord + ":check",
        coord[:-1],
    )


def ingresses(docs):
    return [d for d in docs if isinstance(d, dict) and d.get("kind") == "Ingress"]


def rules_of(ing):
    """(хост, путь, тип пути, служба, порт) каждого правила объекта."""
    out = []
    for rule in (ing.get("spec") or {}).get("rules") or []:
        host = rule.get("host") or ""
        for p in ((rule.get("http") or {}).get("paths")) or []:
            svc = ((p.get("backend") or {}).get("service")) or {}
            port = svc.get("port") or {}
            port_id = port.get("name") if port.get("name") is not None else port.get("number")
            out.append((host, p.get("path") or "", p.get("pathType") or "", svc.get("name") or "", port_id))
    return out


def protocol_of(ing):
    ann = (ing.get("metadata") or {}).get("annotations") or {}
    return str(ann.get(BACKEND_PROTOCOL, "HTTP"))


def name_of(ing):
    return (ing.get("metadata") or {}).get("name") or "?"


def prefix_covers(prefix, path):
    if prefix == "/":
        return True
    base = prefix.rstrip("/")
    return path == base or path.startswith(base + "/")


def route(ings, host, path, findings):
    """Правило, которое выиграет у посредника; None — не выиграет никакое."""
    exact, prefix = [], []
    for ing in ings:
        for (h, p, t, svc, port) in rules_of(ing):
            if h != host:
                continue
            won = {"ingress": name_of(ing), "path": p, "type": t, "service": svc,
                   "port": port, "protocol": protocol_of(ing)}
            if t == "Exact":
                if p == path:
                    exact.append(won)
            elif t == "Prefix":
                if prefix_covers(p, path):
                    prefix.append(won)
            else:
                findings.append(
                    f"правило {name_of(ing)} {p} типа «{t}»: исход маршрутизации из рендера невыводим")
    pool = exact
    if not pool and prefix:
        longest = max(len(w["path"].rstrip("/")) for w in prefix)
        pool = [w for w in prefix if len(w["path"].rstrip("/")) == longest]
    if len(pool) > 1:
        findings.append(
            f"путь {path}: равных правил {len(pool)} ({', '.join(w['ingress'] for w in pool)}) — "
            "посредник выберет одно по возрасту объекта, из рендера исход невыводим")
    return pool[0] if pool else None


def same_rule(a, b):
    return a is not None and b is not None and all(
        a[k] == b[k] for k in ("ingress", "path", "type", "service", "port", "protocol"))


def describe(r):
    if r is None:
        return "никакое правило"
    return f"{r['ingress']} {r['type']} {r['path']} → {r['service']}:{r['port']} {r['protocol']}"


def audit(docs, console_host):
    """(находки, перепись | None, причина пропуска | None).

    console_host — хост консоли, объявленный ПРОФИЛЕМ стека (`uif.ingress.host`
    действующих значений, см. console_host_of_profile), либо None, когда профиль
    консоли за контроллером входа не объявляет.
    """
    findings = []
    ings = ingresses(docs)
    edge = [i for i in ings if any(svc == EDGE_SERVICE for (_, _, _, svc, _) in rules_of(i))]
    if not edge:
        return findings, None, f"входа к службе {EDGE_SERVICE} в рендере нет"

    # 5. Второго внешнего входа нет. Класс — один на все объекты, ведущие к
    # краю: объект другого класса обслуживает другой контроллер, то есть другой
    # вход.
    classes = sorted({str((i.get("spec") or {}).get("ingressClassName")) for i in edge})
    if len(classes) != 1:
        findings.append(f"объекты входа края стоят на классах {classes} — это второй внешний вход")
        return findings, (0, 0, 0, 0), None
    # Состязаются правила одного посредника: объект другого класса обслуживает
    # другой контроллер и маршрут этого не угоняет.
    ings = [i for i in ings if str((i.get("spec") or {}).get("ingressClassName")) == classes[0]]

    # Хост, на котором к краю ведёт хоть одно правило, — одно из двух. Либо это
    # ВХОД КРАЯ: путь `/` на нём выигрывает сам край. Либо это хост ДРУГОЙ
    # поверхности (консоли), чей `/` выигрывает её собственная служба, а к краю
    # ведут лишь полосы — звено фронта kacho#3028: контроллер ведёт полосы,
    # которые раздача консоли и так проксировала к краю, прямо на внешний
    # слушатель края. Полоса не заводит входа: она стоит на ТОМ ЖЕ хосте, классе
    # и TLS, что и `/` этого хоста. Хост, на котором `/` не выигрывает никто, —
    # отдельный вход края, хотя бы и из трёх координат.
    entry_hosts, link_hosts = [], []
    for h in sorted({h for i in edge for (h, _, _, svc, _) in rules_of(i) if svc == EDGE_SERVICE}):
        r = route(ings, h, "/", findings)
        if r is not None and r["service"] == EDGE_SERVICE:
            entry_hosts.append(h)
        elif r is not None:
            link_hosts.append((h, r))
        else:
            findings.append(f"к краю ведёт хост {h}, путь / на котором не выигрывает ни одно правило — "
                            "это второй внешний вход, а не полоса хоста другой поверхности")
    if len(entry_hosts) != 1:
        findings.append(f"путь / к краю ведут хосты {entry_hosts} — это второй внешний вход, а не правило первого")
        return findings, (0, 0, 0, 0), None
    # Хост другой поверхности с полосами к краю — ТОЛЬКО хост консоли, который
    # объявил профиль стека (`uif.ingress.host`): звено фронта (kacho#3028) ведёт
    # полосы ровно с него. Любой другой хост — ещё одно публичное имя, по которому
    # снаружи доходят до края, то есть второй внешний вход, сколь бы законной ни
    # была форма его полос и сколько бы таких хостов ни было. Счёт хостов этого
    # не видит: единственный хост с полосами может быть и не консолью.
    for (h, _) in link_hosts:
        if h != console_host:
            findings.append(f"полосы к краю ведёт хост {h}, а хост консоли профиля — "
                            f"{console_host if console_host else 'не объявлен'} (uif.ingress.host): "
                            "это второй внешний вход, а не полоса хоста консоли")
    host = entry_hosts[0]

    def tls_secrets(i, h):
        return sorted({str(t.get("secretName")) for t in ((i.get("spec") or {}).get("tls") or [])
                       if h in (t.get("hosts") or [])})

    def edge_on(h):
        return [i for i in ings if any(rh == h and svc == EDGE_SERVICE for (rh, _, _, svc, _) in rules_of(i))]

    # На входе края: TLS объявлен каждым объектом, секрет — один на всех.
    secrets = sorted({s for i in edge_on(host) for s in tls_secrets(i, host)})
    for i in edge_on(host):
        if not tls_secrets(i, host):
            findings.append(f"объект {name_of(i)} не объявляет TLS на хосте края {host}")
    if len(secrets) > 1:
        findings.append(f"объекты входа края несут секреты TLS {secrets} — это второй внешний вход")

    # На хосте другой поверхности: полоса к краю — на внешний слушатель края
    # (Internal* → 404) протоколом HTTPS и с тем же TLS, что у `/` этого хоста.
    n_link = 0
    for (h, root) in link_hosts:
        root_ing = next(i for i in ings if name_of(i) == root["ingress"])
        want = tls_secrets(root_ing, h)
        for i in edge_on(h):
            mine = tls_secrets(i, h)
            if mine != want:
                findings.append(f"объект {name_of(i)} на хосте {h} несёт TLS {mine}, а путь / этого хоста "
                                f"({root['ingress']}) — {want}: это второй внешний вход, а не полоса хоста")
            for (rh, p, t, svc, port) in rules_of(i):
                if rh != h or svc != EDGE_SERVICE:
                    continue
                n_link += 1
                if port != EDGE_PORT_NAME:
                    findings.append(f"полоса {name_of(i)} {t} {p} на хосте {h}: ведёт на {svc}:{port}, "
                                    f"а не на слушатель {EDGE_SERVICE}:{EDGE_PORT_NAME}, помеченный внешним")
                if protocol_of(i) != CEREMONY_PROTOCOL:
                    findings.append(f"полоса {name_of(i)} {t} {p} на хосте {h}: протокол бэкенда "
                                    f"{protocol_of(i)}, а не {CEREMONY_PROTOCOL}")

    # 4. Прежнее правило — то, что выигрывает `/`.
    prior = route(ings, host, "/", findings)
    if prior is None:
        findings.append(f"путь / на {host} не выигрывает ни одно правило — прежнего правила нет")
        return findings, (0, 0, 0, 0), None
    if prior["service"] != EDGE_SERVICE or prior["port"] != EDGE_PORT_NAME or prior["protocol"] != PRIOR_PROTOCOL:
        findings.append(f"прежнее правило ({describe(prior)}) — не {PRIOR_PROTOCOL} к {EDGE_SERVICE}:{EDGE_PORT_NAME}")

    # 1. Координаты — точным правилом, HTTPS, к слушателю, помеченному внешним.
    n_coord = 0
    for c in CEREMONY_COORDINATES:
        r = route(ings, host, c, findings)
        n_coord += 1
        if r is None or r["type"] != "Exact" or r["path"] != c:
            findings.append(f"координата {c}: выигрывает {describe(r)}, а не правило точного совпадения")
            continue
        if r["service"] != EDGE_SERVICE or r["port"] != EDGE_PORT_NAME:
            findings.append(f"координата {c}: ведёт на {r['service']}:{r['port']}, "
                            f"а не на слушатель {EDGE_SERVICE}:{EDGE_PORT_NAME}, помеченный внешним")
        if r["protocol"] != CEREMONY_PROTOCOL:
            findings.append(f"координата {c}: протокол бэкенда {r['protocol']}, а не {CEREMONY_PROTOCOL}")

    # 2. Близнец и соседи — прежним правилом.
    n_near = 0
    for p in (NEGATIVE_TWIN,) + tuple(n for c in CEREMONY_COORDINATES for n in neighbours(c)):
        r = route(ings, host, p, findings)
        n_near += 1
        if not same_rule(r, prior):
            findings.append(f"путь {p}: выигрывает {describe(r)}, а не прежнее правило ({describe(prior)})")

    # 3. Прочие пути — прежним правилом.
    n_other = 0
    for p in OTHER_PATHS:
        r = route(ings, host, p, findings)
        n_other += 1
        if not same_rule(r, prior):
            findings.append(f"путь {p}: выигрывает {describe(r)}, а не прежнее правило ({describe(prior)})")

    # 5. Правил сверх прежнего и трёх координат у края нет.
    for i in ings:
        for (h, p, t, svc, port) in rules_of(i):
            if h != host:
                continue
            if (p, t) == (prior["path"], prior["type"]) and name_of(i) == prior["ingress"]:
                continue
            if t == "Exact" and p in CEREMONY_COORDINATES:
                continue
            findings.append(f"правило {name_of(i)} {t} {p} на хосте края — сверх прежнего и трёх координат")

    return findings, (n_coord, n_near, n_other, n_link), None


# ── Хост консоли из профиля ──────────────────────────────────────────────────
#
# Источник — ЗНАЧЕНИЕ ЧАРТА, а не литерал: тот же `uif.ingress.host`, который
# читают шаблон полос (templates/console-edge-lanes-ingress.yaml) и вход самой
# консоли (подчарт `uif`). Действующее значение складывается так же, как у helm:
# умолчания подчарта под ключом `uif`, поверх — умолчания умбреллы, поверх —
# профили цепочки стека слева направо; карты сливаются по ключам, прочее
# замещается целиком, `null` ключ удаляет. Та же сборка, что у Go-гейтов
# каталога deploy (mergedValuesOfStack + coalesceSubchartDefaults).

CONSOLE_CHART = "uif"


class ProfileError(Exception):
    """Профиль не прочитан — это «не выполнилось», а не находка о дереве."""


def merge_values(dst, src, drop_null=False):
    """Слой src поверх dst. `null` между слоями значений замещает, как любое
    значение; удаляет ключ он при наложении на УМОЛЧАНИЯ подчарта (drop_null)."""
    for k, v in (src or {}).items():
        if v is None and drop_null:
            dst.pop(k, None)
        elif v is None:
            dst[k] = None
        elif isinstance(v, dict):
            cur = dst.get(k)
            dst[k] = merge_values(cur if isinstance(cur, dict) else {}, v, drop_null)
        else:
            dst[k] = v
    return dst


def _yaml_map(text, where):
    try:
        tree = yaml.safe_load(text)
    except yaml.YAMLError as exc:
        raise ProfileError(f"{where} не разбирается как YAML: {exc}") from exc
    if tree is None:
        return {}
    if not isinstance(tree, dict):
        raise ProfileError(f"{where}: корень не карта")
    return tree


def subchart_defaults(umbrella, chart):
    """Умолчания подчарта — архивом ЛИБО каталогом, ровно одним источником."""
    srcs = sorted(glob.glob(os.path.join(umbrella, "charts", f"{chart}-*.tgz")))
    folder = os.path.join(umbrella, "charts", chart, "values.yaml")
    if os.path.isfile(folder):
        srcs.append(folder)
    if len(srcs) != 1:
        raise ProfileError(f"умолчания подчарта {chart}: источников {len(srcs)} ({srcs}) — "
                           "нужен ровно один (архив либо каталог)")
    src = srcs[0]
    if src.endswith(".tgz"):
        try:
            with tarfile.open(src) as tf:
                raw = tf.extractfile(f"{chart}/values.yaml").read().decode("utf-8")
        except (OSError, KeyError, tarfile.TarError, AttributeError) as exc:
            raise ProfileError(f"умолчания подчарта {chart} в {src} не читаются: {exc}") from exc
    else:
        with open(src, encoding="utf-8") as fh:
            raw = fh.read()
    tree = _yaml_map(raw, src)
    if not tree:
        raise ProfileError(f"умолчания подчарта {chart} ({src}) пусты — унаследованный хост судить не с чем")
    return tree


def console_host_of_profile(umbrella, chain):
    """Хост консоли, объявленный профилем, либо None (консоль за входом не объявлена)."""
    layers = [os.path.join(umbrella, "values.yaml")] + [os.path.join(umbrella, p) for p in chain]
    merged = {}
    for path in layers:
        try:
            with open(path, encoding="utf-8") as fh:
                merge_values(merged, _yaml_map(fh.read(), path))
        except OSError as exc:
            raise ProfileError(f"профиль {path} не читается: {exc}") from exc
    declared = merged.get(CONSOLE_CHART)
    uif = merge_values(merge_values({}, subchart_defaults(umbrella, CONSOLE_CHART)),
                       declared if isinstance(declared, dict) else {}, drop_null=True)
    ing = uif.get("ingress") if isinstance(uif.get("ingress"), dict) else {}
    host = ing.get("host")
    # Переключатели судятся истинностью — так их читает helm (`condition:` и `if`).
    if uif.get("enabled") and ing.get("enabled") and isinstance(host, str) and host:
        return host
    return None


def emit(findings, scope, skip):
    for f in findings:
        print(f"FINDING {f}")
    if skip is not None:
        print(f"SKIP {skip}")
    else:
        print(f"SCOPE {scope[0]} {scope[1]} {scope[2]} {scope[3]}")


# ── Самопроверка ─────────────────────────────────────────────────────────────

def _ing(name, paths, protocol, host="api.kacho.local", cls="nginx", secret="api-gateway-tls"):
    ann = {} if protocol is None else {BACKEND_PROTOCOL: protocol}
    return {
        "apiVersion": "networking.k8s.io/v1", "kind": "Ingress",
        "metadata": {"name": name, "annotations": ann},
        "spec": {
            "ingressClassName": cls,
            "tls": [{"hosts": [host], "secretName": secret}],
            "rules": [{"host": host, "http": {"paths": [
                {"path": p, "pathType": t,
                 "backend": {"service": {"name": svc, "port": ({"name": port} if isinstance(port, str) else {"number": port})}}}
                for (p, t, svc, port) in paths]}}],
        },
    }


def _prior():
    return _ing("api-gateway", [("/", "Prefix", EDGE_SERVICE, EDGE_PORT_NAME)], PRIOR_PROTOCOL)


def _ceremony(**kw):
    paths = kw.pop("paths", [(c, "Exact", EDGE_SERVICE, EDGE_PORT_NAME) for c in CEREMONY_COORDINATES])
    return _ing("api-gateway-ceremony", paths, kw.pop("protocol", CEREMONY_PROTOCOL), **kw)


# Хост консоли, который профиль синтетического стека объявляет (uif.ingress.host).
CONSOLE_HOST = "console.kacho.local"


def _console():
    return _ing("ui", [("/", "Prefix", "ui", 8080)], None, host="console.kacho.local", secret="ui-tls")


# Полосы края на хосте консоли — та же форма, что у
# templates/console-edge-lanes-ingress.yaml (kacho#3028).
CONSOLE_LANES = ("/vpc/", "/iam/v1/")


def _console_lanes(**kw):
    paths = kw.pop("paths", [(p, "Prefix", EDGE_SERVICE, EDGE_PORT_NAME) for p in CONSOLE_LANES])
    return _ing("console-edge-lanes", paths, kw.pop("protocol", CEREMONY_PROTOCOL),
                host=kw.pop("host", "console.kacho.local"), secret=kw.pop("secret", "ui-tls"), **kw)



def self_test():
    rc = 0
    legit = [_prior(), _ceremony(), _console()]
    cases = [
        # (имя, рендер, ожидается находка с подстрокой | None — молчание)
        ("законная форма: прежнее правило + три точных HTTPS", legit, None),
        ("законная форма без входа консоли", [_prior(), _ceremony()], None),
        ("законная форма: полосы края на хосте консоли тем же TLS", legit + [_console_lanes()], None),
        ("полосы консоли на внутренний слушатель", legit + [_console_lanes(paths=[
            (p, "Prefix", EDGE_SERVICE, "cmux") for p in CONSOLE_LANES])],
         "а не на слушатель api-gateway:tls"),
        ("полосы консоли по GRPCS", legit + [_console_lanes(protocol="GRPCS")], "протокол бэкенда GRPCS"),
        ("полосы консоли со своим секретом TLS", legit + [_console_lanes(secret="lanes-tls")],
         "второй внешний вход"),
        ("полосы к краю на хосте без своего /", legit + [_console_lanes(host="lanes.kacho.local")],
         "второй внешний вход"),
        ("полосы к краю на втором хосте другой поверхности", legit + [_console_lanes(),
            _ing("ui-second", [("/", "Prefix", "ui", 8080)], None, host="console2.kacho.local", secret="ui-tls"),
            _console_lanes(host="console2.kacho.local")],
         "полосы к краю ведёт хост console2.kacho.local"),
        # ИНЪЕКЦИЯ, ради которой прощение сужено (kacho#3028, check-verifier
        # стека a8f60d): хост с полосами ОДИН, форма полос законна, свой `/` у
        # хоста есть — но это не хост консоли профиля. Счёт хостов её не видит.
        ("полосы к краю на единственном хосте, который не хост консоли профиля", [_prior(), _ceremony(),
            _ing("docs", [("/", "Prefix", "docs", 8080)], None, host="docs.kacho.local", secret="ui-tls"),
            _console_lanes(host="docs.kacho.local")],
         "полосы к краю ведёт хост docs.kacho.local"),
        ("полосы к краю на хосте консоли, когда профиль консоли не объявляет", legit + [_console_lanes()],
         "хост консоли профиля — не объявлен", None),
        ("хост консоли целиком отдан краю", [_prior(), _ceremony(),
            _ing("console-edge", [("/", "Prefix", EDGE_SERVICE, EDGE_PORT_NAME)], PRIOR_PROTOCOL,
                 host="console.kacho.local", secret="ui-tls")],
         "второй внешний вход"),
        ("дерево до правки: только прежнее правило", [_prior(), _console()],
         "координата /iam/v1/authorize: выигрывает api-gateway Prefix /"),
        ("приставка вместо точного совпадения", [_prior(), _ceremony(paths=[
            (c, "Prefix", EDGE_SERVICE, EDGE_PORT_NAME) for c in CEREMONY_COORDINATES]), _console()],
         "а не правило точного совпадения"),
        ("координаты по GRPCS", [_prior(), _ceremony(protocol="GRPCS"), _console()],
         "протокол бэкенда GRPCS"),
        ("координаты без протокола (HTTP открытым текстом на TLS-слушатель)",
         [_prior(), _ceremony(protocol=None), _console()], "протокол бэкенда HTTP"),
        ("координаты на внутренний слушатель", [_prior(), _ceremony(paths=[
            (c, "Exact", EDGE_SERVICE, "cmux") for c in CEREMONY_COORDINATES]), _console()],
         "а не на слушатель api-gateway:tls"),
        ("вторая координата пропущена", [_prior(), _ceremony(paths=[
            (c, "Exact", EDGE_SERVICE, EDGE_PORT_NAME) for c in CEREMONY_COORDINATES if c != "/iam/v1/token"]), _console()],
         "координата /iam/v1/token"),
        ("правило близнеца :check на HTTPS", [_prior(), _ceremony(paths=[
            (c, "Exact", EDGE_SERVICE, EDGE_PORT_NAME) for c in CEREMONY_COORDINATES + (NEGATIVE_TWIN,)]), _console()],
         "путь /iam/v1/authorize:check"),
        ("правило сверх трёх координат", [_prior(), _ceremony(paths=[
            (c, "Exact", EDGE_SERVICE, EDGE_PORT_NAME) for c in CEREMONY_COORDINATES + ("/iam/v1/auth/login",)]), _console()],
         "путь /iam/v1/auth/login"),
        ("координаты на отдельном хосте", [_prior(), _ceremony(host="auth.kacho.local"), _console()],
         "второй внешний вход"),
        ("координаты на другом классе входа", [_prior(), _ceremony(cls="nginx-public"), _console()],
         "второй внешний вход"),
        ("координаты с другим секретом TLS", [_prior(), _ceremony(secret="ceremony-tls"), _console()],
         "второй внешний вход"),
        ("вход консоли угоняет координату на хосте края", [_prior(), _ceremony(),
            _ing("ui-token", [("/iam/v1/token", "Exact", "ui", 8080)], None)],
         "равных правил 2"),
        ("прежнее правило стало HTTPS", [_ing("api-gateway", [("/", "Prefix", EDGE_SERVICE, EDGE_PORT_NAME)], "HTTPS"),
            _ceremony(), _console()],
         "прежнее правило"),
    ]
    for case in cases:
        name, docs, want = case[:3]
        findings, scope, skip = audit(docs, case[3] if len(case) > 3 else CONSOLE_HOST)
        if want is None:
            if findings or skip is not None:
                print(f"  ПРОВАЛ {name}: законная форма дала находки {findings} / пропуск {skip}")
                rc = 1
            elif scope[:3] != (3, 1 + 5 * len(CEREMONY_COORDINATES), len(OTHER_PATHS)) or \
                    scope[3] != sum(len(rules_of(i)) for i in docs if name_of(i) == "console-edge-lanes"):
                print(f"  ПРОВАЛ {name}: перепись {scope} — осмотрено не всё объявленное")
                rc = 1
            else:
                print(f"  ОК    {name}: молчит, перепись {scope}")
        else:
            if not any(want in f for f in findings):
                print(f"  ПРОВАЛ {name}: ждали находку «{want}», получили {findings}")
                rc = 1
            else:
                print(f"  ОК    {name}: краснеет ({len(findings)} находок)")
    findings, scope, skip = audit([_console()], CONSOLE_HOST)
    if skip is None or findings:
        print(f"  ПРОВАЛ рендер без входа края: ждали пропуск, получили {findings} / {scope}")
        rc = 1
    else:
        print("  ОК    рендер без входа края: пропуск, а не зелёное")
    n_profile, rc_profile = self_test_profile()
    rc |= rc_profile
    print(f"=== {'PASS' if rc == 0 else 'FAIL'}: ceremony-ingress-audit.py --self-test "
          f"({len(cases) + 1 + n_profile} случаев)")
    return rc


def self_test_profile():
    """Хост консоли берётся из ДЕЙСТВУЮЩИХ значений, а не из одного слоя."""
    import io
    import tempfile

    def chart(root, form, sub):
        os.makedirs(os.path.join(root, "charts"), exist_ok=True)
        raw = yaml.safe_dump(sub).encode("utf-8")
        if form in ("tgz", "both"):
            with tarfile.open(os.path.join(root, "charts", f"{CONSOLE_CHART}-9.9.9.tgz"), "w:gz") as tf:
                info = tarfile.TarInfo(f"{CONSOLE_CHART}/values.yaml")
                info.size = len(raw)
                tf.addfile(info, io.BytesIO(raw))
        if form in ("dir", "both"):
            os.makedirs(os.path.join(root, "charts", CONSOLE_CHART), exist_ok=True)
            with open(os.path.join(root, "charts", CONSOLE_CHART, "values.yaml"), "wb") as fh:
                fh.write(raw)

    sub = {"ingress": {"enabled": True, "className": "nginx", "host": "console.default.local"}}
    base = {CONSOLE_CHART: {"enabled": False}}
    on = {CONSOLE_CHART: {"enabled": True}}
    cases = [
        # (имя, форма подчарта, слои цепочки, ожидаемый хост | ProfileError)
        ("умолчание подчарта, консоль включена цепочкой", "tgz", [on], "console.default.local"),
        ("то же, подчарт каталогом", "dir", [on], "console.default.local"),
        ("хост переопределён последним слоем цепочки", "tgz",
         [on, {CONSOLE_CHART: {"ingress": {"host": "console.stand.example"}}}], "console.stand.example"),
        ("консоль выключена умбреллой и не включена цепочкой", "tgz", [], None),
        ("вход консоли выключен профилем", "tgz", [on, {CONSOLE_CHART: {"ingress": {"enabled": False}}}], None),
        ("хост снят null — умолчание подчарта удалено, как у helm", "tgz",
         [on, {CONSOLE_CHART: {"ingress": {"host": None}}}], None),
        ("подчарт и архивом, и каталогом — источник неоднозначен", "both", [on], ProfileError),
        ("подчарта нет — умолчания взять неоткуда", "none", [on], ProfileError),
    ]
    rc = 0
    for name, form, chain, want in cases:
        with tempfile.TemporaryDirectory() as root:
            with open(os.path.join(root, "values.yaml"), "w", encoding="utf-8") as fh:
                yaml.safe_dump(base, fh)
            if form != "none":
                chart(root, form, sub)
            names = []
            for i, layer in enumerate(chain):
                names.append(f"values.l{i}.yaml")
                with open(os.path.join(root, names[-1]), "w", encoding="utf-8") as fh:
                    yaml.safe_dump(layer, fh)
            try:
                got = console_host_of_profile(root, names)
            except ProfileError as exc:
                got = ProfileError
                why = str(exc)
        if got != want:
            print(f"  ПРОВАЛ профиль: {name}: ждали {want}, получили {got}")
            rc = 1
        else:
            print(f"  ОК    профиль: {name}: {got.__name__ + ' (' + why + ')' if got is ProfileError else got}")
    return len(cases), rc


def main(argv):
    if "--self-test" in sys.argv:
        return self_test()
    if len(argv) < 3:
        print("usage: ceremony-ingress-audit.py <render.yaml> <каталог умбреллы> [профиль …] | --self-test",
              file=sys.stderr)
        return 2
    try:
        console_host = console_host_of_profile(argv[2], argv[3:])
    except ProfileError as exc:
        print(f"хост консоли из профиля не получен: {exc}", file=sys.stderr)
        return 2
    with open(argv[1], encoding="utf-8") as fh:
        docs = list(yaml.safe_load_all(fh))
    print(f"CONSOLE {console_host if console_host else '-'}")
    emit(*audit(docs, console_host))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
