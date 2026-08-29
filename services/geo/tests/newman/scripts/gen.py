#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""
tests/newman/scripts/gen.py — генератор Postman collections для kacho-geo из
декларативных case-файлов (Case/Step DSL, паритет с vpc/compute/iam suite'ами).

Использование:
    python3 scripts/gen.py             # все case-файлы
    python3 scripts/gen.py region      # один case-файл (region.py)
    python3 scripts/gen.py --validate  # делегирует в validate-cases.py

Источник истины — модули в tests/newman/cases/<name>.py, каждый экспортирует
переменную CASES — список объектов Case. gen.py делает 1:1 коллекцию на каждый
case-файл (collections/<name>.postman_collection.json).

Гео-специфика (в отличие от vpc/compute):
  * Region/Zone — ГЛОБАЛЬНЫЙ cluster-scoped каталог, НЕ project-scoped: у кейсов
    нет projectId, нет labels/description, нет per-object list-authz.
  * Публичное чтение (RegionService/ZoneService Get/List) — `<exempt>` в
    permission-каталоге: per-RPC FGA-Check с него СНЯТ, а аутентификация нет.
    Ветка `<exempt>` api-gateway требует валидного принципала и без него отвечает
    UNAUTHENTICATED, поэтому кейсу нужен любой живой токен, но НЕ нужен грант.
    Admin-CRUD гейтится `system_admin`@cluster (jwtBootstrap несёт его).
  * Admin-мутации живут в InternalRegion/ZoneService на cluster-internal REST
    listener ({{internalBaseUrl}}, :8081) — на публичном {{baseUrl}} их нет by
    design (ban #6). Помечай такие Step'ы `internal=True`.
  * Форма ответа мутации — СИНХРОННО ЗАВЕРШЁННАЯ Operation (syncop): каталог —
    config-INSERT в одной БД, без саги и async-worker'а, поэтому Internal
    Create/Update/Delete возвращают `Operation{done:true}` НЕМЕДЛЕННО:
    `metadata` несёт id ресурса, `result.response` — полное public-тело.
    Op-poll не требуется; `.response` разворачивается прямо из ответа мутации.
  * ОТКАЗ МУТАЦИИ ПРИЕЗЖАЕТ В `result.error`, А НЕ КОДОМ HTTP. Ошибки, найденные
    базой (23505 UNIQUE, 23503 FK), syncop финализирует как `Operation{done:true,
    error:…}` под HTTP 200; синхронным 4xx отвечает только предзаписная валидация
    (malformed id, coupling, required name, countryCode, immutable). Поэтому шаг
    мутации ОБЯЗАН проверять `result.error`: `assert_operation_envelope()` — для
    ожидаемого успеха, `assert_operation_failed(code, …)` — для ожидаемого отказа.
    Проверка «200 + форма конверта» без этого зачитывает ПРОВАЛИВШУЮСЯ мутацию
    как принятую.
  * Материализацию happy-path подтверждаем публичным read
    (RegionService.Get/ZoneService.Get); `retry_get_until_found` — ограниченный
    страховочный ретрай на 404 поверх окна видимости записи.
"""
from __future__ import annotations

import json
import re
import sys
import functools
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
    _PUB_ASSIGN_RE,
    _PUB_BIND_RE,
    _PUB_DECL_RE,
    _PUB_RESERVED,
    _PUB_SET_RE,
    _published_id_outcome_assert,
    _published_resource_vars,
    _reset_captured_operation_id,
    step_to_postman,
    _strip_js_comments,
)


ROOT = Path(__file__).resolve().parents[1]
SCRIPTS_DIR = Path(__file__).resolve().parent
CASES_DIR = ROOT / "cases"
OUT_DIR = ROOT / "collections"


# ---------------------------------------------------------------------------
# Декларативные структуры (паритет с vpc/compute gen.py — тот же Postman-emit)
# ---------------------------------------------------------------------------

@dataclass
class Step:
    """Один HTTP-запрос внутри case."""
    name: str
    method: str
    path: str  # относительный, {{baseUrl}}/{{internalBaseUrl}} префикс добавляется автоматически
    body: Optional[Dict] = None
    pre_script: List[str] = field(default_factory=list)
    test_script: List[str] = field(default_factory=list)
    # Per-step auth override.
    #   None          — заголовок не трогается (default — collection-level jwtBootstrap)
    #   "anonymous"   — Authorization снимается перед запросом
    #   "<envVar>"    — Authorization: Bearer {{envVar}} (значение из env)
    auth: Optional[str] = None
    # internal=True — запрос идёт на cluster-internal REST listener ({{internalBaseUrl}}),
    # а НЕ на публичный {{baseUrl}}. Internal*-RPC (InternalRegion/ZoneService) на публичном
    # листенере ОТСУТСТВУЮТ by design (ban #6) — там 404/Unimplemented.
    internal: bool = False


@dataclass
class Case:
    """Один тестовый кейс — может содержать несколько шагов."""
    id: str            # напр. REG-GET-CRUD-OK
    title: str         # человеко-читаемое описание
    classes: List[str] # CRUD / VAL / NEG / BVA / CONF / AUTHZ / PAGE / ...
    priority: str      # P0 / P1 / P2 / P3
    steps: List[Step]


# ---------------------------------------------------------------------------
# Утилиты-сниппеты pm.* (вставляются в шаги по необходимости)
# ---------------------------------------------------------------------------

# runId уникализирует ids свежесозданных Region/Zone в пределах прогона. Формат
# строго slug-safe ([a-z0-9]) — id Region/Zone обязаны быть lowercase-slug'ами
# (domain.ValidateID: ^[a-z][a-z0-9]*(-[a-z0-9]+)*$). Default-auth = jwtBootstrap
# (несёт system_viewer для public read И system_admin для internal admin-CRUD);
# per-step auth= его переопределяет.
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
    "const __jwt = pm.environment.get('jwtBootstrap') || pm.variables.get('jwtBootstrap') || '';",
    "if (__jwt && !pm.request.headers.has('Authorization')) {",
    "  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __jwt});",
    "}",
    *_URL_VAR_GUARD,
]


def save_from_response(jsonpath: str, env_var: str) -> List[str]:
    """Сохранить значение из response в env (best-effort, не роняет при отсутствии).

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
    """Internal Create/Update/Delete, которая обязана УДАТЬСЯ → 200 + Operation
    {done:true, без result.error}.

    geo Operation-id = ids.NewID('geo') = 'geo' + 17-char crockford-base32 (20 симв).
    Проверяем: 200, id совпадает с /^geo[0-9a-z]/, metadata — объект, операция
    ЗАВЕРШЕНА (`done:true` — syncop, без async-worker'а) и завершена БЕЗ ошибки.

    Про `result.error` отдельно, потому что без него проверка бессодержательна:
    отказ мутации, найденный базой (дубль имени/id, нарушение FK), приезжает НЕ
    кодом HTTP, а полем `error` внутри 200-конверта. Форма конверта у провалившейся
    мутации та же самая — id на месте, metadata на месте (id ресурса аллоцируется
    ДО записи). Шаг, проверяющий только форму, зачитывает провал как принятую
    мутацию, и суита остаётся зелёной ровно на том, что должна ловить. Правило
    платформы то же: дождаться `done` → убедиться, что ошибки нет → и только
    потом брать id из metadata.

    Для шага, который ОБЯЗАН провалиться, есть assert_operation_failed()."""
    return [
        *assert_status(200),
        "pm.test('Operation envelope (geo-prefixed id + metadata)', () => {",
        "  const j = pm.response.json();",
        "  pm.expect(j.id, 'operation.id ' + JSON.stringify(j)).to.match(/^geo[0-9a-z]+$/);",
        "  pm.expect(j.metadata, 'operation.metadata').to.be.an('object');",
        "});",
        "pm.test('operation is done (syncop: no async worker, no poll)', () => {",
        "  pm.expect(pm.response.json().done, 'operation.done ' + JSON.stringify(pm.response.json())).to.eql(true);",
        "});",
        "pm.test('mutation ACCEPTED — operation carries no result.error', () => {",
        "  const j = pm.response.json();",
        "  pm.expect(Boolean(j.error), 'мутация завершилась ОШИБКОЙ внутри 200-конверта: ' +",
        "    JSON.stringify(j.error || {})).to.eql(false);",
        "});",
    ]


def assert_operation_failed(code: int, code_name: str, message_substr: str = "") -> List[str]:
    """Мутация ПРИНЯТА транспортом (200 + Operation), но обязана ПРОВАЛИТЬСЯ:
    отказ, найденный базой, финализируется в `Operation{done:true, error:{code}}`.

    Зеркало assert_operation_envelope() для негативов. Без него негативный шаг
    приходится проверять только по побочному следствию («ресурс не появился»), а
    это молчит, если мутация провалилась ПО ДРУГОЙ причине — или если не
    провалилась вовсе, а следствие не наступило по третьей.

    code/code_name — ожидаемый google.rpc.Code (6 ALREADY_EXISTS, 9
    FAILED_PRECONDITION); message_substr — необязательный фрагмент контракт-тона."""
    lines = [
        *assert_status(200),
        "pm.test('Operation envelope (geo-prefixed id)', () => {",
        "  pm.expect(pm.response.json().id, 'operation.id').to.match(/^geo[0-9a-z]+$/);",
        "});",
        "pm.test('operation is done (syncop finalises the failure too)', () => {",
        "  pm.expect(pm.response.json().done, JSON.stringify(pm.response.json())).to.eql(true);",
        "});",
        f"pm.test({js_str(f'mutation REJECTED — result.error code {code} ({code_name})')}, () => {{",
        "  const j = pm.response.json();",
        "  pm.expect(Boolean(j.error), 'мутация ПРОШЛА, хотя обязана была быть отвергнута: ' +",
        "    JSON.stringify(j)).to.eql(true);",
        f"  pm.expect(j.error.code, JSON.stringify(j.error)).to.eql({code});",
        "});",
    ]
    if message_substr:
        # Апостроф в фрагменте порвал бы JS-литерал в кавычках — экранируем.
        message_substr = message_substr.replace("\\", "\\\\").replace("'", "\\'")
        lines += [
            f"pm.test({js_str(f'result.error message: {message_substr}')}, () => {{",
            "  const j = pm.response.json();",
            f"  pm.expect(String((j.error || {{}}).message), JSON.stringify(j.error)).to.include({js_str(message_substr)});",
            "});",
        ]
    return lines


def save_op_metadata_id(env_var: str) -> List[str]:
    """Сохранить <resource>Id из Operation.metadata (regionId/zoneId) в env."""
    return save_from_response(
        "(j.metadata && Object.keys(j.metadata).filter(k => k.endsWith('Id')).map(k => j.metadata[k])[0]) || ''",
        env_var,
    )


_RETRY_SEQ = [0]


def retry_get_until_found(step: Step, budget: int = 20, interval_ms: int = 500) -> Step:
    """Ограниченный страховочный ретрай публичного GET свежесозданного СВОЕГО ресурса.

    Мутация каталога завершается синхронно (syncop: строка закоммичена до того, как
    вернулся `Operation{done:true}`), поэтому в норме публичный read видит ресурс
    ПЕРВЫМ же запросом — ретрай здесь не рационал контракта, а узкая страховка на
    окно видимости записи (чтение с отставшей реплики, переподключение пула).
    Ретраим СЕБЯ на 404 до появления 200, spacing ~interval_ms (busy-wait — newman
    стреляет setNextRequest до setTimeout). Fail-open по budget: реальные assertions
    прогоняются ОДИН раз на терминальном ответе и падают, если ресурс так и не
    появился (никогда не маскируется, не бесконечно).

    Ретрай НЕ заменяет проверку самой мутации: успех/провал решает `result.error` в
    её ответе (assert_operation_envelope / assert_operation_failed). «Ресурс появился»
    — следствие, а не вердикт.

    Оборачивать ТОЛЬКО первый публичный read СВОЕГО свежесозданного ресурса — НЕ
    негативы (absent/malformed/cross-principal), там ретрай маскировал бы реальный
    отказ. Имя уникализируется (-rgf<N>), чтобы self-setNextRequest резолвился в СЕБЯ."""
    guard = [
        "// страховочный ретрай публичного GET на окно видимости записи (мутация уже",
        "// закоммичена синхронно). Ретраим СЕБЯ пока 404, потом реальные assertions.",
        "if (pm.environment.get('_rgfStarted') !== pm.info.requestName) {",
        "  pm.environment.set('_rgfCount', '0');",
        "  pm.environment.set('_rgfStarted', pm.info.requestName);",
        "}",
        "const _rc = parseInt(pm.environment.get('_rgfCount') || '0', 10);",
        f"if (pm.response.code === 404 && _rc < {budget}) {{",
        "  pm.environment.set('_rgfCount', String(_rc + 1));",
        f"  const _d = Date.now(); while (Date.now() - _d < {interval_ms}) {{ /* commit wait */ }}",
        "  pm.execution.setNextRequest(pm.info.requestName);",
        "  return;",
        "}",
        "pm.environment.unset('_rgfCount');",
        "pm.environment.unset('_rgfStarted');",
    ]
    _RETRY_SEQ[0] += 1
    return replace(step, name=f"{step.name}-rgf{_RETRY_SEQ[0]}",
                   test_script=guard + list(step.test_script))


def assert_createdat_truncated(field_expr: str = "pm.response.json().createdAt") -> List[str]:
    """CONF: createdAt усечён до секунд на wire (api-conventions: Truncate(time.Second)).

    RFC3339 без дробных секунд → 'YYYY-MM-DDTHH:MM:SSZ' (точки нет: дробные секунды —
    единственный источник '.' в timestamp). Микросекунды из БД не текут на wire."""
    return [
        "pm.test('createdAt truncated to seconds (no sub-second digits)', () => {",
        f"  const ts = String({field_expr});",
        "  pm.expect(ts, 'RFC3339 seconds ' + ts).to.match(/^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}(Z|[+-]\\d{2}:\\d{2})$/);",
        "});",
    ]


def assert_no_infra_fields(root_expr: str = "pm.response.json()") -> List[str]:
    """Two-projection инвариант: публичная проекция НЕ несёт инфра-полей.

    Region/Zone на public поверхности НИКОГДА не отдают numericInfraId / infra /
    hostClasses / underlayAnchor / capacityHint / failureDomainCount (инфра-
    чувствительные данные — только Internal*, security.md §Инфра-данные). В AS-IS
    Region/Zone message'и их не несут by construction — этот кейс лочит инвариант,
    чтобы регресс (добавление инфра-поля на public) был пойман."""
    return [
        "pm.test('public projection carries NO infra fields (two-projection invariant)', () => {",
        f"  const o = {root_expr};",
        "  const body = JSON.stringify(o).toLowerCase();",
        "  ['numericinfraid','infra','hostclasses','underlayanchor','capacityhint','failuredomaincount'].forEach(k => {",
        "    pm.expect(body, 'leaked infra field: ' + k + ' in ' + body).to.not.include(k);",
        "  });",
        "});",
    ]


# ---------------------------------------------------------------------------
# Сериализация в Postman v2.1 (идентична vpc/compute — тот же collection-format,
# так что run.sh / newman-parallel.sh / assert-suites-green совместимы byte-wise)
# ---------------------------------------------------------------------------

def _auth_pre_script(auth: str) -> List[str]:
    """JS-сниппет для per-step Authorization-header.

    "anonymous" → снимает Authorization. Имя env-переменной →
    Authorization: Bearer <значение env-var>."""
    if auth == "anonymous":
        return [
            "// per-step: anonymous",
            "pm.request.headers.remove('Authorization');",
        ]
    return [
        f"// per-step: bearer from env '{js_comment(auth)}'",
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
    _RETRY_SEQ[0] = 0


def _check_duplicate_ids() -> int:
    """HARD-FAIL: case-id обязан быть уникален среди всех кейсов всех файлов."""
    seen: Dict[str, str] = {}
    dups: List[str] = []
    for f in sorted(CASES_DIR.glob("*.py")):
        mod = load_cases(f)
        for c in getattr(mod, "CASES", []):
            if c.id in seen:
                dups.append(f"  - {c.id!r}: {seen[c.id]} и {f.name}")
            else:
                seen[c.id] = f.name
    if dups:
        sys.stderr.write("gen: FAIL — дубли case-id:\n")
        sys.stderr.write("\n".join(dups) + "\n")
        return 1
    return 0


def main(argv: List[str]) -> int:
    args = argv[1:]
    if "--validate" in args:
        import runpy
        sys.argv = [str(SCRIPTS_DIR / "validate-cases.py")]
        runpy.run_path(str(SCRIPTS_DIR / "validate-cases.py"), run_name="__main__")
        return 0

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
        mod = load_cases(f)
        cases = getattr(mod, "CASES", [])
        col = build_collection(_EMIT, svc, cases)
        out = OUT_DIR / f"{svc}.postman_collection.json"
        out.write_text(json.dumps(col, indent=2, ensure_ascii=False))
        print(f"[{svc}] {len(cases)} cases → {out.relative_to(ROOT)}")
    return 0


# ─────────────────────────────────────────────────────────────────────────────
# РЕШЕНИЯ НАБОРА, от которых зависит форма коллекции (#1379). Форму собирает
# общий слой; здесь объявлено ТОЛЬКО то, чем этот набор от остальных отличается.
# ─────────────────────────────────────────────────────────────────────────────

def _geo_case_steps(case):
    """Конвейер шагов кейса geo: утверждения об исходе, без обёртки видимости.

    Обёртки первого доступа здесь НЕТ намеренно: каталог размещения — глобальный
    справочник, права на него не материализуются под арендатора, и ждать нечего.
    """
    return _assert_published_id_outcome(
        _reset_captured_operation_id(_assert_delete_operation_outcome(case.steps)))


_EMIT = Emit(
    id_slug="kacho-geo",
    display_name="kacho-geo / newman",
    pre_global=lambda key: PRE_GLOBAL,
    steps_of=_geo_case_steps,
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
    "save_from_response": save_from_response,
    "save_op_metadata_id": save_op_metadata_id,
    "assert_operation_envelope": assert_operation_envelope,
    "assert_operation_failed": assert_operation_failed,
    "retry_get_until_found": retry_get_until_found,
    "assert_createdat_truncated": assert_createdat_truncated,
    "assert_no_infra_fields": assert_no_infra_fields,
}

# Загрузчик модулей кейсов, СВЯЗАННЫЙ решениями этого набора: перечень
# впрыскиваемых имён, сброс счётчиков имён шагов и трактовка дефиса в имени
# модуля. Отдельная проверка кейсов (`validate-cases.py`) обязана видеть ровно
# то, что увидит генерация, поэтому зовёт ЭТО связывание, а не общий загрузчик
# своими руками: иначе решения набора записаны дважды и расходятся молча.
load_cases = functools.partial(load_cases_module, injected=_INJECTED, before=_reset_step_name_counters, stem_dashes_to_underscores=False)

if __name__ == "__main__":
    sys.exit(main(sys.argv))
