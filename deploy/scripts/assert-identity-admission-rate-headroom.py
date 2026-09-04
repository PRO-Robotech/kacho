#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Гейт: ни одна личность волны не заводит аккаунтов больше, чем позволяет ТЕМП.

ПРЕДМЕТ — ДРУГОЙ, ЧЕМ У СОСЕДА, И РАЗЛИЧИЕ НЕСУЩЕЕ. Сосед
(`assert-identity-account-peak-under-ceiling.py`) считает ОДНОВРЕМЕННО ЖИВЫЕ аккаунты
против потолка ОБЪЁМА: снятие освобождает слот. Здесь считается ЧИСЛО ЗАВЕДЕНИЙ за
окно против потолка ТЕМПА (`#618`, умолчание — три заведения в час на внешний
идентификатор входа), и снятие НЕ возвращает ничего: списывается допуск, а не место.
Волна укладывается в окно целиком, поэтому «за окно» здесь равно «за волну».

ЧЕМ ЭТО КОНЧИЛОСЬ, ИЗМЕРЕННО. Восемь заведений волны шли под ОДНИМ человеком
церемонии, у которого посев уже занял два допуска (личный аккаунт первого входа плюс
собственный аккаунт человека): десять списаний при потолке три. Первое заведение
проходило, семь получали `RESOURCE_EXHAUSTED`, и падало не то место, которое полку
исчерпало, — падали шаги, шедшие СЛЕДОМ за несозданным аккаунтом. Прогон
`32612214045`: 83 упавших утверждения, диагноз по первому отказу, а не по именам
упавших шагов.

ПОЧЕМУ ГЕЙТ, А НЕ КОММЕНТАРИЙ. Отказ по темпу приходит через сорок минут прогона и
на чужом кейсе; здесь он приходит за секунду и с именем виновника. Гейт статический —
стенда не требует и обязан падать до подъёма первого кластера.

ЧТО СЧИТАЕТСЯ И ПОЧЕМУ ИМЕННО ТАК.

  * ПОРЯДОК ВОЛНЫ, ФОРМЫ ЗАВЕДЕНИЯ и РАЗБИЕНИЕ ПРЕДЪЯВИТЕЛЕЙ ПО ЛИЧНОСТЯМ берутся у
    соседнего гейта и у единственного объявления церемонии — своей копии здесь нет.
    Два места об одном предмете расходятся на первой же правке генератора.
  * ЗАВЕДЕНИЕМ считается шаг, ЗАХВАТИВШИЙ идентификатор аккаунта, — тот же признак,
    что у соседа. Ответ 200 таким признаком не является: кейс про занятое имя
    получает 200 и `Operation.error`, строки за ним нет, и допуск не списан (отказ
    уникальности отменяет транзакцию раньше, чем отложенный триггер её фиксирует).
  * БАЗОВЫЙ УРОВЕНЬ — те же два источника, что у соседа, и он ПО ЛИЧНОСТИ: личный
    аккаунт первого входа достаётся каждой (безусловная ветвь вставки окна допуска),
    аккаунты посева — только той личности, для которой посев их заводит. Кому именно,
    объявлено (`ceremony_credentials.SEED_OWNED_ACCOUNTS`), а объявление держится
    сверкой с числом, выведенным из исходника посева: разойдутся — отказ. Раздать
    аккаунты посева ВСЕМ было бы завышением на единицу у восьми личностей, то есть
    находкой там, где её нет.

ПРЕДИКАТ. Находка, если списаний ≥ потолок, то есть запаса не осталось вовсе. Запас
требуется хотя бы в единицу: без него ЛЮБОЕ новое заведение под той же личностью
упрётся в потолок — а это и есть состояние, из которого гейт заведён.

ПРОВЕРКА ПРЕДПОСЫЛОК. «Ноль находок» обязано быть отличимо от «ноль прочитанного»:
пустая волна, непрочитанный потолок темпа, невыведенный базовый уровень и ноль
заведений под всеми личностями сразу — ОТКАЗ, а не «чисто».

Использование:
    python3 deploy/scripts/assert-identity-admission-rate-headroom.py [--root .]
    python3 deploy/scripts/assert-identity-admission-rate-headroom.py --self-test
