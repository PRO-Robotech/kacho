#!/usr/bin/env python3

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""
tests/newman/scripts/gen.py — generator of Postman collections from declarative
case-modules under tests/newman/cases/*.py (kacho-nlb).

Usage:
    python3 scripts/gen.py                      # all case modules → collections/<name>.postman_collection.json
    python3 scripts/gen.py load-balancer        # one module
    python3 scripts/gen.py --validate           # delegate to validate-cases.py (dup-id + CASES-INDEX coverage)

The generator is intentionally a near-mirror of kacho-vpc/tests/newman/scripts/gen.py
(KAC-VPC convention). NLB-specific helpers and the unified poll_operation_until_done
step live here so case modules only import the high-level Case / Step / helpers via
the module namespace (no `from gen import ...` because gen.py is loaded by path).
"""
from __future__ import annotations

import json
import re
import subprocess
import sys
import uuid
import importlib.util
from pathlib import Path
from dataclasses import dataclass, field, replace
from typing import List, Dict, Optional


def js_str(value: str) -> str:
    r"""Строковый литерал JavaScript, произведённый СЕРИАЛИЗАТОРОМ (#1181).

    Текст вызывающего — пояснение, фрагмент контракт-тона, подпись шага, имя
    переменной — уезжает в ПОРОЖДАЕМЫЙ скрипт шага. Апостроф закрывает литерал,
    перевод строки рвёт строку, `</script>` закрывает элемент: ломается не
    текст, а СИНТАКСИС файла, которого автор фразы не видит.

    ПОЧЕМУ ЭТО НЕ ВИДНО В ВЕРДИКТЕ. newman пишет исключение скрипта в
    `testScripts`, а НЕ в `assertions.failed`. Шаг, чей скрипт не разобрался,
    даёт НОЛЬ упавших утверждений: кейс перестаёт проверять что бы то ни было и
    продолжает отчитываться зелёным по этой величине. Это третья категория
    исхода («не выполнилось»), зачтённая в «прошло».

    ПОЧЕМУ СЕРИАЛИЗАТОР, А НЕ ЗАМЕНА ЗНАКОВ. Рукописная замена всегда неполна:
    geo экранировал обратный слэш и апостроф, но не перевод строки, и потому
    закрывал ровно тот случай, который однажды заметили. Полный набор — обратный
    слэш, управляющие знаки, кавычка — делает `json.dumps`. Сверх него закрыты
    три случая, которых JSON не знает, и каждый ЗНАЧЕНИЯ литерала не меняет:

      * U+2028/U+2029 — законный JSON, но до ES2019 рвали литерал JS;
      * `</` → `<\/` — иначе закрылся бы элемент `script`, если текст шага
        встроят в отчёт-документ; `\/` в JS тождественно `/`;
      * апостроф → `\'` — литерал одинарно-кавычечный (ниже о том, почему).
        Правило применяется ПОСЛЕ сериализатора, когда каждый обратный слэш уже
        удвоен, поэтому оно не может ни пропустить случай, ни съесть чужой
        экранирующий знак.

    ПОЧЕМУ ОДИНАРНАЯ КАВЫЧКА, А НЕ ДВОЙНАЯ ИЗ `json.dumps`. Порождаемый скрипт
    цитирует одинарной; двойная кавычка сменила бы БАЙТЫ 91 закоммиченной
    коллекции, которые читают два десятка гейтов, ничего не изменив по существу.
    Одинарная форма даёт байт-в-байт то же, что вклейка, на всяком входе, где
    вклейка была законна, — поэтому перегенерация после этой правки обязана дать
    ПУСТОЙ diff, и это единственное, что доказывает: экранирование ничего не
    исказило.

    ЧЕМ ДЕРЖИТСЯ. Проба
    `services/iam/tests/newman/scripts/js_literal_escape_test.py` — одна на все
    восемь генераторов, потому что шов один, а восемь копий разошлись бы. Она
    утверждает четыре разных вещи: ФОРМУ по всему дереву (ни одной подстановки
    в литерал помимо этих двух помощников), СУЩЕСТВО по швам (враждебный вход
    даёт РАЗБИРАЕМЫЙ скрипт), положительный контроль (безобидная фраза читается
    дословно) и ОБРАТИМОСТЬ настоящим движком — node, а не `json.loads`: судить
    надо тем языком, который литерал и будет исполнять.
    """
    body = json.dumps(str(value), ensure_ascii=False)[1:-1]
    body = body.replace("\u2028", "\\u2028").replace("\u2029", "\\u2029")
    body = body.replace("</", "<\\/")
    body = body.replace('\\"', '"').replace("'", "\\'")
    return "'" + body + "'"


def js_comment(value: str) -> str:
    r"""Текст вызывающего ВНУТРИ комментария порождаемого скрипта (#1181).

    У комментария опасен ровно один класс знаков — КОНЕЦ СТРОКИ: он закрывает
    комментарий, и остаток значения становится КОДОМ. Кавычки внутри комментария
    безвредны, поэтому литерала тут не строят — строку вставляют в текст, и
    внешние кавычки сериализатора снимаются.

    Концов строки у JavaScript ЧЕТЫРЕ, а у JSON два: сверх `\n` и `\r` строку
    завершают U+2028 и U+2029, и `json.dumps` их не трогает — они законный JSON.
    Именно на этом правило и ловилось: враждебное имя с U+2028 закрывало
    комментарий, и `${...}` за ним разбирался как выражение. Поэтому два знака
    дописываются к набору сериализатора явно — не вместо него, а поверх.
    """
    text = json.dumps(str(value), ensure_ascii=False)[1:-1]
    return text.replace("\u2028", "\\u2028").replace("\u2029", "\\u2029")


_REGEX_FLAGS = "dgimsuvy"
_REGEX_PARSE_CACHE: Dict[tuple, str] = {}


def js_regex_src(pattern: str, *, where: str, flags: str = "") -> str:
    r"""ОБРАЗЕЦ вызывающего внутри литерала регулярного выражения (#1202).

    Здесь вызывающий даёт КОД, а не текст: знаки выражения значимы, и
    сериализатор строки (`js_str`) СМЕНИЛ БЫ СМЫСЛ — образец перестал бы
    совпадать. Поэтому образец возвращается ДОСЛОВНО, а исход у него другой:
    он проверяется ПРИ ГЕНЕРАЦИИ, и негодный роняет её С ИМЕНЕМ МЕСТА.

    ПОЧЕМУ ЭТО НЕ ВИДНО В ВЕРДИКТЕ. Негодный образец ломает не текст, а
    СИНТАКСИС порождаемого файла, которого автор значения не видит. newman
    пишет отказ разбора в `testScripts`, а НЕ в `assertions.failed`: шаг с
    неразобранным скриптом даёт НОЛЬ упавших утверждений и отчитывается зелёным
    по этой величине. Третья категория исхода, зачтённая в «прошло».

    ПРОВЕРОК ДВЕ, И ОДНОЙ НЕ ХВАТАЕТ — ЭТО ИЗМЕРЕНО, А НЕ ПРЕДПОЛОЖЕНО.
    `new Function("return /" + образец + "/;")` на образце
    `x/; process.exit(1); //` разбирается УСПЕШНО: литерал закрылся на первом же
    разделителе, а хвост стал КОДОМ. То есть проверка «разбирается ли» пропускает
    ровно ту подмену, ради которой заведена. Поэтому:

      1. ОХВАТ — литерал обязан вобрать ВЕСЬ образец. Это лексический разбор
         тела выражения, свой, без движка: спросить движок «где кончился
         литерал» можно только исполнив собранную строку, а исполнять чужой код
         в генераторе нельзя;
      2. РАЗБИРАЕМОСТЬ — грамматику судит НАСТОЯЩИЙ движок, тот самый, который
         будет исполнять литерал. Питонов `re` — другой язык: он не знает ни
         `\p{L}`, ни именованных групп JavaScript, и отвергал бы законное.

    Порядок именно такой: охват доказан ДО того, как строка попадает в node,
    поэтому подмена туда не доезжает by construction.

    ЧЕМ ДЕРЖИТСЯ. Проба
    `services/iam/tests/newman/scripts/js_regex_literal_test.py` — одна на все
    генераторы: перепись по дереву (каждая подстановка в литерал выражения несёт
    ЗАПИСАННЫЙ исход), инъекция негодным образцом (обязан упасть, назвав место) и
    положительный контроль законным (обязан пройти молча и остаться ДОСЛОВНЫМ).
    """
    if not isinstance(pattern, str) or pattern == "":
        raise ValueError(
            f"{where}: образец регулярного выражения пуст. Пустой литерал `//` —"
            f" это КОММЕНТАРИЙ JavaScript, а не выражение: остаток строки станет"
            f" прозой, и утверждение не исполнится вовсе")
    unknown = sorted({f for f in flags if f not in _REGEX_FLAGS})
    if unknown or len(set(flags)) != len(flags):
        raise ValueError(
            f"{where}: негодные флаги выражения {flags!r}"
            + (f" — неизвестны: {unknown}" if unknown else " — флаг повторён"))
    _regex_literal_must_contain_the_whole_pattern(pattern, where)
    _regex_must_parse_in_javascript(pattern, flags, where)
    return pattern


def _regex_literal_must_contain_the_whole_pattern(pattern: str, where: str) -> None:
    """Литерал `/…/` обязан кончиться ТАМ, где кончился образец, и не раньше."""
    in_class, i = False, 0
    while i < len(pattern):
        ch = pattern[i]
        # Разделители строк — экранированными: знаками они невидимы в
        # исходнике, и первый же редактор молча их съест.
        if ch in "\n\r\u2028\u2029":
            raise ValueError(
                f"{where}: образец несёт конец строки (U+{ord(ch):04X}) —"
                f" литерал регулярного выражения его не переживёт, скрипт"
                f" порвётся на этой строке")
        if ch == "\\":
            if i + 1 >= len(pattern):
                raise ValueError(
                    f"{where}: образец кончается одиноким обратным слэшем —"
                    f" он экранирует закрывающий разделитель, и литерал не"
                    f" закроется")
            i += 2
            continue
        if in_class:
            if ch == "]":
                in_class = False
        elif ch == "[":
            in_class = True
        elif ch == "/":
            raise ValueError(
                f"{where}: образец несёт НЕэкранированный разделитель `/` —"
                f" литерал закроется на нём, а хвост образца станет КОДОМ."
                f" Напишите `\\/`: в регулярном выражении это тот же знак")
        i += 1
    if in_class:
        raise ValueError(
            f"{where}: в образце незакрытый класс символов `[` — движок дочитает"
            f" его до закрывающего разделителя и объявит литерал незавершённым")


def _regex_must_parse_in_javascript(pattern: str, flags: str, where: str) -> None:
    """Грамматику судит движок, который литерал и будет исполнять."""
    key = (pattern, flags)
    verdict = _REGEX_PARSE_CACHE.get(key)
    if verdict is None:
        driver = ("const a=JSON.parse(process.argv[1]);"
                  "try{new Function('return /'+a.p+'/'+a.f+';');"
                  "process.stdout.write('OK');}"
                  "catch(e){process.stdout.write('ERR '+e.message);}")
        payload = json.dumps({"p": pattern, "f": flags})
        try:
            proc = subprocess.run(["node", "-e", driver, payload],
                                  capture_output=True, text=True, timeout=60)
        except (OSError, subprocess.SubprocessError) as exc:
            raise ValueError(
                f"{where}: образец проверить НЕЧЕМ — node не запускается ({exc})."
                f" Это «ноль прочитанного», а не «ноль находок»: генерация"
                f" отказывает, а не пропускает непроверенный образец") from None
        verdict = (proc.stdout.strip() if proc.returncode == 0
                   else f"ERR node {proc.returncode}: {proc.stderr[:200]}")
        _REGEX_PARSE_CACHE[key] = verdict
    if verdict != "OK":
        raise ValueError(
            f"{where}: образец /{pattern}/{flags} не разбирается как регулярное"
            f" выражение JavaScript — {verdict}")

ROOT = Path(__file__).resolve().parents[1]
SCRIPTS_DIR = Path(__file__).resolve().parent
CASES_DIR = ROOT / "cases"
OUT_DIR = ROOT / "collections"

# Monotonic sequence for poll-step names within a single collection build.
# poll_operation_until_done() self-retries via pm.execution.setNextRequest(
# pm.info.requestName); newman resolves setNextRequest by request NAME and jumps
# to the FIRST match in the flattened collection. Identically-named "poll-op"
# steps (one per mutation, hundreds per collection) therefore made a mid-suite
# retry jump back to an early folder and skip the folders in between — a plain
# `newman run <collection>` traversed only a fraction of the cases. A per-step
# unique name keeps the self-retry unambiguous so full linear traversal is
# preserved. Reset to 0 at the start of every module load (load_cases_module) so
# names are deterministic per collection.
_poll_seq = 0


# ---------------------------------------------------------------------------
# Declarative structures
# ---------------------------------------------------------------------------

@dataclass
class Step:
    """A single HTTP request within a Case."""
    name: str
    method: str
    path: str  # relative; {{baseUrl}} prefix prepended automatically
    body: Optional[Dict] = None
    pre_script: List[str] = field(default_factory=list)
    test_script: List[str] = field(default_factory=list)
    # auth override per-step (None = inherit collection-level default Bearer):
    #   "anonymous"       — strip Authorization header before request
    #   "<envVarName>"    — Authorization: Bearer {{envVarName}} (resolved from env)
    auth: Optional[str] = None


@dataclass
class Case:
    """One test case — may contain multiple sequential steps."""
    id: str        # e.g. NLB-CR-CRUD-OK
    title: str     # human-readable summary
    classes: List[str]   # CRUD / VAL / NEG / BVA / CONF / STATE / IDEM / LSG / AZD
    priority: str        # P0 / P1 / P2 / P3
    steps: List[Step]


# ---------------------------------------------------------------------------
# Global pre-request — runs before every request in every case
# ---------------------------------------------------------------------------

# СТРАЖ НЕРАЗРЕШЁННОГО АДРЕСА — на уровне КОЛЛЕКЦИИ.
#
# Newman подставляет `{{имя}}` только если переменная где-то определена; неопределённую
# он оставляет ЛИТЕРАЛОМ и отправляет как есть. Запрос уходит на адрес вида
# `/operations/{{someOp}}`, сервис честно отвечает `invalid operation id`, а поллер
# крутит на этом весь свой предел — «не done» он читает как «ещё не готово», хотя повтор
# не сойдётся НИКОГДА. Замер боевой посадки 2026-07-30: из 12823 исполнений 992 ушли с
# неразрешённым `{{имя}}`; 791 из них размножили ОДНУ отвергнутую мутацию в десятки
# одинаковых отказов, а 201 не дали НИ ОДНОГО утверждения — исполнялись против шаблона и
# исчезали из вердикта («не выполнилось» тихо шло в зачёт «прошло», testing.md).
#
# Полный разбор класса и доказательство инъекцией — в `services/iam/tests/newman/scripts/gen.py`
# (там же перепись: 2571 опрос операции на 82 коллекции, под стражем helper'а — 188).
#
# ПРЕДИКАТ УЗКИЙ: срабатывает, только когда имя НЕ ОПРЕДЕЛЕНО НИ В ОДНОЙ области.
# Переменная, заданная ПУСТОЙ, — законный негативный кейс: newman подставит её пустой
# строкой, литерала не останется, страж до неё не доберётся by construction.
_URL_VAR_GUARD = [
    "(function () {",
    "  var _u = '';",
    "  try { _u = pm.request.url.toString(); } catch (e) { return; }",
    "  var _all = _u.match(/\\{\\{[A-Za-z0-9_]+\\}\\}/g);",
    "  if (!_all) { return; }",
    "  var _n = null;",
    "  for (var _i = 0; _i < _all.length; _i++) {",
    "    var _c = _all[_i].slice(2, -2);",
    "    if (!pm.variables.has(_c)) { _n = _c; break; }",
    "  }",
    "  if (_n === null) { return; }",
    "  pm.test('предусловие: {{' + _n + '}} не было захвачено — запрос не отправлен', function () {",
    "    pm.expect.fail(_n + ' не определена ни в одной области. Мутация, которая должна была её "
    "захватить, не вернула Operation (была отвергнута), либо захват не состоялся. Отправка ушла бы "
    "литералом и повтор не сошёлся бы никогда.');",
    "  });",
    "  pm.execution.skipRequest();",
    "})();",
]


PRE_GLOBAL = [
    "if (!pm.environment.get('runId') || pm.environment.get('runId') === '') {",
    "  const t = Date.now().toString(36);",
    "  const r = Math.floor(Math.random() * 1e9).toString(36);",
    "  pm.environment.set('runId', (t + r).replace(/[^a-z0-9]/g, '').slice(-10));",
    "}",
    "pm.environment.set('_suiteProjectId', pm.environment.get('existingProjectId'));",
    "pm.environment.set('_suiteProjectCrossId', pm.environment.get('existingProjectCrossId'));",
    "pm.environment.set('_suiteRegionId', pm.environment.get('existingRegionId'));",
    "pm.environment.set('_suiteRegionAltId', pm.environment.get('existingRegionAltId'));",
    "// Default auth: project-editor JWT on project A (sufficient for most happy-path steps).",
    "// Per-step auth= overrides via _auth_pre_script.",
    # СУБЪЕКТ СУИТЫ НЕ ПОДМЕНЯЕТСЯ. Здесь стоял fallback на jwtBootstrap
    # (кластерный администратор) «пока per-subject JWT не засеяны». Подмена молчаливая:
    # при незасеянном jwtProjectEditorA вся суита уезжала под администратора и проверяла
    # КАСКАД ЕГО ПРАВ, а не то, что заявлено (проектный editor). Зелёная суита при этом
    # ничего не говорила о заявленном субъекте, а happy-path 401, который fallback
    # «снимал», был честным сообщением о несделанном посеве.
    "const __defaultJwt = pm.environment.get('jwtProjectEditorA') || pm.variables.get('jwtProjectEditorA') || '';",
    "if (__defaultJwt && !pm.request.headers.has('Authorization')) {",
    "  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __defaultJwt});",
    "}",
    # REQUIRED-FIXTURE GUARD: отсутствие фикстуры — ОТКАЗ с именем переменной, а не
    # подмена и не тишина. Пустой идентификатор уезжает в запрос как есть и отвергается
    # на авторизации, поэтому каждый последующий провал называл бы неверную причину.
    "const __needed = ['baseUrl', 'existingProjectId', 'existingRegionId', 'jwtProjectEditorA'];",
    "const __missing = __needed.filter((k) => !(pm.environment.get(k) || pm.variables.get(k) || ''));",
    "if (__missing.length > 0) {",
    "  pm.test('FIXTURE REQUIRED: ' + __missing.join(', '), () => pm.expect.fail('the suite fixture "
    "seed did not run: ' + __missing.join(', ') + ' are empty. Seed via tests/authz-fixtures "
    "(prodrun.sh patches the suite env). Falling back to another subject would test a DIFFERENT "
    "principal and report green about a subject that was never exercised.'));",
    # ...ЗАТЕМ ПРОПУСТИТЬ. Санкционированная форма — утвердить (назвав переменную) и
    # НЕ отправлять; она объявлена в exec-coverage.py (раздел STATIC BANS), эталон —
    # iam gen.py::require_env_url. Этот скрипт — pre-request КОРНЯ коллекции, то есть
    # исполняется перед КАЖДЫМ запросом: без пропуска весь набор уезжал бы без
    # предъявителя, о котором сказано прямо в тексте отказа выше, и каждый шаг
    # засчитывался бы против ДРУГОГО принципала. Утверждение уже отработало, поэтому
    # пропуск остаётся ЗАПИСАННЫМ падением с именем переменной, а не немым.
    "  pm.execution.skipRequest();",
    "}",
    *_URL_VAR_GUARD,
]


# ---------------------------------------------------------------------------
# АДРЕС НАРЕЗАЕМОЙ ПОДСЕТИ — ИЗ ПЛАНА СЕТИ, А НЕ ИЗ СОБСТВЕННОГО ХЕША
# ---------------------------------------------------------------------------
#
# Сеть набора (`existingNetworkId`) приходит из посева, и посев объявляет ей
# адресный план. Подсеть обязана лежать ВНУТРИ него: иначе синхронный отказ
# `subnet CIDR <X> is not within any network CIDR block`.
#
# Здесь стояли ПЯТЬ почти одинаковых копий генератора адреса, собиравших его из
# хеша прогона без всякой связи с планом (`'10.' + октет + '.' + октет + '.0/24'`,
# `'fd' + число.toString(16) + …`). Попадание в план было СОВПАДЕНИЕМ: держалось
# на том, что один из двух посевов набора объявляет `10.0.0.0/8` и `fd00::/8`.
# Второй посев (`tests/authz-fixtures/prodseed_nlb_ext.py`, посадка prodrun)
# объявляет план у́же — `10.196.0.0/16` и `fd00:196::/48`, — и мимо него уходили
# ВСЕ адреса всех пяти копий, на всех хешах. Симптом при этом уезжал далеко от
# причины: отказ получал шаг нарезки, а падали за ним шаги, которые ничего не
# резали, — с сообщением «предусловие: {{…}} не было захвачено».
#
# Теперь адрес ВЫВОДИТСЯ из плана, который посев публикует переменными
# `existingNetworkV4Plan` / `existingNetworkV6Plan`. Посев волен менять план —
# кейсы следуют за ним сами, и второго места об одном предмете не заводится.
#
# Держит это гейт `internal/repohygiene`
# `TestOutOfCaseCarveTakesItsCidrFromThePublishedPlan`: шаг, режущий подсеть в сети
# из посева и не прочитавший её плана, — находка. Гейт закрывает ту слепую зону,
# которую соседний `newmansubnetsupernet_test.go` объявлял числом.
#
# ---------------------------------------------------------------------------
# ПОЗИЦИЯ ВНУТРИ ПЛАНА — ВЫЧИСЛЯЕТСЯ, А НЕ РАЗЫГРЫВАЕТСЯ
# ---------------------------------------------------------------------------
#
# Попадание в план ничего не говорит о том, что ДВА набора одного прогона возьмут
# разные адреса. Пока позиция разыгрывалась хешем, столкновение было вопросом
# вероятности — и вероятность эта измерена, а не предположена: 69 нарезок за
# прогон (перепись по committed-коллекциям), план `10.0.0.0/8` даёт 65 536
# различных /24, симуляция самой формулы на 20 000 runId — **3.40 %** прогонов с
# хотя бы одним столкновением. По прогонам `e2e-newman` за 08-13…08-16 —
# два столкновения на 63 прогона с вердиктом шарда nlb, то есть 3.2 %.
#
# Под ВТОРЫМ посевом (`prodseed_nlb_ext.py`, план `10.196.0.0/16`) в плане всего
# 256 /24 — там жеребьёвка сталкивается практически ВСЕГДА (1 − e^(−69·68/512) ≈
# 99.99 %). То есть у одного и того же кода два режима: «через раз» и «всегда».
#
# Поэтому пространство плана делится на равные полосы — по одной на набор
# (`CIDR_BANDS` ниже), — а позиция внутри полосы задаётся порядковым номером
# нарезки. Столкновение становится невозможным ПО ПОСТРОЕНИЮ. Энтропия прогона
# остаётся и делает СВОЮ работу: она смещает всю сетку целиком, поэтому два
# прогона на одном стенде по-прежнему расходятся, а полосы внутри прогона
# по-прежнему не пересекаются (общий сдвиг по модулю сохраняет непересечение).
#
# Держит это гейт `internal/repohygiene`
# `TestCarvedSubnetsOfOneRunCanNotCollide`.

# CIDR_BANDS — полоса адресного пространства на набор. Таблица ЯВНАЯ, а не хеш от
# имени: хеш вернул бы ту же жеребьёвку, только на пяти значениях вместо 65 536.
# Набор, которого здесь нет, генерацию РОНЯЕТ — молча делить полосу с соседом
# нельзя, иначе непересечение перестаёт быть свойством и становится удачей.
CIDR_BANDS: Dict[str, int] = {
    "authz-deny": 0,
    "cross-resource": 1,
    "listener": 2,
    "load-balancer": 3,
    "placement-coherence": 4,
}

# Номера обязаны быть в точности 0..N-1, и это проверяется ЗДЕСЬ, при импорте.
#
# Ширина полосы — `span / __bands`, где `__bands = len(CIDR_BANDS)`, а позиция берётся
# по модулю `span`. Поэтому номер `>= len` не «выходит за план» (модуль вернёт его
# внутрь) — он ложится ПОВЕРХ чужой полосы: номера остаются различными, а адреса двух
# наборов совпадают снова, то есть возвращается ровно тот дефект, ради которого полосы
# и заведены. Дырка в нумерации даёт то же самое: снять набор из таблицы, не
# перенумеровав остальные, — самая правдоподобная правка этого места.
#
# Проверка стоит при импорте, а не в `carve_cidr_pre`: там она сработала бы только для
# наборов, которые что-то режут, и неверная таблица дожила бы до прогона.
_expected_bands = set(range(len(CIDR_BANDS)))
if set(CIDR_BANDS.values()) != _expected_bands:
    raise ValueError(
        f"gen: номера полос в CIDR_BANDS обязаны быть в точности "
        f"{sorted(_expected_bands)}, а объявлены {sorted(CIDR_BANDS.values())}. "
        f"Номер вне диапазона ложится ПОВЕРХ чужой полосы (позиция считается по "
        f"модулю ширины плана), и два набора одного прогона снова вырезают один /24 — "
        f"«Subnet CIDRs can not overlap» на шаге нарезки и каскад на шагах, которые "
        f"нарезки не делали. Снимая набор, перенумеруйте остальные подряд."
    )

_CARVE_HELPERS = [
    # Энтропия позиции: хеш прогона + порядковый номер нарезки + соль позиции.
    # Детерминирована по (runId, seq), поэтому повтор прогона воспроизводим, а
    # параллельные прогоны расходятся.
    #
    # ПЕРЕМЕШИВАНИЕ ЗДЕСЬ — НЕ КОСМЕТИКА, и оно измерено, а не предположено:
    # коллизия адресов и есть тот блуждающий флейк, разбор которого лежит в шапке
    # `_CIDR_ALLOC_PRE` набора listener. Соседние позиции отличаются лишь
    # постоянным слагаемым, поэтому БЕЗ перемешивания младшие байты двух позиций
    # связаны намертво. Замер на 20000 различных runId при seq=1, план `10.0.0.0/8`
    # (различных /24):
    #     снятая формула              16857   (сетка 220×256)
    #     без перемешивания             256   (обе позиции — один и тот же байт)
    #     с перемешиванием (здесь)    17243   при потолке 17236 для 16 свободных
    #                                         бит — то есть позиции независимы
    # Потолок задаёт ШИРИНА ПЛАНА, а не помощник: под планом `/16` свободен один
    # октет, и различных /24 достижимо ровно 256 — это свойство посева, и названо
    # здесь, чтобы его не искали в генераторе.
    #
    # Множитель выбран так, чтобы произведение оставалось ТОЧНЫМ в double
    # (65599 × 2³¹ < 2⁵³): `Math.imul` в песочнице newman'а недоступен, а молча
    # потерянная точность дала бы смещённый разброс, который ничем не виден.
    "function __mix(v) {",
    "  v = (v ^ (v >>> 13)) & 0x7fffffff;",
    "  v = (v * 65599) % 2147483647;",
    "  return (v ^ (v >>> 11)) & 0x7fffffff;",
    "}",
    "function __ent(k) {",
    "  return __mix(__h + (__seq + 1) * 7919 + k * 104729) & 0xffff;",
    "}",
    # Разбор плана и нарезка ВНУТРИ него. Позиция, целиком накрытая префиксом
    # плана, сохраняется как есть; накрытая частично — по маске; свободная —
    # энтропией. Так адрес лежит внутри плана ЛЮБОЙ ширины, а не только той, под
    # которую его однажды подогнали.
    # Позиция /24 внутри плана: полоса набора + порядковый номер нарезки, всё
    # смещено общим для прогона сдвигом. Арифметика, а не битовые операции: план
    # шире /8 даёт номера до 2²⁴, и `<<` пришлось бы читать со знаком.
    #
    # `'!band'` — ОТДЕЛЬНЫЙ исход, а не пустая строка: «плана нет» и «набор
    # перерос свою полосу» лечатся разным, и общий текст отказа сделал бы их
    # неразличимыми — ровно тот класс, который мы ловим в продукте.
    "function __carve4(plan) {",
    "  var m = /^\\s*(\\d+)\\.(\\d+)\\.(\\d+)\\.(\\d+)\\/(\\d+)\\s*$/.exec(plan || '');",
    "  if (!m) { return ''; }",
    "  var o = [+m[1], +m[2], +m[3], +m[4]], L = +m[5];",
    "  if (L > 24) { return ''; }",
    "  var span = Math.pow(2, 24 - L);",
    "  var width = Math.floor(span / __bands);",
    "  if (width < 1 || __seq > width) { return '!band'; }",
    "  var top = o[0] * 65536 + o[1] * 256 + o[2];",
    "  var net = top - (top % span);",
    "  var pos = (__mix(__runh) % span + __bandIndex * width + (__seq - 1)) % span;",
    "  var v = net + pos;",
    "  return Math.floor(v / 65536) + '.' + (Math.floor(v / 256) % 256) + "
    "'.' + (v % 256) + '.0/24';",
    "}",
    "function __hextets(addr) {",
    "  var parts = String(addr).split('::');",
    "  var head = parts[0] ? parts[0].split(':') : [];",
    "  var tail = (parts.length > 1 && parts[1]) ? parts[1].split(':') : [];",
    "  var all;",
    "  if (parts.length > 1) {",
    "    var fill = 8 - head.length - tail.length;",
    "    if (fill < 0) { return null; }",
    # Развёртка `::` — явным циклом, а не `new Array(n).fill()`: песочница newman'а
    # урезана (по той же причине здесь нет `Math.imul`), и цена отказа — каждая
    # нарезка каждого набора.
    "    var mid = [];",
    "    for (var f = 0; f < fill; f++) { mid.push('0'); }",
    "    all = head.concat(mid).concat(tail);",
    "  } else { all = head; }",
    "  if (all.length !== 8) { return null; }",
    "  var v = [];",
    "  for (var i = 0; i < 8; i++) {",
    "    var n = parseInt(all[i] || '0', 16);",
    "    if (isNaN(n)) { return null; }",
    "    v.push(n & 0xffff);",
    "  }",
    "  return v;",
    "}",
    "function __carve6(plan) {",
    "  var s = String(plan || '').split('/');",
    "  if (s.length !== 2) { return ''; }",
    "  var h = __hextets(s[0]), L = parseInt(s[1], 10);",
    "  if (!h || isNaN(L) || L > 64) { return ''; }",
    "  for (var k = 0; k < 4; k++) {",
    "    var before = k * 16;",
    "    if (L >= before + 16) { continue; }",
    "    var keep = Math.max(0, L - before);",
    "    var mask = keep === 0 ? 0 : ((0xffff << (16 - keep)) & 0xffff);",
    "    h[k] = (h[k] & mask) | (__ent(k + 8) & (~mask & 0xffff));",
    "  }",
    "  return h[0].toString(16) + ':' + h[1].toString(16) + ':' + h[2].toString(16) +",
    "    ':' + h[3].toString(16) + '::/64';",
    "}",
]


def _carve_guard(plan_var: str, cidr_var: str) -> List[str]:
    """Отсутствие/непригодность плана — ОТКАЗ с именем переменной, а не тихий адрес.

    Подстановка запасного литерала здесь была бы тем же дефектом, от которого
    помощник и заводится: адрес, не связанный с планом. Поэтому запрос НЕ уходит,
    а падение называет переменную и путь её появления.
    """
    return [
        # Полоса кончилась — это НЕ «плана нет»: план на месте, набор перерос свою
        # долю. Исходов два (расширить план посева либо разбить набор), и отказ
        # обязан их различать, иначе читатель начнёт чинить не то.
        f"if ({cidr_var} === '!band') {{",
        f"  pm.test({js_str(f'FIXTURE REQUIRED: {plan_var} band exhausted')}, () => pm.expect.fail("
        "'the suite asked for carve #' + __seq + " + js_str(
            f" but its band inside {plan_var} "
            "holds fewer. The plan is split into __bands equal bands, one per suite "
            "(CIDR_BANDS in services/nlb/tests/newman/scripts/gen.py), so that two suites "
            "of one run can never carve the same /24. Widen the plan the seeder declares, "
            "or split the suite — do NOT go back to drawing the position, that is the "
            "collision this band exists to kill.") + "));",
        "  pm.execution.skipRequest();",
        "  return;",
        "}",
        f"if (!{cidr_var}) {{",
        f"  pm.test({js_str(f'FIXTURE REQUIRED: {plan_var}')}, () => pm.expect.fail(" + js_str(
            f"{plan_var} is empty or not a CIDR the suite can carve a subnet from. "
            "The seeder that creates existingNetworkId publishes the address plan it "
            "declared (deploy/scripts/seed-nlb-fixtures.sh, tests/authz-fixtures/"
            "prodseed_nlb_ext.py). Hardcoding an address instead would restore the very "
            "defect this helper exists to kill: a CIDR unrelated to the plan, which lands "
            "inside it only by coincidence.") + "));",
        "  pm.execution.skipRequest();",
        "  return;",
        "}",
    ]


def carve_cidr_pre(scope: str, v4_var: str = "_subnetCidr",
                   v6_var: Optional[str] = None) -> List[str]:
    """Pre-request: run-scoped адрес(а) подсети, вырезанные ИЗ ПЛАНА сети посева.

    `scope` разводит наборы между собой, `__seq` разводит нарезки внутри одного
    набора. Для v4 это разведение ВЫЧИСЛЯЕТСЯ (полоса набора + номер нарезки),
    поэтому столкновение невозможно по построению; для v6 остаётся хеш — там
    свободных бит 56, и предмета у запрета нет (см. шапку `CIDR_BANDS`).
    """
    if scope not in CIDR_BANDS:
        raise KeyError(
            f"gen: набор '{scope}' не имеет полосы в CIDR_BANDS. Полоса — то, чем "
            f"держится непересечение адресов между наборами одного прогона; без "
            f"записи набор делил бы полосу с соседом, и столкновение вернулось бы "
            f"молча. Заведите номер в CIDR_BANDS (services/nlb/tests/newman/"
            f"scripts/gen.py) — он же попадёт в коллекцию и будет проверен гейтом "
            f"TestCarvedSubnetsOfOneRunCanNotCollide."
        )
    out = [
        "(function () {",
        "  var __seq = parseInt(pm.environment.get('_cidrSeq') || '0', 10) + 1;",
        "  pm.environment.set('_cidrSeq', String(__seq));",
        f"  var __bandIndex = {CIDR_BANDS[scope]};",
        f"  var __bands = {len(CIDR_BANDS)};",
        "  var __runOnly = pm.environment.get('runId') || 'x0';",
        f"  var __run = __runOnly + {js_str(f'/{scope}')};",
        "  var __h = 0;",
        "  for (var i = 0; i < __run.length; i++) { __h = ((__h << 5) - __h + __run.charCodeAt(i)) | 0; }",
        "  __h = __h & 0x7fffffff;",
        # Сдвиг сетки берётся из runId БЕЗ набора: он обязан быть общим для всех
        # полос, иначе полосы разъедутся по-разному и перестанут не пересекаться.
        "  var __runh = 0;",
        "  for (var j = 0; j < __runOnly.length; j++) "
        "{ __runh = ((__runh << 5) - __runh + __runOnly.charCodeAt(j)) | 0; }",
        "  __runh = __runh & 0x7fffffff;",
        *["  " + ln for ln in _CARVE_HELPERS],
        "  var __v4 = __carve4(pm.environment.get('existingNetworkV4Plan'));",
        *["  " + ln for ln in _carve_guard("existingNetworkV4Plan", "__v4")],
        f"  pm.environment.set({js_str(v4_var)}, __v4);",
    ]
    if v6_var:
        out += [
            "  var __v6 = __carve6(pm.environment.get('existingNetworkV6Plan'));",
            *["  " + ln for ln in _carve_guard("existingNetworkV6Plan", "__v6")],
            f"  pm.environment.set({js_str(v6_var)}, __v6);",
        ]
    out.append("})();")
    return out


# ---------------------------------------------------------------------------
# Reusable assertion snippets (pm.*) — same names as kacho-vpc
# ---------------------------------------------------------------------------

def assert_status(code: int) -> List[str]:
    return [
        f"pm.test({js_str(f'status {code}')}, () => pm.expect(pm.response.code).to.eql({code}));",
    ]


def assert_grpc_code(code: int, code_name: str) -> List[str]:
    return [
        f"pm.test({js_str(f'grpc code {code} ({code_name})')}, () => {{",
        "  const j = pm.response.json();",
        f"  pm.expect(j.code, JSON.stringify(j)).to.eql({code});",
        "});",
    ]


def assert_field_violation(field_name: str) -> List[str]:
    return [
        f"pm.test({js_str(f'field violation on \"{field_name}\"')}, () => {{",
        "  const j = pm.response.json();",
        "  const det = (j.details || []).find(d => (d['@type']||'').includes('BadRequest'));",
        "  pm.expect(det, 'BadRequest detail').to.be.an('object');",
        f"  const fv = (det.fieldViolations || []).find(v => v.field === {js_str(field_name)});",
        f"  pm.expect(fv, {js_str(f'fieldViolation for {field_name}')}).to.be.an('object');",
        "});",
    ]


def assert_unscoped_rejected() -> List[str]:
    """Unscoped create/list (без projectId/parent-scope) — ОТВЕРГНУТ. Два защитимых
    исхода, оба = «отклонено» (defense-in-depth, security.md «authz-first», parity
    с vpc 446e25b / compute 32be094):
      403 PERMISSION_DENIED (code 7) — gateway scope_extractor fail-closed
        «no path: unscoped resource» ДО backend-валидации: нельзя авторизовать
        запрос без scope для anti-BOLA;
      400 INVALID_ARGUMENT  (code 3) — backend «required field» при passthrough.
    Толерантен к обоим. Techniques: ECP (класс «unscoped запрос») + error-guessing
    (authz-vs-validation ordering)."""
    return [
        "pm.test('unscoped rejected (400 InvalidArgument or 403 authz-first)', () => {",
        "  pm.expect(pm.response.code, JSON.stringify(pm.response.json())).to.be.oneOf([400, 403]);",
        "});",
        "pm.test('grpc code 3 (INVALID_ARGUMENT) or 7 (PERMISSION_DENIED)', () => {",
        "  const j = pm.response.json();",
        "  pm.expect(j.code, JSON.stringify(j)).to.be.oneOf([3, 7]);",
        "});",
    ]


def assert_absent_id_rejected() -> List[str]:
    """Negative-запрос на ОТСУТСТВУЮЩИЙ / malformed id (Get/Update/Delete или
    :verb-action / вложенный list по нему) — ОТВЕРГНУТ. Три защитимых исхода, все
    = «отклонено» (defense-in-depth, security.md «authz-first», parity с vpc):
      403 PERMISSION_DENIED (code 7) — gateway scope_extractor не может резолвить
        target→project для anti-BOLA у несуществующего/битого id → fail-closed ДО
        backend format-check / repo.Get (для МУТАЦИЙ устойчиво, id захардкожен как
        garbage — не из setup, поэтому не зависит от фикстур);
      404 NOT_FOUND (code 5) — well-formed-но-нет: sync AuthZ-Get/repo.Get;
      400 INVALID_ARGUMENT (code 3) — malformed id: corevalidate.ResourceID.
    Толерантен 400|403|404 (code 3|5|7) — семантика негатива (rejected) сохранена
    без ложного провала на корректном authz-first 403 (GATE-RUN #5:
    upd-imm/del-unknown/move-nx/stop-unknown/list-ops возвращали 403 вместо 400/404).
    Techniques: ECP (класс «absent id») + error-guessing (authz-vs-existence ordering)."""
    return [
        "pm.test('absent-id request rejected (400/403/404)', () => {",
        "  pm.expect(pm.response.code, JSON.stringify(pm.response.json())).to.be.oneOf([400, 403, 404]);",
        "});",
        "pm.test('grpc code INVALID_ARGUMENT/NOT_FOUND/PERMISSION_DENIED (3/5/7)', () => {",
        "  const j = pm.response.json();",
        "  pm.expect(j.code, JSON.stringify(j)).to.be.oneOf([3, 5, 7]);",
        "});",
    ]


def assert_refused_sync_or_async(what: str, sync_codes=(400,), async_lane: bool = True) -> List[str]:
    """The request under test is REFUSED, by whichever of the two lawful lanes decides it.

    Kachō mutations answer with an Operation (ban #9), so a refusal has two shapes and a
    case naming one input as illegal must accept both — and NOTHING else:

      * the sync validator refuses before the Operation exists → `400 INVALID_ARGUMENT`;
      * the worker refuses (a peer that does not resolve, a DB constraint, a state guard)
        → `200` carrying an Operation that finishes WITH an error.

    What this replaces was `oneOf([200, 400])` with nothing after it. Written that way the
    second lane is not checked at all: `200` alone passes, so the case is satisfied by the
    product ACCEPTING exactly the input it exists to prove refused, and a regression that
    drops the guard leaves it green. That is not tolerance of an ordering, it is the
    absence of an assertion.

    Pair this with `poll_operation_until_done(must_fail=True)` as the very next step — that
    is what states the second lane. On the sync lane `opId` is cleared here, so the poll has
    no subject and asserts nothing; on the async lane it holds the Operation to account.
    `what` names the input in the assertion text so a failure is actionable.

    `sync_codes` widens the FIRST lane where the ordering genuinely admits more than one
    refusal — the gateway can deny before the backend looks at the body (`403`), and a
    resource it cannot resolve reads as absent (`404`). Every entry must be a REFUSAL;
    `200` is supplied by this helper as the async lane and belongs nowhere else.

    `async_lane=False` — THE SECOND LANE DOES NOT EXIST FOR THIS INPUT, so naming it
    would be a promise the product cannot break. Use it only where no Operation can be
    minted at all, and say why at the call site. Measured case: an input naming a project
    that does not exist. `project_id` is the scope the edge resolves for the anti-BOLA
    check, so an unresolvable one is denied BEFORE the backend reads the body — there is
    no path on which a worker gets to refuse it later. With the lane declared anyway,
    `opId` stays unset and the paired poll addresses `{{opId}}` as a literal; the address
    guard names it, and rightly — the step had no subject. Two of nlb's fifteen red
    assertions on the production-posture run of 2026-07-31 were exactly that, and the
    other eleven users of this helper DO reach the async lane, which is why the lane is
    parameterised rather than removed.
    """
    codes = ", ".join(str(c) for c in ((*sync_codes, 200) if async_lane else sync_codes))
    named = "/".join(str(c) for c in sync_codes)
    # The title is ENCODED, not pasted. `what` comes from the caller and may legitimately
    # contain an apostrophe; pasted into a single-quoted literal it breaks the string, and
    # the whole step script stops parsing. The step then does not fail — it does not RUN:
    # `pm.test` is never reached, neither is `pm.environment.unset('opId')`, and the paired
    # poller travels on an `opId` left over from an earlier step. This is the sibling of the
    # vpc defect measured 2026-08-03 (10 unparseable scripts there); nlb is where that helper
    # was borrowed from, so it carried the same latent form with no apostrophe to fire it.
    if not async_lane:
        return [
            # `opId` СБРАСЫВАЕТСЯ В ПУСТОЕ, а не снимается.
            #
            # Снятое имя не определено ни в одной области, и страж неразрешённых
            # подстановок (см. _URL_VAR_GUARD) справедливо считает такой шаг находкой:
            # запрос ушёл бы литералом `{{opId}}`. Но здесь отсутствия Operation —
            # ОБЪЯВЛЕННЫЙ и уже проверенный исход синхронного отказа, а не промах
            # захвата. Пустое значение решает обе задачи разом: устаревший
            # идентификатор предыдущего шага не переживает шаг (ради чего снятие и
            # вводилось), имя остаётся определённым, поэтому страж до него не
            # доберётся by construction — ровно тот законный негативный случай,
            # который его собственный комментарий оговаривает, — а парный поллер
            # видит пустую строку, она ложна, и он выходит не утверждая ничего.
            "pm.environment.set('opId', '');",
            f"pm.test({json.dumps(f'{what} refused synchronously ({named}) — no Operation is minted')}, () => "
            f"  pm.expect(pm.response.code, pm.response.text()).to.be.oneOf([{codes}]));",
        ]
    return [
        "pm.environment.set('opId', '');",
        f"pm.test({json.dumps(f'{what} refused (sync {named}, or an Operation that fails)')}, () => "
        f"  pm.expect(pm.response.code, pm.response.text()).to.be.oneOf([{codes}]));",
        "if (pm.response.code === 200) {",
        "  const j = pm.response.json();",
        # An accepted mutation that hands back no Operation leaves nothing to hold to
        # account: the refusal would be asserted against a subject that does not exist.
        "  pm.test('accepted response carries the Operation that must then fail', () => "
        "    pm.expect(j.id, pm.response.text()).to.be.a('string'));",
        "  if (j.id) pm.environment.set('opId', j.id);",
        "}",
    ]


def _is_operation_id_var(env_var: str) -> bool:
    """Держит ли это имя идентификатор Operation — то есть читает ли его шаг опроса?

    Соглашение об именах едино на всё дерево: общий `opId` либо собственное имя
    кейса, оканчивающееся на `OpId`/`OperationId`. Идентификаторы РЕСУРСОВ под него
    не подпадают намеренно — их сохраняют один раз и читают много шагов спустя,
    тогда как устаревание опасно ровно у того, что потребляется следующим запросом.
    """
    return env_var == "opId" or env_var.endswith("OpId") or env_var.endswith("OperationId")


def save_from_response(jsonpath: str, env_var: str) -> List[str]:
    """Сохранить значение из response в env.

    ИДЕНТИФИКАТОР ОПЕРАЦИИ СБРАСЫВАЕТСЯ ПЕРЕД ЗАХВАТОМ — это ЗАМЕНА, а не дозапись.

    Отвергнутая мутация тела с `id` не несёт, поэтому запись ниже не происходит, и
    имя молча сохраняло бы операцию ПРЕДЫДУЩЕГО шага — как правило давно
    завершённую. Опрос подтверждает её `done`, кейс зеленеет БЫСТРО и уверенно, не
    проверив собственную мутацию вовсе.

    СБРОС В ПУСТОЕ, А НЕ СНЯТИЕ ИМЕНИ — и это выбор по порядку исполнения, а не по
    вкусу. Здесь страж неразрешённого адреса (`_URL_VAR_GUARD`) стоит в пред-скрипте
    КОЛЛЕКЦИИ, то есть исполняется раньше любого пред-скрипта шага: снятое имя он
    справедливо считает находкой и роняет опрос — включая тот, чей шаг удаления
    ОБЪЯВИЛ ветку «уже нет» законной (`oneOf([200, 404])`, 222 таких шага в дереве).
    Пустое значение решает задачу гейта (устаревший идентификатор не переживает шаг)
    и не превращает объявленный исход в отказ: имя остаётся определённым, страж до
    него не доберётся by construction, а парный опрос видит пустую строку и выходит,
    не утверждая ничего. Собственный исход мутации при этом НЕ теряется — его
    утверждает сам шаг мутации (гейт `assert-delete-steps-are-asserted.py`:
    1359 из 1359 шагов удаления несут утверждение).

    Ровно эту форму и по этой же причине уже выбрал `assert_refused_sync_or_async`.
    В iam сброс записан СНЯТИЕМ — там страж переехал в конец пред-скрипта каждого
    шага и идёт ПОСЛЕ `_op_id_guard`, который сам решает, находка это или
    санкционированный пропуск уборки.

    Разбор класса с переписью — `services/iam/tests/newman/scripts/gen.py`, где он
    был закрыт первым; гейт по дереву на обе половины пары «удаление → опрос» —
    `deploy/scripts/assert-delete-operation-outcome.py`.
    """
    reset = [f"pm.environment.set({js_str(env_var)}, '');"] if _is_operation_id_var(env_var) else []
    return [
        *reset,
        "try {",
        "  const j = pm.response.json();",
        f"  const v = ({jsonpath});",
        f"  if (v !== undefined && v !== null) pm.environment.set({js_str(env_var)}, String(v));",
        "} catch (e) {}",
    ]


def assert_operation_envelope(prefix_regex: str = "^(nlb|tgr|lst)[a-z0-9]+$") -> List[str]:
    return [
        "pm.test('Operation envelope returned', () => {",
        "  const j = pm.response.json();",
        f"  pm.expect(j.id, 'operation.id').to.match(/{js_regex_src(prefix_regex, where='nlb/assert_operation_envelope/prefix_regex')}/);",
        "  pm.expect(j.metadata, 'operation.metadata').to.be.an('object');",
        "});",
    ]


_RYA_SEQ = [0]


def retry_until_present(step: Step, id_env_var: str, budget: int = 25,
                        interval_ms: int = 500) -> Step:
    """Bounded retry a LIST step until the caller's OWN fresh resource id appears in
    the returned array (read-your-writes over the list-authz visibility window; opgate
    removed -> owner-tuple eventual-consistency). The list returns 200 with the id
    ABSENT until the tuple materializes, so retry_until_authorized (403/404) does not
    apply -- we retry while the id is missing. Fail-open after budget: the real
    assertion then runs once and FAILS if still absent (never masked, never infinite).
    Use ONLY on a list of the caller's OWN just-created resource."""
    guard = [
        "// bounded read-your-writes retry until own fresh id is present in the list",
        "// (opgate removed -> eventual-consistency); retries SELF while id absent.",
        "if (pm.environment.get('_lstRetryStarted') !== pm.info.requestName) {",
        "  pm.environment.set('_lstRetryCount', '0');",
        "  pm.environment.set('_lstRetryStarted', pm.info.requestName);",
        "}",
        "const _lrc = parseInt(pm.environment.get('_lstRetryCount') || '0', 10);",
        "let _present = false;",
        "try { const _arr = Object.values(pm.response.json()).find(v => Array.isArray(v)) || [];"
        " _present = _arr.map(x => x.id).includes(pm.environment.get('" + id_env_var + "')); } catch (e) {}",
        f"if (pm.response.code === 200 && !_present && _lrc < {budget}) {{",
        "  pm.environment.set('_lstRetryCount', String(_lrc + 1));",
        f"  const _lrd = Date.now(); while (Date.now() - _lrd < {interval_ms}) {{ /* list-visibility wait */ }}",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        "pm.environment.unset('_lstRetryCount');",
        "pm.environment.unset('_lstRetryStarted');",
    ]
    _RYA_SEQ[0] += 1
    return replace(step, name=f"{step.name}-lst{_RYA_SEQ[0]}",
                   test_script=guard + list(step.test_script))


