#!/usr/bin/env python3

# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""tests/newman/scripts/gen.py — newman collection generator for the api-gateway suite.

Usage:
    python3 scripts/gen.py                # generate all case files
    python3 scripts/gen.py cluster_admin  # one case file

Source of truth: tests/newman/cases/<name>.py modules, each exporting a CASES
list of Case objects.

Slim adaptation of services/iam/tests/newman/scripts/gen.py — only the helpers the
api-gateway-owned cases need. This suite owns the cluster-RBAC admin surface
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
    _MUTATION_METHODS,
    _OP_POLL_PATH,
    _PUB_SET_RE,
    _assert_delete_operation_outcome,
    _asserts_done,
    _asserts_outcome,
    _assigns_env_var,
    _carries_assertion,
    _js_code_and_literals,
    _published_id_outcome_assert,
    _strip_js_comments,
    js_comment,
    js_str,
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

def assert_status(code: int) -> List[str]:
    return [
        f"pm.test({js_str(f'status {code}')}, () => pm.expect(pm.response.code, pm.response.text()).to.eql({code}));",
    ]


def assert_grpc_code(code: int, code_name: str) -> List[str]:
    return [
        f"pm.test({js_str(f'grpc code {code} ({code_name})')}, () => {{",
        "  const j = pm.response.json();",
        f"  pm.expect(j.code, JSON.stringify(j)).to.eql({code});",
        "});",
    ]


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


def _is_operation_id_var(env_var: str) -> bool:
    """Держит ли это имя идентификатор Operation — то есть читает ли его шаг опроса?

    Соглашение об именах едино на всё дерево: общий `opId` либо собственное имя
    кейса, оканчивающееся на `OpId`/`OperationId`. Идентификаторы РЕСУРСОВ под него
    не подпадают намеренно — их сохраняют один раз и читают много шагов спустя,
    тогда как устаревание опасно ровно у того, что потребляется следующим запросом.
    """
    return env_var == "opId" or env_var.endswith("OpId") or env_var.endswith("OperationId")


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

    OperationService is `<exempt>` in the gateway authz catalog, but kacho-iam still
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


# Утверждение о том, что удаление ПРИНЯТО. Ровно одно и однозначное: `oneOf`
# со взаимоисключающими исходами утверждением не является (testing.md).
_DELETE_ACCEPTED = [
    "// УТВЕРЖДЕНИЕ ПО УМОЛЧАНИЮ для шага удаления: без него шаг зеленел бы и на",
    "// отказе, а следующий опрос уехал бы на opId предыдущей операции.",
    "pm.test('delete accepted: status 200', () => "
    "pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
]


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
    if step.mux not in _MUX_VAR:
        raise ValueError(f"step {step.name!r}: unknown mux {step.mux!r} "
                         f"(expected one of {sorted(_MUX_VAR)})")
    var = _MUX_VAR[step.mux]
    raw = "{{" + var + "}}" + step.path
    item: Dict = {
        "name": step.name,
        "request": {
            "method": step.method,
            "header": [{"key": "Content-Type", "value": "application/json"}],
            "url": {
                "raw": raw,
                "host": ["{{" + var + "}}"],
                "path": [p for p in step.path.split("?")[0].strip("/").split("/") if p],
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
    # Non-public listeners are addressed through a harness variable that MUST be
    # present; require_env_url asserts it by name before skipping.
    if step.mux != "public":
        why = {
            "internal": "Internal* RPCs live ONLY on the cluster-internal REST listener",
            "external": "external-isolation check — Internal* paths must 404 on the "
                        "advertised TLS endpoint",
        }[step.mux]
        pre = require_env_url(var, step.path, why) + pre
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


def case_to_postman(case: Case) -> Dict:
    tags = [f"class:{c}" for c in case.classes] + [f"priority:{case.priority}"]
    return {
        "name": f"{case.id} — {case.title}",
        "description": " | ".join(tags),
        "item": [step_to_postman(s) for s in _assert_published_id_outcome(
            _assert_delete_operation_outcome(case.steps))],
    }


def build_collection(resource: str, cases: List[Case]) -> Dict:
    return {
        "info": {
            # Deterministic _postman_id (UUIDv5 over the collection name) so a
            # regeneration with no source change produces no diff. A random id
            # here made every regeneration dirty every collection, which meant
            # "generated matches source" could never be checked and a real drift
            # had nowhere to show. Postman only needs this to be stable+unique.
            "_postman_id": str(uuid.uuid5(uuid.NAMESPACE_URL, f"kacho-gateway/newman/{resource}")),
            "name": f"kacho-api-gateway / newman / {resource}",
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


def load_cases_module(path: Path):
    spec = importlib.util.spec_from_file_location(path.stem.replace("-", "_"), path)
    mod = importlib.util.module_from_spec(spec)
    for k, v in _INJECTED.items():
        setattr(mod, k, v)
    spec.loader.exec_module(mod)
    return mod


def main(argv: List[str]) -> int:
    want = set(argv[1:])
    found = sorted(CASES_DIR.glob("*.py"))
    if not found:
        print(f"no case files in {CASES_DIR}", file=sys.stderr)
        return 1
    COLLECTIONS_DIR.mkdir(parents=True, exist_ok=True)
    for f in found:
        res = f.stem
        if res.startswith("__"):
            continue
        if want and res not in want:
            continue
        mod = load_cases_module(f)
        cases = getattr(mod, "CASES", [])
        bad = [type(c).__name__ for c in cases if not isinstance(c, Case)]
        if bad:
            sys.stderr.write(f"[{res}] FAIL — non-Case items in CASES ({bad[:3]}).\n")
            return 1
        ids = [c.id for c in cases]
        dups = {x for x in ids if ids.count(x) > 1}
        if dups:
            sys.stderr.write(f"[{res}] FAIL — duplicate case-id: {sorted(dups)}\n")
            return 1
        col = build_collection(res, cases)
        out = COLLECTIONS_DIR / f"{res}.postman_collection.json"
        out.write_text(json.dumps(col, indent=2, ensure_ascii=False) + "\n")
        print(f"[{res}] {len(cases)} cases → {out.relative_to(ROOT)}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
