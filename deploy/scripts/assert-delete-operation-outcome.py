#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1
"""Асинхронное удаление: ИСХОД операции утверждается, а её идентификатор — свой.

Первая волна закрыла шаг удаления, не несущий НИ ОДНОГО утверждения
(`assert-delete-steps-are-asserted.py`). Она же обнажила два остатка, которые
живут не в самом шаге удаления, а в ПАРЕ «удаление → опрос операции». Оба
зеленеют, поэтому ни один прогон их не показывал.

ЧТО ИЗМЕРЕНО (перепись по дереву, `git ls-files '*.postman_collection.json'`,
ревизия 72325f00, ЭТИМ ЖЕ предикатом — снимок на момент находки, а не
инвариант; живые числа печатает сам гейт при каждом проходе)
---------------------------------------------------------------------------
82 коллекции, 8237 шагов, 1359 из них DELETE, 2683 шага опрашивают операцию.
Шагов опроса, чей предмет — удаление: 909; они складываются в 880 ЦЕПОЧЕК
(у одного удаления читателей его операции бывает несколько — см. ниже).

  • КЛАСС А — исход не назван НИ ОДНИМ читателем цепочки: 842 из 880 (826
    называют только завершение, ещё 16 не называют ничего). Операция,
    завершившаяся ОШИБКОЙ, тоже `done`: `done === true` держится, шаг
    зелёный, ресурс жив.
  • КЛАСС Б — идентификатор операции захватывается, не будучи сброшенным:
    706 из 880. Отказ тела `id` не несёт, захват не срабатывает, и имя
    сохраняет значение ПРЕДЫДУЩЕЙ операции. Опрос уезжает на чужую операцию —
    как правило давно завершённую, поэтому зелёный приходит БЫСТРО и уверенно.

РАЗДЕЛЕНИЕ ПО СУЩЕСТВУ, А НЕ ПО ФОРМЕ. «Захват без сброса» опасен ровно тогда,
когда имя может нести значение, произведённое ВНЕ этого кейса. Перепись по
области жизни имени: из 706 у 701 есть писатель в ДРУГОМ кейсе той же коллекции
(все — общий `opId`, у которого в коллекции от 3 до 89 писателей), и лишь у 5
писатель один и тот же кейс — там чужого значения взять неоткуда by
construction. Требование сбрасывать предъявляется всем: на кейс-приватном имени
сброс безвреден, а различать их в гейте значило бы держать предикат, который
разъедется с генератором.

ПОЧЕМУ ЭТО ДВА РАЗНЫХ КЛАССА, А НЕ ОДИН. Класс Б делает опрос беспредметным
(отвечает чужая операция), класс А — беззубым (своя операция отвечает, но её
ответ не читают). Починка одного не закрывает другой: сняв устаревший
идентификатор, мы получим опрос СВОЕЙ операции — и по-прежнему не спросим, чем
она кончилась.

ЧТО ПРОВЕРЯЕТСЯ
---------------
Для каждого шага, опрашивающего операцию (`GET /operations/{{<имя>}}`), предмет
определяется ближайшим предшествующим шагом-мутацией ТОГО ЖЕ кейса; шаги с
одним предметом образуют ЦЕПОЧКУ читателей этой операции. Если предмет —
`DELETE`:

  А. ИСХОД операции обязан назвать хотя бы один читатель цепочки — прочитать
     `error` внутри `pm.expect(...)`. Утверждения одного лишь `done`
     недостаточно: у завершившейся с ошибкой операции `done` тоже `true`;
  Б. шаг удаления, ЗАПИСЫВАЮЩИЙ имя, которое читает опрос, обязан его сперва
     сбросить — снять (`pm.environment.unset('<имя>')`) либо задать пустым
     (`pm.environment.set('<имя>', '')`). Тогда несостоявшийся захват не
     подменяется чужой операцией.

Форма утверждения не навязывается — навязывается наличие: `j.error`,
`_do.error`, `Boolean(j.response) && !j.error`, `lastOpError` читаются
одинаково. Не навязывается и ЗНАК исхода: кейс, чей предмет — ОТКАЗ удаления,
утверждает наличие ошибки и этим класс закрывает.

ЧИТАЕТСЯ ИСПОЛНЯЕМАЯ ЧАСТЬ, А НЕ ТЕКСТ. Комментарии (`//`, `/* */`) снимаются
до поиска: оба класса объясняются в коде рядом с защитой, и поиск по сырому
тексту принял бы объяснение за защиту (`testing.md` §«Гейт на класс», п. 4).

ПОЧИНКА — В ГЕНЕРАТОРЕ. Коллекции регенерируются побайтово из
`*/tests/newman/scripts/gen.py`; правка сгенерированного файла будет затёрта.
Класс А ставит `_assert_delete_operation_outcome` (проход по шагам кейса в
`case_to_postman`), класс Б — `save_from_response`, снимающий имя операции
перед захватом.

Запуск:
    python3 deploy/scripts/assert-delete-operation-outcome.py
    python3 deploy/scripts/assert-delete-operation-outcome.py --self-test
Код возврата: 0 — исход удаления утверждается и опрашивается своя операция;
1 — находка.
"""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import tempfile