def retry_until_authorized(step: Step, budget: int = 25, interval_ms: int = 500,
                           retry_on=(403, 404)) -> Step:
    """Wrap the FIRST access of the caller's OWN just-created resource in a bounded
    read-your-writes retry over the owner-tuple materialization window.

    opgate (the create confirm-gate) was removed by design-review: Operation.done now
    means the resource is DURABLE, but its owner/creator FGA tuple materializes
    eventually-consistent (at-least-once drainer + reconciler + sync-registrar
    optimisation). Under load the first post-create Get/Update/Delete of the fresh
    resource can briefly return 403 (PERMISSION_DENIED) or 404 at the authz gate
    before the tuple is visible. This is a textbook read-your-writes lag -> the CLIENT
    retries; it is NOT a server barrier.

    Retries the SAME request (setNextRequest -> self) while the response code is in
    `retry_on` (default 403/404), spacing attempts by ~interval_ms (busy-wait -- newman
    fires setNextRequest before any setTimeout). budget*interval_ms bounds the wait:
    with the defaults in this signature that is 25*500ms = ~12.5s.

    READ THE SIGNATURE, NOT THIS PARAGRAPH'S HISTORY. What stood here claimed
    "default 60*500ms = ~30s -- raised from 40*400ms/~16s in round 4", and that raise
    NEVER LANDED IN THIS FILE: `git log -S'budget: int = 60'` and `-S'budget: int = 40'`
    over this path are both EMPTY, and the emitted guards in collections/*.json carry
    `_arc < 25` / `_ard < 500` throughout. So for as long as the paragraph stood, these
    steps were racing a 12.5s window while every reader -- including the removed
    known-RED whitelist, which justified itself as covering "the residual saturation
    tail past ~30s" -- believed 30s. A deduction resting on a window that was never
    shipped is not a narrow exception; it is an unfalsifiable one.

    The measurement it cited is still the useful part and is kept as a measurement:
    ci-rep4 load-balancer put async op-latency at ~1.5s (poll-op p90=3) while the
    wrapped first-access materialization was p50~10s with a heavy tail. Against 12.5s
    that p50 is marginal by construction. The budget is deliberately NOT raised here to
    make that go away: a budget picked to outlast a slow materialization path converts a
    visible red into a slow green, and past the runner's own timeout into a cancelled
    run. If these steps do not converge, the finding is about the materialization path
    (nlb races LAST in the umbrella, so the fga_register_drainer backlog peaks exactly
    when nlb reads) and it belongs in docs/RESULTS.md as a number.

    fail-closed: on any other code the wrapped step's real test_script runs exactly once,
    and once the budget is spent it ALSO runs on the terminal 403/404 (a genuine,
    non-converging deny still FAILS the real assertions -- never masked, never infinite).

    Use ONLY on the first access of the caller's OWN fresh resource. Do NOT wrap
    negative / cross-account-deny / absent-id steps (a poll there would mask a real
    deny). The counter/started env-vars are request-name-scoped (step names are
    globally unique after serialization) so the loop never bleeds across cases or
    steps -- same discipline as poll_operation_until_done.

    ГРАНИЦА, КОТОРУЮ ЭТА ОБЁРТКА НЕ ПЕРЕХОДИТ (issue #351). Решение о повторе
    принимается по КОДУ ОТВЕТА шага, поэтому закрыта ровно одна полоса — СИНХРОННЫЙ
    отказ этого запроса (шлюз не разрешил цель проверки прав → 403; чтение скрытого
    ресурса → 404). У АСИНХРОННОЙ мутации отказ ВЛАДЕЛЬЦА чужого ресурса приезжает
    иначе: шаг отвечает `200` и конвертом `Operation`, а отказ лежит терминальной
    ошибкой ВНУТРИ операции и читается уже другим шагом — сюда он не попадает НИКОГДА.
    Обёртка на таком шаге не инертна (полосу шлюза она по-прежнему закрывает), но
    читать её как «окно видимости здесь закрыто» — ошибка: чужой свежий идентификатор
    обязан быть либо ПРОЧИТАН до мутации (`retry_until_authorized(GET <владелец>/<id>)`
    — форма, посаженная PR #350), либо прикрыт повтором по ИСХОДУ операции
    (`poll_operation_until_done(retry_from=…)`). Свойство держит по всему дереву гейт
    `internal/repohygiene/artifactgates`
    `TestAsyncMutationDoesNotCarryAnUnwarmedPeerId`.
    """
    # Полоса, по которой приходит окно видимости, задаётся МЕТОДОМ, а не вкусом
    # автора: у мутации отказ виден как 403, а у ЧТЕНИЯ он спрятан под 404
    # (hide-existence: текст отказа побайтово равен настоящему «не найдено», см.
    # `security.md`). Поэтому рукописное `retry_on=(403,)` на GET — обёртка,
    # которая не может сработать на том коде, который она увидит: форма есть,
    # содержания нет. Такое место в дереве нашлось (vpc1 `get-no-dhcp`: 404 на
    # первом же обращении, ретрай не сработал ни разу, шаг упал). Чинится по
    # построению здесь, а не перечнем в кейсах; 404, названный шагом законным
    # исходом, в retry_on не попадает вовсе — его отсекает вызывающий.
    # Ожидание НИКОГДА не включает код, который шаг сам объявил приемлемым
    # исходом: иначе проба пережидала бы ровно то, ради чего написана, и жгла бы
    # бюджет на успехе. Нормализация здесь, а не у вызывающих: рукописные
    # обёртки этого не делали (5 мест в дереве ждали заявленный ими же 404).
    _acc = _accepted_http_codes("\n".join(step.test_script))
    retry_on = tuple(c for c in retry_on if c not in _acc)
    if step.method == "GET" and 404 not in retry_on and 404 not in _acc:
        retry_on = tuple(retry_on) + (404,)
    # То же и у УДАЛЕНИЯ, и по той же причине — только код приходит не из
    # hide-existence чтения, а из утверждения по умолчанию: шаг удаления без
    # собственного утверждения получит при сериализации `delete accepted: status 200`
    # (_DELETE_ACCEPTED), и 404 для него — падение, а не законное «уже нет».
    # Держим это ЗДЕСЬ, а не у вызывающих: рукописное `retry_on=(403,)` на таком шаге
    # даёт обёртку, которая не ждёт единственный код, на котором шаг упадёт, — форма
    # ожидания есть, содержания нет. 404, названный шагом законным исходом, сюда
    # по-прежнему не попадает: его отсекает `_acc`.
    if (step.method == "DELETE" and not _carries_assertion(list(step.test_script))
            and 404 not in retry_on and 404 not in _acc):
        retry_on = tuple(retry_on) + (404,)
    if not retry_on:
        # Ждать нечего: все коды полосы видимости объявлены исходами. Обёртка
        # выродилась бы в петлю, которая не может сработать, — не ставим её.
        return step
    # ЗДЕСЬ ЖЕ — граница, которую этот отказ ставить обёртку НЕ ловит (issue #351).
    # Условие выше отсекает вырожденный случай «ждать нечего», и это верно только
    # для полосы, ВИДИМОЙ КОДОМ ОТВЕТА. У асинхронной мутации есть вторая полоса —
    # отказ ВЛАДЕЛЬЦА чужого ресурса внутри `Operation`, — и она не видна отсюда ни
    # при каком `retry_on`: обёртка ставится, читается как закрытое окно и им не
    # является. Молча этот случай не проходит: эмитируемый комментарий ниже называет
    # свою полосу вслух, а свойство по дереву держит гейт
    # `internal/repohygiene/artifactgates` TestAsyncMutationDoesNotCarryAnUnwarmedPeerId.
    retry_set = ",".join(str(c) for c in retry_on)
    guard = [
        "// bounded read-your-writes retry over the owner-tuple materialization window",
        "// (opgate removed -> eventual-consistency); retries SELF only on 403/404.",
        "// ПОЛОСА — синхронный отказ ЭТОГО шага. У асинхронной мутации отказ ВЛАДЕЛЬЦА",
        "// приезжает внутри Operation и сюда НЕ попадает: нужен прогрев чтением.",
        "if (pm.environment.get('_authRetryStarted') !== pm.info.requestName) {",
        "  pm.environment.set('_authRetryCount', '0');",
        "  pm.environment.set('_authRetryStarted', pm.info.requestName);",
        "}",
        "const _arc = parseInt(pm.environment.get('_authRetryCount') || '0', 10);",
        f"if ([{retry_set}].includes(pm.response.code) && _arc < {budget}) {{",
        "  pm.environment.set('_authRetryCount', String(_arc + 1));",
        f"  const _ard = Date.now(); while (Date.now() - _ard < {interval_ms}) {{ /* owner-tuple materialization wait */ }}",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        "pm.environment.unset('_authRetryCount');",
        "pm.environment.unset('_authRetryStarted');",
    ]
    _RYA_SEQ[0] += 1
    # Give the wrapped step a globally-unique name so its self-retry
    # setNextRequest(pm.info.requestName) always resolves to ITSELF. Newman resolves a
    # setNextRequest name to the FIRST item with that name in the collection; these
    # suites mostly do NOT prefix step names by case-id, so a wrapped step whose bare
    # name repeats would otherwise jump the retry to an earlier same-named step — the
    # exact hazard poll_operation_until_done avoids via its unique poll-op-<n> name.
    return replace(step, name=f"{step.name}-rya{_RYA_SEQ[0]}",
                   test_script=guard + list(step.test_script))


