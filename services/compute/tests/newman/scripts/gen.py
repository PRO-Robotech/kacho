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
import sys
import uuid
import importlib.util
from pathlib import Path
from dataclasses import dataclass, field, replace
from typing import List, Dict, Optional

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
# Глобальный prerequest (runId генерация + _suiteFolder* алиасы)
# ---------------------------------------------------------------------------

PRE_GLOBAL = [
    "if (!pm.environment.get('runId') || pm.environment.get('runId') === '') {",
    "  // runId формат: только [a-z0-9], без точки, начинается с буквы — чтобы проходить compute name regex",
    "  const t = Date.now().toString(36);",
    "  const r = Math.floor(Math.random() * 1e9).toString(36);",
    "  pm.environment.set('runId', ('r' + t + r).replace(/[^a-z0-9]/g, '').slice(0, 11));",
    "}",
    "pm.environment.set('_suiteFolderId', pm.environment.get('existingProjectId'));",
    "pm.environment.set('_suiteFolderCrossId', pm.environment.get('existingProjectCrossId'));",
    "// internalBaseUrl fallback: CI-драйвер (newman-e2e.sh) прокидывает --env-var,",
    "// но для standalone-прогона деривируем cluster-internal listener из baseUrl",
    "// (публичный :8080/:18080 → internal-rest :8081/:18081). Internal*-шаги",
    "// (InternalMachineTypeService seed, COMP-1 F7) идут на {{internalBaseUrl}}.",
    "if (!pm.environment.get('internalBaseUrl') || pm.environment.get('internalBaseUrl') === '') {",
    "  const __b = pm.environment.get('baseUrl') || 'http://localhost:18080';",
    "  pm.environment.set('internalBaseUrl', __b.replace(/:(1?)8080(\\b|$)/, ':$18081'));",
    "}",
    "// Дефолтный Bearer (bootstrap cluster-admin) для шагов с auth=None: без него все",
    "// запросы анонимны → IAM authn-gate 401 fail-closed. Per-step auth ('anonymous'",
    "// снимает, '<envVar>' переопределяет) идёт в item-pre-request ПОСЛЕ collection-",
    "// pre-request, поэтому этот дефолт им не мешает (e2e-newman fullscope root A1).",
    "const __defAuth = pm.environment.get('jwtBootstrap');",
    "if (__defAuth) { pm.request.headers.upsert({ key: 'Authorization', value: 'Bearer ' + __defAuth }); }",
]


# ---------------------------------------------------------------------------
# Утилиты-сниппеты pm.*
# ---------------------------------------------------------------------------

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
        f"pm.test('field violation on \"{field_name}\"', () => {{",
        "  const j = pm.response.json();",
        "  const det = (j.details || []).find(d => (d['@type']||'').includes('BadRequest'));",
        "  pm.expect(det, 'BadRequest detail').to.be.an('object');",
        f"  const fv = (det.fieldViolations || []).find(v => v.field === '{field_name}');",
        f"  pm.expect(fv, 'fieldViolation for {field_name}').to.be.an('object');",
        "});",
    ]


def save_from_response(jsonpath: str, env_var: str) -> List[str]:
    """Сохранить значение из response в env."""
    return [
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
    """
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
    gone — NEVER to paper over a cross-account deny or a real hole. The step name is preserved
    (self-loop uses the dynamic pm.info.requestName)."""
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
    return replace(step, test_script=guard + list(step.test_script))


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
        f"pm.test('error code {code} ({code_name})', () => pm.expect(j.error && j.error.code, JSON.stringify(j)).to.eql({code}));",
    ]
    if msg_substr is not None:
        body.append(f"pm.test('error text includes \"{msg_substr}\"', () => pm.expect((j.error && j.error.message || '').toLowerCase()).to.include('{msg_substr.lower()}'));")
    if msg_regex is not None:
        body.append(f"pm.test('error text matches /{msg_regex}/', () => pm.expect(j.error && j.error.message || '').to.match(/{msg_regex}/));")
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
        f"pm.test('error code in {codes_js} ({code_names})', () => pm.expect(j.error && j.error.code, JSON.stringify(j)).to.be.oneOf({codes_js}));",
    ]
    if msg_substr is not None:
        body.append(f"pm.test('error text includes \"{msg_substr}\"', () => pm.expect((j.error && j.error.message || '').toLowerCase()).to.include('{msg_substr.lower()}'));")
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

