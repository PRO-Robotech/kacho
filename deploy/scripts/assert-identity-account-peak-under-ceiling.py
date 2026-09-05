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
IAM_MIGRATIONS = "services/iam/internal/migrations"
BOOTSTRAP_SOURCE = "services/iam/internal/apps/kacho/api/user/internal_upsert.go"

# Предъявители ОДНОГО человека церемонии. Оба обязаны стоять в объявлении церемонии —
# это проверяется, а не предполагается: переименуют один, и счёт молча перестанет
# видеть половину созданий.
CEREMONY_IDENTITY_BEARERS = ("jwtHumanCeremony", "jwtHumanCeremonyStepUp")

# Предъявитель шага читается из пре-скрипта: генератор пишет туда маркер, а ключа
# `auth` в коллекции нет вовсе.
BEARER = re.compile(r"bearer from env '([A-Za-z0-9_]+)'")

# Величина потолка — там, где она НАЗНАЧАЕТСЯ, а не там, где её ожидают тексты
# отказов.
#
# АДРЕСУЕТСЯ ПО ТОМУ, ЧТО ДЕРЕВО ПРОИЗВОДИТ, А НЕ ПО ИМЕНИ ФАЙЛА (2026-09-04).
# Здесь стояло имя одной миграции и ПОЗИЦИОННЫЙ разбор её значений
# (`('lim-N', 'DEFAULT', '', 'iam.account', N)`). Свод 171 миграции iam в одну
# первичную (`c0114fffe1`) отнял у гейта ОБА допущения разом: файла с таким
# именем больше нет, а у сохранившейся строки другой порядок колонок — между
# идентификатором и областью встала `created_at`. Величина при этом ЖИВА и
# по-прежнему назначается затравкой; предмет не исчез, исчез его адрес.
#
# Поэтому: обход ВСЕГО каталога миграций и разбор ПО ИМЕНИ КОЛОНКИ. Первое
# переживает переименование и свод файлов, второе — перестановку колонок,
# то есть ровно те два способа, которыми гейт уже сломался.
QUOTA_TABLE = "kaname.limits"
QUOTA_WHERE = {"scope": "DEFAULT", "scope_id": "", "kind": "iam.account",
               "withdrawn_at": "NULL"}

# Затравка миграции — оператор ОДНОЙ СТРОКОЙ со списком колонок. Форма измерена,
# а не предположена: в каталоге 449 операторов вставки, все этой формы, ноль без
# списка колонок и ноль `INSERT … SELECT`. Оператор, который разбор НЕ ПРОЧЁЛ, —
# находка, а не молчание: иначе новая форма записи увела бы величину из-под
# наблюдения, оставив гейт зелёным.
_INSERT = re.compile(
    r"^INSERT\s+INTO\s+(?P<table>[\w.]+)\s*\((?P<cols>[^)]*)\)\s*"
    r"VALUES\s*\((?P<vals>.*)\)\s*;\s*$")
_INSERT_HEAD = re.compile(r"^INSERT\s+INTO\s", re.I)

# ТЕЛО ФУНКЦИИ — НЕ ЗАТРАВКА, и различать их обязательно. Внутри `$$ … $$` лежат
# операторы вставки ВРЕМЕНИ ИСПОЛНЕНИЯ (журнал личности, окно допуска, списание
# квоты): они многострочны, значений не несут и строкой затравки не являются.
# Их девять на 458 вхождений, и без этого различения разбор честно объявлял бы
# каждое из них незнакомой формой.
#
# Разделяем по ТОМУ, ЧТО ЭТО ТАКОЕ (тело функции против верхнего уровня), а не по
# отступу: отступ — соглашение об оформлении, и первый же переформатированный
# файл увёл бы затравку из-под наблюдения молча. Форм долларовой кавычки в
# каталоге две — `$$` и `$_$`, — обе закрываются своей же меткой.
_DOLLAR_TAG = re.compile(r"\$[A-Za-z_]*\$")

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


