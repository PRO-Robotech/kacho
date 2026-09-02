#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""
tests/newman/scripts/gen.py — генератор Postman collections из декларативных case-файлов.

Использование:
    python3 scripts/gen.py             # все ресурсы
    python3 scripts/gen.py volume      # один ресурс

Источник истины — модули в tests/newman/cases/<resource>.py, каждый экспортирует
переменную CASES — список объектов Case (см. ниже).

Специфика storage (домен выделен из блочного хранения compute): REST-префикс
`/storage/v1/`, операции — `/operations/{id}` (общий OpsProxy api-gateway,
prefix `sop` — op-root storage; opsproxy маршрутизирует Operation.Get по первым
трём символам id → backend `storage`), env-var `garbageStorageId`.
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
import subprocess
import sys
import uuid
import importlib.util
from pathlib import Path
from dataclasses import dataclass, field, replace
from typing import List, Dict, Optional

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
    _RYA_SEQ,
    _accepted_http_codes,
    assert_created_at_seconds,
    _assert_delete_operation_outcome,
    assert_field_violation,
    assert_grpc_code,
    assert_op_refusal_message,
    assert_op_refusal_message_contains,
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
    _is_operation_id_var,
    _js_code_and_literals,
    js_comment,
    js_name,
    js_regex_src,
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
    _REGEX_FLAGS,
    _regex_literal_must_contain_the_whole_pattern,
    _regex_must_parse_in_javascript,
    _REGEX_PARSE_CACHE,
    _reset_captured_operation_id,
    step_to_postman,
    _strip_js_comments,
    _VAR_REF_RE,
    _wrap_own_fresh_reads,
)


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
    # internal=True — запрос идёт на cluster-internal REST listener ({{internalBaseUrl}},
    # :8081), где живут Internal*Service admin-мутации (ban #6: на публичном
    # {{baseUrl}} их нет by design). Форма перенята у geo-suite, которая этим
    # листенером сеет свой каталог. Нужна storage-кейсам, чьё предусловие — ВТОРОЙ
    # регион: базовый стенд несёт ровно один, а региональную когерентность нечем
    # проверить, пока сравнивать не с чем.
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
    # Default auth: jwtBootstrap (cluster system_admin) — mirrors the compute suite.
    # storage.* permissions are not all covered by the project `edit` role (storage
    # is a newer domain), so a project-editor default would 403 on volume/snapshot
    # create; system_admin holds every permission cluster-wide (and is granted
    # project-editor in the seed for LIST visibility). Parity with vpc/iam/compute
    # (the storage gen.py was missing default-auth entirely → no-auth steps sent NO
    # Authorization header → 401). Per-step auth= overrides via the item-level
    # pre-request (runs after this collection-level one); 'anonymous' removes it.
    "const __defaultJwt = pm.environment.get('jwtBootstrap') || pm.variables.get('jwtBootstrap') || '';",
    "if (__defaultJwt && !pm.request.headers.has('Authorization')) {",
    "  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __defaultJwt});",
    "}",
    *_URL_VAR_GUARD,
]


# ---------------------------------------------------------------------------
# Утилиты-сниппеты pm.*
# ---------------------------------------------------------------------------

def assert_unscoped_rejected() -> List[str]:
    """Unscoped create/list (без projectId) — ОТВЕРГНУТ. Два защитимых исхода, оба =
    «отклонено» (defense-in-depth, security.md «authz-first», parity с compute 446e25b
    / vpc):
      403 PERMISSION_DENIED (code 7) — project-scope authz fail-closed «empty object id»
        (пустой projectId → нет scope-объекта для anti-BOLA-проверки) ДО backend-валидации;
      400 INVALID_ARGUMENT  (code 3) — backend «project_id required» при passthrough.
    Толерантен к обоим — семантика негатива (rejected) сохранена, без ложного провала на
    корректном authz-first 403. До #62-фикса storage гейтил на cluster-синглтоне (никогда
    empty) → backend 400; теперь project-scoped (как compute/vpc) → empty projectId = 403.
    Techniques: ECP (класс «unscoped запрос») + error-guessing (authz-vs-validation ordering)."""
    return [
        "pm.test('unscoped rejected (400 InvalidArgument or 403 authz-first)', () => {",
        "  pm.expect(pm.response.code, JSON.stringify(pm.response.json())).to.be.oneOf([400, 403]);",
        "});",
        "pm.test('grpc code 3 (INVALID_ARGUMENT) or 7 (PERMISSION_DENIED)', () => {",
        "  const j = pm.response.json();",
        "  pm.expect(j.code, JSON.stringify(j)).to.be.oneOf([3, 7]);",
        "});",
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
        "  pm.expect(j.id, 'operation.id').to.match(/^sop[a-z0-9]+$/);",
        "  pm.expect(j.metadata, 'operation.metadata').to.be.an('object');",
        "});",
    ]




