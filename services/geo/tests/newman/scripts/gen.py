#!/usr/bin/env python3
# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""
tests/newman/scripts/gen.py — генератор Postman collections из декларативных
case-файлов kacho-geo.

Использование:
    python3 scripts/gen.py            # все case-файлы
    python3 scripts/gen.py region     # один (cases/region.py → collections/region.postman_collection.json)
    python3 scripts/gen.py --validate # делегирует в validate-cases.py (dup-id + CASES-INDEX)

Источник истины — модули в tests/newman/cases/<name>.py, каждый экспортирует
переменную CASES — список объектов Case (см. ниже). Структура/DSL воспроизводят
эталон kacho-vpc/tests/newman (тот же Step/Case + assert_*-хелперы + сериализация
в Postman v2.1 + per-step auth-override), суженные под read-only leaf-каталог geo:

  * geo — не project-scoped: public read (RegionService/ZoneService Get/List)
    гейтится ambient authN (см. env jwtBootstrap); нет _suiteProjectId / zone-resolve
    setup-item / pool-seed из vpc-варианта.
  * poll_operation_until_done оставлен для будущего расширения на admin-CRUD
    (InternalRegion/ZoneService на :9091) — публичные read его не используют.
"""
from __future__ import annotations

import json
import sys
import uuid
import importlib.util
from pathlib import Path
from dataclasses import dataclass, field
from typing import List, Dict, Optional

ROOT = Path(__file__).resolve().parents[1]
SCRIPTS_DIR = Path(__file__).resolve().parent
CASES_DIR = ROOT / "cases"
OUT_DIR = ROOT / "collections"


# ---------------------------------------------------------------------------
# Декларативные структуры (идентичны эталону kacho-vpc)
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
    # Per-step auth override.
    #   None         — header не трогается (наследует collection default — jwtBootstrap)
    #   "anonymous"  — Authorization снимается перед запросом (authN-negative)
    #   "<envVar>"   — Authorization: Bearer {{envVar}} (значение из env при выполнении)
    auth: Optional[str] = None
    # internal=True — запрос идёт на cluster-internal REST listener ({{internalBaseUrl}}),
    # а НЕ на публичный ({{baseUrl}}). Public geo read его не использует; оставлен для
    # будущего admin-CRUD (InternalRegion/ZoneService — Internal-only, security.md ban #6).
    internal: bool = False


@dataclass
class Case:
    """Один тестовый кейс — может содержать несколько шагов."""
    id: str  # например REG-GET-CRUD-OK
    title: str  # человеко-читаемое описание
    classes: List[str]  # CRUD / VAL / NEG / BVA / CONF / AUTHZ / PAGE / SEC
    priority: str  # P0 / P1 / P2 / P3
    steps: List[Step]


# ---------------------------------------------------------------------------
# Collection-level pre-request: runId + default auth (jwtBootstrap).
#
# geo public read гейтится ambient authN. Дефолтный принципал — jwtBootstrap
# (seeded admin с viewer-floor @ cluster — см. tests/authz-fixtures; тот же
# субъект, что использует iam/geo-read.py). Per-step auth= перекрывает это
# item-level pre-request скриптом (_auth_pre_script): "anonymous" снимает header,
# имя env-var подставляет другой Bearer.
# ---------------------------------------------------------------------------

PRE_GLOBAL = [
    "if (!pm.environment.get('runId') || pm.environment.get('runId') === '') {",
    "  // runId формат: только [a-z0-9] — стабилен для любых суффиксов имён.",
    "  const t = Date.now().toString(36);",
    "  const r = Math.floor(Math.random() * 1e9).toString(36);",
    "  pm.environment.set('runId', (t + r).replace(/[^a-z0-9]/g, '').slice(-10));",
    "}",
    "// Default auth: jwtBootstrap (admin/viewer-floor). Per-step auth= overrides",
    "// this via the item-level pre-request script (_auth_pre_script).",
    "const __defaultJwt = pm.environment.get('jwtBootstrap') || pm.variables.get('jwtBootstrap') || '';",
    "if (__defaultJwt && !pm.request.headers.has('Authorization')) {",
    "  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __defaultJwt});",
    "}",
]


# ---------------------------------------------------------------------------
# Утилиты-сниппеты pm.* (вставляются в шаги по необходимости; сигнатуры — как в vpc)
# ---------------------------------------------------------------------------

def assert_status(code: int) -> List[str]:
    return [
        f"pm.test('status {code}', () => pm.expect(pm.response.code, JSON.stringify(pm.response.text())).to.eql({code}));",
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
    """Сохранить значение из response в env (для List→capture-id→Get цепочек)."""
    return [
        "try {",
        "  const j = pm.response.json();",
        f"  const v = ({jsonpath});",
        f"  if (v !== undefined && v !== null) pm.environment.set('{env_var}', String(v));",
        "} catch (e) {}",
    ]


def assert_body_notcontains_infra() -> List[str]:
    """Two-projection / capacity-anonymization security-lock (GEO-1-05 / GEO-1-33):
    публичная проекция Region/Zone НЕ несёт инфра-полей (numericInfraId, hostClasses,
    underlayAnchor, capacityHint, failureDomainCount) и ось-дискриминатор
    (placementType/placementScope) — эти данные живут ТОЛЬКО в Internal*-проекции
    (:9091). Ассерт на СЕРИАЛИЗОВАННОМ теле (не только на распарсенных ключах) —
    host-class-токен физически не выходит на public ни в каком виде.

    Инвариант ungated (действует в GEO-1 безусловно) и стабилен через границу
    редизайна: в AS-IS этих полей нет вовсе; после редизайна они уезжают в Internal —
    в обоих состояниях public-тело их не содержит."""
    return [
        "pm.test('two-projection: public body carries no infra/placement/host-class fields', () => {",
        "  const raw = pm.response.text();",
        "  const forbidden = ['numericInfraId','hostClasses','underlayAnchor','capacityHint',",
        "                     'failureDomainCount','placementType','placementScope'];",
        "  forbidden.forEach(k => pm.expect(raw, 'leaked ' + k + ': ' + raw).to.not.include(k));",
        "});",
    ]


# ---------------------------------------------------------------------------
# poll_operation_until_done — reusable poll step (для будущего admin-CRUD на :9091;
# публичные read его не используют). УНИКАЛЬНОЕ имя poll-op-<N>: setNextRequest(
# pm.info.requestName) обязан ретраить СЕБЯ (общее имя → newman прыгает в другой
# poll-op → пропуск setup-шагов кейсов). До 30 попыток × ~500ms (≈15s async-tail).
# ---------------------------------------------------------------------------

_POLL_SEQ = [0]


def poll_operation_until_done() -> Step:
    _POLL_SEQ[0] += 1
    return Step(
        name=f"poll-op-{_POLL_SEQ[0]}",
        method="GET",
        path="/operations/{{opId}}",
        internal=True,
        test_script=[
            "if (!pm.environment.get('opId') || pm.response.code !== 200) {",
            "  pm.environment.unset('_pollCount');",
            "  return;",
            "}",
            "pm.test('poll status 200', () => pm.expect(pm.response.code).to.eql(200));",
            "const j = pm.response.json();",
            "const pc = parseInt(pm.environment.get('_pollCount') || '0', 10);",
            "if (!j.done && pc < 30) {",
            "  pm.environment.set('_pollCount', String(pc + 1));",
            "  const _pd = Date.now(); while (Date.now() - _pd < 500) { /* inter-poll delay ~500ms */ }",
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


# ---------------------------------------------------------------------------
# Сериализация в Postman v2.1 (идентична эталону kacho-vpc)
# ---------------------------------------------------------------------------

def _auth_pre_script(auth: str) -> List[str]:
    """JS-сниппет для per-step Authorization-header.
    "anonymous" → снимает Authorization. Имя env-переменной → Bearer <env-var>."""
    if auth == "anonymous":
        return [
            "// per-step: anonymous (authN-negative)",
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


def build_collection(name: str, cases: List[Case]) -> Dict:
    return {
        "info": {
            "_postman_id": str(uuid.uuid4()),
            "name": f"kacho-geo / newman / {name}",
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
    mod.assert_field_violation = assert_field_violation
    mod.save_from_response = save_from_response
    mod.assert_body_notcontains_infra = assert_body_notcontains_infra
    mod.poll_operation_until_done = poll_operation_until_done
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
        sys.stderr.write("gen: FAIL — дубли case-id (case-id должен быть уникален):\n")
        sys.stderr.write("\n".join(dups) + "\n")
        return 1
    return 0


def main(argv: List[str]) -> int:
    args = argv[1:]
    if "--validate" in args:
        # делегируем полную валидацию (dup-id + каталогизация в CASES-INDEX) в validate-cases.py
        import runpy
        sys.argv = [str(SCRIPTS_DIR / "validate-cases.py")]
        runpy.run_path(str(SCRIPTS_DIR / "validate-cases.py"), run_name="__main__")
        return 0  # validate-cases.py делает sys.exit сам

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    want = set(args)
    found = sorted(CASES_DIR.glob("*.py"))
    if not found:
        print(f"no case files in {CASES_DIR}")
        return 1
    if _check_duplicate_ids() != 0:
        return 1
    for f in found:
        name = f.stem
        if want and name not in want:
            continue
        mod = load_cases_module(f)
        cases = getattr(mod, "CASES", [])
        col = build_collection(name, cases)
        out = OUT_DIR / f"{name}.postman_collection.json"
        out.write_text(json.dumps(col, indent=2, ensure_ascii=False))
        print(f"[{name}] {len(cases)} cases → {out.relative_to(ROOT)}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
