#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""
tests/newman/shared/harness.py — ЕДИНЫЙ shared helper-namespace для newman-генераторов
всех сервисов монорепо (H0 плана стандартизации тестов Kachō).

Раньше каждый `services/<svc>/tests/newman/scripts/gen.py` держал СВОЮ копию этого
namespace → неизбежный drift (registry скопировал nlb целиком с неверными дефолтами;
compute/storage/nlb недосут подмножества хелперов). Parity через copy-paste недостижим.

Теперь канон живёт ЗДЕСЬ (импорт, не копия). Каждый `gen.py`:
    import sys; from pathlib import Path
    sys.path.insert(0, str(Path(__file__).resolve().parents[3] / "tests" / "newman" / "shared"))
    from harness import (Step, Case, assert_status, assert_grpc_code, ...)
и оставляет у себя ТОЛЬКО resource-специфичные define/emit-блоки + machinery
(step_to_postman / build_collection / load_cases_module / main). PRE_GLOBAL остаётся
локальным (service-специфичен: zone-resolve и т.п.).

Извлечено из reference-эталона `services/vpc/tests/newman/scripts/gen.py` (vpc —
reference per `.claude/rules`, из него извлечена common-test-schema). Канонический
эталон сигнатур — `docs/plans/kacho-redesign-2026/tests/common-test-schema.md` §2.

Точки ЛЕГИТИМНОЙ per-service вариации параметризованы (НЕ форк кода):
  * `assert_operation_envelope(prefix_regex=...)` — op-id префикс у каждого домена свой
    (vpc `^[a-z0-9]+$`, iam/compute `^epd…`, storage `^sop…`, nlb `^(nlb|tgr|lst)…`);
  * `poll_operation_until_done(auth, budget, opid_guard, retry_comment, unset_started,
    name_counter, started_guard)` — per-service poll-дефолты (Koren-1 budget, opId-guard,
    started-guard дизайн iam, auth-несущий poll compute/iam);
  * `retry_until_authorized(budget, interval_ms)` — per-service RYW budget (vpc 25/500,
    iam/storage 15/400);
  * `retry_until_absent(rename, lead_comment)` — vpc суффиксирует шаг `-abs<N>`;
    iam/compute сохраняют имя (шаг — цель pre-clean `setNextRequest`), другой lead-коммент.