MUTATION_METHODS = ("POST", "PUT", "PATCH", "DELETE")

# Опрос операции: шаг GET, чей адрес — /operations/{{<имя>}}. Имя берётся из
# подстановки, потому что общий `opId` в дереве не единственный: кейсы iam
# заводят собственные (`_abDelOpId_<case>`), и требовать от них общего имени
# значило бы чинить не тот предмет.
OP_POLL_URL = re.compile(r"/operations/\{\{(\w+)\}\}")

# Утверждение ОБ ИСХОДЕ / О ЗАВЕРШЕНИИ — обращение к полю операции ВНУТРИ
# аргумента `pm.expect(...)`. Опознаётся поле, а не носитель: имя переменной у
# каждого поллера своё (`j`, `_dj`, `_do`), а форма выражения — тем более
# (`j.error && j.error.code`, `Boolean(j.response) && !j.error`,
# `pm.environment.get('lastOpError') || ''`). Требовать одну запись значило бы
# ловить полюбившуюся форму вместо существа.
_FIELD_OUTCOME = re.compile(r"\.error\b|lastOpError")
_FIELD_DONE = re.compile(r"\.done\b")


def _expect_args(code: str):
    """Аргументы каждого вызова `pm.expect(...)` — до конца стейтмента."""
    for m in re.finditer(r"pm\.expect\(", code):
        yield code[m.end():m.end() + 300].split(";")[0]


def asserts_outcome(code: str) -> bool:
    return any(_FIELD_OUTCOME.search(a) for a in _expect_args(code))


def asserts_done(code: str) -> bool:
    return any(_FIELD_DONE.search(a) for a in _expect_args(code))


def strip_js_comments(src: str) -> str:
    """Снять `//`-хвосты и `/* */`-блоки, сохранив переводы строк.

    Посимвольный разбор с учётом строковых литералов: `//` внутри строки (в
    URL, например) комментарием не является, и срезать его значило бы рубить
    код. Регулярным выражением это не делается — оно не различает литерал и
    код, а именно за это различение гейт здесь и отвечает.
    """
    out = []
    i, n = 0, len(src)
    quote = None
    while i < n:
        ch = src[i]
        nxt = src[i + 1] if i + 1 < n else ""
        if quote:
            out.append(ch)
            if ch == "\\" and i + 1 < n:
                out.append(nxt)
                i += 2
                continue
            if ch == quote:
                quote = None
            i += 1
            continue
        if ch in ("'", '"', "`"):
            quote = ch
            out.append(ch)
            i += 1
            continue
        if ch == "/" and nxt == "/":
            while i < n and src[i] != "\n":
                i += 1
            continue
        if ch == "/" and nxt == "*":
            i += 2
            while i < n and not (src[i] == "*" and i + 1 < n and src[i + 1] == "/"):
                if src[i] == "\n":
                    out.append("\n")
                i += 1
            i += 2
            continue
        out.append(ch)
        i += 1
    return "".join(out)


