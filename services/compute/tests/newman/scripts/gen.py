#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""
tests/newman/scripts/gen.py — генератор Postman collections из декларативных case-файлов.

Использование:
    python3 scripts/gen.py             # все ресурсы
    python3 scripts/gen.py disk        # один ресурс

Источник истины — модули в tests/newman/cases/<resource>.py, каждый экспортирует
переменную CASES — список объектов Case (см. ниже).

Структурно — копия `../kacho-vpc/tests/newman/scripts/gen.py`, адаптированная под
compute: REST-префикс `/compute/v1/`, операции — `/operations/{id}` (общий
OpsProxy api-gateway, prefix `epd`), env-var `garbageComputeId`. LRO-poll helper
(POST → Operation → poll GET /operations/{id} до done → assert response/error)
сохранён 1-в-1.
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
from typing import List, Dict, Optional, Tuple


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
CASES_DIR = ROOT / "cases"
OUT_DIR = ROOT / "collections"


# ---------------------------------------------------------------------------
# Декларативные структуры
# ---------------------------------------------------------------------------

@dataclass
class Step:
    """Один HTTP-запрос внутри case."""
    name: str
    method: str
    path: str  # относительный, {{baseUrl}} префикс автоматически
    body: Optional[Dict] = None
    pre_script: List[str] = field(default_factory=list)
    test_script: List[str] = field(default_factory=list)
    # KAC-122: per-step auth override для authz-deny suite.
    #   None              — header не трогается (default — inherit collection Bearer если есть)
    #   "anonymous"       — Authorization header снимается перед запросом
    #   "<envVarName>"    — Authorization: Bearer {{envVarName}} (значение читается из env при выполнении)
    auth: Optional[str] = None
    # internal=True — запрос идёт на api-gateway cluster-internal REST listener
    # ({{internalBaseUrl}}, :8081 → port-forward :18081), НЕ на публичный mux
    # ({{baseUrl}}, :8080). Internal*-RPC (InternalMachineTypeService admin-CRUD,
    # COMP-1 F7 seed) живут ТОЛЬКО там (ban #6) — на публичном :8080 их нет by design.
    # CI-драйвер (deploy/scripts/newman-e2e.sh) прокидывает --env-var internalBaseUrl;
    # PRE_GLOBAL даёт fallback-деривацию из baseUrl для standalone-прогона.
    internal: bool = False


@dataclass
class Case:
    """Один тестовый кейс — может содержать несколько шагов."""
    id: str  # например DISK-CR-CRUD-OK
    title: str  # человеко-читаемое описание
    classes: List[str]  # CRUD / VAL / NEG / BVA / ...
    priority: str  # P0 / P1 / P2 / P3
    steps: List[Step]


# ---------------------------------------------------------------------------
# Глобальный prerequest (runId генерация + _suiteProject* алиасы)
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
    "  // runId формат: только [a-z0-9], без точки, начинается с буквы — чтобы проходить compute name regex",
    "  const t = Date.now().toString(36);",
    "  const r = Math.floor(Math.random() * 1e9).toString(36);",
    "  pm.environment.set('runId', ('r' + t + r).replace(/[^a-z0-9]/g, '').slice(0, 11));",
    "}",
    "pm.environment.set('_suiteProjectId', pm.environment.get('existingProjectId'));",
    "pm.environment.set('_suiteProjectCrossId', pm.environment.get('existingProjectCrossId'));",
    "// internalBaseUrl fallback: CI-драйвер (newman-e2e.sh) прокидывает --env-var,",
    "// но для standalone-прогона деривируем cluster-internal listener из baseUrl",
    "// (публичный :8080/:18080 → internal-rest :8081/:18081). Internal*-шаги",
    "// (InternalMachineTypeService seed, COMP-1 F7) идут на {{internalBaseUrl}}.",
    "if (!pm.environment.get('internalBaseUrl') || pm.environment.get('internalBaseUrl') === '') {",
    "  const __b = pm.environment.get('baseUrl') || 'http://localhost:18080';",
    "  pm.environment.set('internalBaseUrl', __b.replace(/:(1?)8080(\\b|$)/, ':$18081'));",
    "}",
    "// Дефолтный Bearer — ПРОЕКТНЫЙ актор (editor @ project-A1 и project-A2), а НЕ",
    "// бутстрап-админ. Без какого-либо дефолта все шаги с auth=None анонимны → authn-",
    "// гейт края отвечает 401 fail-closed; с бутстрапом же они проходят ВСЕГДА, потому",
    "// что у него права на всё, — и тогда суита не может отличить работающую",
    "// project-scope авторизацию от сломанной. Ровно этот класс уже ловился в дереве:",
    "// карта прав сервиса разошлась с каталогом края по паре scope+relation, проектный",
    "// принципал получал отказ на СВОИХ ресурсах, а бутстрап-админ этого не видел.",
    "// Паритет с services/vpc/tests/newman/scripts/gen.py (там дефолт проектный).",
    "// Шаг, которому НУЖЕН cluster-admin (InternalMachineTypeService — system_admin на",
    "// cluster-singleton), объявляет это САМ: auth='jwtBootstrap'. Требование держит",
    "// проверка 4 в scripts/validate-cases.py, а не соглашение.",
    "// Per-step auth ('anonymous' снимает, '<envVar>' переопределяет) идёт в",
    "// item-pre-request ПОСЛЕ collection-pre-request, поэтому этот дефолт им не мешает.",
    "const __defAuth = pm.environment.get('jwtProjectAdminA1') || pm.variables.get('jwtProjectAdminA1') || '';",
    "if (__defAuth) { pm.request.headers.upsert({ key: 'Authorization', value: 'Bearer ' + __defAuth }); }",
    *_URL_VAR_GUARD,
]

# Актор, которого требует cluster-scoped админ-поверхность compute
# (`InternalMachineTypeService.Create/Delete/Update` → `system_admin` @
# `cluster:cluster_kacho_root`). Проектный актор его НЕ держит и держать не должен:
# посев выдаёт матричным служебным учёткам только `system_viewer@cluster` — этаж чтения
# глобального каталога, — поэтому админ-CRUD каталога размерностей остаётся за
# бутстрапом. Имя вынесено сюда, чтобы у всех кейсов был ОДИН источник и они не
# разъезжались по написанию.
ADMIN_AUTH = "jwtBootstrap"