def _split_values(s: str) -> list[str]:
    """Значения оператора вставки по ВЕРХНЕМУ уровню запятых.

    Наивный `split(",")` рвёт `now()` и строку с запятой внутри; после свода
    миграций такие значения в затравке есть (`now()` стоит второй колонкой).
    """
    out, cur, depth, quote = [], [], 0, False
    i = 0
    while i < len(s):
        ch = s[i]
        if quote:
            cur.append(ch)
            if ch == "'":
                if i + 1 < len(s) and s[i + 1] == "'":   # '' — экранированная кавычка
                    cur.append(s[i + 1])
                    i += 2
                    continue
                quote = False
        elif ch == "'":
            quote = True
            cur.append(ch)
        elif ch == "(":
            depth += 1
            cur.append(ch)
        elif ch == ")":
            depth -= 1
            cur.append(ch)
        elif ch == "," and depth == 0:
            out.append("".join(cur).strip())
            cur = []
        else:
            cur.append(ch)
        i += 1
    out.append("".join(cur).strip())
    return out


def _unquote(v: str) -> str:
    v = v.strip()
    if len(v) >= 2 and v[0] == "'" and v[-1] == "'":
        return v[1:-1].replace("''", "'")
    return v


def seeded_rows(root: str, table: str) -> tuple[list[dict[str, str]], dict[str, int]]:
    """Строки затравки таблицы `table` из ВСЕХ миграций iam + перепись.

    Возвращает `(строки, перепись)`; перепись несёт ДВЕ величины по каждой оси —
    осмотрено и найдено, — потому что «ноль найденных» обязано быть отличимо от
    «ноль прочитанных». Оператор вставки, который разбор не прочёл, — ОТКАЗ:
    молчание на незнакомой форме и есть тот способ, которым величина уходит
    из-под наблюдения незамеченной.
    """
    d = os.path.join(root, IAM_MIGRATIONS)
    if not os.path.isdir(d):
        raise PremiseError(
            f"каталога миграций iam нет: {d} — читать величину неоткуда, и это "
            f"ОТКАЗ, а не «чисто»")
    files = sorted(f for f in os.listdir(d) if f.endswith(".sql"))
    census = {"files": len(files), "inserts": 0, "rows": 0}
    if not files:
        raise PremiseError(
            f"в {IAM_MIGRATIONS} ноль файлов .sql — обход пуст, вердикт был бы "
            f"о непрочитанном дереве")
    rows: list[dict[str, str]] = []
    census["bodies"] = 0
    for name in files:
        body: str | None = None
        with open(os.path.join(d, name), encoding="utf-8") as fh:
            for lineno, line in enumerate(fh, 1):
                tags = _DOLLAR_TAG.findall(line)
                inside_at_start = body is not None
                for t in tags:
                    if body is None:
                        body = t
                        census["bodies"] += 1
                    elif body == t:
                        body = None
                if inside_at_start or body is not None:
                    continue          # тело функции — не затравка
                line = line.strip()
                if not _INSERT_HEAD.match(line):
                    continue
                census["inserts"] += 1
                m = _INSERT.match(line)
                if not m:
                    raise PremiseError(
                        f"{IAM_MIGRATIONS}/{name}:{lineno}: оператор вставки не "
                        f"разобран — форма записи затравки изменилась. Это ОТКАЗ, "
                        f"а не пропуск: непрочитанный оператор увёл бы величину "
                        f"из-под наблюдения, оставив гейт зелёным")
                if m.group("table") != table:
                    continue
                cols = [c.strip() for c in m.group("cols").split(",")]
                vals = _split_values(m.group("vals"))
                if len(cols) != len(vals):
                    raise PremiseError(
                        f"{IAM_MIGRATIONS}/{name}:{lineno}: колонок {len(cols)}, "
                        f"значений {len(vals)} — разбор неверен, а не строка")
                rows.append({c: _unquote(v) for c, v in zip(cols, vals)})
                census["rows"] += 1
        if body is not None:
            raise PremiseError(
                f"{IAM_MIGRATIONS}/{name}: долларовая кавычка {body} не закрыта — "
                f"разбор потерял границу тела функции, и часть дерева осталась "
                f"неосмотренной")
    return rows, census


