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
    _reset_captured_operation_id,
    _strip_js_comments,
    js_comment,
    js_str,
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


def assert_status(code: int) -> List[str]:
    return [
        f"pm.test({js_str(f'status {code}')}, () => pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.eql({code}));",
    ]


def assert_grpc_code(code: int, code_name: str) -> List[str]:
    return [
        f"pm.test({js_str(f'grpc code {code} ({code_name})')}, () => {{",
        "  let j; try { j = pm.response.json(); } catch (e) { j = {}; }",
        f"  pm.expect(j.code, JSON.stringify(j)).to.eql({code});",
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


# Утверждение о том, что удаление ПРИНЯТО. Ровно одно и однозначное: `oneOf`
# со взаимоисключающими исходами утверждением не является (testing.md).
_DELETE_ACCEPTED = [
    "// УТВЕРЖДЕНИЕ ПО УМОЛЧАНИЮ для шага удаления: без него шаг зеленел бы и на",
    "// отказе, а следующий опрос уехал бы на opId предыдущей операции.",
    "pm.test('delete accepted: status 200', () => "
    "pm.expect(pm.response.code, pm.response.text()).to.eql(200));",
]


_PUB_BIND_RE = re.compile(r"\b(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=")
# Объявление БЕЗ инициализатора (`let j;`) и присваивание отдельным оператором
# (`j = pm.response.json()`). Форма `let j; try { j = pm.response.json(); } catch (e)
# { j = null; }` — самая частая запись безопасного разбора тела в этом корпусе, и
# `_PUB_BIND_RE` её не узнаёт вовсе: она требует `=` В ОБЪЯВЛЕНИИ. Пока узнавалось
# только объявление-с-инициализатором, цепочка происхождения рвалась на первом
# звене, и проход не видел ни публикации, ни всего, что от этого имени
# производилось дальше. Тот же распознаватель и по той же причине расширен в гейте
# `internal/repohygiene/artifactgates` — проход и гейт обязаны считать ОДНО И ТО ЖЕ,
# иначе они разойдутся на первом же шаге, записанном не по канону.
_PUB_DECL_RE = re.compile(r"\b(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*[;,]")
# Имя непосредственно перед `=`: `a.b = c` отсекается предшествующей точкой,
# `==`/`===`/`=>` — заглядыванием вперёд, `+=`/`!==`/`>=` — тем, что между именем и
# `=` у них стоит оператор.
_PUB_ASSIGN_RE = re.compile(r"(?:^|[^.\w$])([A-Za-z_$][\w$]*)\s*=(?![=>])")
# Слова, за которыми `имя =` связыванием значения не является. Перечень закрытый:
# «что-нибудь похожее на ключевое слово» отсекло бы имя, начинающееся так же.
_PUB_RESERVED = frozenset((
    "if", "for", "while", "switch", "return", "function", "const", "let", "var",
    "catch", "typeof", "new", "delete", "void", "in", "of",
))


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

    # ОБЛАСТЬ ВИДИМОСТИ БЕРЁТСЯ У ОБЪЯВЛЕНИЯ, А НЕ У ПРИСВАИВАНИЯ. `let j;` стоит на
    # верхнем уровне скрипта, а значение ему присваивают внутри `try { … }` — то
    # есть глубже. Считать глубиной связывания глубину присваивания значило бы
    # закрывать имя вместе с блоком `try`, и все последующие чтения `j` оказались бы
    # «вне области» — ровно наоборот тому, как это работает в JavaScript.
    decl_depth = {}
    for m in _PUB_DECL_RE.finditer(code):
        decl_depth[m.group(1)] = depth[m.start()]
    for m in _PUB_BIND_RE.finditer(code):
        decl_depth[m.group(1)] = depth[m.start()]

    sites = []  # (offset, depth, name, expr_at)
    for m in _PUB_BIND_RE.finditer(code):
        sites.append((m.start(), depth[m.start()], m.group(1), m.end()))
    for m in _PUB_ASSIGN_RE.finditer(code):
        name = m.group(1)
        if name in _PUB_RESERVED:
            continue
        at = m.start(1)
        # Объявление-с-инициализатором уже учтено выше: `const v = …` матчится и
        # сюда. Считать его дважды безвредно для вердикта, но смещение связывания
        # разошлось бы на длину `const `, а от смещения зависит проверка
        # «объявлено ДО использования».
        head = code[:at].rstrip()
        if head.endswith(("const", "let", "var")) and len(head) < at:
            tail = "const" if head.endswith("const") else ("let" if head.endswith("let") else "var")
            j = len(head) - len(tail)
            if j == 0 or not (code[j - 1].isalnum() or code[j - 1] in "_$"):
                continue
        sites.append((at, decl_depth.get(name, 0), name, m.end()))
    sites.sort(key=lambda s: s[0])

    for off, d, name, expr_at in sites:
        semi = code.find(";", expr_at)
        expr = code[expr_at:semi if semi >= 0 else len(code)]
        binds.append((off, d, name, "metadata" in expr or visible(off, expr)))

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
    host = "{{internalBaseUrl}}" if step.internal else "{{baseUrl}}"
    item: Dict = {
        "name": step.name,
        "request": {
            "method": step.method,
            "header": [{"key": "Content-Type", "value": "application/json"}],
            "url": {
                "raw": host + step.path,
                "host": [host],
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


def case_to_postman(case: Case) -> Dict:
    tags = [f"class:{c}" for c in case.classes] + [f"priority:{case.priority}"]
    return {
        "name": f"{case.id} — {case.title}",
        "description": " | ".join(tags),
        "item": [step_to_postman(s) for s in _assert_published_id_outcome(
            _reset_captured_operation_id(_assert_delete_operation_outcome(case.steps)))],
    }


def build_collection(service: str, cases: List[Case]) -> Dict:
    return {
        "info": {
            # Deterministic _postman_id (UUIDv5 over the collection name) so a
            # regeneration with no source change produces no diff. A random id
            # here made every regeneration dirty every collection, which meant
            # "generated matches source" could never be checked and a real drift
            # had nowhere to show. Postman only needs this to be stable+unique.
            "_postman_id": str(uuid.uuid5(uuid.NAMESPACE_URL, f"kacho-geo/newman/{service}")),
            "name": f"kacho-geo / newman / {service}",
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


def load_cases_module(path: Path):
    _reset_step_name_counters()
    spec = importlib.util.spec_from_file_location(path.stem, path)
    mod = importlib.util.module_from_spec(spec)
    # пробрасываем helpers в namespace модуля
    mod.Step = Step
    mod.Case = Case
    mod.assert_status = assert_status
    mod.assert_grpc_code = assert_grpc_code
    mod.save_from_response = save_from_response
    mod.save_op_metadata_id = save_op_metadata_id
    mod.assert_operation_envelope = assert_operation_envelope
    mod.assert_operation_failed = assert_operation_failed
    mod.retry_get_until_found = retry_get_until_found
    mod.assert_createdat_truncated = assert_createdat_truncated
    mod.assert_no_infra_fields = assert_no_infra_fields
    spec.loader.exec_module(mod)
    return mod


def _check_duplicate_ids() -> int:
    """HARD-FAIL: case-id обязан быть уникален среди всех кейсов всех файлов."""
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
        mod = load_cases_module(f)
        cases = getattr(mod, "CASES", [])
        col = build_collection(svc, cases)
        out = OUT_DIR / f"{svc}.postman_collection.json"
        out.write_text(json.dumps(col, indent=2, ensure_ascii=False))
        print(f"[{svc}] {len(cases)} cases → {out.relative_to(ROOT)}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