# Окно видимости прав — РЕШЕНИЕ НАБОРА, а не общего слоя (#1379): путь
# материализации у доменов разный, и одно число за всех было бы решением
# за них. Здесь — storage материализует так же, как iam. Величина видна
# на связывании, а не в прозе шапки: три копии из шести называли ЧУЖУЮ.
# Голова полосы — общая (#1477). Шаг, чей адрес собран из НЕЗАХВАЧЕННОЙ переменной,
# спрашивает не о ресурсе: окно видимости прав такой адрес не наполнит никогда, а
# отказ по нему приходит кодом ИЗ полосы ожидания — то есть шаг выжигает весь бюджет
# и падает, называя следствие вместо предмета.
_rya = functools.partial(retry_until_authorized,
                        budget=15, interval_ms=400, lane_head=True)
_RDY_SEQ = [0]

# Состояния, из которых ресурс ЕЩЁ может стать пригодным. Всё остальное —
# терминально, и ждать его бессмысленно: `ERROR` не рассосётся сам, а
# `DELETING` ведёт в противоположную сторону.
_PENDING_STATUSES = ("CREATING", "MIGRATING", "STATUS_UNSPECIFIED", "")


def wait_until_ready(step: Step, ready: str, subject: str,
                     budget: int = 60, interval_ms: int = 500) -> Step:
    """Обернуть первое обращение к своему свежему ресурсу ожиданием ПРИГОДНОСТИ.

    `Operation.done` означает «намерение закоммичено», и ТОЛЬКО это: строка
    ресурса записана, а объекта у плоскости данных ещё нет. Пригодным ресурс
    объявляет СВЕРЩИК, увидев объект, — то есть между завершением операции и
    готовностью лежит окно, длина которого задана периодом обхода сверщика, а не
    нашим ожиданием. Кейс, читающий состояние сразу после `done`, утверждает не о
    контракте, а о том, успел ли обход; и он же — единственный, кто мог бы
    заметить, что сверщик не работает вовсе.

    Поэтому здесь ОЖИДАНИЕ, а не утверждение сразу, и оно ограничено: бюджет
    израсходован → шаг ПАДАЕТ, назвав наблюдённое состояние и причину
    (`statusReason`). Так «плоскости данных на стенде нет» читается прямо из
    отчёта, а не выглядит загадочным отказом соседнего шага три запроса спустя.

    Терминальное состояние НЕ пережидается: `ERROR` роняет шаг сразу — ждать его
    исправления нечего, а бюджет, потраченный на заведомо конечный исход, лишь
    отодвинул бы находку.

    Окно видимости owner-tuple (403/404 на первом обращении к своему свежему
    ресурсу) поглощается здесь же — отдельная обёртка `retry_until_authorized`
    поверх не нужна и вредна: две петли на одном шаге делили бы один счётчик.
    """
    if ready in _PENDING_STATUSES:
        raise ValueError(f"wait_until_ready: {ready!r} — состояние ожидания, а не готовности")
    pending = ",".join(f"'{s}'" for s in _PENDING_STATUSES)
    guard = [
        "// ПРИГОДНОСТЬ ПРОИЗВОДИТ СВЕРЩИК, А НЕ ОПЕРАЦИЯ: Operation.done = «намерение",
        "// закоммичено», объекта у плоскости данных может ещё не быть. Ожидание",
        "// ограничено: бюджет израсходован — шаг падает, назвав состояние и причину.",
        "if (pm.environment.get('_readyWaitFor') !== pm.info.requestName) {",
        "  pm.environment.set('_readyWaitCount', '0');",
        "  pm.environment.set('_readyWaitFor', pm.info.requestName);",
        "}",
        "const _rwc = parseInt(pm.environment.get('_readyWaitCount') || '0', 10);",
        "let _rwj; try { _rwj = pm.response.json(); } catch (e) { _rwj = {}; }",
        "const _rwOk = pm.response.code === 200;",
        "const _rwStatus = _rwOk ? String(_rwj.status === undefined ? '' : _rwj.status) : '';",
        "const _rwReason = _rwOk ? String(_rwj.statusReason === undefined ? '' : _rwj.statusReason) : '';",
        f"const _rwPending = (pm.response.code === 403 || pm.response.code === 404) || (_rwOk && [{pending}].includes(_rwStatus));",
        f"if (_rwPending && _rwc < {budget}) {{",
        "  pm.environment.set('_readyWaitCount', String(_rwc + 1));",
        f"  const _rwd = Date.now(); while (Date.now() - _rwd < {interval_ms}) {{ /* обход сверщика */ }}",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        "pm.environment.unset('_readyWaitCount');",
        "pm.environment.unset('_readyWaitFor');",
        f"pm.test({js_str(f'{subject} стал пригоден: status {ready} (объявляет сверщик, не Operation.done)')}, () => {{",
        "  pm.expect(pm.response.code, pm.response.text()).to.eql(200);",
        f"  pm.expect(_rwStatus, 'наблюдено status=' + _rwStatus + ', statusReason=' + _rwReason +",
        f"    '; за {budget * interval_ms}ms ресурс так и не стал пригоден — либо сверщик не"
        " довёл объект, либо плоскости данных на этом стенде нет').to.eql('" + ready + "');",
        "});",
    ]
    _RDY_SEQ[0] += 1
    # Имя делается глобально уникальным по той же причине, что у
    # `retry_until_authorized`: newman резолвит setNextRequest в ПЕРВЫЙ item с
    # этим именем, и повтор имени увёл бы ожидание на чужой шаг.
    return replace(step, name=f"{step.name}-rdy{_RDY_SEQ[0]}",
                   test_script=guard + list(step.test_script))