def warm_peer_fixture(base_path: str, id_var: str, suffix: str,
                      auth: Optional[str] = None) -> Step:
    """Прогреть ЧУЖОЙ свежий ресурс чтением — до того, как его идентификатор уедет в
    асинхронную мутацию этого набора.

    Зачем (issue #351). `retry_until_authorized` решает о повторе по КОДУ ОТВЕТА шага,
    а асинхронная мутация отвечает `200` и конвертом `Operation` ВСЕГДА: отказ владельца
    чужого ресурса приезжает терминальной ошибкой ВНУТРИ операции и читается уже другим
    шагом. Значит на такой мутации окно материализации прав у ЧУЖОГО идентификатора не
    закрыто ничем — обёртка на ней закрывает только полосу шлюза (собственную цель
    проверки прав этого RPC).

    Отличить открытое окно от настоящего промаха кейс не может by construction: владелец
    прячет существование и берёт текст из общей таблицы (`security.md` §6), поэтому
    красное приезжает как утверждение о продукте — ровно так и было прочитано в разборе,
    из которого заведён #351.

    Греется ЧТЕНИЕМ, потому что на нём обёртка работает: 403/404 видны кодом ответа,
    ожидание ограничено бюджетом, и ничего не маскируется — постоянный отказ по-прежнему
    роняет посев, и роняет его НА СВОЁМ шаге, а не через три шага на чужом.

    Второй законный исход — повтор по ИСХОДУ операции
    (`poll_operation_until_done(retry_from=…)`); он дороже (перезапускает саму мутацию) и
    применяется там, где чтение чужого ресурса кейсу недоступно.

    Свойство держит гейт по дереву `internal/repohygiene/artifactgates`
    `TestAsyncMutationDoesNotCarryAnUnwarmedPeerId`.
    """
    return retry_until_authorized(Step(
        name=f"warm-{suffix}", method="GET",
        path=f"{base_path}/{{{{{id_var}}}}}", auth=auth,
        test_script=[*assert_status(200)]))


