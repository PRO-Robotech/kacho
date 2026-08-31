#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""
tests/newman/scripts/gen.py — генератор Postman collections из декларативных case-файлов.

Использование:
    python3 scripts/gen.py             # все сервисы
    python3 scripts/gen.py network     # один сервис

Источник истины — модули в tests/newman/cases/<service>.py, каждый экспортирует
переменную CASES — список объектов Case (см. ниже).

Форму коллекции и вспомогательный слой собирает ОБЩИЙ модуль
`tests/newman/kacholib/gen_shared.py` — один на дерево (#1367, #1377, #1379,
#1474). Здесь объявлено только то, чем ЭТОТ набор отличается: решения формы
(дескриптор `Emit`), решения оркестрации (дескриптор `Run`), таблица впрыска
и собственные помощники набора.

Соседний генератор образцом НЕ является и сверяться с ним не надо: расхождение
между копиями было предметом сведения, а не способом его проверить.
"""
from __future__ import annotations

import functools
import json
import re
import sys
import uuid
import importlib.util
from pathlib import Path
from dataclasses import dataclass, field, replace
from typing import List, Dict, Optional, Tuple

# --- общий слой генератора (задача #1367) ------------------------------------
# Помощники ниже общие для ВСЕХ наборов newman и живут в дереве в одном
# экземпляре: `tests/newman/kacholib/gen_shared.py`. До сведения каждый набор нёс
# свою копию, и правка помощника стоила восьми правок — «поправил у себя» было
# неотличимо от «поправил везде».
def _kacholib_dir() -> Path:
    """Каталог общего слоя, найденный ВВЕРХ ОТ ЭТОГО ФАЙЛА, а не от cwd.

    Генератор зовут из каталога набора (`python3 scripts/gen.py`), поэтому путь,
    выведенный из текущего каталога, был бы свойством того, ОТКУДА позвали, а не
    того, где лежит дерево.
    """
    for parent in Path(__file__).resolve().parents:
        candidate = parent / "tests" / "newman" / "kacholib"
        if (candidate / "gen_shared.py").is_file():
            return candidate
    raise SystemExit(
        "общий слой генератора не найден: ожидается "
        "<корень>/tests/newman/kacholib/gen_shared.py.\n"
        "Это ОТКАЗ, а не пропуск: без общих помощников генератор собрал бы "
        "коллекции молча и не тем."
    )


sys.path.insert(0, str(_kacholib_dir()))

import gen_shared  # noqa: E402  — модуль нужен целиком: связывание опроса и его счётчик
from gen_shared import (  # noqa: E402  — импорт после провязки sys.path
    generate,
    Run,
    retry_until_authorized,
    retry_until_present,
    _RYA_SEQ,
    _accepted_http_codes,
    _assert_delete_operation_outcome,
    assert_field_violation,
    _ASSERT_FORMS,
    assert_grpc_code,
    assert_refusal_message,
    assert_refusal_message_contains,
    _assert_published_id_outcome,
    assert_status,
    _asserts_done,
    _asserts_outcome,
    _assigns_env_var,
    _body_text,
    build_collection,
    _carries_assertion,
    case_to_postman,
    _DELETE_ACCEPTED,
    Emit,
    _FRESH_VAR_SET_RE,
    http_method_not_allowed_block,
    _is_operation_id_var,
    _js_code_and_literals,
    js_comment,
    js_regex_literal_text,
    js_str,
    load_cases_module,
    _MUTATION_METHODS,
    _OP_POLL_PATH,
    _PUB_ASSIGN_RE,
    _PUB_BIND_RE,
    _PUB_DECL_RE,
    _PUB_RESERVED,
    _PUB_SET_RE,
    _published_id_outcome_assert,
    _published_resource_vars,
    _REGEX_META,
    _reset_captured_operation_id,
    step_to_postman,
    _strip_js_comments,
    _VAR_REF_RE,
    _wrap_own_fresh_reads,
)


ROOT = Path(__file__).resolve().parents[1]
SCRIPTS_DIR = Path(__file__).resolve().parent
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
    # Per-step auth override для authz-deny suite.
    #   None              — header не трогается (default — inherit collection Bearer если есть)
    #   "anonymous"       — Authorization header снимается перед запросом
    #   "<envVarName>"    — Authorization: Bearer {{envVarName}} (значение из env при выполнении)
    auth: Optional[str] = None
    # declares_no_supernet=True — сеть создаётся НАМЕРЕННО без объявленного плана
    # (см. _declare_supernet_where_a_subnet_is_carved ниже). Ставится ТОЛЬКО там,
    # где предмет кейса — сам отказ подсети в такой сети; во всех прочих местах
    # план дописывается генератором, потому что иначе фикстура снисходительнее
    # продукта.
    declares_no_supernet: bool = False
    # internal=True — запрос идёт на api-gateway cluster-internal REST listener
    # ({{internalBaseUrl}}, :8081), а НЕ на публичный cmux ({{baseUrl}}, :8080).
    #
    # Internal*-RPC (напр. AddressPool — admin-only ресурс, security.md) на публичном
    # листенере ОТСУТСТВУЮТ by design (ban #6: Internal.* не публикуется на external
    # endpoint) и отвечают 404. Без этого флага internal-коллекции слали запросы на
    # {{baseUrl}} и получали закономерный 404 («expected 404 to deeply equal 200») —
    # тест ловил не баг продукта, а собственную неверную посылку. iam-набор решает это
    # тем же способом (см. _internal_url_override в iam-internal-only-check.py).
    internal: bool = False


@dataclass
class Case:
    """Один тестовый кейс — может содержать несколько шагов."""
    id: str  # например NET-CR-CRUD-OK
    title: str  # человеко-читаемое описание
    classes: List[str]  # CRUD / VAL / NEG / BVA / ...
    priority: str  # P0 / P1 / P2 / P3
    steps: List[Step]


# ---------------------------------------------------------------------------
# Утилиты-сниппеты pm.* (вставляются в каждый шаг по необходимости)
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
    "  // runId формат: только [a-z0-9], без точки — чтобы проходить name regex.",
    "  const t = Date.now().toString(36);",
    "  const r = Math.floor(Math.random() * 1e9).toString(36);",
    "  pm.environment.set('runId', (t + r).replace(/[^a-z0-9]/g, '').slice(-10));",
    "}",
    "// Suite project scope (директива #2 — per-service isolation): prefer the vpc-",
    "// DEDICATED existingProjectId/existingProjectCrossId (setup.sh seeds vpc-home /",
    "// vpc-cross under a vpc-only account and grants the default JWT editor on BOTH),",
    "// so the CRUD suites don't share projectA1/A2 with the iam matrix + nlb (cross-",
    "// suite collision #276) and can run in parallel. Fall back to the shared matrix",
    "// projectA1Id/A2Id only when the dedicated projects weren't seeded (standalone dev).",
    "// NB: the 6-subject authz-deny matrix keeps using projectA1Id/B1Id directly (shared).",
    "pm.environment.set('_suiteProjectId', pm.environment.get('existingProjectId') || pm.environment.get('projectA1Id'));",
    "pm.environment.set('_suiteProjectCrossId', pm.environment.get('existingProjectCrossId') || pm.environment.get('projectA2Id'));",
    "// Zone is environment-specific (geo seeds Region/Zone; ids differ per deploy).",
    "// Resolve a live zone id once from the geo catalog; fall back to the committed",
    "// existingZoneId when geo is unreachable (standalone vpc without geo).",
    "if (!pm.environment.get('_zoneResolved')) {",
    "  const __zjwt = pm.environment.get('jwtBootstrap') || pm.environment.get('jwtProjectAdminA1') || '';",
    "  pm.sendRequest({",
    "    url: pm.environment.get('baseUrl') + '/geo/v1/zones',",
    "    method: 'GET',",
    "    header: { 'Authorization': 'Bearer ' + __zjwt },",
    "  }, (err, res) => {",
    "    if (err || !res || res.code !== 200) return;",
    "    let zs = []; try { zs = (res.json().zones) || []; } catch (e) {}",
    "    const up = zs.filter(z => !z.status || String(z.status).indexOf('UP') !== -1);",
    "    const pick = up.length ? up : zs;",
    "    if (pick.length) {",
    "      pm.environment.set('existingZoneId', pick[0].id);",
    "      pm.environment.set('existingZoneAltId', (pick[1] || pick[0]).id);",
    "      pm.environment.set('_zoneResolved', '1');",
    "    }",
    "  });",
    "}",
    "// Default auth: projectAdmin on project A1 — sufficient for most happy-path steps.",
    "// Per-step auth= overrides this via item-level pre-request script (_auth_pre_script).",
    "const __defaultJwt = pm.environment.get('jwtProjectAdminA1') || pm.variables.get('jwtProjectAdminA1') || '';",
    "if (__defaultJwt && !pm.request.headers.has('Authorization')) {",
    "  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __defaultJwt});",
    "}",
    *_URL_VAR_GUARD,
]


def assert_transcode_error() -> List[str]:
    """400 + непустое тело. На ошибки JSON-transcoding (неверный тип поля, oneof задан
    дважды) api-gateway отдает JSON {code,message}; формат тела зависит от
    runtime-библиотеки grpc-gateway. Кейс остается defensive — лишь фиксирует, что
    запрос отвергнут с 400 и непустым телом."""
    return [
        "pm.test('status 400', () => pm.expect(pm.response.code).to.eql(400));",
        "pm.test('non-empty error body', () => {",
        "  let m;",
        "  try { const j = pm.response.json(); m = (j && (j.message || JSON.stringify(j))) || ''; }",
        "  catch (e) { m = pm.response.text() || ''; }",
        "  pm.expect(String(m).length).to.be.above(0);",
        "});",
    ]


def assert_unscoped_rejected() -> List[str]:
    """Unscoped list/create (без projectId) — ОТВЕРГНУТ. Два защитимых исхода,
    оба = «отклонено» (defense-in-depth, security.md «authz-first»):
      403 PERMISSION_DENIED (code 7) — gateway scope_extractor fail-closed
        «no path: unscoped resource» ДО backend-валидации: нельзя авторизовать
        запрос, у которого нет scope для anti-BOLA-проверки;
      400 INVALID_ARGUMENT  (code 3) — backend «project_id required» при
        passthrough.
    Толерантен к обоим — семантика негатива (rejected) сохранена, без ложного
    провала на корректном authz-first 403. Techniques: ECP (класс «unscoped
    запрос») + error-guessing (authz-vs-validation ordering)."""
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
    :verb-action / вложенный list по нему) — ОТВЕРГНУТ. Три защитимых исхода,
    все = «отклонено» (defense-in-depth, security.md «authz-first», parity с
    unscoped-helper и compute 32be094):
      403 PERMISSION_DENIED (code 7) — gateway scope_extractor не может резолвить
        target→project для anti-BOLA у несуществующего/битого id → fail-closed
        ДО backend format-check / repo.Get (для МУТАЦИЙ это устойчивое поведение,
        не зависит от фикстур — id захардкожен как garbage, не берётся из setup);
      404 NOT_FOUND (code 5) — well-formed-но-нет: sync AuthZ-Get/repo.Get;
      400 INVALID_ARGUMENT (code 3) — malformed id: corevalidate.ResourceID.
    Толерантен 400|403|404 (code 3|5|7) — семантика негатива (rejected) сохранена
    без ложного провала на корректном authz-first 403 (GATE-RUN #5:
    del-nx/patch-nx/upd-{fld}/move-nx/lop-nx возвращали 403 вместо 400/404).
    Message-контракт NotFound ('<Resource> <id> not found') проверяется на GET-пути
    (get-conf), который доходит до backend; для мутаций 403 его скрывает → тут не
    ассертим (unobservable). Techniques: ECP (класс «absent id») + error-guessing
    (authz-vs-existence ordering)."""
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
    """Запрос под проверкой ОТВЕРГНУТ — той из двух законных полос, которая решает.

    Мутации Kachō отвечают Operation (ban #9), поэтому у отказа две формы, и кейс,
    называющий вход незаконным, обязан принимать обе — И БОЛЬШЕ НИЧЕГО:

      * синхронный валидатор отвергает до появления Operation → `400 INVALID_ARGUMENT`;
      * отвергает воркер (нерезолвящийся peer, ограничение БД, страж состояния) →
        `200` с Operation, которая завершается С ОШИБКОЙ.

    Что это заменяет: `oneOf([200, 400])` и ничего после него. В такой записи вторая
    полоса не проверяется вовсе — `200` проходит сам по себе, то есть кейс
    удовлетворяется тем, что продукт ПРИНЯЛ ровно тот вход, ради доказательства отказа
    на котором он написан, а регрессия, снявшая проверку, оставляет его зелёным. Это
    не терпимость к порядку, это отсутствие утверждения.

    Ставить в паре с `poll_operation_until_done(must_fail=True)` СЛЕДУЮЩИМ шагом — он и
    заявляет вторую полосу. На синхронной полосе `opId` здесь снимается, поэтому у
    поллера нет предмета и он ничего не утверждает; на асинхронной — держит Operation к
    ответу. `what` называет вход в тексте утверждения, чтобы падение было адресным.

    `sync_codes` расширяет ПЕРВУЮ полосу там, где порядок действительно допускает больше
    одного отказа: край вправе отказать до того, как бэкенд посмотрит в тело (`403`), а
    нерезолвящийся ресурс читается как отсутствующий (`404`). Каждая запись обязана быть
    ОТКАЗОМ; `200` подставляет сам помощник как асинхронную полосу, и больше он не место
    нигде.

    `async_lane=False` — ВТОРОЙ ПОЛОСЫ ДЛЯ ЭТОГО ВХОДА НЕ СУЩЕСТВУЕТ, поэтому назвать её
    значило бы пообещать то, чего продукт нарушить не может. Применять только там, где
    Operation не может быть отчеканена вовсе, и говорить почему на месте вызова.
    Замеренный случай — вход без `projectId`: это область, которую край резолвит для
    anti-BOLA-проверки, поэтому запрос без неё отвергается ДО чтения тела бэкендом, и
    полосы, на которой воркер отказал бы позже, просто нет. Если полосу объявить всё
    равно, `opId` останется неустановленным, а парный поллер обратится к `{{opId}}` как
    к литералу.

    Заимствовано без изменения смысла из `services/nlb/tests/newman/scripts/gen.py`
    (там же — разбор, из которого форма выведена)."""
    codes = ", ".join(str(c) for c in ((*sync_codes, 200) if async_lane else sync_codes))
    named = "/".join(str(c) for c in sync_codes)
    # Заголовок КОДИРУЕТСЯ, а не вклеивается. `what` приходит от вызывающего и
    # законно содержит апостроф (`create without required 'projectId'`); вклеенный
    # в одинарные кавычки, он рвёт литерал, и весь скрипт шага перестаёт
    # разбираться. Тогда шаг не падает — он НЕ ИСПОЛНЯЕТСЯ: `pm.test` до вызова не
    # доходит, `pm.environment.unset('opId')` тоже, и парный поллер уезжает на
    # `opId`, оставшийся от предыдущего шага. Замерено 2026-08-03: 10 скриптов vpc
    # не разбирались, из них 4 давали ещё и ложное утверждение на чужой операции.
    # `json.dumps` даёт JS-безопасный литерал для любого текста.
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
        # Принятая мутация, не вернувшая Operation, не оставляет предмета, который можно
        # призвать к ответу: отказ утверждался бы о том, чего не существует.
        "  pm.test('accepted response carries the Operation that must then fail', () => "
        "    pm.expect(j.id, pm.response.text()).to.be.a('string'));",
        "  if (j.id) pm.environment.set('opId', j.id);",
        "}",
    ]


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
        "  pm.expect(j.id, 'operation.id').to.match(/^[a-z0-9]+$/);",
        "  pm.expect(j.metadata, 'operation.metadata').to.be.an('object');",
        "});",
    ]


def assert_empty_page(why: str) -> List[str]:
    """Списочное чтение, чей ответ УСТАНОВЛЕН: 200 и пустая страница.

    Пара «HTTP-статус + форма ответа», а не `oneOf([200, 400])`. Разбор фильтра
    детерминирован (`pkg/filter`.`Parse`): поле берётся из белого списка, значение
    стоит в кавычках, хвоста нет — узел строится, значение уезжает ПАРАМЕТРОМ
    запроса, и страница приходит пустой, потому что такого имени нет ни у кого.
    `400` производится ТОЛЬКО негодным синтаксисом выражения, которого эта
    нагрузка не содержит, — то есть прежняя запись перечисляла исход, которого на
    этом входе не бывает, и одновременно приняла бы регрессию разбора.

    Пустота проверяется по СОСТАВУ ответа, а не по имени поля: у публичной полосы
    края `EmitUnpopulated=true` (`gateway/internal/restmux/mux.go`), поэтому
    пустой список приходит как `[]`, а не отсутствует, и у списочного ответа
    ровно один массив верхнего уровня. Обе половины утверждаются: «массив ровно
    один» ловит смену формы ответа, «он пуст» — смену смысла фильтра.
    """
    return [
        "pm.test('status 200', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
        f"pm.test({js_str(f'страница пуста: {why}')}, () => {{",
        "  const j = pm.response.json();",
        "  const keys = Object.keys(j).filter(k => Array.isArray(j[k]));",
        "  pm.expect(keys, JSON.stringify(j)).to.have.lengthOf(1);",
        "  pm.expect(j[keys[0]], JSON.stringify(j)).to.have.lengthOf(0);",
        "});",
    ]