# Маршрут админ-CRUD каталога размерностей на cluster-internal REST-листенере.
# Держится здесь же, потому что по нему проверка 4 в validate-cases.py опознаёт шаги,
# обязанные нести ADMIN_AUTH.
MT_INTERNAL_PATH = "/compute/v1/internal/machineTypes"


# ---------------------------------------------------------------------------
# Утилиты-сниппеты pm.*
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


def assert_unscoped_rejected() -> List[str]:
    """Unscoped create/list/get (без projectId, либо empty-body, либо method-mismatch
    на collection-endpoint) — ОТВЕРГНУТ. Два защитимых исхода, оба = «отклонено»
    (defense-in-depth, security.md «authz-first», parity с vpc 446e25b):
      403 PERMISSION_DENIED (code 7) — gateway scope_extractor fail-closed
        «no path: unscoped resource» ДО backend-валидации: нельзя авторизовать
        запрос, у которого нет scope для anti-BOLA-проверки;
      400 INVALID_ARGUMENT  (code 3) — backend «project_id required» при passthrough.
    Толерантен к обоим — семантика негатива (rejected) сохранена, без ложного провала
    на корректном authz-first 403 (реальный GATE-RUN #3: disk/image/snapshot unscoped
    cr-nf/list-nf/glf-nf/cr-empty-body возвращали code 7, тест ждал 3). Techniques:
    ECP (класс «unscoped запрос») + error-guessing (authz-vs-validation ordering)."""
    return [
        "pm.test('unscoped rejected (400 InvalidArgument or 403 authz-first)', () => {",
        "  pm.expect(pm.response.code, JSON.stringify(pm.response.json())).to.be.oneOf([400, 403]);",
        "});",
        "pm.test('grpc code 3 (INVALID_ARGUMENT) or 7 (PERMISSION_DENIED)', () => {",
        "  const j = pm.response.json();",
        "  pm.expect(j.code, JSON.stringify(j)).to.be.oneOf([3, 7]);",
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


def assert_operation_envelope() -> List[str]:
    return [
        "pm.test('Operation envelope returned', () => {",
        "  const j = pm.response.json();",
        "  pm.expect(j.id, 'operation.id').to.match(/^epd[a-z0-9]+$/);",
        "  pm.expect(j.metadata, 'operation.metadata').to.be.an('object');",
        "});",
    ]


def assert_created_at_seconds(jsonpath="pm.response.json().createdAt") -> List[str]:
    """CONF: created_at truncate до секунд (verbatim YC) — нет дробной части."""
    return [
        "pm.test('createdAt truncated to seconds', () => {",
        f"  const ts = ({jsonpath});",
        "  pm.expect(ts, 'createdAt present').to.be.a('string');",
        "  // RFC3339; если есть дробная часть — это .000... либо отсутствует",
        "  const m = ts.match(/\\.(\\d+)/);",
        "  if (m) pm.expect(parseInt(m[1].padEnd(9,'0'), 10), 'sub-second part is zero').to.eql(0);",
        "});",
    ]


_POLL_SEQ = [0]


_RYA_SEQ = [0]


# Separate counter: retry_until_absent is used by ONE suite, and sharing _RYA_SEQ
# would renumber the -rya/-lst/-st suffixes of every collection generated after it,
# burying a real change in cosmetic churn. The `-abs` prefix already keeps the two
# families apart, and audit_jumps proves uniqueness mechanically either way.
_ABS_SEQ = [0]


def retry_until_present(step: Step, id_env_var: str, budget: int = 50,
                        interval_ms: int = 600) -> Step:
    """Bounded retry a LIST step until the caller's OWN fresh resource id appears in
    the returned array (read-your-writes over the list-authz visibility window; opgate
    removed -> owner-tuple eventual-consistency). The list returns 200 with the id
    ABSENT until the tuple materializes, so retry_until_authorized (403/404) does not
    apply -- we retry while the id is missing. Fail-open after budget: the real
    assertion then runs once and FAILS if still absent (never masked, never infinite).
    Use ONLY on a list of the caller's OWN just-created resource.

    budget*interval_ms bounds the wait (default 50*600ms = 30s). Raised 40->50 (24s->30s,
    modest and targeted to THIS helper — not a blanket suite-wide widen): every call site
    already polls the create op to done first (most also warm the owner-tuple with a direct-
    read GET), yet the list-authz (ListObjects) materialization tail was observed to exceed
    the 24s default on the umbrella parallel lane (ListObjects consistency can lag the direct
    Check that a warm-GET satisfies). Fast lanes never consume the extra window (they converge
    in the first few polls), so the raise only extends the genuine tail — it does not mask a
    real over-hide, which still FAILS at budget."""
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


def retry_until_authorized(step: Step, budget: int = 40, interval_ms: int = 600,
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
    fires setNextRequest before any setTimeout). budget*interval_ms bounds the wait
    (default 15*400ms = ~6s) -- fail-closed: on any other code the wrapped step's real
    test_script runs exactly once, and once the budget is spent it ALSO runs on the
    terminal 403/404 (a genuine, non-converging deny still FAILS the real assertions --
    never masked, never infinite).

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


def retry_until_state(step: Step, converged_expr: str, budget: int = 40,
                      interval_ms: int = 600, retry_on=(403, 404)) -> Step:
    """Wrap the FIRST post-mutation / after-op VERIFY of the caller's OWN fresh resource
    in a bounded read-your-writes retry until the OBSERVED STATE has CONVERGED.

    Operation.done means the mutated resource is DURABLE (api-conventions.md), but a read
    that verifies a specific post-mutation field value can be transient in TWO ways: (a)
    the owner-tuple authz gate returns 403/404 before the tuple materialises, OR (b) the
    read returns 200 but with a STALE value before the write is reflected on the read path
    (e.g. GetLatestByFamily resolving the older image before the newer one is visible to
    the family query). retry_until_authorized covers only (a); this covers BOTH — retries
    SELF while the response is a transient 403/404 OR a 200 whose `converged_expr` (a JS
    boolean, TRUE once the expected state is observed) is still false, spacing attempts by
    ~interval_ms (busy-wait — newman fires setNextRequest before any setTimeout).

    Fail-OPEN at the budget: once spent, the wrapped step's real asserts run exactly once
    on the terminal response — a genuine never-converging state (a real product bug) STILL
    FAILS (never masked, never infinite). Use ONLY on a POSITIVE verify of the caller's OWN
    fresh resource — NEVER a negative / cross-account / absent-id read. Strict superset of
    retry_until_authorized (never hides what the authz-retry caught, only ADDS the
    state-convergence wait)."""
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


def retry_until_absent(step: Step, still_present_expr: str, budget: int = 25,
                       interval_ms: int = 500) -> Step:
    """Bounded retry a "must-be-ABSENT/empty" read over a read-your-writes-ON-REVOKE
    window — the MIRROR of retry_until_authorized for the deny/revoke side.

    A grant a subject just lost (revoked, or stripped by a pre-clean) can still be visible
    for a beat: the FGA tuple removal / list-authz negative-cache lags a few seconds after
    the revoke Operation is done (Kachō is eventually-consistent — api-conventions.md). So a
    "not-granted subject does NOT see the id" leak-guard flakes on the pre-convergence window
    under parallel load (the serial run's timing hid it).

    `still_present_expr` is a JS boolean, TRUE while the thing that MUST become absent is
    STILL present (e.g. the leaked id is still in the returned array). Retries SELF while it
    is truthy, spacing attempts by ~interval_ms (busy-wait — newman fires setNextRequest
    before any setTimeout).

    Fail-OPEN at the budget: once spent, the wrapped step's real assertions run exactly once
    on the terminal response — so a GENUINE over-grant / real leak (the thing NEVER becomes
    absent) still FAILS. It is impossible to mask a persistent leak; only a transient
    revoke/pre-clean-materialization window is absorbed. Use ONLY on a negative
    "must be absent" read whose absence is GUARANTEED once the subject's grant is genuinely
    gone — NEVER to paper over a cross-account deny or a real hole.

    The wrapped step is given a globally-unique name (`-abs<n>`), for the same reason
    retry_until_authorized/_present/_state do: a self-loop is expressed as
    setNextRequest(pm.info.requestName), and newman resolves that NAME against a
    collection-wide index in which a repeated name keeps only ONE entry — so a bare,
    repeated step name does not resolve to the running step at all. This suite names its
    steps by HTTP verb (`get`/`post`/…), so before the rename the deny-matrix carried 18
    items named `get`, and the first retry jumped the run to a different `get` near the end
    of the collection: 29 of the 42 matrix requests were never sent, and newman's report
    simply did not mention them — a skipped request is not a failed one, so the suite read
    green while two thirds of the matrix had not been asked."""
    guard = [
        "// bounded retry over the revoke/pre-clean materialization window (read-your-writes",
        "// ON REVOKE): retry SELF while the must-be-absent thing is still present, spacing",
        "// ~interval_ms. Fail-open at budget -> the real assertion below runs once and FAILS",
        "// if it is STILL present (a GENUINE over-grant / leak never clears -> NEVER masked).",
        "if (pm.environment.get('_absRetryStarted') !== pm.info.requestName) {",
        "  pm.environment.set('_absRetryCount', '0');",
        "  pm.environment.set('_absRetryStarted', pm.info.requestName);",
        "}",
        "const _absc = parseInt(pm.environment.get('_absRetryCount') || '0', 10);",
        "let _stillPresent = false;",
        f"try {{ _stillPresent = ({still_present_expr}); }} catch (e) {{ _stillPresent = false; }}",
        f"if (pm.response.code === 200 && _stillPresent && _absc < {budget}) {{",
        "  pm.environment.set('_absRetryCount', String(_absc + 1));",
        f"  const _absd = Date.now(); while (Date.now() - _absd < {interval_ms}) {{ /* revoke-materialization wait */ }}",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        "pm.environment.unset('_absRetryCount');",
        "pm.environment.unset('_absRetryStarted');",
    ]
    _ABS_SEQ[0] += 1
    return replace(step, name=f"{step.name}-abs{_ABS_SEQ[0]}",
                   test_script=guard + list(step.test_script))


def poll_operation_until_done(auth: Optional[str] = None) -> Step:
    """Reusable poll step: до 30 попыток с ~500ms задержкой между ними (через setNextRequest),
    потом fail если done остался false. Budget*interval ≈ 15s покрытия async-op tail (Koren #1).
    Уникальное имя per-call (poll-op-<N>): setNextRequest(pm.info.requestName) ретраит СЕБЯ, а не
    другой poll-op коллекции (иначе прыжок через кейсы → ложный fail). e2e-newman fullscope root A3.

    auth: КОГДА мутация создана НЕ дефолтным cluster-admin Bearer'ом (например
    jwtProjectAdminA1 в list-filter authz-суите), poll ОБЯЗАН нести ту же identity —
    `OperationService.Get` энфорсит ownership (owner = principal, создавший op) и отдаёт
    NotFound (no-leak) чужому caller'у. Без совпадения identity poll дефолтным bearer'ом
    получает 404 на op, созданную project-admin'ом (ownership-mismatch, НЕ GC/routing).
    Передавай auth=<тот же env-var, что у create-шага>; None → inherit collection Bearer."""
    _POLL_SEQ[0] += 1
    return Step(
        name=f"poll-op-{_POLL_SEQ[0]}",
        method="GET",
        path="/operations/{{opId}}",
        auth=auth,
        test_script=[
            "pm.test('poll status 200', () => pm.expect(pm.response.code).to.eql(200));",
            "const j = pm.response.json();",
            "const pc = parseInt(pm.environment.get('_pollCount') || '0', 10);",
            # Poll budget raised 20→30 (Koren-1): cover the p99 async-op tail under
            # suite load; the confirm-gate tail is cut by the HIGHER_CONSISTENCY read.
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
        ],
    )


def assert_op_error(code: int, code_name: str, msg_substr: Optional[str] = None,
                    msg_regex: Optional[str] = None, auth: Optional[str] = None) -> Step:
    """Поллит /operations/{opId} и проверяет, что operation завершилась с error.code == code.

    auth: как в poll_operation_until_done — при не-дефолтном создателе op читать
    Operation обязана та же identity (ownership-энфорс), иначе NotFound (no-leak)."""
    body = [
        "const j = pm.response.json();",
        "pm.test('operation done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
        f"pm.test({js_str(f'error code {code} ({code_name})')}, () => pm.expect(j.error && j.error.code, JSON.stringify(j)).to.eql({code}));",
    ]
    if msg_substr is not None:
        body.append(f"pm.test({js_str(f'error text includes \"{msg_substr}\"')}, () => pm.expect((j.error && j.error.message || '').toLowerCase()).to.include({js_str(msg_substr.lower())}));")
    if msg_regex is not None:
        body.append(f"pm.test({js_str(f'error text matches /{msg_regex}/')}, () => pm.expect(j.error && j.error.message || '').to.match(/{js_regex_src(msg_regex, where='compute/assert_op_error/msg_regex')}/));")
    return Step(name="assert-op-error", method="GET", path="/operations/{{opId}}", auth=auth, test_script=body)


def assert_op_error_oneof(codes: List[int], code_names: str,
                          msg_substr: Optional[str] = None, auth: Optional[str] = None) -> Step:
    """Как assert_op_error, но допускает НАБОР gRPC-кодов (когда точный код —
    3 vs 5 / 3 vs 9 — не зафиксирован контрактом). Проверка БЕЗУСЛОВНА: операция
    обязана завершиться с error (не response) — regression, при которой нелегальная
    операция начинает УСПЕШНО проходить, даёт RED (project-rule #12/#13; закрывает
    false-green `if (j.error)`-паттерн 3-го аудита)."""
    codes_js = "[" + ", ".join(str(c) for c in codes) + "]"
    body = [
        "const j = pm.response.json();",
        "pm.test('operation done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
        "pm.test('operation rejected (op-error present, not success)', () => pm.expect(Boolean(j.error), JSON.stringify(j)).to.eql(true));",
        f"pm.test({js_str(f'error code in {codes_js} ({code_names})')}, () => pm.expect(j.error && j.error.code, JSON.stringify(j)).to.be.oneOf({codes_js}));",
    ]
    if msg_substr is not None:
        body.append(f"pm.test({js_str(f'error text includes \"{msg_substr}\"')}, () => pm.expect((j.error && j.error.message || '').toLowerCase()).to.include({js_str(msg_substr.lower())}));")
    return Step(name="assert-op-error", method="GET", path="/operations/{{opId}}", auth=auth, test_script=body)


def assert_op_success(auth: Optional[str] = None) -> Step:
    # auth: как в poll_operation_until_done — при не-дефолтном создателе op
    # (jwtProjectAdminA1 и т.п.) читать Operation обязана та же identity
    # (ownership-энфорс на OperationService.Get), иначе NotFound (no-leak).
    return Step(name="assert-op-success", method="GET", path="/operations/{{opId}}", auth=auth,
                test_script=[
                    "const j = pm.response.json();",
                    "pm.test('operation done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
                    "pm.test('operation succeeded (response, no error)', () => pm.expect(Boolean(j.response) && !j.error, JSON.stringify(j)).to.eql(true));",
                ])


# ---------------------------------------------------------------------------
# Переиспользуемые блоки кейсов (compute-specific, generic)
# ---------------------------------------------------------------------------

def list_page_block(prefix, list_path, project_param=True):
    """BVA для List RPC: page_size 0 / 1 / 1000 / 1001 / garbage token.

    project_param=True — list_path требует ?projectId=... (Disk/Image/Snapshot/Instance);
    project_param=False — справочники (DiskType/Zone) — без projectId.
    """
    base = f"{list_path}?projectId={{{{_suiteProjectId}}}}&" if project_param else f"{list_path}?"
    return [
        Case(id=f"{prefix}-LST-BVA-PAGESIZE-ZERO",
             title="List pageSize=0 → default applied (200)",
             classes=["BVA", "PAGE"], priority="P2",
             steps=[Step(name="ps0", method="GET", path=f"{base}pageSize=0",
                         test_script=[*assert_status(200)])]),
        Case(id=f"{prefix}-LST-BVA-PAGESIZE-1",
             title="List pageSize=1 → ≤1 item",
             classes=["BVA", "PAGE"], priority="P2",
             steps=[Step(name="ps1", method="GET", path=f"{base}pageSize=1",
                         test_script=[*assert_status(200),
                                      "pm.test('at most 1 item', () => { const j = pm.response.json(); const k = Object.keys(j).find(x => Array.isArray(j[x])); pm.expect((j[k]||[]).length).to.be.at.most(1); });"])]),
        Case(id=f"{prefix}-LST-BVA-PAGESIZE-MAX-1000",
             title="List pageSize=1000 (boundary max) → 200",
             classes=["BVA", "PAGE"], priority="P2",
             steps=[Step(name="ps1000", method="GET", path=f"{base}pageSize=1000",
                         test_script=[*assert_status(200)])]),
        Case(id=f"{prefix}-LST-BVA-PAGESIZE-OVER-1001",
             title="List pageSize=1001 (over max) → 400 InvalidArgument",
             classes=["BVA", "VAL"], priority="P1",
             steps=[Step(name="ps1001", method="GET", path=f"{base}pageSize=1001",
                         test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])]),
        Case(id=f"{prefix}-LST-PAGE-TOKEN-GARBAGE",
             title="List с garbage page_token → 400 InvalidArgument",
             classes=["PAGE", "VAL"], priority="P1",
             steps=[Step(name="bad-token", method="GET", path=f"{base}pageSize=10&pageToken=not-a-real-token",
                         test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])]),
    ]