def read_seeded_value(root: str, table: str, where: dict[str, str],
                      want: tuple[str, ...], what: str
                      ) -> tuple[tuple[str, ...], dict[str, int]]:
    """Значения колонок `want` у ЕДИНСТВЕННОЙ строки затравки, подходящей под `where`.

    Побеждает ПОСЛЕДНЯЯ подходящая строка — тот же порядок, что у самой базы, —
    и число подошедших называется: две затравки одной величины расходятся молча,
    и здесь такое уже оплачено соседним гейтом (`assert-alt-fixtures-are-another`).
    """
    rows, census = seeded_rows(root, table)
    hit = [r for r in rows
           if all(r.get(k, "\0") == v for k, v in where.items())]
    if not hit:
        raise PremiseError(
            f"{what} не найден: ни одна миграция iam не заводит строку {table} "
            f"по условию {where}. Осмотрено файлов {census['files']}, операторов "
            f"вставки {census['inserts']}, строк {table} {census['rows']}. "
            f"Либо величина снята вместе со своим предметом, либо изменилось "
            f"условие — вердикт был бы о выдуманном числе")
    row = hit[-1]
    missing = [c for c in want if c not in row]
    if missing:
        raise PremiseError(
            f"{what}: у строки {table} нет колонок {missing} — состав колонок "
            f"изменился, и вердикт был бы о выдуманном числе")
    census = dict(census)
    census["matched"] = len(hit)
    return tuple(row[c] for c in want), census


def read_ceiling(root: str) -> int:
    """Величина потолка. Перепись прочитанного — `ceiling_census`."""
    return read_ceiling_with_census(root)[0]


def read_ceiling_with_census(root: str) -> tuple[int, dict[str, int]]:
    (value,), census = read_seeded_value(root, QUOTA_TABLE, QUOTA_WHERE,
                                         ("limit_value",), "потолок")
    if not value.isdigit():
        raise PremiseError(
            f"потолок прочитан как {value!r} — не число, вердикт был бы о "
            f"выдуманной величине")
    return int(value), census


