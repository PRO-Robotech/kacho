#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Блок пула обязан ВХОДИТЬ в полосу своего набора, а не просто отличаться.

ПРЕДМЕТ
-------
`address_pool_cidrs` несёт ограничение
`EXCLUDE USING gist (kind WITH =, block inet_ops WITH &&)`
(`services/vpc/internal/migrations/0004_address_pool_cidrs.sql`). Измерения зоны
и проекта у него НЕТ: два пула одного вида не могут нести пересекающиеся блоки
нигде во всём кластере. Значит блок пула — ГЛОБАЛЬНОЕ имя, и наборы проб делят
одно пространство имён, даже когда сидят в разных проектах и зонах.

Второе ограничение — `address_pools_zone_kind_default_uniq` `(zone_id, kind)
WHERE is_default`: два набора не могут одновременно держать умолчательный пул
одного вида в одной зоне.

ПОЧЕМУ РАСПИСАНИЕ ЭТОГО НЕ ДЕРЖАЛО
----------------------------------
Развод во ВРЕМЕНИ (перечень последовательных коллекций) не делает имя разным. Он
работает ровно до первого прогона, где два набора оказались рядом по любой другой
причине — умбрелла, повторный запуск, ручной вызов одной коллекции. И он не
проверяем: расписание молчит и когда оно нужно, и когда его предмет исчез.

Обоснование расписания при этом ссылалось на число, которого в дереве нет:
«зон всего три, zoneC≡zoneD». Посев `deploy/scripts/geo-baseline.sql` заводит
ШЕСТЬ зон, из них резолвер набора берёт семейство `ru-central1-` — ПЯТЬ, и
раздаёт zoneA..zoneD = a, b, c, d. Схлопывания нет.
Предикат: `grep -c "'ru-central1-" deploy/scripts/geo-baseline.sql` → 5.

ЧТО ПРОВЕРЯЕТСЯ
---------------
1. Каждый блок пула ВХОДИТ в полосу своего набора (по своему семейству).
   Именно вхождение: блок вне полосы отличается от чужих блоков и при этом
   ложится поверх чужой полосы — различие столкновения не запрещает.
2. Ни один блок не накрывает сеть из `OCCUPIED` (посевы стенда и посев nlb).
3. Полосы попарно не пересекаются — включая номера, забронированные за
   владельцами вне набора проб.
4. Каждый файл кейсов, несущий блок пула, ОБЪЯВЛЕН: у него есть полоса либо он
   назван набором без посадки. Новый набор с пулами не может завестись молча.
5. У набора «без посадки» каждое создание пула действительно утверждает отказ —
   вызов несёт `mode="gate"`. Освобождение проверяется, а не принимается на веру.
6. Занятая сеть, лежащая ВНУТРИ полосы, обязана называть эту полосу (`carves`).
   Двойное владение допустимо только названным вслух.
7. Две оси развода: ни одна зона не держит умолчательные пулы ДВУХ разных
   наборов, и объявленная за набором зона совпадает с тем, что делают кейсы.

ПРЕДПОСЫЛКА ПРОВЕРЯЕТСЯ. «Ноль находок» обязано отличаться от «ноль
прочитанного»: перепись осмотренного печатается всегда, а ноль прочитанных
файлов либо ноль разобранных блоков — отказ, а не чистота. На ПУСТОМ перечне
находок гейт проходит: пустой перечень есть цель, а не поломка.

Запуск:
    python3 deploy/scripts/assert-pool-bands-are-disjoint.py
    python3 deploy/scripts/assert-pool-bands-are-disjoint.py --self-test