def name_validation_block(prefix, create_path, body_extra=None, wrap=None):
    """ECP/BVA по полю name (единая форма дерева — DNS label по RFC 1123,
    `pkg/validate.NameForm` `^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`):
      - empty name → 200, имя подставляется идентификатором ресурса (NameOrDefault)
      - len=63 (max) → 200
      - len=64 (over) → 400
      - UPPERCASE → 400
      - подчёркивание → 400
      - начинается с дефиса → 400
      - спец-символы → 400

    ЦИФРА ПЕРВЫМ СИМВОЛОМ БОЛЬШЕ НЕ ОТВЕРГАЕТСЯ — RFC 1123 её разрешает; кейс,
    утверждавший обратное, переведён на подчёркивание (см. комментарий у него).

    ВНИМАНИЕ: у этого блока СЕЙЧАС НЕТ НИ ОДНОГО ВЫЗЫВАЮЩЕГО в `cases/*.py`, то
    есть ни один из перечисленных кейсов не попадает в коллекции и не исполняется
    (предикат: `grep -rn name_validation_block services/compute/tests/newman/cases/`
    → пусто; в `collections/` нет ни одного `*-CR-VAL-NAME-*` из этого блока).
    Исходов два, и оба требуют решения владельца суиты: провязать блок к ресурсам
    либо снять его вместе с этим комментарием. Держать его дальше «как есть»
    значит держать проверку, которая ничего не проверяет.

    body_extra — обязательные поля кроме projectId/name.
    wrap(case) — опциональный декоратор (для Image/Snapshot/Instance которым нужен pre-disk и т.п.);
                 если задан — name-кейсы которые ожидают 200 оборачиваются (нужен реальный ресурс),
                 остальные (400) — нет (отказ синхронный, до создания зависимостей).
    """
    body_extra = body_extra or {}
    wrap = wrap or (lambda c: c)
    base = lambda name: {"projectId": "{{_suiteProjectId}}", "name": name, **body_extra}
    out = []
    out.append(wrap(Case(id=f"{prefix}-CR-VAL-NAME-EMPTY-OK",
        title="Create с empty name → 200 (proto pattern допускает пустую строку)",
        classes=["VAL", "BVA"], priority="P2",
        steps=[Step(name="cr-empty", method="POST", path=create_path, body=base(""),
                    test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
               poll_operation_until_done()])))
    out.append(wrap(Case(id=f"{prefix}-CR-BVA-NAME-MAX-63",
        title="Create с name len=63 (max) → 200",
        classes=["BVA"], priority="P2",
        steps=[Step(name="cr-max63", method="POST", path=create_path,
                    body=base("n" + "abcdefghij" * 6 + "ab"),  # 1+60+2 = 63
                    test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
               poll_operation_until_done()])))
    out.append(Case(id=f"{prefix}-CR-BVA-NAME-OVER-64",
        title="Create с name len=64 (over-max) → 400 InvalidArgument",
        classes=["BVA", "VAL"], priority="P1",
        steps=[Step(name="cr-over", method="POST", path=create_path,
                    body=base("n" + "abcdefghij" * 6 + "abc"),  # 64
                    test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])]))
    out.append(Case(id=f"{prefix}-CR-VAL-NAME-UPPERCASE",
        title="Create с UPPERCASE name → 400 (compute lowercase-only — НЕ как VPC)",
        classes=["VAL"], priority="P1",
        steps=[Step(name="cr-upper", method="POST", path=create_path, body=base("InvalidUpper-{{runId}}"),
                    test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])]))
    # ЗДЕСЬ БЫЛ КЕЙС «имя начинается с цифры → 400». Его предмета больше нет:
    # единая форма имени (DNS label по RFC 1123, `pkg/validate.NameForm`) разрешает
    # цифру первым символом, и `9invalid-…` теперь ЗАКОННОЕ имя. Кейс не удалён, а
    # переведён на ось, которая у формы действительно сузилась, — подчёркивание.
    out.append(Case(id=f"{prefix}-CR-VAL-NAME-UNDERSCORE",
        title="Create с подчёркиванием в name → 400 (форма имени: буквы, цифры, дефис)",
        classes=["VAL"], priority="P1",
        steps=[Step(name="cr-underscore", method="POST", path=create_path, body=base("bad_name-{{runId}}"),
                    test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])]))
    out.append(Case(id=f"{prefix}-CR-VAL-NAME-HYPHEN-START",
        title="Create с name начинающимся с дефиса → 400",
        classes=["VAL"], priority="P1",
        steps=[Step(name="cr-hyphen", method="POST", path=create_path, body=base("-bad-{{runId}}"),
                    test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])]))
    out.append(Case(id=f"{prefix}-CR-VAL-NAME-SPECIAL-CHARS",
        title="Create с спец-символами в name → 400",
        classes=["VAL"], priority="P1",
        steps=[Step(name="cr-special", method="POST", path=create_path, body=base("name!@#-{{runId}}"),
                    test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])]))
    return out