Дефолты = канон vpc. Каждый сервис привязывает свои значения тонким wrapper'ом в своём
gen.py → drift становится ЯВНЫМ config'ом в одном месте (а не скрытой форк-копией) и
сходится к канону в H1 отдельным reviewed-шагом (это уже изменение поведения).
"""
from __future__ import annotations

from dataclasses import dataclass, field, replace
from typing import Dict, List, Optional


# ---------------------------------------------------------------------------
# Декларативные структуры (источник — vpc reference; canonical Step/Case)
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
    # internal=True — запрос идёт на api-gateway cluster-internal REST listener
    # ({{internalBaseUrl}}, :8081), а НЕ на публичный cmux ({{baseUrl}}, :8080).
    #
    # Internal*-RPC (напр. AddressPool — admin-only ресурс, security.md) на публичном
    # листенере ОТСУТСТВУЮТ by design (ban #6: Internal.* не публикуется на external
    # endpoint) и отвечают 404. Без этого флага internal-коллекции слали запросы на
    # {{baseUrl}} и получали закономерный 404 («expected 404 to deeply equal 200») —
    # тест ловил не баг продукта, а собственную неверную посылку.
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
# Глобальные счётчики уникальных имён шагов (self-retry setNextRequest → себя).
#
# ВАЖНО: счётчики МОДУЛЬНЫЕ (живут в harness) и общие для ВСЕХ retry/poll-хелперов
# одного gen-прогона. Сервис-специфичный локальный retry-хелпер (напр. nlb `-cr`),
# который делит нумерацию с каноничными `-lst`/`-rya`/`-abs`, ОБЯЗАН инкрементить
# ИМЕННО `harness._RYA_SEQ` (а не свою копию), иначе interleaving суффиксов разъедется
# и коллекция перестанет быть byte-identical. Список (mutable) — чтобы `import`-щики
# делили ОДИН объект.
# ---------------------------------------------------------------------------

_POLL_SEQ = [0]
_RYA_SEQ = [0]


# ---------------------------------------------------------------------------
# Ассерт-хелперы (детерминированные, без retry) — §2.1 схемы
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


def save_from_response(jsonpath: str, env_var: str) -> List[str]:
    """Сохранить значение из response в env."""
    return [
        "try {",
        "  const j = pm.response.json();",
        f"  const v = ({jsonpath});",
        f"  if (v !== undefined && v !== null) pm.environment.set('{env_var}', String(v));",
        "} catch (e) {}",
    ]


def assert_operation_envelope(prefix_regex: str = "^[a-z0-9]+$") -> List[str]:
    """Mutation → async Operation envelope `{id: /<prefix_regex>/, metadata: object}`.

    `prefix_regex` — точный JS-regex тела id (по умолчанию канон vpc `^[a-z0-9]+$`).
    Каждый домен несёт свой op-id-префикс (iam/compute `^epd[a-z0-9]+$`, storage
    `^sop[a-z0-9]+$`, nlb/registry — свой) → сервис привязывает его wrapper'ом. Имя
    параметра `prefix_regex` сохранено для back-compat с уже-параметризованными
    nlb/registry call-site'ами (`assert_operation_envelope(prefix_regex="^nlb…")`)."""
    return [
        "pm.test('Operation envelope returned', () => {",
        "  const j = pm.response.json();",
        f"  pm.expect(j.id, 'operation.id').to.match(/{prefix_regex}/);",
        "  pm.expect(j.metadata, 'operation.metadata').to.be.an('object');",
        "});",
    ]


# ---------------------------------------------------------------------------
# EC read-your-writes retry-хелперы — §2.3 схемы.
# Оборачивать ТОЛЬКО первый доступ к своему свежему ресурсу (никогда negatives /
# cross-account deny / absent-id — там retry маскирует реальный deny).
# ---------------------------------------------------------------------------

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


# Канонический (vpc) lead-коммент retry_until_absent. iam/compute несут другой
# wording (name-preserving дизайн) → передают свой `lead_comment` из своего gen.py.
_ABSENT_LEAD_CANONICAL = [
    "// bounded retry over the revoke/contamination materialization window (read-your-writes",
    "// ON REVOKE): retry SELF while the must-be-absent thing is still present, spacing ~interval_ms.",
    "// Fail-open at budget -> the real leak-guard assertion runs once and FAILS if it is STILL",
    "// present (a GENUINE over-show hole never clears -> NEVER masked).",
]


