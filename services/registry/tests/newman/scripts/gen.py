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

# Monotonic counter for retry_until_authorized / retry_until_present wrapped steps.
# Each wrapped step gets a globally-unique `-rya<n>`/`-lst<n>` suffix so its
# setNextRequest(pm.info.requestName) self-retry always resolves to ITSELF (newman
# resolves a name to the FIRST item bearing it) — same hazard poll_operation_until_done
# avoids via unique poll-op-<n>. NOT reset per module: global uniqueness is the goal.
_RYA_SEQ = [0]


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
    "const __defaultJwt = pm.environment.get('jwtProjectEditorA') || pm.variables.get('jwtProjectEditorA') || '';",
    "if (__defaultJwt && !pm.request.headers.has('Authorization')) {",
    "  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __defaultJwt});",
    "}",
    # REQUIRED-FIXTURE GUARD. Идентификаторы проекта/региона и токен по умолчанию
    # приходят из посева фикстур (tests/authz-fixtures → patch-env.py); в
    # закоммиченных окружениях они пусты by design. Пустое значение НЕ даёт пропуска
    # — оно уезжает в URL как `projectId=`, и запрос отвергается на авторизации до
    # всякой проверки предмета: суита краснеет каскадом, а причина («посев не
    # выполнялся») в отчёте не названа НИГДЕ. Поэтому отсутствие фикстуры — отказ,
    # названный по имени, на ПЕРВОМ же шаге прогона.
    "const __needed = ['baseUrl', 'existingProjectId', 'existingRegionId', 'jwtProjectEditorA'];",
    "const __missing = __needed.filter((k) => !(pm.environment.get(k) || pm.variables.get(k) || ''));",
    "if (__missing.length > 0) {",
    "  pm.test('FIXTURE REQUIRED: ' + __missing.join(', '), () => pm.expect.fail('the suite fixture "
    "seed did not run: ' + __missing.join(', ') + ' are empty. Seed via tests/authz-fixtures "
    "(prodrun.sh patches the suite env). An empty id is not a skip — it is sent as-is and the "
    "request is refused on authorization, so every later failure names the wrong cause.'));",
    # ...ЗАТЕМ ПРОПУСТИТЬ. Санкционированная форма — утвердить (назвав переменную) и
    # НЕ отправлять; она объявлена в exec-coverage.py (раздел STATIC BANS), эталон —
    # iam gen.py::require_env_url. Этот скрипт — pre-request КОРНЯ коллекции, то есть
    # исполняется перед КАЖДЫМ запросом: без пропуска весь набор уезжал бы с пустым
    # идентификатором, о котором в тексте отказа выше сказано прямо — «отправляется
    # как есть, и каждая последующая ошибка называет неверную причину». Утверждение
    # уже отработало, поэтому пропуск остаётся ЗАПИСАННЫМ падением с именем
    # переменной, а не немым.
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


def save_from_response(jsonpath: str, env_var: str) -> List[str]:
    return [
        "try {",
        "  const j = pm.response.json();",
        f"  const v = ({jsonpath});",
        f"  if (v !== undefined && v !== null) pm.environment.set('{env_var}', String(v));",
        "} catch (e) {}",
    ]


def save_operation_id() -> List[str]:
    """Capture THIS response's Operation id into `opId`, clearing any previous one first.

    STALE-opId GUARD. `save_from_response` writes only when the field is present, so a
    step that fails (404/403/400 — no `id` in the body) silently leaves the PREVIOUS
    step's `opId` in the environment. The `poll_operation_until_done()` that follows then
    polls an unrelated, already-done Operation and the capture step asserts against
    ANOTHER resource's payload — reporting a second, misleading failure that buries the
    real one.

    Observed in CI run 30135586348, REG-RD-F5-INHERIT-PRIVATE: `repo-create` 404'd for
    its whole retry budget, after which `poll-op-5` cheerfully polled the Operation left
    over from `delete-rnRegId` and `repo-capture` reported
    "new repo inherits visibility PRIVATE: expected undefined to deeply equal 'PRIVATE'"
    — a symptom with nothing to do with the cause.

    Every call site that captures an operation id means "the operation THIS response
    minted", so clearing first is strictly correct: no case legitimately wants to poll a
    previous step's operation.
    """
    return ["pm.environment.unset('opId');", *save_from_response("j.id", "opId")]


def assert_operation_envelope(prefix_regex: str = "^(nlb|tgr|lst)[a-z0-9]+$") -> List[str]:
    return [
        "pm.test('Operation envelope returned', () => {",
        "  const j = pm.response.json();",
        f"  pm.expect(j.id, 'operation.id').to.match(/{prefix_regex}/);",
        "  pm.expect(j.metadata, 'operation.metadata').to.be.an('object');",
        "});",
    ]