def labels_validation_block(prefix, create_path, body_extra=None, wrap=None):
    """ECP по labels: uppercase key → 400; invalid key char → 400; 64 (max) → 200; 65 (over) → 400."""
    body_extra = body_extra or {}
    wrap = wrap or (lambda c: c)
    base = lambda name, labels: {"projectId": "{{_suiteProjectId}}", "name": name, "labels": labels, **body_extra}
    return [
        Case(id=f"{prefix}-CR-VAL-LABELS-UPPERCASE-KEY",
             title="Create с UPPERCASE label key → 400",
             classes=["VAL"], priority="P1",
             steps=[Step(name="cr-lbl-up", method="POST", path=create_path,
                         body=base(f"{prefix.lower()}-lblup-{{{{runId}}}}", {"BADKEY": "v"}),
                         test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])]),
        Case(id=f"{prefix}-CR-VAL-LABELS-INVALID-KEY-CHAR",
             title="Create с invalid char в label key → 400",
             classes=["VAL"], priority="P1",
             steps=[Step(name="cr-lbl-bad", method="POST", path=create_path,
                         body=base(f"{prefix.lower()}-lblbad-{{{{runId}}}}", {"bad key!": "v"}),
                         test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])]),
        wrap(Case(id=f"{prefix}-CR-BVA-LABELS-MAX-64",
             title="Create с 64 labels (max) → 200",
             classes=["BVA"], priority="P2",
             steps=[Step(name="cr-lbl-max", method="POST", path=create_path,
                         body=base(f"{prefix.lower()}-lblm-{{{{runId}}}}", {f"k{i}": f"v{i}" for i in range(64)}),
                         test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
                    poll_operation_until_done()])),
        Case(id=f"{prefix}-CR-BVA-LABELS-OVER-65",
             title="Create с 65 labels (over-max) → 400",
             classes=["BVA", "VAL"], priority="P1",
             steps=[Step(name="cr-lbl-over", method="POST", path=create_path,
                         body=base(f"{prefix.lower()}-lblo-{{{{runId}}}}", {f"k{i}": f"v{i}" for i in range(65)}),
                         test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])]),
    ]