def script(item: dict, listen: str) -> str:
    for ev in item.get("event") or []:
        if ev.get("listen") == listen:
            return "\n".join(ev.get("script", {}).get("exec") or [])
    return ""


def executable(item: dict) -> str:
    """Исполняемая часть обоих скриптов шага (проверка + пред-запрос)."""
    return strip_js_comments(script(item, "test")) + "\n" + strip_js_comments(
        script(item, "prerequest"))


def raw_url(item: dict) -> str:
    url = (item.get("request") or {}).get("url")
    if isinstance(url, dict):
        return url.get("raw", "")
    return url or ""


def method(item: dict) -> str:
    return (item.get("request") or {}).get("method", "")


def poll_var(item: dict) -> str:
    """Имя переменной, по которой шаг опрашивает операцию, либо пустая строка."""
    if method(item) != "GET":
        return ""
    m = OP_POLL_URL.search(raw_url(item))
    return m.group(1) if m else ""


def writes_var(code: str, var: str) -> bool:
    return re.search(r"environment\.set\(\s*['\"]" + re.escape(var) + r"['\"]\s*,", code) is not None


def clears_var(code: str, var: str) -> bool:
    """Снятие имени — либо `unset`, либо присвоение пустой строки.

    Пустая строка — законная форма: её ставит помощник синхронного отказа,
    чтобы имя ОСТАЛОСЬ ОПРЕДЕЛЁННЫМ (иначе страж неразрешённой подстановки
    справедливо уронил бы опрос там, где отсутствия операции и ждали).
    Обе формы решают задачу гейта: устаревшее значение не переживает шаг.
    """
    if re.search(r"environment\.unset\(\s*['\"]" + re.escape(var) + r"['\"]\s*\)", code):
        return True
    return re.search(
        r"environment\.set\(\s*['\"]" + re.escape(var) + r"['\"]\s*,\s*(''|\"\")\s*\)", code
    ) is not None


def collection_files(root: str) -> list[str]:
    """Отслеживаемые коллекции. Авторитет — версионный контроль: то же множество,
    что увидит CI на свежем checkout'е. В синтетическом дереве самопроверки
    git'а нет — там обход файловой системы."""
    git = subprocess.run(["git", "-C", root, "ls-files", "-z", "*.postman_collection.json"],
                         capture_output=True, text=True)
    if git.returncode == 0 and git.stdout.strip("\0"):
        return sorted(n for n in git.stdout.split("\0") if n)
    names = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d != ".git"]
        for fn in filenames:
            if fn.endswith(".postman_collection.json"):
                names.append(os.path.relpath(os.path.join(dirpath, fn), root))
    return sorted(names)


def walk(items, trail):
    for it in items or []:
        name = it.get("name", "<без имени>")
        if "request" in it:
            yield it, trail + [name]
        if "item" in it:
            yield from walk(it["item"], trail + [name])