def poll_operation_until_done() -> Step:
    """Reusable poll step with up-to-30 setNextRequest retries spaced ~500ms apart;
    guards on empty opId. Budget*interval ≈ 15s covers the async-op tail instead of
    hammering back-to-back (~15ms/poll) which never waits for the op (Koren #1).

    Each emitted step carries a unique name (`poll-op-<n>`) so the
    setNextRequest self-retry is unambiguous under `newman run <collection>`
    (see `_poll_seq` note): a duplicate "poll-op" name would make newman resolve
    the retry jump to the first such step and skip intervening folders."""
    global _poll_seq
    _poll_seq += 1
    return Step(
        name=f"poll-op-{_poll_seq}",
        method="GET",
        path="/operations/{{opId}}",
        test_script=[
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
            "if (!j.done && pc < 60) {",
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


def retry_until_authorized(step: Step, budget: int = 80, interval_ms: int = 600,
                           retry_on=(403, 404)) -> Step:
    """Wrap the FIRST access of the caller's OWN just-created resource in a bounded
    read-your-writes retry over the owner-tuple materialization window.

    Kachō is eventually-consistent (api-conventions.md Operation.done = DURABLE, not
    downstream side-effect visibility). A registry/repository owner-tuple materialises
    via register-outbox → drainer → IAM RegisterResource → FGA reconciler (registry
    `internal/clients/iam/register_applier.go`). Until it is visible, the FIRST
    post-create Get/Update/Delete/Rename of the fresh resource can briefly return 403
    (PERMISSION_DENIED) or 404 (existence-hiding deny) at the per-repo v_* Check /
    gateway scope gate — a textbook read-your-writes lag; the CLIENT retries, it is
    NOT a server barrier.

    Retries the SAME request (setNextRequest -> self) while the response code is in
    `retry_on` (default 403/404), spacing attempts by ~interval_ms (busy-wait — newman
    fires setNextRequest before any setTimeout). budget*interval_ms bounds the wait
    (default 25*500ms ≈ 12s) — fail-closed: on any other code the wrapped step's real
    test_script runs exactly once, and once the budget is spent it ALSO runs on the
    terminal 403/404 (a genuine, non-converging deny still FAILS the real assertions —
    never masked, never infinite).

    Use ONLY on the first access of the caller's OWN fresh resource. Do NOT wrap
    negative / cross-account-deny / absent-id steps (a poll there would mask a real
    deny). The counter/started env-vars are request-name-scoped (step names are
    globally unique after serialization) so the loop never bleeds across cases/steps.
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
        "// (eventual-consistency); retries SELF only on 403/404 of own fresh resource.",
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
    return replace(step, name=f"{step.name}-rya{_RYA_SEQ[0]}",
                   test_script=guard + list(step.test_script))


def retry_until_present(step: Step, id_env_var: str, budget: int = 40,
                        interval_ms: int = 600) -> Step:
    """Bounded retry a LIST step until the caller's OWN fresh resource id appears in
    the returned array (read-your-writes over the list-authz visibility window —
    owner-tuple eventual-consistency). The list returns 200 with the id ABSENT until
    the tuple materialises, so retry_until_authorized (403/404) does not apply — we
    retry while the id is missing. Fail-open after budget: the real assertion then runs
    once and FAILS if still absent (never masked, never infinite). Use ONLY on a list
    of the caller's OWN just-created resource."""
    guard = [
        "// bounded read-your-writes retry until own fresh id is present in the list",
        "// (eventual-consistency); retries SELF while id absent.",
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
            if _VAR_REF_RE.findall(st.path) and (set(_VAR_REF_RE.findall(st.path)) & fresh):
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
        "item": [step_to_postman(s) for s in _wrap_own_fresh_reads(case.steps)],
    }


def build_collection(service: str, cases: List[Case]) -> Dict:
    return {
        "info": {
            # Deterministic _postman_id (UUIDv5 over the collection name) so a
            # regeneration with no source change produces no diff. A random id
            # here made every regeneration dirty every collection, which meant
            # "generated matches source" could never be checked and a real drift
            # had nowhere to show. Postman only needs this to be stable+unique.
            "_postman_id": str(uuid.uuid5(uuid.NAMESPACE_URL, f"kacho-registry/newman/{service}")),
            "name": f"kacho-registry / newman / {service}",
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
    mod.assert_field_violation = assert_field_violation
    mod.assert_operation_envelope = assert_operation_envelope
    mod.save_from_response = save_from_response
    mod.save_operation_id = save_operation_id
    mod.poll_operation_until_done = poll_operation_until_done
    mod.retry_until_authorized = retry_until_authorized
    mod.retry_until_present = retry_until_present
    mod.http_method_not_allowed_block = http_method_not_allowed_block
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