def retry_until_state(step: Step, converged_expr: str, budget: int = 25,
                      interval_ms: int = 500, retry_on=(403, 404)) -> Step:
    """Wrap the FIRST post-mutation / after-op VERIFY of the caller's OWN fresh resource
    in a bounded read-your-writes retry until the OBSERVED STATE has CONVERGED.

    Operation.done means the mutated resource is DURABLE (api-conventions.md), but the
    read that verifies a specific post-mutation FIELD VALUE can briefly be transient in
    TWO ways: (a) the owner-tuple authz gate returns 403/404 before the tuple
    materialises, OR (b) the read returns 200 but with a STALE field value before the
    write is reflected on the read path (e.g. a PATCH'd field / a status that settles a
    beat after the Operation is durable). retry_until_authorized covers only (a); this
    helper covers BOTH — it retries SELF while the response is a transient 403/404 OR a
    200 whose `converged_expr` (a JS boolean, TRUE once the expected state is observed)
    is still false, spacing attempts by ~interval_ms (busy-wait — newman fires
    setNextRequest before any setTimeout).

    Fail-OPEN at the budget: once spent, the wrapped step's real asserts run exactly once
    on the terminal response — a genuine never-converging state (a real product bug) STILL
    FAILS (never masked, never infinite). Use ONLY on a POSITIVE verify of the caller's
    OWN fresh resource — NEVER a negative / cross-account-deny / absent-id read (a poll
    there would mask a real deny). It is a strict superset of retry_until_authorized:
    converting an authz-only wrap to this never hides anything the authz-retry caught, it
    only ADDS the state-convergence wait. Unique step name (`-st<n>`) keeps the self-retry
    setNextRequest unambiguous (same discipline as retry_until_authorized / poll-op)."""
    retry_set = ",".join(str(c) for c in retry_on)
    guard = [
        "// bounded read-your-writes retry until the caller's OWN post-mutation state converges",
        "// (eventual-consistency): retries SELF on transient 403/404 OR a 200 whose state has",
        "// not yet caught up. Fail-open at budget -> the real asserts run once and FAIL if",
        "// still unconverged (a genuine never-converging state is never masked).",
        "if (pm.environment.get('_stRetryStarted') !== pm.info.requestName) {",
        "  pm.environment.set('_stRetryCount', '0');",
        "  pm.environment.set('_stRetryStarted', pm.info.requestName);",
        "}",
        "const _stc = parseInt(pm.environment.get('_stRetryCount') || '0', 10);",
        "let _converged = false;",
        f"try {{ _converged = !!({converged_expr}); }} catch (e) {{ _converged = false; }}",
        f"const _stTransient = [{retry_set}].includes(pm.response.code) || (pm.response.code === 200 && !_converged);",
        f"if (_stTransient && _stc < {budget}) {{",
        "  pm.environment.set('_stRetryCount', String(_stc + 1));",
        f"  const _std = Date.now(); while (Date.now() - _std < {interval_ms}) {{ /* state-convergence wait */ }}",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        "pm.environment.unset('_stRetryCount');",
        "pm.environment.unset('_stRetryStarted');",
    ]
    _RYA_SEQ[0] += 1
    return replace(step, name=f"{step.name}-st{_RYA_SEQ[0]}",
                   test_script=guard + list(step.test_script))


def retry_create_until_present(step: Step, budget: int = 25, interval_ms: int = 500) -> Step:
    """Wrap a CREATE/POST step that references a peer resource (e.g. a vpc Subnet /
    Address) just provisioned inline in the SAME case, in a bounded read-your-writes
    retry over the *cross-service* visibility window.

    A subnet/address created through vpc returns its Operation done (durable in vpc),
    but the peer read on nlb's side (nlb -> vpc SubnetService.Get during LB/Listener
    Create) is briefly stale under load: the sync create rejects with
    InvalidArgument/NotFound `"subnet <id> not found"` (code 3/5) before vpc's write is
    visible to the nlb peer client. Confirmed under `--jobs 4` parallel collections
    (ci-rep2: placement-coherence create-same-zone/-region + INTERNAL-REGIONAL cr-internal
    reddened on `subnet <id> not found`, while the identical provision->poll->create
    pattern in cross-resource happened to win the race and stayed green). This is a
    textbook cross-service read-your-writes lag -> the CLIENT retries the create; it is
    NOT a server barrier.

    Retries the SAME request (setNextRequest -> self) while the response says the peer
    reference did not resolve, spacing attempts ~interval_ms (busy-wait -- newman fires
    setNextRequest before any setTimeout). TWO discriminators, and the difference between
    them matters:

      * `ErrorInfo.reason == 'PEER_RESOURCE_MISSING'` in `details[]` -- the MACHINE
        discriminator api-conventions.md mandates ("клиент машинно различает линии по
        reason-token ... НЕ парся прозу message"). nlb emits it on the linked-address
        lane, whose prose is deliberately generic anti-oracle text
        ("Illegal argument addressId") and therefore carries no sniffable substring;
      * the legacy prose test (400/404 whose body message contains 'not found') -- the
        subnet lane still answers `"subnet <id> not found"` and is matched by prose only.

    Prose sniffing alone was NOT enough, and that is the whole reason the token branch
    exists: an address whose owner-tuple had not materialised answered with the generic
    text, matched nothing, and the wrapped step burned its single attempt ~0.5s after the
    address was created (CI 30919903252: cr-link / cr-ext-link / cr-linked, one attempt
    each, zero retries).

    A rejected create allocates NOTHING (sync reject before the Operation is even minted),
    so re-POSTing is leak-free and idempotent. budget*interval_ms bounds the wait
    (default 25*500ms = ~12.5s) -- fail-closed: on any other outcome the wrapped step's
    real test_script runs exactly once, and once the budget is spent it ALSO runs on the
    terminal rejection (a genuinely-absent peer still FAILS the real assertions --
    never masked, never infinite).

    Terminal rejections stay terminal by construction: nlb attaches the token ONLY to the
    peer-miss lane, so ownership/family/kind/placement mismatches (same generic prose,
    code 3, no token) are not retried and their negative cases keep failing fast.

    Use ONLY on a create whose peer dependency was provisioned earlier in the SAME case.
    Do NOT wrap negative fixture-absent creates (they legitimately expect the rejection).
    """
    guard = [
        "// bounded read-your-writes retry over the cross-service peer-visibility window",
        "// (vpc subnet/address just provisioned; nlb peer-read briefly stale). Retries",
        "// SELF only while the sync create says the PEER REFERENCE did not resolve:",
        "//   - ErrorInfo.reason == PEER_RESOURCE_MISSING  (machine discriminator; the",
        "//     linked-address lane's prose is generic anti-oracle text on purpose), or",
        "//   - a 400/404 whose message contains 'not found' (subnet lane prose).",
        "// A terminal mismatch carries NEITHER, so negatives still fail on attempt one.",
        "if (pm.environment.get('_crRetryStarted') !== pm.info.requestName) {",
        "  pm.environment.set('_crRetryCount', '0');",
        "  pm.environment.set('_crRetryStarted', pm.info.requestName);",
        "}",
        "const _crc = parseInt(pm.environment.get('_crRetryCount') || '0', 10);",
        "let _crNotFound = false;",
        "try {",
        "  const _crb = pm.response.json();",
        "  const _crPeerMiss = (_crb.details || []).some(d =>"
        " d && d.reason === 'PEER_RESOURCE_MISSING');",
        "  _crNotFound = _crPeerMiss || ([400, 404].includes(pm.response.code)"
        " && /not found/i.test(_crb.message || ''));",
        "} catch (e) {}",
        f"if (_crNotFound && _crc < {budget}) {{",
        "  pm.environment.set('_crRetryCount', String(_crc + 1));",
        f"  const _crd = Date.now(); while (Date.now() - _crd < {interval_ms}) {{ /* peer-visibility wait */ }}",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        "pm.environment.unset('_crRetryCount');",
        "pm.environment.unset('_crRetryStarted');",
    ]
    _RYA_SEQ[0] += 1
    return replace(step, name=f"{step.name}-cr{_RYA_SEQ[0]}",
                   test_script=guard + list(step.test_script))


