#!/usr/bin/env python3

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""tests/newman/scripts/gen.py — newman collection generator for the api-gateway suite.

Usage:
    python3 scripts/gen.py                # generate all case files
    python3 scripts/gen.py cluster_admin  # one case file

Source of truth: tests/newman/cases/<name>.py modules, each exporting a CASES
list of Case objects.

Форму коллекции и вспомогательный слой собирает ОБЩИЙ модуль
`tests/newman/kacholib/gen_shared.py` — один на дерево (#1367, #1377, #1379,
#1474). Здесь объявлено только то, чем ЭТОТ набор отличается: решения формы
(дескриптор `Emit`), решения оркестрации (дескриптор `Run`), таблица впрыска
и собственные помощники набора.

Соседний генератор образцом НЕ является и сверяться с ним не надо: расхождение
между копиями было предметом сведения, а не способом его проверить.

This suite owns the cluster-RBAC admin surface
(`InternalClusterService`), which lives on the api-gateway CLUSTER-INTERNAL REST
listener and therefore has no home in any per-service suite.

LAYOUT (changed 2026-07-26): collections are written to `collections/`, NOT next to
the case files. The shared CI gate (services/iam/tests/newman/scripts/
assert-suites-green.sh) and the execution-coverage gate both walk
`collections/*.postman_collection.json` against `out/<name>.json`. Emitting to
`cases/` put this suite outside both gates — one of the reasons it could sit
unexecuted without anything going red.

THREE HARNESS INVARIANTS THIS GENERATOR ENFORCES (all three were absent, and their
absence is exactly why a direct run produced 54 requests, zero Authorization headers,
69 failed assertions — and not one word about its own misconfiguration):

  1. A missing subject token FAILS; it does not silently drop the header. Dropping it
     turns "ordinary user must get 403" into "anonymous must get 403" — which still
     passes, for a completely different reason. See `_auth_pre_script`.
  2. A missing base-URL variable FAILS; it does not silently skip. See `require_env_url`.
  3. Every self-retry poll carries a REAL inter-poll delay. newman runs test scripts
     synchronously and honours `setNextRequest` before any `setTimeout`, so a busy-wait
     is the only thing that actually spaces the polls. See `poll_operation`.
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

from gen_shared import (  # noqa: E402  — импорт после провязки sys.path
    generate,
    Run,
    _assert_delete_operation_outcome,
    assert_grpc_code,
    _assert_published_id_outcome,
    assert_status,
    _asserts_done,
    _asserts_outcome,
    _assigns_env_var,
    build_collection,
    _carries_assertion,
    case_to_postman,
    _DELETE_ACCEPTED,
    Emit,
    _is_operation_id_var,
    _js_code_and_literals,
    js_comment,
    js_str,
    load_cases_module,
    _MUTATION_METHODS,
    _OP_POLL_PATH,
    _PUB_BIND_RE,
    _PUB_SET_RE,
    _published_id_outcome_assert,
    _published_resource_vars,
    step_to_postman,
    _strip_js_comments,
)


ROOT = Path(__file__).resolve().parents[1]
CASES_DIR = ROOT / "cases"
COLLECTIONS_DIR = ROOT / "collections"

# Poll budget for async Operations. The delay is sized FROM the cap rather than
# fixed, so raising the cap cannot silently multiply the worst-case wall-time
# (testing.md: `delay = clamp(30000/cap, 100..500)ms`).
POLL_CAP = 45
POLL_DELAY_MS = max(100, min(500, 30000 // POLL_CAP))


# ---------------------------------------------------------------------------
# Declarative structures
# ---------------------------------------------------------------------------

@dataclass
class Step:
    """One HTTP request inside a case."""
    name: str
    method: str
    path: str  # relative; the mux prefix is added automatically
    body: Optional[Dict] = None
    pre_script: List[str] = field(default_factory=list)
    test_script: List[str] = field(default_factory=list)
    # Per-step auth override.
    #   None              — Authorization header untouched
    #   "anonymous"       — Authorization header stripped (DELIBERATE anonymous case)
    #   "<envVarName>"    — Authorization: Bearer {{envVarName}}; missing value = FAIL
    auth: Optional[str] = None
    # Which api-gateway listener this step targets.
    #   "public"   — {{baseUrl}}          (:8080 cmux, the tenant-facing REST surface)
    #   "internal" — {{internalBaseUrl}}  (:8081 cluster-internal REST listener)
    #   "external" — {{externalBaseUrl}}  (the advertised public TLS endpoint)
    # Internal* RPCs are served ONLY on "internal"; "public" 404s them by design
    # (ban #6), which is what the "external" negatives assert.
    mux: str = "public"


@dataclass
class Case:
    """One test case — may contain multiple steps."""
    id: str
    title: str
    classes: List[str]
    priority: str
    steps: List[Step]


# ---------------------------------------------------------------------------
# Global prerequest: generate a runId once per collection run
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
    "  pm.environment.set('runId', ('r' + t + r).replace(/[^a-z0-9]/g, '').slice(0, 11));",
    "}",
    *_URL_VAR_GUARD,
]


# ---------------------------------------------------------------------------
# pm.* assertion helpers
# ---------------------------------------------------------------------------

def assert_error_message_eql(expected: str) -> List[str]:
    """Exact-match error message. Error text is part of the Kachō contract
    (api-conventions.md: «Тексты — часть контракта»), so this is `.to.eql`,
    never a substring match."""
    payload = json.dumps(expected)
    return [
        f"pm.test({js_str(f'error message exactly equals {payload}')}, () => {{",
        "  const j = pm.response.json();",
        f"  pm.expect(j.message, JSON.stringify(j)).to.eql({payload});",
        "});",
    ]


def save_from_response(jsonpath: str, env_var: str) -> List[str]:
    """Save a value from the response into a Postman env var.

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


def assert_iam_operation_envelope() -> List[str]:
    """IAM mutations return an Operation whose id carries the `iop` prefix."""
    return [
        "pm.test('IAM Operation envelope returned', () => {",
        "  const j = pm.response.json();",
        "  pm.expect(j.id, 'operation.id must start with iop: ' + JSON.stringify(j)).to.match(/^iop[a-z0-9]+$/);",
        "  pm.expect(j.done, 'operation.done present').to.be.a('boolean');",
        "});",
    ]


def require_env_url(var: str, path: str, why: str = "") -> List[str]:
    """Pre-request block: point this request at {{<var>}}+path, FAILING if <var> is unset.

    WHY THIS ASSERTS INSTEAD OF ONLY SKIPPING. Two guards look identical and are not
    the same thing:

      * an OPERATION guard — `if (!opId) skipRequest()` — is a LEGAL skip. The create
        under test was refused on purpose, so there is no operation to poll.
      * an ENVIRONMENT guard — `if (!internalBaseUrl) skipRequest()` — is a BROKEN
        HARNESS. The check is still meaningful and still expected to run; the runner
        merely failed to inject the variable.

    newman leaves NO trace of a skipped request, so the second kind used to pass by
    never running. The variable is therefore asserted by name before the skip.
    exec-coverage.py enforces this shape statically: a `skipRequest()` guard that reads
    a `*BaseUrl` variable and carries no `pm.test(` fails the gate.
    """
    reason = f" — {why}" if why else ""
    return [
        f"// HARNESS-CONFIG GUARD — {js_comment(var)} is injected by the newman runner (--env-var).",
        "// Missing value = misconfigured harness, NOT a legal mode: FAIL, then skip.",
        f"const __cfgUrl = pm.environment.get({js_str(var)}) || pm.variables.get({js_str(var)}) || '';",
        "if (__cfgUrl) {",
        f"  pm.request.url = __cfgUrl + {js_str(path)};",
        "} else {",
        f"  pm.test({js_str(f'harness config: {var} is set{reason}')}, () => {{",
        "    pm.expect.fail(" + js_str(
            f"{var} is not set — the newman runner "
            "(deploy/scripts/newman-parallel.sh --env-var) did not inject it. This step cannot "
            "run, and a check that cannot run MUST NOT be silently dropped.") + ");",
        "  });",
        "  pm.execution.skipRequest();",
        "}",
    ]


def poll_operation(op_var: str = "opId", auth: str = "jwtBootstrap",
                   name: str = "poll-op") -> Step:
    """Poll /operations/{{<op_var>}} until done, with a REAL inter-poll delay.

    newman executes the test script synchronously and acts on `setNextRequest` before
    any `setTimeout` fires, so a busy-wait immediately before the self-retry is the only
    construct that actually spaces the polls. Without it a 45-iteration loop covers
    ~0.2s of wall-clock and the poller "gives up" while the async tail is perfectly
    healthy (testing.md — the class that looks like a materialization lag but is really
    a probe that never waited).

    OperationService is `<exempt>` in the gateway authz catalog, but kaname still
    rejects unauthenticated callers, so the poll carries a Bearer.
    """
    return Step(
        name=name,
        method="GET",
        path="/operations/{{" + op_var + "}}",
        auth=auth,
        pre_script=[
            "// OPERATION guard (legal skip): the mutation under test was refused on",
            "// purpose, so there is no Operation to poll and nothing to assert.",
            f"if (!pm.environment.get({js_str(op_var)})) {{ pm.execution.skipRequest(); }}",
        ],
        test_script=[
            "pm.test('poll status 200', () => pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
            "const j = pm.response.json();",
            "if (pm.environment.get('_pollStarted') !== pm.info.requestName) {",
            "  pm.environment.set('_pollCount', '0');",
            "  pm.environment.set('_pollStarted', pm.info.requestName);",
            "}",
            "const pc = parseInt(pm.environment.get('_pollCount') || '0', 10);",
            f"if (!j.done && pc < {POLL_CAP}) {{",
            "  pm.environment.set('_pollCount', String(pc + 1));",
            f"  const _ipd = Date.now(); while (Date.now() - _ipd < {POLL_DELAY_MS}) void 0;"
            f"  /* real inter-poll delay: cap {POLL_CAP} x {POLL_DELAY_MS}ms budget (testing.md) */",
            "  pm.execution.setNextRequest(pm.info.requestName);",
            "  return;",
            "}",
            "pm.environment.unset('_pollCount');",
            "pm.environment.unset('_pollStarted');",
            "pm.test('operation done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
            "pm.test('operation succeeded (no error)', () => {",
            "  pm.expect(j.error, 'op error: ' + JSON.stringify(j.error || {})).to.be.oneOf([undefined, null]);",
            "});",
        ],
    )


# ---------------------------------------------------------------------------
# Postman v2.1 serialization
# ---------------------------------------------------------------------------

_MUX_VAR = {
    "public": "baseUrl",
    "internal": "internalBaseUrl",
    "external": "externalBaseUrl",
}


def _auth_pre_script(auth: str) -> List[str]:
    """JS snippet that sets/clears the Authorization header for one step."""
    if auth == "anonymous":
        return [
            "// per-step auth: DELIBERATE anonymous step",
            "pm.request.headers.remove('Authorization');",
        ]
    return [
        f"// per-step auth: bearer from env '{js_comment(auth)}'",
        f"const __t = pm.environment.get({js_str(auth)}) || pm.variables.get({js_str(auth)}) || '';",
        "if (__t) {",
        "  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __t});",
        "} else {",
        # HARNESS-CONFIG GUARD. An `auth="<envVar>"` step names the SUBJECT the case is
        # about ("an ordinary user must get 403"). If the fixture seed never wrote that
        # variable, dropping the header does not skip the check — it runs it as
        # ANONYMOUS, i.e. against a different principal entirely. The expectation
        # (401/403) usually still holds, so the case passes FOR THE WRONG REASON and the
        # subject under test is never exercised. That is precisely how this suite could
        # issue 54 header-less requests and still call one of them a 403 test.
        #
        # So the step is NOT SENT. FAIL naming the variable, THEN SKIP — the sanctioned
        # shape, identical to `require_env_url`. `pm.execution.skipRequest()` skips
        # exactly one request (its test script does not run either), so no other
        # assertion of this step is scored against a principal the case never named;
        # the pre-request assertion above has already run, so the skip stays RECORDED
        # as a failure naming the variable, never a mute one.
        f"  pm.test({js_str(f'harness config: {auth} is set (subject under test)')}, () => {{",
        "    pm.expect.fail(" + js_str(
            f"{auth} is not set — the authz-fixture seed "
            "(tests/authz-fixtures/setup.sh, or prodseed_all.py under production posture) did "
            "not provide this subject. Running the step anonymously would test a DIFFERENT "
            "principal and pass for the wrong reason.") + ");",
        "  });",
        "  pm.execution.skipRequest();",
        "}",
    ]


# ---------------------------------------------------------------------------
# Discovery + main
# ---------------------------------------------------------------------------

_INJECTED = {
    "Step": Step,
    "Case": Case,
    "assert_status": assert_status,
    "assert_grpc_code": assert_grpc_code,
    "assert_error_message_eql": assert_error_message_eql,
    "save_from_response": save_from_response,
    "assert_iam_operation_envelope": assert_iam_operation_envelope,
    "require_env_url": require_env_url,
    "poll_operation": poll_operation,
    "POLL_CAP": POLL_CAP,
    "POLL_DELAY_MS": POLL_DELAY_MS,
}
# ─────────────────────────────────────────────────────────────────────────────
# РЕШЕНИЯ НАБОРА, от которых зависит форма коллекции (#1379). Форму собирает
# общий слой; здесь объявлено ТОЛЬКО то, чем этот набор от остальных отличается.
# ─────────────────────────────────────────────────────────────────────────────

def _gateway_case_steps(case):
    """Конвейер шагов кейса края.

    Сброса захваченного идентификатора операции здесь НЕТ намеренно: край
    проверяет саму поверхность, а не жизненный цикл ресурса, и цепочек
    «мутация → опрос операции» между кейсами не строит.
    """
    return _assert_published_id_outcome(_assert_delete_operation_outcome(case.steps))


def _gateway_host_var(step):
    """Имя переменной адреса по объявленному слушателю шага.

    У края слушателей ТРИ, а не два: кроме публичного и внутреннего есть проверка
    внешней изоляции — тот же путь обязан отвечать отказом на объявленном TLS-крае.
    Неизвестное значение — ОТКАЗ генерации, а не молчаливый публичный адрес.
    """
    if step.mux not in _MUX_VAR:
        raise ValueError(f"step {step.name!r}: unknown mux {step.mux!r} "
                         f"(expected one of {sorted(_MUX_VAR)})")
    return _MUX_VAR[step.mux]


def _gateway_pre_head(step, var):
    """Непубличный слушатель адресуется переменной, которая ОБЯЗАНА быть задана.

    Профиль, её не задавший, обязан получить отказ по имени переменной, а не
    пропуск шага: пропуск неотличим от прохода.
    """
    if step.mux == "public":
        return []
    why = {
        "internal": "Internal* RPCs live ONLY on the cluster-internal REST listener",
        "external": "external-isolation check — Internal* paths must 404 on the "
                    "advertised TLS endpoint",
    }[step.mux]
    return require_env_url(var, step.path, why)


_EMIT = Emit(
    id_slug="kacho-gateway",
    # Слаг идентификатора и видимое имя РАСХОДЯТСЯ, и это не описка: слаг —
    # вход UUIDv5, и его смена перечеканила бы идентификаторы всех коллекций
    # края. Расхождение историческое, сведение его не трогает.
    display_name="kacho-api-gateway / newman",
    pre_global=lambda key: PRE_GLOBAL,
    steps_of=_gateway_case_steps,
    auth_pre=_auth_pre_script,
    host_var=_gateway_host_var,
    pre_head=_gateway_pre_head,
    # Строка запроса в сегменты пути не входит: у края кейсы адресуются с ней.
    path_segments=lambda path: [p for p in path.split("?")[0].strip("/").split("/") if p],
)


_RUN = Run(
    root=ROOT,
    cases_dir=CASES_DIR,
    out_dir=COLLECTIONS_DIR,
    scripts_dir=Path(__file__).resolve().parent,
    emit=_EMIT,
    case_cls=Case,
    injected=_INJECTED,
    before=None,
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