def scan(root: str):
    """→ (находки А, находки Б, счётчики).

    Предмет опроса — ближайший предшествующий шаг-мутация ТОГО ЖЕ кейса. Граница
    кейса соблюдается намеренно: без неё опрос подхватывал бы удаление из
    соседнего кейса и гейт судил бы о паре, которой нет.

    КЛАСС А СУДИТСЯ ПО ЦЕПОЧКЕ, А НЕ ПО ОТДЕЛЬНОМУ ШАГУ. У одного удаления
    опросов бывает несколько: первый дожидается завершения, следующий читает ту
    же операцию и утверждает о ней предметное. Разбирая шаги поодиночке, гейт
    объявил бы находкой ожидающий шаг кейса, чей ПРЕДМЕТ — отказ удаления, и
    потребовал бы от него утверждения, ПРОТИВОПОЛОЖНОГО тому, которое кейс уже
    делает соседним шагом (замеренный случай: удаление отсутствующего образа —
    ожидается ошибка операции; удаление роли, на которую есть выдача — то же).
    Поэтому вопрос задаётся один на цепочку: сказал ли КТО-НИБУДЬ из читателей
    этой операции о её исходе — успехе или отказе. Находка называется первым
    шагом цепочки, потому что чинится она там.
    """
    a_findings, b_findings = [], []
    n_col = n_step = n_del = n_poll = n_del_poll = n_chain = 0
    for rel in collection_files(root):
        try:
            with open(os.path.join(root, rel), encoding="utf-8") as fh:
                doc = json.load(fh)
        except (OSError, ValueError) as exc:
            a_findings.append(f"{rel}: коллекция не читается ({exc}) — шаги не осмотрены")
            continue
        n_col += 1
        steps = list(walk(doc.get("item"), []))
        n_step += len(steps)
        chains: dict[int, list[int]] = {}
        subject_idx: int | None = None
        subject_case: list | None = None
        for idx, (item, trail) in enumerate(steps):
            if method(item) == "DELETE":
                n_del += 1
            if poll_var(item):
                n_poll += 1
                if subject_idx is not None and trail[:-1] == subject_case:
                    chains.setdefault(subject_idx, []).append(idx)
                continue
            if method(item) in MUTATION_METHODS:
                subject_idx, subject_case = idx, trail[:-1]
        for sidx, polls in chains.items():
            if method(steps[sidx][0]) != "DELETE":
                continue
            n_chain += 1
            n_del_poll += len(polls)
            code = "\n".join(strip_js_comments(script(steps[k][0], "test")) for k in polls)
            if not asserts_outcome(code):
                why = ("утверждается только завершение" if asserts_done(code)
                       else "не утверждается ничего")
                a_findings.append(f"{rel} :: " + " :: ".join(steps[polls[0]][1])
                                  + f"  [{why}; опросов в цепочке {len(polls)}]")
            var = poll_var(steps[polls[0]][0])
            subj_code = executable(steps[sidx][0])
            if writes_var(subj_code, var) and not clears_var(subj_code, var):
                b_findings.append(f"{rel} :: " + " :: ".join(steps[sidx][1]) + f"  [{var}]")
    return a_findings, b_findings, (n_col, n_step, n_del, n_poll, n_del_poll, n_chain)


# ─────────────────────────── самопроверка ───────────────────────────────────
def _step(name: str, mtd: str, path: str, exec_lines: list[str],
          pre_lines: list[str] | None = None) -> dict:
    events = [{"listen": "test", "script": {"type": "text/javascript", "exec": exec_lines}}]
    if pre_lines:
        events.insert(0, {"listen": "prerequest",
                          "script": {"type": "text/javascript", "exec": pre_lines}})
    return {
        "name": name,
        "request": {"method": mtd, "url": {"raw": "{{baseUrl}}" + path}},
        "event": events,
    }


CAPTURE = [
    "try {",
    "  const j = pm.response.json();",
    "  if (j.id) pm.environment.set('opId', String(j.id));",
    "} catch (e) {}",
]
POLL_DONE_ONLY = [
    "const j = pm.response.json();",
    "pm.test('operation done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
]
POLL_WITH_OUTCOME = POLL_DONE_ONLY + [
    "pm.test('operation succeeded', () => "
    "pm.expect(j.error && JSON.stringify(j.error), 'operation.error').to.eql(undefined));",
]


def _case(name: str, steps: list[dict]) -> dict:
    return {"name": name, "item": steps}