Код возврата: 0 — все блоки в своих полосах; 1 — находка либо потерянная
предпосылка.
"""
from __future__ import annotations

import argparse
import ast
import importlib.util
import ipaddress
import os
import shutil
import sys
import tempfile

# Ключ тела запроса → семейство блока, объявленное ПОЛЕМ. Настоящее семейство
# берётся у самого блока: кейсы кросс-семейного отказа кладут v6-префикс в v4-поле
# намеренно, и такой блок судится полосой СВОЕГО семейства, а не полем.
_POOL_KEYS = {"v4CidrBlocks": 4, "v6CidrBlocks": 6}

_BANDS_REL = os.path.join("services", "vpc", "tests", "newman", "scripts", "pool_bands.py")
_CASES_REL = os.path.join("services", "vpc", "tests", "newman", "cases")


# ── загрузка объявления ────────────────────────────────────────────────────────

def load_bands(root: str):
    """Объявление полос — единственный источник; его отсутствие это отказ."""
    path = os.path.join(root, _BANDS_REL)
    if not os.path.isfile(path):
        return None
    spec = importlib.util.spec_from_file_location("kacho_vpc_pool_bands", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


# ── разбор кейсов ──────────────────────────────────────────────────────────────

class Block:
    """Один блок пула: сеть, координата и то, каким полем он объявлен."""

    __slots__ = ("net", "raw", "rel", "line", "field_family")

    def __init__(self, net, raw, rel, line, field_family):
        self.net = net
        self.raw = raw
        self.rel = rel
        self.line = line
        self.field_family = field_family

    @property
    def family(self) -> int:
        return self.net.version

    def where(self) -> str:
        return f"{self.rel}:{self.line}"


def _const_str(node, scope: dict):
    """Строковая постоянная либо имя, разрешимое умолчанием объемлющей функции."""
    if isinstance(node, ast.Constant) and isinstance(node.value, str):
        return node.value
    if isinstance(node, ast.Name):
        val = scope.get(node.id)
        return val if isinstance(val, str) else None
    return None


def _const_bool(node, scope: dict):
    if isinstance(node, ast.Constant) and isinstance(node.value, bool):
        return node.value
    if isinstance(node, ast.Name):
        val = scope.get(node.id)
        return val if isinstance(val, bool) else None
    return None


def _fn_defaults(fn: ast.FunctionDef) -> dict:
    """argname → значение-постоянная из умолчаний функции.

    Тело пула у набора адресов собирается ВНУТРИ помощника, поэтому зона, признак
    умолчательности и сам блок приходят туда именами. Без разрешения умолчаний
    главная фабрика пулов набора была бы невидима гейту — и «ноль находок» там
    означало бы «ноль прочитанного».
    """
    out = {}
    args = fn.args
    positional = list(args.posonlyargs) + list(args.args)
    for name, default in zip(positional[len(positional) - len(args.defaults):], args.defaults):
        if isinstance(default, ast.Constant):
            out[name.arg] = default.value
    for name, default in zip(args.kwonlyargs, args.kw_defaults):
        if isinstance(default, ast.Constant):
            out[name.arg] = default.value
    return out


def _parse_net(raw: str):
    """Строка → сеть, либо None. Шаблон postman сетью не является."""
    if "{{" in raw or "}}" in raw:
        return None
    try:
        return ipaddress.ip_network(raw, strict=False)
    except ValueError:
        return None


def _walk_scoped(tree):
    """(узел, умолчания объемлющей функции) по всему дереву разбора."""
    stack = [(tree, {})]
    while stack:
        node, scope = stack.pop()
        yield node, scope
        child_scope = scope
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            child_scope = dict(scope)
            child_scope.update(_fn_defaults(node))
        for child in ast.iter_child_nodes(node):
            stack.append((child, child_scope))


def extract(root: str, rel: str, bands) -> tuple:
    """(блоки, дефолт-зоны, счётчики, находки-правила-5).

    Возвращает также перепись: словарей с ключами пула, шаблонных строк и строк,
    не разобравшихся в сеть, — чтобы молчание гейта было отличимо от слепоты.
    """
    path = os.path.join(root, rel)
    try:
        with open(path, encoding="utf-8") as fh:
            tree = ast.parse(fh.read(), filename=rel)
    except (OSError, SyntaxError):
        return [], set(), {"dicts": 0, "templates": 0, "unparsed": 0}, []

    suite = os.path.splitext(os.path.basename(rel))[0]
    blocks: list = []
    default_zones: set = set()
    counts = {"dicts": 0, "templates": 0, "unparsed": 0}
    gate_findings: list = []
    no_landing = suite in getattr(bands, "NO_LANDING_SUITES", {})

    # Помощники набора, чей ИМЕНОВАННЫЙ аргумент несёт блок (тело словаря
    # собирается внутри помощника — по литералу словаря блок не виден).
    helpers = {(fn, arg): fam for s, fn, arg, fam in getattr(bands, "HELPER_BLOCK_ARGS", [])
               if s == suite}

    for node, scope in _walk_scoped(tree):
        if isinstance(node, ast.Dict):
            is_pool = any(isinstance(k, ast.Constant) and k.value in _POOL_KEYS
                          for k in node.keys)
            if not is_pool:
                continue
            counts["dicts"] += 1
            zone = None
            is_default = None
            for k, v in zip(node.keys, node.values):
                if not (isinstance(k, ast.Constant) and isinstance(k.value, str)):
                    continue
                if k.value == "zoneId":
                    zone = _const_str(v, scope)
                elif k.value == "isDefault":
                    is_default = _const_bool(v, scope)
                elif k.value in _POOL_KEYS:
                    fam = _POOL_KEYS[k.value]
                    items = v.elts if isinstance(v, ast.List) else [v]
                    for item in items:
                        raw = _const_str(item, scope)
                        if raw is None:
                            continue
                        net = _parse_net(raw)
                        if net is None:
                            counts["templates" if "{{" in raw else "unparsed"] += 1
                            continue
                        blocks.append(Block(net, raw, rel, getattr(item, "lineno", k.lineno), fam))
            if is_default and zone:
                default_zones.add(zone)

        elif isinstance(node, ast.Call):
            fname = node.func.id if isinstance(node.func, ast.Name) else (
                node.func.attr if isinstance(node.func, ast.Attribute) else None)
            kwargs = {kw.arg: kw.value for kw in node.keywords if kw.arg}
            for arg_name, value in kwargs.items():
                fam = helpers.get((fname, arg_name))
                if fam is None:
                    continue
                raw = _const_str(value, scope)
                if raw is None:
                    continue
                net = _parse_net(raw)
                if net is None:
                    counts["templates" if "{{" in raw else "unparsed"] += 1
                    continue
                blocks.append(Block(net, raw, rel, getattr(value, "lineno", node.lineno), fam))

            # Правило 5: у набора «без посадки» вызов, несущий тело пула, обязан
            # утверждать отказ. Иначе освобождение от полосы держится обещанием.
            if no_landing:
                carries_pool = any(
                    isinstance(a, ast.Dict) and any(
                        isinstance(k, ast.Constant) and k.value in _POOL_KEYS for k in a.keys)
                    for a in list(node.args) + list(kwargs.values()))
                if carries_pool:
                    mode = _const_str(kwargs.get("mode"), scope) if "mode" in kwargs else None
                    if mode != "gate":
                        gate_findings.append(
                            f"{rel}:{node.lineno}: набор объявлен «без посадки», но этот "
                            f"вызов создаёт пул и НЕ утверждает отказ (mode={mode!r}, "
                            f"ожидается 'gate') — блок доедет до ограничения, а полосы "
                            f"у набора нет")
    return blocks, default_zones, counts, gate_findings


def case_files(root: str) -> list:
    d = os.path.join(root, _CASES_REL)
    if not os.path.isdir(d):
        return []
    return sorted(os.path.join(_CASES_REL, n) for n in os.listdir(d)
                  if n.endswith(".py") and not n.startswith("__"))


# ── правила ────────────────────────────────────────────────────────────────────

def bands_are_pairwise_disjoint(bands) -> list:
    """Полосы (включая забронированные) не пересекаются между собой."""
    out = []
    owners = dict(bands.SUITE_BANDS)
    for idx, who in getattr(bands, "RESERVED_BANDS", {}).items():
        owners[f"[бронь] {who}"] = idx
    items = sorted(owners.items(), key=lambda kv: (kv[1], kv[0]))
    for i, (name_a, idx_a) in enumerate(items):
        for name_b, idx_b in items[i + 1:]:
            if idx_a == idx_b:
                out.append(f"объявление: номер полосы {idx_a} выдан дважды — "
                           f"{name_a!r} и {name_b!r}")
                continue
            for fam in (4, 6):
                a = bands.band_v4(idx_a) if fam == 4 else bands.band_v6(idx_a)
                b = bands.band_v4(idx_b) if fam == 4 else bands.band_v6(idx_b)
                if a.overlaps(b):
                    out.append(f"объявление: полосы v{fam} {name_a!r} ({a}) и "
                               f"{name_b!r} ({b}) пересекаются")
    return out


def occupied_inside_a_band_is_named(bands) -> list:
    """Занятая сеть внутри полосы обязана называть эту полосу."""
    out = []
    owners = dict(bands.SUITE_BANDS)
    owners.update({f"[бронь] {w}": i for i, w in getattr(bands, "RESERVED_BANDS", {}).items()})
    for net, owner, carves in bands.occupied_networks():
        for name, idx in sorted(owners.items(), key=lambda kv: kv[1]):
            band = bands.band_v4(idx) if net.version == 4 else bands.band_v6(idx)
            if not net.overlaps(band):
                continue
            if carves != idx:
                out.append(
                    f"объявление: занятая сеть {net} ({owner}) лежит внутри полосы "
                    f"{name!r} ({band}), но не называет её (`carves`={carves!r}, "
                    f"ожидается {idx}) — двойное владение молчит до первого "
                    f"расширения набора")
    return out


def blocks_are_inside_their_band(bands, suite: str, blocks: list) -> list:
    out = []
    for b in blocks:
        try:
            band = bands.band_for(suite, b.family)
        except KeyError:
            continue  # набор без полосы — это правило 4, не это
        if not b.net.subnet_of(band):
            out.append(
                f"{b.where()}: блок {b.raw} набора {suite!r} НЕ входит в его полосу "
                f"v{b.family} ({band}). Отличаться от чужих блоков недостаточно: блок "
                f"вне полосы ложится поверх чужой и сталкивается по EXCLUDE "
                f"`(kind, block &&)` так же, как совпадающий")
    return out


def blocks_avoid_occupied(bands, blocks: list) -> list:
    out = []
    occupied = bands.occupied_networks()
    for b in blocks:
        for net, owner, _carves in occupied:
            if net.version != b.net.version:
                continue
            if b.net.overlaps(net):
                out.append(f"{b.where()}: блок {b.raw} накрывает занятую сеть {net} "
                           f"({owner})")
    return out


def every_pool_suite_is_declared(bands, suite: str, blocks: list, rel: str) -> list:
    if not blocks:
        return []
    if suite in bands.declared_suites():
        return []
    return [f"{rel}: набор {suite!r} создаёт пулы ({len(blocks)} блок(а/ов)), но в "
            f"объявлении полос его нет — он делит глобальное пространство имён "
            f"вслепую"]


def default_zone_lanes(bands, observed: dict) -> list:
    """Две оси развода: партишн `(zone_id, kind) WHERE is_default`.

    observed: набор → множество зон, в которых его кейсы создают умолчательные пулы.
    """
    out = []
    lanes = getattr(bands, "DEFAULT_POOL_ZONE", {})
    banded = {s: z for s, z in observed.items() if s in bands.SUITE_BANDS}
    names = sorted(banded)
    for i, a in enumerate(names):
        for b in names[i + 1:]:
            shared = banded[a] & banded[b]
            if shared:
                out.append(
                    f"объявление: наборы {a!r} и {b!r} держат умолчательные пулы в "
                    f"одной зоне {sorted(shared)} — партишн "
                    f"`(zone_id, kind) WHERE is_default` один, и параллельный прогон "
                    f"даст 409 ALREADY_EXISTS")
    for suite, lane in sorted(lanes.items()):
        if suite not in banded:
            continue
        seen = banded[suite]
        if lane is None and seen:
            out.append(f"объявление: за набором {suite!r} зона умолчательных пулов не "
                       f"закреплена (None), а кейсы создают их в {sorted(seen)}")
        elif lane is not None and lane not in seen:
            out.append(f"объявление: за набором {suite!r} закреплена зона {lane!r}, но "
                       f"кейсы держат умолчательные пулы в {sorted(seen) or '∅'} — "
                       f"объявление пережило свой предмет")
    return out


# ── прогон ─────────────────────────────────────────────────────────────────────

def run(root: str) -> int:
    print("===== блок пула обязан входить в полосу своего набора =====")
    bands = load_bands(root)
    if bands is None:
        print(f"FAIL: объявления полос нет ({_BANDS_REL}) — судить не по чему; "
              f"«ноль находок» тут означало бы «ноль прочитанного».", file=sys.stderr)
        return 1

    findings: list = []
    files = dicts = total = templates = unparsed = 0
    observed_zones: dict = {}
    per_suite: dict = {}

    for rel in case_files(root):
        suite = os.path.splitext(os.path.basename(rel))[0]
        blocks, zones, counts, gate_findings = extract(root, rel, bands)
        files += 1
        dicts += counts["dicts"]
        templates += counts["templates"]
        unparsed += counts["unparsed"]
        total += len(blocks)
        findings += gate_findings
        if blocks:
            per_suite[suite] = len(blocks)
        observed_zones[suite] = zones
        findings += every_pool_suite_is_declared(bands, suite, blocks, rel)
        if suite in bands.SUITE_BANDS:
            findings += blocks_are_inside_their_band(bands, suite, blocks)
        findings += blocks_avoid_occupied(bands, blocks)

    findings += bands_are_pairwise_disjoint(bands)
    findings += occupied_inside_a_band_is_named(bands)
    findings += default_zone_lanes(bands, observed_zones)

    band_lines = []
    for suite, idx in sorted(bands.SUITE_BANDS.items(), key=lambda kv: kv[1]):
        band_lines.append(f"    #{idx} {suite:<14} v4 {bands.band_v4(idx)}  "
                          f"v6 {bands.band_v6(idx)}  блоков в кейсах: "
                          f"{per_suite.get(suite, 0)}")
    for idx, who in sorted(getattr(bands, "RESERVED_BANDS", {}).items()):
        band_lines.append(f"    #{idx} [бронь]        v4 {bands.band_v4(idx)}  "
                          f"v6 {bands.band_v6(idx)}  — {who.splitlines()[0]}")

    print(f"прочитано файлов кейсов: {files}; словарей с ключами пула: {dicts}; "
          f"блоков разобрано: {total}")
    print(f"пропущено: шаблонных значений {templates}, неразобранных строк {unparsed}")
    print(f"объявлено полос: {len(bands.SUITE_BANDS)} за наборами + "
          f"{len(getattr(bands, 'RESERVED_BANDS', {}))} в брони; "
          f"занятых сетей вне лестницы: {len(bands.OCCUPIED)}; "
          f"наборов «без посадки»: {len(getattr(bands, 'NO_LANDING_SUITES', {}))}")
    for line in band_lines:
        print(line)
    zone_lines = [f"    {s:<14} {sorted(z)}" for s, z in sorted(observed_zones.items()) if z]
    if zone_lines:
        print("зоны умолчательных пулов, увиденные в кейсах:")
        for line in zone_lines:
            print(line)

    if files == 0:
        print(f"FAIL: не прочитано НИ ОДНОГО файла кейсов ({_CASES_REL}) — обход "
              f"потерял предмет.", file=sys.stderr)
        return 1
    if total == 0:
        print("FAIL: не разобрано НИ ОДНОГО блока пула — либо ключи тела "
              "переименованы, либо разбор их больше не видит. «Ноль находок» в этом "
              "случае ничего не значит.", file=sys.stderr)
        return 1
    if findings:
        print(f"FAIL: {len(findings)} находк(а/и):", file=sys.stderr)
        for f in findings:
            print(f"  {f}", file=sys.stderr)
        print("EXCLUDE `(kind, block &&)` не знает ни зоны, ни проекта: блок пула — "
              "глобальное имя. Развод расписанием держится ровно до первого прогона, "
              "где наборы оказались рядом.", file=sys.stderr)
        return 1
    print("OK: каждый блок пула входит в полосу своего набора; полосы не "
          "пересекаются; умолчательные пулы наборов сидят в разных зонах.")
    return 0


# ── самопроверка инъекцией в обе стороны ───────────────────────────────────────

def _synthetic(root_src: str, td: str):
    """Синтетическое дерево: настоящее объявление + пустой каталог кейсов."""
    bands_dst = os.path.join(td, _BANDS_REL)
    os.makedirs(os.path.dirname(bands_dst))
    shutil.copyfile(os.path.join(root_src, _BANDS_REL), bands_dst)
    os.makedirs(os.path.join(td, _CASES_REL))


def _case(td: str, name: str, text: str):
    with open(os.path.join(td, _CASES_REL, name), "w", encoding="utf-8") as fh:
        fh.write(text)


_LEGAL_INTERNAL = '''CASES = []
CASES.append(Step(body={"name": "x", "kind": "EXTERNAL_PUBLIC",
                        "zoneId": "{{zoneC}}",
                        "v4CidrBlocks": ["100.100.7.0/24"], "v6CidrBlocks": [],
                        "isDefault": True}))
'''

_LEGAL_ADDRESS = '''CASES = []
CASES.append(Step(body={"name": "y", "kind": "EXTERNAL_PUBLIC",
                        "zoneId": "{{zoneD}}",
                        "v4CidrBlocks": [], "v6CidrBlocks": ["2001:db8:101:9::/64"],
                        "isDefault": True}))
'''


def self_test(root_src: str) -> int:
    ok = True

    def check(cond, msg):
        nonlocal ok
        if not cond:
            print(f"SELF-TEST FAIL: {msg}", file=sys.stderr)
            ok = False

    # (1) КОНТРОЛЬ: законные блоки внутри своих полос — гейт МОЛЧИТ.
    #     Без этой стороны инъекция ничего не доказывает: краснеющий на всём гейт
    #     краснеет и на дефекте.
    with tempfile.TemporaryDirectory() as td:
        _synthetic(root_src, td)
        _case(td, "internal-pool.py", _LEGAL_INTERNAL)
        _case(td, "address.py", _LEGAL_ADDRESS)
        check(run(td) == 0, "законные блоки внутри полос объявлены находкой")

    # (2) ИНЪЕКЦИЯ: блок вне полосы своего набора. Он ОТЛИЧАЕТСЯ от всех чужих
    #     блоков — и всё равно обязан краснеть, потому что ложится поверх чужой
    #     полосы. Ровно это различие между «отличается» и «входит».
    with tempfile.TemporaryDirectory() as td:
        _synthetic(root_src, td)
        _case(td, "internal-pool.py",
              _LEGAL_INTERNAL.replace("100.100.7.0/24", "100.101.77.0/24"))
        _case(td, "address.py", _LEGAL_ADDRESS)
        bands = load_bands(td)
        blocks, _, _, _ = extract(td, os.path.join(_CASES_REL, "internal-pool.py"), bands)
        f = blocks_are_inside_their_band(bands, "internal-pool", blocks)
        check(len(f) == 1 and "internal-pool.py:" in f[0] and "100.101.77.0/24" in f[0],
              f"блок вне полосы не назван координатой: {f}")
        check(run(td) == 1, "блок вне полосы не покраснел на полном прогоне")

    # (3) ИНЪЕКЦИЯ: v6-блок вне полосы (лестница v6 выводится отдельно).
    with tempfile.TemporaryDirectory() as td:
        _synthetic(root_src, td)
        _case(td, "internal-pool.py", _LEGAL_INTERNAL)
        _case(td, "address.py", _LEGAL_ADDRESS.replace("2001:db8:101:9::/64",
                                                       "2001:db8:cafe::/64"))
        check(run(td) == 1, "v6-блок вне полосы не покраснел")

    # (4) КОНТРОЛЬ: кросс-семейный отрицательный кейс — v6-префикс в v4-поле.
    #     Судится полосой СВОЕГО семейства, поэтому законная форма молчит.
    with tempfile.TemporaryDirectory() as td:
        _synthetic(root_src, td)
        _case(td, "internal-pool.py", _LEGAL_INTERNAL.replace(
            '"v4CidrBlocks": ["100.100.7.0/24"], "v6CidrBlocks": []',
            '"v4CidrBlocks": ["2001:db8:100:9::/64"], "v6CidrBlocks": []'))
        _case(td, "address.py", _LEGAL_ADDRESS)
        check(run(td) == 0, "кросс-семейный блок внутри своей полосы объявлен находкой")

    # (5) ИНЪЕКЦИЯ: набор с пулами, которого нет в объявлении.
    with tempfile.TemporaryDirectory() as td:
        _synthetic(root_src, td)
        _case(td, "internal-pool.py", _LEGAL_INTERNAL)
        _case(td, "address.py", _LEGAL_ADDRESS)
        _case(td, "brand-new.py", _LEGAL_INTERNAL)
        check(run(td) == 1, "новый набор с пулами прошёл без полосы")

    # (6) Набор «без посадки»: с `mode="gate"` — тихо, без него — находка.
    with tempfile.TemporaryDirectory() as td:
        _synthetic(root_src, td)
        _case(td, "internal-pool.py", _LEGAL_INTERNAL)
        _case(td, "address.py", _LEGAL_ADDRESS)
        _case(td, "authz-deny.py",
              'emit("APL-CR", "POST", "/vpc/v1/addressPools",\n'
              '     {"kind": "EXTERNAL_PUBLIC", "v4CidrBlocks": ["198.51.100.0/24"]},\n'
              '     subj, mode="gate")\n')
        check(run(td) == 0, "набор «без посадки» с утверждённым отказом объявлен находкой")
        _case(td, "authz-deny.py",
              'emit("APL-CR", "POST", "/vpc/v1/addressPools",\n'
              '     {"kind": "EXTERNAL_PUBLIC", "v4CidrBlocks": ["198.51.100.0/24"]},\n'
              '     subj)\n')
        check(run(td) == 1, "создание пула без утверждения отказа прошло молча")

    # (7) ИНЪЕКЦИЯ: два набора держат умолчательные пулы в ОДНОЙ зоне — это и есть
    #     та ось, ради которой прежде стояло расписание.
    with tempfile.TemporaryDirectory() as td:
        _synthetic(root_src, td)
        _case(td, "internal-pool.py", _LEGAL_INTERNAL.replace("{{zoneC}}", "{{zoneD}}"))
        _case(td, "address.py", _LEGAL_ADDRESS)
        check(run(td) == 1, "общая зона умолчательных пулов прошла молча")

    # (8) Предпосылка: кейсов нет вовсе / блоков нет вовсе — отказ, не чистота.
    with tempfile.TemporaryDirectory() as td:
        _synthetic(root_src, td)
        check(run(td) == 1, "дерево без блоков объявлено чистым")
    with tempfile.TemporaryDirectory() as td:
        check(run(td) == 1, "дерево без объявления полос объявлено чистым")

    # (9) Шаблон postman — не сеть; он обязан считаться отдельно, а не молча
    #     проходить как разобранный блок.
    with tempfile.TemporaryDirectory() as td:
        _synthetic(root_src, td)
        _case(td, "internal-pool.py",
              _LEGAL_INTERNAL + 'CASES.append(Step(body={"v4CidrBlocks": ["{{rmCidr}}"]}))\n')
        _case(td, "address.py", _LEGAL_ADDRESS)
        bands = load_bands(td)
        _, _, counts, _ = extract(td, os.path.join(_CASES_REL, "internal-pool.py"), bands)
        check(counts["templates"] == 1, f"шаблон не посчитан отдельно: {counts}")
        check(run(td) == 0, "шаблонное значение объявлено находкой")

    print("SELF-TEST OK" if ok else "SELF-TEST FAILED")
    return 0 if ok else 1


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--self-test", action="store_true",
                    help="доказать инъекцией, что блок вне полосы краснеет, а внутри — нет")
    ap.add_argument("--root", default=None)
    args = ap.parse_args()
    root = args.root or os.path.abspath(
        os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
    if args.self_test:
        return self_test(root)
    return run(root)


if __name__ == "__main__":
    sys.exit(main())