def description_validation_block(prefix, create_path, body_extra=None, wrap=None):
    """BVA по description: 256 (max) → 200; 257 (over) → 400."""
    body_extra = body_extra or {}
    wrap = wrap or (lambda c: c)
    base = lambda name, desc: {"projectId": "{{_suiteProjectId}}", "name": name, "description": desc, **body_extra}
    return [
        wrap(Case(id=f"{prefix}-CR-BVA-DESC-MAX-256",
             title="Create с description len=256 (max) → 200",
             classes=["BVA"], priority="P2",
             steps=[Step(name="cr-desc-max", method="POST", path=create_path,
                         body=base(f"{prefix.lower()}-descm-{{{{runId}}}}", "x" * 256),
                         test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
                    poll_operation_until_done()])),
        Case(id=f"{prefix}-CR-BVA-DESC-OVER-257",
             title="Create с description len=257 (over-max) → 400",
             classes=["BVA", "VAL"], priority="P1",
             steps=[Step(name="cr-desc-over", method="POST", path=create_path,
                         body=base(f"{prefix.lower()}-d2-{{{{runId}}}}", "x" * 257),
                         test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])]),
    ]


def filter_block(prefix, list_path):
    """Filter syntax: name="X" → 200; мусор и неизвестное поле → 400 InvalidArgument.

    Исход у обоих отрицаний УСТАНОВЛЕН разборщиком `pkg/filter`.`Parse`, а не
    «как получится»: имя поля берётся из белого списка вызывающего, и `this` из
    «this is not valid syntax» ровно так же не в списке, как и `nonexistent_field`.
    Оба дают `ParseError` → `InvalidArgument` с текстом «Bad expression at column N.».
    Прежнее `oneOf([200, 400])` перечисляло исход, которого на этих входах нет, и
    тем же утверждением приняло бы регрессию: разборщик, ПРОГЛОТИВШИЙ неизвестное
    поле, зеленел бы наравне с исправным.
    """
    sep = "&"
    return [
        Case(id=f"{prefix}-LST-FILTER-NAME-OK",
             title="List с filter name=\"foo\" → 200",
             classes=["FILTER", "CRUD"], priority="P2",
             steps=[Step(name="flt-ok", method="GET",
                         path=f"{list_path}?projectId={{{{_suiteProjectId}}}}{sep}filter=name%3D%22foo%22",
                         test_script=[*assert_status(200)])]),
        Case(id=f"{prefix}-LST-FILTER-GARBAGE",
             title="List с garbage filter syntax → 400 InvalidArgument",
             classes=["FILTER", "VAL"], priority="P2",
             steps=[Step(name="flt-bad", method="GET",
                         path=f"{list_path}?projectId={{{{_suiteProjectId}}}}{sep}filter=this%20is%20not%20valid%20syntax",
                         test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])]),
        Case(id=f"{prefix}-LST-FILTER-UNKNOWN-FIELD",
             title="List с filter на unsupported field → 400 InvalidArgument",
             classes=["FILTER", "VAL"], priority="P2",
             steps=[Step(name="flt-unk", method="GET",
                         path=f"{list_path}?projectId={{{{_suiteProjectId}}}}{sep}filter=nonexistent_field%3D%22x%22",
                         test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])]),
    ]