def self_test() -> int:
    print("=== исход операции удаления: инъекции в синтетическое дерево ===")
    rc = 0
    with tempfile.TemporaryDirectory() as tmp:
        os.makedirs(os.path.join(tmp, "svc", "collections"), exist_ok=True)
        cases = [
            # ── ДЕФЕКТ А + ДЕФЕКТ Б в одной паре: ровно та форма, что стояла в дереве.
            _case("CASE-defect", [
                _step("del-thing", "DELETE", "/x/v1/things/{{id}}",
                      ["pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
                       *CAPTURE]),
                _step("poll-op-1", "GET", "/operations/{{opId}}", POLL_DONE_ONLY),
            ]),
            # ── ДЕФЕКТ А, ВТОРАЯ ФОРМА: опрос не утверждает ВООБЩЕ ничего
            # (рукописный поллер уборки). Захват при этом снят — значит гейт
            # обязан назвать ТОЛЬКО класс А, не приписав ему класс Б.
            _case("CASE-silent-poll", [
                _step("del-silent", "DELETE", "/x/v1/things/{{id}}",
                      ["pm.environment.unset('opId');",
                       "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
                       *CAPTURE]),
                _step("poll-op-silent", "GET", "/operations/{{opId}}",
                      ["let _dj; try { _dj = pm.response.json(); } catch (e) { _dj = {}; }",
                       "pm.environment.unset('_pollCount');",
                       "pm.environment.unset('opId');"]),
            ]),
            # ── ЗАКОННЫЙ БЛИЗНЕЦ обоих классов: снятие перед захватом + утверждение
            # исхода. Гейт обязан молчать по обоим.
            _case("CASE-clean", [
                _step("del-clean", "DELETE", "/x/v1/things/{{id}}",
                      ["pm.environment.unset('opId');",
                       "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
                       *CAPTURE]),
                _step("poll-op-2", "GET", "/operations/{{opId}}", POLL_WITH_OUTCOME),
            ]),
            # ── ВТОРОЙ ЗАКОННЫЙ БЛИЗНЕЦ: снятие записано ПУСТОЙ СТРОКОЙ (форма
            # помощника синхронного отказа), исход утверждён ДРУГИМ носителем
            # ответа (`_do`, не `j`). Гейт требует наличия, а не полюбившейся
            # ему записи.
            _case("CASE-clean-empty", [
                _step("del-refused", "DELETE", "/x/v1/things/{{id}}",
                      ["pm.environment.set('opId', '');",
                       "pm.test('refused', () => pm.expect(pm.response.code).to.be.oneOf([400, 200]));",
                       "if (pm.response.code === 200) { const j = pm.response.json();"
                       " if (j.id) pm.environment.set('opId', j.id); }"]),
                _step("poll-op-3", "GET", "/operations/{{opId}}",
                      ["const _do = pm.response.json();",
                       "pm.test('operation refused', () => "
                       "pm.expect(_do.error, JSON.stringify(_do)).to.be.an('object'));"]),
            ]),
            # ── ДЕФЕКТ А, ЗАМАСКИРОВАННЫЙ ОБЪЯСНЕНИЕМ: единственное упоминание
            # `error` стоит в КОММЕНТАРИИ, объясняющем защиту. Поиск по сырому
            # тексту счёл бы объяснение защитой и промолчал.
            _case("CASE-commented", [
                _step("del-commented", "DELETE", "/x/v1/things/{{id}}",
                      ["pm.environment.unset('opId');",
                       "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
                       *CAPTURE]),
                _step("poll-op-4", "GET", "/operations/{{opId}}",
                      ["const j = pm.response.json();",
                       "// здесь должно стоять pm.expect(j.error, '...').to.eql(undefined);",
                       "/* и ещё pm.expect(j.error).to.be.undefined — тоже объяснение */",
                       "pm.test('operation done', () => pm.expect(j.done).to.eql(true));"]),
            ]),
            # ── НЕ ПРЕДМЕТ ГЕЙТА: предмет опроса — СОЗДАНИЕ. Тот же класс на
            # создании существует, но предмет этого гейта — удаление; ловя всё
            # подряд, он перестал бы называть то, что чинит.
            _case("CASE-create", [
                _step("create-thing", "POST", "/x/v1/things",
                      ["pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
                       *CAPTURE]),
                _step("poll-op-5", "GET", "/operations/{{opId}}", POLL_DONE_ONLY),
            ]),
            # ── ГРАНИЦА КЕЙСА: опрос БЕЗ своей мутации. Предыдущий кейс кончился
            # удалением — без границы гейт приписал бы этому опросу чужой предмет.
            _case("CASE-orphan-poll", [
                _step("poll-op-6", "GET", "/operations/{{opId}}", POLL_DONE_ONLY),
            ]),
            # ── ГРАНИЦА РАЗБОРА: `//` внутри строкового литерала — не комментарий.
            # Срезав его, читатель отрубил бы настоящее утверждение следом.
            _case("CASE-url-in-string", [
                _step("del-url", "DELETE", "/x/v1/things/{{id}}",
                      ["pm.environment.unset('opId');",
                       "const base = 'https://api.kacho.local//v1';",
                       "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
                       *CAPTURE]),
                _step("poll-op-7", "GET", "/operations/{{opId}}",
                      ["const base = 'https://api.kacho.local//v1';",
                       "const j = pm.response.json();",
                       "pm.test('operation done', () => pm.expect(j.done).to.eql(true));",
                       "pm.test('no error', () => pm.expect(j.error).to.eql(undefined));"]),
            ]),
            # ── СВОЁ ИМЯ ОПЕРАЦИИ: опрос идёт не по общему `opId`. Гейт обязан
            # видеть и такую пару — иначе кейсы с собственным именем выпали бы
            # из предмета молча.
            _case("CASE-own-var", [
                _step("del-own", "DELETE", "/x/v1/things/{{id}}",
                      ["pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
                       "const j = pm.response.json();",
                       "pm.environment.set('_abDelOpId_X', String(j.id));"]),
                _step("poll-op-own", "GET", "/operations/{{_abDelOpId_X}}", POLL_WITH_OUTCOME),
            ]),
            # ── ЗАКОННЫЙ БЛИЗНЕЦ, РАДИ КОТОРОГО КЛАСС СУДИТСЯ ПО ЦЕПОЧКЕ: кейс,
            # чей ПРЕДМЕТ — отказ удаления. Ожидающий шаг об исходе молчит, а
            # следующий читатель ТОЙ ЖЕ операции утверждает, что она завершилась
            # ошибкой. Гейт обязан молчать по ОБОИМ: требование к ожидающему
            # означало бы утверждение, противоположное предмету кейса.
            _case("CASE-delete-must-fail", [
                _step("del-absent", "DELETE", "/x/v1/things/{{absentId}}",
                      ["pm.environment.unset('opId');",
                       "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
                       *CAPTURE]),
                _step("poll-op-8", "GET", "/operations/{{opId}}", POLL_DONE_ONLY),
                _step("assert-op-error", "GET", "/operations/{{opId}}",
                      ["const j = pm.response.json();",
                       "pm.test('error code 5', () => "
                       "pm.expect(j.error && j.error.code, JSON.stringify(j)).to.eql(5));"]),
            ]),
            # ── ВТОРАЯ ЗАКОННАЯ ЗАПИСЬ УСПЕХА В ЦЕПОЧКЕ: исход утверждён не
            # прямым чтением поля, а через `Boolean(j.response) && !j.error` и
            # через `lastOpError`. Обе — утверждения об исходе; узнавать одну
            # запись значило бы ловить форму.
            _case("CASE-chain-other-forms", [
                _step("del-chained", "DELETE", "/x/v1/things/{{id}}",
                      ["pm.environment.unset('opId');",
                       "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
                       *CAPTURE]),
                _step("poll-op-9", "GET", "/operations/{{opId}}", POLL_DONE_ONLY),
                _step("assert-op-success", "GET", "/operations/{{opId}}",
                      ["const j = pm.response.json();",
                       "pm.test('operation succeeded', () => "
                       "pm.expect(Boolean(j.response) && !j.error, JSON.stringify(j)).to.eql(true));"]),
            ]),
            # ── ЦЕПОЧКА ИЗ ДВУХ, ГДЕ НЕ ГОВОРИТ НИ ОДИН: находка называется
            # ПЕРВЫМ шагом цепочки (там она и чинится), второй не дублируется.
            _case("CASE-chain-silent", [
                _step("del-chain-silent", "DELETE", "/x/v1/things/{{id}}",
                      ["pm.environment.unset('opId');",
                       "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
                       *CAPTURE]),
                _step("poll-op-10", "GET", "/operations/{{opId}}", POLL_DONE_ONLY),
                _step("poll-op-11", "GET", "/operations/{{opId}}", POLL_DONE_ONLY),
            ]),
        ]
        path = os.path.join(tmp, "svc", "collections", "probe.postman_collection.json")
        with open(path, "w", encoding="utf-8") as fh:
            json.dump({"info": {"name": "probe"}, "item": cases}, fh)

        a_f, b_f, (n_col, n_step, n_del, n_poll, n_del_poll, n_chain) = scan(tmp)
        a_names = {f.split(" :: ")[-1].split("  [")[0] for f in a_f}
        b_names = {f.split(" :: ")[-1].split("  [")[0] for f in b_f}

        for want, why in (("poll-op-1", "утверждение одного лишь done принято за исход"),
                          ("poll-op-silent", "опрос без единого утверждения не назван"),
                          ("poll-op-10", "цепочка, где не говорит ни один, не названа"),
                          ("poll-op-4", "объяснение защиты принято за защиту")):
            if want in a_names:
                print(f"  ОК  А: дефект найден: {want}")
            else:
                print(f"  ПРОВАЛ А: дефект НЕ найден: {want} — {why}")
                rc = 1
        for quiet, why in (("poll-op-2", "исход утверждён `j.error`"),
                           ("poll-op-3", "исход утверждён другим носителем ответа `_do`"),
                           ("poll-op-5", "предмет опроса — создание, не удаление"),
                           ("poll-op-6", "у опроса нет мутации в своём кейсе"),
                           ("poll-op-7", "`//` внутри строкового литерала срезан как комментарий"),
                           ("poll-op-own", "исход утверждён на своём имени операции"),
                           ("poll-op-8", "предмет кейса — ОТКАЗ удаления, и его утверждает "
                                         "следующий читатель той же операции"),
                           ("assert-op-error", "он и есть утверждение об исходе"),
                           ("poll-op-9", "исход утверждён формой `Boolean(response) && !error`"),
                           ("assert-op-success", "он и есть утверждение об исходе"),
                           ("poll-op-11", "находка названа первым шагом цепочки, второй не дублируется")):
            if quiet in a_names:
                print(f"  ПРОВАЛ А: ложное срабатывание: {quiet} — {why}")
                rc = 1
            else:
                print(f"  ОК  А: законная форма пропущена: {quiet}")

        for want, why in (("del-thing", "захват без снятия принят за безопасный"),
                          ("del-own", "своё имя операции выпало из предмета")):
            if want in b_names:
                print(f"  ОК  Б: дефект найден: {want}")
            else:
                print(f"  ПРОВАЛ Б: дефект НЕ найден: {want} — {why}")
                rc = 1
        for quiet, why in (("del-clean", "снятие записано unset"),
                           ("del-refused", "снятие записано пустой строкой"),
                           ("del-silent", "снятие есть — это находка класса А, не Б"),
                           ("del-commented", "снятие есть"),
                           ("del-url", "снятие есть"),
                           ("del-absent", "снятие есть"),
                           ("del-chained", "снятие есть"),
                           ("del-chain-silent", "снятие есть"),
                           ("create-thing", "предмет — создание, не удаление")):
            if quiet in b_names:
                print(f"  ПРОВАЛ Б: ложное срабатывание: {quiet} — {why}")
                rc = 1
            else:
                print(f"  ОК  Б: законная форма пропущена: {quiet}")

        # КООРДИНАТА. Гейт, который говорит «есть находки», но не говорит где,
        # не чинится — его снимают.
        coord = next((f for f in a_f if "poll-op-1" in f), "")
        if coord.startswith("svc/collections/") and coord.count(" :: ") >= 2:
            print(f"  ОК  находка названа координатой: {coord}")
        else:
            print(f"  ПРОВАЛ находка без координаты: {coord!r}")
            rc = 1

        if (n_col, n_del, n_del_poll, n_chain) == (1, 10, 13, 10):
            print(f"  ОК  объём осмотренного считается: коллекций {n_col}, шагов {n_step}, "
                  f"DELETE {n_del}, опросов {n_poll}, из них с предметом-удалением "
                  f"{n_del_poll} в {n_chain} цепочках")
        else:
            print(f"  ПРОВАЛ объём осмотренного неверен: коллекций {n_col}, шагов {n_step}, "
                  f"DELETE {n_del}, опросов {n_poll}, с предметом-удалением {n_del_poll}, "
                  f"цепочек {n_chain} (ожидалось 1 / 10 / 13 / 10)")
            rc = 1

        # ПРЕДПОСЫЛКА: пустое дерево — ОТКАЗ, а не «чисто».
        with tempfile.TemporaryDirectory() as empty:
            _, _, (e_col, _, _, _, e_dp, _) = scan(empty)
            if (e_col, e_dp) == (0, 0):
                print("  ОК  пустое дерево даёт ноль ПРОЧИТАННОГО — основной проход обязан падать")
            else:
                print(f"  ПРОВАЛ пустое дерево дало {e_col} коллекций / {e_dp} пар")
                rc = 1

    print()
    print("PASS: гейт «исход операции удаления утверждается, идентификатор — свой»" if rc == 0
          else "FAIL: гейт «исход операции удаления утверждается, идентификатор — свой»")
    return rc


