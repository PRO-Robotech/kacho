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
непрочитанный потолок темпа, невыведенный базовый уровень и ноль заведений под
всеми личностями сразу — ОТКАЗ, а не «чисто».

ПУСТАЯ ВОЛНА — ИСХОД РАЗРЕЗА, И ОН ЗАПИСАН У СОСЕДА. Линия выноса службы доступа
унесла суиту волны и затравку темпа; отказ стал вечным и называл не ту причину —
«каталога миграций iam нет», хотя мерить было нечего ещё до темпа. Состояния
волны различает сосед (`wave_presence`), здесь они только читаются. Состояний ТРИ,
и беспредметно ровно ОДНО: каталога суиты нет вовсе — БЕСПРЕДМЕТНО с числами обхода
и кодом ноль; каталог ЕСТЬ, а коллекций волны в нём нет — ОТКАЗ; обход прочитал ноль
коллекций — ОТКАЗ. Два отказных состояния приходили сюда чужим классом и уходили
трассой; теперь их переводит граница. Текст исхода печатает сосед, единственный его
производитель.

ПОРЯДОК НЕСУЩИЙ и доказан инъекцией в обе стороны: сперва ВОЛНА (предмет), потом
ТЕМП (мерка). Между двумя прогонами фикстуры меняется ровно один факт — лежит ли
в суите коллекция под личностью церемонии, — и мерки нет в обоих: без волны
исход беспредметен, с волной — отказ по мерке.

ОТДЕЛЬНО: ЧУЖОЙ ОТКАЗ — НАШ ОТКАЗ, И ЭТО ДЕРЖИТСЯ ГРАНИЦЕЙ, А НЕ ПЕРЕХВАТАМИ.

Здесь стояло «самопроверка больше не падает трассой стека», и это было верно для
самопроверки и НЕВЕРНО для прод-пути того же гейта — то есть заявление шире
сделанного. Перевод чужого отказа стоял на ДВУХ вызовах из пятнадцати; на
остальных чужой `PremiseError` уходил наружу непойманным, потому что у соседа СВОЙ
одноимённый класс, а `except PremiseError` в `main()` ловит здешний. Читатель
получал стек с кодом 1 вместо объявленного кода 2 с названным отказом — находку,
называющую симптом вместо причины.

Замер разбором на ревизии находки: вызовов к соседу 15, чужой отказ уходил
непереведённым с ВОСЬМИ — пять на прод-пути и три в самопроверке. Приёмка назвала
один; остальные семь молчали тем же способом.

Починено НЕ восемью перехватами: восемь закрылись бы, а девятый вызов забыл бы
перевод так же молча. Сосед достаётся через ФАСАД (`_Peer`), который переводит
чужой исход и чужой отказ в наши на КАЖДОМ вызове; сырой модуль границу не
покидает, поэтому переводить не нужно помнить. Утверждается это тремя условиями
границы плюс поведенческой пробой на поддельном соседе — по каждому имени
требуемого набора и по каждому чужому классу.

Перевод УТОЧНЯЕТ предмет, а не ослабляет утверждение: отсутствие величины законно
РОВНО тогда, когда и волны в дереве нет, — есть волна, а величины нет — провал.

ДВА ИСХОДА ОТДАЮТ КОД НОЛЬ, И РАЗЛИЧАЕТ ИХ ЗНАК ПЕРВОЙ СТРОКИ. «Проверено, запаса
хватает» печатает `PASS_MARK`, «мерить нечего» — `VOID_MARK` соседа, находка — ни
одного. Свой код возврата для беспредметности рассмотрен и отвергнут: условие
создать НЕЛЬЗЯ (суита в другом дереве), поэтому ненулевой код дал бы вечное
красное, а вечное красное снимают вместе с гейтом. Чего это не закрывает, сказано
прямо: сегодняшний вызывающий читает код возврата, а не знак, и для него оба
исхода по-прежнему один; эта половина держится вниманием.

Использование:
    python3 deploy/scripts/assert-identity-admission-rate-headroom.py [--root .]
    python3 deploy/scripts/assert-identity-admission-rate-headroom.py --self-test
