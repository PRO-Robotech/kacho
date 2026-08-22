#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Гейт: волна церемонии не подходит к потолку аккаунтов ОДНОЙ личности вплотную.

ПРЕДМЕТ. Аккаунт занимает слот потолка своей ЛИЧНОСТИ (вид `iam.account`, носитель —
внешний идентификатор входа), а ВСЯ волна церемонии идёт под ОДНИМ человеком: другой
её вести не может — аккаунт принадлежит пользователю by construction, и машинный
предъявитель его не создаёт. Значит одновременно живые аккаунты волны складываются, и
их сумма упирается в один и тот же потолок.

ЧЕМ ЭТО КОНЧАЕТСЯ, ЕСЛИ НЕ СЧИТАТЬ. Отказ приходит НЕ ТОМУ кейсу, который исчерпал
полку, а тому, который создаёт аккаунт следующим, — и читается как каскад отказов в
правах, потому что квоту в тексте никто не связывает с волной. Наблюдалось: пик 4 при
потолке 5, запас в одну единицу, и падение досталось кейсу из другой коллекции.

ЧТО СЧИТАЕТСЯ И ПОЧЕМУ ИМЕННО ТАК.

  * ПОРЯДОК ВОЛНЫ берётся у ЕДИНСТВЕННОГО объявления (`ceremony_credentials.py`,
    `stems_for_suite`) — того же, которым волну гоняет прогонщик. Свой список
    разошёлся бы с деревом молча: в этом репозитории такое уже случалось.
  * ФОРМЫ создания и снятия берутся у соседнего гейта
    (`assert-cocreated-child-is-torn-down.py`), а не переписываются. Два места об
    одном предмете расходятся на первой же правке генератора.
  * ЛИЧНОСТЬ — пара предъявителей одного человека церемонии. Второй человек
    (`…NoBindings`) — ДРУГАЯ личность, и его потолок свой; складывать их нельзя.
  * СЛОТ ЗАНЯТ ОТ СОЗДАНИЯ ДО ПОСЛЕДНЕГО СНЯТИЯ этой переменной. Не «до первого»:
    у волны есть кейс, чей ПРЕДМЕТ — законный отказ удаления (аккаунт непуст), и
    после такого снятия строка ЖИВА. Считать по первому снятию значит занизить пик
    ровно там, где он опасен, — а занижение здесь хуже завышения.
  * СОЗДАНИЕМ считается шаг, ЗАХВАТИВШИЙ идентификатор аккаунта. Ответ 200 таким
    признаком не является: кейс про занятое имя получает 200 и `Operation.error`,
    строки за ним нет. Наивный предикат «assert 200 ⇒ слот занят» на нём и ломается.

ПРЕДИКАТ. Находка, если пик ≥ потолок − 1, то есть запас меньше двух. Запас в одну
единицу означает, что ЛЮБОЙ следующий аккаунт под тем же человеком где угодно в волне
снова упрёт в потолок; запас в две — что для этого нужно ДВА новых, и такое изменение
уже видно в обзоре.

ПРОВЕРКА ПРЕДПОСЫЛОК. «Ноль находок» обязано быть отличимо от «ноль прочитанного»,
поэтому пустая волна, нечитаемая коллекция, ноль созданий, невыведенный базовый
уровень и непрочитанный потолок — ОТКАЗ, а не «чисто».

Использование:
    python3 deploy/scripts/assert-identity-account-peak-under-ceiling.py [--root .]
    python3 deploy/scripts/assert-identity-account-peak-under-ceiling.py --self-test