def list_page_block(prefix, list_path, folder_param=True):
    """BVA для List RPC: page_size 0 / 1 / 1000 / 1001 / garbage token.

    folder_param=True — list_path требует ?projectId=... (Disk/Image/Snapshot/Instance);
    folder_param=False — справочники (DiskType/Zone) — без projectId.
    """
    base = f"{list_path}?projectId={{{{_suiteFolderId}}}}&" if folder_param else f"{list_path}?"
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
    """ECP/BVA по полю name для compute (lowercase-only regex `|[a-z]([-_a-z0-9]{0,61}[a-z0-9])?`):
      - empty name → 200 (proto pattern допускает пустую строку)
      - len=63 (max) → 200
      - len=64 (over) → 400
      - UPPERCASE → 400  (compute lowercase-only — НЕ как VPC)
      - начинается с цифры → 400
      - начинается с дефиса → 400
      - спец-символы → 400

    body_extra — обязательные поля кроме projectId/name.
    wrap(case) — опциональный декоратор (для Image/Snapshot/Instance которым нужен pre-disk и т.п.);
                 если задан — name-кейсы которые ожидают 200 оборачиваются (нужен реальный ресурс),
                 остальные (400) — нет (отказ синхронный, до создания зависимостей).
    """
    body_extra = body_extra or {}
    wrap = wrap or (lambda c: c)
    base = lambda name: {"projectId": "{{_suiteFolderId}}", "name": name, **body_extra}
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
    out.append(Case(id=f"{prefix}-CR-VAL-NAME-DIGIT-START",
        title="Create с name начинающимся с цифры → 400 (verbatim YC regex)",
        classes=["VAL"], priority="P1",
        steps=[Step(name="cr-digit", method="POST", path=create_path, body=base("9invalid-{{runId}}"),
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
    base = lambda name, labels: {"projectId": "{{_suiteFolderId}}", "name": name, "labels": labels, **body_extra}
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
    base = lambda name, desc: {"projectId": "{{_suiteFolderId}}", "name": name, "description": desc, **body_extra}
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
    """Filter syntax: name="X" → 200; garbage → 200|400; unknown field → 200|400."""
    sep = "&"
    return [
        Case(id=f"{prefix}-LST-FILTER-NAME-OK",
             title="List с filter name=\"foo\" → 200",
             classes=["FILTER", "CRUD"], priority="P2",
             steps=[Step(name="flt-ok", method="GET",
                         path=f"{list_path}?projectId={{{{_suiteFolderId}}}}{sep}filter=name%3D%22foo%22",
                         test_script=[*assert_status(200)])]),
        Case(id=f"{prefix}-LST-FILTER-GARBAGE",
             title="List с garbage filter syntax → 200 или 400",
             classes=["FILTER", "VAL"], priority="P2",
             steps=[Step(name="flt-bad", method="GET",
                         path=f"{list_path}?projectId={{{{_suiteFolderId}}}}{sep}filter=this%20is%20not%20valid%20syntax",
                         test_script=["pm.test('200 or 400', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));"])]),
        Case(id=f"{prefix}-LST-FILTER-UNKNOWN-FIELD",
             title="List с filter на unsupported field → 200 или 400",
             classes=["FILTER", "VAL"], priority="P2",
             steps=[Step(name="flt-unk", method="GET",
                         path=f"{list_path}?projectId={{{{_suiteFolderId}}}}{sep}filter=nonexistent_field%3D%22x%22",
                         test_script=["pm.test('200 or 400', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));"])]),
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
                        body={"projectId": "{{_suiteFolderId}}", "name": payload[:200], **body_extra},
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
                    path=f"{list_path}?projectId={{{{_suiteFolderId}}}}&filter=name%3D%22a%27%20OR%201%3D1--%22",
                    test_script=["pm.test('not 500', () => pm.expect(pm.response.code).to.not.eql(500));",
                                 "pm.test('handled', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));"])]))
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
             steps=[Step(name="put-list", method="PUT", path=base_path, body={"projectId": "{{_suiteFolderId}}"},
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
        f"// KAC-122 authz-deny: bearer from env '{auth}'",
        f"const __t = pm.environment.get('{auth}') || pm.variables.get('{auth}') || '';",
        "if (__t) {",
        "  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __t});",
        "} else {",
        "  pm.request.headers.remove('Authorization');",
        "}",
    ]


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
    if step.test_script:
        events.append({"listen": "test", "script": {"type": "text/javascript", "exec": step.test_script}})
    if events:
        item["event"] = events
    return item


def case_to_postman(case: Case) -> Dict:
    tags = [f"class:{c}" for c in case.classes] + [f"priority:{case.priority}"]
    return {
        "name": f"{case.id} — {case.title}",
        "description": " | ".join(tags),
        "item": [step_to_postman(s) for s in case.steps],
    }


def build_collection(resource: str, cases: List[Case]) -> Dict:
    return {
        "info": {
            "_postman_id": str(uuid.uuid4()),
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

def load_cases_module(path: Path):
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
    spec.loader.exec_module(mod)
    return mod


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
        out = OUT_DIR / f"{res}.postman_collection.json"
        out.write_text(json.dumps(col, indent=2, ensure_ascii=False))
        print(f"[{res}] {len(cases)} cases → {out.relative_to(ROOT)}")
    return rc


if __name__ == "__main__":
    sys.exit(main(sys.argv))
