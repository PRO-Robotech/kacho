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
# Reusable assertion snippets (pm.*) — same names as kacho-vpc
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


def save_from_response(jsonpath: str, env_var: str) -> List[str]:
    return [
        "try {",
        "  const j = pm.response.json();",
        f"  const v = ({jsonpath});",
        f"  if (v !== undefined && v !== null) pm.environment.set('{env_var}', String(v));",
        "} catch (e) {}",
    ]


def assert_operation_envelope(prefix_regex: str = "^(nlb|tgr|lst)[a-z0-9]+$") -> List[str]:
    return [
        "pm.test('Operation envelope returned', () => {",
        "  const j = pm.response.json();",
        f"  pm.expect(j.id, 'operation.id').to.match(/{prefix_regex}/);",
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

    Retries the SAME request (setNextRequest -> self) while the response is a
    `<something> not found` rejection (400/404 whose body message contains 'not found'),
    spacing attempts ~interval_ms (busy-wait -- newman fires setNextRequest before any
    setTimeout). A rejected create allocates NOTHING (sync reject before the Operation is
    even minted), so re-POSTing is leak-free and idempotent. budget*interval_ms bounds
    the wait (default 30*400ms = ~12s) -- fail-closed: on any other outcome the wrapped
    step's real test_script runs exactly once, and once the budget is spent it ALSO runs
    on the terminal not-found (a genuinely-absent peer still FAILS the real assertions --
    never masked, never infinite).

    Use ONLY on a create whose peer dependency was provisioned earlier in the SAME case.
    Do NOT wrap negative fixture-absent creates (they legitimately expect the rejection).
    """
    guard = [
        "// bounded read-your-writes retry over the cross-service peer-visibility window",
        "// (vpc subnet/address just provisioned; nlb peer-read briefly stale). Retries",
        "// SELF only while the sync create is a transient '<peer> not found' rejection.",
        "if (pm.environment.get('_crRetryStarted') !== pm.info.requestName) {",
        "  pm.environment.set('_crRetryCount', '0');",
        "  pm.environment.set('_crRetryStarted', pm.info.requestName);",
        "}",
        "const _crc = parseInt(pm.environment.get('_crRetryCount') || '0', 10);",
        "let _crNotFound = false;",
        "try { _crNotFound = [400, 404].includes(pm.response.code)"
        " && /not found/i.test(pm.response.json().message || ''); } catch (e) {}",
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
            *[f"  pm.environment.unset('{v}');" for v in ids],
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
            f"try {{ _opTransient = !!j.error && /{retry_when}/i.test(j.error.message || ''); }} catch (e) {{}}",
            f"if (_opTransient && _orc < {retry_budget}) {{",
            "  pm.environment.set('_opRedriveCount', String(_orc + 1));",
            # The create step's OWN sync-retry counter is keyed on its request name and
            # only resets when the name changes; re-entering the same step would find a
            # spent budget. Clear it so each re-drive gets a full sync-retry window.
            "  pm.environment.unset('_crRetryStarted');",
            "  pm.environment.unset('_crRetryCount');",
            "  pm.environment.unset('opId');",
            f"  const _ord = Date.now(); while (Date.now() - _ord < {retry_interval_ms}) {{ /* peer-visibility wait */ }}",
            f"  pm.execution.setNextRequest('{retry_from}');",
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
        f"// AZD per-step: bearer from env '{auth}'",
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

def load_cases_module(path: Path):
    # Reset the poll-step counter so each collection's poll-op-<n> names are
    # deterministic (stable across regenerations) rather than depending on how
    # many modules were loaded before this one.
    global _poll_seq
    _poll_seq = 0
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
    mod.retry_until_present = retry_until_present
    mod.retry_until_state = retry_until_state
    mod.retry_create_until_present = retry_create_until_present
    mod.http_method_not_allowed_block = http_method_not_allowed_block
    mod.conf_alreadyexists_block = conf_alreadyexists_block
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
