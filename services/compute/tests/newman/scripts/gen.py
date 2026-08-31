#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""
tests/newman/scripts/gen.py — генератор Postman collections из декларативных case-файлов.

Использование:
    python3 scripts/gen.py             # все ресурсы
    python3 scripts/gen.py operation   # один ресурс

Источник истины — модули в tests/newman/cases/<resource>.py, каждый экспортирует
переменную CASES — список объектов Case (см. ниже).

Специфика compute: REST-префикс `/compute/v1/`, операции — `/operations/{id}`
(общий OpsProxy api-gateway, prefix `epd`), env-var `garbageComputeId`.
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
    _RYA_SEQ,
    _accepted_http_codes,
    assert_created_at_seconds,
    _assert_delete_operation_outcome,
    assert_field_violation,
    assert_grpc_code,
    assert_op_refusal_message,
    assert_op_refusal_message_contains,
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
    _is_operation_id_var,
    _js_code_and_literals,
    js_comment,
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


# Окно видимости прав — РЕШЕНИЕ НАБОРА, а не общего слоя (#1379): путь
# материализации у доменов разный, и одно число за всех было бы решением
# за них. Здесь — путь материализации compute. Величина видна
# на связывании, а не в прозе шапки: три копии из шести называли ЧУЖУЮ.
# Голова полосы — общая (#1477). Шаг, чей адрес собран из НЕЗАХВАЧЕННОЙ переменной,
# спрашивает не о ресурсе: окно видимости прав такой адрес не наполнит никогда, а
# отказ по нему приходит кодом ИЗ полосы ожидания — то есть шаг выжигает весь бюджет
# и падает, называя следствие вместо предмета.
_rya = functools.partial(retry_until_authorized,
                        budget=40, interval_ms=600, lane_head=True)
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


def assert_op_error(code: int, code_name: str, msg_substr: Optional[str] = None,
                    msg_regex: Optional[str] = None, auth: Optional[str] = None,
                    msg_text: Optional[str] = None,
                    msg_text_contains: Optional[str] = None) -> Step:
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
    if msg_text is not None:
        body.extend(assert_op_refusal_message(msg_text))
    if msg_text_contains is not None:
        body.extend(assert_op_refusal_message_contains(msg_text_contains))
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
    _ABS_SEQ[0] = 0
    gen_shared._POLL_SEQ[0] = 0
    _RYA_SEQ[0] = 0


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



# Перепись разбора переходов — решение compute (#1474). Накапливается по всему
# набору и печатается ОДИН раз: своя копия оркестрации печатала её на каждой
# коллекции строкой рядом с именем файла.
_JUMP_CENSUS = {"items": 0, "loops": 0}


def _audit_jumps_before_write(res: str, col: Dict) -> List[str]:
    """Разбор переходов ДО записи коллекции — решение compute (#1474).

    Возвращает находки в форме, которую ждёт общая оркестрация. Коллекция с
    негодным переходом на диск не попадает: прогон по ней МОЛЧА пропустил бы
    запросы, а «не выполнилось» не вычитается из вердикта и не зачитывается в
    успех.
    """
    findings, n_items, n_loops = audit_jumps(col)
    _JUMP_CENSUS["items"] += n_items
    _JUMP_CENSUS["loops"] += n_loops
    return [f"  - {f}" for f in findings]


def _report_jump_census(_out_dir: Path) -> int:
    """Объём осмотренного разбором переходов — печатается ВСЕГДА.

    Без него «негодных переходов не найдено» неотличимо от «разбор перестал
    узнавать переход»: предикат, потерявший предмет, молча стал бы вечнозелёным.
    Ноль осмотренных шагов — отказ, а не пустая работа.
    """
    print("[gen] разбор переходов: шагов %d, петель повтора %d — все состоятельны"
          % (_JUMP_CENSUS["items"], _JUMP_CENSUS["loops"]))
    if _JUMP_CENSUS["items"] == 0:
        sys.stderr.write("gen: FAIL — разбор переходов не узнал ни одного шага; "
                         "перепись беспредметна\n")
        return 1
    return 0
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


# Опрос операции: тело общее (#1475), решения набора — здесь. Величина бюджета и
# паузы у compute те же, что были в его копии; актор опроса приходит вызовом.
poll_operation_until_done = functools.partial(
    gen_shared.op_poll_step, Step, budget=30, interval_ms=500)

_EMIT = Emit(
    id_slug="kacho-compute",
    display_name="kacho-compute / newman",
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
    "assert_refusal_message": assert_refusal_message,
    "assert_refusal_message_contains": assert_refusal_message_contains,
    "assert_field_violation": assert_field_violation,
    "save_from_response": save_from_response,
    "assert_operation_envelope": assert_operation_envelope,
    "assert_created_at_seconds": assert_created_at_seconds,
    "poll_operation_until_done": poll_operation_until_done,
    "retry_until_authorized": _rya,
    "retry_until_present": retry_until_present,
    "retry_until_absent": retry_until_absent,
    "assert_op_error": assert_op_error,
    "assert_op_error_oneof": assert_op_error_oneof,
    "assert_op_success": assert_op_success,
    "list_page_block": list_page_block,
    "ADMIN_AUTH": ADMIN_AUTH,
    "MT_INTERNAL_PATH": MT_INTERNAL_PATH,
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
    per_collection=_audit_jumps_before_write,
    after_all=_report_jump_census,
)

# Точка входа — связывание, а не своё тело (#1474). Оркестрация одна на дерево;
# здесь набор связывает СВОИ решения. Имя `main` сохранено: его импортирует
# тонкая обёртка края (`from gen import main`).
main = functools.partial(generate, _RUN)


if __name__ == "__main__":
    sys.exit(main(sys.argv))