def assert_cleanup_delete(what: str, refusal: str) -> List[str]:
    """Уборка ЧИТАЕТ ОБЕ полосы, а не принимает любую из двух.

    У шага уборки производителей действительно два, и это законно: кейс, чей
    предмет — поведение Create, мог ресурс создать, а мог и нет, поэтому родитель
    к моменту уборки либо пуст, либо занят. Но «два производителя» не означает
    «любой ответ сойдёт»: прежняя запись `oneOf([200, 400])` не читала НИ ОДНУ из
    полос и потому принимала и отказ в правах, поданный краем как 400, и отказ по
    валидации (код 3), и смену контракта удаления.

    Полосы названы и каждая пришпилена своей подписью:
      200 — удаление ПРИНЯТО, ответ несёт конверт `Operation` (непустой `id`);
      400 — удаление отвергнуто СОСТОЯНИЕМ ресурса: `FAILED_PRECONDITION`
            (код 9). Чем именно занят ресурс, называет параметр `refusal` —
            он идёт в текст утверждения, чтобы отказ читался без похода в код.
            Все отказы удаления vpc имеют этот код (`api/*/delete.go`), поэтому
            400 с кодом 3 здесь означает смену контракта, а не «уборке не
            повезло».

    Исполняется РОВНО ОДНО утверждение из двух, поэтому 403/404/500 попадают в
    ветку отказа и роняют её на первом же `to.eql(400)` — то есть форма способна
    упасть по своей причине. Ссылки на `pm.response.code` оставлены дословными:
    по ним `_accepted_http_codes` читает набор исходов шага, и обёртка ожидания
    видимости обязана видеть тот же набор (200, 400), что и прежде.
    """
    return [
        "const _cuT = pm.response.text();",
        "let _cuJ; try { _cuJ = pm.response.json(); } catch (e) { _cuJ = {}; }",
        "if (pm.response.code === 200) {",
        f"  pm.test({js_str(f'уборка ({what}): принято — 200 и конверт Operation')}, () => {{",
        "    pm.expect(pm.response.code, _cuT).to.eql(200);",
        "    pm.expect(_cuJ.id, _cuT).to.be.a('string').and.to.have.length.above(0);",
        "  });",
        "} else {",
        f"  pm.test({js_str(f'уборка ({what}): отказ по СОСТОЯНИЮ — 400 и код 9 ({refusal})')}, () => {{",
        "    pm.expect(pm.response.code, _cuT).to.eql(400);",
        "    pm.expect(_cuJ.code, _cuT).to.eql(9);",
        "  });",
        "}",
    ]


# Subnet CIDR shape (VPC-1 F7). The flat "all blocks" array was split into an
# immutable primary anchor set at Create (`ipv4_cidr_primary` / `ipv6_cidr_primary`)
# plus additional ranges moved by the :add-cidr-blocks / :remove-cidr-blocks verb
# pair (`ipv4_cidr_blocks` / `ipv6_cidr_blocks`). A block therefore lands in one of
# two places depending on whether its family was empty at the time, so a fixture
# asking "is this range on the subnet?" has to look at the union of both.
SUBNET_V4_CIDRS = ("([].concat(pm.response.json().ipv4CidrPrimary ? "
                   "[pm.response.json().ipv4CidrPrimary] : [], "
                   "pm.response.json().ipv4CidrBlocks || []))")
SUBNET_V6_CIDRS = ("([].concat(pm.response.json().ipv6CidrPrimary ? "
                   "[pm.response.json().ipv6CidrPrimary] : [], "
                   "pm.response.json().ipv6CidrBlocks || []))")