def security_injection_block(prefix, create_path, list_path, body_extra=None):
    """Security probes: SQL/cmd/XSS injection в name; никогда 500 / нет утечки pgx-stack."""
    body_extra = body_extra or {}
    injections = [
        ("sqli", "test' OR 1=1--"),
        ("union", "x' UNION SELECT * FROM operations--"),
        ("xss", "<script>alert(1)</script>"),
        ("cmd", "; rm -rf / ;"),
        ("path", "../../etc/passwd"),
        ("longpayload", "a" * 200),
    ]
    out = []
    for name, payload in injections:
        out.append(Case(id=f"{prefix}-CR-SEC-{name.upper()}",
            title=f"Security probe: {name} в name → handled, без 500/leak",
            classes=["SEC", "VAL", "NEG"], priority="P0",
            steps=[Step(name=f"sec-{name}", method="POST", path=create_path,
                        body={"projectId": "{{_suiteProjectId}}", "name": payload[:200], **body_extra},
                        test_script=[
                            "pm.test('not 500', () => pm.expect(pm.response.code).to.not.eql(500));",
                            "pm.test('handled 2xx/4xx', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 413]));",
                            "const body = JSON.stringify(pm.response.json() || {}).toLowerCase();",
                            "pm.test('no panic/sqlstate/stacktrace leak', () => { pm.expect(body).to.not.include('panic'); pm.expect(body).to.not.include('sqlstate'); pm.expect(body).to.not.include('goroutine'); });",
                        ])]))
    out.append(Case(id=f"{prefix}-LST-SEC-FILTER-SQLI",
        title="Security: SQL injection в filter → не 500",
        classes=["SEC", "VAL", "NEG"], priority="P0",
        steps=[Step(name="lst-sqli", method="GET",
                    path=f"{list_path}?projectId={{{{_suiteProjectId}}}}&filter=name%3D%22a%27%20OR%201%3D1--%22",
                    test_script=["pm.test('not 500', () => pm.expect(pm.response.code).to.not.eql(500));",
                                 "pm.test('status 200', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
                                 "pm.test('страница пуста: такого имени нет ни у кого', () => {",
                                 "  const j = pm.response.json();",
                                 "  const keys = Object.keys(j).filter(k => Array.isArray(j[k]));",
                                 "  pm.expect(keys, JSON.stringify(j)).to.have.lengthOf(1);",
                                 "  pm.expect(j[keys[0]], JSON.stringify(j)).to.have.lengthOf(0);",
                                 "});"])]))
    return out