"""
from __future__ import annotations

import argparse
import ast
import importlib.util
import json
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))

SUITE = "services/iam/tests/newman"
CEREMONY_DECL = "tests/authz-fixtures/ceremony_credentials.py"
CEREMONY_SEED = "tests/authz-fixtures/prodseed_ceremony.py"
NEIGHBOUR_GATE = "deploy/scripts/assert-cocreated-child-is-torn-down.py"
QUOTA_MIGRATION = "services/iam/internal/migrations/484002_account_quota_identity_carrier.sql"
BOOTSTRAP_SOURCE = "services/iam/internal/apps/kacho/api/user/internal_upsert.go"

# Предъявители ОДНОГО человека церемонии. Оба обязаны стоять в объявлении церемонии —
# это проверяется, а не предполагается: переименуют один, и счёт молча перестанет
# видеть половину созданий.
CEREMONY_IDENTITY_BEARERS = ("jwtHumanCeremony", "jwtHumanCeremonyStepUp")

# Предъявитель шага читается из пре-скрипта: генератор пишет туда маркер, а ключа
# `auth` в коллекции нет вовсе.
BEARER = re.compile(r"bearer from env '([A-Za-z0-9_]+)'")

# Величина потолка — там, где она НАЗНАЧАЕТСЯ, а не там, где её ожидают тексты
# отказов. Прецедент чтения этой же миграции гейтом уже есть
# (`identity_quota_carrier_test.go`).
CEILING = re.compile(
    r"\('lim-\d+',\s*'DEFAULT',\s*'',\s*'iam\.account',\s*(\d+)\)")

# Базовый уровень выводится из ДВУХ источников, и оба самоистекают.
BOOTSTRAP_ON_FIRST_LOGIN = re.compile(r"ownedAccounts == 0")
SEED_OWN_ACCOUNT = re.compile(r'_req\(\s*"POST",\s*f"\{[A-Z_]+\}/iam/v1/accounts"')


# ТРЕБУЕМЫЙ ЗАПАС. Две единицы, а не одна: запас в единицу означает, что ЛЮБОЙ
# следующий аккаунт под тем же человеком где угодно в волне снова упрёт в потолок.
# При двух для этого нужно ДВА новых создания — а такое изменение видно в обзоре.
HEADROOM_REQUIRED = 2


def decide(peak: int, ceiling: int) -> tuple[int, bool]:
    """Вердикт по паре «пик, потолок» → (код возврата, находка ли).

    ЕДИНСТВЕННЫЙ производитель вердикта, и зовут его ОБА пути — прод и
    самопроверка. Второй кодек здесь запрещён: он расходится с первым молча и
    именно там, где расхождение не видно.

    Цена измерена, а не предположена. Прежняя редакция сравнивала литералы,
    написанные в самой пробе, вместо того чтобы звать предикат: правка ОДНОЙ
    прод-строки (`peak >= ceiling - 1` → `peak >= ceiling`) оставляла
    самопроверку ЗЕЛЁНОЙ и пропускала коллекцию с пиком 4 при потолке 5. Порог —
    единственный предмет этого гейта, и его не держало ничего.
    """
    finding = ceiling - peak < HEADROOM_REQUIRED
    return (1 if finding else 0), finding


class PremiseError(RuntimeError):
    """Предпосылка вердикта не выполнена — это ОТКАЗ, а не «чисто»."""


def _load(path: str, name: str):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise PremiseError(f"модуль не загружается: {path}")
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def load_declarations(root: str):
    """Волна и формы — из их единственных объявлений, а не из копии здесь."""
    decl = os.path.join(root, CEREMONY_DECL)
    gate = os.path.join(root, NEIGHBOUR_GATE)
    for p in (decl, gate):
        if not os.path.exists(p):
            raise PremiseError(f"объявления нет в дереве: {p}")
    return _load(decl, "kacho_ceremony_decl"), _load(gate, "kacho_teardown_gate")


def read_ceiling(root: str) -> int:
    p = os.path.join(root, QUOTA_MIGRATION)
    try:
        text = open(p, encoding="utf-8").read()
    except OSError as exc:
        raise PremiseError(f"потолок не прочитан: {exc}") from exc
    m = CEILING.search(text)
    if not m:
        raise PremiseError(
            f"величина потолка не найдена в {QUOTA_MIGRATION} — форма объявления "
            f"изменилась, и вердикт был бы о выдуманном числе")
    return int(m.group(1))


def read_base_level(root: str) -> tuple[int, list[str]]:
    """Сколько аккаунтов у личности церемонии ДО первого шага волны.

    Выводится, а не выписывается: выписанное число пережило бы свой предмет молча.

    НАПРАВЛЕНИЕ ОШИБКИ НАЗВАНО. Создания посева считаются ВСЕ, без разбора, какому
    человеку они достаются: сегодня человек в посеве один, а разбирать вызывающего
    разбором питона значило бы завести второй, хрупкий предикат. Если посев заведёт
    аккаунт ВТОРОМУ человеку, базовый уровень окажется завышен — гейт станет строже
    и скажет об этом громко. Обратная ошибка (занижение) здесь была бы тихой и
    опасной, поэтому размен выбран в эту сторону сознательно.
    """
    why: list[str] = []
    src = os.path.join(root, BOOTSTRAP_SOURCE)
    try:
        text = open(src, encoding="utf-8").read()
    except OSError as exc:
        raise PremiseError(f"источник личного аккаунта не прочитан: {exc}") from exc
    if not BOOTSTRAP_ON_FIRST_LOGIN.search(text):
        raise PremiseError(
            f"в {BOOTSTRAP_SOURCE} нет ветки «аккаунтов ноль ⇒ завести личный» — "
            f"базовый уровень выведен быть не может")
    base = 1
    why.append(f"личный аккаунт первого входа (+1, {BOOTSTRAP_SOURCE})")

    seed = os.path.join(root, CEREMONY_SEED)
    try:
        seed_text = open(seed, encoding="utf-8").read()
    except OSError as exc:
        raise PremiseError(f"посев церемонии не прочитан: {exc}") from exc
    seeded = len(SEED_OWN_ACCOUNT.findall(seed_text))
    if seeded:
        base += seeded
        why.append(f"аккаунты посева церемонии (+{seeded}, {CEREMONY_SEED})")
    else:
        why.append(f"посев церемонии аккаунтов не заводит (+0, {CEREMONY_SEED})")
    return base, why


def _pre_code(step) -> str:
    return "\n".join(
        "\n".join(ev.get("script", {}).get("exec", []) or [])
        for ev in (step.get("event") or []) if ev.get("listen") == "prerequest")


def wave_collections(root: str, decl) -> list[tuple[str, dict]]:
    """Коллекции волны В ПОРЯДКЕ ИСПОЛНЕНИЯ, разобранные."""
    stems = decl.stems_for_suite(SUITE, root)
    if not stems:
        raise PremiseError("волна пуста — считать нечего, и это не «чисто»")
    out = []
    for stem in stems:
        p = os.path.join(root, SUITE, "collections", f"{stem}.postman_collection.json")
        if not os.path.exists(p):
            raise PremiseError(f"коллекция волны не найдена: {p}")
        try:
            out.append((stem, json.load(open(p, encoding="utf-8"))))
        except ValueError as exc:
            raise PremiseError(f"коллекция не разбирается: {p}: {exc}") from exc
    return out


def timeline_of(wave, gate, bearers=CEREMONY_IDENTITY_BEARERS):
    """Линейная лента событий волны: (знак, переменная, коллекция, кейс, шаг)."""
    events, cases, steps = [], 0, 0
    for stem, coll in wave:
        for case in coll.get("item", []):
            if "item" not in case:
                continue
            cases += 1
            cname = case.get("name", "?").split(" —")[0]
            for st in gate._leaves(case["item"]):
                steps += 1
                req = st.get("request", {}) or {}
                url, method = gate._raw_url(req), req.get("method", "")
                sname = st.get("name", "?")
                if method == "POST" and gate.CREATE_ACCOUNT.search(url):
                    m = BEARER.search(_pre_code(st))
                    if m and m.group(1) in bearers:
                        cap = gate.CAPTURE_ACCOUNT.search(gate._test_code(st))
                        if cap:
                            events.append(("+", cap.group("var"), stem, cname, sname))
                elif method == "DELETE":
                    # Снятие предъявителем НЕ фильтруется: слот освобождает удаление
                    # строки, кем бы оно ни было сделано.
                    d = gate.DELETE_ACCOUNT.search(url)
                    if d:
                        events.append(("-", d.group(1), stem, cname, sname))
    return events, cases, steps


def peak_of(events, base):
    """Пик одновременно живых. Слот занят до ПОСЛЕДНЕГО снятия своей переменной.

    Форма, на которую счёт НЕ рассчитан, называется отказом, а не считается
    приблизительно: переменная, в которую пишут ДВА разных аккаунта, делает
    «последнее снятие» неоднозначным — первый аккаунт пережил бы свою переменную,
    и пик вышел бы занижен. В дереве такой формы нет; появится — гейт скажет об
    этом, вместо того чтобы молча ошибиться в опасную сторону.
    """
    seen = set()
    for sign, v, stem, cname, sname in events:
        if sign != "+":
            continue
        if v in seen:
            raise PremiseError(
                f"переменная {v!r} принимает второй аккаунт ({stem}/{cname}/{sname}) — "
                f"счёт «до последнего снятия» на такую форму не рассчитан и занизил бы пик")
        seen.add(v)
    born = {v for sign, v, *_ in events if sign == "+"}
    last_release = {}
    for i, (sign, v, *_rest) in enumerate(events):
        if sign == "-" and v in born:
            last_release[v] = i
    live = peak = base
    at = None
    n_created = n_released = 0
    for i, (sign, v, stem, cname, sname) in enumerate(events):
        if sign == "+":
            live += 1
            n_created += 1
        elif last_release.get(v) == i:
            live -= 1
            n_released += 1
        else:
            continue
        if live > peak:
            peak, at = live, (stem, cname, sname)
    return peak, at, n_created, n_released


def audit(root: str):
    decl, gate = load_declarations(root)
    for b in CEREMONY_IDENTITY_BEARERS:
        if b not in decl.CEREMONY_ONLY_ENV:
            raise PremiseError(
                f"предъявитель {b!r} не объявлен в CEREMONY_ONLY_ENV — счёт видел бы "
                f"не все создания под личностью церемонии")
    ceiling = read_ceiling(root)
    base, why = read_base_level(root)
    wave = wave_collections(root, decl)
    events, cases, steps = timeline_of(wave, gate)
    peak, at, n_created, n_released = peak_of(events, base)
    if n_created == 0:
        raise PremiseError(
            "созданий аккаунта под личностью церемонии НЕ НАЙДЕНО ни одного — "
            "предикат ослеп, чинить надо гейт, а не выходить успехом")
    census = {
        "collections": len(wave), "cases": cases, "steps": steps,
        "created": n_created, "released": n_released,
        "order": [stem for stem, _ in wave],
    }
    return peak, at, ceiling, base, why, census


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--root", default=REPO, help="корень монорепо")
    ap.add_argument("--self-test", action="store_true",
                    help="доказательство инъекцией в обе стороны")
    a = ap.parse_args(argv)
    if a.self_test:
        return self_test()

    try:
        peak, at, ceiling, base, why, census = audit(a.root)
    except PremiseError as exc:
        print(f"ОТКАЗ (предпосылка): {exc}", file=sys.stderr)
        return 2

    print(f"волна церемонии выведена из {CEREMONY_DECL} (--stems, порядок исполнения):")
    print(f"  {' → '.join(census['order'])}")
    print(f"осмотрено: {census['collections']} коллекц(ий), {census['cases']} кейс(ов), "
          f"{census['steps']} шаг(ов); созданий аккаунта под личностью церемонии "
          f"{census['created']}, освобождений {census['released']}")
    print("базовый уровень " + str(base) + ": " + "; ".join(why))
    print(f"потолок {ceiling} ({QUOTA_MIGRATION}); пик одновременно живых {peak}, "
          f"запас {ceiling - peak}")

    code, finding = decide(peak, ceiling)
    if finding:
        where = " / ".join(at) if at else "базовый уровень"
        print(f"\nНАХОДКА: пик {peak} при потолке {ceiling} — запас {ceiling - peak}.",
              file=sys.stderr)
        print(f"  достигнут: {where}", file=sys.stderr)
        print("  Следующий аккаунт под тем же человеком ГДЕ УГОДНО в волне упрётся в",
              file=sys.stderr)
        print("  потолок, и отказ достанется не тому кейсу, который его вызвал.",
              file=sys.stderr)
        print("  Чинится волной, а не потолком: разнесите долгоживущий аккаунт и",
              file=sys.stderr)
        print("  недолговечные так, чтобы они не пересекались.", file=sys.stderr)
        return code

    print(f"запас не меньше {HEADROOM_REQUIRED}: одиночное новое создание в волну "
          f"помещается.")
    return code


# ─────────────────────────────────────────────────────────────────────────────
# Доказательство инъекцией — В ОБЕ СТОРОНЫ
# ─────────────────────────────────────────────────────────────────────────────
# Утверждения гоняют ТУ ЖЕ функцию `peak_of`, что и вердикт: проба, считающая пик
# своими словами, проверяла бы свою копию. Каждому отрицанию дан положительный
# контроль — без него «находка есть» зеленело бы на предикате, который краснеет
# всегда.
def _ev(sign, var, step="шаг"):
    return (sign, var, "коллекция", "кейс", step)


def _threshold_sites(source: str) -> list[str]:
    """Имена функций, где пик сравнивается с потолком. Судит РАЗБОР, а не поиск
    по образцу: слова `peak` и `ceiling` стоят в этом файле десятками, в том
    числе в текстах и комментариях, и предикат по подстроке краснел бы на
    собственных объяснениях."""
    tree = ast.parse(source)
    owner: dict[int, str] = {}
    for fn in ast.walk(tree):
        if isinstance(fn, ast.FunctionDef):
            for node in ast.walk(fn):
                if isinstance(node, ast.Compare):
                    owner.setdefault(id(node), fn.name)
    sites = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Compare):
            continue
        names = {n.id for n in ast.walk(node) if isinstance(n, ast.Name)}
        if {"peak", "ceiling"} <= names:
            sites.append(owner.get(id(node), "<уровень модуля>"))
    return sorted(sites)


def self_test() -> int:
    ok = True
    asserts = 0

    def check(label, got, want):
        nonlocal ok, asserts
        asserts += 1
        if got == want:
            print(f"  ok   {label}")
        else:
            print(f"  FAIL {label}: получили {got}, ждали {want}")
            ok = False

    def note(passed, label):
        """Утверждение, у которого нет пары «получили/ждали» (исключения)."""
        nonlocal ok, asserts
        asserts += 1
        if passed:
            print(f"  ok   {label}")
        else:
            print(f"  FAIL {label}")
            ok = False

    print("── пик считается по ленте событий")
    # Дефект во плоти: долгоживущий аккаунт перекрыт двумя недолговечными.
    overlap = [_ev("+", "long"), _ev("+", "t1"), _ev("-", "t1"),
               _ev("+", "t2"), _ev("-", "t2"), _ev("-", "long")]
    check("перекрытие даёт пик 4 при базовом 2", peak_of(overlap, 2)[0], 4)
    # Законный близнец той же формы: те же кейсы, но не пересекаются.
    apart = [_ev("+", "long"), _ev("-", "long"), _ev("+", "t1"), _ev("-", "t1"),
             _ev("+", "t2"), _ev("-", "t2")]
    check("развёденные во времени дают пик 3", peak_of(apart, 2)[0], 3)
    check("базовый уровень входит в пик", peak_of([], 2)[0], 2)

    print("── снятие, которого не было, слот НЕ освобождает")
    # Кейс про законный отказ удаления: первое снятие отвергнуто, живёт до второго.
    refused = [_ev("+", "a"), _ev("-", "a", "отказ"), _ev("+", "b"),
               _ev("-", "b"), _ev("-", "a", "уборка")]
    check("слот живёт до ПОСЛЕДНЕГО снятия (пик 4)", peak_of(refused, 2)[0], 4)
    # Законный близнец: те же события без второго снятия — счёт по первому снятию
    # дал бы 3 и спрятал бы ровно этот случай.
    naive = [_ev("+", "a"), _ev("-", "a"), _ev("+", "b"), _ev("-", "b")]
    check("одиночное снятие освобождает сразу (пик 3)", peak_of(naive, 2)[0], 3)

    print("── переменная, принимающая ВТОРОЙ аккаунт, — отказ, а не приблизительный счёт")
    try:
        peak_of([_ev("+", "a"), _ev("-", "a"), _ev("+", "a")], 2)
        note(False, "повторное создание в ту же переменную прошло молча")
    except PremiseError:
        note(True, "повторное создание в ту же переменную: ОТКАЗ")
    # Законный близнец той же формы: РАЗНЫЕ переменные считаются как обычно.
    check("две разные переменные подряд считаются (пик 3)",
          peak_of([_ev("+", "a"), _ev("-", "a"), _ev("+", "b")], 2)[0], 3)

    print("── снятие чужой переменной ничего не освобождает")
    foreign = [_ev("+", "mine"), _ev("-", "seeded"), _ev("+", "second")]
    check("снятие незнакомой переменной пропущено (пик 4)", peak_of(foreign, 2)[0], 4)

    print("── предикат вердикта: ЗОВЁТСЯ тот же, что на прод-пути")
    # Утверждается ПАРА (код возврата, находка), а не пересчитанное здесь
    # неравенство: сравнение литералов проверяло бы работу оператора `>=`, а не
    # гейт, и правка порога в проде оставалась бы незамеченной.
    for peak, ceiling, want in ((4, 5, (1, True)), (3, 5, (0, False)),
                                (5, 5, (1, True)), (2, 5, (0, False)),
                                (0, 1, (1, True))):
        check(f"пик {peak} при потолке {ceiling} → {want}", decide(peak, ceiling), want)

    print("── сравнение пика с потолком в модуле РОВНО ОДНО, и живёт оно в decide")
    # Иначе предикат можно обойти, вписав своё неравенство рядом с вызовом: тогда
    # утверждения выше остались бы зелёными, а вердикт производился бы не ими.
    sites = _threshold_sites(open(os.path.abspath(__file__), encoding="utf-8").read())
    check("мест сравнения", sites, ["decide"])

    print("── лента строится из коллекции, а не из выдумки")
    coll = {"item": [{"name": "CASE-A — заголовок", "item": [
        {"name": "CASE-A :: create", "request": {
            "method": "POST", "url": {"raw": "{{baseUrl}}/iam/v1/accounts"}},
         "event": [
             {"listen": "prerequest", "script": {"exec": [
                 "// per-step auth: bearer from env 'jwtHumanCeremony'"]}},
             {"listen": "test", "script": {"exec": [
                 "const v = (j.metadata && j.metadata.accountId);",
                 "  if (v !== undefined && v !== null) pm.environment.set('accX', String(v));"]}}]},
        {"name": "CASE-A :: dup", "request": {
            "method": "POST", "url": {"raw": "{{baseUrl}}/iam/v1/accounts"}},
         "event": [
             {"listen": "prerequest", "script": {"exec": [
                 "// per-step auth: bearer from env 'jwtHumanCeremony'"]}},
             {"listen": "test", "script": {"exec": ["pm.test('status 200', () => {});"]}}]},
        {"name": "CASE-A :: machine", "request": {
            "method": "POST", "url": {"raw": "{{baseUrl}}/iam/v1/accounts"}},
         "event": [
             {"listen": "prerequest", "script": {"exec": [
                 "// per-step auth: bearer from env 'jwtAccountAdminA'"]}},
             {"listen": "test", "script": {"exec": [
                 "const v = (j.metadata && j.metadata.accountId);",
                 "  if (v !== undefined && v !== null) pm.environment.set('accY', String(v));"]}}]},
        {"name": "CASE-A :: teardown", "request": {
            "method": "DELETE", "url": {"raw": "{{baseUrl}}/iam/v1/accounts/{{accX}}"}},
         "event": []},
    ]}]}
    try:
        _decl, gate = load_declarations(REPO)
    except PremiseError as exc:
        note(False, f"предпосылка самопроверки: {exc}")
        return 1
    events, cases, steps = timeline_of([("синт", coll)], gate)
    check("захвативший идентификатор создатель учтён", [e[0] for e in events].count("+"), 1)
    check("создание без захвата (занятое имя) не учтено",
          [e[1] for e in events], ["accX", "accX"])
    check("кейсов прочитано", cases, 1)
    check("шагов прочитано", steps, 4)

    print("── предпосылки: пустое и нечитаемое суть ОТКАЗ, а не «чисто»")
    for label, fn in (
        ("пустая волна", lambda: wave_collections(REPO, _EmptyDecl())),
        ("потолка нет в дереве", lambda: read_ceiling(os.path.join(REPO, "нет-такого"))),
        ("источника базового уровня нет",
         lambda: read_base_level(os.path.join(REPO, "нет-такого"))),
    ):
        try:
            fn()
            note(False, f"{label}: прошло молча")
        except PremiseError:
            note(True, f"{label}: ОТКАЗ")

    print("── настоящее дерево читается, перепись непуста")
    try:
        _peak, _at, ceiling, base, _why, census = audit(REPO)
        for label, got in (("коллекций", census["collections"]), ("шагов", census["steps"]),
                           ("созданий", census["created"]), ("потолок", ceiling),
                           ("базовый уровень", base)):
            note(got > 0, f"{label} > 0 ({got})" if got > 0
                 else f"{label} == 0 — предикат ослеп")
    except PremiseError as exc:
        note(False, f"настоящее дерево: {exc}")

    print()
    # Перепись СВОЯ, а не только дерева: «ноль провалов» обязано быть отличимо от
    # «ноль прогнанного», и число обязано быть посчитанным, а не выписанным.
    print(f"утверждений исполнено: {asserts}")
    print("PASS: пик аккаунтов личности" if ok else "FAIL: пик аккаунтов личности")
    return 0 if ok else 1


class _EmptyDecl:
    """Объявление, отдающее пустую волну — фикстура предпосылки."""

    @staticmethod
    def stems_for_suite(_suite, _root):
        return []


if __name__ == "__main__":
    sys.exit(main())
