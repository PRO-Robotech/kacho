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
    нет projectId, нет labels/description, нет per-object list-authz. Public read
    гейтится `viewer`@cluster (jwtBootstrap несёт system_viewer); admin-CRUD
    гейтится `system_admin`@cluster (jwtBootstrap несёт и его).
  * Admin-мутации живут в InternalRegion/ZoneService на cluster-internal REST
    listener ({{internalBaseUrl}}, :8081) — на публичном {{baseUrl}} их нет by
    design (ban #6). Помечай такие Step'ы `internal=True`.
  * Async-форма: Internal Create/Update/Delete возвращают Operation{done:false}.
    ВНИМАНИЕ — geo Operation-id ('geo…') сейчас НЕ маршрутизируется api-gateway
    OpsProxy (prefix 'geo' отсутствует в prefixToBackend → InvalidArgument):
    PRO-Robotech/kacho#55. Поэтому GREEN-кейсы НЕ поллят Operation, а подтверждают
    материализацию мутации через ПУБЛИЧНЫЙ read (RegionService.Get/ZoneService.Get)
    с bounded read-your-writes retry (retry_get_until_found). Явный op-poll держит
    ОДИН RED-lock кейс (operation.py, `# verifies #55`).
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
]


def assert_status(code: int) -> List[str]:
    return [
        f"pm.test('status {code}', () => pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.eql({code}));",
    ]


def assert_grpc_code(code: int, code_name: str) -> List[str]:
    return [
        f"pm.test('grpc code {code} ({code_name})', () => {{",
        "  let j; try { j = pm.response.json(); } catch (e) { j = {}; }",
        f"  pm.expect(j.code, JSON.stringify(j)).to.eql({code});",
        "});",
    ]


def save_from_response(jsonpath: str, env_var: str) -> List[str]:
    """Сохранить значение из response в env (best-effort, не роняет при отсутствии)."""
    return [
        "try {",
        "  const j = pm.response.json();",
        f"  const v = ({jsonpath});",
        f"  if (v !== undefined && v !== null) pm.environment.set('{env_var}', String(v));",
        "} catch (e) {}",
    ]


def assert_operation_envelope() -> List[str]:
    """Internal Create/Update/Delete → 200 + Operation envelope с geo op-id.

    geo Operation-id = ids.NewID('geo') = 'geo' + 17-char crockford-base32 (20 симв).
    Проверяем форму синхронного ответа мутации: 200, id совпадает с /^geo[0-9a-z]/,
    metadata — объект. Это работает НЕЗАВИСИМО от #55 (роутинг ломается только на
    последующем op-poll, не на самой мутации)."""
    return [
        *assert_status(200),
        "pm.test('Operation envelope (geo-prefixed id + metadata)', () => {",
        "  const j = pm.response.json();",
        "  pm.expect(j.id, 'operation.id ' + JSON.stringify(j)).to.match(/^geo[0-9a-z]+$/);",
        "  pm.expect(j.metadata, 'operation.metadata').to.be.an('object');",
        "});",
    ]


def save_op_metadata_id(env_var: str) -> List[str]:
    """Сохранить <resource>Id из Operation.metadata (regionId/zoneId) в env."""
    return save_from_response(
        "(j.metadata && Object.keys(j.metadata).filter(k => k.endsWith('Id')).map(k => j.metadata[k])[0]) || ''",
        env_var,
    )


_RETRY_SEQ = [0]


def retry_get_until_found(step: Step, budget: int = 20, interval_ms: int = 500) -> Step:
    """Bounded read-your-writes retry публичного GET свежесозданного СВОЕГО ресурса.

    Internal Create/Update — async (worker коммитит ресурс вне синхронного ответа).
    Op-poll недоступен (geo Operation-id не маршрутизируется, #55), поэтому
    материализацию мутации подтверждаем публичным RegionService.Get/ZoneService.Get:
    он кратко отдаёт 404, пока worker не закоммитил row. Ретраим СЕБЯ на 404 до
    появления 200, spacing ~interval_ms (busy-wait — newman стреляет setNextRequest
    до setTimeout). budget*interval покрывает async-worker tail (~10s). Fail-open по
    budget: реальные assertions прогоняются ОДИН раз на терминальном ответе и падают,
    если ресурс так и не появился (никогда не маскируется, не бесконечно).

    Оборачивать ТОЛЬКО первый публичный read СВОЕГО свежесозданного ресурса — НЕ
    негативы (absent/malformed/cross-principal), там ретрай маскировал бы реальный
    отказ. Имя уникализируется (-rgf<N>), чтобы self-setNextRequest резолвился в СЕБЯ."""
    guard = [
        "// bounded read-your-writes retry публичного GET (async-worker commit window;",
        "// op-poll недоступен из-за #55). Ретраим СЕБЯ пока 404, потом реальные assertions.",
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


def poll_geo_op_red() -> Step:
    """RED op-poll шаг для operation.py bug-lock (`# verifies #55`).

    В ОТЛИЧИЕ от штатного poll (skip-on-non-200) этот шаг ЯВНО ассертит, что
    op-poll МАРШРУТИЗИРУЕТСЯ (не 400 InvalidArgument) и доходит до done — то есть
    держит пост-фикс контракт. Сейчас gateway OpsProxy отвергает geo op-id с
    400 'invalid operation id' → эти assertions КРАСНЫЕ; станут зелёными после
    добавления geo-prefix в prefixToBackend (#55). Ретраит СЕБЯ на done:false до
    budget (config-INSERT завершается быстро, но worker всё же async)."""
    return Step(
        name="poll-geo-op",
        method="GET",
        path="/operations/{{geoOpId}}",
        test_script=[
            "pm.test('op-poll routes to geo backend — NOT InvalidArgument (verifies #55)', () => {",
            "  pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.not.eql(400);",
            "});",
            "pm.test('op-poll status 200 (verifies #55)', () => pm.expect(pm.response.code).to.eql(200));",
            "if (pm.response.code !== 200) { pm.environment.unset('_geoPollCount'); return; }",
            "const j = pm.response.json();",
            "const pc = parseInt(pm.environment.get('_geoPollCount') || '0', 10);",
            "if (!j.done && pc < 20) {",
            "  pm.environment.set('_geoPollCount', String(pc + 1));",
            "  const _d = Date.now(); while (Date.now() - _d < 500) { /* inter-poll delay */ }",
            "  pm.execution.setNextRequest(pm.info.requestName);",
            "  return;",
            "}",
            "pm.environment.unset('_geoPollCount');",
            "pm.test('operation done', () => pm.expect(j.done, JSON.stringify(j)).to.eql(true));",
        ],
    )


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
        f"// per-step: bearer from env '{auth}'",
        f"const __t = pm.environment.get('{auth}') || pm.variables.get('{auth}') || '';",
        "if (__t) {",
        "  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __t});",
        "} else {",
        "  pm.request.headers.remove('Authorization');",
        "}",
    ]


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
            "_postman_id": str(uuid.uuid4()),
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

def load_cases_module(path: Path):
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
    mod.retry_get_until_found = retry_get_until_found
    mod.poll_geo_op_red = poll_geo_op_red
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