def main() -> int:
    if "--self-test" in sys.argv[1:]:
        return self_test()

    root = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
    a_f, b_f, (n_col, n_step, n_del, n_poll, n_del_poll, n_chain) = scan(root)

    print(f"=== опрос операции удаления: осмотрено коллекций {n_col}, шагов {n_step}, "
          f"DELETE {n_del}, опросов операции {n_poll}, из них с предметом-удалением "
          f"{n_del_poll} в {n_chain} цепочках ===")
    if n_col == 0 or n_del_poll == 0:
        print("FAIL: обход не нашёл ни коллекций, ни пар «удаление → опрос операции» —")
        print("      предмет запрета потерян либо обход сломан. Пустая находка на пустом")
        print("      обходе не является доказательством чего-либо.")
        return 1

    rc = 0
    if a_f:
        rc = 1
        print(f"FAIL (класс А): {len(a_f)} опрос(ов) не утверждают ИСХОД операции удаления —")
        print("      операция, завершившаяся ошибкой, тоже `done`, поэтому отказ удаления")
        print("      читается как успех: ресурс остаётся жить, вердикт не меняется.")
        for f in a_f:
            print(f"    {f}")
        print()
    if b_f:
        rc = 1
        print(f"FAIL (класс Б): {len(b_f)} шаг(ов) удаления захватывают идентификатор операции,")
        print("      не сняв предыдущий. Отказ тела `id` не несёт — захват не срабатывает,")
        print("      и опрос уезжает на ЧУЖУЮ, давно завершённую операцию: зелёный приходит")
        print("      быстро и уверенно, а собственное удаление не проверено вовсе.")
        for f in b_f:
            print(f"    {f}")
        print()
    if rc:
        print("Чинить в ГЕНЕРАТОРЕ (*/tests/newman/scripts/gen.py): класс А —")
        print("`_assert_delete_operation_outcome` в `case_to_postman`, класс Б —")
        print("`save_from_response`, снимающий имя операции перед захватом. Правка")
        print("сгенерированной коллекции будет затёрта следующим прогоном генератора.")
        return 1

    print(f"PASS: все {n_chain} цепочек опроса удаления утверждают исход "
          f"({n_del_poll} шагов опроса) и опрашивают СВОЮ операцию")
    return 0


if __name__ == "__main__":
    sys.exit(main())