def http_method_block(prefix, base_path):
    """HTTP method semantics: PUT / DELETE-on-list → 403|404|405|501.

    403 добавлен (parity vpc 446e25b, GATE-RUN #3): gateway scope_extractor
    fail-closes PERMISSION_DENIED на method без catalog-path (PUT/DELETE-on-list)
    ДО HTTP-method-routing — authz-first (security.md). 403|404|405|501 все =
    «operation not permitted», семантика негатива сохранена."""
    return [
        Case(id=f"{prefix}-METHOD-PUT-NOT-ALLOWED",
             title="PUT на List endpoint → 403/404/405/501 (rejected)",
             classes=["VAL", "NEG"], priority="P3",
             steps=[Step(name="put-list", method="PUT", path=base_path, body={"projectId": "{{_suiteProjectId}}"},
                         test_script=["pm.test('not allowed', () => pm.expect(pm.response.code).to.be.oneOf([403, 404, 405, 501]));"])]),
        Case(id=f"{prefix}-METHOD-DELETE-LIST",
             title="DELETE на List endpoint (без id) → 403/404/405/501 (rejected)",
             classes=["VAL", "NEG"], priority="P3",
             steps=[Step(name="del-list", method="DELETE", path=base_path,
                         test_script=["pm.test('not allowed', () => pm.expect(pm.response.code).to.be.oneOf([403, 404, 405, 501]));"])]),
    ]


def malformed_body_block(prefix, create_path):
    """Malformed JSON / empty body."""
    return [
        Case(id=f"{prefix}-CR-VAL-MALFORMED-JSON",
             title="Create с malformed JSON → 400/415",
             classes=["VAL", "NEG"], priority="P2",
             steps=[Step(name="cr-malformed", method="POST", path=create_path, body=None,
                         pre_script=["pm.request.body = { mode: 'raw', raw: '{invalid json---}' };"],
                         test_script=["pm.test('400 or 415', () => pm.expect(pm.response.code).to.be.oneOf([400, 415]));"])]),
        Case(id=f"{prefix}-CR-VAL-EMPTY-BODY",
             title="Create с пустым body → rejected (400 project_id required OR 403 authz-first, unscoped)",
             classes=["VAL", "NEG"], priority="P2",
             steps=[Step(name="cr-empty-body", method="POST", path=create_path, body={},
                         test_script=[*assert_unscoped_rejected()])]),
    ]


# ---------------------------------------------------------------------------
# Сериализация в Postman v2.1
# ---------------------------------------------------------------------------