def read_base_components(root: str) -> tuple[int, int, list[str]]:
    """Базовый уровень ПО СОСТАВУ: (общий на всех, заводимый посевом, объяснение).

    Выводится, а не выписывается: выписанное число пережило бы свой предмет молча.

    СОСТАВНЫХ ЧАСТЕЙ ДВЕ, И ОНИ ДОСТАЮТСЯ РАЗНЫМ ЛИЧНОСТЯМ.
      * личный аккаунт первого входа — КАЖДОЙ вошедшей личности, безусловно
        (ветвь вставки; отказ по темпу на первом входе был бы отказом во входе);
      * аккаунты, которые заводит САМ посев, — только той личности, для которой он
        их заводит, и это объявлено (`ceremony_credentials.SEED_OWNED_ACCOUNTS`).

    ЗДЕСЬ БЫЛО «СЧИТАЕМ ВСЕ, БЕЗ РАЗБОРА, КОМУ». Пока личность волны была одна, разницы
    не существовало. С появлением личностей у заводящих проб она стала наблюдаемой и
    ЛОЖНОЙ В ОПАСНУЮ СТОРОНУ — не в объявленную: завышенный на единицу базовый уровень
    у восьми личностей превращал настоящий запас (единица) в нулевой, то есть гейт
    сообщал находку там, где её нет. Инструмент, у которого находки ложные, перестают
    читать. Разбор «кому достался аккаунт посева» по-прежнему НЕ делается разбором
    питона — он объявлен, а объявление держится сверкой сумм (`seeded_by_identity`).
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
    why.append(f"личный аккаунт первого входа — каждой личности (+1, {BOOTSTRAP_SOURCE})")

    seed = os.path.join(root, CEREMONY_SEED)
    try:
        seed_text = open(seed, encoding="utf-8").read()
    except OSError as exc:
        raise PremiseError(f"посев церемонии не прочитан: {exc}") from exc
    seeded = len(SEED_OWN_ACCOUNT.findall(seed_text))
    if seeded:
        why.append(f"аккаунты посева церемонии — названным личностям (+{seeded} всего, "
                   f"{CEREMONY_SEED})")
    else:
        why.append(f"посев церемонии аккаунтов не заводит (+0, {CEREMONY_SEED})")
    return 1, seeded, why


def read_base_level(root: str) -> tuple[int, list[str]]:
    """Наибольший базовый уровень среди личностей — совместимая форма ответа."""
    common, seeded, why = read_base_components(root)
    return common + seeded, why


def seeded_by_identity(decl, seeded_total: int) -> dict[str, int]:
    """Объявленное «кому достаются аккаунты посева» + СВЕРКА с выведенной суммой.

    Объявление без сверки устарело бы тихо: посев завёл бы второй аккаунт, и он
    достался бы никому. Сверка делает расхождение отказом.
    """
    got = getattr(decl, "SEED_OWNED_ACCOUNTS", None)
    if got is None:
        raise PremiseError(
            f"в {CEREMONY_DECL} нет `SEED_OWNED_ACCOUNTS` — кому достаются аккаунты "
            f"посева, сказать нечем, а раздать их всем значило бы выдумать базовый "
            f"уровень")
    declared = dict(got)
    if sum(declared.values()) != seeded_total:
        raise PremiseError(
            f"объявление `SEED_OWNED_ACCOUNTS` называет {sum(declared.values())} "
            f"аккаунт(ов) посева, а в {CEREMONY_SEED} их {seeded_total} — объявление "
            f"разошлось с посевом, и базовый уровень был бы о выдуманном составе")
    return declared


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


def identities_of(decl) -> dict[str, tuple[str, ...]]:
    """Личность → её предъявители, ИЗ ОБЪЯВЛЕНИЯ, а не из копии здесь.

    Пик считается ПО ЛИЧНОСТИ, а не по всей волне: потолок носит личность, и
    складывать чужие аккаунты в одну сумму значит выдумывать пик, которого нет.
    Прежняя редакция знала ровно одну личность — человека церемонии, — и это было
    верно ровно до того дня, когда заводящие кейсы получили СВОИХ людей: гейт
    честно нашёл бы ноль созданий под церемонией и объявил бы предикат ослепшим.
    Ослеп бы он и наоборот — при десяти личностях счёт по одной оставил бы девять
    вне наблюдения молча.
    """
    got = getattr(decl, "HUMAN_IDENTITIES", None)
    if not got:
        raise PremiseError(
            f"в {CEREMONY_DECL} нет `HUMAN_IDENTITIES` — разбить предъявителей по "
            f"личностям не на чем, а счёт по всем сразу выдумал бы пик")
    out = dict(got)
    for name, bearers in out.items():
        if not bearers:
            raise PremiseError(
                f"у личности {name!r} не объявлено ни одного предъявителя — её "
                f"создания не попали бы в счёт, и это молчание, а не ноль")
    return out


def audit(root: str):
    decl, gate = load_declarations(root)
    identities = identities_of(decl)
    for name, bearers in identities.items():
        for b in bearers:
            if b not in decl.CEREMONY_ONLY_ENV:
                raise PremiseError(
                    f"предъявитель {b!r} (личность {name!r}) не объявлен в "
                    f"CEREMONY_ONLY_ENV — счёт видел бы не все создания этой личности")
    ceiling, mig_census = read_ceiling_with_census(root)
    common, seeded_total, why = read_base_components(root)
    seeded = seeded_by_identity(decl, seeded_total)
    for name in seeded:
        if name not in identities:
            raise PremiseError(
                f"`SEED_OWNED_ACCOUNTS` называет личность {name!r}, которой нет в "
                f"`HUMAN_IDENTITIES` — её аккаунты не попали бы ни в чей счёт")
    base = common + max(seeded.values(), default=0)
    wave = wave_collections(root, decl)

    per_identity: list[tuple[str, int, tuple | None, int, int]] = []
    cases = steps = 0
    for name, bearers in sorted(identities.items()):
        events, cases, steps = timeline_of(wave, gate, bearers)
        peak, at, n_created, n_released = peak_of(events, common + seeded.get(name, 0))
        per_identity.append((name, peak, at, n_created, n_released))

    total_created = sum(row[3] for row in per_identity)
    if total_created == 0:
        raise PremiseError(
            "созданий аккаунта НЕ НАЙДЕНО ни под одной объявленной личностью — "
            "предикат ослеп, чинить надо гейт, а не выходить успехом")

    # Вердикт выносит САМАЯ НАГРУЖЕННАЯ личность: потолок у каждой свой, поэтому
    # запас волны равен наименьшему запасу среди них.
    worst = max(per_identity, key=lambda row: row[1])
    census = {
        "collections": len(wave), "cases": cases, "steps": steps,
        "created": total_created,
        "released": sum(row[4] for row in per_identity),
        "identities": per_identity,
        "order": [stem for stem, _ in wave],
        # Перепись ЧТЕНИЯ ВЕЛИЧИНЫ — две числа по каждой оси, осмотрено и найдено.
        # Печатается и на ЗЕЛЁНОМ пути: «ноль находок» обязано быть отличимо от
        # «ноль прочитанного» до того, как что-нибудь сломается, а не после.
        "migrations": mig_census,
    }
    return worst[1], worst[2], ceiling, base, why, census


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
          f"{census['steps']} шаг(ов); личност(ей) {len(census['identities'])}, "
          f"созданий аккаунта {census['created']}, освобождений {census['released']}")
    print("базовый уровень " + str(base) + ": " + "; ".join(why))
    for name, ipeak, _iat, icreated, ireleased in census["identities"]:
        print(f"  личность {name}: создани(й) {icreated}, освобождени(й) {ireleased}, "
              f"пик {ipeak}, запас {ceiling - ipeak}")
    mc = census["migrations"]
    print(f"величина потолка: осмотрено файлов миграций {mc['files']}, операторов "
          f"затравки {mc['inserts']}, из них строк {QUOTA_TABLE} {mc['rows']}, "
          f"подошло под условие {mc['matched']}")
    print(f"потолок {ceiling} ({QUOTA_TABLE} @ {IAM_MIGRATIONS}); "
          f"наибольший пик одновременно живых "
          f"{peak}, наименьший запас {ceiling - peak}")

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

    # ── чтение величины: инъекции в РАЗБОРЩИК ────────────────────────────────
    # Гейт сломался ДВАЖДЫ одним изменением — свод миграций отнял и имя файла, и
    # порядок колонок, — поэтому каждая из двух осей проверяется отдельно, а
    # рядом стоит законный близнец. Каждый случай меняет РОВНО ОДИН факт против
    # него: иначе неизвестно, какой из двух дал отказ.
    import tempfile  # noqa: PLC0415 — нужен только здесь

    _CANON = ("INSERT INTO kaname.limits (id, created_at, scope, scope_id, "
              "kind, limit_value, withdrawn_at, revision) VALUES "
              "('lim-00000000000000032', now(), 'DEFAULT', '', 'iam.account', "
              "5, NULL, 32);\n")

    def _tree(body: str) -> str:
        d = tempfile.mkdtemp(prefix="kacho-mig-")
        os.makedirs(os.path.join(d, IAM_MIGRATIONS), exist_ok=True)
        with open(os.path.join(d, IAM_MIGRATIONS, "0001_initial.sql"), "w",
                  encoding="utf-8") as fh:
            fh.write(body)
        return d

    print("── величина читается по ИМЕНИ КОЛОНКИ и по всему каталогу миграций")
    # Законный близнец: канонический оператор затравки читается.
    try:
        note(read_ceiling(_tree(_CANON)) == 5, "канонический оператор → 5")
    except PremiseError as exc:
        note(False, f"канонический оператор: {exc}")

    # ОСЬ 1 — перестановка колонок. Ровно тот факт, на котором гейт сломался:
    # позиционный разбор вернул бы чужое число либо промолчал.
    _shuffled = ("INSERT INTO kaname.limits (kind, limit_value, scope, "
                 "scope_id, withdrawn_at, id, revision, created_at) VALUES "
                 "('iam.account', 5, 'DEFAULT', '', NULL, "
                 "'lim-00000000000000032', 32, now());\n")
    try:
        note(read_ceiling(_tree(_shuffled)) == 5, "колонки переставлены → всё ещё 5")
    except PremiseError as exc:
        note(False, f"перестановка колонок: {exc}")

    # ОСЬ 2 — имя файла. Величина в файле с ДРУГИМ именем обязана читаться:
    # адрес — каталог, а не имя, иначе следующий свод отнимет предмет снова.
    _renamed = tempfile.mkdtemp(prefix="kacho-mig-")
    os.makedirs(os.path.join(_renamed, IAM_MIGRATIONS), exist_ok=True)
    with open(os.path.join(_renamed, IAM_MIGRATIONS, "99999_other_name.sql"), "w",
              encoding="utf-8") as _fh:
        _fh.write(_CANON)
    try:
        note(read_ceiling(_renamed) == 5, "файл переименован → всё ещё 5")
    except PremiseError as exc:
        note(False, f"переименование файла: {exc}")

    # Дубль затравки: побеждает ПОСЛЕДНЯЯ строка — тот же порядок, что у базы.
    _dup = _CANON + _CANON.replace(", 5, NULL, 32)", ", 9, NULL, 33)")
    try:
        note(read_ceiling(_tree(_dup)) == 9, "две затравки → побеждает последняя (9)")
    except PremiseError as exc:
        note(False, f"дубль затравки: {exc}")

    # Отказы: у каждого свой ОДИН изменённый факт против канонического близнеца.
    for label, body in (
        # Незнакомая форма записи — ОТКАЗ, а не молчаливый пропуск: пропуск увёл
        # бы величину из-под наблюдения, оставив гейт зелёным.
        ("оператор без списка колонок",
         "INSERT INTO kaname.limits VALUES ('lim-1', now(), 'DEFAULT', '', "
         "'iam.account', 5, NULL, 32);\n"),
        # Строка есть, но условие не подходит — величины нет.
        ("вид сменён", _CANON.replace("'iam.account'", "'iam.project'")),
        ("затравка отозвана", _CANON.replace(", 5, NULL, 32)", ", 5, now(), 32)")),
        # Тело функции затравкой НЕ является.
        ("оператор внутри тела функции",
         "CREATE FUNCTION f() RETURNS void AS $$\nBEGIN\n    " + _CANON
         + "END;\n$$ LANGUAGE plpgsql;\n"),
        ("в каталоге ноль файлов .sql", ""),
    ):
        d = _tree(body) if body else tempfile.mkdtemp(prefix="kacho-mig-")
        if not body:
            os.makedirs(os.path.join(d, IAM_MIGRATIONS), exist_ok=True)
        try:
            got = read_ceiling(d)
            note(False, f"{label}: прошло молча (вернул {got})")
        except PremiseError:
            note(True, f"{label}: ОТКАЗ")

    print("── предпосылки: пустое и нечитаемое суть ОТКАЗ, а не «чисто»")
    for label, fn in (
        ("пустая волна", lambda: wave_collections(REPO, _EmptyDecl())),
        ("потолка нет в дереве", lambda: read_ceiling(os.path.join(REPO, "нет-такого"))),
        ("источника базового уровня нет",
         lambda: read_base_level(os.path.join(REPO, "нет-такого"))),
        # Разбиение по личностям — предпосылка вердикта, а не удобство: без него
        # счёт идёт по одной личности и не видит остальных.
        ("разбиения по личностям нет", lambda: identities_of(_NoIdentitiesDecl())),
        ("у личности ноль предъявителей", lambda: identities_of(_EmptyBearersDecl())),
    ):
        try:
            fn()
            note(False, f"{label}: прошло молча")
        except PremiseError:
            note(True, f"{label}: ОТКАЗ")

    # Положительный контроль к двум предыдущим: настоящее объявление читается и
    # даёт больше одной личности. Без него «отказ на пустом» зеленел бы на
    # предикате, который отказывает всегда.
    try:
        _decl_live, _ = load_declarations(REPO)
        live = identities_of(_decl_live)
        note(len(live) > 1, f"настоящее объявление даёт личност(ей) {len(live)} (> 1)")
    except PremiseError as exc:
        note(False, f"настоящее объявление личностей: {exc}")

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


class _NoIdentitiesDecl:
    """Объявление БЕЗ разбиения по личностям — фикстура предпосылки."""

    CEREMONY_ONLY_ENV: dict[str, str] = {}


class _EmptyBearersDecl:
    """Личность объявлена, а предъявителей у неё ноль — фикстура предпосылки."""

    CEREMONY_ONLY_ENV: dict[str, str] = {}
    HUMAN_IDENTITIES: dict[str, tuple[str, ...]] = {"пустая": ()}


class _EmptyDecl:
    """Объявление, отдающее пустую волну — фикстура предпосылки."""

    @staticmethod
    def stems_for_suite(_suite, _root):
        return []


if __name__ == "__main__":
    sys.exit(main())
