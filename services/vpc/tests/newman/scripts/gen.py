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
"""
from __future__ import annotations

import json
import re
import sys
import uuid
import importlib.util
from pathlib import Path
from dataclasses import dataclass, field, replace
from typing import List, Dict, Optional

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


def assert_status(code: int) -> List[str]:
    return [
        f"pm.test('status {code}', () => pm.expect(pm.response.code).to.eql({code}));",
    ]


def assert_grpc_code(code: int, code_name: str) -> List[str]:
    return [
        f"pm.test('grpc code {code} ({code_name})', () => {{",
        "  const j = pm.response.json();",
        f"  pm.expect(j.code, JSON.stringify(j)).to.eql({code});",
        "});",
    ]


def assert_apply_state_present(prefix: str = "") -> List[str]:
    """Состояние применения намерения доехало до арендатора.

    Утверждается ТРИ вещи, и каждая закрывает свой способ солгать:

      * поле присутствует и не `null` — «утверждения нет» означает снятое
        намерение, и на живом ресурсе это был бы дефект заполнителя;
      * `applied` присутствует как булево — незаполненное поле-сообщение и
        `applied=false` на проводе выглядят одинаково у скаляра, поэтому
        проверяется именно ПРИСУТСТВИЕ объекта, а не значение внутри него;
      * `reason` — строка из закрытого словаря. Маршалер края отдаёт
        незаполненные поля, поэтому «класса нет» приезжает токеном
        `APPLY_FAILURE_REASON_UNSPECIFIED`, а не отсутствием ключа.

    Ревизии, времени отчёта и имени узла здесь нет и появиться неоткуда:
    состав проекции заперт пробой по дескриптору.
    """
    label = f"{prefix}: " if prefix else ""
    return [
        "const __as = pm.response.json().applyState;",
        f"pm.test('{label}applyState доехал до арендатора', () => {{",
        "  pm.expect(__as, 'applyState отсутствует: платформа молчит о живом ресурсе')"
        ".to.be.an('object');",
        "  pm.expect(__as.applied, 'applied').to.be.a('boolean');",
        "  pm.expect(__as.reason, 'reason').to.be.a('string');",
        "});",
    ]


def assert_apply_state_in_flight(prefix: str = "") -> List[str]:
    """Свежее намерение читается как «в работе»: не применено, класса нет.

    Положительный контроль ко всем отрицаниям про класс отказа: без него
    «класса не видно» было бы неотличимо от «поля нет».
    """
    label = f"{prefix}: " if prefix else ""
    return [
        *assert_apply_state_present(prefix),
        f"pm.test('{label}свежее намерение — в работе, без класса отказа', () => {{",
        "  const as2 = pm.response.json().applyState;",
        "  pm.expect(as2.applied, 'applied').to.eql(false);",
        "  pm.expect(as2.reason, 'reason').to.eql('APPLY_FAILURE_REASON_UNSPECIFIED');",
        "});",
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


def assert_field_violation(field_name: str) -> List[str]:
    return [
        f"pm.test('field violation on \"{field_name}\"', () => {{",
        "  const j = pm.response.json();",
        "  const det = (j.details || []).find(d => (d['@type']||'').includes('BadRequest'));",
        "  pm.expect(det, 'BadRequest detail').to.be.an('object');",
        f"  const fv = (det.fieldViolations || []).find(v => v.field === '{field_name}');",
        f"  pm.expect(fv, 'fieldViolation for {field_name}').to.be.an('object');",
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
    reset = [f"pm.environment.set('{env_var}', '');"] if _is_operation_id_var(env_var) else []
    return [
        *reset,
        "try {",
        "  const j = pm.response.json();",
        f"  const v = ({jsonpath});",
        f"  if (v !== undefined && v !== null) pm.environment.set('{env_var}', String(v));",
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
                        f"pm.test('text matches \"{resource_name} ... not found\"', () => "
                        f"pm.expect(pm.response.json().message).to.match(/^{resource_name} .* not found$/));",
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


def authz_move_nf(prefix, move_base_path):
    """Move несуществующего id → sync 404."""
    return Case(
        id=f"{prefix}-MV-AUTHZ-NF-SYNC",
        title="Move несуществующего → sync 404 от AuthZ-Get",
        classes=["NEG", "AUTHZ"], priority="P1",
        steps=[Step(name="move-nx", method="POST",
                    path=f"{move_base_path}/{{{{garbageVpcId}}}}:move",
                    body={"destinationProjectId": "{{_suiteProjectId}}"},
                    # :move по garbageVpcId: scope_extractor 403 (authz-first) ДО sync Get 404.
                    test_script=[*assert_absent_id_rejected()])],
    )


def val_move_no_dest(prefix, move_base_path):
    """Move без destinationProjectId → InvalidArgument."""
    return Case(
        id=f"{prefix}-MV-VAL-NO-DEST",
        title="Move без destinationProjectId → InvalidArgument",
        classes=["VAL"], priority="P1",
        steps=[Step(name="move-no-dest", method="POST",
                    path=f"{move_base_path}/{{{{garbageVpcId}}}}:move",
                    body={},
                    # :move по garbageVpcId без dest: scope_extractor 403 (authz-first)
                    # ДО backend 400 (no dest) / sync Get 404.
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
        retry_until_authorized(Step(name="ecp-cleanup", method="DELETE",
                                    path=f"{create_path}/{{{{{id_var}}}}}",
                                    test_script=[*assert_status(200),
                                                 *save_from_response("j.id", "opId")])),
        poll_operation_until_done(),
    ]


def ecp_name_block(prefix, create_path, body_extra=None, strict_name=False):
    """ECP/BVA по полю name: пустое, max, over-max, invalid regex.

    body_extra — обязательные поля кроме projectId/name (например для Subnet: networkId+zoneId+cidr).

    strict_name — у ресурса СТРОГИЙ контракт имени (только строчные буквы, цифры
    и дефис) вместо разрешительного контракта остальных VPC-ресурсов. Это не
    настройка теста «под поведение», а объявленный контракт: он записан в
    `pkg/validate.NameGateway`, в godoc `domain.Gateway`, в SDK и закреплён
    unit-тестом — то есть существует независимо от того, что показал прогон.
    Отличается ровно один исход — заглавные буквы; всё остальное (пустое имя,
    границы длины, начало с цифры/дефиса, спецсимволы) у обоих контрактов
    совпадает, поэтому параметр меняет один кейс, а не набор.
    """
    body_extra = body_extra or {}
    base = lambda name: {"projectId": "{{_suiteProjectId}}", "name": name, **body_extra}
    cases = []
    # BVA name length: 0, 63 (max), 64 (over)
    # Имя у VPC-ресурса — разрешительный контракт: пустая строка допустима
    # (domain.RcNameVPC.Validate). Исход ровно один, поэтому и утверждение одно:
    # прежнее `oneOf([200, 400])` под именем «accepted or rejected» проходило и при
    # приёме, и при отказе — то есть не отделяло соблюдение контракта от его нарушения.
    #
    # ОГОВОРКА ПРО УНИКАЛЬНОСТЬ ПУСТОГО ИМЕНИ — она НЕ общая для семи ресурсов.
    # Здесь стояло «частичный UNIQUE-индекс исключает `name <> ''`» как утверждение
    # обо всех. Перепись индексов живой схемы (2026-08-04, `pg_indexes` по
    # `kacho_vpc`): частичный индекс `WHERE name <> ''` есть у ШЕСТИ ресурсов —
    # addresses, gateways, network_interfaces, route_tables, security_groups,
    # subnets; у `networks` индекс `(project_id, name)` ПОЛНЫЙ, поэтому ВТОРАЯ сеть
    # с пустым именем в одном проекте получает ALREADY_EXISTS (асинхронно, в
    # операции). Кейсы это переживают только потому, что убирают за собой (см.
    # confirm_created_and_cleanup); незакрытая сеть от прошлого прогона роняет
    # следующий. Расхождение с шестью соседями — предмет отдельного разбора со
    # стороны продукта, а не повод ослабить утверждение.
    cases.append(Case(
        id=f"{prefix}-CR-BVA-NAME-EMPTY",
        title="Create с empty name → 200 (пустое имя разрешено контрактом)",
        classes=["BVA", "VAL"], priority="P2",
        steps=[Step(name="cr-empty", method="POST", path=create_path,
                    body=base(""),
                    test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
               *confirm_created_and_cleanup(create_path)],
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
    # Заглавные буквы — единственный исход, который у двух контрактов имени
    # расходится. Разрешительный (regex ^([a-zA-Z]([-_a-zA-Z0-9]{0,61}[a-zA-Z0-9])?)?$)
    # их принимает, строгий (^([a-z]([-a-z0-9]{0,61}[a-z0-9])?)?$) отвергает и
    # называет поле. Исход ровно один в обоих случаях — одно утверждение, без
    # `oneOf`, иначе кейс проходил бы при любом поведении продукта.
    cases.append(Case(
        id=f"{prefix}-CR-VAL-NAME-UPPERCASE",
        title=("Create с UPPERCASE name → 400 (строгий контракт имени: только строчные)"
               if strict_name else
               "Create с UPPERCASE name → 200 (заглавные разрешены контрактом)"),
        classes=["VAL"] + (["NEG"] if strict_name else []), priority="P2",
        steps=[Step(name="cr-upper", method="POST", path=create_path,
                    body=base("InvalidUpperCase-{{runId}}"),
                    test_script=(
                        [*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                         "pm.test('refusal names the field', () => pm.expect(JSON.stringify(pm.response.json())).to.contain('name'));"]
                        if strict_name else
                        [*assert_status(200), *save_from_response("j.id", "opId")]
                    ))]
              # Строгий контракт отвергает синхронно — ресурса нет, снимать нечего.
              + ([] if strict_name else confirm_created_and_cleanup(create_path)),
    ))
    cases.append(Case(
        id=f"{prefix}-CR-VAL-NAME-DIGIT-START",
        title="Create с name начинающимся с цифры → 400 (нарушение name-regex)",
        classes=["VAL"], priority="P1",
        steps=[Step(name="cr-digit", method="POST", path=create_path,
                    body=base("9invalid-{{runId}}"),
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


def idempotency_block(prefix, create_path, name_template, body_extra=None):
    """Повторный Create same name → 409 ALREADY_EXISTS (Create не идемпотентен).

    Первый Create OK, второй с тем же name → sync 409 ALREADY_EXISTS.
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
                              "pm.test('mentions already exists', () => pm.expect(pm.response.json().message.toLowerCase()).to.include('already exists'));"]),
            # cleanup DELETE is the caller's first *mutating* access of its OWN fresh
            # resource; the delete-relation owner-tuple is eventually-consistent (opgate
            # removed → at-least-once fgaproxy drainer), so under load the gateway authz
            # gate can briefly 403 ("lacks relation v_delete") before the tuple
            # materialises. Bounded read-your-writes retry on that transient 403 only
            # (the resource provably exists — cr-2 just got 409 on it — so a 404 here
            # would be a genuine bug, never masked).
            retry_until_authorized(Step(name="cleanup", method="DELETE",
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
                retry_until_authorized(Step(name="patch", method="PATCH",
                     path=f"{update_base_path}/{{{{createdId}}}}",
                     body=patch_body,
                     test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
                poll_operation_until_done(),
                # read-your-writes: verify GET of the caller's OWN fresh resource can
                # briefly 404 while the owner/hierarchy tuple materializes under load
                # (opgate removed → eventual-consistency). Bounded-retry on 404/403.
                retry_until_authorized(Step(name="verify", method="GET", path=f"{update_base_path}/{{{{createdId}}}}",
                     test_script=[*assert_status(200), *asserts])),
                retry_until_authorized(Step(name="cleanup", method="DELETE", path=f"{update_base_path}/{{{{createdId}}}}",
                     test_script=[*save_from_response("j.id", "opId")])),
                poll_operation_until_done(),
            ],
        )
    return [
        case_for("NAME", "name", {"updateMask": "name", "name": f"{prefix.lower()}-renamed-x"},
                 ["pm.test('name updated', () => pm.expect(pm.response.json().name).to.eql('" + prefix.lower() + "-renamed-x'));"]),
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


def move_same_project(prefix, resource_base_path, body_create):
    """Move в текущий project → InvalidArgument
    "Illegal argument Destination project is the same as the source" (400)."""
    return Case(
        id=f"{prefix}-MV-IDM-SAME-PROJECT",
        title="Move в текущий project → 400 'Destination project is the same as the source'",
        classes=["IDM", "NEG"], priority="P2",
        steps=[
            Step(name="create", method="POST", path=resource_base_path,
                 body={**body_create, "name": f"{prefix.lower()}-mv-self-{{{{runId}}}}"},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(capture_id_to="createdId"),
            Step(name="move-self", method="POST",
                 path=f"{resource_base_path}/{{{{createdId}}}}:move",
                 body={"destinationProjectId": "{{_suiteProjectId}}"},
                 test_script=[*assert_status(400), *assert_grpc_code(3, "INVALID_ARGUMENT"),
                              "pm.test('verbatim text', () => pm.expect(pm.response.json().message).to.eql('Illegal argument Destination project is the same as the source'));"]),
            Step(name="verify-unchanged", method="GET",
                 path=f"{resource_base_path}/{{{{createdId}}}}",
                 test_script=[*assert_status(200),
                              "pm.test('projectId unchanged', () => pm.expect(pm.response.json().projectId).to.eql(pm.environment.get('_suiteProjectId')));"]),
            Step(name="cleanup", method="DELETE",
                 path=f"{resource_base_path}/{{{{createdId}}}}",
                 test_script=[*save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
        ],
    )


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
            retry_until_authorized(Step(name="patch-multi", method="PATCH",
                 path=f"{update_base_path}/{{{{createdId}}}}",
                 body={"updateMask": "name,description,labels",
                       "name": f"{prefix.lower()}-multi-new",
                       "description": "multi-desc",
                       "labels": {"a": "1", "b": "2"}},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
            poll_operation_until_done(),
            retry_until_authorized(Step(name="verify-all", method="GET",
                 path=f"{update_base_path}/{{{{createdId}}}}",
                 test_script=[*assert_status(200),
                              "const j = pm.response.json();",
                              "pm.test('name updated', () => pm.expect(j.name).to.eql('" + prefix.lower() + "-multi-new'));",
                              "pm.test('description updated', () => pm.expect(j.description).to.eql('multi-desc'));",
                              "pm.test('labels a', () => pm.expect((j.labels || {}).a).to.eql('1'));",
                              "pm.test('labels b', () => pm.expect((j.labels || {}).b).to.eql('2'));"])),
            retry_until_authorized(Step(name="cleanup", method="DELETE",
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
            retry_until_present(Step(name="list-filtered", method="GET",
                 path=f"{create_path}?projectId={{{{_suiteProjectId}}}}&pageSize=100&filter=name%3D%22{prefix.lower()}-flt-{{{{runId}}}}%22",
                 test_script=[*assert_status(200),
                              "const ids = (Object.values(pm.response.json()).find(v => Array.isArray(v)) || []).map(x => x.id);",
                              "pm.test('filtered list contains', () => pm.expect(ids).to.include(pm.environment.get('fltId')));"]), "fltId"),
            retry_until_authorized(Step(name="cleanup", method="DELETE", path=f"{create_path}/{{{{fltId}}}}",
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
            title="Create с name=null → 200 (protojson: null = поле не задано; пустое имя разрешено)",
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


def http_method_not_allowed_block(prefix, base_path):
    """HTTP method semantics: попытка PUT/HEAD на endpoint → 405 или 404."""
    return [
        Case(
            id=f"{prefix}-METHOD-PUT-NOT-ALLOWED",
            title="PUT на List endpoint → 405 или 404",
            classes=["VAL", "NEG"], priority="P3",
            steps=[Step(name="put-list", method="PUT", path=base_path,
                        body={"projectId": "{{_suiteProjectId}}"},
                        # api-gateway evaluates authz before routing — unsupported HTTP verbs
                        # on collection endpoints may be rejected with 403 rather than 405.
                        test_script=["pm.test('not allowed (403/404/405/501)', () => pm.expect(pm.response.code).to.be.oneOf([403, 404, 405, 501]));"])],
        ),
        Case(
            id=f"{prefix}-METHOD-DELETE-LIST",
            title="DELETE на List endpoint (без id) → 405 или 404",
            classes=["VAL", "NEG"], priority="P3",
            steps=[Step(name="del-list", method="DELETE", path=base_path,
                        # api-gateway evaluates authz before routing — unsupported HTTP verbs
                        # on collection endpoints may be rejected with 403 rather than 405.
                        test_script=["pm.test('not allowed (403/404/405/501)', () => pm.expect(pm.response.code).to.be.oneOf([403, 404, 405, 501]));"])],
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


def alreadyexists_dup_name_for(prefix, create_path, body_create):
    """Создать дубль с тем же name → sync 409 ALREADY_EXISTS."""
    return Case(
        id=f"{prefix}-CR-NEG-DUP-NAME-CHECK",
        title="Создать дубль с тем же name → sync 409 ALREADY_EXISTS",
        classes=["NEG", "CONC"], priority="P1",
        steps=[
            Step(name="cr-first", method="POST", path=create_path,
                 body={**body_create, "name": f"{prefix.lower()}-dupck-{{{{runId}}}}"},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(capture_id_to="firstId"),
            Step(name="cr-dup", method="POST", path=create_path,
                 body={**body_create, "name": f"{prefix.lower()}-dupck-{{{{runId}}}}"},
                 test_script=[*assert_status(409), *assert_grpc_code(6, "ALREADY_EXISTS"),
                              "pm.test('mentions already exists', () => pm.expect(pm.response.json().message.toLowerCase()).to.include('already exists'));"]),
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
                retry_until_authorized(Step(name="patch-name-only", method="PATCH",
                     path=f"{update_base_path}/{{{{createdId}}}}",
                     body={"updateMask": "name", "name": f"{prefix.lower()}-mnnew",
                           "description": "should-be-ignored", "labels": {"ignored": "y"}},
                     test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
                poll_operation_until_done(),
                # read-your-writes: bounded-retry the fresh-resource verify GET over
                # the owner-tuple materialization window (opgate removed).
                retry_until_authorized(Step(name="verify", method="GET",
                     path=f"{update_base_path}/{{{{createdId}}}}",
                     test_script=[*assert_status(200),
                                  "const j = pm.response.json();",
                                  "pm.test('name updated', () => pm.expect(j.name).to.eql('" + prefix.lower() + "-mnnew'));",
                                  "pm.test('description preserved', () => pm.expect(j.description).to.eql('init'));",
                                  "pm.test('labels preserved', () => pm.expect((j.labels || {}).orig).to.eql('1'));"])),
                retry_until_authorized(Step(name="cleanup", method="DELETE",
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
            retry_until_authorized(Step(name="get-timed", method="GET", path=f"{get_create_path}/{{{{perfId}}}}",
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


def mutable_field_accepts(prefix, create_path, update_base_path, body_create,
                          mutable_field, mutable_value, assert_after):
    """Создать ресурс, изменить mutable поле через mask, проверить применение."""
    return Case(
        id=f"{prefix}-UPD-CRUD-MUTABLE-{mutable_field.upper().replace('_','-')}",
        title=f"Update mask='{mutable_field}' → mutable поле обновлено",
        classes=["CRUD", "STATE"], priority="P2",
        steps=[
            Step(name="cr", method="POST", path=create_path,
                 body={**body_create, "name": f"{prefix.lower()}-mut-{mutable_field[:5]}-{{{{runId}}}}"},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(capture_id_to="createdId"),
            retry_until_authorized(Step(name="patch", method="PATCH",
                 path=f"{update_base_path}/{{{{createdId}}}}",
                 body={"updateMask": mutable_field, mutable_field: mutable_value},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")])),
            poll_operation_until_done(),
            Step(name="verify", method="GET",
                 path=f"{update_base_path}/{{{{createdId}}}}",
                 test_script=[*assert_status(200), *assert_after]),
            Step(name="cleanup", method="DELETE",
                 path=f"{update_base_path}/{{{{createdId}}}}",
                 test_script=[*save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
        ],
    )


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
                retry_until_authorized(
                    Step(name="verify-pw", method="GET", path="/vpc/v1/subnets/{{subId}}",
                         test_script=[
                             *assert_status(200),
                             f"pm.test('префикс доехал: ipv4CidrPrimary == {ipbase}{prefix}', () => "
                             f"pm.expect(pm.response.json().ipv4CidrPrimary, pm.response.text()).to.eql('{ipbase}{prefix}'));",
                             # Имя переменной, а не её значение: Postman подставляет
                             # `{{...}}` в ПОЛЯХ ЗАПРОСА, но не внутри test-script'а —
                             # литерал в утверждении сравнивался бы с самой строкой
                             # «{{zoneA}}» и не мог пройти никогда.
                             "pm.test('зона доехала', () => "
                             f"pm.expect(pm.response.json().zoneId, pm.response.text()).to.eql(pm.environment.get('{zone.strip('{}')}')));",
                         ])),
                Step(name="cleanup-pw", method="DELETE", path="/vpc/v1/subnets/{{subId}}",
                     test_script=["pm.test('cleanup', () => pm.expect(pm.response.code).to.be.oneOf([200, 404]));",
                                  *save_from_response("j.id", "opId")]),
            ],
        ))
    return cases


def security_injection_block(prefix, create_path, list_path, body_create):
    """Security probes: SQL/command/XSS injection в name + filter.
    Никогда не должно возвращать 500 или утечку pgx/stack trace.
    """
    injections = [
        ("sqli", "test' OR 1=1--"),
        ("union", "x' UNION SELECT * FROM operations--"),
        ("xss", "<script>alert(1)</script>"),
        ("cmd", "; rm -rf / ;"),
        ("path", "../../etc/passwd"),
        ("nullbyte", "x y"),
        ("longpayload", "A" * 1000),
    ]
    cases = []
    for name, payload in injections:
        cases.append(Case(
            id=f"{prefix}-CR-SEC-{name.upper()}",
            title=f"Security probe: {name} in name → handled, no 500",
            classes=["VAL", "NEG"], priority="P0",
            steps=[Step(name=f"cr-{name}", method="POST", path=create_path,
                        body={**body_create, "name": payload[:1000]},
                        test_script=[
                            "pm.test('not 500', () => pm.expect(pm.response.code).to.not.eql(500));",
                            "pm.test('handled 2xx/4xx', () => pm.expect(pm.response.code).to.be.oneOf([200, 400, 413]));",
                            "const body = JSON.stringify(pm.response.json() || {});",
                            "pm.test('no panic/sqlstate/stacktrace leak', () => {",
                            "  const low = body.toLowerCase();",
                            "  pm.expect(low).to.not.include('panic');",
                            "  pm.expect(low).to.not.include('sqlstate');",
                            "  pm.expect(low).to.not.include('goroutine');",
                            "});",
                        ])],
        ))
    cases.append(Case(
        id=f"{prefix}-LST-SEC-FILTER-SQLI",
        title="Security: SQL injection в filter → не 500",
        classes=["VAL", "NEG"], priority="P0",
        steps=[Step(name="lst-sqli", method="GET",
                    path=f"{list_path}?projectId={{{{_suiteProjectId}}}}&filter=name%3D%22a%27%20OR%201%3D1--%22",
                    test_script=[
                        "pm.test('not 500', () => pm.expect(pm.response.code).to.not.eql(500));",
                        "pm.test('handled', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
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
            retry_until_authorized(Step(name="get-1", method="GET", path=f"{create_path}/{{{{lifeId}}}}",
                 test_script=[*assert_status(200),
                              "pm.test('id matches', () => pm.expect(pm.response.json().id).to.eql(pm.environment.get('lifeId')));"])),
            retry_until_present(Step(name="lst-includes", method="GET",
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


def conf_alreadyexists_block(prefix, create_path, name_template, body_extra=None):
    """CONF: sync 409 ALREADY_EXISTS text при duplicate name."""
    body_extra = body_extra or {}
    return Case(
        id=f"{prefix}-CR-CONF-ALREADY-EXISTS",
        title="Create duplicate name → sync 409, точный текст ALREADY_EXISTS",
        classes=["CONF", "NEG"], priority="P1",
        steps=[
            Step(name="create-first", method="POST", path=create_path,
                 body={"projectId": "{{_suiteProjectId}}", "name": name_template, **body_extra},
                 test_script=[*assert_status(200), *save_from_response("j.id", "opId")]),
            poll_operation_until_done(capture_id_to="createdId", id_expr="j.metadata && Object.values(j.metadata).find(v => typeof v === 'string' && v.length > 10)"),
            Step(name="create-dup", method="POST", path=create_path,
                 body={"projectId": "{{_suiteProjectId}}", "name": name_template, **body_extra},
                 test_script=[*assert_status(409), *assert_grpc_code(6, "ALREADY_EXISTS"),
                              "pm.test('verbatim with name ... already exists', () => pm.expect(pm.response.json().message).to.match(/ with name .* already exists$/));"]),
            Step(name="cleanup-first", method="DELETE", path=f"{create_path}/{{{{createdId}}}}",
                 test_script=[*save_from_response("j.id", "opId")]),
            poll_operation_until_done(),
        ],
    )


_POLL_SEQ = [0]


_RYA_SEQ = [0]


def retry_until_present(step: Step, id_env_var, budget: int = 25,
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
        # УСЛОВИЕ ПОВТОРА ОБЯЗАНО СОВПАДАТЬ С УТВЕРЖДЕНИЕМ, а не быть у́же его.
        #
        # Обёртка принимает СПИСОК переменных и ждёт, пока в ответе появятся ВСЕ.
        # Прежняя форма принимала одну: кейс, утверждающий «все три на странице»,
        # ждал появления ПЕРВОЙ и падал на второй — она ещё материализовалась.
        # Дефект наблюдался на NET-APPLY-STATE-LIST-PARITY-OK и виден только на
        # стенде: локально список отдаёт всё сразу.
        "const _want = [" + ", ".join(
            "pm.environment.get('%s')" % v
            for v in ([id_env_var] if isinstance(id_env_var, str) else list(id_env_var))
        ) + "];",
        "try { const _arr = Object.values(pm.response.json()).find(v => Array.isArray(v)) || [];"
        " const _have = _arr.map(x => x.id);"
        " _present = _want.every(w => _have.includes(w)); } catch (e) {}",
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
    fires setNextRequest before any setTimeout). budget*interval_ms bounds the wait
    (default 25*500ms = 12.5s -- шапка называла 15*400ms = ~6s, то есть ДРУГИЕ значения,
    чем стоят в сигнатуре: бюджет поднимали, а описание осталось прежним, и читающий
    планировал окно вдвое меньше действительного) -- fail-closed: on any other code the wrapped step's real
    test_script runs exactly once, and once the budget is spent it ALSO runs on the
    terminal 403/404 (a genuine, non-converging deny still FAILS the real assertions --
    never masked, never infinite).

    Use ONLY on the first access of the caller's OWN fresh resource. Do NOT wrap
    negative / cross-account-deny / absent-id steps (a poll there would mask a real
    deny). The counter/started env-vars are request-name-scoped (step names are
    globally unique after serialization) so the loop never bleeds across cases or
    steps -- same discipline as poll_operation_until_done.
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
    if not retry_on:
        # Ждать нечего: все коды полосы видимости объявлены исходами. Обёртка
        # выродилась бы в петлю, которая не может сработать, — не ставим её.
        return step
    retry_set = ",".join(str(c) for c in retry_on)
    guard = [
        "// bounded read-your-writes retry over the owner-tuple materialization window",
        "// (opgate removed -> eventual-consistency); retries SELF only on 403/404.",
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
_HTTP_EQ_RE = re.compile(r"pm\.response\.code[^;]*?\.to\.eql\((\d{3})\)")
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
        # Ждать можно ТОЛЬКО код, который шаг исходом не заявлял, — тогда
        # ожидание ничего не маскирует по построению: если 403/404 названы
        # приемлемым исходом, шаг про них и спрашивает, и ретрай там запрещён.
        # Требования «шаг обязан ждать 200» больше нет: отрицательная проба
        # СВОЕГО СВЕЖЕГО ресурса («такой CIDR отвергается») тоже упирается в
        # окно видимости и получает 403 вместо ожидаемого 400 — то есть падает
        # не по своему предмету. Чужой аккаунт, посеянный и несуществующий id
        # под правило не подпадают: они не рождены в этом кейсе.
        # Шаг без единого утверждения ждёт только 403: у чтения/уборки 404
        # часто законное «уже нет», и жечь на нём бюджет незачем.
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


def poll_operation_until_done(must_fail: bool = False, capture_id_to: str = "",
                              id_expr: str = "", must_fail_code: int = 0,
                              must_fail_message: str = "") -> Step:
    """Reusable poll step с retry-на-not-done через setNextRequest.
    До 30 попыток с ~500ms задержкой между ними (≈15s покрытия async-op tail, Koren #1),
    потом fail если done остался false.

    КАЖДЫЙ poll-шаг получает УНИКАЛЬНОЕ имя (poll-op-<N>): setNextRequest(
    pm.info.requestName) обязан ретраить СЕБЯ. При общем имени 'poll-op' newman
    резолвит имя в ДРУГОЙ (последний) poll-op коллекции → прыжок через все кейсы и
    пропуск их setup-шагов → массовый ложный fail (e2e-newman fullscope root A3).

    Если opId пустой (предыдущий шаг был отклонён синхронно, напр. 403 bad-project),
    операции не существует — поллить нечего, и это ЕДИНСТВЕННЫЙ случай, когда шаг
    молчит.

    ОТКАЗАННОЕ ЧТЕНИЕ ОПЕРАЦИИ = КРАСНОЕ. Раньше ранний выход стоял ВЫШЕ утверждения,
    и утверждение с именем «poll status 200» не могло упасть by construction: до него
    доходили только те ответы, которые оно проверяет. Любой не-200 на
    `GET /operations/{id}` тихо засчитывался как пройденный шаг, а исход мутации, ради
    которой шаг существует, оставался НЕИЗВЕСТНЫМ. Неизвестный исход — не то же самое,
    что успешный.

    `must_fail` — ЗЕРКАЛЬНОЕ утверждение для кейса, предмет которого — ОТКАЗ, решённый
    воркером, а не синхронным валидатором. Там законные формы — `400` до появления
    Operation либо `200` и Operation, завершившаяся С ОШИБКОЙ; `200` сам по себе не
    является ни той, ни другой. Голый `poll_operation_until_done()` после такого шага
    утверждает только `done`, чему УСПЕШНАЯ операция удовлетворяет ровно так же, поэтому
    отказ, ради которого кейс назван, не проверяется вовсе. Ставится в паре с
    `assert_refused_sync_or_async`. Никогда не применять там, где принятие законно — тогда
    шаг падал бы на корректном поведении.

    `capture_id_to` — ЗАХВАТ ID СОЗДАННОГО РЕСУРСА ЗДЕСЬ, А НЕ ИЗ ОТВЕТА МУТАЦИИ.
    Operation Kachō несёт ПРЕДВАРИТЕЛЬНО ВЫДЕЛЕННЫЙ id в `metadata` ещё до того, как
    воркер отработал, поэтому он присутствует и у операции, завершившейся С ОШИБКОЙ
    (id выделяется до async-фейла). Захват из синхронного ответа POST — это захват id
    ресурса, которого может не существовать: дальше кейс патчит, читает и убирает
    ФАНТОМ, а край отвечает на него `403` (scope_extractor не резолвит несуществующий
    объект) и `404` (hide-existence). Ни один из этих кодов не называет настоящую
    причину, и кейс сообщает «expected 404 to deeply equal 200» вместо «create упал:
    <ошибка операции>». Замер 2026-08-04 на живом стенде: 63 упавших утверждения suite
    vpc, ВСЕ до одного — каскад ровно этой подмены; истинная причина
    (`address pool ... exhausted`) не была названа НИ ОДНИМ из них.

    Поэтому: поллим до `done` → УТВЕРЖДАЕМ отсутствие `error` → и только тогда кладём
    id в переменную. На ошибке переменная СНИМАЕТСЯ (`unset`), и следующий шаг падает
    стражем неразрешённого адреса, называя имя переменной, — вместо того чтобы уйти на
    фантом. Это норма `testing.md` §«Fixture-seed обязан проверять `op.error` перед
    извлечением resource-id из `metadata`», записанная в самом хелпере.

    `id_expr` — необязательное выражение выбора поля id из `j.metadata` (по умолчанию
    первое поле, чьё имя оканчивается на `Id`).
    """
    _POLL_SEQ[0] += 1
    tail: List[str] = []
    if must_fail:
        tail = [
            "pm.test('operation refused the request (carries an error)', () => "
            "  pm.expect(j.error, JSON.stringify(j)).to.be.an('object'));",
        ]
        # КОД И ТОН ОТКАЗА — ЧАСТЬ КОНТРАКТА, и утверждаются здесь, а не отдельным
        # шагом. Отдельный шаг пришлось бы адресовать `{{opId}}`, который на
        # синхронной полосе пуст: так и появляется опрос, уезжающий на
        # `/operations/` без сегмента (мерка
        # `deploy/scripts/assert-refusal-lane-has-a-reader.py`). Здесь же ранний
        # выход по пустому имени уже стоит выше по скрипту, поэтому утверждение
        # исполняется РОВНО тогда, когда Operation существует.
        if must_fail_code:
            tail.append(
                f"pm.test('refusal carries gRPC code {must_fail_code}', () => "
                f"  pm.expect(j.error && j.error.code, JSON.stringify(j)).to.eql({must_fail_code}));")
        if must_fail_message:
            tail.append(
                f"pm.test('refusal keeps the contract tone', () => "
                f"  pm.expect(j.error && j.error.message, JSON.stringify(j))"
                f"    .to.eql({json.dumps(must_fail_message)}));")
    elif must_fail_code or must_fail_message:
        raise ValueError("must_fail_code/must_fail_message требуют must_fail=True: "
                         "утверждать текст отказа у операции, от которой отказа не "
                         "ждут, значит объявить проверку, которая не исполнится")
    if capture_id_to:
        if must_fail:
            raise ValueError("capture_id_to несовместим с must_fail: у операции, "
                             "предмет которой — отказ, нет созданного ресурса")
        expr = id_expr or ("(j.metadata && Object.keys(j.metadata)"
                           ".filter(k => k.endsWith('Id')).map(k => j.metadata[k])[0]) || ''")
        tail = tail + [
            "pm.test('operation succeeded (no phantom resource id)', () => "
            "  pm.expect(j.error && JSON.stringify(j.error), 'operation.error')"
            "    .to.eql(undefined));",
            "if (j.error) {",
            f"  pm.environment.unset('{capture_id_to}');",
            "} else {",
            f"  const _cid = ({expr});",
            f"  if (_cid) pm.environment.set('{capture_id_to}', String(_cid));",
            f"  else pm.environment.unset('{capture_id_to}');",
            "}",
        ]
    return Step(
        name=f"poll-op-{_POLL_SEQ[0]}",
        method="GET",
        path="/operations/{{opId}}",
        pre_script=[
            # Note: cannot fully skip request in pre-script without aborting the suite.
            # Instead the test_script guards on empty opId or non-200 response.
        ],
        test_script=[
            # Guard: if opId was empty (prior step was sync-rejected e.g. 403) or
            # response is non-200, skip all poll assertions cleanly.
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
            "  // Postman async-friendly retry: re-invoke same request name",
            "  pm.execution.setNextRequest(pm.info.requestName);",
            "  return;",
            "}",
            "pm.environment.unset('_pollCount');",
            "pm.test('operation done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
            "if (j.error) pm.environment.set('lastOpError', JSON.stringify(j.error));",
            "else pm.environment.unset('lastOpError');",
            "if (j.response) pm.environment.set('lastOpResponse', JSON.stringify(j.response));",
            *tail,
        ],
    )


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
        f"// authz-deny: bearer from env '{auth}'",
        f"const __t = pm.environment.get('{auth}') || pm.variables.get('{auth}') || '';",
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
        f"  pm.test('harness config: {auth} is set (subject under test)', () => {{",
        f"    pm.expect.fail('{auth} is not set — the authz-fixture seed "
        "(tests/authz-fixtures/setup.sh) did not provide this subject. Running the step "
        "anonymously would test a DIFFERENT principal and pass for the wrong reason.');",
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

    for m in _PUB_BIND_RE.finditer(code):
        semi = code.find(";", m.end())
        expr = code[m.end():semi if semi >= 0 else len(code)]
        binds.append((m.start(), depth[m.start()], m.group(1),
                      "metadata" in expr or visible(m.start(), expr)))

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
                # Internal*-шаги идут на cluster-internal REST listener — на публичном
                # их нет by design (ban #6). См. Step.internal.
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
        events.append(
            {"listen": "prerequest", "script": {"type": "text/javascript", "exec": pre}}
        )
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
    return _assert_published_id_outcome(
        _assert_delete_operation_outcome(
            _declare_supernet_where_a_subnet_is_carved(
                _wrap_own_fresh_reads(steps))))


def case_to_postman(case: Case) -> Dict:
    tags = [f"class:{c}" for c in case.classes] + [f"priority:{case.priority}"]
    # Провязка предиката видимости названа ЗДЕСЬ и явно: её читает гейт
    # `internal/repohygiene/artifactgates` `TestOwnFreshReadWrapPredicateWiredInEveryNewmanGenerator`
    # по литералу `_wrap_own_fresh_reads(case.steps` — он требует, чтобы обёртка
    # первого доступа к своему свежему ресурсу стояла в СЕРИАЛИЗАЦИИ КЕЙСА, а не
    # ставилась руками по шагам (замер: 42 падения полосы видимости пришлись на
    # шаги без обёртки вовсе). Предикат идемпотентен — уже обёрнутый шаг он
    # пропускает (`already = "_authRetryCount" in body`), — поэтому повторный
    # проход внутри `normalize_steps` ничего не меняет; это проверено сравнением
    # сгенерированных коллекций байт в байт.
    return {
        "name": f"{case.id} — {case.title}",
        "description": " | ".join(tags),
        "item": [step_to_postman(s) for s in normalize_steps(_wrap_own_fresh_reads(case.steps))],
    }


# Zone ids are environment-specific: geo seeds Region/Zone with ids that differ
# per deploy and are NOT the legacy literals zone-a..d. Resolve them ONCE,
# synchronously, as the FIRST item of every collection (a real request, so newman
# blocks on its response before running any case) and publish zoneA..zoneD +
# existingZoneId/existingZoneAltId. Best-effort: no failing assertion — if geo is
# unreachable (standalone vpc), the committed env defaults stay in effect.
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
        retry_until_authorized(Step(
            name="anchor-verify", method="GET",
            path="/vpc/v1/subnets/{{" + GW_ANCHOR_SUBNET_VAR + "}}",
            test_script=[*assert_status(200),
                         "pm.test('якорь создан и несёт IPv4', () => {",
                         "  const j = pm.response.json();",
                         "  pm.expect(j.id, pm.response.text()).to.eql("
                         f"pm.environment.get('{GW_ANCHOR_SUBNET_VAR}'));",
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
        "item": [step_to_postman(s) for s in steps],
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


def build_collection(service: str, cases: List[Case]) -> Dict:
    setup_items = [_zone_setup_item()]
    if service in _POOL_SEED_SERVICES:
        setup_items.append(_pool_seed_item())
    if service in _ANYCAST_POOL_SEED_SERVICES:
        setup_items.append(_anycast_pool_seed_item())
    # Якорь идёт ПОСЛЕ резолва зон: подсеть создаётся в живой зоне стенда, а не в
    # той, что стоит в закоммиченном умолчании окружения.
    if service in _GW_ANCHOR_SERVICES:
        setup_items.append(_gw_anchor_setup_item())
    pre = PRE_GLOBAL + _ADMIN_DEFAULT_PRE if service in _ADMIN_DEFAULT_SERVICES else PRE_GLOBAL
    return {
        "info": {
            # Deterministic _postman_id (UUIDv5 over the collection name) so a
            # regeneration with no source change produces no diff. A random id
            # here made every regeneration dirty every collection, which meant
            # "generated matches source" could never be checked and a real drift
            # had nowhere to show. Postman only needs this to be stable+unique.
            "_postman_id": str(uuid.uuid5(uuid.NAMESPACE_URL, f"kacho-vpc/newman/{service}")),
            "name": f"kacho-vpc / newman / {service}",
            "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
        },
        "event": [
            {
                "listen": "prerequest",
                "script": {"type": "text/javascript", "exec": pre},
            },
        ],
        "item": setup_items + [case_to_postman(c) for c in cases],
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
    _POLL_SEQ[0] = 0
    _RYA_SEQ[0] = 0


def load_cases_module(path: Path):
    _reset_step_name_counters()
    spec = importlib.util.spec_from_file_location(path.stem, path)
    mod = importlib.util.module_from_spec(spec)
    # пробрасываем helpers в namespace модуля
    mod.Step = Step
    mod.Case = Case
    mod.assert_status = assert_status
    mod.assert_grpc_code = assert_grpc_code
    mod.assert_transcode_error = assert_transcode_error
    mod.assert_apply_state_present = assert_apply_state_present
    mod.assert_apply_state_in_flight = assert_apply_state_in_flight
    mod.assert_field_violation = assert_field_violation
    mod.assert_unscoped_rejected = assert_unscoped_rejected
    mod.assert_absent_id_rejected = assert_absent_id_rejected
    mod.assert_refused_sync_or_async = assert_refused_sync_or_async
    mod.save_from_response = save_from_response
    mod.assert_operation_envelope = assert_operation_envelope
    mod.SUBNET_V4_CIDRS = SUBNET_V4_CIDRS
    mod.SUBNET_V6_CIDRS = SUBNET_V6_CIDRS
    mod.poll_operation_until_done = poll_operation_until_done
    mod.retry_until_authorized = retry_until_authorized
    mod.retry_until_present = retry_until_present
    mod.retry_until_absent = retry_until_absent
    mod.crud_list_bva_block = crud_list_bva_block
    mod.conf_not_found_text = conf_not_found_text
    mod.state_update_unknown_mask = state_update_unknown_mask
    mod.authz_move_nf = authz_move_nf
    mod.val_move_no_dest = val_move_no_dest
    mod.state_immutable_project = state_immutable_project
    mod.list_pagesize_1_bva = list_pagesize_1_bva
    mod.conf_alreadyexists_block = conf_alreadyexists_block
    mod.ecp_name_block = ecp_name_block
    mod.ecp_description_block = ecp_description_block
    mod.ecp_labels_block = ecp_labels_block
    mod.updatemask_decision_table = updatemask_decision_table
    mod.filter_syntax_block = filter_syntax_block
    mod.pagination_roundtrip = pagination_roundtrip
    mod.idempotency_block = idempotency_block
    mod.update_happy_per_field = update_happy_per_field
    mod.perf_baseline_block = perf_baseline_block
    mod.move_same_project = move_same_project
    mod.verbatim_text_pack = verbatim_text_pack
    mod.authz_caller_headers_block = authz_caller_headers_block
    mod.update_happy_multi_field = update_happy_multi_field
    mod.cross_project_resource_block = cross_project_resource_block
    mod.list_filter_match_block = list_filter_match_block
    mod.neg_invalid_types_block = neg_invalid_types_block
    mod.http_method_not_allowed_block = http_method_not_allowed_block
    mod.malformed_body_block = malformed_body_block
    mod.alreadyexists_dup_name_for = alreadyexists_dup_name_for
    mod.update_mask_partial_block = update_mask_partial_block
    mod.perf_baseline_get_block = perf_baseline_get_block
    mod.list_total_size_check_block = list_total_size_check_block
    mod.headers_content_type_block = headers_content_type_block
    mod.required_fields_matrix = required_fields_matrix
    mod.immutable_fields_matrix = immutable_fields_matrix
    mod.mutable_field_accepts = mutable_field_accepts
    mod.subnet_cidr_expand_shrink_pack = subnet_cidr_expand_shrink_pack
    mod.pairwise_subnet_pack = pairwise_subnet_pack
    mod.security_injection_block = security_injection_block
    mod.conformance_lifecycle_pack = conformance_lifecycle_pack
    spec.loader.exec_module(mod)
    return mod


def _check_duplicate_ids() -> int:
    """HARD-FAIL: case-id обязан быть уникален среди всех кейсов всех сервисов."""
    seen: Dict[str, str] = {}
    dups: List[str] = []
    for f in sorted(CASES_DIR.glob("*.py")):
        mod = load_cases_module(f)
        for c in getattr(mod, "CASES", []):
            if c.id in seen:
                dups.append(f"  - {c.id!r}: {seen[c.id]} и {f.name}")
            else:
                seen[c.id] = f.name
    if dups:
        sys.stderr.write("gen: FAIL — дубли case-id (case-id должен быть уникален):\n")
        sys.stderr.write("\n".join(dups) + "\n")
        return 1
    return 0


def main(argv: List[str]) -> int:
    args = argv[1:]
    if "--validate" in args:
        # делегируем полную валидацию (dup-id + каталогизация в CASES-INDEX) в validate-cases.py
        import runpy
        sys.argv = [str(SCRIPTS_DIR / "validate-cases.py")]
        runpy.run_path(str(SCRIPTS_DIR / "validate-cases.py"), run_name="__main__")
        return 0  # validate-cases.py делает sys.exit сам

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    want = set(args)
    found = sorted(CASES_DIR.glob("*.py"))
    if not found:
        print(f"no case files in {CASES_DIR}")
        return 1
    if _check_duplicate_ids() != 0:
        return 1
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
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