def retry_delete_until_released(step: Step, budget: int = 60, interval_ms: int = 500) -> Step:
    """Обернуть УБОРКУ подсети в ограниченный повтор на время освобождения адресов.

    Удаление балансировщика отвечает Operation `done` — предмет мутации долговечен. Но
    возврат выделенных адресов в свободный список идёт СЛЕДОМ и асинхронно (см.
    data-integrity.md §lease-recycle-on-delete), поэтому подсеть в узком окне ещё
    держит их и синхронно отвергает своё удаление: `FAILED_PRECONDITION` с текстом
    "Subnet has allocated internal addresses".

    Это ЗАКОННЫЙ временный отказ, а не дефект: кейс не про него, а про совпадение зон.
    Наблюдался в шарде nlb, где уборка v6-подсети падала ПОСЛЕ успешного удаления
    балансировщика (первое исполнение 200, второе — 400 с этим текстом).

    ПОЧЕМУ ЭТО НЕ ОСЛАБЛЕНИЕ. Повтор различается ПО СУЩЕСТВУ отказа, а не по коду:
    сообщение конкретно и производится ровно одной причиной. Бюджет ограничен, и по его
    исчерпании настоящее утверждение шага исполняется РОВНО ОДИН раз на терминальном
    ответе — подсеть, которая не освобождается никогда (настоящий дефект пула), всё
    равно уронит кейс. Ни маскировки, ни бесконечного ожидания.
    """
    guard = [
        "// SELF-повтор, пока подсеть держит ещё не возвращённые адреса: удаление",
        "// балансировщика долговечно, а возврат аренды в пул идёт следом и асинхронно.",
        "// Любой ДРУГОЙ отказ терминален и роняет шаг с первой попытки.",
        "if (pm.environment.get('_dlRetryStarted') !== pm.info.requestName) {",
        "  pm.environment.set('_dlRetryCount', '0');",
        "  pm.environment.set('_dlRetryStarted', pm.info.requestName);",
        "}",
        "const _dlc = parseInt(pm.environment.get('_dlRetryCount') || '0', 10);",
        "let _dlHeld = false;",
        "try {",
        "  const _dlb = pm.response.json();",
        "  _dlHeld = [400, 409].includes(pm.response.code)"
        " && /allocated internal addresses/i.test(_dlb.message || '');",
        "} catch (e) {}",
        f"if (_dlHeld && _dlc < {budget}) {{",
        "  pm.environment.set('_dlRetryCount', String(_dlc + 1));",
        f"  const _dld = Date.now(); while (Date.now() - _dld < {interval_ms}) {{ /* lease-release wait */ }}",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        "pm.environment.unset('_dlRetryCount');",
        "pm.environment.unset('_dlRetryStarted');",
        # ДИАГНОСТИКА ТЕРМИНАЛЬНОГО ИСХОДА. Без неё падение уборки печатает только
        # «delete accepted: status 200» — то есть НЕ говорит ни кода, ни сообщения,
        # и разбор упирается в незнание того, что ответил сервер. Именно так и вышло
        # в шарде nlb: чинить пришлось бы гадая, временный это отказ (аренда ещё не
        # вернулась, окно мало) или иной по существу (тогда повтор вообще не при чём
        # и расширять бюджет — значит прятать другой дефект).
        #
        # Печатается ТОЛЬКО на не-200: на успехе шум не нужен. Это не утверждение и
        # ничего не смягчает — само строгое `delete accepted` остаётся ниже и падает
        # как падало.
        "if (pm.response.code !== 200) {",
        "  let _dlm = '';",
        "  try { _dlm = (pm.response.json().message || '').slice(0, 200); } catch (e) { _dlm = '<не JSON>'; }",
        "  console.log('[cleanup] ' + pm.info.requestName + ': код ' + pm.response.code"
        " + ', повторов ' + _dlc + ', сообщение: ' + _dlm);",
        "}",
    ]
    _RYA_SEQ[0] += 1
    return replace(step, name=f"{step.name}-dl{_RYA_SEQ[0]}",
                   test_script=guard + list(step.test_script))


def poll_operation_until_done(
    fixture_ids: Optional[List[str]] = None,
    must_succeed: bool = False,
    must_fail: bool = False,
    retry_from: Optional[str] = None,
    retry_when: str = "not found",
    retry_budget: int = 8,
    retry_interval_ms: int = 700,
    auth: Optional[str] = None,
) -> Step:
    """Reusable poll step with up-to-30 setNextRequest retries spaced ~500ms apart;
    guards on empty opId. Budget*interval ≈ 15s covers the async-op tail instead of
    hammering back-to-back (~15ms/poll) which never waits for the op (Koren #1).

    Each emitted step carries a unique name (`poll-op-<n>`) so the
    setNextRequest self-retry is unambiguous under `newman run <collection>`
    (see `_poll_seq` note): a duplicate "poll-op" name would make newman resolve
    the retry jump to the first such step and skip intervening folders.

    `fixture_ids` — PHANTOM-ID GUARD (testing.md: «Fixture-seed обязан проверять
    `op.error` перед извлечением resource-id из `metadata`»). A Kachō Operation
    carries the PRE-ALLOCATED resource id in `metadata` even when it finishes
    `done:true` WITH an `error` (the id is minted before the async worker runs), so
    a create step that stores `metadata.<res>Id` unconditionally publishes the id of
    a resource that does NOT exist. Downstream steps then address a phantom: the
    gateway scope_extractor cannot resolve target→project and answers 403, or the
    backend answers 404 — a cascade whose symptom (authz denial) has nothing to do
    with its cause (a failed create). Naming the env vars a create step published
    makes this poll UNSET them on `op.error` and FAIL right here, attributably.
    Use ONLY for fixture creates that must succeed — never where an op error is the
    asserted outcome.

    БОЛЬШЕ НЕ ЕДИНСТВЕННЫЙ НОСИТЕЛЬ ЭТОЙ ЗАЩИТЫ И НЕ ОБЯЗАТЕЛЬНЫЙ. Ручная пометка
    неотличима от решения не помечать, и класс возвращался ровно так — закрыт в
    одном кейсе, через несколько часов проявился в соседнем. Теперь то же свойство
    ставит ПО СВОЙСТВУ шага проход `_assert_published_id_outcome` (ниже), и держит
    его по всему дереву гейт `internal/repohygiene`
    `TestPublishedResourceIdIsGuardedByOperationOutcome`. Явное перечисление
    остаётся законным — проход видит уже названный исход и ничего не дописывает, —
    но забыть его теперь не значит остаться без защиты.

    `must_succeed` — same statement WITHOUT the unset: assert this Operation finished
    without an error, while leaving the env intact so the case's own cleanup still
    addresses a real id. For the MUTATION UNDER TEST (a drain toggle, a label update)
    there is no fixture id to withdraw, but the outcome still has to be stated: the
    alternative that was in the tree is a downstream `if (!lastOpError)`, which converts
    "the mutation failed" into "the assertion about it did not run" — the case then
    greens on the miss. `fixture_ids` implies `must_succeed`.

    `must_fail` — the MIRROR statement, for a case whose subject is a REFUSAL decided by
    the worker rather than by the sync validator (a peer that does not resolve, a DB
    constraint, a state guard). There the lawful shapes are `400` before the Operation
    exists, or `200` and an Operation that finishes WITH an error — and `200` on its own is
    neither. A bare `poll_operation_until_done()` after such a step asserts only `done`,
    which a SUCCESSFUL operation satisfies just as well, so the refusal the case is named
    for is never checked: the delete that was supposed to be blocked goes through and the
    case still greens. This asserts the error is there. Never use it where acceptance is
    legitimate — it would then fail on correct behaviour.

    `retry_from` — bounded ASYNC-LANE read-your-writes re-drive. `retry_create_until_present`
    only sees the SYNC response of a create, so it covers a peer miss that is rejected
    before the Operation is minted. The very same cross-service window can instead land
    on the WORKER (peer visible to the sync precheck, still stale to the worker's own
    peer call) — the create then returns 200 and the Operation fails afterwards.
    Re-drives the named create step while the op error message matches `retry_when`
    (default `not found` — the peer-visibility discriminator, NOT capacity: an
    exhausted pool answers with the opaque capacity text and is never retried).
    Fail-open on budget: the `fixture_ids` assertion below then runs and FAILS.
    """
    global _poll_seq
    _poll_seq += 1
    ids = list(fixture_ids or [])
    script = [
        # Nothing to poll: the preceding step was refused synchronously and minted no
        # Operation. This is the ONLY case in which the step asserts nothing.
        "if (!pm.environment.get('opId')) {",
        "  pm.environment.unset('_pollCount');",
        "  return;",
        "}",
        # A REFUSED OPERATION READ IS RED. The early return used to sit ABOVE this line,
        # so the assertion could not fail by construction — only the responses it already
        # accepts ever reached it. Any non-200 on GET /operations/{id} (403 on somebody
        # else's operation, 404, 5xx) counted as a passed step while the outcome of the
        # mutation the step exists for stayed UNKNOWN. Unknown is not the same as fine.
        "pm.test('poll status 200', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
        # The assertion above has already said no; leave before json() throws on an error
        # body — a script exception would hide the very failure it reports (and lands in
        # the fourth outcome category of assert-suites-green.sh, not in `failed`).
        "if (pm.response.code !== 200) {",
        "  pm.environment.unset('_pollCount');",
        "  return;",
        "}",
        "const j = pm.response.json();",
        "const pc = parseInt(pm.environment.get('_pollCount') || '0', 10);",
        # Poll budget raised 6→30 to match the Koren-1 baseline of the other
        # suites; with the ~500ms inter-poll delay below this covers ~15s.
        "if (!j.done && pc < 30) {",
        "  pm.environment.set('_pollCount', String(pc + 1));",
        # Real inter-poll delay (~500ms) between retries. newman runs test scripts
        # synchronously and fires setNextRequest before any setTimeout callback, so a
        # busy-wait is the only way to actually space out polls; 30*0.5s ≈ 15s then
        # covers the async-op tail (p95 3s / max 10s) instead of hammering back-to-back
        # (~15ms/poll via --delay-request 15) which never waits for the op (Koren #1).
        "  const _pd = Date.now(); while (Date.now() - _pd < 500) { /* inter-poll delay ~500ms (Koren #1) */ }",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        "pm.environment.unset('_pollCount');",
        "pm.test('operation done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
        "if (j.error) pm.environment.set('lastOpError', JSON.stringify(j.error));",
        "else pm.environment.unset('lastOpError');",
        "if (j.response) pm.environment.set('lastOpResponse', JSON.stringify(j.response));",
    ]

    if ids:
        # Phantom-id guard: a pre-allocated metadata id whose op FAILED never
        # reaches a downstream step.
        script += [
            "if (j.error) {",
            *[f"  pm.environment.unset({js_str(v)});" for v in ids],
            "}",
        ]

    if retry_from:
        script += [
            "// bounded ASYNC-lane read-your-writes re-drive of the create step: the",
            "// peer was visible to the sync precheck but still stale to the worker's",
            "// own peer call, so the failure surfaced on the Operation, not on the",
            "// create response. Discriminated on the peer-miss text — a capacity",
            "// refusal reads differently and is NEVER retried.",
            "if (pm.environment.get('_opRedriveStarted') !== pm.info.requestName) {",
            "  pm.environment.set('_opRedriveCount', '0');",
            "  pm.environment.set('_opRedriveStarted', pm.info.requestName);",
            "}",
            "const _orc = parseInt(pm.environment.get('_opRedriveCount') || '0', 10);",
            "let _opTransient = false;",
            f"try {{ _opTransient = !!j.error && /{js_regex_src(retry_when, where='nlb/poll_operation_until_done/retry_when', flags='i')}/i.test(j.error.message || ''); }} catch (e) {{}}",
            f"if (_opTransient && _orc < {retry_budget}) {{",
            "  pm.environment.set('_opRedriveCount', String(_orc + 1));",
            # The create step's OWN sync-retry counter is keyed on its request name and
            # only resets when the name changes; re-entering the same step would find a
            # spent budget. Clear it so each re-drive gets a full sync-retry window.
            "  pm.environment.unset('_crRetryStarted');",
            "  pm.environment.unset('_crRetryCount');",
            "  pm.environment.unset('opId');",
            f"  const _ord = Date.now(); while (Date.now() - _ord < {retry_interval_ms}) {{ /* peer-visibility wait */ }}",
            f"  pm.execution.setNextRequest({js_str(retry_from)});",
            "  return;",
            "}",
            "pm.environment.unset('_opRedriveCount');",
            "pm.environment.unset('_opRedriveStarted');",
        ]

    if must_fail and (ids or must_succeed):
        raise ValueError("poll_operation_until_done: must_fail contradicts must_succeed/fixture_ids")

    if ids:
        script += [
            "pm.test('fixture operation succeeded (no phantom resource id)', () => "
            "  pm.expect(j.error, JSON.stringify(j.error || {})).to.be.undefined);",
        ]
    elif must_succeed:
        script += [
            "pm.test('operation succeeded', () => "
            "  pm.expect(j.error, JSON.stringify(j.error || {})).to.be.undefined);",
        ]
    elif must_fail:
        script += [
            "pm.test('operation refused the request (carries an error)', () => "
            "  pm.expect(j.error, JSON.stringify(j)).to.be.an('object'));",
        ]

    return Step(
        name=f"poll-op-{_poll_seq}",
        method="GET",
        path="/operations/{{opId}}",
        test_script=script,
        # THE POLL MUST BE THE SAME PRINCIPAL AS THE MUTATION IT POLLS.
        # Reading an Operation is owner-scoped by design: the predicate is the
        # creator's (principal_type, principal_id) in SQL, and a stranger gets the
        # same no-leak 404 as for an id that does not exist. So a step performed
        # under `auth="<subject>"` followed by a poll left on the collection default
        # asks a DIFFERENT subject about that operation, and the honest 404 reads as
        # "the operation vanished". Measured 2026-07-30: a service account created a
        # load balancer (200, Operation minted) and the two polls that followed came
        # back 404 — the case looked like a product defect and was a mislaid identity.
        auth=auth,
    )


def http_method_not_allowed_block(prefix: str, base_path: str) -> List[Case]:
    """HTTP method semantics: PUT/DELETE on collection endpoint → not-allowed status."""
    return [
        Case(
            id=f"{prefix}-METHOD-PUT-NOT-ALLOWED",
            title="PUT on List endpoint → 403/404/405/501",
            classes=["VAL", "NEG"], priority="P3",
            steps=[Step(name="put-list", method="PUT", path=base_path,
                        body={"projectId": "{{_suiteProjectId}}"},
                        test_script=["pm.test('not allowed (403/404/405/501)', () => pm.expect(pm.response.code).to.be.oneOf([403, 404, 405, 501]));"])],
        ),
        Case(
            id=f"{prefix}-METHOD-DELETE-LIST",
            title="DELETE on List endpoint (no id) → 403/404/405/501",
            classes=["VAL", "NEG"], priority="P3",
            steps=[Step(name="del-list", method="DELETE", path=base_path,
                        test_script=["pm.test('not allowed (403/404/405/501)', () => pm.expect(pm.response.code).to.be.oneOf([403, 404, 405, 501]));"])],
        ),
    ]