"""
from __future__ import annotations

import argparse
import ast
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))

# Сосед — ЕДИНСТВЕННЫЙ источник форм, порядка волны и базового уровня. Импортируется,
# а не переписывается: переписанная копия разошлась бы с ним молча.
PEAK_GATE = "deploy/scripts/assert-identity-account-peak-under-ceiling.py"

RATE_MIGRATIONS_DIR = "services/iam/internal/migrations"

# Величина темпа — там, где она НАЗНАЧАЕТСЯ, а не там, где её ожидают тексты отказов.
#
# ИЩЕТСЯ ПО СОДЕРЖИМОМУ, А НЕ ПО ИМЕНИ ФАЙЛА — тот же класс, что у соседа:
# сведение цепочки iam в одну первичную миграцию растворило имя
# `20260822231600_account_admission_rate.sql`, и гейт падал СВОЕЙ предпосылкой.
#
# Форм записи ДВЕ, распознаватель обязан знать обе:
#   • форма дампа (сегодняшняя):
#       INSERT INTO kacho_iam.account_admission_rate_limits
#         (id, kind, max_events, window_seconds, ...) VALUES (1, 'iam.account', 3, 3600, ...);
#   • форма рукописной миграции (прежняя, INSERT … SELECT):
#       INSERT INTO kacho_iam.account_admission_rate_limits (kind, max_events, window_seconds)
#       SELECT 'iam.account', 3, 3600
#
# Якорь — имя таблицы плюс хвост `'iam.account', <заведений>, <секунд>`. Имя
# таблицы обязательно: без него регулярка поймала бы строку ОБЪЁМНОГО потолка
# (kacho_iam.limits), у которой за видом стоит одно число, а не пара. `[^;]*?`
# держит поиск внутри ОДНОГО оператора — иначе якорь одного INSERT дотянулся бы
# до значений следующего.
RATE = re.compile(
    r"kacho_iam\.account_admission_rate_limits\b[^;]*?"
    r"'iam\.account',\s*(\d+),\s*(\d+)", re.S)

# ТРЕБУЕМЫЙ ЗАПАС. Одна единица, а не две, как у объёма, и различие обосновано: у
# объёма запас тратится на ОДНОВРЕМЕННОСТЬ, поэтому перестановка кейсов способна
# съесть его молча. Здесь порядок ничего не меняет — списания складываются, — и
# запас нужен ровно затем, чтобы одиночное новое заведение под уже занятой личностью
# роняло гейт, а не стенд.
HEADROOM_REQUIRED = 1


class PremiseError(RuntimeError):
    """Предпосылка вердикта не выполнена — это ОТКАЗ, а не «чисто»."""


def decide(charged: int, ceiling: int) -> tuple[int, bool]:
    """Вердикт по паре «списаний, потолок» → (код возврата, находка ли).

    ЕДИНСТВЕННЫЙ производитель вердикта, и зовут его ОБА пути — прод и самопроверка.
    Второй кодек здесь запрещён: он расходится с первым молча и именно там, где
    расхождение не видно. Прецедент уже оплачен у соседа: проба, сравнивавшая
    литералы вместо вызова предиката, оставалась зелёной при правке порога.
    """
    finding = ceiling - charged < HEADROOM_REQUIRED
    return (1 if finding else 0), finding


def load_peak_gate(root: str):
    """Сосед целиком: его `_load` умеет грузить и объявление церемонии."""
    import importlib.util  # noqa: PLC0415 — нужен только здесь

    path = os.path.join(root, PEAK_GATE)
    if not os.path.exists(path):
        raise PremiseError(
            f"соседнего гейта нет в дереве: {path} — формы заведения, порядок волны "
            f"и базовый уровень брать не у кого, а своя копия разошлась бы молча")
    spec = importlib.util.spec_from_file_location("kacho_peak_gate", path)
    if spec is None or spec.loader is None:
        raise PremiseError(f"соседний гейт не загружается: {path}")
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    for want in ("load_declarations", "identities_of", "wave_collections",
                 "timeline_of", "read_base_components", "seeded_by_identity"):
        if not hasattr(mod, want):
            raise PremiseError(
                f"у соседнего гейта нет `{want}` — его форма изменилась, и вердикт "
                f"был бы о том, чего он больше не считает")
    return mod


def read_rate(root: str) -> tuple[int, int, str, int]:
    """Темп → (заведений, секунд окна, где найдено, файлов осмотрено).

    Осмотренное едет ВМЕСТЕ с величиной: «объявление не найдено» и «не прочитано
    ни одного файла» — разные исходы, и вызывающий обязан их различать.
    """
    d = os.path.join(root, RATE_MIGRATIONS_DIR)
    try:
        names = sorted(n for n in os.listdir(d) if n.endswith(".sql"))
    except OSError as exc:
        raise PremiseError(f"каталог миграций не прочитан: {exc}") from exc
    if not names:
        raise PremiseError(
            f"в {RATE_MIGRATIONS_DIR} ноль файлов .sql — обход пуст, и «темп не "
            f"объявлен» неотличимо от «читать было нечего»")

    found: list[tuple[str, int, int]] = []
    for n in names:
        try:
            text = open(os.path.join(d, n), encoding="utf-8").read()
        except OSError as exc:
            raise PremiseError(f"миграция {n} не прочитана: {exc}") from exc
        for m in RATE.finditer(text):
            found.append((n, int(m.group(1)), int(m.group(2))))

    if not found:
        raise PremiseError(
            f"величина темпа не найдена ни в одной из {len(names)} миграций "
            f"{RATE_MIGRATIONS_DIR} — форма объявления изменилась, и вердикт "
            f"был бы о выдуманном числе")
    if len({(a, b) for _, a, b in found}) > 1:
        where = ", ".join(f"{n}:{a}/{b}" for n, a, b in found)
        raise PremiseError(
            f"темп объявлен НЕОДНОЗНАЧНО ({where}) — вердикт зависел бы от "
            f"порядка обхода, а не от дерева")
    return found[0][1], found[0][2], found[0][0], len(names)


def audit(root: str):
    peak_gate = load_peak_gate(root)
    decl, forms = peak_gate.load_declarations(root)
    identities = peak_gate.identities_of(decl)
    ceiling, window, rate_at, rate_files = read_rate(root)
    common, seeded_total, why = peak_gate.read_base_components(root)
    seeded = peak_gate.seeded_by_identity(decl, seeded_total)
    base = common + max(seeded.values(), default=0)
    wave = peak_gate.wave_collections(root, decl)

    per_identity = []
    cases = steps = 0
    for name, bearers in sorted(identities.items()):
        events, cases, steps = peak_gate.timeline_of(wave, forms, bearers)
        created = sum(1 for sign, *_ in events if sign == "+")
        per_identity.append((name, created, common + seeded.get(name, 0) + created))

    total = sum(row[1] for row in per_identity)
    if total == 0:
        raise PremiseError(
            "заведений аккаунта НЕ НАЙДЕНО ни под одной объявленной личностью — "
            "предикат ослеп, чинить надо гейт, а не выходить успехом")

    worst = max(per_identity, key=lambda row: row[2])
    census = {
        "collections": len(wave), "cases": cases, "steps": steps,
        "created": total, "identities": per_identity,
        "order": [stem for stem, _ in wave], "window": window,
        "rate_at": rate_at, "rate_files": rate_files,
    }
    return worst, ceiling, base, why, census


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--root", default=REPO, help="корень монорепо")
    ap.add_argument("--self-test", action="store_true",
                    help="доказательство инъекцией в обе стороны")
    a = ap.parse_args(argv)
    if a.self_test:
        return self_test()

    try:
        worst, ceiling, base, why, census = audit(a.root)
    except PremiseError as exc:
        print(f"ОТКАЗ (предпосылка): {exc}", file=sys.stderr)
        return 2

    print(f"волна церемонии выведена из объявления церемонии (порядок исполнения):")
    print(f"  {' → '.join(census['order'])}")
    print(f"осмотрено: {census['collections']} коллекц(ий), {census['cases']} кейс(ов), "
          f"{census['steps']} шаг(ов); личност(ей) {len(census['identities'])}, "
          f"заведений аккаунта {census['created']}")
    print("базовый уровень " + str(base) + ": " + "; ".join(why))
    for name, created, charged in census["identities"]:
        print(f"  личность {name}: заведени(й) пробами {created}, списаний всего "
              f"{charged}, запас {ceiling - charged}")
    print(f"потолок темпа {ceiling} заведени(й) за {census['window']} с "
          f"({RATE_MIGRATIONS_DIR}/{census['rate_at']}, миграций осмотрено "
          f"{census['rate_files']}); наибольшее списание {worst[2]} (личность {worst[0]}), "
          f"наименьший запас {ceiling - worst[2]}")

    code, finding = decide(worst[2], ceiling)
    if finding:
        print(f"\nНАХОДКА: личность {worst[0]} списывает {worst[2]} допуск(ов) при "
              f"потолке {ceiling} — запас {ceiling - worst[2]}.", file=sys.stderr)
        print("  Заведения сверх потолка получат RESOURCE_EXHAUSTED, и падение",
              file=sys.stderr)
        print("  достанется не им, а шагам, идущим следом за несозданным аккаунтом.",
              file=sys.stderr)
        print("  Чинится ЛИЧНОСТЬЮ, а не потолком: заводящая проба приводит СВОЮ",
              file=sys.stderr)
        print("  (`ceremony_credentials.ADMISSION_SLOTS`) и заводит ровно один аккаунт.",
              file=sys.stderr)
        return code

    print(f"запас не меньше {HEADROOM_REQUIRED} у каждой личности: одиночное новое "
          f"заведение волну не роняет.")
    return code


# ─────────────────────────────────────────────────────────────────────────────
# Доказательство инъекцией — В ОБЕ СТОРОНЫ
# ─────────────────────────────────────────────────────────────────────────────
def _threshold_sites(source: str) -> list[str]:
    """Имена функций, где списание сравнивается с потолком. Судит РАЗБОР, а не поиск
    по образцу: слова `charged` и `ceiling` стоят в этом файле десятками, в том
    числе в прозе, и предикат по подстроке краснел бы на собственных объяснениях."""
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
        if {"charged", "ceiling"} <= names:
            sites.append(owner.get(id(node), "<уровень модуля>"))
    return sorted(sites)


def _step(bearer: str, capture: bool, name: str = "шаг"):
    """Синтетический шаг заведения: предъявитель + захват идентификатора либо без."""
    tests = ["const v = (j.metadata && j.metadata.accountId);",
             "  if (v !== undefined && v !== null) pm.environment.set('acc"
             + name + "', String(v));"] if capture else [
        "pm.test('status 200', () => {});"]
    return {"name": name, "request": {
        "method": "POST", "url": {"raw": "{{baseUrl}}/iam/v1/accounts"}},
        "event": [
            {"listen": "prerequest", "script": {"exec": [
                f"// per-step auth: bearer from env '{bearer}'"]}},
            {"listen": "test", "script": {"exec": tests}}]}


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
        nonlocal ok, asserts
        asserts += 1
        print(("  ok   " if passed else "  FAIL ") + label)
        if not passed:
            ok = False

    print("── предикат вердикта: ЗОВЁТСЯ тот же, что на прод-пути")
    # Утверждается ПАРА (код возврата, находка), а не пересчитанное здесь
    # неравенство: сравнение литералов проверяло бы работу оператора, а не гейт.
    for charged, ceiling, want in ((2, 3, (0, False)), (3, 3, (1, True)),
                                   (10, 3, (1, True)), (1, 3, (0, False)),
                                   (0, 1, (0, False)), (1, 1, (1, True))):
        check(f"списаний {charged} при потолке {ceiling} → {want}",
              decide(charged, ceiling), want)

    print("── сравнение списания с потолком в модуле РОВНО ОДНО, и живёт оно в decide")
    sites = _threshold_sites(open(os.path.abspath(__file__), encoding="utf-8").read())
    check("мест сравнения", sites, ["decide"])

    print("── величина темпа читается из миграции, а не выписана")
    rate, window, _at, _files = read_rate(REPO)
    note(rate > 0 and window > 0, f"потолок {rate} заведени(й) за {window} с")
    try:
        read_rate(os.path.join(REPO, "нет-такого"))
        note(False, "миграции темпа нет: прошло молча")
    except PremiseError:
        note(True, "миграции темпа нет: ОТКАЗ")

    print("── лента заведений строится из коллекции, а не из выдумки")
    try:
        peak_gate = load_peak_gate(REPO)
        _decl, forms = peak_gate.load_declarations(REPO)
    except PremiseError as exc:
        note(False, f"предпосылка самопроверки: {exc}")
        return 1

    # ДЕФЕКТ ВО ПЛОТИ: одна личность заводит три аккаунта — ровно та форма, из-за
    # которой прогон 32612214045 дал 83 упавших утверждения.
    hoarding = {"item": [{"name": "CASE — заголовок", "item": [
        _step("jwtHumanX", True, "a"), _step("jwtHumanX", True, "b"),
        _step("jwtHumanX", True, "c")]}]}
    events, _c, _s = peak_gate.timeline_of([("синт", hoarding)], forms, ("jwtHumanX",))
    charged_hoarding = 1 + sum(1 for sign, *_ in events if sign == "+")
    check("три заведения одной личностью при базовом 1 → списаний 4",
          charged_hoarding, 4)
    check("и это находка при потолке 3", decide(charged_hoarding, 3), (1, True))

    # ЗАКОННЫЙ БЛИЗНЕЦ ТОЙ ЖЕ ФОРМЫ: те же три заведения, но у каждого СВОЯ личность.
    # Без него «находка есть» зеленело бы на предикате, который краснеет всегда.
    spread = {"item": [{"name": "CASE — заголовок", "item": [
        _step("jwtHumanA", True, "a"), _step("jwtHumanB", True, "b"),
        _step("jwtHumanC", True, "c")]}]}
    for bearer in ("jwtHumanA", "jwtHumanB", "jwtHumanC"):
        ev, _c, _s = peak_gate.timeline_of([("синт", spread)], forms, (bearer,))
        charged = 1 + sum(1 for sign, *_ in ev if sign == "+")
        check(f"{bearer}: списаний 2 при потолке 3 → не находка",
              (charged, decide(charged, 3)), (2, (0, False)))

    # Заведение БЕЗ захвата идентификатора (занятое имя) допуска не списывает:
    # уникальность отменяет транзакцию раньше отложенного триггера.
    dup = {"item": [{"name": "CASE — заголовок", "item": [
        _step("jwtHumanX", True, "a"), _step("jwtHumanX", False, "dup")]}]}
    ev, _c, _s = peak_gate.timeline_of([("синт", dup)], forms, ("jwtHumanX",))
    check("заведение без захвата не считается", sum(1 for sign, *_ in ev if sign == "+"), 1)

    print("── предпосылки: пустое и нечитаемое суть ОТКАЗ, а не «чисто»")
    try:
        load_peak_gate(os.path.join(REPO, "нет-такого"))
        note(False, "соседнего гейта нет: прошло молча")
    except PremiseError:
        note(True, "соседнего гейта нет: ОТКАЗ")

    print("── настоящее дерево читается, перепись непуста")
    try:
        worst, ceiling, base, _why, census = audit(REPO)
        for label, got in (("коллекций", census["collections"]),
                           ("шагов", census["steps"]),
                           ("заведений", census["created"]),
                           ("личностей", len(census["identities"])),
                           ("потолок темпа", ceiling),
                           ("базовый уровень", base)):
            note(got > 0, f"{label} > 0 ({got})" if got > 0
                 else f"{label} == 0 — предикат ослеп")
        note(ceiling - worst[2] >= HEADROOM_REQUIRED,
             f"наименьший запас {ceiling - worst[2]} (личность {worst[0]})")
    except PremiseError as exc:
        note(False, f"настоящее дерево: {exc}")

    print()
    print(f"утверждений исполнено: {asserts}")
    print("PASS: темп заведения аккаунтов" if ok else "FAIL: темп заведения аккаунтов")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