def wait_until_ready_step(name: str, path: str, ready: str, subject: str,
                          auth: Optional[str] = None) -> Step:
    """Ожидание пригодности отдельным шагом — для ФИКСТУР, у которых своего
    чтения нет вовсе.

    Фикстура, создающая источник (том под снимок, снимок под образ), обязана
    дождаться его пригодности: снимок снимается только с готового тома, образ
    захватывается только с готового источника. Без ожидания предмет кейса
    отвергается предусловием, а падает шаг, сделавший ровно то, что положено при
    неготовом источнике, — и виновником называется невиновный.
    """
    return wait_until_ready(
        Step(name=name, method="GET", path=path, auth=auth), ready=ready, subject=subject)




def assert_op_error_oneof(codes: List[int], code_names: str,
    # ВЫЗЫВАЮЩИЙ У НЕЁ ЕСТЬ, И ОН МЕЖНАБОРНЫЙ (#1478). Ни один модуль кейсов
    # storage её не зовёт, и перепись «объявление без вызывающего» назвала её
    # мёртвой — но её зовёт проба стойкости сериализатора, живущая в наборе iam
    # и обходящая генераторы ВСЕХ наборов. Снятие уронило пробу: разбор не знал
    # этой формы вызывающего. Оставлена намеренно; форма учтена переписью.
                          msg_substr: Optional[str] = None) -> Step:
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
    return Step(name="assert-op-error", method="GET", path="/operations/{{opId}}", test_script=body)


def assert_op_error(code: int, code_name: str, msg_substr: Optional[str] = None,
                    msg_regex: Optional[str] = None,
                    msg_text: Optional[str] = None,
                    msg_text_contains: Optional[str] = None) -> Step:
    """Поллит /operations/{opId} и проверяет, что operation завершилась с error.code == code."""
    body = [
        "const j = pm.response.json();",
        "pm.test('operation done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
        f"pm.test({js_str(f'error code {code} ({code_name})')}, () => pm.expect(j.error && j.error.code, JSON.stringify(j)).to.eql({code}));",
    ]
    if msg_substr is not None:
        body.append(f"pm.test({js_str(f'error text includes \"{msg_substr}\"')}, () => pm.expect((j.error && j.error.message || '').toLowerCase()).to.include({js_str(msg_substr.lower())}));")
    if msg_regex is not None:
        body.append(f"pm.test({js_str(f'error text matches /{msg_regex}/')}, () => pm.expect(j.error && j.error.message || '').to.match(/{js_regex_src(msg_regex, where='storage/assert_op_error/msg_regex')}/));")
    if msg_text is not None:
        body.extend(assert_op_refusal_message(msg_text))
    if msg_text_contains is not None:
        body.extend(assert_op_refusal_message_contains(msg_text_contains))
    return Step(name="assert-op-error", method="GET", path="/operations/{{opId}}", test_script=body)