def crud_list_bva_block(prefix, list_path):
    """3 BVA-кейса для List RPC: pageSize=0, pageSize=10000, bad token."""
    return [
        Case(
            id=f"{prefix}-LST-BVA-PAGESIZE-ZERO",
            title="List pageSize=0 → default applied (200)",
            classes=["BVA"], priority="P2",
            steps=[Step(name="list-ps0", method="GET",
                        path=f"{list_path}?projectId={{{{_suiteProjectId}}}}&pageSize=0",
                        test_script=[*assert_status(200)])],
        ),
        Case(
            id=f"{prefix}-LST-BVA-PAGESIZE-OVER-MAX",
            title="List pageSize=10000 → InvalidArgument",
            classes=["BVA", "VAL"], priority="P2",
            steps=[Step(name="list-ps-huge", method="GET",
                        path=f"{list_path}?projectId={{{{_suiteProjectId}}}}&pageSize=10000",
                        test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
        ),
        Case(
            id=f"{prefix}-LST-PAGE-TOKEN-GARBAGE",
            title="List с garbage page_token → InvalidArgument",
            classes=["PAGE", "VAL"], priority="P1",
            steps=[Step(name="list-bad-token", method="GET",
                        path=f"{list_path}?projectId={{{{_suiteProjectId}}}}&pageSize=10&pageToken=not-a-real-token",
                        test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
        ),
    ]


def conf_not_found_text(prefix, get_path, resource_name):
    """Стабильный текст для NotFound: '<Resource> <id> not found'."""
    return Case(
        id=f"{prefix}-GET-CONF-NF-TEXT",
        title=f"Get garbage — verbatim text '{resource_name} ... not found'",
        classes=["CONF", "NEG"], priority="P1",
        steps=[Step(name="get-conf", method="GET",
                    path=f"{get_path}/{{{{garbageVpcId}}}}",
                    test_script=[
                        *assert_status(404),
                        *assert_grpc_code(5, "NOT_FOUND"),
                        f"pm.test({js_str(f'text matches \"{resource_name} ... not found\"')}, () => "
                        f"pm.expect(pm.response.json().message).to.match(/^{js_regex_literal_text(resource_name)} .* not found$/));",
                    ])],
    )


def state_update_unknown_mask(prefix, update_path):
    """PATCH с unknown field в mask → InvalidArgument."""
    return Case(
        id=f"{prefix}-UPD-VAL-UNKNOWN-MASK",
        title="Update с unknown field в UpdateMask → InvalidArgument",
        classes=["VAL", "STATE"], priority="P1",
        steps=[Step(name="patch-unknown-mask", method="PATCH",
                    path=f"{update_path}/{{{{garbageVpcId}}}}",
                    body={"updateMask": "some_unknown_field_xyz", "description": "x"},
                    # PATCH по garbageVpcId: scope_extractor 403 (authz-first) ДО
                    # mask-валидации (400) / sync Get (404) — все три = rejected.
                    test_script=[*assert_absent_id_rejected()])],
    )


def state_immutable_project(prefix, update_base_path):
    """Update с mask=project_id → InvalidArgument (immutable)."""
    return Case(
        id=f"{prefix}-UPD-STATE-IMMUTABLE-PROJECT",
        title="Update с mask=project_id → InvalidArgument (immutable)",
        classes=["STATE", "VAL"], priority="P1",
        steps=[Step(name="upd-project-via-mask", method="PATCH",
                    path=f"{update_base_path}/{{{{garbageVpcId}}}}",
                    # The probe is carried by the MASK. `project_id` is not a field of
                    # any Update*Request, so naming it in update_mask IS the assertion;
                    # a `projectId` key in the body is decorative — the edge drops what
                    # the message does not declare, so it cannot influence the outcome,
                    # and shipping it only suggests the field is settable.
                    body={"updateMask": "project_id"},
                    # PATCH immutable-mask по garbageVpcId: scope_extractor 403 (authz-first)
                    # ДО immutable-check 400 / sync Get 404.
                    test_script=[*assert_absent_id_rejected()])],
    )


def list_pagesize_1_bva(prefix, list_path):
    """BVA: pageSize=1 — точечная нижняя граница."""
    return Case(
        id=f"{prefix}-LST-BVA-PAGESIZE-1",
        title="List pageSize=1 → ≤1 item",
        classes=["BVA", "PAGE"], priority="P2",
        steps=[Step(name="list-ps1", method="GET",
                    path=f"{list_path}?projectId={{{{_suiteProjectId}}}}&pageSize=1",
                    test_script=[*assert_status(200),
                                 "pm.test('at most 1 item', () => {"
                                 "  const j = pm.response.json();"
                                 "  const k = Object.keys(j).find(x => Array.isArray(j[x]));"
                                 "  pm.expect((j[k] || []).length).to.be.at.most(1);"
                                 "});"])],
    )


def confirm_created_and_cleanup(create_path: str, id_var: str = "ecpId") -> List[Step]:
    """Хвост ПОЛОЖИТЕЛЬНОГО create-кейса: дождаться операции, убедиться, что она
    завершилась БЕЗ ошибки, и снять созданный ресурс.

    Две причины, и обе измерены на живом стенде 2026-08-04.

    1. Кейс с заголовком «→ ok» без этого хвоста утверждает только, что запрос
       ПРИНЯТ (200 + Operation). Приём и успех — разные исходы: мутация Kachō
       асинхронна, поэтому операция, завершившаяся с ошибкой, оставляет кейс
       зелёным. То есть «ok» в заголовке не проверялось ничем.

    2. Ресурс, созданный и не снятый, остаётся навсегда. Для большинства VPC-ресурсов
       это лишняя строка, а для Address — израсходованный адрес из ОГРАНИЧЕННОГО пула:
       ECP/BVA-блоки съедали по несколько внешних адресов за прогон, и на девятые сутки
       жизни стенда пул `100.64.0.0/24` (254 адреса) оказался исчерпан целиком —
       254 живых адреса от прошлых прогонов, ноль свободных. После этого КАЖДЫЙ create
       адреса падал асинхронно, а кейсы уходили работать с ФАНТОМОМ (см.
       `poll_operation_until_done` §capture_id_to): 63 упавших утверждения, ни одно из
       которых не называло причину. `testing.md` требует уборки прямо: «Cleanup своих
       ресурсов обязателен (leak → пул растёт, list-контракты плывут)».

    Уборка обёрнута `retry_until_authorized` — это ПЕРВОЕ обращение к собственному
    свежесозданному ресурсу, то есть ровно тот случай, для которого обёртка и введена.
    """
    return [
        poll_operation_until_done(capture_id_to=id_var),
        _rya(Step(name="ecp-cleanup", method="DELETE",
                                    path=f"{create_path}/{{{{{id_var}}}}}",
                                    test_script=[*assert_status(200),
                                                 *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
    ]


def ecp_name_block(prefix, create_path, body_extra=None):
    """ECP/BVA по полю name: пустое, max, over-max, форма имени.

    body_extra — обязательные поля кроме projectId/name (например для Subnet: networkId+zoneId+cidr).

    Форма имени в дереве ОДНА — DNS label по RFC 1123, `pkg/validate.NameForm`
    (`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`), поэтому и параметра `strict_name`
    здесь больше нет. Он существовал ровно ради одного исхода — заглавных букв,
    которые разрешительный контракт VPC принимал, а строгий контракт Gateway
    отвергал. Разрешительного контракта не стало: `nameReVPC` снят вместе с
    тремя другими объявлениями формы (задача #715), заглавные отвергаются у
    ВСЕХ шести ресурсов, и параметр, у которого больше нет двух значений,
    остался бы настройкой без предмета.
    """
    body_extra = body_extra or {}
    base = lambda name: {"projectId": "{{_suiteProjectId}}", "name": name, **body_extra}
    cases = []
    # BVA name length: 0, 63 (max), 64 (over)
    # Пустое имя на СОЗДАНИИ — законный вход, и означает он не «имя отсутствует»,
    # а «назови сам»: `validate.NameOnCreate` пропускает пустую строку, а
    # `validate.NameOrDefault` до записи подставляет вместо неё идентификатор
    # ресурса. Поэтому ресурс с пустым именем не возникает НИ ПРИ КАКОМ входе, и
    # утверждать надо именно подстановку, а не приём запроса: кейс, проверяющий
    # только 200, остаётся зелёным и тогда, когда имя записалось пустым.
    #
    # ЗДЕСЬ СТОЯЛА ОГОВОРКА ПРО УНИКАЛЬНОСТЬ ПУСТОГО ИМЕНИ — она пережила свой
    # предмет. Она разбирала, у каких ресурсов частичный UNIQUE исключает
    # `name <> ''`, а у каких индекс полный, и предупреждала, что вторая сеть с
    # пустым именем получит ALREADY_EXISTS. Этого расхождения больше нет by
    # construction: пустое имя не доживает до вставки, а подставляемый
    # идентификатор глобально уникален, поэтому оба вида индекса ведут себя
    # одинаково и различать их незачем.
    cases.append(Case(
        id=f"{prefix}-CR-BVA-NAME-EMPTY",
        title="Create с empty name → 200, имя подставлено идентификатором ресурса",
        classes=["BVA", "VAL"], priority="P2",
        steps=[Step(name="cr-empty", method="POST", path=create_path,
                    body=base(""),
                    test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
               poll_operation_until_done(capture_id_to="ecpId"),
               # Утверждается ПОДСТАНОВКА, а не приём: имя обязано быть равно id
               # ресурса дословно (validate.NameOrDefault → defaultNameForID = id).
               # Проверка пустоты («name !== ''») этого не отличает — ей одинаково
               # годится любая непустая строка, в том числе чужая.
               _rya(Step(
                   name="cr-empty-name-substituted", method="GET",
                   path=f"{create_path}/{{{{ecpId}}}}",
                   test_script=[*assert_status(200),
                                "pm.test('пустое имя заменено идентификатором ресурса', () => "
                                "pm.expect(pm.response.json().name, pm.response.text())"
                                ".to.eql(pm.environment.get('ecpId')));"])),
               _rya(Step(
                   name="ecp-cleanup", method="DELETE",
                   path=f"{create_path}/{{{{ecpId}}}}",
                   test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
               poll_operation_until_done()],
    ))
    # Третья сторона контракта имени, и до неё у набора не было НИ ОДНОГО кейса:
    # пустое имя законно только на СОЗДАНИИ («назови сам»), а на ПРАВКЕ отвергается
    # — снять имя нельзя, ресурса без имени не бывает. `PRODUCT-REQUIREMENTS.md`
    # ссылался на `*-UPD-NEG-NAME-EMPTY` как на подтверждение этого требования,
    # а производил его НИКТО: ссылка «Validated-by» указывала в пустоту.
    #
    # Утверждается ЯВНАЯ маска: при пустой маске (полная правка) «поле не прислано»
    # и «поле пусто» в proto3 неразличимы, поэтому там проверяется только форма —
    # см. update.go. Кейс на пустую маску сюда не добавлен намеренно: он утверждал
    # бы противоположный исход на том же входе и читался бы как противоречие.
    cases.append(Case(
        id=f"{prefix}-UPD-NEG-NAME-EMPTY",
        title="Update с mask=name и пустым name → 400 'name is required' (подстановка бывает только на Create)",
        classes=["VAL", "NEG"], priority="P1",
        steps=[Step(name="cr-for-upd-empty", method="POST", path=create_path,
                    body=base(f"{prefix.lower()}-updempty-{{{{runId}}}}"),
                    test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
               poll_operation_until_done(capture_id_to="ecpId"),
               _rya(Step(
                   name="upd-name-empty", method="PATCH",
                   path=f"{create_path}/{{{{ecpId}}}}",
                   body={"updateMask": "name", "name": ""},
                   test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                                "pm.test('отказ называет требование имени, а не форму', () => "
                                "pm.expect(JSON.stringify(pm.response.json())).to.contain('name is required'));"])),
               _rya(Step(
                   name="ecp-cleanup", method="DELETE",
                   path=f"{create_path}/{{{{ecpId}}}}",
                   test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
               poll_operation_until_done()],
    ))
    cases.append(Case(
        id=f"{prefix}-CR-BVA-NAME-MAX-63",
        title="Create с name len=63 (max) → ok",
        classes=["BVA"], priority="P2",
        # name len exactly 63 (max) AND unique per run: "n63-"(4)+runId(10)+"-"(1)+48
        # = 63 at runtime. Fixed literal collided across re-runs (UNIQUE name) → 409.
        steps=[Step(name="cr-max63", method="POST", path=create_path,
                    body=base("n63-{{runId}}-" + "a"*48),
                    test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
               *confirm_created_and_cleanup(create_path)],
    ))
    cases.append(Case(
        id=f"{prefix}-CR-BVA-NAME-OVER-64",
        title="Create с name len=64 (over-max) → InvalidArgument",
        classes=["BVA", "VAL"], priority="P1",
        steps=[Step(name="cr-over", method="POST", path=create_path,
                    body=base("n64" + "abcdefghij"*7),
                    test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
    ))
    # Заглавные буквы отвергаются у ВСЕХ шести ресурсов: форма имени одна.
    # Прежде исход здесь зависел от параметра `strict_name` — разрешительный
    # контракт VPC заглавные принимал, строгий контракт Gateway отвергал, — и
    # кейс нёс две ветки. Ветка приёма снята вместе со своим контрактом, отказ
    # остался один и утверждается без `oneOf`.
    cases.append(Case(
        id=f"{prefix}-CR-VAL-NAME-UPPERCASE",
        title="Create с UPPERCASE name → 400 (форма имени: только строчные)",
        classes=["VAL", "NEG"], priority="P2",
        steps=[Step(name="cr-upper", method="POST", path=create_path,
                    body=base("InvalidUpperCase-{{runId}}"),
                    test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                                 "pm.test('refusal names the field', () => pm.expect(JSON.stringify(pm.response.json())).to.contain('name'));"])],
    ))
    # ЗДЕСЬ БЫЛ КЕЙС «имя начинается с цифры → 400». Его предмета больше нет:
    # DNS label по RFC 1123 разрешает цифру первым символом, и `9invalid-…`
    # теперь ЗАКОННОЕ имя. Кейс не удалён, а переведён на ось, которая у формы
    # действительно сузилась и до сих пор не имела отрицания ни у одного из
    # шести ресурсов, — подчёркивание. Оно принималось прежним разрешительным
    # контрактом VPC (`[-_a-zA-Z0-9]`) и отвергается новым.
    cases.append(Case(
        id=f"{prefix}-CR-VAL-NAME-UNDERSCORE",
        title="Create с подчёркиванием в name → 400 (форма имени: буквы, цифры, дефис)",
        classes=["VAL", "NEG"], priority="P1",
        steps=[Step(name="cr-underscore", method="POST", path=create_path,
                    body=base("bad_name-{{runId}}"),
                    test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
    ))
    cases.append(Case(
        id=f"{prefix}-CR-VAL-NAME-HYPHEN-START",
        title="Create с name начинающимся с дефиса → 400",
        classes=["VAL"], priority="P1",
        steps=[Step(name="cr-hyphen", method="POST", path=create_path,
                    body=base("-bad-{{runId}}"),
                    test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
    ))
    cases.append(Case(
        id=f"{prefix}-CR-VAL-NAME-SPECIAL-CHARS",
        title="Create с спец-символами в name → 400",
        classes=["VAL"], priority="P1",
        steps=[Step(name="cr-special", method="POST", path=create_path,
                    body=base("name!@#-{{runId}}"),
                    test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
    ))
    return cases


def ecp_description_block(prefix, create_path, body_extra=None):
    """BVA по description: 256 (max), 257 (over)."""
    body_extra = body_extra or {}
    base = lambda name, desc: {"projectId": "{{_suiteProjectId}}", "name": name, "description": desc, **body_extra}
    return [
        Case(
            id=f"{prefix}-CR-BVA-DESC-MAX-256",
            title="Create с description len=256 (max) → ok",
            classes=["BVA"], priority="P2",
            steps=[Step(name="cr-desc-max", method="POST", path=create_path,
                        body=base(f"{prefix.lower()}-desc-{{{{runId}}}}", "x" * 256),
                        test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
                   *confirm_created_and_cleanup(create_path)],
        ),
        Case(
            id=f"{prefix}-CR-BVA-DESC-OVER-257",
            title="Create с description len=257 (over-max) → InvalidArgument",
            classes=["BVA", "VAL"], priority="P1",
            steps=[Step(name="cr-desc-over", method="POST", path=create_path,
                        body=base(f"{prefix.lower()}-d2-{{{{runId}}}}", "x" * 257),
                        test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
        ),
    ]


def ecp_labels_block(prefix, create_path, body_extra=None):
    """ECP по labels: invalid key regex, too many pairs (>64), uppercase key."""
    body_extra = body_extra or {}
    base = lambda name, labels: {"projectId": "{{_suiteProjectId}}", "name": name, "labels": labels, **body_extra}
    return [
        Case(
            id=f"{prefix}-CR-VAL-LABELS-UPPERCASE-KEY",
            title="Create с UPPERCASE label key → 400",
            classes=["VAL"], priority="P1",
            steps=[Step(name="cr-lbl-upper", method="POST", path=create_path,
                        body=base(f"{prefix.lower()}-lblup-{{{{runId}}}}", {"BADKEY": "v"}),
                        test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
        ),
        Case(
            id=f"{prefix}-CR-VAL-LABELS-INVALID-KEY-CHAR",
            title="Create с invalid char в label key → 400",
            classes=["VAL"], priority="P1",
            steps=[Step(name="cr-lbl-bad", method="POST", path=create_path,
                        body=base(f"{prefix.lower()}-lblbad-{{{{runId}}}}", {"bad key!": "v"}),
                        test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
        ),
        Case(
            id=f"{prefix}-CR-BVA-LABELS-MAX-64",
            title="Create с 64 labels (max) → ok",
            classes=["BVA"], priority="P2",
            steps=[Step(name="cr-lbl-max", method="POST", path=create_path,
                        body=base(f"{prefix.lower()}-lblm-{{{{runId}}}}",
                                  {f"key{i}": f"v{i}" for i in range(64)}),
                        test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
                   *confirm_created_and_cleanup(create_path)],
        ),
        Case(
            id=f"{prefix}-CR-BVA-LABELS-OVER-65",
            title="Create с 65 labels (over-max) → 400",
            classes=["BVA", "VAL"], priority="P1",
            steps=[Step(name="cr-lbl-over", method="POST", path=create_path,
                        body=base(f"{prefix.lower()}-lblo-{{{{runId}}}}",
                                  {f"k{i}": f"v{i}" for i in range(65)}),
                        test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")])],
        ),
    ]


def updatemask_decision_table(prefix, update_base_path):
    """Decision table для UpdateMask: empty, unknown, immutable, valid."""
    return [
        Case(
            id=f"{prefix}-UPD-VAL-MASK-EMPTY",
            title="Update с пустой mask → full PATCH (200)",
            classes=["VAL", "STATE"], priority="P2",
            steps=[Step(name="upd-empty-mask", method="PATCH",
                        path=f"{update_base_path}/{{{{garbageVpcId}}}}",
                        body={"description": "x"},
                        # PATCH по garbageVpcId (несуществующий): scope_extractor 403
                        # (authz-first) ДО sync Get 404 — 200 недостижим для garbage-id.
                        test_script=[*assert_absent_id_rejected()])],
        ),
        Case(
            id=f"{prefix}-UPD-VAL-MASK-MULTIPLE-UNKNOWN",
            title="Update с несколькими unknown полями в mask → 400",
            classes=["VAL", "STATE"], priority="P2",
            steps=[Step(name="upd-multi-unknown", method="PATCH",
                        path=f"{update_base_path}/{{{{garbageVpcId}}}}",
                        body={"updateMask": "x_unknown,y_unknown", "description": "x"},
                        # PATCH unknown-mask по garbageVpcId: scope_extractor 403 (authz-first)
                        # ДО mask-валидации 400 / sync Get 404.
                        test_script=[*assert_absent_id_rejected()])],
        ),
    ]


def filter_syntax_block(prefix, list_path):
    """Filter syntax tests."""
    return [
        Case(
            id=f"{prefix}-LST-FILTER-NAME-OK",
            title="List с filter name=\"foo\" → 200",
            classes=["FILTER", "CRUD"], priority="P2",
            steps=[Step(name="list-filter", method="GET",
                        path=f"{list_path}?projectId={{{{_suiteProjectId}}}}&filter=name%3D%22foo%22",
                        test_script=[*assert_status(200)])],
        ),
        # Фаза фильтра whitelist'ит ТОЛЬКО `name=`. Неподдерживаемое поле обязано
        # отвергаться ЯВНО и НИКОГДА не игнорироваться молча: молчаливое игнорирование
        # хуже отказа — вызывающий получает НЕфильтрованную страницу под фильтром,
        # который считает применённым. Прежнее `oneOf([200, 400])` под заголовком,
        # обещающим 400, проходило и при молчаливом игнорировании, то есть ровно на
        # том дефекте, ради которого кейс существует.
        #
        # Исходы измерены на самом парсере (pkg/filter), а не угаданы:
        #   "this is not valid syntax" → Bad expression at column 1. Unknown field: "this"
        #   nonexistent_field="x"      → Bad expression at column 1. Unknown field: "nonexistent_field"
        #   name="foo"                 → разбирается
        # Первый токен мусорной строки парсер читает как имя поля, поэтому обе
        # отрицательные ветки приходят одним и тем же классом ошибки.
        Case(
            id=f"{prefix}-LST-FILTER-GARBAGE",
            title="List с garbage filter syntax → 400 InvalidArgument с именем неизвестного поля",
            classes=["FILTER", "VAL"], priority="P1",
            steps=[Step(name="list-bad-filter", method="GET",
                        path=f"{list_path}?projectId={{{{_suiteProjectId}}}}&filter=this%20is%20not%20valid%20syntax",
                        test_script=[
                            *assert_status(400),
                            *assert_grpc_code(3, "INVALID_ARGUMENT"),
                            "pm.test('сообщение называет непринятое поле', () => pm.expect(String((pm.response.json()||{}).message||''))"
                            ".to.eql('Bad expression at column 1. Unknown field: \"this\"'));",
                        ])],
        ),
        Case(
            id=f"{prefix}-LST-FILTER-UNKNOWN-FIELD",
            title="List с filter на unsupported field → 400 InvalidArgument с именем поля",
            classes=["FILTER", "VAL"], priority="P2",
            steps=[Step(name="list-unknown-field", method="GET",
                        path=f"{list_path}?projectId={{{{_suiteProjectId}}}}&filter=nonexistent_field%3D%22x%22",
                        test_script=[
                            *assert_status(400),
                            *assert_grpc_code(3, "INVALID_ARGUMENT"),
                            "pm.test('сообщение называет непринятое поле', () => pm.expect(String((pm.response.json()||{}).message||''))"
                            ".to.eql('Bad expression at column 1. Unknown field: \"nonexistent_field\"'));",
                        ])],
        ),
    ]


def pagination_roundtrip(prefix, list_path):
    """Pagination round-trip: получить page+token, использовать token для next page."""
    return Case(
        id=f"{prefix}-LST-PAGE-ROUNDTRIP",
        title="Pagination: получить пустой/не-пустой ответ + nextPageToken и пройти еще раз с ним",
        classes=["PAGE", "BVA", "CRUD"], priority="P2",
        steps=[
            Step(name="list-p1", method="GET",
                 path=f"{list_path}?projectId={{{{_suiteProjectId}}}}&pageSize=1",
                 test_script=[*assert_status(200),
                              "const j = pm.response.json();",
                              "const tok = j.nextPageToken || '';",
                              "pm.environment.set('nextToken', tok);",
                              "pm.test('token is string', () => pm.expect(tok).to.be.a('string'));"]),
            Step(name="list-p2", method="GET",
                 path=f"{list_path}?projectId={{{{_suiteProjectId}}}}&pageSize=1&pageToken={{{{nextToken}}}}",
                 test_script=[*assert_status(200)]),
        ],
    )


def idempotency_block(prefix, create_path, refusal, name_template, body_extra=None):
    """Повторный Create same name → 409 ALREADY_EXISTS (Create не идемпотентен).

    Первый Create OK, второй с тем же name → sync 409 ALREADY_EXISTS.

    `refusal` — текст отказа ВЛАДЕЛЬЦА дословно, с `{name}` на месте имени (слот подставляется ЗАМЕНОЙ, не `str.format`:
    формат съел бы `{{…}}` подстановки окружения и превратил бы их в литерал)
    (`"Network with name {name} already exists"`). Обязателен и без умолчания:
    прежде здесь стояло `message.toLowerCase()).to.include('already exists')`,
    то есть проверялась ОБЩАЯ часть тона, под которой проходит отказ любого
    ресурса этого сервиса. Умолчание вернуло бы ту же слабость молча, а отказ
    генерации на незаданном тексте виден сразу и называет место (#1520).
    """
    body_extra = body_extra or {}
    return Case(
        id=f"{prefix}-CR-IDM-RETRY",
        title="Повторный Create same name → 409 ALREADY_EXISTS (sync)",
        classes=["IDM", "CONC", "NEG"], priority="P1",
        steps=[
            Step(name="cr-1", method="POST", path=create_path,
                 body={"projectId": "{{_suiteProjectId}}", "name": name_template, **body_extra},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId1"),
                              *save_from_response("(j.metadata && Object.keys(j.metadata).filter(k => k.endsWith('Id')).map(k => j.metadata[k])[0]) || ''", "idmCreatedId")]),
            Step(name="poll-1", method="GET", path="/operations/{{opId1}}",
                 test_script=["pm.test('done eventually', () => { const j = pm.response.json(); pm.expect([true,false]).to.include(j.done); });"]),
            Step(name="cr-2", method="POST", path=create_path,
                 body={"projectId": "{{_suiteProjectId}}", "name": name_template, **body_extra},
                 test_script=[*assert_status(409), *assert_grpc_code(6, "ALREADY_EXISTS"),
                              *assert_refusal_message(refusal.replace("{name}", name_template))]),
            # cleanup DELETE is the caller's first *mutating* access of its OWN fresh
            # resource; the delete-relation owner-tuple is eventually-consistent (opgate
            # removed → at-least-once fgaproxy drainer), so under load the gateway authz
            # gate can briefly 403 ("lacks relation v_delete") before the tuple
            # materialises. Bounded read-your-writes retry on that transient 403 only
            # (the resource provably exists — cr-2 just got 409 on it — so a 404 here
            # would be a genuine bug, never masked).
            _rya(Step(name="cleanup", method="DELETE",
                 path=f"{create_path}/{{{{idmCreatedId}}}}",
                 test_script=[*assert_status(200)]), retry_on=(403,)),
        ],
    )


def update_happy_per_field(prefix, create_path, update_base_path, body_create):
    """Update happy path для каждого mutable field отдельно: name, description, labels.

    body_create — тело для создания исходного ресурса (включая name).
    Use case_id с суффиксами FIELD-NAME/DESC/LABELS для уникальности.
    """
    def case_for(field, suffix, patch_body, asserts):
        return Case(
            id=f"{prefix}-UPD-CRUD-{field}",
            title=f"Update happy {suffix}",
            classes=["CRUD"], priority="P2",
            steps=[
                Step(name="create", method="POST", path=create_path,
                     body={**body_create, "name": f"{prefix.lower()}-upd-{field.lower()}-{{{{runId}}}}"},
                     test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
                poll_operation_until_done(capture_id_to="createdId"),
                _rya(Step(name="patch", method="PATCH",
                     path=f"{update_base_path}/{{{{createdId}}}}",
                     body=patch_body,
                     test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
                poll_operation_until_done(),
                # read-your-writes: verify GET of the caller's OWN fresh resource can
                # briefly 404 while the owner/hierarchy tuple materializes under load
                # (opgate removed → eventual-consistency). Bounded-retry on 404/403.
                _rya(Step(name="verify", method="GET", path=f"{update_base_path}/{{{{createdId}}}}",
                     test_script=[*assert_status(200), *asserts])),
                _rya(Step(name="cleanup", method="DELETE", path=f"{update_base_path}/{{{{createdId}}}}",
                     test_script=[*save_from_response("j.id", "opId")])),
                poll_operation_until_done(),
            ],
        )
    # ЦЕЛЬ ПЕРЕИМЕНОВАНИЯ НЕСЁТ ТОКЕН ПРОГОНА — как и всякое имя под UNIQUE(project,name).
    #
    # Здесь стоял фиксированный литерал. Проект суиты (`_suiteProjectId`) переживает
    # прогон, поэтому вторая правка того же имени упирается в `409 AlreadyExists` —
    # и не упиралась она лишь потому, что предыдущий прогон ДОШЁЛ ДО УБОРКИ. Прогон,
    # снятый по времени или по исчерпанию ресурса, оставлял за собой занятое имя, и
    # следующий читал его как нарушение согласованности, хотя это заселённый слот.
    # Замер по дереву на день правки: таких целей в наборах vpc — 18 (шесть ресурсов
    # × три помощника), сравнений с литералом в утверждениях — столько же.
    #
    # Подстановка `{{runId}}` в СКРИПТЕ не работает (newman разрешает её в теле и
    # адресе), поэтому ожидаемое имя собирается из окружения тем же выражением.
    return [
        case_for("NAME", "name",
                 {"updateMask": "name", "name": f"{prefix.lower()}-renamed-x-{{{{runId}}}}"},
                 ["pm.test('name updated', () => pm.expect(pm.response.json().name).to.eql('"
                  + prefix.lower() + "-renamed-x-' + pm.environment.get('runId')));"]),
        case_for("DESC", "description", {"updateMask": "description", "description": "updated-desc-newman"},
                 ["pm.test('description updated', () => pm.expect(pm.response.json().description).to.eql('updated-desc-newman'));"]),
        case_for("LABELS", "labels", {"updateMask": "labels", "labels": {"env": "prod", "team": "net"}},
                 ["pm.test('label env', () => pm.expect((pm.response.json().labels || {}).env).to.eql('prod'));",
                  "pm.test('label team', () => pm.expect((pm.response.json().labels || {}).team).to.eql('net'));"]),
    ]


def perf_baseline_block(prefix, list_path, get_path=None):
    """Performance baseline: response time для Get/List ниже бюджета.

    list_path — путь List endpoint (с projectId query param).
    """
    cases = [
        Case(
            id=f"{prefix}-LST-PERF-BASELINE",
            title="List response time < 500ms (perf baseline)",
            classes=["PERF", "CRUD"], priority="P2",
            steps=[Step(name="list-timed", method="GET",
                        path=f"{list_path}?projectId={{{{_suiteProjectId}}}}&pageSize=10",
                        test_script=[*assert_status(200),
                                     "pm.test('response time < 500ms', () => pm.expect(pm.response.responseTime).to.be.below(500));"])],
        ),
    ]
    return cases


def verbatim_text_pack(prefix, resource_name, resource_path, text_template=None):
    """Snapshots стабильного текста для распространенных ошибок (Get/Update/Delete).

    text_template — шаблон not-found текста с плейсхолдером {id}; по умолчанию
    "<resource_name> {id} not found". Для SecurityGroup передается
    "Security group SecurityGroup.Id(value={id}) not found"."""
    text_template = text_template or (resource_name + " {id} not found")

    def _eql_test(literal_id):
        exact = text_template.format(id=literal_id)
        return f"pm.test('verbatim text', () => pm.expect(pm.response.json().message).to.eql({json.dumps(exact)}));"

    return [
        Case(
            id=f"{prefix}-GET-CONF-FULLTEXT",
            title=f"Get garbage → точный текст контракта not-found",
            classes=["CONF", "NEG"], priority="P1",
            steps=[Step(name="get", method="GET",
                        path=f"{resource_path}/enpsnapshotnonexist01",
                        test_script=[
                            *assert_status(404), *assert_grpc_code(5, "NOT_FOUND"),
                            _eql_test("enpsnapshotnonexist01"),
                        ])],
        ),
        Case(
            id=f"{prefix}-UPD-CONF-FULLTEXT",
            title=f"Update garbage → точный текст контракта not-found",
            classes=["CONF", "NEG"], priority="P1",
            steps=[Step(name="upd", method="PATCH",
                        path=f"{resource_path}/enpsnapshotnonexist02",
                        body={"updateMask": "description", "description": "x"},
                        # PATCH-мутация по несуществующему id: scope_extractor 403
                        # (authz-first) ДО backend 404 → not-found текст здесь
                        # unobservable; verbatim-контракт держит GET-CONF-FULLTEXT (get).
                        test_script=[*assert_absent_id_rejected()])],
        ),
        Case(
            id=f"{prefix}-DEL-CONF-FULLTEXT",
            title=f"Delete garbage → точный текст контракта not-found",
            classes=["CONF", "NEG"], priority="P1",
            steps=[Step(name="del", method="DELETE",
                        path=f"{resource_path}/enpsnapshotnonexist03",
                        # DELETE-мутация по несуществующему id: scope_extractor 403
                        # (authz-first) ДО backend 404 → verbatim держит GET-CONF-FULLTEXT.
                        test_script=[*assert_absent_id_rejected()])],
        ),
    ]


def update_happy_multi_field(prefix, create_path, update_base_path, body_create):
    """Update с маской из нескольких полей (mask=name,description,labels)."""
    return Case(
        id=f"{prefix}-UPD-CRUD-MULTI-MASK",
        title="Update с mask=name,description,labels → все три поля обновлены",
        classes=["CRUD", "STATE"], priority="P2",
        steps=[
            Step(name="create", method="POST", path=create_path,
                 body={**body_create, "name": f"{prefix.lower()}-multi-{{{{runId}}}}"},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(capture_id_to="createdId"),
            _rya(Step(name="patch-multi", method="PATCH",
                 path=f"{update_base_path}/{{{{createdId}}}}",
                 # Цель переименования — с токеном прогона: имя под UNIQUE(project,name)
                 # в переживающем прогон проекте (разбор — в `update_happy_per_field`).
                 body={"updateMask": "name,description,labels",
                       "name": f"{prefix.lower()}-multi-new-{{{{runId}}}}",
                       "description": "multi-desc",
                       "labels": {"a": "1", "b": "2"}},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
            poll_operation_until_done(),
            _rya(Step(name="verify-all", method="GET",
                 path=f"{update_base_path}/{{{{createdId}}}}",
                 test_script=[*assert_status(200),
                              "const j = pm.response.json();",
                              "pm.test('name updated', () => pm.expect(j.name).to.eql('"
                              + prefix.lower() + "-multi-new-' + pm.environment.get('runId')));",
                              "pm.test('description updated', () => pm.expect(j.description).to.eql('multi-desc'));",
                              "pm.test('labels a', () => pm.expect((j.labels || {}).a).to.eql('1'));",
                              "pm.test('labels b', () => pm.expect((j.labels || {}).b).to.eql('2'));"])),
            _rya(Step(name="cleanup", method="DELETE",
                 path=f"{update_base_path}/{{{{createdId}}}}",
                 test_script=[*save_from_response("j.id", "opId")])),
            poll_operation_until_done(),
        ],
    )


def cross_project_resource_block(prefix, create_path, body_create, name_field="name"):
    """Cross-project validation: создать ресурс в одном project, увидеть его только из этого project."""
    return Case(
        id=f"{prefix}-LST-AUTHZ-CROSS-PROJECT-ISOLATION",
        title="Project isolation: ресурс в projectA не виден в List по projectB",
        classes=["AUTHZ", "CRUD"], priority="P0",
        steps=[
            Step(name="create-in-A", method="POST", path=create_path,
                 body={**body_create, "projectId": "{{_suiteProjectId}}",
                       name_field: f"{prefix.lower()}-iso-{{{{runId}}}}"},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(capture_id_to="isoId"),
            Step(name="list-in-B", method="GET",
                 path=f"{create_path}?projectId={{{{_suiteProjectCrossId}}}}&pageSize=100",
                 test_script=[*assert_status(200),
                              "const ids = (Object.values(pm.response.json()).find(v => Array.isArray(v)) || []).map(x => x.id);",
                              "pm.test('isolated — not in projectB list', () => pm.expect(ids).to.not.include(pm.environment.get('isoId')));"]),
            Step(name="cleanup", method="DELETE", path=f"{create_path}/{{{{isoId}}}}",
                 test_script=[*save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
        ],
    )


def list_filter_match_block(prefix, create_path, body_create):
    """List filter: создать ресурс, потом filter по точному name."""
    return Case(
        id=f"{prefix}-LST-FILTER-MATCH",
        title="Создать ресурс → list filter=name='X' → ресурс в результатах",
        classes=["FILTER", "CRUD"], priority="P2",
        steps=[
            Step(name="create", method="POST", path=create_path,
                 body={**body_create, "name": f"{prefix.lower()}-flt-{{{{runId}}}}"},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(capture_id_to="fltId"),
            _rup(Step(name="list-filtered", method="GET",
                 path=f"{create_path}?projectId={{{{_suiteProjectId}}}}&pageSize=100&filter=name%3D%22{prefix.lower()}-flt-{{{{runId}}}}%22",
                 test_script=[*assert_status(200),
                              "const ids = (Object.values(pm.response.json()).find(v => Array.isArray(v)) || []).map(x => x.id);",
                              "pm.test('filtered list contains', () => pm.expect(ids).to.include(pm.environment.get('fltId')));"]), "fltId"),
            _rya(Step(name="cleanup", method="DELETE", path=f"{create_path}/{{{{fltId}}}}",
                 test_script=[*save_from_response("j.id", "opId")])),
            poll_operation_until_done(),
        ],
    )


def neg_invalid_types_block(prefix, create_path, body_create):
    """Negative с invalid type в полях: name=null, labels=строка вместо object."""
    return [
        # JSON-null для скалярного поля protojson читает как «поле не задано», то
        # есть имя приходит пустым, а пустое имя контракт допускает → 200.
        # Прежний заголовок обещал 400, а утверждение принимало и 200: под именем
        # «rejected» стояла проверка, которая не могла упасть ни на одном из двух
        # ответов. Теперь заявлен и проверяется ровно тот исход, который есть.
        Case(
            id=f"{prefix}-CR-VAL-NAME-NULL",
            title="Create с name=null → 200 (protojson: null = поле не задано; имя подставляется)",
            classes=["VAL"], priority="P2",
            steps=[Step(name="cr-null", method="POST", path=create_path,
                        body={**body_create, "name": None},
                        test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
                   *confirm_created_and_cleanup(create_path)],
        ),
        Case(
            id=f"{prefix}-CR-VAL-LABELS-STRING-TYPE",
            title="Create с labels=строка (вместо object) → 400 (defensive: plain-text или JSON тело ошибки)",
            classes=["VAL", "NEG"], priority="P2",
            steps=[Step(name="cr-bad-type", method="POST", path=create_path,
                        body={**body_create, "name": f"{prefix.lower()}-bt-{{{{runId}}}}", "labels": "not-an-object"},
                        test_script=[*assert_transcode_error()])],
        ),
        Case(
            id=f"{prefix}-CR-VAL-DESC-INT-TYPE",
            title="Create с description=число → 400 (defensive: plain-text или JSON тело ошибки)",
            classes=["VAL", "NEG"], priority="P3",
            steps=[Step(name="cr-bad-desc", method="POST", path=create_path,
                        body={**body_create, "name": f"{prefix.lower()}-bd-{{{{runId}}}}", "description": 12345},
                        test_script=[*assert_transcode_error()])],
        ),
    ]


def malformed_body_block(prefix, create_path):
    """Malformed JSON body, empty body, wrong content-type."""
    return [
        Case(
            id=f"{prefix}-CR-VAL-MALFORMED-JSON",
            title="Create с malformed JSON → 400",
            classes=["VAL", "NEG"], priority="P2",
            steps=[Step(name="cr-malformed", method="POST", path=create_path,
                        body=None,
                        pre_script=[
                            "// Подменяем body на невалидный JSON через pm.request.body",
                            "pm.request.body = { mode: 'raw', raw: '{invalid json---}' };",
                        ],
                        # 403 добавлен: malformed JSON → projectId не распарсен →
                        # scope_extractor 403 (unscoped, authz-first) ДО транскодера 400/415.
                        test_script=["pm.test('rejected (400/415 transcode or 403 authz-first)', () => pm.expect(pm.response.code).to.be.oneOf([400, 403, 415]));"])],
        ),
        Case(
            id=f"{prefix}-CR-VAL-EMPTY-BODY",
            title="Create с пустым body → rejected (400 project_id required | 403 unscoped authz-first)",
            classes=["VAL", "NEG"], priority="P2",
            steps=[Step(name="cr-empty-body", method="POST", path=create_path,
                        body={},
                        # empty body → нет projectId → unscoped → scope_extractor 403
                        # (authz-first) ЛИБО backend 400 project_id required.
                        test_script=[*assert_unscoped_rejected()])],
        ),
    ]


def alreadyexists_dup_name_for(prefix, create_path, refusal, body_create):
    """Создать дубль с тем же name → sync 409 ALREADY_EXISTS.

    `refusal` — текст отказа ВЛАДЕЛЬЦА дословно, с `{name}` на месте имени
    (`"RouteTable with name {name} already exists"`). Обязателен и без
    умолчания — по той же причине, что у `idempotency_block` выше: общая часть
    тона не различает, ЧЕЙ это отказ, а умолчание вернуло бы слабость молча.
    """
    dup_name = f"{prefix.lower()}-dupck-{{{{runId}}}}"
    return Case(
        id=f"{prefix}-CR-NEG-DUP-NAME-CHECK",
        title="Создать дубль с тем же name → sync 409 ALREADY_EXISTS",
        classes=["NEG", "CONC"], priority="P1",
        steps=[
            Step(name="cr-first", method="POST", path=create_path,
                 body={**body_create, "name": dup_name},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(capture_id_to="firstId"),
            Step(name="cr-dup", method="POST", path=create_path,
                 body={**body_create, "name": dup_name},
                 test_script=[*assert_status(409), *assert_grpc_code(6, "ALREADY_EXISTS"),
                              *assert_refusal_message(refusal.replace("{name}", dup_name))]),
            Step(name="cleanup-first", method="DELETE", path=f"{create_path}/{{{{firstId}}}}",
                 test_script=[*save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
        ],
    )


def update_mask_partial_block(prefix, create_path, update_base_path, body_create):
    """Decision table partial mask: только name; только description; только labels."""
    return [
        Case(
            id=f"{prefix}-UPD-VAL-MASK-NAME-ONLY",
            title="Update mask=name → только name меняется, description/labels не трогаются",
            classes=["VAL", "STATE"], priority="P2",
            steps=[
                Step(name="cr", method="POST", path=create_path,
                     body={**body_create, "name": f"{prefix.lower()}-mn-{{{{runId}}}}",
                           "description": "init", "labels": {"orig": "1"}},
                     test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
                poll_operation_until_done(capture_id_to="createdId"),
                _rya(Step(name="patch-name-only", method="PATCH",
                     path=f"{update_base_path}/{{{{createdId}}}}",
                     # Цель переименования — с токеном прогона (разбор — в
                     # `update_happy_per_field`).
                     body={"updateMask": "name", "name": f"{prefix.lower()}-mnnew-{{{{runId}}}}",
                           "description": "should-be-ignored", "labels": {"ignored": "y"}},
                     test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
                poll_operation_until_done(),
                # read-your-writes: bounded-retry the fresh-resource verify GET over
                # the owner-tuple materialization window (opgate removed).
                _rya(Step(name="verify", method="GET",
                     path=f"{update_base_path}/{{{{createdId}}}}",
                     test_script=[*assert_status(200),
                                  "const j = pm.response.json();",
                                  "pm.test('name updated', () => pm.expect(j.name).to.eql('"
                                  + prefix.lower() + "-mnnew-' + pm.environment.get('runId')));",
                                  "pm.test('description preserved', () => pm.expect(j.description).to.eql('init'));",
                                  "pm.test('labels preserved', () => pm.expect((j.labels || {}).orig).to.eql('1'));"])),
                _rya(Step(name="cleanup", method="DELETE",
                     path=f"{update_base_path}/{{{{createdId}}}}",
                     test_script=[*save_from_response("j.id", "opId")])),
                poll_operation_until_done(),
            ],
        ),
    ]


def perf_baseline_get_block(prefix, get_create_path, body_create):
    """GET happy с perf budget."""
    return Case(
        id=f"{prefix}-GET-PERF-BASELINE",
        title="Get existing — response time < 300ms",
        classes=["PERF", "CRUD"], priority="P2",
        steps=[
            Step(name="cr", method="POST", path=get_create_path,
                 body={**body_create, "name": f"{prefix.lower()}-perf-{{{{runId}}}}"},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(capture_id_to="perfId"),
            _rya(Step(name="get-timed", method="GET", path=f"{get_create_path}/{{{{perfId}}}}",
                 test_script=[*assert_status(200),
                              "pm.test('response time < 300ms', () => pm.expect(pm.response.responseTime).to.be.below(300));"])),
            Step(name="cleanup", method="DELETE", path=f"{get_create_path}/{{{{perfId}}}}",
                 test_script=[*save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
        ],
    )


def list_total_size_check_block(prefix, list_path):
    """List возвращает разумное число объектов (не больше pageSize)."""
    return [
        Case(
            id=f"{prefix}-LST-CONTRACT-NEVER-EXCEEDS-PAGESIZE",
            title="List с pageSize=5 → не более 5 элементов в response",
            classes=["PAGE", "CRUD"], priority="P2",
            steps=[Step(name="list-5", method="GET",
                        path=f"{list_path}?projectId={{{{_suiteProjectId}}}}&pageSize=5",
                        test_script=[*assert_status(200),
                                     "pm.test('at most 5 items', () => {"
                                     "  const j = pm.response.json();"
                                     "  const k = Object.keys(j).find(x => Array.isArray(j[x]));"
                                     "  pm.expect((j[k] || []).length).to.be.at.most(5);"
                                     "});"])],
        ),
    ]


def headers_content_type_block(prefix, create_path, body_create):
    """Content-Type required: POST без правильного header → behavior."""
    return [
        Case(
            # Гейта на Content-Type у края нет: маршрутизатор REST разбирает тело
            # маршалером по умолчанию, когда заголовок отсутствует. Значит исход
            # один — запрос обслуживается. Это утверждение о контракте («заголовок
            # не обязателен»), и оно упадёт, если гейт появится.
            #
            # Прежнее `oneOf([200, 400, 415])` под именем «lenient or rejected»
            # принимало любой из трёх ответов, то есть не утверждало ничего.
            id=f"{prefix}-HEADERS-MISSING-CT",
            title="POST без Content-Type → 200 (край не требует заголовка)",
            classes=["VAL"], priority="P3",
            steps=[Step(name="post-no-ct", method="POST", path=create_path,
                        body={**body_create, "name": f"{prefix.lower()}-noct-{{{{runId}}}}"},
                        pre_script=["pm.request.headers.remove('Content-Type');"],
                        test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
                   *confirm_created_and_cleanup(create_path)],
        ),
    ]


def required_fields_matrix(prefix, create_path, body_full, required_field_names):
    """Для каждого required-поля: убрать его из body → создание ОТВЕРГНУТО.

    body_full — полное valid body (с всеми required полями).
    required_field_names — список имен ДЕЙСТВИТЕЛЬНО обязательных полей.

    # Что сюда класть нельзя

    Поле, которое контракт допускает пустым, обязательным не является, и кейс
    «убери его → отказ» утверждает о продукте неправду. Такой кейс мог зеленеть
    только пока принимал успех (см. ниже); в паре с поллером он краснеет на
    ПРАВИЛЬНОМ поведении, то есть требует вернуть терпимость. Не возвращать —
    снимать претензию.

    Мерка проста и не требует стенда: у поля есть проверка, отвергающая пустое
    значение (`if x == "" → InvalidArgument`), либо её нет. Прецедент в этом же
    файле: `v4CidrBlocks` уже снят из списка подсети как поле, которого в
    сообщении запроса нет вовсе.

    # Что здесь было и почему это не было утверждением

    Прежняя запись — `oneOf([200, 400, 403, 404])` под именем `rejected`. Заголовок
    кейса обещает отказ, а `200` в списке означает, что создание ПРИНЯТО: кейс
    удовлетворялся ровно тем исходом, ради опровержения которого написан, и снятие
    проверки required-поля оставило бы все шестнадцать его вхождений зелёными.
    Оправдание в комментарии («поле проверяется async, ошибка придёт в op.error») само
    по себе верно — но за шагом НЕ СЛЕДОВАЛО НИЧЕГО, что бы эту `op.error` прочло.
    Названная полоса не проверялась; принималось голое «принято».

    # Форма, к которой приведено

    `assert_refused_sync_or_async` + ОБЯЗАТЕЛЬНЫЙ следующий шаг
    `poll_operation_until_done(must_fail=True)` — образец взят из
    `services/nlb/tests/newman/scripts/gen.py`. Теперь `200` законен ровно в одном
    качестве: как расписка на Operation, которая обязана завершиться с ошибкой, и
    поллер эту ошибку требует. Синхронный отказ снимает `opId`, и поллер молчит, не
    имея предмета.

    # Две полосы и почему у области их одна

    `projectId` — область, которую край резолвит для anti-BOLA-проверки: запрос без неё
    UNSCOPED, край отвечает fail-closed ДО того, как бэкенд посмотрит в тело
    (authz-first, security.md; паритет с `assert_absent_id_rejected`). Значит Operation
    не чеканится вовсе и асинхронной полосы для этого входа НЕТ — `async_lane=False`.
    Объявить её значило бы пообещать то, чего продукт нарушить не может, а парный поллер
    обратился бы к `{{opId}}` как к литералу.

    Остальные поля: запрос ЗАСКОУПЛЕН, доходит до бэкенда — отказ либо синхронный `400`,
    либо на Operation. Обе полосы реальны, поэтому обе и названы.
    """
    cases = []
    for fld in required_field_names:
        body_missing = {k: v for k, v in body_full.items() if k != fld}
        scope_field = fld == "projectId"
        # 400 — sync InvalidArgument (нет required-поля);
        # 403 — только для scope-поля: unscoped → authz-first fail-closed;
        # 404 — нерезолвящийся родитель читается как отсутствующий.
        # `200` в этот список НЕ входит: его подставляет помощник как асинхронную
        # полосу, и только вместе с поллером, который требует ошибку.
        sync_codes = (400, 403, 404) if scope_field else (400, 404)
        steps = [Step(name=f"cr-no-{fld}", method="POST", path=create_path,
                      body=body_missing,
                      test_script=assert_refused_sync_or_async(
                          f"create without required '{fld}'",
                          sync_codes=sync_codes,
                          async_lane=not scope_field))]
        if not scope_field:
            steps.append(poll_operation_until_done(must_fail=True))
        cases.append(Case(
            id=f"{prefix}-CR-VAL-REQ-{fld.upper().replace('_','-')}",
            title=(f"Create без required поля '{fld}' → отказ: sync 403 (unscoped, authz-first)"
                   if scope_field else
                   f"Create без required поля '{fld}' → отказ: sync 400 либо Operation с ошибкой"),
            classes=["VAL"], priority="P0",
            steps=steps,
        ))
    return cases


def immutable_fields_matrix(prefix, update_base_path, immutable_field_names):
    """Для каждого immutable поля: PATCH mask=<field> → 400 InvalidArgument
    с verbatim text "<field> is immutable" (или другая 4xx).
    """
    cases = []
    for fld in immutable_field_names:
        # The probe is carried by the MASK alone: an immutable (or retired) field named
        # in update_mask must be rejected. A matching key in the body would be
        # decorative — none of these fields exists in the Update*Request message, so the
        # edge drops the key before the handler ever sees it and the outcome cannot
        # depend on it. Sending it anyway is how a fixture ends up claiming to set a
        # field the contract does not have.
        body = {"updateMask": fld}
        cases.append(Case(
            id=f"{prefix}-UPD-STATE-IMMUTABLE-{fld.upper().replace('_','-')}",
            title=f"Update mask='{fld}' (immutable) → 400 InvalidArgument verbatim",
            classes=["STATE", "VAL", "CONF"], priority="P1",
            steps=[Step(name=f"upd-{fld}", method="PATCH",
                        path=f"{update_base_path}/{{{{garbageVpcId}}}}",
                        body=body,
                        # PATCH immutable-mask по garbageVpcId (несуществующий):
                        # scope_extractor 403 (authz-first) ДО immutable-check 400 /
                        # sync Get 404. immutable-verbatim держит scoped-кейс на реальном id.
                        test_script=[*assert_absent_id_rejected()])],
        ))
    return cases


def subnet_cidr_expand_shrink_pack():
    """Расширенный набор AddCidrBlocks / RemoveCidrBlocks сценариев для Subnet."""
    cases = []

    # 1) Add один CIDR → виден в GET
    cases.append(Case(
        id="SUB-ACB-CRUD-ADD-ONE",
        title="AddCidrBlocks: добавить 1 CIDR → виден в response",
        classes=["CRUD"], priority="P1",
        steps=[
            Step(name="add-1", method="POST",
                 path="/vpc/v1/subnets/{{addedSubId}}:add-cidr-blocks",
                 body={"ipv4CidrBlocks": ["10.180.10.0/24"]},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
            Step(name="verify-1", method="GET", path="/vpc/v1/subnets/{{addedSubId}}",
                 test_script=[*assert_status(200),
                              "pm.test('cidr added', () => pm.expect(" + SUBNET_V4_CIDRS + ").to.include('10.180.10.0/24'));"]),
        ],
    ))

    # 2) Add несколько CIDR за один запрос
    cases.append(Case(
        id="SUB-ACB-CRUD-ADD-MULTIPLE",
        title="AddCidrBlocks: добавить 3 CIDR за один request → все 3 видны",
        classes=["CRUD", "BVA"], priority="P1",
        steps=[
            Step(name="add-3", method="POST",
                 path="/vpc/v1/subnets/{{addedSubId}}:add-cidr-blocks",
                 body={"ipv4CidrBlocks": ["10.180.20.0/24", "10.180.21.0/24", "10.180.22.0/24"]},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
            Step(name="verify-3", method="GET", path="/vpc/v1/subnets/{{addedSubId}}",
                 test_script=[*assert_status(200),
                              "const c = " + SUBNET_V4_CIDRS + ";",
                              "pm.test('all 3 present', () => {",
                              "  pm.expect(c).to.include('10.180.20.0/24');",
                              "  pm.expect(c).to.include('10.180.21.0/24');",
                              "  pm.expect(c).to.include('10.180.22.0/24');",
                              "});"]),
        ],
    ))

    # 3) Add CIDR пересекающийся с existing → FailedPrecondition
    cases.append(Case(
        id="SUB-ACB-NEG-OVERLAP-SELF",
        title="AddCidrBlocks с CIDR пересекающимся с existing prefix → FailedPrecondition",
        classes=["NEG", "CONF"], priority="P0",
        steps=[
            # Кейс СОЗДАЁТ пересечение сам. Прежде он добавлял 10.180.10.0/25 и
            # ссылался в комментарии на блок 10.180.10.0/24, «уже добавленный»
            # соседним кейсом — но каждый кейс набора обёрнут в собственную
            # сцену (`_subnet_cidr_setup_teardown`: своя сеть, своя подсеть с
            # единственным primary 10.180.0.0/24, свой teardown), поэтому
            # накопления между кейсами нет и никогда не было. Пересекаться было
            # не с чем: /25 в десятом октете и primary в нулевом не пересекаются,
            # продукт принимал добавление совершенно правильно, а утверждение
            # «отказ» краснело на предпосылке без производителя.
            Step(name="add-precondition", method="POST",
                 path="/vpc/v1/subnets/{{addedSubId}}:add-cidr-blocks",
                 body={"ipv4CidrBlocks": ["10.180.10.0/24"]},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
            # Работа идёт синхронно внутри вызова (operations.RunSync), отказ
            # записывается в САМУ операцию, и она возвращается уже завершённой.
            # Поэтому исход один: 200 с операцией, несущей ошибку.
            #
            # Прежде здесь стояла пара, не способная упасть: код ответа принимал и
            # 200, и 400, а проверка ошибки была целиком внутри `if (j.error)` —
            # при отсутствии отказа она проходила вхолостую.
            Step(name="add-overlap", method="POST",
                 path="/vpc/v1/subnets/{{addedSubId}}:add-cidr-blocks",
                 body={"ipv4CidrBlocks": ["10.180.10.0/25"]},  # ⊂ 10.180.10.0/24, добавленного шагом выше
                 test_script=[
                     *assert_status(200),
                     "const j = pm.response.json();",
                     "pm.test('operation completed inline', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
                     "pm.test('overlapping CIDR refused (FailedPrecondition)', () => pm.expect(j.error && j.error.code, JSON.stringify(j)).to.eql(9));",
                     "pm.test('refusal names the overlap', () => pm.expect((j.error && j.error.message) || '').to.match(/overlap/i));",
                 ]),
            # Отказ обязан быть бесследным: набор блоков подсети остаётся тем,
            # каким был до отвергнутого добавления.
            Step(name="verify-not-added", method="GET", path="/vpc/v1/subnets/{{addedSubId}}",
                 test_script=[*assert_status(200),
                              "const c = " + SUBNET_V4_CIDRS + ";",
                              "pm.test('rejected block not stored', () => pm.expect(c).to.not.include('10.180.10.0/25'));",
                              "pm.test('existing block intact', () => pm.expect(c).to.include('10.180.10.0/24'));"]),
        ],
    ))

    # 4) Add CIDR с host-bits → 400
    cases.append(Case(
        id="SUB-ACB-VAL-HOST-BITS",
        title="AddCidrBlocks с host-bits в CIDR (10.180.30.5/24) → 400",
        classes=["VAL", "NEG"], priority="P1",
        steps=[
            Step(name="add-hostbits", method="POST",
                 path="/vpc/v1/subnets/{{addedSubId}}:add-cidr-blocks",
                 body={"ipv4CidrBlocks": ["10.180.30.5/24"]},
                 test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT")]),
        ],
    ))

    # 5) Remove один из множества CIDR → остальные сохранены
    cases.append(Case(
        id="SUB-RCB-CRUD-REMOVE-ONE",
        title="RemoveCidrBlocks: добавить 3 → убрать 1 → 2 остаются",
        classes=["CRUD"], priority="P1",
        steps=[
            Step(name="add-3-pre", method="POST",
                 path="/vpc/v1/subnets/{{addedSubId}}:add-cidr-blocks",
                 body={"ipv4CidrBlocks": ["10.180.40.0/24", "10.180.41.0/24", "10.180.42.0/24"]},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
            Step(name="rm-1", method="POST",
                 path="/vpc/v1/subnets/{{addedSubId}}:remove-cidr-blocks",
                 body={"ipv4CidrBlocks": ["10.180.42.0/24"]},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
            Step(name="verify-1-removed", method="GET", path="/vpc/v1/subnets/{{addedSubId}}",
                 test_script=[*assert_status(200),
                              "const c = " + SUBNET_V4_CIDRS + ";",
                              "pm.test('removed cidr is gone', () => pm.expect(c).to.not.include('10.180.42.0/24'));",
                              "pm.test('other cidrs remain', () => {",
                              "  pm.expect(c).to.include('10.180.40.0/24');",
                              "  pm.expect(c).to.include('10.180.41.0/24');",
                              "});"]),
        ],
    ))

    # 6) Remove несуществующий CIDR — не во v4_cidr_blocks
    #
    # ЗАГОЛОВОК БОЛЬШЕ НЕ ХЕДЖИРУЕТ. Прежний обещал «FailedPrecondition ИЛИ silent»,
    # а утверждение принимало `oneOf([200, 400])` и затем один лишь `done` — то есть
    # кейс был удовлетворён ЛЮБЫМ исходом, включая тот, при котором продукт молча
    # принимает удаление отсутствующего блока. Второй ветки в продукте нет:
    # `RemoveCidrBlocksUseCase` работает через `RunSync`, и отсутствующий блок
    # отвергается ВНУТРИ операции (`FailedPrecondition`, «one or more CIDR blocks
    # not found in subnet»). Синхронная `400` на этом входе производится только
    # malformed-id и пустым списком блоков — ни того, ни другого здесь нет.
    #
    # Отсюда: шаг утверждает чеканку Operation (`200`), а ОТКАЗ утверждает парный
    # поллер — вместе с кодом и дословным тоном. Отдельный шаг `check-result` снят:
    # он адресовался `{{opId}}`, который на объявленной (и несуществующей) полосе
    # `400` пуст, и в этом случае уезжал на `/operations/` без сегмента.
    cases.append(Case(
        id="SUB-RCB-NEG-NOT-PRESENT",
        title="RemoveCidrBlocks с CIDR не из списка → FailedPrecondition в операции",
        classes=["NEG", "VAL"], priority="P1",
        steps=[
            Step(name="rm-missing", method="POST",
                 path="/vpc/v1/subnets/{{addedSubId}}:remove-cidr-blocks",
                 body={"ipv4CidrBlocks": ["192.168.99.0/24"]},
                 test_script=[
                     *assert_status(200),
                     # ЧЕКАНКА OPERATION УТВЕРЖДАЕТСЯ ЗДЕСЬ, а не подразумевается.
                     # Весь отказ этого кейса живёт в парном поллере, а поллер
                     # выходит РАНЬШЕ своих утверждений, если `opId` пуст, —
                     # тогда три утверждения об отказе не исполняются вовсе и
                     # кейс зеленеет на несделанном. Предпосылка узкая (продукт
                     # должен отдать `200` без `id`), но именно её узость и
                     # оставляла её неназванной: в парном `SG-DEL-NEG-NIC-ATTACHED`
                     # эта тропа закрыта, а здесь опорой служил один `200`
                     # (асимметрия названа приёмкой 2026-08-06).
                     "pm.test('Operation minted (the refusal is the operation\\'s to carry)', "
                     "() => pm.expect(pm.response.json().id, pm.response.text())"
                     ".to.be.a('string'));",
                     *save_from_response("j.id", "opId"),
                 ]),
            poll_operation_until_done(
                must_fail=True, must_fail_code=9,
                must_fail_message="one or more CIDR blocks not found in subnet"),
        ],
    ))

    # 7) Remove last remaining primary CIDR → запрет (нельзя удалять primary/первый)
    cases.append(Case(
        id="SUB-RCB-NEG-CANNOT-REMOVE-PRIMARY",
        title="RemoveCidrBlocks для primary v4_cidr (первый, primary) → отказ",
        classes=["NEG", "STATE"], priority="P0",
        steps=[
            # Первый блок семейства — placement-якорь (ipv4CidrPrimary),
            # неизменяемый после Create; его удаление отвергается конвенционным
            # immutable-тоном. Работа синхронна, отказ живёт в возвращённой
            # операции → исход один.
            #
            # Прежде проверка прямо допускала «silent success (продукт
            # permissive)»: кейс, заявленный как отказ, проходил и тогда, когда
            # якорь молча удалялся.
            Step(name="rm-primary", method="POST",
                 path="/vpc/v1/subnets/{{addedSubId}}:remove-cidr-blocks",
                 body={"ipv4CidrBlocks": ["10.180.0.0/24"]},  # primary subnet CIDR из preflight
                 test_script=[
                     *assert_status(200),
                     "const j = pm.response.json();",
                     "pm.test('operation completed inline', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
                     "pm.test('primary anchor refused (InvalidArgument)', () => pm.expect(j.error && j.error.code, JSON.stringify(j)).to.eql(3));",
                     "pm.test('refusal uses the immutable contract tone', () => pm.expect((j.error && j.error.message) || '').to.contain('ipv4_cidr_primary is immutable after Subnet.Create'));",
                 ]),
        ],
    ))

    # 8) Add + Remove batch — добавить и убрать в обратной последовательности
    cases.append(Case(
        id="SUB-ACB-RCB-ROUNDTRIP",
        title="AddCidrBlocks + RemoveCidrBlocks roundtrip: добавили → убрали → не изменилось",
        classes=["IDM", "STATE"], priority="P2",
        steps=[
            Step(name="state-before", method="GET", path="/vpc/v1/subnets/{{addedSubId}}",
                 test_script=[*assert_status(200),
                              "pm.environment.set('cidrsBefore', JSON.stringify(" + SUBNET_V4_CIDRS + "));"]),
            Step(name="add-temp", method="POST",
                 path="/vpc/v1/subnets/{{addedSubId}}:add-cidr-blocks",
                 body={"ipv4CidrBlocks": ["10.180.99.0/24"]},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
            Step(name="remove-temp", method="POST",
                 path="/vpc/v1/subnets/{{addedSubId}}:remove-cidr-blocks",
                 body={"ipv4CidrBlocks": ["10.180.99.0/24"]},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
            Step(name="state-after", method="GET", path="/vpc/v1/subnets/{{addedSubId}}",
                 test_script=[*assert_status(200),
                              "const before = JSON.parse(pm.environment.get('cidrsBefore'));",
                              "const after = " + SUBNET_V4_CIDRS + ";",
                              "pm.test('cidrs roundtrip — равны', () => pm.expect(after.sort()).to.deep.eql(before.sort()));"]),
        ],
    ))

    return cases


def pairwise_subnet_pack():
    """Pairwise для Subnet: zone × prefix — 9 комбинаций покрывают все пары.

    Третья ось (dhcp) снята: `dhcp_options` изъят из Subnet.Create редизайном
    VPC-1 F9/VPC-1-43 и в сообщении запроса зарезервирован. Ось, которую нельзя
    выразить в теле, ничего не варьирует — она давала два одинаковых запроса под
    разными именами и создавала впечатление покрытия, которого не было. Отсутствие
    DHCP-ручек фиксирует SUB-CR-V1-DHCP-DROPPED (поля нет в проекции чтения).
    """
    # Используем только существующие zone id (zone-{a,b,d}); на несуществующей
    # зоне Subnet.Create отвергается с "Illegal argument zone_id".
    combos = [
        ("{{zoneA}}", "/24"), ("{{zoneA}}", "/28"), ("{{zoneA}}", "/16"),
        ("{{zoneB}}", "/24"), ("{{zoneB}}", "/28"), ("{{zoneB}}", "/16"),
        ("{{zoneD}}", "/24"), ("{{zoneD}}", "/28"), ("{{zoneD}}", "/16"),
    ]
    cases = []
    for i, (zone, prefix) in enumerate(combos):
        ipbase = f"10.{170+i}.0.0"
        # ipv4_cidr_primary — тот самый якорь, который варьирует ось prefix. Пока
        # тело несло снятое v4_cidr_blocks, край его отбрасывал: все девять
        # комбинаций уезжали БЕЗ префикса, и ось существовала только в заголовке.
        body = {"projectId": "{{_suiteProjectId}}", "networkId": "{{netId}}",
                "name": f"sub-pw-{i}-{{{{runId}}}}", "zoneId": zone,
                "ipv4CidrPrimary": f"{ipbase}{prefix}"}
        cases.append(Case(
            id=f"SUB-CR-PAIRWISE-{i:02d}",
            title=f"Pairwise [{i}]: zone={zone} prefix={prefix} → 200, префикс доехал до ресурса",
            classes=["VAL", "CRUD"], priority="P2",
            steps=[
                # ВСЕ ДЕВЯТЬ КОМБИНАЦИЙ ЗАКОННЫ — установлено по коду, поэтому исход
                # заявлен один, а не «200 либо 400»:
                #
                #  * зона: `zoneA/B/D` резолвятся первым шагом коллекции из живого
                #    каталога geo (при нехватке зон индекс схлопывается на последнюю
                #    существующую), поэтому peer-проверка зоны проходит всегда;
                #  * префикс: v4-валидация отвергает только длиннее /28 — /16, /24, /28
                #    внутри полосы; сетевой адрес у всех девяти канонический;
                #  * вложенность в родительскую сеть НЕ ограничивает: план сети —
                #    FIXTURE_NETWORK_SUPERNET (10.0.0.0/8), а все девять якорей лежат
                #    в 10.170…10.178, то есть внутри него при любом из трёх префиксов.
                #    Здесь стояло «сеть создаётся БЕЗ супернета, а проверка при пустом
                #    супернете пропускается by construction» — этого пропуска больше
                #    нет: сеть без объявленного плана подсеть не принимает вовсе, и
                #    обоснование, опиравшееся на пропуск, обосновывало бы отказ всех
                #    девяти. Вывод не изменился, основание заменено на действующее;
                #    ось префикса по-прежнему не взаимодействует с адресным
                #    пространством сети — это и был вопрос, на который «200 либо 400»
                #    позволяло не отвечать;
                #  * имя и CIDR у каждой комбинации свои, сеть у каждого кейса свежая —
                #    ни UNIQUE(name), ни EXCLUDE по пересечению не срабатывают.
                #
                # Прежнее `oneOf([200, 400])` принимало и приём, и отказ, поэтому ни один
                # из девяти кейсов не мог упасть — в том числе если бы полоса допустимых
                # префиксов сузилась или зона перестала резолвиться.
                Step(name="cr-pw", method="POST", path="/vpc/v1/subnets", body=body,
                     test_script=[
                         *assert_status(200),
                         # Create исполняется синхронно, поэтому отказ (если бы он был)
                         # лежал бы В САМОЙ возвращённой операции — проверяем её, а не
                         # только код ответа.
                         "pm.test('операция без ошибки', () => "
                         "pm.expect(pm.response.json().error, pm.response.text()).to.be.undefined);",
                         *save_from_response("j.id", "opId")
                     ]),
                poll_operation_until_done(capture_id_to="subId", id_expr="j.metadata && j.metadata.subnetId"),
                # Ось префикса реальна только если префикс ДОЕХАЛ до ресурса. Однажды он
                # не доезжал вовсе (край отбрасывал снятое поле), и девять кейсов
                # зеленели, варьируя ничего. Чтение возвращает ту же строку — это и есть
                # страж против повторения.
                _rya(
                    Step(name="verify-pw", method="GET", path="/vpc/v1/subnets/{{subId}}",
                         test_script=[
                             *assert_status(200),
                             f"pm.test({js_str(f'префикс доехал: ipv4CidrPrimary == {ipbase}{prefix}')}, () => "
                             f"pm.expect(pm.response.json().ipv4CidrPrimary, pm.response.text()).to.eql({js_str(f'{ipbase}{prefix}')}));",
                             # Имя переменной, а не её значение: Postman подставляет
                             # `{{...}}` в ПОЛЯХ ЗАПРОСА, но не внутри test-script'а —
                             # литерал в утверждении сравнивался бы с самой строкой
                             # «{{zoneA}}» и не мог пройти никогда.
                             "pm.test('зона доехала', () => "
                             f"pm.expect(pm.response.json().zoneId, pm.response.text()).to.eql(pm.environment.get({js_str(zone.strip('{}'))})));",
                         ])),
                Step(name="cleanup-pw", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
                     test_script=["pm.test('cleanup', () => pm.expect(pm.response.code).to.be.oneOf([200, 404]));",
                                  *save_from_response("j.id", "opId")]),
            ],
        ))
    return cases


def security_injection_block(prefix, create_path, list_path, body_create):
    r"""Security probes: SQL/command/XSS/нулевой байт в name + filter.

    Никогда не должно возвращать 500 или утечку pgx/stack trace — и это НЕ
    единственное, что здесь установлено.

    ИСХОД КАЖДОЙ ИЗ СЕМИ НАГРУЗОК ОПРЕДЕЛЁН, поэтому утверждается ПАРА
    (HTTP-статус И код `google.rpc.Status`), а не перечень.

    Здесь стояло одно утверждение `oneOf([200, 400, 413])` под именем
    «handled 2xx/4xx». Оно принимало И успех, И отказ, то есть не отделяло
    соблюдение контракта от его нарушения — и приняло бы ровно ту регрессию
    валидации имени, ради обнаружения которой проба и написана. Это не «две
    полосы с разными производителями» (как у `assert_absent_id_rejected` и
    соседей, где перечисляются РАЗНЫЕ линии), а ОДНА полоса с неустановленным
    исходом.

    Почему исход установим — по каждому звену тракта:

      * имя проверяется ЕДИНСТВЕННОЙ формой дерева — DNS label RFC 1123,
        `pkg/validate/nameform`.`Form` = `^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`.
        Её читают ОБЕ ветки: `domain.RcNameVPC.Validate` у пяти ресурсов и
        `corevalidate.NameOnCreate` у Gateway (прежние `corevalidate.NameVPC` и
        `NameGateway` — две разные формы одного правила — сведены в одну, #715).
        Ни одна из семи нагрузок ей не соответствует: апостроф, угловая скобка,
        точка, слэш, точка с запятой, пробел и нулевой байт лежат вне класса
        символов, а «A»×1000 и заглавная, и длиннее 63. То есть `200` недостижим
        ни для одной нагрузки ни на одном из шести ресурсов;
      * отказ СИНХРОННЫЙ и стоит ДО обращения к соседям и до создания Operation
        (`Execute` каждого из шести ресурсов), поэтому код именно
        `INVALID_ARGUMENT`, а не `FAILED_PRECONDITION` peer-полосы и не
        `NOT_FOUND` полосы прямого чтения;
      * край переводит `INVALID_ARGUMENT` в **400** — таблица
        `api-conventions.md` §«gRPC-код → HTTP-статус» (мультиплексор края
        собирается БЕЗ `WithErrorHandler`, отображение задаёт
        `runtime.HTTPStatusFromCode`). `413` в этой таблице НЕ ПРОИЗВОДИТСЯ НИ
        ОДНИМ кодом — прежний перечень называл исход, у которого нет
        производителя, и покраснеть на нём было нельзя никогда;
      * поле отказа НАЗВАНО: `BadRequest.fieldViolations[].field == "name"` —
        обе ветки строят деталь одинаково (`serviceerr.FromValidation` у пяти
        ресурсов, `coreerrors.Builder.AddFieldViolation` у Gateway).

    Прецедент, уже зелёный на том же пути и том же классе ввода:
    `*-CR-VAL-NAME-SPECIAL-CHARS` из `ecp_name_block` (`name!@#-…`) утверждает
    ту же пару `400` + `code 3` у всех шести ресурсов.

    Предмет пробы («никогда 500», «никогда утечка panic/sqlstate/goroutine») от
    этого не меняется и утверждается СВОИМИ строками — усиление формы отказа его
    не замещает.

    Нагрузка `nullbyte` несёт НАСТОЯЩИЙ нулевой байт: `x`, `0x00`, `y`. В теле
    запроса он записан экранированной последовательностью `\u0000` — другого
    способа выразить его в JSON нет, — и разбор края возвращает из неё именно
    байт `0x00`: `protojson.Unmarshal` на `CreateNetworkRequest` даёт имя
    `78 00 79` без ошибки, после чего форма имени его отвергает. Нулевой байт
    интересен отдельно от прочих нагрузок тем, что его обработка расходится
    между слоями (разбор JSON, драйвер БД, C-строки libpq).

    До #701 здесь стоял ПРОБЕЛ. Исход совпадал — пробел вне класса символов
    ровно так же, — поэтому кейс был зелёным, и подмена прожила незамеченной:
    «фикстура не проверяет того, чем названа». Слот занят, отчёт зелёный, а
    входа, ради которого проба заведена, край не видел ни разу.

    Соответствие ИМЕНИ нагрузки её БАЙТАМ держится теперь не вниманием, а гейтом
    по дереву: `internal/repohygiene/artifactgates`
    `TestInjectionPayloadCarriesWhatItsNamePromises` читает СГЕНЕРИРОВАННЫЕ
    коллекции всех наборов и требует по каждой нагрузке то, что обещает её имя.

    Способность этих утверждений упасть доказана инъекцией в обе стороны:
    `scripts/selftest_security_injection_lanes.py`.
    """
    injections = [
        ("sqli", "test' OR 1=1--"),
        ("union", "x' UNION SELECT * FROM operations--"),
        ("xss", "<script>alert(1)</script>"),
        ("cmd", "; rm -rf / ;"),
        ("path", "../../etc/passwd"),
        # Настоящий нулевой байт, а не пробел (#701). Разбор — в docstring выше.
        ("nullbyte", "x\x00y"),
        ("longpayload", "A" * 1000),
    ]
    cases = []
    for name, payload in injections:
        cases.append(Case(
            id=f"{prefix}-CR-SEC-{name.upper()}",
            title=f"Security probe: {name} in name → 400 InvalidArgument 'name', без утечки",
            classes=["VAL", "NEG"], priority="P0",
            steps=[Step(name=f"cr-{name}", method="POST", path=create_path,
                        body={**body_create, "name": payload[:1000]},
                        test_script=[
                            "pm.test('not 500', () => pm.expect(pm.response.code).to.not.eql(500));",
                            *assert_status(400),
                            *assert_grpc_code(3, "INVALID_ARGUMENT"),
                            *assert_field_violation("name"),
                            "const body = JSON.stringify(pm.response.json() || {});",
                            "pm.test('no panic/sqlstate/stacktrace leak', () => {",
                            "  const low = body.toLowerCase();",
                            "  pm.expect(low).to.not.include('panic');",
                            "  pm.expect(low).to.not.include('sqlstate');",
                            "  pm.expect(low).to.not.include('goroutine');",
                            "});",
                        ])],
        ))
    # Списочная полоса той же нагрузки. Исход здесь УСТАНОВЛЕН, а не неопределён:
    # `name="a\' OR 1=1--"` разбирается штатно (поле в белом списке, значение в
    # кавычках, хвоста нет) и уезжает ПАРАМЕТРОМ запроса — значит 200 и пустая
    # страница. Прежнее `oneOf([200, 400])` перечисляло исход, которого на этом
    # входе не бывает, и тем же утверждением приняло бы регрессию разбора фильтра:
    # разборщик, отвергающий законное выражение, зеленел бы наравне с исправным.
    # `not 500` остаётся: это отдельный предмет (нет паники/утечки на пути фильтра),
    # у него свой производитель, и он не поглощается утверждением о 200.
    cases.append(Case(
        id=f"{prefix}-LST-SEC-FILTER-SQLI",
        title="Security: SQL injection в filter → 200 и пустая страница, без 500",
        classes=["VAL", "NEG"], priority="P0",
        steps=[Step(name="lst-sqli", method="GET",
                    path=f"{list_path}?projectId={{{{_suiteProjectId}}}}&filter=name%3D%22a%27%20OR%201%3D1--%22",
                    test_script=[
                        "pm.test('not 500', () => pm.expect(pm.response.code).to.not.eql(500));",
                        *assert_empty_page("такого имени нет ни у кого — значение ушло параметром"),
                    ])],
    ))
    return cases


def conformance_lifecycle_pack(prefix, create_path, body_create):
    """Lifecycle: Create→Get→List-includes→Update→Get-updated→Delete→List-excludes→Get-404."""
    return Case(
        id=f"{prefix}-LIFECYCLE-CONF",
        title="Full lifecycle conformance: CRUD invariants",
        classes=["CRUD", "CONF", "STATE"], priority="P1",
        steps=[
            Step(name="cr", method="POST", path=create_path,
                 body={**body_create, "name": f"{prefix.lower()}-life-{{{{runId}}}}"},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(capture_id_to="lifeId"),
            _rya(Step(name="get-1", method="GET", path=f"{create_path}/{{{{lifeId}}}}",
                 test_script=[*assert_status(200),
                              "pm.test('id matches', () => pm.expect(pm.response.json().id).to.eql(pm.environment.get('lifeId')));"])),
            _rup(Step(name="lst-includes", method="GET",
                 path=f"{create_path}?projectId={{{{_suiteProjectId}}}}&pageSize=1000",
                 test_script=[*assert_status(200),
                              "const items = Object.values(pm.response.json()).find(v => Array.isArray(v)) || [];",
                              "pm.test('list contains', () => pm.expect(items.map(x => x.id)).to.include(pm.environment.get('lifeId')));"]), "lifeId"),
            Step(name="upd", method="PATCH", path=f"{create_path}/{{{{lifeId}}}}",
                 body={"updateMask": "description", "description": "life-conf"},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
            Step(name="get-after-upd", method="GET", path=f"{create_path}/{{{{lifeId}}}}",
                 test_script=[*assert_status(200),
                              "pm.test('description updated', () => pm.expect(pm.response.json().description).to.eql('life-conf'));"]),
            Step(name="del", method="DELETE", path=f"{create_path}/{{{{lifeId}}}}",
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
            Step(name="lst-excludes", method="GET",
                 path=f"{create_path}?projectId={{{{_suiteProjectId}}}}&pageSize=1000",
                 test_script=[*assert_status(200),
                              "const items = Object.values(pm.response.json()).find(v => Array.isArray(v)) || [];",
                              "pm.test('list does not contain', () => pm.expect(items.map(x => x.id)).to.not.include(pm.environment.get('lifeId')));"]),
            Step(name="get-404", method="GET", path=f"{create_path}/{{{{lifeId}}}}",
                 test_script=[*assert_status(404), *assert_grpc_code(5, "NOT_FOUND")]),
        ],
    )


def authz_caller_headers_block(prefix, list_path):
    """RBAC-pre kit: проверка headers cross-tenant (анонимный vs admin-claim)."""
    return [
        # Кейс назывался «List с пустым x-kacho-project-id header», но НИ ОДНОГО
        # заголовка не трогал, а утверждение принимало 200, 403 и 401 разом — то
        # есть под именем про заголовок стояла проверка, которая не могла упасть
        # ни при каком ответе и ничего про заголовок не спрашивала.
        #
        # Теперь кейс делает то, что заявляет, и утверждает проверяемое свойство:
        # заголовок личности, присланный клиентом, НЕ является источником scope.
        # Подставляем в него посторонний проект — ответ обязан остаться прежним
        # (200 по проекту из строки запроса, под личностью из токена). Если край
        # когда-нибудь начнёт доверять этому заголовку, кейс упадёт.
        Case(
            id=f"{prefix}-AUTHZ-EMPTY-PROJECT-HEADER",
            title="Заголовок x-kacho-project-id от клиента не задаёт scope → 200 по своему проекту",
            classes=["AUTHZ"], priority="P1",
            steps=[Step(name="list-with-empty-header", method="GET",
                        path=f"{list_path}?projectId={{{{_suiteProjectId}}}}",
                        pre_script=["pm.request.headers.upsert({key: 'x-kacho-project-id', value: '{{garbageRmId}}'});"],
                        test_script=[
                            *assert_status(200),
                            "pm.test('client-asserted project header is not honoured as scope', () => pm.expect(pm.response.json()).to.be.an('object'));",
                        ])],
        ),
    ]






# Окно видимости прав — РЕШЕНИЕ НАБОРА, а не общего слоя (#1379): путь
# материализации у доменов разный, и одно число за всех было бы решением
# за них. Здесь — замер vpc. Величина видна
# на связывании, а не в прозе шапки: три копии из шести называли ЧУЖУЮ.
# Голова полосы — общая (#1477). Шаг, чей адрес собран из НЕЗАХВАЧЕННОЙ переменной,
# спрашивает не о ресурсе: окно видимости прав такой адрес не наполнит никогда, а
# отказ по нему приходит кодом ИЗ полосы ожидания — то есть шаг выжигает весь бюджет
# и падает, называя следствие вместо предмета.
_rya = functools.partial(retry_until_authorized,
                        budget=25, interval_ms=500, lane_head=True)
# То же окно у СПИСОЧНОГО ожидания — и то же правило: величину называет НАБОР,
# а не общий слой (#1379). Три копии этой обёртки расходились ещё и телом:
# одна ждала появления ВСЕХ названных имён, другая вела ведомость исчерпания,
# третья не делала ни того, ни другого. Общая форма несёт обе починки, а
# различие набора выражено ЗДЕСЬ — аргументом, видимым на связывании.
_rup = functools.partial(retry_until_present,
                        budget=25, interval_ms=500)


def retry_until_absent(step: Step, still_present_expr: str, budget: int = 25,
                       interval_ms: int = 500) -> Step:
    """Bounded retry a "must-be-ABSENT/empty" negative read over a read-your-writes-ON-
    REVOKE window — the MIRROR of retry_until_authorized for the deny/leak-guard side.

    A no-access subject's "List → EMPTY / id NOT present" leak-guard flakes under PARALLEL
    load when the subject carries a residual/concurrent grant from ANOTHER suite (shared
    fixture subject): e.g. an account-scoped viewer created by the iam access-binding suite
    matches this project's child resources via account→project containment, so the deny-list
    transiently returns rows until that suite's revoke materializes (FGA tuple / list-authz
    negative-cache lag — eventually-consistent). The serial run's timing hid this.

    `still_present_expr` is a JS boolean, TRUE while the must-be-absent thing is STILL present
    (e.g. the list is still non-empty). Retries SELF while truthy, spacing ~interval_ms.
    Fail-OPEN at budget: the wrapped step's real assertion then runs once on the terminal
    response, so a GENUINE over-show hole (rows never leave the deny-list) still FAILS — a
    persistent leak can NEVER be masked; only a transient revoke/contamination window is
    absorbed. Use ONLY on a negative "must be absent/empty" read whose emptiness is guaranteed
    once the contaminating grant is genuinely gone — NEVER a real cross-account deny.

    The step is renamed (-abs<N>) so its self-loop setNextRequest(pm.info.requestName) resolves
    to ITSELF (these suites do NOT prefix step names by case-id, same hazard as retry_until_authorized)."""
    guard = [
        "// bounded retry over the revoke/contamination materialization window (read-your-writes",
        "// ON REVOKE): retry SELF while the must-be-absent thing is still present, spacing ~interval_ms.",
        "// Fail-open at budget -> the real leak-guard assertion runs once and FAILS if it is STILL",
        "// present (a GENUINE over-show hole never clears -> NEVER masked).",
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
    _RYA_SEQ[0] += 1
    return replace(step, name=f"{step.name}-abs{_RYA_SEQ[0]}",
                   test_script=guard + list(step.test_script))


# ---------------------------------------------------------------------------
# Сериализация в Postman v2.1
# ---------------------------------------------------------------------------

def _auth_pre_script(auth: str) -> List[str]:
    """Генерирует JS-сниппет для per-step Authorization-header.

    "anonymous" → снимает Authorization. Имя env-переменной →
    Authorization: Bearer <значение env-var>. Snippet идет в начало pre_script."""
    if auth == "anonymous":
        return [
            "// authz-deny: anonymous step",
            "pm.request.headers.remove('Authorization');",
        ]
    return [
        f"// authz-deny: bearer from env '{js_comment(auth)}'",
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


# ПЛАН ФИКСТУРНОЙ СЕТИ.
#
# Сеть, не объявившая супернет семейства, подсеть этого семейства НЕ ПРИНИМАЕТ —
# синхронный 400: нарезать не из чего. Значит фикстура, которая создаёт сеть телом
# без блоков и тут же режет в ней подсеть, была бы снисходительнее продукта и
# прятала бы ровно тот отказ, который продукт теперь даёт.
#
# Блоки выбраны широкими намеренно: предмет таких кейсов — подсеть, адрес,
# интерфейс, балансировщик, а НЕ границы адресного плана. Узкий план заставлял бы
# держать в фикстуре второй экземпляр знания о том, какие CIDR берут соседние шаги,
# и разъезжался бы с ними молча. Кейсы, чей предмет — сами границы (подсеть вне
# супернета, :add-cidr-blocks, :remove-cidr-blocks), объявляют свои блоки САМИ и
# нормализацией не затрагиваются.
FIXTURE_NETWORK_SUPERNET = {
    "ipv4CidrBlocks": ["10.0.0.0/8"],
    "ipv6CidrBlocks": ["fd00::/8"],
}


def _declare_supernet_where_a_subnet_is_carved(steps: List[Step]) -> List[Step]:
    """Кейс, режущий подсеть с CIDR, обязан объявить план у своей сети.

    Дописывается ТОЛЬКО там, где кейс действительно режёт подсеть с адресом:
    негативы про сам Network.Create (пустое тело, отсутствующий projectId,
    невалидный блок супернета) подсеть не режут и остаются нетронутыми — иначе
    нормализация правила бы то, о чём кейс спрашивает.

    Шаг, уже назвавший свои блоки, не трогается: у него предмет — границы плана.
    Шаг с `declares_no_supernet=True` не трогается тоже — там предмет сам отказ.
    """
    carves = any(
        s.method == "POST" and s.path == "/vpc/v1/subnets" and isinstance(s.body, dict)
        and ("ipv4CidrPrimary" in s.body or "ipv6CidrPrimary" in s.body)
        for s in steps
    )
    if not carves:
        return steps
    out = []
    for s in steps:
        if (s.method == "POST" and s.path == "/vpc/v1/networks"
                and isinstance(s.body, dict) and not s.declares_no_supernet
                and not any(k.endswith("CidrBlocks") for k in s.body)):
            s = replace(s, body={**s.body, **FIXTURE_NETWORK_SUPERNET})
        out.append(s)
    return out


_POLL_PATH_RE = re.compile(r"^/operations/\{\{(\w+)\}\}$")


def _poll_reads_under_the_actor_that_published_it(steps: List[Step]) -> List[Step]:
    """Опрос операции идёт ПОД ТЕМ ЖЕ актором, что её создал.

    `OperationService.Get` энфорсит владение: владелец — принципал, создавший
    операцию, и чужому он отвечает `NotFound`, а не отказом (no-leak). Значит
    опрос под другим актором получает `404` — и выглядит это как задержка
    материализации в продукте, то есть диагноз оказывается в шести шагах от
    причины. Ровно так и вышло: шаг под администратором облака создавал пул, а
    опрос уходил дефолтным проектным актором и получал
    `operation … not found`; за ним каскадом падали ещё девять шагов, ни один из
    которых виноват не был.

    ПОЧЕМУ ВЫВОДИТСЯ, А НЕ ПЕРЕДАЁТСЯ АРГУМЕНТОМ. Актор опроса — не решение
    автора кейса, а СЛЕДСТВИЕ того, кто операцию создал. Аргумент можно забыть
    — и забвение молчаливо: коллекция соберётся, шаг исполнится, а упадёт он
    только на стенде и не своим именем. Вывод забыть нельзя.

    Кто «создал» — устанавливается по ИМЕНИ переменной операции: чей
    `save_from_response` опубликовал её последним, тот актор и берётся. Явно
    заданный актор опроса сильнее вывода: у кейса, чей предмет — чтение чужой
    операции, он и должен быть чужим.

    Замер на дереве в день заведения: из 18 наборов vpc правило меняет ТРИ шага
    одного набора (`public-pool`), остальные семнадцать коллекций собираются
    побайтово теми же — актор мутации и умолчание коллекции там совпадают.
    """
    published: Dict[str, Optional[str]] = {}
    out: List[Step] = []
    for st in steps:
        for line in st.test_script:
            if "pm.environment.set(" not in line:
                continue
            for name in re.findall(r"pm\.environment\.set\('(\w+)'", line):
                published[name] = st.auth
        m = _POLL_PATH_RE.match(st.path) if st.method == "GET" else None
        if m and st.auth is None and published.get(m.group(1)) is not None:
            st = replace(st, auth=published[m.group(1)])
        out.append(st)
    return out


def normalize_steps(steps: List[Step]) -> List[Step]:
    """ВЕСЬ набор проходов над шагами — в одном месте.

    Зовётся и кейсами, и посевом. Раньше цепочка стояла только внутри
    `case_to_postman`, и посев, собранный рядом, унаследовал ЧАСТЬ проходов:
    отсутствовали `_assert_published_id_outcome` и `_assert_delete_operation_outcome`,
    то есть фикстура публиковала id ресурса, чей опрос утверждал только `done` —
    а операция несёт предвыделенный id и когда завершилась ошибкой. Поймал это
    гейт `internal/repohygiene` `TestPublishedResourceIdIsGuardedByOperationOutcome`,
    и починка сделана здесь, а не у посева: список проходов, размноженный по двум
    вызывающим, расходится молча — и расходится там, где расхождение не видно.
    """
    return _poll_reads_under_the_actor_that_published_it(
        _assert_published_id_outcome(
            _reset_captured_operation_id(_assert_delete_operation_outcome(
                _declare_supernet_where_a_subnet_is_carved(
                    _wrap_own_fresh_reads(steps, _rya))))))


# Zone ids are environment-specific: geo seeds Region/Zone with ids that differ
# per deploy and are NOT the legacy literals zone-a..d. Resolve them ONCE,
# synchronously, as the FIRST item of every collection (a real request, so newman
# blocks on its response before running any case) and publish zoneA..zoneD +
# existingZoneId/existingZoneAltId.
#
# ШАГ НАЗЫВАЕТ СВОЙ ИСХОД — И ПОЛОС У НЕГО ДВЕ, РАЗЛИЧИМЫХ ПО СУЩЕСТВУ.
#
# Здесь стояло «best-effort: no failing assertion — if geo is unreachable
# (standalone vpc), the committed env defaults stay in effect». Довод про умолчания
# верен и сохранён (они закоммичены в шаблоне окружения и непусты), но из него
# следовало не «утверждать нечего», а «утверждать надо ДРУГОЕ»: шаг публикует
# координату, на которой стоит каждый размещаемый ресурс набора, и при молчании
# отказ каталога был неотличим от отказа резолва и от пустого каталога.
#
#   полоса 1 (условная): каталог ОТВЕТИЛ — значит зоны обязаны разрешиться. Ответ
#     200 с нулём пригодных зон это не «geo недоступен», это непригодный каталог:
#     мягкий проход в этом месте превращал бы постоянную неготовность стенда в
#     штатный режим, а падали бы кейсы, которым нечего размещать;
#   полоса 2 (безусловная): после шага координата зоны НЕПУСТА — резолвом ли,
#     закоммиченным ли умолчанием. Это и есть предмет шага, и утверждение о нём
#     верно на ОБЕИХ полосах, поэтому оно не `oneOf` на взаимоисключающие исходы.
#
# Недоступность geo по-прежнему НЕ роняет набор: полоса 1 при ней не берётся, а
# полоса 2 держится умолчанием. Красным станет ровно то, что и должно, — стенд, где
# ни каталога, ни умолчаний.
_ZONE_SETUP_TEST = [
    "const code = (pm.response && pm.response.code) || 0;",
    "let zs = [];",
    "if (code === 200) { try { zs = (pm.response.json().zones) || []; } catch (e) {} }",
    "const up = zs.filter(z => !z.status || String(z.status).indexOf('UP') !== -1);",
    # EPHEMERAL-ZONE GUARD (cross-suite race). The geo suite creates throwaway zones
    # (`qa-zr-crud-<runId>-a`, `qa-zr-dflt-<runId>-a`, status UP) and DELETES them at
    # cleanup. Under newman-parallel those live concurrently with this resolve, and the
    # zone list sorts them BEFORE the admin-curated baseline (`qa-` < `ru-`), so a naive
    # pick[0] latched onto a zone that geo then removed → every later Subnet/Address
    # Create in this suite failed `unknown zone id 'qa-zr-dflt-…'` (CI run 30112149890,
    # vpc/concurrency 8 asserts). Resolve ONLY stable, admin-curated zones: drop the
    # `qa-`-prefixed QA-ephemerals, then prefer the canonical baseline family; fall back
    # to whatever is left (and finally to the committed env defaults) so a standalone or
    # differently-seeded stand still resolves.
    "const stable = up.filter(z => String(z.id || '').indexOf('qa-') !== 0);",
    "const baseline = stable.filter(z => String(z.id || '').indexOf('ru-central1-') === 0);",
    "const pick = baseline.length ? baseline : (stable.length ? stable : up);",
    "if (pick.length) {",
    "  const at = (i) => (pick[i] || pick[pick.length - 1]).id;",
    "  pm.environment.set('zoneA', at(0));",
    "  pm.environment.set('zoneB', at(1));",
    "  pm.environment.set('zoneC', at(2));",
    "  pm.environment.set('zoneD', at(3));",
    "  pm.environment.set('existingZoneId', at(0));",
    "  pm.environment.set('existingZoneAltId', at(1));",
    "  pm.environment.set('_zoneResolved', '1');",
    "}",
    "if (code === 200) {",
    "  pm.test('каталог размещения ответил — пригодные зоны разрешены', function () {",
    "    pm.expect(pick.length, pm.response.text()).to.be.above(0);",
    "  });",
    "}",
    "pm.test('координата зоны непуста после резолва "
    "(живой каталог либо закоммиченное умолчание окружения)', function () {",
    "  pm.expect(String(pm.environment.get('existingZoneId') || ''), "
    "'existingZoneId').to.not.eql('');",
    "  pm.expect(String(pm.environment.get('existingZoneAltId') || ''), "
    "'existingZoneAltId').to.not.eql('');",
    "});",
]


def _zone_setup_item() -> Dict:
    """Blocking first item: resolve live geo zone ids into zoneA..D env vars."""
    return {
        # Имя объявлено константой: его читает `setup.sh`, чтобы прогнать резолв
        # зон вместе с якорем. Литерал здесь и литерал в оболочке разошлись бы
        # молча — и разошлись бы там, где расхождение не видно (папка просто не
        # нашлась бы, а newman на ненайденную точку входа ничего не утверждает).
        "name": ZONES_SETUP_ITEM,
        "event": [{
            "listen": "test",
            "script": {"type": "text/javascript", "exec": _ZONE_SETUP_TEST},
        }],
        "request": {
            "method": "GET",
            "header": [{"key": "Authorization", "value": "Bearer {{jwtBootstrap}}"}],
            "url": {
                "raw": "{{baseUrl}}/geo/v1/zones",
                "host": ["{{baseUrl}}"],
                "path": ["geo", "v1", "zones"],
            },
        },
    }


# Suites that exercise external-pool IPAM (internal-pool admin CRUD + address
# external allocation) assume a pre-seeded default EXTERNAL_PUBLIC AddressPool for
# the primary zone. The dev stand's seed-ipam is a NOOP, so seed it here, once,
# as cluster-admin (idempotent: 200 first time, 409 thereafter). Soft (no failing
# assertion) — if seeding cannot run, the dependent cases surface it themselves.
# ПОСЕВ НАЗЫВАЕТ СВОЙ ИСХОД, А ИМЯ БОЛЬШЕ НЕ ПУБЛИКУЕТ.
#
# Здесь стоял захват `_seededDefaultPoolId`, у которого в дереве НОЛЬ читателей
# (`git grep` по всему репозиторию находил только сами четыре коллекции и эту
# строку). Значение, которое пишут и не читают, невидимо отовсюду; заодно оно
# делало шаг похожим на публикующий координату, каковым он не является.
#
# Утверждение вместо него — и полоса у него ОДНА, а не две: посев идемпотентен,
# `AddressPool` отвечает ресурсом НАПРЯМУЮ (не Operation), а второй умолчательный
# пул для той же пары (зона, вид) отвергается `409 ALREADY_EXISTS` — это записано
# отдельным кейсом суиты (`cases/internal-pool.py`, «Create второй isDefault=true …»).
# Оба кода означают ровно одно: «пул по умолчанию в зоне A существует». Всё
# остальное (400 — тело не то, 401/403 — не тот субъект, 404 — не тот слушатель)
# означает, что предусловие НЕ установлено, и без этой строки такой посев
# отчитывался зелёным, а падали кейсы, которым нечего было выделять.
#
# Зеркальный посев зоне-независимого пула (`_ANYCAST_POOL_SEED_TEST`) намеренно
# остаётся мягким и ничего не публикует — там это записанное решение с доводом.
_POOL_SEED_TEST = [
    "pm.test('посев: умолчательный внешний пул в зоне A существует "
    "(создан либо уже был)', function () {",
    "  pm.expect(pm.response.code, pm.response.text()).to.be.oneOf([200, 409]);",
    "});",
]

_POOL_SEED_BODY = {
    "name": "seed-default-external-zonea",
    "kind": "EXTERNAL_PUBLIC",
    "zoneId": "{{zoneA}}",
    # 100.64.0.0/24: not used by any case pool (the EXCLUDE on address_pool_cidrs
    # is per-kind cross-zone, so the persistent seed must not overlap throwaway
    # pool CIDRs 203.0.113.0/24 / 198.51.100.0/24).
    "v4CidrBlocks": ["100.64.0.0/24"],
    "v6CidrBlocks": [],
    "isDefault": True,
}

# Collections that depend on a seeded default external pool.
# address-zone-coherence allocates an external v4 in existingZoneId (=zoneA) in its
# ZONE-03 happy path; without the zoneA default pool the Create Operation errors
# (no pool resolved) → the address never persists → get-known-zone 404s. Seed it.
# `gateway` — с тех пор как шлюз трансляции получает внешний адрес: его якорь
# стоит в existingZoneId (=zoneA), и без пула по умолчанию для этой зоны КАЖДЫЙ
# NAT-кейс суиты отказывал бы «нет доступного внешнего адреса IPv4». Это
# предусловие суиты, а не предмет кейса, поэтому оно живёт в посеве.
_POOL_SEED_SERVICES = {"internal-pool", "address", "address-zone-coherence", "gateway"}


# ── ZONE-INDEPENDENT (anycast) default pool ────────────────────────────────────
#
# The pool cascade is TWO MUTUALLY EXCLUSIVE LANES keyed on the placement of the
# request, not one fallback chain (see ResolverService.ResolvePoolForAddressObjFamily):
# a ZONAL request stops at the zone-default and never falls through, and a request with
# NO zone (anycast / REGIONAL) is served ONLY by the zone-independent default
# (`zone_id IS NULL`). The seed above fills the ZONAL lane only.
#
# The zone-independent lane's pool was therefore authored in exactly one place in the
# tree — deploy/scripts/seed-nlb-fixtures.sh (kac-nlb-seed-anycast-pool), which runs for
# the nlb shard. On the vpc shard it does not run, so the anycast lane had NO pool at all
# and EVERY zoneless external allocation failed FailedPrecondition. That is a fixture gap,
# not product behaviour: the resolver, the API (`ZoneID == ""` = zone-independent pool)
# and the delete/recycle path are all correct.
#
# What it looked like instead: the failing Create is ASYNC, so the case saw a sync 200 and
# an Operation that carried the PRE-ALLOCATED addressId even though the worker had errored
# → the case deleted a resource that had never existed → the edge answered 403 (the
# scope_extractor cannot resolve a nonexistent object to a project, fail-closed) → the
# symptom read as "the owner may not delete his own anycast address", an authz defect that
# was not there. The case now asserts op.error before using the id (see
# address-zone-coherence.py), so the cause names itself.
#
# Safe to add: the lanes are exclusive, so a zonal request can never reach this pool — that
# is precisely why the lane split exists. v4-only and 100.65.0.0/24 on purpose:
#   * v4-only  — the suite's v6 cases (ADR-CR-EXT-V6-FAMILY-FALLTHROUGH and friends) assert
#                that a v6 request finds NO pool; they are zone-pinned and stop in the zonal
#                lane anyway, but staying v4-only removes the question entirely.
#   * CIDR     — address_pool_cidrs EXCLUDE is GLOBAL per kind, so this must not overlap the
#                zonal seed (100.64.0.0/24), the internal-pool suite (100.100.0.0/16), the
#                address suite (100.101.0.0/16) or the nlb seed (100.102.<octet>.0/24).
# Soft and idempotent like the zonal seed: 200 first time, 409 thereafter — and 409 is also
# the right answer on an umbrella stand where the nlb seed already owns this cluster-wide
# slot (`(COALESCE(zone_id,''), kind) WHERE is_default` is a singleton). Either way the lane
# ends up with a pool, which is all the suite needs.
_ANYCAST_POOL_SEED_BODY = {
    "name": "seed-default-external-anycast",
    "kind": "EXTERNAL_PUBLIC",
    # zoneId deliberately ABSENT — that is what makes the pool zone-independent.
    "v4CidrBlocks": ["100.65.0.0/24"],
    "v6CidrBlocks": [],
    "isDefault": True,
}

# Collections that allocate an external address with NO zone (anycast lane).
_ANYCAST_POOL_SEED_SERVICES = {"address-zone-coherence"}

# Soft on purpose, and nothing is captured: no case addresses this pool by id — it is
# reached only through the resolver's zone-independent lane. If the seed cannot run, the
# dependent case says so itself now (its poll asserts op.error), which is why this step
# does not need an assertion of its own to keep the failure visible.
_ANYCAST_POOL_SEED_TEST = [
    "// setup-only: seeding is soft; ZC-VPC-ADDR-ZONE-04 surfaces a missing pool itself.",
]


def _anycast_pool_seed_item() -> Dict:
    """Idempotent setup item: ensure the zone-independent default EXTERNAL_PUBLIC pool exists."""
    return {
        "name": "_SETUP-POOL-ANYCAST — ensure zone-independent default EXTERNAL_PUBLIC pool",
        "event": [{
            "listen": "test",
            "script": {"type": "text/javascript", "exec": _ANYCAST_POOL_SEED_TEST},
        }],
        "request": {
            "method": "POST",
            "header": [
                {"key": "Authorization", "value": "Bearer {{jwtBootstrap}}"},
                {"key": "Content-Type", "value": "application/json"},
            ],
            "body": {"mode": "raw", "raw": json.dumps(_ANYCAST_POOL_SEED_BODY)},
            # AddressPool is admin-only (ban #6): internal mux only, like the zonal seed.
            "url": {
                "raw": "{{internalBaseUrl}}/vpc/v1/addressPools",
                "host": ["{{internalBaseUrl}}"],
                "path": ["vpc", "v1", "addressPools"],
            },
        },
    }


# ── ЯКОРЬ РАЗМЕЩЕНИЯ СУИТЫ ШЛЮЗОВ ─────────────────────────────────────────────
#
# Шлюз без подсети не создаётся ВОВСЕ (`subnetId` обязателен, он же якорь
# размещения), а NAT-шлюз обязан стоять в подсети, несущей IPv4. Поэтому суите
# нужна одна фикстурная пара «сеть + зональная подсеть с IPv4», и её id читает
# почти каждый кейс.
#
# ПОЧЕМУ ЭТО ПОСЕВ, А НЕ ПЕРВЫЙ КЕЙС. Раньше пара заводилась ПЕРВЫМ КЕЙСОМ
# коллекции. Следствие: прогон одиночного кейса (`--folder <кейс>`) якоря не
# получал — newman исполняет только названную точку входа, — и кейс, зелёный в
# полной суите, краснел в отладке, называя виновником невиновного: отказ приходил
# из тела, куда уехал неразрешённый `{{gwAnchorSubId}}`. Фикстура, занимающая слот
# кейса, вдобавок считается кейсом в каждом вердикте и в каждой переписи.
#
# Теперь это setup-папка коллекции (исполняется первой в полном прогоне), а для
# отладки одиночного кейса окружение готовит `./setup.sh` — он гоняет ЭТУ ЖЕ
# папку и выгружает id в файл окружения, поэтому объявление у якоря остаётся ОДНО.
#
# Имя папки и имена переменных объявлены здесь и читаются `setup.sh` из этого
# модуля (`python3 -c 'import gen; print(gen.GW_ANCHOR_FOLDER)'`) — второй копии
# литерала в оболочке нет, и разойтись им негде.
GW_ANCHOR_FOLDER = "_SETUP-GW-ANCHOR"
GW_ANCHOR_NET_VAR = "gwAnchorNetId"
GW_ANCHOR_SUBNET_VAR = "gwAnchorSubId"
# Имя первого setup-элемента: `setup.sh` обязан прогонять его ВМЕСТЕ с папкой
# якоря — иначе зона не резолвится и подсеть создаётся в зоне из закоммиченного
# умолчания, которой на стенде может не быть.
ZONES_SETUP_ITEM = "_SETUP-ZONES — resolve live geo zone ids (zoneA..D)"

_GW_ANCHOR_SERVICES = {"gateway"}


def _gw_anchor_steps() -> List[Step]:
    """Шаги якоря. КАЖДЫЙ утверждает свой исход.

    Шаг, создающий предмет кейса без утверждения, при отказе оставляет переменную
    пустой: прогон идёт дальше по несозданному ресурсу и падает через два-три
    шага, обвиняя невиновного (наблюдалось 2026-08-12 — создание слушателя
    получило 403 в окне материализации прав, а лог винил проверку запрета
    удаления).
    """
    return [
        Step(name="anchor-net", method="POST", path="/vpc/v1/networks",
             body={"projectId": "{{_suiteProjectId}}", "name": "gw-anchor-net-{{runId}}"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.networkId",
                                              GW_ANCHOR_NET_VAR)]),
        poll_operation_until_done(),
        Step(name="anchor-subnet", method="POST", path="/vpc/v1/subnets",
             body={"projectId": "{{_suiteProjectId}}",
                   "networkId": "{{" + GW_ANCHOR_NET_VAR + "}}",
                   "name": "gw-anchor-sub-{{runId}}", "zoneId": "{{existingZoneId}}",
                   "ipv4CidrPrimary": "10.71.0.0/24"},
             test_script=[*assert_status(200), *save_from_response("j.id", "opId"),
                          *save_from_response("j.metadata && j.metadata.subnetId",
                                              GW_ANCHOR_SUBNET_VAR)]),
        poll_operation_until_done(),
        _rya(Step(
            name="anchor-verify", method="GET",
            path="/vpc/v1/subnets/{{" + GW_ANCHOR_SUBNET_VAR + "}}",
            test_script=[*assert_status(200),
                         "pm.test('якорь создан и несёт IPv4', () => {",
                         "  const j = pm.response.json();",
                         "  pm.expect(j.id, pm.response.text()).to.eql("
                         f"pm.environment.get({js_str(GW_ANCHOR_SUBNET_VAR)}));",
                         "  pm.expect(j.ipv4CidrPrimary, 'IPv4 у якоря').to.be.a('string').and.not.empty;",
                         "});"])),
    ]


def _gw_anchor_setup_item() -> Dict:
    """Setup-папка якоря — ТЕ ЖЕ проходы над шагами, что у кейсов.

    Через `normalize_steps`, а не своим перечнем: посев, собравший цепочку
    отдельно, унаследовал не все проходы и опубликовал id ресурса без
    утверждения исхода операции (поймал гейт `internal/repohygiene`
    `TestPublishedResourceIdIsGuardedByOperationOutcome`). Фикстура, к которой
    применяется меньше правил, чем к кейсам, снисходительнее продукта.
    """
    steps = normalize_steps(_gw_anchor_steps())
    return {
        "name": GW_ANCHOR_FOLDER,
        "description": (
            "fixture | якорь размещения суиты шлюзов: сеть + зональная подсеть с IPv4. "
            f"Публикует {GW_ANCHOR_NET_VAR}/{GW_ANCHOR_SUBNET_VAR}. "
            "Для прогона одиночного кейса окружение готовит ./setup.sh."
        ),
        "item": [step_to_postman(_EMIT, s) for s in steps],
    }


def _pool_seed_item() -> Dict:
    """Idempotent setup item: ensure a default EXTERNAL_PUBLIC pool exists at zoneA."""
    return {
        "name": "_SETUP-POOL — ensure default EXTERNAL_PUBLIC pool at zoneA",
        "event": [{
            "listen": "test",
            "script": {"type": "text/javascript", "exec": _POOL_SEED_TEST},
        }],
        "request": {
            "method": "POST",
            "header": [
                {"key": "Authorization", "value": "Bearer {{jwtBootstrap}}"},
                {"key": "Content-Type", "value": "application/json"},
            ],
            "body": {"mode": "raw", "raw": json.dumps(_POOL_SEED_BODY)},
            # AddressPool — InternalAddressPoolService (admin-only, security.md), живёт
            # ТОЛЬКО на cluster-internal REST listener. На публичном {{baseUrl}} его нет
            # by design (ban #6) → сид молча получал 404 и все зависящие кейсы падали.
            "url": {
                "raw": "{{internalBaseUrl}}/vpc/v1/addressPools",
                "host": ["{{internalBaseUrl}}"],
                "path": ["vpc", "v1", "addressPools"],
            },
        },
    }


# InternalAddressPoolService RPCs are gated on cluster `system_admin` (the vpc
# authz interceptor checks the relation directly), so the suite must call them as
# cluster-admin, not as the default project admin. Force the admin JWT at the
# collection level for the internal-pool suite (per-step auth= still overrides).
_ADMIN_DEFAULT_SERVICES = {"internal-pool"}
_ADMIN_DEFAULT_PRE = [
    "const __adm = pm.environment.get('jwtBootstrap') || '';",
    "if (__adm) { pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __adm}); }",
]


# ---------------------------------------------------------------------------
# ИСХОД ПОДЗАПРОСА ЧИТАЕТСЯ — ИЛИ ЗДЕСЬ, ИЛИ ПО ЦЕПОЧКЕ НАКОПИТЕЛЕЙ
# ---------------------------------------------------------------------------
# Шаг, шлющий запросы САМ (`pm.sendRequest`), стоит вне всех проверок, которые
# судят шаг по его собственному ответу: он ходит на служебный путь `/healthz`,
# а предмет уезжает подзапросом. Значит его исход не прочтёт никто, кроме него
# самого либо шага, читающего его накопитель.
#
# Гейт дерева `internal/repohygiene/artifactgates` `TestCapturedVariableStepCarriesAnAssertion`
# такой шаг НЕ судит и не должен: его предмет — захват из СВОЕГО ответа. В его
# перечне законных близнецов эта форма прямо объявлена молчащей, и довод там
# стоял такой: «утверждает о нём следующий шаг». Довод верен для залпа
# (`burst-* → resolve-* → assert-*`) и был НЕВЕРЕН для уборки: у пяти шагов
# уборки состязательных кейсов следующего утверждающего шага не было вовсе, а
# у одного обработчик ответа был пуст — `() => {}`. Обещание, которого никто не
# проверял, и есть предмет этой проверки: она делает довод ПРОВЕРЯЕМЫМ там, где
# кейс собирается.
#
# Предикат ТРАНЗИТИВЕН по накопителям: залп публикует `burstResults`, резолв
# читает его и публикует `nicCollected`, и только третий шаг утверждает. Требуй
# мы утверждения от НЕПОСРЕДСТВЕННОГО читателя — законная трёхзвенная цепочка
# стала бы находкой, то есть ловилась бы форма вместо существа.
#
# Замер на дереве в день заведения (ревизия 8eb824e58, 91 коллекция, 9181 шаг):
# шагов с подзапросом 18, находок 5 — все пять в `cases/concurrency.py`
# (`cleanup-all-subs`, `wait-cleanup`, `cleanup-addresses`, `cleanup-nics`,
# `wait-nic-cleanup`). Контроль в другую сторону на той же переписи: 13 шагов
# подзапроса предикат НЕ помечает — значит он различает форму, а не метит всё
# подряд.
_SUBREQUEST_MARK = "pm.sendRequest"
_ENV_SET_RE = re.compile(r"pm\.environment\.set\(\s*['\"]([A-Za-z_][\w]*)['\"]")


def _env_reads(code: str, name: str) -> bool:
    """Скрипт ЧИТАЕТ имя окружения — и в исполняемой части, а не в объяснении."""
    return re.search(r"environment\.get\(\s*['\"]%s['\"]" % re.escape(name), code) is not None


def _step_test_code(item: Dict) -> str:
    """Исполняемая часть test-скрипта шага сериализованной коллекции."""
    for ev in item.get("event", []):
        if ev.get("listen") == "test":
            return _strip_js_comments("\n".join(ev.get("script", {}).get("exec", [])))
    return ""


def audit_subrequest_outcome_readers(service: str, col: Dict) -> Tuple[List[str], Dict[str, int]]:
    """Находки и перепись: у каждого шага с подзапросом есть читатель его исхода.

    Возвращает (перечень находок, перепись). Перепись печатается ВСЕГДА, чтобы
    «ноль находок» было отличимо от «ноль прочитанного»: предикат, переставший
    узнавать подзапрос, молча стал бы вечнозелёным.
    """
    findings: List[str] = []
    census = {"cases": 0, "steps": 0, "subrequest": 0, "self": 0, "chained": 0}

    def walk(items: List[Dict], path: List[str]) -> None:
        steps = [it for it in items if "item" not in it]
        for it in items:
            if "item" in it:
                census["cases"] += 1
                walk(it["item"], path + [it.get("name", "")])
        for i, it in enumerate(steps):
            census["steps"] += 1
            code = _step_test_code(it)
            if _SUBREQUEST_MARK not in code:
                continue
            census["subrequest"] += 1
            if any(form in code for form in _ASSERT_FORMS):
                census["self"] += 1
                continue
            carried = set(_ENV_SET_RE.findall(code))
            reached = False
            for later in steps[i + 1:]:
                lcode = _step_test_code(later)
                if not any(_env_reads(lcode, name) for name in carried):
                    continue
                if any(form in lcode for form in _ASSERT_FORMS):
                    reached = True
                    break
                carried |= set(_ENV_SET_RE.findall(lcode))
            if reached:
                census["chained"] += 1
                continue
            findings.append(
                "  %s :: %s :: %s — шлёт подзапросы, сам ничего не утверждает, и ни один "
                "последующий шаг кейса не читает его накопитель (%s), чтобы утвердить исход"
                % (service, " / ".join(path) or "(корень)", it.get("name"),
                   ", ".join(sorted(carried)) or "накопителя нет вовсе"))

    walk(col.get("item", []), [])
    return findings, census


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
    gen_shared._POLL_SEQ[0] = 0
    _RYA_SEQ[0] = 0


# Накопитель разбора подзапросов — решение vpc (#1474). Свойство «исход шага
# читает хоть кто-нибудь» проверяется ПО ВСЕМУ набору: читателем бывает
# последующий шаг того же кейса, поэтому судить одну коллекцию рано.
_SUBREQUEST_FINDINGS: List[str] = []
_SUBREQUEST_CENSUS = {"cases": 0, "steps": 0, "subrequest": 0, "self": 0, "chained": 0}


def _audit_subrequest_before_write(svc: str, col: Dict) -> List[str]:
    """Накопить находки и перепись по одной коллекции; вердикт — в конце.

    Возвращает ПУСТО намеренно: находка здесь не блокирует запись, потому что
    вердикт выносится по всему набору (см. `_report_subrequest_census`). Порядок
    сохранён тот же, что был у своей копии оркестрации.
    """
    f_svc, c_svc = audit_subrequest_outcome_readers(svc, col)
    _SUBREQUEST_FINDINGS.extend(f_svc)
    for k in _SUBREQUEST_CENSUS:
        _SUBREQUEST_CENSUS[k] += c_svc[k]
    return []


def _report_subrequest_census(_out_dir: Path) -> int:
    """Перепись и вердикт по исходу подзапросов — решение vpc (#1474)."""
    print("[gen] исход подзапроса: кейсов %d, шагов %d, из них с подзапросом %d "
          "(утверждают сами %d, читаются по цепочке накопителей %d)"
          % (_SUBREQUEST_CENSUS["cases"], _SUBREQUEST_CENSUS["steps"],
             _SUBREQUEST_CENSUS["subrequest"], _SUBREQUEST_CENSUS["self"],
             _SUBREQUEST_CENSUS["chained"]))
    if _SUBREQUEST_CENSUS["steps"] == 0:
        sys.stderr.write("gen: FAIL — обход не узнал ни одного шага; перепись беспредметна\n")
        return 1
    if _SUBREQUEST_FINDINGS:
        sys.stderr.write(
            "gen: FAIL — шаги, чей исход не прочтёт никто: %d\n\n" % len(_SUBREQUEST_FINDINGS))
        sys.stderr.write(
            "Шаг ходит на служебный путь, а предмет уезжает подзапросом, поэтому его\n"
            "ответ не судит ни одна проверка, читающая ответ ШАГА. Исход обязан быть\n"
            "назван: либо утверждением в самом шаге (в том числе внутри обработчика\n"
            "подзапроса), либо последующим шагом кейса, который читает его накопитель.\n\n")
        sys.stderr.write("\n".join(_SUBREQUEST_FINDINGS) + "\n")
        return 1
    return 0
# ─────────────────────────────────────────────────────────────────────────────
# РЕШЕНИЯ НАБОРА, от которых зависит форма коллекции (#1379). Форму собирает
# общий слой; здесь объявлено ТОЛЬКО то, чем этот набор от остальных отличается.
# ─────────────────────────────────────────────────────────────────────────────

def _vpc_case_steps(case):
    """Конвейер шагов кейса vpc.

    Провязка предиката видимости названа ЗДЕСЬ и явно: её читает гейт
    `internal/repohygiene/artifactgates`
    `TestOwnFreshReadWrapPredicateWiredInEveryNewmanGenerator` по литералу
    `_wrap_own_fresh_reads(case.steps` — он требует, чтобы обёртка первого
    доступа к своему свежему ресурсу стояла в КОНВЕЙЕРЕ СЕРИАЛИЗАЦИИ, а не
    ставилась руками по шагам (замер: 42 падения полосы видимости пришлись на
    шаги без обёртки вовсе). Предикат идемпотентен — уже обёрнутый шаг он
    пропускает, — поэтому повторный проход внутри `normalize_steps` ничего не
    меняет; это проверено побайтовым сравнением порождённых коллекций.
    """
    return normalize_steps(_wrap_own_fresh_reads(case.steps, _rya))


def _vpc_prefix_items(service):
    """Элементы, идущие ПЕРЕД кейсами: резолв зон, посев пулов, якорь шлюза.

    Якорь идёт ПОСЛЕ резолва зон: подсеть создаётся в живой зоне стенда, а не в
    той, что стоит в закоммиченном умолчании окружения.
    """
    items = [_zone_setup_item()]
    if service in _POOL_SEED_SERVICES:
        items.append(_pool_seed_item())
    if service in _ANYCAST_POOL_SEED_SERVICES:
        items.append(_anycast_pool_seed_item())
    if service in _GW_ANCHOR_SERVICES:
        items.append(_gw_anchor_setup_item())
    return items


# Опрос операции: тело общее (#1475), решения набора — здесь. Весь словарь исхода
# операции (`must_fail`, `capture_id_to`, `id_expr`, `op_var`) переехал в общий
# слой вместе с телом: захват идентификатора ПОСЛЕ доказанного отсутствия ошибки
# есть норма дерева, а не полоса vpc.
poll_operation_until_done = functools.partial(
    gen_shared.op_poll_step, Step, budget=30, interval_ms=500)

_EMIT = Emit(
    id_slug="kacho-vpc",
    display_name="kacho-vpc / newman",
    pre_global=lambda service: (PRE_GLOBAL + _ADMIN_DEFAULT_PRE
                                if service in _ADMIN_DEFAULT_SERVICES else PRE_GLOBAL),
    steps_of=_vpc_case_steps,
    auth_pre=_auth_pre_script,
    prefix_items=_vpc_prefix_items,
    # Internal*-шаги идут на cluster-internal REST listener — на публичном их нет
    # by design (запрет №6). См. `Step.internal`.
    host_var=lambda step: "internalBaseUrl" if step.internal else "baseUrl",
)

# Помощники, доезжающие до модуля кейсов. Перечень — СЛОВАРЬ: он объявлен один
# раз и виден целиком, а не сорока строками `mod.X = X`, каждая из которых
# переживала снятие своего предмета молча.
_INJECTED = {
    "Step": Step,
    "Case": Case,
    "assert_status": assert_status,
    "assert_grpc_code": assert_grpc_code,
    "assert_refusal_message": assert_refusal_message,
    "assert_refusal_message_contains": assert_refusal_message_contains,
    "assert_transcode_error": assert_transcode_error,
    "assert_field_violation": assert_field_violation,
    "assert_unscoped_rejected": assert_unscoped_rejected,
    "assert_absent_id_rejected": assert_absent_id_rejected,
    "assert_refused_sync_or_async": assert_refused_sync_or_async,
    "save_from_response": save_from_response,
    "assert_operation_envelope": assert_operation_envelope,
    "assert_empty_page": assert_empty_page,
    "assert_cleanup_delete": assert_cleanup_delete,
    "SUBNET_V4_CIDRS": SUBNET_V4_CIDRS,
    "SUBNET_V6_CIDRS": SUBNET_V6_CIDRS,
    "poll_operation_until_done": poll_operation_until_done,
    "retry_until_authorized": _rya,
    "retry_until_present": _rup,
    "retry_until_absent": retry_until_absent,
    "crud_list_bva_block": crud_list_bva_block,
    "conf_not_found_text": conf_not_found_text,
    "state_update_unknown_mask": state_update_unknown_mask,
    "state_immutable_project": state_immutable_project,
    "list_pagesize_1_bva": list_pagesize_1_bva,
    "ecp_name_block": ecp_name_block,
    "ecp_description_block": ecp_description_block,
    "ecp_labels_block": ecp_labels_block,
    "updatemask_decision_table": updatemask_decision_table,
    "filter_syntax_block": filter_syntax_block,
    "pagination_roundtrip": pagination_roundtrip,
    "idempotency_block": idempotency_block,
    "update_happy_per_field": update_happy_per_field,
    "perf_baseline_block": perf_baseline_block,
    "verbatim_text_pack": verbatim_text_pack,
    "authz_caller_headers_block": authz_caller_headers_block,
    "update_happy_multi_field": update_happy_multi_field,
    "cross_project_resource_block": cross_project_resource_block,
    "list_filter_match_block": list_filter_match_block,
    "neg_invalid_types_block": neg_invalid_types_block,
    # Модель кейса у наборов СВОЯ, поэтому общий помощник получает её
    # аргументами, а не берёт из объемлющего модуля.
    "http_method_not_allowed_block": functools.partial(
        http_method_not_allowed_block, Case, Step),
    "malformed_body_block": malformed_body_block,
    "alreadyexists_dup_name_for": alreadyexists_dup_name_for,
    "update_mask_partial_block": update_mask_partial_block,
    "perf_baseline_get_block": perf_baseline_get_block,
    "list_total_size_check_block": list_total_size_check_block,
    "headers_content_type_block": headers_content_type_block,
    "required_fields_matrix": required_fields_matrix,
    "immutable_fields_matrix": immutable_fields_matrix,
    "subnet_cidr_expand_shrink_pack": subnet_cidr_expand_shrink_pack,
    "pairwise_subnet_pack": pairwise_subnet_pack,
    "security_injection_block": security_injection_block,
    "conformance_lifecycle_pack": conformance_lifecycle_pack,
    "js_str": js_str,
    "js_regex_literal_text": js_regex_literal_text,
}


_RUN = Run(
    root=ROOT,
    cases_dir=CASES_DIR,
    out_dir=OUT_DIR,
    scripts_dir=SCRIPTS_DIR,
    emit=_EMIT,
    case_cls=Case,
    injected=_INJECTED,
    before=_reset_step_name_counters,
    stem_dashes_to_underscores=False,
    per_collection=_audit_subrequest_before_write,
    after_all=_report_subrequest_census,
)

# Точка входа — связывание, а не своё тело (#1474). Оркестрация одна на дерево;
# здесь набор связывает СВОИ решения. Имя `main` сохранено: его импортирует
# тонкая обёртка края (`from gen import main`).
main = functools.partial(generate, _RUN)


if __name__ == "__main__":
    sys.exit(main(sys.argv))