"""
from __future__ import annotations

import argparse
import ast
import functools
import os
import sys
import types

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))

# Сосед — ЕДИНСТВЕННЫЙ источник форм, порядка волны и базового уровня. Импортируется,
# а не переписывается: переписанная копия разошлась бы с ним молча.
PEAK_GATE = "deploy/scripts/assert-identity-account-peak-under-ceiling.py"

# ТРЕБУЕМЫЙ НАБОР СОСЕДА. Объявлением, а не литералом внутри загрузчика: по этому
# же перечню самопроверка доказывает, что перевод чужого отказа ТОТАЛЕН — иначе
# доказательство перечисляло бы свой список имён и разошлось бы с проверкой формы
# молча, ровно там, где оба отвечают «валидно».
REQUIRED_PEER_API = (
    "load_declarations", "identities_of", "wave_collections", "wave_presence",
    "timeline_of", "read_base_components", "seeded_by_identity",
    "read_seeded_value", "SubjectElsewhere", "PremiseError",
    "report_subject_elsewhere",
)

# Величина темпа — там, где она НАЗНАЧАЕТСЯ, а не там, где её ожидают тексты отказов.
#
# АДРЕСУЕТСЯ ПО ТОМУ, ЧТО ДЕРЕВО ПРОИЗВОДИТ (2026-09-04). Здесь стояло имя одной
# миграции и её форма записи — `INSERT … (kind, max_events, window_seconds)
# SELECT 'iam.account', N, M`. Свод миграций iam отнял оба: файла с таким именем
# нет, а уцелевшая затравка записана `VALUES` с другим составом колонок. Величина
# при этом ЖИВА — исчез её адрес, а не предмет.
#
# РАЗБОРЩИК БЕРЁТСЯ У СОСЕДА, а не переписывается: он уже умеет обходить весь
# каталог миграций и читать значения ПО ИМЕНИ КОЛОНКИ, а вторая копия того же
# разбора разошлась бы с первой молча — и разошлась бы именно там, где обе
# отвечают «валидно» на валидном входе. Ровно тем же доводом сосед объявлен
# единственным источником форм заведения и порядка волны.
RATE_TABLE = "kaname.account_admission_rate_limits"
RATE_WHERE = {"kind": "iam.account", "withdrawn_at": "NULL"}

# ТРЕБУЕМЫЙ ЗАПАС. Одна единица, а не две, как у объёма, и различие обосновано: у
# объёма запас тратится на ОДНОВРЕМЕННОСТЬ, поэтому перестановка кейсов способна
# съесть его молча. Здесь порядок ничего не меняет — списания складываются, — и
# запас нужен ровно затем, чтобы одиночное новое заведение под уже занятой личностью
# роняло гейт, а не стенд.
HEADROOM_REQUIRED = 1

# ЗНАК СУЩЕСТВЕННОГО ИСХОДА. Беспредметный знак печатает сосед — единственный его
# производитель (`VOID_MARK`), — а этот принадлежит здешнему вердикту. Требуется от
# пары не совпадение, а РАЗЛИЧИЕ: оба исхода отдают КОД НОЛЬ, и вызывающему нужен
# машинный различитель, а не проза первой строки. Различие утверждает самопроверка,
# снимая вывод обоих путей; чего это не закрывает — сказано у соседа при `VOID_MARK`.
PASS_MARK = "ПРОВЕРЕНО"


class PremiseError(RuntimeError):
    """Предпосылка вердикта не выполнена — это ОТКАЗ, а не «чисто»."""


class SubjectElsewhere(RuntimeError):
    """Волны церемонии в ЭТОМ дереве нет — предмет уехал вместе со службой доступа.

    СВОЙ КЛАСС, А НЕ КЛАСС СОСЕДА, и это не дублирование: сосед загружается
    динамически, поэтому его класс недоступен в `except` до загрузки. Перевод живёт
    в ОДНОМ месте — на границе (`_Peer`), — а не на выбранных вызовах: именно
    выборочный перевод и дал трассу стека на прод-пути.

    ТЕКСТ ИСХОДА НЕ КОПИРУЕТСЯ: печатает его сосед, единственный производитель, а
    исключение несёт только перепись и ссылку на печать. Вторая копия текста
    разошлась бы с первой молча — как разошлись бы два кодека одного вердикта.
    """

    def __init__(self, census: dict, reporter):
        super().__init__("волна церемонии в этом дереве не производится")
        self.census = census
        self._reporter = reporter

    def report(self, what: str) -> int:
        return self._reporter(self.census, what)


class _Peer:
    """Сосед, у которого КАЖДЫЙ вызов переводит чужой отказ в наш.

    ПОЧЕМУ ОДНО МЕСТО, А НЕ ПЕРЕХВАТ НА КАЖДОМ ВЫЗОВЕ. Перевод стоял ровно на
    двух вызовах из пятнадцати, и это не было видно ниоткуда: у соседа СВОИ
    одноимённые классы, поэтому `except PremiseError` в `main()` ловит наш и
    чужой НЕ ловит — чужой уходит трассой стека с кодом 1 вместо объявленного 2.
    Замер разбором на ревизии находки: вызовов к соседу 15, чужой отказ уходил
    непереведённым с ВОСЬМИ — пять на прод-пути (`load_declarations`,
    `identities_of`, `wave_collections`, `read_base_components`,
    `seeded_by_identity`) и три в самопроверке. Приёмка назвала один из восьми;
    остальные семь молчали тем же способом.

    Перехват на каждом вызове закрыл бы восемь и не закрыл девятый: следующий
    вызов к соседу забудет перевод — забыть его нельзя только тогда, когда
    переводить не нужно помнить. Здесь перевод ТОТАЛЕН by construction, и
    доказан он поведенчески — поддельным соседом, поднимающим чужой отказ из
    КАЖДОГО имени требуемого набора.

    ПЕРЕВОДЯТСЯ ФУНКЦИИ, А НЕ ВСЁ. Классы и константы отдаются как есть: обёртка
    вокруг класса сломала бы `except peer.SubjectElsewhere` у всякого, кто ещё
    захочет ловить чужой класс прямо, а константы вызывать не нужно.
    """

    def __init__(self, mod):
        self.__dict__["_mod"] = mod
        self.__dict__["_calls"] = {}

    def __getattr__(self, name):
        mod, cache = self.__dict__["_mod"], self.__dict__["_calls"]
        if name in cache:
            return cache[name]
        obj = getattr(mod, name)
        if not isinstance(obj, types.FunctionType):
            return obj
        cache[name] = _translating(obj, mod)
        return cache[name]


def _translating(fn, mod):
    """Обёртка одного вызова: чужой исход → наш исход, чужой отказ → наш отказ.

    Порядок ветвей НЕСУЩИЙ: `SubjectElsewhere` идёт первой. Будь она подклассом
    `PremiseError` у соседа — перестановка превратила бы беспредметность в отказ,
    то есть вечное красное там, где условие создать нельзя.
    """
    @functools.wraps(fn)
    def call(*a, **kw):
        try:
            return fn(*a, **kw)
        except mod.SubjectElsewhere as exc:          # чужой исход — наш исход
            raise SubjectElsewhere(
                exc.census, mod.report_subject_elsewhere) from exc
        except mod.PremiseError as exc:              # чужой отказ — наш отказ
            raise PremiseError(str(exc)) from exc
    return call


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
    for want in REQUIRED_PEER_API:
        if not hasattr(mod, want):
            raise PremiseError(
                f"у соседнего гейта нет `{want}` — его форма изменилась, и вердикт "
                f"был бы о том, чего он больше не считает")
    # ФАСАД, А НЕ СЫРОЙ МОДУЛЬ: сырой отдавал чужие классы отказа наружу, и
    # `except PremiseError` у вызывающего их не ловил — см. `_Peer`.
    return _Peer(mod)


def read_rate(root: str) -> tuple[int, int]:
    """Пара «сколько заведений и за сколько секунд» из затравки миграций iam.

    Читает разборщиком СОСЕДА — того же, что читает потолок объёма. Своей копии
    здесь нет намеренно: два разбора одной формы расходятся молча.
    """
    return read_rate_with_census(root)[:2]


def read_rate_with_census(root: str) -> tuple[int, int, dict[str, int]]:
    peak = load_peak_gate(root)
    # Перехвата здесь НЕТ намеренно: чужой отказ переводит фасад (`_Peer`), один
    # раз и для всех вызовов. Перехват на месте был бы девятой копией идиомы —
    # той самой, которую забыли на восьми других вызовах.
    (events, window), census = peak.read_seeded_value(
        root, RATE_TABLE, RATE_WHERE, ("max_events", "window_seconds"),
        "потолок темпа")
    if not (events.isdigit() and window.isdigit()):
        raise PremiseError(
            f"темп прочитан как ({events!r}, {window!r}) — не числа, вердикт был "
            f"бы о выдуманной величине")
    return int(events), int(window), census


def audit(root: str):
    peak_gate = load_peak_gate(root)
    decl, forms = peak_gate.load_declarations(root)
    identities = peak_gate.identities_of(decl)
    # ПОРЯДОК НЕСУЩИЙ, и он тот же, что у соседа: сперва ВОЛНА (предмет), потом
    # темп (мерка). Мерка, спрошенная раньше предмета, отказывает по своей
    # недоступности там, где мерить нечего, — и называет читателю не ту причину.
    # Ровно это и происходило после разреза службы доступа: отказ говорил
    # «каталога миграций iam нет», хотя волны в дереве не было вовсе.
    wave = peak_gate.wave_collections(root, decl)
    ceiling, window, mig_census = read_rate_with_census(root)
    common, seeded_total, why = peak_gate.read_base_components(root)
    seeded = peak_gate.seeded_by_identity(decl, seeded_total)
    base = common + max(seeded.values(), default=0)

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
        # Перепись ЧТЕНИЯ ВЕЛИЧИНЫ — две числа по каждой оси, и печатается она на
        # ЗЕЛЁНОМ пути тоже: «ноль найденных» обязано быть отличимо от «ноль
        # прочитанных» до поломки, а не после неё.
        "migrations": mig_census,
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
    except SubjectElsewhere as exc:
        return exc.report("темп заведения аккаунтов")
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
    mc = census["migrations"]
    print(f"величина темпа: осмотрено файлов миграций {mc['files']}, "
          f"операторов затравки {mc['inserts']}, из них строк {RATE_TABLE} "
          f"{mc['rows']}, подошло под условие {mc['matched']}")
    print(f"потолок темпа {ceiling} заведени(й) за {census['window']} с "
          f"({RATE_TABLE}); наибольшее списание {worst[2]} (личность {worst[0]}), "
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

    print(f"{PASS_MARK}: запас не меньше {HEADROOM_REQUIRED} у каждой личности — "
          f"одиночное новое заведение волну не роняет.")
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


def _untranslated_peer_handles(source: str) -> tuple[list[str], dict[str, int]]:
    """→ (места, где сосед достаётся вызывающему СЫРЫМ, перепись осмотренного).

    СВОЙСТВО, КОТОРОЕ УТВЕРЖДАЕТСЯ, — не «каждый вызов переводит», а «переводить
    НЕ НУЖНО ПОМНИТЬ»: сырой модуль соседа не покидает границу (`load_peak_gate`),
    поэтому чужой класс отказа до вызывающего добраться НЕ МОЖЕТ. Перехват на
    каждом вызове закрыл бы восемь известных мест и не закрыл девятое; здесь
    девятого не бывает by construction.

    ПОЧЕМУ НЕ «ПЕРЕЧИСЛИТЬ ВЫЗОВЫ И ПРОВЕРИТЬ ПЕРЕХВАТ». Такой предикат судил бы
    СНЯТУЮ конструкцию: после фасада чужой класс на вызовы не приходит вовсе, и
    предикат требовал бы перехвата того, чего там нет, — то есть краснел бы на
    исправном коде. Первая его редакция именно это и сделала, и красное было
    верным сигналом о предикате, а не о дереве.

    Три условия, и все три судят РАЗБОР, а не поиск по образцу: имена `_Peer`,
    `_mod` и `module_from_spec` стоят в этом файле и в прозе про сам этот класс.

      1. каждый возврат `load_peak_gate` отдаёт `_Peer(...)` — иначе наружу уедет
         сырой модуль;
      2. модуль из спеки собирается ТОЛЬКО внутри `load_peak_gate` — второй
         загрузчик обошёл бы границу;
      3. внутреннее поле фасада (`_mod`) не читает никто, кроме самого фасада, —
         иначе сырой модуль достают в обход.
    """
    tree = ast.parse(source)
    funcs = {n.name: n for n in tree.body if isinstance(n, ast.FunctionDef)}
    classes = {n.name: n for n in tree.body if isinstance(n, ast.ClassDef)}
    bad: list[str] = []
    census = {"returns": 0, "loaders": 0, "mod_reads": 0}

    loader = funcs.get("load_peak_gate")
    if loader is None:
        return ["нет `load_peak_gate` — границы перевода не существует"], census
    for n in ast.walk(loader):
        if not isinstance(n, ast.Return) or n.value is None:
            continue
        census["returns"] += 1
        v = n.value
        ok = (isinstance(v, ast.Call) and isinstance(v.func, ast.Name)
              and v.func.id == "_Peer")
        if not ok:
            bad.append(f"load_peak_gate:{n.lineno} отдаёт не `_Peer(...)` — "
                       f"наружу уедет сырой модуль соседа")

    inside = {id(n) for n in ast.walk(loader)}
    for fn_name, fn in list(funcs.items()) + [(f"{c}.<тело>", v)
                                              for c, v in classes.items()]:
        for n in ast.walk(fn):
            if (isinstance(n, ast.Call) and isinstance(n.func, ast.Attribute)
                    and n.func.attr == "module_from_spec"):
                census["loaders"] += 1
                if id(n) not in inside:
                    bad.append(f"{fn_name}:{n.lineno} собирает модуль соседа в обход "
                               f"границы перевода")

    peer_body = classes.get("_Peer")
    peer_ids = {id(n) for n in ast.walk(peer_body)} if peer_body else set()
    for n in ast.walk(tree):
        read = None
        if isinstance(n, ast.Attribute) and n.attr == "_mod":
            read = n
        elif (isinstance(n, ast.Subscript) and isinstance(n.slice, ast.Constant)
              and n.slice.value == "_mod"):
            read = n
        if read is None:
            continue
        census["mod_reads"] += 1
        if id(read) not in peer_ids:
            bad.append(f"строка {read.lineno}: сырой модуль достают через `_mod` "
                       f"в обход фасада")
    return bad, census


def _fake_peer(names, raising: str):
    """Поддельный сосед: КАЖДОЕ имя набора поднимает чужой отказ `raising`.

    ПОДДЕЛКА СТРУКТУРНО НЕ СПОСОБНА ДАТЬ ЗЕЛЁНОЕ по своему предмету: ни одна её
    функция не возвращает значения — она только поднимает. Провязка доказывается
    тем, что перевод СРАБОТАЛ на каждом имени, а не тем, что подделка ответила
    «хорошо».
    """
    import types as _t  # noqa: PLC0415 — нужен только здесь

    mod = _t.ModuleType("kacho_fake_peer")

    class FakePremise(RuntimeError):
        pass

    class FakeElsewhere(RuntimeError):
        def __init__(self, census):
            super().__init__("предмет вне дерева")
            self.census = census

    mod.PremiseError = FakePremise
    mod.SubjectElsewhere = FakeElsewhere
    mod.report_subject_elsewhere = lambda census, what="?": 0
    exc = (FakePremise("чужой отказ подделки") if raising == "PremiseError"
           else FakeElsewhere({"files_read": 1, "stems": 0, "suite_present": False,
                               "suite": "синт"}))

    def make(_name):
        def fn(*_a, **_kw):
            raise exc
        return fn

    for name in names:
        if name in ("PremiseError", "SubjectElsewhere", "report_subject_elsewhere"):
            continue
        setattr(mod, name, make(name))
    return mod


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
    _mine = open(os.path.abspath(__file__), encoding="utf-8").read()
    sites = _threshold_sites(_mine)
    check("мест сравнения", sites, ["decide"])

    # ═════════════════════════════════════════════════════════════════════════
    # ЧУЖОЙ ОТКАЗ — НАШ ОТКАЗ: перевод ТОТАЛЕН, а не на выбранных вызовах
    # ═════════════════════════════════════════════════════════════════════════
    # ЗДЕСЬ БЫЛА ТРАССА СТЕКА НА ПРОД-ПУТИ. Перевод стоял на двух вызовах из
    # пятнадцати; на остальных чужой `PremiseError` уходил непойманным, потому что
    # `except PremiseError` в `main()` ловит ЗДЕШНИЙ одноимённый класс. Читатель
    # получал стек с кодом 1 вместо объявленного кода 2 с названным отказом.
    #
    # Утверждается СВОЙСТВО, а не починенное место: непереведённых вызовов НОЛЬ.
    print("── граница перевода: сырой модуль соседа её НЕ ПОКИДАЕТ")
    _bad, _cen = _untranslated_peer_handles(_mine)
    note(_cen["returns"] > 0 and _cen["loaders"] > 0,
         f"осмотрено: возвратов границы {_cen['returns']}, сборок модуля "
         f"{_cen['loaders']}, чтений внутреннего поля {_cen['mod_reads']}"
         + ("" if _cen["returns"] and _cen["loaders"]
            else " — разбор ослеп, чинить надо предикат"))
    check("мест, где сосед уходит сырым", _bad, [])

    # ПРЕДИКАТ ОБЯЗАН УМЕТЬ КРАСНЕТЬ, и инъекция меняет РОВНО ОДИН факт против
    # законного близнеца — что именно отдаёт граница. Без этой пары «ноль мест»
    # было бы неотличимо от «разбор не видит ни одной формы».
    _twin = ("def load_peak_gate(root):\n"
             "    spec = importlib.util.spec_from_file_location('x', root)\n"
             "    mod = importlib.util.module_from_spec(spec)\n"
             "    return _Peer(mod)\n"
             "class _Peer:\n"
             "    def __init__(self, mod):\n"
             "        self.__dict__['_mod'] = mod\n")
    _hurt = _twin.replace("    return _Peer(mod)", "    return mod")
    _g_twin, _ = _untranslated_peer_handles(_twin)
    _g_hurt, _ = _untranslated_peer_handles(_hurt)
    note(_g_twin == [], f"законный близнец МОЛЧИТ: {_g_twin}")
    note(len(_g_hurt) == 1 and "сырой модуль" in _g_hurt[0],
         f"граница отдала сырой модуль → НАХОДКА: {_g_hurt or 'НЕ НАЙДЕНА'}")
    # Вторая ось того же свойства: загрузчик В ОБХОД границы.
    _g_2nd, _ = _untranslated_peer_handles(
        _twin + "def other(root):\n"
                "    return importlib.util.module_from_spec(root)\n")
    note(len(_g_2nd) == 1 and "в обход" in _g_2nd[0],
         f"второй загрузчик в обход → НАХОДКА: {_g_2nd or 'НЕ НАЙДЕНА'}")
    # Третья ось: сырой модуль достают через внутреннее поле фасада.
    _g_3rd, _ = _untranslated_peer_handles(
        _twin + "def other(p):\n    return p.__dict__['_mod']\n")
    note(len(_g_3rd) == 1 and "в обход фасада" in _g_3rd[0],
         f"чтение `_mod` снаружи → НАХОДКА: {_g_3rd or 'НЕ НАЙДЕНА'}")

    # ПОВЕДЕНЧЕСКОЕ доказательство тотальности: подделка поднимает чужой отказ из
    # КАЖДОГО имени требуемого набора, и каждое обязано выйти НАШИМ классом.
    # Перечень имён — тот же `REQUIRED_PEER_API`, что сверяет форму соседа: свой
    # список здесь разошёлся бы с проверкой формы молча.
    print("── перевод доказан поведением: каждое имя набора, оба чужих класса")
    _api = [n for n in REQUIRED_PEER_API
            if n not in ("PremiseError", "SubjectElsewhere",
                         "report_subject_elsewhere")]
    note(len(_api) > 0, f"имён набора под проверкой {len(_api)}")
    for _foreign, _ours, _label in ((["PremiseError"], PremiseError, "отказ"),
                                    (["SubjectElsewhere"], SubjectElsewhere,
                                     "беспредметность")):
        _peer = _Peer(_fake_peer(REQUIRED_PEER_API, _foreign[0]))
        _bad = []
        for _n in _api:
            try:
                getattr(_peer, _n)()
            except _ours:
                continue
            except BaseException as _exc:       # noqa: BLE001 — предмет пробы
                _bad.append(f"{_n}→{type(_exc).__name__}")
        check(f"чужой {_label} переведён на всех {len(_api)} именах", _bad, [])

    # Классы и константы соседа фасад отдаёт КАК ЕСТЬ: обёртка вокруг класса
    # сломала бы `except peer.SubjectElsewhere` у всякого, кто ловит чужой прямо.
    _peer = _Peer(_fake_peer(REQUIRED_PEER_API, "PremiseError"))
    note(isinstance(_peer.PremiseError, type)
         and isinstance(_peer.SubjectElsewhere, type),
         "классы соседа отдаются классами, а не обёртками")

    # ЗДЕСЬ БЫЛА ТРАССА СТЕКА. Вызов стоял без перехвата, и после разреза службы
    # доступа самопроверка падала НЕПЕРЕХВАЧЕННЫМ `PremiseError`: вместо названного
    # отказа читатель получал стек, а объявленный код возврата 2 не производился
    # вовсе (питон отдаёт 1 на необработанном исключении). Находка, называющая
    # симптом вместо причины, посылает искать не там — и тратит на это прогон.
    #
    # Перехват НЕ ослабляет утверждение, а уточняет его предмет: величина темпа
    # живёт в затравке службы, и её отсутствие законно РОВНО ТОГДА, когда и волны
    # в этом дереве нет. Есть волна, а мерки нет — ПРОВАЛ, и это та сторона, без
    # которой перехват стал бы маской.
    print("── величина темпа: читается из миграции либо предмета в дереве нет")
    try:
        _pg = load_peak_gate(REPO)
        _decl, _f = _pg.load_declarations(REPO)
    except PremiseError as exc:
        note(False, f"предпосылка самопроверки: {exc}")
        return 1
    try:
        rate, window = read_rate(REPO)
        note(rate > 0 and window > 0, f"потолок {rate} заведени(й) за {window} с")
    except PremiseError as exc:
        try:
            _pg.wave_presence(REPO, _decl)
            note(False, f"волна в дереве ЕСТЬ, а величина темпа не читается: {exc}")
        except SubjectElsewhere as sub:          # НАШ класс: перевёл фасад
            note(True, f"величины темпа в дереве нет — и волны тоже: предмет вне "
                       f"дерева (коллекций прочитано {sub.census['files_read']}, "
                       f"волны {sub.census['stems']})")
        except PremiseError as inner:
            note(False, f"величины темпа нет, и состояние волны неясно: {inner}")
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

    # ЧТЕНИЕ ВЕЛИЧИНЫ ТЕМПА — СВОЙ случай, а не следствие предыдущего. Пока сосед
    # брался из того же корня, «корня нет» роняло загрузку соседа РАНЬШЕ чтения, и
    # отказ читателя величины не был доказан ничем: одно утверждение закрывало два
    # разных предмета и об одном из них молчало. Ниже сосед НА МЕСТЕ, а меняется
    # ровно один факт — затравка темпа.
    import shutil, tempfile  # noqa: PLC0415 — нужны только здесь

    _RATE_ROW = ("INSERT INTO kaname.account_admission_rate_limits "
                 "(id, kind, max_events, window_seconds, withdrawn_at, created_at) "
                 "VALUES (1, 'iam.account', 3, 3600, NULL, now());\n")

    def _root_with(body: str) -> str:
        d = tempfile.mkdtemp(prefix="kacho-rate-")
        os.makedirs(os.path.join(d, os.path.dirname(PEAK_GATE)), exist_ok=True)
        shutil.copy(os.path.join(REPO, PEAK_GATE), os.path.join(d, PEAK_GATE))
        mig = os.path.join(d, "services/iam/internal/migrations")
        os.makedirs(mig, exist_ok=True)
        with open(os.path.join(mig, "0001_initial.sql"), "w", encoding="utf-8") as fh:
            fh.write(body)
        return d

    # Законный близнец: сосед на месте, затравка на месте — величина читается.
    try:
        note(read_rate(_root_with(_RATE_ROW)) == (3, 3600),
             "сосед и затравка на месте → (3, 3600)")
    except PremiseError as exc:
        note(False, f"законный близнец: {exc}")

    # Та же ось, что сломала гейт: колонки переставлены — величина обязана уцелеть.
    _shuffled = ("INSERT INTO kaname.account_admission_rate_limits "
                 "(window_seconds, kind, created_at, max_events, id, withdrawn_at) "
                 "VALUES (3600, 'iam.account', now(), 3, 1, NULL);\n")
    try:
        note(read_rate(_root_with(_shuffled)) == (3, 3600),
             "колонки переставлены → всё ещё (3, 3600)")
    except PremiseError as exc:
        note(False, f"перестановка колонок: {exc}")

    # Отказы: сосед на месте, изменён РОВНО ОДИН факт затравки.
    for label, body in (
        ("затравки темпа нет вовсе", "-- миграция без затравки темпа\n"),
        ("вид сменён", _RATE_ROW.replace("'iam.account'", "'iam.project'")),
        ("затравка отозвана", _RATE_ROW.replace("NULL, now()", "now(), now()")),
    ):
        try:
            got = read_rate(_root_with(body))
            note(False, f"{label}: прошло молча (вернул {got})")
        except PremiseError:
            note(True, f"{label}: ОТКАЗ")

    print("── настоящее дерево: либо перепись непуста, либо предмет НАЗВАН вне его")
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
    except SubjectElsewhere as exc:
        # Беспредметность законна ТОЛЬКО с непустой переписью: пустой обход
        # означал бы «не смогли прочитать», а это другое состояние.
        note(exc.census["files_read"] > 0 and exc.census["stems"] == 0
             and not exc.census["suite_present"],
             f"волна вне дерева НАЗВАНА: прочитано коллекций "
             f"{exc.census['files_read']}, каталог суиты отсутствует, волны 0")
    except PremiseError as exc:
        note(False, f"настоящее дерево: {exc}")

    print("── ПОРЯДОК: темп спрашивается ПОСЛЕ волны, а не вместо неё")
    # Фикстура — СОСЕДА, единственный её производитель: порядок есть свойство обоих
    # гейтов полосы, и вторая копия фикстуры разошлась бы с первой молча. Между
    # прогонами меняется РОВНО ОДИН факт — есть ли в суите коллекция под личностью
    # церемонии.
    import shutil as _shutil  # noqa: PLC0415 — нужен только здесь

    _r = _pg.synthetic_wave_root(with_wave=False, with_seed=False)
    try:
        audit(_r)
        note(False, "волны нет, темпа нет: прошло молча")
    except SubjectElsewhere:
        note(True, "волны нет, темпа нет → БЕСПРЕДМЕТНО (темп не спрашивали)")
    except PremiseError as exc:
        note(False, f"волны нет — а отказ по темпу: {exc}")
    _shutil.rmtree(_r, ignore_errors=True)

    _r = _pg.synthetic_wave_root(with_wave=True, with_seed=False)
    try:
        audit(_r)
        note(False, "волна ЕСТЬ, темпа нет: прошло молча")
    except SubjectElsewhere:
        note(False, "волна ЕСТЬ, а исход объявлен беспредметным — темп перестали "
                    "спрашивать при живом предмете")
    except PremiseError as exc:
        note("темп" in str(exc) or "миграц" in str(exc),
             "волна ЕСТЬ, темпа нет → ОТКАЗ по темпу")
    _shutil.rmtree(_r, ignore_errors=True)

    # ═════════════════════════════════════════════════════════════════════════
    # ПУСТАЯ ВОЛНА: ТРИ СОСТОЯНИЯ, И ДВА ИЗ НИХ ПРИХОДИЛИ ТРАССОЙ СТЕКА
    # ═════════════════════════════════════════════════════════════════════════
    # Беспредметность (каталога суиты нет вовсе) переводилась и раньше — её ловил
    # перехват на одном вызове. Два ОТКАЗНЫХ состояния той же функции соседа
    # перехвата не имели: чужой `PremiseError` уходил наружу, `except PremiseError`
    # в `main()` ловил ЗДЕШНИЙ одноимённый класс, и читатель получал стек с кодом 1
    # вместо объявленного кода 2 с названным отказом.
    #
    # Инъекция ОДНО-ФАКТНАЯ: базой служит тот же корень `with_wave=False`, что дал
    # беспредметность строкой выше, и меняется ровно один факт дерева — наличие
    # каталога суиты. Текст отказа сверяется по предмету, а не по совпадению слов:
    # «каталог ЕСТЬ, а коллекций волны нет» — это не «предмет в другом дереве».
    print("── пустая волна: отказные состояния приходят ОТКАЗОМ, а не трассой")
    _r = _pg.synthetic_wave_root(with_wave=False, with_seed=True,
                                 seed_body=_RATE_ROW)
    os.makedirs(os.path.join(_r, _pg.SUITE, "collections"), exist_ok=True)
    try:
        audit(_r)
        note(False, "каталог суиты ЕСТЬ, волны нет: прошло молча")
    except SubjectElsewhere:
        note(False, "каталог суиты ЕСТЬ, а исход объявлен беспредметным — "
                    "отказ подменён беспредметностью")
    except PremiseError as exc:
        note("каталог суиты" in str(exc) and "коллекций волны в нём нет" in str(exc),
             f"каталог суиты ЕСТЬ, волны нет → ОТКАЗ по предмету: {exc}")
    _shutil.rmtree(_r, ignore_errors=True)

    # Второе отказное состояние той же функции: обход прочитал НОЛЬ коллекций.
    # Один факт против базы — снята коллекция соседней суиты, которую фикстура
    # кладёт всегда именно затем, чтобы обход не был пуст.
    _r = _pg.synthetic_wave_root(with_wave=False, with_seed=True,
                                 seed_body=_RATE_ROW)
    _shutil.rmtree(os.path.join(_r, "services", "zz"), ignore_errors=True)
    try:
        audit(_r)
        note(False, "прочитано ноль коллекций: прошло молча")
    except SubjectElsewhere:
        note(False, "прочитано ноль коллекций, а исход беспредметен — «ноль "
                    "найденных» стало неотличимо от «ноль прочитанного»")
    except PremiseError as exc:
        note("НИ ОДНОЙ" in str(exc) or "ноль" in str(exc).lower(),
             f"прочитано ноль коллекций → ОТКАЗ: {exc}")
    _shutil.rmtree(_r, ignore_errors=True)

    # ИЗВЕСТНАЯ ДЫРА ФАСАДА, ЗАКРЫТАЯ УТВЕРЖДЕНИЕМ: перевод накрывает ФУНКЦИИ.
    # Объяви сосед любое имя набора вызываемым объектом другого рода (частичное
    # применение, экземпляр с `__call__`), и оно уехало бы мимо перевода — молча.
    print("── набор соседа: каждое имя либо функция (переводится), либо класс")
    _mod_real = load_peak_gate(REPO)
    _odd = []
    for _n in REQUIRED_PEER_API:
        _obj = getattr(_mod_real, _n)
        if isinstance(_obj, type):
            continue
        if _n in ("PremiseError", "SubjectElsewhere"):
            _odd.append(f"{_n}→не класс")
            continue
        # функция прошла через фасад ⇒ это обёртка, а у обёртки есть __wrapped__
        if not hasattr(_obj, "__wrapped__"):
            _odd.append(f"{_n}→{type(_obj).__name__} мимо перевода")
    check(f"имён набора {len(REQUIRED_PEER_API)}, мимо перевода", _odd, [])

    # ═════════════════════════════════════════════════════════════════════════
    # ДВА МИРА ПОД ОДНИМ КОДОМ НОЛЬ — И ОНИ РАЗЛИЧИМЫ МАШИННО
    # ═════════════════════════════════════════════════════════════════════════
    # «Проверено, запаса хватает» и «мерить нечего, предмет в другом дереве» оба
    # отдают НОЛЬ. Различать их обязан машинный знак первой строки, а не проза:
    # два разных мира, печатающих одну строку, суть один мир.
    #
    # Свой код возврата рассмотрен и отвергнут — почему, сказано у соседа при
    # `VOID_MARK` (условие создать НЕЛЬЗЯ, ненулевой код дал бы вечное красное).
    # Чего это не закрывает, сказано там же: сегодняшний вызывающий знака не
    # читает, и эта половина держится вниманием.
    #
    # Утверждение ПОВЕДЕНЧЕСКОЕ: снимается вывод обоих путей и сверяется знак.
    # Объявление знака проверяло бы, что константа объявлена, а не что она
    # напечатана, — и осталось бы зелёным при знаке, потерянном из строки.
    print("── два исхода кода НОЛЬ: знак первой строки их различает")
    import contextlib as _ctx, io as _io  # noqa: PLC0415 — нужны только здесь

    # Знак и код беспредметности берутся У СОСЕДА — единственного их производителя.
    # Своя копия разошлась бы с ним молча, и разошлась бы именно там, где обе
    # отвечают «валидно»: на смене `VOID_EXIT` с нуля на два.
    _void_mark, _void_exit = _pg.VOID_MARK, _pg.VOID_EXIT
    note(_void_mark != PASS_MARK,
         f"знаки РАЗНЫЕ: беспредметный {_void_mark!r}, существенный {PASS_MARK!r}")

    def _run(root: str) -> tuple[int, str]:
        buf = _io.StringIO()
        with _ctx.redirect_stdout(buf):
            code = main(["--root", root])
        return code, buf.getvalue()

    _r = _pg.synthetic_wave_root(with_wave=False, with_seed=True,
                                 seed_body=_RATE_ROW)
    _code, _out = _run(_r)
    note(_code == _void_exit and _out.startswith(_void_mark + ":")
         and PASS_MARK not in _out,
         f"беспредметный путь: код {_code} (объявлен соседом {_void_exit}), знак "
         f"{_out.split(':')[0]!r}, существенного знака нет")
    _shutil.rmtree(_r, ignore_errors=True)

    # СУЩЕСТВЕННЫЙ ИСХОД — ДВА, и знак обязан стоять РОВНО НА ОДНОМ. Между двумя
    # прогонами меняется один факт: величина потолка в затравке. При настоящей
    # величине (три) этот корень даёт НАХОДКУ — ровно ту, ради которой гейт заведён:
    # личный аккаунт первого входа плюс два аккаунта посева плюс одно заведение
    # волны — четыре списания. При девяти запаса хватает.
    _WIDE = _RATE_ROW.replace(" 3, 3600,", " 9, 3600,")
    note(_WIDE != _RATE_ROW, "затравка с широким потолком отличается от настоящей")

    _r = _pg.synthetic_wave_root(with_wave=True, with_seed=True,
                                 seed_body=_WIDE, with_base=True)
    _code, _out = _run(_r)
    note(_code == 0 and (PASS_MARK + ":") in _out and _void_mark not in _out,
         f"существенный путь (запас есть): код {_code}, знак {PASS_MARK!r} "
         f"напечатан, беспредметного знака нет")
    _shutil.rmtree(_r, ignore_errors=True)

    # НАХОДКА знака существенного исхода НЕ печатает: иначе «проверено, чисто»
    # стало бы неотличимо от «найдено» — тем же способом, каким беспредметность
    # была неотличима от чистого прогона.
    _r = _pg.synthetic_wave_root(with_wave=True, with_seed=True,
                                 seed_body=_RATE_ROW, with_base=True)
    _code, _out = _run(_r)
    note(_code == 1 and PASS_MARK not in _out and _void_mark not in _out,
         f"находка: код {_code}, ни одного знака исхода в выводе не стоит")
    _shutil.rmtree(_r, ignore_errors=True)

    # ОБРАТНАЯ СТОРОНА ФИКСТУРЫ, на том же одном факте: снят источник базового
    # уровня — и существенный исход обязан стать ОТКАЗОМ, а не зелёным. Без этой
    # половины `with_base=True` был бы способом получить зелёное, а не корнем, на
    # котором предикат выполняется.
    _r = _pg.synthetic_wave_root(with_wave=True, with_seed=True,
                                 seed_body=_RATE_ROW, with_base=False)
    try:
        audit(_r)
        note(False, "источника базового уровня нет: прошло молча")
    except PremiseError as exc:
        note("базов" in str(exc) or "личного аккаунта" in str(exc),
             f"источника базового уровня нет → ОТКАЗ: {exc}")
    _shutil.rmtree(_r, ignore_errors=True)

    print()
    print(f"утверждений исполнено: {asserts}")
    print("PASS: темп заведения аккаунтов" if ok else "FAIL: темп заведения аккаунтов")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