def retry_until_absent(step: Step, still_present_expr: str, budget: int = 25,
                       interval_ms: int = 500, rename: bool = True,
                       lead_comment: Optional[List[str]] = None) -> Step:
    """Bounded retry a "must-be-ABSENT/empty" negative read over a read-your-writes-ON-
    REVOKE window — the MIRROR of retry_until_authorized for the deny/leak-guard side.

    `still_present_expr` is a JS boolean, TRUE while the must-be-absent thing is STILL
    present. Retries SELF while truthy, spacing ~interval_ms. Fail-OPEN at budget: the
    wrapped step's real assertion then runs once on the terminal response, so a GENUINE
    over-show hole still FAILS — a persistent leak can NEVER be masked; only a transient
    revoke/contamination window is absorbed. Use ONLY on a negative "must be absent/empty"
    read whose emptiness is guaranteed once the contaminating grant is genuinely gone —
    NEVER a real cross-account deny.

    `rename` (default True, vpc-канон): суффиксирует шаг `-abs<N>` чтобы self-loop
    setNextRequest(pm.info.requestName) резолвился в СЕБЯ (suites не префиксуют имена
    шагов case-id'ом). iam/compute передают `rename=False` — их leak-guard-шаги сами
    являются целью pre-clean `setNextRequest('<name>')`-прыжка, поэтому имя сохраняется
    (self-loop использует динамический pm.info.requestName и резолвится без rename).
    `lead_comment` (default канон vpc) — per-service wording lead-коммента."""
    lead = lead_comment if lead_comment is not None else _ABSENT_LEAD_CANONICAL
    guard = list(lead) + [
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
    new_ts = guard + list(step.test_script)
    if rename:
        _RYA_SEQ[0] += 1
        return replace(step, name=f"{step.name}-abs{_RYA_SEQ[0]}", test_script=new_ts)
    return replace(step, test_script=new_ts)


# ---------------------------------------------------------------------------
# op-poll с РЕАЛЬНОЙ inter-poll задержкой — §2.4 схемы.
# ---------------------------------------------------------------------------

def poll_operation_until_done(auth: Optional[str] = None, *, budget: int = 30,
                              opid_guard: bool = True, retry_comment: bool = True,
                              unset_started: bool = False, name_counter: bool = True,
                              started_guard: bool = False) -> Step:
    """Reusable poll step с retry-на-not-done через setNextRequest.
    До `budget` попыток с ~500ms задержкой между ними (≈budget*0.5s покрытия async-op
    tail, Koren #1), потом fail если done остался false.

    КАЖДЫЙ poll-шаг получает УНИКАЛЬНОЕ имя (poll-op-<N>) при `name_counter=True`:
    setNextRequest(pm.info.requestName) обязан ретраить СЕБЯ. При общем имени 'poll-op'
    (name_counter=False — iam/storage) newman резолвит имя в ДРУГОЙ poll-op → полагаться
    на started-guard/дисциплину сервиса.

    Параметры (per-service дефолты привязываются wrapper'ом в gen.py, канон = vpc):
      * `auth` — Bearer env-var для poll-запроса (iam: без него anti-anonymous
        интерсептор iam роняет 401; compute — опционален; None → header не трогается);
      * `budget` — retry cap (vpc/compute/nlb/registry/storage 30, iam 50/POLL_CAP);
      * `opid_guard` — guard на пустой opId / non-200 (пропустить poll-ассерты чисто,
        не добавлять к failure-count) — vpc/nlb/registry; iam/compute/storage без него;
      * `retry_comment` — inline-коммент перед setNextRequest (только vpc-канон);
      * `unset_started` — очистить `_pollStarted` на терминальном выходе (iam);
      * `name_counter` — уникальный суффикс `-<N>` в имени (vpc/compute/nlb/registry);
      * `started_guard` — pre-request reset `_pollCount` под request-name-scoped флагом
        `_pollStarted` (iam — иммунитет от bleed прежнего кейса mid-exhaustion).

    Сохраняет `lastOpError`/`lastOpResponse` в env для последующей проверки.
    """
    if name_counter:
        _POLL_SEQ[0] += 1
        name = f"poll-op-{_POLL_SEQ[0]}"
    else:
        name = "poll-op"

    pre_script: List[str] = []
    if started_guard:
        pre_script = [
            "// poll-counter reset on first entry (request-name-scoped flag);",
            "// re-invocations via setNextRequest skip the reset.",
            "if (pm.environment.get('_pollStarted') !== pm.info.requestName) {",
            "  pm.environment.set('_pollCount', '0');",
            "  pm.environment.set('_pollStarted', pm.info.requestName);",
            "}",
        ]

    test_script: List[str] = []
    if opid_guard:
        # Guard: if opId was empty (prior step was sync-rejected e.g. 403) or
        # response is non-200, skip all poll assertions cleanly.
        test_script += [
            "if (!pm.environment.get('opId') || pm.response.code !== 200) {",
            "  pm.environment.unset('_pollCount');",
            "  return;",
            "}",
        ]
    test_script += [
        "pm.test('poll status 200', () => pm.expect(pm.response.code).to.eql(200));",
        "const j = pm.response.json();",
        "const pc = parseInt(pm.environment.get('_pollCount') || '0', 10);",
        f"if (!j.done && pc < {budget}) {{",
        "  pm.environment.set('_pollCount', String(pc + 1));",
        # Real inter-poll delay (~500ms) between retries. newman runs test scripts
        # synchronously and fires setNextRequest before any setTimeout callback, so a
        # busy-wait is the only way to actually space out polls (Koren #1).
        "  const _pd = Date.now(); while (Date.now() - _pd < 500) { /* inter-poll delay ~500ms (Koren #1) */ }",
    ]
    if retry_comment:
        test_script += ["  // Postman async-friendly retry: re-invoke same request name"]
    test_script += [
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        "pm.environment.unset('_pollCount');",
    ]
    if unset_started:
        test_script += ["pm.environment.unset('_pollStarted');"]
    test_script += [
        "pm.test('operation done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
        "if (j.error) pm.environment.set('lastOpError', JSON.stringify(j.error));",
        "else pm.environment.unset('lastOpError');",
        "if (j.response) pm.environment.set('lastOpResponse', JSON.stringify(j.response));",
    ]

    return Step(
        name=name,
        method="GET",
        path="/operations/{{opId}}",
        auth=auth,
        pre_script=pre_script,
        test_script=test_script,
    )


# ---------------------------------------------------------------------------
# ensure_<resource> — анти-phantom op.error-guard (§2.5 схемы).
#
# Фикстур-seed helper'ы (`tests/authz-fixtures/setup.sh`: bash `ensure_account`/
# `ensure_project`/…) СОЗДАЮТ ресурс асинхронно (Operation) и извлекают его id из
# `metadata.<res>Id`. Kachō Operation несёт pre-allocated id в `metadata` ДАЖЕ на
# `done:true` С `error` (id аллоцируется до async-фейла) → чтение `metadata.<res>Id`
# без проверки `result.error` вернёт ФАНТОМНЫЙ id несозданного ресурса → downstream
# FGA-биндинги пишутся против фантома (gateway 200), а cross-service peer-check
# (vpc/compute → iam ProjectService.Get) отдаёт NOT_FOUND → каскад.
#
# КАНОНИЧЕСКИЙ порядок (энфорсить в КАЖДОМ ensure_<resource>):
#     POST → op_id → poll_op(op_id) до done:true → ASSERT !result.error →
#     ТОЛЬКО ТОГДА извлечь metadata.<res>Id
#
# Для newman-кейсов, self-seed'ящих ресурс через api-gateway, хелпер ниже эмитит
# test_script, реализующий этот guard (assert !lastOpError ПЕРЕД save id). Использует
# `lastOpError`, который проставляет poll_operation_until_done. Additive — не ломает
# byte-identity существующих коллекций (ни один сервис его пока не зовёт; проводка в
# self-seed-кейсы — H4).
# ---------------------------------------------------------------------------

def ensure_resource_id_guarded(metadata_field: str, env_var: str) -> List[str]:
    """Анти-phantom: assert предыдущий Operation НЕ error (lastOpError пуст) ПЕРЕД
    извлечением pre-allocated id из `metadata.<metadata_field>` в `env_var`.

    Ставить СРАЗУ после `poll_operation_until_done()` для create-шага фикстуры/self-seed.
    Если Operation завершился с error — id фантомный, save НЕ выполняется, а ассерт
    падает (fail-closed), не давая фантому утечь в downstream FGA/peer-check."""
    return [
        "pm.test('operation succeeded (no phantom id)', () => {",
        "  pm.expect(pm.environment.get('lastOpError'), 'operation error -> phantom id')"
        ".to.be.oneOf([undefined, null, '']);",
        "});",
        "if (!pm.environment.get('lastOpError')) {",
        "  try {",
        "    const j = pm.response.json();",
        f"    const v = ((j.metadata || {{}}).{metadata_field});",
        f"    if (v !== undefined && v !== null) pm.environment.set('{env_var}', String(v));",
        "  } catch (e) {}",
        "}",
    ]