def conf_alreadyexists_block(prefix: str, create_path: str, name_template: str,
                              body_extra: Optional[Dict] = None,
                              id_field_pattern: str = "Id") -> Case:
    """CONF: duplicate (project_id, name) on Create returns ALREADY_EXISTS verbatim text.

    NLB pattern: sync 409 on duplicate name (partial UNIQUE in DB). Worker also returns
    error envelope if INSERT race wins both syncs.

    ОТКАЗ ЗДЕСЬ РОВНО ОДИН, И ОН СИНХРОННЫЙ. Проверка дубликата имени стоит в use-case'е
    ДО создания строки операции (`assertNameUnique` в loadbalancer/create.go и
    targetgroup/create.go), поэтому второй Create получает `AlreadyExists` немедленно —
    409, а не конверт операции. Асинхронная полоса в этом кейсе недостижима: первый
    Create дождался `done`, значит его строка закоммичена, значит предпроверка второго её
    видит. Insert в worker'е остаётся атомарным backstop'ом на состязание двух
    ОДНОВРЕМЕННЫХ создателей — но серийный кейс такого состязания не ставит.

    Прежде шаг принимал `oneOf([200, 409])`. Первая редакция утверждала что-либо только в
    ветке 409, поэтому принятый дубль проходил кейс, названный «duplicate → ALREADY_EXISTS».
    Вторая редакция это починила, потребовав ошибку в операции (`must_fail`), — но
    сохранила приём двух исходов там, где код даёт один, и тем оставляла кейс глухим к
    настоящему регрессу: перенос проверки из синхронной части в worker'а сменил бы полосу
    отказа, а утверждение молчало бы. Теперь полоса заявлена: 409, код 6, текст."""
    body_extra = body_extra or {}
    return Case(
        id=f"{prefix}-CR-CONF-ALREADY-EXISTS",
        title=f"Create duplicate name → sync 409 ALREADY_EXISTS verbatim text",
        classes=["CONF", "NEG", "IDEM"], priority="P1",
        steps=[
            Step(name="create-first", method="POST", path=create_path,
                 body={"projectId": "{{_suiteProjectId}}", "name": name_template, **body_extra},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                              *save_from_response(
                                  "(j.metadata && Object.keys(j.metadata).filter(k => k.endsWith('Id') && k !== 'projectId').map(k => j.metadata[k])[0]) || ''",
                                  "createdId")]),
            # must_succeed: предмет кейса — отказ ВТОРОГО создания, и он имеет смысл
            # только если первое действительно создало строку. Сорванная подготовка
            # обязана краснеть здесь, под своим именем, а не превращать 409 ниже в
            # необъяснимый 200.
            poll_operation_until_done(must_succeed=True),
            Step(name="create-dup", method="POST", path=create_path,
                 body={"projectId": "{{_suiteProjectId}}", "name": name_template, **body_extra},
                 test_script=[
                     # Снимаем opId предыдущей (удавшейся) операции: синхронный отказ
                     # новой операции не создаёт, и поллинг ниже не должен опросить чужую.
                     "pm.environment.unset('opId');",
                     *assert_status(409),
                     *assert_grpc_code(6, "ALREADY_EXISTS"),
                     "pm.test('mentions already exists', () => pm.expect(pm.response.json().message.toLowerCase()).to.include('already exists'));",
                 ]),
            Step(name="cleanup-first", method="DELETE", path=f"{create_path}/{{{{createdId}}}}",
                 test_script=[*save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
        ],
    )


# ---------------------------------------------------------------------------
# Postman v2.1 serialization
# ---------------------------------------------------------------------------

def _auth_pre_script(auth: str) -> List[str]:
    if auth == "anonymous":
        return [
            "// AZD per-step: anonymous (strip Authorization header)",
            "pm.request.headers.remove('Authorization');",
        ]
    return [
        f"// AZD per-step: bearer from env '{js_comment(auth)}'",
        f"const __t = pm.environment.get({js_str(auth)}) || pm.variables.get({js_str(auth)}) || '';",
        "if (__t) {",
        "  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __t});",
        "} else {",
        # HARNESS-CONFIG GUARD. An `auth="<envVar>"` step names a SUBJECT the case
        # is about ("a never-granted user must see nothing", "a foreign editor must
        # be denied"). If the fixture seed never wrote that variable, silently
        # dropping the header does not skip the check — it runs it as ANONYMOUS,
        # against a different subject entirely. The typical expectation (401/403)
        # then still holds, so the case passes FOR THE WRONG REASON and the subject
        # under test is never exercised. Missing subject = misconfigured harness:
        # FAIL naming the variable, THEN SKIP — the sanctioned shape, declared by
        # services/iam/tests/newman/scripts/exec-coverage.py (section STATIC BANS) and
        # implemented there as gen.py::require_env_url; this generator carries no such
        # helper, so the shape is written out here. Dropping the header and sending
        # anyway is NOT the
        # sanctioned shape: the step still travels, and every OTHER assertion it
        # carries is then scored against a principal the case never named.
        # `pm.execution.skipRequest()` skips exactly one request — its test script
        # does not run either — so nothing of this step is scored, while the
        # pre-request assertion above has ALREADY run and keeps the skip RECORDED as
        # a failure naming the variable, never a mute one. (`auth="anonymous"` is the
        # DELIBERATE anonymous case and takes the branch above — never affected.)
        f"  pm.test({js_str(f'harness config: {auth} is set (subject under test)')}, () => {{",
        "    pm.expect.fail(" + js_str(
            f"{auth} is not set — the authz-fixture seed "
            "(tests/authz-fixtures/setup.sh) did not provide this subject. Running the step "
            "anonymously would test a DIFFERENT principal and pass for the wrong reason.") + ");",
        "  });",
        "  pm.execution.skipRequest();",
        "}",
    ]


# ─────────────────────────────────────────────────────────────────────────────
# ШАГ УДАЛЕНИЯ ОБЯЗАН НЕСТИ УТВЕРЖДЕНИЕ — ставится ЗДЕСЬ, при сериализации.
#
# Перепись по дереву (82 коллекции, 8233 шага, 1359 из них DELETE) нашла 457
# шагов удаления БЕЗ единого утверждения: ни `pm.test`, ни голого `pm.expect`,
# ни `pm.response.to.*`. Такой шаг читает 200, 403, 404 и 500 одинаково и
# зеленеет на каждом.
#
# Тихим это не остаётся. У асинхронного удаления шаг захватывает `opId` из тела
# ответа, а следующий шаг опрашивает операцию по этому имени. Отказ тела не
# несёт — захват не срабатывает, `opId` остаётся ОТ ПРЕДЫДУЩЕЙ операции (как
# правило уже `done`), и опрос подтверждает чужой, давно завершённый успех.
# Кейс отчитывается зелёным по операции, которую он не запускал; ресурс при
# этом жив — фикстура течёт, ограниченный пул деградирует, списочные контракты
# плывут.
#
# ПОЧЕМУ ИСХОД ОДИН, А НЕ «ЛИБО УСПЕХ, ЛИБО ОТКАЗ». Перепись действующего лица
# по этим шагам: все они удаляют СВОЙ ресурс под предъявителем, которому это
# разрешено (434 — предъявитель коллекции, 23 — администратор аккаунта своего
# же кейса), и ни один не идёт под субъектом, которому отказ полагается по
# замыслу. Отрицательные кейсы удаления утверждение уже несут — и по этому
# признаку injection их не касается by construction.
#
# Отказ ПРЕДМЕТА у асинхронного удаления («тип машины ещё используется»)
# приезжает ошибкой операции, а HTTP при этом 200 — поэтому утверждение о коде
# ответа не конкурирует с утверждением об исходе операции: первое проверяет,
# что запрос принят, второе — что он сделал.
#
# ВСТАВКА В КОНЕЦ — не вкусовщина. Обёртка повторного обращения
# (`retry_until_authorized`) возвращает управление из скрипта, пока ждёт окна
# видимости; утверждение, поставленное ПЕРЕД ней, роняло бы шаг на первом же
# 403, который обёртка обязана переждать. В конце оно исполняется ровно один
# раз — на терминальном ответе.
#
# ВЫКЛЮЧАТЕЛЯ НЕТ. Шаг, которому полагается другой исход, пишет СВОЁ
# утверждение — и тем самым подавляет это по построению. Список исключений не
# заводится: ему было бы нечего исключать, а исключение без предмета переживает
# свой предмет и начинает лгать.
#
# Свойство держится гейтом `deploy/scripts/assert-delete-steps-are-asserted.py`
# (он же — авторитет по предикату; расхождение с ним видно как красный гейт).
_ASSERT_FORMS = ("pm.test(", "pm.expect(", "pm.response.to.")


def _strip_js_comments(src: str) -> str:
    """Снять `//`-хвосты и `/* */`-блоки, не трогая строковые литералы.

    Читается ИСПОЛНЯЕМАЯ часть, а не текст: обёртка повторного обращения
    приносит в шаг несколько строк объяснений, и поиск по сырому тексту принял
    бы объяснение защиты за саму защиту. `//` внутри строки (в URL) при этом
    комментарием не является — срезав его, читатель отрубил бы код следом.
    """
    out, i, n, quote = [], 0, len(src), None
    while i < n:
        ch, nxt = src[i], (src[i + 1] if i + 1 < n else "")
        if quote:
            out.append(ch)
            if ch == "\\" and i + 1 < n:
                out.append(nxt); i += 2; continue
            if ch == quote:
                quote = None
            i += 1; continue
        if ch in ("'", '"', "`"):
            quote = ch; out.append(ch); i += 1; continue
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
            i += 2; continue
        out.append(ch); i += 1
    return "".join(out)


def _carries_assertion(exec_lines: List[str]) -> bool:
    code = _strip_js_comments("\n".join(exec_lines))
    return any(form in code for form in _ASSERT_FORMS)


# Утверждение о том, что удаление ПРИНЯТО. Ровно одно и однозначное: `oneOf`
# со взаимоисключающими исходами утверждением не является (testing.md).
_DELETE_ACCEPTED = [
    "// УТВЕРЖДЕНИЕ ПО УМОЛЧАНИЮ для шага удаления: без него шаг зеленел бы и на",
    "// отказе, а следующий опрос уехал бы на opId предыдущей операции.",
    "pm.test('delete accepted: status 200', () => "
    "pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
]


# ---------------------------------------------------------------------------
# ИСХОД ОПЕРАЦИИ УДАЛЕНИЯ УТВЕРЖДАЕТСЯ, А НЕ ТОЛЬКО ЕЁ ЗАВЕРШЕНИЕ
# ---------------------------------------------------------------------------
# Опрос дожидается `done` и на этом успокаивается. Но `done` — это «воркер
# закончил», а не «сделал»: операция, завершившаяся ОШИБКОЙ, тоже `done`.
# Поэтому отказ удаления читался как успех — ресурс оставался жить, ограниченный
# пул деградировал, списочные контракты плыли, а вердикт не менялся.
#
# Утверждение ставится ПРОХОДОМ ПО ШАГАМ КЕЙСА, а не параметром помощника:
# опросов в дереве больше, чем вызовов помощника (часть кейсов несёт собственные,
# рукописные), и параметр закрыл бы только своих — то есть починил бы экземпляр,
# а не класс. Предмет опроса — ближайшая предшествующая мутация ТОГО ЖЕ кейса;
# граница кейса соблюдается намеренно, иначе опрос подхватил бы удаление из
# соседнего и утверждение относилось бы к паре, которой нет.
#
# Кейс, чей ПРЕДМЕТ — отказ удаления (`must_fail`), исход уже утверждает сам, и
# проход его не трогает: наличие утверждения об `error` — единственный признак,
# по которому шаг признаётся закрытым, а форма записи не навязывается.
#
# Гейт по дереву на обе половины пары — `deploy/scripts/assert-delete-operation-outcome.py`.
_OP_POLL_PATH = re.compile(r"/operations/\{\{(\w+)\}\}")
_MUTATION_METHODS = ("POST", "PUT", "PATCH", "DELETE")
# Утверждение об исходе / о завершении — обращение к полю операции ВНУТРИ аргумента
# `pm.expect(...)`. Опознаётся ПОЛЕ, а не носитель и не форма выражения: имя
# переменной у каждого поллера своё (`j`, `_dj`, `_do`), а записей исхода в дереве
# три (`j.error && j.error.code`, `Boolean(j.response) && !j.error`,
# `pm.environment.get('lastOpError') || ''`). Узнавать одну значило бы ловить
# полюбившуюся запись вместо существа — и дописать утверждение туда, где оно уже есть.
_FIELD_OUTCOME = re.compile(r"\.error\b|lastOpError")
_FIELD_DONE = re.compile(r"\.done\b")


def _expect_args(code: str):
    for m in re.finditer(r"pm\.expect\(", code):
        yield code[m.end():m.end() + 300].split(";")[0]


def _asserts_outcome(code: str) -> bool:
    return any(_FIELD_OUTCOME.search(a) for a in _expect_args(code))


def _asserts_done(code: str) -> bool:
    return any(_FIELD_DONE.search(a) for a in _expect_args(code))


def _delete_outcome_assert(need_done: bool) -> List[str]:
    """Утверждение об исходе операции удаления, дописываемое в КОНЕЦ скрипта опроса.

    Конец, а не начало: у опроса есть ранние выходы — «поллить нечего» (мутация
    отвергнута синхронно, имя пустое), «ответ не 200» и «ещё не done, повторяем».
    Дописанное в конец исполняется ровно тогда, когда опрос дошёл до терминального
    состояния, — и не утверждает ничего там, где утверждать не о чем.

    `need_done` — у рукописных поллеров уборки завершение не утверждается вовсе;
    для них к исходу добавляется и оно, иначе повисшая операция осталась бы зелёной.
    Носитель ответа читается ЗАНОВО (`_do`), а не переиспользуется: имя переменной
    у каждого поллера своё, и опираться на него значило бы связать проход с формой.
    """
    lines = [
        "// ИСХОД УДАЛЕНИЯ, А НЕ ТОЛЬКО ЕГО ЗАВЕРШЕНИЕ: операция, завершившаяся",
        "// ошибкой, тоже done — без этого утверждения отказ удаления читается как",
        "// успех, ресурс остаётся жить, а вердикт не меняется.",
        "(function () {",
        "  var _do; try { _do = pm.response.json(); } catch (e) { return; }",
    ]
    if need_done:
        lines += [
            "  pm.test('delete operation done', function () {",
            "    pm.expect(_do.done, JSON.stringify(_do)).to.eql(true);",
            "  });",
        ]
    lines += [
        "  pm.test('delete operation succeeded (no operation.error)', function () {",
        "    pm.expect(_do.error && JSON.stringify(_do.error), 'operation.error')"
        ".to.eql(undefined);",
        "  });",
        "})();",
    ]
    return lines


def _assert_delete_operation_outcome(steps: List[Step]) -> List[Step]:
    """У каждого удаления кто-нибудь из читателей его операции обязан назвать ИСХОД.

    Вопрос задаётся ОДИН НА ЦЕПОЧКУ, а не каждому шагу. У одного удаления опросов
    бывает несколько: первый дожидается завершения, следующий читает ту же операцию
    и утверждает о ней предметное. Требуя утверждения от каждого, проход дописал бы
    «операция удаления УСПЕШНА» ожидающему шагу кейса, чей ПРЕДМЕТ — ОТКАЗ удаления,
    и кейс стал бы утверждать обе взаимоисключающие вещи разом. Замеренные случаи:
    удаление отсутствующего образа (ожидается ошибка операции с точным текстом) и
    удаление роли, на которую есть выдача. Поэтому: если исход называет ЛЮБОЙ шаг
    цепочки — успехом или отказом, — дописывать нечего.

    Дописывается ПЕРВОМУ шагу цепочки: он и есть тот, кто дождался терминального
    состояния, и чинить класс надо там, где он возникает.
    """
    out = list(steps)
    chains = {}
    subject = None
    for idx, st in enumerate(out):
        if st.method == "GET" and _OP_POLL_PATH.search(st.path):
            if subject is not None:
                chains.setdefault(subject, []).append(idx)
            continue
        if st.method in _MUTATION_METHODS:
            subject = idx
    for sidx, polls in chains.items():
        if out[sidx].method != "DELETE":
            continue
        code = "\n".join(_strip_js_comments("\n".join(out[k].test_script)) for k in polls)
        if _asserts_outcome(code):
            continue
        k = polls[0]
        out[k] = replace(out[k], test_script=list(out[k].test_script)
                         + _delete_outcome_assert(not _asserts_done(code)))
    return out


_ENV_WRITE_TPL = r"environment\.set\(\s*['\"]%s['\"]\s*,"
_ENV_CLEAR_TPL = r"environment\.unset\(\s*['\"]%s['\"]\s*\)"
_ENV_EMPTY_TPL = r"environment\.set\(\s*['\"]%s['\"]\s*,\s*(''|\"\")\s*\)"


def _writes_env(code: str, var: str) -> bool:
    return re.search(_ENV_WRITE_TPL % re.escape(var), code) is not None


def _clears_env(code: str, var: str) -> bool:
    """Снятие имени — либо `unset`, либо присвоение ПУСТОЙ строки.

    Обе формы решают одну задачу: устаревшее значение не переживает шаг. Пустая
    строка — законная запись помощника синхронного отказа: имя остаётся
    ОПРЕДЕЛЁННЫМ, и страж неразрешённой подстановки не роняет опрос там, где
    отсутствия операции и ждали.
    """
    return (re.search(_ENV_CLEAR_TPL % re.escape(var), code) is not None
            or re.search(_ENV_EMPTY_TPL % re.escape(var), code) is not None)


def _reset_captured_operation_id(steps: List[Step]) -> List[Step]:
    """Захват идентификатора операции — ЗАМЕНА, а не дозапись: имя снимается первым.

    ЧТО ИНАЧЕ ПРОИСХОДИТ. Имя, которое читает следующий опрос, пишется телом
    ответа мутации. У ОТВЕРГНУТОЙ мутации тела с `id` нет — запись не
    выполняется, и в имени остаётся значение ПРЕДЫДУЩЕЙ операции. Опрос уезжает
    на чужую, давно завершённую операцию: `done === true` держится, зелёный
    приходит быстро и уверенно, а мутация, ради которой кейс написан, не
    проверена вовсе.

    ПОЧЕМУ ПРОХОДОМ ПО ШАГАМ, А НЕ ТОЛЬКО В `save_from_response`. Помощник
    снятие уже делает — но захват в дереве пишут и РУКАМИ, прямо в кейсе
    (`pm.environment.set('opId', pm.response.json().id)`). Требование,
    предъявленное только помощнику, обходится тем, что помощника не позвали, и
    обходится молча. Проход задаёт ТОТ ЖЕ вопрос, что гейт
    `deploy/scripts/assert-delete-operation-outcome.py`, и по тому же признаку:
    имя берётся из адреса опроса, а не из соглашения об именовании — общий
    `opId` в дереве не единственный, кейсы заводят собственные имена, и часть их
    не оканчивается на `OpId` (`_opGetAnon_opId`, `_igBindAnchorOp`).

    ПРЕДМЕТ — ЛЮБАЯ МУТАЦИЯ, НЕ ТОЛЬКО УДАЛЕНИЕ. Подмена чужой операцией
    происходит от отказа захвата, а не от глагола: перепись по дереву на
    1577829c7 дала 205 таких цепочек — DELETE 1, PATCH 18, POST 186.

    КУДА ВСТАВЛЯЕТСЯ. В начало того скрипта, где стоит сам захват: снятие после
    захвата было бы не снятием, а стиранием только что захваченного.
    """
    out = list(steps)
    chains: Dict[int, List[int]] = {}
    subject: Optional[int] = None
    for idx, st in enumerate(out):
        if st.method == "GET" and _OP_POLL_PATH.search(st.path):
            if subject is not None:
                chains.setdefault(subject, []).append(idx)
            continue
        if st.method in _MUTATION_METHODS:
            subject = idx
    for sidx, polls in chains.items():
        m = _OP_POLL_PATH.search(out[polls[0]].path)
        if not m:
            continue
        var = m.group(1)
        pre = _strip_js_comments("\n".join(out[sidx].pre_script))
        test = _strip_js_comments("\n".join(out[sidx].test_script))
        if not (_writes_env(pre, var) or _writes_env(test, var)):
            continue
        if _clears_env(pre, var) or _clears_env(test, var):
            continue
        reset = [f"pm.environment.unset({js_str(var)});"]
        if _writes_env(pre, var):
            out[sidx] = replace(out[sidx], pre_script=reset + list(out[sidx].pre_script))
        else:
            out[sidx] = replace(out[sidx], test_script=reset + list(out[sidx].test_script))
    return out


def _js_code_and_literals(src: str):
    """Разложить скрипт на ИСПОЛНЯЕМУЮ часть и значения строковых литералов.

    Комментарии снимаются, каждый строковый литерал заменяется меткой `@S<k>@`, а
    его значение уходит в список под индексом `k`. Так решение о публикации
    принимается по коду (текст внутри литерала им не является — иначе
    `pm.test('has metadata', …)` сошло бы за захват идентификатора), а ИМЯ
    переменной окружения всё-таки читается: в скелете, где содержимое литералов
    погашено, его бы уже не было.

    Разбор один на обе надобности намеренно: два разборщика расходятся молча и
    расходятся там, где расхождение не видно.
    """
    out, lits, i, n = [], [], 0, len(src)
    while i < n:
        ch, nxt = src[i], (src[i + 1] if i + 1 < n else "")
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
        if ch in ("'", '"', "`"):
            q, j, buf = ch, i + 1, []
            while j < n:
                if src[j] == "\\" and j + 1 < n:
                    buf.append(src[j + 1]); j += 2; continue
                if src[j] == q:
                    break
                buf.append(src[j]); j += 1
            out.append("@S%d@" % len(lits))
            lits.append("".join(buf))
            i = j + 1
            continue
        out.append(ch)
        i += 1
    return "".join(out), lits


_PUB_SET_RE = re.compile(r"pm\.environment\.set\(\s*@S(\d+)@\s*,")
_PUB_BIND_RE = re.compile(r"\b(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=")
# Объявление БЕЗ инициализатора (`let j;`) и присваивание отдельным оператором
# (`j = pm.response.json()`). Форма `let j; try { j = pm.response.json(); } catch (e)
# { j = null; }` — самая частая запись безопасного разбора тела в этом корпусе, и
# `_PUB_BIND_RE` её не узнаёт вовсе: она требует `=` В ОБЪЯВЛЕНИИ. Пока узнавалось
# только объявление-с-инициализатором, цепочка происхождения рвалась на первом
# звене, и проход не видел ни публикации, ни всего, что от этого имени
# производилось дальше. Тот же распознаватель и по той же причине расширен в гейте
# `internal/repohygiene/artifactgates` — проход и гейт обязаны считать ОДНО И ТО ЖЕ,
# иначе они разойдутся на первом же шаге, записанном не по канону.
_PUB_DECL_RE = re.compile(r"\b(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*[;,]")
# Имя непосредственно перед `=`: `a.b = c` отсекается предшествующей точкой,
# `==`/`===`/`=>` — заглядыванием вперёд, `+=`/`!==`/`>=` — тем, что между именем и
# `=` у них стоит оператор.
_PUB_ASSIGN_RE = re.compile(r"(?:^|[^.\w$])([A-Za-z_$][\w$]*)\s*=(?![=>])")
# Слова, за которыми `имя =` связыванием значения не является. Перечень закрытый:
# «что-нибудь похожее на ключевое слово» отсекло бы имя, начинающееся так же.
_PUB_RESERVED = frozenset((
    "if", "for", "while", "switch", "return", "function", "const", "let", "var",
    "catch", "typeof", "new", "delete", "void", "in", "of",
))


def _published_resource_vars(src: str, op_var: str) -> List[str]:
    """Имена окружения, которым шаг присваивает значение ИЗ `metadata` операции.

    Одного вхождения слова `metadata` в скрипт мало: один и тот же шаг захватывает
    и идентификатор ОПЕРАЦИИ (`j.id`), и идентификатор РЕСУРСА
    (`j.metadata.<res>Id`), причём оба — через локальную `const v` в СВОЁМ блоке
    (`save_from_response`). Без учёта области видимости ручка операции сошла бы за
    координату ресурса, и проход дописывал бы защиту там, где публиковать нечего.

    `op_var` — имя, которым цепочка адресует саму операцию (берётся из адреса
    опроса). Оно исключается: это ручка, а не координата ресурса.
    """
    code, lits = _js_code_and_literals(src)
    depth, cur = [0] * (len(code) + 1), 0
    for i, ch in enumerate(code):
        depth[i] = cur
        if ch == "{":
            cur += 1
        elif ch == "}":
            cur -= 1
    depth[len(code)] = cur

    binds = []  # (offset, depth, name, derived)

    def visible(at: int, expr: str) -> bool:
        for off, d, name, derived in binds:
            if off >= at or not derived:
                continue
            if any(depth[k] < d for k in range(off, at)):
                continue  # блок объявления уже закрыт — имя не видно
            if re.search(r"\b" + re.escape(name) + r"\b", expr):
                return True
        return False

    # ОБЛАСТЬ ВИДИМОСТИ БЕРЁТСЯ У ОБЪЯВЛЕНИЯ, А НЕ У ПРИСВАИВАНИЯ. `let j;` стоит на
    # верхнем уровне скрипта, а значение ему присваивают внутри `try { … }` — то
    # есть глубже. Считать глубиной связывания глубину присваивания значило бы
    # закрывать имя вместе с блоком `try`, и все последующие чтения `j` оказались бы
    # «вне области» — ровно наоборот тому, как это работает в JavaScript.
    decl_depth = {}
    for m in _PUB_DECL_RE.finditer(code):
        decl_depth[m.group(1)] = depth[m.start()]
    for m in _PUB_BIND_RE.finditer(code):
        decl_depth[m.group(1)] = depth[m.start()]

    sites = []  # (offset, depth, name, expr_at)
    for m in _PUB_BIND_RE.finditer(code):
        sites.append((m.start(), depth[m.start()], m.group(1), m.end()))
    for m in _PUB_ASSIGN_RE.finditer(code):
        name = m.group(1)
        if name in _PUB_RESERVED:
            continue
        at = m.start(1)
        # Объявление-с-инициализатором уже учтено выше: `const v = …` матчится и
        # сюда. Считать его дважды безвредно для вердикта, но смещение связывания
        # разошлось бы на длину `const `, а от смещения зависит проверка
        # «объявлено ДО использования».
        head = code[:at].rstrip()
        if head.endswith(("const", "let", "var")) and len(head) < at:
            tail = "const" if head.endswith("const") else ("let" if head.endswith("let") else "var")
            j = len(head) - len(tail)
            if j == 0 or not (code[j - 1].isalnum() or code[j - 1] in "_$"):
                continue
        sites.append((at, decl_depth.get(name, 0), name, m.end()))
    sites.sort(key=lambda s: s[0])

    for off, d, name, expr_at in sites:
        semi = code.find(";", expr_at)
        expr = code[expr_at:semi if semi >= 0 else len(code)]
        binds.append((off, d, name, "metadata" in expr or visible(off, expr)))

    def arg_tail(pos: int) -> str:
        lvl = 1
        for k in range(pos, len(code)):
            if code[k] == "(":
                lvl += 1
            elif code[k] == ")":
                lvl -= 1
                if lvl == 0:
                    return code[pos:k]
        return code[pos:]

    names: List[str] = []
    for m in _PUB_SET_RE.finditer(code):
        name = lits[int(m.group(1))]
        if not name or name == op_var or name in names:
            continue
        expr = arg_tail(m.end())
        if "metadata" in expr or visible(m.start(), expr):
            names.append(name)
    return sorted(names)


def _published_id_outcome_assert(names: List[str], need_done: bool,
                                 need_assert: bool = True) -> List[str]:
    """Снятие фантомного идентификатора и (по надобности) утверждение об ИСХОДЕ.

    Дописывается в КОНЕЦ скрипта: у опроса есть ранние выходы — «поллить нечего»
    (мутация отвергнута синхронно), «ответ не 200» и «ещё не done, повторяем».
    Дописанное в конец исполняется ровно тогда, когда исход уже известен.

    СНЯТИЕ ИМЕНИ НУЖНО ДАЖЕ ТАМ, ГДЕ ИСХОД УЖЕ УТВЕРЖДЁН. newman не прекращает
    кейс на упавшем утверждении: без снятия фантомный идентификатор всё равно
    уезжает в следующие шаги, и к настоящей находке добавляется каскад чужих
    отказов вокруг несуществующего объекта. Поэтому `need_assert=False` — форма
    для синхронной операции, чей шаг сам назвал исход: утверждать второй раз
    нечего, а снимать — надо.

    На успешной операции ветка не берётся вовсе: зелёный прогон эта правка не
    меняет ничем.

    Носитель ответа читается ЗАНОВО (`_po`), а не переиспользуется: имя переменной
    у каждого поллера своё, и опираться на него значило бы связать проход с формой.
    """
    lines = [
        "// ИСХОД, А НЕ ТОЛЬКО ЗАВЕРШЕНИЕ: операция несёт предвыделенный идентификатор",
        "// ресурса в metadata и тогда, когда завершилась ошибкой, — done у неё такой же",
        "// true. Опубликованный без этой проверки идентификатор уезжает дальше",
        "// координатой ресурса, которого нет, и падает уже не тот шаг, который ошибся.",
        "(function () {",
        "  var _po; try { _po = pm.response.json(); } catch (e) { return; }",
    ]
    if need_done:
        lines += [
            "  pm.test('operation done', function () {",
            "    pm.expect(_po.done, JSON.stringify(_po)).to.eql(true);",
            "  });",
        ]
    lines += ["  if (_po.error) {"]
    lines += ["    pm.environment.unset('%s');" % v for v in names]
    lines += ["  }"]
    if need_assert:
        lines += [
            "  pm.test('operation succeeded (no phantom %s)', function () {" % ", ".join(names),
            "    pm.expect(_po.error && JSON.stringify(_po.error), 'operation.error')"
            ".to.eql(undefined);",
            "  });",
        ]
    lines += ["})();"]
    return lines


def _assigns_env_var(src: str, name: str) -> bool:
    """Шаг ПРИСВАИВАЕТ это имя окружения (любым значением, включая сброс в пустое)."""
    code, lits = _js_code_and_literals(src)
    return any(lits[int(m.group(1))] == name for m in _PUB_SET_RE.finditer(code))


def _assert_published_id_outcome(steps: List[Step]) -> List[Step]:
    """Опубликовал идентификатор ресурса из metadata — назови ИСХОД операции.

    Операция несёт предвыделенный идентификатор в `metadata` ДАЖЕ когда завершилась
    ошибкой: он чеканится до того, как отработает воркер. Шаг, сохранивший
    `metadata.<res>Id`, и опрос, утверждающий только `done`, вместе публикуют
    координату ресурса, которого нет, — `done` у провалившейся операции такой же
    `true`. Дальше по этой координате идут привязки прав (край отвечает успехом) и
    межсервисные запросы (владелец отвечает «не найдено»), и падает не тот шаг,
    который ошибся: симптом к причине отношения не имеет.

    Ставится ПО СВОЙСТВУ шага, а не по перечню имён: ручная пометка неотличима от
    решения не помечать, и класс возвращался ровно так — закрыт в одном кейсе,
    через несколько часов проявился в соседнем.

    ОПРОС ПРИНАДЛЕЖИТ ТОМУ, ЧЬЮ ОПЕРАЦИЮ ЧИТАЕТ, а не просто предыдущей мутации.
    Между созданием и его опросом законно стоит другая мутация — отмена той же
    операции (`/operations/{{opId}}:cancel`), — и правило «последняя мутация»
    отдало бы опрос ей, оставив создание без единого читателя исхода. Поэтому
    опрос отходит ближайшей предшествующей мутации, которая ПРИСВАИВАЕТ имя,
    стоящее в адресе опроса; если такой нет — ближайшей предшествующей мутации.

    Вопрос задаётся ОДИН НА ЦЕПОЧКУ: если исход называет сам шаг мутации (так
    устроена синхронная операция без опроса вовсе) или ЛЮБОЙ её опрос — успехом
    или отказом, — дописывать нечего. Иначе проход дописал бы «операция успешна»
    кейсу, чей ПРЕДМЕТ — отказ операции, и кейс утверждал бы обе взаимоисключающие
    вещи разом.

    Держит свойство по дереву гейт `internal/repohygiene`
    `TestPublishedResourceIdIsGuardedByOperationOutcome` — он читает
    СГЕНЕРИРОВАННЫЕ коллекции, поэтому правка мимо генератора его не обходит.
    """
    out = list(steps)
    muts = [i for i, st in enumerate(out)
            if st.method in _MUTATION_METHODS and not _OP_POLL_PATH.search(st.path)]
    chains = {i: [] for i in muts}
    for idx, st in enumerate(out):
        if st.method != "GET":
            continue
        m = _OP_POLL_PATH.search(st.path)
        if not m:
            continue
        owner = None
        for i in muts:
            if i >= idx:
                break
            if _assigns_env_var("\n".join(out[i].test_script), m.group(1)):
                owner = i
        if owner is None:
            owner = max((i for i in muts if i < idx), default=None)
        if owner is not None:
            chains[owner].append(idx)
    for sidx, polls in chains.items():
        if not polls:
            continue  # операцию никто не опрашивает — вписать утверждение некуда
        op_var = "opId"
        for k in polls:
            m = _OP_POLL_PATH.search(out[k].path)
            if m:
                op_var = m.group(1)
        own = "\n".join(out[sidx].test_script)
        names = _published_resource_vars(own, op_var)
        if not names:
            continue
        if _asserts_outcome(_strip_js_comments(own)):
            # Исход назван самой мутацией — так устроена СИНХРОННАЯ операция
            # (`done:true` в ответе, опрашивать нечего). Утверждать второй раз
            # нечего, но снять опубликованное имя на ошибке всё равно надо.
            out[sidx] = replace(out[sidx], test_script=list(out[sidx].test_script)
                                + _published_id_outcome_assert(names, False, need_assert=False))
            continue
        code = "\n".join(_strip_js_comments("\n".join(out[k].test_script)) for k in polls)
        if _asserts_outcome(code):
            continue
        k = polls[0]
        out[k] = replace(out[k], test_script=list(out[k].test_script)
                         + _published_id_outcome_assert(names, not _asserts_done(code)))
    return out


def step_to_postman(step: Step) -> Dict:
    item: Dict = {
        "name": step.name,
        "request": {
            "method": step.method,
            "header": [{"key": "Content-Type", "value": "application/json"}],
            "url": {
                "raw": "{{baseUrl}}" + step.path,
                "host": ["{{baseUrl}}"],
                "path": [p for p in step.path.strip("/").split("/") if p],
            },
        },
    }
    if step.body is not None:
        item["request"]["body"] = {
            "mode": "raw",
            "raw": json.dumps(step.body, ensure_ascii=False),
            "options": {"raw": {"language": "json"}},
        }
    pre = list(step.pre_script)
    if step.auth is not None:
        pre = _auth_pre_script(step.auth) + pre
    events = []
    if pre:
        events.append({"listen": "prerequest", "script": {"type": "text/javascript", "exec": pre}})
    # Шаг удаления без собственного утверждения получает утверждение по умолчанию
    # (см. _DELETE_ACCEPTED выше). Ставится в КОНЕЦ — после обёртки ожидания.
    test_exec = list(step.test_script)
    if step.method == "DELETE" and not _carries_assertion(test_exec):
        test_exec = test_exec + _DELETE_ACCEPTED
    if test_exec:
        events.append({"listen": "test", "script": {"type": "text/javascript", "exec": test_exec}})
    if events:
        item["event"] = events
    return item


# --- Класс: первый доступ к СВОЕМУ свежему ресурсу без ограниченного ретрая ---
#
# Обёртка `retry_until_authorized` ставилась ВРУЧНУЮ, поэтому её пропуск был
# неотличим от решения не оборачивать. Замер по артефактам прогона CI
# 31002239590 (8 суит, 82 отчёта, 15648 утверждений, 151 падение): из 68
# падений полосы видимости (403/404) **42** пришлись на шаги, у которых обёртки
# не было ВОВСЕ, при том что соседние шаги той же формы в тех же кейсах
# обёрнуты — то есть пропуск, а не замысел.
#
# Предикат ставит обёртку ПО СВОЙСТВУ шага, а не по списку имён, и потому
# закрывает класс, а не перечисленные экземпляры. Четыре условия — все
# обязательны:
#   1. шаг УТВЕРЖДАЕТ УСПЕХ — то есть 200 входит в набор исходов, которые он
#      принимает, а 403 в него НЕ входит. Набор читается и из `to.eql(200)`, и
#      из `to.be.oneOf([...])` над `pm.response.code`: уборка своего свежего
#      ресурса сплошь записана вторым способом («удалилось 200 ЛИБО состояние
#      не позволило 400»), и пока предикат смотрел на буквальное `to.eql(200)`,
#      такие шаги были ему невидимы ПО ПОСТРОЕНИЮ — в суите vpc это 77 записей
#      из 93. Шаг, принимающий 403 своим исходом (authz-first толерантность
#      негатива), не оборачивается никогда: там отказ и есть проверяемое, а
#      ретрай маскировал бы его (`testing.md` — «НЕ оборачивать: negatives,
#      cross-account deny»). Пережидаются ТОЛЬКО те коды полосы видимости,
#      которых шаг исходом не заявлял: если 404 заявлен («уже нет»), ретрай
#      идёт лишь по 403, иначе обёртка жгла бы бюджет на принятом исходе;
#   2. адрес шага ссылается на переменную, РОЖДЁННУЮ РАНЕЕ В ЭТОМ ЖЕ КЕЙСЕ
#      (её published предыдущий шаг). Чужой/заранее известный id предикату
#      неизвестен — значит absent-id-негативы остаются строгими;
#   3. у шага НЕТ собственной петли (`setNextRequest`) — поллер операции ведёт
#      свою и переименован под себя; вторая петля сломала бы резолв имени;
#   4. шаг ещё не обёрнут вручную (идемпотентность).
#
# ЧТО ЗДЕСЬ ДОКАЗАНО, А ЧТО НЕТ (`PRO-Robotech/kacho#1277`).
#
# ПРОВЯЗАННОСТЬ предиката в ЭТОМ генераторе держит гейт дерева
# `internal/repohygiene/artifactgates/newmanfreshreadwrap_test.go`: генератор, у
# которого есть `retry_until_authorized`, но нет предиката в сериализации, — его
# находка.
#
# РАБОТОСПОСОБНОСТЬ предиката — инъекция настоящего пропуска и законные близнецы,
# на которых он обязан молчать, — доказана ТОЛЬКО для копии vpc:
# `services/vpc/tests/newman/scripts/selftest_autowrap.py` (её зовут `ci.yaml` и
# `e2e-newman.yml`). ЭТА копия ею НЕ ПОКРЫТА: своей самопроверки у набора нет.
# Правя предикат здесь, проверяй его сам — обещания, что правка проверена в обе
# стороны, тут нет.
#
# Прежняя редакция обещала обратное: она называла `scripts/selftest_autowrap.py`,
# то есть путь относительно ЭТОГО набора, где такого файла нет. Утверждение было
# не о стиле, а о ДОКАЗАННОСТИ, поэтому читатель, правящий предикат, не стал бы
# проверять сам. Держит правду гейт
# `internal/repohygiene/newmanproofclaim_test.go`: утверждение о доказательстве
# обязано называть координату, которая резолвится.
_FRESH_VAR_SET_RE = re.compile(
    r"pm\.(?:environment|collectionVariables|globals)\.set\(\s*['\"]([A-Za-z_][A-Za-z0-9_]*)['\"]"
)
_VAR_REF_RE = re.compile(r"\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}")

# Набор HTTP-исходов, которые шаг ПРИНИМАЕТ. Оба выражения привязаны к
# `pm.response.code`, поэтому набор gRPC-кодов (`pm.expect(j.code, …).to.be
# .oneOf([5, 9])`) сюда не попадает: числа там из другого пространства и на
# полосу видимости не отображаются. Границей служит `;` — конец стейтмента.
_HTTP_EQ_RE = re.compile(r"pm\.response\.code[^;]*?\.to\.(?:be\.)?(?:eql|equal)\((\d{3})\)")
_HTTP_ONEOF_RE = re.compile(r"pm\.response\.code[^;]*?\.to\.be\.oneOf\(\[([0-9,\s]+)\]\)")


def _accepted_http_codes(body: str) -> set:
    """HTTP-коды, объявленные шагом как приемлемый исход."""
    acc = set()
    for m in _HTTP_EQ_RE.finditer(body):
        acc.add(int(m.group(1)))
    for m in _HTTP_ONEOF_RE.finditer(body):
        for part in m.group(1).split(","):
            part = part.strip()
            if part:
                acc.add(int(part))
    return {c for c in acc if 100 <= c <= 599}


def _body_text(step: Step) -> str:
    """Текст тела запроса для поиска ссылок на переменные.

    Тело — произвольной вложенности, поэтому сериализуется целиком, а не
    обходится по верхним ключам: ссылка на свежий ресурс встречается и внутри
    вложенного объекта (`{"v4Source": {"subnetId": "{{subId}}"}}`).
    """
    if not step.body:
        return ""
    try:
        return json.dumps(step.body)
    except (TypeError, ValueError):
        return str(step.body)


def _wrap_own_fresh_reads(steps: List[Step], rename: bool = True) -> List[Step]:
    """Обернуть положительные первые обращения к своему свежему ресурсу.

    Возвращает НОВЫЙ список шагов; исходные Step не мутируются.

    `rename=False` — для генератора, который САМ делает имена шагов глобально
    уникальными при сериализации (iam: `<case-id> :: <шаг>`) и переписывает
    буквальные переходы `setNextRequest('<сосед>')` по БАЗОВЫМ именам. Там
    переименование обёрткой сломало бы резолв такого перехода, а нужды в нём
    нет: `pm.info.requestName` резолвится в итоговое имя, уже уникальное.
    """
    fresh: set = set()
    out: List[Step] = []
    for st in steps:
        body = "\n".join(st.test_script)
        self_looped = "setNextRequest" in body
        already = "_authRetryCount" in body or "_absRetryCount" in body
        accepted = _accepted_http_codes(body) if st.test_script else set()
        # Шаг УДАЛЕНИЯ без собственного утверждения ПОЛУЧИТ утверждение по
        # умолчанию — `delete accepted: status 200` (_DELETE_ACCEPTED) — и получит его
        # ПОСЛЕ этой обёртки, при сериализации. Решать, чего ждать, надо по
        # скрипту, который шаг ПОНЕСЁТ, а не по тому, который он несёт сейчас:
        # иначе решение принимается на предпосылке, которую следующий же проход
        # отменяет, и обёртка не ждёт тот единственный код, на котором шаг упадёт.
        if st.method == "DELETE" and not _carries_assertion(list(st.test_script)):
            accepted = accepted | {200}
        # Ждать можно ТОЛЬКО код, который шаг исходом не заявлял, — тогда
        # ожидание ничего не маскирует по построению: если 403/404 названы
        # приемлемым исходом, шаг про них и спрашивает, и ретрай там запрещён.
        # Требования «шаг обязан ждать 200» больше нет: отрицательная проба
        # СВОЕГО СВЕЖЕГО ресурса («такой CIDR отвергается») тоже упирается в
        # окно видимости и получает 403 вместо ожидаемого 400 — то есть падает
        # не по своему предмету. Чужой аккаунт, посеянный и несуществующий id
        # под правило не подпадают: они не рождены в этом кейсе.
        # Шаг без единого утверждения и НЕ удаляющий ждёт только 403: у такого
        # чтения 404 часто законное «уже нет», и жечь на нём бюджет незачем. У удаления
        # это больше не так: там 404 роняет утверждение по умолчанию.
        retry_on = tuple(c for c in (403, 404) if c not in accepted)
        if not accepted:
            retry_on = (403,)
        if st.test_script and not self_looped and not already and 403 not in accepted and retry_on:
            # Цель проверки прав называется адресом ЛИБО ПОЛЕМ ЗАПРОСА: край берёт
            # объект из `scope_extractor.from_request_field` каталога прав, и у
            # создания вложенного ресурса адрес коллекционный, а свежий родитель
            # стоит в теле. Условие, читавшее только `st.path`, такой шаг не видело
            # ПО ПОСТРОЕНИЮ — и это не мелочь: пропущенный шаг обычно СОЗДАЁТ
            # фикстуру, на которой стоит предмет кейса, поэтому его отказ уезжает
            # не в «фикстура не создалась», а в красное утверждение о предмете
            # (наблюдалось на удалении группы целей: ссылки не возникло, продукт
            # верно разрешил удаление, а кейс отчитался о сломанной ссылочной
            # целостности). Ждать на СОСЕДНЕЙ полосе нельзя: чтение родителя
            # гейтится одним отношением, создание вложенного — другим.
            if set(_VAR_REF_RE.findall(st.path + _body_text(st))) & fresh:
                w = retry_until_authorized(st, retry_on=retry_on)
                st = replace(w, name=st.name) if not rename else w
                body = "\n".join(st.test_script)
        for name in _FRESH_VAR_SET_RE.findall(body):
            fresh.add(name)
        out.append(st)
    return out



def case_to_postman(case: Case) -> Dict:
    tags = [f"class:{c}" for c in case.classes] + [f"priority:{case.priority}"]
    return {
        "name": f"{case.id} — {case.title}",
        "description": " | ".join(tags),
        "item": [step_to_postman(s) for s in
                 _assert_published_id_outcome(
                     _reset_captured_operation_id(_assert_delete_operation_outcome(_wrap_own_fresh_reads(case.steps))))],
    }


def build_collection(service: str, cases: List[Case]) -> Dict:
    return {
        "info": {
            # Deterministic _postman_id (UUIDv5 over the collection name) so a
            # regeneration with no source change produces no diff. A random id
            # here made every regeneration dirty every collection, which meant
            # "generated matches source" could never be checked and a real drift
            # had nowhere to show. Postman only needs this to be stable+unique.
            "_postman_id": str(uuid.uuid5(uuid.NAMESPACE_URL, f"kacho-nlb/newman/{service}")),
            "name": f"kacho-nlb / newman / {service}",
            "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
        },
        "event": [
            {
                "listen": "prerequest",
                "script": {"type": "text/javascript", "exec": PRE_GLOBAL},
            },
        ],
        "item": [case_to_postman(c) for c in cases],
        "variable": [],
    }


# ---------------------------------------------------------------------------
# Module discovery & main
# ---------------------------------------------------------------------------

def _reset_step_name_counters() -> None:
    """Reset every counter that feeds a STEP NAME, before loading a case module.

    A step name must be a function of the CASE, never of the environment. These
    counters live at module scope and only ever grow, so without this reset a
    name would depend on how many case modules were loaded before this one:
    `gen.py <module>` and a full `gen.py` would then emit DIFFERENT names for
    the same case. Two consequences, both observed: regenerating one module
    leaves a tree the full run does not produce (a one-module commit for #307
    needed a 670-line follow-up), and the step names used to diagnose a red run
    do not match between runs.

    Resetting is safe by construction: newman resolves setNextRequest by request
    name WITHIN the collection being run, and one case module produces exactly
    one collection — so uniqueness is only ever required within that scope.

    Held by internal/repohygiene TestGeneratedStepNamesDoNotDependOnHowManyModulesRan,
    which executes this generator in both modes and compares the bytes.
    """
    global _poll_seq
    _poll_seq = 0
    _RYA_SEQ[0] = 0


def load_cases_module(path: Path):
    _reset_step_name_counters()
    spec = importlib.util.spec_from_file_location(path.stem, path)
    mod = importlib.util.module_from_spec(spec)
    # Inject helpers into the module's namespace so case files don't import gen.
    mod.Step = Step
    mod.Case = Case
    mod.assert_status = assert_status
    mod.assert_grpc_code = assert_grpc_code
    mod.assert_unscoped_rejected = assert_unscoped_rejected
    mod.assert_absent_id_rejected = assert_absent_id_rejected
    mod.assert_field_violation = assert_field_violation
    mod.assert_refused_sync_or_async = assert_refused_sync_or_async
    mod.assert_operation_envelope = assert_operation_envelope
    mod.save_from_response = save_from_response
    mod.poll_operation_until_done = poll_operation_until_done
    mod.retry_until_authorized = retry_until_authorized
    mod.warm_peer_fixture = warm_peer_fixture
    mod.retry_until_present = retry_until_present
    mod.retry_until_state = retry_until_state
    mod.retry_create_until_present = retry_create_until_present
    mod.retry_delete_until_released = retry_delete_until_released
    mod.http_method_not_allowed_block = http_method_not_allowed_block
    mod.conf_alreadyexists_block = conf_alreadyexists_block
    # Адрес нарезаемой подсети — только через этот помощник: он единственный
    # читает объявленный сетью план. Своя копия генератора в кейсе — тот самый
    # класс, ради которого помощник и заведён (см. раздел «АДРЕС НАРЕЗАЕМОЙ
    # ПОДСЕТИ» выше и гейт TestOutOfCaseCarveTakesItsCidrFromThePublishedPlan).
    mod.carve_cidr_pre = carve_cidr_pre
    spec.loader.exec_module(mod)
    return mod


def _check_duplicate_ids() -> int:
    seen: Dict[str, str] = {}
    dups: List[str] = []
    for f in sorted(CASES_DIR.glob("*.py")):
        if f.name.startswith("_"):
            continue
        mod = load_cases_module(f)
        for c in getattr(mod, "CASES", []):
            if c.id in seen:
                dups.append(f"  - {c.id!r}: {seen[c.id]} and {f.name}")
            else:
                seen[c.id] = f.name
    if dups:
        sys.stderr.write("gen: FAIL — duplicate case-id (must be unique across all modules):\n")
        sys.stderr.write("\n".join(dups) + "\n")
        return 1
    return 0


def main(argv: List[str]) -> int:
    args = argv[1:]
    if "--validate" in args:
        import runpy
        sys.argv = [str(SCRIPTS_DIR / "validate-cases.py")]
        runpy.run_path(str(SCRIPTS_DIR / "validate-cases.py"), run_name="__main__")
        return 0  # validate-cases.py calls sys.exit itself

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    want = set(args)
    found = sorted(f for f in CASES_DIR.glob("*.py") if not f.name.startswith("_"))
    if not found:
        print(f"no case files in {CASES_DIR}")
        return 1
    if _check_duplicate_ids() != 0:
        return 1
    total = 0
    for f in found:
        svc = f.stem
        if want and svc not in want:
            continue
        mod = load_cases_module(f)
        cases = getattr(mod, "CASES", [])
        col = build_collection(svc, cases)
        out = OUT_DIR / f"{svc}.postman_collection.json"
        out.write_text(json.dumps(col, indent=2, ensure_ascii=False))
        print(f"[{svc}] {len(cases)} cases → {out.relative_to(ROOT)}")
        total += len(cases)
    print(f"total: {total} cases")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