def _auth_pre_script(auth: str) -> List[str]:
    """KAC-122: генерирует JS-сниппет для per-step Authorization-header.

    Для "anonymous" — снимает Authorization. Для имени env-переменной —
    Authorization: Bearer <значение env-var>. Snippet идёт в начало
    step.pre_script, перед всеми остальными pre-script строками."""
    if auth == "anonymous":
        return [
            "// KAC-122 authz-deny: anonymous step",
            "pm.request.headers.remove('Authorization');",
        ]
    return [
        f"// KAC-122 authz-deny: bearer from env '{js_comment(auth)}'",
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
                # internal=True → cluster-internal REST listener ({{internalBaseUrl}},
                # :8081) для Internal*-RPC (ban #6); иначе публичный mux ({{baseUrl}}).
                "raw": ("{{internalBaseUrl}}" if step.internal else "{{baseUrl}}") + step.path,
                "host": ["{{internalBaseUrl}}" if step.internal else "{{baseUrl}}"],
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
# Доказательство в обе стороны — `scripts/selftest_autowrap.py`: инъекция
# настоящего пропуска (краснеет без предиката) и ЧЕТЫРЕ законных близнеца
# (негатив, поллер, уже обёрнутый, чужой id), на которых предикат обязан
# молчать.
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


def build_collection(resource: str, cases: List[Case]) -> Dict:
    return {
        "info": {
            # Deterministic _postman_id (UUIDv5 over the collection name) so a
            # regeneration with no source change produces no diff. A random id
            # here made every regeneration dirty every collection, which meant
            # "generated matches source" could never be checked and a real drift
            # had nowhere to show. Postman only needs this to be stable+unique.
            "_postman_id": str(uuid.uuid5(uuid.NAMESPACE_URL, f"kacho-compute/newman/{resource}")),
            "name": f"kacho-compute / newman / {resource}",
            "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
        },
        "event": [
            {"listen": "prerequest", "script": {"type": "text/javascript", "exec": PRE_GLOBAL}},
        ],
        "item": [case_to_postman(c) for c in cases],
        "variable": [],
    }


# ---------------------------------------------------------------------------
# Discovery + main
# ---------------------------------------------------------------------------

def _reset_step_name_counters() -> None:
    """Reset every counter that feeds a STEP NAME, before loading a case module.

    A step name must be a function of the CASE, never of the environment. These
    counters live at module scope and only ever grow, so without this reset a
    name would depend on how many case modules were loaded before this one, and
    `gen.py <module>` would emit different names than a full `gen.py` for the
    same case — leaving a tree the full run does not produce, and step names
    that do not match between runs when a red run is being diagnosed.

    Resetting is safe by construction: newman resolves setNextRequest by request
    name WITHIN the collection being run, and one case module produces exactly
    one collection — so uniqueness is only ever required within that scope.

    Held by internal/repohygiene TestGeneratedStepNamesDoNotDependOnHowManyModulesRan.
    """
    _ABS_SEQ[0] = 0
    _POLL_SEQ[0] = 0
    _RYA_SEQ[0] = 0


def load_cases_module(path: Path):
    _reset_step_name_counters()
    spec = importlib.util.spec_from_file_location(path.stem.replace("-", "_"), path)
    mod = importlib.util.module_from_spec(spec)
    # пробрасываем helpers в namespace модуля
    mod.Step = Step
    mod.Case = Case
    mod.assert_status = assert_status
    mod.assert_grpc_code = assert_grpc_code
    mod.assert_unscoped_rejected = assert_unscoped_rejected
    mod.assert_field_violation = assert_field_violation
    mod.save_from_response = save_from_response
    mod.assert_operation_envelope = assert_operation_envelope
    mod.assert_created_at_seconds = assert_created_at_seconds
    mod.poll_operation_until_done = poll_operation_until_done
    mod.retry_until_authorized = retry_until_authorized
    mod.retry_until_present = retry_until_present
    mod.retry_until_state = retry_until_state
    mod.retry_until_absent = retry_until_absent
    mod.assert_op_error = assert_op_error
    mod.assert_op_error_oneof = assert_op_error_oneof
    mod.assert_op_success = assert_op_success
    mod.list_page_block = list_page_block
    mod.name_validation_block = name_validation_block
    mod.labels_validation_block = labels_validation_block
    mod.description_validation_block = description_validation_block
    mod.filter_block = filter_block
    mod.security_injection_block = security_injection_block
    mod.http_method_block = http_method_block
    mod.malformed_body_block = malformed_body_block
    mod.ADMIN_AUTH = ADMIN_AUTH
    mod.MT_INTERNAL_PATH = MT_INTERNAL_PATH
    spec.loader.exec_module(mod)
    return mod


_SELF_RETRY_CALL = "pm.execution.setNextRequest(pm.info.requestName)"
_NAMED_JUMP_RE = re.compile(r"""setNextRequest\(\s*['"]([^'"]+)['"]\s*\)""")
_BUSY_WAIT_RE = re.compile(r"while\s*\(\s*Date\.now\(\)\s*-\s*_")


def audit_jumps(collection: Dict) -> Tuple[List[str], int, int]:
    """Gate the built collection on newman's setNextRequest semantics.

    Two properties, both learned the hard way, and neither visible in a diff:

    (a) A jump target is a NAME, resolved against a collection-wide index that keeps one
        entry per name. A self-loop written as setNextRequest(pm.info.requestName) on a
        step whose name repeats therefore does not resolve to the running step — it lands
        on whichever same-named item the index kept, and the run continues from THERE.
        Landing further down the collection makes every item in between go unsent, and an
        unsent request is absent from newman's report rather than failed: the suite reads
        green on questions nobody asked. So every self-looping step must own its name, and
        every literal jump target must name exactly one item.

    (b) A retry with no wait is not a retry. newman executes test scripts synchronously and
        acts on setNextRequest before any setTimeout callback, so the only way to actually
        space attempts is a busy-wait; without one a 30-iteration loop spans milliseconds
        and gives up while the thing it waits for is still perfectly healthy.

    Returns (findings, items_scanned, loops_scanned) — the census is returned so that
    "no findings" stays distinguishable from "nothing was read"."""
    items: List[Tuple[str, Dict]] = []

    def walk(node: Dict, trail: str):
        for child in node.get("item", []):
            if "item" in child:
                walk(child, f"{trail}{child.get('name', '?')} / ")
            else:
                items.append((trail, child))

    walk(collection, "")
    name_counts: Dict[str, int] = {}
    for _, it in items:
        name_counts[it["name"]] = name_counts.get(it["name"], 0) + 1

    findings: List[str] = []
    loops = 0
    for trail, it in items:
        for ev in it.get("event", []):
            if ev.get("listen") != "test":
                continue
            lines = ev.get("script", {}).get("exec", [])
            for i, line in enumerate(lines):
                # Read the executable part only: a comment ABOUT a jump is not a jump,
                # and a comment about waiting is not a wait (testing.md — a gate that
                # greps raw text finds the защита in the comment explaining it).
                if line.lstrip().startswith("//"):
                    continue
                for target, kind in (
                    [(it["name"], "self")] if _SELF_RETRY_CALL in line else []
                ) + [(m, "named") for m in _NAMED_JUMP_RE.findall(line)]:
                    loops += 1
                    where = f"{trail}{it['name']} (test line {i + 1})"
                    seen = name_counts.get(target, 0)
                    if kind == "self" and seen != 1:
                        findings.append(
                            f"{where}: self-retry on step name {target!r}, which names "
                            f"{seen} items in this collection — the jump will not resolve "
                            f"to this step and the run will skip forward. Give the wrapped "
                            f"step a unique name (see retry_until_* helpers)."
                        )
                    elif kind == "named" and seen != 1:
                        findings.append(
                            f"{where}: jump to {target!r}, which names {seen} items in "
                            f"this collection (needs exactly 1)."
                        )
                    # the last 4 EXECUTABLE lines above the jump (comments do not count
                    # as either a wait or a line of the window)
                    window = [w for w in lines[:i] if not w.lstrip().startswith("//")][-4:]
                    if not any(_BUSY_WAIT_RE.search(w) for w in window):
                        findings.append(
                            f"{where}: retry jump with no busy-wait in the 4 lines above — "
                            f"newman fires setNextRequest before any setTimeout, so this "
                            f"loop spins with no delay and gives up in milliseconds."
                        )
    return findings, len(items), loops


def main(argv: List[str]) -> int:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    want = set(argv[1:])
    found = sorted(CASES_DIR.glob("*.py"))
    if not found:
        print(f"no case files in {CASES_DIR}")
        return 1
    rc = 0
    for f in found:
        res = f.stem
        if want and res not in want:
            continue
        mod = load_cases_module(f)
        cases = getattr(mod, "CASES", [])
        # детект дублей case-id — HARD-FAIL (case-id обязан быть уникален)
        ids = [c.id for c in cases]
        dups = {x for x in ids if ids.count(x) > 1}
        if dups:
            sys.stderr.write(f"[{res}] FAIL — duplicate case-id (должен быть уникален): {sorted(dups)}\n")
            return 1
        col = build_collection(res, cases)
        jump_findings, n_items, n_loops = audit_jumps(col)
        if jump_findings:
            sys.stderr.write(
                f"[{res}] FAIL — unsound retry jump(s); a run would silently skip requests:\n"
            )
            for fnd in jump_findings:
                sys.stderr.write(f"  - {fnd}\n")
            return 1
        out = OUT_DIR / f"{res}.postman_collection.json"
        out.write_text(json.dumps(col, indent=2, ensure_ascii=False))
        print(f"[{res}] {len(cases)} cases → {out.relative_to(ROOT)} "
              f"(jump-audit: {n_loops} retry jump(s) over {n_items} steps, all sound)")
    return rc


if __name__ == "__main__":
    sys.exit(main(sys.argv))