def assert_op_success(auth: Optional[str] = None) -> Step:
    # `auth`: single-GET op read; must run under the op's CREATOR (creator-only
    # OperationService.Get, анти-BOLA) when the create ran under an auth= override.
    return Step(name="assert-op-success", method="GET", path="/operations/{{opId}}", auth=auth,
                test_script=[
                    "const j = pm.response.json();",
                    "pm.test('operation done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
                    "pm.test('operation succeeded (response, no error)', () => pm.expect(Boolean(j.response) && !j.error, JSON.stringify(j)).to.eql(true));",
                ])


# ---------------------------------------------------------------------------
# Переиспользуемые блоки кейсов (compute-specific, generic)
# ---------------------------------------------------------------------------

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
    _RDY_SEQ[0] = 0
    _RYA_SEQ[0] = 0
    gen_shared._POLL_SEQ[0] = 0
# ─────────────────────────────────────────────────────────────────────────────
# РЕШЕНИЯ НАБОРА, от которых зависит форма коллекции (#1379). Форму собирает
# общий слой; здесь объявлено ТОЛЬКО то, чем этот набор от остальных отличается.
# ─────────────────────────────────────────────────────────────────────────────

def _case_steps(case):
    """Конвейер шагов кейса: обёртка первого доступа к своему свежему ресурсу,
    затем утверждения об исходе удаления, сброс захваченного идентификатора
    операции и утверждение об исходе публикации идентификатора.

    Порядок значим и не переставляется: обёртка возвращает управление из скрипта,
    пока ждёт окна видимости, поэтому утверждения ставятся ПОСЛЕ неё.
    """
    return _assert_published_id_outcome(
        _reset_captured_operation_id(_assert_delete_operation_outcome(
            _wrap_own_fresh_reads(case.steps, _rya))))


# Опрос операции: тело общее (#1475), решения набора — здесь. Величины те же,
# что были в копии storage; актор опроса приходит вызовом.
poll_operation_until_done = functools.partial(
    gen_shared.op_poll_step, Step, budget=30, interval_ms=500)

_EMIT = Emit(
    id_slug="kacho-storage",
    display_name="kacho-storage / newman",
    pre_global=lambda key: PRE_GLOBAL,
    steps_of=_case_steps,
    auth_pre=_auth_pre_script,
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
    "assert_unscoped_rejected": assert_unscoped_rejected,
    "assert_field_violation": assert_field_violation,
    "save_from_response": save_from_response,
    "assert_operation_envelope": assert_operation_envelope,
    "assert_created_at_seconds": assert_created_at_seconds,
    "poll_operation_until_done": poll_operation_until_done,
    "retry_until_authorized": _rya,
    "wait_until_ready": wait_until_ready,
    "wait_until_ready_step": wait_until_ready_step,
    "assert_op_error": assert_op_error,
    "assert_op_error_oneof": assert_op_error_oneof,
    "assert_op_success": assert_op_success,
    "js_regex_src": js_regex_src,
    "js_name": js_name,
}


_RUN = Run(
    root=ROOT,
    cases_dir=CASES_DIR,
    out_dir=OUT_DIR,
    scripts_dir=Path(__file__).resolve().parent,
    emit=_EMIT,
    case_cls=Case,
    injected=_INJECTED,
    before=_reset_step_name_counters,
    stem_dashes_to_underscores=True,
    per_collection=None,
    after_all=None,
)

# Точка входа — связывание, а не своё тело (#1474). Оркестрация одна на дерево;
# здесь набор связывает СВОИ решения. Имя `main` сохранено: его импортирует
# тонкая обёртка края (`from gen import main`).
main = functools.partial(generate, _RUN)


if __name__ == "__main__":
    sys.exit(main(sys.argv))
